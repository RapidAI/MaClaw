package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
