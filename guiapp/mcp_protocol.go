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
	Name             string
	Prompt           string
	Mode             string
	WorkingDir       string
	Provider         string
	Model            string
	Wait             bool
	ClientRequestID  string
	SSHHost          string
	SSHUser          string
	SSHPassword      string
	SSHPort          int
	RemoteWorkDir    string
	Location         string
	CloudWorkspaceID string
	GrantID          string
	// ArgError is a parse failure, such as an ssh_port outside 1..65535.
	ArgError string
}

// mcpHTTP carries the accepted bearer tokens plus the task API. tokens()
// returns every key currently valid — the live token and the previous one
// kept across a rotation — so a just-rotated key's predecessor stays usable
// until the next generation without re-reading the key file per request.
type mcpHTTP struct {
	tokens func() []string
	api    mcpTaskAPI
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
	return newMCPHTTPTokens(func() []string { return []string{token()} }, api, lanAllowed)
}

// newMCPHTTPTokens builds the handler from a multi-token source. The host
// passes mcpHost.tokenValues so both the current and previous keys work.
func newMCPHTTPTokens(tokens func() []string, api mcpTaskAPI, lanAllowed func() bool) http.Handler {
	if tokens == nil {
		tokens = func() []string { return nil }
	}
	return &mcpHTTP{tokens: tokens, api: api, lanAllowed: lanAllowed}
}

