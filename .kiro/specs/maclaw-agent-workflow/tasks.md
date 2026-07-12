# 实施计划：MaClaw Agent 工作流引擎

## 概览

基于 `corelib/workflow/` 构建代码级状态机工作流引擎，替代现有 `coding-workflow.md` 纯 prompt 引导方案。采用自底向上的实现策略：先构建核心类型和纯函数组件，再逐步组装引擎，最后集成到 GUI/TUI 和前端。所有后端代码使用 Go，前端使用 TypeScript/React。

## 任务

- [x] 1. 核心类型与基础组件
  - [x] 1.1 创建 `corelib/workflow/types.go`，定义所有核心类型
    - 定义 WorkflowType 常量（coding, product_design, innovation, business_plan, testing）
    - 定义 StructuredIntent、PhaseTemplate、WorkflowTemplate、WorkflowResponse 结构体
    - 定义 WorkflowState、WorkflowStatus、QualityGateResult、GateCheckItem 结构体
    - 定义 ToolFilterPolicy 常量（none, doc_only, full）
    - 定义 EngineCallbacks 接口（SendTextToUser, EmitPhaseUpdate, EmitDocUpdate, EmitGateResult）
    - 定义 FilterResult 常量（small_talk, simple_directive, active_workflow, active_understanding, needs_understanding）
    - 定义 UnderstandingState 常量和 UnderstandingSession、UnderstandingRound 结构体
    - 定义 LLMCaller 接口
    - 定义 PersistenceStore 接口
    - _需求: 1.1-1.6, 2.1-2.8, 3.1-3.5, 5.1-5.9_

  - [x] 1.2 创建 `corelib/workflow/quick_filter.go`，实现 QuickFilter 消息分流器
    - 实现 QuickFilter 结构体，持有 WorkflowEngine 引用
    - 实现 Classify 方法，按优先级执行纯规则分流：active_workflow → active_understanding → small_talk → simple_directive → needs_understanding → 默认 simple_directive
    - 实现 small talk 模式匹配（短消息 + 问候词/时间词/感谢词）
    - 实现简单指令模式匹配（翻译/格式化/总结等）
    - 实现复杂任务特征检测（动词 + 目标对象 + 约束条件）
    - 确保无 I/O 操作，纯内存规则判断
    - _需求: 1.1, 1.2, 1.3, 1.4, 1.5, 1.6_

  - [x]* 1.3 编写 QuickFilter 属性测试 `corelib/workflow/quick_filter_property_test.go`
    - **Property 1: 活跃会话路由优先级** — 有活跃工作流时返回 active_workflow，有活跃意图理解会话时返回 active_understanding
    - **验证: 需求 1.3, 1.4**
    - **Property 2: QuickFilter 分类正确性** — small_talk/simple_directive/needs_understanding 模式匹配正确
    - **验证: 需求 1.1, 1.2, 1.6**
    - **Property 3: QuickFilter 性能保证** — 任意长度消息（0-10000 字符）执行时间 <5ms
    - **验证: 需求 1.5, 13.1**

  - [x]* 1.4 编写 QuickFilter 单元测试 `corelib/workflow/quick_filter_test.go`
    - 测试各分类的边界情况和典型消息
    - 测试空消息、超长消息、纯标点消息
    - _需求: 1.1-1.6_

