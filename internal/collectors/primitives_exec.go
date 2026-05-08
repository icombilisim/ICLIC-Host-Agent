package collectors

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// execPrimitive runs an arbitrary command and returns its stdout, optionally
// parsed as a number. Designed for operator-defined probes — WildFly CLI,
// jboss-cli, custom shell scripts, etc. The whole point of the pluggable
// collector model is that legacy / non-Docker stacks can be observed without
// forking the agent.
//
// Args:
//
//	cmd:         []string         required — argv-style, no shell expansion
//	timeout_sec: number           optional, default 5, capped at 30
//	parse:       "raw" (default) | "trimmed" | "int" | "float" | "json"
//
// Stdout-only by design — stderr is dropped to keep the metric value clean.
// A non-zero exit is treated as an error and the metric is omitted from the
// heartbeat.
func execPrimitive(ctx context.Context, args map[string]any) (any, error) {
	cmd, err := argStringSlice(args, "cmd")
	if err != nil {
		return nil, err
	}
	if len(cmd) == 0 {
		return nil, fmt.Errorf("cmd: at least one argument required")
	}

	timeoutSec := argFloat(args, "timeout_sec", 5)
	if timeoutSec > 30 {
		timeoutSec = 30
	}
	if timeoutSec < 1 {
		timeoutSec = 1
	}
	cctx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec*float64(time.Second)))
	defer cancel()

	out, err := exec.CommandContext(cctx, cmd[0], cmd[1:]...).Output()
	if err != nil {
		return nil, fmt.Errorf("%s: %w", cmd[0], err)
	}

	parse, _ := args["parse"].(string)
	s := string(out)
	switch parse {
	case "", "raw":
		return s, nil
	case "trimmed":
		return strings.TrimSpace(s), nil
	case "int":
		n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("parse int: %w", err)
		}
		return n, nil
	case "float":
		f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
		if err != nil {
			return nil, fmt.Errorf("parse float: %w", err)
		}
		return f, nil
	case "json":
		var value any
		if err := json.Unmarshal([]byte(s), &value); err != nil {
			return nil, fmt.Errorf("parse json: %w", err)
		}
		return value, nil
	default:
		return nil, fmt.Errorf("unknown parse %q (want raw | trimmed | int | float | json)", parse)
	}
}

// systemctlIsActive returns true when the named unit is active, false when
// inactive/failed/unknown. Doesn't error on inactive — that's a fact about
// the unit, not a probe failure.
//
// Args:
//
//	unit: string  required — e.g. "wildfly.service" or "nginx"
func systemctlIsActive(ctx context.Context, args map[string]any) (any, error) {
	unit, _ := args["unit"].(string)
	if unit == "" {
		return nil, fmt.Errorf("unit: required")
	}
	cctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	out, _ := exec.CommandContext(cctx, "systemctl", "is-active", unit).Output()
	return strings.TrimSpace(string(out)) == "active", nil
}

func argStringSlice(args map[string]any, key string) ([]string, error) {
	raw, ok := args[key]
	if !ok {
		return nil, fmt.Errorf("%s: required", key)
	}
	switch v := raw.(type) {
	case []string:
		return v, nil
	case []any:
		out := make([]string, len(v))
		for i, x := range v {
			s, ok := x.(string)
			if !ok {
				return nil, fmt.Errorf("%s[%d]: must be string, got %T", key, i, x)
			}
			out[i] = s
		}
		return out, nil
	default:
		return nil, fmt.Errorf("%s: must be string list, got %T", key, raw)
	}
}

func argFloat(args map[string]any, key string, def float64) float64 {
	v, ok := args[key]
	if !ok {
		return def
	}
	switch x := v.(type) {
	case float64:
		return x
	case int:
		return float64(x)
	case int64:
		return float64(x)
	}
	return def
}
