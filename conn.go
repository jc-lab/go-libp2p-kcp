package kcp

import (
	ma "github.com/multiformats/go-multiaddr"
	"net"
)

type Conn struct {
	net.Conn
	laddr ma.Multiaddr
	raddr ma.Multiaddr
}

func (c *Conn) LocalMultiaddr() ma.Multiaddr {
	return c.laddr
}

func (c *Conn) RemoteMultiaddr() ma.Multiaddr {
	return c.raddr
}

func wrapNetConn(conn net.Conn) (*Conn, error) {
	raddr, err := ParseKcpNetAddr(conn.RemoteAddr())
	if err != nil {
		return nil, err
	}
	laddr, err := ParseKcpNetAddr(conn.LocalAddr())
	if err != nil {
		return nil, err
	}
	return &Conn{
		Conn:  conn,
		raddr: raddr,
		laddr: laddr,
	}, nil
}
