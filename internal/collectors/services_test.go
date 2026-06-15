package collectors

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExpandServiceAxes(t *testing.T) {
	s := serviceDef{
		Name:    "tleasy",
		Up:      axis{"tcp": 8080},
		Health:  axis{"http": "http://127.0.0.1:8080/health", "path": "status"},
		Version: axis{"exec": []any{"/opt/tleasy/bin/version"}, "parse": "trimmed"},
		Metrics: []serviceMetric{{Key: "queue_depth", Exec: []string{"/opt/tleasy/bin/q"}, Parse: "int"}},
	}
	got := map[string]Binding{}
	bs, err := expandService(s)
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range bs {
		got[b.OutputKey] = b
	}

	if b := got["tleasy_up"]; b.Primitive != "tcp.connect" || b.Args["port"] != 8080 || b.Args["host"] != "127.0.0.1" {
		t.Fatalf("up: %+v", b)
	}
	if b := got["tleasy_health"]; b.Primitive != "http.get_json" || b.Args["path"] != "status" {
		t.Fatalf("health: %+v", b)
	}
	if b := got["tleasy_version"]; b.Primitive != "exec" || b.Args["parse"] != "trimmed" {
		t.Fatalf("version: %+v", b)
	}
	if b := got["tleasy_queue_depth"]; b.Primitive != "exec" || b.Args["parse"] != "int" {
		t.Fatalf("metric: %+v", b)
	}
}

func TestExpandServiceDockerAndSystemd(t *testing.T) {
	bs, err := expandService(serviceDef{Name: "x", Up: axis{"docker": "x-container"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(bs) != 1 || bs[0].Primitive != "exec" || bs[0].OutputKey != "x_up" {
		t.Fatalf("docker up: %+v", bs)
	}
	bs2, err := expandService(serviceDef{Name: "y", Up: axis{"systemd": "y.service"}})
	if err != nil {
		t.Fatal(err)
	}
	if bs2[0].Primitive != "systemctl.is_active" || bs2[0].Args["unit"] != "y.service" {
		t.Fatalf("systemd up: %+v", bs2[0])
	}
}

func TestExpandServiceUnknownAxisErrors(t *testing.T) {
	if _, err := expandService(serviceDef{Name: "z", Up: axis{"wat": 1}}); err == nil {
		t.Fatal("expected error for unknown probe key")
	}
}

func TestLoadServiceDir(t *testing.T) {
	dir := t.TempDir()
	yaml := "service:\n  name: app\n  label: App\n  up: { tcp: 9000 }\n  metrics:\n    - { key: q, exec: [echo, \"1\"], parse: int }\n"
	if err := os.WriteFile(filepath.Join(dir, "app.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	// A non-service file in the dir must be skipped, not error.
	if err := os.WriteFile(filepath.Join(dir, "note.yaml"), []byte("just: notes\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	bs, err := LoadServiceDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	keys := map[string]bool{}
	for _, b := range bs {
		keys[b.OutputKey] = true
	}
	if !keys["app_up"] || !keys["app_q"] {
		t.Fatalf("expected app_up + app_q, got %v", keys)
	}
}
