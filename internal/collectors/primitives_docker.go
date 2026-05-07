package collectors

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// Docker primitives talk to dockerd directly via the Unix socket
// (/var/run/docker.sock) instead of shelling out to the `docker` CLI. This
// keeps the agent self-contained — no PATH lookups, no fork/exec per tick,
// and we get structured JSON back instead of parsing `docker ps` columns.
// One concession: the agent process must be in the docker group (or run as
// root) so it can read the socket. Same auth model as the systemd unit
// already uses for /proc, /sys, etc.

const (
	dockerDefaultSocket  = "/var/run/docker.sock"
	dockerDefaultTimeout = 4 * time.Second
)

// dockerClient builds an http.Client whose transport dials a Unix socket.
// We don't reuse it across primitives — the cost is a single Dial per tick
// and the agent's collector model expects each binding to be independent.
func dockerClient(socket string, timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, "unix", socket)
			},
		},
	}
}

func dockerGet(ctx context.Context, socket, path string, timeout time.Duration, out any) error {
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodGet, "http://docker"+path, nil)
	if err != nil {
		return err
	}
	resp, err := dockerClient(socket, timeout).Do(req)
	if err != nil {
		return fmt.Errorf("docker socket %s: %w", socket, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("docker %s: HTTP %d: %s", path, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// dockerContainers returns one row per container plus a state-bucket summary.
// We render the summary on the Servers page (running/exited/restarting badges)
// and the per-container rows in the new "Containers" section on ServerDetail.
//
// Args:
//
//	socket:      string  optional, default /var/run/docker.sock
//	all:         bool    optional, default true (include stopped containers)
//	timeout_sec: number  optional, default 4
//
// Returns:
//
//	{
//	  "total": 12, "running": 9, "exited": 2, "restarting": 1,
//	  "paused": 0, "dead": 0, "created": 0,
//	  "list": [{ name, image, state, status, restart_count, started_at }]
//	}
func dockerContainers(ctx context.Context, args map[string]any) (any, error) {
	socket := argString(args, "socket", dockerDefaultSocket)
	all := argBool(args, "all", true)
	timeout := time.Duration(argFloat(args, "timeout_sec", 4) * float64(time.Second))

	type rawContainer struct {
		ID      string   `json:"Id"`
		Names   []string `json:"Names"`
		Image   string   `json:"Image"`
		State   string   `json:"State"`
		Status  string   `json:"Status"`
		Created int64    `json:"Created"`
	}

	var raws []rawContainer
	path := "/containers/json"
	if all {
		path += "?all=true"
	}
	if err := dockerGet(ctx, socket, path, timeout, &raws); err != nil {
		return nil, err
	}

	type containerRow struct {
		Name         string `json:"name"`
		Image        string `json:"image"`
		State        string `json:"state"`
		Status       string `json:"status"`
		RestartCount int    `json:"restart_count"`
	}
	rows := make([]containerRow, 0, len(raws))
	counts := map[string]int{
		"running": 0, "exited": 0, "restarting": 0,
		"paused": 0, "dead": 0, "created": 0, "removing": 0,
	}
	for _, c := range raws {
		name := "?"
		if len(c.Names) > 0 {
			name = strings.TrimPrefix(c.Names[0], "/")
		}
		// /containers/json gives state but not RestartCount — we fetch
		// it on-demand from /containers/{id}/json. Skipping for non-running
		// to avoid N round-trips per tick when most are off.
		restartCount := 0
		if c.State == "restarting" || c.State == "running" {
			restartCount = dockerRestartCount(ctx, socket, c.ID, timeout)
		}
		rows = append(rows, containerRow{
			Name: name, Image: c.Image, State: c.State,
			Status: c.Status, RestartCount: restartCount,
		})
		if _, ok := counts[c.State]; ok {
			counts[c.State]++
		}
	}

	return map[string]any{
		"total":      len(raws),
		"running":    counts["running"],
		"exited":     counts["exited"],
		"restarting": counts["restarting"],
		"paused":     counts["paused"],
		"dead":       counts["dead"],
		"created":    counts["created"],
		"removing":   counts["removing"],
		"list":       rows,
	}, nil
}

// dockerRestartCount fetches a single container's RestartCount. The /json/{id}
// inspect endpoint is the only way — /containers/json doesn't include it.
func dockerRestartCount(ctx context.Context, socket, id string, timeout time.Duration) int {
	var inspect struct {
		RestartCount int `json:"RestartCount"`
	}
	if err := dockerGet(ctx, socket, "/containers/"+id+"/json", timeout, &inspect); err != nil {
		return 0
	}
	return inspect.RestartCount
}

// dockerStats fetches CPU + memory for every running container. Docker's
// `stats` endpoint with stream=false returns a single snapshot — we compute
// CPU% the same way `docker stats` does (delta of cpu_usage.total_usage over
// delta of system_cpu_usage, multiplied by online CPUs).
//
// Args:
//
//	socket:      string  optional, default /var/run/docker.sock
//	timeout_sec: number  optional, default 6 (per-tick budget across containers)
//
// Returns: list of { name, image, state, cpu_pct, mem_used_mb, mem_limit_mb,
//
//	mem_pct, restart_count }
func dockerStats(ctx context.Context, args map[string]any) (any, error) {
	socket := argString(args, "socket", dockerDefaultSocket)
	timeout := time.Duration(argFloat(args, "timeout_sec", 6) * float64(time.Second))

	type rawContainer struct {
		ID    string   `json:"Id"`
		Names []string `json:"Names"`
		Image string   `json:"Image"`
		State string   `json:"State"`
	}
	var running []rawContainer
	if err := dockerGet(ctx, socket, "/containers/json", timeout, &running); err != nil {
		return nil, err
	}

	// Per-container budget: split the timeout across them so one slow stats
	// call can't starve the rest. Floor at 500ms so the dial+JSON overhead
	// has a chance.
	per := timeout
	if n := len(running); n > 1 {
		per = timeout / time.Duration(n)
		if per < 500*time.Millisecond {
			per = 500 * time.Millisecond
		}
	}

	type statsRow struct {
		Name         string  `json:"name"`
		Image        string  `json:"image"`
		State        string  `json:"state"`
		CPUPct       float64 `json:"cpu_pct"`
		MemUsedMB    int64   `json:"mem_used_mb"`
		MemLimitMB   int64   `json:"mem_limit_mb"`
		MemPct       float64 `json:"mem_pct"`
		RestartCount int     `json:"restart_count"`
	}
	out := make([]statsRow, 0, len(running))
	for _, c := range running {
		name := "?"
		if len(c.Names) > 0 {
			name = strings.TrimPrefix(c.Names[0], "/")
		}
		row := statsRow{
			Name: name, Image: c.Image, State: c.State,
			RestartCount: dockerRestartCount(ctx, socket, c.ID, per),
		}

		var s dockerStatsResponse
		if err := dockerGet(ctx, socket, "/containers/"+c.ID+"/stats?stream=false", per, &s); err == nil {
			row.CPUPct = roundTo(calcCPUPct(s), 2)
			row.MemUsedMB = s.MemoryStats.Usage / (1024 * 1024)
			row.MemLimitMB = s.MemoryStats.Limit / (1024 * 1024)
			if s.MemoryStats.Limit > 0 {
				row.MemPct = roundTo(float64(s.MemoryStats.Usage)*100.0/float64(s.MemoryStats.Limit), 2)
			}
		}
		out = append(out, row)
	}
	return out, nil
}

// dockerStatsResponse mirrors the bits of /containers/{id}/stats we actually
// read. The full payload is huge (per-blkio-device entries, network
// per-interface, etc.) — we deliberately deserialize a narrow slice.
type dockerStatsResponse struct {
	CPUStats    cpuStats `json:"cpu_stats"`
	PreCPUStats cpuStats `json:"precpu_stats"`
	MemoryStats struct {
		Usage int64 `json:"usage"`
		Limit int64 `json:"limit"`
		Stats struct {
			Cache        int64 `json:"cache"`         // cgroup v1
			InactiveFile int64 `json:"inactive_file"` // cgroup v2
		} `json:"stats"`
	} `json:"memory_stats"`
}

type cpuStats struct {
	CPUUsage struct {
		TotalUsage  int64 `json:"total_usage"`
		PercpuUsage []int64 `json:"percpu_usage"`
	} `json:"cpu_usage"`
	SystemUsage int64 `json:"system_cpu_usage"`
	OnlineCPUs  int   `json:"online_cpus"`
}

// calcCPUPct mirrors moby's cli/command/container/stats_helpers.go. Result is
// "% of one CPU core" multiplied by online_cpus, so a 2-core box at 100% on
// each core reports 200.0 — same as `docker stats`.
func calcCPUPct(s dockerStatsResponse) float64 {
	cpuDelta := float64(s.CPUStats.CPUUsage.TotalUsage - s.PreCPUStats.CPUUsage.TotalUsage)
	sysDelta := float64(s.CPUStats.SystemUsage - s.PreCPUStats.SystemUsage)
	online := s.CPUStats.OnlineCPUs
	if online == 0 {
		online = len(s.CPUStats.CPUUsage.PercpuUsage)
	}
	if online == 0 {
		online = 1
	}
	if sysDelta <= 0 || cpuDelta <= 0 {
		return 0
	}
	return (cpuDelta / sysDelta) * float64(online) * 100.0
}

func argString(args map[string]any, key, def string) string {
	if v, ok := args[key].(string); ok && v != "" {
		return v
	}
	return def
}
