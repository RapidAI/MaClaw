# Maclaw 记忆管理能力整理与收敛设计

日期：2026-05-19
范围：`corelib/memory`、GUI/TUI/agent 对 `corelib/memory` 的接入。`iWorkerCenter` 已复用 corelib，本设计暂不展开它自己的 `shared_memories` 管理表、经验提取接口和后台管理能力。

## 1. 背景

Maclaw 的记忆能力已经从单一长期记忆库演进为一套多索引、多召回、多维护管线的系统：写入侧有脱敏、注入扫描、hash/substring 去重、语义去重、候选治理、在线抽取；召回侧有 BM25、向量、图扩展、实体索引、主题层、LightMem 规划、Adaptive 主题扩展；维护侧有衰减、压缩、合成、TMT consolidation、profile consolidation、theme rebuild。

当前主要问题不是能力不足，而是同类能力层数过多、入口命名分散、职责边界不够清晰。继续堆能力会让后续调试变难，也会提高重复写入、重复召回、重复 LLM 调用的概率。

本设计目标是整理现有能力，明确主路径、降级路径和实验能力，减少重复概念，并把长期记忆从“持续压缩文本”转成“证据优先、延迟抽象、带边界召回”的系统。

## 2. 现状能力地图

### 2.1 核心模型

核心记录为 `memory.Entry`，定义在 `corelib/memory/types.go`。它已经包含：

- 内容与分类：`Content`、`Title`、`Category`、`Tags`。
- 生命周期：`Status`、`Strength`、`Pinned`、`Stale`、`AccessCount`。
- 召回增强：`Embedding`、`CompactForm`、`RelatedIDs`、`RelatedEdges`。
- 层级记忆：`Level`、`Interval`、`ParentID`、`ChildIDs`。
- 来源与治理：`SourceURL`、`SourceType`、`ContentHash`、`Versions`。
- 图谱与时态：`ValidAt`、`InvalidAt`、`Entities`、`Stability`。
- 多用户隔离：`OwnerID`、`Version`。

分类体系同时存在 legacy category 与 Claude-style category：

- legacy：`self_identity`、`user_fact`、`preference`、`project_knowledge`、`instruction`、`conversation_summary`、`session_checkpoint`、`task_artifact`、`profile`。
- Claude-style：`user`、`feedback`、`project`、`reference`。

现有 `MapToCanonical` 已经把 Claude-style category 映射回内部 canonical category，因此分类重复是可控的，建议保留为 API 兼容层。

### 2.2 Store 与索引

`memory.Store` 定义在 `corelib/memory/store.go`，承担长期记忆的唯一聚合根职责。它维护：

- 基础索引：BM25、vector index、memory graph。
- 层级索引：Temporal Memory Tree。
- 项目/实体/语义图：ProjectIndex、EntityIndex、SemanticGraph。
- 主题层：ThemeManager。
- 写入治理：OnlineExtractor、pending semantic dedup、LLM dedup。
- 存储后端：JSON backend、SQLite backend、跨实例 sync。

GUI 当前优先用 SQLite store factory；TUI 入口也应统一通过 store factory 打开同一个 `memoryDir`，避免 JSON/SQLite 选择逻辑分叉。
统一边界：GUI、TUI、MaClawSrv/agentservice 的长期记忆入口都必须直接接入 `corelib/memory.StoreFactory` 和 `corelib/memory.Store`。历史的 `agent_memory.json` 只作为 legacy seed 输入，不再作为新的长期记忆后端。`ConversationMemory` 仅保存活跃会话/未完成任务等 session-state；`KnowledgeStore` 仅保存带引用的文档、卡片、事实检索资料；`ToolMemoryStore` 仅保存工具执行提示。它们可以给 agent 提供上下文，但不承担长期记忆、召回审计或派生手术职责。

### 2.3 写入路径

现有写入主路径：

1. 工具/代码调用 `Save`、`SaveForUser` 或 `SaveWithContext`。
2. 写入前执行 prompt injection 扫描与敏感信息脱敏。
3. 通过 content hash 和完全内容比较做确定性幂等。
4. 通过 substring duplicate 处理近似重复。
5. 如有 embedder，则生成 embedding，并加入异步 semantic dedup 候选队列。
6. 持久化并更新 BM25、vector、graph、project/entity/semantic graph、theme layer。

