// Package patch applies ROM hack patches: IPS, UPS and BPS. UPS and BPS
// carry checksums of the ROM they apply to and the ROM they make, so a
// patch applied to the wrong base ROM is caught; IPS has none.
package patch

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
)

// MaxSize is the largest ROM a patch may make: a GBA ROM is at most 32 MiB.
const MaxSize = 32 << 20

// Info describes a patch.
type Info struct {
	Format string // "ips", "ups" or "bps"
	// SourceSize and SourceCRC identify the ROM a UPS or BPS patch applies
	// to; zero for IPS.
	SourceSize int64
	SourceCRC  uint32
}

// Checked is whether the patch names the ROM it applies to.
func (i Info) Checked() bool { return i.Format != "ips" }

var (
	ErrFormat   = errors.New("not an IPS, UPS or BPS patch")
	ErrCorrupt  = errors.New("the patch file is damaged")
	ErrWrongROM = errors.New("this patch is for a different ROM")
	ErrTooLarge = fmt.Errorf("the patched ROM would be larger than %d MiB", MaxSize>>20)
)

// Inspect reads a patch's format and, for UPS and BPS, which ROM it's for.
func Inspect(p []byte) (Info, error) {
	switch {
	case bytes.HasPrefix(p, []byte("PATCH")):
		return Info{Format: "ips"}, nil
	case bytes.HasPrefix(p, []byte("UPS1")), bytes.HasPrefix(p, []byte("BPS1")):
		if len(p) < 4+12 {
			return Info{}, ErrCorrupt
		}
		r := &reader{p: p[:len(p)-12], i: 4}
		size, err := r.number()
		if err != nil {
			return Info{}, err
		}
		format := "ups"
		if p[0] == 'B' {
			format = "bps"
		}
		return Info{Format: format, SourceSize: int64(size), SourceCRC: binary.LittleEndian.Uint32(p[len(p)-12:])}, nil
	}
	return Info{}, ErrFormat
}

// Apply patches a ROM, returning the new one; the source isn't changed.
func Apply(source, p []byte) ([]byte, error) {
	info, err := Inspect(p)
	if err != nil {
		return nil, err
	}
	if info.Format == "ips" {
		return ips(source, p)
	}
	// The footer: source, target and patch checksums.
	foot := p[len(p)-12:]
	if crc32.ChecksumIEEE(p[:len(p)-4]) != binary.LittleEndian.Uint32(foot[8:]) {
		return nil, ErrCorrupt
	}
	if int64(len(source)) != info.SourceSize || crc32.ChecksumIEEE(source) != info.SourceCRC {
		return nil, ErrWrongROM
	}
	var out []byte
	if info.Format == "ups" {
		out, err = ups(source, p)
	} else {
		out, err = bps(source, p)
	}
	if err != nil {
		return nil, err
	}
	if crc32.ChecksumIEEE(out) != binary.LittleEndian.Uint32(foot[4:]) {
		return nil, ErrCorrupt
	}
	return out, nil
}

type reader struct {
	p []byte
	i int
}

func (r *reader) byte() (byte, error) {
	if r.i >= len(r.p) {
		return 0, ErrCorrupt
	}
	b := r.p[r.i]
	r.i++
	return b, nil
}

func (r *reader) bytes(n int) ([]byte, error) {
	if n < 0 || r.i+n > len(r.p) {
		return nil, ErrCorrupt
	}
	b := r.p[r.i : r.i+n]
	r.i += n
	return b, nil
}

// number reads UPS and BPS's variable-length numbers.
func (r *reader) number() (uint64, error) {
	var data, shift uint64 = 0, 1
	for range 10 {
		x, err := r.byte()
		if err != nil {
			return 0, err
		}
		data += uint64(x&0x7f) * shift
		if x&0x80 != 0 {
			return data, nil
		}
		shift <<= 7
		data += shift
	}
	return 0, ErrCorrupt
}

