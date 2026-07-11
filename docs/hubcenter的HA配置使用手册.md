# hubcenter 的 HA 配置使用手册

本文档说明 `hubcenter` 第一版 3 节点 HA 的配置方式、管理后台使用方法、推荐部署目录、`hub`/`hubcenter` 的配合关系，以及 `deploy_all.cmd` 的自动化部署约定。

## 1. 方案范围

当前方案边界如下：

- 固定 3 台 `hubcenter`
- 每台机器独立使用本地 SQLite 文件
- `hubcenter` 节点之间通过 HA op 日志做最终一致同步
- `hub` 侧配置多个 `hubcenter` 地址并自动做故障切换
- 不引入外部数据库、共享存储、Raft 或主从切换组件
- 当前部署环境按公网直连设计，不依赖内网

这套方案适合你们现在的三机首发场景，重点是简单、可落地、没有单点。

## 2. 当前拓扑

### hubcenter 节点

- `hc-1` -> `https://hubs.mypapers.top`
- `hc-2` -> `https://hubs.maclaw.top`
- `hc-3` -> `https://hubs2.maclaw.top`

### hub 域名

- `https://hub.mypapers.top`
- `https://hub.maclaw.top`

### 推荐映射

| 节点 ID | 节点名称 | 对外地址 |
| --- | --- | --- |
| `hc-1` | `hubcenter-1` | `https://hubs.mypapers.top` |
| `hc-2` | `hubcenter-2` | `https://hubs.maclaw.top` |
| `hc-3` | `hubcenter-3` | `https://hubs2.maclaw.top` |

你们当前没有内网，所以 `advertise_url` 和 `peers[].base_url` 都直接填写公网地址。

## 3. 管理后台配置入口

现在 HA 参数有两个入口：

- 启动 YAML 配置文件
- `hubcenter` 管理后台中的“多机热备”页

推荐做法：

1. 首次部署时先用 YAML 或自动部署脚本启动 3 台机器。
2. 上线后日常调参优先在后台页面操作。
3. 后台确认过的参数，再同步回运维配置文件，避免后续重启时回退到旧值。

## 4. 后台页面操作方法

登录任意一台 `hubcenter` 管理后台，进入“多机热备”页。

页面支持直接配置：

- `Enable HA on this node`
- `Node ID`
- `Node name`
- `Advertise URL`
- `Cluster secret`
- `Sync interval`
- `Push debounce interval`
- `Pull batch size`
- `Heartbeat sync minimum interval`
- `peers` 列表

页面还提供以下辅助能力：

- `hc-1` / `hc-2` / `hc-3` 模板按钮
- 3 节点部署卡片：直接展示每台机器应填写的 `node_id`、对外地址和 peer 列表
- 每张部署卡都支持“套用到表单 / 复制 YAML / 复制上线清单”
- `Generate Secret`、显示/隐藏、复制按钮
- readiness 状态卡
- runtime 与 saved config 对比卡
- 节点摘要复制功能
- 前端校验：禁止 self peer、重复 peer ID、重复 peer URL

推荐操作顺序：

1. 在 3 台机器分别登录对应后台。
2. 选择当前节点对应模板。
3. 核对本机 `node_id`、`node_name`、`advertise_url`。
4. 在任意一台生成 `cluster_secret`，然后复制到 3 台机器。
5. 每台机器只填写另外两台为 peer，不要把自己写进 `peers`。
6. 点击保存。
7. 可以直接使用页面上方的 3 节点部署卡片，对照每台机器的固定模板再次核对。
8. 保存后按节点顺序重启 `hubcenter`，让运行中的同步线程加载新配置。

## 5. 核心 YAML 示例

```yaml
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
```

已生成好的参考配置：

- [hubcenter-hc-1.yaml](/D:/workprj/aicoder/deploy/out-mypapers-maclaw/hubcenter-hc-1.yaml)
- [hubcenter-hc-2.yaml](/D:/workprj/aicoder/deploy/out-mypapers-maclaw/hubcenter-hc-2.yaml)
- [hubcenter-hc-3.yaml](/D:/workprj/aicoder/deploy/out-mypapers-maclaw/hubcenter-hc-3.yaml)
- [hub-mypapers.yaml](/D:/workprj/aicoder/deploy/out-mypapers-maclaw/hub-mypapers.yaml)
- [hub-maclaw.yaml](/D:/workprj/aicoder/deploy/out-mypapers-maclaw/hub-maclaw.yaml)

