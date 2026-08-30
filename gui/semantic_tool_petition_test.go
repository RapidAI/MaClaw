package main

import (
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

func petitionTestOfficeCallbacks(t *testing.T, classification *intent.ClassificationResult) *sharedAgentLoopCallbacks {
	t.Helper()
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, classification.Primary)}
	h.semanticTrustedOfficeWrite = func(string, string, map[string]interface{}) (string, error) { return "ok", nil }
	h.semanticTrustedWebSearch = func(userID, query string) (string, error) { return "found: " + query, nil }
	registerBuiltinTools(h.registry, h)
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "网上找几张布偶猫照片做成生日PPT", "desktop", "root-petition", "turn-petition", classification,
	)
	if err != nil || !handled || surface == nil || len(defs) == 0 {
		t.Fatalf("office surface handled=%v err=%v", handled, err)
	}
	return &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface, platform: "desktop"}
}

// Lookup legs outside the office archetype bundle (search/fetch/download/
// read are now offered deterministically on every office turn) still complete
// through the petition path: the host re-plans with the petitioned label and
// publishes a child revision that carries the grant. current_datetime is the
// remaining lookup leg an office face does not already render.
func TestSemanticToolCallPetitionGrantsDeclaredLookupLeg(t *testing.T) {
	cb := petitionTestOfficeCallbacks(t, &intent.ClassificationResult{Primary: intent.LabelOffice, Confidence: .98})
	parent := cb.semanticSurface
	if semanticGrantNameForAdapter(parent, semanticTrustedClockAdapter) != "" {
		t.Fatal("fixture must start without a clock grant")
	}
	granted, message := cb.PetitionToolCall("current_datetime")
	if !granted || !strings.Contains(message, "current_datetime") || !strings.Contains(message, "授权") {
		t.Fatalf("granted=%v message=%q", granted, message)
	}
	child := cb.semanticSurface
	if child == parent {
		t.Fatal("petition must publish a child revision")
	}
	if !planHasCapabilities(child.plan, tool.CapabilityDocumentWriteOffice, "artifact.deliver.current_channel", "information.current_time") {
		t.Fatalf("child plan=%#v", child.plan.Selections)
	}
	if name := semanticGrantNameForAdapter(child, semanticTrustedClockAdapter); name != "current_datetime" {
		t.Fatalf("child clock grant name=%q, want current_datetime", name)
	}
	// The parent authority survives: the office grant is still bound.
	if name := semanticGrantNameForAdapter(child, semanticTrustedOfficeWriteAdapter); name != "office" {
		t.Fatalf("child office grant name=%q, want office", name)
	}
	// The next request rebuild observes the widened surface.
	defs := cb.BuildToolsForModelRequest("网上找几张布偶猫照片做成生日PPT", 1)
	found := false
	for _, def := range defs {
		if extractToolName(def) == "current_datetime" {
			found = true
		}
	}
	if !found {
		t.Fatalf("widened request surface lacks current_datetime: %#v", defs)
	}
	// The child replan input must carry the expanded classification so a later
	// failure replan keeps the petitioned leg.
	if !classificationHasLabel(child.replan.Classification, intent.LabelCurrentTime) {
		t.Fatalf("child replan classification=%+v", child.replan.Classification)
	}
	// The expansion did not consume the failure-replan budget.
	if child.replan.Attempts != parent.replan.Attempts {
		t.Fatalf("child attempts=%d, parent=%d", child.replan.Attempts, parent.replan.Attempts)
	}
}

// The lookup budget is one expansion per turn: after a granted petition the
// flag is spent, and any further lookup name is denied — an already-rendered
// one by the grant recheck, anything else by the budget.
func TestSemanticToolCallPetitionBudgetIsOnePerTurn(t *testing.T) {
	cb := petitionTestOfficeCallbacks(t, &intent.ClassificationResult{Primary: intent.LabelOffice, Confidence: .98})
	granted, _ := cb.PetitionToolCall("current_datetime")
	if !granted {
		t.Fatal("first petition must be granted")
	}
	if !cb.semanticPetitionConsumed {
		t.Fatal("a granted lookup petition must consume the lookup budget")
	}
	if granted, message := cb.PetitionToolCall("web_search"); granted || message != "" {
		t.Fatalf("second petition must be denied: granted=%v message=%q", granted, message)
	}
}

