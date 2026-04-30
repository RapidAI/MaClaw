# 记忆管理体系架构审计

## 审计方法

系统性检查所有记忆写入路径、召回路径、压缩路径、清理路径，寻找类似"两条路径做同一件事但预算不一致"的架构问题。

---

## 发现的架构问题

### 问题 1 (P1): 写入路径 OwnerID 不一致——9 个 Save 调用中 5 个不设 OwnerID

**现状**：#67 引入了 `Entry.OwnerID` 字段用于 maclawsrv 多租户隔离。但只有部分写入路径设置了 OwnerID：

| 写入路径 | 文件 | 设置 OwnerID? | 影响 |
|---------|------|-------------|------|
| `conversation_archiver.Archive()` | gui/conversation_archiver.go | ✅ `userID` | |
| `knowledge_extractor.Extract()` | corelib/memory/knowledge_extractor.go | ✅ `userID` | |
| `online_extractor.classifyAndApply()` | corelib/memory/online_extractor.go | ✅ `ownerID` param | |
| iWorkerCenter handler | iWorkerCenter/.../handler.go | ✅ `SaveForUser` | |
| `sedimentTaskEntry()` | gui/im_task_sediment.go | ❌ | maclawsrv: 任务沉淀跨用户可见 |
| `workflow_artifact_saver.SaveArtifact()` | gui/workflow_artifact_saver.go | ❌ | maclawsrv: 工作流产出物跨用户可见 |
| `memorySink` in `saveConversationHistoryTimed` | gui/im_message_handler.go | ❌ | maclawsrv: 截断沉淀跨用户可见 |
| `toolMemory` save (GUI) | gui/im_tools_misc.go | ❌ | maclawsrv: LLM 保存的记忆跨用户可见 |
| `ToolMemory` save (corelib) | corelib/agent/tool_memory.go | ❌ | TUI: 无影响（单用户）|
| `session_checkpoint` | gui/session_checkpoint.go | ❌ | maclawsrv: 编程会话快照跨用户可见 |
| TUI CLI `memorySave` | tui/commands/memory.go | ❌ | TUI: 无影响（单用户）|
| Wails `SaveMemory` | gui/app_wails_bindings.go | ❌ | GUI: 无影响（单用户）|

**根因**：#67 只修复了 archiver、knowledge_extractor、online_extractor、consolidator 四个路径。其余路径在 GUI/TUI 单用户场景下 OwnerID 为空（共享），不影响功能。但在 maclawsrv 多租户场景下，这些路径写入的记忆对所有用户可见。

**影响范围**：仅 maclawsrv。GUI/TUI 单用户不受影响。

**修复方向**：所有 GUI 侧的 `Save()` 调用需要传递 `userID`。最干净的方式是让 `IMMessageHandler` 在调用 `Save` 时统一注入 OwnerID，而不是在每个调用点手动设置。

---

### 问题 2 (P1): conversation 压缩有三条独立路径，职责重叠

**现状**（删除 `compactHistory` 后仍有三条）：

| # | 函数 | 文件 | 操作对象 | 触发时机 | 触发条件 |
|---|------|------|---------|---------|---------|
| A | `trimConversation` | gui/im_conversation_trim.go | `conversation []interface{}` | loop 内每次迭代 | token > effectiveTokenLimit |
| B | `autoCompressConversation` | gui/im_compress.go | `conversation []interface{}` | loop 内每次迭代（A 之前） | token > 80% of MaxContextTokens |
| C | `trimHistoryWithSummary` | gui/im_conversation_trim.go | `history []ConversationEntry` | post-loop Save 前 | entry 数 > limit OR token > limit |

A 和 B 操作同一个 `conversation`，在同一个迭代中顺序调用。B 先跑（`autoCompressConversation`），然后 A 跑（`trimConversation`）。

**问题**：B 的 `autoCompressConversation` 在有 tool_calls 时**完全跳过**（因为 `ctxcompress.Message` 丢失 `reasoning_content` 和 `tool_calls` 字段）。而 agent loop 中几乎每次迭代都有 tool_calls。这意味着 B 在实际使用中**几乎从不执行**——它是死代码。

