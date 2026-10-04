package store

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// Save states are snapshots of the whole machine, taken by the emulator
// (mGBA for the GBA, a RetroArch core for the rest). Each game
// has QuickSlots slots you save to by hand, and slot 0, the state taken
// when you leave the game, to carry on from there on any device. Saving to
// a slot replaces what was in it; only in-game saves keep a history.
const (
	AutoSlot   = 0
	QuickSlots = 4
	// MaxStateSize fits a DS state (~6.5 MiB) with its screenshot; mGBA's
	// GBA states are ~400 KiB.
	MaxStateSize = 16 << 20
	// MaxStreamedStateSize fits a 3DS state (~22 MiB) with its screenshot.
	// They're written to disk as they arrive, never held in memory.
	MaxStreamedStateSize = 64 << 20
)

// citraState starts the 3DS core's (Azahar's) states.
var citraState = []byte("CST\x1b")

// ErrInvalidState is returned for data that isn't a save state the game's
// emulator can load.
var ErrInvalidState = errors.New("this isn't a save state for this game's emulator")

// ErrInvalidSlot is returned for a slot outside 0..QuickSlots.
var ErrInvalidSlot = fmt.Errorf("slots are 0 (when you left) to %d", QuickSlots)

// State is the save state in one of a game's slots.
type State struct {
	ID      int64  `json:"id"`
	GameID  int64  `json:"game_id"`
	Slot    int    `json:"slot"`
	Created string `json:"created"`
	Size    int64  `json:"size"`
	SHA256  string `json:"sha256"`
	Device  string `json:"device"`
	// Note says where an imported state came from.
	Note string `json:"note"`
	// Image is whether the state carries a screenshot (mGBA writes states
	// as PNGs of the screen, and Parlor wraps RetroArch's the same way;
	// other frontends write the bare state).
	Image bool `json:"image"`
}

var pngMagic = []byte("\x89PNG\r\n\x1a\n")

// gbaStateMagic is the first word of mGBA's GBA state, little-endian; the
// low byte is the state version.
const gbaStateMagic = 0x01000000

// raState starts RetroArch's states, which EmulatorJS (and so RomM's
// player) writes.
var raState = []byte("RASTATE")

// NormalizeState checks a state is one the platform's emulator can load,
// unwrapping gzip (some frontends compress their states). PNG states are
// a screenshot with the state in a chunk: mGBA's own, or RetroArch's
// wrapped by Parlor.
func NormalizeState(platform string, data []byte) ([]byte, error) {
	if len(data) > 2 && data[0] == 0x1f && data[1] == 0x8b {
		r, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			return nil, ErrInvalidState
		}
		data, err = io.ReadAll(io.LimitReader(r, MaxStateSize+1))
		if err != nil {
			return nil, ErrInvalidState
		}
	}
	if len(data) > MaxStateSize {
		return nil, ErrInvalidState
	}
	if bytes.HasPrefix(data, pngMagic) {
		return data, nil
	}
	if platform == "3ds" {
		if !bytes.HasPrefix(data, citraState) {
			return nil, ErrInvalidState
		}
		return data, nil
	}
	if platform != "gba" {
		if !bytes.HasPrefix(data, raState) {
			return nil, ErrInvalidState
		}
		return data, nil
	}
	if len(data) < 0x400 {
		return nil, ErrInvalidState
	}
	magic := uint32(data[0]) | uint32(data[1])<<8 | uint32(data[2])<<16 | uint32(data[3])<<24
	if magic&^0xff != gbaStateMagic {
		return nil, ErrInvalidState
	}
	return data, nil
}

const stateColumns = "id, game_id, slot, created, size, sha256, device, note"

func (s *Store) statePath(gameID int64, slot int) string {
	return filepath.Join(s.Dir, "states", strconv.FormatInt(gameID, 10), strconv.Itoa(slot)+".ss")
}

func (s *Store) scanState(row interface{ Scan(...any) error }) (State, error) {
	var v State
	err := row.Scan(&v.ID, &v.GameID, &v.Slot, &v.Created, &v.Size, &v.SHA256, &v.Device, &v.Note)
	if err != nil {
		return v, err
	}
	if f, err := os.Open(s.statePath(v.GameID, v.Slot)); err == nil {
		head := make([]byte, len(pngMagic))
		_, err = io.ReadFull(f, head)
		v.Image = err == nil && bytes.Equal(head, pngMagic)
		f.Close()
	}
	return v, nil
}

