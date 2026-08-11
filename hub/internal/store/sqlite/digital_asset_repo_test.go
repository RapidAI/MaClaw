package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

func TestDigitalAssetRepository_LibraryRoundTrip(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	lib := &store.DigitalAssetLibrary{
		ID:                 "lib_1",
		TenantID:           "tenant_a",
		Name:               "制度库",
		Description:        "desc",
		Status:             store.DigitalAssetStatusActive,
		SyncEnabled:        true,
		ACLMode:            store.DigitalAssetACLRestricted,
		ACLDepartmentsJSON: `["dept_1"]`,
		ACLUsersJSON:       `["a@example.com"]`,
		ContentRev:         0,
		StorePath:          "digital_assets/tenant_a/lib_1",
		CreatedBy:          "admin@example.com",
		UpdatedBy:          "admin@example.com",
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	if err := st.DigitalAssets.CreateLibrary(ctx, lib); err != nil {
		t.Fatalf("CreateLibrary: %v", err)
	}

	got, err := st.DigitalAssets.GetLibrary(ctx, "tenant_a", "lib_1")
	if err != nil {
		t.Fatalf("GetLibrary: %v", err)
	}
	if got == nil {
		t.Fatal("expected library")
	}
	if got.Name != "制度库" || got.ACLMode != store.DigitalAssetACLRestricted || !got.SyncEnabled {
		t.Fatalf("unexpected library: %+v", got)
	}
	// Cross-tenant isolation
	other, err := st.DigitalAssets.GetLibrary(ctx, "tenant_b", "lib_1")
	if err != nil {
		t.Fatalf("GetLibrary other tenant: %v", err)
	}
	if other != nil {
		t.Fatal("expected nil for other tenant")
	}

	items, total, err := st.DigitalAssets.ListLibraries(ctx, store.DigitalAssetLibraryFilter{
		TenantID: "tenant_a",
		Limit:    20,
	})
	if err != nil {
		t.Fatalf("ListLibraries: %v", err)
	}
	if total != 1 || len(items) != 1 {
		t.Fatalf("list total=%d len=%d", total, len(items))
	}

	got.Name = "制度库-更新"
	got.ContentRev = 3
	got.ContentHash = "abc"
	got.UpdatedAt = now.Add(time.Minute)
	if err := st.DigitalAssets.UpdateLibrary(ctx, got); err != nil {
		t.Fatalf("UpdateLibrary: %v", err)
	}
	got2, err := st.DigitalAssets.GetLibrary(ctx, "tenant_a", "lib_1")
	if err != nil || got2 == nil {
		t.Fatalf("reload: %v %#v", err, got2)
	}
	if got2.Name != "制度库-更新" || got2.ContentRev != 3 || got2.ContentHash != "abc" {
		t.Fatalf("update not applied: %+v", got2)
	}

	if err := st.DigitalAssets.ArchiveLibrary(ctx, "tenant_a", "lib_1", now.Add(2*time.Minute), "admin"); err != nil {
		t.Fatalf("ArchiveLibrary: %v", err)
	}
	got3, _ := st.DigitalAssets.GetLibrary(ctx, "tenant_a", "lib_1")
	if got3 == nil || got3.Status != store.DigitalAssetStatusArchived {
		t.Fatalf("expected archived, got %+v", got3)
	}

	if err := st.DigitalAssets.SoftDeleteLibrary(ctx, "tenant_a", "lib_1", now.Add(3*time.Minute), "admin"); err != nil {
		t.Fatalf("SoftDelete: %v", err)
	}
	items, total, err = st.DigitalAssets.ListLibraries(ctx, store.DigitalAssetLibraryFilter{TenantID: "tenant_a"})
	if err != nil {
		t.Fatalf("list after delete: %v", err)
	}
	if total != 0 || len(items) != 0 {
		t.Fatalf("deleted library should be hidden: total=%d", total)
	}
}

func TestDigitalAssetRepository_ChangelogAndJobsAndCursor(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	lib := &store.DigitalAssetLibrary{
		ID: "lib_ch", TenantID: "tenant_a", Name: "ch", Status: store.DigitalAssetStatusActive,
		SyncEnabled: true, ACLMode: store.DigitalAssetACLAllMembers,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := st.DigitalAssets.CreateLibrary(ctx, lib); err != nil {
		t.Fatalf("create: %v", err)
	}

	readyAt := now.Add(time.Second)
	if err := st.DigitalAssets.InsertChangelog(ctx, &store.DigitalAssetChangelog{
		TenantID: "tenant_a", LibraryID: "lib_ch", Rev: 1, Op: "replace_snapshot",
		PackageStatus: "pending", CreatedAt: now,
	}); err != nil {
		t.Fatalf("insert changelog: %v", err)
	}
	if err := st.DigitalAssets.UpdateChangelogPackage(ctx, "tenant_a", "lib_ch", 1, "ready",
		"packages/rev_1.jsonl", "sha1", 100, "hash1", "", &readyAt); err != nil {
		t.Fatalf("update package: %v", err)
	}
	if err := st.DigitalAssets.InsertChangelog(ctx, &store.DigitalAssetChangelog{
		TenantID: "tenant_a", LibraryID: "lib_ch", Rev: 2, Op: "upsert_sources",
		PackageStatus: "ready", PackageRef: "packages/rev_2.jsonl", ContentHash: "hash2",
		CreatedAt: now.Add(2 * time.Second), ReadyAt: &readyAt,
	}); err != nil {
		t.Fatalf("insert rev2: %v", err)
	}

	rows, err := st.DigitalAssets.ListChangelogSince(ctx, "tenant_a", "lib_ch", 0, true, 10)
	if err != nil {
		t.Fatalf("list since: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("want 2 ready revs, got %d", len(rows))
	}
	tip, err := st.DigitalAssets.LatestReadyRev(ctx, "tenant_a", "lib_ch")
	if err != nil || tip != 2 {
		t.Fatalf("LatestReadyRev=%d err=%v", tip, err)
	}

	job := &store.DigitalAssetImportJob{
		ID: "job_1", TenantID: "tenant_a", LibraryID: "lib_ch",
		Kind: "upload", Status: "running", ProgressJSON: `{"phase":"importing"}`,
		CreatedBy: "admin", CreatedAt: now, UpdatedAt: now,
	}
	if err := st.DigitalAssets.CreateJob(ctx, job); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	n, err := st.DigitalAssets.CountRunningJobs(ctx, "tenant_a")
	if err != nil || n != 1 {
		t.Fatalf("CountRunningJobs=%d err=%v", n, err)
	}
	job.Status = "succeeded"
	job.UpdatedAt = now.Add(time.Minute)
	if err := st.DigitalAssets.UpdateJob(ctx, job); err != nil {
		t.Fatalf("UpdateJob: %v", err)
	}
	gotJob, err := st.DigitalAssets.GetJob(ctx, "tenant_a", "job_1")
	if err != nil || gotJob == nil || gotJob.Status != "succeeded" {
		t.Fatalf("GetJob: %#v err=%v", gotJob, err)
	}
	n, _ = st.DigitalAssets.CountRunningJobs(ctx, "tenant_a")
	if n != 0 {
		t.Fatalf("running after succeed: %d", n)
	}

	stale := &store.DigitalAssetImportJob{
		ID: "job_stale", TenantID: "tenant_a", LibraryID: "lib_ch",
		Kind: "upload", Status: "running", ProgressJSON: `{"phase":"importing","percent":40}`,
		CreatedBy: "admin", CreatedAt: now.Add(-3 * time.Hour), UpdatedAt: now.Add(-3 * time.Hour),
	}
	if err := st.DigitalAssets.CreateJob(ctx, stale); err != nil {
		t.Fatalf("CreateJob stale: %v", err)
	}
	n, _ = st.DigitalAssets.CountRunningJobs(ctx, "tenant_a")
	if n != 1 {
		t.Fatalf("running before reclaim: %d", n)
	}
	reclaimed, err := st.DigitalAssets.FailStaleRunningJobs(ctx, "tenant_a", now.Add(-2*time.Hour), "stale test")
	if err != nil || reclaimed != 1 {
		t.Fatalf("FailStaleRunningJobs reclaimed=%d err=%v", reclaimed, err)
	}
	staleGot, err := st.DigitalAssets.GetJob(ctx, "tenant_a", "job_stale")
	if err != nil || staleGot == nil || staleGot.Status != "failed" {
		t.Fatalf("stale job after reclaim: %#v err=%v", staleGot, err)
	}
	if staleGot.ErrorMessage != "stale test" {
		t.Fatalf("stale error message=%q", staleGot.ErrorMessage)
	}
	n, _ = st.DigitalAssets.CountRunningJobs(ctx, "tenant_a")
	if n != 0 {
		t.Fatalf("running after reclaim: %d", n)
	}

	cur := &store.DigitalAssetSyncCursor{
		TenantID: "tenant_a", LibraryID: "lib_ch", UserID: "u1", DeviceID: "d1",
		LastRev: 2, LastSyncAt: now, LastStatus: "ok",
	}
	if err := st.DigitalAssets.UpsertSyncCursor(ctx, cur); err != nil {
		t.Fatalf("UpsertSyncCursor: %v", err)
	}
	cur.LastRev = 2
	cur.LastStatus = "ok2"
	cur.LastSyncAt = now.Add(time.Minute)
	if err := st.DigitalAssets.UpsertSyncCursor(ctx, cur); err != nil {
		t.Fatalf("upsert again: %v", err)
	}
	gotCur, err := st.DigitalAssets.GetSyncCursor(ctx, "tenant_a", "lib_ch", "u1", "d1")
	if err != nil || gotCur == nil || gotCur.LastRev != 2 || gotCur.LastStatus != "ok2" {
		t.Fatalf("GetSyncCursor: %#v err=%v", gotCur, err)
	}
}
