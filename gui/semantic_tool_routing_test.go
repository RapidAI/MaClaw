package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

func semanticTestClassifier(t *testing.T) *intent.UnifiedIntentClassifier {
	t.Helper()
	return intent.New(intent.Config{LLMFunc: func(_, _ string) (string, error) {
		return `{"top":[{"skill":"screenshot","score":0.98}]}`, nil
	}})
}

func semanticClassifierForLabel(t *testing.T, label intent.IntentLabel) *intent.UnifiedIntentClassifier {
	t.Helper()
	return intent.New(intent.Config{LLMFunc: func(_, _ string) (string, error) {
		return `{"top":[{"skill":"` + string(label) + `","score":0.98}]}`, nil
	}})
}

func TestIMSemanticNeedResolverIgnoresLegacyToolNames(t *testing.T) {
	registry := newIMSemanticCapabilityRegistry()
	needs, managed, err := semanticIntentNeedsFromClassification(registry, intent.ClassificationResult{
		Primary: intent.LabelCurrentTime, Confidence: .98,
		// This legacy field is intentionally hostile. It must not select a
		// provider or change the governed need.
		ToolNames: []string{"send_secrets", "call_mcp_tool"},
	})
	if err != nil || !managed || len(needs) != 1 {
		t.Fatalf("needs=%#v managed=%v err=%v", needs, managed, err)
	}
	if needs[0].Capability != "information.current_time" || strings.Contains(needs[0].ID, "mcp") {
		t.Fatalf("legacy tool names influenced semantic need: %#v", needs[0])
	}
}

func TestIMSemanticWebNeedResolverIgnoresLegacyToolNames(t *testing.T) {
	registry := newIMSemanticCapabilityRegistry()
	needs, managed, err := semanticIntentNeedsFromClassification(registry, intent.ClassificationResult{
		Primary: intent.LabelLiveData, Confidence: .98,
		ToolNames: []string{"call_mcp_tool", "send_to_im", "reference_lookup"},
	})
	// The declared search family (1 required invocation + the archetype
	// bundle's 4 optional ceiling siblings, §4.2 max-budget rule) plus the
	// retrieval archetype bundle's 5 optional web_fetch siblings.
	if err != nil || !managed || len(needs) != 10 {
		t.Fatalf("needs=%#v managed=%v err=%v", needs, managed, err)
	}
	if needs[0].Capability != "information.search.web" || needs[0].Qualifiers["freshness"] != "current" || strings.Contains(needs[0].ID, "mcp") {
		t.Fatalf("legacy tool names influenced semantic web need: %#v", needs[0])
	}
}

func TestIMSemanticWebProviderUsesOneCanonicalSchemaForRenderAndExecution(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry()}
	registerBuiltinTools(h.registry, h)
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "北京天气", "desktop", "root-web-schema", "turn-web-schema",
		&intent.ClassificationResult{Primary: intent.LabelLiveData, Confidence: .98},
	)
	if err != nil || !handled || surface == nil || len(defs) < 1 {
		t.Fatalf("defs=%#v handled=%v surface=%#v err=%v", defs, handled, surface, err)
	}
	// The retrieval bundle also offers web_fetch on this face; locate the
	// search def by its stable grant name rather than by position.
	searchName := semanticGrantNameForAdapter(surface, semanticTrustedWebSearchAdapter)
	if searchName == "" {
		t.Fatalf("search grant missing: defs=%#v", defs)
	}
	var definition map[string]interface{}
	for _, def := range defs {
		if extractToolName(def) == searchName {
			definition = def["function"].(map[string]interface{})
		}
	}
	if definition == nil {
		t.Fatalf("search def missing for grant %q: defs=%#v", searchName, defs)
	}
	parameters := definition["parameters"].(map[string]interface{})
	properties := parameters["properties"].(map[string]interface{})
	if _, ok := properties["query"].(map[string]interface{}); !ok || len(properties) != 1 {
		t.Fatalf("rendered query schema type=%T, schema=%#v", properties["query"], parameters)
	}
	if _, exists := properties["max_results"]; exists {
		t.Fatalf("host search schema still exposes max_results: %#v", properties)
	}
	grant := surface.grants[searchName]
	selection, ok := semanticSelectionByID(surface.plan, grant.SelectionID)
	if !ok {
		t.Fatalf("selection not found for grant=%+v", grant)
	}
	if selection.AdapterName != semanticTrustedWebSearchAdapter {
		t.Fatalf("selection=%+v, want host search adapter", selection)
	}
	callback := &sharedAgentLoopCallbacks{semanticSurface: surface}
	if _, err := callback.semanticCanonicalArguments(selection, `{"query":"北京天气","max_results":5}`); err == nil || !strings.Contains(err.Error(), "parameter_unknown_field") && !strings.Contains(err.Error(), "parameter_reserved_field") {
		t.Fatalf("max_results must be rejected err=%v", err)
	}
	request, err := callback.semanticCanonicalArguments(selection, `{"query":"北京天气"}`)
	if err != nil || !strings.Contains(string(request.CanonicalJSON), `"query":"北京天气"`) {
		t.Fatalf("canonical request=%+v err=%v", request, err)
	}
}

func TestSemanticPlanningCancellationFailsClosedWithoutLegacyFallback(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelLiveData)}
	ctx := NewLoopContext("cancelled-semantic-route", 3, nil)
	ctx.Cancel()
	_, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithContext(ctx, "user-1", "北京天气", "desktop")
	if !handled || surface != nil || err == nil || !strings.Contains(err.Error(), "canceled") {
		t.Fatalf("cancelled semantic route handled=%v surface=%#v err=%v", handled, surface, err)
	}
}

func TestSemanticPlanContextCancellationIsHandledBeforeCatalogPublication(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelLiveData)}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	prepared, handled, err := h.semanticPlanForTurnWithContextAndClassificationAndAttachments(
		ctx, "user-1", "北京天气", "desktop", "root-cancelled", "turn-cancelled",
		&intent.ClassificationResult{Primary: intent.LabelLiveData, Confidence: .98}, nil,
	)
	if !handled || prepared != nil || err == nil || !strings.Contains(err.Error(), "canceled") {
		t.Fatalf("cancelled semantic plan prepared=%#v handled=%v err=%v", prepared, handled, err)
	}
}

func TestSemanticCallSurfaceCancellationNeverMaterializesAGrant(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry()}
	registerBuiltinTools(h.registry, h)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithContextAndIdentityAndClassificationAndAttachments(
		ctx, "user-1", "北京天气", "desktop", "root-cancelled-surface", "turn-cancelled-surface",
		&intent.ClassificationResult{Primary: intent.LabelLiveData, Confidence: .98}, nil,
	)
	if !handled || surface != nil || len(defs) != 0 || err == nil || !strings.Contains(err.Error(), "canceled") {
		t.Fatalf("cancelled surface defs=%#v surface=%#v handled=%v err=%v", defs, surface, handled, err)
	}
}

func TestSemanticHostCallConnectionIDIsSurfacePrivate(t *testing.T) {
	cb := &sharedAgentLoopCallbacks{
		semanticSurface: &semanticCallSurface{hostConnectionID: "agent-loop-surface:opaque"},
		loopCtx:         &LoopContext{ID: "public-loop", Runtime: RuntimeContext{RequestID: "public-request"}},
		checkpointRunID: "public-checkpoint",
	}
	if got := cb.semanticHostConnectionID(); got != "agent-loop-surface:opaque" {
		t.Fatalf("connection ID = %q, want surface-private ID", got)
	}

	// A malformed/uninitialized surface must fail closed instead of reviving
	// the former RequestID/LoopID/checkpoint fallbacks as journal authority.
	cb.semanticSurface = &semanticCallSurface{}
	if got := cb.semanticHostConnectionID(); got != "" {
		t.Fatalf("uninitialized surface derived connection ID %q from runtime state", got)
	}
}

func TestSemanticReplanCancellationNeverPublishesChildRevision(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry()}
	registerBuiltinTools(h.registry, h)
	defs, parent, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "北京天气", "desktop", "root-replan-cancelled", "turn-replan-cancelled",
		&intent.ClassificationResult{Primary: intent.LabelLiveData, Confidence: .98},
	)
	if err != nil || !handled || parent == nil || len(defs) < 1 {
		t.Fatalf("parent defs=%#v handled=%v surface=%#v err=%v", defs, handled, parent, err)
	}
	prior, err := parent.routeState.CurrentRevision(parent.scope)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	child, childDefs, err := h.replanSemanticCallSurfaceWithContext(ctx, parent, "dynamic_binding_stale")
	if child != nil || len(childDefs) != 0 || err == nil || !strings.Contains(err.Error(), "canceled") {
		t.Fatalf("cancelled replan child=%#v defs=%#v err=%v", child, childDefs, err)
	}
	current, err := parent.routeState.CurrentRevision(parent.scope)
	if err != nil || current != prior {
		t.Fatalf("cancelled replan changed route current=%+v prior=%+v err=%v", current, prior, err)
	}
}

func TestSemanticReplanSubsetRejectsWidenedAuthority(t *testing.T) {
	parent := tool.ToolPlan{RootTaskID: "root", Selections: []tool.PlannedSelection{{
		ID: "parent", NeedID: "need", Phase: tool.PlanPhaseExecution,
		ParameterAuthorization: tool.ParameterAuthorization{Digest: "params", CanonicalizerVer: "v1"},
		FitProof:               tool.FitProof{MatchedCapability: "information.search.web", QualifierBindings: map[string]string{"freshness": "current"}},
		Effects:                []tool.EffectClass{tool.EffectReadOnly},
	}}}
	child := parent
	child.Selections = append([]tool.PlannedSelection(nil), parent.Selections...)
	child.Selections[0].Effects = []tool.EffectClass{tool.EffectExternalEffect}
	if err := validateSemanticReplanSubset(parent, child); err == nil || !strings.Contains(err.Error(), "expands authority") {
		t.Fatalf("effect-widened child was accepted: %v", err)
	}
	child = parent
	child.Selections = append([]tool.PlannedSelection(nil), parent.Selections...)
	child.Selections[0].ParameterAuthorization.Digest = "wider-params"
	if err := validateSemanticReplanSubset(parent, child); err == nil || !strings.Contains(err.Error(), "expands authority") {
		t.Fatalf("parameter-widened child was accepted: %v", err)
	}
	child = parent
	child.Selections = append(append([]tool.PlannedSelection(nil), parent.Selections...), tool.PlannedSelection{ID: "extra", NeedID: "extra"})
	if err := validateSemanticReplanSubset(parent, child); err == nil || !strings.Contains(err.Error(), "expands authority") {
		t.Fatalf("extra-selection child was accepted: %v", err)
	}
}

func TestSemanticReplanAttemptExhausted(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry()}
	_, _, err := h.replanSemanticCallSurface(&semanticCallSurface{
		replan:     &semanticReplanInput{Attempts: 1, UserID: "user", Channel: "desktop", RootTaskID: "root"},
		routeState: tool.NewMemoryRouteStateStore(),
		issuer:     &tool.InvocationIssuer{},
		executor:   &tool.PlanExecutor{},
		registry:   newIMSemanticCapabilityRegistry(),
	}, "dynamic_binding_stale")
	if err == nil || !strings.Contains(err.Error(), "semantic replan attempt exhausted") {
		t.Fatalf("second child revision must fail closed: %v", err)
	}
}

func TestSemanticReplanBindingReplacementCannotAddParameterAuthority(t *testing.T) {
	parent := tool.ToolPlan{RootTaskID: "root", Selections: []tool.PlannedSelection{{
		ID: "parent", NeedID: "need", Phase: tool.PlanPhaseExecution,
		Provider:               tool.ProviderBinding{Kind: "mcp", ProviderID: "server-a", ImplementationID: "lookup", SchemaDigest: "old"},
		ParameterAuthorization: tool.ParameterAuthorization{Digest: "old-params", CanonicalizerVer: "semantic-parameters-v1", AllowedFields: []string{"query"}},
		FitProof:               tool.FitProof{MatchedCapability: "information.search.web", QualifierBindings: map[string]string{"freshness": "current"}},
		Effects:                []tool.EffectClass{tool.EffectReadOnly},
	}}}
	child := parent
	child.Selections = append([]tool.PlannedSelection(nil), parent.Selections...)
	child.Selections[0].Provider = tool.ProviderBinding{Kind: "builtin", ProviderID: "host", ImplementationID: "fallback", SchemaDigest: "new"}
	child.Selections[0].ParameterAuthorization = tool.ParameterAuthorization{Digest: "new-params", CanonicalizerVer: "semantic-parameters-v1", AllowedFields: []string{"query", "untrusted_target"}}
	if semanticReplanIsBindingOnlyReplacement(parent, child) {
		t.Fatal("binding replacement expanded model parameter authority")
	}
	child.Selections[0].ParameterAuthorization = tool.ParameterAuthorization{Digest: "new-params", CanonicalizerVer: "semantic-parameters-v1", AllowedFields: []string{"query"}}
	if !semanticReplanIsBindingOnlyReplacement(parent, child) {
		t.Fatal("binding replacement with a parameter subset was rejected")
	}
}

func TestSemanticExternalEffectWithoutReceiptBoundaryFailsBeforeLegacyDispatch(t *testing.T) {
	called := false
	selection := tool.PlannedSelection{
		ID: "selection:untracked-external",
		Provider: tool.ProviderBinding{
			Kind: "builtin", ProviderID: "host", ImplementationID: "untracked-external",
		},
		AdapterName: "untracked_external_adapter",
		Effects:     []tool.EffectClass{tool.EffectExternalEffect},
	}
	callback := &sharedAgentLoopCallbacks{
		handler: &IMMessageHandler{registry: NewToolRegistry()},
		semanticSurface: &semanticCallSurface{
			parameterSchemas: map[string]map[string]interface{}{
				selection.AdapterName: {"type": "object", "properties": map[string]interface{}{}, "additionalProperties": false},
			},
		},
	}
	if err := callback.handler.registry.Register(RegisteredTool{
		Name: selection.AdapterName, Status: RegToolAvailable,
		Handler: func(map[string]interface{}) string {
			called = true
			return "legacy handler executed"
		},
	}); err != nil {
		t.Fatal(err)
	}
	result := callback.executeBoundSemanticSelectionCanonical(selection, tool.CanonicalRequest{CanonicalJSON: []byte(`{}`), Values: map[string]interface{}{}})
	if result.Succeeded || result.AwaitingReceipt || result.Unknown || result.ReasonCode != "external_effect_receipt_boundary_missing" {
		t.Fatalf("result=%+v", result)
	}
	if called {
		t.Fatal("untracked external selection reached legacy adapter")
	}
}

func TestIMSemanticNeedResolverGenericNonCodingIsUnmanaged(t *testing.T) {
	registry := newIMSemanticCapabilityRegistry()
	needs, managed, err := semanticIntentNeedsFromClassification(registry, intent.ClassificationResult{
		Primary: intent.LabelNonCoding, Confidence: .78,
	})
	if err != nil || managed || len(needs) != 0 {
		t.Fatalf("generic non_coding must fall through, needs=%#v managed=%v err=%v", needs, managed, err)
	}
}

func TestIMSemanticNeedResolverLiveDataKeepsNeedWhenSecondaryIsNonCoding(t *testing.T) {
	registry := newIMSemanticCapabilityRegistry()
	needs, managed, err := semanticIntentNeedsFromClassification(registry, intent.ClassificationResult{
		Primary: intent.LabelLiveData, Secondary: []intent.IntentLabel{intent.LabelNonCoding}, Confidence: .90,
	})
	// The declared live_data family (1 required invocation + the bundle's 4
	// optional ceiling siblings, §4.2 max-budget rule) plus the retrieval
	// bundle's 5 optional web_fetch siblings.
	if err != nil || !managed || len(needs) != 10 {
		t.Fatalf("generic secondary discarded live_data need: needs=%#v managed=%v err=%v", needs, managed, err)
	}
	if needs[0].Capability != "information.search.web" || needs[0].Qualifiers["freshness"] != "current" {
		t.Fatalf("need=%#v", needs[0])
	}
}

func TestIMSemanticNeedResolverHandlesManagedSecondaryLabel(t *testing.T) {
	registry := newIMSemanticCapabilityRegistry()
	needs, managed, err := semanticIntentNeedsFromClassification(registry, intent.ClassificationResult{
		Primary: intent.LabelNonCoding, Secondary: []intent.IntentLabel{intent.LabelSearch}, Confidence: .98,
		ToolNames: []string{"send_to_im", "web_fetch"},
	})
	if err != nil || !managed || len(needs) != 5 {
		t.Fatalf("needs=%#v managed=%v err=%v", needs, managed, err)
	}
	if needs[0].Capability != "information.search.web" || needs[0].Qualifiers["freshness"] != "reference" {
		t.Fatalf("secondary managed label did not select web capability: %#v", needs[0])
	}
	for i, need := range needs[1:] {
		if need.Capability != needs[0].Capability || need.Qualifiers["freshness"] != "reference" {
			t.Fatalf("repeat sibling %d must share the search capability: %#v", i+2, need)
		}
	}
}

func TestIMSemanticManagedCoverageRejectsUnmappedCapabilityLabel(t *testing.T) {
	fixture := semanticUnmigratedFixtureLabel(t)
	result := intent.ClassificationResult{
		Primary: intent.LabelSearch, Secondary: []intent.IntentLabel{fixture}, Confidence: .98,
	}
	if label, unmapped := imSemanticUnmappedCapabilityLabel(result); !unmapped || label != fixture {
		t.Fatalf("unmapped label = %q, %v; want %q, true", label, unmapped, fixture)
	}
	if _, unmapped := imSemanticUnmappedCapabilityLabel(intent.ClassificationResult{
		Primary: intent.LabelNonCoding, Secondary: []intent.IntentLabel{intent.LabelSearch}, Confidence: .98,
	}); unmapped {
		t.Fatal("generic non_coding label must not block a governed search need")
	}
}

func TestIMSemanticDocumentReadNeedRequiresTrustedInput(t *testing.T) {
	registry := newIMSemanticCapabilityRegistry()
	needs, managed, err := semanticIntentNeedsFromClassification(registry, intent.ClassificationResult{
		Primary: intent.LabelDocumentRead, Confidence: .98, ToolNames: []string{"office", "read_file", "send_file"},
	})
	// The declared document read plus the local-file bundle's optional
	// fs.read/fs.write offers.
	if err != nil || !managed || len(needs) != 3 || needs[0].Capability != "document.read.local" {
		t.Fatalf("needs=%#v managed=%v err=%v", needs, managed, err)
	}
	if _, err := semanticNeedsForTrustedDocumentInputs(needs, nil); err == nil || !strings.Contains(err.Error(), "trusted_document_input_missing") {
		t.Fatalf("missing trusted document input error=%v", err)
	}
}

func TestIMSemanticAttachmentDeliveryNeedsExactlyOneTrustedInput(t *testing.T) {
	registry := newIMSemanticCapabilityRegistry()
	needs, managed, err := semanticIntentNeedsFromClassification(registry, intent.ClassificationResult{
		Primary: intent.LabelAttachmentDelivery, Confidence: .98, ToolNames: []string{"send_file", "send_to_im", "office"},
	})
	if err != nil || !managed || len(needs) != 1 || needs[0].Capability != "artifact.deliver.current_channel" || needs[0].Qualifiers["format"] != "file" {
		t.Fatalf("needs=%#v managed=%v err=%v", needs, managed, err)
	}
	if _, err := semanticNeedsForTrustedDocumentInputs(needs, nil); err == nil || !strings.Contains(err.Error(), "trusted_document_input_missing") {
		t.Fatalf("missing trusted file error=%v", err)
	}
}

