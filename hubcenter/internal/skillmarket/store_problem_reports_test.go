package skillmarket

import (
	"context"
	"testing"
	"time"
)

func TestProblemReportLifecycle(t *testing.T) {
	store := newTestStore(t)
	now := time.Now().UTC()
	report := &ProblemReport{ID: "BR-test", ReporterUserID: "user-1", ReporterContact: "phone:13800138000", OSVersion: "Windows 11", Description: "fails", Status: "pending", DiagnosticsPath: "diagnostics.zip", ScreenshotPaths: []string{"screenshot-01.png"}, OriginURL: "https://origin.example.com", CreatedAt: now, UpdatedAt: now}
	if err := store.CreateProblemReport(context.Background(), report); err != nil {
		t.Fatal(err)
	}
	items, total, err := store.ListProblemReports(context.Background(), "user-1", "pending", 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(items) != 1 || items[0].ReporterContact != "phone:13800138000" {
		t.Fatalf("unexpected list: total=%d items=%+v", total, items)
	}
	if err := store.UpdateProblemReport(context.Background(), "BR-test", "fixed", "resolved", time.Time{}); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetProblemReport(context.Background(), "BR-test")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "fixed" || got.AdminNote != "resolved" || got.OriginURL != "https://origin.example.com" {
		t.Fatalf("unexpected update: %+v", got)
	}
}

func TestProblemReportSnapshotReplicatesMetadataWithoutAttachments(t *testing.T) {
	origin := newTestStore(t)
	now := time.Now().UTC()
	report := &ProblemReport{ID: "BR-ha", ReporterUserID: "user-1", ReporterContact: "user@example.com", OSVersion: "Linux", Description: "fails", Status: "pending", DiagnosticsPath: "diagnostics.zip", ScreenshotPaths: []string{"screenshot-01.png"}, OriginURL: "https://origin.example.com", CreatedAt: now, UpdatedAt: now}
	if err := origin.CreateProblemReport(context.Background(), report); err != nil {
		t.Fatal(err)
	}
	snapshot, err := origin.DumpSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.ProblemReports) != 1 || snapshot.ProblemReports[0].DiagnosticsPath != "" || len(snapshot.ProblemReports[0].ScreenshotPaths) != 0 {
		t.Fatalf("attachments leaked into HA snapshot: %+v", snapshot.ProblemReports)
	}
	peer := newTestStore(t)
	if err := peer.LoadSnapshot(context.Background(), snapshot); err != nil {
		t.Fatal(err)
	}
	got, err := peer.GetProblemReport(context.Background(), "BR-ha")
	if err != nil {
		t.Fatal(err)
	}
	if got.OriginURL != "https://origin.example.com" || got.DiagnosticsPath != "" || len(got.ScreenshotPaths) != 0 {
		t.Fatalf("unexpected replicated report: %+v", got)
	}
}

func TestProblemReportSnapshotReplicatesDeletionTombstone(t *testing.T) {
	origin := newTestStore(t)
	now := time.Now().UTC()
	report := &ProblemReport{ID: "BR-delete", ReporterUserID: "user-1", ReporterContact: "user@example.com", OSVersion: "Linux", Description: "fails", Status: "pending", OriginURL: "https://origin.example.com", CreatedAt: now, UpdatedAt: now}
	if err := origin.CreateProblemReport(context.Background(), report); err != nil {
		t.Fatal(err)
	}
	peer := newTestStore(t)
	initial, err := origin.DumpSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := peer.LoadSnapshot(context.Background(), initial); err != nil {
		t.Fatal(err)
	}
	if err := origin.DeleteProblemReport(context.Background(), report.ID); err != nil {
		t.Fatal(err)
	}
	afterDelete, err := origin.DumpSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(afterDelete.ProblemReports) != 0 || len(afterDelete.ProblemReportDeletes) != 1 {
		t.Fatalf("unexpected delete snapshot: %+v", afterDelete)
	}
	if err := peer.LoadSnapshot(context.Background(), afterDelete); err != nil {
		t.Fatal(err)
	}
	if _, err := peer.GetProblemReport(context.Background(), report.ID); err != ErrNotFound {
		t.Fatalf("deleted report remained on peer: %v", err)
	}
}

func TestProblemReportSnapshotDoesNotOverwriteNewerPeerStateAtSameTimestamp(t *testing.T) {
	origin := newTestStore(t)
	peer := newTestStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	original := &ProblemReport{ID: "BR-conflict", ReporterUserID: "user-1", ReporterContact: "user@example.com", OSVersion: "Linux", Description: "origin", Status: "pending", OriginURL: "https://origin.example.com", CreatedAt: now, UpdatedAt: now}
	if err := origin.CreateProblemReport(context.Background(), original); err != nil {
		t.Fatal(err)
	}
	if err := peer.CreateProblemReport(context.Background(), &ProblemReport{ID: original.ID, ReporterUserID: "user-1", ReporterContact: "user@example.com", OSVersion: "Linux", Description: "peer", Status: "fixed", OriginURL: "https://peer.example.com", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	snapshot, err := origin.DumpSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := peer.LoadSnapshot(context.Background(), snapshot); err != nil {
		t.Fatal(err)
	}
	got, err := peer.GetProblemReport(context.Background(), original.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.OriginURL != "https://peer.example.com" || got.Description != "peer" {
		t.Fatalf("older deterministic winner overwrote peer state: %+v", got)
	}
}

func TestProblemReportUpdateUsesSubsecondTimestamp(t *testing.T) {
	store := newTestStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	if err := store.CreateProblemReport(context.Background(), &ProblemReport{ID: "BR-precision", ReporterUserID: "user-1", ReporterContact: "user@example.com", OSVersion: "Linux", Description: "fails", Status: "pending", OriginURL: "https://origin.example.com", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateProblemReport(context.Background(), "BR-precision", "fixed", "resolved", time.Time{}); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetProblemReport(context.Background(), "BR-precision")
	if err != nil {
		t.Fatal(err)
	}
	if !got.UpdatedAt.After(now) {
		t.Fatalf("updated_at lost subsecond ordering: created=%s updated=%s", now, got.UpdatedAt)
	}
}
