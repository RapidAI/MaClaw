# MaClaw Agent 工作流引擎 — 需求文档

## 1. 背景与问题

### 1.1 现状

当前 MaClaw 的编程工作流通过 `.kiro/steering/coding-workflow.md` 纯 prompt 引导实现：
- LLM 被指示"先需求→再设计→再任务拆分→最后编码"
- 阶段推进完全依赖 LLM 自觉遵守 prompt 规则
- 无代码级状态管理，LLM 可能跳过阶段或误判用户确认
- `gui/im_message_handler.go` 的 `handleIMMessageWithLoop` 中有 `CodingToolGate` 做首轮编码工具拦截，但仅覆盖第一轮，后续阶段无保障

### 1.2 核心问题

1. **Prompt 引导不可靠**：LLM 可能忽略 steering 文件中的阶段约束，直接开始编码
2. **无意图理解能力**：用户说"帮我做一个 CRM"，系统直接进入 agent loop，不做需求澄清
3. **无工作流状态管理**：复杂任务需要多阶段推进，当前系统无法跟踪"用户在哪个阶段"
4. **小白用户无引导**：用户不知道"做产品设计应该先做什么"，系统不提供最佳实践引导
5. **扩展性差**：新增业务类型需要修改 steering 文件，无法通过注册机制扩展
6. **仅覆盖编程场景**：steering 文件只定义了编程三阶段，产品设计、创新、商业计划、测试等场景无引导

### 1.3 目标

在 `corelib/workflow/` 构建代码级状态机工作流引擎（GUI + TUI 共享），实现：
- 对复杂任务进行多轮意图理解和澄清，确保系统与用户对"要做什么"达成共识
- 内置行业最佳实践的阶段模板，自动引导用户按规范流程推进
- 每个阶段通过动态 system prompt 注入引导 LLM，阶段推进由代码控制
- 新增业务类型只需注册模板，不需要修改引擎代码
- 替代现有 `coding-workflow.md` 纯 prompt 方案

### 1.4 架构决策（已确认）

- **代码位置**：`corelib/workflow/`（GUI + TUI 共享）
- **架构模式**：代码级状态机 + 动态 system prompt 注入（类似 Kiro 的 spec workflow 机制）
- **集成方式**：拦截 `runAgentLoop` 之前做意图理解；注入阶段 prompt 到 `runAgentLoop` 内部
- **Hub 角色**：纯数据通道，不承载任何工作流逻辑
- **持久化**：本地 `.maclaw/` 目录 SQLite

## 2. 术语表

- **Workflow_Engine**：工作流引擎，`corelib/workflow/` 包中的核心组件，负责状态管理和阶段推进
- **Intent_Understanding**：意图理解会话，独立的多轮 LLM 对话，不使用工具，在 agent loop 之前运行
- **Workflow_Template**：工作流模板，定义某种业务类型的标准阶段流程
- **Phase**：工作流阶段，模板中的一个步骤，包含 prompt、checklist、deliverable 等属性
- **Structured_Intent**：结构化意图，意图理解阶段的输出，包含类别、摘要、目标、约束等字段
- **Quick_Filter**：快速分流器，纯规则判断消息类型，避免不必要的 LLM 调用
- **System_Prompt_Injection**：系统提示注入，在 `runAgentLoop` 的每轮迭代中动态插入阶段相关的 system prompt
- **Quality_Gate**：质量门禁，每个阶段产出后 LLM 对照 checklist 自检的机制
- **Agent_Loop**：现有的 `runAgentLoop`（GUI）/ `RunAgentLoop`（TUI），LLM + 工具调用循环

## 3. 功能需求

### 需求 1：消息快速分流

**用户故事**：作为 MaClaw 用户，我希望简单消息（问候、翻译等）不触发工作流引擎，直接快速响应，以获得流畅的交互体验。

#### 验收标准

1. WHEN 用户发送 small talk 消息（如"你好"、"谢谢"、"今天天气"），THE Quick_Filter SHALL 将消息标记为 small_talk 类型，不触发意图理解流程
2. WHEN 用户发送简单指令（如"翻译这段话成英文"、"帮我格式化这段 JSON"），THE Quick_Filter SHALL 将消息标记为 simple_directive 类型，直接透传给 Agent_Loop 执行
3. WHEN 用户在活跃工作流阶段内发送消息，THE Quick_Filter SHALL 将消息路由到当前工作流的 Phase 处理器，不重新分类
4. WHEN 用户在活跃意图理解会话内发送消息，THE Quick_Filter SHALL 将消息路由到 Intent_Understanding 会话，不重新分类
5. THE Quick_Filter SHALL 在 5ms 内完成分流判断，不执行任何 I/O 操作
6. WHEN 用户发送复杂任务描述（包含动词 + 目标对象 + 多个约束条件），THE Quick_Filter SHALL 将消息标记为 needs_understanding 类型，进入意图理解流程