// States lists a game's filled slots, by slot.
func (s *Store) States(gameID int64) ([]State, error) {
	rows, err := s.DB.Query("SELECT "+stateColumns+" FROM states WHERE game_id=? ORDER BY slot", gameID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []State{}
	for rows.Next() {
		v, err := s.scanState(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// StateIn returns the state in a slot.
func (s *Store) StateIn(gameID int64, slot int) (State, error) {
	v, err := s.scanState(s.DB.QueryRow("SELECT "+stateColumns+" FROM states WHERE game_id=? AND slot=?", gameID, slot))
	if errors.Is(err, sql.ErrNoRows) {
		return v, ErrNotFound
	}
	return v, err
}

// StateData reads a state's bytes.
func (s *Store) StateData(v State) ([]byte, error) {
	return os.ReadFile(s.statePath(v.GameID, v.Slot))
}

// OpenState opens a state's file, to send it without holding it in memory
// (a DS state is 6.5 MiB).
func (s *Store) OpenState(v State) (*os.File, error) {
	return os.Open(s.statePath(v.GameID, v.Slot))
}

// NewState describes a state being put in a slot.
type NewState struct {
	Data   []byte
	Device string
	Note   string
	// Created backdates it (imports use the file's time); zero means now.
	Created time.Time
	// IfNewer keeps the slot's state when it's newer than this one: a
	// state that waited offline doesn't replace a later one.
	IfNewer bool
}

// PutState puts a state in a slot, replacing what was there.
func (s *Store) PutState(gameID int64, slot int, n NewState) (State, error) {
	if slot < 0 || slot > QuickSlots {
		return State{}, ErrInvalidSlot
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	g, err := s.Game(gameID)
	if err != nil {
		return State{}, err
	}
	data, err := NormalizeState(g.Platform, n.Data)
	if err != nil {
		return State{}, err
	}
	created := n.Created
	if created.IsZero() || created.After(s.Now()) {
		created = s.Now()
	}
	if n.IfNewer {
		old, err := s.StateIn(gameID, slot)
		if err == nil && old.Created >= stamp(created) {
			return old, nil
		}
	}
	sum := sha256.Sum256(data)
	path := s.statePath(gameID, slot)
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return State{}, err
	}
	tmp := path + ".tmp"
	if err = os.WriteFile(tmp, data, 0600); err != nil {
		return State{}, err
	}
	if err = os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return State{}, err
	}
	_, err = s.DB.Exec(`INSERT INTO states(game_id, slot, created, size, sha256, device, note) VALUES(?,?,?,?,?,?,?)
		ON CONFLICT(game_id, slot) DO UPDATE SET created=excluded.created, size=excluded.size,
		sha256=excluded.sha256, device=excluded.device, note=excluded.note`,
		gameID, slot, stamp(created), len(data), hex.EncodeToString(sum[:]), n.Device, n.Note)
	if err != nil {
		return State{}, err
	}
	return s.StateIn(gameID, slot)
}

// PutStateFrom puts a streamed console's state (3DS) in a slot, writing it
// to disk as it's read: these states are too big to hold in memory. It
// must be a bare state or one wrapped in a PNG.
func (s *Store) PutStateFrom(gameID int64, slot int, r io.Reader, n NewState) (State, error) {
	if slot < 0 || slot > QuickSlots {
		return State{}, ErrInvalidSlot
	}
	g, err := s.Game(gameID)
	if err != nil {
		return State{}, err
	}
	head := make([]byte, len(pngMagic))
	if _, err := io.ReadFull(r, head); err != nil {
		return State{}, ErrInvalidState
	}
	if !bytes.Equal(head, pngMagic) {
		if _, err := NormalizeState(g.Platform, head); err != nil {
			return State{}, err
		}
	}
	path := s.statePath(gameID, slot)
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return State{}, err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "upload-*")
	if err != nil {
		return State{}, err
	}
	defer os.Remove(tmp.Name())
	h := sha256.New()
	size, err := io.Copy(io.MultiWriter(tmp, h), io.LimitReader(io.MultiReader(bytes.NewReader(head), r), MaxStreamedStateSize+1))
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return State{}, err
	}
	if size > MaxStreamedStateSize {
		return State{}, ErrInvalidState
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	created := n.Created
	if created.IsZero() || created.After(s.Now()) {
		created = s.Now()
	}
	if n.IfNewer {
		old, err := s.StateIn(gameID, slot)
		if err == nil && old.Created >= stamp(created) {
			return old, nil
		}
	}
	if err = os.Chmod(tmp.Name(), 0600); err != nil {
		return State{}, err
	}
	if err = os.Rename(tmp.Name(), path); err != nil {
		return State{}, err
	}
	_, err = s.DB.Exec(`INSERT INTO states(game_id, slot, created, size, sha256, device, note) VALUES(?,?,?,?,?,?,?)
		ON CONFLICT(game_id, slot) DO UPDATE SET created=excluded.created, size=excluded.size,
		sha256=excluded.sha256, device=excluded.device, note=excluded.note`,
		gameID, slot, stamp(created), size, hex.EncodeToString(h.Sum(nil)), n.Device, n.Note)
	if err != nil {
		return State{}, err
	}
	return s.StateIn(gameID, slot)
}

// DeleteState empties a slot.
func (s *Store) DeleteState(gameID int64, slot int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	res, err := s.DB.Exec("DELETE FROM states WHERE game_id=? AND slot=?", gameID, slot)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	if err = os.Remove(s.statePath(gameID, slot)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
