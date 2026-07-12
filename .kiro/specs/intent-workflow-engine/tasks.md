# 意图理解与工作流引擎 — 任务拆分

## Phase 1: 核心类型定义与数据层

### Task 1.1: 工作流类型与模板数据结构
- [x] 新建 `hub/internal/im/workflow_types.go`
- [x] 定义 `WorkflowType` 字符串枚举（coding / product_design / innovation / business_plan）
- [x] 定义 `PhaseTemplate` 结构体（ID, Name, Description, Prompt, Deliverable, Checklist []string, NeedsConfirm, NeedsDevice, CanSkip）
- [x] 定义 `WorkflowTemplate` 结构体（Type, Name, Description, Keywords []string, Phases []PhaseTemplate）
- [x] 定义 `StructuredIntent` 结构体（Category, Summary, Goals, Constraints, OpenQuestions, Confidence, Ready）
- [x] 定义 `UnderstandingRound` 结构体（UserText, AssistantText, Timestamp）
- [x] 定义 `UnderstandingSession` 结构体（ID, UserID, Intent StructuredIntent, Rounds []UnderstandingRound, State, CreatedAt, UpdatedAt）
- [x] 定义 `UnderstandingState` 枚举（active / confirmed / cancelled）
- [x] 定义 `WorkflowState` 结构体（ID, UserID, Type, TemplateRef, Intent StructuredIntent, CurrentPhase, PhaseOutputs map[string]string, CreatedAt, UpdatedAt）
- [x] 定义 `WorkflowResponse` 结构体（Text, Advance, Complete, RouteAction）

### Task 1.2: 工作流模板注册表
- [x] 新建 `hub/internal/im/workflow_registry.go`
- [x] 实现 `WorkflowRegistry` 结构体（mu sync.RWMutex, templates map[WorkflowType]*WorkflowTemplate）
- [x] 实现 `NewWorkflowRegistry()` 构造函数，自动注册 4 种内置模板
- [x] 实现 `Register(t *WorkflowTemplate)` 方法
- [x] 实现 `Match(wt WorkflowType) *WorkflowTemplate` 方法
- [x] 实现 `AllDescriptions() string` 方法，返回所有模板的类型+名称+描述+关键词，供 LLM prompt 使用
- [x] 编写单元测试 `workflow_registry_test.go`：注册、匹配、AllDescriptions 输出格式

### Task 1.3: 内置模板定义
- [x] 新建 `hub/internal/im/workflow_templates.go`
- [x] 实现 `builtinCodingTemplate()` — 5 阶段：requirements → tech_design → task_breakdown → implementation → review，每阶段含 Prompt + Checklist
- [x] 实现 `builtinProductDesignTemplate()` — 4 阶段：problem_discovery → solution_design → prd → prototype
- [x] 实现 `builtinInnovationTemplate()` — 5 阶段：opportunity → ideation → validation → roadmap → action_plan
- [x] 实现 `builtinBusinessPlanTemplate()` — 5 阶段：executive_summary → market_analysis → product_strategy → operations → financial_projection
- [x] 编写单元测试：验证每个模板的阶段数量、ID 唯一性、必填字段非空

### Task 1.4: SQLite 持久化层
- [x] 新建 `hub/internal/store/sqlite/workflow_repo.go`
- [x] 创建 `understanding_sessions` 表（id, user_id, intent_json, rounds_json, state, created_at, updated_at）
- [x] 创建 `workflow_states` 表（id, user_id, type, template_type, intent_json, current_phase, phase_outputs_json, created_at, updated_at）
- [x] 实现 `SaveUnderstandingSession` / `GetActiveUnderstandingSession(userID)` / `DeleteUnderstandingSession`
- [x] 实现 `SaveWorkflowState` / `GetActiveWorkflowState(userID)` / `DeleteWorkflowState`
- [x] 实现 `CleanupExpired(olderThan time.Duration)` — 清理已完成/已取消且超过 7 天的记录
- [x] 在 `hub/internal/store/store.go` 的 `Repositories` 接口中添加 `WorkflowRepo` 字段
- [x] 编写单元测试：CRUD 操作、过期清理、并发安全

