# 需求文档：MaClaw Harness Engineering

## 简介

为 MaClaw 项目引入 Harness Engineering 能力，构建两层 Harness 体系。第一层为 MaClaw Agent 自身（IMMessageHandler 驱动的 agent loop）添加目标锚定、漂移检测、结构化进度追踪和智能重试机制，防止 LLM 在长程任务中漂移、遗忘目标或盲目重试。第二层为 MaClaw 调度的底层编程工具（Claude Code / Codex / Gemini 等）构建更好的运行环境，包括分层 Context File 注入、Linter/CI 反馈注入、失败模式学习和编程工具产出验证。

Harness Engineering 的核心理念：AI Agent 的瓶颈不在模型能力，而在运行环境的设计。Harness 是围绕 Agent 的脚手架、约束和反馈循环的总和。

## 术语表

- **Agent_Loop**：IMMessageHandler 中由 LLM 驱动的 tool_call 决策循环，是 MaClaw Agent 的核心执行引擎
- **Goal_Anchor**：目标锚定模块，负责在长程任务中周期性地将原始用户目标和当前进度摘要重新注入 LLM context 前部
- **Drift_Detector**：漂移检测模块，通过分析最近 K 步 tool_call 序列检测循环模式或目标偏离
- **Progress_Tracker**：结构化进度追踪模块，维护显式的 task checklist，每次 LLM 决策时可见已完成/未完成状态
- **Adaptive_Retry**：智能重试模块，在 tool_call 失败时分析失败原因后调整策略，替代现有盲重试
- **Context_Injector**：分层上下文注入模块，负责将分层的 context files 按工作目录递进注入到编程工具 session
- **Feedback_Injector**：反馈注入模块，从 OutputPipeline 提取 linter 错误和 CI 失败信息，注入到下一次编程工具 context
- **Failure_Learner**：失败模式学习模块，从 OutputPipeline 事件流中提取重复失败模式，自动追加约束规则
- **Harness_Gate**：Harness 门控模块，扩展 SecurityFirewall 为通用验证门控，检查编程工具产出是否符合项目约束
- **Trajectory_Recorder**：现有的 LLM 交互轨迹记录器（gui/llm_trajectory.go）
- **Output_Pipeline**：现有的编程工具输出管道（corelib/remote/output_pipeline.go）
- **Security_Firewall**：现有的安全防火墙（corelib/security/firewall.go）
- **Loop_Context**：Agent Loop 的执行上下文，包含迭代计数、最大迭代数、状态等信息

## 需求

### 需求 1：目标锚定（Goal Anchoring）

**用户故事：** 作为 MaClaw 用户，我希望 Agent 在执行长程任务时始终记住我的原始目标，以便 Agent 不会被中间输出淹没而偏离方向。

#### 验收标准

1. WHEN Agent_Loop 的迭代次数达到锚定间隔 N（默认 N=5），THE Goal_Anchor SHALL 将原始用户目标文本和当前进度摘要重新注入到 LLM context 的系统提示区域
2. THE Goal_Anchor SHALL 从 Agent_Loop 的首条用户消息中提取原始目标文本，并在整个 loop 生命周期内保持该文本不变
3. WHEN 进度摘要被注入时，THE Goal_Anchor SHALL 包含已完成步骤数、当前步骤描述和剩余待完成项的计数
4. THE Goal_Anchor SHALL 将锚定内容控制在 500 tokens 以内，避免过度占用 context 窗口
5. IF 原始用户目标文本超过 200 个字符，THEN THE Goal_Anchor SHALL 截断并保留前 200 个字符加省略标记

### 需求 2：自我漂移检测（Drift Detection）

**用户故事：** 作为 MaClaw 用户，我希望 Agent 能自动检测到自己在重复无效操作或偏离目标，以便 Agent 能及时暂停并重新规划而不是浪费迭代次数。

#### 验收标准

