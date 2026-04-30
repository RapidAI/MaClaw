# Mem0 / Graphiti 论文对比 MacLaw 记忆系统分析

## 一、三个系统的核心架构对比

### 1.1 数据模型

| 维度 | Mem0 | Graphiti (Zep) | MacLaw |
|------|------|----------------|--------|
| 基本单元 | 自然语言事实（flat text） | 三层子图：Episode → Semantic Entity → Community | Entry（结构化记录，含 30+ 字段） |
| 图结构 | Mem0^g: 有向标签图 (V,E,L)，节点=实体，边=关系 | 三层知识图谱 + 双时间线模型 | memoryGraph：无向加权图，节点=Entry，边=相关性 |
| 时间建模 | 无显式时间建模（仅 CreatedAt） | **双时间线**：T（事件时间）+ T'（摄入时间），每条边 4 个时间戳 | TemporalTree (TiMem L1-L5) + Entry.CreatedAt/UpdatedAt |
| 存储后端 | 向量数据库（embedding 检索） | Neo4j 图数据库 + Lucene 全文索引 | 内存 []Entry + JSON 文件持久化 |
| 容量 | 无硬上限（云服务） | 无硬上限（Neo4j） | **maxItems=500**（硬上限）+ Archive 1000 |

### 1.2 写入管线

| 维度 | Mem0 | Graphiti (Zep) | MacLaw |
|------|------|----------------|--------|
| 触发时机 | **每对消息实时处理** (m_{t-1}, m_t) | **每条消息实时处理** | 被动：会话过期 / LLM 主动调用 / Pipeline 6h |
| 提取方式 | LLM 提取事实 → 4 操作分类（ADD/UPDATE/DELETE/NOOP） | LLM 提取实体 + 关系三元组 → 实体解析 → 事实解析 → 时间提取 → 边失效 | KnowledgeExtractor（LLM 提取知识点，1h 冷却） |
| 去重/冲突 | 向量相似度 top-s → LLM 判断操作类型 | 实体解析（embedding + 全文搜索 + LLM）+ 边去重 + 时间失效 | hash 精确去重 + 子串去重 + Pipeline 语义去重 |
| 上下文感知 | ✅ 对话摘要 S + 最近 m 条消息 | ✅ 最近 n=4 条消息 + 反射技术 | ⚠️ SaveWithContext（Phase 2 已实现，但仅 memory tool 路径） |

### 1.3 召回管线

| 维度 | Mem0 | Graphiti (Zep) | MacLaw |
|------|------|----------------|--------|
| 检索方式 | 向量相似度 | 三路搜索：cosine + BM25 + BFS 图遍历 | 三路 RRF：BM25 + Vector + Tag 交叉 + 图扩展 |
| 重排序 | 无（直接返回 top-k） | RRF / MMR / 图距离 / 提及频率 / 交叉编码器 | RRF 融合 + memoryStreamScore + RecallGating |
| 时间感知 | ❌ | ✅ 边的 valid_at/invalid_at 过滤 | ⚠️ Ebbinghaus 衰减（Strength 字段），但无事件时间过滤 |
| 社区/全局 | ❌ | ✅ Community 子图提供全局摘要 | ❌ 无社区检测 |

---

## 二、MacLaw 相对于 Mem0 的问题与改进点

### 问题 1 (P0): 写入管线是被动的——Mem0 的核心优势是实时增量处理

**Mem0 的做法**：每对消息 (m_{t-1}, m_t) 到达时立即触发 extraction → update 管线。LLM 从当前消息对中提取事实，与已有记忆做向量相似度比较，通过 tool call 决定 ADD/UPDATE/DELETE/NOOP。整个过程是**在线的、增量的**。

**MacLaw 的现状**：
- `KnowledgeExtractor`：1 小时冷却，且只在会话过期时触发
- `ConversationArchiver`：同上
- `memory(action=save)`：依赖 LLM 主动调用（不可靠）
- Phase 1 的 `task_artifact` 沉淀：只覆盖工作流阶段产出物

