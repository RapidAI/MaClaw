# 内网 Bot 节点：从源码到运行

这份说明给内网 Linux 机器上的 agent。Hub 在公网，已经开着。调用方给你三样东西：Hub 地址（下面写成 `HUB_URL`）、这次安装 desktopd 还是连 MaClawSrv 一起装，以及一次性注册码（下面写成 `ENROLL_CODE`）。

`HUB_URL` 不要带路径，不要带末尾斜杠。例如 `https://hub.example.com`。不要用文档里的示例域名。

注册码由 Hub 管理员在管理台发给调用方，再交给你。不要自己向 Hub 申请注册码。码属于发码的那个租户，10 分钟内有效，用过即废。Hub 重启后，还没用掉的码也会失效。

这台机器给多个用户共用。不要把它登记成某一个人的内网桌面。

不要把注册码、`DESKTOPD_TOKEN`、`MACLAW_ADMIN_SECRET`、`MACLAW_TOKEN_SECRET`、`node_secret`、`hub_token` 写进 git，也不要在回复里复述。不要把 18080 或 18081 暴露到公网。Hub 不会连进来，这台机器只主动连出。

调用方指定下面两种选择之一。两种都要安装并注册 botnode。

- **同时安装 MaClawSrv 和 desktopd**：`WITH_SRV=1`。编译并安装 desktopd、MaClawSrv、botnode。desktopd 和 botnode 的步骤都要做，另外做第 7 节和第 9 节。
- **只安装 desktopd**：`WITH_SRV=0`。编译并安装 desktopd 和 botnode。不要编译、不要配置、不要启动 MaClawSrv。跳过第 7 节和第 9 节。`BOTNODE_SRV_ADDR=off`。

## 0. 权限

写 `/etc`、`/opt`、systemd，以及访问 Docker 套接字，都要 root。登录用户不是 root 时：调用方说明可以 sudo，就用 sudo，密码用登录密码。调用方说明不能 sudo，就停下来。

下面命令按 root 来写。换了一条 shell 之后，变量就没了。每条新 shell 先重新赋值 `WITH_SRV`、`HUB_URL`、`ENROLL_CODE`，不要 echo。`DESKTOPD_TOKEN` 和 `MACLAW_ADMIN_SECRET` 从文件读，不要打印。

## 1. Hub 要先开着内网节点

用仓库里的 `start.sh` 或 `run-systemd.sh` 启动时，环境里没写 `HUB_EDGE_NODES` 会默认打开。`edge.env` 里写成 `off` 才会关掉。

没走这两个脚本、进程环境里也没有 `HUB_EDGE_NODES=on`（或 `1` / `true` / `yes`）时，注册接口不存在。botnode 看到的是普通 404，不是 JSON。注册码还没用掉。改完环境要重启 Hub 进程。重启会丢掉当时还在进行的对话。

先不要点「生成内网注册码」。编译经常超过 10 分钟，码会在 desktopd 起来之前过期。

## 2. 这台机器要有的东西

- Linux x86_64 或 aarch64，systemd
- Docker，并且当前用户是 root 或可以 sudo
- Git、curl、Go 1.26 或更新、openssl、python3
- 出站能访问 `HUB_URL`

`uname -m` 是 aarch64 或 arm64 时，编译用 `GOARCH=arm64`。x86_64 或 amd64 用 `GOARCH=amd64`。

没有 Go 1.26 时，第 4 节会安装官方 Go 1.26.0 到 `/usr/local/go`。不要用发行版的 `golang` 包，那个版本不够。`go version` 已经是 1.26 或更新时，用 PATH 里的 `go`。

安装时不要构建镜像，源码还没下载。本机没有 `maclaw-gui:2` 时，desktopd 第一次建桌面会从 `ghcr.io/rapidai/maclaw-gui:2` 拉取并打上本地 tag。只有那次拉取失败，才执行第 3 节末尾的构建，而且两条里只跑一条。

## 3. 下载源码

远程是 `https://github.com/RapidAI/MaClaw.git` 时只更新。半截目录或别的远程先清掉再 clone。只动 `/opt/maclaw-src`。

