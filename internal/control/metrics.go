package control

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// liveMetrics is one sample streamed by the metrics.live verb. The Fleet UI
// plots these as a rolling real-time chart. (#379)
type liveMetrics struct {
	Ts    int64   `json:"ts"`    // unix millis (agent clock)
	CPU   float64 `json:"cpu"`   // busy CPU %, 0..100 across all cores
	Mem   float64 `json:"mem"`   // used memory %, Total-Available convention
	Load1 float64 `json:"load1"` // 1-minute load average
}

// cpuSample is a /proc/stat aggregate-CPU reading; CPU% is the busy delta
// between two samples over the same window.
type cpuSample struct {
	total uint64
	idle  uint64
}

// readCPUSample reads the aggregate "cpu" line of /proc/stat. idle counts
// idle+iowait; total is the sum of every column. (#379)
func readCPUSample() (cpuSample, error) {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return cpuSample{}, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "cpu ") {
			continue
		}
		fields := strings.Fields(line)[1:] // drop the "cpu" label
		var s cpuSample
		for i, f := range fields {
			v, perr := strconv.ParseUint(f, 10, 64)
			if perr != nil {
				continue
			}
			s.total += v
			if i == 3 || i == 4 { // idle, iowait
				s.idle += v
			}
		}
		return s, nil
	}
	return cpuSample{}, fmt.Errorf("no aggregate cpu line in /proc/stat")
}

// cpuPct is the busy percentage between two samples. Returns 0 when the window
// has no elapsed jiffies (back-to-back reads).
func cpuPct(prev, cur cpuSample) float64 {
	totalDelta := float64(cur.total - prev.total)
	idleDelta := float64(cur.idle - prev.idle)
	if totalDelta <= 0 {
		return 0
	}
	pct := (totalDelta - idleDelta) * 100.0 / totalDelta
	if pct < 0 {
		return 0
	}
	if pct > 100 {
		return 100
	}
	return round1(pct)
}

// readMemPct returns used memory % using the Total-Available convention so
// cache pages don't inflate usage. (#379)
func readMemPct() float64 {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0
	}
	var total, avail int64
	for _, line := range strings.Split(string(data), "\n") {
		colon := strings.IndexByte(line, ':')
		if colon < 0 {
			continue
		}
		key := line[:colon]
		val := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(line[colon+1:]), " kB"))
		n, perr := strconv.ParseInt(val, 10, 64)
		if perr != nil {
			continue
		}
		switch key {
		case "MemTotal":
			total = n
		case "MemAvailable":
			avail = n
		}
	}
	if total <= 0 {
		return 0
	}
	return round1(float64(total-avail) * 100.0 / float64(total))
}

// readLoad1 returns the 1-minute load average from /proc/loadavg.
func readLoad1() float64 {
	data, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return 0
	}
	parts := strings.Fields(string(data))
	if len(parts) == 0 {
		return 0
	}
	f, err := strconv.ParseFloat(parts[0], 64)
	if err != nil {
		return 0
	}
	return f
}

func round1(f float64) float64 {
	return float64(int64(f*10+0.5)) / 10
}