// Read-only legs are petitionable on any primary: the agent decides what the
// turn needs, and the harness no longer judges the pairing. A shell turn may
// therefore gain web_search through one read-only petition. Unknown names are
// still denied, and a denied petition never consumes the turn's budget.
func TestSemanticToolCallPetitionAdmitsReadOnlyLegOnAnyPrimary(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelShellCommand)}
	h.semanticTrustedShell = func(string, string, time.Duration) (string, error) { return "ok", nil }
	h.semanticTrustedWebSearch = func(userID, query string) (string, error) { return "found: " + query, nil }
	registerBuiltinTools(h.registry, h)
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "清空当前目录", "desktop", "root-petition-shell", "turn-petition-shell",
		&intent.ClassificationResult{Primary: intent.LabelShellCommand, Confidence: .98},
	)
	if err != nil || !handled || surface == nil || len(defs) == 0 {
		t.Fatalf("shell surface handled=%v err=%v", handled, err)
	}
	cb := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface, platform: "desktop"}
	if semanticGrantNameForAdapter(surface, semanticTrustedWebSearchAdapter) != "" {
		t.Fatal("fixture must start without a search grant")
	}
	granted, message := cb.PetitionToolCall("web_search")
	if !granted || !strings.Contains(message, "web_search") {
		t.Fatalf("read-only petition must be admitted on any primary: granted=%v message=%q", granted, message)
	}
	if !planHasCapabilities(cb.semanticSurface.plan, tool.CapabilityShellExecuteLocal, "information.search.web") {
		t.Fatalf("child plan=%#v", cb.semanticSurface.plan.Selections)
	}
	if name := semanticGrantNameForAdapter(cb.semanticSurface, semanticTrustedWebSearchAdapter); name != "web_search" {
		t.Fatalf("child search grant name=%q, want web_search", name)
	}
	if !cb.semanticPetitionConsumed {
		t.Fatal("a granted read-only petition must consume the read-only budget")
	}

	office := petitionTestOfficeCallbacks(t, &intent.ClassificationResult{Primary: intent.LabelOffice, Confidence: .98})
	if granted, message := office.PetitionToolCall("nonexistent_tool"); granted || message != "" {
		t.Fatalf("unknown name must be denied: granted=%v message=%q", granted, message)
	}
	if office.semanticPetitionConsumed || office.semanticEffectfulPetitionConsumed {
		t.Fatal("a gate-denied petition must not consume either turn budget")
	}
	// A denied petition leaves the surface untouched.
	if office.semanticSurface == nil || len(office.semanticSurface.grants) == 0 {
		t.Fatal("surface must survive denied petitions")
	}
}

// Effectful petitions are the agent's problem-solving fallback: an office
// turn whose rendered surface cannot finish the job (the 2026-08-26 PPT turn
// had no way to craft the deck itself) may petition bash once per turn. The
// child revision carries the whole shell sibling family, so a script can be
// written, run, fixed and rerun inside the same turn.
func TestSemanticToolCallPetitionGrantsEffectfulShellLeg(t *testing.T) {
	cb := petitionTestOfficeCallbacks(t, &intent.ClassificationResult{Primary: intent.LabelOffice, Confidence: .98})
	parent := cb.semanticSurface
	if semanticGrantNameForAdapter(parent, semanticTrustedShellAdapter) != "" {
		t.Fatal("fixture must start without a shell grant")
	}
	granted, message := cb.PetitionToolCall("bash")
	if !granted || !strings.Contains(message, "bash") {
		t.Fatalf("granted=%v message=%q", granted, message)
	}
	child := cb.semanticSurface
	if child == parent {
		t.Fatal("petition must publish a child revision")
	}
	if !planHasCapabilities(child.plan, tool.CapabilityDocumentWriteOffice, tool.CapabilityShellExecuteLocal) {
		t.Fatalf("child plan=%#v", child.plan.Selections)
	}
	if name := semanticGrantNameForAdapter(child, semanticTrustedShellAdapter); name != "bash" {
		t.Fatalf("child shell grant name=%q, want bash", name)
	}
	// The shell family is budgeted for iterative craft-and-run work.
	shellSiblings := 0
	for _, selection := range child.plan.Selections {
		if selection.FitProof.MatchedCapability == tool.CapabilityShellExecuteLocal {
			shellSiblings++
		}
	}
	if shellSiblings < 2 {
		t.Fatalf("petitioned shell family must carry an invocation budget, got %d selection(s)", shellSiblings)
	}
	// The parent authority survives.
	if name := semanticGrantNameForAdapter(child, semanticTrustedOfficeWriteAdapter); name != "office" {
		t.Fatalf("child office grant name=%q, want office", name)
	}
	// The effectful budget is spent; the lookup budget is independent and
	// still available for the same turn.
	if granted, message := cb.PetitionToolCall("delegate_task"); granted || message != "" {
		t.Fatalf("second effectful petition must be denied by the budget: granted=%v message=%q", granted, message)
	}
	if granted, message := cb.PetitionToolCall("current_datetime"); !granted || !strings.Contains(message, "current_datetime") {
		t.Fatalf("lookup petition must remain available after an effectful one: granted=%v message=%q", granted, message)
	}
}

