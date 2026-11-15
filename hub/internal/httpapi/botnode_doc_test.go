package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBotnodeDocIsTheRunbook(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/docs/botnode", nil)
	rec := httptest.NewRecorder()
	serveBotnodeDoc(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	if !strings.Contains(rec.Header().Get("Content-Type"), "text/plain") {
		t.Fatal(rec.Header().Get("Content-Type"))
	}
	body := rec.Body.String()
	for _, phrase := range []string{"git clone", "go build", "BOTNODE_ENROLL_CODE", "HUB_EDGE_NODES", "/opt/maclaw-node/botnode"} {
		if !strings.Contains(body, phrase) {
			t.Fatalf("runbook missing %q", phrase)
		}
	}
}