```sh
origin=$(git -C /opt/maclaw-src remote get-url origin 2>/dev/null || true)
case "$origin" in
  https://github.com/RapidAI/MaClaw|https://github.com/RapidAI/MaClaw.git) ;;
  *) rm -rf /opt/maclaw-src ;;
esac
if [ -d /opt/maclaw-src/.git ]; then
  git -C /opt/maclaw-src fetch --depth 1 origin main || exit 1
  git -C /opt/maclaw-src reset --hard FETCH_HEAD || exit 1
else
  rm -rf /opt/maclaw-src
  git clone --depth 1 --branch main https://github.com/RapidAI/MaClaw.git /opt/maclaw-src || exit 1
fi
```

不要换远程地址。浅克隆不要用 `git pull`。

安装过程到这里就停，不要构建镜像。只有以后建桌面时拉不到 `ghcr.io/rapidai/maclaw-gui:2`，才执行下面两条里的一条，不要两条都跑：

```sh
docker build -f /opt/maclaw-src/desktopd/image/Dockerfile.v2 -t maclaw-gui:2 /opt/maclaw-src/desktopd/image
```

基础镜像也拉不到时，改用这一条：

```sh
docker build -f /opt/maclaw-src/desktopd/image/Dockerfile.v2 --build-arg BASE_IMAGE=mirror.ccs.tencentyun.com/library/debian:bookworm --build-arg APT_MIRROR=mirrors.tencentyun.com -t maclaw-gui:2 /opt/maclaw-src/desktopd/image
```

## 4. 编译

先建目录，再编译。输出路径的父目录不会自动创建。sqlite 用纯 Go 驱动，`CGO_ENABLED=0`。

```sh
mkdir -p /opt/maclaw-node /var/lib/maclaw/desktopd /var/lib/maclaw/srv /etc/maclaw
cd /opt/maclaw-src || exit 1
case "${WITH_SRV}" in 1|0) ;; *) echo "set WITH_SRV=1 or WITH_SRV=0"; exit 1 ;; esac
go_ok() { go version 2>/dev/null | grep -Eq 'go1\.(2[6-9]|[3-9][0-9])'; }
if ! go_ok; then
  export PATH="/usr/local/go/bin:${PATH}"
fi
if ! go_ok; then
  command -v curl >/dev/null || { echo "curl is required to install Go"; exit 1; }
  case "$(uname -m)" in
    aarch64|arm64) goarch=arm64 ;;
    x86_64|amd64) goarch=amd64 ;;
    *) echo "unsupported arch: $(uname -m)"; exit 1 ;;
  esac
  tmp=$(mktemp) || exit 1
  if ! curl -fsSL --connect-timeout 15 --max-time 300 -o "$tmp" "https://go.dev/dl/go1.26.0.linux-${goarch}.tar.gz" \
    && ! curl -fsSL --connect-timeout 15 --max-time 300 -o "$tmp" "https://golang.google.cn/dl/go1.26.0.linux-${goarch}.tar.gz"; then
    rm -f "$tmp"
    exit 1
  fi
  rm -rf /usr/local/go
  tar -C /usr/local -xzf "$tmp" || exit 1
  rm -f "$tmp"
  export PATH="/usr/local/go/bin:${PATH}"
fi
if ! go_ok; then
  echo "Go 1.26 or newer is required"
  exit 1
fi
export CGO_ENABLED=0
export GOOS=linux
export GOTOOLCHAIN=local
export GOPROXY=https://goproxy.cn,direct
case "$(uname -m)" in
  aarch64|arm64) export GOARCH=arm64 ;;
  x86_64|amd64) export GOARCH=amd64 ;;
  *) echo "unsupported arch: $(uname -m)"; exit 1 ;;
esac
go build -ldflags "-s -w" -o /opt/maclaw-node/desktopd ./desktopd/cmd/desktopd || exit 1
go build -ldflags "-s -w" -o /opt/maclaw-node/botnode ./cmd/botnode || exit 1
if [ "$WITH_SRV" = "1" ]; then
  go build -ldflags "-s -w" -o /opt/maclaw-node/maclawsrv ./MaClawSrv || exit 1
fi
```

