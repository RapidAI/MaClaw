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

func TestRaiseExistingLocalFileWriteFloorKeepsTheEditIterative(t *testing.T) {
	base := coretool.CapabilityNeed{
		ID:         "need:fs.write.local:paper",
		Capability: coretool.CapabilityFSWriteLocal,
		Required:   true,
	}
	got := RaiseExistingLocalFileWriteFloor([]coretool.CapabilityNeed{
		base,
		{ID: "need:fs.read.local:paper", Capability: coretool.CapabilityFSReadLocal, Required: false},
	}, 0.91)
	writes, required, reads, shells := 0, 0, 0, 0
	for _, need := range got {
		switch need.Capability {
		case coretool.CapabilityFSWriteLocal:
			writes++
			if need.Required {
				required++
			}
			if need.ID != base.ID && coretool.RepeatFamilyID(need.ID) != base.ID {
				t.Fatalf("raised sibling left the write family: %s", need.ID)
			}
		case coretool.CapabilityFSReadLocal:
			reads++
		case coretool.CapabilityShellExecuteLocal:
			shells++
		}
	}
	if writes != localFileWriteRepeatFloor || required != 1 || reads != 1 || shells != 0 {
		t.Fatalf("writes=%d required=%d reads=%d shells=%d", writes, required, reads, shells)
	}
	if again := RaiseExistingLocalFileWriteFloor(got, 0.91); len(again) != len(got) {
		t.Fatalf("raising twice grew the family from %d to %d", len(got), len(again))
	}
	companion := []coretool.CapabilityNeed{
		{ID: "need:zz-baseline:fs.write.local:ssh", Capability: coretool.CapabilityFSWriteLocal},
		{ID: "need:other:fs.write.local:task", Capability: coretool.CapabilityFSWriteLocal},
	}
	if got := RaiseExistingLocalFileWriteFloor(companion, 0.9); len(got) != len(companion) {
		t.Fatalf("two write families were merged: %#v", got)
	}
	if got := RaiseExistingLocalFileWriteFloor(nil, 0.9); got != nil {
		t.Fatalf("empty needs grew: %#v", got)
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

func TestLiveDataBundlePlansPageFetch(t *testing.T) {
	if LatentArchetypeCapability(coretool.CapabilityInformationFetchWeb, intent.LabelLiveData) {
		t.Fatal("page fetch must not be latent on a live_data bundle")
	}
	if !LatentArchetypeCapability(coretool.CapabilityInformationFetchWeb, intent.LabelSearch) {
		t.Fatal("reference search must keep page fetch latent")
	}
	registry := coretool.NewCapabilityRegistry("live-data-fetch")
	for _, id := range []coretool.CapabilityID{CapabilityInformationSearchWeb, coretool.CapabilityInformationFetchWeb} {
		if err := registry.Register(coretool.CapabilityDescriptor{
			ID: id, Version: "v1", Effects: []coretool.EffectClass{coretool.EffectReadOnly}, Owner: "test",
		}); err != nil {
			t.Fatal(err)
		}
	}
	result := intent.ClassificationResult{Primary: intent.LabelLiveData, Confidence: .98}
	resolved, err := resolveIntentLabelCapabilityNeeds(registry, IMSemanticIntentCapabilityNeedRules(), ReviewedIntentMinimumConfidence, result)
	if err != nil || !resolved.Managed {
		t.Fatalf("resolved=%#v err=%v", resolved, err)
	}
	got := ExpandArchetypeBundleNeeds(registry, IMSemanticIntentCapabilityNeedRules(), result, true, resolved.Needs, intent.LabelLiveData)
	fetch := 0
	for _, need := range got {
		if need.Capability == coretool.CapabilityInformationFetchWeb {
			fetch++
		}
	}
	if fetch != 0 {
		t.Fatalf("live_data fetch siblings=%d, want 0 (companions stay in the directory); needs=%#v", fetch, got)
	}
}

// An office verdict with an unconfirmed live_data companion must admit web
// search without making it an obligation. The 2026-11-05 "做个演示ppt" turn
// delivered the deck and still hard-exited required_effect_unsettled because
// the synthesized lookup was required. The office write stays required. The
// archetype ceiling may raise the search family; every sibling stays optional.
func TestOfferedLookupOnOfficeAdmitsOptionalSearch(t *testing.T) {
	registry := coretool.NewCapabilityRegistry("office-offered-lookup")
	for _, id := range []coretool.CapabilityID{
		coretool.CapabilityDocumentWriteOffice,
		CapabilityArtifactDeliverCurrent,
		CapabilityInformationSearchWeb,
		coretool.CapabilityInformationFetchWeb,
		coretool.CapabilityArtifactAcquireRemote,
		coretool.CapabilityFSReadLocal,
	} {
		if err := registry.Register(coretool.CapabilityDescriptor{
			ID: id, Version: "v1", Effects: []coretool.EffectClass{coretool.EffectReadOnly}, Owner: "test",
		}); err != nil {
			t.Fatal(err)
		}
	}
	result := intent.ClassificationResult{
		Primary:    intent.LabelOffice,
		Offered:    []intent.IntentLabel{intent.LabelLiveData},
		Confidence: 0.999,
	}
	rules := IMSemanticIntentCapabilityNeedRules()
	resolved, err := resolveIntentLabelCapabilityNeeds(registry, rules, ReviewedIntentMinimumConfidence, result)
	if err != nil || !resolved.Managed {
		t.Fatalf("resolved managed=%v err=%v needs=%#v", resolved.Managed, err, resolved.Needs)
	}
	got := ExpandArchetypeBundleNeeds(registry, rules, result, true, resolved.Needs, intent.LabelOffice)
	writeRequired, search, searchRequired, offerBase := 0, 0, 0, 0
	for _, need := range got {
		switch need.Capability {
		case coretool.CapabilityDocumentWriteOffice:
			if need.Required {
				writeRequired++
			}
		case CapabilityInformationSearchWeb:
			search++
			if need.Required {
				searchRequired++
			}
			// The family base is what a lost Optional bit would treat as the
			// task. The archetype ceiling may replace evidence on later
			// siblings; the base has to keep the offer mark.
			if coretool.RepeatFamilyID(need.ID) == need.ID && coretool.EvidenceMarksCompanion(need.EvidenceIDs) {
				offerBase++
			}
		}
	}
	if writeRequired != 1 {
		t.Fatalf("office write required=%d, want 1; needs=%#v", writeRequired, got)
	}
	if search != 0 || searchRequired != 0 || offerBase != 0 {
		t.Fatalf("search siblings=%d required=%d offer base=%d, want no first-wave search grant; needs=%#v", search, searchRequired, offerBase, got)
	}
}

// A lookup the tree or the local leader confirmed stays an obligation.
// Offering it would let a weather chart finish with no facts.
func TestSecondaryLookupOnOfficeStaysRequired(t *testing.T) {
	registry := coretool.NewCapabilityRegistry("office-required-lookup")
	for _, id := range []coretool.CapabilityID{
		coretool.CapabilityDocumentWriteOffice,
		CapabilityArtifactDeliverCurrent,
		CapabilityInformationSearchWeb,
	} {
		if err := registry.Register(coretool.CapabilityDescriptor{
			ID: id, Version: "v1", Effects: []coretool.EffectClass{coretool.EffectReadOnly}, Owner: "test",
		}); err != nil {
			t.Fatal(err)
		}
	}
	result := intent.ClassificationResult{
		Primary:    intent.LabelOffice,
		Secondary:  []intent.IntentLabel{intent.LabelLiveData},
		Confidence: 0.999,
	}
	resolved, err := resolveIntentLabelCapabilityNeeds(registry, IMSemanticIntentCapabilityNeedRules(), ReviewedIntentMinimumConfidence, result)
	if err != nil || !resolved.Managed {
		t.Fatalf("resolved managed=%v err=%v needs=%#v", resolved.Managed, err, resolved.Needs)
	}
	searchRequired := 0
	for _, need := range resolved.Needs {
		if need.Capability == CapabilityInformationSearchWeb && need.Required {
			searchRequired++
		}
	}
	if searchRequired != 0 {
		t.Fatalf("secondary lookup minted a search grant; needs=%#v", resolved.Needs)
	}
}

// Offered belongs to the artifact primary synthesis attached it to. A
// rewrite that moves the primary to file delete must not keep the search,
// even when that capability is registered.
func TestOfferedLookupDoesNotFollowRewrittenPrimary(t *testing.T) {
	registry := coretool.NewCapabilityRegistry("rewritten-primary-drops-offer")
	for _, id := range []coretool.CapabilityID{
		coretool.CapabilityFSDeleteLocal,
		CapabilityInformationSearchWeb,
	} {
		if err := registry.Register(coretool.CapabilityDescriptor{
			ID: id, Version: "v1", Effects: []coretool.EffectClass{coretool.EffectReadOnly}, Owner: "test",
		}); err != nil {
			t.Fatal(err)
		}
	}
	result := intent.ClassificationResult{
		Primary:    intent.LabelFileDelete,
		Offered:    []intent.IntentLabel{intent.LabelLiveData},
		Confidence: 0.999,
	}
	resolved, err := resolveIntentLabelCapabilityNeeds(registry, IMSemanticIntentCapabilityNeedRules(), ReviewedIntentMinimumConfidence, result)
	if err != nil || !resolved.Managed {
		t.Fatalf("resolved managed=%v err=%v needs=%#v", resolved.Managed, err, resolved.Needs)
	}
	for _, need := range resolved.Needs {
		if need.Capability == CapabilityInformationSearchWeb {
			t.Fatalf("rewritten primary kept offered search; needs=%#v", resolved.Needs)
		}
	}
}
