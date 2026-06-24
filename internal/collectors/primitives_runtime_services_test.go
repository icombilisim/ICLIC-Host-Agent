package collectors

import (
	"fmt"
	"testing"
)

// A small info document (e.g. Spring actuator /info) embeds; a large one (e.g.
// an OpenAPI spec used as the version source) is omitted so the heartbeat stays
// under the ICLIC WAF's limit. (#52)
func TestInfoWithinEmbedCap(t *testing.T) {
	small := map[string]any{"app": map[string]any{"version": "1.2.3"}, "git": map[string]any{"commit": map[string]any{"id": "abc1234"}}}
	if _, ok := infoWithinEmbedCap(small); !ok {
		t.Fatalf("small info document should be embedded")
	}

	// Build a document well over runtimeInfoMaxBytes, like a real OpenAPI spec.
	big := map[string]any{}
	for i := 0; i < 500; i++ {
		big[fmt.Sprintf("path_%d", i)] = "a-fairly-long-value-standing-in-for-schema-text"
	}
	if _, ok := infoWithinEmbedCap(big); ok {
		t.Fatalf("info document over %d bytes should be omitted", runtimeInfoMaxBytes)
	}
}
