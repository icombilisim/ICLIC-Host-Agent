package collectors

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// diskUsage returns disk usage for one or all mounts. Implementation shells
// out to `df -kP` so the same code compiles unchanged on developer laptops
// (Linux/macOS) and the runtime target — the agent is Linux-only in practice
// but the build doesn't need to be.
//
// Args:
//
//	mount:  string  (optional — when set, return one map for that mount)
//	exclude_pseudo: bool (default true — drop tmpfs / squashfs / overlay)
//
// Returns:
//
//	when mount is set:    {"mount": "/", "used_pct": 42.0, "total_gb": 100}
//	when mount is unset:  [{...}, {...}]  one per non-pseudo mount
func diskUsage(ctx context.Context, args map[string]any) (any, error) {
	rows, err := dfRows(ctx, argBool(args, "exclude_pseudo", true))
	if err != nil {
		return nil, err
	}
	if mount, ok := args["mount"].(string); ok && mount != "" {
		for _, r := range rows {
			if r["mount"] == mount {
				return r, nil
			}
		}
		return nil, fmt.Errorf("mount %q not found in df output", mount)
	}
	out := make([]map[string]any, len(rows))
	for i, r := range rows {
		out[i] = r
	}
	return out, nil
}

// diskMaxUsedPct returns the highest used_pct across all real mounts. The
// backend `buildSummary` reads this directly to render "disk=72%" next to
// the timestamp on the Servers list. Splitting it from disk.usage means
// 00-linux-host.yaml can produce both shapes without a second df shell-out
// in the same binding (each binding is independent — we accept the small
// duplication for the simpler model).
func diskMaxUsedPct(ctx context.Context, args map[string]any) (any, error) {
	rows, err := dfRows(ctx, argBool(args, "exclude_pseudo", true))
	if err != nil {
		return nil, err
	}
	var max float64
	for _, r := range rows {
		if pct, ok := r["used_pct"].(float64); ok && pct > max {
			max = pct
		}
	}
	return roundTo(max, 1), nil
}

// dfRows parses `df -kP` (POSIX) into a list of {mount, used_pct, total_gb}.
// Pseudo filesystems are dropped by default — they distort the "max" metric
// (e.g. /run/user/* is always near 0%) and add noise to the per-mount list.
func dfRows(ctx context.Context, excludePseudo bool) ([]map[string]any, error) {
	cmd := exec.CommandContext(ctx, "df", "-kP")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("df -kP: %w", err)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) < 2 {
		return nil, fmt.Errorf("df produced no rows")
	}
	rows := make([]map[string]any, 0, len(lines)-1)
	for _, line := range lines[1:] {
		fields := strings.Fields(line)
		if len(fields) < 6 {
			continue
		}
		fs := fields[0]
		if excludePseudo && isPseudoFS(fs) {
			continue
		}
		// Filesystem 1024-blocks Used Available Capacity Mounted-on
		totalKB, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			continue
		}
		usedKB, err := strconv.ParseInt(fields[2], 10, 64)
		if err != nil {
			continue
		}
		mount := fields[5]
		var pct float64
		if totalKB > 0 {
			pct = roundTo(float64(usedKB)*100.0/float64(totalKB), 1)
		}
		rows = append(rows, map[string]any{
			"mount":    mount,
			"used_pct": pct,
			"total_gb": totalKB / (1024 * 1024),
		})
	}
	return rows, nil
}

func isPseudoFS(device string) bool {
	switch {
	case strings.HasPrefix(device, "tmpfs"),
		strings.HasPrefix(device, "devtmpfs"),
		strings.HasPrefix(device, "overlay"),
		strings.HasPrefix(device, "squashfs"),
		strings.HasPrefix(device, "udev"),
		strings.HasPrefix(device, "proc"),
		strings.HasPrefix(device, "sysfs"),
		strings.HasPrefix(device, "cgroup"),
		strings.HasPrefix(device, "efivarfs"),
		strings.HasPrefix(device, "debugfs"),
		strings.HasPrefix(device, "tracefs"),
		strings.HasPrefix(device, "securityfs"),
		strings.HasPrefix(device, "pstore"),
		strings.HasPrefix(device, "bpf"),
		strings.HasPrefix(device, "mqueue"),
		strings.HasPrefix(device, "ramfs"),
		device == "none":
		return true
	}
	return false
}

func argBool(args map[string]any, key string, def bool) bool {
	v, ok := args[key]
	if !ok {
		return def
	}
	if b, ok := v.(bool); ok {
		return b
	}
	return def
}
