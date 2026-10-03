package library

import (
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
	for _, p := range []string{"b.gba", "a.GBA", "sub/c.gba", "notes.txt", ".hidden/d.gba", ".e.gba"} {
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
	want := []string{"a.GBA", "b.gba", "sub/c.gba"}
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