## 6. 字段说明

### `ha.enabled`

- `true` 表示开启 HA
- `false` 表示单节点运行

### `ha.node_id`

- 当前节点唯一标识
- 3 台机器之间必须不重复
- 推荐固定使用 `hc-1`、`hc-2`、`hc-3`

### `ha.node_name`

- 当前节点展示名
- 用于后台和状态页显示

### `ha.advertise_url`

- 当前节点告诉其它节点“如何访问我”的地址
- 在你们当前公网环境下，直接填公网域名即可

### `ha.cluster_secret`

- `hubcenter` 节点间同步使用的 Bearer secret
- 3 台机器必须完全相同
- 不要泄露给 `hub` 客户端或外部用户

### `ha.sync_interval_seconds`

- 拉取远端 HA op 的周期
- 推荐首版保持 `180`，允许几分钟最终一致，降低 peer 轮询压力

### `ha.push_debounce_seconds`

- 本地写入后推送到 peer 的合并等待窗口
- 推荐首版保持 `180`，把短时间内多次写入合并为一次推送

### `ha.pull_batch_size`

- 单次拉取的 op 数量上限
- 推荐首版保持 `200`

### `ha.heartbeat_sync_min_interval_seconds`

- 对高频心跳同步做节流
- 推荐首版保持 `600`

### `ha.history_retention_days`

- 自动清理 HA 历史日志的保留天数
- 推荐首版保持 `0.5`

### `ha.history_max_retained_ops`

- 自动清理后最多保留的 HA op 数量
- 推荐首版保持 `50000`

### `ha.history_prune_interval_minutes`

- 后台历史清理运行间隔
- 推荐首版保持 `10`

### `ha.history_prune_batch_size`

- 每次删除的历史操作上限，避免 SQLite 长时间写锁
- 推荐首版保持 `20000`

### `ha.peers`

- 当前节点认识的其它 `hubcenter` 节点列表
- 不要把自己加入 `peers`
- 每台都应配置另外两台

## 7. 启动时配置校验

程序会自动校验以下问题，校验失败会直接启动失败：

- `ha.enabled=true` 时 `node_id` 不能为空
- `ha.enabled=true` 时 `advertise_url` 不能为空
- `ha.enabled=true` 时 `cluster_secret` 不能为空
- 至少要有一个启用的 peer
- `peer.node_id` 不能为空
- `peer.node_id` 不能等于自己的 `node_id`
- `peer.node_id` 不能重复
- `peer.base_url` 不能重复

## 8. hub 侧配合方式

`hubcenter` 的 HA 解决的是中心节点同步，真正的接入侧切换由 `hub` 完成。

`hub` 侧推荐配置：

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

- 探测 `/api/client/quality`
- 优先选择质量更高的 `hubcenter`
- 记住上次成功节点
- 当前节点不可用或返回 `HUB_NOT_READY_ON_NODE` 时自动切换下一节点

## 9. 服务目录规划

当前 `deploy_all.cmd` 会调用 `deploy/deploy_all_ha.ps1`，并按下面的目录规划部署。

脚本在正式上传和构建前会先做远端预检，默认检查：

- `sh`、`tar`、`go` 是否存在
- 远端临时目录、`hubcenter` 目录是否可创建和可写
- 对需要部署 `hub` 的节点，额外检查 `hub` 目录可写，以及系统中至少有 `curl` 或 `wget` 之一用于模型下载

如果 3 台机器的目录不完全一致，脚本也支持按节点覆盖：

- 全局变量：`REMOTE_TMP_DIR`、`REMOTE_HUB_DIR`、`REMOTE_HUBCENTER_DIR`
- 按节点覆盖：`REMOTE_TMP_DIR_HC1`、`REMOTE_TMP_DIR_HC2`、`REMOTE_TMP_DIR_HC3`
- 按节点覆盖：`REMOTE_HUB_DIR_HC1`、`REMOTE_HUB_DIR_HC2`、`REMOTE_HUB_DIR_HC3`
- 按节点覆盖：`REMOTE_HUBCENTER_DIR_HC1`、`REMOTE_HUBCENTER_DIR_HC2`、`REMOTE_HUBCENTER_DIR_HC3`

