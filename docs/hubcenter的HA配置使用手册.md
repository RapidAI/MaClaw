# hubcenter 的 HA 配置使用手册

本文档说明 `hubcenter` 第一版 3 节点 HA 的配置方式、后台页面使用方法、字段含义、推荐示例、启动顺序和排障方法。

## 1. 方案范围

当前 HA 方案的边界是：

- 固定 3 台 `hubcenter`
- 每台机器独立使用本地 SQLite 文件
- `hubcenter` 之间通过 HA op 日志做最终一致同步
- `hub` 侧配置多个 `hubcenter` 地址并自动故障转移
- 不引入外部数据库、共享存储、Raft 或主从切换组件

这套方案适合你们现在的公网部署方式，也就是节点之间直接通过公网域名互相访问。

## 2. 配置入口

现在 HA 参数有两个配置入口：

- 启动 YAML 配置文件
- `hubcenter` 管理后台的“多机热备”页

推荐做法：

- 第一次部署时，先用 YAML 启动出 3 台节点
- 上线后，日常调参优先在后台页面修改
- 把后台确认过的参数再同步回运维配置文件，避免后续重启时不一致

## 3. 后台页面配置方法

登录 `hubcenter` 管理后台后，进入“多机热备”页，现在页面除了查看状态，还可以直接编辑 HA 配置。

页面可配置项包括：

- `Enable HA on this node`
- `Node ID`
- `Node name`
- `Advertise URL`
- `Cluster secret`
- `Sync interval`
- `Pull batch size`
- `Heartbeat sync minimum interval`
- peer 列表

页面里还新增了两个很实用的辅助能力：

- 运行态/已保存配置对比状态卡，可以直接看出当前是否需要重启
- 一键复制节点摘要、YAML 片段和上线清单，方便把三台机器配置横向比对

推荐操作步骤：

1. 在 3 台机器上分别登录对应的 `hubcenter` 后台。
2. 在每台机器的“多机热备”页填写本机的 `node_id`、`node_name`、`advertise_url`。
3. 在 3 台机器上填写完全相同的 `cluster_secret`。
4. 在每台机器上添加另外两台为 peer，不要把自己写进 peer 列表。
5. 页面会先检查是否把自己填进 peer、是否有重复 peer ID、是否有重复 peer URL。
6. 点击“保存 HA 配置”。
7. 如有需要，可点击“复制上线清单”发给运维同学逐项核对。
8. 按节点顺序重启 `hubcenter`，让探测线程和同步线程完整加载新参数。
9. 重启后回到“多机热备”页确认 3 个节点都可达。


需要注意：

- 配置会立即保存到本机数据库。
- 但当前版本的 HA goroutine 是启动时创建的，所以保存后要重启 `hubcenter` 才能完整生效。
- 如果只保存不重启，页面里的配置值已经变更，但运行中的同步线程仍可能继续使用旧参数。

## 4. 核心 YAML 配置段

`hubcenter` 的 HA 配置位于 YAML 的 `ha:` 段：

```yaml
ha:
  enabled: true
  node_id: hc-1
  node_name: hubcenter-1
  advertise_url: https://hubs.mypapers.top
  cluster_secret: replace-with-a-long-random-shared-secret
  sync_interval_seconds: 3
  pull_batch_size: 200
  heartbeat_sync_min_interval_seconds: 10
  peers:
    - node_id: hc-2
      name: hubcenter-2
      base_url: https://hubs.maclaw.top
      enabled: true
    - node_id: hc-3
      name: hubcenter-3
      base_url: https://hubs2.maclaw.top
      enabled: true
```

## 5. 字段说明

### `ha.enabled`

- `true` 表示开启多节点 HA
- `false` 表示单节点独立运行

### `ha.node_id`

- 当前节点的唯一标识
- 在 3 台机器之间必须不同
- 推荐用固定值，例如 `hc-1`、`hc-2`、`hc-3`

### `ha.node_name`

- 当前节点的展示名
- 主要用于管理后台和状态页面显示
- 可以与机器名一致，也可以写成业务含义更明确的名字

### `ha.advertise_url`

- 当前节点告诉其他节点“应该如何访问我”的地址
- 在你们没有内网的情况下，直接写公网域名即可
- 必须能被其他 `hubcenter` 节点访问到

### `ha.cluster_secret`

- `hubcenter` 节点之间同步时使用的 Bearer secret
- 3 台机器必须完全相同
- 不能留空
- 不应该暴露给 `hub` 或客户端

### `ha.sync_interval_seconds`

- 节点拉取远端 HA op 的周期
- 默认 `3` 秒，第一版建议保持默认
- 值越小，同步越快，但请求更频繁

### `ha.pull_batch_size`

- 单次从 peer 拉取的最大 op 数量
- 默认 `200`

### `ha.heartbeat_sync_min_interval_seconds`

- Hub 在线心跳写入 HA 日志的最小间隔
- 默认 `10` 秒
- 用来避免心跳太频繁导致同步日志过多

### `ha.peers`

- 当前节点认识的其他 `hubcenter` 节点列表
- 不要把自己写进 `peers`
- 建议每台都写另外两台

每个 peer 项包含：

