package guiapp

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// These tests pin the architectural guarantee: an MCP the user installed and
// whose tools/list the host observed must be reachable, whatever intent label
// the classifier picks and whatever the vendor named its tools.
//
// The production failure this guards against: a worklog MCP was installed and
// healthy, its contract was published, and the turn still died with
// no_feasible_provider before the model was consulted once — because routing to
// it depended on its label appearing in an allowlist it did not appear in.
// Nothing in the logs distinguished that from "no such MCP exists".

// TestContractOnlyGraftMintsOneNeedPerCapability guards a hard failure found in
// review: two required needs for the SAME capability each minted a graft with
// the same derived ID. The planner rejects duplicate need ids outright
// ("duplicate capability need id", semantic_planner.go), so instead of
// rescuing the turn this turned it into a planning error — strictly worse than
// the original bug.
//
// Needs-with-same-capability is not exotic: a read plus its update leg, or a
// repeat sibling, are all normal ways for it to happen.
func TestContractOnlyGraftMintsOneNeedPerCapability(t *testing.T) {
	lookup := tool.ProviderSpec{
		AdapterName: "v",
		Binding:     tool.ProviderBinding{Kind: "mcp", ProviderID: "s", ImplementationID: "v"},
		Provides: []tool.CapabilityProvision{{
			Capability: agentservice.CapabilityInformationLookup,
			Qualifiers: map[string]string{agentservice.QualifierInformationScope: agentservice.InformationScopeReference},
		}},
		Effects: []tool.EffectClass{tool.EffectReadOnly}, Ready: true,
	}
	needs := []tool.CapabilityNeed{
		{ID: "need:record.read.worklog:aaa", Capability: tool.CapabilityRecordReadWorklog, Polarity: tool.NeedRequire, Required: true},
		{ID: "need:record.read.worklog:bbb", Capability: tool.CapabilityRecordReadWorklog, Polarity: tool.NeedRequire, Required: true},
	}
	got := semanticNeedsWithContractOnlyGraft(needs, []tool.ProviderSpec{lookup}, nil, newIMSemanticCapabilityRegistry())
	if len(got) != 3 {
		t.Fatalf("two needs for one capability must yield exactly one graft, got %d needs: %#v", len(got), got)
	}
	seen := map[string]bool{}
	for _, need := range got {
		if seen[need.ID] {
			t.Fatalf("duplicate need id %q would make the planner fail the whole plan", need.ID)
		}
		seen[need.ID] = true
	}
}

// TestContractOnlyGraftIsDeterministicAcrossCapabilityOrder: the graft must not
// depend on map iteration order, or identical inputs produce different plans
// and different Unmet rows run to run. Two contract-only capabilities that
// both fall back to information.lookup share one graft: a second need would
// select the same adapter again and the renderer rejects the name collision.
func TestContractOnlyGraftIsDeterministicAcrossCapabilityOrder(t *testing.T) {
	lookup := tool.ProviderSpec{
		AdapterName: "v",
		Binding:     tool.ProviderBinding{Kind: "mcp", ProviderID: "s", ImplementationID: "v"},
		Provides: []tool.CapabilityProvision{{
			Capability: agentservice.CapabilityInformationLookup,
			Qualifiers: map[string]string{agentservice.QualifierInformationScope: agentservice.InformationScopeReference},
		}},
		Effects: []tool.EffectClass{tool.EffectReadOnly}, Ready: true,
	}
	// The write leg needs its own capable backend: one tool carries one effect
	// class (openLookupEffectsForTool returns either read_only or
	// external_effect, never both), so a reader cannot license relaxing a write.
	writer := lookup
	writer.AdapterName = "w"
	writer.Binding.ImplementationID = "w"
	writer.Effects = []tool.EffectClass{tool.EffectExternalEffect}
	providers := []tool.ProviderSpec{lookup, writer}
	build := func() []tool.CapabilityNeed {
		return []tool.CapabilityNeed{
			{ID: "need:record.read.worklog:aaa", Capability: tool.CapabilityRecordReadWorklog, Polarity: tool.NeedRequire, Required: true},
			{ID: "need:record.update.worklog:bbb", Capability: tool.CapabilityRecordUpdateWorklog, Polarity: tool.NeedRequire, Required: true},
		}
	}
	first := semanticNeedsWithContractOnlyGraft(build(), providers, nil, newIMSemanticCapabilityRegistry())
	const runs = 20
	for i := 0; i < runs; i++ {
		got := semanticNeedsWithContractOnlyGraft(build(), providers, nil, newIMSemanticCapabilityRegistry())
		if len(got) != len(first) {
			t.Fatalf("run %d produced %d needs, want %d", i, len(got), len(first))
		}
		for index := range got {
			if got[index].ID != first[index].ID {
				t.Fatalf("run %d produced id %q at %d, want %q: order is not deterministic", i, got[index].ID, index, first[index].ID)
			}
		}
	}
	if len(first) != 3 || first[0].Required || first[1].Required {
		t.Fatalf("both obligations must relax onto one shared lookup graft, got %#v", first)
	}
	lookups := 0
	for _, need := range first {
		if need.Capability == agentservice.CapabilityInformationLookup {
			lookups++
		}
	}
	if lookups != 1 || first[2].ID != "need:~installed-fallback:record.read.worklog" {
		t.Fatalf("the shared graft must be the single deterministic lookup need, got %#v", first)
	}
}

