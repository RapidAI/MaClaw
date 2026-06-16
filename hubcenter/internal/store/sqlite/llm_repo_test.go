package sqlite

import (
	"context"
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
}
