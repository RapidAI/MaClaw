# MacLaw Skill 全生命周期机制分析与改进计划

## 调查范围

本文从机制层面分析 MacLaw skill 系统的六个生命周期阶段：**发现 → 安装 → 调用 → 错误处理 → 自修复 → 自改进**，识别每个阶段的结构性断裂点，制定机制性修复方案。

---

## 一、当前架构全景

### 数据流总览

```
用户消息
  → LLM 决策调用 manage_skill(action=run, name="xxx", args={...})
  → GUI: toolManageSkill → skillRun → SkillRunner.StartRun → executeAsync
  → TUI: newManageSkillHandler → skillRun → toolRunSkill (同步)
  → 步骤执行: resolveSkillStep → substituteSkillVariables → runBashStep/executeCraftTool
  → 结果: updateUsageStats → (nudge injection) → 返回 LLM
  → 失败时: classifyBashError → (capability gap detection) → (self-repair?)
```

### 关键模块映射

| 生命周期阶段 | 核心模块 | 状态 |
|-------------|---------|------|
| 发现/搜索 | `corelib/skill/hub_search.go` HubClient.SearchAll | ✅ 三源统一 + 本地历史重排序 |
| 安装 | `gui/app_nl_skills.go` + `tui/tool_manage_skill.go` | ✅ 基本完整 |
| 调用/执行 | `gui/skill_runner.go` SkillRunner + `tui/tool_manage_skill.go` skillRun | ⚠️ 双路径，共享层已提取 |
| 错误分类 | `corelib/skill/error_classifier.go` ClassifyStepError | ✅ 统一分类器 |
| 自修复 | `corelib/skill/self_repair.go` | ✅ GUI + TUI 均已接入 |
| 自改进/学习 | `corelib/tool/usage_tracker.go` + `nudge/nudge.go` | ✅ 闭环（nudge 与 self-repair 协调） |
| 参数传递 | `corelib/skill/substitute.go` 共享替换引擎 | ⚠️ 共享层已提取，参数契约层待实施 |

---

## 二、六个机制性问题

### 问题 1 (P0): GUI 自修复未接入——self_repair.go 是死代码

**现状**：

`corelib/skill/self_repair.go` 有完整的自修复骨架：
- `ShouldAttemptRepair()` — 判断 skill 是否需要修复（UsageCount≥3, 成功率<50%, RepairAttemptCount<3）
- `AttemptRepairWithContext()` — LLM 分析失败 trace 并提出修复方案
- `ApplyRepair()` — 将修复写回 skill entry
- `MarkRepairVerified()` / `ResetRepairCount()` — 验证和重置

**TUI 已接入**：`tui/commands/nlskill.go:291` 在 skill 执行失败后调用 `maybeRepairSkillTUI()`，后台 goroutine 执行修复。

**GUI 完全未接入**：在 `gui/` 目录下 grep `maybeRepair|ShouldAttemptRepair|AttemptRepair|self_repair` 返回 **零结果**。`gui/skill_runner.go` 的 `updateUsageStats()` 只更新计数器，不触发自修复。

**影响**：GUI 是主要使用路径（桌面面板 + IM 通道），大量 skill 失败数据被记录但从不触发修复。`memento-skills-inspired-improvements.md` Phase 2 标注"✅ 完成（corelib + GUI Runner 接入）"，但实际 GUI Runner 接入代码不存在。

**修复方案**：

