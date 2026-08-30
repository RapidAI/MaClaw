package main

import (
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/tool"
	v2 "github.com/RapidAI/CodeClaw/corelib/workflow/v2"
)

// semanticSensitiveFamilyCase describes one builtin sensitive family migrated
// to governed semantic routing in the S2a slice.
type semanticSensitiveFamilyCase struct {
	name        string
	label       intent.IntentLabel
	capability  tool.CapabilityID
	qualifiers  map[string]string
	adapterName string
	// invocationArgs is the request body this family accepts on the managed
	// surface. Most families take anything the canonicalizer admits; mis_data
	// is bounded by an action allowlist (semantic_mis_action_bound.go), so an
	// empty body is refused before the adapter runs and would make this
	// chain test fail for a reason it is not about.
	invocationArgs string
	// inputSchema is the provider schema the stub publishes. It must admit
	// whatever invocationArgs sends, or the canonicalizer rejects the field
	// before the action bound is ever consulted.
	inputSchema map[string]interface{}
}

func (c semanticSensitiveFamilyCase) requestSchema() map[string]interface{} {
	if c.inputSchema != nil {
		return c.inputSchema
	}
	return map[string]interface{}{}
}

func (c semanticSensitiveFamilyCase) requestBody() string {
	if strings.TrimSpace(c.invocationArgs) == "" {
		return `{}`
	}
	return c.invocationArgs
}

func semanticSensitiveFamilyCases() []semanticSensitiveFamilyCase {
	return []semanticSensitiveFamilyCase{
		{name: "office", label: intent.LabelOffice, capability: tool.CapabilityDocumentWriteOffice, qualifiers: map[string]string{"format": "spreadsheet"}, adapterName: "office"},
		{name: "mis_data", label: intent.LabelBusinessData, capability: tool.CapabilityBusinessDataMIS, adapterName: "mis_data", invocationArgs: `{"action":"status"}`,
			inputSchema: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{"action": map[string]interface{}{"type": "string"}},
			}},
		{name: "knowledge_save_text", label: intent.LabelKnowledgeWrite, capability: tool.CapabilityKnowledgeIngestLocal, adapterName: "knowledge_save_text"},
	}
}

func dropAmbientRetrievalNeeds(needs []tool.CapabilityNeed) []tool.CapabilityNeed {
	out := make([]tool.CapabilityNeed, 0, len(needs))
	for _, need := range needs {
		ambient := strings.HasPrefix(need.ID, "need:~ambient:")
		for _, evidence := range need.EvidenceIDs {
			if evidence == "ambient:retrieval" {
				ambient = true
			}
		}
		if !ambient {
			out = append(out, need)
		}
	}
	return out
}

func semanticIntentNeedsFromClassification(registry *tool.CapabilityRegistry, result intent.ClassificationResult) ([]tool.CapabilityNeed, bool, error) {
	needs, managed, err := semanticNeedsFromClassification(registry, result)
	return dropAmbientRetrievalNeeds(needs), managed, err
}

func TestSemanticSensitiveFamiliesAreManagedAndMapToGovernedNeeds(t *testing.T) {
	registry := newIMSemanticCapabilityRegistry()
	for _, tc := range semanticSensitiveFamilyCases() {
		t.Run(tc.name, func(t *testing.T) {
			classification := intent.ClassificationResult{Primary: tc.label, Confidence: .98}
			managed, unmapped := imSemanticIntentCoverage(classification)
			if !managed || unmapped != "" {
				t.Fatalf("coverage managed=%v unmapped=%q, want managed without unmapped", managed, unmapped)
			}
			needs, resolvedManaged, err := semanticIntentNeedsFromClassification(registry, classification)
			if err != nil || !resolvedManaged {
				t.Fatalf("needs=%#v managed=%v err=%v", needs, resolvedManaged, err)
			}
			// A family may plan companion needs — business_data plans the
			// read-only surface alongside the sensitive one, and office plans
			// the current-channel file delivery for its produced document — but
			// the sensitive capability is the one the turn cannot proceed
			// without, so it stays required.
			var required []tool.CapabilityNeed
			for _, need := range needs {
				if need.Required {
					required = append(required, need)
				}
			}
			found := false
			for _, need := range required {
				if need.Capability == tc.capability {
					found = true
					continue
				}
				// The only other required need a family may declare is the
				// delivery half of a write+deliver pair (office).
				if need.Capability != "artifact.deliver.current_channel" || need.Qualifiers["format"] != "file" {
					t.Fatalf("unexpected required companion need %#v (all needs %#v)", need, needs)
				}
			}
			if !found {
				t.Fatalf("required=%#v (all needs %#v), want %s required", required, needs, tc.capability)
			}
		})
	}
}

