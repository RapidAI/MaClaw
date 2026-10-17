# 每用户 Agent Computer 架构设计

状态：**设计稿，未实施**。本文只描述目标架构和迁移顺序，不包含代码改动。

范围：desktopd 管理的云桌面（生产部署目录 `/data/soft/maclaw_desktopd`，容器镜像 `maclaw-gui:2`），以及 Hub、MaClawSrv 上的 desktop bot 如何到达这些容器并做 computer use。本机 GUI 的 `computer_*` 工具（Wails、UIA、本机键鼠）是另一条路径，不在本文的实现范围内；文中只在需要划界时提到它。

依据：仓库内 `desktopd/`、`corelib/desktop/`、`hub/internal/desktoppool/`、`hub/internal/botmgmt/`、`MaClawSrv/desktop_*.go`，以及 `desktopd/README.md`、`docs/desktop-gui-v2.md`、`docs/desktop-per-bot.md`。本文撰写时这台环境上没有 `/data/soft/maclaw_desktopd` 的实机，部署事实以这些源码和运维文档为准。

---

## 1. 结论

每个用户一台隔离的机器（agent computer），机器里一个 supervisor。屏幕 1 是这个人自己的桌面和自己的 Chrome；每个正在干活的 agent 再加一块屏幕，带自己的 Chrome。文件、已安装的工具、浏览器登录在会话之间保留。几分钟没有人用，就把文件同步到存储，留下磁盘快照（内存快照另作评估），然后把机器停掉。下一次工具调用或明确要使用桌面的消息再把它唤醒。对话状态留在 Hub / MaClawSrv，不放进机器快照。

现在用 Docker 实现这台机器。以后在有 KVM 的宿主机上换成 Firecracker microVM。两边实现同一个 runtime 接口，Hub 和 bot 不直接碰 Docker 或 Firecracker。

一期休眠不要用 CRIU。现有的「刷登录态 + 停容器 + 私有卷保留」已经能保住登录和装在卷里的软件；容器可写层在 `docker stop` 之后也还在。CRIU 对 XFCE + Chromium 不可靠，腾讯云普通 CVM 上 Firecracker 所需的 `/dev/kvm` 也不能默认假设存在。内存级快照放到 Firecracker 阶段，并且只在确认有 KVM 的机器上做。

对用户说话只有一个出口。shell、文件、浏览器、桌面、记忆、技能、子助手、云端 agent、应用连接、网页搜索、生图、录屏都是干活的工具，它们的结果回到模型，不直接变成用户看见的话。用户看见的每一句都来自一次 `send_to_user`。模型自己的正文不投递。路由在 Hub，再分到桌面 GUI、手机、邮件（以后还有网页、Slack、语音）。见第 16 节。

---

## 2. 现状

### 2.1 部署形态

一台装了 Docker 的主机跑 desktopd。生产文档里的默认目录：

| 路径 | 内容 |
| --- | --- |
| `/data/soft/maclaw_desktopd` | 二进制 `bin/desktopd`、`.env`（0600）、`state/`（面板账号和额外 API key）、`logs/desktopd.log` |
| `/data/soft/hub` | Hub |
| `/data/soft/maclaw_srv` | MaClawSrv |

`hub.mypapers.top`、`dockerd.mypapers.top`、`maclawsrv.mypapers.top` 在文档中是同一台机器。desktopd 由 systemd 单元 `maclaw-desktopd.service` 拉起，`Restart=always`，工作目录就是部署目录。监听默认 `127.0.0.1:18081`（`DESKTOPD_ADDR`）。对外只经 nginx 反代 API（`desktopd/deploy/nginx-dockerd.conf.example`，`proxy_read_timeout 600s`）。CDP 和 noVNC 的宿主机端口不走这个域名。

至少有一类主机仍是 Ubuntu 18.04（systemd 237，写不了 `StandardOutput=append:`）。另有 Ubuntu 24.04 + Docker 29 的新主机记录。镜像构建在腾讯云上走 `mirror.ccs.tencentyun.com` 和 `mirrors.tencentyun.com`，说明生产宿主机是腾讯云 CVM，不是裸金属。

desktopd 进程自己不保存每用户桌面的运行状态机。容器是事实来源：`docker ps` / `docker inspect`，加上容器里的 `/desktops/registry.json`。

### 2.2 一台容器对应一个用户

身份是 `sha256(tenant + NUL + user)[:8]` 的 16 位十六进制，`corelib/desktop.UserKey`。容器名 `maclaw-desktop-<key>`。标签 `maclaw.tenant`、`maclaw.user`、`maclaw.image`、`maclaw.memory`、`maclaw.cpus`、`maclaw.shm`、`maclaw.desktop-proxy`。

默认资源（Hub 桌面服务表单与 `corelib/desktop` 一致）：镜像 `maclaw-gui:2`，内存 `3g`，CPU `1.5`，shm `1g`。屏幕默认 `1440x900x24`，可用 `MACLAW_DESKTOP_GEOMETRY` 覆盖。纯 CPU 渲染，不需要 GPU。

`docker run` 的要点（`desktopd/service.go` 的 `runContainer`）：

- `--restart unless-stopped`：Docker 守护进程重启会把容器拉起来；`docker stop` 之后保持停止。
- 只发布两个端口：容器内 `19020/tcp`（CDP gate）和 `6080/tcp`（noVNC gate），映射到宿主机随机高位端口。
- 入口不是桌面进程，而是 pid 1 的 shell：收到 `TERM`/`INT` 先执行 `desktop_supervisor.py flush <key>`，再退出。`docker stop --time 25`，宽限长于 flush 的 15 秒，避免 SIGKILL 打断写 cookie。
- 四个命名卷，只属于这个用户：

| 卷 | 挂载点 | 内容 |
| --- | --- | --- |
| `maclaw-desktops-<key>` | `/desktops` | profile、`registry.json`、每屏幕 `pids.json`、登录态、桌面日志 |
| `maclaw-home-<key>` | `/home/desktop` | 家目录、桌面文件、应用配置 |
| `maclaw-opt-<key>` | `/opt` | 装在 /opt 的软件 |
| `maclaw-local-<key>` | `/usr/local` | 装在 /usr/local 的软件 |

镜像契约禁止把用户软件装进镜像层的 `/opt` 和 `/usr/local`，因为这两个路径会被卷盖住。

`apt-get install` 进的是容器可写层，不在这四个卷里。这个层只在**重建容器**时被 `docker commit` 成 `maclaw-desktop-user-<key>:state`，并打上 `maclaw.image`。`docker stop` 不 commit：容器还在，可写层还在，下次 `docker start` 软件还在。请求的镜像 tag 变了（`maclaw-gui:1` → `maclaw-gui:2`）时，旧 `:state` 会被改名为 `:prev`，旧层里的 apt 包不会跟到新镜像，需要重装。比较的是镜像**名**不是镜像 ID，原地重建同一个 tag 不会赶走已有用户。

打开桌面时若容器已在、镜像/shm/卷/代理都没变：只 `docker update` 资源，没在跑就 `docker start`，然后 `docker exec` supervisor `ensure`。要重建时顺序是：确认新镜像存在 → flush 浏览器 → `docker stop` → commit 用户层 → 必要时把旧共享卷上的文件迁到私有卷 → `docker rm -f` → 用 `:state`（仅当它的 `maclaw.image` 与请求镜像一致）或基础镜像 `docker run`。新镜像拉不下来时旧容器保持原样。

### 2.3 容器里的 supervisor

`desktopd/image/desktop_supervisor.py` 由 `docker exec` 调用，不是常驻的控制面。pid 1 只负责接停止信号并 flush。

`ensure <key>` 在 `/desktops/lock` 上 `flock`。主人 key 固定显示 `:20`（`DISPLAY_MIN`）。第一次会拉起：

