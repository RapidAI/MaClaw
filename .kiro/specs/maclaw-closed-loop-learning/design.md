# 技术设计文档：MacLaw 闭环学习系统

## 概述

本设计覆盖三个改进方向：P0 Skill 反哺 Router、P1 Skill 自动迭代、P2 UsageTracker → Memory 桥梁。所有改动均在现有架构上增量扩展，不破坏已有接口。

## P0：Skill 反哺 Router

### 架构变更

```
用户消息 → Router.Route()
              ├── BM25 + Vector → retrieval score
              ├── UsageTracker → experience score
              ├── Registry → priority bonus
              └── SkillProvider → skill_match score  ← 新增
                    ↓
              四信号融合 → 排序 → 选择 top-N
```

### 接口定义

在 `corelib/tool/router.go` 中新增：

```go
// SkillSummary is a lightweight view of an active Skill for routing.
type SkillSummary struct {
    Name        string
    Triggers    []string
    Description string
}

// SkillProvider abstracts access to active skills for routing decisions.
type SkillProvider interface {
    ListActiveSkills() []SkillSummary
}
```

### Router 改动

1. `Router` struct 增加 `skillProvider SkillProvider` 字段
2. 新增 `SetSkillProvider(provider SkillProvider)` 方法
3. `Route()` 中增加 skill match 评分逻辑：
   - 构建所有 skill 的 triggers+description 文本，用 BM25 对 userMessage 评分
   - 对 `run_skill` 工具的 candidate，取最高 skill match 分作为 skill_match_score
   - 融合公式变为：`0.5×retrieval + 0.25×experience + 0.15×skill_match + 0.1×priority`
4. 当 skill_match_score > 0.3 时，临时修改 `run_skill` 的 description 追加匹配到的 skill 名称

### DynamicToolBuilder 同步改动

`DynamicToolBuilder` 增加相同的 `SetSkillProvider` 和 `Build()` 中的四信号逻辑，与 Router 保持一致。

### BM25 索引复用

为 skill matching 创建一个独立的 `bm25.Index`（`skillBM25`），在 `SetSkillProvider` 时构建，避免与工具路由的 BM25 索引混淆。当 skill 列表变化时（通过 `RefreshSkillIndex()` 方法）重建索引。

### 接线点

- GUI 侧：`gui/tool_router.go` 或 `gui/tool_builder.go` 中，将 `SkillExecutor`（已有）适配为 `SkillProvider` 接口
- TUI 侧：`tui/agent_tools.go` 中同步接线

### 路由日志增强

`writeRouteLog()` 增加 `skillMatchScore float64` 和 `matchedSkills []string` 参数，在日志中输出。

## P1：Skill 自动迭代

### 架构变更

```
SkillAutoSummaryPipeline.RunPipeline()
  ├── Stage 1: AnalyzeComplexity
  ├── Stage 2: DraftSkill
  ├── Stage 2.5: FindSimilarSkill  ← 新增
  │     ├── 匹配到 → Stage 3b: VersionedUpdate
  │     └── 未匹配 → Stage 3: ValidateSkillDraft (现有)
  ├── Stage 3b: VersionedUpdate  ← 新增
  │     ├── 比较新旧质量
  │     ├── 备份旧版本 skill.yaml.v{N}
  │     └── 写入新版本
  ├── Stage 4: RunQualityGate
  └── Stage 5: RunAutoUpload
```

### 新增文件：`corelib/skill/versioner.go`

```go
// Versioner manages skill version history within a skill directory.
type Versioner struct{}

// BackupCurrent backs up the current skill.yaml to skill.yaml.v{N}.
// Returns the version number assigned.
func (v *Versioner) BackupCurrent(skillDir string) (int, error)

// CleanOldVersions keeps only the latest maxVersions backup files.
func (v *Versioner) CleanOldVersions(skillDir string, maxVersions int) error

// LatestVersion returns the highest version number found in skillDir.
func (v *Versioner) LatestVersion(skillDir string) int
```

### 新增方法：`corelib/skill/scanner.go`

```go
// FindSimilarSkill searches all active skills for one similar to the given
// description. Returns the best match and its BM25 score, or nil if no
// match exceeds the threshold.
func FindSimilarSkill(description string, threshold float64) (*NLSkillEntry, float64)
```

实现：扫描 `ScanAllSkillDirs()` 的结果，对每个 skill 构建 `description + triggers` 文本，用 BM25 评分，返回最高分且 > threshold 的 skill。

### Pipeline 改动：`gui/skill_auto_summary.go`

