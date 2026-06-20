package main

import (
	"archive/zip"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	coreknowledge "github.com/RapidAI/CodeClaw/corelib/knowledge"
	corememory "github.com/RapidAI/CodeClaw/corelib/memory"
)

func TestUserDataMigrationMemoryRestoreReplacesExistingEntries(t *testing.T) {
	store, err := corememory.NewStoreWithMode(t.TempDir(), corememory.StoreModeJSON)
	if err != nil {
		t.Fatalf("NewStoreWithMode: %v", err)
	}
	t.Cleanup(store.Stop)

	app := &App{memoryStore: store}
	if err := store.UpsertEntriesByID([]corememory.Entry{
		userDataMigrationTestEntry("old", "old migration memory"),
		userDataMigrationTestEntry("keep", "memory before migration"),
	}); err != nil {
		t.Fatalf("seed memory: %v", err)
	}

	if err := app.restoreUserDataMigrationMemory([]corememory.Entry{
		userDataMigrationTestEntry("keep", "memory after migration"),
		userDataMigrationTestEntry("new", "new migration memory"),
	}); err != nil {
		t.Fatalf("restore memory: %v", err)
	}

	got := map[string]string{}
	for _, entry := range store.List("", "") {
		got[entry.ID] = entry.Content
	}
	if _, ok := got["old"]; ok {
		t.Fatalf("old entry should have been removed: %#v", got)
	}
	if got["keep"] != "memory after migration" || got["new"] != "new migration memory" {
		t.Fatalf("unexpected restored memory: %#v", got)
	}
}

func TestUserDataMigrationKnowledgeAssetsRollbackRestoresExisting(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	oldPath := filepath.Join(app.GetDataDir(), "knowledge_assets", "source-old", "old.txt")
	if err := os.MkdirAll(filepath.Dir(oldPath), 0o755); err != nil {
		t.Fatalf("mkdir old assets: %v", err)
	}
	if err := os.WriteFile(oldPath, []byte("old asset"), 0o600); err != nil {
		t.Fatalf("write old asset: %v", err)
	}

	assetSrc := filepath.Join(t.TempDir(), "knowledge_assets")
	newPath := filepath.Join(assetSrc, "source-new", "new.txt")
	if err := os.MkdirAll(filepath.Dir(newPath), 0o755); err != nil {
		t.Fatalf("mkdir new assets: %v", err)
	}
	if err := os.WriteFile(newPath, []byte("new asset"), 0o600); err != nil {
		t.Fatalf("write new asset: %v", err)
	}

	_, rollback, commit, err := app.replaceUserDataMigrationKnowledgeAssets(assetSrc, t.TempDir())
	if err != nil {
		t.Fatalf("replace assets: %v", err)
	}
	if _, err := os.Stat(filepath.Join(app.GetDataDir(), "knowledge_assets", "source-new", "new.txt")); err != nil {
		t.Fatalf("new asset not installed: %v", err)
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("old asset should be moved aside before rollback, err=%v", err)
	}

	if err := rollback(); err != nil {
		t.Fatalf("rollback assets: %v", err)
	}
	if _, err := os.Stat(oldPath); err != nil {
		t.Fatalf("old asset not restored: %v", err)
	}
	if _, err := os.Stat(filepath.Join(app.GetDataDir(), "knowledge_assets", "source-new", "new.txt")); !os.IsNotExist(err) {
		t.Fatalf("new asset should be removed by rollback, err=%v", err)
	}
	if commit != nil {
		commit()
	}
}

