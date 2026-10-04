package server

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"hash/crc32"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/audemed44/parlor/internal/store"
	"github.com/audemed44/parlor/internal/testrom"
)

const token = "test-token-0123456789"

type harness struct {
	t    *testing.T
	srv  *httptest.Server
	roms string
}

func setup(t *testing.T) *harness {
	t.Helper()
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	roms, imports := t.TempDir(), t.TempDir()
	os.WriteFile(filepath.Join(roms, "Parlor Test.gba"), testrom.ROM(), 0644)
	os.MkdirAll(filepath.Join(imports, "retrodeck"), 0755)
	os.WriteFile(filepath.Join(imports, "retrodeck", "Parlor Test.srm"), []byte("deck"), 0644)
	if _, err = db.Scan(roms); err != nil {
		t.Fatal(err)
	}
	s := &Server{
		Store: db, ROMs: roms, ImportDir: imports, Token: token,
		Files: fstest.MapFS{"index.html": {Data: []byte("<!doctype html>")}},
	}
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)
	return &harness{t, srv, roms}
}

// do sends a request with the bearer token; body may be a []byte (raw) or
// anything else (JSON).
func (h *harness) do(method, path string, body any) (*http.Response, []byte) {
	h.t.Helper()
	var r io.Reader
	ctype := ""
	switch b := body.(type) {
	case nil:
	case []byte:
		r, ctype = bytes.NewReader(b), "application/octet-stream"
	default:
		data, _ := json.Marshal(b)
		r, ctype = bytes.NewReader(data), "application/json"
	}
	req, _ := http.NewRequest(method, h.srv.URL+path, r)
	req.Header.Set("Authorization", "Bearer "+token)
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return resp, data
}

func TestAuthAndHeaders(t *testing.T) {
	h := setup(t)
	resp, err := http.Get(h.srv.URL + "/api/games")
	if err != nil || resp.StatusCode != 401 {
		t.Fatalf("unauthenticated: %v %v", resp.StatusCode, err)
	}
	for k, want := range map[string]string{
		"Cross-Origin-Opener-Policy":   "same-origin",
		"Cross-Origin-Embedder-Policy": "require-corp",
	} {
		if got := resp.Header.Get(k); got != want {
			t.Errorf("%s = %q", k, got)
		}
	}
	// Signing in sets a session cookie that then works on its own.
	resp, err = http.Post(h.srv.URL+"/api/login", "application/json", strings.NewReader(`{"token":"`+token+`"}`))
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("login: %v %v", resp.StatusCode, err)
	}
	cookie := resp.Cookies()[0]
	req, _ := http.NewRequest("GET", h.srv.URL+"/api/games", nil)
	req.AddCookie(cookie)
	if resp, _ = http.DefaultClient.Do(req); resp.StatusCode != 200 {
		t.Fatalf("with cookie: %d", resp.StatusCode)
	}
	resp, _ = http.Post(h.srv.URL+"/api/login", "application/json", strings.NewReader(`{"token":"wrong"}`))
	if resp.StatusCode != 401 {
		t.Fatalf("wrong token: %d", resp.StatusCode)
	}
	// Writes from another site are refused even with the cookie.
	req, _ = http.NewRequest("PUT", h.srv.URL+"/api/games/1/save", strings.NewReader("x"))
	req.AddCookie(cookie)
	req.Header.Set("Origin", "https://evil.example")
	if resp, _ = http.DefaultClient.Do(req); resp.StatusCode != 403 {
		t.Fatalf("cross-origin: %d", resp.StatusCode)
	}
}

