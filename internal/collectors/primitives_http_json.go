package collectors

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// httpGetJSON fetches a JSON URL and either returns the whole document (when
// no path is given) or extracts a single value by dotted path. Built for
// Spring Boot's /actuator/* endpoints — `/actuator/health` and
// `/actuator/metrics/{id}` together cover the headline app-health signals
// the user wants on ServerDetail without writing a Spring-specific
// collector. Generic enough to scrape any JSON-shaped admin endpoint
// (Nexus, SonarQube, Dokploy) the operator points it at.
//
// Args:
//
//	url:         string  required
//	path:        string  optional — dotted path; "" returns the whole document
//	header:      map     optional — extra request headers (Authorization, …)
//	basic_user:  string  optional — pair with basic_pass for HTTP Basic auth
//	basic_pass:  string  optional
//	timeout_sec: number  optional, default 4
//
// Path syntax:
//
//	"status"                 → top-level key
//	"components.db.status"   → nested keys
//	"measurements.0.value"   → array index supported as a numeric segment
//
// Numbers and booleans are returned as their JSON-decoded Go types
// (float64 / bool); strings as string. Missing keys yield nil with no error
// — the binding's metric is omitted from the heartbeat in that case.
func httpGetJSON(ctx context.Context, args map[string]any) (any, error) {
	url := argString(args, "url", "")
	if url == "" {
		return nil, fmt.Errorf("url: required")
	}
	timeout := time.Duration(argFloat(args, "timeout_sec", 4) * float64(time.Second))
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(cctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "iclic-host-agent collector probe")
	req.Header.Set("Accept", "application/json")
	if hdr, ok := args["header"].(map[string]any); ok {
		for k, v := range hdr {
			if s, ok := v.(string); ok {
				req.Header.Set(k, s)
			}
		}
	}
	if user := argString(args, "basic_user", ""); user != "" {
		req.SetBasicAuth(user, argString(args, "basic_pass", ""))
	}

	client := &http.Client{Timeout: timeout + time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var doc any
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&doc); err != nil {
		return nil, fmt.Errorf("json decode: %w", err)
	}

	path := argString(args, "path", "")
	if path == "" {
		return doc, nil
	}
	return jsonPath(doc, path), nil
}

// jsonPath walks doc by dot-segments. Numeric segments index arrays; string
// segments key into maps. Returns nil at any miss — callers omit the metric.
func jsonPath(doc any, path string) any {
	if path == "" {
		return doc
	}
	cur := doc
	for _, seg := range strings.Split(path, ".") {
		switch v := cur.(type) {
		case map[string]any:
			cur = v[seg]
		case []any:
			i, err := strconv.Atoi(seg)
			if err != nil || i < 0 || i >= len(v) {
				return nil
			}
			cur = v[i]
		default:
			return nil
		}
		if cur == nil {
			return nil
		}
	}
	return cur
}