### 需求 2：意图理解会话

**用户故事**：作为 MaClaw 用户，我希望在提出复杂任务时，系统能先理解我的真实意图并与我确认，而不是直接开始执行，以确保产出物符合我的预期。

#### 验收标准

1. WHEN 用户发送被 Quick_Filter 标记为 needs_understanding 的消息，THE Workflow_Engine SHALL 创建一个 Intent_Understanding 会话，使用独立的 LLM 对话（无工具调用）分析用户意图，输出 Structured_Intent 并向用户复述理解
2. WHEN 用户在 Intent_Understanding 会话中补充信息，THE Workflow_Engine SHALL 更新 Structured_Intent 并再次向用户复述更新后的理解
3. THE Intent_Understanding SHALL 支持不限轮数的追问，每轮回复末尾提示用户"确定了就告诉我'开工'"
4. WHEN 用户在 Intent_Understanding 会话中说"开工"、"开始"、"可以了"、"就这样"、"没问题了"等确认词，THE Workflow_Engine SHALL 判断 Structured_Intent 的 ready 字段为 true，结束意图理解并进入工作流执行
5. WHEN 用户说"开始我觉得还需要加个功能"等包含确认词但实际在补充需求的消息，THE Workflow_Engine SHALL 判断 ready 为 false，继续意图理解对话
6. WHEN 用户在 Intent_Understanding 会话中说"算了"、"取消"、"不做了"，THE Workflow_Engine SHALL 清理会话状态，回到空闲状态
7. WHEN Intent_Understanding 会话 30 分钟无活动，THE Workflow_Engine SHALL 自动过期该会话并清理状态
8. THE Intent_Understanding SHALL 使用独立的 LLM 调用（不经过 Agent_Loop），不携带工具定义，仅做纯对话理解

### 需求 3：工作流模板注册

**用户故事**：作为 MaClaw 开发者，我希望通过注册机制添加新的工作流模板，而不需要修改引擎代码，以支持不同业务场景的最佳实践引导。

#### 验收标准

1. THE Workflow_Engine SHALL 在初始化时自动注册 6 种内置模板：coding（编程开发）、product_design（产品设计）、innovation（创新制定）、business_plan（商业计划）、testing（测试方案）
2. WHEN 调用 Register 方法注册新模板时，THE Workflow_Engine SHALL 将模板加入注册表，后续意图分类可匹配到该模板
3. THE Workflow_Engine SHALL 提供 AllDescriptions 方法，返回所有已注册模板的描述文本，供 LLM 意图分类使用
4. THE Workflow_Template SHALL 为每个 Phase 定义以下属性：ID、名称、描述、LLM 指令 Prompt、产出物描述、质量检查 Checklist、NeedsConfirm（是否需要用户确认）、CanSkip（是否可跳过）
5. WHEN 重复注册同类型模板时，THE Workflow_Engine SHALL 用新模板覆盖旧模板

### 需求 4：内置工作流模板定义

**用户故事**：作为 MaClaw 用户，我希望系统为常见业务场景提供行业最佳实践的阶段引导，以确保产出物质量。

#### 验收标准

1. THE Workflow_Engine SHALL 提供 coding 模板，包含 5 个阶段：requirements（需求分析）→ tech_design（技术设计）→ task_breakdown（任务拆分）→ implementation（编码实现）→ review（代码审查）
2. THE Workflow_Engine SHALL 提供 product_design 模板，包含 4 个阶段：problem_discovery（问题发现）→ solution_design（方案设计）→ prd（产品需求文档）→ prototype（原型设计）
3. THE Workflow_Engine SHALL 提供 innovation 模板，包含 5 个阶段：opportunity（机会识别）→ ideation（创意发散）→ validation（可行性验证）→ roadmap（路线图）→ action_plan（行动计划）
4. THE Workflow_Engine SHALL 提供 business_plan 模板，包含 5 个阶段：executive_summary（执行摘要）→ market_analysis（市场分析）→ product_strategy（产品策略）→ operations（运营计划）→ financial_projection（财务预测）
5. THE Workflow_Engine SHALL 提供 testing 模板，包含 5 个阶段：test_strategy（测试策略）→ test_design（测试用例设计）→ test_environment（测试环境规划）→ test_execution（测试执行）→ defect_report（缺陷跟踪与报告）
6. THE coding 模板的 implementation 阶段 SHALL 将任务路由到现有 Agent_Loop 执行（携带编码工具），其余阶段通过动态 System_Prompt_Injection 引导 LLM 生成文档产出物
7. THE testing 模板的每个阶段 SHALL 包含该阶段的质量检查 Checklist，覆盖测试方法论的关键要素

