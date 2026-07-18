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

	// Browser should map to the single stable browser automation surface.
	browserTools := mapping[LabelBrowser]
	for _, want := range []string{"browser"} {
		found := false
		for _, name := range browserTools {
			if name == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("browser label should map to %q; got %#v", want, browserTools)
		}
	}
	for _, removed := range []string{"gui_record_start", "gui_record_stop"} {
		for _, name := range browserTools {
			if name == removed {
				t.Errorf("browser label should not map to legacy recorder tool %q; got %#v", removed, browserTools)
			}
		}
	}

	liveDataTools := mapping[LabelLiveData]
	wantLive := map[string]bool{"web_search": true, "web_fetch": true, "download_file": true}
	if len(liveDataTools) != len(wantLive) {
		t.Errorf("live_data label tool count = %d want %d; got %#v", len(liveDataTools), len(wantLive), liveDataTools)
	}
	for _, name := range liveDataTools {
		if !wantLive[name] {
			t.Errorf("live_data unexpected tool %q in %#v", name, liveDataTools)
		}
	}
}

func TestBusinessDataDefinitionRoutesToMISData(t *testing.T) {
	defs := DefaultDefinitions()
	mapping := BuildToolAffinityFromDefinitions(defs)

	tools := mapping[LabelBusinessData]
	if len(tools) == 0 {
		t.Fatal("business_data label should map to at least one tool")
	}
	for _, name := range tools {
		if name == "mis_data" {
			return
		}
	}
	t.Fatalf("business_data label should route to mis_data, got %#v", tools)
}

func TestNonCodingDefinitionDoesNotClaimLiveDataOrSearch(t *testing.T) {
	for _, def := range DefaultDefinitions() {
		if def.Label != LabelNonCoding {
			continue
		}
		for _, blocked := range []string{"天气", "weather", "搜索", "search"} {
			if containsSubstring(def.TreeText, blocked) {
				t.Fatalf("non_coding TreeText should not claim live/search domain %q: %s", blocked, def.TreeText)
			}
			for _, text := range def.EmbedTexts {
				if containsSubstring(text, blocked) {
					t.Fatalf("non_coding EmbedTexts should not claim live/search domain %q: %s", blocked, text)
				}
			}
		}
		return
	}
	t.Fatal("non_coding definition not found")
}

func TestWorkflowDefinitionCoversCNIPAPatentApplicationTypes(t *testing.T) {
	for _, def := range DefaultDefinitions() {
		if def.Label != LabelWorkflowTask {
			continue
		}
		for _, want := range []string{
			"patent_application=中国专利申请/撰写(按发明、实用新型、外观设计类型准备申请文件)",
			"帮我准备一份实用新型专利申请文件",
			"帮我整理外观设计专利申请图片和简要说明",
		} {
			found := containsSubstring(def.TreeText, want)
			for _, text := range def.EmbedTexts {
				found = found || containsSubstring(text, want)
			}
			if !found {
				t.Fatalf("workflow definition missing %q", want)
			}
		}
		return
	}
	t.Fatal("workflow_task definition not found")
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

	// SSH should have diagnostic keywords populated from defaultKeywords.
	for _, def := range defs {
		if def.Label == LabelSSH {
			if len(def.Keywords) == 0 {
				t.Error("SSH should have diagnostic keywords")
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

	origTime := original.ToolsFor(LabelCurrentTime)
	defsTime := fromDefs.ToolsFor(LabelCurrentTime)
	if len(origTime) != 1 || len(defsTime) != 1 || origTime[0] != "current_datetime" || defsTime[0] != "current_datetime" {
		t.Errorf("CurrentTime tools mismatch: original=%v, fromDefs=%v", origTime, defsTime)
	}

	origLiveData := original.ToolsFor(LabelLiveData)
	defsLiveData := fromDefs.ToolsFor(LabelLiveData)
	if len(origLiveData) != len(defsLiveData) {
		t.Errorf("LiveData tools mismatch: original=%v, fromDefs=%v", origLiveData, defsLiveData)
	}
	for _, name := range []string{"web_search", "web_fetch", "download_file"} {
		if !sliceContainsString(origLiveData, name) || !sliceContainsString(defsLiveData, name) {
			t.Errorf("LiveData missing %q: original=%v fromDefs=%v", name, origLiveData, defsLiveData)
		}
	}
}

func sliceContainsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
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
