package heartbeat

import (
	"os"
	"path/filepath"
	"testing"
)

// TestRecordDesiredVersion covers the persistence contract the Phase 3 updater
// relies on: empty = no file, write-on-change, atomic overwrite. (#480)
func TestRecordDesiredVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "desired-version")
	t.Setenv("ICLIC_AGENT_DESIRED_VERSION_FILE", path)

	// No directive → no file created.
	recordDesiredVersion("")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("empty desired should not create the file, stat err = %v", err)
	}

	// First directive is written with a trailing newline.
	recordDesiredVersion("v0.17.0")
	if got := readFile(t, path); got != "v0.17.0\n" {
		t.Fatalf("after first write got %q", got)
	}

	// Repeating the same value keeps the content stable.
	recordDesiredVersion("v0.17.0")
	if got := readFile(t, path); got != "v0.17.0\n" {
		t.Fatalf("after repeat got %q", got)
	}

	// A new value overwrites atomically and leaves no .tmp behind.
	recordDesiredVersion("v0.18.0")
	if got := readFile(t, path); got != "v0.18.0\n" {
		t.Fatalf("after change got %q", got)
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Fatal("temp file should not survive a successful write")
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}
