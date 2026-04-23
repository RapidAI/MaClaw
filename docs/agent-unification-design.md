# Agent 统一架构设计

## 0. 实施进度

| 步骤 | 状态 | 说明 |
|------|------|------|
| 设计文档 | ✅ 完成 | 本文档 |
| `h.app` 依赖提取 | ✅ 完成 | ~160 处 → ~29 处（GUI 特有），新增 `gui/im_app_accessors.go` |
| `NewIMMessageHandlerStandalone` | ✅ 完成 | 不依赖 `*App` 的构造函数，3 个测试通过 |
| `ConversationMemory` 迁移 | ✅ 完成 | `gui/im_conversation_memory.go` → `corelib/agent/conversation_memory.go` |
| `IMUserMessage` / `MessageAttachment` 统一 | ✅ 完成 | GUI 类型改为 `corelib/agent` 别名 |
| `corelib/agent` 接口层 | ✅ 完成 | `Handler` 接口 + `Config` + `HandlerFactory` 注册机制 |
| 共享 agent loop | ✅ 完成 | `corelib/agent/loop.go`：`RunLoop` + `LoopCallbacks` + `LoopHooks`，含空响应 hard exit + 漂移检测，8 个测试 |
| TUI 统一 loop 适配 | ✅ 完成 | `tui/agent_loop_unified.go`：TUI 通过 `LoopCallbacks` 使用共享 loop |
| TUI 旧 loop 删除 | ✅ 完成 | `tui/agent_handler.go`：`RunAgentLoop` 删除 188 行，改为委托 `RunUnifiedLoop` |
| TUI 编入 GUI 二进制 | ✅ 完成 | `gui/tui_mode.go`：`maclaw tui` 直接使用 `IMMessageHandler`，能力与桌面/IM 完全一致 |
| TUI 独立二进制废弃 | ✅ 重建 | `maclaw-tui` 重建为独立二进制，通过 `corelib/agent` 共享组件，仅 UI 层使用 Bubble Tea |
| GUI bridge | ✅ 完成 | `gui/agent_handler_bridge.go` 将 `IMMessageHandler` 注册为 `agent.Handler` |
| TUI adapter | ✅ 完成 | `tui/app.go` 通过 `agent.LoopCallbacks` 使用共享 loop + 共享工具 |
| Package 迁移（`runAgentLoop` → `corelib/agent`） | ⏭️ 绕过 | 通过 `LoopCallbacks` 接口解决：共享 loop 在 `corelib/agent/loop.go`，TUI 通过回调提供工具/prompt |
| TUI 切换到统一 handler | ✅ 完成 | `MACLAW_UNIFIED_LOOP=1` 环境变量启用，TUI 使用 `agent.RunLoop` |
| Hub 退化为纯消息代理 | ✅ 完成 | `/workflow` 转发设备、`hubDirectAnswer` 移除、QuickFilter 移除、死代码清理 -2791 行 |

**核心阻塞**：~~`gui/` 和 `tui/` 都是 `package main`~~ 已通过 `LoopCallbacks` 接口 + `maclaw tui` 子命令解决。TUI 独立二进制已重建：通过 `corelib/agent.RunLoop` + `corelib/agent.Tool*` + `corelib/agent.BuildSystemPrompt` 共享所有 agent 逻辑，仅 Bubble Tea UI 层是 TUI 特有代码。`maclaw tui` 子命令（GUI 内嵌 TUI）和 `maclaw-tui`（独立二进制）两条路径并存。

**共享代码统计**：`corelib/agent/` 6730+ 行 + `corelib/llm/anthropic_convert.go` 114 行 = 6844+ 行平台无关代码。Hub 死代码清理 -2791 行。TUI 独立二进制 `tui/app.go` 仅 ~300 行（纯 UI 接线），所有 agent 逻辑来自 `corelib/agent/`。

## 1. 问题陈述

当前系统存在三套 agent 实现，导致每个功能改进都要在多处同步修改，bug 修复经常遗漏某一侧：

| 层 | 代码位置 | 角色 |
|----|----------|------|
| GUI Agent | `gui/im_message_handler.go` + 周边 | 桌面面板 + IM 通道的 agent loop、工具执行、workflow |
| TUI Agent | `tui/agent_handler.go` + 周边 | 终端的独立 agent loop、独立工具定义、独立工具执行 |
| Hub Agent | `hub/internal/im/workflow_engine.go` + 周边 | 独立的 workflow engine、intent understanding、quick filter |

**维护代价量化**：

TUI 独立维护的代码（与 GUI 功能重复）：
- `agent_handler.go`：RunAgentLoop（~200 行，GUI 的 runAgentLoop 有 ~2000 行，功能子集）
- `agent_handler.go`：buildBuiltinToolDefinitions（~280 行，与 `gui/im_tool_definitions.go` 完全重复）
- `agent_handler.go`：dispatchTool（~160 行，与 GUI 的 executeTool 完全重复）
- `agent_tools.go` + `agent_tools_ssh.go` + `agent_tools_office.go` + `agent_tools_missing.go`：每个工具 handler 都是 GUI 侧的重新实现
- `agent_handler_workflow.go`：TUI 侧 workflow 拦截（GUI 有更完整的实现）
- `agent_handler_nudge.go`、`agent_handler_outcome.go`、`agent_compress.go`：各种辅助功能的 TUI 版本

