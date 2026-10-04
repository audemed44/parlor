// Package retro runs a libretro core headless: one core and one game per
// process (cores keep global state, so a second game gets a new process).
// Every call must come from the same OS thread, the one holding the core's
// OpenGL context: lock it with runtime.LockOSThread.
package retro

/*
#cgo pkg-config: egl gbm opengl
#cgo LDFLAGS: -ldl
#include <stdlib.h>
#include "host.h"
*/
import "C"

import (
	"errors"
	"sort"
	"unsafe"
)

// Pixel formats of a Frame.
const (
	RGBA     = C.HOST_RGBA
	XRGB8888 = C.HOST_XRGB8888
	RGB565   = C.HOST_RGB565
	RGB1555  = C.HOST_0RGB1555
	// NV12 is OpenGL cores' frames, converted on the GPU: the Y plane, then
	// U and V interleaved at half size; Stride is the width.
	NV12 = C.HOST_NV12
)

// Libretro's joypad buttons, as bit numbers in Input.Buttons.
const (
	B = iota
	Y
	Select
	Start
	Up
	Down
	Left
	Right
	A
	X
	L
	R
	L2
	R2
	L3
	R3
)

// Config is the core and where it keeps its files.
type Config struct {
	Core      string
	SystemDir string
	SaveDir   string
	// RenderNode is the GPU for cores that draw with OpenGL, like
	// /dev/dri/renderD128; "" for none.
	RenderNode string
	Options    map[string]string
}

// AV is the game's timing and frame size.
type AV struct {
	FPS        float64
	SampleRate float64
	Width      int
	Height     int
}

// Frame is the last frame the core drew. Pix is only valid until the
// next Run.
type Frame struct {
	Pix           []byte
	Width, Height int
	Stride        int
	Format        int
	// Flip is set when the rows run bottom-up (OpenGL's).
	Flip bool
}

// Input is everything held on the controller.
type Input struct {
	Buttons uint16
	// The analog sticks, -32767..32767.
	LX, LY, RX, RY int16
	Touching       bool
	// The pointer, -32767..32767 across the whole frame.
	TX, TY int16
}

var opened bool

func errText(buf []byte) error {
	n := 0
	for n < len(buf) && buf[n] != 0 {
		n++
	}
	return errors.New(string(buf[:n]))
}

// Open loads a core. A process can only open one.
func Open(c Config) error {
	if opened {
		return errors.New("a core is already open")
	}
	opened = true
	keys := make([]string, 0, len(c.Options))
	for k := range c.Options {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	opts := make([]*C.char, 0, 2*len(keys))
	for _, k := range keys {
		opts = append(opts, C.CString(k), C.CString(c.Options[k]))
	}
	defer func() {
		for _, p := range opts {
			C.free(unsafe.Pointer(p))
		}
	}()
	var optp **C.char
	if len(opts) > 0 {
		optp = (**C.char)(C.malloc(C.size_t(len(opts)) * C.size_t(unsafe.Sizeof(uintptr(0)))))
		defer C.free(unsafe.Pointer(optp))
		copy(unsafe.Slice(optp, len(opts)), opts)
	}
	core, sys, save, node := C.CString(c.Core), C.CString(c.SystemDir), C.CString(c.SaveDir), C.CString(c.RenderNode)
	defer C.free(unsafe.Pointer(core))
	defer C.free(unsafe.Pointer(sys))
	defer C.free(unsafe.Pointer(save))
	defer C.free(unsafe.Pointer(node))
	buf := make([]byte, 512)
	if C.host_open(core, sys, save, node, optp, C.int(len(opts)), (*C.char)(unsafe.Pointer(&buf[0])), C.int(len(buf))) != 0 {
		return errText(buf)
	}
	return nil
}

// Load loads the game.
func Load(rom string) error {
	p := C.CString(rom)
	defer C.free(unsafe.Pointer(p))
	buf := make([]byte, 512)
	if C.host_load(p, (*C.char)(unsafe.Pointer(&buf[0])), C.int(len(buf))) != 0 {
		return errText(buf)
	}
	return nil
}

// Info is the game's timing and frame size, which can change as it runs.
func Info() AV {
	var fps, rate C.double
	var w, h C.unsigned
	C.host_av(&fps, &rate, &w, &h)
	return AV{FPS: float64(fps), SampleRate: float64(rate), Width: int(w), Height: int(h)}
}

// Run emulates a frame; true when the core drew a new one.
func Run() bool { return C.host_run() != 0 }

// LastFrame is the last frame drawn; its Pix is nil before the first.
func LastFrame() Frame {
	var w, h, stride C.unsigned
	var format, flip C.int
	p := C.host_frame(&w, &h, &stride, &format, &flip)
	if p == nil || w == 0 {
		return Frame{}
	}
	size := int(stride) * int(h)
	if format == C.HOST_NV12 {
		size = size * 3 / 2
	}
	return Frame{
		Pix:   unsafe.Slice((*byte)(unsafe.Pointer(p)), size),
		Width: int(w), Height: int(h), Stride: int(stride), Format: int(format), Flip: flip != 0,
	}
}

// Audio is the sound made since ClearAudio: interleaved
// stereo samples. It's only valid until the next Run.
func Audio() []int16 {
	var n C.size_t
	p := C.host_audio(&n)
	if p == nil || n == 0 {
		return nil
	}
	return unsafe.Slice((*int16)(unsafe.Pointer(p)), int(n)*2)
}

// ClearAudio forgets the sound made so far.
func ClearAudio() { C.host_audio_clear() }

// SetInput sets what's held, for the next frames.
func SetInput(in Input) {
	touching := 0
	if in.Touching {
		touching = 1
	}
	C.host_input(C.uint32_t(in.Buttons), C.int16_t(in.LX), C.int16_t(in.LY), C.int16_t(in.RX), C.int16_t(in.RY),
		C.int(touching), C.int16_t(in.TX), C.int16_t(in.TY))
}

// SetOption changes a core option; the core picks it up on its next frame.
func SetOption(key, value string) {
	k, v := C.CString(key), C.CString(value)
	defer C.free(unsafe.Pointer(k))
	defer C.free(unsafe.Pointer(v))
	C.host_set_option(k, v)
}

// SetMaxHeight scales OpenGL frames taller than h down to it (0: never),
// so a game can render at a higher resolution than it's streamed at.
func SetMaxHeight(h int) { C.host_set_max_height(C.unsigned(max(h, 0))) }

// Serialize is a save state of the running game.
func Serialize() ([]byte, error) {
	n := C.host_state_size()
	if n == 0 {
		return nil, errors.New("the core can't take save states")
	}
	buf := make([]byte, int(n))
	if C.host_serialize(unsafe.Pointer(&buf[0]), n) != 0 {
		return nil, errors.New("the core couldn't take a save state")
	}
	return buf, nil
}

// Unserialize loads a save state.
func Unserialize(data []byte) error {
	if len(data) == 0 || C.host_unserialize(unsafe.Pointer(&data[0]), C.size_t(len(data))) != 0 {
		return errors.New("the core couldn't load the save state")
	}
	return nil
}

// Reset restarts the game.
func Reset() { C.host_reset() }

// Close unloads the game and the core.
func Close() { C.host_close() }