- Xvfb `:20`，几何 `1440x900x24`，`-s 0` 防止 X 自己黑屏
- 该显示专用的 `dbus-daemon`（`unix:path=/tmp/.maclaw-dbus-20`），XFCE、fcitx5、Chromium 共用，否则拼音不可用
- `startxfce4`（没有 XFCE 的旧镜像回退 fluxbox）
- Chromium，`--user-data-dir=/desktops/<key>/profile`，CDP 只听容器内 `18020`
- x11vnc + websockify 只听容器内 loopback
- gate：`19020` → CDP，`6080` → noVNC。连接的第一段 HTTP 必须带 `Authorization: Bearer <token>`，通过之后原样转发，CDP 和 WebSocket 都能过

token 是每个显示 16 字节随机 hex，写在 `registry.json`。desktopd 把它放进 `cdp_url` / `novnc_url` 的 userinfo（`http://desktop:<token>@<DESKTOPD_ADVERTISE_HOST>:<宿主端口>`）。MaClawSrv 的 CDP 客户端和 Hub 的 noVNC 代理把它改成 Bearer 头。

浏览器登录的保留不靠内存：停止前 `flush` 让 Chromium 把 cookie 写入 profile，并尽量把页面存储落盘。下次 `ensure` 会清掉陈旧的 profile 锁（否则 Chromium 会另开空 profile，登录丢失），再 `restore_page_login`。

### 2.4 创建与回收

创建没有单独的「分配机器」步骤。Hub 或 MaClawSrv 调用 `POST /v1/desktops` 或 `POST /v1/desktops/session` 时，desktopd 按用户加锁后幂等确保容器在跑，再 `ensure`。

回收有两条，粒度不同：

1. **主人容器（可选，默认关）**。`DESKTOPD_MAX_IDLE` 为空则从不回收。示例写成 `24h`。回收器每 10 分钟扫一遍带 `maclaw.tenant` 标签的运行中容器，`exec supervisor idle <key>`。空闲秒数来自 `registry.json` 的 `last_used`，而 `last_used` **只在 `ensure` 时更新**。超过阈值就 `supervisor stop`（先 flush），再确认容器已停；还在跑则 `docker stop --time 25`。有 isolated bot 显示正忙时故意留下容器，只停主人会话。回收器存在的原因是 Hub 可能已经叫不出这个容器（分配被改、Hub 重启、MaClawSrv 在 stop 之前崩溃）。
2. **bot 屏幕（总是开）**。每个 bot 显示自己的 watcher 每 5 秒看一次。`MACLAW_BOT_IDLE_SECONDS` 默认 600。没有接管、也没有活动超过这个时间，就停掉这块屏幕的进程，profile 留在 `/desktops/<botkey>/`。容器继续跑。同时最多 `MACLAW_DESKTOP_MAX_BOTS`（默认 3）块 bot 屏幕；再开一块时，踢掉最久未用且不忙的。忙的定义：正在接管，或 120 秒（`BOT_BUSY_SECONDS`）内有活动。全忙则允许暂时超过上限。

API 级的 `POST /v1/desktops/stop`：主人会 flush + `docker stop`。bot 的 stop 是空操作，等它自己的空闲计时，避免每个回合结束都冷启动。主人 stop 时若有 bot 正忙，只停 `:20`，容器留下。

空闲时钟的洞：`file_*`、`bash`、容器内 `http`（`web_search` / `web_fetch`）不走 `session`，因此不调用 `ensure`，不刷新 `last_used`，也不会把已停止的容器拉起来。它们直接 `docker exec`。一旦回收器把容器停了，这些调用会失败，直到下一次走 `session` 的动作（截图、键鼠、打开程序）把容器启动。反过来，只做文件和 shell、长时间不 `ensure` 的用户，在打开了 `DESKTOPD_MAX_IDLE` 时可能被当成空闲停掉。

### 2.5 Hub 和 desktop bot 怎么到达容器

```
用户 / GUI / bot 回合
        │
        ▼
MaClawSrv  desktop 工具
  MACLAW_HUB_URL + MACLAW_DESKTOP_API_TOKEN
        │  POST /api/v1/desktop-services/{session,app,open,install,file,bash,http,screenshot,ctl,stop,hold}
        ▼
Hub  desktoppool
  按租户设置 desktop_service 解析：用户 > 部门链 > global
  isolated bot 的桌面用户是 <owner>::desk::<bot id>，解析 Docker 服务时只用 owner
        │  POST /v1/desktops/*    Authorization: Bearer <该服务的 access_token>
        ▼
nginx TLS  →  desktopd 127.0.0.1:18081
        │  docker run / start / exec
        ▼
容器 maclaw-desktop-<userkey>
  supervisor ensure → 显示、CDP、noVNC
```

Hub 不连接 Docker。一台 Hub 可以登记多台 desktopd（`base_url` + `access_token` + 镜像和资源），用分配表把用户、部门或全员指到某一台。

MaClawSrv 的回合若带 `hub_user_id`（以及通常的 `hub_tenant_id`、`hub_bot=1`），桌面就是这个人的云桌面，不是 MaClawSrv 进程自己的容器。没有绑定的人时，工具直接失败，不会落到服务账号的本机桌面。

人看屏幕：desktopd 返回的 `novnc_url` 带 gate token。Hub `gateDesktopHandoff` 把它收成 `/api/v1/desktop-handoff/<随机 token>/vnc.html`，在内存里记 2 小时，反代时再把 Bearer 加上。GUI 只连 Hub。dockerd 域名继续只反代 API。Hub 重启后，这条内存映射丢失，需要重新打开视图。

bot 的 `GET/POST /api/v1/bots/{id}/desktop` 走同一条打开路径。`request_takeover` 让回复带 `desktop_handoff` 和 `novnc_url`。人接管期间，以及 noVNC 上一次按键、点击、粘贴之后约 20 秒（`MACLAW_BOT_HUMAN_YIELD_SECONDS`），这块屏幕上的 bot 动作被拒绝。

### 2.6 Computer-use 数据流（云桌面）

云端的 computer use 不是 GUI 的 `computer_observe` / `computer_click`。它是 MaClawSrv 的 `desktop` 工具，动作包括 `screenshot`、`app_run`、`computer`、`app_probe`、`window`、`clipboard_*`、`wait`，以及浏览器侧的 `probe` / `script` / `navigate` / `click` / `type`。

一次键鼠或截图：

1. 回合进入执行（或计划阶段的只读动作）。计划阶段只能 probe、screenshot、app_list、file_read、file_list、skill_list、app_probe、window_list、screen_info、clipboard_get、wait。
2. `desktopSession` 调用 Hub `POST /api/v1/desktop-services/session`。Hub 再调用 desktopd `POST /v1/desktops/session`。这一步创建或启动容器并 `ensure`，返回 `cdp_url` 和 `display`。Hub 客户端超时 2 分钟。镜像还在拉取时，调用方会看到 “still being pulled”，拉取本身最多 `DESKTOPD_IMAGE_PULL_TIMEOUT`（默认 30 分钟），不占这次 HTTP。
3. 同一用户在 MaClawSrv 进程内还有一把 `desktopBinding` 锁，避免两个回合同时驱动同一浏览器。
4. 分叉：
   - **网页**：CDP 连到 gate。`probe` 读无障碍/DOM 控件，`script` 在同一页上点击、输入、滚动。不把浏览器窗口当像素来点。
   - **其它窗口**：`app_open` 在该 `DISPLAY` 上启动程序（argv，不是 shell；拒绝 shell、解释器、apt、以及另一个浏览器）。`app_run` / `computer` 把 Anthropic 或 OpenAI 风格的 computer-use 动作收成 `corelib/desktop/computeruse` 的步骤，再变成 `xdotool`，经 Hub `POST .../app` 到 desktopd `docker exec -e DISPLAY=... xdotool`。截图像素会映射回屏幕坐标。
   - **截图**：Hub `POST .../screenshot` → desktopd 在容器里 `import -window root`（没有 ImageMagick 时用 scrot）。可以缩放、区域、jpeg/webp、画光标、画 a11y 标号。图片回到模型；支持视觉的模型直接看图，文本模型才走本地 OCR / OmniParser。base64 不写入对话历史正文。
   - **控件树、剪贴板、窗口、等待**：`POST .../ctl`，supervisor 在对应显示上做（含 AT-SPI `a11y`）。
