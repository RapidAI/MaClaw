# 需求文档

## 简介

提升 MaClaw 的自主 Skill 总结能力：当 MaClaw 完成多步骤且复杂的任务后，自主识别该任务是否值得总结为 Skill，将执行步骤整理为结构化的 Skill 定义，经过验证和现有评分机制评估后，自动上传到 Skill Market。

该功能依赖并集成以下现有组件：
- `gui/llm_trajectory.go` — LLM 轨迹记录（回溯执行步骤）
- `gui/agent_activity.go` — Agent 活动记录
- `gui/skill_evaluator.go` — Skill 评分机制
- `gui/auto_upload_trigger.go` — 自动上传触发器
- `gui/tag_generator.go` — 标签生成器
- `gui/skillmarket_client.go` — Skill Market 客户端
- `gui/skill_security_policy.go` — Skill 安全策略
- `gui/skill_backup.go` — Skill 备份与序列化
- `corelib/skill/scanner.go` — Skill 扫描器
- `corelib/tool/craft.go` — Tool 构建工具

## 术语表

- **MaClaw**：本系统的 AI Agent，负责执行用户任务
- **Skill**：一组结构化的可复用步骤定义，存储为 `skill.yaml` 文件，包含 name、description、triggers、steps 等字段
- **Trajectory**：LLM 交互轨迹，由 `TrajectoryRecorder` 记录的完整会话数据（包含 role、content、tool_calls 等）
- **Skill_Evaluator**：现有的 Skill 执行评分器（`gui/skill_evaluator.go`），根据执行结果生成 -2 到 +2 的评分
- **Auto_Upload_Trigger**：现有的自动上传触发器（`gui/auto_upload_trigger.go`），基于执行次数、评分均值和本地变更判断是否触发上传
- **Skill_Market**：Skill 交易市场，由 HubCenter 后端提供
- **Tag_Generator**：现有的标签生成器（`gui/tag_generator.go`），从 Skill 内容推断 tags 和 price
- **Complexity_Score**：任务复杂度评分，用于判断任务是否值得总结为 Skill
- **Skill_Draft**：从 Trajectory 提取并整理后的 Skill 草稿，尚未通过验证
- **Security_Policy_Checker**：现有的安全策略检查器（`gui/skill_security_policy.go`），检查 Skill 安全标签

## 需求

### 需求 1：任务复杂度识别

**用户故事：** 作为 MaClaw 用户，我希望 MaClaw 在完成任务后自动判断该任务是否足够复杂且有复用价值，以便只有高质量的任务被总结为 Skill。

#### 验收标准

1. WHEN 一个 Agent Loop 会话结束时，THE Complexity_Analyzer SHALL 从 TrajectorySession 中提取步骤数量、工具调用种类数、总交互轮次三个指标
2. WHEN 步骤数量 ≥ 3 且工具调用种类数 ≥ 2 且总交互轮次 ≥ 5 时，THE Complexity_Analyzer SHALL 将 Complexity_Score 标记为 "worth_summarizing"
3. WHEN 步骤数量 < 3 或工具调用种类数 < 2 或总交互轮次 < 5 时，THE Complexity_Analyzer SHALL 将 Complexity_Score 标记为 "too_simple" 并跳过后续总结流程
4. THE Complexity_Analyzer SHALL 从 TrajectorySession.Entries 中统计 role="assistant" 且包含 ToolCalls 的条目作为步骤数量
5. THE Complexity_Analyzer SHALL 从所有 ToolCalls 中提取去重后的工具名称集合作为工具调用种类数
6. IF TrajectorySession 为 nil 或 Entries 为空，THEN THE Complexity_Analyzer SHALL 返回 "too_simple" 且不产生错误

### 需求 2：Trajectory 到 Skill 草稿的转换

**用户故事：** 作为 MaClaw 用户，我希望 MaClaw 能将复杂任务的执行轨迹自动整理为结构化的 Skill 定义，以便该 Skill 可以被复用。

#### 验收标准

1. WHEN Complexity_Score 为 "worth_summarizing" 时，THE Skill_Drafter SHALL 从 TrajectorySession 中提取所有 tool_calls 并按时间顺序整理为 Skill Steps
2. THE Skill_Drafter SHALL 为每个 Step 生成 action（对应工具名称）和 params（对应工具参数）字段
3. THE Skill_Drafter SHALL 从 TrajectorySession 中第一条 role="user" 的 Content 提取任务描述，作为 Skill 的 description 字段
4. THE Skill_Drafter SHALL 调用 `corelib/tool/craft.go` 中的 GenerateSkillName 函数生成 Skill 名称
5. THE Skill_Drafter SHALL 调用 `corelib/tool/craft.go` 中的 ExtractTriggerKeywords 函数从任务描述中提取触发关键词
6. THE Skill_Drafter SHALL 合并连续相同工具的重复调用为单个 Step，并在 params 中标注重复次数
7. IF 某个 tool_call 的执行结果（对应的 role="tool" 条目）包含 "[error]" 或 "[stderr]" 前缀，THEN THE Skill_Drafter SHALL 为该 Step 设置 on_error 字段为 "skip"
8. THE Skill_Drafter SHALL 生成的 Skill_Draft 符合 `corelib/skill/scanner.go` 中 SkillYAMLFile 的结构定义

