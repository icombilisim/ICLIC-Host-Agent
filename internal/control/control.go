// Package control maintains the agent's outbound control channel to ICLIC.
//
// Transport model (design: fleet-control-channel-design.md, #40 Faz 4a): the
// agent dials a persistent outbound WebSocket — firewall/NAT-friendly, the agent
// never listens. ICLIC then rides the held socket to REQUEST on-demand data; the
// agent is the authority and serves only the closed, typed verb set its local
// allow-list (control.yaml) enables. There is no shell pass-through.
package control

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
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
	readLimitBytes  = 256 * 1024  // inbound requests are tiny; cap to bound memory
	lineBufferBytes = 1024 * 1024 // a single log line may be large; cap, don't OOM
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
	Verbs []string  `json:"verbs"`
	Logs  *logsCaps `json:"logs,omitempty"`
}

type logsCaps struct {
	Sources          []string `json:"sources"`
	MaxLines         int      `json:"maxLines"`
	MaxFollowSeconds int      `json:"maxFollowSeconds"`
}

// reqFrame is an ICLIC-initiated request or cancel. The agent decides what (if
// anything) it does — unknown/unpermitted verbs are refused, never executed.
type reqFrame struct {
	Type   string         `json:"type"` // "req" | "cancel"
	ReqID  string         `json:"reqId"`
	Verb   string         `json:"verb"`
	Target string         `json:"target"`
	Args   map[string]any `json:"args"`
}

// session wraps one live control socket: a write mutex (the WS allows only one
// writer at a time) and the set of in-flight streams so a cancel can stop them.
type session struct {
	conn    *websocket.Conn
	cfg     ControlConfig
	writeMu sync.Mutex
	mu      sync.Mutex
	streams map[string]context.CancelFunc
}

// RunControlChannel keeps a control socket up for the agent's lifetime,
// reconnecting with capped exponential backoff. Returns only when ctx is
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

// connectOnce dials, advertises capabilities, then serves request frames until
// the socket or context closes.
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
	defer conn.Close(websocket.StatusInternalError, "")
	conn.SetReadLimit(readLimitBytes)

	cfg := loadControlConfig() // re-read per connect so opt-in changes apply on reconnect
	s := &session{conn: conn, cfg: cfg, streams: make(map[string]context.CancelFunc)}
	slog.Info("control channel connected", "url", url, "verbs", cfg.verbs())

	if err := s.write(ctx, s.helloFrame(agentVersion)); err != nil {
		return err
	}
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return err
		}
		s.dispatch(ctx, data)
	}
}

func (s *session) helloFrame(agentVersion string) hello {
	caps := capabilities{Verbs: s.cfg.verbs()}
	if s.cfg.logsEnabled() {
		caps.Logs = &logsCaps{
			Sources:          s.cfg.sourceNames(),
			MaxLines:         s.cfg.Control.Logs.MaxLines,
			MaxFollowSeconds: s.cfg.Control.Logs.MaxFollowSeconds,
		}
	}
	return hello{
		Type: "hello", AgentVersion: agentVersion, ProtocolVersion: protocolVersion,
		OS: runtime.GOOS, Arch: runtime.GOARCH, Capabilities: caps,
	}
}

// dispatch routes one inbound frame. req verbs run in their own goroutine so the
// read loop stays free to receive cancels; cancel stops an in-flight stream.
func (s *session) dispatch(ctx context.Context, data []byte) {
	var f reqFrame
	if err := json.Unmarshal(data, &f); err != nil {
		slog.Warn("control channel: bad frame", "err", err)
		return
	}
	switch f.Type {
	case "req":
		s.handleReq(ctx, f)
	case "cancel":
		s.cancelStream(f.ReqID)
	}
}

