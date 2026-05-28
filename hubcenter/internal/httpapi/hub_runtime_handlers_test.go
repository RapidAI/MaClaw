package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/hubs"
)

func TestListHubRuntimeStatusesHandler(t *testing.T) {
	svc := newHubCenterHTTPTestServices(t)
	token := issueAdminToken(t, svc)

	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/public/model_download/status" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":            "ready",
			"model_dir":         "/data/soft/hub/data/models",
			"public_models_url": "https://hub.example.com/api/v1/models/{filename}",
			"initialized":       true,
			"downloading":       false,
			"ready":             true,
			"expected_files":    []string{"a.gguf", "b.gguf"},
			"missing_files":     []string{},
		})
	}))
	defer remote.Close()

	_, err := svc.hubs.RegisterHub(context.Background(), hubs.RegisterHubRequest{
		OwnerEmail:     "owner@example.com",
		Name:           "Runtime Hub",
		BaseURL:        remote.URL,
		Visibility:     "shared",
		EnrollmentMode: "open",
	})
	if err != nil {
		t.Fatalf("register hub: %v", err)
	}

	resp := doJSONRequest(t, svc.handler, http.MethodGet, "/api/admin/hubs/runtime", nil, token)
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}
	body := resp.Body.String()
	if !strings.Contains(body, `"fetch_ok":true`) || !strings.Contains(body, `"status":"ready"`) {
		t.Fatalf("unexpected body=%s", body)
	}
}

func TestFetchHubRuntimeStatusesLimitsConcurrency(t *testing.T) {
	var mu sync.Mutex
	active := 0
	maxActive := 0
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		active++
		if active > maxActive {
			maxActive = active
		}
		mu.Unlock()

		time.Sleep(20 * time.Millisecond)
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "ready", "ready": true})

		mu.Lock()
		active--
		mu.Unlock()
	}))
	defer remote.Close()

	items := make([]hubRuntimeHub, 20)
	for i := range items {
		items[i] = hubRuntimeHub{id: "hub", name: "Hub", baseURL: remote.URL}
	}
	results := fetchHubRuntimeStatuses(context.Background(), remote.Client(), items, 3)
	if len(results) != len(items) {
		t.Fatalf("len(results)=%d, want %d", len(results), len(items))
	}
	for i, item := range results {
		if !item.FetchOK {
			t.Fatalf("result %d not ok: %+v", i, item)
		}
	}
	if maxActive > 3 {
		t.Fatalf("max concurrent fetches = %d, want <= 3", maxActive)
	}
}
