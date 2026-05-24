# ProactAgent 论文启发的 MacLaw 机制改进方案

## 文章来源

**Ask Only When Needed: Proactive Retrieval from Memory and Skills for Experience-Driven Lifelong Agents**  
arXiv:2604.20572v1，2026-04-22  
Source: https://arxiv.org/abs/2604.20572

论文提出 ProactAgent：经验召回不是固定注入，也不是每步检索，而是 agent 决策循环中的显式动作；经验库不是单一文本池，而是事实、情节、成功技能、失败技能、对比技能组成的结构化系统；召回价值不是靠相似度猜测，而是由任务结果和反事实分支反馈。

本文档按“理顺流程、改机制”的原则整理。目标不是给现有 recall 外面包一层开关，也不是增加临时 shadow workaround，而是把 MacLaw 的经验生产、召回、注入、归因、治理和技能沉淀连成一条主链路。

---

## 总体结论

MacLaw 应把现有分散能力收敛为一条 **Experience Lifecycle**：

```text
运行轨迹
  -> 经验事件采集
  -> 类型化经验归档
  -> 召回策略决策
  -> 证据/技能均衡注入
  -> 结果归因
  -> priority 与治理状态更新
  -> 技能/规则草稿沉淀
```

这条链路解决三个结构性问题：

1. **什么时候召回**：从系统固定注入，变成 agent/策略层基于缺口判断的显式决策。
2. **召回什么**：从单池相似检索，变成事实证据和行为技能的类型均衡组合。
3. **召回是否有用**：从召回次数和相似度，变成条目级 outcome attribution。

不建议把 ProactAgent 理解为“再加一个检索工具”。真正要改的是主流程：经验必须有类型，召回必须有意图，注入必须有预算，结果必须回写，升级为技能必须经过治理。

---

## 机制原则

### 1. Experience 是一等对象

memory、skill、tool usage、workflow trace、recovery evidence 不应只是各自存储里的局部概念。需要一个统一的经验视图，作为召回和治理的共同接口。

统一视图不等于立刻重写底层存储。底层仍可保留 `memory.Store`、`UsageTracker`、skill library、experience governance；但对上层必须暴露同一种经验契约。

### 2. Retrieval 是决策动作

召回不应只由 prompt builder 在每轮自动塞入。agent 执行中遇到知识缺口、历史工具失败模式、项目约束不明或路径选择不确定时，应能发起显式召回请求。系统再用策略层判断预算、类型、边界和注入方式。

### 3. Injection 受类型和预算约束

不是召回到什么就注入什么。注入应按类型、来源、token 成本、边界和当前任务阶段做裁剪。事实用于校验，情节用于定位，成功技能给可行路径，失败技能给避坑，对比技能给选择理由。

### 4. Utility 是闭环信号

每次召回都要有 usage record。条目是否帮助了任务，应由成功率、步骤数、token、错误类型、用户反馈、可回放对比分支共同决定。priority 只影响排序和候选强度，不删除原始证据。

### 5. Skill 升级必须走治理

失败经验、对比经验、成功路径不能直接变成强规则。它们先成为 evidence，再成为 draft，经过 review 后才进入 skill library 或 steering 规则。

---

## 目标架构

### 核心模块

| 模块 | 职责 | 现有基础 |
|---|---|---|
| Experience Event Sink | 采集轨迹、工具调用、召回、结果、用户反馈 | workflow trace、UsageTracker、experience learning；第一版已落到 `corelib/experience/lifecycle` |
| Experience Store View | 跨 memory/skill/tool 的统一经验视图 | memory.Store、SkillMemory、tool recovery |
| Retrieval Policy | 判断是否召回、召回类型、预算、边界 | proactive recall、system prompt builder |
| Balanced Retriever | 按类型配额检索并去冗余 | RecallDynamic、adaptive recall、theme layer |
| Injection Composer | 把经验组装成可执行上下文 | prompt sections、scene index、source hints |
| Outcome Attributor | 把任务结果归因到召回条目和召回决策 | UsageTracker、ContextOutcomeScore |
| Experience Governance | 审核 harmful/high-impact 经验和升级草稿 | experience governance、skill maintenance |
| Comparative Skill Distiller | 从 A/B 路径沉淀对比技能 | 当前缺失 |

### 主链路

```text
Agent step starts
  -> Retrieval Policy checks context gap
  -> if needed, creates RetrievalDecision
  -> Balanced Retriever returns typed candidates
  -> Injection Composer injects bounded evidence/guidance
  -> Agent acts
  -> Event Sink records action, retrieved IDs, injected token cost
  -> Outcome Attributor updates utility after task/phase result
  -> Governance promotes stable patterns into drafts
```

---

## 经验契约

### ExperienceEntry

```go
type ExperienceEntry struct {
    ID           string
    EntryType    ExperienceEntryType
    WhenToUse    string
    Content      string
    SourceType   string
    SourceURL    string
    EvidenceIDs  []string
    Boundary     ExperienceBoundary
    Priority     float64
    Utility      ExperienceUtilityStats
    Governance   ExperienceGovernanceState
}
```

### ExperienceEntryType

```go
type ExperienceEntryType string

const (
    ExperienceFactual          ExperienceEntryType = "factual"
    ExperienceEpisodic         ExperienceEntryType = "episodic"
    ExperienceSuccessSkill     ExperienceEntryType = "success_skill"
    ExperienceFailureSkill     ExperienceEntryType = "failure_skill"
    ExperienceComparativeSkill ExperienceEntryType = "comparative_skill"
)
```

### ExperienceBoundary

```go
type ExperienceBoundary struct {
    OwnerID     string
    ProjectPath string
    TaskType    string
    Workflow    string
    Toolchain   []string
    TimeWindow  string
    SourceScope string
}
```

### ExperienceUtilityStats

```go
type ExperienceUtilityStats struct {
    RetrievedCount int
    InjectedCount  int
    HelpfulCount   int
    HarmfulCount   int
    SuccessCount   int
    LastUsedAt     time.Time
    AvgTokenCost   float64
    AvgStepDelta   float64
}
```

这个契约应成为 memory recall、skill recall、tool recovery、experience learning 的共同上层接口。没有必要第一步迁移所有存储，但新增能力必须面向这个契约开发。

---

## 五类经验映射

| 类型 | 作用 | MacLaw 来源 | 注入方式 |
|---|---|---|---|
| factual | 校验事实、工具输出、项目状态 | memory facts、tool output refs、SourceURL | 短证据 + 可回查来源 |
| episodic | 复用相似任务轨迹和局部约束 | task artifact、session checkpoint、scene index | 摘要 + trace/source |
| success_skill | 给出成功策略和步骤模式 | skill library、成功 workflow、craft_tool 结果 | 行动建议 |
| failure_skill | 提醒失败模式和规避条件 | tool recovery、self-repair、失败复盘 | 风险/禁忌/修复入口 |
| comparative_skill | 解释为什么 A 路径优于 B 路径 | paired branch、成功/失败路径对比 | 决策理由 |

关键变化：comparative skill 是新增一等类型。它不是普通总结，而是路径选择证据。

---

## 改进阶段

### Phase 1：统一经验事件采集

目标：所有会影响未来行为的信号都先进入同一条事件流。

事件类型：

- `tool_call_started` / `tool_call_finished`
- `memory_retrieved` / `skill_retrieved`
- `experience_injected`
- `workflow_phase_completed`
- `task_succeeded` / `task_failed`
- `user_feedback_received`
- `repair_attempted` / `repair_applied`

新增结构：

```go
type ExperienceEvent struct {
    TraceID      string
    TaskID       string
    EventType    string
    StepIndex    int
    ToolName     string
    EntryIDs     []string
    Query        string
    Reason       string
    TokenCost    int
    Outcome      string
    ErrorClass   string
    CreatedAt    time.Time
}
```

落点：

- `UsageTracker` 不再只记录工具 outcome，还记录 retrieval outcome。
- workflow trace 和 memory recall 共用 `TraceID`。
- 所有自动 recall 和主动 recall 都必须发事件。

验收：

- 任意一次任务可以从 trace 看到“召回了什么、为什么召回、注入了多少、后来结果如何”。
- 事件采集不改变现有执行结果，只建立可观测主链路。

### Phase 2：建立统一 Experience Store View

目标：召回层不直接关心条目来自 memory、skill 还是 tool recovery。

接口：

```go
type ExperienceProvider interface {
    ListExperience(ctx context.Context, scope ExperienceScope) ([]ExperienceEntry, error)
    SearchExperience(ctx context.Context, query ExperienceQuery) ([]ExperienceCandidate, error)
    UpdateUtility(ctx context.Context, update ExperienceUtilityUpdate) error
}
```

Provider：

- `MemoryExperienceProvider`
- `SkillExperienceProvider`
- `ToolRecoveryExperienceProvider`
- `WorkflowExperienceProvider`

机制要求：

- `when_to_use` 是正式字段或正式合成字段，不能只是 prompt 文案。
- `entry_type` 必须稳定输出。
- `boundary` 必须参与过滤。
- source/evidence id 必须可回查。

验收：

- `memory(action=recall, debug=true)` 和 skill recall 可输出同一套 entry metadata。
- 新 provider 可以接入而不改 Retrieval Policy。

### Phase 3：引入 Retrieval Policy

目标：统一决定是否召回、召回类型、预算和边界。

Retrieval Policy 输入：

```go
type RetrievalPolicyInput struct {
    TaskID          string
    CurrentGoal     string
    RecentTrace      []ExperienceEvent
    KnownContext     string
    MissingSignals   []string
    TokenBudget      int
    Boundary         ExperienceBoundary
}
```

输出：

```go
type RetrievalDecision struct {
    ShouldRetrieve bool
    Query          string
    Types          []ExperienceEntryType
    Reason         string
    Budget         RetrievalBudget
    Mode           string // auto, agent_requested, recovery, workflow_phase
}
```

决策来源：

- agent 显式请求：`retrieve_experience`。
- 系统策略：任务启动、阶段切换、失败恢复、上下文缺口。
- 历史统计：同类上下文中召回是否有效。

机制要求：

- 自动 recall 不再直接查库注入，而是先生成 `RetrievalDecision`。
- agent 主动召回也不绕过策略层，必须通过预算和边界检查。
- repeated query、无明确 reason、越界 scope 应被 policy 降权或拒绝。

验收：

- 日志中所有召回都有 `RetrievalDecision`。
- 可以统计 auto vs agent_requested 的收益差异。

### Phase 4：Balanced Retriever 成为主召回路径

目标：从单池检索升级为类型均衡召回。

默认配额：

```json
{
  "factual": 1,
  "episodic": 1,
  "success_skill": 1,
  "failure_skill": 1,
  "comparative_skill": 1
}
```

排序：

```text
score = relevance + priority_weight * priority + boundary_bonus - redundancy_penalty - token_cost_penalty
```

流程：

```text
RetrievalDecision
  -> candidate search per type
  -> boundary filter
  -> utility-aware rerank
  -> type quota selection
  -> cross-type redundancy removal
  -> backfill empty quota
```

机制要求：

- Balanced Retriever 是正式主路径，不是 debug 模式。
- `RecallDynamic`、theme recall、BM25/vector 都可以作为候选源，但最终由 balanced selection 出结果。
- 回填必须记录原因，便于评估某类经验是否长期缺失。