// TestDynamicLookupNeedIgnoresADownProvider pins the same "usable" definition
// for the ambient attach path. Before this shared providerProvisionList, a
// server whose only tool had failed observation still put the open-family need
// on the plan; the planner could select nothing from it, so the turn carried an
// unmet row for a capability the user never asked for.
func TestDynamicLookupNeedIgnoresADownProvider(t *testing.T) {
	down := tool.ProviderSpec{
		AdapterName: "down_tool",
		Binding:     tool.ProviderBinding{Kind: "mcp", ProviderID: "bad", ImplementationID: "down_tool"},
		Provides: []tool.CapabilityProvision{{
			Capability: agentservice.CapabilityInformationLookup,
			Qualifiers: map[string]string{agentservice.QualifierInformationScope: agentservice.InformationScopeReference},
		}},
		Effects: []tool.EffectClass{tool.EffectReadOnly}, Ready: false,
	}
	if got := semanticNeedsWithDynamicLookupForProviders(nil, []tool.ProviderSpec{down}); len(got) != 0 {
		t.Fatalf("a down-only provider must not attach the need, got %#v", got)
	}
	healthy := down
	healthy.Ready = true
	if got := semanticNeedsWithDynamicLookupForProviders(nil, []tool.ProviderSpec{healthy}); len(got) != 1 {
		t.Fatalf("a healthy provider must attach exactly one need, got %#v", got)
	}
}

// TestCallerWantsDynamicLookupIsNotAnAllowlistOfLabels is the anti-regression
// test for the whole change. The decision must be encoded as what makes the
// open family unsafe, never as a list of labels that happen to work today: a
// list silently stops every future enterprise MCP the moment someone adds its
// label to the wrong list.
func TestCallerWantsDynamicLookupIsNotAnAllowlistOfLabels(t *testing.T) {
	// The families whose absence caused the incident must now be open.
	for _, label := range []intent.IntentLabel{intent.LabelWorklogRead, intent.LabelWorklogUpdate} {
		if !intent.CallerWantsDynamicLookup(label) {
			t.Fatalf("%s must be reachable by an installed MCP", label)
		}
	}
	// A presenting/administering outcome still cannot be produced by "call some
	// vendor tool", so those stay closed — this is the property, not the list.
	for _, label := range []intent.IntentLabel{
		intent.LabelAttachmentDelivery, intent.LabelDocumentDelivery, intent.LabelAppLaunch,
		intent.LabelFileDelete, intent.LabelConfigManage, intent.LabelSessionManage,
		intent.LabelKnowledgeAdmin, intent.LabelCurrentTime,
	} {
		if intent.CallerWantsDynamicLookup(label) {
			t.Fatalf("%s must not be answered by an arbitrary vendor tool", label)
		}
	}
	// A non-capability label is not a request for anything.
	if intent.CallerWantsDynamicLookup(intent.IntentLabel("")) {
		t.Fatal("empty label must not attach the open family")
	}
	// The two retrieval-family decisions are now independent: excluding a label
	// from the host's own warehouse companions must NOT exclude it from being
	// served by what the user installed. Conflating them is the original defect.
	if intent.WantsAmbientRetrieval(intent.LabelWorklogRead) {
		t.Fatal("worklog_read must still not grow host knowledge companions")
	}
	if !intent.CallerWantsDynamicLookup(intent.LabelWorklogRead) {
		t.Fatal("installed-MCP reachability must be independent of the ambient list")
	}
}

// TestContractOnlyGraftRescuesUnservableRequiredNeed is the end of the failure
// mode: a required need that only an installed implementation could serve now
// gets a second path through what the user actually installed.
func TestContractOnlyGraftRescuesUnservableRequiredNeed(t *testing.T) {
	lookup := tool.ProviderSpec{
		AdapterName: "some_vendor_tool",
		Binding: tool.ProviderBinding{
			Kind: "mcp", ProviderID: "server-installed-by-user",
			ImplementationID: "arbitrary_vendor_name",
			SchemaDigest:     tool.SchemaDigest([]byte("arbitrary_vendor_name")),
		},
		Provides: []tool.CapabilityProvision{{
			Capability: agentservice.CapabilityInformationLookup,
			Qualifiers: map[string]string{agentservice.QualifierInformationScope: agentservice.InformationScopeReference},
			Quality:    1,
		}},
		Effects: []tool.EffectClass{tool.EffectReadOnly},
		Ready:   true,
	}
	needs := []tool.CapabilityNeed{{
		ID:         "need:record.read.worklog:6d6f8a6a31c1",
		Capability: tool.CapabilityRecordReadWorklog,
		Polarity:   tool.NeedRequire,
		Required:   true,
	}}
	got := semanticNeedsWithContractOnlyGraft(needs, []tool.ProviderSpec{lookup}, nil, newIMSemanticCapabilityRegistry())
	if len(got) != 2 {
		t.Fatalf("graft must add one need, got %d: %#v", len(got), got)
	}
	added := got[len(got)-1]
	if added.Capability != agentservice.CapabilityInformationLookup {
		t.Fatalf("graft target = %v, want the open family", added.Capability)
	}
	if added.Required {
		t.Fatal("graft must be optional: installing nothing still fails cleanly")
	}
	if strings.TrimSpace(added.ID) == "" {
		t.Fatal("graft need needs a stable identity")
	}
}

