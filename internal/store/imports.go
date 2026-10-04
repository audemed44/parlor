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

// Candidate is a save file or save state in the import folder.
type Candidate struct {
	Path string `json:"path"` // relative to the import folder
	// Kind is "save" (in-game, .srm/.sav) or "state" (.state, .ss0-9).
	Kind     string `json:"kind"`
	Size     int64  `json:"size"`
	Modified string `json:"modified"`
	SHA256   string `json:"sha256"`
	// Suggested is the game the file name matches, 0 for none.
	Suggested int64 `json:"suggested"`
	// Imported is the game it was imported into, 0 if it hasn't been.
	Imported int64 `json:"imported"`
}

// kind is what an import file is by its extension: "save", "state" or "".
func kind(name string) string {
	ext := strings.ToLower(filepath.Ext(name))
	switch {
	case ext == ".srm" || ext == ".sav":
		return "save"
	case ext == ".state" || ext == ".ss" || len(ext) == 4 && strings.HasPrefix(ext, ".ss") && ext[3] >= '0' && ext[3] <= '9':
		return "state"
	}
	return ""
}

// Candidates lists the in-game saves (.srm, .sav) and save states (.state
// from RomM, .ss1 from mGBA) under dir, newest first, each with the game
// its name suggests and whether it was imported already.
func (s *Store) Candidates(dir string) ([]Candidate, error) {
	titles, err := s.Titles()
	if err != nil {
		return nil, err
	}
	// A save in a console's folder only matches that console's games.
	byPlatform := map[string]map[int64]string{}
	for id, t := range titles {
		p := library.PlatformOf(t.path)
		if byPlatform[p] == nil {
			byPlatform[p] = map[int64]string{}
		}
		byPlatform[p][id] = t.title
	}
	all := map[int64]string{}
	for id, t := range titles {
		all[id] = t.title
	}
	out := []Candidate{}
	err = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		k := kind(d.Name())
		if d.IsDir() || k == "" {
			return nil
		}
		limit := int64(MaxSaveSize)
		if k == "state" {
			limit = MaxStateSize
		}
		info, err := d.Info()
		if err != nil || !info.Mode().IsRegular() || info.Size() == 0 || info.Size() > limit {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(dir, path)
		sum := sha256.Sum256(data)
		match := all
		if p := library.PlatformHint(rel); p != "" {
			match = byPlatform[p]
		}
		c := Candidate{
			Path:      filepath.ToSlash(rel),
			Kind:      k,
			Size:      info.Size(),
			Modified:  stamp(info.ModTime()),
			SHA256:    hex.EncodeToString(sum[:]),
			Suggested: library.Match(d.Name(), match),
		}
		_ = s.DB.QueryRow("SELECT game_id FROM imports WHERE sha256=? AND path=?", c.SHA256, c.Path).Scan(&c.Imported)
		out = append(out, c)
		return nil
	})
	sort.SliceStable(out, func(i, j int) bool { return out[i].Modified > out[j].Modified })
	return out, err
}

// ImportState puts a save state from dir in one of a game's slots.
func (s *Store) ImportState(dir, rel string, gameID int64, slot int) (State, error) {
	path, err := within(dir, rel)
	if err != nil || kind(path) != "state" {
		return State{}, ErrNotFound
	}
	info, err := os.Stat(path)
	if err != nil || info.Size() > MaxStateSize {
		return State{}, ErrNotFound
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return State{}, err
	}
	v, err := s.PutState(gameID, slot, NewState{Data: data, Device: "Import", Note: rel, Created: info.ModTime()})
	if err != nil {
		return State{}, err
	}
	sum := sha256.Sum256(data)
	_, err = s.DB.Exec("INSERT OR REPLACE INTO imports(sha256, path, game_id, imported) VALUES(?,?,?,?)",
		hex.EncodeToString(sum[:]), filepath.ToSlash(rel), gameID, stamp(s.Now()))
	return v, err
}

// Import adds a save file from dir to a game, dated by the file's time, so
// an older file lands in the history without replacing a newer save. A file
// whose bytes the game already has is only recorded as imported.
func (s *Store) Import(dir, rel string, gameID int64) (Save, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	path, err := within(dir, rel)
	if err != nil || kind(path) != "save" {
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
