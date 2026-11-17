# 自装内网桌面

这份说明给桌面客户端发起的一键安装。调用方会给你当前登录的 Hub 地址（下面写成 `HUB_URL`）以及这次的安装选择。`HUB_URL` 不要带路径，不要带末尾斜杠。不要用文档里的示例域名，也不要用别的 Hub。

不要把 `DESKTOPD_TOKEN`、`MACLAW_ADMIN_SECRET`、`MACLAW_TOKEN_SECRET`、`hub_token` 写进 git，也不要发到对话里。不要把 18080 或 18081 暴露到公网；18081 只在内网放行，桌面客户端会直连它。不要安装 botnode，也不要自己向 Hub 注册。客户端在 desktopd 健康之后自己注册。

调用方指定下面两种选择之一。

## 0. 这台机器要有的东西

- Linux x86_64 或 aarch64，systemd
- 能访问 Docker 的 root，或可以 sudo
- git、curl、tar。写配置时用 `base64`（coreutils）或 `openssl`，二选一即可
- Go 1.26 或更新版本。没有就装到 `/usr/local/go`（https://go.dev/dl/ 或 https://golang.google.cn/dl/）
- 出站能访问 `HUB_URL`
- 能得到镜像 `maclaw-gui:2`。拉不到时，下面的安装脚本会在这台机器上构建

宿主机不用安装 Python。desktopd 和 MaClawSrv 都是 Go 程序，安装、注册、平时运行都不调用宿主机上的 `python3`。旧系统装不上 Python 3 也照样装这台桌面服务，不要为了安装去装 Python。

桌面里的 Python 在 Docker 镜像 `maclaw-gui:2` 里面，不在宿主机上。镜像里有 `python3` 和 `/desktop_supervisor.py`。打开一个桌面后，容器里用它管画面、浏览器和文档。安装脚本检查「镜像里有没有 python3」时，是 `docker run` 进容器看的。宿主机上 `python3: command not found` 是正常的，不要据此判断安装失败。

`uname -m` 是 aarch64 时，编译用 `GOARCH=arm64`。x86_64 用 `GOARCH=amd64`。

## 1. 源码

用 GitHub 上最新一个 Release 的 tag，不要用 `main`。先读 Release 接口里的 `tag_name`。接口没有这个字段时，再看 `https://github.com/RapidAI/MaClaw/releases/latest` 跳转后的地址，最后一段就是 tag。两个都没有就停。不要在全部 git tag 里按版本号取最后一个：仓库里有比最新 Release 更新、但还没有发布的 tag。tag 保持接口返回的大小写。

```sh
tag=$(curl -fsSL --max-time 20 -H "Accept: application/vnd.github+json" -H "User-Agent: maclaw-desktop-install" https://api.github.com/repos/RapidAI/MaClaw/releases/latest | grep -oE '"tag_name"[[:space:]]*:[[:space:]]*"[^"]*"' | head -n 1 | sed -n 's/.*"\([^"]*\)"$/\1/p')
if [ -z "$tag" ]; then
  rel=$(curl -fsSL --max-time 20 -o /dev/null -w '%{url_effective}' https://github.com/RapidAI/MaClaw/releases/latest || true)
  tag=${rel##*/}
  if [ "$tag" = "latest" ]; then
    tag=""
  fi
fi
if [ -z "$tag" ]; then
  echo "no release tag"
  exit 1
fi
case "$tag" in
  *[!A-Za-z0-9._-]*) echo "bad release tag"; exit 1 ;;
esac
printf 'source tag: %s\n' "$tag"
origin=$(git -c safe.directory=/opt/maclaw-src -C /opt/maclaw-src remote get-url origin 2>/dev/null || true)
case "$origin" in
  https://github.com/RapidAI/MaClaw|https://github.com/RapidAI/MaClaw.git) ;;
  "") ;;
  *) rm -rf /opt/maclaw-src || exit 1 ;;
esac
if [ -d /opt/maclaw-src/.git ]; then
  git -c safe.directory=/opt/maclaw-src -C /opt/maclaw-src fetch --depth 1 origin "+refs/tags/${tag}:refs/tags/${tag}" || exit 1
else
  rm -rf /opt/maclaw-src || exit 1
  git clone --depth 1 --branch "$tag" https://github.com/RapidAI/MaClaw.git /opt/maclaw-src || exit 1
  git -c safe.directory=/opt/maclaw-src -C /opt/maclaw-src fetch --depth 1 origin "+refs/tags/${tag}:refs/tags/${tag}" || exit 1
fi
git -c safe.directory=/opt/maclaw-src -C /opt/maclaw-src reset --hard "refs/tags/${tag}^{}" || exit 1
head=$(git -c safe.directory=/opt/maclaw-src -C /opt/maclaw-src rev-parse HEAD) || exit 1
want=$(git -c safe.directory=/opt/maclaw-src -C /opt/maclaw-src rev-parse "refs/tags/${tag}^{}") || exit 1
if [ "$head" != "$want" ]; then
  echo "checkout is not the release tag"
  exit 1
fi
```