func TestIMSemanticAttachmentDeliveryUsesBoundFileArtifact(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry()}
	attachment := MessageAttachment{Type: "file", FileName: "report.pdf", MimeType: "application/pdf", SourceMediaID: "media-file-delivery", Data: base64.StdEncoding.EncodeToString([]byte("%PDF-1.7\ntrusted file payload"))}
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassificationAndAttachments("user-1", "send this attachment back", "lansenger", "root-file", "turn-file", &intent.ClassificationResult{Primary: intent.LabelAttachmentDelivery, Confidence: .98, ToolNames: []string{"send_file"}}, []MessageAttachment{attachment})
	if err != nil || !handled || surface == nil || len(defs) < 1 {
		t.Fatalf("defs=%#v handled=%v surface=%#v err=%v", defs, handled, surface, err)
	}
	selection := surface.plan.Selections[0]
	if selection.AdapterName != "semantic_deliver_current_file" || len(selection.ArtifactDependencies) != 1 || selection.ArtifactDependencies[0].Artifact.ID == "" {
		t.Fatalf("selection=%+v", selection)
	}
	name := extractToolName(defs[0])
	cb := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface, platform: "lansenger", loopCtx: &LoopContext{DeliveryTarget: &agent.DeliveryTarget{ChannelScope: "lansenger", DestinationID: "user:user-1"}}}
	if got := cb.ExecuteTool(name, `{"artifact_id":"forged","path":"C:/Windows/win.ini"}`); !strings.Contains(got, "parameter_unknown_field") {
		t.Fatalf("forged model input=%q", got)
	}
	// Rejection is an admitted use of a one-shot grant. Do not make a failed
	// parameter attempt retryable; start a distinct logical turn for the
	// success-path delivery assertion below.
	if got := cb.ExecuteTool(name, `{}`); !strings.Contains(got, "invocation_grant_replayed") {
		t.Fatalf("replayed rejected invocation=%q", got)
	}
	defs, surface, handled, err = h.semanticCallSurfaceForSharedTurnWithIdentityAndClassificationAndAttachments("user-1", "send this attachment back", "lansenger", "root-file-valid", "turn-file-valid", &intent.ClassificationResult{Primary: intent.LabelAttachmentDelivery, Confidence: .98}, []MessageAttachment{attachment})
	if err != nil || !handled || surface == nil || len(defs) < 1 {
		t.Fatalf("valid delivery surface defs=%#v handled=%v surface=%#v err=%v", defs, handled, surface, err)
	}
	selection = surface.plan.Selections[0]
	name = extractToolName(defs[0])
	cb = &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface, platform: "lansenger", loopCtx: &LoopContext{DeliveryTarget: &agent.DeliveryTarget{ChannelScope: "lansenger", DestinationID: "user:user-1"}}}
	if got := cb.ExecuteTool(name, `{}`); !strings.Contains(got, "Delivery committed") {
		t.Fatalf("delivery result=%q", got)
	}
	resp := &IMAgentResponse{}
	attachSharedLoopArtifacts(resp, cb)
	if resp.FileData != attachment.Data || resp.FileName != "attachment.pdf" || resp.FileMimeType != "application/pdf" || resp.SemanticDelivery == nil {
		t.Fatalf("response=%+v", resp)
	}
	record, err := surface.artifacts.store.Delivery(resp.SemanticDelivery.Scope, resp.SemanticDelivery.SelectionID)
	if err != nil || record.State != tool.DeliveryPrepared || record.ArtifactID != selection.ArtifactDependencies[0].ArtifactID {
		t.Fatalf("record=%+v err=%v", record, err)
	}
	if _, dispatch, err := resp.SemanticDelivery.Store.ClaimDeliveryDispatch(resp.SemanticDelivery.Scope, resp.SemanticDelivery.SelectionID, time.Now().UTC()); err != nil || !dispatch {
		t.Fatalf("dispatch=%v err=%v", dispatch, err)
	}
	if err := resp.SemanticDelivery.recordOutcome(tool.DeliveryAccepted); err != nil {
		t.Fatal(err)
	}
	if accepted, err := surface.artifacts.store.Delivery(resp.SemanticDelivery.Scope, resp.SemanticDelivery.SelectionID); err != nil || accepted.State != tool.DeliveryAccepted {
		t.Fatalf("accepted=%+v err=%v", accepted, err)
	}
	if execution, err := surface.executor.Execution(resp.SemanticDelivery.Scope, resp.SemanticDelivery.SelectionID); err != nil || execution.State != tool.PlanExecutionSucceeded {
		t.Fatalf("accepted delivery did not settle execution: execution=%+v err=%v", execution, err)
	}
}

func TestIMSemanticAttachmentDeliveryFailsClosedOnUnsupportedChannel(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry()}
	attachment := MessageAttachment{Type: "file", FileName: "report.txt", MimeType: "text/plain", Data: base64.StdEncoding.EncodeToString([]byte("payload"))}
	_, handled, err := h.semanticPlanForTurnWithClassificationAndAttachments("user-1", "send this attachment back", "qqbot", "root-file-qq", "turn-file-qq", &intent.ClassificationResult{Primary: intent.LabelAttachmentDelivery, Confidence: .98}, []MessageAttachment{attachment})
	if !handled || err == nil || !strings.Contains(err.Error(), "unmet needs") {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
}

func TestIMSemanticAttachmentDeliveryRejectsForgedBoundArtifact(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry()}
	attachment := MessageAttachment{Type: "file", FileName: "report.txt", MimeType: "text/plain", SourceMediaID: "media-file-tamper", Data: base64.StdEncoding.EncodeToString([]byte("bound file content"))}
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassificationAndAttachments("user-1", "send this attachment back", "lansenger", "root-file-tamper", "turn-file-tamper", &intent.ClassificationResult{Primary: intent.LabelAttachmentDelivery, Confidence: .98}, []MessageAttachment{attachment})
	if err != nil || !handled || surface == nil || len(defs) < 1 {
		t.Fatalf("defs=%#v handled=%v surface=%#v err=%v", defs, handled, surface, err)
	}
	selection := surface.plan.Selections[0]
	dependency := selection.ArtifactDependencies[0]
	forged, err := tool.NewArtifactPayload(dependency.Artifact.Scope, "trusted-input:channel-attachment:forged", "document", "text/plain", base64.StdEncoding.EncodeToString([]byte("forged file content")), time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := surface.artifacts.store.Publish(forged); err != nil {
		t.Fatal(err)
	}
	cb := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface, platform: "lansenger", loopCtx: &LoopContext{DeliveryTarget: &agent.DeliveryTarget{ChannelScope: "lansenger", DestinationID: "user:user-1"}}}
	if got := cb.ExecuteTool(extractToolName(defs[0]), `{}`); !strings.Contains(got, "Delivery committed") {
		t.Fatalf("delivery result=%q", got)
	}
	if cb.semanticDeliveryFileData != attachment.Data || cb.semanticDeliveryFileData == forged.Base64 {
		t.Fatalf("delivery used an artifact other than its exact binding: %q", cb.semanticDeliveryFileData)
	}
}

func TestSemanticDeliveryProjectionSettlesOnlyAfterClaimedReceipt(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry()}
	attachment := MessageAttachment{Type: "file", FileName: "report.txt", MimeType: "text/plain", Data: base64.StdEncoding.EncodeToString([]byte("receipt-bound file"))}
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassificationAndAttachments("user-1", "send this attachment back", "lansenger", "root-file-receipt", "turn-file-receipt", &intent.ClassificationResult{Primary: intent.LabelAttachmentDelivery, Confidence: .98}, []MessageAttachment{attachment})
	if err != nil || !handled || surface == nil || len(defs) < 1 {
		t.Fatalf("defs=%#v handled=%v surface=%#v err=%v", defs, handled, surface, err)
	}
	cb := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface, platform: "lansenger", loopCtx: &LoopContext{DeliveryTarget: &agent.DeliveryTarget{ChannelScope: "lansenger", DestinationID: "user:user-1"}}}
	if got := cb.ExecuteTool(extractToolName(defs[0]), `{}`); !strings.Contains(got, "Delivery committed") {
		t.Fatalf("delivery result=%q", got)
	}
	projection := cb.semanticDelivery
	if projection == nil {
		t.Fatal("missing prepared delivery projection")
	}
	if err := projection.recordOutcome(tool.DeliveryAccepted); err == nil || !strings.Contains(err.Error(), "delivery_outcome_conflict") {
		t.Fatalf("unclaimed receipt outcome error=%v", err)
	}
	if execution, err := surface.executor.Execution(surface.scope, projection.SelectionID); err != nil || execution.State != tool.PlanExecutionAwaitingReceipt {
		t.Fatalf("unclaimed receipt settled execution=%+v err=%v", execution, err)
	}
	if _, dispatch, err := projection.Store.ClaimDeliveryDispatch(projection.Scope, projection.SelectionID, time.Now().UTC()); err != nil || !dispatch {
		t.Fatalf("claim dispatch=%v err=%v", dispatch, err)
	}
	if err := projection.recordOutcome(tool.DeliveryAccepted); err != nil {
		t.Fatal(err)
	}
	if execution, err := surface.executor.Execution(surface.scope, projection.SelectionID); err != nil || execution.State != tool.PlanExecutionSucceeded {
		t.Fatalf("claimed receipt did not settle execution=%+v err=%v", execution, err)
	}
}

func TestIMSemanticDocumentReadUsesOpaqueTrustedAttachment(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelDocumentRead)}
	attachment := MessageAttachment{
		Type: "file", FileName: "notes.txt", MimeType: "text/plain", SourceMediaID: "channel-media-1",
		Data: base64.StdEncoding.EncodeToString([]byte("trusted attachment body")),
	}
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassificationAndAttachments(
		"user-1", "read my attachment", "lansenger", "root-doc", "turn-doc",
		&intent.ClassificationResult{Primary: intent.LabelDocumentRead, Confidence: .98, ToolNames: []string{"office", "read_file"}}, []MessageAttachment{attachment},
	)
	if err != nil || !handled || surface == nil || len(defs) < 1 {
		t.Fatalf("defs=%#v handled=%v surface=%#v err=%v", defs, handled, surface, err)
	}
	selection := surface.plan.Selections[0]
	if selection.AdapterName != "semantic_read_trusted_document" || selection.FitProof.QualifierBindings["format"] != "text" {
		t.Fatalf("selection=%+v", selection)
	}
	if len(selection.ArtifactDependencies) != 1 || selection.ArtifactDependencies[0].ArtifactID == "" || selection.ArtifactDependencies[0].ProducerSelection != "" {
		t.Fatalf("document dependency=%#v", selection.ArtifactDependencies)
	}
	definition := defs[0]["function"].(map[string]interface{})
	properties := definition["parameters"].(map[string]interface{})["properties"].(map[string]interface{})
	for _, forbidden := range []string{"file_path", "path", "action", "artifact_id", "provider"} {
		if _, exists := properties[forbidden]; exists {
			t.Fatalf("model-facing document schema exposed %q: %#v", forbidden, properties)
		}
	}
	name := extractToolName(defs[0])
	cb := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface}
	if got := cb.ExecuteTool("semantic_read_trusted_document", `{}`); !strings.Contains(got, "selection_not_authorized") {
		t.Fatalf("direct semantic adapter call=%q", got)
	}
	if got := cb.ExecuteTool(name, `{"file_path":"C:/Windows/win.ini"}`); !strings.Contains(got, "parameter_unknown_field") {
		t.Fatalf("forged path result=%q", got)
	}
}

func TestIMSemanticDesktopDocumentContinuationUsesHostBoundContext(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry()}
	path := filepath.Join(t.TempDir(), "resume.txt")
	if err := os.WriteFile(path, []byte("trusted desktop resume body"), 0o600); err != nil {
		t.Fatal(err)
	}
	firstTurn := "根据简历写学术简介\n\n" + filePathPromptPrefix + "\n" + path + "\n"
	if err := h.captureSelectedLocalDocuments("desktop-user", "desktop", "", firstTurn); err != nil {
		t.Fatalf("capture selected document: %v", err)
	}
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassificationAndAttachments(
		"desktop-user", "浓缩成300字左右的个人学术简介", "desktop", "root-followup", "turn-followup",
		&intent.ClassificationResult{Primary: intent.LabelUnknown, Confidence: .25, Degraded: true}, nil,
	)
	if err != nil || !handled || surface == nil || len(defs) == 0 {
		t.Fatalf("defs=%#v handled=%v surface=%#v err=%v", defs, handled, surface, err)
	}
	// Ambient retrieval (knowledge/memory) may add selections; locate document read explicitly.
	foundDoc := false
	for _, sel := range surface.plan.Selections {
		if sel.AdapterName == "semantic_read_trusted_document" && len(sel.ArtifactDependencies) == 1 {
			foundDoc = true
			break
		}
	}
	if !foundDoc {
		t.Fatalf("document read selection missing: %+v", surface.plan.Selections)
	}
	// No rendered definition should expose a file path – document identity is host-bound.
	for _, d := range defs {
		fn, _ := d["function"].(map[string]interface{})
		params, _ := fn["parameters"].(map[string]interface{})
		props, _ := params["properties"].(map[string]interface{})
		if _, found := props["file_path"]; found {
			t.Fatalf("document continuation exposed a path schema: %#v", props)
		}
	}
}

