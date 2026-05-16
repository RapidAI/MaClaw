# 实现计划：飞书自然语言远程工具控制（Agent Passthrough 架构）

## 概述

本实现计划将设计文档中的 Agent Passthrough 架构分解为增量式编码任务。核心变更：Hub 不再做意图解析（移除 NL_Router + BridgeExecutor 管线），改为纯消息路由；所有自然语言理解和工具调用由 MaClaw 客户端的 Agent（LLM）完成。MCP_Registry 和 Skill 系统迁移到客户端侧。

## Tasks

- [x] 1. Hub 侧：实现 MessageRouter 替代 NL_Router + BridgeExecutor
  - [x] 1.1 创建 MessageRouter (`hub/internal/im/router.go`)
    - 定义 `MessageRouter` 结构体，包含 `DeviceBinder`（查找用户在线设备）、`rateLimiter`、`IdentityResolver`、`pendingReqs map[string]*PendingIMRequest`
    - 定义 `PendingIMRequest` 结构体（RequestID、UserID、PlatformUID、Text、ResponseCh chan *AgentResponse、CreatedAt、Timeout）
    - 实现 `RouteToAgent(ctx, msg IncomingMessage) (*GenericResponse, error)` 方法：
      1. 身份映射（platformUID → userID）
      2. 速率限制（复用现有 rateLimiter）
      3. 查找用户的在线设备（通过 DeviceBinder）
      4. 检查设备 LLM 配置状态
      5. 生成 request_id，创建 PendingIMRequest 并注册到 pendingReqs
      6. 通过 WebSocket 发送 `im.user_message` 到 MaClaw 客户端
      7. 等待 `im.agent_response`（带 120 秒超时）
      8. 转换 AgentResponse 为 GenericResponse 返回
    - 实现 `HandleAgentResponse(requestID string, resp *AgentResponse)` 方法：收到 Agent 回复时，查找 pendingReqs 并通过 ResponseCh 发送
    - 实现超时清理：定期清理过期的 PendingIMRequest
    - _需求: 1.1, 1.2, 1.3, 1.5, 1.6, 1.7, 1.8_

  - [x] 1.2 定义 WebSocket 协议消息类型
    - 定义 `IMUserMessage` 结构体（Type="im.user_message"、RequestID、UserID、Platform、Text、Timestamp）
    - 定义 `IMAgentResponse` 结构体（Type="im.agent_response"、RequestID、Response AgentResponse）
    - 定义 `AgentResponse` 结构体（Text、Fields []ResponseField、Actions []ResponseAction、ImageKey、Error）
    - 这些类型可放在 `hub/internal/im/router.go` 或独立的 `hub/internal/im/agent_types.go`
    - _需求: 1.2, 1.3, 7.1, 7.2_

  - [x] 1.3 在 WebSocket Gateway 中添加 `im.agent_response` 处理 (`hub/internal/ws/handlers_machine.go`)
    - 在 `HandleWS` 的 switch 中添加 `case "im.agent_response"` 分支
    - 实现 `handleIMAgentResponse(ctx *ConnContext, msg Envelope)` 方法：
      1. 解析 `IMAgentResponse` payload
      2. 调用 `MessageRouter.HandleAgentResponse(requestID, &resp)` 将回复路由回等待的请求
    - _需求: 1.3_

  - [x]* 1.4 编写 MessageRouter 单元测试
    - 测试正常消息路由流程（发送 → 等待 → 收到回复）
    - 测试 120 秒超时返回超时提示
    - 测试设备离线时返回离线提示
    - 测试 LLM 未配置时返回配置提示
    - 测试速率限制
    - _需求: 1.1, 1.6, 1.7, 1.8, 6.2_