验收：

- trace 显示每类候选数、过滤数、入选数、回填数。
- 召回结果稳定包含 evidence 与 guidance 两组信息。

### Phase 5：Injection Composer 统一注入

目标：召回结果不直接塞 prompt，由 composer 按任务阶段和 token 预算组装。

注入结构：

```text
[Relevant Evidence]
- factual/episodic entries with source hints

[Behavioral Guidance]
- success/failure/comparative skills

[Boundaries]
- project/user/task scope constraints
```

机制要求：

- factual 和 episodic 默认短注入，保留 SourceURL drill down。
- skill 类条目注入为行动约束或策略，不伪装成事实。
- comparative skill 必须保留正反路径，避免只剩抽象结论。
- composer 记录 injected entry ids 和 token cost。

验收：

- prompt section 可追踪到 entry ids。
- 同一条经验被注入后的结果可回写 utility。

### Phase 6：Outcome Attribution

目标：判断这次召回是否有用，并更新条目级 utility。

归因信号：

- 任务成功/失败。
- 工具调用是否减少重试。
- 步数变化。
- token 成本。
- 错误类型是否从“未知”变成“可恢复”。
- 用户反馈。
- 可回放对比分支。

更新：

```go
type ExperienceUtilityUpdate struct {
    EntryID      string
    TraceID      string
    Helpful      bool
    Harmful      bool
    Success      bool
    StepDelta    int
    TokenDelta   int
    Reason       string
    EvidenceType string
}
```

机制要求：

- priority 更新必须有 evidence record。
- harmful 不自动删除，进入治理队列。
- utility 不跨 boundary 泛化，除非治理阶段明确提升 scope。

验收：

- 可查询某条经验为何 priority 上升或下降。
- 可按类型统计召回收益。

### Phase 7：Counterfactual Evaluation 进入评估管线

目标：把 paired-branch 从临时 shadow run 变成标准评估能力。

适用范围：

- 可重放 workflow。
- 只读工具链。
- 测试/构建命令可在隔离 workspace 中执行。
- 模拟环境或 dry-run provider。

流程：

```text
prefix checkpoint
  -> branch A: with retrieval
  -> branch B: policy suppresses retrieval
  -> compare outcome
  -> write CounterfactualRetrievalEvidence
```

机制要求：

- 反事实评估属于评估管线，不属于用户交互热路径。
- 执行隔离、权限、成本预算由统一 evaluator 管理。
- 结果只更新 utility 和 governance evidence，不直接改当前任务执行。

验收：

- evaluator 可按 trace id 重跑。
- 有副作用任务默认不可评估，除非有明确 sandbox/dry-run。

### Phase 8：Comparative Skill Distiller

目标：把稳定的对比证据沉淀为 skill draft。

输入：

- counterfactual evidence。
- 同一任务成功/失败轨迹。
- 工具恢复中的有效路径/无效路径。

输出：

```yaml
type: comparative_skill
when_to_use: "当 Go 测试因 SQLite WAL 锁或并发写入不稳定失败时"
content: |
  优先检查测试是否共享同一 memory.db，或后台 memory pipeline 是否仍在运行。
  与其重复跑全量测试，更稳的是隔离临时 DB、关闭后台维护，或缩小测试范围。
positive_path: "隔离 DB 后运行目标测试"
negative_path: "直接重复运行全量测试"
evidence_ids:
  - trace:...
boundary:
  task_type: "go_test"
  toolchain: ["go", "sqlite"]
```

机制要求：

- 默认写入 draft，不直接进入强执行规则。
- draft 合并后作为 `comparative_skill` 参与 Balanced Retriever。
- skill retire/merge 复用既有 skill maintenance 流程。

验收：

- comparative skill 可被按类型召回。
- 可从 skill 追溯回正反路径 evidence。

---

## 与现有系统关系

| 现有能力 | 调整方向 |
|---|---|
| `RecallDynamic` / adaptive recall | 变成 Balanced Retriever 的候选源，而不是最终注入者 |
| prompt builder proactive recall | 改为调用 Retrieval Policy，再由 Injection Composer 注入 |
| `UsageTracker` | 扩展到 retrieval usage 和 entry utility |
| `SkillMemory` | 变成 Experience Provider，不独立决定注入内容 |
| tool recovery | 输出 failure_skill 和 recovery evidence |
| self-repair | 产生 repair evidence，可被 distiller 转成 failure/comparative skill draft |
| scene index | 作为 episodic/navigation evidence provider |
| experience governance | 作为 harmful/high-impact/update-scope 的统一审核入口 |

---

## 与已有文档关系

| 文档 | 关系 |
|---|---|
| `memento-skills-inspired-improvements.md` | 已有 context-aware outcome、self-repair、craft_tool 持久化；本文把这些纳入统一 Experience Lifecycle |
| `xmemory-memory-improvements.md` | 已有 theme/adaptive recall；本文增加召回决策、类型均衡和效用归因 |
| `tencentdb-agent-memory-inspired-improvements.md` | 已强调 source/evidence/scene；本文沿用 evidence first，避免 priority 覆盖证据 |
| `maclaw-memory-management-consolidation-design.md` | 已强调 evidence over compression；本文要求 skill/priority 都可追溯 |
| `skill-maintenance-*` | comparative skill draft 应进入既有 review/merge/retire 流程 |

---

## 不做的事

1. 不训练 GRPO，不把模型参数更新作为第一阶段目标。
2. 不把 FAISS 作为强依赖；先复用现有 BM25/vector/theme 候选源。
3. 不让 prompt builder 绕过 Retrieval Policy 直接注入经验。
4. 不让 agent 主动召回绕过预算、边界和审计。
5. 不用 priority 删除证据；删除、降权、合并必须走治理。
6. 不把单次失败经验直接升级为全局规则。
7. 不在热路径执行有副作用的反事实分支。

---

## 实施优先级

| 阶段 | 优先级 | 原因 |
|---|---|---|
| Phase 1 事件采集 | P0 | 没有统一事件，就无法做归因 |
| Phase 2 Experience Store View | P0 | 先统一上层契约，避免继续分散 |
| Phase 3 Retrieval Policy | P0 | 召回决策必须收口 |
| Phase 4 Balanced Retriever | P1 | 改善召回质量和上下文结构 |
| Phase 5 Injection Composer | P1 | 保证注入可追踪、可预算 |
| Phase 6 Outcome Attribution | P1 | 形成 priority 闭环 |
| Phase 7 Counterfactual Evaluation | P2 | 成本较高，进入评估管线 |
| Phase 8 Comparative Skill Distiller | P2 | 依赖前面 evidence 和 governance |

---

## 需要确认的设计点

1. `ExperienceEvent` 放在 `corelib/tool`、`corelib/memory`，还是新建独立中心？已采用 `corelib/experience/lifecycle` 子包，避免既有 `corelib/experience` 对 `corelib` 的依赖反向造成 import cycle。
2. `ExperienceEntry` 是持久化新表，还是第一阶段作为 provider view？建议第一阶段 view，第二阶段按性能和治理需求决定持久化。
3. `Retrieval Policy` 是否允许 LLM 参与？建议第一版规则 + 统计信号，LLM 只负责生成 agent-requested query/reason。
4. Balanced Retriever 是否替换自动 recall 默认路径？建议作为新的主路径接入 prompt builder，保留旧路径作为候选源和回退。
5. Counterfactual Evaluation 第一批支持哪些任务？建议从只读工具链、测试命令隔离 workspace、workflow dry-run 三类开始。
6. Comparative skill draft 写入哪里？建议先写入 experience governance draft，再由 skill maintenance 合并进 skill library。

---

## 当前状态

状态：P0 骨架已开工。  
已完成：`corelib/experience/lifecycle` 定义 `Entry`、`Event`、`Provider`、`RetrievalDecision`、`EventTrail`；`UsageTracker` 已可挂载 `EventSink` 并在工具 outcome 时写入 `tool_call_finished` 事件；`memory.Store` 已可挂载同一 `EventSink`，`RecallDynamic` / `RecallDynamicStrict` 会写入 `experience_retrieved`，`ProactiveContextForPrompt` 会在实际 prompt 注入后写入 `experience_injected` 和 token cost；GUI 和 TUI 已创建共享 `EventTrail` 并把 usage/memory 两侧接入同一 sink。  
下一步建议：给 lifecycle event 补 `task_id/trace_id` 来源，让 GUI/TUI 的真实任务、工具 outcome、memory recall、prompt injection 能按一次任务聚合；随后开始做 Retrieval Policy，把自动 recall 先收口为 `RetrievalDecision`。
## 2026-05-24 推进记录

已把 `task_id/trace_id` 纳入热路径事件：`lifecycle.EventContext` 负责把一次运行的 `TraceID` / `TaskID` 注入事件；GUI 在创建 `LoopContext` / AI trace run 后再构建 prompt，因此 proactive recall 与 prompt injection 事件能带上同一条 trace；`memory.ProactivePromptOptions` / `ProactiveRecallOptions` 会把上下文继续传到补充召回路径，避免只在最外层做日志包装。

这一步仍保持机制主线：事件不是 prompt builder 的 side log，而是 recall / injection / tool outcome 共用的生命周期信号。下一步应做 `RetrievalPolicy.Decide(input) -> RetrievalDecision`，让当前 `ProactiveContextForPrompt` 不再直接调用 `RecallProactive`，而是先产生决策，再由统一 retriever/composer 执行。

## 2026-05-24 Retrieval Policy 推进记录

已把 proactive prompt recall 收口到 `RetrievalPolicy.Decide(input) -> RetrievalDecision`：`lifecycle.RetrievalPolicy` / `RetrievalPolicyFunc` / `DefaultRetrievalPolicy` 成为正式机制入口；`ProactiveContextForPrompt` 先记录 `retrieval_decided` 事件，再按决策执行 `RecallProactiveWithDecision`，因此自动召回不再直接绕过策略层。

当前默认策略仍是保守规则版：空 goal 跳过；非空 goal 进入 auto recall；预算和 max entries 进入 `RetrievalBudget`；默认类型覆盖 factual / episodic / success_skill / failure_skill / comparative_skill，并带默认均衡 quota。下一步应把 `RecallLightMem` / `RecallDynamic` 的候选结果提升到 typed candidates，然后接 Balanced Retriever 做类型配额、boundary 过滤、utility-aware rerank。

## 2026-05-24 Balanced Retriever 推进记录

已把 proactive recall 的最终选择从“按候选原始顺序截断”改成 Balanced Retriever：候选先被映射为 factual / episodic / success_skill / failure_skill / comparative_skill，再按 `RetrievalDecision.Budget.Quotas` 和 `Decision.Types` 做类型配额选择；空配额会回填原始候选，保证机制收口但不牺牲召回覆盖。

当前版本仍复用 `RecallLightMem` / `RecallDynamic` 作为候选源，类型判定先用 category、source、tag、content marker 的保守规则。下一步应把候选源显式升级为 `lifecycle.Candidate`，补齐 relevance / priority / boundary / token cost 分数，再让 Balanced Retriever 按 `relevance + priority + boundary - redundancy - token_cost` 排序。

## 2026-05-24 Candidate Scoring 推进记录

