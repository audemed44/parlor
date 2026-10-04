package server

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/audemed44/parlor/internal/store"
)

// slot is the {slot} path value, or -1 when it isn't a slot number.
func slot(r *http.Request) int {
	n, err := strconv.Atoi(r.PathValue("slot"))
	if err != nil || n < 0 || n > store.QuickSlots {
		return -1
	}
	return n
}

func (s *Server) stateRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/games/{id}/states", func(w http.ResponseWriter, r *http.Request) {
		if _, err := s.Store.Game(pathID(r)); err != nil {
			storeFailure(w, err)
			return
		}
		states, err := s.Store.States(pathID(r))
		if err != nil {
			storeFailure(w, err)
			return
		}
		jsonResponse(w, states)
	})
	// The state's bytes, as mGBA wrote them; its version is X-State-Id.
	mux.HandleFunc("GET /api/games/{id}/states/{slot}", func(w http.ResponseWriter, r *http.Request) {
		v, data, ok := s.state(w, r)
		if !ok {
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("X-State-Id", strconv.FormatInt(v.ID, 10))
		w.Write(data)
	})
	// The screenshot: mGBA's states are PNGs of the screen.
	mux.HandleFunc("GET /api/games/{id}/states/{slot}/image", func(w http.ResponseWriter, r *http.Request) {
		v, data, ok := s.state(w, r)
		if !ok {
			return
		}
		if !v.Image {
			failure(w, 404, "This state has no screenshot")
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
		http.ServeContent(w, r, "", modTime(v.Created), bytes.NewReader(data))
	})
	mux.HandleFunc("PUT /api/games/{id}/states/{slot}", func(w http.ResponseWriter, r *http.Request) {
		n := slot(r)
		if n < 0 {
			failure(w, 400, store.ErrInvalidSlot.Error())
			return
		}
		data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, store.MaxStateSize))
		if err != nil {
			failure(w, 413, "Save state is too large")
			return
		}
		// at is when the player took the state; one that waited offline
		// doesn't replace a newer one.
		at, _ := time.Parse(time.RFC3339, r.URL.Query().Get("at"))
		v, err := s.Store.PutState(pathID(r), n, store.NewState{
			Data: data, Device: device(r.URL.Query().Get("device")), Created: at, IfNewer: !at.IsZero(),
		})
		stateResult(w, v, err)
	})
	mux.HandleFunc("DELETE /api/games/{id}/states/{slot}", func(w http.ResponseWriter, r *http.Request) {
		if err := s.Store.DeleteState(pathID(r), slot(r)); err != nil {
			storeFailure(w, err)
			return
		}
		jsonResponse(w, map[string]bool{"ok": true})
	})
}

func (s *Server) state(w http.ResponseWriter, r *http.Request) (store.State, []byte, bool) {
	v, err := s.Store.StateIn(pathID(r), slot(r))
	if err != nil {
		storeFailure(w, err)
		return v, nil, false
	}
	data, err := s.Store.StateData(v)
	if err != nil {
		storeFailure(w, err)
		return v, nil, false
	}
	return v, data, true
}

func stateResult(w http.ResponseWriter, v store.State, err error) {
	switch {
	case errors.Is(err, store.ErrInvalidState), errors.Is(err, store.ErrInvalidSlot):
		failure(w, 400, err.Error())
	case err != nil:
		storeFailure(w, err)
	default:
		jsonResponse(w, v)
	}
}
