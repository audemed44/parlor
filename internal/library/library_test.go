package library

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

func TestKey(t *testing.T) {
	for in, want := range map[string]string{
		"Pokémon Heart and Soul (v2.0.4)":                      "pokemon heart and soul",
		"Pokemon Heart and Soul":                               "pokemon heart and soul",
		"Pokemon - FireRed Version (USA, Europe) [2025-11-24]": "pokemon firered version",
		"Pokemon Elysium_A":                                    "pokemon elysium a",
		"  ":                                                   "",
	} {
		if got := Key(in); got != want {
			t.Errorf("Key(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMatch(t *testing.T) {
	titles := map[int64]string{
		1: "Pokémon Heart and Soul (v2.0.4)",
		2: "Pokemon - FireRed Version (USA, Europe)",
		3: "Pokemon Elysium_A",
		4: "Pokemon Elysium_B",
		5: "Pokemon Emerald Seaglass",
		6: "Pokemon - Emerald Version (USA, Europe)",
	}
	for name, want := range map[string]int64{
		"Pokemon Heart and Soul.srm": 1,
		"Pokemon - FireRed Version (USA, Europe) [2025-11-24 12-35-32-901].srm": 2,
		"Pokemon Elysium_B.sav":           4,
		"Pokemon Elysium.srm":             3, // ambiguous: lowest ID of the longest match
		"Pokemon Emerald Seaglass v3.srm": 5,
		"Something else.srm":              0,
	} {
		if got := Match(name, titles); got != want {
			t.Errorf("Match(%q) = %d, want %d", name, got, want)
		}
	}
}

func TestScan(t *testing.T) {
	root := t.TempDir()
	for _, p := range []string{"b.gba", "a.GBA", "sub/c.gba", "notes.txt", ".hidden/d.gba", ".e.gba",
		"nds/roms/f.nds", "snes/g.sfc", "h.gbc", "i.3ds"} {
		path := filepath.Join(root, p)
		os.MkdirAll(filepath.Dir(path), 0755)
		os.WriteFile(path, []byte("rom"), 0644)
	}
	files, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	got := []string{}
	for _, f := range files {
		got = append(got, f.Path)
	}
	want := []string{"a.GBA", "b.gba", "h.gbc", "nds/roms/f.nds", "snes/g.sfc", "sub/c.gba"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
	if Title("sub/c.gba") != "c" {
		t.Fatal("title")
	}
}

func TestPlatformOf(t *testing.T) {
	for path, want := range map[string]string{
		"a.gba": "gba", "b.GB": "gb", "c.gbc": "gbc", "d.nes": "nes", "e.smc": "snes", "f.sfc": "snes",
		"g.nds": "nds", "h.3ds": "", "j.zip": "", "k": "",
	} {
		if got := PlatformOf(path); got != want {
			t.Errorf("PlatformOf(%q) = %q, want %q", path, got, want)
		}
	}
	for path, want := range map[string]string{
		"saves/nds/Pokemon Platinum.sav": "nds", "retrodeck-saves/n3ds/x.sav": "",
		"users/1/saves/gba/7/Heart.srm": "gba", "Heart.srm": "", "nds.sav": "",
	} {
		if got := PlatformHint(path); got != want {
			t.Errorf("PlatformHint(%q) = %q, want %q", path, got, want)
		}
	}
}

func TestUsed(t *testing.T) {
	// A DS cartridge padded to 64 KiB holding a 0x1000-byte game.
	nds := make([]byte, 64<<10)
	binary.LittleEndian.PutUint32(nds[0x80:], 0x1000)
	if got := Used(bytes.NewReader(nds), "nds", int64(len(nds))); got != 0x1088 {
		t.Fatalf("DS: %#x", got)
	}
	// DSi-enhanced: the DSi part ends later.
	nds[0x12] = 2
	binary.LittleEndian.PutUint32(nds[0x210:], 0x3000)
	if got := Used(bytes.NewReader(nds), "nds", int64(len(nds))); got != 0x3088 {
		t.Fatalf("DSi: %#x", got)
	}
	// A size past the end of the file means the header isn't one.
	binary.LittleEndian.PutUint32(nds[0x80:], 1<<30)
	if got := Used(bytes.NewReader(nds), "nds", int64(len(nds))); got != int64(len(nds)) {
		t.Fatalf("bad DS header: %#x", got)
	}
	if got := Used(bytes.NewReader(nds), "gba", int64(len(nds))); got != int64(len(nds)) {
		t.Fatalf("GBA: %#x", got)
	}
}
