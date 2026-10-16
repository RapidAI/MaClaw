# MaClawSrv / MaClaw GUI 架构代码审查与优化建议

审查范围：`MaClawSrv/`、`corelib/agentservice/`、`corelib/agent/` 以及 GUI Agent 相关代码（尤其是 `gui/im_agent_loop_shared.go`、`gui/im_handler_wiring.go`）。

审查目标：让 **MaClawSrv 成为 MaClaw GUI 的无头版本**，并能够在 GUI 完成 Agent loop、工具、技能、MCP、记忆、知识库、编码运行时等改进后，由 srv 自动获得同一行为，而不是再维护一套近似实现。

## 1. 结论摘要

当前代码已经具备较好的共享基础：`MaClawSrv` 使用 `corelib/agentservice.CoreAgentExecutor`，执行路径最终调用 `corelib/agent.RunLoop`；租户、用户、实例、会话、运行记录、知识库和动态能力也主要位于 `corelib`。这解决了早期“srv 只有 HTTP 骨架”的问题。

但它还不能严格称为“GUI 的无头版本”。GUI 的大量行为仍然封装在 `gui` 这个 `package main` 中，srv 只能使用已经手工提取到 `corelib/agentservice` 的子集。GUI 的新能力若没有同步迁移，srv 不会自动获得；而且两侧已经出现不同的 prompt、工具面、事件和生命周期语义。

原文中“新增 `corelib/agentruntime`”还不够严格：如果 GUI 继续保留一套 callback、srv 再实现一套 callback，即使两边都调用 `RunLoop`，仍然会发生行为漂移。因此下一阶段的架构目标必须定义为：

> **一个可导入的、无 UI 的 Agent Runtime；GUI 和 MaClawSrv 都只是 Host/Transport Adapter。**

> **只有一份非界面 Agent 实现；GUI 和 MaClawSrv 都只能通过 Host/Transport Adapter 使用它。**

“自然共享”的判定标准是：开发者修改一次非界面 Agent 规则，GUI 和 srv 在同一次构建中自动得到该修改；不允许再有“同步迁移到另一侧”的人工步骤。优先级最高的工作不是继续给 `MaClawSrv/http.go` 增加 endpoint，而是先落实唯一实现、依赖方向、事件协议和生命周期管理，再让 HTTP/Wails 分别接入。

### 1.1 不可违反的架构不变量

1. `gui/` 和 `MaClawSrv/` 中与 Agent 行为有关的代码只能是 UI、传输、宿主能力和装配；不得新增 prompt、tool switch、Agent loop、workflow 规则或 Agent 安全策略。服务专属的 IM/设备控制面仍可留在 srv，但不能进入共享 Agent Runtime。
2. 所有可被 GUI Agent 使用的非界面逻辑只能位于 `corelib`；`corelib` 不得 import `gui`、Wails、Bubble Tea 或 HTTP server。
3. GUI 与 srv 的 Agent 入口必须接受同一个 `agent.Runtime` 接口；禁止 GUI 直接构造私有 callback、srv 直接调用另一套 executor。
4. 工具、Prompt section、策略和事件都必须通过 core registry/module 注册；禁止以文件复制、字符串黑名单或 handler `switch` 扩展能力。
5. 宿主差异只能由 `HostCapabilities`/`CapabilityProfile` 表达。没有宿主能力时，工具应以稳定的 `capability_unavailable` 结果呈现，而不是从注册表中消失或变成 `unknown tool`。
6. 每一个 Runtime 改动必须同时通过 GUI 与 headless contract test；CI 必须能阻止“只改 GUI”或“只改 srv”的非界面提交。

## 2. 当前实现证据

| 观察 | 代码证据 | 影响 |
|---|---|---|
| srv 通过共享 loop 执行 Agent | `corelib/agentservice/core_agent_executor.go:420-438, 567-570` | 共享了 `RunLoop`，方向正确 |
| GUI 仍有更大的专用 callback/编排层 | `guiapp/im_agent_loop_shared.go:1121-5200`；约 5,200 行 | GUI 改进不一定进入 srv |
| srv callback 是另一套实现 | `corelib/agentservice/core_agent_executor.go:1150-2829`；约 1,700 行 callback 逻辑 | prompt、工具门控、事件和降级行为可能漂移 |
| GUI bridge 只能在 GUI 二进制内注册 | `guiapp/agent_handler_bridge.go:16-42` | srv 无法直接 import GUI Agent；只能复制/提取代码 |
| HTTP 路由与 handler 过于集中 | `MaClawSrv/http.go` 约 5,800 行，路由注册从 `:645` 延伸至近千行 | 改一个 API 容易影响认证、序列化和其他域 |
| Service 也承担大量域和运行时职责 | `corelib/agentservice/service.go` 约 5,800 行 | 持久化、运行编排、配置、审计和恢复耦合 |
| 构造函数启动后台副作用 | `MaClawSrv/http.go:174-220`：启动模型下载、微信/IM runtime、release catalog、sandbox diagnose | 无法做纯构造测试；部分失败只能运行时发现 |
| 构造错误被压成 nil | `MaClawSrv/http.go:183-190` 返回 `nil`；`MaClawSrv/main.go:158-166` 无条件调用 `server.Handler()` | 初始化失败时可能 panic，而不是可诊断启动错误 |
| srv shutdown 没有关闭 Service | `MaClawSrv/main.go:196-211` 只关闭 HTTP、gateway、knowledge；没有 `svc.Close()` | SQLite、动态 registry、内存持久化 goroutine 可能泄漏或丢数据 |
| run 事件实际是轮询快照 | `MaClawSrv/http.go:4789-4863` 每 300ms 查询并发送 snapshot | 不等同 GUI 的 token/tool/progress 流；高并发下产生无意义读放大 |
| 消息提交是同步执行 | `MaClawSrv/http.go:4749-4767` → `Service.PostMessage`；`service.go:2964-3131` 直接调用 executor | 长模型调用占用 HTTP worker；客户端断线与后台运行语义不清晰 |
| 幂等检查与写入不是原子操作 | `service.go:2858-2862`、`2920-2930` 先查后建；随后 `2964+` 保存 message/run | 并发重试可能产生重复 session/message/run |
| capability parity 测试偏“目录存在性” | `corelib/agentservice/gui_srv_catalog_parity_test.go` 主要断言工具名称和 enabled 状态 | 不能证明 GUI 与 srv 对同一输入有相同 prompt、工具选择和结果 |
| 工具调用参数/别名曾由 GUI 维护 | `gui/im_tool_execution.go`、`im_tools_local.go` 与 service callback 各自解析 JSON/alias | 同一个模型输出在 GUI、srv、TUI 可能落到不同 canonical tool 或错误路径 |

## 3. 重点问题与改进建议

### P0：修复启动失败路径，禁止 nil server 进入运行态

`NewHTTPServer` 在动态能力 registry 或 publisher 创建失败时直接返回 `nil`，没有返回错误；主程序随后在 `server.Handler()` 解引用。这个问题与业务功能无关，却会把一个可恢复/可诊断的启动错误变成 panic。

建议：

```go
func NewHTTPServer(opts HTTPServerOptions) (*HTTPServer, error)
```

所有可选组件明确三种结果：`ready`、`disabled(reason)`、`fatal(error)`。主程序只在拿到非 nil、已完成自检的 runtime 后启动监听；启动失败要包含组件名和 remediation，不打印 secret 或绝对路径。

### P1：建立统一 Runtime，GUI 不再是 `package main` 中的能力仓库

现有 `corelib/agent.RunLoop` 已经是一个良好的底座，但真正决定 GUI 行为的 callback、prompt policy、工具门控、语义路由和状态编排仍在 `gui`。`gui/agent_handler_bridge.go` 的注册机制只对编译进 GUI 的进程有效，MaClawSrv 无法通过它复用 GUI 改进。

建议新增可导入的 `corelib/agentruntime`，并把现有 `corelib/agentservice.CoreAgentExecutor` 收敛为该 Runtime 的 service adapter（不能让新旧两套实现长期并存）。`corelib/agent` 保留通用类型和 `RunLoop` 契约，`agentruntime` 成为唯一的 Agent 行为实现：

- `Runtime` / `RuntimeOptions`：LLM、记忆、知识、技能、MCP、任务、审计和持久化依赖；
- `TurnContext`：tenant/user/instance/session、工作区、工具策略、能力 profile、取消信号；
- 统一的 `BuildPrompt`、`BuildToolSurface`、`ExecuteTool`、`RunTurn`；
- GUI 与 srv 共用的 steering、drift detection、adaptive retry、ask_user、tool-batch commit、coding-runtime 边界；
- 只通过 `HostCapabilities` 注入桌面捕获、文档打开、URL 打开、音频播放等宿主能力；
- `ToolModule`、`PromptContributor`、`PolicyModule`、`EventSink` 四类扩展点，顺序和版本由 Runtime 管理。

迁移顺序应是“移动实现，不重写语义”，并且每一步都让旧入口变成薄代理：

1. 在 `corelib/agentruntime` 定义 facade 和 contract test；先由 `core_agent_executor.go` 实现 facade，保证 srv 行为不变。
2. 将 `gui/im_agent_loop_shared.go`、`gui/im_system_prompt.go`、`gui/semantic_tool_routing.go` 中不依赖 Wails/桌面状态的代码原样移动到 Runtime；GUI 通过 `GUIHostCapabilities` 接回界面副作用。
3. 将 `MaClawSrv` 的 `CoreAgentExecutor` 改为 `agentservice.RuntimeExecutor` 薄适配器，只负责 principal、repository 和 headless capabilities 注入。
4. GUI 的 `IMMessageHandler` 保留兼容外壳，但内部只能调用 Runtime；srv、TUI 和 GUI 都走同一 facade 后，再删除旧的重复 callback 和工具 switch。
5. 在删除旧实现前加入依赖检查：任何 `gui/` 或 `MaClawSrv/` 文件出现 `BuildSystemPrompt`、`ExecuteTool`、工具名 switch、策略判断等非 UI 逻辑时 CI 失败。

现有 `gui` 为 `package main` 是迁移的硬阻塞点。应拆成 `guiapp`（可导入的 Wails/IM host package）与 `cmd/maclaw-gui`（仅 `main` 和启动参数）；拆包完成前，不允许把 `gui` 当作 srv 的依赖。否则 Go 编译器会迫使团队继续复制代码或使用脆弱的 `init` bridge。

推荐的代码归属如下：

| 当前文件/能力 | 目标归属 | 迁移判定 |
|---|---|---|
| `gui/im_agent_loop_shared.go` 的 turn 编排、tool policy、retry、drift | `corelib/agentruntime/turn`、`policy` | 原样迁移，禁止保留 GUI 分支 |
| `gui/im_system_prompt.go`、`im_system_prompt_gui_sections.go` | `corelib/agentruntime/prompt` | GUI 特有文案改为 `PromptContributor`，核心规则共享 |
| `gui/im_tool_definitions.go`、`semantic_tool_routing.go` | `corelib/agentruntime/modules` | 工具 schema/路由只保留一份 |
| `gui/im_tool_execution.go` / `im_tools_local.go` 的参数清理与兼容别名 | `corelib/agentruntime/tool_calls.go` | 归一化、object 解析、browser/Glob alias 只能由 Runtime 提供；Host 仅执行策略和副作用 |
| `gui/im_handler_wiring.go` | `guiapp/adapter` | 仅装配和 Wails/IM 回调，不保留 Agent 规则 |
| `corelib/agentservice/core_agent_executor.go` | `agentservice/runtime_adapter.go` | 变成 principal/repository/port 适配器 |
| `MaClawSrv/http.go` | `MaClawSrv/transport` | 只做 HTTP DTO、认证、响应和事件订阅 |

禁止“先复制到 corelib，再让两边并行演进”。迁移期间只能有一个 authoritative implementation，旧位置只能转发调用；否则无法保证后续 GUI 改进自然进入 srv。

建议先冻结以下最小 contract（名称可调整，但职责不可合并回 host）：

```go
type Runtime interface {
    Execute(context.Context, TurnRequest) (TurnResult, error)
    DescribeCapabilities(context.Context, CapabilityRequest) (CapabilitySnapshot, error)
    Close() error
}

type TurnRequest struct {
    Scope       PrincipalScope       // tenant/user/instance/session
    Message     UserMessage
    History     []ConversationEntry
    Config      EffectiveConfig
    Policy      ExecutionPolicy
    Host        HostCapabilities
    Events      EventSink
}

type HostCapabilities interface {
    Profile() CapabilityProfile
    DesktopCapture() (DesktopCapturePort, bool)
    DocumentLauncher() (DocumentLauncherPort, bool)
    URLLauncher() (URLLauncherPort, bool)
    SpeechRenderer() (SpeechRendererPort, bool)
}
```

`Runtime` 必须是无 UI、可并发调用的核心；`TurnRequest` 携带请求级依赖，不能依赖包级当前用户、当前项目或全局 Wails 状态。需要跨轮次的 steering、ask_user、memory 和 run 状态由 `agentservice` 通过 port 提供，并由 `Scope` 做强隔离。

宿主能力接口应优先使用这种**类型化、最小权限**的 port，而不是 `Invoke(action string, args map[string]any)`。后者很容易重新形成“模型参数决定宿主动作”的旁路；每个 port 都应在 Runtime 外完成 principal、路径、大小、超时和审计校验。

模块注册也要分成两类：

- **静态 builtin module**：编译进 `corelib`，通过 `RegisterBuiltinModules` 统一注册，GUI/srv 不得手工列清单；
- **动态 tenant module**：由 service 返回已授权 descriptor（Skill/MCP/企业能力），Runtime 只按 descriptor + `HostCapabilities` 构造受限 surface，执行仍经过同一个 core dispatcher。

这样既能让 GUI 新增 builtin tool 自动进入 srv，也不会把某个租户的动态能力或宿主权限误当成全局能力。

### P1：统一事件模型，解决同步 POST 与伪 SSE

GUI 已经有 token、tool call、tool result、new round 等回调；srv 的 `/runs/{id}/events` 目前只是每 300ms 读取数据库快照。两者对客户端呈现的运行语义不同。

