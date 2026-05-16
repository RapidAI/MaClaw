# 记忆存储 SQLite 化 + 多实例同步设计方案

> **目标**：将 `memory.Store` 的持久化层从 JSON 文件迁移到 SQLite，实现同一台服务器上多个 maclawsrv 实例间的记忆自动同步。同时简化现有的持久化机制（删除分区管理、debounce 写盘等复杂度）。

## 一、问题陈述

### 1.1 现状

| 组件 | 实现 | 问题 |
|------|------|------|
| 持久化 | JSON 文件（单文件或 5 个分区文件） | 全量序列化写放大 |
| 并发控制 | `sync.RWMutex`（进程内） | 跨进程无保护 |
| 写盘策略 | `persistLoop` 5s debounce + `FlushNow` 同步 | 复杂，仍有丢数据窗口 |
| 多实例 | 无同步机制 | 实例 A 写入的记忆，实例 B 完全不可见 |

### 1.2 部署场景

```
同一台 Linux 服务器（典型：4C8G ~ 8C16G）
├── maclawsrv 实例 1 (port 8081) ── 飞书 webhook
├── maclawsrv 实例 2 (port 8082) ── 微信 webhook
├── maclawsrv 实例 3 (port 8083) ── QQ webhook
└── 共享文件系统: ~/.maclaw/
```

同一个用户可能从飞书问"记住我叫张三"，然后从微信问"我叫什么"。需要跨实例可见。

### 1.3 设计约束

- **零外部依赖**：不引入 PostgreSQL/Redis/消息队列，保持单二进制部署
- **GUI/TUI 兼容**：桌面单用户场景行为不变，可选择保留 JSON 或切换 SQLite
- **内存索引保留**：BM25/Vector/Graph 等索引仍在内存中，SQLite 只做持久化和同步
- **最终一致性**：允许 3-5 秒的同步延迟（记忆场景可接受）

---

## 二、方案概览

### 2.1 架构分层

```
┌─────────────────────────────────────────────────────────────┐
│  memory.Store (业务层)                                       │
│  - Save/Update/Delete/Recall 逻辑不变                        │
│  - BM25/Vec/Graph/Entity/Theme 索引仍在内存                   │
│  - 通过 StorageBackend 接口读写持久化层                        │
├─────────────────────────────────────────────────────────────┤
│  StorageBackend 接口                                         │
│  - LoadAll() ([]Entry, error)                               │
│  - SaveEntry(Entry) error                                   │
│  - UpdateEntry(Entry) error                                 │
│  - DeleteEntry(id string) error                             │
│  - Since(version int64) ([]Entry, []string, error)          │
│  - NextVersion() int64                                      │
│  - Close() error                                            │
├─────────────────────────────────────────────────────────────┤
│  实现 A: sqliteBackend        │  实现 B: jsonFileBackend     │
│  - WAL 模式                   │  - 包装现有 JSON 逻辑        │
│  - version 列支持增量查询      │  - GUI/TUI 向后兼容          │
│  - busy_timeout=5000          │  - 无同步能力                │
├───────────────────────────────┴─────────────────────────────┤
│  SyncLoop (仅 maclawsrv + sqliteBackend 时启用)              │
│  - 定时轮询 Since(lastVersion)                               │
│  - 增量合并到内存索引                                         │
│  - 软删除感知                                                │
└─────────────────────────────────────────────────────────────┘
```

### 2.2 数据流

**写入路径**（实例 A）：
```
Store.Save(entry)
  → s.mu.Lock()
  → 内存索引更新 (BM25/Vec/Graph/Entity)
  → s.backend.SaveEntry(entry)  // SQLite INSERT, 自动分配 version
  → s.mu.Unlock()
  // 无需 signalSave/persistLoop/flush — 写入即持久化
```

**同步路径**（实例 B 感知实例 A 的写入）：
```
syncLoop (每 3 秒):
  → newEntries, deletedIDs := s.backend.Since(s.lastSyncVersion)
  → s.mu.Lock()
  → 对 newEntries: 更新/追加到 s.entries + 重建受影响的索引
  → 对 deletedIDs: 从 s.entries 移除 + 清理索引
  → s.lastSyncVersion = max(newEntries.version)
  → s.mu.Unlock()
```