同时，`corelib/agent/compress.go` 中有一个 `AutoCompressConversation`（大写导出版本），与 `gui/im_compress.go` 中的 `autoCompressConversation`（小写版本）是**完全重复的实现**。两者的逻辑几乎相同，但 GUI 版本多了 tool_calls 跳过检查。

**根因**：`corelib/agent/compress.go` 是 agent-unification 计划的产物（从 GUI 提取到 corelib），但 GUI 侧的原始实现没有被删除。两份代码并存。

---

### 问题 3 (P2): `corelib/agent.TrimHistory` 是死代码

`corelib/agent/conversation_trim.go` 中的 `TrimHistory` 函数（大写导出）在非测试代码中没有任何调用者。GUI 使用自己的 `gui/im_conversation_trim.go` 中的 `trimHistory`（小写）→ `trimHistoryWithSummary`。TUI 使用 `corelib/agent.RunLoop` 中的 `TrimConversation`（操作 `[]interface{}`），不操作 `[]ConversationEntry`。

这是 agent-unification 迁移的遗留——`TrimHistory` 被提取到 corelib 但 GUI 没有切换到使用它。

---

### 问题 4 (P2): `corelib/agent.TrimConversation` 与 `gui/trimConversation` 重复

`corelib/agent/conversation_trim.go:TrimConversation` 和 `gui/im_conversation_trim.go:trimConversation` 是**几乎相同的函数**（相同的签名、相同的逻辑）。GUI 版本是原始实现，corelib 版本是 agent-unification 迁移的副本。

`corelib/agent.TrimConversation` 只被 `corelib/agent/compress.go:AutoCompressConversation` 的 fallback 路径调用。GUI 的 `trimConversation` 被 agent loop 和 `context_compressor.go` 调用。

---

### 问题 5 (P1): RecallDynamic 硬编码过滤 CategoryUserFact——与 proactive recall 的过滤不一致

**现状**：

`RecallDynamic`（`corelib/memory/store.go:1218`）硬编码跳过 `CategoryUserFact`：
```go
if e.Category == CategoryUserFact {
    continue
}
```

`appendProactiveRecall`（`gui/im_system_prompt.go:800`）额外过滤：
```go
if canonical == corememory.CategoryUserFact || canonical == corememory.CategorySelfIdentity {
    continue
}
if e.Category == corememory.CategorySessionCheckpoint || e.Category == corememory.CategoryConversationSummary {
    continue
}
```

**问题**：`RecallDynamic` 过滤了 `user_fact`，但没有过滤 `self_identity`。`appendProactiveRecall` 在 `RecallDynamic` 之后再过滤 `self_identity`。这意味着 `RecallDynamic` 返回的 15 条结果中可能包含 `self_identity` 条目，占据了有限的 15 条名额，然后被 `appendProactiveRecall` 丢弃。

同样，`session_checkpoint` 和 `conversation_summary` 在 `RecallDynamic` 中不被过滤，但在 `appendProactiveRecall` 中被过滤。它们占据 `RecallDynamic` 的 15 条名额后被丢弃。

**根因**：过滤逻辑分散在两个层——`RecallDynamic`（通用召回）和 `appendProactiveRecall`（消费方）。`RecallDynamic` 不知道消费方会过滤什么，所以返回了消费方不需要的条目，浪费了名额。

**影响**：当记忆库中有多条 `self_identity`/`session_checkpoint`/`conversation_summary` 时，`RecallDynamic` 的 15 条名额被这些条目占据，真正有用的 `project_knowledge`/`task_artifact` 被挤出。

**修复方向**：`RecallDynamic` 接受一个 `excludeCategories []Category` 参数，让消费方声明不需要的类别。`appendProactiveRecall` 传入 `[user_fact, self_identity, session_checkpoint, conversation_summary]`。`toolMemory` recall 不传（返回所有类别）。

---

### 问题 6 (P2): `gui/im_compress.go` 和 `corelib/agent/compress.go` 的转换函数完全重复

两个文件各自定义了相同的转换函数：