已把 Balanced Retriever 的内部候选升级为 `lifecycle.Candidate`：每个 memory entry 会生成 typed candidate，包含 `EntryType`、source/evidence/boundary 元数据、`Relevance`、`PriorityScore`、`BoundaryScore` 和 `TokenCost`。选择排序从原始顺序进一步变成 `relevance + priority + boundary - token_cost`，并在进入 quota 选择前做 owner/project boundary 过滤。

当前 priority 先复用 memory 的 `Pinned`、`Strength`、`AccessCount`；boundary 先复用 `OwnerID`、`ScopeProject`、project path tag 和 `MemoryBoundary`；token cost 先用 prompt token 估算。下一步应补 redundancy penalty 和跨 provider 候选合并，让 skill/tool recovery/workflow trace 也能以同一 `lifecycle.Candidate` 进入 Balanced Retriever。

## 2026-05-24 Redundancy Penalty 推进记录

已给 Balanced Retriever 增加冗余抑制：同类型候选会用内容 token Jaccard 相似度做轻量 redundancy 判断，quota 选择和第一次回填都会跳过与已选候选高度重复的条目；如果预算仍未填满，才允许冗余候选作为最后回填，避免“去重导致没上下文”。

这一步把 `relevance + priority + boundary - token_cost` 扩展为实际选择流程里的 `- redundancy`，但仍保持保守：不删除证据、不改写 memory，只影响本次注入选择。下一步应做跨 provider 候选池，把 memory、skill、tool recovery、workflow trace 都转成 `lifecycle.Candidate` 后统一去重和排序。

## 2026-05-24 Candidate Pool 推进记录

已把 Balanced Retriever 从 `corelib/memory` 局部逻辑上移到 `corelib/experience/lifecycle.SelectBalancedCandidates`：quota、rerank、redundancy、backfill 都在 lifecycle 层对 `[]lifecycle.Candidate` 执行。memory 现在只负责把 `Entry` 转成候选，再调用统一选择器并映射回原 memory entry。

这一步让跨 provider 合并有了真实入口：后续 skill、tool recovery、workflow trace 不需要接入 memory.Store，也不需要各自实现选择逻辑，只要产出 `lifecycle.Candidate` 即可进入同一个候选池。下一步应实现 `MemoryExperienceProvider` 的 `SearchExperience`，再接一个 `CompositeExperienceProvider` 汇合多 provider 候选。

## 2026-05-24 Provider 推进记录

已实现 `memory.ExperienceProvider`，把 `memory.Store` 接入 `lifecycle.Provider`：`ListExperience` 按 boundary/type 输出统一 `lifecycle.Entry`，`SearchExperience` 复用 memory recall 候选并转为 typed/scored `lifecycle.Candidate`，`UpdateUtility` 把 helpful/success/harmful 信号回写到 memory 的 `AccessCount` / `Strength`。

已实现 `lifecycle.CompositeProvider`，支持多个 provider 的 list/search/update fanout。这样 memory、skill、tool recovery、workflow trace 后续都可以作为 provider 并联接入，上层只面对统一候选池。下一步应让 proactive prompt recall 通过 provider search 构造候选池，而不是直接从 `RecallLightMem` 取 `Entry`。

## 2026-05-24 Provider-Driven Recall 推进记录

已把 `RecallProactiveWithDecision` 改为 provider-driven：自动 prompt recall 不再直接从 `RecallLightMem` 取 `Entry`，而是通过 `MemoryExperienceProvider.SearchExperience` 得到 `lifecycle.Candidate` 池，再交给 `lifecycle.SelectBalancedCandidates` 统一选择，最后映射回 memory entry 注入 prompt。

这一步把“召回决策 -> provider search -> balanced candidate selection -> injection”的主链路串起来了。`RecallLightMem` / `RecallDynamic` 退到 provider 内部候选源位置，不再是 prompt recall 的外层主流程。下一步应把 skill/tool recovery/workflow trace provider 接进 `CompositeProvider`，并让 prompt recall 使用 composite provider 而不是单一 memory provider。

## 2026-05-24 Composite Provider / Candidate Injection Progress

Done: proactive recall now has an explicit provider hook on `ProactiveRecallOptions.Provider`. The default path builds a `lifecycle.CompositeProvider` with the memory provider inside, so the hot path is already shaped as `policy decision -> composite provider search -> balanced candidate selection -> candidate injection`. This is mechanism-level plumbing: future skill/tool/workflow providers can join by implementing `lifecycle.Provider`; they do not need to write memory entries first and do not need their own prompt-injection logic.

Done: prompt injection now formats `[]lifecycle.Candidate` directly through `FormatExperienceCandidatesForPrompt`. `ProactiveContextForPrompt` still returns `[]memory.Entry` for existing host logging compatibility, but the rendered prompt no longer depends on converting every selected candidate back to memory. This removes the previous bottleneck where non-memory providers could be retrieved and selected but could not be injected.

Done: retrieval and injection lifecycle events now record selected candidate ids through `recordCandidateExperienceEvent`, not only memory entry ids. That keeps `experience_retrieved` and `experience_injected` aligned with the unified candidate pool.

Done: composite provider search no longer truncates the merged candidate pool before selection. Each provider can return its candidates, then the lifecycle balanced retriever applies type quotas, redundancy control, and budget. This prevents an early memory provider from starving later skill/tool/workflow providers.

Next mechanism step: add first non-memory provider, likely a skill/workflow provider that exposes governed skill drafts or recent workflow repair evidence as `success_skill`, `failure_skill`, and `comparative_skill` candidates. After that, wire provider fanout from the app layer instead of relying only on the default memory provider.

## 2026-05-24 Skill Provider Progress

Done: added `skill.ExperienceProvider`, the first non-memory provider for the unified lifecycle path. Installed skills now expose governed `lifecycle.Candidate` entries without being copied into memory first. Active and historically successful skills become `success_skill`; skills with `needs_review`, `LastError`, or failure-only history become `failure_skill`; repaired/workaround-bearing skills become `comparative_skill` evidence.

Done: GUI proactive prompt recall now builds a composite provider from memory plus local skills. The flow is now `RetrievalPolicy -> CompositeProvider(memory, skill) -> BalancedRetriever -> CandidateInjection`, so skill evidence and memory evidence compete under the same type quotas, priority scoring, redundancy handling, and injection budget.

Done: skill provider candidates carry governance state (`reviewed`, `draft`, `blocked`), source metadata, trigger/required-arg context, usage-derived priority, and failure/repair evidence. This keeps skill recall as evidence, not as direct automatic execution.

Next mechanism step: add workflow/repair evidence provider, then connect outcome attribution back into provider utility updates so successful/failed injected candidates can update priority through the same lifecycle.

## 2026-05-24 Workflow Provider Progress

Done: added `workflow.ExperienceProvider`, so active workflow state can enter the same lifecycle candidate pool. Phase outputs become `episodic` evidence; failed quality gates become `failure_skill` evidence; pending review regeneration becomes `comparative_skill` evidence. This keeps workflow knowledge as typed evidence rather than hidden prompt-only context.

Done: GUI proactive prompt recall now builds `CompositeProvider(memory, skill, workflow)` when an active workflow exists for the user. The recall chain remains one path: `RetrievalPolicy -> CompositeProvider -> BalancedRetriever -> CandidateInjection`. Workflow evidence now competes with memory and skill evidence under shared quotas, boundary scoring, redundancy handling, and token budget.

Done: workflow candidates carry owner/project/workflow boundary metadata and governance state (`evidence_only` for phase outputs, `draft` for gate failures and review revisions). This prepares outcome attribution to update candidate utility without turning a single failed phase into a hard global rule.

Next mechanism step: outcome attribution. Need connect selected candidate ids from `experience_injected` with later task/tool/workflow success or failure events, then call provider `UpdateUtility` through the same composite provider instead of writing priority from scattered locations.

## 2026-05-24 Outcome Attribution Progress

Done: added `lifecycle.AttributingEventSink`. It wraps the lifecycle event trail, watches later outcome events, finds the latest same-trace `experience_injected` event, and turns each injected candidate id into a `UtilityUpdate`. Success-like outcomes mark entries helpful/successful; failure-like outcomes mark them harmful. Attribution is trace-bound, deduplicated, and uses the provider interface rather than writing memory priority from prompt code.

Done: GUI and TUI now wire memory/tool lifecycle events through the attributing sink. GUI resolves a composite provider for attribution from memory plus the current skill/workflow providers; TUI starts with memory provider attribution. This makes `experience_injected -> outcome event -> provider.UpdateUtility` a real mechanism path.

Current limit: tool usage events still need trace/task context at their call sites before tool-level attribution can fire reliably. The sink intentionally refuses empty-trace attribution to avoid cross-task credit assignment.

Next mechanism step: carry `EventContext` into tool execution/usage tracking, then emit task/workflow terminal events (`task_succeeded`, `task_failed`, `workflow_phase_completed`) from agent-loop boundaries so attribution has stable outcome anchors.

## 2026-05-24 Tool Context / Terminal Outcome Progress

Done: tool usage recording now carries `lifecycle.EventContext` from the active agent loop. Every executed agent-loop tool call records a `tool_call_finished` event through `UsageTracker.RecordExperience`, with `TraceID` and `TaskID` attached from `LoopContext`. Trial reflection no longer owns tool usage as a side path; it observes the loop, while tool execution itself emits lifecycle evidence.

Done: agent-loop boundaries now emit terminal lifecycle anchors. Normal IM loops record `task_succeeded` or `task_failed` during finalization; background loops record the same after their loop state is resolved. Stopped/paused loops are intentionally not marked harmful, so user cancellation does not become negative utility evidence.

Done: workflow phase capture now emits `workflow_phase_completed` with the same loop trace when `SavePhaseOutputAndMaybeAdvance` accepts a generated phase output. This connects workflow deliverables to the same attribution chain as prompt-injected experience.

Mechanism chain now active on the hot path: `RetrievalPolicy -> CompositeProvider -> BalancedRetriever -> CandidateInjection -> tool/task/workflow outcome event -> AttributingEventSink -> Provider.UpdateUtility`. Remaining gap: workflow review intent events and longer-term skill draft promotion still need governance-level handlers, but the core attribution spine no longer depends on workaround logging.

## 2026-05-24 Workflow Review Feedback Progress

Done: workflow review intent is now part of the lifecycle event stream. When a workflow phase output is captured, GUI stores the phase's `TraceID` / `TaskID` as the review context. When the user later confirms, supplements, skips, cancels, or switches task, GUI emits `user_feedback_received` on that same trace.

Done: attribution now understands review feedback. `confirm` is treated as helpful/successful evidence; `supplement`, `modify`, `cancel`, and `switch_task` are treated as harmful evidence for the injected candidates that shaped the phase output; `other` stays neutral. This turns review responses into utility signals instead of free-form workflow control text.

Done: review context is lifecycle-managed: confirm/skip/cancel/switch clear the stored phase context; supplement keeps it until the regenerated phase output replaces it. Session cleanup also clears pending review context. This keeps feedback attribution tied to the phase lifecycle rather than a loose global flag.

Next mechanism step: skill draft promotion/governance. Use attributed failures and repair evidence to form governed comparative drafts, then route them through review/block/promote states before they become reusable skills.

## 2026-05-24 Skill Governance Draft Provider Progress

