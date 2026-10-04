package server

import (
	"net/http"
	"os"
	"path/filepath"

	"github.com/audemed44/parlor/internal/store"
)

func (s *Server) gameRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/games", func(w http.ResponseWriter, r *http.Request) {
		games, err := s.Store.Games()
		if err != nil {
			storeFailure(w, err)
			return
		}
		jsonResponse(w, games)
	})
	mux.HandleFunc("POST /api/library/scan", func(w http.ResponseWriter, r *http.Request) {
		res, err := s.Store.Scan(s.ROMs)
		if err != nil {
			storeFailure(w, err)
			return
		}
		jsonResponse(w, res)
	})
	mux.HandleFunc("GET /api/games/{id}", func(w http.ResponseWriter, r *http.Request) {
		g, err := s.Store.Game(pathID(r))
		if err != nil {
			storeFailure(w, err)
			return
		}
		saves, err := s.Store.Saves(g.ID)
		if err != nil {
			storeFailure(w, err)
			return
		}
		states, err := s.Store.States(g.ID)
		if err != nil {
			storeFailure(w, err)
			return
		}
		jsonResponse(w, struct {
			store.Game
			Saves  []store.Save  `json:"saves"`
			States []store.State `json:"states"`
		}{g, saves, states})
	})
	mux.HandleFunc("POST /api/games/{id}/notes", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Notes string `json:"notes"`
		}
		if !decode(w, r, &body) {
			return
		}
		if len(body.Notes) > 10000 {
			failure(w, 400, "Notes are limited to 10,000 characters")
			return
		}
		if err := s.Store.SetNotes(pathID(r), body.Notes); err != nil {
			storeFailure(w, err)
			return
		}
		jsonResponse(w, map[string]bool{"ok": true})
	})
	mux.HandleFunc("POST /api/games/{id}/played", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Seconds int64 `json:"seconds"`
		}
		if !decode(w, r, &body) {
			return
		}
		if err := s.Store.AddPlay(pathID(r), body.Seconds); err != nil {
			storeFailure(w, err)
			return
		}
		jsonResponse(w, map[string]bool{"ok": true})
	})
	mux.HandleFunc("GET /api/games/{id}/rom", func(w http.ResponseWriter, r *http.Request) {
		g, err := s.Store.Game(pathID(r))
		if err != nil {
			storeFailure(w, err)
			return
		}
		f, err := os.Open(filepath.Join(s.ROMs, filepath.FromSlash(g.Path)))
		if err != nil {
			failure(w, 404, "The ROM file is missing; rescan the library")
			return
		}
		defer f.Close()
		info, err := f.Stat()
		if err != nil {
			storeFailure(w, err)
			return
		}
		// The checksum is the version: the browser keeps its own copy and
		// revalidates.
		w.Header().Set("Cache-Control", "private, no-cache")
		w.Header().Set("ETag", `"`+g.SHA1+`"`)
		w.Header().Set("Content-Type", "application/octet-stream")
		http.ServeContent(w, r, "", info.ModTime(), f)
	})
}