在 `RunPipeline()` 的 Stage 2（DraftSkill）和 Stage 3（ValidateSkillDraft）之间插入：

```go
// Stage 2.5: FindSimilarSkill
existing, score := skill.FindSimilarSkill(draft.Description, 0.6)
if existing != nil {
    // 迭代模式
    if shouldUpdate(draft, existing) {
        versioner := &skill.Versioner{}
        versioner.BackupCurrent(existing.SkillDir)
        // 写入新版本
        ...
        versioner.CleanOldVersions(existing.SkillDir, 5)
    }
    return // 跳过后续新建流程
}
// 未匹配，继续正常新建流程
```

### 质量比较逻辑

```go
func shouldUpdate(newDraft *SkillYAMLFile, existing *NLSkillEntry) bool {
    newSteps := len(newDraft.Steps)
    oldSteps := len(existing.Steps)
    newErrors := countErrorSteps(newDraft.Steps)
    oldErrors := countErrorSteps(existing.Steps)
    
    // 新版本步骤更少（更高效）
    if newSteps < oldSteps { return true }
    // 新版本 error step 更少
    if newErrors < oldErrors { return true }
    return false
}
```

## P2：UsageTracker → Memory 桥梁

### 架构变更

```
UsageTracker (tool_usage.json)
       ↓ 每24小时
UsagePatternBridge.Run()
       ↓ ExtractPatterns(7)
[]UsagePattern
       ↓ 去重 + 写入
Memory Store (memories.json)
  category=project_knowledge
  tags=["usage_pattern", toolName]
```

### UsageTracker 扩展：`corelib/tool/usage_tracker.go`

```go
// UsagePattern describes a high-frequency successful tool usage pattern.
type UsagePattern struct {
    ToolName    string
    TopTokens   []string
    SuccessRate float64
    Count       int
    Description string
}

// ExtractPatterns scans records from the last windowDays and returns
// patterns for tools with success rate > 80% and count > 5.
func (t *UsageTracker) ExtractPatterns(windowDays int) []UsagePattern
```

### 新增文件：`gui/usage_to_memory.go`

```go
// UsagePatternBridge periodically extracts usage patterns and writes
// them to the Memory Store as project_knowledge entries.
type UsagePatternBridge struct {
    tracker *tool.UsageTracker
    memory  *memory.Store
    stopCh  chan struct{}
}

func NewUsagePatternBridge(tracker *tool.UsageTracker, memory *memory.Store) *UsagePatternBridge

// Start begins the 24-hour periodic extraction loop.
func (b *UsagePatternBridge) Start()

// Stop halts the periodic loop.
func (b *UsagePatternBridge) Stop()

// RunOnce executes a single extraction + write cycle (exposed for testing).
func (b *UsagePatternBridge) RunOnce()
```

### 去重策略

`RunOnce()` 中：
1. 调用 `tracker.ExtractPatterns(7)`
2. 对每个 pattern，在 Memory Store 中搜索 tags 包含 `["usage_pattern", pattern.ToolName]` 的条目
3. 如果找到且 content 相同 → 仅 TouchAccess
4. 如果找到但 content 不同 → Update content
5. 如果未找到 → Save 新条目

### 接线点

在 `gui/app.go` 或 `gui/main.go` 的初始化流程中，创建 `UsagePatternBridge` 并调用 `Start()`。在 app 关闭时调用 `Stop()`。

## 文件变更汇总

| 文件 | 变更类型 | 说明 |
|------|----------|------|
| `corelib/tool/router.go` | 修改 | 增加 SkillProvider 接口、四信号融合、日志增强 |
| `corelib/tool/builder.go` | 修改 | 同步支持 SkillProvider |
| `corelib/tool/usage_tracker.go` | 修改 | 增加 ExtractPatterns 方法和 UsagePattern 类型 |
| `corelib/skill/scanner.go` | 修改 | 增加 FindSimilarSkill 方法 |
| `corelib/skill/versioner.go` | 新增 | Skill 版本管理 |
| `gui/skill_auto_summary.go` | 修改 | 插入 FindSimilarSkill + VersionedUpdate 阶段 |
| `gui/usage_to_memory.go` | 新增 | UsagePatternBridge |
| `gui/tool_router.go` 或 `gui/tool_builder.go` | 修改 | 接线 SkillProvider |
| `tui/agent_tools.go` | 修改 | TUI 侧接线 SkillProvider |
| `gui/app.go` | 修改 | 启动 UsagePatternBridge |
