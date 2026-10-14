# Docker 桌面 maclaw-gui:2 使用与运维手册

`maclaw-gui:2` 是 desktopd 为每个 `tenant+user` 启动的新版桌面镜像:
完整 XFCE 桌面 + Chromium + fcitx5 中文拼音,纯 CPU 渲染,不需要 GPU。
Agent(CDP、xdotool、截图)和人(noVNC)用同一个桌面。

本文覆盖镜像能力、架构、构建、部署(Windows / Linux)、Hub 配置、
从 `maclaw-gui:1` 迁移、截图 API、验证、回滚、已知问题与安全建议。
desktopd 服务本身的说明见 [desktopd/README.md](../desktopd/README.md)。

![maclaw-gui:2 桌面](images/desktop-gui-v2.png)

## 公开镜像(快速开始)

`maclaw-gui:2` 有公开的预构建镜像,**匿名即可拉取**,不需要登录 GHCR:

| 镜像 | 说明 |
| --- | --- |
| `ghcr.io/rapidai/maclaw-gui:2` | 当前 v2,随 main 上 `desktopd/image/**` 的改动更新 |
| `ghcr.io/rapidai/maclaw-gui:2-<短sha>` | 固定到某次提交(如 `2-cc24156`),不会再变 |
| `ghcr.io/rapidai/maclaw-gui:latest` | 与 main 上最新的 `2` 相同 |
| `ghcr.io/rapidai/maclaw-gui@sha256:<digest>` | 按内容固定,docker 拉取时校验 |

由 `.github/workflows/desktop-image.yml` 构建(`debian:bookworm` + deb.debian.org,仅 linux/amd64),
推送前先过镜像契约检查和镜像文件系统的密钥扫描。包页面:
<https://github.com/orgs/RapidAI/packages/container/package/maclaw-gui>。
首个公开版本:`2-cc24156` = `sha256:4078a47c8712c61486702e7c5623a77166b9551b1fe31407809bff936afbfed6`
(压缩后约 783MB,解压约 1.85GB)。

```sh
docker pull ghcr.io/rapidai/maclaw-gui:2
docker tag ghcr.io/rapidai/maclaw-gui:2 maclaw-gui:2      # desktopd 和 Hub 配置用的本地名
# 固定 digest(生产推荐;digest 见包页面或 docker inspect --format '{{index .RepoDigests 0}}')
docker pull ghcr.io/rapidai/maclaw-gui@sha256:4078a47c8712c61486702e7c5623a77166b9551b1fe31407809bff936afbfed6
```

验证匿名拉取(不需要任何凭据,返回 `HTTP 200` 和 `docker-content-digest`):

```sh
T=$(curl -s "https://ghcr.io/token?scope=repository:rapidai/maclaw-gui:pull" | python3 -c 'import sys,json;print(json.load(sys.stdin)["token"])')
curl -sI -H "Authorization: Bearer $T" \
  -H 'Accept: application/vnd.docker.distribution.manifest.v2+json,application/vnd.oci.image.index.v1+json' \
  https://ghcr.io/v2/rapidai/maclaw-gui/manifests/2 | grep -Ei '^HTTP|docker-content-digest'
```

