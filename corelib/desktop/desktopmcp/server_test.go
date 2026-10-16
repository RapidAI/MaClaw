package desktopmcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

type fakeBackend struct {
	apps     []string
	ctl      []map[string]any
	shots    []desktop.ShotOptions
	viewers  int
	bashRuns []string
	class    string
}

func pngOf(w, h int) []byte {
	var buf bytes.Buffer
	_ = png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, w, h)))
	return buf.Bytes()
}

func (f *fakeBackend) Screenshot(_ context.Context, _ Identity, opts desktop.ShotOptions) (desktop.Shot, error) {
	f.shots = append(f.shots, opts)
	w := 1280
	if opts.MaxWidth > 0 && opts.MaxWidth < w {
		w = opts.MaxWidth
	}
	scale := float64(w) / 1280
	h := int(800*scale + 0.5)
	return desktop.Shot{Data: pngOf(w, h), MIME: "image/png", ScreenWidth: 1280, ScreenHeight: 800, ImageWidth: w, ImageHeight: h, Scale: scale, Region: desktop.ShotRegion{Width: 1280, Height: 800}}, nil
}

func (f *fakeBackend) App(_ context.Context, _ Identity, argv []string) (string, error) {
	line := strings.Join(argv, " ")
	if line == "getactivewindow getwindowclassname" {
		return f.class, nil
	}
	f.apps = append(f.apps, line)
	if strings.Contains(line, "click 3") {
		return "", fmt.Errorf("exit status 1")
	}
	return "", nil
}

func (f *fakeBackend) Ctl(_ context.Context, _ Identity, request map[string]any) (map[string]any, error) {
	f.ctl = append(f.ctl, request)
	switch request["op"] {
	case desktop.CtlStatus:
		return map[string]any{"ok": true, "viewers": float64(f.viewers)}, nil
	case desktop.CtlClipboardGet:
		return map[string]any{"ok": true, "type": "text", "text": "你好"}, nil
	case desktop.CtlA11yProbe:
		return map[string]any{"ok": true, "windows": 1.0, "elements": []any{map[string]any{"id": 1.0, "role": "push button", "name": "保存", "bounds": []any{100.0, 50.0, 80.0, 30.0}}}}, nil
	}
	return map[string]any{"ok": true}, nil
}

func (f *fakeBackend) Open(context.Context, Identity, string, []string) (string, error) {
	return "", nil
}
func (f *fakeBackend) File(_ context.Context, _ Identity, action, path, content string) (string, error) {
	if action == "bytes" {
		return "aGVsbG8=", nil
	}
	return action + " " + path, nil
}
func (f *fakeBackend) Bash(_ context.Context, _ Identity, command string) (string, error) {
	f.bashRuns = append(f.bashRuns, command)
	return "5 bytes", nil
}
func (f *fakeBackend) Install(context.Context, Identity, []string) (string, error) { return "ok", nil }
func (f *fakeBackend) Session(context.Context, Identity) (map[string]any, error) {
	return map[string]any{"display": ":20", "cdp_url": "http://desktop:x@host:1"}, nil
}

var testTokens map[string]Identity

func newTestServer(f *fakeBackend) (*Server, *httptest.Server) {
	testTokens = map[string]Identity{
		HashToken("control-token"): {Name: "c", TenantID: "t", UserID: "u", Scope: "control"},
		HashToken("read-token"):    {Name: "r", TenantID: "t", UserID: "u2", Scope: "read"},
		HashToken("admin-token"):   {Name: "a", TenantID: "t", Scope: "control", AnyUser: true},
	}
	s := New(f, Options{Auth: StaticTokens(testTokens), Settle: time.Millisecond})
	return s, httptest.NewServer(s)
}

func rpc(t *testing.T, url, token, method string, params any) map[string]any {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	req, _ := http.NewRequest(http.MethodPost, url+"/mcp", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("%s: status %d: %v", method, resp.StatusCode, err)
	}
	return out
}

func callTool(t *testing.T, url, token, name string, args map[string]any) map[string]any {
	t.Helper()
	out := rpc(t, url, token, "tools/call", map[string]any{"name": name, "arguments": args})
	result, _ := out["result"].(map[string]any)
	if result == nil {
		t.Fatalf("%s: %v", name, out)
	}
	return result
}

