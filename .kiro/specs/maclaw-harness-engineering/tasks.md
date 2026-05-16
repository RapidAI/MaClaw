# 实现计划：MaClaw Harness Engineering

## 概述

按两层 Harness 体系分组实现 8 个组件，每个组件包含核心实现和测试子任务。第一层（Agent Loop Harness）嵌入 `gui/im_message_handler.go` 的 `runAgentLoop`；第二层（Coding Tool Harness）嵌入 `gui/remote_session_manager.go` 的编程工具 session 生命周期。所有属性测试使用 `testing/quick`。

## 任务

### 第一层：Agent Loop Harness

- [x] 1. 实现 GoalAnchor（目标锚定）
  - [x] 1.1 创建 `gui/goal_anchor.go`，实现 GoalAnchor 结构体
    - 实现 `NewGoalAnchor(userText string, interval int) *GoalAnchor`，从用户首条消息提取目标（≤200 字符截断）
    - 实现 `ShouldAnchor(iteration int) bool`，在 iteration > 0 且 iteration % N == 0 时返回 true
    - 实现 `BuildAnchorContent(progressSummary string) string`，生成锚定内容（≤500 tokens）
    - 空消息时使用默认目标文本 "完成用户请求"
    - _需求: 1.1, 1.2, 1.3, 1.4, 1.5_

  - [ ]* 1.2 编写 GoalAnchor 属性测试（Property 1: 目标锚定周期性触发）
    - **Property 1: 目标锚定周期性触发**
    - 在 `gui/goal_anchor_test.go` 中使用 `testing/quick` 验证 ShouldAnchor 的周期性行为
    - *For any* iteration 和 interval N，ShouldAnchor 在且仅在 iteration > 0 且 iteration % N == 0 时返回 true
    - **验证: 需求 1.1**

  - [ ]* 1.3 编写 GoalAnchor 属性测试（Property 2: 目标文本不变性）
    - **Property 2: 目标文本不变性**
    - 在 `gui/goal_anchor_test.go` 中验证构造后原始目标文本不变
    - *For any* 用户目标文本，多次调用 BuildAnchorContent 后内部目标文本始终等于构造时的值
    - **验证: 需求 1.2**

  - [ ]* 1.4 编写 GoalAnchor 属性测试（Property 3: 锚定内容完整性与大小约束）
    - **Property 3: 锚定内容完整性与大小约束**
    - 在 `gui/goal_anchor_test.go` 中验证 BuildAnchorContent 输出包含进度信息且 ≤500 tokens
    - 验证超过 200 字符的目标被截断为 200 字符加省略标记
    - **验证: 需求 1.3, 1.4, 1.5**

- [x] 2. 实现 DriftDetector（漂移检测）
  - [x] 2.1 创建 `gui/drift_detector.go`，实现 DriftDetector 结构体
    - 实现 `NewDriftDetector(windowSize int, threshold float64) *DriftDetector`
    - 实现 `ToolCallRecord` 结构体和 `DriftResult` 结构体
    - 实现 `Record(rec ToolCallRecord)` 记录 tool_call
    - 实现 `DetectDrift() DriftResult`，分析最近 K 步检测循环模式（连续 3 次相同工具且参数相似度 ≥ 80%）
    - 实现 `ResetWindow()` 重置检测窗口
    - 二次漂移触发时设置 NeedHumanHelp=true
    - _需求: 2.1, 2.2, 2.3, 2.4, 2.5, 2.6_

  - [ ]* 2.2 编写 DriftDetector 属性测试（Property 4: 循环模式检测准确性）
    - **Property 4: 循环模式检测准确性**
    - 在 `gui/drift_detector_test.go` 中使用 `testing/quick` 验证循环检测逻辑
    - *For any* tool_call 序列，连续 3 次相同工具且参数哈希相同时 Drifted=true；否则 Drifted=false
    - **验证: 需求 2.1, 2.2, 2.3**

  - [ ]* 2.3 编写 DriftDetector 属性测试（Property 5: 漂移检测窗口重置）
    - **Property 5: 漂移检测窗口重置**
    - 在 `gui/drift_detector_test.go` 中验证 ResetWindow 后需重新累积 K 步才能再次触发
    - **验证: 需求 2.5**

  - [ ]* 2.4 编写 DriftDetector 属性测试（Property 6: 二次漂移触发人工介入）
    - **Property 6: 二次漂移触发人工介入**
    - 在 `gui/drift_detector_test.go` 中验证同一 loop 内第二次漂移返回 NeedHumanHelp=true
    - **验证: 需求 2.6**

