#!/usr/bin/env bash
# One-time ceremony: generate the Ed25519 keypair that signs releases. (#35)
#
# It prints three artefacts and tells you exactly where each one goes:
#   1. PRIVATE key PEM  → GitHub repo secret  AGENT_RELEASE_SIGNING_KEY
#   2. PUBLIC key PEM   → installer/install.sh  RELEASE_PUBKEY_PEM
#   3. PUBLIC key b64   → internal/release/verify.go  releasePublicKeyB64
#
# The private key is printed to your terminal ONLY — it is never written to
# disk by this script. Paste it straight into the GitHub secret, then close the
# terminal. Losing it just means generating a new pair and re-embedding the
# public halves; a leak means an attacker can sign releases, so treat it like a
# production credential.
#
# Requires OpenSSL 3 (Ed25519 raw verify). Usage:  bash scripts/gen-release-signing-key.sh
set -euo pipefail

if ! command -v openssl >/dev/null 2>&1; then
  echo "ERROR: openssl is required." >&2
  exit 1
fi

WORK="$(mktemp -d)"
trap 'rm -rf "${WORK}"' EXIT

openssl genpkey -algorithm ed25519 -out "${WORK}/priv.pem" 2>/dev/null
openssl pkey -in "${WORK}/priv.pem" -pubout -out "${WORK}/pub.pem" 2>/dev/null
PUB_B64="$(openssl pkey -in "${WORK}/priv.pem" -pubout -outform DER 2>/dev/null | tail -c 32 | base64 | tr -d '\n')"

line() { printf '%s\n' "──────────────────────────────────────────────────────────────"; }

echo
line
echo "1) GitHub repo secret  →  AGENT_RELEASE_SIGNING_KEY"
echo "   (Settings → Secrets and variables → Actions → New repository secret)"
line
cat "${WORK}/priv.pem"
line
echo
line
echo "2) installer/install.sh  →  replace the RELEASE_PUBKEY_PEM block with:"
line
cat "${WORK}/pub.pem"
line
echo
line
echo "3) internal/release/verify.go  →  set releasePublicKeyB64 to:"
line
echo "${PUB_B64}"
line
echo
echo "After embedding (2) and (3) and saving (1), commit the two source changes."
echo "The next release will be signed and the auto-updater will enforce it."