Hub 独立维护的代码（与 `corelib/workflow` 功能重复）：
- `workflow_engine.go`：独立的 WorkflowEngine（~870 行，`corelib/workflow/engine.go` 的简化版）
- `workflow_types.go`：独立的类型定义（只有 4 个 WorkflowType，corelib 有 19 个）
- `workflow_templates.go`：独立的模板定义（4 个模板，corelib 有 19 个）
- `workflow_registry.go`：独立的 Registry（~80 行，corelib 的简化版）
- `intent_understanding.go`：独立的意图理解（~530 行，corelib 的简化版）
- `quick_filter.go`：独立的消息分类（~130 行，corelib 的简化版，缺少三层语义保底）

**改进记录中的同步修改证据**（每条都要改两处或三处）：
- #5 Skill Runner 跨步骤状态传递：TUI + GUI 各改一遍
- #7 Windows 兼容性：TUI + GUI 各改一遍（BUG-001~005 每个都是两份代码）
- #8 SSH sudo 支持：TUI + GUI 各改一遍
- #9 Operations/Poll/When：TUI + GUI 各改一遍
- #10 SKILL.md 变量传递：TUI + GUI 各改一遍
- #14 工具合并：TUI + GUI 各改一遍
- #49 Steering 机制：TUI + GUI 各改一遍

## 2. 目标架构

```
                    ┌─────────────────────────────────────────┐
                    │              用户入口                     │
                    ├──────────┬──────────────┬────────────────┤
                    │ 桌面面板  │  IM 平台      │  终端 (TUI)    │
                    │ (Wails)  │ (飞书/微信/QQ) │  (Bubble Tea)  │
                    └────┬─────┴──────┬───────┴───────┬────────┘
                         │            │               │
                         │      ┌─────┴─────┐         │
                         │      │  Hub      │         │
                         │      │ 纯消息代理 │         │
                         │      └─────┬─────┘         │
                         │            │               │
                    ┌────┴────────────┴───────────────┴────────┐
                    │         IMMessageHandler                  │
                    │         (唯一的 agent loop)                │
                    │                                           │
                    │  platform = "desktop" | "feishu" | "tui"  │
                    ├───────────────────────────────────────────┤
                    │  共享基础设施                               │
                    │  - corelib/workflow (唯一 workflow engine)  │
                    │  - corelib/tool (唯一 tool router)         │
                    │  - corelib/intent (唯一 intent classifier) │
                    │  - Tool Registry + Tool Execution          │
                    │  - Memory / Steering / Drift Detection     │
                    └───────────────────────────────────────────┘
```

**核心原则**：一个功能只有一份实现。平台差异通过数据（`platform` 字段）而非代码分支解决。

## 3. 当前架构详细分析

### 3.1 GUI Agent（桌面 + IM 已统一）

桌面面板和 IM 通道**已经共享同一个 `IMMessageHandler`**：

```
桌面面板:
  SendAIAssistantMessage() (Wails binding)
    → msg = IMUserMessage{UserID: "desktop-user", Platform: "desktop", ...}
    → handler.HandleIMMessageWithProgressAndStream(msg, onProgress, onToken, ...)

IM 通道:
  handleIMUserMessage() (WebSocket from Hub)
    → msg = IMUserMessage{UserID: "wx_123", Platform: "wechat", ...}
    → handler.HandleIMMessageWithProgress(msg, onProgress)
```

两者汇入同一个 `runAgentLoop()`，差异仅通过 `platform` 参数做分支：
- 文档交付：桌面输出 Markdown，IM 生成 PDF
- 流式推送：桌面有 `onToken` 回调，IM 没有
- 文件处理：桌面保存本地返回路径，IM 发送给用户

这是正确的架构——**平台差异通过数据驱动，不是代码分支**。

### 3.2 TUI Agent（完全独立）

TUI 有自己的 `TUIAgentHandler.RunAgentLoop()`，是 GUI `runAgentLoop()` 的简化版：

**TUI 有但 GUI 也有的（纯重复）**：
- Agent loop（LLM 调用 → 工具执行 → 循环）
- 工具定义（~50 个工具，与 GUI 完全相同）
- 工具执行（dispatchTool，每个 case 都是 GUI 的重新实现）
- Workflow 拦截（handleTUIWorkflowInterception）
- 工具路由（Router.Route）
- Session pinning（ActivateSessionTool）
- Skill outcome tracking
- Nudge injection
- Auto-compress conversation

**TUI 有但 GUI 没有的（TUI 特有）**：
- Bubble Tea UI 集成（stream callback → `views.ChatStreamMsg`）
- 对话历史格式（`[]map[string]string` vs GUI 的 `[]conversationEntry`）
- MemoryShot 持久化（TUI 用 memoryshot，GUI 用 conversationMemory）

