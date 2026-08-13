package sqlite

import (
	"context"
	"database/sql"
	"math"
	"path/filepath"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/llmservice"
)

func TestLLMAuthRepoAllowsOnlyOneAuthorizationPerCardOrder(t *testing.T) {
	provider, err := NewProvider(Config{DSN: filepath.Join(t.TempDir(), "llm-auth-card-order-unique.db"), WAL: false})
	if err != nil {
		t.Fatalf("NewProvider() error = %v", err)
	}
	t.Cleanup(func() { _ = provider.Close() })
	if err := RunMigrations(provider.Write); err != nil {
		t.Fatalf("RunMigrations() error = %v", err)
	}
	if err := EnsureLLMTables(provider.Write); err != nil {
		t.Fatalf("EnsureLLMTables() error = %v", err)
	}

	ctx := context.Background()
	repo := NewLLMAuthRepo(provider)
	now := time.Now().UTC().Truncate(time.Second)
	first := &llmservice.TenantAuthorization{ID: "auth-order-1", HubID: "hub-1", TenantID: "tenant-1", AdminEmail: "owner@example.com", ServiceGroupID: "group-1", CreditsTotal: 100, StartsAt: now, ExpiresAt: now.Add(time.Hour), Status: "active", Source: "card", CardOrderID: "HC-UNIQUE", CreatedAt: now, UpdatedAt: now}
	second := *first
	second.ID = "auth-order-2"
	if err := repo.Create(ctx, first); err != nil {
		t.Fatalf("Create(first) error = %v", err)
	}
	if err := repo.Create(ctx, &second); err == nil {
		t.Fatal("Create(second) succeeded, want unique card order error")
	}
	got, err := repo.GetByCardOrderID(ctx, first.CardOrderID)
	if err != nil {
		t.Fatalf("GetByCardOrderID() error = %v", err)
	}
	if got == nil || got.ID != first.ID {
		t.Fatalf("GetByCardOrderID() = %#v, want %s", got, first.ID)
	}
}

