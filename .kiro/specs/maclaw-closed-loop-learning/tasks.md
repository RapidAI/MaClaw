# 任务列表：MacLaw 闭环学习系统

## P0：Skill 反哺 Router

### Task 1: SkillProvider 接口与 Router 集成
- [x] 在 `corelib/tool/router.go` 中定义 `SkillSummary` struct 和 `SkillProvider` interface
- [x] Router struct 增加 `skillProvider` 字段和 `SetSkillProvider()` 方法
- [x] 增加 `skillBM25 *bm25.Index` 字段，用于 skill trigger 匹配
- [x] 新增 `refreshSkillIndex()` 内部方法：从 SkillProvider 获取 skills，构建 BM25 索引
- [x] 在 `Route()` 中增加 skill match 评分：对 `run_skill` 候选工具，用 skillBM25 对 userMessage 评分
- [x] 修改融合公式：当 skillProvider != nil 时使用 `0.5×retrieval + 0.25×experience + 0.15×skill_match + 0.1×priority`
- [x] 当 skill_match_score > 0.3 时，临时修改 `run_skill` 的 description 追加 top-3 匹配 skill 名称
- [x] 编写单元测试：`corelib/tool/router_skill_test.go` — 6 个测试覆盖有/无 SkillProvider、匹配/不匹配、空 provider、enrichment

### Task 2: DynamicToolBuilder 同步支持
- [x] `corelib/tool/builder.go` 中 DynamicToolBuilder 增加 `skillProvider` 字段和 `SetSkillProvider()` 方法
- [x] `Build()` 方法中同步实现四信号融合和 skill description 增强逻辑
- [x] 编写单元测试：`TestDynamicToolBuilder_SkillProvider`

### Task 3: GUI 侧接线
- [x] 在 `gui/tool_router.go` 中增加 `SetSkillProvider()` 委托方法
- [x] 在 `gui/tool_builder.go` 中增加 `SetSkillProvider()` 委托方法
- [ ] 在 Router/Builder 初始化时调用 `SetSkillProvider(adapter)`（需要 SkillExecutor adapter — 被 corelib/browser 预存 build error 阻塞）

### Task 4: TUI 侧接线
- [ ] TUI 侧接线（TUI 的 skill 列表来自 FileConfigStore，需要适配为 SkillProvider — 低优先级）

### Task 5: 路由日志增强
- [x] 修改 `writeRouteLog()` 签名，增加 `skillMatchScore float64` 和 `matchedSkills []string` 参数
- [x] 在日志输出中增加 "Skill match" 行
- [x] 更新 `Route()` 中调用 `writeRouteLog()` 的参数传递
- [x] 修复 `router_body_test.go` 中的 `writeRouteLog` 调用以匹配新签名

## P1：Skill 自动迭代

### Task 6: FindSimilarSkill 方法
- [x] 在 `corelib/skill/scanner.go` 中新增 `FindSimilarSkill(description string, threshold float64) (*NLSkillEntry, float64)` 方法
- [x] 实现：扫描 ScanAllSkillDirs()，对每个 skill 构建 description+triggers 文本，用简化 BM25 评分
- [x] 返回最高分且 > threshold 的 skill，否则返回 nil
- [x] 辅助函数：`tokenizeSimple()`, `bm25ScoreSimple()`

### Task 7: Skill Versioner
- [x] 新建 `corelib/skill/versioner.go`
- [x] 实现 `BackupCurrent(skillDir string) (int, error)`
- [x] 实现 `CleanOldVersions(skillDir string, maxVersions int) error`
- [x] 实现 `LatestVersion(skillDir string) int`
- [x] 编写单元测试：`corelib/skill/versioner_test.go` — 6 个测试全部通过

### Task 8: Pipeline 迭代模式集成
- [x] 在 `gui/skill_auto_summary.go` 的 `RunPipeline()` 中插入 Stage 2.5 FindSimilarSkill 调用
- [x] 实现 `shouldUpdateSkill(newDraft, existing)` 比较逻辑
- [x] 匹配到且应更新时：Versioner.BackupCurrent → 写入新 skill.yaml → CleanOldVersions
- [x] 匹配到但不应更新时：记录日志，跳过
- [x] 未匹配时：走现有新建流程
- [x] 增加结构化日志
- [x] 编写单元测试：`TestShouldUpdateSkill_*` — 4 个测试（在 gui/skill_auto_summary_test.go 中）

## P2：UsageTracker → Memory 桥梁

### Task 9: ExtractPatterns 方法
- [x] 在 `corelib/tool/usage_tracker.go` 中定义 `UsagePattern` struct
- [x] 实现 `ExtractPatterns(windowDays int) []UsagePattern`
- [x] 编写单元测试：`corelib/tool/usage_pattern_test.go` — 6 个测试全部通过

### Task 10: UsagePatternBridge
- [x] 新建 `gui/usage_to_memory.go`
- [x] 实现 `NewUsagePatternBridge(tracker, memory)` 构造函数
- [x] 实现 `RunOnce()`：提取 + 去重 + 写入
- [x] 实现 `Start()`：启动 goroutine，每 24 小时调用 RunOnce()
- [x] 实现 `Stop()`：关闭 goroutine

### Task 11: 接线与启动
- [ ] 在 `gui/app.go` 初始化流程中创建 UsagePatternBridge 并调用 Start()（被 corelib/browser 预存 build error 阻塞）

## 测试汇总

| 测试文件 | 测试数 | 状态 |
|----------|--------|------|
| `corelib/tool/router_skill_test.go` | 7 | 全部通过 |
| `corelib/tool/usage_pattern_test.go` | 6 | 全部通过 |
| `corelib/tool/router_body_test.go` | (已修复签名) | 通过 |
| `corelib/skill/versioner_test.go` | 6 | 全部通过 |
| `gui/skill_auto_summary_test.go` | +4 shouldUpdateSkill | 编译通过（GUI 整体 build 被 browser 包阻塞） |

## 未完成项说明

Task 3（GUI SkillExecutor adapter 接线）和 Task 11（UsagePatternBridge 启动接线）需要在 `gui/app.go` 中添加几行代码，但当前被 `corelib/browser/` 包的预存编译错误阻塞（与本 spec 无关）。接线代码模式：

```go
// Task 3: 在 ensureRemoteInfra() 或 handler 初始化后
if a.skillExecutor != nil && a.toolRouter != nil {
    a.toolRouter.SetSkillProvider(&skillExecutorAdapter{exec: a.skillExecutor})
}

// Task 11: 在 ensureMemoryStore() 之后
if a.usageTracker != nil && a.memoryStore != nil {
    bridge := NewUsagePatternBridge(a.usageTracker, a.memoryStore)
    bridge.Start()
    // 在 shutdown 中调用 bridge.Stop()
}
```
