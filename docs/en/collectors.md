# Operator-defined collectors

> **Version** v0.20.0 · **Last updated** 2026-06-25 · **Canonical language** English
> · part of the [ICLIC Host Agent docs](../README.md)

The agent's metric body is built from one or more YAML files in
`/etc/iclic-host-agent/collectors.d/`. Each file is a flat list of *bindings*;
each binding names a built-in *primitive*, supplies its arguments, and declares
the key the result lands under in the heartbeat's `metrics` map.

Files are read alphabetically and merged into a single binding list on every
tick — there is no agent restart when you edit, add, or remove a file.

## Anatomy of a binding

```yaml
- id: cpu_load_1m            # human label, used in error logs only
  primitive: procfs.loadavg  # one of the built-ins below
  args: { window: 1m }       # forwarded verbatim to the primitive
  output_key: load_1m        # key under metrics{} where the result is placed
```

Bindings with `primitive: <unknown>`, missing `output_key`, or empty `id` are
skipped with a warning. A primitive that returns an error logs at WARN and omits
its metric for that tick — never crashes the agent.

## Built-in primitives

### procfs.loadavg

Reads `/proc/loadavg`. Returns a number (load average for the requested window).

| Arg    | Type   | Default | Description    |
|--------|--------|---------|----------------|
| window | string | `1m`    | `1m` / `5m` / `15m` |

### procfs.uptime

Reads `/proc/uptime`. Returns kernel uptime in whole seconds (int).

### procfs.memory

Reads `/proc/meminfo`. "Used" follows the modern Linux convention
`MemTotal - MemAvailable` so cache pages don't inflate the number.

| Arg   | Type   | Default    | Description |
|-------|--------|------------|-------------|
| field | string | `used_pct` | `used_pct` / `total_mb` / `used_mb` / `free_mb` / `available_mb` |

### procfs.swap

Reads `/proc/meminfo` (`SwapTotal` / `SwapFree`). A host with no swap configured
reports `0` for `used_pct` so the metric is always present.

| Arg   | Type   | Default    | Description |
|-------|--------|------------|-------------|
| field | string | `used_pct` | `used_pct` / `total_mb` / `used_mb` |

### procfs.cpu_count

Returns the number of online logical CPUs (int).

### procfs.cpu_used_pct

Samples `/proc/stat` twice and returns host CPU utilisation as a percentage
(float). Added in v0.13.0 to feed ICLIC's metric history.

### procfs.diskstats

Samples `/proc/diskstats` twice and returns aggregate disk I/O rates across real
(whole) disks: `{ read_mbps, write_mbps, iops }`.

| Arg        | Type   | Default | Description |
|------------|--------|---------|-------------|
| sample_sec | number | `1`     | Sampling window, clamped to 0.2..5 |

### procfs.netdev

Samples `/proc/net/dev` twice and returns aggregate network rates across real
interfaces: `{ rx_mbps, tx_mbps, rx_errors, tx_errors }`.

| Arg        | Type   | Default | Description |
|------------|--------|---------|-------------|
| sample_sec | number | `1`     | Sampling window, clamped to 0.2..5 |
| iface      | string | (all)   | Restrict to one interface; otherwise all non-virtual |

### os.release

Reads `/etc/os-release`. Returns one field as a string.

| Arg   | Type   | Default   | Description |
|-------|--------|-----------|-------------|
| field | string | `version` | `id` / `name` / `version` / `version_id` / `pretty_name` |

### os.hostname

Returns the kernel-reported hostname (string).

### os.kernel

Returns the running kernel release (string, e.g. `6.8.0-31-generic`).

### os.arch

Returns the agent binary's GOARCH (string, `amd64` / `arm64`).

### reboot.required

Returns `true` when `/var/run/reboot-required` exists (bool).

### disk.usage

Shells out to `df -kP`. Pseudo filesystems (tmpfs, overlay, …) are dropped
unless `exclude_pseudo: false`.

| Arg            | Type   | Default | Description |
|----------------|--------|---------|-------------|
| mount          | string | (unset) | Single mount → returns one map. Unset → returns a list of all real mounts. |
| exclude_pseudo | bool   | `true`  | Drop tmpfs / overlay / squashfs / udev / sysfs / cgroup / proc / `none` |

Per-mount shape: `{mount: "/", used_pct: 22.0, total_gb: 100}`.

### disk.max_used_pct