```go
// gui/skill_runner.go — updateUsageStats() 末尾新增
func (r *SkillRunner) updateUsageStats(skill *corelib.NLSkillEntry, execErr error) {
    // ... 现有统计更新逻辑 ...

    // 触发自修复（异步，不阻塞）
    if execErr != nil && skill.LastError != "" {
        go r.maybeRepairSkill(skill)
    }
}

func (r *SkillRunner) maybeRepairSkill(entry *corelib.NLSkillEntry) {
    if !cskill.ShouldAttemptRepair(entry) {
        return
    }
    repairer := r.buildLLMRepairer()
    if repairer == nil || !repairer.IsConfigured() {
        return
    }
    // Deep copy Steps 和 RepairHistory 防数据竞态
    entryCopy := *entry
    entryCopy.Steps = append([]corelib.NLSkillStep(nil), entry.Steps...)
    entryCopy.RepairHistory = append([]corelib.SkillRepairRecord(nil), entry.RepairHistory...)

    result, err := cskill.AttemptRepair(repairer, &entryCopy)
    if err != nil {
        log.Printf("[skill-repair-gui] repair failed for %q: %v", entry.Name, err)
        return
    }
    if cskill.ApplyRepair(&entryCopy, result) {
        r.executor.mu.Lock()
        skills := r.executor.loadSkills()
        for i, s := range skills {
            if s.Name == entryCopy.Name {
                skills[i].Steps = entryCopy.Steps
                skills[i].Status = entryCopy.Status
                skills[i].LastError = entryCopy.LastError
                skills[i].RepairAttemptCount = entryCopy.RepairAttemptCount
                skills[i].LastRepairAt = entryCopy.LastRepairAt
                skills[i].RepairHistory = entryCopy.RepairHistory
                break
            }
        }
        _ = r.executor.saveSkills(skills)
        r.executor.mu.Unlock()
        log.Printf("[skill-repair-gui] repaired skill %q", entryCopy.Name)
    }
}
```

**机制性保证**：GUI 和 TUI 共享 `corelib/skill/self_repair.go` 的修复逻辑，只是触发点不同。修复后两条路径行为一致。

---

### 问题 2 (P0): 参数契约缺失——LLM 和 Skill 之间没有共享的参数 schema

**已在 `skill-runner-mechanism-fix.md` 问题 4 中详细分析**。这里只补充与其他问题的交叉影响：

- **与自修复的交叉**：`AttemptRepairWithContext` 的 `RepairContext.RunArgs` 记录了 LLM 传入的参数，但修复 LLM 不知道这些参数名是否正确（可能是 LLM 猜错了参数名导致的失败，而非 skill 本身的 bug）。参数契约层建立后，`RepairContext` 应该同时包含 `DeclaredParams`（skill 声明的参数 schema）和 `ActualArgs`（LLM 传入的参数），让修复 LLM 能区分"参数名错误"和"skill 逻辑错误"。

- **与 nudge 的交叉**：`SkillFailureWorkaround` nudge 提示 LLM "Consider patching it with manage_skill(action=patch)"，但 LLM 不知道该 patch 什么。如果失败原因是参数名不匹配，patch 应该修改 skill.yaml 的 `params` 声明（添加别名），而非修改 command 模板。

---

### 问题 3 (P1): 错误分类分散——三套独立的错误模式库

**现状**：

| 模块 | 函数 | 位置 | 覆盖的错误模式 |
|------|------|------|--------------|
| GUI Runner | `classifyBashError()` | `gui/skill_runner.go` | exit 9009/127, HTTP 429, ENOENT, python/node 缺失 |
| TUI Runner | `classifySkillStepError()` | `tui/agent_tools.go` | session_not_found, auth_error, timeout, network_error, ENOENT, HTTP 429, context deadline |
| Self-Repair | `nonRepairableErrorClasses` | `corelib/skill/self_repair.go` | rate_limit, network_error |
| Capability Gap | `gapKeywords` | `gui/capability_gap_detector.go` | "无法", "不支持", "cannot", "unable" |

**断裂点**：

