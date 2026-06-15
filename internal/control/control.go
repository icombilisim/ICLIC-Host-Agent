// Package control maintains the agent's outbound control channel to ICLIC.
//
// Transport model (design: fleet-control-channel-design.md, #40 Faz 4a): the
// agent dials a persistent outbound WebSocket — firewall/NAT-friendly, the agent
// never listens. ICLIC then rides the held socket to REQUEST on-demand data;
// the agent is the authority and serves a closed, typed set of verbs. There is
// no shell pass-through. Spike scope: prove transport + auth + request/response
// with a single `ping` verb; real verbs (logs.tail, proc.top, disk.df) land next.
package control

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"runtime"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/icombilisim/iclic-host-agent/internal/config"
)

const (
	// protocolVersion is the control-channel wire version, independent of the
	// heartbeat ProtocolVersion. Bumped on breaking frame-shape changes.
	protocolVersion = 1
	dialTimeout     = 15 * time.Second
	maxBackoff      = 30 * time.Second
)

// hello is the first frame the agent sends after connecting: identity plus the
// capabilities ICLIC may request. The Fleet UI derives button states from this.
type hello struct {
	Type            string       `json:"type"`
	AgentVersion    string       `json:"agentVersion"`
	ProtocolVersion int          `json:"protocolVersion"`
	OS              string       `json:"os"`
	Arch            string       `json:"arch"`
	Capabilities    capabilities `json:"capabilities"`
}

type capabilities struct {
	Verbs []string `json:"verbs"`
}

// reqFrame is an ICLIC-initiated request. The agent decides what (if anything)
// it does — unknown/unpermitted verbs are rejected, never executed blindly.
type reqFrame struct {
	Type   string         `json:"type"`
	ReqID  string         `json:"reqId"`
	Verb   string         `json:"verb"`
	Target string         `json:"target"`
	Args   map[string]any `json:"args"`
}

// RunControlChannel keeps a control socket up for the agent's lifetime,
// reconnecting with capped exponential backoff. It returns only when ctx is
// cancelled (process shutdown).
func RunControlChannel(ctx context.Context, cfg *config.Config, agentVersion string) {
	bearer := cfg.AgentKid + "." + cfg.AgentSecret
	url := controlURL(cfg.ICLICUrl)
	backoff := time.Second
	for {
		if ctx.Err() != nil {
			return
		}
		err := connectOnce(ctx, url, bearer, agentVersion)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			slog.Warn("control channel disconnected", "err", err, "retry_in", backoff.String())
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff *= 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}

// connectOnce dials, sends hello, then serves request frames until the socket
// or context closes.
func connectOnce(ctx context.Context, url, bearer, agentVersion string) error {
	dialCtx, cancel := context.WithTimeout(ctx, dialTimeout)
	defer cancel()

	conn, _, err := websocket.Dial(dialCtx, url, &websocket.DialOptions{
		HTTPHeader: http.Header{
			"Authorization": []string{"Bearer " + bearer},
			"User-Agent":    []string{"iclic-host-agent/" + agentVersion},
		},
	})
	if err != nil {
		return err
	}
	// Default close; replaced by a clean close if the read loop ends normally.
	defer conn.Close(websocket.StatusInternalError, "")
	slog.Info("control channel connected", "url", url)

	h := hello{
		Type:            "hello",
		AgentVersion:    agentVersion,
		ProtocolVersion: protocolVersion,
		OS:              runtime.GOOS,
		Arch:            runtime.GOARCH,
		Capabilities:    capabilities{Verbs: []string{"ping"}},
	}
	hb, err := json.Marshal(h)
	if err != nil {
		return err
	}
	if err := conn.Write(ctx, websocket.MessageText, hb); err != nil {
		return err
	}

	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return err
		}
		handleFrame(ctx, conn, data)
	}
}

// handleFrame dispatches one ICLIC request. Spike: only `ping` is served.
func handleFrame(ctx context.Context, conn *websocket.Conn, data []byte) {
	var req reqFrame
	if err := json.Unmarshal(data, &req); err != nil {
		slog.Warn("control channel: bad frame", "err", err)
		return
	}
	if req.Type != "req" {
		return
	}
	switch req.Verb {
	case "ping":
		writeFrame(ctx, conn, map[string]any{"type": "res", "reqId": req.ReqID, "seq": 0, "data": "pong"})
		writeFrame(ctx, conn, map[string]any{"type": "res", "reqId": req.ReqID, "eof": true})
	default:
		// The agent serves only its closed verb set; everything else is refused. (#40)
		writeFrame(ctx, conn, map[string]any{"type": "res", "reqId": req.ReqID, "error": "unknown_verb", "code": 400})
	}
}

func writeFrame(ctx context.Context, conn *websocket.Conn, frame map[string]any) {
	b, err := json.Marshal(frame)
	if err != nil {
		slog.Warn("control channel: marshal frame", "err", err)
		return
	}
	if err := conn.Write(ctx, websocket.MessageText, b); err != nil {
		slog.Warn("control channel: write frame", "err", err)
	}
}

// controlURL derives the wss:// control endpoint from the configured ICLIC base
// URL, reusing the same host the heartbeat already trusts.
func controlURL(base string) string {
	u := base
	switch {
	case strings.HasPrefix(u, "https://"):
		u = "wss://" + strings.TrimPrefix(u, "https://")
	case strings.HasPrefix(u, "http://"):
		u = "ws://" + strings.TrimPrefix(u, "http://")
	}
	return strings.TrimRight(u, "/") + "/api/v1/server/control"
}
