// Package config loads agent configuration from a JSON file (default
// /etc/iclic-host-agent/config.json) with environment-variable overrides for
// dev runs. JSON keeps the dependency graph at zero — a YAML loader can be
// dropped in later if config files start hand-edited often.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
)

const (
	defaultConfigPath        = "/etc/iclic-host-agent/config.json"
	defaultIntervalSec       = 60
	envConfigPath            = "ICLIC_AGENT_CONFIG"
	envICLICUrl              = "ICLIC_URL"
	envServerID              = "ICLIC_SERVER_ID"
	envAgentKid              = "ICLIC_AGENT_KID"
	envAgentSecret           = "ICLIC_AGENT_SECRET"
	envHeartbeatIntervalSecs = "ICLIC_HEARTBEAT_INTERVAL_SECS"
)

// Config is the resolved agent configuration. All fields are required except
// where noted.
type Config struct {
	ICLICUrl                 string `json:"iclic_url"`
	ServerID                 string `json:"server_id"`
	AgentKid                 string `json:"agent_kid"`
	AgentSecret              string `json:"agent_secret"`
	HeartbeatIntervalSeconds int    `json:"heartbeat_interval_seconds"`
}

// Load reads JSON config from the path in $ICLIC_AGENT_CONFIG (or the default
// /etc/iclic-host-agent/config.json), then overlays env-var overrides for any
// field. Missing required fields return a descriptive error.
func Load() (*Config, error) {
	cfg := &Config{HeartbeatIntervalSeconds: defaultIntervalSec}

	path := os.Getenv(envConfigPath)
	if path == "" {
		path = defaultConfigPath
	}
	if data, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("parse %s: %w", path, err)
		}
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	if v := os.Getenv(envICLICUrl); v != "" {
		cfg.ICLICUrl = v
	}
	if v := os.Getenv(envServerID); v != "" {
		cfg.ServerID = v
	}
	if v := os.Getenv(envAgentKid); v != "" {
		cfg.AgentKid = v
	}
	if v := os.Getenv(envAgentSecret); v != "" {
		cfg.AgentSecret = v
	}
	if v := os.Getenv(envHeartbeatIntervalSecs); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return nil, fmt.Errorf("invalid %s: %w", envHeartbeatIntervalSecs, err)
		}
		cfg.HeartbeatIntervalSeconds = n
	}

	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c *Config) validate() error {
	if c.ICLICUrl == "" {
		return fmt.Errorf("iclic_url (or %s) is required", envICLICUrl)
	}
	if c.ServerID == "" {
		return fmt.Errorf("server_id (or %s) is required", envServerID)
	}
	if c.AgentKid == "" {
		return fmt.Errorf("agent_kid (or %s) is required - run the installer to enroll", envAgentKid)
	}
	if c.AgentSecret == "" {
		return fmt.Errorf("agent_secret (or %s) is required - run the installer to enroll", envAgentSecret)
	}
	if c.HeartbeatIntervalSeconds < 10 {
		return fmt.Errorf("heartbeat_interval_seconds must be >= 10, got %d", c.HeartbeatIntervalSeconds)
	}
	return nil
}
