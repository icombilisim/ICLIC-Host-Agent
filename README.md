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
That is the entire job — push-only, no inbound port, no remote control.

The collector engine is **YAML-driven**: 21 built-in primitives
(`procfs.*`, `os.*`, `disk.*`, `exec`, `systemctl.*`, `systemd.resources`,
`tcp.connect`, `http.get`, `http.get_json`, `file.stat`, `apt.*`,
`docker.*`) are wired up by binding files. New components are added by
dropping a YAML file — no agent restart required. See
[`docs/collectors.md`](docs/collectors.md) for the primitive reference.

## What ships with the agent — collector profiles

| Profile | File | Covers |
|---------|------|--------|
| `host`    | `00-linux-host.yaml`     | CPU load, memory, disk, uptime, OS, kernel, security-update count |
| `docker`  | `10-docker.yaml`         | Container summary + per-container stats via `/var/run/docker.sock` |
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
- No reading of `/etc/passwd`, `/home`, application data, or logs
- No remote-control endpoint — agent **pushes**, never accepts inbound

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

## Install (first time, per host)

After registering the server in ICLIC and generating a one-shot
enrolment token:

```bash
curl -fsSL https://github.com/icombilisim/ICLIC-Host-Agent/releases/latest/download/install.sh \
  -o /tmp/install.sh

sudo TOKEN=<one-shot-token> \
     ICLIC_URL=https://iclic.icombilisim.com \
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

1. Bump `AgentVersion` in `internal/heartbeat/heartbeat.go`
2. Commit + push
3. `git tag v0.X.Y && git push --tags`
4. `.github/workflows/release.yml` builds linux-amd64 + linux-arm64,
   computes `SHA256SUMS`, bundles `configs/` as `configs.tar.gz`, and
   creates a GitHub Release with all assets attached
5. Run `deploy-all.sh` against the prod inventory once smoke-tested
   on devops

The workflow rejects tag/const drift — it errors out if the tag
doesn't match `AgentVersion`.

## Protocol

The heartbeat payload contract lives in [`docs/protocol.md`](docs/protocol.md).
Both the agent and ICLIC pin a `protocolVersion`. ICLIC accepts the last
N versions; the agent emits the latest it knows.

## Build from source

```bash
go build -o iclic-host-agent ./cmd/agent
```

## License

MIT. See [`LICENSE`](LICENSE).
