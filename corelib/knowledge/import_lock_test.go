package knowledge

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// lockProbeDistiller tries to take a write lock while distillation runs.
// Folder-watch imports used to keep one write transaction open across the
// model call, so this begin failed with SQLITE_BUSY. The import store also
// has a single connection; an idle transaction checks that out even when it
// holds no file lock, so the probe begins on that same pool first.
type lockProbeDistiller struct {
	path  string
	store *SQLiteStore
	calls int
	err   error
}

func (d *lockProbeDistiller) DistillCards(ctx context.Context, source Source, nodes []DocumentNode) ([]Card, error) {
	d.calls++
	if d.store != nil {
		probeCtx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
		defer cancel()
		probe, err := d.store.db.BeginTx(probeCtx, nil)
		if err != nil {
			d.err = err
			return lockProbeCards(), nil
		}
		_ = probe.Rollback()
	}
	db, err := sql.Open("sqlite", d.path)
	if err != nil {
		d.err = err
		return lockProbeCards(), nil
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if _, err = db.ExecContext(ctx, `PRAGMA busy_timeout=300`); err != nil {
		d.err = err
		return lockProbeCards(), nil
	}
	if _, err = db.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
		d.err = err
		return lockProbeCards(), nil
	}
	if _, err = db.ExecContext(ctx, `COMMIT`); err != nil {
		d.err = err
	}
	return lockProbeCards(), nil
}

func lockProbeCards() []Card {
	return []Card{{
		Title:      "api2 note",
		Claim:      "folder watch released the write lock",
		Summary:    "api2",
		Confidence: 0.9,
		Importance: 1,
	}}
}

func TestFolderWatchImportReleasesWriteLockDuringDistill(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	docs := filepath.Join(root, "docs")
	if err := os.MkdirAll(docs, 0o755); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(root, "knowledge.db")
	bodies := []string{
		"# api2\n\nfolder watch batch one\n",
		"# api2\n\nfolder watch batch two\n",
	}
	for i, name := range []string{"one.md", "two.md"} {
		if err := os.WriteFile(filepath.Join(docs, name), []byte(bodies[i]), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	store, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	distiller := &lockProbeDistiller{path: dbPath, store: store}
	store.SetCardDistiller(distiller)

	started := time.Now()
	// Explicit LLM mode stays sequential and must release the write lock during distillation.
	result, err := store.ImportDirectory(ctx, DirectoryImportRequest{
		RootPath:    docs,
		TopicHint:   "doc",
		IncludeExts: []string{".md"},
		DistillMode: DistillModeLLMIfAny,
	})
	if err != nil {
		t.Fatalf("ImportDirectory: %v", err)
	}
	if result.ImportedFiles != 2 || result.FailedFiles != 0 {
		t.Fatalf("import result = %#v", result)
	}
	if distiller.calls != 2 {
		t.Fatalf("distiller calls = %d, want 2", distiller.calls)
	}
	if distiller.err != nil {
		t.Fatalf("distillation could not take a write lock: %v", distiller.err)
	}
	if time.Since(started) > 3*time.Second {
		t.Fatalf("import took %s; distillation was waiting on the database", time.Since(started))
	}
}

func TestSchemaV2ObjectNamesParse(t *testing.T) {
	stmts := schemaV2DDL()
	if len(stmts) == 0 {
		t.Fatal("schema v2 DDL is empty")
	}
	seen := make(map[string]bool, len(stmts))
	for _, stmt := range stmts {
		match := schemaV2ObjectNamePattern.FindStringSubmatch(stmt)
		if match == nil {
			t.Fatalf("unparsed DDL: %s", stmt)
		}
		if seen[match[1]] {
			t.Fatalf("duplicate schema object %s", match[1])
		}
		seen[match[1]] = true
	}
}

func TestReopenAppliesMissingSchemaV2Index(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "knowledge.db")
	store, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DROP INDEX IF EXISTS idx_kb_facts_object`); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var n int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name = 'idx_kb_facts_object'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("missing index was not created on open: %d", n)
	}
}

func TestOpenCurrentKnowledgeStoreWhileImportHoldsWriteLock(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "knowledge.db")
	store, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	hold, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer hold.Close()
	tx, err := hold.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE kb_meta SET value = value WHERE key = 'schema_version'`); err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() {
		opened, openErr := NewSQLiteStore(dbPath)
		if openErr == nil {
			openErr = opened.Close()
		}
		done <- openErr
	}()
	select {
	case openErr := <-done:
		if openErr != nil {
			t.Fatalf("open while a writer holds the database: %v", openErr)
		}
	case <-time.After(2 * time.Second):
		_ = tx.Rollback()
		t.Fatal("opening a current knowledge store blocked on the writer")
	}
}