建议定义版本化事件：

```text
run.started
turn.started
assistant.delta
tool.call
tool.result
checkpoint
ask_user
run.completed | run.failed | run.cancelled
```

每个事件至少带 `event_id`、`run_id`、`sequence`、`occurred_at`、`tenant_id`（对外响应可脱敏）和 `schema_version`。事件写入 durable outbox，再由 SSE/NDJSON/WebSocket adapter 推送；SSE 支持 `Last-Event-ID` 断线续传，而不是重复全量 snapshot。

Outbox 必须和 run/message 状态在同一个 repository 事务中提交：先写 `run.started`，再允许执行；终态事件与终态 run 一起提交。`sequence` 由 `(tenant, run)` 作用域的唯一约束生成，重复提交返回已有事件。GUI 可以使用内存 event sink 做即时渲染，但同一事件模型仍要由 Runtime 发出，不能让 GUI 定义另一套事件类型。

兼容现有客户端的方式：

1. 保留现有同步 POST 返回格式；
2. 增加 `Prefer: respond-async`（已实现，返回 `202 + run + status_url`；同时兼容 `?async=true`）；
3. 新客户端订阅事件流，旧客户端继续 GET run；
4. `POST /cancel` 同时取消 request context 和 durable runtime task。

这样既不会破坏已有 API，又能让 srv 复用 GUI 的流式改进。

### P1：把幂等性从“查找约定”提升为存储约束

`client_message_id` 和 `client_session_key` 当前是“先 list，再 create”。并发重试时两个请求可以同时看不到记录，然后各自创建资源。

建议：

- 在 repository 层增加原子方法，例如 `CreateMessageRunIdempotent(ctx, key, ...)`；
- SQLite 增加 `(tenant_id, user_id, instance_id, session_id, client_message_id)` 唯一索引；
- session key 也增加 principal/instance 作用域唯一索引；
- 返回已有资源时标记 `idempotent_replay=true`，便于客户端和审计判断；
- 不要把幂等保证放在 HTTP handler 或内存 map 中。

### P1：补齐关闭顺序和所有权

`Service.Close` 已经存在，但主程序正常 shutdown 没有调用。`CoreAgentExecutor` 又缓存了按用户打开的 `memory.Store`，这些 store 自己带后台保存 loop，需要 `Stop()`。

建议引入单一 composition root：

```text
Runtime.Close()
  -> stop ingress (HTTP/WebSocket/IM)
  -> drain/cancel active runs
  -> stop scheduler/background workers
  -> stop per-user memory stores and MCP processes
  -> close coding ledger / dynamic registries / SQLite
  -> flush audit/outbox
```

`Close` 必须幂等，并返回聚合错误。主程序在所有退出分支（监听失败、context cancel、初始化中途失败）都走同一 cleanup 栈；不要依赖散落的 `defer` 只关闭部分组件。

### P1：配置 schema 只能有一个来源

srv 目前通过 `sharedClientConfigKeys`、`maclawSrvHiddenAppConfigKeys`、`preserve/strip...Config` 等手工列表决定哪些 GUI 配置可见或可写。GUI 新增 `AppConfig` 字段时，很容易出现“GUI 可用、srv 不可用”或保存时被静默清掉。

建议：

- 由 `corelib/config` 生成带 `scope`、`secret`、`mutable`、`restart_required`、`headless_supported` 的 schema；
- GUI 和 srv 都消费同一 schema；
- 服务端只做 policy overlay（例如禁止桌面专属字段），不再维护字段名黑名单；
- 每次配置更新返回 `config_revision` 和生效状态；
- 增加 schema parity CI：对 GUI、srv 的可见字段、默认值、校验结果做快照比较。

### P2：拆分 HTTP Transport 与业务域

`http.go` 目前同时承担路由、认证、JSON 解码、分页、run 流、实例、配置、技能、MCP、知识、设备和平台集成。建议按端口拆分，而不是继续堆 handler：

```text
MaClawSrv/
  transport/http/       # middleware、error、pagination、SSE
  transport/openapi/    # schema / generated docs
  api/agent/             # instances/sessions/runs/messages
  api/admin/             # admin/auth/config/ops
  api/knowledge/        # knowledge endpoints
  api/integration/      # IM/device/platform
```

每个 handler 只做：认证后的 principal 解析 → request DTO 校验 → 调用 application service → response mapping。路径注册可用分组函数或生成代码，避免一个函数内维护数百条路由。

### P2：把后台任务从内存 manager 升级为可恢复队列

技能安装、模型下载、知识导入、MCP 操作已有 async job，但不同域的 job 状态和恢复策略不完全一致。建议统一 `Job` 接口：持久化状态、租户作用域、进度事件、重试策略、取消策略、幂等键和恢复检查点。进程重启后只能按明确的 replay policy 恢复；对可能产生外部副作用的任务默认进入 `unknown/reconcile`，不能自动重放。

### P2：可观测性要以 run 为主线

在所有入口生成 `request_id`，并把 `trace_id`、`run_id`、`tenant_id`、`user_id`（可哈希）贯穿日志、审计、metrics 和事件。标准 `log.Printf` 可保留为 fallback，但生产应使用结构化 logger，并对 URL、文件路径、token、provider key 做统一 redaction。metrics 除总量外，至少增加：首 token 延迟、run 时长分位数、tool 错误率、队列等待、事件丢失/重放、每租户并发和限流。

## 4. 推荐目标架构

```text
                 ┌──────────────────────────┐
                 │ corelib/agentruntime     │
                 │ prompt / loop / tools    │
                 │ policy / events / cancel │
                 └─────────────┬────────────┘
                               │
              ┌────────────────┴────────────────┐
              │                                 │
   ┌──────────▼──────────┐          ┌───────────▼──────────┐
   │ GUI Host Adapter     │          │ Headless Host Adapter │
   │ Wails / desktop      │          │ MaClawSrv / HTTP      │
   │ capture/open/play    │          │ no desktop side effect│
   └──────────┬──────────┘          └───────────┬──────────┘
              │                                 │
   ┌──────────▼──────────┐          ┌───────────▼──────────┐
   │ GUI Transport        │          │ Service Application   │
   │ Wails/IM callbacks   │          │ tenant/session/run    │
   └─────────────────────┘          └───────────┬──────────┘
                                                │
                                    ┌───────────▼──────────┐
                                    │ HTTP/OpenAPI/SSE      │
                                    │ admin + user APIs     │
                                    └──────────────────────┘
```

关键边界：

- Runtime 不依赖 Wails、Bubble Tea、HTTP 或 `package main`；
- Service 拥有 principal、数据隔离、repository 和 durable run；
- Host Adapter 只能提供显式能力，不得从模型参数反推宿主权限；
- Transport 不直接调用 executor，所有执行都经过 Service/Runtime；
- GUI 与 srv 的差异由 capability profile 和 event sink 表达，而不是复制 loop。

### 4.0 Runtime 的状态与生命周期

为避免“共享代码”变成“共享可变全局状态”，采用以下模型：

```text
进程级：BuiltinModuleRegistry（只读）
请求级：TurnRuntime（可取消、不可跨请求复用）
会话级：SessionState / Conversation / Steering（由 agentservice 持久化）
租户级：Skill/MCP/Config/Quota（由 service repository 隔离）
宿主级：HostCapabilities（GUI 或 headless 注入）
```

- `Runtime` 可由一个进程共享，但每次 `Execute` 都必须创建独立的 `TurnRuntime`；不在 Runtime 中保存“当前 session/user”。
- 所有缓存必须声明作用域和上限（tenant/user/session），并有失效和关闭路径；禁止用 `map[userID]*Store` 作为唯一生命周期管理。
- `Close` 先禁止新 turn，再等待/取消在途 turn，最后关闭 port 实现（memory、MCP 子进程、SQLite、outbox）。GUI 和 srv 调用同一个关闭接口。
- 服务重启后的恢复由 `agentservice` 负责，Runtime 不自行重放有副作用的 tool；恢复只接收经过授权的 checkpoint。

一次请求的唯一执行路径应固定为：

```text
GUI Wails/IM 或 srv HTTP
  -> agentservice.SubmitTurn (鉴权、幂等、加载 scope/config)
  -> agentruntime.Execute(TurnRequest)
  -> EventSink + ports（tool/memory/knowledge/MCP）
  -> agentservice 持久化 run/message/checkpoint
  -> 各自 Transport 渲染响应
```

任何入口绕过 `SubmitTurn` 直接调用 LLM、工具或 `RunLoop`，都视为架构违规；这条规则能保证多租户隔离、审计、取消和 GUI/srv 语义始终经过同一层。

### 4.1 包依赖与职责矩阵

```text
corelib/agent          # 公共类型、RunLoop/Hook 契约、事件和错误码
        ↑
corelib/agentruntime   # 唯一 prompt/tool/policy/workflow/turn 实现
        ↑
corelib/agentservice   # principal、repository、run/job、审计、恢复
        ↑                         ↑
GUI Host/Transport                 MaClawSrv HTTP/IM Transport
```

| 能力 | 唯一实现位置 | GUI 允许做什么 | srv 允许做什么 |
|---|---|---|---|
| Agent loop / prompt / tool surface | `corelib/agentruntime` | 提供 host capabilities、渲染事件 | 提供 headless capabilities、发布事件 |
| 租户/用户/实例/会话/run | `corelib/agentservice` | 调用 service API | 调用 service API |
| 文件、SSH、浏览器、桌面、音频 | `corelib` contract + Host adapter | 注入真实桌面实现 | 仅注入明确允许的服务端实现 |
| HTTP/Wails/IM/SSE | 各自 transport | DTO、认证、UI 状态 | DTO、认证、API 兼容 |
| 配置 schema | `corelib/config` | UI 展示/编辑 | policy overlay + 校验 |

### 4.2 如何保证“改一次、两端生效”

- 所有 Runtime module 在 `corelib` 注册，并由 Service composition root 的 `Config.RuntimeModules`/`RegisterBuiltinModules` 装配；GUI 和 srv 不允许自行维护模块列表。
- module 的 capability、prompt section、tool schema 和 event schema 从同一 Go 定义生成 OpenAPI/前端类型，避免手工同步。
- 每个 module 带 `ModuleID`、`Version`、`HeadlessSupport` 和 `RequiredHostCapabilities`；启动时 Runtime 输出完整 capability snapshot。
- 可调用工具必须同时实现 `ToolHandler`；只有具备 handler 的定义才进入模型工具面，调用由 `ModuleRegistry.InvokeTool` 统一路由。仅描述能力而没有执行端的 module 仍可出现在 snapshot，但不会被模型误用。
- GUI 和 srv 的构建都执行同一组 `runtime_contract_test`，测试 fixture、prompt digest、tool selection、事件序列和错误码。
- 增加静态架构 gate：检查 import 方向、禁止 `gui`/`MaClawSrv` 定义 Runtime 接口实现，禁止在 transport 层出现工具名和 prompt 关键字列表。
- 版本升级以 Runtime contract version 为准。GUI 和 srv 不得通过“兼容旧字段”各自改变 Agent 语义；兼容逻辑必须放在 core adapter。

### 4.3 避免 Go import cycle 的依赖倒置

`agentruntime` 不应反向 import `agentservice`。Runtime 只依赖窄接口（例如 `MemoryPort`、`KnowledgePort`、`SkillPort`、`MCPPort`、`RunStatePort`、`AuditPort`），由 `agentservice` 提供实现；GUI/srv Host 再提供桌面或服务端能力。这样既保持包依赖单向，也避免为了复用一个工具而把 HTTP、多租户或 SQLite 依赖带入 Runtime。

建议的调用关系是：

```text
agentservice.Service (Application Service)
  └─ owns Runtime + implements Runtime ports
       ├─ GUI adapter: Wails/IM events + GUIHostCapabilities
       └─ srv adapter: HTTP/IM events + HeadlessHostCapabilities
```

Runtime 的构造必须是显式的 `NewRuntime(RuntimeOptions)`；禁止通过包级 `init()` 注册“当前宿主”的实现。包级注册只登记无状态 module，所有有状态依赖在 composition root 注入，避免测试和多租户运行时相互污染。

### 4.4 非界面改动的强制交付规则

把每次 GUI 改动先分类：

- 纯界面（布局、Wails binding、渲染）：可以只改 `gui/`；
- 非界面（prompt、工具、工作流、策略、记忆、重试、事件、配置）：必须改 `corelib` Runtime module，并同时更新 GUI/srv contract fixture；
- 宿主能力（桌面捕获、打开文件、音频播放）：改 `HostCapabilities` contract，并为 headless 提供显式降级结果。

Pull Request 模板和 CI 应强制检查：非界面改动是否触及 Runtime、是否有两端 contract test、是否更新 module version/changelog。没有 Runtime 变更的 GUI Agent 行为 PR 应直接拒绝，借此把“自然共享”从口号变成仓库规则。

仓库级 `scripts/check-agent-architecture.mjs` 已落地（而不是依赖人工 review），并已接入 `.github/workflows/main.yml` 的 `verify-agent-architecture` 必过 job；发布 job 通过 `needs` 强制依赖该守门。该 job 执行 import/composition-root 静态检查，并运行 Runtime、Service、MaClawSrv contract tests；完成 `guiapp` 拆包后，可继续扩展并执行：

```text
go list -deps ./corelib/agentruntime ./corelib/agentservice ./guiapp ./MaClawSrv
go test ./corelib/agentruntime ./corelib/agentservice ./guiapp ./MaClawSrv
go test -race ./corelib/agentruntime ./corelib/agentservice
```

脚本检查 import 方向、Runtime module 唯一注册点、transport 禁止调用点，以及 GUI/srv capability snapshot 的差异。任何一个检查失败都阻止合并。

### 4.5 明确不采用的方案

- **不采用 GUI 进程被 srv 远程调用**：这会引入桌面进程依赖、版本漂移和网络故障，且无法满足真正的无头部署；共享必须发生在同一 Go module 的编译期。
- **不采用复制 GUI 目录后再做 parity**：parity 测试只能发现差异，不能消除第二份实现；唯一 source of truth 必须由包依赖强制。
- **不把所有宿主能力都塞进 Runtime**：Runtime 只定义 port/contract，桌面捕获、打开文件、播放音频等副作用必须由 Host adapter 实现。
- **不把动态 Skill/MCP 当作编译期 builtin**：动态能力仍按租户授权和运行时 descriptor 注入，避免跨租户泄漏和重启后错误复用。

