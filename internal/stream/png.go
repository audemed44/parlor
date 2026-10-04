package stream

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/png"

	"github.com/audemed44/parlor/internal/stream/retro"
)

// Parlor keeps RetroArch cores' save states inside a PNG of the screen, so
// slots show what they hold: the state goes in a private chunk ("prLs")
// before the image's end, as the browser player does (frontend/src/png.ts).
const stateChunk = "prLs"

var pngMagic = []byte("\x89PNG\r\n\x1a\n")

// Wrap puts a state into a PNG of the frame.
func Wrap(img image.Image, state []byte) ([]byte, error) {
	var buf bytes.Buffer
	if err := (&png.Encoder{CompressionLevel: png.BestSpeed}).Encode(&buf, img); err != nil {
		return nil, err
	}
	p := buf.Bytes()
	end := len(p) - 12 // IEND is the last chunk
	out := make([]byte, 0, len(p)+12+len(state))
	out = append(out, p[:end]...)
	out = binary.BigEndian.AppendUint32(out, uint32(len(state)))
	start := len(out)
	out = append(out, stateChunk...)
	out = append(out, state...)
	out = binary.BigEndian.AppendUint32(out, crc32.ChecksumIEEE(out[start:]))
	return append(out, p[end:]...), nil
}

// Unwrap takes the state out of a PNG made by Wrap; a bare state comes
// back as it is.
func Unwrap(data []byte) ([]byte, error) {
	if !bytes.HasPrefix(data, pngMagic) {
		return data, nil
	}
	for at := 8; at+12 <= len(data); {
		n := int(binary.BigEndian.Uint32(data[at:]))
		if n < 0 || at+12+n > len(data) {
			break
		}
		if string(data[at+4:at+8]) == stateChunk {
			return data[at+8 : at+8+n], nil
		}
		at += 12 + n
	}
	return nil, errors.New("this picture holds no save state")
}

// Picture is the frame as an image, the right way up.
func Picture(f retro.Frame) image.Image {
	img := image.NewNRGBA(image.Rect(0, 0, f.Width, f.Height))
	if f.Format == retro.NV12 {
		nv12(img, f)
		return img
	}
	for y := 0; y < f.Height; y++ {
		sy := y
		if f.Flip {
			sy = f.Height - 1 - y
		}
		row := f.Pix[sy*f.Stride:]
		out := img.Pix[y*img.Stride:]
		for x := 0; x < f.Width; x++ {
			var r, g, b byte
			switch f.Format {
			case retro.RGBA:
				r, g, b = row[x*4], row[x*4+1], row[x*4+2]
			case retro.XRGB8888:
				r, g, b = row[x*4+2], row[x*4+1], row[x*4]
			case retro.RGB565:
				v := uint16(row[x*2]) | uint16(row[x*2+1])<<8
				r, g, b = byte(v>>11)<<3, byte(v>>5&0x3f)<<2, byte(v&0x1f)<<3
			default:
				v := uint16(row[x*2]) | uint16(row[x*2+1])<<8
				r, g, b = byte(v>>10&0x1f)<<3, byte(v>>5&0x1f)<<3, byte(v&0x1f)<<3
			}
			out[x*4], out[x*4+1], out[x*4+2], out[x*4+3] = r, g, b, 255
		}
	}
	return img
}

// nv12 converts the GPU's frames back: BT.709, limited range.
func nv12(img *image.NRGBA, f retro.Frame) {
	uv := f.Pix[f.Stride*f.Height:]
	clamp := func(v float64) byte { return byte(min(max(v, 0)+0.5, 255)) }
	for y := 0; y < f.Height; y++ {
		out := img.Pix[y*img.Stride:]
		for x := 0; x < f.Width; x++ {
			c := 1.164 * (float64(f.Pix[y*f.Stride+x]) - 16)
			i := (y/2)*f.Stride + x&^1
			u, v := float64(uv[i])-128, float64(uv[i+1])-128
			out[x*4] = clamp(c + 1.793*v)
			out[x*4+1] = clamp(c - 0.213*u - 0.533*v)
			out[x*4+2] = clamp(c + 2.112*u)
			out[x*4+3] = 255
		}
	}
}