- [x] 2. 模板注册与内置模板
  - [x] 2.1 创建 `corelib/workflow/registry.go`，实现 WorkflowRegistry
    - 实现 NewWorkflowRegistry 构造函数（自动注册内置模板）
    - 实现 Register 方法（同类型覆盖语义）
    - 实现 Match 方法（按 WorkflowType 精确匹配）
    - 实现 AllDescriptions 方法（返回所有模板摘要文本）
    - 使用 sync.RWMutex 保护并发安全
    - _需求: 3.1, 3.2, 3.3, 3.5_

  - [x] 2.2 创建 `corelib/workflow/templates.go`，定义 5 种内置模板
    - coding 模板：requirements → tech_design → task_breakdown → implementation → review（implementation 阶段 ToolPolicy=full，其余 doc_only）
    - product_design 模板：problem_discovery → solution_design → prd → prototype（全部 doc_only）
    - innovation 模板：opportunity → ideation → validation → roadmap → action_plan（全部 doc_only）
    - business_plan 模板：executive_summary → market_analysis → product_strategy → operations → financial_projection（全部 doc_only）
    - testing 模板：test_strategy → test_design → test_environment → test_execution → defect_report（test_execution 阶段 full，其余 doc_only）
    - 每个阶段包含完整的 Prompt、Deliverable、Checklist、NeedsConfirm、CanSkip 定义
    - _需求: 4.1, 4.2, 4.3, 4.4, 4.5, 4.6, 4.7_

  - [x]* 2.3 编写模板注册属性测试 `corelib/workflow/registry_property_test.go`
    - **Property 5: 模板注册-匹配往返一致性** — Register 后 Match 返回该模板，重复注册覆盖旧模板
    - **验证: 需求 3.2, 3.5**
    - **Property 6: AllDescriptions 完整性** — 返回文本包含每个模板的 Name 和 Description
    - **验证: 需求 3.3**

  - [x]* 2.4 编写内置模板结构验证单元测试 `corelib/workflow/templates_test.go`
    - 验证 5 种模板的阶段数量、阶段 ID、阶段顺序
    - 验证每个阶段的必填字段（ID, Name, Prompt, Checklist）非空
    - 验证 coding 模板 implementation 阶段 ToolPolicy=full
    - _需求: 4.1-4.7_

- [x] 3. 检查点 — 确保核心类型和模板测试通过
  - 确保所有测试通过，ask the user if questions arise.

- [x] 4. PromptBuilder 与持久化层
  - [x] 4.1 创建 `corelib/workflow/prompt_builder.go`，实现阶段 Prompt 构建
    - 实现 BuildPhaseSystemPrompt 函数：拼接阶段名称/描述、LLM 指令、StructuredIntent 摘要、前序阶段产出物摘要、Checklist
    - 实现 BuildQualityGatePrompt 函数：构建质量门禁检查 prompt
    - 实现 GetToolFilterForPhase 函数：返回阶段的工具过滤策略
    - _需求: 5.2, 6.1, 6.2, 6.3, 6.4_

  - [x]* 4.2 编写 PromptBuilder 属性测试
    - **Property 8: BuildPhasePrompt 结构完整性** — 输出包含阶段名称、LLM 指令、Intent 摘要、前序产出物、Checklist 每一项
    - **验证: 需求 5.2, 6.1, 6.2**
    - **Property 12: 工具过滤策略与阶段配置一致** — GetToolFilterForPhase 返回值与 PhaseTemplate.ToolPolicy 一致
    - **验证: 需求 6.3, 6.4**

  - [x] 4.3 创建 `corelib/workflow/persistence.go`，定义 PersistenceStore 接口（已在 types.go 中声明，此处提供文档注释和辅助类型）

  - [x] 4.4 创建 `corelib/workflow/sqlite_store.go`，实现 SQLiteStore
    - 实现 NewSQLiteStore 构造函数（在指定路径创建 SQLite 数据库）
    - 实现 understanding_sessions 和 workflow_states 表的自动建表和索引
    - 实现 SaveUnderstandingSession / LoadUnderstandingSession / DeleteUnderstandingSession
    - 实现 SaveWorkflowState / LoadWorkflowState / DeleteWorkflowState / ListActiveWorkflows
    - 实现 CleanupExpired（按 olderThan 参数清理已完成/已取消记录）
    - JSON 序列化/反序列化 intent、rounds、outputs、gates 字段
    - _需求: 7.1, 7.2, 7.3, 7.4, 7.5_

  - [x]* 4.5 编写持久化层属性测试 `corelib/workflow/sqlite_store_property_test.go`
    - **Property 13: 持久化往返一致性** — Save 后 Load 返回等价数据
    - **验证: 需求 7.1, 7.2, 7.3, 7.4**
    - **Property 14: 过期记录清理正确性** — CleanupExpired 正确清理超期 completed/cancelled 记录，保留 active 记录
    - **验证: 需求 7.5**

- [x] 5. 检查点 — 确保 PromptBuilder 和持久化层测试通过
  - 确保所有测试通过，ask the user if questions arise.

