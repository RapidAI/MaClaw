package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMobileAgentKnowledgeIngestHandlerRequiresAuth(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/mobile/agent/knowledge/ingest", strings.NewReader(`{"text":"hi"}`))
	MobileAgentKnowledgeIngestHandler(nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d want 401", rec.Code)
	}
}

func TestMobileAgentKnowledgeIngestHandlerSavesNote(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	token, _ := issueViewerToken(t, identity, "mobile-knowledge-ingest@example.com")
	initMobileCoreAgentForTest(t, t.TempDir())

	req := httptest.NewRequest(http.MethodPost, "/api/mobile/agent/knowledge/ingest", strings.NewReader(
		`{"text":"手机备忘：明天下午同步设计文档","title":"会议备忘"}`,
	))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	MobileAgentKnowledgeIngestHandler(identity).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["ok"] != true {
		t.Fatalf("body=%#v", body)
	}
	if body["title"] != "会议备忘" {
		t.Fatalf("title=%v", body["title"])
	}
	if body["source_id"] == nil || body["source_id"] == "" {
		t.Fatalf("missing source_id: %#v", body)
	}

	// Status should reflect at least one source.
	statusReq := httptest.NewRequest(http.MethodGet, "/api/mobile/agent/knowledge/status", nil)
	statusReq.Header.Set("Authorization", "Bearer "+token)
	statusRec := httptest.NewRecorder()
	MobileAgentKnowledgeStatusHandler(identity).ServeHTTP(statusRec, statusReq)
	if statusRec.Code != http.StatusOK {
		t.Fatalf("status API=%d body=%s", statusRec.Code, statusRec.Body.String())
	}
	var status map[string]any
	if err := json.Unmarshal(statusRec.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if status["available"] != true {
		t.Fatalf("status=%#v", status)
	}
	sources, _ := status["sources"].(float64)
	if sources < 1 {
		t.Fatalf("expected sources>=1 after ingest, status=%#v", status)
	}
}

func TestMobileAgentKnowledgeIngestHandlerRejectsEmptyText(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	token, _ := issueViewerToken(t, identity, "mobile-knowledge-empty@example.com")
	initMobileCoreAgentForTest(t, t.TempDir())

	req := httptest.NewRequest(http.MethodPost, "/api/mobile/agent/knowledge/ingest", strings.NewReader(`{"text":"  "}`))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	MobileAgentKnowledgeIngestHandler(identity).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d want 400 body=%s", rec.Code, rec.Body.String())
	}
}
