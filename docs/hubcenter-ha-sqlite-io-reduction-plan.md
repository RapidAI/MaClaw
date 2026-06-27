# HubCenter HA SQLite IO 减少方案

## 问题描述

HubCenter 的 HA 机制在正常运行中产生过多 SQLite 磁盘 IO，主要来源是 Hub 心跳热路径。每个 Hub 客户端每 30-60 秒发送一次心跳，每次心跳在 HubCenter 侧触发多次 SQLite 写操作。当集群管理 50+ Hub 实例时，磁盘 IO 成为瓶颈。

## 根因分析

### 心跳热路径的完整写操作链（每次 `HeartbeatHubWithSecret` 调用）

| # | 操作 | 条件 | SQLite 写类型 |
|---|------|------|--------------|
| 1 | `hubs.GetByID(hubID)` | 无条件 | **读**（不计入） |
| 2 | `hubs.UpdateRegistration(hub)` | `update != nil`（首次心跳含注册信息） | 写：UPDATE hub_instances 全行 |
| 3 | `recordHubInstance(hub)` → `AppendHubInstance` → `AppendLocalWithVersion` | `update != nil` | 写：BEGIN IMMEDIATE + SELECT + UPSERT ha_entity_versions + INSERT ha_sync_ops + COMMIT（独占连接事务，不走 batcher） |
| 4 | `syncDomainRoutes(hub, domains)` | `update != nil` | 写：可能多次 UPSERT/DELETE hub_domain_routes + 对应 HA sync ops |
| 5 | `syncHubUserEmailInventoryFromCapabilities(hubID, caps)` | `update != nil && caps != nil` | 写：可能多次 UPSERT/DELETE hub_user_links + 对应 HA sync ops |
| 6 | **`hubs.UpdateHeartbeat(hubID, now)`** | **无条件** | **写：UPDATE hub_instances SET last_seen_at, updated_at（通过 batcher）** |
| 7 | `hubs.UpdateInvitationCodeRequired(hubID, required)` | `invitationCodeRequired != nil` | 写：UPDATE hub_instances |
| 8 | `ensureDefaultHubRegistrationPolicy(hubID)` | 无条件 | 读（策略已存在时 early return）或 写（首次创建策略） |
| 9 | `sync.SyncHubHeartbeat(hubID)` | `sync != nil`（HA 模式） | 读（throttled，600s 内只读 ha_heartbeat_sync_state）或 写（超过 600s 时执行 AppendLocalWithVersion + Upsert ha_heartbeat_sync_state） |
| 10 | `refreshRoutes(ctx)` | `refresher != nil` | 读：重建内存路由表（ListAll 操作） |

### 关键发现

**问题 1：`UpdateHeartbeat`（#6）无条件写入——最大的浪费**

每次心跳都执行 `UPDATE hub_instances SET last_seen_at=?, updated_at=? WHERE id=?`，即使 `last_seen_at` 距离上次写入只有 30 秒。这个写操作没有任何 HA 价值（HA sync 已在 #9 中被 600s 节流），纯粹是更新时间戳。

- 50 个 Hub × 每 30s 心跳 = **100 次/分钟无意义写入**

**问题 2：`AppendLocalWithVersion`（#3）绕过 writeBatcher 使用独占连接事务**

`AppendLocalWithVersion` 通过 `r.db.Conn(ctx)` 获取独占连接执行 `BEGIN IMMEDIATE` 事务。这完全绕过了 writeBatcher 的批量优化。每次调用是一个独立的磁盘 fsync 事务。

设计上这是合理的（需要原子读-改-写：读最新 seq → 去重比较 → 递增版本 → 插入），但在高频心跳场景下是 IO 热点。

**问题 3：Syncer 的 `cursors.Upsert` 在无变化时仍然写入**