func TestUserDataMigrationBuildRejectsInvalidKnowledgeAssetsPath(t *testing.T) {
	store, err := corememory.NewStoreWithMode(t.TempDir(), corememory.StoreModeJSON)
	if err != nil {
		t.Fatalf("NewStoreWithMode: %v", err)
	}
	t.Cleanup(store.Stop)

	app := &App{testHomeDir: t.TempDir(), memoryStore: store}
	assetPath := filepath.Join(app.GetDataDir(), "knowledge_assets")
	if err := os.MkdirAll(filepath.Dir(assetPath), 0o755); err != nil {
		t.Fatalf("mkdir data dir: %v", err)
	}
	if err := os.WriteFile(assetPath, []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("write invalid assets path: %v", err)
	}

	_, _, err = app.buildUserDataMigrationPackage(context.Background(), userDataMigrationClientConfig{
		TenantID:    "tenant-a",
		UserID:      "user-a",
		MachineID:   "machine-a",
		MachineName: "Machine A",
	}, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "knowledge_assets") {
		t.Fatalf("expected knowledge_assets error, got %v", err)
	}
}

func TestUserDataMigrationPackageKeepsAssetManifestFiles(t *testing.T) {
	sourceStore, err := corememory.NewStoreWithMode(t.TempDir(), corememory.StoreModeJSON)
	if err != nil {
		t.Fatalf("NewStoreWithMode source: %v", err)
	}
	t.Cleanup(sourceStore.Stop)
	source := &App{testHomeDir: t.TempDir(), memoryStore: sourceStore}
	assetPath := filepath.Join(source.GetDataDir(), "knowledge_assets", "source-a", "manifest.json")
	if err := os.MkdirAll(filepath.Dir(assetPath), 0o755); err != nil {
		t.Fatalf("mkdir asset path: %v", err)
	}
	if err := os.WriteFile(assetPath, []byte("asset manifest"), 0o600); err != nil {
		t.Fatalf("write asset manifest: %v", err)
	}

	zipPath, manifest, err := source.buildUserDataMigrationPackage(context.Background(), userDataMigrationClientConfig{
		TenantID:    "tenant-a",
		UserID:      "user-a",
		MachineID:   "machine-a",
		MachineName: "Machine A",
	}, t.TempDir())
	if err != nil {
		t.Fatalf("build package: %v", err)
	}
	found := false
	for _, file := range manifest.Files {
		if file.Path == "knowledge_assets/source-a/manifest.json" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("asset manifest file missing from package manifest: %#v", manifest.Files)
	}

	targetStore, err := corememory.NewStoreWithMode(t.TempDir(), corememory.StoreModeJSON)
	if err != nil {
		t.Fatalf("NewStoreWithMode target: %v", err)
	}
	t.Cleanup(targetStore.Stop)
	target := &App{testHomeDir: t.TempDir(), memoryStore: targetStore}
	if _, err := target.restoreUserDataMigrationPackage(context.Background(), zipPath, t.TempDir()); err != nil {
		t.Fatalf("restore package: %v", err)
	}
	restored, err := os.ReadFile(filepath.Join(target.GetDataDir(), "knowledge_assets", "source-a", "manifest.json"))
	if err != nil {
		t.Fatalf("read restored asset manifest: %v", err)
	}
	if string(restored) != "asset manifest" {
		t.Fatalf("restored asset manifest mismatch: %q", restored)
	}
}

func TestUserDataMigrationEncryptionRequiresPasswordAndPlainHash(t *testing.T) {
	dir := t.TempDir()
	plainPath := filepath.Join(dir, "migration.zip")
	plain := []byte("migration package bytes")
	if err := os.WriteFile(plainPath, plain, 0o600); err != nil {
		t.Fatalf("write plain package: %v", err)
	}
	plainHash, _, err := userDataMigrationFileSHA256(plainPath)
	if err != nil {
		t.Fatalf("hash plain package: %v", err)
	}

	encryptedPath := filepath.Join(dir, "migration.mlawenc")
	if err := encryptUserDataMigrationFile(plainPath, encryptedPath, "correct-password", plainHash); err != nil {
		t.Fatalf("encrypt package: %v", err)
	}

	if err := decryptUserDataMigrationFile(encryptedPath, filepath.Join(dir, "wrong-password.zip"), "wrong-password", plainHash); err == nil || !strings.Contains(err.Error(), "password") {
		t.Fatalf("expected wrong password error, got %v", err)
	}
	if err := decryptUserDataMigrationFile(encryptedPath, filepath.Join(dir, "wrong-hash.zip"), "correct-password", strings.Repeat("0", 64)); err == nil || !strings.Contains(err.Error(), "hash mismatch") {
		t.Fatalf("expected hash mismatch error, got %v", err)
	}

	restoredPath := filepath.Join(dir, "restored.zip")
	if err := decryptUserDataMigrationFile(encryptedPath, restoredPath, "correct-password", plainHash); err != nil {
		t.Fatalf("decrypt package: %v", err)
	}
	restored, err := os.ReadFile(restoredPath)
	if err != nil {
		t.Fatalf("read restored package: %v", err)
	}
	if string(restored) != string(plain) {
		t.Fatalf("restored package mismatch: %q", restored)
	}
}

