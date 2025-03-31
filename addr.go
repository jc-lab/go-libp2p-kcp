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
	"fmt"
	ma "github.com/multiformats/go-multiaddr"
	mafmt "github.com/multiformats/go-multiaddr-fmt"
	manet "github.com/multiformats/go-multiaddr/net"
	"net"
)

const P_KCP_SCOP = 0x80a1

var dialMatcher = mafmt.And(
	mafmt.Or(mafmt.IP, mafmt.DNS),
	mafmt.Base(ma.P_UDP),
	mafmt.Base(P_KCP_SCOP),
)

var protoKCP = ma.Protocol{
	Name:  "kcp+scop",
	Code:  P_KCP_SCOP,
	VCode: ma.CodeToVarint(P_KCP_SCOP),
}

var kcpComponent *ma.Component

func init() {
	var err error
	manet.RegisterToNetAddr(parseBasicNetMaddr, "kcp+scop")
	if err = ma.AddProtocol(protoKCP); err != nil {
		panic(err)
	}
	kcpComponent, err = ma.NewComponent("kcp+scop", "")
	if err != nil {
		panic(err)
	}
}

func parseBasicNetMaddr(maddr ma.Multiaddr) (net.Addr, error) {
	network, host, err := manet.DialArgs(maddr)
	if err != nil {
		return nil, err
	}

	switch network {
	case "tcp", "tcp4", "tcp6":
		return net.ResolveTCPAddr(network, host)
	case "udp", "udp4", "udp6":
		return net.ResolveUDPAddr(network, host)
	case "ip", "ip4", "ip6":
		return net.ResolveIPAddr(network, host)
	case "unix":
		return net.ResolveUnixAddr(network, host)
	}

	return nil, fmt.Errorf("network not supported: %s", network)
}

func ParseKcpNetAddr(a net.Addr) (ma.Multiaddr, error) {
	maddr, err := manet.FromNetAddr(a)
	if err != nil {
		return nil, err
	}
	return maddr.AppendComponent(kcpComponent), nil
}
