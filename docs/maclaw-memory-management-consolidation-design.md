# Maclaw 记忆管理能力整理与收敛设计

日期：2026-05-19  
范围：`corelib/memory`、GUI/TUI/agent 对 `corelib/memory` 的接入。`iWorkerCenter` 已复用 corelib，本设计暂不展开它自己的 `shared_memories` 管理表、经验提取接口和后台管理能力。

## 1. 背景

Maclaw 的记忆能力已经从单一长期记忆库演进为一套多索引、多召回、多维护管线的系统：写入侧有脱敏、注入扫描、hash/substring 去重、语义去重、候选治理、在线抽取；召回侧有 BM25、向量、图扩展、实体索引、主题层、LightMem 规划、Adaptive 主题扩展；维护侧有衰减、压缩、合成、TMT consolidation、profile consolidation、theme rebuild。

当前主要问题不是能力不足，而是同类能力层数过多、入口命名分散、职责边界不够清晰。继续堆能力会让后续调试变难，也会提高重复写入、重复召回、重复 LLM 调用的概率。

本设计目标是整理现有能力，明确主路径、降级路径和实验能力，减少重复概念，而不是大规模重写。

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

### 2.3 写入路径

现有写入主路径：

1. 工具/代码调用 `Save`、`SaveForUser` 或 `SaveWithContext`。
2. 写入前执行 prompt injection 扫描与敏感信息脱敏。
3. 通过 content hash 和完全内容比较做确定性幂等。
4. 通过 substring duplicate 处理近似重复。
5. 如有 embedder，则生成 embedding，并加入异步 semantic dedup 候选队列。
6. 持久化并更新 BM25、vector、graph、project/entity/semantic graph、theme layer。

另有治理写入入口 `SaveGovernedWithContext`：

- accept：直接写入。
- quarantine：以 dormant + `memory_candidate` tag 暂存。
- reject：返回 `ErrMemoryCandidateRejected`。

### 2.4 抽取路径

当前抽取路径有三层：

- `OnlineExtractor`：主路径。每轮 agent loop 结束后触发，提取结构化事实，并使用 ADD/UPDATE/DELETE/NOOP 四操作写入。
- `KnowledgeExtractor`：fallback。会话归档时触发，已通过 `OnlineExtractor.HasRecentSuccess(60min)` 避免重复。
- `ConversationArchiver`：会话摘要归档。也已在 OnlineExtractor 成功时跳过摘要写入。

这块已经有互斥设计，建议文档化并保持 OnlineExtractor 为唯一主路径。

### 2.5 召回路径

当前可见模式：

- `dynamic`：主召回。融合 BM25、向量、实体/标签、图扩展、项目/owner 过滤、recency/importance/relevance 等信号。
- `hybrid` / `recall`：经 `SearchByMode(SearchHybrid)` 进入另一套 hybrid search。
- `lightmem`：基于意图信号构建多路召回计划，再调用 `RecallDynamic`。
- `adaptive`：先调用 `RecallDynamic` 取 seed，再通过 ThemeManager 做主题扩展和多样性选择。
- `auto`：简单查询走 dynamic，复杂查询走 adaptive。

问题在于 `dynamic` 与 `hybrid` 对外语义容易混淆；`lightmem` 和 `adaptive` 实际都是 `dynamic` 之上的控制器，却作为平级模式暴露。

### 2.6 后台维护管线

`Pipeline.RunOnce` 当前顺序：

1. strength 衰减并标记 dormant。
2. 处理 pending semantic dedup。
3. Consolidate quarantined memory candidates。
4. ExperienceDistiller 分析经验类型，给压缩/合成提供保护样本。
5. Compressor 做精确去重、LLM 语义合并、压缩、CompactForm 回填。
6. Synthesizer 合并 promotion/reflection。
7. Consolidator 做 TMT 分层整合。
8. ProfileConsolidator 做 profile 层整合。
9. ThemeManager rebuild。

整体链路合理，但去重/合并相关步骤较多，需要明确每一层只处理自己的职责。

