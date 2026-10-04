package stream

import (
	"bufio"
	"bytes"
	"crypto/subtle"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Sidecar is parlor-stream's HTTP API, for Parlor only (it needs Parlor's
// token): POST /session starts a game for a browser's offer, DELETE
// /session ends it. Each game runs in a process of its own, and only one
// at a time: starting another ends the one before, keeping its place.
type Sidecar struct {
	Config Config
	// Exe starts a game's process: this program, with "session".
	Exe string

	mu   sync.Mutex
	cur  *exec.Cmd
	done chan struct{}
	game int64
}

// answer is what a game's process prints first: its WebRTC answer, or
// why it couldn't start.
type answer struct {
	SDP   string `json:"sdp,omitempty"`
	Error string `json:"error,omitempty"`
}

// Handler is the API.
func (s *Sidecar) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })
	mux.HandleFunc("GET /session", s.auth(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		game := s.game
		s.mu.Unlock()
		reply(w, 200, map[string]any{"game_id": game})
	}))
	mux.HandleFunc("POST /session", s.auth(s.start))
	mux.HandleFunc("DELETE /session", s.auth(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.stop()
		reply(w, 200, map[string]bool{"ok": true})
	}))
	return mux
}

func reply(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Sidecar) auth(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if s.Config.Token == "" || subtle.ConstantTimeCompare([]byte(got), []byte(s.Config.Token)) != 1 {
			reply(w, 401, map[string]string{"error": "unauthorized"})
			return
		}
		h(w, r)
	}
}

// validROM is a path inside the library.
func validROM(p string) bool {
	c := path.Clean(p)
	return c != "." && c == p && !path.IsAbs(c) && c != ".." && !strings.HasPrefix(c, "../")
}

func (s *Sidecar) start(w http.ResponseWriter, r *http.Request) {
	var job Job
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&job); err != nil {
		reply(w, 400, map[string]string{"error": "invalid request"})
		return
	}
	if _, ok := Systems[job.Platform]; !ok || job.GameID <= 0 || !validROM(job.ROM) || job.Offer == "" {
		reply(w, 400, map[string]string{"error": "invalid game"})
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stop()

	body, _ := json.Marshal(job)
	cmd := exec.Command(s.Exe, "session")
	cmd.Stdin = bytes.NewReader(body)
	cmd.Stderr = os.Stderr
	out, err := cmd.StdoutPipe()
	if err != nil {
		reply(w, 500, map[string]string{"error": err.Error()})
		return
	}
	if err := cmd.Start(); err != nil {
		reply(w, 500, map[string]string{"error": err.Error()})
		return
	}
	done := make(chan struct{})
	lines := make(chan answer, 1)
	go func() {
		var a answer
		line, err := bufio.NewReader(out).ReadBytes('\n')
		if err != nil || json.Unmarshal(line, &a) != nil {
			a.Error = "the game's process didn't answer"
		}
		lines <- a
		_, _ = io.Copy(io.Discard, out)
	}()
	go func() {
		_ = cmd.Wait()
		close(done)
		s.mu.Lock()
		if s.cur == cmd {
			s.cur, s.game = nil, 0
		}
		s.mu.Unlock()
	}()
	s.cur, s.done, s.game = cmd, done, job.GameID

	var a answer
	select {
	case a = <-lines:
	case <-time.After(20 * time.Second):
		a.Error = "the game's process didn't answer in time"
	}
	if a.Error != "" {
		s.stop()
		reply(w, 502, map[string]string{"error": a.Error})
		return
	}
	reply(w, 200, a)
}

// stop ends the running game, letting it keep the player's place first.
// The caller holds mu.
func (s *Sidecar) stop() {
	if s.cur == nil {
		return
	}
	_ = s.cur.Process.Signal(syscall.SIGTERM)
	select {
	case <-s.done:
	case <-time.After(90 * time.Second):
		slog.Warn("the game didn't stop; killing it")
		_ = s.cur.Process.Kill()
		<-s.done
	}
	s.cur, s.game = nil, 0
}

// Shutdown ends the running game.
func (s *Sidecar) Shutdown() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stop()
}

// RunSession is a game's process: the job on stdin, the answer on
// stdout, then the game until it ends.
func RunSession(cfg Config, stdin io.Reader, stdout io.Writer) error {
	var job Job
	if err := json.NewDecoder(stdin).Decode(&job); err != nil {
		return err
	}
	s, sdp, err := Start(cfg, job)
	a := answer{SDP: sdp}
	if err != nil {
		a.Error = err.Error()
	}
	if werr := json.NewEncoder(stdout).Encode(a); werr != nil && err == nil {
		err = werr
	}
	if err != nil {
		return err
	}
	if f, ok := stdout.(interface{ Close() error }); ok {
		f.Close()
	}
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, os.Interrupt)
	go func() {
		<-sig
		s.Stop()
	}()
	return s.Run()
}
