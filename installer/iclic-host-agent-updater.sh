#!/usr/bin/env bash
# iclic-host-agent-updater — applies the host-agent version ICLIC requested.
#
# Driven by iclic-host-agent-updater.timer (nightly). The unprivileged agent
# records the desired version from its heartbeat into a state file (Phase 2);
# this script (root) reads that file and, if it differs from the installed
# version, runs the SIGNED installer in strict mode, then health-gates: a
# heartbeat must be accepted within the window or it rolls back to the previous
# binary. The agent never updates itself — privilege stays here. (#43)
#
# Env overrides (mainly for tests / non-standard layouts):
#   INSTALL_DIR     default /opt/iclic-host-agent
#   STATE_DIR       default /var/lib/iclic-host-agent
#   HEALTH_TIMEOUT  seconds to wait for a healthy heartbeat (default 180)
set -uo pipefail

INSTALL_DIR="${INSTALL_DIR:-/opt/iclic-host-agent}"
STATE_DIR="${STATE_DIR:-/var/lib/iclic-host-agent}"
DESIRED_FILE="${ICLIC_AGENT_DESIRED_VERSION_FILE:-${STATE_DIR}/desired-version}"
CURRENT_LINK="${INSTALL_DIR}/iclic-host-agent"
INSTALL_SH="${INSTALL_DIR}/install.sh"
HEALTH_TIMEOUT="${HEALTH_TIMEOUT:-180}"
GITHUB_REPO="icombilisim/ICLIC-Host-Agent"
UNIT="iclic-host-agent"

log() { echo "iclic-host-agent-updater: $*"; }

if [[ "$(id -u)" -ne 0 ]]; then
  log "must run as root"
  exit 1
fi

# ── What does ICLIC want this host on? ─────────────────────────────
if [[ ! -f "${DESIRED_FILE}" ]]; then
  log "no desired-version file (${DESIRED_FILE}) — nothing to do"
  exit 0
fi
desired="$(tr -d '[:space:]' < "${DESIRED_FILE}")"
if [[ -z "${desired}" ]]; then
  log "desired-version empty — nothing to do"
  exit 0
fi

# 'latest' → resolve to a concrete tag so the compare + pin are deterministic
# and the whole fleet converges on the same version even across a release. (#43)
if [[ "${desired}" == "latest" ]]; then
  desired="$(curl -fsSL "https://api.github.com/repos/${GITHUB_REPO}/releases/latest" \
    | sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -n1)"
  if [[ -z "${desired}" ]]; then
    log "could not resolve 'latest' release tag — skipping this run"
    exit 1
  fi
fi

# Installed version = the symlink target's tag suffix (…/iclic-host-agent-vX.Y.Z).
prev_bin="$(readlink -f "${CURRENT_LINK}" 2>/dev/null || true)"
current="${prev_bin##*/iclic-host-agent-}"

if [[ "${desired}" == "${current}" ]]; then
  log "already on ${current} — nothing to do"
  exit 0
fi

if [[ ! -x "${INSTALL_SH}" ]]; then
  log "installer not found at ${INSTALL_SH} — cannot self-update"
  exit 1
fi

# ── Apply the signed upgrade ───────────────────────────────────────
log "updating ${current:-unknown} -> ${desired}"
upgrade_start="$(date -u +%s)"

# STRICT_VERIFY=1: refuse to install anything whose signature can't be verified.
# install.sh verifies BEFORE swapping, so a bad/missing signature aborts with the
# current binary still in place — no rollback needed in that case. (#43)
if ! AGENT_VERSION="${desired}" STRICT_VERIFY=1 bash "${INSTALL_SH}"; then
  log "installer failed for ${desired} — leaving ${current:-current} in place"
  exit 1
fi

# ── Health-gate: demand a fresh accepted heartbeat ─────────────────
# An accepted heartbeat (logged by the agent) after the upgrade proves the new
# binary actually talks to ICLIC — not just that the process is alive. (#43)
healthy=0
deadline=$(( $(date -u +%s) + HEALTH_TIMEOUT ))
while [[ "$(date -u +%s)" -lt "${deadline}" ]]; do
  if systemctl is-active --quiet "${UNIT}" \
     && journalctl -u "${UNIT}" --since "@${upgrade_start}" -o cat 2>/dev/null \
        | grep -q "heartbeat accepted"; then
    healthy=1
    break
  fi
  sleep 10
done

if [[ "${healthy}" -eq 1 ]]; then
  log "update to ${desired} healthy"
  exit 0
fi

# ── Rollback ───────────────────────────────────────────────────────
log "no healthy heartbeat within ${HEALTH_TIMEOUT}s — rolling back to ${prev_bin:-<none>}"
if [[ -n "${prev_bin}" && -x "${prev_bin}" ]]; then
  ln -sfn "${prev_bin}" "${CURRENT_LINK}"
  systemctl restart "${UNIT}"
  log "rolled back to ${prev_bin##*/iclic-host-agent-}"
else
  log "no previous binary to roll back to — manual intervention required"
fi
exit 1
