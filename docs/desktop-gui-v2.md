# Docker 桌面 maclaw-gui:2 使用与运维手册

`maclaw-gui:2` 是 desktopd 为每个 `tenant+user` 启动的新版桌面镜像:
完整 XFCE 桌面 + Chromium + fcitx5 中文拼音,纯 CPU 渲染,不需要 GPU。
Agent(CDP、xdotool、截图)和人(noVNC)用同一个桌面。

本文覆盖镜像能力、架构、构建、部署(Windows / Linux)、Hub 配置、
从 `maclaw-gui:1` 迁移、截图 API、验证、回滚、已知问题与安全建议。
desktopd 服务本身的说明见 [desktopd/README.md](../desktopd/README.md)。

![maclaw-gui:2 桌面](images/desktop-gui-v2.png)

## 1. 镜像提供什么

| 能力 | 说明 |
| --- | --- |
| 桌面 | Debian 12 + XFCE(面板、Thunar、xfce4-terminal、Mousepad),`startxfce4` 启动;没有 XFCE 的旧镜像自动回退 fluxbox |
| 浏览器 | Chromium,CDP 端口经 token gate 暴露;登录态写在每用户 profile 卷里 |
| 中文输入 | fcitx5 + 拼音,默认输入法列表 `keyboard-us` + `pinyin`,**Ctrl+Space** 切换;`GTK_IM_MODULE/QT_IM_MODULE/XMODIFIERS=fcitx`,`LANG=zh_CN.UTF-8` |
| 字体 | Noto Sans/Serif CJK SC(fontconfig 默认中文字体),emoji 字体 |
| 软件安装 | 容器内是 root,可以 `apt-get install`;装到 `/opt`、`/usr/local` 的东西在每用户卷里,重建容器也保留 |
| 分辨率 | 默认 `1440x900x24`;容器环境变量 `MACLAW_DESKTOP_GEOMETRY=1920x1080x24` 覆盖(格式 `宽x高[x色深]`,非法值回退默认) |
| 工具 | noVNC/websockify、x11vnc、xdotool、ImageMagick `import`、scrot、at-spi2(无障碍树) |

![fcitx5 拼音输入 "nihao shijie" 得到 "你好世界"](images/desktop-gui-v2-pinyin.png)

## 2. 架构

```
Hub / MaClawSrv ──HTTP Bearer DESKTOPD_TOKEN──> desktopd (127.0.0.1:18081,对外经 nginx 反代)
                                                   │ docker run / exec
                                                   ▼
                      容器 maclaw-desktop-<key>(--restart unless-stopped)
                      desktop_supervisor.py(每个 display 一套进程,pid 记在 pids.json)
                        ├─ Xvfb :20  (1440x900x24)
                        ├─ dbus-daemon  unix:path=/tmp/.maclaw-dbus-20   ← XFCE/fcitx5/Chromium 共用
                        ├─ startxfce4(xfwm4、xfce4-panel、xfdesktop…)
                        ├─ fcitx5 -d
                        ├─ Chromium  --remote-debugging-port=18000+display(只监听容器内)
                        ├─ x11vnc  localhost:5900  →  websockify 127.0.0.1:6081
                        ├─ gate 19020/tcp  → Chromium CDP(要求 Bearer gate token)
                        └─ gate 6080/tcp   → noVNC/websockify(要求 Bearer gate token)
```

- **端口**:容器只发布 `19020/tcp`(CDP gate)和 `6080/tcp`(noVNC gate),映射到宿主机随机端口;
  desktopd 用 `docker port` 查出端口,拼成 `cdp_url` / `novnc_url` 返回(主机名是
  `DESKTOPD_ADVERTISE_HOST`)。Chromium 调试端口、x11vnc、websockify 都只在容器内。
- **CDP / noVNC 鉴权**:每个容器一个随机 gate token,以 URL userinfo 形式放在
  `http://desktop:<token>@host:port` 里分发;客户端要转成 `Authorization: Bearer <token>`
  (MaClawSrv 的 CDP 客户端、Hub 的 noVNC 代理会自动做)。无 token 或 token 错误直接拒绝。
  Chromium 的 `/json` 拒绝 Host 为域名的请求,手动调试时用 IP 或 localhost 访问
  (`desktopd/scripts/cdp.py` 默认 `--host 127.0.0.1`)。