// delegate_task is the other effectful petition: a turn that needs real
// coding ability (skills, multi-step code) can hand one bounded subtask to
// the coding agent. The delegate provider only publishes when a host app
// exists, so this uses an app-backed handler.
func TestSemanticToolCallPetitionGrantsEffectfulDelegateLeg(t *testing.T) {
	app := newProjectSearchTestApp(t)
	defer app.closeSemanticInvocationStore()
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelOffice), app: app}
	h.semanticTrustedOfficeWrite = func(string, string, map[string]interface{}) (string, error) { return "ok", nil }
	registerBuiltinTools(h.registry, h)
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		desktopUserID, "做个生日PPT", "desktop", "root-petition-delegate", "turn-petition-delegate",
		&intent.ClassificationResult{Primary: intent.LabelOffice, Confidence: .98},
	)
	if err != nil || !handled || surface == nil || len(defs) == 0 {
		t.Fatalf("office surface handled=%v err=%v", handled, err)
	}
	cb := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface, platform: "desktop"}
	granted, message := cb.PetitionToolCall("delegate_task")
	if !granted || !strings.Contains(message, "delegate_task") {
		t.Fatalf("granted=%v message=%q", granted, message)
	}
	child := cb.semanticSurface
	if !planHasCapabilities(child.plan, tool.CapabilityDocumentWriteOffice, tool.CapabilityAgentDelegateSubtask) {
		t.Fatalf("child plan=%#v", child.plan.Selections)
	}
	if name := semanticGrantNameForAdapter(child, semanticTrustedDelegateAdapter); name != "delegate_task" {
		t.Fatalf("child delegate grant name=%q, want delegate_task", name)
	}
}

// Group-restricted contexts deny local administration at execution time, so
// the petition must fail there too instead of surfacing a tool that can only
// be rejected. The denial does not consume the effectful budget.
func TestSemanticToolCallPetitionDeniesEffectfulUnderGroupPolicy(t *testing.T) {
	cb := petitionTestOfficeCallbacks(t, &intent.ClassificationResult{Primary: intent.LabelOffice, Confidence: .98})
	cb.loopCtx = &LoopContext{LansengerGroupPermissions: &lansengerGroupPermissionPolicy{}}
	if granted, message := cb.PetitionToolCall("bash"); granted || message != "" {
		t.Fatalf("group policy must deny a shell petition: granted=%v message=%q", granted, message)
	}
	if cb.semanticEffectfulPetitionConsumed {
		t.Fatal("a policy-denied petition must not consume the effectful budget")
	}
	// The group gate itself only blocks effectful legs: a lookup petition is
	// admitted by its own composite gate and spends its own budget (the
	// re-plan may still deny it under a restrictive policy, which is fine).
	if granted, _ := cb.PetitionToolCall("current_datetime"); granted {
		t.Fatal("restrictive group policy must not gain current_datetime either")
	}
	if !cb.semanticPetitionConsumed {
		t.Fatal("lookup petition must pass the group gate and consume its own budget")
	}
}

