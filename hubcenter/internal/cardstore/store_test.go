package cardstore

import (
	"context"
	"strings"
	"testing"
	"time"

	corecardstore "github.com/RapidAI/CodeClaw/corelib/cardstore"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/llmservice"
)

type cardTypeTestRepo struct {
	created []*CardType
	updated []*CardType
	byID    map[string]*CardType
	all     []*CardType
}

func (r *cardTypeTestRepo) Create(_ context.Context, ct *CardType) error {
	r.created = append(r.created, ct)
	return nil
}

func (r *cardTypeTestRepo) Update(_ context.Context, ct *CardType) error {
	r.updated = append(r.updated, ct)
	return nil
}

func (r *cardTypeTestRepo) GetByID(_ context.Context, id string) (*CardType, error) {
	if r.byID == nil {
		return nil, nil
	}
	return r.byID[id], nil
}

func (r *cardTypeTestRepo) ListEnabled(_ context.Context) ([]*CardType, error) {
	return nil, nil
}

func (r *cardTypeTestRepo) ListAll(_ context.Context) ([]*CardType, error) {
	return r.all, nil
}

func (r *cardTypeTestRepo) Delete(_ context.Context, _ string) error {
	return nil
}

type orderTestRepo struct {
	created []*PurchaseOrder
	byNo    map[string]*PurchaseOrder
}

func (r *orderTestRepo) Create(_ context.Context, order *PurchaseOrder) error {
	r.created = append(r.created, order)
	if r.byNo == nil {
		r.byNo = map[string]*PurchaseOrder{}
	}
	r.byNo[order.OrderNo] = order
	return nil
}

func (r *orderTestRepo) GetByOrderNo(_ context.Context, orderNo string) (*PurchaseOrder, error) {
	if r.byNo == nil {
		return nil, nil
	}
	return r.byNo[orderNo], nil
}

func (r *orderTestRepo) List(_ context.Context, _ OrderFilter) ([]*PurchaseOrder, int, error) {
	return nil, 0, nil
}

func (r *orderTestRepo) UpdateStatus(_ context.Context, orderNo, status string, _ time.Time) error {
	if r.byNo != nil && r.byNo[orderNo] != nil {
		r.byNo[orderNo].Status = status
	}
	return nil
}

func (r *orderTestRepo) Update(_ context.Context, order *PurchaseOrder) error {
	if r.byNo == nil {
		r.byNo = map[string]*PurchaseOrder{}
	}
	r.byNo[order.OrderNo] = order
	return nil
}

func (r *orderTestRepo) Delete(_ context.Context, orderNo string) error {
	if r.byNo != nil {
		delete(r.byNo, orderNo)
	}
	return nil
}

func (r *orderTestRepo) Archive(_ context.Context, orderNo string, archivedAt time.Time) error {
	if r.byNo != nil && r.byNo[orderNo] != nil {
		r.byNo[orderNo].ArchivedAt = archivedAt.Format(time.RFC3339)
	}
	return nil
}

type authTestRepo struct {
	created []*llmservice.TenantAuthorization
}

func (r *authTestRepo) Create(_ context.Context, auth *llmservice.TenantAuthorization) error {
	r.created = append(r.created, auth)
	return nil
}

func (r *authTestRepo) GetByID(_ context.Context, _ string) (*llmservice.TenantAuthorization, error) {
	return nil, nil
}

func (r *authTestRepo) ListByHubTenant(_ context.Context, _, _ string) ([]*llmservice.TenantAuthorization, error) {
	return nil, nil
}

func (r *authTestRepo) ListByServiceGroup(_ context.Context, _ string) ([]*llmservice.TenantAuthorization, error) {
	return nil, nil
}

func (r *authTestRepo) ListAll(_ context.Context) ([]*llmservice.TenantAuthorization, error) {
	return nil, nil
}

func (r *authTestRepo) Update(_ context.Context, _ *llmservice.TenantAuthorization) error {
	return nil
}

