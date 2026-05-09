#!/usr/bin/env bash
# Roll out a host-agent upgrade across an inventory of already-enrolled
# servers. First-time enrolment still happens by hand on each box —
# the one-shot TOKEN comes from ICLIC's "Servers → New" flow and is
# different per host, so it doesn't fit a fan-out script.
#
# Usage:
#   bash deploy-all.sh <inventory-file> [agent-version]
#
# Inventory line format (one host per line, blank lines + `#` comments
# ignored):
#
#   host:profiles[:user[:port]]
#
# Example:
#
#   test.example.com:host,docker,systemd,icosys,mysql,redis,nginx
#   devops.example.com:host,docker,systemd,devops:icadmin
#   iclic.example.com:host,docker,systemd,iclic:icadmin:22
#
# Defaults: user=root, port=22.
#
# Exit code is the number of failed hosts (0 = all green). Failures
# don't short-circuit the loop — partial fleet upgrades are normal
# and operators want to see every result before rolling back. (#112)
set -uo pipefail

if [[ $# -lt 1 ]]; then
  echo "Usage: $0 <inventory-file> [agent-version]" >&2
  exit 64
fi

INVENTORY="$1"
AGENT_VERSION="${2:-latest}"

if [[ ! -f "${INVENTORY}" ]]; then
  echo "ERROR: inventory file not found: ${INVENTORY}" >&2
  exit 64
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
INSTALL_SCRIPT="${SCRIPT_DIR}/install.sh"
if [[ ! -f "${INSTALL_SCRIPT}" ]]; then
  echo "ERROR: install.sh not found alongside deploy-all.sh: ${INSTALL_SCRIPT}" >&2
  exit 64
fi

declare -a FAILED_HOSTS=()
declare -a SUCCEEDED_HOSTS=()

while IFS= read -r raw_line || [[ -n "${raw_line}" ]]; do
  line="$(echo "${raw_line}" | sed -E 's/^\s+|\s+$//g')"
  if [[ -z "${line}" || "${line}" =~ ^# ]]; then
    continue
  fi

  IFS=':' read -ra parts <<< "${line}"
  host="${parts[0]:-}"
  profiles="${parts[1]:-host,docker,systemd}"
  user="${parts[2]:-root}"
  port="${parts[3]:-22}"

  if [[ -z "${host}" ]]; then
    echo "WARN: skipping malformed line: ${raw_line}" >&2
    continue
  fi

  echo ""
  echo "═══════════════════════════════════════════════════════════════"
  echo "▶ ${user}@${host}:${port}  profiles=${profiles}  version=${AGENT_VERSION}"
  echo "═══════════════════════════════════════════════════════════════"

  # Ship install.sh to the host's /tmp, then run as root with the
  # right environment. SSH ControlMaster would speed this up but
  # we deliberately don't assume the operator has it configured.
  if ! scp -q -P "${port}" "${INSTALL_SCRIPT}" "${user}@${host}:/tmp/iclic-install.sh"; then
    echo "✗ scp failed" >&2
    FAILED_HOSTS+=("${host}")
    continue
  fi

  # `sudo -n` fails fast if the operator doesn't already have a
  # password-less sudoer entry on the target host. The alternative
  # (interactive password prompt per host) defeats the loop.
  remote_cmd="sudo -n env AGENT_VERSION='${AGENT_VERSION}' PROFILES='${profiles}' bash /tmp/iclic-install.sh"

  if ssh -p "${port}" -o BatchMode=yes "${user}@${host}" "${remote_cmd}"; then
    echo "✓ ${host} upgraded"
    SUCCEEDED_HOSTS+=("${host}")
  else
    echo "✗ ${host} install.sh exited non-zero" >&2
    FAILED_HOSTS+=("${host}")
  fi

  ssh -p "${port}" -o BatchMode=yes "${user}@${host}" "rm -f /tmp/iclic-install.sh" >/dev/null 2>&1 || true
done < "${INVENTORY}"

echo ""
echo "═══════════════════════════════════════════════════════════════"
echo "Summary — version ${AGENT_VERSION}"
echo "  succeeded: ${#SUCCEEDED_HOSTS[@]}"
for h in "${SUCCEEDED_HOSTS[@]}"; do echo "    ✓ ${h}"; done
echo "  failed:    ${#FAILED_HOSTS[@]}"
for h in "${FAILED_HOSTS[@]}"; do echo "    ✗ ${h}"; done
echo "═══════════════════════════════════════════════════════════════"

exit "${#FAILED_HOSTS[@]}"