// TestSemanticSensitiveFamiliesExecuteBoundBuiltinHandlers proves the full
// ToolCatalog → ToolPlanner → InvocationIssuer → CatalogRenderer →
// PlanExecutor chain for each migrated sensitive family: the plan selects the
// annotated builtin provider, the model only sees an opaque grant, and the
// admitted invocation lands on the legacy handler through the host-local
// mutation receipt boundary.
func TestSemanticSensitiveFamiliesExecuteBoundBuiltinHandlers(t *testing.T) {
	for _, tc := range semanticSensitiveFamilyCases() {
		if tc.adapterName == "knowledge_save_text" || tc.adapterName == "office" {
			// Managed knowledge ingest / office write no longer bind the legacy soups.
			continue
		}
		t.Run(tc.name, func(t *testing.T) {
			h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, tc.label)}
			called := 0
			if err := h.registry.Register(RegisteredTool{
				Name: tc.adapterName, Description: "untrusted family description", Status: RegToolAvailable,
				InputSchema:          tc.requestSchema(),
				CapabilityProvisions: []tool.CapabilityProvision{{Capability: tc.capability, Qualifiers: tc.qualifiers, Quality: 1}},
				SemanticEffects:      []tool.EffectClass{tool.EffectSensitive},
				Handler: func(map[string]interface{}) string {
					called++
					return "family-result:" + tc.name
				},
			}); err != nil {
				t.Fatal(err)
			}
			defs, surface, handled, err := h.semanticCallSurfaceForSharedTurn("user-1", "family request", "lansenger")
			if err != nil || !handled || surface == nil || len(defs) < 1 {
				t.Fatalf("defs=%#v handled=%v surface=%#v err=%v", defs, handled, surface, err)
			}
			name := extractToolName(defs[0])
			selection := surface.plan.Selections[0]
			if want := tool.RenderedSemanticFunctionName(selection.AdapterName, ""); name != want || name == "" {
				t.Fatalf("function name=%q, want stable host name %q", name, want)
			}
			if selection.AdapterName != tc.adapterName || selection.FitProof.MatchedCapability != tc.capability {
				t.Fatalf("selection=%+v, want adapter %q capability %s", selection, tc.adapterName, tc.capability)
			}
			if !semanticSelectionRequiresReceipt(selection) || !semanticBuiltinLocalMutationSelection(selection) {
				t.Fatalf("sensitive builtin selection must use the local mutation receipt boundary: %+v", selection.Effects)
			}
			callback := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface}
			if name != tc.adapterName {
				if got := callback.ExecuteTool(tc.adapterName, tc.requestBody()); !strings.Contains(got, "selection_not_authorized") {
					t.Fatalf("direct provider call=%q", got)
				}
			}
			if got := callback.ExecuteTool(name, tc.requestBody()); got != "family-result:"+tc.name || called != 1 {
				t.Fatalf("bound call=%q called=%d", got, called)
			}
			if got := callback.ExecuteTool(name, tc.requestBody()); !strings.Contains(got, "invocation_grant_replayed") || called != 1 {
				t.Fatalf("replay=%q called=%d", got, called)
			}
		})
	}
}