func TestIMSemanticDesktopDocumentContinuationFailsClosedWhenSourceChanges(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry()}
	path := filepath.Join(t.TempDir(), "resume.txt")
	if err := os.WriteFile(path, []byte("original resume"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := h.captureSelectedLocalDocuments("desktop-user", "desktop", "", filePathPromptPrefix+"\n"+path+"\n"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("changed resume"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, handled, err := h.semanticPlanForTurnWithClassificationAndAttachments(
		"desktop-user", "浓缩成300字", "desktop", "root-stale", "turn-stale",
		&intent.ClassificationResult{Primary: intent.LabelUnknown, Confidence: .25, Degraded: true}, nil,
	)
	if !handled || err == nil || !strings.Contains(err.Error(), "trusted_document_context_stale") {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
	if h.hasActiveLocalDocument("desktop-user", "desktop", "") {
		t.Fatal("stale context must be revoked after the failed validation")
	}
}

func TestIMSemanticExplicitNewDocumentDoesNotReuseActiveContext(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry()}
	path := filepath.Join(t.TempDir(), "resume.txt")
	if err := os.WriteFile(path, []byte("resume"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := h.captureSelectedLocalDocuments("desktop-user", "desktop", "", filePathPromptPrefix+"\n"+path+"\n"); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(t.TempDir(), "other.txt")
	_, handled, err := h.semanticPlanForTurnWithClassificationAndAttachments(
		"desktop-user", "读取另一份文件 "+other, "desktop", "root-other", "turn-other",
		&intent.ClassificationResult{Primary: intent.LabelDocumentRead, Confidence: .98}, nil,
	)
	if !handled || err == nil || !strings.Contains(err.Error(), "trusted_document_input_missing") {
		t.Fatalf("new source must not reuse active document: handled=%v err=%v", handled, err)
	}
}

func TestIMSemanticCurrentPickerMismatchDoesNotReuseActiveContext(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry()}
	first := filepath.Join(t.TempDir(), "first.txt")
	second := filepath.Join(t.TempDir(), "second.txt")
	for _, path := range []string{first, second} {
		if err := os.WriteFile(path, []byte("resume"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := h.captureSelectedLocalDocuments("desktop-user", "desktop", "", filePathPromptPrefix+"\n"+first+"\n"); err != nil {
		t.Fatal(err)
	}
	_, handled, err := h.semanticPlanForTurnWithClassificationAndAttachments(
		"desktop-user", "读取新选择的文件\n"+filePathPromptPrefix+"\n"+second+"\n", "desktop", "root-picker", "turn-picker",
		&intent.ClassificationResult{Primary: intent.LabelDocumentRead, Confidence: .98}, nil,
	)
	if !handled || err == nil || !strings.Contains(err.Error(), "trusted_document_context_picker_mismatch") {
		t.Fatalf("mismatched picker must not use old context: handled=%v err=%v", handled, err)
	}
}

func TestIMSemanticHistoricalPickerMarkerDoesNotReplaceActiveContext(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry()}
	path := filepath.Join(t.TempDir(), "resume.txt")
	if err := os.WriteFile(path, []byte("resume"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := h.captureSelectedLocalDocuments("desktop-user", "desktop", "", filePathPromptPrefix+"\n"+path+"\n"); err != nil {
		t.Fatal(err)
	}
	if err := h.captureSelectedLocalDocuments("desktop-user", "desktop", "", filePathPromptPrefixHistorical+"\nC:\\old\\resume.pdf\n"); err != nil {
		t.Fatalf("historical marker must be ignored, got %v", err)
	}
	if !h.hasActiveLocalDocument("desktop-user", "desktop", "") {
		t.Fatal("historical marker must not revoke current active context")
	}
}

func TestIMSemanticNewNonDocumentAttachmentDoesNotReuseActiveContext(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry()}
	path := filepath.Join(t.TempDir(), "resume.txt")
	if err := os.WriteFile(path, []byte("resume"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := h.captureSelectedLocalDocuments("desktop-user", "desktop", "", filePathPromptPrefix+"\n"+path+"\n"); err != nil {
		t.Fatal(err)
	}
	image := MessageAttachment{Type: "image", FileName: "new.png", MimeType: "image/png", Data: base64.StdEncoding.EncodeToString([]byte("not a document"))}
	_, handled, err := h.semanticPlanForTurnWithClassificationAndAttachments(
		"desktop-user", "读取这张新图片", "desktop", "root-image", "turn-image",
		&intent.ClassificationResult{Primary: intent.LabelDocumentRead, Confidence: .98}, []MessageAttachment{image},
	)
	if !handled || err == nil || !strings.Contains(err.Error(), "trusted_document_input_missing") {
		t.Fatalf("new attachment must not reuse active document: handled=%v err=%v", handled, err)
	}
}

func TestSemanticTrustedDocumentContextFailureIsHostRejected(t *testing.T) {
	err := fmt.Errorf("wrap: trusted_document_context_stale")
	if !semanticPlanErrorBlocksSession(err) {
		t.Fatal("stale trusted document must not fall through to legacy tools")
	}
	response := semanticHostRejectResponseForPlanError(err)
	if response == nil || response.Error != "semantic_trusted_document_context_stale" || !strings.Contains(response.Text, "重新选择") {
		t.Fatalf("response=%#v", response)
	}
}

func TestSemanticMissingTrustedDocumentInputIsHostRejected(t *testing.T) {
	err := fmt.Errorf("trusted_document_input_missing")
	if !semanticPlanErrorBlocksSession(err) {
		t.Fatal("missing trusted input must not fall back to legacy local-file tools")
	}
	response := semanticHostRejectResponseForPlanError(err)
	if response == nil || response.Error != "semantic_trusted_document_input_required" {
		t.Fatalf("response=%#v", response)
	}
}

func TestIMSemanticDesktopDocumentContextDoesNotGrantUnrelatedOrResetTurn(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry()}
	path := filepath.Join(t.TempDir(), "resume.txt")
	if err := os.WriteFile(path, []byte("resume"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := h.captureSelectedLocalDocuments("desktop-user", "desktop", "", filePathPromptPrefix+"\n"+path+"\n"); err != nil {
		t.Fatal(err)
	}
	if _, handled, err := h.semanticPlanForTurnWithClassificationAndAttachments(
		"desktop-user", "解释一下 MTP 推理", "desktop", "root-unrelated", "turn-unrelated",
		&intent.ClassificationResult{Primary: intent.LabelNonCoding, Confidence: .98}, nil,
	); handled || err != nil {
		t.Fatalf("unrelated turn must not inherit document access: handled=%v err=%v", handled, err)
	}
	h.clearActiveLocalDocumentsForUser("desktop-user")
	if h.hasActiveLocalDocument("desktop-user", "desktop", "") {
		t.Fatal("session reset must revoke active document context")
	}
}

func TestIMSemanticBareContinuationDoesNotInheritActiveDocument(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry()}
	path := filepath.Join(t.TempDir(), "resume.txt")
	if err := os.WriteFile(path, []byte("resume"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := h.captureSelectedLocalDocuments("desktop-user", "desktop", "", filePathPromptPrefix+"\n"+path+"\n"); err != nil {
		t.Fatal(err)
	}
	if _, handled, err := h.semanticPlanForTurnWithClassificationAndAttachments(
		"desktop-user", "继续", "desktop", "root-continue", "turn-continue",
		&intent.ClassificationResult{Primary: intent.LabelContinuation, Confidence: .98}, nil,
	); handled || err != nil {
		t.Fatalf("bare continuation must not inherit a document: handled=%v err=%v", handled, err)
	}
}

func TestActiveLocalDocumentContextUsesDestinationScope(t *testing.T) {
	h := &IMMessageHandler{}
	path := filepath.Join(t.TempDir(), "resume.txt")
	if err := os.WriteFile(path, []byte("resume"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := h.captureSelectedLocalDocuments("desktop-user", "desktop", "tab:one", filePathPromptPrefix+"\n"+path+"\n"); err != nil {
		t.Fatal(err)
	}
	if !h.hasActiveLocalDocument("desktop-user", "desktop", "tab:one") {
		t.Fatal("owner destination must resolve its own active document")
	}
	if h.hasActiveLocalDocument("desktop-user", "desktop", "tab:two") {
		t.Fatal("another destination must not reuse the active document")
	}
}

func TestActiveLocalDocumentContextMatchesSemanticRoutingDestination(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry()}
	path := filepath.Join(t.TempDir(), "resume.txt")
	if err := os.WriteFile(path, []byte("resume"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx := &LoopContext{DeliveryTarget: &agent.DeliveryTarget{ChannelScope: "desktop", DestinationID: "user:tab-one"}}
	if err := h.captureSelectedLocalDocuments("desktop-user", "desktop", sessionGovernedDestination(ctx), filePathPromptPrefix+"\n"+path+"\n"); err != nil {
		t.Fatal(err)
	}
	requestCtx, cancel := semanticRoutingContext(ctx)
	defer cancel()
	prepared, handled, err := h.semanticPlanForTurnWithContextAndClassificationAndAttachments(
		requestCtx, "desktop-user", "浓缩成300字", "desktop", "root-destination", "turn-destination",
		&intent.ClassificationResult{Primary: intent.LabelUnknown, Confidence: .25, Degraded: true}, nil,
	)
	if err != nil || !handled || prepared == nil || !planHasCapabilities(prepared.plan, "document.read.local") {
		t.Fatalf("context must use the same destination scope as routing: prepared=%#v handled=%v err=%v", prepared, handled, err)
	}
}

func TestIMSemanticDesktopDocumentContextExpiresAndNewAttachmentWins(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry()}
	path := filepath.Join(t.TempDir(), "resume.txt")
	if err := os.WriteFile(path, []byte("resume"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := h.captureSelectedLocalDocuments("desktop-user", "desktop", "", filePathPromptPrefix+"\n"+path+"\n"); err != nil {
		t.Fatal(err)
	}
	key := activeLocalDocumentContextKey("desktop-user", "desktop", "")
	value, ok := h.activeLocalDocuments.Load(key)
	if !ok {
		t.Fatal("missing captured context")
	}
	contexts := value.([]activeLocalDocumentContext)
	contexts[0].ExpiresAt = time.Now().UTC().Add(-time.Second)
	h.activeLocalDocuments.Store(key, contexts)
	if h.hasActiveLocalDocument("desktop-user", "desktop", "") {
		t.Fatal("expired context must be revoked")
	}
	if err := h.captureSelectedLocalDocuments("desktop-user", "desktop", "", filePathPromptPrefix+"\n"+path+"\n"); err != nil {
		t.Fatal(err)
	}
	attachment := MessageAttachment{Type: "file", FileName: "new.txt", MimeType: "text/plain", Data: base64.StdEncoding.EncodeToString([]byte("new attachment"))}
	defs, _, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassificationAndAttachments(
		"desktop-user", "浓缩成300字", "desktop", "root-new", "turn-new",
		&intent.ClassificationResult{Primary: intent.LabelUnknown, Confidence: .25, Degraded: true}, []MessageAttachment{attachment},
	)
	if err != nil || handled || len(defs) != 0 {
		// The fresh attachment must not be silently reclassified as the older
		// desktop document. Its weak generic classification remains chat (handled=false).
		t.Fatalf("new attachment must suppress old context: defs=%#v handled=%v err=%v", defs, handled, err)
	}
}

func TestIMSemanticDesktopDocumentContextRejectsAnyExpiredInput(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry()}
	first := filepath.Join(t.TempDir(), "first.txt")
	second := filepath.Join(t.TempDir(), "second.txt")
	for _, path := range []string{first, second} {
		if err := os.WriteFile(path, []byte("resume"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := h.captureSelectedLocalDocuments("desktop-user", "desktop", "", filePathPromptPrefix+"\n"+first+"\n"+second+"\n"); err != nil {
		t.Fatal(err)
	}
	key := activeLocalDocumentContextKey("desktop-user", "desktop", "")
	value, _ := h.activeLocalDocuments.Load(key)
	contexts := value.([]activeLocalDocumentContext)
	contexts[1].ExpiresAt = time.Now().UTC().Add(-time.Second)
	h.activeLocalDocuments.Store(key, contexts)
	if h.hasActiveLocalDocument("desktop-user", "desktop", "") {
		t.Fatal("any expired document must revoke the whole ambiguous context")
	}
}

func TestIMSemanticDocumentReadRejectsAmbiguousOrCrossScopeInput(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry()}
	classification := &intent.ClassificationResult{Primary: intent.LabelDocumentRead, Confidence: .98}
	encoded := base64.StdEncoding.EncodeToString([]byte("x"))
	_, handled, err := h.semanticPlanForTurnWithClassificationAndAttachments("user-1", "read both", "lansenger", "root", "turn", classification, []MessageAttachment{
		{Type: "file", FileName: "a.txt", MimeType: "text/plain", Data: encoded},
		{Type: "file", FileName: "b.txt", MimeType: "text/plain", Data: encoded},
	})
	if !handled || err == nil || !strings.Contains(err.Error(), "trusted_document_input_ambiguous") {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
	inputs, err := semanticDocumentInputsForTurn("root", "turn", "session-1", "user-1", []MessageAttachment{{Type: "file", FileName: "a.txt", MimeType: "text/plain", Data: encoded}})
	if err != nil || len(inputs) != 1 {
		t.Fatalf("inputs=%#v err=%v", inputs, err)
	}
	store := tool.NewMemoryArtifactStore()
	if _, err := store.Publish(inputs[0].Payload); err != nil {
		t.Fatal(err)
	}
	contract := tool.ArtifactContract{Kind: "document", Required: true}
	wrongScope := tool.InvocationScope{RootTaskID: "root", PlanID: "plan:other", SessionID: "user-2", TurnID: "turn", PrincipalID: "user-2"}
	if _, err := store.IssueProjectedAccessGrant(inputs[0].Payload.Ref, wrongScope, "selection:read", contract, time.Minute); err == nil {
		t.Fatal("cross-principal document input was granted")
	}
}

func TestIMSemanticDocumentReadConsumesOnlyBoundArtifact(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry()}
	classification := &intent.ClassificationResult{Primary: intent.LabelDocumentRead, Confidence: .98}
	attachment := MessageAttachment{Type: "file", FileName: "notes.txt", MimeType: "text/plain", SourceMediaID: "media-bound", Data: base64.StdEncoding.EncodeToString([]byte("bound document content"))}
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassificationAndAttachments("user-1", "read attachment", "lansenger", "root-bound", "turn-bound", classification, []MessageAttachment{attachment})
	if err != nil || !handled || surface == nil || len(defs) < 1 {
		t.Fatalf("defs=%#v handled=%v surface=%#v err=%v", defs, handled, surface, err)
	}
	selection := surface.plan.Selections[0]
	dependency := selection.ArtifactDependencies[0]
	if dependency.Artifact.ID == "" || dependency.Artifact.ID != dependency.ArtifactID {
		t.Fatalf("bound artifact provenance missing: %#v", dependency)
	}
	forgedPayload, err := tool.NewArtifactPayload(dependency.Artifact.Scope, "trusted-input:channel-attachment:forged", "document", "text/plain", base64.StdEncoding.EncodeToString([]byte("forged document content")), time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := surface.artifacts.store.Publish(forgedPayload); err != nil {
		t.Fatal(err)
	}
	name := extractToolName(defs[0])
	cb := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface}
	got := cb.ExecuteTool(name, `{}`)
	if !strings.Contains(got, "bound document content") || strings.Contains(got, "forged document content") {
		t.Fatalf("read result=%q", got)
	}
	if strings.Contains(got, "semantic-document-") || strings.Contains(got, "# path:") {
		t.Fatalf("trusted temporary path leaked to model result: %q", got)
	}
	if got := cb.ExecuteTool(name, `{}`); !strings.Contains(got, "invocation_grant_replayed") {
		t.Fatalf("replayed document grant=%q", got)
	}
}

func TestIMSemanticDocumentReadRejectsTamperedArtifactBinding(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry()}
	attachment := MessageAttachment{Type: "file", FileName: "notes.txt", MimeType: "text/plain", SourceMediaID: "media-tamper", Data: base64.StdEncoding.EncodeToString([]byte("bound document content"))}
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassificationAndAttachments("user-1", "read attachment", "lansenger", "root-tamper", "turn-tamper", &intent.ClassificationResult{Primary: intent.LabelDocumentRead, Confidence: .98}, []MessageAttachment{attachment})
	if err != nil || !handled || surface == nil || len(defs) < 1 {
		t.Fatalf("defs=%#v handled=%v surface=%#v err=%v", defs, handled, surface, err)
	}
	surface.plan.Selections[0].ArtifactDependencies[0].Artifact.IntegrityDigest = strings.Repeat("0", 64)
	got := (&sharedAgentLoopCallbacks{handler: h, semanticSurface: surface}).ExecuteTool(extractToolName(defs[0]), `{}`)
	if !strings.Contains(got, "artifact not found") && !strings.Contains(got, "artifact_projection_invalid") {
		t.Fatalf("tampered artifact binding was accepted: %q", got)
	}
}

func TestSemanticDocumentReadResultProjectionRemovesLegacyPathContinuation(t *testing.T) {
	result := semanticDocumentReadResultProjection("# format: text\n# path: C:\\private\\semantic-document-123.txt\n# truncated: true\n# next_offset: 42\n# continue: office(action=\"read_document\", file_path=\"C:\\private\\semantic-document-123.txt\", offset=42)\nbody")
	if strings.Contains(result, "semantic-document-123") || strings.Contains(result, "file_path") || strings.Contains(result, "office(action") {
		t.Fatalf("legacy continuation leaked private adapter detail: %q", result)
	}
	if !strings.Contains(result, "# next_offset: 42") || !strings.Contains(result, "body") {
		t.Fatalf("useful continuation result was lost: %q", result)
	}
}

func TestSemanticPlanUnmanagedLabelsFallThroughToLegacyTools(t *testing.T) {
	h := &IMMessageHandler{}
	for _, tc := range []struct {
		name  string
		label intent.IntentLabel
		text  string
	}{
		{name: "non_coding", label: intent.LabelNonCoding, text: "LLM推理中的mtp 实际上就是一种自身的投机解码？"},
		{name: "continuation", label: intent.LabelContinuation, text: "继续"},
		{name: "unknown", label: intent.LabelUnknown, text: "你好"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			prepared, handled, err := h.semanticPlanForTurnWithClassification(
				"user", tc.text, "desktop", "root", "turn",
				&intent.ClassificationResult{Primary: tc.label, Confidence: .78},
			)
			if err != nil || handled || prepared != nil {
				t.Fatalf("unmanaged %s must not fail-close the legacy tool surface, prepared=%#v handled=%v err=%v", tc.name, prepared, handled, err)
			}
		})
	}
}

func TestSemanticPlanLiveDataWithNonCodingSecondaryStillPlans(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry()}
	registerBuiltinTools(h.registry, h)
	prepared, handled, err := h.semanticPlanForTurnWithClassification(
		"user", "北京天气", "desktop", "root", "turn",
		&intent.ClassificationResult{Primary: intent.LabelLiveData, Secondary: []intent.IntentLabel{intent.LabelNonCoding}, Confidence: .90},
	)
	if err != nil || !handled || prepared == nil || !planHasCapabilities(prepared.plan, "information.search.web") {
		t.Fatalf("live_data + non_coding secondary must keep the search plan, prepared=%#v handled=%v err=%v", prepared, handled, err)
	}
}

func TestSemanticPlanDoesNotPromoteSearchFromWording(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry()}
	registerBuiltinTools(h.registry, h)
	classification := &intent.ClassificationResult{Primary: intent.LabelKnowledgeRead, Confidence: .96}
	prepared, handled, err := h.semanticPlanForTurnWithClassification(
		"user", "全网搜索张慧妹资料", "desktop", "root", "turn", classification,
	)
	if err != nil || !handled || prepared == nil {
		t.Fatalf("semantic knowledge read must retain its own plan, prepared=%#v handled=%v err=%v", prepared, handled, err)
	}
	if planHasCapabilities(prepared.plan, "information.search.web") {
		t.Fatalf("wording must not promote an unconfirmed search capability: %#v", prepared.plan.Selections)
	}
}

func TestSemanticPlanTreeConfirmedLiveDataKeepsCurrentFreshness(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry()}
	registerBuiltinTools(h.registry, h)
	classification := &intent.ClassificationResult{Primary: intent.LabelLiveData, Confidence: .94, Layer: 3}
	prepared, handled, err := h.semanticPlanForTurnWithClassification(
		"user", "网上查北京天气", "desktop", "root", "turn", classification,
	)
	if err != nil || !handled || prepared == nil {
		t.Fatalf("tree-confirmed live_data must plan lookup, prepared=%#v handled=%v err=%v", prepared, handled, err)
	}
	selection, ok := semanticSelectionForCapability(prepared.plan, "information.search.web")
	if !ok || selection.FitProof.QualifierBindings["freshness"] != "current" {
		t.Fatalf("freshness=%q found=%v, want current", selection.FitProof.QualifierBindings["freshness"], ok)
	}
}

func TestSemanticPlanConfirmedSearchPreservesSemanticGenerateChain(t *testing.T) {
	h := registerDocumentGeneratePDF(t)
	if err := h.registry.Register(RegisteredTool{
		Name: "web_search", Status: RegToolAvailable, InputSchema: map[string]interface{}{},
		CapabilityProvisions: []tool.CapabilityProvision{{Capability: "information.search.web", Qualifiers: map[string]string{"freshness": "reference"}, Quality: 1}},
		SemanticEffects:      []tool.EffectClass{tool.EffectReadOnly},
		Handler:              func(map[string]interface{}) string { return "ok" },
	}); err != nil {
		t.Fatal(err)
	}
	// Both labels are supplied by semantic classification. Wording must never
	// create either lookup or document generation capability.
	classification := &intent.ClassificationResult{
		Primary: intent.LabelSearch, Secondary: []intent.IntentLabel{intent.LabelDocumentGenerate}, Confidence: .96, Layer: 3,
	}
	prepared, handled, err := h.semanticPlanForTurnWithClassification(
		"user", "全网搜索张慧妹资料，并生成pdf报告", "desktop", "root", "turn", classification,
	)
	if err != nil || !handled || prepared == nil {
		t.Fatalf("search+pdf must plan, prepared=%#v handled=%v err=%v", prepared, handled, err)
	}
	if !planHasCapabilities(prepared.plan, "information.search.web", "document.generate.file", "artifact.deliver.current_channel") {
		t.Fatalf("selections=%#v", prepared.plan.Selections)
	}
	assertGenerateRequiresOnlyBaseSearch(t, prepared.plan)
	degraded := &intent.ClassificationResult{Primary: intent.LabelUnknown, Confidence: .65, Degraded: true}
	prepared, handled, err = h.semanticPlanForTurnWithClassification(
		"user", "全网搜索张慧妹资料，并生成pdf报告", "desktop", "root-degraded", "turn", degraded,
	)
	if err != nil || handled || prepared != nil {
		t.Fatalf("degraded generic text must not gain lookup or generation, prepared=%#v handled=%v err=%v", prepared, handled, err)
	}
}

func TestSemanticPlanFailsClosedForMixedManagedAndUnmappedCapabilityLabels(t *testing.T) {
	h := &IMMessageHandler{}
	classification := &intent.ClassificationResult{
		Primary: intent.LabelSearch, Secondary: []intent.IntentLabel{semanticUnmigratedFixtureLabel(t)}, Confidence: .98,
	}
	prepared, handled, err := h.semanticPlanForTurnWithClassification("user", "search then send", "lansenger", "root", "turn", classification)
	if !handled || prepared != nil || err == nil || !strings.Contains(err.Error(), "unmapped capability label") {
		t.Fatalf("prepared=%#v handled=%v err=%v", prepared, handled, err)
	}
}

func TestSemanticPlanUsesTurnClassificationInsteadOfReclassifying(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry()}
	if err := h.registry.Register(RegisteredTool{
		Name: "current_datetime", Status: RegToolAvailable, InputSchema: map[string]interface{}{},
		CapabilityProvisions: []tool.CapabilityProvision{{Capability: "information.current_time", Quality: 1}},
		SemanticEffects:      []tool.EffectClass{tool.EffectReadOnly},
		Handler:              func(map[string]interface{}) string { return "clock" },
	}); err != nil {
		t.Fatal(err)
	}
	classification := &intent.ClassificationResult{Primary: intent.LabelCurrentTime, Confidence: .98}
	prepared, handled, err := h.semanticPlanForTurnWithClassification("user", "unrelated wording", "lansenger", "root", "turn", classification)
	if !handled || err != nil || prepared == nil || len(prepared.plan.Selections) != 1 {
		t.Fatalf("prepared=%#v handled=%v err=%v", prepared, handled, err)
	}
}

func TestSemanticCurrentTimeUsesOpaqueBoundCapability(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelCurrentTime)}
	called := 0
	if err := h.registry.Register(RegisteredTool{
		Name: "current_datetime", Status: RegToolAvailable,
		InputSchema:          map[string]interface{}{},
		CapabilityProvisions: []tool.CapabilityProvision{{Capability: "information.current_time", Quality: 1}},
		SemanticEffects:      []tool.EffectClass{tool.EffectReadOnly},
		Handler: func(map[string]interface{}) string {
			called++
			return "clock"
		},
	}); err != nil {
		t.Fatal(err)
	}
	if registered, ok := h.registry.Get("current_datetime"); !ok || registered.SemanticCatalogState != SemanticCatalogCapability {
		t.Fatalf("current_datetime semantic registration=%#v found=%v", registered, ok)
	}
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurn("user-1", "what time is it now?", "lansenger")
	if err != nil || !handled || surface == nil || len(defs) < 1 {
		t.Fatalf("defs=%#v handled=%v surface=%#v err=%v", defs, handled, surface, err)
	}
	name := extractToolName(defs[0])
	if name != "current_datetime" {
		t.Fatalf("function name=%q, want current_datetime", name)
	}
	if surface.plan.Selections[0].AdapterName != semanticTrustedClockAdapter {
		t.Fatalf("selection=%+v, want host clock adapter", surface.plan.Selections[0])
	}
	callback := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface, tools: defs}
	if got := callback.ExecuteTool(semanticTrustedClockAdapter, `{}`); !strings.Contains(got, "selection_not_authorized") {
		t.Fatalf("direct adapter call=%q", got)
	}
	if got := callback.ExecuteTool(name, `{}`); !strings.Contains(got, "ISO week") || called != 0 {
		t.Fatalf("bound call=%q called=%d", got, called)
	}
}

func TestSemanticCurrentTimeUsesHostAdapterWhenRegistryEmpty(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelCurrentTime)}
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurn("user-1", "what time is it now?", "lansenger")
	if err != nil || !handled || surface == nil || len(defs) < 1 {
		t.Fatalf("defs=%#v handled=%v surface=%#v err=%v", defs, handled, surface, err)
	}
	if surface.plan.Selections[0].AdapterName != semanticTrustedClockAdapter {
		t.Fatalf("empty registry must still select the host clock, selection=%+v", surface.plan.Selections[0])
	}
}

func TestSemanticWebSearchUsesFreshnessQualifiedOpaqueBoundCapability(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelSearch)}
	called := 0
	h.semanticTrustedWebSearch = func(userID, query string) (string, error) {
		called++
		return "Public web results for \"" + query + "\" (1):\n\n1. Channels\n   https://example.com", nil
	}
	if err := h.registry.Register(RegisteredTool{
		Name: "public_web_lookup", Description: "untrusted web lookup description", Status: RegToolAvailable,
		InputSchema: map[string]interface{}{
			"query": map[string]interface{}{"type": "string"},
		},
		Required: []string{"query"},
		CapabilityProvisions: []tool.CapabilityProvision{{
			Capability: "information.search.web", Qualifiers: map[string]string{"freshness": "reference"}, Quality: 1,
		}},
		SemanticEffects: []tool.EffectClass{tool.EffectReadOnly},
		Handler:         func(map[string]interface{}) string { return "soup" },
	}); err != nil {
		t.Fatal(err)
	}
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurn("user-1", "find Go concurrency documentation", "lansenger")
	if err != nil || !handled || surface == nil || len(defs) < 1 {
		t.Fatalf("defs=%#v handled=%v surface=%#v err=%v", defs, handled, surface, err)
	}
	name := semanticGrantNameForAdapter(surface, semanticTrustedWebSearchAdapter)
	if name != "web_search" {
		t.Fatalf("function name=%q, want web_search", name)
	}
	selection, ok := semanticSelectionForCapability(surface.plan, "information.search.web")
	if !ok || selection.AdapterName != semanticTrustedWebSearchAdapter || selection.FitProof.QualifierBindings["freshness"] != "reference" {
		t.Fatalf("selection=%+v found=%v, want host search adapter with reference freshness", selection, ok)
	}
	callback := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface}
	if got := callback.ExecuteTool("public_web_lookup", `{"query":"forged"}`); !strings.Contains(got, "selection_not_authorized") {
		t.Fatalf("direct provider call=%q", got)
	}
	if got := callback.ExecuteTool(name, `{"query":"channels"}`); !strings.Contains(got, "Public web results") || called != 1 {
		t.Fatalf("bound call=%q called=%d", got, called)
	}
}

func TestSemanticLiveDataSelectsCurrentFreshnessProvider(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelLiveData)}
	for _, registered := range []RegisteredTool{
		{
			Name: "reference_lookup", Status: RegToolAvailable, InputSchema: map[string]interface{}{"query": map[string]interface{}{"type": "string"}}, Required: []string{"query"},
			CapabilityProvisions: []tool.CapabilityProvision{{Capability: "information.search.web", Qualifiers: map[string]string{"freshness": "reference"}, Quality: 10}},
			SemanticEffects:      []tool.EffectClass{tool.EffectReadOnly}, Handler: func(map[string]interface{}) string { return "wrong" },
		},
		{
			Name: "current_lookup", Status: RegToolAvailable, InputSchema: map[string]interface{}{"query": map[string]interface{}{"type": "string"}}, Required: []string{"query"},
			CapabilityProvisions: []tool.CapabilityProvision{{Capability: "information.search.web", Qualifiers: map[string]string{"freshness": "current"}, Quality: 1}},
			SemanticEffects:      []tool.EffectClass{tool.EffectReadOnly}, Handler: func(map[string]interface{}) string { return "current" },
		},
	} {
		if err := h.registry.Register(registered); err != nil {
			t.Fatal(err)
		}
	}
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurn("user-1", "what is the latest weather?", "lansenger")
	if err != nil || !handled || surface == nil || len(defs) < 1 {
		t.Fatalf("defs=%#v handled=%v surface=%#v err=%v", defs, handled, surface, err)
	}
	selection, ok := semanticSelectionForCapability(surface.plan, "information.search.web")
	if !ok || selection.AdapterName != semanticTrustedWebSearchAdapter || selection.FitProof.QualifierBindings["freshness"] != "current" {
		t.Fatalf("selection=%+v found=%v, want host search adapter with current qualifier", selection, ok)
	}
}

func TestSemanticWebSearchUsesHostAdapterWhenOnlyWrongFreshnessSoupExists(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelLiveData)}
	if err := h.registry.Register(RegisteredTool{
		Name: "reference_lookup", Status: RegToolAvailable, InputSchema: map[string]interface{}{"query": map[string]interface{}{"type": "string"}}, Required: []string{"query"},
		CapabilityProvisions: []tool.CapabilityProvision{{Capability: "information.search.web", Qualifiers: map[string]string{"freshness": "reference"}, Quality: 1}},
		SemanticEffects:      []tool.EffectClass{tool.EffectReadOnly}, Handler: func(map[string]interface{}) string { return "wrong" },
	}); err != nil {
		t.Fatal(err)
	}
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurn("user-1", "what is the latest weather?", "lansenger")
	if err != nil || !handled || surface == nil || len(defs) < 1 {
		t.Fatalf("handled=%v surface=%#v err=%v", handled, surface, err)
	}
	selection, ok := semanticSelectionForCapability(surface.plan, "information.search.web")
	if !ok || selection.AdapterName != semanticTrustedWebSearchAdapter {
		t.Fatalf("wrong-freshness soup must stay unpublished, selection=%+v found=%v", selection, ok)
	}
}