func TestUserDataMigrationKnowledgeImportReplacesExistingSnapshot(t *testing.T) {
	target := &App{testHomeDir: t.TempDir()}
	oldSource, err := target.KnowledgeSaveText(coreknowledge.TextSaveRequest{
		Title:       "Old local note",
		Text:        "old local migration note",
		SaveScope:   coreknowledge.SaveScopePersonal,
		DistillMode: coreknowledge.DistillModeRules,
	})
	if err != nil {
		t.Fatalf("seed old knowledge: %v", err)
	}

	source := &App{testHomeDir: t.TempDir()}
	newSource, err := source.KnowledgeSaveText(coreknowledge.TextSaveRequest{
		Title:       "Migrated note",
		Text:        "new migrated knowledge note",
		SaveScope:   coreknowledge.SaveScopePersonal,
		DistillMode: coreknowledge.DistillModeRules,
	})
	if err != nil {
		t.Fatalf("seed migrated knowledge: %v", err)
	}
	snapshotPath := filepath.Join(t.TempDir(), "knowledge_snapshot.jsonl")
	if _, err := source.KnowledgeExportSnapshotWithOptions(coreknowledge.ExportOptions{OutputPath: snapshotPath}); err != nil {
		t.Fatalf("export migrated snapshot: %v", err)
	}

	if _, err := target.importUserDataMigrationKnowledgeSnapshot(snapshotPath, t.TempDir()); err != nil {
		t.Fatalf("import migrated snapshot: %v", err)
	}
	sources := userDataMigrationKnowledgeSourceIDs(t, target)
	if _, ok := sources[oldSource.ID]; ok {
		t.Fatalf("old knowledge source should be replaced, sources=%#v", sources)
	}
	if _, ok := sources[newSource.ID]; !ok {
		t.Fatalf("migrated knowledge source missing, sources=%#v", sources)
	}
}

func TestUserDataMigrationKnowledgeImportDoesNotDeleteOpenDatabaseFile(t *testing.T) {
	target := &App{testHomeDir: t.TempDir()}
	oldSource, err := target.KnowledgeSaveText(coreknowledge.TextSaveRequest{
		Title:       "Old local note",
		Text:        "old local migration note",
		SaveScope:   coreknowledge.SaveScopePersonal,
		DistillMode: coreknowledge.DistillModeRules,
	})
	if err != nil {
		t.Fatalf("seed old knowledge: %v", err)
	}
	pinnedStore, err := target.openKnowledgeStore()
	if err != nil {
		t.Fatalf("open pinned knowledge store: %v", err)
	}
	defer pinnedStore.Close()

	source := &App{testHomeDir: t.TempDir()}
	newSource, err := source.KnowledgeSaveText(coreknowledge.TextSaveRequest{
		Title:       "Migrated note",
		Text:        "new migrated knowledge note",
		SaveScope:   coreknowledge.SaveScopePersonal,
		DistillMode: coreknowledge.DistillModeRules,
	})
	if err != nil {
		t.Fatalf("seed migrated knowledge: %v", err)
	}
	snapshotPath := filepath.Join(t.TempDir(), "knowledge_snapshot.jsonl")
	if _, err := source.KnowledgeExportSnapshotWithOptions(coreknowledge.ExportOptions{OutputPath: snapshotPath}); err != nil {
		t.Fatalf("export migrated snapshot: %v", err)
	}

	if _, err := target.importUserDataMigrationKnowledgeSnapshot(snapshotPath, t.TempDir()); err != nil {
		t.Fatalf("import migrated snapshot with open database handle: %v", err)
	}
	sources := userDataMigrationKnowledgeSourceIDs(t, target)
	if _, ok := sources[oldSource.ID]; ok {
		t.Fatalf("old knowledge source should be replaced, sources=%#v", sources)
	}
	if _, ok := sources[newSource.ID]; !ok {
		t.Fatalf("migrated knowledge source missing, sources=%#v", sources)
	}
}

