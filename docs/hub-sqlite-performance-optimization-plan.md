# Hub/HubCenter SQLite 读写性能优化方案

## 现状分析

### 当前架构

Hub 和 HubCenter 已有良好的基础设施：

| 机制 | 当前实现 | 状态 |
|------|---------|------|
| 读写分离连接池 | `Provider.Write` + `Provider.Read` | ✅ 已有 |
| WAL 模式 | `PRAGMA journal_mode = WAL` | ✅ 已有 |
| Write Batcher | channel + timer flush + 单事务合并 | ✅ 已有 |
| 基础 PRAGMA | foreign_keys/busy_timeout/synchronous=NORMAL/temp_store=MEMORY | ✅ 已有 |
| 索引 | 大量覆盖性索引（hub_user_links、hub_domain_routes、ha_sync_ops 等） | ✅ 已有 |

### 当前配置参数

| 参数 | Hub 默认值 | HubCenter 默认值 |
|------|-----------|-----------------|
| MaxReadOpenConns | 8 | 4 |
| MaxReadIdleConns | 4 | 4 |
| MaxWriteOpenConns | 1 | 1 |
| MaxWriteIdleConns | 1 | 1 |
| BusyTimeoutMS | 5000 | 10000 |
| BatchFlushMS | 250 | 250 |
| BatchMaxSize | 64 | 64 |
| BatchQueueSize | 1024 | 1024 |

### 识别的性能瓶颈（按影响排序）

1. **缺少关键 PRAGMA：`cache_size` 和 `mmap_size`**——默认 cache_size=-2000（~2MB），大量用户时 page cache 频繁淘汰
2. **WAL 检查点未显式管理**——依赖 SQLite 默认的自动检查点（每 1000 pages），大量并发写入时 WAL 文件膨胀导致读性能下降
3. **Hub ReadPool 连接数不足以应对大量用户**——当前 8 个连接，每个 HTTP 请求可能持有一个连接直到查询完成
4. **部分高频查询缺少覆盖索引**——如 `sessions` 表按 `machine_id`、`user_id`、`status` 的组合查询
5. **Write Batcher 错误处理粗粒度**——一个 job 执行失败导致整个 batch 回滚，所有 job 都失败
6. **`lower()` 函数调用阻止索引使用**——`WHERE lower(email) = lower(?)` 无法使用 email 列的索引

---

## 优化方案

### Phase 1: PRAGMA 调优（零代码改动，纯配置）

修改 `applyPragmas()` 函数，添加以下 PRAGMA：

```go
func applyPragmas(db *sql.DB, cfg Config) error {
    stmts := []string{
        "PRAGMA foreign_keys = ON;",
        fmt.Sprintf("PRAGMA busy_timeout = %d;", cfg.BusyTimeoutMS),
        "PRAGMA synchronous = NORMAL;",
        "PRAGMA temp_store = MEMORY;",
        // --- 新增 ---
        fmt.Sprintf("PRAGMA cache_size = -%d;", cfg.CacheSizeKB),   // 负值表示 KB
        fmt.Sprintf("PRAGMA mmap_size = %d;", cfg.MmapSizeBytes),   // 内存映射 I/O
        "PRAGMA page_size = 4096;",                                  // 确认 4KB page
    }
    if cfg.WAL {
        stmts = append(stmts, "PRAGMA journal_mode = WAL;")
        stmts = append(stmts, "PRAGMA wal_autocheckpoint = 2000;")  // 提高到 2000 pages
    }
    // ...
}
```

**新增配置字段和默认值**：

| 字段 | Hub 默认值 | HubCenter 默认值 | 说明 |
|------|-----------|-----------------|------|
| CacheSizeKB | 32768 (32MB) | 16384 (16MB) | page cache 大小 |
| MmapSizeBytes | 268435456 (256MB) | 134217728 (128MB) | 内存映射大小 |

**原理**：
- `cache_size=-32768`：将 page cache 从默认 2MB 提升到 32MB。对于有 100+ 在线用户的场景，热数据（users、machines、sessions、viewer_tokens）能完全放入缓存
- `mmap_size=256MB`：启用内存映射 I/O，读操作直接通过页表映射读取数据库文件，绕过 `read()` 系统调用。对读密集的 Hub 收益巨大
- `wal_autocheckpoint=2000`：默认 1000 pages 时检查点过于频繁（写密集时每秒触发），提升到 2000 减少检查点开销

