package collectors

import (
	"bufio"
	"context"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
)

// procfsLoadavg reads /proc/loadavg and returns the requested load window.
//
// Args:
//
//	window: "1m" | "5m" | "15m"   (default "1m")
func procfsLoadavg(_ context.Context, args map[string]any) (any, error) {
	data, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return nil, err
	}
	parts := strings.Fields(string(data))
	if len(parts) < 3 {
		return nil, fmt.Errorf("malformed /proc/loadavg: %q", string(data))
	}
	window, _ := args["window"].(string)
	idx := 0
	switch window {
	case "", "1m":
		idx = 0
	case "5m":
		idx = 1
	case "15m":
		idx = 2
	default:
		return nil, fmt.Errorf("unknown window %q (want 1m | 5m | 15m)", window)
	}
	return strconv.ParseFloat(parts[idx], 64)
}

// procfsUptime returns the kernel's uptime in whole seconds (rounded down).
// /proc/uptime ships two floats: uptime + idle. We only need the first.
func procfsUptime(_ context.Context, _ map[string]any) (any, error) {
	data, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return nil, err
	}
	parts := strings.Fields(string(data))
	if len(parts) == 0 {
		return nil, fmt.Errorf("malformed /proc/uptime: %q", string(data))
	}
	f, err := strconv.ParseFloat(parts[0], 64)
	if err != nil {
		return nil, err
	}
	return int64(f), nil
}

// procfsMemory reads /proc/meminfo and returns one summary field.
//
// Args:
//
//	field: "used_pct" (default) | "total_mb" | "used_mb" | "free_mb" | "available_mb"
//
// "used" follows the modern Linux convention: Total - Available (not the
// Total - Free that older `free` command output reported), so cache pages
// don't inflate the apparent usage.
func procfsMemory(_ context.Context, args map[string]any) (any, error) {
	fields, err := readMeminfoKB()
	if err != nil {
		return nil, err
	}
	field, _ := args["field"].(string)
	total := fields["MemTotal"]
	avail := fields["MemAvailable"]
	used := total - avail

	switch field {
	case "", "used_pct":
		if total == 0 {
			return 0.0, nil
		}
		return roundTo(float64(used)*100.0/float64(total), 1), nil
	case "total_mb":
		return total / 1024, nil
	case "used_mb":
		return used / 1024, nil
	case "free_mb":
		return fields["MemFree"] / 1024, nil
	case "available_mb":
		return avail / 1024, nil
	default:
		return nil, fmt.Errorf("unknown field %q", field)
	}
}

// procfsCPUCount returns the number of online logical CPUs as reported by
// /proc/cpuinfo. Useful for normalizing load against capacity client-side.
func procfsCPUCount(_ context.Context, _ map[string]any) (any, error) {
	f, err := os.Open("/proc/cpuinfo")
	if err != nil {
		return nil, err
	}
	defer f.Close()
	count := 0
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		if strings.HasPrefix(scanner.Text(), "processor") {
			count++
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return count, nil
}

func readMeminfoKB() (map[string]int64, error) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return nil, err
	}
	defer f.Close()
	out := make(map[string]int64, 8)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		colon := strings.IndexByte(line, ':')
		if colon < 0 {
			continue
		}
		name := line[:colon]
		rest := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(line[colon+1:]), " kB"))
		n, err := strconv.ParseInt(rest, 10, 64)
		if err != nil {
			continue
		}
		out[name] = n
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func roundTo(f float64, digits int) float64 {
	mult := math.Pow(10, float64(digits))
	return math.Round(f*mult) / mult
}