另有治理写入入口 `SaveGovernedWithContext`：

- 通过 `OnlineExtractor` 执行 ADD/UPDATE/DELETE/NOOP 四操作。
- 能利用相似记忆减少重复写入。
- 是后续事实抽取和在线记忆更新的主路径。

### 2.4 维护路径

`Pipeline.RunOnce` 是后台维护聚合入口，主要包括：

- 衰减：`DecayMemories`。
- 压缩：`Compressor.Compress`。
- 候选治理：`consolidateCandidates`。
- 异步语义去重：`processSemanticDedupPending`。
- TMT consolidation：`TMTConsolidator.Consolidate`。
- profile consolidation：`ProfileConsolidator.ConsolidateForOwner`。
- theme rebuild：`ThemeManager.Rebuild`。
- synthesis：`Synthesizer.Run`。

目前风险是：压缩、合成、画像、主题都可能被误解为“更高级的事实来源”。整理后要明确它们是派生视图，不能吞掉原始 evidence。

### 2.5 召回路径

主要召回模式：

- `dynamic`：主召回。融合 BM25、向量、实体标签、图扩展、项目 owner 过滤、recency/importance/relevance 等信号。
- `hybrid` / `recall`：经 `SearchByMode(SearchHybrid)` 进入另一套 hybrid search。
- `lightmem`：基于意图信号构建多路召回计划，再调用 `RecallDynamic`。
- `adaptive`：先调用 `RecallDynamic` 做 seed，再通过 ThemeManager 做主题扩展和多样性选择。
- `auto`：简单查询走 dynamic，复杂查询走 adaptive。

收敛建议：对外默认推荐 `auto`，内部保留其它模式用于调试、评估和低预算场景。

## 3. 重复、冗余与冲突点

### 3.1 去重层过多

现有去重层包括：

- deterministic hash / exact duplicate。
- substring duplicate。
- pending semantic dedup。
- `Compressor` 内部 dedup/merge。
- candidate consolidation。
- OnlineExtractor 基于相似记忆做 ADD/UPDATE/DELETE/NOOP。

整理口径：

- realtime save 只做安全、幂等和低风险重复判断。
- semantic dedup 是异步整理。
- compressor 是维护兜底，不应替代 OnlineExtractor 的事实治理。
- candidate consolidation 是候选队列治理，不等价于普通 dedup。

### 3.2 抽取路径并行

现有抽取/归档相关能力包括：

- `OnlineExtractor`：当前最完整的在线事实治理路径。
- `KnowledgeExtractor`：从文本中抽知识，适合作为 fallback 或离线导入。
- `ConversationArchiver`：对会话做摘要归档，更像 legacy/兜底上下文。

整理口径：

- OnlineExtractor 是事实写入主路径。
- KnowledgeExtractor / ConversationArchiver 标注为 fallback，避免继续和 OnlineExtractor 平行演进。
- Archiver 产物不应默认参与普通长期记忆注入，除非调用方明确需要摘要上下文。

“fallback”的意思是：这两条能力保留，但只在主路径不可用或特定离线/兼容场景中使用；新功能、新测试、新调优优先落到 OnlineExtractor 和 Store 主链路上。

### 3.3 派生层与证据层混用

当前 summary、compact form、theme、profile、schema 都会影响召回，但它们不是原始事实。若持续由 LLM 重写，可能产生错误抽象、过度泛化和跨任务污染。

整理口径：

- raw episode、明确事实、候选证据是 evidence layer。
- summary、theme、profile、schema 是 derived view。
- derived view 必须引用 evidence IDs/source/version，并带适用边界。
- schema 可被 supersede、降权或删除；原始 evidence 默认保留。

## 4. 指导原则

这组原则来自三点判断：长期记忆首先是证据系统，其次才是压缩系统；情景记录和规则形成必须分层；跨任务经验不能被无边界累积成平滑但错误的通用规则。

### 4.1 论文经验：连续更新的抽象记忆会变坏

