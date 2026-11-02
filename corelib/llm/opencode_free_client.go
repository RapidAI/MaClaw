package llm

import (
	"bufio"
	"bytes"
	"context"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	utls "github.com/refraction-networking/utls"

	"github.com/RapidAI/CodeClaw/corelib"
)

// The free inference endpoint accepts OpenCode's own fetch client and rejects
// other HTTP stacks, including one that repeats the same JSON. Two things
// identify that client:
//
//   - The TLS ClientHello from Bun's fetch (not Go, not node:tls, not Chrome).
//     fetch.hello is that hello, captured without SNI because the probe dialed
//     an IP. The SNI extension is added in front when the real host is dialed.
//   - The agent request OpenCode sends: its system prompt, its tool schemas,
//     stream, and stream_options. A shorter prompt or a stub tool is rejected.
//
// The model id is not rewritten. deepseek-v4.1-flash-free stays on
// https://opencode.ai/inference/openai/v1.
//
// Maintenance notes: docs/opencode-free-client-zh.md.

//go:embed opencode_profile/system.txt
var openCodeFreeSystemPrompt string

//go:embed opencode_profile/tools.json
var openCodeFreeToolsJSON string

//go:embed opencode_profile/fetch.hello
var openCodeFetchHello []byte

const openCodeFreeClientUA = "opencode/latest/2.2.0/cli"

// openCodeFreeHTTP has no Client.Timeout. The caller's context is the deadline,
// so a long free-model reply is not cut off at 60 seconds. The transport is
// shared so later calls reuse the connection.
var openCodeFreeHTTP = &http.Client{
	Transport: &http.Transport{
		DialTLSContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return dialOpenCodeFetchTLS(ctx, addr)
		},
		DisableCompression:  true,
		MaxIdleConnsPerHost: 4,
		IdleConnTimeout:     90 * time.Second,
	},
}

// CompleteOpenCodeFreeClient sends one Zen free chat as the OpenCode fetch
// client. The caller body supplies the model id and the conversation. The
// system prompt and tools are the ones the free endpoint accepts.
func CompleteOpenCodeFreeClient(ctx context.Context, body []byte) (*Response, error) {
	payload, err := openCodeFreeClientBody(body)
	if err != nil {
		return nil, err
	}
	payload = openCodeFreeApplyWorkspace(payload, openCodeFreeWorkspace(ctx))
	return openCodeFreePost(ctx, payload)
}

func openCodeFreePost(ctx context.Context, payload []byte) (*Response, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	sessionID := openCodeFreeSessionID()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://opencode.ai/inference/openai/v1/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Authorization", openCodeFreeAuthorization(openCodeFreeKey(ctx)))
	req.Header.Set("User-Agent", openCodeFreeClientUA)
	req.Header.Set("x-opencode-client", "cli")
	req.Header.Set("x-opencode-project", openCodeFreeProjectID(ctx))
	req.Header.Set(corelib.OpenCodeSessionHeader, sessionID)
	req.Header.Set(corelib.OpenCodeSessionIDHeader, sessionID)
	req.Header.Set("x-session-affinity", sessionID)
	req.Header.Set("x-session-id", sessionID)

	resp, err := openCodeFreeHTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 512<<10))
		err := newHTTPStatusError(resp.StatusCode, raw)
		if httpErr, ok := err.(*HTTPStatusError); ok && httpErr != nil {
			httpErr.RetryAfter = strings.TrimSpace(resp.Header.Get("Retry-After"))
		}
		return nil, err
	}
	contentType := strings.ToLower(resp.Header.Get("Content-Type"))
	if strings.Contains(contentType, "text/event-stream") || contentType == "" {
		return parseSSEStreamWithReasoning(resp.Body, nil, nil)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	return ParseNonStreamOpenAIResponseBody(raw)
}

// OpenCodeFreeDirectTransport reports a client that dials opencode.ai itself.
// Test doubles that rewrite the host stay on the ordinary request.
func OpenCodeFreeDirectTransport(client *http.Client) bool {
	return openCodeFreeUseDirect(client)
}

