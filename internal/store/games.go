package store

import (
	"database/sql"
	"errors"
	"log/slog"
	"path/filepath"
	"slices"
	"strings"

	"github.com/audemed44/parlor/internal/library"
)

// Game is one ROM in the library, with its latest save.
type Game struct {
	ID          int64  `json:"id"`
	Path        string `json:"path"`
	Title       string `json:"title"`
	Size        int64  `json:"size"`
	SHA1        string `json:"sha1"`
	Missing     bool   `json:"missing"`
	Added       string `json:"added"`
	LastPlayed  string `json:"last_played"`
	PlaySeconds int64  `json:"play_seconds"`
	Notes       string `json:"notes"`
	// SaveType and RTC override what mGBA detects; "" leaves it to mGBA.
	SaveType string `json:"save_type"`
	RTC      string `json:"rtc"`
	// Hidden games are left out of the library grid (an old version of a
	// hack, say), but keep their saves.
	Hidden bool  `json:"hidden"`
	Save   *Save `json:"save"`
}

// Patched is the path prefix of ROMs Parlor made by applying a patch. They
// live in <data>/roms, since the library folder is read-only.
const Patched = "parlor:"

// ROMFile is where a game's ROM is on disk.
func (s *Store) ROMFile(root string, g Game) string {
	if rest, ok := strings.CutPrefix(g.Path, Patched); ok {
		return filepath.Join(s.Dir, "roms", filepath.FromSlash(rest))
	}
	return filepath.Join(root, filepath.FromSlash(g.Path))
}

// scanAll lists the library's ROMs and the patched ones, with the patched
// ones' paths prefixed.
func (s *Store) scanAll(root string) ([]library.File, error) {
	files, err := library.Scan(root)
	if err != nil {
		return nil, err
	}
	patched, err := library.Scan(filepath.Join(s.Dir, "roms"))
	if err != nil {
		return nil, err
	}
	for _, f := range patched {
		f.Path = Patched + f.Path
		files = append(files, f)
	}
	return files, nil
}

func title(path string) string { return library.Title(strings.TrimPrefix(path, Patched)) }

// ScanResult counts what a library scan changed.
type ScanResult struct {
	Added   int `json:"added"`
	Updated int `json:"updated"`
	Missing int `json:"missing"`
	Total   int `json:"total"`
}

// Scan brings the games table in line with the ROMs under root. A ROM
// keeps its ID (and so its saves) while its path stays the same; a ROM that
// was renamed or moved is recognised by its checksum. ROMs that disappear
// are marked missing, never deleted, so their saves stay.
func (s *Store) Scan(root string) (ScanResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var res ScanResult
	files, err := s.scanAll(root)
	if err != nil {
		return res, err
	}
	type known struct {
		id          int64
		size, mtime int64
		sha1        string
		missing     bool
		seen        bool
	}
	byPath := map[string]*known{}
	rows, err := s.DB.Query("SELECT id, path, size, mtime, sha1, missing FROM games")
	if err != nil {
		return res, err
	}
	for rows.Next() {
		k := &known{}
		var path string
		if err = rows.Scan(&k.id, &path, &k.size, &k.mtime, &k.sha1, &k.missing); err != nil {
			rows.Close()
			return res, err
		}
		byPath[path] = k
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return res, err
	}
	now := stamp(s.Now())
	var added []library.File
	for _, f := range files {
		k := byPath[f.Path]
		if k == nil {
			added = append(added, f)
			continue
		}
		k.seen = true
		if k.size == f.Size && k.mtime == f.MTime && !k.missing {
			continue
		}
		sum := k.sha1
		if k.size != f.Size || k.mtime != f.MTime {
			if sum, err = library.Hash(s.ROMFile(root, Game{Path: f.Path})); err != nil {
				slog.Warn("could not read ROM", "path", f.Path, "err", err)
				continue
			}
		}
		if _, err = s.DB.Exec("UPDATE games SET size=?, mtime=?, sha1=?, missing=0 WHERE id=?",
			f.Size, f.MTime, sum, k.id); err != nil {
			return res, err
		}
		res.Updated++
	}
	// Gone from their path: candidates for a rename.
	gone := map[string]*known{}
	for path, k := range byPath {
		if !k.seen {
			gone[path] = k
		}
	}
	for _, f := range added {
		sum, err := library.Hash(s.ROMFile(root, Game{Path: f.Path}))
		if err != nil {
			slog.Warn("could not read ROM", "path", f.Path, "err", err)
			continue
		}
		renamed := ""
		for path, k := range gone {
			if k.sha1 == sum {
				renamed = path
				break
			}
		}
		if renamed != "" {
			k := gone[renamed]
			delete(gone, renamed)
			_, err = s.DB.Exec("UPDATE games SET path=?, title=?, size=?, mtime=?, missing=0 WHERE id=?",
				f.Path, title(f.Path), f.Size, f.MTime, k.id)
			res.Updated++
		} else {
			_, err = s.DB.Exec("INSERT INTO games(path, title, size, mtime, sha1, added) VALUES(?,?,?,?,?,?)",
				f.Path, title(f.Path), f.Size, f.MTime, sum, now)
			res.Added++
		}
		if err != nil {
			return res, err
		}
	}
	for _, k := range gone {
		if k.missing {
			continue
		}
		if _, err = s.DB.Exec("UPDATE games SET missing=1 WHERE id=?", k.id); err != nil {
			return res, err
		}
		res.Missing++
	}
	err = s.DB.QueryRow("SELECT COUNT(*) FROM games WHERE missing=0").Scan(&res.Total)
	return res, err
}

