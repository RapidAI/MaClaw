package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/llmservice"
	"github.com/RapidAI/CodeClaw/hub/internal/security"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
	"github.com/RapidAI/CodeClaw/hub/internal/store/sqlite"
)

type testLLMServiceSystemSettings struct {
	data      map[string]string
	getCounts map[string]*atomic.Int32
}

func newTestLLMServiceSystemSettings() *testLLMServiceSystemSettings {
	return &testLLMServiceSystemSettings{data: map[string]string{}, getCounts: map[string]*atomic.Int32{}}
}

func (s *testLLMServiceSystemSettings) Set(_ context.Context, key, valueJSON string) error {
	s.data[key] = valueJSON
	return nil
}

func (s *testLLMServiceSystemSettings) Get(_ context.Context, key string) (string, error) {
	s.counterForKey(key).Add(1)
	return s.data[key], nil
}

func (s *testLLMServiceSystemSettings) GetCount(key string) int {
	return int(s.counterForKey(key).Load())
}

func (s *testLLMServiceSystemSettings) ResetGetCounts() {
	for _, counter := range s.getCounts {
		if counter != nil {
			counter.Store(0)
		}
	}
}

func (s *testLLMServiceSystemSettings) counterForKey(key string) *atomic.Int32 {
	counter := s.getCounts[key]
	if counter == nil {
		counter = &atomic.Int32{}
		s.getCounts[key] = counter
	}
	return counter
}

type testAdminAuditRepo struct {
	logs       []*store.AdminAuditLog
	lastFilter store.AdminAuditLogFilter
}

func (r *testAdminAuditRepo) Create(_ context.Context, log *store.AdminAuditLog) error {
	clone := *log
	r.logs = append(r.logs, &clone)
	return nil
}

func (r *testAdminAuditRepo) List(_ context.Context, filter store.AdminAuditLogFilter) ([]*store.AdminAuditLog, error) {
	r.lastFilter = filter
	items := make([]*store.AdminAuditLog, len(r.logs))
	copy(items, r.logs)
	return items, nil
}