- [x] 2. Hub 侧：重构 IM Adapter Core 使用 MessageRouter (`hub/internal/im/core.go`)
  - [x] 2.1 修改 Adapter 结构体
    - 移除 `router IntentRouter` 和 `executor IntentExecutor` 字段
    - 添加 `messageRouter *MessageRouter` 字段
    - 修改 `NewAdapter` 构造函数签名，接收 `*MessageRouter` 而非 `IntentRouter` + `IntentExecutor`
    - _需求: 1.1_

  - [x] 2.2 简化 HandleMessage 管线
    - 移除步骤 3（pending confirmation check）— Agent 自行处理确认
    - 移除步骤 4（injection detection）— Agent 侧做输入校验
    - 移除步骤 5（NL Router intent parsing）— 不再做意图解析
    - 移除步骤 6（high-risk confirmation prompt）— Agent 自行处理
    - 替换步骤 7（intent execution）为：调用 `messageRouter.RouteToAgent(ctx, msg)`
    - 保留步骤 1（身份映射）、步骤 2（速率限制）、步骤 8（响应格式化）
    - 保留邮箱绑定流程作为前置拦截（未绑定用户只能执行绑定）
    - _需求: 1.1, 6.1, 6.5_

  - [x] 2.3 清理不再需要的代码
    - 移除 `IntentRouter` 和 `IntentExecutor` 接口定义（从 core.go）
    - 移除 `PendingConfirmation` 结构体和相关方法（requestConfirmation、handleConfirmationReply）
    - 移除 `isHighRiskIntent` 函数
    - 移除 `containsInjection` 函数和 `shellMetaChars`
    - 注意：速率限制器（rateLimiter）保留，移到 MessageRouter 中或保留在 Adapter 中
    - _需求: 1.1_

  - [x]* 2.4 编写重构后的 IM Adapter 单元测试
    - 测试消息通过 MessageRouter 路由到 Agent
    - 测试身份映射失败时的错误响应
    - 测试速率限制
    - 测试未绑定用户只能执行绑定流程
    - _需求: 1.1, 6.1, 6.2_

- [x] 3. 检查点 — 确保 Hub 侧编译通过并测试通过
  - 运行 `go build ./hub/...` 确保编译通过
  - 运行 `go test ./hub/internal/im/...` 确保测试通过
  - 如有问题请向用户确认

- [x] 4. Hub 侧：更新 Bootstrap 和服务注册 (`hub/internal/app/bootstrap.go`)
  - [x] 4.1 修改 Bootstrap 函数
    - 移除 NL_Router、BridgeExecutor、ContextWindowManager 的初始化
    - 创建 `MessageRouter`，注入 `deviceService`（作为 DeviceBinder）和 `identityService`
    - 修改 `im.NewAdapter` 调用，传入 `MessageRouter` 而非 `nlRouter` + `bridgeExecutor`
    - 将 `MessageRouter` 注入到 WebSocket Gateway，使 Gateway 能将 `im.agent_response` 路由到 MessageRouter
    - _需求: 1.1, 1.3_

  - [x] 4.2 清理 Bootstrap 中不再需要的模块
    - 移除 `memoryStore` 初始化（Memory_Store 不再在 Hub 侧使用）
    - 移除 `discoveryProtocol` 初始化（Tool_Discovery_Protocol 迁移到客户端侧）
    - 移除 `mcpRegistry` 初始化（MCP_Registry 迁移到客户端侧）
    - 移除 `skillExecutor` 和 `skillCrystallizer` 初始化（Skill 系统迁移到客户端侧）
    - 移除 `contextWindowMgr` 初始化
    - 移除 `ruleEngine` 和 `nlRouter` 初始化
    - 移除 `bridgeExecutor` 初始化
    - 更新 `App` 结构体，移除对应字段
    - 更新 `httpapi.NewRouter` 调用，移除不再需要的参数（skillExecutor、skillCrystallizer、mcpRegistry）
    - _需求: 1.1_

- [x] 5. 检查点 — 确保 Hub 侧完整编译和测试通过
  - 运行 `go build ./hub/...`
  - 运行 `go test ./hub/...`
  - 如有问题请向用户确认

