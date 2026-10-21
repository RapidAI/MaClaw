package desktopd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMCPRequiresAKnownGateToken(t *testing.T) {
	desktopGates = gateScreens{}
	h := mcpHandler(&Service{})
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`
	for _, auth := range []string{"", "Bearer nope", "Bearer " + strings.Repeat("a", 32)} {
		req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("auth %q: code=%d body=%s", auth, rec.Code, rec.Body.String())
		}
	}
}

func TestMCPGateTokenListsControlTools(t *testing.T) {
	desktopGates = gateScreens{}
	token := strings.Repeat("b", 32)
	(&Service{}).rememberGate(Spec{TenantID: "tenant", UserID: "alice", BotID: "bot_1"}, token, ":21")
	h := mcpHandler(&Service{})
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	var out struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, tool := range out.Result.Tools {
		names[tool.Name] = true
	}
	for _, want := range []string{"desktop_screenshot", "desktop_bash"} {
		if !names[want] {
			t.Fatalf("missing %s in %v", want, names)
		}
	}
	for name := range names {
		if strings.Contains(name, "install") {
			t.Fatalf("gate token must not get install: %s", name)
		}
	}

	id, ok := mcpAuth(req)
	if !ok || id.TenantID != "tenant" || !strings.Contains(id.UserID, "bot_1") {
		t.Fatalf("identity=%+v", id)
	}
	if display, bot := mcpScreen(id); display != ":21" || bot != "bot_1" {
		t.Fatalf("screen=%s bot=%s", display, bot)
	}
	desktopGates.forget("tenant", "alice")
	if _, ok := mcpAuth(req); ok {
		t.Fatal("stopped desktop's token must be refused")
	}
}

func TestMCPCanBeTurnedOff(t *testing.T) {
	t.Setenv("DESKTOPD_MCP", "off")
	rec := httptest.NewRecorder()
	mcpHandler(&Service{}).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader("{}")))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("code=%d", rec.Code)
	}
}
