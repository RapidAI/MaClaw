package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
)

func TestNormalizeWorkflowDraftDescription(t *testing.T) {
	description, ok := normalizeWorkflowDraftDescription("  Purchase approval  ")
	if !ok || description != "Purchase approval" {
		t.Fatalf("description = %q ok=%v", description, ok)
	}
	if description, ok := normalizeWorkflowDraftDescription("   "); ok || description != "" {
		t.Fatalf("blank description = %q ok=%v", description, ok)
	}
	long := make([]byte, workflowDraftDescriptionMaxBytes+10)
	for i := range long {
		long[i] = 'a'
	}
	description, ok = normalizeWorkflowDraftDescription(string(long))
	if !ok || len(description) != workflowDraftDescriptionMaxBytes {
		t.Fatalf("truncated length = %d ok=%v", len(description), ok)
	}
	longUnicode := string(long[:workflowDraftDescriptionMaxBytes-1]) + "\u5ba1\u6279"
	description, ok = normalizeWorkflowDraftDescription(longUnicode)
	if !ok || !utf8.ValidString(description) || len(description) > workflowDraftDescriptionMaxBytes {
		t.Fatalf("unicode description length = %d valid=%v ok=%v", len(description), utf8.ValidString(description), ok)
	}
}

func TestWorkflowDraftLLMHandlerRejectsOversizedBody(t *testing.T) {
	authenticator := fakeVEMachineAuth{
		token: "machine-token",
		principals: map[string]*auth.MachinePrincipal{
			"machine-1": {TenantID: "tenant_default", UserID: "user-1", MachineID: "machine-1"},
		},
	}
	body := `{"description":"` + strings.Repeat("a", workflowDraftDescriptionMaxBytes*2) + `","language":"en"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/workflow-drafts/generate", strings.NewReader(body))
	req.Header.Set("X-Machine-ID", "machine-1")
	req.Header.Set("Authorization", "Bearer machine-token")
	rec := httptest.NewRecorder()

	WorkflowDraftLLMHandler(authenticator, nil, nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "REQUEST_TOO_LARGE") {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestWorkflowDraftLLMHandlerRequiresMachineAuth(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/workflow-drafts/generate", strings.NewReader(`{"description":"Purchase approval"}`))
	rec := httptest.NewRecorder()

	WorkflowDraftLLMHandler(nil, nil, nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "MACHINE_UNAUTHORIZED") {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestBuildFallbackWorkflowDraftReturnsEditorGraph(t *testing.T) {
	draft := buildFallbackWorkflowDraft("Purchase approval", "en")
	if draft["name"] != "Approval workflow draft" {
		t.Fatalf("name = %q", draft["name"])
	}
	if draft["description"] != "Purchase approval" {
		t.Fatalf("description = %q", draft["description"])
	}
	graph, ok := draft["graph"].(map[string]any)
	if !ok {
		t.Fatalf("graph = %#v", draft["graph"])
	}
	nodes, ok := graph["nodes"].([]map[string]any)
	if !ok || len(nodes) != 4 {
		t.Fatalf("nodes = %#v", graph["nodes"])
	}
	edges, ok := graph["edges"].([]map[string]any)
	if !ok || len(edges) != 3 {
		t.Fatalf("edges = %#v", graph["edges"])
	}
	wantTypes := []string{"trigger", "form", "approval", "terminal"}
	for i, want := range wantTypes {
		if nodes[i]["type"] != want {
			t.Fatalf("node %d type = %q, want %q", i, nodes[i]["type"], want)
		}
	}
	triggerConfig, ok := nodes[0]["config"].(map[string]any)
	if !ok || triggerConfig["description"] != "Purchase approval" {
		t.Fatalf("trigger config = %#v", nodes[0]["config"])
	}
}

func TestBuildFallbackWorkflowDraftLocalizesChineseLabels(t *testing.T) {
	draft := buildFallbackWorkflowDraft("\u91c7\u8d2d\u5ba1\u6279", "zh-Hans")
	if draft["name"] != "\u5ba1\u6279\u6d41\u7a0b\u8349\u7a3f" {
		t.Fatalf("name = %q", draft["name"])
	}
	graph := draft["graph"].(map[string]any)
	nodes := graph["nodes"].([]map[string]any)
	if nodes[0]["label"] != "\u5f00\u59cb" || nodes[2]["label"] != "\u5ba1\u6279" {
		t.Fatalf("localized labels = %#v %#v", nodes[0]["label"], nodes[2]["label"])
	}
}