// cancelOnSecondDistiller cancels the import context on the second model call
// so a later file returns after the previous file has already committed.
type cancelOnSecondDistiller struct {
	cancel func()
	calls  int
}

func (d *cancelOnSecondDistiller) DistillCards(ctx context.Context, source Source, nodes []DocumentNode) ([]Card, error) {
	d.calls++
	if d.calls >= 2 && d.cancel != nil {
		d.cancel()
	}
	return lockProbeCards(), nil
}

func TestImportErrorReportsCommittedFileCount(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	root := t.TempDir()
	docs := filepath.Join(root, "docs")
	if err := os.MkdirAll(docs, 0o755); err != nil {
		t.Fatal(err)
	}
	bodies := []string{
		"# api2\n\nfolder watch batch one\n",
		"# api2\n\nfolder watch batch two\n",
	}
	for i, name := range []string{"one.md", "two.md"} {
		if err := os.WriteFile(filepath.Join(docs, name), []byte(bodies[i]), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	dbPath := filepath.Join(root, "knowledge.db")
	store, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	distiller := &cancelOnSecondDistiller{cancel: cancel}
	store.SetCardDistiller(distiller)

	result, err := store.ImportDirectory(ctx, DirectoryImportRequest{
		RootPath:    docs,
		TopicHint:   "doc",
		IncludeExts: []string{".md"},
		DistillMode: DistillModeLLMIfAny,
	})
	if err == nil {
		t.Fatal("expected import error after the second file")
	}
	if distiller.calls < 2 {
		t.Fatalf("distiller calls = %d, want at least 2", distiller.calls)
	}
	if result.Status != ImportStatusFailed {
		t.Fatalf("status = %q, result %#v, err %v", result.Status, result, err)
	}
	if result.ImportedFiles != 1 {
		t.Fatalf("imported = %d, want 1 (result %#v, err %v)", result.ImportedFiles, result, err)
	}
	var status string
	var imported int
	if err := store.db.QueryRow(`SELECT status, imported_files FROM knowledge_import_batches WHERE id = ?`, result.BatchID).Scan(&status, &imported); err != nil {
		t.Fatal(err)
	}
	if status != ImportStatusFailed || imported != 1 {
		t.Fatalf("batch status=%s imported_files=%d", status, imported)
	}
}

func TestDirectorySpreadsheetReadDoesNotHoldStoreConnection(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	docs := filepath.Join(root, "docs")
	if err := os.MkdirAll(docs, 0o755); err != nil {
		t.Fatal(err)
	}
	csvBody := "姓名,部门,入职日期,金额\n张三,法务,2024-01-05,120000\n李四,销售,2023-12-01,98000\n"
	if err := os.WriteFile(filepath.Join(docs, "employees.csv"), []byte(csvBody), 0o644); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(root, "knowledge.db")
	store, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	orig := spreadsheetImportSnapshot
	var probeErr error
	calls := 0
	spreadsheetImportSnapshot = func(filePath, kind string) (string, func(), error) {
		calls++
		probeCtx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
		defer cancel()
		probe, beginErr := store.db.BeginTx(probeCtx, nil)
		if beginErr != nil {
			probeErr = beginErr
		} else {
			_ = probe.Rollback()
		}
		if probeErr == nil {
			db, openErr := sql.Open("sqlite", dbPath)
			if openErr != nil {
				probeErr = openErr
			} else {
				defer db.Close()
				db.SetMaxOpenConns(1)
				if _, execErr := db.ExecContext(probeCtx, `PRAGMA busy_timeout=300`); execErr != nil {
					probeErr = execErr
				} else if _, execErr = db.ExecContext(probeCtx, `BEGIN IMMEDIATE`); execErr != nil {
					probeErr = execErr
				} else if _, execErr = db.ExecContext(probeCtx, `COMMIT`); execErr != nil {
					probeErr = execErr
				}
			}
		}
		return orig(filePath, kind)
	}
	defer func() { spreadsheetImportSnapshot = orig }()

	result, err := store.ImportDirectory(ctx, DirectoryImportRequest{
		RootPath:    docs,
		IncludeExts: []string{".csv"},
	})
	if err != nil {
		t.Fatalf("ImportDirectory: %v", err)
	}
	if calls == 0 {
		t.Fatal("spreadsheet snapshot was not used")
	}
	if probeErr != nil {
		t.Fatalf("workbook read held the store connection: %v", probeErr)
	}
	if result.ImportedFiles != 1 || result.FailedFiles != 0 {
		t.Fatalf("import result = %#v", result)
	}
	var rows int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM kb_rows`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 2 {
		t.Fatalf("kb_rows = %d, want 2", rows)
	}
}