论文 `Useful Memories Become Faulty When Continuously Updated by LLMs`（arXiv:2605.12978）验证了一个和 Maclaw 当前整理方向高度相关的风险：有用经验在被 LLM 连续重写成文本抽象后，记忆效用会先升后降，甚至低于无记忆基线。论文把故障归因到 consolidation loop 本身，而不是原始经验质量。

可借鉴结论：

- raw episode 应是 first-class evidence，而不是等待被压缩的废料。
- consolidation 不能在每次交互后强制触发，应由显式 gate 决定是否发生。
- episodic store 和 abstract/schema store 必须架构上分离；保留、删除、抽象应是不同动作。
- 抽象失败主要有三类：misgrouping、overgeneralization/interference、narrow-stream overfit。
- 评估记忆系统时不能只看“有记忆 vs 无记忆”，还要比较 no-memory、episodic-only、abstract-only、episodic+abstract、forced-consolidation。

对 Maclaw 的设计含义：

- 默认策略应是 retain evidence，谨慎 abstract；抽象产物少而精，比持续追加/重写更安全。
- `Pipeline.RunOnce` 中的合成、画像、主题重建应视为 gated consolidation，而不是固定收益步骤。
- 每个 derived schema 都应能被审计和 surgery：如果某条规则降低召回/执行效果，系统应能定位来源、降权、supersede 或移除该 schema，而不是删除原始证据。

### 4.2 Evidence over compression

长期记忆的目标不是尽早把经验压缩成更短文本，而是在保留证据链的前提下，让系统逐步形成可验证、可回退、可解释的抽象。压缩、合成、主题和画像都应被视为 derived view，而不是原始证据的替代品。

设计约束：

- 停止盲目整合：实时写入链路只做安全扫描、显著重复判断、候选治理和明确事实保存。
- 敬畏原始数据：原始交互轨迹、明确事实、候选证据应作为最高级别证据保留；summary、compact form、theme、profile 是派生层。
- 延迟抽象：只有当同类证据形成足够清晰、可隔离的证据群体后，维护管线才进行受控抽象。

### 4.3 Episodic 与 Schema 分层

情景记录和规则形成不能进入同一个压缩/重写循环。情景记录是证据层，描述某次交互、某个任务、某个时间点发生了什么；规则抽象是派生层，描述在多条证据支持下形成的偏好、流程、项目规律或长期画像。

设计约束：

- episodic 记录可以快速写入，但只做轻治理，不急于变成规则。
- schema 只能由维护管线在证据足够时生成，必须引用来源证据，不能吞掉来源记录。
- schema 被新证据推翻时应 supersede、降权或版本化，而不是删除历史情景证据。
- 召回侧可以优先展示 schema 以节省上下文，但需要能按需展开到原始 evidence。

### 4.4 Boundary before cumulative integration

不要把连续任务切换中的所有经验做无边界 cumulative 整合。每次抽象都必须先确认适用前提，避免把相邻但不同任务的经验平滑成错误通用规则。

设计约束：

- 每条 schema 必须保留适用边界，例如 project、task type、workflow、toolchain、time window、owner 或 source scope。
- 跨项目、跨任务类型、跨时间窗口的证据默认不合并，除非存在明确共同前提。
- `ThemeManager`、`ProfileConsolidator`、`Synthesizer` 生成 schema 时必须记录边界；召回时按当前上下文重新筛选。
- 维护管线可以发现跨任务共性，但不能把具体前提抹平；抽象结果应表达“在什么条件下成立”。

一句话：Maclaw 记忆整理应追求更晚、更有证据链、更有适用边界的抽象，而不是更早、更激进、跨任务无边界的压缩。

## 5. 目标架构

### 5.1 分层心智模型

对系统内部，记忆分为三层：

1. 证据层：episodic 原始记录、明确事实、候选证据、source、版本链。它负责保真、追溯和回放。
2. 派生层：summary、compact form、theme、profile、schema。它负责节省上下文、加速召回和形成可解释抽象。
3. 边界层：project、owner、task type、workflow、toolchain、time window、source scope。它负责防止跨任务污染。

对调用方，只暴露三个动作：

1. `save`：写入长期记忆或候选证据。
2. `recall`：默认 `mode=auto`，由系统按当前上下文选择轻量/复杂召回路径，并混合证据层与派生层。
3. `maintain`：后台整理，包括候选治理、去重、压缩视图、合成、主题维护和 schema 边界校验。