**差距**：MacLaw 的大部分对话内容在截断前没有进入长期记忆。Mem0 论文的核心贡献就是证明了"实时增量提取 + 四操作更新"比被动提取效果好 26%（相对 OpenAI）。

**改进建议**：实现类似 Mem0 的在线提取管线——每轮对话结束后（不是每对消息，MacLaw 的 agent loop 粒度更粗），异步触发轻量 LLM 提取。关键设计点：
- 不阻塞 agent loop（异步 goroutine）
- 提取结果与已有记忆做向量/BM25 比较
- 四操作分类（ADD/UPDATE/DELETE/NOOP）替代当前的"只 ADD"模式
- 冷却从 1 小时降到 5 分钟（或按对话轮次触发）

### 问题 2 (P0): 缺少 UPDATE 和 DELETE 操作——记忆只增不减

**Mem0 的做法**：每个新提取的事实都与已有记忆比较，LLM 决定是 ADD（新信息）、UPDATE（补充已有信息）、DELETE（矛盾信息）还是 NOOP（已存在）。这保证了记忆库的**一致性**——矛盾的旧信息被删除，不完整的信息被更新。

**MacLaw 的现状**：
- `Save()` 只做 ADD（hash 去重 + 子串去重只防止完全重复，不处理矛盾）
- `Status = StatusSuperseded` 字段存在但只在 Pipeline 的 `dreamCycle` 中被设置（6 小时周期）
- 没有实时的矛盾检测和旧信息失效机制

**差距**：用户说"我搬到了上海"后，旧记忆"用户住在北京"不会被立即失效。要等 6 小时 Pipeline 的 `dreamCycle` 才可能检测到冲突。在此期间，两条矛盾的记忆同时存在，RecallDynamic 可能返回过时信息。

**改进建议**：在 `SaveWithContext` 路径中，对新 entry 做向量相似度 top-5 检索，当相似度超过阈值时，调用 LLM 判断是 UPDATE（合并）还是 DELETE（失效旧 entry）。这与 MacLaw 已有的 `pendingDedup` 异步语义去重机制类似，但需要扩展为四操作分类。

### 问题 3 (P1): 500 条硬上限——在长期使用中严重不足

**Mem0 的做法**：无硬上限，使用向量数据库存储，容量随使用增长。

**MacLaw 的现状**：`maxItems=500`，超过后 LRU 淘汰到 Archive。Archive 有 1000 条上限。

**差距**：对于日常使用的 AI 助手，500 条记忆在几周内就会被填满。LRU 淘汰不区分重要性——一条关键的 SSH 服务器信息可能因为长时间未访问而被淘汰。

**改进建议**：
- 短期：提升 maxItems 到 2000-5000（JSON 文件 + 内存索引在这个规模仍然可行）
- 中期：Phase 5 的分区存储 + 按 Category 差异化容量（project_knowledge 500 条，conversation_summary 200 条等）
- 长期：考虑 SQLite 替代 JSON（MacLaw 的"不做"列表中排除了数据库，但 500 条上限是实际痛点）

---

## 三、MacLaw 相对于 Graphiti/Zep 的问题与改进点

### 问题 4 (P0): 缺少双时间线模型——无法回答"什么时候"类问题

**Graphiti 的做法**：每条边（事实）有 4 个时间戳：
- `t'_created` / `t'_expired`：系统摄入/失效时间（审计用）
- `t_valid` / `t_invalid`：事实在现实世界中成立/失效的时间

这使得 Graphiti 能回答"用户去年住在哪里？"（查 t_valid 在去年的 `lives_in` 边）和"用户什么时候搬家的？"（查 `lives_in` 边的 t_invalid）。

**MacLaw 的现状**：
- `Entry.CreatedAt` / `Entry.UpdatedAt`：只有系统时间
- `Entry.Interval`：TiMem 的时间区间，但只用于 Consolidator 的层级聚合，不用于事实的有效期建模
- 没有"事实在现实世界中何时成立/失效"的概念