1. GUI 的 `classifyBashError` 和 TUI 的 `classifySkillStepError` 有不同的错误模式集合。GUI 识别 `exit 9009` 但 TUI 不识别；TUI 识别 `session_not_found` 但 GUI 不识别。
2. Self-Repair 的 `nonRepairableErrorClasses` 只有 2 个条目，与 Runner 的分类结果不对齐。Runner 分类出 `timeout` 但 Self-Repair 不知道 `timeout` 是否可修复。
3. 错误分类结果不传递给 Self-Repair。`updateUsageStats` 只存储 `execErr.Error()`（原始错误字符串），不存储分类后的 `errorClass`。Self-Repair 的 `RepairContext.ErrorClass` 需要调用方手动填充，但 GUI Runner 没有填充它（因为 GUI 没接入 Self-Repair）。

**修复方案**：统一错误分类到 `corelib/skill/error_classifier.go`

```go
// corelib/skill/error_classifier.go

// ErrorClass 是错误分类的枚举
type ErrorClass string

const (
    ErrCommandNotFound  ErrorClass = "command_not_found"   // exit 9009/127
    ErrRateLimit        ErrorClass = "rate_limit"          // HTTP 429
    ErrFileNotFound     ErrorClass = "file_not_found"      // ENOENT
    ErrTimeout          ErrorClass = "timeout"             // context deadline
    ErrNetworkError     ErrorClass = "network_error"       // connection refused/reset
    ErrAuthError        ErrorClass = "auth_error"          // 401/403
    ErrSessionNotFound  ErrorClass = "session_not_found"   // session expired
    ErrUnknown          ErrorClass = "unknown"
)

// ClassifiedError 包含分类结果和用户友好提示
type ClassifiedError struct {
    Class       ErrorClass
    UserMessage string  // 用户友好的错误提示
    Repairable  bool    // 是否值得尝试自修复
    Retryable   bool    // 是否值得自动重试
}

// ClassifyStepError 统一的错误分类函数
// GUI 和 TUI 的 Runner 都调用这一个函数
func ClassifyStepError(exitCode int, output string, err error) ClassifiedError { ... }
```

GUI 的 `classifyBashError` 和 TUI 的 `classifySkillStepError` 都委托给 `ClassifyStepError`。Self-Repair 的 `nonRepairableErrorClasses` 改为读取 `ClassifiedError.Repairable`。

---

### 问题 4 (P1): craft_tool 产出不持久化——一次性脚本无法复用

**现状**：

`craft_tool` 步骤类型让 LLM 动态生成脚本并执行。执行成功后，脚本被丢弃。下次遇到相同类型的任务，LLM 需要重新生成。

`memento-skills-inspired-improvements.md` Phase 3 标注"✅ corelib 层完成（PersistCraftedSkill + 去重）"，`corelib/skill/craft_to_skill.go` 已实现 `PersistCraftedSkill()` 函数。但 **GUI 的 craft_tool 调用点未接入**——`buildCraftSuccessResult` 只做内存注册（`registerCraftedSkillEntry`），不调用 `PersistCraftedSkill`。

**影响**：
- 每次 craft_tool 成功执行消耗 1 次 LLM 调用（生成脚本）+ 执行时间
- 相同任务重复执行时，LLM 可能生成不同质量的脚本（不稳定）
- 用户无法在 skill 列表中看到和管理 crafted skill

**修复方案**：

在 `gui/skill_runner.go` 的 `executeCraftToolCore` 成功路径中调用 `PersistCraftedSkill`：

```go
// gui/skill_runner.go — executeCraftToolCore 成功后
if execErr == nil && scriptContent != "" {
    go func() {
        result, err := cskill.PersistCraftedSkill(
            skill.Name, skill.Description, scriptContent, scriptExt, skillDir)
        if err != nil {
            log.Printf("[craft-persist] failed to persist crafted skill: %v", err)
            return
        }
        if result.IsUpdate {
            log.Printf("[craft-persist] updated existing skill %q", result.SkillName)
        } else {
            log.Printf("[craft-persist] created new skill %q at %s", result.SkillName, result.SkillDir)
        }
    }()
}
```

**去重机制**（已实现）：`PersistCraftedSkill` 内部用 BM25 对比已有 crafted skill 的描述，阈值 3.0 以上视为同一 skill 的更新而非新建。

