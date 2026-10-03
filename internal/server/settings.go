package server

import (
	"net/http"
	"slices"
	"strconv"
)

// Settings apply to every device.
type Settings struct {
	// FastForward is the speed the fast-forward button plays at.
	FastForward int `json:"fast_forward"`
}

// FastForwardSpeeds are the speeds to choose from; 2× is the default.
var FastForwardSpeeds = []int{2, 3, 4, 6, 8}

func (s *Server) settings() (Settings, error) {
	out := Settings{FastForward: 2}
	v, err := s.Store.Setting("fast_forward")
	if n, _ := strconv.Atoi(v); slices.Contains(FastForwardSpeeds, n) {
		out.FastForward = n
	}
	return out, err
}

func (s *Server) settingsRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/settings", func(w http.ResponseWriter, r *http.Request) {
		out, err := s.settings()
		if err != nil {
			storeFailure(w, err)
			return
		}
		jsonResponse(w, out)
	})
	mux.HandleFunc("POST /api/settings", func(w http.ResponseWriter, r *http.Request) {
		var body Settings
		if !decode(w, r, &body) {
			return
		}
		if !slices.Contains(FastForwardSpeeds, body.FastForward) {
			failure(w, 400, "Fast forward must be 2×, 3×, 4×, 6× or 8×")
			return
		}
		if err := s.Store.SetSetting("fast_forward", strconv.Itoa(body.FastForward)); err != nil {
			storeFailure(w, err)
			return
		}
		jsonResponse(w, body)
	})
}
