# 实现计划：MaClaw Skill 自动总结

## 概述

基于设计文档中的 6 个组件，按增量方式实现端到端的 Skill 自动总结流水线。所有代码集中在 `gui/skill_auto_summary.go`，测试分布在 `gui/skill_auto_summary_test.go` 和 `gui/skill_auto_summary_property_test.go`。最后在 `gui/llm_trajectory.go` 的 Flush 方法中集成触发点。

## Tasks

- [x] 1. 实现 ComplexityAnalyzer 和 SkillDrafter 核心组件
  - [x] 1.1 实现 ComplexityResult 结构体和 AnalyzeComplexity 函数
    - 在 `gui/skill_auto_summary.go` 中定义 `ComplexityResult` 结构体
    - 实现 `AnalyzeComplexity(session *TrajectorySession) ComplexityResult`
    - 遍历 Entries 统计 StepCount（role="assistant" 且 ToolCalls 非 nil）、ToolKindCount（去重工具名称数）、TurnCount（Entries 长度）
    - 阈值判定：StepCount ≥ 3 && ToolKindCount ≥ 2 && TurnCount ≥ 5 → "worth_summarizing"，否则 → "too_simple"
    - nil/空 session 返回 "too_simple"
    - _Requirements: 1.1, 1.2, 1.3, 1.4, 1.5, 1.6_

  - [x] 1.2 实现 DraftSkill 函数
    - 在 `gui/skill_auto_summary.go` 中实现 `DraftSkill(session *TrajectorySession) (*SkillYAMLFile, error)`
    - 提取所有 tool_calls 按时间顺序整理为 SkillYAMLStep
    - 合并连续相同工具调用，params 中标注 `_repeat_count`
    - 从第一条 role="user" 的 Content 提取 description
    - 调用 `GenerateSkillName` 和 `ExtractTriggerKeywords` 生成 name 和 triggers
    - 对执行出错的 Step（对应 tool 条目包含 "[error]" 或 "[stderr]"）设置 on_error="skip"
    - 无 tool_calls 时返回错误；无 user 消息时用 session_id 作为 fallback description
    - _Requirements: 2.1, 2.2, 2.3, 2.4, 2.5, 2.6, 2.7, 2.8_

  - [ ]* 1.3 为 ComplexityAnalyzer 编写属性测试
    - **Property 1: 复杂度指标提取正确性**
    - **Property 2: 复杂度阈值分类正确性**
    - **Validates: Requirements 1.1, 1.2, 1.3, 1.4, 1.5**

  - [ ]* 1.4 为 SkillDrafter 编写属性测试
    - **Property 3: 草稿步骤提取忠实性**
    - **Property 4: 草稿描述提取正确性**
    - **Property 5: 草稿名称与触发词生成一致性**
    - **Property 6: 连续重复工具调用合并**
    - **Property 7: 错误步骤标记**
    - **Validates: Requirements 2.1, 2.2, 2.3, 2.4, 2.5, 2.6, 2.7**


- [x] 2. 实现 SkillValidator 验证组件
  - [x] 2.1 实现 ValidationError 和 ValidateSkillDraft 函数
    - 在 `gui/skill_auto_summary.go` 中定义 `ValidationError` 结构体（含 Reasons []string）
    - 实现 `ValidateSkillDraft(draft *SkillYAMLFile, checker *SecurityPolicyChecker, existingNames map[string]bool) (*SkillYAMLFile, error)`
    - 验证规则：name 非空且 ≤ 60 字符、description 非空且 ≤ 500 字符、至少 1 个 Step 且 action 非空、triggers 至少 1 个
    - 调用 `SecurityPolicyChecker.CheckLabels` 检查安全标签
    - 名称冲突时追加时间戳后缀
    - 收集所有失败原因，返回 `ValidationError`
    - _Requirements: 3.1, 3.2, 3.3, 3.4, 3.5, 3.6, 3.7_

  - [ ]* 2.2 为 SkillValidator 编写属性测试
    - **Property 9: 验证规则完整性**
    - **Property 10: 安全标签验证**
    - **Property 11: 名称去重唯一性**
    - **Validates: Requirements 3.1, 3.2, 3.3, 3.4, 3.5, 3.6, 3.7**

  - [ ]* 2.3 为 YAML round-trip 编写属性测试
    - **Property 8: Skill YAML 序列化 round-trip**
    - **Validates: Requirements 2.8**

- [x] 3. Checkpoint — 确保所有测试通过
  - 运行 `go test ./gui/ -run TestSkillAutoSummary` 确保所有测试通过，如有问题请向用户确认。