**修改文件**：
- `hub/internal/store/sqlite/provider.go`
- `hubcenter/internal/store/sqlite/provider.go`
- `hub/internal/config/config.go`
- `hubcenter/internal/config/config.go`

---

### Phase 2: WAL 检查点管理——后台周期性 PASSIVE 检查点

**问题**：高并发写入时 WAL 文件持续增长，读者需要从 WAL + 主文件合并数据。WAL 越大，读越慢。

**修复**：Provider 启动一个后台 goroutine，定期执行 PASSIVE 检查点（不阻塞读写）。

```go
// provider.go 新增

type Provider struct {
    Write    *sql.DB
    Read     *sql.DB
    batch    *writeBatcher
    stopCkpt chan struct{}
    doneCkpt chan struct{}
}

func (p *Provider) startCheckpointer(interval time.Duration) {
    p.stopCkpt = make(chan struct{})
    p.doneCkpt = make(chan struct{})
    go func() {
        defer close(p.doneCkpt)
        ticker := time.NewTicker(interval)
        defer ticker.Stop()
        for {
            select {
            case <-p.stopCkpt:
                return
            case <-ticker.C:
                // PASSIVE checkpoint: moves WAL pages to DB without blocking
                _, _ = p.Write.Exec("PRAGMA wal_checkpoint(PASSIVE);")
            }
        }
    }()
}
```

**配置**：

| 字段 | 默认值 | 说明 |
|------|--------|------|
| CheckpointIntervalSec | 60 | PASSIVE 检查点间隔（秒）|

**原理**：
- PASSIVE 不阻塞并发读写，只把已完成事务的 WAL pages 回写到主文件
- 将 WAL 文件大小控制在可预测范围内（不超过 autocheckpoint × page_size ≈ 8MB）
- 与 autocheckpoint 互补：autocheckpoint 在写事务结束时触发（可能被并发读阻塞），后台 PASSIVE 在无读者时执行

**修改文件**：
- `hub/internal/store/sqlite/provider.go`
- `hubcenter/internal/store/sqlite/provider.go`

---

### Phase 3: 连接池参数自适应——基于配置的扩容

当前 Hub 的 `MaxReadOpenConns=8` 在 100+ 在线用户时不足。每个 WebSocket 心跳 + HTTP API 请求都需要一个 read 连接。

**修改 Config 默认值**：

| 参数 | 当前 Hub | 优化后 Hub | 当前 HubCenter | 优化后 HubCenter |
|------|---------|-----------|---------------|-----------------|
| MaxReadOpenConns | 8 | 16 | 4 | 8 |
| MaxReadIdleConns | 4 | 8 | 4 | 4 |
| BusyTimeoutMS | 5000 | 10000 | 10000 | 10000 |

**原理**：
- WAL 模式下读连接互不阻塞，增加连接数只增加少量内存开销（每连接 ~50KB）
- `BusyTimeoutMS` 提升避免写密集时读者过早 `SQLITE_BUSY` 报错
- `MaxReadIdleConns` 跟随 OpenConns 提升，避免频繁创建/销毁连接

**修改文件**：
- `hub/internal/config/config.go`
- `hubcenter/internal/config/config.go`

---

### Phase 4: Write Batcher 增强——partial success + 分组提交

**问题 1**：当前 `flush()` 中任意一个 job 执行失败，整个 batch 回滚，所有 job 都收到错误。批量心跳更新中一个无效 machineID 导致整批失败。

**修复**：失败时切换到逐条执行模式（graceful degradation）。

