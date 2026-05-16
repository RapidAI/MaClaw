# 需求文档：MacLaw 闭环学习系统

## 简介

受 Hermes Agent 闭环学习架构启发，本需求定义 MacLaw 的三项能力升级，使其从"能生成 Skill"进化为"Skill 反哺决策 + Skill 自我迭代 + 经验自动沉淀"的闭环系统。

MacLaw 当前已具备：
- Skill 自动总结 pipeline（`gui/skill_auto_summary.go`）：trajectory → skill draft → validate → quality gate → upload
- 工具路由三信号融合（`corelib/tool/router.go`）：retrieval + experience + priority
- UsageTracker 工具使用历史（`corelib/tool/usage_tracker.go`）
- Memory Store 长期记忆（`corelib/memory/store.go`）：BM25 + vector + graph + Memory Stream 评分

缺失的闭环：
1. 已学到的 Skill 不参与 Router 的工具选择决策
2. 新 trajectory 完成同类任务时，已有 Skill 不会被更新迭代
3. UsageTracker 的高频成功模式不会沉淀为 Memory 中的项目知识

## 术语表

- **Router**: 工具路由器（`corelib/tool/router.go`），根据用户消息从候选工具中选择最相关的子集提供给 LLM
- **DynamicToolBuilder**: 动态工具构建器（`corelib/tool/builder.go`），从 Registry 构建工具定义列表
- **UsageTracker**: 工具使用追踪器（`corelib/tool/usage_tracker.go`），记录工具调用历史并计算 ExperienceScore
- **SkillProvider**: 新增接口，提供当前活跃 Skill 的摘要信息（name + triggers + description）
- **SkillAutoSummaryPipeline**: 现有的 Skill 自动总结流水线（`gui/skill_auto_summary.go`）
- **Memory_Store**: 长期记忆存储（`corelib/memory/store.go`）
- **Skill_Versioner**: 新增组件，管理 Skill 的版本历史
- **UsagePatternBridge**: 新增组件，从 UsageTracker 提取高频模式写入 Memory Store

## 需求

### 需求 1：Skill 反哺 Router — SkillProvider 接口

**用户故事：** 作为开发者，我希望 Router 在选择工具时能感知已有的 Skill，以便当用户消息匹配到已学 Skill 时，`run_skill` 工具获得更高的路由评分。

#### 验收标准

1. THE Router SHALL 接受一个 SkillProvider 接口，该接口提供 `ListActiveSkills() []SkillSummary` 方法，返回所有 status="active" 的 Skill 的 name、triggers、description
2. WHEN Router.Route() 执行评分时，THE Router SHALL 对名为 `run_skill` 的候选工具计算 skill_match_score：用 BM25 对 userMessage 和所有 Skill 的 triggers+description 做匹配，取最高分并归一化到 [0,1]
3. THE Router SHALL 使用四信号融合公式：`0.5×retrieval + 0.25×experience + 0.15×skill_match + 0.1×priority`（当 SkillProvider 已配置时）
4. WHEN SkillProvider 未配置时，THE Router SHALL 回退到现有三信号公式，行为不变
5. THE DynamicToolBuilder SHALL 同步支持 SkillProvider 注入和四信号评分逻辑

### 需求 2：Skill 反哺 Router — 动态描述增强

**用户故事：** 作为开发者，我希望 `run_skill` 工具的描述能动态包含匹配到的 Skill 名称，以便 LLM 知道有哪个具体 Skill 可用。

#### 验收标准

1. WHEN Router.Route() 发现 skill_match_score > 0.3 时，THE Router SHALL 将匹配到的 top-3 Skill 名称追加到 `run_skill` 工具的 description 中，格式为 `"... 可用 Skill: {name1}, {name2}, {name3}"`
2. THE Router SHALL 仅在当次 Route() 调用中临时修改 description，不影响原始工具定义
3. IF 没有 Skill 匹配到（skill_match_score ≤ 0.3），THEN THE Router SHALL 保持 `run_skill` 的原始 description 不变

