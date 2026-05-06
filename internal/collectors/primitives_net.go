package collectors

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"time"
)

// tcpConnect probes whether a TCP port accepts connections. Returns true on
// successful three-way handshake within timeout, false on any error.
//
// Args:
//
//	host:        string  required
//	port:        number  required
//	timeout_sec: number  optional, default 2
func tcpConnect(ctx context.Context, args map[string]any) (any, error) {
	host, _ := args["host"].(string)
	if host == "" {
		return nil, fmt.Errorf("host: required")
	}
	port := int(argFloat(args, "port", 0))
	if port <= 0 || port > 65535 {
		return nil, fmt.Errorf("port: required, must be 1..65535")
	}
	timeoutSec := argFloat(args, "timeout_sec", 2)
	d := net.Dialer{Timeout: time.Duration(timeoutSec * float64(time.Second))}
	cctx, cancel := context.WithTimeout(ctx, time.Duration((timeoutSec+1)*float64(time.Second)))
	defer cancel()

	conn, err := d.DialContext(cctx, "tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		return false, nil
	}
	_ = conn.Close()
	return true, nil
}

// httpGet does a single GET and reports either the status code (default) or
// a boolean "is the response in the expected range". Used for cheap liveness
// probes against legacy admin endpoints (/manager/text, /server-status, etc.)
// without the operator having to script curl.
//
// Args:
//
//	url:         string  required
//	timeout_sec: number  optional, default 3
//	expect:     "code" (default — return status int) | "ok" (return 200..299 bool)
func httpGet(ctx context.Context, args map[string]any) (any, error) {
	url, _ := args["url"].(string)
	if url == "" {
		return nil, fmt.Errorf("url: required")
	}
	timeoutSec := argFloat(args, "timeout_sec", 3)
	cctx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec*float64(time.Second)))
	defer cancel()

	req, err := http.NewRequestWithContext(cctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "iclic-host-agent collector probe")
	client := &http.Client{Timeout: time.Duration((timeoutSec + 1) * float64(time.Second))}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	expect, _ := args["expect"].(string)
	switch expect {
	case "", "code":
		return resp.StatusCode, nil
	case "ok":
		return resp.StatusCode >= 200 && resp.StatusCode < 300, nil
	default:
		return nil, fmt.Errorf("unknown expect %q (want code | ok)", expect)
	}
}