### 5.2 写入链路

```text
manual/tool save
  -> SaveWithContext
  -> security scan + redact
  -> deterministic dedup
  -> persist evidence entry or quarantined candidate
  -> enqueue semantic dedup candidate when embedding is available
  -> pipeline later: pending semantic dedup + candidate consolidation

online extraction
  -> ExtractAndIntegrate
  -> find similar memories within owner/project boundary
  -> LLM classifies ADD/UPDATE/DELETE/NOOP
  -> SaveGovernedWithContext / Update / Delete
  -> preserve evidence references for derived updates
```

写入链路默认产出证据，不默认产出规则。明确事实可以直接保存；偏好、画像和流程规则优先进入候选或等待维护管线聚合。

### 5.3 Schema 形成链路

```text
Pipeline.RunOnce
  -> collect candidate evidence groups
  -> check quorum and boundary consistency
  -> run consolidation gate
       -> enough diverse evidence?
       -> clean segmentation?
       -> no conflicting boundary?
       -> expected utility beats episodic-only?
  -> generate/update schema view
  -> attach evidence IDs / source / version chain
  -> mark previous schema superseded when contradicted
  -> record audit trail for memory surgery
```

`Compressor` 只在 exact duplicate、明确 substring duplicate 或维护期合并判断足够明确时改写/合并；复杂语义合并要保留 `Versions`、`RelatedIDs` 或来源证据。`Synthesizer`、`ProfileConsolidator`、`ThemeManager` 产生的是摘要视图、画像视图和主题索引，不应被当成唯一事实来源。

### 5.4 召回链路

```text
memory recall(mode=auto)
  -> classify query complexity and current boundary
  -> simple query: RecallDynamic over evidence + derived views
  -> complex query: RecallAdaptiveHier
       -> seed = RecallDynamic
       -> select themes by seed/facet/boundary
       -> expand theme evidence
       -> diversity and token-budget selection
       -> expose evidence when debug or confidence is low
```

`lightmem` 保留给低预算场景或调试，不作为默认对外推荐路径。召回可以优先返回派生层，但必须保留展开证据的路径；当 schema 的适用边界与当前上下文不匹配时，应降权或排除。

### 5.5 维护链路

```text
Pipeline.RunOnce
  -> decay
  -> compressor
  -> pending semantic dedup
  -> candidate consolidation
  -> synthesis gate
  -> TMT Consolidator
  -> ProfileConsolidator
  -> ThemeManager rebuild
  -> schema boundary audit
```

维护链路负责延迟抽象，而不是实时重写。它可以生成更短、更稳定的派生视图，但不能把 evidence 当成压缩废料。对高风险 schema，维护链路还应支持 audit/surgery：按效果、边界匹配度、证据覆盖率定位坏规则，降权或 supersede 派生记忆。

## 6. 当前实施切片

本轮先落地兼容性最强的一层：不改存储主表、不删除旧能力，只给派生 schema 增加可审计元数据和 gate，并让召回候选过滤读取边界。JSON backend 可直接持久化，SQLite backend 通过 `extra` 字段兼容保存，旧数据不需要迁移。

### 6.1 数据结构

- `Entry.EvidenceIDs`：派生记忆引用的原始证据 entry IDs。
- `Entry.DerivedKind`：标记 `schema:recurring`、`schema:insight`、`profile`、`theme`、`summary` 等派生类型。
- `Entry.Boundary`：记录 `project_path`、`owner_id`、`task_type`、`workflow`、`toolchain`、`source_scope`、`since/until` 等适用边界。
- SQLite backend 继续使用 `extra` JSON 字段保存这些兼容字段，并已覆盖 reload 后的 `EvidenceIDs`、`RelatedIDs`、`DerivedKind`、`Boundary`；JSON backend 直接随 `Entry` 序列化。

### 6.2 Gate 入口

