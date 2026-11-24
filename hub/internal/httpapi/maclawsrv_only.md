# 私有云端服务：只安装 MaClawSrv（不装桌面）

这份说明给内网 Linux 机器上的 agent。调用方是 MaClaw 桌面端的「安装私有云端服务」。目标是在这台服务器上装好 MaClawSrv（云端任务的运行时）和 botnode（到 Hub 的隧道），让这个用户的云端任务跑在这台机器上。

**这次不安装 desktopd。** 不要安装 Docker，不要构建或拉取桌面镜像 `maclaw-gui:2`，不要创建 `/var/lib/maclaw/desktopd`，不要写 `/etc/maclaw/desktopd.env`，不要装 `maclaw-desktopd` 服务。这台机器只跑云任务，不提供内网桌面。

调用方给你两样东西：Hub 地址（下面写成 `HUB_URL`）和一次性注册码（下面写成 `ENROLL_CODE`）。`HUB_URL` 不要带路径，不要带末尾斜杠，例如 `https://hub.example.com`。不要用文档里的示例域名。

注册码由桌面端向 Hub 申请（属于当前登录的用户），3 小时内有效，用过即废。不要自己向 Hub 申请注册码。不要把注册码、`MACLAW_ADMIN_SECRET`、`MACLAW_TOKEN_SECRET`、`node_secret`、`hub_token` 写进 git，也不要在回复里复述。不要把 18080 暴露到公网。Hub 不会连进来，这台机器只主动连出。

这台节点属于发码的用户个人，不是共用机器；不要把它登记给别人。

## 0. 权限

写 `/etc`、`/opt`、systemd 都要 root。登录用户不是 root 时：调用方说明可以 sudo，就用 sudo，密码用登录密码。调用方说明不能 sudo，就停下来。

下面命令按 root 来写。换了一条 shell 之后，变量就没了。每条新 shell 先重新赋值 `HUB_URL`、`ENROLL_CODE`，不要 echo。`MACLAW_ADMIN_SECRET`、`hub_token` 从文件读，不要打印。

## 1. Hub 要先开着内网节点

用仓库里的 `start.sh` 或 `run-systemd.sh` 启动时，环境里没写 `HUB_EDGE_NODES` 会默认打开。`edge.env` 里写成 `off` 才会关掉。

没开时，注册接口不存在，botnode 看到普通 404。注册码还没用掉。改完环境要重启 Hub 进程。注册码 3 小时内有效；超过 3 小时还没用掉，就向调用方要一张新码，不要用旧码重试。

## 2. 这台机器要有的东西

- Linux x86_64 或 aarch64，systemd
- Git、curl、Go 1.26 或更新、openssl。写配置时用 `base64`（coreutils）或 `openssl`，二选一即可
- 出站能访问 `HUB_URL`

不需要 Docker。MaClawSrv 和 botnode 都是纯 Go 程序，编译、安装、注册、平时运行都不调用宿主机上的 `python3`。宿主机上 `python3: command not found` 是正常的。

`uname -m` 是 aarch64 或 arm64 时，编译用 `GOARCH=arm64`。x86_64 或 amd64 用 `GOARCH=amd64`。

没有 Go 1.26 时，下一节会安装官方 Go 1.26.0 到 `/usr/local/go`。不要用发行版的 `golang` 包，那个版本不够。

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

## 4. 编译

只编译 `maclawsrv` 和 `botnode` 两个二进制。不要编译 desktopd。sqlite 用纯 Go 驱动，`CGO_ENABLED=0`。

```sh
mkdir -p /opt/maclaw-node /var/lib/maclaw/srv /etc/maclaw
cd /opt/maclaw-src || exit 1
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
go build -ldflags "-s -w" -o /opt/maclaw-node/maclawsrv ./MaClawSrv || exit 1
go build -ldflags "-s -w" -o /opt/maclaw-node/botnode ./cmd/botnode || exit 1
```

`GOTOOLCHAIN=local` 让这次编译用刚装上的 Go，不要再从网上下载另一套工具链。模块下不来时，把 `GOPROXY` 改成 `https://proxy.golang.org,direct` 再执行这一节。

