package collectors

import (
	"context"
	"os"
	"strconv"
	"strings"
	"time"
)

// Disk I/O and network throughput are rates, so both primitives sample the
// relevant /proc file twice (interval apart) and report the per-second delta.
// The parse + rate functions are pure so they're unit-testable without /proc or
// a real sleep. Linux-only (/proc); on other OSes the read fails and the metric
// is simply omitted — Windows is covered later via gopsutil (Faz 4b). (#40 W1)

const (
	sectorBytes      = 512
	defaultSampleSec = 1.0
	maxSampleSec     = 5.0
	minSampleSec     = 0.2
)

// --- disk I/O ------------------------------------------------------------

type diskCounters struct {
	sectorsRead    int64
	sectorsWritten int64
	ios            int64 // reads_completed + writes_completed
}

// parseDiskstats parses /proc/diskstats into per-device counters.
func parseDiskstats(data string) map[string]diskCounters {
	out := make(map[string]diskCounters)
	for _, line := range strings.Split(data, "\n") {
		f := strings.Fields(line)
		if len(f) < 10 {
			continue
		}
		dev := f[2]
		readsCompleted, _ := strconv.ParseInt(f[3], 10, 64)
		sectorsRead, _ := strconv.ParseInt(f[5], 10, 64)
		writesCompleted, _ := strconv.ParseInt(f[7], 10, 64)
		sectorsWritten, _ := strconv.ParseInt(f[9], 10, 64)
		out[dev] = diskCounters{
			sectorsRead:    sectorsRead,
			sectorsWritten: sectorsWritten,
			ios:            readsCompleted + writesCompleted,
		}
	}
	return out
}

// isVirtualDisk skips pseudo devices that aren't real backing storage.
func isVirtualDisk(dev string) bool {
	for _, p := range []string{"loop", "ram", "sr", "dm-", "fd", "md", "zram", "dm"} {
		if strings.HasPrefix(dev, p) {
			return true
		}
	}
	return false
}

// isPartitionOf reports whether dev is a partition of some other whole disk in
// the set (e.g. sda1 of sda, nvme0n1p1 of nvme0n1) — so we don't double-count.
func isPartitionOf(dev string, all map[string]diskCounters) bool {
	for name := range all {
		if name != dev && strings.HasPrefix(dev, name) {
			return true
		}
	}
	return false
}

// diskRates aggregates whole-disk deltas into read/write MB/s and IOPS.
func diskRates(prev, cur map[string]diskCounters, interval float64) map[string]any {
	var dSectorsR, dSectorsW, dIOs int64
	for dev, c := range cur {
		if isVirtualDisk(dev) || isPartitionOf(dev, cur) {
			continue
		}
		p, ok := prev[dev]
		if !ok {
			continue
		}
		if d := c.sectorsRead - p.sectorsRead; d > 0 {
			dSectorsR += d
		}
		if d := c.sectorsWritten - p.sectorsWritten; d > 0 {
			dSectorsW += d
		}
		if d := c.ios - p.ios; d > 0 {
			dIOs += d
		}
	}
	if interval <= 0 {
		interval = 1
	}
	return map[string]any{
		"read_mbps":  roundTo(float64(dSectorsR*sectorBytes)/interval/1e6, 2),
		"write_mbps": roundTo(float64(dSectorsW*sectorBytes)/interval/1e6, 2),
		"iops":       int64(roundTo(float64(dIOs)/interval, 0)),
	}
}

// procfsDiskstats samples /proc/diskstats twice and returns aggregate disk I/O
// rates { read_mbps, write_mbps, iops } across real (whole) disks.
//
// Args:
//
//	sample_sec: number  optional, default 1 (0.2..5)
func procfsDiskstats(ctx context.Context, args map[string]any) (any, error) {
	prev, interval, err := sampleProc(ctx, "/proc/diskstats", args)
	if err != nil {
		return nil, err
	}
	cur, err := os.ReadFile("/proc/diskstats")
	if err != nil {
		return nil, err
	}
	return diskRates(parseDiskstats(prev), parseDiskstats(string(cur)), interval), nil
}

// --- network -------------------------------------------------------------

type netCounters struct {
	rxBytes, txBytes int64
	rxErrs, txErrs   int64
}

