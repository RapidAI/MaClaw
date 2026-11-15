# 自装内网桌面

这份说明给桌面客户端发起的一键安装。调用方会给你当前登录的 Hub 地址（下面写成 `HUB_URL`）以及这次的安装选择。`HUB_URL` 不要带路径，不要带末尾斜杠。不要用文档里的示例域名，也不要用别的 Hub。

不要把 `DESKTOPD_TOKEN`、`MACLAW_ADMIN_SECRET`、`MACLAW_TOKEN_SECRET`、`hub_token` 写进 git，也不要发到对话里。不要把 18080 或 18081 暴露到公网。不要安装 botnode，也不要自己向 Hub 注册。客户端在 desktopd 健康之后自己注册。

调用方指定下面两种选择之一。

## 0. 这台机器要有的东西

- Linux x86_64 或 aarch64，systemd
- 能访问 Docker 的 root，或可以 sudo
- git、curl、tar、python3
- Go 1.26 或更新版本。没有就装到 `/usr/local/go`（https://go.dev/dl/ 或 https://golang.google.cn/dl/）
- 出站能访问 `HUB_URL`
- 能得到镜像 `maclaw-gui:2`。拉不到时，下面的安装脚本会在这台机器上构建

`uname -m` 是 aarch64 时，编译用 `GOARCH=arm64`。x86_64 用 `GOARCH=amd64`。

## 1. 源码

```sh
git clone --depth 1 --branch main https://github.com/RapidAI/MaClaw.git /opt/maclaw-src
```

已经有这份源码时，在仓库根目录执行：

```sh
git -C /opt/maclaw-src fetch --depth 1 origin
git -C /opt/maclaw-src reset --hard FETCH_HEAD
```

不要换远程地址。服务器上的代码来自 GitHub 的 main，不包含尚未推送的本地改动。

## 2. 只安装 desktopd

安装目录是 `/data/soft/maclaw_desktopd`，监听 `127.0.0.1:18081`，systemd 服务名 `maclaw-desktopd`。

在仓库根目录打包（`GOPROXY=https://goproxy.cn,direct`）：

```sh
mkdir -p /tmp/maclaw_desktopd_deploy/stage/bin /tmp/maclaw_desktopd_deploy/stage/image
cd /opt/maclaw-src
export CGO_ENABLED=0
export GOOS=linux
/usr/local/go/bin/go build -ldflags "-s -w" -o /tmp/maclaw_desktopd_deploy/stage/bin/desktopd ./desktopd/cmd/desktopd
cp desktopd/image/desktop_supervisor.py desktopd/image/Dockerfile desktopd/image/Dockerfile.v2 desktopd/image/close_range_shim.c desktopd/image/fcitx5-profile /tmp/maclaw_desktopd_deploy/stage/image/
tar -czf /tmp/maclaw_desktopd_deploy/desktopd-deploy.tar.gz -C /tmp/maclaw_desktopd_deploy/stage .
cp desktopd/remote_deploy.sh /tmp/maclaw_desktopd_deploy/
```

`/usr/local/go/bin/go` 不存在、但 `go version` 已是 1.26 或更新时，用 PATH 里的 `go`。

以 root 运行安装脚本。耗时可能很长：

```sh
REMOTE_TMP_DIR=/tmp/maclaw_desktopd_deploy \
DESKTOPD_DEPLOY_DIR=/data/soft/maclaw_desktopd \
DESKTOPD_BIND_ADDR=127.0.0.1:18081 \
DESKTOPD_PORT=18081 \
DESKTOPD_INTRANET=1 \
DESKTOPD_IMAGE_FROM=auto \
sh /tmp/maclaw_desktopd_deploy/remote_deploy.sh
```

不要设置 `DESKTOPD_ADVERTISE_HOST`，也不要设置 `DESKTOPD_PUBLIC_URL`。这是内网桌面，访问地址由 Hub 隧道提供。

