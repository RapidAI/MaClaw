# Bot 节点：公网 / 内网混合部署与无电脑 bot

状态：**设计稿，待实施**。依据是 2026-11-15 对仓库代码的阅读（`hub/internal/desktoppool`、`hub/internal/botmgmt`、`hub/internal/httpapi`、`desktopd/`、`MaClawSrv/desktop_*.go`、`corelib/localrelay`），没有在真实内网环境验证。第 10 节 P0 就是那次验证。

---

## 1. 目标与约束

目标：

1. 管理员在 Hub 的 bot 管理里「添加 Bot 节点」，选公网或内网。公网沿用现有的填 URL 和 Token；内网用注册码，由内网 desktopd 主动连上 Hub。
2. 注册之后，公网节点和内网节点在分配、启停、看桌面、接管上没有区别，用户看不出差别。
3. 每个租户注册、管理自己的节点，租户之间互相看不到、调不到。
4. 公网 desktopd 和内网 desktopd 可以在同一个租户里混用。
5. 支持无电脑的 bot（`desktop_mode = none`），提高单台机器能撑的用户数。
6. 授权时可以限定「可用电脑的 bot 数」，超出后只能创建无电脑 bot。

硬约束：**现有部署零功能影响。** 没有配置节点、没有设置额度的租户，走的代码路径要和现在逐行相同（第 9 节）。

非目标：

- 节点之间迁移用户数据（第 11 节，后续）。
- Firecracker、多 Hub。
- 只有 srv、不带 desktopd 的「轻量沙箱」，也就是能跑命令但没有桌面的那一档。

---

## 2. 现状：谁连谁

| 发起方 → 目标 | 现在怎么连 | 代码 |
| --- | --- | --- |
| Hub → MaClawSrv | 每个租户一个 `base_url` + 凭证 | `botmgmt.record.BaseURL`，`Service.do` |
| Hub → desktopd API | 每台 Docker 服务一个 `BaseURL` + Bearer | `desktoppool.Server`，`Pool.callClient` |
| Hub → 画面（noVNC / H.264） | 接管代理去连 `novnc_url` 的主机 | `botmgmt/handoff.go` 里的 `ProxyDesktopHandoff` |
| MaClawSrv → Hub | `MACLAW_HUB_URL` + `MACLAW_DESKTOP_API_TOKEN`，主动连出 | `MaClawSrv/desktop_remote.go` |
| MaClawSrv → CDP | 直连 desktopd 返回的 `cdp_url` | `MaClawSrv/desktop_runtime.go` |
| 容器里的 relay helper → Hub | 主动连出 | `MaClawSrv/desktop_local_relay.go` |
| Hub 对 srv 的认证 | **全局一把 token**，租户取自请求体的 `tenant_id` | `httpapi/desktop_admin_handler.go` 里的 `authorizeDesktopAPI` |

desktopd 放到内网后，前三条「Hub 主动连出去」的会断。后面几条本来就是往外连，不受影响。

---

## 3. 概念

### 3.1 Bot 节点

一台机器（或同一个内网）上的一组服务：

| 能力 | 承载 |
| --- | --- |
| srv + desktopd（默认部署单元） | `shared`、`isolated`、`none` 三种 bot |
| 只有 srv | 只有 `none` bot |
| 只有 desktopd | 不承载 bot，给其它 srv 扩桌面容量 |

接入方式：

- **公网（direct）**：Hub 能直接访问。底层就是现有的 Docker 服务记录和 srv 连接，不迁移数据。
- **内网（tunnel）**：节点通过出站 WebSocket 接到 Hub，Hub 访问节点的请求都走这条隧道。

### 3.2 路线（系统自动计算，管理员不配置）

| srv 位置 | desktopd 位置 | CDP 路线 | Hub 访问 desktopd |
| --- | --- | --- | --- |
| 公网 | 公网 | 直连（现状） | 直连 |
| 节点 X | 同一节点 X | 本地直连 | 隧道 |
| 节点 X | 公网 | 直连（srv 主动连出） | 直连 |
| 公网，或节点 Y | 节点 X | **经 Hub 中转** | 隧道 |