func TestPlaySaveFlow(t *testing.T) {
	h := setup(t)
	resp, body := h.do("GET", "/api/games", nil)
	var games []store.Game
	json.Unmarshal(body, &games)
	if resp.StatusCode != 200 || len(games) != 1 || games[0].Title != "Parlor Test" || games[0].Save != nil {
		t.Fatalf("games: %s", body)
	}
	if resp, _ = h.do("GET", "/api/games/1/save", nil); resp.StatusCode != 404 {
		t.Fatalf("no save yet: %d", resp.StatusCode)
	}
	resp, body = h.do("GET", "/api/games/1/rom", nil)
	if resp.StatusCode != 200 || !bytes.Equal(body, testrom.ROM()) || resp.Header.Get("ETag") == "" {
		t.Fatalf("rom: %d %d bytes", resp.StatusCode, len(body))
	}

	// The phone's first save, then the desktop's, which started from it.
	resp, body = h.do("PUT", "/api/games/1/save?base=0&device=iPhone", []byte("phone"))
	var phone store.Save
	json.Unmarshal(body, &phone)
	if resp.StatusCode != 200 || phone.ID == 0 || phone.Device != "iPhone" {
		t.Fatalf("first save: %d %s", resp.StatusCode, body)
	}
	resp, body = h.do("PUT", "/api/games/1/save?device=Desktop&base="+itoa(phone.ID), []byte("desktop"))
	if resp.StatusCode != 200 {
		t.Fatalf("second save: %d %s", resp.StatusCode, body)
	}
	// The phone, still on its first save, gets a conflict.
	resp, body = h.do("PUT", "/api/games/1/save?device=iPhone&base="+itoa(phone.ID), []byte("phone again"))
	if resp.StatusCode != 409 || !strings.Contains(string(body), `"device":"Desktop"`) {
		t.Fatalf("conflict: %d %s", resp.StatusCode, body)
	}
	if resp, body = h.do("PUT", "/api/games/1/save?force=1&base="+itoa(phone.ID), []byte("phone again")); resp.StatusCode != 200 {
		t.Fatalf("forced: %d %s", resp.StatusCode, body)
	}
	resp, body = h.do("GET", "/api/games/1/save", nil)
	if string(body) != "phone again" || resp.Header.Get("X-Save-Id") == "" {
		t.Fatalf("latest: %s", body)
	}

	// Restore the phone's first save from the history.
	if resp, body = h.do("POST", "/api/saves/"+itoa(phone.ID)+"/restore", map[string]string{"device": "Desktop"}); resp.StatusCode != 200 {
		t.Fatalf("restore: %d %s", resp.StatusCode, body)
	}
	resp, body = h.do("GET", "/api/saves/"+itoa(phone.ID), nil)
	if string(body) != "phone" || !strings.Contains(resp.Header.Get("Content-Disposition"), "Parlor Test.srm") {
		t.Fatalf("download: %q %q", body, resp.Header.Get("Content-Disposition"))
	}
	resp, body = h.do("GET", "/api/games/1", nil)
	var detail struct {
		store.Game
		Saves []store.Save `json:"saves"`
	}
	json.Unmarshal(body, &detail)
	if len(detail.Saves) != 4 || detail.Save.Source != "restore" {
		t.Fatalf("detail: %s", body)
	}

	if resp, _ = h.do("PUT", "/api/games/1/save?force=1", []byte{}); resp.StatusCode != 400 {
		t.Fatalf("empty save: %d", resp.StatusCode)
	}
	if resp, _ = h.do("PUT", "/api/games/9/save?force=1", []byte("x")); resp.StatusCode != 404 {
		t.Fatalf("unknown game: %d", resp.StatusCode)
	}
	if resp, _ = h.do("POST", "/api/games/1/played", map[string]int{"seconds": 60}); resp.StatusCode != 200 {
		t.Fatalf("played: %d", resp.StatusCode)
	}
	if resp, _ = h.do("POST", "/api/games/1/notes", map[string]string{"notes": "Badge 3"}); resp.StatusCode != 200 {
		t.Fatalf("notes: %d", resp.StatusCode)
	}
	_, body = h.do("GET", "/api/games/1", nil)
	json.Unmarshal(body, &detail)
	if detail.PlaySeconds != 60 || detail.Notes != "Badge 3" {
		t.Fatalf("after play: %s", body)
	}
}

func TestUploadAndImport(t *testing.T) {
	h := setup(t)
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("file", "backup.srm")
	fw.Write([]byte("uploaded"))
	mw.WriteField("device", "Desktop")
	mw.Close()
	req, _ := http.NewRequest("POST", h.srv.URL+"/api/games/1/saves", &buf)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("upload: %v %v", resp.StatusCode, err)
	}

	resp, body := h.do("GET", "/api/imports", nil)
	var cands []store.Candidate
	json.Unmarshal(body, &cands)
	if resp.StatusCode != 200 || len(cands) != 1 || cands[0].Suggested != 1 || cands[0].Imported != 0 {
		t.Fatalf("candidates: %s", body)
	}
	if resp, body = h.do("POST", "/api/imports", map[string]any{"path": cands[0].Path, "game_id": 1}); resp.StatusCode != 200 {
		t.Fatalf("import: %d %s", resp.StatusCode, body)
	}
	_, body = h.do("GET", "/api/imports", nil)
	json.Unmarshal(body, &cands)
	if cands[0].Imported != 1 {
		t.Fatalf("after import: %s", body)
	}
	if resp, _ = h.do("POST", "/api/imports", map[string]any{"path": "../x.srm", "game_id": 1}); resp.StatusCode != 404 {
		t.Fatalf("escape: %d", resp.StatusCode)
	}
}

