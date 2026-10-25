package knowledge

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestOpenCopiesKBSourcesAndRetargetsBodyParent(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "knowledge.db")
	legacy, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := applyPragmas(legacy); err != nil {
		t.Fatal(err)
	}
	if err := createTables(legacy); err != nil {
		t.Fatal(err)
	}
	if err := createSchemaV2(ctx, legacy); err != nil {
		t.Fatal(err)
	}
	if err := setSchemaVersion(ctx, legacy, knowledgeSchemaVersionV2); err != nil {
		t.Fatal(err)
	}
	if err := installLegacyBodyParents(ctx, legacy); err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.Exec(`INSERT INTO kb_sources(id, kind, uri, title, fetched_at, content_hash, owner_id, project_path, status, created_at, updated_at)
		VALUES ('legacy-only', 'csv', 'legacy.csv', 'Legacy Only', '2026-01-01T00:00:00Z', 'hash', 'desktop-user:task-9', 'D:/Team', 'parsed', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.Exec(`INSERT INTO kb_sources(id, kind, uri, title, fetched_at, content_hash, owner_id, status, created_at, updated_at)
		VALUES ('legacy-empty', 'csv', 'empty.csv', 'Legacy Empty', '2026-01-01T00:00:00Z', 'hash-empty', '', 'parsed', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.Exec(`INSERT INTO kb_tables(id, source_id, sheet_name, row_count, created_at, updated_at)
		VALUES ('legacy-table', 'legacy-only', 'Sheet1', 3, '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.Exec(`INSERT INTO kb_columns(id, table_id, column_index, column_name, normalized_name, value_type, created_at, updated_at)
		VALUES ('legacy-col', 'legacy-table', 1, '部门', '部门', 'string', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.Exec(`INSERT INTO kb_rows(id, table_id, source_id, row_index, row_text, created_at, updated_at)
		VALUES ('legacy-row', 'legacy-table', 'legacy-only', 2, '张三 法务', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.Exec(`INSERT INTO kb_cells(id, row_id, table_id, column_id, column_name, normalized_column_name, raw_value, value_type, created_at, updated_at)
		VALUES ('legacy-cell', 'legacy-row', 'legacy-table', 'legacy-col', '部门', '部门', '法务', 'string', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	// Backfill is already finished, which is the desktop library this copy meets.
	if err := setKnowledgeMeta(ctx, legacy, membershipBackfillMetaKey, desktopMembershipUser); err != nil {
		t.Fatal(err)
	}
	if err := setKnowledgeMeta(ctx, legacy, collectionProjectBackfillMetaKey, collectionProjectBackfillDone); err != nil {
		t.Fatal(err)
	}
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}

	store, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	var title, owner string
	if err := store.db.QueryRowContext(ctx, `SELECT title, owner_id FROM knowledge_sources WHERE id = 'legacy-only'`).Scan(&title, &owner); err != nil {
		t.Fatalf("copied source: %v", err)
	}
	if title != "Legacy Only" || owner != "desktop-user:task-9" {
		t.Fatalf("copied source = %q %q", title, owner)
	}
	var emptyOwner string
	if err := store.db.QueryRowContext(ctx, `SELECT owner_id FROM knowledge_sources WHERE id = 'legacy-empty'`).Scan(&emptyOwner); err != nil {
		t.Fatalf("copied empty source: %v", err)
	}
	if emptyOwner != "" {
		t.Fatalf("empty owner = %q", emptyOwner)
	}
	var userKey, projectKey string
	if err := store.db.QueryRowContext(ctx, `SELECT scope_key FROM knowledge_memberships WHERE source_id = 'legacy-only' AND kind = 'user'`).Scan(&userKey); err != nil {
		t.Fatalf("user membership: %v", err)
	}
	if userKey != desktopMembershipUser {
		t.Fatalf("user membership = %q", userKey)
	}
	if err := store.db.QueryRowContext(ctx, `SELECT scope_key FROM knowledge_memberships WHERE source_id = 'legacy-only' AND kind = 'project'`).Scan(&projectKey); err != nil {
		t.Fatalf("project membership: %v", err)
	}
	if projectKey != "D:/Team" {
		t.Fatalf("project membership = %q", projectKey)
	}
	var emptyMemberships int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM knowledge_memberships WHERE source_id = 'legacy-empty'`).Scan(&emptyMemberships); err != nil {
		t.Fatal(err)
	}
	if emptyMemberships != 0 {
		t.Fatalf("empty owner memberships = %d", emptyMemberships)
	}
	var cell string
	if err := store.db.QueryRowContext(ctx, `SELECT raw_value FROM kb_cells WHERE id = 'legacy-cell'`).Scan(&cell); err != nil {
		t.Fatalf("cell survived: %v", err)
	}
	if cell != "法务" {
		t.Fatalf("cell = %q", cell)
	}
	var fkProblems int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_foreign_key_check`).Scan(&fkProblems); err != nil {
		t.Fatal(err)
	}
	if fkProblems != 0 {
		t.Fatalf("foreign key problems = %d", fkProblems)
	}
	var sheet string
	if err := store.db.QueryRowContext(ctx, `SELECT sheet_name FROM kb_tables WHERE id = 'legacy-table'`).Scan(&sheet); err != nil {
		t.Fatalf("table survived: %v", err)
	}
	if sheet != "Sheet1" {
		t.Fatalf("sheet = %q", sheet)
	}
	for _, table := range []string{"kb_tables", "kb_rows", "kb_cards", "kb_facts"} {
		parent, ok, err := kbSourceForeignKeyParent(ctx, store.db, table)
		if err != nil {
			t.Fatal(err)
		}
		if !ok || parent != "knowledge_sources" {
			t.Fatalf("%s parent = %q ok=%v", table, parent, ok)
		}
	}
	var legacySources int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM kb_sources WHERE id = 'legacy-only'`).Scan(&legacySources); err != nil {
		t.Fatal(err)
	}
	if legacySources != 1 {
		t.Fatalf("kb_sources rows = %d, want the historical row kept", legacySources)
	}
}

// installLegacyBodyParents rewrites the child tables so their source foreign
// key points at kb_sources, which is the shape an existing library still has.
func installLegacyBodyParents(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, `PRAGMA foreign_keys=OFF`); err != nil {
		return err
	}
	defer db.ExecContext(context.Background(), `PRAGMA foreign_keys=ON`)
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, stmt := range []string{
		`DROP TABLE IF EXISTS kb_facts`,
		`DROP TABLE IF EXISTS kb_cards`,
		`DROP TABLE IF EXISTS kb_rows`,
		`DROP TABLE IF EXISTS kb_tables`,
		`CREATE TABLE kb_tables (
			id TEXT PRIMARY KEY,
			source_id TEXT NOT NULL,
			sheet_name TEXT NOT NULL,
			table_title TEXT,
			header_row_index INTEGER DEFAULT 0,
			row_count INTEGER DEFAULT 0,
			column_count INTEGER DEFAULT 0,
			schema_json TEXT,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			FOREIGN KEY(source_id) REFERENCES kb_sources(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE kb_rows (
			id TEXT PRIMARY KEY,
			table_id TEXT NOT NULL,
			source_id TEXT NOT NULL,
			row_index INTEGER NOT NULL,
			primary_key_text TEXT,
			row_text TEXT,
			row_json TEXT,
			embedding BLOB,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			FOREIGN KEY(table_id) REFERENCES kb_tables(id) ON DELETE CASCADE,
			FOREIGN KEY(source_id) REFERENCES kb_sources(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE kb_cards (
			id TEXT PRIMARY KEY,
			source_id TEXT NOT NULL,
			row_id TEXT,
			origin_type TEXT NOT NULL DEFAULT 'document',
			title TEXT,
			claim TEXT NOT NULL,
			summary TEXT,
			entities_json TEXT,
			topics_json TEXT,
			tags_json TEXT,
			project_path TEXT,
			owner_id TEXT,
			tenant_id TEXT,
			valid_at TEXT,
			invalid_at TEXT,
			confidence REAL DEFAULT 0.5,
			importance REAL DEFAULT 1.0,
			source_trust REAL DEFAULT 0.5,
			embedding BLOB,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			FOREIGN KEY(source_id) REFERENCES kb_sources(id) ON DELETE CASCADE,
			FOREIGN KEY(row_id) REFERENCES kb_rows(id) ON DELETE SET NULL
		)`,
		`CREATE TABLE kb_facts (
			id TEXT PRIMARY KEY,
			card_id TEXT NOT NULL,
			source_id TEXT NOT NULL,
			row_id TEXT,
			subject TEXT NOT NULL,
			predicate TEXT NOT NULL,
			object TEXT NOT NULL,
			normalized_object TEXT,
			value_type TEXT,
			number_value REAL,
			date_value TEXT,
			bool_value INTEGER,
			negated INTEGER DEFAULT 0,
			valid_at TEXT,
			invalid_at TEXT,
			confidence REAL DEFAULT 0.5,
			created_at TEXT NOT NULL,
			FOREIGN KEY(card_id) REFERENCES kb_cards(id) ON DELETE CASCADE,
			FOREIGN KEY(source_id) REFERENCES kb_sources(id) ON DELETE CASCADE,
			FOREIGN KEY(row_id) REFERENCES kb_rows(id) ON DELETE SET NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_kb_tables_source_sheet ON kb_tables(source_id, sheet_name)`,
		`CREATE INDEX IF NOT EXISTS idx_kb_tables_source_sheet_lower ON kb_tables(source_id, LOWER(sheet_name))`,
		`CREATE INDEX IF NOT EXISTS idx_kb_rows_table_row ON kb_rows(table_id, row_index)`,
		`CREATE INDEX IF NOT EXISTS idx_kb_rows_source_row ON kb_rows(source_id, row_index)`,
		`CREATE INDEX IF NOT EXISTS idx_kb_cards_scope ON kb_cards(tenant_id, owner_id, project_path, updated_at)`,
		`CREATE INDEX IF NOT EXISTS idx_kb_facts_subject ON kb_facts(subject)`,
		`CREATE INDEX IF NOT EXISTS idx_kb_facts_object ON kb_facts(object)`,
	} {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func TestImportStampsCardOwnerWithSource(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "note.md")
	if err := os.WriteFile(path, []byte("桌面导入应把归属同时写在来源和卡片上。"), 0o644); err != nil {
		t.Fatal(err)
	}
	store, err := NewSQLiteStore(filepath.Join(dir, "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	store.SetDesktopOwnerStamp(desktopMembershipUser)
	res, err := store.ImportFiles(ctx, DirectoryImportRequest{
		IncludeExts:  []string{".md"},
		MaxFileBytes: 4096,
		DistillMode:  DistillModeRules,
	}, []string{path})
	if err != nil {
		t.Fatal(err)
	}
	if res.ImportedFiles != 1 {
		t.Fatalf("imported = %#v", res)
	}
	var sourceOwner, cardOwner string
	if err := store.db.QueryRowContext(ctx, `SELECT owner_id FROM knowledge_sources`).Scan(&sourceOwner); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRowContext(ctx, `SELECT owner_id FROM knowledge_cards`).Scan(&cardOwner); err != nil {
		t.Fatal(err)
	}
	if sourceOwner != desktopMembershipUser || cardOwner != desktopMembershipUser {
		t.Fatalf("source owner = %q, card owner = %q", sourceOwner, cardOwner)
	}
}

func TestDesktopReimportKeepsStampedSource(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "note.md")
	if err := os.WriteFile(path, []byte("第一版正文，用来确认再次导入仍是同一条来源。"), 0o644); err != nil {
		t.Fatal(err)
	}
	store, err := NewSQLiteStore(filepath.Join(dir, "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	store.SetDesktopOwnerStamp(desktopMembershipUser)
	first := importDesktopNote(t, store, path, "")
	if first.ImportedFiles != 1 {
		t.Fatalf("first import = %#v", first)
	}
	var id, owner string
	if err := store.db.QueryRowContext(ctx, `SELECT id, owner_id FROM knowledge_sources`).Scan(&id, &owner); err != nil {
		t.Fatal(err)
	}
	if owner != desktopMembershipUser {
		t.Fatalf("owner = %q", owner)
	}
	if err := os.WriteFile(path, []byte("第二版正文，内容已经变化，仍应替换原来源。"), 0o644); err != nil {
		t.Fatal(err)
	}
	second := importDesktopNote(t, store, path, "")
	if second.ImportedFiles != 1 {
		t.Fatalf("second import = %#v", second)
	}
	var n int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM knowledge_sources`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("sources = %d", n)
	}
	var id2, owner2 string
	if err := store.db.QueryRowContext(ctx, `SELECT id, owner_id FROM knowledge_sources`).Scan(&id2, &owner2); err != nil {
		t.Fatal(err)
	}
	if id2 != id || owner2 != desktopMembershipUser {
		t.Fatalf("id %q -> %q owner %q", id, id2, owner2)
	}
	var mismatched int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM knowledge_cards WHERE COALESCE(owner_id, '') <> ?`, desktopMembershipUser).Scan(&mismatched); err != nil {
		t.Fatal(err)
	}
	if mismatched != 0 {
		t.Fatalf("cards with a different owner = %d", mismatched)
	}
}

func TestDesktopReimportKeepsTaskOwner(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "note.md")
	if err := os.WriteFile(path, []byte("任务归属的第一版正文。"), 0o644); err != nil {
		t.Fatal(err)
	}
	store, err := NewSQLiteStore(filepath.Join(dir, "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	taskOwner := desktopMembershipUser + ":task-9"
	first := importDesktopNote(t, store, path, taskOwner)
	if first.ImportedFiles != 1 {
		t.Fatalf("first import = %#v", first)
	}
	store.SetDesktopOwnerStamp(desktopMembershipUser)
	if err := os.WriteFile(path, []byte("任务归属的第二版正文，空请求不应改写任务归属。"), 0o644); err != nil {
		t.Fatal(err)
	}
	second := importDesktopNote(t, store, path, "")
	if second.ImportedFiles != 1 {
		t.Fatalf("second import = %#v", second)
	}
	var n int
	var owner string
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*), MIN(owner_id) FROM knowledge_sources`).Scan(&n, &owner); err != nil {
		t.Fatal(err)
	}
	if n != 1 || owner != taskOwner {
		t.Fatalf("sources = %d owner = %q", n, owner)
	}
	var cardOwner string
	if err := store.db.QueryRowContext(ctx, `SELECT owner_id FROM knowledge_cards`).Scan(&cardOwner); err != nil {
		t.Fatal(err)
	}
	if cardOwner != taskOwner {
		t.Fatalf("card owner = %q", cardOwner)
	}
}

func TestDesktopReimportDoesNotAdoptOtherOwner(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "note.md")
	if err := os.WriteFile(path, []byte("别人的第一版正文。"), 0o644); err != nil {
		t.Fatal(err)
	}
	store, err := NewSQLiteStore(filepath.Join(dir, "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	first := importDesktopNote(t, store, path, "alice")
	if first.ImportedFiles != 1 {
		t.Fatalf("first import = %#v", first)
	}
	store.SetDesktopOwnerStamp(desktopMembershipUser)
	if err := os.WriteFile(path, []byte("桌面用户的第二版正文，不应并进 alice。"), 0o644); err != nil {
		t.Fatal(err)
	}
	second := importDesktopNote(t, store, path, "")
	if second.ImportedFiles != 1 {
		t.Fatalf("second import = %#v", second)
	}
	var n, alice, desktop int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM knowledge_sources`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM knowledge_sources WHERE owner_id = 'alice'`).Scan(&alice); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM knowledge_sources WHERE owner_id = ?`, desktopMembershipUser).Scan(&desktop); err != nil {
		t.Fatal(err)
	}
	if n != 2 || alice != 1 || desktop != 1 {
		t.Fatalf("sources = %d alice = %d desktop = %d", n, alice, desktop)
	}
}

func TestDesktopUnchangedImportKeepsTaskSourceID(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "note.md")
	if err := os.WriteFile(path, []byte("未改动的正文，重复导入应带回原来源。"), 0o644); err != nil {
		t.Fatal(err)
	}
	store, err := NewSQLiteStore(filepath.Join(dir, "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	taskOwner := desktopMembershipUser + ":task-9"
	first := importDesktopNote(t, store, path, taskOwner)
	if first.ImportedFiles != 1 {
		t.Fatalf("first import = %#v", first)
	}
	var id string
	if err := store.db.QueryRowContext(ctx, `SELECT id FROM knowledge_sources`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	store.SetDesktopOwnerStamp(desktopMembershipUser)
	second := importDesktopNote(t, store, path, "")
	if second.ImportedFiles != 0 || second.DuplicateFiles != 1 {
		t.Fatalf("second import = %#v", second)
	}
	var n int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM knowledge_sources`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("sources = %d", n)
	}
	var sourceID string
	for _, item := range second.Items {
		if item.Status == ItemStatusSkippedDuplicate {
			sourceID = item.SourceID
		}
	}
	if sourceID != id {
		t.Fatalf("skipped source = %q want %q items=%#v", sourceID, id, second.Items)
	}
}

func TestDesktopUnchangedImportAdoptsEmptyOwner(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "note.md")
	if err := os.WriteFile(path, []byte("尚未归属的正文，再次导入应收下桌面用户。"), 0o644); err != nil {
		t.Fatal(err)
	}
	store, err := NewSQLiteStore(filepath.Join(dir, "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	first := importDesktopNote(t, store, path, "")
	if first.ImportedFiles != 1 {
		t.Fatalf("first import = %#v", first)
	}
	var id, owner string
	if err := store.db.QueryRowContext(ctx, `SELECT id, TRIM(COALESCE(owner_id, '')) FROM knowledge_sources`).Scan(&id, &owner); err != nil {
		t.Fatal(err)
	}
	if owner != "" {
		t.Fatalf("owner = %q", owner)
	}
	var cardsBefore int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM knowledge_cards WHERE source_id = ?`, id).Scan(&cardsBefore); err != nil {
		t.Fatal(err)
	}
	if cardsBefore == 0 {
		t.Fatal("expected cards on the unowned source")
	}
	store.SetDesktopOwnerStamp(desktopMembershipUser)
	second := importDesktopNote(t, store, path, "")
	if second.ImportedFiles != 0 || second.DuplicateFiles != 1 {
		t.Fatalf("second import = %#v", second)
	}
	var n int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM knowledge_sources`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("sources = %d", n)
	}
	if err := store.db.QueryRowContext(ctx, `SELECT TRIM(COALESCE(owner_id, '')) FROM knowledge_sources WHERE id = ?`, id).Scan(&owner); err != nil {
		t.Fatal(err)
	}
	if owner != desktopMembershipUser {
		t.Fatalf("owner = %q", owner)
	}
	var members, mismatched int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM knowledge_memberships WHERE source_id = ? AND kind = ? AND scope_key = ?`, id, MembershipKindUser, desktopMembershipUser).Scan(&members); err != nil {
		t.Fatal(err)
	}
	if members != 1 {
		t.Fatalf("user memberships = %d", members)
	}
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM knowledge_cards WHERE source_id = ? AND TRIM(COALESCE(owner_id, '')) <> ?`, id, desktopMembershipUser).Scan(&mismatched); err != nil {
		t.Fatal(err)
	}
	if mismatched != 0 {
		t.Fatalf("cards with a different owner = %d", mismatched)
	}
	var sourceID string
	for _, item := range second.Items {
		if item.Status == ItemStatusSkippedDuplicate {
			sourceID = item.SourceID
		}
	}
	if sourceID != id {
		t.Fatalf("skipped source = %q want %q", sourceID, id)
	}
}

func TestDesktopUnchangedImportKeepsProjectPath(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "note.md")
	if err := os.WriteFile(path, []byte("两个项目里的同一份文件应共用一条来源。"), 0o644); err != nil {
		t.Fatal(err)
	}
	store, err := NewSQLiteStore(filepath.Join(dir, "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	store.SetDesktopOwnerStamp(desktopMembershipUser)
	first := importDesktopProjectNote(t, store, path, "D:/Alpha")
	if first.ImportedFiles != 1 {
		t.Fatalf("first import = %#v", first)
	}
	var id, project string
	if err := store.db.QueryRowContext(ctx, `SELECT id, TRIM(COALESCE(project_path, '')) FROM knowledge_sources`).Scan(&id, &project); err != nil {
		t.Fatal(err)
	}
	if project != "D:/Alpha" {
		t.Fatalf("project = %q", project)
	}
	second := importDesktopProjectNote(t, store, path, "D:/Beta")
	if second.ImportedFiles != 0 || second.DuplicateFiles != 1 {
		t.Fatalf("second import = %#v", second)
	}
	var n int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM knowledge_sources`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("sources = %d", n)
	}
	if err := store.db.QueryRowContext(ctx, `SELECT TRIM(COALESCE(project_path, '')) FROM knowledge_sources WHERE id = ?`, id).Scan(&project); err != nil {
		t.Fatal(err)
	}
	if project != "D:/Alpha" {
		t.Fatalf("project = %q", project)
	}
	var alpha, beta int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM knowledge_memberships WHERE source_id = ? AND kind = ? AND scope_key = ?`, id, MembershipKindProject, "D:/Alpha").Scan(&alpha); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM knowledge_memberships WHERE source_id = ? AND kind = ? AND scope_key = ?`, id, MembershipKindProject, "D:/Beta").Scan(&beta); err != nil {
		t.Fatal(err)
	}
	if alpha != 1 || beta != 1 {
		t.Fatalf("project memberships alpha=%d beta=%d", alpha, beta)
	}
	var sourceID string
	for _, item := range second.Items {
		if item.Status == ItemStatusSkippedDuplicate {
			sourceID = item.SourceID
		}
	}
	if sourceID != id {
		t.Fatalf("skipped source = %q want %q", sourceID, id)
	}
}

func TestDesktopChangedImportInOtherProjectStaysSeparate(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "note.md")
	if err := os.WriteFile(path, []byte("项目甲里的第一版。"), 0o644); err != nil {
		t.Fatal(err)
	}
	store, err := NewSQLiteStore(filepath.Join(dir, "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	store.SetDesktopOwnerStamp(desktopMembershipUser)
	first := importDesktopProjectNote(t, store, path, "D:/Alpha")
	if first.ImportedFiles != 1 {
		t.Fatalf("first import = %#v", first)
	}
	if err := os.WriteFile(path, []byte("项目乙里的第二版，内容不同，应另存一条。"), 0o644); err != nil {
		t.Fatal(err)
	}
	second := importDesktopProjectNote(t, store, path, "D:/Beta")
	if second.ImportedFiles != 1 || second.DuplicateFiles != 0 {
		t.Fatalf("second import = %#v", second)
	}
	var n int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM knowledge_sources`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("sources = %d", n)
	}
}

func TestDesktopReimportUpdatesLocalSourceWhenOtherProjectHasSameBytes(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "note.md")
	if err := os.WriteFile(path, []byte("项目甲里的第一版正文。"), 0o644); err != nil {
		t.Fatal(err)
	}
	store, err := NewSQLiteStore(filepath.Join(dir, "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	store.SetDesktopOwnerStamp(desktopMembershipUser)
	first := importDesktopProjectNote(t, store, path, "D:/Alpha")
	if first.ImportedFiles != 1 {
		t.Fatalf("first import = %#v", first)
	}
	var alphaID, alphaHash string
	if err := store.db.QueryRowContext(ctx, `SELECT id, content_hash FROM knowledge_sources WHERE project_path = ?`, "D:/Alpha").Scan(&alphaID, &alphaHash); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("两个项目都见过的第二版正文。"), 0o644); err != nil {
		t.Fatal(err)
	}
	second := importDesktopProjectNote(t, store, path, "D:/Beta")
	if second.ImportedFiles != 1 || second.DuplicateFiles != 0 {
		t.Fatalf("beta import = %#v", second)
	}
	third := importDesktopProjectNote(t, store, path, "D:/Alpha")
	if third.ImportedFiles != 1 || third.DuplicateFiles != 0 {
		t.Fatalf("alpha reimport = %#v", third)
	}
	var n int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM knowledge_sources`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("sources = %d", n)
	}
	var id, hash, project string
	if err := store.db.QueryRowContext(ctx, `SELECT id, content_hash, project_path FROM knowledge_sources WHERE id = ?`, alphaID).Scan(&id, &hash, &project); err != nil {
		t.Fatal(err)
	}
	if id != alphaID || project != "D:/Alpha" || hash == alphaHash {
		t.Fatalf("local source id=%s project=%s hash changed=%v", id, project, hash != alphaHash)
	}
	var betaHash string
	if err := store.db.QueryRowContext(ctx, `SELECT content_hash FROM knowledge_sources WHERE project_path = ?`, "D:/Beta").Scan(&betaHash); err != nil {
		t.Fatal(err)
	}
	if hash != betaHash {
		t.Fatalf("alpha hash %q beta hash %q", hash, betaHash)
	}
	var betaID string
	if err := store.db.QueryRowContext(ctx, `SELECT id FROM knowledge_sources WHERE project_path = ?`, "D:/Beta").Scan(&betaID); err != nil {
		t.Fatal(err)
	}
	again := importDesktopProjectNote(t, store, path, "D:/Beta")
	if again.ImportedFiles != 0 || again.DuplicateFiles != 1 {
		t.Fatalf("beta reimport = %#v", again)
	}
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM knowledge_sources`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("sources after beta reimport = %d", n)
	}
	var linked int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM knowledge_memberships WHERE source_id = ? AND kind = ? AND scope_key = ?`, alphaID, MembershipKindProject, "D:/Beta").Scan(&linked); err != nil {
		t.Fatal(err)
	}
	if linked != 0 {
		t.Fatalf("alpha source gained beta membership")
	}
	var skipped string
	for _, item := range again.Items {
		if item.Status == ItemStatusSkippedDuplicate {
			skipped = item.SourceID
		}
	}
	if skipped != betaID {
		t.Fatalf("skipped source = %q want beta %q", skipped, betaID)
	}
}

func TestDesktopHashKeepsTaskSourceInProject(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "note.md")
	if err := os.WriteFile(path, []byte("任务项目里的第一版。"), 0o644); err != nil {
		t.Fatal(err)
	}
	store, err := NewSQLiteStore(filepath.Join(dir, "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	store.SetDesktopOwnerStamp(desktopMembershipUser)
	taskOwner := desktopMembershipUser + ":task-9"
	first, err := store.ImportFiles(ctx, DirectoryImportRequest{
		OwnerID: taskOwner, ProjectPath: "D:/Alpha", SaveScope: SaveScopeProject,
		IncludeExts: []string{".md"}, MaxFileBytes: 4096, DistillMode: DistillModeRules,
	}, []string{path})
	if err != nil {
		t.Fatal(err)
	}
	if first.ImportedFiles != 1 {
		t.Fatalf("task import = %#v", first)
	}
	var alphaID, owner string
	if err := store.db.QueryRowContext(ctx, `SELECT id, owner_id FROM knowledge_sources WHERE project_path = ?`, "D:/Alpha").Scan(&alphaID, &owner); err != nil {
		t.Fatal(err)
	}
	if owner != taskOwner {
		t.Fatalf("owner = %q", owner)
	}
	if err := os.WriteFile(path, []byte("桌面项目里的第二版。"), 0o644); err != nil {
		t.Fatal(err)
	}
	second := importDesktopProjectNote(t, store, path, "D:/Beta")
	if second.ImportedFiles != 1 || second.DuplicateFiles != 0 {
		t.Fatalf("beta import = %#v", second)
	}
	third := importDesktopProjectNote(t, store, path, "D:/Alpha")
	if third.ImportedFiles != 1 || third.DuplicateFiles != 0 {
		t.Fatalf("alpha update = %#v", third)
	}
	if err := store.db.QueryRowContext(ctx, `SELECT owner_id FROM knowledge_sources WHERE id = ?`, alphaID).Scan(&owner); err != nil {
		t.Fatal(err)
	}
	if owner != taskOwner {
		t.Fatalf("owner after update = %q", owner)
	}
	again := importDesktopProjectNote(t, store, path, "D:/Alpha")
	if again.ImportedFiles != 0 || again.DuplicateFiles != 1 {
		t.Fatalf("alpha reimport = %#v", again)
	}
	var n, linked int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM knowledge_sources`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("sources = %d", n)
	}
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM knowledge_memberships WHERE source_id <> ? AND kind = ? AND scope_key = ?`, alphaID, MembershipKindProject, "D:/Alpha").Scan(&linked); err != nil {
		t.Fatal(err)
	}
	if linked != 0 {
		t.Fatalf("beta source gained alpha membership")
	}
	var skipped string
	for _, item := range again.Items {
		if item.Status == ItemStatusSkippedDuplicate {
			skipped = item.SourceID
		}
	}
	if skipped != alphaID {
		t.Fatalf("skipped source = %q want %q", skipped, alphaID)
	}
}

func TestDesktopBatchCopyDoesNotLinkOtherProject(t *testing.T) {
	for _, order := range []string{"copy-first", "note-first"} {
		t.Run(order, func(t *testing.T) {
			ctx := context.Background()
			dir := t.TempDir()
			note := filepath.Join(dir, "note.md")
			copyPath := filepath.Join(dir, "copy.md")
			if err := os.WriteFile(note, []byte("项目甲里的第一版正文。"), 0o644); err != nil {
				t.Fatal(err)
			}
			store, err := NewSQLiteStore(filepath.Join(dir, "knowledge.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			store.SetDesktopOwnerStamp(desktopMembershipUser)
			first := importDesktopProjectNote(t, store, note, "D:/Alpha")
			if first.ImportedFiles != 1 {
				t.Fatalf("first import = %#v", first)
			}
			var alphaID string
			if err := store.db.QueryRowContext(ctx, `SELECT id FROM knowledge_sources WHERE project_path = ?`, "D:/Alpha").Scan(&alphaID); err != nil {
				t.Fatal(err)
			}
			v2 := []byte("同一批里的第二版正文，另一个项目已经存过。")
			if err := os.WriteFile(note, v2, 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(copyPath, v2, 0o644); err != nil {
				t.Fatal(err)
			}
			second := importDesktopProjectNote(t, store, note, "D:/Beta")
			if second.ImportedFiles != 1 || second.DuplicateFiles != 0 {
				t.Fatalf("beta import = %#v", second)
			}
			paths := []string{note, copyPath}
			if order == "copy-first" {
				paths = []string{copyPath, note}
			}
			batch, err := store.ImportFiles(ctx, DirectoryImportRequest{
				ProjectPath:  "D:/Alpha",
				SaveScope:    SaveScopeProject,
				IncludeExts:  []string{".md"},
				MaxFileBytes: 4096,
				DistillMode:  DistillModeRules,
			}, paths)
			if err != nil {
				t.Fatal(err)
			}
			if batch.ImportedFiles != 1 || batch.DuplicateFiles != 1 {
				t.Fatalf("batch = %#v", batch)
			}
			var n int
			if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM knowledge_sources`).Scan(&n); err != nil {
				t.Fatal(err)
			}
			if n != 2 {
				t.Fatalf("sources = %d", n)
			}
			var id, hash, project string
			if err := store.db.QueryRowContext(ctx, `SELECT id, content_hash, project_path FROM knowledge_sources WHERE id = ?`, alphaID).Scan(&id, &hash, &project); err != nil {
				t.Fatal(err)
			}
			var betaID, betaHash string
			if err := store.db.QueryRowContext(ctx, `SELECT id, content_hash FROM knowledge_sources WHERE project_path = ?`, "D:/Beta").Scan(&betaID, &betaHash); err != nil {
				t.Fatal(err)
			}
			if id != alphaID || project != "D:/Alpha" || hash != betaHash {
				t.Fatalf("local id=%s project=%s hash=%s beta=%s", id, project, hash, betaHash)
			}
			var linked int
			if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM knowledge_memberships WHERE source_id = ? AND kind = ? AND scope_key = ?`, betaID, MembershipKindProject, "D:/Alpha").Scan(&linked); err != nil {
				t.Fatal(err)
			}
			if linked != 0 {
				t.Fatal("beta source gained alpha membership")
			}
			var copyID string
			for _, item := range batch.Items {
				if !strings.HasSuffix(item.FilePath, "copy.md") {
					continue
				}
				if item.Status != ItemStatusSkippedDuplicate {
					t.Fatalf("copy status = %s", item.Status)
				}
				copyID = item.SourceID
			}
			if copyID != alphaID {
				t.Fatalf("copy source = %q want %q", copyID, alphaID)
			}
		})
	}
}

func TestDesktopSharedMembershipUpdatesInPlace(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "note.md")
	if err := os.WriteFile(path, []byte("两个项目共用的第一版。"), 0o644); err != nil {
		t.Fatal(err)
	}
	store, err := NewSQLiteStore(filepath.Join(dir, "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	store.SetDesktopOwnerStamp(desktopMembershipUser)
	first := importDesktopProjectNote(t, store, path, "D:/Alpha")
	if first.ImportedFiles != 1 {
		t.Fatalf("first import = %#v", first)
	}
	second := importDesktopProjectNote(t, store, path, "D:/Beta")
	if second.ImportedFiles != 0 || second.DuplicateFiles != 1 {
		t.Fatalf("share import = %#v", second)
	}
	var id, oldHash, project string
	if err := store.db.QueryRowContext(ctx, `SELECT id, content_hash, project_path FROM knowledge_sources`).Scan(&id, &oldHash, &project); err != nil {
		t.Fatal(err)
	}
	if project != "D:/Alpha" {
		t.Fatalf("project = %q", project)
	}
	if err := os.WriteFile(path, []byte("共用来源上的第二版，应更新这一条。"), 0o644); err != nil {
		t.Fatal(err)
	}
	third := importDesktopProjectNote(t, store, path, "D:/Beta")
	if third.ImportedFiles != 1 || third.DuplicateFiles != 0 {
		t.Fatalf("beta update = %#v", third)
	}
	var n int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM knowledge_sources`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("sources = %d", n)
	}
	wantID := id
	var hash string
	if err := store.db.QueryRowContext(ctx, `SELECT id, content_hash, project_path FROM knowledge_sources`).Scan(&id, &hash, &project); err != nil {
		t.Fatal(err)
	}
	if id != wantID || project != "D:/Alpha" || hash == oldHash {
		t.Fatalf("id=%s want=%s project=%s hash changed=%v", id, wantID, project, hash != oldHash)
	}
	var alpha, beta int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM knowledge_memberships WHERE source_id = ? AND kind = ? AND scope_key = ?`, id, MembershipKindProject, "D:/Alpha").Scan(&alpha); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM knowledge_memberships WHERE source_id = ? AND kind = ? AND scope_key = ?`, id, MembershipKindProject, "D:/Beta").Scan(&beta); err != nil {
		t.Fatal(err)
	}
	if alpha != 1 || beta != 1 {
		t.Fatalf("alpha=%d beta=%d", alpha, beta)
	}
}