func TestCreateLLMServiceCardHandlerGeneratesBatchCodes(t *testing.T) {
	ctx := context.Background()
	system := newTestLLMServiceSystemSettings()
	if err := llmservice.SaveRegistry(ctx, system, &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{{ID: "coding-basic", Name: "Coding Basic"}},
	}); err != nil {
		t.Fatal(err)
	}

	body := []byte(`{"label":"April","service_group_ids":["coding-basic"],"duration_days":30,"credits":1000,"count":3}`)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/llm/service-cards", bytes.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), adminUserContextKey, &store.AdminUser{ID: "adm-1"}))
	rec := httptest.NewRecorder()
	audit := &testAdminAuditRepo{}
	CreateLLMServiceCardHandler(system, audit).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Cards []struct {
			Code string `json:"code"`
		} `json:"cards"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Cards) != 3 {
		t.Fatalf("expected 3 cards, got %#v", resp.Cards)
	}
	seen := map[string]struct{}{}
	for _, card := range resp.Cards {
		if err := llmservice.ValidateCardCode(card.Code); err != nil {
			t.Fatalf("generated code %q failed validation: %v", card.Code, err)
		}
		if _, exists := seen[card.Code]; exists {
			t.Fatalf("duplicate response code: %q", card.Code)
		}
		seen[card.Code] = struct{}{}
	}

	saved, err := llmservice.LoadRegistry(ctx, system)
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Cards) != 3 {
		t.Fatalf("expected 3 saved cards, got %d", len(saved.Cards))
	}
	for _, card := range saved.Cards {
		if card.CodeHash == "" {
			t.Fatalf("saved card missing code hash: %#v", card)
		}
	}
	if len(audit.logs) != 1 {
		t.Fatalf("expected 1 audit log, got %d", len(audit.logs))
	}
	if audit.logs[0].Action != "llm.service_card.create" {
		t.Fatalf("unexpected audit action: %s", audit.logs[0].Action)
	}
	if audit.logs[0].AdminUserID != "adm-1" {
		t.Fatalf("unexpected audit admin user id: %s", audit.logs[0].AdminUserID)
	}
}

func TestRedeemLLMServiceCardHandlerScopesCardsByTenant(t *testing.T) {
	ctx := context.Background()
	identity, _, _ := newHTTPAPITestServices(t)
	system := newTestLLMServiceSystemSettings()
	code, err := llmservice.GenerateCardCode()
	if err != nil {
		t.Fatalf("GenerateCardCode: %v", err)
	}
	enc, err := llmservice.EncryptCardCode(code)
	if err != nil {
		t.Fatalf("EncryptCardCode: %v", err)
	}
	baseRegistry := llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{{ID: "coding-basic", Name: "Coding Basic"}},
	}
	if err := llmservice.SaveRegistry(ctx, scopedSystemSettingsForTenant("tenant_a", system), &llmservice.Registry{
		ModelServiceGroups: baseRegistry.ModelServiceGroups,
		Cards: []llmservice.RechargeCard{{
			ID:              "card-tenant-a",
			CodeHash:        llmservice.HashCode(code),
			EncryptedCode:   enc,
			Label:           "Tenant A",
			ServiceGroupIDs: []string{"coding-basic"},
			DurationDays:    30,
			Credits:         100,
			CreatedAt:       time.Now().UTC(),
		}},
	}); err != nil {
		t.Fatalf("Save tenant_a registry: %v", err)
	}
	if err := llmservice.SaveRegistry(ctx, scopedSystemSettingsForTenant("tenant_b", system), &baseRegistry); err != nil {
		t.Fatalf("Save tenant_b registry: %v", err)
	}

	tenantBToken := issueViewerTokenForTenant(t, identity, "tenant_b", "same@example.com")
	req := httptest.NewRequest(http.MethodPost, "/api/llm/service/redeem", bytes.NewReader([]byte(fmt.Sprintf(`{"code":%q}`, code))))
	req.Header.Set("Authorization", "Bearer "+tenantBToken)
	rec := httptest.NewRecorder()
	RedeemLLMServiceCardHandler(identity, system, nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("tenant_b redeem status = %d, body = %s", rec.Code, rec.Body.String())
	}
	savedA, err := llmservice.LoadRegistry(ctx, scopedSystemSettingsForTenant("tenant_a", system))
	if err != nil {
		t.Fatalf("Load tenant_a registry: %v", err)
	}
	if savedA.Cards[0].RedeemedAt != nil {
		t.Fatalf("tenant_b should not redeem tenant_a card: %#v", savedA.Cards[0])
	}

	tenantAToken := issueViewerTokenForTenant(t, identity, "tenant_a", "same@example.com")
	req = httptest.NewRequest(http.MethodPost, "/api/llm/service/redeem", bytes.NewReader([]byte(fmt.Sprintf(`{"code":%q}`, code))))
	req.Header.Set("Authorization", "Bearer "+tenantAToken)
	rec = httptest.NewRecorder()
	RedeemLLMServiceCardHandler(identity, system, nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("tenant_a redeem status = %d, body = %s", rec.Code, rec.Body.String())
	}
	savedA, err = llmservice.LoadRegistry(ctx, scopedSystemSettingsForTenant("tenant_a", system))
	if err != nil {
		t.Fatalf("Reload tenant_a registry: %v", err)
	}
	if savedA.Cards[0].RedeemedAt == nil || savedA.Cards[0].RedeemedByEmail != "same@example.com" {
		t.Fatalf("tenant_a card was not redeemed by tenant_a user: %#v", savedA.Cards[0])
	}
}

func issueViewerTokenForTenant(t *testing.T, identity *auth.IdentityService, tenantID, email string) string {
	t.Helper()
	ctx := auth.WithTenant(context.Background(), tenantID)
	user, err := identity.ManualBindForTenant(ctx, tenantID, email)
	if err != nil {
		t.Fatalf("ManualBindForTenant(%s): %v", tenantID, err)
	}
	token, err := identity.IssueViewerTokenForUser(ctx, user.ID)
	if err != nil {
		t.Fatalf("IssueViewerTokenForUser(%s): %v", tenantID, err)
	}
	return token
}

func TestCreateLLMServiceCardHandlerAppliesDefaultCreditsWhenMissingOrInvalid(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name string
		body string
		want float64
	}{
		{name: "missing", body: `{"service_group_ids":["coding-basic"],"duration_days":30,"count":1}`, want: 5000},
		{name: "negative", body: `{"service_group_ids":["coding-basic"],"duration_days":7,"credits":-10,"count":1}`, want: 1200},
		{name: "zero", body: `{"service_group_ids":["coding-basic"],"duration_days":1,"credits":0,"count":1}`, want: 300},
		{name: "override", body: `{"service_group_ids":["coding-basic"],"duration_days":30,"credits":1234,"count":1}`, want: 1234},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			system := newTestLLMServiceSystemSettings()
			if err := llmservice.SaveRegistry(ctx, system, &llmservice.Registry{
				ModelServiceGroups: []llmservice.ModelServiceGroup{{ID: "coding-basic", Name: "Coding Basic"}},
			}); err != nil {
				t.Fatal(err)
			}

			req := httptest.NewRequest(http.MethodPost, "/api/admin/llm/service-cards", bytes.NewReader([]byte(tc.body)))
			rec := httptest.NewRecorder()
			CreateLLMServiceCardHandler(system, nil).ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
			}
			saved, err := llmservice.LoadRegistry(ctx, system)
			if err != nil {
				t.Fatal(err)
			}
			if len(saved.Cards) != 1 || saved.Cards[0].Credits != tc.want {
				t.Fatalf("credits = %#v, want %v", saved.Cards, tc.want)
			}
		})
	}
}

func TestCreateLLMServiceCardHandlerPersistsPeriodLimitsAndCapsDuration(t *testing.T) {
	ctx := context.Background()
	system := newTestLLMServiceSystemSettings()
	if err := llmservice.SaveRegistry(ctx, system, &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{{ID: "coding-basic", Name: "Coding Basic"}},
	}); err != nil {
		t.Fatal(err)
	}

	body := []byte(`{"service_group_ids":["coding-basic"],"duration_days":365,"credits":1000,"five_hour_credits":50,"daily_credits":100,"weekly_credits":400,"monthly_credits":800,"count":1}`)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/llm/service-cards", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	CreateLLMServiceCardHandler(system, nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Cards []struct {
			PeriodLimits llmservice.CreditPeriodLimits `json:"period_limits"`
		} `json:"cards"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Cards) != 1 || resp.Cards[0].PeriodLimits.Daily != 100 {
		t.Fatalf("unexpected response period limits: %#v", resp.Cards)
	}
	saved, err := llmservice.LoadRegistry(ctx, system)
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Cards) != 1 || saved.Cards[0].PeriodLimits.FiveHour != 50 || saved.Cards[0].PeriodLimits.Monthly != 800 {
		t.Fatalf("unexpected saved period limits: %#v", saved.Cards)
	}

	body = []byte(`{"service_group_ids":["coding-basic"],"duration_days":2,"credits":100,"count":1}`)
	req = httptest.NewRequest(http.MethodPost, "/api/admin/llm/service-cards", bytes.NewReader(body))
	rec = httptest.NewRecorder()
	CreateLLMServiceCardHandler(system, nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected unsupported duration rejection, status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestCreateLLMServiceCardHandlerAllowsFixedDurations(t *testing.T) {
	ctx := context.Background()
	allowed := []int{1, 7, 30, 91, 365}
	for _, days := range allowed {
		t.Run(fmt.Sprintf("%d_days", days), func(t *testing.T) {
			system := newTestLLMServiceSystemSettings()
			if err := llmservice.SaveRegistry(ctx, system, &llmservice.Registry{
				ModelServiceGroups: []llmservice.ModelServiceGroup{{ID: "coding-basic", Name: "Coding Basic"}},
			}); err != nil {
				t.Fatal(err)
			}

			body := []byte(fmt.Sprintf(`{"service_group_ids":["coding-basic"],"duration_days":%d,"credits":100,"count":1}`, days))
			req := httptest.NewRequest(http.MethodPost, "/api/admin/llm/service-cards", bytes.NewReader(body))
			rec := httptest.NewRecorder()
			CreateLLMServiceCardHandler(system, nil).ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("duration %d rejected: status = %d, body = %s", days, rec.Code, rec.Body.String())
			}
			saved, err := llmservice.LoadRegistry(ctx, system)
			if err != nil {
				t.Fatal(err)
			}
			if len(saved.Cards) != 1 || saved.Cards[0].DurationDays != days {
				t.Fatalf("unexpected saved duration for %d: %#v", days, saved.Cards)
			}
		})
	}
}

func TestCreateLLMServiceCardHandlerClearsInapplicablePeriodLimits(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name string
		days int
		want llmservice.CreditPeriodLimits
	}{
		{name: "day", days: 1, want: llmservice.CreditPeriodLimits{FiveHour: 50}},
		{name: "week", days: 7, want: llmservice.CreditPeriodLimits{FiveHour: 50, Daily: 100}},
		{name: "month", days: 30, want: llmservice.CreditPeriodLimits{FiveHour: 50, Daily: 100, Weekly: 200}},
		{name: "quarter", days: 91, want: llmservice.CreditPeriodLimits{FiveHour: 50, Daily: 100, Weekly: 200, Monthly: 300}},
		{name: "year", days: 365, want: llmservice.CreditPeriodLimits{FiveHour: 50, Daily: 100, Weekly: 200, Monthly: 300}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			system := newTestLLMServiceSystemSettings()
			if err := llmservice.SaveRegistry(ctx, system, &llmservice.Registry{
				ModelServiceGroups: []llmservice.ModelServiceGroup{{ID: "coding-basic", Name: "Coding Basic"}},
			}); err != nil {
				t.Fatal(err)
			}

			body := []byte(fmt.Sprintf(`{"service_group_ids":["coding-basic"],"duration_days":%d,"credits":100,"five_hour_credits":50,"daily_credits":100,"weekly_credits":200,"monthly_credits":300,"count":1}`, tc.days))
			req := httptest.NewRequest(http.MethodPost, "/api/admin/llm/service-cards", bytes.NewReader(body))
			rec := httptest.NewRecorder()
			CreateLLMServiceCardHandler(system, nil).ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
			}
			saved, err := llmservice.LoadRegistry(ctx, system)
			if err != nil {
				t.Fatal(err)
			}
			if len(saved.Cards) != 1 || saved.Cards[0].PeriodLimits != tc.want {
				t.Fatalf("period limits = %#v, want %#v", saved.Cards, tc.want)
			}
		})
	}
}

