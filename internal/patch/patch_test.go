package patch

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"testing"
)

func number(v uint64) []byte {
	var out []byte
	for {
		x := byte(v & 0x7f)
		v >>= 7
		if v == 0 {
			return append(out, x|0x80)
		}
		out = append(out, x)
		v--
	}
}

// footer adds UPS/BPS's checksums.
func footer(p, source, target []byte) []byte {
	p = binary.LittleEndian.AppendUint32(p, crc32.ChecksumIEEE(source))
	p = binary.LittleEndian.AppendUint32(p, crc32.ChecksumIEEE(target))
	return binary.LittleEndian.AppendUint32(p, crc32.ChecksumIEEE(p))
}

var (
	source = []byte("Pokemon Emerald, the base ROM, with some bytes in it")
	target = []byte("Pokemon Heart and Soul, a hack of the base ROM, with some bytes in it!!")
)

// bpsPatch makes target from source: a source read, a target read, a
// source copy and a target copy, so every action is used.
func bpsPatch() []byte {
	p := []byte("BPS1")
	p = append(p, number(uint64(len(source)))...)
	p = append(p, number(uint64(len(target)))...)
	p = append(p, number(0)...)
	act := func(cmd, n int) { p = append(p, number(uint64((n-1)<<2|cmd))...) }
	act(0, 8) // "Pokemon "
	tail := []byte("Heart and Soul, a hack of")
	act(1, len(tail))
	p = append(p, tail...)
	// " the base ROM, with some bytes in it" from source offset 16.
	act(2, len(source)-16)
	p = append(p, number(16<<1)...)
	// "!!": copy the target's last byte... from target offset len-1.
	act(1, 1)
	p = append(p, '!')
	act(3, 1)
	at := uint64(len(target) - 2)
	p = append(p, number(at<<1)...)
	return footer(p, source, target)
}

func upsPatch() []byte {
	p := []byte("UPS1")
	p = append(p, number(uint64(len(source)))...)
	p = append(p, number(uint64(len(target)))...)
	// One XOR run from the first difference to the end.
	first := 0
	for first < len(source) && source[first] == target[first] {
		first++
	}
	p = append(p, number(uint64(first))...)
	for i := first; i < len(target); i++ {
		var s byte
		if i < len(source) {
			s = source[i]
		}
		x := s ^ target[i]
		if x == 0 {
			// A zero ends a run; a new run starts after it.
			p = append(p, 0)
			p = append(p, number(0)...)
			continue
		}
		p = append(p, x)
	}
	p = append(p, 0)
	return footer(p, source, target)
}

func TestBPS(t *testing.T) {
	p := bpsPatch()
	info, err := Inspect(p)
	if err != nil || info.Format != "bps" || info.SourceSize != int64(len(source)) || info.SourceCRC != crc32.ChecksumIEEE(source) {
		t.Fatalf("inspect: %+v %v", info, err)
	}
	out, err := Apply(source, p)
	if err != nil || !bytes.Equal(out, target) {
		t.Fatalf("apply: %q %v", out, err)
	}
	if _, err = Apply(append(bytes.Clone(source[:len(source)-1]), 'X'), p); !errors.Is(err, ErrWrongROM) {
		t.Fatalf("wrong ROM: %v", err)
	}
	broken := bytes.Clone(p)
	broken[10] ^= 1
	if _, err = Apply(source, broken); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("damaged: %v", err)
	}
}

func TestUPS(t *testing.T) {
	out, err := Apply(source, upsPatch())
	if err != nil || !bytes.Equal(out, target) {
		t.Fatalf("apply: %q %v", out, err)
	}
	if _, err = Apply(target, upsPatch()); !errors.Is(err, ErrWrongROM) {
		t.Fatalf("wrong ROM: %v", err)
	}
}

func TestIPS(t *testing.T) {
	p := []byte("PATCH")
	p = append(p, 0, 0, 8, 0, 5)
	p = append(p, "Ruby!"...)
	// A run of 3 'z' past the end, growing the ROM.
	end := len(source) + 2
	p = append(p, byte(end>>16), byte(end>>8), byte(end), 0, 0, 0, 3, 'z')
	p = append(p, "EOF"...)
	out, err := Apply(source, p)
	want := append(append(bytes.Clone(source[:8]), "Ruby!"...), source[13:]...)
	want = append(want, 0, 0, 'z', 'z', 'z')
	if err != nil || !bytes.Equal(out, want) {
		t.Fatalf("apply: %q %v", out, err)
	}
	if !bytes.Equal(source[:8], []byte("Pokemon ")) {
		t.Fatal("source changed")
	}
	// Truncated after EOF.
	cut := append(bytes.Clone(p), 0, 0, 7)
	if out, _ = Apply(source, cut); string(out) != "Pokemon" {
		t.Fatalf("truncate: %q", out)
	}
	if _, err = Apply(source, []byte("PATCH\x00\x00")); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("short: %v", err)
	}
	if _, err = Inspect([]byte("hello")); !errors.Is(err, ErrFormat) {
		t.Fatalf("format: %v", err)
	}
}