**差距**：用户说"我上个月换了工作"，MacLaw 保存的记忆没有"上个月"的时间标注。后续问"用户什么时候换的工作？"时，RecallDynamic 能找到这条记忆，但 LLM 需要从记忆内容中推断时间——如果内容被 CompactForm 压缩过，时间信息可能丢失。

**改进建议**：
- Entry 新增 `ValidAt` / `InvalidAt` 字段（可选，`omitempty`）
- KnowledgeExtractor 和在线提取管线中，用 LLM 从对话上下文提取事实的时间信息（复用 Graphiti 的 temporal extraction prompt 思路）
- RecallDynamic 新增时间过滤选项：`RecallDynamic(query, ..., timeRange *TimeRange)`
- 不需要 Neo4j——在内存 []Entry 上做时间过滤足够（500-2000 条规模）

### 问题 5 (P1): 图结构过于简单——缺少实体-关系三元组

**Graphiti 的做法**：知识图谱的节点是**实体**（人、地点、概念），边是**关系**（lives_in, works_for, prefers）。每条边有 fact 字段（自然语言描述）和 relation_type（结构化标签）。这使得图遍历能发现隐含关联。

**Mem0^g 的做法**：类似，有向标签图 G=(V,E,L)，节点有类型分类和 embedding，关系是三元组 (source, relation, destination)。

**MacLaw 的现状**：
- `memoryGraph` 的节点是 Entry（整条记忆），不是实体
- 边是无类型的加权相关性（`graphEdge.Strength`），没有关系语义
- `LinkType` 字段存在（references, supersedes, derived_from, conflicts）但只有 4 种通用类型，不是领域特定的关系
- `autoLink` 基于 tag 重叠建立边，不是基于实体-关系提取

**差距**：MacLaw 的图是"记忆条目之间的相关性网络"，不是"实体之间的关系图谱"。前者只能做"找到相关的记忆"，后者能做"从用户 → works_at → 公司 → located_in → 城市"的多跳推理。

**改进建议**：这是一个大的架构变更，建议分阶段：
- **Phase A**（低成本）：在 KnowledgeExtractor 中，除了提取知识点，同时提取实体和关系三元组。将三元组存储为 Entry 的 Tags（如 `["entity:Alice", "relation:lives_in", "entity:Shanghai"]`）。RecallDynamic 的 tag 匹配自然覆盖实体检索。
- **Phase B**（中成本）：新增 `EntityIndex`（类似已有的 `ProjectIndex`），维护实体名 → Entry ID 的映射。支持"给定实体，找到所有相关事实"的查询。
- **Phase C**（高成本）：完整的实体-关系图谱，类似 Graphiti 的 Semantic Entity Subgraph。需要 Neo4j 或自建图索引。

### 问题 6 (P1): 缺少社区检测——无法提供全局摘要

**Graphiti 的做法**：Community Subgraph 通过 label propagation 算法检测强连接的实体簇，生成高层摘要。检索时，community 名称的 embedding 搜索提供全局视角。

**MacLaw 的现状**：
- TiMem L5 (Profile) 是最接近的等价物——它是用户画像的全局摘要
- 但 L5 只有一个节点（用户画像），不是多个主题簇
- 没有"项目 A 的技术栈"、"用户的饮食偏好"等主题级别的摘要

**改进建议**：
- 利用已有的 `ProjectIndex` 扩展——每个项目自动生成项目级摘要（类似 community summary）
- 在 Pipeline 中新增"主题聚类"步骤：对 project_knowledge 类别的记忆做 tag 聚类，生成主题摘要
- 不需要完整的社区检测算法——MacLaw 的 500 条规模用 tag 聚类足够

---

## 四、MacLaw 的独特优势（Mem0/Graphiti 没有的）

### 优势 1: TiMem 五级时间层次结构

