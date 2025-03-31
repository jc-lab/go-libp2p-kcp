// Copyright 2025 JC-Lab.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package kcp

import (
	"context"
	"github.com/jc-lab/go-libp2p-kcp/scop"
	"github.com/libp2p/go-libp2p/core/pnet"
	"github.com/libp2p/go-libp2p/core/transport"
	ma "github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
	"github.com/pkg/errors"
	kcpgo "github.com/xtaci/kcp-go"
	"github.com/xtaci/kcp-go/v5"
	"net"
)

type listener struct {
	laddr       ma.Multiaddr
	psk         pnet.PSK
	kcpListener net.Listener
}

func (l *listener) start(laddr ma.Multiaddr) error {
	var block kcp.BlockCrypt
	if l.psk != nil {
		block, _ = kcp.NewAESBlockCrypt(l.psk)
	}
	network, lnaddr, err := manet.DialArgs(laddr)
	if err != nil {
		return errors.WithStack(err)
	}
	udpaddr, err := net.ResolveUDPAddr(network, lnaddr)
	if err != nil {
		return errors.WithStack(err)
	}
	conn, err := net.ListenUDP(network, udpaddr)
	if err != nil {
		return errors.WithStack(err)
	}
	kcpListener, err := kcpgo.ServeConn(block, 10, 3, conn)
	if err != nil {
		return err
	}
	l.kcpListener = kcpListener
	l.laddr, err = ParseKcpNetAddr(kcpListener.Addr())
	if err != nil {
		return err
	}
	return nil
}

func (l *listener) clientNegotiation(ctx context.Context, conn net.Conn) (manet.Conn, error) {
	stream, err := scop.ServerWithContext(ctx, conn)
	if err != nil {
		log.Errorf("scop server error: %+v", err)
		return nil, err
	}
	maConn, err := manet.WrapNetConn(stream)
	if err != nil {
		log.Errorf("manet.WrapNetConn error: %+v", err)
		return nil, err
	}
	return maConn, nil
}

func (l *listener) Accept() (manet.Conn, error) {
	for {
		netConn, err := l.kcpListener.Accept()
		if err != nil {
			return nil, err
		}
		maConn, err := l.clientNegotiation(context.Background(), netConn)
		if err != nil {
			log.Warnf("client[%s] negotiation failed: %+v", netConn.RemoteAddr().String(), err)
			continue
		}
		return maConn, nil
	}
}

func (l *listener) Close() error {
	return l.kcpListener.Close()
}

func (l *listener) Multiaddr() ma.Multiaddr {
	return l.laddr
}

func (l *listener) Addr() net.Addr {
	return l.kcpListener.Addr()
}

type transportListener struct {
	transport.Listener
}

func (l *transportListener) Accept() (transport.CapableConn, error) {
	conn, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return &capableConn{CapableConn: conn}, nil
}