// TestContractOnlyGraftStaysClosedWithoutAnInstalledProvider is the fail-closed
// half. The graft must never invent a capability out of nothing: with no MCP
// installed, the turn must still be refused rather than routed somewhere wrong.
func TestContractOnlyGraftStaysClosedWithoutAnInstalledProvider(t *testing.T) {
	needs := []tool.CapabilityNeed{{
		ID:         "need:record.read.worklog:6d6f8a6a31c1",
		Capability: tool.CapabilityRecordReadWorklog,
		Polarity:   tool.NeedRequire,
		Required:   true,
	}}
	// No dynamic provider at all.
	if got := semanticNeedsWithContractOnlyGraft(needs, nil, nil, newIMSemanticCapabilityRegistry()); len(got) != 1 {
		t.Fatalf("no provider must leave the need alone, got %#v", got)
	}
	// A provider exists but serves nothing contract-only: nothing was installed
	// for this, so there is no honest graft target.
	other := tool.ProviderSpec{
		Binding:     tool.ProviderBinding{Kind: "builtin", ProviderID: "im", ImplementationID: "x"},
		Provides:    []tool.CapabilityProvision{{Capability: tool.CapabilityFSReadLocal}},
		Effects:     []tool.EffectClass{tool.EffectReadOnly},
		Ready:       true,
		AdapterName: "read_file",
	}
	if got := semanticNeedsWithContractOnlyGraft(needs, []tool.ProviderSpec{other}, nil, newIMSemanticCapabilityRegistry()); len(got) != 1 {
		t.Fatalf("an unrelated provider must not license a graft, got %#v", got)
	}
}

// TestContractOnlyGraftRespectsAnExplicitUserPin: when the user named an
// implementation, substituting another one answers a different question.
func TestContractOnlyGraftRespectsAnExplicitUserPin(t *testing.T) {
	lookup := tool.ProviderSpec{
		AdapterName: "another_vendor",
		Binding:     tool.ProviderBinding{Kind: "mcp", ProviderID: "server-b", ImplementationID: "other"},
		Provides: []tool.CapabilityProvision{{
			Capability: agentservice.CapabilityInformationLookup,
			Qualifiers: map[string]string{agentservice.QualifierInformationScope: agentservice.InformationScopeReference},
		}},
		Effects: []tool.EffectClass{tool.EffectReadOnly},
		Ready:   true,
	}
	needs := []tool.CapabilityNeed{{
		ID:         "need:record.read.worklog:abc",
		Capability: tool.CapabilityRecordReadWorklog,
		Polarity:   tool.NeedRequire,
		Required:   true,
	}}
	pins := []tool.ProviderPin{{
		Capability: tool.CapabilityRecordReadWorklog,
		Kind:       "mcp",
		ProviderID: "server-a",
	}}
	if got := semanticNeedsWithContractOnlyGraft(needs, []tool.ProviderSpec{lookup}, pins, newIMSemanticCapabilityRegistry()); len(got) != 1 {
		t.Fatalf("a pinned capability must not be grafted, got %#v", got)
	}
}

// TestContractOnlyGraftSkipsACapabilityItCanAlreadyServe: a reviewed provider
// must win on its own quality, never be shadowed by a lower-rank graft.
func TestContractOnlyGraftSkipsACapabilityItCanAlreadyServe(t *testing.T) {
	reviewed := tool.ProviderSpec{
		AdapterName: "work_log_query",
		Binding: tool.ProviderBinding{
			Kind: "mcp", ProviderID: "reviewed-server",
			ImplementationID: "work_log_query",
			SchemaDigest:     tool.SchemaDigest([]byte("work_log_query")),
		},
		Provides: []tool.CapabilityProvision{{Capability: tool.CapabilityRecordReadWorklog, Quality: 3}},
		Effects:  []tool.EffectClass{tool.EffectReadOnly},
		Ready:    true,
	}
	needs := []tool.CapabilityNeed{{
		ID: "need:record.read.worklog:abc", Capability: tool.CapabilityRecordReadWorklog,
		Polarity: tool.NeedRequire, Required: true,
	}}
	if got := semanticNeedsWithContractOnlyGraft(needs, []tool.ProviderSpec{reviewed}, nil, newIMSemanticCapabilityRegistry()); len(got) != 1 {
		t.Fatalf("a servable capability must not be grafted, got %#v", got)
	}
}