func contentText(result map[string]any) string {
	var parts []string
	for _, raw := range result["content"].([]any) {
		c := raw.(map[string]any)
		if c["type"] == "text" {
			parts = append(parts, c["text"].(string))
		} else {
			parts = append(parts, "<"+fmt.Sprint(c["type"])+">")
		}
	}
	return strings.Join(parts, "\n")
}

func TestMCPInitializeListAndAuth(t *testing.T) {
	_, srv := newTestServer(&fakeBackend{})
	defer srv.Close()
	resp, _ := http.Post(srv.URL+"/mcp", "application/json", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no token status=%d", resp.StatusCode)
	}
	init := rpc(t, srv.URL, "control-token", "initialize", map[string]any{"protocolVersion": "2025-03-26", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "test", "version": "1"}})
	result := init["result"].(map[string]any)
	if result["protocolVersion"] != "2025-03-26" || result["serverInfo"].(map[string]any)["name"] != serverName {
		t.Fatalf("initialize = %v", init)
	}
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/mcp", strings.NewReader(`{"jsonrpc":"2.0","method":"notifications/initialized"}`))
	req.Header.Set("Authorization", "Bearer control-token")
	if resp, _ := http.DefaultClient.Do(req); resp.StatusCode != http.StatusAccepted {
		t.Fatalf("notification status=%d", resp.StatusCode)
	}
	names := func(token string) string {
		list := rpc(t, srv.URL, token, "tools/list", map[string]any{})["result"].(map[string]any)["tools"].([]any)
		var out []string
		for _, item := range list {
			tool := item.(map[string]any)
			if tool["inputSchema"].(map[string]any)["type"] != "object" {
				t.Fatalf("schema of %v", tool["name"])
			}
			out = append(out, tool["name"].(string))
		}
		return strings.Join(out, ",")
	}
	control, read := names("control-token"), names("read-token")
	if !strings.Contains(control, "desktop_computer") || strings.Contains(control, "desktop_bash") || strings.Contains(read, "desktop_computer") || !strings.Contains(read, "desktop_screenshot") {
		t.Fatalf("control=%s read=%s", control, read)
	}
	if out := callTool(t, srv.URL, "read-token", "desktop_computer", map[string]any{"actions": []any{map[string]any{"action": "left_click", "coordinate": []any{1, 1}}}}); out["isError"] != true {
		t.Fatalf("read token ran computer: %v", out)
	}
	if out := rpc(t, srv.URL, "control-token", "nope/method", nil); out["error"] == nil {
		t.Fatal("unknown method accepted")
	}
}

func TestMCPComputerUsesTheScaledFrameAndStopsOnFailure(t *testing.T) {
	f := &fakeBackend{class: "Mousepad"}
	_, srv := newTestServer(f)
	defer srv.Close()
	shot := callTool(t, srv.URL, "control-token", "desktop_screenshot", map[string]any{"max_width": 1024})
	if !strings.Contains(contentText(shot), `"scale":0.8`) || !strings.Contains(contentText(shot), "<image>") {
		t.Fatalf("screenshot = %s", contentText(shot))
	}
	out := callTool(t, srv.URL, "control-token", "desktop_computer", map[string]any{"actions": []any{
		map[string]any{"action": "left_click", "coordinate": []any{512, 320}},
		map[string]any{"type": "keypress", "keys": []any{"CTRL", "S"}},
		map[string]any{"action": "right_click", "coordinate": []any{10, 10}},
		map[string]any{"action": "type", "text": "never"},
	}})
	text := contentText(out)
	if out["isError"] != true || !strings.Contains(text, "3. click: failed") || !strings.Contains(text, "4. type: not executed") {
		t.Fatalf("computer = %s", text)
	}
	if f.apps[0] != "mousemove --sync 640 400 click 1" || f.apps[1] != "key ctrl+s" {
		t.Fatalf("apps = %v", f.apps)
	}
}