MacLaw 的 TemporalTree (L1-L5) 是 Mem0 和 Graphiti 都没有的。Mem0 是扁平的事实列表，Graphiti 有时间线但没有层级聚合。TiMem 的 Segment → Session → Day → Week → Profile 层级提供了不同粒度的记忆视图，这在复杂查询（"上周我们讨论了什么？"）中有优势。

**建议**：保持 TiMem 结构，但需要确保 Consolidator 的 LLM 调用质量。当前 L2-L5 的聚合依赖 Pipeline 6h 周期，可以考虑将 L2 (Session) 的聚合前移到会话结束时。

### 优势 2: Ebbinghaus 遗忘曲线

MacLaw 的 `Strength` 字段 + `batchDecayAndMark` 实现了基于遗忘曲线的记忆衰减。Mem0 和 Graphiti 都没有这个机制——它们的记忆不会自然遗忘。

**建议**：这是一个好的设计，但需要与 UPDATE/DELETE 操作配合。当前的衰减是纯时间驱动的（不访问就衰减），应该加入"被 UPDATE 替代"的加速衰减。

### 优势 3: 多租户隔离 (OwnerID)

MacLaw 的 OwnerID 字段提供了 maclawsrv 多用户场景的记忆隔离。Mem0 的开源版本没有内置的多租户支持（云服务版有）。Graphiti/Zep 作为云服务天然支持多租户。

### 优势 4: 安全防护（注入扫描、密钥脱敏）

MacLaw 在记忆写入时做 prompt injection 检测和密钥脱敏。Mem0 和 Graphiti 的论文中没有提到这些安全机制。

---

## 五、优先级排序的改进路线图

### 第一优先级（P0）——解决"记忆不完整"和"记忆过时"

| # | 改进 | 来源 | 预估工作量 | 影响 |
|---|------|------|-----------|------|
| 1 | **在线增量提取管线**：每轮对话后异步提取事实 | Mem0 核心机制 | 3-5 天 | 解决"对话内容在截断前未进入长期记忆" |
| 2 | **四操作更新**（ADD/UPDATE/DELETE/NOOP） | Mem0 核心机制 | 2-3 天 | 解决"矛盾信息共存"和"记忆只增不减" |
| 3 | **提升 maxItems 到 2000** | Mem0/Graphiti 无上限 | 0.5 天 | 缓解容量瓶颈 |

### 第二优先级（P1）——解决"时间推理弱"和"关系推理弱"

| # | 改进 | 来源 | 预估工作量 | 影响 |
|---|------|------|-----------|------|
| 4 | **事实时间标注**（ValidAt/InvalidAt） | Graphiti 双时间线 | 2-3 天 | 支持"什么时候"类问题 |
| 5 | **实体-关系三元组提取**（Phase A: tag 方式） | Graphiti + Mem0^g | 2-3 天 | 改善多跳推理 |
| 6 | **主题聚类摘要** | Graphiti Community | 1-2 天 | 提供全局视角 |

### 第三优先级（P2）——架构演进

| # | 改进 | 来源 | 预估工作量 | 影响 |
|---|------|------|-----------|------|
| 7 | **实体索引**（Phase B） | Graphiti Semantic Entity | 3-4 天 | 支持实体中心查询 |
| 8 | **BFS 图遍历检索** | Graphiti 三路搜索 | 1-2 天 | 发现上下文相关的记忆 |
| 9 | **SQLite 替代 JSON** | 生产化需求 | 5-7 天 | 解决容量和写放大 |

---

## 六、具体实现建议

### 6.1 在线增量提取管线（最高优先级）

```
对话轮次完成
  │
  ├─ 同步路径：正常返回响应给用户
  │
  └─ 异步 goroutine：
      1. 从最近 5 条对话历史构建提取 prompt
      2. LLM 提取事实列表 [{content, category, tags}]
      3. 对每个事实：
         a. 向量相似度 top-5 检索已有记忆
         b. LLM 判断操作：ADD / UPDATE / DELETE / NOOP
         c. 执行操作
      4. 冷却 5 分钟（防止密集对话中过度调用 LLM）
```

