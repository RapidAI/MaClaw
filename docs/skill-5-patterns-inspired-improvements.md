# Skills 5 种架构设计模式启发的 Skill Runner 改进

## 文章来源

**Skills 的 5 种架构设计模式**（玄姐聊AGI，2026-04-29）

基于 OpenAI、Google Labs、Trail of Bits 等 7 个顶级 Skill 仓库的深度分析，提炼出 5 种经过验证的设计模式。核心观点：Skill 不是 Prompt 堆砌，而是让 LLM 在特定场景下"知道该做什么、知道怎么做"的知识注入体系。

Content was rephrased for compliance with licensing restrictions.
Source: https://mp.weixin.qq.com/s/E3qtUVkUiYXh7ZBCl5ioqg

---

## 5 种模式概要

| 模式 | 适用场景 | 代表 | 核心结构 |
|------|---------|------|---------|
| 1. 线性流程 | 部署、安装、迁移 | OpenAI vercel-deploy (77行) | Prerequisites -> Steps -> Fallback -> Troubleshooting |
| 2. 决策树+按需加载 | 大平台选型、产品导航 | OpenAI cloudflare-deploy (224行) | Auth -> Decision Trees -> Product Index + references/ |
| 3. 循环迭代 | TDD、代码审查 | obra test-driven-development (371行) | Iron Law -> Red-Green-Refactor Loop -> Verification Checklist |
| 4. 接力棒循环 | 跨 Session 长期项目 | Google Labs stitch-loop (203行) | Read Baton -> Execute -> Update -> Prepare Next Baton |
| 5. 多阶段+检查点 | 复杂项目编排 | Dean Peters discovery-process (502行) | Phase(Activities -> Outputs -> Decision Point) |
| 特殊: 思维框架 | 安全审计、架构分析 | Trail of Bits audit-context-building (302行) | 分析框架 + 量化阈值 + 非目标约束 |

---

## 机制性分析：MacLaw Skill Runner 的结构性缺口

### 文章的核心洞察

> Skill 的设计质量直接决定 LLM 的执行准确率。Skill 不生成新工具，它注入的是"指令文本"。

这个洞察与 SkVM 论文的发现一致——Skill 携带隐式的能力需求和执行模式，但系统没有机制来表达和适配。文章进一步细化了 5 种执行模式，每种模式对 Runner 有不同的要求。

### MacLaw 当前状态

MacLaw 的 Skill Runner 是一条**单模式管线**：StartRun -> executeAsync -> executeStepWithContext。所有 skill 不论其架构模式，都走完全相同的执行路径。这条管线只支持模式 1（线性流程）的子集——顺序执行 steps，无决策分支、无循环、无跨 session 状态、无检查点。

已有改进（skvm-inspired-improvements.md）解决了三个缺口：
1. Requirements 契约层（统一依赖检查）
2. craft_tool 固化（脚本进化为 bash 步骤）
3. 结构感知注入（按 taxonomy 选择注入策略）

已有改进（memento-skills-inspired-improvements.md）解决了四个缺口：
1. Context-Aware Behavioral Utility（上下文感知评分）
2. Skill Self-Repair（失败驱动自修复）
3. craft_tool 持久化（CreateOnMiss -> Skill Library）
4. Skill Memory 聚合层

已有改进（skill-runner-mechanism-fix.md）解决了四个问题：
1. SKILL.md 文档注入 LLM context
2. SKILL.md Frontmatter 元数据完整性（YAML 解析）
3. Coding Tool Gate 误剥离 bash/write_file
4. 参数契约层（Schema 声明 + 合成 + 绑定引擎）

**本文档聚焦文章提出的 5 种模式中，MacLaw 尚未覆盖的机制性缺口。**


---

## 缺口 1：Frontmatter Description 质量——决定 Skill 是否被加载的"门面"

### 问题本质

文章强调 description 是 LLM 扫描所有可用 Skill 时的第一接触点，好 description 需要三要素：
1. **触发短语**：用户可能说的话（"deploy my app"、"push this live"）
2. **时序位置**：在什么之前/之后使用（"before writing implementation code"）
3. **产品关键词**：覆盖的平台或产品名

MacLaw 的 skill 匹配机制有两条路径：
- 	riggers 列表：关键词精确匹配（已有，skill-runner-mechanism-fix.md 问题 2 增强了 YAML 解析）
- description 字段：注入到 LLM context 后由 LLM 自主判断

**缺口**：description 字段没有质量校验。模糊的描述如"帮助处理文件"会导致 LLM 无法判断何时该调用。同时 	riggers 和 description 之间没有一致性检查——triggers 写了"pdf"但 description 没提 PDF，LLM 看到 description 后不知道这个 skill 能处理 PDF。

### 修复：Description 质量评分 + Trigger-Description 一致性检查

#### corelib/skill/description_quality.go（新文件）