// The petition label resolves from the rule set alone, deterministically:
// a single-required-template label wins, the lookup preference order breaks
// ties, and a capability no rule backs is not petitionable. The turn's
// classification no longer participates in the decision.
func TestSemanticPetitionLabelResolutionIsDeterministic(t *testing.T) {
	cases := []struct {
		capability tool.CapabilityID
		want       intent.IntentLabel
	}{
		{"information.search.web", intent.LabelSearch},                         // preference order beats live_data
		{tool.CapabilityInformationFetchWeb, intent.LabelWebFetch},             //
		{"information.current_time", intent.LabelCurrentTime},                  //
		{tool.CapabilityFSReadLocal, intent.LabelFileRead},                     // sole required beats the coding rule
		{tool.CapabilityFSWriteLocal, intent.LabelFileWrite},                   //
		{tool.CapabilityRepoInspectVCS, intent.LabelGitInspect},                //
		{tool.CapabilityRepoMutateVCS, intent.LabelGitMutate},                  //
		{tool.CapabilityShellExecuteLocal, intent.LabelShellCommand},           //
		{tool.CapabilityAgentDelegateSubtask, intent.LabelDelegateTask},        //
		{tool.CapabilityArtifactAcquireRemote, intent.LabelFileDownload},       //
		{tool.CapabilityDocumentWriteOffice, intent.LabelOffice},               // delivery leg stays optional
		{"document.generate.file", intent.LabelDocumentGenerate},               // multi-need label, whole label admitted
		{"visual.capture.desktop", intent.LabelScreenshot},                     //
		{"artifact.deliver.current_channel", intent.LabelAttachmentDelivery},   //
		{"artifact.deliver.specified_target", intent.LabelDocumentDelivery},    //
		{tool.CapabilitySystemLaunchLocal, intent.LabelAppLaunch},              // lexicographic tie-break
		{tool.CapabilityShellExecuteRemoteHost, intent.LabelSSH},               //
		{tool.CapabilityBrowserControlWeb, intent.LabelBrowser},                //
		{tool.CapabilityComputerControlDesktop, intent.LabelComputerUse},       //
		{tool.CapabilityAudioTranscribeSpeech, intent.LabelAudioTranscribe},    //
		{tool.CapabilityAudioCaptureMicrophone, intent.LabelAudioRecord},       //
		{tool.CapabilitySecurityAuditRead, intent.LabelAuditRead},              //
		{tool.CapabilityKnowledgeReadLocal, intent.LabelKnowledgeRead},         //
		{tool.CapabilityKnowledgeIngestLocal, intent.LabelKnowledgeWrite},      //
		{tool.CapabilityKnowledgeAdminMaintenance, intent.LabelKnowledgeAdmin}, //
		{tool.CapabilityMemoryManageAgent, intent.LabelMemoryManage},           //
		{tool.CapabilityTaskTrackLocal, intent.LabelTaskTrack},                 //
		{tool.CapabilityGoalManageLongRunning, intent.LabelGoalManage},         //
		{tool.CapabilityTemplateManageSession, intent.LabelTemplateManage},     //
		{tool.CapabilitySessionManageCoding, intent.LabelSessionManage},        //
		{tool.CapabilityScheduleAdministerLocal, intent.LabelScheduleManage},   // dispatch rule is multi-need
		{tool.CapabilityConfigManageSelf, intent.LabelConfigManage},            //
		{tool.CapabilityBusinessDataMIS, intent.LabelBusinessData},             // read leg stays optional
	}
	for _, tc := range cases {
		label, ok := semanticPetitionLabelForCapability(tc.capability)
		if !ok || label != tc.want {
			t.Errorf("capability %s: label=%q ok=%v, want %q", tc.capability, label, ok, tc.want)
		}
		// Map iteration order must not leak into the result.
		again, okAgain := semanticPetitionLabelForCapability(tc.capability)
		if !okAgain || again != label {
			t.Errorf("capability %s: resolution is not deterministic (%q vs %q)", tc.capability, label, again)
		}
	}
	if _, ok := semanticPetitionLabelForCapability(tool.CapabilityMessageSendIM); ok {
		t.Fatal("quarantined capability without a rule label must not resolve")
	}
	if _, ok := semanticPetitionLabelForCapability(tool.CapabilityMemoryRecallAgent); ok {
		t.Fatal("ambient-only capability without a rule label must not resolve")
	}
}

// A turn whose plan is exhausted after a successful current-channel delivery
// is done: the closing LLM call would only produce summary text while paying
// full latency and outage exposure (production 2026-08-26: ~65s of 502
// retries after the deck was already delivered). EarlyStop must then fire as
// a clean, code-less stop; an exhausted plan without a delivery, or a
// delivered plan with selections still open, must not.
func TestSemanticEarlyStopAfterDeliveryCompletesPlan(t *testing.T) {
	cb := petitionTestOfficeCallbacks(t, &intent.ClassificationResult{Primary: intent.LabelOffice, Confidence: .98})
	surface := cb.semanticSurface
	if stop, code, _ := cb.EarlyStop(); stop || code != "" {
		t.Fatalf("fresh turn must not stop: stop=%v code=%q", stop, code)
	}
	deliverID := ""
	for _, selection := range surface.plan.Selections {
		if selection.AdapterName == "semantic_deliver_current_file" {
			deliverID = selection.ID
			continue
		}
		surface.completed[selection.ID] = true
	}
	if deliverID == "" {
		t.Fatal("fixture plan must include a current-channel file delivery selection")
	}
	if stop, _, _ := cb.EarlyStop(); stop {
		t.Fatal("plan exhausted without a delivery must not stop: the model never delivered")
	}
	surface.completed[deliverID] = true
	stop, code, text := cb.EarlyStop()
	if !stop || code != "" || text != "" {
		t.Fatalf("delivered+exhausted turn: stop=%v code=%q text=%q, want clean code-less stop", stop, code, text)
	}
}

