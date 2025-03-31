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

package kcp

import (
	"context"
	logging "github.com/ipfs/go-log/v2"
	"github.com/jc-lab/go-libp2p-kcp/scop"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/pnet"
	"github.com/libp2p/go-libp2p/core/transport"
	ma "github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
	kcpgo "github.com/xtaci/kcp-go"
	"github.com/xtaci/kcp-go/v5"
)

var log = logging.Logger("kcp-transport")

type KcpTransport struct {
	upgrader transport.Upgrader
	rcmgr    network.ResourceManager
	psk      pnet.PSK
	opts     []scop.Option
}

var _ transport.Transport = (*KcpTransport)(nil)

func NewTransport(upgrader transport.Upgrader, rcmgr network.ResourceManager, psk pnet.PSK, opts ...scop.Option) (*KcpTransport, error) {
	if rcmgr == nil {
		rcmgr = &network.NullResourceManager{}
	}
	return &KcpTransport{
		upgrader: upgrader,
		rcmgr:    rcmgr,
		psk:      psk,
		opts:     opts,
	}, nil
}

func (t *KcpTransport) Dial(ctx context.Context, raddr ma.Multiaddr, p peer.ID) (transport.CapableConn, error) {
	connScope, err := t.rcmgr.OpenConnection(network.DirOutbound, true, raddr)
	if err != nil {
		return nil, err
	}
	c, err := t.dialWithScope(ctx, raddr, p, connScope)
	if err != nil {
		connScope.Done()
		return nil, err
	}
	return c, nil
}

func (t *KcpTransport) dialWithScope(ctx context.Context, raddr ma.Multiaddr, p peer.ID, connScope network.ConnManagementScope) (transport.CapableConn, error) {
	macon, err := t.maDial(ctx, raddr)
	if err != nil {
		return nil, err
	}
	conn, err := t.upgrader.Upgrade(ctx, t, macon, network.DirOutbound, p, connScope)
	if err != nil {
		return nil, err
	}
	return &capableConn{CapableConn: conn}, nil
}

func (t *KcpTransport) maDial(ctx context.Context, raddr ma.Multiaddr) (manet.Conn, error) {
	_, host, err := manet.DialArgs(raddr)
	if err != nil {
		return nil, err
	}
	var block kcp.BlockCrypt
	if t.psk != nil {
		block, _ = kcp.NewAESBlockCrypt(t.psk)
	}
	c, err := kcpgo.DialWithOptions(host, block, 0, 0)
	if err != nil {
		return nil, err
	}
	stream, err := scop.ClientWithContext(ctx, c)
	if err != nil {
		return nil, err
	}

	mnc, err := manet.WrapNetConn(stream)
	if err != nil {
		_ = stream.Close()
		return nil, err
	}

	return mnc, nil
}

func (t *KcpTransport) CanDial(addr ma.Multiaddr) bool {
	return dialMatcher.Matches(addr)
}

func (t *KcpTransport) maListen(laddr ma.Multiaddr) (manet.Listener, error) {
	l := &listener{
		psk: t.psk,
	}
	if err := l.start(laddr); err != nil {
		return nil, err
	}
	return l, nil
}

func (t *KcpTransport) Listen(a ma.Multiaddr) (transport.Listener, error) {
	malist, err := t.maListen(a)
	if err != nil {
		return nil, err
	}
	return &transportListener{Listener: t.upgrader.UpgradeListener(t, malist)}, nil
}

func (t *KcpTransport) Protocols() []int {
	return []int{P_KCP_SCOP}
}

func (t *KcpTransport) Proxy() bool {
	return false
}

type capableConn struct {
	transport.CapableConn
}

func (c *capableConn) ConnState() network.ConnectionState {
	cs := c.CapableConn.ConnState()
	cs.Transport = "kcp"
	return cs
}