- **D-Bus 会话共享**:supervisor 为每个 display 启一个 `dbus-daemon --session`,地址固定为
  `unix:path=/tmp/.maclaw-dbus-<display>`,XFCE、fcitx5、Chromium 都用它。否则每个程序各自
  autolaunch 一个 bus,fcitx5 在 Chromium 里不可用。调试时:
  `docker exec -e DISPLAY=:20 -e DBUS_SESSION_BUS_ADDRESS=unix:path=/tmp/.maclaw-dbus-20 <容器> fcitx5-remote`
  (返回 2 = 拼音激活,1 = 英文)。
- **close_range 垫片**:Docker 20.10 + libseccomp < 2.5.2 的默认 seccomp 对 `close_range`
  返回 `EPERM`(而不是 `ENOSYS`),GLib ≥ 2.74 因此认为 spawn 失败,XFCE 组件、Thunar、
  终端都起不来。镜像里的 `close_range_shim.c` 编译成 `/usr/local/lib/...` 下的 so,并写进
  `/etc/ld.so.preload`,把 `EPERM` 改成 `ENOSYS`,GLib 回退到逐个 close 的路径。仅 x86_64;
  Docker ≥ 23 / libseccomp ≥ 2.5.2 的主机不需要,但留着无害。
- **残留 X 锁清理**:`docker stop` 超时后会 SIGKILL Xvfb,留下 `/tmp/.X20-lock` 与
  `/tmp/.X11-unix/X20`,下次启动 Xvfb 报 "Server is already active",而残留 socket 又让
  display 看起来已就绪。supervisor 的 `clear_stale_display()` 只在锁里的 pid 不是存活的
  Xvfb 时删除这两个文件。
- **每用户持久化**:4 个私有卷 `maclaw-desktops-<key>`(`/desktops`,profile 与日志)、
  `maclaw-home-<key>`(`/home/desktop`)、`maclaw-opt-<key>`(`/opt`)、
  `maclaw-local-<key>`(`/usr/local`)。容器层(apt 装的软件)在停止/迁移时 commit 成
  `maclaw-desktop-user-<key>:state`。

## 3. 构建镜像

构建上下文是 `desktopd/image`,Dockerfile 是 `Dockerfile.v2`。build args:

| 参数 | 默认 | 说明 |
| --- | --- | --- |
| `BASE_IMAGE` | `debian:bookworm` | 无法访问 Docker Hub 时换成镜像站 |
| `APT_MIRROR` | 空(deb.debian.org) | 只写主机名,如 `mirrors.tencentyun.com` |

```sh
# 能访问 Docker Hub
docker build -f desktopd/image/Dockerfile.v2 -t maclaw-gui:2 desktopd/image

# 腾讯云(生产主机,无 Docker Hub)
docker build -f desktopd/image/Dockerfile.v2 \
  --build-arg BASE_IMAGE=mirror.ccs.tencentyun.com/library/debian:bookworm \
  --build-arg APT_MIRROR=mirrors.tencentyun.com \
  -t maclaw-gui:2 desktopd/image
```

部署脚本(`desktopd/remote_deploy.sh`)会在主机上先构建到临时 tag,检查镜像契约后才打
`maclaw-gui:2`,失败不覆盖旧镜像;能访问腾讯云元数据服务时自动用上面的腾讯镜像源。
覆盖变量:`DESKTOPD_BASE_IMAGE`、`DESKTOPD_APT_MIRROR`(`none` = deb.debian.org)、
`DESKTOPD_SKIP_IMAGE_BUILD=1`。不要删除或重新打 tag `maclaw-gui:1`,迁移和回滚都依赖它。

## 4. 部署

一台 Docker 主机上同时跑 desktopd、Hub、MaClawSrv 时,按顺序部署:
**备份 → desktopd(含镜像构建)→ Hub → MaClawSrv → 验证 → 冒烟 → 切换 Hub 镜像**。
在 Hub 配置切换之前,已有用户继续用 `maclaw-gui:1`,不会被迁移。