---

## 三、SQLite 表结构设计

### 3.1 主表

```sql
CREATE TABLE IF NOT EXISTS memories (
    id           TEXT PRIMARY KEY,
    content      TEXT NOT NULL,
    compact_form TEXT DEFAULT '',
    content_hash TEXT DEFAULT '',
    category     TEXT NOT NULL,
    owner_id     TEXT DEFAULT '',
    tags         TEXT DEFAULT '[]',       -- JSON array of strings
    entities     TEXT DEFAULT '[]',       -- JSON array of strings
    embedding    BLOB DEFAULT NULL,       -- float32 数组的二进制编码
    strength     REAL DEFAULT 1.0,
    access_count INTEGER DEFAULT 1,
    scope        TEXT DEFAULT '',
    source_type  TEXT DEFAULT '',
    source_url   TEXT DEFAULT '',
    stale        INTEGER DEFAULT 0,       -- boolean
    dormant      INTEGER DEFAULT 0,       -- boolean
    superseded   INTEGER DEFAULT 0,       -- boolean
    tier         TEXT DEFAULT '',
    version      INTEGER NOT NULL,        -- 单调递增，同步用
    created_at   TEXT NOT NULL,           -- RFC3339
    updated_at   TEXT NOT NULL,           -- RFC3339
    deleted_at   TEXT DEFAULT NULL,       -- 软删除时间戳，NULL=未删除
    extra        TEXT DEFAULT '{}'        -- 其余低频字段 JSON 打包
);

-- 同步查询：拉取 version > X 的增量
CREATE INDEX idx_memories_version ON memories(version);

-- OwnerID 过滤（多租户隔离）
CREATE INDEX idx_memories_owner ON memories(owner_id) WHERE owner_id != '';

-- Category 过滤（RecallDynamic 指定 category 时）
CREATE INDEX idx_memories_category ON memories(category);

-- ContentHash 去重（Save 时快速查重）
CREATE INDEX idx_memories_hash ON memories(content_hash) WHERE content_hash != '';
```

### 3.2 Version 管理

```sql
-- 全局 version 计数器（所有实例共享，SQLite 写锁保证原子性）
CREATE TABLE IF NOT EXISTS memory_meta (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

-- 初始化
INSERT OR IGNORE INTO memory_meta(key, value) VALUES ('max_version', '0');
```

写入时获取下一个 version：
```sql
UPDATE memory_meta SET value = CAST(CAST(value AS INTEGER) + 1 AS TEXT) WHERE key = 'max_version';
SELECT value FROM memory_meta WHERE key = 'max_version';
```

SQLite 的写锁保证这两条语句在同一个事务中原子执行，多实例并发写入时 version 严格递增不重复。

### 3.3 `extra` 字段打包的低频字段

以下字段使用频率低，打包到 `extra` JSON 中避免表结构过宽：

```json
{
  "versions": [...],           // VersionSnapshot 历史
  "stability": {...},          // StabilityInfo
  "relations": [...],          // 图谱边
  "project_path": "",
  "linked_ids": [],
  "superseded_by": "",
  "raw_entities": null         // ExtractedFact.RawEntities
}
```

### 3.4 Embedding 存储

`embedding` 列使用 BLOB 类型，存储 `[]float32` 的 little-endian 二进制编码：

```go
func encodeEmbedding(vec []float32) []byte {
    buf := make([]byte, len(vec)*4)
    for i, v := range vec {
        binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(v))
    }
    return buf
}

func decodeEmbedding(data []byte) []float32 {
    vec := make([]float32, len(data)/4)
    for i := range vec {
        vec[i] = math.Float32frombits(binary.LittleEndian.Uint32(data[i*4:]))
    }
    return vec
}
```

768 维 embedding = 3072 bytes/条。2000 条 = ~6MB BLOB 数据。SQLite 处理无压力。

---

## 四、StorageBackend 接口定义

