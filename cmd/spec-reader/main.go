package main

import (
	"flag"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/D-Luop/spec-reader/internal/config"
	"github.com/D-Luop/spec-reader/internal/server"
)

func main() {
	configPath := flag.String("config", "config.yaml", "path to the instance configuration")
	addr := flag.String("addr", ":3013", "HTTP listen address")
	healthcheck := flag.Bool("healthcheck", false, "check the local server health endpoint and exit")
	flag.Parse()
	if *healthcheck {
		client := &http.Client{Timeout: 3 * time.Second}
		resp, err := client.Get("http://127.0.0.1:3013/health")
		if err != nil || resp.StatusCode != http.StatusOK {
			os.Exit(1)
		}
		_ = resp.Body.Close()
		return
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		slog.Error("load configuration", "error", err)
		os.Exit(1)
	}

	handler, err := server.New(cfg)
	if err != nil {
		slog.Error("create server", "error", err)
		os.Exit(1)
	}

	slog.Info("spec-reader listening", "addr", *addr, "specs", len(cfg.Specs))
	if err := http.ListenAndServe(*addr, handler); err != nil {
		slog.Error("serve", "error", err)
		os.Exit(1)
	}
}
