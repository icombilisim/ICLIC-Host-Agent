// Package release verifies the authenticity of a downloaded release before the
// installer or the (future) auto-updater trusts its bytes. SHA256SUMS proves
// integrity (no truncation/corruption); this Ed25519 signature over SHA256SUMS
// proves authenticity (the bytes came from us, not from whoever compromised the
// GitHub release). Auto-update makes this non-optional. (#35)
package release

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
)

// releasePublicKeyB64 is the base64-encoded raw Ed25519 public key (32 bytes)
// that matches the private key held in the AGENT_RELEASE_SIGNING_KEY GitHub
// secret. Generated 2026-06-22 via scripts/gen-release-signing-key.sh; verified
// end-to-end against the v0.16.0 release signature. Rotate by re-running the
// script (new secret + new key here + installer PEM). (#35)
const releasePublicKeyB64 = "jGLvOjNYFDA8tHhsFFBaLXT9UKxyChImRaqY4sDOsQY="

// ErrKeyNotConfigured is returned while the signing public key is still the
// build-time placeholder. Callers must treat it as "cannot verify" and refuse
// to install. (#35)
var ErrKeyNotConfigured = errors.New("release signing public key not configured (placeholder still in place)")

// publicKey decodes the embedded key, refusing the placeholder.
func publicKey() (ed25519.PublicKey, error) {
	if releasePublicKeyB64 == "" || releasePublicKeyB64 == "REPLACE_WITH_BASE64_ED25519_PUBLIC_KEY" {
		return nil, ErrKeyNotConfigured
	}
	raw, err := base64.StdEncoding.DecodeString(releasePublicKeyB64)
	if err != nil {
		return nil, fmt.Errorf("decode release public key: %w", err)
	}
	if len(raw) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("release public key must be %d bytes, got %d", ed25519.PublicKeySize, len(raw))
	}
	return ed25519.PublicKey(raw), nil
}

// Verify returns nil only when sigPath holds a valid Ed25519 signature, made
// with the release signing key, over the exact bytes of sumsPath. Every other
// outcome — missing file, wrong size, bad signature, unconfigured key — is an
// error, so callers fail closed. (#35)
func Verify(sumsPath, sigPath string) error {
	pub, err := publicKey()
	if err != nil {
		return err
	}
	return verifyWith(pub, sumsPath, sigPath)
}

// verifyWith is the key-agnostic core, split out so tests can exercise the
// crypto with a generated key while the embedded key stays a placeholder.
func verifyWith(pub ed25519.PublicKey, sumsPath, sigPath string) error {
	msg, err := os.ReadFile(sumsPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", sumsPath, err)
	}
	sig, err := os.ReadFile(sigPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", sigPath, err)
	}
	if len(sig) != ed25519.SignatureSize {
		return fmt.Errorf("signature must be %d bytes, got %d", ed25519.SignatureSize, len(sig))
	}
	if !ed25519.Verify(pub, msg, sig) {
		return errors.New("signature verification failed")
	}
	return nil
}