**desktopd 自动拉取**:desktopd 发现本机没有 `maclaw-gui:2` 时(启动时在后台检查一次,打开桌面时
按需再检查),会拉取 `DESKTOPD_IMAGE_SOURCE`(默认 `ghcr.io/rapidai/maclaw-gui:2`),跑一遍镜像契约检查,
通过后再打成本地 tag `maclaw-gui:2`。本机**已有**这个镜像时什么都不做:不会重新拉取,也不会自动替换。
关闭:`DESKTOPD_IMAGE_SOURCE=off`。细节见 [3.1](#31-desktopd-自动拉取与部署脚本的镜像来源)。

**中国大陆**:从 ghcr.io 拉取可能很慢甚至超时。可以:

- 继续在主机上构建(生产做法,腾讯云自动使用腾讯镜像源,见第 3 节);
- 把 `DESKTOPD_IMAGE_SOURCE` 指向可访问的 ghcr.io 代理/镜像仓库(例如自建的 registry,内容按 digest
  固定以防被替换),如 `DESKTOPD_IMAGE_SOURCE=registry.example.cn/rapidai/maclaw-gui@sha256:<digest>`;
- 在海外机器上 `docker pull` 后 `docker save ghcr.io/rapidai/maclaw-gui:2 | zstd > gui2.tar.zst`,传到主机
  `zstd -dc gui2.tar.zst | docker load`,再 `docker tag` 成 `maclaw-gui:2`。

## 1. 镜像提供什么

| 能力 | 说明 |
| --- | --- |
| 桌面 | Debian 12 + XFCE(面板、Thunar、xfce4-terminal、Mousepad),`startxfce4` 启动;没有 XFCE 的旧镜像自动回退 fluxbox |
| 浏览器 | Chromium,CDP 端口经 token gate 暴露;登录态写在每用户 profile 卷里 |
| 共用浏览器 | 面板/底部 dock 的“网络浏览器”、应用菜单、`xdg-open`/`exo-open`、`x-www-browser` 都进入**同一个**受监管 Chromium(同 profile、同登录态、带 CDP):有窗口就恢复并激活(含最小化窗口),带链接就作为新标签打开;浏览器被关掉/崩溃时只重启浏览器(恢复 CDP 与登录态),不会再起第二套桌面。实现见 `desktop_supervisor.py browser` 与 `/usr/bin/maclaw-browser`、`/etc/chromium.d/zz-maclaw-shared-browser` |
| 中文输入 | fcitx5 + 拼音,默认输入法列表 `keyboard-us` + `pinyin`,**Ctrl+Space** 切换;`GTK_IM_MODULE/QT_IM_MODULE/XMODIFIERS=fcitx`,`LANG=zh_CN.UTF-8` |
| 字体 | Noto Sans/Serif CJK SC(fontconfig 默认中文字体),emoji 字体 |
| 软件安装 | 容器内是 root,可以 `apt-get install`;装到 `/opt`、`/usr/local` 的东西在每用户卷里,重建容器也保留。执行阶段的 desktop `action=install` 用包名调用 `apt-get update` 和 `apt-get install -y`，不经过 shell。安装前先等这次打开桌面时后台的 apt 源测速；若安装更早拿到锁，就由安装自己测完并写源。上次安装若被时间预算打断，这一次会先等 dpkg 的锁（dpkg 自己不等），再 `dpkg --configure -a`。拆到一半、configure 配不完的包，在源更新成功后用不带包名的 `apt-get -f install` 补完，再安装这次要的包。配置失败而源更新也失败时，带回配置这一步的错误。失败时只带回 apt 日志末尾。apt 在 20 分钟停下并说明是超时，不把下载进度当成失败原因；送回这句的客户端和写截止再多等一分钟。计划阶段只把包名写进安排 |
| apt 源 | 镜像只带 `deb.debian.org`(构建用的 `APT_MIRROR` 不进成品)。每次打开桌面,supervisor 在后台测 `deb.debian.org` 与 `mirrors.tencent.com`(直连与经 `HTTP(S)_PROXY` 各一次),把更快的写进 `/etc/apt/sources.list.d/debian.sources`,有出口代理时写 `/etc/apt/apt.conf.d/90maclaw-proxy`(选中的源若直连更快则对它 `DIRECT`),并让 sudo 保留代理变量(`/etc/sudoers.d/maclaw-proxy-env`);结果在 `/var/lib/maclaw/apt-mirror.json`,24 小时或代理变化后重测。容器环境变量 `MACLAW_APT_MIRROR` 覆盖:主机名/URL(多个用逗号分隔则在其中测速)、`off` 保留自己的源。手动重测:`python3 /desktop_supervisor.py apt-mirror`。用户自己改成其他镜像站的 debian.sources 不会被覆盖 |
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
| `APT_MIRROR` | 空(deb.debian.org) | 只写主机名,如 `mirrors.tencentyun.com`;只用于构建,成品镜像的源恢复为 deb.debian.org(内网源在其他主机和出口代理后不可达) |

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
`DESKTOPD_SKIP_IMAGE_BUILD=1`;不想构建时用公开镜像(`DESKTOPD_IMAGE_FROM=pull|auto`,见 3.1)。不要删除或重新打 tag `maclaw-gui:1`,迁移和回滚都依赖它。

### 3.1 desktopd 自动拉取与部署脚本的镜像来源

**desktopd 运行时**(`desktopd/image_source.go`):

| 变量 | 默认 | 说明 |
| --- | --- | --- |
| `DESKTOPD_IMAGE_SOURCE` | `ghcr.io/rapidai/maclaw-gui:2` | 本地 `maclaw-gui:2` 缺失时从哪里拉。可写 `@sha256:` digest;`off`/`none` 关闭;多个镜像用 `本地名=引用,...`,如 `maclaw-gui:2=ghcr.io/rapidai/maclaw-gui:2-cc24156` |
| `DESKTOPD_IMAGE_PULL_TIMEOUT` | `30m` | 单次拉取超时(Go duration,如 `45m`) |

- 启动时在后台检查(不阻塞 `/v1/health` 和管理面板);打开桌面(含迁移重建前的“新镜像必须存在”检查)
  时按需检查。同一镜像同时只有一个拉取,其他请求等待同一个结果;调用方超时(Hub 约 2 分钟)不会
  中断拉取,下次请求直接用拉好的镜像。
- 顺序:`docker pull <source>` → 契约检查(与 `remote_deploy.sh` 相同的工具清单)→ 再确认本地仍没有
  `maclaw-gui:2`(期间有人构建了就保留本地的)→ `docker tag <镜像ID> maclaw-gui:2`。
- 失败(网络不通、超时、契约不通过):不打 tag、不碰现有容器,迁移不会开始,用户的旧桌面保持原样;
  1 分钟内不重复拉取;日志提示改为在主机上构建。配置了来源的镜像不会再退回 `docker pull maclaw-gui:2`
  (那会去 Docker Hub)。没有配置来源的其他镜像名保持原来的直接 `docker pull`。
- 管理面板“镜像来源”一行显示状态:本地已有或未触发 / 拉取中 / 已拉取 / 拉取失败(含原因)。
- 日志关键字:`image maclaw-gui:2 is missing; pulling`、`ready from`、`failed after`、`is present; not pulling`。

**部署脚本**(`remote_deploy.sh`、`deploy_desktopd.cmd`、`deploy/linux/rollout_*`):

| 变量 | 默认 | 说明 |
| --- | --- | --- |
| `DESKTOPD_IMAGE_FROM` | `build` | `build`:主机上 `docker build`(生产默认,不变);`pull`:拉 `DESKTOPD_IMAGE_SOURCE`、契约检查、打 tag,失败则部署失败且旧镜像不变;`auto`:先拉(受超时限制),失败再构建 |
| `DESKTOPD_IMAGE_SOURCE` | `ghcr.io/rapidai/maclaw-gui:2` | 同上;显式传入时同时写进主机 `.env`,desktopd 运行时也用它 |
| `DESKTOPD_IMAGE_PULL_TIMEOUT` | `30m` | 脚本里用 coreutils `timeout`(`30m`、`1800s`);显式传入时写进 `.env` |
| `DESKTOPD_SKIP_IMAGE_BUILD` | `0` | `1` = 不构建也不拉取,要求镜像已存在 |

```sh
# 海外/能访问 ghcr 的主机:直接用公开镜像,按 digest 固定
DESKTOPD_IMAGE_FROM=pull \
DESKTOPD_IMAGE_SOURCE=ghcr.io/rapidai/maclaw-gui@sha256:4078a47c8712c61486702e7c5623a77166b9551b1fe31407809bff936afbfed6 \
  deploy/linux/rollout_desktop_gui2.sh deploy-desktopd
# 先试拉 10 分钟,不行再构建
DESKTOPD_IMAGE_FROM=auto DESKTOPD_IMAGE_PULL_TIMEOUT=10m deploy/linux/rollout_desktop_gui2.sh deploy-desktopd
```

```bat
:: Windows
set DESKTOPD_IMAGE_FROM=pull
deploy_desktopd.cmd docker-host.example
```

与构建一样,部署脚本的 `pull`/`auto` 是运维显式操作,会把 `maclaw-gui:2` 指向新拉的镜像;desktopd
运行时只补缺失的镜像,从不替换。desktopd 比较的是镜像**名**:`maclaw-gui:2` 指向新内容后,已有用户
不会因此重建,只有新建/重建的容器用新内容;要让所有人换新版,用新的镜像名并修改 Hub 配置(第 6 节)。

## 4. 部署

一台 Docker 主机上同时跑 desktopd、Hub、MaClawSrv 时,按顺序部署:
**备份 → desktopd(含镜像构建)→ Hub → MaClawSrv → 验证 → 冒烟 → 切换 Hub 镜像**。
在 Hub 配置切换之前,已有用户继续用 `maclaw-gui:1`,不会被迁移。

`hub.mypapers.top` 上已经在跑的桌面不要走 4.1 的脚本。同一桌面逻辑只在三个 Go 二进制里时,按第 4.4 节替换二进制。

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
| `DESKTOPD_IMAGE_FROM` / `DESKTOPD_IMAGE_SOURCE` / `DESKTOPD_IMAGE_PULL_TIMEOUT` | `build` / ghcr / `30m` | 镜像来源,见 3.1 |
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

### 4.3 新主机只部署 desktopd(能访问 ghcr.io)

已验证于 Ubuntu 24.04 + Docker 29(主机上已有 nginx 与通配证书,另有 Hub 在跑也不受影响):

```sh
export REMOTE_HOST=hubs.example.com GOTOOLCHAIN=go1.26.5 ONLY=desktopd
deploy/linux/rollout_desktop_gui2.sh build upload
DESKTOPD_ADVERTISE_HOST=dockerd.example.com DESKTOPD_IMAGE_FROM=pull \
  deploy/linux/rollout_desktop_gui2.sh deploy-desktopd     # 镜像解压后约 2.7GB,境外主机约 2 分钟
```

- 首次部署在 `/data/soft/maclaw_desktopd/.env`(600)生成新的 `DESKTOPD_TOKEN`,不要复用其他实例的。
- `DESKTOPD_ADVERTISE_HOST` 要填对外域名:它出现在返回给 Hub 的 `cdp_url`/`novnc_url` 里。
- nginx:按 `desktopd/deploy/nginx-dockerd.conf.example` 改 `server_name` 和证书路径,放进
  `/etc/nginx/conf.d/`,`nginx -t && systemctl reload nginx`;不带 token 访问
  `https://dockerd.example.com/v1/health` 应返回 401。
- 冒烟:`TEST_USER=deploy-test MIGRATE=0 DESKTOPD_URL=https://dockerd.example.com sh $REMOTE_TMP/scripts/smoke_gui2.sh`
  (新主机没有 maclaw-gui:1,必须 `MIGRATE=0`),结束后用 `dapi.sh POST /v1/desktops/stop` 停掉测试桌面。
- 防火墙:每个桌面的 CDP/noVNC gate 端口由 Docker 发布在随机高位端口(如 32768+),Docker 的
  iptables 规则绕过 ufw,所以 ufw 不放行也能从公网访问;这些端口只靠每桌面 token 保护。
  需要收紧时在 `DOCKER-USER` 链限制来源,而不是改 ufw。

### 4.4 hub.mypapers.top 只替换三个二进制

同一桌面（bot 的文件、命令、浏览器和容器网络上的 `web_search` / `web_fetch` 使用 noVNC 那个人的容器，家目录 `/home/desktop`）编在 maclawsrv、hub、desktopd 三个二进制里。容器里的 supervisor 和镜像 `maclaw-gui:2` 本来就把 `HOME` 放在 `/home/desktop`。这类发布不重建镜像，不把 supervisor 拷进正在运行的容器，不重启 Docker。

不要用 `deploy_desktopd.cmd` 和 `desktopd/remote_deploy.sh`。那个脚本会重写 `/etc/systemd/system/maclaw-desktopd.service`（`StandardOutput=append:` 在这台机器上解析失败）、默认构建镜像，并对每个 `maclaw-desktop-*` 容器执行 `docker cp` supervisor。不要用 `deploy_maclawsrv.cmd` 附带的 `build/maclawsrv_deploy/remote_deploy_maclawsrv.sh`。它会重写 `/data/soft/maclaw_srv/start.sh` 和 `maclawsrv.service`。Hub 用目录里已有的 `start.sh`，不覆盖 `configs/config.yaml`，不停 `/data/soft/hubcenter/maclaw-hubcenter`。

主机是 `maclawsrv.mypapers.top`（与 `hub.mypapers.top`、`dockerd.mypapers.top` 同一台）。常规二进制替换不改 nginx、单元文件和已有 `.env`。Hub 机器人要连上这台人的云桌面，还要有下面的连接配置。desktopd 收到停止信号时只关掉自己的 HTTP 服务，已有容器继续跑。重启这三个进程会丢掉当时还在进行的对话。

本机交叉编译（`CGO_ENABLED=0 GOOS=linux GOARCH=amd64`，用当前工作区，不只编译 HEAD）：

```bash
go build -ldflags "-s -w -X main.serviceVersion=20261010.samedesktop -X main.serviceCommit=<git>-dirty -X main.serviceBuiltAt=<RFC3339>" -o maclawsrv ./MaClawSrv
go build -ldflags "-s -w" -o maclaw-hub ./hub/cmd/hub
go build -ldflags "-s -w" -o desktopd ./desktopd/cmd/desktopd
```

主机上先备份再覆盖，`chmod 755`，然后只重启已有进程：

```bash
systemctl restart maclawsrv.service
( cd /data/soft/hub && ./start.sh )
systemctl restart maclaw-desktopd.service
```

maclawsrv 启动后大约 3 秒才监听，立刻访问会 connection refused。确认：

```bash
curl -fsS --max-time 3 http://127.0.0.1:18080/version
curl -sS -o /dev/null -w '%{http_code}\n' --max-time 3 http://127.0.0.1:9399/api/mobile/bootstrap
systemctl show maclawsrv.service maclaw-desktopd.service -p Id,MainPID,ActiveState --no-page
docker ps -a --filter name=maclaw-desktop- --format '{{.ID}} {{.Status}} {{.Names}}'
```

Hub 二进制没有 `/version`。用新 pid 和 `sha256sum /data/soft/hub/maclaw-hub` 确认。`/api/mobile/bootstrap` 返回 401 表示 Hub 在听。

2026-10-10 这次替换的结果：

| 项 | 值 |
| --- | --- |
| maclawsrv `/version` | `20261010.samedesktop`，commit `5e3bfe7b-dirty`，built_at `2026-10-09T22:58:46Z`，MainPID 1034 |
| hub | start.sh 报出的 pid 1093 |
| desktopd | 单元 `maclaw-desktopd.service`，MainPID 1099 |
| HubCenter | pid 11326，未重启 |
| 已运行的桌面容器 | `ba5d94bd0611`（`maclaw-desktop-af24af109248707f`）发布前后都是 Up，浏览器没有被这次替换重启 |
| Hub 探测 | `127.0.0.1:9399 /api/mobile/bootstrap` 返回 401 |

SHA256：

- maclawsrv `19b2f52aa3b42b6964024f98f4c967c37c656a2540c8836bfdec013687e71f6d`
- maclaw-hub `fc5853d5075b45907389b5539ab9caa91976b4a76eb57a5d6a698c4775bb1874`
- desktopd `afdc48cb23b8d814d27080acd7747a646cfea43ab0e4967ca95dfdf5ab730184`

备份在二进制旁边，不要覆盖：

- `/data/soft/maclaw_srv/bin/maclawsrv.bak-samedesktop-20261010`（上一份是 `20261010.desktop`）
- `/data/soft/hub/maclaw-hub.bak-samedesktop-20261010`
- `/data/soft/maclaw_desktopd/bin/desktopd.bak-samedesktop-20261010`

回滚是把这三份备份拷回原名，`chmod 755`，再按上面三条命令重启。桌面 GUI 不在这次发布里，要等单独构建。

结果核对只编在 maclawsrv 里。2026-10-10 这一次只替换了 `/data/soft/maclaw_srv/bin/maclawsrv`，没有重启 hub、desktopd、HubCenter，也没有启动或停止已有桌面容器。版本是 `20261010.outcome`，commit `87078b30-dirty`，built_at `2026-10-10T07:22:27Z`，MainPID 12187，SHA256 `60b144c87430473aca2197fb6c79aed2db756d72c8c95cf72f33cd4932f73552`。上一份 `20261010.botssh` 留在 `maclawsrv.bak-outcome-20261010`（SHA256 `aaf36016a46e6f03e731727d99904521b190435f63b8dbb52b133525c1cc478e`）。hub 仍是 pid 7067，desktopd 仍是 pid 1440，HubCenter 仍是 pid 2181。回滚只把这份备份拷回原名并 `systemctl restart maclawsrv.service`。

#### 连接配置（2026-10-10 07:41 CST）

`20261010.samedesktop` 上线后，Bot 1 执行「打开百度、截屏、把图片放到桌面」时，`app_list`、`screenshot` 等桌面工具都返回 `this bot's desktop is the person's cloud desktop, and the connection is not configured`。当时 maclawsrv 进程没有这两个变量，hub 进程没有 `MACLAW_DESKTOP_API_TOKEN`。人在页面里打开 noVNC 走登录态的桌面接口，不读这组变量，所以画面还能开。

这次只补连接，没有换二进制，没有重启 desktopd、Docker、HubCenter，也没有启动或停止已有桌面容器：

- `/data/soft/hub/desktop-api.env` 权限 600，一行 `MACLAW_DESKTOP_API_TOKEN`（64 个十六进制字符）。值只留在这台机器上，文档、仓库和日志都不记录。
- `/data/soft/hub/start.sh` 在 `APP_DIR=` 之后，若该文件存在则 `set -a` 再 source。仓库 `hub/start.sh` 有同样几行。以后替换二进制时不要覆盖这份 `start.sh`，否则 hub 进程会丢掉这个变量。备份是 `/data/soft/hub/start.sh.bak-desktop-api-20261010`。
- `/data/soft/maclaw_srv/.env` 追加 `MACLAW_HUB_URL=http://127.0.0.1:9399` 和同一个 token，文件权限仍是 600。备份是 `/data/soft/maclaw_srv/.env.bak-desktop-api-20261010`。systemd 的 `EnvironmentFile` 只在 maclawsrv 启动时读入，所以执行了 `systemctl restart maclawsrv.service`。Hub 用已有的 `start.sh` 重启后才带上 token。
- `DESKTOPD_TOKEN` 是 desktopd 自己的钥匙，和这组变量无关。

确认时没有打开真实用户桌面。向 `POST /api/v1/desktop-services/session` 提交一段非法 JSON，并带上这个 Bearer：返回 400，表示鉴权已通过、请求在打开容器之前被拒绝。不带或带错时仍是 401。`/api/mobile/bootstrap` 仍是 401。两边进程里 token 长度都是 64，maclawsrv 的 `MACLAW_HUB_URL` 长度是 21。

| 项 | 值 |
| --- | --- |
| maclawsrv `/version` | 仍是 `20261010.samedesktop`，MainPID 21705，07:41:07 CST 日志 `maclawsrv listening` |
| hub | start.sh 报出的 pid 21772 |
| desktopd | MainPID 仍是 1099，状态 active |
| HubCenter | pid 仍是 11326 |
| 桌面容器 `ba5d94bd0611` | 配置写入之前已在 2026-10-10 07:34:17 CST 以退出码 0 结束（`StartedAt` `2026-10-09T20:44:23Z`）。这次重启没有对它执行启动或停止 |

重启 maclawsrv 和 hub 会丢掉当时还在进行的对话。把原来的桌面请求再发一次后，Hub 会打开这同一个容器。这次配置探测只证明接口认这把钥匙，还没有替用户完成百度截图。

#### desktopd 二进制刷新（2026-10-11 22:14 CST）

工作区 `corelib/tool` 与 `corelib` 有未提交改动，desktopd 依赖这两个包，所以按本节重编并替换二进制。没有跑 `deploy_desktopd.cmd`，没有重建镜像，没有 `docker cp` supervisor，没有改 nginx、单元文件和 `.env`。

本机编译（`CGO_ENABLED=0 GOOS=linux GOARCH=amd64`，用当前工作区 commit `beeb2e8e-dirty`，不只编译 HEAD）：

```bash
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -ldflags "-s -w" -o desktopd ./desktopd/cmd/desktopd
```

| 项 | 值 |
| --- | --- |
| 旧 desktopd | SHA256 `7a0da1d961b6a3357198e7952a4273b9660b9c59b018dc5020ad54d54266fb69`（2026-10-10 13:08 那份），MainPID 1440 |
| 新 desktopd | SHA256 `3fae509c69673180dfd93be30381837bfa3b4db9dfd8ac87b504a141f62baf8e`，MainPID 5452 |
| 备份 | `/data/soft/maclaw_desktopd/bin/desktopd.bak-corelib-20261011`（不要覆盖） |
| 启动日志 | 22:14:15 `desktopd listening on 127.0.0.1:18081`、`[desktopd-proxy] forward proxy listening on 172.17.0.1:18083`、`idle reaper enabled max_idle=24h0m0s`、`image maclaw-gui:2 is present; not pulling` |
| 验证 | `/v1/health` 带 `DESKTOPD_TOKEN` 返回 200 `{"ok":true}`，不带仍是 401；四个 `maclaw-desktop-*` 容器的 ID 与状态发布前后一致（都是 Exited），没有启动或停止任何容器 |

回滚是把这份备份拷回原名，`chmod 755`，再 `systemctl restart maclaw-desktopd.service`。

同一次操作也把这份二进制刷到了 `maclawsrv.maclaw.top`（2026-10-12 14:11 CST，那台机是 UTC 时钟）。这台机运行我的 papers 那台用的出网代理 `*:18082`，所以替换时会短暂断开已建立的 CONNECT 隧道。

步骤与上面相同：没有重建镜像，没有改单元文件和 `.env`。

| 项 | 值 |
| --- | --- |
| 旧 desktopd | SHA256 `a5bf071b3ccad3390d96cf7ea0143869bf917c9c056a0f22e69fdcb796d4c625`（2026-10-07 21:59 那份），MainPID 2744953 |
| 新 desktopd | SHA256 `3fae509c69673180dfd93be30381837bfa3b4db9dfd8ac87b504a141f62baf8e`，MainPID 3818958 |
| 备份 | `/data/soft/maclaw_desktopd/bin/desktopd.bak-corelib-20261011`（不要覆盖） |
| 验证 | `/v1/health` 带 `DESKTOPD_TOKEN` 返回 200 `{"ok":true}`；两个 `maclaw-desktop-*` 容器 ID 与状态前后一致（都是 Exited）；本机走 TLS 代理访问 google 返回 204 |

出网链路发布后复测正常（证书 CN `*.maclaw.top`，Let's Encrypt `YE1`，有效期至 2026-12-17；本机用 `127.0.0.1` 测要加 `--proxy-insecure`，SAN 不含 IP）：

- mypapers → `dockerd.maclaw.top:18082` → google：204，connect 0.25s / total 0.95s
- mypapers 本机桥接 `172.17.0.1:18083` → google：204 / 百度：200

发布前 19:00 那条 `hm.baidu.com:443` 的 CONNECT `i/o timeout` 是瞬时抖动，不是这次替换引入的：重启前后各出现过一次，按现在的延时复测都是通过。

#### 三件套核对与 12344（2026-10-12 22:06 CST）

HEAD 从 `beeb2e8e` 进到 `11160b75`，`build_number` 是 `12344`，所以按 `deploy_maclawsrv.cmd` 的取参方式（`VERSION` 读 `build_number`、`COMMIT` 短哈希、`BUILT_AT` 当前时间，`-dirty` 手工补）重编三件套，先比哈希再决定动哪个：

| 二进制 | 本机编译（工作区 `11160b75-dirty`） | 替换前的线上 | 处理 |
| --- | --- | --- | --- |
| maclawsrv | `80b25b612fb0a1629a1e0af10ba3162b2c01db4aa2b527b6b9b6b5936a097e52`（`-X main.serviceVersion=12344`） | `/version` 报 `12342`（21:53 那份） | 替换并重启 `maclawsrv.service` |
| maclaw-hub | `93f2fcf162cae2d382ee3495648c8c57def8201b30bdf319ed6b9291717d8ec2` | `93f2fcf162cae2d382ee3495648c8c57def8201b30bdf319ed6b9291717d8ec2` | 一致，没有替换、没有重启 |
| desktopd | `fdee68940fe9d6e5ea99d5fa822b102ee693e8d9503f62e8ae24c47c57f50c44` | 两台都是 `3fae509c…`（上一节那份） | 两台都替换并重启 `maclaw-desktopd.service` |

比哈希要先对齐编译配方，否则 strip 与否就会造成假差异：hub 按 `deploy_all_maclinux.sh` 的 `run_go_build` 编译（不注入 ldflags、不 strip）才和线上的 92 MB 一致，stripped 版只有 66 MB。

| 项 | 值 |
| --- | --- |
| maclawsrv | `/version` 返回 `{"built_at":"2026-10-12T14:03:30Z","commit":"11160b75-dirty","version":"12344"}`，MainPID 604 |
| desktopd（mypapers） | MainPID 568，`/v1/health` 带 token 200 |
| desktopd（maclaw.top） | MainPID 3898868，`/v1/health` 带 token 200 |
| hub / hubcenter | pid 仍是 29891 / 29837，没有重启 |
| 桌面容器 | mypapers 四个、maclaw.top 两个，ID 与状态前后一致（都是 Exited） |
| `/api/mobile/bootstrap` | 仍是 401 |
| 备份 | 两台 `desktopd.bak-12344-20261012`、`/data/soft/maclaw_srv/bin/maclawsrv.bak-12344-20261012` |

maclawsrv 这次大约 10 秒后才开始监听，比本节前面记的 3 秒慢：重启后马上 `curl /version` 会 connection refused，先等十几秒再判定。

回滚是把这三份备份拷回原名、`chmod 755`，再 `systemctl restart maclawsrv.service`（仅 mypapers）和 `maclaw-desktopd.service`（两台）。

#### 两台对齐到 12351（2026-10-13 09:00 CST）

`build_number` 从 `12344` 走到 `12351`；`maclawsrv.maclaw.top` 上有人先刷到了 `12350`，两边不一致。按同一配方重编后先比哈希再动手：desktopd 重编出来仍是 `fdee6894…`，跟两台线上的一致，这次没有替换。

| 二进制 | 新哈希（12351） | 处理 |
| --- | --- | --- |
| maclawsrv | `b3ee772492ed609bdb7d280c03d185720dbcdc03ecabab7e755feff642dc6e6f` | 两台都替换并重启 |
| maclaw-hub | `db661da5752190db73a72e34cf158bc5c668d0173c0c5ae6ed8f2785f0cf7b8d` | 两台都用各自的 `start.sh` 重启，不覆盖 `configs/config.yaml`，不停 hubcenter |
| desktopd | `fdee68940fe9d6e5ea99d5fa822b102ee693e8d9503f62e8ae24c47c57f50c44` | 与线上一致，没有替换 |

- mypapers：`/version` 报 `{"built_at":"2026-10-13T00:59:06Z","commit":"11160b75-dirty","version":"12351"}`，maclawsrv 仍用 `systemctl restart maclawsrv.service`。
- maclaw.top：那份 `maclawsrv.service` 是标准单元（`EnvironmentFile=/data/soft/maclaw_srv/.env`、`ExecStart=/data/soft/maclaw_srv/bin/maclawsrv`），直接 `systemctl restart` 即可。目录里的 `start.sh` 是 `pkill` + `nohup` 那套，会和 systemd 抢管理权，**不要用它**启停 maclawsrv（hub 的 `start.sh` 是另一回事，正常用）。
- 备份：两台 `maclawsrv.bak-12351-20261013`、`maclaw-hub.bak-12351-20261013`。
- 这次部署赶上并行的发布活动：中间 mypapers 的 hub 与 hubcenter 被别的进程重启过（PID 从 29891/29837 变到几千号段），本机重建的 hub 备份也是对方 07:49 那份。两边同时发布会互相覆盖，核对结果前先重新取 PID。

#### 12352（2026-10-13 15:35 CST）

`build_number` 从 `12351` 走到 `12352`，HEAD 仍是 `11160b75`（工作区改动从 222 涨到 354 个文件）。三个二进制重编后哈希全变了——hub 和 desktopd 没有注入版本变量，哈希变就说明代码真的动了，不是 `built_at` 造成的假差异。

| 二进制 | 新哈希（12352） | 处理 |
| --- | --- | --- |
| maclawsrv | `def33dc772f56821137dcb42e223dcab8ef8189b9ad2601eb55f62495788a893` | 两台替换重启，`/version` 报 `{"built_at":"2026-10-13T07:34:20Z","commit":"11160b75-dirty","version":"12352"}` |
| maclaw-hub | `f74d6a8cd1d09b4f6f9b6e9d9c53fdc06bffd32b85ee6ce9b402fb48666f4cdd` | 两台用各自的 `start.sh` 重启 |
| desktopd | `2bed659da9f6e9400972a743ff1302709a8d5a3f5eb7fbfc0bb7ae7142522771` | 两台 `systemctl restart maclaw-desktopd.service` |

验证：两台 `/v1/health` 带 token 都是 200、`/api/mobile/bootstrap` 401、桌面容器 ID 与状态前后一致（mypapers 四个、maclaw.top 两个，都是 Exited）。重启后 PID：mypapers hub 31435、maclawsrv 31295、desktopd 31254；maclaw.top hub 4079481、maclawsrv 4079320、desktopd 4079304。

备份（两台各一份）：`maclawsrv.bak-12352-20261013`、`maclaw-hub.bak-12352-20261013`、`desktopd.bak-12352-20261013`。回滚是把三份拷回原名、`chmod 755` 后重启对应服务。

这次又撞上并行发布：maclaw.top 上抓到 `cp -f /tmp/aicoder_deploy/src/bin/maclaw-hubcenter /data/soft/hubcenter/maclaw-hubcenter`，另一个发布流程同时在刷 hubcenter。本节的流程不碰 hubcenter，但两边同时重启会互相干扰，验证前先重新取 PID。

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
这些接口一律返回 401。`hub.mypapers.top` 上这组变量的落点见第 4.4 节。

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

**Agent**:MaClawSrv 的 `desktop` 工具 `{"action":"screenshot"}`。`MACLAW_HUB_URL` 与
`MACLAW_DESKTOP_API_TOKEN` 都存在时走 Hub,操作 noVNC 那个人的云桌面。Hub 机器人缺任一变量时工具直接失败,返回 `this bot's desktop is the person's cloud desktop, and the connection is not configured`,不会改去截本机 `maclaw-gui`。没有绑定云桌面的调用仍可以走本机容器。图片附给支持视觉的模型
(过大时转 JPEG),对话历史里只有尺寸说明,像素坐标与 `app_run` 点击坐标一致。

### 7.1 截图参数(缩放 / 局部放大 / 格式 / 光标 / 标号)

三层接口都接受同一组可选参数(GET 用 query,POST 用 JSON 字段;Hub 同 JSON 字段)。全部不填时与旧接口逐字节一致(全屏 PNG)。

| 参数 | 说明 |
|---|---|
| `max_width` / `max_height` | 等比缩小到不超过该宽/高(只缩不放,16..8192) |
| `region` | 局部放大:`{"x":..,"y":..,"width":..,"height":..}`(屏幕像素;GET 写 `region=x,y,w,h`)。先裁剪再按 max_* 缩放 |
| `format` / `quality` | `png`(默认)/`jpeg`/`webp`;`quality` 1..100,默认 80 |
| `cursor` | 在图上画鼠标指针(Xvfb 截图本身不含指针) |
| `marks` | 画出最近一次 AT-SPI `app_probe` 的编号框(set-of-marks) |

desktopd 在响应头里返回几何信息:`X-Desktop-Screen-Size: 1280x800`、`X-Desktop-Image-Size: 1024x640`、
`X-Desktop-Shot-Region: 0,0,1280,800`、`X-Desktop-Shot-Scale: 0.800000`(图像像素/屏幕像素)、`X-Desktop-Cursor: x,y`;
Content-Type 随格式变化。Hub JSON 增加 `screen_width/screen_height/image_width/image_height/scale/region/cursor`。
屏幕像素 = region 原点 + 图像像素 / scale。处理在容器内用 ImageMagick 完成(WebP 需要它)。

Agent(MaClawSrv `desktop` 工具)记住每个用户最近一次**全屏**截图的几何:之后 `app_run`/`computer` 里的坐标都按该截图像素换算回屏幕像素;
`zoom`/`region` 截图不改变坐标系;输入后的自动截图沿用上次的缩放/格式参数。

### 7.2 桌面控制 API(剪贴板 / 窗口 / 屏幕 / 等待 / 无障碍树)

`POST /v1/desktops/ctl`(desktopd)与 `POST /api/v1/desktop-services/ctl`(Hub,同一 desktop API token),body:
`{"tenant_id":..,"user_id":..,"display":":20","request":{"op":...}}`,返回 supervisor 输出的 JSON(`"ok":false` 时带 `error`,HTTP 仍为 200;未知 op 为 400)。
实现在容器内 `python3 /desktop_supervisor.py ctl <base64 json>`,随 supervisor 一起 `docker cp` 更新,旧容器下次打开即可用。

| op | 参数 | 说明 |
|---|---|---|
| `clipboard_get` | `type`: text/image | 读剪贴板(UTF-8 文本,或 PNG base64) |
| `clipboard_set` | `text` 或 `image_base64`(PNG) | 写 CLIPBOARD(文本同时写 PRIMARY);用于粘贴长文本/中文,绕过输入法 |
| `windows` | `all` | 窗口列表:id、title、class、pid、x/y/width/height、desktop、active/maximized/minimized |
| `window` | `id` 或 `title`,`do`: activate/maximize/minimize/restore/move/resize/move_resize/close,`x,y,width,height` | 有 wmctrl 时用 wmctrl(新镜像已装),旧容器回退 xdotool/xprop(最大化改为铺满工作区) |
| `screen` | | 分辨率、指针位置 |
| `wait` | `for`: change/window,`title`,`gone`,`timeout`(≤60s),`threshold` | 等屏幕变化或窗口出现/关闭 |
| `status` | | 当前 noVNC 连接数(`viewers`),MCP 用来判断有人在接管 |
| `a11y_probe` | `window`: focused/all/标题片段,`max_nodes`(≤400,默认 120),`max_depth`,`interactive_only` | AT-SPI 元素树:id、role、name、states、bounds(屏幕像素);只列 showing+visible 且有尺寸的元素,可交互的优先;浏览器不在内(用 CDP) |
| `a11y_act` | `id`,`do`: click/focus/set_text/locate,`text` | 按最近一次 probe 的 id 操作;id 对应的元素变了会报错要求重新 probe;click 默认返回中心点由 xdotool 真点击,focus 用 grabFocus,set_text 用 EditableText(密码框拒绝) |

### 7.3 Agent 动作(MaClawSrv `desktop` 工具)

- `app_run` 步骤新增:`right_click`/`double_click`/`triple_click`/`middle_click`、`click{button,clicks,modifiers}`、`move`、`mouse_down`/`mouse_up`、
  `drag{from_x,from_y,to_x,to_y}`、`scroll{x,y,direction,amount,modifiers}`、`cursor_position`、`wait{duration}`、`hold_key{key,duration}`、
  `key{key,repeat}`、`paste{text}`(剪贴板 + Ctrl+V,终端里 Ctrl+Shift+V)、按元素 id 的 `click/focus/set_text`、`screenshot`、`zoom{region}`。
  只用老四种步骤且未使用缩放截图时走原路径,行为不变。
- `action=computer`:`actions` 数组,兼容 Anthropic computer-use(`left_click`、`left_click_drag`、`scroll` 的 `scroll_direction/scroll_amount`、`key`/`hold_key` 的 `text`、`zoom` 的 `region` 等;
  `{name:"computer",input:{...}}` 也可)与 OpenAI CUA(`{type:click|double_click|scroll|type|keypress|wait|screenshot|move|drag}`)。按顺序执行,遇到第一个失败即停,
  逐步返回结果,后续步骤标为 `not executed`;有输入时最后附一张新截图。
- `app_probe`(可 `marks:true` 附带编号截图)、`clipboard_get`/`clipboard_set`、`window_list`/`window`、`screen_info`、`wait`。
- 密码框保护、用户接管时让出(每步之前再检查)保持不变;`paste` 的 Ctrl+V 与 `type` 走同一个密码框检查。
- 共享实现在 `corelib/desktop/computeruse`(MCP 服务也用)。

### 7.4 减少干扰(镜像与 supervisor 安装)

`install_browser_integration`(镜像构建时与每次 ensure 时都执行,幂等):
- Chromium 托管策略 `/etc/chromium/policies/managed/maclaw-agent.json`:`TranslateEnabled=false`、`PromptForDownloadLocation=false`、
  `DownloadDirectory=${home}/Downloads`、`PasswordManagerEnabled=false`、`AutofillAddressEnabled=false`、`AutofillCreditCardEnabled=false`、
  `DefaultNotificationsSetting=2`、`DefaultBrowserSettingEnabled=false`、`PromotionalTabsEnabled=false`(另有 `maclaw-browser-signin.json`)。
- xfce4-notifyd:用户 autostart 覆盖文件 `Hidden=true`(包文件不动),并在 `/etc/xdg/xfce4/xfconf/.../xfce4-notifyd.xml` 默认开启勿扰。
- 新容器默认分辨率 1280x800:镜像 `ENV MACLAW_DESKTOP_GEOMETRY=1280x800x24`(容器级 env 仍可覆盖)。supervisor 内置回退值仍为 1440x900,所以旧镜像创建的现有容器拿到新 supervisor 后分辨率不变。
- 会话环境去掉 `NO_AT_BRIDGE`,设置 `QT_ACCESSIBILITY=1`(GTK 程序默认就暴露 AT-SPI)。
- `--test-type`:已在测试桌面评估(2026-10-14)——加上后 `--no-sandbox` 警告条消失、CDP 正常;但它是测试框架开关,会把警告对人也隐藏,其它副作用不属于稳定契约。结论:默认不启用,仅当容器环境 `MACLAW_BROWSER_TEST_TYPE=1` 时追加(opt-in)。
- xfwm4 没有"新窗口一律最大化"的设置,未做;Chromium 仍用 `--start-maximized`,其它窗口可用 `window maximize`。

### 7.5 MCP 服务

见 [desktop-mcp.md](desktop-mcp.md)。

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
  断点续传)或在离主机近的机器上编译;镜像在主机上构建或从 ghcr.io 拉取,不要上传镜像。