`GOTOOLCHAIN=local` 让这次编译用刚装上的 Go，不要再从网上下载另一套工具链。模块下不来时，把 `GOPROXY` 改成 `https://proxy.golang.org,direct` 再执行这一节。

## 5. 安装 desktopd

生成只放在这台机器上的令牌。这个值既是 desktopd 的 API 钥匙，也是注册时交给 Hub 的 `desktopd_key`。Hub 用它经隧道调用本机 desktopd。

文件里已经有 `DESKTOPD_TOKEN` 时沿用同一把，不要另生成。换钥匙只走最后一节。

```sh
umask 077
DESKTOPD_TOKEN=
if [ -f /etc/maclaw/desktopd.env ]; then
  DESKTOPD_TOKEN=$(sed -n 's/^DESKTOPD_TOKEN=//p' /etc/maclaw/desktopd.env | tr -d '[:space:]')
fi
if [ -z "$DESKTOPD_TOKEN" ]; then
  DESKTOPD_TOKEN=$(openssl rand -hex 32) || exit 1
fi
if [ -z "$DESKTOPD_TOKEN" ]; then
  echo "DESKTOPD_TOKEN is empty"
  exit 1
fi
printf 'DESKTOPD_TOKEN=%s\n' "$DESKTOPD_TOKEN" > /etc/maclaw/desktopd.env
cat >> /etc/maclaw/desktopd.env <<'EOF'
DESKTOPD_ADDR=127.0.0.1:18081
DESKTOPD_PUBLISH_BIND=127.0.0.1
DESKTOPD_STATE_DIR=/var/lib/maclaw/desktopd
DESKTOPD_INTRANET=1
DESKTOPD_HUB_LINK=off
EOF
chmod 600 /etc/maclaw/desktopd.env
```

`DESKTOPD_INTRANET=1` 表示这是内网桌面。`DESKTOPD_HUB_LINK=off` 关掉 desktopd 自己的 Hub 连接，隧道只由 botnode 建立。不关的话，desktopd 会一直等自己的注册，桌面建不出来。已经在跑的进程要 `systemctl restart maclaw-desktopd` 才会读到这一行。开会话若仍报 `waiting for its hub link`，编出来的是旧源码：更新 `/opt/maclaw-src` 后重新编译 desktopd 再重启。不要写 `DESKTOPD_ADVERTISE_HOST`，也不要写 `DESKTOPD_PUBLIC_URL`。不要给 desktopd 配 Hub 地址或注册码。写了对外地址会被当成公网；内网桌面的访问地址由 Hub 隧道提供。

写入 `/etc/systemd/system/maclaw-desktopd.service`：

```ini
[Unit]
Description=MaClaw desktopd
After=docker.service
Requires=docker.service

[Service]
EnvironmentFile=/etc/maclaw/desktopd.env
ExecStart=/opt/maclaw-node/desktopd
Restart=on-failure
WorkingDirectory=/var/lib/maclaw/desktopd

[Install]
WantedBy=multi-user.target
```

```sh
systemctl enable --now docker || exit 1
systemctl daemon-reload
systemctl enable maclaw-desktopd || exit 1
systemctl reset-failed maclaw-desktopd || true
if ! systemctl restart maclaw-desktopd; then
  systemctl stop maclaw-desktopd
  echo "maclaw-desktopd did not start; see journalctl -u maclaw-desktopd"
  exit 1
fi
command -v curl >/dev/null || { echo "curl is required"; exit 1; }
DESKTOPD_TOKEN=$(sed -n 's/^DESKTOPD_TOKEN=//p' /etc/maclaw/desktopd.env | tr -d '[:space:]')
i=0
until curl -fsS --max-time 2 -H "Authorization: Bearer ${DESKTOPD_TOKEN}" http://127.0.0.1:18081/v1/health; do
  i=$((i+1))
  if [ "$i" -ge 15 ]; then
    echo "desktopd health check failed; see journalctl -u maclaw-desktopd"
    exit 1
  fi
  sleep 1
done
```

