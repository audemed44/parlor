package server

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/audemed44/parlor/internal/store"
)

// streamed are the consoles too heavy for a phone's browser: they play on
// the server, in parlor-stream, and stream to the browser over WebRTC.
var streamed = map[string]bool{"3ds": true}

func (s *Server) streamRoutes(mux *http.ServeMux) {
	// The game parlor-stream is playing now (game_id 0 for none).
	mux.HandleFunc("GET /api/stream", func(w http.ResponseWriter, r *http.Request) {
		if s.StreamURL == "" {
			jsonResponse(w, map[string]int64{"game_id": 0})
			return
		}
		s.forward(w, r, "GET", nil)
	})
	// Start playing a streamed game: the browser's WebRTC offer in,
	// parlor-stream's answer out. Starting one ends the game playing
	// before, on any device, keeping its place.
	mux.HandleFunc("POST /api/games/{id}/stream", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			SDP     string `json:"sdp"`
			CarryOn bool   `json:"carry_on"`
			Device  string `json:"device"`
			Scale   int    `json:"scale"`
		}
		if !decode(w, r, &body) {
			return
		}
		if s.StreamURL == "" {
			failure(w, 503, "3DS games need parlor-stream: see the README")
			return
		}
		g, err := s.Store.Game(pathID(r))
		if err != nil {
			storeFailure(w, err)
			return
		}
		if !streamed[g.Platform] {
			failure(w, 400, "This game plays in the browser")
			return
		}
		if g.Missing {
			failure(w, 404, "This game's ROM is missing from the library folder.")
			return
		}
		if strings.HasPrefix(g.Path, store.Patched) {
			failure(w, 400, "Patched 3DS games can't be played yet")
			return
		}
		job, _ := json.Marshal(map[string]any{
			"game_id": g.ID, "platform": g.Platform, "rom": g.Path, "sdp": body.SDP,
			"carry_on": body.CarryOn, "device": device(body.Device), "scale": body.Scale,
		})
		s.forward(w, r, "POST", job)
	})
}

// forward makes a request of parlor-stream's /session and passes its
// answer on.
func (s *Server) forward(w http.ResponseWriter, r *http.Request, method string, body []byte) {
	req, err := http.NewRequestWithContext(r.Context(), method, strings.TrimSuffix(s.StreamURL, "/")+"/session", bytes.NewReader(body))
	if err != nil {
		storeFailure(w, err)
		return
	}
	req.Header.Set("Authorization", "Bearer "+s.Token)
	req.Header.Set("Content-Type", "application/json")
	client := s.StreamClient
	if client == nil {
		// Ending the game before can take a while: it keeps its place.
		client = &http.Client{Timeout: 2 * time.Minute}
	}
	res, err := client.Do(req)
	if err != nil {
		slog.Error("parlor-stream", "err", err)
		failure(w, 502, "Couldn't reach parlor-stream")
		return
	}
	defer res.Body.Close()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(res.StatusCode)
	_, _ = io.Copy(w, io.LimitReader(res.Body, 1<<20))
}