- [x] 6. IntentUnderstandingManager 意图理解
  - [x] 6.1 创建 `corelib/workflow/intent_understanding.go`，实现 IntentUnderstandingManager
    - 实现 IntentUnderstandingManager 结构体（sessions map、store、llm、registry 引用）
    - 实现 Start 方法：创建新会话，构建意图理解 system prompt（含所有模板描述），调用 LLMCaller，解析 JSON 响应
    - 实现 HandleInput 方法：更新会话，调用 LLM，返回 reply/ready/cancelled
    - 实现 GetSession / HasActiveSession 方法
    - 实现 CleanupExpired 方法（30 分钟无活动过期）
    - 实现取消检测（"算了"/"取消"/"不做了"）
    - 实现 ready 判断由 LLM 语义分析（非简单关键词匹配）
    - 使用 sync.RWMutex 保护并发安全
    - _需求: 2.1, 2.2, 2.3, 2.4, 2.5, 2.6, 2.7, 2.8_

  - [x]* 6.2 编写意图理解属性测试
    - **Property 4: 意图理解会话过期清理** — CleanupExpired 后超过 30 分钟的会话被移除，未超时的保留
    - **验证: 需求 2.7**

  - [x]* 6.3 编写意图理解单元测试 `corelib/workflow/intent_understanding_test.go`
    - 使用 Mock LLMCaller 测试 Start/HandleInput 流程
    - 测试取消词检测、会话过期、LLM 返回格式错误处理
    - _需求: 2.1-2.8_

- [x] 7. WorkflowEngine 核心引擎
  - [x] 7.1 创建 `corelib/workflow/engine.go`，实现 WorkflowEngine
    - 实现 WorkflowEngine 结构体（workflows map、registry、understanding、store、callbacks、filter）
    - 实现 NewWorkflowEngine 构造函数（初始化所有组件）
    - 实现 StartWorkflow 方法：验证单用户单活跃工作流、匹配模板、创建 WorkflowState、持久化、通知回调
    - 实现 HandleInput 方法：解析确认词/跳过/修改请求，控制阶段推进逻辑
    - 实现 advancePhase 内部方法：推进到下一阶段或标记 completed
    - 实现 GetActiveWorkflow / HasActiveWorkflow 方法
    - 实现 CancelWorkflow 方法：标记 cancelled，保留 PhaseOutputs
    - 实现 BuildPhasePrompt / GetPhaseToolFilter 方法（委托 PromptBuilder）
    - 实现 RestoreFromStore 方法：从 SQLite 恢复活跃工作流
    - 实现 CleanupExpired 方法：清理超过 7 天的 completed/cancelled 记录
    - 使用 sync.RWMutex 保护并发安全
    - _需求: 5.1-5.9, 7.3, 7.5, 9.4, 9.5, 12.1-12.6_

  - [x]* 7.2 编写 WorkflowEngine 属性测试 `corelib/workflow/engine_property_test.go`
    - **Property 7: StartWorkflow 初始化第一阶段** — PhaseIndex=0, CurrentPhase=第一阶段 ID, Status=active
    - **验证: 需求 5.1**
    - **Property 9: NeedsConfirm 阶段阻止非确认推进** — 非确认输入不推进阶段
    - **验证: 需求 5.4, 5.5**
    - **Property 10: 跳过行为遵循 CanSkip 标志** — CanSkip=true 时跳过推进，CanSkip=false 时保持不变
    - **验证: 需求 5.6, 5.7**
    - **Property 11: 最后阶段完成标记** — 最后阶段推进后 Status=completed
    - **验证: 需求 5.8**
    - **Property 15: 单用户单活跃工作流不变量** — 已有活跃工作流时 StartWorkflow 返回错误
    - **验证: 需求 12.1, 12.2**
    - **Property 16: LLM 失败保持状态不变** — LLM 错误后 PhaseIndex/CurrentPhase/Status 不变
    - **验证: 需求 12.3, 15.1**
    - **Property 17: 取消保留已完成阶段产出物** — CancelWorkflow 后 PhaseOutputs 保持不变
    - **验证: 需求 9.5**

  - [x]* 7.3 编写 WorkflowEngine 单元测试 `corelib/workflow/engine_test.go`
    - 测试斜杠命令兼容性（/new 清理工作流状态、/cancel 取消工作流）
    - 测试 LLM 未配置降级（跳过意图理解，透传 Agent_Loop）
    - 测试 LLM 超时降级（意图理解 >10s 降级、工作流阶段 >30s 保留状态）
    - 测试 SQLite 不可用降级（纯内存模式）
    - 测试完整工作流生命周期（Start → HandleInput → advancePhase → completed）
    - _需求: 5.1-5.9, 9.3-9.5, 12.1-12.6, 15.1-15.3_