func ips(source, p []byte) ([]byte, error) {
	out := bytes.Clone(source)
	r := &reader{p: p, i: 5}
	grow := func(end int) error {
		if end > MaxSize {
			return ErrTooLarge
		}
		if end > len(out) {
			out = append(out, make([]byte, end-len(out))...)
		}
		return nil
	}
	for {
		head, err := r.bytes(3)
		if err != nil {
			return nil, err
		}
		if string(head) == "EOF" {
			break
		}
		offset := int(head[0])<<16 | int(head[1])<<8 | int(head[2])
		n, err := r.bytes(2)
		if err != nil {
			return nil, err
		}
		size := int(n[0])<<8 | int(n[1])
		if size > 0 {
			data, err := r.bytes(size)
			if err != nil {
				return nil, err
			}
			if err = grow(offset + size); err != nil {
				return nil, err
			}
			copy(out[offset:], data)
			continue
		}
		// A run of one byte.
		rle, err := r.bytes(3)
		if err != nil {
			return nil, err
		}
		size = int(rle[0])<<8 | int(rle[1])
		if err = grow(offset + size); err != nil {
			return nil, err
		}
		for i := range size {
			out[offset+i] = rle[2]
		}
	}
	// Some IPS patches end with the size to cut the ROM to.
	if cut, err := r.bytes(3); err == nil {
		size := int(cut[0])<<16 | int(cut[1])<<8 | int(cut[2])
		if size < len(out) {
			out = out[:size]
		}
	}
	return out, nil
}

func ups(source, p []byte) ([]byte, error) {
	r := &reader{p: p[:len(p)-12], i: 4}
	if _, err := r.number(); err != nil {
		return nil, err
	}
	size, err := r.number()
	if err != nil {
		return nil, err
	}
	if size > MaxSize {
		return nil, ErrTooLarge
	}
	out := make([]byte, size)
	copy(out, source)
	at := uint64(0)
	for r.i < len(r.p) {
		skip, err := r.number()
		if err != nil {
			return nil, err
		}
		at += skip
		// XOR bytes up to and including a zero.
		for {
			x, err := r.byte()
			if err != nil {
				return nil, err
			}
			if at < size {
				var s byte
				if at < uint64(len(source)) {
					s = source[at]
				}
				out[at] = s ^ x
			}
			at++
			if x == 0 {
				break
			}
		}
	}
	return out, nil
}

func bps(source, p []byte) ([]byte, error) {
	r := &reader{p: p[:len(p)-12], i: 4}
	if _, err := r.number(); err != nil {
		return nil, err
	}
	size, err := r.number()
	if err != nil {
		return nil, err
	}
	if size > MaxSize {
		return nil, ErrTooLarge
	}
	meta, err := r.number()
	if err != nil {
		return nil, err
	}
	if _, err = r.bytes(int(min(meta, uint64(len(p))+1))); err != nil {
		return nil, err
	}
	out := make([]byte, 0, size)
	var src, dst int64
	for r.i < len(r.p) {
		data, err := r.number()
		if err != nil {
			return nil, err
		}
		n := int(data>>2) + 1
		if uint64(len(out))+uint64(n) > size {
			return nil, ErrCorrupt
		}
		switch data & 3 {
		case 0: // source read: the same bytes as the source, in place
			at := len(out)
			if at+n > len(source) {
				return nil, ErrCorrupt
			}
			out = append(out, source[at:at+n]...)
		case 1: // target read: new bytes from the patch
			b, err := r.bytes(n)
			if err != nil {
				return nil, err
			}
			out = append(out, b...)
		case 2, 3: // source or target copy, from a relative offset
			d, err := r.number()
			if err != nil {
				return nil, err
			}
			delta := int64(d >> 1)
			if d&1 != 0 {
				delta = -delta
			}
			if data&3 == 2 {
				src += delta
				if src < 0 || src+int64(n) > int64(len(source)) {
					return nil, ErrCorrupt
				}
				out = append(out, source[src:src+int64(n)]...)
				src += int64(n)
			} else {
				dst += delta
				if dst < 0 || dst >= int64(len(out)) {
					return nil, ErrCorrupt
				}
				// Byte by byte: the copy may overlap what it writes.
				for range n {
					out = append(out, out[dst])
					dst++
				}
			}
		}
	}
	if uint64(len(out)) != size {
		return nil, ErrCorrupt
	}
	return out, nil
}
