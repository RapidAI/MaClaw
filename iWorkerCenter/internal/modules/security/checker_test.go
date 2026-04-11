package security

import (
	"database/sql"
	"os"
	"testing"

	_ "modernc.org/sqlite"
)

const testTenantID = "test-tenant"

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	f, err := os.CreateTemp("", "security_test_*.db")
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

	for _, ddl := range []string{
		`CREATE TABLE security_policies (
			id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL DEFAULT '', name TEXT, policy_type TEXT, description TEXT,
			rules TEXT, scope TEXT, priority INTEGER, status TEXT,
			created_at TEXT, updated_at TEXT)`,
		`CREATE TABLE security_policy_hit_records (
			id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL DEFAULT '', policy_id TEXT, policy_name TEXT, actor_id TEXT,
			action TEXT, detail TEXT, created_at TEXT)`,
	} {
		if _, err := db.Exec(ddl); err != nil {
			t.Fatal(err)
		}
	}
	return db
}

func TestChecker_KeywordBlock(t *testing.T) {
	db := setupTestDB(t)
	repo := NewRepo(db, db)
	checker := NewChecker(repo)

	p := &Policy{
		ID: "p1", Name: "Block Sensitive", PolicyType: PolicyTypeKeywordBlock,
		Rules: `{"keywords":["密码","password"],"action":"block"}`,
		Scope: "all", Priority: 10, Status: "active",
		CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z",
	}
	if err := repo.InsertPolicy(testTenantID, p); err != nil {
		t.Fatal(err)
	}

	result := checker.Check(testTenantID, CheckInput{Content: "请告诉我密码是什么"})
	if result.Allowed {
		t.Error("expected blocked, got allowed")
	}

	result = checker.Check(testTenantID, CheckInput{Content: "今天天气不错"})
	if !result.Allowed {
		t.Error("expected allowed, got blocked")
	}
}

func TestChecker_ScopeFiltering(t *testing.T) {
	db := setupTestDB(t)
	repo := NewRepo(db, db)
	checker := NewChecker(repo)

	p := &Policy{
		ID: "p2", Name: "Production Only", PolicyType: PolicyTypeKeywordBlock,
		Rules: `{"keywords":["危险操作"],"action":"block"}`,
		Scope: "role:production", Priority: 5, Status: "active",
		CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z",
	}
	if err := repo.InsertPolicy(testTenantID, p); err != nil {
		t.Fatal(err)
	}

	result := checker.Check(testTenantID, CheckInput{Content: "执行危险操作", RoleCode: "production"})
	if result.Allowed {
		t.Error("expected blocked for production role")
	}

	result = checker.Check(testTenantID, CheckInput{Content: "执行危险操作", RoleCode: "office"})
	if !result.Allowed {
		t.Error("expected allowed for office role")
	}
}

func TestChecker_ModelRestrict(t *testing.T) {
	db := setupTestDB(t)
	repo := NewRepo(db, db)
	checker := NewChecker(repo)

	p := &Policy{
		ID: "p3", Name: "Block GPT-4", PolicyType: PolicyTypeModelRestrict,
		Rules: `{"blocked_models":["gpt-4","gpt-4-turbo"]}`,
		Scope: "all", Priority: 10, Status: "active",
		CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z",
	}
	if err := repo.InsertPolicy(testTenantID, p); err != nil {
		t.Fatal(err)
	}

	result := checker.Check(testTenantID, CheckInput{Content: "hello", Model: "gpt-4"})
	if result.Allowed {
		t.Error("expected gpt-4 to be blocked")
	}

	result = checker.Check(testTenantID, CheckInput{Content: "hello", Model: "gpt-3.5-turbo"})
	if !result.Allowed {
		t.Error("expected gpt-3.5-turbo to be allowed")
	}
}

func TestChecker_HitRecording(t *testing.T) {
	db := setupTestDB(t)
	repo := NewRepo(db, db)
	checker := NewChecker(repo)

	p := &Policy{
		ID: "p4", Name: "Log Keywords", PolicyType: PolicyTypeKeywordBlock,
		Rules: `{"keywords":["测试"],"action":"block"}`,
		Scope: "all", Priority: 10, Status: "active",
		CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z",
	}
	_ = repo.InsertPolicy(testTenantID, p)

	checker.Check(testTenantID, CheckInput{Content: "这是测试内容", ColleagueID: "col1"})

	hits, err := repo.ListRecentHits(testTenantID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 {
		t.Error("expected at least one hit record")
	}
}