Done: added `skill.GovernanceDraftProvider`. It turns the existing skill maintenance plan into first-class lifecycle candidates without mutating the skill library. Repair, contract, merge, lifecycle-refresh, index-refresh, archive, and needs-review recommendations now become typed `lifecycle.Entry` values with `GovernanceDraft` state.

Done: GUI proactive recall and attribution provider resolution now include skill governance drafts alongside memory, installed skills, and workflow state. The shared path is still one mechanism: `CompositeProvider(memory, skill, skill_governance_draft, workflow) -> BalancedRetriever -> CandidateInjection`. Drafts compete under the same type quotas, relevance, boundary, priority, and redundancy rules.

Done: draft semantics are explicit. `attempt_repair`, `improve_contract`, `merge_duplicate`, `refresh_lifecycle`, and `refresh_index` are `comparative_skill` evidence; `mark_needs_review` and `archive_stale` are `failure_skill` evidence. Every injected draft says review is required before repair, merge, archive, or promotion, keeping skill evolution governed rather than automatic.

Next mechanism step: add an approval/promotion executor that can consume reviewed draft ids and apply only the allowed metadata subset, while file-backed patches and merges remain review packets until a separate explicit apply step.

## 2026-05-24 Reviewed Draft Execution Progress

Done: added `ExecuteReviewedGovernanceDrafts`. It consumes reviewed skill governance draft ids, rebuilds the current maintenance plan, selects only matching draft actions, and delegates to the existing approval-gated maintenance executor. This fixes the previous coarse approval shape where approving an action name could apply every action of that kind.

Done: real execution now requires `reviewed_draft_ids` or the older `approved_actions`; draft-id execution is the safer path because it is scoped to exact `skill_draft:<action>:<skill>:<related>` ids. Dry-run can preview selected draft ids or all current drafts.

Done: file-backed patches and merges still do not auto-apply. Reviewed draft execution can apply local metadata updates such as `needs_review`, lifecycle refresh, config-backed contract completion, stale learned-skill archive, and explicitly allowed duplicate retirement. File-backed contract patches return `PatchDraft`; merge consolidation returns or gates through `MergeDraft` until a separate explicit apply step.

Done: `manage_skill(action="execute_maintenance_plan")` now accepts `approved_draft_ids`, so the mechanism is reachable from the existing governed skill-maintenance tool without adding a parallel workaround path.

Next mechanism step: persist draft review state and connect `record_draft_review` / `approved_draft_ids` so approved review records can be selected directly from the governance queue.

## 2026-05-24 Draft Review Queue Execution Progress

Done: `record_draft_review` now accepts and stores an exact `draft_id`. Draft review traces expose that id through `ExperienceTraceDetail.DraftID`, so the governance queue can carry the actionable pointer instead of relying on users to copy an action name or infer a skill name from markdown.

Done: completed `skill_draft` reviews now return a recommended `manage_skill(action="execute_maintenance_plan", dry_run=true, approved_draft_ids=[...])` tool call. The review record remains non-executing; it only hands off to an explicit preview path.

Done: `manage_skill(action="execute_maintenance_plan")` now accepts `approved_review_trace_ids`. It resolves each trace to a completed `skill_draft_review`, extracts the stored `draft_id`, and feeds that into `approved_draft_ids`. Non-skill, incomplete, or draft-id-less review traces are rejected before any skill metadata is touched.

Mechanism now forms a closed governed loop: `skill governance draft -> record_draft_review(completed, draft_id) -> approved_review_trace_ids -> approved_draft_ids -> reviewed draft executor`. Execution stays opt-in and scoped to exact reviewed draft ids.

Next mechanism step: surface approved skill draft reviews in governance summaries as the preferred execution queue, so operators can inspect pending reviewed drafts without manually filtering trace details.

## 2026-05-24 Approved Skill Draft Queue Progress

Done: approved `skill_draft_review` records are now first-class governance queue items. `ExperienceLearningSnapshot` collects completed skill draft reviews with stored `DraftID` into `approved_skill_draft_reviews` and exposes `approved_skill_draft_review_count`, so reviewed skill governance drafts no longer hide inside generic follow-up audit history.

Done: governance summary now prefers `execute_approved_skill_draft_reviews` when no higher-priority review or next-action queue is pending. The recommended tool call points directly to `manage_skill(action="execute_maintenance_plan", dry_run=true, approved_review_trace_ids=[...])`, using the review trace as the durable approval pointer. This preserves the mechanism chain: review trace -> draft id resolution -> dry-run preview -> explicit confirm.

Done: `follow_up_actions` also recognizes completed `skill_draft_review` traces with a `DraftID` and returns the same dry-run `manage_skill` handoff. Operators can inspect the queue or query the exact follow-up class and still land on the governed executor path, not a copy/paste workaround.

Next mechanism step: add execution audit feedback after `manage_skill` applies reviewed draft ids, so governance summary can distinguish approved-but-not-previewed, previewed, applied, and blocked draft reviews instead of repeatedly surfacing already-consumed approvals.

## 2026-05-24 Skill Draft Execution Audit Progress

Done: `manage_skill(action="execute_maintenance_plan")` now writes execution-state audit back to the approved `skill_draft_review` trace when called through `approved_review_trace_ids`. Dry-run records `previewed`; successful confirmed execution records `applied`; failed preview/execution or save failure records `blocked`.

Done: `ExperienceTraceDetail` now exposes `draft_execution_status`, `draft_execution_at`, and `draft_execution_note`. `approved_skill_draft_reviews` keeps previewed approvals visible but removes applied/blocked approvals from the active execution queue, so already-consumed reviews stop being recommended as fresh work.

Done: the audit state stays on the review trace, not in a side cache. The mechanism remains trace-native: `record_draft_review -> approved_review_trace_ids -> manage_skill dry-run/apply -> same review trace execution audit -> governance queue refresh`.

Next mechanism step: split the governance summary into explicit subqueues (`approved_unpreviewed`, `previewed_waiting_confirm`, `applied`, `blocked`) and use the blocked queue to produce repair/evidence follow-up drafts instead of only hiding it from active execution recommendations.

## 2026-05-24 Skill Draft Review Subqueue Progress

Done: skill draft review governance is now split into explicit state queues: `approved_unpreviewed`, `previewed_waiting_confirm`, `applied`, and `blocked`. The older `approved_skill_draft_reviews` compatibility list is now the active execution queue (`approved_unpreviewed + previewed_waiting_confirm`), while applied and blocked records stay visible in `skill_draft_review_queues` for audit and follow-up.

Done: governance summary now exposes `blocked_skill_draft_review_count` and the full `skill_draft_review_queues` object. Previewed approvals stay in the active queue and can be confirmed; applied approvals leave the active queue; blocked approvals are no longer silently hidden.

Done: blocked skill draft executions now have a governance action: `inspect_blocked_skill_draft_reviews`. When no higher-priority queue is pending, governance recommends a read-only `experience_learning(action="trace_details", filter="followups", kind="skill_draft_review", query="skill_draft_blocked")` handoff. This creates a typed repair/evidence intake point without retrying execution or approving another draft automatically.

Next mechanism step: convert blocked skill draft traces into non-executing repair/evidence drafts, with source draft id, blocked reason, current maintenance-plan diff, and required reviewer decision before re-approval.

## 2026-05-24 Blocked Skill Draft Repair Draft Progress

Done: added `build_blocked_skill_draft`. It consumes a blocked completed `skill_draft_review` trace and emits a non-executing repair/evidence draft with the original `draft_id`, source trace, execution status/note, source evidence, and reviewer checklist.

Done: blocked repair drafts include a current maintenance-plan diff by dry-running the reviewed draft id against the current local skill maintenance plan. The draft marks whether the original reviewed draft is still present; if not, it tells the reviewer to treat the approval as stale until a new draft is accepted.

Done: `inspect_blocked_skill_draft_reviews` now recommends `experience_learning(action="build_blocked_skill_draft", trace_id=...)` when a blocked queue item exists. This moves blocked approvals into a typed repair/evidence intake path instead of a generic trace inspection path, while still avoiding retry, skill mutation, file writes, or re-approval.

Next mechanism step: let the blocked repair/evidence draft be recorded as its own `skill_draft` review outcome, then use that outcome to either reopen preview with a new draft id or permanently close the blocked trace as stale/rejected.

## 2026-05-24 Blocked Draft Review Resolution Progress

Done: blocked repair/evidence drafts can now feed back into review state. When `record_draft_review` records a completed `skill_draft` review with `source_trace_id` pointing at a blocked skill draft trace and a replacement `draft_id`, the source blocked trace receives a `reopened` execution audit and leaves the blocked queue; the new review enters the active preview queue.

Done: if the repair/evidence review records a blocked outcome for a blocked source trace, the source trace is marked `closed`, removing it from active and blocked recommendation queues while preserving the audit trail in trace details.

Done: execution-state tag replacement now uses desired tags rather than merge-only updates, so a trace has one current `skill_draft_execution_status` at a time (`previewed`, `applied`, `blocked`, `reopened`, or `closed`). This prevents stale blocked tags from keeping reopened traces in the blocked queue.

Mechanism now supports: `blocked trace -> build_blocked_skill_draft -> record repair/evidence review -> reopened preview with new draft id OR closed stale/rejected trace`.

Next mechanism step: expose reopened/closed counts in governance summary history and add a lightweight stale-age policy so old blocked traces are nudged toward closure instead of staying indefinitely blocked.

## 2026-05-24 Blocked Draft History / Stale Policy Progress

Done: governance summary now keeps reopened and closed skill-draft review history in `skill_draft_review_queues.reopened` and `.closed`, with top-level queue counts. Reopened and closed traces stay auditable but no longer enter the active preview or blocked repair queues.

Done: blocked skill draft reviews now carry a lightweight stale-age policy. If a blocked execution has been blocked for at least 14 days, the queue item is marked `stale` with `stale_days` and a `stale_recommendation` telling the operator to close or replace the approval before retrying preview.

Done: `inspect_blocked_skill_draft_reviews` changes its reason when stale blocked reviews exist, and the recommended focus context carries the stale fields for the priority blocked trace. This makes stale closure pressure part of the governance mechanism, not a UI-side reminder.

Next mechanism step: turn stale blocked recommendations into explicit close/reopen review templates, so the operator can choose `closed` or replacement `draft_id` from a guided non-executing action instead of hand-authoring the review note.

## 2026-05-24 Blocked Draft Close/Reopen Template Progress

Done: added `record_blocked_skill_draft_review`, a guided non-executing action for blocked skill draft traces. It accepts `resolution=close` or `resolution=reopen`; reopen requires `replacement_draft_id`, while close records a stale/rejected closure note automatically.

Done: `build_blocked_skill_draft` now returns `review_options.close` and `review_options.reopen` tool-call templates. Operators no longer need to hand-author the review note or remember which status maps to closing versus reopening.

Done: close/reopen resolution still routes through `RecordExperienceDraftReview`, so the same trace-native resolution logic applies: close marks the source blocked trace `closed`; reopen marks it `reopened` and places the replacement draft review into the active preview queue.

Next mechanism step: add UI-facing affordance metadata around those `review_options` so the frontend can render two explicit buttons with required replacement-draft input instead of exposing raw tool-call args.

## 2026-05-24 Blocked Draft UI Affordance Progress

