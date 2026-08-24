package main

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// semanticS2b1FamilyCase describes one builtin capability family migrated to
// governed semantic routing in the S2b1 slice.
type semanticS2b1FamilyCase struct {
	name        string
	label       intent.IntentLabel
	capability  tool.CapabilityID
	adapterName string
	sensitive   bool
}

func semanticS2b1ReadOnlyFamilyCases() []semanticS2b1FamilyCase {
	return []semanticS2b1FamilyCase{
		{name: "fs_read", label: intent.LabelFileRead, capability: tool.CapabilityFSReadLocal, adapterName: "read_file"},
		{name: "git_inspect", label: intent.LabelGitInspect, capability: tool.CapabilityRepoInspectVCS, adapterName: "git_status"},
		{name: "web_fetch", label: intent.LabelWebFetch, capability: tool.CapabilityInformationFetchWeb, adapterName: "web_fetch"},
		{name: "audio_transcribe", label: intent.LabelAudioTranscribe, capability: tool.CapabilityAudioTranscribeSpeech, adapterName: "asr"},
		{name: "audit_read", label: intent.LabelAuditRead, capability: tool.CapabilitySecurityAuditRead, adapterName: "session_search"},
		{name: "knowledge_read", label: intent.LabelKnowledgeRead, capability: tool.CapabilityKnowledgeReadLocal, adapterName: "knowledge_search"},
	}
}

func semanticS2b1SensitiveFamilyCases() []semanticS2b1FamilyCase {
	return []semanticS2b1FamilyCase{
		{name: "file_write", label: intent.LabelFileWrite, capability: tool.CapabilityFSWriteLocal, adapterName: "write_file", sensitive: true},
		{name: "shell_command", label: intent.LabelShellCommand, capability: tool.CapabilityShellExecuteLocal, adapterName: "bash", sensitive: true},
		{name: "audio_record", label: intent.LabelAudioRecord, capability: tool.CapabilityAudioCaptureMicrophone, adapterName: "record_audio", sensitive: true},
	}
}

func semanticS2b1AllFamilyCases() []semanticS2b1FamilyCase {
	return append(semanticS2b1ReadOnlyFamilyCases(), semanticS2b1SensitiveFamilyCases()...)
}

// TestSemanticS2b1FamiliesAreManagedAndMapToGovernedNeeds pins the coverage
// gate and the label → capability projection for every S2b1 family.
func TestSemanticS2b1FamiliesAreManagedAndMapToGovernedNeeds(t *testing.T) {
	registry := newIMSemanticCapabilityRegistry()
	for _, tc := range semanticS2b1AllFamilyCases() {
		t.Run(tc.name, func(t *testing.T) {
			classification := intent.ClassificationResult{Primary: tc.label, Confidence: .98}
			managed, unmapped := imSemanticIntentCoverage(classification)
			if !managed || unmapped != "" {
				t.Fatalf("coverage managed=%v unmapped=%q, want managed without unmapped", managed, unmapped)
			}
			needs, resolvedManaged, err := semanticIntentNeedsFromClassification(registry, classification)
			if err != nil || !resolvedManaged || len(needs) != 1 {
				t.Fatalf("needs=%#v managed=%v err=%v", needs, resolvedManaged, err)
			}
			if needs[0].Capability != tc.capability || !needs[0].Required {
				t.Fatalf("need=%+v, want required %s", needs[0], tc.capability)
			}
		})
	}
}