## 5. 分阶段落地计划

| 阶段 | 工作项 | 完成标准 |
|---|---|---|
| Phase 0（1 周） | 修复 nil server、统一 cleanup、补 request/run correlation；冻结 GUI/srv 非界面逻辑新增 | 初始化失败不 panic；`go test -race` 覆盖 shutdown；新增 Agent 功能只能提交到 `corelib` |
| Phase 1（2–3 周） | 落地 `corelib/agentruntime` facade；先让现有 `CoreAgentExecutor` 和 GUI 外壳都转发到 facade | 同一 fixture 下两端 prompt digest、tool surface、策略和错误码一致；旧实现只剩代理 |
| Phase 2（2 周） | 将 GUI 纯逻辑按 module 移入 Runtime，接入 `ToolModule/PromptContributor/PolicyModule` 注册表 | 任意一个 module 只存在一份源码；srv 自动获得 GUI 新增 module，headless 缺能力时有稳定降级 |
| Phase 3（2 周） | durable event outbox + SSE/NDJSON adapter；保留同步 API 兼容层 | token/tool/ask_user/终态事件可重放；断线续传不重复、不丢失 |
| Phase 4（1–2 周） | repository 原子幂等、统一 async Job、补 run cancellation/recovery | 并发重复请求只产生一个 run；重启后任务按 policy 收敛 |
| Phase 5（持续） | HTTP 域拆分、配置 schema/OpenAPI/SDK 生成、静态依赖 gate 和性能压测 | 新增 GUI Agent 能力只改 Runtime/Host contract；CI 阻止两端出现第二份实现（AppConfig schema 元数据与版本合同已先行落地） |

## 6. 验收与回归建议

新增一个跨宿主 contract test suite，至少覆盖：

1. 同一消息、历史和 `AppConfig` 生成相同 system prompt digest；
2. light/full prompt、steering、drift detection、adaptive retry 行为一致；
3. 工具调用顺序、参数拒绝、mutation scope、ask_user 状态一致；
4. token、tool、终态事件 sequence 单调，SSE 断线可续传；
5. 取消在 HTTP 断开、显式 cancel、进程 shutdown 三种情况下都能收敛；
6. 同一 `Idempotency-Key` 并发 20 次只产生一个 message/run；
7. 租户 A 无法读取租户 B 的 session、memory、knowledge、artifact 或 event；
8. 启动/关闭、SQLite 锁、memory flush、MCP 子进程回收通过 `-race` 和故障注入；
9. headless profile 对桌面专属工具返回稳定的 capability error，而不是“unknown tool”；
10. GUI 新增一个 Runtime tool 后，srv capability catalog、OpenAPI 和 SDK contract CI 自动发现变更。
11. 在临时分支中只新增一个 `corelib/agentruntime` module，不修改 GUI/srv 代码，GUI 与 srv 的 capability snapshot 均自动出现该 module；
12. 静态 gate 能检测并拒绝 transport 层新增工具名、prompt 规则、策略 switch，或 `gui`/`MaClawSrv` 绕过 Runtime 直接调用 LLM/executor；
13. 删除任一旧 GUI callback/工具实现后，所有目标（GUI、srv、TUI）仍能从同一 Runtime 构建通过，证明没有隐式复制依赖。

## 6.1 本轮实施状态（2026-09-01）

本轮已将设计中的低风险基础能力落到代码，并保持旧调用方兼容：