### 4.1 Windows(原有脚本)

```bat
deploy_desktopd.cmd      :: 编译 desktopd、打包 image/,上传并执行 desktopd/remote_deploy.sh
deploy_all.cmd           :: Hub(deploy/deploy_all_ha.ps1 deploy_hub)
deploy_maclawsrv.cmd     :: MaClawSrv
```

### 4.2 Linux / macOS(`deploy/linux/rollout_desktop_gui2.sh`)

本地脚本负责交叉编译、打包、断点续传上传;主机端步骤在 `deploy/linux/rollout_remote.sh`,
工具脚本在 `desktopd/scripts/`(一起上传到 `$REMOTE_TMP/scripts`)。依赖:go、git、xz、
rsync、ssh(用密码登录时再加 `sshpass`)。

```sh
export REMOTE_HOST=hubs.example.com          # 必填
export GOTOOLCHAIN=go1.26.5                  # 可选:与生产二进制一致
# 用密码登录时只通过环境变量传:read -rs SSHPASS; export SSHPASS(不要写进文件/历史)

deploy/linux/rollout_desktop_gui2.sh build upload     # 编译 + 上传(rsync --partial,断线自动续传)
deploy/linux/rollout_desktop_gui2.sh backup           # 记下输出里的时间戳 <ts>
deploy/linux/rollout_desktop_gui2.sh deploy-desktopd  # 主机上后台执行,轮询日志
deploy/linux/rollout_desktop_gui2.sh deploy-hub deploy-maclawsrv
HUB_PUBLIC_URL=https://hub.example.com deploy/linux/rollout_desktop_gui2.sh verify
deploy/linux/rollout_desktop_gui2.sh smoke            # 测试用户冒烟(见第 8 节)
CONFIRM=1 deploy/linux/rollout_desktop_gui2.sh switch-image   # 改 Hub 配置为 maclaw-gui:2
```

`all` = `build upload backup deploy-desktopd deploy-hub deploy-maclawsrv verify`(不含切换)。
`ONLY="desktopd"` 只编译/上传部分组件。常用变量:

| 变量 | 默认 | 说明 |
| --- | --- | --- |
| `REMOTE_HOST` / `REMOTE_USER` / `REMOTE_PORT` | — / `root` / `22` | SSH 目标;复用 ControlMaster 连接,连接被重置(exit 255)自动重试 |
| `REMOTE_TMP` | `/tmp/maclaw_gui2_rollout` | 主机上的上传目录 |
| `DESKTOPD_ADVERTISE_HOST` | `REMOTE_HOST` | 仅首次部署写入 `.env`;已有值保留 |
| `DESKTOPD_BASE_IMAGE` / `DESKTOPD_APT_MIRROR` / `DESKTOPD_SKIP_IMAGE_BUILD` | 自动 | 同第 3 节 |
| `DESKTOP_IMAGE` / `DESKTOP_MEMORY` / `DESKTOP_CPUS` / `DESKTOP_SHM` | `maclaw-gui:2` / `3g` / `1.5` / `1g` | `switch-image` 写入 Hub 的值 |
| `DESKTOP_SERVER_ID` | 空 = 全部服务 | 只切换某一个 Docker 服务 |
| `BACKUP_TS` | 上次 backup 的时间戳 | `rollback` 用 |

主机端目录默认 `/data/soft/maclaw_desktopd`、`/data/soft/hub`、`/data/soft/maclaw_srv`,
可用 `DESKTOPD_DEPLOY_DIR`、`HUB_DIR`、`MACLAWSRV_DEPLOY_DIR` 覆盖(直接在主机上运行
`rollout_remote.sh` 时)。各步骤说明:

- `backup`:`<目录>.bak.<ts>`(desktopd 的 bin/image/.env/state 和容器里的 supervisor,
  Hub 的二进制/start.sh/configs/web 和桌面服务配置,MaClawSrv 的二进制/.env/start.sh/unit),
  以及 `/data/soft/backups/gui2-rollout-<ts>/`(服务状态、容器/镜像/卷列表、inspect、md5)。
  目录权限 700,文件 600(含 `.env` 副本)。
