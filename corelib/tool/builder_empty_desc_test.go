package tool

import (
	"testing"
)

// TestBuildAll_SkipsEmptyDescription verifies that BuildAll() does not
// include tools with empty descriptions (backward-compat aliases).
func TestBuildAll_SkipsEmptyDescription(t *testing.T) {
	reg := NewRegistry()
	reg.Register(RegisteredTool{Name: "real_tool", Description: "A real tool", Category: CategoryBuiltin})
	reg.Register(RegisteredTool{Name: "alias_tool", Description: "", Category: CategoryBuiltin})
	reg.Register(RegisteredTool{Name: "another_real", Description: "Another real tool", Category: CategoryNonCode})

	builder := NewDynamicToolBuilder(reg)
	result := builder.BuildAll()

	names := make(map[string]bool)
	for _, def := range result {
		name := ExtractToolName(def)
		names[name] = true
		desc := ExtractToolDescription(def)
		if desc == "" {
			t.Errorf("tool %q has empty description in BuildAll output", name)
		}
	}

	if !names["real_tool"] {
		t.Error("real_tool should be in BuildAll output")
	}
	if !names["another_real"] {
		t.Error("another_real should be in BuildAll output")
	}
	if names["alias_tool"] {
		t.Error("alias_tool (empty description) should NOT be in BuildAll output")
	}
}

// TestBuild_SkipsEmptyDescription verifies that Build() does not include
// tools with empty descriptions when tool count exceeds threshold.
func TestBuild_SkipsEmptyDescription(t *testing.T) {
	reg := NewRegistry()
	// Register enough tools to trigger the scoring path (> maxDirectTools=20).
	for i := 0; i < 25; i++ {
		reg.Register(RegisteredTool{
			Name:        "tool_" + string(rune('a'+i)),
			Description: "tool description",
			Category:    CategoryBuiltin,
		})
	}
	// Add an empty-description alias.
	reg.Register(RegisteredTool{Name: "legacy_alias", Description: "", Category: CategoryBuiltin})

	builder := NewDynamicToolBuilder(reg)
	result := builder.Build("test query")

	for _, def := range result {
		name := ExtractToolName(def)
		if name == "legacy_alias" {
			t.Error("legacy_alias (empty description) should NOT be in Build output")
		}
		desc := ExtractToolDescription(def)
		if desc == "" {
			t.Errorf("tool %q has empty description in Build output", name)
		}
	}
}

// TestBuild_SmallSet_SkipsEmptyDescription verifies that Build() skips
// empty-description tools even when the total count is below the threshold
// (the fast path that returns all tools without scoring).
func TestBuild_SmallSet_SkipsEmptyDescription(t *testing.T) {
	reg := NewRegistry()
	reg.Register(RegisteredTool{Name: "real_tool", Description: "A real tool", Category: CategoryBuiltin})
	reg.Register(RegisteredTool{Name: "alias_tool", Description: "", Category: CategoryBuiltin})

	builder := NewDynamicToolBuilder(reg)
	result := builder.Build("test query")

	for _, def := range result {
		name := ExtractToolName(def)
		if name == "alias_tool" {
			t.Error("alias_tool (empty description) should NOT be in Build output")
		}
	}
	if len(result) != 1 {
		t.Errorf("expected 1 tool in output, got %d", len(result))
	}
}
