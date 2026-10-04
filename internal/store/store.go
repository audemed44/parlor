// Package store keeps Parlor's state: SQLite at <dir>/parlor.db, every save
// version as a file under <dir>/saves/<game>/<version>.srm and save states
// under <dir>/states/<game>/<slot>.ss. Keep them together in backups.
package store

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "modernc.org/sqlite" // pure Go, so the build stays cgo-free
)

const schema = `PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000; PRAGMA foreign_keys=ON;
CREATE TABLE IF NOT EXISTS games(
  id INTEGER PRIMARY KEY, path TEXT UNIQUE NOT NULL, title TEXT NOT NULL,
  size INTEGER NOT NULL, mtime INTEGER NOT NULL, sha1 TEXT NOT NULL,
  missing INTEGER NOT NULL DEFAULT 0, added TEXT NOT NULL,
  last_played TEXT NOT NULL DEFAULT '', play_seconds INTEGER NOT NULL DEFAULT 0,
  notes TEXT NOT NULL DEFAULT '');
CREATE TABLE IF NOT EXISTS saves(
  id INTEGER PRIMARY KEY, game_id INTEGER NOT NULL REFERENCES games(id),
  created TEXT NOT NULL, size INTEGER NOT NULL, sha256 TEXT NOT NULL,
  source TEXT NOT NULL, device TEXT NOT NULL DEFAULT '', note TEXT NOT NULL DEFAULT '');
CREATE INDEX IF NOT EXISTS saves_game ON saves(game_id, created);
CREATE TABLE IF NOT EXISTS imports(
  sha256 TEXT NOT NULL, path TEXT NOT NULL, game_id INTEGER NOT NULL, imported TEXT NOT NULL,
  PRIMARY KEY(sha256, path));
CREATE TABLE IF NOT EXISTS settings(key TEXT PRIMARY KEY, value TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS states(
  id INTEGER PRIMARY KEY, game_id INTEGER NOT NULL REFERENCES games(id), slot INTEGER NOT NULL,
  created TEXT NOT NULL, size INTEGER NOT NULL, sha256 TEXT NOT NULL,
  device TEXT NOT NULL DEFAULT '', note TEXT NOT NULL DEFAULT '', UNIQUE(game_id, slot));`

// timeFormat is how times are stored: UTC, fixed width, so they sort as text.
const timeFormat = "2006-01-02T15:04:05.000Z"

func stamp(t time.Time) string { return t.UTC().Format(timeFormat) }

// Store is the database plus the save and state files.
type Store struct {
	DB  *sql.DB
	Dir string
	// Keep is how many recent save versions each game keeps; older ones
	// are thinned to one a day for KeepDays days.
	Keep     int
	KeepDays int
	// Now is the clock; tests replace it.
	Now func() time.Time
	// mu serialises writes that read then update: library scans and saves.
	mu sync.Mutex
}

// ErrNotFound is returned for a game or save that doesn't exist.
var ErrNotFound = errors.New("not found")

// Open opens (creating and upgrading as needed) the store in dir.
func Open(dir string) (*Store, error) {
	for _, sub := range []string{"saves", "states"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0700); err != nil {
			return nil, err
		}
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, "parlor.db"))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if _, err = db.Exec(schema); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{DB: db, Dir: dir, Keep: 20, KeepDays: 30, Now: time.Now}, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.DB.Close() }

// Setting returns a stored value, or "" when it isn't set.
func (s *Store) Setting(key string) (string, error) {
	var v string
	err := s.DB.QueryRow("SELECT value FROM settings WHERE key=?", key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return v, err
}

// SetSetting stores a value.
func (s *Store) SetSetting(key, value string) error {
	_, err := s.DB.Exec(
		"INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value",
		key, value)
	return err
}