// tokenMatches reports whether req equals any currently accepted token. The
// previous key stays valid after a rotation until the next one is generated.
func (h *mcpHTTP) tokenMatches(req string) bool {
	req = strings.TrimSpace(req)
	if req == "" {
		return false
	}
	for _, token := range h.tokens() {
		if secureTokenEqual(req, token) {
			return true
		}
	}
	return false
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
	if !h.tokenMatches(mcpRequestToken(r)) {
		w.Header().Set("WWW-Authenticate", "Bearer")
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	// Authenticated requests carry who they are: create_task/send_command log
	// the caller (IP + masked key) the moment a task actually starts.
	ctx := r.Context()
	if info, ok := mcpClientInfoFromRequest(r, mcpRequestToken(r)); ok {
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
	ctx = mcpContextWithOpts(ctx, mcpCallOptsFromArgs(name, args))
	switch name {
	case "knowledge_search":
		searcher, ok := h.api.(interface {
			mcpKnowledgeSearch(ctx context.Context, query string, limit int) (map[string]any, error)
		})
		if !ok {
			return mcpToolError("这台电脑的知识库检索还没有准备好"), nil
		}
		payload, err := searcher.mcpKnowledgeSearch(ctx, mcpFirstString(args, "query", "q"), mcpLookupInt(args, "limit"))
		if err != nil {
			if payload == nil {
				return mcpToolFail(err), nil
			}
			return mcpToolText(payload, true), nil
		}
		return mcpToolText(payload, false), nil
	case "list_models":
		catalog, err := h.api.mcpListModels(ctx)
		if err != nil {
			return mcpToolFail(err), nil
		}
		return mcpToolText(catalog, false), nil
	case "create_task":
		view, err := h.api.mcpCreateTask(ctx, mcpParseCreateArgs(args))
		if err != nil {
			return mcpToolFail(err), nil
		}
		return mcpToolText(view, view.Status == mcpStatusFailed), nil
	case "get_task":
		path := mcpFirstString(args, "project_path", "projectPath")
		runID := mcpFirstString(args, "run_id", "runId")
		view, err := h.api.mcpGetTask(ctx, path, runID)
		if err != nil {
			return mcpToolFail(err), nil
		}
		return mcpToolText(view, view.Status == mcpStatusFailed), nil
	case "stop_task", "cancel_task":
		path := mcpFirstString(args, "project_path", "projectPath")
		view, err := h.api.mcpStopTask(ctx, path)
		if err != nil {
			return mcpToolFail(err), nil
		}
		return mcpToolText(view, view.Status == mcpStatusFailed), nil
	case "send_command":
		path := mcpFirstString(args, "project_path", "projectPath")
		command := mcpFirstString(args, "command", "text", "prompt", "message")
		confirmationID := mcpFirstString(args, "confirmation_id", "confirmationId")
		clientRequestID := mcpFirstString(args, "client_request_id", "clientRequestId")
		view, err := h.api.mcpSendCommand(ctx, path, command, confirmationID, clientRequestID, mcpLookupBool(args, "wait"))
		if err != nil {
			return mcpToolFail(err), nil
		}
		return mcpToolText(view, view.Status == mcpStatusFailed), nil
	case "list_tasks":
		tasks, err := h.api.mcpListTasks(ctx)
		if err != nil {
			return mcpToolFail(err), nil
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
			return mcpToolFail(err), nil
		}
		return mcpToolText(page, false), nil
	case "list_artifacts":
		path := mcpFirstString(args, "project_path", "projectPath")
		list, err := h.api.mcpListArtifacts(ctx, path)
		if err != nil {
			return mcpToolFail(err), nil
		}
		return mcpToolText(list, false), nil
	case "read_artifact":
		path := mcpFirstString(args, "project_path", "projectPath")
		rel := mcpFirstString(args, "path", "file")
		body, err := h.api.mcpReadArtifact(ctx, path, rel, mcpLookupBool(args, "download"))
		if err != nil {
			return mcpToolFail(err), nil
		}
		return mcpToolText(body, false), nil
	case "get_request":
		id := mcpFirstString(args, "client_request_id", "clientRequestId")
		state, err := h.api.mcpGetRequest(ctx, id)
		if err != nil {
			return mcpToolFail(err), nil
		}
		return mcpToolText(state, false), nil
	case "get_capabilities":
		info, err := h.api.mcpCapabilities(ctx)
		if err != nil {
			return mcpToolFail(err), nil
		}
		return mcpToolText(info, false), nil
	case "retry_steps":
		path := mcpFirstString(args, "project_path", "projectPath")
		confirmationID := mcpFirstString(args, "confirmation_id", "confirmationId")
		clientRequestID := mcpFirstString(args, "client_request_id", "clientRequestId")
		view, err := h.api.mcpRetrySteps(ctx, path, mcpLookupStrings(args, "step_ids", "stepIds", "steps"), confirmationID, clientRequestID, mcpLookupBool(args, "wait"))
		if err != nil {
			return mcpToolFail(err), nil
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
			return mcpToolFail(err), nil
		}
		return mcpToolText(result, false), nil
	case "pack_artifacts":
		path := mcpFirstString(args, "project_path", "projectPath")
		packed, err := h.api.mcpPackArtifacts(ctx, path, mcpLookupPathList(args, "paths", "files"))
		if err != nil {
			return mcpToolFail(err), nil
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
			"name":        "knowledge_search",
			"description": "搜索这台电脑的本地知识库。只读检索用户已经导入或保存的知识；query 必填，limit 默认 8、最大 50。结果包含可引用的知识摘要和来源信息，不修改知识库。",
			"inputSchema": object(map[string]any{
				"query": str("要检索的问题、主题或关键词。"),
				"limit": map[string]any{"type": "integer", "description": "最多返回多少条。默认 8，最大 50。"},
			}, []string{"query"}),
		},
		{
			"name":        "create_task",
			"description": "创建会话、本机编程、SSH 远程编程、云端 agent 或远程设备上的任务，并可选地立刻开始。location 为 local（本机，默认）、cloud（云端 agent）或 remote_device（已授权的远程设备）。省略 location 时，带 grant_id 视为远程设备，否则是本机。云端 agent 会新建云端实例并在上面创建任务，mode 为 chat 或 coding_dev，使用实例自己的工作目录，不接受 grant_id、cloud_workspace_id、provider 或 model。cloud_workspace_id 只是本机会话的工作目录，不是位置，也不是云端 agent。远程设备需要 grant_id，mode 为 chat 或 coding_dev，working_dir 是对方电脑上的目录。本机任务的创建、停止和查看都在这台电脑上。这条接口不提供 ACP，也不提供远控桌面操作。mode 省略且带了本机工作目录时按编程任务处理；显式 chat 保持会话。mode 省略但带了 ssh_host、ssh_user、ssh_password、ssh_port 或 remote_work_dir 时按远程编程处理。显式 chat 或 coding_dev 不能再带这些 SSH 字段。mode 为 remote_coding_dev 时创建远程编程任务，连接成功后这一轮交给远程编程 agent。provider 和 model 可选，指定 model 时必须同时指定 provider。远程设备和云端 agent 不接受这两个参数，模型由对方电脑或云端实例决定。wait 默认 false，true 会等到这轮结束。编程计划或其它确认会以 needs_confirmation 结束，并用 commands 告诉调用方怎么继续。",
			"inputSchema": object(map[string]any{
				"name":               str("任务名称。为空时从 prompt 取第一行。"),
				"prompt":             str("要执行的内容。为空则只创建任务，不开始。"),
				"instruction":        str("prompt 的别名。"),
				"location":           str("local、cloud 或 remote_device。别名：本地、本机、云端、云端agent、cloud_agent、远程设备、device、machine。省略时带 grant_id 视为远程设备，否则是 local。cloud_workspace_id 不能用来推断位置。"),
				"cloud_workspace_id": str("本机会话的工作目录，填写云端工作区 id。不是位置，location 为 cloud 时不要传。别名 cloudWorkspaceId、workspace_id。"),
				"grant_id":           str("远程设备的远控授权 id。location 为 remote_device 时必填。云端 agent 自己打开授权，不要传。别名 grantId。"),
				"mode":               str("本机：chat、coding_dev 或 remote_coding_dev。云端 agent 是 chat 或 coding_dev。远程设备是 chat 或 coding_dev。省略时只有本机工作目录算 coding_dev；带了 SSH 字段算 remote_coding_dev；带了 cloud_workspace_id 按会话处理。远程别名还有 remote_coding、remote_programming、remote_code、remote。"),
				"working_dir":        str("本机编程任务的绝对路径。SSH 远程编程里它是远端目录的别名。远程设备上它是对方电脑的目录。云端工作区用 cloud_workspace_id，不要写在这里。别名 workingDir、work_dir。"),
				"remote_work_dir":    str("远程编程任务的远端工作目录。别名 remoteWorkDir、ssh_work_dir。"),
				"ssh_host":           str("远程编程任务的 SSH 主机。别名 sshHost。"),
				"ssh_user":           str("远程编程任务的 SSH 用户。别名 sshUser。"),
				"ssh_password":       str("远程编程任务的 SSH 密码。只用于这次连接，不会写入任务结果，也不会进入请求编号。别名 sshPassword。"),
				"ssh_port":           map[string]any{"type": "integer", "description": "SSH 端口。省略时为 22。别名 sshPort。"},
				"provider":           str("服务商 id 或名称。"),
				"model":              str("该服务商下的模型 id。"),
				"client_request_id":  str("调用方自己的唯一请求编号。超时后用 get_request 查询，同一个编号不会把任务创建两次。连接失败时这个编号可以改过密码再试。"),
				"wait":               map[string]any{"type": "boolean", "description": "为 true 时阻塞到本轮结束。默认 false。"},
			}, nil),
		},
		{
			"name":        "get_task",
			"description": "查看一个任务的状态和结果。返回 provider、model、working_dir、location、round、run_id、current_run_id、task_id、status_version、started_at、finished_at、end_reason、end_detail。location 为 local、cloud 或 remote_device。云端 agent 带 grant_id，状态从云端实例读取。cloud_workspace_id 只是工作目录。远程设备任务带 grant_id，状态从对方电脑读取。task_id 等于 project_path。current_run_id 是当前轮，传入的 run_id 是正在查看的那一轮命令。run_id 是这一轮的会话标识，后来的命令不会盖住它。outcome 是给调用方决策的短说明：running 时要等这一轮结束再查，needs_confirmation 时先看 outcome、commands 和 confirmation_id。没有找到任务时 code 是 task_not_found。已知任务没有这一轮时 code 是 run_not_found，消息是「没有这一轮执行」，不会退回当前轮。记录过了保留期时 code 是 task_expired。working_dir 是实际工作目录，另外有 working_dir_requested、working_dir_effective、tool_root、session_path。status_version 随这一轮的状态变化增加。stop 给出 class、source、reason、at、step_id 和 deadline，class 可以是 user_stop、timeout、program_error、dependency_failure、waiting_review、no_progress 或 unconfirmed。deadline 只在已证实超时或已证实无进展时有值。这一轮还在产出 token、进展或工具事件时不会因为固定墙钟被取消；单次模型请求仍按自己的超时失败，不占用整轮预算。无进展时 status 是 failed，class 是 no_progress，reason 是「这一轮没有进展」，这不是用户停止。用户停止仍是 user_stop。主机自己的等待结束时 class 是 unconfirmed，reason 是「主机等待已结束，尚未证实为超时」，这不是已证实的执行超时。steps 列出每一步的 id、status、error、retryable，未执行的步骤还有 blocked_by 和 block_reason。等待子任务审阅时步骤和任务的 status 都是 waiting_review，不是 failed，也不是 completed。必要步骤没通过时 status 不会是 completed。正文仍是待确认计划时 status 是 needs_confirmation，不是 completed。无法识别的状态是 blocked_unknown，原始值在 status_raw。child_task_id 是第一个内部任务，children 列出全部；已接受的子任务 status 是 admitted，不是 completed。checks 是浏览器截图和断言，带当时的文件哈希；文件变了之后 stale 为 true。远程编程任务还会返回 ssh_host、ssh_user、ssh_port、remote_work_dir，不含密码。end_reason 在结束后是 completed、failed、cancelled、waiting_review 或 needs_confirmation。无进展失败的 end_reason 是 failed。进程重启后加载到的 running 记录没有仍在执行的模型调用，按无进展失败，不是 cancelled，也不是 user_stop，也不会挡住下一条命令。needs_confirmation 表示这轮已经停下来等确认。running 且 confirmation_kind 是 scope 或 critical 时，outcome 仍说明先处理 commands 里的确认，并写明授权说明在 text。有确认时还会给出 confirmation_id。commands 里的每一条都可以原样交给 send_command，决定类命令必须带上这个 confirmation_id。授权路径若只等于工作目录，以 send_command 当时的 commands 为准。",
			"inputSchema": object(map[string]any{
				"project_path": str("create_task 返回的 project_path。"),
				"run_id":       str("只查询这一轮。省略时返回当前轮。别名 runId。"),
			}, []string{"project_path"}),
		},
		{
			"name":        "stop_task",
			"description": "停止当前这一轮。结果是 cancelled，stop.class 为 user_stop。待确认卡被撤下，批准命令不再出现。空闲任务视为已经停止。不按归档的 run_id 停历史轮。cancel_task 是同一操作的别名，不在工具列表里单独出现。可选 expected_status_version：与当前 status_version 不一致时 code 是 status_version_conflict，消息是「状态版本已经变化，这次没有执行」，不停止，也不取消确认。省略时不比较。",
			"inputSchema": object(map[string]any{
				"project_path":            str("要停止的任务。"),
				"expected_status_version": map[string]any{"type": "integer", "description": "调用方看到的 status_version。省略时不比较。0 也是一个版本，只要字段出现就会比较。别名 expectedStatusVersion。"},
			}, []string{"project_path"}),
		},
		{
			"name":        "send_command",
			"description": "向一个已有任务发送任意命令或后续内容。斜杠命令、确认按钮和自由回答都原样进入该任务。返回这一轮的 run_id 和 status_version。新一轮的 steps 只属于这一轮，上一轮用 get_task 的 run_id 查询。编程计划用 /plan approve、/plan skip、/plan reject。确认卡、未完成项、会话恢复、授权和安全确认用该任务 commands 里的原文，并且必须带上这次 get_task 返回的 confirmation_id。编号不匹配或已经用过会拒绝，也不会沿用上一轮确认。普通命令在任务 running 时会拒绝；commands 里的授权和安全确认可以在 running 时发送，当前这轮会继续。可以带 client_request_id，同一个编号不会把指令再执行一次。返回已接受不代表执行完成。wait 默认 false。可选 expected_status_version：与当前 status_version 不一致时 code 是 status_version_conflict，消息是「状态版本已经变化，这次没有执行」，不启动、不改状态，也不占用 client_request_id。可选 dry_run：只预检版本和同一工作目录上的其他任务，有未提交改动时注明「既有改动、来源未证实」，不启动执行。用户停止之后仍可以显式发送；自动恢复不会进行。每个命令是单独一轮，返回的 run_id 是这一轮的会话标识。status 为 running 时结果还没出来，outcome 会写明这一点，text 不带上一轮的回答。结束后用 get_task 传入这个 run_id，或用 get_request 查询；status、outcome、commands、confirmation_id 都属于这一轮。看到终态后再发下一条命令。可选 provider 和 model，规则与 create_task 相同：指定 model 时必须同时指定 provider。只写入这次新开的一轮，上一轮保持原样。任务还在 running 时，这两个参数和命令一起拒绝。授权或安全确认这种续跑当前轮的命令若带了模型参数，会拒绝并说明续跑当前轮不能同时改模型，确认编号不消耗。dry_run 会在预检里写将要使用的服务商和模型，不保存、不启动。",
			"inputSchema": object(map[string]any{
				"project_path":            str("任务的 project_path。"),
				"command":                 str("要发回的原文。别名 text、prompt、message。"),
				"confirmation_id":         str("get_task 返回的 confirmation_id。决定类命令必填。"),
				"client_request_id":       str("调用方自己的唯一请求编号。超时后用 get_request 查询。"),
				"wait":                    map[string]any{"type": "boolean", "description": "为 true 时阻塞到本轮结束。默认 false。"},
				"expected_status_version": map[string]any{"type": "integer", "description": "调用方看到的 status_version。省略时不比较。0 也是一个版本，只要字段出现就会比较。别名 expectedStatusVersion。"},
				"dry_run":                 map[string]any{"type": "boolean", "description": "为 true 时只预检，不启动。别名 dryRun。"},
				"provider":                str("下一轮使用的服务商 id 或名称。省略时沿用这一轮已经记下的服务商。指定 model 时必填。"),
				"model":                   str("该服务商下的模型 id。省略时沿用这一轮已经记下的模型。"),
			}, []string{"project_path", "command"}),
		},
		{
			"name":        "list_tasks",
			"description": "列出这台电脑上最近的任务，并带上 MCP 正在跟踪的状态。回包是 {tasks}。任务列表最多 50 条，MCP 登记里多出来的路径再附在后面。每条任务包含和服务详情一样的 provider、model、working_dir、round 和结束信息。可见状态与 get_task 相同：running、completed、failed、cancelled、needs_confirmation、waiting_review、idle、blocked_unknown。还在推进的轮次保持 running，不会因为固定墙钟变成 cancelled。无进展失败是 failed，stop.class 是 no_progress。进程重启后加载到的 running 记录也是这个失败，不是 cancelled。用户停止是 cancelled，stop.class 是 user_stop。",
			"inputSchema": object(map[string]any{}, nil),
		},
		{
			"name":        "read_log",
			"description": "按游标读取任务进展、工具调用和错误。cursor 传上一次的 next_cursor，第一次传 0。不传 run_id 时序号跨轮次继续增加。传入 run_id 时只返回这一轮，并带上 run_id 和 status_version。如果中间丢过记录，gap 为 true。没有事件时 events 为空数组，empty 为 true，next_cursor 保持这次传入的 cursor。没有找到任务时 code 是 task_not_found。已知任务没有这一轮时 code 是 run_not_found，消息是「没有这一轮执行」，不会退回当前轮。记录过了保留期时 code 是 task_expired。不会返回整段拼接文本。密钥会打码。",
			"inputSchema": object(map[string]any{
				"project_path": str("任务的 project_path。"),
				"run_id":       str("只读取这一轮。省略时跨轮次继续。别名 runId。"),
				"cursor":       map[string]any{"type": "integer", "description": "上次返回的 next_cursor。第一次用 0。"},
			}, []string{"project_path"}),
		},
		{
			"name":        "list_artifacts",
			"description": "列出任务工作目录里的文件，包含相对路径、大小和 sha256。不能超出这个目录，密钥文件和链接不会列出。点目录和点文件默认不列出，include_hidden 为 true 时列出。node_modules、.git、dist、vendor 始终跳过。exclude_dirs 再排除额外目录名。cursor 是上一页最后一个相对路径，省略时从头开始。limit 最大 200。结果带 scope，截断时 truncated_reason 是 page_limit，next_cursor 是本页最后一条路径。远程编程任务的目录在 SSH 主机上，不从本机列出。单文件或本次列出的哈希总量超过 32MiB 时 hash_skipped 为 true。",
			"inputSchema": object(map[string]any{
				"project_path":   str("任务的 project_path。"),
				"cursor":         str("上一页最后一条相对路径。省略时从头开始。数字也会按路径文本比较。"),
				"limit":          map[string]any{"type": "integer", "description": "本页最多多少个文件。默认且最大 200。"},
				"include_hidden": map[string]any{"type": "boolean", "description": "为 true 时列出点目录和点文件。别名 includeHidden。"},
				"exclude_dirs":   map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "额外排除的目录名。别名 excludeDirs。"},
			}, []string{"project_path"}),
		},
		{
			"name":        "read_artifact",
			"description": "读取任务工作目录里的一个文件。文本返回 utf-8，最大 1MiB。download 为 true 时返回 base64，最大 16MiB，适合截图和测试报告。路径必须留在工作目录内。链接、密钥文件和 Windows 设备名会拒绝。可选 expected_sha256：与当时内容不一致时 code 是 artifact_stale，消息是「文件内容和期望的哈希不一致」，不返回内容。成功时 snapshot 为 true，表示这是读取当时的内容。远程编程任务的目录在 SSH 主机上，不从本机读取。",
			"inputSchema": object(map[string]any{
				"project_path":    str("任务的 project_path。"),
				"path":            str("相对工作目录的文件路径。"),
				"download":        map[string]any{"type": "boolean", "description": "为 true 时用 base64 返回内容。"},
				"expected_sha256": str("期望的 sha256。可以带 sha256: 前缀。别名 expectedSha256。不一致时不返回内容。"),
			}, []string{"project_path", "path"}),
		},
		{
			"name":        "get_request",
			"description": "按调用方传入的 client_request_id 查询创建、发指令或定向续跑是否已经受理。超时后先查这个，再决定要不要重试。state 始终存在：not_found、expired_or_unknown、accepted、running、failed、completed。没见过的编号是 not_found。保留期外或已经被挤掉的编号是 expired_or_unknown，accepted 为 false。pending 表示还在处理。payload_hash 是请求正文的 sha256，scope 给出 task_id 和 action。受理后如果调用失败，error 是当时的原因，同一个编号不会再执行一次。同一个编号配了另一份正文时 code 是 request_payload_conflict。受理前失败时编号可以重试。wait 为 false 且任务仍在运行时，state 不会是 completed。run_id 是这次命令自己的会话标识，task 是这一轮的结果，后来的命令不会把它换成新一轮。needs_confirmation 时 state 仍是 accepted，决定看 task.status、task.outcome、task.commands 和 task.confirmation_id。这一轮记录已经不在时 code 是 run_not_found，消息是「没有这一轮执行」，不会再报 state 为 running。没有记下 run_id 的请求仍返回 state，task 可以为空。",
			"inputSchema": object(map[string]any{
				"client_request_id": str("create_task、send_command 或 retry_steps 传入的请求编号。"),
			}, []string{"client_request_id"}),
		},
		{
			"name":        "get_capabilities",
			"description": "返回接口版本、支持的功能、当前模型是否已配置，以及浏览器测试的具体调用方式。schema_version 是 mcp-task-v2。server_build 是这个进程的版本，commit 可能为空字符串。persistence 说明轮次会落盘，保留 7 天。current_model.configured 只表示当前配置里有没有模型，probe_ok 固定为 null，不把已配置当成拨测成功。feature_flags 列出持久轮次、版本比较、dry_run、制品分页和期望哈希。browser_methods 列出 open、observe、click、key、screenshot、assert 的工具名、动作和参数。电脑使用关闭时 available 为 false。不拨测网络，也没有任意 Shell。",
			"inputSchema": object(map[string]any{}, nil),
		},
		{
			"name":        "retry_steps",
			"description": "按步骤编号重试失败或未完成的步骤。已通过的步骤保留结果，不会重跑整份任务。step_ids 接受 T5、5 或「步骤 5」。任务还在运行时会拒绝。当前有确认编号时必须带上这次的 confirmation_id，编号不匹配不会消耗它。可以带 client_request_id，受理前同一个编号可以再试。可选 expected_status_version：不一致时 code 是 status_version_conflict，不启动，也不占用 client_request_id。可选 dry_run：只预检，不要求编码检查点，也不启动。",
			"inputSchema": object(map[string]any{
				"project_path":            str("任务的 project_path。"),
				"step_ids":                map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "要重试的步骤编号。也可以传一条用逗号分隔的字符串。"},
				"confirmation_id":         str("当前 get_task 返回的 confirmation_id。有确认时必填。"),
				"client_request_id":       str("调用方自己的唯一请求编号。超时后用 get_request 查询。"),
				"wait":                    map[string]any{"type": "boolean", "description": "为 true 时阻塞到本轮结束。默认 false。"},
				"expected_status_version": map[string]any{"type": "integer", "description": "调用方看到的 status_version。省略时不比较。别名 expectedStatusVersion。"},
				"dry_run":                 map[string]any{"type": "boolean", "description": "为 true 时只预检，不启动。别名 dryRun。"},
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
			"description": "把任务工作目录里选定的文件打成一个 zip。返回每个文件的路径、大小、sha256，以及压缩包的 sha256。内容用 base64，最大 16MiB。成功时 snapshot 为 true。可选 expected_sha256：与压缩包不一致时 code 是 artifact_stale，不返回压缩包内容。目录、链接、密钥和越界路径不会打进包，列在 omitted 里。远程编程任务的目录在 SSH 主机上，不从本机打包。",
			"inputSchema": object(map[string]any{
				"project_path":    str("任务的 project_path。"),
				"paths":           map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "相对工作目录的文件路径。也可以传一条用逗号分隔的字符串。"},
				"expected_sha256": str("期望的压缩包 sha256。可以带 sha256: 前缀。别名 expectedSha256。不一致时不返回内容。"),
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
	location, _ := mcpLookupString(args, "location")
	cloudID, _ := mcpLookupString(args, "cloud_workspace_id", "cloudWorkspaceId", "workspace_id")
	grantID, _ := mcpLookupString(args, "grant_id", "grantId")
	port, portErr := mcpLookupPort(args, "ssh_port", "sshPort")
	argError := ""
	if portErr != nil {
		argError = portErr.Error()
	}
	return mcpCreateArgs{
		Name:             name,
		Prompt:           prompt,
		Mode:             mode,
		WorkingDir:       dir,
		Provider:         provider,
		Model:            model,
		Wait:             mcpLookupBool(args, "wait"),
		ClientRequestID:  clientRequestID,
		SSHHost:          host,
		SSHUser:          user,
		SSHPassword:      password,
		SSHPort:          port,
		RemoteWorkDir:    remoteDir,
		Location:         location,
		CloudWorkspaceID: cloudID,
		GrantID:          grantID,
		ArgError:         argError,
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

// mcpLookupOptionalInt reports whether the key was present. A present 0 is
// a real version, not a missing argument. A value that is not an integer
// stays present and compares as -1, so it cannot match a live version.
func mcpLookupOptionalInt(args map[string]any, keys ...string) (int, bool) {
	if args == nil {
		return 0, false
	}
	for _, key := range keys {
		value, ok := args[key]
		if !ok || value == nil {
			continue
		}
		switch typed := value.(type) {
		case float64:
			if typed != float64(int(typed)) {
				return -1, true
			}
			return int(typed), true
		case int:
			return typed, true
		case string:
			n, err := strconv.Atoi(strings.TrimSpace(typed))
			if err != nil {
				return -1, true
			}
			return n, true
		default:
			return -1, true
		}
	}
	return 0, false
}

func mcpLookupOptionalString(args map[string]any, keys ...string) (string, bool) {
	if args == nil {
		return "", false
	}
	for _, key := range keys {
		value, ok := args[key]
		if !ok || value == nil {
			continue
		}
		switch typed := value.(type) {
		case string:
			return strings.TrimSpace(typed), true
		case float64:
			return strconv.FormatFloat(typed, 'f', -1, 64), true
		case int:
			return strconv.Itoa(typed), true
		default:
			return "", true
		}
	}
	return "", false
}

// mcpLookupArtifactCursor reads a path cursor. A numeric cursor is the path
// text, and only list_artifacts uses it. read_log keeps the integer cursor.
func mcpLookupArtifactCursor(args map[string]any) string {
	text, ok := mcpLookupOptionalString(args, "cursor", "artifact_cursor", "artifactCursor")
	if !ok {
		return ""
	}
	return text
}

func mcpLookupBoolKeys(args map[string]any, keys ...string) bool {
	for _, key := range keys {
		if mcpLookupBool(args, key) {
			return true
		}
	}
	return false
}

func mcpCallOptsFromArgs(name string, args map[string]any) mcpCallOpts {
	opts := mcpCallOpts{}
	if n, ok := mcpLookupOptionalInt(args, "expected_status_version", "expectedStatusVersion"); ok {
		opts.HasExpected = true
		opts.ExpectedVersion = n
	}
	opts.DryRun = mcpLookupBoolKeys(args, "dry_run", "dryRun")
	switch name {
	case "list_artifacts":
		opts.ArtifactCursor = mcpLookupArtifactCursor(args)
		if n, ok := mcpLookupOptionalInt(args, "limit"); ok {
			opts.HasLimit = true
			opts.Limit = n
		}
		opts.IncludeHidden = mcpLookupBoolKeys(args, "include_hidden", "includeHidden")
		opts.ExcludeDirs = mcpLookupPathList(args, "exclude_dirs", "excludeDirs")
	case "read_artifact", "pack_artifacts":
		if sum, ok := mcpLookupOptionalString(args, "expected_sha256", "expectedSha256"); ok {
			opts.HasExpectedSHA = true
			opts.ExpectedSHA = sum
		}
	case "send_command":
		if provider, ok := mcpLookupOptionalString(args, "provider"); ok {
			opts.Provider = provider
		}
		if model, ok := mcpLookupOptionalString(args, "model"); ok {
			opts.Model = model
		}
	}
	return opts
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
