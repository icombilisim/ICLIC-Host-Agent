package collectors

// DefaultRegistry maps the primitive name an operator writes in YAML to the
// Go function that runs it. New built-in primitives get a single line here.
// Out-of-tree primitives are intentionally not supported — the surface area
// is meant to be auditable and finite; everything operator-extensible is the
// `exec` + binding YAML combination.
func DefaultRegistry() map[string]PrimitiveFunc {
	return map[string]PrimitiveFunc{
		"procfs.loadavg":      procfsLoadavg,
		"procfs.uptime":       procfsUptime,
		"procfs.memory":       procfsMemory,
		"procfs.cpu_count":    procfsCPUCount,
		"os.release":          osRelease,
		"os.hostname":         osHostname,
		"os.kernel":           osKernel,
		"os.arch":             osArch,
		"reboot.required":     rebootRequired,
		"disk.usage":          diskUsage,
		"disk.max_used_pct":   diskMaxUsedPct,
		"exec":                execPrimitive,
		"systemctl.is_active": systemctlIsActive,
		"tcp.connect":         tcpConnect,
		"http.get":            httpGet,
		"file.stat":           fileStat,
		"apt.security_count":  aptSecurityCount,
	}
}
