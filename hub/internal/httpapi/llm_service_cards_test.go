package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/llmservice"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
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
	logs []*store.AdminAuditLog
}

func (r *testAdminAuditRepo) Create(_ context.Context, log *store.AdminAuditLog) error {
	clone := *log
	r.logs = append(r.logs, &clone)
	return nil
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

func TestCreateLLMServiceCardHandlerClampsNegativeCredits(t *testing.T) {
	ctx := context.Background()
	system := newTestLLMServiceSystemSettings()
	if err := llmservice.SaveRegistry(ctx, system, &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{{ID: "coding-basic", Name: "Coding Basic"}},
	}); err != nil {
		t.Fatal(err)
	}

	body := []byte(`{"service_group_ids":["coding-basic"],"duration_days":30,"credits":-10,"count":1}`)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/llm/service-cards", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	CreateLLMServiceCardHandler(system, nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Cards []struct {
			Credits float64 `json:"credits"`
		} `json:"cards"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Cards) != 1 || resp.Cards[0].Credits != 0 {
		t.Fatalf("expected response credits to be clamped to 0, got %#v", resp.Cards)
	}
	saved, err := llmservice.LoadRegistry(ctx, system)
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Cards) != 1 || saved.Cards[0].Credits != 0 {
		t.Fatalf("expected saved credits to be clamped to 0, got %#v", saved.Cards)
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
