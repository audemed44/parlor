// Package library finds the GBA ROMs in the library folder and matches save
// files to them by name.
package library

import (
	"crypto/sha1"
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

// MaxROMSize is the largest GBA ROM (32 MiB); anything bigger isn't one.
const MaxROMSize = 32 << 20

// Scan lists the .gba files under root, sorted by path. Hidden files and
// folders are skipped.
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
		if d.IsDir() || !strings.EqualFold(filepath.Ext(path), ".gba") {
			return nil
		}
		info, err := d.Info()
		if err != nil || !info.Mode().IsRegular() || info.Size() > MaxROMSize {
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