不要换远程地址。目录属主和当前用户不一致时，git 会拒绝更新，所以上面的命令带了 `safe.directory=/opt/maclaw-src`。已有的 tag 用 `+` 强制更新，避免第二次安装被「would clobber existing tag」拦住。`reset` 指向这个 tag 剥开后的提交，不要用 `FETCH_HEAD`：浅克隆里它可能仍是旧的 `main`。核对 `HEAD` 和这个提交一致后再编译。没有 tag 时停下来，不要改去拉 `main`。

编译时加 `-buildvcs=false`。Go 自己会调用 git 读取版本信息，不会带上面的 `safe.directory`，目录属主不一致时编译会失败。

## 2. 只安装 desktopd

安装目录是 `/data/soft/maclaw_desktopd`，监听 `:18081`（所有网卡；内网可达，桌面客户端直连画面和截图就走这个端口），systemd 服务名 `maclaw-desktopd`。

在仓库根目录打包。`GOTOOLCHAIN=local` 用本机的 Go，不要再从网上下载另一套工具链。模块先走 `https://goproxy.cn,direct` 和 `sum.golang.google.cn`，失败再改用 `https://proxy.golang.org,direct` 和 `sum.golang.org`。安装包里的镜像目录是整个 `desktopd/image`，包含 `theme/`。Dockerfile 会 COPY `theme/` 下的文件；只拷贝几个散文件时，主机构建会在 COPY 失败。

```sh
rm -rf /tmp/maclaw_desktopd_deploy/stage || exit 1
mkdir -p /tmp/maclaw_desktopd_deploy/stage/bin /tmp/maclaw_desktopd_deploy/stage/image
cd /opt/maclaw-src || exit 1
if [ -x /usr/local/go/bin/go ]; then
  go_bin=/usr/local/go/bin/go
else
  go_bin=$(command -v go) || { echo "Go 1.26 or newer is required"; exit 1; }
fi
export CGO_ENABLED=0
export GOOS=linux
export GOTOOLCHAIN=local
export GOPROXY=https://goproxy.cn,direct
export GOSUMDB=sum.golang.google.cn
case "$(uname -m)" in
  aarch64|arm64) export GOARCH=arm64 ;;
  x86_64|amd64) export GOARCH=amd64 ;;
  *) echo "unsupported arch: $(uname -m)"; exit 1 ;;
esac
if ! "$go_bin" build -buildvcs=false -ldflags "-s -w" -o /tmp/maclaw_desktopd_deploy/stage/bin/desktopd ./desktopd/cmd/desktopd; then
  export GOPROXY=https://proxy.golang.org,direct
  export GOSUMDB=sum.golang.org
  "$go_bin" build -buildvcs=false -ldflags "-s -w" -o /tmp/maclaw_desktopd_deploy/stage/bin/desktopd ./desktopd/cmd/desktopd || exit 1
fi
cp -R desktopd/image/. /tmp/maclaw_desktopd_deploy/stage/image/
find /tmp/maclaw_desktopd_deploy/stage/image -type d -name __pycache__ -exec rm -rf {} + || true
tar -czf /tmp/maclaw_desktopd_deploy/desktopd-deploy.tar.gz -C /tmp/maclaw_desktopd_deploy/stage .
cp desktopd/remote_deploy.sh /tmp/maclaw_desktopd_deploy/
```

