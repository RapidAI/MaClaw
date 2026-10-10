package tool

import (
	"sort"
	"strings"
	"testing"
	"time"
)

// The first sibling must keep the identity a non-repeating need always had.
// If it shifted, every already-published plan, durable execution key, and
// stored grant built from that identity would stop matching after an upgrade.
func TestRepeatSiblingZeroKeepsTheHistoricalIdentity(t *testing.T) {
	base := "need:fs.read.local:abc123def456"
	if got := RepeatSiblingNeedID(base, 0); got != base {
		t.Fatalf("first sibling id = %q, want the unchanged base %q", got, base)
	}
	if got := RepeatSiblingNeedID(base, -3); got != base {
		t.Fatalf("negative index id = %q, want the unchanged base %q", got, base)
	}
}

// Siblings are exposed in plan order, so their identities must sort in
// invocation order rather than lexically stumbling at ten.
func TestRepeatSiblingsSortInInvocationOrder(t *testing.T) {
	base := "need:fs.read.local:abc123def456"
	ids := make([]string, 0, 12)
	for index := 0; index < 12; index++ {
		ids = append(ids, RepeatSiblingNeedID(base, index))
	}
	ordered := append([]string(nil), ids...)
	sort.Strings(ordered)
	for i := range ids {
		if ids[i] != ordered[i] {
			t.Fatalf("sibling order breaks at %d: generated=%v sorted=%v", i, ids, ordered)
		}
	}
}

func TestRepeatFamilyCollapsesSiblingsAndLeavesOthersAlone(t *testing.T) {
	base := "need:fs.read.local:abc123def456"
	for index := 0; index < 12; index++ {
		if got := RepeatFamilyID(RepeatSiblingNeedID(base, index)); got != base {
			t.Fatalf("sibling %d family = %q, want %q", index, got, base)
		}
	}
	// A qualifier, capability, or adapter is free to contain the separator.
	// Splitting on it blindly would merge two unrelated selections into one
	// family and silently hide the second from the model. Only suffixes the
	// sibling minter can produce collapse: three-digit ids are mintable now
	// that a family can run past 99 invocations, but anything outside the
	// mintable range (tag text, value below 2, beyond the family ceiling 256)
	// stays its own identity.
	for _, id := range []string{
		"need:information.lookup:c0ffee#tag",
		"need:shell.execute.local:abc#1",
		"need:fs.read.local:abc#999",
		"need:fs.read.local:abc#1234",
		"#02",
		"need:fs.read.local:abc#00",
	} {
		if got := RepeatFamilyID(id); got != id {
			t.Fatalf("non-sibling id %q collapsed to %q", id, got)
		}
	}
	// A three-digit suffix inside the mintable range is a real sibling under
	// the extended family ceiling and must collapse.
	if got := RepeatFamilyID("need:fs.read.local:abc#123"); got != "need:fs.read.local:abc" {
		t.Fatalf("three-digit minted suffix %q did not collapse: %q", "need:fs.read.local:abc#123", got)
	}
}

func TestRepeatSiblingRequiredKeepsOnlyTheFirstInvocationObligatory(t *testing.T) {
	if !RepeatSiblingRequired(true, 0) || RepeatSiblingRequired(true, 1) || RepeatSiblingRequired(true, 4) {
		t.Fatal("a required template must require only its first sibling")
	}
	if RepeatSiblingRequired(false, 0) || RepeatSiblingRequired(false, 1) {
		t.Fatal("an optional family stays optional as a whole")
	}
	if !RepeatSiblingRequired(true, -1) {
		t.Fatal("negative index is still the historical first sibling")
	}
}