- `MaClawSrv.NewHTTPServerWithError` 显式返回构造错误；旧 `NewHTTPServer` 仅作为兼容包装器，避免初始化失败后继续启动。
- `MaClawSrv` 所有退出路径统一执行 `HTTPServer.Close`、知识库关闭和 `Service.Close`；HTTP/Service/Executor 的关闭操作均幂等。
- `CoreAgentExecutor.Close` 回收按用户缓存的 memory store、scheduler manager 和 SSH 会话，防止后台持久化 goroutine 在 srv 退出后泄漏。
- `Service.Close` 现在收集 Runtime、记录库、事件库和动态注册表的全部关闭错误并通过 `errors.Join` 返回，同时保持幂等。
- 对未实现 `RunEventStore` 的自定义控制面 Store，`Service.Close` 也会纳入统一清理栈；若 Store 同时拥有事件仓库则避免重复关闭，嵌入方不再泄漏数据库句柄。
- Coding runtime child registry 增加 `CancelAll`，关闭时先中断所有活动子任务，再回收宿主资源。
- 新增 `corelib/agentruntime` contract（Runtime、TurnRequest、HostCapabilities、EventSink、ModuleDescriptor）以及显式 headless capability 实现。
- 新增 `agentservice.RuntimeExecutor` 薄适配器；`Service.PostMessage` 通过该 facade 执行，后续 GUI 迁移只需替换 adapter，不再新增 srv 专用 Agent 逻辑。
- 新增 `agentruntime.TurnInput` 作为 GUI/headless 共同的 transport-neutral turn 输入；GUI 入口现在提交标准字段（system prompt、用户文本、历史、附件、平台和迭代预算），旧 handler 状态仅留在 adapter 的非序列化 `HostPayload`，外部 Runtime 可直接接管而无需依赖 `guiapp` 私有类型。
- `agentruntime.TurnCallbacks` 已提供无 GUI 类型的兼容回调面；GUI adapter 从中恢复 Wails/IM callback，headless Runtime 可忽略这些字段并完全依赖 EventSink。
- `NewIMMessageHandlerWithRuntime` 与 `StandaloneConfig.SharedAgentRuntime` 已将 Runtime 注入提升到 GUI/TUI composition root；宿主不必先创建 handler 再调用 setter，nil 仍兼容旧的惰性迁移 adapter。
- `RuntimeExecutor.DescribeCapabilities` 现在接受同一 `agentruntime.TurnInput` 并优先使用其中的有效 Prompt digest；GUI 与 srv 可针对同一轮输入比较 capability surface，而不再只能比较进程级空 Prompt。
- Runtime callback（token/tool）通过 `EventSink` 统一桥接到 Service-owned outbox；Service 不再在 transport 路径复制一套 callback 事件逻辑，未来 GUI 只需提供同一 sink/host adapter 即可复用事件语义。
- `RuntimeExecutor` 会记录并回传 `EventSink` 的首个持久化错误；事件 outbox 不可用时，执行不会被错误标记为成功，Service 可按统一失败/重试路径收敛 run 终态。
- 当底层 executor 与 EventSink 同时失败时，RuntimeExecutor 通过可判别的 joined error 同时保留两类原因；调用方可以确认模型失败与 outbox 不可用并存，避免盲目重试造成重复副作用。
- EventSink 写入 outbox 时统一使用 `maclaw.run-event/v1` envelope schema（与 Runtime contract version 解耦），避免 GUI 与 srv 因内部 Runtime 版本变化产生事件解析分叉。
- Runtime 事件 envelope 现在支持可选 `event_id` 与 `occurred_at`，并由 `agentruntime.NormalizeEventEnvelope` 在 GUI/headless sink 之前统一校验、UTC 归一化、换行防注入和嵌套 payload 深拷贝；Service sink 只透传受限的 producer id/时间，sequence 仍由 durable outbox 分配，避免 Runtime 与 `run.started` 争抢游标或伪造顺序。过长、换行或 run-scope 不一致的 id 在落库前拒绝；同一 producer id 在不同 run 可安全复用，重试仍按本 run scope 得到 canonical 事件。GUI shared loop 现在记录首个 EventSink 错误，并将其转换为 `runtime_event_sink_failed`，不再把 outbox 拒写的执行报告成成功。
- `agentruntime.FanoutEventSink` 允许同一事件同时进入 durable outbox 与即时 UI/SSE 投影；所有 sink 都会尝试执行，错误通过 `errors.Join` 保留，避免展示层正常时掩盖持久化失败。
- 事件类型常量已集中到 `agentruntime/event_types.go`，GUI 与 srv callback bridge 使用同一 `run.*`、`assistant.delta`、`tool.*` 词汇，减少 transport 层字符串漂移。
- Legacy tool catalog 的 capability 投影已集中到 `agentruntime.CapabilityToolsFromSpecs`；srv 不再自行维护排序、空名称过滤和 disabled reason 映射，GUI/OpenAI 与 srv catalog 可共享同一投影规则。
- GUI 新增 `GetAppConfigSchema` Wails 入口，直接返回 `corelib/config.AppConfigSchema` 与版本号；前端设置页可按同一 schema 渲染，避免继续扩展 transport-local 字段黑名单。
- GUI shared loop 已通过 `runtimeEventProjectionSink` 从统一事件 envelope 投影旧的 token callback；durable sink 与 UI 投影由同一 `FanoutEventSink` 组合，避免 UI 继续维护独立 token 事件路径。
- GUI shared loop 的 `run.started`、token/tool 事件和终态事件现在共享同一个单调 `sequence`，并生成稳定 `event_id`；生命周期事件不再与 callback 事件使用彼此独立的游标。
- `agentruntime.Scope` 增加可选 `run_id` correlation，Runtime 事件和宿主 outbox 可在不依赖 HTTP/Wails 的情况下稳定关联到同一轮执行。
- `client_session_key` 与 `client_message_id` 使用 Service 内 keyed mutex 做并发原子化，GUI、HTTP、IM 重试共享同一幂等语义，并有并发回归测试。
- HTTP `Idempotency-Key` 会被统一映射到 `client_message_id`，并拒绝与 body 中不一致的重复键。
- 新增 `RunEventStore`（内存/SQLite 实现），统一持久化 `run.started`、token、tool、`ask_user` 和终态事件；现有 SSE 在保持 snapshot 兼容的同时增量推送事件并携带 `id`。
- SQLite event append 在 sequence/event-id 冲突时返回数据库中已提交的原事件，避免重试响应被篡改 payload 污染；内存实现同步按 sequence 与 event-id 去重并保持最大 sequence 单调递增。
- SSE 支持 `Last-Event-ID`：事件 id 可解析为 per-run sequence，断线后只发送游标之后的事件；`ListRunEventsForInstance` 在读取 outbox 前校验 tenant/user、instance 与 run 的关系，避免跨实例探测。
- Run 状态、事件追加及事件游标读取统一纳入 `Service.activeRequests`；关闭流程会先阻止新读写、等待在途事件/状态请求完成，再关闭 event store，避免异步执行与 SSE 读取访问已关闭存储。
- OpenAPI 与 `API_MANUAL.md` 已声明 `Last-Event-ID` 请求头及增量事件 envelope，GUI/SDK 可按同一 contract 实现重连而无需理解 srv 内部轮询细节。
- 新增 `GET /api/v1/instances/{instanceId}/runtime-capabilities`，直接返回 `agentruntime.CapabilitySnapshot`；GUI、srv 及后续 TUI 可使用同一 module/host contract，而无需从旧 `AgentCapabilities` DTO 反推行为。
- `POST /api/v1/instances/{instanceId}/sessions/{sessionId}/messages` 与一步式 `POST /api/v1/instances/{instanceId}/messages` 均支持 `Prefer: respond-async`：Service 在 run 持久化后立即返回 `202`，后台执行继续纳入 active-request、取消和关闭生命周期；异步重试仍复用同一 `client_message_id` 幂等 run。
- 新增可选 `RunAdmissionStore` / `RunAdmissionEventStore` 扩展，Service 通过单一 `saveRunAdmissionWithEvents` 边界提交 user message、初始 run 与 `run.started`；未迁移的 Store 仍可只实现旧接口。
- 新增可选 `RunCompletionStore` / `RunCompletionEventStore` 扩展，终态 assistant message、run 与 `ask_user`/`assistant.message`/`run.completed` 通过同一边界提交；失败/取消则使用 `RunTerminalEventStore`。
- 新增 `RunLifecycleTransactionStore` 统一事务扩展：自定义 Store 只需实现一个 `CommitRunLifecycle`，并将 `RunEventStore` 嵌入同一 repository，即可让 Service 对 admission/completion/terminal 三个阶段共用单一数据库事务契约；Service 优先使用该扩展，GUI、srv、TUI 不再感知具体 Store 类型。
- `MemoryStore`、`FileStore`、`SQLiteStore` 现在同时实现 `RunEventStore`、`RunLifecycleTransactionStore` 与 admission/completion/terminal event transaction 扩展；默认 Service composition 会优先把同一 repository 作为 run outbox，确保 `run.started`、assistant message + `run.completed`（以及失败/取消终态）与 run/message 在同一锁/原子文件替换或 SQLite 事务中提交。未迁移的自定义 Store 仍通过 SQLite/内存事件 store 兼容运行。
- 对尚未迁移的自定义 Store，Service 的兼容分支在第二次生命周期写入失败时会尽力删除刚写入的孤立 message，并继续以 `best_effort` 能力级别对外声明；这降低了半提交残留，但不冒充数据库事务。迁移顺序建议先实现 `RunLifecycleTransactionStore`，再把 outbox 查询切换到同一数据库，最后移除兼容回退。
- `DescribeLifecyclePersistence` 会根据 Store 扩展接口计算 `atomic`/`mixed`/`best_effort` 保证级别；Runtime capability snapshot 的 metadata 同时公布 `lifecycle_persistence_mode`、`durable_outbox` 以及 admission/completion/terminal 的事务能力。GUI、srv、TUI 可以据此决定是否安全重试或依赖断线重放，而不需要猜测后端类型。
- `agentservice.ErrorCode` 为共享 Service 错误提供稳定 snake_case code；MaClawSrv 的统一 `writeRedactedError` 响应现在同时返回 `code` 与脱敏 `error` 文本。跨入口客户端可按 code 分支，不再依赖 srv/GUI 的语言或文案差异。
- `agentruntime.Job`/`JobStatus` 统一异步任务 envelope 与 pending/running/succeeded/failed/canceled/unknown 词汇；MaClawSrv 的兼容任务管理器直接复用该结构，并在任务结果中返回 `error_code`（取消、重启、业务错误和结果序列化失败均可区分），为 GUI/TUI 迁移到同一 Job contract 保留稳定边界。
- `NormalizeJobStatus` 对重启加载的未知/历史状态 fail-closed 为 `unknown`，避免客户端把未来状态误当作可重试或成功；后续恢复策略可在此边界上按显式 policy 扩展。
- `agentruntime.UserFacingText` 统一清理 Agent working-state 标记；GUI 保留的 `sharedLoopUserFacingText` 仅是兼容包装，srv/TUI 可直接复用相同的可见文本投影。
- Service assistant message 持久化现在统一经过 `agentruntime.UserFacingText`，因此 HTTP/SSE、GUI 兼容层和其他宿主不会把 loop 内部 working-state 泄露给用户。
- 参数 schema 拒绝后的模型恢复指引已下沉到 `agentruntime.ParameterRejectionGuidance`；GUI 仅保留包装，srv/TUI 在复用同一工具面时可得到相同的拒绝与重试语义。
- 新增 `SQLiteStore` 控制面 repository：以版本化 SQLite state row 作为单一提交边界，完整实现 `Store`、`RunEventStore` 与 admission/completion/terminal event transaction 扩展；支持跨进程读刷新、WAL/busy timeout、重启恢复和 repository 级幂等约束。`Config.StoreBackend="sqlite"` 可显式启用，MaClawSrv 默认使用该后端（可通过 `MACLAW_STORE_BACKEND=file` 临时回退）；`file/json` 仍保留兼容回退。
- SQLite 首次启用时会从同目录的旧 `store.json` 导入控制面状态，并通过 `MemoryStore` 的凭据归一化去除历史明文 `api_key`；旧文件保留为回滚备份，不在启动迁移中删除。
- GUI 的设备闹钟 prompt 规则已迁移到 `corelib/agentruntime.BuildClientToolPrompt`；`gui/im_system_prompt.go` 仅保留类型转换和 LoopContext 适配，srv/未来宿主可直接复用同一文案与门控测试。
- Assistant/Bot 绑定的工作目录、文档目录和管理员约束投影已迁移到 `corelib/agentruntime.BuildAssistantBindingPrompt`；GUI 入口仅保留兼容代理，headless/TUI 可复用同一授权范围提示，避免不同宿主对绑定身份和路径边界产生漂移。
- 轻量 turn 的身份、低复杂度动作边界、执行 profile、客户端能力和绑定上下文已集中在 `agentruntime.BuildLightPrompt`；GUI 只负责专家身份解析和最后的语义 grant fence，srv/headless 可直接复用同一 light prompt contract。
- 工作流文档交付的桌面 Markdown 预览规则与 IM PDF 规则已分别下沉到 `agentruntime.DesktopWorkflowDocumentDeliveryPrompt` / `IMWorkflowDocumentDeliveryPrompt`；GUI 仅按平台选择模块，未来 srv/TUI 可采用同一版本化 policy，而无需复制中文规则或在 handler 中维护分叉。
- Light turn 不再复用完整 semantic petition 长围栏：`agentruntime.EnsureLightSemanticGrantPromptFence` 提供有界、幂等的工具列表状态契约，GUI/headless 共用且受 3.2KB prompt 回归门禁保护；完整 turn 仍使用详细 petition/replan 规则。
- GUI shared-loop 的 reasoning 展示回退已迁移到 `corelib/agentruntime.DisplayReasoning`，GUI 仅保留兼容包装；这类纯结果投影不再在 `package main` 中形成第二份实现。
- shared-loop 历史 semantic grant 的安全投影已迁移到 `corelib/agentruntime.RewriteExpiredSemanticGrantNames`（含 `PreviousTurnSemanticToolName` 合同）；GUI 只保留兼容包装，headless/TUI 可复用同一 stale-token 防护和深拷贝语义。
- Agent Runtime 附件批次 admission（8 个文件、25 MiB 聚合软上限及 base64 估算）已迁移到 `corelib/agentruntime.AttachmentsWithinRuntimeLimit`；GUI 仅保留适配函数，srv/headless 及其他宿主可直接复用同一边界而不复制 magic numbers。
- 全局 thinking mode 的 alias 解析（`enable/on/true/1`、`disable/off/none/0`、`auto` 及非法值回退）已统一到 `corelib.ParseGlobalThinkingMode`；GUI、MaClawSrv 配置 mutation、LLM resolver 和 MoA resolver 不再各自解释同一配置，srv 与 GUI 对 unset/非法值统一采用 `enabled` 默认值。
- `agentruntime.RegisterBuiltinModules` 作为统一 composition-root 入口落地（`NewModuleRegistry` 保留为兼容构造器）；Service 只从这一入口装配 Runtime module，避免 GUI/srv 手工维护第二份 builtin 清单。
- Service 的 admission/completion/failed/cancelled 路径已统一生成版本化终态事件，并只在 repository 未接管事务时回退到独立 `emitRunEvent`，避免事务路径重复事件；事件 payload 克隆 bug 也已修复，重放不会丢字段或被调用方 map 修改污染。
- Runtime event sink 现在对 producer 提供的 tenant/user/instance/session/run scope 做 fail-closed 校验；可选字段为空时由 sink 补齐，任何跨认证运行边界的显式值都会在落库前拒绝。
- Memory/FileStore 与独立 RunEventStore 现在共享事件 envelope 归一化：自动补齐 schema/id/time、校验 payload 可序列化并深拷贝嵌套 JSON 容器；FileStore 生命周期事务在内存变更或原子替换失败时回滚快照，避免出现“调用失败但进程内已可见”的半提交状态。
- `NewService` 的初始化失败路径现在会清理已创建的内部 Store、records、operations、capabilities 和 event store；调用方传入的 Store 仍由调用方拥有，SQLite 句柄释放由回归测试验证，避免构造失败后遗留锁或后台资源。
- 独立 Memory/SQLite RunEventStore 的 `Close` 现为真正的 ingress gate：关闭幂等，关闭后的 append/read 统一返回 `ErrServiceClosed`，并通过互斥保护关闭与并发写入的时序。
- Memory/FileStore 对 `client_session_key` 与 user message 的 `client_message_id` 增加 repository 级唯一性校验；Service 在唯一约束赢得跨进程竞争时重新读取 canonical session/run，避免把正常幂等重试误报为冲突。
- 终态事件使用 `run:<run_id>:<event_type>` 稳定 event id；显式取消与执行器感知取消会收敛为同一 `run.cancelled` 记录，不再因两个生命周期入口重复推送终态事件。
- Runtime capability 查询与执行均将请求级 `HostCapabilities` 注入底层 executor，避免 GUI/headless profile 只停留在传输层而未影响实际工具可用性判断。
- MaClawSrv 的 headless profile 现在显式公布服务端 `bash`、`ssh` 及文件传输能力，模块可据此进行统一 capability gate，而不会把宿主能力误判为空。
- RuntimeExecutor 对未注入的宿主能力统一归一为显式 headless profile，并清理调用方携带的 Runtime prompt/tool/invoker 字段，确保这些字段只能由 composition root 的注册表生成。
- `Service.Config.RuntimeModules` 与 `NewRuntimeExecutorWithModules` 提供唯一 composition-root module 注册点；共享 module descriptor 会自动出现在 runtime capability snapshot，重复 legacy descriptor 由新 module 覆盖，即使测试/嵌入方清空 runtime facade 也不会丢失注册表。
- `ModuleRegistry` 已提供确定性的 prompt 聚合、tool surface 聚合及 policy gate；RuntimeExecutor 在调用旧 Executor 前先执行共享 policy modules，并拒绝重复 tool 名称，避免 GUI/srv 各自编排顺序。
- `ModuleRegistry` 会按当前 `HostCapabilities` 过滤不支持宿主的 prompt、工具和 policy module；`SnapshotForHost` 与执行面使用同一判定，避免 GUI 专属 module 在 srv 中误启用或产生漂移。
- 对已经存在于注册表但缺少宿主能力的工具，`InvokeTool` 现在先识别工具再返回稳定的 `capability_unavailable`（含缺失能力名）；因此旧 GUI surface 或跨主机重试不会被误报成 `unknown tool`，而正常模型面仍按宿主过滤。
- RuntimeExecutor 现在会把 module 的 prompt/tool contribution 注入 `ExecuteRequest`；CoreAgentExecutor 将共享 prompt 合并进 system prompt，并仅暴露实现了 `ToolHandler` 的模块工具，调用经同一 registry 路由，避免“模型看见工具但执行端 unknown tool”。仅用于能力发现的 descriptor 仍可注册，但不会进入模型工具面。
- `CapabilitySnapshot` 同步返回确定性 `tools` 列表及 `prompt_digest`（不泄露 prompt 原文），GUI 与 srv 可在 contract test/启动诊断中直接比较共享 Agent surface。
- `CapabilitySnapshot.surface_digest` 现由 Runtime 对 profile、module、tool schema/可用性和 `prompt_digest` 做规范化 SHA-256；忽略 transport metadata（路径、租户和持久化状态），并对集合排序后计算，避免 GUI/srv 因输出顺序或部署细节产生假漂移。宿主可在发送任务前直接比较该 digest，发现非界面 surface 不一致时 fail-closed 并进入诊断。
- `corelib/config/app_config_schema.go` 现提供唯一的 AppConfig 字段元数据（scope、secret、headless、user-web visibility、complex-field）及通用 `CopyAppConfigFields` 投影 helper；`agentservice` 参数定义与 MaClawSrv 用户配置投影均消费该 schema，新增字段不再要求两端分别维护 allow/deny 列表或 reflection 赋值实现。旧变量仅保留为兼容测试快照的只读 map 副本。
- GUI 用户数据迁移和设置页 DTO 的 AppConfig JSON tag 解析也统一调用 `corelib/config.JSONFieldName`；架构守门会拒绝宿主/服务重新引入本地 tag parser，避免新增字段在迁移、设置页和 srv schema 之间出现命名漂移。
- `/api/v1/config/schema` 与 `/api/v1/admin/client-config/schema` 返回 `schema_version=maclaw.app-config/v1` 及上述元数据，客户端可据版本做缓存和 parity 校验。
- Snapshot metadata 只允许策略/能力布尔值；workspace、data-root、tenant/user 标识等敏感路径不会从新 endpoint 输出，旧 endpoint 继续使用原有 API 脱敏层。
- 旧 `/capabilities` 已改为 `LegacyCapabilitiesFromRuntime` 兼容映射，避免 legacy DTO 与 Runtime snapshot 各自演进；新字段应优先加入 Runtime contract，再由兼容层映射。
- 新增 `scripts/check-agent-architecture.mjs`（`npm run check:agent-architecture`），校验 Runtime import 方向、composition-root 唯一注册点，以及 GUI/srv 不得自行构造 Runtime/module registry；该守门已接入发布 CI 的 `verify-agent-architecture` job，并与 `go test ./corelib/agentruntime ./corelib/agentservice ./MaClawSrv` 同步执行。
- 回归测试覆盖 SQLite replay canonical result、并发 sequence 分配、跨实例拒绝、Runtime callback 事件桥接、SSE 断线恢复和 OpenAPI header contract；核心用例已通过 `-race`。
- 异步 Job admission 现在采用 fail-closed：生产持久层为 `state/jobs.db`，只有 SQLite 事务成功后才启动 worker；读写、初始化或旧快照迁移失败都会将 job repository 标记为不健康。写入失败时内存诊断快照终态为 `failed` 且 `error_code=job_persistence_failed`；运行/完成阶段的持久化失败也不会继续报告 `succeeded`。`/readyz`、Admin readiness 和 Prometheus 均暴露 repository 健康状态。
- `agentruntime.JobRepository` 已成为 transport-neutral 的多写者异步任务边界：`Admit` 在数据库唯一索引内原子返回 canonical Job，`Update`/`Delete` 使用 `Job.version` CAS，避免旧进程以全量快照覆盖新状态。旧 `JobStore`/文件 adapter 只保留为单写者兼容层，不再用于 MaClawSrv 生产 composition。
- `agentruntime.JobReporter`、`JobUpdate` 与 `JobCheckpoint` 已成为共享 worker 进度合同。宿主通过 `WithJobReporter` 把 reporter 注入任务 context，领域 worker 只调用 `ReportJobUpdate`，不再捕获 MaClawSrv manager 或 GUI 状态；progress 与 checkpoint 在一次 repository CAS 更新中提交，失败时同时回滚。用户数据 migration 已是首个真实消费者，后续可把同一 worker 原样装配到 GUI/TUI 宿主。
- checkpoint 的共享 envelope 只允许单调递增的 `sequence`、安全的 `phase` 和宿主生成的 `updated_at`。领域 replay payload、凭据、文件路径及外部资源标识必须留在各自受权限保护的 repository 中；checkpoint 仅用于进度审计和 reconcile 定位，本身不构成自动重放授权。持久化 checkpoint 非法时 JobStore 加载 fail-closed，并使 readiness 失败。
- `agentruntime.JobRetryPolicy` 已统一当前进程内的有界重试语义：默认 `max_attempts=1`，只有领域显式用 `MarkJobErrorRetryable` 标记的错误才允许按共享指数退避再次执行；`attempt`、`next_attempt_at` 和脱敏后的上次失败进入同一 durable Job envelope。退避等待可被 cancel/shutdown 立即中断，重试状态持久化失败则 job fail-closed，不会在无 durable 记录时继续执行。
- 自动重试不等于重启 replay。即使 `max_attempts>1`，进程重启后的 `pending/running` 仍严格按 `JobRecoveryPolicy` 进入 `unknown/reconcile_required` 或 `failed/service_restarted`，不会根据 retry policy 恢复闭包。MCP health-check 是首个生产消费者：只有实际 probe 失败被标记为 retryable，资源不存在、权限或配置错误不会重试；该任务同时使用 side-effect-free 的 `recovery_policy=fail`。
- `JobIdempotencyIdentity` 已定义共享 admission 身份：原始 `Idempotency-Key` 和请求 payload 只在入口内存中出现，durable Job 与 worker context 仅得到按 tenant/user/kind 作用域生成的 SHA-256 digest 和 request digest。MaClawSrv manager 在同一互斥区内完成“查 canonical job → 校验 request digest → durable admission”，并发同键只启动一个 worker；同键异参返回稳定 conflict，服务重启后只要 canonical job 尚在 retention 内仍返回同一 job，admission 响应用 `idempotent_replay=true` 标识。若初始 envelope 未能持久化，worker 不启动且 key 立即释放，存储恢复后同键可安全重新 admission。
- SQLite JobRepository 已在非空 `idempotency_digest` 上建立 partial unique index；两个独立 repository handle 并发 admission 时只有一个 winner 启动 worker，其余返回相同 canonical Job，同键异参返回稳定 conflict。每次状态更新和批量删除都校验版本，批量删除在一个事务中全成或全败。HTTP 层通过统一 `admitUserJob` adapter 消费该合同，MCP async create/update/start/stop/health-check 与 migration export/import 已接入；密码和 MCP secret 只参与内存 request digest 计算，不进入 Job envelope。原始 key 不会被 trim，前后空白会按共享 identity 规则拒绝。首次启用 `jobs.db` 会一次性导入并校验旧 `jobs.json`，旧文件保留为备份且不会重复导入。
- JobRepository schema v2 保存 transport-private 的 worker owner/lease，活跃 manager 以有界 heartbeat 续租；其他 srv 会保留未过期的 foreign worker，不再在启动时误报 `unknown`。lease 过期或旧格式无 lease 的 `pending/running` 才进入生命周期恢复：可能有外部副作用的任务按默认 `reconcile` 转为 `unknown/reconcile_required`，显式 `fail` 才转为 `failed/service_restarted`。API 读取会自动对账已过期记录，旧 worker 的迟到结果因 version CAS 不能复活已恢复任务；lease 元数据不进入 HTTP JSON。
- `agentruntime.JobEffect`、`JobEffectRepository`、context-injected `JobEffectRecorder` 与 `JobReconciler` 已形成共享的受保护 effect/replay 边界；SQLite 实现位于 `corelib/agentservice` 的 `state/job_effects.db`，不是 srv 私有 JSON，也不进入通用 Job API。manager 只允许当前 owner 的活跃 worker prepare/bind/settle effect；未知 Job 的领域 reconciler 只得到 immutable Job 与受保护 effect 记录，返回值只能是 unresolved/succeeded/failed，不能产生 pending/running 或取得原 worker 闭包。
- migration export/import 是首批真实消费者：provider resource id 与领域 payload 保存在 effect repository，raw receipt 只落 SHA-256 摘要；Hub 新增 principal-scoped 的只读 export receipt probe。worker lease 过期后，GET Job 会依据本地 committed receipt 或 Hub 状态把 migration `unknown` 以 Job version CAS 收敛为真实终态；`importing` 等证据不足状态继续 unknown，且整个路径不调用 export create、upload、claim、complete 或 local restore。effect 持久化在外部 I/O 后失效会显式产生 `unknown/reconcile_required`，不会伪报普通失败或触发 retry。
- MCP mutation、Skill install/import/upload、Knowledge file/URL/directory/package/share import 已接入同一 `JobEffectRecorder`：worker 在调用服务前 prepare，成功后 bind 资源 ID 并提交脱敏结果 receipt；配置/进程/下载/文件系统等副作用后的持久化失败统一进入 `unknown/reconcile_required`，不会被通用 Job retry 重放。GET Job 为上述 kind 注册只读 domain reconciler：优先回放受保护 committed payload，或通过 MCP server、已安装 Skill、Knowledge source 的当前状态证明终态；reconcile 绝不调用原 worker 闭包，也不把 effect payload/resource ID 放入 Job/API envelope。
- Admin public knowledge URL/file imports 现在也通过统一 `admitUserJob`，支持 `Idempotency-Key`、request digest、重放标记和 multipart staging 清理；文本导入在携带 `Idempotency-Key` 时同样进入 durable Job + protected effect，未携带时保留旧同步响应，避免破坏既有 Admin API。管理员 Job 查询复用同一只读 Knowledge/MCP/Skill reconciler，避免 Admin Web 与用户 API 展示不同的 unknown 终态。公共导入的审计事件仅在首次 admission 写入，重试不会制造重复审计记录。
- HTTP host 现在生成并回传安全的 `X-Request-ID`，并解析 W3C `traceparent` 的 trace-id/父 span-id；`agentruntime.CorrelationID/TraceID/SpanID` 贯穿异步 PostMessage 的 Run metadata、run lifecycle/runtime event payload 与 run audit；SSE 复用同一 Run snapshot/event，因此 GUI、srv、TUI 可按 request/run/trace correlation 对齐诊断。共享 `agentruntime.RuntimeMetrics` 已补齐首 token、队列等待、租户哈希、tool/outbox 和 quota 基线；共享 Service admission 现在可选启用有界租户 token-bucket，429 携带标准重试提示并独立计入 rate-limit 指标。
- Run 现在在保留入站 `span_id` 兼容语义的同时，基于 `trace_id + parent_span_id + run_id` 派生稳定的 `run_span_id`；该 child span 会随生命周期、Runtime token/tool 事件和 run audit 一起持久化，跨进程重放仍可建立父子链路，而无需在宿主内引入不可靠的随机 exporter。
- 新增共享 `agentruntime.RuntimeMetrics`：GUI、MaClawSrv、TUI 可复用同一套 run admission/终态、active run、队列等待、首 token、token/tool、outbox、quota 计数；租户维度仅输出有界的 SHA-256 前缀，避免 cardinality 爆炸和标识泄漏。MaClawSrv `/metrics` 仅负责 Prometheus 文本投影，不再在 transport 层自行推导 Agent 指标。
- Job 显式删除与 retention/count housekeeping 在 canonical Job 删除成功后清理对应 protected effect receipt；Job 与 effect 位于独立 SQLite 文件时无法做跨库事务，清理失败会写入仅包含 Job ID 的 durable cleanup marker，保留在 readiness 诊断并由后续清理轮次（包括进程重启后）重试，不会把已提交的 Job 删除伪报为失败。
- `HTTPServer.Close` 现在先关闭 Job admission 并取消所有 `pending/running` worker，再回收 IM/模型/coding runtime；即使某个 worker 忽略取消并迟到返回成功，也不能把 shutdown 已收敛的 `canceled` 状态重新写成 `succeeded`。
- coding runtime SQLite ledger 只在 Service executor 明确实现共享 coding-runtime port 时打开；普通控制面/测试 executor 不再无条件创建并长期持有无消费者的数据库连接，宿主资源所有权与 capability 声明保持一致。
- GUI shared-loop 的 semantic selection failure、unknown outcome 和 grant rejection 文案已迁移到 `corelib/agentruntime/semantic_outcomes.go`，GUI 仅保留兼容包装；srv/TUI 可复用同一重试/对账判定，不再复制系统标记字符串。
- semantic petition 成功后的“重新发起调用”指引已进一步收敛到 `agentruntime.PetitionGrantedMessage`；GUI 只保留兼容包装，headless/TUI 的动态工具扩展可复用同一文案和空名称保护。
- semantic surface 推进失败时保留已提交结果的保护文案已下沉到 `agentruntime.AdvanceAfterSuccess`；宿主只记录诊断错误，不再把成功的外部副作用转换为可重试拒绝。
- 文档读取结果中的 legacy continuation 提示，以及旧客户端空 delivery 参数 envelope，已分别迁移到 `agentruntime.DocumentReadResultProjection` / `DeliveryInvocationArgs`；GUI 仅做兼容代理，headless/TUI 可复用相同的路径泄漏和参数洗净边界。
- Tool-call 的纯归一化契约已迁移到 `corelib/agentruntime/tool_calls.go`：空参数/代码围栏清理、JSON object 解析、`browser_*` 合并入口别名、稳定浏览器禁用动作，以及 `Glob`/`search_files` 等本地搜索别名均由共享 Runtime 提供。GUI 的 IM loop、GUI 本地工具和 `agentservice` headless executor 现在调用同一套函数；宿主仍只负责策略、能力和副作用，因此新增兼容别名不会再出现 GUI/srv 漂移。
- `database` 工具的 schema、凭据拒绝、连接/查询/执行结果投影已下沉到 `corelib/database`；GUI 与 srv 复用 `ToolDescription`、`ToolParameters` 和 `HandleTool`，srv 为每个 tenant/user/instance/session 保持受控连接管理器并在 executor 关闭时统一释放，不再因 GUI 新增数据库能力而出现 srv catalog 缺项。
- 数据库写操作的审批边界已进一步收敛：模型 schema 不再暴露 `approval_token`，模型参数只要出现该字段即拒绝；GUI task-panel 生成一次性内存 opaque token，批准后通过 request context 注入 `corelib/database`，审计只保留非敏感 `approval_id`。`ExecuteRequest.DatabaseApproval` 仅为 host-only bridge，禁止 JSON/持久化投影；PolicyEngine 对 token 与 SQL/schema 绑定及 pending mutation 持久化仍是发布前门槛。
- 数据库 GUI adapter 已切换为 `HandlerCtx`，并通过共享 `database.RequestScope` 覆盖模型伪造的 owner/session 字段；连接绑定与审计因此遵循同一可信请求范围，srv/TUI 可复用相同 context contract。
- `generate_pdf` 的参数洗净与“标题-only” admission 已迁移到 `corelib/agentruntime/semantic_pdf.go`。GUI 仅保留兼容代理；日期折叠、装饰字段剥离和过薄报告拒绝由 GUI、srv、TUI 共用，避免小模型在不同宿主消耗不同的一次性 grant。
- PDF 结果的 deferred/failure 文案清理已迁移到 `corelib/agentruntime/semantic_pdf_text.go`；artifact 真正发布后，各宿主统一移除“请稍候/无法授权/重新生成”等误导性文本，避免 GUI 显示成功而 srv 仍返回旧承诺。
- artifact 类型识别、PDF 成功标记和“附件已生成但错误仍需保留”的终态投影已迁移到 `corelib/agentruntime/artifact_outcomes.go`；GUI/srv/TUI 不再各自判断 MIME、文件后缀或可清除错误。
- PDF 标题提取、正文长度门槛、计划 capability 查找、receipt effect 分类以及稳定字符串集合投影已进一步下沉到 `corelib/agentruntime`/`corelib/tool`；GUI 与 headless dynamic executor 只保留宿主差异参数，不能再各自维护 effect 循环或 PDF 文案规则。
- RunEventStore 的 producer `event_id` 幂等范围已与 contract 对齐为 `(tenant_id,user_id,run_id,event_id)`；SQLite 首次打开会把旧的全局主键表在线重建为 scoped unique 约束，保留既有事件并允许不同 run 复用确定性 callback id。SSE 的游标仍按 run sequence 解析，跨租户/跨 run 不会互相命中。