`go
// DescriptionQuality 评估 skill description 的质量。
// 不是 lint 工具——不阻止执行，只在 skill 安装/更新时给出建议。
type DescriptionQuality struct {
    Score       float64  // 0.0-1.0
    Missing     []string // 缺失的要素: "trigger_phrases", "temporal_position", "product_keywords"
    Suggestions []string // 改进建议
}

// EvaluateDescription 评估 description 质量。
// 基于文章的三要素模型：触发短语、时序位置、产品关键词。
//
// 评分规则（数据驱动，不依赖 LLM）：
//   - 长度 >= 20 字符: +0.2
//   - 包含动词短语（"生成"/"转换"/"查询"/"deploy"/"convert"）: +0.2
//   - 包含时序词（"之前"/"之后"/"before"/"after"/"when"）: +0.2
//   - 包含具体产品/格式名（从 triggers 交叉验证）: +0.2
//   - triggers 中的关键词在 description 中出现 >= 50%: +0.2
func EvaluateDescription(description string, triggers []string) DescriptionQuality

// SuggestDescriptionImprovement 基于 skill 的 steps 和 triggers
// 生成 description 改进建议。不调用 LLM——纯模板拼接。
func SuggestDescriptionImprovement(skill *corelib.NLSkillEntry) string
`

#### 集成点

- gui/app_nl_skills.go：installSkillFromHub / installSkillFromGitHub 安装后调用 EvaluateDescription，score < 0.4 时在安装结果中附带改进建议
- 	ui/tool_manage_skill.go：skillInstall 同步
- 不阻止安装——只是建议

### 与现有机制的关系

- 复用 	riggers 字段做交叉验证（skill-runner-mechanism-fix.md 问题 2 已增强 triggers 解析）
- 不替代 ppendKnowledgeSkillSection 的匹配逻辑——匹配仍由 triggers + 名称匹配驱动
- 评分结果可选存入 NLSkillEntry.DescriptionScore，供 Router 的 skill_match 信号使用


---

## 缺口 2：决策树模式——Skill 内部的条件分支执行

### 问题本质

模式 2（决策树+按需加载）的核心是：**一个 Skill 内部根据用户意图走不同的执行分支**。文章以 cloudflare-deploy 为例——用户说"I need to run code"走 Workers 分支，说"I need a database"走 D1 分支。

MacLaw 已有 operations 机制（改进记录 #9.1）：skill.yaml 可以声明多个 operation，每个 operation 有 labels 指向不同的 steps 子集。LLM 调用 manage_skill(action=run, operation="generate") 时，Runner 只执行该 operation 的 labels 对应的 steps。

**缺口**：operations 是 LLM 在调用时显式选择的——LLM 必须知道有哪些 operation 可选。但文章的决策树模式是**在 SKILL.md 中用自然语言描述决策逻辑**，LLM 读完后自主决策。当前 ppendKnowledgeSkillSection 注入 SKILL.md 时不区分"决策树型"和"线性型"——两者使用相同的注入策略。

更关键的缺口是**渐进式披露**：主文件控制在 7KB，eferences/ 按需展开。MacLaw 的 SKILL.md 注入是全量的——要么注入完整文档，要么不注入。没有"先注入摘要，LLM 需要时再加载详情"的机制。

### 修复：references/ 按需加载 + Skill Taxonomy 增强

#### 1. references/ 目录支持（corelib/skill/scanner.go + corelib/types.go）

`go
// NLSkillEntry 新增：
References []SkillReference json:"references,omitempty"

// SkillReference 描述一个按需加载的参考文档。
// LLM 在 SKILL.md 中看到 "详见 references/workers.md"，
// 需要时调用 manage_skill(action=read_ref, name="skill-name", ref="workers.md")
type SkillReference struct {
    Filename    string json:"filename"
    Description string json:"description,omitempty" // 一行摘要，注入到 context
    TokenCount  int    json:"token_count,omitempty" // 预估 token 数
}
`

loadSkillFromDir 扫描 {skillDir}/references/ 目录，为每个 .md 文件创建 SkillReference（description 从文件第一行 # 标题 提取）。

#### 2. manage_skill 新增 ead_ref action

`go
// gui/im_tools_misc.go + tui/agent_tools.go
case "read_ref":
    skillName := args["name"]
    refFile := args["ref"]
    // 安全校验：refFile 不能包含 .. 或绝对路径
    content := loadSkillReference(skill.SkillDir, refFile)
    return content, nil
`

#### 3. 注入策略增强（corelib/skill/taxonomy.go）

FormatSkillForContext 的 TaxonomyKnowledge 分支增强：当 skill 有 eferences/ 时，注入 SKILL.md 主文件 + references 索引（文件名 + 一行描述），不注入 references 全文。

`
### Skill: cloudflare-deploy
[SKILL.md 主文件内容，~2K tokens]

可用参考文档（使用 manage_skill(action=read_ref) 按需加载）：
  - workers.md: Cloudflare Workers 部署指南 (~800 tokens)
  - d1.md: D1 数据库配置 (~600 tokens)
  - r2.md: R2 存储桶设置 (~500 tokens)
`

LLM 读到决策树后，根据用户意图调用 ead_ref 加载对应分支的详细文档。

#### 4. Frontmatter 新增 eferences 声明（SKILL.md YAML）

`yaml
---
name: cloudflare-deploy
description: 部署应用到 Cloudflare 平台（Workers/Pages/D1/R2）
references:
  - file: workers.md
    description: Workers 边缘函数部署
  - file: d1.md
    description: D1 数据库配置
---
`

