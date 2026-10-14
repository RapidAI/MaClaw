package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDesktopMCPTokensAndCalls(t *testing.T) {
	t.Setenv("MACLAW_DESKTOP_API_TOKEN", "desktop-api-token")
	var sawUser string
	docker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		sawUser, _ = body["user_id"].(string)
		switch r.URL.Path {
		case "/v1/desktops/ctl":
			_, _ = w.Write([]byte(`{"ok":true,"width":1280,"height":800,"viewers":0}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(docker.Close)
	settings := newDesktopSettingsMem()
	tokens := &desktopMCPTokenStore{system: settings}
	mcp := DesktopMCPHandler(desktopPoolFake(t, settings, docker.URL), tokens, nil, nil)
	noAdmin := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) { writeError(w, http.StatusUnauthorized, "ADMIN_UNAUTHORIZED", "no") }
	}
	admin := DesktopMCPAdminTokensHandler(tokens, noAdmin)

	create := func(bearer, body string) (int, map[string]any) {
		req := httptest.NewRequest(http.MethodPost, "/api/admin/desktop-mcp/tokens", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+bearer)
		w := httptest.NewRecorder()
		admin.ServeHTTP(w, req)
		var out map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &out)
		return w.Code, out
	}
	if code, _ := create("nope", `{"user_id":"alice"}`); code != http.StatusUnauthorized {
		t.Fatalf("create without auth = %d", code)
	}
	code, out := create("desktop-api-token", `{"tenant_id":"tenant-a","user_id":"alice","scope":"control"}`)
	control, _ := out["token"].(string)
	if code != http.StatusCreated || !strings.HasPrefix(control, "mcpd_") {
		t.Fatalf("create = %d %v", code, out)
	}
	_, out = create("desktop-api-token", `{"tenant_id":"tenant-a","user_id":"alice","scope":"read"}`)
	read, _ := out["token"].(string)
	raw, _ := settings.Get(t.Context(), desktopMCPTokensKey)
	if strings.Contains(raw, control) || strings.Contains(raw, read) {
		t.Fatal("plaintext token stored")
	}

	call := func(bearer, method string, params any) (int, string) {
		body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
		req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(string(body)))
		if bearer != "" {
			req.Header.Set("Authorization", "Bearer "+bearer)
		}
		w := httptest.NewRecorder()
		mcp.ServeHTTP(w, req)
		return w.Code, w.Body.String()
	}
	if code, _ := call("", "ping", nil); code != http.StatusUnauthorized {
		t.Fatalf("no token = %d", code)
	}
	if code, _ := call("mcpd_wrong", "ping", nil); code != http.StatusUnauthorized {
		t.Fatalf("wrong token = %d", code)
	}
	if code, _ := call("desktop-api-token", "ping", nil); code != http.StatusUnauthorized {
		t.Fatalf("service token must not open /mcp: %d", code)
	}
	code, body := call(control, "tools/call", map[string]any{"name": "desktop_screen_info", "arguments": map[string]any{}})
	if code != http.StatusOK || !strings.Contains(body, "1280") || sawUser != "alice" {
		t.Fatalf("screen_info = %d %s (user %q)", code, body, sawUser)
	}
	_, body = call(control, "tools/call", map[string]any{"name": "desktop_screen_info", "arguments": map[string]any{"user_id": "bob"}})
	if !strings.Contains(body, "only reaches its own desktop") {
		t.Fatalf("other user = %s", body)
	}
	_, body = call(read, "tools/list", map[string]any{})
	if strings.Contains(body, `"desktop_computer"`) || !strings.Contains(body, "desktop_screenshot") {
		t.Fatalf("read tools/list = %s", body)
	}
	_, body = call(read, "tools/call", map[string]any{"name": "desktop_computer", "arguments": map[string]any{"actions": []any{map[string]any{"action": "key", "text": "Return"}}}})
	if !strings.Contains(body, "read-only") {
		t.Fatalf("read control = %s", body)
	}
}
