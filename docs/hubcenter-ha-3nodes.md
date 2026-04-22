# Hub Center 三节点热备部署

本文档描述第一版轻量级 Hub Center 多机热备方案：保留每台机器本地单文件 SQLite，不引入外部数据库、Raft 或复杂中间件，通过 3 台 Hub Center 之间的操作日志同步和 Hub 侧多地址故障转移来保证服务连续性。

## 适用目标

- 第一版固定按 3 台 Hub Center 部署，避免双节点脑裂和单点问题。
- 每台 Hub Center 独立使用本机 SQLite 文件，不共享数据库文件。
- Hub 客户端配置 3 个 Hub Center 地址，按质量探测结果选择可用节点。
- Hub Center 之间做最终一致同步，短暂网络抖动时优先保证服务可继续。

## 端口与地址示例

| 节点 | 内网地址 | 对外地址 | SQLite 文件 |
| --- | --- | --- | --- |
| `hc-1` | `http://10.0.0.11:9388` | `https://hc-1.example.com` | `./data/hubcenter-hc-1.db` |
| `hc-2` | `http://10.0.0.12:9388` | `https://hc-2.example.com` | `./data/hubcenter-hc-2.db` |
| `hc-3` | `http://10.0.0.13:9388` | `https://hc-3.example.com` | `./data/hubcenter-hc-3.db` |

`advertise_url` 和 `peers[].base_url` 必须是其他节点能访问到的地址。生产环境建议使用内网地址做节点间同步，公网域名只给 Hub 和管理端访问。

## Hub Center 配置

三台机器的 `ha.cluster_secret` 必须完全一致，并且要使用足够随机的值。不要使用示例里的 `change-me`。

### hc-1

```yaml
server:
  listen_host: 0.0.0.0
  listen_port: 9388
  public_base_url: https://hc-1.example.com

ha:
  enabled: true
  node_id: hc-1
  node_name: hubcenter-1
  advertise_url: http://10.0.0.11:9388
  cluster_secret: replace-with-a-long-random-shared-secret
  sync_interval_seconds: 3
  pull_batch_size: 200
  heartbeat_sync_min_interval_seconds: 10
  peers:
    - node_id: hc-2
      name: hubcenter-2
      base_url: http://10.0.0.12:9388
      enabled: true
    - node_id: hc-3
      name: hubcenter-3
      base_url: http://10.0.0.13:9388
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
  public_base_url: https://hc-2.example.com

ha:
  enabled: true
  node_id: hc-2
  node_name: hubcenter-2
  advertise_url: http://10.0.0.12:9388
  cluster_secret: replace-with-a-long-random-shared-secret
  sync_interval_seconds: 3
  pull_batch_size: 200
  heartbeat_sync_min_interval_seconds: 10
  peers:
    - node_id: hc-1
      name: hubcenter-1
      base_url: http://10.0.0.11:9388
      enabled: true
    - node_id: hc-3
      name: hubcenter-3
      base_url: http://10.0.0.13:9388
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
  public_base_url: https://hc-3.example.com

ha:
  enabled: true
  node_id: hc-3
  node_name: hubcenter-3
  advertise_url: http://10.0.0.13:9388
  cluster_secret: replace-with-a-long-random-shared-secret
  sync_interval_seconds: 3
  pull_batch_size: 200
  heartbeat_sync_min_interval_seconds: 10
  peers:
    - node_id: hc-1
      name: hubcenter-1
      base_url: http://10.0.0.11:9388
      enabled: true
    - node_id: hc-2
      name: hubcenter-2
      base_url: http://10.0.0.12:9388
      enabled: true

database:
  driver: sqlite
  dsn: ./data/hubcenter-hc-3.db
  wal: true
```

## Hub 配置

Hub 侧保留 `center.base_url` 兼容旧配置，同时新增 `center.base_urls`。第一版建议把 3 个 Hub Center 地址都写进去，Hub 会探测 `/api/client/quality` 后选择质量最高的节点注册和心跳。

```yaml
center:
  enabled: true
  base_url: https://hc-1.example.com
  base_urls:
    - https://hc-1.example.com
    - https://hc-2.example.com
    - https://hc-3.example.com
  register_on_startup: true
  heartbeat_interval_sec: 30
```

Hub 会记住上次成功的 Hub Center 地址。下一次注册或心跳时，如果该节点仍然可达且质量分接近最优，会优先复用；如果返回 `HUB_NOT_READY_ON_NODE` 或请求失败，会继续尝试下一个节点。

## 启动顺序

1. 分别在 3 台机器准备配置文件，确认 `node_id` 唯一、`cluster_secret` 一致、`peers` 不包含自己。
2. 先启动任意一台 Hub Center，再启动剩余两台。
3. 登录任意 Hub Center 管理后台，打开“多机热备”页确认 3 个节点状态。
4. 配置 Hub 的 `center.base_urls`，启动 Hub 或在管理端重新注册。

## 验证命令

检查单节点质量：

```powershell
Invoke-RestMethod https://hc-1.example.com/api/client/quality
```

检查客户端可用节点列表：

```powershell
Invoke-RestMethod https://hc-1.example.com/api/client/endpoints
```

检查节点间同步鉴权，未带 secret 应返回 `401`：

```powershell
Invoke-WebRequest https://hc-1.example.com/api/internal/ha/ops
```

带 secret 拉取同步日志：

```powershell
Invoke-RestMethod https://hc-1.example.com/api/internal/ha/ops?after_seq=0 -Headers @{ Authorization = "Bearer replace-with-a-long-random-shared-secret" }
```

管理后台接口需要管理员登录态，推荐直接打开 `/admin` 的“多机热备”页查看节点质量、延迟、同步位点和错误信息。

## 数据一致性规则

第一版采用操作日志同步，属于最终一致：

- 每个本地写入会生成一条 HA op，其他节点按游标增量拉取。
- 同一个实体并发更新时，先比较 `entity_version`，相同再比较 `occurred_at`，仍相同则比较 `source_node_id`，保证 3 台机器最终按同一结果收敛。
- Hub 心跳的在线时间会按 `heartbeat_sync_min_interval_seconds` 节流同步，避免高频心跳把同步日志刷爆。

## 运维注意事项

- 不要让 3 台 Hub Center 共享同一个 SQLite 文件。每台机器必须有自己的本地数据库。
- `cluster_secret` 只用于 Hub Center 节点间同步，应该只放在服务端配置里，不要暴露给 Hub 客户端。
- 建议只允许 3 台 Hub Center 的内网 IP 访问 `/api/internal/ha/ops`，即使接口已有 Bearer secret 校验也不要直接暴露到公网。
- 如果某台节点长时间离线，恢复后会从自己的 peer cursor 继续追同步日志。离线期间其他两台仍可对外服务。
- 如果 3 台之间网络完全分区，系统会继续接收本地写入，网络恢复后按确定性冲突规则收敛；强一致事务不属于第一版目标。
