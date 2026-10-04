package stream

import (
	"archive/tar"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// A 3DS game keeps its save as files on the SD card (its save data and
// extra data), not one battery RAM. Parlor stores them as one save: a tar
// of the folder the emulator keeps them in, written the same way every
// time (sorted, no owners or times) so the same files give the same bytes.

// MaxSave is the most a save folder may hold; Parlor takes up to 8 MiB.
const MaxSave = 8 << 20

// Pack is the folder's files as a tar; empty folders are kept, since the
// emulator expects the save's folders to be there.
func Pack(dir string) ([]byte, error) {
	type entry struct {
		name string
		dir  bool
		size int64
	}
	var entries []entry
	var total int64
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == dir {
			return nil
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		switch {
		case d.IsDir():
			entries = append(entries, entry{name: filepath.ToSlash(rel) + "/", dir: true})
		case info.Mode().IsRegular():
			total += info.Size()
			if total > MaxSave {
				return fmt.Errorf("the save folder holds more than %d MiB", MaxSave>>20)
			}
			entries = append(entries, entry{name: filepath.ToSlash(rel), size: info.Size()})
		}
		return nil
	})
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, nil
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].name < entries[j].name })
	var buf bytes.Buffer
	w := tar.NewWriter(&buf)
	for _, e := range entries {
		h := &tar.Header{Name: e.name, Mode: 0o644, ModTime: time.Unix(0, 0), Format: tar.FormatPAX}
		if e.dir {
			h.Typeflag = tar.TypeDir
			h.Mode = 0o755
			if err := w.WriteHeader(h); err != nil {
				return nil, err
			}
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(e.name)))
		if err != nil {
			return nil, err
		}
		h.Typeflag = tar.TypeReg
		h.Size = int64(len(data))
		if err := w.WriteHeader(h); err != nil {
			return nil, err
		}
		if _, err := w.Write(data); err != nil {
			return nil, err
		}
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Unpack replaces the folder's contents with a tar made by Pack.
func Unpack(dir string, data []byte) error {
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	r := tar.NewReader(bytes.NewReader(data))
	var total int64
	for {
		h, err := r.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("this save isn't a 3DS save folder: %w", err)
		}
		name := path.Clean(h.Name)
		if name == "." || path.IsAbs(name) || name == ".." || strings.HasPrefix(name, "../") {
			return fmt.Errorf("this save has a file outside its folder: %q", h.Name)
		}
		p := filepath.Join(dir, filepath.FromSlash(name))
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(p, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			total += h.Size
			if total > MaxSave {
				return errors.New("this save is too large")
			}
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				return err
			}
			f, err := os.OpenFile(p, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
			if err != nil {
				return err
			}
			_, err = io.Copy(f, io.LimitReader(r, h.Size))
			if cerr := f.Close(); err == nil {
				err = cerr
			}
			if err != nil {
				return err
			}
		}
	}
}

// Fingerprint changes whenever a file in the folder does: names, sizes
// and times. Cheap enough to check every few seconds.
func Fingerprint(dir string) string {
	var b strings.Builder
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		fmt.Fprintf(&b, "%s|%d|%d\n", p, info.Size(), info.ModTime().UnixNano())
		return nil
	})
	return b.String()
}