func TestCreateLLMServiceCardHandlerRejectsOversizedBatch(t *testing.T) {
	system := newTestLLMServiceSystemSettings()
	if err := llmservice.SaveRegistry(context.Background(), system, &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{{ID: "coding-basic", Name: "Coding Basic"}},
	}); err != nil {
		t.Fatal(err)
	}

	body := []byte(`{"service_group_ids":["coding-basic"],"count":1001}`)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/llm/service-cards", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	CreateLLMServiceCardHandler(system, nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestListLLMServiceCardsHandlerPaginatesAndFilters(t *testing.T) {
	now := time.Now().UTC()
	system := newTestLLMServiceSystemSettings()
	if err := llmservice.SaveRegistry(context.Background(), system, &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{{ID: "coding-basic", Name: "Coding Basic"}},
		Cards: []llmservice.RechargeCard{
			{ID: "card-1", Label: "Alpha", ServiceGroupIDs: []string{"coding-basic"}, DurationDays: 30, Credits: 100, CreatedAt: now},
			{ID: "card-2", Label: "Beta", ServiceGroupIDs: []string{"coding-basic"}, DurationDays: 30, Credits: 100, CreatedAt: now},
			{ID: "card-3", Label: "Gamma", ServiceGroupIDs: []string{"coding-basic"}, DurationDays: 30, Credits: 100, CreatedAt: now, RedeemedByEmail: "user@example.com", RedeemedAt: &now},
		},
	}); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/admin/llm/service-cards?status=unused&search=a&page=1&page_size=1", nil)
	rec := httptest.NewRecorder()
	ListLLMServiceCardsHandler(system).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
		Total    int `json:"total"`
		Page     int `json:"page"`
		PageSize int `json:"page_size"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Total != 2 {
		t.Fatalf("expected total 2, got %d", resp.Total)
	}
	if resp.Page != 1 || resp.PageSize != 1 {
		t.Fatalf("unexpected page payload: %#v", resp)
	}
	if len(resp.Items) != 1 || resp.Items[0].ID != "card-1" {
		t.Fatalf("unexpected items: %#v", resp.Items)
	}
}

func TestLLMServiceCardsAdminHandlersScopeByTenant(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	system := newTestLLMServiceSystemSettings()
	tenantASettings := scopedSystemSettingsForTenant("tenant_a", system)
	tenantBSettings := scopedSystemSettingsForTenant("tenant_b", system)
	if err := llmservice.SaveRegistry(ctx, tenantASettings, &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{{ID: "coding-basic", Name: "Coding Basic"}},
		Cards: []llmservice.RechargeCard{
			{ID: "card-tenant-a", Label: "Tenant A", ServiceGroupIDs: []string{"coding-basic"}, DurationDays: 30, Credits: 100, CreatedAt: now},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := llmservice.SaveRegistry(ctx, tenantBSettings, &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{{ID: "coding-basic", Name: "Coding Basic"}},
		Cards: []llmservice.RechargeCard{
			{ID: "card-tenant-b", Label: "Tenant B", ServiceGroupIDs: []string{"coding-basic"}, DurationDays: 30, Credits: 100, CreatedAt: now},
		},
	}); err != nil {
		t.Fatal(err)
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/admin/llm/service-cards?status=all", nil)
	listReq = listReq.WithContext(context.WithValue(listReq.Context(), adminUserContextKey, &store.AdminUser{ID: "adm-a", Scope: "tenant", TenantID: "tenant_a"}))
	listRec := httptest.NewRecorder()
	ListLLMServiceCardsHandler(system).ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status = %d, body = %s", listRec.Code, listRec.Body.String())
	}
	var listResp struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	if err := json.Unmarshal(listRec.Body.Bytes(), &listResp); err != nil {
		t.Fatal(err)
	}
	if len(listResp.Items) != 1 || listResp.Items[0].ID != "card-tenant-a" {
		t.Fatalf("tenant A list leaked cards: %#v", listResp.Items)
	}

	createBody := []byte(`{"label":"Issued","service_group_ids":["coding-basic"],"duration_days":30,"credits":100,"count":1}`)
	createReq := httptest.NewRequest(http.MethodPost, "/api/admin/llm/service-cards", bytes.NewReader(createBody))
	createReq = createReq.WithContext(context.WithValue(createReq.Context(), adminUserContextKey, &store.AdminUser{ID: "adm-a", Scope: "tenant", TenantID: "tenant_a"}))
	createRec := httptest.NewRecorder()
	CreateLLMServiceCardHandler(system, nil).ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusOK {
		t.Fatalf("create status = %d, body = %s", createRec.Code, createRec.Body.String())
	}

	tenantAReg, err := llmservice.LoadRegistry(ctx, tenantASettings)
	if err != nil {
		t.Fatal(err)
	}
	tenantBReg, err := llmservice.LoadRegistry(ctx, tenantBSettings)
	if err != nil {
		t.Fatal(err)
	}
	defaultReg, err := llmservice.LoadRegistry(ctx, system)
	if err != nil {
		t.Fatal(err)
	}
	if len(tenantAReg.Cards) != 2 {
		t.Fatalf("tenant A cards = %d, want 2", len(tenantAReg.Cards))
	}
	if len(tenantBReg.Cards) != 1 || tenantBReg.Cards[0].ID != "card-tenant-b" {
		t.Fatalf("tenant B registry changed: %#v", tenantBReg.Cards)
	}
	if len(defaultReg.Cards) != 0 {
		t.Fatalf("default registry received tenant cards: %#v", defaultReg.Cards)
	}

	exportReq := httptest.NewRequest(http.MethodGet, "/api/admin/llm/service-cards/export?status=all&format=txt", nil)
	exportReq = exportReq.WithContext(context.WithValue(exportReq.Context(), adminUserContextKey, &store.AdminUser{ID: "adm-a", Scope: "tenant", TenantID: "tenant_a"}))
	exportRec := httptest.NewRecorder()
	ExportLLMServiceCardsHandler(system).ServeHTTP(exportRec, exportReq)
	if exportRec.Code != http.StatusOK {
		t.Fatalf("export status = %d, body = %s", exportRec.Code, exportRec.Body.String())
	}
	exportText := exportRec.Body.String()
	if !strings.Contains(exportText, "card-tenant-a") || strings.Contains(exportText, "card-tenant-b") {
		t.Fatalf("tenant A export leaked cards: %s", exportText)
	}
}

func TestUpdateLLMServicesAdminHandlerValidatesSecurityGroupsWithinTenant(t *testing.T) {
	ctx := context.Background()
	system := newTestLLMServiceSystemSettings()
	provider, err := sqlite.NewProvider(sqlite.Config{DSN: t.TempDir() + "/security-tenant.db"})
	if err != nil {
		t.Fatalf("new sqlite provider: %v", err)
	}
	t.Cleanup(func() { _ = provider.Close() })
	secStore := security.NewSecurityStore(provider.Write)
	if err := secStore.InitSchema(ctx); err != nil {
		t.Fatalf("init schema: %v", err)
	}
	ctxA := security.WithTenant(ctx, "tenant_a")
	ctxB := security.WithTenant(ctx, "tenant_b")
	if err := secStore.InitRootGroup(ctxA); err != nil {
		t.Fatalf("init tenant A root: %v", err)
	}
	if err := secStore.InitRootGroup(ctxB); err != nil {
		t.Fatalf("init tenant B root: %v", err)
	}
	secSvc := security.NewSecurityService(secStore, nil, nil)
	rootA, err := secStore.GetRootGroup(ctxA)
	if err != nil || rootA == nil {
		t.Fatalf("get tenant A root: %v", err)
	}
	rootB, err := secStore.GetRootGroup(ctxB)
	if err != nil || rootB == nil {
		t.Fatalf("get tenant B root: %v", err)
	}
	groupA, err := secSvc.CreateGroup(ctxA, "A Only", rootA.ID)
	if err != nil {
		t.Fatalf("create tenant A group: %v", err)
	}
	groupB, err := secSvc.CreateGroup(ctxB, "B Only", rootB.ID)
	if err != nil {
		t.Fatalf("create tenant B group: %v", err)
	}

	requestBody := func(groupID string) *bytes.Reader {
		return bytes.NewReader([]byte(fmt.Sprintf(`{"model_service_groups":[{"id":"coding-basic","name":"Coding Basic"}],"group_bindings":[{"group_id":%q,"service_group_ids":["coding-basic"]}]}`, groupID)))
	}
	req := httptest.NewRequest(http.MethodPut, "/api/admin/llm/services", requestBody(groupB.ID))
	req = req.WithContext(context.WithValue(req.Context(), adminUserContextKey, &store.AdminUser{ID: "adm-a", Scope: "tenant", TenantID: "tenant_a"}))
	rec := httptest.NewRecorder()
	UpdateLLMServicesAdminHandler(system, secSvc).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected cross-tenant group rejection, got %d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPut, "/api/admin/llm/services", requestBody(groupA.ID))
	req = req.WithContext(context.WithValue(req.Context(), adminUserContextKey, &store.AdminUser{ID: "adm-a", Scope: "tenant", TenantID: "tenant_a"}))
	rec = httptest.NewRecorder()
	UpdateLLMServicesAdminHandler(system, secSvc).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected tenant group accepted, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestUpdateLLMServicesAdminHandlerValidatesNewUserLimitCardGroups(t *testing.T) {
	system := newTestLLMServiceSystemSettings()
	body := []byte(`{
		"model_service_groups":[
			{"id":"welcome","name":"Welcome","access_policy":"free"},
			{"id":"recharge","name":"Recharge","access_policy":"grant_required"}
		],
		"default_new_user_limit_card":{"service_group_ids":["recharge"],"period_limits":{"five_hour":10,"daily":20}}
	}`)
	req := httptest.NewRequest(http.MethodPut, "/api/admin/llm/services", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	UpdateLLMServicesAdminHandler(system, nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "LLM_NEW_USER_LIMIT_CARD_GROUP_INVALID") {
		t.Fatalf("unexpected body: %s", rec.Body.String())
	}

	body = []byte(`{
		"model_service_groups":[{"id":"welcome","name":"Welcome","access_policy":"free"}],
		"default_new_user_limit_card":{"service_group_ids":["welcome"],"duration_days":0,"period_limits":{"five_hour":10,"daily":20}}
	}`)
	req = httptest.NewRequest(http.MethodPut, "/api/admin/llm/services", bytes.NewReader(body))
	rec = httptest.NewRecorder()
	UpdateLLMServicesAdminHandler(system, nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("binding-active group should be accepted: status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestUpdateLLMServicesAdminHandlerPreservesBenefitModeWhenOmitted(t *testing.T) {
	ctx := context.Background()
	system := newTestLLMServiceSystemSettings()
	if err := llmservice.SaveRegistry(ctx, system, &llmservice.Registry{
		ModelServiceGroups:        []llmservice.ModelServiceGroup{{ID: "welcome", Name: "Welcome", AccessPolicy: llmservice.AccessPolicyFree}},
		DefaultNewUserBenefitMode: llmservice.NewUserBenefitModeLimitCard,
	}); err != nil {
		t.Fatal(err)
	}

	body := []byte(`{"model_service_groups":[{"id":"welcome","name":"Welcome","access_policy":"free"}]}`)
	req := httptest.NewRequest(http.MethodPut, "/api/admin/llm/services", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	UpdateLLMServicesAdminHandler(system, nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	reg, err := llmservice.LoadRegistry(ctx, system)
	if err != nil {
		t.Fatal(err)
	}
	if reg.NewUserBenefitMode() != llmservice.NewUserBenefitModeLimitCard {
		t.Fatalf("omitted benefit mode reverted to %q", reg.NewUserBenefitMode())
	}
}

func TestCreateLLMServiceCardHandlerAuditsGlobalAdminTenantQuery(t *testing.T) {
	ctx := context.Background()
	system := newTestLLMServiceSystemSettings()
	if err := llmservice.SaveRegistry(ctx, scopedSystemSettingsForTenant("tenant_a", system), &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{{ID: "coding-basic", Name: "Coding Basic"}},
	}); err != nil {
		t.Fatal(err)
	}
	audit := &testAdminAuditRepo{}
	body := []byte(`{"label":"Tenant Audit","service_group_ids":["coding-basic"],"duration_days":30,"credits":100,"count":1}`)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/llm/service-cards?tenant_id=tenant_a", bytes.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), adminUserContextKey, &store.AdminUser{ID: "global-admin", Scope: "global"}))
	rec := httptest.NewRecorder()

	CreateLLMServiceCardHandler(system, audit).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if len(audit.logs) != 1 {
		t.Fatalf("expected 1 audit log, got %d", len(audit.logs))
	}
	if audit.logs[0].TenantID != "tenant_a" || audit.logs[0].AdminUserID != "global-admin" {
		t.Fatalf("unexpected audit tenant/admin: %#v", audit.logs[0])
	}
}

func TestListLLMServiceCardsHandlerClampsPageToLastPage(t *testing.T) {
	now := time.Now().UTC()
	system := newTestLLMServiceSystemSettings()
	if err := llmservice.SaveRegistry(context.Background(), system, &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{{ID: "coding-basic", Name: "Coding Basic"}},
		Cards: []llmservice.RechargeCard{
			{ID: "card-1", Label: "Alpha", ServiceGroupIDs: []string{"coding-basic"}, DurationDays: 30, Credits: 100, CreatedAt: now},
			{ID: "card-2", Label: "Beta", ServiceGroupIDs: []string{"coding-basic"}, DurationDays: 30, Credits: 100, CreatedAt: now},
			{ID: "card-3", Label: "Gamma", ServiceGroupIDs: []string{"coding-basic"}, DurationDays: 30, Credits: 100, CreatedAt: now},
		},
	}); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/admin/llm/service-cards?status=all&page=9&page_size=2", nil)
	rec := httptest.NewRecorder()
	ListLLMServiceCardsHandler(system).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
		Total    int `json:"total"`
		Page     int `json:"page"`
		PageSize int `json:"page_size"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Page != 2 {
		t.Fatalf("expected page to clamp to 2, got %d", resp.Page)
	}
	if len(resp.Items) != 1 || resp.Items[0].ID != "card-3" {
		t.Fatalf("expected last page item, got %#v", resp.Items)
	}
}