```go
// corelib/memory/backend.go

package memory

// StorageBackend abstracts the persistence layer for memory entries.
// Implementations must be safe for concurrent use from a single goroutine
// (the Store serializes access via its own mutex).
type StorageBackend interface {
    // LoadAll returns all non-deleted entries. Called once at startup.
    LoadAll() ([]Entry, error)

    // SaveEntry persists a new entry. The backend assigns entry.Version.
    SaveEntry(entry *Entry) error

    // UpdateEntry persists changes to an existing entry (by ID).
    // The backend increments the version.
    UpdateEntry(entry *Entry) error

    // DeleteEntry soft-deletes an entry by ID.
    // The backend sets deleted_at and increments version (for sync propagation).
    DeleteEntry(id string) error

    // Since returns entries modified after the given version, plus IDs of
    // entries deleted after that version. Used by syncLoop for incremental sync.
    Since(version int64) (modified []Entry, deletedIDs []string, err error)

    // MaxVersion returns the current maximum version number.
    MaxVersion() (int64, error)

    // Close releases resources (DB connections, file handles).
    Close() error
}
```

### 4.1 sqliteBackend 实现要点

```go
// corelib/memory/backend_sqlite.go

type sqliteBackend struct {
    db   *sql.DB
    path string
}

func NewSQLiteBackend(dbPath string) (*sqliteBackend, error) {
    db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000&_synchronous=NORMAL")
    if err != nil {
        return nil, err
    }
    // WAL 模式：允许并发读，写操作排队
    // busy_timeout=5000：写冲突时等待最多 5 秒（而非立即 SQLITE_BUSY）
    // synchronous=NORMAL：WAL 模式下安全且性能好
    
    if err := b.migrate(); err != nil {
        return nil, err
    }
    return &sqliteBackend{db: db, path: dbPath}, nil
}
```

### 4.2 jsonFileBackend 实现（向后兼容）

包装现有的 JSON 文件读写逻辑，`Since()` 返回空（不支持同步）。GUI/TUI 单实例模式使用。

```go
// corelib/memory/backend_json.go

type jsonFileBackend struct {
    path    string
    partMgr *partitionManager // 复用现有分区逻辑
}

func (b *jsonFileBackend) Since(version int64) ([]Entry, []string, error) {
    return nil, nil, nil // JSON 模式不支持增量同步
}
```

---

## 五、SyncLoop 设计

### 5.1 启动条件

```go
// Store 初始化时判断是否启动 syncLoop
func NewStore(path string, opts ...StoreOption) (*Store, error) {
    // ...
    if s.backend.SupportsSync() && s.syncEnabled {
        go s.syncLoop()
    }
}
```

`syncEnabled` 通过 `StoreOption` 配置：
- maclawsrv：`WithSync(true)`（默认 true）
- GUI/TUI：`WithSync(false)`（单实例无需同步）

### 5.2 核心循环

```go
func (s *Store) syncLoop() {
    ticker := time.NewTicker(s.syncInterval) // 默认 3 秒
    defer ticker.Stop()

    for {
        select {
        case <-s.stopCh:
            return
        case <-ticker.C:
            s.syncOnce()
        }
    }
}

func (s *Store) syncOnce() {
    modified, deletedIDs, err := s.backend.Since(s.lastSyncVersion)
    if err != nil {
        log.Printf("[memory_sync] poll error: %v", err)
        return
    }
    if len(modified) == 0 && len(deletedIDs) == 0 {
        return // 无变化，快速返回
    }

    s.mu.Lock()
    defer s.mu.Unlock()

    // 合并修改的 entries
    for _, remote := range modified {
        if remote.Version <= s.lastSyncVersion {
            continue // 防御性检查
        }
        localIdx := s.findEntryIndexByID(remote.ID)
        if localIdx >= 0 {
            // 已存在：比较 UpdatedAt，last-write-wins
            if remote.UpdatedAt.After(s.entries[localIdx].UpdatedAt) {
                s.entries[localIdx] = remote
                s.updateIndicesForEntry(remote)
            }
        } else {
            // 新 entry：追加
            s.entries = append(s.entries, remote)
            s.addToIndices(remote)
        }
    }

    // 处理删除
    for _, id := range deletedIDs {
        s.removeFromEntriesAndIndices(id)
    }

    // 更新同步水位
    if len(modified) > 0 {
        maxVer := modified[len(modified)-1].Version
        if maxVer > s.lastSyncVersion {
            s.lastSyncVersion = maxVer
        }
    }
}
```