`/usr/local/go/bin/go` 不存在、但 PATH 里的 `go version` 已是 1.26 或更新时，上面的脚本会改用 PATH 里的 `go`。

以 root 运行安装脚本。耗时可能很长：

```sh
REMOTE_TMP_DIR=/tmp/maclaw_desktopd_deploy \
DESKTOPD_DEPLOY_DIR=/data/soft/maclaw_desktopd \
DESKTOPD_BIND_ADDR=:18081 \
DESKTOPD_PORT=18081 \
DESKTOPD_INTRANET=1 \
DESKTOPD_IMAGE_FROM=auto \
sh /tmp/maclaw_desktopd_deploy/remote_deploy.sh
```

不要设置 `DESKTOPD_ADVERTISE_HOST`，也不要设置 `DESKTOPD_PUBLIC_URL`。这是内网桌面，访问地址由 Hub 隧道提供。

若脚本因为缺少 `DESKTOPD_ADVERTISE_HOST` 退出，说明脚本还是旧版。不要填这台机器的 IP 或域名，也不要改 `.env`。只把该变量设成回环地址 `127.0.0.1` 再运行一次，让旧脚本通过。客户端随后会删掉它。

脚本会生成 `/data/soft/maclaw_desktopd/.env`，权限 600。装完后这个文件里要有：

```
DESKTOPD_ADDR=:18081
DESKTOPD_TOKEN=<脚本生成，不要回显>
DESKTOPD_STATE_DIR=/data/soft/maclaw_desktopd/state
DESKTOPD_INTRANET=1
```

不要留下 `DESKTOPD_ADVERTISE_HOST` 或 `DESKTOPD_PUBLIC_URL`。`DESKTOPD_PUBLISH_BIND` 如果要写，只能是 `127.0.0.1`。

防火墙放行至内网：18081 是桌面客户端直连 desktopd 的端口（屏幕画面、截图和文件直连都走它），不要把这个端口映射到公网。

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
cd /opt/maclaw-src || exit 1
if [ -x /usr/local/go/bin/go ]; then
  go_bin=/usr/local/go/bin/go
else
  go_bin=$(command -v go) || { echo "Go 1.26 or newer is required"; exit 1; }
fi
export CGO_ENABLED=0
export GOOS=linux
export GOTOOLCHAIN=local
export GOPROXY=https://goproxy.cn,direct
export GOSUMDB=sum.golang.google.cn
case "$(uname -m)" in
  aarch64|arm64) export GOARCH=arm64 ;;
  x86_64|amd64) export GOARCH=amd64 ;;
  *) echo "unsupported arch: $(uname -m)"; exit 1 ;;
esac
if ! "$go_bin" build -buildvcs=false -ldflags "-s -w" -o /data/soft/maclaw_srv/bin/maclawsrv ./MaClawSrv; then
  export GOPROXY=https://proxy.golang.org,direct
  export GOSUMDB=sum.golang.org
  "$go_bin" build -buildvcs=false -ldflags "-s -w" -o /data/soft/maclaw_srv/bin/maclawsrv ./MaClawSrv || exit 1
fi
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

要让桌面客户端直连这台桌面，`.env` 里的 `DESKTOPD_ADDR` 不能是回环地址：旧安装写的 `127.0.0.1:18081` 要改成 `:18081`，再 `systemctl restart maclaw-desktopd`，并在防火墙对内网放行 18081。

这台机器已经有 MaClawSrv 时，只重新编译替换 `/data/soft/maclaw_srv/bin/maclawsrv`，保留已有 `.env` 和密钥，不要重新生成，不要重启到一个空配置上。没有 MaClawSrv 时不要顺便装上。

不要重新注册，也不要注销。