## 5. MaClawSrv 的环境

先写出环境。Hub 令牌要等注册成功后再由第 7 节写入。先不要启动 maclawsrv。文件里已经有管理员密钥、签名密钥或 `MACLAW_DESKTOP_API_TOKEN` 时沿用，不要另生成，也不要清掉令牌。

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

## 6. 注册并保持隧道

`botnode` 用注册码向 Hub 换长期密钥，把结果写入 `/var/lib/maclaw/botnode.json`（权限 600），然后连上 `GET /api/v1/edge/link`。连接是出站 WebSocket。本机是隧道的服务端：Hub 每开一条流，第一行是 `desktopd` 或 `srv`，本机只转发到配置的本机地址，其它目标直接关掉。这台节点只配 `srv`，`desktopd` 必须写成 `off`。

`BOTNODE_SRV_ADMIN` 必须是 `/etc/maclaw/maclawsrv.env` 里的 `MACLAW_ADMIN_SECRET`，注册时 Hub 会核对它。

先确认这同一条 shell 里 `HUB_URL`、`ENROLL_CODE` 已经赋值。不要 echo。只写这一份 `botnode.env`：

```sh
umask 077
HUB_URL=$(printf '%s' "$HUB_URL" | tr -d '[:space:]')
ENROLL_CODE=$(printf '%s' "$ENROLL_CODE" | tr -d '[:space:]')
MACLAW_ADMIN_SECRET=$(sed -n 's/^MACLAW_ADMIN_SECRET=//p' /etc/maclaw/maclawsrv.env | tr -d '[:space:]')
if [ -z "$HUB_URL" ] || [ -z "$ENROLL_CODE" ] || [ -z "$MACLAW_ADMIN_SECRET" ]; then
  echo "HUB_URL, ENROLL_CODE, or MACLAW_ADMIN_SECRET is empty"
  exit 1
fi
cat > /etc/maclaw/botnode.env <<EOF
BOTNODE_HUB_URL=${HUB_URL}
BOTNODE_ENROLL_CODE=${ENROLL_CODE}
BOTNODE_STATE=/var/lib/maclaw/botnode.json
BOTNODE_DESKTOPD_ADDR=off
BOTNODE_SRV_ADDR=127.0.0.1:18080
BOTNODE_SRV_ADMIN=${MACLAW_ADMIN_SECRET}
EOF
chmod 600 /etc/maclaw/botnode.env
```

`BOTNODE_DESKTOPD_ADDR=off` 不能省：留空会被当成 `127.0.0.1:18081`，隧道会去连一个不存在的 desktopd。

写入 `/etc/systemd/system/maclaw-botnode.service`：

```ini
[Unit]
Description=MaClaw intranet bot node
After=network-online.target
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

- 404 或 `edge nodes are disabled`：Hub 没开内网节点，或开了但没重启。注册码还没用掉。
- `enrollment code is invalid`：码过期、输错，或已经用过。向调用方要新码，改 `BOTNODE_ENROLL_CODE` 后再启动。不要复用旧码。
- `srv admin secret is required`：`BOTNODE_SRV_ADMIN` 没写或和 `MACLAW_ADMIN_SECRET` 对不上。改好后 `systemctl restart maclaw-botnode`。
- `这个租户的节点数已到上限`：码还没用掉。让用户在桌面端处理限额后再启动。

成功之后状态文件里有 `node_id`、`node_secret`、`hub_token`、`hub_url`。之后重启 botnode 会直接用已保存的密钥连隧道，不再读取注册码。`BOTNODE_HUB_URL` 必须和状态文件里的 `hub_url` 一致。

## 7. 把 Hub 令牌交给 MaClawSrv 并启动

把 `hub_token` 写入 srv 的环境。先收紧新建文件的权限。这个脚本不把令牌打到终端：

```sh
umask 077
tok=$(sed -n 's/.*"hub_token"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' /var/lib/maclaw/botnode.json | head -n 1)
if [ -z "$tok" ]; then echo "hub_token missing"; exit 1; fi
p=/etc/maclaw/maclawsrv.env
tmp=$(mktemp) || exit 1
seen=0
while IFS= read -r line || [ -n "$line" ]; do
  case "$line" in
    MACLAW_DESKTOP_API_TOKEN=*) printf 'MACLAW_DESKTOP_API_TOKEN=%s\n' "$tok"; seen=1 ;;
    *) printf '%s\n' "$line" ;;
  esac
