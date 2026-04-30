# ICLIC Host Agent → ICLIC Heartbeat Protocol

**Status:** v1 — initial scaffold. Field set will grow before first prod
deploy; bump `protocol_version` for any breaking change.

## Transport

- HTTPS only.
- One agent → one ICLIC URL (configured at install time).
- Agent only sends; ICLIC never connects to the agent. No inbound port on the
  monitored host.

## Endpoint

```
POST {ICLIC_URL}/api/v1/server/{serverId}/heartbeat
Content-Type: application/json
Authorization: HMAC kid=<kid>, ts=<unix_seconds>, sig=<hex>
User-Agent:    iclic-host-agent/<version>
```

`serverId` in the path matches the `server_id` field in the body — the path
exists for ICLIC routing/logging, the body field is what the signature covers.

## HMAC scheme

```
canonical = METHOD + "\n" +
            REQUEST-TARGET + "\n" +
            TS + "\n" +
            HEX(SHA256(BODY))

sig = HEX(HMAC-SHA256(secret, canonical))
```

- `REQUEST-TARGET` is path + raw query (e.g. `/api/v1/server/abc/heartbeat`).
- `TS` is integer seconds since epoch. ICLIC rejects requests with `|TS - now| > 300`.
- Body is the exact JSON bytes sent — ICLIC reads the same bytes off the wire,
  hashes them, recomputes the signature.
- Replay defense beyond the 5-minute window is not the agent's job — ICLIC
  may track `(kid, ts, body-hash)` if it ever needs strict idempotency.

## Payload v1

```json
{
  "protocol_version": 1,
  "agent_version": "0.1.0",
  "server_id": "f5b9...uuid",
  "reported_at": "2026-04-30T12:34:56Z",
  "status": "UP",
  "host": {
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

### Field notes

| Field | Notes |
|-------|-------|
| `protocol_version` | Integer. Bumped on breaking change. ICLIC accepts the last N versions. |
| `agent_version` | Free-form. Used by ICLIC to emit "agent outdated" badges; not load-bearing. |
| `status` | `UP` \| `DEGRADED` — agent's self-assessment. ICLIC may override to `STALE` on missed heartbeats. |
| `host.disks[]` | One entry per mount the agent is configured to watch. Empty array is legal. |
| `host.docker_version` | Optional — omitted on hosts without Docker. |
| `host.os_security_updates_pending` | -1 means "agent could not determine" (e.g. apt locked, dnf timeout). |

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
| 400 | Malformed payload or unsupported `protocol_version` | Log + retry on next tick (no backoff) |
| 401 | Bad signature, expired ts, unknown kid | Log + retry; if persistent, sysadmin must re-enroll |
| 403 | Server `enrollment_status` revoked | Stop and exit non-zero so systemd surfaces the failure |
| 5xx | ICLIC down | Log + retry on next tick |

The agent does not implement exponential backoff; the systemd `Restart=on-failure`
policy and the fixed 60-second tick are deliberately the only retry mechanism.
