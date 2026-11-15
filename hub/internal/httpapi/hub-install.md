# 安装 Hub 并注册到 Hub Center

这份说明给 Linux 机器上的 agent。调用方给你这几样东西：SSH 登录信息、`HUB_PUBLIC_URL`（这台 Hub 对外的地址）、`OWNER_EMAIL`（注册用的邮箱）、`ADMIN_USER`、`ADMIN_PASSWORD`，以及 `CENTER_URLS`（Hub Center 地址，多个用空格或换行隔开）。

`HUB_PUBLIC_URL` 不要带路径，不要带末尾斜杠。例如 `http://203.0.113.10:9399`。这是客户端以后访问这台 Hub 的地址，也是注册时交给 Hub Center 的地址。不要用文档里的示例地址。9399 是 Hub 的端口，不是 SSH 端口。

`OWNER_EMAIL` 必须是带 `@` 的邮箱。Hub 用它作为这台 Hub 在 Hub Center 上的主人。`ADMIN_PASSWORD` 是十六进制，不要改，也不要打印。

不要把 SSH 密码、`ADMIN_PASSWORD`、`hub_secret` 写进 git，也不要在回复里复述。

## 0. 权限

写 `/opt`、`/etc` 和 systemd 都要 root。登录用户不是 root 时：可以 sudo 就用 sudo，密码用登录密码。不能提权就停下来。

下面命令按 root 来写。换了一条 shell 之后，变量就没了。每条新 shell 先重新赋值 `HUB_PUBLIC_URL`、`OWNER_EMAIL`、`ADMIN_USER`、`ADMIN_PASSWORD`、`ADMIN_SETUP_MARK`、`CENTER_URLS`，不要 echo。

## 1. 这台机器要有的东西

- Linux x86_64 或 aarch64，systemd
- Git、curl、Go 1.26 或更新、python3
- 出站能访问 `CENTER_URLS` 里的 Hub Center

没有 Go 1.26 时，第 3 节会安装官方 Go 1.26.0 到 `/usr/local/go`。不要用发行版的 `golang` 包。缺 git、curl 或 python3 时先装上。确认注册和管理员初始化都要用 python3。先删掉 `/opt/maclaw-hub/data/admin-setup.result`。上一轮留下的 `200` 会让这次安装在新的管理台密码写好前就结束。

```sh
rm -f /opt/maclaw-hub/data/admin-setup.result
missing=""
command -v git >/dev/null 2>&1 || missing="$missing git"
command -v curl >/dev/null 2>&1 || missing="$missing curl"
command -v python3 >/dev/null 2>&1 || missing="$missing python3"
if [ -n "$missing" ]; then
  if command -v apt-get >/dev/null 2>&1; then
    DEBIAN_FRONTEND=noninteractive apt-get update || exit 1
    DEBIAN_FRONTEND=noninteractive apt-get install -y $missing || exit 1
  elif command -v dnf >/dev/null 2>&1; then
    dnf install -y $missing || exit 1
  else
    echo "missing:$missing"
    exit 1
  fi
fi
```

## 2. 下载源码

远程是 `https://github.com/RapidAI/MaClaw.git` 时只更新。半截目录或别的远程先清掉再 clone。只动 `/opt/maclaw-src`。内网节点如果已经克隆过，这里只更新。

```sh
origin=$(git -c safe.directory=/opt/maclaw-src -C /opt/maclaw-src remote get-url origin 2>/dev/null || true)
case "$origin" in
  https://github.com/RapidAI/MaClaw|https://github.com/RapidAI/MaClaw.git) ;;
  *) rm -rf /opt/maclaw-src || exit 1 ;;
esac
if [ -d /opt/maclaw-src/.git ]; then
  git -c safe.directory=/opt/maclaw-src -C /opt/maclaw-src fetch --depth 1 origin main || exit 1
  git -c safe.directory=/opt/maclaw-src -C /opt/maclaw-src reset --hard FETCH_HEAD || exit 1
else
  rm -rf /opt/maclaw-src || exit 1
  git clone --depth 1 --branch main https://github.com/RapidAI/MaClaw.git /opt/maclaw-src || exit 1
fi
```