- `deploy-desktopd`:调用未改动的 `desktopd/remote_deploy.sh`(与 Windows 相同)。
- `deploy-hub`:替换二进制、`meeting_asr_worker`、`start.sh`、`config.example.yaml`,web 目录先
  在旁边暂存并用 sha256 清单校验再整体替换;**保留 `configs/config.yaml`**;`./start.sh` 重启后
  轮询 `/healthz`。
- `deploy-maclawsrv`:要求主机上已有 `.env`(只补缺失键,不覆盖);写 `start.sh` 与 systemd unit
  (示例见 `deploy/linux/systemd/`),重启后打印 `/health`、`/version`。

nginx 反代示例:`desktopd/deploy/nginx-dockerd.conf.example`(只反代 desktopd API,
不暴露容器端口)。

## 5. Hub 配置

Hub 把 Docker 桌面服务存在 `system_settings` 表的 `desktop_service` 键(多租户为
`tenant:<id>:desktop_service`),每次请求都读,改完立即生效、不用重启。结构示例:
`desktopd/deploy/desktop_service.settings.example.json`。

| 字段 | 推荐值 | 说明 |
| --- | --- | --- |
| `base_url` | `https://dockerd.example.com` | desktopd 地址 |
| `access_token` | desktopd 的 `DESKTOPD_TOKEN` 或面板里的额外 key | 不会在 API 返回里回显 |
| `image` | `maclaw-gui:2` | 改这个字段触发迁移(见第 6 节) |
| `memory` / `cpus` / `shm_size` | `3g` / `1.5` / `1g` | XFCE + Chromium 低于 2g 容易 OOM |

修改方式(三选一):

1. Hub 管理页 → "桌面服务" → 编辑服务。
2. Admin API(租户管理员登录态):
   ```sh
   curl -X PATCH https://hub.example.com/api/admin/desktop-services/<server_id> \
     -H "Authorization: Bearer <admin-session-token>" -H 'Content-Type: application/json' \
     -d '{"name":"docker-desktop-1","base_url":"https://dockerd.example.com","image":"maclaw-gui:2","memory":"3g","cpus":"1.5","shm_size":"1g"}'
   ```
   不带 `access_token` 字段时保留原 token。
3. 在 Hub 主机上直接改库(rollout 用的方式,先备份):
   ```sh
   export HUB_DB=/data/soft/hub/data/codeclaw-hub.db
   python3 desktopd/scripts/hub_desktop_settings.py show                 # token 已打码
   python3 desktopd/scripts/hub_desktop_settings.py backup /root/desktop_service.json
   python3 desktopd/scripts/hub_desktop_settings.py set --image maclaw-gui:2 --memory 3g --cpus 1.5 --shm 1g --dry-run
   python3 desktopd/scripts/hub_desktop_settings.py set --image maclaw-gui:2 --memory 3g --cpus 1.5 --shm 1g
   python3 desktopd/scripts/hub_desktop_settings.py restore /root/desktop_service.json   # 撤销
   ```
   `--server-id` 只改一个服务,`--tenant <id>` 改租户级配置。

Hub 的 `/api/v1/desktop-services/*`(MaClawSrv 远程调用、截图)需要 Hub 进程环境变量
`MACLAW_DESKTOP_API_TOKEN`,MaClawSrv 的 `.env` 里配同一个值和 `MACLAW_HUB_URL`;未设置时
这些接口一律返回 401。

## 6. 从 maclaw-gui:1 迁移

- **懒迁移**:改 Hub 配置不会重建任何容器。只有用户下次**打开桌面**(Hub 管理页创建/查看、
  机器人消息触发桌面、`/api/v1/desktop-services/session`)时,desktopd 发现容器标签
  `maclaw.image`(旧容器没有标签,视为 `maclaw-gui:1`)与请求镜像不同,才重建这一个容器。
  内存/shm 变化或私有卷未挂载同样触发重建。
