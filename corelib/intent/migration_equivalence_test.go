package intent

import (
	"sort"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Migration equivalence tests — verify that definitions-derived data matches
// the original hardcoded data sources. These tests ensure the Phase 3
// migration from scattered data to unified IntentDefinition didn't lose
// or corrupt any data.
// ---------------------------------------------------------------------------

// TestMigration_AnchorsEquivalence verifies that BuildAnchorsFromDefinitions
// produces the same anchor labels and text counts as the old defaultAnchors().
func TestMigration_AnchorsEquivalence(t *testing.T) {
	oldAnchors := defaultAnchors()
	newAnchors := BuildAnchorsFromDefinitions(DefaultDefinitions())

	// New definitions may have MORE anchors than old (new labels added post-migration).
	// Verify all old anchors are preserved in new.
	if len(newAnchors) < len(oldAnchors) {
		t.Fatalf("anchor count regression: old=%d, new=%d (new should be >= old)", len(oldAnchors), len(newAnchors))
	}

	// Build maps for comparison.
	oldMap := make(map[IntentLabel]int)
	for _, a := range oldAnchors {
		oldMap[a.Label] = len(a.Texts)
	}
	newMap := make(map[IntentLabel]int)
	for _, a := range newAnchors {
		newMap[a.Label] = len(a.Texts)
	}

	for label, oldCount := range oldMap {
		newCount, ok := newMap[label]
		if !ok {
			t.Errorf("label %s missing from definitions-derived anchors", label)
			continue
		}
		if newCount < oldCount {
			t.Errorf("label %s text count regression: old=%d, new=%d", label, oldCount, newCount)
		}
	}
}

// TestMigration_KeywordRegistryEquivalence verifies that
// NewKeywordRegistryFromDefinitions(FullDefinitions()) produces the same
// entries as NewKeywordRegistry().
func TestMigration_KeywordRegistryEquivalence(t *testing.T) {
	old := NewKeywordRegistry()
	new := NewKeywordRegistryFromDefinitions(FullDefinitions())

	// Same entry count.
	if len(old.entries) != len(new.entries) {
		t.Errorf("entry count: old=%d, new=%d", len(old.entries), len(new.entries))
	}

	// Same strong index.
	if len(old.strongIndex) != len(new.strongIndex) {
		t.Errorf("strong index size: old=%d, new=%d", len(old.strongIndex), len(new.strongIndex))
	}
	for kw, oldLabel := range old.strongIndex {
		if newLabel, ok := new.strongIndex[kw]; !ok {
			t.Errorf("strong keyword %q missing from new registry", kw)
		} else if oldLabel != newLabel {
			t.Errorf("strong keyword %q: old=%s, new=%s", kw, oldLabel, newLabel)
		}
	}

	// Same label groups.
	for label, oldEntries := range old.byLabel {
		newEntries := new.byLabel[label]
		if len(oldEntries) != len(newEntries) {
			t.Errorf("label %s entry count: old=%d, new=%d", label, len(oldEntries), len(newEntries))
		}
	}
}

// TestMigration_ToolAffinityEquivalence verifies that
// NewToolAffinityRegistryFromDefinitions(FullDefinitions()) produces the
// same tool mappings as NewToolAffinityRegistry().
func TestMigration_ToolAffinityEquivalence(t *testing.T) {
	old := NewToolAffinityRegistry()
	new := NewToolAffinityRegistryFromDefinitions(FullDefinitions())

	for _, label := range AllLabels() {
		oldTools := old.ToolsFor(label)
		newTools := new.ToolsFor(label)

		// Sort for comparison.
		sort.Strings(oldTools)
		sort.Strings(newTools)

		// New definitions may have tools for labels that didn't exist in old
		// (post-migration additions). Only check labels that exist in old.
		if len(oldTools) == 0 && len(newTools) > 0 {
			continue // new label with tools — not a migration regression
		}

		if len(oldTools) != len(newTools) {
			t.Errorf("label %s tool count: old=%d, new=%d", label, len(oldTools), len(newTools))
			continue
		}
		for i := range oldTools {
			if oldTools[i] != newTools[i] {
				t.Errorf("label %s tool[%d]: old=%s, new=%s", label, i, oldTools[i], newTools[i])
			}
		}
	}
}

// TestMigration_DefinitionsHaveAllLabels verifies that DefaultDefinitions
// covers every label in the taxonomy.
func TestMigration_DefinitionsHaveAllLabels(t *testing.T) {
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

// TestMigration_FullDefinitionsKeywordCount verifies that FullDefinitions
// populates keywords for all labels that have entries in defaultKeywords.
func TestMigration_FullDefinitionsKeywordCount(t *testing.T) {
	defs := FullDefinitions()

	// Count keywords per label from defaultKeywords directly.
	directCount := make(map[IntentLabel]int)
	for _, kw := range defaultKeywords {
		directCount[kw.Label]++
	}

	// Count keywords per label from FullDefinitions.
	defsCount := make(map[IntentLabel]int)
	for _, def := range defs {
		defsCount[def.Label] = len(def.Keywords)
	}

	for label, expected := range directCount {
		got := defsCount[label]
		if got != expected {
			t.Errorf("label %s keyword count: defaultKeywords=%d, FullDefinitions=%d",
				label, expected, got)
		}
	}
}

// TestMigration_LLMPromptFromDefinitions verifies that buildLLMSystemPrompt()
// auto-generates intent labels from definitions and includes all labels.
func TestMigration_LLMPromptFromDefinitions(t *testing.T) {
	prompt := buildLLMSystemPrompt(DefaultDefinitions())

	// Should contain all 12 labels.
	for _, label := range AllLabels() {
		if !strings.Contains(prompt, string(label)) {
			t.Errorf("LLM prompt missing label %q", label)
		}
	}

	// Should contain disambiguation principles (not hardcoded rules).
	if !strings.Contains(prompt, "Disambiguation Principles") {
		t.Error("LLM prompt missing disambiguation principles section")
	}

	// Should NOT contain hardcoded scenario-specific rules.
	if strings.Contains(prompt, "帮我关掉chrome") {
		t.Error("LLM prompt should not hardcode chrome→SSH rule (context-dependent)")
	}

	// Should contain the general principle about context-dependent messages.
	if !strings.Contains(prompt, "Context-dependent messages") {
		t.Error("LLM prompt missing context-dependent principle")
	}
	if !strings.Contains(prompt, "Creation vs operation") {
		t.Error("LLM prompt missing creation vs operation principle")
	}

	// Should contain output format.
	if !strings.Contains(prompt, "Output Format") {
		t.Error("LLM prompt missing output format section")
	}

	// Should contain TreeText content from definitions (positive definitions, not hardcoded "区别于").
	if !strings.Contains(prompt, "从零创建软件") {
		t.Error("LLM prompt should contain coding TreeText")
	}

	// Should NOT contain hardcoded cross-reference disambiguation.
	if strings.Contains(prompt, "区别于") {
		t.Error("LLM prompt should not contain hardcoded '区别于' hints — disambiguation is auto-generated by domain grouping")
	}
}
