# ICLIC Host Agent

A small, single-purpose monitoring agent that reports host-level health from a
server to the ICLIC license authority. Built to be **boring and auditable** —
ONPREM customers should be able to read every line before installing it.

## What it does

Every 60 seconds, the agent reads a small set of metrics from the local
operating system and POSTs them to ICLIC over HTTPS, authenticated with a
PAT-style bearer key (`Bearer <kid>.<secret>`) that was issued during
enrollment. That is the entire job.

## What it reads from the host

| Source | Used for |
|--------|----------|
| `/proc/uptime`, `/proc/loadavg`, `/proc/meminfo` | uptime, CPU load, RAM usage |
| `statfs(2)` on each configured mount | disk usage per mount |
| `/etc/os-release`, `uname(2)` | OS name/version, kernel |
| `docker version --format` (if installed) | Docker version |
| `apt list --upgradable` (Debian/Ubuntu) or `dnf check-update` (RHEL) | pending OS security updates count |

## What it does NOT do

- No shell execution beyond the two read-only update-list commands above
- No file writes outside its own state file (`/var/lib/iclic-host-agent/state.json`)
- No outbound traffic except to the configured ICLIC URL
- No reading of `/etc/passwd`, `/home`, application data, logs, or anything
  outside the system metrics surface
- No remote-control endpoint — the agent only **pushes**, never accepts inbound
  connections

## Filesystem layout

```
/opt/iclic-host-agent/iclic-host-agent      # binary
/etc/iclic-host-agent/config.json           # 0640 root:iclic-agent
/var/lib/iclic-host-agent/state.json        # 0600 iclic-agent:iclic-agent
/etc/systemd/system/iclic-host-agent.service
```

The agent runs as the `iclic-agent` system user. It needs a tiny sudoers entry
to read the security-update list (this is the one privileged operation) — the
installer prints the exact line and asks before writing it.

## Install

After registering the server in ICLIC and generating an enrollment token:

```bash
curl -fsSL https://github.com/icombilisim/ICLIC-Host-Agent/releases/latest/download/install.sh \
  | TOKEN=<one-shot-token> ICLIC_URL=https://iclic.icombilisim.com bash
```

The token is single-use and TTL-capped. The installer exchanges it at
`POST /api/v1/agent/enroll` for a permanent bearer (`<kid>.<secret>`),
writes the config, installs the systemd unit, and starts the service.

## Verify

```bash
systemctl status iclic-host-agent
journalctl -u iclic-host-agent -f
```

A successful first heartbeat appears in ICLIC's Server detail page within
60 seconds; the server's `enrollment_status` flips from `PENDING_ENROLLMENT`
to `HEALTHY`.

## Protocol

The heartbeat payload contract lives in [`docs/protocol.md`](docs/protocol.md).
Both the agent and ICLIC pin a `protocolVersion`. ICLIC accepts the last N
versions; the agent emits the latest it knows.

## Build from source

```bash
go build -o iclic-host-agent ./cmd/agent
```

Reproducible-build flags and signed releases land in a follow-up issue.

## License

MIT. See [`LICENSE`](LICENSE).