```go
func (b *writeBatcher) flush(batch []writeBatchJob) []error {
    results := make([]error, len(batch))

    // 尝试批量提交
    tx, err := b.db.BeginTx(context.Background(), nil)
    if err != nil {
        for i := range results {
            results[i] = err
        }
        return results
    }

    allOK := true
    for i, job := range batch {
        select {
        case <-job.ctx.Done():
            results[i] = job.ctx.Err()
            continue
        default:
        }
        if _, err := tx.ExecContext(job.ctx, job.query, job.args...); err != nil {
            results[i] = err
            allOK = false
            break
        }
    }

    if allOK {
        if err := tx.Commit(); err != nil {
            for i := range results {
                if results[i] == nil {
                    results[i] = err
                }
            }
        }
        return results
    }

    // Batch 失败——回滚后逐条重试
    _ = tx.Rollback()
    for i, job := range batch {
        if results[i] != nil {
            continue // 已有错误（ctx cancelled 或首次失败的 job）
        }
        select {
        case <-job.ctx.Done():
            results[i] = job.ctx.Err()
            continue
        default:
        }
        if _, err := b.db.ExecContext(job.ctx, job.query, job.args...); err != nil {
            results[i] = err
        }
    }
    return results
}
```

**问题 2**：`BatchMaxSize=64` 对心跳更新过小。100 台机器每 10 秒心跳 → 10 jobs/s × 25s flush interval = 250 pending。队列会积压。

**修复**：提升 `BatchMaxSize` 默认值到 128，`BatchFlushMS` 降低到 100ms。

| 参数 | 当前 | 优化后 | 说明 |
|------|------|--------|------|
| BatchFlushMS | 250 | 100 | 更快刷盘，降低写入延迟 |
| BatchMaxSize | 64 | 128 | 允许更大批次，减少事务开销 |
| BatchQueueSize | 1024 | 4096 | 峰值时不丢 job |

**修改文件**：
- `hub/internal/store/sqlite/batcher.go`
- `hubcenter/internal/store/sqlite/batcher.go`
- `hub/internal/config/config.go`
- `hubcenter/internal/config/config.go`

---

### Phase 5: 关键索引补全

根据代码中的查询模式，补全以下缺失索引：

```sql
-- Hub: sessions 表高频查询（WebSocket 连接管理）
CREATE INDEX IF NOT EXISTS idx_sessions_machine_status 
  ON sessions(machine_id, status);
CREATE INDEX IF NOT EXISTS idx_sessions_user_status 
  ON sessions(tenant_id, user_id, status);
CREATE INDEX IF NOT EXISTS idx_sessions_updated_at 
  ON sessions(updated_at DESC);

-- Hub: machines 表按 user_id 查询
CREATE INDEX IF NOT EXISTS idx_machines_user_tenant 
  ON machines(tenant_id, user_id, status);

-- Hub: viewer_tokens 按 user_id 查询
CREATE INDEX IF NOT EXISTS idx_viewer_tokens_user_id 
  ON viewer_tokens(user_id);

-- Hub: login_tokens 过期清理
CREATE INDEX IF NOT EXISTS idx_login_tokens_expires_at 
  ON login_tokens(expires_at);

-- HubCenter: hub_user_usage_daily 聚合查询优化
CREATE INDEX IF NOT EXISTS idx_hub_user_usage_daily_hub_tenant_day 
  ON hub_user_usage_daily(hub_id, tenant_id, day, user_email);
```

**修改文件**：
- `hub/internal/store/sqlite/migrations.go`
- `hubcenter/internal/store/sqlite/migrations.go`

---

### Phase 6: `lower()` 函数索引替代——写入时规范化

**问题**：`WHERE lower(email) = lower(?)` 使用函数包裹列，SQLite 无法使用 email 索引。`modernc.org/sqlite` 不支持 expression index。

**修复策略**：写入时统一将 email 转为小写存储，查询时不再需要 `lower()` 函数。

受影响的写入点（已在代码中发现部分已这样做）：
- `userRepo.Create`：`user.Email` → `strings.ToLower(user.Email)`
- `userRepo.getByEmail`：查询参数 `strings.ToLower(email)` ✅ 已有
- 查询条件从 `lower(email) = lower(?)` 改为 `email = ?`

**受影响查询**（需要改为 `email = ?`）：
- `userRepo.getByEmail`：`WHERE lower(email) = lower(?)`
- `userRepo.DeleteByTenantEmail`：`WHERE tenant_id = ? AND lower(email) = lower(?)`
- `userRepo.MarkEmailVerified`：`WHERE tenant_id = ? AND lower(email) = lower(?)`
- `enrollmentRepo.DeleteByTenantEmail`：`WHERE tenant_id = ? AND lower(email) = lower(?)`