Same source as `disk.usage` but reduced to a single number — the highest
`used_pct` across all real mounts. The backend uses this for the one-line
"disk=72%" summary on the Servers list.

| Arg            | Type | Default | Description |
|----------------|------|---------|-------------|
| exclude_pseudo | bool | `true`  | Same as `disk.usage` |

### exec

Run an arbitrary command. The escape hatch — anything not covered by a named
primitive (WildFly CLI, jboss-cli, custom scripts) goes through this.

| Arg         | Type     | Default | Description |
|-------------|----------|---------|-------------|
| cmd         | []string | —       | argv-style; no shell expansion |
| timeout_sec | number   | `5`     | Capped at 30 |
| parse       | string   | `raw`   | `raw` / `trimmed` / `int` / `float` / `json` |
| path        | string   | (unset) | Only with `parse: json` — dotted path extracted from the decoded document. Same syntax as `http.get_json` below. |

A non-zero exit is treated as an error (metric omitted that tick).

### systemctl.is_active

Returns `true` when `systemctl is-active <unit>` reports `active` (bool).

| Arg  | Type   | Default | Description |
|------|--------|---------|-------------|
| unit | string | —       | e.g. `nginx.service` |

### systemd.resources

Reads cgroup-driven CPU + memory + restart counters for one or more units via
`systemctl show -p Id,ActiveState,SubState,LoadState,MainPID,MemoryCurrent,CPUUsageNSec,NRestarts`.
Returns a list of one map per requested unit so the server-detail "Services"
panel can render a single table.

| Arg         | Type     | Default | Description |
|-------------|----------|---------|-------------|
| units       | []string | —       | Full unit names, e.g. `icosys-icglb.service` |
| timeout_sec | number   | `4`     | Per-binding cap; one fork+exec per unit |

Per-unit shape:

```
{ unit, id, active_state, sub_state, load_state, main_pid,
  memory_mb, cpu_ns, n_restarts }
```

`cpu_ns` is the cumulative `CPUUsageNSec` counter — the backend derives a
percentage between heartbeats. Missing units come back with
`load_state: not-found` instead of dropping the row, so the UI keeps a stable
slot even for services that have been removed.

### tcp.connect

Returns `true` if a TCP connection completes within timeout (bool).

| Arg         | Type   | Default | Description |
|-------------|--------|---------|-------------|
| host        | string | —       | DNS name or IP |
| port        | number | —       | 1..65535 |
| timeout_sec | number | `2`     | |

### http.get

Single GET request.

| Arg         | Type   | Default | Description |
|-------------|--------|---------|-------------|
| url         | string | —       | Full URL |
| timeout_sec | number | `3`     | |
| expect      | string | `code`  | `code` (return status int) / `ok` (return 200..299 bool) |

### http.probe

Synthetic uptime check: one GET reporting reachability, latency, and status as a
map `{ up, latency_ms, status }`. A connection failure returns `up: false`
(`status: 0`) rather than an error — so "down" is a recorded data point, not a
missing metric. `up` is `true` for 2xx/3xx by default, or exactly `expect_status`
when set.

| Arg           | Type   | Default | Description |
|---------------|--------|---------|-------------|
| url           | string | —       | Full URL |
| timeout_sec   | number | `5`     | |
| expect_status | number | (unset) | If set, `up` requires this exact status code |

### http.get_json

Fetches a JSON document and returns either the whole body or one value pulled
out by a dotted path. The cheapest way to scrape a Spring Boot actuator
endpoint, a Nexus admin API, or any other JSON-shaped admin surface without
writing a per-target collector.

| Arg         | Type   | Default | Description |
|-------------|--------|---------|-------------|
| url         | string | —       | Full URL |
| path        | string | (unset) | Dotted path; empty = return whole document |
| header      | map    | (unset) | Extra request headers (`Authorization`, etc.) |
| basic_user  | string | (unset) | Pair with `basic_pass` for HTTP Basic auth |
| basic_pass  | string | (unset) | |
| timeout_sec | number | `4`     | |

Path syntax:

- `status` — top-level key
- `components.db.status` — nested keys
- `measurements.0.value` — numeric segment indexes into an array

Numbers and booleans come back as `float64` / `bool`; strings as `string`.
Missing keys yield `nil` and the binding's metric is omitted. Response body is
capped at 1 MB to keep one misbehaving endpoint from blowing up a tick.

### ssl.cert_expiry

