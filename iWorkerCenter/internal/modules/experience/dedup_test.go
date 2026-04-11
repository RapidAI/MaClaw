package experience

import (
	"database/sql"
	"os"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func setupDedupDB(t *testing.T) *sql.DB {
	t.Helper()
	f, err := os.CreateTemp("", "dedup_test_*.db")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	t.Cleanup(func() { os.Remove(f.Name()) })

	db, err := sql.Open("sqlite", f.Name())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	_, err = db.Exec(`CREATE TABLE shared_memories (
		id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL DEFAULT '', title TEXT, content TEXT, level TEXT, scope TEXT,
		tags TEXT, version INTEGER, status TEXT, created_at TEXT, updated_at TEXT)`)
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func insertMemory(db *sql.DB, id, title, scope string, createdAt time.Time) {
	db.Exec(`INSERT INTO shared_memories (id, tenant_id, title, content, level, scope, tags, version, status, created_at, updated_at)
		VALUES (?, 'test-tenant', ?, 'content', 'role', ?, '["自动提取"]', 1, 'active', ?, ?)`,
		id, title, scope, createdAt.Format(time.RFC3339), createdAt.Format(time.RFC3339))
}

func TestDedup_MergesDuplicates(t *testing.T) {
	db := setupDedupDB(t)
	dedup := NewDeduplicator(db, db)

	now := time.Now()
	insertMemory(db, "m1", "如何处理质量问题报告", "quality", now.Add(-2*time.Hour))
	insertMemory(db, "m2", "如何处理质量问题的报告", "quality", now.Add(-1*time.Hour))
	insertMemory(db, "m3", "完全不同的经验标题", "quality", now)

	result := dedup.RunDedup()
	if result.Scanned != 3 {
		t.Errorf("expected 3 scanned, got %d", result.Scanned)
	}
	if result.Merged != 1 {
		t.Errorf("expected 1 merged, got %d", result.Merged)
	}

	// m1 should be merged (disabled), m2 and m3 should remain active
	var status string
	db.QueryRow("SELECT status FROM shared_memories WHERE id='m1'").Scan(&status)
	if status != "merged" {
		t.Errorf("expected m1 status 'merged', got %q", status)
	}
}

func TestDedup_DifferentScopesNotMerged(t *testing.T) {
	db := setupDedupDB(t)
	dedup := NewDeduplicator(db, db)

	now := time.Now()
	insertMemory(db, "m1", "如何处理质量问题", "quality", now)
	insertMemory(db, "m2", "如何处理质量问题", "production", now)

	result := dedup.RunDedup()
	if result.Merged != 0 {
		t.Errorf("expected 0 merged (different scopes), got %d", result.Merged)
	}
}

func TestDedup_Expiry(t *testing.T) {
	db := setupDedupDB(t)
	dedup := NewDeduplicator(db, db)

	old := time.Now().AddDate(0, 0, -100)
	recent := time.Now().Add(-1 * time.Hour)
	insertMemory(db, "m1", "旧经验", "quality", old)
	insertMemory(db, "m2", "新经验", "quality", recent)

	expired := dedup.RunExpiry(90)
	if expired != 1 {
		t.Errorf("expected 1 expired, got %d", expired)
	}

	var status string
	db.QueryRow("SELECT status FROM shared_memories WHERE id='m1'").Scan(&status)
	if status != "expired" {
		t.Errorf("expected m1 status 'expired', got %q", status)
	}
	db.QueryRow("SELECT status FROM shared_memories WHERE id='m2'").Scan(&status)
	if status != "active" {
		t.Errorf("expected m2 status 'active', got %q", status)
	}
}

func TestJaccardSimilarity(t *testing.T) {
	tests := []struct {
		a, b     string
		minSim   float64
		maxSim   float64
	}{
		{"完全相同", "完全相同", 1.0, 1.0},
		{"如何处理质量问题", "如何处理质量问题报告", 0.6, 1.0},
		{"苹果", "橘子", 0.0, 0.3},
		{"", "", 1.0, 1.0},
	}
	for _, tt := range tests {
		sim := jaccardSimilarity(tt.a, tt.b)
		if sim < tt.minSim || sim > tt.maxSim {
			t.Errorf("jaccardSimilarity(%q, %q) = %.2f, want [%.2f, %.2f]", tt.a, tt.b, sim, tt.minSim, tt.maxSim)
		}
	}
}