## 3. 重复、冗余与冲突点

### 3.1 召回模式概念重复

问题：`dynamic`、`hybrid`、`lightmem`、`adaptive`、`auto` 同时作为用户/工具可见模式，调用者很难判断默认应该用哪个。

判断：

- `dynamic` 是事实上的主召回引擎。
- `lightmem` 是轻量预算和路由控制器。
- `adaptive` 是复杂查询的主题扩展器。
- `auto` 应该成为默认策略选择器。
- `hybrid` 更像内部检索策略，不应该作为主入口概念继续扩张。

设计决策：对外推荐只使用 `mode=auto`；保留其他模式用于调试和兼容，但文档中降级为 internal/debug modes。

### 3.2 去重/合并能力重复

现有相关能力：

- `SaveWithContext`：hash、exact、substring 去重。
- `semantic_dedup.go`：embedding candidate + LLM precise merge。
- `candidate_consolidation.go`：quarantine candidate 的 promote/merge/reject。
- `Compressor.dedup`：全量精确/substring 去重。
- `Compressor.mergeSemanticDuplicates`：维护期 LLM 语义合并。
- `OnlineExtractor`：四操作分类中的 update/delete/noop。
- `ConflictDetector`：独立冲突检测，目前没有接入主写入链路。

设计决策：统一为四层职责，不互相抢职责。

| 层级 | 执行时机 | 只做什么 | 不做什么 |
|---|---|---|---|
| 写入快速层 | `SaveWithContext` | 安全扫描、脱敏、hash/exact/substring 幂等 | 不调用 LLM，不判断复杂冲突 |
| 写入治理层 | `SaveGovernedWithContext` / OnlineExtractor | 判断是否值得写、ADD/UPDATE/DELETE/NOOP | 不做全库压缩 |
| 异步精确层 | `ProcessPendingDedup` | 对高相似候选做 LLM 精确合并 | 不扫描全库 |
| 维护整理层 | Pipeline/Compressor/Candidates | 批处理候选、全量去重、压缩、主题重建 | 不介入实时响应路径 |

`ConflictDetector` 当前应标记为实验能力。后续如果要启用，应接入 OnlineExtractor 的 UPDATE/DELETE 分类，或接入 `SaveGovernedWithContext`，避免成为第七套并行冲突机制。

### 3.3 抽取路径重复但已有互斥

问题：OnlineExtractor、KnowledgeExtractor、ConversationArchiver 都可能从会话产生长期记忆。

现状已有保护：

- OnlineExtractor 有 3 分钟 cooldown。
- KnowledgeExtractor 在 OnlineExtractor 60 分钟内成功时跳过。
- ConversationArchiver 在 OnlineExtractor 60 分钟内成功时跳过 summary。
- OnlineExtractor 和 KnowledgeExtractor 会检查显式 memory write signal；ConversationArchiver 的 summary 路径当前只检查 OnlineExtractor 是否最近成功。

设计决策：保持三者共存，但定义主从关系：

- 主路径：OnlineExtractor。
- fallback：KnowledgeExtractor。
- legacy/summary：ConversationArchiver，只在 OnlineExtractor 不可用或未成功时写摘要。

### 3.4 存储入口分叉

问题：GUI 使用 `NewStoreWithMode(baseDir, StoreModeSQLite)`；TUI runtime 曾把 `memoryDir` 目录直接传给 `NewStore`，CLI memory command 曾直接打开 `memories.json`。Store factory 已经提供 Auto/SQLite/JSON 选择，但入口没有完全统一。

设计决策：所有生产入口先解析 `memoryDir := filepath.Join(dataDir, "memory")`，再使用 `NewStoreWithMode(memoryDir, StoreModeAuto)`，并通过配置或环境变量选择 JSON/SQLite。测试可继续直接用 `NewStore(path)`。

### 3.5 分类体系双轨但可接受

Claude-style category 与 legacy category 并存，但 `MapToCanonical` 已经把它们收敛到内部语义。该重复主要是兼容层，不建议删除。

