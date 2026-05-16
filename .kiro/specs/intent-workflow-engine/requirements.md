# 意图理解与工作流引擎 — 需求文档

## 1. 背景与问题

### 1.1 现状

当前 Hub IM 消息处理管道为三层结构：

- `core.go HandleMessage`：命令解析（/call, /discuss 等），800+ 行 if-else 链
- `coordinator.go Coordinate`：空间状态分发（lobby/private/meeting）→ `RuleEngine` 确定性规则
- `intent_classifier.go Classify`：LLM 意图分类，输出 5 种 `IntentType`（route_single / broadcast / discuss / need_clarification / direct_answer）

所有意图类型都是"路由层"意图——只回答"消息发给谁"，不回答"消息怎么处理"。

### 1.2 核心问题

1. **无任务模式维度**：用户说"帮我开发一个 CRM"，系统只能分类为 route_single 发给某台设备，无法识别这是一个需要多阶段交互的编程任务
2. **无意图理解能力**：分类器只输出一个枚举值，不做复述、不做追问、不做澄清。用户模糊的需求直接透传给设备 Agent，质量不可控
3. **无工作流状态管理**：复杂任务（编程、产品设计、创新制定）需要多阶段推进（需求→设计→实现），当前系统无法跟踪"用户在哪个阶段"
4. **小白用户无引导**：用户不知道"做产品设计应该先做什么"，系统也不提供最佳实践引导，导致产出质量参差不齐
5. **扩展性差**：新增业务类型需要修改 Coordinator 的 switch-case，无法通过注册机制扩展

### 1.3 目标

构建一个可扩展的意图理解与工作流引擎，实现：
- 对复杂任务进行多轮意图理解和澄清，确保系统与用户对"要做什么"达成共识
- 内置行业最佳实践的阶段模板，自动引导用户按规范流程推进
- 每个阶段有质量检查清单，防止关键步骤被跳过
- 新增业务类型只需注册模板，不需要修改引擎代码

## 2. 功能需求

### FR-1: 消息快速分流（QuickFilter）

**描述**：在进入 LLM 意图理解之前，用规则快速判断消息类型，避免不必要的 LLM 调用。

**分流规则**：
- 命令消息（`/` 前缀）→ 现有 CommandParser 处理
- 活跃工作流内的消息 → 交给当前 WorkflowHandler
- 活跃意图理解会话内的消息 → 交给 UnderstandingSession
- Small talk（短消息、无动词、问候语）→ 直接回答或透传设备
- 简单指令（翻译、格式化等无需多阶段的任务）→ 直接执行，不进工作流
- 其余消息 → 进入意图理解流程

**验收标准**：
- AC-1.1: "你好"、"谢谢"、"今天天气" 等 small talk 不触发意图理解，直接回答
- AC-1.2: "翻译这段话成英文" 等简单指令直接透传设备执行
- AC-1.3: 用户在工作流阶段内发送的消息不重新分类，直接交给当前 handler
- AC-1.4: 分流判断延迟 < 5ms（纯规则，无 I/O）

### FR-2: 意图理解会话（IntentUnderstanding）

**描述**：对复杂任务进行多轮 LLM 对话，理解用户真实意图，输出结构化的意图描述。

**核心流程**：
1. 用户发送复杂任务描述（如"帮我做一个能让团队协作的项目管理工具"）
2. LLM 分析消息，输出结构化意图（类别、摘要、目标、约束、模糊点）
3. MaClaw 向用户复述理解 + 列出模糊点追问
4. 用户补充/修正/追问，LLM 更新结构化意图
5. 重复 3-4，不限轮数，用户主导节奏
6. 用户说"开工/开始/可以了"等确认词 → LLM 判断 `ready=true` → 进入工作流

**结构化意图（StructuredIntent）字段**：
- `category`: 工作流类型（coding / product_design / innovation / business_plan / ...）
- `summary`: 一句话复述核心诉求
- `goals`: 拆解出的具体目标点列表
- `constraints`: 用户提到的约束条件（技术栈、平台、时间、规模等）
- `open_questions`: LLM 识别出的模糊点
- `confidence`: 理解置信度 0-1
- `ready`: 用户是否已确认可以开工（由 LLM 综合判断，不纯靠关键词）

**验收标准**：
- AC-2.1: 首次复杂消息自动创建 UnderstandingSession，LLM 复述理解并追问
- AC-2.2: 用户补充信息后，LLM 更新 StructuredIntent 并再次复述
- AC-2.3: 追问不限轮数，每轮回复末尾提示"确定了就告诉我'开工'"
- AC-2.4: 用户说"开工/开始/可以了/就这样/没问题了"时，LLM 输出 `ready=true`
- AC-2.5: 用户说"开始我觉得还需要加个功能"时，LLM 不误判为确认（`ready=false`）
- AC-2.6: 用户说"算了/取消/不做了"时，清理会话回到 idle
- AC-2.7: 会话 30 分钟无活动自动过期
- AC-2.8: 意图理解阶段的 LLM 调用走 Hub LLM，不路由到设备

