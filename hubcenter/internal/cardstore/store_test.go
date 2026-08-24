package cardstore

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	corecardstore "github.com/RapidAI/CodeClaw/corelib/cardstore"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/llmservice"
)

type cardTypeTestRepo struct {
	created   []*CardType
	updated   []*CardType
	byID      map[string]*CardType
	all       []*CardType
	createErr error
}

func (r *cardTypeTestRepo) Create(_ context.Context, ct *CardType) error {
	if r.createErr != nil {
		return r.createErr
	}
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
	var enabled []*CardType
	for _, ct := range r.all {
		if ct != nil && ct.Enabled {
			enabled = append(enabled, ct)
		}
	}
	return enabled, nil
}

func (r *cardTypeTestRepo) ListAll(_ context.Context) ([]*CardType, error) {
	return r.all, nil
}

func (r *cardTypeTestRepo) Delete(_ context.Context, _ string) error {
	return nil
}

type orderTestRepo struct {
	created    []*PurchaseOrder
	byNo       map[string]*PurchaseOrder
	updateErr  error
	lastFilter OrderFilter
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

func (r *orderTestRepo) List(_ context.Context, filter OrderFilter) ([]*PurchaseOrder, int, error) {
	r.lastFilter = filter
	var orders []*PurchaseOrder
	for _, order := range r.byNo {
		orders = append(orders, order)
	}
	return orders, len(orders), nil
}

func (r *orderTestRepo) UpdateStatus(_ context.Context, orderNo, status string, _ time.Time) error {
	if r.byNo != nil && r.byNo[orderNo] != nil {
		r.byNo[orderNo].Status = status
	}
	return nil
}

func (r *orderTestRepo) Update(_ context.Context, order *PurchaseOrder) error {
	if r.updateErr != nil {
		return r.updateErr
	}
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

func (r *orderTestRepo) Unarchive(_ context.Context, orderNo string, _ time.Time) error {
	if r.byNo != nil && r.byNo[orderNo] != nil {
		r.byNo[orderNo].ArchivedAt = ""
	}
	return nil
}

type authTestRepo struct {
	created              []*llmservice.TenantAuthorization
	byID                 map[string]*llmservice.TenantAuthorization
	byHub                map[string][]*llmservice.TenantAuthorization
	getByIDCalls         int
	listByHubTenantCalls int
	createErr            error
}

func (r *authTestRepo) Create(_ context.Context, auth *llmservice.TenantAuthorization) error {
	if r.createErr != nil {
		return r.createErr
	}
	r.created = append(r.created, auth)
	if r.byID == nil {
		r.byID = map[string]*llmservice.TenantAuthorization{}
	}
	r.byID[auth.ID] = auth
	if r.byHub == nil {
		r.byHub = map[string][]*llmservice.TenantAuthorization{}
	}
	key := auth.HubID + "\x00" + auth.TenantID
	r.byHub[key] = append(r.byHub[key], auth)
	return nil
}

func (r *authTestRepo) GetByID(_ context.Context, id string) (*llmservice.TenantAuthorization, error) {
	r.getByIDCalls++
	if r.byID == nil {
		return nil, nil
	}
	return r.byID[id], nil
}

func (r *authTestRepo) GetByCardOrderID(_ context.Context, orderNo string) (*llmservice.TenantAuthorization, error) {
	for _, auth := range r.byID {
		if auth != nil && auth.CardOrderID == orderNo {
			return auth, nil
		}
	}
	return nil, nil
}

func (r *authTestRepo) ListByHubTenant(_ context.Context, hubID, tenantID string) ([]*llmservice.TenantAuthorization, error) {
	r.listByHubTenantCalls++
	return r.byHub[hubID+"\x00"+tenantID], nil
}

func (r *authTestRepo) ListByServiceGroup(_ context.Context, _ string) ([]*llmservice.TenantAuthorization, error) {
	return nil, nil
}

func (r *authTestRepo) ListAll(_ context.Context) ([]*llmservice.TenantAuthorization, error) {
	items := make([]*llmservice.TenantAuthorization, 0, len(r.byID))
	for _, auth := range r.byID {
		items = append(items, auth)
	}
	return items, nil
}

func (r *authTestRepo) Update(_ context.Context, auth *llmservice.TenantAuthorization) error {
	if auth == nil {
		return nil
	}
	if r.byID == nil {
		r.byID = map[string]*llmservice.TenantAuthorization{}
	}
	copied := *auth
	r.byID[auth.ID] = &copied
	return nil
}

func (r *authTestRepo) DeductCredits(_ context.Context, _ string, credits float64, _ time.Time) (float64, error) {
	return credits, nil
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

func TestEnsureDefaultComputeCardTypesKeepsExistingEnabledCards(t *testing.T) {
	repo := &cardTypeTestRepo{all: []*CardType{{ID: "custom", ServiceGroupID: "grant-group", Enabled: true}}}
	svc := NewService(repo, nil, nil)

	if err := svc.EnsureDefaultComputeCardTypes(context.Background(), "grant-group"); err != nil {
		t.Fatalf("EnsureDefaultComputeCardTypes: %v", err)
	}
	if len(repo.created) != 0 {
		t.Fatalf("created defaults despite existing cards: %#v", repo.created)
	}
}

func TestEnsureDefaultComputeCardTypesCreatesBuiltinsWhenOnlyDisabledCardsExist(t *testing.T) {
	repo := &cardTypeTestRepo{all: []*CardType{{ID: "disabled-custom", ServiceGroupID: "grant-group", Enabled: false}}}
	svc := NewService(repo, nil, nil)

	if err := svc.EnsureDefaultComputeCardTypes(context.Background(), "grant-group"); err != nil {
		t.Fatalf("EnsureDefaultComputeCardTypes: %v", err)
	}
	if len(repo.created) != 3 {
		t.Fatalf("created count = %d, want 3", len(repo.created))
	}
	for _, ct := range repo.created {
		if !ct.Enabled {
			t.Fatalf("default card should be enabled: %#v", ct)
		}
	}
}

func TestEnsureDefaultComputeCardTypesDoesNotSwallowPartialCreateFailure(t *testing.T) {
	repo := &cardTypeTestRepo{
		createErr: errors.New("database locked"),
		byID: map[string]*CardType{
			"maclaw_compute_month_10000": {ID: "maclaw_compute_month_10000", ServiceGroupID: "grant-group", Enabled: true},
		},
	}
	svc := NewService(repo, nil, nil)

	err := svc.EnsureDefaultComputeCardTypes(context.Background(), "grant-group")
	if err == nil || !strings.Contains(err.Error(), "database locked") {
		t.Fatalf("error = %v, want database locked", err)
	}
}

func TestEnsureDefaultComputeCardTypesTreatsConcurrentCompleteInsertAsSuccess(t *testing.T) {
	defaults := DefaultComputeCardTypes("grant-group")
	byID := map[string]*CardType{}
	for _, ct := range defaults {
		byID[ct.ID] = ct
	}
	repo := &cardTypeTestRepo{
		createErr: errors.New("constraint failed"),
		byID:      byID,
	}
	svc := NewService(repo, nil, nil)

	if err := svc.EnsureDefaultComputeCardTypes(context.Background(), "grant-group"); err != nil {
		t.Fatalf("EnsureDefaultComputeCardTypes: %v", err)
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

func TestCreateOrderUsesConfiguredManualChannelWhenPayChannelEmpty(t *testing.T) {
	cardRepo := &cardTypeTestRepo{byID: map[string]*CardType{
		"ct1": {ID: "ct1", ServiceGroupID: "group-1", Label: "Plan", Credits: 100, Period: "month", PriceRMB: 10, Template: "enterprise_monthly_blue", Enabled: true},
	}}
	orderRepo := &orderTestRepo{}
	svc := NewService(cardRepo, orderRepo, &authTestRepo{})
	svc.SetPaymentConfig(corecardstore.PersonalPaymentConfig{
		Channels: []corecardstore.PersonalPaymentChannel{{
			ID: "wechat", Label: "WeChat", Enabled: true, ImageURL: "https://pay.example/qr.png",
		}},
	}, corecardstore.AlipayDirectConfig{})

	order, err := svc.CreateOrder(context.Background(), "ct1", "owner@example.com", "hub-1", "tenant-a", "")
	if err != nil {
		t.Fatalf("CreateOrder: %v", err)
	}
	if order.PaymentMode != corecardstore.PaymentModeSemiManual || order.PayChannel != "wechat" || order.PayQRURL == "" {
		t.Fatalf("payment fields = mode %q channel %q qr %q", order.PaymentMode, order.PayChannel, order.PayQRURL)
	}
}

func TestCreateOrderUsesPublicBaseURLProviderForAlipayCallbacks(t *testing.T) {
	cardRepo := &cardTypeTestRepo{byID: map[string]*CardType{
		"ct1": {ID: "ct1", ServiceGroupID: "group-1", Label: "Plan", Credits: 100, Period: "month", PriceRMB: 10, Template: "enterprise_monthly_blue", Enabled: true},
	}}
	orderRepo := &orderTestRepo{}
	svc := NewService(cardRepo, orderRepo, &authTestRepo{})
	svc.SetPaymentConfig(corecardstore.PersonalPaymentConfig{}, corecardstore.AlipayDirectConfig{
		AppID:      "app-1",
		PrivateKey: testRSAPrivateKeyPEM(t),
	})
	svc.SetPublicBaseURLProvider(func(context.Context) (string, error) {
		return "https://center.example.com/", nil
	})

	order, err := svc.CreateOrder(context.Background(), "ct1", "owner@example.com", "hub-1", "tenant-a", "")
	if err != nil {
		t.Fatalf("CreateOrder: %v", err)
	}
	payURL, err := url.Parse(order.PayURL)
	if err != nil {
		t.Fatalf("parse pay_url: %v", err)
	}
	if got := payURL.Query().Get("notify_url"); got != "https://center.example.com/api/cardstore/payment/notify" {
		t.Fatalf("notify_url = %q", got)
	}
	returnURL, err := url.Parse(payURL.Query().Get("return_url"))
	if err != nil {
		t.Fatalf("parse return_url: %v", err)
	}
	if got := returnURL.Scheme + "://" + returnURL.Host + returnURL.Path; got != "https://center.example.com/api/cardstore/payment/return" {
		t.Fatalf("return_url path = %q", got)
	}
	q := returnURL.Query()
	if q.Get("ctx_email") != "owner@example.com" || q.Get("ctx_hub_id") != "hub-1" || q.Get("ctx_tenant_id") != "tenant-a" || q.Get("ctx_order_no") != order.OrderNo {
		t.Fatalf("return_url query = %s", returnURL.RawQuery)
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

func TestListOrdersHydratesMissingSemiManualPaymentDetails(t *testing.T) {
	orderRepo := &orderTestRepo{byNo: map[string]*PurchaseOrder{
		"HC-PENDING": {
			Order: corecardstore.Order{
				OrderNo:     "HC-PENDING",
				Email:       "owner@example.com",
				Status:      corecardstore.StatusPersonalCreated,
				PaymentMode: corecardstore.PaymentModeSemiManual,
				PayChannel:  "bank_transfer",
				Amount:      88,
			},
			HubID: "hub-1", TenantID: "tenant-a",
		},
	}}
	svc := NewService(nil, orderRepo, nil)
	svc.SetPaymentConfig(corecardstore.PersonalPaymentConfig{
		Instruction: "pay manually",
		Channels: []corecardstore.PersonalPaymentChannel{{
			ID: "bank_transfer", Label: "Bank", Enabled: true, BankName: "Bank", BankAccount: "123", BankHolder: "MaClaw",
		}},
	}, corecardstore.AlipayDirectConfig{})

	orders, total, err := svc.ListOrders(context.Background(), OrderFilter{})
	if err != nil {
		t.Fatalf("ListOrders: %v", err)
	}
	if total != 1 || len(orders) != 1 {
		t.Fatalf("total=%d len=%d, want 1", total, len(orders))
	}
	if orders[0].PayInstruction == "" {
		t.Fatalf("PayInstruction was not hydrated")
	}
	if !strings.Contains(orders[0].PayInstruction, "HC-PENDING") {
		t.Fatalf("PayInstruction = %q, want order number", orders[0].PayInstruction)
	}
}

func TestListOrdersDoesNotHydrateActivatedOrderPaymentDetails(t *testing.T) {
	orderRepo := &orderTestRepo{byNo: map[string]*PurchaseOrder{
		"HC-ACTIVE": {
			Order: corecardstore.Order{
				OrderNo:     "HC-ACTIVE",
				Email:       "owner@example.com",
				Status:      corecardstore.StatusActivated,
				PaymentMode: corecardstore.PaymentModeSemiManual,
				PayChannel:  "wechat",
				Amount:      88,
			},
			HubID: "hub-1", TenantID: "tenant-a",
		},
	}}
	svc := NewService(nil, orderRepo, nil)
	svc.SetPaymentConfig(corecardstore.PersonalPaymentConfig{
		Channels: []corecardstore.PersonalPaymentChannel{{
			ID: "wechat", Label: "WeChat", ImageURL: "https://pay.example/qr.png", Enabled: true,
		}},
	}, corecardstore.AlipayDirectConfig{})

	orders, _, err := svc.ListOrders(context.Background(), OrderFilter{})
	if err != nil {
		t.Fatalf("ListOrders: %v", err)
	}
	if orders[0].PayQRURL != "" {
		t.Fatalf("PayQRURL = %q, want empty for activated order", orders[0].PayQRURL)
	}
}

func TestListOrdersHydratesAuthorizationUsage(t *testing.T) {
	now := time.Date(2026, 6, 19, 14, 0, 0, 0, time.UTC)
	authID := "auth-HC-ACTIVE"
	orderRepo := &orderTestRepo{byNo: map[string]*PurchaseOrder{
		"HC-ACTIVE": {
			Order: corecardstore.Order{
				OrderNo:   "HC-ACTIVE",
				Email:     "owner@example.com",
				Status:    corecardstore.StatusActivated,
				PaymentID: authID,
				Amount:    88,
			},
			HubID:          "hub-1",
			TenantID:       "tenant-a",
			ServiceGroupID: "redeem",
			Credits:        520000,
			Period:         "month",
		},
	}}
	authRepo := &authTestRepo{byID: map[string]*llmservice.TenantAuthorization{
		authID: {
			ID:             authID,
			HubID:          "hub-1",
			TenantID:       "tenant-a",
			ServiceGroupID: "redeem",
			CreditsTotal:   520000,
			CreditsUsed:    1.1,
			StartsAt:       now,
			ExpiresAt:      now.AddDate(0, 1, 0),
			Status:         "active",
			CardOrderID:    "HC-ACTIVE",
			CreatedAt:      now,
		},
	}}
	svc := NewService(nil, orderRepo, authRepo)

	orders, _, err := svc.ListOrders(context.Background(), OrderFilter{})
	if err != nil {
		t.Fatalf("ListOrders: %v", err)
	}
	if len(orders) != 1 {
		t.Fatalf("orders len = %d, want 1", len(orders))
	}
	got := orders[0]
	if got.AuthorizationID != authID || got.AuthorizationStatus != "active" {
		t.Fatalf("authorization summary = id:%q status:%q", got.AuthorizationID, got.AuthorizationStatus)
	}
	if got.CreditsUsed == nil || *got.CreditsUsed != 1.1 {
		t.Fatalf("CreditsUsed = %#v, want 1.1", got.CreditsUsed)
	}
	if got.CreditsRemaining == nil || *got.CreditsRemaining != 519998.9 {
		t.Fatalf("CreditsRemaining = %#v, want 519998.9", got.CreditsRemaining)
	}
	if got.AuthorizationExpiresAt == nil || !got.AuthorizationExpiresAt.Equal(now.AddDate(0, 1, 0)) {
		t.Fatalf("AuthorizationExpiresAt = %#v", got.AuthorizationExpiresAt)
	}
}

func TestListOrdersRoundsAuthorizationCreditDisplay(t *testing.T) {
	now := time.Date(2026, 6, 21, 16, 0, 0, 0, time.UTC)
	authID := "auth-HC-FLOAT"
	orderRepo := &orderTestRepo{byNo: map[string]*PurchaseOrder{
		"HC-FLOAT": {
			Order: corecardstore.Order{
				OrderNo:   "HC-FLOAT",
				Email:     "owner@example.com",
				Status:    corecardstore.StatusActivated,
				PaymentID: authID,
				Amount:    88,
			},
			HubID:          "hub-1",
			TenantID:       "tenant-a",
			ServiceGroupID: "redeem",
			Credits:        520000,
			Period:         "month",
		},
	}}
	authRepo := &authTestRepo{byID: map[string]*llmservice.TenantAuthorization{
		authID: {
			ID:             authID,
			HubID:          "hub-1",
			TenantID:       "tenant-a",
			ServiceGroupID: "redeem",
			CreditsTotal:   520000,
			CreditsUsed:    12102.734400000001,
			StartsAt:       now,
			ExpiresAt:      now.AddDate(0, 1, 0),
			Status:         "active",
			CardOrderID:    "HC-FLOAT",
			CreatedAt:      now,
		},
	}}
	svc := NewService(nil, orderRepo, authRepo)

	orders, _, err := svc.ListOrders(context.Background(), OrderFilter{})
	if err != nil {
		t.Fatalf("ListOrders: %v", err)
	}
	got := orders[0]
	if got.CreditsUsed == nil {
		t.Fatal("CreditsUsed = nil, want 12102.7344")
	}
	if *got.CreditsUsed != 12102.7344 {
		t.Fatalf("CreditsUsed = %.17g, want 12102.7344", *got.CreditsUsed)
	}
	if got.CreditsRemaining == nil {
		t.Fatal("CreditsRemaining = nil, want 507897.2656")
	}
	if *got.CreditsRemaining != 507897.2656 {
		t.Fatalf("CreditsRemaining = %.17g, want 507897.2656", *got.CreditsRemaining)
	}
}

func TestListOrdersHydratesAuthorizationUsageByOrderIDFallback(t *testing.T) {
	now := time.Date(2026, 6, 19, 14, 0, 0, 0, time.UTC)
	orderRepo := &orderTestRepo{byNo: map[string]*PurchaseOrder{
		"HC-LEGACY": {
			Order: corecardstore.Order{
				OrderNo: "HC-LEGACY",
				Email:   "owner@example.com",
				Status:  corecardstore.StatusActivated,
				Amount:  88,
			},
			HubID:          "hub-1",
			TenantID:       "tenant-a",
			ServiceGroupID: "redeem",
			Credits:        1000,
			Period:         "month",
		},
	}}
	authRepo := &authTestRepo{byHub: map[string][]*llmservice.TenantAuthorization{
		"hub-1\x00tenant-a": {{
			ID:             "auth-legacy",
			HubID:          "hub-1",
			TenantID:       "tenant-a",
			ServiceGroupID: "redeem",
			CreditsTotal:   1000,
			CreditsUsed:    250,
			StartsAt:       now,
			ExpiresAt:      now.AddDate(0, 1, 0),
			Status:         "active",
			CardOrderID:    "HC-LEGACY",
			CreatedAt:      now,
		}},
	}}
	svc := NewService(nil, orderRepo, authRepo)

	orders, _, err := svc.ListOrders(context.Background(), OrderFilter{})
	if err != nil {
		t.Fatalf("ListOrders: %v", err)
	}
	got := orders[0]
	if got.AuthorizationID != "auth-legacy" {
		t.Fatalf("AuthorizationID = %q, want auth-legacy", got.AuthorizationID)
	}
	if got.CreditsUsed == nil || *got.CreditsUsed != 250 {
		t.Fatalf("CreditsUsed = %#v, want 250", got.CreditsUsed)
	}
}

func TestListOrdersHydratesAuthorizationUsageByTenantAliasFallback(t *testing.T) {
	now := time.Date(2026, 6, 19, 14, 0, 0, 0, time.UTC)
	orderRepo := &orderTestRepo{byNo: map[string]*PurchaseOrder{
		"HC-DEFAULT-ALIAS": {
			Order: corecardstore.Order{
				OrderNo: "HC-DEFAULT-ALIAS",
				Email:   "owner@example.com",
				Status:  corecardstore.StatusActivated,
				Amount:  88,
			},
			HubID:          "hub-1",
			TenantID:       "tenant_default",
			ServiceGroupID: "redeem",
			Credits:        1000,
			Period:         "month",
		},
	}}
	authRepo := &authTestRepo{byHub: map[string][]*llmservice.TenantAuthorization{
		"hub-1\x00default": {{
			ID:             "auth-default-alias",
			HubID:          "hub-1",
			TenantID:       "default",
			ServiceGroupID: "redeem",
			CreditsTotal:   1000,
			CreditsUsed:    250,
			StartsAt:       now,
			ExpiresAt:      now.AddDate(0, 1, 0),
			Status:         "active",
			CardOrderID:    "HC-DEFAULT-ALIAS",
			CreatedAt:      now,
		}},
	}}
	svc := NewService(nil, orderRepo, authRepo)

	orders, _, err := svc.ListOrders(context.Background(), OrderFilter{})
	if err != nil {
		t.Fatalf("ListOrders: %v", err)
	}
	got := orders[0]
	if got.AuthorizationID != "auth-default-alias" {
		t.Fatalf("AuthorizationID = %q, want auth-default-alias", got.AuthorizationID)
	}
	if got.CreditsUsed == nil || *got.CreditsUsed != 250 {
		t.Fatalf("CreditsUsed = %#v, want 250", got.CreditsUsed)
	}
	if got.CreditsRemaining == nil || *got.CreditsRemaining != 750 {
		t.Fatalf("CreditsRemaining = %#v, want 750", got.CreditsRemaining)
	}
	if authRepo.listByHubTenantCalls != 3 {
		t.Fatalf("ListByHubTenant calls = %d, want 3 alias lookups", authRepo.listByHubTenantCalls)
	}
}

func TestListOrdersHydratesAuthorizationUsageForEmptyTenantAlias(t *testing.T) {
	now := time.Date(2026, 6, 19, 14, 0, 0, 0, time.UTC)
	orderRepo := &orderTestRepo{byNo: map[string]*PurchaseOrder{
		"HC-EMPTY-TENANT": {
			Order: corecardstore.Order{
				OrderNo: "HC-EMPTY-TENANT",
				Email:   "owner@example.com",
				Status:  corecardstore.StatusActivated,
				Amount:  88,
			},
			HubID:          "hub-1",
			TenantID:       "",
			ServiceGroupID: "redeem",
			Credits:        1000,
			Period:         "month",
		},
	}}
	authRepo := &authTestRepo{byHub: map[string][]*llmservice.TenantAuthorization{
		"hub-1\x00tenant_default": {{
			ID:             "auth-empty-tenant-alias",
			HubID:          "hub-1",
			TenantID:       "tenant_default",
			ServiceGroupID: "redeem",
			CreditsTotal:   1000,
			CreditsUsed:    400,
			StartsAt:       now,
			ExpiresAt:      now.AddDate(0, 1, 0),
			Status:         "active",
			CardOrderID:    "HC-EMPTY-TENANT",
			CreatedAt:      now,
		}},
	}}
	svc := NewService(nil, orderRepo, authRepo)

	orders, _, err := svc.ListOrders(context.Background(), OrderFilter{})
	if err != nil {
		t.Fatalf("ListOrders: %v", err)
	}
	got := orders[0]
	if got.AuthorizationID != "auth-empty-tenant-alias" {
		t.Fatalf("AuthorizationID = %q, want auth-empty-tenant-alias", got.AuthorizationID)
	}
	if got.CreditsUsed == nil || *got.CreditsUsed != 400 {
		t.Fatalf("CreditsUsed = %#v, want 400", got.CreditsUsed)
	}
	if got.CreditsRemaining == nil || *got.CreditsRemaining != 600 {
		t.Fatalf("CreditsRemaining = %#v, want 600", got.CreditsRemaining)
	}
}

func TestListOrdersCachesAuthorizationFallbackByHubTenant(t *testing.T) {
	now := time.Date(2026, 6, 19, 14, 0, 0, 0, time.UTC)
	orderRepo := &orderTestRepo{byNo: map[string]*PurchaseOrder{
		"HC-LEGACY-1": {
			Order:          corecardstore.Order{OrderNo: "HC-LEGACY-1", Email: "owner@example.com", Status: corecardstore.StatusActivated},
			HubID:          "hub-1",
			TenantID:       "tenant-a",
			ServiceGroupID: "redeem",
			Credits:        1000,
			Period:         "month",
		},
		"HC-LEGACY-2": {
			Order:          corecardstore.Order{OrderNo: "HC-LEGACY-2", Email: "owner@example.com", Status: corecardstore.StatusActivated},
			HubID:          "hub-1",
			TenantID:       "tenant-a",
			ServiceGroupID: "redeem",
			Credits:        2000,
			Period:         "month",
		},
	}}
	authRepo := &authTestRepo{byHub: map[string][]*llmservice.TenantAuthorization{
		"hub-1\x00tenant-a": {
			{ID: "auth-1", HubID: "hub-1", TenantID: "tenant-a", ServiceGroupID: "redeem", CreditsTotal: 1000, CreditsUsed: 100, StartsAt: now, ExpiresAt: now.AddDate(0, 1, 0), Status: "active", CardOrderID: "HC-LEGACY-1", CreatedAt: now},
			{ID: "auth-2", HubID: "hub-1", TenantID: "tenant-a", ServiceGroupID: "redeem", CreditsTotal: 2000, CreditsUsed: 200, StartsAt: now, ExpiresAt: now.AddDate(0, 1, 0), Status: "active", CardOrderID: "HC-LEGACY-2", CreatedAt: now},
		},
	}}
	svc := NewService(nil, orderRepo, authRepo)

	orders, _, err := svc.ListOrders(context.Background(), OrderFilter{})
	if err != nil {
		t.Fatalf("ListOrders: %v", err)
	}
	if len(orders) != 2 {
		t.Fatalf("orders len = %d, want 2", len(orders))
	}
	if authRepo.listByHubTenantCalls != 2 {
		t.Fatalf("ListByHubTenant calls = %d, want 2 alias lookups", authRepo.listByHubTenantCalls)
	}
}

func TestListOrdersCachedMissingAuthorizationIDStillFallsBackByOrderID(t *testing.T) {
	now := time.Date(2026, 6, 19, 14, 0, 0, 0, time.UTC)
	orderRepo := &orderTestRepo{byNo: map[string]*PurchaseOrder{
		"HC-MISSING-ID-1": {
			Order:          corecardstore.Order{OrderNo: "HC-MISSING-ID-1", Email: "owner@example.com", Status: corecardstore.StatusActivated, PaymentID: "missing-auth"},
			HubID:          "hub-1",
			TenantID:       "tenant-a",
			ServiceGroupID: "redeem",
			Credits:        1000,
			Period:         "month",
		},
		"HC-MISSING-ID-2": {
			Order:          corecardstore.Order{OrderNo: "HC-MISSING-ID-2", Email: "owner@example.com", Status: corecardstore.StatusActivated, PaymentID: "missing-auth"},
			HubID:          "hub-1",
			TenantID:       "tenant-a",
			ServiceGroupID: "redeem",
			Credits:        2000,
			Period:         "month",
		},
	}}
	authRepo := &authTestRepo{byHub: map[string][]*llmservice.TenantAuthorization{
		"hub-1\x00tenant-a": {
			{ID: "auth-1", HubID: "hub-1", TenantID: "tenant-a", ServiceGroupID: "redeem", CreditsTotal: 1000, CreditsUsed: 100, StartsAt: now, ExpiresAt: now.AddDate(0, 1, 0), Status: "active", CardOrderID: "HC-MISSING-ID-1", CreatedAt: now},
			{ID: "auth-2", HubID: "hub-1", TenantID: "tenant-a", ServiceGroupID: "redeem", CreditsTotal: 2000, CreditsUsed: 200, StartsAt: now, ExpiresAt: now.AddDate(0, 1, 0), Status: "active", CardOrderID: "HC-MISSING-ID-2", CreatedAt: now},
		},
	}}
	svc := NewService(nil, orderRepo, authRepo)

	orders, _, err := svc.ListOrders(context.Background(), OrderFilter{})
	if err != nil {
		t.Fatalf("ListOrders: %v", err)
	}
	seen := map[string]bool{}
	for _, order := range orders {
		if order.AuthorizationID == "" {
			t.Fatalf("order %s was not hydrated after cached missing auth id", order.OrderNo)
		}
		seen[order.OrderNo] = true
	}
	if len(seen) != 2 {
		t.Fatalf("hydrated orders = %+v, want both orders", seen)
	}
	if authRepo.getByIDCalls != 1 {
		t.Fatalf("GetByID calls = %d, want 1", authRepo.getByIDCalls)
	}
	if authRepo.listByHubTenantCalls != 2 {
		t.Fatalf("ListByHubTenant calls = %d, want 2 alias lookups", authRepo.listByHubTenantCalls)
	}
}

func TestListOrdersMismatchedAuthorizationIDFallsBackByOrderID(t *testing.T) {
	now := time.Date(2026, 6, 19, 14, 0, 0, 0, time.UTC)
	orderRepo := &orderTestRepo{byNo: map[string]*PurchaseOrder{
		"HC-MISMATCH": {
			Order:          corecardstore.Order{OrderNo: "HC-MISMATCH", Email: "owner@example.com", Status: corecardstore.StatusActivated, PaymentID: "auth-other-tenant"},
			HubID:          "hub-1",
			TenantID:       "tenant-a",
			ServiceGroupID: "redeem",
			Credits:        1000,
			Period:         "month",
		},
	}}
	authRepo := &authTestRepo{
		byID: map[string]*llmservice.TenantAuthorization{
			"auth-other-tenant": {ID: "auth-other-tenant", HubID: "hub-1", TenantID: "tenant-b", ServiceGroupID: "redeem", CreditsTotal: 1000, CreditsUsed: 900, StartsAt: now, ExpiresAt: now.AddDate(0, 1, 0), Status: "active", CardOrderID: "HC-OTHER", CreatedAt: now},
		},
		byHub: map[string][]*llmservice.TenantAuthorization{
			"hub-1\x00tenant-a": {
				{ID: "auth-correct", HubID: "hub-1", TenantID: "tenant-a", ServiceGroupID: "redeem", CreditsTotal: 1000, CreditsUsed: 125, StartsAt: now, ExpiresAt: now.AddDate(0, 1, 0), Status: "active", CardOrderID: "HC-MISMATCH", CreatedAt: now},
			},
		},
	}
	svc := NewService(nil, orderRepo, authRepo)

	orders, _, err := svc.ListOrders(context.Background(), OrderFilter{})
	if err != nil {
		t.Fatalf("ListOrders: %v", err)
	}
	got := orders[0]
	if got.AuthorizationID != "auth-correct" {
		t.Fatalf("AuthorizationID = %q, want auth-correct", got.AuthorizationID)
	}
	if got.CreditsUsed == nil || *got.CreditsUsed != 125 {
		t.Fatalf("CreditsUsed = %#v, want 125", got.CreditsUsed)
	}
	if authRepo.getByIDCalls != 1 {
		t.Fatalf("GetByID calls = %d, want 1", authRepo.getByIDCalls)
	}
	if authRepo.listByHubTenantCalls != 2 {
		t.Fatalf("ListByHubTenant calls = %d, want 2 alias lookups", authRepo.listByHubTenantCalls)
	}
}

func TestListOrdersDoesNotFallbackForUnactivatedOrderWithPaymentID(t *testing.T) {
	now := time.Date(2026, 6, 19, 14, 0, 0, 0, time.UTC)
	orderRepo := &orderTestRepo{byNo: map[string]*PurchaseOrder{
		"HC-PENDING": {
			Order:          corecardstore.Order{OrderNo: "HC-PENDING", Email: "owner@example.com", Status: corecardstore.StatusPending, PaymentID: "gateway-payment-id"},
			HubID:          "hub-1",
			TenantID:       "tenant-a",
			ServiceGroupID: "redeem",
			Credits:        1000,
			Period:         "month",
		},
	}}
	authRepo := &authTestRepo{
		byID: map[string]*llmservice.TenantAuthorization{
			"gateway-payment-id": {ID: "gateway-payment-id", HubID: "hub-1", TenantID: "tenant-a", ServiceGroupID: "redeem", CreditsTotal: 1000, CreditsUsed: 300, StartsAt: now, ExpiresAt: now.AddDate(0, 1, 0), Status: "active", CardOrderID: "HC-PENDING", CreatedAt: now},
		},
		byHub: map[string][]*llmservice.TenantAuthorization{
			"hub-1\x00tenant-a": {
				{ID: "auth-pending", HubID: "hub-1", TenantID: "tenant-a", ServiceGroupID: "redeem", CreditsTotal: 1000, CreditsUsed: 100, StartsAt: now, ExpiresAt: now.AddDate(0, 1, 0), Status: "active", CardOrderID: "HC-PENDING", CreatedAt: now},
			},
		},
	}
	svc := NewService(nil, orderRepo, authRepo)

	orders, _, err := svc.ListOrders(context.Background(), OrderFilter{})
	if err != nil {
		t.Fatalf("ListOrders: %v", err)
	}
	if orders[0].AuthorizationID != "" || orders[0].CreditsUsed != nil {
		t.Fatalf("unactivated order was hydrated: %#v", orders[0])
	}
	if authRepo.getByIDCalls != 0 {
		t.Fatalf("GetByID calls = %d, want 0", authRepo.getByIDCalls)
	}
	if authRepo.listByHubTenantCalls != 0 {
		t.Fatalf("ListByHubTenant calls = %d, want 0", authRepo.listByHubTenantCalls)
	}
}

func TestListOrdersClearsStaleAuthorizationUsage(t *testing.T) {
	staleUsed := 10.0
	orderRepo := &orderTestRepo{byNo: map[string]*PurchaseOrder{
		"HC-STALE": {
			Order: corecardstore.Order{
				OrderNo: "HC-STALE",
				Email:   "owner@example.com",
				Status:  corecardstore.StatusActivated,
			},
			HubID:          "hub-1",
			TenantID:       "tenant-a",
			ServiceGroupID: "redeem",
			Credits:        1000,
			Period:         "month",
			CreditsUsed:    &staleUsed,
		},
	}}
	svc := NewService(nil, orderRepo, nil)

	orders, _, err := svc.ListOrders(context.Background(), OrderFilter{})
	if err != nil {
		t.Fatalf("ListOrders: %v", err)
	}
	if orders[0].CreditsUsed != nil || orders[0].AuthorizationID != "" {
		t.Fatalf("stale authorization fields were not cleared: %#v", orders[0])
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

func TestRestoreArchivedOrderRequiresActivatedArchivedOrder(t *testing.T) {
	now := time.Now().UTC()
	orderRepo := &orderTestRepo{byNo: map[string]*PurchaseOrder{
		"HC-ACTIVATED": {
			Order:      corecardstore.Order{OrderNo: "HC-ACTIVATED", Email: "owner@example.com", Status: corecardstore.StatusActivated, CreatedAt: now, UpdatedAt: now},
			HubID:      "hub-1",
			TenantID:   "tenant-a",
			ArchivedAt: now.Format(time.RFC3339),
		},
		"HC-PENDING": {
			Order:      corecardstore.Order{OrderNo: "HC-PENDING", Email: "owner@example.com", Status: corecardstore.StatusPending, CreatedAt: now, UpdatedAt: now},
			HubID:      "hub-1",
			TenantID:   "tenant-a",
			ArchivedAt: now.Format(time.RFC3339),
		},
		"HC-ACTIVE": {
			Order: corecardstore.Order{OrderNo: "HC-ACTIVE", Email: "owner@example.com", Status: corecardstore.StatusActivated, CreatedAt: now, UpdatedAt: now},
			HubID: "hub-1", TenantID: "tenant-a",
		},
		"HC-UPPER": {
			Order:      corecardstore.Order{OrderNo: "HC-UPPER", Email: "owner@example.com", Status: "ACTIVATED", CreatedAt: now, UpdatedAt: now},
			HubID:      "hub-1",
			TenantID:   "tenant-a",
			ArchivedAt: now.Format(time.RFC3339),
		},
	}}
	svc := NewService(nil, orderRepo, nil)

	if err := svc.RestoreArchivedOrder(context.Background(), "HC-ACTIVE"); err != nil {
		t.Fatalf("RestoreArchivedOrder(active) error = %v, want idempotent success", err)
	}
	if err := svc.RestoreArchivedOrder(context.Background(), "HC-PENDING"); err == nil || !strings.Contains(err.Error(), "cannot be restored") {
		t.Fatalf("RestoreArchivedOrder(pending) error = %v, want cannot be restored", err)
	}
	if err := svc.RestoreArchivedOrder(context.Background(), "HC-ACTIVATED"); err != nil {
		t.Fatalf("RestoreArchivedOrder(activated): %v", err)
	}
	if got := orderRepo.byNo["HC-ACTIVATED"].ArchivedAt; got != "" {
		t.Fatalf("ArchivedAt = %q, want empty after restore", got)
	}
	if err := svc.RestoreArchivedOrder(context.Background(), "HC-UPPER"); err != nil {
		t.Fatalf("RestoreArchivedOrder(uppercase activated): %v", err)
	}
	if got := orderRepo.byNo["HC-UPPER"].ArchivedAt; got != "" {
		t.Fatalf("uppercase ArchivedAt = %q, want empty after restore", got)
	}
}

func TestDeleteArchivedUnprocessedOrderRequiresArchivedPendingOrder(t *testing.T) {
	now := time.Now().UTC()
	orderRepo := &orderTestRepo{byNo: map[string]*PurchaseOrder{
		"HC-ACTIVE": {
			Order: corecardstore.Order{OrderNo: "HC-ACTIVE", Email: "owner@example.com", Status: corecardstore.StatusPending, CreatedAt: now, UpdatedAt: now},
			HubID: "hub-1", TenantID: "tenant-a",
		},
		"HC-ACTIVATED": {
			Order:      corecardstore.Order{OrderNo: "HC-ACTIVATED", Email: "owner@example.com", Status: corecardstore.StatusActivated, CreatedAt: now, UpdatedAt: now},
			HubID:      "hub-1",
			TenantID:   "tenant-a",
			ArchivedAt: now.Format(time.RFC3339),
		},
		"HC-ARCHIVED-PENDING": {
			Order:      corecardstore.Order{OrderNo: "HC-ARCHIVED-PENDING", Email: "owner@example.com", Status: corecardstore.StatusPending, CreatedAt: now, UpdatedAt: now},
			HubID:      "hub-1",
			TenantID:   "tenant-a",
			ArchivedAt: now.Format(time.RFC3339),
		},
	}}
	svc := NewService(nil, orderRepo, nil)

	if err := svc.DeleteArchivedUnprocessedOrder(context.Background(), "HC-ACTIVE"); err == nil || !strings.Contains(err.Error(), "not archived") {
		t.Fatalf("DeleteArchivedUnprocessedOrder(active) error = %v, want not archived", err)
	}
	if err := svc.DeleteArchivedUnprocessedOrder(context.Background(), "HC-ACTIVATED"); err == nil || !strings.Contains(err.Error(), "cannot be deleted") {
		t.Fatalf("DeleteArchivedUnprocessedOrder(activated) error = %v, want cannot be deleted", err)
	}
	if err := svc.DeleteArchivedUnprocessedOrder(context.Background(), "HC-ARCHIVED-PENDING"); err != nil {
		t.Fatalf("DeleteArchivedUnprocessedOrder(archived pending) error = %v", err)
	}
	if got := orderRepo.byNo["HC-ARCHIVED-PENDING"]; got != nil {
		t.Fatalf("archived pending order still exists: %+v", got)
	}
	if orderRepo.byNo["HC-ACTIVE"] == nil || orderRepo.byNo["HC-ACTIVATED"] == nil {
		t.Fatalf("non-deletable orders were removed: %+v", orderRepo.byNo)
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

func TestConfirmOrderReusesAuthorizationAlreadyCreatedForOrder(t *testing.T) {
	now := time.Now().UTC()
	order := &PurchaseOrder{Order: corecardstore.Order{OrderNo: "HC-IDEMPOTENT", Email: "owner@example.com", Status: corecardstore.StatusPaid}, HubID: "hub-1", TenantID: "tenant-a", ServiceGroupID: "group-a", Credits: 100, Period: "month"}
	orderRepo := &orderTestRepo{byNo: map[string]*PurchaseOrder{order.OrderNo: order}}
	existing := &llmservice.TenantAuthorization{ID: "auth-existing", CardOrderID: order.OrderNo, CreatedAt: now, UpdatedAt: now}
	authRepo := &authTestRepo{byID: map[string]*llmservice.TenantAuthorization{existing.ID: existing}}
	svc := NewService(nil, orderRepo, authRepo)

	if err := svc.ConfirmOrder(context.Background(), order.OrderNo, "admin@example.com"); err != nil {
		t.Fatalf("ConfirmOrder: %v", err)
	}
	if len(authRepo.created) != 0 {
		t.Fatalf("created auth count = %d, want 0", len(authRepo.created))
	}
	if got := orderRepo.byNo[order.OrderNo].PaymentID; got != existing.ID {
		t.Fatalf("PaymentID = %q, want %q", got, existing.ID)
	}
}

func TestConfirmOrderUsesDeterministicAuthorizationID(t *testing.T) {
	order := &PurchaseOrder{Order: corecardstore.Order{OrderNo: "HC-STABLE-AUTH", Email: "owner@example.com", Status: corecardstore.StatusPending}, HubID: "hub-1", TenantID: "tenant-a", ServiceGroupID: "group-a", Credits: 100, Period: "month"}
	orderRepo := &orderTestRepo{byNo: map[string]*PurchaseOrder{order.OrderNo: order}}
	authRepo := &authTestRepo{}
	svc := NewService(nil, orderRepo, authRepo)

	if err := svc.ConfirmOrder(context.Background(), order.OrderNo, "admin@example.com"); err != nil {
		t.Fatalf("ConfirmOrder: %v", err)
	}
	if len(authRepo.created) != 1 {
		t.Fatalf("created auth count = %d, want 1", len(authRepo.created))
	}
	if got, want := authRepo.created[0].ID, "auth_"+order.OrderNo; got != want {
		t.Fatalf("authorization id = %q, want %q", got, want)
	}
	if got, want := orderRepo.byNo[order.OrderNo].PaymentID, "auth_"+order.OrderNo; got != want {
		t.Fatalf("order PaymentID = %q, want %q", got, want)
	}
}

func TestConfirmOrderReturnsErrorWhenReactivatedOrderCannotBeSaved(t *testing.T) {
	order := &PurchaseOrder{Order: corecardstore.Order{OrderNo: "HC-RETRY-SAVE", Email: "owner@example.com", Status: corecardstore.StatusPaid}, HubID: "hub-1", TenantID: "tenant-a", ServiceGroupID: "group-a", Credits: 100, Period: "month"}
	orderRepo := &orderTestRepo{
		byNo:      map[string]*PurchaseOrder{order.OrderNo: order},
		updateErr: errors.New("database unavailable"),
	}
	authRepo := &authTestRepo{}
	svc := NewService(nil, orderRepo, authRepo)

	err := svc.ConfirmOrder(context.Background(), order.OrderNo, "admin@example.com")
	if err == nil || !strings.Contains(err.Error(), "save reactivated order") {
		t.Fatalf("ConfirmOrder error = %v, want reactivated-order save failure", err)
	}
}

func TestListOrdersActiveCardsOnlyKeepsUsableSoldCards(t *testing.T) {
	now := time.Now().UTC()
	archived := usableSoldOrder("HC-ARCHIVED", "g1", now)
	archived.ArchivedAt = now.Format(time.RFC3339)
	orderRepo := &orderTestRepo{byNo: map[string]*PurchaseOrder{
		"HC-ACTIVE":    usableSoldOrder("HC-ACTIVE", "g1", now),
		"HC-ARCHIVED":  archived,
		"HC-EXPIRED":   expiredSoldOrder("HC-EXPIRED", "g1", now),
		"HC-EXHAUSTED": exhaustedSoldOrder("HC-EXHAUSTED", "g1", now),
		"HC-PENDING": {
			Order:          corecardstore.Order{OrderNo: "HC-PENDING", Status: corecardstore.StatusPending},
			HubID:          "hub-1",
			TenantID:       "tenant-a",
			ServiceGroupID: "g1",
			Credits:        100,
		},
	}}
	authRepo := &authTestRepo{byID: map[string]*llmservice.TenantAuthorization{
		"auth-HC-ACTIVE":    usableSoldAuth("HC-ACTIVE", "g1", now),
		"auth-HC-ARCHIVED":  usableSoldAuth("HC-ARCHIVED", "g1", now),
		"auth-HC-EXPIRED":   expiredSoldAuth("HC-EXPIRED", "g1", now),
		"auth-HC-EXHAUSTED": exhaustedSoldAuth("HC-EXHAUSTED", "g1", now),
	}}
	svc := NewService(nil, orderRepo, authRepo)

	all, _, err := svc.ListOrders(context.Background(), OrderFilter{})
	if err != nil {
		t.Fatalf("ListOrders: %v", err)
	}
	byNo := ordersByNo(all)
	if !byNo["HC-ACTIVE"].CanRebindServiceGroup {
		t.Fatal("usable sold card should allow rebind")
	}
	if byNo["HC-EXPIRED"].CanRebindServiceGroup || byNo["HC-EXHAUSTED"].CanRebindServiceGroup || byNo["HC-PENDING"].CanRebindServiceGroup {
		t.Fatal("expired, exhausted, or pending orders must not allow rebind")
	}

	orders, total, err := svc.ListOrders(context.Background(), OrderFilter{ActiveCardsOnly: true, ArchivedOnly: true})
	if err != nil {
		t.Fatalf("ListOrders active cards: %v", err)
	}
	if !orderRepo.lastFilter.IncludeArchived || orderRepo.lastFilter.ArchivedOnly {
		t.Fatalf("active-card list filter = %+v, want all activated orders including archived", orderRepo.lastFilter)
	}
	if total != 2 || len(orders) != 2 {
		t.Fatalf("active cards = %d/%d %#v, want HC-ACTIVE and HC-ARCHIVED", total, len(orders), orderNos(orders))
	}
	got := ordersByNo(orders)
	if got["HC-ACTIVE"] == nil || got["HC-ARCHIVED"] == nil {
		t.Fatalf("active cards = %#v, want HC-ACTIVE and HC-ARCHIVED", orderNos(orders))
	}
}

func TestListOrdersEnrichesHubAndTenantNames(t *testing.T) {
	now := time.Now().UTC()
	orderRepo := &orderTestRepo{byNo: map[string]*PurchaseOrder{"HC-ACTIVE": usableSoldOrder("HC-ACTIVE", "g1", now)}}
	authRepo := &authTestRepo{byID: map[string]*llmservice.TenantAuthorization{"auth-HC-ACTIVE": usableSoldAuth("HC-ACTIVE", "g1", now)}}
	svc := NewService(nil, orderRepo, authRepo)
	svc.SetHubTenantResolver(func(_ context.Context, hubID, tenantID string) (string, string) {
		if hubID != "hub-1" || tenantID != "tenant-a" {
			t.Fatalf("resolver ids = %q/%q", hubID, tenantID)
		}
		return "EcoFlow Hub", "研发部"
	})

	orders, _, err := svc.ListOrders(context.Background(), OrderFilter{})
	if err != nil {
		t.Fatalf("ListOrders: %v", err)
	}
	if len(orders) != 1 || orders[0].HubName != "EcoFlow Hub" || orders[0].TenantName != "研发部" {
		t.Fatalf("order identity = %#v", orders)
	}
}

func TestRebindOrderServiceGroupUpdatesOrderAndAuthorization(t *testing.T) {
	now := time.Now().UTC()
	orderRepo := &orderTestRepo{byNo: map[string]*PurchaseOrder{"HC-ACTIVE": usableSoldOrder("HC-ACTIVE", "g1", now)}}
	auth := usableSoldAuth("HC-ACTIVE", "g1", now)
	auth.AllowExternalProviders = true
	authRepo := &authTestRepo{byID: map[string]*llmservice.TenantAuthorization{"auth-HC-ACTIVE": auth}}
	svc := NewService(nil, orderRepo, authRepo)
	svc.SetServiceGroupCatalog(func(context.Context) ([]ServiceGroupRecord, string) {
		return []ServiceGroupRecord{
			{ID: "g1", Name: "Group 1", AccessPolicy: llmservice.AccessPolicyGrantRequired},
			{ID: "g2", Name: "Group 2", AgentID: "agent-2", AgentName: "Agent 2", AccessPolicy: llmservice.AccessPolicyGrantRequired},
		}, "g2"
	})

	got, err := svc.RebindOrderServiceGroup(context.Background(), "HC-ACTIVE", "g2", false)
	if err != nil {
		t.Fatalf("RebindOrderServiceGroup: %v", err)
	}
	if got.ServiceGroupID != "g2" || got.ServiceGroup != "Group 2" || got.AgentName != "Agent 2" {
		t.Fatalf("order group = %+v", got)
	}
	if authRepo.byID["auth-HC-ACTIVE"].ServiceGroupID != "g2" {
		t.Fatalf("auth group = %q", authRepo.byID["auth-HC-ACTIVE"].ServiceGroupID)
	}
	if orderRepo.byNo["HC-ACTIVE"].ServiceGroupID != "g2" {
		t.Fatalf("persisted order group = %q", orderRepo.byNo["HC-ACTIVE"].ServiceGroupID)
	}
	if authRepo.byID["auth-HC-ACTIVE"].AllowExternalProviders {
		t.Fatal("rebind onto a billed group must clear external provider permission")
	}
}

func TestRebindOrderServiceGroupUsesDefaultGroup(t *testing.T) {
	now := time.Now().UTC()
	orderRepo := &orderTestRepo{byNo: map[string]*PurchaseOrder{"HC-ACTIVE": usableSoldOrder("HC-ACTIVE", "g1", now)}}
	authRepo := &authTestRepo{byID: map[string]*llmservice.TenantAuthorization{"auth-HC-ACTIVE": usableSoldAuth("HC-ACTIVE", "g1", now)}}
	svc := NewService(nil, orderRepo, authRepo)
	svc.SetServiceGroupCatalog(func(context.Context) ([]ServiceGroupRecord, string) {
		return []ServiceGroupRecord{
			{ID: "g1", Name: "Group 1", AccessPolicy: llmservice.AccessPolicyGrantRequired},
			{ID: "default-g", Name: "Default", AccessPolicy: llmservice.AccessPolicyGrantRequired},
		}, "default-g"
	})

	got, err := svc.RebindOrderServiceGroup(context.Background(), "HC-ACTIVE", DefaultServiceGroupSentinel, false)
	if err != nil {
		t.Fatalf("RebindOrderServiceGroup default: %v", err)
	}
	if got.ServiceGroupID != "default-g" {
		t.Fatalf("group = %q, want default-g", got.ServiceGroupID)
	}
}

func TestRebindOrderServiceGroupRevertsAuthWhenOrderUpdateFails(t *testing.T) {
	now := time.Now().UTC()
	orderRepo := &orderTestRepo{
		byNo:      map[string]*PurchaseOrder{"HC-ACTIVE": usableSoldOrder("HC-ACTIVE", "g1", now)},
		updateErr: errors.New("database unavailable"),
	}
	auth := usableSoldAuth("HC-ACTIVE", "g1", now)
	auth.AllowExternalProviders = true
	authRepo := &authTestRepo{byID: map[string]*llmservice.TenantAuthorization{"auth-HC-ACTIVE": auth}}
	svc := NewService(nil, orderRepo, authRepo)
	svc.SetServiceGroupCatalog(func(context.Context) ([]ServiceGroupRecord, string) {
		return []ServiceGroupRecord{
			{ID: "g1", Name: "Group 1", AccessPolicy: llmservice.AccessPolicyGrantRequired},
			{ID: "g2", Name: "Group 2", AccessPolicy: llmservice.AccessPolicyGrantRequired},
		}, "g2"
	})

	_, err := svc.RebindOrderServiceGroup(context.Background(), "HC-ACTIVE", "g2", false)
	if err == nil || !strings.Contains(err.Error(), "update order group") {
		t.Fatalf("error = %v", err)
	}
	if got := authRepo.byID["auth-HC-ACTIVE"].ServiceGroupID; got != "g1" {
		t.Fatalf("auth group = %q, want g1 after revert", got)
	}
	if got := orderRepo.byNo["HC-ACTIVE"].ServiceGroupID; got != "g1" {
		t.Fatalf("order group = %q, want g1 after failed persist", got)
	}
	if !authRepo.byID["auth-HC-ACTIVE"].AllowExternalProviders {
		t.Fatal("external provider permission should revert when order persist fails")
	}
}

func TestRebindOrderServiceGroupClearsStaleExternalFlagOnSameGroup(t *testing.T) {
	now := time.Now().UTC()
	orderRepo := &orderTestRepo{byNo: map[string]*PurchaseOrder{"HC-ACTIVE": usableSoldOrder("HC-ACTIVE", "g1", now)}}
	auth := usableSoldAuth("HC-ACTIVE", "g1", now)
	auth.AllowExternalProviders = true
	authRepo := &authTestRepo{byID: map[string]*llmservice.TenantAuthorization{"auth-HC-ACTIVE": auth}}
	svc := NewService(nil, orderRepo, authRepo)
	svc.SetServiceGroupCatalog(func(context.Context) ([]ServiceGroupRecord, string) {
		return []ServiceGroupRecord{{ID: "g1", Name: "Group 1", AccessPolicy: llmservice.AccessPolicyGrantRequired}}, "g1"
	})

	got, err := svc.RebindOrderServiceGroup(context.Background(), "HC-ACTIVE", "g1", false)
	if err != nil {
		t.Fatalf("same-group rebind: %v", err)
	}
	if got.ServiceGroupID != "g1" {
		t.Fatalf("group = %q, want g1", got.ServiceGroupID)
	}
	if authRepo.byID["auth-HC-ACTIVE"].AllowExternalProviders {
		t.Fatal("same-group save must clear leftover external provider permission")
	}
}

func TestRebindOrderServiceGroupClearsStaleExternalFlagWhenCurrentLeftCatalog(t *testing.T) {
	now := time.Now().UTC()
	orderRepo := &orderTestRepo{byNo: map[string]*PurchaseOrder{"HC-ACTIVE": usableSoldOrder("HC-ACTIVE", "stale-g", now)}}
	auth := usableSoldAuth("HC-ACTIVE", "stale-g", now)
	auth.AllowExternalProviders = true
	authRepo := &authTestRepo{byID: map[string]*llmservice.TenantAuthorization{"auth-HC-ACTIVE": auth}}
	svc := NewService(nil, orderRepo, authRepo)
	svc.SetServiceGroupCatalog(func(context.Context) ([]ServiceGroupRecord, string) {
		return []ServiceGroupRecord{{ID: "g2", Name: "Group 2", AccessPolicy: llmservice.AccessPolicyGrantRequired}}, "g2"
	})

	got, err := svc.RebindOrderServiceGroup(context.Background(), "HC-ACTIVE", "stale-g", false)
	if err != nil {
		t.Fatalf("same-group heal: %v", err)
	}
	if got.ServiceGroupID != "stale-g" {
		t.Fatalf("group = %q, want stale-g", got.ServiceGroupID)
	}
	if orderRepo.byNo["HC-ACTIVE"].ServiceGroupID != "stale-g" {
		t.Fatalf("persisted order group = %q, want stale-g", orderRepo.byNo["HC-ACTIVE"].ServiceGroupID)
	}
	if authRepo.byID["auth-HC-ACTIVE"].AllowExternalProviders {
		t.Fatal("saving the current group must clear leftover external permission even if it left the catalog")
	}
}

func TestRebindOrderServiceGroupKeepsExternalFlagOnExternalGroup(t *testing.T) {
	now := time.Now().UTC()
	orderRepo := &orderTestRepo{byNo: map[string]*PurchaseOrder{"HC-ACTIVE": usableSoldOrder("HC-ACTIVE", llmservice.ExternalComputePermissionServiceGroupID, now)}}
	auth := usableSoldAuth("HC-ACTIVE", llmservice.ExternalComputePermissionServiceGroupID, now)
	auth.AllowExternalProviders = true
	authRepo := &authTestRepo{byID: map[string]*llmservice.TenantAuthorization{"auth-HC-ACTIVE": auth}}
	svc := NewService(nil, orderRepo, authRepo)
	svc.SetServiceGroupCatalog(func(context.Context) ([]ServiceGroupRecord, string) {
		return []ServiceGroupRecord{{ID: "g2", Name: "Group 2", AccessPolicy: llmservice.AccessPolicyGrantRequired}}, "g2"
	})

	got, err := svc.RebindOrderServiceGroup(context.Background(), "HC-ACTIVE", llmservice.ExternalComputePermissionServiceGroupID, false)
	if err != nil {
		t.Fatalf("external-group no-op: %v", err)
	}
	if got.ServiceGroupID != llmservice.ExternalComputePermissionServiceGroupID {
		t.Fatalf("group = %q, want external sentinel", got.ServiceGroupID)
	}
	if !authRepo.byID["auth-HC-ACTIVE"].AllowExternalProviders {
		t.Fatal("saving the current external group must keep provider permission")
	}
}

func TestRebindOrderServiceGroupNoopsWhenCurrentGroupLeftCatalog(t *testing.T) {
	now := time.Now().UTC()
	orderRepo := &orderTestRepo{byNo: map[string]*PurchaseOrder{"HC-ACTIVE": usableSoldOrder("HC-ACTIVE", "stale-g", now)}}
	authRepo := &authTestRepo{byID: map[string]*llmservice.TenantAuthorization{"auth-HC-ACTIVE": usableSoldAuth("HC-ACTIVE", "stale-g", now)}}
	svc := NewService(nil, orderRepo, authRepo)
	svc.SetServiceGroupCatalog(func(context.Context) ([]ServiceGroupRecord, string) {
		return []ServiceGroupRecord{{ID: "g2", Name: "Group 2"}}, "g2"
	})

	got, err := svc.RebindOrderServiceGroup(context.Background(), "HC-ACTIVE", "stale-g", false)
	if err != nil {
		t.Fatalf("same-group rebind: %v", err)
	}
	if got.ServiceGroupID != "stale-g" {
		t.Fatalf("group = %q, want stale-g", got.ServiceGroupID)
	}
}

func TestListOrdersActiveCardsOnlyPaginates(t *testing.T) {
	now := time.Now().UTC()
	orderRepo := &orderTestRepo{byNo: map[string]*PurchaseOrder{
		"HC-A":       usableSoldOrder("HC-A", "g1", now),
		"HC-B":       usableSoldOrder("HC-B", "g1", now),
		"HC-C":       usableSoldOrder("HC-C", "g1", now),
		"HC-EXPIRED": expiredSoldOrder("HC-EXPIRED", "g1", now),
	}}
	authRepo := &authTestRepo{byID: map[string]*llmservice.TenantAuthorization{
		"auth-HC-A":       usableSoldAuth("HC-A", "g1", now),
		"auth-HC-B":       usableSoldAuth("HC-B", "g1", now),
		"auth-HC-C":       usableSoldAuth("HC-C", "g1", now),
		"auth-HC-EXPIRED": expiredSoldAuth("HC-EXPIRED", "g1", now),
	}}
	svc := NewService(nil, orderRepo, authRepo)

	page1, total, err := svc.ListOrders(context.Background(), OrderFilter{ActiveCardsOnly: true, Offset: 0, Limit: 2})
	if err != nil {
		t.Fatalf("page1: %v", err)
	}
	if total != 3 || len(page1) != 2 {
		t.Fatalf("page1 = %d/%d, want 2/3", len(page1), total)
	}
	page2, total, err := svc.ListOrders(context.Background(), OrderFilter{ActiveCardsOnly: true, Offset: 2, Limit: 2})
	if err != nil {
		t.Fatalf("page2: %v", err)
	}
	if total != 3 || len(page2) != 1 {
		t.Fatalf("page2 = %d/%d, want 1/3", len(page2), total)
	}
}

func TestRebindOrderServiceGroupRejectsFreeGroup(t *testing.T) {
	now := time.Now().UTC()
	orderRepo := &orderTestRepo{byNo: map[string]*PurchaseOrder{"HC-ACTIVE": usableSoldOrder("HC-ACTIVE", "g1", now)}}
	authRepo := &authTestRepo{byID: map[string]*llmservice.TenantAuthorization{"auth-HC-ACTIVE": usableSoldAuth("HC-ACTIVE", "g1", now)}}
	svc := NewService(nil, orderRepo, authRepo)
	svc.SetServiceGroupCatalog(func(context.Context) ([]ServiceGroupRecord, string) {
		return []ServiceGroupRecord{
			{ID: "g1", Name: "Group 1", AccessPolicy: llmservice.AccessPolicyGrantRequired},
			{ID: "free-g", Name: "Free", AccessPolicy: llmservice.AccessPolicyFree},
		}, "free-g"
	})

	_, err := svc.RebindOrderServiceGroup(context.Background(), "HC-ACTIVE", "free-g", false)
	if err == nil || !strings.Contains(err.Error(), "does not require grant") {
		t.Fatalf("free group error = %v", err)
	}
	if got := orderRepo.byNo["HC-ACTIVE"].ServiceGroupID; got != "g1" {
		t.Fatalf("order group = %q, want g1", got)
	}
	_, err = svc.RebindOrderServiceGroup(context.Background(), "HC-ACTIVE", DefaultServiceGroupSentinel, false)
	if err == nil || !strings.Contains(err.Error(), "default service group does not require grant") {
		t.Fatalf("free default error = %v", err)
	}
	_, err = svc.RebindOrderServiceGroup(context.Background(), "HC-ACTIVE", "g1", false)
	if err != nil {
		t.Fatalf("same grant-required group should no-op: %v", err)
	}
}

func TestRebindOrderServiceGroupRejectsEmptyAccessPolicy(t *testing.T) {
	now := time.Now().UTC()
	orderRepo := &orderTestRepo{byNo: map[string]*PurchaseOrder{"HC-ACTIVE": usableSoldOrder("HC-ACTIVE", "g1", now)}}
	authRepo := &authTestRepo{byID: map[string]*llmservice.TenantAuthorization{"auth-HC-ACTIVE": usableSoldAuth("HC-ACTIVE", "g1", now)}}
	svc := NewService(nil, orderRepo, authRepo)
	svc.SetServiceGroupCatalog(func(context.Context) ([]ServiceGroupRecord, string) {
		return []ServiceGroupRecord{{ID: "g1", Name: "Group 1"}, {ID: "g2", Name: "Group 2"}}, "g2"
	})
	_, err := svc.RebindOrderServiceGroup(context.Background(), "HC-ACTIVE", "g2", false)
	if err == nil || !strings.Contains(err.Error(), "does not require grant") {
		t.Fatalf("empty policy error = %v", err)
	}
}

func TestRebindOrderServiceGroupRejectsUnknownGroup(t *testing.T) {
	now := time.Now().UTC()
	orderRepo := &orderTestRepo{byNo: map[string]*PurchaseOrder{"HC-ACTIVE": usableSoldOrder("HC-ACTIVE", "g1", now)}}
	authRepo := &authTestRepo{byID: map[string]*llmservice.TenantAuthorization{"auth-HC-ACTIVE": usableSoldAuth("HC-ACTIVE", "g1", now)}}
	svc := NewService(nil, orderRepo, authRepo)
	svc.SetServiceGroupCatalog(func(context.Context) ([]ServiceGroupRecord, string) {
		return []ServiceGroupRecord{{ID: "g1", Name: "Group 1"}}, ""
	})
	_, err := svc.RebindOrderServiceGroup(context.Background(), "HC-ACTIVE", "missing", false)
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("error = %v", err)
	}
	_, err = svc.RebindOrderServiceGroup(context.Background(), "HC-ACTIVE", DefaultServiceGroupSentinel, false)
	if err == nil || !strings.Contains(err.Error(), "default service group is not configured") {
		t.Fatalf("default error = %v", err)
	}
}

func TestRebindOrderServiceGroupRejectsUnusableCards(t *testing.T) {
	now := time.Now().UTC()
	tests := []struct {
		name  string
		order *PurchaseOrder
		auth  *llmservice.TenantAuthorization
	}{
		{name: "expired", order: expiredSoldOrder("HC-EXPIRED", "g1", now), auth: expiredSoldAuth("HC-EXPIRED", "g1", now)},
		{name: "expired-status-case", order: usableSoldOrder("HC-EXPIRED-CASE", "g1", now), auth: expiredStatusCaseAuth("HC-EXPIRED-CASE", "g1", now)},
		{name: "exhausted", order: exhaustedSoldOrder("HC-EXHAUSTED", "g1", now), auth: exhaustedSoldAuth("HC-EXHAUSTED", "g1", now)},
		{name: "pending", order: &PurchaseOrder{Order: corecardstore.Order{OrderNo: "HC-PENDING", Status: corecardstore.StatusPending}, HubID: "hub-1", TenantID: "tenant-a", ServiceGroupID: "g1"}, auth: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			orderRepo := &orderTestRepo{byNo: map[string]*PurchaseOrder{tt.order.OrderNo: tt.order}}
			authRepo := &authTestRepo{byID: map[string]*llmservice.TenantAuthorization{}}
			if tt.auth != nil {
				authRepo.byID[tt.auth.ID] = tt.auth
			}
			svc := NewService(nil, orderRepo, authRepo)
			svc.SetServiceGroupCatalog(func(context.Context) ([]ServiceGroupRecord, string) {
				return []ServiceGroupRecord{{ID: "g2", Name: "Group 2"}}, "g2"
			})
			_, err := svc.RebindOrderServiceGroup(context.Background(), tt.order.OrderNo, "g2", false)
			if err == nil || !strings.Contains(err.Error(), "only unused remaining active cards") {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func usableSoldOrder(orderNo, groupID string, now time.Time) *PurchaseOrder {
	return &PurchaseOrder{
		Order:          corecardstore.Order{OrderNo: orderNo, Status: corecardstore.StatusActivated, PaymentID: "auth-" + orderNo},
		HubID:          "hub-1",
		TenantID:       "tenant-a",
		ServiceGroupID: groupID,
		Credits:        1000,
	}
}

func expiredSoldOrder(orderNo, groupID string, now time.Time) *PurchaseOrder {
	return usableSoldOrder(orderNo, groupID, now)
}

func exhaustedSoldOrder(orderNo, groupID string, now time.Time) *PurchaseOrder {
	return usableSoldOrder(orderNo, groupID, now)
}

func usableSoldAuth(orderNo, groupID string, now time.Time) *llmservice.TenantAuthorization {
	return &llmservice.TenantAuthorization{
		ID: "auth-" + orderNo, HubID: "hub-1", TenantID: "tenant-a", ServiceGroupID: groupID,
		CreditsTotal: 1000, CreditsUsed: 10, StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(24 * time.Hour),
		Status: "active", Source: "card", CardOrderID: orderNo, CreatedAt: now,
	}
}

func expiredSoldAuth(orderNo, groupID string, now time.Time) *llmservice.TenantAuthorization {
	auth := usableSoldAuth(orderNo, groupID, now)
	auth.ExpiresAt = now.Add(-time.Hour)
	auth.Status = "expired"
	return auth
}

func expiredStatusCaseAuth(orderNo, groupID string, now time.Time) *llmservice.TenantAuthorization {
	auth := usableSoldAuth(orderNo, groupID, now)
	auth.Status = "EXPIRED"
	return auth
}

func exhaustedSoldAuth(orderNo, groupID string, now time.Time) *llmservice.TenantAuthorization {
	auth := usableSoldAuth(orderNo, groupID, now)
	auth.CreditsUsed = 1000
	auth.Status = "exhausted"
	return auth
}

func ordersByNo(orders []*PurchaseOrder) map[string]*PurchaseOrder {
	out := map[string]*PurchaseOrder{}
	for _, order := range orders {
		if order != nil {
			out[order.OrderNo] = order
		}
	}
	return out
}

func orderNos(orders []*PurchaseOrder) []string {
	var out []string
	for _, order := range orders {
		if order != nil {
			out = append(out, order.OrderNo)
		}
	}
	return out
}