- [x] 8. 检查点 — 确保核心引擎所有测试通过
  - 确保所有测试通过，ask the user if questions arise.

- [x] 9. GUI 适配层与集成
  - [x] 9.1 创建 `gui/workflow_adapter.go`，实现 GUIWorkflowAdapter
    - 实现 GUIWorkflowAdapter 结构体（持有 App 和 WorkflowEngine 引用）
    - 实现 EngineCallbacks 接口：
      - SendTextToUser：通过 Wails EventsEmit 发送文本消息
      - EmitPhaseUpdate：发送 `workflow:phase_update` 事件
      - EmitDocUpdate：发送 `workflow:doc_update` 事件
      - EmitGateResult：发送 `workflow:gate_result` 事件
    - _需求: 9.1, 9.2, 16.1-16.4_

  - [x] 9.2 修改 `gui/im_message_handler.go`，集成工作流拦截
    - 在 handleIMMessageWithLoop 中，斜杠命令处理之后、runAgentLoop 之前插入工作流拦截逻辑
    - 调用 QuickFilter.Classify 对消息分流
    - small_talk → 快速回答（复用现有 isShortChitChatMessage 逻辑）
    - simple_directive → 透传现有 runAgentLoop
    - needs_understanding → 调用 IntentUnderstandingManager.Start
    - active_understanding → 调用 IntentUnderstandingManager.HandleInput，ready=true 时启动工作流
    - active_workflow → 调用 WorkflowEngine.HandleInput，根据 WorkflowResponse 决定是否调用 runAgentLoop（注入阶段 prompt）
    - /new、/reset 时同时清理工作流和意图理解状态
    - /cancel 时取消活跃工作流
    - _需求: 9.1, 9.2, 9.3, 9.4, 9.5_

  - [x] 9.3 在 `gui/app.go` 或 `gui/app_maclaw_llm.go` 中初始化 WorkflowEngine
    - 创建 SQLiteStore（路径 ~/.maclaw/workflow.db）
    - 创建 WorkflowRegistry（自动注册内置模板）
    - 创建 IntentUnderstandingManager（注入 LLMCaller 适配器）
    - 创建 WorkflowEngine（注入所有依赖）
    - 创建 GUIWorkflowAdapter 并设置为 EngineCallbacks
    - 调用 RestoreFromStore 恢复活跃工作流
    - 启动定时清理 goroutine（CleanupExpired）
    - _需求: 7.3, 7.4, 7.5, 17.1_

  - [ ]* 9.4 编写 GUI 集成测试 `gui/workflow_integration_test.go`
    - 测试 handleIMMessageWithLoop 中的工作流拦截流程（Mock LLM）
    - 测试意图理解 → 工作流启动 → 阶段推进完整流程
    - 测试 /new 和 /cancel 命令对工作流状态的影响
    - _需求: 9.1-9.5_