func TestEnsureLLMTablesDeduplicatesLegacyCardOrderAuthorizations(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "llm-auth-card-order-legacy.db"))
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`CREATE TABLE llm_tenant_authorizations (
		id TEXT PRIMARY KEY, hub_id TEXT NOT NULL, tenant_id TEXT NOT NULL, admin_email TEXT NOT NULL,
		service_group_id TEXT NOT NULL, credits_total REAL NOT NULL, credits_used REAL NOT NULL,
		starts_at TEXT NOT NULL, expires_at TEXT NOT NULL, allow_external_providers INTEGER NOT NULL,
		source TEXT NOT NULL, card_order_id TEXT NOT NULL, bound_node_id TEXT NOT NULL, bound_at TEXT NOT NULL,
		status TEXT NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL
	)`); err != nil {
		t.Fatalf("create legacy auth table: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE llm_usage_records (
		id INTEGER PRIMARY KEY AUTOINCREMENT, hub_id TEXT NOT NULL, tenant_id TEXT NOT NULL,
		model TEXT NOT NULL, provider_id TEXT NOT NULL, input_tokens INTEGER NOT NULL DEFAULT 0,
		output_tokens INTEGER NOT NULL DEFAULT 0, credits_deducted REAL NOT NULL DEFAULT 0,
		cache_hit INTEGER NOT NULL DEFAULT 0, auth_id TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL
	)`); err != nil {
		t.Fatalf("create legacy usage table: %v", err)
	}
	for _, id := range []string{"legacy-auth-1", "legacy-auth-2"} {
		if _, err := db.Exec(`INSERT INTO llm_tenant_authorizations (id, hub_id, tenant_id, admin_email, service_group_id, credits_total, credits_used, starts_at, expires_at, allow_external_providers, source, card_order_id, bound_node_id, bound_at, status, created_at, updated_at) VALUES (?, 'hub-1', 'tenant-1', 'owner@example.com', 'group-1', 100, 0, '2026-01-01T00:00:00Z', '2026-02-01T00:00:00Z', 0, 'card', 'HC-LEGACY-DUP', '', '', 'active', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`, id); err != nil {
			t.Fatalf("insert %s: %v", id, err)
		}
	}
	if _, err := db.Exec(`INSERT INTO llm_usage_records (hub_id, tenant_id, model, provider_id, credits_deducted, cache_hit, auth_id, created_at) VALUES ('hub-1', 'tenant-1', 'model-a', 'provider-a', 1, 0, 'legacy-auth-2', '2026-01-02T00:00:00Z')`); err != nil {
		t.Fatalf("insert legacy usage: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE llm_card_orders (order_no TEXT PRIMARY KEY, payment_id TEXT NOT NULL)`); err != nil {
		t.Fatalf("create legacy orders: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO llm_card_orders (order_no, payment_id) VALUES ('HC-LEGACY-DUP', 'legacy-auth-2')`); err != nil {
		t.Fatalf("insert legacy order: %v", err)
	}
	if err := EnsureLLMTables(db); err != nil {
		t.Fatalf("EnsureLLMTables() error = %v", err)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM llm_tenant_authorizations WHERE card_order_id = 'HC-LEGACY-DUP'`).Scan(&count); err != nil {
		t.Fatalf("count canonical auth: %v", err)
	}
	if count != 1 {
		t.Fatalf("canonical authorizations = %d, want 1", count)
	}
	var id string
	if err := db.QueryRow(`SELECT id FROM llm_tenant_authorizations WHERE card_order_id = 'HC-LEGACY-DUP'`).Scan(&id); err != nil {
		t.Fatalf("read canonical auth: %v", err)
	}
	if id != "legacy-auth-1" {
		t.Fatalf("canonical auth = %q, want earliest legacy-auth-1", id)
	}
	var usageAuthID, orderAuthID string
	if err := db.QueryRow(`SELECT auth_id FROM llm_usage_records LIMIT 1`).Scan(&usageAuthID); err != nil {
		t.Fatalf("read migrated usage: %v", err)
	}
	if err := db.QueryRow(`SELECT payment_id FROM llm_card_orders WHERE order_no = 'HC-LEGACY-DUP'`).Scan(&orderAuthID); err != nil {
		t.Fatalf("read migrated order: %v", err)
	}
	if usageAuthID != "legacy-auth-1" || orderAuthID != "legacy-auth-1" {
		t.Fatalf("repaired references usage=%q order=%q, want canonical legacy-auth-1", usageAuthID, orderAuthID)
	}
}

func TestLLMAuthRepoUpdateRefreshesValidityFields(t *testing.T) {
	provider, err := NewProvider(Config{DSN: filepath.Join(t.TempDir(), "llm-auth.db"), WAL: false})
	if err != nil {
		t.Fatalf("NewProvider() error = %v", err)
	}
	t.Cleanup(func() { _ = provider.Close() })
	if err := RunMigrations(provider.Write); err != nil {
		t.Fatalf("RunMigrations() error = %v", err)
	}
	if err := EnsureLLMTables(provider.Write); err != nil {
		t.Fatalf("EnsureLLMTables() error = %v", err)
	}

	ctx := context.Background()
	repo := NewLLMAuthRepo(provider)
	createdAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	auth := &llmservice.TenantAuthorization{
		ID:             "auth_external",
		HubID:          "hub1",
		TenantID:       "tenant1",
		AdminEmail:     "admin@example.com",
		ServiceGroupID: llmservice.ExternalComputePermissionServiceGroupID,
		CreditsTotal:   1,
		CreditsUsed:    1,
		StartsAt:       createdAt,
		ExpiresAt:      createdAt.Add(time.Hour),
		Status:         "expired",
		Source:         "external_provider_permission",
		CreatedAt:      createdAt,
		UpdatedAt:      createdAt,
	}
	if err := repo.Create(ctx, auth); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	refreshedStart := createdAt.Add(24 * time.Hour)
	refreshedExpiry := refreshedStart.AddDate(10, 0, 0)
	auth.CreditsTotal = 1000000000000
	auth.CreditsUsed = 0
	auth.StartsAt = refreshedStart
	auth.ExpiresAt = refreshedExpiry
	auth.Status = "active"
	auth.AllowExternalProviders = true
	auth.Source = "external_provider_permission"
	auth.UpdatedAt = refreshedStart.Add(30 * time.Minute)
	if err := repo.Update(ctx, auth); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	got, err := repo.GetByID(ctx, auth.ID)
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}
	if got == nil {
		t.Fatal("GetByID() = nil")
	}
	if !got.AllowExternalProviders || got.Status != "active" || got.CreditsRemaining() <= 0 {
		t.Fatalf("updated auth = %#v, want active external compute grant", got)
	}
	if !got.StartsAt.Equal(refreshedStart) || !got.ExpiresAt.Equal(refreshedExpiry) {
		t.Fatalf("validity = %s..%s, want %s..%s", got.StartsAt, got.ExpiresAt, refreshedStart, refreshedExpiry)
	}
	if !got.UpdatedAt.Equal(auth.UpdatedAt) {
		t.Fatalf("updated_at = %s, want preserved %s", got.UpdatedAt, auth.UpdatedAt)
	}
}

func TestLLMAuthRepoDeductCreditsPersistsFractionalUsage(t *testing.T) {
	provider, err := NewProvider(Config{DSN: filepath.Join(t.TempDir(), "llm-auth-fractional.db"), WAL: false})
	if err != nil {
		t.Fatalf("NewProvider() error = %v", err)
	}
	t.Cleanup(func() { _ = provider.Close() })
	if err := RunMigrations(provider.Write); err != nil {
		t.Fatalf("RunMigrations() error = %v", err)
	}
	if err := EnsureLLMTables(provider.Write); err != nil {
		t.Fatalf("EnsureLLMTables() error = %v", err)
	}

	ctx := context.Background()
	repo := NewLLMAuthRepo(provider)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	auth := &llmservice.TenantAuthorization{
		ID:             "auth_fractional",
		HubID:          "hub1",
		TenantID:       "tenant1",
		AdminEmail:     "admin@example.com",
		ServiceGroupID: "maclaw-official",
		CreditsTotal:   10,
		CreditsUsed:    1,
		StartsAt:       now.Add(-time.Hour),
		ExpiresAt:      now.Add(time.Hour),
		Status:         "active",
		Source:         "card",
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := repo.Create(ctx, auth); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	actual, err := repo.DeductCredits(ctx, auth.ID, 0.1, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("DeductCredits() error = %v", err)
	}
	if math.Abs(actual-0.1) > 1e-9 {
		t.Fatalf("DeductCredits() actual = %.17g, want 0.1", actual)
	}

	got, err := repo.GetByID(ctx, auth.ID)
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}
	if got.CreditsUsed != 1.1 {
		t.Fatalf("CreditsUsed = %.17g, want 1.1", got.CreditsUsed)
	}
	if got.CreditsRemaining() != 8.9 {
		t.Fatalf("CreditsRemaining = %.17g, want 8.9", got.CreditsRemaining())
	}
}

func TestLLMAuthRepoDeductCreditsCapsInsufficientBalance(t *testing.T) {
	provider, err := NewProvider(Config{DSN: filepath.Join(t.TempDir(), "llm-auth-insufficient.db"), WAL: false})
	if err != nil {
		t.Fatalf("NewProvider() error = %v", err)
	}
	t.Cleanup(func() { _ = provider.Close() })
	if err := RunMigrations(provider.Write); err != nil {
		t.Fatalf("RunMigrations() error = %v", err)
	}
	if err := EnsureLLMTables(provider.Write); err != nil {
		t.Fatalf("EnsureLLMTables() error = %v", err)
	}

	ctx := context.Background()
	repo := NewLLMAuthRepo(provider)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	auth := &llmservice.TenantAuthorization{
		ID:             "auth_insufficient",
		HubID:          "hub1",
		TenantID:       "tenant1",
		AdminEmail:     "admin@example.com",
		ServiceGroupID: "maclaw-official",
		CreditsTotal:   10,
		CreditsUsed:    9.95,
		StartsAt:       now.Add(-time.Hour),
		ExpiresAt:      now.Add(time.Hour),
		Status:         "active",
		Source:         "card",
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := repo.Create(ctx, auth); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	actual, err := repo.DeductCredits(ctx, auth.ID, 0.1, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("DeductCredits() error = %v", err)
	}
	if math.Abs(actual-0.05) > 1e-9 {
		t.Fatalf("DeductCredits() actual = %.17g, want remaining balance 0.05", actual)
	}

	got, err := repo.GetByID(ctx, auth.ID)
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}
	if got.CreditsUsed != 10 {
		t.Fatalf("CreditsUsed = %.17g, want capped total 10", got.CreditsUsed)
	}
	if got.CreditsRemaining() != 0 {
		t.Fatalf("CreditsRemaining = %.17g, want 0", got.CreditsRemaining())
	}
	if got.Status != "exhausted" {
		t.Fatalf("Status = %q, want exhausted", got.Status)
	}
}

func TestLLMAuthRepoDeductCreditsMarksExactBalanceExhausted(t *testing.T) {
	provider, err := NewProvider(Config{DSN: filepath.Join(t.TempDir(), "llm-auth-exact-exhaust.db"), WAL: false})
	if err != nil {
		t.Fatalf("NewProvider() error = %v", err)
	}
	t.Cleanup(func() { _ = provider.Close() })
	if err := RunMigrations(provider.Write); err != nil {
		t.Fatalf("RunMigrations() error = %v", err)
	}
	if err := EnsureLLMTables(provider.Write); err != nil {
		t.Fatalf("EnsureLLMTables() error = %v", err)
	}

	ctx := context.Background()
	repo := NewLLMAuthRepo(provider)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	auth := &llmservice.TenantAuthorization{
		ID:             "auth_exact_exhaust",
		HubID:          "hub1",
		TenantID:       "tenant1",
		AdminEmail:     "admin@example.com",
		ServiceGroupID: "maclaw-official",
		CreditsTotal:   10,
		CreditsUsed:    9.9,
		StartsAt:       now.Add(-time.Hour),
		ExpiresAt:      now.Add(time.Hour),
		Status:         "active",
		Source:         "card",
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := repo.Create(ctx, auth); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	actual, err := repo.DeductCredits(ctx, auth.ID, 0.1, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("DeductCredits() error = %v", err)
	}
	if math.Abs(actual-0.1) > 1e-9 {
		t.Fatalf("DeductCredits() actual = %.17g, want 0.1", actual)
	}

	got, err := repo.GetByID(ctx, auth.ID)
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}
	if got.CreditsUsed != 10 {
		t.Fatalf("CreditsUsed = %.17g, want 10", got.CreditsUsed)
	}
	if got.CreditsRemaining() != 0 {
		t.Fatalf("CreditsRemaining = %.17g, want 0", got.CreditsRemaining())
	}
	if got.Status != "exhausted" {
		t.Fatalf("Status = %q, want exhausted", got.Status)
	}
}

func TestLLMAuthRepoListByHub(t *testing.T) {
	provider, err := NewProvider(Config{DSN: filepath.Join(t.TempDir(), "llm-auth-by-hub.db"), WAL: false})
	if err != nil {
		t.Fatalf("NewProvider() error = %v", err)
	}
	t.Cleanup(func() { _ = provider.Close() })
	if err := RunMigrations(provider.Write); err != nil {
		t.Fatalf("RunMigrations() error = %v", err)
	}
	if err := EnsureLLMTables(provider.Write); err != nil {
		t.Fatalf("EnsureLLMTables() error = %v", err)
	}

	ctx := context.Background()
	repo := NewLLMAuthRepo(provider)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, auth := range []*llmservice.TenantAuthorization{{
		ID: "auth_hub1_a", HubID: "hub1", TenantID: "tenant_a", ServiceGroupID: "group", CreditsTotal: 1, StartsAt: now, ExpiresAt: now.Add(time.Hour), Status: "active", CreatedAt: now, UpdatedAt: now,
	}, {
		ID: "auth_hub1_b", HubID: "hub1", TenantID: "tenant_b", ServiceGroupID: "group", CreditsTotal: 1, StartsAt: now, ExpiresAt: now.Add(time.Hour), Status: "active", CreatedAt: now.Add(time.Minute), UpdatedAt: now.Add(time.Minute),
	}, {
		ID: "auth_hub2", HubID: "hub2", TenantID: "tenant_a", ServiceGroupID: "group", CreditsTotal: 1, StartsAt: now, ExpiresAt: now.Add(time.Hour), Status: "active", CreatedAt: now, UpdatedAt: now,
	}} {
		if err := repo.Create(ctx, auth); err != nil {
			t.Fatalf("Create(%s) error = %v", auth.ID, err)
		}
	}

	got, err := repo.ListByHub(ctx, "hub1")
	if err != nil {
		t.Fatalf("ListByHub() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("ListByHub len = %d, want 2: %#v", len(got), got)
	}
	for _, auth := range got {
		if auth.HubID != "hub1" {
			t.Fatalf("ListByHub returned other hub auth: %#v", auth)
		}
	}
}
