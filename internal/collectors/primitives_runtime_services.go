package collectors

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"
)

const runtimeDefaultTimeout = 4 * time.Second

// runtimeInfoMaxBytes caps the raw info document embedded in a runtime signal's
// payload. Larger documents (e.g. an OpenAPI spec served as the version source)
// are summarised out so the heartbeat stays small and clears the ICLIC WAF. (#52)
const runtimeInfoMaxBytes = 4096

// runtimeServices converts an operator-owned service registry into ICLIC
// runtime_instance signals. It never drops a configured service just because
// the probe failed; failed probes become STALE rows so Fleet shows a broken
// service instead of a missing one. (#112)
func runtimeServices(ctx context.Context, args map[string]any) (any, error) {
	services, err := runtimeServiceConfigs(args["services"])
	if err != nil {
		return nil, err
	}
	if len(services) == 0 {
		return []runtimeSignal{}, nil
	}
	socket := argString(args, "socket", dockerDefaultSocket)
	timeout := time.Duration(argFloat(args, "timeout_sec", 4) * float64(time.Second))
	if timeout <= 0 {
		timeout = runtimeDefaultTimeout
	}

	out := make([]runtimeSignal, len(services))
	var wg sync.WaitGroup
	for i, svc := range services {
		i, svc := i, svc
		wg.Add(1)
		go func() {
			defer wg.Done()
			out[i] = probeRuntimeService(ctx, socket, timeout, svc)
		}()
	}
	wg.Wait()
	return out, nil
}

type runtimeServiceConfig struct {
	ProductCode    string
	ComponentCode  string
	Name           string
	Container      string
	HealthURL      string
	InfoURL        string
	Environment    string
	InstanceKey    string
	Probe          string
	VersionPath    string
	GitCommitPath  string
	ExpectedStatus string
}

type runtimeSignal struct {
	ProductCode    string         `json:"productCode,omitempty"`
	ComponentCode  string         `json:"componentCode,omitempty"`
	InstanceKey    string         `json:"instanceKey,omitempty"`
	Environment    string         `json:"environment,omitempty"`
	Status         string         `json:"status,omitempty"`
	VersionSource  string         `json:"versionSource,omitempty"`
	RunningVersion string         `json:"runningVersion,omitempty"`
	GitCommit      string         `json:"gitCommit,omitempty"`
	Notes          string         `json:"notes,omitempty"`
	Payload        map[string]any `json:"payload,omitempty"`
}

func probeRuntimeService(ctx context.Context, socket string, timeout time.Duration,
	svc runtimeServiceConfig) runtimeSignal {
	payload := map[string]any{
		"source":    "host-agent-runtime-services",
		"name":      svc.Name,
		"container": svc.Container,
		"probe":     firstNonBlank(svc.Probe, "http"),
		"checkedAt": time.Now().UTC().Format(time.RFC3339),
	}
	signal := runtimeSignal{
		ProductCode:   svc.ProductCode,
		ComponentCode: svc.ComponentCode,
		InstanceKey:   firstNonBlank(svc.InstanceKey, svc.Container, svc.ComponentCode),
		Environment:   svc.Environment,
		VersionSource: "HOST_AGENT",
		Status:        "STALE",
		Payload:       payload,
	}

	if svc.Container != "" {
		state, err := dockerContainerState(ctx, socket, svc.Container, timeout)
		if err != nil {
			signal.Notes = err.Error()
			payload["error"] = err.Error()
			return signal
		}
		payload["containerState"] = state
		if state != "running" {
			signal.Notes = "container is " + state
			return signal
		}
	}

	health, statusCode, err := runtimeJSONProbe(ctx, svc, svc.HealthURL, timeout)
	payload["healthHttpStatus"] = statusCode
	if err != nil {
		signal.Notes = err.Error()
		payload["error"] = err.Error()
		return signal
	}
	payload["health"] = health
	expected := firstNonBlank(svc.ExpectedStatus, "UP")
	actual := fmt.Sprint(jsonPath(health, "status"))
	if actual != expected {
		signal.Notes = "actuator health status is " + actual
		return signal
	}

	if svc.InfoURL != "" {
		info, infoStatus, infoErr := runtimeJSONProbe(ctx, svc, svc.InfoURL, timeout)
		payload["infoHttpStatus"] = infoStatus
		if infoErr == nil {
			// Only embed the raw info document when it is small. Some services
			// (e.g. the AI Gateway, whose version lives in /openapi.json) return
			// a large spec; embedding it bloats every heartbeat and trips the
			// ICLIC WAF on the runtime-instances POST (HTTP 400). The version and
			// commit we actually need are extracted into dedicated fields below,
			// so capping the raw embed loses nothing operationally. (#52)
			if embedded, ok := infoWithinEmbedCap(info); ok {
				payload["info"] = embedded
			} else {
				payload["infoOmitted"] = "info document exceeds embed cap"
			}
			signal.RunningVersion = stringAtPath(info, svc.VersionPath, "app.version", "build.version")
			signal.GitCommit = stringAtPath(info, svc.GitCommitPath, "git.commit.id")
		} else {
			payload["infoError"] = infoErr.Error()
		}
	}

	signal.Status = "HEALTHY"
	return signal
}

