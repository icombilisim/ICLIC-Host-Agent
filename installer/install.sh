#!/usr/bin/env bash
# ICLIC Host Agent installer / upgrader.
#
# Usage:
#   First install (enrolment):
#     TOKEN=<one-shot> ICLIC_URL=https://iclic.icombilisim.com \
#       PROFILES=host,docker,systemd,icosys \
#       bash install.sh
#
#   Upgrade (re-run on a host that already has /etc/iclic-host-agent/config.json):
#     bash install.sh                       # latest release, current profiles
#     AGENT_VERSION=v0.3.0 bash install.sh  # pin a specific tag
#     PROFILES=host,docker,systemd,iclic bash install.sh  # change profiles
#
# Required on first install:
#   TOKEN      one-shot enrolment token issued by ICLIC for this Server
#   ICLIC_URL  base URL of the ICLIC backend
#
# Optional:
#   AGENT_VERSION  release tag to install (default: latest)
#   PROFILES       comma-separated collector profiles (default: host,docker,systemd)
#                  available: host, docker, systemd, icosys, mysql, redis,
#                             nginx, iclic, devops, aigw-test, aigw-prod
#   INSTALL_DIR    default /opt/iclic-host-agent
#   CONFIG_DIR     default /etc/iclic-host-agent
#   STATE_DIR      default /var/lib/iclic-host-agent
#
# Re-running this script on an enrolled host is safe — it skips enrolment,
# swaps the binary in-place via a versioned symlink (rollback = retarget
# the symlink), and restarts the systemd unit.
set -euo pipefail

GITHUB_REPO="icombilisim/ICLIC-Host-Agent"

INSTALL_DIR="${INSTALL_DIR:-/opt/iclic-host-agent}"
CONFIG_DIR="${CONFIG_DIR:-/etc/iclic-host-agent}"
STATE_DIR="${STATE_DIR:-/var/lib/iclic-host-agent}"
SERVICE_USER="iclic-agent"
AGENT_VERSION="${AGENT_VERSION:-latest}"
PROFILES="${PROFILES:-host,docker,systemd}"

# `current` symlink path — defined early because the release-signature gate
# verifies the new release with the already-installed (trusted) binary. (#35)
CURRENT_LINK="${INSTALL_DIR}/iclic-host-agent"

# STRICT_VERIFY=1 turns "cannot verify the signature" into a hard failure. The
# Phase 3 root auto-updater always runs strict; manual installs default to
# trust-on-first-install over HTTPS so a host without a usable verifier (or a
# release published before signing was enabled) is not bricked. (#35)
STRICT_VERIFY="${STRICT_VERIFY:-0}"

# Embedded Ed25519 release public key (PEM), matching the AGENT_RELEASE_SIGNING_KEY
# secret. Generated 2026-06-22 via scripts/gen-release-signing-key.sh; verified
# end-to-end against the v0.16.0 release signature. Rotate by re-running the
# script. (#35)
RELEASE_PUBKEY_PEM='-----BEGIN PUBLIC KEY-----
MCowBQYDK2VwAyEAjGLvOjNYFDA8tHhsFFBaLXT9UKxyChImRaqY4sDOsQY=
-----END PUBLIC KEY-----'

