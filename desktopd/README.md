# desktopd — Docker 桌面服务

desktopd 在一台安装了 Docker 的主机上为每个 `tenant+user` 运行一个独立的 Chromium
桌面容器。Hub 的 desktoppool 和 MaClawSrv 通过 HTTP `/v1/*` API 调用它。

## 部署

从仓库根目录:

```sh
build_thirdapp.cmd        # 或按需编译 desktopd 与打包 image/desktop_supervisor.py
# remote_deploy.sh 由 deploy_desktopd.cmd 调起,负责:安装二进制、首次生成
# DESKTOPD_TOKEN、写入 .env、构建带 supervisor 的镜像、注册 systemd 服务。
```

必需环境变量(ds systemd 读 `.env`):

| 变量 | 说明 |
| --- | --- |
| `DESKTOPD_TOKEN` | 主 API key(`/v1/*` 的 Bearer,首次部署自动生成) |
| `DESKTOPD_ADVERTISE_HOST` | 容器端口回调时的对外主机名(必须是 Hub/MaClawSrv 可达的名字) |
| `DESKTOPD_ADDR` | 监听地址,默认 `127.0.0.1:18081`(反代或 SSH 隧道用) |
| `DESKTOPD_STATE_DIR` | 管理面板的状态目录(部署脚本指向 `<部署目录>/state`);`off` 完全禁用面板 |
| `DESKTOPD_MAX_IDLE` | 可选空闲回收,如 `24h`;超时的桌面会被停止(登录态先落盘) |
| `DESKTOPD_ALLOW_REMOTE_SETUP` | 设为 `1` 时允许从非 loopback 来源执行一次性的面板初始化 |

## 桌面镜像 maclaw-gui:2

默认镜像是 `maclaw-gui:2`(`desktopd/image/Dockerfile.v2`):Debian 12 + XFCE
完整桌面(面板、Thunar、终端、Mousepad)、Chromium、fcitx5 拼音、Noto CJK 字体、
noVNC、xdotool、ImageMagick/scrot。纯 CPU 软件渲染,不需要 GPU。默认资源
`3g` 内存 / `1.5` CPU / `1g` shm(`corelib/desktop`,Hub 桌面服务表单默认值相同)。
屏幕固定 `1440x900x24`,可用容器环境变量 `MACLAW_DESKTOP_GEOMETRY` 覆盖;
supervisor 在没有 XFCE 的旧镜像(`maclaw-gui:1`)上自动回退 fluxbox。

构建(在 Docker 主机上,构建上下文是 `desktopd/image`):

```sh
# 能访问 Docker Hub 的主机
docker build -f desktopd/image/Dockerfile.v2 -t maclaw-gui:2 desktopd/image
# 腾讯云(生产;无 Docker Hub)
docker build -f desktopd/image/Dockerfile.v2 \
  --build-arg BASE_IMAGE=mirror.ccs.tencentyun.com/library/debian:bookworm \
  --build-arg APT_MIRROR=mirrors.tencentyun.com \
  -t maclaw-gui:2 desktopd/image
```

`deploy_desktopd.cmd` / `remote_deploy.sh` 会在主机上自动构建:先构建到临时
tag、检查镜像契约(supervisor、Xvfb、x11vnc、websockify、xdotool、chromium、
XFCE、import、noVNC)后才打上 `maclaw-gui:2`,构建失败不会覆盖现有镜像;
在腾讯云上(元数据服务可达)自动使用腾讯镜像源。可用 `DESKTOPD_BASE_IMAGE`、
`DESKTOPD_APT_MIRROR`(`none` = deb.debian.org)、`DESKTOPD_SKIP_IMAGE_BUILD=1`
覆盖。旧的 `image/Dockerfile` 只用于 `maclaw-gui:1`,不要把它的构建结果标成 `:2`。

注意:

- 不要往镜像的 `/opt`、`/usr/local` 装东西,desktopd 在这两个位置挂每用户卷。
- `close_range_shim.c` 是 LD_PRELOAD 垫片:Docker 20.10 + 旧 libseccomp 对
  `close_range` 返回 EPERM,GLib 2.74 因此无法启动子进程(XFCE 起不来);垫片把
  EPERM 改成 ENOSYS 让 GLib 走回退路径。升级 Docker/libseccomp 后可去掉。

### 从 maclaw-gui:1 迁移

desktopd 在容器上记录 `maclaw.image` 标签(请求的镜像名)。打开桌面时:

1. 容器的镜像名与请求的不一致(没有标签的旧容器视为 `maclaw-gui:1`)→ 先让
   浏览器写入登录态、`docker stop`、把容器层 commit 成
   `maclaw-desktop-user-<key>:state`(带 `maclaw.image` 标签)、删除容器,再用新镜像
   重建。四个私有卷(`/desktops`、`/home/desktop`、`/opt`、`/usr/local`)原样挂回,
   浏览器登录、home、装在 /opt 或 /usr/local 的软件都保留。
2. `:state` 镜像只在它的 `maclaw.image` 与请求镜像相同时才使用(没有标签的旧
   `:state` 视为来自 `maclaw-gui:1`)。不匹配的会被改名为 `:prev`(只保留一代,
   需要时可手动找回 apt 装过的包),用户从新镜像开始;apt 装进旧镜像层的软件
   需要重新安装。
3. 比较的是镜像名而不是镜像 ID:原地重建 `maclaw-gui:2` 不会重启所有桌面;
   要让所有人换新镜像,请换一个 tag(如 `maclaw-gui:3`)并在 Hub 里改镜像名。

Hub 里已保存的 Docker 服务记录带有显式镜像名,升级后需在 Hub 管理页"桌面
服务"里把镜像改成 `maclaw-gui:2`(内存 `3g`、shm `1g`),迁移才会发生。

## 管理面板

浏览器打开 `http://<desktopd地址>:18081/admin`(默认只绑 loopback;远程通过
SSH 隧道 `ssh -L 18081:127.0.0.1:18081 <host>` 访问,连接在服务端表现为
loopback,不受 setup 来源限制影响)。

- **首次设置**:创建面板管理员(与 API key 无关)。默认只允许来源为
  loopback 的请求执行 setup,防止暴露的面板被远端抢注。
- **登录后**:查看服务信息(Docker 状态、默认镜像与资源)、查看主 key
  (`DESKTOPD_TOKEN`,不可删除)、添加/查看/删除额外 API key(删除立即生效)、
  查看运行中的桌面容器。

Hub 侧配置(管理页"桌面服务"标签页或 `POST /api/admin/desktop-services`)
使用面板里的任意一个 key 作为 `access_token`,校验方式:
`curl -H "Authorization: Bearer <key>" http://<host>:18081/v1/health`。

## 与 Hub / MaClawSrv 的关系

- Hub 保持 desktopd 服务与用户/部门分配表;API:
  `POST /v1/desktops`(创建)、`/stop`、`/session`(拿 CDP/noVNC URL)、
  `/app`(xdotool)、`GET|POST /v1/desktops/screenshot`(返回 `image/png`)。
  全部使用同一个 Bearer key。
- 截图:`GET /v1/desktops/screenshot?tenant_id=..&user_id=..[&display=:20]` 或
  `POST` 同名 JSON 字段;desktopd 在容器里 `docker exec` 执行
  `import -window root`(无 ImageMagick 时用 scrot),不会启动桌面,只截已打开的。
  Hub 转发为 `POST /api/v1/desktop-services/screenshot`(返回
  `{"mime","image_base64","bytes"}`);MaClawSrv 的 `desktop` 工具
  `action=screenshot` 把图片附给支持视觉的模型(`SupportsVision`),文本里只有
  尺寸说明,不会把 base64 写进对话历史。
- 容器把 CDP(19020)与 noVNC(6080)发布到宿主机。两者的 gate 在容器内
  (desktop_supervisor.py)要求 Bearer token;token 以 URL userinfo 形式随
  `cdp_url` / `novnc_url` 分发,CDP 客户端转成 Authorization 头,Hub 的
  noVNC 代理代为注入。旧镜像无 token 时自动回退直连行为。
- 容器内 supervisor 每用户一个 X display + profile 卷;`ensure` 幂等,
  停止前 flush 浏览器登录态。

## 排障

- 服务日志:`<部署目录>/logs/desktopd.log` / `desktopd.err.log`。
- 每用户桌面日志:`/desktops/<key>/desktop.log`(容器内)。
- `systemctl status maclaw-desktopd`;手动起:`systemctl restart maclaw-desktopd`。
- token 不生效:先确认 `.env` 的 `DESKTOPD_TOKEN` 与 Hub 配置一致;面板
  删除 key 会立即生效。
- 首次建容器慢:镜像不存在时 desktopd 会 `docker pull`,可能超过调用方 2 分钟
  超时;`maclaw-gui:2` 是本地构建的镜像,先在主机上构建好(见上)。
- 迁移日志:`desktopd: recreating ... (image changed from maclaw-gui:1 to maclaw-gui:2)`、
  `... keeping it as ...:prev`,在 `desktopd.err.log`(log 包写 stderr)。