- [x] 4. 实现 QualityGate 和 AutoUploader 组件
  - [x] 4.1 实现 QualityGateResult 和 RunQualityGate 函数
    - 在 `gui/skill_auto_summary.go` 中定义 `QualityGateResult` 结构体
    - 实现 `RunQualityGate(draft *SkillYAMLFile, tagGen *TagGenerator) (*QualityGateResult, error)`
    - 将 Skill 写入 `PrimarySkillsDir` 返回的目录
    - 调用 `TagGenerator.GenerateTags` + `WriteBackToYAML` 补全元数据
    - 调用 `EvaluateSkillExecution` 评分，score ≥ 1 → "approved"，否则 → "draft"
    - 写入失败时返回错误并中止
    - _Requirements: 4.1, 4.2, 4.3, 4.4, 4.5, 4.6, 4.7_

  - [x] 4.2 实现 RunAutoUpload 函数
    - 在 `gui/skill_auto_summary.go` 中实现 `RunAutoUpload(ctx, skillName, skillDir, score, trigger, skillExec, client) error`
    - 调用 `AutoUploadTrigger.RecordExecution` 记录执行
    - 调用 `ExportLearnedSkillsZip` 打包
    - 调用 `ShouldUpload` 判断是否上传
    - 调用 `SkillMarketClient.SubmitSkill` 上传
    - 上传成功后写入 `upload_status.json`
    - HubCenter 未配置时跳过上传；上传失败时保留本地 Skill
    - _Requirements: 5.1, 5.2, 5.3, 5.4, 5.5, 5.6, 5.7_

  - [ ]* 4.3 为 QualityGate 编写属性测试
    - **Property 12: 评分阈值决定审批状态**
    - **Validates: Requirements 4.5, 4.6**

- [x] 5. 实现 Pipeline 编排和集成触发
  - [x] 5.1 实现 SkillAutoSummaryPipeline 结构体和 RunPipeline 方法
    - 在 `gui/skill_auto_summary.go` 中定义 `SkillAutoSummaryPipeline` 结构体
    - 包含 `processed map[string]bool` 用于幂等保护
    - 实现 `RunPipeline(session *TrajectorySession)` 方法
    - 按顺序调用：AnalyzeComplexity → DraftSkill → ValidateSkillDraft → RunQualityGate → RunAutoUpload
    - 任一阶段失败则中止后续阶段并记录日志
    - 同一 session_id 重复调用时直接跳过
    - 更新 AgentActivityStore 状态为 "skill_summarizing"
    - _Requirements: 6.1, 6.2, 6.3, 6.4, 6.5, 6.6_

  - [x] 5.2 在 TrajectoryRecorder.Flush 中集成触发点
    - 修改 `gui/llm_trajectory.go` 的 `Flush` 方法
    - 在写入 JSON 文件成功后，启动后台 goroutine 调用 `pipeline.RunPipeline(session)`
    - 确保 goroutine 不阻塞 Flush 返回
    - _Requirements: 6.1, 6.2_

  - [ ]* 5.3 为 Pipeline 编写属性测试
    - **Property 13: 流水线幂等性**
    - **Property 14: 流水线阶段失败中止**
    - **Validates: Requirements 6.4, 6.5**

- [x] 6. 编写单元测试覆盖边界情况
  - [x] 6.1 在 `gui/skill_auto_summary_test.go` 中编写单元测试
    - 测试 nil/空 session 的 AnalyzeComplexity 行为
    - 测试单步骤简单任务（不满足阈值）
    - 测试包含错误结果的 tool_call 序列的 DraftSkill 行为
    - 测试名称冲突去重的 ValidateSkillDraft 行为
    - 测试 HubCenter 未配置时 RunAutoUpload 的跳过行为
    - 测试上传失败后本地 Skill 保留
    - _Requirements: 1.6, 2.7, 3.6, 5.6, 5.7_

- [x] 7. Final checkpoint — 确保所有测试通过
  - 运行 `go test ./gui/ -run "TestSkillAutoSummary|TestProperty"` 确保所有测试通过，如有问题请向用户确认。

## Notes

- Tasks marked with `*` are optional and can be skipped for faster MVP
- 所有新代码集中在 `gui/skill_auto_summary.go`，唯一的现有文件修改是 `gui/llm_trajectory.go`（Task 5.2）
- 属性测试使用 Go 标准库 `testing/quick`，每个属性至少 100 次迭代
- 每个属性测试通过注释引用设计文档中的属性编号
- Checkpoints 确保增量验证