func TestListLLMServiceCardsHandlerRejectsInvalidStatus(t *testing.T) {
	system := newTestLLMServiceSystemSettings()
	if err := llmservice.SaveRegistry(context.Background(), system, &llmservice.Registry{}); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/admin/llm/service-cards?status=archived", nil)
	rec := httptest.NewRecorder()
	ListLLMServiceCardsHandler(system).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestExportLLMServiceCardsHandlerFiltersStatusAndFormat(t *testing.T) {
	now := time.Now().UTC()
	system := newTestLLMServiceSystemSettings()
	if err := llmservice.SaveRegistry(context.Background(), system, &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{{ID: "coding-basic", Name: "Coding Basic"}},
		Cards: []llmservice.RechargeCard{
			{ID: "card-unused", Label: "Unused", ServiceGroupIDs: []string{"coding-basic"}, DurationDays: 30, Credits: 100, CreatedAt: now},
			{ID: "card-used", Label: "Used", ServiceGroupIDs: []string{"coding-basic"}, DurationDays: 30, Credits: 100, CreatedAt: now, RedeemedByEmail: "user@example.com", RedeemedAt: &now},
		},
	}); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/admin/llm/service-cards/export?status=redeemed&format=csv", nil)
	rec := httptest.NewRecorder()
	ExportLLMServiceCardsHandler(system).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	body, _ := io.ReadAll(rec.Body)
	text := string(body)
	if !strings.Contains(text, "card-used") || strings.Contains(text, "card-unused") {
		t.Fatalf("unexpected export body: %s", text)
	}
	if got := rec.Header().Get("Content-Type"); !strings.Contains(got, "text/csv") {
		t.Fatalf("content type = %q", got)
	}
}