- [x] 6. MaClaw 客户端：实现 IMMessageHandler
  - [x] 6.1 创建 IMMessageHandler (`im_message_handler.go`)
    - 定义 `IMMessageHandler` 结构体，包含 `app *App`、`manager *RemoteSessionManager`
    - 实现 `HandleIMMessage(msg IMUserMessage) *AgentResponse` 方法：
      1. 检查 LLM 配置（调用 `app.isMaclawLLMConfigured()`）
      2. 构建 system prompt（注入设备状态、活跃会话列表、可用工具列表）
      3. 调用 LLM（OpenAI-compatible API，使用 `app_maclaw_llm.go` 中的配置）
      4. 如果 LLM 返回 tool_call，执行对应工具
      5. 将结果封装为 AgentResponse 返回
    - 定义 Agent 可调用的工具列表：list_sessions、create_session、send_input、interrupt_session、kill_session、screenshot
    - 实现工具调用分发逻辑，调用 `RemoteSessionManager` 的对应方法
    - _需求: 2.1, 2.2, 2.3, 2.4, 2.5, 2.6, 2.7_

  - [x] 6.2 实现 Agent System Prompt 构建
    - 实现 `buildSystemPrompt()` 方法，注入：
      - 设备名称、平台信息
      - 当前活跃会话列表（ID、工具名、标题、状态）
      - 可用工具定义（名称、描述、参数 schema）
      - 中文回复指令
    - _需求: 2.2, 2.6_

  - [x] 6.3 实现 Agent 工具执行
    - 实现 `executeTool(name string, args map[string]interface{}) (string, error)` 方法
    - list_sessions: 调用 `manager.List()` 返回会话列表
    - create_session: 调用 `app.StartRemoteSessionForProject()` 创建会话
    - send_input: 调用 `manager.WriteInput()` 发送输入
    - interrupt_session: 调用 `manager.Interrupt()` 中断会话
    - kill_session: 调用 `manager.Kill()` 终止会话
    - screenshot: 调用 `manager.CaptureScreenshot()` 截屏
    - _需求: 3.1, 3.2, 3.3, 3.4, 3.5, 3.6_

  - [ ]* 6.4 编写 IMMessageHandler 单元测试
    - 测试 system prompt 构建包含正确的上下文信息
    - 测试工具调用分发逻辑
    - 测试 LLM 未配置时的错误处理
    - _需求: 2.1, 2.2, 2.7_

- [x] 7. MaClaw 客户端：在 RemoteHubClient 中集成 IM 消息处理 (`remote_hub_client.go`)
  - [x] 7.1 添加 `im.user_message` 处理
    - 在 `readLoop` 的 switch 中添加 `case "im.user_message"` 分支
    - 实现 `handleIMUserMessage(msg inboundHubEnvelope)` 方法：
      1. 解析 `IMUserMessage` payload
      2. 调用 `IMMessageHandler.HandleIMMessage(msg)` 获取 AgentResponse
      3. 通过 WebSocket 发送 `im.agent_response` 回 Hub
    - 注意：Agent 处理可能耗时较长，应在 goroutine 中执行以避免阻塞 readLoop
    - _需求: 1.2, 1.3, 2.1_

  - [x] 7.2 实现 `sendIMAgentResponse` 方法
    - 构建 `HubEnvelope{Type: "im.agent_response", RequestID: ..., Payload: AgentResponse}`
    - 通过 WebSocket 发送到 Hub
    - _需求: 1.3_

  - [x] 7.3 初始化 IMMessageHandler
    - 在 `NewRemoteHubClient` 或 `Connect` 中创建 `IMMessageHandler` 实例
    - 注入 `app` 和 `manager` 依赖
    - _需求: 2.1_

- [x] 8. 检查点 — 确保客户端侧编译通过
  - 运行 `go build ./...` 确保整体编译通过
  - 如有问题请向用户确认

- [x] 9. MaClaw 客户端：实现 MCP_Registry（客户端侧）
  - [x] 9.1 创建客户端侧 MCP_Registry (`app_nl_mcp.go` 或新文件)
    - 定义 `MCPRegistry` 结构体，管理本地 MCP Server 注册
    - 实现 `Register`、`Unregister`、`ListServers`、`CallTool`、`HealthCheck` 方法
    - 使用 `app.SaveSetting` / `app.LoadSetting` 持久化（键 `mcp_servers`）
    - 实现 30 秒调用超时
    - 实现健康状态维护（连续 3 次失败标记不可用）
    - _需求: 4.1, 4.2, 4.3, 4.4, 4.5_

  - [x] 9.2 将 MCP 工具注册为 Agent 可调用工具
    - 在 IMMessageHandler 中添加 `list_mcp_tools` 和 `call_mcp_tool` 工具
    - `list_mcp_tools`: 调用 `MCPRegistry.ListServers()` 返回工具列表
    - `call_mcp_tool`: 调用 `MCPRegistry.CallTool()` 执行工具
    - _需求: 4.2, 4.6_

  - [x] 9.3 添加 Wails 绑定函数
    - `ListMCPServers() []MCPServer`
    - `RegisterMCPServer(server MCPServer) error`
    - `UpdateMCPServer(server MCPServer) error`
    - `UnregisterMCPServer(serverID string) error`
    - `GetMCPServerTools(serverID string) []MCPTool`
    - `CheckMCPServerHealth(serverID string) error`
    - _需求: 9.3_