最后一行应打印包含 `"ok":true` 的 JSON。401 表示令牌和 `.env` 不一致。健康检查没通过之前，不要写注册码，也不要启动 botnode。

## 6. 这时再要注册码

desktopd 健康检查通过之后再发注册码。管理员登录 Hub 管理台，打开 Bot 管理，在「Bot 节点」那一节填写节点名称（不超过 80 个字符），点「生成内网注册码」。页面上会写：把注册码交给内网 agent，10 分钟内有效，只能用一次。码是 16 位十六进制，大小写都行。

按钮若提示「这个租户还不能自建节点」或「这个租户的节点数已到上限」，先让管理员改限额。不要反复点。换密钥不受这个上限限制。

调用方如果在编译前已经拿到码，等健康检查通过后立刻用。启动 botnode 时码已经过期、输错或用过，就停下来，向管理员要新码。不要用旧码反复重试。

`/var/lib/maclaw/botnode.json` 里已经有 `node_id` 时，botnode 不再读取注册码。这不是新装。不要删这个文件，除非管理员明确要换密钥（见最后一节）。

## 7. 安装 MaClawSrv 的环境

只安装 desktopd 时跳过这一节。

先写出环境。Hub 令牌要等注册成功后再由第 9 节写入。先不要启动 maclawsrv。文件里已经有管理员密钥、签名密钥或 `MACLAW_DESKTOP_API_TOKEN` 时沿用，不要另生成，也不要清掉令牌。换了管理员密钥而 botnode 环境里还是旧的，隧道上的管理调用会对不上。

```sh
umask 077
read_env() {
  if [ -f "$1" ]; then
    sed -n "s/^${2}=//p" "$1" | tr -d '[:space:]'
  fi
  return 0
}
HUB_URL=$(printf '%s' "$HUB_URL" | tr -d '[:space:]')
if [ -z "$HUB_URL" ]; then echo "HUB_URL is empty"; exit 1; fi
MACLAW_ADMIN_SECRET=$(read_env /etc/maclaw/maclawsrv.env MACLAW_ADMIN_SECRET)
MACLAW_TOKEN_SECRET=$(read_env /etc/maclaw/maclawsrv.env MACLAW_TOKEN_SECRET)
MACLAW_DESKTOP_API_TOKEN=$(read_env /etc/maclaw/maclawsrv.env MACLAW_DESKTOP_API_TOKEN)
if [ -z "$MACLAW_ADMIN_SECRET" ]; then
  MACLAW_ADMIN_SECRET=$(openssl rand -hex 32) || exit 1
fi
if [ -z "$MACLAW_TOKEN_SECRET" ]; then
  MACLAW_TOKEN_SECRET=$(openssl rand -hex 32) || exit 1
fi
if [ -z "$MACLAW_ADMIN_SECRET" ] || [ -z "$MACLAW_TOKEN_SECRET" ]; then
  echo "openssl did not produce MaClawSrv secrets"
  exit 1
fi
mkdir -p /var/lib/maclaw/srv
cat > /etc/maclaw/maclawsrv.env <<EOF
MACLAW_HTTP_ADDR=127.0.0.1:18080
MACLAW_HUB_URL=${HUB_URL}
MACLAW_ADMIN_SECRET=${MACLAW_ADMIN_SECRET}
MACLAW_TOKEN_SECRET=${MACLAW_TOKEN_SECRET}
MACLAW_DATA_ROOT=/var/lib/maclaw/srv
MACLAW_DESKTOP_API_TOKEN=${MACLAW_DESKTOP_API_TOKEN}
EOF
chmod 600 /etc/maclaw/maclawsrv.env
```

`MACLAW_ADMIN_SECRET` 至少 24 个字符，`MACLAW_TOKEN_SECRET` 至少 32 个字符。上面缺哪把才生成，各 64 个十六进制字符。缺 `MACLAW_TOKEN_SECRET` 时 MaClawSrv 拒绝启动。监听地址是本机回环，不必再设 `MACLAW_ALLOW_INSECURE_HTTP`。

## 8. 注册并保持隧道

