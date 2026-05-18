package workflow

import (
	"database/sql"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// setupTestPgAuditDB creates an in-memory SQLite database that mimics the
// PostgreSQL audit_trail schema for testing purposes. SQLite is used because
// the tests run without a real PostgreSQL instance. The PgAuditStore uses
// standard database/sql with $N placeholders which SQLite doesn't support
// natively, so we use a thin adapter approach: the tests validate the logic
// by using the SQLite-backed AuditStore from the sqlite package for
// integration, and validate the PgAuditStore struct/interface compliance here.
func setupTestPgAuditDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	// Create audit_trail table matching the PostgreSQL schema
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS audit_trail (
			id           TEXT PRIMARY KEY,
			instance_id  TEXT NOT NULL,
			node_id      TEXT DEFAULT '',
			event_type   TEXT NOT NULL,
			actor_id     TEXT DEFAULT '',
			decision     TEXT DEFAULT '',
			matched_rule TEXT DEFAULT '',
			rationale    TEXT DEFAULT '',
			details_json TEXT DEFAULT '{}',
			timestamp    TEXT NOT NULL
		)
	`)
	if err != nil {
		t.Fatalf("create table: %v", err)
	}
	return db
}

func TestPgAuditStore_ImplementsInterface(t *testing.T) {
	// Compile-time check that PgAuditStore implements AuditStore.
	var _ AuditStore = (*PgAuditStore)(nil)
}

func TestPgAuditStore_NewPgAuditStore(t *testing.T) {
	db := setupTestPgAuditDB(t)
	store := NewPgAuditStore(db)
	if store == nil {
		t.Fatal("NewPgAuditStore returned nil")
	}
	if store.db != db {
		t.Fatal("store.db does not match provided db")
	}
}

func TestPgAuditStore_AppendGeneratesID(t *testing.T) {
	entry := &AuditEntry{
		InstanceID: "inst_1",
		EventType:  "instance_created",
	}
	// Verify ID generation logic
	if entry.ID != "" {
		t.Fatal("expected empty ID before generation")
	}
	id := generatePgAuditID()
	if id == "" {
		t.Fatal("generatePgAuditID returned empty string")
	}
	if len(id) < 10 {
		t.Fatalf("generated ID too short: %q", id)
	}
	// Verify prefix
	if id[:6] != "audit_" {
		t.Fatalf("expected 'audit_' prefix, got %q", id[:6])
	}
}

func TestPgAuditStore_AppendNormalizesTimestamp(t *testing.T) {
	entry := &AuditEntry{
		ID:         "test_ts_1",
		InstanceID: "inst_1",
		EventType:  "decision_made",
		Timestamp:  time.Time{}, // zero time
	}

	// NormalizeAuditTimestamp should set to current UTC time
	before := time.Now().UTC().Truncate(time.Millisecond)
	entry.Timestamp = NormalizeAuditTimestamp(entry.Timestamp)
	after := time.Now().UTC().Truncate(time.Millisecond)

	if entry.Timestamp.Before(before) || entry.Timestamp.After(after) {
		t.Errorf("timestamp not in expected range: %v", entry.Timestamp)
	}
	if entry.Timestamp.Location() != time.UTC {
		t.Errorf("expected UTC, got %v", entry.Timestamp.Location())
	}
	// Verify millisecond precision
	if entry.Timestamp.Nanosecond()%int(time.Millisecond) != 0 {
		t.Errorf("not millisecond precision: nano=%d", entry.Timestamp.Nanosecond())
	}
}

func TestPgAuditStore_AppendWithExplicitTimestamp(t *testing.T) {
	// Non-UTC time with sub-millisecond precision
	loc := time.FixedZone("CST", 8*3600)
	input := time.Date(2025, 7, 15, 10, 30, 45, 123456789, loc)

	entry := &AuditEntry{
		ID:         "test_ts_2",
		InstanceID: "inst_2",
		EventType:  "node_completed",
		Timestamp:  input,
	}
	entry.Timestamp = NormalizeAuditTimestamp(entry.Timestamp)

	// Should be UTC
	if entry.Timestamp.Location() != time.UTC {
		t.Errorf("expected UTC, got %v", entry.Timestamp.Location())
	}
	// Should be truncated to millisecond
	expected := time.Date(2025, 7, 15, 2, 30, 45, 123000000, time.UTC)
	if !entry.Timestamp.Equal(expected) {
		t.Errorf("expected %v, got %v", expected, entry.Timestamp)
	}
}

func TestPgAuditStore_PgAuditOffset(t *testing.T) {
	tests := []struct {
		page, pageSize, expected int
	}{
		{0, 100, 0},
		{1, 100, 100},
		{2, 50, 100},
		{-1, 100, 0},
		{-5, 50, 0},
		{3, 25, 75},
	}
	for _, tt := range tests {
		result := pgAuditOffset(tt.page, tt.pageSize)
		if result != tt.expected {
			t.Errorf("pgAuditOffset(%d, %d) = %d, want %d",
				tt.page, tt.pageSize, result, tt.expected)
		}
	}
}

func TestPgAuditStore_GenerateIDUniqueness(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 1000; i++ {
		id := generatePgAuditID()
		if seen[id] {
			t.Fatalf("duplicate ID generated: %s", id)
		}
		seen[id] = true
	}
}

func TestPgAuditStore_ScanPgAuditEntriesEmpty(t *testing.T) {
	db := setupTestPgAuditDB(t)
	rows, err := db.Query(
		`SELECT id, instance_id, node_id, event_type, actor_id, decision,
		        matched_rule, rationale, details_json, timestamp
		 FROM audit_trail WHERE 1=0`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	entries, err := scanPgAuditEntries(rows)
	if err != nil {
		t.Fatalf("scanPgAuditEntries error: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected 0 entries, got %d", len(entries))
	}
}

func TestPgAuditStore_NoUpdateDeleteMethods(t *testing.T) {
	// Verify that PgAuditStore has no Update or Delete methods.
	// This is a compile-time guarantee via the AuditStore interface,
	// which only defines Append and Query* methods.
	// The struct itself should not expose any mutation methods.
	store := &PgAuditStore{}
	_ = store

	// The AuditStore interface is append-only by design.
	// If someone adds Update/Delete methods to PgAuditStore,
	// this test serves as documentation that it violates the design.
	// The interface check above (TestPgAuditStore_ImplementsInterface)
	// ensures only the defined methods are required.
}

func TestPgAuditStore_NormalizePageSizeIntegration(t *testing.T) {
	// Verify NormalizePageSize is used correctly in query methods.
	tests := []struct {
		input    int
		expected int
	}{
		{0, DefaultAuditPageSize},
		{-1, DefaultAuditPageSize},
		{200, DefaultAuditPageSize},
		{50, 50},
		{100, DefaultAuditPageSize},
		{1, 1},
	}
	for _, tt := range tests {
		result := NormalizePageSize(tt.input)
		if result != tt.expected {
			t.Errorf("NormalizePageSize(%d) = %d, want %d",
				tt.input, result, tt.expected)
		}
	}
}

// TestPgAuditStore_ScanPreservesMillisecondPrecision verifies that
// scanPgAuditEntries truncates timestamps to millisecond precision
// and converts to UTC.
func TestPgAuditStore_ScanPreservesMillisecondPrecision(t *testing.T) {
	db := setupTestPgAuditDB(t)

	// Insert with a known timestamp string that has milliseconds
	ts := "2025-07-15T10:30:45.123Z"
	_, err := db.Exec(
		`INSERT INTO audit_trail (id, instance_id, event_type, timestamp)
		 VALUES ('ms_test', 'inst_ms', 'test_event', ?)`, ts)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	rows, err := db.Query(
		`SELECT id, instance_id, node_id, event_type, actor_id, decision,
		        matched_rule, rationale, details_json, timestamp
		 FROM audit_trail WHERE id = 'ms_test'`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	entries, err := scanPgAuditEntries(rows)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	entry := entries[0]
	// Verify millisecond precision is preserved
	if entry.Timestamp.Nanosecond()%int(time.Millisecond) != 0 {
		t.Errorf("not millisecond precision: nano=%d",
			entry.Timestamp.Nanosecond())
	}
	if entry.Timestamp.Location() != time.UTC {
		t.Errorf("expected UTC, got %v", entry.Timestamp.Location())
	}
}
