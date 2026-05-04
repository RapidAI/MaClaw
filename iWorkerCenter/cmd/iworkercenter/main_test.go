package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

func TestRegisterV1RoutesFallsBackToCenterSettingsModels(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	settingsPath := filepath.Join(home, ".iworkercenter", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		t.Fatalf("mkdir settings dir: %v", err)
	}
	settings := `{"providers":[{"id":"local-openai","name":"Local OpenAI","base_url":"https://local.example/v1","api_key":"sk-local","protocol":"openai","model":"gpt-local","enabled":true,"priority":50}]}`
	if err := os.WriteFile(settingsPath, []byte(settings), 0o644); err != nil {
		t.Fatalf("write settings: %v", err)
	}

	mux := http.NewServeMux()
	syncMgr := centercompute.NewSyncManager("http://127.0.0.1:1", "center-1", "secret-1")
	_ = syncMgr.SyncNow()
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
	if len(body.Data) != 1 || body.Data[0].ID != "gpt-local" {
		t.Fatalf("models = %+v, want gpt-local", body.Data)
	}
}

func TestRegisterV1RoutesFallsBackToCenterSettingsChat(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	var upstreamCalled bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalled = true
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("upstream path = %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "chat-local", "object": "chat.completion", "model": "gpt-local",
			"choices": []map[string]any{{"index": 0, "message": map[string]string{"role": "assistant", "content": "local ok"}, "finish_reason": "stop"}},
		})
	}))
	defer upstream.Close()
	settingsPath := filepath.Join(home, ".iworkercenter", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		t.Fatalf("mkdir settings dir: %v", err)
	}
	settings := fmt.Sprintf(`{"providers":[{"id":"local-openai","name":"Local OpenAI","base_url":%q,"api_key":"sk-local","protocol":"openai","model":"gpt-local","enabled":true,"priority":50}]}`, upstream.URL)
	if err := os.WriteFile(settingsPath, []byte(settings), 0o644); err != nil {
		t.Fatalf("write settings: %v", err)
	}

	mux := http.NewServeMux()
	syncMgr := centercompute.NewSyncManager("http://127.0.0.1:1", "center-1", "secret-1")
	_ = syncMgr.SyncNow()
	registerV1Routes(mux, &app.Center{ComputeSourceManager: centercompute.NewSourceManager(syncMgr)})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-local","messages":[{"role":"user","content":"hi"}]}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req.WithContext(context.Background()))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !upstreamCalled {
		t.Fatal("local settings upstream was not called")
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

func TestEmbeddedAdminFrontendReferencesExistingStableAssets(t *testing.T) {
	indexPath := filepath.Join("web", "admin", "index.html")
	body, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatalf("read admin index: %v", err)
	}
	indexHTML := string(body)
	for _, asset := range []string{"/admin/assets/index.js", "/admin/assets/index.css"} {
		if !strings.Contains(indexHTML, asset) {
			t.Fatalf("admin index does not reference %s: %s", asset, indexHTML)
		}
	}
	if strings.Contains(indexHTML, "/admin/assets/index-") {
		t.Fatalf("admin index should use stable asset names, got: %s", indexHTML)
	}
	for _, asset := range []string{"index.js", "index.css"} {
		path := filepath.Join("web", "admin", "assets", asset)
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("admin asset %s missing: %v", asset, err)
		}
		if info.Size() == 0 {
			t.Fatalf("admin asset %s is empty", asset)
		}
	}
}