`botnode` 用注册码向 Hub 换长期密钥，把结果写入 `/var/lib/maclaw/botnode.json`（权限 600），然后连上 `GET /api/v1/edge/link`。连接是出站 WebSocket。本机是隧道的服务端：Hub 每开一条流，第一行是 `desktopd` 或 `srv`，本机只转发到配置的本机地址，其它目标直接关掉。

空的 `BOTNODE_SRV_ADDR` 会被当成 `127.0.0.1:18080`。只安装 desktopd 时必须写成 `off`（或 `-`），并且不要写 `BOTNODE_SRV_ADMIN`。

先确认这同一条 shell 里 `WITH_SRV`、`HUB_URL`、`ENROLL_CODE` 已经赋值。不要 echo。下面只写一次 `botnode.env`：`WITH_SRV=1` 带上 MaClawSrv，否则 `BOTNODE_SRV_ADDR=off`。不要再写第二份把前一份盖掉。

```sh
umask 077
case "${WITH_SRV}" in 1|0) ;; *) echo "set WITH_SRV=1 or WITH_SRV=0"; exit 1 ;; esac
HUB_URL=$(printf '%s' "$HUB_URL" | tr -d '[:space:]')
ENROLL_CODE=$(printf '%s' "$ENROLL_CODE" | tr -d '[:space:]')
DESKTOPD_TOKEN=
if [ -f /etc/maclaw/desktopd.env ]; then
  DESKTOPD_TOKEN=$(sed -n 's/^DESKTOPD_TOKEN=//p' /etc/maclaw/desktopd.env | tr -d '[:space:]')
fi
if [ -z "$HUB_URL" ] || [ -z "$ENROLL_CODE" ] || [ -z "$DESKTOPD_TOKEN" ]; then
  echo "HUB_URL, ENROLL_CODE, or DESKTOPD_TOKEN is empty"
  exit 1
fi
if [ "$WITH_SRV" = "1" ]; then
  MACLAW_ADMIN_SECRET=
  if [ -f /etc/maclaw/maclawsrv.env ]; then
    MACLAW_ADMIN_SECRET=$(sed -n 's/^MACLAW_ADMIN_SECRET=//p' /etc/maclaw/maclawsrv.env | tr -d '[:space:]')
  fi
  if [ -z "$MACLAW_ADMIN_SECRET" ]; then
    echo "maclawsrv.env has no MACLAW_ADMIN_SECRET"
    exit 1
  fi
  cat > /etc/maclaw/botnode.env <<EOF
BOTNODE_HUB_URL=${HUB_URL}
BOTNODE_ENROLL_CODE=${ENROLL_CODE}
BOTNODE_STATE=/var/lib/maclaw/botnode.json
BOTNODE_DESKTOPD_ADDR=127.0.0.1:18081
BOTNODE_SRV_ADDR=127.0.0.1:18080
BOTNODE_DESKTOPD_KEY=${DESKTOPD_TOKEN}
BOTNODE_SRV_ADMIN=${MACLAW_ADMIN_SECRET}
EOF
else
  cat > /etc/maclaw/botnode.env <<EOF
BOTNODE_HUB_URL=${HUB_URL}
BOTNODE_ENROLL_CODE=${ENROLL_CODE}
BOTNODE_STATE=/var/lib/maclaw/botnode.json
BOTNODE_DESKTOPD_ADDR=127.0.0.1:18081
BOTNODE_SRV_ADDR=off
BOTNODE_DESKTOPD_KEY=${DESKTOPD_TOKEN}
EOF
fi
chmod 600 /etc/maclaw/botnode.env
```

写入 `/etc/systemd/system/maclaw-botnode.service`：

```ini
[Unit]
Description=MaClaw intranet bot node
After=maclaw-desktopd.service
StartLimitIntervalSec=0

[Service]
EnvironmentFile=/etc/maclaw/botnode.env
ExecStart=/opt/maclaw-node/botnode
Restart=always
RestartSec=3

[Install]
WantedBy=multi-user.target
```

