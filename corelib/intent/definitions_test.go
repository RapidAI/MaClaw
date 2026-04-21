package intent

import (
	"testing"
)

func TestDefaultDefinitions_AllLabelsHaveTreeText(t *testing.T) {
	defs := DefaultDefinitions()
	for _, def := range defs {
		if def.TreeText == "" {
			t.Errorf("definition for %s has empty TreeText", def.Label)
		}
	}
}

func TestDefaultDefinitions_AllLabelsHaveDomain(t *testing.T) {
	defs := DefaultDefinitions()
	for _, def := range defs {
		if def.Domain == "" {
			t.Errorf("definition for %s has empty Domain", def.Label)
		}
	}
}

func TestDefaultDefinitions_CoverAllNonSpecialLabels(t *testing.T) {
	defs := DefaultDefinitions()
	covered := make(map[IntentLabel]bool)
	for _, def := range defs {
		covered[def.Label] = true
	}

	for _, label := range AllLabels() {
		if !covered[label] {
			t.Errorf("label %s not covered by DefaultDefinitions", label)
		}
	}
}

func TestBuildAnchorsFromDefinitions(t *testing.T) {
	defs := DefaultDefinitions()
	anchors := BuildAnchorsFromDefinitions(defs)

	// Should have anchors for all labels with EmbedTexts (not ambiguous/unknown).
	if len(anchors) < 8 {
		t.Errorf("expected at least 8 anchor sets, got %d", len(anchors))
	}

	// Verify no empty anchor sets.
	for _, a := range anchors {
		if len(a.Texts) == 0 {
			t.Errorf("anchor for %s has no texts", a.Label)
		}
	}
}

func TestBuildToolAffinityFromDefinitions(t *testing.T) {
	defs := DefaultDefinitions()
	mapping := BuildToolAffinityFromDefinitions(defs)

	// SSH should map to "ssh" tool.
	sshTools := mapping[LabelSSH]
	found := false
	for _, name := range sshTools {
		if name == "ssh" {
			found = true
			break
		}
	}
	if !found {
		t.Error("SSH label should map to 'ssh' tool")
	}

	// Browser should have many tools.
	browserTools := mapping[LabelBrowser]
	if len(browserTools) < 10 {
		t.Errorf("expected browser to have 10+ tools, got %d", len(browserTools))
	}
}

func TestBuildIntentTreeFromDefinitions(t *testing.T) {
	defs := DefaultDefinitions()
	tree := BuildIntentTreeText(defs)

	// Should contain domain headers.
	if tree == "" {
		t.Fatal("tree text should not be empty")
	}

	// Should contain at least coding and ssh entries.
	if !containsSubstring(tree, "coding:") {
		t.Error("tree should contain coding entry")
	}
	if !containsSubstring(tree, "ssh:") {
		t.Error("tree should contain ssh entry")
	}
}

// TestFullDefinitions_KeywordsPopulated verifies that FullDefinitions()
// returns definitions with Keywords populated from defaultKeywords.
func TestFullDefinitions_KeywordsPopulated(t *testing.T) {
	defs := FullDefinitions()

	// SSH should have many keywords (40+ in defaultKeywords).
	for _, def := range defs {
		if def.Label == LabelSSH {
			if len(def.Keywords) < 30 {
				t.Errorf("SSH should have 30+ keywords, got %d", len(def.Keywords))
			}
			return
		}
	}
	t.Error("SSH definition not found")
}

// TestFullDefinitions_RoundTrip verifies that building a KeywordRegistry
// from FullDefinitions produces the same entries as NewKeywordRegistry.
func TestFullDefinitions_RoundTrip(t *testing.T) {
	original := NewKeywordRegistry()
	fromDefs := NewKeywordRegistryFromDefinitions(FullDefinitions())

	if len(original.entries) != len(fromDefs.entries) {
		t.Errorf("entry count mismatch: original=%d, fromDefs=%d",
			len(original.entries), len(fromDefs.entries))
	}

	// Verify same strong index.
	for kw, label := range original.strongIndex {
		if fromDefs.strongIndex[kw] != label {
			t.Errorf("strong index mismatch for %q: original=%s, fromDefs=%s",
				kw, label, fromDefs.strongIndex[kw])
		}
	}
}

// TestFullDefinitions_ToolAffinityRoundTrip verifies that building a
// ToolAffinityRegistry from FullDefinitions produces equivalent mappings.
func TestFullDefinitions_ToolAffinityRoundTrip(t *testing.T) {
	original := NewToolAffinityRegistry()
	fromDefs := NewToolAffinityRegistryFromDefinitions(FullDefinitions())

	// Check SSH tools.
	origSSH := original.ToolsFor(LabelSSH)
	defsSSH := fromDefs.ToolsFor(LabelSSH)
	if len(origSSH) != len(defsSSH) {
		t.Errorf("SSH tool count mismatch: original=%d, fromDefs=%d",
			len(origSSH), len(defsSSH))
	}

	// Check Browser tools.
	origBrowser := original.ToolsFor(LabelBrowser)
	defsBrowser := fromDefs.ToolsFor(LabelBrowser)
	if len(origBrowser) != len(defsBrowser) {
		t.Errorf("Browser tool count mismatch: original=%d, fromDefs=%d",
			len(origBrowser), len(defsBrowser))
	}
}

func containsSubstring(s, sub string) bool {
	return len(s) > 0 && len(sub) > 0 && indexOf(s, sub) >= 0
}

func indexOf(s, sub string) int {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