func TestExtendRepeatFamilyRaisesCeilingWithoutRemintingBase(t *testing.T) {
	base := CapabilityNeed{
		ID:          "need:information.search.web:abc123def456",
		Capability:  "information.search.web",
		Qualifiers:  map[string]string{"freshness": "current"},
		Polarity:    NeedRequire,
		Required:    true,
		Confidence:  0.5,
		EvidenceIDs: []string{"intent:live_data"},
	}
	got := ExtendRepeatFamily(base, 1, 3, 0.98, []string{"intent:archetype_bundle"})
	if len(got) != 2 {
		t.Fatalf("siblings=%d, want 2: %#v", len(got), got)
	}
	if got[0].ID != RepeatSiblingNeedID(base.ID, 1) || got[1].ID != RepeatSiblingNeedID(base.ID, 2) {
		t.Fatalf("ids drifted: %#v", got)
	}
	if got[0].Required || got[1].Required {
		t.Fatalf("ceiling must stay optional: %#v", got)
	}
	if got[0].Capability != base.Capability || got[0].Polarity != base.Polarity || got[0].Qualifiers["freshness"] != "current" {
		t.Fatalf("family fields drifted: %#v", got[0])
	}
	if got[0].Confidence != 0.98 || len(got[0].EvidenceIDs) != 1 || got[0].EvidenceIDs[0] != "intent:archetype_bundle" {
		t.Fatalf("upgrade confidence/evidence drifted: %#v", got[0])
	}
	base.Qualifiers["freshness"] = "reference"
	got[0].Qualifiers["freshness"] = "changed"
	if got[1].Qualifiers["freshness"] != "current" {
		t.Fatalf("siblings must not share qualifier maps: %#v", got)
	}
	if ExtendRepeatFamily(base, 3, 3, 0.9, nil) != nil {
		t.Fatal("already at budget must emit nothing")
	}
	if ExtendRepeatFamily(base, 0, 1, 0.9, nil) != nil {
		t.Fatal("single-invocation family must not remint the base")
	}
	fromZero := ExtendRepeatFamily(base, 0, 2, 0.9, nil)
	if len(fromZero) != 1 || fromZero[0].ID != RepeatSiblingNeedID(base.ID, 1) {
		t.Fatalf("from=0 must start at the first ceiling sibling: %#v", fromZero)
	}
	if ExtendRepeatFamily(CapabilityNeed{}, 1, 3, 0.9, nil) != nil {
		t.Fatal("empty identity must emit nothing")
	}

	existing := CloneCapabilityNeed(base)
	existing.ID = RepeatSiblingNeedID(base.ID, 1)
	raised := ExtendRepeatFamily(existing, 2, 4, 0.9, []string{"intent:archetype_bundle"})
	if len(raised) != 2 || raised[0].ID != RepeatSiblingNeedID(base.ID, 2) || raised[1].ID != RepeatSiblingNeedID(base.ID, 3) {
		t.Fatalf("extending from an existing sibling must keep the family identity: %#v", raised)
	}
}

func TestIsRepeatCeilingIDSplitsOnlyMintedSiblings(t *testing.T) {
	base := "need:information.search.web:abc123def456"
	if IsRepeatCeilingID(base) || IsRepeatCeilingID(RepeatSiblingNeedID(base, 0)) {
		t.Fatal("the family base is not a ceiling sibling")
	}
	if !IsRepeatCeilingID(RepeatSiblingNeedID(base, 1)) || !IsRepeatCeilingID("selection:"+RepeatSiblingNeedID(base, 2)) {
		t.Fatal("minted #02/#03 siblings are ceiling ids")
	}
	if IsRepeatCeilingID("need:information.lookup:c0ffee#tag") || IsRepeatCeilingID("") {
		t.Fatal("unrelated hashes and empty ids must not look like ceiling siblings")
	}
}

func TestRepeatFamilySpentBudgetNoteRequiresABudgetedExhaustedFamily(t *testing.T) {
	base := "need:fs.read.local:abc123def456"
	sibling := RepeatSiblingNeedID(base, 1)
	plan := ToolPlan{Selections: []PlannedSelection{
		{ID: base, FitProof: FitProof{MatchedCapability: "fs.read.local"}},
		{ID: sibling, FitProof: FitProof{MatchedCapability: "fs.read.local"}},
	}}
	materialized := map[string]bool{base: true, sibling: true}
	note := RepeatFamilySpentBudgetNote(plan, sibling, materialized, nil)
	if note == "" || !strings.Contains(note, "fs.read.local") || !strings.Contains(note, "(2)") || !strings.Contains(note, "Do not narrate tool limits") {
		t.Fatalf("exhausted budget note = %q", note)
	}
	if got := RepeatFamilySpentBudgetNote(plan, sibling, materialized, []string{base}); got != "" {
		t.Fatalf("live sibling must suppress the notice, got %q", got)
	}
	if got := RepeatFamilySpentBudgetNote(ToolPlan{Selections: []PlannedSelection{{ID: base, FitProof: FitProof{MatchedCapability: "fs.read.local"}}}}, base, map[string]bool{base: true}, nil); got != "" {
		t.Fatalf("single-invocation family must stay silent, got %q", got)
	}
	got := ApplySpentBudgetNote(SelectionExecutionResult{Result: "ok", Succeeded: true}, plan, sibling, materialized, nil)
	if !strings.Contains(got.Result, "ok") || !strings.Contains(got.Result, "(2)") || !strings.Contains(got.Result, "Do not narrate tool limits") {
		t.Fatalf("apply spent-budget note=%q", got.Result)
	}
}

