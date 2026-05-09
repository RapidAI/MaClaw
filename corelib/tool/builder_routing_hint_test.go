package tool

import (
	"testing"
	"time"
)

func TestDynamicToolBuilderAppliesRoutingHintAdjustment(t *testing.T) {
	reg := NewRegistry()
	reg.Register(RegisteredTool{Name: "preferred_tool", Description: "generic browser button helper", Category: CategoryNonCode})
	reg.Register(RegisteredTool{Name: "avoided_tool", Description: "generic browser button helper", Category: CategoryNonCode})

	tracker, _ := NewUsageTracker("")
	now := time.Now()
	for i := 0; i < 6; i++ {
		tracker.RecordExperience(ToolExperience{
			ToolName:     "avoided_tool",
			QueryTokens:  []string{"browser", "button"},
			Success:      false,
			RecoveryTool: "preferred_tool",
			Timestamp:    now,
		})
	}

	builder := NewDynamicToolBuilder(reg)
	builder.maxDirectTools = 0
	builder.maxDynamic = 1
	builder.SetUsageTracker(tracker)

	result := builder.Build("browser button")
	if len(result) != 1 {
		t.Fatalf("Build returned %d tools, want 1: %#v", len(result), result)
	}
	if got := ExtractToolName(result[0]); got != "preferred_tool" {
		t.Fatalf("routing hint should select preferred recovery tool, got %q", got)
	}
}