# verify_signature checks SHA256SUMS.sig against SHA256SUMS. It prefers the
# already-trusted current binary (dependency-free, what the auto-updater uses)
# and falls back to OpenSSL with the embedded key. A genuine signature mismatch
# always aborts; "cannot verify here" only aborts under STRICT_VERIFY=1. (#35)
verify_signature() {
  local sums="$1" sig="$2"

  if [[ "${RELEASE_PUBKEY_PEM}" == *REPLACE_WITH_ED25519_PUBLIC_KEY_PEM* ]]; then
    echo "   WARN: release signing key not configured in installer — skipping" >&2
    echo "         signature check (trust-on-first-install over HTTPS)." >&2
    if [[ "${STRICT_VERIFY}" == "1" ]]; then
      echo "ERROR: STRICT_VERIFY=1 but no signing key is configured." >&2
      exit 1
    fi
    return 0
  fi

  # Preferred verifier: the trusted binary already on disk, if it understands
  # the subcommand (older agents predate it). Probe output without tripping
  # `set -o pipefail`.
  local probe=""
  if [[ -x "${CURRENT_LINK}" ]]; then
    probe="$("${CURRENT_LINK}" verify-release 2>&1 || true)"
  fi
  if [[ "${probe}" == *"--sums"* ]]; then
    if "${CURRENT_LINK}" verify-release --sums "${sums}" --sig "${sig}"; then
      echo "   signature OK (verified by the current agent binary)"
      return 0
    fi
    echo "ERROR: release signature verification FAILED. Refusing to install." >&2
    exit 1
  fi

  # Fallback verifier: OpenSSL + the embedded public key.
  if command -v openssl >/dev/null 2>&1; then
    printf '%s\n' "${RELEASE_PUBKEY_PEM}" > "${WORK_DIR}/release_pub.pem"
    local out
    if out="$(openssl pkeyutl -verify -pubin -inkey "${WORK_DIR}/release_pub.pem" \
              -rawin -in "${sums}" -sigfile "${sig}" 2>&1)"; then
      echo "   signature OK (verified by openssl)"
      return 0
    fi
    if echo "${out}" | grep -qiE "verification fail"; then
      echo "ERROR: release signature verification FAILED. Refusing to install." >&2
      exit 1
    fi
    echo "   WARN: openssl could not run Ed25519 verification (${out%%$'\n'*})." >&2
  else
    echo "   WARN: openssl not found — cannot verify the signature on this host." >&2
  fi

  if [[ "${STRICT_VERIFY}" == "1" ]]; then
    echo "ERROR: STRICT_VERIFY=1 and no usable verifier — refusing to install." >&2
    exit 1
  fi
  echo "   WARN: proceeding without signature verification (TOFU over HTTPS)." >&2
  return 0
}

if [[ "${EUID}" -ne 0 ]]; then
  echo "ERROR: installer must run as root (or via sudo)." >&2
  exit 1
fi

# ─── Mode detection ────────────────────────────────────────────────
# Enrolment is one-shot per ICLIC's design — re-running with TOKEN on
# an already-enrolled host would fail at the API anyway. Detect the
# state and skip enrolment if config.json is present. (#112)
ALREADY_ENROLLED=0
if [[ -f "${CONFIG_DIR}/config.json" ]]; then
  ALREADY_ENROLLED=1
  echo ">> Existing enrolment detected — running in upgrade mode."
else
  : "${TOKEN:?TOKEN env var is required (one-shot enrolment token from ICLIC)}"
  : "${ICLIC_URL:?ICLIC_URL env var is required}"
  echo ">> No existing enrolment — running in fresh-install mode."
fi

# ─── Architecture ──────────────────────────────────────────────────
ARCH="$(uname -m)"
case "${ARCH}" in
  x86_64|amd64) ARCH=amd64 ;;
  aarch64|arm64) ARCH=arm64 ;;
  *) echo "ERROR: unsupported arch ${ARCH} (linux-amd64 and linux-arm64 only)" >&2; exit 1 ;;
esac

# ─── Resolve target version ────────────────────────────────────────
# `latest` → use GitHub's latest-release redirect for the binary, but
# still need the concrete tag string for the versioned-binary path.
if [[ "${AGENT_VERSION}" == "latest" ]]; then
  RESOLVED_VERSION="$(curl -fsSL "https://api.github.com/repos/${GITHUB_REPO}/releases/latest" \
    | sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' \
    | head -n1)"
  if [[ -z "${RESOLVED_VERSION}" ]]; then
    echo "ERROR: could not resolve latest release tag from GitHub." >&2
    exit 1
  fi
else
  RESOLVED_VERSION="${AGENT_VERSION}"
fi
echo ">> Target version: ${RESOLVED_VERSION}"

# ─── Stage download in /tmp ────────────────────────────────────────
WORK_DIR="$(mktemp -d)"
trap 'rm -rf "${WORK_DIR}"' EXIT

RELEASE_BASE="https://github.com/${GITHUB_REPO}/releases/download/${RESOLVED_VERSION}"