5. 密码框有硬拒绝（`not_password_field`）。扫码登录、验证码、支付才 `request_takeover`，把屏幕交给人。

文件和 shell 是同一容器上的另一条门：`POST /v1/desktops/file|bash|http|install`，不经过 X，也不刷新空闲时钟。`bash` 在容器里执行。容器外面的机器用单独的 `ssh` 工具，不走这条。

isolated bot（`desktop_mode=isolated`，默认仍是 `shared`）不新开容器。desktopd 把用户串拆开：容器是主人的，显示是 bot 的。supervisor key 是主人 16 hex 再加 bot id 的 16 hex。显示从 `:21` 到 `:50`。这块屏幕只有 Xvfb、xfwm4、自己的 Chromium profile（不复制主人的登录）、一块轻量面板。VNC 在第一次有人看的时候才起。CDP 和 noVNC 仍从主人已发布的 `19020` / `6080` 进去，按 bot 自己的 token 转到对应的内部端口。客户端不能指定 bot 的 display 号。

`shared` 模式下，同一个人的所有 bot 共用 `:20` 和同一个 Chrome profile。

### 2.7 现在已经有的持久化，和没有的

已有：

- 四个卷在 `docker stop`、容器删除、镜像迁移之后都还在。浏览器 profile、家目录、`/opt`、`/usr/local` 跟着用户。
- 容器可写层在停止后还在；只有删除容器时才依赖 commit 出的 `:state`。
- flush 把网站登录写入 profile，所以冷启动 Chrome 后登录还在。打开的未保存窗口、内存里的会话、未刷盘的页面存储不保证在。
- 对话、run、接管标记、handoff token 在 Hub / MaClawSrv，不在容器里。

没有：

- 没有「睡眠」状态。容器要么在跑（占着 cgroup 内存和 CPU 配额），要么被 stop 后整机进程都没了。
- 没有内存快照，没有把卷同步到对象存储。卷只在这台 Docker 主机的本地。
- 没有 runtime 接口。调用链写死 `docker` 子命令。
- 主人桌面的空闲回收默认关闭；打开后也是 24 小时量级，而且空闲定义不包含文件/shell 活动。
- bot 屏幕会自己停，但容器和主人桌面的内存不释放。

---

## 3. 差距

| 目标 | 现在 | 差距 |
| --- | --- | --- |
| 每用户一台隔离机器 | 每用户一个 Docker 容器，共享宿主内核 | 边界已经按用户切开，隔离强度不够；以后换 microVM，而不是再拆容器 |
| 机器内 supervisor：文件服务 + 命令执行 + 多屏 | supervisor 是被 exec 的脚本；文件/shell/截图/点击由宿主机 `docker exec` | 控制面在容器外，runtime 一换就全部失效 |
| 屏幕 1 独立 Chrome；屏幕 2..N 按活跃 agent 增加 | `:20` 主人全套 XFCE+Chrome；isolated bot 用 `:21+` 轻量屏，默认 `shared` 仍挤在 `:20` | 模型和端口复用可以留；`shared` 与「每 agent 一块屏」不一致；屏幕之间没有安全边界（见第 8 节） |
| 文件、工具、登录跨会话还在 | 卷 + 可写层 + flush 已经做到登录和卷内文件 | 缺对象存储副本；apt 层在换镜像 tag 时会丢 |
| 空闲几分钟后睡眠：同步文件、存快照、停机器 | 默认不回收；可选 24h `docker stop`；bot 屏 10 分钟只停进程 | 没有短空闲、没有同步、没有快照、睡眠时容器仍可能因 `--restart` 以外的路径被拉起 |
| 下次工具调用/消息唤醒，登录和工具还在，对话在服务端 | `session` 会 `docker start` + 冷启动 XFCE/Chrome；对话本来就在服务端 | 没有 waking 状态，调用方只能干等；file/bash 不会唤醒；没有快照恢复 |
| Docker 现在、Firecracker 以后，接口同一套 | 只有 Docker | 需要 runtime 接口；Firecracker 还要 KVM |

不改的决定：

- 隔离单位仍是**用户**，不是每个 agent 一台机器。agent 是这块机器上的屏幕。
- 对话、记忆、run 状态继续放在服务端。快照里不放对话，避免唤醒后分叉出两份历史。
- Hub 仍是 desktopd 的客户端，仍按用户/部门/全局做分配。bot 不拿 Docker socket。

---

## 4. 组件

```
                    Hub / MaClawSrv
                    （对话、run、分配、handoff）
                              │  现有 /v1 与 /api/v1/desktop-services
                              ▼
                    desktopd 控制面（宿主机）
                    ┌─────────────────────────────────┐
                    │ 生命周期管理器  每用户锁  租约     │
                    │ Runtime 接口                     │
                    │   DockerRuntime    （现在）      │
                    │   FirecrackerRuntime（以后）     │
                    └─────────────────────────────────┘
                              │ Ensure / Exec / Snapshot / Restore
                              ▼
                    该用户的一台机器
                    ┌─────────────────────────────────┐
                    │ supervisor（常驻）               │
                    │   文件服务：文件、应用连接、实时画面 │
                    │   命令执行：shell、截图、点击     │
                    │   屏幕 1：XFCE + 此人的 Chrome    │
                    │   屏幕 2..N：每个活跃 agent 一块  │
                    └─────────────────────────────────┘
                              │
                    本地卷 + 对象存储副本 + 可选内存快照
```

### 4.1 Runtime 接口

控制面只依赖这一组操作。Docker 和 Firecracker 各自实现，Hub 的 HTTP 形状在迁移期间保持不变。

| 操作 | 含义 |
| --- | --- |
| `Ensure(spec)` | 机器进入可执行。已在跑则返回；睡眠则走唤醒；没有则创建 |
| `Exec(id, req)` | 在机器里做一件事：shell、文件、截图、点击、ctl。实现可以是 `docker exec`，以后是进客户机的 API |
| `Snapshot(id)` | 刷盘、同步文件、留下恢复所需的磁盘镜像；若该 runtime 支持，再留内存镜像 |
| `Restore(id)` | 从最近一次成功的快照或从「停着的容器 + 卷」拉起来，直到 supervisor 健康 |
| `Stop(id)` | 进入睡眠或显式停止。必须先 flush |
| `Status(id)` | `running` / `idle` / `sleeping` / `waking` 以及屏幕表 |

`Exec` 在机器睡着时由控制面先 `Ensure`，调用方看到的仍是一次工具调用。

DockerRuntime 的对应关系：

| 接口 | 现在的实现 |
| --- | --- |
| Ensure | `createUnlocked` + `ensure` |
| Exec | `docker exec`（文件、bash、xdotool、截图、ctl） |
| Snapshot | flush；卷打点或同步；容器留下则不必 commit；要删容器才 `docker commit` |
| Restore | `docker start`，或用 `:state` + 四个卷 `docker run`，然后 `ensure` |
| Stop | 现有 `stopUnlocked` |

### 4.2 机器内 supervisor

今天的脚本保留职责，但变成机器里的常驻进程，对控制面暴露一个只听内部地址的 API。宿主机不再为每个动作拼 `docker exec` 参数。这样 Firecracker 阶段不用把 Docker 语义带进 microVM。

三个职责：

1. **文件服务**
   - 文件：今天的 read / write / edit / list / bytes，路径仍限制在容器家目录和允许的根（现有 `ContainerPath` 规则）。
   - 应用连接：每块屏幕的 CDP 地址、profile 路径、gate token、显示号。控制面和 MaClawSrv 向它要连接，而不是自己猜 `18000+display`。
   - 实时画面：这块屏幕的 noVNC/websockify。人看屏幕仍经 Hub handoff，不把客户机端口直接暴露到公网。
2. **命令执行**
   - shell：今天的 `bash`。
   - 截图：今天的 `import` / scrot 和缩放选项。
   - 点击和键鼠：今天的 xdotool 与 ctl（窗口、剪贴板、等待、AT-SPI）。
