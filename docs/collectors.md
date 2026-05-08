# Operator-defined collectors

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
skipped with a warning. A primitive that returns an error logs at WARN and
omits its metric for that tick — never crashes the agent.

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

### procfs.cpu_count

Returns the number of online logical CPUs (int).

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

Run an arbitrary command. The escape hatch — anything not covered by a
named primitive (WildFly CLI, jboss-cli, custom scripts) goes through this.

| Arg         | Type     | Default | Description |
|-------------|----------|---------|-------------|
| cmd         | []string | —       | argv-style; no shell expansion |
| timeout_sec | number   | `5`     | Capped at 30 |
| parse       | string   | `raw`   | `raw` / `trimmed` / `int` / `float` |

A non-zero exit is treated as an error (metric omitted that tick).

### systemctl.is_active

Returns `true` when `systemctl is-active <unit>` reports `active` (bool).

| Arg  | Type   | Default | Description |
|------|--------|---------|-------------|
| unit | string | —       | e.g. `nginx.service` |

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

### file.stat

| Arg   | Type   | Default  | Description |
|-------|--------|----------|-------------|
| path  | string | —        | |
| field | string | `exists` | `exists` (bool) / `size_bytes` (int) / `mtime_seconds` (int). On missing path, `size_bytes` and `mtime_seconds` return `-1`. |

### apt.security_count

Counts pending security updates by parsing `apt-get -s upgrade`. Returns int.
On RHEL/CentOS or when apt is locked / missing / times out, returns `-1` —
the documented sentinel for "agent could not determine". Never errors so
non-Debian hosts get the right "unknown" signal until a `dnf.security_count`
primitive exists.

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

Save the file → wait one tick (default 60 s) → ICLIC server detail page picks
up the new keys automatically. The Server Detail "Host Metrics" panel renders
the well-known keys from the linux-host profile by default; everything else
is visible via the raw payload viewer in the "Heartbeat History" panel.

## Runtime deployment status

ICLIC also accepts runtime version signals under the reserved
`runtime_instances` output key. The agent forwards each item to
`POST /api/v1/server/runtime-instances/heartbeat` after the normal host
heartbeat succeeds. This keeps Docker, systemd, WildFly, PHP, Python, .NET,
Go, and other legacy stacks on one collection path.

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
    "payload": {
      "source": "systemd",
      "unit": "icosys-hrm.service"
    }
  }
]
```

`productCode` + `componentCode` identify the ICLIC runtime component catalog
row. `instanceKey` should be stable across restarts. If omitted, ICLIC falls
back to the authenticated server id plus component code. `versionSource` and
`status` are optional; the agent defaults them to `HOST_AGENT` and `HEALTHY`.

## Security notes

- `/etc/iclic-host-agent/collectors.d/` is `0750 root:iclic-agent`. Only root
  can write — the agent only reads.
- The agent runs as the `iclic-agent` system user. The `exec` primitive
  inherits that user's privileges. Operators who need a probe to read a
  privileged file (e.g. `/var/log/audit/audit.log`) typically grant the
  group via ACL rather than running the agent as root.
- Probes are run with a per-binding timeout (default 5 s) and a per-tick
  total budget (30 s). A pathological probe never blocks the heartbeat.