**GUI 有但 TUI 没有的（TUI 缺失）**：
- Drift detection（漂移检测）
- Coding Tool Gate（编码工具门控）
- NeedsConfirm gate（确认门控）
- Capability gap detection（能力缺口检测）
- Task execution orchestrator（逐任务调度）
- Pending ask_user state tracking
- 流式过滤器（Browser: 前缀剥离等）
- finish_reason=length 截断检测
- 连续空响应 hard exit
- 5xx 网关错误友好提示
- Adaptive retry
- 确认面板（execution confirmation）
- 跨轮次漂移记忆

每次在 GUI 加新功能，TUI 都不会自动获得。这不是 workaround 能解决的——**机制上，两份代码就意味着两倍维护量和永远不同步**。

### 3.3 Hub Agent（独立 workflow engine）

Hub 的 `Coordinator` 在消息到达设备之前做了大量 agent 逻辑：

```
IM 平台 → Hub Adapter.HandleMessage()
  → slash commands (/call, /discuss, /workflow, /cancel, ...)
  → Coordinator.Coordinate()
    → SpaceState 判断 (Private/Meeting/Workflow/Lobby)
    → SpaceWorkflow → WorkflowEngine.HandleWorkflowInput()
      → detectOffTopic()
      → isAdvanceTrigger() / isSkipTrigger() / isCancelTrigger()
      → executePhase() → Hub 自己调 LLM 生成文档
      → executeDevicePhase() → 委托设备执行
    → SpaceLobby → classifyAndRoute()
      → IntentClassifier.Classify() → Hub 调 LLM 做意图分类
      → IntentDirectAnswer → hubDirectAnswer() → Hub 自己调 LLM 回答
      → IntentRouteSingle → routeToSingleMachine() → 转发给设备
```

**Hub 做了不该做的事**：
1. **Hub 自己调 LLM 生成工作流阶段文档**（`executePhase`）——这应该是设备 agent 的工作
2. **Hub 自己调 LLM 做意图分类**（`IntentClassifier.Classify`）——设备侧已有更完善的 UIC
3. **Hub 自己调 LLM 直接回答**（`hubDirectAnswer`）——设备 agent 完全能做
4. **Hub 维护独立的 workflow state**——与设备侧的 workflow engine 状态不同步
5. **Hub 只有 4 个模板**——设备侧有 19 个，永远不同步

**Hub 应该做的事**（统一后保留）：
1. 用户身份映射（IM 平台 UID → 统一 UserID）
2. 设备发现和选择（多设备时选哪台）
3. Slash commands 处理（/call, /discuss, /machines, /help 等）
4. 消息转发（WebSocket 代理）
5. 进度中继（Agent progress → IM 平台）
6. 响应交付（Agent response → IM 平台，含文件/图片）
7. 任务队列（TaskDispatcher，排队等待设备处理）
8. 讨论模式（多设备 AI-to-AI discussion）
9. 设备离线提示

## 4. 统一方案

### 4.1 TUI → IMMessageHandler（Phase 1）

**机制**：TUI 不再有自己的 agent loop，改为构造 `IMUserMessage{Platform: "tui"}` 调用 `IMMessageHandler`。

**适配层**：新增 `tui/agent_adapter.go`，职责是 TUI I/O 模型与 IMMessageHandler 接口之间的桥接。

```go
// tui/agent_adapter.go

// TUIAgentAdapter bridges TUI's Bubble Tea I/O model to IMMessageHandler.
// It does NOT contain any agent logic — all agent logic lives in IMMessageHandler.
type TUIAgentAdapter struct {
    handler  *gui.IMMessageHandler
    streamCb func(msgType, toolName, content string)
}

func (a *TUIAgentAdapter) RunMessage(text string) gui.IMAgentResponse {
    msg := gui.IMUserMessage{
        UserID:   "tui-user",
        Platform: "tui",
    }
        Text:     text,
    }

    onProgress := func(progressText string) {
        a.streamCb("progress", "", progressText)
    }
    onToken := func(delta string) {
        a.streamCb("token", "", delta)
    }

    resp := a.handler.HandleIMMessageWithProgressAndStream(msg, onProgress, onToken, nil, nil)
    return *resp
}
```

**需要解决的问题**：

| 问题 | 机制性解决方案 |
|------|---------------|
| TUI 对话历史格式不同（`[]map[string]string` vs `conversationEntry`） | IMMessageHandler 已有 `conversationMemory` 管理历史，TUI 不再自己管理。TUI 的 `memoryshot` 持久化改为读写 `conversationMemory` 的数据。 |
| TUI 没有 `App` 实例 | 新增 `IMMessageHandler` 的轻量构造路径：`NewIMMessageHandlerStandalone(opts)`，不依赖 `gui.App`，接受 `memory.Store`、`tool.Router`、`workflow.WorkflowEngine` 等组件作为参数。 |
| TUI 的 stream callback 模型不同 | `onToken` 回调已支持（桌面面板就是这么用的）。TUI 的 `SetStreamCallback` 映射到 `onToken` + `onProgress`。 |
| TUI 工具执行需要 TUI 特有的 session manager | `IMMessageHandler` 的工具执行通过 `ToolRegistry` 分发。TUI 初始化时注册 TUI 版本的 session 工具到同一个 registry。 |
| TUI 的安全策略不同（critical 默认拒绝） | `IMMessageHandler` 已有 `Firewall` 接口。TUI 传入自己的 `Firewall` 实例（`onAsk` 回调拒绝 critical）。 |