**注意**：需要一次性迁移将所有已有数据的 email 列转为小写：

```sql
UPDATE users SET email = LOWER(TRIM(email)) WHERE email <> LOWER(TRIM(email));
UPDATE user_enrollments SET email = LOWER(TRIM(email)) WHERE email <> LOWER(TRIM(email));
UPDATE email_blocklist SET email = LOWER(TRIM(email)) WHERE email <> LOWER(TRIM(email));
```

**修改文件**：
- `hub/internal/store/sqlite/repositories_stub.go`
- `hub/internal/store/sqlite/migrations.go`（添加数据迁移）

---

### Phase 7: 预编译语句缓存（Prepared Statement Pool）

**问题**：每次查询都经过 SQL 解析 → 查询计划生成。高频操作（心跳更新、session 状态查询、token 验证）重复执行相同 SQL。

**修复**：为高频 SQL 创建预编译语句缓存。

```go
// prepared_stmts.go 新增

type preparedStmtCache struct {
    mu    sync.RWMutex
    stmts map[string]*sql.Stmt
    db    *sql.DB
}

func newPreparedStmtCache(db *sql.DB) *preparedStmtCache {
    return &preparedStmtCache{stmts: make(map[string]*sql.Stmt), db: db}
}

func (c *preparedStmtCache) Get(query string) (*sql.Stmt, error) {
    c.mu.RLock()
    if stmt, ok := c.stmts[query]; ok {
        c.mu.RUnlock()
        return stmt, nil
    }
    c.mu.RUnlock()

    c.mu.Lock()
    defer c.mu.Unlock()
    // Double-check after acquiring write lock
    if stmt, ok := c.stmts[query]; ok {
        return stmt, nil
    }
    stmt, err := c.db.Prepare(query)
    if err != nil {
        return nil, err
    }
    c.stmts[query] = stmt
    return stmt, nil
}

func (c *preparedStmtCache) Close() {
    c.mu.Lock()
    defer c.mu.Unlock()
    for _, stmt := range c.stmts {
        _ = stmt.Close()
    }
    c.stmts = nil
}
```

高频语句候选：
- `UPDATE machines SET last_seen_at = ?, status = ?, updated_at = ? WHERE id = ?`（心跳，每机每 10s）
- `SELECT ... FROM viewer_tokens WHERE token_hash = ?`（每次 API 请求鉴权）
- `SELECT ... FROM sessions WHERE machine_id = ? AND status = ?`（WebSocket 连接管理）
- `UPDATE hub_instances SET status = ..., last_seen_at = ? WHERE id = ?`（HubCenter 心跳）

**Provider 新增字段**：

```go
type Provider struct {
    Write     *sql.DB
    Read      *sql.DB
    batch     *writeBatcher
    readStmts *preparedStmtCache
    stopCkpt  chan struct{}
    doneCkpt  chan struct{}
}
```

**修改文件**：
- `hub/internal/store/sqlite/prepared_stmts.go`（新文件）
- `hub/internal/store/sqlite/provider.go`
- `hubcenter/internal/store/sqlite/prepared_stmts.go`（新文件）
- `hubcenter/internal/store/sqlite/provider.go`

---

## 实施优先级

| Phase | 收益 | 改动范围 | 风险 | 推荐顺序 |
|-------|------|---------|------|---------|
| 1: PRAGMA 调优 | 🔴 高（读性能 2-5x） | 低（配置） | 极低 | **立即** |
| 2: WAL 检查点 | 🟡 中（写密集场景） | 低 | 极低 | **立即** |
| 3: 连接池扩容 | 🟡 中（并发读） | 极低 | 极低 | **立即** |
| 4: Batcher 增强 | 🟡 中（写吞吐+可靠性） | 中 | 低 | 第二批 |
| 5: 索引补全 | 🟡 中（特定查询） | 低 | 极低 | 第二批 |
| 6: email 规范化 | 🟢 低-中（email 查询） | 中 | 低 | 第三批 |
| 7: 预编译语句 | 🟢 低（减少 CPU） | 中 | 低 | 第三批 |
| **8: Write Coalescer** | **🔴 极高（万人级核心）** | **中** | **低** | **已实现** |

---

