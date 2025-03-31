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
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/sec"
	"github.com/libp2p/go-libp2p/core/sec/insecure"
	"github.com/libp2p/go-libp2p/p2p/muxer/yamux"
	tptu "github.com/libp2p/go-libp2p/p2p/net/upgrader"
	ttransport "github.com/libp2p/go-libp2p/p2p/transport/testsuite"
	"github.com/stretchr/testify/require"
	"testing"
)

var muxers = []tptu.StreamMuxer{{ID: "/yamux", Muxer: yamux.DefaultTransport}}

func TestTcpTransport(t *testing.T) {
	peerA, ia := makeInsecureMuxer(t)
	_, ib := makeInsecureMuxer(t)

	ua, err := tptu.New(ia, muxers, nil, nil, nil)
	require.NoError(t, err)
	ta, err := NewTransport(ua, nil, nil)
	require.NoError(t, err)
	ub, err := tptu.New(ib, muxers, nil, nil, nil)
	require.NoError(t, err)
	tb, err := NewTransport(ub, nil, nil)
	require.NoError(t, err)

	zero := "/ip4/127.0.0.1/udp/0/kcp+scop"
	ttransport.SubtestTransport(t, ta, tb, zero, peerA)
}

func makeInsecureMuxer(t *testing.T) (peer.ID, []sec.SecureTransport) {
	t.Helper()
	priv, _, err := crypto.GenerateKeyPair(crypto.Ed25519, 256)
	require.NoError(t, err)
	id, err := peer.IDFromPrivateKey(priv)
	require.NoError(t, err)
	return id, []sec.SecureTransport{insecure.NewWithIdentity(insecure.ID, id, priv)}
}
