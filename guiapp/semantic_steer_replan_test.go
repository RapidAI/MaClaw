package guiapp

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

func TestAcceptSteerEmbeddingComposesPDFOntoPublishedLookup(t *testing.T) {
	current := intent.ClassificationResult{Primary: intent.LabelLiveData, Confidence: 0.88, Layer: 2, Reason: "embedding lookup"}
	embedded := intent.ClassificationResult{
		Primary: intent.LabelDocumentGenerate, Confidence: 0.91, Layer: 2,
		RunnerUp: intent.LabelLiveData, RunnerUpScore: 0.40,
	}
	got, ok := acceptSteerEmbedding(current, embedded)
	if !ok || !got.HasLabel(intent.LabelDocumentGenerate) || got.Primary != intent.LabelLiveData {
		t.Fatalf("accepted=%v verdict=%+v", ok, got)
	}
	if labels := steerPlanLabels(current, got); len(labels) != 1 || labels[0] != intent.LabelDocumentGenerate {
		t.Fatalf("labels=%v", labels)
	}
}

func TestAcceptSteerEmbeddingRefusesUnresolvedPDF(t *testing.T) {
	current := intent.ClassificationResult{Primary: intent.LabelOffice, Confidence: 0.9, Layer: 2}
	weak := intent.ClassificationResult{Primary: intent.LabelDocumentGenerate, Confidence: 0.74, Layer: 2}
	if _, ok := acceptSteerEmbedding(current, weak); ok {
		t.Fatal("PDF on a non-lookup plan must go to the tree")
	}
	lookup := intent.ClassificationResult{Primary: intent.LabelLiveData, Confidence: 0.88, Layer: 2}
	narrow := intent.ClassificationResult{
		Primary: intent.LabelDocumentGenerate, Confidence: 0.90, Layer: 2,
		RunnerUp: intent.LabelLiveDataVisual, RunnerUpScore: 0.86,
	}
	if _, ok := acceptSteerEmbedding(lookup, narrow); ok {
		t.Fatal("a narrow PDF/chart gap must go to the tree")
	}
	degraded := intent.ClassificationResult{Primary: intent.LabelDocumentGenerate, Confidence: 0.95, Degraded: true}
	if _, ok := acceptSteerEmbedding(lookup, degraded); ok {
		t.Fatal("degraded embedding must go to the tree")
	}
	// The published plan is a lookup, but it also carries an unrelated
	// capability and sits under the write floor, so the composed pair cannot
	// plan. That has to stay a tree question.
	mixed := intent.ClassificationResult{
		Primary: intent.LabelLiveData, Secondary: []intent.IntentLabel{intent.LabelOffice},
		Confidence: 0.50, Layer: 2,
	}
	strong := intent.ClassificationResult{
		Primary: intent.LabelDocumentGenerate, Confidence: 0.91, Layer: 2,
		RunnerUp: intent.LabelLiveData, RunnerUpScore: 0.40,
	}
	if _, ok := acceptSteerEmbedding(mixed, strong); ok {
		t.Fatal("a composite the planner would drop must go to the tree")
	}
}

func TestSteerDocumentGenerateSurvivesTreeMiss(t *testing.T) {
	current := intent.ClassificationResult{Primary: intent.LabelLiveData, Confidence: 0.88, Layer: 2, Reason: "embedding lookup"}
	lead := intent.ClassificationResult{
		Primary: intent.LabelDocumentGenerate, Confidence: 0.90, Layer: 2,
		RunnerUp: intent.LabelSearch, RunnerUpScore: 0.84,
	}
	miss := intent.ClassificationResult{Primary: intent.LabelUnknown, Confidence: 0.30, Layer: 2, Degraded: true}
	got, ok := steerDocumentGenerateAfterTreeMiss(current, lead, miss)
	if !ok || !got.HasLabel(intent.LabelDocumentGenerate) || got.Primary != intent.LabelLiveData {
		t.Fatalf("accepted=%v verdict=%+v", ok, got)
	}
	chart := lead
	chart.RunnerUp = intent.LabelLiveDataVisual
	chart.RunnerUpScore = 0.86
	if _, ok := steerDocumentGenerateAfterTreeMiss(current, chart, miss); ok {
		t.Fatal("a chart contest must stay unresolved when the tree misses")
	}
	answered := intent.ClassificationResult{Primary: intent.LabelLiveData, Confidence: 0.80, Layer: 3}
	if _, ok := steerDocumentGenerateAfterTreeMiss(current, lead, answered); ok {
		t.Fatal("a tree that answered must not be overwritten")
	}
	weak := lead
	weak.Confidence = 0.74
	if _, ok := steerDocumentGenerateAfterTreeMiss(current, weak, miss); ok {
		t.Fatal("a below-floor lead must not survive a tree miss")
	}
}

