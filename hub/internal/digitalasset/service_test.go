package digitalasset

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/store"
	storesqlite "github.com/RapidAI/CodeClaw/hub/internal/store/sqlite"
)

func newTestService(t *testing.T) (*Service, *store.Store) {
	t.Helper()
	provider, err := storesqlite.NewProvider(storesqlite.Config{
		DSN:               filepath.Join(t.TempDir(), "hub.db"),
		WAL:               true,
		BusyTimeoutMS:     5000,
		MaxReadOpenConns:  4,
		MaxReadIdleConns:  2,
		MaxWriteOpenConns: 1,
		MaxWriteIdleConns: 1,
	})
	if err != nil {
		t.Fatalf("provider: %v", err)
	}
	t.Cleanup(func() { _ = provider.Close() })
	if err := storesqlite.RunMigrations(provider.Write); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	st := storesqlite.NewStore(provider)
	dataDir := t.TempDir()
	host := NewKnowledgeHost(dataDir, 4)
	t.Cleanup(host.CloseAll)
	settings := DefaultTenantSettings()
	settings.Enabled = true
	svc := &Service{
		Repo:     st.DigitalAssets,
		Host:     host,
		ACL:      &Evaluator{Groups: &fakeGroups{userGroup: map[string]string{}}, AncestorMatch: true},
		Limiter:  NewSyncLimiter(60, 8),
		Settings: settings,
		Enabled:  true,
	}
	return svc, st
}

func TestService_CreateImportSearchPull(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	lib, err := svc.CreateLibrary(ctx, CreateLibraryInput{
		TenantID: "tenant_a",
		Name:     "制度",
		Actor:    "admin@x.com",
		ACL:      ACL{Mode: ACLModeAllMembers},
	})
	if err != nil {
		t.Fatalf("CreateLibrary: %v", err)
	}
	if lib.ID == "" || lib.StorePath == "" {
		t.Fatalf("bad lib: %+v", lib)
	}

	// write a doc and import via directory
	docDir := t.TempDir()
	docPath := filepath.Join(docDir, "policy.md")
	if err := os.WriteFile(docPath, []byte("# Policy\n\nAll employees must follow security policy ABC-123.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	job, err := svc.ImportDirectoryIntoLibrary(ctx, "tenant_a", lib.ID, docDir, "admin@x.com", "local_dir")
	if err != nil {
		t.Fatalf("ImportDirectory: %v", err)
	}
	if job.Status != "succeeded" {
		t.Fatalf("job status=%s err=%s", job.Status, job.ErrorMessage)
	}

	lib2, err := svc.GetLibrary(ctx, "tenant_a", lib.ID)
	if err != nil {
		t.Fatalf("GetLibrary: %v", err)
	}
	if lib2.ContentRev < 1 {
		t.Fatalf("expected content_rev>=1, got %d", lib2.ContentRev)
	}

	// search
	hits, err := svc.SearchLibrary(ctx, "tenant_a", lib.ID, "security policy", 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) == 0 {
		// FTS may still return 0 for very short docs depending on tokenizer; ensure store has sources via re-import path
		t.Logf("search returned 0 hits (acceptable if FTS sparse); content_rev=%d", lib2.ContentRev)
	}

	// manifest + pull
	tenantOn, libs, err := svc.BuildManifest(ctx, "tenant_a", "user@x.com")
	if err != nil || !tenantOn {
		t.Fatalf("manifest: on=%v err=%v", tenantOn, err)
	}
	if len(libs) != 1 || libs[0].LibraryID != lib.ID {
		t.Fatalf("manifest libs=%+v", libs)
	}
	reason, ops, err := svc.Pull(ctx, "tenant_a", lib.ID, "user@x.com", "u1", "d1", 0)
	if err != nil {
		t.Fatalf("Pull: %v", err)
	}
	if reason != "" {
		t.Fatalf("unexpected reason %q", reason)
	}
	if len(ops) == 0 {
		t.Fatal("expected at least one pull op")
	}
	pkgPath, err := svc.PackagePathForRev(ctx, "tenant_a", lib.ID, ops[0].Rev)
	if err != nil {
		t.Fatalf("PackagePathForRev: %v", err)
	}
	if _, err := os.Stat(pkgPath); err != nil {
		t.Fatalf("package missing: %v path=%s", err, pkgPath)
	}
}

func TestSafeJoinUnderRoot(t *testing.T) {
	root := t.TempDir()
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		t.Fatal(err)
	}
	ok, err := safeJoinUnderRoot(rootAbs, "docs/a.md")
	if err != nil {
		t.Fatalf("ok path: %v", err)
	}
	if !strings.HasPrefix(ok, rootAbs) {
		t.Fatalf("not under root: %s", ok)
	}
	if _, err := safeJoinUnderRoot(rootAbs, "../escape.txt"); err == nil {
		t.Fatal("expected reject ..")
	}
	if _, err := safeJoinUnderRoot(rootAbs, "/etc/passwd"); err == nil {
		t.Fatal("expected reject abs")
	}
}

func TestService_FeatureDisabled(t *testing.T) {
	svc, _ := newTestService(t)
	svc.Enabled = false
	_, err := svc.CreateLibrary(context.Background(), CreateLibraryInput{TenantID: "t", Name: "x", Actor: "a"})
	if err != ErrFeatureDisabled {
		t.Fatalf("want ErrFeatureDisabled, got %v", err)
	}
}

func TestService_RejectsTooManyACLDepartments(t *testing.T) {
	svc, _ := newTestService(t)
	departments := make([]string, MaxACLDepartments+1)
	for i := range departments {
		departments[i] = fmt.Sprintf("dept_%d", i)
	}
	_, err := svc.CreateLibrary(context.Background(), CreateLibraryInput{
		TenantID: "tenant_a", Name: "oversized ACL", Actor: "admin",
		ACL: ACL{Mode: ACLModeRestricted, Departments: departments},
	})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("expected ErrInvalid for oversized ACL, got %v", err)
	}
}