func TestSettings(t *testing.T) {
	h := setup(t)
	resp, body := h.do("GET", "/api/settings", nil)
	if resp.StatusCode != 200 || strings.TrimSpace(string(body)) != `{"fast_forward":2,"stream":false}` {
		t.Fatalf("default: %d %s", resp.StatusCode, body)
	}
	if resp, _ = h.do("POST", "/api/settings", map[string]int{"fast_forward": 5}); resp.StatusCode != 400 {
		t.Fatalf("5x accepted: %d", resp.StatusCode)
	}
	if resp, _ = h.do("POST", "/api/settings", map[string]int{"fast_forward": 4}); resp.StatusCode != 200 {
		t.Fatalf("4x: %d", resp.StatusCode)
	}
	_, body = h.do("GET", "/api/settings", nil)
	if strings.TrimSpace(string(body)) != `{"fast_forward":4,"stream":false}` {
		t.Fatalf("after: %s", body)
	}
}

func TestStates(t *testing.T) {
	h := setup(t)
	png := append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 64)...)
	resp, body := h.do("PUT", "/api/games/1/states/0?device=iPhone", png)
	if resp.StatusCode != 200 {
		t.Fatalf("put: %d %s", resp.StatusCode, body)
	}
	if resp, _ = h.do("PUT", "/api/games/1/states/9", png); resp.StatusCode != 400 {
		t.Fatalf("slot 9: %d", resp.StatusCode)
	}
	if resp, _ = h.do("PUT", "/api/games/1/states/1", []byte("junk")); resp.StatusCode != 400 {
		t.Fatalf("junk: %d", resp.StatusCode)
	}
	resp, body = h.do("GET", "/api/games/1/states/0", nil)
	if resp.StatusCode != 200 || !bytes.Equal(body, png) || resp.Header.Get("X-State-Id") == "" {
		t.Fatalf("get: %d", resp.StatusCode)
	}
	if resp, _ = h.do("GET", "/api/games/1/states/0/image", nil); resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "image/png" {
		t.Fatalf("image: %d", resp.StatusCode)
	}
	var detail struct {
		States []store.State `json:"states"`
	}
	_, body = h.do("GET", "/api/games/1", nil)
	json.Unmarshal(body, &detail)
	if len(detail.States) != 1 || detail.States[0].Device != "iPhone" || !detail.States[0].Image {
		t.Fatalf("detail: %s", body)
	}
	if resp, _ = h.do("DELETE", "/api/games/1/states/0", nil); resp.StatusCode != 200 {
		t.Fatalf("delete: %d", resp.StatusCode)
	}
	if resp, _ = h.do("GET", "/api/games/1/states/0", nil); resp.StatusCode != 404 {
		t.Fatalf("after delete: %d", resp.StatusCode)
	}
}

func TestGameSettings(t *testing.T) {
	h := setup(t)
	resp, body := h.do("POST", "/api/games/1/settings", map[string]any{"save_type": "FLASH1M", "rtc": "on"})
	var g store.Game
	json.Unmarshal(body, &g)
	if resp.StatusCode != 200 || g.SaveType != "FLASH1M" || g.RTC != "on" {
		t.Fatalf("set: %d %s", resp.StatusCode, body)
	}
	if resp, _ = h.do("POST", "/api/games/1/settings", map[string]any{"save_type": "CARD"}); resp.StatusCode != 400 {
		t.Fatalf("bad: %d", resp.StatusCode)
	}
}

// bpsFor is a BPS patch that turns source into target by writing every
// byte of the target.
func bpsFor(source, target []byte) []byte {
	num := func(v uint64) (out []byte) {
		for {
			x := byte(v & 0x7f)
			v >>= 7
			if v == 0 {
				return append(out, x|0x80)
			}
			out = append(out, x)
			v--
		}
	}
	p := append([]byte("BPS1"), num(uint64(len(source)))...)
	p = append(p, num(uint64(len(target)))...)
	p = append(p, num(0)...)
	p = append(p, num(uint64((len(target)-1)<<2|1))...)
	p = append(p, target...)
	p = binary.LittleEndian.AppendUint32(p, crc32.ChecksumIEEE(source))
	p = binary.LittleEndian.AppendUint32(p, crc32.ChecksumIEEE(target))
	return binary.LittleEndian.AppendUint32(p, crc32.ChecksumIEEE(p))
}

