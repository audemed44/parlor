package store

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
)

// MaxCoverSize is the largest cover image accepted. The app shrinks them
// before uploading, so this is generous.
const MaxCoverSize = 5 << 20

// CoverTypes are the image types a cover may be.
var CoverTypes = []string{"image/png", "image/jpeg", "image/webp", "image/gif"}

// ErrInvalidCover is returned for a cover that isn't a PNG, JPEG, WebP or
// GIF image.
var ErrInvalidCover = errors.New("a cover must be a PNG, JPEG, WebP or GIF image of at most 5 MB")

func (s *Store) coverPath(id int64) string {
	return filepath.Join(s.Dir, "covers", strconv.FormatInt(id, 10))
}

// SetCover stores a game's cover image, replacing any before it.
func (s *Store) SetCover(id int64, data []byte) (Game, error) {
	if len(data) == 0 || len(data) > MaxCoverSize || !slices.Contains(CoverTypes, http.DetectContentType(data)) {
		return Game{}, ErrInvalidCover
	}
	if _, err := s.Game(id); err != nil {
		return Game{}, err
	}
	path := s.coverPath(id)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return Game{}, err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return Game{}, err
	}
	sum := sha256.Sum256(data)
	if err := s.updateGame("UPDATE games SET cover=? WHERE id=?", hex.EncodeToString(sum[:6]), id); err != nil {
		return Game{}, err
	}
	return s.Game(id)
}

// CoverData reads a game's cover, with its type.
func (s *Store) CoverData(id int64) ([]byte, string, error) {
	data, err := os.ReadFile(s.coverPath(id))
	if errors.Is(err, os.ErrNotExist) {
		return nil, "", ErrNotFound
	}
	if err != nil {
		return nil, "", err
	}
	return data, http.DetectContentType(data), nil
}

// RemoveCover goes back to the text cover.
func (s *Store) RemoveCover(id int64) error {
	if err := s.updateGame("UPDATE games SET cover='' WHERE id=?", id); err != nil {
		return err
	}
	if err := os.Remove(s.coverPath(id)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