画面路线：公网 desktopd 和现在一样走 Hub 接管代理；内网 desktopd 在 GUI 能访问到时内网直连，否则走 Hub 中转。

---

## 4. 节点隧道

### 4.1 链路

```
内网 desktopd ──wss──> Hub /api/v1/edge/link   (节点密钥认证)
              yamux 多路复用（复用 corelib/localrelay 的 WSConn、YamuxConfig）
Hub 开子连接 ──> 头部写明目标 "desktopd" 或 "srv" ──> desktopd 转发到配置好的本地地址
```

- 隧道代码放在 desktopd 进程里。一台节点只建一条隧道，同时承载同机 srv 的流量。
- 转发目标只有面板里配置的两个地址，例如 `desktopd` 对应本进程，`srv` 对应 `127.0.0.1:18080`。其它目标一律拒绝，Hub 不能借这条隧道访问内网里的其它机器。
- Hub 侧只转发 `/v1/*` API、画面通道和 srv API，**不转发 desktopd 的 `/admin`**。
- 心跳 20 秒一次（沿用 `YamuxConfig`），断开后指数退避重连，最长 30 秒一次。

### 4.2 Hub 侧

- 新包 `hub/internal/edgenode`，包含 `Broker` 和 `Store`：
  - `Broker` 在内存里按 `(tenantID, nodeID)` 管理 yamux 会话；同一节点重复连接时，新的替换旧的。
  - `Dial(ctx, tenantID, nodeID, target) (net.Conn, error)`：调用方的租户和节点所属租户不一致就拒绝，作为第二道防线。
  - `Transport(tenantID, nodeID, target) http.RoundTripper`：一个 `http.Transport`，`DialContext` 走 `Dial`，并支持 WebSocket 升级。
- **不替换任何现有 HTTP 客户端**。只有记录里带 `NodeID` 时，才用 `edgenode.Transport` 新建客户端；`NodeID` 为空时继续用原来的 `p.HTTP`、`s.HTTP` 和 `ReverseProxy{Transport: nil}`。

### 4.3 需要按节点切换客户端的调用点（P1 逐个处理）

| 位置 | 现在 | 改成 |
| --- | --- | --- |
| `desktoppool.Pool.callClient` / `client()` | `p.HTTP` 或 2 分钟超时的默认客户端 | `server.NodeID != ""` 时用节点客户端 |
| `desktoppool` 里的安装、上传、截图这类长超时客户端 | 各自新建 | 同上 |
| `botmgmt.Service.do`、`doAdmin`、token 交换 | `s.HTTP` + `rec.BaseURL` | srv 连接带 `NodeID` 时用节点客户端 |
| `botmgmt.ProxyDesktopHandoff` | `ReverseProxy`，`Transport` 为 nil | 这次查看属于节点时，设置节点 Transport |
| `httpapi/platform_handlers.go` 里调 srv runtime 的 SSE | 独立客户端 | 同上（如果 runtime 也在节点上） |

P1 开工时先 `rg "http.Client{|ReverseProxy{" hub/internal`，把清单补全。

### 4.4 内网 desktopd 的地址

- `DESKTOPD_PUBLIC_URL` 设为 srv 能访问的内网地址，这样同机 srv 的 `cdp_url` 走本地。
- `OpenDesktop` 和 `PeekDesktopView` 拿到 `novnc_url` 后，如果这台服务属于某个节点，Hub 把它的 origin 换成「节点虚拟 origin」，例如 `http://desktopd.<nodeID>.node.invalid`，再交给 `gateDesktopHandoff`。接管代理按 `handoffView` 上记录的 `NodeID` 选 Transport。这个虚拟主机名不会被 DNS 解析，只在 Transport 内部用来查节点。

