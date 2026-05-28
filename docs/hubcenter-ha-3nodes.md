# Hub Center 三节点热备部署

本文档描述第一版轻量级 Hub Center 多机热备方案：保留每台机器本地单文件 SQLite，不引入外部数据库、Raft 或复杂中间件，通过 3 台 Hub Center 之间的操作日志同步和 Hub 侧多地址故障转移来保证服务连续性。

上线前建议配合 [Hub Center 的 HA 配置使用手册](/D:/workprj/aicoder/docs/hubcenter的HA配置使用手册.md)、[Hub Center HA 上线操作 SOP](/D:/workprj/aicoder/docs/hubcenter-ha-ops-sop.md) 和 [Hub Center 三节点首发上线检查清单](/D:/workprj/aicoder/docs/hubcenter-ha-go-live-checklist.md) 一起使用。

## 1. 目标

- 第一版固定按 3 台 `hubcenter` 部署，避免双节点脑裂和单点故障。
- 每台 `hubcenter` 独立使用本机 SQLite 文件，不共享数据库文件。
- `hub` 客户端内置 3 个 `hubcenter` 地址，按质量探测结果选择当前服务节点。
- `hubcenter` 之间做最终一致同步，短时网络抖动时优先保证服务可继续。

## 2. 你们当前推荐拓扑

当前已确认的公网域名：

- `hubcenter` 节点
- `https://hubs.mypapers.top`
- `https://hubs.maclaw.top`
- `https://hubs2.maclaw.top`

- `hub` 域名
- `https://hub.mypapers.top`
- `https://hub.maclaw.top`

推荐节点映射：

| 节点 ID | 节点名称 | 对外地址 |
| --- | --- | --- |
| `hc-1` | `hubcenter-1` | `https://hubs.mypapers.top` |
| `hc-2` | `hubcenter-2` | `https://hubs.maclaw.top` |
| `hc-3` | `hubcenter-3` | `https://hubs2.maclaw.top` |

你们当前没有内网，所以 `advertise_url` 和 `peers[].base_url` 都直接使用公网地址。

## 3. Hub Center 配置示例

三台机器的 `ha.cluster_secret` 必须完全一致，并且要使用足够随机的值。

### hc-1

```yaml
server:
  listen_host: 0.0.0.0
  listen_port: 9388
  public_base_url: https://hubs.mypapers.top

ha:
  enabled: true
  node_id: hc-1
  node_name: hubcenter-1
  advertise_url: https://hubs.mypapers.top
  cluster_secret: replace-with-a-long-random-shared-secret
  sync_interval_seconds: 180
  push_debounce_seconds: 180
  pull_batch_size: 200
  heartbeat_sync_min_interval_seconds: 600
  history_retention_days: 0.5
  history_max_retained_ops: 50000
  history_prune_interval_minutes: 10
  history_prune_batch_size: 20000
  peers:
    - node_id: hc-2
      name: hubcenter-2
      base_url: https://hubs.maclaw.top
      enabled: true
    - node_id: hc-3
      name: hubcenter-3
      base_url: https://hubs2.maclaw.top
      enabled: true

database:
  driver: sqlite
  dsn: ./data/hubcenter-hc-1.db
  wal: true
```

### hc-2

```yaml
server:
  listen_host: 0.0.0.0
  listen_port: 9388
  public_base_url: https://hubs.maclaw.top

ha:
  enabled: true
  node_id: hc-2
  node_name: hubcenter-2
  advertise_url: https://hubs.maclaw.top
  cluster_secret: replace-with-a-long-random-shared-secret
  sync_interval_seconds: 180
  push_debounce_seconds: 180
  pull_batch_size: 200
  heartbeat_sync_min_interval_seconds: 600
  history_retention_days: 0.5
  history_max_retained_ops: 50000
  history_prune_interval_minutes: 10
  history_prune_batch_size: 20000
  peers:
    - node_id: hc-1
      name: hubcenter-1
      base_url: https://hubs.mypapers.top
      enabled: true
    - node_id: hc-3
      name: hubcenter-3
      base_url: https://hubs2.maclaw.top
      enabled: true

database:
  driver: sqlite
  dsn: ./data/hubcenter-hc-2.db
  wal: true
```

### hc-3