---

### 问题 5 (P1): Nudge 系统是单向提示——LLM 收到建议但无法自动执行

**现状**：

`nudge/nudge.go` 在三种场景下注入系统消息：
1. `ComplexTask`：≥5 次工具调用后提示"Consider saving the approach as a skill"
2. `SkillFailureWorkaround`：skill 失败后提示"Consider patching it with manage_skill(action=patch)"
3. `UserCorrection`：用户纠正后提示"Consider saving this as a memory entry or skill"

**断裂点**：

1. **Nudge 是英文的**：系统面向中文用户，但 nudge 消息全是英文。LLM 可能不理解或忽略。
2. **Nudge 只是提示，不是指令**：LLM 收到"Consider patching..."后，需要自己决定是否执行 `manage_skill(action=patch)`。大多数情况下 LLM 会忽略这个低优先级的系统消息，继续回答用户的下一个问题。
3. **Nudge 不包含具体的修复信息**：`SkillFailureWorkaround` 只告诉 LLM skill 名字，不告诉它失败原因、错误分类、建议的 patch 内容。LLM 即使想 patch，也不知道该改什么。
4. **Nudge 注入时机在响应之后**：`injectNudgeMessages` 在 agent loop 的末尾调用，注入到对话历史中供**下一轮**使用。但下一轮用户可能问了完全不同的问题，nudge 的上下文已经丢失。

**修复方案**：从"被动提示"升级为"主动修复触发"

Nudge 系统的定位应该从"提示 LLM 去做"变为"系统自动做，通知 LLM 结果"：

```
当前: skill 失败 → nudge 提示 LLM "你应该 patch" → LLM 大概率忽略
改后: skill 失败 → self_repair 自动修复 → 修复结果注入下一轮 system prompt
```

具体改动：
1. `SkillFailureWorkaround` nudge 改为中文，包含错误分类和建议操作
2. 当 self-repair 成功修复了 skill 后，在下一轮 system prompt 中注入通知："Skill「xxx」在上次执行中失败，系统已自动修复（修改了步骤 2 的超时设置）。下次调用将使用修复后的版本。"
3. 当 self-repair 无法修复（`RepairAttemptCount >= 3`）时，nudge 包含具体的错误信息和建议的 patch 内容，让 LLM 有足够信息执行 `manage_skill(action=patch)`

---

### 问题 6 (P2): GUI/TUI 执行路径能力不对等——TUI 缺少多项 Runner 能力

**现状**：

| 能力 | GUI (`skill_runner.go`) | TUI (`tool_manage_skill.go`) |
|------|------------------------|------------------------------|
| 异步执行 | ✅ goroutine + runID | ❌ 同步阻塞 |
| 进度回调 | ✅ onProgress | ❌ 无 |
| Operations 路由 | ✅ selectedSteps | ✅ |
| Poll 轮询 | ✅ executeStepWithPoll | ✅ runSkillStepWithPollTUI |
| When 条件 | ✅ evaluateSimpleCondition | ✅ evaluateSimpleConditionTUI |
| 变量 Capture | ✅ captureOutputVariables | ✅ captureOutputVariablesTUI |
| 自修复触发 | ❌ (问题 1) | ✅ maybeRepairSkillTUI |
| 参数绑定 | ⚠️ substituteSkillVariables | ❌ 不做变量替换 |
| 安全评估 | ✅ RiskAssessor | ❌ 无 |
| 审计日志 | ✅ AuditLog | ❌ 无 |
| 自动上传 | ✅ AutoUploadTrigger | ❌ 无 |
| 8.3 短路径 | ✅ normalizeWindowsShortPath | ✅ normalizeWindowsShortPathTUI |
| Bash/CMD 选择 | ✅ needsBashShell | ✅ needsBashShellTUI |

**断裂点**：