当 eferences/ 目录存在但 frontmatter 未声明时，自动扫描生成。frontmatter 声明优先（可自定义 description）。

### 与现有机制的关系

- operations（#9.1）是执行层的分支——Runner 按 labels 选择 steps
- eferences/ 是知识层的分支——LLM 按需加载文档
- 两者互补：operations 用于 executable skill（有 steps），references 用于 knowledge skill（有文档）
- Token 预算机制不变——references 索引占用极少 token（~50），全文按需加载时走独立的 tool result 通道

### 预期收益

- 大型 knowledge skill（如平台部署指南）的初始注入从 5K+ token 降到 2K（主文件）+ 50 token（索引）
- LLM 按需加载只需要的分支，不浪费 context 在无关分支上
- 与文章的"渐进式披露"模式对齐


---

## 缺口 3：循环迭代模式——Skill 内部的 Loop + 退出条件

### 问题本质

模式 3（循环迭代）的核心是 **做 -> 验证 -> 改进** 的循环，直到满足退出条件。文章以 TDD skill 为例：RED -> Verify RED -> GREEN -> Verify GREEN -> REFACTOR -> Repeat，8 项 checklist 作为退出条件。

MacLaw 已有 poll 机制（#9.2）：步骤可以声明 poll.interval/poll.max_attempts/poll.until_match，Runner 循环执行直到输出匹配正则或达到上限。但 poll 是**输出匹配驱动的被动等待**——适合异步任务轮询（"status 变成 completed 了吗？"），不适合主动迭代（"测试通过了吗？没通过就改代码再跑"）。

**缺口**：没有"执行步骤 A -> 验证 -> 失败则执行步骤 B -> 回到步骤 A"的循环机制。when 条件（#9.3）只做单次跳过判断，不做循环。

### 修复：Step Loop 声明 + Verification Gate

#### 1. NLSkillStep 新增 loop 字段

`go
// corelib/types.go
type StepLoopConfig struct {
    // MaxIterations 是循环最大次数。必须声明，防止无限循环。
    MaxIterations int    json:"max_iterations" yaml:"max_iterations"
    // UntilStep 是验证步骤的 label。每次循环体执行后，
    // 执行验证步骤，输出匹配 UntilMatch 则退出循环。
    UntilStep     string json:"until_step,omitempty" yaml:"until_step,omitempty"
    // UntilMatch 是验证步骤输出的正则匹配。匹配则退出。
    UntilMatch    string json:"until_match,omitempty" yaml:"until_match,omitempty"
    // OnFailStep 是验证失败时执行的修复步骤 label（可选）。
    // 执行后回到循环体重新开始。
    OnFailStep    string json:"on_fail_step,omitempty" yaml:"on_fail_step,omitempty"
}

// NLSkillStep 新增：
Loop *StepLoopConfig json:"loop,omitempty" yaml:"loop,omitempty"
`

#### 2. Skill YAML 示例（TDD 模式）

`yaml
name: tdd-workflow
description: 测试驱动开发工作流
steps:
  - action: bash
    label: write_test
    params:
      command: "python -m pytest {{test_file}} -x --tb=short"
    loop:
      max_iterations: 5
      until_step: verify_green
      until_match: "passed"
      on_fail_step: fix_code

  - action: craft_tool
    label: fix_code
    params:
      task: "根据测试失败信息修复代码"

  - action: bash
    label: verify_green
    params:
      command: "python -m pytest {{test_file}} -x --tb=short"
`

执行流程：write_test -> 失败 -> ix_code -> write_test -> ... -> erify_green 匹配 "passed" -> 退出循环 -> 继续后续步骤。

#### 3. Runner 集成（gui/skill_runner.go + 	ui/agent_tools.go）

executeAsync 在执行带 loop 的步骤时：

`go
func (r *SkillRunner) executeStepWithLoop(ctx context.Context, step corelib.NLSkillStep,
    allSteps []corelib.NLSkillStep, vars map[string]string, ...) (string, error) {

    loop := step.Loop
    if loop == nil || loop.MaxIterations <= 0 {
        // 无循环，正常执行
        return r.executeStepWithContext(ctx, step, ...)
    }

    verifyStep := findStepByLabel(allSteps, loop.UntilStep)
    fixStep := findStepByLabel(allSteps, loop.OnFailStep) // 可选

    for i := 0; i < loop.MaxIterations; i++ {
        // 1. 执行循环体
        output, err := r.executeStepWithContext(ctx, step, ...)

        // 2. 执行验证步骤
        if verifyStep != nil {
            verifyOutput, verifyErr := r.executeStepWithContext(ctx, *verifyStep, ...)
            if verifyErr == nil && matchesPattern(verifyOutput, loop.UntilMatch) {
                return verifyOutput, nil // 退出循环
            }
        } else if err == nil && matchesPattern(output, loop.UntilMatch) {
            return output, nil // 无独立验证步骤，用循环体输出判断
        }

        // 3. 验证失败，执行修复步骤（如果有）
        if fixStep != nil {
            r.executeStepWithContext(ctx, *fixStep, ...)
        }
    }
    return "", fmt.Errorf("循环达到上限 %d 次仍未通过验证", loop.MaxIterations)
}
`

#### 与 poll 的区别

