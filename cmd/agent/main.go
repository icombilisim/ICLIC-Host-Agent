// Package main is the entry point for the ICLIC Host Agent.
//
// The agent's only job is to send a periodic heartbeat to the ICLIC backend.
// Real metric collection lands in follow-up commits — this initial scaffold
// emits a placeholder payload so the end-to-end HMAC + enrollment flow can be
// wired through against a stub backend.
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

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load config", "err", err)
		os.Exit(exitConfig)
	}
	slog.Info("agent starting",
		"server_id", cfg.ServerID,
		"iclic_url", cfg.ICLICUrl,
		"interval_sec", cfg.HeartbeatIntervalSeconds,
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

	sender := heartbeat.NewSender(cfg)
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