- [x] 3. 实现 HarnessProgressTracker（结构化进度追踪）
  - [x] 3.1 创建 `gui/harness_progress_tracker.go`，实现 HarnessProgressTracker 结构体
    - 实现 `ChecklistItem` 结构体
    - 实现 `NewHarnessProgressTracker(items []ChecklistItem, maxTokens int) *HarnessProgressTracker`
    - 实现 `MarkComplete(index int)` 标记完成
    - 实现 `BuildChecklistContent() string`，生成 Markdown checkbox 格式（≤300 tokens）
    - 实现 `AllComplete() bool` 和 `Summary() string`
    - 超出 token 限制时仅保留最近 3 个已完成项和全部未完成项
    - _需求: 3.1, 3.2, 3.3, 3.4, 3.5, 3.6_

  - [ ]* 3.2 编写 HarnessProgressTracker 属性测试（Property 7: Checklist 格式与大小约束）
    - **Property 7: Checklist 格式与大小约束**
    - 在 `gui/harness_progress_tracker_test.go` 中使用 `testing/quick` 验证输出格式和 token 限制
    - 已完成项以 `- [x]` 开头，未完成项以 `- [ ]` 开头，总 token ≤ 300
    - **验证: 需求 3.2, 3.4**

  - [ ]* 3.3 编写 HarnessProgressTracker 属性测试（Property 8: Checklist 完成状态一致性）
    - **Property 8: Checklist 完成状态一致性**
    - 在 `gui/harness_progress_tracker_test.go` 中验证 AllComplete 和 MarkComplete 的一致性
    - 当且仅当所有项 Completed=true 时 AllComplete 返回 true
    - **验证: 需求 3.5, 3.6**

- [x] 4. 实现 AdaptiveRetry（智能重试）
  - [x] 4.1 创建 `gui/adaptive_retry.go`，实现 AdaptiveRetry 结构体
    - 实现 `FailureCategory` 类型和常量（network, permission, args, logic, unknown）
    - 实现 `RetryDecision` 结构体
    - 实现 `NewAdaptiveRetry(recorder *TrajectoryRecorder) *AdaptiveRetry`
    - 实现 `Classify(toolName string, err error) FailureCategory`，分析错误信息分类
    - 实现 `Decide(toolName string, category FailureCategory, attempt int) RetryDecision`
    - 实现 `RecordFailure` 记录到 TrajectoryRecorder，`IsDisabled` 检查工具是否被禁用
    - 网络错误：指数退避重试（≤3 次）；参数/逻辑错误：注入错误上下文请求修正；权限错误：跳过
    - 同一工具累计失败 ≥5 次时标记为不可用
    - _需求: 4.1, 4.2, 4.3, 4.4, 4.5, 4.6_

  - [ ]* 4.2 编写 AdaptiveRetry 属性测试（Property 9: 失败分类到重试策略的映射）
    - **Property 9: 失败分类到重试策略的映射**
    - 在 `gui/adaptive_retry_test.go` 中使用 `testing/quick` 验证分类到策略的映射
    - 网络错误 → retry + 指数退避 + attempt ≤ 3；参数/逻辑错误 → fix + ErrorContext 非空；权限错误 → skip
    - **验证: 需求 4.2, 4.3, 4.4**

  - [ ]* 4.3 编写 AdaptiveRetry 属性测试（Property 10: 工具禁用阈值）
    - **Property 10: 工具禁用阈值**
    - 在 `gui/adaptive_retry_test.go` 中验证累计失败 ≥5 次时 IsDisabled 返回 true
    - **验证: 需求 4.6**

- [x] 5. 检查点 — 第一层 Agent Loop Harness 验证
  - 确保所有测试通过，如有问题请向用户确认。

### 第二层：Coding Tool Harness

