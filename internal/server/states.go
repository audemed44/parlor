package server

import (
	"errors"
	"io"
	"net/http"
	"os"
	"runtime/debug"
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
		v, f, ok := s.state(w, r)
		if !ok {
			return
		}
		defer f.Close()
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("X-State-Id", strconv.FormatInt(v.ID, 10))
		http.ServeContent(w, r, "", time.Time{}, f)
	})
	// The screenshot: mGBA's states are PNGs of the screen.
	mux.HandleFunc("GET /api/games/{id}/states/{slot}/image", func(w http.ResponseWriter, r *http.Request) {
		v, f, ok := s.state(w, r)
		if !ok {
			return
		}
		defer f.Close()
		if !v.Image {
			failure(w, 404, "This state has no screenshot")
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
		http.ServeContent(w, r, "", modTime(v.Created), f)
	})
	mux.HandleFunc("PUT /api/games/{id}/states/{slot}", func(w http.ResponseWriter, r *http.Request) {
		n := slot(r)
		if n < 0 {
			failure(w, 400, store.ErrInvalidSlot.Error())
			return
		}
		data, err := readBody(w, r, store.MaxStateSize)
		if err != nil {
			failure(w, 413, "Save state is too large")
			return
		}
		// A DS state is 6.5 MiB: give the memory back afterwards.
		if len(data) > 1<<20 {
			defer debug.FreeOSMemory()
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

func (s *Server) state(w http.ResponseWriter, r *http.Request) (store.State, *os.File, bool) {
	v, err := s.Store.StateIn(pathID(r), slot(r))
	if err != nil {
		storeFailure(w, err)
		return v, nil, false
	}
	f, err := s.Store.OpenState(v)
	if err != nil {
		storeFailure(w, err)
		return v, nil, false
	}
	return v, f, true
}

// readBody reads a request body of at most limit bytes into a buffer of
// its declared size, rather than one grown by doubling.
func readBody(w http.ResponseWriter, r *http.Request, limit int64) ([]byte, error) {
	body := http.MaxBytesReader(w, r.Body, limit)
	if n := r.ContentLength; n > 0 && n <= limit {
		data := make([]byte, n)
		_, err := io.ReadFull(body, data)
		return data, err
	}
	return io.ReadAll(body)
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
