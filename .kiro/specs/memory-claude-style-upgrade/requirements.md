# Requirements Document

## Introduction

本需求文档描述将 Claude Code Memory 2.0 的五大核心记忆管理能力整合到 maclaw 现有记忆系统中的功能升级。升级涵盖：主动记忆机制、会话后知识提取、归档冷存储、Pin 钉住机制、智能 GC。所有改动基于 `corelib/memory` 包进行，保持向后兼容，不破坏现有 JSON 持久化格式和 GUI 端别名桥接。

## Glossary

- **Memory_Store**: `corelib/memory.Store`，maclaw 的持久化长期记忆存储，提供 CRUD、Recall、LRU 淘汰等能力
- **Entry**: `corelib/memory.Entry`，单条记忆记录，包含 ID、Content、Category、Tags、Embedding、Strength、Status、Scope 等字段
- **Compressor**: `corelib/memory.Compressor`，记忆压缩器，负责去重、LLM 语义合并、LLM 内容压缩和备份管理
- **Archiver**: `corelib/memory.Archiver`，对话归档器，从过期对话中提取关键信息存为 conversation_summary
- **Agent**: maclaw 的 AI 助手（TUI 端或 GUI 端），通过 memory 工具与 Memory_Store 交互
- **LLM_Caller**: 抽象的 LLM 调用接口（`LLMChatCaller`），用于语义压缩和知识提取
- **Active_Memory**: 活跃记忆，Status 为空（默认）的 Entry，参与正常 Recall
- **Archive_Storage**: 归档冷存储，存放被 GC 淘汰的记忆条目，独立于 Active_Memory 的持久化文件
- **Pinned_Entry**: 被钉住的记忆条目，标记为 📌，永远不会被 LRU 淘汰或 LLM 压缩
- **Knowledge_Extractor**: 会话后知识提取器，从对话记录中提取遗漏的知识点并写入 Memory_Store
- **Cooldown_Period**: 冷却期，Knowledge_Extractor 两次提取之间的最小时间间隔
- **GC_Threshold**: 垃圾回收触发阈值，当 Active_Memory 条目数达到该值时触发智能 GC
- **Proactive_Note**: Agent 在会话中主动发现并记录的非显而易见的技术细节

## Requirements

### Requirement 1: 主动记忆机制（Proactive Note-Taking）

**User Story:** As a developer, I want the Agent to proactively save non-obvious technical details discovered during a session, so that important knowledge is captured without me explicitly asking.

#### Acceptance Criteria

1. THE Agent SHALL include proactive memory instructions in the system prompt that guide the Agent on when and how to record Proactive_Notes
2. WHEN the Agent discovers a non-obvious technical detail during a session (such as a workaround, undocumented behavior, configuration quirk, or debugging insight), THE Agent SHALL save the detail as an Entry via the memory save action without requiring explicit user instruction
3. WHEN the Agent saves a Proactive_Note, THE Memory_Store SHALL store the Entry with category `project_knowledge` or `instruction` and include a tag `proactive` to distinguish proactive notes from user-initiated saves
4. THE Agent SHALL limit proactive saves to at most 5 entries per session to avoid excessive memory writes
5. WHEN the Agent saves a Proactive_Note, THE Agent SHALL include a brief inline notification in the response indicating that a memory was proactively saved (e.g., "💾 已主动记录: <summary>")

### Requirement 2: 会话后知识提取（Post-Session Knowledge Extraction）

**User Story:** As a developer, I want the system to automatically extract missed knowledge points from conversation history after a session ends, so that no important information is lost.

#### Acceptance Criteria

1. WHEN a session ends or a conversation expires, THE Knowledge_Extractor SHALL filter the conversation history to retain only user and assistant text messages, removing tool calls, system messages, and other noise
2. WHEN the filtered conversation exceeds 20 turns, THE Knowledge_Extractor SHALL pre-compress the conversation using a lightweight LLM call before performing knowledge extraction
3. WHEN the filtered (and optionally pre-compressed) conversation is ready, THE Knowledge_Extractor SHALL use the LLM_Caller to extract knowledge points and save each as an Entry in the Memory_Store
4. THE Knowledge_Extractor SHALL enforce a Cooldown_Period of 1 hour between consecutive extraction runs to prevent excessive LLM usage
5. WHEN the Knowledge_Extractor saves extracted entries, THE Memory_Store SHALL store each Entry with a tag `extracted` to distinguish extracted knowledge from user-initiated and proactive saves
6. IF the LLM_Caller is not configured or returns an error, THEN THE Knowledge_Extractor SHALL skip extraction gracefully without affecting the session lifecycle
7. THE Knowledge_Extractor SHALL deduplicate extracted knowledge points against existing entries in the Memory_Store before saving, using content similarity comparison

### Requirement 3: 归档冷存储（Archive Cold Storage）

**User Story:** As a developer, I want evicted memory entries to be archived instead of permanently deleted, so that valuable knowledge can be recovered if needed later.

