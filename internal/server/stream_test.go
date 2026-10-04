package server

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"hash/crc32"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/audemed44/parlor/internal/store"
)

// streamSetup is a library with one 3DS game (not a real ROM: parlor-stream
// is faked) and a fake parlor-stream.
func streamSetup(t *testing.T, sidecar http.HandlerFunc) *harness {
	t.Helper()
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	roms := t.TempDir()
	os.MkdirAll(filepath.Join(roms, "3ds"), 0755)
	os.WriteFile(filepath.Join(roms, "3ds", "Test Game.3ds"), []byte("not a real 3DS game"), 0644)
	if _, err = db.Scan(roms); err != nil {
		t.Fatal(err)
	}
	s := &Server{Store: db, ROMs: roms, Token: token, Files: fstest.MapFS{"index.html": {Data: []byte("x")}}}
	if sidecar != nil {
		side := httptest.NewServer(sidecar)
		t.Cleanup(side.Close)
		s.StreamURL = side.URL
	}
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)
	return &harness{t, srv, roms}
}

func TestStream(t *testing.T) {
	var job map[string]any
	h := streamSetup(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token || r.URL.Path != "/session" {
			w.WriteHeader(401)
			return
		}
		if r.Method == "GET" {
			w.Write([]byte(`{"game_id":1}`))
			return
		}
		json.NewDecoder(r.Body).Decode(&job)
		w.Write([]byte(`{"sdp":"the answer"}`))
	})
	resp, body := h.do("GET", "/api/settings", nil)
	if !bytes.Contains(body, []byte(`"stream":true`)) {
		t.Fatalf("settings: %d %s", resp.StatusCode, body)
	}
	resp, body = h.do("POST", "/api/games/1/stream", map[string]any{"sdp": "the offer", "carry_on": true, "device": "iPhone", "scale": 3})
	if resp.StatusCode != 200 || !bytes.Contains(body, []byte("the answer")) {
		t.Fatalf("stream: %d %s", resp.StatusCode, body)
	}
	if job["rom"] != "3ds/Test Game.3ds" || job["platform"] != "3ds" || job["sdp"] != "the offer" ||
		job["carry_on"] != true || job["scale"] != 3.0 || job["device"] != "iPhone" {
		t.Fatalf("job: %v", job)
	}
	if _, body = h.do("GET", "/api/stream", nil); !bytes.Contains(body, []byte(`"game_id":1`)) {
		t.Fatalf("status: %s", body)
	}
}

func TestStreamNotSetUp(t *testing.T) {
	h := streamSetup(t, nil)
	if _, body := h.do("GET", "/api/settings", nil); !bytes.Contains(body, []byte(`"stream":false`)) {
		t.Fatalf("settings: %s", body)
	}
	if resp, _ := h.do("POST", "/api/games/1/stream", map[string]any{"sdp": "x"}); resp.StatusCode != 503 {
		t.Fatalf("got %d", resp.StatusCode)
	}
	if _, body := h.do("GET", "/api/stream", nil); !bytes.Contains(body, []byte(`"game_id":0`)) {
		t.Fatalf("status: %s", body)
	}
}

// wrapState puts a state in a PNG, as parlor-stream does.
func wrapState(t *testing.T, state []byte) []byte {
	var buf bytes.Buffer
	png.Encode(&buf, image.NewGray(image.Rect(0, 0, 4, 4)))
	p := buf.Bytes()
	end := len(p) - 12
	chunk := binary.BigEndian.AppendUint32(nil, uint32(len(state)))
	chunk = append(chunk, "prLs"...)
	chunk = append(chunk, state...)
	chunk = binary.BigEndian.AppendUint32(chunk, crc32.ChecksumIEEE(chunk[4:]))
	return append(append(append([]byte{}, p[:end]...), chunk...), p[end:]...)
}

func TestStreamedStates(t *testing.T) {
	h := streamSetup(t, nil)
	// 3DS states are bigger than any other console's.
	state := append([]byte("CST\x1b"), bytes.Repeat([]byte{7}, 20<<20)...)
	resp, body := h.do("PUT", "/api/games/1/states/1", wrapState(t, state))
	if resp.StatusCode != 200 {
		t.Fatalf("put: %d %s", resp.StatusCode, body)
	}
	var v store.State
	json.Unmarshal(body, &v)
	if !v.Image || v.Size < int64(len(state)) {
		t.Fatalf("state: %+v", v)
	}
	_, got := h.do("GET", "/api/games/1/states/1", nil)
	if !bytes.Contains(got, state[:64]) || len(got) != int(v.Size) {
		t.Fatalf("got %d bytes back", len(got))
	}
	// The picture comes without the state inside.
	resp, pic := h.do("GET", "/api/games/1/states/1/image", nil)
	if resp.StatusCode != 200 || len(pic) > 1024 {
		t.Fatalf("picture: %d, %d bytes", resp.StatusCode, len(pic))
	}
	if _, err := png.Decode(bytes.NewReader(pic)); err != nil {
		t.Fatalf("picture: %v", err)
	}
	// A bare state is fine; anything else isn't.
	if resp, body := h.do("PUT", "/api/games/1/states/2", state); resp.StatusCode != 200 {
		t.Fatalf("bare: %d %s", resp.StatusCode, body)
	}
	if resp, _ := h.do("PUT", "/api/games/1/states/2", []byte("RASTATE not this core's")); resp.StatusCode != 400 {
		t.Fatalf("wrong core: %d", resp.StatusCode)
	}
	if resp, _ := h.do("PUT", "/api/games/1/states/2", []byte("CST")); resp.StatusCode != 400 {
		t.Fatalf("short: %d", resp.StatusCode)
	}
}