**删除的代码**：
- `tui/agent_handler.go` 中的 `RunAgentLoop`、`buildBuiltinToolDefinitions`、`dispatchTool`、`executeTool`、`buildSystemPrompt*`
- `tui/agent_tools.go` 中所有工具 handler（bash、read_file、write_file 等）
- `tui/agent_tools_ssh.go`、`agent_tools_office.go`、`agent_tools_missing.go` 整个文件
- `tui/agent_handler_workflow.go` 中的 TUI workflow 拦截
- `tui/agent_handler_nudge.go`、`agent_handler_outcome.go`、`agent_compress.go`

**保留的代码**：
- `tui/app.go`：TUI 应用框架（Bubble Tea）、初始化逻辑
- `tui/agent_adapter.go`（新增）：I/O 桥接层
- `tui/session_manager.go`：TUI 特有的 PTY session 管理
- `tui/views/`：UI 组件
- `tui/config_watcher.go`、`tui/logger.go` 等基础设施

**`IMMessageHandler` 需要的改造**：

1. **`NewIMMessageHandlerStandalone()`**：不依赖 `gui.App` 的构造函数

```go
// gui/im_message_handler_standalone.go

type StandaloneConfig struct {
    MemoryStore     *memory.Store
    ToolRouter      *tool.Router
    WorkflowEngine  *workflow.WorkflowEngine
    SteeringStore   *steering.Store
    UsageTracker    *tool.UsageTracker
    Firewall        *security.Firewall
    SSHManager      *remote.SSHSessionManager
    SessionManager  SessionManager  // interface，TUI 和 GUI 各自实现
    ToolRegistry    *ToolRegistry
    // ... 其他可选组件
}

func NewIMMessageHandlerStandalone(cfg StandaloneConfig) *IMMessageHandler {
    h := &IMMessageHandler{
        memory:         newConversationMemoryWithStore(cfg.MemoryStore),
        toolRouter:     cfg.ToolRouter,
        // ... 接线
    }
    // 不依赖 h.app，所有通过 h.app 访问的组件改为直接字段
    return h
}
```

2. **消除 `h.app` 硬依赖**：当前 `runAgentLoop` 中大量 `h.app.xxx` 调用。需要将这些依赖提取为接口或直接字段：

```go
// 当前（硬依赖 App）：
if h.app != nil && h.app.workflowEngine != nil {
    tools = h.applyWorkflowToolFilter(userID, tools)
}

// 改造后（接口依赖）：
if h.workflowEngine != nil {
    tools = h.applyWorkflowToolFilter(userID, tools)
}
```

这是最大的工作量——需要逐个排查 `h.app.` 引用，将其提取为 `IMMessageHandler` 的直接字段。

**✅ 已完成**：`h.app` 依赖从 ~160 处降到 ~29 处（GUI 特有功能）。新增 `gui/im_app_accessors.go`（35+ 访问器）和 `gui/im_handler_standalone.go`（`NewIMMessageHandlerStandalone`）。

### 4.1.1 Package 边界问题（关键阻塞）

`IMMessageHandler` 在 `gui/`（`package main`），TUI 在 `tui/`（`package main`）。Go 不允许两个 `main` 包互相导入。

**解决方案**：将 `IMMessageHandler` 及其核心依赖提取到 `corelib/agent/` 库包。

需要迁移的核心类型：
- `IMMessageHandler` → `agent.Handler`
- `IMUserMessage` → `agent.UserMessage`
- `IMAgentResponse` → `agent.Response`
- `StandaloneConfig` → `agent.Config`
- `conversationMemory` → `agent.ConversationMemory`
- `LoopContext` → 已在 `corelib/agent/loop_context.go`
- 访问器方法 → `agent.Handler` 的方法

保留在 `gui/` 的（GUI 特有）：
- `registerNonCodeTools`（Git 工具，依赖 `*App`）
- `registerBrowserTools`（浏览器自动化，依赖 `*App`）
- `GUIWorkflowAdapter`（Wails 事件发射）
- `App` 构造路径（`NewIMMessageHandler` 保留为 `gui/` 的 wrapper）

迁移策略：
1. 在 `corelib/agent/` 定义 `Handler` 结构体和 `Config`
2. 将 `runAgentLoop`、`handleIMMessageWithLoop`、工具执行等核心逻辑迁移
3. `gui/` 的 `NewIMMessageHandler` 改为构造 `agent.Handler` + 注册 GUI 特有工具
4. `tui/` 的 `sendAgentMessage` 改为构造 `agent.Handler` + 调用 `HandleMessage`

这是一个大的重构，但它是机制性的——一次迁移后，所有平台共享同一份代码，不再有同步问题。

### 4.1.2 Package 迁移实施路径

