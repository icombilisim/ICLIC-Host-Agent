// Package heartbeat builds and sends a heartbeat payload to ICLIC.
//
// Real metric collectors (CPU, memory, disk, OS, security updates) land in a
// follow-up commit. The current Sender emits a placeholder payload so the
// end-to-end HMAC + enrollment + protocol-version flow can be exercised
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
	"github.com/icombilisim/iclic-host-agent/internal/hmacsign"
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
	signer *hmacsign.Signer
	client *http.Client
}

// NewSender wires a Sender from agent config.
func NewSender(cfg *config.Config) *Sender {
	return &Sender{
		cfg:    cfg,
		signer: hmacsign.New(cfg.AgentKid, cfg.AgentSecret),
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
	s.signer.Sign(req, body)

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

// Payload is the wire shape — see docs/protocol.md.
type Payload struct {
	ProtocolVersion int       `json:"protocol_version"`
	AgentVersion    string    `json:"agent_version"`
	ServerID        string    `json:"server_id"`
	ReportedAt      time.Time `json:"reported_at"`
	Status          string    `json:"status"`
	Host            HostInfo  `json:"host"`
}

// HostInfo carries the host-level metric snapshot. Fields filled in by real
// collectors in a follow-up commit; the scaffold emits zero values + the OS
// strings the runtime exposes for free.
type HostInfo struct {
	UptimeSec                int64   `json:"uptime_sec"`
	OSName                   string  `json:"os_name"`
	OSVersion                string  `json:"os_version"`
	Kernel                   string  `json:"kernel"`
	CPULoad1m                float64 `json:"cpu_load_1m"`
	CPULoad5m                float64 `json:"cpu_load_5m"`
	MemUsedPct               float64 `json:"mem_used_pct"`
	MemTotalMB               int64   `json:"mem_total_mb"`
	Disks                    []Disk  `json:"disks"`
	DockerVersion            string  `json:"docker_version,omitempty"`
	OSSecurityUpdatesPending int     `json:"os_security_updates_pending"`
}

// Disk is a single mount's usage snapshot.
type Disk struct {
	Mount   string  `json:"mount"`
	UsedPct float64 `json:"used_pct"`
	TotalGB int64   `json:"total_gb"`
}

func (s *Sender) buildPayload() Payload {
	return Payload{
		ProtocolVersion: ProtocolVersion,
		AgentVersion:    AgentVersion,
		ServerID:        s.cfg.ServerID,
		ReportedAt:      time.Now().UTC(),
		Status:          "UP",
		Host:            HostInfo{Disks: []Disk{}},
	}
}
