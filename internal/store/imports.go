package store

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/audemed44/parlor/internal/library"
)

// Candidate is a save file in the import folder.
type Candidate struct {
	Path     string `json:"path"` // relative to the import folder
	Size     int64  `json:"size"`
	Modified string `json:"modified"`
	SHA256   string `json:"sha256"`
	// Suggested is the game the file name matches, 0 for none.
	Suggested int64 `json:"suggested"`
	// Imported is the game it was imported into, 0 if it hasn't been.
	Imported int64 `json:"imported"`
}

func isSaveFile(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	return ext == ".srm" || ext == ".sav"
}

// Candidates lists the in-game saves (.srm, .sav) under dir, newest first,
// each with the game its name suggests and whether it was imported already.
// Save states aren't included.
func (s *Store) Candidates(dir string) ([]Candidate, error) {
	titles, err := s.Titles()
	if err != nil {
		return nil, err
	}
	out := []Candidate{}
	err = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !isSaveFile(d.Name()) {
			return nil
		}
		info, err := d.Info()
		if err != nil || !info.Mode().IsRegular() || info.Size() == 0 || info.Size() > MaxSaveSize {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(dir, path)
		sum := sha256.Sum256(data)
		c := Candidate{
			Path:      filepath.ToSlash(rel),
			Size:      info.Size(),
			Modified:  stamp(info.ModTime()),
			SHA256:    hex.EncodeToString(sum[:]),
			Suggested: library.Match(d.Name(), titles),
		}
		_ = s.DB.QueryRow("SELECT game_id FROM imports WHERE sha256=? AND path=?", c.SHA256, c.Path).Scan(&c.Imported)
		out = append(out, c)
		return nil
	})
	sort.SliceStable(out, func(i, j int) bool { return out[i].Modified > out[j].Modified })
	return out, err
}

// Import adds a save file from dir to a game, dated by the file's time, so
// an older file lands in the history without replacing a newer save. A file
// whose bytes the game already has is only recorded as imported.
func (s *Store) Import(dir, rel string, gameID int64) (Save, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	path, err := within(dir, rel)
	if err != nil || !isSaveFile(path) {
		return Save{}, ErrNotFound
	}
	info, err := os.Stat(path)
	if err != nil {
		return Save{}, ErrNotFound
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Save{}, err
	}
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])
	v, err := scanSave(s.DB.QueryRow("SELECT "+saveColumns+" FROM saves WHERE game_id=? AND sha256=? ORDER BY id LIMIT 1", gameID, hash))
	if err != nil {
		v, err = s.addSave(gameID, NewSave{
			Data: data, Source: "import", Device: "Import", Note: rel,
			Force: true, Created: info.ModTime(),
		})
		if err != nil {
			return Save{}, err
		}
	}
	_, err = s.DB.Exec("INSERT OR REPLACE INTO imports(sha256, path, game_id, imported) VALUES(?,?,?,?)",
		hash, filepath.ToSlash(rel), gameID, stamp(s.Now()))
	return v, err
}

// within resolves rel inside dir, refusing anything that escapes it.
func within(dir, rel string) (string, error) {
	if dir == "" || !filepath.IsLocal(filepath.FromSlash(rel)) {
		return "", errors.New("path outside the folder")
	}
	return filepath.Join(dir, filepath.FromSlash(rel)), nil
}
