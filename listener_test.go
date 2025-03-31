package kcp

import (
	"github.com/multiformats/go-multiaddr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

func TestListenIp4(t *testing.T) {
	l := &listener{}
	err := l.start(multiaddr.StringCast("/ip4/0.0.0.0/udp/0/kcp+scop"))
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(l.Multiaddr().String(), "/ip4/"), l.Multiaddr().String())
}

func TestListenIp6(t *testing.T) {
	l := &listener{}
	err := l.start(multiaddr.StringCast("/ip6/::/udp/0/kcp+scop"))
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(l.Multiaddr().String(), "/ip6/"), l.Multiaddr().String())
}