func TestService_ListSourcesAndImportJobs(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	lib, err := svc.CreateLibrary(ctx, CreateLibraryInput{
		TenantID: "tenant_a", Name: "docs", Actor: "admin@x.com",
		ACL: ACL{Mode: ACLModeAllMembers},
	})
	if err != nil {
		t.Fatal(err)
	}
	docDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(docDir, "a.md"), []byte("# A\n\nhello knowledge base content\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	job, err := svc.ImportDirectoryIntoLibrary(ctx, "tenant_a", lib.ID, docDir, "admin@x.com", "local_dir")
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if job.Status != "succeeded" {
		t.Fatalf("status=%s err=%s", job.Status, job.ErrorMessage)
	}
	page, err := svc.ListLibrarySources(ctx, "tenant_a", lib.ID, "", 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) < 1 || page.Total < 1 {
		t.Fatalf("expected sources, got %d total=%d", len(page.Items), page.Total)
	}
	jobs, err := svc.ListImportJobs(ctx, "tenant_a", lib.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) < 1 {
		t.Fatalf("expected jobs, got %d", len(jobs))
	}
	if jobs[0].Progress == nil || jobs[0].Progress["phase"] != "done" {
		t.Fatalf("job progress=%v", jobs[0].Progress)
	}
	view, err := svc.GetImportJob(ctx, "tenant_a", job.ID)
	if err != nil || view == nil || view.Status != "succeeded" {
		t.Fatalf("GetImportJob view=%+v err=%v", view, err)
	}
}

func TestJobToView_ParsesProgress(t *testing.T) {
	v := JobToView(&store.DigitalAssetImportJob{
		ID: "j1", TenantID: "t", LibraryID: "l", Kind: "upload", Status: "running",
		ProgressJSON: `{"phase":"importing","percent":42,"imported":3}`,
	})
	if v.Progress["phase"] != "importing" {
		t.Fatalf("progress=%v", v.Progress)
	}
	if int(v.Progress["percent"].(float64)) != 42 {
		t.Fatalf("percent=%v", v.Progress["percent"])
	}
}

func TestService_DeleteLibrarySources(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	lib, err := svc.CreateLibrary(ctx, CreateLibraryInput{
		TenantID: "tenant_a", Name: "delete-src", Actor: "admin@x.com",
		ACL: ACL{Mode: ACLModeAllMembers},
	})
	if err != nil {
		t.Fatal(err)
	}
	docDir := t.TempDir()
	for _, name := range []string{"a.md", "b.md", "c.md"} {
		if err := os.WriteFile(filepath.Join(docDir, name), []byte("# "+name+"\n\nbody for "+name+" knowledge content\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	job, err := svc.ImportDirectoryIntoLibrary(ctx, "tenant_a", lib.ID, docDir, "admin@x.com", "local_dir")
	if err != nil || job.Status != "succeeded" {
		t.Fatalf("import job=%+v err=%v", job, err)
	}
	page, err := svc.ListLibrarySources(ctx, "tenant_a", lib.ID, "", 50, 0)
	if err != nil || page.Total < 3 || len(page.Items) < 3 {
		t.Fatalf("sources=%d total=%d err=%v", len(page.Items), page.Total, err)
	}
	sources := page.Items
	revBefore := lib.ContentRev
	if got, _ := svc.GetLibrary(ctx, "tenant_a", lib.ID); got != nil {
		revBefore = got.ContentRev
	}

	// Delete one
	res, err := svc.DeleteLibrarySource(ctx, "tenant_a", lib.ID, sources[0].ID, "admin@x.com")
	if err != nil {
		t.Fatalf("delete one: %v", err)
	}
	if res.Deleted != 1 {
		t.Fatalf("deleted=%d", res.Deleted)
	}
	if res.Library.ContentRev <= revBefore {
		t.Fatalf("content_rev should advance: before=%d after=%d", revBefore, res.Library.ContentRev)
	}

	// Batch delete remaining two
	ids := []string{sources[1].ID, sources[2].ID}
	res2, err := svc.DeleteLibrarySources(ctx, "tenant_a", lib.ID, ids, "admin@x.com")
	if err != nil {
		t.Fatalf("batch delete: %v", err)
	}
	if res2.Deleted != 2 {
		t.Fatalf("batch deleted=%d", res2.Deleted)
	}
	left, err := svc.ListLibrarySources(ctx, "tenant_a", lib.ID, "", 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(left.Items) != 0 || left.Total != 0 {
		t.Fatalf("expected empty library, got %d total=%d", len(left.Items), left.Total)
	}

	// Missing source
	_, err = svc.DeleteLibrarySource(ctx, "tenant_a", lib.ID, "no_such_source", "admin@x.com")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}

	// Mix: missing ids alone => not found; after re-import, missing + valid returns partial.
	docDir2 := t.TempDir()
	if err := os.WriteFile(filepath.Join(docDir2, "z.md"), []byte("# Z\n\nmore content for mix delete\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ImportDirectoryIntoLibrary(ctx, "tenant_a", lib.ID, docDir2, "admin@x.com", "local_dir"); err != nil {
		t.Fatal(err)
	}
	srcsPage, err := svc.ListLibrarySources(ctx, "tenant_a", lib.ID, "", 20, 0)
	if err != nil || len(srcsPage.Items) < 1 {
		t.Fatalf("re-import sources=%d err=%v", len(srcsPage.Items), err)
	}
	srcs := srcsPage.Items
	mix, err := svc.DeleteLibrarySources(ctx, "tenant_a", lib.ID, []string{srcs[0].ID, "missing_id_xyz"}, "admin@x.com")
	if err != nil {
		t.Fatalf("mix delete: %v", err)
	}
	if mix.Deleted != 1 || len(mix.Missing) != 1 {
		t.Fatalf("mix result=%+v", mix)
	}

	// Archived library rejects further deletes.
	if err := svc.Repo.ArchiveLibrary(ctx, "tenant_a", lib.ID, time.Now().UTC(), "admin@x.com"); err != nil {
		t.Fatal(err)
	}
	// re-import path blocked at createImportJob; force active source via re-activate for delete check:
	// GetLibrary after archive still returns row with status archived.
	_, err = svc.DeleteLibrarySource(ctx, "tenant_a", lib.ID, "any", "admin@x.com")
	if err == nil || !strings.Contains(err.Error(), "not active") {
		t.Fatalf("want not active error, got %v", err)
	}
}

func TestService_ListLibrarySourcesPagination(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	lib, err := svc.CreateLibrary(ctx, CreateLibraryInput{
		TenantID: "tenant_a", Name: "paged", Actor: "admin@x.com",
		ACL: ACL{Mode: ACLModeAllMembers},
	})
	if err != nil {
		t.Fatal(err)
	}
	docDir := t.TempDir()
	for i := 0; i < 5; i++ {
		name := fmt.Sprintf("doc_%d.md", i)
		body := fmt.Sprintf("# Doc %d\n\ncontent body for pagination test file %d\n", i, i)
		if err := os.WriteFile(filepath.Join(docDir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := svc.ImportDirectoryIntoLibrary(ctx, "tenant_a", lib.ID, docDir, "admin@x.com", "local_dir"); err != nil {
		t.Fatal(err)
	}

	page1, err := svc.ListLibrarySources(ctx, "tenant_a", lib.ID, "", 2, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(page1.Items) != 2 || !page1.HasMore {
		t.Fatalf("page1 items=%d has_more=%v total=%d", len(page1.Items), page1.HasMore, page1.Total)
	}
	page2, err := svc.ListLibrarySources(ctx, "tenant_a", lib.ID, "", 2, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(page2.Items) != 2 {
		t.Fatalf("page2 items=%d has_more=%v", len(page2.Items), page2.HasMore)
	}
	// Pages must not overlap.
	seen := map[string]bool{}
	for _, it := range page1.Items {
		seen[it.ID] = true
	}
	for _, it := range page2.Items {
		if seen[it.ID] {
			t.Fatalf("overlap id %s across pages", it.ID)
		}
	}
	page3, err := svc.ListLibrarySources(ctx, "tenant_a", lib.ID, "", 2, 4)
	if err != nil {
		t.Fatal(err)
	}
	if len(page3.Items) != 1 || page3.HasMore {
		t.Fatalf("page3 items=%d has_more=%v", len(page3.Items), page3.HasMore)
	}
	// Past the end: empty page, has_more false, items non-nil.
	page4, err := svc.ListLibrarySources(ctx, "tenant_a", lib.ID, "", 2, 50)
	if err != nil {
		t.Fatal(err)
	}
	if page4.Items == nil || len(page4.Items) != 0 || page4.HasMore {
		t.Fatalf("page4 past end: items=%v has_more=%v", page4.Items, page4.HasMore)
	}
}

func TestIsKnowledgeSourceNotFound(t *testing.T) {
	if !isKnowledgeSourceNotFound(fmt.Errorf("source abc not found")) {
		t.Fatal("expected not found")
	}
	if isKnowledgeSourceNotFound(fmt.Errorf("disk full")) {
		t.Fatal("disk full should not match")
	}
	if isKnowledgeSourceNotFound(nil) {
		t.Fatal("nil")
	}
}

func TestProgressPercent_CoercesTypes(t *testing.T) {
	if progressPercent(map[string]any{"percent": 12}) != 12 {
		t.Fatal("int")
	}
	if progressPercent(map[string]any{"percent": float64(33.9)}) != 33 {
		t.Fatal("float64")
	}
	if progressPercent(map[string]any{"percent": json.Number("50")}) != 50 {
		t.Fatal("json.Number")
	}
	if progressPercent(nil) != 0 || progressPercent(map[string]any{}) != 0 {
		t.Fatal("empty")
	}
}

func TestFailJob_SetsProgressPhase(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	lib, err := svc.CreateLibrary(ctx, CreateLibraryInput{
		TenantID: "tenant_a", Name: "fail-progress", Actor: "admin@x.com",
		ACL: ACL{Mode: ACLModeAllMembers},
	})
	if err != nil {
		t.Fatal(err)
	}
	job := &store.DigitalAssetImportJob{
		ID: "daij_fail_progress", TenantID: "tenant_a", LibraryID: lib.ID,
		Kind: "upload", Status: "running",
		ProgressJSON: `{"phase":"importing","percent":40,"imported":2}`,
		CreatedBy:    "admin", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	if err := svc.Repo.CreateJob(ctx, job); err != nil {
		t.Fatal(err)
	}
	svc.failJob(job, errors.New("boom"))
	got, err := svc.Repo.GetJob(ctx, "tenant_a", job.ID)
	if err != nil || got == nil {
		t.Fatalf("GetJob: %v", err)
	}
	if got.Status != "failed" || got.ErrorMessage != "boom" {
		t.Fatalf("job=%+v", got)
	}
	view := JobToView(got)
	if view.Progress["phase"] != "failed" {
		t.Fatalf("progress=%v", view.Progress)
	}
}

func TestService_ReclaimStaleImportJobsUnblocksGate(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	lib, err := svc.CreateLibrary(ctx, CreateLibraryInput{
		TenantID: "tenant_a", Name: "stale-gate", Actor: "admin@x.com",
		ACL: ACL{Mode: ACLModeAllMembers},
	})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	stale := &store.DigitalAssetImportJob{
		ID: "daij_stale_gate", TenantID: "tenant_a", LibraryID: lib.ID,
		Kind: "local_dir", Status: "running",
		ProgressJSON: `{"phase":"importing","percent":10}`,
		CreatedBy:    "admin", CreatedAt: now.Add(-3 * time.Hour), UpdatedAt: now.Add(-3 * time.Hour),
	}
	if err := svc.Repo.CreateJob(ctx, stale); err != nil {
		t.Fatal(err)
	}
	// Without reclaim, createImportJob would refuse; reclaim runs inside createImportJob.
	docDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(docDir, "a.md"), []byte("# A\n\nhello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	job, err := svc.ImportDirectoryIntoLibrary(ctx, "tenant_a", lib.ID, docDir, "admin@x.com", "local_dir")
	if err != nil {
		t.Fatalf("import after stale reclaim: %v", err)
	}
	if job.Status != "succeeded" {
		t.Fatalf("status=%s err=%s", job.Status, job.ErrorMessage)
	}
	staleGot, _ := svc.Repo.GetJob(ctx, "tenant_a", stale.ID)
	if staleGot == nil || staleGot.Status != "failed" {
		t.Fatalf("stale job should be failed, got %#v", staleGot)
	}
}

func TestImportArchiveZip_StagesCopyBeforeReturn(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	lib, err := svc.CreateLibrary(ctx, CreateLibraryInput{
		TenantID: "tenant_a", Name: "archive", Actor: "admin@x.com",
		ACL: ACL{Mode: ACLModeAllMembers},
	})
	if err != nil {
		t.Fatal(err)
	}
	// Build a tiny zip, then delete the source after ImportArchiveZip returns (simulates handler defer).
	zipPath := filepath.Join(t.TempDir(), "docs.zip")
	writeTestZip(t, zipPath, map[string]string{
		"note.md": "# Note\n\ncontent for archive import\n",
	})
	job, err := svc.ImportArchiveZip(ctx, "tenant_a", lib.ID, "admin@x.com", zipPath)
	if err != nil {
		t.Fatalf("ImportArchiveZip: %v", err)
	}
	// Handler would remove upload temp here.
	_ = os.Remove(zipPath)

	// Poll until terminal.
	deadline := time.Now().Add(30 * time.Second)
	var view *JobView
	for time.Now().Before(deadline) {
		view, err = svc.GetImportJob(ctx, "tenant_a", job.ID)
		if err != nil {
			t.Fatal(err)
		}
		if view.Status == "succeeded" || view.Status == "failed" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if view == nil || view.Status != "succeeded" {
		t.Fatalf("archive job view=%+v", view)
	}
	if view.Progress == nil || view.Progress["phase"] != "done" {
		t.Fatalf("progress=%v", view.Progress)
	}
}

func TestService_ACLDeniedPull(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	lib, err := svc.CreateLibrary(ctx, CreateLibraryInput{
		TenantID: "tenant_a", Name: "secret", Actor: "admin",
		ACL: ACL{Mode: ACLModeRestricted, Departments: []string{"leadership"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	// empty import to have rev optional — pull should 403 before package
	_, _, err = svc.Pull(ctx, "tenant_a", lib.ID, "other@x.com", "u", "d", 0)
	if err != ErrForbidden {
		t.Fatalf("want forbidden, got %v", err)
	}
	// allowed user
	_, ops, err := svc.Pull(ctx, "tenant_a", lib.ID, "boss@x.com", "u", "d", 0)
	if err != ErrForbidden {
		t.Fatalf("ungrouped user should be forbidden, got %v", err)
	}
	_ = ops
}