done < "$p" > "$tmp"
[ "$seen" -eq 1 ] || printf 'MACLAW_DESKTOP_API_TOKEN=%s\n' "$tok" >> "$tmp"
chmod 600 "$tmp" || exit 1
mv "$tmp" "$p"
```

`MACLAW_DESKTOP_API_TOKEN` 必须是状态文件里的 `hub_token`，不要填 Hub 的全局桌面 API 令牌。这把 `hub_token` 只能访问发码的那个用户。上面的写入完成之前不要启动 maclaw-srv。

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

## 8. 确认

在这台机器上：

```sh
systemctl is-active maclaw-srv maclaw-botnode
grep -Eq '"node_id"[[:space:]]*:[[:space:]]*"[^"]+"' /var/lib/maclaw/botnode.json || exit 1
grep -Eq '^MACLAW_DESKTOP_API_TOKEN=.+' /etc/maclaw/maclawsrv.env || exit 1
```

两个服务都是 active，状态文件里有节点 id，令牌已写入且不打印它。通过后删掉环境文件里的一次性注册码，再重启 botnode。状态文件还在时，重启不会再读注册码：

```sh
sed -i '/^BOTNODE_ENROLL_CODE=/d' /etc/maclaw/botnode.env || exit 1
systemctl reset-failed maclaw-botnode || true
systemctl restart maclaw-botnode || exit 1
```

不要安装 desktopd，也不要在这一步补装 Docker。这台机器上出现 `maclaw-desktopd` 服务或 `maclaw-gui` 镜像都说明装错了文档。

`botnode` 的日志里不应出现注册码或令牌。隧道断开后进程会自己重连。已经写入 `botnode.json` 的节点不受 Hub 重启影响，等 Hub 起来后隧道会重连。

注册完成后，调用方的桌面端会在「设置 → 云端 Agent」的节点列表里看到这台机器（名字是发码时的名字）。用户选中并保存后，云端任务就会跑在这台机器上。

## 升级

这是已经装好并在运行的机器。升级 = 更新源码，重新编译 MaClawSrv 和 botnode 两个二进制，重启两个服务。**不是重新注册**：`/var/lib/maclaw/botnode.json`、`/etc/maclaw/maclawsrv.env`、`/etc/maclaw/botnode.env` 和 `/var/lib/maclaw/srv` 里的已有数据必须原样保留，不要删，不要改，不要重跑注册码。

先确认同一条 shell 里 `HUB_URL` 已赋值。执行第 3 节更新 `/opt/maclaw-src`，然后按第 4 节重新编译两个二进制（只编 `maclawsrv` 和 `botnode`，不要编 desktopd），最后重启：

```sh
systemctl restart maclaw-srv maclaw-botnode || exit 1
sleep 2
systemctl is-active maclaw-srv maclaw-botnode || exit 1
```

两行都是 `active` 才算完成。确认状态文件还在且不打印内容：

```sh
grep -Eq '"node_id"' /var/lib/maclaw/botnode.json || exit 1
grep -Eq '^MACLAW_DESKTOP_API_TOKEN=.+' /etc/maclaw/maclawsrv.env || exit 1
```

升级不需要新注册码，也不要动 Hub 上的节点。`systemctl is-active` 已经 active 不代表二进制换过了——必须先编译安装完再重启，不要跳过第 4 节直接重启。

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

轮换会换掉 `hub_token`，旧令牌马上失效。再执行第 7 节的写入，然后 `systemctl restart maclaw-srv`。

轮换失败并出现 `issue a new code` 时，再点一次这台节点的「轮换密钥」。不要改去点「生成内网注册码」。
