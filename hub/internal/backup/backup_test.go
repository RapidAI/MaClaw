package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"database/sql"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/config"
	_ "modernc.org/sqlite"
)

func TestCreateInspectRestore(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	if err := os.MkdirAll(filepath.Join(dataDir, "skills", "skill-a"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "configs"), 0o755); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(dataDir, "hub.db")
	seedSQLite(t, dbPath)
	writeFile(t, filepath.Join(root, "configs", "config.yaml"), "database:\n  dsn: ./data/hub.db\n")
	writeFile(t, filepath.Join(dataDir, "rsa_private.pem"), "private")
	writeFile(t, filepath.Join(dataDir, "gossip_cache.json.gz"), "gossip")
	writeFile(t, filepath.Join(dataDir, "skills", "skill-a", "skill.yaml"), "name: skill-a\n")
	writeFile(t, filepath.Join(dataDir, "hub-run.log"), "runtime log")
	tlsDir := filepath.Join(root, "tls-external")
	certPath := filepath.Join(tlsDir, "hub.crt")
	keyPath := filepath.Join(tlsDir, "hub.key")
	writeFile(t, certPath, "cert")
	writeFile(t, keyPath, "key")

	oldwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldwd)
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}

	cfg := config.Default()
	cfg.Database.DSN = dbPath
	cfg.TLS.CertFile = certPath
	cfg.TLS.KeyFile = keyPath
	archivePath := filepath.Join(dataDir, "backups", "backup.tar.gz")
	result, err := Create(context.Background(), cfg, CreateOptions{
		ConfigPath: "configs/config.yaml",
		OutputPath: archivePath,
		Now:        time.Date(2026, 4, 28, 1, 2, 3, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if result.ArchivePath != archivePath {
		t.Fatalf("archive path = %q, want %q", result.ArchivePath, archivePath)
	}

	manifest, err := Inspect(archivePath)
	if err != nil {
		t.Fatalf("Inspect() error = %v", err)
	}
	assertEntry(t, manifest, "configs/config.yaml", "config")
	assertEntry(t, manifest, "data/hub.db", "sqlite_snapshot")
	assertEntry(t, manifest, "data/rsa_private.pem", "certificate")
	assertEntry(t, manifest, "data/gossip_cache.json.gz", "gossip_cache")
	assertEntry(t, manifest, "data/skills/skill-a/skill.yaml", "skill")
	assertEntry(t, manifest, "external/tls_certificate/hub.crt", "tls_certificate")
	assertEntry(t, manifest, "external/tls_private_key/hub.key", "tls_private_key")
	assertNoEntry(t, manifest, "data/hub-run.log")
	assertNoEntry(t, manifest, "data/backups/backup.tar.gz")
	assertTarGzEntry(t, archivePath, ManifestPath)

	restoreDir := filepath.Join(root, "restore")
	restoreResult, err := Restore(RestoreOptions{ArchivePath: archivePath, TargetRoot: restoreDir})
	if err != nil {
		t.Fatalf("Restore() error = %v", err)
	}
	if len(restoreResult.Restored) == 0 {
		t.Fatal("Restore() restored no entries")
	}
	if _, err := os.Stat(filepath.Join(restoreDir, "data", "hub.db")); err != nil {
		t.Fatalf("restored db missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(restoreDir, "configs", "config.yaml")); err != nil {
		t.Fatalf("restored config missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(restoreDir, "external", "tls_certificate", "hub.crt")); err != nil {
		t.Fatalf("restored tls cert missing: %v", err)
	}

	dryRunResult, err := Restore(RestoreOptions{ArchivePath: archivePath, TargetRoot: restoreDir, DryRun: true})
	if err != nil {
		t.Fatalf("Restore() dry-run error = %v", err)
	}
	if len(dryRunResult.Skipped) == 0 {
		t.Fatal("Restore() dry-run should report existing entries")
	}

	if _, err := Restore(RestoreOptions{ArchivePath: archivePath, TargetRoot: restoreDir}); err == nil {
		t.Fatal("Restore() without force should refuse overwrites")
	}
}

func seedSQLite(t *testing.T, path string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE items (id INTEGER PRIMARY KEY, name TEXT); INSERT INTO items(name) VALUES ('one')`); err != nil {
		t.Fatal(err)
	}
}

func writeFile(t *testing.T, path, value string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(value), 0o644); err != nil {
		t.Fatal(err)
	}
}

func assertEntry(t *testing.T, manifest *Manifest, path, kind string) {
	t.Helper()
	for _, entry := range manifest.Entries {
		if entry.Path == path && entry.Kind == kind {
			return
		}
	}
	t.Fatalf("manifest missing entry %s kind %s; entries=%v", path, kind, manifest.Entries)
}

func assertNoEntry(t *testing.T, manifest *Manifest, path string) {
	t.Helper()
	for _, entry := range manifest.Entries {
		if entry.Path == path {
			t.Fatalf("manifest unexpectedly contains %s", path)
		}
	}
}

func assertTarGzEntry(t *testing.T, archivePath, name string) {
	t.Helper()
	file, err := os.Open(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	gr, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	defer gr.Close()
	tr := tar.NewReader(gr)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if header.Name == name {
			return
		}
	}
	t.Fatalf("tar.gz missing entry %s", name)
}
