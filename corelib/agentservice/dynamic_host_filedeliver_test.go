package agentservice

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/scheduler"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

type fakeHostFileDeliverer struct {
	principal     Principal
	destinationID string
	path          string
	result        string
	err           error
}

func (f *fakeHostFileDeliverer) PrepareReviewedHostFileDeliver(_ context.Context, principal Principal, destinationID, path string) (string, error) {
	f.principal = principal
	f.destinationID = destinationID
	f.path = path
	return f.result, f.err
}

func TestReviewedHostFileDeliverRequiresTrustedDestinationAndRejectsSoup(t *testing.T) {
	if _, _, _, err := ProjectReviewedHostFileDeliverProvider(&fakeHostFileDeliverer{}, ""); err == nil {
		t.Fatal("empty destination must not project file deliver")
	}
	if _, _, _, err := ProjectReviewedHostFileDeliverProvider(&fakeHostFileDeliverer{}, "ops"); err == nil {
		t.Fatal("bare group name must not project file deliver")
	}
	provider, definition, _, err := ProjectReviewedHostFileDeliverProvider(&fakeHostFileDeliverer{result: "prepared"}, "group:ops")
	if err != nil {
		t.Fatal(err)
	}
	if provider.Provides[0].Capability != CapabilityArtifactDeliverSpecified || provider.AdapterName == "send_file" || provider.AdapterName == "send_to_im" || provider.AdapterName == "im_message" {
		t.Fatalf("provider=%#v", provider)
	}
	if len(provider.Provides) != 3 || provider.Provides[0].Qualifiers[QualifierArtifactFormat] != ArtifactFormatFile || provider.Provides[1].Qualifiers[QualifierArtifactFormat] != ArtifactFormatImage || provider.Provides[2].Qualifiers[QualifierArtifactFormat] != ArtifactFormatVoice {
		t.Fatalf("qualifiers=%#v", provider.Provides)
	}
	fn, _ := definition["function"].(map[string]interface{})
	params, _ := fn["parameters"].(map[string]interface{})
	props, _ := params["properties"].(map[string]interface{})
	if _, ok := props["path"]; !ok || len(props) != 1 {
		t.Fatalf("file deliver schema=%#v", props)
	}
	for _, key := range []string{"channel", "destination", "group_name", "group_id", "user_id", "file_name", "caption"} {
		if _, ok := props[key]; ok {
			t.Fatalf("file deliver schema leaked %s", key)
		}
	}
}

