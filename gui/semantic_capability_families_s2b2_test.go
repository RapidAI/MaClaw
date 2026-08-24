package main

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// semanticS2b2FamilyCase describes one builtin administration family migrated
// to governed semantic routing in the S2b2 slice.
type semanticS2b2FamilyCase struct {
	name        string
	label       intent.IntentLabel
	capability  tool.CapabilityID
	adapterName string
}

// semanticS2b2ManagedFamilyCases lists the S2b2 sensitive families whose
// labels have an owner-published capability rule. All of them mutate
// host-local state observed by the host itself, so every selection crosses
// the builtin local mutation receipt boundary.
func semanticS2b2ManagedFamilyCases() []semanticS2b2FamilyCase {
	return []semanticS2b2FamilyCase{
		{name: "app_launch", label: intent.LabelAppLaunch, capability: tool.CapabilitySystemLaunchLocal, adapterName: "open"},
		{name: "file_download", label: intent.LabelFileDownload, capability: tool.CapabilityArtifactAcquireRemote, adapterName: "download_file"},
		{name: "config_manage", label: intent.LabelConfigManage, capability: tool.CapabilityConfigManageSelf, adapterName: "manage_config"},
		{name: "memory_manage", label: intent.LabelMemoryManage, capability: tool.CapabilityMemoryManageAgent, adapterName: "memory"},
		{name: "task_track", label: intent.LabelTaskTrack, capability: tool.CapabilityTaskTrackLocal, adapterName: "task"},
		{name: "goal_manage", label: intent.LabelGoalManage, capability: tool.CapabilityGoalManageLongRunning, adapterName: "goal"},
		{name: "template_manage", label: intent.LabelTemplateManage, capability: tool.CapabilityTemplateManageSession, adapterName: "manage_template"},
		{name: "session_manage", label: intent.LabelSessionManage, capability: tool.CapabilitySessionManageCoding, adapterName: "list_sessions"},
		{name: "delegate_task", label: intent.LabelDelegateTask, capability: tool.CapabilityAgentDelegateSubtask, adapterName: "delegate_task"},
		{name: "knowledge_admin", label: intent.LabelKnowledgeAdmin, capability: tool.CapabilityKnowledgeAdminMaintenance, adapterName: "knowledge_maintain"},
	}
}

// semanticS2b2CatalogOnlyCapabilities are annotated on builtin providers but
// deliberately have no intent rule in this slice: the external-effect
// families lack a trusted receipt boundary, and the read-only interaction
// entry has no reliable user-utterance signal.
func semanticS2b2CatalogOnlyCapabilities() []tool.CapabilityID {
	return []tool.CapabilityID{
		tool.CapabilityScheduleManageLocal,
		tool.CapabilityAudioSynthesizeSpeech,
		tool.CapabilityInteractionAskUser,
	}
}

