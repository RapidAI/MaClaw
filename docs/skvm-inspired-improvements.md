# SkVM 论文启发的机制性改进

## 论文概要

**SkVM: Revisiting Language VM for Skills across Heterogenous LLMs and Harnesses**
- 来源：上海交通大学 IPADS 实验室，arXiv:2604.03088，2026-04
- 代码：[github.com/SJTU-IPADS/SkVM](https://github.com/SJTU-IPADS/SkVM)

核心类比：**Skills 是代码，LLMs 是异构处理器**。当前 agent 系统把 skill 当原始文本塞进 context，没有任何适配。SkVM 借鉴编译器设计，通过 AOT 编译（能力画像 + 环境绑定 + 并发提取）和 JIT 运行时（代码固化 + 自适应重编译）让 skill 在异构目标上可移植执行。

关键数据：任务完成率 +15.3%，token 消耗 -40%，代码固化 19-50x 加速，回归率仅 4.5%（原始 skill 15%）。

Content was rephrased for compliance with licensing restrictions. Source: [arxiv.org/html/2604.03088](https://arxiv.org/html/2604.03088)

---

## 机制性分析：MacLaw 的结构性缺口

SkVM 的核心洞察不是"检查 pip 包"或"缓存脚本"——这些是具体手段。核心洞察是：

> **Skill 携带隐式的能力需求，但系统没有机制来表达、度量、适配 skill 需求与目标能力之间的差距。**

MacLaw 的 Skill Runner 有一条从 `StartRun` → `executeAsync` → `executeStepWithContext` 的执行管线。这条管线的结构性问题是：**它是一条单态管线——对所有 (skill, model, environment) 组合使用完全相同的执行路径，没有任何适配层。**

具体表现为三个机制性缺口：

### 缺口 1：Skill 需求与目标能力之间没有契约层

**问题本质**：Skill 的 `steps` 隐式假设了目标的能力（模型能生成正确的 shell 命令、环境有 ffmpeg、Python 有 pdfplumber），但这些假设没有被显式声明。`RequiresPython`/`RequiresNode`/`RequiredEnv` 是三个独立的硬编码字段，每新增一种依赖类型就要加一个字段——这不是机制，是枚举。

**SkVM 的机制**：Primitive Capabilities——一个统一的抽象词汇表，skill 用它声明需求，target 用它声明能力，编译器用它度量差距。

**MacLaw 的机制性修复**：将 `RequiresPython`/`RequiresNode`/`RequiredEnv` 统一为声明式的 **Skill Requirements**，用一个通用的 `requires` 结构表达所有类型的前置条件。Runner 在执行前通过统一的 `CheckRequirements` 接口验证，新增依赖类型只需注册一个 checker，不改 Runner 代码。

### 缺口 2：craft_tool 每次都从零生成——没有执行经验的结构化复用

**问题本质**：`executeCraftToolCore` 的流程是 `generateScript → saveScript → executeScript → verify`。每次调用都走完整流程，即使是完全相同的任务模式（如"将 Markdown 转 PDF"）。`self_repair.go` 的 `AttemptRepair` 处理失败路径，但没有对应的成功路径复用机制。

这不是"缓存"问题——缓存是 workaround（key 怎么算？过期怎么办？参数变了怎么办？）。问题是 **craft_tool 的执行结果没有结构化地回流到 skill 定义中**。SkVM 的 Code Solidification 本质是：当一段代码被验证为正确后，它从"LLM 每次推理生成"提升为"skill 的固定步骤"。

**SkVM 的机制**：三阶段提升——AOT 分析候选 → 运行时验证签名 → 提升为可执行函数。

**MacLaw 的机制性修复**：当 craft_tool 步骤连续成功时，将验证通过的脚本**固化为 bash 步骤写回 skill 定义**。这不是缓存——是 skill 的进化。craft_tool 步骤变成 bash 步骤后，后续执行完全不需要 LLM 推理。失败时通过 `self_repair.go` 的已有机制回退。

### 缺口 3：Skill 注入到 LLM context 时不区分结构类型

**问题本质**：`appendKnowledgeSkillSection` 对所有匹配的 skill 使用相同的注入策略——截断到 token 预算后塞进 system prompt。但 SkVM 发现 skill 有三种结构类型（工具引用 52%、流程指导 28%、内容生成 20%），前两类的正确性取决于是否执行了规定步骤，不取决于 LLM 的生成能力。对这两类 skill，注入完整文档是浪费 token——LLM 只需要知道参数 schema 和步骤摘要。

**SkVM 的机制**：Skill Taxonomy——基于内容结构的分类，不同类型使用不同的编译策略。

**MacLaw 的机制性修复**：在 skill 加载时自动分类（基于 steps 结构和 content 特征），注入时按类型选择策略。executable skill 注入参数 schema + 步骤摘要（已有 `FormatParamSchema`），knowledge skill 注入完整文档。分类是数据驱动的（从 skill 结构推断），不是硬编码的 if-else。

---

## 改进 1：统一的 Skill Requirements 契约

### 根因

`NLSkillEntry` 有 5 个独立的依赖字段：

```go
RequiresPython []string  // pip packages
RequiresNode   []string  // npm packages
RequiredEnv    []string  // environment variables
RequiresTools  []string  // tool names
RequiresGUI    bool      // GUI environment
```

每个字段有独立的检查逻辑散布在 `StartRun`（`RequiredArgs`、`RequiredEnv`）、`executeAsync`（`autoInstallSkillDependencies`）、`checkPlatformCompat`（`Platforms`）、`checkFileReferences` 中。新增一种依赖类型（如 Go module、Rust crate、系统服务）需要：
1. 在 `NLSkillEntry` 加字段
2. 在 `scanner.go` 加解析
3. 在 `StartRun` 或 `executeAsync` 加检查逻辑
4. 在 TUI 的 `toolRunSkill` 加相同的检查逻辑

这是 O(types × consumers) 的维护成本。

### 修复：声明式 Requirements + 可扩展 Checker 注册表

**设计原则**：依赖检查的逻辑不散布在 Runner 中，而是集中在一个注册表里。Runner 只调用 `CheckAll(skill) → []Violation`，不知道具体有哪些 checker。

#### `corelib/skill/requirement.go`（新文件）

```go
// Requirement 是 skill 前置条件的统一表示。
// 所有依赖类型（pip 包、npm 包、环境变量、系统命令、平台、GUI）
// 都表示为 Requirement，由对应的 Checker 验证。
type Requirement struct {
    Type    string // "pip", "npm", "env", "command", "platform", "gui"
    Name    string // 包名/变量名/命令名
    Version string // 版本约束（可选）
    // Source 标记这个 requirement 的来源：
    //   "explicit" = skill.yaml 显式声明
    //   "inferred" = 从 step commands 自动推断
    Source  string
}

// Violation 描述一个未满足的前置条件。
type Violation struct {
    Requirement Requirement
    Message     string // 用户友好的错误信息
    AutoFixable bool   // 是否可自动修复
    Severity    string // "error" (阻止执行) | "warning" (记录但不阻止)
}

// Checker 验证一类 Requirement 是否满足。
// 实现者只需关注自己的类型，不需要知道其他 checker 的存在。
type Checker interface {
    Type() string // 返回此 checker 处理的 Requirement.Type
    Check(req Requirement) *Violation // nil = 满足
    AutoFix(req Requirement) error    // 尝试自动修复，不支持则返回 error
}

// Registry 是 Checker 的注册表。Runner 通过它验证所有 requirements。
type Registry struct {
    checkers map[string]Checker
}

// Register 注册一个 Checker。同一 Type 只能注册一个。
func (r *Registry) Register(c Checker) {
    if r.checkers == nil {
        r.checkers = make(map[string]Checker)
    }
    r.checkers[c.Type()] = c
}

// CheckAll 验证 skill 的所有 requirements，返回所有 violations。
// 这是 Runner 调用的唯一入口——Runner 不知道有哪些 checker。
func (r *Registry) CheckAll(reqs []Requirement) []Violation

// AutoFixAll 尝试自动修复所有 AutoFixable 的 violations。
// 返回修复后仍然存在的 violations。
func (r *Registry) AutoFixAll(violations []Violation) []Violation
```

#### `corelib/skill/requirement_extract.go`（新文件）

```go
// ExtractRequirements 从 NLSkillEntry 中提取所有 requirements。
// 这是从旧字段（RequiresPython/RequiresNode/RequiredEnv/Platforms/RequiresGUI）
// 到统一 Requirement 的桥接层。
//
// 同时从 step commands 中推断隐式依赖（如命令中使用了 ffmpeg 但
// 没有在 requires 中声明）。推断的 requirements 标记 Source="inferred"。
func ExtractRequirements(skill *corelib.NLSkillEntry) []Requirement
```

#### 内置 Checkers

```go
// requirement_checkers.go
type PipChecker struct{}    // Check: pip show; AutoFix: pip install
type NpmChecker struct{}    // Check: npm list; AutoFix: npm install
type EnvChecker struct{}    // Check: os.Getenv; AutoFix: not supported
type CommandChecker struct{} // Check: exec.LookPath; AutoFix: not supported
type PlatformChecker struct{} // Check: runtime.GOOS; AutoFix: not supported
```

#### Runner 集成

`StartRun` 中散布的 5 处检查逻辑替换为：

```go
reqs := cskill.ExtractRequirements(target)
violations := r.requirementRegistry.CheckAll(reqs)
if autoRepair {
    violations = r.requirementRegistry.AutoFixAll(violations)
}
errors := filterErrors(violations)
if len(errors) > 0 {
    return "", formatViolations(errors)
}
```

#### 扩展方式

新增依赖类型（如 Go module）：
1. 实现 `GoModChecker`（`Check` 用 `go list`，`AutoFix` 用 `go install`）
2. `registry.Register(&GoModChecker{})`
3. 不改 Runner、不改 NLSkillEntry、不改 scanner

#### 与现有机制的关系

- `RequiresPython`/`RequiresNode`/`RequiredEnv` 字段保留（向后兼容），`ExtractRequirements` 桥接
- `autoInstallSkillDependencies` 被 `AutoFixAll` 替代（逻辑相同，但通过 checker 注册表分发）
- `checkPlatformCompat` 被 `PlatformChecker` 替代
- `classifySkillStepError` 的 `ErrCommandNotFound`/`ErrMissingEnvVar` 仍然需要（运行时错误分类），但执行前检查减少了它们被触发的频率
- TUI 的 `toolRunSkill` 调用同一个 `registry.CheckAll`，不再需要独立实现检查逻辑

---

## 改进 2：craft_tool 成功脚本固化为 bash 步骤

### 根因

`craft_tool` 的执行流程：

```
LLM 生成脚本 → 保存到 ~/.maclaw/crafted_tools/ → 执行 → 验证
```

每次调用都走完整流程。但 75% 的 skill 包含结构固定的代码（SkVM 数据），只有参数变化。当 craft_tool 连续成功时，生成的脚本已经被验证为正确——它应该被提升为 skill 的固定步骤，不再需要 LLM 推理。

这不是缓存——缓存有 key 匹配、过期、一致性问题。这是 **skill 的进化**：craft_tool 是 skill 的"解释执行"模式，bash 是"编译执行"模式。SkVM 的 Code Solidification 本质就是这个提升过程。

### 修复：Solidification Pipeline

#### 阶段 1：记录（每次 craft_tool 执行后）

`executeStepWithContext` 的 `craft_tool` 分支在成功后，将脚本和参数记录到 skill 的 `SolidificationCandidates`：

```go
// corelib/types.go
type SolidificationCandidate struct {
    StepIndex    int               `json:"step_index"`
    ScriptPath   string            `json:"script_path"`
    Language     string            `json:"language"`
    ParamSlots   []string          `json:"param_slots"`   // 从脚本中提取的参数槽位
    SuccessCount int               `json:"success_count"`  // 连续成功次数
    LastUsed     string            `json:"last_used"`
}

// NLSkillEntry 新增：
SolidificationCandidates []SolidificationCandidate `json:"solidification_candidates,omitempty"`
```

#### 阶段 2：提升（连续 N 次成功后）

当 `SuccessCount >= SolidificationThreshold`（默认 3）时，将 craft_tool 步骤替换为 bash 步骤：

```go
// corelib/skill/solidify.go

const SolidificationThreshold = 3

// TrySolidify 检查 skill 的 craft_tool 步骤是否可以固化。
// 返回 true 表示 skill 被修改（调用方需要持久化）。
//
// 固化过程：
// 1. 检查 candidate 的 SuccessCount >= threshold
// 2. 将 craft_tool 步骤替换为 bash 步骤（command = 脚本路径 + 参数槽位）
// 3. 保留原始 craft_tool 步骤在 FallbackStep 中（失败时回退）
func TrySolidify(skill *corelib.NLSkillEntry) bool
```

#### 阶段 3：回退（固化后执行失败时）

bash 步骤执行失败时，检查是否有 `FallbackStep`。有则回退到 craft_tool 重新生成：

```go
// NLSkillStep 新增：
FallbackStep *NLSkillStep `json:"fallback_step,omitempty"` // 固化前的原始步骤
```

`executeStepWithContext` 的 bash 分支在失败后检查 `FallbackStep`：

```go
if step.FallbackStep != nil && step.FallbackStep.Action == "craft_tool" {
    log.Printf("[skill-runner] solidified step failed, falling back to craft_tool")
    // 重置 solidification candidate
    resetSolidificationCandidate(skill, stepIndex)
    // 执行 fallback step
    return r.executeStepWithContext(ctx, *step.FallbackStep, ...)
}
```

#### 与现有机制的关系

- 复用 `self_repair.go` 的失败追踪——`ShouldAttemptRepair` 在固化步骤失败时也适用
- 复用 `verifyCraftExecution` 的验证逻辑——固化前的验证标准不变
- 不影响 `craft_tool` 的正常流程——固化是渐进的，前 N 次仍走 LLM 生成
- `manage_skill(action=patch)` 可以手动触发/撤销固化

#### 预期收益

- 固化后的步骤执行时间从 10-30s（LLM 生成 + 执行）降到 <1s（直接执行）
- 完全消除 craft_tool 的 API 调用——降低 rate limit 压力（#20）
- Skill 随使用自动进化——从"需要 LLM 推理"到"直接执行"

---

## 改进 3：Skill 注入的结构感知策略

### 根因

`appendKnowledgeSkillSection` 对所有 skill 使用相同的注入模板：

```
### Skill: {name}
{description}
{content or SKILL.md}
```

但 executable skill 的 SKILL.md 通常是使用手册（"先生成 XML 再调用 run.js"），LLM 需要的是参数 schema 和步骤摘要，不是完整手册。完整手册浪费 token，且可能干扰 LLM 的决策（手册中的示例被当作指令执行）。

### 修复：基于 Skill 结构的注入策略

#### Skill 结构分类

在 `scanner.go` 的 `loadSkillFromDir` 中，基于已有数据自动分类：

```go
// corelib/skill/taxonomy.go

type SkillTaxonomy string

const (
    // TaxonomyExecutable: 有 steps 的 skill，LLM 不需要看完整文档，
    // 只需要参数 schema + 步骤摘要。Runner 负责执行。
    TaxonomyExecutable SkillTaxonomy = "executable"

    // TaxonomyKnowledge: 无 steps 的 knowledge skill，LLM 需要完整文档
    // 来指导自己的行为。
    TaxonomyKnowledge SkillTaxonomy = "knowledge"

    // TaxonomyCraftable: 有 craft_tool 步骤的 skill，LLM 需要任务描述
    // 来生成脚本。注入任务模板 + 参数 schema。
    TaxonomyCraftable SkillTaxonomy = "craftable"
)

// ClassifySkill 基于 skill 的结构特征自动分类。
// 分类完全基于数据（steps 是否存在、steps 的 action 类型），
// 不依赖关键词匹配。
func ClassifySkill(skill *corelib.NLSkillEntry) SkillTaxonomy {
    if len(skill.Steps) == 0 {
        return TaxonomyKnowledge
    }
    for _, step := range skill.Steps {
        if step.Action == "craft_tool" {
            return TaxonomyCraftable
        }
    }
    return TaxonomyExecutable
}
```

#### 注入策略

```go
// FormatSkillForContext 根据 skill 的分类生成 LLM context 注入文本。
// 不同分类使用不同的注入模板，优化 token 效率。
func FormatSkillForContext(skill *corelib.NLSkillEntry, taxonomy SkillTaxonomy) string {
    switch taxonomy {
    case TaxonomyExecutable:
        // 参数 schema + 步骤摘要（不注入完整 SKILL.md）
        return formatExecutableSkillContext(skill)
    case TaxonomyKnowledge:
        // 完整文档（现有行为）
        return formatKnowledgeSkillContext(skill)
    case TaxonomyCraftable:
        // 任务模板 + 参数 schema
        return formatCraftableSkillContext(skill)
    }
    return ""
}

func formatExecutableSkillContext(skill *corelib.NLSkillEntry) string {
    var b strings.Builder
    b.WriteString("### Skill: ")
    b.WriteString(skill.Name)
    b.WriteString("\n")
    b.WriteString(skill.Description)
    b.WriteString("\n")

    // 参数 schema（已有 FormatParamSchema）
    params := skill.Params
    if len(params) == 0 {
        params = SynthesizeParams(skill.Steps, skill.RequiredArgs)
    }
    if schema := FormatParamSchema(params); schema != "" {
        b.WriteString(schema)
    }

    // 步骤摘要（不是完整 SKILL.md）
    b.WriteString("执行步骤:\n")
    for i, step := range skill.Steps {
        b.WriteString(fmt.Sprintf("  %d. %s", i+1, step.Action))
        if step.Name != "" {
            b.WriteString(": ")
            b.WriteString(step.Name)
        }
        b.WriteString("\n")
    }

    return b.String()
}
```

#### 与现有机制的关系

- `appendKnowledgeSkillSection` 的 Category 1（knowledge skill）和 Category 2（executable skill + SKILL.md）分支替换为 `FormatSkillForContext` 调用
- `FormatParamSchema`（已有）被 executable 和 craftable 策略复用
- Token 预算机制不变——`FormatSkillForContext` 的输出仍受 token 预算截断
- 分类是自动的（从 skill 结构推断），不需要 skill 作者声明

#### 预期收益

- Executable skill 的注入 token 从完整 SKILL.md（500-2000 token）降到参数 schema + 步骤摘要（50-200 token）
- LLM 不再被 SKILL.md 中的示例干扰（如 drawio-skill 的 SKILL.md 包含 XML 示例，LLM 可能直接复制示例而非生成正确内容）
- Knowledge skill 行为不变

---

## 修改文件清单

### 改进 1：统一 Requirements

| 文件 | 操作 | 说明 |
|------|------|------|
| `corelib/skill/requirement.go` | 新增 | `Requirement`、`Violation`、`Checker` 接口、`Registry` |
| `corelib/skill/requirement_extract.go` | 新增 | `ExtractRequirements`：从 NLSkillEntry 桥接 + 从 steps 推断 |
| `corelib/skill/requirement_checkers.go` | 新增 | 5 个内置 Checker（pip/npm/env/command/platform） |
| `corelib/skill/requirement_test.go` | 新增 | 测试 |
| `gui/skill_runner.go` | 修改 | `StartRun` 中 5 处散布的检查逻辑替换为 `registry.CheckAll` |
| `gui/skill_runner.go` | 修改 | `executeAsync` 中 `autoInstallSkillDependencies` 替换为 `registry.AutoFixAll` |

### 改进 2：craft_tool 固化

| 文件 | 操作 | 说明 |
|------|------|------|
| `corelib/types.go` | 修改 | `NLSkillEntry` 新增 `SolidificationCandidates`；`NLSkillStep` 新增 `FallbackStep` |
| `corelib/skill/solidify.go` | 新增 | `TrySolidify`、`RecordCraftSuccess`、`ResetCandidate` |
| `corelib/skill/solidify_test.go` | 新增 | 测试 |
| `gui/skill_runner.go` | 修改 | `executeStepWithContext` craft_tool 成功后调用 `RecordCraftSuccess`；bash 失败后检查 `FallbackStep` |

### 改进 3：结构感知注入

| 文件 | 操作 | 说明 |
|------|------|------|
| `corelib/skill/taxonomy.go` | 新增 | `SkillTaxonomy`、`ClassifySkill`、`FormatSkillForContext` |
| `corelib/skill/taxonomy_test.go` | 新增 | 测试 |
| `gui/im_system_prompt.go` | 修改 | `appendKnowledgeSkillSection` 调用 `FormatSkillForContext` |

---

## 验收标准

### 改进 1
- 新增 Go module checker 只需实现 `Checker` 接口 + `Register`，不改 Runner
- `StartRun` 中不再有 `RequiresPython`/`RequiresNode`/`RequiredEnv` 的独立检查逻辑
- TUI 和 GUI 共享同一个 `Registry`
- 所有现有 skill_runner 测试通过

### 改进 2
- craft_tool 步骤连续成功 3 次后，skill 定义中该步骤变为 bash 步骤
- 固化后的步骤执行不调用 LLM API
- 固化步骤失败时自动回退到 craft_tool 重新生成
- `manage_skill(action=patch)` 可以手动撤销固化

### 改进 3
- Executable skill 注入 token 减少 60%+（参数 schema + 步骤摘要 vs 完整 SKILL.md）
- Knowledge skill 注入行为不变
- 分类不依赖关键词——纯基于 skill 结构（steps 是否存在、action 类型）