---

## 5. 注册与凭证

### 5.1 流程

1. Hub 租户管理员在 bot 管理里点「添加 Bot 节点 → 内网」，填名称。Hub 生成注册码：一次性使用，10 分钟有效，绑定 `tenantID` 和 `nodeID`；`nodeID` 是 128 位随机值。
2. 在内网 desktopd 的 `/admin` 面板「Hub 连接」卡片里填 Hub 地址和注册码。可选：勾选同机 srv，填它的本地地址和 admin secret 文件路径。
3. desktopd 调 `POST /api/v1/edge/enroll`，提交注册码和能力信息，换回：
   - `node_secret`：长期的节点密钥。Hub 只存它的 SHA-256。
   - `hub_token`：按租户限定的 Hub token，给同机 srv 用，见 5.3。
4. desktopd 在面板的「额外 key」里新建一把 `hub-<nodeID>` 并上报。Hub 用这把 key 调 desktopd API，主 `DESKTOPD_TOKEN` 不交出去。
5. 隧道建立后，Hub 通过隧道探测 desktopd 的 `/v1/health` 和 srv 的 `/version`，页面显示「在线 / 能力 / 版本」。
6. Hub 自动生成底层记录：
   - Docker 服务一条：`NodeID = <id>`，`BaseURL` 留空，Token 用第 4 步上报的 key。
   - 如果有 srv：生成 srv 连接，`NodeID = <id>`，用 admin secret 签发连接凭证（沿用 `provisionConnectionCredential`）。

无界面部署：在 `.env` 里写 `DESKTOPD_HUB_URL` 和 `DESKTOPD_HUB_ENROLL_CODE`，首次启动时自动注册；注册码用过就作废。

### 5.2 desktopd 本地状态

- `<stateDir>/hub_link.json`（权限 600）：Hub 地址、`nodeID`、`tenantID`、`node_secret`、srv 本地地址、内网访问地址。
- 注册后 desktopd 只接受 `tenant_id == 绑定租户` 的 `/v1/*` 请求，作为第三道防线。没注册的 desktopd 行为不变。

### 5.3 srv 回调 Hub 的认证（安全关键）

现在 `authorizeDesktopAPI` 只比对一把全局 token。租户自建的 srv **不能**拿到这把 token。

- 新增 `authorizeDesktopAPIPrincipal(r) (tenantLock string, ok bool)`：
  1. 先按原样比对全局 token。匹配就返回 `tenantLock = ""`，表示不锁定租户，行为和现在相同。
  2. 不匹配再查节点 `hub_token` 的哈希。匹配就返回该节点所属的租户。
- 所有 srv 会调到的 Hub 接口都改用这个函数：`/api/v1/desktop-services/*`（session、tool、memory、stop、hold、app、open、install、screenshot、file、relay 等）、出站消息、模型代理、计费。`tenantLock` 不为空时，请求体里的 `tenant_id` 必须相等，否则返回 403。
- 节点的 srv 把 `MACLAW_DESKTOP_API_TOKEN` 配成 `hub_token`，srv 的代码不用改。

### 5.4 吊销与轮换

- Hub 页面「删除节点」：作废 `node_secret` 和 `hub_token`，断开隧道，删除自动生成的底层记录。删除前，如果还有分配指向这个节点，先提示。
- desktopd 面板「解除注册」：删除 `hub_link.json`，删掉 `hub-<nodeID>` 这把 key，解除租户锁定。
- `node_secret` 支持重新注册来轮换：生成新码、注册，旧密钥作废。

---

## 6. 租户隔离与平台管控

