# 内网 Bot 节点：从源码到运行

这份说明给内网 Linux 机器上的 agent。Hub 在公网，已经开着。管理员会给你两样东西：Hub 的地址（下面写成 `HUB_URL`），以及一次性注册码（下面写成 `ENROLL_CODE`）。注册码 10 分钟内有效，用过即废，只属于发码的那个租户。

不要把注册码、`DESKTOPD_TOKEN`、`MACLAW_ADMIN_SECRET`、`node_secret`、`hub_token` 写进 git，也不要发到聊天以外的地方。不要把 18080 或 18081 端口暴露到公网。Hub 不会连进来，这台机器只主动连出。

## 0. 管理员在 Hub 上先做

Hub 进程的环境变量加上 `HUB_EDGE_NODES=on`，然后重启 Hub。没加这个变量时，下面的注册接口不存在，注册会得到 404。

重启会按现有部署规则丢掉正在进行的对话。

管理员登录 Hub 管理台，打开 Bot 管理，在 Docker 服务那一节填写节点名称（不超过 80 个字符），点「生成内网注册码」。把页面上的注册码和 Hub 地址交给这台内网机器上的 agent。注册码不要写入仓库。

## 1. 这台机器要有的东西

- Linux amd64
- 能访问 Docker 的 root（desktopd 要操作 dockerd）
- Git、Go 1.26、openssl、python3、systemd
- 出站能访问 `HUB_URL` 的 443 或 80
- 能拉取 `ghcr.io/rapidai/maclaw-gui:2`。拉不到就在这台机器上按仓库 `desktopd/README.md` 构建本地 tag `maclaw-gui:2`

## 2. 下载源码

```sh
git clone --depth 1 --branch main https://github.com/RapidAI/MaClaw.git /opt/maclaw-src
cd /opt/maclaw-src
```

已经有这份源码时，在仓库根目录执行 `git pull --ff-only`，不要换远程地址。

## 3. 编译

在仓库根目录编译三个二进制。sqlite 用纯 Go 驱动，`CGO_ENABLED=0`。

```sh
export CGO_ENABLED=0
export GOOS=linux
export GOARCH=amd64
go build -ldflags "-s -w" -o /opt/maclaw-node/desktopd ./desktopd/cmd/desktopd
go build -ldflags "-s -w" -o /opt/maclaw-node/maclawsrv ./MaClawSrv
go build -ldflags "-s -w" -o /opt/maclaw-node/botnode ./cmd/botnode
```

`/opt/maclaw-node` 不存在时先 `mkdir -p /opt/maclaw-node /var/lib/maclaw /etc/maclaw`。

## 4. 安装 desktopd

生成只放在这台机器上的令牌。这个值既是 desktopd 的 API 钥匙，也是注册时交给 Hub 的 `desktopd_key`。Hub 用它经隧道调用本机 desktopd。

```sh
umask 077
DESKTOPD_TOKEN=$(openssl rand -hex 32)
printf 'DESKTOPD_TOKEN=%s\n' "$DESKTOPD_TOKEN" > /etc/maclaw/desktopd.env
cat >> /etc/maclaw/desktopd.env <<'EOF'
DESKTOPD_ADDR=127.0.0.1:18081
DESKTOPD_PUBLISH_BIND=127.0.0.1
DESKTOPD_STATE_DIR=/var/lib/maclaw/desktopd
DESKTOPD_INTRANET=1
EOF
chmod 600 /etc/maclaw/desktopd.env
mkdir -p /var/lib/maclaw/desktopd
```

`DESKTOPD_INTRANET=1` 表示这是内网桌面。不要写 `DESKTOPD_ADVERTISE_HOST`，也不要写 `DESKTOPD_PUBLIC_URL`。写了会被当成公网地址；内网桌面的访问地址由 Hub 隧道提供。

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
systemctl daemon-reload
systemctl enable --now maclaw-desktopd
curl -fsS -H "Authorization: Bearer ${DESKTOPD_TOKEN}" http://127.0.0.1:18081/v1/health
```

最后一行应打印包含 `"ok":true` 的 JSON。401 表示令牌和 `.env` 不一致。

## 5. 安装 MaClawSrv

先写出还不含 Hub 令牌的环境。Hub 令牌要等第 6 步注册成功后再写入。

```sh
MACLAW_ADMIN_SECRET=$(openssl rand -hex 32)
MACLAW_TOKEN_SECRET=$(openssl rand -hex 32)
mkdir -p /var/lib/maclaw/srv
cat > /etc/maclaw/maclawsrv.env <<EOF
MACLAW_HTTP_ADDR=127.0.0.1:18080
MACLAW_HUB_URL=${HUB_URL}
MACLAW_ADMIN_SECRET=${MACLAW_ADMIN_SECRET}
MACLAW_TOKEN_SECRET=${MACLAW_TOKEN_SECRET}
MACLAW_DATA_ROOT=/var/lib/maclaw/srv
MACLAW_DESKTOP_API_TOKEN=
EOF
chmod 600 /etc/maclaw/maclawsrv.env
```

把 `HUB_URL` 换成管理员给的地址，不要带路径，不要带末尾斜杠。例如 `https://hub.example.com`。