Done: `build_blocked_skill_draft` now returns typed `review_affordances` beside the compatibility `review_options` map. The affordances define stable ids, labels, intents, button variants, required inputs, tool calls, and non-executing boundaries for close and reopen decisions.

Done: close has no required input and records `record_blocked_skill_draft_review(resolution="close")`; reopen requires `replacement_draft_id` and records `resolution="reopen"`. This gives the UI enough metadata to render two explicit review actions without reverse-engineering tool-call payloads.

Mechanism now reads: `blocked trace -> repair/evidence draft -> typed close/reopen affordance -> record_blocked_skill_draft_review -> closed audit OR reopened active preview queue`.

Next mechanism step: wire the frontend review panel to consume `review_affordances`, so operators use governed buttons and input validation rather than hand-editing JSON-like tool calls.

## 2026-05-24 Blocked Draft Frontend Affordance Progress

Done: the experience-learning frontend now consumes `review_affordances` for blocked skill draft reviews. A blocked `skill_draft_review` trace exposes a repair draft button, renders the non-executing repair/evidence draft, then shows governed close/reopen actions from backend metadata.

Done: reopen now has UI validation for `replacement_draft_id` before calling `RecordBlockedSkillDraftReview`; close and reopen both route through the typed backend method instead of asking the operator to edit raw tool-call arguments.

Done: Wails bindings and component tests cover the new flow: `BuildExperienceBlockedSkillDraft -> review_affordances -> replacement_draft_id input -> RecordBlockedSkillDraftReview`.

Next mechanism step: add governance queue rendering for skill draft review subqueues (`approved_unpreviewed`, `previewed_waiting_confirm`, `blocked`, `reopened`, `closed`) so operators can enter the right trace directly from queue state instead of filtering traces manually.

## 2026-05-24 Skill Draft Review Queue UI Progress

Done: governance summary now renders `skill_draft_review_queues` as first-class queue cards for approved, previewed, blocked, reopened, and closed review states. Each card shows count, leading trace, draft id/status context, and stale blocked recommendations when present.

Done: queue cards can focus the related trace directly, so blocked repairs and preview confirmations start from governance state rather than manual trace filtering.

Mechanism now exposes both layers in the UI: queue state tells the operator what class of work exists, and trace detail exposes the governed action (`Repair Draft`, close/reopen affordance, or preview/apply handoff).

Next mechanism step: add an explicit preview-confirm affordance for `previewed_waiting_confirm`, so the operator can confirm an already-previewed skill draft review through a typed UI path instead of re-running the generic maintenance executor manually.

## 2026-05-24 Previewed Draft Confirm Affordance Progress

Done: `previewed_waiting_confirm` queue items now carry an `execution_affordances` entry with stable id `confirm_previewed_skill_draft`, label, intent, and the governed `manage_skill(action="execute_maintenance_plan", dry_run=false, confirm=true, approved_review_trace_ids=[...])` tool call.

Done: added `ConfirmPreviewedSkillDraftReview(trace_id)` as a typed App method. It verifies the trace is a completed `skill_draft_review` with `draft_execution_status=previewed`, then applies through the same reviewed draft executor path and writes the existing applied/blocked execution audit.

Done: the governance queue UI renders the confirm button from queue affordance metadata and calls the typed method. Operators no longer need to hand-run `manage_skill` after a successful preview.

Mechanism now reads: `approved review -> dry-run preview -> previewed queue affordance -> typed confirm -> reviewed draft executor -> applied/blocked audit -> queue refresh`.

Next mechanism step: split the queue confirm result into a compact execution receipt in the UI, showing executed count, blocked reason, and refreshed queue state so confirmation has visible closure without opening raw JSON.

## 2026-05-24 Review / Fix / Optimize Pass

Fixed: removed stray `execution_affordances` typing from unrelated follow-up summaries and kept affordance metadata scoped to skill draft review queue items.

Optimized: preview confirmation now renders a compact execution receipt with applied/blocked status, executed count, queued count, and error reason. Operators get visible closure after confirm without reading raw JSON.

Verified: targeted Go, TypeScript, frontend component, Vite build, and strict encoding checks pass after the cleanup.

Next mechanism step: include queue-refresh deltas in the receipt, so the UI can say which queue the trace moved into (`applied` or `blocked`) after confirmation.

## 2026-05-24 Review / Fix / Optimize Pass 2

Fixed: `normalizeExperienceLearningRecommendedToolCall` now preserves an explicit `non_executing=false`. This matters for the preview-confirm affordance, because that action is intentionally executable after a dry-run preview and must not be mislabeled as read-only.

Fixed: added a regression assertion that `confirm_previewed_skill_draft` exposes `non_executing=false` in its tool-call metadata.

Verified: targeted Go, TypeScript, component, Vite, and strict encoding checks pass after the executable-affordance fix.

Next mechanism step: include post-confirm queue-refresh deltas in the receipt, so the UI can show whether the trace moved to `applied` or `blocked` after confirmation.

## 2026-05-24 Review / Fix / Optimize Pass 3

Fixed: `record_draft_review(completed, kind="skill_draft")` now recommends `approved_review_trace_ids` instead of raw `approved_draft_ids`. The immediate handoff uses the review trace as the durable approval pointer, so dry-run preview and final confirm both write audit back to the same review trace.

Fixed: blocked draft repair resolution no longer silently ignores source-trace audit update failures. A failed source audit update now returns an error instead of leaving the operator with a false-success close/reopen result.

Verified: updated tests assert the immediate skill-draft review recommendation uses `approved_review_trace_ids`.

Next mechanism step: make the record-and-source-audit update atomic at the memory-store layer, so repair review creation and source trace transition cannot partially commit.

## 2026-05-24 Review / Fix / Optimize Pass 4

Fixed: `approved_review_trace_ids` now enforces execution state. Dry-run accepts only unpreviewed or already-previewed reviews; confirmed execution requires a previewed review; applied, blocked, reopened, closed, or unknown states are rejected before skill metadata can be touched.

Fixed: `ConfirmPreviewedSkillDraftReview` now returns post-confirm trace deltas: `review_trace_id`, `draft_id`, `draft_execution_status`, and `draft_execution_queue`. The UI receipt shows the refreshed queue (`applied` or `blocked`) beside executed/queued counts and error text.

Optimized: blocked repair/close source audit updates are now pre-staged and injection-scanned before the new review record is written. This reduces partial-success risk and turns unsafe source audit content into a hard error before operator-facing success is produced.

Verified: targeted Go tests, frontend component tests, `npx tsc --noEmit`, `npx vite build`, and strict encoding check pass after the replay guard and receipt delta changes.

Remaining mechanism gap: true multi-entry atomicity still belongs in the memory-store write layer. The current app-layer staging prevents known false-success paths, but a store-level batch/transaction API is still the clean endpoint.

## 2026-05-24 Review / Fix / Optimize Pass 5

Fixed: reviewed governance draft execution now treats missing reviewed draft ids as a hard blocked result. If the current maintenance plan no longer contains the reviewed `draft_id`, `ExecuteReviewedGovernanceDrafts` returns `ok=false` with an error instead of reporting a successful preview/apply with only a skipped action.

Fixed: `manage_skill(action="execute_maintenance_plan")` now records that missing-draft result back to the review trace as `skill_draft_execution_status:blocked`. This keeps stale approvals from being mislabeled `previewed` and moves them into the blocked repair/evidence queue.

Fixed: skill-maintenance save failures now return structured JSON instead of plain text, while still writing a blocked audit to the review trace. `ConfirmPreviewedSkillDraftReview` can therefore return a compact blocked receipt with refreshed queue state instead of surfacing an unstructured error.

Verified: added regression tests for missing reviewed draft ids in `corelib/skill` and the GUI trace-audit path; targeted Go tests pass.

Next mechanism step: add a memory-store batch/transaction write API for review-record + source-trace transitions, replacing the current app-layer preflight staging with true all-or-nothing persistence.

## 2026-05-24 Review / Fix / Optimize Pass 6

Fixed: missing reviewed draft ids now block before any selected reviewed draft action runs. If a request mixes one valid `draft_id` with one stale/missing `draft_id`, the whole reviewed execution returns `ok=false`, skips every selected action, and returns the original skill list unchanged. This prevents partial application under a stale approval set.

Fixed: `manage_skill` guard responses now preserve the requested execution mode. `dry_run=false` without `confirm=true`, or without approved actions/drafts, returns structured JSON with `dry_run:false` instead of pretending the blocked request was a dry-run preview.

Verified: added regression coverage for mixed valid+missing reviewed draft ids and for the `dry_run=false` guard JSON. Targeted `corelib/skill` and `gui` tests pass.

Next mechanism step: thread the same all-or-nothing approval-set rule into multi-trace `approved_review_trace_ids` audits, so a batch confirm cannot partially audit some traces if another trace is invalid at audit-write time.

## 2026-05-24 Review / Fix / Optimize Pass 7

Fixed: multi-trace skill draft execution audit now prevalidates the whole trace batch before writing any audit line. If one trace is not a completed `skill_draft_review` with a `draft_id`, no earlier trace in the same batch is marked `previewed`, `applied`, or `blocked`.

Fixed: staged audit entries are injection-scanned before persistence. This moves known content rejection ahead of writes and avoids false partial audit success for invalid review batches.

Verified: added regression coverage for a valid+invalid audit batch; the valid trace remains unmodified after the batch is rejected. Targeted GUI tests pass.

Remaining mechanism gap: persistence-level failures during the final write loop can still partially commit without a memory-store transaction. Batch prevalidation removes semantic partial commits; true disk/backend atomicity still needs store-level transaction support.

## 2026-05-24 Review / Fix / Optimize Pass 8

Fixed: added a store-level batch update mechanism for existing memory entries. `UpdateEntriesByID` validates the whole batch before changing in-memory state, and backends can implement `BatchStorageBackend` for one-shot persistence.

Fixed: SQLite memory backend now implements atomic `UpdateEntries` with a single transaction and per-entry version increments. JSON backend exposes the same batch hook as a no-op persistence signal, preserving existing single-process behavior.

Fixed: skill draft execution audit now uses the memory-store batch update instead of per-trace upserts. Multi-trace audit writes are no longer just semantically prevalidated; SQLite-backed stores now persist the audit batch transactionally.

Verified: added SQLite batch persistence coverage and reran targeted memory and GUI tests.

Remaining mechanism gap: use the same batch API for blocked repair review creation plus source-trace transition, so `RecordExperienceDraftReview` close/reopen can become fully transactional too.

## 2026-05-24 Review / Fix / Optimize Pass 9

Fixed: added `Store.UpsertEntriesByID`, a batch create-or-update path for exact-id entries. It validates all entries before mutating memory, then uses the backend batch hook when available.

Fixed: blocked skill draft repair resolution now writes the new review record and the source blocked trace transition through one memory-store batch. On SQLite-backed stores this is a single transaction, so close/reopen no longer has the old create-review-then-update-source partial commit path.

Fixed: removed the separate source-trace resolution write helper from `RecordExperienceDraftReview`; source transition is now part of the governed review-record batch when required.

Verified: added SQLite coverage for mixed create+update batch persistence and reran targeted memory and GUI tests.

Remaining mechanism gap: broader generated-memory writers still use individual upsert paths. The skill draft review/confirm chain now has transactional batch persistence, but other multi-entry governance flows should migrate to the same primitive as they grow.