**与已有机制的关系**：
- 替代 `KnowledgeExtractor` 的 1h 冷却被动提取
- 复用 `SaveWithContext` 的上下文标签增强
- 复用 `pendingDedup` 的异步语义去重框架
- `KnowledgeExtractor` 保留为 fallback（LLM 不可用时）

### 6.2 四操作更新（与 6.1 配合）

```go
// corelib/memory/store.go

type MemoryOperation string
const (
    OpAdd    MemoryOperation = "add"
    OpUpdate MemoryOperation = "update"  
    OpDelete MemoryOperation = "delete"
    OpNoop   MemoryOperation = "noop"
)

// ClassifyOperation 使用 LLM 判断新事实与已有记忆的关系
func (s *Store) ClassifyOperation(newFact string, similarEntries []Entry) (MemoryOperation, string, error) {
    // 构建 prompt：新事实 + top-5 相似记忆
    // LLM 返回：操作类型 + 目标 entry ID（UPDATE/DELETE 时）
    // 复用已有的 LLMChatCaller 接口
}
```

### 6.3 事实时间标注

```go
// corelib/memory/types.go — Entry 新增字段

type Entry struct {
    // ... 现有字段 ...
    
    // ValidAt is when this fact became true in the real world.
    // nil means "unknown" or "always true".
    ValidAt   *time.Time `json:"valid_at,omitempty"`
    
    // InvalidAt is when this fact stopped being true.
    // nil means "still true" or "unknown".
    InvalidAt *time.Time `json:"invalid_at,omitempty"`
}
```

提取方式：复用 Graphiti 的 temporal extraction prompt 思路，在在线提取管线中，对每个提取的事实，额外调用 LLM 提取时间信息。参考时间戳从消息的发送时间获取。

---

## 七、总结

| 维度 | MacLaw 现状 | Mem0 的启示 | Graphiti 的启示 | 建议优先级 |
|------|------------|------------|----------------|-----------|
| 写入时机 | 被动（1h 冷却 / 会话过期） | **实时增量**（每对消息） | 实时增量（每条消息） | **P0** |
| 更新策略 | 只 ADD | **ADD/UPDATE/DELETE/NOOP** | 实体解析 + 边失效 | **P0** |
| 容量 | 500 条硬上限 | 无上限 | 无上限 | **P0** |
| 时间建模 | TiMem 层级（聚合用） | 无 | **双时间线（事实有效期）** | P1 |
| 图结构 | Entry 相关性网络 | 有向标签图 | **实体-关系三元组** | P1 |
| 全局视角 | TiMem L5 画像 | 无 | **Community 社区摘要** | P1 |
| 检索方式 | BM25+Vec+Tag RRF | 向量相似度 | BM25+Vec+**BFS 图遍历** | P2 |
| 遗忘机制 | ✅ Ebbinghaus 衰减 | ❌ | ❌ | 保持优势 |
| 多租户 | ✅ OwnerID | ❌（开源版） | ✅（云服务） | 保持优势 |
| 安全防护 | ✅ 注入扫描+密钥脱敏 | ❌ | ❌ | 保持优势 |

**核心结论**：MacLaw 的记忆系统在架构复杂度上已经超过 Mem0（有 TiMem、图、遗忘曲线等），但在**数据流转的及时性**（被动 vs 实时）和**记忆一致性维护**（只 ADD vs 四操作）上存在根本性差距。这两个问题是 Mem0 论文的核心贡献，也是 MacLaw 最应该借鉴的。Graphiti 的双时间线和实体-关系图谱是更高级的能力，可以作为第二阶段的改进目标。


---

## 八、实施状态（2026-04-30）

### 已完成的改进