// infoWithinEmbedCap returns the info document and true when it is small enough
// to embed in a heartbeat payload, or nil/false when it serialises to more than
// runtimeInfoMaxBytes (see that constant for the WAF rationale). (#52)
func infoWithinEmbedCap(info any) (any, bool) {
	raw, err := json.Marshal(info)
	if err != nil || len(raw) > runtimeInfoMaxBytes {
		return nil, false
	}
	return info, true
}

func dockerContainerState(ctx context.Context, socket, container string, timeout time.Duration) (string, error) {
	var inspect struct {
		State struct {
			Status string `json:"Status"`
		} `json:"State"`
	}
	if err := dockerGet(ctx, socket, "/containers/"+container+"/json", timeout, &inspect); err != nil {
		return "missing", err
	}
	if inspect.State.Status == "" {
		return "unknown", nil
	}
	return inspect.State.Status, nil
}

func runtimeJSONProbe(ctx context.Context, svc runtimeServiceConfig, url string,
	timeout time.Duration) (any, int, error) {
	if svc.Probe == "docker_exec" {
		return runtimeJSONDockerExec(ctx, svc.Container, url, timeout)
	}
	return runtimeJSONGet(ctx, url, timeout)
}

func runtimeJSONDockerExec(ctx context.Context, container, url string, timeout time.Duration) (any, int, error) {
	if container == "" {
		return nil, 0, fmt.Errorf("container: required for docker_exec probe")
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	out, err := exec.CommandContext(cctx, "docker", "exec", container, "wget", "-qO-", url).Output()
	if err != nil {
		return nil, 0, fmt.Errorf("docker exec %s: %w", container, err)
	}
	var doc any
	if err := json.Unmarshal(out, &doc); err != nil {
		return nil, 200, err
	}
	return doc, 200, nil
}

func runtimeJSONGet(ctx context.Context, url string, timeout time.Duration) (any, int, error) {
	if url == "" {
		return nil, 0, fmt.Errorf("url: required")
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("User-Agent", "iclic-host-agent runtime probe")
	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, resp.StatusCode, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var doc any
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&doc); err != nil {
		return nil, resp.StatusCode, err
	}
	return doc, resp.StatusCode, nil
}

func runtimeServiceConfigs(raw any) ([]runtimeServiceConfig, error) {
	items, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("services: required list")
	}
	out := make([]runtimeServiceConfig, 0, len(items))
	for i, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("services[%d]: must be map", i)
		}
		svc := runtimeServiceConfig{
			ProductCode:    argString(m, "product_code", ""),
			ComponentCode:  argString(m, "component_code", ""),
			Name:           argString(m, "name", ""),
			Container:      argString(m, "container", ""),
			HealthURL:      argString(m, "health_url", ""),
			InfoURL:        argString(m, "info_url", ""),
			Environment:    argString(m, "environment", ""),
			InstanceKey:    argString(m, "instance_key", ""),
			Probe:          argString(m, "probe", ""),
			VersionPath:    argString(m, "version_path", ""),
			GitCommitPath:  argString(m, "git_commit_path", ""),
			ExpectedStatus: argString(m, "expected_status", ""),
		}
		if svc.ProductCode == "" || svc.ComponentCode == "" || svc.HealthURL == "" {
			return nil, fmt.Errorf("services[%d]: product_code, component_code and health_url are required", i)
		}
		out = append(out, svc)
	}
	return out, nil
}

func stringAtPath(doc any, paths ...string) string {
	for _, path := range paths {
		if path == "" {
			continue
		}
		if value := jsonPath(doc, path); value != nil {
			s := strings.TrimSpace(fmt.Sprint(value))
			if s != "" && s != "<nil>" {
				return s
			}
		}
	}
	return ""
}

func firstNonBlank(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
