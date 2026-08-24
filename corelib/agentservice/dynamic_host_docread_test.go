package agentservice

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

type fakeHostDocumentReader struct {
	principal Principal
	args      map[string]interface{}
	result    string
	err       error
}

func (f *fakeHostDocumentReader) ReadReviewedHostDocument(_ context.Context, principal Principal, args map[string]interface{}) (string, error) {
	f.principal = principal
	f.args = args
	return f.result, f.err
}

func TestReviewedHostDocumentReadExecutesPagingAndRejectsPathMapping(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	reader := &fakeHostDocumentReader{result: "hello attachment"}
	observed := dynamicCatalogLifecycleForKind("mcp", IncompleteDynamicCatalogLifecycle(coretool.CatalogCoverageReasonNotReady))
	catalog, lifecycle, err := prepareReviewedDynamicSemanticCatalog(registry, nil, nil, observed, reviewedHostOwnedServices{DocumentRead: reader})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := coretool.NewToolCatalog(registry).PublishWithCoverage(catalog.Providers, lifecycle.Coverage, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	inputs, err := reviewedHostDocumentInputsForTurn("root", "turn", "user", []agent.MessageAttachment{{
		FileName: "notes.txt", MimeType: "text/plain", Data: base64.StdEncoding.EncodeToString([]byte("hello attachment")),
	}})
	if err != nil || len(inputs) != 1 {
		t.Fatalf("inputs=%#v err=%v", inputs, err)
	}
	plan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "task", TurnID: "turn", Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{{
			ID: "doc", Capability: CapabilityDocumentRead, Required: true,
			Qualifiers: map[string]string{QualifierDocumentFormat: DocumentFormatText},
		}},
		Facts: reviewedHostDocumentFacts(inputs),
	})
	if err != nil || len(plan.Selections) != 1 || len(plan.Unmet) != 0 {
		t.Fatalf("document read plan=%#v err=%v", plan, err)
	}
	if plan.Selections[0].Provider.Kind != reviewedHostProviderKind || plan.Selections[0].FitProof.MatchedCapability != CapabilityDocumentRead {
		t.Fatalf("selection=%#v", plan.Selections[0])
	}
	principal := Principal{TenantID: "tenant", UserID: "user"}
	result := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"max_chars":120}`)
	if !result.Succeeded || result.Result != reader.result {
		t.Fatalf("document read result=%#v", result)
	}
	if reader.principal.TenantID != principal.TenantID || reader.principal.UserID != principal.UserID {
		t.Fatalf("reader=%#v", reader)
	}
	rejected := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"file_path":"C:\\\\secret.pdf"}`)
	if rejected.Succeeded || rejected.Unknown {
		t.Fatalf("file_path args must fail closed, result=%#v", rejected)
	}
	channel := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"channel":"lansenger"}`)
	if channel.Succeeded || channel.Unknown {
		t.Fatalf("channel args must fail closed, result=%#v", channel)
	}

	filePlan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "task-file", TurnID: "turn-file", Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{{ID: "file", Capability: CapabilityFileRead, Required: true}},
	})
	if err != nil || len(filePlan.Selections) != 0 {
		t.Fatalf("file_read must not be satisfied by host document read, plan=%#v err=%v", filePlan, err)
	}
}

func TestReviewedHostDocumentReadIsAbsentWithoutReader(t *testing.T) {
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
			ID: "doc", Capability: CapabilityDocumentRead, Required: true,
			Qualifiers: map[string]string{QualifierDocumentFormat: DocumentFormatText},
		}},
	})
	if err != nil || len(plan.Selections) != 0 {
		t.Fatalf("document read without reader must stay unmet, plan=%#v err=%v", plan, err)
	}
}

func TestProjectReviewedHostDocumentReadRejectsPathAndChannelFields(t *testing.T) {
	provider, definition, _, err := ProjectReviewedHostDocumentReadProvider(&fakeHostDocumentReader{})
	if err != nil {
		t.Fatal(err)
	}
	if provider.Provides[0].Capability != CapabilityDocumentRead {
		t.Fatalf("provider=%#v", provider)
	}
	if len(provider.Consumes) != 1 || provider.Consumes[0].Kind != "document" {
		t.Fatalf("consumes=%#v", provider.Consumes)
	}
	fn, _ := definition["function"].(map[string]interface{})
	params, _ := fn["parameters"].(map[string]interface{})
	props, _ := params["properties"].(map[string]interface{})
	for _, key := range []string{"path", "file_path", "action", "artifact_id", "channel", "destination", "group_name", "content", "save_path", "query"} {
		if _, ok := props[key]; ok {
			t.Fatalf("document read schema leaked %s", key)
		}
	}
	if _, ok := props["max_chars"]; !ok {
		t.Fatalf("document read schema=%#v", props)
	}
}

func TestApplyReviewedHostDocumentInputsFailClosedWithoutExactAttachment(t *testing.T) {
	needs := []coretool.CapabilityNeed{{ID: "doc", Capability: CapabilityDocumentRead, Required: true}}
	if _, err := applyReviewedHostDocumentInputs(needs, nil); err == nil || !strings.Contains(err.Error(), "trusted_document_input_missing") {
		t.Fatalf("missing attachment err=%v", err)
	}
	two := []reviewedHostDocumentInput{{Format: DocumentFormatText}, {Format: DocumentFormatPDF}}
	if _, err := applyReviewedHostDocumentInputs(needs, two); err == nil || !strings.Contains(err.Error(), "trusted_document_input_ambiguous") {
		t.Fatalf("ambiguous attachment err=%v", err)
	}
	one := []reviewedHostDocumentInput{{Format: DocumentFormatPDF}}
	resolved, err := applyReviewedHostDocumentInputs(needs, one)
	if err != nil || resolved[0].Qualifiers[QualifierDocumentFormat] != DocumentFormatPDF {
		t.Fatalf("resolved=%#v err=%v", resolved, err)
	}
	fileNeeds := []coretool.CapabilityNeed{{ID: "file", Capability: CapabilityFileRead, Required: true}}
	kept, err := applyReviewedHostDocumentInputs(fileNeeds, nil)
	if err != nil || kept[0].Capability != CapabilityFileRead {
		t.Fatalf("file_read must ignore missing attachments, kept=%#v err=%v", kept, err)
	}
	clock := []coretool.CapabilityNeed{{Capability: CapabilityCurrentTime, Required: true}}
	got, err := bindReviewedHostDocumentTurn(clock, nil, fmt.Errorf("trusted_document_attachment_content_missing"))
	if err != nil || len(got) != 1 || got[0].Capability != CapabilityCurrentTime {
		t.Fatalf("corrupt document must not fail a non-read turn, got=%#v err=%v", got, err)
	}
	if _, err := bindReviewedHostDocumentTurn(needs, nil, fmt.Errorf("trusted_document_attachment_content_missing")); err == nil {
		t.Fatal("document_read must fail closed on corrupt attachment")
	}
	deliverNeeds := []coretool.CapabilityNeed{{
		ID: "deliver", Capability: CapabilityArtifactDeliverCurrent, Required: true,
		Qualifiers: map[string]string{QualifierArtifactFormat: ArtifactFormatFile},
	}}
	if _, err := bindReviewedHostDocumentTurn(deliverNeeds, nil, nil); err == nil || !strings.Contains(err.Error(), "trusted_document_input_missing") {
		t.Fatalf("attachment deliver must fail closed without an attachment, err=%v", err)
	}
	notes := []byte("hello trusted document")
	urlInputs, err := reviewedHostDocumentInputsForTurn("root", "turn", "user", []agent.MessageAttachment{{
		FileName: "notes.txt", MimeType: "text/plain", Data: base64.URLEncoding.EncodeToString(notes),
	}})
	if err != nil || len(urlInputs) != 1 {
		t.Fatalf("url-safe document base64 must be accepted, inputs=%#v err=%v", urlInputs, err)
	}
	decoded, err := base64.StdEncoding.DecodeString(urlInputs[0].Payload.Base64)
	if err != nil || string(decoded) != string(notes) {
		t.Fatalf("document payload must be re-encoded as standard base64, payload=%q err=%v", urlInputs[0].Payload.Base64, err)
	}
	if _, err := reviewedHostDocumentInputsForTurn("root", "turn", "user", []agent.MessageAttachment{{
		FileName: "notes.txt", MimeType: "text/plain", Data: "!!!invalid!!!",
	}}); err == nil || !strings.Contains(err.Error(), "trusted_document_attachment_content_missing") {
		t.Fatalf("corrupt document attachment err=%v", err)
	}
	sniffed, err := reviewedHostDocumentInputsForTurn("root", "turn", "user", []agent.MessageAttachment{{
		FileName: "file", MimeType: "application/octet-stream", Data: base64.StdEncoding.EncodeToString([]byte("%PDF-1.4\n")),
	}})
	if err != nil || len(sniffed) != 1 || sniffed[0].Format != DocumentFormatPDF {
		t.Fatalf("unnamed pdf must bind as trusted document, inputs=%#v err=%v", sniffed, err)
	}
}

func TestReviewedHostDocumentReadUsesNativeReaderAndHidesTempPath(t *testing.T) {
	principal := Principal{TenantID: "tenant", UserID: "user"}
	inputs, err := reviewedHostDocumentInputsForTurn("root", "turn", "tenant:user", []agent.MessageAttachment{{
		FileName: "notes.txt", MimeType: "text/plain", Data: base64.StdEncoding.EncodeToString([]byte("hello trusted document")),
	}})
	if err != nil || len(inputs) != 1 {
		t.Fatalf("inputs=%#v err=%v", inputs, err)
	}
	cb := &coreAgentCallbacks{principal: principal, reviewedHostDocument: &inputs[0]}
	out, err := cb.ReadReviewedHostDocument(context.Background(), principal, map[string]interface{}{})
	if err != nil || !strings.Contains(out, "hello trusted document") {
		t.Fatalf("document read=%q err=%v", out, err)
	}
	if strings.Contains(out, os.TempDir()) || strings.Contains(out, "# path:") {
		t.Fatalf("temp path leaked: %q", out)
	}
	if _, err := cb.ReadReviewedHostDocument(context.Background(), Principal{TenantID: "other", UserID: "user"}, nil); err == nil {
		t.Fatal("principal mismatch must fail closed")
	}
}