### 5.3 索引增量更新

当前 `rebuildDerivedIndexesLocked(false)` 是全量重建。需要新增增量方法：

```go
// 增量添加单条 entry 到所有索引
func (s *Store) addToIndices(e Entry) {
    s.bm25.addEntry(e)
    s.vecIndex.add(e.ID, e.Embedding)
    s.autoLink(e)
    if s.entityIndex != nil { s.entityIndex.IndexEntry(&e) }
    if s.projIndex != nil { s.projIndex.IndexEntry(&e) }
    if s.semanticGraph != nil { s.semanticGraph.IndexEntry(&e) }
}

// 增量更新单条 entry 的索引
func (s *Store) updateIndicesForEntry(e Entry) {
    s.bm25.updateEntry(e)
    s.vecIndex.add(e.ID, e.Embedding) // add 是 upsert 语义
    if s.entityIndex != nil { s.entityIndex.IndexEntry(&e) }
    if s.semanticGraph != nil { s.semanticGraph.IndexEntry(&e) }
}

// 从所有索引中移除
func (s *Store) removeFromEntriesAndIndices(id string) {
    for i, e := range s.entries {
        if e.ID == id {
            s.entries = append(s.entries[:i], s.entries[i+1:]...)
            break
        }
    }
    s.bm25.removeEntry(id)
    s.vecIndex.remove(id)
    s.graph.remove(id)
    if s.entityIndex != nil { s.entityIndex.RemoveEntry(id) }
    if s.semanticGraph != nil { s.semanticGraph.RemoveEntry(id) }
}
```

### 5.4 写入时跳过自己的变更

本实例写入的 entry 已经在内存索引中了，syncLoop 不应重复处理：

```go
// Store 新增字段
type Store struct {
    // ...
    instanceID      string // 启动时生成的唯一实例标识
    lastSyncVersion int64  // 上次同步的 version 水位
}

// SaveEntry 时记录 instanceID
func (s *Store) Save(entry Entry) error {
    // ...
    entry.InstanceID = s.instanceID // 标记写入来源
    s.backend.SaveEntry(&entry)
}

// syncOnce 中跳过自己写入的
for _, remote := range modified {
    if remote.InstanceID == s.instanceID {
        // 自己写的，内存索引已更新，只更新水位
        continue
    }
    // ... 合并逻辑
}
```

`InstanceID` 不持久化到 DB（或存在 extra 中），只用于运行时过滤。

---

## 六、迁移策略

### 6.1 从 JSON 迁移到 SQLite

首次启动检测逻辑：

```go
func (s *Store) load() error {
    // 1. 如果 memory.db 已存在 → 直接用 SQLite
    if fileExists(sqlitePath) {
        s.backend = NewSQLiteBackend(sqlitePath)
        entries, _ := s.backend.LoadAll()
        s.entries = entries
        return nil
    }

    // 2. 如果 JSON 文件存在 → 迁移到 SQLite
    if fileExists(jsonPath) || partitionFilesExist() {
        entries := loadFromJSON() // 复用现有加载逻辑
        s.backend = NewSQLiteBackend(sqlitePath)
        for i := range entries {
            s.backend.SaveEntry(&entries[i])
        }
        // 重命名旧文件
        renameToMigrated(jsonPath)
        renamePartitionFiles()
        s.entries = entries
        return nil
    }

    // 3. 全新安装 → 创建空 SQLite
    s.backend = NewSQLiteBackend(sqlitePath)
    return nil
}
```

### 6.2 配置控制

```go
// AppConfig 新增
type AppConfig struct {
    // ...
    MemoryBackend string `json:"memory_backend,omitempty"` // "sqlite" | "json" | ""(auto)
}
```

- `"sqlite"`：强制使用 SQLite（maclawsrv 默认）
- `"json"`：强制使用 JSON（向后兼容）
- `""`（空/auto）：maclawsrv 自动选 SQLite，GUI/TUI 自动选 JSON

### 6.3 回滚方案

如果 SQLite 出现问题，可以通过配置切回 JSON：
1. 设置 `memory_backend: "json"`
2. 从 SQLite 导出 entries 到 JSON（提供 CLI 工具 `maclaw-tool memory export --format=json`）
3. 重启


