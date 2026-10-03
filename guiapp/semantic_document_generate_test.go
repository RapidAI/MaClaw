package guiapp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

func TestPlannedSuccessorDenialRequiresAnOutstandingPredecessor(t *testing.T) {
	pdf := tool.CapabilityID("document.generate.file")
	search := tool.CapabilityID("information.search.web")
	bash := tool.CapabilityShellExecuteLocal
	waiting := &sharedAgentLoopCallbacks{semanticSurface: &semanticCallSurface{
		completed: map[string]bool{},
		grants:    map[string]tool.InvocationGrant{"web_search": {SelectionID: "search", Token: "live"}},
		plan: tool.ToolPlan{Selections: []tool.PlannedSelection{
			{ID: "search", FitProof: tool.FitProof{MatchedCapability: search}},
			{ID: "pdf", FitProof: tool.FitProof{MatchedCapability: pdf}, Requires: []string{"search"}},
			{ID: "bash", FitProof: tool.FitProof{MatchedCapability: bash}},
		}},
	}}
	if msg := waiting.plannedSuccessorDenial("generate_pdf", pdf); !successorNotYetListedDenial(msg, "generate_pdf") {
		t.Fatalf("lookup-gated pdf must explain the next request: %q", msg)
	}
	if msg := waiting.plannedSuccessorDenial("bash", bash); msg != "" {
		t.Fatalf("a ready baseline must keep the generic denial: %q", msg)
	}
	confirmed := &sharedAgentLoopCallbacks{semanticSurface: &semanticCallSurface{
		completed: map[string]bool{},
		plan: tool.ToolPlan{Selections: []tool.PlannedSelection{
			{ID: "pdf", FitProof: tool.FitProof{MatchedCapability: pdf}, Requires: []string{"confirmation:need"}},
		}},
	}}
	if msg := confirmed.plannedSuccessorDenial("generate_pdf", pdf); msg != "" {
		t.Fatalf("confirmation must not be described as a listed step: %q", msg)
	}
	held := &sharedAgentLoopCallbacks{
		semanticHoldDependantIssue: true,
		semanticSurface: &semanticCallSurface{
			completed: map[string]bool{"search": true},
			plan: tool.ToolPlan{Selections: []tool.PlannedSelection{
				{ID: "search", FitProof: tool.FitProof{MatchedCapability: search}},
				{ID: "pdf", FitProof: tool.FitProof{MatchedCapability: pdf}, Requires: []string{"search"}},
			}},
		},
	}
	if msg := held.plannedSuccessorDenial("generate_pdf", pdf); !successorHeldForNextRequest(msg, "generate_pdf") {
		t.Fatalf("held ready generate must wait for the next request without another lookup: %q", msg)
	}
	released := &sharedAgentLoopCallbacks{semanticSurface: held.semanticSurface}
	if msg := released.plannedSuccessorDenial("generate_pdf", pdf); msg != "" {
		t.Fatalf("a ready generate outside the hold must not promise another request: %q", msg)
	}
	deliver := tool.CapabilityID("artifact.deliver.current_channel")
	chain := &sharedAgentLoopCallbacks{semanticSurface: &semanticCallSurface{
		completed: map[string]bool{},
		plan: tool.ToolPlan{Selections: []tool.PlannedSelection{
			{ID: "search", FitProof: tool.FitProof{MatchedCapability: search}},
			{ID: "pdf", FitProof: tool.FitProof{MatchedCapability: pdf}, Requires: []string{"search"}},
			{ID: "deliver", FitProof: tool.FitProof{MatchedCapability: deliver}, Requires: []string{"pdf"}},
		}},
	}}
	if msg := chain.plannedSuccessorDenial("send_file", deliver); !successorListedAfterEarlierSteps(msg, "send_file") {
		t.Fatalf("delivery behind an unready generate must not promise the next request: %q", msg)
	}
	named := &sharedAgentLoopCallbacks{semanticSurface: &semanticCallSurface{
		completed: map[string]bool{},
		grants: map[string]tool.InvocationGrant{
			"web_search": {SelectionID: "search", Token: "live"},
		},
		plan: tool.ToolPlan{Selections: []tool.PlannedSelection{
			{ID: "search", FitProof: tool.FitProof{MatchedCapability: search}},
			{ID: "pdf", FitProof: tool.FitProof{MatchedCapability: pdf}, Requires: []string{"search"}},
			{ID: "deliver", FitProof: tool.FitProof{MatchedCapability: deliver}, Requires: []string{"pdf"}},
		}},
	}}
	if msg := named.plannedSuccessorDenial("generate_pdf", pdf); !strings.Contains(msg, `"web_search"`) || strings.Contains(msg, "a tool that is listed now") {
		t.Fatalf("pdf denial must name the listed lookup: %q", msg)
	}
	if msg := named.plannedSuccessorDenial("send_file", deliver); !strings.Contains(msg, `Call "web_search" now`) || !strings.Contains(msg, `"generate_pdf" appears on the next request`) || !strings.Contains(msg, `Call "send_file" only after "send_file" is listed`) || strings.Contains(msg, `"send_file" appears`) {
		t.Fatalf("delivery denial must name the lookup and the generate step, not promise send_file next: %q", msg)
	}
	afterSearch := &sharedAgentLoopCallbacks{
		semanticHoldDependantIssue: true,
		semanticSurface: &semanticCallSurface{
			completed: map[string]bool{"search": true},
			grants:    map[string]tool.InvocationGrant{"web_search": {SelectionID: "search", Token: "spent-looking-live"}},
			plan: tool.ToolPlan{Selections: []tool.PlannedSelection{
				{ID: "search", FitProof: tool.FitProof{MatchedCapability: search}},
				{ID: "pdf", FitProof: tool.FitProof{MatchedCapability: pdf}, Requires: []string{"search"}},
				{ID: "deliver", FitProof: tool.FitProof{MatchedCapability: deliver}, Requires: []string{"pdf"}},
			}},
		},
	}
	if msg := afterSearch.plannedSuccessorDenial("send_file", deliver); !strings.Contains(msg, `"generate_pdf" appears on the next request`) || strings.Contains(msg, `"send_file" appears`) || strings.Contains(msg, "Call a tool that is listed now") {
		t.Fatalf("delivery behind a held generate must not promise send_file on the next request: %q", msg)
	}
}

// A coordinator publish leaves rendered empty even though the definitions
// were handed to the model. Adopting that list is what makes the predecessor
// callable; until then the denial must not invent a listed step.
func TestAdoptedDefinitionsNameTheListedPredecessor(t *testing.T) {
	pdf := tool.CapabilityID("document.generate.file")
	search := tool.CapabilityID("information.search.web")
	cb := &sharedAgentLoopCallbacks{semanticSurface: &semanticCallSurface{
		rendered:  map[string]bool{},
		completed: map[string]bool{},
		grants:    map[string]tool.InvocationGrant{"web_search": {SelectionID: "search", Token: "live"}},
		plan: tool.ToolPlan{Selections: []tool.PlannedSelection{
			{ID: "search", FitProof: tool.FitProof{MatchedCapability: search}},
			{ID: "pdf", FitProof: tool.FitProof{MatchedCapability: pdf}, Requires: []string{"search"}},
		}},
	}}
	if msg := cb.plannedSuccessorDenial("generate_pdf", pdf); strings.Contains(msg, `"web_search"`) {
		t.Fatalf("unrecorded grant must not be described as listed: %q", msg)
	}
	cb.setVisibleToolDefinitions([]map[string]interface{}{
		{"type": "function", "function": map[string]interface{}{"name": "web_search"}},
	})
	msg := cb.plannedSuccessorDenial("generate_pdf", pdf)
	if !successorNotYetListedDenial(msg, "generate_pdf") || !strings.Contains(msg, `"web_search"`) || strings.Contains(msg, "a tool that is listed now") {
		t.Fatalf("adopted lookup must be the named next step: %q", msg)
	}
	cb.setVisibleToolDefinitions([]map[string]interface{}{
		{"type": "function", "function": map[string]interface{}{"name": "tools_search"}},
	})
	if cb.semanticSurface.rendered["web_search"] || !cb.semanticSurface.rendered["tools_search"] {
		t.Fatalf("adopted names must replace the rendered set, got %#v", cb.semanticSurface.rendered)
	}
	if dropped := cb.plannedSuccessorDenial("generate_pdf", pdf); strings.Contains(dropped, `"web_search"`) {
		t.Fatalf("a name that left the adopted list must stop counting as listed: %q", dropped)
	}
	untracked := &sharedAgentLoopCallbacks{semanticSurface: &semanticCallSurface{
		grants: map[string]tool.InvocationGrant{"web_search": {SelectionID: "search", Token: "live"}},
		plan: tool.ToolPlan{Selections: []tool.PlannedSelection{
			{ID: "search", FitProof: tool.FitProof{MatchedCapability: search}},
			{ID: "pdf", FitProof: tool.FitProof{MatchedCapability: pdf}, Requires: []string{"search"}},
		}},
	}}
	untracked.setVisibleToolDefinitions([]map[string]interface{}{
		{"type": "function", "function": map[string]interface{}{"name": "web_search"}},
	})
	if untracked.semanticSurface.rendered != nil {
		t.Fatal("a nil rendered map means the caller is not tracking emission")
	}
}

func successorHeldForNextRequest(message, name string) bool {
	return strings.Contains(message, "next request") &&
		strings.Contains(message, "held until") &&
		strings.Contains(message, name) &&
		strings.Contains(message, "do not ask the user to re-authorize") &&
		!strings.Contains(message, "Call a tool that is listed now") &&
		!strings.Contains(message, "Do not retry")
}

func successorListedAfterEarlierSteps(message, name string) bool {
	return strings.Contains(message, "not listed yet") &&
		strings.Contains(message, "still unfinished") &&
		strings.Contains(message, name) &&
		strings.Contains(message, "do not ask the user to re-authorize") &&
		!strings.Contains(message, "next request") &&
		!strings.Contains(message, "Do not retry")
}

func successorNotYetListedDenial(message, name string) bool {
	return strings.Contains(message, "not listed yet") &&
		strings.Contains(message, name) &&
		strings.Contains(message, "next request") &&
		strings.Contains(message, "do not ask the user to re-authorize") &&
		!strings.Contains(message, "Do not retry") &&
		!strings.Contains(message, "was not available in this request's rendered tool surface")
}

func documentGenerateClassification() *intent.ClassificationResult {
	return &intent.ClassificationResult{Primary: intent.LabelDocumentGenerate, Confidence: .98}
}

func liveDataGenerateClassification() *intent.ClassificationResult {
	return &intent.ClassificationResult{
		Primary: intent.LabelLiveData, Secondary: []intent.IntentLabel{intent.LabelDocumentGenerate}, Confidence: .98,
	}
}

func searchGenerateClassification() *intent.ClassificationResult {
	return &intent.ClassificationResult{
		Primary: intent.LabelSearch, Secondary: []intent.IntentLabel{intent.LabelDocumentGenerate}, Confidence: .85, Layer: 3,
	}
}

func TestIMSemanticDocumentGenerateIsManagedAndNotDelivery(t *testing.T) {
	managed, unmapped := imSemanticIntentCoverage(*documentGenerateClassification())
	if !managed || unmapped != "" {
		t.Fatalf("managed=%v unmapped=%q", managed, unmapped)
	}
	if _, unmapped := imSemanticUnmappedCapabilityLabel(intent.ClassificationResult{Primary: intent.LabelDocumentDelivery, Confidence: .98}); unmapped {
		t.Fatal("document_delivery is specified-target deliver, not unmapped")
	}
	registry := newIMSemanticCapabilityRegistry()
	needs, resolved, err := semanticIntentNeedsFromClassification(registry, *documentGenerateClassification())
	if err != nil || !resolved || len(needs) == 0 {
		t.Fatalf("needs=%#v managed=%v err=%v", needs, resolved, err)
	}
	foundGenerate, foundDeliver, foundRead := false, false, false
	for _, need := range needs {
		switch need.Capability {
		case "document.generate.file":
			foundGenerate = need.Qualifiers["format"] == "pdf" && need.Required
		case "artifact.deliver.current_channel":
			foundDeliver = need.Qualifiers["format"] == "file" && need.Required
		case tool.CapabilityFSReadLocal:
			foundRead = !need.Required
		case "information.search.web", tool.CapabilityInformationFetchWeb:
			t.Fatalf("cold document turn materialized latent %s", need.Capability)
		}
	}
	if !foundGenerate || !foundDeliver || !foundRead {
		t.Fatalf("needs=%#v, want generate.file(pdf), deliver.file and an optional read leg", needs)
	}
}

func TestIMSemanticDocumentGenerateFileDeliverSkipsAttachmentLookup(t *testing.T) {
	registry := newIMSemanticCapabilityRegistry()
	needs, _, err := semanticIntentNeedsFromClassification(registry, *documentGenerateClassification())
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := semanticNeedsForTrustedDocumentInputs(needs, nil)
	if err != nil || len(resolved) == 0 {
		t.Fatalf("generate file deliver must not require an attachment: resolved=%#v err=%v", resolved, err)
	}
	foundGenerate := false
	for _, need := range resolved {
		if need.Capability == "document.generate.file" && need.Required {
			foundGenerate = true
		}
	}
	if !foundGenerate {
		t.Fatalf("generate need dropped: %#v", resolved)
	}
}

func TestIMSemanticAttachmentPlusGenerateFailClosed(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry()}
	_, handled, err := h.semanticPlanForTurnWithClassification("user", "send attachment and generate pdf", "lansenger", "root", "turn", &intent.ClassificationResult{
		Primary: intent.LabelAttachmentDelivery, Secondary: []intent.IntentLabel{intent.LabelDocumentGenerate}, Confidence: .98,
	})
	if !handled || err == nil || !strings.Contains(err.Error(), "conflicting") {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
}

func TestPetitionDeliveryBesideDocumentGenerateIsNotOpeningConflict(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry()}
	_, _, err := h.semanticPlanForTurnWithContextAndClassificationAndAttachments(
		withSemanticPetitionExpansion(context.Background()),
		"user", "", "desktop", "root-petition-deliver", "turn-petition-deliver",
		&intent.ClassificationResult{
			Primary:    intent.LabelDocumentGenerate,
			Secondary:  []intent.IntentLabel{intent.LabelAttachmentDelivery},
			Confidence: .98,
		},
		nil,
	)
	if err != nil && strings.Contains(err.Error(), "conflicting attachment_delivery and document_generate") {
		t.Fatalf("petition to deliver after generate was rejected as an opening conflict: %v", err)
	}
}

