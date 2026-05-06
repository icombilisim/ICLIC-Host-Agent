// Package main is the entry point for the ICLIC Host Agent.
//
// The agent's only job is to send a periodic heartbeat to the ICLIC backend.
// The metric body is produced by a pluggable collector pipeline reading
// /etc/iclic-host-agent/collectors.d/*.yaml — see internal/collectors and
// docs/collectors.md for the operator-facing schema.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/icombilisim/iclic-host-agent/internal/config"
	"github.com/icombilisim/iclic-host-agent/internal/heartbeat"
)

const (
	exitOK       = 0
	exitConfig   = 2
	exitInternal = 3
)

// defaultCollectorDir is overridable via $ICLIC_COLLECTOR_DIR for dev runs.
const defaultCollectorDir = "/etc/iclic-host-agent/collectors.d"

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load config", "err", err)
		os.Exit(exitConfig)
	}
	collectorDir := os.Getenv("ICLIC_COLLECTOR_DIR")
	if collectorDir == "" {
		collectorDir = defaultCollectorDir
	}
	slog.Info("agent starting",
		"server_id", cfg.ServerID,
		"iclic_url", cfg.ICLICUrl,
		"interval_sec", cfg.HeartbeatIntervalSeconds,
		"collector_dir", collectorDir,
		"agent_version", heartbeat.AgentVersion,
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-stop
		slog.Info("received signal, shutting down", "signal", sig.String())
		cancel()
	}()

	sender := heartbeat.NewSender(cfg, collectorDir)
	ticker := time.NewTicker(time.Duration(cfg.HeartbeatIntervalSeconds) * time.Second)
	defer ticker.Stop()

	if err := sender.SendOnce(ctx); err != nil {
		slog.Warn("initial heartbeat failed", "err", err)
	}

	for {
		select {
		case <-ctx.Done():
			slog.Info("agent stopped")
			os.Exit(exitOK)
		case <-ticker.C:
			if err := sender.SendOnce(ctx); err != nil {
				slog.Warn("heartbeat failed", "err", err)
			}
		}
	}
}