func TestReviewedHostFileDeliverPlansOnlyWithTrustedDestination(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	deliverer := &fakeHostFileDeliverer{result: "Document prepared for group:ops (sheet.xlsx). This is not a send."}
	catalog, lifecycle, err := prepareReviewedDynamicSemanticCatalog(registry, nil, nil, DynamicCatalogLifecycle{}, reviewedHostOwnedServices{
		FileDeliver: deliverer, DestinationID: "group:ops",
	})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := coretool.NewToolCatalog(registry).PublishWithCoverage(catalog.Providers, lifecycle.Coverage, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	plan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "task-deliver", TurnID: "turn-deliver", Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{{
			ID: "deliver", Capability: CapabilityArtifactDeliverSpecified, Required: true,
			Qualifiers: map[string]string{QualifierArtifactFormat: ArtifactFormatFile},
		}},
	})
	if err != nil || len(plan.Selections) != 1 || len(plan.Unmet) != 0 {
		t.Fatalf("file deliver plan=%#v err=%v", plan, err)
	}
	if plan.Selections[0].AdapterName == "send_file" || plan.Selections[0].AdapterName == "send_to_im" {
		t.Fatalf("selection=%#v", plan.Selections[0])
	}
	if dynamicHostLocalMutationSelection(plan.Selections[0]) || dynamicHostObservedExternalSelection(plan.Selections[0]) || !dynamicSelectionRequiresReceipt(plan.Selections[0]) {
		t.Fatalf("file deliver must require the IM receipt coordinator, selection=%#v", plan.Selections[0])
	}
	for _, format := range []string{ArtifactFormatImage, ArtifactFormatVoice} {
		mediaPlan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
			RootTaskID: "task-deliver-" + format, TurnID: "turn-deliver-" + format, Snapshot: snapshot,
			Needs: []coretool.CapabilityNeed{{
				ID: "deliver", Capability: CapabilityArtifactDeliverSpecified, Required: true,
				Qualifiers: map[string]string{QualifierArtifactFormat: format},
			}},
		})
		if err != nil || len(mediaPlan.Selections) != 1 || len(mediaPlan.Unmet) != 0 {
			t.Fatalf("specified_target format=%s plan=%#v err=%v", format, mediaPlan, err)
		}
	}
	principal := Principal{TenantID: "t", UserID: "u"}
	rejected := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"path":"sheet.xlsx","channel":"lansenger","group_name":"ops"}`)
	if rejected.Succeeded || rejected.Unknown {
		t.Fatalf("channel soup must fail closed, result=%#v", rejected)
	}
	withoutCoordinator := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"path":"sheet.xlsx"}`)
	if withoutCoordinator.Succeeded || withoutCoordinator.ReasonCode != "dynamic_effect_coordinator_unavailable" {
		t.Fatalf("valid deliver without coordinator must fail closed, result=%#v", withoutCoordinator)
	}

	unmetCatalog, unmetLifecycle, err := prepareReviewedDynamicSemanticCatalog(registry, nil, nil, DynamicCatalogLifecycle{}, reviewedHostOwnedServices{})
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
			ID: "deliver", Capability: CapabilityArtifactDeliverSpecified, Required: true,
			Qualifiers: map[string]string{QualifierArtifactFormat: ArtifactFormatFile},
		}},
	})
	if err != nil || len(unmetPlan.Selections) != 0 {
		t.Fatalf("file deliver without destination must stay unmet, plan=%#v err=%v", unmetPlan, err)
	}
}