---

## 七、简化收益——删除的代码和机制

### 7.1 删除清单

| 组件/机制 | 文件 | 行数估计 | 删除理由 |
|-----------|------|---------|---------|
| `persistLoop` goroutine | store.go | ~30 行 | SQLite 写入即持久化，无需 debounce |
| `saveCh` channel + `signalSave()` | store.go | ~15 行 | 同上 |
| `dirtyGen` + `dirty` 标记 | store.go | ~20 行 | 同上 |
| `FlushNow()` 方法 | store.go | ~10 行 | 同上（改为 no-op 或删除） |
| `flush()` 方法 | store.go | ~50 行 | 被 backend.SaveEntry 替代 |
| `partitionManager` 整体 | partition.go | ~200 行 | SQLite 替代分区文件 |
| `migrateFromLegacy()` | partition.go | ~40 行 | 一次性迁移后删除 |
| `AtomicWriteFile` 对 memory 的调用 | store.go | ~5 行 | SQLite 自带 ACID |
| `FlushNow` 的 6 个调用点 | im_message_handler.go 等 | ~12 行 | 不再需要"确保写盘" |
| in-flight marker 的 FlushNow 调用 | im_message_handler.go | ~4 行 | SQLite 写入即持久化 |

**预计净删除：~380 行代码**

### 7.2 简化后的 Store 结构体

```go
type Store struct {
    mu              sync.RWMutex
    entries         []Entry
    backend         StorageBackend      // 新增：持久化抽象
    stopCh          chan struct{}
    stopOnce        sync.Once
    maxItems        int
    instanceID      string              // 新增：实例标识
    lastSyncVersion int64               // 新增：同步水位
    syncInterval    time.Duration       // 新增：同步间隔

    // 索引（不变）
    bm25            *bm25Index
    vecIndex        *vectorIndex
    graph           *memoryGraph
    embedder        embedding.Embedder
    embedderGen     uint64
    archive         *ArchiveStore
    tmt             *TemporalTree
    projIndex       *ProjectIndex
    semanticGraph   *SemanticGraph
    entityIndex     *EntityIndex
    themeManager    *ThemeManager
    onlineExtractor *OnlineExtractor
    inferenceEngine *InferenceEngine

    // 删除的字段：
    // path         string           ← 移到 backend 内部
    // dirty        bool             ← 不再需要
    // dirtyGen     uint64           ← 不再需要
    // saveCh       chan struct{}     ← 不再需要
    // partMgr      *partitionManager ← 删除
    // pendingDedup []pendingDedupPair ← 保留（与持久化无关）
    // llmDedup     LLMChatCaller     ← 保留
}
```

---

## 八、性能分析

### 8.1 写入延迟

| 操作 | JSON 文件（当前） | SQLite WAL |
|------|-----------------|-----------|
| 单条 Save | ~0ms（内存）+ 5s 后批量写盘 | ~0.5-1ms（WAL append） |
| 并发写入 | 无（单进程） | 排队等锁，busy_timeout=5s |
| 数据安全 | 5s 窗口内可能丢失 | 写入即持久化 |

SQLite WAL 模式下单条 INSERT 延迟 <1ms，对 `Save()` 路径的影响可忽略（当前 `Save()` 的瓶颈是 embedding 计算 2-10ms，不是持久化）。

### 8.2 同步延迟

- syncLoop 间隔：3 秒
- `Since(version)` 查询：有 `idx_memories_version` 索引，2000 条表中 <0.1ms
- 最坏情况延迟：3 秒（用户在实例 A 写入后，实例 B 最多 3 秒后可见）

### 8.3 启动时间

| 操作 | JSON 文件（当前） | SQLite |
|------|-----------------|--------|
| 加载 2000 条 entries | ~50-100ms（JSON parse） | ~30-50ms（SQLite scan） |
| 重建索引 | ~200-500ms | ~200-500ms（不变） |

启动时间基本不变。

### 8.4 磁盘占用

| 存储 | JSON 文件 | SQLite |
|------|----------|--------|
| 2000 条（无 embedding） | ~2-4MB | ~3-5MB |
| 2000 条（含 768 维 embedding） | ~8-12MB | ~9-12MB |
| WAL 文件 | N/A | ~1-2MB（checkpoint 后清理） |