| 层 | 措施 |
| --- | --- |
| 归属 | 注册码只能由该租户管理员生成（`requireTenantAdmin`）；节点归属在注册时确定，之后不能修改 |
| 存储 | `tenant:<id>:edge_nodes`，和 `desktop_service`、bot 记录分开存。另有一张全局索引 `nodeID → tenantID`，只给隧道认证用 |
| Hub → 节点 | 记录里存 `NodeID`，不存可手填的 URL；表单拒绝 `.node.invalid` 这类主机名；`Broker.Dial` 校验租户 |
| 节点 → Hub | 节点密钥只能建隧道；`hub_token` 锁定租户（5.3） |
| desktopd | 注册后锁定租户（5.2） |
| 平台（超级管理员） | 每个租户单独开关「允许自建节点」；节点数上限；每个租户的隧道并发子连接数和画面路数上限；全局节点列表只看元数据 |

---

## 7. 无电脑 bot 与电脑额度

### 7.1 `desktop_mode = none`

- `corelib/desktop/botdesktop.go` 新增 `BotDesktopModeNone = "none"`。
- **Hub 侧**，`none` 模式的 bot：
  - 跳过 `Ensure`、`OpenDesktop`、`desktopMemoryPressured`、桌面忙排队和键盘归属；
  - `WakeDesktopBot` 和 `WatchDesktopBot` 返回明确的「这个 bot 没有电脑」，不创建容器；
  - 下发消息时在 metadata 里带 `bot_desktop = none`。
- **srv 侧**：每一轮读 `bot_desktop`。值为 `none` 时，不挂 `desktop`、`computer_*`、浏览器、经容器的 `local_computer` 这些工具；保留对话、记忆、知识库、`send_to_user`、日程、搜索、服务端抓取网页和连接器。`desktopRuntimeAvailable` 从按进程判断改成按每一轮判断。
- **能力边界**：`none` bot 没有 bash、没有文件空间、没有真实浏览器，因为这些都在 desktopd 里执行。

### 7.2 授权额度

```go
type Grant struct {
	ID              string `json:"id"`
	Scope           string `json:"scope"`
	TargetID        string `json:"target_id,omitempty"`
	DesktopBotLimit *int   `json:"desktop_bot_limit,omitempty"` // nil 不限；0 只能建无电脑 bot
}
```

- 解析：用户匹配到多条授权时，按「用户 > 部门（从近到远）> 全局」取最具体的一条，和 `desktoppool.resolve` 一致。
- 计数：这个用户名下 `desktop_mode` 是 `shared` 或 `isolated` 的 bot。
- 检查点，都在同一把 `s.mu` 锁里检查并写入：
  - `CreateBotForUser`：要电脑时检查额度。
  - `SetBotDesktopModeForUser`：从 `none` 改成要电脑时检查额度。
  - 管理员接口：允许超额设置，但返回警告字段。
- 调低额度时不降级已有的 bot，只限制以后的新建和升级。

### 7.3 接口

| 接口 | 变化 |
| --- | --- |
| `POST /api/v1/bots` | 请求体加 `desktop: bool`。不传时按 `true` 处理，额度用完时返回 `DESKTOP_QUOTA_EXCEEDED` 和中文说明 |
| `GET /api/v1/bots` | 响应加 `desktop_quota: {limit: int\|null, used: int}`；每个 bot 带 `desktop_mode` |
| `PUT /api/v1/bots/{id}/desktop_mode` | 增加 `none` 取值，并检查额度 |
| `POST /api/admin/bots/grants` | 接受 `desktop_bot_limit` |
| `PATCH /api/admin/bots/grants/{id}` | 新增，用来修改额度 |

### 7.4 界面

- **Hub 管理台**（`hub/web/admin/bot-tab.js`）：
  - 授权行加一列「可用电脑的 bot 数」，留空表示不限；
  - bot 列表加一列「电脑：共用 / 独立 / 无」，超额的标「超额」；
  - 「Bot 节点」区块：添加（公网 / 内网）、状态、能力、版本、删除。
