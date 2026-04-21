package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/llmservice"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

type testLLMServiceSystemSettings struct {
	data map[string]string
}

func newTestLLMServiceSystemSettings() *testLLMServiceSystemSettings {
	return &testLLMServiceSystemSettings{data: map[string]string{}}
}

func (s *testLLMServiceSystemSettings) Set(_ context.Context, key, valueJSON string) error {
	s.data[key] = valueJSON
	return nil
}

func (s *testLLMServiceSystemSettings) Get(_ context.Context, key string) (string, error) {
	return s.data[key], nil
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

func TestDeleteLLMServiceCardHandlerDeletesUnusedOnly(t *testing.T) {
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
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("delete redeemed status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestDeleteLLMServiceCardsBatchHandlerDeletesUnusedAndSkipsRedeemed(t *testing.T) {
	now := time.Now().UTC()
	system := newTestLLMServiceSystemSettings()
	if err := llmservice.SaveRegistry(context.Background(), system, &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{{ID: "coding-basic", Name: "Coding Basic"}},
		Cards: []llmservice.RechargeCard{
			{ID: "card-unused-a", Label: "Unused A", ServiceGroupIDs: []string{"coding-basic"}, DurationDays: 30, Credits: 100, CreatedAt: now},
			{ID: "card-unused-b", Label: "Unused B", ServiceGroupIDs: []string{"coding-basic"}, DurationDays: 30, Credits: 100, CreatedAt: now},
			{ID: "card-used", Label: "Used", ServiceGroupIDs: []string{"coding-basic"}, DurationDays: 30, Credits: 100, CreatedAt: now, RedeemedByEmail: "user@example.com", RedeemedAt: &now},
		},
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
	if card, _ := saved.FindCardByID("card-used"); card == nil {
		t.Fatal("expected redeemed card to remain")
	}
	if len(audit.logs) != 1 {
		t.Fatalf("expected 1 audit log, got %d", len(audit.logs))
	}
	if audit.logs[0].Action != "llm.service_card.delete_batch" {
		t.Fatalf("unexpected audit action: %s", audit.logs[0].Action)
	}
}
