package server

import (
	"net/http"
)

func (s *Server) importRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/imports", func(w http.ResponseWriter, r *http.Request) {
		if s.ImportDir == "" {
			failure(w, 404, "Set PARLOR_IMPORT_DIR to import saves")
			return
		}
		cands, err := s.Store.Candidates(s.ImportDir)
		if err != nil {
			storeFailure(w, err)
			return
		}
		jsonResponse(w, cands)
	})
	mux.HandleFunc("POST /api/imports", func(w http.ResponseWriter, r *http.Request) {
		if s.ImportDir == "" {
			failure(w, 404, "Set PARLOR_IMPORT_DIR to import saves")
			return
		}
		var body struct {
			Path   string `json:"path"`
			GameID int64  `json:"game_id"`
			// Slot is where a save state goes; in-game saves ignore it.
			Slot *int `json:"slot"`
		}
		if !decode(w, r, &body) {
			return
		}
		if body.Slot != nil {
			v, err := s.Store.ImportState(s.ImportDir, body.Path, body.GameID, *body.Slot)
			stateResult(w, v, err)
			return
		}
		v, err := s.Store.Import(s.ImportDir, body.Path, body.GameID)
		s.saved(w, v, err)
	})
}