func (r *authTestRepo) DeductCredits(_ context.Context, _ string, _ float64, _ time.Time) error {
	return nil
}

func TestUpdateCardTypeRequiresCompleteValidCard(t *testing.T) {
	tests := []struct {
		name string
		card CardType
		want string
	}{
		{
			name: "missing service group",
			card: CardType{ID: "ct1", Label: "Plan", Credits: 100, Period: "month", PriceRMB: 10, Template: "enterprise_monthly_blue"},
			want: "service_group_id is required",
		},
		{
			name: "missing label",
			card: CardType{ID: "ct1", ServiceGroupID: "g1", Credits: 100, Period: "month", PriceRMB: 10, Template: "enterprise_monthly_blue"},
			want: "label is required",
		},
		{
			name: "zero credits",
			card: CardType{ID: "ct1", ServiceGroupID: "g1", Label: "Plan", Period: "month", PriceRMB: 10, Template: "enterprise_monthly_blue"},
			want: "credits must be positive",
		},
		{
			name: "zero price",
			card: CardType{ID: "ct1", ServiceGroupID: "g1", Label: "Plan", Credits: 100, Period: "month", Template: "enterprise_monthly_blue"},
			want: "price must be positive",
		},
		{
			name: "bad period",
			card: CardType{ID: "ct1", ServiceGroupID: "g1", Label: "Plan", Credits: 100, Period: "week", PriceRMB: 10, Template: "enterprise_monthly_blue"},
			want: "period must be month, quarter, or year",
		},
		{
			name: "bad template",
			card: CardType{ID: "ct1", ServiceGroupID: "g1", Label: "Plan", Credits: 100, Period: "month", PriceRMB: 10, Template: "bad"},
			want: "invalid card template",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &cardTypeTestRepo{}
			svc := NewService(repo, nil, nil)
			err := svc.UpdateCardType(context.Background(), &tt.card)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want containing %q", err, tt.want)
			}
			if len(repo.updated) != 0 {
				t.Fatalf("invalid card was persisted: %#v", repo.updated)
			}
		})
	}
}

func TestUpdateCardTypeNormalizesAndPersistsValidCard(t *testing.T) {
	repo := &cardTypeTestRepo{}
	svc := NewService(repo, nil, nil)
	card := &CardType{
		ID:             " ct1 ",
		ServiceGroupID: " g1 ",
		Label:          " Plan ",
		Description:    " Detail ",
		Credits:        100,
		Period:         " month ",
		PriceRMB:       10,
		Template:       " enterprise_monthly_blue ",
	}

	if err := svc.UpdateCardType(context.Background(), card); err != nil {
		t.Fatalf("UpdateCardType: %v", err)
	}
	if len(repo.updated) != 1 {
		t.Fatalf("updated count = %d, want 1", len(repo.updated))
	}
	got := repo.updated[0]
	if got.ID != "ct1" || got.ServiceGroupID != "g1" || got.Label != "Plan" || got.Description != "Detail" || got.Period != "month" || got.Template != "enterprise_monthly_blue" {
		t.Fatalf("card not normalized: %#v", got)
	}
	if got.UpdatedAt.IsZero() {
		t.Fatal("UpdatedAt was not set")
	}
}

func TestCreateCardTypeDefaultsTemplateAndPersistsValidCard(t *testing.T) {
	repo := &cardTypeTestRepo{}
	svc := NewService(repo, nil, nil)
	card := &CardType{
		ID:             " ct1 ",
		ServiceGroupID: " g1 ",
		Label:          " Plan ",
		Credits:        100,
		Period:         " month ",
		PriceRMB:       10,
	}

	if err := svc.CreateCardType(context.Background(), card); err != nil {
		t.Fatalf("CreateCardType: %v", err)
	}
	if len(repo.created) != 1 {
		t.Fatalf("created count = %d, want 1", len(repo.created))
	}
	got := repo.created[0]
	if got.Template != "enterprise_monthly_blue" {
		t.Fatalf("default template = %q, want enterprise_monthly_blue", got.Template)
	}
	if got.ID != "ct1" || got.ServiceGroupID != "g1" || got.Label != "Plan" || got.Period != "month" {
		t.Fatalf("card not normalized: %#v", got)
	}
	if got.CreatedAt.IsZero() || got.UpdatedAt.IsZero() {
		t.Fatalf("timestamps not set: created=%v updated=%v", got.CreatedAt, got.UpdatedAt)
	}
}