// TestSemanticS2b1FamiliesExecuteBoundBuiltinHandlers proves the full
// ToolCatalog → ToolPlanner → InvocationIssuer → CatalogRenderer →
// PlanExecutor chain for each migrated S2b1 family: the plan selects the
// annotated builtin provider, the model only sees an opaque grant, the
// admitted invocation lands on the legacy handler, and a consumed grant
// cannot be replayed. Read-only selections must not require a receipt;
// sensitive selections must cross the builtin local mutation receipt
// boundary.
func TestSemanticS2b1FamiliesExecuteBoundBuiltinHandlers(t *testing.T) {
	for _, tc := range semanticS2b1AllFamilyCases() {
		if tc.adapterName == "asr" || tc.adapterName == "session_search" || tc.adapterName == "write_file" || tc.adapterName == "knowledge_search" || tc.adapterName == "read_file" || tc.adapterName == "git_status" || tc.adapterName == "web_fetch" || tc.adapterName == "bash" {
			// Managed transcribe/audit/file-write/knowledge-read/file-read/repo-inspect/web-fetch
			// no longer bind the path-taking asr adapter, session_search,
			// write_file / edit_file, knowledge_search, read_file,
			// git_status / git_diff, or web_fetch soup.
			continue
		}
		t.Run(tc.name, func(t *testing.T) {
			effect := tool.EffectReadOnly
			if tc.sensitive {
				effect = tool.EffectSensitive
			}
			h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, tc.label)}
			called := 0
			if err := h.registry.Register(RegisteredTool{
				Name: tc.adapterName, Description: "untrusted family description", Status: RegToolAvailable,
				InputSchema:          map[string]interface{}{},
				CapabilityProvisions: []tool.CapabilityProvision{{Capability: tc.capability, Quality: 1}},
				SemanticEffects:      []tool.EffectClass{effect},
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
			if tc.sensitive {
				if !semanticSelectionRequiresReceipt(selection) || !semanticBuiltinLocalMutationSelection(selection) {
					t.Fatalf("sensitive builtin selection must use the local mutation receipt boundary: %+v", selection.Effects)
				}
			} else if semanticSelectionRequiresReceipt(selection) {
				t.Fatalf("read-only selection must not require a receipt: %+v", selection.Effects)
			}
			callback := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface}
			if name != tc.adapterName {
				if got := callback.ExecuteTool(tc.adapterName, `{}`); !strings.Contains(got, "selection_not_authorized") {
					t.Fatalf("direct provider call=%q", got)
				}
			}
			if got := callback.ExecuteTool(name, `{}`); got != "family-result:"+tc.name || called != 1 {
				t.Fatalf("bound call=%q called=%d", got, called)
			}
			if got := callback.ExecuteTool(name, `{}`); !strings.Contains(got, "invocation_grant_replayed") || called != 1 {
				t.Fatalf("replay=%q called=%d", got, called)
			}
		})
	}
}

