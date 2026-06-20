package sqlite

import (
	"context"
	"math"
	"path/filepath"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/llmservice"
)

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