### Phase 8: Write Coalescer——万人级核心机制（已实现）

**问题本质**：10K 在线机器，每台每 10-60 秒发一次心跳，每次心跳触发 2 个 DB 写入（UpdateMetadata + UpdateHeartbeat）。即使有 batcher，也是 334+ 个独立 SQL 语句/秒通过 batcher 进入 SQLite。Batcher 只合并"不同语句"为一个事务——但每个语句更新不同的 machineID，batcher 无法去重。

**WriteCoalescer 设计**：

```
机器 A 心跳 → coalescer.Set("machine_hb:A", UPDATE...) → 内存覆盖
机器 A 心跳 → coalescer.Set("machine_hb:A", UPDATE...) → 内存覆盖（前一个丢弃）
机器 B 心跳 → coalescer.Set("machine_hb:B", UPDATE...)
... 5秒后 ...
coalescer.flush() → 一个事务写入 A + B 的最新值
```

与 batcher 的本质区别：
- **Batcher**：每个 job 必须执行（语义：所有写入都必须持久化）。适合 INSERT、审计日志。
- **Coalescer**：同 key 的写入只保留最后一个（语义：只要最终状态正确即可）。适合心跳、状态更新、preview 增量。

**万人级数据**：
- 10K 机器 × 2 writes/heartbeat × 1 heartbeat/10s = **2000 writes/sec 输入**
- Coalescer 每 5s flush → 每次 flush ~2000 条去重后的 UPDATE 语句
- SQLite 写压力：从 **2000 writes/sec** → **1 transaction per 5s**（~2000 UPDATE）
- 单事务 2000 条 UPDATE 在 SQLite WAL 模式下 < 50ms

**已接入的写操作**：

| 操作 | coalesce key | 峰值输入 | flush 后实际 |
|------|-------------|---------|-------------|
| machine.UpdateHeartbeat | `machine_hb:{id}` | 167/sec | ~2K/5s tx |
| machine.UpdateMetadata | `machine_meta:{id}` | 167/sec | ~2K/5s tx |
| session.UpdatePreview | `session_preview:{id}` | 500/sec | ~1K/5s tx |

**配置**（`config.yaml`）：
```yaml
database:
  coalesce_flush_ms: 5000    # 5秒刷盘周期
  coalesce_max_batch: 512    # 单事务最大语句数（超出分多个 tx）
```

**数据丢失窗口**：进程崩溃最多丢失 5 秒心跳/preview。可接受——心跳只影响 `last_seen_at` 显示精度，preview 在 WebSocket 断开后本身不可见。重要数据（session 创建/关闭、用户注册、token 等）不经过 coalescer。

---

## 预期效果

以 10,000 在线用户、10,000 台机器为基准：

| 指标 | 当前（无优化） | 优化后（预估） |
|------|-------------|--------------|
| 心跳写入压力 | 2000 SQL/sec（每条独立事务） | 1 tx/5s（含 ~4000 条 UPDATE） |
| Session preview 写入 | 500 SQL/sec via batcher | 1 tx/5s（含 ~1000 条 UPDATE） |
| 读查询 p99 延迟 | 5-15ms | 1-3ms（mmap + 32MB cache） |
| WAL 文件峰值大小 | 不可控（无显式管理） | ≤8MB（PASSIVE 检查点每 60s） |
| Read 连接等待时间 | 队头阻塞（8 conn） | 近零（16 并发连接） |
| Batcher 可靠性 | 1 个失败全 batch 回滚 | 逐条重试，只有真正失败的 job 报错 |
| SQLite 总写事务数/sec | ~2500 | ~2-3（coalescer 5s flush + batcher 100ms flush） |

---

## 未来考虑（500+ 用户规模）

当单 Hub 用户超过 500 时，SQLite 单文件的写串行化会成为真正的瓶颈。到时候的升级路径：

1. **读写库分离**：将只读副本（如审计日志、使用量统计）独立为单独的 SQLite 文件
2. **分片**：按 tenant_id 分库（每个租户独立 SQLite 文件）
3. **迁移到 PostgreSQL**：`store.Store` 接口层已经抽象好了，换存储引擎只需实现新的 repo 层

当前的 Phase 1-7 优化足以支撑 200-300 在线用户的场景。
