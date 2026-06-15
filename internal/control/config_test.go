package control

import (
	"os"
	"path/filepath"
	"testing"
)

// A service's logs: block becomes a tailable control source — without re-listing
// it in control.yaml — once the operator has enabled the channel + logs.
func TestLoadControlConfigMergesServiceLogs(t *testing.T) {
	dir := t.TempDir()
	controlPath := filepath.Join(dir, "control.yaml")
	if err := os.WriteFile(controlPath, []byte("control:\n  enabled: true\n  logs:\n    enabled: true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	svcDir := filepath.Join(dir, "services.d")
	if err := os.MkdirAll(svcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(svcDir, "app.yaml"),
		[]byte("service:\n  name: app\n  logs: { type: file, path: /var/log/app.log }\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv(envControlConfigPath, controlPath)
	cfg := loadControlConfig()

	src, ok := cfg.source("app")
	if !ok || src.Type != "file" || src.Path != "/var/log/app.log" {
		t.Fatalf("expected merged file source for 'app', got %+v ok=%v", src, ok)
	}
	if !cfg.logsEnabled() {
		t.Fatal("logs should be enabled once a service source is merged")
	}
}

// The opt-in still governs: a service-def must NOT expose logs if the operator
// hasn't enabled the channel's logs.
func TestServiceLogsRequireOptIn(t *testing.T) {
	dir := t.TempDir()
	controlPath := filepath.Join(dir, "control.yaml")
	if err := os.WriteFile(controlPath, []byte("control:\n  enabled: true\n  logs:\n    enabled: false\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	svcDir := filepath.Join(dir, "services.d")
	if err := os.MkdirAll(svcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(svcDir, "app.yaml"),
		[]byte("service:\n  name: app\n  logs: { type: file, path: /var/log/app.log }\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv(envControlConfigPath, controlPath)
	cfg := loadControlConfig()
	if _, ok := cfg.source("app"); ok {
		t.Fatal("service log source must NOT be served when logs are not opted in")
	}
}
