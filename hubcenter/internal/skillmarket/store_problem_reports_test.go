package skillmarket

import (
	"context"
	"strings"
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

func TestMarkProblemReportFixedOnlyFromPending(t *testing.T) {
	store := newTestStore(t)
	now := time.Now().UTC()
	report := &ProblemReport{ID: "BR-repair", ReporterUserID: "user-1", Description: "slow", Status: "pending", CreatedAt: now, UpdatedAt: now}
	if err := store.CreateProblemReport(context.Background(), report); err != nil {
		t.Fatal(err)
	}
	if err := store.ResolveUnrepairedProblemReport(context.Background(), "BR-repair", "rejected", "不是 bug"); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetProblemReport(context.Background(), "BR-repair")
	if err != nil || got.Status != "rejected" || got.AdminNote != "不是 bug" || !got.ArchivedAt.IsZero() {
		t.Fatalf("rejected report = %+v err=%v", got, err)
	}
	if err := store.ResolveUnrepairedProblemReport(context.Background(), "BR-repair", "fixed", "again"); err != ErrProblemReportNotPending {
		t.Fatalf("second resolve err = %v", err)
	}
	if again, err := store.GetProblemReport(context.Background(), "BR-repair"); err != nil || again.AdminNote != "不是 bug" {
		t.Fatalf("note changed after rejected resolve: %+v err=%v", again, err)
	}
	if err := store.ResolveUnrepairedProblemReport(context.Background(), "BR-missing", "fixed", "note"); err != ErrNotFound {
		t.Fatalf("missing resolve err = %v", err)
	}
	if err := store.ResolveUnrepairedProblemReport(context.Background(), "BR-repair", "nope", "note"); err != ErrProblemReportStatus {
		t.Fatalf("bad status err = %v", err)
	}
}

func TestProblemReportAgentAPIKeySnapshotKeepsNewerKey(t *testing.T) {
	origin := newTestStore(t)
	peer := newTestStore(t)
	ctx := context.Background()
	if err := origin.SetProblemReportAgentAPIKey(ctx, "prk_new_key_123456"); err != nil {
		t.Fatal(err)
	}
	fresh, err := origin.DumpSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := peer.LoadSnapshot(ctx, fresh); err != nil {
		t.Fatal(err)
	}
	if got := peer.GetConfigWithDefault(ctx, ProblemReportAgentAPIKey, ""); got != "prk_new_key_123456" {
		t.Fatalf("peer key = %q", got)
	}
	stale := newTestStore(t)
	if err := stale.SetConfig(ctx, ProblemReportAgentAPIKey, "prk_old_key_123456"); err != nil {
		t.Fatal(err)
	}
	if err := stale.SetConfig(ctx, ProblemReportAgentAPIKeyAt, time.Now().UTC().Add(-time.Hour).Format(timeFmt)); err != nil {
		t.Fatal(err)
	}
	oldSnap, err := stale.DumpSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := peer.LoadSnapshot(ctx, oldSnap); err != nil {
		t.Fatal(err)
	}
	if got := peer.GetConfigWithDefault(ctx, ProblemReportAgentAPIKey, ""); got != "prk_new_key_123456" {
		t.Fatalf("stale snapshot rolled the key back to %q", got)
	}
}

func TestProblemReportAgentAPIKeySnapshotAppliesUnstampedKey(t *testing.T) {
	src := newTestStore(t)
	peer := newTestStore(t)
	ctx := context.Background()
	if err := src.SetConfig(ctx, ProblemReportAgentAPIKey, "prk_unstamped_key_1"); err != nil {
		t.Fatal(err)
	}
	if err := src.SetConfig(ctx, ProblemReportAgentAPIKeyAt, ""); err != nil {
		t.Fatal(err)
	}
	snap, err := src.DumpSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := peer.LoadSnapshot(ctx, snap); err != nil {
		t.Fatal(err)
	}
	if got := peer.GetConfigWithDefault(ctx, ProblemReportAgentAPIKey, ""); got != "prk_unstamped_key_1" {
		t.Fatalf("empty stamp dropped the key: %q", got)
	}
}

func TestProblemReportAgentAPIKeySnapshotRevokeClearsPeer(t *testing.T) {
	origin := newTestStore(t)
	peer := newTestStore(t)
	ctx := context.Background()
	if err := origin.SetProblemReportAgentAPIKey(ctx, "prk_live_key_123456"); err != nil {
		t.Fatal(err)
	}
	live, err := origin.DumpSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := peer.LoadSnapshot(ctx, live); err != nil {
		t.Fatal(err)
	}
	if err := origin.SetProblemReportAgentAPIKey(ctx, ""); err != nil {
		t.Fatal(err)
	}
	revoked, err := origin.DumpSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := peer.LoadSnapshot(ctx, revoked); err != nil {
		t.Fatal(err)
	}
	if got := peer.GetConfigWithDefault(ctx, ProblemReportAgentAPIKey, "fallback"); got != "" {
		t.Fatalf("revoke did not clear the peer key: %q", got)
	}
	stale := newTestStore(t)
	if err := stale.SetConfig(ctx, ProblemReportAgentAPIKey, "prk_live_key_123456"); err != nil {
		t.Fatal(err)
	}
	if err := stale.SetConfig(ctx, ProblemReportAgentAPIKeyAt, time.Now().UTC().Add(-time.Hour).Format(timeFmt)); err != nil {
		t.Fatal(err)
	}
	oldSnap, err := stale.DumpSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := peer.LoadSnapshot(ctx, oldSnap); err != nil {
		t.Fatal(err)
	}
	if got := peer.GetConfigWithDefault(ctx, ProblemReportAgentAPIKey, "fallback"); got != "" {
		t.Fatalf("stale key restored a revoked credential: %q", got)
	}
	unstamped := newTestStore(t)
	if err := peer.SetProblemReportAgentAPIKey(ctx, "prk_again_key_12345"); err != nil {
		t.Fatal(err)
	}
	if err := unstamped.SetConfig(ctx, ProblemReportAgentAPIKey, ""); err != nil {
		t.Fatal(err)
	}
	if err := unstamped.SetConfig(ctx, ProblemReportAgentAPIKeyAt, ""); err != nil {
		t.Fatal(err)
	}
	blank, err := unstamped.DumpSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := peer.LoadSnapshot(ctx, blank); err != nil {
		t.Fatal(err)
	}
	if got := peer.GetConfigWithDefault(ctx, ProblemReportAgentAPIKey, ""); got != "prk_again_key_12345" {
		t.Fatalf("unstamped blank wiped the key: %q", got)
	}
	stamp := peer.GetConfigWithDefault(ctx, ProblemReportAgentAPIKeyAt, "")
	sameInstant := newTestStore(t)
	if err := sameInstant.SetConfig(ctx, ProblemReportAgentAPIKey, ""); err != nil {
		t.Fatal(err)
	}
	if err := sameInstant.SetConfig(ctx, ProblemReportAgentAPIKeyAt, stamp); err != nil {
		t.Fatal(err)
	}
	tied, err := sameInstant.DumpSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := peer.LoadSnapshot(ctx, tied); err != nil {
		t.Fatal(err)
	}
	if got := peer.GetConfigWithDefault(ctx, ProblemReportAgentAPIKey, "fallback"); got != "" {
		t.Fatalf("same-instant revoke kept the key: %q", got)
	}
}

func TestProblemReportAgentAPIKeySecondRevokeKeepsStamp(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	if err := store.SetProblemReportAgentAPIKey(ctx, "prk_live_key_123456"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetProblemReportAgentAPIKey(ctx, ""); err != nil {
		t.Fatal(err)
	}
	stamp := store.GetConfigWithDefault(ctx, ProblemReportAgentAPIKeyAt, "")
	if stamp == "" {
		t.Fatal("revoke did not record a stamp")
	}
	if err := store.SetProblemReportAgentAPIKey(ctx, ""); err != nil {
		t.Fatal(err)
	}
	if got := store.GetConfigWithDefault(ctx, ProblemReportAgentAPIKeyAt, ""); got != stamp {
		t.Fatalf("second revoke moved the stamp from %q to %q", stamp, got)
	}
}

func TestProblemReportAgentAPIKeySameInstantTextDoesNotReplace(t *testing.T) {
	peer := newTestStore(t)
	ctx := context.Background()
	if err := peer.SetProblemReportAgentAPIKey(ctx, "prk_live_key_123456"); err != nil {
		t.Fatal(err)
	}
	const whole = "2026-11-19T03:04:05Z"
	if err := peer.SetConfig(ctx, ProblemReportAgentAPIKeyAt, whole); err != nil {
		t.Fatal(err)
	}
	other := newTestStore(t)
	if err := other.SetConfig(ctx, ProblemReportAgentAPIKey, "prk_other_key_123456"); err != nil {
		t.Fatal(err)
	}
	if err := other.SetConfig(ctx, ProblemReportAgentAPIKeyAt, "2026-11-19T03:04:05.000000000Z"); err != nil {
		t.Fatal(err)
	}
	snap, err := other.DumpSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := peer.LoadSnapshot(ctx, snap); err != nil {
		t.Fatal(err)
	}
	if got := peer.GetConfigWithDefault(ctx, ProblemReportAgentAPIKey, ""); got != "prk_live_key_123456" {
		t.Fatalf("same instant written with a fraction replaced the key: %q", got)
	}
	revoker := newTestStore(t)
	if err := revoker.SetConfig(ctx, ProblemReportAgentAPIKey, ""); err != nil {
		t.Fatal(err)
	}
	if err := revoker.SetConfig(ctx, ProblemReportAgentAPIKeyAt, "2026-11-19T03:04:05.000000000Z"); err != nil {
		t.Fatal(err)
	}
	revoked, err := revoker.DumpSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := peer.LoadSnapshot(ctx, revoked); err != nil {
		t.Fatal(err)
	}
	if got := peer.GetConfigWithDefault(ctx, ProblemReportAgentAPIKey, "fallback"); got != "" {
		t.Fatalf("same-instant revoke in fractional form kept the key: %q", got)
	}
}

func TestProblemReportSnapshotReplicatesMetadataWithoutAttachments(t *testing.T) {
	origin := newTestStore(t)
	now := time.Now().UTC()
	token := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	report := &ProblemReport{ID: "BR-ha", ReporterUserID: "user-1", ReporterContact: "user@example.com", OSVersion: "Linux", Description: "fails", Status: "pending", DiagnosticsPath: "diagnostics.zip", ScreenshotPaths: []string{"screenshot-01.png"}, OriginURL: "https://origin.example.com", PublicDownloadToken: token, CreatedAt: now, UpdatedAt: now}
	if err := origin.CreateProblemReport(context.Background(), report); err != nil {
		t.Fatal(err)
	}
	snapshot, err := origin.DumpSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.ProblemReports) != 1 || snapshot.ProblemReports[0].DiagnosticsPath != "" || len(snapshot.ProblemReports[0].ScreenshotPaths) != 0 || snapshot.ProblemReports[0].PublicDownloadToken != token {
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
	if got.OriginURL != "https://origin.example.com" || got.DiagnosticsPath != "" || len(got.ScreenshotPaths) != 0 || got.PublicDownloadToken != token {
		t.Fatalf("unexpected replicated report: %+v", got)
	}
}

func TestProblemReportSnapshotKeepsDownloadTokenWhenReplicaOmitsIt(t *testing.T) {
	origin := newTestStore(t)
	peer := newTestStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	token := "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"
	if err := peer.CreateProblemReport(context.Background(), &ProblemReport{ID: "BR-token", ReporterUserID: "user-1", ReporterContact: "user@example.com", OSVersion: "Linux", Description: "peer", Status: "pending", OriginURL: "https://peer.example.com", PublicDownloadToken: token, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	later := now.Add(2 * time.Second)
	if err := origin.CreateProblemReport(context.Background(), &ProblemReport{ID: "BR-token", ReporterUserID: "user-1", ReporterContact: "user@example.com", OSVersion: "Linux", Description: "origin", Status: "pending", OriginURL: "https://origin.example.com", CreatedAt: now, UpdatedAt: later}); err != nil {
		t.Fatal(err)
	}
	snapshot, err := origin.DumpSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.ProblemReports) != 1 || snapshot.ProblemReports[0].PublicDownloadToken != "" {
		t.Fatalf("origin snapshot should carry an empty token: %+v", snapshot.ProblemReports)
	}
	if err := peer.LoadSnapshot(context.Background(), snapshot); err != nil {
		t.Fatal(err)
	}
	got, err := peer.GetProblemReport(context.Background(), "BR-token")
	if err != nil {
		t.Fatal(err)
	}
	if got.Description != "origin" || got.PublicDownloadToken != token || got.DiagnosticsPath != "" {
		t.Fatalf("empty replica token erased the public link: %+v", got)
	}
}

func TestProblemReportSnapshotFillsDownloadTokenWithoutTakingOlderFields(t *testing.T) {
	peer := newTestStore(t)
	origin := newTestStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	token := strings.Repeat("cd", 32)
	same := now
	if err := peer.CreateProblemReport(context.Background(), &ProblemReport{ID: "BR-same", ReporterUserID: "user-1", ReporterContact: "user@example.com", OSVersion: "Linux", Description: "local", Status: "fixed", OriginURL: "https://peer.example.com", CreatedAt: same, UpdatedAt: same}); err != nil {
		t.Fatal(err)
	}
	newer := same.Add(5 * time.Second)
	if err := peer.CreateProblemReport(context.Background(), &ProblemReport{ID: "BR-newer", ReporterUserID: "user-1", ReporterContact: "user@example.com", OSVersion: "Linux", Description: "newer-status", Status: "fixed", OriginURL: "https://peer.example.com", CreatedAt: same, UpdatedAt: newer}); err != nil {
		t.Fatal(err)
	}
	if err := origin.CreateProblemReport(context.Background(), &ProblemReport{ID: "BR-same", ReporterUserID: "user-1", ReporterContact: "user@example.com", OSVersion: "Linux", Description: "stale", Status: "pending", OriginURL: "https://origin.example.com", PublicDownloadToken: token, CreatedAt: same, UpdatedAt: same}); err != nil {
		t.Fatal(err)
	}
	if err := origin.CreateProblemReport(context.Background(), &ProblemReport{ID: "BR-newer", ReporterUserID: "user-1", ReporterContact: "user@example.com", OSVersion: "Linux", Description: "stale", Status: "pending", OriginURL: "https://origin.example.com", PublicDownloadToken: token, CreatedAt: same, UpdatedAt: same}); err != nil {
		t.Fatal(err)
	}
	snapshot, err := origin.DumpSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := peer.LoadSnapshot(context.Background(), snapshot); err != nil {
		t.Fatal(err)
	}
	sameRow, err := peer.GetProblemReport(context.Background(), "BR-same")
	if err != nil {
		t.Fatal(err)
	}
	if sameRow.Description != "local" || sameRow.Status != "fixed" || sameRow.PublicDownloadToken != token {
		t.Fatalf("same-revision snapshot rewrote the row or dropped the token: %+v", sameRow)
	}
	newerRow, err := peer.GetProblemReport(context.Background(), "BR-newer")
	if err != nil {
		t.Fatal(err)
	}
	if newerRow.Description != "newer-status" || newerRow.Status != "fixed" || newerRow.PublicDownloadToken != token || !newerRow.UpdatedAt.Equal(newer) {
		t.Fatalf("older snapshot took the newer row: %+v", newerRow)
	}
}

func TestProblemReportSnapshotSubsecondRepairBeatsExactSecond(t *testing.T) {
	peer := newTestStore(t)
	origin := newTestStore(t)
	ctx := context.Background()
	base := time.Date(2026, 11, 19, 1, 32, 10, 0, time.UTC)
	repaired := base.Add(200 * time.Millisecond)
	if err := peer.CreateProblemReport(ctx, &ProblemReport{
		ID: "BR-subsecond", ReporterUserID: "user-1", Description: "已修复", Status: "fixed",
		AdminNote: "修了启动卡顿", OriginURL: "https://peer.example.com", CreatedAt: base, UpdatedAt: repaired,
	}); err != nil {
		t.Fatal(err)
	}
	if err := origin.CreateProblemReport(ctx, &ProblemReport{
		ID: "BR-subsecond", ReporterUserID: "user-1", Description: "旧的待处理", Status: "pending",
		OriginURL: "https://origin.example.com", CreatedAt: base, UpdatedAt: base,
	}); err != nil {
		t.Fatal(err)
	}
	stale, err := origin.DumpSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := peer.LoadSnapshot(ctx, stale); err != nil {
		t.Fatal(err)
	}
	got, err := peer.GetProblemReport(ctx, "BR-subsecond")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "fixed" || got.AdminNote != "修了启动卡顿" || !got.UpdatedAt.Equal(repaired) {
		t.Fatalf("exact-second snapshot rolled back the repair: %+v", got)
	}

	fixed, err := peer.DumpSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := origin.LoadSnapshot(ctx, fixed); err != nil {
		t.Fatal(err)
	}
	synced, err := origin.GetProblemReport(ctx, "BR-subsecond")
	if err != nil {
		t.Fatal(err)
	}
	if synced.Status != "fixed" || synced.AdminNote != "修了启动卡顿" {
		t.Fatalf("subsecond repair did not replicate: %+v", synced)
	}
}

func TestProblemReportSnapshotDeleteKeepsNewerSubsecondRow(t *testing.T) {
	peer := newTestStore(t)
	ctx := context.Background()
	base := time.Date(2026, 11, 19, 1, 32, 10, 0, time.UTC)
	repaired := base.Add(200 * time.Millisecond)
	if err := peer.CreateProblemReport(ctx, &ProblemReport{
		ID: "BR-keep", ReporterUserID: "user-1", Description: "仍在", Status: "fixed",
		AdminNote: "刚修好", OriginURL: "https://peer.example.com", CreatedAt: base, UpdatedAt: repaired,
	}); err != nil {
		t.Fatal(err)
	}
	if err := peer.LoadSnapshot(ctx, &Snapshot{ProblemReportDeletes: []ProblemReportTombstone{{
		ID: "BR-keep", DeletedAt: base.Format(timeFmt),
	}}}); err != nil {
		t.Fatal(err)
	}
	got, err := peer.GetProblemReport(ctx, "BR-keep")
	if err != nil || got.Status != "fixed" || got.AdminNote != "刚修好" {
		t.Fatalf("older tombstone removed the repair: %+v err=%v", got, err)
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