#### Acceptance Criteria

1. WHEN the Memory_Store evicts an Entry via LRU, THE Memory_Store SHALL move the evicted Entry to the Archive_Storage instead of deleting the Entry permanently
2. THE Archive_Storage SHALL persist archived entries to a separate JSON file (`archive.json`) in the same directory as the main memory file
3. THE Archive_Storage SHALL enforce a maximum capacity of 1000 archived entries, evicting the oldest archived entries (by `UpdatedAt`) when the limit is exceeded
4. THE Memory_Store SHALL provide a `ListArchive` method that returns archived entries filtered by category and keyword, consistent with the existing `List` method signature
5. THE Memory_Store SHALL provide a `RestoreFromArchive` method that moves a specified archived Entry back to Active_Memory and removes the Entry from Archive_Storage
6. WHEN an Entry is restored from Archive_Storage, THE Memory_Store SHALL update the Entry's `UpdatedAt` to the current time and set `AccessCount` to 1
7. THE Archive_Storage SHALL load archived entries from disk on Memory_Store initialization and persist changes using the same delayed-write mechanism as the main store

### Requirement 4: Pin 机制（Pin Mechanism）

**User Story:** As a developer, I want to pin important memory entries so that they are never evicted by LRU or compressed by the Compressor, giving me flexible control over which memories are permanent.

#### Acceptance Criteria

1. THE Entry struct SHALL include a `Pinned` boolean field (`json:"pinned,omitempty"`) that indicates whether the Entry is pinned
2. WHEN an Entry has `Pinned` set to true, THE Memory_Store SHALL exclude the Entry from LRU eviction, regardless of AccessCount or UpdatedAt
3. WHEN an Entry has `Pinned` set to true, THE Compressor SHALL skip the Entry during LLM content compression and semantic merge operations
4. THE Memory_Store SHALL provide a `PinEntry` method that sets `Pinned` to true for the Entry with the given ID
5. THE Memory_Store SHALL provide a `UnpinEntry` method that sets `Pinned` to false for the Entry with the given ID
6. THE Agent memory tool SHALL support `pin` and `unpin` actions that call `PinEntry` and `UnpinEntry` respectively, accepting an entry ID as parameter
7. WHEN listing or searching entries, THE Memory_Store SHALL include a 📌 indicator in the output for pinned entries to provide visual distinction
8. THE Memory_Store SHALL allow any category of Entry to be pinned, providing more flexible protection than the existing `self_identity`-only protection

### Requirement 5: 智能 GC（Intelligent Garbage Collection）

**User Story:** As a developer, I want the memory system to perform intelligent garbage collection that archives instead of deleting, respects pinned entries, and can revive relevant archived entries, so that memory management is smarter and less lossy.

#### Acceptance Criteria

1. WHEN the number of Active_Memory entries reaches the GC_Threshold (default: 450, configurable), THE Compressor SHALL trigger an intelligent GC cycle instead of relying solely on the fixed 6-hour interval
2. DURING an intelligent GC cycle, THE Compressor SHALL skip all Pinned_Entries and all entries in protected categories (self_identity) from eviction candidates
3. DURING an intelligent GC cycle, THE Compressor SHALL move eviction candidates to Archive_Storage instead of deleting the candidates permanently
4. DURING an intelligent GC cycle, THE Compressor SHALL scan the Archive_Storage for entries that are relevant to recently active topics (determined by the tags and categories of the top 20 most recently accessed Active_Memory entries) and revive matching archived entries back to Active_Memory
5. THE Compressor SHALL limit archive revival to at most 10 entries per GC cycle to prevent unbounded growth
6. WHEN the Compressor revives an archived Entry, THE Memory_Store SHALL update the Entry's `UpdatedAt` to the current time and set `AccessCount` to 1
7. THE Compressor SHALL emit a `memory:gc` event after each intelligent GC cycle, including counts of archived entries, revived entries, and remaining active entries
8. THE Compressor SHALL log a summary of each GC cycle (entries archived, entries revived, active count before and after) for debugging purposes

### Requirement 6: 向后兼容与数据迁移

**User Story:** As a developer, I want the upgraded memory system to be fully backward compatible with existing memory data, so that no existing memories are lost during the upgrade.

#### Acceptance Criteria

1. THE Memory_Store SHALL load existing `memory.json` files that lack the `pinned` field without error, treating missing `pinned` as false
2. THE Memory_Store SHALL create the `archive.json` file on first use if the file does not exist, without requiring manual setup
3. THE GUI memory alias bridge (`gui/memory_aliases.go`) SHALL continue to function without modification, as all changes are made in the `corelib/memory` package
4. THE existing Agent memory tool actions (`save`, `list`, `search`, `delete`) SHALL continue to function identically after the upgrade
5. WHEN the Memory_Store is initialized with an existing dataset, THE Memory_Store SHALL not trigger any automatic migration or data transformation that modifies existing entries