## 2026-05-24 Review / Fix / Optimize Pass 10

Fixed: added GUI-level regression coverage for the transactional blocked repair path. `RecordBlockedSkillDraftReview(reopen)` is now verified against a SQLite-backed memory store: the replacement review record and the source trace `reopened` transition both appear in the backend after one call.

Optimized: the test checks persistence below the in-memory index via `backend.LoadAll`, so it catches transaction/write-path regressions rather than only confirming the live store view.

Verified: targeted GUI tests pass for the SQLite batch blocked-repair path and the existing skill draft review queue flow.

Next mechanism step: add a negative SQLite batch test with an injected backend write failure, so transactional rollback is proven for failed mixed create/update batches, not only successful ones.

## 2026-05-24 Review / Fix / Optimize Pass 11

Fixed: added negative SQLite transaction coverage for `UpdateEntries`. The test injects a trigger failure on the second row of a batch and verifies the first row is not persisted.

Fixed: the same rollback test verifies `memory_meta.max_version` is rolled back to `0`, proving version increments are inside the transaction rather than leaking through a failed batch.

Verified: targeted memory batch tests pass for successful update, successful create+update, and forced-failure rollback.

Next mechanism step: inspect SQLite FTS maintenance for batch writes. Batch persistence currently protects the main `memories` table and version metadata; search index refresh should be audited so transactional writes and text search stay aligned.

## 2026-05-24 Review / Fix / Optimize Pass 12

Fixed: SQLite `SaveEntry`, `UpdateEntries`, and `DeleteEntry` now maintain `memories_fts` inside the same transaction as the main `memories` row and version update. This replaces the previous unused/out-of-band FTS helpers.

Fixed: single-entry saves and deletes now also use transactions, so `memory_meta.max_version`, the main row, and the FTS row commit or roll back together.

Verified: added FTS alignment coverage for save, batch update, old-text removal, and delete. Existing CJK fallback, filtered FTS, and forced rollback tests pass.

Next mechanism step: audit sync consumers that rely on `Since(version)` after batch writes, ensuring multi-entry transaction versions are observed in deterministic order and no FTS-only changes leak into sync semantics.

## 2026-05-24 Review / Fix / Optimize Pass 13

Fixed: store-level batch upsert now preserves caller input order when assigning SQLite versions. The implementation records first-seen entry IDs before deduplication, so governed batches like `review record -> source transition` no longer depend on Go map iteration order.

Verified: added SQLite regression coverage that upserts a review and source transition, calls `Since(0)`, and checks the observed version order matches the governed batch order.

Next mechanism step: audit `syncOnce` as a consumer of `Since(version)` for mixed modified/deleted windows. Batch write order is now deterministic for modified entries; delete interleaving and watermark behavior still deserve an explicit sync-level regression test.

## 2026-05-24 Review / Fix / Optimize Pass 14

Fixed: `syncOnce` now refreshes derived indexes after a sync merge/delete cycle. Remote changes no longer leave the project index or theme layer stale, and remote deletes also resynchronize graph relationship fields.

Verified: added a cross-instance SQLite sync regression where a remote task-artifact creates a project record, then a remote delete removes it from the receiving store's project index.

Next mechanism step: add a mixed modified/deleted sync-window test that checks watermark behavior when a delete has the highest version in the window.

## 2026-05-24 Review / Fix / Optimize Pass 15

Verified: added a mixed sync-window regression where one remote entry is modified and another is deleted before the receiver polls. The deleted row owns the highest SQLite version, and the receiver must still advance `sync.lastVersion` to backend `MaxVersion()`.

Verified: the same test checks the modified entry is merged and the deleted entry is absent after one `syncOnce`, so `Since(version)` split modified/deleted result sets do not cause a watermark gap.

Next mechanism step: review store-level batch update index refresh for graph/link parity. Batch writes currently update BM25/vector/entity/semantic/project/theme, but relationship-field synchronization should be checked for multi-entry graph-affecting transitions.

## 2026-05-24 Review / Fix / Optimize Pass 16

Fixed: store-level batch update now preserves `RelatedEdges` when updating existing entries. Previously `RelatedIDs` survived but typed edge metadata could be dropped on the batch path.

Fixed: batch upsert/update now rebuilds the legacy memory graph after applying the batch, so graph expansion sees relationship-field changes immediately instead of waiting for a full store reload.

Verified: added SQLite-backed coverage that batch-updates an entry with a typed related edge, confirms the live graph contains the edge, and confirms the backend persists `RelatedEdges`.

Next mechanism step: scan remaining non-batch memory writers that mutate `RelatedIDs` directly, especially profile/project consolidators, and route them through the same derived-index + persistence discipline where needed.

## 2026-05-24 Review / Fix / Optimize Pass 17

Fixed: generated-memory upserts now carry typed `RelatedEdges` through `UpsertByTagsOptions`, duplicate repair, and existing-entry update paths. The old path could update `RelatedIDs` while silently dropping edge metadata.

Fixed: `updateEntryFromUpsert` now rebuilds the legacy memory graph after relationship metadata changes, matching the batch write path's derived-index discipline.

Verified: added SQLite-backed coverage for `UpsertEntryByTags` update with a typed edge; the live graph and backend row both retain the edge.

Next mechanism step: convert existing profile consolidation update to a store-level write primitive instead of direct mutation plus dirty flush, so profile updates get the same backend-aware persistence contract.

## 2026-05-24 Review / Fix / Optimize Pass 18

Fixed: existing profile consolidation updates now route through `Store.UpdateEntriesByID` instead of mutating store entries directly and relying on dirty flush. SQLite-backed profile updates get immediate transactional persistence and version snapshots through the same batch primitive as governed review writes.

Fixed: the store-level batch updater now carries full temporal and lifecycle metadata for existing entries, including `Level`, `Interval`, `ParentID`, `ChildIDs`, validity timestamps, stability metadata, access count, strength, status, pins, entities, and related edges.

Fixed: batch updates now rebuild the temporal tree as well as graph/project/theme indexes, so profile level changes are visible immediately after the write.

Verified: added SQLite-backed coverage for profile update persistence, version snapshot persistence, and evidence/related link persistence. Existing profile TMT and evidence-boundary tests pass.

Next mechanism step: inspect `Consolidator.persistTreeLinks`, which still mutates `ParentID`/`ChildIDs` directly; migrate tree-link persistence onto the same batch primitive.

## 2026-05-24 Review / Fix / Optimize Pass 19

Fixed: `Consolidator.persistTreeLinks` now stages parent and child entry updates and writes them through `Store.UpdateEntriesByID`. TMT parent/child link persistence no longer uses direct store mutation plus dirty flush.

Fixed: consolidation now logs tree-link persistence failures from the batch primitive instead of silently assuming dirty async persistence will recover them.

Verified: added SQLite-backed consolidation coverage that builds a session parent from two segment children, then verifies the backend row for the parent contains `ChildIDs` and both children contain `ParentID`.

Next mechanism step: scan remaining direct store mutations in maintenance/backfill paths (`discoverMissingLinks`, profile/consolidator side effects) and decide which need transactional backend persistence versus lazy JSON-only maintenance semantics.

## 2026-05-24 Review / Fix / Optimize Pass 20

Fixed: `discoverMissingLinks` now persists graph-link changes through `Store.UpdateEntriesByID` after synchronizing `RelatedIDs`/`RelatedEdges` from the in-memory graph. Dream-cycle link discovery no longer relies on dirty async flush for SQLite-backed stores.

Fixed: link persistence failures are logged from the maintenance pass, making backend rejection visible instead of silently leaving graph-only links in memory.

Verified: added SQLite-backed coverage that seeds two unlinked entries, runs `discoverMissingLinks`, and verifies the discovered related IDs are present in backend rows.

Next mechanism step: inspect stale/hash/tag backfill paths. They still perform direct mutations; choose whether to migrate each to batch persistence or explicitly classify them as JSON-only/lazy maintenance.

## 2026-05-24 Review / Fix / Optimize Pass 21

Fixed: stale detection, stale clearing, content-hash backfill, and tag backfill now stage updated entries and persist through `Store.UpdateEntriesByID`. Dream-cycle maintenance no longer mutates entries directly and relies on dirty async flush for SQLite-backed stores.

Fixed: batch updates now preserve the desired `Stale` value instead of always clearing it, so stale detection can use the shared batch primitive without losing its state transition.

Verified: added SQLite-backed coverage for stale flag persistence, content hash backfill persistence, and tag backfill persistence. Existing graph-link and batch persistence tests pass.

Next mechanism step: inspect remaining direct mutation paths outside dream maintenance, especially candidate governance, semantic dedup merge, compact-form/embedding maintenance, and online extractor targeted updates.

## 2026-05-24 Review / Fix / Optimize Pass 22

Fixed: memory candidate promotion and rejection now stage updated entries and persist through `Store.UpdateEntriesByID`. Accept/reject governance transitions no longer mutate store entries directly or depend on dirty async flush for SQLite-backed stores.

Fixed: candidate consolidation now processes queue snapshots and only uses the legacy locked path for duplicate merge/delete, keeping non-destructive state transitions on the batch primitive.

Verified: added SQLite-backed coverage for one promoted candidate and one rejected candidate, checking persisted active status/tag cleanup/source type and persisted stale dormant rejection state.

Remaining mechanism gap: duplicate candidate merge still updates one entry and deletes another through separate backend operations. A batch delete/update primitive is the clean endpoint for that path.

Next mechanism step: inspect semantic dedup merge, which has the same update-one/delete-one pattern and should share the eventual batch update+delete primitive.

## 2026-05-24 Review / Fix / Optimize Pass 23

Fixed: added a store/backend `UpdateEntriesAndDeleteIDs` mutation primitive for merge flows that must update kept memories and retire duplicate memories as one logical write. SQLite now performs the updates, soft deletes, version increments, and FTS cleanup inside one transaction.

Fixed: candidate duplicate consolidation now uses the shared update+delete batch primitive. The active memory absorbs candidate tags while the dormant candidate is retired through the same governed mutation instead of split update/delete operations.

Fixed: semantic dedup LLM merge now uses the same primitive. The kept entry receives merged content/tags/entities, edges to the deleted entry are removed, and the duplicate new entry is retired atomically.

Verified: added SQLite-backed coverage for candidate duplicate merge and semantic dedup merge. Both tests confirm `LoadAll()` sees only the kept memory and `Since(0)` exposes the retired duplicate in the deleted stream. Added forced-failure coverage proving an update+delete batch rolls back the kept-entry update, duplicate delete, and version increments together.

Remaining mechanism gap: compact-form/embedding maintenance and online extractor targeted updates still need review for direct mutation paths. Next pass should either migrate them onto store-level primitives or document why they are intentionally lazy/local.

## 2026-05-24 Review / Fix / Optimize Pass 24

Fixed: host-side experience draft review creation no longer constructs category-specific `memory.Entry` literals. Added `memory.NewProjectKnowledgeEntry` so governed multi-entry batches can build project-knowledge audit entries through a corelib helper, then persist them with `UpsertEntriesByID` when a source trace transition must commit with the review record.

Fixed: `ProjectKnowledgeUpsertOptions` now carries `RelatedEdges`, and `UpsertProjectKnowledge` passes them through both ID-addressed and tag-addressed paths. Project-knowledge helper construction now owns category, scope, source, evidence, related IDs, related edges, derived kind, and boundary defaults instead of leaving host adapters to repeat them.