// TestSemanticSensitiveFamiliesFailClosedOnMixedUnmappedLabels keeps the
// coverage-unmet contract: a request that mixes a migrated family with a label
// that has no capability rule must not materialize the migrated subset.
func TestSemanticSensitiveFamiliesFailClosedOnMixedUnmappedLabels(t *testing.T) {
	for _, tc := range semanticSensitiveFamilyCases() {
		t.Run(tc.name, func(t *testing.T) {
			h := &IMMessageHandler{}
			classification := &intent.ClassificationResult{
				Primary: tc.label, Secondary: []intent.IntentLabel{semanticUnmigratedFixtureLabel(t)}, Confidence: .98,
			}
			prepared, handled, err := h.semanticPlanForTurnWithClassification("user", "mixed request", "lansenger", "root", "turn", classification)
			if !handled || prepared != nil || err == nil || !strings.Contains(err.Error(), "unmapped capability label") {
				t.Fatalf("prepared=%#v handled=%v err=%v", prepared, handled, err)
			}
		})
	}
}

// TestSemanticExternalEffectFamiliesCatalogRegisteredButUnmanaged pins that
// ssh/browser/computer_use are managed external-effect families with a
// reviewed catalog provision. Unbound SSH misses to leftover (builtin ssh);
// browser and computer_use without a bound runtime stay unmet.
func TestSemanticExternalEffectFamiliesCatalogRegisteredButUnmanaged(t *testing.T) {
	for _, label := range []intent.IntentLabel{intent.LabelSSH, intent.LabelBrowser, intent.LabelComputerUse} {
		classification := intent.ClassificationResult{Primary: label, Confidence: .98}
		if managed, unmapped := imSemanticIntentCoverage(classification); !managed || unmapped != "" {
			t.Fatalf("label %q coverage managed=%v unmapped=%q, want managed external-effect family", label, managed, unmapped)
		}
	}
	assertProvision := func(t *testing.T, registry *ToolRegistry, name string, capability tool.CapabilityID) {
		t.Helper()
		registered, ok := registry.Get(name)
		if !ok || registered.SemanticCatalogState != SemanticCatalogCapability {
			t.Fatalf("%s registration=%#v found=%v, want capability catalog state", name, registered, ok)
		}
		if len(registered.CapabilityProvisions) != 1 || registered.CapabilityProvisions[0].Capability != capability {
			t.Fatalf("%s provisions=%+v, want %s", name, registered.CapabilityProvisions, capability)
		}
		if len(registered.SemanticEffects) != 1 || registered.SemanticEffects[0] != tool.EffectExternalEffect {
			t.Fatalf("%s effects=%+v, want external_effect", name, registered.SemanticEffects)
		}
	}
	h := &IMMessageHandler{registry: NewToolRegistry()}
	registerBuiltinTools(h.registry, h)
	assertProvision(t, h.registry, "ssh", tool.CapabilityShellExecuteRemoteHost)

	browserRegistry := NewToolRegistry()
	registerBrowserTools(browserRegistry, nil)
	assertProvision(t, browserRegistry, MergedBrowserToolName, tool.CapabilityBrowserControlWeb)
	if individual, ok := browserRegistry.Get("browser_navigate"); ok && len(individual.CapabilityProvisions) > 0 {
		t.Fatalf("internal browser dispatch target must not be a catalog provider: %+v", individual.CapabilityProvisions)
	}

	computerRegistry := NewToolRegistry()
	registerComputerUseTools(computerRegistry, nil)
	for _, name := range []string{"computer_observe", "computer_click", "computer_done"} {
		assertProvision(t, computerRegistry, name, tool.CapabilityComputerControlDesktop)
	}
}