3. **屏幕管理**
   - 屏幕 1 固定给这个人：全套会话 + 自己的 Chrome profile（`/desktops/<userkey>/profile`）。
   - 屏幕 2..N 按活跃 agent 增加，每块自己的 Chrome profile（`/desktops/<botkey>/profile`），不复制屏幕 1 的登录。
   - 屏幕进程的空闲停用保持今天的 bot watcher 语义；停屏幕不等于睡整机。

pid 1 仍要在关机信号上调用 flush，再退出。CRIU 或 Firecracker 快照不能把「未 flush 的 Chrome」当成唯一的登录副本。

### 4.3 控制面（desktopd）

在现有 HTTP 和回收器之上加生命周期管理器：

- 每个用户一条记录：状态、runtime 种类、宿主机、快照代际、屏幕表、最后活动时间。
- 活动心跳：`session`、键鼠、截图、ctl、文件、bash、http、noVNC 输入、CDP 流量都算活动。补上今天 file/bash 不刷新 `last_used` 的洞。
- 睡眠和唤醒见第 5 节。
- 分配仍由 Hub 决定用户在哪一台 desktopd。记录里写宿主机身份，避免两台控制面同时 Restore。

### 4.4 服务端（Hub / MaClawSrv）

对话状态不下放。唤醒需要的只是：这个人的下一次桌面工具调用（以及执行阶段里已经确定要用桌面的回合）触发 `Ensure`。

纯聊天、只改记忆、不碰桌面的回合不唤醒。否则每条消息都会把机器拉起来，短空闲就没有意义。`desktop` 工具在计划阶段的只读动作（截图、列目录）**会**唤醒，因为它们要读机器里的屏幕或文件。

Hub 的 handoff、接管、yield 保留。睡眠中打开视图等于唤醒到 running，再返回 noVNC。

---

## 5. 生命周期

主状态就是 running、idle、sleeping、waking。另外两个辅助状态用来表示还没有机器，以及恢复失败。

```
                 首次 Ensure
    [*] ───────────────────────────► running
                                      │
                     无活动达到 T_idle │
                                      ▼
                                    idle
                                      │ 仍无活动达到 T_drain
                                      │ 且没有忙屏幕、没有打开的视图、没有在途 Exec
                                      ▼
                               刷登录、同步文件、做快照
                                      │
                                      ▼
                                  sleeping ◄── 快照完成且机器已停
                                      │
                     工具调用 / 要看屏幕的视图 / 执行阶段桌面回合
                                      │
                                      ▼
                                   waking
                                   /    \
                          健康检查过      失败
                                /          \
                               ▼            ▼
                           running        failed
                                            │
                                            │ 退回：用卷 + 磁盘层冷启动
                                            ▼
                                         running
```

| 状态 | 机器 | 调用方看到的 |
| --- | --- | --- |
| running | 进程在，supervisor 健康 | 工具直接执行 |
| idle | 仍在跑，只是计时中 | 与 running 相同；任一活动回到 running 并清零计时 |
| sleeping | 进程不在；卷、磁盘层、对象存储副本在；内存快照可能在 | 下一次需要桌面的调用进入 waking |
| waking | 正在 start 或 restore | 同一用户的其它调用合并到同一次唤醒（singleflight） |
| failed | 快照恢复没过健康检查 | 自动冷启动；冷启动再失败才把错误交给工具 |
| absent | 从未创建 | Ensure 走创建，创建完成后是 running，不经过 sleeping |

退出 running 的条件（全部满足才从 idle 进入睡眠）：

- 超过空闲时间没有活动。建议 **5 分钟**（「几分钟」的具体值，见第 14 节）。bot 屏幕自己的 10 分钟停屏可以更短，那只释放 Chrome，不睡整机。
- 没有接管，没有 120 秒内的 bot 活动，没有打开的 noVNC，没有在途的 `Exec` / apt。
- 拿到该用户的睡眠锁。

睡眠动作的顺序固定：

1. 状态改为「正在睡眠」，开始拒绝新的快照代际；新的 Ensure 直接取消睡眠并留在 running。
2. flush 屏幕 1 和所有还开着的 agent 屏幕的浏览器（沿用今天的 flush，不能跳过）。
3. 同步四个卷到对象存储（第 7 节）。同步失败则**不**停机，保持 running，记下一次失败，下一轮再试。
4. 写磁盘快照：见第 6 节的选型。一期就是「容器停了但还在，可写层还在」。
5. 停机。Docker 上是 `docker stop --time 25`。因为 restart policy 是 `unless-stopped`，显式 stop 之后守护进程重启不会把它拉起来。这一点要保持。
6. 记录状态 sleeping、快照 id、同步代际。然后这个用户不再占宿主机内存和 CPU。

唤醒：

1. 拿同一把用户锁。已经在 waking 的请求等待同一次结果。
2. Restore。健康条件：supervisor API 可应答，屏幕 1 的 X socket 在（若这次调用需要屏幕），需要的 Chrome CDP 可连接。只做文件/shell 的调用可以只要 supervisor 和文件系统，不必等 XFCE 画完，但 Chrome 登录所在的 profile 锁必须已是这次启动清过的。
3. 健康则 running，执行当初的工具调用。
4. 不健康则丢掉这次内存快照（若有），用卷和 `:state`/可写层冷启动。登录来自 profile 文件，不来自内存。对话仍从服务端接着。

显式 stop（人在管理页关掉桌面）与睡眠走同一条 flush 和停机，但状态记为 sleeping，下次工具调用仍可唤醒。不提供「永久销毁」除非管理员删卷。

---

## 6. 唤醒延迟目标

下面是目标，不是现网测量。现网没有分位数数据。用来决定调用方超时和要不要做内存快照。

| 路径 | p50 | p95 | 说明 |
| --- | --- | --- | --- |
| 热：已经 running | +0.2s | +1s | 与今天的一次 `docker exec` 同级，不含模型思考 |
| 温：容器还在，只是 stop 了 | 8s | 20s | `docker start` + Xvfb/XFCE 或至少 Chrome 就绪。supervisor 的 `wait_for` 单组件上限是 20s（80×0.25s） |
| 磁盘冷：容器已删，用卷 + `:state` 重建 | 15s | 40s | 不含拉基础镜像 |
| 内存快照恢复（仅 Firecracker，且有 KVM） | 2s | 8s | 恢复后仍要重连 CDP/noVNC；TCP 连接本身不会活在快照里 |
| CRIU `docker checkpoint` | 不设目标 | 试验项 | 见第 6.1 节，不作为一期验收 |
| 基础镜像缺失、正在 pull | 不算进唤醒 | 不算进唤醒 | 现网最多 30 分钟，只发生在新主机 |

调用方预算：

- 工具调用内等待唤醒最多 **25 秒**。到点仍是 waking，返回可重试的「桌面正在唤醒」，模型或 runtime 再调一次。不要占满 bot 消息的 30 分钟预算。
- Hub → desktopd 的 2 分钟客户端超时保持为硬上限，盖住温启动的 p95。
- nginx 的 600 秒读超时继续只服务安装包这类长操作，不拿来掩盖唤醒失败。

一期只承诺「温启动」那一行。内存快照的 2s/8s 是 Firecracker 阶段的目标，在 KVM 确认之前不写进 SLO。

### 6.1 休眠时存什么：CRIU、commit+卷、Firecracker

三件事要分开，不要合成一个「快照」词。

**文件与登录（必须有）**

flush + 四个卷。这是登录、家目录、`/opt`、`/usr/local` 的权威副本。不论用不用内存快照都要做。对象存储上再留一份，宿主机磁盘坏了还能恢复。

**磁盘层（一期就用这个）**

`docker stop` 留下容器和可写层，apt 装的包装在层里，下次 `docker start` 还在。这就是现网 stop 的语义，也是一期睡眠的实现。

只有在必须删掉容器时才 `docker commit` 成 `:state`（换镜像、换 shm、换代理、卷迁移）。commit 的层不含命名卷里的数据，卷要单独挂回。`:state` 的 `maclaw.image` 必须等于当前请求的镜像名，这条现网规则保持。

不要把「每次睡眠都 commit」当成默认。commit 要复制整个可写层，又慢又占磁盘，而 stop 已经保住了层。

**内存快照（一期不做）**