Fixed: host experience trace snapshots now sort by `CreatedAt` before `UpdatedAt`. This keeps trace chronology stable when persistence, repair, or maintenance touches `UpdatedAt`, and avoids host snapshot ordering being distorted by incidental save order.

Verified: `TestProductionMemoryWritesUseCorelibHelpers`, host experience snapshot coverage, project-knowledge related-edge coverage, and targeted GUI draft review tests pass. Full `go test ./corelib/memory -count=1` now passes.

Remaining mechanism gap: compact-form/embedding maintenance and online extractor targeted updates still need review for direct mutation paths. Next pass should audit those writers against the same store-level primitive rule.

## 2026-05-24 Review / Fix / Optimize Pass 25

Fixed: compact-form backfill now stages updated entries and persists through `Store.UpdateEntriesByID`. Compression maintenance no longer mutates `CompactForm` directly or depends on dirty async flush for SQLite-backed stores.

Fixed: embedding backfill paths now stage embedding updates and route them through the batch updater. This covers both `SetEmbedder` background backfill and theme-maintenance backfill, so vector fields persist through the same backend-aware contract as other maintenance updates.

Fixed: `UpdateEntriesByID` now preserves and persists `CompactForm` and `Embedding` as first-class mutable fields. Content-changing updates still invalidate stale compact text unless the caller provides a new compact form, while same-content maintenance can safely fill compact/vector fields.

Fixed: online extractor UPDATE/DELETE execution no longer mutates target entries directly after classification. UPDATE now builds one merged target entry with content, tags, entities, and temporal metadata, then persists it through `UpdateEntriesByID`; DELETE/supersede now stages the lifecycle transition through the same primitive before adding the contradicting fact.

Verified: added SQLite coverage for compact-form + embedding batch persistence and online extractor metadata update persistence. Full `go test ./corelib/memory -count=1` passes.

Remaining mechanism gap: pin/unpin, conflict detector invalidation, and a few compressor/archive lifecycle paths still use direct mutation. Next pass should migrate lifecycle flag transitions to a small store-level mutation helper instead of scattered lock+persist code.

## 2026-05-24 Review / Fix / Optimize Pass 26

Fixed: added `Store.SupersedeEntryByID` as the shared lifecycle transition primitive for invalidating facts. It stages `StatusSuperseded`, `Stale`, and `InvalidAt` through the same batch updater used by governed memory mutations, then rebuilds derived indexes from authoritative entries.

Fixed: pin/unpin now routes through a metadata-preserving batch update path instead of hand-mutating `Pinned` under lock. This preserves exact existing content, including legacy whitespace-only or whitespace-padded entries, while still persisting lifecycle metadata through backend-aware batch writes.

Fixed: conflict detector supersede and derived-memory surgery now use `SupersedeEntryByID`; they no longer call `supersedeEntryLocked` plus manual dirty signaling. `UpdateEntriesByID` now rebuilds all derived indexes after batch application, so superseded entries are removed from vector/entity/graph recall consistently.

Verified: added SQLite lifecycle coverage for pin and supersede persistence. Targeted pin property tests, supersede index tests, and full `go test ./corelib/memory -count=1` pass.

Remaining mechanism gap: compressor GC/archive paths still perform larger structural lifecycle changes with direct in-memory mutation. Next pass should separate destructive/structural GC from metadata transitions and migrate the safe transition subset onto batch primitives.

## 2026-05-24 Review / Fix / Optimize Pass 27

Fixed: compressor exact/substring dedup now computes duplicate losers from a snapshot, then retires duplicate IDs through `Store.UpdateEntriesAndDeleteIDs`. Duplicate removal no longer rewrites `store.entries` directly or relies on dirty flush; SQLite emits normal delete tombstones through the same sync stream.

Fixed: LLM-assisted merge in compression now stages the survivor update plus loser deletes through the same update+delete batch primitive. Survivor content/tag consolidation and duplicate retirement commit as one logical mutation.

Fixed: pipeline dormant decay now stages lifecycle metadata updates and persists them through the metadata-preserving batch path. The decay step no longer mutates statuses/strength directly under the store lock.

Verified: added SQLite coverage for compressor duplicate deletion persistence and sync tombstones. Targeted compressor/pipeline lifecycle tests pass, and full `go test ./corelib/memory -count=1` passes.

Remaining mechanism gap: full GC archive/revive still performs structural remove/append around the archive store. Next pass should introduce a dedicated archive/revive store primitive that can coordinate active-store tombstones with archive add/remove semantics.

## 2026-05-24 Review / Fix / Optimize Pass 28

Fixed: compressor full GC no longer rewrites `store.entries` directly. It now selects archive candidates from a snapshot, then calls `Store.ArchiveActiveEntries`, which tombstones active IDs through `UpdateEntriesAndDeleteIDs` before adding the entries to cold archive storage.

Fixed: archive revival now goes through `Store.ReviveArchivedEntries`. The primitive removes entries from archive storage, normalizes revive metadata, and restores them through a metadata-preserving ID-addressed batch path, so revived memories rebuild active indexes, preserve exact archived content, and persist through the same backend-aware contract as governed writes.

Fixed: `RestoreFromArchive` now reuses the same revive primitive instead of manually appending entries and rebuilding selected indexes. Manual archive restoration and compressor GC revival now share one active/archive transition path.

Verified: added SQLite-backed GC coverage that archives low-priority active entries, revives a relevant archived entry, verifies active SQLite rows exclude tombstoned archive candidates, verifies revived entries are present, and verifies archived IDs appear in the SQLite sync delete stream. Targeted GC/lifecycle tests pass.

Remaining mechanism gap: capacity eviction in `Store.evictLRU` still performs direct active-list surgery and per-entry backend deletes before archive add. Next pass should migrate capacity eviction onto the same archive-active primitive or a capacity-specific variant that can run without lock reentrancy.

## 2026-05-24 Review / Fix / Optimize Pass 29

Fixed: capacity eviction no longer emits one backend delete per evicted memory. `Store.evictLRU` now computes evicted IDs first, writes all active tombstones through `BatchMutationStorageBackend.UpdateEntriesAndDeleteIDs`, and only then rewrites the active slice and archive list. If backend tombstoning fails, active memory is left untouched.

Fixed: insert-time capacity enforcement now avoids re-indexing a newly saved entry after eviction if that same entry was selected as the LRU victim. This keeps BM25/vector/entity/semantic indexes aligned with the authoritative active slice after capacity trimming.

Verified: added SQLite-backed capacity eviction coverage that saves past `maxItems`, verifies the evicted entry is archived, verifies `LoadAll()` excludes it, and verifies `Since(0)` exposes the eviction tombstone. Targeted GC/capacity tests pass.

Remaining mechanism gap: active/archive operations are now routed through shared backend-aware paths, but archive JSON add/remove is still not atomic with SQLite tombstone/upsert. Next design step is an archive transaction envelope or outbox so cross-store failures can be repaired deterministically.

## 2026-05-24 Review / Fix / Optimize Pass 30

Fixed: archive writes are now ID-idempotent. `ArchiveStore.Add` replaces an existing archived entry with the same ID instead of appending duplicates, so retry/rollback paths cannot accumulate repeated cold copies of the same memory.

Fixed: added `ArchiveStore.RemoveIDs` as a batch archive removal primitive. GC/archive revival now removes all requested archive IDs in one archive mutation, preserves requested order, skips missing IDs, and then restores the removed entries through the metadata-preserving active-store batch path.

Fixed: legacy `ArchiveStore.Remove` now delegates to `RemoveIDs`, so single-entry restore and batch revive share the same archive mutation semantics.

Verified: added archive unit coverage for idempotent add and ordered batch removal, plus reran restore/GC/capacity archive tests. Targeted tests pass.

Remaining mechanism gap: archive mutations are idempotent and batched, but not durably journaled with active SQLite mutations. Next pass should add a small archive transition journal/outbox persisted beside `archive.json` so interrupted active/archive transitions can be replayed or repaired on startup.

## 2026-05-24 Review / Fix / Optimize Pass 31

Fixed: active-to-archive transitions now write cold storage first through `ArchiveStore.AddDurable`, then tombstone active rows. This changes the failure mode from possible memory loss to an idempotent active/archive duplicate that later maintenance can repair.

Fixed: archive-to-active revival now reads archive entries without removing them, restores active entries through the metadata-preserving batch upsert, then removes archive IDs through `RemoveIDsDurable`. If durable archive removal fails after restore, the active memory remains available and the archive copy is harmless/idempotent.

Fixed: capacity eviction uses the same durable archive-first ordering before SQLite tombstones. Backend tombstone failure leaves active memory intact, with an already-durable archive copy rather than a lost entry.

Verified: extended archive coverage so `AddDurable` and `RemoveIDsDurable` are visible after immediate reload, and reran restore/GC/capacity transition tests. Targeted tests pass.

Remaining mechanism gap: durable archive-first ordering removes loss windows, but duplicate repair remains implicit. Next pass should add startup reconciliation that detects active+archive duplicate IDs and removes archive copies when active rows are live.

## 2026-05-24 Review / Fix / Optimize Pass 32

Fixed: startup now reconciles active/archive duplicate IDs. `Store.ReconcileArchiveDuplicates` snapshots live active IDs and removes matching archive copies through durable batch archive removal, closing the expected duplicate left by archive-first crash recovery.

Fixed: both JSON and SQLite store construction paths run the reconciliation after archive initialization. SQLite stores loaded through `NewStoreWithMode` now get the same archive duplicate cleanup as legacy JSON stores.

Verified: added JSON and SQLite startup coverage that seeds a live active memory plus a duplicate archive copy, reopens the store, and verifies the archive duplicate is durably removed. Targeted archive reconciliation tests pass.

Remaining mechanism gap: reconciliation now handles active/archive duplicates, but there is still no explicit transition journal. Next pass should decide whether durable archive-first + startup duplicate repair is sufficient, or add a compact journal only for observability and failure diagnostics.

## 2026-05-24 Review / Fix / Optimize Pass 33

Fixed: added a compact archive transition audit stream beside `archive.json`. Durable archive add/remove operations now append JSONL events with timestamp, action, IDs, and count after the archive file is flushed.

Fixed: archive transition diagnostics are intentionally non-blocking. If audit append fails, the durable archive mutation remains authoritative and the failure is logged; memory availability does not depend on observability storage.

Verified: extended archive durable-operation coverage to reload the archive immediately after add/remove and verify the transition audit records both durable add and durable remove events. Targeted archive reconciliation and GC transition tests pass.

Remaining mechanism gap: archive transition safety is now archive-first + durable flush + startup duplicate reconciliation + audit trail. Next review should move back up-stack: inspect memory write governance paths for any remaining direct writes outside store-level primitives.

## 2026-05-24 Review / Fix / Optimize Pass 34

Fixed: memory `ExperienceProvider.UpdateUtility` no longer mutates `store.entries` directly under lock or calls `persistUpdatedEntryLocked`. It now snapshots the target entry, adjusts utility metadata, and persists through the metadata-preserving batch update path.

Fixed: utility feedback now participates in the same backend-aware mutation contract as lifecycle and maintenance updates. SQLite-backed stores get transactional batch persistence and derived-index rebuilds instead of one-off direct row updates.