- [x] 10. MaClaw 客户端：实现 Skill 系统（客户端侧）
  - [x] 10.1 创建客户端侧 Skill_Executor (`app_nl_skills.go` 或新文件)
    - 定义 `SkillDefinition`、`SkillStep` 结构体（支持 YAML 解析）
    - 实现 `SkillExecutor` 结构体
    - 实现 `Register`、`Delete`、`List`、`Execute` 方法
    - 使用 `app.SaveSetting` / `app.LoadSetting` 持久化（键 `skills`）
    - 支持步骤中引用 MCP 工具和内置操作
    - 步骤失败时停止执行并报告错误
    - _需求: 5.1, 5.2, 5.3, 5.4, 5.5_

  - [x] 10.2 将 Skill 注册为 Agent 可调用工具
    - 在 IMMessageHandler 中添加 `run_skill` 和 `list_skills` 工具
    - _需求: 5.2, 5.6_

  - [x] 10.3 添加 Wails 绑定函数
    - `ListSkills() []SkillDefinition`
    - `CreateSkill(def SkillDefinition) error`
    - `UpdateSkill(def SkillDefinition) error`
    - `DeleteSkill(name string) error`
    - _需求: 9.2_

- [x] 11. 检查点 — 确保整体编译和测试通过
  - 运行 `go build ./...`
  - 运行 `go test ./...`
  - 如有问题请向用户确认

- [x] 12. 前端管理面板
  - [x] 12.1 创建 MCP_Management_Panel (`frontend/src/components/remote/MCPManagementPanel.tsx`)
    - MCP Server 列表展示（名称、端点 URL、健康状态颜色标识、工具数量）
    - 注册、编辑、删除 MCP Server 表单
    - 工具列表展开显示
    - 健康状态颜色标识（绿/黄/红）
    - 通过 Wails 绑定函数与后端通信
    - _需求: 9.3, 9.5_

  - [x] 12.2 创建 Skills_Management_Panel (`frontend/src/components/remote/SkillsManagementPanel.tsx`)
    - Skill 列表展示（名称、描述、触发短语、状态）
    - 创建、编辑、删除 Skill 表单
    - YAML 步骤编辑区
    - 通过 Wails 绑定函数与后端通信
    - _需求: 9.2_

  - [x] 12.3 在 Settings Panel 中集成新 Tab (`frontend/src/App.tsx`)
    - 添加 "skills"（技能管理）和 "mcp"（MCP 管理）tab
    - 条件渲染对应组件
    - _需求: 9.1_

- [x] 13. 清理 Hub 侧废弃模块
  - [x] 13.1 移除或标记废弃的 Hub 侧模块
    - `hub/internal/nlrouter/` — NL_Router、RuleEngine、ContextWindow（不再使用）
    - `hub/internal/memory/` — Memory_Store（迁移到客户端侧或不再需要）
    - `hub/internal/discovery/` — Tool_Discovery_Protocol（迁移到客户端侧）
    - `hub/internal/mcp/` — MCP_Registry（迁移到客户端侧）
    - `hub/internal/skill/` — Skill_Executor、Skill_Crystallizer（迁移到客户端侧）
    - `hub/internal/im/executor.go` — BridgeExecutor（被 MessageRouter 替代）
    - 注意：如果有其他模块依赖这些包，先确认依赖关系再删除
    - 可以先保留代码但从 bootstrap.go 中移除引用，后续再清理
    - _需求: 1.1_

  - [x] 13.2 更新 HTTP API 路由
    - 移除 Hub 侧 MCP 和 Skill 的 HTTP handler（如果有）
    - 或将其改为代理到客户端侧的 Wails 绑定
    - _需求: 1.1_

- [x] 14. 最终检查点 — 确保所有编译和测试通过
  - 运行 `go build ./...`
  - 运行 `go test ./...`
  - 如有问题请向用户确认

## 备注

- 标记 `*` 的任务为可选测试任务，可跳过以加速 MVP 交付
- 任务 1-5 是 Hub 侧重构，任务 6-11 是客户端侧实现，任务 12 是前端，任务 13 是清理
- 核心路径：任务 1 → 2 → 4 → 6 → 7 是最小可用路径（IM 消息透传到 Agent 并回复）
- MCP 和 Skill（任务 9-10）可在核心路径完成后再实现
- 前端面板（任务 12）可与后端并行开发
- 后端使用 Go，前端使用 React/TypeScript
