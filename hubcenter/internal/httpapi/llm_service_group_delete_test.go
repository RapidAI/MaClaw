package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/llmpool"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/cardstore"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/llmservice"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/store"
)

type llmDeleteTestSettings struct {
	data map[string]string
}

func (s *llmDeleteTestSettings) Get(_ context.Context, key string) (string, error) {
	return s.data[key], nil
}

func (s *llmDeleteTestSettings) Set(_ context.Context, key, value string) error {
	if s.data == nil {
		s.data = map[string]string{}
	}
	s.data[key] = value
	return nil
}

func (s *llmDeleteTestSettings) List(_ context.Context) ([]*store.SystemSettingEntry, error) {
	return nil, nil
}

type llmDeleteAuthRepo struct {
	auths []*llmservice.TenantAuthorization
}

func (r *llmDeleteAuthRepo) Create(_ context.Context, auth *llmservice.TenantAuthorization) error {
	r.auths = append(r.auths, auth)
	return nil
}

func (r *llmDeleteAuthRepo) GetByID(_ context.Context, id string) (*llmservice.TenantAuthorization, error) {
	for _, auth := range r.auths {
		if auth.ID == id {
			return auth, nil
		}
	}
	return nil, nil
}

func (r *llmDeleteAuthRepo) ListByHubTenant(_ context.Context, hubID, tenantID string) ([]*llmservice.TenantAuthorization, error) {
	var matches []*llmservice.TenantAuthorization
	for _, auth := range r.auths {
		if auth.HubID == hubID && auth.TenantID == tenantID {
			matches = append(matches, auth)
		}
	}
	return matches, nil
}

func (r *llmDeleteAuthRepo) ListAll(_ context.Context) ([]*llmservice.TenantAuthorization, error) {
	return r.auths, nil
}

func (r *llmDeleteAuthRepo) ListByServiceGroup(_ context.Context, serviceGroupID string) ([]*llmservice.TenantAuthorization, error) {
	var matches []*llmservice.TenantAuthorization
	for _, auth := range r.auths {
		if auth.ServiceGroupID == serviceGroupID {
			matches = append(matches, auth)
		}
	}
	return matches, nil
}

func (r *llmDeleteAuthRepo) Update(_ context.Context, _ *llmservice.TenantAuthorization) error {
	return nil
}

func (r *llmDeleteAuthRepo) DeductCredits(_ context.Context, _ string, _ float64, _ time.Time) error {
	return nil
}

type llmDeleteCardTypeRepo struct {
	types []*cardstore.CardType
}

func (r *llmDeleteCardTypeRepo) Create(_ context.Context, ct *cardstore.CardType) error {
	r.types = append(r.types, ct)
	return nil
}

func (r *llmDeleteCardTypeRepo) Update(_ context.Context, ct *cardstore.CardType) error {
	for i, existing := range r.types {
		if existing.ID == ct.ID {
			r.types[i] = ct
			return nil
		}
	}
	r.types = append(r.types, ct)
	return nil
}

func (r *llmDeleteCardTypeRepo) GetByID(_ context.Context, id string) (*cardstore.CardType, error) {
	for _, ct := range r.types {
		if ct.ID == id {
			return ct, nil
		}
	}
	return nil, nil
}

func (r *llmDeleteCardTypeRepo) ListEnabled(_ context.Context) ([]*cardstore.CardType, error) {
	var enabled []*cardstore.CardType
	for _, ct := range r.types {
		if ct.Enabled {
			enabled = append(enabled, ct)
		}
	}
	return enabled, nil
}

func (r *llmDeleteCardTypeRepo) ListAll(_ context.Context) ([]*cardstore.CardType, error) {
	return r.types, nil
}

func (r *llmDeleteCardTypeRepo) Delete(_ context.Context, id string) error {
	filtered := r.types[:0]
	for _, ct := range r.types {
		if ct.ID != id {
			filtered = append(filtered, ct)
		}
	}
	r.types = filtered
	return nil
}

type llmDeleteOrderRepo struct {
	orders []*cardstore.PurchaseOrder
}

func (r *llmDeleteOrderRepo) Create(_ context.Context, order *cardstore.PurchaseOrder) error {
	r.orders = append(r.orders, order)
	return nil
}

func (r *llmDeleteOrderRepo) GetByOrderNo(_ context.Context, orderNo string) (*cardstore.PurchaseOrder, error) {
	for _, order := range r.orders {
		if order.OrderNo == orderNo {
			return order, nil
		}
	}
	return nil, nil
}

func (r *llmDeleteOrderRepo) List(_ context.Context, filter cardstore.OrderFilter) ([]*cardstore.PurchaseOrder, int, error) {
	var matches []*cardstore.PurchaseOrder
	for _, order := range r.orders {
		if filter.ServiceGroupID != "" && order.ServiceGroupID != filter.ServiceGroupID {
			continue
		}
		matches = append(matches, order)
	}
	return matches, len(matches), nil
}

func (r *llmDeleteOrderRepo) UpdateStatus(_ context.Context, _, _ string, _ time.Time) error {
	return nil
}

func (r *llmDeleteOrderRepo) Update(_ context.Context, _ *cardstore.PurchaseOrder) error {
	return nil
}

