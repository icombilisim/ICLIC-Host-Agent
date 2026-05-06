package collectors

import (
	"context"
	"fmt"
	"os"
)

// fileStat reports presence and size of a path. Useful for detecting log
// rotation gaps, missing pid files, growing audit logs.
//
// Args:
//
//	path:  string  required
//	field: "exists" (default — bool) | "size_bytes" (int) | "mtime_seconds" (int)
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
			case "size_bytes":
				return int64(-1), nil
			case "mtime_seconds":
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
	default:
		return nil, fmt.Errorf("unknown field %q", field)
	}
}