如果不设置节点级变量，就会回退到全局默认值。

### hubcenter

- 部署目录：`/data/soft/hubcenter`
- 二进制：`/data/soft/hubcenter/maclaw-hubcenter`
- 配置：`/data/soft/hubcenter/configs/config.yaml`
- 配置备份：`/data/soft/hubcenter/configs/config.yaml.bak`
- 日志目录：`/data/soft/hubcenter/data/logs`
- 启动脚本：`/data/soft/hubcenter/start.sh`

数据库文件：

- `hc-1`：`/data/soft/hubcenter/data/hubcenter-hc-1.db`
- `hc-2`：`/data/soft/hubcenter/data/hubcenter-hc-2.db`
- `hc-3`：`/data/soft/hubcenter/data/hubcenter-hc-3.db`

### hub

- 部署目录：`/data/soft/hub`
- 二进制：`/data/soft/hub/maclaw-hub`
- 配置：`/data/soft/hub/configs/config.yaml`
- 配置备份：`/data/soft/hub/configs/config.yaml.bak`
- 日志目录：`/data/soft/hub/data/logs`
- 启动脚本：`/data/soft/hub/start.sh`

数据库文件：

- `hub.mypapers.top`：`/data/soft/hub/data/maclaw-hub-mypapers.db`
- `hub.maclaw.top`：`/data/soft/hub/data/maclaw-hub-maclaw.db`

## 10. hub 模型目录与下载策略

### 模型目录

当前约定将模型放在：

- `/data/soft/hub/data/models`

后台下载配套文件：

- 初始化完成标记：`/data/soft/hub/data/models/.models-initialized`
- 下载中锁文件：`/data/soft/hub/data/models/.models-downloading`
- 下载脚本：`/data/soft/hub/data/download-models.sh`
- 下载日志：`/data/soft/hub/data/logs/model-download.log`

兼容复制目录：

- `~/.maclaw/models`

这样既能让模型文件统一归档到 `hub/data/models`，也兼容当前代码里按 `~/.maclaw/models/...` 查找默认模型的逻辑。

`deploy_all.cmd` / `deploy_all_ha.ps1` 的处理规则是：

- 如果远端已经存在 `.models-initialized`，则保留现有模型，不重复下载
- 如果发现已有 `.models-downloading`，则视为后台下载正在进行，不重复触发
- 如果模型缺失，则自动在后台启动下载脚本，并把日志写入 `model-download.log`

### 下载来源

模型从官方 release 下载：