### 需求 5：工作流执行引擎（代码级状态机）

**用户故事**：作为 MaClaw 用户，我希望工作流按模板定义的阶段顺序推进，每个阶段有质量检查，阶段推进由我确认控制，以防止关键步骤被跳过。

#### 验收标准

1. WHEN 用户确认"开工"后，THE Workflow_Engine SHALL 展示工作流阶段概览（所有阶段名称和当前进度），自动进入第一阶段
2. WHEN 进入某个阶段时，THE Workflow_Engine SHALL 构建该阶段的 system prompt（包含阶段指令 + Structured_Intent + 前序阶段产出物），通过 System_Prompt_Injection 注入到 Agent_Loop 的 conversation 中
3. WHEN 阶段产出物生成后，THE Workflow_Engine SHALL 使用 LLM 对照该阶段的 Checklist 进行自检，向用户展示检查结果（通过 / 需关注）
4. WHEN 阶段的 NeedsConfirm 为 true 时，THE Workflow_Engine SHALL 等待用户说"下一步"、"确认"、"继续"后才推进到下一阶段
5. WHEN 用户在确认前说"改一下 XX"，THE Workflow_Engine SHALL 将修改请求注入 system prompt，让 LLM 修改当前阶段产出物，不推进阶段
6. WHEN 用户说"跳过"且当前阶段的 CanSkip 为 true 时，THE Workflow_Engine SHALL 跳过当前阶段，推进到下一阶段
7. IF 用户说"跳过"但当前阶段的 CanSkip 为 false，THEN THE Workflow_Engine SHALL 提示用户该阶段不可跳过，说明原因
8. WHEN 最后一个阶段完成后，THE Workflow_Engine SHALL 将工作流标记为 completed，回到空闲状态
9. THE Workflow_Engine SHALL 通过代码逻辑（而非 LLM 判断）控制阶段推进，确保阶段顺序不被 LLM 绕过

### 需求 6：动态 System Prompt 注入

**用户故事**：作为 MaClaw 开发者，我希望工作流引擎能在现有 Agent_Loop 的每轮迭代中动态注入阶段相关的 system prompt，以引导 LLM 在正确的上下文中工作。

#### 验收标准

1. WHEN 工作流处于某个阶段时，THE Workflow_Engine SHALL 在 Agent_Loop 的每轮 LLM 调用前，将阶段 prompt 作为 system message 注入 conversation 数组
2. THE System_Prompt_Injection SHALL 包含：当前阶段名称和描述、阶段 LLM 指令、Structured_Intent 摘要、前序阶段产出物摘要、质量检查 Checklist
3. WHEN 工作流阶段不需要工具调用（如需求分析、技术设计阶段）时，THE Workflow_Engine SHALL 限制 Agent_Loop 的工具列表，仅保留文档生成相关工具
4. WHEN 工作流阶段需要编码工具（如 implementation 阶段）时，THE Workflow_Engine SHALL 恢复完整的工具列表，允许 Agent_Loop 正常执行编码任务
5. THE System_Prompt_Injection SHALL 与现有的 GoalAnchor、DriftDetector、ProgressTracker 等 Harness 模块兼容，不冲突

### 需求 7：工作流状态持久化

**用户故事**：作为 MaClaw 用户，我希望工作流状态在应用重启后能恢复，以避免因意外关闭而丢失进度。

#### 验收标准

1. THE Workflow_Engine SHALL 将工作流状态（当前阶段、每个阶段的产出物、Structured_Intent）持久化到本地 `.maclaw/` 目录的 SQLite 数据库
2. THE Workflow_Engine SHALL 将 Intent_Understanding 会话状态（对话历史、累积的 Structured_Intent）持久化到同一 SQLite 数据库
3. WHEN MaClaw 应用重启后，THE Workflow_Engine SHALL 从 SQLite 恢复活跃的工作流状态，用户可继续从上次阶段继续
4. WHEN MaClaw 应用重启后，THE Workflow_Engine SHALL 从 SQLite 恢复活跃的 Intent_Understanding 会话，用户可继续意图澄清
5. WHEN 工作流状态为 completed 或 cancelled 且超过 7 天，THE Workflow_Engine SHALL 自动清理该记录

### 需求 8：工作流内跑题处理

