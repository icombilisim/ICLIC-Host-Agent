// Package collectors implements the agent's pluggable metric pipeline.
//
// Configuration lives in /etc/iclic-host-agent/collectors.d/*.yaml — one or
// more files, alphabetically merged, hot-read on every tick. Each "binding"
// names a built-in primitive (procfs, exec, systemctl, …), supplies its args,
// and declares the output key the result lands at in the heartbeat payload's
// `metrics` map. The set of primitives is fixed in Go; the wiring is operator-
// editable so a host running WildFly / Nginx / TL Easy / arbitrary legacy
// software can be observed without forking the agent. (#35)
package collectors

// Binding is one entry in a collectors.d/*.yaml file. The operator writes the
// YAML; the agent treats it as data only — Primitive must resolve to a name
// in the primitive registry, OutputKey is the key under metrics{} where the
// result is placed, and Args is forwarded verbatim to the primitive.
type Binding struct {
	ID        string         `yaml:"id"`
	Primitive string         `yaml:"primitive"`
	Args      map[string]any `yaml:"args"`
	OutputKey string         `yaml:"output_key"`
}