不要换远程地址。浅克隆不要用 `git pull`。目录属主和当前用户不一致时，git 会拒绝更新，所以上面的命令带了 `safe.directory=/opt/maclaw-src`。

## 3. 编译

先建目录，再编译。输出路径的父目录不会自动创建。sqlite 用纯 Go 驱动，`CGO_ENABLED=0`。`GOTOOLCHAIN=local` 让这次编译用本机的 Go，不要再从网上下载另一套工具链。`-buildvcs=false` 让编译不读取 git 状态，目录属主不一致时也不会因此失败。

```sh
mkdir -p /opt/maclaw-hub/configs /opt/maclaw-hub/data
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
export GOSUMDB=sum.golang.google.cn
case "$(uname -m)" in
  aarch64|arm64) export GOARCH=arm64 ;;
  x86_64|amd64) export GOARCH=amd64 ;;
  *) echo "unsupported arch: $(uname -m)"; exit 1 ;;
esac
if ! go build -buildvcs=false -ldflags "-s -w" -o /opt/maclaw-hub/maclaw-hub ./hub/cmd/hub; then
  export GOPROXY=https://proxy.golang.org,direct
  export GOSUMDB=sum.golang.org
  go build -buildvcs=false -ldflags "-s -w" -o /opt/maclaw-hub/maclaw-hub ./hub/cmd/hub || exit 1
fi
cp /opt/maclaw-src/hub/run-systemd.sh /opt/maclaw-hub/run-systemd.sh || exit 1
chmod 755 /opt/maclaw-hub/maclaw-hub /opt/maclaw-hub/run-systemd.sh || exit 1
```

模块下不来，或校验和数据库连不上时，上面的编译会改用 `https://proxy.golang.org,direct` 和 `sum.golang.org` 再编一次。

## 4. 写配置

`installation_id` 在第一次尝试注册时就会写入，那时 Hub Center 可能还没接受。不要只因为它存在就留下错误的邮箱或中心地址。只有配置里的 `recovery_secret`，或数据库里的 `hub_id`，才表示这台 Hub 已经注册，不要覆盖。读不到数据库时不要重写。已注册的身份换掉会变成另一次注册。重写配置时不要删 `/opt/maclaw-hub/data`，里面的安装 id 下次启动还会用。

第一次安装才写。`tls.enabled` 设为 false，这样监听的就是 `HUB_PUBLIC_URL` 里的 HTTP，不会一边生成自签证书、一边把另一个地址交给 Hub Center。