## Phase 2: 消息快速分流

### Task 2.1: QuickFilter 实现
- [x] 新建 `hub/internal/im/quick_filter.go`
- [x] 定义 `FilterResult` 枚举（FilterCommand / FilterActiveWorkflow / FilterActiveUnderstanding / FilterSmallTalk / FilterSimpleDirective / FilterNeedsUnderstanding）
- [x] 实现 `QuickFilter` 结构体，持有 WorkflowEngine 引用（用于检查活跃会话）
- [x] 实现 `Filter(userID, text string) FilterResult` 方法：
  - 检查 `/` 前缀 → FilterCommand
  - 检查活跃工作流 → FilterActiveWorkflow
  - 检查活跃意图理解会话 → FilterActiveUnderstanding
  - `isSmallTalk(text)` 规则判断 → FilterSmallTalk
  - `isSimpleDirective(text)` 规则判断 → FilterSimpleDirective
  - 其余 → FilterNeedsUnderstanding
- [x] 实现 `isSmallTalk(text string) bool` — 短消息（<10 字符）+ 问候词匹配（你好/谢谢/嗯/ok 等）
- [x] 实现 `isSimpleDirective(text string) bool` — 匹配"翻译/格式化/总结/整理"等无需多阶段的指令模式
- [x] 编写单元测试 `quick_filter_test.go`：覆盖各种消息类型的分流结果

### Task 2.2: 集成 QuickFilter 到 Coordinator
- [x] 修改 `hub/internal/im/coordinator.go` 的 `handleLobbyMessage` 方法
- [-] 在现有 `ParseMentions` 之前插入 `QuickFilter.Filter()` 调用
- [ ] FilterActiveWorkflow → 调用 `workflowEngine.HandleWorkflowInput()`
- [ ] FilterActiveUnderstanding → 调用 `workflowEngine.HandleUnderstandingInput()`
- [ ] FilterSmallTalk → 走现有 `hubDirectAnswer` 路径
- [ ] FilterSimpleDirective → 走现有 `routeToSingleMachine` / `routeBroadcast` 路径
- [ ] FilterNeedsUnderstanding → 调用 `workflowEngine.StartUnderstanding()`
- [ ] FilterCommand → 不变（已在 core.go 处理）
- [x] 确保现有 coordinator_test.go 全部通过（回归测试）

## Phase 3: 意图理解会话

### Task 3.1: IntentUnderstanding LLM 交互
- [x] 新建 `hub/internal/im/intent_understanding.go`
- [ ] 定义 `intentUnderstandingSystemPrompt` 常量 — 指导 LLM 输出 JSON 格式的 StructuredIntent + reply + ready 字段
- [ ] 实现 `buildUnderstandingPrompt(session *UnderstandingSession, newText string) []interface{}` — 构建包含历史轮次的 messages 数组
- [ ] 实现 `parseUnderstandingResult(content string) (*StructuredIntent, string, bool, error)` — 解析 LLM JSON 输出，提取 intent + reply + ready
- [ ] 实现 `callUnderstandingLLM(ctx, cfg, session, text) (*StructuredIntent, string, bool, error)` — 完整的 LLM 调用流程（构建 prompt → 调用 → 解析）
- [ ] LLM 调用复用现有 `agent.DoSimpleLLMRequest`，超时 10s
- [x] 编写单元测试：prompt 构建格式、JSON 解析（正常/异常/markdown 包裹）

### Task 3.2: UnderstandingSession 管理
- [ ] 在 `intent_understanding.go` 中实现 `UnderstandingManager` 结构体
- [ ] 持有：configProvider, breaker, llmSem, repo（持久化层）, sessions map（内存缓存）
- [ ] 实现 `StartSession(ctx, userID, text) (*GenericResponse, error)` — 创建会话 → LLM 首轮理解 → 返回复述+追问
- [ ] 实现 `HandleInput(ctx, userID, text) (*GenericResponse, error)` — 追加轮次 → LLM 更新理解 → 检查 ready
- [ ] 实现 `GetActiveSession(userID) *UnderstandingSession` — 先查内存，miss 查 SQLite
- [ ] 实现 `CancelSession(userID)` — 标记 cancelled，清理
- [ ] 实现 `cleanupExpiredSessions()` — 30 分钟无活动的会话自动过期
- [ ] 每轮回复末尾自动追加提示："确定了就告诉我'开工'，或继续补充细节。"
- [ ] ready=true 时返回特殊标记，由 WorkflowEngine 接管创建工作流
- [x] 编写单元测试：会话创建、多轮追加、确认触发、取消、过期清理