Syncer 每 180s 轮询 peers。当 peer 无新 ops 时（`len(resp.Ops) == 0`），仍然执行 `cursors.Upsert` 更新 `last_pulled_at` 和 `last_success_at` 时间戳。对 3 节点集群，每 180s 产生 2 次 cursor 更新写入（即使没有任何数据变化）。

**问题 4：`refreshRoutes` 无条件重建内存路由表**

每次心跳都调用 `refresher.Rebuild(ctx)`，触发 `ListAll` 全表扫描。虽然是读操作，但加剧了 SQLite 锁竞争，间接影响写性能。

**问题 5：`ensureDefaultHubRegistrationPolicy` 每次心跳都执行**

虽然有 early return（策略已存在时只做一次 SELECT），但仍然是每次心跳一次无必要的数据库往返。

## 修复方案

### Fix 1 (P0): `UpdateHeartbeat` 内存节流——仅在间隔超过阈值时写盘

**机制**：在 `hubs.Service` 中维护 `lastHeartbeatWritten sync.Map`（key=hubID, value=time.Time）。`UpdateHeartbeat` 调用前检查距离上次写入是否超过 `heartbeatWriteInterval`（默认 5 分钟）。未超过则跳过写入。

**效果**：50 Hub × 30s 心跳的场景，写入频率从 100 次/分钟降到 **10 次/分钟**（每 Hub 每 5 分钟写一次）。

**风险**：`last_seen_at` 精度从 30s 降为 5 分钟。对"Hub 是否在线"的判断精度降低。但 HA 层的 `SyncHubHeartbeat` 已有 600s 节流，说明 5 分钟精度完全够用。可以通过 `Hub.Status` 字段（在 UpdateRegistration 中设为 "online"）保证在线状态正确，`last_seen_at` 只用于过期检测。

**代码位置**：`hubcenter/internal/hubs/service.go` 的 `HeartbeatHubWithSecret`

```go
// 新增字段
type Service struct {
    // ...
    heartbeatWriteThrottle sync.Map // map[string]time.Time — last UpdateHeartbeat write time per hubID
    heartbeatWriteInterval time.Duration // default 5 minutes
}

// HeartbeatHubWithSecret 中替换无条件 UpdateHeartbeat：
func (s *Service) shouldWriteHeartbeat(hubID string) bool {
    now := time.Now()
    if v, ok := s.heartbeatWriteThrottle.Load(hubID); ok {
        if now.Sub(v.(time.Time)) < s.heartbeatWriteInterval {
            return false
        }
    }
    s.heartbeatWriteThrottle.Store(hubID, now)
    return true
}
```

### Fix 2 (P1): `ensureDefaultHubRegistrationPolicy` 内存缓存——只在首次心跳时查库

**机制**：在 `hubs.Service` 中维护 `knownPolicyHubs sync.Map`（key=hubID, value=struct{}）。一旦该 Hub 的策略存在（SELECT 返回非空），加入 set。后续心跳直接 return。

**效果**：每个 Hub 的策略检查从 N 次/生命周期降为 1 次（首次心跳之后永不再查库）。

**风险**：策略被手动删除后不会自动重建。可通过 admin API 删除策略时同时清理 cache entry 解决。或设置 30 分钟过期。

**代码位置**：`hubcenter/internal/hubs/registration_policy.go`

```go
func (s *Service) ensureDefaultHubRegistrationPolicy(ctx context.Context, hubID string) error {
    // 内存缓存命中则跳过
    if _, ok := s.knownPolicyHubs.Load(hubID); ok {
        return nil
    }
    // ... 原有逻辑 ...
    s.knownPolicyHubs.Store(hubID, struct{}{})
    return nil
}
```

### Fix 3 (P1): `refreshRoutes` 节流——使用 dirty flag 而非每次重建

**机制**：`refresher` 加 dirty flag。只在实际修改了 routes/hubs（注册、域名变更等）时 mark dirty。`refreshRoutes` 检查 dirty flag，非 dirty 则跳过。

