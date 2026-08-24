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

func TestAdminSetLLMProviderSequence(t *testing.T) {
	ctx := context.Background()
	svc := llmservice.NewService(&llmDeleteTestSettings{data: map[string]string{}})
	if err := svc.AddProvider(ctx, llmpool.ProviderConfig{
		ID:     "deepseek",
		Name:   "deepseek",
		APIURL: "https://api.deepseek.com/v1",
	}); err != nil {
		t.Fatalf("add provider: %v", err)
	}

	req := httptest.NewRequest(http.MethodPut, "/api/admin/llm/providers/deepseek/sequence", strings.NewReader(`{"sequence":2}`))
	req.SetPathValue("id", "deepseek")
	rec := httptest.NewRecorder()
	adminSetLLMProviderSequence(svc)(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Status   string `json:"status"`
		Sequence int    `json:"sequence"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload.Status != "ok" || payload.Sequence != 2 {
		t.Fatalf("payload = %#v", payload)
	}
	got, err := svc.GetProvider(ctx, "deepseek")
	if err != nil || got == nil || got.Sequence != 2 {
		t.Fatalf("stored provider = %#v err=%v", got, err)
	}

	req = httptest.NewRequest(http.MethodPut, "/api/admin/llm/providers/deepseek/sequence", strings.NewReader(`{}`))
	req.SetPathValue("id", "deepseek")
	rec = httptest.NewRecorder()
	adminSetLLMProviderSequence(svc)(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing sequence status = %d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPut, "/api/admin/llm/providers/missing/sequence", strings.NewReader(`{"sequence":1}`))
	req.SetPathValue("id", "missing")
	rec = httptest.NewRecorder()
	adminSetLLMProviderSequence(svc)(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing provider status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAdminListLLMProvidersSortedBySequence(t *testing.T) {
	ctx := context.Background()
	svc := llmservice.NewService(&llmDeleteTestSettings{data: map[string]string{}})
	for _, provider := range []llmpool.ProviderConfig{
		{ID: "opencode-2", Name: "OpenCode-2", APIURL: "https://opencode.ai/api/go/v1", Sequence: 26},
		{ID: "deepseek", Name: "deepseek", APIURL: "https://api.deepseek.com/v1", Sequence: 2},
		{ID: "opencode-1", Name: "OpenCode-1", APIURL: "https://opencode.ai/api/go/v1", Sequence: 20},
		{ID: "nanjing", Name: "Nanjing", APIURL: "https://api.example.cn/v1", Sequence: 4},
	} {
		if err := svc.AddProvider(ctx, provider); err != nil {
			t.Fatalf("add %s: %v", provider.ID, err)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/admin/llm/providers", nil)
	rec := httptest.NewRecorder()
	adminListLLMProviders(svc)(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Providers []struct {
			ID          string `json:"id"`
			Sequence    int    `json:"sequence"`
			LBGroup     string `json:"lb_group"`
			LBGroupSize int    `json:"lb_group_size"`
			LBEligible  bool   `json:"lb_eligible"`
		} `json:"providers"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	want := []string{"deepseek", "nanjing", "opencode-1", "opencode-2"}
	if len(payload.Providers) != len(want) {
		t.Fatalf("providers = %#v, want %d items", payload.Providers, len(want))
	}
	for i, id := range want {
		if payload.Providers[i].ID != id {
			t.Fatalf("providers[%d] = %#v, want %s in sequence order", i, payload.Providers[i], id)
		}
		if payload.Providers[i].LBGroup != "x1" || payload.Providers[i].LBGroupSize != 4 || !payload.Providers[i].LBEligible {
			t.Fatalf("providers[%d] lb = %#v, want shared x1 group of 4", i, payload.Providers[i])
		}
	}
}

func TestAdminUpdateLLMProviderSequenceReordersList(t *testing.T) {
	ctx := context.Background()
	svc := llmservice.NewService(&llmDeleteTestSettings{data: map[string]string{}})
	if err := svc.AddProvider(ctx, llmpool.ProviderConfig{ID: "a", Name: "A", APIURL: "https://a.example/v1", Sequence: 1}); err != nil {
		t.Fatalf("add a: %v", err)
	}
	if err := svc.AddProvider(ctx, llmpool.ProviderConfig{ID: "b", Name: "B", APIURL: "https://b.example/v1", Sequence: 2}); err != nil {
		t.Fatalf("add b: %v", err)
	}

	req := httptest.NewRequest(http.MethodPut, "/api/admin/llm/providers/a", strings.NewReader(`{"name":"A","api_url":"https://a.example/v1","sequence":5}`))
	req.SetPathValue("id", "a")
	rec := httptest.NewRecorder()
	adminUpdateLLMProvider(svc)(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("update status = %d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/admin/llm/providers", nil)
	rec = httptest.NewRecorder()
	adminListLLMProviders(svc)(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d body=%s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Providers []struct {
			ID       string `json:"id"`
			Sequence int    `json:"sequence"`
		} `json:"providers"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(payload.Providers) != 2 || payload.Providers[0].ID != "b" || payload.Providers[0].Sequence != 2 {
		t.Fatalf("first = %#v, want b sequence 2", payload.Providers)
	}
	if payload.Providers[1].ID != "a" || payload.Providers[1].Sequence != 5 {
		t.Fatalf("second = %#v, want a sequence 5 after edit", payload.Providers[1])
	}
}

func TestAdminSetLLMProviderSequences(t *testing.T) {
	ctx := context.Background()
	svc := llmservice.NewService(&llmDeleteTestSettings{data: map[string]string{}})
	if err := svc.AddProvider(ctx, llmpool.ProviderConfig{ID: "a", Name: "A", APIURL: "https://a.example/v1", Sequence: 1}); err != nil {
		t.Fatalf("add a: %v", err)
	}
	if err := svc.AddProvider(ctx, llmpool.ProviderConfig{ID: "b", Name: "B", APIURL: "https://b.example/v1", Sequence: 2}); err != nil {
		t.Fatalf("add b: %v", err)
	}

	req := httptest.NewRequest(http.MethodPut, "/api/admin/llm/providers/sequences", strings.NewReader(`{"sequences":{"a":2,"b":1}}`))
	rec := httptest.NewRecorder()
	adminSetLLMProviderSequences(svc)(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/admin/llm/providers", nil)
	rec = httptest.NewRecorder()
	adminListLLMProviders(svc)(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d body=%s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Providers []struct {
			ID       string `json:"id"`
			Sequence int    `json:"sequence"`
		} `json:"providers"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(payload.Providers) != 2 || payload.Providers[0].ID != "b" || payload.Providers[0].Sequence != 1 {
		t.Fatalf("first = %#v, want b sequence 1", payload.Providers)
	}
	if payload.Providers[1].ID != "a" || payload.Providers[1].Sequence != 2 {
		t.Fatalf("second = %#v, want a sequence 2", payload.Providers[1])
	}

	req = httptest.NewRequest(http.MethodPut, "/api/admin/llm/providers/sequences", strings.NewReader(`{}`))
	rec = httptest.NewRecorder()
	adminSetLLMProviderSequences(svc)(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty sequences status = %d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPut, "/api/admin/llm/providers/sequences", strings.NewReader(`{"sequences":{"missing":1}}`))
	rec = httptest.NewRecorder()
	adminSetLLMProviderSequences(svc)(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing provider status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestLLMProviderSequencesRouteIsNotProviderID(t *testing.T) {
	mux := http.NewServeMux()
	var sequencesHit, updateHit bool
	mux.HandleFunc("PUT /api/admin/llm/providers/sequences", func(http.ResponseWriter, *http.Request) {
		sequencesHit = true
	})
	mux.HandleFunc("PUT /api/admin/llm/providers/{id}", func(http.ResponseWriter, *http.Request) {
		updateHit = true
	})

	req := httptest.NewRequest(http.MethodPut, "/api/admin/llm/providers/sequences", nil)
	mux.ServeHTTP(httptest.NewRecorder(), req)
	if !sequencesHit || updateHit {
		t.Fatalf("sequences route hit=%v update hit=%v", sequencesHit, updateHit)
	}

	req = httptest.NewRequest(http.MethodPut, "/api/admin/llm/providers/deepseek", nil)
	sequencesHit, updateHit = false, false
	mux.ServeHTTP(httptest.NewRecorder(), req)
	if !updateHit || sequencesHit {
		t.Fatalf("provider id route hit=%v sequences hit=%v", updateHit, sequencesHit)
	}
}