- [x] 6. 实现 ContextInjector（分层上下文注入）
  - [x] 6.1 创建 `corelib/configfile/context_injector.go`，实现 ContextInjector 结构体
    - 实现 `ContextLayer` 结构体（Level, Path, Content）
    - 实现 `NewContextInjector(maxTokens int) *ContextInjector`
    - 实现 `Collect(workDir, projectRoot string) []ContextLayer`，从工作目录向上递归收集 AGENTS.md
    - 实现 `Build(layers []ContextLayer) string`，拼接带标记的 context（≤8000 tokens）
    - 支持三层结构：[ROOT]、[MODULE: xxx]、[LOCAL]
    - 合并根目录 CLAUDE.md（向后兼容），缺失层级跳过
    - 超出 token 限制时优先保留 ROOT 和 LOCAL，截断中间层级
    - _需求: 5.1, 5.2, 5.3, 5.4, 5.5, 5.6_

  - [ ]* 6.2 编写 ContextInjector 属性测试（Property 11: 分层 Context 收集顺序与大小约束）
    - **Property 11: 分层 Context 收集顺序与大小约束**
    - 在 `corelib/configfile/context_injector_test.go` 中使用 `testing/quick` 验证收集顺序和 token 限制
    - 层级按从根到叶排序，Build 输出 ≤ 8000 tokens，每层带正确标记
    - **验证: 需求 5.2, 5.3, 5.6**

- [x] 7. 实现 FeedbackInjector（Linter/CI 反馈注入）
  - [x] 7.1 创建 `corelib/remote/feedback_injector.go`，实现 FeedbackInjector 结构体
    - 实现 `FeedbackEntry` 结构体（Source, Severity, File, Line, Message）
    - 实现 `NewFeedbackInjector(maxTokens int) *FeedbackInjector`
    - 实现 `ConsumeEvents(sessionID string, events []ImportantEvent)`，从 OutputPipeline 事件提取反馈
    - 实现 `BuildFeedbackBlock(prevSessionID string) string`，生成结构化反馈块（≤2000 tokens）
    - 实现 `Clear(sessionID string)` 清除反馈
    - 按严重程度排序，标注错误来源（linter/test/build/CI）
    - 无错误事件时不注入反馈块
    - _需求: 6.1, 6.2, 6.3, 6.4, 6.5, 6.6_

  - [ ]* 7.2 编写 FeedbackInjector 属性测试（Property 12: 反馈提取格式化与大小约束）
    - **Property 12: 反馈提取格式化与大小约束**
    - 在 `corelib/remote/feedback_injector_test.go` 中使用 `testing/quick` 验证反馈格式和 token 限制
    - 每条反馈包含来源、文件路径、行号和错误描述，总 token ≤ 2000，按严重程度排序
    - **验证: 需求 6.1, 6.2, 6.4, 6.6**

- [x] 8. 实现 FailureLearner（失败模式学习）
  - [x] 8.1 创建 `corelib/remote/failure_learner.go`，实现 FailureLearner 结构体
    - 实现 `LearnedConstraint` 结构体（Rule, TriggerCount, CreatedAt, LastTriggered）
    - 实现 `NewFailureLearner(projectPath string) *FailureLearner`
    - 实现 `RecordError(errorKey, errorDetail string)`，达到阈值（3 次）时自动生成约束
    - 实现 `LoadConstraints() []LearnedConstraint`，从 `.maclaw/learned-constraints.md` 加载
    - 实现 `BuildConstraintBlock() string`，生成约束注入内容（≤1500 tokens，按触发次数降序）
    - 实现 `PruneExpired()`，移除 7 天内未触发的过期约束
    - _需求: 7.1, 7.2, 7.3, 7.4, 7.5, 7.6_

  - [ ]* 8.2 编写 FailureLearner 属性测试（Property 13: 失败模式学习阈值与约束生成）
    - **Property 13: 失败模式学习阈值与约束生成**
    - 在 `corelib/remote/failure_learner_test.go` 中使用 `testing/quick` 验证阈值触发逻辑
    - 同一错误键记录 ≥3 次时生成约束，<3 次时不生成
    - **验证: 需求 7.1, 7.2**

  - [ ]* 8.3 编写 FailureLearner 属性测试（Property 14: 约束过期清理）
    - **Property 14: 约束过期清理**
    - 在 `corelib/remote/failure_learner_test.go` 中验证 PruneExpired 移除超过 7 天的约束
    - **验证: 需求 7.5**

  - [ ]* 8.4 编写 FailureLearner 属性测试（Property 15: 约束内容大小约束）
    - **Property 15: 约束内容大小约束**
    - 在 `corelib/remote/failure_learner_test.go` 中验证 BuildConstraintBlock 输出 ≤1500 tokens 且按触发次数降序
    - **验证: 需求 7.6**

