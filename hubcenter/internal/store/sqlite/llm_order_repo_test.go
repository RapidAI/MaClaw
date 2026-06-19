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

func TestLLMOrderRepoListFiltersMultipleStatuses(t *testing.T) {
	provider, err := NewProvider(Config{DSN: filepath.Join(t.TempDir(), "llm-orders-statuses.db"), WAL: false})
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
			Order: corecardstore.Order{OrderNo: "HC-PENDING", ProductID: "ct1", Email: "owner@example.com", Amount: 10, Status: corecardstore.StatusPending, CreatedAt: now, UpdatedAt: now},
			HubID: "hub-1", TenantID: "tenant-a", CardTypeID: "ct1", ServiceGroupID: "group-1", Credits: 100, Period: "month",
		},
		{
			Order: corecardstore.Order{OrderNo: "HC-OPENED", ProductID: "ct1", Email: "owner@example.com", Amount: 10, Status: corecardstore.StatusPersonalOpened, CreatedAt: now.Add(time.Second), UpdatedAt: now.Add(time.Second)},
			HubID: "hub-1", TenantID: "tenant-a", CardTypeID: "ct1", ServiceGroupID: "group-1", Credits: 100, Period: "month",
		},
		{
			Order: corecardstore.Order{OrderNo: "HC-ACTIVATED", ProductID: "ct1", Email: "owner@example.com", Amount: 10, Status: corecardstore.StatusActivated, CreatedAt: now.Add(2 * time.Second), UpdatedAt: now.Add(2 * time.Second)},
			HubID: "hub-1", TenantID: "tenant-a", CardTypeID: "ct1", ServiceGroupID: "group-1", Credits: 100, Period: "month",
		},
	}
	for _, order := range orders {
		if err := repo.Create(ctx, order); err != nil {
			t.Fatalf("Create(%s) error = %v", order.OrderNo, err)
		}
	}

	got, total, err := repo.List(ctx, cardstore.OrderFilter{
		Email:    "owner@example.com",
		Statuses: []string{corecardstore.StatusPending, corecardstore.StatusPersonalCreated, corecardstore.StatusPersonalOpened},
		Limit:    10,
	})
	if err != nil {
		t.Fatalf("List(statuses) error = %v", err)
	}
	if total != 2 || len(got) != 2 {
		t.Fatalf("List(statuses) total=%d len=%d, want 2", total, len(got))
	}
	if got[0].OrderNo != "HC-OPENED" || got[1].OrderNo != "HC-PENDING" {
		t.Fatalf("List(statuses) order nos = %s, %s; want HC-OPENED, HC-PENDING", got[0].OrderNo, got[1].OrderNo)
	}
}

func TestLLMOrderRepoUpdatePersistsArchivedAt(t *testing.T) {
	provider, err := NewProvider(Config{DSN: filepath.Join(t.TempDir(), "llm-orders-update-archive.db"), WAL: false})
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
	order := &cardstore.PurchaseOrder{
		Order: corecardstore.Order{
			OrderNo:   "HC-UPDATE-ARCHIVE",
			ProductID: "ct1",
			Email:     "owner@example.com",
			Amount:    10,
			Status:    corecardstore.StatusPending,
			CreatedAt: now,
			UpdatedAt: now,
		},
		HubID: "hub-1", TenantID: "tenant-a", CardTypeID: "ct1", ServiceGroupID: "group-1", Credits: 100, Period: "month",
	}
	if err := repo.Create(ctx, order); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	archivedAt := now.Add(time.Minute).Format(time.RFC3339)
	order.ArchivedAt = archivedAt
	order.UpdatedAt = now.Add(time.Minute)
	if err := repo.Update(ctx, order); err != nil {
		t.Fatalf("Update(archive) error = %v", err)
	}
	got, err := repo.GetByOrderNo(ctx, order.OrderNo)
	if err != nil {
		t.Fatalf("GetByOrderNo(archive) error = %v", err)
	}
	if got.ArchivedAt != archivedAt {
		t.Fatalf("ArchivedAt after archive = %q, want %q", got.ArchivedAt, archivedAt)
	}

	order.ArchivedAt = ""
	order.UpdatedAt = now.Add(2 * time.Minute)
	if err := repo.Update(ctx, order); err != nil {
		t.Fatalf("Update(unarchive) error = %v", err)
	}
	got, err = repo.GetByOrderNo(ctx, order.OrderNo)
	if err != nil {
		t.Fatalf("GetByOrderNo(unarchive) error = %v", err)
	}
	if got.ArchivedAt != "" {
		t.Fatalf("ArchivedAt after unarchive = %q, want empty", got.ArchivedAt)
	}
}