func TestUserDataMigrationKnowledgeImportRollsBackOnFailure(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	oldSource, err := app.KnowledgeSaveText(coreknowledge.TextSaveRequest{
		Title:       "Old local note",
		Text:        "old local note survives failed migration",
		SaveScope:   coreknowledge.SaveScopePersonal,
		DistillMode: coreknowledge.DistillModeRules,
	})
	if err != nil {
		t.Fatalf("seed old knowledge: %v", err)
	}
	brokenPath := filepath.Join(t.TempDir(), "broken_snapshot.jsonl")
	if err := os.WriteFile(brokenPath, []byte(`{"type":"node","data":{"id":"node_missing_source","source_id":"missing_source","text":"broken"}}`+"\n"), 0o600); err != nil {
		t.Fatalf("write broken snapshot: %v", err)
	}

	if _, err := app.importUserDataMigrationKnowledgeSnapshot(brokenPath, t.TempDir()); err == nil {
		t.Fatal("expected broken snapshot import to fail")
	}
	sources := userDataMigrationKnowledgeSourceIDs(t, app)
	if _, ok := sources[oldSource.ID]; !ok {
		t.Fatalf("old knowledge source should be restored after failed import, sources=%#v", sources)
	}
}

func TestUserDataMigrationKnowledgeValidationUsesIsolatedStore(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	existingSource, err := app.KnowledgeSaveText(coreknowledge.TextSaveRequest{
		Title:       "Existing local note",
		Text:        "local note should not satisfy migration snapshot references",
		SaveScope:   coreknowledge.SaveScopePersonal,
		DistillMode: coreknowledge.DistillModeRules,
	})
	if err != nil {
		t.Fatalf("seed existing knowledge: %v", err)
	}
	brokenPath := filepath.Join(t.TempDir(), "broken_snapshot.jsonl")
	if err := os.WriteFile(brokenPath, []byte(`{"type":"node","data":{"id":"node_missing_package_source","source_id":"`+existingSource.ID+`","text":"broken"}}`+"\n"), 0o600); err != nil {
		t.Fatalf("write broken snapshot: %v", err)
	}

	if err := app.validateUserDataMigrationKnowledgeSnapshot(brokenPath); err == nil {
		t.Fatal("expected isolated validation to reject missing package source")
	}
}

func TestUserDataMigrationUnzipRejectsExpandedSizeLimit(t *testing.T) {
	zipPath := filepath.Join(t.TempDir(), "migration.zip")
	out, err := os.Create(zipPath)
	if err != nil {
		t.Fatalf("create zip: %v", err)
	}
	zw := zip.NewWriter(out)
	w, err := zw.Create("payload.txt")
	if err != nil {
		t.Fatalf("create zip entry: %v", err)
	}
	if _, err := w.Write([]byte("too large")); err != nil {
		t.Fatalf("write zip entry: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	if err := out.Close(); err != nil {
		t.Fatalf("close file: %v", err)
	}

	if err := userDataMigrationUnzipToDir(zipPath, filepath.Join(t.TempDir(), "out"), 4); err == nil {
		t.Fatal("expected expanded size limit error")
	}
}

func TestUserDataMigrationSafeJoinRejectsTraversal(t *testing.T) {
	if _, err := userDataMigrationSafeJoin(filepath.Join(t.TempDir(), "root"), "../outside.txt"); err == nil {
		t.Fatal("expected traversal path to be rejected")
	}
}

func TestUserDataMigrationCopyDirRejectsSymlink(t *testing.T) {
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "target.txt"), []byte("target"), 0o600); err != nil {
		t.Fatalf("write target: %v", err)
	}
	if err := os.Symlink("target.txt", filepath.Join(src, "link.txt")); err != nil {
		t.Skipf("symlink not available: %v", err)
	}

	if _, err := userDataMigrationCopyDirInto(t.TempDir(), src, "assets"); err == nil {
		t.Fatal("expected symlink copy to be rejected")
	}
}