- **重建过程**:Chromium 落盘登录态 → `docker stop` → 容器层 commit 为
  `maclaw-desktop-user-<key>:state`(带旧 `maclaw.image` 标签)→ 删除容器 → 用新镜像重建,
  4 个私有卷原样挂回。
- **`:state` 与 `:prev`**:`:state` 只在标签与新镜像一致时作为启动镜像;不一致时改名为
  `maclaw-desktop-user-<key>:prev`(只保留一代),用户从干净的 `maclaw-gui:2` 开始。
- **保留**:浏览器登录态与 profile、`/home/desktop`、`/opt`、`/usr/local`、`/desktops`。
- **不保留**:apt 装进旧镜像层的软件(在 `:prev` 里,需要时手动找回或重新安装)、
  旧桌面(fluxbox)的窗口布局、`/tmp`。
- 比较的是镜像**名**:原地重建 `maclaw-gui:2` 不会让大家重建;要强制全员换新镜像,用新 tag
  (如 `maclaw-gui:3`)并改 Hub 配置。

查看迁移状态:

```sh
docker ps -a --filter name=maclaw-desktop- --format '{{.Names}}\t{{.Image}}\t{{.Label "maclaw.image"}}\t{{.Status}}'
docker images 'maclaw-desktop-user-*'
journalctl -u maclaw-desktopd | grep -E 'recreating|keeping it as'
```

## 7. 截图 API

三层接口都返回整个桌面的 PNG(只截已运行的桌面,不会因此启动容器)。

**desktopd**(Bearer `DESKTOPD_TOKEN`,直接返回 `image/png`):

```sh
curl -fsS -H "Authorization: Bearer $DESKTOPD_TOKEN" \
  "https://dockerd.example.com/v1/desktops/screenshot?tenant_id=tenant_default&user_id=<user_id>" -o desk.png
curl -fsS -X POST -H "Authorization: Bearer $DESKTOPD_TOKEN" -H 'Content-Type: application/json' \
  -d '{"tenant_id":"tenant_default","user_id":"<user_id>","display":":20"}' \
  https://dockerd.example.com/v1/desktops/screenshot -o desk.png
# 在 desktopd 主机上(从 .env 读 token,不打印):
desktopd/scripts/dapi.sh GET '/v1/desktops/screenshot?tenant_id=tenant_default&user_id=<user_id>' > desk.png
```

**Hub**(Bearer `MACLAW_DESKTOP_API_TOKEN`,返回 JSON):

```sh
curl -fsS -X POST -H "Authorization: Bearer $MACLAW_DESKTOP_API_TOKEN" -H 'Content-Type: application/json' \
  -d '{"tenant_id":"tenant_default","user_id":"<user_id>"}' \
  https://hub.example.com/api/v1/desktop-services/screenshot | python3 -c \
  'import sys,json,base64; d=json.load(sys.stdin); open("desk.png","wb").write(base64.b64decode(d["image_base64"])); print(d["mime"], d["bytes"])'
```

**Agent**:MaClawSrv 的 `desktop` 工具 `{"action":"screenshot"}`。配置了 `MACLAW_HUB_URL` +
`MACLAW_DESKTOP_API_TOKEN` 时走 Hub,否则截本机 `maclaw-gui` 容器;图片附给支持视觉的模型
(过大时转 JPEG),对话历史里只有尺寸说明,像素坐标与 `app_run` 点击坐标一致。

## 8. 验证与冒烟

部署后(`rollout_desktop_gui2.sh verify` 会做前 3 项):

1. `systemctl is-active maclaw-desktopd maclawsrv`;`desktopd/scripts/dapi.sh GET /v1/health`;
   `curl 127.0.0.1:9399/healthz`;`curl 127.0.0.1:18080/health`。