```sh
HUB_PUBLIC_URL=$(printf '%s' "$HUB_PUBLIC_URL" | tr -d '[:space:]')
OWNER_EMAIL=$(printf '%s' "$OWNER_EMAIL" | tr -d '[:space:]')
if [ -z "$HUB_PUBLIC_URL" ] || [ -z "$OWNER_EMAIL" ] || [ -z "$CENTER_URLS" ]; then
  echo "HUB_PUBLIC_URL, OWNER_EMAIL, or CENTER_URLS is empty"
  exit 1
fi
case "$OWNER_EMAIL" in
  *@*.*) ;;
  *) echo "OWNER_EMAIL is not an email"; exit 1 ;;
esac
has_identity=0
if [ -f /opt/maclaw-hub/configs/config.yaml ] && grep -Eq '^[[:space:]]*recovery_secret:[[:space:]]*"?[^"[:space:]#]' /opt/maclaw-hub/configs/config.yaml; then
  has_identity=1
fi
if [ "$has_identity" -eq 0 ] && [ -f /opt/maclaw-hub/data/maclaw-hub.db ]; then
  ident_status=0
  python3 - <<'PY' || ident_status=$?
import json, sqlite3, sys
con = sqlite3.connect("/opt/maclaw-hub/data/maclaw-hub.db", timeout=5)
con.execute("pragma query_only=ON")
row = con.execute("select value_json from system_settings where key='center_registration'").fetchone()
if not row:
    sys.exit(10)
d = json.loads(row[0])
sys.exit(0 if d.get("hub_id") else 10)
PY
  if [ "$ident_status" -ne 10 ]; then
    has_identity=1
  fi
fi
if [ "$has_identity" -eq 1 ]; then
  echo "config.yaml already has a hub identity; leaving it"
else
  set -f
  center_lines=$(printf '%s' "$CENTER_URLS" | tr -s '[:space:]' '\n' | sed '/^$/d')
  first_center=$(printf '%s\n' "$center_lines" | head -n 1)
  if [ -z "$first_center" ]; then
    echo "CENTER_URLS is empty"
    exit 1
  fi
  umask 077
  config_status=0
  {
    echo "server:"
    echo "  listen_host: 0.0.0.0"
    echo "  listen_port: 9399"
    echo "  public_base_url: \"${HUB_PUBLIC_URL}\""
    echo "tls:"
    echo "  enabled: false"
    echo "database:"
    echo "  driver: sqlite"
    echo "  dsn: ./data/maclaw-hub.db"
    echo "center:"
    echo "  enabled: true"
    echo "  base_url: \"${first_center}\""
    echo "  base_urls:"
    printf '    - "%s"\n' $center_lines
    echo "  owner_email: \"${OWNER_EMAIL}\""
    echo "  register_on_startup: true"
    echo "hub:"
    echo "  name: MaClaw Hub"
    echo "  visibility: private"
    echo "logging:"
    echo "  level: info"
    echo "  dir: ./data/logs"
  } > /opt/maclaw-hub/configs/config.yaml || config_status=$?
  set +f
  if [ "$config_status" -ne 0 ]; then
    echo "config.yaml was not written"
    exit 1
  fi
  chmod 600 /opt/maclaw-hub/configs/config.yaml || exit 1
fi
```

`base_url` 用 `CENTER_URLS` 里的第一个地址，写进 `first_center`。不要用 `set --`，那会改掉当前 shell 的 `$1`。多个地址按空格拆开，每个单独一条；整段当成一个地址时，注册会失败。不写 `base_url` 的话，进程会留下默认的 Hub Center 地址，注册可能发到另一台中心。

`register_on_startup: true` 时，Hub 进程起来后自己向 Hub Center 发 `POST /api/hubs/register`。请求里有 `owner_email`、`public_base_url` 解析出的 host 和端口，以及安装 id。不要从安装脚本直接调用这个接口。Hub Center 返回 `hub_id` 和 `hub_secret`，Hub 把它们写进本机数据库的 `system_settings`，键是 `center_registration`。不要打印 `hub_secret`。

`pending_confirmation` 为 true 也算这次注册已经交给中心，只是等中心管理员确认。`registered` 为 true 是已经确认。两者都会留下 `hub_id`。`disabled` 为 true，或没有 `hub_id`，才是失败。失败原因在 `last_error`。

数据库里已经有 `hub_id` 时不要删 `/opt/maclaw-hub/data`。删掉会丢掉这台 Hub 的身份。

## 5. 启动

不要把 systemd 的 `ExecStart` 指到 `start.sh`。`start.sh` 发现单元存在时会再重启单元，形成循环。用 `run-systemd.sh`。这个脚本在没设置 `HUB_EDGE_NODES` 时会默认打开内网节点。

写入 `/etc/systemd/system/maclaw-hub.service`：

```ini
[Unit]
Description=MaClaw Hub
After=network-online.target
Wants=network-online.target
StartLimitIntervalSec=0

[Service]
WorkingDirectory=/opt/maclaw-hub
ExecStart=/opt/maclaw-hub/run-systemd.sh
Restart=on-failure
RestartSec=3

[Install]
WantedBy=multi-user.target
```

