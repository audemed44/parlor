package store

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func open(t *testing.T) *Store {
	t.Helper()
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	clock := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	s.Now = func() time.Time {
		clock = clock.Add(time.Minute)
		return clock
	}
	return s
}

func write(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, rel)
	os.MkdirAll(filepath.Dir(path), 0755)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestScanKeepsIDsAcrossRenames(t *testing.T) {
	s := open(t)
	root := t.TempDir()
	write(t, root, "Unbound.gba", "unbound")
	write(t, root, "Emerald.gba", "emerald")
	res, err := s.Scan(root)
	if err != nil || res.Added != 2 || res.Total != 2 {
		t.Fatalf("first scan: %+v %v", res, err)
	}
	games, _ := s.Games()
	ids := map[string]int64{}
	for _, g := range games {
		ids[g.Title] = g.ID
	}
	os.Rename(filepath.Join(root, "Unbound.gba"), filepath.Join(root, "Pokemon Unbound.gba"))
	os.Remove(filepath.Join(root, "Emerald.gba"))
	if res, err = s.Scan(root); err != nil || res.Added != 0 || res.Updated != 1 || res.Missing != 1 || res.Total != 1 {
		t.Fatalf("rescan: %+v %v", res, err)
	}
	g, err := s.Game(ids["Unbound"])
	if err != nil || g.Title != "Pokemon Unbound" || g.Missing {
		t.Fatalf("renamed game: %+v %v", g, err)
	}
	// A missing game without saves is hidden; with a save it's listed.
	games, _ = s.Games()
	if len(games) != 1 {
		t.Fatalf("games: %+v", games)
	}
	if _, err = s.AddSave(ids["Emerald"], NewSave{Data: []byte("x"), Source: "upload"}); err != nil {
		t.Fatal(err)
	}
	games, _ = s.Games()
	if len(games) != 2 {
		t.Fatalf("games with missing save: %+v", games)
	}
	write(t, root, "Emerald.gba", "emerald")
	if res, _ = s.Scan(root); res.Updated != 1 || res.Total != 2 {
		t.Fatalf("back again: %+v", res)
	}
}

func TestSaveConflictsAndHistory(t *testing.T) {
	s := open(t)
	root := t.TempDir()
	write(t, root, "Game.gba", "rom")
	s.Scan(root)
	games, _ := s.Games()
	id := games[0].ID

	first, err := s.AddSave(id, NewSave{Data: []byte("one"), Source: "play", Device: "iPhone"})
	if err != nil {
		t.Fatal(err)
	}
	// Same bytes again: no new version.
	same, err := s.AddSave(id, NewSave{Data: []byte("one"), Source: "play", Base: first.ID})
	if err != nil || same.ID != first.ID {
		t.Fatalf("same save: %+v %v", same, err)
	}
	second, err := s.AddSave(id, NewSave{Data: []byte("two"), Source: "play", Device: "Desktop", Base: first.ID})
	if err != nil {
		t.Fatal(err)
	}
	// The phone still thinks the first save is the latest.
	_, err = s.AddSave(id, NewSave{Data: []byte("three"), Source: "play", Base: first.ID})
	var conflict *Conflict
	if !errors.As(err, &conflict) || conflict.Latest.ID != second.ID {
		t.Fatalf("want conflict with %d, got %v", second.ID, err)
	}
	if _, err = s.AddSave(id, NewSave{Data: []byte("three"), Source: "play", Base: first.ID, Force: true}); err != nil {
		t.Fatal(err)
	}
	restored, err := s.Restore(first.ID, "Desktop")
	if err != nil || restored.Source != "restore" {
		t.Fatalf("restore: %+v %v", restored, err)
	}
	data, _ := s.SaveData(restored)
	if string(data) != "one" {
		t.Fatalf("restored data %q", data)
	}
	versions, _ := s.Saves(id)
	if len(versions) != 4 || versions[0].ID != restored.ID {
		t.Fatalf("versions: %+v", versions)
	}
	g, _ := s.Game(id)
	if g.Save == nil || g.Save.ID != restored.ID {
		t.Fatalf("latest: %+v", g.Save)
	}
	if _, err = s.AddSave(id, NewSave{Source: "play", Force: true}); err == nil {
		t.Fatal("empty save accepted")
	}
	if _, err = s.AddSave(999, NewSave{Data: []byte("x"), Force: true}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown game: %v", err)
	}
}

