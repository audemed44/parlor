package server

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"

	"github.com/audemed44/parlor/internal/library"
	"github.com/audemed44/parlor/internal/store"
)

// device is the client's short device label ("iPhone", "Desktop"), shown in
// the save history.
func device(v string) string {
	v = strings.TrimSpace(v)
	if len(v) > 40 {
		v = v[:40]
	}
	return v
}

func (s *Server) saveRoutes(mux *http.ServeMux) {
	// The latest in-game save, as bytes; its version is in X-Save-Id.
	mux.HandleFunc("GET /api/games/{id}/save", func(w http.ResponseWriter, r *http.Request) {
		latest, err := s.Store.Latest(pathID(r))
		if err != nil {
			storeFailure(w, err)
			return
		}
		if latest == nil {
			failure(w, 404, "No save yet")
			return
		}
		s.sendSave(w, *latest, false)
	})
	// The player's autosave: the raw save as the body. base is the version
	// the player loaded; when another device has saved since, 409 with the
	// newer version, unless force=1.
	mux.HandleFunc("PUT /api/games/{id}/save", func(w http.ResponseWriter, r *http.Request) {
		data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, store.MaxSaveSize))
		if err != nil {
			failure(w, 413, "Save is too large")
			return
		}
		q := r.URL.Query()
		base, _ := strconv.ParseInt(q.Get("base"), 10, 64)
		v, err := s.Store.AddSave(pathID(r), store.NewSave{
			Data: data, Source: "play", Device: device(q.Get("device")),
			Base: base, Force: q.Get("force") == "1",
		})
		s.saved(w, v, err)
	})
	// A save file uploaded by hand: from another emulator, or a backup.
	mux.HandleFunc("POST /api/games/{id}/saves", func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, store.MaxSaveSize+(64<<10))
		f, header, err := r.FormFile("file")
		if err != nil {
			failure(w, 400, "Choose a save file (.srm or .sav)")
			return
		}
		defer f.Close()
		data, err := io.ReadAll(f)
		if err != nil {
			failure(w, 413, "Save is too large")
			return
		}
		v, err := s.Store.AddSave(pathID(r), store.NewSave{
			Data: data, Source: "upload", Device: device(r.FormValue("device")),
			Note: header.Filename, Force: true,
		})
		s.saved(w, v, err)
	})
	mux.HandleFunc("GET /api/saves/{id}", func(w http.ResponseWriter, r *http.Request) {
		v, err := s.Store.SaveVersion(pathID(r))
		if err != nil {
			storeFailure(w, err)
			return
		}
		s.sendSave(w, v, true)
	})
	mux.HandleFunc("POST /api/saves/{id}/restore", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Device string `json:"device"`
		}
		if !decode(w, r, &body) {
			return
		}
		v, err := s.Store.Restore(pathID(r), device(body.Device))
		s.saved(w, v, err)
	})
}

func (s *Server) saved(w http.ResponseWriter, v store.Save, err error) {
	var conflict *store.Conflict
	switch {
	case errors.As(err, &conflict):
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(409)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": conflict.Error(), "latest": conflict.Latest})
	case errors.Is(err, store.ErrNotFound):
		failure(w, 404, "Not found")
	case errors.Is(err, store.ErrInvalidSave):
		failure(w, 400, err.Error())
	case err != nil:
		storeFailure(w, err)
	default:
		jsonResponse(w, v)
	}
}

// sendSave writes a save version's bytes; as a download, named after the
// game, like other emulators expect (<ROM name>.srm).
func (s *Server) sendSave(w http.ResponseWriter, v store.Save, download bool) {
	data, err := s.Store.SaveData(v)
	if err != nil {
		storeFailure(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("X-Save-Id", strconv.FormatInt(v.ID, 10))
	if download {
		name := "save.srm"
		if g, err := s.Store.Game(v.GameID); err == nil {
			name = library.Title(g.Path) + ".srm"
			// A 3DS save is the folder of the game's save files, as a tar.
			if streamed[g.Platform] {
				name = library.Title(g.Path) + ".tar"
			}
		}
		w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
	}
	w.Write(data)
}
