package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/llmpool"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/llmservice"
)

func TestAdminCopyLLMProvider(t *testing.T) {
	ctx := context.Background()
	svc := llmservice.NewService(&llmDeleteTestSettings{data: map[string]string{}})
	if err := svc.AddProvider(ctx, llmpool.ProviderConfig{
		ID:     "OpenCode-1",
		Name:   "OpenCode-1",
		APIURL: "https://opencode.ai/zen/go/v1",
		APIKey: "sk-secret",
	}); err != nil {
		t.Fatalf("add provider: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/admin/llm/providers/OpenCode-1/copy", nil)
	req.SetPathValue("id", "OpenCode-1")
	rec := httptest.NewRecorder()
	adminCopyLLMProvider(svc)(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "sk-secret") {
		t.Fatalf("copy response leaked the api key: %s", rec.Body.String())
	}
	var payload struct {
		Status  string `json:"status"`
		ID      string `json:"id"`
		Name    string `json:"name"`
		ArrayID string `json:"array_id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload.Status != "ok" || payload.ID != "OpenCode-1-copy" || payload.Name != "OpenCode-1（副本）" || payload.ArrayID != "OpenCode-1" {
		t.Fatalf("payload = %#v", payload)
	}
	got, err := svc.GetProvider(ctx, payload.ID)
	if err != nil || got == nil || got.APIKey != "sk-secret" || got.APIURL != "https://opencode.ai/zen/go/v1" {
		t.Fatalf("stored copy = %#v err=%v", got, err)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/admin/llm/providers", nil)
	rec = httptest.NewRecorder()
	adminListLLMProviders(svc)(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "sk-secret") {
		t.Fatalf("list leaked the copied api key: %s", rec.Body.String())
	}
	var listed struct {
		Providers []struct {
			ID        string `json:"id"`
			HasAPIKey bool   `json:"has_api_key"`
		} `json:"providers"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	var sawCopy bool
	for _, item := range listed.Providers {
		if item.ID != payload.ID {
			continue
		}
		sawCopy = true
		if !item.HasAPIKey {
			t.Fatal("copied provider list row has no api key")
		}
	}
	if !sawCopy {
		t.Fatalf("copied provider missing from list: %#v", listed.Providers)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/admin/llm/providers/missing/copy", nil)
	req.SetPathValue("id", "missing")
	rec = httptest.NewRecorder()
	adminCopyLLMProvider(svc)(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing provider status = %d body=%s", rec.Code, rec.Body.String())
	}

	if err := svc.EnsureTokenBankArrays(ctx); err != nil {
		t.Fatalf("arrays: %v", err)
	}
	if err := svc.AddProvider(ctx, llmpool.ProviderConfig{
		ID: "bank-row", Name: "bank-row", APIURL: "https://api.example/v1", ArrayID: llmservice.TokenBankArrayMid,
	}); err != nil {
		t.Fatalf("add bank row: %v", err)
	}
	req = httptest.NewRequest(http.MethodPost, "/api/admin/llm/providers/bank-row/copy", nil)
	req.SetPathValue("id", "bank-row")
	rec = httptest.NewRecorder()
	adminCopyLLMProvider(svc)(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("protected copy status = %d body=%s", rec.Code, rec.Body.String())
	}
}