func TestPruneKeepsRecentAndDaily(t *testing.T) {
	s := open(t)
	s.Keep = 3
	s.KeepDays = 30
	root := t.TempDir()
	write(t, root, "Game.gba", "rom")
	s.Scan(root)
	games, _ := s.Games()
	id := games[0].ID
	// Five saves a day apart, then five more within the same day.
	base := time.Date(2026, 9, 20, 12, 0, 0, 0, time.Local)
	for i := range 5 {
		if _, err := s.AddSave(id, NewSave{Data: []byte{byte(i)}, Force: true, Created: base.AddDate(0, 0, i)}); err != nil {
			t.Fatal(err)
		}
	}
	for i := range 5 {
		if _, err := s.AddSave(id, NewSave{Data: []byte{byte(10 + i)}, Force: true, Created: base.AddDate(0, 0, 5).Add(time.Duration(i) * time.Minute)}); err != nil {
			t.Fatal(err)
		}
	}
	versions, _ := s.Saves(id)
	// 3 newest (day 5) + the newest of days 4, 3, 2, 1, 0.
	if len(versions) != 8 {
		t.Fatalf("kept %d versions: %+v", len(versions), versions)
	}
	files, _ := os.ReadDir(filepath.Join(s.Dir, "saves", "1"))
	if len(files) != 8 {
		t.Fatalf("%d files on disk", len(files))
	}
}

func TestImport(t *testing.T) {
	s := open(t)
	root, imports := t.TempDir(), t.TempDir()
	write(t, root, "Pokémon Heart and Soul (v2.0.4).gba", "hns")
	write(t, root, "Pokemon - FireRed Version (USA, Europe).gba", "fr")
	s.Scan(root)
	write(t, imports, "retrodeck-saves/roms/Pokemon Heart and Soul.srm", "deck save")
	write(t, imports, "users/1/saves/gba/2/mgba/Pokemon - FireRed Version (USA, Europe) [2025-11-24 12-35-32-901].srm", "web save")
	write(t, imports, "users/1/states/gba/22/mgba/Pokemon Heart and Soul [x].state", "state")
	old := time.Date(2025, 11, 24, 12, 0, 0, 0, time.UTC)
	os.Chtimes(filepath.Join(imports, "retrodeck-saves/roms/Pokemon Heart and Soul.srm"), old, old)

	cands, err := s.Candidates(imports)
	if err != nil || len(cands) != 2 {
		t.Fatalf("candidates: %+v %v", cands, err)
	}
	titles, _ := s.Titles()
	for _, c := range cands {
		if c.Suggested == 0 || c.Imported != 0 {
			t.Fatalf("candidate %+v", c)
		}
		if _, err = s.Import(imports, c.Path, c.Suggested); err != nil {
			t.Fatal(err)
		}
		g, _ := s.Game(c.Suggested)
		if g.Save == nil || g.Save.Source != "import" {
			t.Fatalf("%s: %+v", titles[c.Suggested], g.Save)
		}
	}
	// Dated by the file, so the deck save is from 2025.
	cands, _ = s.Candidates(imports)
	for _, c := range cands {
		if c.Imported == 0 {
			t.Fatalf("not marked imported: %+v", c)
		}
	}
	// Importing again adds nothing.
	if _, err = s.Import(imports, cands[0].Path, cands[0].Imported); err != nil {
		t.Fatal(err)
	}
	versions, _ := s.Saves(cands[0].Imported)
	if len(versions) != 1 {
		t.Fatalf("re-import added a version: %+v", versions)
	}
	if _, err = s.Import(imports, "../escape.srm", 1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("escape: %v", err)
	}
}

func TestPlayTime(t *testing.T) {
	s := open(t)
	root := t.TempDir()
	write(t, root, "Game.gba", "rom")
	s.Scan(root)
	s.AddPlay(1, 60)
	s.AddPlay(1, 100000)
	g, _ := s.Game(1)
	if g.PlaySeconds != 60+MaxPlayReport || g.LastPlayed == "" {
		t.Fatalf("%+v", g)
	}
	if err := s.AddPlay(2, 1); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}
