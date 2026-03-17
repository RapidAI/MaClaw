# AI 编程工具无头（Headless）协议参考

基于 [hapi](https://github.com/tiann/hapi) 项目源码分析，记录各 AI 编程工具的无头工作模式协议细节。

## 1. Claude Code — Stream JSON 双向协议

**启动参数：**
```
claude --output-format stream-json --input-format stream-json --verbose --include-partial-messages
```

**权限控制参数：**
- `--dangerously-skip-permissions` — 跳过所有权限检查（YOLO 模式）
- `--permission-prompt-tool stdio` — 通过 stdin/stdout 处理权限请求

**通信协议：** stdin/stdout 逐行 JSON

**输入格式（stdin）：**
```json
// 文本消息
{"type":"user","message":{"role":"user","content":"hello"}}

// 图片消息（多部分）
{"type":"user","message":{"role":"user","content":[
  {"type":"text","text":"describe this"},
  {"type":"image","source":{"type":"base64","media_type":"image/png","data":"..."}}
]}}

// 权限响应
{"type":"control_response","response":{"subtype":"success","request_id":"xxx","response":{"behavior":"allow"}}}

// 中断请求
{"type":"control_request","request_id":"xxx","request":{"subtype":"interrupt"}}
```

**输出格式（stdout）：**
```json
// 系统初始化
{"type":"system","subtype":"init","session_id":"xxx"}

// 助手消息（含 tool_use）
{"type":"assistant","message":{"role":"assistant","content":[
  {"type":"text","text":"..."},
  {"type":"tool_use","id":"xxx","name":"Bash","input":{"command":"ls"}}
]}}

// 流式事件（部分内容）
{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"..."}}}

// 结果
{"type":"result","result":{"duration_ms":1234,"num_turns":3}}

// 权限请求
{"type":"control_request","request_id":"xxx","request":{"subtype":"can_use_tool","tool_name":"Bash","input":{...}}}

// 权限取消
{"type":"control_cancel_request","request_id":"xxx"}
```

**会话恢复：** 通过 `--resume` 参数或 session_id 字段

---

## 2. Cursor Agent — Stream JSON 单向协议

**启动参数：**
```
agent -p <message> --output-format stream-json --trust --workspace <cwd>
```

**可选参数：**
- `--resume <sessionId>` — 恢复会话
- `--model <model>` — 指定模型
- `--yolo` — 自动批准所有操作
- `--mode plan|ask` — 设置工作模式

**安装方式：**
```bash
curl https://cursor.com/install -fsS | bash
```

**通信协议：** stdout 逐行 JSON（只读，每次消息启动新进程）

**输出事件类型：**
```json
// 系统初始化（返回 session_id）
{"type":"system","subtype":"init","session_id":"xxx"}

// 思考中
{"type":"thinking","subtype":"started|completed"}

// 助手回复
{"type":"assistant","text":"..."}

// 工具调用
{"type":"tool_call","name":"edit_file","input":{...},"id":"xxx"}

// 工具结果
{"type":"result","output":{...},"id":"xxx"}
```

**特点：**
- 每次用户消息启动一个新的 `agent` 子进程（非持久连接）
- 中断通过 `SIGTERM` 信号
- 与 Claude Code 的 stream-json 格式高度兼容，可复用同一套解析逻辑

---

## 3. OpenAI Codex — 双后端架构

### 3a. App Server 模式（默认）

**启动方式：**
```
codex app-server
```
通过 `spawn('codex', ['app-server'])` 启动子进程。

**通信协议：** stdin/stdout 逐行 JSON-RPC Lite（无 `jsonrpc` 字段）

**客户端→服务端（请求）：**
```json
// 初始化
{"id":1,"method":"initialize","params":{"clientInfo":{"name":"hapi-codex-client","version":"1.0.0"},"capabilities":{"experimentalApi":true}}}

// 创建线程
{"id":2,"method":"thread/start","params":{...}}

// 恢复线程
{"id":3,"method":"thread/resume","params":{"threadId":"xxx",...}}

// 开始回合
{"id":4,"method":"turn/start","params":{"threadId":"xxx","message":"hello",...}}

// 中断回合
{"id":5,"method":"turn/interrupt","params":{"threadId":"xxx","turnId":"xxx"}}

// 通知（无 id）
{"method":"initialized"}
```

**服务端→客户端（通知/事件）：**
```json
// 线程创建
{"method":"thread_started","params":{"thread_id":"xxx"}}

// 任务开始/完成
{"method":"task_started","params":{"turn_id":"xxx"}}
{"method":"task_complete","params":{}}
{"method":"task_failed","params":{"error":"..."}}
{"method":"turn_aborted","params":{}}

// 助手消息
{"method":"agent_message","params":{"message":"..."}}

// 推理
{"method":"agent_reasoning_delta","params":{"delta":"..."}}
{"method":"agent_reasoning","params":{"text":"..."}}

// 命令执行
{"method":"exec_command_begin","params":{"call_id":"xxx","command":"ls"}}
{"method":"exec_command_end","params":{"call_id":"xxx","output":"..."}}

// 文件修改
{"method":"patch_apply_begin","params":{"call_id":"xxx","changes":{...}}}
{"method":"patch_apply_end","params":{"call_id":"xxx","success":true}}

// MCP 工具调用
{"method":"mcp_tool_call_begin","params":{"call_id":"xxx","invocation":{"server":"xxx","tool":"xxx","arguments":{}}}}
{"method":"mcp_tool_call_end","params":{"call_id":"xxx","result":{...}}}

// Token 统计
{"method":"token_count","params":{...}}

// Diff
{"method":"turn_diff","params":{"unified_diff":"..."}}
```

**权限处理：** 服务端通过反向 RPC 请求（带 id 的 method 调用）向客户端请求权限。

### 3b. MCP Server 模式

通过环境变量 `CODEX_USE_MCP_SERVER=1` 启用。使用标准 MCP 协议，调用 `startSession` / `continueSession`。

### 3c. Exec 模式（一次性）

**启动参数：**
```
codex exec --json <prompt>
```

**通信协议：** stdout JSONL（只读，一次性执行）

**事件类型：**
```json
{"type":"thread.started","thread_id":"xxx"}
{"type":"turn.started"}
{"type":"item.started","item":{"id":"xxx","item_type":"assistant_message"}}
{"type":"item.updated","item":{"id":"xxx","item_type":"assistant_message","text":"..."}}
{"type":"item.completed","item":{"id":"xxx","item_type":"assistant_message","text":"..."}}
{"type":"item.started","item":{"id":"xxx","item_type":"command_execution","command":"ls"}}
{"type":"item.completed","item":{"id":"xxx","item_type":"command_execution","exit_code":0,"aggregated_output":"..."}}
{"type":"item.started","item":{"id":"xxx","item_type":"file_change","file_path":"main.go"}}
{"type":"turn.completed","usage":{"input_tokens":100,"output_tokens":50}}
{"type":"turn.failed","error":"..."}
```

**后续消息：** `codex exec resume --last <message>`

---

## 4. Gemini CLI — ACP (Agent Communication Protocol)

**启动参数：**
```
gemini --experimental-acp [--model <model>] [--yolo]
```

**通信协议：** stdin/stdout JSON-RPC（标准化 ACP 协议）

**客户端→服务端：**
```json
// 初始化
{"id":1,"method":"initialize","params":{"protocolVersion":1,"clientCapabilities":{"fs":{"readTextFile":false},"terminal":false},"clientInfo":{"name":"hapi","version":"1.0.0"}}}

// 创建会话
{"id":2,"method":"session/new","params":{"cwd":"/path","mcpServers":[...]}}

// 加载已有会话
{"id":3,"method":"session/load","params":{"sessionId":"xxx","cwd":"/path","mcpServers":[...]}}

// 发送提示
{"id":4,"method":"session/prompt","params":{"sessionId":"xxx","prompt":[{"type":"text","text":"hello"}]}}

// 取消提示（通知，无 id）
{"method":"session/cancel","params":{"sessionId":"xxx"}}
```

**服务端→客户端（通知）：**
```json
// 会话更新（增量）
{"method":"session/update","params":{"sessionId":"xxx","update":{...}}}
```

**权限处理：** 服务端通过反向 RPC 请求：
```json
// 服务端请求权限
{"id":100,"method":"session/request_permission","params":{"sessionId":"xxx","toolCall":{"toolCallId":"xxx","title":"Run command","kind":"shell","rawInput":{...}},"options":[{"optionId":"allow","name":"Allow","kind":"allow_once"},{"optionId":"deny","name":"Deny","kind":"reject_once"}]}}

// 客户端响应
{"id":100,"result":{"outcome":{"outcome":"selected","optionId":"allow"}}}
```

**特点：**
- prompt 请求无超时限制（`timeoutMs: Infinity`）
- 有"静默期"机制：发送 prompt 前后等待 session/update 安静一段时间确保状态同步
- 支持 `session/load` 恢复已有会话

---

## 5. OpenCode — HTTP Server + SSE + Hook 插件

### 5a. HTTP Server 模式

**启动参数：**
```
opencode --server --port <PORT> [--session <sessionId>]
```

**HTTP API：**
- `GET /events` — SSE 事件流
- `POST /message` — 发送用户消息
- `GET /health` — 健康检查

**SSE 事件格式：**
```
event: <event_type>
data: <json_payload>
```

### 5b. Hook 插件模式（本地）

OpenCode 支持通过 hook 插件将事件 POST 到指定 URL。

**配置方式：** 在 OpenCode 配置目录注入 hook 插件，设置环境变量：
- `OPENCODE_CONFIG_DIR` — 配置目录
- `OPENCODE_CONFIG` — 配置文件路径
- `HAPI_OPENCODE_HOOK_URL` — Hook 回调 URL
- `HAPI_OPENCODE_HOOK_TOKEN` — 认证 Token

**Hook 事件类型：**
```json
// 会话事件
{"event":"session.created","sessionId":"xxx","payload":{...}}
{"event":"session.updated","sessionId":"xxx","payload":{...}}

// 消息事件
{"event":"message.updated","payload":{"id":"xxx","role":"assistant",...}}
{"event":"message.part.updated","payload":{"id":"xxx","type":"text","text":"...","delta":"..."}}

// 工具执行事件
{"event":"tool.execute.before","payload":{"tool":{"name":"shell","input":{...}}}}
{"event":"tool.execute.after","payload":{"tool":{"name":"shell","content":"..."}}}

// 权限事件
{"event":"permission.asked","payload":{"id":"xxx","permission":"shell","metadata":{...}}}
{"event":"permission.replied","payload":{"id":"xxx","response":"once","approved":true}}
```

---

## 6. Kilo — HTTP Server + SSE（OpenCode 分支）

与 OpenCode 协议完全相同，仅启动命令不同：

**启动参数：**
```
kilo serve --port <PORT>
```

**环境变量：** `KILO_SERVER_PORT` 替代 `OPENCODE_SERVER_PORT`

---

## 共性架构模式

| 模式 | 工具 | 通信方式 |
|------|------|----------|
| Stream JSON 双向 | Claude Code, Cursor Agent, CodeBuddy | stdin/stdout 逐行 JSON |
| JSON-RPC Lite | Codex App Server | stdin/stdout 逐行 JSON-RPC |
| ACP (JSON-RPC) | Gemini CLI | stdin/stdout 标准 JSON-RPC |
| JSONL 只读 | Codex Exec | stdout JSONL |
| HTTP + SSE | OpenCode, Kilo | HTTP API + SSE 事件流 |

### 权限模型统一

所有工具的权限请求都可以映射到统一模型：
- `auto-approve` / `yolo` — 自动批准所有操作
- `default` — 需要逐个审批
- `read-only` — 拒绝写入/执行操作
- `approved_for_session` — 本次会话内自动批准同类操作

### 会话生命周期

```
Initialize → Create/Resume Session → Send Prompt → Process Events → Wait for Input → Repeat
                                                                          ↓
                                                                    Interrupt/Cancel
                                                                          ↓
                                                                    Cleanup/Exit
```
