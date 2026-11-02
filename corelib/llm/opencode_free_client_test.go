package llm

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestOpenCodeFreeClientBodyKeepsFreeModel(t *testing.T) {
	body, err := openCodeFreeClientBody([]byte(`{"model":"deepseek-v4.1-flash-free","messages":[{"role":"user","content":"Reply with exactly PING"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	if !strings.Contains(text, `"model":"deepseek-v4.1-flash-free"`) {
		t.Fatalf("model id changed: %s", text[:200])
	}
	for _, want := range []string{`"name":"shell"`, `"name":"read"`, `"stream":true`, "You are an AI agent running in OpenCode"} {
		if !strings.Contains(text, want) {
			t.Fatalf("body missing %s", want)
		}
	}
	today := time.Now().Format("Mon Jan 02 2006")
	if !strings.Contains(text, "Today's date: "+today) {
		t.Fatalf("date line is not %s", today)
	}
	if strings.Contains(text, "Sun Nov 01 2026") {
		t.Fatal("frozen prompt date is still sent")
	}
	if !strings.Contains(text, "<env>") || strings.Contains(text, `\u003cenv`) {
		t.Fatal("system prompt tags were escaped")
	}
}

func TestOpenCodeFreeWorkspaceOverrideRewritesEnvironment(t *testing.T) {
	body, err := openCodeFreeClientBody([]byte(`{"model":"deepseek-v4.1-flash-free","messages":[{"role":"user","content":"hi"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if openCodeFreeProjectID(context.Background()) != corelib.OpenCodeProjectGlobal {
		t.Fatal("default project changed")
	}
	ctx := OpenCodeFreeWithProject(context.Background(), "1795298873c0add7aff53b8f200fb562a7d762df")
	if openCodeFreeProjectID(ctx) != "1795298873c0add7aff53b8f200fb562a7d762df" {
		t.Fatalf("project = %s", openCodeFreeProjectID(ctx))
	}
	got := string(openCodeFreeApplyWorkspace(body, openCodeFreeWorkspace(OpenCodeFreeWithWorkspace(ctx, `D:\workprj\aicoder`, true))))
	for _, want := range []string{`Working directory: D:\\workprj\\aicoder`, `Workspace root folder: D:\\workprj\\aicoder`, "Is directory a git repo: yes"} {
		if !strings.Contains(got, want) {
			t.Fatalf("body missing %s", want)
		}
	}
	if strings.Contains(got, `D:\\oc-empty`) {
		t.Fatal("captured workspace is still sent")
	}
	quoted := string(openCodeFreeApplyWorkspace(body, openCodeFreeWorkspace(OpenCodeFreeWithWorkspace(context.Background(), `D:\work "proj"`, true))))
	if !json.Valid([]byte(quoted)) {
		t.Fatal("workspace quote broke the JSON")
	}
	if !strings.Contains(quoted, `D:\\work \"proj\"`) {
		t.Fatal("workspace quote was not escaped")
	}
}

func TestOpenCodeGitProjectReadsDesktopFile(t *testing.T) {
	root := t.TempDir()
	gitDir := filepath.Join(root, ".git")
	if err := os.Mkdir(gitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	const id = "1795298873c0add7aff53b8f200fb562a7d762df"
	if err := os.WriteFile(filepath.Join(gitDir, "opencode"), []byte(id+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(root, "corelib", "llm")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	gotID, gotRoot, git := openCodeGitProject(sub)
	if gotID != id || gotRoot != root || !git {
		t.Fatalf("openCodeGitProject = %s %s %v", gotID, gotRoot, git)
	}
	ctx := OpenCodeFreeWithWorkspace(context.Background(), sub, true)
	if openCodeFreeProjectID(ctx) != id {
		t.Fatalf("project = %s", openCodeFreeProjectID(ctx))
	}
}

func TestOpenCodeFreeLiveShapeUsesGitProject(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	id, root, git := openCodeGitProject(wd)
	if id == "" {
		t.Skip("desktop project file is not present")
	}
	if len(id) != 40 || root == "" || !git {
		t.Fatalf("openCodeGitProject = %s %s %v", id, root, git)
	}
	body, err := openCodeFreeClientBody([]byte(`{"model":"deepseek-v4.1-flash-free","messages":[{"role":"user","content":"Reply with exactly PING"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	ctx := OpenCodeFreeWithWorkspace(context.Background(), root, true)
	if openCodeFreeProjectID(ctx) != id {
		t.Fatalf("project = %s", openCodeFreeProjectID(ctx))
	}
	text := string(openCodeFreeApplyWorkspace(body, openCodeFreeWorkspace(ctx)))
	workspace := "Working directory: " + strings.ReplaceAll(root, `\`, `\\`)
	for _, want := range []string{`"model":"deepseek-v4.1-flash-free"`, workspace, "Is directory a git repo: yes"} {
		if !strings.Contains(text, want) {
			t.Fatalf("body missing %s", want)
		}
	}
	if strings.Contains(text, "oc-empty") {
		t.Fatal("captured workspace is still sent")
	}
}

func TestOpenCodeFreeAuthorizationUsesAccountKey(t *testing.T) {
	if got := openCodeFreeAuthorization(""); got != "Bearer public" {
		t.Fatalf("empty = %s", got)
	}
	if got := openCodeFreeAuthorization("public"); got != "Bearer public" {
		t.Fatalf("public = %s", got)
	}
	if got := openCodeFreeAuthorization("oc_sk_example"); got != "Bearer oc_sk_example" {
		t.Fatalf("account = %s", got)
	}
}

func TestOpenCodeFreeCallContextKeepsKeyAndWorkspace(t *testing.T) {
	base := OpenCodeFreeWithWorkspace(context.Background(), `D:\workprj\aicoder`, true)
	ctx := OpenCodeFreeCallContext(base, "oc_sk_example")
	if got := openCodeFreeAuthorization(openCodeFreeKey(ctx)); got != "Bearer oc_sk_example" {
		t.Fatalf("authorization = %s", got)
	}
	workspace := openCodeFreeWorkspace(ctx)
	if workspace.dir != `D:\workprj\aicoder` || !workspace.git {
		t.Fatalf("workspace = %+v", workspace)
	}
	if openCodeFreeProjectID(ctx) == "" {
		t.Fatal("project id is empty")
	}
}

func TestOpenCodeFreeWithoutLocalWorkspaceLeavesPromptDirectory(t *testing.T) {
	ctx := OpenCodeFreeCallContext(OpenCodeFreeWithoutLocalWorkspace(context.Background()), "oc_sk_example")
	if got := openCodeFreeAuthorization(openCodeFreeKey(ctx)); got != "Bearer oc_sk_example" {
		t.Fatalf("authorization = %s", got)
	}
	if openCodeFreeWorkspace(ctx).dir != "" {
		t.Fatalf("workspace = %s", openCodeFreeWorkspace(ctx).dir)
	}
	if openCodeFreeProjectID(ctx) != corelib.OpenCodeProjectGlobal {
		t.Fatalf("project = %s", openCodeFreeProjectID(ctx))
	}
}

func TestOpenCodeFreeClientBodyRewritesRetiredModel(t *testing.T) {
	body, err := openCodeFreeClientBody([]byte(`{"model":"opencode/big-pickle","messages":[{"role":"user","content":"hi"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"model":"glm-5.3-flash-free"`) {
		t.Fatalf("wire model = %s", body)
	}
}

func TestOpenCodeFreeDirectTransportLeavesTestDoubles(t *testing.T) {
	if !openCodeFreeUseDirect(nil) || !openCodeFreeUseDirect(&http.Client{}) {
		t.Fatal("plain client should dial directly")
	}
	if !openCodeFreeUseDirect(&http.Client{Transport: &http.Transport{}}) {
		t.Fatal("http.Transport should dial directly")
	}
	if openCodeFreeUseDirect(&http.Client{Transport: stubRoundTrip{}}) {
		t.Fatal("custom transport must stay on the test server")
	}
}

func TestOpenCodeFreeDirectAppliesToMaclawGUIProvider(t *testing.T) {
	cfg := corelib.MaclawLLMConfig{
		URL:          "https://opencode.ai/zen/v1",
		Model:        "glm-5.3-flash-free",
		ProviderName: "OpenCode",
		AgentType:    "OpenCode",
		Protocol:     "openai",
	}
	_, body, err := BuildOpenAIChatRequestData(cfg, []interface{}{
		map[string]interface{}{"role": "user", "content": "hello"},
	}, OpenAIChatRequestOptions{})
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: &http.Transport{}}
	if !openCodeFreeDirectApplies(cfg, body, client) {
		t.Fatal("Maclaw GUI OpenCode provider should use the identified client")
	}
	if !strings.Contains(string(body), `"model":"glm-5.3-flash-free"`) {
		t.Fatalf("wire model = %s", body)
	}
	paid := cfg
	paid.Model = "deepseek-v4.1-flash"
	_, paidBody, err := BuildOpenAIChatRequestData(paid, []interface{}{
		map[string]interface{}{"role": "user", "content": "hello"},
	}, OpenAIChatRequestOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if openCodeFreeDirectApplies(paid, paidBody, client) {
		t.Fatal("metered model must stay on the Go gateway")
	}
	rawRetired := []byte(`{"model":"big-pickle","messages":[{"role":"user","content":"hi"}]}`)
	if !openCodeFreeDirectApplies(cfg, rawRetired, client) {
		t.Fatal("retired big-pickle id should use the identified client")
	}
}

func TestOpenCodeFreeSessionIDsAreNotShared(t *testing.T) {
	first := openCodeFreeSessionID()
	second := openCodeFreeSessionID()
	if first == "" || first == second {
		t.Fatalf("session ids = %q %q", first, second)
	}
	for _, id := range []string{first, second} {
		if len(id) < 4 || id[:4] != "ses_" {
			t.Fatalf("session id = %q", id)
		}
	}
}

func TestOpenCodeFreeResponseUsableAcceptsToolCall(t *testing.T) {
	if openCodeFreeResponseUsable(nil) || openCodeFreeResponseUsable(&Response{}) {
		t.Fatal("empty response")
	}
	if openCodeFreeResponseUsable(&Response{Choices: []Choice{{Message: Message{Role: "assistant"}}}}) {
		t.Fatal("blank content")
	}
	if !openCodeFreeResponseUsable(&Response{Choices: []Choice{{Message: Message{Content: "PING"}}}}) {
		t.Fatal("text")
	}
	if !openCodeFreeResponseUsable(&Response{Choices: []Choice{{Message: Message{ToolCalls: []ToolCall{{ID: "1"}}}}}}) {
		t.Fatal("tool call")
	}
}

func TestOpenCodeFreeClientBodyKeepsToolTurn(t *testing.T) {
	body, err := openCodeFreeClientBody([]byte(`{"model":"deepseek-v4.1-flash-free","messages":[` +
		`{"role":"system","content":"You are Codex"},` +
		`{"role":"user","content":"list files"},` +
		`{"role":"assistant","content":null,"tool_calls":[{"function":{"name":"shell","arguments":"{\"command\":\"dir\"}"}}]},` +
		`{"role":"tool","content":"a.txt"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	for _, want := range []string{`"role":"assistant"`, "shell", "dir", `"role":"user"`, "a.txt", "list files"} {
		if !strings.Contains(text, want) {
			t.Fatalf("body missing %s: %s", want, text)
		}
	}
	if strings.Contains(text, "You are Codex") {
		t.Fatal("caller system prompt was kept")
	}
	var parsed struct {
		Messages []struct {
			Role string `json:"role"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatal(err)
	}
	users := 0
	for _, msg := range parsed.Messages {
		if msg.Role == "user" {
			users++
		}
	}
	if users != 2 {
		t.Fatalf("user messages = %d", users)
	}
}

func TestDialOpenCodeProxyCONNECT(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	got := make(chan string, 1)
	release := make(chan struct{})
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		buf := make([]byte, 512)
		n, _ := conn.Read(buf)
		got <- string(buf[:n])
		_, _ = io.WriteString(conn, "HTTP/1.1 200 Connection Established\r\n\r\nHELLO")
		<-release
	}()
	proxyURL, err := url.Parse("http://user:secret@" + ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	conn, err := dialOpenCodeProxyCONNECT(context.Background(), &net.Dialer{}, proxyURL, "opencode.ai:443")
	if err != nil {
		close(release)
		t.Fatal(err)
	}
	defer conn.Close()
	defer close(release)
	buf := make([]byte, 5)
	n, err := io.ReadFull(conn, buf)
	if err != nil || string(buf[:n]) != "HELLO" {
		t.Fatalf("tunnel bytes = %q err=%v", buf[:n], err)
	}
	request := <-got
	if !strings.Contains(request, "CONNECT opencode.ai:443 HTTP/1.1") || !strings.Contains(request, "Proxy-Authorization: Basic") {
		t.Fatalf("CONNECT request = %q", request)
	}
}

type stubRoundTrip struct{}

func (stubRoundTrip) RoundTrip(*http.Request) (*http.Response, error) { return nil, nil }