func (h *harness) form(path string, fields map[string]string, name string, file []byte) (*http.Response, []byte) {
	h.t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("file", name)
	fw.Write(file)
	for k, v := range fields {
		mw.WriteField(k, v)
	}
	mw.Close()
	req, _ := http.NewRequest("POST", h.srv.URL+path, &buf)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return resp, data
}

func TestPatch(t *testing.T) {
	h := setup(t)
	h.do("PUT", "/api/games/1/save?base=0&device=iPhone", []byte("old version's save"))
	h.do("POST", "/api/games/1/notes", map[string]string{"notes": "Gym 3"})
	v2 := append(testrom.ROM(), []byte("version 2")...)
	p := bpsFor(testrom.ROM(), v2)

	// The base ROM is found by the patch's checksum.
	resp, body := h.form("/api/patch", map[string]string{"title": "Parlor Test v2", "carry": "1", "hide": "1"}, "v2.bps", p)
	var g store.Game
	json.Unmarshal(body, &g)
	if resp.StatusCode != 200 || g.Title != "Parlor Test v2" || g.Notes != "Gym 3" || g.Save == nil || g.Save.Source != "carry" {
		t.Fatalf("patch: %d %s", resp.StatusCode, body)
	}
	resp, body = h.do("GET", "/api/games/"+itoa(g.ID)+"/rom", nil)
	if resp.StatusCode != 200 || !bytes.Equal(body, v2) {
		t.Fatalf("rom: %d %d bytes", resp.StatusCode, len(body))
	}
	resp, body = h.do("GET", "/api/games/"+itoa(g.ID)+"/save", nil)
	if string(body) != "old version's save" {
		t.Fatalf("carried save: %s", body)
	}
	var old store.Game
	_, body = h.do("GET", "/api/games/1", nil)
	json.Unmarshal(body, &old)
	if !old.Hidden {
		t.Fatalf("old version not hidden: %s", body)
	}
	// A rescan keeps the patched ROM.
	_, body = h.do("POST", "/api/library/scan", map[string]any{})
	if !strings.Contains(string(body), `"total":2`) || !strings.Contains(string(body), `"missing":0`) {
		t.Fatalf("rescan: %s", body)
	}
	if resp, _ = h.do("POST", "/api/games/1/settings", map[string]any{"save_type": "", "rtc": "", "hidden": false}); resp.StatusCode != 200 {
		t.Fatalf("unhide: %d", resp.StatusCode)
	}

	if resp, body = h.form("/api/patch", map[string]string{"title": "Again"}, "v2.bps", p); resp.StatusCode != 409 {
		t.Fatalf("duplicate: %d %s", resp.StatusCode, body)
	}
	if resp, _ = h.form("/api/patch", map[string]string{"title": "Other", "base": itoa(g.ID)}, "v2.bps", p); resp.StatusCode != 400 {
		t.Fatalf("wrong base: %d", resp.StatusCode)
	}
	ips := []byte("PATCH\x00\x00\x10\x00\x01XEOF")
	if resp, _ = h.form("/api/patch", map[string]string{"title": "IPS"}, "x.ips", ips); resp.StatusCode != 400 {
		t.Fatalf("ips without base: %d", resp.StatusCode)
	}
	if resp, body = h.form("/api/patch", map[string]string{"title": "IPS", "base": "1"}, "x.ips", ips); resp.StatusCode != 200 {
		t.Fatalf("ips: %d %s", resp.StatusCode, body)
	}
	if resp, _ = h.form("/api/patch", map[string]string{"title": "../escape", "base": "1"}, "x.ips", []byte("PATCH\x00\x00\x11\x00\x01YEOF")); resp.StatusCode != 400 {
		t.Fatalf("bad name: %d", resp.StatusCode)
	}
}