echo ">> Downloading release assets"
curl -fsSL "${RELEASE_BASE}/iclic-host-agent-linux-${ARCH}" -o "${WORK_DIR}/iclic-host-agent"
curl -fsSL "${RELEASE_BASE}/SHA256SUMS"                     -o "${WORK_DIR}/SHA256SUMS"
curl -fsSL "${RELEASE_BASE}/configs.tar.gz"                 -o "${WORK_DIR}/configs.tar.gz"
curl -fsSL "${RELEASE_BASE}/iclic-host-agent.service"       -o "${WORK_DIR}/iclic-host-agent.service"
# Phase 3 self-updater assets. Absent on releases published before Phase 3, so
# tolerate a 404 and just skip installing the updater in that case. (#43)
UPDATER_PRESENT=1
curl -fsSL "${RELEASE_BASE}/iclic-host-agent-updater.sh"      -o "${WORK_DIR}/iclic-host-agent-updater.sh"      || UPDATER_PRESENT=0
curl -fsSL "${RELEASE_BASE}/iclic-host-agent-updater.service" -o "${WORK_DIR}/iclic-host-agent-updater.service" || UPDATER_PRESENT=0
curl -fsSL "${RELEASE_BASE}/iclic-host-agent-updater.timer"   -o "${WORK_DIR}/iclic-host-agent-updater.timer"   || UPDATER_PRESENT=0
# The signature may be absent on releases published before signing was enabled;
# tolerate a 404 here and let verify_signature decide (TOFU vs strict). (#35)
SIG_PRESENT=1
curl -fsSL "${RELEASE_BASE}/SHA256SUMS.sig" -o "${WORK_DIR}/SHA256SUMS.sig" || SIG_PRESENT=0

# ─── Verify release signature (authenticity) ───────────────────────
# SHA256SUMS proves the bytes are intact; the Ed25519 signature over it proves
# they came from us. Check authenticity FIRST, before trusting any checksum the
# same download produced. (#35)
echo ">> Verifying release signature"
if [[ "${SIG_PRESENT}" -eq 1 ]]; then
  verify_signature "${WORK_DIR}/SHA256SUMS" "${WORK_DIR}/SHA256SUMS.sig"
else
  echo "   WARN: no SHA256SUMS.sig in this release — cannot verify authenticity." >&2
  if [[ "${STRICT_VERIFY}" == "1" ]]; then
    echo "ERROR: STRICT_VERIFY=1 but the release is unsigned. Refusing to install." >&2
    exit 1
  fi
fi

# ─── Verify SHA256 (integrity) ─────────────────────────────────────
# A truncated download from a flaky network would silently produce a
# corrupt binary; tampering would do the same loudly. Either way,
# verify before we trust the bytes. (#112)
echo ">> Verifying SHA256"
cd "${WORK_DIR}"
verify_one() {
  local file="$1"
  local expected
  expected="$(grep " ${file}\$" SHA256SUMS | awk '{print $1}')"
  if [[ -z "${expected}" ]]; then
    echo "ERROR: ${file} missing from SHA256SUMS" >&2
    exit 1
  fi
  local actual
  actual="$(sha256sum "${file}" | awk '{print $1}')"
  if [[ "${expected}" != "${actual}" ]]; then
    echo "ERROR: SHA256 mismatch on ${file}" >&2
    echo "  expected ${expected}" >&2
    echo "  actual   ${actual}" >&2
    exit 1
  fi
}
# Verify everything we are about to install. The binary file in the
# checksum manifest is the linux-amd64 / linux-arm64 name, but we
# store it locally under iclic-host-agent — copy + verify under the
# manifest name so grep matches.
cp iclic-host-agent "iclic-host-agent-linux-${ARCH}"
verify_one "iclic-host-agent-linux-${ARCH}"
verify_one configs.tar.gz
verify_one iclic-host-agent.service
if [[ "${UPDATER_PRESENT}" -eq 1 ]]; then
  verify_one iclic-host-agent-updater.sh
  verify_one iclic-host-agent-updater.service
  verify_one iclic-host-agent-updater.timer
fi
cd - >/dev/null

