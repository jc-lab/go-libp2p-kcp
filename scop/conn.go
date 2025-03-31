// Copyright 2025 JC-Lab.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package scop

import (
	"context"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

type ConnState int

const (
	StateClosed ConnState = iota
	StateSynSent
	StateEstablished
	StateHalfClosed
)

type Conn struct {
	config *Config
	parent net.Conn
	state  atomic.Int32

	readDeadline  time.Time
	writeDeadline time.Time

	lastRecv       time.Time // 마지막으로 패킷을 받은 시간
	keepaliveTimer *time.Timer

	closeOnce sync.Once
	mutex     sync.Mutex

	ctx        context.Context
	cancelFunc context.CancelFunc

	readBuf []byte
}

func Client(parent net.Conn, opts ...Option) (*Conn, error) {
	return ClientWithContext(context.Background(), parent, opts...)
}

func ClientWithContext(parentCtx context.Context, parent net.Conn, opts ...Option) (*Conn, error) {
	config := DefaultConfig()

	for _, opt := range opts {
		opt(config)
	}

	ctx, cancel := context.WithCancel(parentCtx)
	conn := &Conn{
		config:     config,
		parent:     parent,
		ctx:        ctx,
		cancelFunc: cancel,
	}
	conn.setState(StateSynSent)

	if err := conn.connect(); err != nil {
		_ = conn.reset()
		return nil, err
	}

	conn.setState(StateEstablished)
	conn.startKeepalive()
	return conn, nil
}

func (c *Conn) GetState() ConnState {
	return c.getState()
}

func (c *Conn) setState(state ConnState) {
	c.state.Store(int32(state))
}

func (c *Conn) getState() ConnState {
	return ConnState(c.state.Load())
}

func (c *Conn) connect() error {
	header := &Header{
		Version: Version,
		Flags:   FlagSYN,
	}

	rto := c.config.InitialRTO
	for i := 0; i < c.config.MaxRetries; i++ {
		if err := c.writeSegment(header, nil); err != nil {
			return err
		}

		recvHeader, err := c.waitForAck()
		if err == nil && recvHeader.Flags&FlagSYN != 0 {
			return nil
		}

		timer := time.NewTimer(rto)
		select {
		case <-timer.C:
			rto *= 2
			if rto > c.config.MaxRTO {
				rto = c.config.MaxRTO
			}
			continue
		case <-c.ctx.Done():
			timer.Stop()
			return ErrConnClosed
		}
	}

	return ErrTimeout
}

func (c *Conn) readSegment() (*Header, []byte, error) {
	headerBuf := make([]byte, headerSize)

	n, err := io.ReadFull(c.parent, headerBuf)
	if err != nil {
		return nil, nil, err
	}

	header := &Header{}
	if err := header.Unpack(headerBuf[:n]); err != nil {
		return nil, nil, err
	}

	var data []byte
	if header.DataSize > 0 {
		data = make([]byte, header.DataSize)
		if _, err = io.ReadFull(c.parent, data); err != nil {
			return nil, nil, err
		}
	}

	if header.Version != Version {
		return nil, nil, ErrInvalidPacket
	}

	c.updateLastRecv()

	return header, data, nil
}

func (c *Conn) waitForAck() (*Header, error) {
	header, _, err := c.waitSegment()
	if err != nil {
		return nil, err
	}

	if (header.Flags&FlagACK) == 0 || header.DataSize != 0 {
		return nil, ErrInvalidPacket
	}

	return header, nil
}

func (c *Conn) writeSegment(header *Header, payload []byte) error {
	packet := append(header.Pack(), payload...)
	_, err := c.parent.Write(packet)
	return err
}

func (c *Conn) waitSegment() (*Header, []byte, error) {
	deadline := time.Now().Add(c.config.InitialRTO)
	if err := c.parent.SetReadDeadline(deadline); err != nil {
		return nil, nil, err
	}
	defer c.parent.SetReadDeadline(time.Time{})

	return c.readSegment()
}

func (c *Conn) Write(b []byte) (n int, err error) {
	if c.getState() != StateEstablished {
		return 0, ErrInvalidState
	}

	header := &Header{
		Version:  Version,
		Flags:    FlagPSH,
		DataSize: uint16(len(b)),
	}

	return len(b), c.writeSegment(header, b)
}

func (c *Conn) Read(b []byte) (n int, err error) {
	if len(c.readBuf) > 0 {
		n = copy(b, c.readBuf)
		c.readBuf = c.readBuf[n:]
	}
	if n > 0 {
		return n, nil
	}
	var chunk []byte
	for chunk == nil {
		chunk, err = c.readData()
		if err != nil {
			return 0, err
		}
	}
	n = copy(b, chunk)
	c.readBuf = chunk[n:]
	return
}

func (c *Conn) readData() ([]byte, error) {
	c.mutex.Lock()
	state := c.getState()
	deadline := c.readDeadline
	c.mutex.Unlock()
	readable := state == StateEstablished || state == StateHalfClosed
	if !readable {
		return nil, ErrInvalidState
	}

	if !deadline.IsZero() {
		if err := c.parent.SetReadDeadline(deadline); err != nil {
			return nil, err
		}
		defer c.parent.SetReadDeadline(time.Time{})
	}

	header, data, err := c.readSegment()
	if err != nil {
		_ = c.reset()
		return nil, err
	}

	if err = c.checkControl(header); err != nil {
		return nil, err
	}

	if header.Flags&FlagPSH == 0 {
		return nil, nil
	}

	return data, nil
}

func (c *Conn) checkControl(header *Header) error {
	if header.Flags&FlagRST != 0 {
		c.closeOnce.Do(func() {
			c.closed()
		})
		return io.EOF
	}

	return nil
}

func (c *Conn) closed() {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	go func() {
		// If closing a kcp connection, any pending writes will be canceled without being flushed.
		time.Sleep(time.Millisecond * 500)
		_ = c.parent.Close()
	}()
	c.setState(StateClosed)
	c.cancelFunc()
}

func (c *Conn) reset() error {
	err := c.writeSegment(&Header{
		Version: Version,
		Flags:   FlagRST,
	}, nil)

	c.closeOnce.Do(func() {
		c.closed()
	})

	return err
}

// startKeepalive starts the keepalive routine
func (c *Conn) startKeepalive() {
	if c.config.KeepaliveInterval <= 0 {
		return
	}

	c.lastRecv = time.Now()
	c.keepaliveTimer = time.NewTimer(c.config.KeepaliveInterval)

	go func() {
		for {
			select {
			case <-c.keepaliveTimer.C:
				if time.Since(c.lastRecv) > c.config.KeepaliveTimeout {
					_ = c.reset() // Connection is dead
					return
				}

				// Send keepalive packet
				if err := c.writeSegment(&Header{
					Version: Version,
					Flags:   0, // No flags for keepalive
				}, nil); err != nil {
					_ = c.reset()
					return
				}

				c.keepaliveTimer.Reset(c.config.KeepaliveInterval)

			case <-c.ctx.Done():
				c.keepaliveTimer.Stop()
				return
			}
		}
	}()
}

// updateLastRecv updates the last receive time
func (c *Conn) updateLastRecv() {
	now := time.Now()
	c.mutex.Lock()
	c.lastRecv = now
	c.mutex.Unlock()
}

func (c *Conn) Close() error {
	return c.reset()
}

func (c *Conn) LocalAddr() net.Addr { return c.parent.LocalAddr() }

func (c *Conn) RemoteAddr() net.Addr { return c.parent.RemoteAddr() }

func (c *Conn) SetDeadline(t time.Time) error {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.readDeadline = t
	c.writeDeadline = t
	return nil
}

func (c *Conn) SetReadDeadline(t time.Time) error {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.readDeadline = t
	return nil
}

func (c *Conn) SetWriteDeadline(t time.Time) error {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.writeDeadline = t
	return nil
}