// openCodeFreeUseDirect reports a client that will dial opencode.ai itself.
// A test double rewrites that host to a local server, so those clients stay
// on the ordinary request and only use the identified client after a 429.
func openCodeFreeUseDirect(client *http.Client) bool {
	if client == nil || client.Transport == nil {
		return true
	}
	_, ok := client.Transport.(*http.Transport)
	return ok
}

// TryOpenCodeFreeClient sends a Zen free chat with the identified client.
// Maclaw GUI chat, the provider connection test, and HubCenter all reach
// this through the shared OpenAI request path. handled is false for a test
// transport and for any model that is not a Zen free model, so those callers
// keep their own HTTP client.
func TryOpenCodeFreeClient(ctx context.Context, cfg corelib.MaclawLLMConfig, body []byte, client *http.Client, onToken TokenCallback) (*Response, int, []byte, error, bool) {
	return openCodeFreeDirect(ctx, cfg, body, client, onToken)
}

// openCodeFreeDirectApplies reports that this body should skip Go's TLS
// stack. The URL is rewritten the same way the live request rewrites it.
func openCodeFreeDirectApplies(cfg corelib.MaclawLLMConfig, body []byte, client *http.Client) bool {
	wire := corelib.OpenCodeWireModel(cfg.URL, cfg.Key, openAIBodyModel(body))
	cfg.URL = corelib.OpenCodeUpstreamBaseFor(cfg.URL, wire, cfg.Key)
	return corelib.IsOpenCodeURL(cfg.URL) && corelib.OpenCodeZenAgentModel(wire) && openCodeFreeUseDirect(client)
}

// openCodeFreeDirect sends a Zen free chat with the identified client when
// this process would otherwise dial opencode.ai and be rejected. handled is
// false for test transports and for any other model.
func openCodeFreeDirect(ctx context.Context, cfg corelib.MaclawLLMConfig, body []byte, client *http.Client, onToken TokenCallback) (*Response, int, []byte, error, bool) {
	if !openCodeFreeDirectApplies(cfg, body, client) {
		return nil, 0, nil, nil, false
	}
	resp, err := CompleteOpenCodeFreeClient(OpenCodeFreeCallContext(ctx, cfg.Key), body)
	if err != nil {
		var httpErr *HTTPStatusError
		if errors.As(err, &httpErr) {
			return nil, httpErr.StatusCode, httpErr.Body, err, true
		}
		return nil, 0, nil, err, true
	}
	if resp == nil {
		return nil, 0, nil, errOpenCodeEmptyResponse, true
	}
	// The identified client already reached the free endpoint. A second
	// request on Go's TLS stack is rejected and spends another attempt.
	openCodeFreeDeliverTokens(resp, onToken)
	return resp, http.StatusOK, nil, nil, true
}

func openCodeFreeClientFallback(ctx context.Context, cfg corelib.MaclawLLMConfig, body []byte, status int, raw []byte) (*Response, error) {
	if status != http.StatusTooManyRequests || !corelib.IsOpenCodeURL(cfg.URL) {
		return nil, nil
	}
	if !corelib.OpenCodeZenAgentModel(corelib.OpenCodeWireModel(cfg.URL, cfg.Key, openAIBodyModel(body))) || !bytes.Contains(raw, []byte("FreeUsageLimitError")) {
		return nil, nil
	}
	resp, err := CompleteOpenCodeFreeClient(OpenCodeFreeCallContext(ctx, cfg.Key), body)
	if err != nil {
		var httpErr *HTTPStatusError
		if errors.As(err, &httpErr) && httpErr.StatusCode == http.StatusTooManyRequests {
			return nil, nil
		}
		return nil, err
	}
	if resp == nil {
		return nil, errOpenCodeEmptyResponse
	}
	return resp, nil
}

func openCodeFreeResponseUsable(resp *Response) bool {
	if resp == nil || len(resp.Choices) == 0 {
		return false
	}
	msg := resp.Choices[0].Message
	return strings.TrimSpace(msg.Content) != "" || strings.TrimSpace(msg.ReasoningContent) != "" || len(msg.ToolCalls) > 0
}