func TestSemanticCatalogUsesRegisteredSchemaWhenLegacyDefinitionsOmitManagedBuiltin(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelCurrentTime)}
	if err := h.registry.Register(RegisteredTool{
		Name: "clock_impl", Description: "registered capability schema", Status: RegToolAvailable,
		InputSchema:          map[string]interface{}{},
		CapabilityProvisions: []tool.CapabilityProvision{{Capability: "information.current_time", Quality: 1}},
		SemanticEffects:      []tool.EffectClass{tool.EffectReadOnly},
		Handler:              func(map[string]interface{}) string { return "clock" },
	}); err != nil {
		t.Fatal(err)
	}
	// h.getTools falls back to the legacy hard-coded list in this minimal host;
	// clock_impl is intentionally absent there. The semantic provider must still
	// materialize from its registration-bound schema instead of silently losing
	// the capability to a legacy name-router filter.
	if defs := h.getTools(); findToolDef(defs, "clock_impl") != nil {
		t.Fatal("test requires clock_impl to be absent from legacy definitions")
	}
	defs, _, handled, err := h.semanticCallSurfaceForSharedTurn("user-1", "what time is it now?", "lansenger")
	if err != nil || !handled || len(defs) < 1 {
		t.Fatalf("defs=%#v handled=%v err=%v", defs, handled, err)
	}
	if name := extractToolName(defs[0]); name != "current_datetime" && !strings.HasPrefix(name, "invoke_") {
		t.Fatalf("function name=%q, want current_datetime or leftover grant token", name)
	}
}

func TestSemanticCallSurfaceUsesOpaqueFunctionAndBoundExecution(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry()}
	called := 0
	if err := h.registry.Register(RegisteredTool{
		Name: "screenshot", Description: "untrusted host description", Status: RegToolAvailable,
		InputSchema: map[string]interface{}{"display": map[string]interface{}{"type": "integer"}},
		CapabilityProvisions: []tool.CapabilityProvision{{
			Capability: "visual.capture.desktop", Qualifiers: map[string]string{"display": "primary"}, Quality: 1,
		}},
		SemanticEffects:  []tool.EffectClass{tool.EffectReadOnly},
		SemanticProduces: []tool.ArtifactContract{{Kind: "image", MIMEType: "image/png", Required: true}},
		Handler: func(args map[string]interface{}) string {
			called++
			return "captured"
		},
	}); err != nil {
		t.Fatalf("register screenshot: %v", err)
	}
	h.unifiedClassifier = semanticTestClassifier(t)
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurn("user-1", "capture the primary desktop screen", "lansenger")
	if err != nil || !handled || surface == nil || len(defs) < 1 {
		t.Fatalf("surface defs=%#v handled=%v surface=%#v err=%v", defs, handled, surface, err)
	}
	name := extractToolName(defs[0])
	if name != "screenshot" {
		t.Fatalf("function name = %q, want screenshot", name)
	}
	if strings.Contains(defs[0]["function"].(map[string]interface{})["description"].(string), "untrusted") {
		t.Fatalf("renderer leaked source description: %#v", defs[0])
	}
	callback := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface}
	if got := callback.ExecuteTool(name, `{}`); got != "captured" || called != 1 {
		t.Fatalf("bound call=%q called=%d", got, called)
	}
	if got := callback.ExecuteTool(name, `{}`); !strings.Contains(got, "invocation_grant_replayed") || called != 1 {
		t.Fatalf("replay=%q called=%d", got, called)
	}
}

func TestSemanticCallSurfaceRejectsModelParameterInjectionBeforeLegacyExecution(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticTestClassifier(t)}
	called := 0
	if err := h.registry.Register(RegisteredTool{
		Name: "screenshot", Status: RegToolAvailable,
		InputSchema: map[string]interface{}{
			"type": "object", "properties": map[string]interface{}{"display": map[string]interface{}{"type": "integer"}},
			"additionalProperties": false,
		},
		CapabilityProvisions: []tool.CapabilityProvision{{Capability: "visual.capture.desktop", Qualifiers: map[string]string{"display": "primary"}, Quality: 1}},
		SemanticEffects:      []tool.EffectClass{tool.EffectReadOnly},
		SemanticProduces:     []tool.ArtifactContract{{Kind: "image", MIMEType: "image/png", Required: true}},
		Handler: func(args map[string]interface{}) string {
			called++
			return "captured"
		},
	}); err != nil {
		t.Fatalf("register screenshot: %v", err)
	}
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurn("user-1", "capture primary screen", "lansenger")
	if err != nil || !handled || surface == nil || len(defs) < 1 {
		t.Fatalf("surface defs=%#v handled=%v surface=%#v err=%v", defs, handled, surface, err)
	}
	callback := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface}
	name := extractToolName(defs[0])
	if got := callback.ExecuteTool(name, `{"provider":"forged"}`); !strings.Contains(got, "parameter_reserved_field") {
		t.Fatalf("reserved argument result=%q", got)
	}
	if called != 0 {
		t.Fatalf("handler called after reserved argument injection: %d", called)
	}
	if _, active := surface.grants[name]; active {
		t.Fatalf("parameter-rejected grant remained active: %#v", surface.grants)
	}
	if _, retired := surface.retiredGrants[name]; !retired {
		t.Fatalf("parameter-rejected grant was not retained for replay: %#v", surface.retiredGrants)
	}
	for _, definition := range callback.BuildTools("") {
		if extractToolName(definition) == name {
			t.Fatalf("parameter-rejected function remained visible: %#v", callback.BuildTools(""))
		}
	}
	// Grant consumption intentionally precedes argument validation. A rejected
	// first use is terminal, so the same exposed adapter cannot be varied until
	// it happens to pass downstream validation.
	if got := callback.ExecuteTool(name, `{"display":1}`); !strings.Contains(got, "invocation_grant_replayed") {
		t.Fatalf("replayed grant after rejected parameters=%q", got)
	}
}

func TestSemanticAdapterFailureRetiresOpaqueFunctionWithoutCoordinator(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticTestClassifier(t)}
	if err := h.registry.Register(RegisteredTool{
		Name: "screenshot", Status: RegToolAvailable,
		InputSchema:          map[string]interface{}{"type": "object", "properties": map[string]interface{}{}, "additionalProperties": false},
		CapabilityProvisions: []tool.CapabilityProvision{{Capability: "visual.capture.desktop", Qualifiers: map[string]string{"display": "primary"}, Quality: 1}},
		SemanticEffects:      []tool.EffectClass{tool.EffectReadOnly},
		SemanticProduces:     []tool.ArtifactContract{{Kind: "image", MIMEType: "image/png", Required: true}},
		Handler:              func(map[string]interface{}) string { return "[system rejected] provider_failed" },
	}); err != nil {
		t.Fatalf("register screenshot: %v", err)
	}
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurn("user-1", "capture primary screen", "lansenger")
	if err != nil || !handled || surface == nil || len(defs) < 1 {
		t.Fatalf("defs=%#v handled=%v surface=%#v err=%v", defs, handled, surface, err)
	}
	name := extractToolName(defs[0])
	callback := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface, tools: defs}
	if got := callback.ExecuteTool(name, `{}`); !strings.Contains(got, "provider_failed") {
		t.Fatalf("provider failure result=%q", got)
	}
	if _, active := surface.grants[name]; active {
		t.Fatalf("failed adapter remained active: %#v", surface.grants)
	}
	if _, retired := surface.retiredGrants[name]; !retired {
		t.Fatalf("failed adapter lost replay identity: %#v", surface.retiredGrants)
	}
	if tools := callback.BuildTools(""); len(tools) != 0 {
		t.Fatalf("failed adapter remained visible: %#v", tools)
	}
	if got := callback.ExecuteTool(name, `{}`); !strings.Contains(got, "invocation_grant_replayed") {
		t.Fatalf("retired adapter did not remain one-shot: %q", got)
	}
}

func TestCoordinatedSemanticParameterRejectionKeepsGrantLive(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry()}
	h.semanticTrustedWebSearch = func(_, _ string) (string, error) { return "Beijing weather: clear, 28C", nil }
	registerBuiltinTools(h.registry, h)
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "北京天气", "desktop", "root-web-reject", "turn-web-reject",
		&intent.ClassificationResult{Primary: intent.LabelLiveData, Confidence: .98},
	)
	if err != nil || !handled || surface == nil || len(defs) < 1 {
		t.Fatalf("defs=%#v handled=%v surface=%#v err=%v", defs, handled, surface, err)
	}
	name := semanticGrantNameForAdapter(surface, semanticTrustedWebSearchAdapter)
	callback := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface, tools: defs, checkpointRunID: "loop-web-reject"}
	first := callback.ExecuteToolCall(name, `{"query":"北京天气","unknown":true}`, "call-invalid")
	if !strings.Contains(first.Result, "parameter_unknown_field: unknown") {
		t.Fatalf("invalid parameter result must localize the offending field: %+v", first)
	}
	if !strings.Contains(first.Result, "remains available") {
		t.Fatalf("rejection must tell the model the grant survived: %+v", first)
	}
	// A pre-execution refusal consumes nothing: the grant stays live, the tool
	// stays rendered, and no durable record is needed because the same invalid
	// arguments fail validation deterministically on replay. Grant maps are
	// keyed by the opaque invoke_* name, not the model-visible stable name.
	resolved := surface.resolveFunctionName(name)
	if _, active := surface.grants[resolved]; !active {
		t.Fatalf("pre-execution rejection consumed the grant: %#v", surface.grants)
	}
	if _, retired := surface.retiredGrants[resolved]; retired {
		t.Fatalf("pre-execution rejection retired the grant: %#v", surface.retiredGrants)
	}
	if tools := callback.BuildTools(""); len(tools) == 0 {
		t.Fatal("pre-execution rejection hid the model-visible tool")
	}
	if replay := callback.ExecuteToolCall(name, `{"query":"北京天气","unknown":true}`, "call-invalid"); !strings.Contains(replay.Result, "parameter_unknown_field: unknown") {
		t.Fatalf("same invalid call must replay the deterministic refusal: %+v", replay)
	}
	// The corrected call rides the same still-live grant and executes.
	retry := callback.ExecuteToolCall(name, `{"query":"北京天气"}`, "call-valid-after-reject")
	if !strings.Contains(retry.Result, "Beijing weather: clear, 28C") {
		t.Fatalf("corrected retry did not execute on the surviving grant: %+v", retry)
	}
}

func TestSemanticHostCallJournalReplaysResultWithoutSecondExecution(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticTestClassifier(t)}
	called := 0
	if err := h.registry.Register(RegisteredTool{
		Name: "screenshot", Status: RegToolAvailable,
		InputSchema:          map[string]interface{}{"display": map[string]interface{}{"type": "integer"}},
		CapabilityProvisions: []tool.CapabilityProvision{{Capability: "visual.capture.desktop", Qualifiers: map[string]string{"display": "primary"}, Quality: 1}},
		SemanticEffects:      []tool.EffectClass{tool.EffectReadOnly},
		SemanticProduces:     []tool.ArtifactContract{{Kind: "image", MIMEType: "image/png", Required: true}},
		Handler: func(map[string]interface{}) string {
			called++
			return "captured"
		},
	}); err != nil {
		t.Fatalf("register screenshot: %v", err)
	}
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurn("user-1", "capture primary screen", "lansenger")
	if err != nil || !handled || surface == nil || len(defs) < 1 {
		t.Fatalf("surface defs=%#v handled=%v surface=%#v err=%v", defs, handled, surface, err)
	}
	callback := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface, checkpointRunID: "loop-1"}
	name := extractToolName(defs[0])
	if got := callback.ExecuteToolCall(name, `{"display":1}`, "call-1"); got.Result != "captured" || got.Outcome != agent.ToolExecutionOutcomeOK || called != 1 {
		t.Fatalf("first call=%+v called=%d", got, called)
	}
	if got := callback.ExecuteToolCall(name, `{"display":1}`, "call-1"); got.Result != "captured" || got.Outcome != agent.ToolExecutionOutcomeOK || called != 1 {
		t.Fatalf("replay call=%+v called=%d", got, called)
	}
	if got := callback.ExecuteToolCall(name, `{"display":2}`, "call-1"); !strings.Contains(got.Result, "host_call_conflict") || called != 1 {
		t.Fatalf("conflict call=%+v called=%d", got, called)
	}
}

func TestSemanticCallSurfaceDoesNotFallBackWhenProviderUnavailable(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticTestClassifier(t)}
	_, surface, handled, err := h.semanticCallSurfaceForSharedTurn("user-1", "capture the primary desktop screen", "lansenger")
	if !handled || surface != nil || err == nil || !strings.Contains(err.Error(), "unmet needs") {
		t.Fatalf("handled=%v surface=%#v err=%v", handled, surface, err)
	}
}

func TestSemanticCallSurfaceUsesDurableLoopIdentity(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticTestClassifier(t)}
	if err := h.registry.Register(RegisteredTool{
		Name: "screenshot", Status: RegToolAvailable,
		InputSchema: map[string]interface{}{"display": map[string]interface{}{"type": "integer"}},
		CapabilityProvisions: []tool.CapabilityProvision{{
			Capability: "visual.capture.desktop", Qualifiers: map[string]string{"display": "primary"}, Quality: 1,
		}},
		SemanticEffects:  []tool.EffectClass{tool.EffectReadOnly},
		SemanticProduces: []tool.ArtifactContract{{Kind: "image", MIMEType: "image/png", Required: true}},
		Handler:          func(map[string]interface{}) string { return "captured" },
	}); err != nil {
		t.Fatalf("register screenshot: %v", err)
	}
	ctx := &LoopContext{ID: "stable-loop", Runtime: RuntimeContext{RequestID: "request-7"}}
	_, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithContext(ctx, "user-1", "capture primary screen", "lansenger")
	if err != nil || !handled || surface == nil {
		t.Fatalf("surface=%#v handled=%v err=%v", surface, handled, err)
	}
	if !strings.HasPrefix(surface.scope.RootTaskID, "semantic-root:") || !strings.HasPrefix(surface.scope.TurnID, "semantic-turn:") {
		t.Fatalf("scope=%+v", surface.scope)
	}
	if !strings.HasPrefix(surface.scope.SessionID, "semantic-session:") || surface.scope.PrincipalID != "user-1" {
		t.Fatalf("session/principal identity mixed: scope=%+v", surface.scope)
	}
}

func TestSemanticCallSurfaceUsesTrustedLoopSessionInsteadOfPrincipal(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticTestClassifier(t)}
	if err := h.registry.Register(RegisteredTool{
		Name: "screenshot", Status: RegToolAvailable,
		InputSchema: map[string]interface{}{"display": map[string]interface{}{"type": "integer"}},
		CapabilityProvisions: []tool.CapabilityProvision{{
			Capability: "visual.capture.desktop", Qualifiers: map[string]string{"display": "primary"}, Quality: 1,
		}},
		SemanticEffects:  []tool.EffectClass{tool.EffectReadOnly},
		SemanticProduces: []tool.ArtifactContract{{Kind: "image", MIMEType: "image/png", Required: true}},
		Handler:          func(map[string]interface{}) string { return "captured" },
	}); err != nil {
		t.Fatalf("register screenshot: %v", err)
	}
	ctx := &LoopContext{ID: "stable-loop", SessionID: "trusted-session", Runtime: RuntimeContext{RequestID: "request-8"}}
	_, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithContext(ctx, "user-1", "capture primary screen", "lansenger")
	if err != nil || !handled || surface == nil {
		t.Fatalf("surface=%#v handled=%v err=%v", surface, handled, err)
	}
	if surface.scope.SessionID != "trusted-session" || surface.scope.PrincipalID != "user-1" {
		t.Fatalf("scope=%+v", surface.scope)
	}
}

func TestSemanticRoutingIdentityNeverDerivesTaskIdentityFromUserText(t *testing.T) {
	rootA, turnA := semanticRoutingIdentity(nil, "user-1", "capture primary screen")
	rootB, turnB := semanticRoutingIdentity(nil, "user-1", "capture primary screen")
	if !strings.HasPrefix(rootA, "adhoc:") || !strings.HasPrefix(turnA, "turn:") {
		t.Fatalf("unexpected ephemeral identity: root=%q turn=%q", rootA, turnA)
	}
	if rootA == rootB || turnA == turnB {
		t.Fatalf("identical user text must not create a reusable logical task: first=(%q,%q) second=(%q,%q)", rootA, turnA, rootB, turnB)
	}
	ctx := &LoopContext{ID: "stable-loop", Runtime: RuntimeContext{RequestID: "request-7"}}
	root, turn := semanticRoutingIdentity(ctx, "user-1", "anything")
	if !strings.HasPrefix(root, "semantic-root:") || !strings.HasPrefix(turn, "semantic-turn:") {
		t.Fatalf("loop identity must be host-issued: root=%q turn=%q", root, turn)
	}
	chatA := &LoopContext{ID: "chat", Kind: LoopKindChat}
	chatB := &LoopContext{ID: "chat", Kind: LoopKindChat}
	rootA, turnA = semanticRoutingIdentity(chatA, "user-1", "北京天气")
	rootB, turnB = semanticRoutingIdentity(chatB, "user-1", "天津天气")
	if !strings.HasPrefix(rootA, "semantic-root:") || rootA == rootB || turnA == turnB {
		t.Fatalf("chat turns without request id must not share lineage: first=(%q,%q) second=(%q,%q)", rootA, turnA, rootB, turnB)
	}
}

func TestSemanticRoutingIdentityDoesNotDeriveAuthorityFromRuntimeIDs(t *testing.T) {
	first := &LoopContext{
		ID: "reused-runtime-loop",
		Runtime: RuntimeContext{
			RequestID:    "request-shared",
			Conversation: RuntimeConversationRef{SessionKey: "trusted-conversation"},
		},
	}
	second := &LoopContext{
		ID: "reused-runtime-loop",
		Runtime: RuntimeContext{
			RequestID:    "request-shared",
			Conversation: RuntimeConversationRef{SessionKey: "trusted-conversation"},
		},
	}
	firstRoot, firstTurn := semanticRoutingIdentity(first, "user-a", "capture primary screen")
	secondRoot, secondTurn := semanticRoutingIdentity(second, "user-a", "capture primary screen")
	if firstRoot == secondRoot || firstTurn == secondTurn {
		t.Fatalf("runtime IDs must not mint a reusable semantic lineage: first=(%q,%q) second=(%q,%q)", firstRoot, firstTurn, secondRoot, secondTurn)
	}
	if got := semanticRoutingSessionID(first, firstTurn); !strings.HasPrefix(got, "semantic-session:") {
		t.Fatalf("generic conversation key must not become semantic session authority, got %q", got)
	}
}

func TestPrepareIMLoopContextReplacementRotatesSemanticInvocationIdentity(t *testing.T) {
	h := &IMMessageHandler{}
	ctx := NewLoopContext("chat", 1, nil)
	first := h.prepareIMLoopContext(ctx, IMUserMessage{
		UserID: "user-a", Platform: "desktop", RequestID: "request-a", Text: "capture primary screen",
	}, nil, false, false)
	firstRoot, firstTurn := semanticRoutingIdentity(first, "user-a", "capture primary screen")
	second := h.prepareIMLoopContext(ctx, IMUserMessage{
		UserID: "user-a", Platform: "desktop", RequestID: "request-b", Text: "current time",
	}, nil, false, false)
	secondRoot, secondTurn := semanticRoutingIdentity(second, "user-a", "current time")
	if firstRoot == secondRoot || firstTurn == secondTurn {
		t.Fatalf("replacement request retained semantic identity: first=(%q,%q) second=(%q,%q)", firstRoot, firstTurn, secondRoot, secondTurn)
	}
	if second.Runtime.RequestID != "request-b" {
		t.Fatalf("replacement runtime request id=%q", second.Runtime.RequestID)
	}
}

func TestSemanticDiagnosticDoesNotMaterializeGrant(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticTestClassifier(t)}
	if err := h.registry.Register(RegisteredTool{
		Name: "screenshot", Status: RegToolAvailable,
		InputSchema: map[string]interface{}{"display": map[string]interface{}{"type": "integer"}},
		CapabilityProvisions: []tool.CapabilityProvision{{
			Capability: "visual.capture.desktop", Qualifiers: map[string]string{"display": "primary"}, Quality: 1,
		}},
		SemanticEffects:  []tool.EffectClass{tool.EffectReadOnly},
		SemanticProduces: []tool.ArtifactContract{{Kind: "image", MIMEType: "image/png", Required: true}},
		Handler:          func(map[string]interface{}) string { return "captured" },
	}); err != nil {
		t.Fatalf("register screenshot: %v", err)
	}
	ctx := &LoopContext{ID: "stable-loop", Runtime: RuntimeContext{RequestID: "request-7"}}
	diagnostic := h.semanticRouteDiagnosticForTurnWithContext(ctx, "user-1", "capture primary screen", "lansenger", nil)
	if !diagnostic.Handled || diagnostic.PlanID == "" || diagnostic.Reason != "semantic_route_ready" {
		t.Fatalf("diagnostic=%+v", diagnostic)
	}
	prepared, handled, err := h.semanticPlanForTurn("user-1", "capture primary screen", "lansenger", "loop:stable-loop", "request:request-7")
	if !handled || err != nil || prepared == nil {
		t.Fatalf("prepared=%#v handled=%v err=%v", prepared, handled, err)
	}
	// Planning alone contains no issuer or model-callable token. Materialization
	// is the separate operation exercised by the preceding call-surface test.
	if strings.Contains(prepared.plan.ID, "invoke_") {
		t.Fatalf("shadow plan unexpectedly contains invocation token: %s", prepared.plan.ID)
	}
}