`gui/` 和 `tui/` 都是 `package main`，Go 不允许互相导入。`NewIMMessageHandlerStandalone` 虽然不依赖 `*App`，但它在 `gui/`（`package main`）中，TUI 无法调用。

**唯一的机制性解决方案**：将 `IMMessageHandler` 核心迁移到 `corelib/agent/` 库包。

**已完成的准备工作**：
- `corelib/agent/message.go`：定义了 `UserMessage`、`Response`、`MessageAttachment`、回调类型
- `gui/im_app_accessors.go`：35+ 访问器方法，将 `h.app` 依赖抽象为可替换的访问层
- `gui/im_handler_standalone.go`：`NewIMMessageHandlerStandalone`，证明了 handler 可以脱离 `*App` 构造

**迁移步骤**（按文件）：

| 步骤 | 从 | 到 | 内容 |
|------|-----|-----|------|
| 1 | `gui/im_message_handler.go` | `corelib/agent/handler.go` | `Handler` 结构体（原 `IMMessageHandler`）、`HandleMessage`、`runAgentLoop` |
| 2 | `gui/im_app_accessors.go` | `corelib/agent/handler_accessors.go` | 访问器方法（改为接口回调） |
| 3 | `gui/im_handler_standalone.go` | `corelib/agent/handler_standalone.go` | `NewHandlerStandalone` |
| 4 | `gui/im_conversation_memory.go` | `corelib/agent/conversation_memory.go` | 对话历史管理 |
| 5 | `gui/im_system_prompt.go` | `corelib/agent/system_prompt.go` | System prompt 构建 |
| 6 | `gui/im_tool_execution.go` | `corelib/agent/tool_execution.go` | 工具执行分发 |
| 7 | `gui/im_message_handler_workflow.go` | `corelib/agent/handler_workflow.go` | 工作流拦截 |
| 8 | `gui/im_llm_client.go` | `corelib/agent/llm_client.go` | LLM API 调用 |

**GUI 保留的 wrapper**：
```go
// gui/im_message_handler.go (迁移后)
package main

import "github.com/RapidAI/CodeClaw/corelib/agent"

// IMMessageHandler wraps agent.Handler with GUI-specific extensions.
type IMMessageHandler struct {
    *agent.Handler
    app *App // GUI-specific, for registerNonCodeTools/registerBrowserTools
}

// NewIMMessageHandler creates a GUI-mode handler with App integration.
func NewIMMessageHandler(app *App, manager *RemoteSessionManager) *IMMessageHandler {
    h := &IMMessageHandler{
        Handler: agent.NewHandlerStandalone(agent.Config{
            WorkflowEngine: app.workflowEngine,
            LLMConfigFunc:  app.GetMaclawLLMConfig,
            // ...
        }),
        app: app,
    }
    // Register GUI-specific tools (Git, browser, non-code).
    registerNonCodeTools(h.Handler.Registry(), app)
    registerBrowserTools(h.Handler.Registry(), app)
    return h
}
```

**TUI 的调用**：
```go
// tui/app.go (迁移后)
package main

import "github.com/RapidAI/CodeClaw/corelib/agent"

func (a *TUIApp) initAgentHandler() {
    a.agentHandler = agent.NewHandlerStandalone(agent.Config{
        WorkflowEngine: a.workflowEngine,
        LLMConfigFunc: func() corelib.MaclawLLMConfig {
            cfg, _ := commands.LoadLLMConfig()
            return cfg
        },
        MemoryStore:   a.memoryStore,
        ToolRouter:    a.router,
        SSHManager:    a.sshManager,
        SteeringStore: a.steeringStore,
    })
}

func (a *TUIApp) sendAgentMessage(text string) tea.Cmd {
    return func() tea.Msg {
        resp := a.agentHandler.HandleMessage(agent.UserMessage{
            UserID:   "tui-user",
            Platform: "tui",
            Text:     text,
        })
        return views.ChatResponseMsg{Text: resp.Text, Error: resp.Error}
    }
}
```

**预估工作量**：迁移 8 个核心文件（~5000 行），更新 GUI wrapper + TUI 调用方。需要处理的主要难点：
- `gui/` 中的类型别名（`MaclawLLMConfig`、`MemoryStore` 等）需要统一到 corelib
- LLM 流式处理（`llm_stream.go`）依赖 GUI 的 HTTP transport 配置
- 工具注册（`registerBuiltinTools`）依赖 `*IMMessageHandler` 的方法，迁移后需要改为 `*agent.Handler`
- 测试文件需要同步迁移

### 4.1.3 已完成的桥接基础设施

在发现 package 边界问题后，已建立以下桥接基础设施：

**`corelib/agent/` 新增文件**：
- `message.go`：`UserMessage`、`Response`、`MessageAttachment`、回调类型（`TokenCallback`、`ProgressCallback` 等）
- `config.go`：`Config` 结构体，包含 handler 构造所需的所有组件（WorkflowEngine、MemoryStore、ToolRouter 等）
- `handler_iface.go`：`Handler` 接口（`HandleMessage`、`HandleMessageWithProgress`、`HandleMessageWithStream`、`Stop`）+ `HandlerFactory` 注册机制