- **新主机首次启动 desktopd**:本机没有 `maclaw-gui:2` 时会在后台从 ghcr.io 拉取约 800MB;大陆主机可能
  很慢,在拉完之前打开桌面会返回“still being pulled”。不需要时设 `DESKTOPD_IMAGE_SOURCE=off` 并在主机上构建。
- **Hub 未设 `MACLAW_DESKTOP_API_TOKEN`**:`/api/v1/desktop-services/*` 全部 401,MaClawSrv 远程截图
  和远程桌面会失败;Hub 管理页不受影响。`hub.mypapers.top` 已按第 4.4 节写入这组变量。Hub 机器人缺这组变量时工具直接失败,不会改去截本机 `maclaw-gui`。
- **旧镜像建出的容器里任务栏浏览器打不开**(“无法执行默认网络浏览器/输入输出错误”):原因是 XFCE 默认浏览器
  走 `sensible-browser → /usr/bin/chromium`,以 root 且无 `--no-sandbox` 直接退出,即使启动也是另一个无登录、
  无 CDP 的 profile。已修复:新 supervisor 在每次 `ensure`(打开桌面)时写入共用浏览器配置,所以已有容器只需
  按正常部署 `docker cp` 新 supervisor,下次打开桌面即生效,无需重建容器;新建容器用重建后的镜像。