func TestAdvanceSemanticCallSurfaceDoesNotCreateCallerControlledBindings(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticTestClassifier(t)}
	if err := h.registry.Register(RegisteredTool{
		Name: "screenshot", Status: RegToolAvailable,
		InputSchema: map[string]interface{}{"display": map[string]interface{}{"type": "integer"}},
		CapabilityProvisions: []tool.CapabilityProvision{{
			Capability: "visual.capture.desktop", Qualifiers: map[string]string{"display": "primary"}, Quality: 1,
		}},
		SemanticEffects:  []tool.EffectClass{tool.EffectReadOnly},
		SemanticProduces: []tool.ArtifactContract{{Kind: "image", MIMEType: "image/png", Required: true}},
		Handler:          func(map[string]interface{}) string { return "captured" },
	}); err != nil {
		t.Fatalf("register screenshot: %v", err)
	}
	_, surface, handled, err := h.semanticCallSurfaceForSharedTurn("user-1", "capture primary screen", "lansenger")
	if err != nil || !handled || surface == nil || len(surface.grants) < 1 {
		t.Fatalf("surface=%#v handled=%v err=%v", surface, handled, err)
	}
	before := len(surface.grants)
	if _, err := advanceSemanticCallSurface(surface, "selection:need:visual.capture.desktop"); err != nil {
		t.Fatalf("advance surface: %v", err)
	}
	if len(surface.grants) != before {
		t.Fatalf("advance changed exposed bindings without a ready selection: %#v", surface.grants)
	}
}

func TestSemanticScreenshotRequiresArtifactDeliverySelection(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticTestClassifier(t)}
	if err := h.registry.Register(RegisteredTool{
		Name: "screenshot", Status: RegToolAvailable,
		InputSchema: map[string]interface{}{"display": map[string]interface{}{"type": "integer"}},
		CapabilityProvisions: []tool.CapabilityProvision{{
			Capability: "visual.capture.desktop", Qualifiers: map[string]string{"display": "primary"}, Quality: 1,
		}},
		SemanticEffects:  []tool.EffectClass{tool.EffectReadOnly},
		SemanticProduces: []tool.ArtifactContract{{Kind: "image", MIMEType: "image/png", Required: true}},
		Handler: func(map[string]interface{}) string {
			return "[screenshot_base64]" + testOnePixelPNGBase64
		},
	}); err != nil {
		t.Fatalf("register screenshot: %v", err)
	}
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurn("user-1", "capture the primary desktop screen", "lansenger")
	if err != nil || !handled || surface == nil || len(defs) < 1 {
		t.Fatalf("surface defs=%#v handled=%v surface=%#v err=%v", defs, handled, surface, err)
	}
	captureName := extractToolName(defs[0])
	cb := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface, platform: "lansenger", loopCtx: &LoopContext{DeliveryTarget: &agent.DeliveryTarget{ChannelScope: "lansenger", DestinationID: "user:user-1"}}}
	if got := cb.ExecuteTool(captureName, `{}`); got != toolPayloadPreparedMessage {
		t.Fatalf("capture result = %q", got)
	}
	if len(cb.tools) != 1 || extractToolName(cb.tools[0]) == captureName {
		t.Fatalf("completed capture remained in next tool surface: %#v", cb.tools)
	}
	if cb.semanticDeliveryImageKey != "" || cb.screenshotImageKey != "" {
		t.Fatalf("capture bypassed delivery selection: %+v", cb)
	}
	var deliveryName string
	for name, grant := range surface.grants {
		if grant.AdapterName == "semantic_deliver_current_image" {
			deliveryName = name
			break
		}
	}
	if deliveryName == "" {
		t.Fatalf("delivery selection was not materialized: %#v", surface.grants)
	}
	if got := cb.ExecuteTool(deliveryName, `{}`); !strings.Contains(got, "Delivery committed") {
		t.Fatalf("delivery result = %q", got)
	}
	selectionID := ""
	if grant, ok := surface.grants[deliveryName]; ok {
		selectionID = grant.SelectionID
	} else if grant, ok := surface.retiredGrants[deliveryName]; ok {
		selectionID = grant.SelectionID
	}
	if selectionID == "" {
		t.Fatalf("delivery selection identity was lost: grants=%#v retired=%#v", surface.grants, surface.retiredGrants)
	}
	if completed, err := surface.executor.Completed(surface.scope); err != nil || completed[selectionID] {
		t.Fatalf("prepared delivery became a completed selection: completed=%#v err=%v", completed, err)
	}
	record, err := surface.artifacts.store.Delivery(surface.scope, selectionID)
	if err != nil || record.State != tool.DeliveryPrepared || record.ArtifactID == "" {
		t.Fatalf("delivery was not durably prepared: record=%#v err=%v", record, err)
	}
	resp := &IMAgentResponse{ResponseSource: "shared_agent_loop"}
	attachSharedLoopArtifacts(resp, cb)
	if resp.ImageKey != testOnePixelPNGBase64 || resp.ResponseSource != imResponseSourceScreenshot.String() {
		t.Fatalf("planned delivery did not produce gateway artifact: %+v", resp)
	}
	if resp.SemanticDelivery == nil {
		t.Fatal("planned delivery lost its durable gateway outcome projection")
	}
	if _, dispatch, err := resp.SemanticDelivery.Store.ClaimDeliveryDispatch(resp.SemanticDelivery.Scope, resp.SemanticDelivery.SelectionID, time.Now().UTC()); err != nil || !dispatch {
		t.Fatalf("claim gateway delivery dispatch=%t err=%v", dispatch, err)
	}
	if err := resp.SemanticDelivery.recordOutcome(tool.DeliveryAccepted); err != nil {
		t.Fatalf("record gateway accepted outcome: %v", err)
	}
	accepted, err := surface.artifacts.store.Delivery(surface.scope, selectionID)
	if err != nil || accepted.State != tool.DeliveryAccepted {
		t.Fatalf("gateway outcome was not persisted: record=%#v err=%v", accepted, err)
	}
}

func TestSemanticScreenshotLocalRuntimePlatformUsesCanonicalChannelBinding(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticTestClassifier(t)}
	if err := h.registry.Register(RegisteredTool{
		Name: "screenshot", Status: RegToolAvailable,
		InputSchema: map[string]interface{}{"display": map[string]interface{}{"type": "integer"}},
		CapabilityProvisions: []tool.CapabilityProvision{{
			Capability: "visual.capture.desktop", Qualifiers: map[string]string{"display": "primary"}, Quality: 1,
		}},
		SemanticEffects:  []tool.EffectClass{tool.EffectReadOnly},
		SemanticProduces: []tool.ArtifactContract{{Kind: "image", MIMEType: "image/png", Required: true}},
		Handler:          func(map[string]interface{}) string { return "[screenshot_base64]" + testOnePixelPNGBase64 },
	}); err != nil {
		t.Fatalf("register screenshot: %v", err)
	}
	ctx := NewLoopContext("local-runtime", 3, nil)
	ctx.Runtime.SemanticIntent = &intent.ClassificationResult{Primary: intent.LabelScreenshot, Confidence: .98, Layer: 3}
	ctx.DeliveryTarget = &agent.DeliveryTarget{ChannelScope: "lansenger", DestinationID: "user:user-1"}
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithContext(ctx, "user-1", "截主屏", "lansenger_local")
	if err != nil || !handled || surface == nil || len(defs) < 1 {
		t.Fatalf("defs=%#v handled=%v surface=%#v err=%v", defs, handled, surface, err)
	}
	cb := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface, platform: "lansenger_local", loopCtx: ctx}
	if got := cb.ExecuteTool(extractToolName(defs[0]), `{}`); got != toolPayloadPreparedMessage {
		t.Fatalf("capture result = %q", got)
	}
	for name, grant := range surface.grants {
		if grant.AdapterName == "semantic_deliver_current_image" {
			if got := cb.ExecuteTool(name, `{}`); !strings.Contains(got, "Delivery committed") {
				t.Fatalf("delivery result = %q", got)
			}
			if cb.semanticDelivery == nil || cb.semanticDelivery.ChannelScope != "lansenger" {
				t.Fatalf("delivery projection = %#v", cb.semanticDelivery)
			}
			return
		}
	}
	t.Fatalf("delivery selection was not materialized: %#v", surface.grants)
}

func TestSemanticDeliveryConsumesOnlyItsPlannedProducerArtifact(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticTestClassifier(t)}
	if err := h.registry.Register(RegisteredTool{
		Name: "screenshot", Status: RegToolAvailable,
		InputSchema: map[string]interface{}{"display": map[string]interface{}{"type": "integer"}},
		CapabilityProvisions: []tool.CapabilityProvision{{
			Capability: "visual.capture.desktop", Qualifiers: map[string]string{"display": "primary"}, Quality: 1,
		}},
		SemanticEffects:  []tool.EffectClass{tool.EffectReadOnly},
		SemanticProduces: []tool.ArtifactContract{{Kind: "image", MIMEType: "image/png", Required: true}},
		Handler:          func(map[string]interface{}) string { return "[screenshot_base64]" + testOnePixelPNGBase64 },
	}); err != nil {
		t.Fatalf("register screenshot: %v", err)
	}
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurn("user-1", "capture primary desktop", "lansenger")
	if err != nil || !handled || len(defs) < 1 {
		t.Fatalf("surface defs=%#v handled=%v err=%v", defs, handled, err)
	}
	captureName := extractToolName(defs[0])
	cb := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface, platform: "lansenger", loopCtx: &LoopContext{DeliveryTarget: &agent.DeliveryTarget{ChannelScope: "lansenger", DestinationID: "user:user-1"}}}
	if got := cb.ExecuteTool(captureName, `{}`); got != toolPayloadPreparedMessage {
		t.Fatalf("capture result=%q", got)
	}
	var deliveryName string
	var delivery tool.PlannedSelection
	for functionName, grant := range surface.grants {
		if grant.AdapterName == "semantic_deliver_current_image" {
			deliveryName = functionName
			for _, selection := range surface.plan.Selections {
				if selection.ID == grant.SelectionID {
					delivery = selection
				}
			}
		}
	}
	if deliveryName == "" || len(delivery.ArtifactDependencies) != 1 {
		t.Fatalf("delivery binding was not materialized: name=%q selection=%+v", deliveryName, delivery)
	}
	foreign, err := tool.NewArtifactPayload(surface.scope, "selection:foreign-capture", "image", "image/png", testOnePixelPNGBase64, time.Now().UTC().Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	foreignRef, err := surface.artifacts.store.Publish(foreign)
	if err != nil {
		t.Fatal(err)
	}
	if err := surface.artifacts.registerPublished(foreignRef); err == nil {
		t.Fatal("route state accepted an artifact from an unplanned producer")
	}
	if got := cb.ExecuteTool(deliveryName, `{}`); !strings.Contains(got, "Delivery committed") {
		t.Fatalf("delivery result=%q", got)
	}
	if cb.semanticDeliveryImageKey != testOnePixelPNGBase64 {
		t.Fatalf("delivery chose wrong artifact=%q", cb.semanticDeliveryImageKey)
	}
}

func TestRecoveredSemanticSurfaceHidesDeliveryAwaitingReceipt(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	defer app.closeSemanticInvocationStore()
	h := &IMMessageHandler{app: app, registry: NewToolRegistry(), unifiedClassifier: semanticTestClassifier(t)}
	if err := h.registry.Register(RegisteredTool{
		Name: "screenshot", Status: RegToolAvailable,
		InputSchema:          map[string]interface{}{"display": map[string]interface{}{"type": "integer"}},
		CapabilityProvisions: []tool.CapabilityProvision{{Capability: "visual.capture.desktop", Qualifiers: map[string]string{"display": "primary"}, Quality: 1}},
		SemanticEffects:      []tool.EffectClass{tool.EffectReadOnly}, SemanticProduces: []tool.ArtifactContract{{Kind: "image", MIMEType: "image/png", Required: true}},
		Handler: func(map[string]interface{}) string { return "[screenshot_base64]" + testOnePixelPNGBase64 },
	}); err != nil {
		t.Fatal(err)
	}
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentity("user-1", "capture primary desktop", "lansenger", "root:receipt", "turn:receipt")
	if err != nil || !handled || len(defs) < 1 {
		t.Fatalf("initial surface defs=%#v handled=%v err=%v", defs, handled, err)
	}
	cb := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface, platform: "lansenger", loopCtx: &LoopContext{DeliveryTarget: &agent.DeliveryTarget{ChannelScope: "lansenger", DestinationID: "user:user-1"}}}
	if got := cb.ExecuteTool(extractToolName(defs[0]), `{}`); got != toolPayloadPreparedMessage {
		t.Fatalf("capture result=%q", got)
	}
	var deliveryName string
	for name, grant := range surface.grants {
		if grant.AdapterName == "semantic_deliver_current_image" {
			deliveryName = name
		}
	}
	if deliveryName == "" {
		t.Fatal("delivery selection was not materialized")
	}
	// Exercise the production call path, which records the provider call ID in
	// the host journal before invoking the semantic executor. Receipt-bound
	// effects must retire this opaque function even though they cannot mark the
	// DAG selection complete yet.
	if got := cb.ExecuteToolCall(deliveryName, `{}`, "call:receipt-bound-delivery").Result; !strings.Contains(got, "Delivery committed") {
		t.Fatalf("delivery did not prepare: %q", got)
	}
	selectionID := surface.retiredGrants[deliveryName].SelectionID
	if record, err := surface.executor.Execution(surface.scope, selectionID); err != nil || record.State != tool.PlanExecutionAwaitingReceipt {
		t.Fatalf("execution record=%#v err=%v", record, err)
	}
	defs, recovered, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentity("user-1", "capture primary desktop", "lansenger", "root:receipt", "turn:receipt")
	if err != nil || !handled || len(defs) != 0 || recovered == nil {
		t.Fatalf("recovered defs=%#v handled=%v surface=%#v err=%v", defs, handled, recovered, err)
	}
	if _, stillExposed := recovered.grants[deliveryName]; stillExposed {
		t.Fatalf("awaiting receipt delivery remained exposed=%#v", recovered.grants)
	}
}

func TestSemanticDynamicMCPUsesOpaqueBoundCatalogWithoutRequestStartup(t *testing.T) {
	base := t.TempDir()
	app := &App{testHomeDir: base}
	defer app.closeSemanticInvocationStore()
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	cfg.LocalMCPServers = []corelib.LocalMCPServerEntry{{
		ID: "semantic-local", Name: "untrusted server name", Command: os.Args[0],
		Args: []string{"-test.run=TestLocalMCPHelperProcess", "--", "helper-mcp"}, Env: map[string]string{"GO_WANT_HELPER_PROCESS": "1"},
	}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	app.mcpRegistry = NewMCPRegistry(app)
	h := &IMMessageHandler{app: app, registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelLiveData)}
	// Planning must not construct or start a local provider on demand. The
	// host web-search adapter may satisfy live_data while MCP is still dark.
	_, surface, handled, err := h.semanticCallSurfaceForSharedTurn("user-1", "current information", "desktop")
	if err != nil || !handled || surface == nil {
		t.Fatalf("before lifecycle start: handled=%v surface=%#v err=%v", handled, surface, err)
	}
	if surface.plan.Selections[0].Provider.Kind != "builtin" {
		t.Fatalf("before lifecycle start selected %q", surface.plan.Selections[0].Provider.Kind)
	}
	if app.localMCPManager != nil {
		t.Fatal("semantic planning constructed a local MCP manager")
	}

	app.ensureLocalMCPManager()
	defer app.localMCPManager.StopAll()
	app.localMCPManager.SyncFromConfig()
	tools := app.localMCPManager.GetAllTools()
	if len(tools) != 1 || len(tools[0].Tools) != 1 {
		t.Fatalf("lifecycle did not publish local tool inventory: %#v", tools)
	}
	principal := agentservice.Principal{TenantID: semanticDesktopTenantID(), UserID: "user-1"}
	contracts, err := app.semanticDynamicCapabilityContractsForApp()
	if err != nil {
		t.Fatal(err)
	}
	contract := agentservice.DynamicCapabilityContract{
		Provisions:            []tool.CapabilityProvision{{Capability: "information.search.web", Qualifiers: map[string]string{"freshness": "current"}, Quality: 3}},
		Effects:               []tool.EffectClass{tool.EffectReadOnly},
		ObservedBindingDigest: agentservice.DynamicMCPObservedBindingDigest("semantic-local", "ping", tools[0].Tools[0].InputSchema),
	}
	if err := contracts.PublishMCPContract(principal, "semantic-local", "ping", contract); err != nil {
		t.Fatal(err)
	}
	inventory, err := h.semanticDynamicInventory(context.Background(), "user-1")
	if err != nil || len(inventory.mcpEntries) != 1 || inventory.mcpEntries[0].Contract.Digest() != contract.Digest() {
		t.Fatalf("published MCP inventory=%#v coverage=%+v err=%v", inventory.mcpEntries, inventory.coverage, err)
	}
	dynamicCatalog, err := agentservice.BuildDynamicSemanticCatalog(inventory.mcpEntries, inventory.skillEntries)
	if err != nil || len(dynamicCatalog.Providers) != 1 {
		t.Fatalf("dynamic catalog providers=%#v err=%v", dynamicCatalog.Providers, err)
	}
	if p := dynamicCatalog.Providers[0]; len(p.Provides) != 1 || p.Provides[0].Capability != "information.search.web" || p.Provides[0].Qualifiers["freshness"] != "current" || len(p.ChannelScopes) != 0 {
		t.Fatalf("dynamic provider projection=%+v", p)
	}
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurn("user-1", "current information", "desktop")
	if err != nil || !handled || surface == nil || len(defs) < 1 {
		t.Fatalf("dynamic surface defs=%#v handled=%v surface=%#v err=%v", defs, handled, surface, err)
	}
	var dynamicDef map[string]interface{}
	for _, def := range defs {
		if strings.HasPrefix(extractToolName(def), "invoke_") {
			dynamicDef = def
		}
	}
	if dynamicDef == nil {
		t.Fatalf("dynamic MCP def missing: %#v", defs)
	}
	name := extractToolName(dynamicDef)
	if strings.Contains(name, "ping") || strings.Contains(dynamicDef["function"].(map[string]interface{})["description"].(string), "untrusted") {
		t.Fatalf("dynamic MCP leaked identity: %#v", dynamicDef)
	}
	parameters := dynamicDef["function"].(map[string]interface{})["parameters"].(map[string]interface{})
	if _, leaked := parameters["server_id"]; leaked {
		t.Fatalf("dynamic MCP leaked server selector: %#v", parameters)
	}
	callback := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface}
	if got := callback.ExecuteTool("call_mcp_tool", `{"server_id":"forged","tool":"ping"}`); !strings.Contains(got, "selection_not_authorized") {
		t.Fatalf("free gateway was available: %q", got)
	}
	if err := contracts.RevokeMCPContract(principal, "semantic-local", "ping"); err != nil {
		t.Fatal(err)
	}
	if got := callback.ExecuteTool(name, `{}`); !strings.Contains(got, "dynamic_binding_stale") {
		t.Fatalf("revoked dynamic MCP executed: %q", got)
	}
	defs, surface, handled, err = h.semanticCallSurfaceForSharedTurn("user-1", "current information", "desktop")
	if err != nil || !handled || surface == nil {
		t.Fatalf("revoked MCP must still plan: handled=%v err=%v surface=%#v", handled, err, surface)
	}
	searchSelection, found := semanticSelectionForCapability(surface.plan, "information.search.web")
	if !found || searchSelection.Provider.Kind != "builtin" {
		t.Fatalf("revoked MCP must fall back to host search: selection=%+v found=%v", searchSelection, found)
	}
}

func TestSemanticBoundLocalMCPExecutionUsesPrincipalScopedClient(t *testing.T) {
	base := t.TempDir()
	app := &App{testHomeDir: base}
	defer app.closeSemanticInvocationStore()
	config, err := app.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	config.LocalMCPServers = []corelib.LocalMCPServerEntry{{
		ID: "semantic-owner-local", Name: "owner scoped", Command: os.Args[0],
		Args: []string{"-test.run=TestLocalMCPHelperProcess", "--", "helper-mcp"}, Env: map[string]string{"GO_WANT_HELPER_PROCESS": "1"},
	}}
	if err := app.SaveConfig(config); err != nil {
		t.Fatal(err)
	}
	app.mcpRegistry = NewMCPRegistry(app)
	app.ensureLocalMCPManager()
	defer app.localMCPManager.StopAll()
	app.localMCPManager.SyncFromConfig()
	observed := app.localMCPManager.GetAllTools()
	if len(observed) != 1 || len(observed[0].Tools) != 1 {
		t.Fatalf("lifecycle inventory=%#v", observed)
	}
	principal := agentservice.Principal{TenantID: semanticDesktopTenantID(), UserID: "semantic-owner-a"}
	bridge := guiSemanticMCPBridge{handler: &IMMessageHandler{app: app}}
	binding, err := agentservice.BindMCPTool([]agentservice.MCPToolEntry{{
		ServerID: "semantic-owner-local", ToolName: "ping", InputSchema: observed[0].Tools[0].InputSchema,
		Contract: agentservice.DynamicCapabilityContract{Provisions: []tool.CapabilityProvision{{Capability: "information.search.web", Quality: 1}}, Effects: []tool.EffectClass{tool.EffectReadOnly}},
	}}, "semantic-owner-local", "ping")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bridge.CallBoundTool(context.Background(), principal, binding, nil); err != nil {
		t.Fatalf("bound local MCP call: %v", err)
	}
	app.localMCPManager.mu.RLock()
	byOwner := app.localMCPManager.ownerClients["semantic-owner-local"]
	ownerClient := byOwner[principal.UserID]
	sharedClient := app.localMCPManager.clients["semantic-owner-local"]
	app.localMCPManager.mu.RUnlock()
	if ownerClient == nil || ownerClient == sharedClient {
		t.Fatalf("bound local MCP did not use a principal-scoped client: owner=%p shared=%p", ownerClient, sharedClient)
	}
}

