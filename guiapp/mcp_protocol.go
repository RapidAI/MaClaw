package guiapp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
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
	mcpGetTask(ctx context.Context, projectPath, runID string) (mcpTaskView, error)
	mcpStopTask(ctx context.Context, projectPath string) (mcpTaskView, error)
	mcpSendCommand(ctx context.Context, projectPath, command, confirmationID, clientRequestID string, wait bool) (mcpTaskView, error)
	mcpListTasks(ctx context.Context) ([]mcpTaskView, error)
	mcpReadLog(ctx context.Context, projectPath, runID string, cursor int) (mcpLogPage, error)
	mcpListArtifacts(ctx context.Context, projectPath string) (mcpArtifactList, error)
	mcpReadArtifact(ctx context.Context, projectPath, relPath string, download bool) (mcpArtifactBody, error)
	mcpGetRequest(ctx context.Context, clientRequestID string) (mcpRequestView, error)
	mcpCapabilities(ctx context.Context) (mcpCapabilityView, error)
	mcpRetrySteps(ctx context.Context, projectPath string, stepIDs []string, confirmationID, clientRequestID string, wait bool) (mcpTaskView, error)
	mcpBrowserTest(ctx context.Context, projectPath, action, url, selector, text, key, expect, stepID string, paths []string) (mcpBrowserResult, error)
	mcpPackArtifacts(ctx context.Context, projectPath string, paths []string) (mcpPackResult, error)
}