设计决策：保留双轨输入，内部所有索引、scope、tier、dedup、recall 都必须先 canonicalize。

## 4. 目标架构

### 4.1 对外心智模型

对调用方只暴露三个概念：

1. `save`：写入长期记忆。
2. `recall`：默认 `mode=auto`，由系统选择轻量/复杂召回路径。
3. `maintain`：后台整理，包括候选治理、去重、压缩、合成、主题维护。

### 4.2 写入链路

```text
manual/tool save
  -> SaveGovernedWithContext, optional
  -> SaveWithContext
  -> security scan + redact
  -> deterministic dedup
  -> embedding candidate enqueue
  -> persist + rebuild derived indexes
  -> pipeline later: pending semantic dedup + candidate consolidation

online extraction
  -> ExtractAndIntegrate
  -> find similar memories
  -> LLM classifies ADD/UPDATE/DELETE/NOOP
  -> SaveGovernedWithContext / Update / Delete
```

### 4.3 召回链路

```text
memory recall(mode=auto)
  -> classify query complexity
  -> simple query: RecallDynamic
  -> complex query: RecallAdaptiveHier
       -> seed = RecallDynamic
       -> select themes by seed/facet
       -> expand theme evidence
       -> diversity and token-budget selection
```

`lightmem` 保留给低预算场景或调试，不作为默认对外推荐路径。

### 4.4 维护链路

```text
Pipeline.RunOnce
  -> decay/dormant
  -> ProcessPendingDedup
  -> ConsolidateMemoryCandidates
  -> ExperienceDistiller
  -> Compressor
  -> Synthesizer
  -> TMT Consolidator
  -> ProfileConsolidator
  -> ThemeManager rebuild
```

## 5. 设计变更

### 5.1 统一召回默认策略

变更：所有 UI、agent tool、TUI 帮助文本把 `auto` 作为推荐默认值。保留 `dynamic`、`adaptive`、`lightmem`、`hybrid` 作为高级/调试选项。

建议规则：

- `auto`：默认。
- `dynamic`：验证基础召回质量时使用。
- `adaptive`：验证复杂问题、主题扩展和多样性时使用。
- `lightmem`：验证低 token budget 多路召回时使用。
- `hybrid`：兼容旧调用，不再新增文档入口。

### 5.2 TUI 存储入口改为 StoreFactory

变更：

- `tui/app.go`、`tui/pipe_mode.go`、`tui/commands/memory.go` 统一切到 `memory.NewStoreWithMode(memoryDir, memory.StoreModeAuto)`。
- 测试工具和小范围单元测试可继续直接 `NewStore(path)`。

收益：

- TUI 与 GUI 自动共享 SQLite/JSON 选择逻辑。
- 减少未来迁移 SQLite 时的入口遗漏。

### 5.3 去重职责文档化并补注释

变更：在 `corelib/memory` 增加一份短注释或 package doc，明确：

- write-time deterministic dedup 是实时路径。
- pending semantic dedup 是异步精确路径。
- compressor dedup 是维护兜底。
- candidate consolidation 是治理队列，不等价于普通 dedup。

这一步不改行为，只降低后续误用概率。

### 5.4 ConflictDetector 状态明确化

变更选项：

- 方案 A：接入主链路。把 `ConflictDetector.Check` 纳入 OnlineExtractor 的 UPDATE/DELETE 判断前后，作为 contradiction 辅助证据。
- 方案 B：保留但标为 experimental/internal，不在生产写入路径宣传。

建议先采用方案 B。原因是 OnlineExtractor 已经有四操作分类，贸然接入 `ConflictDetector` 会增加一次 LLM 判断和潜在冲突决策差异。

### 5.5 抽取路径主从关系文档化

变更：在 `KnowledgeExtractor`、`ConversationArchiver` 注释和相关设计文档里明确：

- OnlineExtractor 是事实写入主路径。
- KnowledgeExtractor 是 fallback。
- ConversationArchiver 摘要是 legacy/兜底上下文，不参与普通 `RecallDynamic` 的优先记忆注入。

