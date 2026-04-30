// Package heartbeat builds and sends a heartbeat payload to ICLIC.
//
// Real metric collectors (CPU, memory, disk, OS, security updates) land in a
// follow-up commit. The current Sender emits a placeholder payload so the
// end-to-end enrollment + bearer-auth + protocol-version flow can be exercised
// against the ICLIC backend stub.
package heartbeat

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/icombilisim/iclic-host-agent/internal/config"
)

// AgentVersion is bumped per release; protocol-level changes also bump
// ProtocolVersion in the payload.
const AgentVersion = "0.1.0-scaffold"

// ProtocolVersion is the heartbeat schema version. Bumped on breaking changes;
// ICLIC accepts the last N versions per docs/protocol.md.
const ProtocolVersion = 1

// Sender posts heartbeats to the configured ICLIC backend.
type Sender struct {
	cfg    *config.Config
	bearer string
	client *http.Client
}

// NewSender wires a Sender from agent config. The bearer is precomputed once
// at construction — kid+secret are immutable for the agent's lifetime.
func NewSender(cfg *config.Config) *Sender {
	return &Sender{
		cfg:    cfg,
		bearer: cfg.AgentKid + "." + cfg.AgentSecret,
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

// SendOnce builds a payload and POSTs it to /api/v1/server/{serverId}/heartbeat.
// Errors are returned but not retried — the caller's ticker will retry on the
// next interval.
func (s *Sender) SendOnce(ctx context.Context) error {
	payload := s.buildPayload()
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	url := fmt.Sprintf("%s/api/v1/server/%s/heartbeat", s.cfg.ICLICUrl, s.cfg.ServerID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "iclic-host-agent/"+AgentVersion)
	// PAT-style bearer: ICLIC splits at the first '.' and verifies the
	// secret half against the stored SHA-256 digest. Plain TLS provides
	// confidentiality on the wire — the previous request-signing scheme
	// added complexity without measurable benefit. (#2)
	req.Header.Set("Authorization", "Bearer "+s.bearer)

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("post heartbeat: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("heartbeat rejected: status=%d body=%s", resp.StatusCode, string(respBody))
	}
	slog.Debug("heartbeat accepted", "status", resp.StatusCode)
	return nil
}

// Payload is the wire shape ICLIC accepts on
// POST /api/v1/server/{serverId}/heartbeat. Top-level keys are camelCase to
// match ICLIC's default Jackson naming; the inner Metrics map is free-form so
// the agent can grow new fields without an ICLIC-side schema change. (#2)
type Payload struct {
	AgentVersion    string         `json:"agentVersion"`
	ProtocolVersion int            `json:"protocolVersion"`
	Metrics         map[string]any `json:"metrics"`
}

// Disk is a single mount's usage snapshot. It lives inside the free-form
// metrics map so ICLIC stores it as JSON without a per-field column.
type Disk struct {
	Mount   string  `json:"mount"`
	UsedPct float64 `json:"used_pct"`
	TotalGB int64   `json:"total_gb"`
}

// buildPayload assembles the wire payload. Real collectors land in a follow-up
// commit; the scaffold emits an empty disks list + zero metrics so ICLIC's
// 400-validation path is exercised with a well-formed body. (#2)
func (s *Sender) buildPayload() Payload {
	return Payload{
		AgentVersion:    AgentVersion,
		ProtocolVersion: ProtocolVersion,
		Metrics: map[string]any{
			"reported_at":                 time.Now().UTC().Format(time.RFC3339),
			"status":                      "UP",
			"uptime_sec":                  int64(0),
			"os_name":                     "",
			"os_version":                  "",
			"kernel":                      "",
			"cpu_load_1m":                 0.0,
			"cpu_load_5m":                 0.0,
			"mem_used_pct":                0.0,
			"mem_total_mb":                int64(0),
			"disks":                       []Disk{},
			"os_security_updates_pending": 0,
		},
	}
}