// Optional offers (the archetype bundle legs, ambient retrieval) are
// not turn goals: an office turn that wrote and delivered the deck without
// ever calling the optional search leg is done and must clean-stop. The
// all-selections variant of this check never fired in production because the
// ambient/bundle legs are almost never called.
func TestSemanticEarlyStopIgnoresUnclaimedOptionalLegs(t *testing.T) {
	cb := petitionTestOfficeCallbacks(t, &intent.ClassificationResult{Primary: intent.LabelOffice, Confidence: .98})
	surface := cb.semanticSurface
	optionalLeft := 0
	for _, selection := range surface.plan.Selections {
		// Leave the optional bundle search offers uncompleted; complete the rest.
		if selection.FitProof.MatchedCapability == "information.search.web" {
			optionalLeft++
			continue
		}
		surface.completed[selection.ID] = true
	}
	if optionalLeft == 0 {
		t.Fatal("fixture plan must include the optional bundle search offers")
	}
	stop, code, _ := cb.EarlyStop()
	if !stop || code != "" {
		t.Fatalf("unclaimed optional legs must not block the clean stop: stop=%v code=%q", stop, code)
	}
}

// The archetype bundle lookup offer on an office turn carries the same 5x
// repeat budget as a declared search label: a document turn legitimately
// searches more than once, and the single-shot companion stranded production
// PPT turns after the first search.
func TestSemanticOfficeCompanionSearchBudgetIsThree(t *testing.T) {
	cb := petitionTestOfficeCallbacks(t, &intent.ClassificationResult{Primary: intent.LabelOffice, Confidence: .98})
	for i := 1; i <= 5; i++ {
		if got := cb.ExecuteTool("web_search", `{"query":"ragdoll photos"}`); !strings.Contains(got, "found:") {
			t.Fatalf("bundle search %d must succeed through a sibling grant: %q", i, got)
		}
	}
	if got := cb.ExecuteTool("web_search", `{"query":"ragdoll photos"}`); strings.Contains(got, "found:") {
		t.Fatalf("sixth bundle search must be denied: %q", got)
	}
}

// The acquire half of a document turn's chain is petitionable too: without
// it the model re-searches image URLs it can never use until the lookup
// budget runs out (2026-08-26 PPT turns). Office turns now offer
// download_file through the archetype bundle, so the petition path is
// exercised from a shell turn, whose bundle carries only the file legs.
// Same effectful gate as bash: one effectful petition per turn,
// group-restricted contexts deny it.
func petitionTestShellCallbacks(t *testing.T) *sharedAgentLoopCallbacks {
	t.Helper()
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelShellCommand)}
	h.semanticTrustedShell = func(string, string, time.Duration) (string, error) { return "ok", nil }
	registerBuiltinTools(h.registry, h)
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "清空当前目录", "desktop", "root-petition-shell", "turn-petition-shell",
		&intent.ClassificationResult{Primary: intent.LabelShellCommand, Confidence: .98},
	)
	if err != nil || !handled || surface == nil || len(defs) == 0 {
		t.Fatalf("shell surface handled=%v err=%v", handled, err)
	}
	return &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface, platform: "desktop"}
}

func TestSemanticToolCallPetitionGrantsDownloadLeg(t *testing.T) {
	cb := petitionTestShellCallbacks(t)
	granted, message := cb.PetitionToolCall("download_file")
	if !granted || !strings.Contains(message, "download_file") {
		t.Fatalf("granted=%v message=%q", granted, message)
	}
	if !planHasCapabilities(cb.semanticSurface.plan, tool.CapabilityShellExecuteLocal, tool.CapabilityArtifactAcquireRemote) {
		t.Fatalf("child plan=%#v", cb.semanticSurface.plan.Selections)
	}
	// The shared effectful budget is now spent: bash must be denied.
	if granted, _ := cb.PetitionToolCall("bash"); granted {
		t.Fatal("second effectful petition in one turn must be denied")
	}
}