## 6. 实施计划

### Phase 1：文档和默认入口收敛

- 新增本设计文档。
- 更新 TUI/agent/tool help 文案：推荐 `mode=auto`。
- 在 `tool_service.go` 的错误信息和帮助文案中把 `auto` 放在首位。

验收：

- `maclaw-tui memory recall --help` 或 usage 文案不再把 `hybrid` 作为首选。
- 旧参数仍兼容。

### Phase 2：StoreFactory 统一

- TUI runtime 改用 `NewStoreWithMode(memoryDir, StoreModeAuto)`。
- CLI memory command 以传入的 memory data directory 调用同一 store factory 入口。
- 保持单测直接 `NewStore(path)` 不变，避免测试成本扩散。

验收：

- 无 `MACLAW_MEMORY_BACKEND` 且无 `memory.db` 时仍使用 JSON。
- 存在 `memory.db` 或设置 `MACLAW_MEMORY_BACKEND=sqlite` 时使用 SQLite。
- `go test ./tui/... ./corelib/memory/...` 通过。

### Phase 3：去重职责注释与 ConflictDetector 标记

- 为 `semantic_dedup.go`、`candidate_consolidation.go`、`compressor.go` 增加职责边界注释。
- 为 `conflict.go` 增加 experimental/internal 说明，避免被误认为已在主链路生效。

验收：

- 代码行为不变。
- 文档和注释能解释各层去重职责。

### Phase 4：召回 API 文档化

- 更新 `docs/xmemory-memory-improvements.md` 或新增使用说明，明确 `auto` 为默认。
- TUI/GUI debug 输出保留 `dynamic`、`adaptive`、`lightmem` 细节。

验收：

- 开发者可以按文档判断何时用各模式。
- 旧 eval 用例无需修改。

## 7. 非目标

- 不删除 `CategoryUser/Feedback/Project/Reference`，它们作为兼容输入保留。
- 不删除 `KnowledgeExtractor` 和 `ConversationArchiver`，只把它们定位为 fallback。
- 不在本阶段重写 `RecallDynamic`、`RecallAdaptiveHier` 的评分公式。
- 不把 iWorkerCenter 的 `shared_memories` 管理模型纳入本轮整理。

## 8. 风险与缓解

| 风险 | 影响 | 缓解 |
|---|---|---|
| TUI 切 StoreFactory 后读错旧数据 | 用户看不到旧记忆 | Auto 规则保持无 `memory.db` 时走 JSON；SQLite 迁移已有 legacy import |
| `auto` 默认召回改变结果顺序 | 部分复杂问题结果变化 | 仅推荐默认，不强制改底层旧模式；保留 explicit mode |
| 去重职责文档化不足 | 后续继续新增重复去重层 | 在相关文件加职责注释，并在 pipeline 注释说明顺序 |
| ConflictDetector 长期闲置 | 代码困惑 | 标为 experimental，后续单独评估是否接入 OnlineExtractor |

## 9. 验证建议

最小验证：

```powershell
go test ./corelib/memory/... ./tui/...
```

重点用例：

- 写入相同内容，只保留一条或更新 access/tags。
- 写入 substring 近似内容，合并到较长内容。
- `memory recall --mode auto` 对简单查询走 dynamic，对复杂对比/分析查询走 adaptive。
- OnlineExtractor 成功后，KnowledgeExtractor 和 ConversationArchiver 不重复写摘要。
- TUI 在 JSON/SQLite 两种后端下均能 list/search/recall。

## 10. 最终整理口径

整理后的记忆系统应被描述为：

> Maclaw 的长期记忆由 `corelib/memory.Store` 统一管理。写入侧以 `SaveWithContext` 和 OnlineExtractor 四操作为主，维护侧通过 pipeline 做异步去重、候选治理、压缩和主题重建；召回侧默认使用 `auto`，简单问题走 `RecallDynamic`，复杂问题通过 Adaptive 主题层扩展。其它模式和工具保留为调试、兼容或 fallback 能力。