```yaml
server:
  listen_host: 0.0.0.0
  listen_port: 9388
  public_base_url: https://hubs2.maclaw.top

ha:
  enabled: true
  node_id: hc-3
  node_name: hubcenter-3
  advertise_url: https://hubs2.maclaw.top
  cluster_secret: replace-with-a-long-random-shared-secret
  sync_interval_seconds: 180
  push_debounce_seconds: 180
  pull_batch_size: 200
  heartbeat_sync_min_interval_seconds: 600
  history_retention_days: 0.5
  history_max_retained_ops: 50000
  history_prune_interval_minutes: 10
  history_prune_batch_size: 20000
  peers:
    - node_id: hc-1
      name: hubcenter-1
      base_url: https://hubs.mypapers.top
      enabled: true
    - node_id: hc-2
      name: hubcenter-2
      base_url: https://hubs.maclaw.top
      enabled: true

database:
  driver: sqlite
  dsn: ./data/hubcenter-hc-3.db
  wal: true
```

已生成好的草案文件：

- [hubcenter-hc-1.yaml](/D:/workprj/aicoder/deploy/out-mypapers-maclaw/hubcenter-hc-1.yaml)
- [hubcenter-hc-2.yaml](/D:/workprj/aicoder/deploy/out-mypapers-maclaw/hubcenter-hc-2.yaml)
- [hubcenter-hc-3.yaml](/D:/workprj/aicoder/deploy/out-mypapers-maclaw/hubcenter-hc-3.yaml)

## 4. Hub 配置

Hub 侧保留 `center.base_url` 兼容旧配置，同时新增 `center.base_urls`。第一版建议把 3 个 Hub Center 地址都写进去。

```yaml
center:
  enabled: true
  base_url: https://hubs.mypapers.top
  base_urls:
    - https://hubs.mypapers.top
    - https://hubs.maclaw.top
    - https://hubs2.maclaw.top
  register_on_startup: true
  heartbeat_interval_sec: 30
```

Hub 会：

- 探测 `/api/client/quality`
- 优先选择质量分更高的 `hubcenter`
- 记住上次成功节点
- 当前节点不可用或返回 `HUB_NOT_READY_ON_NODE` 时，自动尝试下一个节点

## 5. 管理后台配置建议

现在 `hubcenter` 管理后台的“多机热备”页已经支持直接编辑 HA 参数。

推荐流程：

1. 先用 YAML 把 3 台节点启动起来。
2. 登录各自后台，在“多机热备”页核对本机 `node_id`、`advertise_url`、`cluster_secret` 和 peer 列表。
3. 如需调参，可以直接在后台保存。
4. 保存后逐台重启 `hubcenter`，让同步线程和探测线程加载新参数。
5. 最后回到“多机热备”页确认 3 个节点都可达。

## 6. 启动顺序

1. 准备好 3 台 `hubcenter` 的配置文件。
2. 先启动任意 1 台 `hubcenter`。
3. 再启动剩余 2 台 `hubcenter`。
4. 登录任意一台 `hubcenter` 管理后台，确认“多机热备”页能看到 3 个节点。
5. 再启动 `hub`。
6. 检查 `hub` 是否成功注册，并能正常发送心跳。

## 7. 验证命令

检查单节点质量：

```powershell
Invoke-RestMethod https://hubs.mypapers.top/api/client/quality
```

检查客户端可用节点列表：

```powershell
Invoke-RestMethod https://hubs.mypapers.top/api/client/endpoints
```

检查节点间同步鉴权，未带 secret 应返回 `401`：

```powershell
Invoke-WebRequest https://hubs.mypapers.top/api/internal/ha/ops
```

带 secret 拉取同步日志：

```powershell
Invoke-RestMethod https://hubs.mypapers.top/api/internal/ha/ops?after_seq=0 -Headers @{ Authorization = "Bearer replace-with-a-long-random-shared-secret" }
```

或直接使用验收脚本：

```powershell
.\deploy\check-hubcenter-ha.ps1 `
  -CenterUrls https://hubs.mypapers.top,https://hubs.maclaw.top,https://hubs2.maclaw.top `
  -ClusterSecret '你的真实cluster_secret'
```

## 8. 数据一致性规则

第一版采用操作日志同步，属于最终一致：

- 每个本地写入会生成一条 HA op，其它节点按游标增量拉取。
- 同一实体并发更新时，先比较 `entity_version`，相同再比较 `occurred_at`，仍相同则比较 `source_node_id`。
- Hub 心跳的在线时间会按 `heartbeat_sync_min_interval_seconds` 节流同步，避免高频心跳把同步日志刷爆。

## 9. 运维注意事项

- 不要让 3 台 `hubcenter` 共享同一个 SQLite 文件。
- `cluster_secret` 只用于 `hubcenter` 节点间同步，不要暴露给 `hub` 客户端。
- 后台改完 HA 参数后，记得同步回 YAML 或运维配置文件，避免后续重启又回到旧配置。
- 如果某台节点长期离线，恢复后会从 peer cursor 继续追同步日志，其它两台仍可继续对外服务。
- 如果 3 台之间出现网络分区，系统会优先保持本地服务可用，网络恢复后再按确定性规则收敛。