- **GUI**（`guiapp/frontend/src/components/bots/`）和**手机**（`mobilebot/lib/features/bots/`）：
  - 创建 bot 的弹窗里有开关「这个 bot 需要电脑」和「已用 x / y」，额度用完时开关置灰；
  - bot 列表的标记：「电脑·共用」「电脑·独立」「无电脑」；无电脑的 bot 不显示「电脑」标签和接管入口；
  - `desktopModeOf` 要认识 `none`。
- **desktopd 面板**（`desktopd/admin.go`、`admin.html`）：「Hub 连接」卡片，包括 Hub 地址、注册码、同机 srv、内网访问地址、状态，以及注册、断开、解除注册。

---

## 8. CDP 中转与画面直连

### 8.1 CDP 经 Hub 中转（只用于第 3.2 节表里的最后一种组合）

- Hub 处理 `POST /api/v1/desktop-services/session` 时，如果这台 desktopd 是节点 X、而调用的 srv 不在节点 X 上，就不原样返回 `cdp_url`，改为返回 `https://<hub>/api/v1/desktop-cdp/<票据>`。票据有效 2 小时，限定在这个用户和这块屏幕。
- 新路由 `/api/v1/desktop-cdp/{ticket}/{rest...}`：验证票据，经隧道转到 desktopd 的 `/v1/desktops/tunnel/<userkey>/cdp/<rest>`，带上网关 token。
- **P0 要验证**：`/json` 返回的 `webSocketDebuggerUrl` 里带主机名，需要时 Hub 把它改写成中转地址。
- 管理页分配用户时，如果会走这条路线，提示「这个组合延迟较高」。

### 8.2 内网画面直连（GUI 和 desktopd 在同一个内网时）

- desktopd 新增 `GET /v1/desktops/viewer/{ticket}/{rest...}`，含 WebSocket。路径不能用 `/v1/desktops/view`，这个前缀已经被 `POST /v1/desktops/view` 占用。
- Hub 处理查看请求时，除了原来的中转地址，再返回 `direct_url` 和票据。票据用注册时约定的密钥签名，限定用户、屏幕、是否允许操作和有效期。
- desktopd 验证签名后自己带上网关 token 转给容器。每次建立 WebSocket 时还要经隧道向 Hub 确认票据有效，Hub 撤回查看权时立即生效。
- GUI：有 `direct_url` 时先探测一次（1 秒超时），通就直连，否则用原来的地址；播放器重连失败也回退到原地址。
- 内网 desktopd 最好配内部 CA 证书。WebView2 的 iframe 能不能加载 `http://` 内网地址，P0 一起验证。

---

## 9. 兼容性：零影响清单

| 改动 | 保证 |
| --- | --- |
| HTTP 客户端 | 不替换任何现有客户端，也不改 `http.DefaultTransport`；`NodeID` 为空时用原对象 |
| 接管代理 | 只有节点的查看才设置 Transport |
| `novnc_url` 和 `cdp_url` | 只有节点服务才改写；公网组合原样返回 |
| `authorizeDesktopAPI` | 全局 token 的判断代码不动，节点 token 只是额外分支 |
| desktopd | 没注册时：路由、租户校验、面板其它功能都不变；新入口用独立路径 `/v1/desktops/viewer/` |
| 存储 | 节点存单独的键；`Grant` 和 Server 只加带 `omitempty` 的可选字段；回滚到旧二进制时现有配置照常可读 |
| bot 模式 | 默认仍是 `shared`；`none` 只在显式选择时生效 |
| 额度 | 默认 `nil` 表示不限；旧客户端不传 `desktop` 时按 `true`，和现在相同 |
| 总开关 | `HUB_EDGE_NODES` 默认关闭；关闭时不注册 `/api/v1/edge/*` 和 `/api/v1/desktop-cdp/*`，管理台也不显示「内网」选项 |

需要知道的两点：

- 升级 Hub 和 desktopd 要重启，按部署规则，重启时正在进行的对话会丢失。
- 隔离设计和总开关只能缩小代码改动的风险，不能消除它。兜底手段是保留旧二进制备份，出问题立即换回。

---

## 10. 分阶段

