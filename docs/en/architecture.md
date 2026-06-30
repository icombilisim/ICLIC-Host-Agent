# Architecture

> **Version** v0.15.0 · **Last updated** 2026-06-22 · **Canonical language** English
> · part of the [ICLIC Host Agent docs](../README.md)

## Module map

```
cmd/
└─ agent/main.go          entrypoint: flags, single-instance lock, wire-up, run loop

internal/
├─ config/
│  └─ config.go           JSON config load ($ICLIC_AGENT_CONFIG, default
│                         /etc/iclic-host-agent/config.json) + env overrides
├─ heartbeat/
│  └─ heartbeat.go        AgentVersion constant (release-please owned), 60 s tick,
│                         POST /heartbeat + per-item /runtime-instances/heartbeat
├─ collectors/
│  ├─ loader.go           reads collectors.d/*.yaml, merges bindings each tick
│  ├─ binding.go          binding shape (id, primitive, args, output_key)
│  ├─ registry.go         primitive name → implementation
│  ├─ runner.go           per-tick execution: timeout budget, WARN-on-error
│  ├─ services.go         runtime.services → runtime_instances signals
│  └─ primitives_*.go     28 built-ins: procfs, os, disk, exec, systemd,
│                         net (tcp/http/ssl), file, apt, docker
└─ control/
   ├─ control.go          outbound WebSocket, capability advertisement, verb dispatch
   ├─ config.go           control.yaml allow-list (absent = serve nothing)
   └─ metrics.go          metrics.live sampler (CPU/mem/load frames)

configs/                  shipped YAML profiles (00-linux-host … 93-vitals) + services.d/
installer/                install.sh, deploy-all.sh, systemd unit, inventory examples
```

## Runtime model

The agent is a single Go process supervised by systemd
(`Restart=on-failure`). On start it:

1. Parses flags. `--version` prints `AgentVersion` and exits; unknown args
   fail-fast. A **single-instance lock** (flock) prevents duplicate agents from
   racing the same heartbeat — a `--version` invocation must never boot a full
   agent. (#26)
2. Loads `config.json` (enrolment `kid`/`secret`, `iclic_url`, interval).
3. Applies a Go runtime soft memory cap (`debug.SetMemoryLimit`, default ~384 MB,
   overridable via `GOMEMLIMIT`).
4. Starts the **heartbeat loop** (60 s tick) and, if `control.yaml` permits, the
   **control channel** goroutine.

There is **no exponential backoff**. The systemd restart policy and the fixed
60-second tick are deliberately the only retry mechanism — heartbeats are
idempotent state-overwrites, so a missed tick simply self-heals on the next one.

## Heartbeat path

```
tick (60 s)
  → loader: read collectors.d/*.yaml → merged binding list
  → runner: execute each binding (per-binding timeout, 30 s total budget)
            primitive error → WARN + omit that key (never crash)
  → assemble metrics{} map
  → POST /api/v1/server/{id}/heartbeat   (Bearer <kid>.<secret>)
  → for each metrics.runtime_instances[i]:
        POST /api/v1/server/runtime-instances/heartbeat   (per-item, non-fatal)
```

The collector pipeline is **YAML-driven**: primitives are wired up by binding
files read fresh every tick, so adding/removing a check needs **no agent
restart**. See [`collectors.md`](collectors.md) for the primitive reference and
[`protocol.md`](protocol.md) for the wire contract.

## Control channel (on-demand, opt-in)

Beyond the heartbeat the agent keeps a single **outbound** WebSocket to ICLIC
(`wss://<iclic>/api/v1/server/control`, same `<kid>.<secret>` bearer). ICLIC
rides this held socket to *request* on-demand data and actions; the agent is the
authority and serves only a closed, typed verb set. **This is not a remote
shell — there is no arbitrary command execution, ever.**

- **Outbound only.** The agent dials ICLIC; no inbound port. Works behind
  NAT/firewalls, same trust path as the heartbeat.
- **Request/response, the agent decides.** ICLIC sends a typed `req`; the agent
  validates it locally and streams `res` frames back. Unknown or unpermitted
  verbs are refused, not run.
- **Opt-in, default OFF.** Only verbs explicitly enabled in
  `/etc/iclic-host-agent/control.yaml` (operator-owned) are served. With no such
  file the agent connects but refuses every request.
- **Capability advertisement.** On connect the agent reports its OS and the
  exact verbs/targets it permits; the Fleet UI shows only what each host allows.
- **Destructive actions gated.** Management verbs (restart, deploy, prune) each
  need their own opt-in; destructive ones additionally require an operator 2FA
  step-up on the ICLIC side, and every request is audited.

### Shipped verbs (read)

`logs.tail` (live/follow) · `proc.top` · `proc.top.live` (auto-refreshing top) ·
`disk.df` · `net.listen` · `cron.list` (crontabs + cron.d + systemd timers) ·
`svc.status` (running + failed services) · `svc.list` (full service inventory) ·
`pkg.list` (installed OS packages, dpkg/rpm) · `docker.ps` · `metrics.live`
(CPU/mem/load samples). `svc.list` and `pkg.list` back the on-demand server
report (ICLIC #766). Write/management verbs are the next phase (ICLIC #339).

### Opt-in config

```yaml
# /etc/iclic-host-agent/control.yaml   (absent = the channel serves nothing)
control:
  enabled: true
  logs:
    enabled: true
    default_lines: 200
    max_lines: 2000            # the agent caps this; ICLIC cannot exceed it
    max_follow_seconds: 600    # live tails auto-stop after this
    sources:                   # logical name -> concrete source (per host)
      icglb: { type: docker,   container: icosys-icglb }
      nginx: { type: file,     path: /var/log/nginx/error.log }
  top:   { enabled: true }     # proc.top + proc.top.live
  df:    { enabled: true }     # disk.df
  ports: { enabled: true }     # net.listen
  cron:  { enabled: true }     # cron.list
  svc:   { enabled: true }     # svc.status + svc.list
  pkg:   { enabled: true }     # pkg.list (installed OS packages)
  docker: { enabled: true }    # docker.ps
  # actions: (write verbs — restart/deploy/prune) land with ICLIC #339
```

The defaults (`default_lines=200`, `max_lines=2000`, `max_follow_seconds=600`)
are enforced agent-side — ICLIC cannot exceed the cap the host declares.

## Filesystem layout

```
/opt/iclic-host-agent/
├─ bin/
│  ├─ iclic-host-agent-v0.14.0     # versioned binary
│  └─ iclic-host-agent-v0.15.0     # versioned binary (after upgrade)
└─ iclic-host-agent               # symlink → bin/iclic-host-agent-v0.15.0

/etc/iclic-host-agent/
├─ config.json                    # 0640 root:iclic-agent — enrolment creds
├─ control.yaml                   # optional — control-channel allow-list
└─ collectors.d/
   ├─ 00-linux-host.yaml
   └─ … (whichever profiles were activated)

/var/lib/iclic-host-agent/
└─ state.json                     # 0600 iclic-agent:iclic-agent

/etc/systemd/system/iclic-host-agent.service
/etc/systemd/system/iclic-host-agent.service.d/   # operator drop-ins (memory, env, pprof)
```

The agent runs as the `iclic-agent` system user. Versioned binaries + a
`current` symlink make rollback one `ln -sfn` away.

## Memory & diagnostics

v0.3.x leaked memory on long uptimes (a shared `http.Transport` was missing).
v0.4.0+ fixed it and layered defence:

- **Go soft cap:** `debug.SetMemoryLimit` (~384 MB default), `GOMEMLIMIT` override.
- **systemd cgroup hard cap:** operators add a `memory.conf` drop-in
  (`MemoryHigh=384M`, `MemoryMax=512M`) so a runaway gets OOM-killed and restarted.
- **pprof on loopback only:** `127.0.0.1:6133/debug/pprof/*`, reachable only via
  SSH port-forward; disable with `ICLIC_AGENT_PPROF_ADDR=disabled`.

See [`deployment.md`](deployment.md) §"Memory control & diagnostics" for the
operator commands.