`docker checkpoint`（CRIU）要实验性守护进程特性、宿主上的 criu、以及足够的内核配置（`CONFIG_CHECKPOINT_RESTORE`）。现网镜像还带着 `close_range` 垫片，因为 Docker 20.10 和旧 libseccomp 让 GLib 起不来 XFCE。这类宿主上的 CRIU 不适合当生产路径。即便在 Docker 29 的新主机上，下面这些也会让一次「看起来成功」的 checkpoint 在恢复后不可用：

- Chromium 的沙箱、seccomp、多进程和大量线程。
- X11 的 `/tmp/.X11-unix` 套接字、dbus 套接字、gate 正在接的 TCP。恢复之后这些连接是断的，CDP 和 noVNC 必须由 supervisor 重新监听，调用方不能假设旧的 `cdp_url` 端口还活着。
- 1g `/dev/shm` 和四个外部命名卷。CRIU 对外部挂载和外部 unix socket 要额外配置，配错就漏掉 profile。
- pid 1 是 `sleep infinity` 加 trap 的 shell，不是正常的 init。
- 恢复时间不一定短于「docker start + 冷启动 Chrome」。若 p95 比温启动还差，内存快照就没有产品价值，因为登录已经在 profile 里。

试验的通过标准（全部满足才允许进入可选路径，否则永久停在 commit+卷 / stop）：

- 非生产宿主，同一套 `maclaw-gui:2`，打开两个已登录站点，睡眠再恢复。
- 恢复后 profile 里的登录还在，且与恢复前 flush 的文件一致。
- 恢复后 supervisor 健康检查在 30 秒内通过，CDP 能新连上。
- 连续 20 次无一次需要手工 `docker rm`。
- p95 恢复时间小于温启动 p95，否则不启用。

**Firecracker snapshot**

Firecracker 的 snapshot/restore 是它自己的功能：客户机内存和磁盘一起恢复，登录和进程树可以还在内存里。这才是「唤醒后窗口还开着」的路径。前提是宿主机有 `/dev/kvm`。没有 KVM 就不做这个 runtime。TCP 和 vsock 上的 CDP/noVNC 仍然要在恢复后重绑，健康检查与 Docker 路径相同。

---

## 7. 存储布局

宿主机本地（一期）：

```
/data/soft/maclaw_desktopd/
  bin/desktopd
  .env
  state/                          # 现有面板状态，不动
  logs/desktopd.log
  computers/<tenant>/<userkey>/   # 新增，0700
    meta.json                     # 状态、runtime、快照 id、屏幕表、代际
    lease.json                    # 这台控制面持有的租约
```

Docker 卷名保持不变：`maclaw-desktops-<key>`、`maclaw-home-<key>`、`maclaw-opt-<key>`、`maclaw-local-<key>`。容器名保持 `maclaw-desktop-<key>`。`:state` 镜像名保持 `maclaw-desktop-user-<key>:state`。

机器内部：

```
/desktops/<userkey>/profile       # 屏幕 1 的 Chrome
/desktops/<userkey>/pids.json
/desktops/<botkey>/profile        # 屏幕 2..N，botkey = userkey + bot id 的 16 hex
/desktops/registry.json           # 显示分配
/desktops/lock
/home/desktop                     # 用户文件
/opt
/usr/local
```

对象存储（二期同步，建议腾讯云 COS，与现网同一账号体系；桶名实施时再定）：

```
maclaw-desktops/<tenant>/<userkey>/
  meta.json
  volumes/desktops-<generation>.tar.zst
  volumes/home-<generation>.tar.zst
  volumes/opt-<generation>.tar.zst
  volumes/usr-local-<generation>.tar.zst
  layer/<image-tag>-<generation>.tar   # 仅当发生了 commit；平时可省略，以宿主机容器层为准
```

规则：

- 本地卷是热副本，对象存储是睡眠后的冷副本。唤醒优先用本地。本地丢了才从对象存储拉回。
- 同步是增量代际。睡眠只在四个卷都到达同一代际后才停机。
- 快照和卷里有 cookie 和登录。对象存储对象加密，桶策略按租户，不进共享的无访问控制备份。`meta.json` 不写 token 明文；gate token 留在卷里的 registry，和今天一样。
- 对话历史不进这个前缀。

---

## 8. 多屏分配

| 屏幕 | 显示号 | 何时存在 | Chrome | 会话 |
| --- | --- | --- | --- | --- |
| 1，这个人 | `:20` 固定 | 机器 running 且调用需要画面时确保启动；只做文件/shell 可以延后 | `/desktops/<userkey>/profile` | 全套 XFCE、fcitx5、dock |
| 2..N，每个活跃 agent | `:21`–`:50` 里稳定绑定 | 该 agent 的第一次桌面动作 | `/desktops/<botkey>/profile`，不复制屏幕 1 | Xvfb + xfwm4 + 轻面板；fcitx5 仅在人接管时启动 |

分配规则沿用现网，并写进 `meta.json` 以便控制面不依赖一次 `docker exec`：

- 显示号由控制面和 supervisor 决定，客户端不能指定。
- 同一个 agent id 永远复用同一显示号和同一 profile，直到管理员重置。
- 同时运行的 agent 屏幕默认仍是 3（`MACLAW_DESKTOP_MAX_BOTS`）。更久未用且不忙的被停掉，profile 保留。全忙则暂时超过上限，不杀正在做的任务。
- 屏幕空闲停进程的默认保持 10 分钟，短于整机睡眠的 5 分钟是错的，所以整机睡眠建议 5 分钟时，屏幕空闲应 **小于或等于** 整机空闲，建议屏幕 3 分钟、整机 5 分钟：先收 Chrome，再睡整机。这个配对在第 14 节是待确认项。
- `desktop_mode=shared` 的 bot 继续用屏幕 1。目标态是每个活跃 agent 一块屏幕；从 shared 迁到 isolated 是产品开关，不是 runtime 的前提。睡眠设计对两种模式都成立：shared 只有屏幕 1 在干活。

屏幕不是安全边界。它们共享同一台机器的文件系统和用户。agent 能读到 `/home/desktop` 和屏幕 1 的 profile 目录，除非以后再做挂载命名空间。本文不把「agent 之间互相看不见家目录」当成目标。

---

## 9. 并发与锁

已经有的锁，全部保留：

| 锁 | 位置 | 保护什么 |
| --- | --- | --- |
| `userGates`（每用户一把） | desktopd 进程 | 同一用户的创建、停止、session、文件、bash 不交错。stop 的 flush 不能和下一次 start 重叠，否则会开出第二个浏览器，登录写进正在退出的那个 |
| `/desktops/lock` | 容器内 flock | `ensure` / `stop` / bot 空闲停屏 |
| `desktopBinding` | MaClawSrv 进程 | 同一桌面用户的键鼠和 CDP 回合串行 |
| 接管 / human yield | supervisor 的 `takeover.json` 和 `human_input` | 人在用这块屏幕时 bot 的动作失败 |

要加的：

- **同一把用户锁覆盖 Snapshot 和 Restore**。睡眠进行到 flush 之后若来了 Ensure，放弃这次睡眠，状态回到 running，不停机。
- **singleflight 唤醒**。同一用户并发的工具调用只做一次 Restore，其它等待。
- **租约**。`computers/<tenant>/<userkey>/lease.json` 记录哪一台 desktopd 拥有这台机器。Hub 把用户改分配到另一台服务时，旧宿主必须先睡眠并同步完，新宿主再 Restore。两台同时 `docker start` 是禁止的。回收器继续负责「Hub 已经不认识、但本机还有容器」的孤儿，但在租约未过期时不把别人的机器停掉。
- **在途 Exec 计数**。降到 0 才允许开始睡眠。apt（最长约 20 分钟）算在途。
- **代际**。每次成功睡眠把 generation +1。恢复使用这一代。更新的同步没完成就不能停机。

死锁顺序：先 Hub 分配，再 desktopd 用户锁，再容器内 flock，再 MaClawSrv binding。不反向持有。

---

## 10. 安全隔离

现在的边界：