| 特性 | poll | loop |
|------|------|------|
| 目的 | 被动等待异步结果 | 主动迭代改进 |
| 循环体 | 重复执行同一个步骤 | 执行循环体 + 验证 + 修复 |
| 退出条件 | 输出匹配正则 | 验证步骤输出匹配正则 |
| 失败处理 | 无（继续等待） | 执行 on_fail_step 修复后重试 |
| 典型场景 | 图片生成轮询 | TDD、代码审查、设计评审 |

### 与现有机制的关系

- poll（#9.2）保持不变——异步等待场景仍用 poll
- when（#9.3）保持不变——单次条件跳过仍用 when
- loop 是新增的第三种流程控制原语
- on_fail_step 复用 indStepByLabel（已有机制）
- craft_tool 可以作为 on_fail_step——LLM 动态生成修复代码


---

## 缺口 4：接力棒循环模式——跨 Session 的 Skill 状态持久化

### 问题本质

模式 4（接力棒循环）的核心是用 
ext-prompt.md 文件作为"接力棒"，LLM 不需要记住"上次做到哪了"，只需读写文件即可续接状态。6 步协议：Read Baton -> Consult Context -> Generate -> Integrate -> Update Docs -> Prepare Next Baton。

MacLaw 已有跨 session 状态机制：
- capture 字段（#5）：步骤间变量传递（session_id 等）
- in-flight task marker（#55）：进程被杀后恢复
- UnfinishedTaskSlot：未完成任务恢复

**缺口**：这些机制都是**会话级**的——在同一个 agent loop 或同一次 manage_skill(action=run) 调用内有效。没有**跨多次独立调用**的状态持久化。用户今天让 skill 做第一阶段，明天继续第二阶段，skill 不知道昨天做了什么。

### 修复：Skill State 持久化 + Baton 协议

#### 1. Skill State 存储（corelib/skill/state.go，新文件）

`go
// SkillState 是 skill 的跨调用持久化状态。
// 存储在 {skillDir}/.state/state.json。
// 每次 manage_skill(action=run) 执行前自动加载，执行后自动保存。
type SkillState struct {
    // CurrentPhase 当前阶段标识（skill 自定义）
    CurrentPhase string            json:"current_phase,omitempty"
    // Vars 持久化变量（跨调用保留）
    Vars         map[string]string json:"vars,omitempty"
    // History 执行历史摘要（最近 10 次）
    History      []StateHistoryEntry json:"history,omitempty"
    // NextPrompt 接力棒内容——下次执行时注入到 LLM context
    NextPrompt   string            json:"next_prompt,omitempty"
    // UpdatedAt 最后更新时间
    UpdatedAt    string            json:"updated_at"
}

type StateHistoryEntry struct {
    Timestamp string json:"timestamp"
    Phase     string json:"phase,omitempty"
    Summary   string json:"summary" // 一行摘要
    Success   bool   json:"success"
}

// LoadState 从 {skillDir}/.state/state.json 加载。
// 文件不存在返回空 state（不报错）。
func LoadState(skillDir string) (*SkillState, error)

// SaveState 保存到 {skillDir}/.state/state.json。
func SaveState(skillDir string, state *SkillState) error

// AppendHistory 追加一条历史记录，保留最近 10 条。
func (s *SkillState) AppendHistory(entry StateHistoryEntry)
`

#### 2. Skill YAML 新增 stateful: true 声明

`yaml
name: research-project
description: 多阶段研究项目管理
stateful: true
steps:
  - action: bash
    label: check_state
    params:
      command: "cat {baseDir}/.state/state.json 2>/dev/null || echo '{}'"
  # ... 后续步骤根据 state 决定执行什么
`

#### 3. Runner 集成

executeAsync 在执行 stateful: true 的 skill 时：

`go
// 执行前：加载 state，注入到 vars
if skill.Stateful {
    state, _ := cskill.LoadState(skill.SkillDir)
    if state != nil {
        for k, v := range state.Vars {
            vars["state_"+k] = v // 前缀 state_ 避免与用户 args 冲突
        }
        if state.NextPrompt != "" {
            vars["next_prompt"] = state.NextPrompt
        }
    }
}

// 执行后：从 capture 结果更新 state
if skill.Stateful {
    state := &cskill.SkillState{
        Vars:      extractStateVars(capturedVars), // state_ 前缀的变量
        UpdatedAt: time.Now().Format(time.RFC3339),
    }
    state.AppendHistory(cskill.StateHistoryEntry{
        Timestamp: state.UpdatedAt,
        Summary:   buildRunSummary(run),
        Success:   run.status.Status == "completed",
    })
    cskill.SaveState(skill.SkillDir, state)
}
`

#### 4. NextPrompt 注入到 LLM context

ppendKnowledgeSkillSection 注入 stateful skill 时，如果 state.NextPrompt 非空，追加到注入内容末尾：

`
### Skill: research-project
[SKILL.md 内容]

[上次执行状态]
阶段: literature_review
接力棒: 已完成 23 篇论文的初筛，其中 8 篇高度相关。
下一步需要对这 8 篇做深度阅读并提取关键发现。
相关文件: research/papers_shortlist.md
`

#### 5. manage_skill 新增 state action

