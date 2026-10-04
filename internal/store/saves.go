package store

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// MaxSaveSize is the largest in-game save accepted. GBA saves are at most
// 128 KiB (Flash 1M), DS ones usually 512 KiB, though a few DS cartridges
// hold up to 8 MiB.
const MaxSaveSize = 8 << 20

// ErrInvalidSave is returned for an empty or oversized save.
var ErrInvalidSave = fmt.Errorf("a save must be between 1 byte and %d KiB", MaxSaveSize>>10)

// Save is one version of a game's in-game save (.srm).
type Save struct {
	ID      int64  `json:"id"`
	GameID  int64  `json:"game_id"`
	Created string `json:"created"`
	Size    int64  `json:"size"`
	SHA256  string `json:"sha256"`
	// Source is how it arrived: play, upload, import or restore.
	Source string `json:"source"`
	Device string `json:"device"`
	Note   string `json:"note"`
}

// Conflict is returned by AddSave when the game's latest save isn't the
// one the writer started from: another device saved in between.
type Conflict struct{ Latest Save }

func (c *Conflict) Error() string { return "the save changed since this game was loaded" }

// NewSave describes a save being added.
type NewSave struct {
	Data   []byte
	Source string
	Device string
	Note   string
	// Base is the save version the writer loaded (0 for none). When it
	// isn't the latest, AddSave refuses with a Conflict unless Force.
	Base  int64
	Force bool
	// Created backdates the version (imports use the file's time); zero
	// means now.
	Created time.Time
}

const saveColumns = "id, game_id, created, size, sha256, source, device, note"

func scanSave(row interface{ Scan(...any) error }) (Save, error) {
	var v Save
	err := row.Scan(&v.ID, &v.GameID, &v.Created, &v.Size, &v.SHA256, &v.Source, &v.Device, &v.Note)
	return v, err
}

func (s *Store) savePath(gameID, id int64) string {
	return filepath.Join(s.Dir, "saves", strconv.FormatInt(gameID, 10), strconv.FormatInt(id, 10)+".srm")
}

// Latest is a game's newest save, or nil when it has none.
func (s *Store) Latest(gameID int64) (*Save, error) {
	v, err := scanSave(s.DB.QueryRow("SELECT "+saveColumns+
		" FROM saves WHERE game_id=? ORDER BY created DESC, id DESC LIMIT 1", gameID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &v, err
}

func (s *Store) latestSaves() (map[int64]*Save, error) {
	rows, err := s.DB.Query("SELECT " + saveColumns + ` FROM saves s WHERE id = (
		SELECT id FROM saves WHERE game_id=s.game_id ORDER BY created DESC, id DESC LIMIT 1)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]*Save{}
	for rows.Next() {
		v, err := scanSave(rows)
		if err != nil {
			return nil, err
		}
		out[v.GameID] = &v
	}
	return out, rows.Err()
}

// Saves lists a game's save versions, newest first.
func (s *Store) Saves(gameID int64) ([]Save, error) {
	rows, err := s.DB.Query("SELECT "+saveColumns+" FROM saves WHERE game_id=? ORDER BY created DESC, id DESC", gameID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Save{}
	for rows.Next() {
		v, err := scanSave(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// SaveVersion returns one save version.
func (s *Store) SaveVersion(id int64) (Save, error) {
	v, err := scanSave(s.DB.QueryRow("SELECT "+saveColumns+" FROM saves WHERE id=?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return v, ErrNotFound
	}
	return v, err
}

// SaveData reads a save version's bytes.
func (s *Store) SaveData(v Save) ([]byte, error) {
	return os.ReadFile(s.savePath(v.GameID, v.ID))
}

// AddSave stores a new save version for a game and thins old ones. Saving
// the same bytes as the latest version changes nothing and returns it.
func (s *Store) AddSave(gameID int64, n NewSave) (Save, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.addSave(gameID, n)
}

func (s *Store) addSave(gameID int64, n NewSave) (Save, error) {
	if len(n.Data) == 0 || len(n.Data) > MaxSaveSize {
		return Save{}, ErrInvalidSave
	}
	if _, err := s.Game(gameID); err != nil {
		return Save{}, err
	}
	sum := sha256.Sum256(n.Data)
	hash := hex.EncodeToString(sum[:])
	latest, err := s.Latest(gameID)
	if err != nil {
		return Save{}, err
	}
	if latest != nil && latest.SHA256 == hash && n.Created.IsZero() {
		return *latest, nil
	}
	if latest != nil && latest.ID != n.Base && !n.Force {
		return Save{}, &Conflict{Latest: *latest}
	}
	created := n.Created
	if created.IsZero() {
		created = s.Now()
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return Save{}, err
	}
	defer tx.Rollback()
	res, err := tx.Exec("INSERT INTO saves(game_id, created, size, sha256, source, device, note) VALUES(?,?,?,?,?,?,?)",
		gameID, stamp(created), len(n.Data), hash, n.Source, n.Device, n.Note)
	if err != nil {
		return Save{}, err
	}
	id, _ := res.LastInsertId()
	path := s.savePath(gameID, id)
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return Save{}, err
	}
	tmp := path + ".tmp"
	if err = os.WriteFile(tmp, n.Data, 0600); err != nil {
		return Save{}, err
	}
	if err = os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return Save{}, err
	}
	if err = tx.Commit(); err != nil {
		os.Remove(path)
		return Save{}, err
	}
	if err = s.prune(gameID); err != nil {
		return Save{}, err
	}
	return s.SaveVersion(id)
}

// Restore makes an older version the latest again, as a new version, so
// history only ever grows forward and nothing is overwritten.
func (s *Store) Restore(id int64, device string) (Save, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, err := s.SaveVersion(id)
	if err != nil {
		return Save{}, err
	}
	data, err := s.SaveData(v)
	if err != nil {
		return Save{}, err
	}
	latest, err := s.Latest(v.GameID)
	if err != nil {
		return Save{}, err
	}
	if latest != nil && latest.ID == v.ID {
		return v, nil
	}
	return s.addSave(v.GameID, NewSave{
		Data: data, Source: "restore", Device: device, Force: true,
		Note: "Restored from " + v.Created,
	})
}

// prune keeps a game's newest Keep versions, plus the newest version of
// each day for the last KeepDays days (in the server's time zone).
func (s *Store) prune(gameID int64) error {
	versions, err := s.Saves(gameID)
	if err != nil {
		return err
	}
	cutoff := s.Now().AddDate(0, 0, -s.KeepDays)
	days := map[string]bool{}
	for i, v := range versions {
		created, err := time.Parse(timeFormat, v.Created)
		if err != nil {
			return err
		}
		day := created.Local().Format("2006-01-02")
		keep := i < s.Keep || (!days[day] && created.After(cutoff))
		days[day] = true
		if keep {
			continue
		}
		if _, err = s.DB.Exec("DELETE FROM saves WHERE id=?", v.ID); err != nil {
			return err
		}
		if err = os.Remove(s.savePath(gameID, v.ID)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}