### FR-3: 工作流模板注册（WorkflowRegistry）

**描述**：提供注册式的工作流模板管理机制，每种业务类型定义标准阶段流程。

**内置模板**：

#### 3.1 编程开发（coding）
参考 SDLC 标准流程：
1. **需求分析**（requirements）— 功能需求、非功能需求、用户角色、边界情况、验收标准
2. **技术设计**（tech_design）— 架构设计、技术选型、模块划分、接口设计、数据结构
3. **任务拆分**（task_breakdown）— 任务列表、优先级、依赖关系、预估工作量
4. **编码实现**（implementation）— 按任务逐个编码（路由到设备执行）
5. **代码审查**（review）— 代码质量、命名、结构、性能、安全

#### 3.2 产品设计（product_design）
参考 PRD 标准流程 + Double Diamond 方法论：
1. **问题发现**（problem_discovery）— 目标用户画像、核心痛点、竞品分析、问题边界
2. **方案设计**（solution_design）— 功能列表、用户故事、信息架构、交互流程
3. **产品需求文档**（prd）— 产品目标、功能规格、非功能需求、发布标准、时间线
4. **原型设计**（prototype）— 原型描述、线框图、关键页面流程

#### 3.3 创新制定（innovation）
参考创新管道框架（Ideation → Validation → Execution → Scaling）：
1. **机会识别**（opportunity）— 市场趋势、用户需求缺口、技术可行性
2. **创意发散**（ideation）— 多方向创意方案、各自优劣对比
3. **可行性验证**（validation）— 技术风险、商业可行性、资源评估、MVP 定义
4. **路线图**（roadmap）— 里程碑、时间线、资源分配
5. **行动计划**（action_plan）— 具体行动项、责任人、完成时间

#### 3.4 商业计划（business_plan）
参考标准商业计划书结构：
1. **执行摘要**（executive_summary）— 一页纸商业概述
2. **市场分析**（market_analysis）— 市场规模、竞争格局、目标客户
3. **产品策略**（product_strategy）— 产品定位、差异化、定价策略
4. **运营计划**（operations）— 团队、流程、供应链、合作伙伴
5. **财务预测**（financial_projection）— 收入预测、成本结构、盈亏平衡

**模板字段**：
- 每个阶段包含：ID、名称、描述、LLM 指令 prompt、产出物描述、质量检查清单、是否需要用户确认、是否需要路由到设备、是否可跳过

**验收标准**：
- AC-3.1: 4 种内置模板在系统启动时自动注册
- AC-3.2: 通过 `Register()` 方法可在运行时注册新模板
- AC-3.3: `AllDescriptions()` 返回所有模板的描述文本，供 LLM 意图分类使用
- AC-3.4: 新增业务类型只需定义模板数据结构并注册，不需要修改引擎代码

### FR-4: 工作流执行引擎（WorkflowEngine）

**描述**：通用的阶段执行引擎，驱动工作流按模板定义的阶段顺序推进。

**核心能力**：
1. **阶段执行**：用阶段 prompt + StructuredIntent + 前序阶段产出构建 LLM 请求，生成当前阶段产出物
2. **质量门禁**：每个阶段产出后，LLM 对照 checklist 自检，向用户展示检查结果（✅/⚠️）
3. **用户确认**：NeedsConfirm=true 的阶段，等待用户说"下一步/确认"才推进
4. **阶段修改**：用户在确认前可以说"改一下 XX"，LLM 修改当前阶段产出
5. **阶段跳过**：CanSkip=true 的阶段，用户可以说"跳过"
6. **设备路由**：NeedsDevice=true 的阶段，将任务路由到设备 Agent 执行
7. **工作流概览**：进入工作流时，向用户展示完整阶段列表和当前进度

**验收标准**：
- AC-4.1: 用户确认"开工"后，展示工作流阶段概览，自动进入第一阶段
- AC-4.2: 每个阶段产出后展示质量检查结果
- AC-4.3: 用户说"下一步/确认/继续"推进到下一阶段
- AC-4.4: 用户说"改一下 XX"时修改当前阶段产出，不推进
- AC-4.5: 用户说"跳过"时跳过当前阶段（仅 CanSkip=true 的阶段）
- AC-4.6: NeedsDevice=true 的阶段自动路由到设备执行
- AC-4.7: 最后一个阶段完成后，工作流标记为 completed，回到 idle

### FR-5: 工作流状态持久化

**描述**：工作流状态和意图理解会话需要持久化，支持 Hub 重启后恢复。