`go
case "state":
    subAction := args["sub_action"] // "get" / "set" / "clear"
    switch subAction {
    case "get":
        state, _ := cskill.LoadState(skill.SkillDir)
        return formatState(state), nil
    case "set":
        // LLM 可以主动更新 next_prompt
        state, _ := cskill.LoadState(skill.SkillDir)
        if np, ok := args["next_prompt"]; ok {
            state.NextPrompt = np
        }
        cskill.SaveState(skill.SkillDir, state)
        return "状态已更新", nil
    case "clear":
        os.RemoveAll(filepath.Join(skill.SkillDir, ".state"))
        return "状态已清除", nil
    }
`

### 与现有机制的关系

- capture（#5）是步骤间变量传递——单次调用内有效
- SkillState 是调用间状态持久化——跨天/跨周有效
- capture 的结果可以流入 SkillState.Vars（通过 state_ 前缀约定）
- in-flight task marker（#55）是进程级恢复——进程被杀后恢复
- SkillState 是业务级恢复——用户主动续接

### 预期收益

- 多阶段研究项目：用户今天做文献综述，明天做数据分析，skill 自动续接
- 长期代码重构：每次执行处理一个模块，state 记录已完成的模块列表
- 与文章的"接力棒循环"模式完全对齐


---

## 缺口 5：多阶段+检查点模式——Skill 编排器

### 问题本质

模式 5（多阶段+检查点）的核心是：大 Skill 调度 10+ 个子 Skill，每个阶段有 Activities -> Outputs -> Decision Point（Go/No-Go）。文章以 discovery-process 为例——502 行的编排器 skill 调度多个子 skill 完成发现流程。

MacLaw 已有 TaskExecutionOrchestrator（#22）编排编码任务，但它是**硬编码在 agent loop 中的编码专用编排器**，不是通用的 skill 编排机制。

**缺口**：没有"skill A 的输出作为 skill B 的输入"的编排机制。当前 skill 之间是完全独立的——LLM 必须手动调用 skill A，拿到结果，再调用 skill B 并传入结果。

### 修复：Skill Pipeline 声明 + 检查点门控

#### 1. Skill YAML 新增 pipeline 字段

`yaml
name: full-stack-deploy
description: 全栈应用部署流水线
mode: pipeline
pipeline:
  - skill: lint-check
    checkpoint: true
    checkpoint_message: "Lint 检查完成。是否继续部署？"

  - skill: unit-test
    params:
      coverage_threshold: "80"
    checkpoint: true
    checkpoint_message: "测试覆盖率 {{coverage}}%。是否继续？"

  - skill: build-frontend
    params:
      input: "{{lint-check.output_dir}}"

  - skill: deploy-to-staging
    checkpoint: true
    checkpoint_message: "已部署到 staging。请验证后确认是否部署到 production。"

  - skill: deploy-to-production
`

#### 2. 数据结构

`go
// corelib/types.go
type SkillPipelineStep struct {
    Skill              string            json:"skill" yaml:"skill"
    Params             map[string]string json:"params,omitempty" yaml:"params,omitempty"
    Checkpoint         bool              json:"checkpoint,omitempty" yaml:"checkpoint,omitempty"
    CheckpointMessage  string            json:"checkpoint_message,omitempty" yaml:"checkpoint_message,omitempty"
    ContinueOnFail     bool              json:"continue_on_fail,omitempty" yaml:"continue_on_fail,omitempty"
    TimeImpactOnReject string            json:"time_impact_on_reject,omitempty" yaml:"time_impact_on_reject,omitempty"
}

// NLSkillEntry 新增：
Pipeline []SkillPipelineStep json:"pipeline,omitempty" yaml:"pipeline,omitempty"
`

#### 3. Pipeline Runner（corelib/skill/pipeline.go，新文件）

`go
// PipelineRunner 执行 skill pipeline。
// 与 TaskExecutionOrchestrator 的区别：
//   - Orchestrator 编排编码任务（task list），在 agent loop 内部运行
//   - PipelineRunner 编排 skill 调用，在 manage_skill(action=run) 内部运行
//
// 检查点通过 ask_user 工具实现（已有，#16）。
type PipelineRunner struct {
    skillExecutor SkillExecutorInterface // 调用子 skill 的接口
    askUser       func(question string, options []string) (string, error)
}

// Run 执行 pipeline 的所有步骤。
// 每个步骤的输出通过 vars 传递给后续步骤（key = "{skill_name}.{var_name}"）。
// 检查点步骤暂停执行，通过 askUser 等待用户确认。
func (pr *PipelineRunner) Run(ctx context.Context, pipeline []SkillPipelineStep,
    initialVars map[string]string) (*PipelineResult, error) {

    vars := maps.Clone(initialVars)

    for i, step := range pipeline {
        // 1. 参数模板替换（引用前序 skill 的输出）
        resolvedParams := resolveTemplateVars(step.Params, vars)

        // 2. 执行子 skill
        result, err := pr.skillExecutor.RunSkill(ctx, step.Skill, resolvedParams)
        if err != nil && !step.ContinueOnFail {
            return &PipelineResult{
                Status:      "failed",
                FailedAt:    i,
                FailedSkill: step.Skill,
                Error:       err.Error(),
            }, nil
        }

        // 3. 子 skill 的 capture 输出注入 vars
        for k, v := range result.CapturedVars {
            vars[step.Skill+"."+k] = v
        }

        // 4. 检查点门控
        if step.Checkpoint {
            msg := resolveTemplateVars(map[string]string{"msg": step.CheckpointMessage}, vars)["msg"]
            options := []string{"继续", "停止"}
            if step.TimeImpactOnReject != "" {
                msg += "\n(停止将延迟 " + step.TimeImpactOnReject + ")"
            }
            answer, err := pr.askUser(msg, options)
            if err != nil || answer == "停止" {
                return &PipelineResult{
                    Status:   "stopped_at_checkpoint",
                    StoppedAt: i,
                }, nil
            }
        }
    }

    return &PipelineResult{Status: "completed"}, nil
}
`

