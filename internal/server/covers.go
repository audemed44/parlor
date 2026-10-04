package server

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/audemed44/parlor/internal/store"
)

func (s *Server) coverRoutes(mux *http.ServeMux) {
	// The cover's URL carries its version (?v=), so it's cached for good.
	mux.HandleFunc("GET /api/games/{id}/cover", func(w http.ResponseWriter, r *http.Request) {
		data, ctype, err := s.Store.CoverData(pathID(r))
		if err != nil {
			storeFailure(w, err)
			return
		}
		w.Header().Set("Content-Type", ctype)
		w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
		http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(data))
	})
	mux.HandleFunc("POST /api/games/{id}/cover", func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, store.MaxCoverSize+(64<<10))
		f, _, err := r.FormFile("file")
		if err != nil {
			failure(w, 400, store.ErrInvalidCover.Error())
			return
		}
		defer f.Close()
		data, err := io.ReadAll(f)
		if err != nil {
			failure(w, 413, store.ErrInvalidCover.Error())
			return
		}
		g, err := s.Store.SetCover(pathID(r), data)
		if errors.Is(err, store.ErrInvalidCover) {
			failure(w, 400, err.Error())
			return
		}
		if err != nil {
			storeFailure(w, err)
			return
		}
		jsonResponse(w, g)
	})
	mux.HandleFunc("DELETE /api/games/{id}/cover", func(w http.ResponseWriter, r *http.Request) {
		if err := s.Store.RemoveCover(pathID(r)); err != nil {
			storeFailure(w, err)
			return
		}
		jsonResponse(w, map[string]bool{"ok": true})
	})
}
