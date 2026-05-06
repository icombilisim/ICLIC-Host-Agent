// Package heartbeat builds and sends a heartbeat payload to ICLIC.
//
// The metric body is produced by the collector pipeline (see internal/collectors)
// — the heartbeat package only owns the wire envelope, transport, and the
// agent-intrinsic fields the operator can't redefine (reported_at, status).
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

	"github.com/icombilisim/iclic-host-agent/internal/collectors"
	"github.com/icombilisim/iclic-host-agent/internal/config"
)

// AgentVersion is bumped per release; protocol-level changes also bump
// ProtocolVersion in the payload.
const AgentVersion = "0.2.0"

// ProtocolVersion is the heartbeat schema version. Bumped on breaking changes;
// ICLIC accepts the last N versions per docs/protocol.md.
const ProtocolVersion = 1

// perBindingTimeout caps any single primitive invocation. The total walltime
// of one tick is also bounded by the heartbeat ctx — see SendOnce.
const perBindingTimeout = 5 * time.Second

// totalCollectTimeout caps the entire collector phase so a slow probe never
// pushes the heartbeat past the next tick. Bindings run concurrently, so the
// budget is per-tick total — not per-binding.
const totalCollectTimeout = 30 * time.Second

// Sender posts heartbeats to the configured ICLIC backend.
type Sender struct {
	cfg          *config.Config
	bearer       string
	client       *http.Client
	collectorDir string
	registry     map[string]collectors.PrimitiveFunc
}

// NewSender wires a Sender from agent config. The bearer is precomputed once
// at construction — kid+secret are immutable for the agent's lifetime.
func NewSender(cfg *config.Config, collectorDir string) *Sender {
	return &Sender{
		cfg:          cfg,
		bearer:       cfg.AgentKid + "." + cfg.AgentSecret,
		client:       &http.Client{Timeout: 10 * time.Second},
		collectorDir: collectorDir,
		registry:     collectors.DefaultRegistry(),
	}
}

// SendOnce builds a payload and POSTs it to /api/v1/server/{serverId}/heartbeat.
// Errors are returned but not retried — the caller's ticker will retry on the
// next interval.
func (s *Sender) SendOnce(ctx context.Context) error {
	payload := s.buildPayload(ctx)
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
	// INFO so journalctl shows the operator a heartbeat actually went through
	// — DEBUG was effectively silent at the default log level. (#35)
	slog.Info("heartbeat accepted",
		"status", resp.StatusCode,
		"binding_count", payload.metricCount(),
	)
	return nil
}

// Payload is the wire shape ICLIC accepts on
// POST /api/v1/server/{serverId}/heartbeat.
type Payload struct {
	AgentVersion    string         `json:"agentVersion"`
	ProtocolVersion int            `json:"protocolVersion"`
	Metrics         map[string]any `json:"metrics"`
}

func (p Payload) metricCount() int { return len(p.Metrics) }

// buildPayload runs the collector pipeline and stamps in the agent-intrinsic
// fields the operator can't redefine.
func (s *Sender) buildPayload(parent context.Context) Payload {
	ctx, cancel := context.WithTimeout(parent, totalCollectTimeout)
	defer cancel()

	bindings, err := collectors.LoadDir(s.collectorDir)
	if err != nil {
		slog.Warn("collectors.LoadDir failed — sending heartbeat without metrics",
			"dir", s.collectorDir,
			"err", err,
		)
		bindings = nil
	}

	metrics := collectors.Run(ctx, bindings, s.registry, perBindingTimeout)

	// Agent-intrinsic fields go in last so they always win over a binding
	// that tries to redefine them. `reported_at` is the agent's wall clock
	// at sample time; ICLIC also stamps `received_at` server-side so clock
	// skew is observable.
	metrics["reported_at"] = time.Now().UTC().Format(time.RFC3339)
	if _, ok := metrics["status"]; !ok {
		metrics["status"] = "UP"
	}

	return Payload{
		AgentVersion:    AgentVersion,
		ProtocolVersion: ProtocolVersion,
		Metrics:         metrics,
	}
}