- `node_id`: 对端节点唯一 ID
- `name`: 对端节点展示名
- `base_url`: 当前节点访问对端时使用的地址
- `enabled`: 是否启用该 peer

## 6. 启动时的配置校验

程序在加载配置时会自动校验以下问题，如果不满足会直接启动失败：

- `ha.enabled=true` 时，`ha.node_id` 不能为空
- `ha.enabled=true` 时，`ha.advertise_url` 不能为空
- `ha.enabled=true` 时，`ha.cluster_secret` 不能为空
- 至少要有一个启用的 `peer`
- `peer.node_id` 不能为空
- `peer.node_id` 不能等于自己的 `ha.node_id`
- `peer.node_id` 不能重复
- `peer.base_url` 不能重复

## 7. 你们当前域名的推荐配置

你们现在已经确定的 3 个 `hubcenter` 公网域名是：

- `https://hubs.mypapers.top`
- `https://hubs.maclaw.top`
- `https://hubs2.maclaw.top`

推荐映射关系：

- `hc-1` 对应 `https://hubs.mypapers.top`
- `hc-2` 对应 `https://hubs.maclaw.top`
- `hc-3` 对应 `https://hubs2.maclaw.top`

已生成好的配置草案：

- [hubcenter-hc-1.yaml](/D:/workprj/aicoder/deploy/out-mypapers-maclaw/hubcenter-hc-1.yaml)
- [hubcenter-hc-2.yaml](/D:/workprj/aicoder/deploy/out-mypapers-maclaw/hubcenter-hc-2.yaml)
- [hubcenter-hc-3.yaml](/D:/workprj/aicoder/deploy/out-mypapers-maclaw/hubcenter-hc-3.yaml)

## 8. 与 hub 的配合关系

`hubcenter` 的 HA 只完成节点间数据同步，真正的接入故障切换由 `hub` 侧完成。

`hub` 侧应配置：

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

`hub` 会：

- 先探测 `/api/client/quality`
- 选质量更高的 `hubcenter`
- 记住上次成功节点
- 当前节点不可用或返回 `HUB_NOT_READY_ON_NODE` 时自动尝试下一个

## 9. 数据同步的内容

当前 HA 已覆盖的主要实体包括：

- `blocked_emails`
- `blocked_ips`
- `news_articles`
- `hub_instances`
- `hub_user_links`

同步模型是最终一致，不是强一致事务。

并发更新时，系统会按以下规则收敛：

1. 先比较 `entity_version`
2. 相同则比较 `occurred_at`
3. 仍相同则比较 `source_node_id`

另外，同节点本地并发写现在已做串行版本分配，避免出现重复版本号。

## 10. 推荐启动顺序

1. 准备好 3 台 `hubcenter` 的配置文件或后台参数。
2. 先启动任意 1 台 `hubcenter`。
3. 再启动剩余 2 台 `hubcenter`。
4. 登录任意一台管理后台，确认“多机热备”页能看到 3 个节点。
5. 如刚通过后台修改过 HA 配置，确认 3 台都已经重启。
6. 再启动 `hub`。
7. 检查 `hub` 是否成功注册，并能正常发送心跳。

## 11. 推荐验收方式

最直接的验收命令：

```powershell
.\deploy\check-hubcenter-ha.ps1 `
  -CenterUrls https://hubs.mypapers.top,https://hubs.maclaw.top,https://hubs2.maclaw.top `
  -ClusterSecret '你的真实cluster_secret'
```

这个脚本会检查：

- `/healthz`
- `/api/client/quality` 返回可路由状态
- `/api/client/endpoints` 至少返回 3 个节点
- `/api/internal/ha/ops` 未授权访问是否返回 `401`
- `/api/internal/ha/ops` 带 secret 后是否返回 `200`

## 12. 常见错误

### 错误 1：3 台机器的 `cluster_secret` 不一致

现象：

- peer 长期 `isolated`
- `/api/internal/ha/ops` 返回未授权
- backlog 不回落

处理：

- 确认 3 台机器的 `ha.cluster_secret` 完全一致

### 错误 2：把自己写进 `peers`

现象：

- 启动时报配置校验错误

处理：

- 每台机器的 `peers` 只写另外两台

### 错误 3：peer 地址写错

现象：

- 质量页显示节点不可达
- peer RTT 或 `last_success_at` 不刷新

处理：

- 检查 `ha.advertise_url` 和 `peers[].base_url` 是否真实可访问

### 错误 4：后台保存后没有重启

现象：

- 页面上看到新配置，但运行状态像旧参数
- 同步周期、peer 信息或心跳同步行为没有按预期变化

处理：

- 保存后逐台重启 `hubcenter`
- 重启后重新查看“多机热备”页状态

### 错误 5：Hub 只写了单个 center 地址

现象：

- 单个 `hubcenter` 故障后，`hub` 不会切换

处理：

- 在 `hub` 中配置完整的 `center.base_urls`

## 13. 运维建议

- 每台 `hubcenter` 使用独立 SQLite 文件，不共享 `.db`
- 定期在“多机热备”页查看 `quality_score`、`lag_seconds`、`backlog`、`last_error`
- 第一版先保持默认同步参数，不建议一开始就频繁调大或调小
- 后台改完参数后，记得同步回运维配置文件，避免后续人工重启把旧 YAML 又带回来