func TestMCPRefusesControlDuringTakeoverAndTypingIntoTheBrowser(t *testing.T) {
	f := &fakeBackend{viewers: 1, class: "Chromium"}
	_, srv := newTestServer(f)
	defer srv.Close()
	if out := callTool(t, srv.URL, "control-token", "desktop_clipboard_set", map[string]any{"text": "x"}); out["isError"] != true || !strings.Contains(contentText(out), "person is using") {
		t.Fatalf("takeover = %v", out)
	}
	if out := callTool(t, srv.URL, "control-token", "desktop_clipboard_get", map[string]any{}); out["isError"] == true || !strings.Contains(contentText(out), "你好") {
		t.Fatalf("read during takeover = %v", out)
	}
	f.viewers = 0
	out := callTool(t, srv.URL, "control-token", "desktop_computer", map[string]any{"actions": []any{map[string]any{"action": "type", "text": "secret"}}})
	if out["isError"] != true || !strings.Contains(contentText(out), "browser") {
		t.Fatalf("browser typing = %s", contentText(out))
	}
}

func TestMCPProbeWindowsFilesAndStdio(t *testing.T) {
	f := &fakeBackend{}
	s, srv := newTestServer(f)
	defer srv.Close()
	if out := contentText(callTool(t, srv.URL, "read-token", "desktop_app_probe", map[string]any{})); !strings.Contains(out, `[1] push button "保存" screen 100,50 80x30`) {
		t.Fatalf("probe = %s", out)
	}
	if out := contentText(callTool(t, srv.URL, "read-token", "desktop_file_download", map[string]any{"path": "~/a.bin"})); out != "<resource>" {
		t.Fatalf("download = %s", out)
	}
	testTokens[HashToken("bash-token")] = Identity{TenantID: "t", UserID: "u", Scope: "control", AllowBash: true}
	if out := callTool(t, srv.URL, "control-token", "desktop_bash", map[string]any{"command": "id"}); out["isError"] != true {
		t.Fatal("bash without allow_bash ran")
	}
	if out := contentText(callTool(t, srv.URL, "bash-token", "desktop_file_upload", map[string]any{"path": "~/Desktop/x.bin", "content_base64": "aGVsbG8="})); !strings.Contains(out, "/home/desktop/Desktop/x.bin") {
		t.Fatalf("upload = %s", out)
	}
	var stdout bytes.Buffer
	in := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05"}}` + "\n" +
		`{"jsonrpc":"2.0","method":"notifications/initialized"}` + "\n" +
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"desktop_windows","arguments":{}}}` + "\n")
	if err := s.ServeStdio(context.Background(), Identity{TenantID: "t", UserID: "u", Scope: "read"}, in, &stdout); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	if len(lines) != 2 || !strings.Contains(lines[0], `"protocolVersion":"2024-11-05"`) || !strings.Contains(lines[1], `"id":2`) {
		t.Fatalf("stdio = %s", stdout.String())
	}
}

func TestAdminTargetsUserAndOthersCannot(t *testing.T) {
	f := &fakeBackend{}
	_, srv := newTestServer(f)
	defer srv.Close()
	out := rpc(t, srv.URL, "admin-token", "tools/call", map[string]any{"name": "desktop_screen_info", "arguments": map[string]any{}})
	if !strings.Contains(fmt.Sprint(out), "user_id is required") {
		t.Fatalf("admin without user_id = %v", out)
	}
	out = rpc(t, srv.URL, "admin-token", "tools/call", map[string]any{"name": "desktop_screen_info", "arguments": map[string]any{"user_id": "alice"}})
	if strings.Contains(fmt.Sprint(out), "isError:true") {
		t.Fatalf("admin with user_id = %v", out)
	}
	out = rpc(t, srv.URL, "admin-token", "tools/call", map[string]any{"name": "desktop_screen_info", "arguments": map[string]any{"user_id": "alice", "tenant_id": "other"}})
	if !strings.Contains(fmt.Sprint(out), "limited to tenant") {
		t.Fatalf("admin other tenant = %v", out)
	}
	out = rpc(t, srv.URL, "control-token", "tools/call", map[string]any{"name": "desktop_screen_info", "arguments": map[string]any{"user_id": "alice"}})
	if !strings.Contains(fmt.Sprint(out), "only reaches its own desktop") {
		t.Fatalf("user naming another = %v", out)
	}
	list := rpc(t, srv.URL, "admin-token", "tools/list", map[string]any{})
	if !strings.Contains(fmt.Sprint(list), "user_id") {
		t.Fatalf("admin tools/list lacks user_id")
	}
}
