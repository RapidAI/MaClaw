# desktopd — Docker 桌面服务

desktopd 在一台安装了 Docker 的主机上为每个 `tenant+user` 运行一个独立的 Chromium
桌面容器。Hub 的 desktoppool 和 MaClawSrv 通过 HTTP `/v1/*` API 调用它。

完整的 `maclaw-gui:2` 使用与运维手册(镜像能力、架构、Linux 部署脚本、Hub 配置、迁移、
截图 API、冒烟、回滚、已知问题)见 [docs/desktop-gui-v2.md](../docs/desktop-gui-v2.md)。
主机辅助脚本在 [`scripts/`](scripts/),配置示例在 [`deploy/`](deploy/)。

## 部署

从仓库根目录:

```sh
build_thirdapp.cmd        # 或按需编译 desktopd 与打包 image/desktop_supervisor.py
# remote_deploy.sh 由 deploy_desktopd.cmd 调起,负责:安装二进制、首次生成
# DESKTOPD_TOKEN、写入 .env、构建带 supervisor 的镜像、注册 systemd 服务。
```

Linux/macOS 上用 `deploy/linux/rollout_desktop_gui2.sh`(编译、打包、断点续传上传、
备份、部署 desktopd/Hub/MaClawSrv、验证、回滚),见 [docs/desktop-gui-v2.md](../docs/desktop-gui-v2.md#4-部署)。

必需环境变量(ds systemd 读 `.env`):

| 变量 | 说明 |
| --- | --- |
| `DESKTOPD_TOKEN` | 主 API key(`/v1/*` 的 Bearer,首次部署自动生成) |
| `DESKTOPD_ADVERTISE_HOST` | 对外 API 名。未设置 `DESKTOPD_PUBLIC_URL` 时,CDP 和 noVNC 走 `https://` 这个名字上的 `/v1/desktops/tunnel/...`,不再带容器映射端口 |
| `DESKTOPD_PUBLIC_URL` | 可选。隧道的公开基址,例如 `https://dockerd.example.com`。设置后覆盖上面的 `https://` + 主机名 |
| `DESKTOPD_PUBLISH_BIND` | Docker 发布浏览器和屏幕端口时绑定的地址,默认 `127.0.0.1`。只能是 IPv4 回环或内网地址。`0.0.0.0` 会被拒绝。已在跑的容器不会因此重建 |
| `DESKTOPD_ADDR` | 监听地址。部署脚本和 `desktopd.env.example` 写的是 `127.0.0.1:18081`(反代或 SSH 隧道用);未设置时二进制回落到 `:18081`(所有网卡,`desktopd/Dockerfile` 的 `-p 18081:18081` 依赖它),裸跑二进制时请显式设置 |
| `DESKTOPD_STATE_DIR` | 管理面板的状态目录(部署脚本指向 `<部署目录>/state`);`off` 完全禁用面板 |
| `DESKTOPD_MAX_IDLE` | 可选空闲回收,如 `24h`。先让 supervisor 停会话并落盘登录态,再确认容器已停;容器还在就 `docker stop`(与 API 停止相同的 25 秒宽限)。容器里有正在使用的 bot 桌面时只停主人会话,不停容器。停止失败会打错误日志 |
| `DESKTOPD_LOG_FILE` | 滚动日志路径,默认 `logs/desktopd.log`(相对工作目录)。`off` 只写 stderr。systemd 237 写不了这个文件,所以由进程自己写 |
| `DESKTOPD_LOG_MAX_BYTES` / `DESKTOPD_LOG_MAX_FILES` | 单文件上限(默认 `32MiB`)和保留的轮转份数(默认 5) |
| `DESKTOPD_LOG_STDERR` | 默认把每一行再复制到 stderr(`journalctl` 仍看得到)。`0` 关闭 |
| `DESKTOPD_PROXY_DIRECT` | 出网代理直连的域名后缀,不走上游。未设置时默认 `xiaohongshu.com,xhscdn.com`;`off` 全部走上游;写了列表就替换默认 |
| `DESKTOPD_ALLOW_REMOTE_SETUP` | 设为 `1` 时允许从非 loopback 来源执行一次性的面板初始化 |
| `DESKTOPD_PROXY` | 设为 `1` 时本机 desktopd 开启内置出网代理（见下节），部署脚本会把这三个变量写入 `.env` |
| `DESKTOPD_PROXY_ADDR` | 出网代理监听地址，默认 `:18082`；必须对消费者机器可达（区别于 API 的 127.0.0.1 约定） |
| `DESKTOPD_PROXY_TOKEN` | 出网代理 key；`DESKTOPD_PROXY=1` 且未提供时部署脚本自动生成 URL 安全的 hex token；手工 `.env` 省略则复用 `DESKTOPD_TOKEN`（base64 含 `/` 时在代理 URL 里写成 `%2F`） |
| `DESKTOPD_PROXY_TLS_CERT` / `DESKTOPD_PROXY_TLS_KEY` | 代理监听的 TLS 证书/私钥（如 `/root/.lego/certificates/_.maclaw.top.{crt,key}`）；配置后消费者用 `https://` 代理 URL 连接——CONNECT 请求与 key 全程加密，也避开对明文 CONNECT 行的连接重置 |
| `DESKTOPD_UPSTREAM_PROXY` | 消费端：docker 拉镜像/构建与本机 desktopd 桥接监听的级联上游（另一台 desktopd 的出网代理 URL，见下节） |
| `DESKTOPD_UPSTREAM_PROXY_APPLY` | `1` 时写 dockerd 的 systemd drop-in 并重启 docker；只影响构建参数与桌面容器时不需要设置 |
| `DESKTOPD_UPSTREAM_PROXY_NO_PROXY` | 覆盖默认 no-proxy 列表 |
| `DESKTOPD_DESKTOP_PROXY_URL` | 消费端：注入桌面容器的代理 URL（`http://<网桥网关>:<端口>`，如 `http://172.17.0.1:18083`）；desktopd 据此在本机网桥地址上开免鉴权监听 |
| `DESKTOPD_IMAGE_SOURCE` | 本机缺少 `maclaw-gui:2` 时拉取的公开镜像,默认 `ghcr.io/rapidai/maclaw-gui:2`;可写 `@sha256:` digest;`off` 关闭。已有的本地镜像从不被拉取覆盖 |
| `DESKTOPD_IMAGE_PULL_TIMEOUT` | 上面那次拉取的超时,默认 `30m` |

全部变量的示例见 [`deploy/desktopd.env.example`](deploy/desktopd.env.example)。

## 桌面镜像 maclaw-gui:2

公开预构建镜像:`docker pull ghcr.io/rapidai/maclaw-gui:2`(匿名可拉,另有 `2-<短sha>` 和 `@sha256:` 固定版本,
见 [docs/desktop-gui-v2.md](../docs/desktop-gui-v2.md#公开镜像快速开始))。desktopd 发现本机没有 `maclaw-gui:2`
时会自动拉取它、做契约检查并打成本地 tag(`DESKTOPD_IMAGE_SOURCE=off` 关闭)。

默认镜像是 `maclaw-gui:2`(`desktopd/image/Dockerfile.v2`):Debian 12 + XFCE
完整桌面(面板、Thunar、终端、Mousepad)、Chromium、fcitx5 拼音、Noto CJK 字体、
noVNC、xdotool、ImageMagick/scrot。纯 CPU 软件渲染,不需要 GPU。默认资源
`3g` 内存 / `1.5` CPU / `1g` shm(`corelib/desktop`,Hub 桌面服务表单默认值相同)。
屏幕固定 `1440x900x24`,可用容器环境变量 `MACLAW_DESKTOP_GEOMETRY` 覆盖;
supervisor 在没有 XFCE 的旧镜像(`maclaw-gui:1`)上自动回退 fluxbox。

H.264 观看与接管(默认关闭,noVNC 仍可用):容器环境变量 `MACLAW_DESKTOP_H264=on` 时,
noVNC gate(6080)响应 `GET /h264.html`(WebCodecs 播放器,缺 WebCodecs 时用 MSE,再不行回退 noVNC)
和 `/h264` 的 WebSocket(ffmpeg x11grab → H.264 → Annex B 长度前缀帧;同一连接上传键鼠、剪贴板,
XTEST 注入;输入租约 5s)。只有 owner 自己的屏幕或 takeover 中的 bot 屏幕接受输入。
开关关闭时 `h264.html` 302 回 noVNC。每个容器最多 4 路,`MACLAW_DESKTOP_H264_FPS` 默认 15。
详见 `docs/desktop-h264-takeover.md`。

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
覆盖;`DESKTOPD_IMAGE_FROM=pull`(拉公开镜像,检查后打 tag)或 `auto`(先拉,失败再构建)可以不在主机上构建,
默认仍是 `build`。旧的 `image/Dockerfile` 只用于 `maclaw-gui:1`,不要把它的构建结果标成 `:2`。

`hub.mypapers.top` 上已经在跑的桌面不要用这个脚本做日常二进制更新。脚本会重写
systemd 单元、默认构建镜像，并把 supervisor 拷进每个 `maclaw-desktop-*` 容器。
同一桌面这类只改三个 Go 二进制的发布见
[docs/desktop-gui-v2.md 第 4.4 节](../docs/desktop-gui-v2.md)。

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

## 出网代理（境外 desktopd 中转）

desktopd 内置一个带鉴权的出网代理：一台境外机器上的 desktopd 打开
`DESKTOPD_PROXY=1`，国内其它 desktopd 主机把 dockerd 指向它，即可访问
Docker Hub、构建镜像和一般 HTTPS 工具流量。代理支持 HTTP CONNECT（隧道，
dockerd / docker build / curl 对 https 目标都走这个）与绝对地址形式的
普通 HTTP 转发。

**管理面板（推荐）**：上面的 .env 是引导值，日常运维在两地的 /admin 完成——

- 境外机（服务端）「出网代理 key」卡片：查看/替换 key（存
  `<state>E/proxy_token.json`，即时生效，无需重启）；key 卡片只在
  `DESKTOPD_PROXY=1` 的机器上显示。未提供 `DESKTOPD_PROXY_TOKEN` 时
  回退使用 `DESKTOPD_TOKEN`，面板「恢复环境变量值」作废面板文件。
- 国内机（消费端）「出网代理（上游与桌面容器）」卡片：填上游主机
  （默认 https，端口 18082）与 api key，可先「测试」（真实走代理访问
  google 204 并显示出口 IP），保存后级联与桌面容器即时生效（存
  `<state>/egress_proxy.json`）；改桌面容器 URL 时桌面在下一次打开
  时重建；「同步 dockerd」重写 dockerd 的 drop-in 并重启 docker
  （会断打开中的桌面会话，可选）。
- 面板文件优先于环境值；「恢复环境变量值」删除面板文件回到 .env 的
  状态（dockerd drop-in 一并恢复）。
- 管理页是手写嵌入页面，测试 `TestAdminPageJsIdsConsistent` 锁住其
  JS↔HTML 元素引用。

**服务端（境外机器）**

```sh
# .env
DESKTOPD_PROXY=1
DESKTOPD_PROXY_ADDR=:18082        # 默认；消费者可达的地址
DESKTOPD_PROXY_TOKEN=<key>        # 省略则复用 DESKTOPD_TOKEN
```

放行防火墙/安全组 18082（TCP 入）。验证：

```sh
curl -sv -x "http://docker:<key>@<境外机>:18082" https://www.google.com/generate_204 -o /dev/null
# 期望 HTTP/1.1 200 Connection established 后接 204
```

**消费端（国内机器）**

dockerd 只认 HTTP 代理（daemon.json `proxies` 或 systemd 环境），所以代理
协议就是上面的 http；**强烈建议把上游 URL 写成 `https://`**（代理监听配好
TLS 后，国内→境外段的 CONNECT 请求与 key 全程加密——实测国内网络上明文
`CONNECT www.google.com:443` 会被连接重置，TLS 后消失）。三条路径各自
独立、按需启用：

1. dockerd 拉取镜像——`/etc/docker/daemon.json`（Docker ≥ 23.0）:

   ```json
   {
     "proxies": {
       "http-proxy":  "http://docker:<key>@<境外机>:18082",
       "https-proxy": "http://docker:<key>@<境外机>:18082",
       "no-proxy":    "localhost,127.0.0.1,*.tencentyun.com,10.0.0.0/8,172.16.0.0/12,192.168.0.0/16"
     }
   }
   ```

   `proxies` 只作用于守护进程自身的 pull/push；然后 `systemctl restart docker`。
2. 镜像构建——设置 `DESKTOPD_UPSTREAM_PROXY` 后，`remote_deploy.sh` 的
   `build_desktop_image` 自动带 `--build-arg HTTP_PROXY/HTTPS_PROXY/NO_PROXY`
   （Docker 预定义 build args，不进 `docker history`），构建期 apt/Docker Hub
   走代理；该路径不需要重启 docker，与方式 1 互不依赖。
3. 桌面容器——设置 `DESKTOPD_DESKTOP_PROXY_URL`（见下节），desktopd 起本机
   网桥免鉴权监听并注入容器环境变量，浏览器经它级联到境外代理。
4. （任意版本的手工等价物）systemd drop-in——部署时传
   `DESKTOPD_UPSTREAM_PROXY=...` 和 `DESKTOPD_UPSTREAM_PROXY_APPLY=1`，
   `remote_deploy.sh` 写 `/etc/systemd/system/docker.service.d/maclaw-proxy.conf`
   并重启 docker（内容不变时不重复重启）。

镜像构建走 `--build-arg HTTP_PROXY/HTTPS_PROXY/NO_PROXY`（见消费端方式 2）。

**桌面容器出网（浏览器等软件走境外）**

dockerd 的代理配置管不到容器内的流量，桌面软件靠 desktopd 的第二条监听：

```sh
# 国内机的 .env（部署时分别传入即可，脚本会写入）
DESKTOPD_DESKTOP_PROXY_URL=http://172.17.0.1:18083
DESKTOPD_UPSTREAM_PROXY=http://docker:<key>@<境外机>:18082
```

网关地址先确认（主机自定义了 default-address-pool 时不是 172.17.0.1）：

```sh
docker network inspect bridge --format '{{(index .IPAM.Config 0).Gateway}}'
```

- desktopd 校验该 URL（必须是 RFC1918 IP 字面量 + 显式端口，loopback 拒绝）
  后在 `172.17.0.1:18083` 上启动**免鉴权**监听——只绑网桥网关地址，外网
  不可达，只有本机容器能用；URL 非法时 desktopd 启动即报错（先修配置），
  端口被占等环境问题则降级为容器直连并在日志里大声提示。
- 本机是境外机时该监听直接出网；国内机则自动级联到 `DESKTOPD_UPSTREAM_PROXY`
  （鉴权由 desktopd 持有，容器永远看不到 key；上游 URL 支持 `https://`，
  国内→境外段可整体加密）。
- desktopd 给每个桌面容器注入 `HTTP_PROXY/HTTPS_PROXY`（小写同值）与
  `NO_PROXY=localhost,127.0.0.1,::1`；容器上的 `maclaw.desktop-proxy` 标签
  记录当前值——开启、修改或停用代理后，桌面在下一次打开/重建时生效（用户
  数据卷与登录态按既有迁移流程保留）。
- supervisor 给 Chromium 追加 `--proxy-server=<host:port>`（去掉 URL 里的
  凭据部分）与 `--proxy-bypass-list`，apt/curl 等直接读环境变量。

注意：

- no-proxy 默认包含腾讯元数据与内网镜像域（`*.tencentyun.com` 等）和私网
  段：腾讯云内网镜像源不能绕道境外，需用 `DESKTOPD_UPSTREAM_PROXY_NO_PROXY`
  保留该行为。
- **重启 docker 会打断正在进行的桌面会话**：容器靠 `--restart unless-stopped`
  自动回来，但未 flush 的浏览器登录态有丢失风险，尽量在无人使用时操作。
- 安全边界：代理 key 走明文 HTTP Basic 时公网可见，建议按来源 IP 限防火墙
  或把上游 URL 写成 `https://`；代理永远不会转发到 loopback / 私网 / 云
  metadata 目标（与 `corelib/mcp` P0-6 的 SSRF 姿态一致），hostname 在境外
  机上解析，解析出私网地址一律拒绝。
- docker 自身拉取还可用国内 registry mirror 替代代理，两者不冲突。
- **上游失败**:级联到境外代理时,CONNECT 如果得到 HTTP 502/503/504,或拨号超时、连接被重置,会再试,最多 3 次(间隔 200ms、800ms),单次尝试 8 秒。仍失败才向容器返回 502。407(key 错误)不重试。
- **不必绕境外的域名**:`DESKTOPD_PROXY_DIRECT` 里的后缀由本机直连,不经过上游。默认包含 `xiaohongshu.com` 和 `xhscdn.com`(境外中转对 `as.xiaohongshu.com:443` 经常 `i/o timeout`)。`off` 关闭。googlevideo 这类需要出境的名字不在默认列表里,走重试。
- **错误日志**:同一类错误打到同一个目标,每分钟最多一行,行里带累计计数。另外每分钟有一条 `upstream errors` 汇总。`GET /v1/health` 的 `proxy` 字段是进程启动以来的计数:`timeout`、`http_502`、`other`、`retries`、`direct`、`suppressed_logs`。

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

## 日志(systemd 237)

Ubuntu 18.04 的 systemd 237 不认识 `StandardOutput=append:` / `StandardError=append:`(v240 才加入)。旧单元每次 `daemon-reload` 都警告 `Failed to parse output specifier`,输出落到 journal,配置的 `logs/desktopd.log` 一直是空的。

desktopd 自己追加并轮转这个文件,跟 systemd 版本无关:

- 默认 `logs/desktopd.log`,相对进程工作目录(单元里的 `WorkingDirectory` 就是部署目录)。
- 超过 `DESKTOPD_LOG_MAX_BYTES`(默认 32MiB)时改名为 `desktopd.log.1`,最多保留 `DESKTOPD_LOG_MAX_FILES`(默认 5)份。已有内容不会在打开时被截断。
- 默认再复制一份到 stderr,所以 `journalctl -u maclaw-desktopd -f` 仍然看得到。panic 和打开日志文件之前的致命错误只在 journal 里。
- 跟踪轮转用 `tail -F`(大写 F)。

**已有 18.04 主机**(不要为此整脚本重部署):编辑 `/etc/systemd/system/maclaw-desktopd.service`,删掉 `StandardOutput=append:` 和 `StandardError=append:` 两行,然后

```sh
systemctl daemon-reload
systemctl restart maclaw-desktopd
```

不要改成 `StandardOutput=file:`。`file:` 在 systemd 237 上能解析,但每次启动都会截断。删掉这两行后 systemd 用默认的 journal,文件由进程追加。旧的 `logs/desktopd.err.log` 不再更新;以前写到 stderr 的 `log` 行现在在 `logs/desktopd.log`。

## 排障

- 服务日志:`<部署目录>/logs/desktopd.log`(轮转 `.1` … `.5`),以及 `journalctl -u maclaw-desktopd`。
- 空闲回收:日志里的 `reaping` 只是开始处理。确认停下的是 `reaped ... via=docker-stop`(或容器本来就停了的 `via=container-already-stopped`)。`reaper stop failed` 表示没停下来。有 bot 桌面正在用时,容器会故意留着,日志写明 `bot display is busy`。
- 每用户桌面日志:`/desktops/<key>/desktop.log`(容器内)。
- `systemctl status maclaw-desktopd`;手动起:`systemctl restart maclaw-desktopd`。
- token 不生效:先确认 `.env` 的 `DESKTOPD_TOKEN` 与 Hub 配置一致;面板
  删除 key 会立即生效。
- 首次建容器慢:本机没有 `maclaw-gui:2` 时 desktopd 从 `DESKTOPD_IMAGE_SOURCE` 拉取(启动时已在后台开始),
  可能超过调用方 2 分钟超时,此时返回 “still being pulled”,拉完后重试即可;管理面板“镜像来源”显示进度/失败原因。
  拉不下来(大陆主机访问 ghcr 慢)就在主机上构建(见上)或设 `DESKTOPD_IMAGE_SOURCE=off`。
  日志:`image maclaw-gui:2 is missing; pulling ...`、`ready from ...`、`failed after ...`。
- 迁移日志:`desktopd: recreating ... (image changed from maclaw-gui:1 to maclaw-gui:2)`、
  `... keeping it as ...:prev`,在 `logs/desktopd.log`(并复制到 journal)。
- 出网上游超时或 502:先看 `logs/desktopd.log` 里的 `[desktopd-proxy] upstream errors` 汇总,或 `curl -H "Authorization: Bearer <key>" http://127.0.0.1:18081/v1/health` 的 `proxy` 计数。同一目标不会再每条失败都刷一行。
