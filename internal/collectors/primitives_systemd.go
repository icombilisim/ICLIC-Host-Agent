package collectors

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// systemdResources reads detailed resource metrics for one or more units via
// `systemctl show -p Prop1,Prop2,... unit1 unit2 …`. The existing
// systemctl.is_active primitive only answers a yes/no liveness question; this
// primitive complements it with the data the new ServerDetail "Services"
// section needs (memory in MB, CPU usage delta as %, restart count, sub-state
// for the "running / dead / failed" badge).
//
// Args:
//
//	units:       []string  required — full unit names (e.g. "icosys-icglb.service")
//	timeout_sec: number    optional, default 4
//
// Returns: list of {
//
//	  unit, active_state, sub_state, main_pid,
//	  memory_mb, cpu_ns, n_restarts, load_state
//	}
//
// CPU% is intentionally NOT computed here — we ship the cumulative
// CPUUsageNSec counter and let the backend derive a rate between heartbeats.
// That keeps the per-tick cost flat (no sleeping for a delta sample on the
// agent) and makes the rate comparable across heartbeats.
func systemdResources(ctx context.Context, args map[string]any) (any, error) {
	units, err := argStringSlice(args, "units")
	if err != nil {
		return nil, err
	}
	if len(units) == 0 {
		return []map[string]any{}, nil
	}
	timeout := time.Duration(argFloat(args, "timeout_sec", 4) * float64(time.Second))
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	props := []string{
		"Id", "ActiveState", "SubState", "LoadState", "MainPID",
		"MemoryCurrent", "CPUUsageNSec", "NRestarts",
	}
	cmdArgs := []string{"show", "-p", strings.Join(props, ","), "--value", "--"}
	cmdArgs = append(cmdArgs, units...)

	// `systemctl show --value` prints values one per line in the order of -p,
	// then a blank line, then the next unit. Run per-unit to avoid having to
	// guess where one unit's block ends and the next begins on systems that
	// strip blank lines (some Ubuntu builds drop the trailing newline).
	out := make([]map[string]any, 0, len(units))
	for _, u := range units {
		row, err := systemdShowOne(cctx, u, props)
		if err != nil {
			// Don't abort the whole binding for one missing unit — surface
			// a row with load_state=not-found so the UI can render a slot.
			out = append(out, map[string]any{
				"unit":         u,
				"active_state": "unknown",
				"sub_state":    "unknown",
				"load_state":   "not-found",
			})
			continue
		}
		out = append(out, row)
	}
	return out, nil
}

func systemdShowOne(ctx context.Context, unit string, props []string) (map[string]any, error) {
	// NOTE: we deliberately do NOT pass `--value` here. With --value, systemctl
	// emits one bare value per line in DBus property order (numerics first,
	// strings after) — NOT in the order we passed via -p. Parsing by index
	// silently shuffled fields (MainPID landed under Id, MemoryCurrent under
	// SubState, etc.). The `Key=value` form is order-agnostic and forward-
	// compatible if systemd ever adds new lines. (#40)
	cmd := exec.CommandContext(ctx, "systemctl", "show", "-p", strings.Join(props, ","), "--", unit)
	raw, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("systemctl show %s: %w", unit, err)
	}

	values := make(map[string]string, len(props))
	for _, line := range strings.Split(strings.TrimRight(string(raw), "\n"), "\n") {
		eq := strings.IndexByte(line, '=')
		if eq <= 0 {
			continue
		}
		values[line[:eq]] = strings.TrimSpace(line[eq+1:])
	}

	memBytes := parseInt64(values["MemoryCurrent"])
	cpuNs := parseInt64(values["CPUUsageNSec"])
	mainPID := parseInt64(values["MainPID"])
	nRestarts := parseInt64(values["NRestarts"])

	return map[string]any{
		"unit":         unit,
		"id":           values["Id"],
		"active_state": values["ActiveState"],
		"sub_state":    values["SubState"],
		"load_state":   values["LoadState"],
		"main_pid":     mainPID,
		"memory_mb":    memBytes / (1024 * 1024),
		"cpu_ns":       cpuNs,
		"n_restarts":   nRestarts,
	}, nil
}

// parseInt64 returns 0 for systemd's literal "[not set]" sentinel and
// "infinity" sentinel — both surface as garbage in the UI otherwise.
func parseInt64(s string) int64 {
	s = strings.TrimSpace(s)
	if s == "" || s == "[not set]" || s == "infinity" {
		return 0
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0
	}
	return n
}
