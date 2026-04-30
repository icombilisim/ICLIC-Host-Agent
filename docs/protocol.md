# ICLIC Host Agent → ICLIC Heartbeat Protocol

**Status:** v1 — initial scaffold. Field set will grow before first prod
deploy; bump `protocol_version` for any breaking change.

## Transport

- HTTPS only.
- One agent → one ICLIC URL (configured at install time).
- Agent only sends; ICLIC never connects to the agent. No inbound port on the
  monitored host.

## Endpoints

### Enrollment (one-shot)

```
POST {ICLIC_URL}/api/v1/agent/enroll
Content-Type: application/json
```

```json
{
  "token": "<one-shot-bootstrap-token>",
  "label": "agent on api-01"
}
```

The token is issued by an ICLIC admin (TTL-capped, single-use). The path is
public — the token IS the credential. The successful response returns a
`server_id`, `agent_kid`, and `agent_secret` exactly once; the installer
persists them locally and the secret is never re-issuable.

### Heartbeat

```
POST {ICLIC_URL}/api/v1/server/{serverId}/heartbeat
Content-Type:  application/json
Authorization: Bearer <kid>.<secret>
User-Agent:    iclic-host-agent/<version>
```

`serverId` in the path matches the `server_id` field in the body — the path
exists for ICLIC routing/logging, the body field is what the agent self-reports.

## Authentication

PAT-style bearer:

```
Authorization: Bearer <kid>.<secret>
```

ICLIC splits at the first `.` and verifies the secret half against the
SHA-256 digest stored against the kid. TLS provides confidentiality on the
wire — there is no per-request signature.

The same bearer scheme is used for every authenticated `/api/v1/server/**`
endpoint, so the agent does not need separate credentials per call.

### Why not request-signing?

Earlier drafts of this protocol used HMAC-SHA256 over a canonical request
string. Plain bearer-over-TLS was chosen instead because:

- ICLIC's existing PAT scheme (used for installation→authority calls) already
  uses the bearer form; sharing the credential type keeps the verifier code
  paths uniform.
- Replay defense beyond the TLS session window has no concrete adversary
  the agent is trying to defeat — heartbeats are idempotent state-overwrites,
  not commands.
- A leaked bearer is recoverable via the existing key revocation flow.

If a future endpoint genuinely needs replay-proof semantics (e.g. command
delivery from ICLIC back to the agent), it can layer a nonce on top — but
the host-monitoring agent has no such surface.

## Payload v1

The wire envelope is **camelCase** at the top level (matching ICLIC's default
Jackson naming) and a free-form `metrics` map below. The agent grows new
metric keys over time without an ICLIC-side schema change.

```json
{
  "agentVersion": "0.1.0",
  "protocolVersion": 1,
  "metrics": {
    "reported_at": "2026-04-30T12:34:56Z",
    "status": "UP",
    "uptime_sec": 1234567,
    "os_name": "ubuntu",
    "os_version": "24.04",
    "kernel": "6.8.0-31-generic",
    "cpu_load_1m": 0.45,
    "cpu_load_5m": 0.31,
    "mem_used_pct": 48.2,
    "mem_total_mb": 16384,
    "disks": [
      { "mount": "/",                "used_pct": 22.0, "total_gb": 100 },
      { "mount": "/var/lib/docker",  "used_pct": 56.0, "total_gb": 500 }
    ],
    "docker_version": "26.1.4",
    "os_security_updates_pending": 0
  }
}
```

### Why two cases on the wire?

- `agentVersion` and `protocolVersion` are **typed envelope fields** that
  ICLIC special-cases (badge rendering, version-skew detection). They follow
  Jackson's default camelCase, same as the rest of the ICLIC API surface.
- Everything inside `metrics` is **opaque to ICLIC** — stored verbatim as JSON
  in `server.host_metrics_json` and copied to `server_heartbeat_history`. The
  agent uses snake_case there because the source data (`/proc/*`,
  `os-release`) is itself snake_case; renaming would invent friction without
  a consumer.

### Field notes

| Field | Notes |
|-------|-------|
| `protocolVersion` | Integer. Bumped on breaking change. ICLIC accepts the last N versions. |
| `agentVersion` | Free-form. Used by ICLIC to emit "agent outdated" badges; not load-bearing. |
| `metrics.status` | `UP` \| `DEGRADED` — agent's self-assessment. ICLIC may override to `STALE` on missed heartbeats. |
| `metrics.disks[]` | One entry per mount the agent is configured to watch. Empty array is legal. |
| `metrics.docker_version` | Optional — omitted on hosts without Docker. |
| `metrics.os_security_updates_pending` | -1 means "agent could not determine" (e.g. apt locked, dnf timeout). |
| `metrics.reported_at` | Agent-side wall clock at sample time. ICLIC also stamps its own `received_at` server-side; the two are kept separate so clock skew is observable. |

## Versioning rules

- **Additive change** (new optional field) → no version bump. Old agents
  continue to work; ICLIC tolerates missing fields.
- **Breaking change** (rename, remove, type change) → bump
  `protocol_version`. ICLIC keeps the previous version's parser for at least
  one major release.
- The agent always sends the latest version it knows. ICLIC never asks the
  agent to downgrade — if the agent is too new, ICLIC returns `400` and
  responsibility falls on the operator to upgrade ICLIC.

## Errors

| Status | Meaning | Agent reaction |
|--------|---------|----------------|
| 200 / 204 | Accepted | Continue |
| 400 | Malformed payload, unsupported `protocol_version`, or path serverId mismatches the bearer | Log + retry on next tick (no backoff) |
| 401 | Bearer is missing, expired, revoked, or unknown | Log + retry; if persistent, sysadmin must re-enroll |
| 403 | Server `enrollment_status` is DISABLED | Stop and exit non-zero so systemd surfaces the failure |
| 5xx | ICLIC down | Log + retry on next tick |

The agent does not implement exponential backoff; the systemd `Restart=on-failure`
policy and the fixed 60-second tick are deliberately the only retry mechanism.
