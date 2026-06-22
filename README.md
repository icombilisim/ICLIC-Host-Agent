# ICLIC Host Agent

A small, single-purpose monitoring agent that reports host + service health
from a server to the ICLIC license authority. Built to be **boring and
auditable** — ONPREM customers should be able to read every line before
installing it.

## What it does

Every 60 seconds the agent runs the bindings declared in
`/etc/iclic-host-agent/collectors.d/*.yaml`, packs the results into a
single map, and POSTs them to ICLIC over HTTPS authenticated with a
PAT-style bearer key (`Bearer <kid>.<secret>`) issued during enrolment.
That heartbeat is push-only. Alongside it the agent keeps a single
**outbound** control channel (see [Control channel](#control-channel-on-demand-opt-in))
over which ICLIC can *request* on-demand data — the agent still never accepts an
inbound connection, and serves only an opt-in, closed set of typed verbs.

The collector engine is **YAML-driven**: 21 built-in primitives
(`procfs.*`, `os.*`, `disk.*`, `exec`, `systemctl.*`, `systemd.resources`,
`tcp.connect`, `http.get`, `http.get_json`, `file.stat`, `apt.*`,
`docker.*`) are wired up by binding files. New components are added by
dropping a YAML file — no agent restart required. See
[`docs/en/collectors.md`](docs/en/collectors.md) for the primitive reference
([Türkçe](docs/tr/toplayicilar.md)). Full docs index: [`docs/`](docs/README.md).

## What ships with the agent — collector profiles

| Profile | File | Covers |
|---------|------|--------|
| `host`    | `00-linux-host.yaml`     | CPU load, memory, disk, uptime, OS, kernel, security-update count |
| `docker`  | `10-docker.yaml`         | Container summary + per-container stats + published ports via `/var/run/docker.sock` |
| `systemd` | `20-systemd.yaml`        | Resource usage of named systemd units (cgroup-driven) |
| `icosys`  | `30-icosys-actuator.yaml` | ICOSYS Spring Boot services (icglb 8010, icbpm 8020, icdms 8030, ichrm 8040, icasm 8050, icwfl 8060) — structured `runtime_instances`, health, version, git commit |
| `mysql`   | `40-mysql.yaml`          | MySQL liveness + version (no auth needed for version) |
| `redis`   | `50-redis.yaml`          | Redis liveness + ping + version |
| `nginx`   | `60-nginx.yaml`          | nginx liveness + version + 80/443 ports |
| `iclic`   | `70-iclic.yaml`          | ICLIC Spring Boot actuator (port 8001) |
| `devops`  | `80-devops-stack.yaml`   | Nexus + SonarQube + Dokploy + Postgres |

Each profile can be installed independently — a host running only nginx
and Postgres uses `host,nginx,devops`, nothing else.

## What it does NOT do

- No shell execution beyond what's declared in YAML bindings
- No file writes outside its own state file (`/var/lib/iclic-host-agent/state.json`)
- No outbound traffic except to the configured ICLIC URL
- No reading of `/etc/passwd`, `/home`, or application data
- No inbound port and **no shell pass-through** — the control channel is an
  agent-**dialed outbound** socket that serves only an opt-in, closed set of
  typed verbs (default: OFF). ICLIC requests; the agent decides.

## Filesystem layout

```
/opt/iclic-host-agent/
├─ bin/
│  ├─ iclic-host-agent-v0.3.0     # versioned binary
│  └─ iclic-host-agent-v0.4.0     # versioned binary (after upgrade)
└─ iclic-host-agent               # symlink → bin/iclic-host-agent-v0.4.0

/etc/iclic-host-agent/
├─ config.json                    # 0640 root:iclic-agent — enrolment creds
└─ collectors.d/
   ├─ 00-linux-host.yaml
   ├─ 10-docker.yaml
   └─ … (whichever profiles were activated)

/var/lib/iclic-host-agent/
└─ state.json                     # 0600 iclic-agent:iclic-agent

/etc/systemd/system/iclic-host-agent.service
```

The agent runs as the `iclic-agent` system user. Versioned binaries +
symlink mean rollback is one `ln -sfn` away.

## Control channel (on-demand, opt-in)

Beyond the periodic heartbeat the agent keeps a single **outbound** WebSocket to
ICLIC (`wss://<iclic>/api/v1/server/control`, authenticated with the same
`<kid>.<secret>` as the heartbeat). ICLIC rides this held socket to *request*
on-demand data and actions; the agent is the authority and serves only a closed,
typed set of verbs. **This is not a remote shell — there is no arbitrary command
execution, ever.**

- **Outbound only.** The agent dials ICLIC; no inbound port is opened. Works
  behind NAT/firewalls, same trust path as the heartbeat.
- **Request/response, the agent decides.** ICLIC sends a typed `req` (e.g. tail a
  log, list processes); the agent validates it locally and streams `res` frames
  back. Unknown or unpermitted verbs are refused, not run.
- **Opt-in, default OFF.** Only verbs explicitly enabled in
  `/etc/iclic-host-agent/control.yaml` (operator-owned) are served. With no such
  file the agent connects but refuses every request.
- **Capability advertisement.** On connect the agent reports its OS and the exact
  verbs/targets it permits; the Fleet UI shows only what each host allows.
- **Destructive actions gated.** Management verbs (restart, deploy, prune) each
  need their own opt-in; destructive ones additionally require an operator 2FA
  step-up on the ICLIC side, and every request is audited.

**Status:** read verbs shipped — `logs.tail` (live/follow), `proc.top`,
`proc.top.live` (auto-refreshing top), `disk.df`, `net.listen`, `cron.list`
(crontabs + cron.d + systemd timers). Write/management verbs (restart/deploy/prune,
2FA-gated on the ICLIC side) are the next phase. Tracking: ICLIC #40 · #337 (read)
· #348 (live top + cron) · #339 (write).

Opt-in config (only what you list is ever served):

```yaml
# /etc/iclic-host-agent/control.yaml   (absent = the channel serves nothing)
control:
  enabled: true
  logs:
    enabled: true
    default_lines: 200
    max_lines: 2000            # the agent caps this; ICLIC cannot exceed it
    max_follow_seconds: 600    # live tails auto-stop after this
    sources:                   # logical name -> concrete source (flexible per host)
      icglb: { type: docker,   container: icosys-icglb }
      nginx: { type: file,     path: /var/log/nginx/error.log }
      # iclic: { type: journald, unit: iclic-backend }
  top:   { enabled: true }     # proc.top + proc.top.live — process list (snapshot + live)
  df:    { enabled: true }     # disk.df   — filesystem usage
  ports: { enabled: true }     # net.listen — listening ports + owning service
  cron:  { enabled: true }     # cron.list — crontabs + /etc/cron.d + systemd timers
  # actions: (write verbs — restart/deploy/prune) land with ICLIC #339
```

## Install (first time, per host)

After registering the server in ICLIC and generating a one-shot
enrolment token:

```bash
curl -fsSL https://github.com/icombilisim/ICLIC-Host-Agent/releases/latest/download/install.sh \
  -o /tmp/install.sh

sudo TOKEN=<one-shot-token> \
     ICLIC_URL=https://iclic.app \
     PROFILES=host,docker,systemd,icosys \
     bash /tmp/install.sh
```

The token is single-use and TTL-capped. The installer exchanges it at
`POST /api/v1/agent/enroll` for a permanent bearer (`<kid>.<secret>`),
writes `config.json`, drops the requested collector profiles, installs
the systemd unit, and starts the service.

## Upgrade (re-run on an enrolled host)

```bash
# Latest release, current profiles
sudo bash /tmp/install.sh

# Pin a specific tag
sudo AGENT_VERSION=v0.4.0 bash /tmp/install.sh

# Add or change profiles
sudo PROFILES=host,docker,systemd,icosys,mysql,redis bash /tmp/install.sh
```

`config.json` is preserved. The new binary lands as
`bin/iclic-host-agent-<tag>`, the `current` symlink retargets, and
systemd restarts the unit.

## Rollback

```bash
sudo ln -sfn /opt/iclic-host-agent/bin/iclic-host-agent-v0.3.0 \
              /opt/iclic-host-agent/iclic-host-agent
sudo systemctl restart iclic-host-agent
```

## Fleet upgrade — `deploy-all.sh`

For multi-host upgrades (already-enrolled hosts):

```bash
cd installer
cp inventory.example inventory.local
$EDITOR inventory.local         # one host per line: host:profiles[:user[:port]]
bash deploy-all.sh inventory.local v0.4.0
```

Per-host install.sh failures don't abort the loop; a summary at the
end lists succeeded vs. failed hosts. Exit code = number of failures.

The script assumes the operator has password-less `sudo` on each
target. First-time enrolment is intentionally not part of this loop —
each host's TOKEN is one-shot and per-server.

## Verify

```bash
systemctl status iclic-host-agent
journalctl -u iclic-host-agent -f
```

A successful first heartbeat appears in ICLIC's Server detail page
within 60 seconds; the server's `enrollment_status` flips from
`PENDING_ENROLLMENT` to `HEALTHY`.

## Releasing (maintainers)

Releases are automated by [release-please](https://github.com/googleapis/release-please):

1. Land Conventional-Commit PRs on `main` (`feat:` → minor, `fix:` → patch).
2. release-please opens/updates a **release PR** (`chore(main): release X.Y.Z`)
   that bumps `AgentVersion` (via the `x-release-please-version` annotation in
   `internal/heartbeat/heartbeat.go`) and the `CHANGELOG`.
3. Merge the release PR → release-please tags `vX.Y.Z` and creates the GitHub
   Release; the `build` job then attaches the linux binaries, `configs.tar.gz`,
   `install.sh`, the systemd unit, and `SHA256SUMS`.
4. Roll it out: `deploy-all.sh` against the prod inventory once smoke-tested.

Don't hand-bump `AgentVersion` or push `v*` tags manually — release-please owns
both (manifest: `.release-please-manifest.json`, config: `release-please-config.json`).

## Protocol

The heartbeat payload contract lives in [`docs/en/protocol.md`](docs/en/protocol.md)
([Türkçe](docs/tr/protokol.md)). Both the agent and ICLIC pin a `protocolVersion`.
ICLIC accepts the last N versions; the agent emits the latest it knows.

Full documentation (EN + TR) is indexed at [`docs/`](docs/README.md).

## Build from source

```bash
go mod tidy           # first build after pulling: resolves coder/websocket + go.sum
go build -o iclic-host-agent ./cmd/agent
```

## License

MIT. See [`LICENSE`](LICENSE).