| # | 改进 | 文件 | 状态 |
|---|------|------|------|
| 1 | **在线增量提取管线**（Mem0 核心机制） | `corelib/memory/online_extractor.go` | ✅ 已实现 |
| 2 | **四操作更新**（ADD/UPDATE/DELETE/NOOP） | `corelib/memory/online_extractor.go` + `types.go` | ✅ 已实现 |
| 3 | **提升 maxItems 到 2000** | `corelib/memory/store.go` | ✅ 已实现 |
| 4 | **事实时间标注**（ValidAt/InvalidAt） | `corelib/memory/types.go` | ✅ 已实现 |
| 5 | **实体-关系三元组提取**（Phase A: tag + Entities 字段） | `corelib/memory/types.go` + `online_extractor.go` | ✅ 已实现 |
| 6 | **实体索引**（Phase B: entity name → entry ID） | `corelib/memory/entity_index.go` | ✅ 已实现 |
| 7 | **主题聚类摘要**（Graphiti Community 轻量替代） | `corelib/memory/topic_cluster.go` | ✅ 已实现 |
| 8 | **BFS 图遍历检索**（Graphiti φ_bfs） | `corelib/memory/store.go` RecallWithBFS | ✅ 已实现 |
| 9 | **BM25 索引增强**（实体名纳入索引） | `corelib/memory/bm25.go` | ✅ 已实现 |
| 10 | **KnowledgeExtractor 冷却降低**（1h → 10min） | `corelib/memory/knowledge_extractor.go` | ✅ 已实现 |
| 11 | **Pipeline 集成主题聚类** | `corelib/memory/pipeline.go` | ✅ 已实现 |

### 新增文件

| 文件 | 说明 | 行数 |
|------|------|------|
| `corelib/memory/online_extractor.go` | Mem0 风格在线增量提取管线 | ~380 行 |
| `corelib/memory/entity_index.go` | 实体名 → Entry ID 索引 | ~170 行 |
| `corelib/memory/topic_cluster.go` | 基于 tag 的主题聚类 | ~200 行 |
| `corelib/memory/online_extractor_test.go` | 在线提取管线测试 | ~220 行 |
| `corelib/memory/entity_index_test.go` | 实体索引测试 | ~130 行 |
| `corelib/memory/topic_cluster_test.go` | 主题聚类测试 | ~100 行 |

### 修改文件

| 文件 | 改动 |
|------|------|
| `corelib/memory/types.go` | Entry 新增 ValidAt/InvalidAt/Entities 字段 + MemoryOperation/ExtractedFact/ClassifiedOperation 类型 |
| `corelib/memory/store.go` | maxItems 500→2000 + entityIndex/topicClusterer/onlineExtractor 字段 + EntityIndex/RecallWithBFS/FindByEntity 方法 |
| `corelib/memory/bm25.go` | entryToDoc 纳入 Entities 字段 |
| `corelib/memory/knowledge_extractor.go` | cooldown 1h→10min |
| `corelib/memory/pipeline.go` | RunOnce 新增 Step 6 主题聚类 |
| `corelib/memory/store_archive_property_test.go` | 适配 maxItems=2000 |
| `corelib/memory/substring_match_test.go` | 适配 maxItems=2000 |
| `corelib/memory/backward_compat_test.go` | 适配 maxItems=2000 |

### 测试结果

- 所有 memory 包测试通过（含新增 10 个测试 + 所有现有测试）
- GUI 编译通过
- TUI 编译通过
- corelib/agent 编译通过

### 待 GUI/TUI 侧接线

在线提取管线（`OnlineExtractor`）已在 corelib 层完整实现，但需要 GUI/TUI 侧接线才能在实际对话中触发：

1. **GUI 侧**（`gui/im_message_handler.go`）：在 `runAgentLoop` 正常退出路径中，异步调用 `store.OnlineExtractor().ExtractAndIntegrate()`
2. **TUI 侧**（`tui/agent_handler.go`）：同上
3. **初始化**（`gui/app.go` + `tui/app.go`）：创建 `OnlineExtractor` 并通过 `store.SetOnlineExtractor()` 注入

实体索引（`EntityIndex`）和 BFS 检索（`RecallWithBFS`）已在 Store 中自动初始化和维护，无需额外接线。主题聚类（`TopicClusterer`）已集成到 Pipeline 的 6h 周期中。