// TestSemanticS2b2FamiliesAreManagedAndMapToGovernedNeeds pins the coverage
// gate and the label → capability projection for every managed S2b2 family.
func TestSemanticS2b2FamiliesAreManagedAndMapToGovernedNeeds(t *testing.T) {
	registry := newIMSemanticCapabilityRegistry()
	for _, tc := range semanticS2b2ManagedFamilyCases() {
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

// TestSemanticS2b2FamiliesExecuteBoundBuiltinHandlers proves the full
// ToolCatalog → ToolPlanner → InvocationIssuer → CatalogRenderer →
// PlanExecutor chain for each managed S2b2 family: the plan selects the
// annotated builtin provider, the model only sees an opaque grant, the
// admitted invocation lands on the legacy handler, a consumed grant cannot
// be replayed, and every selection crosses the builtin local mutation
// receipt boundary.
func TestSemanticS2b2FamiliesExecuteBoundBuiltinHandlers(t *testing.T) {
	for _, tc := range semanticS2b2ManagedFamilyCases() {
		if tc.adapterName == "knowledge_maintain" || tc.adapterName == "manage_config" ||
			tc.adapterName == "memory" || tc.adapterName == "task" ||
			tc.adapterName == "goal" || tc.adapterName == "manage_template" ||
			tc.adapterName == "list_sessions" || tc.adapterName == "delegate_task" ||
			tc.adapterName == "download_file" {
			// Managed knowledge/config/memory/task/goal/template/session/download
			// no longer bind the legacy soups.
			continue
		}
		t.Run(tc.name, func(t *testing.T) {
			h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, tc.label)}
			called := 0
			if err := h.registry.Register(RegisteredTool{
				Name: tc.adapterName, Description: "untrusted family description", Status: RegToolAvailable,
				InputSchema:          map[string]interface{}{},
				CapabilityProvisions: []tool.CapabilityProvision{{Capability: tc.capability, Quality: 1}},
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

// TestSemanticS2b2FamiliesFailClosedOnMixedUnmappedLabels keeps the
// coverage-unmet contract: a request that mixes an S2b2 family with a label
// that has no capability rule must not materialize the migrated subset.
func TestSemanticS2b2FamiliesFailClosedOnMixedUnmappedLabels(t *testing.T) {
	for _, tc := range semanticS2b2ManagedFamilyCases() {
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

// TestSemanticS2b2CatalogOnlyFamiliesStayUnmanaged pins the remaining
// catalog-only capabilities: they have no intent rule. schedule_manage is
// now mapped to schedule.administer.local; mixed file_read+ssh still
// fail-closes on the unmapped ssh label.
func TestSemanticS2b2CatalogOnlyFamiliesStayUnmanaged(t *testing.T) {
	for _, capability := range semanticS2b2CatalogOnlyCapabilities() {
		for label, templates := range imSemanticIntentRuleSet {
			for _, template := range templates {
				if template.Capability == capability {
					t.Fatalf("label %q must not map to catalog-only capability %s in this slice", label, capability)
				}
			}
		}
	}
	classification := intent.ClassificationResult{Primary: intent.LabelScheduleManage, Confidence: .98}
	if managed, unmapped := imSemanticIntentCoverage(classification); !managed || unmapped != "" {
		t.Fatalf("schedule_manage coverage managed=%v unmapped=%q, want managed administer", managed, unmapped)
	}
	h := &IMMessageHandler{registry: NewToolRegistry()}
	registerBuiltinTools(h.registry, h)
	mixed := &intent.ClassificationResult{
		Primary: intent.LabelFileRead, Secondary: []intent.IntentLabel{intent.LabelSSH}, Confidence: .98,
	}
	prepared, handled, err := h.semanticPlanForTurnWithClassification("user", "read a file and restart the server", "lansenger", "root", "turn", mixed)
	if !handled || err == nil || !strings.Contains(err.Error(), "unmet") {
		t.Fatalf("file_read+ssh without ssh runtime must be unmet, prepared=%#v handled=%v err=%v", prepared, handled, err)
	}
}

// TestSemanticS2b2BuiltinCatalogAnnotations verifies the real registration
// sites: every in-scope S2b2 tool carries its reviewed capability provision
// and effect class.
func TestSemanticS2b2BuiltinCatalogAnnotations(t *testing.T) {
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
			t.Fatalf("%s provisions=%+v, want exactly %s", name, registered.CapabilityProvisions, capability)
		}
		if len(registered.SemanticEffects) != 1 || registered.SemanticEffects[0] != effect {
			t.Fatalf("%s effects=%+v, want %s", name, registered.SemanticEffects, effect)
		}
	}

	// Managed sensitive families.
	assertProvision(t, "open", tool.CapabilitySystemLaunchLocal, tool.EffectSensitive)
	assertProvision(t, "download_file", tool.CapabilityArtifactAcquireRemote, tool.EffectSensitive)
	for _, name := range []string{
		"manage_config", "switch_llm_provider", "set_max_iterations", "manage_user_model",
		"get_config", "update_config", "batch_update_config", "list_config_schema", "export_config", "import_config",
	} {
		assertProvision(t, name, tool.CapabilityConfigManageSelf, tool.EffectSensitive)
	}
	assertProvision(t, "memory", tool.CapabilityMemoryManageAgent, tool.EffectSensitive)
	assertProvision(t, "task", tool.CapabilityTaskTrackLocal, tool.EffectSensitive)
	assertProvision(t, "schedule_administer", tool.CapabilityScheduleAdministerLocal, tool.EffectLocalMutation)
	assertProvision(t, "tts_local", tool.CapabilityAudioSynthesizeLocal, tool.EffectLocalMutation)
	assertProvision(t, "tts_render", tool.CapabilityAudioRenderSpeech, tool.EffectLocalMutation)
	assertProvision(t, "goal", tool.CapabilityGoalManageLongRunning, tool.EffectSensitive)
	for _, name := range []string{"manage_template", "create_template", "list_templates", "launch_template"} {
		assertProvision(t, name, tool.CapabilityTemplateManageSession, tool.EffectSensitive)
	}
	for _, name := range []string{
		"list_sessions", "project_manage", "list_providers", "send_input",
		"get_session_output", "get_session_events", "interrupt_session", "kill_session",
	} {
		assertProvision(t, name, tool.CapabilitySessionManageCoding, tool.EffectSensitive)
	}
	for _, name := range []string{"delegate_task", "parallel_execute"} {
		assertProvision(t, name, tool.CapabilityAgentDelegateSubtask, tool.EffectSensitive)
	}
	for _, name := range []string{
		"knowledge_maintain", "knowledge_delete_source", "knowledge_disable_source",
		"knowledge_enable_source", "knowledge_refresh_source", "knowledge_stats",
		"knowledge_execute_quality_maintenance_plan", "knowledge_list_sources",
	} {
		assertProvision(t, name, tool.CapabilityKnowledgeAdminMaintenance, tool.EffectSensitive)
	}

	// Catalog-only external families.
	for _, name := range []string{
		"manage_schedule", "create_scheduled_task", "list_scheduled_tasks",
		"delete_scheduled_task", "update_scheduled_task",
	} {
		assertProvision(t, name, tool.CapabilityScheduleManageLocal, tool.EffectExternalEffect)
	}
	assertProvision(t, "tts", tool.CapabilityAudioSynthesizeSpeech, tool.EffectExternalEffect)
	assertProvision(t, "send_file", tool.CapabilityMessageSendIM, tool.EffectExternalEffect)
	assertProvision(t, "set_nickname", tool.CapabilityMessageSendIM, tool.EffectExternalEffect)
	for _, name := range []string{"git_commit", "git_push"} {
		assertProvision(t, name, tool.CapabilityRepoMutateVCS, tool.EffectExternalEffect)
	}

	// Catalog-only read-only interaction entry.
	assertProvision(t, "ask_user", tool.CapabilityInteractionAskUser, tool.EffectReadOnly)

	// experience_learning stays legacy-only: its `tool` filter parameter is a
	// reserved invocation field that the closed canonical schema rejects, the
	// same quarantine as query_audit_log's `tool_name`.
	for _, name := range []string{"experience_learning", "query_audit_log"} {
		registered, ok := h.registry.Get(name)
		if !ok {
			t.Fatalf("%s missing from registry", name)
		}
		if len(registered.CapabilityProvisions) != 0 || registered.SemanticCatalogState == SemanticCatalogCapability {
			t.Fatalf("%s must stay outside the semantic capability catalog: %+v", name, registered.CapabilityProvisions)
		}
	}

	// Multi-provision entries: send_to_im keeps the S2a current-channel
	// delivery provision and adds message.send.im; im_message declares both
	// text and file send qualifiers.
	assertMulti := func(t *testing.T, name string, capabilities ...tool.CapabilityID) {
		t.Helper()
		registered, ok := h.registry.Get(name)
		if !ok || len(registered.CapabilityProvisions) != len(capabilities) {
			t.Fatalf("%s provisions=%+v found=%v, want %d provisions", name, registered.CapabilityProvisions, ok, len(capabilities))
		}
		for i, capability := range capabilities {
			if registered.CapabilityProvisions[i].Capability != capability {
				t.Fatalf("%s provision[%d]=%s, want %s", name, i, registered.CapabilityProvisions[i].Capability, capability)
			}
		}
		if len(registered.SemanticEffects) != 1 || registered.SemanticEffects[0] != tool.EffectExternalEffect {
			t.Fatalf("%s effects=%+v, want external_effect", name, registered.SemanticEffects)
		}
	}
	assertMulti(t, "send_to_im", "artifact.deliver.current_channel", tool.CapabilityMessageSendIM)
	assertMulti(t, "im_message", tool.CapabilityMessageSendIM, tool.CapabilityMessageSendIM)
}

// TestSemanticS2b2PolicyAdapterNewFamilies pins the workflow-state projection
// for the S2b2 families. doc_only mirrors the legacy DocOnlyAllowedTools
// allowances: open (system.launch.local), memory (memory.manage.agent) and
// send_file/send_to_im/set_nickname (message.send.im) stay available, every
// other new sensitive/external family is denied. planning, ops_controlled
// and blocked deny all of them.
func TestSemanticS2b2PolicyAdapterNewFamilies(t *testing.T) {
	docOnlyAllowed := []tool.CapabilityID{
		tool.CapabilitySystemLaunchLocal, tool.CapabilityMemoryManageAgent, tool.CapabilityMessageSendIM,
	}
	docOnlyDenied := []tool.CapabilityID{
		tool.CapabilityArtifactAcquireRemote, tool.CapabilityConfigManageSelf,
		tool.CapabilityTaskTrackLocal, tool.CapabilityGoalManageLongRunning,
		tool.CapabilityTemplateManageSession, tool.CapabilitySessionManageCoding,
		tool.CapabilityAgentDelegateSubtask, tool.CapabilityKnowledgeAdminMaintenance,
		tool.CapabilityScheduleManageLocal, tool.CapabilityScheduleDispatchChannel, tool.CapabilityScheduleAdministerLocal, tool.CapabilityAudioSynthesizeLocal, tool.CapabilityAudioRenderSpeech, tool.CapabilityAudioSynthesizeSpeech, tool.CapabilityRepoMutateVCS,
	}
	docOnly := semanticPolicyDenySet(t, "doc_only")
	for _, capability := range docOnlyDenied {
		if !docOnly[capability] {
			t.Fatalf("doc_only must deny %s", capability)
		}
	}
	for _, capability := range docOnlyAllowed {
		if docOnly[capability] {
			t.Fatalf("doc_only keeps the legacy allowance of %s", capability)
		}
	}
	restricted := append(append([]tool.CapabilityID(nil), docOnlyAllowed...), docOnlyDenied...)
	for _, state := range []string{"planning", "ops_controlled", imSemanticPolicyStateBlocked} {
		denied := semanticPolicyDenySet(t, state)
		for _, capability := range restricted {
			if !denied[capability] {
				t.Fatalf("state %q must deny %s", state, capability)
			}
		}
	}
	// The catalog-only read-only interaction entry stays unconstrained in
	// every state, like every read-only family.
	for _, state := range []string{"doc_only", "planning", "ops_controlled", imSemanticPolicyStateBlocked} {
		denied := semanticPolicyDenySet(t, state)
		if denied[tool.CapabilityInteractionAskUser] {
			t.Fatalf("state %q must not constrain read-only %s", state, tool.CapabilityInteractionAskUser)
		}
	}
}
