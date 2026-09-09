package agentruntime

import "testing"

func TestCapabilityToolsFromOpenAISortsAndProjects(t *testing.T) {
	got := CapabilityToolsFromOpenAI([]map[string]interface{}{
		{"type": "function", "function": map[string]interface{}{"name": " z ", "description": "Z", "parameters": map[string]interface{}{"type": "object"}}},
		{"type": "function", "function": map[string]interface{}{"name": "a", "description": "A"}},
		{"type": "function", "function": map[string]interface{}{"name": ""}},
	})
	if len(got) != 2 || got[0].Name != "a" || got[1].Name != "z" {
		t.Fatalf("unexpected projection: %#v", got)
	}
	if got[1].Parameters["type"] != "object" || !got[0].Enabled {
		t.Fatalf("projection lost fields: %#v", got)
	}
}

func TestCapabilityToolsFromSpecsPreservesDisabledReasonAndSorts(t *testing.T) {
	got := CapabilityToolsFromSpecs([]CapabilityToolInput{
		{Name: " zed ", Enabled: false, DisabledReason: "headless"},
		{Name: "alpha", Enabled: true},
		{Name: "", Enabled: true},
	})
	if len(got) != 2 || got[0].Name != "alpha" || got[1].Name != "zed" {
		t.Fatalf("projected tools = %#v", got)
	}
	if got[1].Enabled || got[1].DisabledReason != "headless" {
		t.Fatalf("disabled projection = %#v", got[1])
	}
}

func TestMergeCapabilityToolsLetsModulesOverrideHostCopies(t *testing.T) {
	merged := MergeCapabilityTools(
		[]CapabilityTool{{Name: "screenshot", Description: "host", Enabled: true}, {Name: "bash", Enabled: true}},
		[]CapabilityTool{{Name: "screenshot", Description: "runtime", Enabled: false, DisabledReason: "capability_unavailable"}},
	)
	if len(merged) != 2 || merged[0].Name != "bash" || merged[1].Name != "screenshot" {
		t.Fatalf("merged = %#v", merged)
	}
	if merged[1].Description != "runtime" || merged[1].Enabled {
		t.Fatalf("module overlay lost: %#v", merged[1])
	}
}
