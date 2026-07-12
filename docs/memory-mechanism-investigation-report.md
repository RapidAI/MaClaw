# MacLaw 记忆管理机制调查报告

**调查日期**: 2026-04-25  
**调查范围**: corelib/memory 包完整实现 + GUI 侧集成

---

## 一、架构概览

### 1.1 三层存储架构

```
┌─────────────────────────────────────────────────────────────────┐
│                     对话历史 (ConversationMemory)                 │
│  - 短期存储：当前会话的完整对话                                    │
│  - 截断策略：trimHistoryWithSummary (两层截断 + LLM 摘要)          │
│  - 持久化：~/.maclaw/data/conversations/{userID}.json            │
└─────────────────────────────────────────────────────────────────┘
                              ↓ 知识提取 / 归档
┌─────────────────────────────────────────────────────────────────┐
│                     长期记忆 (memory.Store)                       │
│  - 容量上限：500 条 entries                                       │
│  - 分区存储：5 个分区文件 (identity/user/project/episodic/profile) │
│  - 索引：BM25 + Vector + Graph + TMT (Temporal Memory Tree)      │
│  - 召回：RecallDynamic (RRF 融合 + Memory Stream 评分)            │
└─────────────────────────────────────────────────────────────────┘
                              ↓ GC 驱逐
┌─────────────────────────────────────────────────────────────────┐
│                     冷存储 (ArchiveStore)                         │
│  - 容量上限：1000 条 entries                                      │
│  - 持久化：~/.maclaw/data/archive.json                           │
│  - 复活机制：RunGC 时根据活跃记忆的 tags/categories 召回相关归档   │
└─────────────────────────────────────────────────────────────────┘
```

### 1.2 两条注入路径

| 路径 | 触发时机 | 内容 | 实现位置 |
|------|---------|------|---------|
| **静态注入** | 每次 LLM 调用 | self_identity + user_fact 摘要 | `appendMemorySection()` |
| **动态注入** | 每次 LLM 调用 | 基于用户消息的 RecallDynamic 结果 | `appendProactiveRecall()` |

### 1.3 后台维护管线 (Pipeline)

每 6 小时执行一次，包含 4 个组件：

| 组件 | 功能 | 关键方法 |
|------|------|---------|
| **Compressor** | 子串去重 + LLM 语义合并 + CompactForm 回填 + GC | `Run()`, `RunGC()` |
| **Archiver** | 从过期对话中提取摘要 | `Archive()` |
| **KnowledgeExtractor** | 从对话中提取结构化知识点 | `Extract()` |
| **Consolidator** | TiMem 风格分层整合 (L1-L5) | `ConsolidateSegment()`, `ConsolidateLevel()` |

---

## 二、已实施改进验证 (7 个 Phase)

### Phase 1: 高价值产出物实时沉淀 
**实现位置**: `gui/workflow_artifact_saver.go`

**机制**:
- `WorkflowEngine.SavePhaseOutput()` 在保存阶段产出物后，通过 `ArtifactSaver` 接口沉淀到长期记忆
- 新增 `CategoryTaskArtifact` 类别 (TierSemantic, ScopeProject, ImportanceWeight=3.0)
- 去重机制：ContentHash 精确去重 + phaseTag 更新去重

**验证结果**: 代码完整实现

### Phase 2: 上下文感知的标签增强 
**实现位置**: `corelib/memory/store.go`

**机制**:
- `SaveWithContext(entry, contextHint)` 从对话上下文中提取实体作为额外 tags
- `tagExactMatchBoost()` 当 query 实体与 entry tag 精确匹配时给予 +5.0 分 boost (上限 10.0)

**验证结果**: 代码完整实现

```go
// store.go:131-140
if contextHint != "" {
    ctxExpanded := ExpandQuery(contextHint)
    if len(ctxExpanded.Entities) > 0 {
        entry.Tags = mergeTags(entry.Tags, ctxExpanded.Entities)
    }
}
```

### Phase 3: 写入时增量子串去重 
**实现位置**: `corelib/memory/store.go`