磁盘占用基本持平。

---

## 九、SQLite 并发安全分析

### 9.1 WAL 模式下的并发模型

```
实例 A (写)     实例 B (读)     实例 C (读)
    │               │               │
    │  EXCLUSIVE    │  SHARED       │  SHARED
    │  (写 WAL)    │  (读 DB+WAL)  │  (读 DB+WAL)
    ▼               ▼               ▼
┌─────────────────────────────────────────┐
│  memory.db (主文件)                      │
│  memory.db-wal (WAL 日志)                │
│  memory.db-shm (共享内存，协调用)         │
└─────────────────────────────────────────┘
```

- **读-读并发**：完全并行，无锁
- **读-写并发**：读者看到写入前的快照（MVCC），不阻塞
- **写-写并发**：排队等待（`busy_timeout=5000ms`），不会死锁

### 9.2 多进程安全保证

SQLite 的多进程安全依赖：
1. 文件锁（`fcntl` on Linux / `LockFileEx` on Windows）
2. 共享内存文件（`.db-shm`）用于 WAL 索引协调
3. 所有进程必须在同一个文件系统上（不能是 NFS/CIFS）

**限制**：如果 `~/.maclaw/` 在 NFS 上，SQLite 多进程不安全。但同一台服务器的多实例通常共享本地文件系统，无此问题。

### 9.3 Checkpoint 策略

WAL 文件会持续增长，需要定期 checkpoint（将 WAL 内容合并回主文件）：

```go
// 每 5 分钟执行一次 passive checkpoint
func (b *sqliteBackend) checkpointLoop() {
    ticker := time.NewTicker(5 * time.Minute)
    for range ticker.C {
        b.db.Exec("PRAGMA wal_checkpoint(PASSIVE)")
    }
}
```

`PASSIVE` 模式不阻塞读写，只在没有活跃读者时合并。

---

## 十、实施计划

### Phase 1: StorageBackend 接口抽象（1-2 天）

**目标**：定义接口，将现有 JSON 逻辑包装为 `jsonFileBackend`，Store 的 `load()`/`flush()` 委托给 backend。零行为变化。

**改动文件**：
- 新增 `corelib/memory/backend.go`：接口定义
- 新增 `corelib/memory/backend_json.go`：包装现有逻辑
- 修改 `corelib/memory/store.go`：`NewStore` 接受 backend 参数，`load()`/`flush()` 委托

**验收**：所有现有 memory 测试通过，行为不变。

### Phase 2: sqliteBackend 实现（2-3 天）

**目标**：实现 SQLite 后端，支持 CRUD + version + Since 查询。

**改动文件**：
- 新增 `corelib/memory/backend_sqlite.go`：完整实现
- 新增 `corelib/memory/backend_sqlite_test.go`：单元测试
- 修改 `go.mod`：添加 `github.com/mattn/go-sqlite3`（或 `modernc.org/sqlite` 纯 Go 版本）

**SQLite 驱动选择**：
- `modernc.org/sqlite`（纯 Go，无 CGO）：编译简单，跨平台无痛，性能略低
- `github.com/mattn/go-sqlite3`（CGO）：性能最优，但 Windows 交叉编译需要 GCC

**推荐**：`modernc.org/sqlite`——maclawsrv 部署在 Linux 上，性能差异可忽略，且避免 CGO 编译问题。

**验收**：新增 15+ 个测试覆盖 CRUD/version/Since/并发写入/busy_timeout。

### Phase 3: SyncLoop 实现（2-3 天）

**目标**：实现跨实例增量同步。

**改动文件**：
- 新增 `corelib/memory/sync.go`：syncLoop + syncOnce + mergeRemoteEntries
- 修改 `corelib/memory/store.go`：NewStore 启动 syncLoop（条件启用）
- 新增索引增量方法：`addToIndices`/`updateIndicesForEntry`/`removeFromEntriesAndIndices`

**验收**：
- 测试：两个 Store 实例共享同一个 SQLite 文件，实例 A 写入后 3 秒内实例 B 可召回
- 测试：实例 A 删除后实例 B 同步移除
- 测试：并发写入不丢数据

### Phase 4: 迁移 + 清理（1-2 天）