type mcpCreateArgs struct {
	Name            string
	Prompt          string
	Mode            string
	WorkingDir      string
	Provider        string
	Model           string
	Wait            bool
	ClientRequestID string
	SSHHost         string
	SSHUser         string
	SSHPassword     string
	SSHPort         int
	RemoteWorkDir   string
	// ArgError is a parse failure, such as an ssh_port outside 1..65535.
	ArgError string
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
	// Authenticated requests carry who they are: create_task/send_command log
	// the caller (IP + masked key) the moment a task actually starts.
	ctx := r.Context()
	if info, ok := mcpClientInfoFromRequest(r, h.token()); ok {
		ctx = context.WithValue(ctx, mcpClientInfoContextKey{}, info)
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
	result, rpcErr := h.dispatch(ctx, req.Method, req.Params)
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
		runID := mcpFirstString(args, "run_id", "runId")
		view, err := h.api.mcpGetTask(ctx, path, runID)
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
		confirmationID := mcpFirstString(args, "confirmation_id", "confirmationId")
		clientRequestID := mcpFirstString(args, "client_request_id", "clientRequestId")
		view, err := h.api.mcpSendCommand(ctx, path, command, confirmationID, clientRequestID, mcpLookupBool(args, "wait"))
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
	case "read_log":
		path := mcpFirstString(args, "project_path", "projectPath")
		runID := mcpFirstString(args, "run_id", "runId")
		page, err := h.api.mcpReadLog(ctx, path, runID, mcpLookupInt(args, "cursor"))
		if err != nil {
			return mcpToolError(err.Error()), nil
		}
		return mcpToolText(page, false), nil
	case "list_artifacts":
		path := mcpFirstString(args, "project_path", "projectPath")
		list, err := h.api.mcpListArtifacts(ctx, path)
		if err != nil {
			return mcpToolError(err.Error()), nil
		}
		return mcpToolText(list, false), nil
	case "read_artifact":
		path := mcpFirstString(args, "project_path", "projectPath")
		rel := mcpFirstString(args, "path", "file")
		body, err := h.api.mcpReadArtifact(ctx, path, rel, mcpLookupBool(args, "download"))
		if err != nil {
			return mcpToolError(err.Error()), nil
		}
		return mcpToolText(body, false), nil
	case "get_request":
		id := mcpFirstString(args, "client_request_id", "clientRequestId")
		state, err := h.api.mcpGetRequest(ctx, id)
		if err != nil {
			return mcpToolError(err.Error()), nil
		}
		return mcpToolText(state, false), nil
	case "get_capabilities":
		info, err := h.api.mcpCapabilities(ctx)
		if err != nil {
			return mcpToolError(err.Error()), nil
		}
		return mcpToolText(info, false), nil
	case "retry_steps":
		path := mcpFirstString(args, "project_path", "projectPath")
		confirmationID := mcpFirstString(args, "confirmation_id", "confirmationId")
		clientRequestID := mcpFirstString(args, "client_request_id", "clientRequestId")
		view, err := h.api.mcpRetrySteps(ctx, path, mcpLookupStrings(args, "step_ids", "stepIds", "steps"), confirmationID, clientRequestID, mcpLookupBool(args, "wait"))
		if err != nil {
			return mcpToolError(err.Error()), nil
		}
		return mcpToolText(view, view.Status == mcpStatusFailed), nil
	case "browser_test":
		path := mcpFirstString(args, "project_path", "projectPath")
		action := mcpFirstString(args, "action")
		pageURL := mcpFirstString(args, "url")
		selector := mcpFirstString(args, "selector")
		text := mcpFirstString(args, "text", "target")
		key := mcpFirstString(args, "key")
		expect := mcpFirstString(args, "expect", "assertion")
		stepID := mcpFirstString(args, "step_id", "stepId")
		result, err := h.api.mcpBrowserTest(ctx, path, action, pageURL, selector, text, key, expect, stepID, mcpLookupPathList(args, "paths", "files"))
		if err != nil {
			return mcpToolError(err.Error()), nil
		}
		return mcpToolText(result, false), nil
	case "pack_artifacts":
		path := mcpFirstString(args, "project_path", "projectPath")
		packed, err := h.api.mcpPackArtifacts(ctx, path, mcpLookupPathList(args, "paths", "files"))
		if err != nil {
			return mcpToolError(err.Error()), nil
		}
		return mcpToolText(packed, false), nil
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
			"description": "在这台电脑上创建会话、本机编程或远程编程任务，并可选地立刻开始。这台 GUI 是会话服务，创建、停止和查看与远控、ACP 共用同一套能力。mode 省略且带了本机工作目录时按编程任务处理；显式 chat 保持会话。mode 省略但带了 ssh_host、ssh_user、ssh_password、ssh_port 或 remote_work_dir 时按远程编程处理。显式 chat 或 coding_dev 不能再带这些 SSH 字段。mode 为 remote_coding_dev 时创建远程编程任务，连接成功后这一轮交给远程编程 agent。provider 和 model 可选，指定 model 时必须同时指定 provider。wait 默认 false，true 会等到这轮结束。编程计划或其它确认会以 needs_confirmation 结束，并用 commands 告诉调用方怎么继续。",
			"inputSchema": object(map[string]any{
				"name":              str("任务名称。为空时从 prompt 取第一行。"),
				"prompt":            str("要执行的内容。为空则只创建任务，不开始。"),
				"instruction":       str("prompt 的别名。"),
				"mode":              str("chat、coding_dev 或 remote_coding_dev。省略时只有本机工作目录算 coding_dev；带了 SSH 字段算 remote_coding_dev。远程别名还有 remote_coding、remote_programming、remote_code、remote。"),
				"working_dir":       str("本机编程任务的绝对路径。远程任务里它是远端目录的别名。别名 workingDir、work_dir。"),
				"remote_work_dir":   str("远程编程任务的远端工作目录。别名 remoteWorkDir、ssh_work_dir。"),
				"ssh_host":          str("远程编程任务的 SSH 主机。别名 sshHost。"),
				"ssh_user":          str("远程编程任务的 SSH 用户。别名 sshUser。"),
				"ssh_password":      str("远程编程任务的 SSH 密码。只用于这次连接，不会写入任务结果，也不会进入请求编号。别名 sshPassword。"),
				"ssh_port":          map[string]any{"type": "integer", "description": "SSH 端口。省略时为 22。别名 sshPort。"},
				"provider":          str("服务商 id 或名称。"),
				"model":             str("该服务商下的模型 id。"),
				"client_request_id": str("调用方自己的唯一请求编号。超时后用 get_request 查询，同一个编号不会把任务创建两次。连接失败时这个编号可以改过密码再试。"),
				"wait":              map[string]any{"type": "boolean", "description": "为 true 时阻塞到本轮结束。默认 false。"},
			}, nil),
		},
		{
			"name":        "get_task",
			"description": "查看一个任务的状态和结果。返回 provider、model、working_dir、round、run_id、status_version、started_at、finished_at、end_reason、end_detail。run_id 是这一轮执行；传入时只返回这一轮，不传则返回当前轮。status_version 随这一轮的状态变化增加。stop 给出 class、source、reason、at 和 step_id，class 可以是 user_stop、timeout、program_error、dependency_failure 或 waiting_review。steps 列出每一步的 id、status、error、retryable，未执行的步骤还有 blocked_by 和 block_reason。等待子任务审阅时步骤和任务的 status 都是 waiting_review，不是 failed，也不是 completed。必要步骤没通过时 status 不会是 completed。child_task_id 是第一个内部任务，children 列出全部；已接受的子任务 status 是 admitted，不是 completed。checks 是浏览器截图和断言，带当时的文件哈希；文件变了之后 stale 为 true。远程编程任务还会返回 ssh_host、ssh_user、ssh_port、remote_work_dir，不含密码。end_reason 在结束后是 completed、failed、cancelled、waiting_review 或 needs_confirmation。needs_confirmation 表示这轮已经停下来等确认。running 时如果带了 confirmation_kind 和 commands，表示这轮还在跑，但卡在授权或安全确认上。有确认时还会给出 confirmation_id。commands 里的每一条都可以原样交给 send_command，决定类命令必须带上这个 confirmation_id。",
			"inputSchema": object(map[string]any{
				"project_path": str("create_task 返回的 project_path。"),
				"run_id":       str("只查询这一轮。省略时返回当前轮。别名 runId。"),
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
			"description": "向一个已有任务发送任意命令或后续内容。斜杠命令、确认按钮和自由回答都原样进入该任务。返回这一轮的 run_id 和 status_version。新一轮的 steps 只属于这一轮，上一轮用 get_task 的 run_id 查询。编程计划用 /plan approve、/plan skip、/plan reject。确认卡、未完成项、会话恢复、授权和安全确认用该任务 commands 里的原文，并且必须带上这次 get_task 返回的 confirmation_id。编号不匹配或已经用过会拒绝，也不会沿用上一轮确认。普通命令在任务 running 时会拒绝；commands 里的授权和安全确认可以在 running 时发送，当前这轮会继续。可以带 client_request_id，同一个编号不会把指令再执行一次。返回已接受不代表执行完成。wait 默认 false。",
			"inputSchema": object(map[string]any{
				"project_path":      str("任务的 project_path。"),
				"command":           str("要发回的原文。别名 text、prompt、message。"),
				"confirmation_id":   str("get_task 返回的 confirmation_id。决定类命令必填。"),
				"client_request_id": str("调用方自己的唯一请求编号。超时后用 get_request 查询。"),
				"wait":              map[string]any{"type": "boolean", "description": "为 true 时阻塞到本轮结束。默认 false。"},
			}, []string{"project_path", "command"}),
		},
		{
			"name":        "list_tasks",
			"description": "列出这台电脑上最近的任务，并带上 MCP 正在跟踪的状态。每条任务包含和服务详情一样的 provider、model、working_dir、round 和结束信息。",
			"inputSchema": object(map[string]any{}, nil),
		},
		{
			"name":        "read_log",
			"description": "按游标读取任务进展、工具调用和错误。cursor 传上一次的 next_cursor，第一次传 0。不传 run_id 时序号跨轮次继续增加。传入 run_id 时只返回这一轮，并带上 run_id 和 status_version。如果中间丢过记录，gap 为 true。不会返回整段拼接文本。密钥会打码。",
			"inputSchema": object(map[string]any{
				"project_path": str("任务的 project_path。"),
				"run_id":       str("只读取这一轮。省略时跨轮次继续。别名 runId。"),
				"cursor":       map[string]any{"type": "integer", "description": "上次返回的 next_cursor。第一次用 0。"},
			}, []string{"project_path"}),
		},
		{
			"name":        "list_artifacts",
			"description": "列出任务工作目录里的文件，包含相对路径、大小和 sha256。不能超出这个目录，密钥文件和链接不会列出。远程编程任务的目录在 SSH 主机上，不从本机列出。单文件或本次列出的哈希总量超过 32MiB 时 hash_skipped 为 true。",
			"inputSchema": object(map[string]any{
				"project_path": str("任务的 project_path。"),
			}, []string{"project_path"}),
		},
		{
			"name":        "read_artifact",
			"description": "读取任务工作目录里的一个文件。文本返回 utf-8，最大 1MiB。download 为 true 时返回 base64，最大 16MiB，适合截图和测试报告。路径必须留在工作目录内。链接、密钥文件和 Windows 设备名会拒绝。远程编程任务的目录在 SSH 主机上，不从本机读取。",
			"inputSchema": object(map[string]any{
				"project_path": str("任务的 project_path。"),
				"path":         str("相对工作目录的文件路径。"),
				"download":     map[string]any{"type": "boolean", "description": "为 true 时用 base64 返回内容。"},
			}, []string{"project_path", "path"}),
		},
		{
			"name":        "get_request",
			"description": "按调用方传入的 client_request_id 查询创建、发指令或定向续跑是否已经受理。超时后先查这个，再决定要不要重试。pending 表示还在处理。受理后如果调用失败，error 是当时的原因，同一个编号不会再执行一次。受理前失败时编号可以重试。",
			"inputSchema": object(map[string]any{
				"client_request_id": str("create_task、send_command 或 retry_steps 传入的请求编号。"),
			}, []string{"client_request_id"}),
		},
		{
			"name":        "get_capabilities",
			"description": "返回接口版本、支持的功能、当前模型是否已配置，以及浏览器测试的具体调用方式。browser_methods 列出 open、observe、click、key、screenshot、assert 的工具名、动作和参数。电脑使用关闭时 available 为 false。不拨测网络，也没有任意 Shell。",
			"inputSchema": object(map[string]any{}, nil),
		},
		{
			"name":        "retry_steps",
			"description": "按步骤编号重试失败或未完成的步骤。已通过的步骤保留结果，不会重跑整份任务。step_ids 接受 T5、5 或「步骤 5」。任务还在运行时会拒绝。当前有确认编号时必须带上这次的 confirmation_id，编号不匹配不会消耗它。可以带 client_request_id，受理前同一个编号可以再试。",
			"inputSchema": object(map[string]any{
				"project_path":      str("任务的 project_path。"),
				"step_ids":          map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "要重试的步骤编号。也可以传一条用逗号分隔的字符串。"},
				"confirmation_id":   str("当前 get_task 返回的 confirmation_id。有确认时必填。"),
				"client_request_id": str("调用方自己的唯一请求编号。超时后用 get_request 查询。"),
				"wait":              map[string]any{"type": "boolean", "description": "为 true 时阻塞到本轮结束。默认 false。"},
			}, []string{"project_path", "step_ids"}),
		},
		{
			"name":        "browser_test",
			"description": "对任务页面做浏览器测试。action 是 open、observe、click、key、screenshot、assert。电脑使用关闭时不可用。screenshot 和 assert 必须带 step_id。截图和断言记下当时的步骤和文件哈希，文件改过之后 get_task 里对应记录的 stale 为 true。断言不符合时 ok 为 false，这次调用仍然成功。没有任意 Shell。",
			"inputSchema": object(map[string]any{
				"project_path": str("任务的 project_path。"),
				"action":       str("open、observe、click、key、screenshot 或 assert。"),
				"url":          str("open 要打开的地址。http 和 https 可以直接打开。file 必须留在任务工作目录内。"),
				"selector":     str("click 的 CSS 选择器。和 text 至少给一个。"),
				"text":         str("click 要点的可见文字。别名 target。"),
				"key":          str("key 要按的键，例如 Enter。"),
				"expect":       str("assert 要在页面文字里找到的内容。别名 assertion。"),
				"step_id":      str("screenshot 和 assert 所属的步骤编号，例如 T5。"),
				"paths":        map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "这次检查要记住版本的工作目录文件。省略时使用该步骤已经记录的文件。"},
			}, []string{"project_path", "action"}),
		},
		{
			"name":        "pack_artifacts",
			"description": "把任务工作目录里选定的文件打成一个 zip。返回每个文件的路径、大小、sha256，以及压缩包的 sha256。内容用 base64，最大 16MiB。目录、链接、密钥和越界路径不会打进包，列在 omitted 里。远程编程任务的目录在 SSH 主机上，不从本机打包。",
			"inputSchema": object(map[string]any{
				"project_path": str("任务的 project_path。"),
				"paths":        map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "相对工作目录的文件路径。也可以传一条用逗号分隔的字符串。"},
			}, []string{"project_path", "paths"}),
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
	clientRequestID, _ := mcpLookupString(args, "client_request_id", "clientRequestId")
	host, _ := mcpLookupString(args, "ssh_host", "sshHost")
	user, _ := mcpLookupString(args, "ssh_user", "sshUser")
	password, _ := mcpLookupString(args, "ssh_password", "sshPassword")
	remoteDir, _ := mcpLookupString(args, "remote_work_dir", "remoteWorkDir", "ssh_work_dir")
	port, portErr := mcpLookupPort(args, "ssh_port", "sshPort")
	argError := ""
	if portErr != nil {
		argError = portErr.Error()
	}
	return mcpCreateArgs{
		Name:            name,
		Prompt:          prompt,
		Mode:            mode,
		WorkingDir:      dir,
		Provider:        provider,
		Model:           model,
		Wait:            mcpLookupBool(args, "wait"),
		ClientRequestID: clientRequestID,
		SSHHost:         host,
		SSHUser:         user,
		SSHPassword:     password,
		SSHPort:         port,
		RemoteWorkDir:   remoteDir,
		ArgError:        argError,
	}
}