### Task 3.3: 跑题检测
- [ ] 在 `intent_understanding.go` 中实现 `detectOffTopic(session, text) OffTopicResult`
- [ ] OffTopicResult 枚举：OnTopic / OffTopicSimple / OffTopicComplex
- [ ] 工作流内消息先经过跑题检测：
  - OnTopic → 正常处理
  - OffTopicSimple → 快速回答（走 hubDirectAnswer），不影响工作流状态
  - OffTopicComplex → 返回提示"当前有活跃的 XX 工作流，建议先完成或发送 /workflow cancel 取消"
- [ ] 跑题检测用轻量规则（关键词 + 长度），不额外调 LLM
- [ ] 编写单元测试

## Phase 4: 工作流执行引擎

### Task 4.1: WorkflowEngine 核心
- [x] 新建 `hub/internal/im/workflow_engine.go`
- [ ] 实现 `WorkflowEngine` 结构体，持有：registry, understandingMgr, repo, configProvider, breaker, llmSem, router（MessageRouter 引用）
- [ ] 实现 `NewWorkflowEngine(...)` 构造函数
- [ ] 实现 `StartWorkflow(ctx, userID, session *UnderstandingSession) (*GenericResponse, error)`:
  - 从 registry 匹配模板
  - 创建 WorkflowState
  - 持久化
  - 生成阶段概览文本
  - 自动执行第一阶段
- [ ] 实现 `HandleWorkflowInput(ctx, userID, text) (*GenericResponse, error)`:
  - 获取活跃工作流
  - 跑题检测
  - 判断用户意图（确认/修改/跳过/取消）
  - 分发到对应处理逻辑
- [ ] 实现 `CancelWorkflow(userID)` — 标记 cancelled，清理
- [ ] 实现 `GetActiveWorkflow(userID) *WorkflowState`

### Task 4.2: 阶段执行与质量门禁
- [ ] 在 `workflow_engine.go` 中实现 `executePhase(ctx, state, phase) (*GenericResponse, error)`:
  - 构建阶段 LLM prompt（phase.Prompt + state.Intent + 前序阶段产出）
  - 调用 LLM 生成产出物（超时 30s）
  - 调用 `runChecklist(ctx, output, phase.Checklist)` 自检
  - 存储产出到 `state.PhaseOutputs[phase.ID]`
  - 持久化更新
  - 格式化输出：产出物 + 检查结果 + 操作提示
- [ ] 实现 `runChecklist(ctx, output string, checklist []string) []CheckResult`:
  - 构建 LLM prompt：给定产出物和检查项，逐项判断 pass/warn/fail
  - 解析 JSON 结果
  - 返回 []CheckResult{Item, Status, Detail}
- [ ] 实现 `formatPhaseOutput(output string, checks []CheckResult, phase PhaseTemplate) string`:
  - 产出物文本
  - 检查结果（//图标）
  - 操作提示（"说'下一步'继续，或直接提修改意见"）
- [x] 编写单元测试：prompt 构建、checklist 解析、格式化输出

### Task 4.3: 阶段推进与用户交互
- [ ] 在 `workflow_engine.go` 中实现用户指令识别：
  - `isAdvanceTrigger(text)` — "下一步/确认/继续/next" 等
  - `isSkipTrigger(text)` — "跳过/skip"
  - `isCancelTrigger(text)` — "取消/cancel/算了/不做了"
  - `isModifyRequest(text)` — 其余非触发词文本视为修改请求
- [ ] 实现 `advancePhase(ctx, state)`:
  - 找到下一个阶段
  - 更新 state.CurrentPhase
  - 如果是最后一个阶段完成 → 标记 Complete
  - 否则执行下一阶段
