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
	pool "github.com/libp2p/go-buffer-pool"
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

const MaxTransportMsgLength = 0xffff // kcpgo.IKCP_MTU_DEF - kcpgo.IKCP_OVERHEAD - headerSize

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
	readLock  sync.Mutex
	writeLock sync.Mutex

	ctx        context.Context
	cancelFunc context.CancelFunc

	qbuf  []byte
	qseek int
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
	var header Header
	header.Version = Version
	header.Flags = FlagSYN

	rto := c.config.InitialRTO
	for i := 0; i < c.config.MaxRetries; i++ {
		if err := c.writeSegment(&header, nil); err != nil {
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
	header := &Header{}

	segBuf := pool.Get(headerSize)
	defer pool.Put(segBuf)

	n, err := io.ReadFull(c.parent, segBuf)
	if err != nil {
		return nil, nil, err
	}

	if err := header.Unpack(segBuf[:n]); err != nil {
		return nil, nil, err
	}

	var data []byte
	if header.DataSize > 0 {
		data = pool.Get(int(header.DataSize))
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
	header, data, err := c.waitSegment()
	if err != nil {
		return nil, err
	}
	if data != nil {
		pool.Put(data)
	}

	if (header.Flags&FlagACK) == 0 || header.DataSize != 0 {
		return nil, ErrInvalidPacket
	}

	return header, nil
}

func (c *Conn) writeSegment(header *Header, payload []byte) error {
	segBuf := pool.Get(headerSize + MaxTransportMsgLength)
	defer pool.Put(segBuf)
	n := header.PackTo(segBuf[:headerSize])
	n += copy(segBuf[n:], payload)
	_ = c.parent.SetWriteDeadline(time.Now().Add(time.Second * 3))
	defer c.parent.SetWriteDeadline(time.Time{})
	_, err := c.parent.Write(segBuf[:n])
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

func (c *Conn) Write(data []byte) (n int, err error) {
	var header Header

	c.writeLock.Lock()
	defer c.writeLock.Unlock()

	if c.getState() != StateEstablished {
		return 0, ErrInvalidState
	}

	header.Version = Version
	header.Flags = FlagPSH

	total := len(data)
	written := 0
	for written < total {
		end := written + MaxTransportMsgLength
		if end > total {
			end = total
		}

		chunk := data[written:end]

		header.DataSize = uint16(len(chunk))
		err = c.writeSegment(&header, chunk)
		if err != nil {
			return written, err
		}

		written = end
	}

	return written, nil
}

func (c *Conn) Read(b []byte) (n int, err error) {
	c.readLock.Lock()
	defer c.readLock.Unlock()

	if len(c.qbuf) > 0 {
		copied := copy(b, c.qbuf[c.qseek:])
		c.qseek += copied
		c.qbufClearIfNeeded()
		return copied, nil
	}

	for c.qbuf == nil {
		c.qbuf, err = c.readData()
		if err != nil {
			return 0, err
		}
	}
	copied := copy(b, c.qbuf)
	c.qseek = copied
	c.qbufClearIfNeeded()
	return copied, nil
}

func (c *Conn) qbufClearIfNeeded() {
	if c.qseek == len(c.qbuf) {
		pool.Put(c.qbuf)
		c.qseek, c.qbuf = 0, nil
	}
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
		if data != nil {
			pool.Put(data)
		}
		return nil, err
	}

	if header.Flags&FlagPSH == 0 {
		if data != nil {
			pool.Put(data)
		}
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
	var header Header
	header.Version = Version
	header.Flags = FlagRST
	err := c.writeSegment(&header, nil)

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
		var header Header
		header.Version = Version
		header.Flags = 0 // No flags for keepalive

		for {
			select {
			case <-c.keepaliveTimer.C:
				if time.Since(c.lastRecv) > c.config.KeepaliveTimeout {
					_ = c.reset() // Connection is dead
					return
				}

				// Send keepalive packet
				if err := c.writeSegment(&header, nil); err != nil {
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