#### 4. Runner 集成

executeAsync 检测 skill.Mode == "pipeline" 时，委托给 PipelineRunner：

`go
if skill.Mode == "pipeline" && len(skill.Pipeline) > 0 {
    pr := &PipelineRunner{
        skillExecutor: r,
        askUser:       r.askUserFunc, // 复用 ask_user 工具
    }
    result, err := pr.Run(ctx, skill.Pipeline, vars)
    // ... 处理结果
    return
}
`

### 与现有机制的关系

- TaskExecutionOrchestrator（#22）编排编码任务——在 agent loop 层面
- PipelineRunner 编排 skill 调用——在 skill runner 层面
- 两者不冲突：编码任务用 orchestrator，skill 流水线用 pipeline
- 检查点复用 sk_user 工具（#16）——不新增交互机制
- 子 skill 间的数据传递复用 capture 机制（#5）

### 预期收益

- 复杂部署流程：lint -> test -> build -> deploy，每步有检查点
- 数据处理流水线：采集 -> 清洗 -> 分析 -> 报告
- 与文章的"多阶段+检查点"模式对齐
- 时间影响标注让用户了解拒绝检查点的延迟成本


---

## 缺口 6：防偷懒机制——强硬语气 + 借口反驳表 + 负面指令

### 问题本质

文章总结了 4 种防止 LLM 偷懒的武器：
1. **强硬语气**：命令式表达提高遵从率（"Delete it. Start over."）
2. **借口反驳表**：预判 LLM 的 12 种偷懒借口并逐一反驳
3. **量化阈值**：硬性最低标准（"每个函数最少 3 个不变量"）
4. **负面指令**：明确说"不要做 X"

MacLaw 的 SKILL.md 注入是原样注入——skill 作者写什么就注入什么。如果 skill 作者没有写强硬语气和负面指令，LLM 就会偷懒。

**缺口**：系统层面没有为 executable skill 的执行注入防偷懒指令。这不是 skill 作者的责任——是 Runner 的责任。

### 修复：Skill Execution Preamble（执行前言注入）

#### corelib/skill/execution_preamble.go（新文件）

`go
// BuildExecutionPreamble 为 skill 执行生成前言指令。
// 注入到 craft_tool 的 task 描述前，或 knowledge skill 的
// SKILL.md 注入后。
//
// 前言内容基于 skill 的结构特征自动生成，不需要 skill 作者手动编写。
func BuildExecutionPreamble(skill *corelib.NLSkillEntry) string {
    var parts []string

    // 1. 安全默认值（所有 skill 通用）
    parts = append(parts, "执行规则：")
    parts = append(parts, "- 严格按照 skill 定义的步骤顺序执行，不要跳过任何步骤")
    parts = append(parts, "- 不要自行修改 skill 脚本的参数或逻辑")
    parts = append(parts, "- 遇到错误时报告具体错误信息，不要猜测原因")

    // 2. 负面指令（基于 skill 类型）
    if hasDeploySteps(skill) {
        parts = append(parts, "- 不要部署到 production 环境，除非用户明确要求")
        parts = append(parts, "- 不要删除或覆盖已有的部署配置")
    }
    if hasDatabaseSteps(skill) {
        parts = append(parts, "- 不要执行 DROP/DELETE/TRUNCATE 操作，除非用户明确要求")
    }

    // 3. 量化阈值（craft_tool 专用）
    if hasCraftToolSteps(skill) {
        parts = append(parts, "- 生成的脚本必须包含错误处理（try/catch 或 set -e）")
        parts = append(parts, "- 生成的脚本必须在开头验证所有输入参数非空")
        parts = append(parts, "- 不要生成超过 200 行的单个脚本——拆分为多个函数")
    }

    // 4. 验证要求（有 poll/loop 的 skill）
    if skill.HasVerificationSteps() {
        parts = append(parts, "- 验证步骤的输出必须完整引用，不要截断或总结")
        parts = append(parts, "- 验证失败时必须执行修复步骤，不要跳过")
    }

    if len(parts) <= 1 {
        return "" // 只有标题没有内容，不注入
    }
    return strings.Join(parts, "\n")
}
`

#### 集成点

- gui/skill_runner.go：executeCraftToolCore 的 task 描述前追加 preamble
- gui/im_system_prompt.go：ppendKnowledgeSkillSection 注入 SKILL.md 后追加 preamble
- 	ui/agent_tools.go：unCraftToolStepTUI 同步