**机制**:
- `findSubstringDuplicate()` 只扫描最近 50 条 entries
- 双向子串检查：新内容包含已有 OR 已有包含新内容
- 匹配时合并 tags，保留较长内容

**验证结果**: 代码完整实现

### Phase 4: 产出物可召回索引 
**实现位置**: `corelib/agent/tool_memory.go`

**机制**:
- memory tool 的 recall action 对 `task_artifact` 类别读取 SourceURL 指向的文件全文 (最多 5000 字符)
- proactive recall 仍只注入 200 字符摘要

**验证结果**: 代码完整实现

### Phase 5: 按 Category 分区存储 
**实现位置**: `corelib/memory/partition.go`

**机制**:
- 5 个分区组：identity / user / project / episodic / profile
- `flushDirty()` 只写入 dirty 的分区文件
- 迁移策略：≥100 条记忆时自动从单文件迁移到分区文件

**验证结果**: 代码完整实现

```go
// partition.go:15-21
var partitionGroups = map[string][]Category{
    "identity": {CategorySelfIdentity},
    "user":     {CategoryUserFact, CategoryUser, CategoryPreference, CategoryInstruction, CategoryFeedback},
    "project":  {CategoryProjectKnowledge, CategoryProject, CategoryReference, CategoryTaskArtifact},
    "episodic": {CategoryConversationSummary, CategorySessionCheckpoint},
    "profile":  {CategoryProfile},
}
```

### Phase 6: maclawsrv 多用户记忆隔离 
**实现位置**: `corelib/memory/store.go`, `corelib/memory/types.go`

**机制**:
- `Entry.OwnerID` 字段：空字符串表示共享，非空表示用户专属
- `SaveForUser(entry, ownerID)` 设置 OwnerID
- `RecallDynamic()` 的 ownerID 可变参数过滤
- `graphExpand()` 后二次过滤

**验证结果**: 代码完整实现

```go
// store.go:1024-1027
if filterOwner != "" && e.OwnerID != "" && e.OwnerID != filterOwner {
    continue
}
```

### Phase 7: 对话历史智能压缩 
**实现位置**: `gui/im_conversation_trim.go`

**机制**:
- `trimHistoryWithSummary()` 接受 summarizer 和 memorySink 回调
- 被截断的 entries 用 LLM 摘要替代静态占位符
- 实质性 assistant 消息 (>500 rune) 沉淀为 task_artifact

**验证结果**: 代码完整实现

---

## 三、发现的问题 (5 个 OwnerID 相关)

### 问题 1 (P1): Consolidator 缺少 OwnerID 处理

**位置**: `corelib/memory/consolidator.go`

**根因分析**:

`ConsolidateSegment()` 创建的 entry 没有设置 OwnerID：

```go
// consolidator.go:60-68
entry := Entry{
    Content:  content,
    Category: CategoryConversationSummary,
    Tags:     []string{"tmt", "L1", "segment"},
    Level:    LevelSegment,
    Interval: &interval,
    // 缺少 OwnerID
}
if err := c.store.Save(entry); err != nil { ... }
```

`ConsolidateLevel()` 整合多个 entry 时同样没有处理 OwnerID：

```go
// consolidator.go:113-121
entry := Entry{
    Content:  content,
    Category: cat,
    Tags:     []string{"tmt", fmt.Sprintf("L%d", level), level.String()},
    Level:    level,
    Interval: &window,
    // 缺少 OwnerID
}
if err := c.store.Save(entry); err != nil { ... }
```

**影响范围**:
- **GUI/TUI (单用户)**: 无影响，所有记忆天然属于同一用户
- **maclawsrv (多租户)**: 整合后的记忆 OwnerID 为空，对所有用户可见

**机制性修复方案**:

```go
// 方案：ConsolidateSegment 和 ConsolidateLevel 需要接受 ownerID 参数

func (c *Consolidator) ConsolidateSegment(ctx context.Context, userMsg, assistantMsg string, turnTime time.Time, ownerID string) (*ConsolidationResult, error) {
    // ...
    entry := Entry{
        Content:  content,
        Category: CategoryConversationSummary,
        Tags:     []string{"tmt", "L1", "segment"},
        Level:    LevelSegment,
        Interval: &interval,
        OwnerID:  ownerID,  // 设置 OwnerID
    }
    // ...
}

func (c *Consolidator) ConsolidateLevel(ctx context.Context, level TemporalLevel, window TimeInterval, ownerID string) (*ConsolidationResult, error) {
    // ...
    // 整合时需要验证所有 child entries 的 OwnerID 一致
    // 或者只整合同一 OwnerID 的 entries
    // ...
}
```

---

### 问题 2 (P1): Archiver 缺少 OwnerID 处理

**位置**: `corelib/memory/archiver.go`

**根因分析**:

`Archive()` 创建的 `conversation_summary` entry 没有设置 OwnerID：

```go
// archiver.go:68-76
entry := Entry{
    Content:  summary,
    Category: CategoryConversationSummary,
    Tags:     tags,
    // 缺少 OwnerID
}
return a.store.Save(entry)  // 应该用 SaveForUser(entry, userID)
```

注意：`Archive()` 方法签名已经有 `userID` 参数，但没有使用它设置 OwnerID。

**影响范围**:
- **GUI/TUI (单用户)**: 无影响
- **maclawsrv (多租户)**: 归档的对话摘要对所有用户可见

**机制性修复方案**:

```go
// archiver.go:68-76
entry := Entry{
    Content:  summary,
    Category: CategoryConversationSummary,
    Tags:     tags,
    OwnerID:  userID,  // 使用已有的 userID 参数
}
return a.store.Save(entry)
```

---

### 问题 3 (P2): ArchiveStore.FindRelevant 缺少 OwnerID 过滤

**位置**: `corelib/memory/archive.go`

**根因分析**:

`FindRelevant()` 按 tags/categories 查找归档记忆，不检查 OwnerID：

```go
// archive.go:82-107
func (a *ArchiveStore) FindRelevant(tags []string, categories []Category, limit int) []Entry {
    // ...
    for _, e := range a.entries {
        if len(result) >= limit {
            break
        }
        // Match by category.
        if catSet[e.Category] {
            result = append(result, e)
            continue
        }
        // Match by tag overlap.
        for _, et := range e.Tags {
            if tagSet[strings.ToLower(et)] {
                result = append(result, e)
                break
            }
        }
        // 没有 OwnerID 过滤
    }
    return result
}
```

`RunGC()` 的 revive 逻辑调用 `FindRelevant()` 时可能跨用户恢复记忆：

```go
// compressor.go:810-825
relevant := mc.store.archive.FindRelevant(tags, cats, 10)
var revived []Entry
for _, re := range relevant {
    // ...
    removed, err := mc.store.archive.Remove(re.ID)
    // ...
    revived = append(revived, *removed)
}
// revived entries 可能属于其他用户
```

**影响范围**:
- **GUI/TUI (单用户)**: 无影响
- **maclawsrv (多租户)**: GC revive 可能将用户 A 的归档记忆恢复到用户 B 的活跃记忆中

**机制性修复方案**:

```go
// 方案 1：FindRelevant 新增 ownerID 参数
func (a *ArchiveStore) FindRelevant(tags []string, categories []Category, limit int, ownerID string) []Entry {
    // ...
    for _, e := range a.entries {
        // Multi-tenant isolation
        if ownerID != "" && e.OwnerID != "" && e.OwnerID != ownerID {
            continue
        }
        // ...
    }
}

// 方案 2：RunGC 调用时传递 ownerID（需要 RunGC 也接受 ownerID 参数）
// 但 RunGC 是全局操作，不应该按用户执行
// 更好的方案是在 revive 后检查 OwnerID 一致性
```

---

## 四、KnowledgeExtractor 验证 (无问题)

**位置**: `corelib/memory/knowledge_extractor.go`

`Extract()` 正确设置了 OwnerID：