func (s *session) handleReq(ctx context.Context, f reqFrame) {
	switch f.Verb {
	case "ping":
		s.write(ctx, resFrame(f.ReqID, 0, "pong"))
		s.write(ctx, eofFrame(f.ReqID))
	case "logs.tail":
		s.spawn(ctx, f, s.logsJob)
	case "proc.top":
		s.spawn(ctx, f, s.procTopJob)
	case "disk.df":
		s.spawn(ctx, f, s.diskDfJob)
	case "net.listen":
		s.spawn(ctx, f, s.netListenJob)
	default:
		// The agent serves only its closed verb set; everything else is refused. (#337)
		s.write(ctx, errFrame(f.ReqID, "unknown_verb", 400))
	}
}

// spawn runs a streaming job in its own goroutine, registered so a cancel frame
// can stop it. The cancel also fires when the job returns, killing any lingering
// process. (#337)
func (s *session) spawn(ctx context.Context, f reqFrame, job func(context.Context, reqFrame)) {
	reqCtx, cancel := context.WithCancel(ctx)
	s.addStream(f.ReqID, cancel)
	go func() {
		defer s.removeStream(f.ReqID)
		defer cancel()
		job(reqCtx, f)
	}()
}

// logsJob serves logs.tail: resolve the logical target to a concrete source and
// stream (or follow) its lines, bounded by the allow-list.
func (s *session) logsJob(ctx context.Context, f reqFrame) {
	if !s.cfg.logsEnabled() {
		s.bestEffort(errFrame(f.ReqID, "not_permitted", 403))
		return
	}
	src, ok := s.cfg.source(f.Target)
	if !ok {
		s.bestEffort(errFrame(f.ReqID, "unknown_target", 404))
		return
	}
	lines := clampInt(argInt(f.Args, "lines", s.cfg.Control.Logs.DefaultLines), 1, s.cfg.Control.Logs.MaxLines)
	follow := argBool(f.Args, "follow", false)
	if follow {
		// Live streams auto-stop — a stolen ICLIC can't hold a tail open forever.
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(s.cfg.Control.Logs.MaxFollowSeconds)*time.Second)
		defer cancel()
	}
	argv, err := logArgs(src, lines, follow)
	if err != nil {
		s.bestEffort(errFrame(f.ReqID, err.Error(), 400))
		return
	}
	s.streamCommand(ctx, f.ReqID, argv, 0) // source --tail/-f bounds the line count
}

// procTopJob serves proc.top: a snapshot of the busiest processes (Linux ps).
func (s *session) procTopJob(ctx context.Context, f reqFrame) {
	if !s.cfg.topEnabled() {
		s.bestEffort(errFrame(f.ReqID, "not_permitted", 403))
		return
	}
	n := clampInt(argInt(f.Args, "lines", 20), 1, 100)
	argv := []string{"ps", "-eo", "pid,user,pcpu,pmem,rss,comm", "--sort=-pcpu"}
	s.streamCommand(ctx, f.ReqID, argv, n+1) // +1 for the header row
}

// diskDfJob serves disk.df: filesystem usage snapshot (Linux df).
func (s *session) diskDfJob(ctx context.Context, f reqFrame) {
	if !s.cfg.dfEnabled() {
		s.bestEffort(errFrame(f.ReqID, "not_permitted", 403))
		return
	}
	s.streamCommand(ctx, f.ReqID, []string{"df", "-hPT"}, 0)
}

// netListenJob serves net.listen: listening sockets + owning process (Linux ss).
func (s *session) netListenJob(ctx context.Context, f reqFrame) {
	if !s.cfg.portsEnabled() {
		s.bestEffort(errFrame(f.ReqID, "not_permitted", 403))
		return
	}
	s.streamCommand(ctx, f.ReqID, []string{"ss", "-tulnp"}, 500)
}