```sh
systemctl daemon-reload
systemctl enable maclaw-botnode || exit 1
systemctl reset-failed maclaw-botnode || true
if ! systemctl restart maclaw-botnode; then
  systemctl stop maclaw-botnode
  echo "maclaw-botnode did not start; see journalctl -u maclaw-botnode"
  exit 1
fi
i=0
until grep -Eq '"node_id"[[:space:]]*:[[:space:]]*"[^"]+"' /var/lib/maclaw/botnode.json 2>/dev/null; do
  i=$((i+1))
  if [ "$i" -ge 30 ]; then
    systemctl stop maclaw-botnode
    echo "botnode did not enroll; see journalctl -u maclaw-botnode"
    exit 1
  fi
  sleep 2
done
```

`restart` 返回时注册往往还没写完。上面等到状态文件里有 `node_id`，最多约一分钟。超时会停掉服务，避免用旧码一直重试。失败时看 `journalctl -u maclaw-botnode`。不要把日志里的密钥贴出来。

单元是 `Restart=always`，并且关掉了启动次数限制，所以失败会一直重试。改环境前先 `systemctl stop maclaw-botnode`，改完后执行 `systemctl reset-failed maclaw-botnode`，再 `systemctl restart maclaw-botnode`。只改文件不会换掉进程已经读入的码；不停的话会用旧码一直重试。没清掉失败状态时，systemd 会拒绝再次启动。

- 404 或 `edge nodes are disabled`：Hub 没开内网节点，或开了但没重启。注册码还没用掉。让管理员改完再启动本服务。
- `enrollment code is invalid`：码过期、输错，或已经用过。让管理员重新生成，改 `BOTNODE_ENROLL_CODE` 后再启动。不要复用旧码。
- `issue a new code`：Hub 没能记下桌面服务，这张码已经用掉。让管理员再发一张新码。
- `这个租户的节点数已到上限` 或 `这个租户还不能自建节点`：码还没用掉。让管理员改限额后再启动。

成功之后状态文件里有 `node_id`、`node_secret`、`hub_token`，以及 `hub_url`。之后重启 botnode 会直接用已保存的密钥连隧道，不再读取注册码。`BOTNODE_HUB_URL` 必须和状态文件里的 `hub_url` 一致。

## 9. 把 Hub 令牌交给 MaClawSrv 并启动

只安装 desktopd 时跳过这一节。

把 `hub_token` 写入 srv 的环境。先收紧新建文件的权限。这个脚本不把令牌打到终端：

```sh
umask 077
command -v python3 >/dev/null || { echo "python3 is required"; exit 1; }
python3 - <<'PY'
import json, os
p = "/etc/maclaw/maclawsrv.env"
tok = json.load(open("/var/lib/maclaw/botnode.json")).get("hub_token") or ""
if not tok:
    raise SystemExit(1)
with open(p) as src:
    lines = src.read().splitlines()
out = []
found = False
for line in lines:
    if line.startswith("MACLAW_DESKTOP_API_TOKEN="):
        out.append("MACLAW_DESKTOP_API_TOKEN=" + tok)
        found = True
    else:
        out.append(line)
if not found:
    out.append("MACLAW_DESKTOP_API_TOKEN=" + tok)
tmp = p + ".tmp"
with open(tmp, "w") as dst:
    dst.write("\n".join(out) + "\n")
os.chmod(tmp, 0o600)
os.replace(tmp, p)
PY
```

`MACLAW_DESKTOP_API_TOKEN` 必须是状态文件里的 `hub_token`，不要填 Hub 的全局桌面 API 令牌。这把 `hub_token` 只能访问发码的那个租户。上面的写入完成之前不要启动 maclaw-srv。

写入 `/etc/systemd/system/maclaw-srv.service`：

```ini
[Unit]
Description=MaClaw srv
After=network-online.target

[Service]
EnvironmentFile=/etc/maclaw/maclawsrv.env
ExecStart=/opt/maclaw-node/maclawsrv
Restart=on-failure
WorkingDirectory=/var/lib/maclaw

[Install]
WantedBy=multi-user.target
```