```go
// knowledge_extractor.go (已验证)
func (ke *KnowledgeExtractor) Extract(userID string, msgs []ConversationEntry) error {
    // ...
    for _, kp := range knowledgePoints {
        entry := Entry{
            Content:  kp.Content,
            Category: kp.Category,
            Tags:     kp.Tags,
            OwnerID:  userID,  // 正确设置
        }
        if err := ke.store.Save(entry); err != nil { ... }
    }
}
```

---

## 五、Compressor 验证

### 5.1 Store.Update() 保留 OwnerID 
`Store.Update()` 方法只更新 `Content`、`Category`、`Tags`、`CompactForm`、`ContentHash`、`UpdatedAt`、`Stale` 字段，**不修改 `OwnerID`**。

这意味着 `mergeBatch()` 在合并时：
1. 选择 survivor entry（保留其 OwnerID）
2. 调用 `Update()` 只更新内容，不改变 OwnerID
3. 删除其他被合并的 entries

### 5.2 问题 4 (P1): mergeSemanticDuplicates 不按 OwnerID 分组

**位置**: `corelib/memory/compressor.go:355-375`

**根因分析**:

`mergeSemanticDuplicates()` 按 Category 分组，但不按 OwnerID 分组：

```go
// compressor.go:355-375
for cat := range catSet {
    // ...
    var entries []Entry
    for _, e := range mc.store.entries {
        if e.Category == cat && !e.Pinned {
            entries = append(entries, e)  // 不检查 OwnerID
        }
    }
    // ...
}
```

**影响范围**:
- **GUI/TUI (单用户)**: 无影响，所有记忆天然属于同一用户
- **maclawsrv (多租户)**: 
  - 用户 A 的记忆和用户 B 的记忆可能被合并到同一个 batch
  - LLM 可能将它们判定为语义重复并合并
  - 合并后的 entry 只保留 survivor 的 OwnerID
  - 另一个用户的记忆内容被"吸收"到 survivor 中

**机制性修复方案**:

```go
// 方案：按 (Category, OwnerID) 二元组分组

func (mc *Compressor) mergeSemanticDuplicates(ctx context.Context) (int, error) {
    totalMerged := 0

    mc.store.mu.RLock()
    // 按 (Category, OwnerID) 分组
    type groupKey struct {
        cat     Category
        ownerID string
    }
    groups := make(map[groupKey][]Entry)
    for _, e := range mc.store.entries {
        if e.Category.IsProtected() || e.Pinned {
            continue
        }
        key := groupKey{cat: e.Category, ownerID: e.OwnerID}
        groups[key] = append(groups[key], e)
    }
    mc.store.mu.RUnlock()

    for _, entries := range groups {
        if len(entries) < 2 {
            continue
        }
        // ... 对同一 (Category, OwnerID) 组内的 entries 进行合并
    }
    return totalMerged, nil
}
```

### 5.3 问题 5 (P1): dedup() 不检查 OwnerID

**位置**: `corelib/memory/compressor.go:260-300`

**根因分析**:

`dedup()` 在比较两个 entry 是否重复时，只检查 Category 是否相同，不检查 OwnerID：

```go
// compressor.go:272-285
for i := 0; i < n; i++ {
    // ...
    for j := i + 1; j < n; j++ {
        // ...
        if !isDuplicateLower(mc.store.entries[i], mc.store.entries[j], lower[i], lower[j]) {
            continue
        }
        loser := pickLoser(mc.store.entries, i, j)
        remove[loser] = true  // 不检查 OwnerID
    }
}

// isDuplicateLower 只检查 Category，不检查 OwnerID
func isDuplicateLower(a, b Entry, ca, cb string) bool {
    if ca == cb {
        return true
    }
    if a.Category == b.Category {  // 只检查 Category
        // ...
    }
    return false
}
```

**影响范围**:
- **GUI/TUI (单用户)**: 无影响
- **maclawsrv (多租户)**: 
  - 用户 A 和用户 B 如果有相同内容的记忆（如都记录了"PostgreSQL 16"），其中一个会被删除
  - 被删除的用户会丢失这条记忆

