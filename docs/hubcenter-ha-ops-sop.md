# Hub Center HA 上线操作 SOP

本文档用于 3 节点 `hubcenter` 首次上线、后台改参后重启生效，以及上线后的快速验收。

适用节点：

- `hc-1` -> `https://hubs.mypapers.top`
- `hc-2` -> `https://hubs.maclaw.top`
- `hc-3` -> `https://hubs2.maclaw.top`

## 1. 上线前准备

确认以下条件全部满足：

- 3 台机器都已部署 `hubcenter`
- 每台机器都有独立 SQLite 文件
- 3 台机器之间可通过公网互相访问
- 3 台机器都已准备好一致的 `cluster_secret`
- `hub` 侧已准备 `center.base_urls`

建议提前准备：

- 同一个 `cluster_secret`
- 3 份 YAML 配置草案
- 一份记录表，写清每台机器的 `node_id`、域名、peer 列表

## 2. 节点映射

| 节点 ID | 节点名称 | 对外地址 | peer 目标 |
| --- | --- | --- | --- |
| `hc-1` | `HubCenter 1` | `https://hubs.mypapers.top` | `hc-2`、`hc-3` |
| `hc-2` | `HubCenter 2` | `https://hubs.maclaw.top` | `hc-1`、`hc-3` |
| `hc-3` | `HubCenter 3` | `https://hubs2.maclaw.top` | `hc-1`、`hc-2` |

## 3. 后台配置步骤

在每台机器分别登录 `hubcenter` 后台，进入“多机热备”页，按下面顺序填写：

1. 勾选 `Enable HA on this node`
2. 选择对应模板按钮：
   - `hc-1` 机器点 `Use hc-1 Template`
   - `hc-2` 机器点 `Use hc-2 Template`
   - `hc-3` 机器点 `Use hc-3 Template`
3. 核对本机字段：
   - `Node ID`
   - `Node name`
   - `Advertise URL`
4. 在任意一台点击 `Generate Secret`
5. 把同一个 `cluster_secret` 复制到 3 台机器
6. 确认每台机器的 peer 列表里只有另外两台，不包含自己
7. 保持默认参数：
   - `Sync interval = 3`
   - `Pull batch size = 200`
   - `Heartbeat sync minimum interval = 10`
8. 点击 `Save HA Config`

## 4. 保存后检查

每台机器保存后，立即检查：

- readiness 卡片显示“已具备上线条件”
- runtime 卡片如果提示“待重启”是正常现象
- 页面没有提示重复 peer ID、重复 peer URL、self peer
- `Node Summary` 可正常复制

如果 readiness 仍未通过，优先检查：

- 是否启用了 HA
- `node_id` 是否为空
- `advertise_url` 是否为空
- `cluster_secret` 是否为空或过短
- 是否只配了 1 个 peer

## 5. 重启顺序

后台保存后，按下面顺序逐台重启：

1. 重启 `hc-1`
2. 等 `hc-1` 健康后，重启 `hc-2`
3. 等 `hc-2` 健康后，重启 `hc-3`

每台重启后先确认：

- `/healthz` 返回 `200`
- 后台可登录
- “多机热备”页可打开

注意：

- 只保存不重启，已保存配置不会完整进入运行态
- runtime 卡片只有在运行配置和已保存配置一致后才会变成同步状态

## 6. Hub 侧配置

确保 `hub` 配置了完整的 3 个地址：

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

上线时建议在 3 台 `hubcenter` 全部完成并健康后，再启动 `hub`。

## 7. 验收命令

推荐直接运行：

```powershell
.\deploy\check-hubcenter-ha.ps1 `
  -CenterUrls https://hubs.mypapers.top,https://hubs.maclaw.top,https://hubs2.maclaw.top `
  -ClusterSecret '你的真实cluster_secret'
```

通过标准：

- 每个节点 `/healthz = 200`
- `quality_status` 正常
- `routable = true`
- `endpoints_count >= 3`
- `ha_auth = unauthorized:401;authorized:200`

也可以手工抽查：

```powershell
Invoke-RestMethod https://hubs.mypapers.top/api/client/quality
Invoke-RestMethod https://hubs.mypapers.top/api/client/endpoints
Invoke-WebRequest https://hubs.mypapers.top/api/internal/ha/ops?after_seq=0&limit=1
Invoke-WebRequest https://hubs.mypapers.top/api/internal/ha/ops?after_seq=0&limit=1 -Headers @{ Authorization = "Bearer 你的cluster_secret" }
```

## 8. 异常处理

### 现象：`ha_auth` 不是 `unauthorized:401;authorized:200`

处理：

- 检查 3 台机器 `cluster_secret` 是否完全一致
- 检查 `/api/internal/ha/ops` 的鉴权配置是否生效
- 检查是否误填了旧 secret

### 现象：peer 长期不可达

处理：

- 检查 peer `base_url` 是否写对
- 检查目标节点公网域名是否能访问
- 检查目标节点服务是否真的启动

### 现象：页面显示已保存，但运行态不一致

处理：

- 先确认该节点是否已经重启
- 再刷新“多机热备”页
- 如仍不一致，检查是否后台改完后又被旧 YAML 覆盖

### 现象：Hub 没有切换到健康节点

处理：

- 检查 `hub` 是否真的配置了 `center.base_urls`
- 检查 `hub` 是否能访问 3 个 `/api/client/quality`
- 检查当前节点是否返回了异常状态或 `HUB_NOT_READY_ON_NODE`

## 9. 上线后日常巡检

建议每天或每次发布后检查：

- 3 个节点是否都能打开“多机热备”页
- `quality_score` 是否异常下降
- `lag_seconds` 是否长期偏大
- `backlog` 是否持续不回落
- `last_error` 是否持续出现同类错误

## 10. 关联文档

- [hubcenter 的 HA 配置使用手册](/D:/workprj/aicoder/docs/hubcenter的HA配置使用手册.md)
- [Hub Center 三节点热备部署](/D:/workprj/aicoder/docs/hubcenter-ha-3nodes.md)
- [Hub Center 三节点首发上线检查清单](/D:/workprj/aicoder/docs/hubcenter-ha-go-live-checklist.md)