**效果**：心跳无 `update` 时不触发 Rebuild（绝大多数情况）。有 `update` 时 domain routes 可能变化，此时 Rebuild 是正确的。

**代码位置**：新增 `RouteRefresher` 接口方法 `MarkDirty()` 和 `RebuildIfDirty(ctx)`

```go
type routeRefresher interface {
    Rebuild(context.Context) error
    MarkDirty()
    RebuildIfDirty(context.Context) error
}
```

修改 `refreshRoutes`：
```go
func (s *Service) refreshRoutes(ctx context.Context) {
    if r, ok := s.refresher.(interface{ RebuildIfDirty(context.Context) error }); ok {
        _ = r.RebuildIfDirty(ctx)
        return
    }
    if s.refresher != nil {
        _ = s.refresher.Rebuild(ctx)
    }
}
```

只在 `syncDomainRoutes`、`UpdateRegistration`、`DeleteByID` 等真正改变路由的操作后调用 `MarkDirty()`。

### Fix 4 (P1): Syncer cursor 写入节流——无新 ops 时不更新时间戳

**机制**：当 `len(resp.Ops) == 0` 且 cursor 的 `LastPulledSeq` 已等于 `resp.NextAfterSeq` 时，跳过 `cursors.Upsert`。只更新内存中的 peer sync status。

**效果**：静态集群（无数据变化时），每 180s 的 sync 轮询从 2 次 cursor 写入降为 0 次。

**代码位置**：`hubcenter/internal/ha/syncer.go` 的 `syncPeer`

```go
if len(resp.Ops) == 0 {
    nextSeq := resp.NextAfterSeq
    if nextSeq < afterSeq {
        nextSeq = afterSeq
    }
    // 只在 cursor 位置真正变化时写盘
    if cursor != nil && cursor.LastPulledSeq == nextSeq {
        // 位置没变，只更新内存状态
        s.svc.updatePeerSync(peer.NodeID, 0)
        return
    }
    // 位置变了（首次同步或 gap-forward），写盘
    _ = s.svc.cursors.Upsert(ctx, &store.HAPeerCursor{...})
    ...
}
```

### Fix 5 (P2): `normalizedNoisyHAPayload` 扩展到所有 entity types

**机制**：`haPayloadEquivalent` 中的 `normalizedNoisyHAPayload` 当前只处理 4 种 entity type（hub_instance、hub_user_link、hub_domain_route、llm_card_order）。对其他 entity type 直接返回 `(nil, false)`，跳过去重——即使 payload 完全相同也会创建新 op。

扩展为通用去重：对所有 entity type，统一 delete `updated_at`，然后做 hash/DeepEqual 比较。

**效果**：`system_setting`、`gossip_snapshot`、`skillhub_snapshot`、`blocked_email` 等 entity type 的重复 upsert 被 payload 去重拦截。

**代码位置**：`hubcenter/internal/store/sqlite/ha_repo.go`

```go
func normalizedNoisyHAPayload(entityType, payloadJSON string) (map[string]any, bool) {
    var payload map[string]any
    if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
        return nil, false
    }
    // 所有 entity type 统一去除时间戳噪音字段
    delete(payload, "updated_at")
    delete(payload, "created_at") // 某些 entity 的 created_at 也是噪音
    switch entityType {
    case "hub_instance":
        delete(payload, "last_seen_at")
    }
    return payload, true
}
```

### Fix 6 (P2): `AppendLocalWithVersion` 内存预检——version cache 跳过无变化写入

**机制**：维护 `entityVersionCache sync.Map`（key=entityType+":"+entityID, value=lastPayloadHash）。`AppendLocalWithVersion` 在获取独占连接之前先做内存预检：如果 payload hash 与缓存一致，直接返回（零 IO）。

这是对 `haPayloadEquivalent` 检测的**前置加速**——将原本需要 BEGIN IMMEDIATE + SELECT + COMMIT 的去重检测变为纯内存比较。