func TestLLMOrderRepoUpdatePersistsOrderSnapshotFields(t *testing.T) {
	provider, err := NewProvider(Config{DSN: filepath.Join(t.TempDir(), "llm-orders-update-snapshot.db"), WAL: false})
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
	order := &cardstore.PurchaseOrder{
		Order: corecardstore.Order{
			OrderNo:   "HC-UPDATE-SNAPSHOT",
			ProductID: "ct-old",
			Email:     "old@example.com",
			Amount:    10,
			Status:    corecardstore.StatusPending,
			CreatedAt: now,
			UpdatedAt: now,
		},
		HubID: "hub-old", TenantID: "tenant-old", CardTypeID: "ct-old", ServiceGroupID: "group-old", AgentID: "agent-old", AgentName: "Agent Old", Credits: 100, Period: "month",
	}
	if err := repo.Create(ctx, order); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	order.Email = "new@example.com"
	order.HubID = "hub-new"
	order.TenantID = "tenant-new"
	order.CardTypeID = "ct-new"
	order.ServiceGroupID = "group-new"
	order.AgentID = "agent-new"
	order.AgentName = "Agent New"
	order.Credits = 200
	order.Period = "year"
	order.Amount = 25000
	order.CreatedAt = now.Add(-time.Hour)
	order.UpdatedAt = now.Add(time.Hour)
	if err := repo.Update(ctx, order); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	got, err := repo.GetByOrderNo(ctx, order.OrderNo)
	if err != nil {
		t.Fatalf("GetByOrderNo() error = %v", err)
	}
	if got.Email != order.Email || got.HubID != order.HubID || got.TenantID != order.TenantID || got.CardTypeID != order.CardTypeID || got.ServiceGroupID != order.ServiceGroupID {
		t.Fatalf("snapshot identity fields = email:%q hub:%q tenant:%q card:%q group:%q", got.Email, got.HubID, got.TenantID, got.CardTypeID, got.ServiceGroupID)
	}
	if got.AgentID != order.AgentID || got.AgentName != order.AgentName || got.Credits != order.Credits || got.Period != order.Period || got.Amount != order.Amount {
		t.Fatalf("snapshot value fields = agent:%q/%q credits:%v period:%q amount:%v", got.AgentID, got.AgentName, got.Credits, got.Period, got.Amount)
	}
	if !got.CreatedAt.Equal(order.CreatedAt) {
		t.Fatalf("CreatedAt = %v, want %v", got.CreatedAt, order.CreatedAt)
	}
}

func TestLLMOrderRepoListTreatsDefaultTenantAliasesAsSameScope(t *testing.T) {
	provider, err := NewProvider(Config{DSN: filepath.Join(t.TempDir(), "llm-orders-default-tenant.db"), WAL: false})
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
			Order: corecardstore.Order{OrderNo: "HC-TENANT-DEFAULT", ProductID: "ct1", Email: "owner@example.com", Amount: 10, Status: corecardstore.StatusPersonalCreated, CreatedAt: now, UpdatedAt: now},
			HubID: "hub-1", TenantID: "tenant_default", CardTypeID: "ct1", ServiceGroupID: "group-1", Credits: 100, Period: "month",
		},
		{
			Order: corecardstore.Order{OrderNo: "HC-DEFAULT", ProductID: "ct1", Email: "owner@example.com", Amount: 10, Status: corecardstore.StatusPersonalCreated, CreatedAt: now.Add(time.Second), UpdatedAt: now.Add(time.Second)},
			HubID: "hub-1", TenantID: "default", CardTypeID: "ct1", ServiceGroupID: "group-1", Credits: 100, Period: "month",
		},
		{
			Order: corecardstore.Order{OrderNo: "HC-OTHER", ProductID: "ct1", Email: "owner@example.com", Amount: 10, Status: corecardstore.StatusPersonalCreated, CreatedAt: now.Add(2 * time.Second), UpdatedAt: now.Add(2 * time.Second)},
			HubID: "hub-1", TenantID: "tenant-b", CardTypeID: "ct1", ServiceGroupID: "group-1", Credits: 100, Period: "month",
		},
	}
	for _, order := range orders {
		if err := repo.Create(ctx, order); err != nil {
			t.Fatalf("Create(%s) error = %v", order.OrderNo, err)
		}
	}

	got, total, err := repo.List(ctx, cardstore.OrderFilter{HubID: "hub-1", TenantID: "tenant_default", Email: "owner@example.com", Limit: 10})
	if err != nil {
		t.Fatalf("List(tenant_default) error = %v", err)
	}
	if total != 2 || len(got) != 2 {
		t.Fatalf("List(tenant_default) total=%d len=%d, want 2", total, len(got))
	}
	if got[0].OrderNo != "HC-DEFAULT" || got[1].OrderNo != "HC-TENANT-DEFAULT" {
		t.Fatalf("order nos = %s, %s; want HC-DEFAULT, HC-TENANT-DEFAULT", got[0].OrderNo, got[1].OrderNo)
	}
}

func TestLLMOrderRepoPersistsPaymentDetails(t *testing.T) {
	provider, err := NewProvider(Config{DSN: filepath.Join(t.TempDir(), "llm-orders-payment.db"), WAL: false})
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
	order := &cardstore.PurchaseOrder{
		Order: corecardstore.Order{
			OrderNo:        "HC-PAY",
			ProductID:      "ct1",
			Email:          "owner@example.com",
			Amount:         10,
			Status:         corecardstore.StatusPersonalCreated,
			PayQRURL:       "https://pay.example/qr.png",
			PayDeepLink:    "alipays://pay",
			PayInstruction: "transfer with order number HC-PAY",
			PayURL:         "https://pay.example/checkout",
			CreatedAt:      now,
			UpdatedAt:      now,
		},
		HubID: "hub-1", TenantID: "tenant-a", CardTypeID: "ct1", ServiceGroupID: "group-1", Credits: 100, Period: "month",
	}
	if err := repo.Create(ctx, order); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	got, err := repo.GetByOrderNo(ctx, "HC-PAY")
	if err != nil {
		t.Fatalf("GetByOrderNo() error = %v", err)
	}
	if got.PayQRURL != order.PayQRURL || got.PayDeepLink != order.PayDeepLink || got.PayInstruction != order.PayInstruction || got.PayURL != order.PayURL {
		t.Fatalf("payment details = qr %q deep %q instruction %q url %q", got.PayQRURL, got.PayDeepLink, got.PayInstruction, got.PayURL)
	}
}