1. **TUI 不做变量替换**：`tui/tool_manage_skill.go` 的 `skillRun` 直接执行步骤，不调用 `substituteSkillVariables`。LLM 传入的 `args` 不会替换 command 模板中的占位符。
2. **TUI 无安全评估**：Hub 安装的 skill 在 TUI 中不经过 `RiskAssessor`，critical 风险的 skill 可以直接执行。
3. **TUI 无审计日志**：skill 执行不记录到审计日志，无法追溯。
4. **重复实现**：`needsBashShellTUI`、`normalizeWindowsShortPathTUI`、`evaluateSimpleConditionTUI`、`captureOutputVariablesTUI` 等函数是 GUI 对应函数的 copy-paste，维护两份代码。

**修复方案**：提取共享执行层到 `corelib/skill/runner.go`

这与 `skill-runner-mechanism-fix.md` 问题 4 的修复 7 对齐——将核心执行逻辑提取到 `corelib/skill/`，GUI 和 TUI 都委托给共享层：

```
corelib/skill/runner.go:
  - ResolveStep()          — 参数绑定 + 模板替换
  - NeedsBashShell()       — Shell 选择
  - NormalizeWindowsPath() — 8.3 路径规范化
  - CaptureOutputVars()    — 变量捕获
  - EvaluateCondition()    — When 条件
  - ClassifyStepError()    — 错误分类（问题 3）

gui/skill_runner.go:
  - 异步执行、进度回调、审计日志、安全评估（GUI 专有）
  - 委托 corelib/skill 做核心逻辑

tui/tool_manage_skill.go:
  - 同步执行（TUI 专有）
  - 委托 corelib/skill 做核心逻辑
```

**删除的重复代码**：
- `needsBashShellTUI` → `corelib/skill.NeedsBashShell`
- `normalizeWindowsShortPathTUI` → `corelib/skill.NormalizeWindowsPath`
- `evaluateSimpleConditionTUI` → `corelib/skill.EvaluateCondition`
- `captureOutputVariablesTUI` → `corelib/skill.CaptureOutputVars`
- `classifySkillStepError` → `corelib/skill.ClassifyStepError`

---

## 三、交叉问题：六个阶段之间的信号断裂

上面六个问题是各阶段内部的断裂。更严重的是**阶段之间的信号不流通**：

### 断裂 A: 执行结果不回流到发现/安装

**现象**：用户安装了 `any2pdf` skill，执行 3 次全部失败（依赖缺失）。用户再次搜索 "pdf 转换" 时，`any2pdf` 仍然排在搜索结果前列。

**根因**：`HubClient.SearchAll()` 的排序完全基于 Hub 侧的元数据（star 数、下载量），不考虑本地执行历史。`UsageTracker` 记录了 `any2pdf` 的 0% 成功率，但搜索结果不查询 `UsageTracker`。

**修复**：`SearchAll` 返回结果后，用 `UsageTracker.OutcomeScore` 对已安装 skill 的搜索结果做重排序。低成功率的 skill 降权，高成功率的升权。

### 断裂 B: 自修复结果不回流到 LLM context

**现象**：`self_repair` 在后台修复了 skill，但 LLM 不知道。下次 LLM 调用这个 skill 时，仍然用旧的参数/方式调用，可能再次触发同样的错误。

**根因**：`ApplyRepair` 修改了 skill 的 Steps，但没有通知 LLM。LLM 的 system prompt 中注入的 SKILL.md 文档是修复前的版本。

**修复**：修复成功后，在 `SkillMemory.BuildCapabilitySummary` 中注入修复通知。或者更简单——修复后更新 SKILL.md 文档（如果有的话），下次 `appendKnowledgeSkillSection` 自然注入新版本。

### 断裂 C: Nudge 建议不回流到自修复

**现象**：`SkillFailureWorkaround` nudge 提示 LLM patch skill，但 LLM 忽略了。同时 `self_repair` 也没有触发（因为 GUI 未接入）。两个修复机制互不知道对方的存在。

