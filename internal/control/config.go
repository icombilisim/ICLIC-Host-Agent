package control

import (
	"os"
	"path/filepath"
	"sort"

	"github.com/icombilisim/iclic-host-agent/internal/collectors"
	"gopkg.in/yaml.v3"
)

// defaultControlConfigPath is the operator-owned allow-list. ABSENT BY DESIGN —
// if the file does not exist the control channel connects but serves nothing.
// Overridable via $ICLIC_CONTROL_CONFIG for dev runs.
const defaultControlConfigPath = "/etc/iclic-host-agent/control.yaml"
const envControlConfigPath = "ICLIC_CONTROL_CONFIG"

// hard ceilings the operator config can lower but never exceed — a stolen ICLIC
// must not be able to request an unbounded stream. (#337)
const (
	hardMaxLines         = 5000
	hardMaxFollowSeconds = 3600
	defaultLines         = 200
	defaultMaxLines      = 2000
	defaultFollowSeconds = 600
)

// ControlConfig is the agent-authoritative allow-list. The agent — not ICLIC —
// is the authority: only what is enabled here is ever served. (#337)
type ControlConfig struct {
	Control sectionControl `yaml:"control"`
}

type sectionControl struct {
	Enabled bool       `yaml:"enabled"`
	Logs    logsConfig `yaml:"logs"`
	Top     simpleVerb `yaml:"top"`   // proc.top
	Df      simpleVerb `yaml:"df"`    // disk.df
	Ports   simpleVerb `yaml:"ports"` // net.listen
}

// simpleVerb is an opt-in toggle for a read verb that needs no source map.
type simpleVerb struct {
	Enabled bool `yaml:"enabled"`
}

type logsConfig struct {
	Enabled          bool                 `yaml:"enabled"`
	DefaultLines     int                  `yaml:"default_lines"`
	MaxLines         int                  `yaml:"max_lines"`
	MaxFollowSeconds int                  `yaml:"max_follow_seconds"`
	Sources          map[string]logSource `yaml:"sources"`
}

// logSource maps a logical name (sent by ICLIC) to a concrete, host-specific
// source. Flexibility lives here: the same verb works whether the logs are in a
// docker container, a file, or journald — ICLIC never needs to know which.
type logSource struct {
	Type      string `yaml:"type"`      // docker | file | journald
	Container string `yaml:"container"` // type=docker
	Path      string `yaml:"path"`      // type=file
	Unit      string `yaml:"unit"`      // type=journald
}

// loadControlConfig reads the allow-list, applying safe defaults. A missing file
// (or parse error) yields a fully-disabled config — fail closed. (#337)
func loadControlConfig() ControlConfig {
	path := os.Getenv(envControlConfigPath)
	if path == "" {
		path = defaultControlConfigPath
	}
	var cfg ControlConfig
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg // absent/unreadable = serve nothing
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return ControlConfig{} // malformed = serve nothing, never a partial parse
	}
	l := &cfg.Control.Logs
	if l.DefaultLines <= 0 {
		l.DefaultLines = defaultLines
	}
	if l.MaxLines <= 0 {
		l.MaxLines = defaultMaxLines
	}
	if l.MaxLines > hardMaxLines {
		l.MaxLines = hardMaxLines
	}
	if l.MaxFollowSeconds <= 0 {
		l.MaxFollowSeconds = defaultFollowSeconds
	}
	if l.MaxFollowSeconds > hardMaxFollowSeconds {
		l.MaxFollowSeconds = hardMaxFollowSeconds
	}

	// Service definitions contribute log sources to the channel — define a
	// service once (services.d) and its logs are tailable without re-listing
	// them here. Gated on the operator having enabled the channel + logs
	// (default OFF), so a service-def can never bypass the opt-in. control.yaml
	// sources win on a name clash. (#342 4d-2)
	if cfg.Control.Enabled && cfg.Control.Logs.Enabled {
		servicesDir := filepath.Join(filepath.Dir(path), "services.d")
		if sources, srcErr := collectors.LoadServiceLogSources(servicesDir); srcErr == nil {
			for _, s := range sources {
				if s.Type == "" {
					continue // a logs: block with no type isn't usable
				}
				if l.Sources == nil {
					l.Sources = map[string]logSource{}
				}
				if _, exists := l.Sources[s.Name]; !exists {
					l.Sources[s.Name] = logSource{Type: s.Type, Container: s.Container, Path: s.Path, Unit: s.Unit}
				}
			}
		}
	}
	return cfg
}

// logsEnabled reports whether logs.tail may be served at all.
func (c ControlConfig) logsEnabled() bool {
	return c.Control.Enabled && c.Control.Logs.Enabled && len(c.Control.Logs.Sources) > 0
}

func (c ControlConfig) topEnabled() bool   { return c.Control.Enabled && c.Control.Top.Enabled }
func (c ControlConfig) dfEnabled() bool    { return c.Control.Enabled && c.Control.Df.Enabled }
func (c ControlConfig) portsEnabled() bool { return c.Control.Enabled && c.Control.Ports.Enabled }

// source resolves a logical name to its concrete source, honouring the allow-list.
func (c ControlConfig) source(name string) (logSource, bool) {
	s, ok := c.Control.Logs.Sources[name]
	return s, ok
}

// verbs lists the verbs this agent currently permits — advertised to ICLIC so
// the Fleet UI shows only what each host actually allows.
func (c ControlConfig) verbs() []string {
	verbs := []string{}
	if c.logsEnabled() {
		verbs = append(verbs, "logs.tail")
	}
	if c.topEnabled() {
		verbs = append(verbs, "proc.top")
	}
	if c.dfEnabled() {
		verbs = append(verbs, "disk.df")
	}
	if c.portsEnabled() {
		verbs = append(verbs, "net.listen")
	}
	return verbs
}

// sourceNames returns the permitted log source names (sorted, for stable UI).
func (c ControlConfig) sourceNames() []string {
	names := make([]string, 0, len(c.Control.Logs.Sources))
	for k := range c.Control.Logs.Sources {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}
