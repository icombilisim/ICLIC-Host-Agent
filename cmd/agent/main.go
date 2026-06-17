// Package main is the entry point for the ICLIC Host Agent.
//
// The agent's only job is to send a periodic heartbeat to the ICLIC backend.
// The metric body is produced by a pluggable collector pipeline reading
// /etc/iclic-host-agent/collectors.d/*.yaml — see internal/collectors and
// docs/collectors.md for the operator-facing schema.
package main

import (
	"context"
	"flag"
	"fmt"
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
	exitOK             = 0
	exitUsage          = 2
	exitConfig         = 2
	exitInternal       = 3
	exitAlreadyRunning = 4
)

// defaultLockFile lives in the agent's writable state dir (created 0700 by the
// installer, owned by the service user) so the single-instance lock survives in
// the strict-sandbox systemd unit. Override with $ICLIC_AGENT_LOCK_FILE. (#25)
const defaultLockFile = "/var/lib/iclic-host-agent/agent.lock"

// instanceLockFile is kept alive for the whole process lifetime: if the *os.File
// were garbage-collected its finalizer would close the fd and release the flock.
// The kernel drops the lock automatically when the process exits. (#25)
var instanceLockFile *os.File

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
	// Handle --version BEFORE any setup. The binary takes no operational flags
	// (config comes from env / config.json), so an unknown flag like --version
	// used to be silently ignored and the agent booted anyway — that is how a
	// diagnostic "version check" spawned permanent duplicate agents. (#25)
	handleCLIArgs()

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	// Refuse to start a second agent on the same host — orphans left after an
	// upgrade or a stray manual run race heartbeats and corrupt the reported
	// version in the Fleet UI. (#25)
	acquireSingleInstanceLock()

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

// handleCLIArgs parses command-line flags. The agent has no operational flags,
// but it MUST answer --version explicitly and reject anything it does not
// recognise — flag.ExitOnError fails fast on an unknown flag instead of letting
// the process fall through and boot a stray agent. Also accepts the bare
// `iclic-host-agent version` subcommand form. (#25)
func handleCLIArgs() {
	fs := flag.NewFlagSet("iclic-host-agent", flag.ExitOnError)
	versionFlag := fs.Bool("version", false, "print version and exit")
	fs.BoolVar(versionFlag, "v", false, "print version and exit (shorthand)")
	_ = fs.Parse(os.Args[1:]) // ExitOnError → unknown flags exit(2) with usage

	if *versionFlag || fs.Arg(0) == "version" {
		fmt.Printf("iclic-host-agent %s\n", heartbeat.AgentVersion)
		os.Exit(exitOK)
	}
	// Any leftover positional argument is unsupported — fail rather than boot.
	if fs.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "iclic-host-agent: unexpected argument %q\n", fs.Arg(0))
		os.Exit(exitUsage)
	}
}

// acquireSingleInstanceLock takes an exclusive, non-blocking advisory lock so
// only one agent runs per host. A duplicate (orphan after upgrade, a second
// systemd start, or a manual run) exits immediately instead of racing
// heartbeats. Best-effort in dev: if the lock dir is missing/unwritable we warn
// and continue; in prod the installer-created state dir always exists. (#25)
func acquireSingleInstanceLock() {
	path := os.Getenv("ICLIC_AGENT_LOCK_FILE")
	if path == "" {
		path = defaultLockFile
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		slog.Warn("single-instance lock unavailable, continuing", "path", path, "err", err)
		return
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		slog.Error("another agent instance is already running, exiting", "lock", path, "err", err)
		os.Exit(exitAlreadyRunning)
	}
	instanceLockFile = f // hold the fd open for the process lifetime
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