func TestUserDataMigrationCleanupRetryUsesCurrentExport(t *testing.T) {
	completeCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/migration/exports/current":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"export": map[string]interface{}{
					"export_id":             "mig-current",
					"status":                "imported",
					"claimed_by_machine_id": "machine-current",
				},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/migration/imports/mig-current/complete":
			completeCalls++
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "status": "deleted"})
		case r.URL.Path == "/api/v1/migration/instances":
			t.Fatalf("cleanup retry should not depend on instances list")
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	app := &App{}
	result, err := app.runUserDataMigrationCleanup(context.Background(), userDataMigrationClientConfig{
		HubURL:       srv.URL,
		MachineID:    "machine-current",
		MachineToken: "token-current",
	}, "mig-current", func(float64, string) {})
	if err != nil {
		t.Fatalf("cleanup retry: %v", err)
	}
	if completeCalls != 1 {
		t.Fatalf("complete calls = %d, want 1", completeCalls)
	}
	if result["cleanup_retried"] != true {
		t.Fatalf("cleanup result missing retry marker: %#v", result)
	}
}

func TestUserDataMigrationCleanupRetryRequiresCurrentMachineClaim(t *testing.T) {
	completeCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/migration/exports/current":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"export": map[string]interface{}{
					"export_id":             "mig-current",
					"status":                "imported",
					"claimed_by_machine_id": "machine-other",
				},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/migration/imports/mig-current/complete":
			completeCalls++
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "status": "deleted"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	app := &App{}
	_, err := app.runUserDataMigrationCleanup(context.Background(), userDataMigrationClientConfig{
		HubURL:       srv.URL,
		MachineID:    "machine-current",
		MachineToken: "token-current",
	}, "mig-current", func(float64, string) {})
	if err == nil || !strings.Contains(err.Error(), "not claimed by this machine") {
		t.Fatalf("expected current machine claim error, got %v", err)
	}
	if completeCalls != 0 {
		t.Fatalf("complete calls = %d, want 0", completeCalls)
	}
}

func TestUserDataMigrationCleanupRetryAllowsCurrentMachineImportingClaim(t *testing.T) {
	clearUserDataMigrationJobsForTest()
	t.Cleanup(clearUserDataMigrationJobsForTest)

	completeCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/migration/exports/current":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"export": map[string]interface{}{
					"export_id":             "mig-current",
					"status":                "importing",
					"claimed_by_machine_id": "machine-current",
				},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/migration/imports/mig-current/complete":
			completeCalls++
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "status": "deleted"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	app := &App{}
	_, err := app.runUserDataMigrationCleanup(context.Background(), userDataMigrationClientConfig{
		HubURL:       srv.URL,
		MachineID:    "machine-current",
		MachineToken: "token-current",
	}, "mig-current", func(float64, string) {})
	if err == nil || !strings.Contains(err.Error(), "not ready for cleanup retry") {
		t.Fatalf("expected importing cleanup retry to require local pending marker, got %v", err)
	}
	if completeCalls != 0 {
		t.Fatalf("complete calls before pending marker = %d, want 0", completeCalls)
	}

	cfg := userDataMigrationClientConfig{
		HubURL:       srv.URL,
		TenantID:     "tenant-a",
		UserID:       "user-a",
		MachineID:    "machine-current",
		MachineToken: "token-current",
	}
	key := userDataMigrationCleanupPendingKey(cfg, "mig-current")
	userDataMigrationCleanupPendingExports.Store(key, time.Now().UTC())
	result, err := app.runUserDataMigrationCleanup(context.Background(), cfg, "mig-current", func(float64, string) {})
	if err != nil {
		t.Fatalf("cleanup retry for importing claim: %v", err)
	}
	if completeCalls != 1 {
		t.Fatalf("complete calls = %d, want 1", completeCalls)
	}
	if result["cleanup_retried"] != true {
		t.Fatalf("cleanup result missing retry marker: %#v", result)
	}
	if _, ok := userDataMigrationCleanupPendingExports.Load(key); ok {
		t.Fatalf("cleanup pending marker should be cleared after cleanup succeeds")
	}
}

