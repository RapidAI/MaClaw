package agentservice

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/intent"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

type fakeHostDocumentGenerator struct {
	principal Principal
	content   string
	result    string
	err       error
}

func (f *fakeHostDocumentGenerator) GenerateReviewedHostDocument(_ context.Context, principal Principal, content string) (string, error) {
	f.principal = principal
	f.content = content
	return f.result, f.err
}

func reviewedHostTestPDF() []byte {
	return []byte("%PDF-1.4\n%test\n")
}

func TestReviewedHostDocumentGenerateExecutesWithoutCoordinatorAndRejectsSoup(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	generator := &fakeHostDocumentGenerator{result: "PDF artifact published; deliver it through the current-channel file adapter. This is not a send."}
	catalog, lifecycle, err := prepareReviewedDynamicSemanticCatalog(registry, nil, nil, DynamicCatalogLifecycle{}, reviewedHostOwnedServices{DocumentGenerate: generator})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := coretool.NewToolCatalog(registry).PublishWithCoverage(catalog.Providers, lifecycle.Coverage, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	plan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "task-gen", TurnID: "turn-gen", Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{{
			ID: "generate", Capability: CapabilityDocumentGenerate, Required: true,
			Qualifiers: map[string]string{QualifierDocumentFormat: DocumentFormatPDF},
		}},
	})
	if err != nil || len(plan.Selections) != 1 || len(plan.Unmet) != 0 {
		t.Fatalf("generate plan=%#v err=%v", plan, err)
	}
	if plan.Selections[0].AdapterName == "generate_pdf" || plan.Selections[0].AdapterName == "office" {
		t.Fatalf("generate leaked soup: %#v", plan.Selections[0])
	}
	if !dynamicHostLocalMutationSelection(plan.Selections[0]) || dynamicSelectionRequiresReceipt(plan.Selections[0]) {
		t.Fatalf("generate must use the local mutation receipt, selection=%#v", plan.Selections[0])
	}
	principal := Principal{TenantID: "t", UserID: "u"}
	result := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"content":"# Hello"}`)
	if !result.Succeeded || result.Unknown || !strings.Contains(result.Result, "This is not a send.") || generator.content != "# Hello" {
		t.Fatalf("generate result=%#v writer=%#v", result, generator)
	}
	rejected := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"content":"# Hello","channel":"lansenger","path":"out.pdf"}`)
	if rejected.Succeeded || rejected.Unknown {
		t.Fatalf("path soup must fail closed, result=%#v", rejected)
	}
	bypass := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"content":"[file_base64|a.pdf|application/pdf]AAAA"}`)
	if bypass.Succeeded || bypass.Unknown {
		t.Fatalf("file_base64 must fail closed, result=%#v", bypass)
	}
}

func TestReviewedHostDocumentGenerateAndCurrentDeliverPlanTogether(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	generator := &fakeHostDocumentGenerator{result: "prepared"}
	catalog, lifecycle, err := prepareReviewedDynamicSemanticCatalog(registry, nil, nil, DynamicCatalogLifecycle{}, reviewedHostOwnedServices{
		DocumentGenerate:  generator,
		AttachmentDeliver: &fakeHostAttachmentDeliverer{result: "not a send"},
		DestinationID:     "user:alice",
	})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := coretool.NewToolCatalog(registry).PublishWithCoverage(catalog.Providers, lifecycle.Coverage, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	plan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "task-chain", TurnID: "turn-chain", Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{{
			ID: "generate", Capability: CapabilityDocumentGenerate, Required: true,
			Qualifiers: map[string]string{QualifierDocumentFormat: DocumentFormatPDF},
		}, {
			ID: "deliver", Capability: CapabilityArtifactDeliverCurrent, Required: true,
			Qualifiers: map[string]string{QualifierArtifactFormat: ArtifactFormatFile},
		}},
	})
	if err != nil || len(plan.Selections) != 2 || len(plan.Unmet) != 0 {
		t.Fatalf("generate+deliver plan=%#v err=%v", plan, err)
	}
	var generateID string
	var deliverSel coretool.PlannedSelection
	for _, selection := range plan.Selections {
		if selection.FitProof.MatchedCapability == CapabilityDocumentGenerate {
			generateID = selection.ID
		}
		if selection.FitProof.MatchedCapability == CapabilityArtifactDeliverCurrent {
			deliverSel = selection
		}
	}
	found := false
	for _, requirement := range deliverSel.Requires {
		if requirement == generateID {
			found = true
		}
	}
	if generateID == "" || !found {
		t.Fatalf("deliver must wait for generate, generate=%s requires=%#v", generateID, deliverSel.Requires)
	}
}

func TestReviewedHostDocumentGenerateIsAbsentWithoutGenerator(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	catalog, lifecycle, err := prepareReviewedDynamicSemanticCatalog(registry, nil, nil, DynamicCatalogLifecycle{}, reviewedHostOwnedServices{})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := coretool.NewToolCatalog(registry).PublishWithCoverage(catalog.Providers, lifecycle.Coverage, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	plan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "task", TurnID: "turn", Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{{
			ID: "generate", Capability: CapabilityDocumentGenerate, Required: true,
			Qualifiers: map[string]string{QualifierDocumentFormat: DocumentFormatPDF},
		}},
	})
	if err != nil || len(plan.Selections) != 0 {
		t.Fatalf("generate without writer must stay unmet, plan=%#v err=%v", plan, err)
	}
}

func TestProjectReviewedHostDocumentGenerateRejectsSoupFields(t *testing.T) {
	provider, definition, _, err := ProjectReviewedHostDocumentGenerateProvider(&fakeHostDocumentGenerator{})
	if err != nil {
		t.Fatal(err)
	}
	if provider.Provides[0].Capability != CapabilityDocumentGenerate || provider.AdapterName == "generate_pdf" || provider.AdapterName == "office" {
		t.Fatalf("provider=%#v", provider)
	}
	fn, _ := definition["function"].(map[string]interface{})
	params, _ := fn["parameters"].(map[string]interface{})
	props, _ := params["properties"].(map[string]interface{})
	if _, ok := props["content"]; !ok || len(props) != 1 {
		t.Fatalf("generate schema=%#v", props)
	}
	for _, key := range []string{"path", "channel", "destination", "group_name", "file_name", "action"} {
		if _, ok := props[key]; ok {
			t.Fatalf("generate schema leaked %s", key)
		}
	}
}

func TestBindReviewedHostDeliverableTurnSkipsInboundWhenGeneratePresent(t *testing.T) {
	needs := []coretool.CapabilityNeed{{
		ID: "generate", Capability: CapabilityDocumentGenerate, Required: true,
		Qualifiers: map[string]string{QualifierDocumentFormat: DocumentFormatPDF},
	}, {
		ID: "deliver", Capability: CapabilityArtifactDeliverCurrent, Required: true,
		Qualifiers: map[string]string{QualifierArtifactFormat: ArtifactFormatFile},
	}}
	resolved, doc, img, voice, err := bindReviewedHostDeliverableTurn(needs, reviewedHostDeliverableTurnInputs{})
	if err != nil || doc != nil || img != nil || voice != nil || len(resolved) != 2 {
		t.Fatalf("generate+deliver without attachment must bind, resolved=%#v err=%v", resolved, err)
	}
	if resolved[1].Qualifiers[QualifierArtifactFormat] != ArtifactFormatFile {
		t.Fatalf("deliver format rewritten: %#v", resolved[1])
	}
}

func TestReviewedHostAttachmentDeliverPrepareGeneratedPDFIsNotASend(t *testing.T) {
	generated, err := reviewedHostGeneratedDocumentFromPDF("hello", reviewedHostTestPDF())
	if err != nil {
		t.Fatal(err)
	}
	principal := Principal{TenantID: "t", UserID: "u"}
	cb := &coreAgentCallbacks{
		principal:                     principal,
		trustedDestinationID:          "user:alice",
		reviewedHostGenerate:          true,
		reviewedHostGeneratedDocument: generated,
		imFileHandler: func(map[string]interface{}) string {
			t.Fatal("generated deliver must not call send_file soup")
			return "sent"
		},
	}
	services := cb.reviewedHostOwnedServices()
	if services.AttachmentDeliver == nil {
		t.Fatal("generated PDF must publish attachment deliver")
	}
	out, err := cb.PrepareReviewedHostAttachmentDeliver(context.Background(), principal, "user:alice")
	if err != nil || !strings.Contains(out, "This is not a send.") {
		t.Fatalf("prepare=%q err=%v", out, err)
	}
}

func TestReviewedHostDocumentGenerateCallbackPublishesArtifactWithoutSend(t *testing.T) {
	principal := Principal{TenantID: "t", UserID: "u"}
	cb := &coreAgentCallbacks{
		principal:            principal,
		workspace:            t.TempDir(),
		reviewedHostGenerate: true,
		reviewedHostPDFRenderer: func(content string) ([]byte, error) {
			if content != "hello" {
				t.Fatalf("content=%q", content)
			}
			return reviewedHostTestPDF(), nil
		},
		imFileHandler: func(map[string]interface{}) string {
			t.Fatal("generate must not call send_file soup")
			return "sent"
		},
	}
	out, err := cb.GenerateReviewedHostDocument(context.Background(), principal, "hello")
	if err != nil || !strings.Contains(out, "This is not a send.") || cb.reviewedHostGeneratedDocument == nil {
		t.Fatalf("generate=%q err=%v generated=%v", out, err, cb.reviewedHostGeneratedDocument)
	}
	if string(cb.reviewedHostGeneratedDocument.Data) != string(reviewedHostTestPDF()) {
		t.Fatalf("generated=%#v", cb.reviewedHostGeneratedDocument)
	}
}

func TestReviewedDynamicIntentRulesResolveDocumentGenerateWithoutOffice(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	resolver := &IntentLabelCapabilityNeedResolver{
		Classifier: fixedIntentClassificationSource{result: intent.ClassificationResult{
			Primary: intent.LabelDocumentGenerate, Confidence: .99,
			ToolNames: []string{"generate_pdf", "office", "write_file", "send_file"},
		}},
		Registry: registry, Rules: ReviewedDynamicIntentCapabilityNeedRules(),
	}
	resolution, err := resolver.ResolveDynamicCapabilityNeeds(context.Background(), DynamicCapabilityNeedRequest{UserText: "生成一份PDF文档"})
	if err != nil || !resolution.Managed || len(resolution.Needs) != 2 {
		t.Fatalf("resolution=%#v err=%v", resolution, err)
	}
	got := map[coretool.CapabilityID]bool{}
	for _, need := range resolution.Needs {
		got[need.Capability] = true
	}
	if !got[CapabilityDocumentGenerate] || !got[CapabilityArtifactDeliverCurrent] {
		t.Fatalf("needs=%#v", resolution.Needs)
	}
	if resolution.Needs[0].Capability == CapabilityOfficeWrite || resolution.Needs[0].Capability == CapabilityFileWrite {
		t.Fatal("document_generate must not resolve to office write or fs.write.local")
	}
}

func TestReviewedHostOwnedServicesPublishGenerateWithWorkspace(t *testing.T) {
	cb := &coreAgentCallbacks{principal: Principal{TenantID: "t", UserID: "u"}, workspace: t.TempDir()}
	services := cb.reviewedHostOwnedServices()
	if services.DocumentGenerate == nil {
		t.Fatal("workspace must publish document generate")
	}
}
