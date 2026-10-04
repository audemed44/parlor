package store

import (
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ErrBadName is returned for a patched game's name that can't be a file name.
var ErrBadName = errors.New("give the game a name, without / or \\")

// ErrExists is returned when the patched ROM is already in the library.
type ErrExists struct{ Title string }

func (e ErrExists) Error() string { return fmt.Sprintf("“%s” is already this ROM", e.Title) }

// NewPatched is a ROM made by applying a patch.
type NewPatched struct {
	Title string
	ROM   []byte
	// CarryFrom is the game whose save, notes, play time and settings the
	// new one takes over (an older version of the same hack); 0 for none.
	CarryFrom int64
	// HideOld hides CarryFrom from the library.
	HideOld bool
	Device  string
}

// AddPatched stores a patched ROM in <data>/roms and adds it to the
// library, carrying the older version's save over as its first.
func (s *Store) AddPatched(n NewPatched) (Game, error) {
	name := strings.TrimSpace(n.Title)
	if name == "" || len(name) > 150 || strings.ContainsAny(name, `/\`) || strings.HasPrefix(name, ".") ||
		strings.ContainsFunc(name, func(r rune) bool { return r < 32 }) {
		return Game{}, ErrBadName
	}
	var from Game
	var err error
	if n.CarryFrom != 0 {
		if from, err = s.Game(n.CarryFrom); err != nil {
			return Game{}, err
		}
	}
	sum := sha1.Sum(n.ROM)
	hash := hex.EncodeToString(sum[:])
	s.mu.Lock()
	var existing string
	if s.DB.QueryRow("SELECT title FROM games WHERE sha1=? AND missing=0", hash).Scan(&existing) == nil {
		s.mu.Unlock()
		return Game{}, ErrExists{existing}
	}
	path := filepath.Join(s.Dir, "roms", name+".gba")
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if errors.Is(err, os.ErrExist) {
		s.mu.Unlock()
		return Game{}, ErrExists{name}
	}
	if err == nil {
		_, err = f.Write(n.ROM)
		if cerr := f.Close(); err == nil {
			err = cerr
		}
	}
	if err != nil {
		os.Remove(path)
		s.mu.Unlock()
		return Game{}, err
	}
	info, err := os.Stat(path)
	if err != nil {
		s.mu.Unlock()
		return Game{}, err
	}
	res, err := s.DB.Exec(`INSERT INTO games(path, title, size, mtime, sha1, added, notes, play_seconds, last_played, save_type, rtc)
		VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		Patched+name+".gba", name, info.Size(), info.ModTime().Unix(), hash, stamp(s.Now()),
		from.Notes, from.PlaySeconds, from.LastPlayed, from.SaveType, from.RTC)
	if err != nil {
		os.Remove(path)
		s.mu.Unlock()
		return Game{}, err
	}
	id, _ := res.LastInsertId()
	s.mu.Unlock()

	if from.Save != nil {
		data, err := s.SaveData(*from.Save)
		if err != nil {
			return Game{}, err
		}
		_, err = s.AddSave(id, NewSave{
			Data: data, Source: "carry", Device: n.Device, Note: "From " + from.Title, Force: true,
		})
		if err != nil {
			return Game{}, err
		}
	}
	if n.HideOld && from.ID != 0 {
		if err = s.SetHidden(from.ID, true); err != nil {
			return Game{}, err
		}
	}
	return s.Game(id)
}