func TestDesktopSharedMembershipUpdateSkipsOtherProjectCopy(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "note.md")
	if err := os.WriteFile(path, []byte("两个项目共用的第一版。"), 0o644); err != nil {
		t.Fatal(err)
	}
	store, err := NewSQLiteStore(filepath.Join(dir, "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	store.SetDesktopOwnerStamp(desktopMembershipUser)
	if res := importDesktopProjectNote(t, store, path, "D:/Alpha"); res.ImportedFiles != 1 {
		t.Fatalf("alpha import = %#v", res)
	}
	if res := importDesktopProjectNote(t, store, path, "D:/Beta"); res.ImportedFiles != 0 || res.DuplicateFiles != 1 {
		t.Fatalf("share import = %#v", res)
	}
	var sharedID string
	if err := store.db.QueryRowContext(ctx, `SELECT id FROM knowledge_sources`).Scan(&sharedID); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("另一个项目先写入的第二版。"), 0o644); err != nil {
		t.Fatal(err)
	}
	if res := importDesktopProjectNote(t, store, path, "D:/Gamma"); res.ImportedFiles != 1 || res.DuplicateFiles != 0 {
		t.Fatalf("gamma import = %#v", res)
	}
	var n int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM knowledge_sources`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("sources after gamma = %d", n)
	}
	var gammaID, gammaHash string
	if err := store.db.QueryRowContext(ctx, `SELECT id, content_hash FROM knowledge_sources WHERE id <> ?`, sharedID).Scan(&gammaID, &gammaHash); err != nil {
		t.Fatal(err)
	}
	updated := importDesktopProjectNote(t, store, path, "D:/Beta")
	if updated.ImportedFiles != 1 || updated.DuplicateFiles != 0 {
		t.Fatalf("beta update = %#v", updated)
	}
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM knowledge_sources`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("sources = %d", n)
	}
	var hash, project string
	if err := store.db.QueryRowContext(ctx, `SELECT content_hash, project_path FROM knowledge_sources WHERE id = ?`, sharedID).Scan(&hash, &project); err != nil {
		t.Fatal(err)
	}
	if project != "D:/Alpha" || hash != gammaHash {
		t.Fatalf("shared project=%s hash=%s gamma=%s", project, hash, gammaHash)
	}
	var linked int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM knowledge_memberships WHERE source_id = ? AND kind = ? AND scope_key = ?`, gammaID, MembershipKindProject, "D:/Beta").Scan(&linked); err != nil {
		t.Fatal(err)
	}
	if linked != 0 {
		t.Fatal("gamma source gained beta membership")
	}
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM knowledge_memberships WHERE source_id = ? AND kind = ? AND scope_key = ?`, sharedID, MembershipKindProject, "D:/Beta").Scan(&linked); err != nil {
		t.Fatal(err)
	}
	if linked != 1 {
		t.Fatalf("shared beta membership = %d", linked)
	}
}