func TestExportLLMServiceCardsHandlerAppliesSearchFilter(t *testing.T) {
	now := time.Now().UTC()
	system := newTestLLMServiceSystemSettings()
	if err := llmservice.SaveRegistry(context.Background(), system, &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{{ID: "coding-basic", Name: "Coding Basic"}},
		Cards: []llmservice.RechargeCard{
			{ID: "card-alpha", Label: "Alpha Campaign", ServiceGroupIDs: []string{"coding-basic"}, DurationDays: 30, Credits: 100, CreatedAt: now},
			{ID: "card-beta", Label: "Beta Campaign", ServiceGroupIDs: []string{"coding-basic"}, DurationDays: 30, Credits: 100, CreatedAt: now, RedeemedByEmail: "search@example.com", RedeemedAt: &now},
		},
	}); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/admin/llm/service-cards/export?status=all&format=txt&search=search@example.com", nil)
	rec := httptest.NewRecorder()
	ExportLLMServiceCardsHandler(system).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	body, _ := io.ReadAll(rec.Body)
	text := string(body)
	if !strings.Contains(text, "card-beta") || strings.Contains(text, "card-alpha") {
		t.Fatalf("unexpected export body: %s", text)
	}
}

func TestExportSelectedLLMServiceCardsHandlerReturnsRequestedCards(t *testing.T) {
	now := time.Now().UTC()
	system := newTestLLMServiceSystemSettings()
	if err := llmservice.SaveRegistry(context.Background(), system, &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{{ID: "coding-basic", Name: "Coding Basic"}},
		Cards: []llmservice.RechargeCard{
			{ID: "card-alpha", Label: "Alpha", ServiceGroupIDs: []string{"coding-basic"}, DurationDays: 30, Credits: 100, CreatedAt: now},
			{ID: "card-beta", Label: "Beta", ServiceGroupIDs: []string{"coding-basic"}, DurationDays: 30, Credits: 100, CreatedAt: now, RedeemedByEmail: "user@example.com", RedeemedAt: &now},
		},
	}); err != nil {
		t.Fatal(err)
	}

	body := []byte(`{"ids":["card-beta"],"format":"csv"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/llm/service-cards/export-selected", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	ExportSelectedLLMServiceCardsHandler(system).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	payload, _ := io.ReadAll(rec.Body)
	text := string(payload)
	if !strings.Contains(text, "card-beta") || strings.Contains(text, "card-alpha") {
		t.Fatalf("unexpected export body: %s", text)
	}
}

func TestExportSelectedLLMServiceCardsHandlerRejectsMissingMatches(t *testing.T) {
	system := newTestLLMServiceSystemSettings()
	if err := llmservice.SaveRegistry(context.Background(), system, &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{{ID: "coding-basic", Name: "Coding Basic"}},
	}); err != nil {
		t.Fatal(err)
	}

	body := []byte(`{"ids":["missing-card"],"format":"csv"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/llm/service-cards/export-selected", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	ExportSelectedLLMServiceCardsHandler(system).ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestUpdateLLMServicesAdminHandlerPreservesCardsWhenOmitted(t *testing.T) {
	now := time.Now().UTC()
	system := newTestLLMServiceSystemSettings()
	if err := llmservice.SaveRegistry(context.Background(), system, &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{{ID: "coding-basic", Name: "Coding Basic"}},
		GroupBindings:      []llmservice.GroupBinding{{GroupID: "ops", ServiceGroupIDs: []string{"coding-basic"}}},
		Cards: []llmservice.RechargeCard{
			{ID: "card-1", CodeHash: "hash-1", Label: "Alpha", ServiceGroupIDs: []string{"coding-basic"}, DurationDays: 30, Credits: 100, CreatedAt: now},
			{ID: "card-2", CodeHash: "hash-2", Label: "Beta", ServiceGroupIDs: []string{"coding-basic"}, DurationDays: 45, Credits: 200, CreatedAt: now},
		},
	}); err != nil {
		t.Fatal(err)
	}

	body := []byte(`{"model_service_groups":[{"id":"coding-basic","name":"Coding Basic Updated"}],"group_bindings":[{"group_id":"ops","service_group_ids":["coding-basic"]}],"user_bindings":[],"grants":[],"default_new_user_service_groups":["coding-basic"],"default_new_user_duration_days":30,"tokens_per_credit":10000}`)
	req := httptest.NewRequest(http.MethodPut, "/api/admin/llm/services", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	UpdateLLMServicesAdminHandler(system, nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	saved, err := llmservice.LoadRegistry(context.Background(), system)
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Cards) != 2 {
		t.Fatalf("expected 2 cards to remain, got %d", len(saved.Cards))
	}
	if saved.Cards[0].CodeHash != "hash-1" || saved.Cards[1].CodeHash != "hash-2" {
		t.Fatalf("expected existing card hashes to be preserved, got %#v", saved.Cards)
	}
	updated := false
	for _, group := range saved.ModelServiceGroups {
		if group.ID == "coding-basic" && group.Name == "Coding Basic Updated" {
			updated = true
			break
		}
	}
	if !updated {
		t.Fatalf("expected coding-basic group to update, got %#v", saved.ModelServiceGroups)
	}
}

func TestUpdateLLMServicesAdminHandlerPreservesGrantsWhenOmitted(t *testing.T) {
	now := time.Now().UTC()
	system := newTestLLMServiceSystemSettings()
	if err := llmservice.SaveRegistry(context.Background(), system, &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{{ID: "coding-basic", Name: "Coding Basic"}},
		Grants: []llmservice.Grant{
			{ID: "grant-1", Email: "user@example.com", ServiceGroupID: "coding-basic", Source: "card", StartsAt: now, ExpiresAt: now.Add(24 * time.Hour), CreatedAt: now, CreditsTotal: 100},
		},
	}); err != nil {
		t.Fatal(err)
	}

	body := []byte(`{"model_service_groups":[{"id":"coding-basic","name":"Coding Basic Updated"}],"group_bindings":[],"user_bindings":[],"default_new_user_service_groups":["coding-basic"],"default_new_user_duration_days":30,"tokens_per_credit":10000}`)
	req := httptest.NewRequest(http.MethodPut, "/api/admin/llm/services", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	UpdateLLMServicesAdminHandler(system, nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	saved, err := llmservice.LoadRegistry(context.Background(), system)
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Grants) != 1 || saved.Grants[0].ID != "grant-1" || saved.Grants[0].Email != "user@example.com" {
		t.Fatalf("expected existing grants to be preserved, got %#v", saved.Grants)
	}
}

func TestUpdateLLMServicesAdminHandlerCascadeCleansOrphanedReferencesOnGroupDeletion(t *testing.T) {
	now := time.Now().UTC()
	system := newTestLLMServiceSystemSettings()
	if err := llmservice.SaveRegistry(context.Background(), system, &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{{ID: "paid", Name: "Paid"}},
		Cards: []llmservice.RechargeCard{
			{ID: "card-1", CodeHash: "hash-1", Label: "Paid Card", ServiceGroupIDs: []string{"paid"}, DurationDays: 30, Credits: 100, CreatedAt: now},
		},
		Grants: []llmservice.Grant{
			{ID: "grant-1", Email: "user@example.com", ServiceGroupID: "paid", Source: "card", StartsAt: now, ExpiresAt: now.Add(24 * time.Hour), CreatedAt: now, CreditsTotal: 100},
		},
	}); err != nil {
		t.Fatal(err)
	}

	// Remove "paid" from model_service_groups — cascade cleanup should remove orphaned card refs and grants.
	body := []byte(`{"model_service_groups":[{"id":"default","name":"Default (No Model Access)"}],"group_bindings":[],"user_bindings":[],"default_new_user_service_groups":["default"],"default_new_user_duration_days":30,"tokens_per_credit":10000}`)
	req := httptest.NewRequest(http.MethodPut, "/api/admin/llm/services", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	UpdateLLMServicesAdminHandler(system, nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s; expected cascade cleanup to succeed", rec.Code, rec.Body.String())
	}

	// Verify the saved registry has cleaned up the orphaned references.
	saved, err := llmservice.LoadRegistry(context.Background(), system)
	if err != nil {
		t.Fatalf("load registry: %v", err)
	}
	// Card should have its ServiceGroupIDs cleaned (empty).
	for _, card := range saved.Cards {
		if card.ID == "card-1" {
			for _, sgID := range card.ServiceGroupIDs {
				if strings.EqualFold(sgID, "paid") {
					t.Fatalf("card-1 still references deleted group 'paid': %#v", card.ServiceGroupIDs)
				}
			}
		}
	}
	// Grant referencing "paid" should be removed.
	for _, grant := range saved.Grants {
		if strings.EqualFold(grant.ServiceGroupID, "paid") {
			t.Fatalf("grant still references deleted group 'paid': %#v", grant)
		}
	}
}

func TestUpdateLLMServicesAdminHandlerRejectsDuplicateServiceGroupIDs(t *testing.T) {
	system := newTestLLMServiceSystemSettings()
	if err := llmservice.SaveRegistry(context.Background(), system, &llmservice.Registry{}); err != nil {
		t.Fatal(err)
	}

	body := []byte(`{"model_service_groups":[{"id":"ops","name":"Ops"},{"id":"OPS","name":"Ops Duplicate"}],"group_bindings":[],"user_bindings":[],"default_new_user_service_groups":["default"],"default_new_user_duration_days":30,"tokens_per_credit":10000}`)
	req := httptest.NewRequest(http.MethodPut, "/api/admin/llm/services", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	UpdateLLMServicesAdminHandler(system, nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "duplicate model service group id") {
		t.Fatalf("expected duplicate service group validation error, got %s", rec.Body.String())
	}
}

func TestDeleteLLMServiceCardHandlerDeletesCardsAndLinkedGrants(t *testing.T) {
	now := time.Now().UTC()
	system := newTestLLMServiceSystemSettings()
	if err := llmservice.SaveRegistry(context.Background(), system, &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{{ID: "coding-basic", Name: "Coding Basic"}},
		Cards: []llmservice.RechargeCard{
			{ID: "card-unused", Label: "Unused", ServiceGroupIDs: []string{"coding-basic"}, DurationDays: 30, Credits: 100, CreatedAt: now},
			{ID: "card-used", Label: "Used", ServiceGroupIDs: []string{"coding-basic"}, DurationDays: 30, Credits: 100, CreatedAt: now, RedeemedByEmail: "user@example.com", RedeemedAt: &now},
		},
		Grants: []llmservice.Grant{{ID: "grant-used", Email: "user@example.com", ServiceGroupID: "coding-basic", Source: "card", CardID: "card-used", StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour), CreatedAt: now, CreditsTotal: 100}},
	}); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/admin/llm/service-cards/card-unused", nil)
	req.SetPathValue("id", "card-unused")
	rec := httptest.NewRecorder()
	DeleteLLMServiceCardHandler(system, nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete unused status = %d, body = %s", rec.Code, rec.Body.String())
	}
	saved, err := llmservice.LoadRegistry(context.Background(), system)
	if err != nil {
		t.Fatal(err)
	}
	if card, _ := saved.FindCardByID("card-unused"); card != nil {
		t.Fatal("expected unused card to be deleted")
	}

	req = httptest.NewRequest(http.MethodDelete, "/api/admin/llm/service-cards/card-used", nil)
	req.SetPathValue("id", "card-used")
	rec = httptest.NewRecorder()
	DeleteLLMServiceCardHandler(system, nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete redeemed status = %d, body = %s", rec.Code, rec.Body.String())
	}
	saved, err = llmservice.LoadRegistry(context.Background(), system)
	if err != nil {
		t.Fatal(err)
	}
	if card, _ := saved.FindCardByID("card-used"); card != nil {
		t.Fatal("expected redeemed card to be deleted")
	}
	if len(saved.Grants) != 0 {
		t.Fatalf("expected linked grant to be deleted, got %d", len(saved.Grants))
	}
}