# ─── Users + directories ───────────────────────────────────────────
echo ">> Ensuring system user ${SERVICE_USER}"
if ! id -u "${SERVICE_USER}" >/dev/null 2>&1; then
  useradd --system --no-create-home --shell /usr/sbin/nologin "${SERVICE_USER}"
fi

# docker.containers/docker.stats primitives, and the docker-exec-based
# probes in the icosys/mysql/redis/nginx profiles, all need the agent
# to read /var/run/docker.sock — which is owned by root:docker on
# every Docker install. Add the agent to the docker group when the
# group exists (it won't on hosts without Docker, which is fine). (#112)
if getent group docker >/dev/null 2>&1; then
  usermod -aG docker "${SERVICE_USER}"
fi

echo ">> Ensuring directories"
install -d -o root -g root              -m 0755 "${INSTALL_DIR}"
install -d -o root -g root              -m 0755 "${INSTALL_DIR}/bin"
install -d -o root -g "${SERVICE_USER}" -m 0750 "${CONFIG_DIR}"
install -d -o root -g "${SERVICE_USER}" -m 0750 "${CONFIG_DIR}/collectors.d"
install -d -o "${SERVICE_USER}" -g "${SERVICE_USER}" -m 0700 "${STATE_DIR}"

# ─── Versioned binary + symlink ────────────────────────────────────
# Versioned filename keeps the previous build on disk; the `current`
# symlink is the only thing systemd consumes, so rollback is one
# `ln -sfn` away. (#112)
VERSIONED_BIN="${INSTALL_DIR}/bin/iclic-host-agent-${RESOLVED_VERSION}"
# CURRENT_LINK is defined near the top (the signature gate needs it).

echo ">> Installing binary as ${VERSIONED_BIN}"
install -o root -g root -m 0755 "${WORK_DIR}/iclic-host-agent" "${VERSIONED_BIN}"
ln -sfn "${VERSIONED_BIN}" "${CURRENT_LINK}"

# ─── Enrolment (fresh install only) ────────────────────────────────
if [[ "${ALREADY_ENROLLED}" -eq 0 ]]; then
  echo ">> Exchanging enrolment token for permanent HMAC credentials"
  # /api/v1/agent/enroll is public — the one-shot token in the body
  # is the credential. The path lives outside /api/v1/server/** so it
  # falls through the HMAC-protected chain. (#2)
  ENROLL_RESPONSE="$(curl -fsSL -X POST \
    -H "Content-Type: application/json" \
    -d "{\"token\":\"${TOKEN}\"}" \
    "${ICLIC_URL}/api/v1/agent/enroll")"

  # ICLIC's default Jackson serializer emits camelCase keys — extract
  # them directly so we don't depend on a JSON parser at install time. (#2)
  SERVER_ID="$(echo "${ENROLL_RESPONSE}"   | sed -n 's/.*"serverId"[[:space:]]*:[[:space:]]*\([0-9]\+\).*/\1/p')"
  AGENT_KID="$(echo "${ENROLL_RESPONSE}"   | sed -n 's/.*"agentKid"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')"
  AGENT_SECRET="$(echo "${ENROLL_RESPONSE}" | sed -n 's/.*"agentSecret"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')"

  if [[ -z "${SERVER_ID}" || -z "${AGENT_KID}" || -z "${AGENT_SECRET}" ]]; then
    echo "ERROR: enrolment response missing fields:" >&2
    echo "${ENROLL_RESPONSE}" >&2
    exit 1
  fi

  echo ">> Writing ${CONFIG_DIR}/config.json"
  umask 077
  cat > "${CONFIG_DIR}/config.json" <<EOF
{
  "iclic_url": "${ICLIC_URL}",
  "server_id": "${SERVER_ID}",
  "agent_kid": "${AGENT_KID}",
  "agent_secret": "${AGENT_SECRET}",
  "heartbeat_interval_seconds": 60
}
EOF
  chown root:"${SERVICE_USER}" "${CONFIG_DIR}/config.json"
  chmod 0640 "${CONFIG_DIR}/config.json"
else
  echo ">> Keeping existing ${CONFIG_DIR}/config.json"
fi

