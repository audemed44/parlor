// Package library finds the ROMs in the library folder, tells which console
// each is for, and matches save files to them by name.
package library

import (
	"crypto/sha1"
	"encoding/binary"
	"encoding/hex"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
)

// File is one ROM in the library.
type File struct {
	Path  string // relative to the library root, with forward slashes
	Size  int64
	MTime int64 // Unix seconds
}

// Platform is a console Parlor plays.
type Platform struct {
	ID    string
	Short string
	Name  string
	Exts  []string
	// MaxSize is the largest ROM for it; anything bigger isn't one.
	MaxSize int64
}

// Platforms are the consoles, by ROM file extension. The Game Boy Color
// plays Game Boy games too, but they're kept apart for the library.
var Platforms = []Platform{
	{"gba", "GBA", "Game Boy Advance", []string{".gba"}, 32 << 20},
	{"gb", "GB", "Game Boy", []string{".gb"}, 8 << 20},
	{"gbc", "GBC", "Game Boy Color", []string{".gbc"}, 8 << 20},
	{"nes", "NES", "NES", []string{".nes"}, 8 << 20},
	{"snes", "SNES", "Super Nintendo", []string{".sfc", ".smc"}, 16 << 20},
	{"nds", "DS", "Nintendo DS", []string{".nds"}, 1 << 30},
}

// PlatformOf is the console a ROM is for, by its extension; "" for none.
func PlatformOf(path string) string {
	if p, ok := platformOf(path); ok {
		return p.ID
	}
	return ""
}

// ShortName is a console's short name ("DS"), "" for none.
func ShortName(id string) string {
	for _, p := range Platforms {
		if p.ID == id {
			return p.Short
		}
	}
	return ""
}

func platformOf(path string) (Platform, bool) {
	ext := strings.ToLower(filepath.Ext(path))
	for _, p := range Platforms {
		for _, e := range p.Exts {
			if e == ext {
				return p, true
			}
		}
	}
	return Platform{}, false
}

// Scan lists the ROMs under root, sorted by path. Hidden files and folders
// are skipped.
func Scan(root string) ([]File, error) {
	files := []File{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if strings.HasPrefix(d.Name(), ".") && path != root {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		p, ok := platformOf(path)
		if !ok {
			return nil
		}
		info, err := d.Info()
		if err != nil || !info.Mode().IsRegular() || info.Size() > p.MaxSize {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files = append(files, File{Path: filepath.ToSlash(rel), Size: info.Size(), MTime: info.ModTime().Unix()})
		return nil
	})
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, err
}

// Hash is the SHA-1 of a file, the checksum ROM databases use.
func Hash(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha1.New()
	if _, err = io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// folderNames are what save folders call the consoles (RomM's and
// RetroDECK's platform folders).
var folderNames = map[string]string{
	"gba": "gba", "gb": "gb", "gbc": "gbc", "nes": "nes", "famicom": "nes",
	"snes": "snes", "sfc": "snes", "nds": "nds",
}

// PlatformHint is the console a file's folders name, like saves/nds/...;
// "" when none does.
func PlatformHint(path string) string {
	parts := strings.Split(filepath.ToSlash(path), "/")
	for i := len(parts) - 2; i >= 0; i-- {
		if p, ok := folderNames[strings.ToLower(parts[i])]; ok {
			return p
		}
	}
	return ""
}

// Used is how much of a ROM file the game uses. DS dumps are padded to the
// cartridge's size (a 512 MiB file may hold 280 MiB); the header says where
// the game ends, so the padding is never sent. For other consoles, or a
// header that doesn't make sense, it's the whole file.
func Used(r io.ReaderAt, platform string, size int64) int64 {
	var used int64
	switch platform {
	case "nds":
		h := make([]byte, 0x214)
		if _, err := r.ReadAt(h, 0); err != nil {
			return size
		}
		used = int64(binary.LittleEndian.Uint32(h[0x80:]))
		// DSi-enhanced games keep their DSi part after the DS one.
		if h[0x12]&2 != 0 {
			used = max(used, int64(binary.LittleEndian.Uint32(h[0x210:])))
		}
		// Download Play's signature follows the game.
		used += 0x88
	default:
		return size
	}
	if used < 0x200 || used > size {
		return size
	}
	return used
}

// Title is the name shown for a ROM: its file name without the extension.
func Title(path string) string {
	base := filepath.Base(filepath.FromSlash(path))
	return strings.TrimSuffix(base, filepath.Ext(base))
}

var folds = map[rune]rune{
	'á': 'a', 'à': 'a', 'â': 'a', 'ä': 'a', 'ã': 'a', 'å': 'a',
	'é': 'e', 'è': 'e', 'ê': 'e', 'ë': 'e',
	'í': 'i', 'ì': 'i', 'î': 'i', 'ï': 'i',
	'ó': 'o', 'ò': 'o', 'ô': 'o', 'ö': 'o', 'õ': 'o',
	'ú': 'u', 'ù': 'u', 'û': 'u', 'ü': 'u', 'ñ': 'n', 'ç': 'c',
}

// Key is a name reduced for matching: lower case, accents dropped, anything
// in brackets or parentheses removed (regions, versions, timestamps), and
// punctuation turned into single spaces. "Pokémon Heart and Soul (v2.0.4)"
// and "Pokemon Heart and Soul [2026-09-13 19-00-02]" both become
// "pokemon heart and soul".
func Key(name string) string {
	var b strings.Builder
	depth := 0
	space := false
	for _, r := range strings.ToLower(name) {
		switch r {
		case '(', '[':
			depth++
			continue
		case ')', ']':
			if depth > 0 {
				depth--
			}
			continue
		}
		if depth > 0 {
			continue
		}
		if f, ok := folds[r]; ok {
			r = f
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if space && b.Len() > 0 {
				b.WriteByte(' ')
			}
			space = false
			b.WriteRune(r)
		} else {
			space = true
		}
	}
	return b.String()
}

// Match picks the game a save file belongs to, from the save's file name.
// titles maps game IDs to their titles. An exact Key match wins; otherwise
// the game whose key and the save's share the longest word-boundary prefix,
// as long as one is a prefix of the other. 0 means no match.
func Match(saveName string, titles map[int64]string) int64 {
	base := filepath.Base(saveName)
	key := Key(strings.TrimSuffix(base, filepath.Ext(base)))
	if key == "" {
		return 0
	}
	var best int64
	bestLen := 0
	ids := make([]int64, 0, len(titles))
	for id := range titles {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		k := Key(titles[id])
		if k == key {
			return id
		}
		short, long := k, key
		if len(short) > len(long) {
			short, long = long, short
		}
		if short != "" && strings.HasPrefix(long, short+" ") && len(short) > bestLen {
			best, bestLen = id, len(short)
		}
	}
	return best
}