func TestDeleteLLMServiceCardsBatchHandlerDeletesSelectedCardsAndLinkedGrants(t *testing.T) {
	now := time.Now().UTC()
	system := newTestLLMServiceSystemSettings()
	if err := llmservice.SaveRegistry(context.Background(), system, &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{{ID: "coding-basic", Name: "Coding Basic"}},
		Cards: []llmservice.RechargeCard{
			{ID: "card-unused-a", Label: "Unused A", ServiceGroupIDs: []string{"coding-basic"}, DurationDays: 30, Credits: 100, CreatedAt: now},
			{ID: "card-unused-b", Label: "Unused B", ServiceGroupIDs: []string{"coding-basic"}, DurationDays: 30, Credits: 100, CreatedAt: now},
			{ID: "card-used", Label: "Used", ServiceGroupIDs: []string{"coding-basic"}, DurationDays: 30, Credits: 100, CreatedAt: now, RedeemedByEmail: "user@example.com", RedeemedAt: &now},
		},
		Grants: []llmservice.Grant{{ID: "grant-used", Email: "user@example.com", ServiceGroupID: "coding-basic", Source: "card", CardID: "card-used", StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour), CreatedAt: now, CreditsTotal: 100}},
	}); err != nil {
		t.Fatal(err)
	}
	body := []byte(`{"ids":["card-unused-a","card-used","missing-card"]}`)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/llm/service-cards/delete-batch", bytes.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), adminUserContextKey, &store.AdminUser{ID: "adm-2"}))
	rec := httptest.NewRecorder()
	audit := &testAdminAuditRepo{}
	DeleteLLMServiceCardsBatchHandler(system, audit).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("batch delete status = %d, body = %s", rec.Code, rec.Body.String())
	}
	saved, err := llmservice.LoadRegistry(context.Background(), system)
	if err != nil {
		t.Fatal(err)
	}
	if card, _ := saved.FindCardByID("card-unused-a"); card != nil {
		t.Fatal("expected card-unused-a to be deleted")
	}
	if card, _ := saved.FindCardByID("card-unused-b"); card == nil {
		t.Fatal("expected card-unused-b to remain")
	}
	if card, _ := saved.FindCardByID("card-used"); card != nil {
		t.Fatal("expected redeemed card to be deleted")
	}
	if len(saved.Grants) != 0 {
		t.Fatalf("expected linked grants to be deleted, got %d", len(saved.Grants))
	}
	if len(audit.logs) != 1 {
		t.Fatalf("expected 1 audit log, got %d", len(audit.logs))
	}
	if audit.logs[0].Action != "llm.service_card.delete_batch" {
		t.Fatalf("unexpected audit action: %s", audit.logs[0].Action)
	}
}