2026-09-02 补充（第二轮）：继续按同一原则下沉 GUI 纯逻辑，并消除一处已确认的平行重复：

- 文档格式/MIME 分类与临时后缀判定的两份逐行平行实现（GUI 的 `semanticDocumentFormat`/`semanticDocumentTempSuffix` 与 headless 的 `reviewedHostDocumentFormat`/`reviewedHostDocumentTempSuffix`）已收敛为 `corelib/agent.DocumentAttachmentFormat` / `DocumentAttachmentTempSuffix` 唯一实现；GUI 的 `semantic_tool_routing.go` 与 `agentservice/dynamic_host_docread.go` 均改为调用共享实现，format 词汇统一为 `agent.DocumentAttachmentFormat*` 常量。
- prompt token 预算截断 `agentruntime.TruncateToTokenBudget`（`prompt_budget.go`）已下沉；段落>句读>换行>硬切的边界顺序语义随迁，GUI 的 skill 文档注入直接调用共享实现。
- MCP 调用信封归一化族（`NormalizeMCPToolCallArgsForAgentLoop`、routing 字段提升、信封键识别、嵌套 arguments 合并、required args 提取）已并入 `corelib/agentruntime/tool_calls.go`；"非空嵌套值优先"的顺序语义原样保留，GUI 仅保留薄包装，srv/headless 暴露 `call_mcp_tool` 时可复用同一套 server_id/tool_name 提升与合并规则。
- 定时任务准入闸门 `agentruntime.ManageScheduleCreateBlockReason`（`tool_admission.go`）已下沉；"用户须明确提出定时意图"的 cue 词列表冻结为共享词汇，GUI 仅保留包装，srv 的 `manage_schedule` 路径可接入同一准入判定。
- host-owned PDF 报告正文合成 `agentruntime.HostOwnedPDFReportContent`（`semantic_pdf.go`）已下沉：可信证据优先、deferred 承诺与 XML tool call 剥离、共享 `swarm.ValidatePDFContent` 校验，GUI 仅保留薄包装。
- skill 文档匹配引擎（`SkillDocMatchScore`/`SkillDocMatchAliases`/`SkillDocPhraseOccurs`/`SkillDocMatchLooksLikePathOperand`/`CountTriggerMatches`，`skill_doc_match.go`）已下沉；分隔符表（含全角连字符）、ASCII 词边界、路径操作数排除与 CJK 复合文件名防护原样随迁，行为测试同步迁至 corelib，GUI 仅保留薄包装，srv headless 注入 skill 文档时可获得逐字一致的命中语义。
- 注册工具的文本结果三态分类（`RegisteredToolTextOutcome`，`registered_tool_outcome.go`）已下沉：空输出为 uncertain，MCP/PDF/handler 失败 marker 词表冻结为共享词汇，GUI 的 `inferRegisteredToolOutcome` 仅保留枚举映射包装，srv/TUI 可直接复用同一三态判定。与 `ToolTextFailure`（host 适配器布尔探针）的边界保持不变；两侧词表的进一步合并是独立的语义变更，留待专项评估。
- IM 渠道平台词汇与发布策略族已下沉到 `corelib/agentruntime/channel_delivery.go`：`IMMessagePlatformKind` 全部 16 个平台常量、`NormalizeIMMessagePlatformKind` 及 String/IsDesktop/IsIMChannel/ChannelScope/语音格式偏好等方法随类型一起迁移，GUI 通过类型别名零成本兼容；`SemanticFileDeliveryPublished`/`SemanticTrustedDispatchDestination`/`SemanticScheduleDispatchPublished`/`SemanticVoiceDeliveryPublished`/`SemanticAudioSynthesizeLocalPublished`/`SemanticImageDeliveryPublished` 六个渠道治理判定收敛为唯一实现，srv 的 lansenger/weixin 等渠道投递可复用同一规则，GUI 仅保留委托。
- `scripts/check-agent-architecture.mjs` 已为上述迁移新增 requireText 断言，禁止任一侧回退到私有实现。
- 结构化日志边界已落地为 `corelib/logx`：slog JSON/Text handler + 强制脱敏（sensitive key 掩码、Bearer/key=value 凭据、URL userinfo、绝对路径归一为 `[redacted]/basename`，不允许宿主关闭），`WithCorrelation` 把 request/trace/span/run-span ID 注入每条记录。MaClawSrv 启动/关闭/知识库初始化与每个 HTTP 请求的完成日志（method/path/status/elapsed + correlation，5xx 提升为 Warn）已接入，`MACLAW_LOG_FORMAT`/`MACLAW_LOG_LEVEL` 控制格式与级别；stdlib `log.Printf` 仍作为 fallback 保留，GUI/TUI 可直接复用同一 logx 边界。
- tool outcome 词表已完成深度合并：`ToolTextFailure` 吸收了注册工具的 MCP 信封/参数解析/panic 与 PDF 生成器 marker 词表（修复 headless 对 `[mcp error]`、`mcp 调用失败` 等文本的漏判），`RegisteredToolTextOutcome` 不再维护独立词表，直接作为统一词表上的三态投影；`tool_outcome_unified_test.go` 锁定"非空文本下两个 API 的失败判定恒等"。
- 跨服务 span 链接已补齐：`agentruntime.TraceParentHeader` 在 trace+span 齐备时渲染 W3C `traceparent`，srv→hub 的 migration/knowledge-share 已注入，HubCenter Router 现在通过 `withInboundTraceParent` 解析并注入 `agentruntime` trace/span context。
- 2026-09-02 review/fix 轮（六区并行审查后修复）：修复 AdaptiveRetry 键归一化不一致（`RecordFailure` 归一化键而 `Decide`/`IsDisabled` 用原始名，含大写或 `browser_` 前缀的工具名永远无法触发禁用闸门——HEAD 存量 bug，已在共享实现中修复并加回归测试）；`ClassifyAdaptiveRetryFailure` 现在尊重共享 LLM 分类器的 Network 判定（`connection reset` 等不再退化为 unknown）；`MCPSchemaArgumentNames` 属性名排序消除 map 遍历随机序；`TruncateToTokenBudget` 极小预算统一返回稳定 `truncNotice` 形态；`ToolTextFailure` 注释订正首行/全文匹配的适用边界；logx 脱敏加固：slog Group 递归、`slog.Any` 承载的 error/Stringer 字符串形式脱敏、LogValuer Resolve、多 URL userinfo 单趟 builder 替换（修复坐标错位漏脱敏）、fragment 与 IPv6 host 保留、scheme 大小写不敏感、引号包围值与裸 token/cookie 键补齐、敏感键下数值型统计豁免、`RedactString` 廉价预检快速路径；`statusRecorder` 增加 wroteHeader 防重与 `ReadFrom` 零拷贝透传，请求日志在 Debug 关闭时跳过 entry 构建；`CancelRun` 删除永不触发的 terminalErr 分支；knowledge share 的 traceparent 注入限定为与 share API 同源（防止 correlation ID 泄漏到第三方 package_url 主机）；`Makefile` 的 `build-gui` 改指 `./cmd/maclaw-gui/`（改名残留断链，高严重度），仓库根 147MB 旧 `gui` 二进制已删除、`.gitignore` 补 `/gui` 规则；`usage_to_memory.go` 的两个标签归一化函数恢复为共享委托（曾被并行工作回退为复制实现）。
- AdaptiveRetry 子系统已整体下沉到 `corelib/agentruntime/adaptive_retry.go`：失败分类（FailureCategory）、重试决策（RetryAction/RetryDecision）、记忆持久化与 review 门槛逻辑全部迁移；宿主轨迹记录器通过窄端口 `TrajectoryRecordSink` 注入（GUI 的 TrajectoryRecorder 隐式满足），符合 §4.3 依赖倒置原则。级联的共享词汇一并冻结：`experience_vocabulary.go`（review 状态/outcome/lifecycle tag 词汇、trace source/kind、`HasTag`、`SafeFilenameRe`、usage/browser 标签归一化），`llm_retry_error_kind.go`（LLM 瞬断/周期限流/网络/上下文窗口分类全量词汇）。GUI 侧只保留类型别名与薄包装（`adaptive_retry.go`、`llm_retry_error_kind.go`、`experience_review_*`、`experience_trace_kind.go` 等），访问私有字段的宿主测试改经 `SetMaxFailuresForTesting`/`SetSkipTransientRetriesForTesting` 钩子，包内测试随迁 corelib。srv/headless 执行器现在可直接装配同一 AdaptiveRetry 与 LLM 重试分类，不再出现"GUI 有自适应重试、srv 没有"的行为缺口。