**根因**：Nudge 和 Self-Repair 是两条独立的修复路径，没有协调机制。

**修复**：建立优先级——Self-Repair 是自动的（后台执行），Nudge 是手动的（提示 LLM）。Self-Repair 先执行，成功则不发 Nudge；Self-Repair 失败或不适用时，才发 Nudge 提示 LLM 手动 patch。

```go
// 修复后的流程
skill 失败
  → ClassifyStepError() 统一分类
  → if Repairable:
      → ShouldAttemptRepair() → AttemptRepairWithContext() → ApplyRepair()
      → 成功: 注入修复通知到下一轮 system prompt，不发 nudge
      → 失败: 发 nudge（包含错误分类 + 建议 patch 内容）
  → if !Repairable (rate_limit/network_error):
      → 不修复，不 nudge（外部因素，等待重试）
```

---

## 四、改进计划（优先级排序）

### P0: 必须修复（影响核心功能）

| # | 问题 | 修复 | 预估工作量 | 依赖 |
|---|------|------|-----------|------|
| 1 | GUI 自修复未接入 | `skill_runner.go` 的 `updateUsageStats` 触发 `maybeRepairSkill` | 0.5 天 | 无 |
| 2 | 参数契约缺失 | 实施 `skill-runner-mechanism-fix.md` 问题 4 的完整方案 | 3-4 天 | 无 |

### P1: 应该修复（影响可靠性和一致性）

| # | 问题 | 修复 | 预估工作量 | 依赖 |
|---|------|------|-----------|------|
| 3 | 错误分类分散 | 统一到 `corelib/skill/error_classifier.go` | 1 天 | 无 |
| 4 | craft_tool 不持久化 | GUI 调用点接入 `PersistCraftedSkill` | 0.5 天 | 无 |
| 5 | Nudge 单向提示 | 与 self-repair 协调，升级为主动修复触发 | 1 天 | #1, #3 |
| 6 | GUI/TUI 能力不对等 | 提取共享执行层到 `corelib/skill/` | 2-3 天 | #3 |

### P2: 可以改进（提升体验）

| # | 问题 | 修复 | 预估工作量 | 依赖 |
|---|------|------|-----------|------|
| A | 执行结果不回流搜索 | SearchAll 结果用 OutcomeScore 重排序 | 0.5 天 | 无 |
| B | 自修复结果不回流 LLM | 修复通知注入 system prompt | 0.5 天 | #1 |
| C | Nudge 与 Self-Repair 不协调 | 建立优先级协调机制 | 0.5 天 | #1, #5 |

### 实施顺序

```
Week 1:
  #1 GUI 自修复接入 (0.5d)
  #3 统一错误分类 (1d)
  #4 craft_tool 持久化接入 (0.5d)
  #A 搜索结果重排序 (0.5d)

Week 2:
  #2 参数契约层 (3-4d) — 这是最大的改动，需要完整的一周

Week 3:
  #5 Nudge 升级 (1d)
  #6 共享执行层提取 (2-3d)
  #B 自修复结果回流 (0.5d)
  #C Nudge/Self-Repair 协调 (0.5d)
```

**总预估**：10-12 天。#2（参数契约层）是关键路径，其余可并行。

---

## 五、机制性设计原则（贯穿所有修复）

1. **单一代码路径**：GUI 和 TUI 共享 `corelib/skill/` 的核心逻辑，不维护两份实现
2. **数据驱动而非硬编码**：参数 schema 从 YAML 声明或模板自动合成，错误分类从模式库匹配，不硬编码 fallback 映射
3. **错误在执行前暴露**：必需参数缺失、依赖缺失等问题在执行前检测并返回明确错误，不静默产出垃圾
4. **信号闭环**：执行结果 → 错误分类 → 自修复 → 结果通知 → LLM context，每个环节的输出是下一个环节的输入
5. **渐进降级**：自动修复 → 手动 patch 提示 → 用户介入，每层失败后降级到下一层，不直接放弃
