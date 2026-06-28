package collectors

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDemuxDockerLogStreamRaw(t *testing.T) {
	raw := "line1\nline2\n"
	if got := demuxDockerLogStream([]byte(raw)); got != raw {
		t.Fatalf("raw passthrough: got %q want %q", got, raw)
	}
}

func TestDemuxDockerLogStreamFramed(t *testing.T) {
	frame := func(payload string) []byte {
		h := make([]byte, 8)
		h[0] = 1 // stdout stream
		binary.BigEndian.PutUint32(h[4:], uint32(len(payload)))
		return append(h, []byte(payload)...)
	}
	data := append(frame("hello\n"), frame("world\n")...)
	if got := demuxDockerLogStream(data); got != "hello\nworld\n" {
		t.Fatalf("framed demux: got %q", got)
	}
}

func TestCountNginx4xx(t *testing.T) {
	lines := []string{
		`1.2.3.4 - - [27/Jun/2026:12:00:00 +0000] "GET /a HTTP/1.1" 200 12 "-" "ua"`,
		`1.2.3.4 - - [27/Jun/2026:12:00:01 +0000] "GET /b HTTP/1.1" 403 0 "-" "ua"`,
		`1.2.3.4 - - [27/Jun/2026:12:00:02 +0000] "POST /c HTTP/1.1" 429 0 "-" "ua"`,
		`1.2.3.4 - - [27/Jun/2026:12:00:03 +0000] "GET /d HTTP/1.1" 404 0 "-" "ua"`,
		`1.2.3.4 - - [27/Jun/2026:12:00:04 +0000] "GET /e HTTP/1.1" 500 0 "-" "ua"`,
	}
	got := countNginx4xx(lines)
	if got["http_4xx"] != 3 || got["http_403"] != 1 || got["http_429"] != 1 {
		t.Fatalf("nginx counts: %+v", got)
	}
}

func TestCountWAFBlocks(t *testing.T) {
	lines := []string{
		`[client 1.2.3.4] ModSecurity: Access denied with code 403 ... [tag "attack-sqli"]`,
		`[client 5.6.7.8] ModSecurity: Access denied with code 403 ... [tag "attack-rce"]`,
		`[client 9.9.9.9] normal request, status 200`,
	}
	got := countWAFBlocks(lines)
	if got["blocked"] != 2 {
		t.Fatalf("waf blocked: %+v", got)
	}
	byClass, ok := got["by_class"].(map[string]int)
	if !ok || byClass["sqli"] != 1 || byClass["rce"] != 1 {
		t.Fatalf("waf by_class: %+v", got["by_class"])
	}
}

func TestCollectFail2ban(t *testing.T) {
	p := filepath.Join(t.TempDir(), "banned-ips.log")
	content := "1.2.3.4 | 2026-06-27 12:00:00 UTC | modsec\n" +
		"5.6.7.8 | 2026-06-27 09:00:00 UTC | scanner\n" +
		"1.2.3.4 | 2026-06-27 12:05:00 UTC | modsec\n" // duplicate IP, distinct line
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	since, _ := time.Parse("2006-01-02 15:04:05", "2026-06-27 11:00:00")
	got := collectFail2ban(p, since)
	if got == nil {
		t.Fatal("expected a result for a readable log")
	}
	if got["banned_total"] != 2 { // distinct IPs
		t.Fatalf("banned_total: %+v", got)
	}
	if got["banned_window"] != 2 { // two lines after 11:00 (12:00 and 12:05)
		t.Fatalf("banned_window: %+v", got)
	}
}

func TestCollectFail2banMissing(t *testing.T) {
	if got := collectFail2ban(filepath.Join(t.TempDir(), "nope.log"), time.Now()); got != nil {
		t.Fatalf("missing file should self-skip, got %+v", got)
	}
}