- 用户之间靠独立容器、独立卷、独立 `:state` 镜像。key 是哈希，卷名里没有原始 user id。
- 容器内是 root。与宿主共享内核。`close_range` 垫片和 seccomp 差异说明安全策略跟着 Docker 版本走。
- CDP/noVNC 发布在 `0.0.0.0` 的随机端口。Docker 的 iptables 绕过 ufw，公网够得到这些端口，只靠每显示一个 Bearer token。文档已要求用 `DOCKER-USER` 限制来源，而不是改 ufw。
- Hub handoff 把 token 留在服务端，浏览器只拿 2 小时的 Hub 路径。
- 出网代理拒绝 loopback、私网和云 metadata，和 MCP 的 SSRF 姿态一致。
- 屏幕之间不隔离，见第 8 节。

目标增加的约束：

- runtime 接口的实现不得把 Docker socket 或 KVM 设备交给客户机，也不得交给 bot。
- 一期仍是 Docker：补上「发布端口只绑定宿主机内网地址」，由 desktopd 或 Hub 反代。在做到之前，不要把睡眠后的唤醒端口再暴露到新的公网段。
- 对象存储上的卷加密，访问只限该租户的控制面身份。快照等于密码库。
- Firecracker 阶段用 microVM 换内核隔离。Jailer 限制客户机看到的设备。没有 KVM 的宿主留在 DockerRuntime，不降级成「用 qemu 软件仿真冒充 Firecracker」。
- 客户机里继续不放 Hub token、不放其它用户的 key、不放对话库。
- 睡眠和唤醒的日志沿用现有脱敏：不打印 gate token、cookie、代理 key。

---

## 11. 资源与容量

数字来源要分开看。代码和文档里写过的：

- 每容器 cgroup：3 GiB 内存、1.5 CPU、1 GiB shm。
- 注释中的测量：完整桌面加少量应用大约 600 MiB；bot 空闲显示大约 70 MiB，一块 bot Chrome 大约 0.25 GiB，规划时按每个活跃 bot 0.5 GiB。
- 公开镜像：压缩约 783 MiB，解压约 1.85 GiB；另一次 Ubuntu 24.04 部署记录解压后约 2.7 GiB。基础镜像在宿主机上只有一份。
- 屏幕默认 1440×900。

下面是规划估算，不是新的测量。上线前用 `docker stats` 对真实桌面取样再改常数。

单个 running 用户：

```
RSS ≈ 1.5–2.5 GiB（屏幕 1：600 MiB 桌面 + Chrome）
    + 0.5 GiB × 正在跑的 agent 屏幕数
CPU 配额 1.5；空闲时接近 0，computer use 时软件渲染会打满配额
shm 上限 1 GiB，按实际占用计，不要按上限预留
```

宿主机能同时跑多少台（内存）：

```
槽位 = floor( (内存 - 预留) / 每台 RSS )
预留 ≈ 8 GiB（同一台上的 Hub + MaClawSrv + desktopd + dockerd + 系统）
```

举例：64 GiB 内存、预留 8 GiB、每台按 2.5 GiB（屏幕 1 + 一个 agent 屏幕）→ 约 22 台同时 running。只算屏幕 1、按 2 GiB → 约 28 台。三块 agent 屏幕同时开的重用户按约 4 GiB → 同一台宿主大约 14 个这样的用户。

CPU：16 核上，建议同时处于 computer-use（截图+键鼠+Chrome）的会话不超过 8。running 但 idle 的桌面几乎不占 CPU，占的是内存，所以短空闲睡眠的收益主要在内存。

睡眠之后：内存和 CPU 释放。磁盘仍在。

```
每用户磁盘 ≈ 卷（profile + 家目录 + /opt + /usr/local，规划 2–8 GiB）
           + 可写层（apt，0–数 GiB）
           + 对象存储同一数量级的一份
100 个睡眠用户、按每用户 6 GiB ≈ 600 GiB 本地 + 600 GiB 对象存储
```

内存快照若将来启用，再按每台 1–3 GiB（压缩后的客户机内存）另计。一期不加这笔。

睡眠带来的容量变化：今天容器默认一直 running，宿主槽位被「打开过一次但人已经走了」的桌面占满。5 分钟睡眠之后，槽位只给当前真正在用的人。这是一期要验证的主要容量收益。

---

## 12. 分阶段迁移

每阶段都可以单独停住。Hub 的 URL 和 `desktop` 工具的动作名不变。

**阶段 0（本文）**  
只合并设计文档。不改二进制，不改现网 `.env`。

**阶段 1：接口和记账，行为不变**  
抽出 Runtime，DockerRuntime 原样包住现在的 create / start / stop / exec。给每个用户写 `meta.json`，状态只有 running 和 absent。活动心跳覆盖 file、bash、http、截图、键鼠、CDP、noVNC。不缩短空闲，不开启默认回收。验收：现有 desktopd 测试和冒烟（`desktopd/scripts/smoke_gui2.sh`）通过，已有容器不被重建。

**阶段 2：短空闲睡眠，磁盘级，可按租户打开**  
空闲 5 分钟后 flush、把卷同步到对象存储、`docker stop`。下次 `session`、文件、bash、截图、视图都先 Ensure。温启动超过 25 秒返回可重试错误。restart policy 保持 `unless-stopped`。默认仍关，先对一个测试租户打开。验收：登录站点在睡眠后仍登录；apt 装上的包还在；file/bash 能把睡着的机器唤醒；同步失败时容器不被停掉。

**阶段 3：CRIU 试验（不阻塞阶段 2）**  
在非生产宿主上按第 6.1 节的标准做 20 次。失败或慢于温启动，就在文档和配置里把 CRIU 标成不做，睡眠实现停留在阶段 2。

**阶段 4：客户机内的文件服务和命令执行**  
supervisor 常驻并提供内部 API。desktopd 的 Exec 改走这个 API。CDP/noVNC 不再绑定 `0.0.0.0`，只经控制面或 Hub。这一步仍是 Docker。

**阶段 5：Firecracker runtime**  
只在 `ls /dev/kvm` 成功的宿主上提供。同一套 supervisor 和卷的逻辑放进 microVM。snapshot/restore 对应该 runtime 的 Snapshot/Restore。没有 KVM 的腾讯云 CVM 继续 DockerRuntime。两种 runtime 可以按宿主配置并存，一个用户不同时存在于两边。

回滚：阶段 2 的开关关掉就回到「容器一直跑」。卷和容器都还在，不需要从对象存储恢复。对象存储副本保留。

---

## 13. 风险

**CRIU 不可用或恢复后是坏的。**  
Chromium、X11、外部卷、shm、旧 Docker/seccomp、shell pid 1，都在第 6.1 节。缓解：一期不依赖它；试验有硬性通过标准；失败则停在 stop+卷。

**腾讯云上没有 KVM，Firecracker 跑不起来。**  
现网使用腾讯云镜像源和元数据，文档中的主机是 CVM（Ubuntu 18.04 与 24.04），不是裸金属。CVM 本身是虚拟机，客户机里默认没有 `/dev/kvm`，嵌套虚拟化也不会因为装了 Docker 就出现。Firecracker 在这种机器上起不来。可选的宿主是裸金属，或腾讯云明确打开了嵌套虚拟化的实例族。在运维确认某一台生产宿主 `ls /dev/kvm` 可读、并且 Firecracker 能拉起一个客户机之前，阶段 5 不排进生产。不要用纯软件仿真顶上。

**`--restart unless-stopped` 和睡眠抢机器。**  
显式 `docker stop` 与这个策略兼容。若有人改成 `always`，或守护进程在 stop 完成前重启，睡眠会被破坏。阶段 2 要有测试钉住策略，并在启动时拒绝 `always`。

**空闲判定漏掉活动，把正在 apt 或正在 bash 的用户停掉。**  
这是现网 `last_used` 只在 `ensure` 更新的延伸。阶段 1 的心跳是阶段 2 的前提，不能对调。

**flush 和快照竞态，登录写进即将销毁的浏览器。**  
现网已经用用户锁和 `/desktops/lock` 避免「stop 写到一半，start 又开一个 Chrome」。睡眠必须走同一把锁，不能另起一个不拿锁的 checkpoint。