// TestSemanticExternalEffectMixedRequestFailsClosed: unbound SSH on a mixed
// turn must miss to leftover (builtin ssh), not HostReject the other family
// as unmet. Browser/CU still fail closed without a bound runtime.
func TestSemanticExternalEffectMixedRequestFailsClosed(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry()}
	registerBuiltinTools(h.registry, h)
	classification := &intent.ClassificationResult{
		Primary: intent.LabelSearch, Secondary: []intent.IntentLabel{intent.LabelSSH}, Confidence: .98,
	}
	prepared, handled, err := h.semanticPlanForTurnWithClassification("user", "search and restart the server", "lansenger", "root", "turn", classification)
	if handled || prepared != nil || err != nil {
		t.Fatalf("search+ssh without ssh runtime must miss to leftover handled=%v err=%v prepared=%#v", handled, err, prepared)
	}
}

// TestSemanticBuiltinExternalEffectStillFailsClosedWithoutReceipt pins the
// execution guard for external effects: only a host-local builtin sensitive
// mutation may cross the legacy text boundary.
func TestSemanticBuiltinExternalEffectStillFailsClosedWithoutReceipt(t *testing.T) {
	builtinExternal := tool.PlannedSelection{
		Provider: tool.ProviderBinding{Kind: "builtin"},
		Effects:  []tool.EffectClass{tool.EffectExternalEffect},
	}
	if !semanticSelectionRequiresReceipt(builtinExternal) || semanticBuiltinLocalMutationSelection(builtinExternal) {
		t.Fatalf("external effect builtin must keep requiring a trusted receipt: %+v", builtinExternal.Effects)
	}
	builtinSensitive := tool.PlannedSelection{
		Provider: tool.ProviderBinding{Kind: "builtin"},
		Effects:  []tool.EffectClass{tool.EffectSensitive},
	}
	if !semanticBuiltinLocalMutationSelection(builtinSensitive) {
		t.Fatalf("sensitive-only builtin selection must use the local mutation receipt boundary")
	}
	builtinLocal := tool.PlannedSelection{
		Provider: tool.ProviderBinding{Kind: "builtin"},
		Effects:  []tool.EffectClass{tool.EffectLocalMutation},
	}
	if !semanticSelectionRequiresReceipt(builtinLocal) || !semanticBuiltinLocalMutationSelection(builtinLocal) {
		t.Fatalf("local_mutation builtin selection must use the local mutation receipt boundary")
	}
	dynamicSensitive := tool.PlannedSelection{
		Provider: tool.ProviderBinding{Kind: "mcp"},
		Effects:  []tool.EffectClass{tool.EffectSensitive},
	}
	if semanticBuiltinLocalMutationSelection(dynamicSensitive) {
		t.Fatalf("dynamic providers must keep their typed coordinator boundary")
	}
}

func TestIMSemanticWorkflowPolicyStateMapping(t *testing.T) {
	cases := []struct {
		policy v2.ToolFilterPolicy
		apply  bool
		want   string
	}{
		{v2.ToolFilterNone, false, ""},
		{v2.ToolFilterFull, true, ""},
		{v2.ToolFilterNone, true, imSemanticPolicyStateBlocked},
		{v2.ToolFilterDocOnly, true, "doc_only"},
		{v2.ToolFilterPlanning, true, "planning"},
		{v2.ToolPolicyOpsControlled, true, "ops_controlled"},
	}
	for _, tc := range cases {
		if got := imSemanticWorkflowPolicyState(tc.policy, tc.apply); got != tc.want {
			t.Fatalf("state(%q, %v)=%q, want %q", tc.policy, tc.apply, got, tc.want)
		}
	}
}

func semanticPolicyDenySet(t *testing.T, workflowPolicy string) map[tool.CapabilityID]bool {
	t.Helper()
	_, constraints, err := imSemanticCapabilityPolicyAdapter().DynamicCapabilityConstraints(
		agentservice.DynamicCapabilityNeedRequest{WorkflowPolicy: workflowPolicy})
	if err != nil {
		t.Fatal(err)
	}
	denied := make(map[tool.CapabilityID]bool, len(constraints))
	for _, constraint := range constraints {
		if constraint.Effect != "deny" || constraint.Authority != tool.AuthorityPolicy {
			t.Fatalf("constraint=%+v, want policy-authority deny", constraint)
		}
		denied[constraint.Capability] = true
	}
	return denied
}

