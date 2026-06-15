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
	"path/filepath"
	"time"

	"github.com/icombilisim/iclic-host-agent/internal/collectors"
	"github.com/icombilisim/iclic-host-agent/internal/config"
)

// AgentVersion is managed by release-please — the x-release-please-version
// annotation lets it rewrite this line on each release. Don't hand-edit the
// value; protocol-level changes still bump ProtocolVersion below. (#337)
const AgentVersion = "0.8.0" // x-release-please-version

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
	servicesDir  string
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
		// Service definitions live beside collectors.d (sibling services.d), so a
		// dev override of the collector dir moves both together. (#342)
		servicesDir: filepath.Join(filepath.Dir(collectorDir), "services.d"),
		registry:    collectors.DefaultRegistry(),
	}
}

// SendOnce builds a payload and POSTs it to /api/v1/server/{serverId}/heartbeat.
// Errors are returned but not retried — the caller's ticker will retry on the
// next interval.
func (s *Sender) SendOnce(ctx context.Context) error {
	payload := s.buildPayload(ctx)
	hostPayload := payload.withoutMetric("runtime_instances")
	body, err := json.Marshal(hostPayload)
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
	defer drainAndClose(resp.Body)

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("heartbeat rejected: status=%d body=%s", resp.StatusCode, string(respBody))
	}
	runtimeCount := s.sendRuntimeSignals(ctx, payload)
	// INFO so journalctl shows the operator a heartbeat actually went through
	// — DEBUG was effectively silent at the default log level. (#35)
	slog.Info("heartbeat accepted",
		"status", resp.StatusCode,
		"binding_count", payload.metricCount(),
		"runtime_signal_count", runtimeCount,
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

func (p Payload) withoutMetric(key string) Payload {
	if _, ok := p.Metrics[key]; !ok {
		return p
	}
	metrics := make(map[string]any, len(p.Metrics)-1)
	for k, v := range p.Metrics {
		if k != key {
			metrics[k] = v
		}
	}
	p.Metrics = metrics
	return p
}

// RuntimeSignal is the agent-side shape forwarded to ICLIC's runtime
// deployment status endpoint. Operators can produce an array of these under
// metrics.runtime_instances via YAML bindings or legacy scripts. (#97)
type RuntimeSignal struct {
	RuntimeComponentID *int64         `json:"runtimeComponentId,omitempty"`
	ProductCode        string         `json:"productCode,omitempty"`
	ComponentCode      string         `json:"componentCode,omitempty"`
	InstallationID     *int64         `json:"installationId,omitempty"`
	NodeID             *int64         `json:"nodeId,omitempty"`
	InstanceKey        string         `json:"instanceKey,omitempty"`
	Environment        string         `json:"environment,omitempty"`
	Status             string         `json:"status,omitempty"`
	VersionSource      string         `json:"versionSource,omitempty"`
	RunningVersion     string         `json:"runningVersion,omitempty"`
	GitCommit          string         `json:"gitCommit,omitempty"`
	BuildTime          string         `json:"buildTime,omitempty"`
	Notes              string         `json:"notes,omitempty"`
	Payload            map[string]any `json:"payload,omitempty"`
}

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

	// Service definitions (services.d/*.yaml) expand into the same Bindings and
	// run in the same pass. A bad service file skips only the service metrics. (#342)
	if svc, serr := collectors.LoadServiceDir(s.servicesDir); serr != nil {
		slog.Warn("collectors.LoadServiceDir failed — skipping service metrics",
			"dir", s.servicesDir, "err", serr)
	} else {
		bindings = append(bindings, svc...)
	}

	metrics := collectors.Run(ctx, bindings, s.registry, perBindingTimeout)

	// Advertise the host's service definitions so the Fleet UI can render generic
	// per-service cards (status/version from the <name>_* metrics above, plus a
	// logs button) — ICLIC never has to know the app. (#342 4d-3)
	if summaries, serr := collectors.LoadServiceSummaries(s.servicesDir); serr == nil && len(summaries) > 0 {
		metrics["services"] = summaries
	}

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

func (s *Sender) sendRuntimeSignals(ctx context.Context, payload Payload) int {
	signals := runtimeSignals(payload.Metrics["runtime_instances"])
	for _, signal := range signals {
		if signal.VersionSource == "" {
			signal.VersionSource = "HOST_AGENT"
		}
		if signal.Status == "" {
			signal.Status = "HEALTHY"
		}
		if err := s.postRuntimeSignal(ctx, signal); err != nil {
			slog.Warn("runtime signal rejected",
				"product_code", signal.ProductCode,
				"component_code", signal.ComponentCode,
				"instance_key", signal.InstanceKey,
				"err", err,
			)
		}
	}
	return len(signals)
}

func (s *Sender) postRuntimeSignal(ctx context.Context, signal RuntimeSignal) error {
	body, err := json.Marshal(signal)
	if err != nil {
		return fmt.Errorf("marshal runtime signal: %w", err)
	}
	url := s.cfg.ICLICUrl + "/api/v1/server/runtime-instances/heartbeat"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build runtime request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "iclic-host-agent/"+AgentVersion)
	req.Header.Set("Authorization", "Bearer "+s.bearer)

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("post runtime signal: %w", err)
	}
	defer drainAndClose(resp.Body)
	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("status=%d body=%s", resp.StatusCode, string(respBody))
	}
	return nil
}

// drainAndClose fully reads any trailing response bytes (capped at 1 MB) so
// the HTTP/1.1 keep-alive connection can be returned to the shared transport
// pool. Without this, the next heartbeat dials a fresh TCP connection — over
// nine days of 60 s ticks that's ~13K leaked sockets and the Transport's
// idle-conn machinery never gets to amortize. (#2)
func drainAndClose(body io.ReadCloser) {
	_, _ = io.Copy(io.Discard, io.LimitReader(body, 1<<20))
	_ = body.Close()
}

func runtimeSignals(value any) []RuntimeSignal {
	if value == nil {
		return nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		slog.Warn("runtime_instances marshal failed", "err", err)
		return nil
	}
	var signals []RuntimeSignal
	if err := json.Unmarshal(data, &signals); err != nil {
		slog.Warn("runtime_instances parse failed", "err", err)
		return nil
	}
	return signals
}