2026-09-02 补充：本轮完成了 GUI 的 `package main` 拆分。原 `gui/` 目录整体迁移为可导入的 `guiapp` 包（顶层 1685 个 `.go` 全部由 `package main` 改为 `package guiapp`，子包 `guiapp/petpack`、`guiapp/internal/systray` 保持原包名），唯一入口收敛为 `cmd/maclaw-gui`（瘦 main，仅持有 `-ldflags "-X main.version=..."` 注入的 `version` 并调用 `guiapp.Main(version)`；`guiapp/main.go` 的原 `func main()` 改为 `func Main(v string)`，函数体不变）。embed 相对路径、前端 `frontend/dist`、`builtin_skills` 等资源随目录整体移动保持有效；`hubcenter` 及仓库内所有 `CodeClaw/gui/` import 已改指 `CodeClaw/guiapp/`。构建/发布链路（`build_win.bat`、`build_maclinux.sh`、`build_win_tiger.bat`、`.github/workflows/*.yml`、`wails.json`、`scripts/*` 及 `.gitignore`）中构建目标改为 `./cmd/maclaw-gui/`、测试目标改为 `./guiapp/`、前端路径改为 `guiapp/frontend`。`scripts/check-agent-architecture.mjs` 新增静态门禁：`guiapp/` 顶层不得出现 `package main`、`cmd/maclaw-gui` 必须保持瘦入口、corelib 任何包不得 import `guiapp`；原有 `walk('gui')` 检查同步改指 `guiapp`。验证：`go build ./guiapp/ ./cmd/maclaw-gui/ ./hubcenter/...`、守门脚本、guiapp 抽样测试与 corelib/agentservice/MaClawSrv 的 CI 同款测试全部通过。

2026-09-03 补充：针对“一份 Runtime、两个宿主”的未完成项已继续落地。`RegisterBuiltinModules` 现在编译进 default-role / screenshot / open 模块；GUI composition root 与 Service 都加载同一份 builtin 清单。srv `BuildSystemPrompt` 使用 `agentruntime.ResolveRole`（不再使用 REST 专用身份）。请求级 `HostCapabilities` 会覆盖 executor 端口，headless 对 screenshot/open 保持模型可见并返回 `capability_unavailable`。`CoreAgentExecutor` 装配 `AdaptiveRetry`。GUI/VE/BTW/loop-command/coding-verify 以及 srv 都通过 `agentruntime.RunAgentTurn*` 进入 loop。GUI `TurnRequest` 注入 `Host`/`Events`，token callback 不再作为 Runtime 主路径。`cmd/maclaw-gui` 显式注册 handler factory。HTTP `routes()` 拆到 ops/admin/platform/user 注册函数。架构守门在 PR workflow `agent-architecture.yml` 上执行。

2026-09-03 补充（续）：grant 消耗判定、unsettled 状态和 spent-budget 通知从 GUI/srv 双份实现收敛到 `corelib/tool`；srv 现在也会在预算耗尽时附上同一条系统通知。screenshot `display` 进入共享 `DesktopCapturePort.Capture(ctx, DesktopCaptureRequest)`，builtin module、GUI 端口和 srv executor 走同一解析器。GUI `Runtime.Execute` 把请求级 Host 写入 `LoopContext`，`runAgentLoopShared` 不再无条件覆盖为 `guiHostCapabilities`。BTW 身份改走 `agentruntime.ResolveRole`。自定义 Store 缺少 lifecycle 契约时 `NewService` 发出 `best_effort` 警告。

2026-09-03 补充（intent 规则）：GUI `imSemanticIntentRuleSet` / `semanticCodingCapabilityRule` / archetype bundle 已迁到 `agentservice.IMSemanticIntentCapabilityNeedRules`、`ReviewedCodingCapabilityNeedRule`、`ExpandArchetypeBundleNeeds`。headless 与 IM 的 LabelSearch/LabelLiveData 现都映射 `information.search.web`（freshness=reference|current）；`information.lookup` 只留给已发布 MCP/Skill。生产 resolver 打开 ArchetypeBundles，full grant fence 由 `agentruntime.EnsureSemanticGrantPromptFence` 提供给 GUI 与 srv。

2026-09-03 补充（工具 schema）：`RegisterCoreTools` 现在通过 `LookupCoreTool` / `CoreToolJSONSchema` / `OverlayCoreToolSchema` 暴露编译期 schema。srv `coreToolSpecs` 对重叠工具调用 `specFromCoreTool`。GUI `buildToolDefinitions` 与 `registerBuiltinTools` 对重叠工具走 `toolDefFromCore` / `overlayCoreToolSchema`，只 overlay 桌面专属字段（bash `background`、screenshot `session_id`、asr `known_speakers`、workflow `phase_id`/`doc_type` 等）。生产 grant TTL 收敛为 `tool.DefaultInvocationGrantTTL`（10 分钟），GUI/srv/mobile/coding durable 共用。

2026-09-03 补充（surface 签发）：GUI 与 srv 的 coordinator/fallback 签发路径收敛为 `tool.PublishCurrentSurface` 与 `tool.IssueReadySurface`。`list_mcp_tools` / `import_mcp_servers` 进入 `RegisterCoreTools`，srv 列表支持 query/server_id 过滤。

2026-09-03 补充（ExtraSharedHost）：`delegate_task` / `office` / `generate_pdf` / `tts_render` / `edit_lines` 进入 `RegisterCoreTools`。GUI 与 srv 只 overlay 描述和桌面字段；srv `office` 补上 `write_pptx`。`ExtraSharedHostCapabilityNames` 现为空。剩余 GUI `toolDef`（会话/模板/配置汤、`craft_tool`、`call_mcp_tool`、`mis_data` 等）钉在 `HostPrivateCapabilityNames`，不得进 core。

2026-09-03 补充（规划编排助手）：GUI/srv 的 grant 名碰撞绑定、规划预算构造、qualifier 比较收到 `tool.BindIssuedGrants` / `NewPlanningBudget` / `QualifiersEqual`。规划循环本身仍分宿主，但不再各写一份绑定/预算/比较。

2026-09-03 补充（规划循环再抽）：effect/artifact 比较、binding-replacement 权威、完成集合合并、grant 表到 Live 集合的投影收到 `EffectsEqual` / `ArtifactContractsEqual` / `SelectionAuthorityEqualIgnoringProvider` / `MergeCompletedSelections` / `GrantSelectionIDs`。GUI 子 revision 签发也走 `BindIssuedGrant`。

2026-09-03 补充（GUI Job 镜像）：skill runner 与 task orchestrator 通过 `agentruntime.UpsertJob` 把生命周期写入共享 `JobRepository`；user-data migration 的 admit/update 样板也改走同一函数。skill.run / orchestrator.plan 记录 prepared/committed/failed effect，并由宿主实现只读 `JobReconciler`（不重放执行）。生产 GUI 用 `OpenGUIRuntimeJobStores` 把 jobs/effects 落到 data dir 的 SQLite；启动时 `ReconcileRuntimeJobs` 用已提交 effect 结算崩溃留下的 running 任务。

2026-09-03 补充（共享 SQLite Job 仓库）：生产 `async_jobs` schema（lease 列、schema v2、busy retry、idempotency unique index、CAS）从 `MaClawSrv/job_repository.go` 收到 `agentservice.SQLiteJobRepository`。srv 只保留 `NewSQLiteJobRepositoryWithLegacy` 薄包装和 `jobs.json` 一次性导入；GUI 打开同一仓库时若发现旧 `runtime_jobs` 表会一次性迁入。信封校验/克隆是 `agentruntime.ValidateJobEnvelope` / `CloneJob`。跨机器公平仍要外部限流器；srv 的 crash-recovery worker（`domainJobReconcilerFor`）仍比 GUI 启动时 `ReconcileRuntimeJobs` 更完整。

2026-09-03 补充（规划循环再抽 2）：闭环表面过滤、trusted-fact 并集、可见 ready grant 投影收到 `ClosedManagedDefinitions` / `UnionSatisfiedIDs` / `VisibleReadyGrants`。

2026-09-03 补充（规划循环再抽 3）：live grant 名、唯一 live grant、spent-budget 附注、known-grant 查找收到 `LiveGrantNames` / `SoleLiveGrantName` / `SpentBudgetNoteForGrants` / `HasKnownGrant`。srv `retireGrant` 的表迁移走 `RetireLiveGrant`（durable retire 失败时仍从 live 拿掉，与 GUI fail-closed 不同）。refresh vs Definitions 循环体仍分宿主（epoch / skip / rendered-set）。

2026-09-03 补充（规划循环再抽 4）：发 grant+绑定+issued 记录收到 `IssueAndBindReadySurface`；GUI skip 过滤收到 `FilterSelectionIDs`；增量 rendered-set 收到 `UnrenderedReadyGrants`；全量可见面渲染收到 `RenderVisibleReadyDefinitions` / `ProjectRenderedDefinitions`。GUI 仍先 `invalidateEpoch`，srv 仍先 `LoadCompletedSelections`+trusted facts。GUI skill/orchestrator 启动对账收到 `ReconcileOpenJobs`；srv crash-recovery worker（lease + unknown-only + CAS）仍更完整。