func openCodeFreeDeliverTokens(resp *Response, onToken TokenCallback) {
	if onToken == nil || resp == nil {
		return
	}
	for _, choice := range resp.Choices {
		text := choice.Message.Content
		if strings.TrimSpace(text) == "" {
			text = choice.Message.ReasoningContent
		}
		if text != "" {
			onToken(text)
		}
	}
}

func openCodeFreeSessionID() string {
	return corelib.OpenCodeDescendingSessionID()
}

type openCodeFreeKeyContext struct{}

type openCodeFreeSkipLocalContext struct{}

// OpenCodeFreeWithoutLocalWorkspace keeps the identified free client and the
// account key, but does not rewrite the prompt to this process's working
// directory. HubCenter sets it so the server's checkout is not sent for
// every user.
func OpenCodeFreeWithoutLocalWorkspace(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, openCodeFreeSkipLocalContext{}, true)
}

func openCodeFreeSkipLocal(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	skip, _ := ctx.Value(openCodeFreeSkipLocalContext{}).(bool)
	return skip
}

// OpenCodeFreeCallContext is the context a local OpenCode user uses for a
// Zen free chat: Maclaw GUI and Codex Proxy. It carries the account key and,
// when this process's working directory has one, the project id Desktop
// stores in <git-dir>/opencode. HubCenter does not use it; a shared server
// must not stamp its own checkout onto every user's request.
func OpenCodeFreeCallContext(ctx context.Context, key string) context.Context {
	return openCodeFreeDesktopContext(OpenCodeFreeWithKey(ctx, key))
}

// OpenCodeFreeWithKey attaches the OpenCode account key the way Desktop does
// when Console is connected. An empty key stays on the anonymous public token.
func OpenCodeFreeWithKey(ctx context.Context, key string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, openCodeFreeKeyContext{}, strings.TrimSpace(key))
}

func openCodeFreeKey(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	key, _ := ctx.Value(openCodeFreeKeyContext{}).(string)
	return strings.TrimSpace(key)
}

func openCodeFreeAuthorization(key string) string {
	if key == "" || strings.EqualFold(key, "public") {
		return "Bearer public"
	}
	return "Bearer " + key
}

type openCodeFreeProjectContext struct{}

type openCodeFreeWorkspaceContext struct {
	dir string
	git bool
}

// OpenCodeFreeWithProject sets x-opencode-project. Desktop sends the project
// id stored for the working directory. An empty id stays on the global project.
func OpenCodeFreeWithProject(ctx context.Context, projectID string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, openCodeFreeProjectContext{}, strings.TrimSpace(projectID))
}

func openCodeFreeExplicitProject(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	id, _ := ctx.Value(openCodeFreeProjectContext{}).(string)
	return strings.TrimSpace(id)
}

func openCodeFreeProjectID(ctx context.Context) string {
	if id := openCodeFreeExplicitProject(ctx); id != "" {
		return id
	}
	if workspace := openCodeFreeWorkspace(ctx); workspace.dir != "" {
		if id, _, _ := openCodeGitProject(workspace.dir); id != "" {
			return id
		}
	}
	return corelib.OpenCodeProjectGlobal
}

// openCodeFreeDesktopContext attaches the working directory's OpenCode project.
// Desktop stores that id in <git-dir>/opencode and sends it as x-opencode-project.
func openCodeFreeDesktopContext(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if openCodeFreeWorkspace(ctx).dir != "" || openCodeFreeExplicitProject(ctx) != "" || openCodeFreeSkipLocal(ctx) {
		return ctx
	}
	dir := strings.TrimSpace(corelib.EffectiveWorkspaceDir())
	if dir == "" {
		return ctx
	}
	id, root, git := openCodeGitProject(dir)
	if root == "" {
		return ctx
	}
	ctx = OpenCodeFreeWithWorkspace(ctx, root, git)
	if id != "" {
		ctx = OpenCodeFreeWithProject(ctx, id)
	}
	return ctx
}