| 阶段 | 内容 | 验收 |
| --- | --- | --- |
| **P0 验证**（不改代码，1–2 天） | 用 `ssh -R` 或 frp 把一台内网机器的 srv 和 desktopd 映射到 Hub 的回环地址，配成现有的 Docker 服务和 srv 连接 | 桌面 bot 全流程跑通：开桌面、浏览器、截图、看画面并接管、定时任务。记下所有隐含的直连假设，再验证 `webSocketDebuggerUrl` 和 WebView2 混合内容 |
| **M1 无电脑 bot + 额度** | 第 7 节全部。和节点没有依赖，可以先上线 | 第 12 节的额度和模式用例 |
| **P1 内网节点** | 第 4、5、6 节；8.1 节 CDP 中转；Hub「Bot 节点」区块；desktopd「Hub 连接」卡片；每个租户仍然只有一个 srv | P0 的流程不用 frp 也能通过；节点断开时错误信息清楚；隧道拒绝访问第三个目标；跨租户访问一律返回 403 |
| **P2 节点作为分配单位** | 一个租户可以有多个 srv 连接（`record` 改成按节点区分连接，`Principals` 按连接存）；用户和 bot 固定在某个节点上；8.2 节画面直连 | 同一租户里公网 bot 和内网 bot 同时工作，不走 CDP 中转 |
| **P3 优化** | 内网 srv 的截图和文件这类大数据量调用直连本地 desktopd，Hub 只保留授权、唤醒和键盘归属的记录；节点短暂断开时消息排队 | 每次工具调用少两次公网往返 |

发布方式：每个阶段都先关着开关上线，观察一段时间，再只给一个测试租户打开，最后逐步放开。

---

## 11. 未决问题

1. 多条授权同时匹配时，额度取最具体的那条（本稿的做法），还是取最大值？
2. 内网 srv 调模型：只走 Hub 的模型代理，还是允许节点直接配置模型服务？这决定节点出网是只需要 Hub 的 443，还是要放更多出口。
3. P2 之后，用户从一个节点改分配到另一个节点时，对话和记忆要不要迁移，还是从头开始。
4. 节点离线时，发给这个节点上 bot 的消息是排队等节点回来，还是直接失败并提示。P1 先做直接失败。

---

## 12. 测试清单

**Hub**（`hub/internal/edgenode`、`desktoppool`、`botmgmt`、`httpapi`）：

- `NodeID` 为空时，`callClient` 用的就是 `p.HTTP`，并且是同一个对象。
- 全局 token 的行为不变；节点 token 的请求体换成其它租户时返回 403。
- `Broker.Dial` 拒绝跨租户访问；隧道拒绝未配置的目标。
- 表单里手填 `.node.invalid` 主机名会被拒绝。
- 节点服务的 `novnc_url` 会被改写，公网服务原样返回。
- 额度：为空时不限；第 N+1 个要电脑的 bot 被拒，无电脑的仍能创建；`none` 改成要电脑时检查额度，改回后额度空出；并发创建不会超额；调低额度后已有 bot 不变；用户、部门、全局授权同时存在时，用户那条生效。
- `none` bot 的唤醒请求返回明确错误，`desktoppool` 没有收到任何调用。

**desktopd**：

- 没注册时，一个已有的跨租户请求照常通过。
- 注册后，其它租户的请求被拒绝。
- `/v1/desktops/viewer/` 能区分有效和无效的票据，且 `POST /v1/desktops/view` 的行为不变。
- 隧道不转发 `/admin`。

**srv**：

- `bot_desktop = none` 的一轮里，工具列表中没有 desktop、computer、浏览器这几类工具。
- 普通的一轮工具列表不变。

**端到端**：

- P0 和 P1 的验收流程。
- 现有生产租户在开关关闭时做一轮完整回归。

重的 Go 测试按 `AGENTS.md` 的要求，一律经过 `scripts\goheavy.cmd`。