// TestSemanticS2b1FamiliesFailClosedOnMixedUnmappedLabels keeps the
// coverage-unmet contract: a request that mixes an S2b1 family with a label
// that has no capability rule must not materialize the migrated subset.
func TestSemanticS2b1FamiliesFailClosedOnMixedUnmappedLabels(t *testing.T) {
	for _, tc := range semanticS2b1AllFamilyCases() {
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

// TestSemanticS2b1BuiltinCatalogAnnotations verifies the real registration
// sites: every in-scope tool carries its reviewed capability provision and
// effect class, while out-of-scope mutation/admin tools stay catalog-only or
// quarantined.
func TestSemanticS2b1BuiltinCatalogAnnotations(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry()}
	registerBuiltinTools(h.registry, h)
	app := &App{testHomeDir: t.TempDir()}
	registerNonCodeTools(h.registry, app)
	registerKnowledgeTools(h.registry, app)

	assertProvision := func(t *testing.T, name string, capability tool.CapabilityID, effect tool.EffectClass) {
		t.Helper()
		registered, ok := h.registry.Get(name)
		if !ok || registered.SemanticCatalogState != SemanticCatalogCapability {
			t.Fatalf("%s registration found=%v, want capability catalog state", name, ok)
		}
		if len(registered.CapabilityProvisions) != 1 || registered.CapabilityProvisions[0].Capability != capability {
			t.Fatalf("%s provisions=%+v, want %s", name, registered.CapabilityProvisions, capability)
		}
		if len(registered.SemanticEffects) != 1 || registered.SemanticEffects[0] != effect {
			t.Fatalf("%s effects=%+v, want %s", name, registered.SemanticEffects, effect)
		}
	}
	assertProvision(t, "read_file", tool.CapabilityFSReadLocal, tool.EffectReadOnly)
	assertProvision(t, "read_tool_result", tool.CapabilityFSReadLocal, tool.EffectReadOnly)
	assertProvision(t, "list_directory", tool.CapabilityFSReadLocal, tool.EffectReadOnly)
	assertProvision(t, "search_files", tool.CapabilityFSReadLocal, tool.EffectReadOnly)
	assertProvision(t, "git_status", tool.CapabilityRepoInspectVCS, tool.EffectReadOnly)
	assertProvision(t, "git_diff", tool.CapabilityRepoInspectVCS, tool.EffectReadOnly)
	assertProvision(t, "web_fetch", tool.CapabilityInformationFetchWeb, tool.EffectReadOnly)
	assertProvision(t, "asr", tool.CapabilityAudioTranscribeSpeech, tool.EffectReadOnly)
	assertProvision(t, "session_search", tool.CapabilitySecurityAuditRead, tool.EffectReadOnly)
	assertProvision(t, "knowledge_search", tool.CapabilityKnowledgeReadLocal, tool.EffectReadOnly)
	assertProvision(t, "knowledge_context_pack", tool.CapabilityKnowledgeReadLocal, tool.EffectReadOnly)
	assertProvision(t, "write_file", tool.CapabilityFSWriteLocal, tool.EffectSensitive)
	assertProvision(t, "edit_file", tool.CapabilityFSWriteLocal, tool.EffectSensitive)
	assertProvision(t, "bash", tool.CapabilityShellExecuteLocal, tool.EffectSensitive)
	assertProvision(t, "passthrough_task", tool.CapabilityShellExecuteLocal, tool.EffectSensitive)
	assertProvision(t, "record_audio", tool.CapabilityAudioCaptureMicrophone, tool.EffectSensitive)

	// repo.mutate.vcs and the knowledge admin/maintenance surface were still
	// out of scope when this slice landed; S2b2 has since catalog-registered
	// them (see semantic_capability_families_s2b2_test.go). query_audit_log
	// stays legacy-only: its `tool_name` filter parameter is a reserved
	// invocation field that the closed canonical schema rejects. check_health
	// is project compile health and must not provide security.audit.read.
	for _, name := range []string{"query_audit_log", "check_health"} {
		registered, ok := h.registry.Get(name)
		if !ok {
			t.Fatalf("%s missing from registry", name)
		}
		if len(registered.CapabilityProvisions) != 0 || registered.SemanticCatalogState == SemanticCatalogCapability {
			t.Fatalf("%s must stay outside the semantic capability catalog: %+v", name, registered.CapabilityProvisions)
		}
	}
}

// TestSemanticS2b1PolicyAdapterNewFamilies pins the workflow-state projection
// for the S2b1 families: doc_only mirrors the legacy allowance of local shell
// helpers but denies file writes and microphone capture; planning,
// ops_controlled and blocked deny all three sensitive families; the read-only
// families are unconstrained in every state.
func TestSemanticS2b1PolicyAdapterNewFamilies(t *testing.T) {
	newSensitive := []tool.CapabilityID{
		tool.CapabilityFSWriteLocal, tool.CapabilityShellExecuteLocal, tool.CapabilityAudioCaptureMicrophone,
	}
	newReadOnly := []tool.CapabilityID{
		tool.CapabilityFSReadLocal, tool.CapabilityRepoInspectVCS, tool.CapabilityInformationFetchWeb,
		tool.CapabilityAudioTranscribeSpeech, tool.CapabilitySecurityAuditRead, tool.CapabilityKnowledgeReadLocal,
	}

	docOnly := semanticPolicyDenySet(t, "doc_only")
	if !docOnly[tool.CapabilityFSWriteLocal] || !docOnly[tool.CapabilityAudioCaptureMicrophone] {
		t.Fatalf("doc_only must deny fs.write.local and audio.capture.microphone: %+v", docOnly)
	}
	if docOnly[tool.CapabilityShellExecuteLocal] {
		t.Fatalf("doc_only keeps the legacy local-shell helper allowance")
	}
	for _, state := range []string{"planning", "ops_controlled", imSemanticPolicyStateBlocked} {
		denied := semanticPolicyDenySet(t, state)
		for _, capability := range newSensitive {
			if !denied[capability] {
				t.Fatalf("state %q must deny %s", state, capability)
			}
		}
	}
	for _, state := range []string{"doc_only", "planning", "ops_controlled", imSemanticPolicyStateBlocked} {
		denied := semanticPolicyDenySet(t, state)
		for _, capability := range newReadOnly {
			if denied[capability] {
				t.Fatalf("state %q must not constrain read-only %s", state, capability)
			}
		}
	}
}
