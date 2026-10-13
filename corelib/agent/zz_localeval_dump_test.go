package agent

// LOCAL EVAL HARNESS (local branch only, not for upstream): dumps the real
// first-party tool definitions as the model would see them.

import (
	"encoding/json"
	"os"
	"testing"
)

func TestZZDumpCoreToolDefs(t *testing.T) {
	out := os.Getenv("ZZ_OUT")
	if out == "" {
		t.Skip("ZZ_OUT not set")
	}
	r := NewCoreToolRegistry()
	RegisterCoreTools(r, CoreToolDeps{})
	defs := r.BuildDefinitions()
	b, _ := json.MarshalIndent(defs, "", " ")
	if err := os.WriteFile(out, b, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Logf("wrote %d defs", len(defs))
}