```sh
systemctl daemon-reload
systemctl enable maclaw-srv || exit 1
systemctl reset-failed maclaw-srv || true
if ! systemctl restart maclaw-srv; then
  systemctl stop maclaw-srv
  echo "maclaw-srv did not start; see journalctl -u maclaw-srv"
  exit 1
fi
```

## 10. 管理员在 Hub 上收尾

注册成功后，Hub 会在该租户下记一条桌面服务。地址是内部占位名 `http://desktopd.<节点 id>.node.invalid`。管理员不要改成公网 URL，也不要清掉节点。在 Bot 管理的「分配」里把需要电脑的用户、部门或全局指到这条服务。

没有分配之前，用户的 bot 不会用到这台内网桌面。

## 11. 确认

在内网机器上。健康检查从文件读令牌：

```sh
DESKTOPD_TOKEN=$(sed -n 's/^DESKTOPD_TOKEN=//p' /etc/maclaw/desktopd.env)
curl -fsS -H "Authorization: Bearer ${DESKTOPD_TOKEN}" http://127.0.0.1:18081/v1/health
```

同时安装了 MaClawSrv 时：

```sh
systemctl is-active maclaw-desktopd maclaw-srv maclaw-botnode
```

只安装了 desktopd 时，不要启用 `maclaw-srv`：

```sh
systemctl is-active maclaw-desktopd maclaw-botnode
```

确认状态文件里有节点 id，且不打印密钥。通过后删掉环境文件里的一次性注册码，再重启 botnode。状态文件还在时，重启不会再读注册码：

```sh
grep -Eq '"node_id"[[:space:]]*:[[:space:]]*"[^"]+"' /var/lib/maclaw/botnode.json || exit 1
sed -i '/^BOTNODE_ENROLL_CODE=/d' /etc/maclaw/botnode.env || exit 1
systemctl reset-failed maclaw-botnode || true
systemctl restart maclaw-botnode || exit 1
```

同时安装了 MaClawSrv 时，再确认令牌已经写入、且不打印它：

```sh
python3 -c 'import pathlib,sys; ok=any(l.startswith("MACLAW_DESKTOP_API_TOKEN=") and l.split("=",1)[1].strip() for l in pathlib.Path("/etc/maclaw/maclawsrv.env").read_text().splitlines()); sys.exit(0 if ok else 1)'
```

`botnode` 的日志里不应出现注册码或令牌。隧道断开后进程会自己重连。已经写入 `botnode.json` 的节点不受 Hub 重启影响，等 Hub 起来后隧道会重连。

## 要换密钥

找到这台已经在线的节点，点它旁边的「轮换密钥」。不要点「生成内网注册码」：那会再登记一台新节点，也不会作废这台的旧密钥。

停掉 `maclaw-botnode`，删掉 `/var/lib/maclaw/botnode.json`。确认步骤可能已经删掉 `BOTNODE_ENROLL_CODE` 这一行，所以先去掉旧行再写上轮换码，不要把码打到终端：

```sh
ENROLL_CODE=$(printf '%s' "$ENROLL_CODE" | tr -d '[:space:]')
if [ -z "$ENROLL_CODE" ]; then echo "ENROLL_CODE is empty"; exit 1; fi
systemctl stop maclaw-botnode || exit 1
rm -f /var/lib/maclaw/botnode.json || exit 1
sed -i '/^BOTNODE_ENROLL_CODE=/d' /etc/maclaw/botnode.env || exit 1
printf 'BOTNODE_ENROLL_CODE=%s\n' "$ENROLL_CODE" >> /etc/maclaw/botnode.env || exit 1
chmod 600 /etc/maclaw/botnode.env || exit 1
systemctl reset-failed maclaw-botnode || true
systemctl start maclaw-botnode || exit 1
```

轮换成功后节点 id 不变，旧密钥作废。上面先删掉状态文件，botnode 才会读这次的轮换码。

同时装了 MaClawSrv 时，轮换会换掉 `hub_token`，旧令牌马上失效。再执行第 9 节的写入，然后 `systemctl restart maclaw-srv`。

轮换失败并出现 `issue a new code` 时，再点一次这台节点的「轮换密钥」。不要改去点「生成内网注册码」。