**目标**：JSON → SQLite 自动迁移，删除旧代码。

**改动文件**：
- 修改 `corelib/memory/store.go`：迁移逻辑
- 删除 `corelib/memory/partition.go`
- 修改 `gui/app.go`：maclawsrv 模式传入 `WithBackend("sqlite")`
- 修改 `gui/im_message_handler.go`：删除 `FlushNow` 调用
- 新增 `cmd/maclaw-tool/memory_export.go`：导出/回滚工具

**验收**：
- 旧 JSON 文件自动迁移到 SQLite
- 迁移后所有 memory 测试通过
- `maclaw-tool memory export --format=json` 可导出

### Phase 5: 集成测试 + 压测（1 天）

**测试场景**：
1. 3 个实例并发写入 100 条记忆 → 全部同步，无丢失
2. 实例 A 写入"我叫张三" → 3 秒后实例 B `Recall("我叫什么")` 返回"张三"
3. 实例 A 删除记忆 → 实例 B 同步删除
4. 实例 B 重启 → 从 SQLite 加载全量，索引正确
5. 模拟 SQLite busy（写入延迟 >5s）→ 返回错误，不死锁

---

## 十一、风险和缓解

| 风险 | 概率 | 影响 | 缓解 |
|------|------|------|------|
| SQLite 写锁等待超时（busy_timeout） | 低 | 单次写入失败 | 重试 1 次；记忆写入非关键路径，失败可容忍 |
| WAL 文件无限增长 | 低 | 磁盘占满 | checkpointLoop 每 5 分钟清理 |
| 迁移失败（JSON 损坏） | 低 | 启动失败 | 迁移前备份；失败时回退到空 DB + 日志告警 |
| `modernc.org/sqlite` 性能不足 | 极低 | 写入延迟增加 | 2000 条规模下差异 <1ms，可忽略 |
| NFS 文件系统 | 极低 | 数据损坏 | 文档明确要求本地文件系统；启动时检测并告警 |

---

## 十二、与现有改进的关系

| 已有改进 | 关系 |
|---------|------|
| Phase 5 分区存储 (#68) | **替代**——SQLite 替代分区文件，删除 partitionManager |
| Phase 6 OwnerID (#67) | **兼容**——OwnerID 作为 SQLite 列，索引加速过滤 |
| Phase 3 子串去重 (#64) | **不变**——去重逻辑在内存层，与持久化无关 |
| #55 In-Flight Marker | **简化**——SQLite 写入即持久化，FlushNow 不再需要 |
| #54 打断后失忆 | **简化**——saveConversationHistoryTimed 中的 FlushNow 可删除 |
| #89 Compaction | **不变**——compaction 操作内存 entries，通过 backend.UpdateEntry 持久化 |

---

## 十三、不做的事

1. **不做跨服务器同步**——当前场景是同一台服务器多实例，共享本地文件系统。跨服务器需要网络协议，超出范围。
2. **不做实时同步（<100ms）**——3 秒轮询对记忆场景足够。实时同步需要 inotify/fsnotify，增加复杂度但收益不大。
3. **不改 ConversationMemory**——对话历史已按 userID 分 shard，且是会话级数据（2h TTL），不需要跨实例同步。
4. **不引入 ORM**——直接用 `database/sql` + 手写 SQL，保持简单。2000 条记忆的 CRUD 不需要 ORM 的抽象。
5. **不做 embedding 向量的 SQLite 索引**——向量检索仍在内存中（`vecIndex`），SQLite 只存储原始向量数据。SQLite 的向量扩展（sqlite-vec）成熟度不够。

---

## 十四、总结

| 维度 | 当前 | 改进后 |
|------|------|--------|
| 持久化 | JSON 文件 + 5s debounce | SQLite WAL，写入即持久化 |
| 多实例同步 | 无 | 3 秒增量轮询 |
| 数据安全 | 5s 窗口可能丢失 | ACID 保证 |
| 代码复杂度 | persistLoop + partition + FlushNow | 单一 backend 接口 |
| 外部依赖 | 无 | 无（纯 Go SQLite） |
| 迁移成本 | — | 自动迁移，可回滚 |
| 预计工期 | — | 7-11 天 |