### 与现有机制的关系

- 不替代 SKILL.md 中 skill 作者写的指令——preamble 是系统级补充
- 与 steering 规则互补——steering 是全局规则，preamble 是 per-skill 规则
- 基于 skill 结构自动生成——不需要 skill 作者额外配置

---

## 缺口 7：知识组织的 3 层架构——Token 预算优化

### 问题本质

文章提出知识组织的 3 层架构：
- 第 1 层：Frontmatter（~100 tokens）——LLM 扫描决定是否加载
- 第 2 层：SKILL.md 正文（2K-5K tokens）——核心指令
- 第 3 层：references/ 和 resources/（1K-3K tokens/个，按需加载）

总上下文占用建议控制在 10K tokens 以内。

MacLaw 的 ppendKnowledgeSkillSection 已有 token 预算机制（截断到预算），但预算是**全局的**——所有匹配的 skill 共享一个 token 预算。没有 per-skill 的 token 预算声明。

**缺口**：skill 作者无法声明"这个 skill 的 SKILL.md 很大（10K tokens），但核心指令只有 2K，其余是参考文档"。当前的截断是按字符数粗暴截断，可能截断在关键指令中间。

### 修复：SKILL.md 结构化分区 + 智能截断

#### 1. SKILL.md 分区约定

`markdown
---
name: complex-skill
description: ...
---

<!-- CORE: 以下是核心指令，必须完整注入 -->
# 核心流程

1. 先做 A
2. 再做 B
3. 最后做 C

<!-- REFERENCE: 以下是参考文档，token 不足时可截断 -->
# 详细说明

## A 的详细步骤
...

## B 的详细步骤
...
`

#### 2. 解析器增强（corelib/skill/skill_markdown.go）

`go
// SplitSkillDocSections 将 SKILL.md 内容按 <!-- CORE --> 和
// <!-- REFERENCE --> 注释分区。
// 无注释时整个文档视为 CORE（向后兼容）。
func SplitSkillDocSections(content string) (core string, reference string) {
    coreMarker := "<!-- CORE:"
    refMarker := "<!-- REFERENCE:"

    coreIdx := strings.Index(content, coreMarker)
    refIdx := strings.Index(content, refMarker)

    if coreIdx < 0 && refIdx < 0 {
        return content, "" // 无分区标记，整个文档是 core
    }

    if refIdx < 0 {
        // 只有 CORE 标记
        afterCore := content[coreIdx:]
        lineEnd := strings.Index(afterCore, "\n")
        if lineEnd >= 0 {
            return strings.TrimSpace(afterCore[lineEnd+1:]), ""
        }
        return content, ""
    }

    // 有 REFERENCE 标记
    corePart := content
    if coreIdx >= 0 {
        lineEnd := strings.Index(content[coreIdx:], "\n")
        if lineEnd >= 0 {
            corePart = content[coreIdx+lineEnd+1 : refIdx]
        }
    } else {
        corePart = content[:refIdx]
    }

    refLineEnd := strings.Index(content[refIdx:], "\n")
    refPart := ""
    if refLineEnd >= 0 {
        refPart = content[refIdx+refLineEnd+1:]
    }

    return strings.TrimSpace(corePart), strings.TrimSpace(refPart)
}
`

#### 3. 注入策略增强

ppendKnowledgeSkillSection 在注入 SKILL.md 时：
1. 调用 SplitSkillDocSections 分区
2. CORE 部分始终完整注入（不截断）
3. REFERENCE 部分在 token 预算允许时注入，不足时截断或跳过
4. 截断时在 REFERENCE 部分末尾追加 [参考文档已截断，使用 manage_skill(action=read_ref) 查看完整内容]

### 与现有机制的关系

- 与缺口 2 的 eferences/ 目录互补——eferences/ 是独立文件，CORE/REFERENCE 是同一文件内的分区
- 向后兼容——无分区标记的 SKILL.md 整个文档视为 CORE
- Token 预算机制不变——只是截断策略从"粗暴截断"变为"保护 CORE，截断 REFERENCE"

---

## 修改文件清单

### 缺口 1：Description 质量
| 文件 | 操作 | 说明 |
|------|------|------|
| corelib/skill/description_quality.go | 新增 | EvaluateDescription + SuggestDescriptionImprovement |
| corelib/skill/description_quality_test.go | 新增 | 测试 |
| gui/app_nl_skills.go | 修改 | 安装后调用评估 |

### 缺口 2：references/ 按需加载
| 文件 | 操作 | 说明 |
|------|------|------|
| corelib/types.go | 修改 | NLSkillEntry 新增 References |
| corelib/skill/scanner.go | 修改 | loadSkillFromDir 扫描 references/ |
| gui/im_tools_misc.go | 修改 | manage_skill 新增 ead_ref action |
| gui/im_tool_definitions.go | 修改 | manage_skill 工具定义新增 ead_ref |
| corelib/skill/taxonomy.go | 修改 | FormatSkillForContext 注入 references 索引 |
| 	ui/agent_tools.go | 修改 | 	oolManageSkill 新增 ead_ref |