1. WHEN Agent_Loop 累积了 K 步（默认 K=8）tool_call 记录后，THE Drift_Detector SHALL 分析最近 K 步的 tool_call 序列，检测是否存在循环模式
2. THE Drift_Detector SHALL 将以下情况识别为循环模式：连续 3 次或以上调用相同工具且参数相似度超过 80%
3. WHEN 循环模式被检测到，THE Drift_Detector SHALL 向 Agent_Loop 发出暂停信号，并在 LLM context 中注入重新规划提示
4. THE Drift_Detector SHALL 利用 Trajectory_Recorder 已记录的 tool_call 历史进行分析，避免重复存储
5. WHEN 重新规划提示被注入后，THE Drift_Detector SHALL 重置循环检测窗口，从新的起点开始监控
6. IF Agent_Loop 在重新规划后的 K 步内再次触发循环检测，THEN THE Drift_Detector SHALL 将 Agent_Loop 标记为"需要人工介入"并暂停执行

### 需求 3：结构化进度追踪（Progress Invariants）

**用户故事：** 作为 MaClaw 用户，我希望 Agent 维护一个显式的任务清单，以便 LLM 每次决策时都能看到已完成和未完成的状态，减少重复工作。

#### 验收标准

1. WHEN Agent_Loop 开始处理一个包含多步骤的任务时，THE Progress_Tracker SHALL 从用户目标中提取或生成一个结构化的 task checklist
2. THE Progress_Tracker SHALL 以 Markdown checkbox 格式（`- [x]` / `- [ ]`）维护 checklist，每完成一步自动标记
3. WHEN LLM 每次进行 tool_call 决策前，THE Progress_Tracker SHALL 将当前 checklist 状态注入到 LLM context 中
4. THE Progress_Tracker SHALL 将 checklist 内容控制在 300 tokens 以内，超出时仅显示最近 3 个已完成项和全部未完成项
5. IF Agent_Loop 的 tool_call 结果表明某步骤已完成（通过关键词匹配或 LLM 判断），THEN THE Progress_Tracker SHALL 自动将对应 checklist 项标记为已完成
6. THE Progress_Tracker SHALL 在 checklist 全部完成时向 Agent_Loop 发出任务完成信号

### 需求 4：智能重试（Adaptive Retry）

**用户故事：** 作为 MaClaw 用户，我希望 Agent 在 tool_call 失败时能分析原因并调整策略，而不是盲目重试相同的操作。

#### 验收标准

1. WHEN 一个 tool_call 执行失败时，THE Adaptive_Retry SHALL 分析失败的错误信息，将其分类为网络错误、权限错误、参数错误或逻辑错误
2. WHEN 失败类型为网络错误时，THE Adaptive_Retry SHALL 使用指数退避策略重试，最多重试 3 次
3. WHEN 失败类型为参数错误或逻辑错误时，THE Adaptive_Retry SHALL 将错误信息注入 LLM context，请求 LLM 生成修正后的 tool_call 参数
4. WHEN 失败类型为权限错误时，THE Adaptive_Retry SHALL 跳过重试并向用户报告权限问题
5. THE Adaptive_Retry SHALL 记录每次失败的工具名称、错误类型和重试策略到 Trajectory_Recorder
6. IF 同一工具在当前 Agent_Loop 中累计失败超过 5 次，THEN THE Adaptive_Retry SHALL 将该工具标记为不可用，并在 LLM context 中提示使用替代方案

### 需求 5：分层 Context File 注入

**用户故事：** 作为 MaClaw 用户，我希望编程工具能根据工作目录自动获得分层的项目上下文，以便编程工具对项目结构有更准确的理解。

#### 验收标准

