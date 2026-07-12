# 借鉴 Memento-Skills 的 MacLaw 改进计划

> **OBSOLETE（2026-07）** — 「Self-Repair 未接入 Runner / craft 不持久化 / 只分类不回流」等状态描述已过时。  
> 现状与剩余缺口见 **[skill-lifecycle-gap-review-2026.md](./skill-lifecycle-gap-review-2026.md)**。本文保留论文映射与历史 Phase 规划，**勿当实现清单**。

## 论文核心洞察

[Memento-Skills](https://arxiv.org/abs/2603.18743)（Zhou et al., 2026）提出 **deployment-time learning** 范式：LLM 参数冻结，所有学习发生在外部化的 skill 记忆中。核心循环 **Read → Execute → Reflect → Write** 使 agent 从 5 个原子 skill 自动扩展到 235 个，在 HLE 上实现 116.2% 的相对提升。

关键技术贡献：
1. **Skill 作为一等公民**：结构化 Markdown（声明 + prompt + 代码），不是工具的附属品
2. **行为效用路由**：contrastive RL 训练的 skill router，优化行为成功率而非语义相似度
3. **反射式自修复**：失败归因到具体 skill → LLM 重写 skill → unit test gate 防回归
4. **SRDP 形式化**：π(a|s, M_t)，策略同时取决于状态和累积 skill 记忆

## MacLaw 现有基础设施映射

| Memento-Skills 概念 | MacLaw 对应模块 | 当前状态 |
|---------------------|----------------|---------|
| Skill Library | `corelib/skill/` + Hub Skills | 静态，不自进化 |
| Skill Router (contrastive RL) | `corelib/tool/router.go` Route() 5 信号评分 | 有 OutcomeScore 但不 context-aware |
| Read-Write Reflective Loop | `UsageTracker.RecordOutcome()` | 只记录不修复 |
| Failure Attribution | `classifySkillStepError()` | 只分类给用户看，不回流 |
| Skill Self-Repair | `corelib/skill/self_repair.go` | **已有骨架**，但未接入 Runner |
| Unit Test Gate | 无 | — |
| CreateOnMiss | `craft_tool` 步骤类型 | 已有，但不持久化到 skill library |
| SRDP Memory | memory + steering + sessionTools + UsageTracker | 分散、被动 |

## 改进计划：四个 Phase

---

### Phase 1: Context-Aware Behavioral Utility（上下文感知的行为效用评分）

**问题**：`OutcomeScore(toolName)` 只看工具的全局成功率，不区分使用场景。ssh 在"查看服务器资源"场景下成功率 95%，在"部署应用"场景下成功率 60%，但 `OutcomeScore("ssh")` 返回的是混合后的 78%。Router 无法根据当前 query 的场景选择最优工具。

**Memento-Skills 的做法**：contrastive RL 训练 embedding 模型，正样本是"调用后成功的 (query, skill) 对"，负样本是"语义相近但失败的对"。需要 GPU + 训练数据，对桌面应用不现实。

**MacLaw 的轻量替代**：利用已有的 `UsageRecord.QueryTokens`（每次调用已记录 top-5 BM25 token），构建 `(context_hash, tool_name) → outcome_stats` 的统计表，实现零训练成本的 context-aware 行为效用。

#### 实际实现（与原计划的差异）

**`corelib/tool/usage_tracker.go`**：

1. 新增 `ContextOutcomeScore(toolName string, queryTokens []string) float64`
   - 计算 queryTokens 与每条 UsageRecord 的 Jaccard 相似度
   - 只统计相似度 > 0.2 的记录（`contextOutcomeMinJaccard`，单个 token 重叠即有信号）
   - 委托给 `outcomeScoreWithCount` 统一计算（消除代码重复）
   - context 匹配记录不足 3 条时回退到全局 `OutcomeScore`（贝叶斯先验）
   - 未实现缓存层（2000 条记录扫描 <1ms，YAGNI）

2. 新增 `contextToolStats` 内部方法 + `ContextFailureStats` / `ContextSuccessAlternatives` 公共 API
   - 供 `SkillMemory` 通过公共接口访问，不穿透内部字段

3. `Record()` 委托给 `RecordOutcome()`，消除 token 截断/ring buffer/snapshot 的代码重复

**`corelib/tool/router.go`**：

4. `Route()` 中 `outcomeScore` 从 `OutcomeScore(name)` 改为 `ContextOutcomeScore(name, queryTokens)`

#### 验收标准

- 9 个新增测试 + 所有现有 usage_tracker / router 测试通过 
---

### Phase 2: Skill Self-Repair 闭环（失败驱动的 Skill 自修复）

**问题**：`corelib/skill/self_repair.go` 已有 `ShouldAttemptRepair()` + `AttemptRepair()` + `ApplyRepair()` 的完整骨架，但 Skill Runner（`gui/skill_runner.go` 和 `tui/agent_tools.go`）在 skill 失败后只调用 `classifySkillStepError()` 给用户友好提示，不触发自修复。失败数据没有回流到 skill 改进。

**Memento-Skills 的做法**：
1. Failure Attribution：LLM judge 分析 trace，定位失败的 skill
2. Skill Rewrite：LLM 重写 skill 的代码/prompt
3. Unit Test Gate：自动生成测试用例验证修改不引入回归

**MacLaw 的实现**：接入已有的 `self_repair.go` 骨架，在 Skill Runner 失败路径中触发自修复，简化 unit test gate 为"用原始输入重试"。

#### 实际实现（与原计划的差异）

**`corelib/skill/self_repair.go`**：

1. 新增 `RepairContext` 结构体（如计划）

2. 新增 `AttemptRepairWithContext()`，`AttemptRepair()` 委托给它
   - 用 `nonRepairableErrorClasses` 黑名单（而非白名单）——默认可修复，只排除 `rate_limit`/`network_error`
   - System prompt 增加"不要改变核心功能"约束

3. 未实现 `VerifyRepair()` 函数——简化为：修复后下次用户运行时自然验证，成功则 `ResetRepairCount`。避免在后台 goroutine 中执行 skill（可能有副作用）

**`corelib/types.go`**：

4. `NLSkillEntry` 新增 `RepairAttemptCount`、`LastRepairAt`、`RepairHistory []SkillRepairRecord`

**`gui/skill_runner.go`**：

5. 自修复触发点在 `updateUsageStats()`（而非 `executeAsync()`）——统计更新后才有数据判断
   - 异步 goroutine 执行，deep copy `Steps` 和 `RepairHistory` 防数据竞态
   - `skillLLMRepairer` 适配器：30s 超时、HTTP 状态码检查
   - 成功执行后 `ResetRepairCount` 重置修复计数器

#### 验收标准

- 7 个新增测试 + 所有现有 skill 测试通过 
---

### Phase 3: craft_tool 产出持久化（CreateOnMiss → Skill Library）

**问题**：`craft_tool` 步骤类型让 LLM 动态生成脚本并执行，但生成的脚本是一次性的——执行完就丢弃。下次遇到相同类型的任务，LLM 需要重新生成。这与 Memento-Skills 的 "CreateOnMiss" 机制形成对比：当 skill library 中没有匹配的 skill 时，系统自动创建新 skill 并持久化到 library。

**Memento-Skills 的做法**：
1. Router 检索不到匹配 skill → 触发 CreateOnMiss
2. LLM 生成新 skill（声明 + prompt + 代码）
3. 执行成功 → 写入 skill library
4. 下次遇到类似任务 → Router 直接检索到

**MacLaw 的实现**：在 `craft_tool` 执行成功后，将生成的脚本包装为标准 skill 并持久化。

#### 实际实现（与原计划的差异）

**`corelib/skill/craft_to_skill.go`**（新文件）：

1. `CraftToSkillResult` 结构体：`SkillName`、`SkillDir`、`IsUpdate`（精简，不含冗余的 Description/Steps）

2. `PersistCraftedSkill()` 函数——去重逻辑内聚在此函数中（`findSimilarCraftedSkill` 内部函数），不需要独立导出的 `DeduplicateCraftedSkill`
   - BM25 去重阈值 3.0（保守，需要 3+ 个 skill 才有足够 IDF 区分度）
   - 正则编译为包级变量（`whitespaceRe`/`nonAlphanumRe`/`unsafeDirCharRe`）
   - `craftedSkillName`/`craftedScriptExtension`/`buildCraftedScriptCommand` 命名与 gui 的同功能函数区分

**GUI 接入**：craft_tool 调用点（`buildCraftSuccessResult`）接入待做。已有 `registerCraftedSkillEntry` 做内存注册，`PersistCraftedSkill` 补充文件系统持久化层。

#### 验收标准

- 6 个新增测试 + 所有现有 skill 测试通过 
---

### Phase 4: 统一 Skill Memory（分散记忆 → 主动进化的能力库）

**问题**：MacLaw 的学习数据分散在四个独立存储中：
- `UsageTracker`（`~/.maclaw/data/tool_usage.json`）：工具调用统计
- `memory.Store`（`~/.maclaw/memory/`）：对话记忆
- `steering.Store`（`~/.maclaw/steering/`）：行为规则
- `sessionTools`（内存态）：会话级工具固定

这些存储互不通信。`UsageTracker` 知道 ssh 在"查看服务器"场景下成功率高，但 `steering.Store` 不知道——它只在用户消息包含"服务器"关键词时注入 SSH 规则。`memory.Store` 记住了用户的 SSH 服务器信息，但 `UsageTracker` 不知道——它只看工具名和 token。

**Memento-Skills 的做法**：统一的 skill library 是唯一的记忆载体。所有学习（成功模式、失败模式、用户偏好）都编码在 skill 的声明/prompt/代码中。

**MacLaw 的实现**：不替换现有存储（它们各有职责），而是新增一个 **SkillMemory 聚合层**，在 system prompt 注入时统一查询多个存储，生成 context-aware 的能力摘要。

#### 实际实现（与原计划的差异）

**`corelib/tool/skill_memory.go`**（新文件）：

1. `SkillMemory` 结构体只持有 `*UsageTracker`（memory.Store 和 steering.Store 的聚合留给未来）
   - 通过 `UsageTracker` 的公共 API（`ExtractPatterns`/`ContextFailureStats`/`ContextSuccessAlternatives`）访问数据，不穿透内部字段

2. `BuildCapabilitySummary` 只注入与当前 query 有 Jaccard 重叠的 pattern（不注入无关的高成功率工具）

3. `SuggestAlternatives` 供 drift recovery 使用

**`gui/im_system_prompt.go`**：

4. `appendSkillMemorySection()` 注入在记忆 section 之后、knowledge skill section 之前

**`gui/im_message_handler.go`**：

5. `buildDriftRecoverPrompt()` 增强为接受 `alternatives ...string` 可变参数
   - 调用点通过 `SkillMemory.SuggestAlternatives` 提供具体建议（局部变量，非 IIFE）

#### 验收标准

- 7 个新增测试 + 所有现有测试通过 
---

## 实施优先级和依赖关系

```
Phase 1 (Context-Aware Outcome)
    │
    ├──→ Phase 2 (Skill Self-Repair)  [独立于 Phase 1，可并行]
    │
    ├──→ Phase 3 (craft_tool 持久化)  [独立于 Phase 1/2，可并行]
    │
    └──→ Phase 4 (Skill Memory 聚合)  [依赖 Phase 1 的 ContextOutcomeScore]
```

| Phase | 优先级 | 预估工作量 | 依赖 | 风险 | 状态 |
|-------|--------|-----------|------|------|------|
| 1 | P0 | 1-2 天 | 无 | 低（纯增量，向后兼容） | 完成 |
| 2 | P1 | 2-3 天 | 无 | 中（LLM 修复质量不确定，需要 VerifyRepair 兜底） | 完成（corelib + GUI Runner 接入） |
| 3 | P1 | 1-2 天 | 无 | 低（craft_tool 已有，只加持久化） | corelib 层完成（PersistCraftedSkill + 去重），craft_tool 调用点接入待做 |
| 4 | P2 | 2-3 天 | Phase 1 | 低（只读聚合，不改现有存储） | 完成（corelib + system prompt 注入 + drift recovery 增强） |

**总预估**：6-10 天。Phase 1-3 可并行，Phase 4 等 Phase 1 完成后开始。

## 不做的事

1. **不训练 contrastive embedding 模型**——桌面应用没有 GPU，用统计表替代
2. **不替换现有存储**——UsageTracker、memory.Store、steering.Store 各有职责，SkillMemory 是聚合层不是替代层
3. **不自动生成 unit test**——成本太高，用"原始输入重试"替代
4. **不做 skill 版本控制 UI**——RepairHistory 字段足够追溯，不需要 UI
5. **不改 skill YAML 格式**——现有格式已够用，capture/when/poll 等元数据保持现状

## 与现有改进记录的关系

| 本计划 Phase | 关联的已有改进 | 关系 |
|-------------|--------------|------|
| Phase 1 | #15 Tool Outcome Learning | 直接增强：OutcomeScore → ContextOutcomeScore |
| Phase 2 | #6/#7 Skill Runner 错误处理 | 闭环：错误分类 → 自修复 |
| Phase 2 | #8.Bug#2 craft_tool 429 重试 | 互补：429 重试是即时缓解，self-repair 是长期修复 |
| Phase 3 | #7.BUG-003 craft_tool Windows 支持 | 前置：craft_tool 能正常工作后才能持久化 |
| Phase 4 | #12 主动记忆召回增强 | 互补：memory 召回 + skill memory 能力摘要 |
| Phase 4 | #37 漂移检测器 | 增强：通用 recover prompt → 具体替代工具建议 |
| Phase 4 | #49 Steering 机制 | 互补：steering 注入规则，skill memory 注入能力数据 |