Connects over TLS to `host:port` and returns the number of whole days until the
leaf (server) certificate expires (int). Feeds the `90-tls.yaml` profile so ICLIC
can alert before a cert lapses.

| Arg         | Type   | Default | Description |
|-------------|--------|---------|-------------|
| host        | string | —       | What to dial (required) |
| port        | number | `443`   | 1..65535 |
| server_name | string | (host)  | SNI to select the cert; defaults to `host` |
| timeout_sec | number | `5`     | |

### docker.containers

Talks directly to `dockerd` over `/var/run/docker.sock` (no `docker` CLI
needed) and returns one row per container plus a state-bucket summary. The agent
must be a member of the `docker` group on every host that ships this binding —
`installer/install.sh` does that automatically.

| Arg         | Type   | Default               | Description |
|-------------|--------|-----------------------|-------------|
| socket      | string | `/var/run/docker.sock` | Unix socket path |
| all         | bool   | `true`                | Include stopped containers |
| timeout_sec | number | `4`                   | |

Shape:

```
{
  total, running, exited, restarting, paused, dead, created, removing,
  list: [ { name, image, state, status, restart_count } ]
}
```

### docker.stats

Per-container CPU + memory snapshot, fetched via the docker `stats` endpoint
with `stream=false`. The CPU% is computed the same way `docker stats` does —
delta of `cpu_usage.total_usage` over `system_cpu_usage`, multiplied by online
CPUs — so a 2-core box pegged at 100% on each core reports `200.0`.

| Arg         | Type   | Default               | Description |
|-------------|--------|-----------------------|-------------|
| socket      | string | `/var/run/docker.sock` | Unix socket path |
| timeout_sec | number | `6`                   | Per-container timeout; goroutines fan out so the binding's total stays bounded |

Shape: list of

```
{ name, image, state, cpu_pct, mem_used_mb, mem_limit_mb, mem_pct,
  restart_count }
```