### 需求 3：Skill 草稿验证

**用户故事：** 作为 MaClaw 用户，我希望自动生成的 Skill 经过验证确保其结构正确且安全合规，以便避免低质量或不安全的 Skill 进入系统。

#### 验收标准

1. THE Skill_Validator SHALL 验证 Skill_Draft 的 name 字段非空且长度 ≤ 60 个字符
2. THE Skill_Validator SHALL 验证 Skill_Draft 的 description 字段非空且长度 ≤ 500 个字符
3. THE Skill_Validator SHALL 验证 Skill_Draft 至少包含 1 个 Step 且每个 Step 的 action 字段非空
4. THE Skill_Validator SHALL 验证 Skill_Draft 的 triggers 列表至少包含 1 个触发词
5. THE Skill_Validator SHALL 调用 Security_Policy_Checker 的 CheckLabels 方法，根据 Step 中涉及的操作推断安全标签并进行检查
6. IF Skill_Draft 的 name 与本地已有 Skill 名称重复，THEN THE Skill_Validator SHALL 在名称后追加时间戳后缀以确保唯一性
7. IF 验证失败，THEN THE Skill_Validator SHALL 返回包含所有失败原因的错误列表

### 需求 4：Skill 评分与质量门控

**用户故事：** 作为 MaClaw 用户，我希望自动生成的 Skill 通过现有评分机制的质量检查，以便只有达标的 Skill 才会被上传。

#### 验收标准

1. WHEN Skill_Draft 通过验证后，THE Skill_Quality_Gate SHALL 将 Skill_Draft 写入本地 Skill 目录（由 `corelib/skill/scanner.go` 的 PrimarySkillsDir 返回的路径）
2. THE Skill_Quality_Gate SHALL 调用 Tag_Generator 的 GenerateTags 方法为 Skill 补全 tags 和 price 元数据
3. THE Skill_Quality_Gate SHALL 调用 Tag_Generator 的 WriteBackToYAML 方法将补全的元数据写回 skill.yaml
4. THE Skill_Quality_Gate SHALL 使用 Skill_Evaluator 的 EvaluateSkillExecution 函数对 Skill 的首次模拟执行结果进行评分
5. WHEN 评分 ≥ +1 时，THE Skill_Quality_Gate SHALL 将 Skill 标记为 "approved"
6. WHEN 评分 < +1 时，THE Skill_Quality_Gate SHALL 将 Skill 标记为 "draft" 并记录日志，等待后续实际执行积累评分
7. IF 写入本地目录失败，THEN THE Skill_Quality_Gate SHALL 记录错误日志并中止上传流程

### 需求 5：自动上传到 Skill Market

**用户故事：** 作为 MaClaw 用户，我希望达标的 Skill 自动上传到 Skill Market，以便其他用户也能使用。

#### 验收标准

1. WHEN Skill 被标记为 "approved" 时，THE Auto_Uploader SHALL 调用 Auto_Upload_Trigger 的 RecordExecution 方法记录执行信息
2. THE Auto_Uploader SHALL 调用 `gui/skill_backup.go` 中的 ExportLearnedSkillsZip 方法将 Skill 打包为 zip 文件
3. THE Auto_Uploader SHALL 调用 Auto_Upload_Trigger 的 ShouldUpload 方法判断是否满足上传条件（执行次数 ≥ 3、评分均值 ≥ +1、本地有变更）
4. WHEN ShouldUpload 返回 true 时，THE Auto_Uploader SHALL 调用 SkillMarketClient 的 SubmitSkill 方法上传 zip 包
5. WHEN 上传成功后，THE Auto_Uploader SHALL 在 Skill 目录下写入 upload_status.json 文件，包含 submission_id 字段
6. IF SkillMarketClient 的 baseURL 为空（HubCenter 未配置），THEN THE Auto_Uploader SHALL 跳过上传并记录警告日志
7. IF 上传失败，THEN THE Auto_Uploader SHALL 记录错误日志但保留本地 Skill 不删除

### 需求 6：端到端流水线编排

**用户故事：** 作为 MaClaw 用户，我希望从任务完成到 Skill 上传的整个流程是自动化的，无需人工干预。

#### 验收标准

1. WHEN TrajectoryRecorder 的 Flush 方法被调用时，THE Skill_Auto_Summary_Pipeline SHALL 自动触发完整流水线：复杂度分析 → 草稿生成 → 验证 → 质量门控 → 上传
2. THE Skill_Auto_Summary_Pipeline SHALL 在后台 goroutine 中异步执行，避免阻塞主 Agent Loop
3. THE Skill_Auto_Summary_Pipeline SHALL 在每个阶段记录结构化日志，包含 session_id、阶段名称和结果
4. IF 流水线中任一阶段失败，THEN THE Skill_Auto_Summary_Pipeline SHALL 中止后续阶段并记录失败原因
5. THE Skill_Auto_Summary_Pipeline SHALL 对同一个 session_id 保证幂等性，重复触发不会产生重复 Skill
6. WHILE Skill_Auto_Summary_Pipeline 正在执行时，THE AgentActivityStore SHALL 更新活动状态为 "skill_summarizing"，以便其他通道感知