func TestExecutionGrantStateHelpers(t *testing.T) {
	if !ExecutionConsumesModelGrant(PlanExecutionFailed) || !ExecutionConsumesModelGrant(PlanExecutionUnknown) || !ExecutionConsumesModelGrant(PlanExecutionAwaitingReceipt) {
		t.Fatal("consumed grants must include failed, unknown, and awaiting receipt")
	}
	if ExecutionConsumesModelGrant(PlanExecutionSucceeded) || ExecutionConsumesModelGrant(PlanExecutionRunning) {
		t.Fatal("succeeded and running must not consume the model grant")
	}
	if !SelectionExecutionUnsettled(PlanExecutionRunning) || !SelectionExecutionUnsettled(PlanExecutionUnknown) || !SelectionExecutionUnsettled(PlanExecutionAwaitingReceipt) {
		t.Fatal("unsettled states must include running, unknown, and awaiting receipt")
	}
	if SelectionExecutionUnsettled(PlanExecutionFailed) || SelectionExecutionUnsettled(PlanExecutionSucceeded) {
		t.Fatal("failed and succeeded are settled")
	}
}

func TestSoleLiveGrantByAdapterAndRetiredLookup(t *testing.T) {
	grants := map[string]InvocationGrant{
		"send_file": {AdapterName: "semantic_deliver_current_file", Token: "tok-a"},
	}
	name, grant := SoleLiveGrantByAdapter(grants, "semantic_deliver_current_file")
	if name != "send_file" || grant.Token != "tok-a" {
		t.Fatalf("sole live name=%q grant=%#v", name, grant)
	}
	grants["send_file_2"] = InvocationGrant{AdapterName: "semantic_deliver_current_file", Token: "tok-b"}
	if got, _ := SoleLiveGrantByAdapter(grants, "semantic_deliver_current_file"); got != "" {
		t.Fatalf("two live adapters must not pick a winner, got %q", got)
	}
	retired := map[string]InvocationGrant{
		"invoke_pdf": {AdapterName: "generate_pdf", Token: "tok-c"},
	}
	if !HasRetiredGrantByAdapter(retired, "generate_pdf") || HasRetiredGrantByAdapter(retired, "office") {
		t.Fatal("retired adapter lookup mismatch")
	}
}

func TestLiveGrantNameForCapability(t *testing.T) {
	plan := ToolPlan{Selections: []PlannedSelection{{
		ID:       "sel-generate",
		FitProof: FitProof{MatchedCapability: "document.generate.file"},
	}}}
	grants := map[string]InvocationGrant{
		"generate_pdf": {SelectionID: "sel-generate", AdapterName: "generate_pdf"},
	}
	if got := LiveGrantNameForCapability(plan, grants, "document.generate.file"); got != "generate_pdf" {
		t.Fatalf("live generate grant=%q", got)
	}
	if got := LiveGrantNameForCapability(plan, grants, "information.search.web"); got != "" {
		t.Fatalf("unrelated capability=%q", got)
	}
	if got := LiveGrantNameForCapability(plan, grants, ""); got != "" {
		t.Fatalf("empty capability=%q", got)
	}
}