```sh
systemctl daemon-reload
systemctl enable maclaw-hub || exit 1
systemctl reset-failed maclaw-hub || true
if ! systemctl restart maclaw-hub; then
  systemctl stop maclaw-hub
  echo "maclaw-hub did not start; see /opt/maclaw-hub/data/logs/maclaw-hub.out.log and journalctl -u maclaw-hub"
  exit 1
fi
command -v curl >/dev/null || { echo "curl is required"; exit 1; }
i=0
until body=$(curl -fsS --max-time 2 http://127.0.0.1:9399/healthz 2>/dev/null) && printf '%s' "$body" | grep -q '"ok":true' && printf '%s' "$body" | grep -q '"service":"maclaw-hub"'; do
  i=$((i+1))
  if [ "$i" -ge 90 ]; then
    echo "hub health check failed; see /opt/maclaw-hub/data/logs/maclaw-hub.out.log and journalctl -u maclaw-hub"
    exit 1
  fi
  sleep 1
done
if command -v firewall-cmd >/dev/null 2>&1 && firewall-cmd --state >/dev/null 2>&1; then
  firewall-cmd --add-port=9399/tcp || echo "firewall-cmd did not add 9399/tcp; hub is still running"
  firewall-cmd --permanent --add-port=9399/tcp || echo "firewall-cmd did not add 9399/tcp permanently; hub is still running"
fi
if [ -f /etc/ufw/ufw.conf ] && grep -q '^ENABLED=yes' /etc/ufw/ufw.conf; then
  ufw allow 9399/tcp || echo "ufw did not allow 9399/tcp; hub is still running"
fi
```

健康检查应打印包含 `"ok":true` 的 JSON，并且 `service` 是 `maclaw-hub`。端口要等启动时的注册尝试结束才打开。每个 Hub Center 大约要 15 秒，所以这里等到 90 秒，不要在端口还没开时停掉服务。机器开着 firewalld 或 ufw 时，把 9399/tcp 放行，否则外面连不上这台 Hub。firewalld 先改当前规则，再写入永久规则。不要重载 firewalld，重载会断开当前 SSH。

## 6. 确认注册

Hub 在健康检查之前可能已经向 Hub Center 注册过。下面等到数据库里有 `hub_id`。只打印状态和 `hub_id`，不要打印 `hub_secret`。

这一段要在同一条 shell 里跑完。数据库文件还不存在时不要创建它。Hub 进程只在启动时注册一次，端口打开表示这一次尝试已经结束。`last_error` 有内容但没有 `hub_id` 时，继续等不会成功，注册失败要重启 Hub 进程，并且等端口重新打开后再读。旧的 `last_error` 会留到新进程写完为止，不要在端口打开前连续重启。`disabled` 为 true 时重启也没用，停下来。

```sh
command -v python3 >/dev/null || { echo "python3 is required"; exit 1; }
read_reg() {
  status=0
  python3 - <<'PY' || status=$?
import json, os, sqlite3, sys
path = "/opt/maclaw-hub/data/maclaw-hub.db"
if not os.path.isfile(path):
    sys.exit(1)
con = sqlite3.connect(path, timeout=5)
con.execute("pragma query_only=ON")
row = con.execute("select value_json from system_settings where key='center_registration'").fetchone()
if not row:
    sys.exit(1)
d = json.loads(row[0])
hub_id = d.get("hub_id") or ""
disabled = bool(d.get("disabled"))
registered = bool(d.get("registered"))
pending = bool(d.get("pending_confirmation"))
last_error = d.get("last_error") or ""
print(json.dumps({
    "registered": registered,
    "pending_confirmation": pending,
    "disabled": disabled,
    "hub_id": hub_id,
    "last_error": last_error,
}))
if disabled:
    sys.exit(3)
if hub_id and (registered or pending):
    sys.exit(0)
if last_error in (
    "hub center base url is required",
    "admin email is required for hub registration",
) or last_error.startswith((
    "invalid public base url",
    "public base url is missing host",
)):
    sys.exit(3)
if last_error:
    sys.exit(2)
sys.exit(1)
PY
}
wait_health() {
  i=0
  until body=$(curl -fsS --max-time 2 http://127.0.0.1:9399/healthz 2>/dev/null) && printf '%s' "$body" | grep -q '"ok":true' && printf '%s' "$body" | grep -q '"service":"maclaw-hub"'; do
    i=$((i+1))
    if [ "$i" -ge 90 ]; then
      return 1
    fi
    sleep 1
  done
}
try=0
status=1
while [ "$try" -lt 3 ]; do
  read_reg
  if [ "$status" -eq 0 ]; then
    break
  fi
  if [ "$status" -eq 3 ]; then
    echo "hub registration was rejected; the JSON above has last_error; see /opt/maclaw-hub/data/logs/registration.log"
    exit 1
  fi
  try=$((try+1))
  if [ "$try" -ge 3 ]; then
    echo "hub did not register; see /opt/maclaw-hub/data/logs/registration.log"
    exit 1
  fi
  systemctl reset-failed maclaw-hub || true
  systemctl restart maclaw-hub || exit 1
  wait_health || exit 1
done
```

