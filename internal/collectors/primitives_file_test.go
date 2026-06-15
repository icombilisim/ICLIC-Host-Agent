package collectors

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestFileNewestAgeSeconds(t *testing.T) {
	dir := t.TempDir()
	glob := filepath.Join(dir, "*.sql.gz")

	// No backup yet -> -1.
	got, err := fileNewestAgeSeconds(context.Background(), map[string]any{"glob": glob})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got.(int64) != -1 {
		t.Fatalf("expected -1 when nothing matches, got %v", got)
	}

	// A fresh backup -> small non-negative age.
	if err := os.WriteFile(filepath.Join(dir, "dump-1.sql.gz"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err = fileNewestAgeSeconds(context.Background(), map[string]any{"glob": glob})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if age := got.(int64); age < 0 || age > 60 {
		t.Fatalf("expected small non-negative age, got %d", age)
	}
}

func TestFileStatAgeSeconds(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f")
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := fileStat(context.Background(), map[string]any{"path": p, "field": "age_seconds"})
	if err != nil {
		t.Fatal(err)
	}
	if got.(int64) < 0 {
		t.Fatalf("expected age >= 0, got %v", got)
	}
	// Missing file -> -1 sentinel.
	missing, _ := fileStat(context.Background(), map[string]any{"path": filepath.Join(dir, "nope"), "field": "age_seconds"})
	if missing.(int64) != -1 {
		t.Fatalf("expected -1 for missing file, got %v", missing)
	}
}