func TestSemanticDynamicBindingStalePublishesRestrictedReplacementRevision(t *testing.T) {
	base := t.TempDir()
	app := &App{testHomeDir: base}
	defer app.closeSemanticInvocationStore()
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	cfg.LocalMCPServers = []corelib.LocalMCPServerEntry{{
		ID: "semantic-replan-local", Name: "untrusted replacement source", Command: os.Args[0],
		Args: []string{"-test.run=TestLocalMCPHelperProcess", "--", "helper-mcp"}, Env: map[string]string{"GO_WANT_HELPER_PROCESS": "1"},
	}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	app.mcpRegistry = NewMCPRegistry(app)
	app.ensureLocalMCPManager()
	defer app.localMCPManager.StopAll()
	app.localMCPManager.SyncFromConfig()
	observed := app.localMCPManager.GetAllTools()
	if len(observed) != 1 || len(observed[0].Tools) != 1 {
		t.Fatalf("lifecycle inventory=%#v", observed)
	}

	registry := NewToolRegistry()
	if err := registry.Register(RegisteredTool{
		Name: "bounded_fallback_lookup", Status: RegToolAvailable,
		InputSchema:          map[string]interface{}{},
		CapabilityProvisions: []tool.CapabilityProvision{{Capability: "information.search.web", Qualifiers: map[string]string{"freshness": "current"}, Quality: 2.5}},
		SemanticEffects:      []tool.EffectClass{tool.EffectReadOnly},
		Handler:              func(map[string]interface{}) string { return "bounded fallback result" },
	}); err != nil {
		t.Fatal(err)
	}
	h := &IMMessageHandler{app: app, registry: registry, unifiedClassifier: semanticClassifierForLabel(t, intent.LabelLiveData)}
	principal := agentservice.Principal{TenantID: semanticDesktopTenantID(), UserID: "user-1"}
	contracts, err := app.semanticDynamicCapabilityContractsForApp()
	if err != nil {
		t.Fatal(err)
	}
	if err := contracts.PublishMCPContract(principal, "semantic-replan-local", "ping", agentservice.DynamicCapabilityContract{
		Provisions:            []tool.CapabilityProvision{{Capability: "information.search.web", Qualifiers: map[string]string{"freshness": "current"}, Quality: 3}},
		Effects:               []tool.EffectClass{tool.EffectReadOnly},
		ObservedBindingDigest: agentservice.DynamicMCPObservedBindingDigest("semantic-replan-local", "ping", observed[0].Tools[0].InputSchema),
	}); err != nil {
		t.Fatal(err)
	}
	defs, parent, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "current information", "desktop", "root-dynamic-replan", "turn-dynamic-replan",
		&intent.ClassificationResult{Primary: intent.LabelLiveData, Confidence: .98},
	)
	if err != nil || !handled || parent == nil || len(defs) < 1 {
		t.Fatalf("parent defs=%#v handled=%v parent=%#v err=%v", defs, handled, parent, err)
	}
	parentName := ""
	for _, def := range defs {
		if strings.HasPrefix(extractToolName(def), "invoke_") {
			parentName = extractToolName(def)
		}
	}
	if parentName == "" {
		t.Fatalf("dynamic parent def missing: %#v", defs)
	}
	parentGrant := parent.grants[parentName]
	parentSelection, ok := semanticSelectionByID(parent.plan, parentGrant.SelectionID)
	if !ok || parentSelection.Provider.Kind != "mcp" {
		t.Fatalf("expected dynamic parent selection=%+v", parentSelection)
	}

	// Revocation is a lifecycle/control-plane event; it does not provide a
	// model retry right. The same governed need remains satisfiable only by the
	// already-registered lower-quality fallback capability.
	if err := contracts.RevokeMCPContract(principal, "semantic-replan-local", "ping"); err != nil {
		t.Fatal(err)
	}
	callback := &sharedAgentLoopCallbacks{handler: h, semanticSurface: parent, tools: defs}
	if got := callback.ExecuteToolCall(parentName, `{}`, "call-dynamic-stale").Result; !strings.Contains(got, "dynamic_binding_stale") {
		t.Fatalf("stale call result=%q", got)
	}
	child := callback.semanticSurface
	if child != nil && child != parent && child.replan != nil && child.replan.Attempts == 1 {
		if child.scope.RootTaskID != parent.scope.RootTaskID || child.scope.SessionID != parent.scope.SessionID || child.scope.PrincipalID != parent.scope.PrincipalID || child.scope.PlanID == parent.scope.PlanID {
			t.Fatalf("child scope=%+v parent=%+v", child.scope, parent.scope)
		}
		if err := parent.routeState.IsCurrent(parent.scope); err == nil || !strings.Contains(err.Error(), "superseded") {
			t.Fatalf("parent revision remained executable: %v", err)
		}
		childSelection, ok := semanticSelectionForCapability(child.plan, "information.search.web")
		if !ok || childSelection.Provider.Kind != "builtin" || childSelection.AdapterName != "bounded_fallback_lookup" {
			t.Fatalf("child widened or retained stale provider: %+v", childSelection)
		}
		childName := ""
		for grantName, grant := range child.grants {
			if grant.SelectionID == childSelection.ID {
				childName = grantName
			}
		}
		if childName == "" || semanticDefForGrantName(callback.tools, childName) == nil {
			t.Fatalf("child exposure tools=%#v grants=%#v", callback.tools, child.grants)
		}
		if childName == parentName || callback.ExecuteToolCall(parentName, `{}`, "call-old-function").Outcome != agent.ToolExecutionOutcomeError {
			t.Fatalf("retired parent function remained usable: old=%q child=%q", parentName, childName)
		}
		return
	}
	// Host web-search is a complete builtin with a wider schema than the MCP
	// ping contract. Binding-only replan may refuse that widening; the stale
	// grant must still stay dead and a fresh plan must not revive the revoked MCP.
	if got := callback.ExecuteToolCall(parentName, `{}`, "call-old-function").Result; !strings.Contains(got, "dynamic_binding_stale") && !strings.Contains(got, "selection_not_authorized") && !strings.Contains(got, "invocation_grant_replayed") {
		t.Fatalf("stale parent remained executable: %q", got)
	}
	freshDefs, fresh, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "current information", "desktop", "root-dynamic-replan-fresh", "turn-dynamic-replan-fresh",
		&intent.ClassificationResult{Primary: intent.LabelLiveData, Confidence: .98},
	)
	if err != nil || !handled || fresh == nil || len(freshDefs) == 0 {
		t.Fatalf("fresh plan after revoke defs=%#v handled=%v err=%v", freshDefs, handled, err)
	}
	freshSelection, ok := semanticSelectionForCapability(fresh.plan, "information.search.web")
	if !ok || freshSelection.Provider.Kind == "mcp" {
		t.Fatalf("revoked MCP remained selected: %+v", freshSelection)
	}
}

func TestSemanticDynamicSkillIsQuarantinedWithoutReviewedBinding(t *testing.T) {
	base := t.TempDir()
	app := &App{testHomeDir: base}
	defer app.closeSemanticInvocationStore()
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	cfg.NLSkills = []corelib.NLSkillEntry{{SkillID: "test.skill", Name: "untrusted dynamic skill", Status: "active", Version: "1.0.0", Steps: []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "echo safe"}}}}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	h := &IMMessageHandler{app: app, registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelLiveData)}
	_, surface, handled, err := h.semanticCallSurfaceForSharedTurn("user-1", "current information", "desktop")
	if err != nil || !handled || surface == nil {
		t.Fatalf("unreviewed skill planning handled=%v surface=%#v err=%v", handled, surface, err)
	}
	for _, selection := range surface.plan.Selections {
		if selection.Provider.Kind == "skill" {
			t.Fatalf("unreviewed skill was routable: %+v", selection)
		}
	}
	entry := app.skillExecutor.loadSkills()[0]
	contracts, err := app.semanticDynamicCapabilityContractsForApp()
	if err != nil {
		t.Fatal(err)
	}
	principal := agentservice.Principal{TenantID: semanticDesktopTenantID(), UserID: "user-1"}
	if err := contracts.PublishSkillContract(principal, agentservice.DynamicSkillStableID(entry), agentservice.DynamicCapabilityContract{
		Provisions: []tool.CapabilityProvision{{Capability: "information.search.web", Qualifiers: map[string]string{"freshness": "current"}, Quality: 3}}, Effects: []tool.EffectClass{tool.EffectReadOnly},
		ObservedBindingDigest: agentservice.DynamicSkillObservedBindingDigest(agentservice.DynamicSkillStableID(entry), entry.Version, agentservice.DynamicSkillContentDigest(entry)),
	}); err != nil {
		t.Fatal(err)
	}
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurn("user-1", "current information", "desktop")
	if err != nil || !handled || surface == nil || len(defs) < 1 {
		t.Fatalf("reviewed skill surface defs=%#v handled=%v surface=%#v err=%v", defs, handled, surface, err)
	}
	var skillDef map[string]interface{}
	for _, def := range defs {
		if strings.HasPrefix(extractToolName(def), "invoke_") {
			skillDef = def
		}
	}
	if skillDef == nil {
		t.Fatalf("reviewed skill def missing: %#v", defs)
	}
	if name := extractToolName(skillDef); strings.Contains(name, "skill") || strings.Contains(skillDef["function"].(map[string]interface{})["description"].(string), "untrusted") {
		t.Fatalf("skill identity leaked: %#v", skillDef)
	}
	if _, ok := skillDef["function"].(map[string]interface{})["parameters"].(map[string]interface{})["skill_name"]; ok {
		t.Fatalf("skill selector leaked: %#v", skillDef)
	}
	_ = context.Background() // guards accidental future conversion of this test to a model-controlled context
}

func TestSemanticBoundSkillExecutesExactStableIdentityWithoutAliasDrift(t *testing.T) {
	base := t.TempDir()
	app := &App{testHomeDir: base}
	defer app.closeSemanticInvocationStore()
	config, err := app.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	config.NLSkills = []corelib.NLSkillEntry{
		{
			SkillID: "vendor.bound", Name: "reviewed skill", Status: "active", Version: "1",
			Steps: []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "echo bound"}}},
		},
		{
			SkillID: "vendor.alias", Name: "replacement skill", DirName: "reviewed skill", Status: "active", Version: "1",
			Steps: []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "echo replacement"}}},
		},
	}
	if err := app.SaveConfig(config); err != nil {
		t.Fatal(err)
	}
	executor := NewSkillExecutor(app, nil, nil)
	entries := executor.loadSkills()
	var bound corelib.NLSkillEntry
	for _, entry := range entries {
		if entry.SkillID == "vendor.bound" {
			bound = entry
		}
	}
	if bound.SkillID == "" {
		t.Fatalf("bound skill missing from loaded skills=%#v", entries)
	}
	output, err := executor.executeBoundSkill(
		agentservice.DynamicSkillStableID(bound), bound.Name, bound.Version, agentservice.DynamicSkillContentDigest(bound), nil,
	)
	if err != nil || !strings.Contains(output, "bound") || strings.Contains(output, "replacement") {
		t.Fatalf("bound output=%q err=%v", output, err)
	}
	updated, err := app.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if updated.NLSkills[0].UsageCount != 1 || updated.NLSkills[0].SuccessCount != 1 {
		t.Fatalf("bound stats=%+v", updated.NLSkills[0])
	}
	if updated.NLSkills[1].UsageCount != 0 || updated.NLSkills[1].SuccessCount != 0 {
		t.Fatalf("alias replacement received bound stats=%+v", updated.NLSkills[1])
	}
}

func TestSemanticBoundSkillRejectsSameStableIDReplacementDrift(t *testing.T) {
	base := t.TempDir()
	app := &App{testHomeDir: base}
	defer app.closeSemanticInvocationStore()
	config, err := app.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	config.NLSkills = []corelib.NLSkillEntry{{
		SkillID: "vendor.bound", Name: "reviewed skill", Status: "active", Version: "1",
		Steps: []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "echo original"}}},
	}}
	if err := app.SaveConfig(config); err != nil {
		t.Fatal(err)
	}
	executor := NewSkillExecutor(app, nil, nil)
	original := executor.loadSkills()[0]

	config.NLSkills[0].Version = "2"
	config.NLSkills[0].Steps[0].Params["command"] = "echo replacement"
	if err := app.SaveConfig(config); err != nil {
		t.Fatal(err)
	}
	executor.invalidateSkillCache()
	output, err := executor.executeBoundSkill(
		agentservice.DynamicSkillStableID(original), original.Name, original.Version, agentservice.DynamicSkillContentDigest(original), nil,
	)
	if err == nil || !strings.Contains(err.Error(), "skill_binding_stale") || output != "" {
		t.Fatalf("replacement executed output=%q err=%v", output, err)
	}
}

func TestSemanticMCPInventoryTreatsObservedEmptyServerAsComplete(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	defer app.closeSemanticInvocationStore()
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	cfg.MCPServers = []corelib.MCPServerEntry{{ID: "empty-server", Name: "empty", EndpointURL: "https://example.invalid/mcp"}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	app.mcpRegistry = NewMCPRegistry(app)
	app.mcpRegistry.mu.Lock()
	app.mcpRegistry.health["empty-server"] = &mcpHealthState{Status: mcpHealthStatusHealthy}
	app.mcpRegistry.toolsCache["empty-server"] = []MCPToolView{}
	app.mcpRegistry.mu.Unlock()
	h := &IMMessageHandler{app: app}
	inventory, err := h.semanticDynamicInventory(context.Background(), "user-1")
	if err != nil {
		t.Fatal(err)
	}
	coverage := inventory.coverage.ForProviderKind("mcp")
	if coverage.State != tool.CatalogCoverageComplete || len(inventory.mcpEntries) != 0 {
		t.Fatalf("observed empty MCP inventory=%#v coverage=%+v", inventory.mcpEntries, coverage)
	}
}

func registerSemanticCurrentWebLookup(t *testing.T, h *IMMessageHandler, result string) {
	t.Helper()
	if err := h.registry.Register(RegisteredTool{
		Name: "current_lookup", Status: RegToolAvailable,
		InputSchema: map[string]interface{}{"query": map[string]interface{}{"type": "string"}},
		Required:    []string{"query"},
		CapabilityProvisions: []tool.CapabilityProvision{{
			Capability: "information.search.web", Qualifiers: map[string]string{"freshness": "current"}, Quality: 1,
		}},
		SemanticEffects: []tool.EffectClass{tool.EffectReadOnly},
		Handler:         func(map[string]interface{}) string { return result },
	}); err != nil {
		t.Fatal(err)
	}
}

func TestSemanticLiveDataSecondTurnDoesNotInheritCompletedSearch(t *testing.T) {
	base := t.TempDir()
	app := &App{testHomeDir: base}
	h := &IMMessageHandler{app: app, registry: NewToolRegistry()}
	defer app.closeSemanticInvocationStore()
	h.semanticTrustedWebSearch = func(userID, query string) (string, error) {
		return "Public web results for \"" + query + "\" (1):\n\n1. Weather\n   https://example.com", nil
	}
	liveData := &intent.ClassificationResult{Primary: intent.LabelLiveData, Confidence: .98, Layer: 2}
	first := NewLoopContext("chat", 3, nil)
	first.Runtime.RequestID = "req-beijing"
	first.Runtime.SemanticIntent = liveData
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithContext(first, "user-weather-lineage", "北京天气", "desktop")
	if err != nil || !handled || surface == nil || len(defs) < 1 {
		t.Fatalf("first defs=%#v handled=%v err=%v", defs, handled, err)
	}
	name := semanticGrantNameForAdapter(surface, semanticTrustedWebSearchAdapter)
	if name != "web_search" {
		t.Fatalf("first turn search grant=%q, want web_search", name)
	}
	first.Runtime.Execution = ExecutionProfile{Layer: string(executionLayerLight), PromptProfile: "light"}
	cb := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface, loopCtx: first, tools: defs, systemPrompt: "light fence"}
	if !cb.IsToolAllowed(name) {
		t.Fatalf("light authorizer rejected granted lookup %q", name)
	}
	if filtered := agent.FilterToolDefinitionsByAuthorizer(cb, defs); semanticDefForGrantName(filtered, name) == nil {
		t.Fatalf("light authorizer stripped lookup: %#v", filtered)
	}
	consumedToken := ""
	if grant, ok := surface.grants[name]; ok {
		consumedToken = grant.Token
	}
	if consumedToken == "" {
		t.Fatal("first turn must start with a live search grant")
	}
	if got := cb.ExecuteTool(name, `{"query":"北京天气"}`); !strings.Contains(got, "Public web results") || !strings.Contains(got, "北京天气") {
		t.Fatalf("first search=%q", got)
	}
	// The consumed sibling's grant is retired from the active surface; the
	// family's optional ceiling siblings keep the stable name live under a
	// NEW grant, exactly as on a declared search turn (§4.2 max-budget rule).
	if grant, stillIssued := surface.grants[name]; stillIssued && grant.Token == consumedToken {
		t.Fatal("consumed lookup grant remained on the active surface")
	}
	if retired, ok := surface.retiredGrants[name]; !ok || retired.Token != consumedToken {
		t.Fatalf("consumed lookup grant was not retired: %#v", surface.retiredGrants)
	}
	if !cb.RefreshAfterToolExecution(name) {
		t.Fatal("consumed lookup must refresh so the loop drops the spent tool")
	}
	if cb.systemPrompt != "light fence" {
		t.Fatalf("light prompt was replaced: %q", cb.systemPrompt)
	}
	if cb.IsToolAllowed("write_file") {
		t.Fatal("ungranted tools were authorized after lookup")
	}
	second := NewLoopContext("chat", 3, nil)
	second.Runtime.RequestID = "req-tianjin"
	second.Runtime.SemanticIntent = liveData
	defs2, surface2, handled2, err2 := h.semanticCallSurfaceForSharedTurnWithContext(second, "user-weather-lineage", "天津天气", "desktop")
	if err2 != nil || !handled2 || surface2 == nil || len(defs2) == 0 {
		t.Fatalf("second live_data turn inherited completed search: defs=%#v handled=%v err=%v", defs2, handled2, err2)
	}
	if surface2.scope.RootTaskID == surface.scope.RootTaskID {
		t.Fatalf("chat turns shared lineage %q", surface.scope.RootTaskID)
	}
	name2 := semanticGrantNameForAdapter(surface2, semanticTrustedWebSearchAdapter)
	if name2 != "web_search" {
		t.Fatalf("second turn name=%q, want web_search", name2)
	}
	if surface2.grants[name2].Token == surface.retiredGrants[name].Token && surface.retiredGrants[name].Token != "" {
		t.Fatal("second turn reused the consumed grant token")
	}
	second.Runtime.Execution = ExecutionProfile{Layer: string(executionLayerLight), PromptProfile: "light"}
	cb2 := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface2, loopCtx: second, tools: defs2}
	if !cb2.IsToolAllowed(name2) {
		t.Fatalf("stable search name %q must be the live lookup", name2)
	}
	if got := cb2.ExecuteTool(name2, `{"query":"天津天气"}`); !strings.Contains(got, "天津天气") {
		t.Fatalf("stable search name did not run this turn's lookup: %q", got)
	}
}

func TestSemanticDegradedLiveDataLookupIsPlanned(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry()}
	registerSemanticCurrentWebLookup(t, h, "ok")
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "成都天气", "desktop", "root-degraded-live", "turn-degraded-live",
		&intent.ClassificationResult{Primary: intent.LabelLiveData, Confidence: 0.73, Layer: 2, Degraded: true, Reason: "embedding-only fallback"},
	)
	if err != nil || !handled || surface == nil || len(defs) == 0 {
		t.Fatalf("degraded live_data lookup should still plan: defs=%#v handled=%v err=%v", defs, handled, err)
	}
}

func TestSemanticDegradedLookupHintAboveFloorPlansWithoutLexical(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry()}
	registerSemanticCurrentWebLookup(t, h, "ok")
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "帮我看一下这个说法", "desktop", "root-hint-floor", "turn-hint-floor",
		&intent.ClassificationResult{Primary: intent.LabelLiveData, Confidence: 0.74, Layer: 2, Degraded: true, Reason: "embedding ambiguous; tree classification unavailable"},
	)
	if err != nil || !handled || surface == nil || len(defs) == 0 {
		t.Fatalf("hint ≥ 0.70 must plan lookup without weather/PDF markers: defs=%#v handled=%v err=%v", defs, handled, err)
	}
	for _, def := range defs {
		if extractToolName(def) == "generate_pdf" {
			t.Fatal("lookup hint must not mint generate")
		}
	}
}

func TestSemanticDegradedDocumentGenerateFallsThroughToChat(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry()}
	_, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "生成pdf报告", "desktop", "root-degraded-pdf", "turn-degraded-pdf",
		&intent.ClassificationResult{Primary: intent.LabelDocumentGenerate, Confidence: 0.73, Layer: 2, Degraded: true},
	)
	if err != nil || handled || surface != nil {
		t.Fatalf("degraded document_generate must fall through so the turn can continue: surface=%#v handled=%v err=%v", surface, handled, err)
	}
}

func TestSemanticPlanErrorBlocksSessionForEveryManagedPlanningFailure(t *testing.T) {
	if semanticPlanErrorBlocksSession(nil) {
		t.Fatal("nil must not block the session")
	}
	if !semanticPlanErrorBlocksSession(fmt.Errorf("semantic route has unmet needs: generate")) {
		t.Fatal("catalog miss must stay on the managed failure path")
	}
	if !semanticPlanErrorBlocksSession(semanticUnmetNeedsError{Unmet: []tool.UnmetNeed{{NeedID: "generate", ReasonCode: "no_provider"}}}) {
		t.Fatal("provider miss must not revive legacy tools")
	}
	if !semanticPlanErrorBlocksSession(fmt.Errorf("trusted_document_input_ambiguous")) {
		t.Fatal("ambiguous trusted document input must not fall back to legacy local-file tools")
	}
	if !semanticPlanErrorBlocksSession(errSemanticAwaitingConfirmation) {
		t.Fatal("confirmation must still pause the turn")
	}
	if !semanticPlanErrorBlocksSession(fmt.Errorf("wrap: %w", errSemanticGenerateDeliveryConflict)) {
		t.Fatal("grant conflict must still pause the turn")
	}
	if !semanticPlanErrorBlocksSession(semanticUnmetNeedsError{Unmet: []tool.UnmetNeed{{NeedID: "generate", ReasonCode: "policy_denied"}}}) {
		t.Fatal("channel/policy deny must still pause the turn")
	}
	if !semanticPlanErrorBlocksSession(semanticUnmappedCapabilityError{Label: intent.LabelWorkflowTask}) {
		t.Fatal("workflow_task must keep the workflow entry message")
	}
	if !semanticPlanErrorBlocksSession(semanticUnmappedCapabilityError{Label: intent.LabelCoding}) {
		t.Fatal("unmapped or withdrawn labels must stay host-owned refusals")
	}
}

