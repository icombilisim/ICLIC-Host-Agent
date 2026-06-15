package collectors

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Service definitions (Faz 4d, #342) are a higher-level, runtime-agnostic way to
// monitor an app: one `service:` block describes it by composable axes (up /
// health / version / metrics), each axis backed by an existing primitive. The
// agent expands a service into ordinary Bindings — so it reuses the same engine,
// registry, and heartbeat path. Open-ended: tcp/http/systemd/docker/exec cover
// any runtime (Tomcat, WildFly, a bare binary…), no per-app-server knowledge.
// Logs are handled by the control channel, not here. See
// .claude/docs/agent-service-definitions.md.

// serviceFile is one services.d/*.yaml — a single top-level `service:` block.
type serviceFile struct {
	Service serviceDef `yaml:"service"`
}

type serviceDef struct {
	Name    string          `yaml:"name"`
	Label   string          `yaml:"label"`
	Up      axis            `yaml:"up"`
	Health  axis            `yaml:"health"`
	Version axis            `yaml:"version"`
	Metrics []serviceMetric `yaml:"metrics"`
	Logs    map[string]any  `yaml:"logs"` // consumed by the control channel (4d-2)
}

// axis is one composable probe: exactly one of tcp/http/systemd/docker/exec,
// plus optional modifiers (e.g. http `path` for a JSON field). Kept as a raw map
// so the expander reads whichever probe key is present.
type axis map[string]any

type serviceMetric struct {
	Key   string   `yaml:"key"`
	Exec  []string `yaml:"exec"`
	Parse string   `yaml:"parse"`
}

// LoadServiceDir reads services.d/*.yaml and expands every service into Bindings.
// A missing dir yields no bindings and no error. Files without a `service.name`
// are skipped (not an error); a YAML parse failure is reported.
func LoadServiceDir(dir string) ([]Binding, error) {
	defs, err := loadServiceDefs(dir)
	if err != nil {
		return nil, err
	}
	var all []Binding
	for _, s := range defs {
		bs, err := expandService(s)
		if err != nil {
			return nil, fmt.Errorf("service %s: %w", s.Name, err)
		}
		all = append(all, bs...)
	}
	return all, nil
}

// loadServiceDefs reads + parses every service definition in dir. Missing dir =
// no defs, no error; files without `service.name` are skipped; parse errors are
// reported. Shared by the Plane-A expander and the Plane-B log-source extractor.
func loadServiceDefs(dir string) ([]serviceDef, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })

	var defs []serviceDef
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || (!strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml")) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		var sf serviceFile
		if err := yaml.Unmarshal(data, &sf); err != nil {
			return nil, fmt.Errorf("%s: yaml parse: %w", name, err)
		}
		if sf.Service.Name == "" {
			continue // not a service file
		}
		defs = append(defs, sf.Service)
	}
	return defs, nil
}

// ServiceLogSource is a control-channel log source derived from a service's
// `logs:` block — so defining a service also makes its logs tailable (Plane B)
// without re-declaring them in control.yaml. (#342 4d-2)
type ServiceLogSource struct {
	Name      string
	Type      string
	Container string
	Path      string
	Unit      string
}

// LoadServiceLogSources returns one entry per service that declares a `logs:`
// block. The control channel merges these into its allow-list — but only when
// the operator has enabled the channel + logs (the opt-in still governs).
func LoadServiceLogSources(dir string) ([]ServiceLogSource, error) {
	defs, err := loadServiceDefs(dir)
	if err != nil {
		return nil, err
	}
	var out []ServiceLogSource
	for _, s := range defs {
		if len(s.Logs) == 0 {
			continue
		}
		out = append(out, ServiceLogSource{
			Name:      s.Name,
			Type:      logsStr(s.Logs, "type"),
			Container: logsStr(s.Logs, "container"),
			Path:      logsStr(s.Logs, "path"),
			Unit:      logsStr(s.Logs, "unit"),
		})
	}
	return out, nil
}

func logsStr(m map[string]any, key string) string {
	s, _ := m[key].(string)
	return s
}

// expandService turns one service into Bindings: up/health/version axes plus any
// custom metrics, all keyed <name>_<axis> / <name>_<metric>.
func expandService(s serviceDef) ([]Binding, error) {
	if s.Name == "" {
		return nil, fmt.Errorf("service.name required")
	}
	var bs []Binding
	for _, a := range []struct {
		key string
		ax  axis
	}{{"up", s.Up}, {"health", s.Health}, {"version", s.Version}} {
		b, ok, err := axisBinding(s.Name, a.key, a.ax)
		if err != nil {
			return nil, err
		}
		if ok {
			bs = append(bs, b)
		}
	}
	for _, m := range s.Metrics {
		if m.Key == "" || len(m.Exec) == 0 {
			return nil, fmt.Errorf("service %s: each metric needs key + exec", s.Name)
		}
		out := s.Name + "_" + m.Key
		args := map[string]any{"cmd": m.Exec}
		if m.Parse != "" {
			args["parse"] = m.Parse
		}
		bs = append(bs, Binding{ID: out, Primitive: "exec", Args: args, OutputKey: out})
	}
	return bs, nil
}

// axisBinding maps one composable axis to a Binding. Returns ok=false when the
// axis is omitted (empty), an error when a probe key is present but unknown.
func axisBinding(name, axisName string, ax axis) (Binding, bool, error) {
	if len(ax) == 0 {
		return Binding{}, false, nil
	}
	out := name + "_" + axisName
	switch {
	case ax["tcp"] != nil:
		host := axisStr(ax, "host", "127.0.0.1")
		return Binding{ID: out, Primitive: "tcp.connect",
			Args: map[string]any{"host": host, "port": ax["tcp"]}, OutputKey: out}, true, nil
	case ax["http"] != nil:
		if path, ok := ax["path"]; ok && path != nil {
			return Binding{ID: out, Primitive: "http.get_json",
				Args: map[string]any{"url": ax["http"], "path": path}, OutputKey: out}, true, nil
		}
		return Binding{ID: out, Primitive: "http.get",
			Args: map[string]any{"url": ax["http"], "expect": "ok"}, OutputKey: out}, true, nil
	case ax["systemd"] != nil:
		return Binding{ID: out, Primitive: "systemctl.is_active",
			Args: map[string]any{"unit": ax["systemd"]}, OutputKey: out}, true, nil
	case ax["docker"] != nil:
		return Binding{ID: out, Primitive: "exec",
			Args: map[string]any{
				"cmd":   []any{"docker", "inspect", "-f", "{{.State.Running}}", ax["docker"]},
				"parse": "trimmed",
			}, OutputKey: out}, true, nil
	case ax["exec"] != nil:
		args := map[string]any{"cmd": ax["exec"]}
		if p, ok := ax["parse"]; ok {
			args["parse"] = p
		}
		return Binding{ID: out, Primitive: "exec", Args: args, OutputKey: out}, true, nil
	default:
		return Binding{}, false, fmt.Errorf("service %s axis %q: unknown probe (want tcp|http|systemd|docker|exec)", name, axisName)
	}
}

func axisStr(ax axis, key, def string) string {
	if v, ok := ax[key].(string); ok && v != "" {
		return v
	}
	return def
}
