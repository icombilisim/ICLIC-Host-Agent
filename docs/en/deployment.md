# Deployment Guide — release, install, upgrade, rollback

> **Version** v0.15.0 · **Last updated** 2026-06-22 · **Canonical language** English
> · part of the [ICLIC Host Agent docs](../README.md)

A step-by-step guide to releasing, installing, upgrading, and rolling back the
ICLIC Host Agent. Nothing here is meant to be known by heart — every step says
what happens.

---

## 1. What the system does

A small Go binary (`iclic-host-agent`) runs on each server. Every **60 seconds**
it reads `/etc/iclic-host-agent/collectors.d/*.yaml` to learn what to measure
(CPU, RAM, MySQL port, ICOSYS service health, …) and POSTs the result to ICLIC
(`/api/v1/server/{id}/heartbeat`). It opens no port and accepts no command — it
is push-only. To see the signals: **ICLIC → Servers → the server** shows host
metrics and the agent version.

## 2. Architecture at a glance

```
  ┌──────────────────────────┐
  │  ICOSYS / DevOps / ICLIC │
  │         host             │
  │  iclic-host-agent        │
  │  (systemd unit, 60 s tick)│
  │     reads /proc, os-release,
  │     docker.sock, http,   │
  │     collectors.d/*.yaml  │
  │            │             │
  │            ▼  HTTPS       │
  │  POST /api/v1/server/{id}/heartbeat
  │  Authorization: Bearer <kid>.<secret>
  └────────────┬─────────────┘
               │
               ▼
  ┌──────────────────────────┐
  │  ICLIC  (Spring Boot 8001)│
  │  heartbeat controller →   │
  │  Server timeline → detail │
  └──────────────────────────┘
```

The agent dials ICLIC; ICLIC never dials the agent. Outbound HTTPS is enough; no
inbound port is required.

## 3. Profiles — what goes on which host

The agent learns its "measure this" list from YAML profiles. Each YAML is a
**profile**; the operator decides which to install. See
[`collectors.md`](collectors.md) for the primitive reference.

| Profile | File | What it measures |
|---|---|---|
| `host` | `00-linux-host.yaml` | CPU, RAM, disk, uptime, OS, kernel — **REQUIRED ON EVERY HOST** |
| `docker` | `10-docker.yaml` | Container count + per-container stats |
| `systemd` | `20-systemd.yaml` | cgroup CPU/MEM of systemd units |
| `icosys` | `30-icosys-actuator.yaml` | 6 ICOSYS Spring Boot services (8010–8060) |
| `mysql` | `40-mysql.yaml` | MySQL port + version |
| `redis` | `50-redis.yaml` | Redis port + ping + version |
| `nginx` | `60-nginx.yaml` | nginx service + 80/443 + version |
| `iclic` | `70-iclic.yaml` | ICLIC actuator (port 8001) |
| `devops` | `80-devops-stack.yaml` | Nexus + SonarQube + Dokploy + Postgres |

**Rule:** add a profile only for what the host actually runs. Don't probe a port
nothing listens on.

| Host | Profiles |
|---|---|
| ICOSYS test/prod | `host,docker,systemd,icosys,mysql,redis,nginx` |
| DevOps | `host,docker,systemd,devops` |
| ICLIC prod | `host,docker,systemd,iclic` |

## 4. Release flow (maintainers) — release-please