func TestCovers(t *testing.T) {
	h := setup(t)
	png := append([]byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR"), make([]byte, 64)...)
	if resp, _ := h.form("/api/games/1/cover", nil, "cover.txt", []byte("not an image")); resp.StatusCode != 400 {
		t.Fatalf("text accepted: %d", resp.StatusCode)
	}
	resp, body := h.form("/api/games/1/cover", nil, "cover.png", png)
	var g store.Game
	json.Unmarshal(body, &g)
	if resp.StatusCode != 200 || g.Cover == "" {
		t.Fatalf("upload: %d %s", resp.StatusCode, body)
	}
	resp, body = h.do("GET", "/api/games/1/cover?v="+g.Cover, nil)
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "image/png" || !bytes.Equal(body, png) {
		t.Fatalf("get: %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	if resp, _ = h.do("DELETE", "/api/games/1/cover", nil); resp.StatusCode != 200 {
		t.Fatalf("delete: %d", resp.StatusCode)
	}
	if resp, _ = h.do("GET", "/api/games/1/cover", nil); resp.StatusCode != 404 {
		t.Fatalf("after delete: %d", resp.StatusCode)
	}
}

func TestFoyerWidget(t *testing.T) {
	h := setup(t)
	resp, body := h.do("GET", "/api/foyer/widget", nil)
	if resp.StatusCode != 200 || !strings.Contains(string(body), `"items":[]`) {
		t.Fatalf("before playing: %d %s", resp.StatusCode, body)
	}
	h.do("POST", "/api/games/1/played", map[string]int{"seconds": 120})
	_, body = h.do("GET", "/api/foyer/widget", nil)
	var w struct {
		Version int `json:"version"`
		Stats   []struct{ Label, Value string }
		Items   []struct{ Title, Subtitle, URL string }
	}
	json.Unmarshal(body, &w)
	if w.Version != 1 || len(w.Items) != 1 || w.Items[0].Title != "Continue: Parlor Test" ||
		w.Items[0].URL != "/#/play/1" || w.Items[0].Subtitle != "GBA · just now · 2 min played" || w.Stats[0].Value != "1" {
		t.Fatalf("widget: %s", body)
	}
}

// DS games are sent without their padding, and in ranges.
func TestTrimmedROM(t *testing.T) {
	h := setup(t)
	nds := make([]byte, 64<<10)
	binary.LittleEndian.PutUint32(nds[0x80:], 0x1000)
	for i := range 0x1088 {
		nds[0x200+i%0x100] = byte(i)
	}
	os.WriteFile(filepath.Join(h.roms, "Test.nds"), nds, 0644)
	h.do("POST", "/api/library/scan", nil)
	resp, body := h.do("GET", "/api/games/2", nil)
	if !strings.Contains(string(body), `"platform":"nds"`) {
		t.Fatalf("game: %d %s", resp.StatusCode, body)
	}
	resp, body = h.do("GET", "/api/games/2/rom", nil)
	if resp.StatusCode != 200 || !bytes.Equal(body, nds[:0x1088]) {
		t.Fatalf("rom: %d, %d bytes", resp.StatusCode, len(body))
	}
	req, _ := http.NewRequest("GET", h.srv.URL+"/api/games/2/rom", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Range", "bytes=4096-")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 206 || !bytes.Equal(body, nds[0x1000:0x1088]) {
		t.Fatalf("range: %d, %d bytes", resp.StatusCode, len(body))
	}
}

// /play is the app with the looser policy EmulatorJS's cores need; the
// rest of the app keeps the strict one.
func TestPlayPolicy(t *testing.T) {
	h := setup(t)
	resp, _ := h.do("GET", "/play", nil)
	if resp.StatusCode != 200 || !strings.Contains(resp.Header.Get("Content-Security-Policy"), "'unsafe-eval'") {
		t.Fatalf("/play: %d %q", resp.StatusCode, resp.Header.Get("Content-Security-Policy"))
	}
	resp, _ = h.do("GET", "/", nil)
	if strings.Contains(resp.Header.Get("Content-Security-Policy"), "'unsafe-eval'") {
		t.Fatalf("/: %q", resp.Header.Get("Content-Security-Policy"))
	}
}

func TestAgo(t *testing.T) {
	now := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	for stamp, want := range map[string]string{
		"2026-10-04T11:59:30.000Z": "just now",
		"2026-10-04T11:15:00.000Z": "45 min ago",
		"2026-10-04T10:00:00.000Z": "2 h ago",
		"2026-10-03T09:00:00.000Z": "yesterday",
		"2026-09-30T09:00:00.000Z": "4 days ago",
	} {
		if got := ago(stamp, now); got != want {
			t.Errorf("%s: %q, want %q", stamp, got, want)
		}
	}
}

func itoa(n int64) string {
	b, _ := json.Marshal(n)
	return string(b)
}