- [x] 9. 实现 HarnessGate（产出验证门控）
  - [x] 9.1 创建 `corelib/security/harness_gate.go`，实现 HarnessGate 结构体
    - 实现 `ProjectConstraints` 结构体（ForbiddenPaths, RequiredFiles, ForbiddenImports）
    - 实现 `Violation` 结构体（Rule, Detail, File）
    - 实现 `NewHarnessGate(firewall *Firewall, projectPath string) *HarnessGate`
    - 实现 `LoadConstraints(projectPath string) error`，从 `.maclaw/project-constraints.json` 加载
    - 实现 `CheckOutput(sessionID string, changedFiles []string) []Violation`
    - 实现 `BuildViolationReport(violations []Violation) string`
    - 检查维度：禁止修改的文件路径、必须存在的文件、禁止引入的依赖包
    - 检查结果记录到 SecurityFirewall 的 AuditLog
    - 无约束文件时仅执行安全检查
    - _需求: 8.1, 8.2, 8.3, 8.4, 8.5, 8.6_

  - [ ]* 9.2 编写 HarnessGate 属性测试（Property 16: 产出约束检查）
    - **Property 16: 产出约束检查**
    - 在 `corelib/security/harness_gate_test.go` 中使用 `testing/quick` 验证约束检查逻辑
    - 匹配 ForbiddenPaths 返回违规，缺失 RequiredFiles 返回违规，无约束文件返回空列表
    - **验证: 需求 8.2, 8.3**

  - [ ]* 9.3 编写 HarnessGate 属性测试（Property 17: 违规报告生成）
    - **Property 17: 违规报告生成**
    - 在 `corelib/security/harness_gate_test.go` 中验证非空 Violation 列表生成非空报告，空列表返回空字符串
    - **验证: 需求 8.4**

- [x] 10. 检查点 — 第二层 Coding Tool Harness 验证
  - 确保所有测试通过，如有问题请向用户确认。

### 集成与连接

- [x] 11. 将 Harness 模块集成到 Agent Loop
  - [x] 11.1 在 `gui/im_message_handler.go` 中集成第一层 Harness
    - 在 IMMessageHandler 结构体中添加 GoalAnchor、DriftDetector、HarnessProgressTracker、AdaptiveRetry 字段（通过 setter 注入）
    - 在 `runAgentLoop` 开始时初始化 GoalAnchor（提取用户目标）和 HarnessProgressTracker
    - 在每次迭代开始时调用 GoalAnchor.ShouldAnchor 和 BuildAnchorContent，注入系统提示
    - 在每次迭代前注入 HarnessProgressTracker.BuildChecklistContent 到 context
    - 在每次 tool_call 执行后调用 DriftDetector.Record 和 DetectDrift
    - 在 tool_call 失败时使用 AdaptiveRetry 替代现有 isRetryableLLMError 逻辑
    - _需求: 1.1, 2.1, 3.3, 4.1_

  - [x] 11.2 在 `gui/remote_session_manager.go` 中集成第二层 Harness
    - 在 RemoteSessionManager 中添加 ContextInjector、FeedbackInjector、FailureLearner、HarnessGate 字段（通过 setter 注入）
    - 在 session 创建时调用 ContextInjector.Collect + Build，写入编程工具配置
    - 在 session 创建时调用 FeedbackInjector.BuildFeedbackBlock 获取上次反馈并注入
    - 在 session 创建时调用 FailureLearner.BuildConstraintBlock 注入约束
    - 在 OutputPipeline 事件处理后调用 FeedbackInjector.ConsumeEvents 和 FailureLearner.RecordError
    - 在 session 退出后调用 HarnessGate.CheckOutput，违规报告通过 FeedbackInjector 注入下次 session
    - _需求: 5.2, 6.3, 7.4, 8.2_

- [x] 12. 最终检查点 — 全部集成验证
  - 确保所有测试通过，如有问题请向用户确认。

## 备注

- 标记 `*` 的任务为可选任务，可跳过以加速 MVP
- 每个任务引用了具体的需求编号，确保可追溯性
- 检查点确保增量验证
- 属性测试使用 `testing/quick` 验证通用正确性属性
- 单元测试验证具体示例和边界情况
- 所有 Harness 模块通过 setter 注入，不影响现有功能