**`gui/` 新增文件**：
- `agent_handler_bridge.go`：将 `IMMessageHandler` 注册为 `agent.Handler` 的实现，包含 `handlerAdapter`（类型转换层）和 `init()` 注册

**`tui/` 新增文件**：
- `agent_adapter.go`：`newUnifiedAgentConfig()` 从 TUI 组件构建 `agent.Config`，`initUnifiedHandler()` 通过 factory 创建 handler

**当前状态**：所有代码编译通过，但 TUI 和 GUI 是独立二进制，`init()` 注册只在 GUI 二进制中生效。TUI 二进制中 `agent.HasHandlerFactory()` 返回 false，回退到现有的 `TUIAgentHandler`。

### 4.1.4 Package 迁移的正确路径

经过实际编码验证，确认 package 边界是硬约束。解决方案必须是以下之一：

**方案 A：将 `IMMessageHandler` 核心迁移到 `corelib/agent/`**
- 优点：机制性解决，一劳永逸
- 缺点：~5000 行代码迁移，涉及 GUI 类型解耦（`ToolRegistry`、`SkillExecutor`、`MCPRegistry` 等 20+ 类型要么一起搬要么变接口）
- 预估：3-5 天集中工作

**方案 B：合并二进制（`maclaw --tui` 模式）**
- 优点：零代码迁移，TUI 直接调用 `NewIMMessageHandlerStandalone`
- 缺点：GUI 二进制增加 bubbletea 依赖（~2MB），TUI CLI 命令也被拉入 GUI 二进制
- 预估：1 天

**方案 C：进程间通信（TUI 通过 Unix socket 调用 GUI 的 handler）**
- 优点：不改 package 结构
- 缺点：增加延迟、复杂度，流式 token 推送需要额外协议
- 不推荐

**推荐**：方案 A。虽然工作量最大，但它是唯一的机制性解决方案。方案 B 是 workaround（合并二进制不是因为它们应该合并，而是为了绕过 package 限制）。

方案 A 的增量路径：
1. 先迁移类型定义（`IMUserMessage` → `agent.UserMessage` 已完成）
2. 迁移 `conversationMemory`（无外部依赖）
3. 迁移 `runAgentLoop` 核心循环（最大的一步，需要将 GUI 类型依赖改为接口）
4. 迁移工具执行框架（`ToolRegistry`、`executeTool`）
5. 迁移 system prompt 构建
6. GUI 和 TUI 各自注册平台特有的工具和行为

3. **Platform-aware 行为扩展**：在现有的 `platform == "desktop"` 分支旁边加 `platform == "tui"` 分支：

```go
// TUI 不需要 doc preview panel
if platform == "desktop" {
    // emit doc preview event
} else if platform == "tui" {
    // TUI 不需要 doc preview，跳过
}

// TUI 不需要 PDF
if platform == "desktop" {
    systemPrompt += desktopWorkflowDocOverride()
} else if platform == "tui" {
    systemPrompt += desktopWorkflowDocOverride() // TUI 也用 Markdown，不用 PDF
} else if platform != "" {
    systemPrompt += imWorkflowDocDeliveryRule()
}
```

### 4.2 Hub → 纯消息代理（Phase 2）

**机制**：Hub 不再有自己的 workflow engine、intent understanding、LLM 调用。所有 agent 逻辑由设备侧的 `IMMessageHandler` 处理。

**Hub 保留的功能**（纯代理层）：

```
IM 平台 → Hub Adapter.HandleMessage()
  → 身份映射（IM UID → 统一 UserID）
  → 速率限制
  → 去重
  → Slash commands（/call, /machines, /help, /cancel, /discuss, /stop, /queue）
  → 设备发现和选择
  → 消息转发（→ WebSocket → 设备 IMMessageHandler）
  → 进度中继（设备 → IM 平台）
  → 响应交付（设备 → IM 平台）
  → 任务队列（设备忙时排队）
  → 讨论模式（多设备 AI-to-AI）
  → 设备离线提示
```

**Hub 删除的功能**：

| 删除的组件 | 原因 |
|-----------|------|
| `workflow_engine.go` | 设备侧 `corelib/workflow` 是完整实现（19 模板、三层语义、质量门禁） |
| `workflow_types.go` | 被 `corelib/workflow/types.go` 替代 |
| `workflow_templates.go` | 被 `corelib/workflow/templates.go` 替代（4 模板 vs 19 模板） |
| `workflow_registry.go` | 被 `corelib/workflow/registry.go` 替代 |
| `intent_understanding.go` | 被 `corelib/workflow/intent_understanding.go` 替代 |
| `quick_filter.go` | 被 `corelib/workflow/quick_filter.go` 替代（缺少三层语义保底） |
| `intent_classifier.go` | 被 `corelib/intent/classifier.go` (UIC) 替代 |
| `hub_llm_config.go` 中的 LLM 调用 | Hub 不再直接调 LLM |

**Coordinator 改造**：