// TestContractOnlyDescriptorSetAgreesWithTheWiringList guards the two parallel
// lists from drifting. A drift is silent: a family added to only one list would
// be either permanently un-graftable or permanently un-wired.
func TestContractOnlyDescriptorSetAgreesWithTheWiringList(t *testing.T) {
	ids := agentservice.ReviewedContractOnlyCapabilityIDs()
	if len(ids) == 0 {
		t.Fatal("contract-only set must not be empty")
	}
	// ReviewedContractOnlyCapabilityIDs must agree with the string list the
	// wiring test consumes. This is checked by construction: each entry round
	// trips through the predicate.
	for _, id := range ids {
		if !agentservice.IsReviewedContractOnlyCapability(id) {
			t.Fatalf("%s is listed but not recognized", id)
		}
	}
	if agentservice.IsReviewedContractOnlyCapability(tool.CapabilityShellExecuteLocal) {
		t.Fatal("a host-owned capability must not be contract-only")
	}
	// information.lookup is the graft target, not something to be grafted onto.
	if !agentservice.IsReviewedContractOnlyCapability(agentservice.CapabilityInformationLookup) {
		t.Fatal("information.lookup must remain the open dynamic family")
	}
}

// TestContractOnlyGraftIgnoresADownProvider pins the fix found in review: a
// provider whose latest observation finished negative projects Ready=false and
// must NOT count as "installed a server exists" for suppression purposes.
//
// Before the fix, a down/blocked work-log provider made the graft skip, so the
// turn was refused even though a healthy tool that could serve it was installed
// — silently reproducing the original bug for exactly the users whose only
// work-log MCP had gone bad.
func TestContractOnlyGraftIgnoresADownProvider(t *testing.T) {
	downProvider := tool.ProviderSpec{
		AdapterName: "work_log_query",
		Binding:     tool.ProviderBinding{Kind: "mcp", ProviderID: "bad-server", ImplementationID: "work_log_query"},
		Provides:    []tool.CapabilityProvision{{Capability: tool.CapabilityRecordReadWorklog, Quality: 3}},
		Effects:     []tool.EffectClass{tool.EffectReadOnly},
		Ready:       false, // observation finished negative
	}
	lookup := tool.ProviderSpec{
		AdapterName: "healthy_vendor_tool",
		Binding: tool.ProviderBinding{
			Kind: "mcp", ProviderID: "good-server",
			ImplementationID: "healthy_vendor_tool",
			SchemaDigest:     tool.SchemaDigest([]byte("healthy_vendor_tool")),
		},
		Provides: []tool.CapabilityProvision{{
			Capability: agentservice.CapabilityInformationLookup,
			Qualifiers: map[string]string{agentservice.QualifierInformationScope: agentservice.InformationScopeReference},
			Quality:    1,
		}},
		Effects: []tool.EffectClass{tool.EffectReadOnly},
		Ready:   true,
	}
	needs := []tool.CapabilityNeed{{
		ID: "need:record.read.worklog:abc", Capability: tool.CapabilityRecordReadWorklog,
		Polarity: tool.NeedRequire, Required: true,
	}}
	got := semanticNeedsWithContractOnlyGraft(needs, []tool.ProviderSpec{downProvider, lookup}, nil, newIMSemanticCapabilityRegistry())
	if len(got) != 2 {
		t.Fatalf("a down work-log provider must not suppress the graft, got %d needs: %#v", len(got), got)
	}
	if got[len(got)-1].Capability != agentservice.CapabilityInformationLookup {
		t.Fatalf("expected graft to the open family, got %v", got[len(got)-1].Capability)
	}
}

// TestSemanticPermitsDynamicLookupKeepsContinuationTurnsClosed pins the one
// remaining withholding reason that is genuinely about this message continuing
// another, rather than about the intent label.
func TestSemanticPermitsDynamicLookupKeepsContinuationTurnsClosed(t *testing.T) {
	if semanticPermitsDynamicLookup(context.Background(), intent.LabelWorklogRead, true) {
		t.Fatal("a slim 继续 must not grow a vendor grant")
	}
	if !semanticPermitsDynamicLookup(context.Background(), intent.LabelWorklogRead, false) {
		t.Fatal("an ordinary turn must be able to reach an installed MCP")
	}
	if semanticPermitsDynamicLookup(context.Background(), intent.LabelFileDelete, false) {
		t.Fatal("a delete must not be routed to an arbitrary vendor tool")
	}
}

