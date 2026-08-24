package agentservice

import (
	"context"
	"encoding/base64"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/scheduler"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

type fakeHostAttachmentDeliverer struct {
	principal     Principal
	destinationID string
	result        string
	err           error
}

func (f *fakeHostAttachmentDeliverer) PrepareReviewedHostAttachmentDeliver(_ context.Context, principal Principal, destinationID string) (string, error) {
	f.principal = principal
	f.destinationID = destinationID
	return f.result, f.err
}

func TestReviewedHostAttachmentDeliverRequiresTrustedDestinationAndEmptySchema(t *testing.T) {
	if _, _, _, err := ProjectReviewedHostAttachmentDeliverProvider(&fakeHostAttachmentDeliverer{}, ""); err == nil {
		t.Fatal("empty destination must not project attachment deliver")
	}
	if _, _, _, err := ProjectReviewedHostAttachmentDeliverProvider(&fakeHostAttachmentDeliverer{}, "ops"); err == nil {
		t.Fatal("bare group name must not project attachment deliver")
	}
	provider, definition, _, err := ProjectReviewedHostAttachmentDeliverProvider(&fakeHostAttachmentDeliverer{result: "prepared"}, "user:alice")
	if err != nil {
		t.Fatal(err)
	}
	if provider.Provides[0].Capability != CapabilityArtifactDeliverCurrent || provider.AdapterName == "send_file" || provider.AdapterName == "send_to_im" || provider.AdapterName == reviewedHostFileDeliverAdapterName {
		t.Fatalf("provider=%#v", provider)
	}
	if len(provider.Provides) != 3 || provider.Provides[0].Qualifiers[QualifierArtifactFormat] != ArtifactFormatFile || provider.Provides[1].Qualifiers[QualifierArtifactFormat] != ArtifactFormatImage || provider.Provides[2].Qualifiers[QualifierArtifactFormat] != ArtifactFormatVoice {
		t.Fatalf("qualifiers=%#v", provider.Provides)
	}
	fn, _ := definition["function"].(map[string]interface{})
	params, _ := fn["parameters"].(map[string]interface{})
	props, _ := params["properties"].(map[string]interface{})
	if len(props) != 0 {
		t.Fatalf("attachment deliver schema=%#v", props)
	}
}

func TestReviewedHostAttachmentDeliverPlansOnlyWithTrustedAttachment(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	deliverer := &fakeHostAttachmentDeliverer{result: "Trusted attachment prepared for user:alice. This is not a send."}
	catalog, lifecycle, err := prepareReviewedDynamicSemanticCatalog(registry, nil, nil, DynamicCatalogLifecycle{}, reviewedHostOwnedServices{
		AttachmentDeliver: deliverer, DestinationID: "user:alice",
	})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := coretool.NewToolCatalog(registry).PublishWithCoverage(catalog.Providers, lifecycle.Coverage, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	plan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "task-attach", TurnID: "turn-attach", Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{{
			ID: "deliver", Capability: CapabilityArtifactDeliverCurrent, Required: true,
			Qualifiers: map[string]string{QualifierArtifactFormat: ArtifactFormatFile},
		}},
	})
	if err != nil || len(plan.Selections) != 1 || len(plan.Unmet) != 0 {
		t.Fatalf("attachment deliver plan=%#v err=%v", plan, err)
	}
	if dynamicHostLocalMutationSelection(plan.Selections[0]) || dynamicHostObservedExternalSelection(plan.Selections[0]) || !dynamicSelectionRequiresReceipt(plan.Selections[0]) {
		t.Fatalf("attachment deliver must require the IM receipt coordinator, selection=%#v", plan.Selections[0])
	}
	principal := Principal{TenantID: "t", UserID: "u"}
	rejected := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"path":"sheet.xlsx","channel":"lansenger"}`)
	if rejected.Succeeded || rejected.Unknown {
		t.Fatalf("path soup must fail closed, result=%#v", rejected)
	}
	withoutCoordinator := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{}`)
	if withoutCoordinator.Succeeded || withoutCoordinator.ReasonCode != "dynamic_effect_coordinator_unavailable" {
		t.Fatalf("valid deliver without coordinator must fail closed, result=%#v", withoutCoordinator)
	}

	unmetCatalog, unmetLifecycle, err := prepareReviewedDynamicSemanticCatalog(registry, nil, nil, DynamicCatalogLifecycle{}, reviewedHostOwnedServices{DestinationID: "user:alice"})
	if err != nil {
		t.Fatal(err)
	}
	unmetSnapshot, err := coretool.NewToolCatalog(registry).PublishWithCoverage(unmetCatalog.Providers, unmetLifecycle.Coverage, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	unmetPlan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "task-unmet", TurnID: "turn-unmet", Snapshot: unmetSnapshot,
		Needs: []coretool.CapabilityNeed{{
			ID: "deliver", Capability: CapabilityArtifactDeliverCurrent, Required: true,
			Qualifiers: map[string]string{QualifierArtifactFormat: ArtifactFormatFile},
		}},
	})
	if err != nil || len(unmetPlan.Selections) != 0 {
		t.Fatalf("attachment deliver without bound attachment must stay unmet, plan=%#v err=%v", unmetPlan, err)
	}

	imagePlan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "task-image", TurnID: "turn-image", Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{{
			ID: "deliver", Capability: CapabilityArtifactDeliverCurrent, Required: true,
			Qualifiers: map[string]string{QualifierArtifactFormat: ArtifactFormatImage},
		}},
	})
	if err != nil || len(imagePlan.Selections) != 1 || len(imagePlan.Unmet) != 0 {
		t.Fatalf("image deliver plan=%#v err=%v", imagePlan, err)
	}
	if imagePlan.Selections[0].AdapterName == "send_file" || imagePlan.Selections[0].AdapterName == "send_to_im" {
		t.Fatalf("image selection leaked soup: %#v", imagePlan.Selections[0])
	}

	voicePlan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "task-voice", TurnID: "turn-voice", Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{{
			ID: "deliver", Capability: CapabilityArtifactDeliverCurrent, Required: true,
			Qualifiers: map[string]string{QualifierArtifactFormat: ArtifactFormatVoice},
		}},
	})
	if err != nil || len(voicePlan.Selections) != 1 || len(voicePlan.Unmet) != 0 {
		t.Fatalf("voice deliver plan=%#v err=%v", voicePlan, err)
	}
	if voicePlan.Selections[0].AdapterName == "send_file" || voicePlan.Selections[0].AdapterName == "send_to_im" || voicePlan.Selections[0].AdapterName == "asr" {
		t.Fatalf("voice selection leaked soup: %#v", voicePlan.Selections[0])
	}
}

