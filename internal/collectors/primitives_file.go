package collectors

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// fileStat reports presence and size of a path. Useful for detecting log
// rotation gaps, missing pid files, growing audit logs, stale artefacts.
//
// Args:
//
//	path:  string  required
//	field: "exists" (default — bool) | "size_bytes" (int) | "mtime_seconds" (int)
//	       | "age_seconds" (int — now - mtime; -1 if missing)
func fileStat(_ context.Context, args map[string]any) (any, error) {
	path, _ := args["path"].(string)
	if path == "" {
		return nil, fmt.Errorf("path: required")
	}
	field, _ := args["field"].(string)
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			switch field {
			case "", "exists":
				return false, nil
			case "size_bytes", "mtime_seconds", "age_seconds":
				return int64(-1), nil
			}
		}
		return nil, err
	}
	switch field {
	case "", "exists":
		return true, nil
	case "size_bytes":
		return info.Size(), nil
	case "mtime_seconds":
		return info.ModTime().Unix(), nil
	case "age_seconds":
		return int64(time.Since(info.ModTime()).Seconds()), nil
	default:
		return nil, fmt.Errorf("unknown field %q", field)
	}
}

// fileNewestAgeSeconds returns seconds since the most recently modified regular
// file matching a glob — i.e. "how old is the latest backup". Returns -1 when
// nothing matches (no backup at all). Built for timestamped dump dirs where the
// filename changes every run; alert on age greater than your backup interval
// (e.g. > 93600 = 26h for a daily dump). (#40 W1)
//
// Args:
//
//	glob: string  required — e.g. /opt/iclic/mysql/backups/*.sql.gz
func fileNewestAgeSeconds(_ context.Context, args map[string]any) (any, error) {
	pattern, _ := args["glob"].(string)
	if pattern == "" {
		return nil, fmt.Errorf("glob: required")
	}
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("glob %q: %w", pattern, err)
	}
	var newest time.Time
	found := false
	for _, m := range matches {
		info, statErr := os.Stat(m)
		if statErr != nil || !info.Mode().IsRegular() {
			continue
		}
		if !found || info.ModTime().After(newest) {
			newest = info.ModTime()
			found = true
		}
	}
	if !found {
		return int64(-1), nil
	}
	age := int64(time.Since(newest).Seconds())
	if age < 0 {
		age = 0 // clock skew guard
	}
	return age, nil
}