若脚本因为缺少 `DESKTOPD_ADVERTISE_HOST` 退出，说明脚本还是旧版。不要填这台机器的 IP 或域名，也不要改 `.env`。只把该变量设成回环地址 `127.0.0.1` 再运行一次，让旧脚本通过。客户端随后会删掉它。

脚本会生成 `/data/soft/maclaw_desktopd/.env`，权限 600。装完后这个文件里要有：

```
DESKTOPD_ADDR=127.0.0.1:18081
DESKTOPD_TOKEN=<脚本生成，不要回显>
DESKTOPD_STATE_DIR=/data/soft/maclaw_desktopd/state
DESKTOPD_INTRANET=1
```

不要留下 `DESKTOPD_ADVERTISE_HOST` 或 `DESKTOPD_PUBLIC_URL`。`DESKTOPD_PUBLISH_BIND` 如果要写，只能是 `127.0.0.1`。

确认：

```sh
systemctl is-active maclaw-desktopd
sh -c '. /data/soft/maclaw_desktopd/.env; curl -fsS -H "Authorization: Bearer $DESKTOPD_TOKEN" http://127.0.0.1:18081/v1/health'
```

最后一行应包含 `"ok":true`。不要把令牌打出来。健康之后停在这里。客户端读取这份 `.env` 并自己注册。

这次选择是「只安装 desktopd」时，不要编译 MaClawSrv，不要做第 3 节。

## 3. 一并安装 MaClawSrv

先做完第 2 节。然后只编译 MaClawSrv，不要创建 .env，不要生成密钥，不要写 systemd 单元，不要启动服务：

```sh
mkdir -p /data/soft/maclaw_srv/bin /data/soft/maclaw_srv/data /data/soft/maclaw_srv/logs
cd /opt/maclaw-src
CGO_ENABLED=0 GOOS=linux /usr/local/go/bin/go build -ldflags "-s -w" -o /data/soft/maclaw_srv/bin/maclawsrv ./MaClawSrv
test -x /data/soft/maclaw_srv/bin/maclawsrv
```

客户端在注册成功后写入 `/data/soft/maclaw_srv/.env`（权限 600）并启动 `maclawsrv.service`。这份配置是：

```
MACLAW_HTTP_ADDR=127.0.0.1:18080
MACLAW_HUB_URL=<HUB_URL>
MACLAW_ADMIN_SECRET=<至少 24 个字符>
MACLAW_TOKEN_SECRET=<至少 32 个字符>
MACLAW_DATA_ROOT=/data/soft/maclaw_srv/data
MACLAW_ALLOW_INSECURE_HTTP=true
MACLAW_DESKTOP_API_TOKEN=<desktopd 注册后得到的 hub_token>
```

`MACLAW_DESKTOP_API_TOKEN` 必须是这次注册签发的 `hub_token`，不要填 Hub 的全局桌面 API 令牌。缺 `MACLAW_TOKEN_SECRET` 时 MaClawSrv 拒绝启动。监听地址是本机回环。

这次选择是「一并安装 desktopd 和 MaClawSrv」时做到 `test -x` 成功即可，不要替客户端写上面的环境文件。

## 4. 更新

这是更新已经在运行的安装，不是新安装。保留：

- `/data/soft/maclaw_desktopd/.env` 里的 `DESKTOPD_TOKEN` 和其他配置
- `/data/soft/maclaw_desktopd/state`（里面有这台 desktopd 自己保存的 Hub 连接）
- 已有桌面容器和用户数据。不要 `docker rm` 它们

按第 1 节取最新源码，再按第 2 节重新打包。安装脚本改用 `DESKTOPD_IMAGE_FROM=build`，并仍然带 `DESKTOPD_INTRANET=1`。不要设置对外地址。脚本会保留已有 `.env`。

健康检查通过之后，才写入调用方给的完成标记。调用方没给标记就不要写。

这台机器已经有 MaClawSrv 时，只重新编译替换 `/data/soft/maclaw_srv/bin/maclawsrv`，保留已有 `.env` 和密钥，不要重新生成，不要重启到一个空配置上。没有 MaClawSrv 时不要顺便装上。

不要重新注册，也不要注销。
