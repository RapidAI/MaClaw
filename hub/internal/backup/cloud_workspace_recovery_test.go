package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"database/sql"
	"encoding/base64"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/cloudworkspace"
	"github.com/RapidAI/CodeClaw/hub/internal/config"
	storesqlite "github.com/RapidAI/CodeClaw/hub/internal/store/sqlite"
	_ "modernc.org/sqlite"
)

func TestCloudWorkspaceGenerationBackupRestoreDrill(t *testing.T) {
	t.Setenv("MACLAW_CWS_MASTER_KEY", "")
	ctx := context.Background()
	sourceRoot := t.TempDir()
	dataDir := filepath.Join(sourceRoot, "data")
	dbPath := filepath.Join(dataDir, "hub.db")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`PRAGMA journal_mode=WAL`); err != nil {
		t.Fatal(err)
	}
	if err := storesqlite.RunMigrations(db); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)
	store := cloudworkspace.NewStore(db)
	workspace, err := store.Create(ctx, cloudworkspace.CreateParams{TenantID: "tenant-dr", UserID: "user-dr", Name: "DR workspace", Quota: 2}, now)
	if err != nil {
		t.Fatal(err)
	}
	cloudRoot := filepath.Join(dataDir, "cloud-workspaces")
	blobs := &cloudworkspace.BlobStore{Root: cloudRoot, KeyDir: cloudRoot, DB: db}
	plaintext := []byte("disaster recovery object payload")
	put, err := blobs.Put(ctx, "tenant-dr", "user-dr", workspace.ID, plaintext)
	if err != nil {
		t.Fatal(err)
	}
	unreferencedPlaintext := []byte("uploaded but not yet in a manifest")
	unreferenced, err := blobs.Put(ctx, "tenant-dr", "user-dr", workspace.ID, unreferencedPlaintext)
	if err != nil {
		t.Fatal(err)
	}
	lease, err := store.Acquire(ctx, cloudworkspace.AcquireParams{TenantID: "tenant-dr", UserID: "user-dr", WorkspaceID: workspace.ID, MachineID: "machine-dr"}, now)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := store.ReplaceManifest(ctx, "tenant-dr", "user-dr", workspace.ID, "machine-dr", "", []cloudworkspace.ManifestEntry{{Path: "src/readme.txt", SHA256: put.SHA256, Size: int64(len(plaintext))}}, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if manifest.ManifestHash == "" {
		t.Fatal("manifest hash was not committed")
	}
	// Write the sidecar through the service so the canonical file, the sidecar
	// metadata row, and the task binding commit together.
	sidecarPayload := []byte(`{"cloud_task_id":"cloud-task-dr","name":"restored"}`)
	svc := &cloudworkspace.Service{Workspaces: store, Blobs: blobs, Now: func() time.Time { return now.Add(2 * time.Second) }}
	principal := auth.MachinePrincipal{TenantID: "tenant-dr", UserID: "user-dr", MachineID: "machine-dr", FencingToken: lease.FencingToken}
	if _, err := svc.PutSidecarWithRevision(ctx, principal, workspace.ID, cloudworkspace.SidecarTask, "", sidecarPayload); err != nil {
		t.Fatal(err)
	}
	stamp := now.Add(2 * time.Second).Format(time.RFC3339)
	if _, err := db.Exec(`INSERT INTO cloud_workspace_task_provisions (operation_id,tenant_id,user_id,workspace_id,cloud_task_id,name,state,created_at,updated_at) VALUES (?,?,?,?,?,?,?,?,?)`, "cwprov_dr", "tenant-dr", "user-dr", workspace.ID, "cloud-task-dr", "restored", "active", stamp, stamp); err != nil {
		t.Fatal(err)
	}

	cfg := config.Default()
	cfg.Database.DSN = dbPath
	archivePath := filepath.Join(sourceRoot, "backups", "cloud-workspace-dr.tar.gz")
	created, err := Create(ctx, cfg, CreateOptions{OutputPath: archivePath, Now: now.Add(3 * time.Second)})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if created.Manifest.CloudWorkspace == nil || created.Manifest.CloudWorkspace.Workspaces != 1 || created.Manifest.CloudWorkspace.VerifiedObjects != 2 || created.Manifest.CloudWorkspace.Sidecars != 1 {
		t.Fatalf("cloud workspace summary = %+v", created.Manifest.CloudWorkspace)
	}
	if created.Manifest.CloudWorkspace.MasterKey == nil || created.Manifest.CloudWorkspace.MasterKey.Provider != "file-keyring" {
		t.Fatalf("master key descriptor = %+v", created.Manifest.CloudWorkspace.MasterKey)
	}
	assertEntry(t, &created.Manifest, "data/cloud-workspaces/master.key", "cloud_workspace_master_key")
	assertEntry(t, &created.Manifest, "data/cloud-workspaces/master-keyring.json", "cloud_workspace_master_key")

	restoreRoot := filepath.Join(sourceRoot, "restored-hub")
	dryRun, err := Restore(RestoreOptions{ArchivePath: archivePath, TargetRoot: restoreRoot, DryRun: true})
	if err != nil {
		t.Fatalf("Restore(dry-run) error = %v", err)
	}
	if dryRun.Verification == nil || dryRun.Verification.Usage.RetainedBytes != int64(len(plaintext)+len(unreferencedPlaintext)) {
		t.Fatalf("dry-run verification = %+v", dryRun.Verification)
	}
	if _, err := os.Stat(restoreRoot); !os.IsNotExist(err) {
		t.Fatalf("dry-run wrote target root: %v", err)
	}
	if _, err := Restore(RestoreOptions{ArchivePath: archivePath, TargetRoot: restoreRoot}); err != nil {
		t.Fatalf("Restore() error = %v", err)
	}
	restoredDB, err := sql.Open("sqlite", filepath.Join(restoreRoot, "data", "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer restoredDB.Close()
	restoredBlobs := &cloudworkspace.BlobStore{Root: filepath.Join(restoreRoot, "data", "cloud-workspaces"), KeyDir: filepath.Join(restoreRoot, "data", "cloud-workspaces"), DB: restoredDB}
	gotObject, err := restoredBlobs.Get(ctx, "tenant-dr", "user-dr", workspace.ID, put.SHA256)
	if err != nil || string(gotObject) != string(plaintext) {
		t.Fatalf("restored object=%q err=%v", gotObject, err)
	}
	gotUnreferenced, err := restoredBlobs.Get(ctx, "tenant-dr", "user-dr", workspace.ID, unreferenced.SHA256)
	if err != nil || string(gotUnreferenced) != string(unreferencedPlaintext) {
		t.Fatalf("restored unreferenced object=%q err=%v", gotUnreferenced, err)
	}
	gotSidecar, err := restoredBlobs.GetSidecar(ctx, "tenant-dr", "user-dr", workspace.ID, cloudworkspace.SidecarTask)
	if err != nil || string(gotSidecar) != string(sidecarPayload) {
		t.Fatalf("restored sidecar=%q err=%v", gotSidecar, err)
	}
	var bindingCount, provisionCount int
	if err := restoredDB.QueryRow(`SELECT COUNT(*) FROM cloud_workspace_task_bindings WHERE workspace_id=?`, workspace.ID).Scan(&bindingCount); err != nil {
		t.Fatal(err)
	}
	if err := restoredDB.QueryRow(`SELECT COUNT(*) FROM cloud_workspace_task_provisions WHERE workspace_id=? AND state='active'`, workspace.ID).Scan(&provisionCount); err != nil {
		t.Fatal(err)
	}
	if bindingCount != 1 || provisionCount != 1 {
		t.Fatalf("binding/provision counts = %d/%d", bindingCount, provisionCount)
	}
	report, err := cloudworkspace.VerifyRecovery(ctx, restoredDB, restoredBlobs.Root, restoredBlobs.KeyDir)
	if err != nil {
		t.Fatal(err)
	}
	if report.Usage != created.Manifest.CloudWorkspace.Usage {
		t.Fatalf("retained usage after restore = %+v want %+v", report.Usage, created.Manifest.CloudWorkspace.Usage)
	}
}

func TestCloudWorkspaceBackupDoesNotArchiveStaleLocalKeysForEnvironmentProvider(t *testing.T) {
	key := []byte("01234567890123456789012345678901")
	t.Setenv("MACLAW_CWS_MASTER_KEY", base64.RawStdEncoding.EncodeToString(key))
	t.Setenv("MACLAW_CWS_KEYRING", "")
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(dataDir, "hub.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := storesqlite.RunMigrations(db); err != nil {
		t.Fatal(err)
	}
	store := cloudworkspace.NewStore(db)
	ws, err := store.Create(context.Background(), cloudworkspace.CreateParams{TenantID: "tenant-env", UserID: "user-env", Name: "env", Quota: 1}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	cloudRoot := filepath.Join(dataDir, "cloud-workspaces")
	blobs := &cloudworkspace.BlobStore{Root: cloudRoot, KeyDir: cloudRoot, DB: db}
	if _, err := blobs.Put(context.Background(), "tenant-env", "user-env", ws.ID, []byte("env payload")); err != nil {
		t.Fatal(err)
	}
	// Simulate recovery copies left by an earlier file-provider deployment.
	writeFile(t, filepath.Join(cloudRoot, "master.key"), "stale-local-secret")
	writeFile(t, filepath.Join(cloudRoot, "master-keyring.json"), "{\"stale\":true}")
	cfg := config.Default()
	cfg.Database.DSN = dbPath
	archivePath := filepath.Join(root, "env-backup.tar.gz")
	created, err := Create(context.Background(), cfg, CreateOptions{OutputPath: archivePath, Now: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	if created.Manifest.CloudWorkspace == nil || created.Manifest.CloudWorkspace.MasterKey == nil || created.Manifest.CloudWorkspace.MasterKey.Provider != "environment" {
		t.Fatalf("environment descriptor = %+v", created.Manifest.CloudWorkspace)
	}
	assertNoEntry(t, &created.Manifest, "data/cloud-workspaces/master.key")
	assertNoEntry(t, &created.Manifest, "data/cloud-workspaces/master-keyring.json")
}

func TestRestoreRejectsTamperedEntryBeforeTargetMutation(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	dbPath := filepath.Join(dataDir, "hub.db")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	seedSQLite(t, dbPath)
	writeFile(t, filepath.Join(dataDir, "asset.bin"), "original")
	cfg := config.Default()
	cfg.Database.DSN = dbPath
	archivePath := filepath.Join(root, "backup.tar.gz")
	if _, err := Create(context.Background(), cfg, CreateOptions{OutputPath: archivePath, Now: time.Now()}); err != nil {
		t.Fatal(err)
	}
	tampered := filepath.Join(root, "tampered.tar.gz")
	rewriteArchiveEntry(t, archivePath, tampered, "data/asset.bin", []byte("tampered"))
	target := filepath.Join(root, "target")
	writeFile(t, filepath.Join(target, "sentinel.txt"), "keep")
	if _, err := Restore(RestoreOptions{ArchivePath: tampered, TargetRoot: target, Force: true}); err == nil || !strings.Contains(err.Error(), "sha256 mismatch") {
		t.Fatalf("tampered restore error = %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(target, "sentinel.txt"))
	if err != nil || string(raw) != "keep" {
		t.Fatalf("target mutated before validation: %q err=%v", raw, err)
	}
}

func rewriteArchiveEntry(t *testing.T, source, destination, target string, replacement []byte) {
	t.Helper()
	input, err := os.Open(source)
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	reader, err := gzip.NewReader(input)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	output, err := os.Create(destination)
	if err != nil {
		t.Fatal(err)
	}
	gzipWriter := gzip.NewWriter(output)
	tarWriter := tar.NewWriter(gzipWriter)
	tarReader := tar.NewReader(reader)
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		copyHeader := *header
		if header.Name == target {
			copyHeader.Size = int64(len(replacement))
			if err := tarWriter.WriteHeader(&copyHeader); err != nil {
				t.Fatal(err)
			}
			if _, err := tarWriter.Write(replacement); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := tarWriter.WriteHeader(&copyHeader); err != nil {
			t.Fatal(err)
		}
		if _, err := io.Copy(tarWriter, tarReader); err != nil {
			t.Fatal(err)
		}
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := output.Close(); err != nil {
		t.Fatal(err)
	}
}
