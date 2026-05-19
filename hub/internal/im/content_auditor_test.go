package im

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestContentAuditorConfigProviderUsesTenantContext(t *testing.T) {
	var seenTenant string
	auditor := NewContentAuditor("", 1, "pass", nil, func(ctx context.Context) *ContentAuditDynamicConfig {
		seenTenant = TenantIDFromContext(ctx)
		return &ContentAuditDynamicConfig{Keywords: []string{"tenant-keyword"}}
	})

	cfg := auditor.dynamicConfig(WithTenant(context.Background(), "tenant_acme"))
	if seenTenant != "tenant_acme" {
		t.Fatalf("config provider saw tenant %q, want tenant_acme", seenTenant)
	}
	if cfg == nil || len(cfg.Keywords) != 1 || cfg.Keywords[0] != "tenant-keyword" {
		t.Fatalf("unexpected dynamic config: %#v", cfg)
	}
}

func TestSQLiteAuditLogStoreWritesTenantID(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	_, err = db.Exec(`CREATE TABLE content_audit_logs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		tenant_id TEXT NOT NULL DEFAULT 'tenant_default',
		timestamp DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		user_id TEXT NOT NULL,
		platform TEXT NOT NULL,
		content_type TEXT NOT NULL,
		summary TEXT NOT NULL,
		return_code INTEGER NOT NULL,
		duration_ms INTEGER NOT NULL,
		message TEXT NOT NULL,
		content_hash TEXT NOT NULL
	)`)
	if err != nil {
		t.Fatalf("create audit table: %v", err)
	}

	store := NewSQLiteAuditLogStore(db)
	ctx := WithTenant(context.Background(), "tenant_acme")
	if err := store.WriteLog(ctx, &AuditLogEntry{
		TenantID:    TenantIDFromContext(ctx),
		Timestamp:   time.Now(),
		UserID:      "user-1",
		Platform:    "feishu",
		ContentType: "text",
		Summary:     "hello",
		ReturnCode:  0,
		Duration:    10 * time.Millisecond,
		Message:     "ok",
		ContentHash: "hash-1",
	}); err != nil {
		t.Fatalf("write log: %v", err)
	}

	var tenantID string
	if err := db.QueryRow(`SELECT tenant_id FROM content_audit_logs WHERE user_id = 'user-1'`).Scan(&tenantID); err != nil {
		t.Fatalf("query tenant id: %v", err)
	}
	if tenantID != "tenant_acme" {
		t.Fatalf("tenant_id = %q, want tenant_acme", tenantID)
	}
}
