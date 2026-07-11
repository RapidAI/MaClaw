package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestMobileAgentMCPHandlerRequiresAuth(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/mobile/agent/mcp", nil)
	MobileAgentMCPHandler(nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d, want 401", rec.Code)
	}
}

func TestMobileAgentMCPHandlerGetAndPut(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	token, _ := issueViewerToken(t, identity, "mobile-mcp@example.com")
	initMobileCoreAgentForTest(t, t.TempDir())

	// Empty list initially.
	getReq := httptest.NewRequest(http.MethodGet, "/api/mobile/agent/mcp", nil)
	getReq.Header.Set("Authorization", "Bearer "+token)
	getRec := httptest.NewRecorder()
	MobileAgentMCPHandler(identity).ServeHTTP(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("GET status=%d body=%s", getRec.Code, getRec.Body.String())
	}
	var getBody map[string]any
	if err := json.Unmarshal(getRec.Body.Bytes(), &getBody); err != nil {
		t.Fatal(err)
	}
	if getBody["local_mcp_allowed"] != false {
		t.Fatalf("local_mcp_allowed=%v, want false", getBody["local_mcp_allowed"])
	}

	// Put one remote MCP server.
	putBody := `{"mcp_servers":[{"id":"alpha","name":"Alpha","endpoint_url":"https://mcp.example/v1","auth_type":"bearer","auth_secret":"secret-token"}]}`
	putReq := httptest.NewRequest(http.MethodPut, "/api/mobile/agent/mcp", strings.NewReader(putBody))
	putReq.Header.Set("Authorization", "Bearer "+token)
	putReq.Header.Set("Content-Type", "application/json")
	putRec := httptest.NewRecorder()
	MobileAgentMCPHandler(identity).ServeHTTP(putRec, putReq)
	if putRec.Code != http.StatusOK {
		t.Fatalf("PUT status=%d body=%s", putRec.Code, putRec.Body.String())
	}
	var putResp map[string]any
	if err := json.Unmarshal(putRec.Body.Bytes(), &putResp); err != nil {
		t.Fatal(err)
	}
	servers, _ := putResp["mcp_servers"].([]any)
	if len(servers) != 1 {
		t.Fatalf("mcp_servers=%#v", putResp["mcp_servers"])
	}
	item, _ := servers[0].(map[string]any)
	if item["id"] != "alpha" || item["has_auth_secret"] != true {
		t.Fatalf("server item=%#v", item)
	}
	// Secrets must not be echoed.
	if _, ok := item["auth_secret"]; ok {
		t.Fatalf("auth_secret must not be returned: %#v", item)
	}

	// GET again should list the server without secret.
	getReq2 := httptest.NewRequest(http.MethodGet, "/api/mobile/agent/mcp", nil)
	getReq2.Header.Set("Authorization", "Bearer "+token)
	getRec2 := httptest.NewRecorder()
	MobileAgentMCPHandler(identity).ServeHTTP(getRec2, getReq2)
	if getRec2.Code != http.StatusOK {
		t.Fatalf("GET2 status=%d body=%s", getRec2.Code, getRec2.Body.String())
	}
}

func TestMobileValidateMCPServers(t *testing.T) {
	if err := mobileValidateMCPServers(nil); err != nil {
		t.Fatal(err)
	}
	err := mobileValidateMCPServers([]corelib.MCPServerEntry{
		{ID: "", EndpointURL: "https://x"},
	})
	if err == nil {
		t.Fatal("expected error for empty id")
	}
	err = mobileValidateMCPServers([]corelib.MCPServerEntry{
		{ID: "a", EndpointURL: "ftp://x"},
	})
	if err == nil {
		t.Fatal("expected error for non-http endpoint")
	}
}