- [ ] 实现 `modifyCurrentPhase(ctx, state, text)`:
  - 用修改请求 + 当前产出 + 阶段 prompt 构建 LLM 请求
  - 替换 PhaseOutputs 中的当前阶段产出
  - 重新运行 checklist
- [ ] 实现 `skipPhase(ctx, state)`:
  - 检查 CanSkip，不可跳过则提示
  - 可跳过则 advancePhase
- [x] 编写单元测试：各种用户指令的识别、阶段推进逻辑、跳过限制

### Task 4.4: 设备路由阶段
- [ ] 在 `workflow_engine.go` 中实现 `executeDevicePhase(ctx, state, phase)`:
  - NeedsDevice=true 的阶段，将任务文本（前序阶段产出摘要 + 当前阶段描述）路由到设备
  - 复用 Coordinator 的 `routeToTargetWithContext` 或 `routeBroadcast`
  - 设备离线时暂停工作流，返回提示
- [ ] 编写单元测试

## Phase 5: SpaceState 集成与命令支持

### Task 5.1: SpaceState 扩展
- [x] 修改 `hub/internal/im/space_state.go`
- [ ] 新增 `SpaceWorkflow` 常量
- [ ] 新增 `EnterWorkflow(userID, workflowID, workflowType string)` 方法
- [ ] 新增 `ExitWorkflow(userID string)` 方法
- [ ] WorkflowEngine 在 StartWorkflow 时调用 `EnterWorkflow`，在 Complete/Cancel 时调用 `ExitWorkflow`
- [ ] Coordinator 的 `Coordinate` 方法新增 `case SpaceWorkflow:` 分支，直接交给 WorkflowEngine
- [ ] 编写单元测试

### Task 5.2: /workflow 命令
- [x] 修改 `hub/internal/im/core.go` 的 HandleMessage
- [ ] 新增 `/workflow` 命令处理：
  - `/workflow` — 显示当前工作流状态（阶段进度、当前阶段名称）
  - `/workflow cancel` — 取消当前工作流
  - `/workflow skip` — 跳过当前阶段
- [ ] 更新 `/help` 命令输出，包含 /workflow 说明
- [ ] 编写单元测试

### Task 5.3: Coordinator 集成
- [x] 修改 `hub/internal/im/coordinator.go`
- [ ] `NewCoordinator` 新增 `workflowEngine *WorkflowEngine` 参数
- [ ] 在 `Coordinate` 方法中，SpaceWorkflow 状态直接交给 workflowEngine.HandleWorkflowInput
- [ ] 在 `handleLobbyMessage` 中集成 QuickFilter（Task 2.2 的具体实现）
- [ ] 意图理解确认后（ready=true），调用 workflowEngine.StartWorkflow，同时 spaceState.EnterWorkflow
- [ ] 工作流完成/取消后，spaceState.ExitWorkflow，回到 lobby
- [x] 确保所有现有 coordinator_test.go 通过

## Phase 6: Bootstrap 与清理

### Task 6.1: 启动注入
- [x] 修改 `hub/internal/app/bootstrap.go`
- [ ] 创建 WorkflowRegistry（自动注册内置模板）
- [ ] 创建 WorkflowEngine，注入 registry, repo, configProvider, breaker, llmSem, router
- [ ] 将 WorkflowEngine 传入 Coordinator 构造函数
- [ ] 启动后台 goroutine：定期清理过期的 understanding sessions 和 workflow states

### Task 6.2: 数据库迁移
- [ ] 在 Hub SQLite 初始化流程中添加 `understanding_sessions` 和 `workflow_states` 表的 CREATE TABLE IF NOT EXISTS
- [ ] 确保与现有迁移机制兼容

### Task 6.3: 回归测试
- [x] 运行所有现有 `hub/internal/im/*_test.go` 测试，确保无回归
- [ ] 手动测试：飞书/微信发送普通消息，确认现有路由行为不变
- [ ] 手动测试：发送复杂编程需求，验证意图理解 → 工作流 → 阶段推进完整流程
- [ ] 手动测试：工作流中发送 /call、/help 等命令，确认命令优先级正确
- [ ] 手动测试：Hub 重启后工作流状态恢复