// parseNetdev parses /proc/net/dev into per-interface counters.
func parseNetdev(data string) map[string]netCounters {
	out := make(map[string]netCounters)
	for _, line := range strings.Split(data, "\n") {
		colon := strings.IndexByte(line, ':')
		if colon < 0 {
			continue // the two header lines have no ':' in the iface position
		}
		iface := strings.TrimSpace(line[:colon])
		f := strings.Fields(line[colon+1:])
		if iface == "" || len(f) < 16 {
			continue
		}
		rxBytes, _ := strconv.ParseInt(f[0], 10, 64)
		rxErrs, _ := strconv.ParseInt(f[2], 10, 64)
		txBytes, _ := strconv.ParseInt(f[8], 10, 64)
		txErrs, _ := strconv.ParseInt(f[10], 10, 64)
		out[iface] = netCounters{rxBytes: rxBytes, txBytes: txBytes, rxErrs: rxErrs, txErrs: txErrs}
	}
	return out
}

// isVirtualIface skips loopback and container/bridge plumbing so the aggregate
// reflects real host traffic.
func isVirtualIface(iface string) bool {
	if iface == "lo" {
		return true
	}
	for _, p := range []string{"veth", "docker", "br-", "virbr", "cni", "flannel", "cali", "tap", "tun"} {
		if strings.HasPrefix(iface, p) {
			return true
		}
	}
	return false
}

// netRates aggregates interface deltas into rx/tx MB/s and new error counts.
// If onlyIface is non-empty, only that interface is considered.
func netRates(prev, cur map[string]netCounters, interval float64, onlyIface string) map[string]any {
	var dRx, dTx, dRxErr, dTxErr int64
	for iface, c := range cur {
		if onlyIface != "" {
			if iface != onlyIface {
				continue
			}
		} else if isVirtualIface(iface) {
			continue
		}
		p, ok := prev[iface]
		if !ok {
			continue
		}
		if d := c.rxBytes - p.rxBytes; d > 0 {
			dRx += d
		}
		if d := c.txBytes - p.txBytes; d > 0 {
			dTx += d
		}
		if d := c.rxErrs - p.rxErrs; d > 0 {
			dRxErr += d
		}
		if d := c.txErrs - p.txErrs; d > 0 {
			dTxErr += d
		}
	}
	if interval <= 0 {
		interval = 1
	}
	return map[string]any{
		"rx_mbps":   roundTo(float64(dRx)/interval/1e6, 2),
		"tx_mbps":   roundTo(float64(dTx)/interval/1e6, 2),
		"rx_errors": dRxErr,
		"tx_errors": dTxErr,
	}
}

// procfsNetdev samples /proc/net/dev twice and returns aggregate network rates
// { rx_mbps, tx_mbps, rx_errors, tx_errors } across real interfaces.
//
// Args:
//
//	sample_sec: number  optional, default 1 (0.2..5)
//	iface:      string  optional — restrict to one interface (else all non-virtual)
func procfsNetdev(ctx context.Context, args map[string]any) (any, error) {
	prev, interval, err := sampleProc(ctx, "/proc/net/dev", args)
	if err != nil {
		return nil, err
	}
	cur, err := os.ReadFile("/proc/net/dev")
	if err != nil {
		return nil, err
	}
	onlyIface, _ := args["iface"].(string)
	return netRates(parseNetdev(prev), parseNetdev(string(cur)), interval, onlyIface), nil
}

// --- shared sampling -----------------------------------------------------

// sampleProc reads a /proc file, waits the (bounded) sample interval honouring
// ctx, and returns the first snapshot plus the actual interval used.
func sampleProc(ctx context.Context, path string, args map[string]any) (string, float64, error) {
	first, err := os.ReadFile(path)
	if err != nil {
		return "", 0, err
	}
	interval := argFloat(args, "sample_sec", defaultSampleSec)
	if interval > maxSampleSec {
		interval = maxSampleSec
	}
	if interval < minSampleSec {
		interval = minSampleSec
	}
	select {
	case <-ctx.Done():
		return "", 0, ctx.Err()
	case <-time.After(time.Duration(interval * float64(time.Second))):
	}
	return string(first), interval, nil
}