2. `docker image inspect maclaw-gui:2`,`maclaw-gui:1` 仍存在。
3. 容器列表里已有用户仍是 `maclaw-gui:1`(切换前)。
4. 测试用户冒烟(只动测试用户的容器、卷、镜像):
   ```sh
   # 主机上,root
   TEST_USER=rollout-test-gui2 MIGRATE=1 STOP_AFTER=1 sh desktopd/scripts/smoke_gui2.sh
   # 只测新镜像
   TEST_USER=rollout-test-gui2b MIGRATE=0 sh desktopd/scripts/smoke_gui2.sh
   ```
   覆盖:v1 建桌面并在 4 个卷写标记 → v2 打开 → 标签、`:prev`、卷与标记保留、资源;
   desktopd 截图 PNG;XFCE/fcitx5/Xvfb/x11vnc 进程;CDP 经 gate 可用、无 token 被拒;CJK 字体;
   在 Chromium 里用拼音输入 "nihao shijie" 得到 "你好世界";desktopd 日志无错误。
   输出(截图、session.json 不保留)在 `OUT_DIR=/tmp/maclaw-gui2-smoke`,失败时退出码非 0。
5. 手动 CDP:`dapi.sh POST /v1/desktops/session '{"tenant_id":"tenant_default","user_id":"rollout-test-gui2"}' > s.json`,
   然后 `python3 desktopd/scripts/cdp.py --session s.json version|list|eval '<js>'|navigate '<url>'`
   (`s.json` 含 gate token,用完删除)。
6. 人工:Hub 管理页"桌面服务"打开测试用户的 noVNC,确认 XFCE 面板、Ctrl+Space 切拼音、终端可 `apt-get install`。

清理测试用户(确认名字后再执行):

```sh
desktopd/scripts/dapi.sh POST /v1/desktops/stop '{"tenant_id":"tenant_default","user_id":"rollout-test-gui2"}'
docker ps -a --filter label=maclaw.user=rollout-test-gui2 -q    # 找到容器后 docker rm,
# 再按容器名里的 <key> 删除 maclaw-{desktops,home,opt,local}-<key> 卷和 maclaw-desktop-user-<key>:* 镜像
```

## 9. 回滚

1. **先回滚 Hub 配置**(阻止更多用户迁移):
   `python3 desktopd/scripts/hub_desktop_settings.py restore /data/soft/hub.bak.<ts>/desktop_service.settings.json`,
   或 `CONFIRM=1 DESKTOP_IMAGE=maclaw-gui:1 DESKTOP_MEMORY=2500m DESKTOP_SHM=512m rollout_desktop_gui2.sh switch-image`。
2. **二进制**:`BACKUP_TS=<ts> deploy/linux/rollout_desktop_gui2.sh rollback`(按顺序恢复 Hub 配置、
   desktopd 二进制与 image 目录、Hub 二进制/worker/start.sh/web、MaClawSrv 二进制,并重启)。
   Windows 部署的环境同样可以手动从 `<目录>.bak.<ts>` 复制回去。
3. **已迁移到 v2 的用户**:Hub 配置回到 `maclaw-gui:1` 后,下次打开会再次按标签重建为 v1
   (卷保留)。自动重建时 v2 的容器层会被改名为 `:prev`,**覆盖**迁移前 v1 的 `:prev`。
   要找回迁移前 v1 容器层(apt 装的软件),在用户重新打开桌面**之前**执行:
   ```sh
   docker image inspect --format '{{index .Config.Labels "maclaw.image"}}' maclaw-desktop-user-<key>:prev  # 应为 maclaw-gui:1 或空
   docker stop maclaw-desktop-<key> && docker rm maclaw-desktop-<key>     # 命名卷不受影响
   docker tag maclaw-desktop-user-<key>:prev maclaw-desktop-user-<key>:state
   ```
   之后用户打开桌面时 desktopd 直接用这个 `:state` 建 v1 容器(标签一致)。
   注意 v2 期间 apt 装进容器层的软件因此丢弃;卷里的数据都在。
4. `maclaw-gui:1` 镜像和用户卷不要删除;回滚不需要重建镜像。

## 10. 已知问题

- **桌面以 root 运行**:Chromium 带 `--no-sandbox`(页面顶部有提示条),依赖容器隔离;容器内用户
  可以 apt 安装软件,也就能改动自己容器里的一切。