`last_error` 若写着 `hub center base url is required`，配置里没有 Hub Center 地址。`admin email is required for hub registration` 表示 `owner_email` 是空的。`invalid public base url` 或 `public base url is missing host` 表示对外地址写坏了。这几类再重启也不会成功，停下来改配置。出站连不上 Hub Center 时，`last_error` 是网络错误，注册码不会有，因为 Hub 注册不使用内网节点那种一次性码。上面的重启就是为了再跑一次启动注册。

## 7. 创建管理员

注册不依赖管理员账号。管理员是给人登录管理台用的。`POST /api/admin/setup` 只能成功一次。已经初始化过会返回 409，那是正常的，不要重试成另一个账号。

```sh
umask 077
python3 - <<PY
import json
open("/opt/maclaw-hub/data/admin-setup.json","w").write(json.dumps({
    "username": "${ADMIN_USER}",
    "password": "${ADMIN_PASSWORD}",
    "email": "${OWNER_EMAIL}",
}))
PY
code=$(curl -sS -o /opt/maclaw-hub/data/admin-setup.out -w '%{http_code}' \
  -H 'Content-Type: application/json' \
  --data-binary @/opt/maclaw-hub/data/admin-setup.json \
  http://127.0.0.1:9399/api/admin/setup) || { rm -f /opt/maclaw-hub/data/admin-setup.json /opt/maclaw-hub/data/admin-setup.out; exit 1; }
rm -f /opt/maclaw-hub/data/admin-setup.json /opt/maclaw-hub/data/admin-setup.out
case "$code" in
  200|409)
    if [ -n "${ADMIN_SETUP_MARK:-}" ]; then
      printf '%s %s\n' "$code" "$ADMIN_SETUP_MARK" > /opt/maclaw-hub/data/admin-setup.result || exit 1
    else
      printf '%s\n' "$code" > /opt/maclaw-hub/data/admin-setup.result || exit 1
    fi
    chmod 600 /opt/maclaw-hub/data/admin-setup.result || exit 1
    ;;
  *) echo "admin setup failed: $code"; exit 1 ;;
esac
```

把状态码写到 `/opt/maclaw-hub/data/admin-setup.result`。有 `ADMIN_SETUP_MARK` 时写成 `200` 或 `409` 加这个标记，标记对不上就不能当成这次的结果。不要把密码写进去。`409` 表示管理台已经有管理员，这次生成的密码不会生效。

管理台地址是 `HUB_PUBLIC_URL`。用户名是 `ADMIN_USER`，邮箱是 `OWNER_EMAIL`。密码用调用方给的那一串，不要在回复里再写一遍。这次如果是 `409`，不要把这次的密码告诉用户。

这台 Hub 用的是 HTTP。桌面客户端访问非本机地址时要允许明文 HTTP，否则会拒绝连接。以后若改成 HTTPS，把 `public_base_url` 改成同一个主机名的 https 地址并重启，不要另注册一台。