**效果**：重复 payload（心跳场景最常见）从"获取独占连接 → BEGIN → SELECT → 比较 → COMMIT"变为"内存 hash 比较 → return"。

**风险**：进程重启后 cache 为空，第一次写入总是执行全路径。cache 和磁盘不一致时（并发）最坏情况是多写一次（仍然有磁盘层去重兜底）。

**代码位置**：`hubcenter/internal/store/sqlite/ha_repo.go`

```go
type haSyncOpRepo struct {
    db     *sql.DB
    readDB *sql.DB
    batch  *writeBatcher
    // 新增：payload hash 前置缓存
    lastPayloadHash sync.Map // key: "entityType:entityID" → value: payloadHash string
}

func (r *haSyncOpRepo) AppendLocalWithVersion(ctx context.Context, op *store.HASyncOp) (int64, error) {
    if op == nil {
        return 0, errors.New("nil ha sync op")
    }
    
    // 内存预检：payload hash 命中则跳过磁盘操作
    cacheKey := op.EntityType + ":" + op.EntityID
    if op.PayloadHash != "" {
        if cached, ok := r.lastPayloadHash.Load(cacheKey); ok && cached.(string) == op.PayloadHash {
            return 0, nil // 快速路径：payload 未变化
        }
    }
    
    // ... 原有磁盘路径 ...
    
    // 写入成功后更新 cache
    if op.PayloadHash != "" {
        r.lastPayloadHash.Store(cacheKey, op.PayloadHash)
    }
    return version, nil
}
```

## 预期效果汇总

| Fix | 场景 | 写操作减少量 | 延迟影响 |
|-----|------|------------|---------|
| Fix 1 | 50 Hub × 30s 心跳 | -90 次/分钟（从 100 降到 10） | `last_seen_at` 精度从 30s 降为 5min |
| Fix 2 | 每次心跳的策略检查 | -100 次/分钟（全部变为内存查 Set） | 无 |
| Fix 3 | 每次心跳的路由重建 | 0（是读操作，但减少锁竞争） | 无 |
| Fix 4 | 静态集群的 syncer | -0.67 次/分钟（每 180s 少 2 次） | cursor 时间戳精度降低 |
| Fix 5 | 相同 payload 的 HA ops | 取决于重复写频率 | 无 |
| Fix 6 | AppendLocalWithVersion 热路径 | 大量（每次重复 payload 跳过独占事务） | 无 |

**总计**：正常运行（50 Hub、3 节点集群）时，SQLite 写 IO 从 ~200 ops/分钟降到 ~10 ops/分钟。降幅 **95%**。

## 实施优先级

1. **Fix 1**（P0）：影响最大，实现最简单。单独就能解决 90% 的问题。
2. **Fix 6**（P1）：与 Fix 1 互补——Fix 1 减少 heartbeat 写入，Fix 6 减少 HA op 写入的独占事务开销。
3. **Fix 2**（P1）：简单且无风险。
4. **Fix 3**（P1）：减少读竞争，对写性能有间接提升。
5. **Fix 4**（P1）：影响小但实现简单。
6. **Fix 5**（P2）：需要验证所有 entity type 都能安全去除 `updated_at`。

## 配置化

所有阈值通过 `HAConfig` 暴露为可配置项：

```yaml
ha:
  heartbeat_write_interval_seconds: 300    # Fix 1: UpdateHeartbeat 写入间隔
  policy_cache_ttl_minutes: 30             # Fix 2: 策略缓存过期时间
  route_rebuild_debounce_ms: 1000          # Fix 3: 路由重建防抖
  cursor_skip_unchanged: true              # Fix 4: cursor 无变化时跳过写入
  payload_dedup_all_entities: true          # Fix 5: 所有 entity type 去重
  version_cache_precheck: true             # Fix 6: payload hash 内存预检
```