### 需求 3：Skill 反哺 Router — 路由日志增强

**用户故事：** 作为开发者，我希望路由日志中包含 Skill 匹配信息，以便调试和验证闭环效果。

#### 验收标准

1. WHEN writeRouteLog 记录路由决策时，THE Router SHALL 额外记录 skill_match_score 和匹配到的 Skill 名称列表
2. THE 日志格式 SHALL 在现有 "Top-N candidates by fused score" 之后增加 "Skill match: score=X.XXXX matched=[skill1, skill2]" 行

### 需求 4：Skill 自动迭代 — 已有 Skill 匹配

**用户故事：** 作为开发者，我希望 SkillAutoSummaryPipeline 在生成新 Skill 前先检查是否已有类似 Skill，以便更新已有 Skill 而非创建重复。

#### 验收标准

1. WHEN SkillAutoSummaryPipeline.RunPipeline() 在 DraftSkill 阶段完成后，THE Pipeline SHALL 调用 FindSimilarSkill 方法，用 BM25 对 draft.Description 和所有已有 Skill 的 description+triggers 做相似度评分
2. WHEN 最高相似度 > 0.6 时，THE Pipeline SHALL 进入迭代模式而非新建模式
3. IF 没有已有 Skill 的相似度 > 0.6，THEN THE Pipeline SHALL 走正常的新建流程（现有行为不变）

### 需求 5：Skill 自动迭代 — 版本化更新

**用户故事：** 作为开发者，我希望 Skill 更新时保留旧版本，以便审计和回滚。

#### 验收标准

1. WHEN Pipeline 进入迭代模式时，THE Skill_Versioner SHALL 将已有 skill.yaml 备份为 `skill.yaml.v{N}`（N 为递增版本号）
2. THE Skill_Versioner SHALL 比较新旧 Skill 的步骤数量和 error step 数量，仅在新版本更优时执行更新（步骤更少或 error step 更少）
3. IF 新版本不优于旧版本，THEN THE Pipeline SHALL 跳过更新并记录日志 "existing skill is better, skipping iteration"
4. THE Skill_Versioner SHALL 保留最多 5 个历史版本，超出时删除最旧的版本文件

### 需求 6：UsageTracker → Memory 桥梁 — 模式提取

**用户故事：** 作为开发者，我希望 MacLaw 能从工具使用历史中自动发现高频成功模式，以便这些经验被持久化为项目知识。

#### 验收标准

1. THE UsageTracker SHALL 提供 ExtractPatterns(windowDays int) 方法，扫描最近 N 天的 records，按 toolName 分组统计成功率和调用次数
2. WHEN 某工具成功率 > 80% 且调用次数 > 5 时，THE ExtractPatterns SHALL 为该工具生成一条 pattern 描述，包含工具名、关联的 top query tokens、成功率和调用次数
3. THE ExtractPatterns SHALL 返回 []UsagePattern 切片，每个元素包含 ToolName、TopTokens、SuccessRate、Count、Description 字段

### 需求 7：UsageTracker → Memory 桥梁 — 自动写入 Memory

**用户故事：** 作为开发者，我希望提取的使用模式自动写入 Memory Store，以便 Recall 时能在系统 prompt 中注入"这个项目常用 X 工具做 Y 任务"的上下文。

#### 验收标准

1. THE UsagePatternBridge SHALL 每 24 小时执行一次 ExtractPatterns(7)，将结果写入 Memory Store
2. THE UsagePatternBridge SHALL 以 category=`project_knowledge`、tags 包含 `["usage_pattern", toolName]` 写入 Memory Store
3. WHEN Memory Store 中已存在相同 toolName 的 usage_pattern 条目时，THE UsagePatternBridge SHALL 更新该条目的 content 和 updated_at，而非创建新条目
4. THE UsagePatternBridge SHALL 在写入前检查 pattern 的 Description 是否与已有条目语义相同（精确字符串匹配），相同则仅更新 access_count
5. IF Memory Store 未初始化或不可用，THEN THE UsagePatternBridge SHALL 记录警告日志并跳过写入