func TestUserDataMigrationImportSucceedsWhenHubCleanupFails(t *testing.T) {
	sourceStore, err := corememory.NewStoreWithMode(t.TempDir(), corememory.StoreModeJSON)
	if err != nil {
		t.Fatalf("NewStoreWithMode source: %v", err)
	}
	t.Cleanup(sourceStore.Stop)
	if err := sourceStore.UpsertEntriesByID([]corememory.Entry{
		userDataMigrationTestEntry("memory-new", "new migration memory"),
	}); err != nil {
		t.Fatalf("seed source memory: %v", err)
	}
	source := &App{testHomeDir: t.TempDir(), memoryStore: sourceStore}
	if _, err := source.KnowledgeSaveText(coreknowledge.TextSaveRequest{
		Title:       "Migrated note",
		Text:        "new migrated knowledge note",
		SaveScope:   coreknowledge.SaveScopePersonal,
		DistillMode: coreknowledge.DistillModeRules,
	}); err != nil {
		t.Fatalf("seed source knowledge: %v", err)
	}

	workDir := t.TempDir()
	plainPath, _, err := source.buildUserDataMigrationPackage(context.Background(), userDataMigrationClientConfig{
		TenantID:    "tenant-a",
		UserID:      "user-a",
		MachineID:   "machine-source",
		MachineName: "Source Machine",
	}, workDir)
	if err != nil {
		t.Fatalf("build package: %v", err)
	}
	plainHash, _, err := userDataMigrationFileSHA256(plainPath)
	if err != nil {
		t.Fatalf("hash plain package: %v", err)
	}
	encryptedPath := filepath.Join(workDir, "migration.mlawenc")
	if err := encryptUserDataMigrationFile(plainPath, encryptedPath, "secret-pass", plainHash); err != nil {
		t.Fatalf("encrypt package: %v", err)
	}
	encryptedHash, encryptedSize, err := userDataMigrationFileSHA256(encryptedPath)
	if err != nil {
		t.Fatalf("hash encrypted package: %v", err)
	}
	encryptedBytes, err := os.ReadFile(encryptedPath)
	if err != nil {
		t.Fatalf("read encrypted package: %v", err)
	}

	completeCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/migration/imports/mig-source/claim":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"export": map[string]interface{}{
					"export_id":        "mig-source",
					"status":           "ready",
					"chunk_count":      1,
					"chunk_size":       userDataMigrationChunkSize,
					"encrypted_size":   encryptedSize,
					"encrypted_sha256": encryptedHash,
					"plain_sha256":     plainHash,
				},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/migration/imports/mig-source/chunks/0":
			_, _ = w.Write(encryptedBytes)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/migration/imports/mig-source/complete":
			completeCalls++
			http.Error(w, "cleanup unavailable", http.StatusServiceUnavailable)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	targetStore, err := corememory.NewStoreWithMode(t.TempDir(), corememory.StoreModeJSON)
	if err != nil {
		t.Fatalf("NewStoreWithMode target: %v", err)
	}
	t.Cleanup(targetStore.Stop)
	target := &App{testHomeDir: t.TempDir(), memoryStore: targetStore}
	var lastProgress string
	result, err := target.runUserDataMigrationImport(context.Background(), userDataMigrationClientConfig{
		HubURL:       srv.URL,
		MachineID:    "machine-current",
		MachineToken: "token-current",
	}, "mig-source", "secret-pass", func(_ float64, text string) {
		lastProgress = text
	})
	if err != nil {
		t.Fatalf("import should succeed with cleanup pending: %v", err)
	}
	if result["cleanup_pending"] != true {
		t.Fatalf("expected cleanup_pending result, got %#v", result)
	}
	if strings.TrimSpace(result["cleanup_error"].(string)) == "" {
		t.Fatalf("expected cleanup_error detail, got %#v", result)
	}
	if lastProgress != "local import completed; Hub cleanup can be retried" {
		t.Fatalf("last progress = %q", lastProgress)
	}
	if completeCalls != 3 {
		t.Fatalf("complete calls = %d, want 3", completeCalls)
	}
	if got := targetStore.List("", ""); len(got) != 1 || got[0].ID != "memory-new" {
		t.Fatalf("memory not restored: %#v", got)
	}
}