func TestRepeatFamilyHasUnissuedSiblingMatchesSelectionID(t *testing.T) {
	base := "need:shell.execute.remote_host:abc123def456"
	prototype := PlannedSelection{ID: "selection:" + base, NeedID: base, FitProof: FitProof{MatchedCapability: CapabilityShellExecuteRemoteHost}}
	idOnly := "selection:" + RepeatSiblingNeedID(base, 1)
	plan := ToolPlan{Selections: []PlannedSelection{
		prototype,
		{ID: idOnly, FitProof: FitProof{MatchedCapability: CapabilityShellExecuteRemoteHost}},
	}}
	if !RepeatFamilyHasUnissuedSibling(plan, prototype, prototype.ID, func(string) bool { return false }) {
		t.Fatal("a sibling stored only as a selection id was invisible, so the host would append another")
	}
	if RepeatFamilyHasUnissuedSibling(plan, prototype, prototype.ID, func(id string) bool { return id == idOnly }) {
		t.Fatal("an issued selection-id sibling still blocked the next command")
	}
}

func TestRepeatFamilyKeyCollapsesSelectionSpelling(t *testing.T) {
	base := "need:shell.execute.remote_host:abc123def456"
	bareSibling := RepeatSiblingNeedID(base, 1)
	ready := []PlannedSelection{
		{ID: "selection:" + base, NeedID: ""},
		{ID: bareSibling},
	}
	exposed := NextRepeatSelections(RepeatExposure{Ready: ready})
	if len(exposed) != 1 {
		t.Fatalf("two spellings of one family were both exposed: %#v", exposed)
	}
	// An empty-NeedID prototype used to mint selection:selection:need#NN,
	// which the exposure closure then treated as a second family.
	plan := ToolPlan{Selections: ready}
	updated, id, ok := AppendRepeatSibling(plan, "selection:"+base)
	if !ok {
		t.Fatal("empty-NeedID remote prototype refused the next command")
	}
	if strings.HasPrefix(strings.TrimPrefix(id, "selection:"), "selection:") {
		t.Fatalf("next command minted a second spelling: %s", id)
	}
	if RepeatFamilyKey(id) != base {
		t.Fatalf("next command left the family: %s", id)
	}
	if id == bareSibling || id == "selection:"+bareSibling {
		t.Fatalf("next command reused the bare sibling id %s", id)
	}
	_ = updated
	if !SettledIterativeListingAllowed(SelectionExecutionResult{ReasonCode: "selection_execution_failed"}) {
		t.Fatal("a settled command failure must list the next call")
	}
	if SettledIterativeListingAllowed(SelectionExecutionResult{Unknown: true}) ||
		SettledIterativeListingAllowed(SelectionExecutionResult{AwaitingReceipt: true}) ||
		SettledIterativeListingAllowed(SelectionExecutionResult{ReasonCode: "dynamic_binding_stale"}) ||
		SettledIterativeListingAllowed(SelectionExecutionResult{ReasonCode: "dynamic_execution_cancelled"}) {
		t.Fatal("unknown, waiting, binding recovery, and cancellation must not list another call")
	}
}

func TestCeilingSiblingSharesThePublishedFamily(t *testing.T) {
	base := "need:fs.write.local:abc123def456"
	kept := []PlannedSelection{{ID: "selection:" + base, NeedID: base}}
	// A ceiling stored only as selection:+need used to bill a second family
	// and hide its artifact from the consumer bound to the base.
	extra := []PlannedSelection{{ID: "selection:" + RepeatSiblingNeedID(base, 1)}}
	if families, _ := budgetChargeAgainst(kept, extra); families != 0 {
		t.Fatalf("id-only ceiling counted as its own family: %d", families)
	}
	bases := familyBaseSelectionIDs(append(kept, extra...), func(PlannedSelection) bool { return true })
	if len(bases) != 1 || bases[0] != "selection:"+base {
		t.Fatalf("after-edge bases=%v", bases)
	}
	bare := RepeatSiblingNeedID(base, 1)
	got, ok := NewestFamilyProducerArtifact([]RouteArtifactRef{{
		ArtifactID: "art-1", Kind: "document", MIMEType: "application/pdf",
		IntegrityDigest: "abc", ProducerSelection: bare, ProducerPurposeDigest: "purpose",
		CreatedAt: time.Unix(10, 0).UTC(),
	}}, "selection:"+base, ArtifactContract{Kind: "document", MIMEType: "application/pdf"})
	if !ok || got.ID != "art-1" {
		t.Fatalf("revision stored as a bare need was invisible: ok=%v id=%s", ok, got.ID)
	}
}