func TestManagedPlannerFailureNeverFallsBackToLegacyToolSurface(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry()}
	// This is intentionally a legacy-shaped tool with no reviewed capability
	// provision. A document-generate turn is nevertheless a governed family.
	// Its grant is not safe for a light prompt, so the final closed surface is
	// empty; that must produce a host-owned failure rather than re-entering the
	// name-based legacy router and exposing this definition.
	if err := h.registry.Register(RegisteredTool{
		Name:        "generate_pdf",
		Status:      RegToolAvailable,
		Description: "legacy PDF generator",
		InputSchema: map[string]interface{}{"content": map[string]interface{}{"type": "string"}},
		Required:    []string{"content"},
		Handler:     func(map[string]interface{}) string { return "legacy" },
	}); err != nil {
		t.Fatal(err)
	}
	ctx := NewLoopContext("managed-planner-failure", 3, nil)
	ctx.Runtime.SemanticIntent = &intent.ClassificationResult{Primary: intent.LabelDocumentGenerate, Confidence: .98}
	ctx.Runtime.Execution = ExecutionProfile{Layer: string(executionLayerLight), PromptProfile: "light"}
	state := h.prepareAgentLoopStartState(agentLoopStartOptions{
		Context: ctx, UserID: "user-1", UserText: "生成 PDF 报告", Platform: "desktop",
	})
	defer state.Cleanup()
	if state.HostReject == nil || state.HostReject.Error != "semantic_policy_denied" {
		t.Fatalf("managed planner failure must be host-rejected, state=%+v", state.HostReject)
	}
	if len(state.Tools) != 0 || len(state.BaseTools) != 0 {
		t.Fatalf("managed planner failure revived legacy tools: tools=%#v base=%#v", state.Tools, state.BaseTools)
	}
}

func TestClassifierProtocolViolationNeverBuildsLegacyOrZeroToolTurn(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry()}
	ctx := NewLoopContext("classifier-protocol-violation", 3, nil)
	ctx.Runtime.SemanticIntent = &intent.ClassificationResult{
		Primary: intent.LabelUnknown, Confidence: .30, Layer: 3, Degraded: true,
		ControlPlaneFailure: true,
	}
	state := h.prepareAgentLoopStartState(agentLoopStartOptions{
		Context: ctx, UserID: "user-1", UserText: "查询南京天气，并生成pdf报告", Platform: "desktop",
	})
	defer state.Cleanup()
	if state.HostReject == nil || state.HostReject.Error != "semantic_classifier_protocol_violation" {
		t.Fatalf("protocol failure must be host-rejected, state=%+v", state.HostReject)
	}
	if len(state.Tools) != 0 || len(state.BaseTools) != 0 || state.SemanticSurface != nil {
		t.Fatalf("protocol failure must not build a legacy tool surface: tools=%#v base=%#v surface=%#v", state.Tools, state.BaseTools, state.SemanticSurface)
	}
}