Verified: added SQLite-backed coverage for helpful/success and harmful utility updates, checking persisted access/strength changes and unchanged stored content. Targeted experience-provider and archive audit tests pass.

Remaining mechanism gap: continue the upper-stack sweep for direct mutations, especially `Upsert*` repair paths and subconscious/stability maintenance paths that still write store internals directly.

## 2026-05-24 Review / Fix / Optimize Pass 35

Fixed: generated-memory upsert repair no longer mutates target entries directly under the store lock. `updateEntryFromUpsert` now validates duplicates from a snapshot, builds the desired full entry, and persists through `Store.UpdateEntriesByID`.

Fixed: upsert content changes now use the shared batch updater for version history, content hash refresh, compact-form invalidation, backend persistence, and derived-index rebuilds. Generated metadata repair is no longer a bespoke write path.

Verified: added SQLite-backed upsert coverage that updates generated content and metadata, verifies backend persistence, verifies version history, and verifies related-edge metadata. Targeted upsert and experience-provider tests pass.

Remaining mechanism gap: continue with `subconscious.go` helper methods (`MarkVolatile`, `Remove`) and any remaining direct maintenance writers.

## 2026-05-24 Review / Fix / Optimize Pass 36

Fixed: subconscious volatility marking no longer mutates `store.entries` directly. `MarkVolatile` now snapshots the entry, updates stale/stability metadata, and persists through the metadata-preserving batch updater.

Fixed: subconscious fragment removal no longer slices active memory directly. `Remove` now routes through `UpdateEntriesAndDeleteIDs`, producing normal backend tombstones and derived-index rebuilds.

Verified: added SQLite-backed coverage for `MarkVolatile` persistence and `Remove` tombstone propagation. Targeted subconscious/upsert tests pass.

Remaining mechanism gap: continue sweeping remaining direct write helpers in `store.go` hot paths (`Save` duplicate touch and `Update`) so even legacy/manual writes share the batch primitive shape.

## 2026-05-24 Review / Fix / Optimize Pass 37

Fixed: `Store.SaveWithContext` duplicate paths no longer mutate matching entries directly. Exact-content dedup now snapshots the target and persists tag/entity/access metadata through the metadata-preserving batch updater; substring dedup snapshots the target and persists content/embedding/entity changes through `UpdateEntriesByID`.

Fixed: legacy `Store.Update` no longer edits `store.entries` or calls `persistUpdatedEntryLocked` directly. It now validates duplicates from a snapshot, builds the updated entry, and persists through the shared batch updater so version history and derived indexes follow the same mechanism as generated writes.

Verified: added SQLite-backed coverage for Save dedup metadata persistence and manual Update persistence/version history. Targeted save/update/dedup tests pass.

Remaining mechanism gap: direct mutation surface in core memory is much smaller. Next pass should run a fresh direct-write audit and classify remaining hits as test-only, backend internals, or real production debt.

## 2026-05-24 Review / Fix / Optimize Pass 38

Reviewed: reran direct-write audit across `corelib/memory`. Remaining direct mutations are now mostly backend/archive internals, constructor/load/flush code, sync merge application, tests, and intentionally low-level helpers (`syncGraphLinksLocked`, `supersedeEntryLocked`).

Fixed: legacy `Store.Delete` no longer performs its own slice removal, per-index removal, and single backend delete. It now delegates to `UpdateEntriesAndDeleteIDs(nil, []string{id})`, so delete semantics share the same tombstone, FTS cleanup, and derived-index rebuild path as merge/GC/subconscious removals.

Verified: targeted SQLite save/update/delete, sync-delete, project-index delete, and subconscious remove tests pass.

Remaining mechanism gap: remaining production direct mutations are primarily low-level internal reconstruction/sync routines. Next pass should classify `syncGraphLinksLocked` and `supersedeEntryLocked` with comments/tests, or split them into explicit low-level/internal-only primitives so future audits do not confuse them with write-path debt.

## 2026-05-24 Review / Fix / Optimize Pass 39

Fixed: delete batches now clean surviving entries' persisted graph links before the backend mutation commits. `UpdateEntriesAndDeleteIDs` strips deleted IDs from `RelatedIDs` and `RelatedEdges` on caller-updated entries and otherwise-unchanged neighbors, then sends those survivor updates together with tombstones through the same atomic SQLite batch.

Fixed: legacy `Store.Delete` can stay on the shared mutation primitive without leaving stale graph edges in JSON or SQLite persistence. Relationship cleanup, active-row deletion, FTS cleanup, and sync tombstones now move as one mechanism instead of a post-delete repair pass.

Verified: targeted delete/sync/subconscious tests pass, and full `go test ./corelib/memory -count=1` passes.

Remaining mechanism gap: direct-write audit can now focus on naming/guarding intentionally low-level reconstruction helpers (`syncGraphLinksLocked`, `supersedeEntryLocked`) and sync-merge internals, instead of broad production write-path migration.

## 2026-05-24 Review / Fix / Optimize Pass 40

Fixed: removed the legacy `supersedeEntryLocked` direct mutation helper. Supersede tests now exercise `SupersedeEntryByID`, the governed batch path that persists lifecycle state, rebuilds derived indexes, and keeps SQLite/JSON behavior aligned.

Fixed: deleted unused `persistUpdatedEntryLocked` and `persistDeletedEntryLocked` helpers so new production writes cannot accidentally route around store-level batch primitives.

Clarified: `syncGraphLinksLocked` is now documented as an internal reconstruction helper only. Public write paths must stage relationship changes through `UpdateEntriesByID` or `UpdateEntriesAndDeleteIDs` to keep persistence atomic.

Verified: targeted supersede/delete graph tests pass, and full `go test ./corelib/memory -count=1` passes.

Remaining mechanism gap: next audit should inspect sync-merge internals (`sync.go`) and decide whether remote apply/delete should become explicit store-level primitives or remain isolated reconciliation code with stronger comments/tests.

## 2026-05-24 Review / Fix / Optimize Pass 41

Fixed: sync merge/delete now uses one explicit reconciliation primitive, `applyRemoteSyncBatchLocked`, instead of scattered add/update/remove index helpers. Remote SQLite rows remain authoritative; sync applies the whole window to in-memory entries and rebuilds derived indexes once as a local snapshot.

Fixed: removed sync-only index helpers (`addToIndicesLocked`, `updateIndicesForEntryLocked`, `removeFromEntriesAndIndicesLocked`) so remote reconciliation cannot drift from the store-wide rebuild contract.

Fixed: remote delete sync now cleans stale in-memory graph relations via a full graph rebuild plus `syncGraphLinksLocked`. Added coverage for a persisted relation to a remotely deleted entry.

Verified: targeted sync/cross-instance tests pass, and full `go test ./corelib/memory -count=1` passes.

Remaining mechanism gap: active memory now has clear mutation boundaries. Next pass should run a fresh direct-write audit and focus only on constructor/load/flush internals, archive internals, and tests.

## 2026-05-24 Review / Fix / Optimize Pass 42

Reviewed: fresh direct-write audit found one remaining production hot-path write: `TouchAccess` still incremented access metadata directly and depended on delayed file dirtying.

Fixed: `TouchAccess` now snapshots touched entries, applies access/strength boosts to the snapshots, and persists through the metadata-preserving batch updater. Recall touches, session checkpoint touches, and generated upsert duplicate touches now share the same SQLite/JSON mutation mechanism.

Verified: added SQLite coverage for touch persistence through the batch path. Targeted touch/lifecycle/recall tests pass, and full `go test ./corelib/memory -count=1` passes.

Remaining mechanism gap: production direct writes left by audit are now mostly store construction/load/flush, archive internals, backup restore, sync reconciliation, and tests. Next pass should classify backup restore semantics against SQLite mode so restore cannot silently update only JSON while a backend is active.

## 2026-05-24 Review / Fix / Optimize Pass 43

Fixed: backup restore now routes through `Store.RestoreEntriesSnapshot` instead of writing `memories.json` and replacing `store.entries` directly. Restore has one store-level contract for JSON, partitioned JSON, and SQLite-backed stores.

Fixed: SQLite restore now commits restored entries and tombstones for entries absent from the snapshot through one `UpdateEntriesAndDeleteIDs` backend mutation before changing memory. The sync watermark is advanced after restore so the restoring instance does not replay its own snapshot window.

Fixed: JSON/partition restore now marks the store dirty and flushes through the normal store flush path, so partitioned stores no longer risk restoring only the stale legacy JSON file.

Verified: added SQLite restore coverage that replaces an old backend row with the restored snapshot and checks sync watermark advancement. Targeted backup/restore tests pass, and full `go test ./corelib/memory -count=1` passes.

Remaining mechanism gap: remaining direct writes are now constructor/load/flush internals, archive internals, explicit sync reconciliation, and tests. Next pass should either add a write-path policy test for these allowed buckets or document them as accepted internal boundaries.

## 2026-05-24 Review / Fix / Optimize Pass 44

Fixed: added a write-path policy test that scans production `corelib/memory` code for direct `entries` writes and allows them only inside documented internal boundaries: archive storage internals, store batch/snapshot/load/eviction primitives, graph-link reconstruction, and sync reconciliation.

Fixed: the policy test rejects future drift such as new direct `s.entries[...] = ...`, `s.entries = ...`, or append writes outside those buckets, forcing new production mutations back through store-level primitives.

Verified: targeted policy/restore tests pass, and full `go test ./corelib/memory -count=1` passes.

Remaining mechanism gap: policy now prevents broad write-path regression. Next pass should review the allowed bucket list itself and shrink or split any bucket that can be expressed as a narrower helper.

## 2026-05-24 Review / Fix / Optimize Pass 45

Fixed: shrank the direct-write policy surface by introducing `replaceEntriesAndRebuildLocked` as the single Store helper allowed to replace the active entry slice and rebuild derived indexes.

Fixed: `SetEntries`, backup snapshot restore, eviction, and load/corrupt-load recovery now route through that helper instead of each owning `s.entries = ...` plus rebuild mechanics. The policy allowlist no longer permits direct writes in `SetEntries`, `RestoreEntriesSnapshot`, `evictLRU`, or `load`.

Verified: targeted policy/restore/sync tests pass, and full `go test ./corelib/memory -count=1` passes.

Remaining mechanism gap: remaining allowed direct writes are now narrower: batch update/upsert, insert, graph-link reconstruction, the one replacement helper, sync reconciliation, and archive internals. Next pass should review whether sync reconciliation can use the replacement helper for delete windows too.

## 2026-05-24 Review / Fix / Optimize Pass 46

Fixed: sync reconciliation no longer writes `s.entries` directly. `applyRemoteSyncBatchLocked` now stages a copied entry slice for remote merges, additions, and deletes, then commits the local snapshot through `replaceEntriesAndRebuildLocked` once.

Fixed: removed `sync.go` from the direct-write policy allowlist. Remote sync remains an explicit reconciliation primitive, but slice replacement is now owned by the same narrow Store helper used by load, restore, eviction, and tests.

Verified: targeted sync/policy tests pass, and full `go test ./corelib/memory -count=1` passes.

Remaining mechanism gap: direct Store write allowance is now down to batch update/upsert, insert, graph-link reconstruction, and the replacement helper. Archive internals remain separate because archive owns its own cold-store slice.