func TestAdminDeleteLLMServiceGroupRejectsTenantInUse(t *testing.T) {
	ctx := context.Background()
	svc := llmservice.NewService(&llmDeleteTestSettings{})
	if err := svc.SaveRegistry(ctx, &llmservice.Registry{
		ServiceGroups: []llmpool.ServiceGroup{{
			ID:   "group_in_use",
			Name: "Group In Use",
		}},
	}); err != nil {
		t.Fatalf("save registry: %v", err)
	}

	checker := llmservice.NewAuthorizationChecker(&llmDeleteAuthRepo{auths: []*llmservice.TenantAuthorization{{
		ID:             "auth1",
		HubID:          "hub1",
		TenantID:       "tenant1",
		ServiceGroupID: "group_in_use",
		CreditsTotal:   100,
		StartsAt:       time.Now().Add(-time.Hour),
		ExpiresAt:      time.Now().Add(time.Hour),
		Status:         "active",
	}}})

	req := httptest.NewRequest(http.MethodDelete, "/api/admin/llm/service-groups/group_in_use", nil)
	req.SetPathValue("id", "group_in_use")
	rr := httptest.NewRecorder()

	adminDeleteLLMServiceGroup(svc, checker, nil).ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%s", rr.Code, http.StatusBadRequest, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "used by tenant hub1/tenant1") {
		t.Fatalf("response body missing tenant usage message: %s", rr.Body.String())
	}

	reg, err := svc.LoadRegistry(ctx)
	if err != nil {
		t.Fatalf("load registry: %v", err)
	}
	if len(reg.ServiceGroups) != 1 || reg.ServiceGroups[0].ID != "group_in_use" {
		t.Fatalf("service group was deleted despite tenant usage: %#v", reg.ServiceGroups)
	}
}

func TestAdminDeleteLLMServiceGroupRejectsCardTypeInUse(t *testing.T) {
	ctx := context.Background()
	svc := newDeleteTestLLMService(t, "group_with_card")
	cardSvc := cardstore.NewService(&llmDeleteCardTypeRepo{types: []*cardstore.CardType{{
		ID:             "ct1",
		ServiceGroupID: "group_with_card",
		Label:          "Card 1",
		Template:       "enterprise_monthly_blue",
		Enabled:        true,
	}}}, &llmDeleteOrderRepo{}, &llmDeleteAuthRepo{})

	rr := performDeleteServiceGroup(svc, llmservice.NewAuthorizationChecker(&llmDeleteAuthRepo{}), cardSvc, "group_with_card")

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%s", rr.Code, http.StatusBadRequest, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "used by card type ct1") {
		t.Fatalf("response body missing card type usage message: %s", rr.Body.String())
	}
	assertServiceGroupStillExists(t, ctx, svc, "group_with_card")
}

func TestAdminDeleteLLMServiceGroupRejectsPurchaseOrderInUse(t *testing.T) {
	ctx := context.Background()
	svc := newDeleteTestLLMService(t, "group_with_order")
	cardSvc := cardstore.NewService(&llmDeleteCardTypeRepo{}, &llmDeleteOrderRepo{orders: []*cardstore.PurchaseOrder{{
		CardTypeID:     "ct1",
		ServiceGroupID: "group_with_order",
	}}}, &llmDeleteAuthRepo{})

	rr := performDeleteServiceGroup(svc, llmservice.NewAuthorizationChecker(&llmDeleteAuthRepo{}), cardSvc, "group_with_order")

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%s", rr.Code, http.StatusBadRequest, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "has purchase orders") {
		t.Fatalf("response body missing order usage message: %s", rr.Body.String())
	}
	assertServiceGroupStillExists(t, ctx, svc, "group_with_order")
}

func TestAdminDeleteLLMServiceGroupDeletesUnusedGroup(t *testing.T) {
	ctx := context.Background()
	svc := newDeleteTestLLMService(t, "unused_group")
	cardSvc := cardstore.NewService(&llmDeleteCardTypeRepo{}, &llmDeleteOrderRepo{}, &llmDeleteAuthRepo{})

	rr := performDeleteServiceGroup(svc, llmservice.NewAuthorizationChecker(&llmDeleteAuthRepo{}), cardSvc, "unused_group")

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rr.Code, http.StatusOK, rr.Body.String())
	}
	reg, err := svc.LoadRegistry(ctx)
	if err != nil {
		t.Fatalf("load registry: %v", err)
	}
	if len(reg.ServiceGroups) != 0 {
		t.Fatalf("service group was not deleted: %#v", reg.ServiceGroups)
	}
}

func newDeleteTestLLMService(t *testing.T, groupID string) *llmservice.Service {
	t.Helper()
	svc := llmservice.NewService(&llmDeleteTestSettings{})
	if err := svc.SaveRegistry(context.Background(), &llmservice.Registry{
		ServiceGroups: []llmpool.ServiceGroup{{ID: groupID, Name: groupID}},
	}); err != nil {
		t.Fatalf("save registry: %v", err)
	}
	return svc
}

func performDeleteServiceGroup(svc *llmservice.Service, checker *llmservice.AuthorizationChecker, cardSvc *cardstore.Service, groupID string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodDelete, "/api/admin/llm/service-groups/"+groupID, nil)
	req.SetPathValue("id", groupID)
	rr := httptest.NewRecorder()
	adminDeleteLLMServiceGroup(svc, checker, cardSvc).ServeHTTP(rr, req)
	return rr
}

func assertServiceGroupStillExists(t *testing.T, ctx context.Context, svc *llmservice.Service, groupID string) {
	t.Helper()
	reg, err := svc.LoadRegistry(ctx)
	if err != nil {
		t.Fatalf("load registry: %v", err)
	}
	if len(reg.ServiceGroups) != 1 || reg.ServiceGroups[0].ID != groupID {
		t.Fatalf("service group was deleted despite usage: %#v", reg.ServiceGroups)
	}
}
