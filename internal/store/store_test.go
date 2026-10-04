package store

import (
	"bytes"
	"compress/gzip"
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

	all, err := s.Candidates(imports)
	if err != nil || len(all) != 3 {
		t.Fatalf("candidates: %+v %v", all, err)
	}
	var cands []Candidate
	for _, c := range all {
		if c.Kind == "save" {
			cands = append(cands, c)
		}
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
	all, _ = s.Candidates(imports)
	cands = cands[:0]
	for _, c := range all {
		if c.Kind == "save" {
			cands = append(cands, c)
		}
		if c.Kind == "save" && c.Imported == 0 {
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

// fakeState is shaped like a bare mGBA GBA state (version 9), as RomM's
// web player stores them.
func fakeState(fill byte) []byte {
	data := bytes.Repeat([]byte{fill}, 0x61000)
	copy(data, []byte{0x09, 0, 0, 0x01})
	return data
}

func TestStates(t *testing.T) {
	s := open(t)
	root, imports := t.TempDir(), t.TempDir()
	write(t, root, "Pokémon Heart and Soul (v2.0.4).gba", "hns")
	s.Scan(root)
	if _, err := s.PutState(1, 1, NewState{Data: []byte("not a state")}); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("junk: %v", err)
	}
	if _, err := s.PutState(1, QuickSlots+1, NewState{Data: fakeState(1)}); !errors.Is(err, ErrInvalidSlot) {
		t.Fatalf("slot: %v", err)
	}
	png := append([]byte("\x89PNG\r\n\x1a\n"), "image with a state chunk"...)
	if _, err := s.PutState(1, AutoSlot, NewState{Data: png, Device: "iPhone"}); err != nil {
		t.Fatal(err)
	}
	// Compressed states are stored unwrapped.
	var gz bytes.Buffer
	zw := gzip.NewWriter(&gz)
	zw.Write(fakeState(2))
	zw.Close()
	v, err := s.PutState(1, 2, NewState{Data: gz.Bytes(), Device: "Mac"})
	if err != nil || v.Size != 0x61000 || v.Image {
		t.Fatalf("gzip: %+v %v", v, err)
	}
	// Saving to a slot again replaces it.
	if v, err = s.PutState(1, 2, NewState{Data: fakeState(3), Device: "iPhone"}); err != nil || v.Device != "iPhone" {
		t.Fatalf("replace: %+v %v", v, err)
	}
	data, _ := s.StateData(v)
	states, _ := s.States(1)
	if len(states) != 2 || !states[0].Image || states[1].Slot != 2 || data[0x100] != 3 {
		t.Fatalf("states: %+v", states)
	}
	// A state that waited offline doesn't replace a newer one.
	auto, _ := s.StateIn(1, AutoSlot)
	stale := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	if v, err = s.PutState(1, AutoSlot, NewState{Data: fakeState(5), Created: stale, IfNewer: true}); err != nil || v.SHA256 != auto.SHA256 {
		t.Fatalf("stale: %+v %v", v, err)
	}
	if v, err = s.PutState(1, AutoSlot, NewState{Data: fakeState(5), Created: s.Now(), IfNewer: true}); err != nil || v.SHA256 == auto.SHA256 {
		t.Fatalf("newer: %+v %v", v, err)
	}
	if err = s.DeleteState(1, 2); err != nil {
		t.Fatal(err)
	}
	if _, err = s.StateIn(1, 2); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted: %v", err)
	}

	// RomM's states import into a slot, dated by the file.
	write(t, imports, "users/1/states/gba/22/mgba/Pokemon Heart and Soul [2026-09-13 19-00-02].state", string(fakeState(4)))
	cands, _ := s.Candidates(imports)
	if len(cands) != 1 || cands[0].Kind != "state" || cands[0].Suggested != 1 {
		t.Fatalf("candidates: %+v", cands)
	}
	if v, err = s.ImportState(imports, cands[0].Path, 1, 3); err != nil || v.Slot != 3 || v.Device != "Import" {
		t.Fatalf("import: %+v %v", v, err)
	}
	if cands, _ = s.Candidates(imports); cands[0].Imported != 1 {
		t.Fatalf("not marked imported: %+v", cands)
	}
}
