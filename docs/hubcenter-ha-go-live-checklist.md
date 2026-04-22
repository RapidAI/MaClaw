# Hub Center 三节点首发上线检查清单

适用范围：第一版 3 台 Hub Center 加 1 台或多台 Hub，采用本地 SQLite、节点间 HA op 同步、Hub 侧 `center.base_urls` 故障转移。

## 上线前

1. 确认 3 台 Hub Center 都使用独立本地 SQLite 文件，不能共享同一个 `.db` 文件。
2. 确认 3 台 Hub Center 的 `ha.cluster_secret` 完全一致，并替换掉示例值。
3. 确认 3 台 Hub Center 的 `ha.node_id` 两两不同，`ha.peers` 中不包含自己。
4. 确认 `ha.advertise_url` 和各 `peers[].base_url` 都是其他节点实际可访问的地址，优先使用内网地址。
5. 确认 Hub 的 `center.base_urls` 已写入 3 个 Hub Center 地址，且 `center.base_url` 指向其中任意一个可用节点。
6. 确认防火墙或安全组放通 Hub 到 3 台 Hub Center 的访问，以及 3 台 Hub Center 之间的互访。

## 启动检查

1. 先启动 3 台 Hub Center，确认进程都正常监听且无 `validate config file` 错误。
2. 分别访问 3 台机器的 `/healthz`，确认都返回 `200`。
3. 分别访问 3 台机器的 `/api/client/quality`，确认都能返回 JSON。
4. 登录任意一台 Hub Center 管理后台，打开“多机热备”页，确认总节点数为 3，且 peer 不长期处于 `isolated`。
5. 也可以直接运行 `deploy/check-hubcenter-ha.ps1` 做一次基础烟雾检查。

## 同步检查

1. 在任意一台 Hub Center 后台新增一条新闻或封禁项。
2. 10 秒内到另外两台 Hub Center 后台确认该数据已经出现。
3. 在“多机热备”页确认 peer 的 `backlog` 会回落，`last_success_at` 持续刷新。
4. 如果 `/api/internal/ha/ops` 未带 Bearer secret 就能访问，立即停止上线并收紧配置或网关规则。

## Hub 接入检查

1. 启动 Hub，确认能够自动注册到任意一台 Hub Center。
2. 访问 Hub 管理状态，确认 `center.base_urls` 已生效，并且存在当前活动中心地址。
3. 在 Hub Center 后台确认 Hub 状态为在线，心跳时间持续刷新。

## 故障切换检查

1. 暂停当前正在服务 Hub 的那台 Hub Center。
2. 观察一个心跳周期内 Hub 是否切换到另外一台 Hub Center，且不需要人工重新注册。
3. 恢复被暂停节点，确认其重新加入集群并开始追同步日志。
4. 再暂停另一台 Hub Center，确认剩余 2 台仍可继续服务。

## 自动化烟雾检查

PowerShell 示例：

```powershell
.\deploy\check-hubcenter-ha.ps1 `
  -CenterUrls https://hc-1.example.com,https://hc-2.example.com,https://hc-3.example.com `
  -ClusterSecret 'replace-with-a-long-random-shared-secret'
```

如果要按清单一次性生成 3 台 Hub Center 和 1 台 Hub 的最终配置，可以先复制 [hubcenter-ha.inventory.example.psd1](/D:/workprj/aicoder/deploy/hubcenter-ha.inventory.example.psd1)，填入生产域名、内网地址和数据库路径，再运行：

```powershell
.\deploy\render-hubcenter-ha-configs.ps1 `
  -InventoryPath .\deploy\hubcenter-ha.inventory.example.psd1 `
  -OutputDir .\deploy\out-ha-configs
```

脚本会检查：

- `/healthz`
- `/api/client/quality`
- `/api/client/endpoints`
- `/api/internal/ha/ops` 未带 secret 时是否返回 `401`
- 传入 secret 后内部同步口是否可访问

## 上线后观察

1. 首日重点观察“多机热备”页中的 `quality_score`、`lag_seconds`、`backlog`、`last_error`。
2. 如果某台节点持续出现 `isolated` 或 backlog 长时间不回落，优先排查节点间网络、`cluster_secret`、`advertise_url`、peer 地址配置。
3. 如果 Hub 频繁切换节点，优先排查 `/api/client/quality` 的质量分是否被同步延迟或网络抖动持续拉低。

## 回滚原则

1. 单台 Hub Center 异常时，不要立刻整体回滚，优先下线故障节点，让另外两台继续提供服务。
2. 只有在 3 台都无法提供注册、心跳或数据同步时，才考虑整体回滚到单节点模式。
3. 回滚到单节点模式时，Hub 的 `center.base_urls` 也要同步收敛到当前保留节点，避免继续轮询已下线地址。
