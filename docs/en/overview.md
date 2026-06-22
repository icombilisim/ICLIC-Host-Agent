# ICLIC Host Agent — Overview

> **Version** v0.15.0 · **Last updated** 2026-06-22 · **Canonical language** English
> · part of the [ICLIC Host Agent docs](../README.md)

> Reference docs for humans **and AI coding tools**. Read this first, then
> [`architecture.md`](architecture.md), [`protocol.md`](protocol.md),
> [`collectors.md`](collectors.md), [`deployment.md`](deployment.md).

## What it is

A small, single-purpose **monitoring agent** that reports host + service health
from a server to the **ICLIC license authority**. It is built to be **boring and
auditable** — ONPREM customers should be able to read every line before
installing it.

Every 60 seconds the agent runs the bindings declared in
`/etc/iclic-host-agent/collectors.d/*.yaml`, packs the results into a single
map, and POSTs them to ICLIC over HTTPS authenticated with a PAT-style bearer
key (`Bearer <kid>.<secret>`) issued during enrolment. That heartbeat is
**push-only**.

## Why it exists

- **Single pane of fleet health:** CPU/mem/disk, container state, systemd units,
  and ICOSYS/ICLIC service versions for every managed host land in one ICLIC view.
- **No inbound exposure:** the monitored host opens no port. The agent dials out;
  ICLIC never connects in. Works behind NAT/firewalls.
- **Operator-driven, not code-driven:** what gets measured is declared in YAML
  bindings, not compiled into the binary. New checks = drop a YAML file.
- **Auditable by the customer:** no hidden behaviour, no shell pass-through, no
  reading of application data or `/home`.

## What it does NOT do

- No shell execution beyond what's declared in YAML bindings.
- No file writes outside its own state file (`/var/lib/iclic-host-agent/state.json`).
- No outbound traffic except to the configured ICLIC URL.
- No reading of `/etc/passwd`, `/home`, or application data.
- No inbound port and **no shell pass-through** — the control channel is an
  agent-**dialed outbound** socket that serves only an opt-in, closed set of
  typed verbs (default: OFF). ICLIC requests; the agent decides.

## Two channels

| Channel | Direction | Cadence | Purpose |
|---------|-----------|---------|---------|
| **Heartbeat** | agent → ICLIC (HTTPS POST) | every 60 s | Push host + service metrics |
| **Control** | agent → ICLIC (outbound WebSocket) | held open, opt-in | ICLIC *requests* on-demand data over a closed verb set |

Both use the same `<kid>.<secret>` bearer. Neither opens an inbound port. See
[`architecture.md`](architecture.md) for the control channel detail.

## Collector profiles (what ships)

The agent's metric body is built from operator-selected YAML profiles. Each can
be installed independently — a host running only nginx and Postgres uses
`host,nginx,devops`, nothing else.

| Profile | File | Covers |
|---------|------|--------|
| `host` | `00-linux-host.yaml` | CPU load, memory, disk, uptime, OS, kernel, security-update count |
| `docker` | `10-docker.yaml` | Container summary + per-container stats + published ports via `/var/run/docker.sock` |
| `systemd` | `20-systemd.yaml` | Resource usage of named systemd units (cgroup-driven) |
| `icosys` | `30-icosys-actuator.yaml` | ICOSYS Spring Boot services (icglb 8010 … icwfl 8060) — `runtime_instances`, health, version, git commit |
| `mysql` | `40-mysql.yaml` | MySQL liveness + version |
| `redis` | `50-redis.yaml` | Redis liveness + ping + version |
| `nginx` | `60-nginx.yaml` | nginx liveness + version + 80/443 ports |
| `iclic` | `70-iclic.yaml` | ICLIC Spring Boot actuator (port 8001) |
| `devops` | `80-devops-stack.yaml` | Nexus + SonarQube + Dokploy + Postgres |

Additional profiles ship for TLS expiry (`90-tls.yaml`), backups
(`91-backups.yaml`), uptime (`92-uptime.yaml`), and host vitals
(`93-vitals.yaml`). See [`collectors.md`](collectors.md).

## Core invariants (do not break)

1. **Outbound only.** The agent dials ICLIC; it never opens an inbound port.
2. **No arbitrary command execution.** The control channel serves only an
   opt-in, closed, typed verb set — never a shell.
3. **Opt-in by default.** With no `control.yaml`, the agent connects but refuses
   every control request. Destructive verbs additionally require ICLIC-side 2FA.
4. **Single ICLIC URL.** One agent → one ICLIC. No other outbound destination.
5. **Versioned binaries.** Rollback is one `ln -sfn` of the `current` symlink —
   the previous binary stays on disk.
6. **release-please owns the version.** Never hand-bump `AgentVersion` or push
   `v*` tags manually.

## Status (v0.15.0)

Heartbeat backbone is live across the fleet: 28 YAML-driven primitives, nine
shipped collector profiles, `runtime_instances` deployment signals, and a
PII-free, push-only heartbeat. Control channel **read verbs** shipped —
`logs.tail`, `proc.top`, `proc.top.live`, `disk.df`, `net.listen`, `cron.list`,
plus `metrics.live` (CPU/mem/load streaming). **Write/management verbs**
(restart/deploy/prune, 2FA-gated on the ICLIC side) are the next phase.

Tracking: ICLIC #40 · #337 (read) · #348 (live top + cron) · #339 (write).
