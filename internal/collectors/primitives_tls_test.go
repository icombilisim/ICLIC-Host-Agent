package collectors

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
)

func TestSSLCertExpiry(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer srv.Close()

	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}
	host, portStr, err := net.SplitHostPort(u.Host)
	if err != nil {
		t.Fatalf("split host:port: %v", err)
	}
	port, _ := strconv.Atoi(portStr)

	// Numbers arrive from YAML/JSON as float64 — mirror that here.
	got, err := sslCertExpiry(context.Background(), map[string]any{
		"host": host,
		"port": float64(port),
	})
	if err != nil {
		t.Fatalf("sslCertExpiry: %v", err)
	}
	days, ok := got.(int)
	if !ok {
		t.Fatalf("expected int days, got %T", got)
	}
	if days <= 0 {
		t.Fatalf("expected positive days until expiry, got %d", days)
	}
}

func TestSSLCertExpiryUnreachable(t *testing.T) {
	// Nothing listens on :1 — must return an error (metric omitted), never panic.
	if _, err := sslCertExpiry(context.Background(), map[string]any{
		"host": "127.0.0.1", "port": float64(1), "timeout_sec": float64(1),
	}); err == nil {
		t.Fatal("expected error dialing an unused port")
	}
}
