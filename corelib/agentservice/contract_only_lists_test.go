package agentservice

import (
	"testing"

	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

// TestContractOnlyListsShareOneSourceOfTruth pins the invariant the graft
// depends on: the Go-name list the wiring test consumes and the capability-ID
// list the graft consumes must describe exactly the same families.
//
// These two lists exist because they answer different questions — the wiring
// test greps identifier names out of source text to prove "some attach site or
// reviewed binding mentions this", while the graft compares CapabilityNeed
// values — but they were once independent literals, and a divergence between
// them fails silently: a family could become permanently un-graftable (the
// "installed but unusable" failure) or permanently un-wired. A test that checks
// only one direction would miss adding an ID without its name, so both are
// asserted.
func TestContractOnlyListsShareOneSourceOfTruth(t *testing.T) {
	ids := ReviewedContractOnlyCapabilityIDs()
	names := reviewedContractOnlyCapabilities
	if len(ids) == 0 {
		t.Fatal("contract-only ID list must not be empty")
	}
	if len(ids) != len(names) {
		t.Fatalf("id/name list length mismatch: %d ids %v vs %d names %v", len(ids), ids, len(names), names)
	}
	// Names must be non-empty, unique, and each must resolve through the
	// predicate when read back as the ID it names.
	seen := map[string]bool{}
	for _, name := range names {
		if name == "" {
			t.Fatal("contract-only name must not be empty")
		}
		if seen[name] {
			t.Fatalf("duplicate contract-only name %q", name)
		}
		seen[name] = true
	}
	// Every declared ID must be recognized by the predicate, and every name
	// must correspond to a declared ID.
	for _, id := range ids {
		if !IsReviewedContractOnlyCapability(id) {
			t.Fatalf("%s is declared but IsReviewedContractOnlyCapability rejects it", id)
		}
	}
	// Cross-check name -> id via the well-known mapping already used elsewhere
	// in the wiring test, so a typo'd name is caught even if lengths agree.
	byName := map[string]coretool.CapabilityID{
		"CapabilityInformationLookup":   CapabilityInformationLookup,
		"CapabilityRecordReadWorklog":   CapabilityRecordReadWorklog,
		"CapabilityRecordUpdateWorklog": CapabilityRecordUpdateWorklog,
	}
	for _, name := range names {
		id, ok := byName[name]
		if !ok {
			t.Fatalf("contract-only name %q has no known capability id; add it to both lists", name)
		}
		if !IsReviewedContractOnlyCapability(id) {
			t.Fatalf("name %q maps to %s which is not recognized", name, id)
		}
	}
	if len(byName) != len(ids) {
		t.Fatalf("the cross-check table (%d) and the id list (%d) disagree; update both", len(byName), len(ids))
	}
	// A host-owned capability must never be contract-only.
	if IsReviewedContractOnlyCapability(coretool.CapabilityShellExecuteLocal) {
		t.Fatal("a host-owned capability must not be contract-only")
	}
	// information.lookup is the graft target, not a graft candidate.
	if !IsReviewedContractOnlyCapability(CapabilityInformationLookup) {
		t.Fatal("information.lookup must remain the open dynamic family")
	}
}