**持久化内容**：
- UnderstandingSession：对话历史、累积的 StructuredIntent
- WorkflowState：当前阶段、每个阶段的产出物、创建/更新时间

**验收标准**：
- AC-5.1: Hub 重启后，活跃的工作流状态可恢复
- AC-5.2: Hub 重启后，活跃的意图理解会话可恢复
- AC-5.3: 已完成/已取消的工作流保留 7 天后自动清理
- AC-5.4: 存储使用 SQLite（与现有 Hub 存储一致）

### FR-6: 工作流内跑题处理

**描述**：用户在工作流阶段内发送与当前任务无关的消息时，系统需要智能处理。

**处理策略**：
- LLM 判断消息是否与当前工作流相关
- 相关 → 作为阶段内输入处理
- 无关但简单（如"几点了"）→ 快速回答，不影响工作流状态
- 无关且复杂 → 提示用户当前有活跃工作流，建议先完成或取消

**验收标准**：
- AC-6.1: 工作流内发送"今天天气怎么样"，快速回答后继续当前阶段
- AC-6.2: 工作流内发送"帮我做另一个项目"，提示有活跃工作流
- AC-6.3: 工作流内发送与当前阶段相关的修改意见，正常处理

### FR-7: 与现有路由系统集成

**描述**：工作流引擎需要与现有的 Coordinator / RuleEngine / IntentClassifier / SpaceState 系统无缝集成。

**集成点**：
- WorkflowEngine 嵌入 Coordinator，在 `handleLobbyMessage` 中优先检查活跃会话/工作流
- 意图理解阶段的 LLM 调用走 Hub LLM（通过现有 HubLLMConfig + CircuitBreaker + LLMSemaphore）
- 工作流的设备执行阶段复用现有的 `routeToSingleMachine` / `routeBroadcast`
- SpaceState 新增 `SpaceWorkflow` 状态，与 lobby/private/meeting 并列

**验收标准**：
- AC-7.1: 现有的 /call、/discuss、/help 等命令不受影响
- AC-7.2: 现有的 SpaceState（lobby/private/meeting）逻辑不受影响
- AC-7.3: 工作流的设备执行阶段正确路由到目标设备
- AC-7.4: LLM 调用受 CircuitBreaker 保护，失败时降级为 passthrough

## 3. 非功能需求

### NFR-1: 性能
- QuickFilter 分流延迟 < 5ms
- 意图理解 LLM 调用超时 10s，超时降级为 passthrough
- 工作流阶段 LLM 调用超时 30s（阶段产出物较长）

### NFR-2: 可靠性
- LLM 调用失败时，意图理解降级为直接透传设备
- 工作流阶段 LLM 失败时，保留当前状态，用户可重试
- Hub 重启后工作流状态可恢复

### NFR-3: 可扩展性
- 新增工作流类型只需定义模板 + 注册，不修改引擎代码
- 模板定义为纯数据结构，未来可支持从配置文件/远程加载

### NFR-4: 兼容性
- 所有 IM 平台（飞书、微信、QQ、Telegram、WebSocket）统一走同一套工作流引擎
- GUI 本地模式暂不涉及（本需求仅针对 Hub 侧 IM 消息处理）

## 4. 边界情况

### 4.1 并发工作流
- 一个用户同一时间只能有一个活跃工作流
- 用户在工作流中尝试启动新工作流时，提示先完成或取消当前工作流
- 用户可通过 `/workflow cancel` 命令取消当前工作流

### 4.2 设备离线
- 工作流的非设备阶段（需求分析、设计等）不依赖设备在线
- 工作流进入设备执行阶段时，如果目标设备离线，暂停工作流并通知用户

### 4.3 LLM 不可用
- Hub LLM 未配置时，跳过意图理解，直接透传设备（现有行为）
- CircuitBreaker 熔断时，同上

### 4.4 多设备场景
- 意图理解阶段不涉及设备选择
- 工作流进入设备执行阶段时，由现有路由逻辑（RuleEngine + IntentClassifier）决定发给哪台设备

## 5. 涉及文件（现有代码）

- `hub/internal/im/core.go` — HandleMessage 入口，需要插入 QuickFilter
- `hub/internal/im/coordinator.go` — Coordinate 方法，需要集成 WorkflowEngine
- `hub/internal/im/intent_classifier.go` — 现有意图分类器，需要与 IntentUnderstanding 协调
- `hub/internal/im/rule_engine.go` — 现有规则引擎，保持不变
- `hub/internal/im/space_state.go` — 需要新增 SpaceWorkflow 状态
- `hub/internal/im/hub_llm_config.go` — Hub LLM 配置，复用
- `hub/internal/im/circuit_breaker.go` — 熔断器，复用
- `hub/internal/im/llm_semaphore.go` — LLM 信号量，复用
