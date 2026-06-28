package collectors

import (
	"context"
	"encoding/binary"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// security.snapshot is a composite collector that bundles host security
// telemetry — WAF/ModSecurity blocks, nginx 4xx, fail2ban bans, firewall
// drops — into one nested object the backend stores under
// metrics.security_snapshot. It powers the fleet security digest, which must
// be meaningful even on quiet days (the old per-host report showed all zeros).
//
// Each sub-source SELF-SKIPS when unavailable (missing container, unreadable
// log, insufficient privilege) so a host with a different stack reports only
// what it has — no per-server config, new server adapts automatically. (#707)
//
// Cadence: log scans are expensive, so the snapshot is collected at most once
// per `window_seconds` and the cached result is returned on the intervening
// heartbeats (same bytes → the backend dedups it, so only one row per window
// is stored). The cache also avoids WARN spam: a within-window tick never
// re-runs a failing source.
//
// Args:
//
//	window_seconds:   number  optional, default 3600 — collection window/cadence
//	socket:           string  optional, default /var/run/docker.sock
//	waf_container:    string  optional, default icosys-waf
//	nginx_container:  string  optional, default icosys-nginx
//	banned_ips_log:   string  optional, default /var/lib/icosys/auto-ban/banned-ips.log
//	firewall_chain:   string  optional, default DOCKER-USER
//
// Returns (sources absent are omitted):
//
//	{
//	  "collected_at": "2026-06-28T10:00:00Z", "window_seconds": 3600,
//	  "waf":      { "blocked": 1530, "by_class": { "sqli": 200, "rce": 50 } },
//	  "nginx":    { "http_4xx": 4200, "http_403": 300, "http_429": 12 },
//	  "fail2ban": { "banned_total": 340, "banned_window": 4 },
//	  "firewall": { "dropped_packets": 88000 }
//	}
func securitySnapshot(ctx context.Context, args map[string]any) (any, error) {
	window := time.Duration(int(argFloat(args, "window_seconds", 3600))) * time.Second
	if window < time.Minute {
		window = time.Minute
	}
	now := time.Now()

	// Serve the cached snapshot for the rest of the window. Returning the same
	// bytes lets the backend dedup; returning a value (not an error) keeps the
	// runner quiet when a source is down on a non-applicable host.
	secCacheMu.Lock()
	if !secCacheAt.IsZero() && now.Sub(secCacheAt) < window {
		cached := secCache
		secCacheMu.Unlock()
		if cached == nil {
			return nil, nil
		}
		return cached, nil
	}
	secCacheMu.Unlock()

	since := now.Add(-window)
	socket := argString(args, "socket", dockerDefaultSocket)
	out := map[string]any{
		"collected_at":   now.UTC().Format(time.RFC3339),
		"window_seconds": int(window.Seconds()),
	}
	if waf := collectWAFBlocks(ctx, socket, argString(args, "waf_container", "icosys-waf"), since); waf != nil {
		out["waf"] = waf
	}
	if ng := collectNginx4xx(ctx, socket, argString(args, "nginx_container", "icosys-nginx"), since); ng != nil {
		out["nginx"] = ng
	}
	if f2b := collectFail2ban(argString(args, "banned_ips_log", "/var/lib/icosys/auto-ban/banned-ips.log"), since); f2b != nil {
		out["fail2ban"] = f2b
	}
	if fw := collectFirewallDrops(ctx, argString(args, "firewall_chain", "DOCKER-USER")); fw != nil {
		out["firewall"] = fw
	}

	// Cache the attempt either way so a source-less host scans at most once per
	// window. Only emit when at least one real source produced data.
	secCacheMu.Lock()
	secCacheAt = now
	if len(out) <= 2 {
		secCache = nil
	} else {
		secCache = out
	}
	result := secCache
	secCacheMu.Unlock()

	if result == nil {
		return nil, nil
	}
	return result, nil
}

var (
	secCacheMu sync.Mutex
	secCache   map[string]any
	secCacheAt time.Time

	// ModSecurity attack-class tags (OWASP CRS), matched against the blocked
	// request log line.
	wafClassTags = map[string]string{
		"sqli":      "attack-sqli",
		"rce":       "attack-rce",
		"lfi":       "attack-lfi",
		"rfi":       "attack-rfi",
		"xss":       "attack-xss",
		"injection": "attack-injection-php",
		"scanner":   "scanner",
	}
	// nginx combined log: `... "GET /x HTTP/1.1" 403 1234 ...` — capture status.
	nginxStatusRe = regexp.MustCompile(`"\s(\d{3})\s`)
)

// collectWAFBlocks counts ModSecurity "Access denied" lines in the window and
// classifies them by attack tag. nil = WAF container unreachable (self-skip).
func collectWAFBlocks(ctx context.Context, socket, container string, since time.Time) map[string]any {
	lines := dockerLogLines(ctx, socket, container, since)
	if lines == nil {
		return nil
	}
	return countWAFBlocks(lines)
}

// countWAFBlocks tallies ModSecurity "Access denied" lines and classifies them
// by attack tag. Pure (no I/O) so it is unit-testable.
func countWAFBlocks(lines []string) map[string]any {
	blocked := 0
	byClass := map[string]int{}
	for _, ln := range lines {
		if !strings.Contains(ln, "ModSecurity: Access denied") {
			continue
		}
		blocked++
		for class, tag := range wafClassTags {
			if strings.Contains(ln, tag) {
				byClass[class]++
			}
		}
	}
	res := map[string]any{"blocked": blocked}
	if len(byClass) > 0 {
		res["by_class"] = byClass
	}
	return res
}

// collectNginx4xx counts 4xx responses (with 403/429 broken out) in the window
// from the nginx access log. nil = nginx container unreachable (self-skip).
func collectNginx4xx(ctx context.Context, socket, container string, since time.Time) map[string]any {
	lines := dockerLogLines(ctx, socket, container, since)
	if lines == nil {
		return nil
	}
	return countNginx4xx(lines)
}

// countNginx4xx tallies 4xx responses (with 403/429 broken out) from access-log
// lines. Pure (no I/O) so it is unit-testable.
func countNginx4xx(lines []string) map[string]any {
	var c4xx, c403, c429 int
	for _, ln := range lines {
		m := nginxStatusRe.FindStringSubmatch(ln)
		if m == nil {
			continue
		}
		code, _ := strconv.Atoi(m[1])
		if code < 400 || code >= 500 {
			continue
		}
		c4xx++
		switch code {
		case 403:
			c403++
		case 429:
			c429++
		}
	}
	return map[string]any{"http_4xx": c4xx, "http_403": c403, "http_429": c429}
}

// collectFail2ban reads the auto-ban log file (ip | ts UTC | reason). Reports
// cumulative distinct IPs banned and how many fell in the window. nil = the
// log is missing/unreadable, e.g. fail2ban isn't deployed here (self-skip).
func collectFail2ban(bannedLog string, since time.Time) map[string]any {
	data, err := readFileCapped(bannedLog, 8<<20)
	if err != nil {
		return nil
	}
	distinct := map[string]bool{}
	windowCount := 0
	for _, ln := range strings.Split(string(data), "\n") {
		parts := strings.Split(ln, "|")
		if len(parts) < 2 {
			continue
		}
		ip := strings.TrimSpace(parts[0])
		if ip == "" {
			continue
		}
		distinct[ip] = true
		// Timestamp form: "YYYY-MM-DD HH:MM:SS UTC".
		tsRaw := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(parts[1]), "UTC"))
		if ts, perr := time.Parse("2006-01-02 15:04:05", tsRaw); perr == nil && ts.After(since) {
			windowCount++
		}
	}
	return map[string]any{"banned_total": len(distinct), "banned_window": windowCount}
}

