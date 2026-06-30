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

func TestFilterLinesSinceModsec(t *testing.T) {
	now := time.Now()
	layout := "2006/01/02 15:04:05"
	recent := now.Add(-10*time.Minute).Format(layout) + " [error] ModSecurity: Access denied"
	old := now.Add(-3*time.Hour).Format(layout) + " [error] ModSecurity: Access denied"
	in := []string{recent, old, "no-timestamp noise line"}
	got := filterLinesSince(in, modsecLogTimeRe, layout, time.Local, now.Add(-1*time.Hour))
	if len(got) != 1 || got[0] != recent {
		t.Fatalf("modsec window filter: %+v", got)
	}
}

func TestFilterLinesSinceNginx(t *testing.T) {
	layout := "02/Jan/2006:15:04:05 -0700"
	since, _ := time.Parse(layout, "27/Jun/2026:11:00:00 +0000")
	recent := `1.2.3.4 - - [27/Jun/2026:12:00:00 +0000] "GET /a HTTP/1.1" 403 0 "-" "ua"`
	old := `1.2.3.4 - - [27/Jun/2026:09:00:00 +0000] "GET /b HTTP/1.1" 403 0 "-" "ua"`
	got := filterLinesSince([]string{recent, old}, nginxAccessTimeRe, layout, nil, since)
	if len(got) != 1 || got[0] != recent {
		t.Fatalf("nginx window filter: %+v", got)
	}
}

func TestCollectWAFBlocksFile(t *testing.T) {
	now := time.Now()
	layout := "2006/01/02 15:04:05"
	p := filepath.Join(t.TempDir(), "error.log")
	content := now.Add(-5*time.Minute).Format(layout) + ` [error] x ModSecurity: Access denied with code 403 [tag "attack-sqli"]` + "\n" +
		now.Add(-2*time.Minute).Format(layout) + ` [error] x ModSecurity: Access denied with code 403 [tag "attack-rce"]` + "\n" +
		now.Add(-3*time.Hour).Format(layout) + ` [error] x ModSecurity: Access denied with code 403` + "\n" // old, excluded
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	got := collectWAFBlocksFile(p, now.Add(-1*time.Hour))
	if got == nil || got["blocked"] != 2 {
		t.Fatalf("waf file blocked (window): %+v", got)
	}
}

func TestCollectWAFBlocksFileMissing(t *testing.T) {
	if got := collectWAFBlocksFile(filepath.Join(t.TempDir(), "nope.log"), time.Now()); got != nil {
		t.Fatalf("missing file should self-skip, got %+v", got)
	}
}

func TestCollectNginx4xxFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "access.log")
	content := `1.2.3.4 - - [27/Jun/2026:12:00:00 +0000] "GET /a HTTP/1.1" 403 0 "-" "ua"` + "\n" +
		`1.2.3.4 - - [27/Jun/2026:12:00:01 +0000] "GET /b HTTP/1.1" 200 0 "-" "ua"` + "\n" +
		`1.2.3.4 - - [27/Jun/2026:09:00:00 +0000] "GET /c HTTP/1.1" 404 0 "-" "ua"` + "\n" // old, excluded
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	since, _ := time.Parse("02/Jan/2006:15:04:05 -0700", "27/Jun/2026:11:00:00 +0000")
	got := collectNginx4xxFile(p, since)
	if got == nil || got["http_4xx"] != 1 || got["http_403"] != 1 {
		t.Fatalf("nginx file 4xx (window): %+v", got)
	}
}

func TestReadFileTailCapped(t *testing.T) {
	p := filepath.Join(t.TempDir(), "f.log")
	if err := os.WriteFile(p, []byte("AAAA\nBBBB\nCCCC\n"), 0o644); err != nil { // 15 bytes
		t.Fatal(err)
	}
	got, err := readFileTailCapped(p, 5) // last 5 bytes
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "CCCC\n" {
		t.Fatalf("tail read: got %q want %q", string(got), "CCCC\n")
	}
}
