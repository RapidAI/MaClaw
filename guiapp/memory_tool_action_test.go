package guiapp

import "testing"

func TestMemoryToolThemesIsRecallOnlyAllowed(t *testing.T) {
	if got := normalizeMemoryToolAction("themes"); got != memoryToolActionThemes {
		t.Fatalf("normalize themes = %q", got)
	}
	if !memoryToolActionThemes.IsRecallOnlyAllowed() {
		t.Fatal("themes should be allowed in recall-only contexts")
	}
	if got := normalizeMemoryToolAction("memory_candidates"); got != memoryToolActionCandidates {
		t.Fatalf("normalize memory_candidates = %q", got)
	}
	if !memoryToolActionCandidates.IsRecallOnlyAllowed() {
		t.Fatal("candidates should be allowed in recall-only contexts")
	}
	if got := normalizeMemoryToolAction("derived_audit"); got != memoryToolActionDerived {
		t.Fatalf("normalize derived_audit = %q", got)
	}
	if !memoryToolActionDerived.IsRecallOnlyAllowed() {
		t.Fatal("derived audit should be allowed in recall-only contexts")
	}
	if memoryToolActionDerivedSurgery.IsRecallOnlyAllowed() {
		t.Fatal("derived surgery should not be allowed in recall-only contexts")
	}
	if memoryToolActionSave.IsRecallOnlyAllowed() {
		t.Fatal("save should not be allowed in recall-only contexts")
	}
}

func TestMemoryToolActionReturnsRecalledData(t *testing.T) {
	recalled := []memoryToolAction{
		memoryToolActionRecall, memoryToolActionThemes, memoryToolActionScenes,
		memoryToolActionTrace, memoryToolActionCandidates, memoryToolActionDerived,
		memoryToolActionList, memoryToolActionSummary,
	}
	for _, a := range recalled {
		if !memoryToolActionReturnsRecalledData(a) {
			t.Fatalf("%s output is recalled data and must be fenced", a)
		}
	}
	// Mutation actions return host write acknowledgements — fencing them would
	// label a host-issued confirmation as untrusted store content.
	for _, a := range []memoryToolAction{memoryToolActionSave, memoryToolActionDelete, memoryToolActionDerivedSurgery} {
		if memoryToolActionReturnsRecalledData(a) {
			t.Fatalf("%s is a mutation; its confirmation must not be fenced", a)
		}
	}
}