// collectFirewallDrops sums the DROP packet counters of a firewall chain
// (e.g. DOCKER-USER blocking non-Cloudflare origin traffic). Cumulative since
// the last rule reload. nil = iptables unavailable or no privilege, which is
// the common case for the unprivileged agent (self-skip; enabling it needs a
// deliberate CAP_NET_ADMIN grant in the unit). (#707)
func collectFirewallDrops(ctx context.Context, chain string) map[string]any {
	cctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	var out []byte
	var err error
	for _, bin := range []string{"iptables", "/usr/sbin/iptables", "/sbin/iptables"} {
		out, err = exec.CommandContext(cctx, bin, "-vnx", "-L", chain).Output()
		if err == nil {
			break
		}
	}
	if err != nil {
		return nil
	}
	var dropped int64
	for _, ln := range strings.Split(string(out), "\n") {
		f := strings.Fields(ln)
		// Columns: pkts bytes target prot opt in out source destination
		if len(f) >= 3 && f[2] == "DROP" {
			if n, perr := strconv.ParseInt(f[0], 10, 64); perr == nil {
				dropped += n
			}
		}
	}
	return map[string]any{"dropped_packets": dropped}
}

// dockerLogLines fetches a container's logs since `since` over the docker
// socket and returns them as text lines. nil on any error (container absent,
// socket unreadable) so callers self-skip. Reuses the shared docker client.
func dockerLogLines(ctx context.Context, socket, container string, since time.Time) []string {
	cctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	path := "/containers/" + url.PathEscape(container) +
		"/logs?stdout=1&stderr=1&timestamps=0&since=" + strconv.FormatInt(since.Unix(), 10)
	req, err := http.NewRequestWithContext(cctx, http.MethodGet, "http://docker"+path, nil)
	if err != nil {
		return nil
	}
	resp, err := dockerClientFor(socket).Do(req)
	if err != nil {
		return nil
	}
	defer drainAndClose(resp.Body)
	if resp.StatusCode >= 400 {
		return nil
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil || len(data) == 0 {
		return nil
	}
	text := demuxDockerLogStream(data)
	return strings.Split(text, "\n")
}

// demuxDockerLogStream strips docker's 8-byte multiplexing frame headers from a
// non-TTY log stream. Falls back to the raw bytes when the data isn't framed
// (TTY containers send plain text).
func demuxDockerLogStream(data []byte) string {
	var sb strings.Builder
	i := 0
	frames := 0
	for i+8 <= len(data) {
		st := data[i]
		if (st == 0 || st == 1 || st == 2) && data[i+1] == 0 && data[i+2] == 0 && data[i+3] == 0 {
			size := int(binary.BigEndian.Uint32(data[i+4 : i+8]))
			if size < 0 || i+8+size > len(data) {
				break
			}
			sb.Write(data[i+8 : i+8+size])
			i += 8 + size
			frames++
		} else {
			break
		}
	}
	if frames > 0 {
		return sb.String()
	}
	return string(data)
}

// readFileCapped reads up to maxBytes of a file. The cap keeps an ever-growing
// ban log (no logrotate) from ballooning agent memory.
func readFileCapped(path string, maxBytes int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, maxBytes))
}