**快照和卷里的 cookie 泄漏。**  
对象存储权限和加密是阶段 2 的上线条件，不是后续优化。日志不落 token。

**Hub 内存 handoff 在重启后失效。**  
与今天相同。唤醒成功但人还拿着旧的 noVNC 路径时，GUI 需要重新 `GET .../desktop`。设计不把 handoff 表放进客户机快照。

**换镜像 tag 丢掉 apt 层。**  
现网行为。睡眠不改变它。文档和运维要继续把「换 tag」当成一次迁移，而不是一次睡眠。

**容量估算偏差。**  
600 MiB / 0.5 GiB 来自注释和 `docs/desktop-per-bot.md`，不是这次在生产上采的样。阶段 2 打开之前，对真实容器做一次 `docker stats` 采样，再写进宿主的槽位配置。

---

## 14. 开放问题

1. 整机空闲用 5 分钟还是 3 分钟？屏幕级空闲是否定为更短的 3 分钟？需要一次现网「人离开后多久还会立刻回来」的抽样，避免刚睡就被唤醒。
2. 只做文件和 shell 的唤醒，是否可以不启动屏幕 1 的 XFCE 和 Chrome？可以缩短温启动，但 profile 锁和「登录只存在于跑着的 Chrome 里」的现网假设要再对一下。
3. 对象存储用哪个 COS 桶、哪一个账号、跨地域还是同地域？同地域延迟更适合放进睡眠的关键路径。
4. 生产 CVM 的具体实例族，以及有没有一台已经打开嵌套虚拟化或裸金属可以做 Firecracker 试验？以 `ls /dev/kvm` 和一次 microVM 启动为准，不以控制台文案为准。
5. `shared` 何时改成默认 isolated？这决定屏幕 2..N 的密度，不影响睡眠状态机，但影响每台 RSS。
6. 唤醒超过 25 秒时，错误文案要不要变成一条用户可见的「正在打开电脑」，而不是模型可重试的工具错误？这是产品决定。
7. 用户被分配到另一台 desktopd 时，卷的迁移走对象存储拉取，还是要求两台宿主共享一个卷后端？阶段 2 可以先禁止在睡眠设计落地前做跨宿主搬迁，现网回收器已经能处理「分配改了但旧容器还在」。
8. 管理员「删除桌面」是否同时删对象存储代际，以及保留几天？本文不规定销毁流程。
9. `send_to_user` 的进度句要不要继续限制成现有的固定动词表？放开成模型自由文本时，如何沿用手机通知那套脱敏，避免把密码和路径写进进度行。
10. `channel=auto` 的终态消息，邮件默认开还是关？进度永远不进邮件，这一条建议保持。

---

## 15. 非目标

- 不改 GUI 本机 `computer_*` 工具。
- 不把每个 agent 做成一台虚拟机。
- 不把对话历史放进客户机或快照。
- 不一期实现 CRIU 或 Firecracker。
- 不在这个设计里改 `desktop` 工具的动作名、CDP 的使用方式、或接管/交还的产品语义。第 16 节新增的是对用户的出口，不替换这些干活工具。

---

## 16. 单一对用户出口

### 16.1 原则

agent 可以有很多干活的工具：shell、文件、浏览器、桌面、记忆、技能、子助手、云端 agent、应用连接、网页搜索、生图、录屏。这些工具的返回值只给模型，用来决定下一步。

对用户说话只有一个工具，名字用 `send_to_user`。模型回合结束时打印的正文不投递，不出现在任何一个表面上。没有调用这个工具，用户就看不到新的一句。

这个工具不选择「怎么干完」。它只产生一条出站消息。Hub 负责把它送到这个人打开着的表面，并按每个表面的格式改写。桌面 GUI、手机、邮件是第一批；网页、Slack、语音以后挂上同一个路由器，不再各写一条从模型正文里抄文字的路径。

干活和说话分开之后，桌面睡眠（第 5 节）只跟工具调用有关。一条纯 `send_to_user` 不需要唤醒机器。一条要读屏幕或写文件的工具调用才唤醒。

### 16.2 现在消息是怎么到人面前的

desktop bot 没有「发送」这个动作。用户看见的字，是这一回合结束时 MaClawSrv 存下来的助手消息正文。

**前台对话**

1. 桌面 GUI `SendDesktopBotTask` 立刻返回。真正的投递在后台 `finishDesktopBotTask`。
2. GUI 以异步方式调用 Hub `POST /api/v1/bots/{id}/messages`（`Prefer: respond-async` 或 `async=true`）。同步路径是同一个 `PostMessagePhase`，只是会一直等到跑完。
3. Hub `AdmitDesktopMessage` 在共享桌面模式下先打开这个人的云桌面，再以机主身份向 MaClawSrv `POST /api/v1/instances/{id}/messages`。`client_message_id` 保证同一次用户发送不会开出两个 run。
4. 跑的过程中，GUI 轮询 `GET /api/v1/bots/{id}/runs/{runID}`。没结束是 202，里面没有助手正文。
5. 结束后，Hub 的 watcher 读 MaClawSrv 上的 `desktopTurnResult`。`settleDesktopCommand` 把 `Message.Content` 抄进 `Reply.Text`。截图和 `desktop` 工具 `action=deliver` 附上的文件来自这条消息的 attachments。登录交接、接管原因、`ask_user_*` 元数据挂在同一个 `Reply` 上。
6. 正文为空且不是交接，Hub 报 `run ended without a result`，不当成一条空消息。正文为空但是交接，Hub 填一句固定的中文，请人到浏览器里完成登录。
7. GUI 用 `ai-assistant-response` 把 `Reply.Text` 画成聊天气泡，图片和文件落到本机路径后再显示。然后 `POST /api/v1/bots/{id}/runs/{runID}/ack`。
8. GUI 当时不在线时，`GET /api/v1/bots/pending` 列出还没 ack 的已完成 run。`desktop_bot_status` 把它们补画出来，并在没有正文但有图片或文件时，用任务名造一行说明。

所以前台气泡的权威是「模型最后写下的助手正文」，加上 Hub 在空正文时补的那一句。

**后台进度**

进度不走气泡。

- MaClawSrv 在桌面工具的每一步写入一条活动短语，词表是闭集：`正在点击`、`正在输入`、`正在看屏幕`、`正在执行命令`、`正在交付`、`正在思考` 等（`MaClawSrv/desktop_activity.go`）。输入的文字、窗口标题、工具参数不在词表里，避免密码或路径变成进度。
- Hub watcher 把短语记在 admit 状态上。GUI 轮询到 202 时用 `desktop-bot-activity` 更新工作行，不新增气泡。run 一结束短语被清掉。
- 桌面正忙时，下一条任务不失败，GUI 发 `desktop-bot-queued`，气泡仍等后来的结果。
- 定时任务走另一条事件 `desktop-bot-report`，字段和普通回复同一套。
- 人问「进度怎么样」时，Hub `GET /api/v1/bots/{id}/tasks` 用自己记下的 run 状态编一段话。这段话不是模型写的。
- 回合中途追加的补充信息用 `desktop-bot-supplement-state` 回报已应用或错过，也不进模型正文。

**已经分开的表面**

| 表面 | 今天接到什么 | 和 desktop bot 回复的关系 |
| --- | --- | --- |
| 桌面 GUI 气泡 | `Reply.Text`、图片、文件、提问、交接链接 | 主表面。ack 只代表这个表面显示过 |
| 桌面 GUI 工作行 | 固定活动短语 | 同一次 run 的进度，没有消息 id，不能和气泡去重 |
| 定时任务报告 | 同一份回复字段，另一条事件 | 同一段正文可以再出现一次 |
| noVNC | 画面，不是消息 | 交接按钮由 `handoff` 带出，说明文字仍来自正文或 Hub 的固定句 |
| 托盘 | 用用户原话做任务名 | 不用模型正文 |
| 手机数字员工 | 完成时通知一次；进度补丁不通知 | 另一条产品路径，不订阅 desktop `Reply` |
| Hub IM 队列 | 入站平台（微信、Telegram 等）上的 `GenericResponse` | 服务 IM 回合，不服务 desktop bot 的 run |
| 邮件 | 登录和注册确认 | 不投递 bot 回复 |
| bot 之间的便条 | `reply_to` 把委托和结果配对 | 不给人看。被接纳之后会变成新的 run，那个 run 的助手正文再变成人看见的话 |

