// Package media encodes the game's frames to H.264 and its sound to Opus,
// for WebRTC, through FFmpeg's libraries: on the GPU with VAAPI or NVENC,
// or on the CPU with x264.
package media

/*
#cgo pkg-config: libavcodec libavutil libswscale libswresample
#include <stdlib.h>
#include "encode.h"
*/
import "C"

import (
	"errors"
	"unsafe"
)

func errText(buf []byte) error {
	n := 0
	for n < len(buf) && buf[n] != 0 {
		n++
	}
	return errors.New(string(buf[:n]))
}

// Video is an H.264 encoder for one frame size.
type Video struct {
	e             *C.venc
	Width, Height int
}

// VideoConfig chooses the encoder.
type VideoConfig struct {
	// Kind is "vaapi", "nvenc" or "x264".
	Kind string
	// Device is VAAPI's render node.
	Device        string
	Width, Height int
	FPS           int
	Kbps          int
}

// NewVideo opens an encoder.
func NewVideo(c VideoConfig) (*Video, error) {
	kind, dev := C.CString(c.Kind), C.CString(c.Device)
	defer C.free(unsafe.Pointer(kind))
	defer C.free(unsafe.Pointer(dev))
	buf := make([]byte, 256)
	e := C.venc_open(kind, dev, C.int(c.Width), C.int(c.Height), C.int(c.FPS), C.int(c.Kbps),
		(*C.char)(unsafe.Pointer(&buf[0])), C.int(len(buf)))
	if e == nil {
		return nil, errText(buf)
	}
	return &Video{e: e, Width: c.Width, Height: c.Height}, nil
}

// Encode encodes a frame of the encoder's size, in one of retro's pixel
// formats. The result is Annex B, valid until the next call; it may be
// empty while the encoder holds frames back.
func (v *Video) Encode(pix []byte, format, stride int, flip, keyframe bool) ([]byte, error) {
	if len(pix) < stride*v.Height {
		return nil, errors.New("short frame")
	}
	var out *C.uint8_t
	n := C.venc_encode(v.e, (*C.uint8_t)(unsafe.Pointer(&pix[0])), C.int(format), C.int(stride),
		cbool(flip), cbool(keyframe), &out)
	if n < 0 {
		return nil, errors.New("the video encoder failed")
	}
	if n == 0 {
		return nil, nil
	}
	return unsafe.Slice((*byte)(unsafe.Pointer(out)), int(n)), nil
}

// Close frees the encoder.
func (v *Video) Close() {
	if v.e != nil {
		C.venc_close(v.e)
		v.e = nil
	}
}

// Audio is an Opus encoder: 48 kHz stereo, in 20 ms packets.
type Audio struct{ e *C.aenc }

// NewAudio opens an encoder for stereo 16-bit sound at rate.
func NewAudio(rate, kbps int) (*Audio, error) {
	buf := make([]byte, 256)
	e := C.aenc_open(C.int(rate), C.int(kbps), (*C.char)(unsafe.Pointer(&buf[0])), C.int(len(buf)))
	if e == nil {
		return nil, errText(buf)
	}
	return &Audio{e: e}, nil
}

// Push adds interleaved stereo samples.
func (a *Audio) Push(samples []int16) error {
	if len(samples) < 2 {
		return nil
	}
	if C.aenc_push(a.e, (*C.int16_t)(unsafe.Pointer(&samples[0])), C.int(len(samples)/2)) < 0 {
		return errors.New("the audio encoder failed")
	}
	return nil
}

// Next is the next finished packet, valid until the next call; nil when
// there's none.
func (a *Audio) Next() ([]byte, error) {
	var out *C.uint8_t
	n := C.aenc_next(a.e, &out)
	if n < 0 {
		return nil, errors.New("the audio encoder failed")
	}
	if n == 0 {
		return nil, nil
	}
	return unsafe.Slice((*byte)(unsafe.Pointer(out)), int(n)), nil
}

// Close frees the encoder.
func (a *Audio) Close() {
	if a.e != nil {
		C.aenc_close(a.e)
		a.e = nil
	}
}

func cbool(b bool) C.int {
	if b {
		return 1
	}
	return 0
}
