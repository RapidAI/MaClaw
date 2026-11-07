package sqlite

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

func TestListTenantIDsByEmailUsesReadConnection(t *testing.T) {
	s := newTestStore(t)
	repo := s.Users.(*userRepo)
	now := time.Now().UTC()
	if err := repo.Create(context.Background(), &store.User{
		ID: "user_write", TenantID: "tenant_default", Email: "Owner@Example.com", SN: "SN-WRITE", Status: "active", EnrollmentStatus: "approved", CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	closed, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open closed db: %v", err)
	}
	if err := closed.Close(); err != nil {
		t.Fatalf("close write db: %v", err)
	}
	repo.db = closed

	ids, err := repo.ListTenantIDsByEmail(context.Background(), "owner@example.com")
	if err != nil {
		t.Fatalf("ListTenantIDsByEmail: %v", err)
	}
	if len(ids) != 1 || ids[0] != "tenant_default" {
		t.Fatalf("ids = %#v", ids)
	}
}

func TestListTenantIDsByEmailFindsLegacyMixedCaseAccountViaIdentity(t *testing.T) {
	s := newTestStore(t)
	repo := s.Users.(*userRepo)
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := repo.db.Exec(`INSERT INTO users (id, tenant_id, email, sn, status, enrollment_status, created_at, updated_at)
		VALUES ('user_legacy', 'tenant_qianxin', 'Owner@Example.com', 'SN-LEGACY', 'active', 'approved', ?, ?)`, now, now); err != nil {
		t.Fatalf("insert legacy user: %v", err)
	}
	if _, err := repo.db.Exec(`INSERT INTO user_identities (id, tenant_id, user_id, type, value, verified, created_at, updated_at)
		VALUES ('user_legacy_email', 'tenant_qianxin', 'user_legacy', 'email', 'owner@example.com', 0, ?, ?)`, now, now); err != nil {
		t.Fatalf("insert legacy identity: %v", err)
	}
	if _, err := repo.db.Exec(`INSERT INTO user_identities (id, tenant_id, user_id, type, value, verified, created_at, updated_at)
		VALUES ('orphan_email', 'tenant_qianxin', 'missing_user', 'email', 'ghost@example.com', 0, ?, ?)`, now, now); err != nil {
		t.Fatalf("insert orphan identity: %v", err)
	}

	ids, err := repo.ListTenantIDsByEmail(context.Background(), "OWNER@example.com")
	if err != nil {
		t.Fatalf("ListTenantIDsByEmail: %v", err)
	}
	if len(ids) != 1 || ids[0] != "tenant_qianxin" {
		t.Fatalf("ids = %#v", ids)
	}
	orphans, err := repo.ListTenantIDsByEmail(context.Background(), "ghost@example.com")
	if err != nil {
		t.Fatalf("orphan lookup: %v", err)
	}
	if len(orphans) != 0 {
		t.Fatalf("orphan ids = %#v", orphans)
	}
}

func TestListTenantIDsByEmailQueryPlanUsesIndexes(t *testing.T) {
	s := newTestStore(t)
	repo := s.Users.(*userRepo)
	rows, err := repo.db.Query("EXPLAIN QUERY PLAN "+listTenantIDsByEmailSQL, "a@b.c", "a@b.c")
	if err != nil {
		t.Fatalf("explain: %v", err)
	}
	defer rows.Close()
	var plan strings.Builder
	for rows.Next() {
		cols, err := rows.Columns()
		if err != nil {
			t.Fatalf("columns: %v", err)
		}
		raw := make([]sql.RawBytes, len(cols))
		dest := make([]any, len(cols))
		for i := range raw {
			dest[i] = &raw[i]
		}
		if err := rows.Scan(dest...); err != nil {
			t.Fatalf("scan plan: %v", err)
		}
		for _, col := range raw {
			plan.Write(col)
			plan.WriteByte(' ')
		}
		plan.WriteByte('\n')
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("plan rows: %v", err)
	}
	text := plan.String()
	if !strings.Contains(text, "idx_users_email") || !strings.Contains(text, "idx_user_identities_type_value") {
		t.Fatalf("lookup missed indexes:\n%s", text)
	}
	for _, line := range strings.Split(text, "\n") {
		upper := strings.ToUpper(line)
		if strings.Contains(upper, "SCAN") && (strings.Contains(upper, "USERS") || strings.Contains(upper, "USER_IDENTITIES") || strings.Contains(upper, " UI")) {
			t.Fatalf("lookup scans a user table:\n%s", text)
		}
	}
}