func TestDesktopHashKeepsMemberedSource(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "note.md")
	v1 := []byte("成员来源上的第一版。")
	if err := os.WriteFile(path, v1, 0o644); err != nil {
		t.Fatal(err)
	}
	store, err := NewSQLiteStore(filepath.Join(dir, "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	store.SetDesktopOwnerStamp(desktopMembershipUser)
	if res := importDesktopProjectNote(t, store, path, "D:/Alpha"); res.ImportedFiles != 1 {
		t.Fatalf("alpha import = %#v", res)
	}
	if res := importDesktopProjectNote(t, store, path, "D:/Beta"); res.ImportedFiles != 0 || res.DuplicateFiles != 1 {
		t.Fatalf("share import = %#v", res)
	}
	var sharedID, sharedHash string
	if err := store.db.QueryRowContext(ctx, `SELECT id, content_hash FROM knowledge_sources`).Scan(&sharedID, &sharedHash); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("另一个项目先写入的第二版。"), 0o644); err != nil {
		t.Fatal(err)
	}
	if res := importDesktopProjectNote(t, store, path, "D:/Gamma"); res.ImportedFiles != 1 || res.DuplicateFiles != 0 {
		t.Fatalf("gamma import = %#v", res)
	}
	if err := os.WriteFile(path, v1, 0o644); err != nil {
		t.Fatal(err)
	}
	if res := importDesktopProjectNote(t, store, path, "D:/Gamma"); res.ImportedFiles != 1 || res.DuplicateFiles != 0 {
		t.Fatalf("gamma revert = %#v", res)
	}
	var n int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM knowledge_sources`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("sources = %d", n)
	}
	var gammaID, gammaHash string
	if err := store.db.QueryRowContext(ctx, `SELECT id, content_hash FROM knowledge_sources WHERE id <> ?`, sharedID).Scan(&gammaID, &gammaHash); err != nil {
		t.Fatal(err)
	}
	if gammaHash != sharedHash {
		t.Fatalf("gamma hash %s shared %s", gammaHash, sharedHash)
	}
	again := importDesktopProjectNote(t, store, path, "D:/Beta")
	if again.ImportedFiles != 0 || again.DuplicateFiles != 1 {
		t.Fatalf("beta reimport = %#v", again)
	}
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM knowledge_sources`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("sources after beta = %d", n)
	}
	var skipped string
	for _, item := range again.Items {
		if item.Status == ItemStatusSkippedDuplicate {
			skipped = item.SourceID
		}
	}
	if skipped != sharedID {
		t.Fatalf("skipped source = %q want %q", skipped, sharedID)
	}
	var linked int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM knowledge_memberships WHERE source_id = ? AND kind = ? AND scope_key = ?`, gammaID, MembershipKindProject, "D:/Beta").Scan(&linked); err != nil {
		t.Fatal(err)
	}
	if linked != 0 {
		t.Fatal("gamma source gained beta membership")
	}
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM knowledge_memberships WHERE source_id = ? AND kind = ? AND scope_key = ?`, sharedID, MembershipKindProject, "D:/Beta").Scan(&linked); err != nil {
		t.Fatal(err)
	}
	if linked != 1 {
		t.Fatalf("shared beta membership = %d", linked)
	}
}