func TestDeleteLLMServiceGrantHandlerDeletesGrant(t *testing.T) {
	now := time.Now().UTC()
	system := newTestLLMServiceSystemSettings()
	if err := llmservice.SaveRegistry(context.Background(), system, &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{{ID: "coding-basic", Name: "Coding Basic"}},
		Grants:             []llmservice.Grant{{ID: "grant-1", Email: "user@example.com", ServiceGroupID: "coding-basic", Source: "card", CardID: "card-used", StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour), CreatedAt: now, CreditsTotal: 100}},
	}); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/admin/llm/service-grants/grant-1", nil)
	req.SetPathValue("id", "grant-1")
	rec := httptest.NewRecorder()
	DeleteLLMServiceGrantHandler(system, nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete grant status = %d, body = %s", rec.Code, rec.Body.String())
	}
	saved, err := llmservice.LoadRegistry(context.Background(), system)
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Grants) != 0 {
		t.Fatalf("expected grant to be deleted, got %d", len(saved.Grants))
	}
}

func TestCreateLLMServiceCardHandlerPersistsEncryptedCode(t *testing.T) {
	ctx := context.Background()
	system := newTestLLMServiceSystemSettings()
	if err := llmservice.SaveRegistry(ctx, system, &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{{ID: "coding-basic", Name: "Coding Basic"}},
	}); err != nil {
		t.Fatal(err)
	}

	body := []byte(`{"label":"Persist","service_group_ids":["coding-basic"],"duration_days":30,"credits":100,"count":2}`)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/llm/service-cards", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	CreateLLMServiceCardHandler(system, nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	// Parse response codes
	var resp struct {
		Cards []struct {
			Code string `json:"code"`
		} `json:"cards"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Cards) != 2 {
		t.Fatalf("expected 2 cards, got %d", len(resp.Cards))
	}

	// Verify encrypted code is persisted and can be decrypted back
	saved, err := llmservice.LoadRegistry(ctx, system)
	if err != nil {
		t.Fatal(err)
	}
	for i, card := range saved.Cards {
		if card.EncryptedCode == "" {
			t.Fatalf("card %d: EncryptedCode is empty", i)
		}
		plain := card.PlainCode()
		if plain == "" {
			t.Fatalf("card %d: PlainCode() returned empty", i)
		}
		if plain != llmservice.NormalizeCardCode(resp.Cards[i].Code) {
			t.Fatalf("card %d: PlainCode() = %q, want %q", i, plain, resp.Cards[i].Code)
		}
	}
}

func TestListLLMServiceCardsHandlerReturnsCode(t *testing.T) {
	ctx := context.Background()
	system := newTestLLMServiceSystemSettings()
	if err := llmservice.SaveRegistry(ctx, system, &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{{ID: "coding-basic", Name: "Coding Basic"}},
	}); err != nil {
		t.Fatal(err)
	}

	// Create a card first
	createBody := []byte(`{"label":"ListTest","service_group_ids":["coding-basic"],"duration_days":30,"credits":100,"count":1}`)
	createReq := httptest.NewRequest(http.MethodPost, "/api/admin/llm/service-cards", bytes.NewReader(createBody))
	createRec := httptest.NewRecorder()
	CreateLLMServiceCardHandler(system, nil).ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusOK {
		t.Fatalf("create status = %d", createRec.Code)
	}
	var createResp struct {
		Cards []struct {
			Code string `json:"code"`
		} `json:"cards"`
	}
	if err := json.Unmarshal(createRec.Body.Bytes(), &createResp); err != nil {
		t.Fatal(err)
	}
	expectedCode := createResp.Cards[0].Code

	// List cards and verify code is returned
	listReq := httptest.NewRequest(http.MethodGet, "/api/admin/llm/service-cards?status=all", nil)
	listRec := httptest.NewRecorder()
	ListLLMServiceCardsHandler(system).ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status = %d, body = %s", listRec.Code, listRec.Body.String())
	}
	var listResp struct {
		Items []struct {
			ID   string `json:"id"`
			Code string `json:"code"`
		} `json:"items"`
	}
	if err := json.Unmarshal(listRec.Body.Bytes(), &listResp); err != nil {
		t.Fatal(err)
	}
	if len(listResp.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(listResp.Items))
	}
	if listResp.Items[0].Code != expectedCode {
		t.Fatalf("list code = %q, want %q", listResp.Items[0].Code, expectedCode)
	}
}

func TestExportLLMServiceCardsIncludesCode(t *testing.T) {
	ctx := context.Background()
	system := newTestLLMServiceSystemSettings()
	if err := llmservice.SaveRegistry(ctx, system, &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{{ID: "coding-basic", Name: "Coding Basic"}},
	}); err != nil {
		t.Fatal(err)
	}

	// Create a card
	createBody := []byte(`{"label":"ExportTest","service_group_ids":["coding-basic"],"duration_days":30,"credits":100,"count":1}`)
	createReq := httptest.NewRequest(http.MethodPost, "/api/admin/llm/service-cards", bytes.NewReader(createBody))
	createRec := httptest.NewRecorder()
	CreateLLMServiceCardHandler(system, nil).ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusOK {
		t.Fatalf("create status = %d", createRec.Code)
	}
	var createResp struct {
		Cards []struct {
			Code string `json:"code"`
		} `json:"cards"`
	}
	if err := json.Unmarshal(createRec.Body.Bytes(), &createResp); err != nil {
		t.Fatal(err)
	}
	expectedCode := createResp.Cards[0].Code

	// Export as CSV and verify code column is present
	exportReq := httptest.NewRequest(http.MethodGet, "/api/admin/llm/service-cards/export?status=all&format=csv", nil)
	exportRec := httptest.NewRecorder()
	ExportLLMServiceCardsHandler(system).ServeHTTP(exportRec, exportReq)
	if exportRec.Code != http.StatusOK {
		t.Fatalf("export status = %d, body = %s", exportRec.Code, exportRec.Body.String())
	}
	csvBody, _ := io.ReadAll(exportRec.Body)
	csvText := string(csvBody)
	if !strings.Contains(csvText, expectedCode) {
		t.Fatalf("export CSV does not contain card code %q:\n%s", expectedCode, csvText)
	}
	// Verify "code" is in the header
	lines := strings.Split(csvText, "\n")
	if len(lines) < 1 || !strings.Contains(lines[0], "code") {
		t.Fatalf("export CSV header missing 'code' column: %s", lines[0])
	}

	// Export as TXT and verify code is present
	exportTxtReq := httptest.NewRequest(http.MethodGet, "/api/admin/llm/service-cards/export?status=all&format=txt", nil)
	exportTxtRec := httptest.NewRecorder()
	ExportLLMServiceCardsHandler(system).ServeHTTP(exportTxtRec, exportTxtReq)
	if exportTxtRec.Code != http.StatusOK {
		t.Fatalf("export txt status = %d", exportTxtRec.Code)
	}
	txtBody, _ := io.ReadAll(exportTxtRec.Body)
	txtText := string(txtBody)
	if !strings.Contains(txtText, expectedCode) {
		t.Fatalf("export TXT does not contain card code %q:\n%s", expectedCode, txtText)
	}
}

func TestUpdateLLMServicesAdminHandlerWritesBindingAudit(t *testing.T) {
	ctx := context.Background()
	system := newTestLLMServiceSystemSettings()
	if err := llmservice.SaveRegistry(ctx, system, &llmservice.Registry{
		ModelServiceGroups:          []llmservice.ModelServiceGroup{{ID: "coding-basic", Name: "Coding Basic"}, {ID: "ops-pro", Name: "Ops Pro"}},
		DefaultNewUserServiceGroups: []string{"coding-basic"},
	}); err != nil {
		t.Fatal(err)
	}
	audit := &testAdminAuditRepo{}
	body := []byte(`{"model_service_groups":[{"id":"coding-basic","name":"Coding Basic"},{"id":"ops-pro","name":"Ops Pro"}],"global_service_group_ids":["ops-pro"],"group_bindings":[{"group_id":"ops","service_group_ids":["ops-pro"]}],"user_bindings":[{"email":"lead@example.com","service_group_ids":["ops-pro"]}],"default_new_user_service_groups":["coding-basic"],"default_new_user_benefit_mode":"limit_card"}`)
	req := httptest.NewRequest(http.MethodPut, "/api/admin/llm/services", bytes.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), adminUserContextKey, &store.AdminUser{ID: "adm-bind"}))
	rec := httptest.NewRecorder()

	UpdateLLMServicesAdminHandler(system, nil, audit).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if len(audit.logs) != 1 {
		t.Fatalf("expected 1 audit log, got %d", len(audit.logs))
	}
	if audit.logs[0].Action != "llm.service_bindings.update" || audit.logs[0].AdminUserID != "adm-bind" {
		t.Fatalf("unexpected audit log: %#v", audit.logs[0])
	}
	if !strings.Contains(audit.logs[0].PayloadJSON, "global_service_group_ids") || !strings.Contains(audit.logs[0].PayloadJSON, "lead@example.com") || !strings.Contains(audit.logs[0].PayloadJSON, "ops-pro") {
		t.Fatalf("audit payload missing binding details: %s", audit.logs[0].PayloadJSON)
	}
	if !strings.Contains(audit.logs[0].PayloadJSON, `"default_new_user_benefit_mode":"limit_card"`) || !strings.Contains(rec.Body.String(), `"default_new_user_benefit_mode":"limit_card"`) {
		t.Fatalf("benefit mode was not persisted and exposed: audit=%s response=%s", audit.logs[0].PayloadJSON, rec.Body.String())
	}
}

func TestBuildLLMServiceBindingAuditSnapshotDoesNotMutateRegistry(t *testing.T) {
	reg := &llmservice.Registry{
		ModelServiceGroups:          []llmservice.ModelServiceGroup{{ID: " team ", Name: " Team "}},
		GlobalServiceGroupIDs:       []string{" team "},
		DefaultNewUserServiceGroups: []string{" team "},
		GroupBindings:               []llmservice.GroupBinding{{GroupID: " ops ", ServiceGroupIDs: []string{" team "}}},
		UserBindings:                []llmservice.UserBinding{{Email: " Lead@Example.COM ", ServiceGroupIDs: []string{" team "}}},
	}

	snapshot := buildLLMServiceBindingAuditSnapshot(reg)
	if len(snapshot.UserBindings) != 1 || snapshot.UserBindings[0].Email != "lead@example.com" {
		t.Fatalf("snapshot did not normalize copied user binding: %#v", snapshot.UserBindings)
	}
	if reg.ModelServiceGroups[0].ID != " team " || reg.GlobalServiceGroupIDs[0] != " team " || reg.UserBindings[0].Email != " Lead@Example.COM " {
		t.Fatalf("audit snapshot mutated source registry: %#v", reg)
	}
}