func TestSemanticToolCallPetitionDeniesDownloadUnderGroupPolicy(t *testing.T) {
	cb := petitionTestShellCallbacks(t)
	cb.loopCtx = &LoopContext{LansengerGroupPermissions: &lansengerGroupPermissionPolicy{}}
	if granted, _ := cb.PetitionToolCall("download_file"); granted {
		t.Fatal("group policy must deny an acquire petition")
	}
	if cb.semanticEffectfulPetitionConsumed {
		t.Fatal("a policy-denied petition must not consume the effectful budget")
	}
}

// Pin A: discoverability and coverage must not drift. Every petitionable name
// must appear in the tools_search inventory — a petitionable name the model
// cannot discover is dead code — and every inventory name must be backed by
// the petition map or the plan-capability map, so the discovery status can
// never fall through to a guess.
func TestSemanticPetitionableNamesMatchToolsSearchInventory(t *testing.T) {
	inventory := make(map[string]bool, len(semanticToolsSearchInventory))
	for _, entry := range semanticToolsSearchInventory {
		if inventory[entry.name] {
			t.Fatalf("duplicate inventory entry %q", entry.name)
		}
		inventory[entry.name] = true
	}
	for name := range semanticPetitionableCapabilities {
		if !inventory[name] {
			t.Errorf("petitionable name %q has no tools_search inventory entry", name)
		}
	}
	for _, entry := range semanticToolsSearchInventory {
		if _, ok := semanticPetitionableCapabilities[entry.name]; ok {
			continue
		}
		if _, ok := semanticToolsSearchPlanCapabilities[entry.name]; ok {
			continue
		}
		t.Errorf("inventory name %q is neither petitionable nor plan-backed", entry.name)
	}
}

// Pin B: the rule-derived resolution must stay closed and sound. Every
// petitionable capability resolves to a label, that label exists in the rule
// set, and the label's templates actually back the petitioned capability —
// otherwise an expansion could add needs the petition never named.
func TestSemanticPetitionableCapabilitiesResolveRuleBackedLabels(t *testing.T) {
	for name, capability := range semanticPetitionableCapabilities {
		label, ok := semanticPetitionLabelForCapability(capability)
		if !ok {
			t.Errorf("petitionable name %q (%s) resolves no rule label", name, capability)
			continue
		}
		templates, exists := imSemanticIntentRuleSet[label]
		if !exists || len(templates) == 0 {
			t.Errorf("petitionable name %q resolved label %q with no rule template", name, label)
			continue
		}
		backed := false
		for _, template := range templates {
			if template.Capability == capability {
				backed = true
				break
			}
		}
		if !backed {
			t.Errorf("petitionable name %q (%s) resolved label %q whose templates do not back it", name, capability, label)
		}
	}
}

// The read-only/effectful split drives the group-policy gate and the two
// budgets, so it is pinned explicitly: only the verified read-only lookup and
// inspection legs are read-only; every other petitionable name is effectful
// and denied outright in group-restricted contexts.
func TestSemanticPetitionReadOnlyClassification(t *testing.T) {
	readOnly := []string{"web_search", "web_fetch", "current_datetime", "read_file", "git_status", "knowledge_search", "session_search", "asr"}
	for _, name := range readOnly {
		if _, ok := semanticPetitionableCapabilities[name]; !ok {
			t.Errorf("read-only pin %q is not petitionable", name)
			continue
		}
		if semanticPetitionIsEffectful(name) {
			t.Errorf("%q must stay read-only", name)
		}
	}
	for name := range semanticPetitionableCapabilities {
		expected := true
		for _, ro := range readOnly {
			if name == ro {
				expected = false
				break
			}
		}
		if semanticPetitionIsEffectful(name) != expected {
			t.Errorf("%q effectful=%v, want %v", name, !expected, expected)
		}
	}
	if semanticPetitionIsEffectful("nonexistent_tool") {
		t.Fatal("unknown names must not classify as effectful")
	}
}