1. THE Context_Injector SHALL 支持三层 context file 结构：根目录 AGENTS.md（项目地图）、子目录 AGENTS.md（模块上下文）、工作目录 AGENTS.md（局部上下文）
2. WHEN 编程工具 session 启动时，THE Context_Injector SHALL 从工作目录向上递归收集所有层级的 context files，按从根到叶的顺序拼接
3. THE Context_Injector SHALL 将拼接后的 context 内容控制在 8000 tokens 以内，超出时优先保留根目录和工作目录的内容，截断中间层级
4. IF 某层级的 AGENTS.md 文件不存在，THEN THE Context_Injector SHALL 跳过该层级并继续处理其他层级
5. WHEN 现有的 CLAUDE.md 文件存在时，THE Context_Injector SHALL 将其内容合并到根层级 context 中，保持向后兼容
6. THE Context_Injector SHALL 在注入 context 时添加层级标记（如 `[ROOT]`、`[MODULE: corelib/security]`、`[LOCAL]`），帮助编程工具区分上下文来源

### 需求 6：Linter/CI 反馈注入

**用户故事：** 作为 MaClaw 用户，我希望编程工具在上一次 session 失败后，下一次 session 能自动获得 linter 错误和 CI 失败信息，以便编程工具能直接修复问题而不是重复犯错。

#### 验收标准

1. WHEN 编程工具 session 的 Output_Pipeline 检测到 linter 错误或 CI 失败事件时，THE Feedback_Injector SHALL 提取错误的文件路径、行号和错误描述
2. THE Feedback_Injector SHALL 将提取的错误信息格式化为结构化的反馈块（包含错误类型、位置、描述）
3. WHEN 下一次编程工具 session 启动时，THE Feedback_Injector SHALL 将上一次 session 的反馈块注入到编程工具的初始 context 中
4. THE Feedback_Injector SHALL 将反馈块控制在 2000 tokens 以内，超出时按错误严重程度排序并截断低优先级错误
5. IF 上一次 session 没有产生任何错误事件，THEN THE Feedback_Injector SHALL 不注入任何反馈块
6. THE Feedback_Injector SHALL 在反馈块中标注错误来源（linter / test / build / CI），帮助编程工具区分错误类型

### 需求 7：失败模式学习

**用户故事：** 作为 MaClaw 用户，我希望系统能从编程工具的重复失败中学习，自动生成约束规则防止同类错误再次发生。

#### 验收标准

1. WHEN Output_Pipeline 的事件流中出现同一类型的错误事件累计达到 3 次时，THE Failure_Learner SHALL 将该错误模式识别为重复失败模式
2. THE Failure_Learner SHALL 从重复失败模式中提取约束规则，格式为自然语言描述（如"禁止在 X 文件中使用 Y 模式"）
3. WHEN 约束规则被生成后，THE Failure_Learner SHALL 将其追加到项目根目录的 `.maclaw/learned-constraints.md` 文件中
4. THE Failure_Learner SHALL 在每次编程工具 session 启动时，将 learned-constraints.md 的内容注入到编程工具 context 中
5. THE Failure_Learner SHALL 为每条约束规则记录生成时间和触发次数，约束规则在 7 天内未被触发时自动过期移除
6. IF learned-constraints.md 的内容超过 1500 tokens，THEN THE Failure_Learner SHALL 按触发次数降序排列，截断低频约束

### 需求 8：编程工具产出验证（Harness Gate）

**用户故事：** 作为 MaClaw 用户，我希望编程工具的产出不仅通过安全检查，还要符合项目的编码约束和质量标准。

#### 验收标准

1. THE Harness_Gate SHALL 扩展现有 Security_Firewall 的检查维度，在安全检查之外增加项目约束检查
2. WHEN 编程工具 session 完成后，THE Harness_Gate SHALL 检查产出文件是否符合项目的 `.maclaw/project-constraints.json` 中定义的约束规则
3. THE Harness_Gate SHALL 支持以下约束类型：禁止修改的文件路径模式、必须存在的文件（如测试文件）、禁止引入的依赖包
4. IF 产出违反任何约束规则，THEN THE Harness_Gate SHALL 生成违规报告并注入到下一次编程工具 session 的 context 中
5. THE Harness_Gate SHALL 将检查结果记录到 Security_Firewall 的 AuditLog 中，复用现有审计基础设施
6. WHEN `.maclaw/project-constraints.json` 文件不存在时，THE Harness_Gate SHALL 仅执行现有的安全检查，不进行额外约束验证
