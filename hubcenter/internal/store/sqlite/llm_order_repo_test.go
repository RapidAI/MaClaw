package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	corecardstore "github.com/RapidAI/CodeClaw/corelib/cardstore"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/cardstore"
)

func TestLLMOrderRepoArchiveFiltersDefaultList(t *testing.T) {
	provider, err := NewProvider(Config{DSN: filepath.Join(t.TempDir(), "llm-orders.db"), WAL: false})
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
	repo := NewLLMOrderRepo(provider)
	now := time.Now().UTC().Truncate(time.Second)
	orders := []*cardstore.PurchaseOrder{
		{
			Order: corecardstore.Order{OrderNo: "HC-ACTIVE", ProductID: "ct1", Email: "owner@example.com", Amount: 10, Status: corecardstore.StatusPersonalCreated, CreatedAt: now, UpdatedAt: now},
			HubID: "hub-1", TenantID: "tenant-a", CardTypeID: "ct1", ServiceGroupID: "group-1", Credits: 100, Period: "month",
		},
		{
			Order: corecardstore.Order{OrderNo: "HC-ARCHIVED", ProductID: "ct1", Email: "owner@example.com", Amount: 10, Status: corecardstore.StatusActivated, CreatedAt: now.Add(time.Second), UpdatedAt: now.Add(time.Second)},
			HubID: "hub-1", TenantID: "tenant-a", CardTypeID: "ct1", ServiceGroupID: "group-1", Credits: 100, Period: "month",
		},
	}
	for _, order := range orders {
		if err := repo.Create(ctx, order); err != nil {
			t.Fatalf("Create(%s) error = %v", order.OrderNo, err)
		}
	}
	archivedAt := now.Add(2 * time.Second)
	if err := repo.Archive(ctx, "HC-ARCHIVED", archivedAt); err != nil {
		t.Fatalf("Archive() error = %v", err)
	}

	active, total, err := repo.List(ctx, cardstore.OrderFilter{Limit: 10})
	if err != nil {
		t.Fatalf("List(active) error = %v", err)
	}
	if total != 1 || len(active) != 1 || active[0].OrderNo != "HC-ACTIVE" {
		t.Fatalf("active list = total %d orders %+v, want HC-ACTIVE only", total, active)
	}

	archived, total, err := repo.List(ctx, cardstore.OrderFilter{ArchivedOnly: true, Limit: 10})
	if err != nil {
		t.Fatalf("List(archived) error = %v", err)
	}
	if total != 1 || len(archived) != 1 || archived[0].OrderNo != "HC-ARCHIVED" || archived[0].ArchivedAt == "" {
		t.Fatalf("archived list = total %d orders %+v, want archived order with ArchivedAt", total, archived)
	}

	all, total, err := repo.List(ctx, cardstore.OrderFilter{IncludeArchived: true, Limit: 10})
	if err != nil {
		t.Fatalf("List(all) error = %v", err)
	}
	if total != 2 || len(all) != 2 {
		t.Fatalf("all list = total %d len %d, want 2", total, len(all))
	}
}