- **close_range 垫片仅 x86_64**:ARM 主机构建会跳过/失败,需要 Docker ≥ 23 或 libseccomp ≥ 2.5.2。
- **Ubuntu 18.04 / Docker 20.10 主机**:默认 seccomp 拦 `close_range`(见垫片);systemd 237 不支持
  `StandardOutput=append:`,日志看 `journalctl -u maclaw-desktopd` / `-u maclawsrv`;sqlite 3.22 没有
  UPSERT,`hub_desktop_settings.py` 用 UPDATE/INSERT。
- **上行慢的部署机**:二进制压缩后仍有几十 MB。用 `rollout_desktop_gui2.sh upload`(rsync `--partial`
  断点续传)或在离主机近的机器上编译;镜像在主机上构建,不要上传镜像。
- **Hub 未设 `MACLAW_DESKTOP_API_TOKEN`**:`/api/v1/desktop-services/*` 全部 401,MaClawSrv 远程截图
  和远程桌面会失败;Hub 管理页不受影响。
- 截图只截已在运行的 display;桌面没开时返回错误而不是启动桌面。
- 重建会断开该用户当前的 noVNC/CDP 连接;迁移发生在打开桌面时,首次约 10–30 秒。

## 11. 安全建议

- `DESKTOPD_TOKEN`、`MACLAW_DESKTOP_API_TOKEN`、gate token(`cdp_url`/`novnc_url` 里的 userinfo)都是
  凭据:不要贴进工单/聊天,日志里用 `sed -E 's/desktop:[0-9a-f]{16,}@/desktop:<redacted>@/g'` 打码。
  `.env` 权限 600,备份目录 700。
- desktopd 只监听 127.0.0.1,公网只经 nginx(HTTPS)暴露 `/v1/*`;容器发布的随机端口建议用云安全组
  /防火墙只放行 Hub、MaClawSrv 所在地址(gate 已强制 token,这是纵深防御)。
- SSH:改为密钥登录并禁用 root 密码登录,配合 fail2ban 或安全组限制来源(生产 sshd 长期被扫描,
  表现为握手被重置)。部署脚本只从 `SSHPASS` 环境变量读密码,不要写进文件或命令历史。
- 定期轮换 `DESKTOPD_TOKEN`(管理面板可先加新 key、改 Hub、再删旧 key)。
- 不要在共享主机上把 `maclaw-desktop-user-*:state/:prev` 镜像推到任何仓库:里面有用户的数据。

## 12. 相关文件

| 路径 | 用途 |
| --- | --- |
| `desktopd/image/Dockerfile.v2` | maclaw-gui:2 镜像 |
| `desktopd/image/desktop_supervisor.py` | 容器内 supervisor(XFCE/fcitx5/D-Bus/gate) |
| `desktopd/image/close_range_shim.c` | close_range EPERM→ENOSYS 垫片 |
| `desktopd/image/fcitx5-profile` | fcitx5 默认输入法(keyboard-us + pinyin) |
| `desktopd/remote_deploy.sh` | 主机端 desktopd 部署(Windows/Linux 共用) |
| `desktopd/deploy/nginx-dockerd.conf.example` | desktopd 的 nginx 反代示例 |
| `desktopd/deploy/desktop_service.settings.example.json` | Hub `desktop_service` 配置示例 |
| `desktopd/scripts/dapi.sh` | 主机上调用 desktopd API(从 .env 读 token) |
| `desktopd/scripts/cdp.py` | 通过 gate 调 CDP(version/list/eval/navigate) |
| `desktopd/scripts/hub_desktop_settings.py` | 查看/备份/恢复/修改 Hub 桌面服务配置 |
| `desktopd/scripts/smoke_gui2.sh` | 测试用户端到端冒烟(迁移、截图、拼音、CDP) |
| `deploy/linux/rollout_desktop_gui2.sh` | Linux 本地:编译、打包、上传、逐步执行 |
| `deploy/linux/rollout_remote.sh` | 主机端:备份、部署、验证、切换、回滚 |
| `deploy/linux/systemd/*.service.example` | maclaw-desktopd / maclawsrv systemd unit |
