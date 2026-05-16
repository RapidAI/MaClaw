# Tasks: Memory Claude-Style Upgrade

## Task 1: Entry 结构扩展 + Pinned 字段

- [x] 1.1 在 `corelib/memory/types.go` 的 `Entry` 结构体中添加 `Pinned bool json:"pinned,omitempty"` 字段
- [x] 1.2 在 `corelib/memory/store.go` 中添加 `PinEntry(id string) error` 方法
- [x] 1.3 在 `corelib/memory/store.go` 中添加 `UnpinEntry(id string) error` 方法
- [x] 1.4 修改 `evictLRU()` 将 pinned 条目加入 protected 列表（与 self_identity 同等保护）
- [x] 1.5 添加 `ActiveCount() int` 方法返回活跃条目数
- [x] 1.6 编写属性测试 `TestProperty_PinUnpinRoundTrip` (P10)
- [x] 1.7 编写属性测试 `TestProperty_AnyCategoryPin` (P14)
- [x] 1.8 编写属性测试 `TestProperty_BackwardCompatDeserialize` (P18)

## Task 2: ArchiveStore 归档冷存储

- [x] 2.1 创建 `corelib/memory/archive.go`，实现 `ArchiveStore` 结构体（字段：mu, entries, path, dirty, saveCh, stopCh, maxItems=1000）
- [x] 2.2 实现 `NewArchiveStore(path string) (*ArchiveStore, error)` 构造函数（含 load + persistLoop）
- [x] 2.3 实现 `Add(entries ...Entry) error` 方法（含容量上限淘汰最旧条目逻辑）
- [x] 2.4 实现 `Remove(id string) (*Entry, error)` 方法（用于恢复）
- [x] 2.5 实现 `List(category Category, keyword string) []Entry` 方法
- [x] 2.6 实现 `FindRelevant(tags []string, categories []Category, limit int) []Entry` 方法（用于 GC 复活）
- [x] 2.7 实现 `load()`, `flush()`, `persistLoop()`, `Stop()` 持久化方法
- [x] 2.8 在 `Store` 中集成 `ArchiveStore`：`NewStore` 时自动初始化 archive（路径为同目录下 `archive.json`）
- [x] 2.9 在 `Store` 中添加 `ListArchive(category, keyword)` 和 `RestoreFromArchive(id)` 代理方法
- [x] 2.10 修改 `evictLRU()` 将淘汰的条目调用 `archive.Add()` 而非丢弃
- [x] 2.11 编写属性测试 `TestProperty_LRUArchives` (P5)
- [x] 2.12 编写属性测试 `TestProperty_ArchiveRoundTrip` (P6)
- [x] 2.13 编写属性测试 `TestProperty_ArchiveCapacity` (P7)
- [x] 2.14 编写属性测试 `TestProperty_ArchiveListFilter` (P8)
- [x] 2.15 编写属性测试 `TestProperty_RestoreRoundTrip` (P9)

## Task 3: Compressor Pin 保护 + 智能 GC

- [x] 3.1 修改 `corelib/memory/compressor.go` 的 `dedup()` 跳过 `Pinned==true` 的条目
- [x] 3.2 修改 `mergeSemanticDuplicates()` 跳过 pinned 条目（不参与 merge batch）
- [x] 3.3 修改 `compressEntry()` 调用前检查 pinned 状态，跳过 pinned 条目
- [x] 3.4 在 `Compressor` 中添加 `gcThreshold int` 字段（默认 450）和 `SetGCThreshold(n int)` 方法
- [x] 3.5 在 `corelib/memory/types.go` 中添加 `GCResult` 结构体
- [x] 3.6 实现 `RunGC(ctx context.Context) (*GCResult, error)` 方法：跳过 pinned/protected → 按 LRU 排序 → 归档低优先级条目 → 扫描 archive 复活相关条目（限 10 条）→ 发射 `memory:gc` 事件
- [x] 3.7 修改 `Compressor.loop()` 在每次 `runOnce` 前检查 `ActiveCount() >= gcThreshold`，满足时先执行 `RunGC`
- [x] 3.8 同步修改 `gui/memory_compressor.go` 的 `MemoryCompressor`：dedup/merge/compress 跳过 pinned 条目
- [x] 3.9 编写属性测试 `TestProperty_ProtectedSurviveEviction` (P11)
- [x] 3.10 编写属性测试 `TestProperty_PinnedCompressionImmune` (P12)
- [x] 3.11 编写属性测试 `TestProperty_GCThreshold` (P15)
- [x] 3.12 编写属性测试 `TestProperty_GCArchives` (P16)
- [x] 3.13 编写属性测试 `TestProperty_RevivalLimit` (P17)

