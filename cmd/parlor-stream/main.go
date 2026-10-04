// Command parlor-stream plays games on the server and streams them to the
// browser, for consoles too heavy to emulate in a phone's browser (the
// 3DS). It runs beside Parlor, which hands it the games; see package
// stream.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/audemed44/parlor/internal/stream"
)

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, nil)))
	cfg := stream.FromEnv()
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "session":
			// One game, started by the server below.
			if err := stream.RunSession(cfg, os.Stdin, os.Stdout); err != nil {
				os.Exit(1)
			}
			return
		case "healthcheck":
			res, err := http.Get("http://127.0.0.1" + listen() + "/healthz")
			if err != nil || res.StatusCode != 200 {
				os.Exit(1)
			}
			return
		}
	}

	if cfg.Token == "" {
		slog.Error("set PARLOR_TOKEN: the same one Parlor has")
		os.Exit(1)
	}
	if len(cfg.Hosts) == 0 {
		slog.Warn("PARLOR_STREAM_HOSTS is empty: browsers can only reach the stream on the container's own address")
	}
	exe, err := os.Executable()
	if err != nil {
		slog.Error("can't find this program", "err", err)
		os.Exit(1)
	}
	slog.Info("parlor-stream", "gpu", cfg.GPU, "encoder", cfg.Encoder, "scale", cfg.Scale,
		"max_height", cfg.MaxHeight, "udp", cfg.UDPPort, "hosts", cfg.Hosts)
	side := &stream.Sidecar{Config: cfg, Exe: exe}
	srv := &http.Server{Addr: listen(), Handler: side.Handler(), ReadHeaderTimeout: 10 * time.Second}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		side.Shutdown()
		shut, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shut)
	}()
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("server failed", "err", err)
		os.Exit(1)
	}
}

func listen() string {
	if v := os.Getenv("PARLOR_STREAM_LISTEN"); v != "" {
		return v
	}
	return ":8090"
}
