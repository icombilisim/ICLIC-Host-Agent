package collectors

import (
	"context"
	"testing"
	"time"
)

// Two runtime.services bindings sharing output_key `runtime_instances` (the
// icosys profile + a per-host AI Gateway profile) must have their signals
// concatenated, not overwritten — otherwise only the last binding's services
// reach the heartbeat. (#49)
func TestRunMergesRuntimeServiceSignals(t *testing.T) {
	registry := map[string]PrimitiveFunc{
		"runtime.services": func(_ context.Context, args map[string]any) (any, error) {
			return []runtimeSignal{{ComponentCode: args["code"].(string)}}, nil
		},
	}
	bindings := []Binding{
		{ID: "icosys", Primitive: "runtime.services", OutputKey: "runtime_instances",
			Args: map[string]any{"code": "icglb-services"}},
		{ID: "aigw", Primitive: "runtime.services", OutputKey: "runtime_instances",
			Args: map[string]any{"code": "aigateway"}},
	}

	out := Run(context.Background(), bindings, registry, time.Second)

	signals, ok := out["runtime_instances"].([]runtimeSignal)
	if !ok {
		t.Fatalf("runtime_instances is %T, want []runtimeSignal", out["runtime_instances"])
	}
	if len(signals) != 2 {
		t.Fatalf("merged signal count = %d, want 2 (both bindings)", len(signals))
	}
	got := map[string]bool{}
	for _, s := range signals {
		got[s.ComponentCode] = true
	}
	if !got["icglb-services"] || !got["aigateway"] {
		t.Fatalf("merged signals missing a binding: %+v", signals)
	}
}

// Non-slice values under a colliding output_key keep last-writer-wins, the
// pre-existing map semantics. (#49)
func TestRunScalarCollisionKeepsLast(t *testing.T) {
	if got := mergeBindingValue("first", "second"); got != "second" {
		t.Fatalf("scalar collision = %v, want last-writer-wins (second)", got)
	}
	if got := mergeBindingValue(nil, "only"); got != "only" {
		t.Fatalf("nil existing = %v, want incoming", got)
	}
}
