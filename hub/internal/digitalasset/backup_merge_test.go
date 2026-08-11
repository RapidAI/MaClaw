package digitalasset

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

func TestExportImportBackup_RoundTrip(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	lib, err := svc.CreateLibrary(ctx, CreateLibraryInput{
		TenantID: "tenant_a", Name: "BackupMe", Actor: "admin",
		ACL: ACL{Mode: ACLModeAllMembers},
	})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.md"), []byte("# Backup content unique-backup-phrase\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ImportDirectoryIntoLibrary(ctx, "tenant_a", lib.ID, dir, "admin", "local_dir"); err != nil {
		t.Fatalf("import dir: %v", err)
	}

	exp, err := svc.ExportBackup(ctx, ExportBackupInput{
		TenantID: "tenant_a", LibraryIDs: []string{lib.ID}, Actor: "admin",
	})
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if exp.Status != "succeeded" {
		t.Fatalf("export status=%s err=%s", exp.Status, exp.Error)
	}
	if _, err := os.Stat(exp.DownloadPath); err != nil {
		t.Fatalf("zip missing: %v", err)
	}

	job, err := svc.ImportBackup(ctx, ImportBackupInput{
		TenantID: "tenant_a", ZipPath: exp.DownloadPath, Mode: "new_libraries",
		RestoreACL: true, Actor: "admin",
	})
	if err != nil {
		t.Fatalf("import backup: %v", err)
	}
	if job.Status != "succeeded" {
		t.Fatalf("import status=%s err=%s", job.Status, job.ErrorMessage)
	}
	_, total, err := svc.ListLibraries(ctx, store.DigitalAssetLibraryFilter{
		TenantID: "tenant_a", Status: store.DigitalAssetStatusActive, Limit: 50,
	})
	if err != nil {
		t.Fatal(err)
	}
	if total < 2 {
		t.Fatalf("expected >=2 libraries after restore, total=%d", total)
	}
}

func TestMergeLibraries_ArchivesSource(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	target, err := svc.CreateLibrary(ctx, CreateLibraryInput{
		TenantID: "tenant_a", Name: "Target", Actor: "admin", ACL: ACL{Mode: ACLModeAllMembers},
	})
	if err != nil {
		t.Fatal(err)
	}
	src, err := svc.CreateLibrary(ctx, CreateLibraryInput{
		TenantID: "tenant_a", Name: "Source", Actor: "admin", ACL: ACL{Mode: ACLModeAllMembers},
	})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "s.md"), []byte("# merge source unique-merge-phrase\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ImportDirectoryIntoLibrary(ctx, "tenant_a", src.ID, dir, "admin", "local_dir"); err != nil {
		t.Fatalf("import source: %v", err)
	}

	archive := true
	job, err := svc.MergeLibraries(ctx, MergeLibrariesInput{
		TenantID: "tenant_a", TargetLibraryID: target.ID,
		SourceLibraryIDs: []string{src.ID}, ArchiveSources: &archive, Actor: "admin",
	})
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if job.Status != "succeeded" {
		t.Fatalf("status=%s err=%s", job.Status, job.ErrorMessage)
	}
	// Source should no longer be active
	_, err = svc.GetLibrary(ctx, "tenant_a", src.ID)
	if err == nil {
		// GetLibrary rejects deleted but archived is still gettable if status archived
		got, gerr := svc.Repo.GetLibrary(ctx, "tenant_a", src.ID)
		if gerr != nil || got == nil {
			t.Fatalf("get source: %v", gerr)
		}
		if got.Status != store.DigitalAssetStatusArchived {
			t.Fatalf("source status=%s want archived", got.Status)
		}
	}
	tgt, err := svc.GetLibrary(ctx, "tenant_a", target.ID)
	if err != nil {
		t.Fatal(err)
	}
	if tgt.ContentRev < 1 {
		t.Fatalf("target content_rev=%d", tgt.ContentRev)
	}
}
