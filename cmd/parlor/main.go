// Command parlor serves your GBA library to the browser, where the mGBA
// emulator runs, and keeps every game's save on the server.
package main

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
	_ "time/tzdata" // the runtime image may have no zoneinfo; TZ needs this

	"github.com/audemed44/parlor/internal/server"
	"github.com/audemed44/parlor/internal/store"
	"github.com/audemed44/parlor/web"
)

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		os.Exit(healthcheck())
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, nil)))

	token := os.Getenv("PARLOR_TOKEN")
	if token == "" {
		slog.Error("set PARLOR_TOKEN: it's what you sign in with")
		os.Exit(1)
	}
	if len(token) < 16 {
		slog.Warn("PARLOR_TOKEN is short; generate a new one with: openssl rand -hex 32")
	}
	roms := env("PARLOR_ROMS", "/roms")
	if info, err := os.Stat(roms); err != nil || !info.IsDir() {
		slog.Error("PARLOR_ROMS must be a folder of .gba files", "path", roms)
		os.Exit(1)
	}
	keep, err := strconv.Atoi(env("PARLOR_SAVE_VERSIONS", "20"))
	if err != nil || keep < 1 {
		slog.Error("PARLOR_SAVE_VERSIONS must be a number of at least 1")
		os.Exit(1)
	}
	importDir := os.Getenv("PARLOR_IMPORT_DIR")
	if importDir != "" {
		if info, err := os.Stat(importDir); err != nil || !info.IsDir() {
			slog.Warn("PARLOR_IMPORT_DIR isn't a folder; importing is off", "path", importDir)
			importDir = ""
		}
	}
	db, err := store.Open(env("PARLOR_DATA_DIR", "/data"))
	if err != nil {
		slog.Error("could not open the data folder", "err", err)
		os.Exit(1)
	}
	defer db.Close()
	db.Keep = keep

	dist, err := fs.Sub(web.Dist, "dist")
	if err != nil {
		panic(err)
	}
	app := &server.Server{
		Store:         db,
		ROMs:          roms,
		ImportDir:     importDir,
		Token:         token,
		SecureCookies: env("PARLOR_SECURE_COOKIES", "true") == "true",
		Files:         dist,
		FoyerURL:      foyerURL(),
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go scanLoop(ctx, db, roms)

	srv := &http.Server{
		Addr:              env("PARLOR_LISTEN", ":8080"),
		Handler:           app.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       time.Minute,
		WriteTimeout:      10 * time.Minute, // a 32 MiB ROM over a slow phone link
		IdleTimeout:       60 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()
	slog.Info("parlor listening", "addr", srv.Addr, "roms", roms, "imports", importDir != "")
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("server stopped", "err", err)
		os.Exit(1)
	}
}

// scanLoop scans the library at start and every 10 minutes, so new ROMs
// show up without a button press. Unchanged files aren't read again.
func scanLoop(ctx context.Context, db *store.Store, roms string) {
	for {
		res, err := db.Scan(roms)
		if err != nil {
			slog.Error("library scan failed", "err", err)
		} else if res.Added+res.Updated+res.Missing > 0 {
			slog.Info("library scanned", "games", res.Total, "added", res.Added, "updated", res.Updated, "missing", res.Missing)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(10 * time.Minute):
		}
	}
}

// healthcheck is the container's HEALTHCHECK.
func healthcheck() int {
	client := http.Client{Timeout: 3 * time.Second}
	port := env("PARLOR_LISTEN", ":8080")
	port = port[strings.LastIndex(port, ":")+1:]
	resp, err := client.Get("http://127.0.0.1:" + port + "/healthz")
	if err != nil {
		return 1
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 1
	}
	return 0
}

// foyerURL is HOMEPAGE_URL, the link back to Foyer in the header, when
// it's an http(s) address.
func foyerURL() string {
	u := os.Getenv("HOMEPAGE_URL")
	if u != "" && !strings.HasPrefix(u, "https://") && !strings.HasPrefix(u, "http://") {
		slog.Warn("HOMEPAGE_URL isn't an http(s) address; ignoring it", "url", u)
		return ""
	}
	return u
}