func TestReviewedHostFileDeliverPrepareIsNotASend(t *testing.T) {
	dir := t.TempDir()
	sheet := filepath.Join(dir, "sheet.xlsx")
	if err := os.WriteFile(sheet, []byte("xlsx-bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.md"), []byte("trusted text"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "report.pdf"), []byte("%PDF-1.4\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "archive.zip"), []byte("PK\x03\x04"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "photo.png"), []byte("\x89PNG"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "clip.wav"), []byte("RIFF"), 0o600); err != nil {
		t.Fatal(err)
	}
	principal := Principal{TenantID: "t", UserID: "u"}
	cb := &coreAgentCallbacks{
		principal:            principal,
		workspace:            dir,
		trustedDestinationID: "user:alice",
		imFileHandler: func(map[string]interface{}) string {
			t.Fatal("file deliver must not call send_file soup")
			return "sent"
		},
	}
	services := cb.reviewedHostOwnedServices()
	if services.FileDeliver == nil || services.DestinationID != "user:alice" {
		t.Fatalf("services=%#v", services)
	}
	out, err := cb.PrepareReviewedHostFileDeliver(context.Background(), principal, "user:alice", "sheet.xlsx")
	if err != nil || !strings.Contains(out, "This is not a send.") || !strings.Contains(out, "user:alice") || !strings.Contains(out, "sheet.xlsx") {
		t.Fatalf("prepare=%q err=%v", out, err)
	}
	for _, name := range []string{"notes.md", "report.pdf"} {
		got, err := cb.PrepareReviewedHostFileDeliver(context.Background(), principal, "user:alice", name)
		if err != nil || !strings.Contains(got, "This is not a send.") || !strings.Contains(got, name) {
			t.Fatalf("trusted document %s prepare=%q err=%v", name, got, err)
		}
	}
	if _, err := cb.PrepareReviewedHostFileDeliver(context.Background(), principal, "user:bob", "sheet.xlsx"); err == nil {
		t.Fatal("mismatched destination must fail closed")
	}
	if _, err := cb.PrepareReviewedHostFileDeliver(context.Background(), principal, "user:alice", "archive.zip"); err == nil {
		t.Fatal("zip must stay unpublished")
	}
	for _, name := range []string{"photo.png", "clip.wav"} {
		got, err := cb.PrepareReviewedHostFileDeliver(context.Background(), principal, "user:alice", name)
		if err != nil || !strings.Contains(got, "This is not a send.") || !strings.Contains(got, name) {
			t.Fatalf("trusted media %s prepare=%q err=%v", name, got, err)
		}
	}
	empty := (&coreAgentCallbacks{principal: principal, workspace: dir}).reviewedHostOwnedServices()
	if empty.FileDeliver != nil {
		t.Fatal("file deliver without destination must stay unpublished")
	}
	noWorkspace := (&coreAgentCallbacks{principal: principal, trustedDestinationID: "user:alice"}).reviewedHostOwnedServices()
	if noWorkspace.FileDeliver != nil {
		t.Fatal("file deliver without workspace must stay unpublished")
	}
}

func TestFireReviewedHostFileDeliverCASUnknownAndNoResend(t *testing.T) {
	dir := t.TempDir()
	coordinator, err := coretool.NewSQLiteSemanticExecutionCoordinator(filepath.Join(dir, "semantic-execution.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = coordinator.Close() })
	sent := 0
	now := time.Date(2026, 8, 17, 13, 0, 0, 0, time.UTC)
	data := []byte("xlsx-bytes")
	deps := reviewedHostFileDeliverFireDeps{
		Coordinator: coordinator, ChannelScope: "lansenger", DestinationID: "group:ops", PrincipalID: "u",
		FileName: "sheet.xlsx", MIMEType: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", Data: data,
		Send: func(_ context.Context, channel string, targets []scheduler.DeliveryTarget, body []byte, fileName, mimeType string) error {
			if channel != scheduler.DeliveryChannelLansenger || len(targets) != 1 || targets[0].GroupID != "ops" || fileName != "sheet.xlsx" || string(body) != "xlsx-bytes" {
				t.Fatalf("send channel=%s targets=%#v name=%s mime=%s body=%q", channel, targets, fileName, mimeType, body)
			}
			sent++
			return nil
		},
		Now: func() time.Time { return now },
	}
	if err := FireReviewedHostFileDeliver(context.Background(), deps); err == nil || err.Error() != "host_file_deliver_unknown" {
		t.Fatalf("nil send must settle unknown, err=%v", err)
	}
	if sent != 1 {
		t.Fatalf("sent=%d", sent)
	}
	contentKey := coretool.SchemaDigest(data)[:16]
	scope := coretool.InvocationScope{
		RootTaskID: "file-deliver:group:ops", PlanID: "run:" + now.Format("20060102T150405.000000000Z"),
		SessionID: "u", TurnID: "deliver:" + now.Format("20060102T150405.000000000Z") + ":" + contentKey, PrincipalID: "u",
	}
	if _, err := coordinator.SettleStandaloneDelivery(scope, reviewedHostFileDeliverFireSelectionID, coretool.DeliveryAccepted, "late-receipt", "late", now); err == nil {
		t.Fatal("late accepted settle after terminal unknown must not succeed")
	}
	if err := FireReviewedHostFileDeliver(context.Background(), deps); err == nil || err.Error() != "host_file_deliver_unknown" {
		t.Fatalf("same prepared delivery must not resend, err=%v", err)
	}
	if sent != 1 {
		t.Fatalf("same run must not resend, sent=%d", sent)
	}
	now = now.Add(24 * time.Hour)
	if err := FireReviewedHostFileDeliver(context.Background(), deps); err == nil || err.Error() != "host_file_deliver_unknown" {
		t.Fatalf("next occurrence must still be unknown, err=%v", err)
	}
	if sent != 2 {
		t.Fatalf("next occurrence sent=%d", sent)
	}
}

func TestReviewedHostFileDeliverFireUsesScheduleDispatchFileSendOnce(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "sheet.xlsx"), []byte("xlsx-bytes"), 0o600); err != nil {
		t.Fatal(err)
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
		workspace:            dir,
		trustedDestinationID: "user:alice",
		inboundChannelScope:  "lansenger",
		imFileHandler: func(map[string]interface{}) string {
			t.Fatal("file deliver must not call send_file soup")
			return "sent"
		},
		executor: &CoreAgentExecutor{
			ScheduleDispatchFileSend: func(_ context.Context, got Principal, channel string, targets []scheduler.DeliveryTarget, data []byte, fileName, mimeType string) error {
				if got.TenantID != principal.TenantID || got.UserID != principal.UserID || channel != scheduler.DeliveryChannelLansenger || len(targets) != 1 || targets[0].UserID != "alice" || fileName != "sheet.xlsx" || string(data) != "xlsx-bytes" {
					t.Fatalf("send principal=%#v channel=%s targets=%#v name=%s mime=%s data=%q", got, channel, targets, fileName, mimeType, data)
				}
				sent++
				return nil
			},
			dynamicSemanticRouting: &DynamicSemanticRouting{Coordinator: coordinator},
		},
	}
	out, err := cb.PrepareReviewedHostFileDeliver(context.Background(), principal, "user:alice", "sheet.xlsx")
	if out != "" || err == nil || err.Error() != "host_file_deliver_unknown" {
		t.Fatalf("armed fire must stay unknown, out=%q err=%v", out, err)
	}
	if sent != 1 {
		t.Fatalf("send called %d times", sent)
	}
}

func TestReviewedHostFileDeliverFireUsesImageSelectionForPNG(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "photo.png"), []byte("\x89PNG\r\n\x1a\n"), 0o600); err != nil {
		t.Fatal(err)
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
		workspace:            dir,
		trustedDestinationID: "user:alice",
		inboundChannelScope:  "lansenger",
		imFileHandler: func(map[string]interface{}) string {
			t.Fatal("file deliver must not call send_file soup")
			return "sent"
		},
		executor: &CoreAgentExecutor{
			ScheduleDispatchFileSend: func(_ context.Context, got Principal, channel string, targets []scheduler.DeliveryTarget, data []byte, fileName, mimeType string) error {
				if got.TenantID != principal.TenantID || got.UserID != principal.UserID || channel != scheduler.DeliveryChannelLansenger || len(targets) != 1 || targets[0].UserID != "alice" || fileName != "photo.png" || mimeType != "image/png" {
					t.Fatalf("send principal=%#v channel=%s targets=%#v name=%s mime=%s data=%q", got, channel, targets, fileName, mimeType, data)
				}
				sent++
				return nil
			},
			dynamicSemanticRouting: &DynamicSemanticRouting{Coordinator: coordinator},
		},
	}
	out, err := cb.PrepareReviewedHostFileDeliver(context.Background(), principal, "user:alice", "photo.png")
	if out != "" || err == nil || err.Error() != "host_file_deliver_unknown" {
		t.Fatalf("armed image fire must stay unknown, out=%q err=%v", out, err)
	}
	if sent != 1 {
		t.Fatalf("send called %d times", sent)
	}
}

func TestReviewedHostFileDeliverCoreAgentDoesNotFire(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "sheet.xlsx"), []byte("xlsx-bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	coordinator, err := coretool.NewSQLiteSemanticExecutionCoordinator(filepath.Join(dir, "semantic-execution.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = coordinator.Close() })
	principal := Principal{TenantID: "t", UserID: "u"}
	cb := &coreAgentCallbacks{
		principal:            principal,
		workspace:            dir,
		trustedDestinationID: "user:alice",
		inboundChannelScope:  "core-agent",
		imFileHandler: func(map[string]interface{}) string {
			t.Fatal("file deliver must not call send_file soup")
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
	out, err := cb.PrepareReviewedHostFileDeliver(context.Background(), principal, "user:alice", "sheet.xlsx")
	if err != nil || !strings.Contains(out, "This is not a send.") {
		t.Fatalf("core-agent must stay prepare-only, out=%q err=%v", out, err)
	}
}