2026-09-03 补充（规划循环再抽 5）：GUI refresh 与 srv Definitions 的发 grant/渲染循环收到 `MaterializeReadySurface`（GUI 仍先 `invalidateEpoch` 并走增量 RenderedNames；srv 仍先 load completion+trusted facts 并走全量可见面）。unsettled 查找收到 `SelectionUnsettled`。committed/failed effect 只读对账收到 `ReconcileFromProtectedEffects`；resource id / operation 解析收到 `ParseJobEffectResourceIDs` / `JobEffectOperation`。srv 对 prepared/unknown 的 MCP/skill/knowledge 现场探测仍是宿主只读逻辑。

2026-09-04 补充（规划循环再抽 6）：下一轮暴露闭包收到 `NextExposedSelections`，由 `MaterializeReadySurface` 在 Needed 为空时计算；GUI/srv 只再提供 Unsettled。lease 过期判定与 RecoveryPolicy 应用收到 `JobLeaseExpired` / `RecoverExpiredJobLease`；srv `applyExpiredJobRecoveryLocked` 成为薄包装。GUI 启动对账仍不走 lease（skill.run 无 owner）。

2026-09-04 补充（规划循环再抽 7）：srv Definitions 的 completion+trusted facts 准备收到 `PrepareReadySurfaceCompletion`。effect 归属校验收到 `JobEffectMatchesJob`；结算后信封（CompletedAt、清 lease/retry）收到 `ApplyResolvedJobReconcile`，GUI `ReconcileOpenJobs` 与 srv unknown-job CAS 共用。过期 lease 扫描收到 `RecoverExpiredJobs`；srv 仍自己 persist 并跳过本进程仍持有 cancel 的 owner。

2026-09-04 补充（规划循环再抽 8）：lease TTL/deadline/persist 续期收到 `DefaultJobLeaseTTL` / `JobLeaseDeadline` / `PrepareJobLeaseForPersist` / `JobShouldRenewLease`；srv 心跳仍自己 ticker + persist。GUI complete/retire 的按 selection 退 grant 收到 `RetireLiveGrantsForSelection`。epoch 入口与 trusted-fact 入口仍分宿主。

2026-09-04 补充（Job 生命周期信封）：尝试开始、重试停泊、worker 终态、持久化失败、完成写失败（有 protected effect 则 unknown）收到 `BeginJobAttempt` / `ScheduleJobRetry` / `ApplyJobWorkerOutcome` / `MarkJobPersistenceFailed` / `MarkJobCompletionPersistFailed`。srv execute/complete 走这些助手。

2026-09-04 补充（GUI host stamp）：skill runner 与 task orchestrator 的 DTO 镜像收到 `StampHostJobStatus`（running 走 `StampJobRunning`/`BeginJobAttempt`，终态走 `ApplyJobWorkerOutcome`），再 `UpsertJob`。

2026-09-04 补充（规划循环再抽 9）：`MaterializeReadySurface` 入口收到 `ApplyReadySurfaceCompletion`（有 Executor 则 load completion ∪ TrustedFacts，否则 Satisfied = Completed ∪ facts）。GUI refresh 仍先 `invalidateEpoch` 并走增量 RenderedNames；srv Definitions 仍走全量 IndexByName。

2026-09-04 补充（Job 启动对账）：GUI skill/orchestrator 启动收到 `RecoverAndReconcileOpenJobs`（先 `RecoverExpiredJobsInRepository` 把无 owner/过期 lease 的 running 标 unknown，再 `ReconcileOpenJobs` 用 effect 结算）。srv 心跳间隔收到 `DefaultJobLeaseTick`，续期选择收到 `ShouldRenewLocalJobLease`；ticker + persist 仍分宿主。

2026-09-04 补充（取消信封与交付完成）：srv user/admin/shutdown 取消收到 `StampJobCanceled`（走 `ApplyJobWorkerOutcome`，shutdown 可带自定义 ErrorText）。GUI 回合交付完成判定收到 `TurnRequiredDeliveryComplete`（按 current-channel capability，不再写死 adapter 名）。

2026-09-04 补充（消费 grant 退表）：已消费 grant 从 live 拿掉收到 `RetireConsumedGrant`（durable 失败仍移走，避免本进程再暴露一次性函数）。GUI complete/retire 的 `RetireLiveGrantsForSelection` 与 surface 恢复、srv `retireGrant` 都走它。`RetireLiveGrant` 仍是“durable 先成功再移表”的严格原语。

2026-09-04 补充（Job 留存裁剪）：age/count housekeeping 收到 `SelectJobsForRetentionPrune`（unknown 永不裁；默认 24h / 2000 条）。仓库删除收到 `PruneRetainedJobsInRepository`，挂在 `RecoverAndReconcileOpenJobs` 末尾，GUI skill/orchestrator 启动对账会 prune。srv `pruneLocked` 仍自己 persist/回滚。

2026-09-04 补充（恢复消费 grant）：surface 打开时隐藏已消费 grant 收到 `RetireConsumedLiveGrants`（execution not found 保留；其它 store 错误 fail-closed）。GUI open 与 srv constructor 都走它。

2026-09-04 补充（CAS 与 spent-budget）：仓库 CAS/记录消失判定收到 `IsJobRepositoryConcurrencyError`，srv persist-fail-closed 走它。spent-budget 附注打到 selection result 收到 `ApplySpentBudgetNote`（headless Execute）；GUI 仍用 `SpentBudgetNoteForGrants` 作为独立附注。

2026-09-04 补充（generate skip）：host-owned document generate 判定收到 `DocumentGenerateSelection` / `IsDocumentGenerateFile`（capability 优先，generate_pdf 与 host_document_generate_file 为遗留名）。未签发 ready generate 收到 `HasUnissuedReadyDocumentGenerate`。GUI hold-dependant skip 走这些助手。

2026-09-04 补充（generate-after-lookup）：补齐 generate 的已满足 lookup 依赖收到 `ForEachHostSatisfiedLookupForGenerate`（confirmation/非 lookup 阻断 fail-closed）。GUI complete/retire 仍是 visit 回调。

2026-09-04 补充（grant/交付投影）：按 adapter 唯一 live grant 收到 `SoleLiveGrantByAdapter`；已退 grant 按 adapter 查找收到 `HasRetiredGrantByAdapter`（generate_pdf 与 host_document_generate_file）。current-channel 单边交付依赖收到 `CurrentChannelDeliveryDependency` / `ArtifactDependencyKind`。跨机器限流、行为快照 CI、lease ticker 仍分宿主。

2026-09-04 补充（artifact 契约/产物/按能力 grant）：契约通配匹配收到 `ArtifactContractMatches`（required MIME 空为通配；与 exact `sameArtifactContract` 分开）。binding 形态收到 `ArtifactBindingMatchesContract`；planner `producesArtifact` / 可信 fact 绑定走它们。RouteState 产物是否已发布收到 `ProducerArtifactPublished`。按能力查 live grant 收到 `LiveGrantNameForCapability`。GUI broker/auto-delivery/petition 走这些助手。

2026-09-04 补充（artifact 依赖选取）：唯一匹配依赖收到 `UniqueMatchingArtifactDependency`；已绑定形态收到 `UniqueBoundArtifactDependency` / `ValidateBoundArtifactDependency`。repeat 家族内最新产物收到 `NewestFamilyProducerArtifact`（2026-08-26 修订稿投递：delivery 绑在第一 sibling 仍要拿到后来的 revision）。GUI broker consume 走这些助手；ArtifactStore 的 `artifactMatchesContract` 也走 `ArtifactContractMatches`。

2026-09-04 补充（light 表面过滤）：grant 是否 light-safe 收到 `GrantSelectionIsLightPromptSafe`；loop 入口预过滤收到 `ClosedManagedDefinitionsForProfile` / `FilterLightPromptSafeDefinitions`。GUI ForTurn 与 headless BuildTools/authorizer 走这些助手，避免 light 回合带着会被立刻丢掉的 mutating tool 进 RunLoop。

2026-09-04 补充（执行结果/replay）：provider 结果到 PlanExecution 状态收到 `PlanExecutionStateFromResult`（unknown/awaiting 清掉 Succeeded）。host-call replay 以 execution 行为准收到 `ReplayedSelectionResult`；读不到 durable 判定才走 `RecordedSelectionResultFallback`。GUI IM complete、GUI coding complete/replay、headless complete/replay 走这些助手。

2026-09-04 补充（session-governed 副作用）：need 是否会重放 mutation 收到 `CapabilityNeedHasSideEffect` / `CapabilityNeedsHaveSideEffect`。有 registry 时以 Effects 为准；未登记则只读家族回退（lookup/search/fetch/read/inspect/transcribe/ask-user 等）。未知 capability 默认有副作用。GUI 与 headless SessionGovernedTask 走这些助手。

2026-09-04 补充（host-call acquire/lookup 别名）：journal/coordinator acquire 的 terminal 判定收到 `HostCallAcquireTerminal`；grant 指纹冲突被改写成 replay 时走 `HostCallReplayResult`（只用 recorded text，不发明 execution 行判定）。GUI IM/coding 与 headless 三处 acquire 循环走它。web-search 稳定名别名收到 `AliasableLookupSelection` / `SoleLiveLookupGrantName`（不含 fetch/clock）。

2026-09-04 补充（granted needs 投影）：计划 selection → session-governed/continuity need 收到 `GrantedNeedFromSelection` / `GrantedNeedsFromPlan`（NeedID 优先，空则用 selection ID）。need 拷贝收到 `CloneCapabilityNeed` / `CloneCapabilityNeeds`。GUI persist、headless SessionGovernedTaskStore、continuity open-need 走这些助手。

2026-09-04 补充（continuation/qualifier）：UIC 泛型 continuation 判定收到 `ClassificationResult.IsGenericContinuationPrimary`（continuation/unknown/ambiguous；不含 non_coding）。qualifier map 拷贝收到 `CloneNeedQualifiers`；sibling 去重 key 收到 `NeedQualifierKey`。GUI session-governed、coding catalog、expert policy 与 headless resolver/archetype 走这些助手。

2026-09-04 补充（granted-need 覆盖/fact clone）：回放前过滤仍被规则覆盖的 need 收到 `FilterGrantedNeedsStillCovered` / `GrantedNeedStillCovered`（registry 非空则未知 capability fail-closed）。规则表投影收到 `CoveredCapabilitiesFromNeedTemplates`。RoutingFact/Constraint 拷贝收到 `CloneRoutingFact(s)` / `CloneRoutingConstraint(s)`。GUI session-governed 与 coding plan、headless persist/policy 走这些助手。

2026-09-04 补充（need template sibling 展开）：reviewed template → sibling needs 收到 `ExpandNeedTemplateSiblings` / `ExpandNeedTemplates` / `NeedTemplateIdentityKey`。GUI coding policy 与 headless intent resolver、archetype 新家族走同一套 ID（`need:` / `need:coding:` 前缀）和 `RepeatSiblingRequired`。已有家族补 sibling 仍按 base need 克隆（archetype）。

2026-09-04 补充（intent rule coverage）：UIC 相对规则表的 managed/unmapped 扫描收到 `IntentRuleCoverageFromClassification`。至少一条有规则为 managed；第一条非 generic 无规则为 unmapped。GUI `imSemanticIntentCoverage` 与 headless resolver 的混合迁移 fail-closed 走它。

2026-09-04 补充（classification HasLabel）：UIC 是否包含某 label 收到 `ClassificationResult.HasLabel`。GUI `classificationHasLabel` 与 headless `classificationHasIntentLabel`（archetype bundle 选择）走它；NamedSkillInterceptCandidate 也走同一判定。

2026-09-04 补充（petition expansion）：NeedID 一对一映射收到 `SelectionsByNeed`（replan subset / binding-replacement 共用）。petition 子计划严格超集校验收到 `ValidatePetitionExpansion`。GUI IM petition 走它，避免自己再建一份 need 表。

2026-09-04 补充（child turn identity）：binding-recovery 子回合 ID 收到 `ReplanTurnID`；petition 扩展子回合 ID 收到 `PetitionTurnID`（`replan:` / `petition:` 前缀 + digest）。GUI IM 与 headless `ReplanAfterBindingFailure` 走同一公式，journal lineage 不会因宿主分叉。

2026-09-04 补充（petition label 反向映射）：capability → 可 petition 的 intent label 收到 `PetitionLabelForCapability` / `PetitionPreferredLabel`（sole required 优先，search 压 live_data，其余按 label 名）。GUI IM petition 走它，避免规则表迭代顺序挑出第二个 label。

2026-09-04 补充（granted-need → UIC）：session-governed 回放从 granted needs 重建分类收到 `ClassificationFromGrantedNeeds`。generate 压 attachment delivery，search freshness 分 search/live_data，system.launch 固定 app_launch。GUI IM 回放走它；headless ReplayContinuation 仍直接注入 needs。

2026-09-04 补充（trusted input identity）：ingress 附件 sourceID 收到 `TrustedAttachmentSourceID`（SourceMediaID 优先，否则 `attachment:index:basename:mime`）；PlanID 收到 `TrustedInputPlanID`（`input:` + turn）。GUI document/audio 与 headless document/image/voice 走同一套，避免缺 media handle 时铸出第二份 ArtifactRef。

2026-09-04 补充（trusted input 唯一性）：恰好一份 ingress 判定收到 `UniqueTrustedInputCount`（0 → missing，>1 → ambiguous）。GUI document 绑定与 headless document/image/voice deliver 走它；计划错误分类收到 `IsTrustedInputMissingOrAmbiguous`。generate 是否跳过 file-deliver 仍分宿主。

2026-09-04 补充（need membership）：needs 是否包含某 capability 收到 `CapabilityNeedsContain` / `CapabilityNeedsContainAny`。GUI generate 跳过 file-deliver、audio/document-read NeedPresent 与 headless reviewed-host generate/audio/visual NeedPresent 走它。document-read 是否同时认 attachment-deliver 仍分宿主。