func TestReviewedHostAttachmentDeliverPrepareIsNotASend(t *testing.T) {
	inputs, err := reviewedHostDocumentInputsForTurn("root", "turn", "u", []agent.MessageAttachment{{
		FileName: "notes.txt", MimeType: "text/plain", Data: base64.StdEncoding.EncodeToString([]byte("hello")),
	}})
	if err != nil || len(inputs) != 1 {
		t.Fatalf("inputs=%#v err=%v", inputs, err)
	}
	principal := Principal{TenantID: "t", UserID: "u"}
	cb := &coreAgentCallbacks{
		principal:            principal,
		trustedDestinationID: "user:alice",
		reviewedHostDocument: &inputs[0],
		imFileHandler: func(map[string]interface{}) string {
			t.Fatal("attachment deliver must not call send_file soup")
			return "sent"
		},
	}
	services := cb.reviewedHostOwnedServices()
	if services.AttachmentDeliver == nil || services.DestinationID != "user:alice" {
		t.Fatalf("services=%#v", services)
	}
	out, err := cb.PrepareReviewedHostAttachmentDeliver(context.Background(), principal, "user:alice")
	if err != nil || !strings.Contains(out, "This is not a send.") || !strings.Contains(out, "user:alice") {
		t.Fatalf("prepare=%q err=%v", out, err)
	}
	if _, err := cb.PrepareReviewedHostAttachmentDeliver(context.Background(), principal, "user:bob"); err == nil {
		t.Fatal("mismatched destination must fail closed")
	}
	empty := (&coreAgentCallbacks{principal: principal, trustedDestinationID: "user:alice"}).reviewedHostOwnedServices()
	if empty.AttachmentDeliver != nil {
		t.Fatal("attachment deliver without a bound document must stay unpublished")
	}
}