`MACLAW_ADMIN_SECRET` 至少 24 个字符，`MACLAW_TOKEN_SECRET` 至少 32 个字符。上面的命令各生成 64 个十六进制字符。缺 `MACLAW_TOKEN_SECRET` 时 MaClawSrv 拒绝启动。监听地址是本机回环，不必再设 `MACLAW_ALLOW_INSECURE_HTTP`。

先不要启动 maclawsrv。注册需要这个 admin secret，但服务可以在拿到 `hub_token` 之后再起。

## 6. 注册并保持隧道

`botnode` 用注册码向 Hub 换长期密钥，把结果写入 `/var/lib/maclaw/botnode.json`（权限 600），然后连上 `GET /api/v1/edge/link`。连接是出站 WebSocket。本机是隧道的服务端：Hub 每开一条流，第一行是 `desktopd` 或 `srv`，本机只转发到下面两个地址，其它目标直接关掉。

```sh
cat > /etc/maclaw/botnode.env <<EOF
BOTNODE_HUB_URL=${HUB_URL}
BOTNODE_ENROLL_CODE=${ENROLL_CODE}
BOTNODE_STATE=/var/lib/maclaw/botnode.json
BOTNODE_DESKTOPD_ADDR=127.0.0.1:18081
BOTNODE_SRV_ADDR=127.0.0.1:18080
BOTNODE_DESKTOPD_KEY=${DESKTOPD_TOKEN}
BOTNODE_SRV_ADMIN=${MACLAW_ADMIN_SECRET}
EOF
chmod 600 /etc/maclaw/botnode.env
```

写入 `/etc/systemd/system/maclaw-botnode.service`：

```ini
[Unit]
Description=MaClaw intranet bot node
After=maclaw-desktopd.service

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
systemctl enable --now maclaw-botnode
```

等待 `/var/lib/maclaw/botnode.json` 出现。注册失败时看 `journalctl -u maclaw-botnode`。

- 404 或 `edge nodes are disabled`：Hub 没设 `HUB_EDGE_NODES=on`，或设了但没重启。注册码还没用掉，让管理员改完再启动本服务。
- `enrollment code is invalid`：码过期、输错，或已经用过。让管理员重新生成，改 `BOTNODE_ENROLL_CODE` 后再启动。不要复用旧码。
- `issue a new code`：Hub 没能记下桌面服务。让管理员发新码。

成功之后状态文件里有 `node_id`、`node_secret`、`hub_token`。之后重启 `botnode` 会直接用这三项连隧道，不再读取注册码。

把 `hub_token` 写入 srv 的环境，不要打印到日志以外的终端记录里：

```sh
python3 - <<'PY'
import json
p = "/etc/maclaw/maclawsrv.env"
tok = json.load(open("/var/lib/maclaw/botnode.json"))["hub_token"]
lines = open(p).read().splitlines()
out = []
for line in lines:
    if line.startswith("MACLAW_DESKTOP_API_TOKEN="):
        out.append("MACLAW_DESKTOP_API_TOKEN=" + tok)
    else:
        out.append(line)
open(p, "w").write("\n".join(out) + "\n")
PY
chmod 600 /etc/maclaw/maclawsrv.env
```

## 7. 启动 MaClawSrv

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
systemctl enable --now maclaw-srv
```

`MACLAW_DESKTOP_API_TOKEN` 必须是状态文件里的 `hub_token`，不要填 Hub 的全局桌面 API 令牌。这把 `hub_token` 只能访问发码的那个租户。

## 8. 管理员在 Hub 上收尾

注册成功后，Hub 会在该租户下记一条桌面服务。地址是内部占位名，管理员不要改成公网 URL，也不要清掉节点。在 Bot 管理的「分配」里把需要电脑的用户、部门或全局指到这条服务。

没有分配之前，用户的 bot 不会用到这台内网桌面。

## 9. 确认

在内网机器上：

```sh
systemctl is-active maclaw-desktopd maclaw-srv maclaw-botnode
curl -fsS -H "Authorization: Bearer ${DESKTOPD_TOKEN}" http://127.0.0.1:18081/v1/health
```

`botnode` 的日志里不应出现注册码或令牌。隧道断开后进程会自己重连。Hub 重启后未用完的注册码会失效；已经写入 `botnode.json` 的节点不受影响，等 Hub 起来后隧道会重连。

要换密钥：让管理员发新注册码，停掉 `maclaw-botnode`，删掉 `/var/lib/maclaw/botnode.json`，把新码写入 `BOTNODE_ENROLL_CODE`，再启动。旧密钥随新注册作废。