func TestAcceptSteerEmbeddingSkipsTreeForSettledLookup(t *testing.T) {
	current := intent.ClassificationResult{Primary: intent.LabelLiveData, Confidence: 0.88, Layer: 2}
	same := intent.ClassificationResult{
		Primary: intent.LabelLiveData, Confidence: 0.84, Layer: 2,
		RunnerUp: intent.LabelSearch, RunnerUpScore: 0.20, EmbeddingEarlyExit: true,
	}
	got, ok := acceptSteerEmbedding(current, same)
	if !ok {
		t.Fatal("settled lookup steer should not call the tree")
	}
	if labels := steerPlanLabels(current, got); len(labels) != 0 {
		t.Fatalf("labels=%v", labels)
	}
}

func TestAcceptSteerEmbeddingKeepsVerifiedComposite(t *testing.T) {
	current := intent.ClassificationResult{Primary: intent.LabelLiveData, Confidence: 0.88, Layer: 2}
	// NormalizeDeclaredComposite has already put the lookup first. Both
	// halves cleared their floors, so Layer 2 early-exited even though the
	// two scores sit 0.02 apart.
	verified := intent.ClassificationResult{
		Primary: intent.LabelLiveData, Secondary: []intent.IntentLabel{intent.LabelDocumentGenerate},
		Confidence: 0.85, Layer: 2, EmbeddingEarlyExit: true,
		RunnerUp: intent.LabelDocumentGenerate, RunnerUpScore: 0.83,
	}
	got, ok := acceptSteerEmbedding(current, verified)
	if !ok {
		t.Fatal("a verified composite early-exit must not go back to the tree")
	}
	if labels := steerPlanLabels(current, got); len(labels) != 1 || labels[0] != intent.LabelDocumentGenerate {
		t.Fatalf("labels=%v", labels)
	}
}

func TestAcceptSteerEmbeddingRefusesAbstention(t *testing.T) {
	current := intent.ClassificationResult{Primary: intent.LabelLiveData, Confidence: 0.88, Layer: 2}
	// The classifier attaches the artifact half as escalation evidence and
	// returns not-confident. The lookup score must not mint the file.
	ambiguous := intent.ClassificationResult{
		Primary: intent.LabelLiveData, Confidence: 0.88, Layer: 2,
		Secondary: []intent.IntentLabel{intent.LabelDocumentGenerate},
		RunnerUp:  intent.LabelDocumentGenerate, RunnerUpScore: 0.72,
	}
	if _, ok := acceptSteerEmbedding(current, ambiguous); ok {
		t.Fatal("an unresolved composite must go to the tree")
	}
	unsettled := intent.ClassificationResult{Primary: intent.LabelLiveData, Confidence: 0.84, Layer: 2}
	if _, ok := acceptSteerEmbedding(current, unsettled); ok {
		t.Fatal("an embedding that did not early-exit must go to the tree")
	}
}

func TestSteerPlanLabelsAddsMissingDocumentGenerate(t *testing.T) {
	current := intent.ClassificationResult{Primary: intent.LabelLiveData, Confidence: 0.88, Layer: 2}
	verdict := intent.ClassificationResult{
		Primary:    intent.LabelLiveData,
		Secondary:  []intent.IntentLabel{intent.LabelDocumentGenerate},
		Confidence: 0.86,
		Layer:      2,
	}
	labels := steerPlanLabels(current, verdict)
	if len(labels) != 1 || labels[0] != intent.LabelDocumentGenerate {
		t.Fatalf("labels = %v", labels)
	}
}

func TestSteerPlanLabelsRejectsDegradedAndAlreadyDeclared(t *testing.T) {
	current := intent.ClassificationResult{
		Primary:    intent.LabelLiveData,
		Secondary:  []intent.IntentLabel{intent.LabelDocumentGenerate},
		Confidence: 0.9,
		Layer:      2,
	}
	degraded := intent.ClassificationResult{
		Primary: intent.LabelDocumentGenerate, Confidence: 0.95, Layer: 3, Degraded: true,
	}
	if labels := steerPlanLabels(current, degraded); len(labels) != 0 {
		t.Fatalf("degraded steer admitted %v", labels)
	}
	same := intent.ClassificationResult{
		Primary: intent.LabelLiveData, Secondary: []intent.IntentLabel{intent.LabelDocumentGenerate},
		Confidence: 0.9, Layer: 2,
	}
	if labels := steerPlanLabels(current, same); len(labels) != 0 {
		t.Fatalf("already-declared composite admitted %v", labels)
	}
	prose := intent.ClassificationResult{Primary: intent.LabelContinuation, Confidence: 0.95, Layer: 3}
	if labels := steerPlanLabels(current, prose); len(labels) != 0 {
		t.Fatalf("continuation admitted %v", labels)
	}
}

func TestSteerLabelAlreadyPlannedRequiresEveryRequiredLeg(t *testing.T) {
	searchOnly := tool.ToolPlan{Selections: []tool.PlannedSelection{{
		FitProof: tool.FitProof{MatchedCapability: "information.search.web"},
	}}}
	if steerLabelAlreadyPlanned(searchOnly, intent.LabelDocumentGenerate) {
		t.Fatal("search-only plan must not count as a PDF plan")
	}
	full := tool.ToolPlan{Selections: []tool.PlannedSelection{
		{FitProof: tool.FitProof{MatchedCapability: "document.generate.file"}},
		{FitProof: tool.FitProof{MatchedCapability: "artifact.deliver.current_channel"}},
	}}
	if !steerLabelAlreadyPlanned(full, intent.LabelDocumentGenerate) {
		t.Fatal("generate and deliver must count as already planned")
	}
}

