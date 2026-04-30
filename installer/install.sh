#!/usr/bin/env bash
# ICLIC Host Agent installer.
#
# Required environment:
#   TOKEN      one-shot enrollment token issued by ICLIC for this Server
#   ICLIC_URL  base URL of the ICLIC backend (e.g. https://iclic.icombilisim.com)
#
# Optional environment:
#   AGENT_VERSION  pin to a specific release tag (default: latest)
#   INSTALL_DIR    where the binary lives (default: /opt/iclic-host-agent)
#   CONFIG_DIR     where the config lives  (default: /etc/iclic-host-agent)
#   STATE_DIR      where state lives       (default: /var/lib/iclic-host-agent)
set -euo pipefail

: "${TOKEN:?TOKEN env var is required (one-shot enrollment token from ICLIC)}"
: "${ICLIC_URL:?ICLIC_URL env var is required}"

AGENT_VERSION="${AGENT_VERSION:-latest}"
INSTALL_DIR="${INSTALL_DIR:-/opt/iclic-host-agent}"
CONFIG_DIR="${CONFIG_DIR:-/etc/iclic-host-agent}"
STATE_DIR="${STATE_DIR:-/var/lib/iclic-host-agent}"
SERVICE_USER="iclic-agent"

if [[ "${EUID}" -ne 0 ]]; then
  echo "ERROR: installer must run as root (or via sudo)." >&2
  exit 1
fi

ARCH="$(uname -m)"
case "${ARCH}" in
  x86_64|amd64) ARCH=amd64 ;;
  aarch64|arm64) ARCH=arm64 ;;
  *) echo "ERROR: unsupported arch ${ARCH} (linux-amd64 and linux-arm64 only)" >&2; exit 1 ;;
esac

echo ">> Creating system user ${SERVICE_USER} (if missing)"
if ! id -u "${SERVICE_USER}" >/dev/null 2>&1; then
  useradd --system --no-create-home --shell /usr/sbin/nologin "${SERVICE_USER}"
fi

echo ">> Creating directories"
install -d -o root -g root -m 0755 "${INSTALL_DIR}"
install -d -o root -g "${SERVICE_USER}" -m 0750 "${CONFIG_DIR}"
install -d -o "${SERVICE_USER}" -g "${SERVICE_USER}" -m 0700 "${STATE_DIR}"

echo ">> Downloading binary (${AGENT_VERSION}, linux-${ARCH})"
DOWNLOAD_URL="https://github.com/icombilisim/ICLIC-Host-Agent/releases/${AGENT_VERSION}/download/iclic-host-agent-linux-${ARCH}"
if [[ "${AGENT_VERSION}" == "latest" ]]; then
  DOWNLOAD_URL="https://github.com/icombilisim/ICLIC-Host-Agent/releases/latest/download/iclic-host-agent-linux-${ARCH}"
fi
curl -fsSL "${DOWNLOAD_URL}" -o "${INSTALL_DIR}/iclic-host-agent.new"
chmod 0755 "${INSTALL_DIR}/iclic-host-agent.new"
mv "${INSTALL_DIR}/iclic-host-agent.new" "${INSTALL_DIR}/iclic-host-agent"

echo ">> Exchanging enrollment token for permanent HMAC credentials"
ENROLL_RESPONSE="$(curl -fsSL -X POST \
  -H "Content-Type: application/json" \
  -d "{\"token\":\"${TOKEN}\"}" \
  "${ICLIC_URL}/api/v1/server/enroll")"

SERVER_ID="$(echo "${ENROLL_RESPONSE}"   | sed -n 's/.*"server_id"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')"
AGENT_KID="$(echo "${ENROLL_RESPONSE}"   | sed -n 's/.*"agent_kid"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')"
AGENT_SECRET="$(echo "${ENROLL_RESPONSE}" | sed -n 's/.*"agent_secret"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')"

if [[ -z "${SERVER_ID}" || -z "${AGENT_KID}" || -z "${AGENT_SECRET}" ]]; then
  echo "ERROR: enrollment response missing fields:" >&2
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

echo ">> Installing systemd unit"
install -o root -g root -m 0644 \
  "$(dirname "$0")/iclic-host-agent.service" \
  /etc/systemd/system/iclic-host-agent.service 2>/dev/null || \
  curl -fsSL "https://raw.githubusercontent.com/icombilisim/ICLIC-Host-Agent/main/installer/iclic-host-agent.service" \
       -o /etc/systemd/system/iclic-host-agent.service

systemctl daemon-reload
systemctl enable --now iclic-host-agent

echo ">> Done. Verify with:"
echo "   systemctl status iclic-host-agent"
echo "   journalctl -u iclic-host-agent -f"