// The generalization admits every plannable capability, not only the legacy
// five: a search turn may petition the office leg once (the agent judges that
// the turn needs a deck; the harness only checks the deterministic gates).
// The child renders office under its stable name, the effectful budget is
// spent, and the independent read-only budget still admits current_datetime.
func TestSemanticToolCallPetitionGrantsOfficeLegOnSearchTurn(t *testing.T) {
	cb := petitionTestOfficeCallbacks(t, &intent.ClassificationResult{Primary: intent.LabelSearch, Confidence: .98})
	parent := cb.semanticSurface
	if semanticGrantNameForAdapter(parent, semanticTrustedOfficeWriteAdapter) != "" {
		t.Fatal("fixture must start without an office grant")
	}
	granted, message := cb.PetitionToolCall("office")
	if !granted || !strings.Contains(message, "office") {
		t.Fatalf("office petition on a search turn must be admitted: granted=%v message=%q", granted, message)
	}
	child := cb.semanticSurface
	if child == parent {
		t.Fatal("petition must publish a child revision")
	}
	if !planHasCapabilities(child.plan, "information.search.web", tool.CapabilityDocumentWriteOffice) {
		t.Fatalf("child plan=%#v", child.plan.Selections)
	}
	if name := semanticGrantNameForAdapter(child, semanticTrustedOfficeWriteAdapter); name != "office" {
		t.Fatalf("child office grant name=%q, want office", name)
	}
	// The parent authority survives the expansion.
	if name := semanticGrantNameForAdapter(child, semanticTrustedWebSearchAdapter); name != "web_search" {
		t.Fatalf("child search grant name=%q, want web_search", name)
	}
	if !cb.semanticEffectfulPetitionConsumed {
		t.Fatal("a granted effectful petition must consume the effectful budget")
	}
	if granted, _ := cb.PetitionToolCall("bash"); granted {
		t.Fatal("second effectful petition in one turn must be denied")
	}
	if granted, message := cb.PetitionToolCall("current_datetime"); !granted || !strings.Contains(message, "current_datetime") {
		t.Fatalf("read-only petition must remain available after an effectful one: granted=%v message=%q", granted, message)
	}
	if name := semanticGrantNameForAdapter(cb.semanticSurface, semanticTrustedClockAdapter); name != "current_datetime" {
		t.Fatalf("child clock grant name=%q, want current_datetime", name)
	}
}

// Rendered and retired names are never petitions: the core loop owns their
// denial text, and the gate must not spend a budget or re-plan for them.
func TestSemanticToolCallPetitionSkipsRenderedAndRetiredNames(t *testing.T) {
	cb := petitionTestOfficeCallbacks(t, &intent.ClassificationResult{Primary: intent.LabelOffice, Confidence: .98})
	if granted, message := cb.PetitionToolCall("office"); granted || message != "" {
		t.Fatalf("rendered name must not be treated as a petition: granted=%v message=%q", granted, message)
	}
	surface := cb.semanticSurface
	grant, ok := surface.grants["office"]
	if !ok {
		t.Fatal("fixture must grant office")
	}
	delete(surface.grants, "office")
	surface.retiredGrants["office"] = grant
	if granted, message := cb.PetitionToolCall("office"); granted || message != "" {
		t.Fatalf("retired name must not be treated as a petition: granted=%v message=%q", granted, message)
	}
	if cb.semanticPetitionConsumed || cb.semanticEffectfulPetitionConsumed {
		t.Fatal("rendered/retired names must not consume either petition budget")
	}
}


// The 2026-08-28 张惠妹 turn, end to end at the planning boundary: the L3 tree
// answered a lookup task with its natural web_fetch verdict and the synthesis
// attached document_generate from L2 runner-up evidence — confidence 0.68,
// below both the resolver floor (0.78) and the tree hint floor (0.70). Two
// gates used to kill this shape: the declared-composite predicate did not
// accept web_fetch as the lookup half (though semanticLookupHalf does), and
// the below-floor planning gate had no declared-composite path. The turn fell
// to a chat-projected leftover without the PDF leg, and the generate_pdf
// petition could not expand because the label was already declared.
func TestSemanticLookupGenerateCompositePlansBelowTreeFloor(t *testing.T) {
	classification := &intent.ClassificationResult{
		Primary:    intent.LabelWebFetch,
		Secondary:  []intent.IntentLabel{intent.LabelDocumentGenerate},
		Confidence: 0.68,
		Layer:      3,
	}
	if !semanticDeclaredLookupGenerateComposite(*classification) {
		t.Fatal("web_fetch + document_generate must be a declared lookup+generate composite")
	}
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, classification.Primary)}
	h.semanticTrustedWebSearch = func(userID, query string) (string, error) { return "found: " + query, nil }
	registerBuiltinTools(h.registry, h)
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "全网搜索张惠妹歌曲列表，生成详细pdf版本清单", "desktop", "root-composite-floor", "turn-composite-floor", classification,
	)
	if err != nil || !handled || surface == nil {
		t.Fatalf("declared composite below the tree floor must still plan: handled=%v err=%v", handled, err)
	}
	if !planHasCapabilities(surface.plan, "document.generate.file", "artifact.deliver.current_channel", tool.CapabilityInformationFetchWeb) {
		t.Fatalf("plan=%#v", surface.plan.Selections)
	}
	assertGenerateRequiresOnlyLookupFamilyBases(t, surface.plan)
	if len(defs) == 0 {
		t.Fatal("surface must render the lookup legs")
	}
}

