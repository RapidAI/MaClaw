package guiapp

import (
	"reflect"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// The bundle table is an owner-reviewed map from a primary label to companion
// labels, and every member's needs are derived from imSemanticIntentRuleSet.
// A label renamed or dropped from the rule set must turn this pin red instead
// of silently shrinking an archetype face (§4.18: derived from the fact
// source, or pinned to it).
func TestSemanticArchetypeBundleLabelsExistInRuleSet(t *testing.T) {
	for primary, companions := range semanticArchetypeBundles {
		if len(imSemanticIntentRuleSet[primary]) == 0 {
			t.Errorf("bundle primary %q has no rule template", primary)
		}
		if len(companions) == 0 {
			t.Errorf("bundle primary %q carries an empty companion list", primary)
		}
		for _, companion := range companions {
			if len(imSemanticIntentRuleSet[companion]) == 0 {
				t.Errorf("bundle %q companion %q has no rule template", primary, companion)
			}
		}
	}
}

// The bundle deliberately excludes delivery legs: delivery is carried by the
// producing label's own rule and unlocked by the plan DAG phase. No bundle
// expansion may ever offer an artifact.deliver.* need, for any primary and
// any secondary mix.
func TestSemanticArchetypeBundleNeverOffersDelivery(t *testing.T) {
	registry := newIMSemanticCapabilityRegistry()
	for primary := range semanticArchetypeBundles {
		classification := intent.ClassificationResult{Primary: primary, Confidence: .98}
		needs, managed, err := semanticIntentNeedsFromClassification(registry, classification)
		if err != nil || !managed {
			t.Fatalf("primary %q needs=%#v managed=%v err=%v", primary, needs, managed, err)
		}
		for _, need := range needs {
			for _, evidence := range need.EvidenceIDs {
				if evidence == "intent:archetype_bundle" && strings.HasPrefix(string(need.Capability), "artifact.deliver.") {
					t.Fatalf("primary %q bundle offered delivery need %#v", primary, need)
				}
			}
		}
	}
}

// The same classification must always expand to the identical need sequence:
// the face is a function of the classification, never of map iteration or
// call order.
func TestSemanticArchetypeBundleExpansionIsDeterministic(t *testing.T) {
	registry := newIMSemanticCapabilityRegistry()
	for _, primary := range []intent.IntentLabel{
		intent.LabelOffice, intent.LabelDocumentGenerate, intent.LabelSearch,
		intent.LabelLiveData, intent.LabelWebFetch, intent.LabelFileRead,
		intent.LabelFileWrite, intent.LabelDocumentRead, intent.LabelShellCommand,
		intent.LabelDelegateTask, intent.LabelBrowser, intent.LabelComputerUse,
	} {
		classification := intent.ClassificationResult{Primary: primary, Confidence: .98}
		first, _, err := semanticIntentNeedsFromClassification(registry, classification)
		if err != nil {
			t.Fatalf("primary %q: %v", primary, err)
		}
		second, _, err := semanticIntentNeedsFromClassification(registry, classification)
		if err != nil {
			t.Fatalf("primary %q: %v", primary, err)
		}
		if !reflect.DeepEqual(first, second) {
			t.Fatalf("primary %q expansion is not deterministic:\nfirst=%#v\nsecond=%#v", primary, first, second)
		}
	}
}

// A cold office turn keeps the workspace read leg and leaves search, fetch,
// and download latent until the turn declares them or the session ceiling
// is spent.
func TestSemanticArchetypeBundleOfficeOffersDocumentLegs(t *testing.T) {
	registry := newIMSemanticCapabilityRegistry()
	needs, managed, err := semanticIntentNeedsFromClassification(registry, intent.ClassificationResult{Primary: intent.LabelOffice, Confidence: .98})
	if err != nil || !managed {
		t.Fatalf("needs=%#v managed=%v err=%v", needs, managed, err)
	}
	got := map[tool.CapabilityID]int{}
	for _, need := range needs {
		got[need.Capability]++
		if need.Capability == tool.CapabilityFSReadLocal {
			if need.Required {
				t.Fatalf("bundle offer must stay optional: %#v", need)
			}
			if len(need.EvidenceIDs) != 1 || need.EvidenceIDs[0] != "intent:archetype_bundle" {
				t.Fatalf("bundle offer evidence=%#v", need.EvidenceIDs)
			}
		}
	}
	if got[tool.CapabilityFSReadLocal] != 1 || got[tool.CapabilityArtifactAcquireRemote] != 3 {
		t.Fatalf("office companions=%v, want read 1 and download 3; needs=%#v", got, needs)
	}
	for _, latent := range []tool.CapabilityID{"information.search.web", tool.CapabilityInformationFetchWeb} {
		if got[latent] != 0 {
			t.Fatalf("cold office turn materialized latent %s %d times", latent, got[latent])
		}
	}
}

// A pure retrieval turn does not pull the other half of the pair onto the
// cold surface. Search and fetch are latent companions.
func TestSemanticArchetypeBundleSearchOffersWebFetch(t *testing.T) {
	cb := petitionTestOfficeCallbacks(t, &intent.ClassificationResult{Primary: intent.LabelSearch, Confidence: .98})
	if name := semanticGrantNameForAdapter(cb.semanticSurface, semanticTrustedWebFetchAdapter); name != "" {
		t.Fatalf("search face listed latent fetch as %q", name)
	}
	registry := newIMSemanticCapabilityRegistry()
	needs, _, err := semanticIntentNeedsFromClassification(registry, intent.ClassificationResult{Primary: intent.LabelWebFetch, Confidence: .98})
	if err != nil {
		t.Fatal(err)
	}
	for _, need := range needs {
		if need.Capability == "information.search.web" {
			t.Fatalf("web_fetch turn materialized latent search: %#v", need)
		}
	}
}

// A shell turn offers the local file legs its craft-and-run loop iterates
// over, both optional.
func TestSemanticArchetypeBundleShellOffersFileLegs(t *testing.T) {
	registry := newIMSemanticCapabilityRegistry()
	needs, managed, err := semanticIntentNeedsFromClassification(registry, intent.ClassificationResult{Primary: intent.LabelShellCommand, Confidence: .98})
	if err != nil || !managed {
		t.Fatalf("needs=%#v managed=%v err=%v", needs, managed, err)
	}
	foundRead, foundWrite := false, false
	for _, need := range needs {
		switch need.Capability {
		case tool.CapabilityFSReadLocal:
			foundRead = !need.Required
		case tool.CapabilityFSWriteLocal:
			foundWrite = !need.Required
		}
	}
	if !foundRead || !foundWrite {
		t.Fatalf("shell turn must offer optional read/write legs: needs=%#v", needs)
	}
}

// Dedup is by capability regardless of qualifiers: a classification that
// already declares search keeps exactly one search family (the declared,
// required one), and the office bundle never appends a second, optional one.
func TestSemanticArchetypeBundleDedupsDeclaredCapability(t *testing.T) {
	registry := newIMSemanticCapabilityRegistry()
	needs, managed, err := semanticIntentNeedsFromClassification(registry, intent.ClassificationResult{
		Primary: intent.LabelOffice, Secondary: []intent.IntentLabel{intent.LabelSearch}, Confidence: .98,
	})
	if err != nil || !managed {
		t.Fatalf("needs=%#v managed=%v err=%v", needs, managed, err)
	}
	searchNeeds := 0
	requiredSearch := 0
	for _, need := range needs {
		if need.Capability != "information.search.web" {
			continue
		}
		searchNeeds++
		if need.Required {
			requiredSearch++
			if tool.IsRepeatCeilingID(need.ID) {
				t.Fatalf("declared search ceiling must stay optional: %#v", need)
			}
		}
	}
	if searchNeeds != 5 || requiredSearch != 1 {
		t.Fatalf("declared search siblings=%d required=%d, want 5/1; needs=%#v", searchNeeds, requiredSearch, needs)
	}
}

// Production 2026-08-28 PPT turn ("生成庆祝我家布偶宝宝5岁生日的ppt，网上找
// 布偶照片"): the classification carried live_data rather than search. The
// declared live_data template budgets information.search.web at one invocation
// (freshness=current), while the office archetype bundle offers the same
// capability from the search template at five (freshness=reference). The
// capability-only dedup let the declared one-off shadow the archetype budget,
// so web_search died after a single success with "already ran successfully ...
// usage limit" while the turn still needed more lookups. The effective budget
// of one capability must be the MAX across its sources, never an accident of
// which label declared it first.
func TestSemanticArchetypeBundleUpgradesLowerBudgetDeclaredFamily(t *testing.T) {
	registry := newIMSemanticCapabilityRegistry()
	needs, managed, err := semanticIntentNeedsFromClassification(registry, intent.ClassificationResult{
		Primary: intent.LabelOffice, Secondary: []intent.IntentLabel{intent.LabelLiveData}, Confidence: .98,
	})
	if err != nil || !managed {
		t.Fatalf("needs=%#v managed=%v err=%v", needs, managed, err)
	}
	var searchNeeds []tool.CapabilityNeed
	for _, need := range needs {
		if need.Capability == "information.search.web" {
			searchNeeds = append(searchNeeds, need)
		}
	}
	if len(searchNeeds) != 5 {
		t.Fatalf("search siblings=%d, want the archetype budget 5 (max of declared 1 and bundle 5); needs=%#v", len(searchNeeds), needs)
	}
	// The upgrade must grow the EXISTING family, not spawn a second one: two
	// families bound to the same stable function name (web_search) would
	// collide on the rendered surface. The declared leg's qualifier
	// (freshness=current) and its required first invocation are retained —
	// only the ceiling moves, and the added siblings stay optional bundle
	// offers so a tight planning budget sheds them first.
	family := tool.RepeatFamilyID(searchNeeds[0].ID)
	for index, need := range searchNeeds {
		if tool.RepeatFamilyID(need.ID) != family {
			t.Fatalf("search upgrade spawned a second family: %#v", searchNeeds)
		}
		if need.Qualifiers["freshness"] != "current" {
			t.Fatalf("upgraded family lost the declared qualifier: %#v", need)
		}
		if index == 0 && !need.Required {
			t.Fatalf("the declared search invocation must stay required: %#v", need)
		}
		if index > 0 && (need.Required || len(need.EvidenceIDs) != 1 || need.EvidenceIDs[0] != "intent:archetype_bundle") {
			t.Fatalf("the raised ceiling must stay an optional bundle offer: %#v", need)
		}
	}
}

// A pure live_data weather turn gets the same ceiling. The classifier cannot
// distinguish a one-shot weather lookup from an iterative research turn at
// label granularity — that is exactly the jitter the archetype bundle exists
// to absorb — so the face (including the budget ceiling) must be identical for
// identical classifications. The budget is an exposure ceiling, never an
// obligation: siblings reach the model one at a time, and a weather turn
// simply never spends the remaining ones. What must NOT happen is a second
// search family with a different qualifier.
func TestSemanticArchetypeBundleLiveDataSearchBudgetIsSingleFamilyArchetypeMax(t *testing.T) {
	registry := newIMSemanticCapabilityRegistry()
	needs, managed, err := semanticIntentNeedsFromClassification(registry, intent.ClassificationResult{
		Primary: intent.LabelLiveData, Confidence: .98,
	})
	if err != nil || !managed {
		t.Fatalf("needs=%#v managed=%v err=%v", needs, managed, err)
	}
	var searchNeeds []tool.CapabilityNeed
	for _, need := range needs {
		if need.Capability == "information.search.web" {
			searchNeeds = append(searchNeeds, need)
		}
	}
	if len(searchNeeds) != 5 {
		t.Fatalf("live_data search siblings=%d, want the archetype ceiling 5; needs=%#v", len(searchNeeds), needs)
	}
	family := tool.RepeatFamilyID(searchNeeds[0].ID)
	for index, need := range searchNeeds {
		if tool.RepeatFamilyID(need.ID) != family {
			t.Fatalf("live_data search must stay one family: %#v", searchNeeds)
		}
		if need.Qualifiers["freshness"] != "current" {
			t.Fatalf("live_data search qualifier drifted: %#v", need)
		}
		if index == 0 && !need.Required {
			t.Fatalf("the declared live_data search invocation must stay required: %#v", need)
		}
		if index > 0 && need.Required {
			t.Fatalf("the raised ceiling must stay optional: %#v", need)
		}
	}
}

// Production 2026-09-22 "所以，多的脂肪去哪了？": the tree returned live_data
// plus search. Those templates both require information.search.web, with
// freshness current and reference. Two required families do not fit a light
// lookup's one selection, so the first wave was planning_budget_exceeded and
// the turn never started. They bind one adapter and must stay one family.
func TestLiveDataPlusSearchStaysOneSearchFamily(t *testing.T) {
	registry := newIMSemanticCapabilityRegistry()
	needs, managed, err := semanticIntentNeedsFromClassification(registry, intent.ClassificationResult{
		Primary: intent.LabelLiveData, Secondary: []intent.IntentLabel{intent.LabelSearch}, Confidence: .85,
	})
	if err != nil || !managed {
		t.Fatalf("needs=%#v managed=%v err=%v", needs, managed, err)
	}
	var searchNeeds []tool.CapabilityNeed
	for _, need := range needs {
		if need.Capability == "information.search.web" {
			searchNeeds = append(searchNeeds, need)
		}
	}
	if len(searchNeeds) != 5 {
		t.Fatalf("search siblings=%d, want one family raised to the search ceiling 5; needs=%#v", len(searchNeeds), needs)
	}
	family := tool.RepeatFamilyID(searchNeeds[0].ID)
	required := 0
	for _, need := range searchNeeds {
		if tool.RepeatFamilyID(need.ID) != family {
			t.Fatalf("live_data+search spawned a second search family: %#v", searchNeeds)
		}
		if need.Qualifiers["freshness"] != "current" {
			t.Fatalf("primary live_data qualifier lost: %#v", need)
		}
		if need.Required {
			required++
		}
	}
	if required != 1 {
		t.Fatalf("required search invocations=%d, want 1; needs=%#v", required, searchNeeds)
	}
}

// Image delivery and file delivery share artifact.deliver.current_channel
// but name different contracts. Collapsing them the way search freshness
// collapses would drop the PDF hand-off from a weather-card-plus-report turn.
func TestDeliverFormatsStaySeparateFamilies(t *testing.T) {
	registry := newIMSemanticCapabilityRegistry()
	needs, managed, err := semanticIntentNeedsFromClassification(registry, intent.ClassificationResult{
		Primary: intent.LabelLiveDataVisual, Secondary: []intent.IntentLabel{intent.LabelDocumentGenerate}, Confidence: .9,
	})
	if err != nil || !managed {
		t.Fatalf("needs=%#v managed=%v err=%v", needs, managed, err)
	}
	formats := map[string]bool{}
	for _, need := range needs {
		if need.Capability != "artifact.deliver.current_channel" || tool.IsRepeatCeilingID(need.ID) {
			continue
		}
		formats[need.Qualifiers["format"]] = need.Required
	}
	if !formats["image"] || !formats["file"] {
		t.Fatalf("deliver formats=%v, want required image and file; needs=%#v", formats, needs)
	}
}

// The max rule never shrinks a declared budget: when the declared label
// already budgets the capability at or above the bundle template, the family
// is left untouched.
func TestSemanticArchetypeBundleNeverShrinksDeclaredBudget(t *testing.T) {
	registry := newIMSemanticCapabilityRegistry()
	needs, managed, err := semanticIntentNeedsFromClassification(registry, intent.ClassificationResult{
		Primary: intent.LabelOffice, Secondary: []intent.IntentLabel{intent.LabelSearch}, Confidence: .98,
	})
	if err != nil || !managed {
		t.Fatalf("needs=%#v managed=%v err=%v", needs, managed, err)
	}
	searchNeeds := 0
	for _, need := range needs {
		if need.Capability == "information.search.web" {
			searchNeeds++
			if need.Qualifiers["freshness"] != "reference" {
				t.Fatalf("declared search family must stay reference: %#v", need)
			}
			if tool.IsRepeatCeilingID(need.ID) {
				if need.Required {
					t.Fatalf("declared search ceiling must stay optional: %#v", need)
				}
				continue
			}
			if !need.Required {
				t.Fatalf("declared search first invocation must stay required: %#v", need)
			}
		}
	}
	if searchNeeds != 5 {
		t.Fatalf("declared search siblings=%d, want the rule budget 5; needs=%#v", searchNeeds, needs)
	}
}

// Production 2026-08-28 PPT turn, second mechanism: the task "网上找布偶照片
// 做成生日PPT" classified as the declared lookup+generate composite
// (primary=live_data, secondary=document_generate). The bundle keyed on the
// primary label alone, so the turn got the retrieval pair (search+fetch) but
// NOT the document archetype's acquire/read legs — download_file was not on
// the face, the model burned the turn's single effectful petition on it, and
// the office petition the task actually needed then hit the spent budget and
// was hard-denied. A classification that declares document production anywhere
// is a document-archetype turn: the document bundle (a superset of the
// retrieval pair) must apply, or the face again depends on which label won
// primary.
func TestSemanticArchetypeBundleDocumentCompositeCarriesDocumentLegs(t *testing.T) {
	registry := newIMSemanticCapabilityRegistry()
	for _, classification := range []intent.ClassificationResult{
		{Primary: intent.LabelLiveData, Secondary: []intent.IntentLabel{intent.LabelDocumentGenerate}, Confidence: .98},
		{Primary: intent.LabelLiveData, Secondary: []intent.IntentLabel{intent.LabelOffice}, Confidence: .98},
		{Primary: intent.LabelSearch, Secondary: []intent.IntentLabel{intent.LabelDocumentGenerate}, Confidence: .98},
	} {
		needs, managed, err := semanticIntentNeedsFromClassification(registry, classification)
		if err != nil || !managed {
			t.Fatalf("needs=%#v managed=%v err=%v", needs, managed, err)
		}
		bundled := make(map[tool.CapabilityID]int)
		for _, need := range needs {
			if len(need.EvidenceIDs) == 1 && need.EvidenceIDs[0] == "intent:archetype_bundle" {
				if need.Required {
					t.Fatalf("bundle offer must stay optional: %#v", need)
				}
				bundled[need.Capability]++
			}
		}
		if bundled[tool.CapabilityArtifactAcquireRemote] != 3 || bundled[tool.CapabilityFSReadLocal] != 1 {
			t.Fatalf("document composite keeps download and read, not the lookup flood: bundled=%v needs=%#v", bundled, needs)
		}
	}
}

// A pure lookup turn keeps the retrieval pair only: no document label is
// declared, so the acquire/read legs must not appear. The face stays a
// function of the classification.
func TestSemanticArchetypeBundlePureLookupKeepsRetrievalPairOnly(t *testing.T) {
	registry := newIMSemanticCapabilityRegistry()
	needs, managed, err := semanticIntentNeedsFromClassification(registry, intent.ClassificationResult{
		Primary: intent.LabelLiveData, Confidence: .98,
	})
	if err != nil || !managed {
		t.Fatalf("needs=%#v managed=%v err=%v", needs, managed, err)
	}
	for _, need := range needs {
		if need.Capability == tool.CapabilityArtifactAcquireRemote || need.Capability == tool.CapabilityFSReadLocal {
			t.Fatalf("pure lookup turn gained a document leg: %#v", need)
		}
	}
}

// An unmanaged classification gains nothing: bundles are offers on the
// managed face, never a back door onto the legacy one.
func TestSemanticArchetypeBundleSkipsUnmanagedClassification(t *testing.T) {
	registry := newIMSemanticCapabilityRegistry()
	needs, managed, err := semanticIntentNeedsFromClassification(registry, intent.ClassificationResult{Primary: intent.LabelNonCoding, Confidence: .98})
	if err != nil || managed || len(needs) != 0 {
		t.Fatalf("needs=%#v managed=%v err=%v, want unmanaged with no needs", needs, managed, err)
	}
}