- [x] 10. TUI 适配层与集成
  - [x] 10.1 创建 `tui/workflow_adapter.go`，实现 TUIWorkflowAdapter
    - 实现 TUIWorkflowAdapter 结构体
    - 实现 EngineCallbacks 接口：
      - SendTextToUser：通过 TUI 输出文本
      - EmitPhaseUpdate / EmitDocUpdate / EmitGateResult：空实现（TUI 无分栏 UI）
    - _需求: 10.1, 10.2, 10.3_

  - [x] 10.2 修改 `tui/agent_handler.go`，集成工作流拦截
    - 在 RunAgentLoop 中，LLM 调用循环之前插入工作流拦截逻辑
    - 复用 corelib/workflow/ 的 QuickFilter、IntentUnderstandingManager、WorkflowEngine
    - 与 GUI 使用相同的分流和阶段推进逻辑
    - _需求: 10.1, 10.2, 10.3_

  - [x] 10.3 在 TUI 初始化流程中创建和配置 WorkflowEngine
    - 与 GUI 共享 corelib/workflow/ 逻辑，仅适配层不同
    - _需求: 17.1, 17.2_

  - [ ]* 10.4 编写 TUI 集成测试 `tui/workflow_integration_test.go`
    - 测试 RunAgentLoop 中的工作流拦截流程
    - 测试与 GUI 行为一致性
    - _需求: 10.1-10.3, 17.1-17.3_

- [x] 11. 检查点 — 确保 GUI/TUI 集成测试通过
  - 确保所有测试通过，ask the user if questions arise.

- [x] 12. 前端分栏文档预览
  - [x] 12.1 创建 `gui/frontend/src/components/ai/useWorkflowState.ts`，实现工作流 UI 状态管理 Hook
    - 定义 WorkflowUIState 接口（active, splitMode, splitRatio, currentPhaseID, phaseDocuments, gateResults, phases）
    - 实现 useWorkflowState Hook：
      - 监听 Wails 事件 `workflow:phase_update`、`workflow:doc_update`、`workflow:gate_result`
      - 管理 splitMode 状态（工作流文档阶段自动开启，implementation 阶段或非工作流关闭）
      - 管理 phaseDocuments Map（phaseID → markdown content）
      - 提供 openDocPreview / closeDocPreview / setSplitRatio 方法
    - _需求: 16.1, 16.2, 16.3, 16.4, 16.5, 16.9, 16.10, 16.11_

  - [x] 12.2 创建 `gui/frontend/src/components/ai/WorkflowDocPreview.tsx`，实现右侧文档预览面板
    - 实现 WorkflowDocPreview 组件：
      - 接收 phaseDocuments、currentPhaseID、gateResults、onClose props
      - 渲染 Markdown 文档内容（支持滚动浏览）
      - 顶部展示阶段切换标签（PhaseTabs），支持查看前序阶段文档
      - 顶部展示质量门禁结果摘要（/图标）
      - 右上角关闭按钮（×）
    - _需求: 16.2, 16.4, 16.7, 16.9_

  - [x] 12.3 修改 `gui/frontend/src/components/ai/AIAssistantPanel.tsx`，集成分栏布局
    - 引入 useWorkflowState Hook
    - 当 splitMode=true 时渲染左右分栏布局（左侧 ChatPanel + 右侧 WorkflowDocPreview）
    - 添加 ResizeHandle 拖拽分隔条，支持调整左右宽度比例（默认 50:50）
    - 当 splitMode=false 时保持原有单栏全宽布局
    - _需求: 16.1, 16.5, 16.6_

  - [x] 12.4 修改 `gui/frontend/src/components/ai/useAIAssistant.ts`，集成工作流状态
    - 在聊天消息中支持文档名链接渲染（图标 + 阶段名称）
    - 点击文档名链接时触发 openDocPreview 重新打开分栏
    - _需求: 16.10, 16.11, 16.12_

  - [x]* 12.5 编写前端测试 `gui/frontend/src/components/ai/__tests__/useWorkflowState.test.ts`
    - 测试分栏布局状态切换（工作流阶段开启/关闭）
    - 测试文档内容更新和阶段切换
    - 测试质量门禁结果展示
    - 测试文档预览关闭/重新打开
    - _需求: 16.1, 16.3, 16.5, 16.7, 16.9, 16.11_

- [x] 13. 最终检查点 — 确保所有测试通过
  - 确保所有测试通过，ask the user if questions arise.

## 备注

- 标记 `*` 的任务为可选测试任务，可跳过以加速 MVP 交付
- 每个任务引用了具体的需求编号，确保需求全覆盖
- 属性测试验证系统的通用正确性属性，单元测试验证具体场景和边界情况
- 检查点确保增量验证，避免后期大规模返工
- 所有 corelib/workflow/ 代码为 GUI/TUI 共享，适配层仅实现 EngineCallbacks 接口