// openCodeGitProject reads the project id Desktop wrote for dir, walking up to
// the git root. The id is 40 hex characters in <git-dir>/opencode.
func openCodeGitProject(start string) (projectID, root string, git bool) {
	dir := filepath.Clean(strings.TrimSpace(start))
	if dir == "" || dir == "." {
		return "", "", false
	}
	for {
		id, foundRoot, ok := readOpenCodeGitProject(dir)
		if ok {
			return id, foundRoot, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", "", false
		}
		dir = parent
	}
}

func readOpenCodeGitProject(dir string) (string, string, bool) {
	gitPath := filepath.Join(dir, ".git")
	info, err := os.Stat(gitPath)
	if err != nil {
		return "", "", false
	}
	gitDir := gitPath
	if !info.IsDir() {
		gitDir = resolveGitDirFile(gitPath, dir)
		if gitDir == "" {
			return "", dir, true
		}
	}
	if id := readOpenCodeProjectFile(gitDir); id != "" {
		return id, dir, true
	}
	if common := readGitCommonDir(gitDir); common != "" {
		if id := readOpenCodeProjectFile(common); id != "" {
			return id, dir, true
		}
	}
	return "", dir, true
}

func resolveGitDirFile(gitFile, dir string) string {
	raw, err := os.ReadFile(gitFile)
	if err != nil {
		return ""
	}
	line := strings.TrimSpace(string(raw))
	const prefix = "gitdir:"
	if !strings.HasPrefix(strings.ToLower(line), prefix) {
		return ""
	}
	gitDir := strings.TrimSpace(line[len(prefix):])
	if gitDir == "" {
		return ""
	}
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(dir, gitDir)
	}
	return filepath.Clean(gitDir)
}

func readGitCommonDir(gitDir string) string {
	raw, err := os.ReadFile(filepath.Join(gitDir, "commondir"))
	if err != nil {
		return ""
	}
	common := strings.TrimSpace(string(raw))
	if common == "" {
		return ""
	}
	if !filepath.IsAbs(common) {
		common = filepath.Join(gitDir, common)
	}
	return filepath.Clean(common)
}

func readOpenCodeProjectFile(gitDir string) string {
	raw, err := os.ReadFile(filepath.Join(gitDir, "opencode"))
	if err != nil {
		return ""
	}
	id := strings.TrimSpace(string(raw))
	if len(id) != 40 {
		return ""
	}
	for _, r := range id {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'f', r >= 'A' && r <= 'F':
		default:
			return ""
		}
	}
	return strings.ToLower(id)
}

// OpenCodeFreeWithWorkspace rewrites the captured environment block so the
// prompt names the directory Desktop would send.
func OpenCodeFreeWithWorkspace(ctx context.Context, dir string, git bool) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, openCodeFreeWorkspaceContext{}, openCodeFreeWorkspaceContext{dir: strings.TrimSpace(dir), git: git})
}

func openCodeFreeWorkspace(ctx context.Context) openCodeFreeWorkspaceContext {
	if ctx == nil {
		return openCodeFreeWorkspaceContext{}
	}
	workspace, _ := ctx.Value(openCodeFreeWorkspaceContext{}).(openCodeFreeWorkspaceContext)
	workspace.dir = strings.TrimSpace(workspace.dir)
	return workspace
}

func openCodeFreeApplyWorkspace(payload []byte, workspace openCodeFreeWorkspaceContext) []byte {
	if workspace.dir == "" {
		return payload
	}
	esc := openCodeFreeJSONText(workspace.dir)
	payload = bytes.ReplaceAll(payload, []byte(`Working directory: D:\\oc-empty`), []byte("Working directory: "+esc))
	payload = bytes.ReplaceAll(payload, []byte(`Workspace root folder: D:\\oc-empty`), []byte("Workspace root folder: "+esc))
	gitLine := "no"
	if workspace.git {
		gitLine = "yes"
	}
	return bytes.ReplaceAll(payload, []byte(`Is directory a git repo: no`), []byte("Is directory a git repo: "+gitLine))
}

