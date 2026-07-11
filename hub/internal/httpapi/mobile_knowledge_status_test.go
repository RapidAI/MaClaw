package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMobileAgentKnowledgeStatusHandlerRequiresAuth(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/mobile/agent/knowledge/status", nil)
	MobileAgentKnowledgeStatusHandler(nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d want 401", rec.Code)
	}
}

func TestMobileAgentKnowledgeStatusHandlerReady(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	token, _ := issueViewerToken(t, identity, "mobile-knowledge@example.com")
	initMobileCoreAgentForTest(t, t.TempDir())

	req := httptest.NewRequest(http.MethodGet, "/api/mobile/agent/knowledge/status", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	MobileAgentKnowledgeStatusHandler(identity).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["available"] != true {
		t.Fatalf("body=%#v, want available true", body)
	}
	mode, _ := body["mode"].(string)
	if mode != "fts" && mode != "vector+fts" {
		t.Fatalf("mode=%v, want fts or vector+fts", body["mode"])
	}
}
