package release

import (
	"crypto/ed25519"
	"os"
	"path/filepath"
	"testing"
)

// writeTemp writes content to a temp file and returns its path.
func writeTemp(t *testing.T, name string, content []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, content, 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return p
}

func TestVerifyWith(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("keygen: %v", err)
	}
	sums := []byte("deadbeef  iclic-host-agent-linux-amd64\n")
	sumsPath := writeTemp(t, "SHA256SUMS", sums)
	sigPath := writeTemp(t, "SHA256SUMS.sig", ed25519.Sign(priv, sums))

	// Valid signature passes.
	if err := verifyWith(pub, sumsPath, sigPath); err != nil {
		t.Fatalf("expected valid signature, got %v", err)
	}

	// Tampered sums fail closed.
	tampered := writeTemp(t, "SHA256SUMS.bad", append(sums, 'x'))
	if err := verifyWith(pub, tampered, sigPath); err == nil {
		t.Fatal("expected failure on tampered sums, got nil")
	}

	// Wrong key fails closed.
	otherPub, _, _ := ed25519.GenerateKey(nil)
	if err := verifyWith(otherPub, sumsPath, sigPath); err == nil {
		t.Fatal("expected failure with wrong key, got nil")
	}

	// Malformed signature length fails closed.
	shortSig := writeTemp(t, "short.sig", []byte("too-short"))
	if err := verifyWith(pub, sumsPath, shortSig); err == nil {
		t.Fatal("expected failure on short signature, got nil")
	}
}

func TestVerifyFailsClosedWithPlaceholderKey(t *testing.T) {
	// The shipped placeholder must never verify — Verify() must refuse rather
	// than trust an unsigned release. (#35)
	if releasePublicKeyB64 != "REPLACE_WITH_BASE64_ED25519_PUBLIC_KEY" {
		t.Skip("real key configured; placeholder fail-closed test not applicable")
	}
	if err := Verify("/nonexistent/SHA256SUMS", "/nonexistent/SHA256SUMS.sig"); err == nil {
		t.Fatal("expected ErrKeyNotConfigured with placeholder key, got nil")
	}
}