**用户故事**：作为 MaClaw 用户，我希望在工作流阶段内发送无关消息时，系统能智能处理，不影响工作流进度。

#### 验收标准

1. WHEN 用户在工作流阶段内发送与当前任务无关的简单消息（如"几点了"），THE Workflow_Engine SHALL 快速回答该消息，不改变工作流状态
2. WHEN 用户在工作流阶段内发送与当前任务无关的复杂任务请求，THE Workflow_Engine SHALL 提示用户当前有活跃工作流，建议先完成或取消当前工作流
3. WHEN 用户在工作流阶段内发送与当前阶段相关的修改意见，THE Workflow_Engine SHALL 将消息作为阶段内输入正常处理

### 需求 9：GUI 集成

**用户故事**：作为 MaClaw GUI 用户，我希望工作流引擎无缝集成到现有的聊天交互中，不改变基本操作习惯。

#### 验收标准

1. THE Workflow_Engine SHALL 在 `gui/im_message_handler.go` 的 `handleIMMessageWithLoop` 方法中，在进入 `runAgentLoop` 之前拦截消息，检查是否需要进入意图理解流程
2. WHEN 工作流处于执行阶段时，THE Workflow_Engine SHALL 通过回调机制向 `runAgentLoop` 注入阶段 system prompt，复用现有的 streaming、progress、token 回调
3. THE Workflow_Engine SHALL 与现有的 `/new`、`/cancel`、`/help` 等斜杠命令兼容，不影响其功能
4. WHEN 用户发送 `/new` 或 `/reset` 命令时，THE Workflow_Engine SHALL 同时清理活跃的工作流状态和意图理解会话
5. WHEN 用户发送 `/cancel` 命令时，THE Workflow_Engine SHALL 取消当前活跃的工作流，保留已完成阶段的产出物

### 需求 10：TUI 集成

**用户故事**：作为 MaClaw TUI 用户，我希望在终端界面也能使用工作流引擎的全部功能。

#### 验收标准

1. THE Workflow_Engine SHALL 在 `tui/agent_handler.go` 的 `RunAgentLoop` 方法中，在进入 LLM 调用循环之前拦截消息，检查是否需要进入意图理解流程
2. WHEN 工作流处于执行阶段时，THE Workflow_Engine SHALL 向 TUI 的 `RunAgentLoop` 注入阶段 system prompt，与 GUI 使用相同的 `corelib/workflow/` 逻辑
3. THE Workflow_Engine SHALL 在 TUI 中支持与 GUI 相同的工作流命令和交互模式

### 需求 11：替代现有 Steering 文件

**用户故事**：作为 MaClaw 开发者，我希望代码级状态机完全替代现有的 `coding-workflow.md` steering 文件，消除 prompt 引导与代码控制的重复和冲突。

#### 验收标准

1. WHEN Workflow_Engine 初始化完成后，THE coding 模板 SHALL 覆盖 `coding-workflow.md` 中定义的"编程任务强制三阶段流程"的所有功能
2. THE Workflow_Engine SHALL 保留 `coding-workflow.md` 中的"任务类型判断"逻辑（编码任务 vs 内容处理任务），将其实现为 Quick_Filter 的分流规则
3. THE Workflow_Engine SHALL 保留 `coding-workflow.md` 中的"反循环规则"，将其作为 DriftDetector 的补充规则注入阶段 prompt
4. WHEN Workflow_Engine 完全就绪后，THE `coding-workflow.md` 文件 SHALL 被标记为废弃，由 Workflow_Engine 的 coding 模板替代

### 需求 12：并发与边界情况处理

**用户故事**：作为 MaClaw 用户，我希望系统在各种边界情况下都能正确处理，不会出现状态混乱。

#### 验收标准

1. THE Workflow_Engine SHALL 确保一个用户同一时间最多有一个活跃工作流
2. WHEN 用户在工作流中尝试启动新工作流时，THE Workflow_Engine SHALL 提示用户先完成或取消当前工作流
3. WHEN LLM 调用失败时，THE Workflow_Engine SHALL 保留当前工作流状态，提示用户可重试
4. WHEN LLM 未配置时，THE Workflow_Engine SHALL 跳过意图理解，直接将消息透传给 Agent_Loop（保持现有行为）
5. IF 意图理解阶段 LLM 调用超时超过 10 秒，THEN THE Workflow_Engine SHALL 降级为直接透传 Agent_Loop
6. IF 工作流阶段 LLM 调用超时超过 30 秒，THEN THE Workflow_Engine SHALL 保留当前状态并提示用户重试

## 4. 非功能需求

### 需求 13：性能

