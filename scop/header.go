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
	"encoding/binary"
	"io"
)

const (
	Version = 0x01
)

const (
	FlagSYN uint8 = 1 << iota
	FlagACK
	FlagFIN // TODO
	FlagRST
	FlagPSH
)

const headerSize = 4

type Header struct {
	Version  uint8
	Flags    uint8
	DataSize uint16
}

func (h *Header) PackTo(buf []byte) int {
	buf[0] = h.Version
	buf[1] = h.Flags
	binary.BigEndian.PutUint16(buf[2:], h.DataSize)
	return headerSize
}

func (h *Header) Unpack(data []byte) error {
	if len(data) < 4 {
		return io.ErrShortBuffer
	}
	h.Version = data[0]
	h.Flags = data[1]
	h.DataSize = binary.BigEndian.Uint16(data[2:])
	return nil
}