2026-09-04 补充（current-channel deliver format）：need 是否 current-channel deliver 且 format 命中允许集收到 `CurrentChannelDeliverNeed` / `CurrentChannelDeliverAccepts`。GUI file-deliver 绑定传 `file`；headless reviewed deliver 传 file/image/voice/空。generate 跳过 file-deliver 仍分宿主。

2026-09-04 补充（document-read format）：`document.read.local` 判定收到 `IsDocumentRead`；ingress format 写入收到 `BindDocumentReadFormat`。GUI trusted-document 绑定与 headless reviewed document-read 走它。

2026-09-05 补充（in-turn producer 跳过 ingress）：本回合已有产物、current-channel deliver 不应再绑附件收到 `InTurnArtifactProducerPresent`（document generate 必算；宿主可加 extra）。GUI 传入 office write；headless 传入 audio-render / visual-capture。

2026-09-05 补充（repeat family 补 sibling）：已有家族把预算抬到 companion 模板上限收到 `ExtendRepeatFamily`（不重铸 base，后补 sibling 一律 optional）。archetype 已有家族升级与 `ExpandNeedTemplateSiblings` 的 ceiling 走它；两个宿主仍经 `ExpandArchetypeBundleNeeds` / `ExpandNeedTemplates`。

2026-09-05 补充（行为快照 CI）：冻结分类的 need-family 面收到 `SnapshotSemanticNeedFamilies`（capability+polarity+qualifiers+req/opt，不含 need ID / grant token）。`TestGUIAndSrvSemanticBehaviorSnapshot` 对比 IM 规则与 reviewed 规则（生产 ambient+archetype），重叠 required 身份必须一致（office/launch qualifier 仍分宿主），完整面钉在 `testdata/semantic_behavior_snapshot.txt`。GUI 用真实 IM catalog 再跑一遍 required 身份。PR/发布 `verify-agent-architecture` 的 `GUIAndSrv` 正则会跑到。

2026-09-05 补充（reviewed 重复预算）：快照暴露 headless search/fetch/download/office/shell 的 `MaxInvocations` 仍是历史 1，IM 已是 5/5/3/8/8。reviewed 规则对齐后，office+live_data 的 search 天花板与 GUI 相同；`TestIMAndReviewedIntentRulesShareRepeatBudgets` 钉住同一 identity 的预算。office/launch qualifier 仍分宿主。

2026-09-05 补充（plan/surface 快照 CI）：计划选择与第一波暴露收到 `SnapshotSemanticPlanSurface`（capability|qualifiers|adapter|phase 的 sel，或 capability|qualifiers|adapter 的 first；repeat 用 n= 折叠；不含 grant token / selection ID / plan digest）。第一波是空 completed/granted 下的 `NextExposedSelections`。两宿主 golden 经 `SyncSnapshotFile` 写入；重叠第一波身份（capability|qualifiers，去 adapter、去 office write / local launch）由 `PlanSurfaceFirstWaveParityErrors` 对对方 golden 互核；无 sel 的 screenshot/audio-deliver 跳过；本宿主有 sel 而对方 golden 对不上（错宿主/空 peer）视为漂移而不是空过。srv 用 reviewed 宿主目录，GUI 用 IM trusted adapter + desktop 渠道。`TestGUIAndSrvSemanticPlanSurfaceSnapshot` 分别钉 `corelib/agentservice/testdata/semantic_plan_surface_snapshot.txt` 与 `guiapp/testdata/semantic_plan_surface_snapshot.txt`。交付 adapter 仍分宿主（GUI current-channel，srv 无 destination 则 unmet）。

2026-09-03 补充（P2 限流/Job）：`SQLiteDistributedRateLimiter` 给共享 data dir 的多进程提供同一 token-bucket；srv 用 `MACLAW_RUNTIME_RATE_LIMIT_SHARED=true` 打开。跨机器公平仍要外部限流器。`domainJobReconcilerFor` 补上 `migration.export`/`migration.import` 本地回退。

自定义 Store 若尚未实现 `RunLifecycleTransactionStore` 且未设置 `RequireAtomicLifecycle` 仍走 `best_effort`，但 `NewService` 会记录警告。GUI semantic routing 与 srv dynamic semantic 的授权编排仍分叉，grant 消耗/unsettled/spent-budget 判定已是唯一实现。screenshot `display` 由共享 `DesktopCaptureRequest` 贯穿 builtin module、GUI 端口和 srv executor。GUI `Runtime.Execute` 会把请求级 `Host` 写入 `LoopContext`。

## 7. 优先级清单

### 立即处理（P0/P1）

- ✅ `NewHTTPServerWithError` 返回构造错误，旧 `NewHTTPServer` 仅保留兼容包装，消除 nil 解引用启动路径；
- ✅ `NewService` 构造失败统一清理内部 Store 与已打开的 records/operations/capabilities/event store；外部注入 Store 的所有权保持不变；
- ✅ 主程序统一调用 `svc.Close()`，并关闭 `CoreAgentExecutor` 缓存的 memory stores；
- ✅ 内置 SQLite/FileStore/MemoryStore 已为消息/会话幂等键增加数据库唯一约束和原子写入，并实现 `RunLifecycleTransactionStore`；MaClawSrv 生产构造 `RequireAtomicLifecycle=true` 对自定义 Store fail-close；未实现该契约且未开启该开关的嵌入后端仍以 `best_effort` 运行，并在 `NewService` 打出明确警告；
- ✅ 已定义 Runtime/HostCapabilities 接口；请求级 Host 贯穿 srv 执行，builtin module 由 `RegisterBuiltinModules` 编译进 GUI/srv；AdaptiveRetry 已装配到 srv；默认角色与 host-surface 工具不再分叉；
- 🔄 GUI `semantic_tool_routing.go` 与 srv `dynamic_semantic_*` 仍是两套规划编排循环；intent→need 规则表、archetype bundle、full grant fence、surface 签发/materialize、grant 绑定、规划预算、qualifier/effect/artifact 比较、完成集合合并和 repeat Live 投影已收到共享层。GUI 只保留别名。srv 生产 resolver 已打开 ArchetypeBundles。重叠工具 schema（含 ExtraSharedHost 的 `delegate_task` / `office` / `generate_pdf` / `tts_render` / `edit_lines`）已从 `RegisterCoreTools` 长出，GUI 两份目录仅 overlay 桌面字段。LabelSearch/LiveData 已归并到 `information.search.web`。
- ✅ Tool-call 参数 object 解析、空值归一化、browser/Glob 兼容别名已收敛到 `agentruntime`，GUI 与 headless executor 共享同一实现和回归测试；
- ✅ `database` 的描述/schema 与 SQL 连接、查询、执行投影已收敛到 `corelib/database`，GUI/srv 共享同一实现；srv 连接按 session 隔离并由 `CoreAgentExecutor.Close` 回收；
- ✅ 拆出 `guiapp` 与 `cmd/maclaw-gui`，消除 `package main` 对复用和测试的阻塞（2026-09-02 完成：Wails/IM host 代码位于可导入的 `guiapp` 包，瘦入口为 `cmd/maclaw-gui`，守门脚本新增对应静态门禁）；
- ✅ run 已具备 durable event 与真实 token/tool 流，SSE 使用事件游标而非 300ms 轮询。
- ✅ Admin public knowledge async URL/file import 已纳入统一 idempotent admission、effect/reconcile 与 replay-safe audit contract；Job 删除会同步触发 protected effect 清理。
- ✅ Runtime producer event id 已按 run scope 做幂等，SQLite 旧全局 event_id 主键可在线迁移；PDF invocation normalization/title-only admission 已下沉到共享 Runtime。

### 后续处理（P2）

- ✅ 拆分 `http.go` 和 `service.go` 的域边界（2026-09-03：`routes()` 只做装配，路由注册迁入 `http_routes_ops.go` / `http_routes_admin.go` / `http_routes_platform.go` / `http_routes_user.go`。2026-09-02：`http.go` 已从 6,052 行降至约 3,000 行——139 个路由 handler 按域迁入 `admin_handlers.go`（52）、`http_agent.go`（26）、`http_skills.go`（12）、`http_mcp.go`（11）、`http_im.go`（11）、`http_config.go`/`http_jobs.go`/`http_records.go`（各 5）、`http_memory.go`（4），IM/微信/第三方运行时同步与二维码令牌存储迁入 `http_runtime_sync.go`（25），认证限流迁入 `http_auth_limiter.go`（8）。`service.go` 已从 7,024 行降至约 560 行。transport 子包仍可继续下沉 DTO/mux，但不再在 `http.go` 内维护路由表）；
- 🔄 配置 schema、Job 状态词汇、JobRepository/CAS、JobReporter/checkpoint、有界 retry policy、admission 幂等摘要、数据库级跨进程唯一约束、worker lease/ownership、过期恢复 policy、受保护 JobEffectRepository 和跨入口错误码已统一；migration、MCP mutation、Skill install/import、Knowledge import 的 effect/reconcile 已落地，`domainJobReconcilerFor` 覆盖全部 Reconcile 准入 kind（含 migration）；GUI skill runner / 编排计划已镜像进共享 JobRepository，生产路径用 SQLite jobs/effects + 启动 reconcile；跨机器限流仍待外部实现；
- 🔄 已落地 HTTP `X-Request-ID`/`traceparent` 到 Run/audit/event 的 correlation 基线及共享 Runtime 运行指标；✅ Service admission 已提供可配置、有界租户 token-bucket（本地 bucket 满时 LRU 淘汰）；✅ 共享 data dir 可用 `SQLiteDistributedRateLimiter`（`MACLAW_RUNTIME_RATE_LIMIT_SHARED`）；跨机器严格公平仍需外部分布式限流器；✅ 结构化日志边界（`corelib/logx`，强制脱敏 + correlation 注入）以及双向 traceparent 传播（srv→hub 注入、hubcenter 入站解析）已落地；
- ✅ 扩展 parity/contract tests，并将 GUI 与 srv 行为快照纳入 CI。已钉 ExtraSharedHost 为空、HostPrivate 名单、reviewed search.web 与 grant 绑定共享层。冻结分类的 need-family 快照由 `TestGUIAndSrvSemanticBehaviorSnapshot` 钉在 `corelib/agentservice/testdata/semantic_behavior_snapshot.txt`；plan/第一波 surface 由 `TestGUIAndSrvSemanticPlanSurfaceSnapshot` 钉在 `semantic_plan_surface_snapshot.txt`（srv 与 GUI 各一份）。发布/PR 的 `verify-agent-architecture` job 通过 `GUIAndSrv` 正则执行。
- ✅ Runtime surface parity contract 已增加双宿主独立装配、prompt/tool/module digest 相等与故意漂移检测；发布 CI 的 shared Runtime contract test 已纳入该断言。GUI 与 headless 的 receipt effect 分类、PDF 标题/正文门槛、计划 capability 查找、工具定义过滤和稳定字符串集合也已由共享 core contract 提供。
- ✅ capability surface digest 对不可 JSON 序列化的工具 schema 现在 fail-closed，不再把非法 schema 静默折叠为空参数后与合法 surface 产生相同摘要。
- ✅ 搜索证据复用的控制标记/文件载荷污染检查已下沉到 `agentruntime.TrustedLookupEvidence`；GUI 不再单独维护 evidence trust 规则，srv/TUI 可直接采用相同 fail-closed 投影。Semantic plan selection 过滤与按 ID 查找也统一由 `corelib/tool.PlanWithSelections` / `PlanSelectionByID` 提供，GUI 与 headless 不再复制计划裁剪/查找循环。
- ✅ artifact 文件名安全化、文档临时后缀、首个必需 artifact contract 以及 current-channel/schedule selection 判定已统一到 `corelib/tool/semantic_artifact_projection.go`，GUI 只保留适配层。
- ✅ 动态 Skill/MCP selection 的 external/sensitive receipt 判定也已统一到 `corelib/tool.SelectionRequiresExternalReceipt`，GUI 与 srv 不再维护两份 effect 遍历逻辑。
- ✅ Lookup selection 的 evidence-family 判定已统一到 `corelib/tool.IsLookupSelection`；GUI 的 generate-after-lookup 依赖不再直接读取 capability 字符串，后续新增 lookup capability 只需修改 Runtime 规则。
- ✅ MemoryStore 的 admission/completion/terminal lifecycle transaction 现在带完整 savepoint 回滚；事件 envelope 后续校验失败不会留下孤立 message、run 或部分 outbox 事件，与 FileStore/SQLite 的原子语义一致。
- ✅ Semantic tool 文本到旧 Agent outcome 枚举的投影已统一到 `agentruntime.SemanticToolExecutionResult`；GUI 不再自行判断 rejected/unknown 标记，headless/TUI 可复用相同的重试边界。
- ✅ Legacy text-only host tool 的失败/超时 marker 分类已统一到 `agentruntime.ToolTextResult`；GUI 与 `CoreAgentExecutor` 共用首行/超时规则，命令输出中的后续 `Error:` 文本不会被误判为工具失败。

## 8. 最终判断

MaClawSrv 的持久化、多租户、鉴权和 HTTP 能力已经足以作为服务产品继续演进；真正的架构风险是 **Agent 行为的事实来源仍在 GUI `package main`，而不是可复用的 core runtime**。只要继续在 srv 侧补 endpoint 或复制 GUI handler，短期功能会增加，长期却会重新形成“GUI、TUI、srv 三套 Agent”的同步负担。

最终设计原则应收敛为：

> **GUI 不是 Agent 的实现者，MaClawSrv 也不是 Agent 的实现者；二者都是同一个 Runtime 的宿主。**

因此“GUI 改进可直接被 srv 使用”必须成为架构验收条件，而不是靠开发约定：任何 Agent 功能必须先落到 `corelib/agentruntime` 的 module、contract 和测试，再由 GUI 与 MaClawSrv 通过宿主适配器获得；CI 必须阻止只修改 `gui/` 或只修改 `MaClawSrv/` 的非界面实现，也必须阻止新旧 Runtime 并行存在。
