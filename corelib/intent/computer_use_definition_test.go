package intent

import "testing"

// TestComputerUseDefinitionRegistered guards the Computer Use intent
// definition: it must stay registered, must not trigger workflows, and its
// tool affinity must cover the full computer_* tool surface.
func TestComputerUseDefinitionRegistered(t *testing.T) {
	defs := DefaultDefinitions()
	var cu *IntentDefinition
	for i := range defs {
		if defs[i].Label == LabelComputerUse {
			cu = &defs[i]
			break
		}
	}
	if cu == nil {
		t.Fatal("DefaultDefinitions missing LabelComputerUse")
	}
	if cu.MayTriggerWorkflow {
		t.Fatal("LabelComputerUse must not trigger workflows")
	}
	if cu.TreeText == "" {
		t.Fatal("TreeText required for L3 tree reasoning")
	}
	if len(cu.EmbedTexts) < 10 {
		t.Fatalf("want >=10 embed exemplars, got %d", len(cu.EmbedTexts))
	}
	if !LabelComputerUse.IsValid() {
		t.Fatal("LabelComputerUse not in AllLabels")
	}

	wantTools := []string{
		"computer_observe", "computer_click", "computer_type", "computer_key",
		"computer_scroll", "computer_wait", "computer_focus", "computer_done",
		"computer_playbook",
	}
	got := make(map[string]bool, len(cu.ToolNames))
	for _, n := range cu.ToolNames {
		got[n] = true
	}
	if len(got) != len(wantTools) {
		t.Fatalf("ToolNames = %v, want %d entries", cu.ToolNames, len(wantTools))
	}
	for _, w := range wantTools {
		if !got[w] {
			t.Fatalf("ToolNames missing %q", w)
		}
	}

	// Anchors and tool affinity auto-derive from the definition.
	anchored := false
	for _, a := range BuildAnchorsFromDefinitions(defs) {
		if a.Label == LabelComputerUse && len(a.Texts) > 0 {
			anchored = true
		}
	}
	if !anchored {
		t.Fatal("anchors missing LabelComputerUse")
	}
	affinity := NewToolAffinityRegistryFromDefinitions(defs)
	if resolved := affinity.Resolve(LabelComputerUse, nil); len(resolved) == 0 {
		t.Fatal("tool affinity does not resolve LabelComputerUse")
	}
}
