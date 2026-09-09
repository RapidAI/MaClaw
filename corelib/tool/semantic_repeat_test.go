package tool

import (
	"sort"
	"strings"
	"testing"
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
	// family and silently hide the second from the model.
	for _, id := range []string{
		"need:information.lookup:c0ffee#tag",
		"need:shell.execute.local:abc#1",
		"need:fs.read.local:abc#123",
		"#02",
		"need:fs.read.local:abc#00",
	} {
		if got := RepeatFamilyID(id); got != id {
			t.Fatalf("non-sibling id %q collapsed to %q", id, got)
		}
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