// TestContractOnlyGraftRefusesToRelaxAMutationWithoutAWriter pins the safety
// half of the relaxation: dropping a required obligation is only honest when
// the installed replacement can actually perform it.
//
// If the only installed tool is read-only, relaxing a required WRITE would let
// the turn proceed and silently not write — nothing tells the user, and for a
// system of record a missing write is invisible. That is worse than the clear
// refusal it replaced, so the obligation must stay and the turn must fail.
func TestContractOnlyGraftRefusesToRelaxAMutationWithoutAWriter(t *testing.T) {
	readOnlyTool := tool.ProviderSpec{
		AdapterName: "vendor_reader",
		Binding:     tool.ProviderBinding{Kind: "mcp", ProviderID: "s", ImplementationID: "vendor_reader"},
		Provides: []tool.CapabilityProvision{{
			Capability: agentservice.CapabilityInformationLookup,
			Qualifiers: map[string]string{agentservice.QualifierInformationScope: agentservice.InformationScopeReference},
			Quality:    1,
		}},
		Effects: []tool.EffectClass{tool.EffectReadOnly},
		Ready:   true,
	}
	mutation := []tool.CapabilityNeed{{
		ID: "need:record.update.worklog:abc", Capability: tool.CapabilityRecordUpdateWorklog,
		Polarity: tool.NeedRequire, Required: true,
	}}
	got := semanticNeedsWithContractOnlyGraft(mutation, []tool.ProviderSpec{readOnlyTool}, nil, newIMSemanticCapabilityRegistry())
	if len(got) != 1 || !got[0].Required {
		t.Fatalf("a required write must stay required when only readers are installed, got %#v", got)
	}

	// With a tool that admits an external effect, the same write may be relaxed.
	writerTool := readOnlyTool
	writerTool.Effects = []tool.EffectClass{tool.EffectExternalEffect}
	got = semanticNeedsWithContractOnlyGraft(mutation, []tool.ProviderSpec{writerTool}, nil, newIMSemanticCapabilityRegistry())
	if len(got) != 2 || got[0].Required {
		t.Fatalf("a capable writer must license the relaxation, got %#v", got)
	}

	// A read is carried by a read-only tool, so it still relaxes.
	read := []tool.CapabilityNeed{{
		ID: "need:record.read.worklog:abc", Capability: tool.CapabilityRecordReadWorklog,
		Polarity: tool.NeedRequire, Required: true,
	}}
	got = semanticNeedsWithContractOnlyGraft(read, []tool.ProviderSpec{readOnlyTool}, nil, newIMSemanticCapabilityRegistry())
	if len(got) != 2 || got[0].Required {
		t.Fatalf("a read served by a reader must relax, got %#v", got)
	}
}

// TestContractOnlyGraftRelaxesAReadOntoAnUnannotatedTool is the production
// failure: a query MCP is installed and healthy, its tools are not named
// work_log_query, and the vendor did not set readOnlyHint. The tool is
// published as external_effect under information.lookup. That effect is the
// receipt boundary, not a reason to tell the user the catalog does not cover
// the request.
func TestContractOnlyGraftRelaxesAReadOntoAnUnannotatedTool(t *testing.T) {
	installed := unannotatedLookupProvider("query_worklog", "tengyun", nil)
	needs := []tool.CapabilityNeed{{
		ID: "need:record.read.worklog:6d6f8a6a31c1", Capability: tool.CapabilityRecordReadWorklog,
		Polarity: tool.NeedRequire, Required: true,
	}}
	got := semanticNeedsWithContractOnlyGraft(needs, []tool.ProviderSpec{installed}, nil, newIMSemanticCapabilityRegistry())
	if len(got) != 2 || got[0].Required {
		t.Fatalf("an unannotated installed tool must relax a read, got %#v", got)
	}
	if got[1].Capability != agentservice.CapabilityInformationLookup {
		t.Fatalf("without an exact capability the fallback stays the open family, got %v", got[1].Capability)
	}
}

// TestContractOnlyGraftExposesEachInstalledTool: several tools on one server
// share information.lookup. Selecting one by binding order calls an arbitrary
// sibling. Each exact capability must be demanded so the model can call the
// tool whose name matches the request.
func TestContractOnlyGraftExposesEachInstalledTool(t *testing.T) {
	registry := newIMSemanticCapabilityRegistry()
	query := unannotatedLookupProvider("work_log_query", "tengyun", []tool.CapabilityID{
		agentservice.AutoMCPCapabilityIDForBinding("tengyun", "work_log_query"),
	})
	other := unannotatedLookupProvider("list_projects", "tengyun", []tool.CapabilityID{
		agentservice.AutoMCPCapabilityIDForBinding("tengyun", "list_projects"),
	})
	if err := agentservice.EnsureExternalCapabilityVocabulary(registry, []tool.CapabilityID{
		agentservice.AutoMCPCapabilityIDForBinding("tengyun", "work_log_query"),
		agentservice.AutoMCPCapabilityIDForBinding("tengyun", "list_projects"),
	}); err != nil {
		t.Fatal(err)
	}
	needs := []tool.CapabilityNeed{{
		ID: "need:record.read.worklog:6d6f8a6a31c1", Capability: tool.CapabilityRecordReadWorklog,
		Polarity: tool.NeedRequire, Required: true,
	}}
	got := semanticNeedsWithContractOnlyGraft(needs, []tool.ProviderSpec{query, other}, nil, registry)
	if len(got) != 3 || got[0].Required {
		t.Fatalf("each installed tool must be offered and the sealed need relaxed, got %#v", got)
	}
	seen := map[tool.CapabilityID]bool{}
	for _, need := range got[1:] {
		if need.Capability == agentservice.CapabilityInformationLookup {
			t.Fatal("exact capabilities must replace the single shared lookup winner")
		}
		if need.Required {
			t.Fatalf("installed tool need must be optional, got %#v", need)
		}
		seen[need.Capability] = true
	}
	for _, id := range []tool.CapabilityID{
		agentservice.AutoMCPCapabilityIDForBinding("tengyun", "work_log_query"),
		agentservice.AutoMCPCapabilityIDForBinding("tengyun", "list_projects"),
	} {
		if !seen[id] {
			t.Fatalf("missing exact need %s in %#v", id, got)
		}
	}
}