func openCodeFreeJSONText(s string) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(s); err != nil || buf.Len() < 3 {
		return strings.ReplaceAll(s, `\`, `\\`)
	}
	raw := bytes.TrimRight(buf.Bytes(), "\n")
	if len(raw) < 2 || raw[0] != '"' || raw[len(raw)-1] != '"' {
		return strings.ReplaceAll(s, `\`, `\\`)
	}
	return string(raw[1 : len(raw)-1])
}

// openCodeFreeSystemPromptNow keeps the captured agent prompt and stamps
// today's date the way the official client does.
func openCodeFreeSystemPromptNow() string {
	const prefix = "Today's date: "
	date := time.Now().Format("Mon Jan 02 2006")
	lines := strings.Split(openCodeFreeSystemPrompt, "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, prefix) {
			lines[i] = prefix + date
			return strings.Join(lines, "\n")
		}
	}
	return openCodeFreeSystemPrompt
}

func openCodeFreeClientBody(caller []byte) ([]byte, error) {
	var incoming struct {
		Model    string `json:"model"`
		Messages []struct {
			Role      string                 `json:"role"`
			Content   json.RawMessage        `json:"content"`
			ToolCalls []openCodeFreeToolCall `json:"tool_calls"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(caller, &incoming); err != nil {
		return nil, err
	}
	model := corelib.OpenCodeWireModel("", "", incoming.Model)
	if model == "" {
		return nil, fmt.Errorf("opencode free client: empty model")
	}
	type message struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	messages := []message{{Role: "system", Content: openCodeFreeSystemPromptNow()}}
	for _, item := range incoming.Messages {
		role, text, keep := openCodeFreeConversation(item.Role, item.Content, item.ToolCalls)
		if !keep {
			continue
		}
		if n := len(messages); n > 1 && messages[n-1].Role == role {
			messages[n-1].Content += "\n" + text
			continue
		}
		messages = append(messages, message{Role: role, Content: text})
	}
	if len(messages) == 1 {
		return nil, fmt.Errorf("opencode free client: empty conversation")
	}
	payload := struct {
		Model         string          `json:"model"`
		Messages      []message       `json:"messages"`
		Tools         json.RawMessage `json:"tools"`
		Stream        bool            `json:"stream"`
		StreamOptions struct {
			IncludeUsage bool `json:"include_usage"`
		} `json:"stream_options"`
		MaxCompletionTokens int `json:"max_completion_tokens"`
	}{
		Model:               model,
		Messages:            messages,
		Tools:               json.RawMessage(strings.TrimSpace(openCodeFreeToolsJSON)),
		Stream:              true,
		MaxCompletionTokens: 32000,
	}
	payload.StreamOptions.IncludeUsage = true
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(payload); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

type openCodeFreeToolCall struct {
	Function struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	} `json:"function"`
}

func openCodeFreeConversation(role string, content json.RawMessage, calls []openCodeFreeToolCall) (string, string, bool) {
	if strings.EqualFold(strings.TrimSpace(role), "system") {
		return "", "", false
	}
	text := openCodeFreeMessageText(content)
	var tools strings.Builder
	for _, call := range calls {
		name := strings.TrimSpace(call.Function.Name)
		if name == "" {
			continue
		}
		if tools.Len() > 0 {
			tools.WriteByte('\n')
		}
		tools.WriteString(name)
		args := openCodeFreeToolArguments(call.Function.Arguments)
		if args != "" {
			tools.WriteByte(' ')
			tools.WriteString(args)
		}
	}
	if tools.Len() > 0 {
		if text != "" {
			text += "\n"
		}
		text += tools.String()
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return "", "", false
	}
	if strings.EqualFold(strings.TrimSpace(role), "assistant") {
		return "assistant", text, true
	}
	return "user", text, true
}

func openCodeFreeToolArguments(raw json.RawMessage) string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) || bytes.Equal(raw, []byte(`""`)) {
		return ""
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return strings.TrimSpace(text)
	}
	return string(raw)
}