func TestLiveSteerAdmitsPDFOntoLookupPlan(t *testing.T) {
	cb, _ := liveDataDesktopBeforePDFPetition(t)
	var classified string
	cb.handler.unifiedClassifier = intent.New(intent.Config{LLMFunc: func(_, message string) (string, error) {
		classified = message
		return `{"top":[{"skill":"document_generate","score":0.98}]}`, nil
	}})
	cb.userID = "user-1"
	cb.userText = "南京天气"
	cb.loopCtx.Runtime.Execution.Layer = string(executionLayerLight)
	cb.loopCtx.Runtime.Execution.PromptProfile = "light"
	cb.loopCtx.Runtime.Execution.TaskType = string(intent.LabelLiveData)
	cb.loopCtx.Runtime.Execution.ToolBudget = 1
	cb.loopCtx.Runtime.Execution.IterationBudget = 3
	cb.loopCtx.Runtime.Execution.RequiredCapabilities = []string{"information.search.web"}
	cb.loopCtx.Runtime.Execution.Reason = "semantic capability-managed lookup"
	cb.systemPrompt = "light fence: 不要生成文档"

	cb.admitLiveSteerIntoPlan("生成pdf")

	if classified != "生成pdf" {
		t.Fatalf("classifier saw %q, want the steer alone", classified)
	}
	if !planHasCapabilities(cb.semanticSurface.plan, "information.search.web", "document.generate.file", "artifact.deliver.current_channel") {
		t.Fatalf("steer did not publish the PDF chain: %#v unmet=%#v", cb.semanticSurface.plan.Selections, cb.semanticSurface.plan.Unmet)
	}
	profile := cb.loopCtx.Runtime.Execution
	if profile.PromptIsLight() || profile.IsLight() {
		t.Fatalf("PDF steer left the lookup fence in place: %+v", profile)
	}
	if profile.Reason != executionReasonPetitionLeftLight {
		t.Fatalf("reason=%q", profile.Reason)
	}
	if len(profile.RequiredCapabilities) != 0 {
		t.Fatalf("search-only capability list still filters the file step: %v", profile.RequiredCapabilities)
	}
	if strings.Contains(cb.systemPrompt, "不要生成文档") || strings.Contains(cb.systemPrompt, "or generate files") {
		t.Fatalf("system prompt still forbids the file the user just asked for: %s", cb.systemPrompt)
	}
	if !strings.Contains(cb.systemPrompt, "when a render or delivery tool is listed") {
		t.Fatalf("system prompt does not tell the model to call the file tool when it appears: %s", cb.systemPrompt)
	}
	if cb.loopCtx.Runtime.SemanticIntent == nil || !cb.loopCtx.Runtime.SemanticIntent.HasLabel(intent.LabelDocumentGenerate) {
		t.Fatalf("turn intent was not updated: %+v", cb.loopCtx.Runtime.SemanticIntent)
	}
	conversation := []interface{}{map[string]string{"role": "system", "content": "light fence: 不要生成文档"}}
	if prompt := cb.PetitionSystemPrompt(); prompt == "" {
		t.Fatal("leaving the light fence must publish a prompt for this request")
	} else {
		replaceConversationSystemMessage(conversation, prompt)
	}
	got, _ := conversation[0].(map[string]string)
	if strings.Contains(got["content"], "不要生成文档") || strings.Contains(got["content"], "or generate files") {
		t.Fatalf("conversation system turn still forbids files: %s", got["content"])
	}
	if !strings.Contains(got["content"], "when a render or delivery tool is listed") {
		t.Fatalf("conversation system turn lost the file-tool contract: %s", got["content"])
	}
}

func TestLiveSteerLookupDoesNotWidenPlan(t *testing.T) {
	cb, _ := liveDataDesktopBeforePDFPetition(t)
	cb.handler.unifiedClassifier = semanticClassifierForLabel(t, intent.LabelLiveData)
	cb.userID = "user-1"
	cb.userText = "南京天气"
	cb.loopCtx.Runtime.Execution.Layer = string(executionLayerLight)
	cb.loopCtx.Runtime.Execution.PromptProfile = "light"
	cb.loopCtx.Runtime.Execution.Reason = "semantic capability-managed lookup"
	cb.systemPrompt = "light fence: 不要生成文档"

	cb.admitLiveSteerIntoPlan("再说具体一点")

	if planHasCapabilities(cb.semanticSurface.plan, "document.generate.file") {
		t.Fatalf("a lookup steer published PDF: %#v", cb.semanticSurface.plan.Selections)
	}
	if !cb.loopCtx.Runtime.Execution.PromptIsLight() {
		t.Fatalf("lookup steer left the light profile: %+v", cb.loopCtx.Runtime.Execution)
	}
	if cb.systemPrompt != "light fence: 不要生成文档" {
		t.Fatalf("lookup steer rewrote the prompt: %q", cb.systemPrompt)
	}
}