- 新增 `AssessConsolidationGate(evidence, options)`：检查 evidence quorum、source diversity、owner/project 边界一致性；空 owner 视为 shared evidence，不会把单一 owner 的证据群误判为混合 owner。
- 新增 `InferMemoryBoundary(evidence)`：从现有 `OwnerID`、`Scope`、project-like tags、`SourceType`、时间戳以及已有派生 `Boundary` 推导保守边界。
- `Synthesizer` 生成 schema 前先过 gate；LLM 需要返回 `evidence_ids`，系统把它写入 `EvidenceIDs` 和 `RelatedIDs`。
- `ProfileConsolidator` 更新/创建 profile 前先调用 `AssessConsolidationGate`，证据不足或 owner/project 边界混杂时跳过 LLM 与写入；通过后写入 `EvidenceIDs`、`DerivedKind=profile` 和 owner/source/time 边界。
- `ThemeManager` rebuild 后为每个 theme 写入 `EvidenceIDs`、`DerivedKind=theme` 和从成员推导的保守边界。
- `Consolidator` 生成 TMT segment/session/day/week/profile 时写入 `DerivedKind=tmt:*`、上层节点的 `EvidenceIDs/RelatedIDs=child IDs`，并记录 owner/source/time window 边界。
- `UpsertConversationSummary` 与 `UpsertSessionCheckpoint` 也写入 `DerivedKind` 和保守 `Boundary`；`UpsertProjectKnowledge` 与 `UpsertTaskArtifact` 支持调用方显式传入 `EvidenceIDs`、`RelatedIDs`、`DerivedKind`、`Boundary`，用于标注确实是派生视图的生成记录。`SessionStartExtractor` 已改走 `UpsertTaskArtifact`，输出 `summary:session_start` 派生元数据和 owner/source 边界。
- `RecallDynamic`、`RecallDynamicStrict`、project recall 已读取 `Entry.Boundary`：当前 owner 或 project 与派生记忆边界不匹配时排除，未带边界的旧记忆保持原行为。
- `RecallAdaptiveHier` 的 theme selection 也按 `ThemeNode.Boundary` 过滤，避免复杂查询通过主题层选中其它 owner/project 的派生主题。
- `SearchByMode(SearchDirect/SearchKeywordOnly)` 已继续向下传递 `ownerID`，direct/keyword 诊断路径不会绕过 owner 或派生边界。
- `FormatRecallEntryForTool` 已输出 `DerivedKind`、`EvidenceIDs` 和 `Boundary`，让显式 recall/debug 能直接看到派生记忆的证据链和适用前提。
- 新增 `DerivedMemoryAudits` 和 memory tool `action=derived`：按当前 owner/project 边界列出派生记忆，报告缺失 evidence、缺失 boundary 等 surgery 前检查项。
- 新增 `SupersedeDerivedMemory` 和 memory tool `action=derived_surgery`：只允许 supersede 派生记忆，并按当前 owner/project 边界校验；原始 evidence 默认拒绝操作。

### 6.3 本轮暂不做

- 暂不改 `RecallDynamic` 排序公式；本轮只在候选过滤阶段使用边界，避免跨 owner/project 污染。
- 暂不删除或重写 `Compressor` 的现有行为；`ProfileConsolidator` 已接入轻量 consolidation gate，`ThemeManager` 当前只补派生元数据，不改其生成策略。
- 暂不做完整 memory surgery UI；本轮只接入保守的 derived supersede 动作，不做降权、批量删除或 evidence 删除。

## 7. 设计变更

### 7.1 统一召回默认策略

变更：所有 UI、agent tool、TUI 帮助文本把 `auto` 作为推荐默认值。保留 `dynamic`、`adaptive`、`lightmem`、`hybrid` 作为高级/调试选项。

建议规则：

- `auto`：默认。
- 空 `mode` 已在 `RecallByMode` 中归一为 `auto`，避免 GUI/TUI/server agent 默认值分叉。
- `dynamic`：验证基础召回质量时使用。
- `adaptive`：验证复杂问题、主题扩展和多样性时使用。
- `lightmem`：验证低 token budget 多路召回时使用。
- `hybrid`：兼容旧调用，不再新增文档入口。

### 7.2 GUI/TUI/MaClawSrv 存储入口统一到 StoreFactory

变更：