func TestIMSemanticDocumentGenerateWorkflowGateSharedByDispatcherAndPlanner(t *testing.T) {
	result := *liveDataGenerateClassification()
	if !imSemanticIntentIsManaged(result) {
		t.Fatal("non-workflow turn must stay managed")
	}
	if imSemanticIntentIsManagedForLoop(true, result) {
		t.Fatal("WorkflowAgentLoop + document_generate must not be managed")
	}
	if !imSemanticIntentIsManagedForLoop(true, intent.ClassificationResult{Primary: intent.LabelLiveData, Confidence: .98}) {
		t.Fatal("workflow without document_generate must keep current managed behavior")
	}
	h := &IMMessageHandler{registry: NewToolRegistry()}
	ctx := withSemanticWorkflowLoop(context.Background(), true)
	prepared, handled, err := h.semanticPlanForTurnWithContextAndClassificationAndAttachments(
		ctx, "user", "查询南京天气，并生成pdf报告", "desktop", "root", "turn", liveDataGenerateClassification(), nil,
	)
	if handled || prepared != nil || err != nil {
		t.Fatalf("workflow planner must return handled=false, prepared=%#v handled=%v err=%v", prepared, handled, err)
	}
}

func TestIMSemanticDocumentGeneratePublishedOnDesktopTUILansengerNotVE(t *testing.T) {
	if !semanticFileDeliveryPublished("desktop") || !semanticFileDeliveryPublished("tui") || !semanticFileDeliveryPublished("lansenger") || !semanticFileDeliveryPublished("lansenger_local") {
		t.Fatal("desktop/tui/lansenger must publish file deliver")
	}
	if semanticFileDeliveryPublished("ve_group_executor") {
		t.Fatal("VE must not publish generate file deliver")
	}
	if !semanticFileDeliveryPublished("weixin") {
		t.Fatal("weixin must publish receipt-aware file deliver")
	}
	h := registerDocumentGeneratePDF(t)
	for _, channel := range []string{"desktop", "tui", "lansenger"} {
		prepared, handled, err := h.semanticPlanForTurnWithClassification("user", "生成pdf报告", channel, "root-"+channel, "turn", documentGenerateClassification())
		if err != nil || !handled || prepared == nil || len(prepared.plan.Unmet) != 0 {
			t.Fatalf("channel=%s prepared=%#v handled=%v err=%v", channel, prepared, handled, err)
		}
		if !planHasCapabilities(prepared.plan, "document.generate.file", "artifact.deliver.current_channel") {
			t.Fatalf("channel=%s selections=%#v", channel, prepared.plan.Selections)
		}
	}
	_, handled, err := h.semanticPlanForTurnWithClassification("user", "生成pdf报告", "ve_group_executor", "root-ve", "turn", documentGenerateClassification())
	if !handled || err == nil || !strings.Contains(err.Error(), "unmet") {
		t.Fatalf("VE must HostReject via unmet, handled=%v err=%v", handled, err)
	}
}

func TestSemanticTreeConfirmedDocumentGeneratePlansBelowResolverFloor(t *testing.T) {
	h := registerDocumentGeneratePDF(t)
	prepared, handled, err := h.semanticPlanForTurnWithClassification(
		"user", "生成pdf报告", "desktop", "root-tree-generate", "turn-tree-generate",
		&intent.ClassificationResult{Primary: intent.LabelDocumentGenerate, Confidence: 0.75, Layer: 3},
	)
	if err != nil || !handled || prepared == nil || len(prepared.plan.Unmet) != 0 {
		t.Fatalf("tree-confirmed generate must plan, handled=%v err=%v prepared=%#v", handled, err, prepared)
	}
	if !planHasCapabilities(prepared.plan, "document.generate.file", "artifact.deliver.current_channel") {
		t.Fatalf("selections=%#v", prepared.plan.Selections)
	}
}

func TestIMSemanticDocumentDeliveryPrimaryIsNotRewrittenToGenerate(t *testing.T) {
	h := registerDocumentGenerateAndSearch(t)
	prepared, handled, err := h.semanticPlanForTurnWithClassification("user", "杭州天气，生成pdf报告", "desktop", "root-delivery-primary", "turn", &intent.ClassificationResult{
		Primary: intent.LabelDocumentDelivery, Secondary: []intent.IntentLabel{intent.LabelLiveData}, Confidence: 0.9,
	})
	if !handled || err == nil || !strings.Contains(err.Error(), "artifact.deliver.specified_target") {
		t.Fatalf("document_delivery must retain its declared target rather than being rewritten by PDF wording: prepared=%#v handled=%v err=%v", prepared, handled, err)
	}
}

func TestIMSemanticGenerateAndSpecifiedDeliveryRemainDistinct(t *testing.T) {
	h := registerDocumentGeneratePDF(t)
	prepared, handled, err := h.semanticPlanForTurnWithClassification("user", "生成pdf报告", "desktop", "root-gen-delivery", "turn", &intent.ClassificationResult{
		Primary: intent.LabelDocumentGenerate, Secondary: []intent.IntentLabel{intent.LabelDocumentDelivery}, Confidence: 0.9,
	})
	if !handled || err == nil || !strings.Contains(err.Error(), "artifact.deliver.specified_target") {
		t.Fatalf("current-channel generation must not silently discard semantic specified delivery: prepared=%#v handled=%v err=%v", prepared, handled, err)
	}
}

func TestIMSemanticLookupAndSpecifiedDeliveryAreNotRewrittenToGenerate(t *testing.T) {
	h := registerDocumentGenerateAndSearch(t)
	prepared, handled, err := h.semanticPlanForTurnWithClassification("user", "杭州天气，生成pdf报告", "desktop", "root-delivery-pdf", "turn", &intent.ClassificationResult{
		Primary: intent.LabelLiveData, Secondary: []intent.IntentLabel{intent.LabelDocumentDelivery}, Confidence: 0.9,
	})
	if !handled || err == nil || !strings.Contains(err.Error(), "artifact.deliver.specified_target") {
		t.Fatalf("lookup plus specified delivery must not acquire PDF generation from wording: prepared=%#v handled=%v err=%v", prepared, handled, err)
	}
}

// TestIMSemanticWeatherPDFDoesNotRewriteCoding guards the lexical override, not
// the migration gate. Weather wording inside a coding request must not turn the
// turn into a web-search plan.
//
// It used to assert a fail-closed HostReject, but that was only ever a side
// effect of coding having no capability rule: the unmapped check fires before
// any lexical reasoning, so the test would have passed even if the override had
// been wide open. Now that the coding family is managed the request plans, and
// the assertion can finally be about what the name claims.
func TestIMSemanticWeatherPDFDoesNotRewriteCoding(t *testing.T) {
	h := registerDocumentGeneratePDF(t)
	prepared, handled, err := h.semanticPlanForTurnWithClassification("user", "南京天气，生成pdf报告", "desktop", "root-coding-pdf", "turn", &intent.ClassificationResult{
		Primary: intent.LabelCoding, Secondary: []intent.IntentLabel{intent.LabelDocumentGenerate}, Confidence: 0.9,
	})
	if !handled || err != nil || prepared == nil {
		t.Fatalf("coding+pdf must plan, prepared=%#v handled=%v err=%v", prepared, handled, err)
	}
	if planHasCapabilities(prepared.plan, "information.search.web") {
		t.Fatalf("weather wording rewrote a coding turn into a search plan: %#v", prepared.plan.Selections)
	}
	if !planHasCapabilities(prepared.plan, "document.generate.file", "fs.write.local") {
		t.Fatalf("coding+document_generate selections=%#v", prepared.plan.Selections)
	}
}

func TestIMSemanticDegradedWeatherPDFDoesNotCreateGenerateCapability(t *testing.T) {
	h := registerDocumentGenerateAndSearch(t)
	prepared, handled, err := h.semanticPlanForTurnWithClassification("user", "南京天气，生成pdf报告", "desktop", "root-degraded-pdf", "turn", &intent.ClassificationResult{
		Primary: intent.LabelKnowledgeRead, Confidence: 0.61, Degraded: true, Layer: 3,
	})
	if err != nil || handled || prepared != nil {
		t.Fatalf("degraded wording must not create lookup or generation: prepared=%#v handled=%v err=%v", prepared, handled, err)
	}
}

