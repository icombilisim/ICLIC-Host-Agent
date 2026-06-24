package collectors

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// PrimitiveFunc is the contract every built-in collector implements. Args is
// the binding's `args:` block from YAML; the return value is whatever the
// primitive produces (number, string, list, map). An error skips this
// binding's metric for the current tick — no panic, no whole-payload abort.
type PrimitiveFunc func(ctx context.Context, args map[string]any) (any, error)

// Run executes every binding concurrently, each capped by perBindingTimeout,
// and merges successful results into a single map keyed by Binding.OutputKey.
// Failed primitives are logged at WARN; the metric is omitted so the backend
// `buildSummary` falls back gracefully on missing keys.
func Run(ctx context.Context, bindings []Binding, registry map[string]PrimitiveFunc,
	perBindingTimeout time.Duration) map[string]any {

	out := make(map[string]any, len(bindings))
	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, b := range bindings {
		if b.OutputKey == "" || b.Primitive == "" {
			slog.Warn("skipping binding with missing primitive or output_key", "id", b.ID)
			continue
		}
		prim, ok := registry[b.Primitive]
		if !ok {
			slog.Warn("unknown primitive", "binding", b.ID, "primitive", b.Primitive)
			continue
		}

		wg.Add(1)
		go func(b Binding, prim PrimitiveFunc) {
			defer wg.Done()
			bctx, cancel := context.WithTimeout(ctx, perBindingTimeout)
			defer cancel()
			v, err := prim(bctx, b.Args)
			if err != nil {
				slog.Warn("collector failed",
					"binding", b.ID,
					"primitive", b.Primitive,
					"err", err)
				return
			}
			mu.Lock()
			out[b.OutputKey] = mergeBindingValue(out[b.OutputKey], v)
			mu.Unlock()
		}(b, prim)
	}

	wg.Wait()
	return out
}

// mergeBindingValue combines a new binding result with whatever is already
// stored under the same output_key. Multiple runtime.services bindings (the
// icosys profile plus a per-host AI Gateway profile) deliberately share the
// `runtime_instances` key, and the heartbeat sender reads that single key — so
// their []runtimeSignal slices must be concatenated, not overwritten, or every
// binding but the last silently vanishes from the runtime signal feed. Any
// other type keeps last-writer-wins, preserving the prior map semantics. (#49)
func mergeBindingValue(existing, incoming any) any {
	if existing == nil {
		return incoming
	}
	if prev, ok := existing.([]runtimeSignal); ok {
		if next, ok := incoming.([]runtimeSignal); ok {
			return append(prev, next...)
		}
	}
	return incoming
}
