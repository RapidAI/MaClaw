package main

import (
	"testing"
)

// TestBuildAll_ExcludesEmptyDescriptionTools verifies the end-to-end contract:
// tools registered with Description="" in the gui ToolRegistry must NOT appear
// in BuildAll() output.
//
// This is the mechanism that makes browser tool merging and backward-compat
// skill aliases work:
//   - registerBrowserTools sets Description="" on individual browser_* tools;
//     only the merged "browser" tool has a real description.
//   - registerBuiltinTools sets Description="" on legacy aliases (run_skill, etc.);
//     only "manage_skill" has a real description.
//
// guiRegistryToCorelib must preserve empty descriptions so that
// DynamicToolBuilder.BuildAll() (which skips Description=="") filters them out.
// If guiRegistryToCorelib replaces "" with a fallback, these tools leak into
// the LLM tool list, bloating token usage and triggering "Tool names must be
// unique" errors on strict providers like DeepSeek.
func TestBuildAll_ExcludesEmptyDescriptionTools(t *testing.T) {
	r := NewToolRegistry()
	// Merged tool — should appear in output.
	r.Register(RegisteredTool{
		Name: "browser", Description: "浏览器自动化", Status: RegToolAvailable,
	})
	// Individual browser tool — Description="" means dispatch-only, must NOT appear.
	r.Register(RegisteredTool{
		Name: "browser_navigate", Description: "", Status: RegToolAvailable,
	})
	// Backward-compat alias — Description="" means dispatch-only, must NOT appear.
	r.Register(RegisteredTool{
		Name: "run_skill", Description: "", Status: RegToolAvailable,
	})
	// Real tool — should appear in output.
	r.Register(RegisteredTool{
		Name: "generate_pdf", Description: "生成 PDF", Status: RegToolAvailable,
	})
	// Legacy dynamic gateway — even with a description it is a managed-surface
	// capability only and must never become legacy model authority
	// (tool.IsLegacyModelDynamicGateway).
	r.Register(RegisteredTool{
		Name: "manage_skill", Description: "Skill 管理", Status: RegToolAvailable,
	})

	b := NewDynamicToolBuilder(r)
	defs := b.BuildAll()

	names := make(map[string]bool, len(defs))
	for _, d := range defs {
		fn := d["function"].(map[string]interface{})
		names[fn["name"].(string)] = true
	}

	if !names["browser"] {
		t.Error("expected 'browser' (has description) in BuildAll output")
	}
	if !names["generate_pdf"] {
		t.Error("expected 'generate_pdf' (has description) in BuildAll output")
	}
	if names["manage_skill"] {
		t.Error("'manage_skill' is a legacy dynamic gateway and must NOT appear in BuildAll output")
	}
	if names["browser_navigate"] {
		t.Error("'browser_navigate' (empty description) must NOT appear in BuildAll output")
	}
	if names["run_skill"] {
		t.Error("'run_skill' (empty description) must NOT appear in BuildAll output")
	}
	if len(defs) != 2 {
		t.Errorf("BuildAll returned %d tools, want 2", len(defs))
	}
}
