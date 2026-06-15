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
	"net/http"
	_ "net/http/pprof" // exposed on a private loopback mux, see startPprof
	"os"
	"os/signal"
	"runtime/debug"
	"strconv"
	"syscall"
	"time"

	"github.com/icombilisim/iclic-host-agent/internal/config"
	"github.com/icombilisim/iclic-host-agent/internal/control"
	"github.com/icombilisim/iclic-host-agent/internal/heartbeat"
)

const (
	exitOK       = 0
	exitConfig   = 2
	exitInternal = 3
)

// defaultCollectorDir is overridable via $ICLIC_COLLECTOR_DIR for dev runs.
const defaultCollectorDir = "/etc/iclic-host-agent/collectors.d"

// Memory limits. The Go runtime defaults to "no soft limit", which means
// transient allocations from a slow leak can grow well past what the host can
// spare before GC kicks in. We default to a conservative 384 MB soft cap that
// matches the systemd MemoryHigh drop-in the operator is encouraged to install
// (see docs/operations.md). Operators override with the standard GOMEMLIMIT
// env var; setting GOMEMLIMIT=-1 disables. (#2)
const defaultGoMemLimitBytes = 384 * 1024 * 1024

// defaultPprofAddr binds the diagnostic listener to loopback only — no
// external attack surface. Operators set ICLIC_AGENT_PPROF_ADDR=disabled to
// turn it off, or any host:port to relocate it.
const defaultPprofAddr = "127.0.0.1:6133"

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	applyMemoryLimit()
	startPprof()

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

	// Control channel runs alongside the heartbeat ticker on the same
	// process-lifetime ctx, so SIGTERM/SIGINT cancels both. Outbound-only WSS;
	// ICLIC requests, the agent serves its closed verb set. (#40 Faz 4a)
	go control.RunControlChannel(ctx, cfg, heartbeat.AgentVersion)

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

// applyMemoryLimit honours $GOMEMLIMIT if set (including the runtime's own
// suffix syntax — 256MiB, 1GiB, etc.), otherwise installs a 384 MB soft cap.
// The soft cap means the Go GC starts working harder *before* the cgroup
// MemoryMax kills the process; without it a slow collector leak grows
// unchecked until the OS intervenes. (#2)
func applyMemoryLimit() {
	v := os.Getenv("GOMEMLIMIT")
	if v == "" {
		debug.SetMemoryLimit(defaultGoMemLimitBytes)
		slog.Info("memory limit set", "soft_cap_bytes", defaultGoMemLimitBytes, "source", "default")
		return
	}
	// The Go runtime parses GOMEMLIMIT itself when set, but only at process
	// start and only with the suffix syntax. We re-emit it for operator
	// visibility — the actual parsing is whatever the runtime did.
	if n, err := strconv.ParseInt(v, 10, 64); err == nil {
		slog.Info("memory limit set", "soft_cap_bytes", n, "source", "GOMEMLIMIT")
	} else {
		slog.Info("memory limit set", "soft_cap_raw", v, "source", "GOMEMLIMIT")
	}
}

// startPprof exposes /debug/pprof/* on loopback so a future incident can be
// triaged with `go tool pprof http://127.0.0.1:6133/debug/pprof/heap` without
// redeploying. Bound to 127.0.0.1 — never reachable off-host. Set
// ICLIC_AGENT_PPROF_ADDR=disabled to turn off entirely. (#2)
func startPprof() {
	addr := os.Getenv("ICLIC_AGENT_PPROF_ADDR")
	if addr == "" {
		addr = defaultPprofAddr
	}
	if addr == "disabled" || addr == "off" {
		slog.Info("pprof listener disabled")
		return
	}
	go func() {
		// The blank import of net/http/pprof registered handlers on the
		// DefaultServeMux. We bind the listener explicitly to a loopback
		// address so the diagnostic port never leaks to a public interface.
		srv := &http.Server{
			Addr:              addr,
			ReadHeaderTimeout: 3 * time.Second,
		}
		slog.Info("pprof listener starting", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Warn("pprof listener exited", "addr", addr, "err", err)
		}
	}()
}
