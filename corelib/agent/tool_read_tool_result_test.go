package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/toolresult"
)

func TestToolReadToolResult_ByPath(t *testing.T) {
	dir := t.TempDir()
	raw := strings.Repeat("alpha ", 500)
	proj, err := toolresult.Project(toolresult.ProjectOptions{
		ToolName:      "bash",
		SessionKey:    "u1",
		Content:       raw,
		Preview:       toolresult.DefaultPreview(raw, 100),
		Root:          dir,
		MinSpillBytes: 1,
		ForceSpill:    true,
	})
	if err != nil || proj.Handle == nil {
		t.Fatalf("spill: %v %+v", err, proj)
	}

	// Tool uses Resolve with default root — temporarily point by path only.
	// Read via path is validated under Root, so we pass path after writing under
	// a custom root and use ToolReadToolResult which defaults to ToolResultsDir.
	// Exercise handler through package Read by injecting path that is under dir,
	// and call toolresult.Read directly for the store; for the tool handler we
	// write into the process ToolResultsDir is not controllable easily — so test
	// handler with path under a temp store by using ReadOptions via ToolReadToolResult
	// after copying into a subdir of the real store is heavy.
	// Instead: unit-test handler arg parsing with a path under a fake store by
	// writing file and using Resolve path with ToolResultsDir override via
	// environment is unavailable. Test handler logic via toolresult.Read + thin
	// wrapper contract: empty args error, and success path with os.Write under
	// maclawpath would require maclawpath override.

	// Validate missing args.
	if msg := ToolReadToolResult(map[string]interface{}{}); !strings.Contains(msg, "requires id") {
		t.Fatalf("missing args: %s", msg)
	}

	// Success path: write under temporary root and call package-level Read via
	// the same options the tool would use (handler uses default root). Copy
	// spilled file into a path we can pass — but default root check fails.
	// So call toolresult.Read with Root=dir and compare FormatReadResult.
	res, err := toolresult.Read(toolresult.ReadOptions{
		ID:         proj.Handle.ID,
		SessionKey: "u1",
		Root:       dir,
		Limit:      32,
	})
	if err != nil {
		t.Fatal(err)
	}
	out := toolresult.FormatReadResult(res)
	if !strings.Contains(out, "[tool_result_read]") || !strings.Contains(out, "alpha") {
		t.Fatalf("out=%q", out)
	}

	// Handler with path outside store
	outside := filepath.Join(t.TempDir(), "x.txt")
	_ = os.WriteFile(outside, []byte("x"), 0o600)
	if msg := ToolReadToolResult(map[string]interface{}{"path": outside}); !strings.Contains(msg, "error:") {
		t.Fatalf("expected security error, got %s", msg)
	}
}

func TestRegisterCoreTools_IncludesReadToolResult(t *testing.T) {
	reg := NewCoreToolRegistry()
	RegisterCoreTools(reg, CoreToolDeps{})
	found := false
	for _, def := range reg.BuildDefinitions() {
		fn, _ := def["function"].(map[string]interface{})
		if fn == nil {
			continue
		}
		if name, _ := fn["name"].(string); name == "read_tool_result" {
			found = true
			parameters, _ := fn["parameters"].(map[string]interface{})
			properties, _ := parameters["properties"].(map[string]interface{})
			if _, exposed := properties["session_key"]; exposed {
				t.Fatal("read_tool_result exposed host-owned session_key to the model")
			}
			break
		}
	}
	if !found {
		t.Fatal("read_tool_result not registered")
	}
	// Execute missing args via registry
	msg := reg.Execute("read_tool_result", map[string]interface{}{})
	if !strings.Contains(msg, "requires id") {
		t.Fatalf("execute: %s", msg)
	}
}
