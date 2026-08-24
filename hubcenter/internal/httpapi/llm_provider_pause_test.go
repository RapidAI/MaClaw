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

func TestAdminSetLLMProviderPaused(t *testing.T) {
	ctx := context.Background()
	svc := llmservice.NewService(&llmDeleteTestSettings{data: map[string]string{}})
	if err := svc.AddProvider(ctx, llmpool.ProviderConfig{
		ID:     "deepseek",
		Name:   "deepseek",
		APIURL: "https://api.deepseek.com/v1",
	}); err != nil {
		t.Fatalf("add provider: %v", err)
	}

	req := httptest.NewRequest(http.MethodPut, "/api/admin/llm/providers/deepseek/paused", strings.NewReader(`{"paused":true}`))
	req.SetPathValue("id", "deepseek")
	rec := httptest.NewRecorder()
	adminSetLLMProviderPaused(svc)(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Status string `json:"status"`
		Paused bool   `json:"paused"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload.Status != "ok" || !payload.Paused {
		t.Fatalf("payload = %#v", payload)
	}
	got, err := svc.GetProvider(ctx, "deepseek")
	if err != nil || got == nil || !got.Paused {
		t.Fatalf("stored provider = %#v err=%v", got, err)
	}

	req = httptest.NewRequest(http.MethodPut, "/api/admin/llm/providers/deepseek/paused", strings.NewReader(`{}`))
	req.SetPathValue("id", "deepseek")
	rec = httptest.NewRecorder()
	adminSetLLMProviderPaused(svc)(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing paused status = %d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPut, "/api/admin/llm/providers/missing/paused", strings.NewReader(`{"paused":true}`))
	req.SetPathValue("id", "missing")
	rec = httptest.NewRecorder()
	adminSetLLMProviderPaused(svc)(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing provider status = %d body=%s", rec.Code, rec.Body.String())
	}
}
