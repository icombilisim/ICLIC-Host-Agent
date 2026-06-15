package collectors

import "testing"

func TestDiskRatesExcludesPartitionsAndVirtual(t *testing.T) {
	// sda is a whole disk; sda1 is its partition; loop0 is virtual. Only sda's
	// delta must count, proving partition + virtual exclusion and the rate math.
	prev := parseDiskstats(
		"   8 0 sda 0 0 0 0 0 0 0 0 0\n" +
			"   8 1 sda1 0 0 0 0 0 0 0 0 0\n" +
			"   7 0 loop0 0 0 0 0 0 0 0 0 0\n")
	cur := parseDiskstats(
		"   8 0 sda 0 0 2000 0 0 0 0 0 0\n" + // +2000 sectors read = 1,024,000 B
			"   8 1 sda1 0 0 9000000 0 0 0 0 0 0\n" + // would dwarf sda if counted
			"   7 0 loop0 0 0 9000000 0 0 0 0 0 0\n")

	got := diskRates(prev, cur, 1.0)
	if rd := got["read_mbps"].(float64); rd != 1.02 { // round(1024000/1e6,2)
		t.Fatalf("read_mbps: want 1.02 (sda only), got %v", rd)
	}
	if wr := got["write_mbps"].(float64); wr != 0 {
		t.Fatalf("write_mbps: want 0, got %v", wr)
	}
}

func TestNetRatesExcludesLoAndVirtual(t *testing.T) {
	prev := parseNetdev(
		"  eth0: 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0\n" +
			"    lo: 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0\n" +
			"docker0: 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0\n")
	cur := parseNetdev(
		"  eth0: 3000000 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0\n" + // +3,000,000 B rx = 3.0 MB/s
			"    lo: 9000000 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0\n" +
			"docker0: 9000000 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0\n")

	got := netRates(prev, cur, 1.0, "")
	if rx := got["rx_mbps"].(float64); rx != 3.0 {
		t.Fatalf("rx_mbps: want 3.0 (eth0 only), got %v", rx)
	}
}

func TestNetRatesSingleIface(t *testing.T) {
	prev := parseNetdev("  eth0: 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0\n  eth1: 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0\n")
	cur := parseNetdev("  eth0: 2000000 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0\n  eth1: 5000000 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0\n")
	got := netRates(prev, cur, 1.0, "eth1")
	if rx := got["rx_mbps"].(float64); rx != 5.0 {
		t.Fatalf("rx_mbps for eth1: want 5.0, got %v", rx)
	}
}