// streamCommand runs a fixed argv (no shell) and streams its stdout lines back
// as redacted res frames. maxLines==0 means unbounded — the source or the follow
// timeout bounds it instead. (#337)
func (s *session) streamCommand(ctx context.Context, reqID string, argv []string, maxLines int) {
	// CommandContext kills the process when ctx is cancelled (cancel frame, the
	// follow timeout, or socket close), which ends the read below.
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	pr, pw := io.Pipe()
	cmd.Stdout = pw
	cmd.Stderr = pw // docker/journald write to both; merge so nothing is lost
	if err := cmd.Start(); err != nil {
		s.bestEffort(errFrame(reqID, "spawn_failed", 500))
		return
	}
	go func() { _ = cmd.Wait(); _ = pw.Close() }()

	scanner := bufio.NewScanner(pr)
	scanner.Buffer(make([]byte, 0, 64*1024), lineBufferBytes)
	seq := 0
	for scanner.Scan() {
		if maxLines > 0 && seq >= maxLines {
			break
		}
		if err := s.write(ctx, resFrame(reqID, seq, redact(scanner.Text()))); err != nil {
			break // socket gone — stop; the deferred cancel kills the process
		}
		seq++
	}
	s.bestEffort(eofFrame(reqID))
}

// --- frame writing -------------------------------------------------------

func (s *session) write(ctx context.Context, frame any) error {
	b, err := json.Marshal(frame)
	if err != nil {
		return err
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return s.conn.Write(ctx, websocket.MessageText, b)
}

// bestEffort delivers a terminal frame even if the request ctx is already done
// (cancel/timeout), so the requester always sees an eof/error.
func (s *session) bestEffort(frame any) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = s.write(ctx, frame)
}

func resFrame(reqID string, seq int, data string) map[string]any {
	return map[string]any{"type": "res", "reqId": reqID, "seq": seq, "data": data}
}
func eofFrame(reqID string) map[string]any {
	return map[string]any{"type": "res", "reqId": reqID, "eof": true}
}
func errFrame(reqID, errMsg string, code int) map[string]any {
	return map[string]any{"type": "res", "reqId": reqID, "error": errMsg, "code": code}
}

// --- stream registry -----------------------------------------------------

func (s *session) addStream(id string, cancel context.CancelFunc) {
	s.mu.Lock()
	s.streams[id] = cancel
	s.mu.Unlock()
}
func (s *session) removeStream(id string) {
	s.mu.Lock()
	delete(s.streams, id)
	s.mu.Unlock()
}
func (s *session) cancelStream(id string) {
	s.mu.Lock()
	cancel := s.streams[id]
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// --- helpers -------------------------------------------------------------

// logArgs builds the argv for a source. argv-style, no shell — the logical
// target was already validated against the allow-list. (#337)
func logArgs(src logSource, lines int, follow bool) ([]string, error) {
	n := strconv.Itoa(lines)
	switch src.Type {
	case "docker":
		if src.Container == "" {
			return nil, fmt.Errorf("bad_source")
		}
		a := []string{"docker", "logs", "--tail", n}
		if follow {
			a = append(a, "-f")
		}
		return append(a, src.Container), nil
	case "file":
		if src.Path == "" {
			return nil, fmt.Errorf("bad_source")
		}
		a := []string{"tail", "-n", n}
		if follow {
			a = append(a, "-F")
		}
		return append(a, src.Path), nil
	case "journald":
		if src.Unit == "" {
			return nil, fmt.Errorf("bad_source")
		}
		a := []string{"journalctl", "--no-pager", "-n", n, "-u", src.Unit}
		if follow {
			a = append(a, "-f")
		}
		return a, nil
	default:
		return nil, fmt.Errorf("bad_source_type")
	}
}

func argInt(args map[string]any, key string, def int) int {
	if args == nil {
		return def
	}
	if v, ok := args[key]; ok {
		if f, ok := v.(float64); ok { // JSON numbers decode to float64
			return int(f)
		}
	}
	return def
}

func argBool(args map[string]any, key string, def bool) bool {
	if args == nil {
		return def
	}
	if v, ok := args[key]; ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return def
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// redact masks obvious secrets before a log line leaves the host. Belt-and-
// suspenders: ICLIC redacts again on relay. Starting pattern set — extend as
// real-world leaks surface. (#337)
var secretLine = regexp.MustCompile(`(?i)(password|passwd|pwd|secret|token|api[_-]?key|authorization|bearer)(["']?\s*[:=]\s*["']?)([^\s"',]+)`)

func redact(line string) string {
	return secretLine.ReplaceAllString(line, "${1}${2}***")
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