- GUI 初始化仍通过 `NewStoreWithMode(..., StoreModeSQLite)`，失败后显式降级到 JSON。
- TUI memory 命令、pipe mode、app 初始化均通过 `NewStoreWithMode(..., StoreModeAuto)`。
- MaClawSrv/agentservice 通过 `NewStoreWithModeAndLegacyJSON(..., StoreModeAuto, agent_memory.json)` 打开长期记忆，兼容旧 `agent_memory.json` 作为一次性 seed，不让 server 侧继续维护平行 memory backend。
- 存在 `memory.db` 或环境变量指定 SQLite 时走 SQLite。
- 否则保留 JSON backend。
- `ConversationMemory`、`KnowledgeStore`、`MemoryRecordStore`、`ToolMemoryStore` 均在代码注释与策略测试中标明为辅助上下文/控制面/执行提示，不是长期记忆替代品。GUI 的 `MemoryCompressor` 也改为 `corelib/memory.Compressor` 的薄适配层，避免 GUI 侧继续维护独立 dedup/merge/compress/backup 实现。GUI 初始化不再自行拼装 compressor/pipeline/synthesizer/TiMem/profile/online extractor，而是通过 `corelib/memory.NewMaintenance(...).InstallRuntime()` 装配共享维护拓扑。GUI/TUI 的 memory delete 统一走 `corelib/memory.HandleTool(action=delete)`，避免 UI 命令绕过共享 tool action、AfterWrite 和后续审计扩展。`/btw` 这类只读侧查询允许 recall/themes/scenes/trace/candidates/derived，禁止 save/delete/derived_surgery，避免派生记忆审计和派生手术混在同一权限层。

收益：

- GUI、TUI、MaClawSrv 自动共享 SQLite/JSON 选择逻辑和 legacy JSON seed 迁移策略。
- 减少未来迁移 SQLite 时的入口遗漏。
### 7.3 Consolidation gate 与去重职责文档化

变更：在 `corelib/memory` 增加短注释或 package doc，明确：

- realtime dedup 是写入幂等。
- pending semantic dedup 是异步精确路径。
- compressor dedup 是维护兜底。
- candidate consolidation 是治理队列，不等价于普通 dedup。
- schema consolidation 必须经过 gate：证据数量、多样性、边界一致性、冲突检查和 episodic-only 对照收益。

### 7.4 ConflictDetector 状态明确化

变更选项：

- 方案 A：接入 OnlineExtractor 作为 UPDATE/DELETE 前的冲突确认。
- 方案 B：标注为 experimental/internal，只供后续评估。

建议先采用方案 B。原因是 OnlineExtractor 已经有四操作分类，贸然接入 `ConflictDetector` 会增加一次 LLM 判断和潜在冲突决策差异。

### 7.5 抽取路径主从关系文档化

变更：在 `KnowledgeExtractor`、`ConversationArchiver` 注释和相关设计文档里明确：

- OnlineExtractor 是事实写入主路径。
- KnowledgeExtractor 是 fallback。
- ConversationArchiver 摘要是 legacy/兜底上下文，不参与普通 `RecallDynamic` 的优先记忆注入。
- 已在代码注释中标注 fallback/experimental 状态；Archiver 在 OnlineExtractor 最近成功时跳过，避免重复生成摘要记忆。

## 8. 实施计划

### Phase 1：文档和默认入口收敛

- 更新本文档。
- 更新 TUI/agent 帮助文本：推荐 `auto`。
- 不删除旧 mode。

验收：

- 文档解释现有模式差异。
- 用户可理解哪个是默认、哪个是调试。

### Phase 2：TUI 记忆入口统一

- TUI app 初始化、pipe mode、memory command 统一使用 StoreFactory。
- 保留测试用 JSON path 入口。

验收：

- 存在 `memory.db` 或设置 `MACLAW_MEMORY_BACKEND=sqlite` 时使用 SQLite。
- `go test ./tui/... ./corelib/memory/...` 通过。

### Phase 3：Consolidation gate、证据链与 schema 边界标注

