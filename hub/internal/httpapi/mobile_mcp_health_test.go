package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMobileAgentMCPHealthHandlerRequiresAuth(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/mobile/agent/mcp/health", nil)
	MobileAgentMCPHealthHandler(nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d want 401", rec.Code)
	}
}

func TestMobileAgentMCPHealthHandlerEmptyConfig(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	token, _ := issueViewerToken(t, identity, "mobile-mcp-health@example.com")
	initMobileCoreAgentForTest(t, t.TempDir())

	req := httptest.NewRequest(http.MethodPost, "/api/mobile/agent/mcp/health", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	MobileAgentMCPHealthHandler(identity).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["server_count"] != float64(0) && body["server_count"] != 0 {
		// JSON numbers decode as float64
		if n, ok := body["server_count"].(float64); !ok || n != 0 {
			t.Fatalf("body=%#v, want empty servers", body)
		}
	}
	if body["status"] != "ok" {
		t.Fatalf("status field=%v", body["status"])
	}
}