- **停止后再打开,noVNC 反复“桌面 → Connecting → 桌面”**:docker start 后第一次 `ensure` 若被打断
  (desktopd 重启、请求中断、Chromium 启动超时),会留下没记进 `pids.json` 的 Xvfb/XFCE/x11vnc/websockify;
  `pids.json` 在卷上跨 docker stop 保留,重启后旧 pid 又可能被别的进程复用。下一次打开会再起一套桌面,新的
  websockify 绑不上 6081,watcher 每 2 秒杀掉正在用的 x11vnc 重来,于是每 4–5 秒断一次。已修复:supervisor
  按命令行认领容器里实际在跑的桌面进程,`pids.json` 带容器启动标记(重启前的记录作废)、启动中逐步写入,
  watcher 回收僵尸进程,部署新 supervisor 后旧 watcher 在下次打开时被替换。已在运行的容器 `docker cp` 新
  supervisor 后,下次打开桌面(无需停止)即收敛。
- **停止再打开后 Google 网站显示已退出,其他网站仍登录**:Hub 的停止路径是优雅的(supervisor `flush` →
  CDP `Browser.close` → `exit_type` Normal → docker stop),cookie 都在盘上。原因是 Chromium 的
  “允许 Chromium 登录”(`signin.allowed`):没有 Google API key 的 Linux 构建开着它时,每次浏览器启动都会让
  Google 网站登出。已修复:`install-browser`/`ensure` 写入托管策略
  `/etc/chromium/policies/managed/maclaw-browser-signin.json`(`{"BrowserSignin": 0}`),supervisor 启动浏览器前
  把 Preferences 的 `signin.allowed`/`allowed_on_next_startup` 设为 false。下一次浏览器启动生效,此后需重新
  登录 Google 一次。每次停止/启动在 `desktop.log` 记录 Google 登录 cookie 的数量
  (`[maclaw] stop: before quit: google sign-in cookies browser=N disk=M`,只有数量)便于排查。
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
| `desktopd/image_source.go` | 缺失镜像时从公开来源拉取、检查、打 tag |
| `desktopd/deploy/desktopd.env.example` | desktopd `.env` 全部变量示例 |
| `.github/workflows/desktop-image.yml` | CI 构建、检查并发布 `ghcr.io/rapidai/maclaw-gui` |
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