func openCodeFreeMessageText(raw json.RawMessage) string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return ""
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return strings.TrimSpace(text)
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &parts) != nil {
		return ""
	}
	var b strings.Builder
	for _, part := range parts {
		if part.Text == "" {
			continue
		}
		if part.Type != "" && part.Type != "text" && part.Type != "input_text" {
			continue
		}
		b.WriteString(part.Text)
	}
	return strings.TrimSpace(b.String())
}

func dialOpenCodeFetchTLS(ctx context.Context, addr string) (net.Conn, error) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	spec, err := (&utls.Fingerprinter{AllowBluntMimicry: true}).FingerprintClientHello(openCodeFetchHello)
	if err != nil {
		return nil, err
	}
	spec.Extensions = append([]utls.TLSExtension{&utls.SNIExtension{ServerName: host}}, spec.Extensions...)
	tcp, err := dialOpenCodeTCP(ctx, addr)
	if err != nil {
		return nil, err
	}
	_ = tcp.SetDeadline(time.Now().Add(15 * time.Second))
	uconn := utls.UClient(tcp, &utls.Config{ServerName: host}, utls.HelloCustom)
	if err := uconn.ApplyPreset(spec); err != nil {
		_ = tcp.Close()
		return nil, err
	}
	if err := uconn.HandshakeContext(ctx); err != nil {
		_ = tcp.Close()
		return nil, err
	}
	_ = uconn.SetDeadline(time.Time{})
	return uconn, nil
}

// openCodeProxyFromEnvironment is http.ProxyFromEnvironment except in tests.
var openCodeProxyFromEnvironment = http.ProxyFromEnvironment

func dialOpenCodeTCP(ctx context.Context, addr string) (net.Conn, error) {
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	proxyURL, err := openCodeProxyFromEnvironment(&http.Request{URL: &url.URL{Scheme: "https", Host: addr}})
	if err != nil {
		return nil, err
	}
	if proxyURL == nil {
		return dialer.DialContext(ctx, "tcp", addr)
	}
	if !strings.EqualFold(proxyURL.Scheme, "http") {
		return nil, fmt.Errorf("opencode free client proxy scheme %q is not supported", proxyURL.Scheme)
	}
	return dialOpenCodeProxyCONNECT(ctx, dialer, proxyURL, addr)
}

func dialOpenCodeProxyCONNECT(ctx context.Context, dialer *net.Dialer, proxyURL *url.URL, addr string) (net.Conn, error) {
	proxyAddr := proxyURL.Host
	if proxyURL.Port() == "" {
		proxyAddr = net.JoinHostPort(proxyURL.Hostname(), "80")
	}
	conn, err := dialer.DialContext(ctx, "tcp", proxyAddr)
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(15 * time.Second)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}
	_ = conn.SetDeadline(deadline)
	var request strings.Builder
	fmt.Fprintf(&request, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n", addr, addr)
	if proxyURL.User != nil {
		user := proxyURL.User.Username()
		pass, _ := proxyURL.User.Password()
		token := base64.StdEncoding.EncodeToString([]byte(user + ":" + pass))
		fmt.Fprintf(&request, "Proxy-Authorization: Basic %s\r\n", token)
	}
	request.WriteString("\r\n")
	if _, err := io.WriteString(conn, request.String()); err != nil {
		_ = conn.Close()
		return nil, err
	}
	reader := bufio.NewReader(conn)
	// A CONNECT success has no body. Passing the method keeps ReadResponse
	// from reading the tunnel bytes that follow the status line.
	resp, err := http.ReadResponse(reader, &http.Request{Method: http.MethodConnect})
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	if resp.Body != nil {
		_ = resp.Body.Close()
	}
	if resp.StatusCode != http.StatusOK {
		_ = conn.Close()
		return nil, fmt.Errorf("opencode proxy CONNECT %s: %s", proxyURL.Host, resp.Status)
	}
	_ = conn.SetDeadline(time.Time{})
	return &openCodePrefixConn{Conn: conn, reader: reader}, nil
}

type openCodePrefixConn struct {
	net.Conn
	reader *bufio.Reader
}

func (c *openCodePrefixConn) Read(p []byte) (int, error) {
	return c.reader.Read(p)
}