// TestInstalledUnannotatedToolsPlanWithoutCatalogRefusal is the turn the user
// saw die as "当前能力目录未覆盖这项请求". After the graft, the planner must
// select the installed tools and record no unmet need.
func TestInstalledUnannotatedToolsPlanWithoutCatalogRefusal(t *testing.T) {
	registry := newIMSemanticCapabilityRegistry()
	queryID := agentservice.AutoMCPCapabilityIDForBinding("tengyun", "work_log_query")
	listID := agentservice.AutoMCPCapabilityIDForBinding("tengyun", "list_projects")
	if err := agentservice.EnsureExternalCapabilityVocabulary(registry, []tool.CapabilityID{queryID, listID}); err != nil {
		t.Fatal(err)
	}
	query := unannotatedLookupProvider("work_log_query", "tengyun", []tool.CapabilityID{queryID})
	other := unannotatedLookupProvider("list_projects", "tengyun", []tool.CapabilityID{listID})
	catalog := tool.NewToolCatalog(registry)
	snapshot, err := catalog.PublishWithCoverage([]tool.ProviderSpec{query, other}, tool.CatalogCoverage{
		State: tool.CatalogCoverageComplete,
		Families: []tool.CatalogCoverageFamily{
			{Kind: "mcp", State: tool.CatalogCoverageComplete},
		},
	}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	needs := semanticNeedsWithContractOnlyGraft([]tool.CapabilityNeed{{
		ID: "need:record.read.worklog:6d6f8a6a31c1", Capability: tool.CapabilityRecordReadWorklog,
		Polarity: tool.NeedRequire, Required: true,
	}}, []tool.ProviderSpec{query, other}, nil, registry)
	plan, err := tool.NewToolPlanner(registry).Plan(tool.RouteRequest{
		RootTaskID: "r", SessionID: "s", TurnID: "t", ChannelScope: "desktop",
		Snapshot: snapshot, Needs: needs,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Unmet) != 0 {
		t.Fatalf("installed tools must not be a catalog miss, unmet=%#v", plan.Unmet)
	}
	selected := map[string]bool{}
	for _, selection := range plan.Selections {
		selected[selection.Provider.ImplementationID] = true
	}
	if !selected["work_log_query"] || !selected["list_projects"] {
		t.Fatalf("selections = %#v", plan.Selections)
	}
}

// TestContractOnlyGraftDoesNotHideServersBehindAnIncidentalPin: a pin of
// information.lookup names one server for that family. It must not hide
// another server's exact tool, and a pin of a server that cannot carry the
// need must not leave the work-log need required.
func TestContractOnlyGraftDoesNotHideServersBehindAnIncidentalPin(t *testing.T) {
	registry := newIMSemanticCapabilityRegistry()
	namedID := agentservice.AutoMCPCapabilityIDForBinding("tengyun", "work_log_query")
	otherID := agentservice.AutoMCPCapabilityIDForBinding("other", "search_docs")
	if err := agentservice.EnsureExternalCapabilityVocabulary(registry, []tool.CapabilityID{namedID, otherID}); err != nil {
		t.Fatal(err)
	}
	named := unannotatedLookupProvider("work_log_query", "tengyun", []tool.CapabilityID{namedID})
	sibling := unannotatedLookupProvider("search_docs", "other", []tool.CapabilityID{otherID})
	pins := []tool.ProviderPin{{
		Capability: agentservice.CapabilityInformationLookup, Kind: "mcp", ProviderID: "tengyun",
	}}
	got := semanticNeedsWithContractOnlyGraft([]tool.CapabilityNeed{{
		ID: "need:record.read.worklog:abc", Capability: tool.CapabilityRecordReadWorklog,
		Polarity: tool.NeedRequire, Required: true,
	}}, []tool.ProviderSpec{named, sibling}, pins, registry)
	if len(got) != 3 || got[0].Required {
		t.Fatalf("an incidental lookup pin must still expose every carrying tool, got %#v", got)
	}
	seen := map[tool.CapabilityID]bool{}
	for _, need := range got[1:] {
		seen[need.Capability] = true
	}
	if !seen[namedID] || !seen[otherID] {
		t.Fatalf("both servers must stay reachable, got %#v", got)
	}

	absent := []tool.ProviderPin{{
		Capability: agentservice.CapabilityInformationLookup, Kind: "mcp", ProviderID: "absent-server",
	}}
	got = semanticNeedsWithContractOnlyGraft([]tool.CapabilityNeed{{
		ID: "need:record.read.worklog:abc", Capability: tool.CapabilityRecordReadWorklog,
		Polarity: tool.NeedRequire, Required: true,
	}}, []tool.ProviderSpec{named}, absent, registry)
	if len(got) != 2 || got[0].Required || got[1].Capability != namedID {
		t.Fatalf("a pin of a server that cannot carry the need must not restore the catalog miss, got %#v", got)
	}
}

// TestContractOnlyGraftDropsAmbientLookupWhenEachToolIsExposed: the open
// family is attached before the graft on a work-log turn. Leaving it beside
// the exact tools force-calls one arbitrary sibling.
func TestContractOnlyGraftDropsAmbientLookupWhenEachToolIsExposed(t *testing.T) {
	registry := newIMSemanticCapabilityRegistry()
	queryID := agentservice.AutoMCPCapabilityIDForBinding("tengyun", "work_log_query")
	listID := agentservice.AutoMCPCapabilityIDForBinding("tengyun", "list_projects")
	if err := agentservice.EnsureExternalCapabilityVocabulary(registry, []tool.CapabilityID{queryID, listID}); err != nil {
		t.Fatal(err)
	}
	query := unannotatedLookupProvider("work_log_query", "tengyun", []tool.CapabilityID{queryID})
	other := unannotatedLookupProvider("list_projects", "tengyun", []tool.CapabilityID{listID})
	needs := []tool.CapabilityNeed{
		agentservice.AmbientDynamicLookupNeed(),
		{
			ID: "need:record.read.worklog:6d6f8a6a31c1", Capability: tool.CapabilityRecordReadWorklog,
			Polarity: tool.NeedRequire, Required: true,
		},
	}
	got := semanticNeedsWithContractOnlyGraft(needs, []tool.ProviderSpec{query, other}, nil, registry)
	if len(got) != 3 || got[0].Required || got[0].Capability != tool.CapabilityRecordReadWorklog {
		t.Fatalf("exact tools must replace the ambient lookup need, got %#v", got)
	}
	for _, need := range got {
		if need.Capability == agentservice.CapabilityInformationLookup {
			t.Fatalf("ambient lookup must not stay beside exact tools, got %#v", got)
		}
	}
}

// TestContractOnlyGraftReusesAmbientLookupWhenNoExactCapability: a tool
// published only as information.lookup already has the ambient need. A second
// lookup need would select the same adapter twice.
func TestContractOnlyGraftReusesAmbientLookupWhenNoExactCapability(t *testing.T) {
	installed := unannotatedLookupProvider("query_worklog", "tengyun", nil)
	ambient := agentservice.AmbientDynamicLookupNeed()
	needs := []tool.CapabilityNeed{
		ambient,
		{
			ID: "need:record.read.worklog:6d6f8a6a31c1", Capability: tool.CapabilityRecordReadWorklog,
			Polarity: tool.NeedRequire, Required: true,
		},
	}
	got := semanticNeedsWithContractOnlyGraft(needs, []tool.ProviderSpec{installed}, nil, newIMSemanticCapabilityRegistry())
	if len(got) != 2 || got[1].Required {
		t.Fatalf("the sealed need must relax onto the ambient lookup that is already demanded, got %#v", got)
	}
	lookups := 0
	for _, need := range got {
		if need.Capability == agentservice.CapabilityInformationLookup {
			lookups++
			if need.ID != ambient.ID {
				t.Fatalf("fallback must reuse the ambient need, got %#v", got)
			}
		}
	}
	if lookups != 1 {
		t.Fatalf("lookup needs = %d in %#v", lookups, got)
	}
}

// TestContractOnlyGraftDoesNotRepeatANamedToolNeed: a compound tool name
// already minted a required exact need. The graft must not demand that
// capability again, or the planner selects the same tool twice.
func TestContractOnlyGraftDoesNotRepeatANamedToolNeed(t *testing.T) {
	registry := newIMSemanticCapabilityRegistry()
	queryID := agentservice.AutoMCPCapabilityIDForBinding("tengyun", "work_log_query")
	listID := agentservice.AutoMCPCapabilityIDForBinding("tengyun", "list_projects")
	if err := agentservice.EnsureExternalCapabilityVocabulary(registry, []tool.CapabilityID{queryID, listID}); err != nil {
		t.Fatal(err)
	}
	query := unannotatedLookupProvider("work_log_query", "tengyun", []tool.CapabilityID{queryID})
	other := unannotatedLookupProvider("list_projects", "tengyun", []tool.CapabilityID{listID})
	got := semanticNeedsWithContractOnlyGraft([]tool.CapabilityNeed{
		{
			ID: "need:~named:" + string(queryID), Capability: queryID,
			Polarity: tool.NeedRequire, Required: true,
		},
		{
			ID: "need:record.read.worklog:abc", Capability: tool.CapabilityRecordReadWorklog,
			Polarity: tool.NeedRequire, Required: true,
		},
	}, []tool.ProviderSpec{query, other}, nil, registry)
	queryNeeds := 0
	var queryRequired bool
	for _, need := range got {
		if need.Capability == queryID {
			queryNeeds++
			queryRequired = need.Required
		}
		if need.Capability == agentservice.CapabilityInformationLookup {
			t.Fatalf("ambient or shared lookup must stay off, got %#v", got)
		}
	}
	if queryNeeds != 1 || !queryRequired {
		t.Fatalf("the named tool must stay the one required exact need, got %#v", got)
	}
	seenList := false
	worklogOptional := false
	for _, need := range got {
		if need.Capability == listID && !need.Required {
			seenList = true
		}
		if need.Capability == tool.CapabilityRecordReadWorklog && !need.Required {
			worklogOptional = true
		}
	}
	if !seenList || !worklogOptional {
		t.Fatalf("the sibling stays optional and the sealed need relaxes, got %#v", got)
	}
}

// TestContractOnlyGraftUsesTheHostToolAddress: a vendor-declared x:mcp id
// that sorts first must not become the need. The call has to resolve to the
// binding the host issued for this server and tool.
func TestContractOnlyGraftUsesTheHostToolAddress(t *testing.T) {
	registry := newIMSemanticCapabilityRegistry()
	hostID := agentservice.AutoMCPCapabilityIDForBinding("tengyun", "work_log_query")
	decoy := tool.CapabilityID("x:mcp:aaa:decoy")
	if err := agentservice.EnsureExternalCapabilityVocabulary(registry, []tool.CapabilityID{hostID, decoy}); err != nil {
		t.Fatal(err)
	}
	installed := unannotatedLookupProvider("work_log_query", "tengyun", []tool.CapabilityID{decoy, hostID})
	got := semanticNeedsWithContractOnlyGraft([]tool.CapabilityNeed{{
		ID: "need:record.read.worklog:abc", Capability: tool.CapabilityRecordReadWorklog,
		Polarity: tool.NeedRequire, Required: true,
	}}, []tool.ProviderSpec{installed}, nil, registry)
	if len(got) != 2 || got[0].Required || got[1].Capability != hostID {
		t.Fatalf("host address must win over a vendor x:mcp declaration, got %#v", got)
	}
}

// TestContractOnlyGraftKeepsExactSiblingsWhenOneToolHasNoCapability: one
// unpublished auto capability must not hide the tools that do have one, and
// must not add the force-called shared family beside them.
func TestContractOnlyGraftKeepsExactSiblingsWhenOneToolHasNoCapability(t *testing.T) {
	registry := newIMSemanticCapabilityRegistry()
	queryID := agentservice.AutoMCPCapabilityIDForBinding("tengyun", "work_log_query")
	if err := agentservice.EnsureExternalCapabilityVocabulary(registry, []tool.CapabilityID{queryID}); err != nil {
		t.Fatal(err)
	}
	query := unannotatedLookupProvider("work_log_query", "tengyun", []tool.CapabilityID{queryID})
	plain := unannotatedLookupProvider("list_projects", "tengyun", nil)
	got := semanticNeedsWithContractOnlyGraft([]tool.CapabilityNeed{{
		ID: "need:record.read.worklog:abc", Capability: tool.CapabilityRecordReadWorklog,
		Polarity: tool.NeedRequire, Required: true,
	}}, []tool.ProviderSpec{query, plain}, nil, registry)
	if len(got) != 2 || got[0].Required || got[1].Capability != queryID {
		t.Fatalf("the declared tool must stay exact and the shared family must stay off, got %#v", got)
	}
}

func unannotatedLookupProvider(toolName, serverID string, exact []tool.CapabilityID) tool.ProviderSpec {
	provides := []tool.CapabilityProvision{{
		Capability: agentservice.CapabilityInformationLookup,
		Qualifiers: map[string]string{agentservice.QualifierInformationScope: agentservice.InformationScopeReference},
		Quality:    1,
	}}
	for _, id := range exact {
		provides = append(provides, tool.CapabilityProvision{Capability: id, Quality: 1})
	}
	auth, err := tool.NewParameterAuthorization(map[string]interface{}{
		"type": "object", "properties": map[string]interface{}{}, "additionalProperties": false,
	})
	if err != nil {
		panic(err)
	}
	return tool.ProviderSpec{
		AdapterName: toolName + "@" + serverID,
		Binding: tool.ProviderBinding{
			Kind: "mcp", ProviderID: serverID, ImplementationID: toolName,
			SchemaDigest: tool.SchemaDigest([]byte(serverID + "/" + toolName)),
		},
		ParameterAuthorization: auth,
		Provides:               provides,
		Effects:                []tool.EffectClass{tool.EffectExternalEffect},
		Ready:                  true,
	}
}

// TestKnowledgeFamilyIsClosedTogether pins a consistency decision rather than a
// mechanism. A knowledge write's outcome is "it is in the host's store", which
// no vendor tool can produce, and sending the user's content to an installed
// third-party server is not what "记住这条" asked for. Knowledge admin and
// knowledge write must therefore share one answer; leaving one open would let
// the family drift half-open and re-open the exact "which list am I on" failure
// this whole change exists to remove.
func TestKnowledgeFamilyIsClosedTogether(t *testing.T) {
	admin := intent.CallerWantsDynamicLookup(intent.LabelKnowledgeAdmin)
	write := intent.CallerWantsDynamicLookup(intent.LabelKnowledgeWrite)
	if admin != write {
		t.Fatalf("knowledge family must be closed together: admin=%v write=%v", admin, write)
	}
	if admin || write {
		t.Fatal("knowledge admin/write are host-owned outcomes and must stay closed")
	}
	// Retrieval families are the opposite case: an installed tool answering a
	// question is precisely what the open family is for.
	for _, label := range []intent.IntentLabel{intent.LabelSearch, intent.LabelLiveData, intent.LabelWebFetch} {
		if !intent.CallerWantsDynamicLookup(label) {
			t.Fatalf("%s must be reachable by an installed MCP", label)
		}
	}
}