// A control-plane protocol failure must stop at the host boundary.  In
// particular, a zero-tool surface is not a valid reason to send an ordinary
// chat completion and hope the model recovers by itself.
func TestClassifierProtocolViolationBlocksActualLLMDispatch(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"unexpected model request"},"finish_reason":"stop"}]}`))
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{
		MaclawLLMProviders: []corelib.MaclawLLMProvider{{
			ID: "test", Name: "Test", URL: server.URL, Model: "test-model", Protocol: "openai",
		}},
		MaclawLLMCurrentProvider: "Test",
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	h := NewIMMessageHandler(app, &RemoteSessionManager{app: app, sessions: map[string]*RemoteSession{}})
	ctx := NewLoopContext("classifier-protocol-violation-dispatch", 3, server.Client())
	ctx.Runtime.SemanticIntent = &intent.ClassificationResult{
		Primary: intent.LabelUnknown, Confidence: .30, Layer: 3, Degraded: true,
		ControlPlaneFailure: true,
	}

	resp := h.runAgentLoop(ctx, "user-1", "system", nil, "查询南京天气，并生成pdf报告", nil, nil, nil, nil, nil, 0, "desktop")
	if resp == nil || resp.Error != "semantic_classifier_protocol_violation" {
		t.Fatalf("protocol failure must stop the agent loop before dispatch, resp=%+v", resp)
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("classifier protocol failure dispatched %d model requests, want 0", got)
	}
}

func TestRoutingMissRecoverAndInjectionKeepPrivilegeStripped(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry()}
	ctx := &LoopContext{
		Platform: "desktop",
		Runtime: RuntimeContext{
			SemanticIntent: &intent.ClassificationResult{Primary: intent.LabelDocumentGenerate, Confidence: 0.73, Layer: 2, Degraded: true},
		},
	}
	applySemanticRoutingMissFallback(ctx)
	if !ctx.Runtime.RoutingMissFallback || ctx.Runtime.HostAdapterLeftover {
		t.Fatal("fallback must set loop-scoped leftover flags")
	}
	leaky := []map[string]interface{}{
		{"function": map[string]interface{}{"name": "read_file"}},
		{"function": map[string]interface{}{"name": "edit_file"}},
		{"function": map[string]interface{}{"name": "download_file"}},
	}
	restored, _, _ := h.restoreToolsAfterSkillRecover("user-1", ctx, leaky, agentLoopPhase{})
	augmented, _ := h.finalizeInjectionAugmentedTools(ctx, "user-1", leaky)
	for _, got := range [][]map[string]interface{}{restored, augmented} {
		for _, def := range got {
			switch extractToolName(def) {
			case "edit_file", "download_file":
				t.Fatalf("leftover rebuild must not restore %s", extractToolName(def))
			}
		}
	}
}

func TestSemanticRoutingMissFallbackUnlocksLeftoverTools(t *testing.T) {
	ctx := &LoopContext{
		Platform: "desktop",
		Runtime: RuntimeContext{
			SemanticIntent: &intent.ClassificationResult{Primary: intent.LabelDocumentGenerate, Confidence: 0.73, Layer: 2, Degraded: true},
		},
	}
	applySemanticChatProjection(ctx)
	if !loopContextIsSemanticManaged(ctx) {
		t.Fatal("weak generate is not a chat projection and must still look managed before fallback")
	}
	applySemanticRoutingMissFallback(ctx)
	if loopContextIsSemanticManaged(ctx) {
		t.Fatal("routing miss fallback must clear the managed lock")
	}
	if !loopContextHasChatProjection(ctx) {
		t.Fatal("routing miss fallback must look like chat projection to the leftover router")
	}
	if loopContextHasHostAdapterLeftover(ctx) {
		t.Fatal("weak generate must not reopen the legacy document renderer")
	}
}

func TestRoutingMissLeftoverDropsPrivilegeAndGovernedGenerate(t *testing.T) {
	ctx := &LoopContext{
		Platform: "desktop",
		Runtime: RuntimeContext{
			SemanticIntent: &intent.ClassificationResult{Primary: intent.LabelDocumentGenerate, Confidence: 0.73, Layer: 2, Degraded: true},
		},
	}
	applySemanticRoutingMissFallback(ctx)
	routed := []map[string]interface{}{
		{"function": map[string]interface{}{"name": "read_file"}},
		{"function": map[string]interface{}{"name": "bash"}},
		{"function": map[string]interface{}{"name": "write_file"}},
		{"function": map[string]interface{}{"name": "download_file"}},
		{"function": map[string]interface{}{"name": "call_mcp_tool"}},
		{"function": map[string]interface{}{"name": "web_fetch"}},
	}
	all := append(append([]map[string]interface{}{}, routed...),
		map[string]interface{}{"function": map[string]interface{}{"name": "generate_pdf"}},
		map[string]interface{}{"function": map[string]interface{}{"name": "office"}},
	)
	got := applyRoutingMissLeftoverTools(routed, all, ctx)
	names := make(map[string]bool, len(got))
	for _, def := range got {
		names[extractToolName(def)] = true
	}
	for _, name := range []string{"download_file", "call_mcp_tool"} {
		if names[name] {
			t.Fatalf("routing miss must not expand privilege with %s", name)
		}
	}
	// bash and write_file are the guaranteed basic capability floor: a
	// routing-miss leftover turn must keep them so basic functionality
	// survives a degraded or offline planner.
	for _, name := range []string{"read_file", "web_fetch", "bash", "write_file"} {
		if !names[name] {
			t.Fatalf("routing miss must keep basic-floor tool %s: %v", name, names)
		}
	}
	if names["generate_pdf"] {
		t.Fatalf("routing miss must not expose governed generate_pdf: %v", names)
	}
	if names["office"] {
		t.Fatal("generate miss must not pin office; leftover lexical pins keep it when the user named a document")
	}
}

func TestRoutingMissDoesNotPinGenerateFromSecondaryLabel(t *testing.T) {
	ctx := &LoopContext{
		Platform: "desktop",
		Runtime: RuntimeContext{
			SemanticIntent: &intent.ClassificationResult{
				Primary:    intent.LabelLiveData,
				Secondary:  []intent.IntentLabel{intent.LabelDocumentGenerate},
				Confidence: 0.61,
				Degraded:   true,
			},
		},
	}
	applySemanticRoutingMissFallback(ctx)
	if loopContextHasHostAdapterLeftover(ctx) {
		t.Fatal("lookup+generate secondary must not pin the host generate adapter")
	}
}

func TestSemanticHostRejectResponseForPolicyDenied(t *testing.T) {
	resp := semanticHostRejectResponseForPlanError(semanticUnmetNeedsError{
		Unmet: []tool.UnmetNeed{{NeedID: "generate", ReasonCode: "policy_denied"}},
	})
	if resp == nil || resp.Error != "semantic_policy_denied" {
		t.Fatalf("policy deny must not use the catalog-uncovered copy: %#v", resp)
	}
}

func TestRoutingMissLeftoverDoesNotPinGenerateOnVE(t *testing.T) {
	ctx := &LoopContext{
		Platform: "ve_group_executor",
		Runtime: RuntimeContext{
			SemanticIntent: &intent.ClassificationResult{Primary: intent.LabelDocumentGenerate, Confidence: 0.73, Layer: 2, Degraded: true},
		},
	}
	applySemanticRoutingMissFallback(ctx)
	if loopContextHasHostAdapterLeftover(ctx) {
		t.Fatal("VE must not mark host adapter leftover")
	}
	got := applyRoutingMissLeftoverTools(
		[]map[string]interface{}{{"function": map[string]interface{}{"name": "read_file"}}},
		[]map[string]interface{}{
			{"function": map[string]interface{}{"name": "read_file"}},
			{"function": map[string]interface{}{"name": "generate_pdf"}},
		},
		ctx,
	)
	for _, def := range got {
		if extractToolName(def) == "generate_pdf" {
			t.Fatal("VE leftover must not pin generate_pdf")
		}
	}
}

func TestRoutingMissClearsStaleHostAdapterOnChatProjection(t *testing.T) {
	ctx := &LoopContext{
		Platform: "desktop",
		Runtime: RuntimeContext{
			RoutingMissFallback: true,
			HostAdapterLeftover: true,
			SemanticIntent: &intent.ClassificationResult{
				Primary: intent.LabelUnknown, Confidence: 0.30, Degraded: true,
				Reason: "chat projection; host adapter leftover",
			},
		},
	}
	applySemanticRoutingMissFallback(ctx)
	if !loopContextHasRoutingMissFallback(ctx) {
		t.Fatal("chat leftover must stay marked")
	}
	if loopContextHasHostAdapterLeftover(ctx) {
		t.Fatal("a later chat projection must not keep the previous generate pin")
	}
	got := applyRoutingMissLeftoverTools(
		[]map[string]interface{}{{"function": map[string]interface{}{"name": "memory"}}},
		[]map[string]interface{}{{"function": map[string]interface{}{"name": "generate_pdf"}}},
		ctx,
	)
	for _, def := range got {
		if extractToolName(def) == "generate_pdf" {
			t.Fatal("stale generate leftover must not pin generate_pdf")
		}
	}
}

func TestBindLoopSemanticIntentClearsLeftoverFlags(t *testing.T) {
	ctx := &LoopContext{Runtime: RuntimeContext{RoutingMissFallback: true, HostAdapterLeftover: true}}
	next := &intent.ClassificationResult{
		Primary: intent.LabelLiveData, Confidence: 0.61, Degraded: true,
		Reason: "embedding ambiguous; routing miss fallback; host adapter leftover",
	}
	bindLoopSemanticIntent(ctx, next)
	if ctx.Runtime.SemanticIntent != next || ctx.Runtime.RoutingMissFallback || ctx.Runtime.HostAdapterLeftover {
		t.Fatalf("new classification must drop leftover flags: %#v", ctx.Runtime)
	}
	if semanticIntentHasLeftoverReason(next) {
		t.Fatalf("bound classification must not keep leftover reason: %q", next.Reason)
	}
}

func TestRoutingMissUnknownIntentStillBoundsLeftover(t *testing.T) {
	ctx := &LoopContext{
		Platform: "desktop",
		Runtime: RuntimeContext{
			SemanticIntent: &intent.ClassificationResult{Primary: intent.LabelUnknown, Confidence: 0.40},
		},
	}
	applySemanticRoutingMissFallback(ctx)
	if !loopContextHasRoutingMissFallback(ctx) || !loopContextHasChatProjection(ctx) {
		t.Fatal("generic unknown must still unlock leftover")
	}
	if loopContextHasHostAdapterLeftover(ctx) {
		t.Fatal("generic unknown must not pin generate_pdf")
	}
	got := applyRoutingMissLeftoverTools(
		[]map[string]interface{}{
			{"function": map[string]interface{}{"name": "memory"}},
			{"function": map[string]interface{}{"name": "edit_file"}},
		},
		nil,
		ctx,
	)
	if len(got) != 1 || extractToolName(got[0]) != "memory" {
		t.Fatalf("unknown leftover must drop edit_file, got %#v", got)
	}
}

func TestRoutingMissWorkflowGenerateDoesNotReopenLegacyAdapter(t *testing.T) {
	ctx := &LoopContext{
		Platform:          "desktop",
		WorkflowAgentLoop: true,
		Runtime: RuntimeContext{
			SemanticIntent: &intent.ClassificationResult{Primary: intent.LabelDocumentGenerate, Confidence: 0.98},
		},
	}
	if loopContextIsSemanticManaged(ctx) {
		t.Fatal("workflow generate must stay unmanaged so stage PDFs can use leftover generate_pdf")
	}
	applySemanticRoutingMissFallback(ctx)
	if !loopContextHasRoutingMissFallback(ctx) || loopContextHasHostAdapterLeftover(ctx) {
		t.Fatal("workflow generate miss must remain a bounded non-authorizing fallback")
	}
	if !loopContextHasChatProjection(ctx) {
		t.Fatalf("workflow generate fallback must be projected to chat, got %#v", ctx.Runtime.SemanticIntent)
	}
	got := applyRoutingMissLeftoverTools(
		[]map[string]interface{}{
			{"function": map[string]interface{}{"name": "read_file"}},
			{"function": map[string]interface{}{"name": "bash"}},
		},
		[]map[string]interface{}{{"function": map[string]interface{}{"name": "generate_pdf"}}},
		ctx,
	)
	names := make(map[string]bool, len(got))
	for _, def := range got {
		names[extractToolName(def)] = true
	}
	if !names["bash"] || names["generate_pdf"] || !names["read_file"] {
		t.Fatalf("workflow generate fallback must strip generate_pdf but keep basic-floor bash/read_file: %v", names)
	}
}

func TestRoutingMissNilIntentStillBoundsLeftover(t *testing.T) {
	ctx := &LoopContext{Platform: "desktop"}
	applySemanticRoutingMissFallback(ctx)
	if !loopContextHasRoutingMissFallback(ctx) || !loopContextHasChatProjection(ctx) {
		t.Fatal("a plan miss with no classification must still unlock leftover")
	}
	if loopContextHasHostAdapterLeftover(ctx) {
		t.Fatal("unclassified leftover must not pin generate_pdf")
	}
	got := applyRoutingMissLeftoverTools(
		[]map[string]interface{}{
			{"function": map[string]interface{}{"name": "memory"}},
			{"function": map[string]interface{}{"name": "bash"}},
			{"function": map[string]interface{}{"name": "write_file"}},
			{"function": map[string]interface{}{"name": "edit_file"}},
		},
		[]map[string]interface{}{{"function": map[string]interface{}{"name": "generate_pdf"}}},
		ctx,
	)
	names := make(map[string]bool, len(got))
	for _, def := range got {
		names[extractToolName(def)] = true
	}
	// bash/write_file are the guaranteed basic floor and survive a nil-intent
	// miss; editors and governed publishers still do not leak.
	for _, name := range []string{"memory", "bash", "write_file"} {
		if !names[name] {
			t.Fatalf("nil-intent leftover must keep basic-floor %s, got %#v", name, got)
		}
	}
	if names["edit_file"] || names["generate_pdf"] {
		t.Fatalf("nil-intent leftover must drop edit_file/generate_pdf, got %#v", got)
	}
}

func TestLoopContextLeftoverRequiresThisTurnFlags(t *testing.T) {
	ctx := &LoopContext{
		Platform: "desktop",
		Runtime: RuntimeContext{
			SemanticIntent: &intent.ClassificationResult{
				Primary: intent.LabelUnknown, Confidence: 0.30, Degraded: true,
				Reason: "chat projection; routing miss fallback; host adapter leftover",
			},
		},
	}
	if loopContextHasRoutingMissFallback(ctx) || loopContextHasHostAdapterLeftover(ctx) {
		t.Fatal("stale leftover reason without this-turn flags must not unlock leftover tools")
	}
	got := applyRoutingMissLeftoverTools(
		[]map[string]interface{}{
			{"function": map[string]interface{}{"name": "memory"}},
			{"function": map[string]interface{}{"name": "bash"}},
		},
		[]map[string]interface{}{{"function": map[string]interface{}{"name": "generate_pdf"}}},
		ctx,
	)
	if len(got) != 2 {
		t.Fatalf("reason-only leftover must not strip or pin, got %#v", got)
	}
}

func TestRoutingMissChatProjectionAlsoStripsPrivilege(t *testing.T) {
	ctx := &LoopContext{Runtime: RuntimeContext{
		SemanticIntent: &intent.ClassificationResult{
			Primary: intent.LabelUnknown, Confidence: 0.30, Degraded: true, Reason: "chat projection",
		},
	}}
	applySemanticRoutingMissFallback(ctx)
	if !loopContextHasRoutingMissFallback(ctx) {
		t.Fatal("gate-7 chat projection must still be marked as a leftover miss")
	}
	got := applyRoutingMissLeftoverTools(
		[]map[string]interface{}{
			{"function": map[string]interface{}{"name": "memory"}},
			{"function": map[string]interface{}{"name": "edit_file"}},
		},
		nil,
		ctx,
	)
	if len(got) != 1 || extractToolName(got[0]) != "memory" {
		t.Fatalf("chat leftover must drop edit_file, got %#v", got)
	}
}

func TestSemanticWeakLiveDataLookupFallsThroughToChat(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry()}
	registerSemanticCurrentWebLookup(t, h, "ok")
	_, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "北京天所", "desktop", "root-weak-live", "turn-weak-live",
		&intent.ClassificationResult{
			Primary: intent.LabelLiveData, Confidence: 0.61, Layer: 2, Degraded: true,
			Reason: "embedding ambiguous; tree classification unavailable",
		},
	)
	if err != nil || handled || surface != nil {
		t.Fatalf("sub-floor live_data must fall through to chat: surface=%#v handled=%v err=%v", surface, handled, err)
	}
}

func TestSemanticWeakLookupRewritesLoopContextChatProjection(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry()}
	registerSemanticCurrentWebLookup(t, h, "ok")
	ctx := &LoopContext{Runtime: RuntimeContext{
		SemanticIntent: &intent.ClassificationResult{
			Primary: intent.LabelLiveData, Confidence: 0.61, Layer: 2, Degraded: true,
			Reason: "embedding ambiguous; short lookup skipped tree (l2=live_data conf=0.61)",
		},
	}}
	_, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithContextAndAttachments(ctx, "user-1", "北京天所", "desktop", nil)
	if err != nil || handled || surface != nil {
		t.Fatalf("sub-floor live_data must fall through to chat: surface=%#v handled=%v err=%v", surface, handled, err)
	}
	if ctx.Runtime.SemanticIntent == nil || ctx.Runtime.SemanticIntent.Primary != intent.LabelUnknown {
		t.Fatalf("gate 7 must rewrite LoopContext to chat projection, got %#v", ctx.Runtime.SemanticIntent)
	}
	if loopContextIsSemanticManaged(ctx) {
		t.Fatal("chat projection must not stay capability-managed")
	}
	if !strings.Contains(ctx.Runtime.SemanticIntent.Reason, "chat projection") {
		t.Fatalf("reason=%q, want chat projection marker", ctx.Runtime.SemanticIntent.Reason)
	}
	if !loopContextHasChatProjection(ctx) {
		t.Fatal("leftover router must see the chat-projection marker")
	}

	keep := &LoopContext{Runtime: RuntimeContext{
		SemanticIntent: &intent.ClassificationResult{Primary: intent.LabelLiveData, Confidence: 0.73, Layer: 2, Degraded: true},
	}}
	applySemanticChatProjection(keep)
	if keep.Runtime.SemanticIntent.Primary != intent.LabelLiveData {
		t.Fatal("hint at or above the lookup floor must not be projected")
	}
}

func TestSemanticDeclaredLookupGenerateComposite(t *testing.T) {
	composite := intent.ClassificationResult{
		Primary: intent.LabelLiveData, Secondary: []intent.IntentLabel{intent.LabelDocumentGenerate}, Confidence: 0.78,
	}
	if !semanticDeclaredLookupGenerateComposite(composite) || semanticReadOnlyLookupFamily(composite) {
		t.Fatal("weather+PDF must be a declared composite, not a read-only family")
	}
	if semanticNeedsChatProjection(composite) {
		t.Fatal("anchored weather+PDF must not use the chat projection")
	}
}

func TestSemanticUnknownWeatherDoesNotGainLookupFromWording(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry()}
	registerSemanticCurrentWebLookup(t, h, "ok")
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "北京天气", "desktop", "root-unknown-weather", "turn-unknown-weather",
		&intent.ClassificationResult{Primary: intent.LabelUnknown, Confidence: 0.30, Layer: 2, Degraded: true, Reason: "embedding ambiguous; tree classification unavailable (l2=live_data conf=0.61)"},
	)
	if err != nil || handled || surface != nil || len(defs) != 0 {
		t.Fatalf("weather wording must not promote an unknown degraded result: defs=%#v handled=%v surface=%#v err=%v", defs, handled, surface, err)
	}
}

func TestSemanticUnknownWeatherWithStagedImageFallsThrough(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry()}
	registerSemanticCurrentWebLookup(t, h, "ok")
	text := "这张图里的天气如何\n\n" + filePathPromptPrefix + "\nC:\\tmp\\scan.jpg"
	_, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassificationAndAttachments(
		"user-1", text, "desktop", "root-image-weather", "turn-image-weather",
		&intent.ClassificationResult{Primary: intent.LabelUnknown, Confidence: 0.30, Layer: 2, Degraded: true},
		[]MessageAttachment{{Type: "image", FileName: "scan.jpg", MimeType: "image/jpeg", Data: "trusted"}},
	)
	if err != nil || handled || surface != nil {
		t.Fatalf("unknown weather on a staged image must fall through to vision: surface=%#v handled=%v err=%v", surface, handled, err)
	}

	_, surface, handled, err = h.semanticCallSurfaceForSharedTurnWithIdentityAndClassificationAndAttachments(
		"user-1", "杭州天气，生成pdf报告", "desktop", "root-image-pdf", "turn-image-pdf",
		&intent.ClassificationResult{Primary: intent.LabelUnknown, Confidence: 0.30, Layer: 2, Degraded: true},
		[]MessageAttachment{{Type: "image", FileName: "scan.jpg", MimeType: "image/jpeg", Data: "trusted"}},
	)
	if err != nil || handled || surface != nil {
		t.Fatalf("unknown weather/pdf with an image attachment must fall through: surface=%#v handled=%v err=%v", surface, handled, err)
	}
}

func TestSemanticLiveDataLookupWithStagedImageFallsThrough(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry()}
	registerSemanticCurrentWebLookup(t, h, "ok")
	_, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassificationAndAttachments(
		"user-1", "这张图里的天气如何", "desktop", "root-image-livedata", "turn-image-livedata",
		&intent.ClassificationResult{Primary: intent.LabelLiveData, Confidence: 0.98, Layer: 2},
		[]MessageAttachment{{Type: "image", FileName: "scan.jpg", MimeType: "image/jpeg", Data: "trusted"}},
	)
	if err != nil || handled || surface != nil {
		t.Fatalf("confident live_data on a staged image must fall through to vision: surface=%#v handled=%v err=%v", surface, handled, err)
	}

	_, surface, handled, err = h.semanticCallSurfaceForSharedTurnWithIdentityAndClassificationAndAttachments(
		"user-1", "杭州天气，生成pdf报告", "desktop", "root-image-livedata-pdf", "turn-image-livedata-pdf",
		&intent.ClassificationResult{
			Primary:    intent.LabelLiveData,
			Secondary:  []intent.IntentLabel{intent.LabelDocumentGenerate},
			Confidence: 0.98, Layer: 3,
		},
		[]MessageAttachment{{Type: "image", FileName: "scan.jpg", MimeType: "image/jpeg", Data: "trusted"}},
	)
	if err != nil || handled || surface != nil {
		t.Fatalf("weather/pdf lookup on a staged image must fall through: surface=%#v handled=%v err=%v", surface, handled, err)
	}
}

func TestSemanticUnknownWeatherWithFailedStagedImageDoesNotGainLookup(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry()}
	registerSemanticCurrentWebLookup(t, h, "ok")
	text := "北京天气\n\n" + filePathPromptPrefix + "\nC:\\tmp\\missing.jpg\n[Host note: selected image \"missing.jpg\" could not be read: file does not exist]"
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", text, "desktop", "root-failed-image-weather", "turn-failed-image-weather",
		&intent.ClassificationResult{Primary: intent.LabelUnknown, Confidence: 0.30, Layer: 2, Degraded: true},
	)
	if err != nil || handled || surface != nil || len(defs) != 0 {
		t.Fatalf("failed picker load must not promote an unknown semantic result: defs=%#v handled=%v surface=%#v err=%v", defs, handled, surface, err)
	}
}

func TestStagedImageUnderstandDemotesManagedLiveDataRuntime(t *testing.T) {
	ctx := &LoopContext{Runtime: RuntimeContext{
		SemanticIntent: &intent.ClassificationResult{
			Primary: intent.LabelLiveData, Confidence: 0.98, Layer: 2, Reason: "embedding lookup",
		},
		Execution: ExecutionProfile{
			Layer: string(executionLayerLight), TaskType: "live_data", PromptProfile: "light",
			Confidence: 0.98, Reason: "semantic capability-managed lookup", ToolBudget: 1, IterationBudget: 3,
		},
	}}
	applyStagedImageUnderstandRuntime(ctx, "这张图里的天气如何", []MessageAttachment{
		{Type: "image", FileName: "scan.jpg", MimeType: "image/jpeg", Data: "trusted"},
	})
	if !ctx.Runtime.VisionFallthrough {
		t.Fatal("staged image lookup must mark vision fallthrough")
	}
	if loopContextIsSemanticManaged(ctx) {
		t.Fatal("demoted staged-image turn must not stay capability-managed")
	}
	if ctx.Runtime.SemanticIntent == nil || ctx.Runtime.SemanticIntent.Primary != intent.LabelUnknown {
		t.Fatalf("demoted intent = %#v, want unknown", ctx.Runtime.SemanticIntent)
	}
	if ctx.Runtime.Execution.IsLight() || ctx.Runtime.Execution.IsDirect() {
		t.Fatalf("vision fallthrough kept lookup profile %#v", ctx.Runtime.Execution)
	}

	plain := &LoopContext{Runtime: RuntimeContext{
		SemanticIntent: &intent.ClassificationResult{Primary: intent.LabelLiveData, Confidence: 0.98, Layer: 2},
		Execution: ExecutionProfile{
			Layer: string(executionLayerLight), TaskType: "live_data", PromptProfile: "light", Confidence: 0.98,
		},
	}}
	plain.Runtime.VisionFallthrough = true
	applyStagedImageUnderstandRuntime(plain, "北京天气", nil)
	if plain.Runtime.VisionFallthrough || !loopContextIsSemanticManaged(plain) {
		t.Fatal("weather without a staged image must stay a managed lookup")
	}
	if !plain.Runtime.Execution.IsLight() {
		t.Fatalf("weather without a staged image must keep the light lookup profile, got %#v", plain.Runtime.Execution)
	}

	prefixOnly := &LoopContext{Runtime: RuntimeContext{
		SemanticIntent: &intent.ClassificationResult{Primary: intent.LabelLiveData, Confidence: 0.98, Layer: 2},
		Execution: ExecutionProfile{
			Layer: string(executionLayerLight), TaskType: "live_data", PromptProfile: "light", Confidence: 0.98,
		},
	}}
	applyStagedImageUnderstandRuntime(prefixOnly, "北京天气\n\n"+filePathPromptPrefix+"\nC:\\tmp\\missing.jpg", nil)
	if prefixOnly.Runtime.VisionFallthrough {
		t.Fatal("a picker path is not a photo and must not skip the name router")
	}
	if !loopContextIsSemanticManaged(prefixOnly) {
		t.Fatal("pre-materialize picker must keep live_data so a failed load can still plan search")
	}
	if prefixOnly.Runtime.SemanticIntent == nil || prefixOnly.Runtime.SemanticIntent.Primary != intent.LabelLiveData {
		t.Fatalf("pre-materialize must not demote weather to unknown: %#v", prefixOnly.Runtime.SemanticIntent)
	}
	if prefixOnly.Runtime.Execution.IsLight() || prefixOnly.Runtime.Execution.IsDirect() {
		t.Fatalf("pending image staging still needs a full prompt, got %#v", prefixOnly.Runtime.Execution)
	}

	failed := &LoopContext{Runtime: RuntimeContext{
		SemanticIntent: &intent.ClassificationResult{Primary: intent.LabelLiveData, Confidence: 0.98, Layer: 2},
		Execution: ExecutionProfile{
			Layer: string(executionLayerLight), TaskType: "live_data", PromptProfile: "light", Confidence: 0.98,
		},
	}}
	failedText := "北京天气\n\n" + filePathPromptPrefix + "\nC:\\tmp\\missing.jpg\n[Host note: selected image \"missing.jpg\" could not be read: file does not exist]"
	applyStagedImageUnderstandRuntime(failed, failedText, nil)
	if failed.Runtime.VisionFallthrough {
		t.Fatal("failed picker load must not skip search")
	}
	if !loopContextIsSemanticManaged(failed) || failed.Runtime.SemanticIntent.Primary != intent.LabelLiveData {
		t.Fatalf("failed picker load must keep the weather lookup: %#v", failed.Runtime.SemanticIntent)
	}
}

func TestSemanticUnknownTypoFallsThroughToChat(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry()}
	registerSemanticCurrentWebLookup(t, h, "ok")
	_, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "北京天所", "desktop", "root-unknown-typo", "turn-unknown-typo",
		&intent.ClassificationResult{Primary: intent.LabelUnknown, Confidence: 0.30, Layer: 2, Degraded: true, Reason: "embedding ambiguous; tree classification unavailable (l2=live_data conf=0.61)"},
	)
	if err != nil || handled || surface != nil {
		t.Fatalf("unknown typo must fall through to chat: surface=%#v handled=%v err=%v", surface, handled, err)
	}
}

func TestSemanticWeakFileReadWithStagedImageFallsThrough(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry()}
	text := "图中有什么？\n\n" + filePathPromptPrefix + "\nC:\\Users\\ma139\\Pictures\\Camera Roll\\WIN_20260812_14_52_13_Scan.jpg"
	photo := []MessageAttachment{{Type: "image", FileName: "scan.jpg", MimeType: "image/jpeg", Data: "trusted"}}
	_, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassificationAndAttachments(
		"user-1", text, "desktop", "root-image-describe", "turn-image-describe",
		&intent.ClassificationResult{Primary: intent.LabelFileRead, Confidence: 0.69, Layer: 2, Degraded: true, Reason: "embedding ambiguous; tree classification unavailable"},
		photo,
	)
	if err != nil || handled || surface != nil {
		t.Fatalf("weak file_read on a staged image must fall through to vision/OCR: surface=%#v handled=%v err=%v", surface, handled, err)
	}

	_, surface, handled, err = h.semanticCallSurfaceForSharedTurnWithIdentityAndClassificationAndAttachments(
		"user-1", "图中有什么？", "desktop", "root-image-att", "turn-image-att",
		&intent.ClassificationResult{Primary: intent.LabelScreenshot, Confidence: 0.70, Layer: 2, Degraded: true},
		photo,
	)
	if err != nil || handled || surface != nil {
		t.Fatalf("weak screenshot guess on an attached image must fall through: surface=%#v handled=%v err=%v", surface, handled, err)
	}

	_, surface, handled, err = h.semanticCallSurfaceForSharedTurnWithIdentityAndClassificationAndAttachments(
		"user-1", text, "desktop", "root-image-fileread-hot", "turn-image-fileread-hot",
		&intent.ClassificationResult{Primary: intent.LabelFileRead, Confidence: 0.95, Layer: 3},
		photo,
	)
	if err != nil || handled || surface != nil {
		t.Fatalf("confident file_read on a staged image must still fall through: surface=%#v handled=%v err=%v", surface, handled, err)
	}

	_, surface, handled, err = h.semanticCallSurfaceForSharedTurnWithIdentityAndClassificationAndAttachments(
		"user-1", text, "desktop", "root-image-fileread-shot", "turn-image-fileread-shot",
		&intent.ClassificationResult{
			Primary: intent.LabelFileRead, Secondary: []intent.IntentLabel{intent.LabelScreenshot}, Confidence: 0.95, Layer: 3,
		},
		photo,
	)
	if err != nil || handled || surface != nil {
		t.Fatalf("file_read plus secondary screenshot must not force capture: surface=%#v handled=%v err=%v", surface, handled, err)
	}
}

func TestSemanticConfidentScreenshotWithStagedImageStillPlans(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry()}
	_, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassificationAndAttachments(
		"user-1", "capture the primary screen", "desktop", "root-shot-hot", "turn-shot-hot",
		&intent.ClassificationResult{Primary: intent.LabelScreenshot, Confidence: 0.98, Layer: 3},
		[]MessageAttachment{{Type: "image", FileName: "context.png", MimeType: "image/png", Data: "trusted"}},
	)
	if !handled || (surface == nil && err == nil) {
		t.Fatalf("confident screenshot must keep planning capture: surface=%#v handled=%v err=%v", surface, handled, err)
	}
}

func TestSemanticTurnHasHostStagedImageIgnoresMislabeledOffice(t *testing.T) {
	if semanticTurnHasHostStagedImage(filePathPromptPrefix+"\nC:\\tmp\\scan.jpg", nil) {
		t.Fatal("a picker path without image bytes is not a photo")
	}
	if semanticTurnHasHostStagedImage("看这个附件", []MessageAttachment{{
		Type: "image", FileName: "report.docx", MimeType: "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
	}}) {
		t.Fatal("office bytes labelled as image must not count as a staged raster")
	}
	if semanticTurnHasHostStagedImage("看这个附件", []MessageAttachment{{
		Type: "file", FileName: "scan.jpg", MimeType: "image/jpeg",
	}}) {
		t.Fatal("image MIME without bytes is not a photo")
	}
	if !semanticTurnHasHostStagedImage("看这个附件", []MessageAttachment{{
		Type: "file", FileName: "scan.jpg", MimeType: "image/jpeg", Data: "trusted",
	}}) {
		t.Fatal("image MIME with bytes must count as a visible photo")
	}
}

func TestSemanticReadOnlyGovernedPredicates(t *testing.T) {
	for _, testCase := range []struct {
		name       string
		result     intent.ClassificationResult
		lookup     bool
		understand bool
		governed   bool
	}{
		{
			name:     "pure search",
			result:   intent.ClassificationResult{Primary: intent.LabelSearch, Confidence: 0.80},
			lookup:   true,
			governed: true,
		},
		{
			name:       "document_read",
			result:     intent.ClassificationResult{Primary: intent.LabelDocumentRead, Confidence: 0.55},
			understand: true,
			governed:   true,
		},
		{
			name: "knowledge plus search",
			result: intent.ClassificationResult{
				Primary: intent.LabelKnowledgeRead, Secondary: []intent.IntentLabel{intent.LabelSearch},
			},
			understand: true,
			governed:   true,
		},
		{
			name: "search plus generate",
			result: intent.ClassificationResult{
				Primary: intent.LabelSearch, Secondary: []intent.IntentLabel{intent.LabelDocumentGenerate},
			},
		},
		{
			name:   "unknown",
			result: intent.ClassificationResult{Primary: intent.LabelUnknown, Confidence: 0.30, Degraded: true},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if got := semanticReadOnlyLookupFamily(testCase.result); got != testCase.lookup {
				t.Fatalf("lookup=%v, want %v", got, testCase.lookup)
			}
			if got := semanticReadOnlyUnderstandFamily(testCase.result); got != testCase.understand {
				t.Fatalf("understand=%v, want %v", got, testCase.understand)
			}
			if got := semanticReadOnlyGovernedFamily(testCase.result); got != testCase.governed {
				t.Fatalf("governed=%v, want %v", got, testCase.governed)
			}
		})
	}
}

func TestSemanticDegradedConfidentFileReadStillPlans(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry()}
	registerBuiltinTools(h.registry, h)
	registerNonCodeTools(h.registry, &App{testHomeDir: t.TempDir()})
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "看看 notes.txt", "desktop", "root-fread-hot-degraded", "turn-fread-hot-degraded",
		&intent.ClassificationResult{Primary: intent.LabelFileRead, Confidence: 0.85, Layer: 2, Degraded: true},
	)
	if err != nil || !handled || surface == nil || len(defs) == 0 {
		t.Fatalf("confident degraded file_read must still plan: defs=%#v handled=%v err=%v", defs, handled, err)
	}
	if !planHasCapabilities(surface.plan, tool.CapabilityFSReadLocal) {
		t.Fatalf("selections=%#v, want fs.read.local", surface.plan.Selections)
	}
}

func TestSemanticWeakMixedReadOnlyFallsThroughToChat(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry()}
	_, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "帮我看看这个说法", "desktop", "root-mixed-read", "turn-mixed-read",
		&intent.ClassificationResult{
			Primary: intent.LabelKnowledgeRead, Secondary: []intent.IntentLabel{intent.LabelSearch},
			Confidence: 0.55, Layer: 3, Degraded: true,
		},
	)
	if err != nil || handled || surface != nil {
		t.Fatalf("weak knowledge_read+search must fall through to chat: surface=%#v handled=%v err=%v", surface, handled, err)
	}
	if semanticReadOnlyLookupFamily(intent.ClassificationResult{
		Primary: intent.LabelKnowledgeRead, Secondary: []intent.IntentLabel{intent.LabelSearch},
	}) || !semanticReadOnlyUnderstandFamily(intent.ClassificationResult{
		Primary: intent.LabelKnowledgeRead, Secondary: []intent.IntentLabel{intent.LabelSearch},
	}) {
		t.Fatal("mixed read-only must be an understand family, not a pure lookup")
	}
}

func TestSemanticWeakFileReadWithGenerateFallsThroughToChat(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry()}
	_, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "读取文件并生成pdf", "desktop", "root-fread-pdf-weak", "turn-fread-pdf-weak",
		&intent.ClassificationResult{
			Primary: intent.LabelFileRead, Secondary: []intent.IntentLabel{intent.LabelDocumentGenerate},
			Confidence: 0.69, Layer: 2, Degraded: true,
		},
	)
	if err != nil || handled || surface != nil {
		t.Fatalf("weak file_read+generate must fall through so the turn can continue: surface=%#v handled=%v err=%v", surface, handled, err)
	}
}

func TestSemanticWeakFileReadWithoutImageFallsThroughToChat(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry()}
	_, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "读取一下这个文件的内容", "desktop", "root-file-read-weak", "turn-file-read-weak",
		&intent.ClassificationResult{Primary: intent.LabelFileRead, Confidence: 0.69, Layer: 2, Degraded: true},
	)
	if err != nil || handled || surface != nil {
		t.Fatalf("weak file_read without a staged image must fall through to chat: surface=%#v handled=%v err=%v", surface, handled, err)
	}
}

func TestSemanticWeakDocumentReadImageDescribeFallsThroughToChat(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry()}
	text := "图上有什么？\n\n" + filePathPromptPrefix + "\nC:\\Users\\ma139\\Pictures\\Camera Roll\\WIN_20260816_19_06_39_Pro.mp4"
	ctx := &LoopContext{Runtime: RuntimeContext{
		SemanticIntent: &intent.ClassificationResult{
			Primary: intent.LabelDocumentRead, Confidence: 0.55, Layer: 3, Degraded: true,
			Reason: "tree-after-embedding: document_read (0.550)",
		},
	}}
	_, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithContextAndAttachments(ctx, "user-1", text, "desktop", nil)
	if err != nil || handled || surface != nil {
		t.Fatalf("weak document_read on 图上有什么 + local video must not HostReject: surface=%#v handled=%v err=%v", surface, handled, err)
	}
	if ctx.Runtime.SemanticIntent == nil || ctx.Runtime.SemanticIntent.Primary != intent.LabelUnknown {
		t.Fatalf("weak document_read must chat-project, got %#v", ctx.Runtime.SemanticIntent)
	}
	if loopContextIsSemanticManaged(ctx) {
		t.Fatal("chat-projected image-describe must not stay capability-managed")
	}
	if !loopContextHasChatProjection(ctx) {
		t.Fatal("leftover router must see the chat-projection marker")
	}
}

func TestSemanticDegradedDocumentGenerateWithImageStillFailsClosed(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry()}
	text := "生成pdf报告\n\n" + filePathPromptPrefix + "\nC:\\tmp\\chart.jpg"
	_, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", text, "desktop", "root-pdf-image", "turn-pdf-image",
		&intent.ClassificationResult{Primary: intent.LabelDocumentGenerate, Confidence: 0.73, Layer: 2, Degraded: true},
	)
	if err != nil || handled || surface != nil {
		t.Fatalf("degraded generate must fall through even with a picker path: surface=%#v handled=%v err=%v", surface, handled, err)
	}
}