**机制性修复方案**:

```go
// 方案：isDuplicateLower 新增 OwnerID 检查

func isDuplicateLower(a, b Entry, ca, cb string) bool {
    // 不同用户的记忆不视为重复
    if a.OwnerID != "" && b.OwnerID != "" && a.OwnerID != b.OwnerID {
        return false
    }
    
    if ca == cb {
        return true
    }
    if a.Category == b.Category {
        // ...
    }
    return false
}
```

---

## 六、改进建议优先级

| 优先级 | 问题 | 影响 | 修复复杂度 |
|--------|------|------|-----------|
| P1 | Consolidator 缺少 OwnerID | maclawsrv 多租户隔离失效 | 中等 (需要修改方法签名) |
| P1 | Archiver 缺少 OwnerID | maclawsrv 多租户隔离失效 | 低 (只需一行代码) |
| P1 | mergeSemanticDuplicates 不按 OwnerID 分组 | 跨用户记忆合并 | 中等 (需要修改分组逻辑) |
| P1 | dedup() 不检查 OwnerID | 跨用户记忆删除 | 低 (只需几行代码) |
| P2 | ArchiveStore.FindRelevant 缺少 OwnerID | GC revive 可能跨用户 | 中等 (需要修改方法签名 + 调用链) |

---

## 七、其他观察

### 7.1 RecallDynamic 的 OwnerID 处理完整

`RecallDynamic()` 已正确实现多租户隔离：
1. 主循环过滤：`filterOwner != "" && e.OwnerID != "" && e.OwnerID != filterOwner`
2. graphExpand 后二次过滤：防止图谱边拉入其他用户的记忆

### 7.2 分区存储不影响 OwnerID

分区是按 Category 划分的，与 OwnerID 正交。同一分区文件中可以有不同 OwnerID 的 entries。

### 7.3 Pipeline 组件的 OwnerID 处理状态

| 组件 | OwnerID 处理 | 状态 |
|------|-------------|------|
| Compressor.dedup() | 不检查 OwnerID | 问题 5 |
| Compressor.mergeSemanticDuplicates() | 不按 OwnerID 分组 | 问题 4 |
| Compressor.RunGC() | revive 时不检查 OwnerID | 问题 3 |
| Archiver.Archive() | 不设置 OwnerID | 问题 2 |
| KnowledgeExtractor.Extract() | 正确设置 OwnerID | |
| Consolidator.ConsolidateSegment() | 不设置 OwnerID | 问题 1 |
| Consolidator.ConsolidateLevel() | 不设置 OwnerID | 问题 1 |

---

## 八、结论

MacLaw 的记忆管理机制在 **单用户场景 (GUI/TUI)** 下运行良好，7 个改进 Phase 均已正确实施。

在 **多租户场景 (maclawsrv)** 下存在 5 个 OwnerID 相关的隔离缺口，需要机制性修复：

1. **Consolidator**: 整合后的记忆缺少 OwnerID，对所有用户可见
2. **Archiver**: 归档的对话摘要缺少 OwnerID，对所有用户可见
3. **ArchiveStore.FindRelevant**: GC revive 可能跨用户恢复记忆
4. **mergeSemanticDuplicates**: 不按 OwnerID 分组，可能跨用户合并记忆
5. **dedup()**: 不检查 OwnerID，可能跨用户删除重复记忆

这些问题的修复应遵循 `maclaw-improvements.md` 的原则：**从机制上分析与修复，而不是做 workaround**。建议的修复方案是在相关方法中正确传递和设置 OwnerID，而非在调用点做特殊处理。

### 修复优先级建议

1. **立即修复 (P1)**：
   - Archiver.Archive() — 只需一行代码
   - dedup() — 只需几行代码
   - Consolidator — 需要修改方法签名
   - mergeSemanticDuplicates — 需要修改分组逻辑

2. **后续修复 (P2)**：
   - ArchiveStore.FindRelevant — 需要修改方法签名和调用链