// A degraded composite stays a miss: a timeout guess must not mint writes
// through the declared-pair path.
func TestSemanticLookupGenerateCompositeDegradedStaysUnplanned(t *testing.T) {
	degraded := intent.ClassificationResult{
		Primary:    intent.LabelWebFetch,
		Secondary:  []intent.IntentLabel{intent.LabelDocumentGenerate},
		Confidence: 0.68,
		Layer:      3,
		Degraded:   true,
	}
	if semanticClassificationPlansBelowResolverFloor(degraded) {
		t.Fatal("degraded composite must not plan below the resolver floor")
	}
}

// Production shape, 2026-08-28 张惠妹 turn: the classifier returned
// primary=search conf=0.69 layer=2, not degraded, tree unavailable. The old
// gate planned nothing, the turn fell back to the legacy name router (which
// carries no managed tools), and the petition rescue could not fire without a
// semantic surface. A non-degraded all-read-only-governed classification now
// plans through the lookup projection; effectful capabilities still arrive
// only through the petition path.
func TestSemanticSubFloorReadOnlySearchPlansGovernedSurface(t *testing.T) {
	classification := &intent.ClassificationResult{
		Primary:    intent.LabelSearch,
		Confidence: 0.69,
		Layer:      2,
		Reason:     "embedding ambiguous; tree classification unavailable",
	}
	if semanticClassificationMeetsResolverFloor(*classification) {
		t.Fatal("fixture must sit below the resolver floor")
	}
	if !semanticClassificationPlansBelowResolverFloor(*classification) {
		t.Fatal("non-degraded governed read-only classification must plan below the floor")
	}
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, classification.Primary)}
	h.semanticTrustedWebSearch = func(userID, query string) (string, error) { return "found: " + query, nil }
	registerBuiltinTools(h.registry, h)
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "张惠妹歌曲列表", "desktop", "root-subfloor", "turn-subfloor", classification,
	)
	if err != nil || !handled || surface == nil {
		t.Fatalf("sub-floor governed read-only classification must plan: handled=%v err=%v", handled, err)
	}
	if !planHasCapabilities(surface.plan, "information.search.web") {
		t.Fatalf("plan=%#v", surface.plan.Selections)
	}
	if len(defs) == 0 {
		t.Fatal("surface must render the lookup legs")
	}
	// The turn keeps its petition rescue: a semantic surface with replan state
	// is exactly what PetitionToolCall needs to expand generate_pdf on demand.
	if surface.replan == nil {
		t.Fatal("sub-floor surface must keep petition/replan state")
	}
	// The light-profile gate (im_execution_profile.go gate 7) is a separate
	// budget layer and must keep firing for this shape; the planner above
	// still owns the tool surface, so a light turn keeps the governed face
	// instead of falling back to the legacy name router.
	profile := executionProfileFromSemanticIntent(classification, nil)
	if profile.PromptProfile != "light" {
		t.Fatalf("sub-floor lookup hint must keep the light profile, got %+v", profile)
	}
}

// The floor still holds for everything else: a weak effectful label
// (document_generate at 0.69, no declared lookup half) must not mint tool
// legs below 0.78, and a degraded read-only hint stays a miss — a timeout
// guess must not mint grants either.
func TestSemanticSubFloorWeakEffectfulAndDegradedStayMiss(t *testing.T) {
	weakGenerate := intent.ClassificationResult{
		Primary:    intent.LabelDocumentGenerate,
		Confidence: 0.69,
		Layer:      2,
	}
	if semanticClassificationPlansBelowResolverFloor(weakGenerate) {
		t.Fatal("weak L2 generate must stay a miss below the resolver floor")
	}
	degradedSearch := intent.ClassificationResult{
		Primary:    intent.LabelSearch,
		Confidence: 0.69,
		Layer:      2,
		Degraded:   true,
	}
	if semanticClassificationPlansBelowResolverFloor(degradedSearch) {
		t.Fatal("degraded read-only hint below the lookup floor must stay a miss")
	}
}