const gameColumns = "id, path, title, size, sha1, missing, added, last_played, play_seconds, notes, save_type, rtc, hidden"

func scanGame(row interface{ Scan(...any) error }) (Game, error) {
	var g Game
	err := row.Scan(&g.ID, &g.Path, &g.Title, &g.Size, &g.SHA1, &g.Missing, &g.Added, &g.LastPlayed, &g.PlaySeconds, &g.Notes,
		&g.SaveType, &g.RTC, &g.Hidden)
	return g, err
}

// Games lists the library, most recently played first, then by title. Games
// whose ROM is missing are included only when they have saves.
func (s *Store) Games() ([]Game, error) {
	rows, err := s.DB.Query("SELECT " + gameColumns + ` FROM games
		WHERE missing=0 OR EXISTS(SELECT 1 FROM saves WHERE game_id=games.id)
		ORDER BY last_played DESC, title COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	games := []Game{}
	for rows.Next() {
		g, err := scanGame(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		games = append(games, g)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, err
	}
	latest, err := s.latestSaves()
	if err != nil {
		return nil, err
	}
	for i := range games {
		games[i].Save = latest[games[i].ID]
	}
	return games, nil
}

// Game returns one game with its latest save.
func (s *Store) Game(id int64) (Game, error) {
	g, err := scanGame(s.DB.QueryRow("SELECT "+gameColumns+" FROM games WHERE id=?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return g, ErrNotFound
	}
	if err != nil {
		return g, err
	}
	g.Save, err = s.Latest(id)
	return g, err
}

// SetNotes replaces a game's notes.
func (s *Store) SetNotes(id int64, notes string) error {
	return s.updateGame("UPDATE games SET notes=? WHERE id=?", notes, id)
}

// MaxPlayReport caps one play-time report, so a stuck client can't inflate
// the total.
const MaxPlayReport = 300

// AddPlay adds seconds of play time and marks the game played now.
func (s *Store) AddPlay(id, seconds int64) error {
	seconds = max(0, min(seconds, MaxPlayReport))
	return s.updateGame("UPDATE games SET play_seconds=play_seconds+?, last_played=? WHERE id=?",
		seconds, stamp(s.Now()), id)
}

// SaveTypes are the save types a game can be set to, as mGBA names them.
var SaveTypes = []string{"SRAM", "FLASH512", "FLASH1M", "EEPROM", "EEPROM512", "NONE"}

// ErrInvalidSetting is returned for a save type or RTC value that isn't one.
var ErrInvalidSetting = errors.New("unknown save type or clock setting")

// SetOverrides sets a game's save type and real-time clock; "" means
// mGBA's own detection. rtc is "", "on" or "off".
func (s *Store) SetOverrides(id int64, saveType, rtc string) error {
	if saveType != "" && !slices.Contains(SaveTypes, saveType) || rtc != "" && rtc != "on" && rtc != "off" {
		return ErrInvalidSetting
	}
	return s.updateGame("UPDATE games SET save_type=?, rtc=? WHERE id=?", saveType, rtc, id)
}

// SetHidden hides a game from the library, or shows it again.
func (s *Store) SetHidden(id int64, hidden bool) error {
	return s.updateGame("UPDATE games SET hidden=? WHERE id=?", hidden, id)
}

func (s *Store) updateGame(query string, args ...any) error {
	res, err := s.DB.Exec(query, args...)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// Titles maps every game's ID to its title, for matching save files.
func (s *Store) Titles() (map[int64]string, error) {
	rows, err := s.DB.Query("SELECT id, title FROM games")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	titles := map[int64]string{}
	for rows.Next() {
		var id int64
		var title string
		if err = rows.Scan(&id, &title); err != nil {
			return nil, err
		}
		titles[id] = title
	}
	return titles, rows.Err()
}