func TestCreateCardTypeRejectsInvalidCard(t *testing.T) {
	repo := &cardTypeTestRepo{}
	svc := NewService(repo, nil, nil)
	card := &CardType{ID: "ct1", ServiceGroupID: "g1", Label: "Plan", Credits: 100, Period: "month", PriceRMB: 10, Template: "bad"}

	err := svc.CreateCardType(context.Background(), card)
	if err == nil || !strings.Contains(err.Error(), "invalid card template") {
		t.Fatalf("error = %v, want invalid card template", err)
	}
	if len(repo.created) != 0 {
		t.Fatalf("invalid card was persisted: %#v", repo.created)
	}
}

func TestEnsureDefaultComputeCardTypesCreatesBuiltinsWhenEmpty(t *testing.T) {
	repo := &cardTypeTestRepo{}
	svc := NewService(repo, nil, nil)

	if err := svc.EnsureDefaultComputeCardTypes(context.Background(), "grant-group"); err != nil {
		t.Fatalf("EnsureDefaultComputeCardTypes: %v", err)
	}
	if len(repo.created) != 3 {
		t.Fatalf("created count = %d, want 3", len(repo.created))
	}
	for _, ct := range repo.created {
		if ct.ServiceGroupID != "grant-group" || !ct.Enabled || ct.Credits <= 0 || ct.PriceRMB <= 0 {
			t.Fatalf("invalid default card type: %+v", ct)
		}
	}
}

func TestEnsureDefaultComputeCardTypesKeepsExistingCards(t *testing.T) {
	repo := &cardTypeTestRepo{all: []*CardType{{ID: "custom", ServiceGroupID: "grant-group"}}}
	svc := NewService(repo, nil, nil)

	if err := svc.EnsureDefaultComputeCardTypes(context.Background(), "grant-group"); err != nil {
		t.Fatalf("EnsureDefaultComputeCardTypes: %v", err)
	}
	if len(repo.created) != 0 {
		t.Fatalf("created defaults despite existing cards: %#v", repo.created)
	}
}

func TestCreateOrderTrimsHubTenantAndEmail(t *testing.T) {
	cardRepo := &cardTypeTestRepo{byID: map[string]*CardType{
		"ct1": {ID: "ct1", ServiceGroupID: "group-1", Label: "Plan", Credits: 100, Period: "month", PriceRMB: 10, Template: "enterprise_monthly_blue", Enabled: true},
	}}
	orderRepo := &orderTestRepo{}
	authRepo := &authTestRepo{}
	svc := NewService(cardRepo, orderRepo, authRepo)
	svc.payment = corecardstore.PersonalPaymentConfig{
		AdminEmails: []string{"owner@example.com"},
		Channels: []corecardstore.PersonalPaymentChannel{{
			ID: "wechat", Label: "WeChat", Enabled: true,
		}},
	}

	order, err := svc.CreateOrder(context.Background(), " ct1 ", " owner@example.com ", " hub-1 ", " tenant-a ", " wechat ")
	if err != nil {
		t.Fatalf("CreateOrder: %v", err)
	}
	if order.HubID != "hub-1" || order.TenantID != "tenant-a" || order.Email != "owner@example.com" {
		t.Fatalf("order fields not trimmed: %#v", order)
	}
}

