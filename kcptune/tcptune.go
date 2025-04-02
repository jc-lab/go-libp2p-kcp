package kcptune

import kcpgo "github.com/xtaci/kcp-go/v5"

func Default(s *kcpgo.UDPSession) {
	s.SetWindowSize(128, 512)
	s.SetNoDelay(1, 10, 2, 1)
}