- [Model_Release](https://github.com/RapidAI/MaClaw/releases/tag/Model_Release)

当前默认下载模型：

- `embeddinggemma-300M-Q8_0.gguf`
- `sensevoice-small-q8.gguf`
- `omniparser-v2.yolow`

### 下载时机

`deploy_all.cmd` 部署 `hub` 时：

- 如果远端已存在 `.models-initialized`，则保留现有模型，不重复下载
- 如果远端不存在初始化标记，则后台启动下载任务
- 下载完成后自动写入初始化标记
- 下载过程中日志写入 `model-download.log`

### 额外说明

如果后续你们在 `hub` 中维护了对外模型下载地址映射，只需要调整映射逻辑，外部 URL 不必改变；模型继续放在 `hub/data/models` 即可。

## 11. deploy_all.cmd 自动部署约定

当前 [deploy_all.cmd](/D:/workprj/aicoder/deploy_all.cmd) 会调用 [deploy_all_ha.ps1](/D:/workprj/aicoder/deploy/deploy_all_ha.ps1)，按以下拓扑自动部署：

- `hubs.mypapers.top`：部署 `hubcenter` + `hub.mypapers.top`
- `hubs.maclaw.top`：部署 `hubcenter` + `hub.maclaw.top`
- `hubs2.maclaw.top`：部署 `hubcenter`

自动化行为包括：

- 自动生成或使用统一的 `cluster_secret`
- 自动渲染 3 份 `hubcenter` 配置和 2 份 `hub` 配置
- 分别上传到对应机器
- 自动覆盖远端 `configs/config.yaml`
- 首次缺模型时自动后台下载模型
- 自动保留旧配置备份 `config.yaml.bak`

如需覆盖默认值，可在运行前设置环境变量：

- `REMOTE_USER`
- `REMOTE_PORT`
- `REMOTE_TMP_DIR`
- `REMOTE_HUB_DIR`
- `REMOTE_HUBCENTER_DIR`
- `CGO_ENABLED`
- `GOPROXY`
- `CLUSTER_SECRET`
- `HUB_MODEL_BASE_URL`
- `HUB_MODEL_FILES`
- `DEPLOY_HOSTKEY_HC1`
- `DEPLOY_HOSTKEY_HC2`
- `DEPLOY_HOSTKEY_HC3`

## 12. 推荐启动顺序

1. 准备好 3 台 `hubcenter` 的配置。
2. 先启动任意 1 台 `hubcenter`。
3. 再启动另外 2 台 `hubcenter`。
4. 检查后台“多机热备”页是否能看到 3 个节点。
5. 确认 2 台 `hub` 已启动，且模型下载状态正常。
6. 检查 `hub` 是否成功注册并正常发送心跳。

## 13. 验证命令

推荐直接运行：

```powershell
.\deploy\check-hubcenter-ha.ps1 `
  -CenterUrls https://hubs.mypapers.top,https://hubs.maclaw.top,https://hubs2.maclaw.top `
  -ClusterSecret '你的真实 cluster_secret'
```

也可以手工抽查：

```powershell
Invoke-RestMethod https://hubs.mypapers.top/api/client/quality
Invoke-RestMethod https://hubs.mypapers.top/api/client/endpoints
Invoke-WebRequest https://hubs.mypapers.top/api/internal/ha/ops?after_seq=0&limit=1
Invoke-WebRequest https://hubs.mypapers.top/api/internal/ha/ops?after_seq=0&limit=1 -Headers @{ Authorization = "Bearer 你的cluster_secret" }
```

模型下载状态可到远端查看：

```bash
tail -f /data/soft/hub/data/logs/model-download.log
ls -la /data/soft/hub/data/models
```

## 14. 常见问题

### 问题 1：3 台机器的 `cluster_secret` 不一致

现象：

- peer 长期隔离
- `/api/internal/ha/ops` 授权失败
- backlog 不回落

处理：

- 确认 3 台机器的 `ha.cluster_secret` 完全一致

### 问题 2：把自己写进 `peers`

现象：

- 启动时报配置校验错误

处理：

- 每台机器的 `peers` 只写另外两台

### 问题 3：peer 地址写错

现象：

- 质量页显示节点不可达
- peer RTT 或 `last_success_at` 不刷新

处理：

- 检查 `advertise_url` 和 `peers[].base_url` 是否真实可访问

### 问题 4：后台保存后没有重启

现象：

- 页面看到的是新配置，但运行状态像旧参数

处理：

- 保存后逐台重启 `hubcenter`

### 问题 5：hub 只配置了单个 center

现象：

- 单个 `hubcenter` 故障后 `hub` 不切换

处理：

- 在 `hub` 中配置完整的 `center.base_urls`

### 问题 6：hub 首次部署后模型还没准备好

现象：

- `hub` 已启动，但依赖模型的能力暂时不可用
- `model-download.log` 仍在写入

处理：

- 等待后台下载完成
- 确认 `/data/soft/hub/data/models/.models-initialized` 已出现
- 必要时查看下载日志排查网络或 GitHub 访问问题

## 15. 运维建议

- 每台 `hubcenter` 使用独立 SQLite 文件，不共享 `.db`
- 首版保持低压力同步参数，接受几分钟最终一致，不要一开始就频繁调大或调小
- 后台改完参数后，记得同步回运维配置文件
- 定期检查 `quality_score`、`lag_seconds`、`backlog`、`last_error`
- 对 `hub` 额外关注模型下载日志与磁盘空间
- 如果某台节点长期离线，恢复后会按 peer cursor 继续追同步日志