func TestDeleteUnprocessedOrderRemovesOwnedPendingOrder(t *testing.T) {
	orderRepo := &orderTestRepo{byNo: map[string]*PurchaseOrder{
		"HC-PENDING": {
			Order: corecardstore.Order{OrderNo: "HC-PENDING", Email: "owner@example.com", Status: corecardstore.StatusPersonalCreated},
			HubID: "hub-1", TenantID: "tenant-a",
		},
	}}
	svc := NewService(nil, orderRepo, nil)
	if err := svc.DeleteUnprocessedOrder(context.Background(), "HC-PENDING", "owner@example.com", "hub-1", "tenant-a"); err != nil {
		t.Fatalf("DeleteUnprocessedOrder: %v", err)
	}
	if _, ok := orderRepo.byNo["HC-PENDING"]; ok {
		t.Fatalf("pending order was not deleted")
	}
}

func TestDeleteUnprocessedOrderRejectsPaidOrder(t *testing.T) {
	orderRepo := &orderTestRepo{byNo: map[string]*PurchaseOrder{
		"HC-PAID": {
			Order: corecardstore.Order{OrderNo: "HC-PAID", Email: "owner@example.com", Status: corecardstore.StatusActivated},
			HubID: "hub-1", TenantID: "tenant-a",
		},
	}}
	svc := NewService(nil, orderRepo, nil)
	if err := svc.DeleteUnprocessedOrder(context.Background(), "HC-PAID", "owner@example.com", "hub-1", "tenant-a"); err == nil {
		t.Fatalf("DeleteUnprocessedOrder succeeded for activated order")
	}
	if _, ok := orderRepo.byNo["HC-PAID"]; !ok {
		t.Fatalf("activated order was deleted")
	}
}

func TestArchiveOrderKeepsPaymentStatus(t *testing.T) {
	orderRepo := &orderTestRepo{byNo: map[string]*PurchaseOrder{
		"HC-OLD": {
			Order: corecardstore.Order{OrderNo: "HC-OLD", Email: "owner@example.com", Status: corecardstore.StatusActivated},
			HubID: "hub-1", TenantID: "tenant-a",
		},
	}}
	svc := NewService(nil, orderRepo, nil)
	if err := svc.ArchiveOrder(context.Background(), "HC-OLD"); err != nil {
		t.Fatalf("ArchiveOrder: %v", err)
	}
	got := orderRepo.byNo["HC-OLD"]
	if got.Status != corecardstore.StatusActivated {
		t.Fatalf("status = %q, want activated", got.Status)
	}
	if got.ArchivedAt == "" {
		t.Fatalf("ArchivedAt was not set")
	}
}

func TestConfirmOrderMarksExternalPermissionCardAsExternalProviderGrant(t *testing.T) {
	cardRepo := &cardTypeTestRepo{}
	orderRepo := &orderTestRepo{byNo: map[string]*PurchaseOrder{}}
	authRepo := &authTestRepo{}
	svc := NewService(cardRepo, orderRepo, authRepo)

	order := &PurchaseOrder{
		Order: corecardstore.Order{
			OrderNo: "HC202606160001",
			Email:   "owner@example.com",
			Status:  corecardstore.StatusPending,
		},
		HubID:          "hub-1",
		TenantID:       "tenant-a",
		CardTypeID:     "ct-external",
		ServiceGroupID: llmservice.ExternalComputePermissionServiceGroupID,
		Credits:        100,
		Period:         "month",
	}
	orderRepo.byNo[order.OrderNo] = order

	if err := svc.ConfirmOrder(context.Background(), order.OrderNo, "admin@example.com"); err != nil {
		t.Fatalf("ConfirmOrder: %v", err)
	}
	if len(authRepo.created) != 1 {
		t.Fatalf("created auth count = %d, want 1", len(authRepo.created))
	}
	auth := authRepo.created[0]
	if !auth.AllowExternalProviders {
		t.Fatalf("AllowExternalProviders = false, want true")
	}
	if auth.ServiceGroupID != llmservice.ExternalComputePermissionServiceGroupID {
		t.Fatalf("ServiceGroupID = %q", auth.ServiceGroupID)
	}
}