### 缺口 3：循环迭代
| 文件 | 操作 | 说明 |
|------|------|------|
| corelib/types.go | 修改 | NLSkillStep 新增 Loop *StepLoopConfig |
| corelib/skill/scanner.go | 修改 | 解析 loop 字段 |
| gui/skill_runner.go | 修改 | executeStepWithLoop 循环执行逻辑 |
| 	ui/agent_tools.go | 修改 | TUI 同步 |

### 缺口 4：跨 Session 状态
| 文件 | 操作 | 说明 |
|------|------|------|
| corelib/skill/state.go | 新增 | SkillState + LoadState + SaveState |
| corelib/types.go | 修改 | NLSkillEntry 新增 Stateful bool |
| corelib/skill/scanner.go | 修改 | 解析 stateful 字段 |
| gui/skill_runner.go | 修改 | 执行前加载 state，执行后保存 |
| gui/im_tools_misc.go | 修改 | manage_skill 新增 state action |
| gui/im_system_prompt.go | 修改 | stateful skill 注入 NextPrompt |

### 缺口 5：Skill Pipeline
| 文件 | 操作 | 说明 |
|------|------|------|
| corelib/types.go | 修改 | 新增 SkillPipelineStep；NLSkillEntry 新增 Pipeline |
| corelib/skill/scanner.go | 修改 | 解析 pipeline 字段 |
| corelib/skill/pipeline.go | 新增 | PipelineRunner |
| gui/skill_runner.go | 修改 | mode=pipeline 委托给 PipelineRunner |

### 缺口 6：执行前言
| 文件 | 操作 | 说明 |
|------|------|------|
| corelib/skill/execution_preamble.go | 新增 | BuildExecutionPreamble |
| gui/skill_runner.go | 修改 | craft_tool 注入 preamble |
| gui/im_system_prompt.go | 修改 | knowledge skill 注入 preamble |

### 缺口 7：结构化分区
| 文件 | 操作 | 说明 |
|------|------|------|
| corelib/skill/skill_markdown.go | 修改 | SplitSkillDocSections |
| gui/im_system_prompt.go | 修改 | 注入时保护 CORE 截断 REFERENCE |

---

## 实施优先级

`
缺口 7 (结构化分区)     ← 最简单，纯解析增强，零风险
    |
缺口 1 (Description 质量) ← 纯增量，不影响执行
    |
缺口 6 (执行前言)        ← 纯增量，不影响执行
    |
缺口 2 (references/)     ← 新增 action，中等复杂度
    |
缺口 3 (循环迭代)        ← 新增流程控制原语，需要仔细测试
    |
缺口 4 (跨 Session 状态) ← 新增持久化层，需要仔细测试
    |
缺口 5 (Skill Pipeline)  ← 最复杂，依赖 ask_user + 子 skill 调用
`

| 缺口 | 优先级 | 预估工作量 | 依赖 | 风险 |
|------|--------|-----------|------|------|
| 7 | P0 | 0.5 天 | 无 | 极低 |
| 1 | P1 | 1 天 | 无 | 低 |
| 6 | P1 | 1 天 | 无 | 低 |
| 2 | P1 | 1-2 天 | 无 | 低 |
| 3 | P2 | 2 天 | 无 | 中 |
| 4 | P2 | 2 天 | 无 | 中 |
| 5 | P3 | 3 天 | 缺口 4 (state) | 高 |

**总预估**：10-12 天。缺口 7/1/6 可并行，缺口 2/3/4 可并行，缺口 5 最后。

---

## 与已有改进文档的关系

| 本文档缺口 | 关联的已有改进 | 关系 |
|-----------|--------------|------|
| 缺口 1 (Description) | skill-runner-mechanism-fix 问题 2 (Frontmatter YAML) | 互补：问题 2 解决解析，缺口 1 解决质量 |
| 缺口 2 (references/) | skvm-improvements 改进 3 (结构感知注入) | 扩展：taxonomy 分类 + references 按需加载 |
| 缺口 3 (循环迭代) | 改进记录 #9.2 (poll) | 互补：poll 是被动等待，loop 是主动迭代 |
| 缺口 4 (跨 Session) | 改进记录 #5 (capture) + #55 (in-flight) | 扩展：capture 是步骤间，state 是调用间 |
| 缺口 5 (Pipeline) | 改进记录 #22 (TaskExecutionOrchestrator) | 互补：orchestrator 编排编码任务，pipeline 编排 skill |
| 缺口 6 (执行前言) | 改进记录 #49 (Steering) | 互补：steering 是全局规则，preamble 是 per-skill |
| 缺口 7 (分区) | skvm-improvements 改进 3 (注入策略) | 增强：从 taxonomy 级别细化到文档内部分区 |

---

## 不做的事

1. **不实现完整的 Skill 编排 DSL**——pipeline 用 YAML 声明式足够，不需要图灵完备的编排语言
2. **不实现 Skill 版本控制**——state 的 history 字段足够追溯，不需要 git-like 版本管理
3. **不实现 Skill 市场的质量评分排名**——description 质量评分只用于安装时建议，不影响搜索排序
4. **不实现跨 Skill 的共享 state**——每个 skill 的 state 独立，pipeline 通过 vars 传递数据
5. **不实现 LLM 驱动的 description 自动生成**——纯模板拼接足够，LLM 调用成本太高