| gui/im_compress.go | corelib/agent/compress.go |
|--------------------|-----------------------|
| `conversationToContextMessages` | `ConversationToContextMessages` |
| `interfaceSliceToContextMessages` | `InterfaceSliceToContextMessages` |
| `contextMessagesToConversation` | `ContextMessagesToConversation` |
| `contextMessagesToInterfaceSlice` | `ContextMessagesToInterfaceSlice` |
| `entryContentToString` | `EntryContentToString` |
| `extractRoleContent` | `ExtractRoleContent` |
| `makeSummarizeCallback` | `MakeSummarizeCallback` |

GUI 版本是小写（包内可见），corelib 版本是大写（导出）。逻辑完全相同。

---

## 优先级排序

| # | 优先级 | 问题 | 修复复杂度 | 影响 |
|---|--------|------|-----------|------|
| 1 | P1 | OwnerID 不一致 | 中 | maclawsrv 多租户记忆泄漏 |
| 5 | P1 | RecallDynamic 名额浪费 | 低 | 召回质量下降 |
| 2 | P1 | autoCompressConversation 死代码 | 低 | 代码复杂度 |
| 3 | P2 | corelib/agent.TrimHistory 死代码 | 低 | 代码复杂度 |
| 4 | P2 | TrimConversation 重复 | 中 | 代码复杂度 |
| 6 | P2 | 转换函数重复 | 中 | 代码复杂度 |


---

## 第二轮审计：数据生命周期 + 并发安全 + 持久化

### 审计范围

从数据生命周期（创建→索引→召回→衰减→归档→恢复）、并发安全（锁顺序、竞态窗口）、持久化安全（crash 数据丢失窗口）三个维度审查。

### 发现

#### 1. (P2) memory.Store.flush() 的 dirty flag 竞态窗口

`flush()` 在 `RUnlock()` 和 `Lock()` 之间有一个窗口：另一个 goroutine 可以调用 `Save()` 设置 `dirty=true` 并修改 `entries`，然后 `flush()` 设置 `dirty=false`，但新 entry 不在已 flush 的数据中。如果进程在下一次 flush 前崩溃，新 entry 丢失。

**影响**：最多 5 秒的长期记忆数据丢失（persistLoop 的 debounce 周期）。对于有 online_extractor 和 knowledge_extractor 兜底的长期记忆来说，可接受。

**状态**：pre-existing，不修复。

#### 2. (P2) evictLRU 不按 OwnerID 分区

maclawsrv 多租户场景下，所有用户的 entries 在同一个 pool 中竞争 maxItems=2000 的容量。高频用户可能挤出低频用户的 entries。

**影响**：仅 maclawsrv。低频用户的记忆可能被高频用户的记忆挤出。

**状态**：pre-existing，P2 优化项。

#### 3. (OK) 锁顺序一致性

所有 `entityIndex.IndexEntry()` 调用都在 `Store.mu.Lock()` 内。锁顺序 Store.mu → EntityIndex.mu 在所有 3 个调用点一致。无死锁风险。

#### 4. (OK) online_extractor 的锁使用

`classifyAndApply` 正确使用 `oe.store.mu.RLock()` 读取和 `oe.store.mu.Lock()` 写入。虽然直接访问 store 内部字段（紧耦合），但锁使用正确。

#### 5. (OK) InFlightTask 机制

`SetInFlightTask` 和 `ConsumeInFlightTask` 都在 shard lock 内操作。`FlushNow` 在 `SetInFlightTask` 后同步调用，确保 marker 持久化。`ClearInFlightTask` 在 `runAgentLoop` 的 defer 中调用（除了 LLM 错误退出路径，#85 已修复）。机制正确。

#### 6. (OK) ConversationMemory 持久化

150ms debounce + `FlushNow` 在关键路径（SetInFlightTask、saveConversationHistoryTimed）后同步调用。crash 窗口 ≤ 150ms。可接受。

### 结论

没有发现需要立即修复的 P0/P1 问题。两个 P2 pre-existing 问题（flush 竞态窗口、evictLRU 不分区）记录但不修复。
