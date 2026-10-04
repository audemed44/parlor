// Package server is Parlor's HTTP API and the embedded frontend. Every /api/
// call needs the token, as a bearer or the session cookie derived from it,
// and state-changing requests from another origin are refused.
package server

import (
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/audemed44/parlor/internal/store"
)

// Server serves the API and frontend.
type Server struct {
	Store *store.Store
	// ROMs is the library folder, read-only.
	ROMs string
	// ImportDir holds saves to import (RomM's assets, copied RetroDECK
	// saves); "" turns importing off.
	ImportDir     string
	Token         string
	SecureCookies bool
	Files         fs.FS
	// FoyerURL is Foyer, the homelab's start page, linked from the header.
	FoyerURL string
}

// The emulators compile WebAssembly ('wasm-unsafe-eval'). mGBA runs its
// threads as module workers from its own script; EmulatorJS unpacks each
// RetroArch core and runs its script and workers from blob: URLs.
const contentSecurityPolicy = "default-src 'self'; script-src 'self' blob: 'wasm-unsafe-eval'; " +
	"worker-src 'self' blob:; style-src 'self'; img-src 'self' data: blob:; font-src 'self'; " +
	"connect-src 'self'; media-src 'self' blob:; frame-ancestors 'none'; base-uri 'none'; form-action 'self'"

// playPolicy is for /play, the same app as a page of its own for the
// games EmulatorJS runs: the RetroArch cores' glue code builds functions
// from strings ('unsafe-eval') and fetches its WebAssembly from a blob:
// URL, and EmulatorJS sets inline styles. Only that page is allowed them;
// leaving a game there returns to /.
var playPolicy = strings.NewReplacer(
	"script-src 'self' blob:", "script-src 'self' blob: 'unsafe-eval'",
	"style-src 'self'", "style-src 'self' 'unsafe-inline'",
	"connect-src 'self'", "connect-src 'self' blob:",
).Replace(contentSecurityPolicy)

// Handler returns the HTTP handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })
	s.authRoutes(mux)
	s.gameRoutes(mux)
	s.saveRoutes(mux)
	s.stateRoutes(mux)
	s.patchRoutes(mux)
	s.coverRoutes(mux)
	s.foyerRoutes(mux)
	s.importRoutes(mux)
	s.settingsRoutes(mux)
	if s.Files != nil {
		files := http.FileServerFS(s.Files)
		mux.HandleFunc("GET /play", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Security-Policy", playPolicy)
			w.Header().Set("Cache-Control", "no-cache")
			http.ServeFileFS(w, r, s.Files, "index.html")
		})
		mux.Handle("GET /", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case strings.HasPrefix(r.URL.Path, "/assets/"), strings.HasPrefix(r.URL.Path, "/core/"),
				strings.HasPrefix(r.URL.Path, "/ejs/"):
				// Hashed or versioned names: never change.
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			default:
				w.Header().Set("Cache-Control", "no-cache")
			}
			files.ServeHTTP(w, r)
		}))
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Content-Security-Policy", contentSecurityPolicy)
		// Cross-origin isolation: the emulator's threads need
		// SharedArrayBuffer.
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		h.Set("Cross-Origin-Embedder-Policy", "require-corp")
		h.Set("Cross-Origin-Resource-Policy", "same-origin")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			h.Set("Cache-Control", "no-store")
			if r.Method != "GET" && r.Method != "HEAD" && !sameOrigin(r) {
				failure(w, 403, "Cross-origin request refused")
				return
			}
			if r.URL.Path != "/api/login" && !s.authenticated(r) {
				failure(w, 401, "Sign in to Parlor")
				return
			}
		}
		mux.ServeHTTP(w, r)
	})
}

// sameOrigin refuses browser requests from another site. Requests without
// an Origin (curl, Foyer) pass; they still need the token.
func sameOrigin(r *http.Request) bool {
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		return false
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	return err == nil && u.Host == r.Host && (u.Scheme == "http" || u.Scheme == "https")
}

func jsonResponse(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func failure(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// storeFailure answers a store error: 404 for a missing game or save, 500
// (logged) otherwise.
func storeFailure(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrNotFound) {
		failure(w, 404, "Not found")
		return
	}
	slog.Error("request failed", "err", err)
	failure(w, 500, "Something went wrong")
}

// decode reads exactly one JSON object of at most 64 KiB, with no unknown
// fields. On failure it has already written the 400.
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if d.Decode(v) != nil {
		failure(w, 400, "Invalid request body")
		return false
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		failure(w, 400, "Expected one JSON object")
		return false
	}
	return true
}

// modTime reads a stored timestamp, for Last-Modified.
func modTime(stamp string) time.Time {
	t, _ := time.Parse("2006-01-02T15:04:05.000Z", stamp)
	return t
}

// pathID is the {id} path value, or 0 when it isn't a positive integer.
func pathID(r *http.Request) int64 {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		return 0
	}
	return id
}