Releases are automated by [release-please](https://github.com/googleapis/release-please).
**Do not hand-bump `AgentVersion` or push `v*` tags manually** — release-please
owns both (manifest `.release-please-manifest.json`, config
`release-please-config.json`).

```
1. Land Conventional-Commit PRs on main  (feat: → minor, fix: → patch)
2. release-please opens/updates a release PR:
      chore(main): release X.Y.Z
   → bumps AgentVersion (x-release-please-version annotation in
     internal/heartbeat/heartbeat.go) + CHANGELOG
3. Merge the release PR
   → release-please tags vX.Y.Z + creates the GitHub Release
   → the build job attaches:
        iclic-host-agent-linux-amd64
        iclic-host-agent-linux-arm64
        configs.tar.gz   (all YAML profiles)
        install.sh
        iclic-host-agent.service
        SHA256SUMS
4. Roll out: deploy-all.sh against the prod inventory once smoke-tested
```

## 5. First install (new host)

After registering the server in ICLIC and generating a one-shot enrolment token:

```bash
curl -fsSL https://github.com/icombilisim/ICLIC-Host-Agent/releases/latest/download/install.sh \
  -o /tmp/install.sh

sudo TOKEN=<one-shot-token> \
     ICLIC_URL=https://iclic.app \
     PROFILES=host,docker,systemd,icosys \
     bash /tmp/install.sh
```

`install.sh` then:

```
a. creates the iclic-agent user
b. lays out /opt/iclic-host-agent dirs
c. downloads the latest release + verifies SHA256
d. writes the binary as bin/iclic-host-agent-vX.Y.Z
e. points the symlink iclic-host-agent → bin/...vX.Y.Z
f. POSTs TOKEN to /api/v1/agent/enroll → permanent bearer (<kid>.<secret>)
g. writes /etc/iclic-host-agent/config.json
h. copies the requested PROFILES into collectors.d/
i. installs + starts the systemd unit
```

**Token note:** the token is single-use and TTL-capped. If enrolment fails,
generate a fresh token from ICLIC and start over — a used token is rejected.

## 6. Upgrade (re-run on an enrolled host)

```bash
# Latest release, current profiles (no TOKEN — config.json already exists)
sudo bash /tmp/install.sh

# Pin a specific tag
sudo AGENT_VERSION=v0.15.0 bash /tmp/install.sh

# Add or change profiles
sudo PROFILES=host,docker,systemd,icosys,mysql,redis bash /tmp/install.sh
```

`config.json` is preserved. The new binary lands as `bin/iclic-host-agent-<tag>`,
the symlink retargets, and systemd restarts the unit. The previous binary stays
on disk for rollback.

## 7. Fleet upgrade — `deploy-all.sh`

`deploy-all.sh` SSHes to each host in turn and runs `install.sh`.

```bash
cd installer
cp inventory.example inventory.local
$EDITOR inventory.local        # one host per line: host:profiles[:user[:port]]
bash deploy-all.sh inventory.local v0.15.0
```

Per-host failures don't abort the loop; a summary lists succeeded vs. failed
hosts and the exit code equals the number of failures.

**Preconditions:** password-less SSH (key-based) and `sudo -n bash install.sh`
(NOPASSWD) on every target. **`deploy-all.sh` is upgrade-only** — first-time
enrolment is done per host because each TOKEN is one-shot and per-server.

## 8. Rollback

The previous binary is already on disk — just retarget the symlink:

```bash
ssh icadmin@<host>
ls /opt/iclic-host-agent/bin/        # see which versions exist
sudo ln -sfn /opt/iclic-host-agent/bin/iclic-host-agent-v0.14.0 \
              /opt/iclic-host-agent/iclic-host-agent
sudo systemctl restart iclic-host-agent
journalctl -u iclic-host-agent -n 50
```

It takes ~5 seconds and touches no config. Fleet-wide rollback:
`deploy-all.sh inventory.local v0.14.0` (install.sh is idempotent, so it
"upgrades" back to the older tag).

## 9. Verify — installed and running?

**On the host:**

```bash
systemctl status iclic-host-agent
journalctl -u iclic-host-agent -f          # watch the 60 s ticks
ls /etc/iclic-host-agent/collectors.d/     # active profiles
ls -la /opt/iclic-host-agent/iclic-host-agent   # which version the symlink points to
```

**In ICLIC:** Servers → the server → `last_seen_at` should be fresher than 60 s,
`agent_version` should be your tag, and Server Detail → "Host Metrics" should
show real CPU/RAM/disk. "Heartbeat History" shows the raw payload, including the
profile-specific keys (`mysql_running`, `nginx_version`, …).

## 10. Troubleshooting

| Symptom | Check |
|---|---|
| install.sh failed, host still not enrolled | Token expired? Generate a new one. `curl https://iclic.app/actuator/health` reachable? DNS resolves? |
| systemctl active but ICLIC sees no heartbeat | `journalctl -u iclic-host-agent -n 100`; `cat config.json` — is `iclic_url` correct? If stuck on `PENDING_ENROLLMENT`, delete config.json + re-run install.sh with a fresh TOKEN. |
| A collector key is missing | Is the profile in `collectors.d/`? Is the probe runnable on this host (`nginx -v`, `redis-server --version` in PATH)? `journalctl -u iclic-host-agent | grep WARN`. |
| deploy-all.sh hangs | `ssh -o BatchMode=yes <host>` must connect without a prompt; `sudo -n true` must work (NOPASSWD). |
| SHA256 mismatch | Download truncated (or, very unlikely, a tampered release) — re-run install.sh. |

## 11. Quick command reference

```bash
# First install (single host)
sudo TOKEN=xyz ICLIC_URL=https://iclic.app \
     PROFILES=host,docker,systemd,icosys bash install.sh

# Upgrade (single host)
sudo bash install.sh

# Pin / downgrade to a specific tag
sudo AGENT_VERSION=v0.14.0 bash install.sh

# Fleet upgrade
bash deploy-all.sh inventory.local v0.15.0

# Rollback (single host)
sudo ln -sfn /opt/iclic-host-agent/bin/iclic-host-agent-v0.14.0 \
              /opt/iclic-host-agent/iclic-host-agent
sudo systemctl restart iclic-host-agent

# Verify
systemctl status iclic-host-agent
journalctl -u iclic-host-agent -f
```

## 12. File locations (cheat sheet)

| Path | Contents |
|---|---|
| `/opt/iclic-host-agent/iclic-host-agent` | Symlink → active binary |
| `/opt/iclic-host-agent/bin/iclic-host-agent-vX.Y.Z` | Versioned binaries |
| `/etc/iclic-host-agent/config.json` | Enrolment credentials (`0640 root:iclic-agent`) |
| `/etc/iclic-host-agent/control.yaml` | Control-channel allow-list (optional) |
| `/etc/iclic-host-agent/collectors.d/` | Active YAML profiles |
| `/var/lib/iclic-host-agent/state.json` | Agent state |
| `/etc/systemd/system/iclic-host-agent.service` | systemd unit |
| `…/iclic-host-agent.service.d/*.conf` | Operator drop-ins (memory, env, pprof) |

## 13. Memory control & diagnostics (v0.4.0+)

v0.3.x leaked memory on long uptimes. v0.4.0+ uses a shared `http.Transport`,
sets a `GOMEMLIMIT` default, and adds loopback pprof. Two defensive layers are
still recommended:

**a) Go runtime soft cap** — the agent calls `debug.SetMemoryLimit(~384 MB)` on
start. Override:

```bash
# /etc/systemd/system/iclic-host-agent.service.d/env.conf
[Service]
Environment="GOMEMLIMIT=512MiB"
```

**b) systemd cgroup hard cap** — recommended on every host:

```bash
sudo mkdir -p /etc/systemd/system/iclic-host-agent.service.d
sudo tee /etc/systemd/system/iclic-host-agent.service.d/memory.conf >/dev/null <<'EOF'
[Service]
MemoryHigh=384M
MemoryMax=512M
EOF
sudo systemctl daemon-reload && sudo systemctl restart iclic-host-agent
```

**c) pprof (loopback only)** — `127.0.0.1:6133/debug/pprof/*`, reachable via SSH
port-forward:

```bash
ssh -L 6133:127.0.0.1:6133 icadmin@<host>
go tool pprof -http=:0 http://localhost:6133/debug/pprof/heap
```

Disable with `ICLIC_AGENT_PPROF_ADDR=disabled` (env drop-in), or move it with a
value like `127.0.0.1:6200`.

---

**Keep this alive:** reflect every release or flow change here. A doc that rots
is worse than no doc.