去重和确认只覆盖「这个 GUI 有没有把这个 run 画出来」：`client_message_id` 防重复开跑，settle 只做一次，ack 之后 pending 不再返回。没有按表面分开的确认，也没有一条消息 id 能同时代表进度和终态。

### 16.3 差距

| 原则 | 现在 |
| --- | --- |
| 用户只看见 `send_to_user` | 用户看见助手正文。模型把计划、解释、结果写在正文里，就会全部进气泡 |
| 干活工具不直接对人说话 | 工具结果先回模型，这点已经符合。但截图、交付文件、交接、提问是贴在助手消息上的副作用，不是一次明确的发送 |
| 正文不投递 | 正文就是投递。没有正文时 Hub 要么报错，要么替模型写一句交接说明 |
| 进度和终态是同一种消息的两种 | 进度是无 id 的短语，终态是整段正文。定时报告还能把终态再发一遍 |
| 一个路由器，按表面排版 | 排版写在 GUI（路径、任务名前缀、失败时把正文放进 error）。手机和邮件接不上 |
| 每个表面各自 ack | 只有桌面 GUI 的 run ack。第二个表面要么丢，要么重复 |
| 线程 | `client_message_id` 是用户这一次发送的幂等键。bot 之间的 `reply_to` 不用于人对 bot |

空跑也会暴露这个问题：模型干完了但忘了写收尾，run 被当成失败；模型写了一段自言自语，用户却收到整段。文件已经用 `deliver` 附上时，在线路径仍要求有正文，离线补投路径才会在没正文时用任务名凑一行。

### 16.4 `send_to_user`

工具只有模型能调用。参数：

| 字段 | 含义 |
| --- | --- |
| `text` | 给人看的正文。`final` 和 `ask` 必填。`progress` 可以空，空则只更新附件或让表面保持上一条进度 |
| `attachments` | `{id, name, mime}`。`id` 指向这次 run 里已经产生的截图、交付文件或生成图，不把文件字节放进工具参数 |
| `channel` | `auto`（默认）、`desktop`、`mobile`、`email`。预留 `web`、`slack`、`voice`，未实现的值拒绝 |
| `kind` | `progress`、`final`、`ask` |
| `end_turn` | `final` 和 `ask` 默认 true，`progress` 默认 false。true 表示对用户而言这一段说完了，模型还可以继续干活，但不再期待用户把后面的助手正文当成下一句 |
| `reply_to` | 这条话在回答哪一条。可以是用户消息 id，或上一条 `send_to_user` 的 id |
| `thread_id` | 可选。缺省是当前 run |

工具返回给模型的只有 `{id, accepted}`。不把即将展示的正文再回灌成一份「用户已经看到」。

约束：

- 一次调用产生一个 id。同一 `client_message_id`（或工具层幂等键）重复调用返回同一个 id，路由器不再扇出。
- `ask` 带着今天的提问语义：需要人回答才能继续。秘密输入仍由表面渲染成掩码框，秘密值不进 `text`，也不进进度。
- 交接、noVNC、键盘交还仍是桌面控制事件，不塞进 `text`。模型若要解释为什么把屏幕交给人，另发一条 `send_to_user`。Hub 不再在正文为空时替它写那句固定中文。
- 助手正文继续写入 run，供下一轮模型和审计使用。任何表面的渲染函数都不得读取它。
- run 结束时若没有 `final` 或 `ask`，不产生气泡，也不把正文补上。人问进度时仍走现有的任务状态接口，那段话由 Hub 编写，标明是状态而不是模型的话。
- 进度文本要过脱敏。现有活动词表继续作为「模型没写进度、只是工具在跑」时的系统进度，那种进度的作者是 runtime，不是模型，因此可以不经 `send_to_user`。模型自己的 `kind=progress` 才走这个工具。

### 16.5 Hub 路由器

路由器放在 Hub，不放在客户机里，也不放在 MaClawSrv 的回复抄写里。MaClawSrv 只在工具被调用时向 Hub 交一条出站消息。

一条出站记录：

```
id, tenant, user, bot, run, thread_id, reply_to,
kind, end_turn, channel, text, attachments, created
```

扇出：

- `channel=auto`：`final` 和 `ask` 送到这个人已启用的每个表面。`progress` 只送到前台表面：桌面 GUI 若正开着该 bot 页，就更新工作行；手机只做静默状态补丁（与现在数字员工「进度不通知、完成才通知」一致）；邮件不收进度。
- `channel` 指明一个表面时，只送那里，记录仍留在 Hub，便于别的表面以后来拉。
- 未启用的表面跳过，不算失败。

每个表面一个格式器，共享同一条记录：

| 表面 | 格式 |
| --- | --- |
| 桌面 GUI | 气泡用 `text`；附件下成本机文件；`progress` 只更新工作行；`ask` 继续用现有的提问框 |
| 手机 | `final` / `ask` 一条通知，正文截断；`progress` 只改任务状态，不弹通知 |
| 邮件 | 主题取 `text` 的第一行，正文为其余部分，附件用链接而不是 base64。只收 `final` 和 `ask` |
| 以后的网页、Slack、语音 | 同一条记录，各自的格式器。语音只读 `text`，不读附件字节 |

去重：出站 id 全局一次扇出。表面重连时按 id 拉未确认消息，不把同一 id 再通知一遍。进度在同一 `thread_id` 上可以合并，只保留最新一条未确认的进度。

确认：`POST` 确认携带 `message_id` 和 `surface`。某个表面的未确认 `final` / `ask` 就是这个表面的 pending。进度过期即可，不要求确认。今天的 run ack 改成「桌面 GUI 确认了这个 run 的最后一条 `final` 或 `ask`」。一个表面确认了，不代表另一个表面确认了。

顺序：同一 `thread_id` 按创建顺序。`end_turn=true` 之后，这个线程上还可以有新的 `progress`（模型还在干活），但下一条 `final` 是新的一段，用 `reply_to` 指回用户的下一句话或上一条终态。

失败：某个表面投递失败只重试那个表面，不让模型再调一次工具。重试用同一个 id。

### 16.6 迁移

仍不改代码，直到这一节被接受。顺序：

1. **影子。** 加上工具和 Hub 出站表。模型可以调用，路由器只记录。用户仍看见助手正文。用来对照「模型想说的」和「正文里实际出现的」差多少。
2. **有发送就不再抄正文。** 一个 run 里只要出现过 `send_to_user`，这个 run 的气泡、定时报告和 pending 都改读出站记录。没有调用过的 run 仍抄正文，避免一天内所有 bot 突然沉默。
3. **提示词。** 要求凡是要人看见的话都调用 `send_to_user`。活动短语继续由 runtime 写入工作行，不要求模型把「正在点击」再说一遍。
4. **关掉抄写。** 助手正文不再进入任何表面。没有 `final` 或 `ask` 的 run 不产生气泡。任务状态接口保持 Hub 自己的句子。交接不再自动补那句中文。
5. **把现有出口接到路由器。** pending、ack、定时报告、补投都改为按出站 id 和表面确认。`desktop-bot-activity` 仍可由系统进度驱动，与模型的 `progress` 共用工作行，后者覆盖前者直到下一步工具开始。
6. **加表面。** 先手机（完成才通知），再邮件（默认关闭，按人打开）。网页、Slack、语音只加格式器和订阅，不再从 `Message.Content` 取字。

回滚是把第 4 步的开关关掉，重新抄助手正文。出站记录保留，不删。

### 16.7 这一节不做什么

- 不把 shell、桌面、搜索、生图的返回值改成直接显示。
- 不把 bot 之间的便条改成 `send_to_user`。那是 agent 之间的通道。
- 不在这一步改 IM 入站队列的平台回复。IM 以后作为一个表面挂上路由器，而不是在这一期把微信回复改道。
- 不把 noVNC 当成一条消息通道。
