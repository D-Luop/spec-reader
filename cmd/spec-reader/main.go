package main

import (
	"flag"
	"log/slog"
	"net/http"
	"os"

	"github.com/D-Luop/spec-reader/internal/config"
	"github.com/D-Luop/spec-reader/internal/server"
)

func main() {
	configPath := flag.String("config", "config.yaml", "path to the instance configuration")
	addr := flag.String("addr", ":3013", "HTTP listen address")
	flag.Parse()

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