func mcpLookupPort(args map[string]any, keys ...string) (int, error) {
	var raw any
	found := false
	for _, key := range keys {
		value, ok := args[key]
		if !ok || value == nil {
			continue
		}
		found = true
		raw = value
		break
	}
	if !found {
		return 0, nil
	}
	reject := errors.New("ssh_port 必须是 1 到 65535")
	switch typed := raw.(type) {
	case float64:
		if typed < 1 || typed > 65535 || typed != float64(int(typed)) {
			return 0, reject
		}
		return int(typed), nil
	case int:
		if typed < 1 || typed > 65535 {
			return 0, reject
		}
		return typed, nil
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(typed))
		if err != nil || n < 1 || n > 65535 {
			return 0, reject
		}
		return n, nil
	default:
		return 0, reject
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

func mcpLookupInt(args map[string]any, key string) int {
	value, ok := args[key]
	if !ok || value == nil {
		return 0
	}
	switch typed := value.(type) {
	case float64:
		if typed < 0 {
			return 0
		}
		return int(typed)
	case int:
		if typed < 0 {
			return 0
		}
		return typed
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(typed))
		if err != nil || n < 0 {
			return 0
		}
		return n
	default:
		return 0
	}
}

func mcpLookupStrings(args map[string]any, keys ...string) []string {
	for _, key := range keys {
		value, ok := args[key]
		if !ok || value == nil {
			continue
		}
		switch typed := value.(type) {
		case []any:
			out := make([]string, 0, len(typed))
			for _, item := range typed {
				switch one := item.(type) {
				case string:
					if strings.TrimSpace(one) != "" {
						out = append(out, one)
					}
				case float64:
					if one > 0 && one == float64(int(one)) {
						out = append(out, strconv.Itoa(int(one)))
					}
				}
			}
			return out
		case []string:
			return append([]string(nil), typed...)
		case string:
			if strings.TrimSpace(typed) == "" {
				return nil
			}
			return []string{typed}
		case float64:
			if typed > 0 && typed == float64(int(typed)) {
				return []string{strconv.Itoa(int(typed))}
			}
		}
	}
	return nil
}

func mcpLookupPathList(args map[string]any, keys ...string) []string {
	raw := mcpLookupStrings(args, keys...)
	if len(raw) == 0 {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		parts := strings.FieldsFunc(item, func(r rune) bool {
			return r == ',' || r == '，' || r == ';' || r == '；'
		})
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part != "" {
				out = append(out, part)
			}
		}
	}
	return out
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
	return len(mcpTrimRaw(id)) == 0 || string(mcpTrimRaw(id)) == "null"
}

func mcpTrimRaw(raw []byte) []byte {
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
