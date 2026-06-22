package collectors

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

// aptSecurityCount counts pending security updates on Debian/Ubuntu by
// shelling out to `apt-get -s upgrade` and tallying the lines that mention
// the security archive.
//
// Returns:
//
//	int >= 0       — actual count
//	int == -1      — agent could not determine (apt locked, missing,
//	                 timed out). Matches the sentinel documented in
//	                 docs/en/protocol.md so the UI can render "unknown".
//
// Never returns an error — operators on RHEL/CentOS hosts see -1, which is
// the right "not applicable" signal until a dnf-flavored primitive exists.
func aptSecurityCount(ctx context.Context, _ map[string]any) (any, error) {
	cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	out, err := exec.CommandContext(cctx, "apt-get", "-s", "upgrade").Output()
	if err != nil {
		// apt-get not installed, locked by another process, or non-zero exit
		return int(-1), nil
	}
	count := 0
	for _, line := range strings.Split(string(out), "\n") {
		// "Inst foo [1.0-1] (1.0-2 Ubuntu:24.04/noble-security [amd64])"
		// We count any line marked "Inst" that mentions the security archive.
		if !strings.HasPrefix(line, "Inst ") {
			continue
		}
		if strings.Contains(line, "-security") || strings.Contains(line, "Security") {
			count++
		}
	}
	return count, nil
}
