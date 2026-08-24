package main

import (
	"context"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

func TestSessionGovernedTaskPersistsGrantedNeedsNotUICLabels(t *testing.T) {
	h := registerDocumentGeneratePDF(t)
	if err := h.registry.Register(RegisteredTool{
		Name: "web_search", Status: RegToolAvailable, InputSchema: map[string]interface{}{},
		CapabilityProvisions: []tool.CapabilityProvision{{Capability: "information.search.web", Qualifiers: map[string]string{"freshness": "current"}, Quality: 1}},
		SemanticEffects:      []tool.EffectClass{tool.EffectReadOnly},
		Handler:              func(map[string]interface{}) string { return "ok" },
	}); err != nil {
		t.Fatal(err)
	}
	prepared, handled, err := h.semanticPlanForTurnWithClassification("user", "查询南京天气，并生成pdf报告", "desktop", "root-a", "turn-a", liveDataGenerateClassification())
	if err != nil || !handled || prepared == nil {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
	h.persistSessionGovernedTask("user", "desktop", "", false, prepared.plan)
	task, ok := h.loadSessionGovernedTask("user", "desktop", "")
	if !ok || task.Status != sessionGovernedPending {
		t.Fatalf("task=%#v ok=%v", task, ok)
	}
	foundGenerate, foundDeliver, foundSearch := false, false, false
	for _, need := range task.Needs {
		switch need.Capability {
		case "document.generate.file":
			foundGenerate = true
		case "artifact.deliver.current_channel":
			foundDeliver = need.Qualifiers["format"] == "file"
		case "information.search.web":
			foundSearch = true
		}
	}
	if !foundGenerate || !foundDeliver || !foundSearch {
		t.Fatalf("granted needs=%#v", task.Needs)
	}
}

func TestSessionGovernedTaskAppHostNeverUsesLegacyReplayMap(t *testing.T) {
	h := &IMMessageHandler{app: &App{}, registry: NewToolRegistry()}
	plan := tool.ToolPlan{Selections: []tool.PlannedSelection{{
		ID: "selection", NeedID: "need", FitProof: tool.FitProof{MatchedCapability: "document.generate.file"},
	}}}
	h.persistSessionGovernedTask("user", "desktop", "", false, plan)
	if _, ok := h.loadSessionGovernedTask("user", "desktop", ""); ok {
		t.Fatal("App host must not retain a legacy session-governed replay record")
	}
	continuation := intent.ClassificationResult{Primary: intent.LabelContinuation, Confidence: 0.9}
	if replayed, ok := h.applySessionGovernedContinuation("user", "desktop", "", false, continuation, "继续", nil); ok || replayed.Primary != continuation.Primary || replayed.Confidence != continuation.Confidence {
		t.Fatalf("App host replayed legacy task: replayed=%#v ok=%v", replayed, ok)
	}
}

func TestSessionGovernedTaskStandaloneHostNeverUsesLegacyReplayMap(t *testing.T) {
	h := NewIMMessageHandlerStandalone(StandaloneConfig{})
	defer h.memory.Stop()
	plan := tool.ToolPlan{Selections: []tool.PlannedSelection{{
		ID: "selection", NeedID: "need", FitProof: tool.FitProof{MatchedCapability: "document.generate.file"},
	}}}
	h.persistSessionGovernedTask("user", "tui", "", false, plan)
	if _, ok := h.loadSessionGovernedTask("user", "tui", ""); ok {
		t.Fatal("standalone host must not retain a legacy session-governed replay record")
	}
	continuation := intent.ClassificationResult{Primary: intent.LabelContinuation, Confidence: 0.9}
	if replayed, ok := h.applySessionGovernedContinuation("user", "tui", "", false, continuation, "继续", nil); ok || replayed.Primary != continuation.Primary || replayed.Confidence != continuation.Confidence {
		t.Fatalf("standalone host replayed legacy task: replayed=%#v ok=%v", replayed, ok)
	}
}

func TestSessionGovernedTaskWorkflowTurnDoesNotPersistGenerate(t *testing.T) {
	h := registerDocumentGeneratePDF(t)
	prepared, handled, err := h.semanticPlanForTurnWithClassification("user", "生成pdf报告", "desktop", "root", "turn", documentGenerateClassification())
	if err != nil || !handled || prepared == nil {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
	h.persistSessionGovernedTask("user", "desktop", "", true, prepared.plan)
	if _, ok := h.loadSessionGovernedTask("user", "desktop", ""); ok {
		t.Fatal("workflow turn must not write SessionGovernedTask")
	}
	replayed, ok := h.applySessionGovernedContinuation("user", "desktop", "", false, intent.ClassificationResult{Primary: intent.LabelContinuation, Confidence: 0.9}, "", nil)
	if ok || replayed.Primary != intent.LabelContinuation {
		t.Fatalf("continue after workflow must not invent generate: replayed=%#v ok=%v", replayed, ok)
	}
}

func TestSessionGovernedContinuationNeverReplaysOverClassifierProtocolFailure(t *testing.T) {
	h := registerDocumentGeneratePDF(t)
	prepared, handled, err := h.semanticPlanForTurnWithClassification("user", "生成pdf报告", "desktop", "root", "turn", documentGenerateClassification())
	if err != nil || !handled || prepared == nil {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
	h.persistSessionGovernedTask("user", "desktop", "", false, prepared.plan)

	failed := intent.ClassificationResult{
		Primary: intent.LabelUnknown, Confidence: 0.30, Layer: 3, Degraded: true,
		ControlPlaneFailure: true,
	}
	replayed, ok := h.applySessionGovernedContinuation("user", "desktop", "", false, failed, "继续", nil)
	if ok || !replayed.ControlPlaneFailure || replayed.Primary != intent.LabelUnknown {
		t.Fatalf("protocol failure must remain host-owned, replayed=%#v ok=%v", replayed, ok)
	}
}

func TestSessionGovernedContinuationDoesNotInventGenerateFromLookup(t *testing.T) {
	h := registerDocumentGeneratePDF(t)
	if err := h.registry.Register(RegisteredTool{
		Name: "web_search", Status: RegToolAvailable, InputSchema: map[string]interface{}{},
		CapabilityProvisions: []tool.CapabilityProvision{{Capability: "information.search.web", Qualifiers: map[string]string{"freshness": "current"}, Quality: 1}},
		SemanticEffects:      []tool.EffectClass{tool.EffectReadOnly},
		Handler:              func(map[string]interface{}) string { return "ok" },
	}); err != nil {
		t.Fatal(err)
	}
	prepared, handled, err := h.semanticPlanForTurnWithClassification("user", "南京天气", "desktop", "root-lookup", "turn", &intent.ClassificationResult{Primary: intent.LabelLiveData, Confidence: 0.98})
	if err != nil || !handled || prepared == nil {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
	h.persistSessionGovernedTask("user", "desktop", "", false, prepared.plan)
	h.markSessionGovernedTaskStatus("user", "desktop", "", sessionGovernedSucceeded)
	replayed, ok := h.applySessionGovernedContinuation("user", "desktop", "", false, intent.ClassificationResult{Primary: intent.LabelContinuation, Confidence: 0.9}, "", nil)
	if ok {
		t.Fatalf("succeeded lookup must not replay: %#v", replayed)
	}
	_, handled, err = h.semanticPlanForTurnWithClassification("user", "继续", "desktop", "root-continue", "turn-continue", &intent.ClassificationResult{Primary: intent.LabelContinuation, Confidence: 0.9})
	if handled || err != nil {
		t.Fatalf("continue after lookup must stay unmanaged, handled=%v err=%v", handled, err)
	}
}

func TestSessionGovernedContinuationDoesNotReplayOntoLookupHint(t *testing.T) {
	h := registerDocumentGeneratePDF(t)
	if err := h.registry.Register(RegisteredTool{
		Name: "web_search", Status: RegToolAvailable, InputSchema: map[string]interface{}{},
		CapabilityProvisions: []tool.CapabilityProvision{{Capability: "information.search.web", Qualifiers: map[string]string{"freshness": "current"}, Quality: 1}},
		SemanticEffects:      []tool.EffectClass{tool.EffectReadOnly},
		Handler:              func(map[string]interface{}) string { return "ok" },
	}); err != nil {
		t.Fatal(err)
	}
	prepared, handled, err := h.semanticPlanForTurnWithClassification("user", "查询南京天气，并生成pdf报告", "desktop", "root-pdf", "turn-pdf", liveDataGenerateClassification())
	if err != nil || !handled || prepared == nil {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
	h.persistSessionGovernedTask("user", "desktop", "", false, prepared.plan)
	hint := intent.ClassificationResult{Primary: intent.LabelLiveData, Confidence: 0.61, Layer: 2, Degraded: true}
	replayed, ok := h.applySessionGovernedContinuation("user", "desktop", "", false, hint, "北京天所", nil)
	if ok || replayed.Primary != intent.LabelLiveData {
		t.Fatalf("lookup hint must not be generic continuation: replayed=%#v ok=%v", replayed, ok)
	}
}

func TestSessionGovernedContinuationReplaysGrantedGenerateOnNewRootTask(t *testing.T) {
	h := registerDocumentGeneratePDF(t)
	prepared, handled, err := h.semanticPlanForTurnWithClassification("user", "生成pdf报告", "desktop", "root-1", "turn-1", documentGenerateClassification())
	if err != nil || !handled || prepared == nil {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
	h.persistSessionGovernedTask("user", "desktop", "", false, prepared.plan)
	replayed, ok := h.applySessionGovernedContinuation("user", "desktop", "", false, intent.ClassificationResult{Primary: intent.LabelContinuation, Confidence: 0.9}, "", nil)
	if !ok || !classificationHasLabel(replayed, intent.LabelDocumentGenerate) {
		t.Fatalf("pending generate must replay, replayed=%#v ok=%v", replayed, ok)
	}
	continued, handled, err := h.semanticPlanForTurnWithClassification("user", "继续", "desktop", "root-2", "turn-2", &intent.ClassificationResult{Primary: intent.LabelContinuation, Confidence: 0.9})
	if err != nil || !handled || continued == nil {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
	if continued.rootTaskID != "root-2" {
		t.Fatalf("continuation must allocate a new RootTaskID, got %q", continued.rootTaskID)
	}
	if !planHasCapabilities(continued.plan, "document.generate.file", "artifact.deliver.current_channel") {
		t.Fatalf("granted generate+deliver must continue, selections=%#v", continued.plan.Selections)
	}
}

func TestSessionGovernedContinuationDoesNotReplayGenerateOverStagedImage(t *testing.T) {
	h := registerDocumentGeneratePDF(t)
	prepared, handled, err := h.semanticPlanForTurnWithClassification("user-1", "生成pdf报告", "desktop", "root-1", "turn-1", documentGenerateClassification())
	if err != nil || !handled || prepared == nil {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
	h.persistSessionGovernedTask("user-1", "desktop", "", false, prepared.plan)
	text := "图中有什么？\n\n" + filePathPromptPrefix + "\nC:\\tmp\\scan.jpg"
	if replayed, ok := h.applySessionGovernedContinuation("user-1", "desktop", "", false, intent.ClassificationResult{Primary: intent.LabelNonCoding, Confidence: 0.9, Layer: 3}, text, nil); ok {
		t.Fatalf("stripped image-describe must not replay: %#v", replayed)
	}
	if replayed, ok := h.applySessionGovernedContinuation("user-1", "desktop", "", false, intent.ClassificationResult{Primary: intent.LabelContinuation, Confidence: 0.9, Layer: 3}, text, nil); ok || classificationHasLabel(replayed, intent.LabelDocumentGenerate) {
		t.Fatalf("entry-path continuation must not replay generate over a staged image: replayed=%#v ok=%v", replayed, ok)
	}
	_, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", text, "desktop", "root-image", "turn-image",
		&intent.ClassificationResult{Primary: intent.LabelContinuation, Confidence: 0.9, Layer: 3},
	)
	if err != nil || handled || surface != nil {
		t.Fatalf("new staged image must not replay pending generate: surface=%#v handled=%v err=%v", surface, handled, err)
	}
	task, ok := h.loadSessionGovernedTask("user-1", "desktop", "")
	if !ok || task.Status != sessionGovernedSuperseded || task.replayable() {
		t.Fatalf("staged image must retire the pending generate: %#v ok=%v", task, ok)
	}
	if replayed, ok := h.applySessionGovernedContinuation("user-1", "desktop", "", false, intent.ClassificationResult{Primary: intent.LabelContinuation, Confidence: 0.9, Layer: 3}, "继续", nil); ok || classificationHasLabel(replayed, intent.LabelDocumentGenerate) {
		t.Fatalf("oral continue after an image turn must not resurrect generate: replayed=%#v ok=%v", replayed, ok)
	}
	_, surface, handled, err = h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "继续", "desktop", "root-after-image", "turn-after-image",
		&intent.ClassificationResult{Primary: intent.LabelContinuation, Confidence: 0.9, Layer: 3},
	)
	if err != nil || handled || surface != nil {
		t.Fatalf("continue after image-describe must stay unmanaged: surface=%#v handled=%v err=%v", surface, handled, err)
	}
}

func TestSessionGovernedSucceededGenerateDoesNotReplay(t *testing.T) {
	h := registerDocumentGeneratePDF(t)
	prepared, handled, err := h.semanticPlanForTurnWithClassification("user", "生成pdf报告", "desktop", "root", "turn", documentGenerateClassification())
	if err != nil || !handled || prepared == nil {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
	h.persistSessionGovernedTask("user", "desktop", "", false, prepared.plan)
	h.markSessionGovernedTaskStatus("user", "desktop", "", sessionGovernedSucceeded)
	if _, ok := h.applySessionGovernedContinuation("user", "desktop", "", false, intent.ClassificationResult{Primary: intent.LabelContinuation, Confidence: 0.9}, "", nil); ok {
		t.Fatal("succeeded generate must not replay as another PDF")
	}
}

func TestSessionGovernedFailedExecCanReplaySameNeeds(t *testing.T) {
	h := registerDocumentGeneratePDF(t)
	prepared, _, err := h.semanticPlanForTurnWithClassification("user", "生成pdf报告", "desktop", "root-fail", "turn-fail", documentGenerateClassification())
	if err != nil || prepared == nil {
		t.Fatal(err)
	}
	h.persistSessionGovernedTask("user", "desktop", "", false, prepared.plan)
	h.markSessionGovernedTaskStatus("user", "desktop", "", sessionGovernedFailedExec)
	continued, handled, err := h.semanticPlanForTurnWithClassification("user", "切换到完整agent模式", "desktop", "root-retry", "turn-retry", &intent.ClassificationResult{Primary: intent.LabelContinuation, Confidence: 0.9})
	if err != nil || !handled || continued == nil || !planHasCapabilities(continued.plan, "document.generate.file", "artifact.deliver.current_channel") {
		t.Fatalf("failed generate must remain replayable, handled=%v err=%v plan=%#v", handled, err, continued)
	}
}

func TestSessionGovernedTaskClearedOnSessionReset(t *testing.T) {
	h := registerDocumentGeneratePDF(t)
	prepared, _, err := h.semanticPlanForTurnWithClassification("user", "生成pdf报告", "desktop", "root", "turn", documentGenerateClassification())
	if err != nil || prepared == nil {
		t.Fatal(err)
	}
	h.persistSessionGovernedTask("user", "desktop", "", false, prepared.plan)
	h.clearPerUserSessionState("user")
	if _, ok := h.loadSessionGovernedTask("user", "desktop", ""); ok {
		t.Fatal("/new must discard SessionGovernedTask")
	}
}

func TestSemanticChannelConstraintsDenyVEGenerate(t *testing.T) {
	constraints := semanticChannelCapabilityConstraints("ve_group_executor")
	if !routingConstraintDenies(constraints, "document.generate.file", nil) {
		t.Fatalf("VE must deny generate at capability level: %#v", constraints)
	}
	if !routingConstraintDenies(constraints, "artifact.deliver.current_channel", map[string]string{"format": "file"}) {
		t.Fatalf("VE must deny unpublished file deliver: %#v", constraints)
	}
	if routingConstraintDenies(constraints, "artifact.deliver.current_channel", map[string]string{"format": "image"}) {
		t.Fatalf("VE file-deliver deny must not also deny image: %#v", constraints)
	}
	desktop := semanticChannelCapabilityConstraints("desktop")
	if routingConstraintDenies(desktop, "document.generate.file", nil) {
		t.Fatalf("desktop must not deny generate: %#v", desktop)
	}
	if routingConstraintDenies(desktop, "artifact.deliver.current_channel", map[string]string{"format": "file"}) {
		t.Fatalf("desktop must not deny published file deliver: %#v", desktop)
	}
	if !routingConstraintDenies(desktop, "artifact.deliver.current_channel", map[string]string{"format": "voice"}) {
		t.Fatalf("desktop must deny unpublished voice deliver: %#v", desktop)
	}
	if !routingConstraintDenies(desktop, tool.CapabilityAudioRenderSpeech, nil) {
		t.Fatalf("desktop must deny unpublished speech render: %#v", desktop)
	}
	weixin := semanticChannelCapabilityConstraints("weixin")
	if routingConstraintDenies(weixin, "document.generate.file", nil) {
		t.Fatalf("weixin must not deny generate: %#v", weixin)
	}
	if routingConstraintDenies(weixin, "artifact.deliver.current_channel", map[string]string{"format": "file"}) {
		t.Fatalf("weixin must not deny published file deliver: %#v", weixin)
	}
	if !routingConstraintDenies(weixin, "artifact.deliver.current_channel", map[string]string{"format": "voice"}) {
		t.Fatalf("weixin must deny unpublished voice deliver: %#v", weixin)
	}
	workflow := semanticHostRoutingConstraints("desktop", true)
	if !routingConstraintDenies(workflow, "document.generate.file", nil) {
		t.Fatalf("workflow turn must deny generate: %#v", workflow)
	}
}

func routingConstraintDenies(constraints []tool.RoutingConstraint, capability tool.CapabilityID, qualifiers map[string]string) bool {
	for _, constraint := range constraints {
		if constraint.Capability != capability || constraint.Effect != "deny" {
			continue
		}
		if len(constraint.Attributes) == 0 {
			return true
		}
		match := true
		for key, value := range constraint.Attributes {
			if qualifiers[key] != value {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

func TestSemanticHostRoutingFactsRecordChannelAndWorkflow(t *testing.T) {
	desktop := semanticHostRoutingFacts("desktop", false)
	if factAttr(desktop, "file_delivery_published", "published") != "true" || factAttr(desktop, "image_delivery_published", "published") != "true" {
		t.Fatalf("desktop facts=%#v", desktop)
	}
	if factAttr(desktop, "workflow_agent_loop", "active") != "" {
		t.Fatalf("non-workflow must omit workflow fact: %#v", desktop)
	}
	weixin := semanticHostRoutingFacts("weixin", false)
	if factAttr(weixin, "file_delivery_published", "published") != "true" || factAttr(weixin, "image_delivery_published", "published") != "true" {
		t.Fatalf("weixin facts=%#v", weixin)
	}
	workflow := semanticHostRoutingFacts("desktop", true)
	if factAttr(workflow, "workflow_agent_loop", "active") != "true" {
		t.Fatalf("workflow facts=%#v", workflow)
	}
}

func factAttr(facts []tool.RoutingFact, kind, key string) string {
	for _, fact := range facts {
		if fact.Kind == kind {
			return fact.Attributes[key]
		}
	}
	return ""
}

func TestSessionGovernedTaskIsolatesGroupDestinations(t *testing.T) {
	h := registerDocumentGeneratePDF(t)
	prepared, handled, err := h.semanticPlanForTurnWithClassification("user", "生成pdf报告", "lansenger", "root-g1", "turn-g1", documentGenerateClassification())
	if err != nil || !handled || prepared == nil {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
	h.persistSessionGovernedTask("user", "lansenger", "group:g1", false, prepared.plan)
	if _, ok := h.loadSessionGovernedTask("user", "lansenger", "group:g2"); ok {
		t.Fatal("same user in another group must not share granted needs")
	}
	if _, ok := h.loadSessionGovernedTask("user", "lansenger", ""); ok {
		t.Fatal("DM must not share a group SessionGovernedTask")
	}
	continuation := intent.ClassificationResult{Primary: intent.LabelContinuation, Confidence: 0.9}
	if replayed, ok := h.applySessionGovernedContinuation("user", "lansenger", "group:g2", false, continuation, "", nil); ok {
		t.Fatalf("group g2 must not replay g1 generate: %#v", replayed)
	}
	if replayed, ok := h.applySessionGovernedContinuation("user", "lansenger", "", false, continuation, "", nil); ok {
		t.Fatalf("DM must not replay group generate: %#v", replayed)
	}
	replayed, ok := h.applySessionGovernedContinuation("user", "lansenger", "group:g1", false, continuation, "", nil)
	if !ok || !classificationHasLabel(replayed, intent.LabelDocumentGenerate) {
		t.Fatalf("same group must replay granted generate: %#v ok=%v", replayed, ok)
	}
	h.markSessionGovernedTaskStatus("user", "lansenger", "group:g2", sessionGovernedSucceeded)
	task, ok := h.loadSessionGovernedTask("user", "lansenger", "group:g1")
	if !ok || task.Status != sessionGovernedPending {
		t.Fatalf("marking another destination must not settle this group: %#v ok=%v", task, ok)
	}
	ctxG2 := withSemanticDestination(context.Background(), "group:g2")
	_, handled, err = h.semanticPlanForTurnWithContextAndClassificationAndAttachments(ctxG2, "user", "继续", "lansenger", "root-g2", "turn-g2", &continuation, nil)
	if handled || err != nil {
		t.Fatalf("continue in another group must stay unmanaged, handled=%v err=%v", handled, err)
	}
	ctxG1 := withSemanticDestination(context.Background(), "group:g1")
	continued, handled, err := h.semanticPlanForTurnWithContextAndClassificationAndAttachments(ctxG1, "user", "继续", "lansenger", "root-g1b", "turn-g1b", &continuation, nil)
	if err != nil || !handled || continued == nil || !planHasCapabilities(continued.plan, "document.generate.file", "artifact.deliver.current_channel") {
		t.Fatalf("continue in the same group must rematerialize generate, handled=%v err=%v plan=%#v", handled, err, continued)
	}
}

func TestManagedSemanticSurfaceOmitsSkillAndMCPGateways(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry()}
	registerBuiltinTools(h.registry, h)
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification("user", "生成pdf报告", "desktop", "root-surface", "turn-surface", documentGenerateClassification())
	if err != nil || !handled || surface == nil {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
	for _, def := range defs {
		name := extractToolName(def)
		if name == "manage_skill" || name == "call_mcp_tool" {
			t.Fatalf("managed surface leaked %s: %#v", name, defs)
		}
	}
}