A single, process-lifetime HTTP client is shared across all docker primitives
and across ticks (added in v0.4.0). Earlier builds opened a fresh
`http.Transport` per request, which leaked memory on long-running hosts. (#2)

### runtime.services

Turns a configurable service registry into `runtime_instances` signals for ICLIC
Fleet and Deployment Status. Unlike one-off `exec` probes, this primitive always
emits one row per configured service: a failed container or actuator probe
becomes `STALE` with diagnostic payload, so the UI shows a broken service
instead of losing the row. (#112)

| Arg         | Type     | Default               | Description |
|-------------|----------|-----------------------|-------------|
| socket      | string   | `/var/run/docker.sock` | Docker socket used to inspect container state |
| timeout_sec | number   | `4`                   | Per-service probe timeout |
| services    | []map    | —                     | Service registry entries |

Service entry fields:

| Field           | Required | Description |
|-----------------|----------|-------------|
| product_code    | yes      | ICLIC product code, e.g. `ICOSYS` |
| component_code  | yes      | `runtime_component.code`, e.g. `icglb-services` |
| health_url      | yes      | JSON health endpoint |
| info_url        | no       | JSON info/version endpoint |
| container       | no       | Docker container name to inspect |
| probe           | no       | `http` or `docker_exec`; `docker_exec` runs `wget` inside `container` |
| instance_key    | no       | Stable identity; defaults to container or component code |
| environment     | no       | `PROD`, `TEST`, `STAGING`, or `DEV` when known |
| version_path    | no       | JSON dot path for version; defaults to `app.version`, then `build.version` |
| git_commit_path | no       | JSON dot path for commit; defaults to `git.commit.id` |

**Version source.** When `container` is set, the running version is taken from the
container's OCI image label **`org.opencontainers.image.version`** — the canonical
release version baked at build and preserved across promote-by-retag (it lives in
the image config, not the mutable tag). This is read from the same container
inspect used for the run state, so it costs no extra Docker API call and needs no
per-service config. If the image has no such label (label-less / non-ICOM images),
it falls back to the actuator `info` document (`version_path` → `app.version` →
`build.version`). The `com.icom.image.rc` label, when present, is reported as
`buildRef` (RC provenance, shown separately from the version on test rows). (#55)

Example:

```yaml
- id: icosys_runtime_instances
  primitive: runtime.services
  args:
    services:
      - product_code: ICOSYS
        component_code: icglb-services
        container: icosys-icglb
        probe: docker_exec
        health_url: http://127.0.0.1:8010/icglb/services/actuator/health
        info_url: http://127.0.0.1:8010/icglb/services/actuator/info
  output_key: runtime_instances
```

Adding a service is a catalog + config operation: create or activate the
matching `runtime_component` row in ICLIC, add one service entry to the host's
collector YAML, then wait for the next heartbeat. Removing a service is the
reverse: delete the service entry or mark the catalog component inactive.

### file.stat

| Arg   | Type   | Default  | Description |
|-------|--------|----------|-------------|
| path  | string | —        | |
| field | string | `exists` | `exists` (bool) / `size_bytes` (int) / `mtime_seconds` (int) / `age_seconds` (int, seconds since last modification). On missing path, numeric fields return `-1`. |

### file.newest_age_seconds

Returns seconds since the most recently modified regular file matching a glob —
i.e. "how old is the latest backup". Returns `-1` when nothing matches. Built for
timestamped dump dirs where the filename changes every run; alert when the age
exceeds your backup interval (e.g. `> 93600` = 26 h for a daily dump).

| Arg  | Type   | Default | Description |
|------|--------|---------|-------------|
| glob | string | —       | e.g. `/opt/iclic/mysql/backups/*.sql.gz` |

### apt.security_count

Counts pending security updates by parsing `apt-get -s upgrade`. Returns int. On
RHEL/CentOS or when apt is locked / missing / times out, returns `-1` — the
documented sentinel for "agent could not determine". Never errors so non-Debian
hosts get the right "unknown" signal until a `dnf.security_count` primitive
exists.

## Adding a new file

```yaml
# /etc/iclic-host-agent/collectors.d/10-wildfly.yaml
- id: wildfly_state
  primitive: exec
  args:
    cmd: [/opt/wildfly/bin/jboss-cli.sh, -c, /:read-attribute(name=server-state)]
    timeout_sec: 5
    parse: trimmed
  output_key: wildfly_server_state

- id: wildfly_running
  primitive: systemctl.is_active
  args: { unit: wildfly.service }
  output_key: wildfly_running

- id: wildfly_admin_port
  primitive: tcp.connect
  args: { host: 127.0.0.1, port: 9990, timeout_sec: 2 }
  output_key: wildfly_admin_port_open
```

Save the file → wait one tick (default 60 s) → ICLIC server detail page picks up
the new keys automatically. The Server Detail "Host Metrics" panel renders the
well-known keys from the linux-host profile by default; everything else is
visible via the raw payload viewer in the "Heartbeat History" panel.

## Runtime deployment status

ICLIC also accepts runtime version signals under the reserved `runtime_instances`
output key. The agent forwards each item to
`POST /api/v1/server/runtime-instances/heartbeat` after the normal host
heartbeat succeeds. This keeps Docker, systemd, WildFly, PHP, Python, .NET, Go,
and other legacy stacks on one collection path.

The easiest integration point is an operator script that prints JSON and an
`exec` binding with `parse: json`:

```yaml
# /etc/iclic-host-agent/collectors.d/40-runtime-instances.yaml
- id: runtime_instances
  primitive: exec
  args:
    cmd: [/opt/iclic-host-agent/runtime-discovery.sh]
    timeout_sec: 5
    parse: json
  output_key: runtime_instances
```

The script must print an array:

```json
[
  {
    "productCode": "ICOSYS",
    "componentCode": "hrm-backend",
    "instanceKey": "prod-api-01:hrm-backend",
    "runningVersion": "1.21.1",
    "gitCommit": "abc1234",
    "environment": "PROD",
    "payload": { "source": "systemd", "unit": "icosys-hrm.service" }
  }
]
```

`productCode` + `componentCode` identify the ICLIC runtime component catalog row.
`instanceKey` should be stable across restarts. If omitted, ICLIC falls back to
the authenticated server id plus component code. `versionSource` and `status` are
optional; the agent defaults them to `HOST_AGENT` and `HEALTHY`. See
[`protocol.md`](protocol.md) for the endpoint contract.

## Security notes

- `/etc/iclic-host-agent/collectors.d/` is `0750 root:iclic-agent`. Only root
  can write — the agent only reads.
- The agent runs as the `iclic-agent` system user. The `exec` primitive inherits
  that user's privileges. Operators who need a probe to read a privileged file
  (e.g. `/var/log/audit/audit.log`) typically grant the group via ACL rather
  than running the agent as root.
- Probes are run with a per-binding timeout (default 5 s) and a per-tick total
  budget (30 s). A pathological probe never blocks the heartbeat.