func TestAppendRepeatSiblingContinuesPastPublishedWave(t *testing.T) {
	base := "need:artifact.acquire.remote:abc"
	plan := ToolPlan{}
	for index := 0; index < RepeatSiblingBudgetLimit; index++ {
		needID := RepeatSiblingNeedID(base, index)
		plan.Selections = append(plan.Selections, PlannedSelection{
			ID:          "selection:" + needID,
			NeedID:      needID,
			AdapterName: "download_file",
			FitProof:    FitProof{Digest: "proof", MatchedCapability: CapabilityArtifactAcquireRemote},
		})
	}
	updated, id, ok := AppendRepeatSibling(plan, plan.Selections[0].ID)
	if !ok {
		t.Fatal("the 33rd download must extend the published wave")
	}
	if RepeatFamilyID(id) != base && RepeatFamilyID(strings.TrimPrefix(id, "selection:")) != base {
		t.Fatalf("new sibling %q left the download family", id)
	}
	if len(updated.Selections) != RepeatSiblingBudgetLimit+1 {
		t.Fatalf("selections=%d", len(updated.Selections))
	}
	if !updated.Selections[len(updated.Selections)-1].Continuation {
		t.Fatal("the sibling past the published wave was not marked as the one continuation")
	}
	if _, _, again := AppendRepeatSibling(updated, updated.Selections[0].ID); again {
		t.Fatal("a continuation renewed the budget")
	}
	materialized := map[string]bool{}
	for _, selection := range updated.Selections {
		materialized[selection.ID] = true
	}
	if note := RepeatFamilySpentBudgetNote(updated, updated.Selections[len(updated.Selections)-1].ID, materialized, nil); note != "" {
		t.Fatalf("continuation result promised another call: %q", note)
	}
	gapped := ToolPlan{Selections: []PlannedSelection{
		{ID: "selection:" + base, NeedID: base, AdapterName: "download_file"},
		{ID: "selection:" + RepeatSiblingNeedID(base, 5), NeedID: RepeatSiblingNeedID(base, 5), AdapterName: "download_file"},
	}}
	gappedPlan, gappedID, openedGap := AppendRepeatSibling(gapped, gapped.Selections[0].ID)
	if !openedGap {
		t.Fatal("a gap in sibling suffixes refused the next download")
	}
	if gappedID == "selection:"+RepeatSiblingNeedID(base, 2) {
		t.Fatalf("next id %q collided with a count-based suffix", gappedID)
	}
	if RepeatFamilyID(strings.TrimPrefix(gappedID, "selection:")) != base {
		t.Fatalf("gapped sibling %q left the family", gappedID)
	}
	_ = gappedPlan
	oneShot := ToolPlan{Selections: []PlannedSelection{{ID: "selection:once", NeedID: "need:once"}}}
	if _, _, opened := AppendRepeatSibling(oneShot, "selection:once"); opened {
		t.Fatal("a one-shot tool opened another invocation")
	}
	writeBase := "need:fs.write.local:abc123def456"
	singleWrite := ToolPlan{Selections: []PlannedSelection{{
		ID:       "selection:" + writeBase,
		NeedID:   writeBase,
		FitProof: FitProof{MatchedCapability: CapabilityFSWriteLocal},
	}}}
	extendedWrite, writeID, openedWrite := AppendRepeatSibling(singleWrite, singleWrite.Selections[0].ID)
	if !openedWrite {
		t.Fatal("a single local file write must extend when the model asks again")
	}
	if len(extendedWrite.Selections) != 2 || !strings.Contains(writeID, "#02") {
		t.Fatalf("extended write id=%q selections=%d", writeID, len(extendedWrite.Selections))
	}
	if extendedWrite.Selections[len(extendedWrite.Selections)-1].Continuation {
		t.Fatal("the next file write was marked as a budget continuation")
	}
	extendedAgain, _, openedAgain := AppendRepeatSibling(extendedWrite, extendedWrite.Selections[0].ID)
	if !openedAgain || len(extendedAgain.Selections) != 3 {
		t.Fatal("a local file write stopped after one extra sibling")
	}
	writeMaterialized := map[string]bool{}
	for _, selection := range extendedAgain.Selections {
		writeMaterialized[selection.ID] = true
	}
	if note := RepeatFamilySpentBudgetNote(extendedAgain, extendedAgain.Selections[len(extendedAgain.Selections)-1].ID, writeMaterialized, nil); note != "" {
		t.Fatalf("file write note promised another call: %q", note)
	}
	baseline := "need:zz-baseline:shell.execute.local:abc123def456"
	baselinePlan := ToolPlan{Selections: []PlannedSelection{
		{ID: "selection:" + baseline, NeedID: baseline, EvidenceIDs: []string{"intent:baseline_workspace"}, FitProof: FitProof{MatchedCapability: CapabilityShellExecuteLocal}},
		{ID: "selection:" + RepeatSiblingNeedID(baseline, 1), NeedID: RepeatSiblingNeedID(baseline, 1), EvidenceIDs: []string{"intent:baseline_workspace"}, FitProof: FitProof{MatchedCapability: CapabilityShellExecuteLocal}},
	}}
	if _, _, openedBaseline := AppendRepeatSibling(baselinePlan, baselinePlan.Selections[0].ID); openedBaseline {
		t.Fatal("a baseline shell floor opened another call")
	}
	baselineMaterialized := map[string]bool{}
	for _, selection := range baselinePlan.Selections {
		baselineMaterialized[selection.ID] = true
	}
	if note := RepeatFamilySpentBudgetNote(baselinePlan, baselinePlan.Selections[1].ID, baselineMaterialized, nil); note != "" {
		t.Fatalf("baseline shell note promised another call: %q", note)
	}
	owned := "need:shell.execute.local:abc123def456"
	mixed := ToolPlan{Selections: []PlannedSelection{
		{ID: "selection:" + owned, NeedID: owned, EvidenceIDs: []string{"intent:shell_command"}, FitProof: FitProof{MatchedCapability: CapabilityShellExecuteLocal}},
		{ID: "selection:" + RepeatSiblingNeedID(owned, 1), NeedID: RepeatSiblingNeedID(owned, 1), EvidenceIDs: []string{"intent:baseline_workspace"}, FitProof: FitProof{MatchedCapability: CapabilityShellExecuteLocal}},
	}}
	if _, _, openedMixed := AppendRepeatSibling(mixed, mixed.Selections[0].ID); !openedMixed {
		t.Fatal("a task-owned shell was treated as a companion floor")
	}
	capped := ToolPlan{}
	for index := 0; index < MaxRepeatFamilyInvocations; index++ {
		needID := RepeatSiblingNeedID(writeBase, index)
		capped.Selections = append(capped.Selections, PlannedSelection{
			ID:       "selection:" + needID,
			NeedID:   needID,
			FitProof: FitProof{MatchedCapability: CapabilityFSWriteLocal},
		})
	}
	if _, _, openedCap := AppendRepeatSibling(capped, capped.Selections[0].ID); openedCap {
		t.Fatal("file write extended past the turn cap")
	}
	remoteBase := "need:shell.execute.remote_host:abc123def456"
	remotePlan := ToolPlan{Selections: []PlannedSelection{
		{ID: "selection:" + remoteBase, NeedID: remoteBase, FitProof: FitProof{MatchedCapability: CapabilityShellExecuteRemoteHost}},
		{ID: "selection:" + RepeatSiblingNeedID(remoteBase, 1), NeedID: RepeatSiblingNeedID(remoteBase, 1), FitProof: FitProof{MatchedCapability: CapabilityShellExecuteRemoteHost}},
	}}
	remoteExtended, remoteID, openedRemote := AppendRepeatSibling(remotePlan, remotePlan.Selections[1].ID)
	if !openedRemote || len(remoteExtended.Selections) != 3 || !strings.Contains(remoteID, "#03") {
		t.Fatalf("remote command id=%q selections=%d", remoteID, len(remoteExtended.Selections))
	}
	if remoteExtended.Selections[len(remoteExtended.Selections)-1].Continuation {
		t.Fatal("the next remote command was marked as a one-shot continuation")
	}
	remoteAgain, _, openedRemoteAgain := AppendRepeatSibling(remoteExtended, remoteExtended.Selections[1].ID)
	if !openedRemoteAgain || len(remoteAgain.Selections) != 4 {
		t.Fatal("a remote command stopped after one extra call")
	}
	remoteMaterialized := map[string]bool{}
	for _, selection := range remoteAgain.Selections {
		remoteMaterialized[selection.ID] = true
	}
	if note := RepeatFamilySpentBudgetNote(remoteAgain, remoteAgain.Selections[len(remoteAgain.Selections)-1].ID, remoteMaterialized, nil); note != "" {
		t.Fatalf("remote command note promised another call: %q", note)
	}
	inherited := remoteAgain
	inherited.Selections = append([]PlannedSelection(nil), remoteAgain.Selections...)
	inherited.Selections[len(inherited.Selections)-1].Continuation = true
	clearedRemote, _, openedCleared := AppendRepeatSibling(inherited, inherited.Selections[len(inherited.Selections)-1].ID)
	if !openedCleared || clearedRemote.Selections[len(clearedRemote.Selections)-1].Continuation {
		t.Fatal("the next remote command inherited Continuation from its prototype")
	}
	singleRemote := ToolPlan{Selections: []PlannedSelection{{
		ID: "selection:" + remoteBase, NeedID: remoteBase, FitProof: FitProof{MatchedCapability: CapabilityShellExecuteRemoteHost},
	}}}
	if _, _, openedSingle := AppendRepeatSibling(singleRemote, singleRemote.Selections[0].ID); !openedSingle {
		t.Fatal("a single remote command must extend when the task is unfinished")
	}
	localProof := ToolPlan{Selections: []PlannedSelection{{
		ID: "selection:" + remoteBase, NeedID: remoteBase, FitProof: FitProof{MatchedCapability: CapabilityShellExecuteLocal},
	}}}
	if _, _, openedLocal := AppendRepeatSibling(localProof, localProof.Selections[0].ID); openedLocal {
		t.Fatal("a local-shell proof must not extend because its id mentions the remote capability")
	}
	mismatched := ToolPlan{Selections: []PlannedSelection{{
		ID:       "selection:" + writeBase,
		NeedID:   writeBase,
		FitProof: FitProof{MatchedCapability: CapabilityFSReadLocal},
	}}}
	if _, _, openedMismatch := AppendRepeatSibling(mismatched, mismatched.Selections[0].ID); openedMismatch {
		t.Fatal("a read proof must not extend because its id mentions fs.write.local")
	}
	unproven := ToolPlan{Selections: []PlannedSelection{{
		ID:     "selection:" + writeBase,
		NeedID: writeBase,
	}}}
	if _, _, openedUnproven := AppendRepeatSibling(unproven, unproven.Selections[0].ID); !openedUnproven {
		t.Fatal("a file-write need with no fit proof must still extend")
	}
	file := ArtifactContract{Kind: "file", MIMEType: "application/octet-stream", Required: true}
	withEdge := ToolPlan{Selections: []PlannedSelection{
		{ID: "selection:" + base, NeedID: base, AdapterName: "download_file", Produces: []ArtifactContract{file}},
		{ID: "selection:" + RepeatSiblingNeedID(base, 1), NeedID: RepeatSiblingNeedID(base, 1), AdapterName: "download_file", Requires: []string{"selection:" + base}, Consumes: []ArtifactContract{file}, ArtifactDependencies: []ArtifactDependency{{ProducerSelection: "selection:" + base, Contract: file}}},
	}}
	extended, _, opened := AppendRepeatSibling(withEdge, withEdge.Selections[1].ID)
	if !opened {
		t.Fatal("a download with a producer edge did not extend")
	}
	if err := validateToolPlanArtifactDependencies(extended); err != nil {
		t.Fatalf("extended plan is not publishable: %v", err)
	}
}

func TestRepeatSiblingBudgetTreatsSilenceAsSingleInvocation(t *testing.T) {
	for _, declared := range []int{-5, 0, 1} {
		if got := RepeatSiblingBudget(declared); got != 1 {
			t.Fatalf("budget(%d) = %d, want 1", declared, got)
		}
	}
	if got := RepeatSiblingBudget(12); got != 12 {
		t.Fatalf("budget(12) = %d, want 12", got)
	}
	// The budget becomes real plan nodes, so an unbounded rule must be capped
	// rather than trusted.
	if got := RepeatSiblingBudget(5000); got != RepeatSiblingBudgetLimit {
		t.Fatalf("budget(5000) = %d, want the %d cap", got, RepeatSiblingBudgetLimit)
	}
}
