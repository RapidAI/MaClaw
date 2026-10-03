package agentservice

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/intent"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

func TestExpandBaselineWorkspaceNeedsAddsIterativeFallbacksOnSearch(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	result := intent.ClassificationResult{Primary: intent.LabelSearch, Confidence: .98}
	needs, err := resolveIntentLabelCapabilityNeeds(registry, ReviewedDynamicIntentCapabilityNeedRules(), ReviewedIntentMinimumConfidence, result)
	if err != nil || !needs.Managed {
		t.Fatalf("search needs managed=%v err=%v", needs.Managed, err)
	}
	got := ExpandBaselineWorkspaceNeeds(registry, ReviewedDynamicIntentCapabilityNeedRules(), result, true, needs.Needs)
	found := map[coretool.CapabilityID]int{}
	required := map[coretool.CapabilityID]int{}
	for _, need := range got {
		found[need.Capability]++
		if need.Required {
			required[need.Capability]++
		}
	}
	if found[coretool.CapabilityFSReadLocal] < 2 {
		t.Fatalf("baseline file read must be iterative, got %d: %#v", found[coretool.CapabilityFSReadLocal], got)
	}
	if found[coretool.CapabilityFSWriteLocal] < 2 {
		t.Fatalf("baseline file write must be iterative, got %d", found[coretool.CapabilityFSWriteLocal])
	}
	if found[coretool.CapabilityShellExecuteLocal] < 2 {
		t.Fatalf("baseline shell must be iterative, got %d", found[coretool.CapabilityShellExecuteLocal])
	}
	if required[coretool.CapabilityFSReadLocal] != 0 || required[coretool.CapabilityFSWriteLocal] != 0 || required[coretool.CapabilityShellExecuteLocal] != 0 {
		t.Fatalf("baseline workspace needs must stay optional: required=%v", required)
	}
}

func TestExpandBaselineWorkspaceNeedsRaisesOneShotFileAuthoring(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	result := intent.ClassificationResult{Primary: intent.LabelFileWrite, Confidence: .98}
	rules := IMSemanticIntentCapabilityNeedRules()
	resolved, err := resolveIntentLabelCapabilityNeeds(registry, rules, ReviewedIntentMinimumConfidence, result)
	if err != nil || !resolved.Managed {
		t.Fatalf("file write needs managed=%v err=%v", resolved.Managed, err)
	}
	bundled := ExpandArchetypeBundleNeeds(registry, rules, result, true, resolved.Needs, intent.LabelFileWrite)
	got := ExpandBaselineWorkspaceNeeds(registry, rules, result, true, bundled)
	write, writeRequired, read := 0, 0, 0
	for _, need := range got {
		switch need.Capability {
		case coretool.CapabilityFSWriteLocal:
			write++
			if need.Required {
				writeRequired++
			}
		case coretool.CapabilityFSReadLocal:
			read++
		}
	}
	if write < 8 || writeRequired != 1 {
		t.Fatalf("file write floor=%d required=%d, want at least 8 with one required", write, writeRequired)
	}
	if read < 12 {
		t.Fatalf("file read floor=%d, want at least 12", read)
	}
}

func TestExpandBaselineWorkspaceNeedsDoesNotDuplicateShellPrimary(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	result := intent.ClassificationResult{Primary: intent.LabelShellCommand, Confidence: .98}
	needs, err := resolveIntentLabelCapabilityNeeds(registry, ReviewedDynamicIntentCapabilityNeedRules(), ReviewedIntentMinimumConfidence, result)
	if err != nil || !needs.Managed {
		t.Fatalf("shell needs managed=%v err=%v", needs.Managed, err)
	}
	got := ExpandBaselineWorkspaceNeeds(registry, ReviewedDynamicIntentCapabilityNeedRules(), result, true, needs.Needs)
	shellRequired := 0
	shellTotal := 0
	for _, need := range got {
		if need.Capability != coretool.CapabilityShellExecuteLocal {
			continue
		}
		shellTotal++
		if need.Required {
			shellRequired++
		}
	}
	if shellRequired != 1 {
		t.Fatalf("shell primary must keep one required sibling, got %d / %d", shellRequired, shellTotal)
	}
	if shellTotal < 8 {
		t.Fatalf("shell family must keep its iterative budget, got %d", shellTotal)
	}
}
