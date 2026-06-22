# AGENTS.md — guidance for AI coding tools

> Repo: **ICLIC Host Agent** (Go monitoring agent → ICLIC heartbeat).
> Stamp: v0.15.0 · 2026-06-22. Keep this current when the rules change.

This file is the **canonical** guidance for AI coding tools (Codex, Claude Code,
Cursor, …). [`CLAUDE.md`](CLAUDE.md) is a thin pointer to it — edit the rules
here, not there, so the two never drift.

## Read the docs first

English is the source of truth; `docs/tr/` mirrors it. Before changing code, read
`docs/` in this order (index: [`docs/README.md`](docs/README.md)):

1. [`docs/en/overview.md`](docs/en/overview.md) — what it is, the two channels, **core invariants**.
2. [`docs/en/architecture.md`](docs/en/architecture.md) — module map, runtime model, control channel.
3. [`docs/en/protocol.md`](docs/en/protocol.md) — heartbeat wire contract, auth, versioning.
4. [`docs/en/collectors.md`](docs/en/collectors.md) — YAML-driven primitive reference.
5. [`docs/en/deployment.md`](docs/en/deployment.md) — release (release-please), install, upgrade, rollback.

## Non-negotiable invariants (read before editing)

- **Outbound only.** The agent dials ICLIC; it never opens an inbound port.
- **No arbitrary command execution.** The control channel serves only an opt-in,
  closed, typed verb set — never a shell. New verbs are explicit and audited.
- **Opt-in, default OFF.** No `control.yaml` ⇒ the agent connects but refuses
  every control request. Destructive verbs additionally require ICLIC-side 2FA.
- **release-please owns the version.** Never hand-bump `AgentVersion`
  (`internal/heartbeat/heartbeat.go`) or push `v*` tags manually.
- **Releases are Ed25519-signed and verified before install.** CI fails closed
  without `AGENT_RELEASE_SIGNING_KEY`; `install.sh` aborts on a signature
  mismatch. Never publish unsigned or weaken `internal/release.Verify` /
  `verify_signature` — auto-update trusts this gate. See `deployment.md` §14.
- **Versioned binaries.** Rollback = retarget the `current` symlink; the previous
  binary stays on disk. Don't break that on-disk layout.
- **Bump `ProtocolVersion`** only on a breaking wire change (see `protocol.md`);
  additive fields need no bump.

## Conventions

- **Go 1.22.** English doc comments on exported symbols. Inline comments cite the
  issue: `// <reason>. (#N)`.
- **A new collector primitive** = one line in `internal/collectors/registry.go`
  + its implementation in `primitives_*.go` + a doc entry in **BOTH**
  `docs/en/collectors.md` AND `docs/tr/toplayicilar.md`. Primitive names, `args`
  keys, and `output_key` values stay **English** in both languages — only prose
  is translated.
- The primitive surface is **finite and auditable** — operator extension is the
  `exec` + binding-YAML combination, not out-of-tree plugins.
- Run `go vet ./... && go test ./...` before committing.
- **Conventional Commits**; every commit references a GitHub issue (`(#N)`).
- **Docs:** change `docs/en/` first, then mirror into `docs/tr/`. Update the
  `Version` / `Sürüm` stamp on every doc you touch.

## Design rationale (source of truth)

Lives in the parent monorepo, not here: `.claude/docs/` and the ICLIC integration
surface doc (`ICLIC-1.0/docs/iclic-icosys-integration-surface.md`).