**用户故事**：作为 MaClaw 用户，我希望工作流引擎不会显著增加消息处理延迟。

#### 验收标准

1. THE Quick_Filter SHALL 在 5ms 内完成分流判断（纯规则，无 I/O）
2. THE Workflow_Engine 的状态查询操作（检查活跃工作流、获取当前阶段）SHALL 在 1ms 内完成（内存缓存）
3. THE System_Prompt_Injection 的 prompt 构建操作 SHALL 在 2ms 内完成

### 需求 14：可扩展性

**用户故事**：作为 MaClaw 开发者，我希望新增工作流类型只需定义模板并注册，不需要修改引擎代码。

#### 验收标准

1. THE Workflow_Engine SHALL 通过 Register 方法支持运行时注册新模板，不需要修改引擎代码
2. THE Workflow_Template SHALL 为纯数据结构，未来可支持从配置文件或远程加载
3. THE Workflow_Engine 的核心逻辑 SHALL 不包含任何模板特定的 if-else 分支

### 需求 15：可靠性

**用户故事**：作为 MaClaw 用户，我希望工作流引擎在各种异常情况下都能优雅降级，不影响基本使用。

#### 验收标准

1. WHEN LLM 调用失败时，THE Workflow_Engine SHALL 保留当前工作流状态，用户可通过重新发送消息重试
2. WHEN MaClaw 应用重启后，THE Workflow_Engine SHALL 从 SQLite 恢复活跃工作流状态
3. WHEN SQLite 数据库损坏或不可用时，THE Workflow_Engine SHALL 降级为无持久化模式（仅内存状态），不阻塞正常使用

### 需求 16：GUI 分栏文档预览

**用户故事**：作为 MaClaw GUI 用户，我希望在工作流执行阶段（如需求分析、技术设计、任务拆分等），AI 助手面板能自动拆分为左右两栏——左侧继续交互式聊天，右侧实时展示当前阶段的完整文档产出物，以便我在对话的同时查看完整文档，提升工作流体验。

#### 验收标准

1. WHEN 工作流进入文档产出阶段（如 requirements、tech_design、task_breakdown 等非 implementation 阶段）时，THE AIAssistantPanel SHALL 自动切换为左右分栏布局：左侧为聊天区域，右侧为文档预览区域
2. THE 右侧文档预览区域 SHALL 实时渲染当前阶段的 Markdown 产出物，支持滚动浏览完整文档
3. WHEN 用户在左侧聊天区域说"改一下 XX"导致文档更新时，THE 右侧文档预览 SHALL 实时刷新为修改后的版本
4. WHEN 工作流推进到下一阶段时，THE 右侧文档预览 SHALL 自动切换为新阶段的产出物，同时保留前序阶段文档的切换标签
5. WHEN 工作流处于 implementation 阶段或非工作流模式时，THE AIAssistantPanel SHALL 恢复为单栏全宽聊天布局
6. THE 分栏布局 SHALL 支持拖拽调整左右宽度比例，默认比例为 50:50
7. WHEN 质量门禁检查完成后，THE 右侧文档预览 SHALL 在文档顶部展示检查结果摘要（/图标）
8. THE 分栏布局仅适用于 GUI 桌面端，TUI 和 IM 渠道不受影响
9. THE 右侧文档预览区域 SHALL 在右上角提供关闭按钮（×），用户点击后关闭文档预览面板，恢复为单栏聊天布局
10. WHEN 文档预览面板被用户手动关闭后，THE 左侧聊天区域中的阶段产出物消息 SHALL 以可点击的文档名链接形式展示（类似 Kiro 的 spec 文件链接体验），用户点击文档名即可重新打开右侧文档预览面板
11. WHEN 用户点击聊天区域中的文档名链接时，THE AIAssistantPanel SHALL 重新切换为分栏布局，右侧展示对应阶段的文档内容
12. THE 聊天区域中的文档名链接 SHALL 显示阶段名称和文档类型图标（如 需求文档、技术设计、任务列表），便于用户识别

### 需求 17：兼容性

**用户故事**：作为 MaClaw 用户，我希望工作流引擎在 GUI 和 TUI 中行为一致。

#### 验收标准

1. THE Workflow_Engine 的核心逻辑 SHALL 位于 `corelib/workflow/` 包中，GUI 和 TUI 共享同一套代码
2. THE Workflow_Engine SHALL 通过接口抽象与 GUI/TUI 的具体实现解耦，GUI 和 TUI 仅实现集成适配层
3. THE Workflow_Engine SHALL 与现有的 memory、tool router、security firewall 等 corelib 模块兼容
