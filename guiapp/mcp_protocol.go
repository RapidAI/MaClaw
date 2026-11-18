package guiapp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
)

const (
	mcpProtocolPath            = "/mcp"
	mcpBodyLimit               = 2 * 1024 * 1024
	mcpProtocolVersionLatest   = "2025-11-25"
	mcpProtocolVersionFallback = mcpProtocolVersionLatest
)

// mcpTaskAPI is the session-service surface the MCP adapter calls.
// Methods stay unexported so Wails does not publish a second task API.
type mcpTaskAPI interface {
	mcpListModels(ctx context.Context) (mcpModelCatalog, error)
	mcpCreateTask(ctx context.Context, args mcpCreateArgs) (mcpTaskView, error)
	mcpGetTask(ctx context.Context, projectPath string) (mcpTaskView, error)
	mcpStopTask(ctx context.Context, projectPath string) (mcpTaskView, error)
	mcpSendCommand(ctx context.Context, projectPath, command string, wait bool) (mcpTaskView, error)
	mcpListTasks(ctx context.Context) ([]mcpTaskView, error)
}

type mcpCreateArgs struct {
	Name       string
	Prompt     string
	Mode       string
	WorkingDir string
	Provider   string
	Model      string
	Wait       bool
}

type mcpHTTP struct {
	token func() string
	api   mcpTaskAPI
	// lanAllowed reports the wildcard-bind opt-in state; nil means loopback
	// only. LAN clients still need the Bearer key.
	lanAllowed func() bool
}

func newMCPHTTP(token func() string, api mcpTaskAPI) http.Handler {
	return newMCPHTTPLAN(token, api, nil)
}

func newMCPHTTPLAN(token func() string, api mcpTaskAPI, lanAllowed func() bool) http.Handler {
	if token == nil {
		token = func() string { return "" }
	}
	return &mcpHTTP{token: token, api: api, lanAllowed: lanAllowed}
}

type mcpRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (h *mcpHTTP) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != mcpProtocolPath {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	lanOn := h.lanAllowed != nil && h.lanAllowed()
	if !mcpRemoteIsLoopback(r) && !lanOn {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if !mcpOriginAllowed(r.Header.Get("Origin"), lanOn) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if !secureTokenEqual(mcpRequestToken(r), h.token()) {
		w.Header().Set("WWW-Authenticate", "Bearer")
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, mcpBodyLimit))
	if err != nil {
		h.writeResult(w, r, json.RawMessage("null"), nil, &mcpRPCError{Code: -32700, Message: "request too large"})
		return
	}
	var req struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Method  string          `json:"method"`
		Params  json.RawMessage `json:"params"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		h.writeResult(w, r, json.RawMessage("null"), nil, &mcpRPCError{Code: -32700, Message: "parse error"})
		return
	}
	if mcpIsNotification(req.ID) {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	if strings.TrimSpace(req.Method) == "" {
		h.writeResult(w, r, req.ID, nil, &mcpRPCError{Code: -32600, Message: "invalid request"})
		return
	}
	result, rpcErr := h.dispatch(r.Context(), req.Method, req.Params)
	h.writeResult(w, r, req.ID, result, rpcErr)
}

func (h *mcpHTTP) dispatch(ctx context.Context, method string, params json.RawMessage) (any, *mcpRPCError) {
	switch method {
	case "initialize":
		return mcpInitializeResult(params), nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		return map[string]any{"tools": mcpToolDefinitions()}, nil
	case "tools/call":
		return h.callTool(ctx, params)
	default:
		return nil, &mcpRPCError{Code: -32601, Message: "method not found"}
	}
}

func (h *mcpHTTP) callTool(ctx context.Context, params json.RawMessage) (any, *mcpRPCError) {
	if h.api == nil {
		return mcpToolError("MCP 任务服务还没有准备好"), nil
	}
	var call struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if len(params) > 0 && string(params) != "null" {
		if err := json.Unmarshal(params, &call); err != nil {
			return nil, &mcpRPCError{Code: -32602, Message: "invalid params"}
		}
	}
	args, err := mcpArgumentsObject(call.Arguments)
	if err != nil {
		return nil, &mcpRPCError{Code: -32602, Message: "invalid params"}
	}
	name := strings.TrimSpace(call.Name)
	switch name {
	case "list_models":
		catalog, err := h.api.mcpListModels(ctx)
		if err != nil {
			return mcpToolError(err.Error()), nil
		}
		return mcpToolText(catalog, false), nil
	case "create_task":
		view, err := h.api.mcpCreateTask(ctx, mcpParseCreateArgs(args))
		if err != nil {
			return mcpToolError(err.Error()), nil
		}
		return mcpToolText(view, view.Status == mcpStatusFailed), nil
	case "get_task":
		path := mcpFirstString(args, "project_path", "projectPath")
		view, err := h.api.mcpGetTask(ctx, path)
		if err != nil {
			return mcpToolError(err.Error()), nil
		}
		return mcpToolText(view, view.Status == mcpStatusFailed), nil
	case "stop_task", "cancel_task":
		path := mcpFirstString(args, "project_path", "projectPath")
		view, err := h.api.mcpStopTask(ctx, path)
		if err != nil {
			return mcpToolError(err.Error()), nil
		}
		return mcpToolText(view, view.Status == mcpStatusFailed), nil
	case "send_command":
		path := mcpFirstString(args, "project_path", "projectPath")
		command := mcpFirstString(args, "command", "text", "prompt", "message")
		view, err := h.api.mcpSendCommand(ctx, path, command, mcpLookupBool(args, "wait"))
		if err != nil {
			return mcpToolError(err.Error()), nil
		}
		return mcpToolText(view, view.Status == mcpStatusFailed), nil
	case "list_tasks":
		tasks, err := h.api.mcpListTasks(ctx)
		if err != nil {
			return mcpToolError(err.Error()), nil
		}
		if tasks == nil {
			tasks = []mcpTaskView{}
		}
		return mcpToolText(map[string]any{"tasks": tasks}, false), nil
	default:
		return mcpToolError("未知工具: " + name), nil
	}
}

func mcpInitializeResult(params json.RawMessage) map[string]any {
	version := mcpProtocolVersionFallback
	var body struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	if len(params) > 0 {
		_ = json.Unmarshal(params, &body)
	}
	switch strings.TrimSpace(body.ProtocolVersion) {
	case "2024-11-05", "2025-03-26", "2025-06-18", "2025-11-25":
		version = strings.TrimSpace(body.ProtocolVersion)
	}
	serverVersion := strings.TrimSpace(versionVar())
	if serverVersion == "" {
		serverVersion = "dev"
	}
	return map[string]any{
		"protocolVersion": version,
		"capabilities":    map[string]any{"tools": map[string]any{"listChanged": false}},
		"serverInfo":      map[string]any{"name": "maclaw", "version": serverVersion},
	}
}

func versionVar() string { return version }

func mcpToolDefinitions() []map[string]any {
	object := func(props map[string]any, required []string) map[string]any {
		schema := map[string]any{"type": "object", "properties": props}
		if len(required) > 0 {
			schema["required"] = required
		}
		return schema
	}
	str := func(desc string) map[string]any {
		return map[string]any{"type": "string", "description": desc}
	}
	return []map[string]any{
		{
			"name":        "list_models",
			"description": "列出这台电脑上可用的模型服务商和模型。不含密钥。未通过连接测试、且不是当前服务商的项不会出现。",
			"inputSchema": object(map[string]any{}, nil),
		},
		{
			"name":        "create_task",
			"description": "在这台电脑上创建会话或编程任务，并可选地立刻开始。这台 GUI 是会话服务，创建、停止和查看与远控、ACP 共用同一套能力。mode 省略且带了工作目录时按编程任务处理；显式 chat 保持会话。provider 和 model 可选，指定 model 时必须同时指定 provider。wait 默认 false，true 会等到这轮结束。编程计划或其它确认会以 needs_confirmation 结束，并用 commands 告诉调用方怎么继续。",
			"inputSchema": object(map[string]any{
				"name":        str("任务名称。为空时从 prompt 取第一行。"),
				"prompt":      str("要执行的内容。为空则只创建任务，不开始。"),
				"instruction": str("prompt 的别名。"),
				"mode":        str("chat 或 coding_dev。"),
				"working_dir": str("编程任务的本机绝对路径。别名 workingDir、work_dir。"),
				"provider":    str("服务商 id 或名称。"),
				"model":       str("该服务商下的模型 id。"),
				"wait":        map[string]any{"type": "boolean", "description": "为 true 时阻塞到本轮结束。默认 false。"},
			}, nil),
		},
		{
			"name":        "get_task",
			"description": "查看一个任务的状态和结果。needs_confirmation 表示这轮已经停下来等确认。running 时如果带了 confirmation_kind 和 commands，表示这轮还在跑，但卡在授权或安全确认上。commands 里的每一条都可以原样交给 send_command。",
			"inputSchema": object(map[string]any{
				"project_path": str("create_task 返回的 project_path。"),
			}, []string{"project_path"}),
		},
		{
			"name":        "stop_task",
			"description": "停止一个正在运行的任务。空闲任务视为已经停止。",
			"inputSchema": object(map[string]any{
				"project_path": str("要停止的任务。"),
			}, []string{"project_path"}),
		},
		{
			"name":        "send_command",
			"description": "向一个已有任务发送任意命令或后续内容。斜杠命令、确认按钮和自由回答都原样进入该任务。编程计划用 /plan approve、/plan skip、/plan reject。确认卡、未完成项、会话恢复、授权和安全确认用该任务 commands 里的原文。普通命令在任务 running 时会拒绝；commands 里的授权和安全确认可以在 running 时发送，当前这轮会继续。wait 默认 false。",
			"inputSchema": object(map[string]any{
				"project_path": str("任务的 project_path。"),
				"command":      str("要发回的原文。别名 text、prompt、message。"),
				"wait":         map[string]any{"type": "boolean", "description": "为 true 时阻塞到本轮结束。默认 false。"},
			}, []string{"project_path", "command"}),
		},
		{
			"name":        "list_tasks",
			"description": "列出这台电脑上最近的任务，并带上 MCP 正在跟踪的状态。",
			"inputSchema": object(map[string]any{}, nil),
		},
	}
}

func mcpArgumentsObject(raw json.RawMessage) (map[string]any, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return map[string]any{}, nil
	}
	var asObject map[string]any
	if err := json.Unmarshal(raw, &asObject); err == nil {
		if asObject == nil {
			return map[string]any{}, nil
		}
		return asObject, nil
	}
	var encoded string
	if err := json.Unmarshal(raw, &encoded); err != nil {
		return nil, err
	}
	encoded = strings.TrimSpace(encoded)
	if encoded == "" {
		return map[string]any{}, nil
	}
	if err := json.Unmarshal([]byte(encoded), &asObject); err != nil {
		return nil, err
	}
	if asObject == nil {
		return map[string]any{}, nil
	}
	return asObject, nil
}

func mcpParseCreateArgs(args map[string]any) mcpCreateArgs {
	prompt, _ := mcpLookupString(args, "prompt", "instruction")
	name, _ := mcpLookupString(args, "name")
	mode, _ := mcpLookupString(args, "mode")
	dir, _ := mcpLookupString(args, "working_dir", "workingDir", "work_dir")
	provider, _ := mcpLookupString(args, "provider")
	model, _ := mcpLookupString(args, "model")
	return mcpCreateArgs{
		Name:       name,
		Prompt:     prompt,
		Mode:       mode,
		WorkingDir: dir,
		Provider:   provider,
		Model:      model,
		Wait:       mcpLookupBool(args, "wait"),
	}
}

func mcpLookupString(args map[string]any, keys ...string) (string, bool) {
	for _, key := range keys {
		value, ok := args[key]
		if !ok || value == nil {
			continue
		}
		text, ok := value.(string)
		if ok {
			return text, true
		}
	}
	return "", false
}

func mcpFirstString(args map[string]any, keys ...string) string {
	text, _ := mcpLookupString(args, keys...)
	return text
}

func mcpLookupBool(args map[string]any, key string) bool {
	value, ok := args[key]
	if !ok || value == nil {
		return false
	}
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return strings.EqualFold(strings.TrimSpace(typed), "true")
	default:
		return false
	}
}

func mcpToolText(payload any, isError bool) map[string]any {
	raw, err := json.Marshal(payload)
	if err != nil {
		raw = []byte(`{"error":"encode result"}`)
		isError = true
	}
	return map[string]any{
		"content": []map[string]any{{"type": "text", "text": string(raw)}},
		"isError": isError,
	}
}

func mcpToolError(message string) map[string]any {
	return mcpToolText(map[string]string{"error": message}, true)
}

func (h *mcpHTTP) writeResult(w http.ResponseWriter, r *http.Request, id json.RawMessage, result any, rpcErr *mcpRPCError) {
	if len(id) == 0 {
		id = json.RawMessage("null")
	}
	body := map[string]any{"jsonrpc": "2.0", "id": id}
	if rpcErr != nil {
		body["error"] = rpcErr
	} else {
		body["result"] = result
	}
	raw, err := json.Marshal(body)
	if err != nil {
		raw = []byte(`{"jsonrpc":"2.0","id":null,"error":{"code":-32603,"message":"encode response"}}`)
	}
	if mcpWantsSSEOnly(r) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "event: message\ndata: %s\n\n", raw)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(raw)
}

func mcpIsNotification(id json.RawMessage) bool {
	return len(bytesTrimSpace(id)) == 0 || string(bytesTrimSpace(id)) == "null"
}

func bytesTrimSpace(raw []byte) []byte {
	return []byte(strings.TrimSpace(string(raw)))
}

func mcpWantsSSEOnly(r *http.Request) bool {
	if r == nil {
		return false
	}
	accept := r.Header.Get("Accept")
	if strings.TrimSpace(accept) == "" {
		return false
	}
	hasJSON := false
	hasSSE := false
	for _, part := range strings.Split(accept, ",") {
		media := strings.ToLower(strings.TrimSpace(strings.Split(part, ";")[0]))
		switch media {
		case "application/json", "*/*":
			hasJSON = true
		case "text/event-stream":
			hasSSE = true
		}
	}
	return hasSSE && !hasJSON
}

func mcpRemoteIsLoopback(r *http.Request) bool {
	if r == nil {
		return false
	}
	host := strings.TrimSpace(r.RemoteAddr)
	if host == "" {
		return false
	}
	return isLoopbackAddr(&mcpAddr{value: host})
}

type mcpAddr struct{ value string }

func (a mcpAddr) Network() string { return "tcp" }
func (a mcpAddr) String() string  { return a.value }

// mcpOriginAllowed gates browser-supplied Origin headers (DNS-rebinding
// defense). Loopback origins are always accepted. With LAN access on, private
// network origins are accepted too; public hostnames stay rejected because a
// public origin points at an internet page, not a LAN peer.
func mcpOriginAllowed(origin string, lanAllowed bool) bool {
	origin = strings.TrimSpace(origin)
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	if err != nil {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "localhost" || host == "127.0.0.1" || host == "::1" {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		if ip.IsLoopback() {
			return true
		}
		return lanAllowed && ip.IsPrivate()
	}
	return false
}

func mcpRequestToken(r *http.Request) string {
	if r == nil {
		return ""
	}
	header := strings.TrimSpace(r.Header.Get("Authorization"))
	if len(header) >= len("Bearer ") && strings.EqualFold(header[:len("Bearer ")], "Bearer ") {
		return strings.TrimSpace(header[len("Bearer "):])
	}
	return strings.TrimSpace(r.Header.Get("X-Api-Key"))
}