- 为 `semantic_dedup.go`、`candidate_consolidation.go`、`compressor.go` 增加职责边界注释。
- 为 `conflict.go` 增加 experimental/internal 说明，避免被误认为已在主链路生效。
- 梳理 `Synthesizer`、`ProfileConsolidator`、`ThemeManager` 的输出字段，明确 derived view 应引用 evidence IDs/source/version，并携带 project/task/time/owner 等适用边界。`Synthesizer` 与 `ProfileConsolidator` 已在写入前使用 consolidation gate。
- 增加 consolidation gate 的设计入口：证据 quorum、分组纯度、边界一致性、窄流过拟合风险和 expected utility 检查。
- 增加 memory surgery 口径：派生 schema 可降权、supersede 或删除；原始 evidence 默认保留。

验收：

- 代码行为不变，或只补充兼容字段。
- 文档和注释能解释各层去重职责、consolidation gate、证据链和 schema 边界。

### Phase 4：召回 API 文档化

- 给 `SearchByMode`、`RecallByMode`、`RecallDynamic`、`RecallAdaptiveHier` 增加选择说明。（已补代码注释）
- 明确 owner/project boundary 的传递要求。（已补代码注释和 auto 边界测试）

验收：

- 开发者可以按文档判断何时用各模式。
- 旧 eval 用例无需修改。

## 9. 非目标

- 不删除 `CategoryUser/Feedback/Project/Reference`，它们作为兼容输入保留。
- 不删除 `KnowledgeExtractor` 和 `ConversationArchiver`，只把它们定位为 fallback。
- 不在本阶段重写 `RecallDynamic`、`RecallAdaptiveHier` 的评分公式。
- 不把 iWorkerCenter 的 `shared_memories` 管理模型纳入本轮整理。

## 10. 风险与缓解

| 风险 | 影响 | 缓解 |
|---|---|---|
| SQLite/JSON 后端选择不一致 | GUI/TUI 看到不同记忆 | 所有入口走 StoreFactory |
| `auto` 默认召回改变结果顺序 | 部分复杂问题结果变化 | 仅推荐默认，不强制改底层旧模式；保留 explicit mode |
| 去重职责文档化不足 | 后续继续新增重复去重层 | 在相关文件加职责注释，并在 pipeline 注释说明顺序 |
| ConflictDetector 长期闲置 | 代码困惑 | 已标为 experimental/internal，后续单独评估是否接入 OnlineExtractor |
| schema 连续重写导致效用下降 | 更多经验反而带来更差记忆 | 默认保留 evidence；consolidation 通过 gate；增加 episodic-only/abstract-only 对照和 memory surgery |
| schema 边界过窄 | 派生记忆召回不足 | 保留原始 evidence；派生层可在后续 gate 中放宽边界，但必须有证据支持 |

## 11. 验证建议

最小验证：

```powershell
go test ./corelib/memory/... ./tui/...
```

行为验证：

- `memory recall --mode auto` 对简单查询走 dynamic，对复杂对比/分析查询走 adaptive。
- OnlineExtractor 成功后，KnowledgeExtractor 和 ConversationArchiver 不重复写摘要。
- TUI 在 JSON/SQLite 两种后端下均能 list/search/recall。
- 派生 schema/profile/theme/TMT/summary/checkpoint 写入后，显式 recall 或 `action=derived` 能看到 `EvidenceIDs`、`DerivedKind`、`Boundary` 和缺失证据/边界问题。
- owner/project 与派生记忆边界不匹配时，`RecallDynamic`、`RecallDynamicStrict`、`RecallAdaptiveHier` 以及 `SearchByMode` 的 direct/keyword 路径不返回或不选中该派生记忆/主题。
- 新增记忆质量评测时至少比较 no-memory、episodic-only、abstract-only、episodic+abstract、forced-consolidation。
- 对连续任务切换流，统计 overgeneralized schema、garbage schema、schema 边界不匹配和 memory surgery 后收益。

## 12. 最终整理口径

整理后的记忆系统应被描述为：

> Maclaw 的长期记忆由 `corelib/memory.Store` 统一管理。写入侧以 `SaveWithContext` 和 OnlineExtractor 四操作保存证据为主；维护侧通过 pipeline 做异步去重、候选治理、压缩视图、schema 形成和主题重建，但派生结果必须保留 evidence/source/version 与适用边界；召回侧默认使用 `auto`，简单问题走 `RecallDynamic`，复杂问题通过 Adaptive 主题层扩展，并在需要时展开原始证据。其它模式和工具保留为调试、兼容或 fallback 能力。
