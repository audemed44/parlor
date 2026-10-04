package stream

import (
	"encoding/binary"
	"errors"

	"github.com/audemed44/parlor/internal/stream/retro"
)

// The browser sends what's held as a 16-byte message on the "input"
// channel, every change and a few times a second besides:
//
//	0  uint16 buttons, libretro's joypad IDs as bits
//	2  int16  left stick x, y; right stick x, y (-32767..32767)
//	10 uint8  1 while the screen is touched
//	11 uint8  unused
//	12 uint16 touch x, y across the whole frame (0..65535)
//
// little-endian.
const inputSize = 16

// DecodeInput reads one input message.
func DecodeInput(b []byte) (retro.Input, error) {
	if len(b) != inputSize {
		return retro.Input{}, errors.New("bad input message")
	}
	le := binary.LittleEndian
	pointer := func(v uint16) int16 { return int16(int64(v)*65534/65535 - 32767) }
	return retro.Input{
		Buttons:  le.Uint16(b[0:]),
		LX:       int16(le.Uint16(b[2:])),
		LY:       int16(le.Uint16(b[4:])),
		RX:       int16(le.Uint16(b[6:])),
		RY:       int16(le.Uint16(b[8:])),
		Touching: b[10] != 0,
		TX:       pointer(le.Uint16(b[12:])),
		TY:       pointer(le.Uint16(b[14:])),
	}, nil
}

// withStick tilts the left stick with the D-pad, unless the stick is
// already being used.
func withStick(in retro.Input) retro.Input {
	if in.LX != 0 || in.LY != 0 {
		return in
	}
	held := func(b int) bool { return in.Buttons&(1<<b) != 0 }
	switch {
	case held(retro.Left):
		in.LX = -32767
	case held(retro.Right):
		in.LX = 32767
	}
	switch {
	case held(retro.Up):
		in.LY = -32767
	case held(retro.Down):
		in.LY = 32767
	}
	return in
}