func TestIMSemanticCapabilityPolicyAdapterRestrictedStates(t *testing.T) {
	allSix := []tool.CapabilityID{
		tool.CapabilityDocumentWriteOffice, tool.CapabilityBusinessDataMIS, tool.CapabilityKnowledgeIngestLocal,
		tool.CapabilityShellExecuteRemoteHost, tool.CapabilityBrowserControlWeb, tool.CapabilityComputerControlDesktop,
	}
	for _, state := range []string{"ops_controlled", "planning", imSemanticPolicyStateBlocked} {
		denied := semanticPolicyDenySet(t, state)
		for _, capability := range allSix {
			if !denied[capability] {
				t.Fatalf("state %q must deny %s", state, capability)
			}
		}
	}
	docOnly := semanticPolicyDenySet(t, "doc_only")
	if docOnly[tool.CapabilityDocumentWriteOffice] {
		t.Fatalf("doc_only must keep the office document outcome available")
	}
	for _, capability := range allSix[1:] {
		if !docOnly[capability] {
			t.Fatalf("doc_only must deny %s", capability)
		}
	}
	if constraints, err := (&IMMessageHandler{}).semanticCapabilityPolicyConstraints("user"); err != nil || len(constraints) != 0 {
		t.Fatalf("handler without workflow state constraints=%+v err=%v", constraints, err)
	}
}

// TestSemanticPlannerDeniesMigratedFamilyUnderPolicyConstraint proves the
// adapter output is a real planner input: a denied capability becomes an
// explicit policy_denied unmet need instead of a selection.
func TestSemanticPlannerDeniesMigratedFamilyUnderPolicyConstraint(t *testing.T) {
	registry := newIMSemanticCapabilityRegistry()
	invocationSchema := map[string]interface{}{"type": "object", "properties": map[string]interface{}{}, "additionalProperties": false}
	authorization, err := tool.NewParameterAuthorization(invocationSchema)
	if err != nil {
		t.Fatal(err)
	}
	catalog := tool.NewToolCatalog(registry)
	snapshot, err := catalog.PublishWithCoverage([]tool.ProviderSpec{{
		AdapterName:            "office",
		Binding:                tool.ProviderBinding{Kind: "builtin", ProviderID: "im", ImplementationID: "office", SchemaDigest: tool.SchemaDigest([]byte("office-v1"))},
		ParameterAuthorization: authorization,
		Provides:               []tool.CapabilityProvision{{Capability: tool.CapabilityDocumentWriteOffice, Qualifiers: map[string]string{"format": "spreadsheet"}, Quality: 1}},
		Effects:                []tool.EffectClass{tool.EffectSensitive},
		Ready:                  true,
	}}, tool.CatalogCoverage{State: tool.CatalogCoverageComplete}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	_, constraints, err := imSemanticCapabilityPolicyAdapter().DynamicCapabilityConstraints(
		agentservice.DynamicCapabilityNeedRequest{WorkflowPolicy: "ops_controlled"})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := tool.NewToolPlanner(registry).Plan(tool.RouteRequest{
		RootTaskID: "root", SessionID: "user", TurnID: "turn", ChannelScope: "desktop",
		Snapshot:    snapshot,
		Needs:       []tool.CapabilityNeed{{ID: "need-office", Capability: tool.CapabilityDocumentWriteOffice, Required: true}},
		Constraints: constraints,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Selections) != 0 || len(plan.Unmet) != 1 || plan.Unmet[0].ReasonCode != "policy_denied" {
		t.Fatalf("plan=%+v, want one policy_denied unmet need and no selection", plan)
	}
}