```go
// 改造前：Coordinator 自己做意图分类和工作流管理
func (c *Coordinator) Coordinate(...) {
    switch state.State {
    case SpaceWorkflow:
        return c.workflowEngine.HandleWorkflowInput(...)  // Hub 自己处理
    default:
        return c.classifyAndRoute(...)  // Hub 调 LLM 分类
    }
}

// 改造后：Coordinator 只做空间状态管理和消息路由
func (c *Coordinator) Coordinate(...) {
    switch state.State {
    case SpaceMeeting:
        return c.handleMeetingMessage(...)  // 讨论模式保留
    default:
        // 所有消息直接转发给设备，设备侧 IMMessageHandler 处理一切
        return c.router.RouteToAgent(...)
    }
}
```

**SpaceWorkflow 状态的处理**：

当前 Hub 有 `SpaceWorkflow` 状态，用于在 Hub 侧管理工作流。统一后：
- 删除 `SpaceWorkflow` 状态
- 工作流完全由设备侧管理
- Hub 的 `/workflow` 命令改为转发给设备（设备返回工作流状态）
- Hub 的 `/workflow cancel` 改为转发给设备（设备取消工作流）

**设备离线时的处理**：

当前 Hub 在设备离线时可以用自己的 LLM 做意图理解和工作流启动。统一后：
- 设备离线 → Hub 返回"设备不在线"提示（已有逻辑）
- 不再尝试用 Hub LLM 替代设备 agent
- 这是正确的行为——Hub 的 LLM 能力远不如设备侧（没有工具、没有记忆、没有完整 workflow engine）

**`hubDirectAnswer` 的处理**：

当前 Hub 对简单问题（闲聊、问候）直接用 LLM 回答，不转发给设备。统一后：
- 删除 `hubDirectAnswer`
- 所有消息转发给设备
- 设备侧的 `isShortChitChatMessage` 已经能快速处理闲聊（<100ms，不调 LLM）
- 设备侧处理闲聊的延迟 = WebSocket 往返（~50ms）+ 设备处理（~100ms）≈ 150ms，可接受

### 4.3 /workflow 命令的设备侧处理

Hub 的 `/workflow`、`/workflow cancel`、`/workflow skip` 命令当前由 Hub 的 workflow engine 处理。统一后需要设备侧支持这些命令。

**方案**：在 `IMUserMessage` 中新增 `SlashCommand` 字段，Hub 解析后转发：

```go
// Hub 侧：
if strings.HasPrefix(text, "/workflow") {
    subCmd := parseWorkflowSubCommand(text)
    msg := IMUserMessage{
        UserID:       unifiedID,
        Platform:     platformName,
        Text:         text,
        SlashCommand: "workflow:" + subCmd,  // "workflow:status" / "workflow:cancel" / "workflow:skip"
    }
    return router.RouteToAgent(msg)
}

// 设备侧 IMMessageHandler：
if msg.SlashCommand != "" {
    return h.handleSlashCommand(msg)
}
```

设备侧已有 `/new`、`/reset`、`/clear` 等 slash command 处理，`/workflow` 只是新增一个 case。

## 5. 实施计划

### Phase 1: TUI 统一到 IMMessageHandler

**前置条件**：无

**步骤**：

1. **提取 `h.app` 依赖为接口/直接字段**（最大工作量）
   - 扫描 `runAgentLoop` 中所有 `h.app.` 引用
   - 将 `workflowEngine`、`skillExecutor`、`unifiedClassifier` 等提取为 `IMMessageHandler` 的直接字段
   - `gui.App` 构造 `IMMessageHandler` 时接线（行为不变）
   - TUI 构造 `IMMessageHandler` 时传入自己的组件实例

2. **新增 `NewIMMessageHandlerStandalone()`**
   - 不依赖 `gui.App` 的构造函数
   - 接受 `StandaloneConfig` 参数

3. **新增 `tui/agent_adapter.go`**
   - 桥接 Bubble Tea I/O 到 `IMMessageHandler`
   - 映射 stream callback

4. **TUI `initKernel()` 改造**
   - 不再创建 `TUIAgentHandler`
   - 创建 `IMMessageHandler`（通过 `NewIMMessageHandlerStandalone`）
   - 创建 `TUIAgentAdapter` 包装

5. **TUI `sendAgentMessage()` 改造**
   - 调用 `adapter.RunMessage(text)` 而非 `handler.RunAgentLoop(text, conversation)`
   - 不再自己管理对话历史（`IMMessageHandler` 管理）

6. **删除 TUI 独立 agent 代码**
   - 删除 `agent_handler.go` 中的 RunAgentLoop、buildBuiltinToolDefinitions、dispatchTool
   - 删除 `agent_tools*.go` 中所有工具 handler
   - 删除 `agent_handler_workflow.go`、`agent_handler_nudge.go`、`agent_handler_outcome.go`、`agent_compress.go`

7. **验证**
   - TUI 所有现有功能正常工作
   - TUI 自动获得 GUI 的所有功能（drift detection、coding gate、NeedsConfirm 等）
   - 编译通过，现有测试通过

### Phase 2: Hub 退化为纯消息代理

**前置条件**：Phase 1 完成（确保设备侧 agent 能力完整）

**步骤**：

