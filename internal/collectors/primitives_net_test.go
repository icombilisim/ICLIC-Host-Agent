package collectors

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHTTPProbeUp(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	got, err := httpProbe(context.Background(), map[string]any{"url": srv.URL})
	if err != nil {
		t.Fatalf("httpProbe: %v", err)
	}
	m := got.(map[string]any)
	if m["up"] != true {
		t.Fatalf("expected up=true, got %v", m)
	}
	if m["status"] != http.StatusOK {
		t.Fatalf("expected status 200, got %v", m["status"])
	}
	if m["latency_ms"].(int) < 0 {
		t.Fatalf("expected latency_ms >= 0, got %v", m["latency_ms"])
	}
}

func TestHTTPProbeDown(t *testing.T) {
	// Nothing listens on :1 — must report up=false, not error.
	got, err := httpProbe(context.Background(), map[string]any{
		"url": "http://127.0.0.1:1/", "timeout_sec": float64(1),
	})
	if err != nil {
		t.Fatalf("down probe should not error, got %v", err)
	}
	if got.(map[string]any)["up"] != false {
		t.Fatalf("expected up=false for unreachable, got %v", got)
	}
}
