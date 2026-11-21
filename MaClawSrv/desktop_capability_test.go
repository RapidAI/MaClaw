package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/botcapability"
	"github.com/RapidAI/CodeClaw/corelib/desktopbot"
)

func capabilityCtx(t *testing.T, doc botcapability.Document, phase string) context.Context {
	t.Helper()
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.WithValue(context.Background(), desktopAppConfigKey{}, corelib.AppConfig{DesktopBotCapabilities: raw})
	return context.WithValue(ctx, desktopPhaseKey{}, phase)
}

func TestDesktopPromptNamesAccountCapabilitiesWithoutSkillBodies(t *testing.T) {
	t.Setenv(desktopCDPEnv, "http://127.0.0.1:9222/")
	raw, err := json.Marshal(botcapability.Document{
		MCP:    []botcapability.Server{{ID: "search", Name: "Search", Transport: "http", URL: "https://mcp.example/mcp"}},
		Skills: []botcapability.Skill{{Name: "draft", Description: "save a draft", Body: "Write the secret draft body."}},
	})
	if err != nil {
		t.Fatal(err)
	}
	cfg := corelib.AppConfig{DesktopBotCapabilities: raw}
	execute := agentruntime.TurnRequest{Input: agentservice.ExecuteRequest{
		Message: agentservice.Message{Metadata: map[string]string{"bot_phase": "execute"}},
		Config:  cfg,
	}}
	prompt, err := desktopRuntimeModule{}.ContributePrompt(context.Background(), execute)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt, "Search") || !strings.Contains(prompt, "draft") || !strings.Contains(prompt, "save a draft") {
		t.Fatalf("catalog missing: %s", prompt)
	}
	if strings.Contains(prompt, "secret draft body") {
		t.Fatal("skill body entered the prompt")
	}
	plan := execute
	plan.Input = agentservice.ExecuteRequest{
		Message: agentservice.Message{Metadata: map[string]string{"bot_phase": "plan", "bot_phase_decided": "1"}},
		Config:  cfg,
	}
	planned, err := desktopRuntimeModule{}.ContributePrompt(context.Background(), plan)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(planned, "cap_list") || !strings.Contains(planned, "Calling a tool waits until execute") || !strings.Contains(planned, "draft") {
		t.Fatalf("plan catalog missing: %s", planned)
	}
	bare, err := desktopRuntimeModule{}.ContributePrompt(context.Background(), agentruntime.TurnRequest{Input: agentservice.ExecuteRequest{
		Message: agentservice.Message{Metadata: map[string]string{"bot_phase": "execute"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(bare, "This account's MCP servers and skills") {
		t.Fatal("empty account still named capabilities")
	}
}

func TestDesktopCapSkillAndList(t *testing.T) {
	doc := botcapability.Document{
		MCP:    []botcapability.Server{{ID: "files", Name: "Files", Transport: "stdio", Command: "npx", Disabled: true}},
		Skills: []botcapability.Skill{{Name: "draft", Description: "save a draft", Body: "Write the draft."}},
	}
	scope := agentruntime.Scope{TenantID: "t", UserID: "alice", InstanceID: "bot"}
	out, err := operateDesktop(capabilityCtx(t, doc, "execute"), scope, map[string]any{"action": "cap_skill", "name": "draft"})
	if err != nil || !strings.Contains(out, "Write the draft.") || !strings.Contains(out, "save a draft") {
		t.Fatalf("skill: %q %v", out, err)
	}
	listed, err := operateDesktop(capabilityCtx(t, doc, "plan"), scope, map[string]any{"action": "cap_list"})
	if err != nil || strings.Contains(listed, "files") || !strings.Contains(listed, "draft") {
		t.Fatalf("list: %q %v", listed, err)
	}
	_, err = operateDesktop(capabilityCtx(t, doc, "plan"), scope, map[string]any{"action": "cap_mcp", "mcp_server": "files", "mcp_tool": "read"})
	if err == nil || !strings.Contains(err.Error(), desktopbot.PlanPhaseNextStep) {
		t.Fatalf("plan mcp: %v", err)
	}
}

func TestDesktopMCPCloudArgvRewritesWindowsShims(t *testing.T) {
	cmd, args := desktopMCPCloudArgv(`C:\Program Files\nodejs\npx.cmd`, []string{"-y", "pkg"})
	if cmd != "npx" || len(args) != 2 || args[0] != "-y" {
		t.Fatalf("npx: %s %#v", cmd, args)
	}
	cmd, args = desktopMCPCloudArgv("cmd.exe", []string{"/c", "python.exe", "server.py"})
	if cmd != "python3" || len(args) != 1 || args[0] != "server.py" {
		t.Fatalf("cmd: %s %#v", cmd, args)
	}
	cmd, args = desktopMCPCloudArgv("/usr/bin/npx", []string{"-y", "pkg"})
	if cmd != "/usr/bin/npx" || args[0] != "-y" {
		t.Fatalf("linux: %s %#v", cmd, args)
	}
	cmd, args = desktopMCPCloudArgv(`C:\Windows\System32\cmd.exe`, []string{"/d", "/s", "/c", "npx.cmd", "-y", "pkg"})
	if cmd != "npx" || len(args) != 2 || args[0] != "-y" || args[1] != "pkg" {
		t.Fatalf("cmd flags: %s %#v", cmd, args)
	}
	cmd, args = desktopMCPCloudArgv("cmd", []string{"/c", "npx -y pkg"})
	if cmd != "npx" || len(args) != 2 || args[0] != "-y" || args[1] != "pkg" {
		t.Fatalf("cmd line: %s %#v", cmd, args)
	}
	cmd, args = desktopMCPCloudArgv("cmd", []string{"/c", `npx -y "@scope/pkg"`})
	if cmd != "npx" || len(args) != 2 || args[0] != "-y" || args[1] != "@scope/pkg" {
		t.Fatalf("quoted cmd line: %s %#v", cmd, args)
	}
	cmd, args = desktopMCPCloudArgv("cmd", []string{"/c", `"C:\Program Files\nodejs\npx.cmd" -y pkg`})
	if cmd != "npx" || len(args) != 2 || args[0] != "-y" || args[1] != "pkg" {
		t.Fatalf("quoted npx path: %s %#v", cmd, args)
	}
	cmd, args = desktopMCPCloudArgv("python", []string{"server.py"})
	if cmd != "python3" || len(args) != 1 || args[0] != "server.py" {
		t.Fatalf("python: %s %#v", cmd, args)
	}
	cmd, args = desktopMCPCloudArgv("py", []string{"-3.11", "server.py"})
	if cmd != "python3" || len(args) != 1 || args[0] != "server.py" {
		t.Fatalf("py: %s %#v", cmd, args)
	}
	cmd, args = desktopMCPCloudArgv(`C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`, []string{"-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", "npx.cmd -y pkg"})
	if cmd != "npx" || len(args) != 2 || args[0] != "-y" || args[1] != "pkg" {
		t.Fatalf("powershell: %s %#v", cmd, args)
	}
	cmd, args = desktopMCPCloudArgv("powershell", []string{"-Command", "{ npx -y pkg }"})
	if cmd != "powershell" {
		t.Fatalf("powershell script: %s %#v", cmd, args)
	}
}

func TestDesktopMCPStdioDriverFitsBashLimit(t *testing.T) {
	script := "chmod 0600 '/tmp/j'\npython3 - '/tmp/j' '/tmp/j.out' <<'PY'\n" + desktopMCPStdioPy + "\nPY\n"
	if len(script) > 16<<10 {
		t.Fatalf("stdio driver is %d bytes", len(script))
	}
}

func TestDesktopMCPStdioAnswersServerRequests(t *testing.T) {
	py := desktopTestPython(t)
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "driver.py")
	if err := os.WriteFile(scriptPath, []byte(strings.TrimSpace(desktopMCPStdioPy)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	serverPath := filepath.Join(dir, "server.py")
	if err := os.WriteFile(serverPath, []byte(desktopMCPPingServerPy), 0o644); err != nil {
		t.Fatal(err)
	}
	jobPath := filepath.Join(dir, "job.json")
	outPath := filepath.Join(dir, "out.json")
	job, err := json.Marshal(map[string]any{
		"command": py,
		"args":    []string{serverPath},
		"method":  "tools/list",
		"params":  map[string]any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(jobPath, job, 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, py, scriptPath, jobPath, outPath)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	text, readErr := os.ReadFile(outPath)
	if err != nil || readErr != nil {
		t.Fatalf("driver: %v %s %s", err, output, text)
	}
	if !strings.Contains(string(text), `"name": "ping"`) || strings.Contains(string(text), `"error"`) {
		t.Fatalf("result: %s\n%s", text, output)
	}
}

func desktopTestPython(t *testing.T) string {
	t.Helper()
	for _, name := range []string{"python", "python3"} {
		if path, err := exec.LookPath(name); err == nil {
			return path
		}
	}
	t.Skip("python is not installed")
	return ""
}

const desktopMCPPingServerPy = `
import json, sys
def read():
    while True:
        line = sys.stdin.readline()
        if not line:
            return None
        line = line.strip()
        if not line:
            continue
        try:
            return json.loads(line)
        except Exception:
            continue

def write(obj):
    sys.stdout.write(json.dumps(obj) + "\n")
    sys.stdout.flush()

def expect_reply(want):
    msg = read()
    if not msg or msg.get("id") != want or ("result" not in msg and "error" not in msg):
        sys.stderr.write("bad reply %r\n" % (msg,))
        sys.exit(3)
    return msg

msg = read()
if not msg or msg.get("method") != "initialize":
    sys.exit(4)
write({"jsonrpc": "2.0", "id": "ping-1", "method": "ping"})
expect_reply("ping-1")
write({"jsonrpc": "2.0", "id": msg["id"], "result": {"protocolVersion": "2025-06-18", "capabilities": {}, "serverInfo": {"name": "t", "version": "0"}}})
while True:
    msg = read()
    if not msg:
        sys.exit(5)
    if msg.get("method") == "notifications/initialized":
        continue
    if msg.get("method") == "tools/list":
        break
    sys.stderr.write("unexpected %r\n" % (msg,))
    sys.exit(6)
write({"jsonrpc": "2.0", "id": 9, "method": "roots/list"})
got = expect_reply(9)
if "roots" not in (got.get("result") or {}):
    sys.exit(7)
write({"jsonrpc": "2.0", "id": "ask", "method": "sampling/createMessage"})
got = expect_reply("ask")
if (got.get("error") or {}).get("code") != -32601:
    sys.exit(8)
write({"jsonrpc": "2.0", "id": msg["id"], "result": {"tools": [{"name": "ping", "description": "pong"}]}})
`

func TestDesktopCapMCPHTTPAndStdio(t *testing.T) {
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		text := string(body)
		auth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(text, `"initialize"`):
			w.Header().Set("Mcp-Session-Id", "sess-http")
			_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-03-26"}}`)
		case strings.Contains(text, "notifications/initialized"):
			w.WriteHeader(http.StatusAccepted)
		case strings.Contains(text, "tools/list"):
			_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":2,"result":{"tools":[{"name":"ping","inputSchema":{"type":"object","properties":{"q":{"type":"string"}}}}]}}`)
		default:
			_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":3,"result":{"content":[{"type":"text","text":"pong"}]}}`)
		}
	}))
	defer srv.Close()
	doc := botcapability.Document{MCP: []botcapability.Server{
		{ID: "search", Name: "Search", Transport: "http", URL: srv.URL, AuthSecret: "sekret", ReadTools: []string{"ping"}},
		{ID: "files", Name: "Files", Transport: "stdio", Command: "npx", ReadTools: []string{"list"}},
	}}
	scope := agentruntime.Scope{TenantID: "t", UserID: "alice", InstanceID: "bot"}
	out, err := operateDesktop(capabilityCtx(t, doc, "execute"), scope, map[string]any{
		"action": "cap_mcp", "mcp_server": "search", "mcp_tool": "ping", "mcp_arguments": map[string]any{"q": "x"},
	})
	if err != nil || out != "pong" {
		t.Fatalf("http: %q %v", out, err)
	}
	if auth != "Bearer sekret" {
		t.Fatalf("auth %q", auth)
	}

	prev := desktopMCPStdio
	desktopMCPStdio = func(ctx context.Context, scope agentruntime.Scope, server botcapability.Server, method string, params any) (json.RawMessage, error) {
		if server.Command != "npx" {
			t.Fatalf("stdio %s %s", server.Command, method)
		}
		if method == "tools/list" {
			return json.RawMessage(`{"tools":[{"name":"list","inputSchema":{"type":"object","properties":{}}}]}`), nil
		}
		if method != "tools/call" {
			t.Fatalf("stdio method %s", method)
		}
		return json.RawMessage(`{"content":[{"type":"text","text":"local-pong"}]}`), nil
	}
	t.Cleanup(func() { desktopMCPStdio = prev })
	out, err = operateDesktop(capabilityCtx(t, doc, "execute"), scope, map[string]any{
		"action": "cap_mcp", "mcp_server": "Files", "mcp_tool": "list",
	})
	if err != nil || out != "local-pong" {
		t.Fatalf("stdio: %q %v", out, err)
	}

	listSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":{"tools":[{"name":"ping","description":"say pong","inputSchema":{"type":"object","properties":{"q":{"type":"string"}},"required":["q"]}},{"name":"search","description":"look up"}]}}`)
	}))
	defer listSrv.Close()
	listDoc := botcapability.Document{MCP: []botcapability.Server{
		{ID: "search", Name: "Search", Transport: "http", URL: listSrv.URL},
	}}
	listed, err := operateDesktop(capabilityCtx(t, listDoc, "plan"), scope, map[string]any{
		"action": "cap_mcp", "mcp_server": "search",
	})
	if err != nil || !strings.Contains(listed, "ping") || !strings.Contains(listed, "say pong") || !strings.Contains(listed, "q:string*") || strings.Contains(listed, "mcp_tool is required") {
		t.Fatalf("list: %q %v", listed, err)
	}
}
