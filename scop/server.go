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
	"net"
)

func Server(parent net.Conn, opts ...Option) (*Conn, error) {
	return ServerWithContext(context.Background(), parent, opts...)
}

func ServerWithContext(parentCtx context.Context, parent net.Conn, opts ...Option) (*Conn, error) {
	config := DefaultConfig()
	for _, opt := range opts {
		opt(config)
	}

	ctx, cancel := context.WithCancel(parentCtx)
	conn := &Conn{
		parent:     parent,
		config:     config,
		ctx:        ctx,
		cancelFunc: cancel,
	}
	conn.setState(StateClosed)

	readHeader, _, err := conn.waitSegment()
	if err != nil {
		_ = parent.Close()
		return nil, err
	}
	if readHeader.Flags != FlagSYN {
		conn.reset()
		return nil, ErrInvalidPacket
	}

	if err := conn.writeSegment(&Header{
		Version: Version,
		Flags:   FlagSYN | FlagACK,
	}, nil); err != nil {
		conn.closeOnce.Do(func() {
			conn.closed()
		})
		return nil, err
	}

	conn.setState(StateEstablished)
	conn.startKeepalive()
	return conn, nil
}