func TestDesktopUnchangedImportDoesNotSkipOtherOwner(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "note.md")
	if err := os.WriteFile(path, []byte("别人的未改动正文，桌面导入仍应写入自己的来源。"), 0o644); err != nil {
		t.Fatal(err)
	}
	store, err := NewSQLiteStore(filepath.Join(dir, "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	first := importDesktopNote(t, store, path, "alice")
	if first.ImportedFiles != 1 {
		t.Fatalf("first import = %#v", first)
	}
	store.SetDesktopOwnerStamp(desktopMembershipUser)
	second := importDesktopNote(t, store, path, "")
	if second.ImportedFiles != 1 || second.DuplicateFiles != 0 {
		t.Fatalf("second import = %#v", second)
	}
	var n, alice, desktop int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM knowledge_sources`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM knowledge_sources WHERE owner_id = 'alice'`).Scan(&alice); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM knowledge_sources WHERE owner_id = ?`, desktopMembershipUser).Scan(&desktop); err != nil {
		t.Fatal(err)
	}
	if n != 2 || alice != 1 || desktop != 1 {
		t.Fatalf("sources = %d alice = %d desktop = %d", n, alice, desktop)
	}
}

func importDesktopProjectNote(t *testing.T, store *SQLiteStore, path, project string) DirectoryImportResult {
	t.Helper()
	res, err := store.ImportFiles(context.Background(), DirectoryImportRequest{
		ProjectPath:  project,
		SaveScope:    SaveScopeProject,
		IncludeExts:  []string{".md"},
		MaxFileBytes: 4096,
		DistillMode:  DistillModeRules,
	}, []string{path})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func importDesktopNote(t *testing.T, store *SQLiteStore, path, owner string) DirectoryImportResult {
	t.Helper()
	res, err := store.ImportFiles(context.Background(), DirectoryImportRequest{
		OwnerID:      owner,
		IncludeExts:  []string{".md"},
		MaxFileBytes: 4096,
		DistillMode:  DistillModeRules,
	}, []string{path})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestRebuildSourceDerivedRebuildsRowTextWithoutCardVector(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Now().UTC()
	source := Source{
		ID: "row-source", Kind: SourceKindCSV, URI: "rows.csv", Title: "rows", Status: StatusParsed,
		FetchedAt: now, CreatedAt: now, UpdatedAt: now, ContentHash: "rows",
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := insertSource(ctx, tx, source); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO kb_tables(id, source_id, sheet_name, created_at, updated_at) VALUES ('row-table', 'row-source', 'Sheet1', ?, ?)`, formatTime(now), formatTime(now)); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO kb_rows(id, table_id, source_id, row_index, primary_key_text, row_text, created_at, updated_at) VALUES ('row-1', 'row-table', 'row-source', 1, '张三', '部门: 法务', ?, ?)`, formatTime(now), formatTime(now)); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO kb_rows_fts(row_id, primary_key_text, row_text) VALUES ('row-1', 'STALE', 'STALE')`); err != nil {
		t.Fatal(err)
	}
	if err := insertDocumentNode(ctx, tx, DocumentNode{ID: "row-node", SourceID: source.ID, Type: "paragraph", Text: "部门 法务", TokenCount: 4}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	store.SetDesktopOwnerStamp(desktopMembershipUser)
	rebuilt, err := store.RebuildSourceDerived(ctx, source.ID, DistillModeRules)
	if err != nil {
		t.Fatal(err)
	}
	if rebuilt.ID != source.ID {
		t.Fatalf("rebuilt = %#v", rebuilt)
	}
	var owner string
	if err := store.db.QueryRowContext(ctx, `SELECT COALESCE(owner_id, '') FROM knowledge_sources WHERE id = ?`, source.ID).Scan(&owner); err != nil {
		t.Fatal(err)
	}
	if owner != "" {
		t.Fatalf("rebuild stamped empty owner to %q", owner)
	}
	var pk, text string
	if err := store.db.QueryRowContext(ctx, `SELECT primary_key_text, row_text FROM kb_rows_fts WHERE row_id = 'row-1'`).Scan(&pk, &text); err != nil {
		t.Fatalf("row fts: %v", err)
	}
	if pk == "STALE" || text == "STALE" {
		t.Fatalf("row fts was not rebuilt from row text: %q %q", pk, text)
	}
}