## Task 4: KnowledgeExtractor 会话后知识提取

- [x] 4.1 创建 `corelib/memory/knowledge_extractor.go`，定义 `ConversationMessage` 结构体和 `KnowledgeExtractor` 结构体
- [x] 4.2 实现 `NewKnowledgeExtractor(store *Store, llm LLMChatCaller) *KnowledgeExtractor`
- [x] 4.3 实现 `filterMessages(messages []ConversationMessage) []ConversationMessage` — 仅保留 user/assistant 消息
- [x] 4.4 实现 `preCompress(ctx context.Context, messages []ConversationMessage) (string, error)` — 超过 20 轮时 LLM 预压缩
- [x] 4.5 实现 `Extract(userID string, messages []ConversationMessage) error` — 含 cooldown 检查、过滤、预压缩、LLM 提取、去重、保存（tag: extracted）
- [x] 4.6 实现去重逻辑：提取的知识点与现有 entries 做内容相似度比较（精确匹配 + 子串匹配）
- [x] 4.7 在 `gui/conversation_archiver.go` 的 `Archive()` 末尾集成 KnowledgeExtractor 调用
- [x] 4.8 编写属性测试 `TestProperty_ConversationFilter` (P2)
- [x] 4.9 编写属性测试 `TestProperty_CooldownEnforcement` (P3)
- [x] 4.10 编写属性测试 `TestProperty_ExtractedDedup` (P4)

## Task 5: Agent 工具层适配（TUI + GUI）

- [x] 5.1 修改 `tui/agent_tools.go` 的 `toolMemory`：新增 `pin`/`unpin`/`list_archive`/`restore` action 分支
- [x] 5.2 修改 `tui/agent_handler.go` 的 memory 工具定义：在 action description 中添加 pin/unpin/list_archive/restore
- [x] 5.3 修改 GUI `toolMemory`（`gui/im_message_handler.go`）：新增 `pin`/`unpin`/`list_archive`/`restore` action 分支
- [x] 5.4 修改 list/search 输出格式：pinned 条目前缀添加 📌 标记
- [x] 5.5 编写属性测试 `TestProperty_PinIndicatorInOutput` (P13)
- [x] 5.6 编写属性测试 `TestProperty_TagPreservation` (P1)

## Task 6: System Prompt 主动记忆指令

- [x] 6.1 修改 `gui/im_message_handler.go` 的 `buildSystemPromptWithMemory`：在 isFirstTurn 时注入主动记忆指令段落
- [x] 6.2 修改 `tui/agent_handler.go` 的 `buildSystemPrompt`：当 memoryStore 非 nil 时注入主动记忆指令
- [x] 6.3 编写单元测试验证 system prompt 包含主动记忆关键词

## Task 7: 向后兼容验证 + 集成测试

- [x] 7.1 编写属性测试 `TestProperty_ExistingOpsUnchanged` (P19)
- [x] 7.2 编写单元测试：加载无 pinned 字段的 JSON 文件，验证 Pinned 默认 false
- [x] 7.3 编写单元测试：无 archive.json 时 Store 正常初始化，首次归档自动创建文件
- [x] 7.4 编写单元测试：KnowledgeExtractor LLM 未配置时优雅跳过
- [x] 7.5 编写单元测试：KnowledgeExtractor LLM 返回错误时不影响会话
- [x] 7.6 验证 `gui/memory_aliases.go` 无需修改即可编译通过（type alias 自动继承新字段）
