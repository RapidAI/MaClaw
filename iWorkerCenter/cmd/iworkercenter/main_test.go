package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/app"
	centercompute "github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/compute"
)

func TestAdminFrontendAPIRoutesAreForwardedToModules(t *testing.T) {
	paths := []string{
		"/admin/cloud/register",
		"/admin/cloud/license",
		"/admin/goalwatch/status",
		"/admin/iworker/instances",
		"/admin/executive/overview",
		"/admin/diworker-auth/accounts",
	}
	for _, path := range paths {
		if !isAdminAPIPath(path) {
			t.Fatalf("isAdminAPIPath(%q) = false, want true", path)
		}
	}
}

func TestRegisterV1RoutesListsCloudComputeModels(t *testing.T) {
	cloud := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"compute_permission": true,
			"providers": []map[string]any{{
				"id": "p1", "name": "Cloud GPT", "base_url": "https://llm.example/v1", "api_key": "sk-cloud", "protocol": "openai", "model": "gpt-cloud", "enabled": true, "priority": 80,
			}},
		})
	}))
	defer cloud.Close()

	syncMgr := centercompute.NewSyncManager(cloud.URL, "center-1", "secret-1")
	if err := syncMgr.SyncNow(); err != nil {
		t.Fatalf("SyncNow() error: %v", err)
	}
	mux := http.NewServeMux()
	registerV1Routes(mux, &app.Center{ComputeSourceManager: centercompute.NewSourceManager(syncMgr)})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/models", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(body.Data) != 1 || body.Data[0].ID != "gpt-cloud" {
		t.Fatalf("models = %+v, want gpt-cloud", body.Data)
	}
}

func TestRegisterV1RoutesForwardsChatCompletions(t *testing.T) {
	var upstreamCalled bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalled = true
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("upstream path = %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "chat-1", "object": "chat.completion", "model": "gpt-cloud",
			"choices": []map[string]any{{"index": 0, "message": map[string]string{"role": "assistant", "content": "ok"}, "finish_reason": "stop"}},
		})
	}))
	defer upstream.Close()
	cloud := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"compute_permission": true,
			"providers": []map[string]any{{
				"id": "p1", "name": "Cloud GPT", "base_url": upstream.URL, "api_key": "sk-cloud", "protocol": "openai", "model": "gpt-cloud", "enabled": true, "priority": 80,
			}},
		})
	}))
	defer cloud.Close()

	syncMgr := centercompute.NewSyncManager(cloud.URL, "center-1", "secret-1")
	if err := syncMgr.SyncNow(); err != nil {
		t.Fatalf("SyncNow() error: %v", err)
	}
	mux := http.NewServeMux()
	registerV1Routes(mux, &app.Center{ComputeSourceManager: centercompute.NewSourceManager(syncMgr)})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-cloud","messages":[{"role":"user","content":"hi"}]}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req.WithContext(context.Background()))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !upstreamCalled {
		t.Fatal("upstream was not called")
	}
}