func TestReviewedHostAttachmentDeliverFireUsesScheduleDispatchFileSendOnce(t *testing.T) {
	dir := t.TempDir()
	inputs, err := reviewedHostDocumentInputsForTurn("root", "turn", "u", []agent.MessageAttachment{{
		FileName: "notes.txt", MimeType: "text/plain", Data: base64.StdEncoding.EncodeToString([]byte("hello")),
	}})
	if err != nil || len(inputs) != 1 {
		t.Fatalf("inputs=%#v err=%v", inputs, err)
	}
	coordinator, err := coretool.NewSQLiteSemanticExecutionCoordinator(filepath.Join(dir, "semantic-execution.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = coordinator.Close() })
	principal := Principal{TenantID: "t", UserID: "u"}
	sent := 0
	cb := &coreAgentCallbacks{
		principal:            principal,
		trustedDestinationID: "user:alice",
		inboundChannelScope:  "lansenger",
		reviewedHostDocument: &inputs[0],
		imFileHandler: func(map[string]interface{}) string {
			t.Fatal("attachment deliver must not call send_file soup")
			return "sent"
		},
		executor: &CoreAgentExecutor{
			ScheduleDispatchFileSend: func(_ context.Context, got Principal, channel string, targets []scheduler.DeliveryTarget, data []byte, fileName, mimeType string) error {
				if got.UserID != principal.UserID || channel != scheduler.DeliveryChannelLansenger || len(targets) != 1 || targets[0].UserID != "alice" || fileName != "notes.txt" || string(data) != "hello" {
					t.Fatalf("send principal=%#v channel=%s targets=%#v name=%s mime=%s data=%q", got, channel, targets, fileName, mimeType, data)
				}
				sent++
				return nil
			},
			dynamicSemanticRouting: &DynamicSemanticRouting{Coordinator: coordinator},
		},
	}
	out, err := cb.PrepareReviewedHostAttachmentDeliver(context.Background(), principal, "user:alice")
	if out != "" || err == nil || err.Error() != "host_file_deliver_unknown" {
		t.Fatalf("armed fire must stay unknown, out=%q err=%v", out, err)
	}
	if sent != 1 {
		t.Fatalf("send called %d times", sent)
	}
}

func TestReviewedHostAttachmentDeliverCoreAgentDoesNotFire(t *testing.T) {
	dir := t.TempDir()
	inputs, err := reviewedHostDocumentInputsForTurn("root", "turn", "u", []agent.MessageAttachment{{
		FileName: "notes.txt", MimeType: "text/plain", Data: base64.StdEncoding.EncodeToString([]byte("hello")),
	}})
	if err != nil || len(inputs) != 1 {
		t.Fatalf("inputs=%#v err=%v", inputs, err)
	}
	coordinator, err := coretool.NewSQLiteSemanticExecutionCoordinator(filepath.Join(dir, "semantic-execution.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = coordinator.Close() })
	principal := Principal{TenantID: "t", UserID: "u"}
	cb := &coreAgentCallbacks{
		principal:            principal,
		trustedDestinationID: "user:alice",
		inboundChannelScope:  "core-agent",
		reviewedHostDocument: &inputs[0],
		imFileHandler: func(map[string]interface{}) string {
			t.Fatal("attachment deliver must not call send_file soup")
			return "sent"
		},
		executor: &CoreAgentExecutor{
			ScheduleDispatchFileSend: func(context.Context, Principal, string, []scheduler.DeliveryTarget, []byte, string, string) error {
				t.Fatal("core-agent must not fire a channel file send")
				return nil
			},
			dynamicSemanticRouting: &DynamicSemanticRouting{Coordinator: coordinator},
		},
	}
	out, err := cb.PrepareReviewedHostAttachmentDeliver(context.Background(), principal, "user:alice")
	if err != nil || !strings.Contains(out, "This is not a send.") {
		t.Fatalf("core-agent must stay prepare-only, out=%q err=%v", out, err)
	}
}

func TestApplyReviewedHostDocumentInputsRequireExactAttachmentForCurrentDeliver(t *testing.T) {
	needs := []coretool.CapabilityNeed{{
		ID: "deliver", Capability: CapabilityArtifactDeliverCurrent, Required: true,
		Qualifiers: map[string]string{QualifierArtifactFormat: ArtifactFormatFile},
	}}
	if _, err := applyReviewedHostDocumentInputs(needs, nil); err == nil || !strings.Contains(err.Error(), "trusted_document_input_missing") {
		t.Fatalf("missing attachment err=%v", err)
	}
	if _, err := applyReviewedHostDocumentInputs(needs, []reviewedHostDocumentInput{{FileName: "a.txt"}, {FileName: "b.txt"}}); err == nil || !strings.Contains(err.Error(), "trusted_document_input_ambiguous") {
		t.Fatalf("two attachments err=%v", err)
	}
	if _, err := bindReviewedHostDocumentTurn(needs, nil, nil); err == nil || !strings.Contains(err.Error(), "trusted_document_input_missing") {
		t.Fatalf("bind missing attachment err=%v", err)
	}
}

func TestBindReviewedHostDeliverableTurnRewritesImageFormat(t *testing.T) {
	needs := []coretool.CapabilityNeed{{
		ID: "deliver", Capability: CapabilityArtifactDeliverCurrent, Required: true,
		Qualifiers: map[string]string{QualifierArtifactFormat: ArtifactFormatFile},
	}}
	images, err := reviewedHostImageInputsForTurn("root", "turn", "u", []agent.MessageAttachment{{
		FileName: "shot.png", MimeType: "image/png", Data: base64.StdEncoding.EncodeToString(reviewedHostTestPNG()),
	}})
	if err != nil || len(images) != 1 {
		t.Fatalf("images=%#v err=%v", images, err)
	}
	resolved, doc, img, voice, err := bindReviewedHostDeliverableTurn(needs, reviewedHostDeliverableTurnInputs{Images: images})
	if err != nil || doc != nil || img == nil || voice != nil || resolved[0].Qualifiers[QualifierArtifactFormat] != ArtifactFormatImage {
		t.Fatalf("resolved=%#v doc=%v img=%v voice=%v err=%v", resolved, doc, img, voice, err)
	}
	if _, _, _, _, err := bindReviewedHostDeliverableTurn(needs, reviewedHostDeliverableTurnInputs{}); err == nil || !strings.Contains(err.Error(), "trusted_document_input_missing") {
		t.Fatalf("missing image err=%v", err)
	}
	if _, _, _, _, err := bindReviewedHostDeliverableTurn(needs, reviewedHostDeliverableTurnInputs{Images: []reviewedHostImageInput{{FileName: "a.png"}, {FileName: "b.png"}}}); err == nil || !strings.Contains(err.Error(), "trusted_document_input_ambiguous") {
		t.Fatalf("two images err=%v", err)
	}
}

func TestReviewedHostAttachmentDeliverPrepareImageIsNotASend(t *testing.T) {
	images, err := reviewedHostImageInputsForTurn("root", "turn", "u", []agent.MessageAttachment{{
		FileName: "shot.png", MimeType: "image/png", Data: base64.StdEncoding.EncodeToString(reviewedHostTestPNG()),
	}})
	if err != nil || len(images) != 1 {
		t.Fatalf("images=%#v err=%v", images, err)
	}
	principal := Principal{TenantID: "t", UserID: "u"}
	cb := &coreAgentCallbacks{
		principal:            principal,
		trustedDestinationID: "user:alice",
		reviewedHostImage:    &images[0],
		imFileHandler: func(map[string]interface{}) string {
			t.Fatal("image deliver must not call send_file soup")
			return "sent"
		},
	}
	services := cb.reviewedHostOwnedServices()
	if services.AttachmentDeliver == nil {
		t.Fatal("image attachment must publish attachment deliver")
	}
	out, err := cb.PrepareReviewedHostAttachmentDeliver(context.Background(), principal, "user:alice")
	if err != nil || !strings.Contains(out, "This is not a send.") {
		t.Fatalf("prepare=%q err=%v", out, err)
	}
}

func TestReviewedHostAttachmentDeliverFireImageUsesScheduleDispatchFileSendOnce(t *testing.T) {
	dir := t.TempDir()
	images, err := reviewedHostImageInputsForTurn("root", "turn", "u", []agent.MessageAttachment{{
		FileName: "shot.png", MimeType: "image/png", Data: base64.StdEncoding.EncodeToString(reviewedHostTestPNG()),
	}})
	if err != nil || len(images) != 1 {
		t.Fatalf("images=%#v err=%v", images, err)
	}
	coordinator, err := coretool.NewSQLiteSemanticExecutionCoordinator(filepath.Join(dir, "semantic-execution.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = coordinator.Close() })
	principal := Principal{TenantID: "t", UserID: "u"}
	sent := 0
	cb := &coreAgentCallbacks{
		principal:            principal,
		trustedDestinationID: "user:alice",
		inboundChannelScope:  "lansenger",
		reviewedHostImage:    &images[0],
		imFileHandler: func(map[string]interface{}) string {
			t.Fatal("image deliver must not call send_file soup")
			return "sent"
		},
		executor: &CoreAgentExecutor{
			ScheduleDispatchFileSend: func(_ context.Context, got Principal, channel string, targets []scheduler.DeliveryTarget, data []byte, fileName, mimeType string) error {
				if got.UserID != principal.UserID || channel != scheduler.DeliveryChannelLansenger || fileName != "shot.png" || mimeType != "image/png" || len(data) == 0 {
					t.Fatalf("send principal=%#v channel=%s name=%s mime=%s data=%q", got, channel, fileName, mimeType, data)
				}
				sent++
				return nil
			},
			dynamicSemanticRouting: &DynamicSemanticRouting{Coordinator: coordinator},
		},
	}
	out, err := cb.PrepareReviewedHostAttachmentDeliver(context.Background(), principal, "user:alice")
	if out != "" || err == nil || err.Error() != "host_file_deliver_unknown" {
		t.Fatalf("armed image fire must stay unknown, out=%q err=%v", out, err)
	}
	if sent != 1 {
		t.Fatalf("send called %d times", sent)
	}
}

func TestBindReviewedHostDeliverableTurnRewritesVoiceFormat(t *testing.T) {
	needs := []coretool.CapabilityNeed{{
		ID: "deliver", Capability: CapabilityArtifactDeliverCurrent, Required: true,
		Qualifiers: map[string]string{QualifierArtifactFormat: ArtifactFormatFile},
	}}
	voices, err := reviewedHostVoiceInputsForTurn("root", "turn", "u", []agent.MessageAttachment{{
		FileName: "clip.wav", MimeType: "audio/wav", Data: base64.StdEncoding.EncodeToString(reviewedHostTestWAV()),
	}})
	if err != nil || len(voices) != 1 {
		t.Fatalf("voices=%#v err=%v", voices, err)
	}
	resolved, doc, img, voice, err := bindReviewedHostDeliverableTurn(needs, reviewedHostDeliverableTurnInputs{Voices: voices})
	if err != nil || doc != nil || img != nil || voice == nil || resolved[0].Qualifiers[QualifierArtifactFormat] != ArtifactFormatVoice {
		t.Fatalf("resolved=%#v doc=%v img=%v voice=%v err=%v", resolved, doc, img, voice, err)
	}
	if _, _, _, _, err := bindReviewedHostDeliverableTurn(needs, reviewedHostDeliverableTurnInputs{Voices: []reviewedHostVoiceInput{{FileName: "a.wav"}, {FileName: "b.wav"}}}); err == nil || !strings.Contains(err.Error(), "trusted_document_input_ambiguous") {
		t.Fatalf("two voices err=%v", err)
	}
	if _, _, _, _, err := bindReviewedHostDeliverableTurn(needs, reviewedHostDeliverableTurnInputs{
		Images: []reviewedHostImageInput{{FileName: "shot.png"}},
		Voices: voices,
	}); err == nil || !strings.Contains(err.Error(), "trusted_document_input_ambiguous") {
		t.Fatalf("image+voice err=%v", err)
	}
	readNeeds := []coretool.CapabilityNeed{{Capability: CapabilityDocumentRead, Required: true}}
	if _, _, _, _, err := bindReviewedHostDeliverableTurn(readNeeds, reviewedHostDeliverableTurnInputs{Voices: voices}); err == nil || !strings.Contains(err.Error(), "trusted_document_input_missing") {
		t.Fatalf("document_read must not bind voice, err=%v", err)
	}
}

func TestReviewedHostAttachmentDeliverPrepareVoiceIsNotASend(t *testing.T) {
	voices, err := reviewedHostVoiceInputsForTurn("root", "turn", "u", []agent.MessageAttachment{{
		FileName: "clip.wav", MimeType: "audio/wav", Data: base64.StdEncoding.EncodeToString(reviewedHostTestWAV()),
	}})
	if err != nil || len(voices) != 1 {
		t.Fatalf("voices=%#v err=%v", voices, err)
	}
	principal := Principal{TenantID: "t", UserID: "u"}
	cb := &coreAgentCallbacks{
		principal:            principal,
		trustedDestinationID: "user:alice",
		reviewedHostVoice:    &voices[0],
		imFileHandler: func(map[string]interface{}) string {
			t.Fatal("voice deliver must not call send_file soup")
			return "sent"
		},
	}
	services := cb.reviewedHostOwnedServices()
	if services.AttachmentDeliver == nil {
		t.Fatal("voice attachment must publish attachment deliver")
	}
	out, err := cb.PrepareReviewedHostAttachmentDeliver(context.Background(), principal, "user:alice")
	if err != nil || !strings.Contains(out, "This is not a send.") {
		t.Fatalf("prepare=%q err=%v", out, err)
	}
}

func TestReviewedHostAttachmentDeliverFireVoiceUsesScheduleDispatchFileSendOnce(t *testing.T) {
	dir := t.TempDir()
	voices, err := reviewedHostVoiceInputsForTurn("root", "turn", "u", []agent.MessageAttachment{{
		FileName: "clip.wav", MimeType: "audio/wav", Data: base64.StdEncoding.EncodeToString(reviewedHostTestWAV()),
	}})
	if err != nil || len(voices) != 1 {
		t.Fatalf("voices=%#v err=%v", voices, err)
	}
	coordinator, err := coretool.NewSQLiteSemanticExecutionCoordinator(filepath.Join(dir, "semantic-execution.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = coordinator.Close() })
	principal := Principal{TenantID: "t", UserID: "u"}
	sent := 0
	cb := &coreAgentCallbacks{
		principal:            principal,
		trustedDestinationID: "user:alice",
		inboundChannelScope:  "lansenger",
		reviewedHostVoice:    &voices[0],
		imFileHandler: func(map[string]interface{}) string {
			t.Fatal("voice deliver must not call send_file soup")
			return "sent"
		},
		executor: &CoreAgentExecutor{
			ScheduleDispatchFileSend: func(_ context.Context, got Principal, channel string, targets []scheduler.DeliveryTarget, data []byte, fileName, mimeType string) error {
				if got.UserID != principal.UserID || channel != scheduler.DeliveryChannelLansenger || fileName != "clip.wav" || mimeType != "audio/wav" || len(data) == 0 {
					t.Fatalf("send principal=%#v channel=%s name=%s mime=%s data=%q", got, channel, fileName, mimeType, data)
				}
				sent++
				return nil
			},
			dynamicSemanticRouting: &DynamicSemanticRouting{Coordinator: coordinator},
		},
	}
	out, err := cb.PrepareReviewedHostAttachmentDeliver(context.Background(), principal, "user:alice")
	if out != "" || err == nil || err.Error() != "host_file_deliver_unknown" {
		t.Fatalf("armed voice fire must stay unknown, out=%q err=%v", out, err)
	}
	if sent != 1 {
		t.Fatalf("send called %d times", sent)
	}
}