func TestIMSemanticLiveDataGeneratePlansLookupThenGenerate(t *testing.T) {
	h := registerDocumentGenerateAndSearch(t)
	prepared, handled, err := h.semanticPlanForTurnWithClassification("user", "查询南京天气，并生成pdf报告", "desktop", "root", "turn", liveDataGenerateClassification())
	if err != nil || !handled || prepared == nil {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
	if !planHasCapabilities(prepared.plan, "information.search.web", "document.generate.file", "artifact.deliver.current_channel") {
		t.Fatalf("selections=%#v", prepared.plan.Selections)
	}
	assertGenerateRequiresOnlyBaseSearch(t, prepared.plan)
}

func assertGenerateRequiresOnlyBaseSearch(t *testing.T, plan tool.ToolPlan) {
	t.Helper()
	assertGenerateRequiresOnlyLookupFamilyBases(t, plan)
}

func assertGenerateRequiresOnlyLookupFamilyBases(t *testing.T, plan tool.ToolPlan) {
	t.Helper()
	var generate tool.PlannedSelection
	lookupIDs := make([]string, 0)
	for _, selection := range plan.Selections {
		switch {
		case selection.FitProof.MatchedCapability == "document.generate.file":
			generate = selection
		case tool.IsLookupCapability(selection.FitProof.MatchedCapability):
			lookupIDs = append(lookupIDs, selection.ID)
		}
	}
	if generate.ID == "" {
		t.Fatal("generate selection missing")
	}
	familyBase := make(map[string]string)
	for _, id := range lookupIDs {
		family := tool.RepeatFamilyID(id)
		if current, ok := familyBase[family]; !ok || id < current {
			familyBase[family] = id
		}
	}
	foundLookup := false
	for _, requirement := range generate.Requires {
		base, ok := familyBase[tool.RepeatFamilyID(requirement)]
		if !ok {
			continue
		}
		foundLookup = true
		if requirement != base {
			t.Fatalf("generate Requires ceiling sibling %s, want family base %s: %#v", requirement, base, generate.Requires)
		}
	}
	if !foundLookup {
		t.Fatalf("generate Requires=%#v, want a lookup family base from %v", generate.Requires, lookupIDs)
	}
}

func TestIMSemanticWeatherPDFClassifierPlansGovernedChain(t *testing.T) {
	h := registerDocumentGenerateAndSearch(t)
	h.unifiedClassifier = intent.New(intent.Config{
		Embedder: nil, // exercise the semantic tree rather than lexical recovery
		LLMFunc: func(_, userText string) (string, error) {
			if userText != "北京天气，输出 格式化pdf报告" {
				t.Fatalf("userText=%q", userText)
			}
			return `{"top":[{"skill":"live_data","score":0.95},{"skill":"document_generate","score":0.90},{"skill":"non_coding","score":0.40}]}`, nil
		},
	})

	prepared, handled, err := h.semanticPlanForTurnWithClassification(
		"user", "北京天气，输出 格式化pdf报告", "desktop", "root-classifier-pdf", "turn-classifier-pdf", nil,
	)
	if err != nil || !handled || prepared == nil {
		t.Fatalf("handled=%v err=%v prepared=%#v", handled, err, prepared)
	}
	if !planHasCapabilities(prepared.plan, "information.search.web", "document.generate.file", "artifact.deliver.current_channel") {
		t.Fatalf("selections=%#v", prepared.plan.Selections)
	}
	assertGenerateRequiresOnlyBaseSearch(t, prepared.plan)
}

func TestIMSemanticWeatherPDFNormalizesReversedSemanticComposite(t *testing.T) {
	h := registerDocumentGenerateAndSearch(t)
	classification := &intent.ClassificationResult{
		Primary: intent.LabelDocumentGenerate,
		Secondary: []intent.IntentLabel{
			intent.LabelLiveData,
		},
		Confidence: .95,
	}
	prepared, handled, err := h.semanticPlanForTurnWithClassification(
		"user", "生成当前天气 PDF", "desktop", "root-reversed-pdf", "turn-reversed-pdf", classification,
	)
	if err != nil || !handled || prepared == nil {
		t.Fatalf("handled=%v err=%v prepared=%#v", handled, err, prepared)
	}
	if !planHasCapabilities(prepared.plan, "information.search.web", "document.generate.file", "artifact.deliver.current_channel") {
		t.Fatalf("selections=%#v", prepared.plan.Selections)
	}
}

func TestIMSemanticDocumentGeneratePrimaryWithLiveDataSecondaryPlansLookupThenGenerate(t *testing.T) {
	h := registerDocumentGenerateAndSearch(t)
	prepared, handled, err := h.semanticPlanForTurnWithClassification("user", "将实时查询结果导出为PDF", "desktop", "root-generate-primary", "turn-generate-primary", &intent.ClassificationResult{
		Primary: intent.LabelDocumentGenerate, Secondary: []intent.IntentLabel{intent.LabelLiveData}, Confidence: .90, Layer: 2,
	})
	if err != nil || !handled || prepared == nil {
		t.Fatalf("handled=%v err=%v prepared=%#v", handled, err, prepared)
	}
	if !planHasCapabilities(prepared.plan, "information.search.web", "document.generate.file", "artifact.deliver.current_channel") {
		t.Fatalf("selections=%#v", prepared.plan.Selections)
	}
	var generate tool.PlannedSelection
	for _, selection := range prepared.plan.Selections {
		if selection.FitProof.MatchedCapability == "document.generate.file" {
			generate = selection
			break
		}
	}
	// The need ID is intentionally opaque; this structural check verifies a
	// lookup predecessor without depending on tool names or user text.
	hasLookupPredecessor := false
	for _, selection := range prepared.plan.Selections {
		if selection.FitProof.MatchedCapability == "information.search.web" && slices.Contains(generate.Requires, selection.ID) {
			hasLookupPredecessor = true
		}
	}
	if generate.ID == "" || !hasLookupPredecessor {
		t.Fatalf("generate requires=%#v selections=%#v, want lookup predecessor", generate.Requires, prepared.plan.Selections)
	}
}

func TestSemanticNeedsFromClassificationContextNormalizesOnlyLocalPureComposite(t *testing.T) {
	registry := newIMSemanticCapabilityRegistry()
	classification := intent.ClassificationResult{
		Primary:    intent.LabelDocumentGenerate,
		Secondary:  []intent.IntentLabel{intent.LabelLiveData},
		Confidence: .95,
	}
	needs, managed, err := semanticNeedsFromClassificationContext(context.Background(), registry, classification)
	seen := make(map[tool.CapabilityID]bool, len(needs))
	for _, need := range needs {
		seen[need.Capability] = true
	}
	if err != nil || !managed || !seen["information.search.web"] || !seen["document.generate.file"] || !seen["artifact.deliver.current_channel"] {
		t.Fatalf("needs=%#v managed=%v err=%v", needs, managed, err)
	}
	if classification.Primary != intent.LabelDocumentGenerate {
		t.Fatalf("normalization must not mutate caller-owned classification: %+v", classification)
	}
}

func TestLiveDataPDFPetitionSurvivesLookupSelectionCap(t *testing.T) {
	cb, _ := liveDataDesktopBeforePDFPetition(t)
	if cb.loopCtx == nil {
		t.Fatal("loop context missing")
	}
	cb.loopCtx.Runtime.Execution.ToolBudget = 1
	granted, message := cb.PetitionToolCall("generate_pdf")
	if granted || strings.Contains(message, "已由主机授权") {
		t.Fatalf("lookup cap must not turn the petition into a grant: granted=%v message=%q", granted, message)
	}
	if !successorNotYetListedDenial(message, "generate_pdf") || !strings.Contains(message, `"web_search"`) {
		t.Fatalf("lookup cap must still name the predecessor: %q", message)
	}
	if !planHasCapabilities(cb.semanticSurface.plan, "information.search.web", "document.generate.file", "artifact.deliver.current_channel") {
		t.Fatalf("petitioned chain missing under selection cap: %#v unmet=%#v", cb.semanticSurface.plan.Selections, cb.semanticSurface.plan.Unmet)
	}
	for _, selection := range cb.semanticSurface.plan.Selections {
		if selection.FitProof.MatchedCapability == tool.CapabilityInformationFetchWeb {
			t.Fatalf("selection cap must not resurrect an omitted fetch leg: %#v", cb.semanticSurface.plan.Selections)
		}
	}
}

// A non-expert live_data turn is a light prompt. Admitting generate_pdf used
// to leave that prompt in place, so the next request's authorizer stripped
// the grant setVisible had just recorded and the model was told the tool did
// not exist. The signed selection is not light-safe, so the turn has to leave
// the light contract before that request.
func TestLiveDataPDFPetitionLeavesLightPrompt(t *testing.T) {
	cb, searchName := liveDataDesktopBeforePDFPetition(t)
	if cb.loopCtx == nil {
		t.Fatal("loop context missing")
	}
	cb.loopCtx.Runtime.Execution.Layer = string(executionLayerLight)
	cb.loopCtx.Runtime.Execution.PromptProfile = "light"
	cb.loopCtx.Runtime.Execution.TaskType = string(intent.LabelLiveData)
	cb.loopCtx.Runtime.Execution.ToolBudget = 1
	cb.loopCtx.Runtime.Execution.IterationBudget = 3
	cb.loopCtx.Runtime.Execution.Reason = "semantic capability-managed lookup"
	cb.maxIter = 3
	cb.systemPrompt = "light fence: 不要生成文档"

	granted, message := cb.PetitionToolCall("generate_pdf")
	if granted || strings.Contains(message, "已由主机授权") {
		t.Fatalf("withheld generate must not be described as granted: granted=%v message=%q", granted, message)
	}
	if !successorNotYetListedDenial(message, "generate_pdf") || !strings.Contains(message, `"web_search"`) {
		t.Fatalf("petition must still name the lookup: %q", message)
	}
	profile := cb.loopCtx.Runtime.Execution
	if profile.PromptIsLight() || profile.IsLight() {
		t.Fatalf("petitioned generate must leave the light prompt: %+v", profile)
	}
	if profile.Reason != executionReasonPetitionLeftLight {
		t.Fatalf("reason=%q", profile.Reason)
	}
	if !cb.surfaceRefreshPending {
		t.Fatal("the next executed lookup must refresh the system prompt")
	}
	if strings.Contains(cb.systemPrompt, "不要生成文档") || !strings.Contains(cb.systemPrompt, "when a render or delivery tool is listed") {
		t.Fatalf("system prompt still forbids the signed generate step: %s", cb.systemPrompt)
	}
	if prompt := cb.PetitionSystemPrompt(); !strings.Contains(prompt, "when a render or delivery tool is listed") {
		t.Fatal("petition must publish the rewritten prompt for the next request")
	}
	if again := cb.PetitionSystemPrompt(); again != "" {
		t.Fatalf("petition prompt copy must be one-shot, got %q", again)
	}

	delta := []agent.ConversationEntry{{
		Role: "assistant", ToolCalls: []map[string]string{{"id": "call-search", "name": searchName}},
	}}
	_ = cb.OnToolBatchStarting(delta, agent.ToolBatchMetadata{Sequence: 1, LastToolName: searchName})
	if search := cb.ExecuteToolCall(searchName, `{"query":"兰州天气"}`, "call-search").Result; strings.Contains(search, "[system rejected]") {
		t.Fatalf("search result=%q", search)
	}
	committed := append(append([]agent.ConversationEntry(nil), delta...), agent.ConversationEntry{
		Role: "tool", Content: "兰州：多云，24°C / 11°C", ToolCallID: "call-search", ToolName: searchName,
	})
	if err := cb.OnToolBatchCommitted(committed, agent.ToolBatchMetadata{Sequence: 1, LastToolName: searchName}); err != nil {
		t.Fatal(err)
	}
	if !cb.RefreshAfterToolExecution(searchName) {
		t.Fatal("lookup commit must refresh the successor request")
	}
	if cb.loopCtx.Runtime.Execution.PromptIsLight() {
		t.Fatalf("prompt refresh wrote the light profile back: %+v", cb.loopCtx.Runtime.Execution)
	}
	name, grant := soleLiveSemanticGrantByAdapter(cb.semanticSurface, "generate_pdf")
	if name == "" || grant.Token == "" {
		t.Fatalf("generate grant missing after lookup: %#v", cb.semanticSurface.grants)
	}
	if tool.GrantSelectionIsLightPromptSafe(cb.semanticSurface.plan, cb.semanticSurface.grants, name) {
		t.Fatal("generate selection must not be light-prompt-safe")
	}
	if !cb.IsToolAllowed(name) {
		t.Fatal("full profile must allow the petitioned generate grant")
	}
	filtered := agent.FilterToolDefinitionsByAuthorizer(cb, cb.BuildToolsForModelRequest(cb.userText, 1))
	if !toolNamesContain(filtered, name) {
		t.Fatalf("authorizer stripped the petitioned generate grant: %v", toolNamesOf(filtered))
	}
}

func TestReadOnlyPlanStaysOnLightPrompt(t *testing.T) {
	ctx := NewLoopContext("light-readonly-petition", 3, nil)
	ctx.Runtime.Execution = ExecutionProfile{
		Layer: string(executionLayerLight), PromptProfile: "light",
		ToolBudget: 1, IterationBudget: 3, Reason: "semantic capability-managed lookup",
	}
	parent := tool.ToolPlan{Selections: []tool.PlannedSelection{
		{ID: "search", NeedID: "need:search", Effects: []tool.EffectClass{tool.EffectReadOnly}, FitProof: tool.FitProof{MatchedCapability: "information.search.web"}},
		{ID: "render", NeedID: "need:render", Effects: []tool.EffectClass{tool.EffectLocalMutation}, FitProof: tool.FitProof{MatchedCapability: "visual.render.live_data"}},
	}}
	cb := &sharedAgentLoopCallbacks{
		loopCtx: ctx,
		semanticSurface: &semanticCallSurface{
			plan: tool.ToolPlan{Selections: append(append([]tool.PlannedSelection{}, parent.Selections...), tool.PlannedSelection{
				ID: "fetch", NeedID: "need:fetch", Effects: []tool.EffectClass{tool.EffectReadOnly}, FitProof: tool.FitProof{MatchedCapability: tool.CapabilityInformationFetchWeb},
			})},
		},
		systemPrompt: "light fence",
	}
	cb.leaveLightProfileForUnsafePetition(parent)
	if !cb.loopCtx.Runtime.Execution.PromptIsLight() || cb.surfaceRefreshPending || cb.petitionPromptReady {
		t.Fatalf("read-only addition must stay light: %+v pending=%v ready=%v", cb.loopCtx.Runtime.Execution, cb.surfaceRefreshPending, cb.petitionPromptReady)
	}
	if cb.systemPrompt != "light fence" {
		t.Fatalf("read-only addition rewrote the prompt: %q", cb.systemPrompt)
	}
}

func TestAlreadyPlannedSendFileOnLightVisualStaysLight(t *testing.T) {
	ctx := NewLoopContext("light-visual-send", 3, nil)
	ctx.Runtime.Execution = ExecutionProfile{
		Layer: string(executionLayerLight), PromptProfile: "light",
		ToolBudget: 0, IterationBudget: 4, Reason: "semantic capability-managed live visual",
	}
	cb := &sharedAgentLoopCallbacks{
		handler: &IMMessageHandler{},
		loopCtx: ctx,
		semanticSurface: &semanticCallSurface{
			replan:    &semanticReplanInput{},
			completed: map[string]bool{},
			grants: map[string]tool.InvocationGrant{
				"web_search": {SelectionID: "search", Token: "live"},
			},
			plan: tool.ToolPlan{Selections: []tool.PlannedSelection{
				{ID: "search", NeedID: "need:search", Effects: []tool.EffectClass{tool.EffectReadOnly}, FitProof: tool.FitProof{MatchedCapability: "information.search.web"}},
				{ID: "render", NeedID: "need:render", Effects: []tool.EffectClass{tool.EffectLocalMutation}, FitProof: tool.FitProof{MatchedCapability: "visual.render.live_data"}},
				{ID: "deliver", NeedID: "need:deliver", Effects: []tool.EffectClass{tool.EffectExternalEffect}, FitProof: tool.FitProof{MatchedCapability: "artifact.deliver.current_channel"}},
			}},
		},
		systemPrompt: "light fence",
	}
	granted, message := cb.PetitionToolCall("send_file")
	if !ctx.Runtime.Execution.PromptIsLight() || ctx.Runtime.Execution.IterationBudget != 4 {
		t.Fatalf("premature send_file left the weather card: granted=%v message=%q profile=%+v", granted, message, ctx.Runtime.Execution)
	}
	if cb.systemPrompt != "light fence" || cb.petitionPromptReady || cb.surfaceRefreshPending {
		t.Fatalf("weather card prompt changed: prompt=%q ready=%v pending=%v", cb.systemPrompt, cb.petitionPromptReady, cb.surfaceRefreshPending)
	}
}

func toolNamesContain(defs []map[string]interface{}, name string) bool {
	for _, def := range defs {
		if extractToolName(def) == name {
			return true
		}
	}
	return false
}

func toolNamesOf(defs []map[string]interface{}) []string {
	names := make([]string, 0, len(defs))
	for _, def := range defs {
		names = append(names, extractToolName(def))
	}
	return names
}

func TestIMSemanticDefaultPlanningBudgetKeepsGenerateChain(t *testing.T) {
	h := registerDocumentGenerateAndSearch(t)
	prepared, handled, err := h.semanticPlanForTurnWithClassification("user", "查询南京天气，并生成pdf报告", "desktop", "root-budget-default", "turn", liveDataGenerateClassification())
	if err != nil || !handled || prepared == nil {
		t.Fatalf("default budget must fully materialize, handled=%v err=%v", handled, err)
	}
	if !planHasCapabilities(prepared.plan, "information.search.web", "document.generate.file", "artifact.deliver.current_channel") {
		t.Fatalf("default budget truncated generate chain: %#v", prepared.plan.Selections)
	}
	if len(prepared.plan.Unmet) != 0 {
		t.Fatalf("default budget must not report unmet: %#v", prepared.plan.Unmet)
	}
}

func TestIMSemanticHostToolBudgetOneKeepsSearchWaveReportsBudgetExceeded(t *testing.T) {
	h := registerDocumentGenerateAndSearch(t)
	ctx := withSemanticPlanningBudget(context.Background(), 1)
	prepared, handled, err := h.semanticPlanForTurnWithContextAndClassificationAndAttachments(
		ctx, "user", "查询南京天气，并生成pdf报告", "desktop", "root-budget-1", "turn", liveDataGenerateClassification(), nil,
	)
	if err != nil || !handled || prepared == nil {
		t.Fatalf("a budget cut that keeps search must still run, handled=%v err=%v", handled, err)
	}
	searchSiblings := 0
	for _, selection := range prepared.plan.Selections {
		if selection.FitProof.MatchedCapability != "information.search.web" {
			t.Fatalf("MaxSelections=1 must keep only the search family: %#v", prepared.plan.Selections)
		}
		searchSiblings++
	}
	if searchSiblings == 0 {
		t.Fatal("search family missing")
	}
	if len(prepared.plan.Unmet) != 2 {
		t.Fatalf("generate and deliver must remain as unmet, unmet=%#v", prepared.plan.Unmet)
	}
	for _, item := range prepared.plan.Unmet {
		if item.ReasonCode != "budget_exceeded" && item.ReasonCode != "planning_budget_exceeded" {
			t.Fatalf("unmet %s=%q, want budget_exceeded or planning_budget_exceeded", item.NeedID, item.ReasonCode)
		}
	}
}

func TestIMSemanticOfficeDoesNotProvidePDFGenerate(t *testing.T) {
	r := NewToolRegistry()
	registerBuiltinTools(r, &IMMessageHandler{})
	office, ok := r.Get("office")
	if !ok {
		t.Fatal("office missing")
	}
	for _, provision := range office.CapabilityProvisions {
		if provision.Capability == "document.generate.file" {
			t.Fatal("office must not provide document.generate.file")
		}
	}
	pdf, ok := r.Get("generate_pdf")
	if !ok || len(pdf.CapabilityProvisions) != 1 || pdf.CapabilityProvisions[0].Capability != "document.generate.file" {
		t.Fatalf("generate_pdf annotation=%#v", pdf)
	}
}

func TestIMSemanticGeneratePDFSchemaHasNoPathOrDestination(t *testing.T) {
	schema, err := semanticInvocationSchema(semanticGeneratePDFDefinition())
	if err != nil {
		t.Fatal(err)
	}
	properties, _ := schema["properties"].(map[string]interface{})
	for _, blocked := range []string{"path", "artifact_id", "base64", "channel", "destination", "phase_id", "file_base64"} {
		if _, ok := properties[blocked]; ok {
			t.Fatalf("semantic generate schema still exposes %s", blocked)
		}
	}
	for _, required := range []string{"content", "title", "doc_type"} {
		if _, ok := properties[required]; !ok {
			t.Fatalf("semantic generate schema missing %s", required)
		}
	}
}

func TestIMSemanticDocumentGeneratePublishesArtifactWithoutFileBase64(t *testing.T) {
	h := registerDocumentGeneratePDF(t)
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassificationAndAttachments(
		"user-1", "生成pdf报告", "desktop", "root-gen", "turn-gen", documentGenerateClassification(), nil,
	)
	if err != nil || !handled || surface == nil || len(defs) == 0 {
		t.Fatalf("defs=%#v handled=%v err=%v", defs, handled, err)
	}
	var generateSel tool.PlannedSelection
	for _, selection := range surface.plan.Selections {
		if selection.FitProof.MatchedCapability == "document.generate.file" {
			generateSel = selection
		}
	}
	if generateSel.ID == "" {
		t.Fatalf("generate selection missing: %#v", surface.plan.Selections)
	}
	generateName := extractToolName(defs[0])
	if grant, ok := surface.grants[generateName]; !ok || grant.SelectionID != generateSel.ID {
		for name, grant := range surface.grants {
			if grant.SelectionID == generateSel.ID {
				generateName = name
				break
			}
		}
	}
	cb := &sharedAgentLoopCallbacks{
		handler: h, semanticSurface: surface, platform: "desktop",
		loopCtx: &LoopContext{DeliveryTarget: &agent.DeliveryTarget{ChannelScope: "desktop", DestinationID: "user:user-1"}},
	}
	got := cb.ExecuteTool(generateName, `{"content":"# 天气\n18C","title":"南京天气"}`)
	if strings.Contains(got, "[file_base64") || strings.Contains(got, "path") {
		t.Fatalf("generate leaked delivery payload: %q", got)
	}
	if !strings.Contains(got, "PDF artifact published") {
		t.Fatalf("generate result=%q", got)
	}
	published, err := surface.artifacts.store.PublishedArtifacts(surface.scope, generateSel.ID)
	if err != nil || len(published) == 0 {
		t.Fatalf("generate must publish an ArtifactRef: published=%#v err=%v", published, err)
	}
}

// TestIMSemanticWeatherPDFAdvancesToDeliverAfterSearchAndGenerate reproduces
// the live desktop path: search is the only initial grant, generate is issued
// after search succeeds, and current-channel file deliver must then appear.
// App-backed execution uses the SQLite coordinator; a generate-only memory
// host can hide the advance failure that swallows a successful PDF.
func TestIMSemanticWeatherPDFAdvancesToDeliverAfterSearchAndGenerate(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	defer app.closeSemanticInvocationStore()
	h := registerDocumentGenerateAndSearch(t)
	h.app = app
	h.semanticTrustedWebSearch = func(userID, query string) (string, error) {
		return "Nanjing weather: cloudy, 26C", nil
	}
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassificationAndAttachments(
		"user-1", "南京天气，生成pdf报告", "desktop", "root-weather-pdf", "turn-weather-pdf", liveDataGenerateClassification(), nil,
	)
	if err != nil || !handled || surface == nil || len(defs) == 0 {
		t.Fatalf("defs=%#v handled=%v err=%v", defs, handled, err)
	}
	if surface.coordinator == nil {
		t.Fatal("desktop weather+pdf must use the coordinated execution owner")
	}
	if !planHasCapabilities(surface.plan, "information.search.web", "document.generate.file", "artifact.deliver.current_channel") {
		t.Fatalf("selections=%#v", surface.plan.Selections)
	}
	searchName := semanticGrantNameForAdapter(surface, semanticTrustedWebSearchAdapter)
	if searchName == "" {
		t.Fatalf("initial surface lost the search grant: %#v", surface.grants)
	}
	cb := &sharedAgentLoopCallbacks{
		handler: h, semanticSurface: surface, platform: "desktop",
		loopCtx: &LoopContext{
			ID: "weather-pdf", Runtime: RuntimeContext{RequestID: "req-weather-pdf"},
			DeliveryTarget: &agent.DeliveryTarget{ChannelScope: "desktop", DestinationID: "user:user-1"},
		},
	}
	search := cb.ExecuteToolCall(searchName, `{"query":"南京天气"}`, "call-search").Result
	if strings.Contains(search, "[system rejected]") {
		t.Fatalf("search result=%q", search)
	}
	var generateName string
	for name, grant := range surface.grants {
		if grant.AdapterName == "generate_pdf" {
			generateName = name
		}
	}
	if generateName == "" {
		t.Fatalf("generate grant missing after search: tools=%#v grants=%#v", cb.tools, surface.grants)
	}
	got := cb.ExecuteToolCall(generateName, `{"content":"# 南京天气\n18C","title":"南京天气"}`, "call-generate").Result
	if strings.Contains(got, "semantic_plan_advance_failed") {
		t.Fatalf("successful generate must not hide behind advance failure: %q", got)
	}
	if !strings.Contains(got, "PDF artifact published") {
		t.Fatalf("generate result=%q", got)
	}
	var deliveryName string
	for name, grant := range surface.grants {
		if grant.AdapterName == "semantic_deliver_current_file" {
			deliveryName = name
		}
	}
	if deliveryName == "" {
		t.Fatalf("deliver grant missing after generate: tools=%#v grants=%#v result=%q", cb.tools, surface.grants, got)
	}
}

func TestIMSemanticWeatherPDFHostGeneratesWhenModelStopsAfterSearch(t *testing.T) {
	cb := weatherPDFDesktopReadyAfterSearch(t)
	resp := &IMAgentResponse{Text: "已查询到南京天气数据 ✅\n\n**南京天气概览**\n- 今天：多云，26℃，风力不大\n- 明天：小雨，25℃\n\n接下来我将为这份南京天气生成 PDF 报告，请稍候～"}
	attachSharedLoopArtifacts(resp, cb)
	if resp.FileMimeType != "application/pdf" || resp.LocalFilePath == "" {
		t.Fatalf("host must render and attach the PDF after the model stopped: %+v", resp)
	}
	if !strings.Contains(resp.Text, "26℃") || !strings.Contains(resp.Text, "25℃") || !strings.Contains(resp.Text, "请稍候") {
		t.Fatalf("host generate must keep the weather answer: %q", resp.Text)
	}
	if _, grant := soleLiveSemanticGrantByAdapter(cb.semanticSurface, "generate_pdf"); grant.Token != "" {
		t.Fatal("host generate must consume the one-shot generate_pdf grant")
	}
}

func TestHostAutoGeneratePDFSkippedOnInteractivePause(t *testing.T) {
	cb := weatherPDFDesktopReadyAfterSearch(t)
	name, grant := soleLiveSemanticGrantByAdapter(cb.semanticSurface, "generate_pdf")
	if name == "" || grant.Token == "" {
		t.Fatal("generate_pdf grant missing after search")
	}
	cb.skipHostAutoFileDelivery = true
	resp := &IMAgentResponse{Text: "已查询到南京天气数据 ✅\n\n今天多云，26℃。\n\n接下来我将为这份南京天气生成 PDF 报告，请稍候～"}
	attachSharedLoopArtifacts(resp, cb)
	if _, ok := cb.semanticSurface.grants[name]; !ok {
		t.Fatal("interactive pause must not consume generate_pdf")
	}
	if resp.LocalFilePath != "" || resp.FileData != "" {
		t.Fatalf("must not auto-generate during ask_user/record_audio: %+v", resp)
	}
}

func TestHostAutoGeneratePDFUsesSearchEvidenceWhenAssistantIsEmpty(t *testing.T) {
	cb := weatherPDFDesktopReadyAfterSearch(t)
	if strings.TrimSpace(cb.semanticLookupEvidence) == "" {
		t.Fatal("successful search must stash lookup evidence for host generate")
	}
	resp := &IMAgentResponse{}
	attachSharedLoopArtifacts(resp, cb)
	if resp.FileMimeType != "application/pdf" || resp.LocalFilePath == "" {
		t.Fatalf("host must generate from search evidence: %+v", resp)
	}
	if !cb.turnPublishedPDF || resp.Text != "已生成「南京天气」PDF。" {
		t.Fatalf("a PDF published this turn with an empty answer uses the publication receipt: published=%v text=%q", cb.turnPublishedPDF, resp.Text)
	}
}

func TestHostGenerateDoesNotPublishAfterDocumentDelivery(t *testing.T) {
	cb := weatherPDFDesktopReadyAfterSearch(t)
	name, grant := soleLiveSemanticGrantByAdapter(cb.semanticSurface, "generate_pdf")
	if name == "" || grant.Token == "" {
		t.Fatal("generate grant missing after search")
	}
	cb.userText = "发到微信"
	cb.filesForwarded = 1
	cb.semanticDeliveryFileData = base64.StdEncoding.EncodeToString([]byte("%PDF-1.4\nalready-delivered"))
	cb.semanticDeliveryFileName = "崇州天气与风土人情.pdf"
	cb.semanticDeliveryFileMIME = "application/pdf"
	resp := &IMAgentResponse{}
	attachSharedLoopArtifacts(resp, cb)
	if _, ok := cb.semanticSurface.grants[name]; !ok {
		t.Fatal("a delivered document must not consume the live generate grant")
	}
	if cb.turnPublishedPDF || strings.Contains(resp.Text, "已生成") || strings.Contains(resp.Text, "发到微信") {
		t.Fatalf("host published a second report after delivery: published=%v text=%q", cb.turnPublishedPDF, resp.Text)
	}
	want := localizeIMProactiveCaption(cb.handler.imUILangOrZh(), "崇州天气与风土人情.pdf", "application/pdf")
	if resp.FileName != "崇州天气与风土人情.pdf" || resp.Text != want || !strings.Contains(resp.Text, "崇州天气与风土人情.pdf") {
		t.Fatalf("delivered file was replaced: name=%q text=%q want=%q path=%q", resp.FileName, resp.Text, want, resp.LocalFilePath)
	}
}

func TestHostAutoGeneratePDFRunsAfterLLMErrorWhenSearchSucceeded(t *testing.T) {
	cb := weatherPDFDesktopReadyAfterSearch(t)
	resp := &IMAgentResponse{Text: "已查询到南京天气", Error: "LLM call failed: timeout"}
	attachSharedLoopArtifacts(resp, cb)
	if resp.FileMimeType != "application/pdf" || resp.LocalFilePath == "" {
		t.Fatalf("search evidence must still become a PDF after a later LLM error: %+v", resp)
	}
	if resp.Error != "" {
		t.Fatalf("desktop UI treats Error as a failed round and drops the PDF: %q", resp.Error)
	}
}

func TestHostAutoGeneratePDFReplacesPromiseOnlyTextAfterTimeout(t *testing.T) {
	cb := weatherPDFDesktopReadyAfterSearch(t)
	resp := &IMAgentResponse{Text: "接下来我将为这份南京天气生成 PDF 报告，请稍候～", Error: "LLM call failed: timeout"}
	attachSharedLoopArtifacts(resp, cb)
	if resp.FileMimeType != "application/pdf" || resp.LocalFilePath == "" {
		t.Fatalf("promise-only stop must still attach a PDF: %+v", resp)
	}
	if resp.Error != "" {
		t.Fatalf("stale LLM timeout must not hide the PDF: %q", resp.Error)
	}
	if !strings.Contains(resp.Text, "请稍候") || !strings.Contains(resp.Text, "接下来我将") {
		t.Fatalf("attaching a PDF must not rewrite the answer: %q", resp.Text)
	}
}

func TestHostAutoGeneratePDFReplacesWaitOnlyTextAfterTimeout(t *testing.T) {
	cb := weatherPDFDesktopReadyAfterSearch(t)
	resp := &IMAgentResponse{Text: "请稍候～", Error: "LLM call failed: timeout"}
	attachSharedLoopArtifacts(resp, cb)
	if resp.FileMimeType != "application/pdf" || resp.LocalFilePath == "" {
		t.Fatalf("wait-only stop must still attach a PDF: %+v", resp)
	}
	if resp.Error != "" {
		t.Fatalf("stale LLM timeout must not hide the PDF: %q", resp.Error)
	}
	if !strings.Contains(resp.Text, "请稍候") {
		t.Fatalf("attaching a PDF must not rewrite the answer: %q", resp.Text)
	}
}

func TestHostOwnedFileAttachKeepsWeatherAnswer(t *testing.T) {
	summary := "崇州天气 PDF 已生成并发送给你了。\n\n今日（10/03 周六）：多云转小雨，25/16°C，微风，湿度约79%，外出带伞。\n未来趋势：4日小雨 23/16°C，5日阴转多云 21/14°C。"
	resp := &IMAgentResponse{
		Text:          summary,
		FileName:      "崇州天气预报.pdf",
		FileMimeType:  "application/pdf",
		LocalFilePath: `C:\tmp\崇州天气预报.pdf`,
	}
	finalizeHostOwnedFileResponse(resp, &sharedAgentLoopCallbacks{
		userText:               "崇州天气",
		semanticLookupEvidence: "Public web results for 崇州天气\n25/16℃",
	})
	if !strings.Contains(resp.Text, "25/16") || !strings.Contains(resp.Text, "21/14") {
		t.Fatalf("weather answer was replaced by a file receipt: %q", resp.Text)
	}
	if strings.Contains(resp.Text, "已生成「") {
		t.Fatalf("receipt hid the weather: %q", resp.Text)
	}
}

func TestHostOwnedFileAttachDoesNotRelabelNonPDFAsPDF(t *testing.T) {
	resp := &IMAgentResponse{
		Text:          "请稍候～",
		Error:         "LLM call failed: timeout",
		FileMimeType:  "text/plain",
		FileName:      "notes.txt",
		LocalFilePath: `C:\tmp\notes.txt`,
	}
	finalizeHostOwnedFileResponse(resp, &sharedAgentLoopCallbacks{userText: "杭州天气，生成pdf报告"})
	if resp.Text != "请稍候～" {
		t.Fatalf("non-PDF attach must not be rewritten as a PDF receipt: %q", resp.Text)
	}
	if resp.Error != "" {
		t.Fatalf("stale loop error must still clear when any file attached: %q", resp.Error)
	}
}

func TestHostOwnedFileAttachKeepsPolicyErrors(t *testing.T) {
	cb := weatherPDFDesktopReadyAfterSearch(t)
	name, grant := soleLiveSemanticGrantByAdapter(cb.semanticSurface, "generate_pdf")
	if name == "" || grant.Token == "" {
		t.Fatal("generate_pdf grant missing after search")
	}
	resp := &IMAgentResponse{Text: "已查询到南京天气", Error: "semantic_capability_unmet"}
	attachSharedLoopArtifacts(resp, cb)
	if _, ok := cb.semanticSurface.grants[name]; !ok {
		t.Fatal("policy errors must not consume generate_pdf")
	}
	if resp.LocalFilePath != "" || resp.FileData != "" {
		t.Fatalf("must not auto-generate after a policy error: %+v", resp)
	}
	if resp.Error != "semantic_capability_unmet" {
		t.Fatalf("policy errors must stay visible: %q", resp.Error)
	}
}

func TestHostAutoGeneratePDFRunsOnHardExitAfterSearch(t *testing.T) {
	cb := weatherPDFDesktopReadyAfterSearch(t)
	hard := &IMAgentResponse{Text: "已查询到南京天气", HardExit: true}
	attachSharedLoopArtifacts(hard, cb)
	if hard.FileMimeType != "application/pdf" || hard.LocalFilePath == "" {
		t.Fatalf("consecutive-empty HardExit after search must still attach a PDF: %+v", hard)
	}
}

func TestHostAutoGeneratePDFSkippedOnCheckpointFailure(t *testing.T) {
	cb := weatherPDFDesktopReadyAfterSearch(t)
	name, grant := soleLiveSemanticGrantByAdapter(cb.semanticSurface, "generate_pdf")
	if name == "" || grant.Token == "" {
		t.Fatal("generate_pdf grant missing after search")
	}
	resp := &IMAgentResponse{Text: "已查询到南京天气", Error: "recovery_checkpoint_failed", HardExit: true}
	attachSharedLoopArtifacts(resp, cb)
	if _, ok := cb.semanticSurface.grants[name]; !ok {
		t.Fatal("checkpoint failure must not consume generate_pdf")
	}
	if resp.LocalFilePath != "" || resp.FileData != "" {
		t.Fatalf("must not auto-generate after checkpoint failure: %+v", resp)
	}
}

func TestIMSemanticGeneratePDFDropsDecorativeDateField(t *testing.T) {
	cb := weatherPDFDesktopReadyAfterSearch(t)
	name, grant := soleLiveSemanticGrantByAdapter(cb.semanticSurface, "generate_pdf")
	if name == "" || grant.Token == "" {
		t.Fatal("generate_pdf grant missing after search")
	}
	got := cb.ExecuteToolCall(name, `{"content":"# 南京天气\n18C","title":"南京天气","date":"2026-08-20"}`, "call-date").Result
	if !strings.Contains(got, "PDF artifact published") {
		t.Fatalf("decorative date must not fail generate: %q", got)
	}
}

func TestSemanticGeneratePDFInvocationArgsWashesWithoutTitle(t *testing.T) {
	got := semanticGeneratePDFInvocationArgs(`{"content":"# 南京\n18C","date":"2026-08-20","query":"南京天气","path":"out.pdf"}`)
	if strings.Contains(got, `"date"`) || strings.Contains(got, `"query"`) || strings.Contains(got, `"path"`) {
		t.Fatalf("decorative extras survived: %s", got)
	}
	if !strings.Contains(got, "2026-08-20") || strings.Contains(got, "日期：") {
		t.Fatalf("date should fold into content without a language label: %s", got)
	}
	if strings.Contains(got, `"title"`) {
		t.Fatalf("absent title must stay omitted: %s", got)
	}
	if keep := semanticGeneratePDFInvocationArgs(`{"query":"南京天气"}`); keep != `{"query":"南京天气"}` {
		t.Fatalf("query-only must not be remapped: %s", keep)
	}
	if keep := semanticGeneratePDFInvocationArgs(`{"path":"out.pdf","title":"南京天气"}`); !strings.Contains(keep, `"path"`) {
		t.Fatalf("path+title without content must stay rejected: %s", keep)
	}
	titleOnly := semanticGeneratePDFInvocationArgs(`{"content":"南京天气报告","output_path":"C:\\\\tmp\\\\n.pdf"}`)
	if !strings.Contains(titleOnly, `"output_path"`) {
		t.Fatalf("title-only content plus output_path must stay rejected: %s", titleOnly)
	}
	noDateFold := semanticGeneratePDFInvocationArgs(`{"content":"南京天气报告","date":"2026-08-20"}`)
	if noDateFold != `{"content":"南京天气报告","date":"2026-08-20"}` {
		t.Fatalf("title-only plus date must not be folded into a fake body: %s", noDateFold)
	}
	weather := semanticGeneratePDFInvocationArgs(`{"content":"南京今日天气：小雨转多云，气温25-32℃，东风4-5级","output":"C:\\\\tmp\\\\n.pdf","title":"南京天气报告"}`)
	if strings.Contains(weather, `"output"`) || !strings.Contains(weather, "25-32") || !strings.Contains(weather, `"title"`) {
		t.Fatalf("weather body plus output must wash: %s", weather)
	}
	if semanticGeneratePDFLooksLikeTitleOnly("南京今日小雨转多云，外出请带伞。") {
		t.Fatal("punctuated weather must not look like a title")
	}
	if semanticGeneratePDFLooksLikeTitleOnly("Bring an umbrella.") {
		t.Fatal("an English sentence must not look like a title")
	}
	if !semanticGeneratePDFLooksLikeTitleOnly("report.pdf") {
		t.Fatal("a dotted token must stay title-only")
	}
	if !semanticGeneratePDFLooksLikeTitleOnly("南京天气报告") {
		t.Fatal("report title must stay title-only")
	}
	if !semanticGeneratePDFLooksLikeTitleOnly("# 南京天气报告") {
		t.Fatal("heading-only markdown must stay title-only")
	}
	if !semanticGeneratePDFLooksLikeTitleOnly("# 南京天气报告\n\n") {
		t.Fatal("heading plus trailing blanks must stay title-only")
	}
	if semanticGeneratePDFLooksLikeTitleOnly("# 南京天气\n\n小雨31℃") {
		t.Fatal("heading plus weather body was treated as a title")
	}
	if !semanticGeneratePDFLooksLikeTitleOnly("Weekly weather notes") {
		t.Fatal("a short line without a body must look title-only")
	}
	if semanticGeneratePDFLooksLikeTitleOnly(strings.Repeat("天气", 13)) {
		t.Fatal("a long line must not look title-only")
	}
	if !semanticGeneratePDFArgsTooThin(`{"content":"# Weekly Status\n","title":"Weekly Status"}`) {
		t.Fatal("heading identical to title must stay thin")
	}
	if !semanticGeneratePDFArgsTooThin(`{"content":"Weekly Status","title":"Weekly Status"}`) {
		t.Fatal("content identical to title must stay thin")
	}
	if keep := semanticGeneratePDFInvocationArgs(`{"content":true,"title":"南京天气","date":"2026-08-20"}`); strings.Contains(keep, `"content":"`) {
		t.Fatalf("non-string content must not be coerced into a report: %s", keep)
	}
	noFold := semanticGeneratePDFInvocationArgs(`{"content":"# 南京\n18C","date":"南京天气"}`)
	if strings.Contains(noFold, `"date"`) || strings.Contains(noFold, "南京天气") {
		t.Fatalf("query-shaped date must be dropped, not folded: %s", noFold)
	}
}

func TestHostAutoGeneratePDFSurvivesModelSchemaRejectAfterSearch(t *testing.T) {
	cb := weatherPDFDesktopReadyAfterSearch(t)
	name, grant := soleLiveSemanticGrantByAdapter(cb.semanticSurface, "generate_pdf")
	if name == "" || grant.Token == "" {
		t.Fatal("generate_pdf grant missing after search")
	}
	if allowed, reason := cb.IsToolCallAllowed(name, `{"query":"南京天气报告"}`); allowed {
		t.Fatalf("search-shaped generate_pdf must stay at Intake: reason=%q", reason)
	}
	if allowed, reason := cb.IsToolCallAllowed(name, `{"content":"南京小雨31℃","title":"南京天气","date":"2026-08-20"}`); !allowed {
		t.Fatalf("decorative date must be dropped when content+title are present: %q", reason)
	}
	if allowed, reason := cb.IsToolCallAllowed(name, `{"content":"南京小雨31℃","title":"南京天气","query":"南京天气报告"}`); !allowed {
		t.Fatalf("extra query must be dropped when content+title are present: %q", reason)
	}
	if allowed, reason := cb.IsToolCallAllowed(name, `{"content":"南京小雨31℃","date":"2026-08-20"}`); !allowed {
		t.Fatalf("content+date without title is schema-valid after wash: %q", reason)
	}
	if allowed, reason := cb.IsToolCallAllowed(name, `{"path":"out.pdf","title":"南京天气"}`); allowed {
		t.Fatalf("path+title without content must stay at Intake: reason=%q", reason)
	}
	if allowed, reason := cb.IsToolCallAllowed(name, `{"content":"南京天气报告"}`); allowed {
		t.Fatalf("title-only content must stay at Intake: reason=%q", reason)
	}
	if allowed, reason := cb.IsToolCallAllowed(name, `{"content":"# 南京天气报告"}`); allowed {
		t.Fatalf("heading-only content must stay at Intake: reason=%q", reason)
	}
	if allowed, reason := cb.IsToolCallAllowed(name, `{"content":"# 南京天气报告\n\n"}`); allowed {
		t.Fatalf("heading plus trailing blanks must stay at Intake: reason=%q", reason)
	}
	if allowed, reason := cb.IsToolCallAllowed(name, `{"content":"南京今日小雨转多云，外出请带伞。"}`); !allowed {
		t.Fatalf("weather sentence without digits must still be a report body: %q", reason)
	}
	if allowed, reason := cb.IsToolCallAllowed(name, `{"content":"南京天气报告","output_path":"C:\\\\tmp\\\\n.pdf"}`); allowed {
		t.Fatalf("title-only content plus output_path must stay at Intake: reason=%q", reason)
	}
	if allowed, reason := cb.IsToolCallAllowed(name, `{"content":"南京今日天气：小雨转多云，气温25-32℃","output":"C:\\\\tmp\\\\n.pdf","title":"南京天气报告"}`); !allowed {
		t.Fatalf("weather body plus output must be washed: %q", reason)
	}
	if allowed, reason := cb.IsToolCallAllowed(name, `{"content":"南京小雨31℃","title":"南京天气"}`); !allowed {
		t.Fatalf("valid generate_pdf args must still reach Admission: %q", reason)
	}
	if _, live := cb.semanticSurface.grants[name]; !live {
		t.Fatal("Intake reject must not consume generate_pdf")
	}
	resp := &IMAgentResponse{Text: "PDF生成失败（参数无效）\n当前轮次未授权 `generate_pdf` 工具，无法生成PDF。天气数据已获取：南京小雨31℃。"}
	attachSharedLoopArtifacts(resp, cb)
	if resp.FileMimeType != "application/pdf" || resp.LocalFilePath == "" {
		t.Fatalf("host must still attach the PDF after a model schema reject: %+v", resp)
	}
	if strings.Contains(resp.Text, "未授权") || strings.Contains(resp.Text, "generate_pdf") {
		t.Fatalf("tool-status sentence survived host attach: %q", resp.Text)
	}
	if !strings.Contains(resp.Text, "南京小雨31℃") || !strings.Contains(resp.Text, "PDF生成失败") {
		t.Fatalf("sentences that do not cite the tool id must stay: %q", resp.Text)
	}
	if msg := cb.ToolDenialMessage("generate_pdf"); strings.Contains(msg, "not listed yet") || !strings.Contains(msg, "already used") {
		t.Fatalf("consumed generate denial=%q", msg)
	}
}

func TestHostAutoGeneratePDFWithoutThisTurnSearchUsesAssistantReport(t *testing.T) {
	cb, _ := weatherPDFDesktopBeforeSearch(t)
	if name, _ := soleLiveSemanticGrantByAdapter(cb.semanticSurface, "generate_pdf"); name != "" {
		t.Fatal("generate must stay hidden before search")
	}
	weather := "彭州今日多云转小雨，气温22到29摄氏度，东南风3到4级，空气质量良好。午后外出建议携带雨具，路面可能湿滑。夜间气温下降较明显，请适当添衣。明天白天仍有分散阵雨，能见度一般。"
	if !substantialHostPDFReportText(weather) {
		t.Fatal("fixture must be a substantial report")
	}
	resp := &IMAgentResponse{Text: weather + "\n不过，当前回合工具列表中没有 PDF 生成工具 (generate_pdf) 授权，我暂时无法直接生成 PDF 文件。请重新发起生成 PDF 的请求，授权工具出现后我会立即调用并输出报告文件。"}
	attachSharedLoopArtifacts(resp, cb)
	if resp.FileMimeType != "application/pdf" || resp.LocalFilePath == "" {
		t.Fatalf("host must generate from assistant weather without this-turn search: %+v", resp)
	}
	if strings.Contains(resp.Text, "generate_pdf") || strings.Contains(resp.Text, "无法直接生成") || strings.Contains(resp.Text, "工具列表中没有") {
		t.Fatalf("authorization excuse survived host attach: %q", resp.Text)
	}
	if !strings.Contains(resp.Text, "彭州") {
		t.Fatalf("host attach lost the weather summary: %q", resp.Text)
	}
}

func TestHostAutoGeneratePDFWithoutSearchOrReportLeavesGrantUnissued(t *testing.T) {
	cb, searchName := weatherPDFDesktopBeforeSearch(t)
	resp := &IMAgentResponse{Text: "请稍候"}
	attachSharedLoopArtifacts(resp, cb)
	if resp.LocalFilePath != "" || resp.FileData != "" {
		t.Fatalf("must not invent a PDF without report content: %+v", resp)
	}
	if name, _ := soleLiveSemanticGrantByAdapter(cb.semanticSurface, "generate_pdf"); name != "" {
		t.Fatal("empty report must not host-satisfy lookup just to mint generate")
	}
	if _, ok := cb.semanticSurface.grants[searchName]; !ok {
		t.Fatal("unused search grant must stay available when no report exists")
	}
}

func TestHostAutoGeneratePDFDoesNotUnlockDuringParallelSearchBatch(t *testing.T) {
	cb, searchName := weatherPDFDesktopBeforeSearch(t)
	delta := []agent.ConversationEntry{{
		Role: "assistant", ToolCalls: []map[string]string{{"id": "call-search", "name": "web_search"}},
	}}
	_ = cb.OnToolBatchStarting(delta, agent.ToolBatchMetadata{Sequence: 1, LastToolName: "web_search"})
	if !cb.semanticHoldDependantIssue {
		t.Fatal("a non-empty tool batch must hold dependant issue")
	}
	if msg := cb.ToolDenialMessage("generate_pdf"); !strings.Contains(msg, "not listed yet") || !strings.Contains(msg, "do not ask the user to re-authorize") {
		t.Fatalf("unissued generate denial=%q", msg)
	}
	if search := cb.ExecuteToolCall(searchName, `{"query":"南京天气"}`, "call-search").Result; strings.Contains(search, "[system rejected]") {
		t.Fatalf("search result=%q", search)
	}
	if name, grant := soleLiveSemanticGrantByAdapter(cb.semanticSurface, "generate_pdf"); name != "" || grant.Token != "" {
		t.Fatalf("generate must not unlock mid-batch: name=%q grant=%#v", name, grant)
	}
	if cb.IsToolAllowed("generate_pdf") {
		t.Fatal("unlisted generate_pdf must stay unauthorized until the batch ends")
	}
	committed := append(append([]agent.ConversationEntry(nil), delta...), agent.ConversationEntry{
		Role: "tool", Content: "Nanjing weather: cloudy, 26C", ToolCallID: "call-search", ToolName: "web_search",
	})
	_ = cb.OnToolBatchCommitted(committed, agent.ToolBatchMetadata{Sequence: 1, LastToolName: "web_search"})
	if cb.semanticHoldDependantIssue {
		t.Fatal("batch commit must release the dependant hold")
	}
	if name, grant := soleLiveSemanticGrantByAdapter(cb.semanticSurface, "generate_pdf"); name == "" || grant.Token == "" {
		t.Fatalf("generate must issue after the batch: grants=%#v", cb.semanticSurface.grants)
	}
	resp := &IMAgentResponse{Text: "已查询到南京天气"}
	attachSharedLoopArtifacts(resp, cb)
	if resp.FileMimeType != "application/pdf" || resp.LocalFilePath == "" {
		t.Fatalf("host must generate after the delayed unlock: %+v", resp)
	}
}

// Production 2026-08-29 张惠妹 turn: primary=search (MaxInvocations=5) plus
// document_generate. The five search siblings used to all be required, so
// generate waited for unused refinements and never unlocked after one
// successful web_search. One committed search must issue generate_pdf.
func TestSearchDocumentGenerateUnlocksPDFAfterOneCommittedSearch(t *testing.T) {
	cb, searchName := searchPDFDesktopBeforeSearch(t)
	assertGenerateRequiresOnlyBaseSearch(t, cb.semanticSurface.plan)
	delta := []agent.ConversationEntry{{
		Role: "assistant", ToolCalls: []map[string]string{{"id": "call-search", "name": "web_search"}},
	}}
	_ = cb.OnToolBatchStarting(delta, agent.ToolBatchMetadata{Sequence: 1, LastToolName: "web_search"})
	if search := cb.ExecuteToolCall(searchName, `{"query":"张惠妹歌曲列表"}`, "call-search").Result; strings.Contains(search, "[system rejected]") {
		t.Fatalf("search result=%q", search)
	}
	if name, grant := soleLiveSemanticGrantByAdapter(cb.semanticSurface, "generate_pdf"); name != "" || grant.Token != "" {
		t.Fatalf("generate must not unlock mid-batch: name=%q grant=%#v", name, grant)
	}
	committed := append(append([]agent.ConversationEntry(nil), delta...), agent.ConversationEntry{
		Role: "tool", Content: "张惠妹专辑与代表作列表", ToolCallID: "call-search", ToolName: "web_search",
	})
	_ = cb.OnToolBatchCommitted(committed, agent.ToolBatchMetadata{Sequence: 1, LastToolName: "web_search"})
	if name, grant := soleLiveSemanticGrantByAdapter(cb.semanticSurface, "generate_pdf"); name == "" || grant.Token == "" {
		t.Fatalf("generate must issue after one committed search: grants=%#v completed=%#v", cb.semanticSurface.grants, cb.semanticSurface.completed)
	}
	if granted, message := cb.PetitionToolCall("generate_pdf"); granted || message != "" {
		t.Fatalf("listed generate_pdf must not be a petition: granted=%v message=%q", granted, message)
	}
}

func TestSearchPDFEarlySendFileNamesLookupThenGenerate(t *testing.T) {
	cb, _ := searchPDFDesktopBeforeSearch(t)
	granted, message := cb.PetitionToolCall("send_file")
	if granted || !strings.Contains(message, `Call "web_search" now`) || !strings.Contains(message, `"generate_pdf" appears on the next request`) || !strings.Contains(message, `Call "send_file" only after "send_file" is listed`) || strings.Contains(message, `"send_file" appears`) {
		t.Fatalf("real-plan send_file denial granted=%v message=%q", granted, message)
	}
}

// Production 2026-10-02: 「继续完成 兰州天气」 planned only live_data. Petitioning
// generate_pdf added the PDF leg, which still waits on this turn's required
// web_search. The host used to answer with the grant acknowledgement anyway,
// so the model re-called a name the next request did not list.
func TestLiveDataPDFPetitionWaitsForLookupWithoutClaimingGrant(t *testing.T) {
	cb, searchName := liveDataDesktopBeforePDFPetition(t)
	if _, grant := soleLiveSemanticGrantByAdapter(cb.semanticSurface, "generate_pdf"); grant.Token != "" {
		t.Fatal("live_data surface must not list generate_pdf before the petition")
	}
	granted, message := cb.PetitionToolCall("generate_pdf")
	if granted || strings.Contains(message, "已由主机授权") {
		t.Fatalf("withheld generate must not be described as granted: granted=%v message=%q", granted, message)
	}
	if !successorNotYetListedDenial(message, "generate_pdf") || !strings.Contains(message, `"web_search"`) {
		t.Fatalf("petition must name the lookup that unlocks generate: %q", message)
	}
	if !cb.semanticEffectfulPetitionConsumed {
		t.Fatal("the expansion still happened and must spend the effectful budget")
	}
	if !planHasCapabilities(cb.semanticSurface.plan, "information.search.web", "document.generate.file") {
		t.Fatalf("expansion must keep search and add generate: %#v", cb.semanticSurface.plan.Selections)
	}
	if name, grant := soleLiveSemanticGrantByAdapter(cb.semanticSurface, "generate_pdf"); name != "" || grant.Token != "" {
		t.Fatalf("generate must stay unissued until lookup commits: name=%q grant=%#v", name, grant)
	}

	delta := []agent.ConversationEntry{{
		Role: "assistant", ToolCalls: []map[string]string{{"id": "call-search", "name": searchName}},
	}}
	_ = cb.OnToolBatchStarting(delta, agent.ToolBatchMetadata{Sequence: 1, LastToolName: searchName})
	if search := cb.ExecuteToolCall(searchName, `{"query":"兰州天气"}`, "call-search").Result; strings.Contains(search, "[system rejected]") {
		t.Fatalf("search result=%q", search)
	}
	if name, grant := soleLiveSemanticGrantByAdapter(cb.semanticSurface, "generate_pdf"); name != "" || grant.Token != "" {
		t.Fatalf("generate must not unlock mid-batch: name=%q grant=%#v", name, grant)
	}
	committed := append(append([]agent.ConversationEntry(nil), delta...), agent.ConversationEntry{
		Role: "tool", Content: "兰州：多云，24°C / 11°C", ToolCallID: "call-search", ToolName: searchName,
	})
	if err := cb.OnToolBatchCommitted(committed, agent.ToolBatchMetadata{Sequence: 1, LastToolName: searchName}); err != nil {
		t.Fatal(err)
	}
	if name, grant := soleLiveSemanticGrantByAdapter(cb.semanticSurface, "generate_pdf"); name == "" || grant.Token == "" {
		t.Fatalf("one committed search must issue the petitioned generate grant: grants=%#v", cb.semanticSurface.grants)
	}
}

func TestSearchDocumentGeneratePetitionBeforeLookupDoesNotSpendBudget(t *testing.T) {
	cb, _ := searchPDFDesktopBeforeSearch(t)
	granted, message := cb.PetitionToolCall("generate_pdf")
	if granted || !successorNotYetListedDenial(message, "generate_pdf") || !strings.Contains(message, `"web_search"`) {
		t.Fatalf("generate must stay gated until lookup and name web_search: granted=%v message=%q", granted, message)
	}
	if cb.semanticEffectfulPetitionConsumed {
		t.Fatal("already-planned generate must not spend the effectful petition budget")
	}
}

// The model often calls generate_pdf as soon as search returns, before the
// next request's tool list is rebuilt. That used to hit "petition expansion
// added no governed need" because generate was already in the plan. Releasing
// the same-batch hold must let the petition issue the existing selection
// without burning the effectful rescue budget.
func TestSearchDocumentGeneratePetitionExposesAlreadyPlannedPDF(t *testing.T) {
	cb, searchName := searchPDFDesktopBeforeSearch(t)
	delta := []agent.ConversationEntry{{
		Role: "assistant", ToolCalls: []map[string]string{{"id": "call-search", "name": "web_search"}},
	}}
	_ = cb.OnToolBatchStarting(delta, agent.ToolBatchMetadata{Sequence: 1, LastToolName: "web_search"})
	if search := cb.ExecuteToolCall(searchName, `{"query":"张惠妹歌曲列表"}`, "call-search").Result; strings.Contains(search, "[system rejected]") {
		t.Fatalf("search result=%q", search)
	}
	if granted, message := cb.PetitionToolCall("generate_pdf"); granted || !successorHeldForNextRequest(message, "generate_pdf") {
		t.Fatalf("hold after search must keep generate unissued until the next request: granted=%v message=%q", granted, message)
	}
	if cb.semanticEffectfulPetitionConsumed {
		t.Fatal("held already-planned generate must not spend the effectful petition budget")
	}
	cb.hasPendingToolBatch = false
	cb.semanticHoldDependantIssue = false
	granted, message := cb.PetitionToolCall("generate_pdf")
	if !granted || !strings.Contains(message, "generate_pdf") {
		t.Fatalf("already-planned generate must be exposed: granted=%v message=%q grants=%#v", granted, message, cb.semanticSurface.grants)
	}
	if cb.semanticEffectfulPetitionConsumed {
		t.Fatal("exposing an already-planned generate must not spend the effectful petition budget")
	}
	if name, grant := soleLiveSemanticGrantByAdapter(cb.semanticSurface, "generate_pdf"); name == "" || grant.Token == "" {
		t.Fatalf("petition must issue generate: grants=%#v", cb.semanticSurface.grants)
	}
}

func TestHostAutoGeneratePDFDoesNotProjectPendingBatch(t *testing.T) {
	cb, searchName := weatherPDFDesktopBeforeSearch(t)
	cb.semanticHoldDependantIssue = true
	cb.hasPendingToolBatch = true
	if search := cb.ExecuteToolCall(searchName, `{"query":"南京天气"}`, "call-search").Result; strings.Contains(search, "[system rejected]") {
		t.Fatalf("search result=%q", search)
	}
	if !cb.semanticNeedDependantIssue {
		t.Fatal("search must leave its generate dependant held while the batch is pending")
	}

	resp := &IMAgentResponse{Text: "已查询到南京天气"}
	attachSharedLoopArtifacts(resp, cb)

	if resp.FileMimeType != "" || resp.LocalFilePath != "" || resp.FileData != "" {
		t.Fatalf("pending batch must not host-project a PDF: %+v", resp)
	}
	if name, grant := soleLiveSemanticGrantByAdapter(cb.semanticSurface, "generate_pdf"); name != "" || grant.Token != "" {
		t.Fatalf("pending batch must not materialize generate: name=%q grant=%#v", name, grant)
	}
	if !cb.semanticHoldDependantIssue || !cb.semanticNeedDependantIssue {
		t.Fatalf("pending batch released dependant state: hold=%v need=%v", cb.semanticHoldDependantIssue, cb.semanticNeedDependantIssue)
	}
}

func TestHostAutoGeneratePDFDoesNotProjectAfterDurabilityFailure(t *testing.T) {
	cb := weatherPDFDesktopReadyAfterSearch(t)
	name, grant := soleLiveSemanticGrantByAdapter(cb.semanticSurface, "generate_pdf")
	if name == "" || grant.Token == "" {
		t.Fatal("generate_pdf grant missing after search")
	}
	cb.semanticDurabilityBlocked = true

	resp := &IMAgentResponse{Text: "已查询到南京天气"}
	attachSharedLoopArtifacts(resp, cb)

	if resp.FileMimeType != "" || resp.LocalFilePath != "" || resp.FileData != "" {
		t.Fatalf("durability failure must not host-project a PDF: %+v", resp)
	}
	if _, ok := cb.semanticSurface.grants[name]; !ok {
		t.Fatal("durability failure must not consume an already-issued generate grant")
	}
}

// TestIMSemanticWeatherPDFRunLoopPublishesDependentGrantAfterCommittedBatch
// exercises the production path end to end: the core RunLoop sends actual
// OpenAI-compatible requests while the shared callback owns semantic grants.
// A successful lookup may only publish the dependent PDF grant after the
// assistant tool-call batch is complete, but that published grant must be in
// the very next model request.  Keeping the assertion at the HTTP boundary
// prevents a callback-local surface from masking a stale RunLoop tool slice.
func TestIMSemanticWeatherPDFRunLoopPublishesDependentGrantAfterCommittedBatch(t *testing.T) {
	cb, searchName := weatherPDFDesktopBeforeSearch(t)
	if err := cb.syncSemanticToolSurface(); err != nil {
		t.Fatalf("sync initial semantic surface: %v", err)
	}

	request := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var payload struct {
			Tools []map[string]interface{} `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode model request: %v", err)
		}
		request++
		adapters := make(map[string]bool, len(payload.Tools))
		for _, definition := range payload.Tools {
			name := extractToolName(definition)
			if name == semanticToolsSearchName {
				// Grant-less discovery meta-tool: always rendered on a
				// governed surface, never a live semantic grant.
				continue
			}
			grant, ok := cb.semanticSurface.grants[name]
			if !ok {
				t.Fatalf("request %d exposed non-live semantic tool %q", request, name)
			}
			adapters[grant.AdapterName] = true
		}

		var response map[string]interface{}
		switch request {
		case 1:
			// The retrieval bundle's optional web_fetch offer rides along;
			// the phased legs (generate, deliver) must not be exposed yet.
			if !adapters["semantic_search_trusted_web"] || adapters["generate_pdf"] || adapters["semantic_deliver_current_file"] {
				t.Fatalf("first request adapters=%v, want trusted web search without the phased legs", adapters)
			}
			response = semanticLoopToolCallResponse("call-search", searchName, `{"query":"南京天气"}`)
		case 2:
			// generate must be unlocked and deliver must wait. web_search may
			// stay advertised: the family's optional ceiling siblings keep the
			// stable name live until spent, exactly as on a declared search
			// turn (§4.2 max-budget rule).
			if !adapters["generate_pdf"] || adapters["semantic_deliver_current_file"] {
				t.Fatalf("second request adapters=%v, want generate_pdf unlocked", adapters)
			}
			generateName, _ := soleLiveSemanticGrantByAdapter(cb.semanticSurface, "generate_pdf")
			if generateName == "" {
				t.Fatal("generate_pdf was advertised without a live grant")
			}
			response = semanticLoopToolCallResponse("call-generate", generateName, `{"content":"# 南京天气\nNanjing weather: cloudy, 26C","title":"南京天气"}`)
		case 3:
			if !adapters["semantic_deliver_current_file"] || adapters["generate_pdf"] {
				t.Fatalf("third request adapters=%v, want current-channel delivery unlocked", adapters)
			}
			response = map[string]interface{}{"choices": []map[string]interface{}{{
				"message":       map[string]interface{}{"role": "assistant", "content": "PDF report generated."},
				"finish_reason": "stop",
			}}}
		default:
			t.Fatalf("unexpected model request %d", request)
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(response); err != nil {
			t.Fatalf("encode model response: %v", err)
		}
	}))
	defer server.Close()

	cb.llmCfg = corelib.MaclawLLMConfig{URL: server.URL, Model: "semantic-loop-test", Key: "test-key"}
	cb.maxIter = 4
	cb.systemPrompt = "test semantic loop"
	result := agent.RunLoop(cb, cb.userText, nil, server.Client(), cb)
	if result.Error != "" || result.Text != "PDF report generated." {
		t.Fatalf("RunLoop result=%+v", result)
	}
	if request != 3 {
		t.Fatalf("model requests=%d, want 3", request)
	}
}

func semanticLoopToolCallResponse(id, name, arguments string) map[string]interface{} {
	return map[string]interface{}{"choices": []map[string]interface{}{{
		"message": map[string]interface{}{
			"role":    "assistant",
			"content": "",
			"tool_calls": []map[string]interface{}{{
				"id":       id,
				"type":     "function",
				"function": map[string]interface{}{"name": name, "arguments": arguments},
			}},
		},
		"finish_reason": "tool_calls",
	}}}
}

func TestHostAutoGeneratePDFRetriesDependantIssueAfterFailedRefresh(t *testing.T) {
	cb, searchName := weatherPDFDesktopBeforeSearch(t)
	cb.semanticHoldDependantIssue = true
	if search := cb.ExecuteToolCall(searchName, `{"query":"南京天气"}`, "call-search").Result; strings.Contains(search, "[system rejected]") {
		t.Fatalf("search result=%q", search)
	}
	registry := cb.semanticSurface.registry
	cb.semanticSurface.registry = nil
	cb.releaseSemanticDependantIssue()
	if !cb.semanticNeedDependantIssue {
		t.Fatal("a failed delayed issue must stay pending for host flush")
	}
	if name, grant := soleLiveSemanticGrantByAdapter(cb.semanticSurface, "generate_pdf"); name != "" || grant.Token != "" {
		t.Fatalf("failed refresh must not mint generate: name=%q", name)
	}
	cb.semanticSurface.registry = registry
	resp := &IMAgentResponse{Text: "已查询到南京天气"}
	attachSharedLoopArtifacts(resp, cb)
	if resp.FileMimeType != "application/pdf" || resp.LocalFilePath == "" {
		t.Fatalf("host flush must retry the delayed issue: %+v", resp)
	}
}

func TestHostAutoGeneratePDFSkipsInvalidEvidenceWithoutBurningGrant(t *testing.T) {
	cb := weatherPDFDesktopReadyAfterSearch(t)
	name, grant := soleLiveSemanticGrantByAdapter(cb.semanticSurface, "generate_pdf")
	if name == "" || grant.Token == "" {
		t.Fatal("generate_pdf grant missing after search")
	}
	cb.semanticLookupEvidence = "[file_base64|x|application/pdf]AAAA"
	resp := &IMAgentResponse{Text: "请稍候"}
	attachSharedLoopArtifacts(resp, cb)
	if _, ok := cb.semanticSurface.grants[name]; !ok {
		t.Fatal("invalid evidence must not consume generate_pdf")
	}
	if resp.LocalFilePath != "" || resp.FileData != "" {
		t.Fatalf("must not attach a PDF from invalid evidence: %+v", resp)
	}
}

func TestHostOwnedPDFReportHelpers(t *testing.T) {
	if got := hostOwnedPDFReportTitle("查询南京天气，并生成pdf报告"); got != "查询南京天气" {
		t.Fatalf("title=%q", got)
	}
	if got := hostOwnedPDFReportTitle("杭州天气，生成pdf报告"); got != "杭州天气" {
		t.Fatalf("title=%q", got)
	}
	if got := hostOwnedPDFReportTitle("生成pdf报告"); got != "生成pdf报告" {
		t.Fatalf("bare instruction title=%q", got)
	}
	if got := hostOwnedPDFReportTitle("Hangzhou weather, generate a PDF report"); got != "Hangzhou weather" {
		t.Fatalf("english title=%q", got)
	}
	if got := hostOwnedPDFReportTitle("杭州天气，生成一份pdf报告"); got != "杭州天气" {
		t.Fatalf("一份 title=%q", got)
	}
	if got := hostOwnedPDFReportTitle("杭州天气，请帮我生成PDF报告"); got != "杭州天气" {
		t.Fatalf("请帮我 title=%q", got)
	}
	picker := "杭州天气，生成pdf报告\n\n" + filePathPromptPrefix + "\nC:\\tmp\\scan.jpg\n[Host note: selected image \"scan.jpg\" could not be read: missing]"
	if got := hostOwnedPDFReportTitle(picker); got != "杭州天气" {
		t.Fatalf("picker/host notes leaked into title: %q", got)
	}
	kept := agentruntime.ProjectHostPublishedPDFChat("请稍候～\n今天多云。")
	if !strings.Contains(kept, "今天多云") || !strings.Contains(kept, "请稍候") {
		t.Fatalf("the answer was rewritten: %q", kept)
	}
	excuse := agentruntime.OmitHostPDFToolStatus("PDF生成失败（参数无效）\n当前轮次未授权 `generate_pdf` 工具，无法生成PDF。天气数据已获取：南京小雨31℃。")
	if strings.Contains(excuse, "generate_pdf") || strings.Contains(excuse, "未授权") {
		t.Fatalf("tool-status sentence survived: %q", excuse)
	}
	if !strings.Contains(excuse, "南京小雨31℃") || !strings.Contains(excuse, "PDF生成失败") {
		t.Fatalf("sentences that do not cite the tool id must stay: %q", excuse)
	}
	if kept := agentruntime.OmitHostPDFToolStatus("午后大风无法直接生成有效对流。"); !strings.Contains(kept, "无法直接生成") {
		t.Fatalf("report sentence without the tool id was dropped: %q", kept)
	}
	xmlFail := agentruntime.OmitHostPDFToolStatus("模型返回了无法解析的工具调用，已拦截原始工具 XML。请重试，或切换更兼容 OpenAI tool_calls 的模型。")
	if xmlFail != "" {
		t.Fatalf("host malformed-tool notice survived: %q", xmlFail)
	}
	if !strings.Contains(agentruntime.OmitHostPDFToolStatus("午后请稍候再出门。"), "午后请稍候再出门") {
		t.Fatal("report sentence was dropped because of its wording")
	}
	if !shouldClearStaleErrorAfterHostFileAttach("LLM call failed: timeout") || shouldClearStaleErrorAfterHostFileAttach("cancelled") {
		t.Fatal("stale-error helper")
	}
	if shouldClearStaleErrorAfterHostFileAttach("semantic_capability_unmet") || shouldClearStaleErrorAfterHostFileAttach("[system rejected] x") {
		t.Fatal("policy errors must not be cleared")
	}
	promise := "今天多云，26℃。\n接下来我将为这份南京天气生成 PDF 报告，请稍候～\n午后请稍候再出门。"
	if got := agentruntime.ProjectHostPublishedPDFChat(promise); !strings.Contains(got, "26℃") || !strings.Contains(got, "午后请稍候再出门") || !strings.Contains(got, "接下来我将") {
		t.Fatalf("the answer was rewritten: %q", got)
	}
	keptPromise := agentruntime.OmitHostPDFToolStatus(promise)
	if !strings.Contains(keptPromise, "请稍候～") || !strings.Contains(keptPromise, "午后请稍候再出门") {
		t.Fatalf("wording alone must not drop a report or a promise: %q", keptPromise)
	}
	forecast := agentruntime.OmitHostPDFToolStatus("接下来我将为您生成未来三天天气报告。\n今天多云。")
	if !strings.Contains(forecast, "接下来我将为您生成未来三天天气报告") {
		t.Fatalf("report sentence was treated as tool status: %q", forecast)
	}
	english := agentruntime.OmitHostPDFToolStatus("Sunny, 26C. I will generate a PDF report, please wait.")
	if !strings.Contains(english, "Sunny, 26C") || !strings.Contains(strings.ToLower(english), "please wait") {
		t.Fatalf("english promise without the tool id was dropped: %q", english)
	}
	cb := &sharedAgentLoopCallbacks{semanticLookupEvidence: "Nanjing weather: cloudy, 26C"}
	cb.recordSemanticLookupEvidence(tool.PlannedSelection{FitProof: tool.FitProof{MatchedCapability: "information.search.web"}}, "[file_base64|x|application/pdf]AAAA")
	if cb.semanticLookupEvidence != "Nanjing weather: cloudy, 26C" {
		t.Fatalf("invalid search payload must not overwrite evidence: %q", cb.semanticLookupEvidence)
	}
	if shouldClearStaleErrorAfterHostFileAttach("recovery_checkpoint_failed") || shouldClearStaleErrorAfterHostFileAttach("Shared agent loop panicked: x") {
		t.Fatal("checkpoint/panic errors must stay visible")
	}
	if !shouldClearStaleErrorAfterHostFileAttach("LLM call failed: timeout") {
		t.Fatal("timeout must still clear")
	}
	if hostOwnedGeneratePDFSucceeded("未找到可用的中文字体，无法生成 PDF。") || hostOwnedGeneratePDFSucceeded("[system rejected] parameter_schema_invalid") {
		t.Fatal("adapter failure must not count as generate success")
	}
	if hostOwnedGeneratePDFSucceeded("failed: PDF artifact published was not created") {
		t.Fatal("success must be the result prefix, not a substring")
	}
	if !hostOwnedGeneratePDFSucceeded("PDF artifact published; deliver it through the current-channel file adapter.") {
		t.Fatal("published artifact must count as generate success")
	}
	if hostOwnedPDFReportContent("ok", "[file_base64|x|application/pdf]AAAA", "t") != "" {
		t.Fatal("file payload markers must not become PDF content")
	}
	if hostLookupEvidenceUntrusted("杭州晴，28℃。See also: tool_call\n documentation.") {
		t.Fatal("prose mentioning tool_call must still be trusted lookup evidence")
	}
	longAssistant := strings.Repeat("今天多云二十六度。", 12)
	preferred := hostOwnedPDFReportContent(longAssistant, "杭州晴，28℃。", "杭州天气")
	if !strings.Contains(preferred, "杭州晴，28℃") || strings.Contains(preferred, "今天多云二十六度") {
		t.Fatalf("trusted lookup evidence must win over assistant prose: %q", preferred)
	}
	dsmlAssistant := longAssistant + "\n<｜DSML｜tool_calls><｜DSML｜invoke name=\"web_search\"></｜DSML｜invoke></｜DSML｜tool_calls>"
	fallback := hostOwnedPDFReportContent(dsmlAssistant, "[system rejected] parameter_schema_invalid", "杭州天气")
	if fallback == "" || strings.Contains(fallback, "DSML") || strings.Contains(fallback, "web_search") {
		t.Fatalf("assistant fallback must strip tool markup after rejected evidence: %q", fallback)
	}
}

func TestHostFileAttachReplacesMalformedToolXML(t *testing.T) {
	cb, _ := weatherPDFDesktopReadyForHostDeliver(t)
	resp := &IMAgentResponse{Text: "模型返回了无法解析的工具调用，已拦截原始工具 XML。请重试，或切换更兼容 OpenAI tool_calls 的模型。"}
	attachSharedLoopArtifacts(resp, cb)
	if resp.FileMimeType != "application/pdf" || resp.LocalFilePath == "" {
		t.Fatalf("must still attach PDF: %+v", resp)
	}
	if strings.Contains(resp.Text, "无法解析") || strings.Contains(resp.Text, "XML") {
		t.Fatalf("malformed XML intercept survived attach: %q", resp.Text)
	}
}

// TestIMSemanticWeatherPDFDesktopDeliversWithoutInboundTarget reproduces the
// live desktop chat: production never attaches DeliveryTarget, so current-channel
// file deliver used to fail with trusted_delivery_target_missing after a
// successful PDF. Host-owned desktop binding must let the PDF attach.
func TestIMSemanticWeatherPDFDesktopDeliversWithoutInboundTarget(t *testing.T) {
	cb, _ := weatherPDFDesktopReadyForHostDeliver(t)
	// Live desktop turns stop after generate_pdf and never call the follow-up
	// grant. Host-owned current-channel delivery must still attach the PDF.
	resp := &IMAgentResponse{}
	attachSharedLoopArtifacts(resp, cb)
	if resp.FileMimeType != "application/pdf" || resp.LocalFilePath == "" {
		t.Fatalf("desktop chat must receive a local PDF path: %+v", resp)
	}
	if resp.FileData != "" {
		t.Fatalf("desktop Wails event must not carry inline FileData after materialize: %+v", resp)
	}
}

func TestHostAutoCurrentChannelFileDeliverDoesNotBurnGrantWithoutTarget(t *testing.T) {
	cb, deliveryName := weatherPDFDesktopReadyForHostDeliver(t)
	cb.loopCtx.DeliveryTarget = nil
	resp := &IMAgentResponse{}
	attachSharedLoopArtifacts(resp, cb)
	if _, ok := cb.semanticSurface.grants[deliveryName]; !ok {
		t.Fatal("missing target must not consume the one-shot deliver grant")
	}
	if resp.LocalFilePath != "" || resp.FileData != "" {
		t.Fatalf("must not deliver without a trusted target: %+v", resp)
	}
}

func TestHostAutoCurrentChannelFileDeliverDoesNotBurnGrantOnChannelMismatch(t *testing.T) {
	cb, deliveryName := weatherPDFDesktopReadyForHostDeliver(t)
	cb.loopCtx.DeliveryTarget = &agent.DeliveryTarget{ChannelScope: "weixin", DestinationID: "user:wx-1"}
	resp := &IMAgentResponse{}
	attachSharedLoopArtifacts(resp, cb)
	if _, ok := cb.semanticSurface.grants[deliveryName]; !ok {
		t.Fatal("channel mismatch must not consume the one-shot deliver grant")
	}
	if resp.LocalFilePath != "" || resp.FileData != "" {
		t.Fatalf("must not deliver across channels: %+v", resp)
	}
}

func TestHostAutoCurrentChannelFileDeliverSkippedOnPolicyError(t *testing.T) {
	cb, deliveryName := weatherPDFDesktopReadyForHostDeliver(t)
	resp := &IMAgentResponse{Error: "semantic_capability_unmet"}
	attachSharedLoopArtifacts(resp, cb)
	if _, ok := cb.semanticSurface.grants[deliveryName]; !ok {
		t.Fatal("policy errors must not consume the one-shot deliver grant")
	}
	if resp.LocalFilePath != "" || resp.FileData != "" {
		t.Fatalf("must not auto-deliver after a policy error: %+v", resp)
	}
}

func TestHostAutoCurrentChannelFileDeliverSkippedOnInteractivePause(t *testing.T) {
	cb, deliveryName := weatherPDFDesktopReadyForHostDeliver(t)
	cb.skipHostAutoFileDelivery = true
	resp := &IMAgentResponse{}
	attachSharedLoopArtifacts(resp, cb)
	if _, ok := cb.semanticSurface.grants[deliveryName]; !ok {
		t.Fatal("interactive pause must not consume the deliver grant")
	}
	if resp.LocalFilePath != "" || resp.FileData != "" {
		t.Fatalf("must not auto-deliver during ask_user/record_audio: %+v", resp)
	}
}

func searchPDFDesktopBeforeSearch(t *testing.T) (*sharedAgentLoopCallbacks, string) {
	t.Helper()
	return documentGenerateDesktopBeforeSearch(t, "全网搜索张惠妹歌曲列表，生成详细pdf版本清单", searchGenerateClassification())
}

func weatherPDFDesktopBeforeSearch(t *testing.T) (*sharedAgentLoopCallbacks, string) {
	t.Helper()
	return documentGenerateDesktopBeforeSearch(t, "南京天气，生成pdf报告", liveDataGenerateClassification())
}

func liveDataDesktopBeforePDFPetition(t *testing.T) (*sharedAgentLoopCallbacks, string) {
	t.Helper()
	id := strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r == ' ' {
			return '_'
		}
		return r
	}, t.Name())
	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(app.closeSemanticInvocationStore)
	h := registerDocumentGenerateAndSearch(t)
	h.app = app
	h.semanticTrustedWebSearch = func(userID, query string) (string, error) {
		return "兰州：多云，24°C / 11°C", nil
	}
	const userText = "继续完成 兰州天气"
	classification := &intent.ClassificationResult{Primary: intent.LabelLiveData, Confidence: .85, Layer: 3}
	loopCtx := h.prepareIMLoopContext(nil, IMUserMessage{
		UserID: "user-1", Platform: "desktop", Text: userText,
	}, nil, false, false)
	requestCtx, cancel := semanticRoutingContext(loopCtx)
	t.Cleanup(cancel)
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithContextAndIdentityAndClassificationAndAttachments(
		requestCtx, "user-1", userText, "desktop", "root-"+id, "turn-"+id, classification, nil,
	)
	if err != nil || !handled || surface == nil || len(defs) == 0 {
		t.Fatalf("defs=%#v handled=%v err=%v", defs, handled, err)
	}
	if !planHasCapabilities(surface.plan, "information.search.web") || planHasCapabilities(surface.plan, "document.generate.file") {
		t.Fatalf("live_data continuation must plan search without generate: %#v", surface.plan.Selections)
	}
	cb := &sharedAgentLoopCallbacks{
		handler: h, semanticSurface: surface, platform: "desktop", loopCtx: loopCtx, userText: userText,
	}
	searchName := semanticGrantNameForAdapter(surface, semanticTrustedWebSearchAdapter)
	if searchName == "" {
		t.Fatalf("initial surface lost the search grant: %#v", surface.grants)
	}
	return cb, searchName
}

func documentGenerateDesktopBeforeSearch(t *testing.T, userText string, classification *intent.ClassificationResult) (*sharedAgentLoopCallbacks, string) {
	t.Helper()
	id := strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r == ' ' {
			return '_'
		}
		return r
	}, t.Name())
	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(app.closeSemanticInvocationStore)
	h := registerDocumentGenerateAndSearch(t)
	h.app = app
	h.semanticTrustedWebSearch = func(userID, query string) (string, error) {
		return "Nanjing weather: cloudy, 26C", nil
	}
	loopCtx := h.prepareIMLoopContext(nil, IMUserMessage{
		UserID: "user-1", Platform: "desktop", Text: userText,
	}, nil, false, false)
	if loopCtx.DeliveryTarget == nil || loopCtx.DeliveryTarget.ChannelScope != "desktop" || loopCtx.DeliveryTarget.DestinationID != "user:user-1" {
		t.Fatalf("desktop DeliveryTarget = %+v", loopCtx.DeliveryTarget)
	}
	requestCtx, cancel := semanticRoutingContext(loopCtx)
	t.Cleanup(cancel)
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithContextAndIdentityAndClassificationAndAttachments(
		requestCtx, "user-1", userText, "desktop", "root-"+id, "turn-"+id, classification, nil,
	)
	if err != nil || !handled || surface == nil || len(defs) == 0 {
		t.Fatalf("defs=%#v handled=%v err=%v", defs, handled, err)
	}
	if !planHasCapabilities(surface.plan, "information.search.web", "document.generate.file", "artifact.deliver.current_channel") {
		t.Fatalf("selections=%#v", surface.plan.Selections)
	}
	for _, selection := range surface.plan.Selections {
		switch selection.FitProof.MatchedCapability {
		case "information.search.web", "document.generate.file", "artifact.deliver.current_channel", tool.CapabilityInformationFetchWeb,
			tool.CapabilityArtifactAcquireRemote, tool.CapabilityFSReadLocal, tool.CapabilityFSWriteLocal, tool.CapabilityShellExecuteLocal:
			// The retrieval archetype bundle adds the optional web_fetch
			// offers; the lookup+generate composite is a document-archetype
			// turn, so the document bundle's acquire/read offers ride along
			// too (2026-08-28 PPT turn: petition-only download starved the
			// effectful petition budget). Baseline workspace tools (file
			// write, local shell) stay listed as last-resort fallbacks.
		default:
			t.Fatalf("host-owned desktop dest must not add extra selections: %#v", surface.plan.Selections)
		}
	}
	cb := &sharedAgentLoopCallbacks{
		handler: h, semanticSurface: surface, platform: "desktop", loopCtx: loopCtx,
		userText: userText,
	}
	searchName := semanticGrantNameForAdapter(surface, semanticTrustedWebSearchAdapter)
	if searchName == "" {
		t.Fatalf("initial surface lost the search grant: %#v", surface.grants)
	}
	return cb, searchName
}

func weatherPDFDesktopReadyAfterSearch(t *testing.T) *sharedAgentLoopCallbacks {
	t.Helper()
	cb, searchName := weatherPDFDesktopBeforeSearch(t)
	if search := cb.ExecuteToolCall(searchName, `{"query":"南京天气"}`, "call-search").Result; strings.Contains(search, "[system rejected]") {
		t.Fatalf("search result=%q", search)
	}
	if name, grant := soleLiveSemanticGrantByAdapter(cb.semanticSurface, "generate_pdf"); name == "" || grant.Token == "" {
		t.Fatalf("generate grant missing after search: grants=%#v", cb.semanticSurface.grants)
	}
	return cb
}

func weatherPDFDesktopReadyForHostDeliver(t *testing.T) (*sharedAgentLoopCallbacks, string) {
	t.Helper()
	cb := weatherPDFDesktopReadyAfterSearch(t)
	name, grant := soleLiveSemanticGrantByAdapter(cb.semanticSurface, "generate_pdf")
	if name == "" || grant.Token == "" {
		t.Fatalf("generate grant missing after search: grants=%#v", cb.semanticSurface.grants)
	}
	if got := cb.ExecuteToolCall(name, `{"content":"# 南京天气\n18C","title":"南京天气"}`, "call-generate").Result; !strings.Contains(got, "PDF artifact published") {
		t.Fatalf("generate result=%q", got)
	}
	var deliveryName string
	for grantName, item := range cb.semanticSurface.grants {
		if item.AdapterName == "semantic_deliver_current_file" {
			deliveryName = grantName
		}
	}
	if deliveryName == "" {
		t.Fatalf("deliver grant missing after generate: grants=%#v", cb.semanticSurface.grants)
	}
	return cb, deliveryName
}

func TestClassifyIMExecutionProfileLiveDataGenerateIsFull(t *testing.T) {
	profile := classifyIMExecutionProfileWithSemantic(IMUserMessage{Text: "查询南京天气，并生成pdf报告"}, false, false, liveDataGenerateClassification())
	if profile.IsLight() || profile.Layer != string(executionLayerFull) || profile.PromptProfile != "full" {
		t.Fatalf("profile=%+v, want full", profile)
	}
	light := classifyIMExecutionProfileWithSemantic(IMUserMessage{Text: "南京天气"}, false, false, &intent.ClassificationResult{
		Primary: intent.LabelLiveData, Secondary: []intent.IntentLabel{intent.LabelNonCoding}, Confidence: .98,
	})
	if !light.IsLight() {
		t.Fatalf("live_data+non_coding should stay light, got %+v", light)
	}
}

func TestSemanticGrantPromptFenceRejectsHistoryToolNames(t *testing.T) {
	if !strings.Contains(semanticGrantPromptFence, "the live tool list is the ground truth") || !strings.Contains(semanticGrantPromptFence, "web_search") {
		t.Fatalf("fence=%q", semanticGrantPromptFence)
	}
	if !strings.Contains(semanticGrantPromptFence, "previous_turn_tool") || !strings.Contains(semanticGrantPromptFence, "do not tell the user a tool is missing") {
		t.Fatalf("fence omitted staged-grant guidance: %q", semanticGrantPromptFence)
	}
	if !strings.Contains(semanticGrantPromptFence, "this same reply") || !strings.Contains(semanticGrantPromptFence, "please wait") {
		t.Fatalf("fence omitted same-reply generate_pdf guidance: %q", semanticGrantPromptFence)
	}
	if strings.Contains(semanticGrantPromptFence, "完整代理") {
		t.Fatalf("fence leaked workaround copy: %s", semanticGrantPromptFence)
	}
	full := ensureSemanticGrantPromptFence("FULL SYSTEM: use web_search then web_fetch")
	if !strings.Contains(full, "the live tool list is the ground truth") || !strings.Contains(full, "web_search") {
		t.Fatalf("full rebuild lost grant fence: %q", full)
	}
	if got := ensureSemanticGrantPromptFence(full); got != full {
		t.Fatal("grant fence must be idempotent")
	}
}

func TestSemanticHostRejectCopyAvoidsLegacyWorkarounds(t *testing.T) {
	resp := semanticHostRejectResponse()
	if resp == nil || resp.Error != "semantic_capability_unmet" {
		t.Fatalf("resp=%#v", resp)
	}
	for _, blocked := range []string{"完整代理", "设置", "/new", "generate_pdf"} {
		if strings.Contains(resp.Text, blocked) {
			t.Fatalf("host reject mentioned %q: %s", blocked, resp.Text)
		}
	}
}

func registerDocumentGeneratePDF(t *testing.T) *IMMessageHandler {
	t.Helper()
	h := &IMMessageHandler{registry: NewToolRegistry()}
	pdfB64 := base64.StdEncoding.EncodeToString([]byte("%PDF-1.4\n%fake"))
	if err := h.registry.Register(RegisteredTool{
		Name: "generate_pdf", Status: RegToolAvailable,
		InputSchema: map[string]interface{}{
			"content":  map[string]string{"type": "string"},
			"title":    map[string]string{"type": "string"},
			"doc_type": map[string]string{"type": "string"},
			"phase_id": map[string]string{"type": "string"},
		},
		Required: []string{"content"},
		CapabilityProvisions: []tool.CapabilityProvision{{
			Capability: "document.generate.file", Qualifiers: map[string]string{"format": "pdf"}, Quality: 1,
		}},
		SemanticEffects:  []tool.EffectClass{tool.EffectLocalMutation},
		SemanticProduces: []tool.ArtifactContract{{Kind: "document", MIMEType: "application/pdf", Required: true}},
		Handler: func(map[string]interface{}) string {
			return "[file_base64|report.pdf|application/pdf]" + pdfB64
		},
	}); err != nil {
		t.Fatal(err)
	}
	return h
}

func registerDocumentGenerateAndSearch(t *testing.T) *IMMessageHandler {
	t.Helper()
	h := registerDocumentGeneratePDF(t)
	if err := h.registry.Register(RegisteredTool{
		Name: "web_search", Status: RegToolAvailable, InputSchema: map[string]interface{}{},
		CapabilityProvisions: []tool.CapabilityProvision{{Capability: "information.search.web", Qualifiers: map[string]string{"freshness": "current"}, Quality: 1}},
		SemanticEffects:      []tool.EffectClass{tool.EffectReadOnly},
		Handler:              func(map[string]interface{}) string { return "ok" },
	}); err != nil {
		t.Fatal(err)
	}
	return h
}

func planHasCapabilities(plan tool.ToolPlan, capabilities ...tool.CapabilityID) bool {
	found := make(map[tool.CapabilityID]bool, len(capabilities))
	for _, selection := range plan.Selections {
		found[selection.FitProof.MatchedCapability] = true
	}
	for _, capability := range capabilities {
		if !found[capability] {
			return false
		}
	}
	return true
}

// semanticSelectionForCapability finds a planned selection by its matched
// capability regardless of position: the archetype bundle may add optional
// offers that sort ahead of the declared leg a test cares about.
func semanticSelectionForCapability(plan tool.ToolPlan, capability tool.CapabilityID) (tool.PlannedSelection, bool) {
	for _, selection := range plan.Selections {
		if selection.FitProof.MatchedCapability == capability {
			return selection, true
		}
	}
	return tool.PlannedSelection{}, false
}

// semanticDefForGrantName finds a rendered def by its stable function name.
func semanticDefForGrantName(defs []map[string]interface{}, name string) map[string]interface{} {
	for _, def := range defs {
		if extractToolName(def) == name {
			return def
		}
	}
	return nil
}