1. **设备侧新增 `/workflow` slash command 处理**
   - `IMMessageHandler` 处理 `SlashCommand: "workflow:*"`
   - 返回工作流状态 / 执行取消 / 执行跳过

2. **Hub `IMUserMessage` 新增 `SlashCommand` 字段**
   - Hub 解析 `/workflow` 命令后设置此字段
   - 通过 WebSocket 转发给设备

3. **Hub Coordinator 简化**
   - 删除 `SpaceWorkflow` 状态
   - 删除 `classifyAndRoute` 中的 LLM 调用
   - 删除 `hubDirectAnswer`
   - 所有非 slash-command、非 discussion 的消息直接 `RouteToAgent`

4. **删除 Hub 独立 agent 代码**
   - 删除 `workflow_engine.go`
   - 删除 `workflow_types.go`、`workflow_templates.go`、`workflow_registry.go`
   - 删除 `intent_understanding.go`
   - 删除 `quick_filter.go`（Hub 不再需要消息分类）
   - 删除 `intent_classifier.go`
   - 删除 `hub_llm_config.go` 中的 LLM 调用相关代码

5. **验证**
   - IM 通道所有现有功能正常工作
   - 工作流通过设备侧完整执行（19 个模板全部可用）
   - `/workflow` 命令正常工作
   - 设备离线时返回友好提示
   - 讨论模式不受影响

## 6. 风险和缓解

### 风险 1: `h.app` 依赖提取工作量大

`runAgentLoop` 有 ~2000 行，其中大量 `h.app.xxx` 调用。逐个提取可能引入回归。

**缓解**：
- 先做机械性提取（`h.app.workflowEngine` → `h.workflowEngine`），不改逻辑
- 每提取一个依赖就跑一次测试
- GUI 侧行为完全不变（`App` 构造时接线到同样的字段）

### 风险 2: TUI 对话历史迁移

TUI 当前用 `memoryshot` 管理对话历史，GUI 用 `conversationMemory`。统一后 TUI 需要适配 `conversationMemory`。

**缓解**：
- `conversationMemory` 已支持多用户（按 userID 隔离）
- TUI 用 `userID="tui-user"`，与桌面的 `"desktop-user"` 隔离
- `memoryshot` 的持久化功能可以作为 `conversationMemory` 的后端

### 风险 3: Hub 删除 LLM 能力后，设备离线体验下降

当前 Hub 在设备离线时可以用自己的 LLM 做简单回答。

**缓解**：
- Hub 的 LLM 能力本来就很弱（没有工具、没有记忆、没有完整 workflow）
- 设备离线时返回"设备不在线"是正确行为
- 如果未来需要 Hub 侧 fallback，可以加一个极简的闲聊回复（不需要完整 agent）

### 风险 4: Hub 讨论模式依赖 Coordinator 的 LLM 分类

`classifyAndRoute` 中的 `IntentDiscuss` 分支用 LLM 判断用户是否想发起讨论。

**缓解**：
- 讨论模式已有显式命令 `/discuss`
- `IntentDiscuss` 的自动检测可以改为关键词匹配（"讨论"、"辩论"、"让它们聊聊"等）
- 不需要 LLM 做这个判断

## 7. 统一后的收益

### 7.1 维护量减少

| 改进类型 | 统一前 | 统一后 |
|---------|--------|--------|
| 新增工具 | GUI + TUI 各写一遍 handler + definition | 写一遍 |
| Workflow 新模板 | corelib + Hub 各注册一遍 | 写一遍 |
| Bug 修复 | GUI + TUI + Hub 各修一遍 | 修一遍 |
| 新增 agent 功能 | GUI 实现，TUI 永远缺失 | 自动全平台生效 |

### 7.2 功能一致性

TUI 立即获得 GUI 的所有功能：
- Drift detection + 跨轮次漂移记忆
- Coding Tool Gate + bug fix bypass
- NeedsConfirm gate（阶段感知）
- Capability gap detection
- Task execution orchestrator
- 流式过滤器（Browser: 前缀剥离）
- finish_reason=length 截断检测
- 连续空响应 hard exit
- 5xx 网关错误友好提示
- Adaptive retry
- 确认面板
- UIC 预检

### 7.3 Hub 简化

Hub 从"半个 agent"退化为纯消息代理：
- 不再需要配置 LLM
- 不再有 workflow 状态同步问题
- 不再有模板数量不一致问题
- 代码量减少 ~2000 行

## 8. 不做的事

1. **不统一 GUI 和 TUI 的 UI 层**——Wails 和 Bubble Tea 是完全不同的 UI 框架，UI 层保持独立
2. **不统一对话历史的持久化格式**——`conversationMemory` 内部格式是实现细节，TUI 只需要通过接口访问
3. **不删除 Hub 的讨论模式**——多设备 AI-to-AI 讨论是 Hub 的独有功能，不涉及 agent 逻辑重复
4. **不删除 Hub 的 slash commands**——这些是 Hub 层面的路由控制，不是 agent 逻辑
5. **不在 Phase 1 改 Hub**——先确保 TUI 统一成功，再动 Hub
