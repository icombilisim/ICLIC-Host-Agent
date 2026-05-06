package collectors

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"runtime"
	"strings"
)

// osRelease pulls one field out of /etc/os-release.
//
// Args:
//
//	field: "id" | "name" | "version" (default) | "version_id" | "pretty_name"
//
// The file format is shell-style "KEY=VALUE" with optional double-quotes;
// we strip them so the value is the raw human string.
func osRelease(_ context.Context, args map[string]any) (any, error) {
	fields, err := readOsRelease("/etc/os-release")
	if err != nil {
		return nil, err
	}
	field, _ := args["field"].(string)
	switch field {
	case "id":
		return fields["ID"], nil
	case "name":
		return fields["NAME"], nil
	case "", "version":
		return fields["VERSION"], nil
	case "version_id":
		return fields["VERSION_ID"], nil
	case "pretty_name":
		return fields["PRETTY_NAME"], nil
	default:
		return nil, fmt.Errorf("unknown field %q", field)
	}
}

// osHostname returns the kernel-reported hostname.
func osHostname(_ context.Context, _ map[string]any) (any, error) {
	return os.Hostname()
}

// osKernel returns the running kernel release string (e.g. "6.8.0-31-generic").
// /proc/sys/kernel/osrelease avoids needing the syscall.Uname stub on non-Linux
// build hosts — the agent is Linux-only at runtime, but cross-compile from
// developer laptops stays trivial. (#35)
func osKernel(_ context.Context, _ map[string]any) (any, error) {
	data, err := os.ReadFile("/proc/sys/kernel/osrelease")
	if err != nil {
		return nil, err
	}
	return strings.TrimSpace(string(data)), nil
}

// osArch returns the build target arch — same value across every host running
// a given binary, but exposed here so the heartbeat carries it without forcing
// a separate envelope field.
func osArch(_ context.Context, _ map[string]any) (any, error) {
	return runtime.GOARCH, nil
}

// rebootRequired reports whether the system needs a reboot. Debian/Ubuntu
// drops a flag file when the kernel or libc has been upgraded; the file's
// presence is the signal — its content is not load-bearing.
func rebootRequired(_ context.Context, _ map[string]any) (any, error) {
	if _, err := os.Stat("/var/run/reboot-required"); err == nil {
		return true, nil
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	return false, nil
}

func readOsRelease(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	out := make(map[string]string, 8)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		eq := strings.IndexByte(line, '=')
		if eq < 0 {
			continue
		}
		k := line[:eq]
		v := strings.Trim(line[eq+1:], `"`)
		out[k] = v
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