# ─── Collector profiles ────────────────────────────────────────────
# Profile name → asset filename. Re-run with a different PROFILES set
# to add or change collectors; we don't delete files the operator
# might have written by hand, so unrecognised files in collectors.d
# survive an upgrade.
echo ">> Extracting configs bundle"
mkdir -p "${WORK_DIR}/configs"
tar -xzf "${WORK_DIR}/configs.tar.gz" -C "${WORK_DIR}/configs"

declare -A PROFILE_TO_FILE=(
  [host]=00-linux-host.yaml
  [docker]=10-docker.yaml
  [systemd]=20-systemd.yaml
  [icosys]=30-icosys-actuator.yaml
  [mysql]=40-mysql.yaml
  [redis]=50-redis.yaml
  [nginx]=60-nginx.yaml
  [iclic]=70-iclic.yaml
  [devops]=80-devops-stack.yaml
  # AI Gateway is monitored per-host: test and prod need distinct instance_key
  # values (ICLIC dedup ignores the reporting server), so each host gets its
  # own file with the right container name + key. (#46)
  [aigw-test]=90-aigw-test.yaml
  [aigw-prod]=90-aigw-prod.yaml
)

echo ">> Activating profiles: ${PROFILES}"
IFS=',' read -ra REQUESTED <<< "${PROFILES}"
for profile in "${REQUESTED[@]}"; do
  profile="$(echo "${profile}" | xargs)"  # trim whitespace
  if [[ -z "${profile}" ]]; then
    continue
  fi
  target="${PROFILE_TO_FILE[${profile}]:-}"
  if [[ -z "${target}" ]]; then
    echo "   WARN: unknown profile '${profile}', skipping" >&2
    continue
  fi
  src="${WORK_DIR}/configs/${target}"
  if [[ ! -f "${src}" ]]; then
    echo "   WARN: profile '${profile}' has no asset (${target}) in this release, skipping" >&2
    continue
  fi
  install -o root -g "${SERVICE_USER}" -m 0640 "${src}" "${CONFIG_DIR}/collectors.d/${target}"
  echo "   enabled: ${profile} → ${target}"
done

# ─── systemd units + self-updater ──────────────────────────────────
echo ">> Installing systemd unit"
install -o root -g root -m 0644 \
  "${WORK_DIR}/iclic-host-agent.service" \
  /etc/systemd/system/iclic-host-agent.service

if [[ "${UPDATER_PRESENT}" -eq 1 ]]; then
  echo ">> Installing self-updater (nightly timer)"
  install -o root -g root -m 0755 \
    "${WORK_DIR}/iclic-host-agent-updater.sh" "${INSTALL_DIR}/iclic-host-agent-updater"
  install -o root -g root -m 0644 \
    "${WORK_DIR}/iclic-host-agent-updater.service" /etc/systemd/system/iclic-host-agent-updater.service
  install -o root -g root -m 0644 \
    "${WORK_DIR}/iclic-host-agent-updater.timer" /etc/systemd/system/iclic-host-agent-updater.timer
  # The updater re-runs the installer, so keep a stable copy it can call. Skip
  # when piped (curl|bash) where $0 is not a readable file. (#43)
  if [[ -f "$0" && "$0" != "${INSTALL_DIR}/install.sh" ]]; then
    install -o root -g root -m 0755 "$0" "${INSTALL_DIR}/install.sh"
  fi
fi

systemctl daemon-reload

if [[ "${ALREADY_ENROLLED}" -eq 1 ]]; then
  echo ">> Restarting iclic-host-agent (upgrade)"
  systemctl restart iclic-host-agent
else
  echo ">> Enabling and starting iclic-host-agent (fresh install)"
  systemctl enable --now iclic-host-agent
fi

if [[ "${UPDATER_PRESENT}" -eq 1 ]]; then
  echo ">> Enabling nightly self-update timer"
  systemctl enable --now iclic-host-agent-updater.timer
fi

echo ""
echo ">> Done. Installed ${RESOLVED_VERSION}, profiles: ${PROFILES}"
echo "   systemctl status iclic-host-agent"
echo "   journalctl -u iclic-host-agent -f"
echo "   Rollback: ln -sfn ${INSTALL_DIR}/bin/iclic-host-agent-<previous-tag> ${CURRENT_LINK} && systemctl restart iclic-host-agent"
