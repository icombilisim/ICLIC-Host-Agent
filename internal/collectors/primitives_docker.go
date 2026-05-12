package collectors

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
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

// sharedDockerClients caches one *http.Client per socket path for the agent's
// lifetime. The previous implementation built a fresh client+Transport on
// every call, never released them, and on a 19-container host accumulated
// ~60 orphan Transports per heartbeat tick — each carrying idle-conn maps and
// keep-alive goroutines that the GC only reclaims after full sweeps. Nine
// days of uptime translated to ~4 GB resident. (#2)
var (
	sharedDockerClientsMu sync.RWMutex
	sharedDockerClients   = make(map[string]*http.Client)
)

// dockerClientFor returns a process-lifetime client for the given socket. The
// per-request timeout is enforced via the request context, not the client, so
// one shared client can serve requests with different budgets.
func dockerClientFor(socket string) *http.Client {
	sharedDockerClientsMu.RLock()
	c, ok := sharedDockerClients[socket]
	sharedDockerClientsMu.RUnlock()
	if ok {
		return c
	}
	sharedDockerClientsMu.Lock()
	defer sharedDockerClientsMu.Unlock()
	if c, ok := sharedDockerClients[socket]; ok {
		return c
	}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", socket)
		},
		// One socket peer (dockerd), so a small idle pool is enough. Keep
		// IdleConnTimeout above the heartbeat interval so connections survive
		// between ticks — that's the whole point of the shared client.
		MaxIdleConns:          8,
		MaxIdleConnsPerHost:   8,
		IdleConnTimeout:       90 * time.Second,
		ResponseHeaderTimeout: 10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
	c = &http.Client{Transport: transport}
	sharedDockerClients[socket] = c
	return c
}

// drainAndClose ensures the HTTP connection returns to the pool even if the
// caller stopped reading early (e.g. json.Decode hit a syntax error halfway
// through). Without this, a partial read pins the connection and the next
// request has to dial a fresh socket — defeating the shared-client win.
func drainAndClose(body io.ReadCloser) {
	_, _ = io.Copy(io.Discard, io.LimitReader(body, 1<<20))
	_ = body.Close()
}

func dockerGet(ctx context.Context, socket, path string, timeout time.Duration, out any) error {
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodGet, "http://docker"+path, nil)
	if err != nil {
		return err
	}
	resp, err := dockerClientFor(socket).Do(req)
	if err != nil {
		return fmt.Errorf("docker socket %s: %w", socket, err)
	}
	defer drainAndClose(resp.Body)
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

	// Fan out: Docker's /stats?stream=false blocks for one collector tick
	// (~1s) before responding, so serializing N containers blew the budget
	// — the previous code divided `timeout` by N and floored at 500ms, which
	// timed out *every* request and left all fields zeroed out in the
	// payload. Each goroutine now gets the full timeout; dockerd handles
	// dozens of concurrent socket clients without trouble. (#40)
	out := make([]statsRow, len(running))
	var wg sync.WaitGroup
	for i, c := range running {
		i, c := i, c
		wg.Add(1)
		go func() {
			defer wg.Done()
			name := "?"
			if len(c.Names) > 0 {
				name = strings.TrimPrefix(c.Names[0], "/")
			}
			row := statsRow{
				Name: name, Image: c.Image, State: c.State,
				RestartCount: dockerRestartCount(ctx, socket, c.ID, timeout),
			}
			var s dockerStatsResponse
			if err := dockerGet(ctx, socket, "/containers/"+c.ID+"/stats?stream=false", timeout, &s); err == nil {
				row.CPUPct = roundTo(calcCPUPct(s), 2)
				row.MemUsedMB = s.MemoryStats.Usage / (1024 * 1024)
				row.MemLimitMB = s.MemoryStats.Limit / (1024 * 1024)
				if s.MemoryStats.Limit > 0 {
					row.MemPct = roundTo(float64(s.MemoryStats.Usage)*100.0/float64(s.MemoryStats.Limit), 2)
				}
			}
			out[i] = row
		}()
	}
	wg.Wait()
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