func TestUserDataMigrationRejectsConcurrentJobs(t *testing.T) {
	clearUserDataMigrationJobsForTest()
	t.Cleanup(clearUserDataMigrationJobsForTest)

	app := &App{}
	release := make(chan struct{})
	started := make(chan struct{})
	job, err := app.startUserDataMigrationJob("migration.test", func(context.Context, func(float64, string)) (map[string]interface{}, error) {
		close(started)
		<-release
		return map[string]interface{}{"ok": true}, nil
	})
	if err != nil {
		t.Fatalf("start first job: %v", err)
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("first job did not start")
	}

	if _, err := app.startUserDataMigrationJob("migration.test", func(context.Context, func(float64, string)) (map[string]interface{}, error) {
		return nil, nil
	}); err == nil {
		t.Fatal("expected concurrent migration job to be rejected")
	}

	close(release)
	deadline := time.After(2 * time.Second)
	for {
		got, err := app.GetUserDataMigrationJob(job.ID)
		if err != nil {
			t.Fatalf("get job: %v", err)
		}
		if got.Status == "succeeded" {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("job did not finish, status=%s", got.Status)
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func TestUserDataMigrationJobRecoversPanic(t *testing.T) {
	clearUserDataMigrationJobsForTest()
	t.Cleanup(clearUserDataMigrationJobsForTest)

	app := &App{}
	job, err := app.startUserDataMigrationJob("migration.test", func(context.Context, func(float64, string)) (map[string]interface{}, error) {
		panic("boom")
	})
	if err != nil {
		t.Fatalf("start job: %v", err)
	}

	deadline := time.After(2 * time.Second)
	for {
		got, err := app.GetUserDataMigrationJob(job.ID)
		if err != nil {
			t.Fatalf("get job: %v", err)
		}
		if got.Status == "failed" {
			if !strings.Contains(got.Error, "boom") {
				t.Fatalf("panic error not reported: %q", got.Error)
			}
			return
		}
		select {
		case <-deadline:
			t.Fatalf("job did not fail, status=%s", got.Status)
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func userDataMigrationTestEntry(id, content string) corememory.Entry {
	return corememory.Entry{
		ID:       id,
		Content:  content,
		Category: corememory.CategoryProjectKnowledge,
	}
}

func userDataMigrationKnowledgeSourceIDs(t *testing.T, app *App) map[string]struct{} {
	t.Helper()
	items, err := app.KnowledgeListSources(coreknowledge.ListSourcesOptions{Limit: 100})
	if err != nil {
		t.Fatalf("list knowledge sources: %v", err)
	}
	out := map[string]struct{}{}
	for _, item := range items {
		out[item.ID] = struct{}{}
	}
	return out
}

func clearUserDataMigrationJobsForTest() {
	userDataMigrationJobs.Range(func(key, _ interface{}) bool {
		userDataMigrationJobs.Delete(key)
		return true
	})
	userDataMigrationCleanupPendingExports.Range(func(key, _ interface{}) bool {
		userDataMigrationCleanupPendingExports.Delete(key)
		return true
	})
}