func TestSearchByEmbeddingCombinesCardAndNodeVectors(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	store.UseEmbedder(directionalKnowledgeEmbedder{})
	if err := store.EnsureNodeEmbeddingColumn(); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	source := Source{ID: "body-source", Kind: SourceKindText, URI: "manual://body", Status: StatusParsed, FetchedAt: now, CreatedAt: now, UpdatedAt: now}
	if err := insertSource(ctx, tx, source); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, insertCardSQL,
		"card-closer", source.ID, "body-node", "closer", "card claim", "", "[]", "[]", "[]", "", "", "",
		"", "", 0.9, 1, 0.9, float32SliceToBytes([]float32{1, 0}), formatTime(now), formatTime(now)); err != nil {
		t.Fatal(err)
	}
	if err := upsertEmbeddingMetadataTx(ctx, tx, embeddingEntityCard, "card-closer", embeddingModelIdentifier(directionalKnowledgeEmbedder{}), 2); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO document_nodes(id, source_id, type, title, text, page) VALUES ('body-node', ?, 'paragraph', 'body', 'node text', 7)`, source.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO document_nodes(id, source_id, type, title, text, embedding) VALUES ('other-node', ?, 'paragraph', 'other', 'other body', ?)`, source.ID, float32SliceToBytes([]float32{0.8, 0.2})); err != nil {
		t.Fatal(err)
	}
	if err := upsertEmbeddingMetadataTx(ctx, tx, embeddingEntityNode, "other-node", embeddingModelIdentifier(directionalKnowledgeEmbedder{}), 2); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	results, err := store.searchByEmbedding(ctx, SearchOptions{Query: "target semantic query", Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 || results[0].ResultType != "node" || results[0].NodeID != "body-node" || results[0].CardID != "card-closer" || results[1].NodeID != "other-node" {
		t.Fatalf("results = %#v, want distinct bodies after vector deduplication", results)
	}
	if results[0].Page != 7 || !strings.Contains(results[0].Citation, "page 7") {
		t.Fatalf("card-only vector hit lost node citation: %#v", results[0])
	}
}

func TestSearchByEmbeddingExpandsDuplicateCardBodies(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	store.UseEmbedder(directionalKnowledgeEmbedder{})
	if err := store.SaveSource(ctx, Source{ID: "duplicate-vector-source", Kind: SourceKindText, URI: "manual://duplicate-vectors", Status: StatusParsed}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"body-a", "body-b"} {
		if err := store.SaveDocumentNode(ctx, DocumentNode{ID: id, SourceID: "duplicate-vector-source", Type: "paragraph", Text: id}); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 9; i++ {
		if err := store.SaveCard(ctx, Card{ID: fmt.Sprintf("same-body-%d", i), SourceID: "duplicate-vector-source", NodeID: "body-a", Claim: "same body", Embedding: []float32{1, 0}}); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.SaveCard(ctx, Card{ID: "other-body", SourceID: "duplicate-vector-source", NodeID: "body-b", Claim: "other body", Embedding: []float32{0.99, 0.01}}); err != nil {
		t.Fatal(err)
	}
	results, err := store.searchByEmbedding(ctx, SearchOptions{Query: "target semantic query", Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 || results[0].NodeID != "body-a" || results[1].NodeID != "body-b" {
		t.Fatalf("duplicate cards starved distinct bodies: %#v", results)
	}
}
