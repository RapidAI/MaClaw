# 桌面 Bot 消息超时（Hub、nginx、写超时）

本文记录云端桌面 Bot 发一条消息时，整段等待要一起加长的配置。生产主机是 `hub.mypapers.top`。文中的线上值核对于 2026-10-09 07:55 CST。进程号会变，改配置前先做文末的核对。

这条链路和 [faq.md](../faq.md) 第 18 节的 LLM SSE 超时不是同一件事。SSE 要保住思考阶段的字节流；这里的同步 `/messages` 在回合结束前不写响应头。

## 统一预算

同步消息的共享上限是 **30 分钟**。下面几跳使用同一数字。短的那一跳会先断开，后面的加长没有效果。

| 跳 | 位置 | 当前值 | 计时起点 |
| --- | --- | --- | --- |
| 桌面程序 | `guiapp/desktop_bot_hub.go` 的 `desktopBotCall` | 30 分钟 | `context.WithTimeout` 建立时，早于请求发出 |
| 聊天锁 | `DesktopBotWorkspace.tsx` 的 `DESKTOP_BOT_REPLY_WAIT_MS` | 31 分钟 | 只放开输入，不取消 HTTP |
| Hub 入口 nginx | `hub.mypapers.top` 的 `location /` | `proxy_read_timeout` / `proxy_send_timeout` 1800s | 两次向上游读取之间的空隙。响应头出来之前没有字节，所以就是等响应头 |
| Hub 客户端 | `hub/internal/botmgmt/service.go` 的 `httpClient`，路径含 `/messages` | 30 分钟 | Hub 发出 maclawsrv 请求时。前面还有读实例和准备模型的时间 |
| maclawsrv 入口 nginx | `maclawsrv.mypapers.top` 的 `location /` | 1800s | 与 Hub 入口 nginx 相同，等的是 maclawsrv 的响应头 |
| maclawsrv 写超时 | `MaClawSrv/http.go` 的 `messageResponseWriteBudget` | 30 分钟 | `allowMessageResponseWrite` 被调用时，在占用桌面之后 |

写超时在占用桌面之后才开始，比 Hub 客户端晚几秒。回合在 Hub 还连着的时候结束，响应头赶得上这次连接。Hub 或桌面程序先到点断开后，这次回复不会出现在聊天里。写超时设得一样长，是为了避免 maclawsrv 在上游还等着的时候先关掉连接。某一跳更短会先断开，后面的加长没有效果。不要设成无限。

`proxy_connect_timeout` 只管连上上游。Hub 的 `location /` 是 30 秒。它不覆盖等响应头的 30 分钟。

2026-10-09 08:19 构建的桌面窗口，同步路径是 30 分钟调用、31 分钟聊天锁。更早的窗口才是 8 分钟调用、9 分钟聊天锁。`finishDesktopBotTask` 的上下文本身没有更短的截止时间。同步时这 30 分钟加在登记那一次 POST 上。桌面程序取消自己的请求时，Hub 的 `PostBotMessageHandler` 用的是同一个请求上下文，上游调用会一起断开。

## 登记后后台执行

桌面 Bot 的源码已经把「接受」和「做完」拆开。GUI 对 `/messages` 带 `Prefer: respond-async`。Hub 见到 maclawsrv 的 202 和 run id 后，立刻回 `{accepted, run_id, status:running}`。桌面占用和操作留在后台。GUI 再用短请求读 `GET /api/v1/bots/{id}/runs/{runID}`，单次大约 20 秒。没有拿到 HTTP 状态（连接失败、读超时）会再读。已经返回的 HTTP 错误结束桌面这一轮，不再重试。这两种情况都不取消服务端的 run。run 结束后，结果按原来的回复写回对话。

上面的 30 分钟仍留给同步 `/messages`，以及旧 Hub 忽略 Prefer、把正文写在同一次调用里的情况。新 Hub 在没有 Prefer 时仍走同步。先部署 Hub、后构建桌面，或先构建桌面、后部署 Hub，这次登记 POST 仍最多等 30 分钟，回复还能回来。

31 分钟的聊天锁只放开重新打开之后、本进程已经不再持有的那条等待。本进程里还没结束的登记继续占着这个 bot，下一条命令排队。

这段登记还在源码里。08:19 的窗口和当前生产仍是一条长 POST。要等下一次构建和部署才换成后台执行。

确认时看 `~/.maclaw/logs/bots/<bot-id>.log`，同一行也会打到进程日志，前缀是 `[bot-pipeline]`。桌面程序：`gui.admit` 带 `run=` 表示已经登记，`mode=sync` 表示这次调用里就带回了正文。接着 `gui.run_poll`。同一种状态大约 15 秒一条；状态变了（还在跑、短读失败、读到了结果）会立刻再写一条。`retry=yes` 是这次短读没有 HTTP 状态、准备再读。结束那条带 `text_len`。Hub：`hub.admit`，然后 `hub.run_poll`。还在跑、短读失败、`waiting=yes` 也是变了就立刻写，没变则大约 15 秒一条。`ready=yes` 或 `waiting=expired` 每次都写，然后是原来的 `hub.post_end`。maclawsrv：`srv.http_end` 里 `async=true` 是 202 已经返回；`srv.desktop_prepare` 是后台占用桌面；`srv.desktop_finish` 写回截图和交接。同一条 `srv.desktop_finish` 带 `replay=yes` 是同一个幂等键的重复登记：多出来的占用会放开，不会拿走交接，也不会提前写下结果。这次放开如果是用户桌面上最后一次占用，并且拥有者的选择还留着，停不停桌面沿用那次选择。拥有者已经结束并清掉这份选择之后，后来的重复登记仍不会把交接中的桌面关掉。Hub 接受了这次停止之后，交接计数和登录者才会清掉，后面的重复登记会把自己多开的会话关掉。Hub 留下桌面，或调用方已经断开时，这两项还在，后来的重复登记不会把它关掉。登录续跑已经决定停止、但更新的打开先占住这台桌面时，原先这次登录先不再挡住这次打开；这次打开自己结束时会关掉桌面，除非它又做了交接。这次停止若被 Hub 留下或没有成功，或这次打开的调用方已经断开，原先的交接会回来，后来的重复登记不会把它关掉。后面再有一次打开占住这台桌面，已经记下的登录者还在。删掉这个 bot 时，记下的登录者一并去掉，已经算过的交接留着，所以 Hub 留下停止时这个人不会回到桌上，后来的重复登记仍会关掉桌面。没人占着桌面时，多开的会话会关掉。日志里没有正文、截图字节和令牌。

`guiapp/hub_http_client.go` 里默认 30 秒的 Hub 客户端不走这条消息。Bot 消息用的是 `desktopBotCall` 上的显式超时。

执行阶段安装软件（desktop `action=install`）里，apt-get 自己在 20 分钟停下，并返回时间预算用尽这句。停在半截的包装，下一次安装会先等 dpkg 的锁再执行 `dpkg --configure -a`。dpkg 自己拿到锁就立刻失败，被杀掉的 apt 往往还握着这把锁，所以要等它放开。拆到一半的包 configure 配不完，源更新成功后会再跑不带包名的 `apt-get -f install` 补完，然后才安装这次要的包。配置失败而源更新也失败时，带回配置这一步的错误。这些步骤都不经过 shell。MaClawSrv 到 Hub、Hub 到 desktopd 的客户端，以及 desktopd 这次响应的写截止时间，都再多等 1 分钟，好把这句写回去、读回来。整跳仍落在上面的 30 分钟消息预算里。`dockerd.mypapers.top` 的 nginx 如果还是默认 60 秒，大的安装包会在网关被切断；那段 `proxy_read_timeout` 要盖住这 21 分钟。这次没有改生产 nginx。

## 请求怎么走

1. 桌面程序先在本地写下确认句，再调用 Hub。执行阶段的确认是「我按这个安排去做。有结果或需要你时再告诉你。」
2. Hub 把消息转到 `https://maclawsrv.mypapers.top`，路径是实例的 `/messages`。Hub 自己的 `http.Server` 没有 `ReadTimeout` / `WriteTimeout`，不会在 120 秒切断给桌面的响应。
3. maclawsrv 同步处理时，先占用用户桌面，再调用 `allowMessageResponseWrite`，然后才进入 `SendMessage` 或 `PostMessage`。响应头要等回合结束才写。
4. 异步提前返回的路径不调用 `allowMessageResponseWrite`。校验失败等快路径也不调用。桌面 Bot 的 `Prefer: respond-async` 走这条提前返回；截图和交接记在 run 上，由后面的短读取回。

Hub 其他 Bot 接口仍用 15 秒客户端。只有路径里包含 `/messages` 的调用用 30 分钟。`Service.messageTimeout` 设成正数会覆盖这 30 分钟，测试用它。

## 全局 120 秒写超时为什么还在

`MaClawSrv/main.go` 的 `http.Server.WriteTimeout` 保持 **120 秒**。它从读完请求头开始计算，处理函数一直不写响应时不会被重置。普通接口继续用这 120 秒。

同步 `/messages` 在占用桌面之后用 `http.NewResponseController(w).SetWriteDeadline` 把这一次请求的截止时间改成现在起 30 分钟。调用点在 `MaClawSrv/http_agent.go` 的 `handleSendMessage` 和 `handlePostMessage`。

`ResponseController` 的文档写过：写截止时间已经超过之后不能再延长。在 maclawsrv 使用的 HTTP/1 服务器上，`TestAllowMessageResponseWriteExtendsAnExpiredDeadline` 证明：先睡过服务器的 `WriteTimeout`，再调用 `allowMessageResponseWrite`，响应体仍然能写出去。所以这个调用放在占用桌面之后，即使占用本身已经超过 120 秒也仍然有效。

`writeJSON` 忽略编码错误。因此会出现 maclawsrv 日志里的 `http_status=200`，同时 nginx 记录 `upstream prematurely closed connection while reading response header`。那表示响应头还在缓冲里，连接已经被写超时关掉。

相关测试：

- `TestMessageResponseWriteBudgetCoversHubWait`
- `TestAllowMessageResponseWriteOutlivesServerWriteTimeout`
- `TestServerWriteTimeoutDropsSilentHandler`
- `TestAllowMessageResponseWriteExtendsAnExpiredDeadline`
- `TestMessageClientWaitsAsLongAsTheDesktopTurn`

## 生产 nginx

听 `0.0.0.0:443` 的是自定义 nginx/1.17.8，前缀 `/soft/nginx/`。配置文件是 `/data/soft/nginx/conf/nginx.conf`，与 `/soft/nginx/conf/nginx.conf` 是同一个文件。2026-10-09 的 master 是 pid 12102，reload 不换 master。

系统 nginx 1.14.0 的 unit 是失败状态，配置在 `/etc/nginx`。`PATH` 里的 `nginx -T` 打出来的是这份没在听端口的配置。改超时时编辑上面的自定义配置，用下面的二进制检查和重载：

```bash
/soft/nginx/sbin/nginx -p /soft/nginx/ -c conf/nginx.conf -t
/soft/nginx/sbin/nginx -p /soft/nginx/ -c conf/nginx.conf -s reload
```

`-s reload` 返回 0 只表示信号已发出。到错误日志里确认有 `signal process started`，并用 `ps` 看到新的 worker。正在 `shutting down` 的旧 worker 会自己退出，不要杀它。`nginx -t` 失败时，master 仍在用上一份配置。把备份拷回配置文件，不要 reload。只有 `-t` 通过之后才 reload。

Python 3.6 的 `subprocess.run` 没有 `capture_output`。放在这台机器上的脚本不要用这个参数。

### 要改的两处

桌面 Bot 打到 Hub 的 `location /`。`/api/llm/` 即使 `proxy_pass` 也是 `127.0.0.1:9399`，也不是这条消息。

只改下面这几行。`proxy_http_version`、`Upgrade` 和其余 `proxy_set_header` 留在原处。不要用一个只含超时的 `location` 覆盖整段，否则 Hub 的 WebSocket 升级头会被删掉。

`hub.mypapers.top` 443 的 `location /` 里，超时在 `Connection` 那一行后面：

```nginx
# Desktop-bot sync messages and long reasoning wait on response headers. 30 minutes.
proxy_read_timeout 1800s;
proxy_send_timeout 1800s;
proxy_connect_timeout 30s;
```

这段的 `proxy_pass` 是 `http://127.0.0.1:9399`。同一 server 里的 `/api/llm/` 也转到 9399，保持 600 秒，并保持 `proxy_buffering off`。那是 SSE。思考阶段的静默预算仍按 [faq.md](../faq.md) 第 18 节。

`maclawsrv.mypapers.top` 443 的 `location /` 里，超时在 `proxy_http_version` 后面、`proxy_set_header` 前面：

```nginx
# Message turns write headers only when the model finishes. 30 minutes.
proxy_read_timeout 1800s;
proxy_send_timeout 1800s;
```

这段的 `proxy_pass` 是 `http://127.0.0.1:18080`。

同一份配置里还有多段 `location /` 和多段 `proxy_pass http://127.0.0.1:9399`。`hubs.rapidai.tech` 也转到 9399，而且没有单独的 `proxy_read_timeout`。替换时用「`server_name hub.mypapers.top` 的 443 `location /`」和「`proxy_pass http://127.0.0.1:18080` 加上 Message turns 注释」做唯一定位。2026-10-09 改之前全文没有 `1800s`，改完 `proxy_read_timeout 1800s` 和 `proxy_send_timeout 1800s` 各增加 2 条。文件里如果已经有别的 `1800s`，按新增条数核对，不要把全文删到只剩两对。

### 保持不动的配置

- `hub.mypapers.top` 的 `location /api/llm/`：600 秒。
- `hubs.mypapers.top`（9388）、`dockerd.mypapers.top`（18081），以及 skillmarket、iworker、ve 等其他 vhost。
- `hubs.rapidai.tech`。
- Hub 与 HubCenter 之间的 SSE 客户端超时（`MaClawProviderClient`、`proxyStreamingHTTPClient`）。那些数字在 faq 第 18 节。

nginx 的 worker 按事件处理连接。把 `proxy_read_timeout` 加到 30 分钟不会为每个请求占住一个 worker。

备份：

- `/data/soft/nginx/conf/nginx.conf.bak-maclawsrv-timeout-20261009`（当时把 maclawsrv 从默认 60 秒改到 600 秒）
- `/data/soft/nginx/conf/nginx.conf.bak-message-wait-20261009`（2026-10-09 07:53 把上述两处改到 1800 秒）

[hub_manual.md](../hub_manual.md) 第 12 节的示例把站点和长连接写在同一个 `location /` 里，示例值是 3600 秒。那是自建 Hub 的示例，不是这台机器上拆开的 `/` 与 `/api/llm/`。

## Hub 与 maclawsrv 进程

Hub 不是 systemd 服务。二进制是 `/data/soft/hub/maclaw-hub`，配置是 `/data/soft/hub/configs/config.yaml`，用 `/data/soft/hub/start.sh` 重启。配置文件已经存在时，这个脚本不会覆盖它。它只停 Hub 自己的 pid、`codeclaw-hub`，以及工作目录在 Hub 目录下的 `./maclaw-hub`。不要重启 `/data/soft/hubcenter/maclaw-hubcenter`。

Hub 二进制没有版本字符串。新文件先覆盖正在运行的二进制（Linux 上旧进程继续用旧 inode），再执行 `start.sh`。用新 pid 和文件修改时间确认，不要找 `/version`。2026-10-09 07:55 换上的进程 pid 是 23504。HubCenter 当时的 pid 4483 没有动。

maclawsrv 是 systemd 单元 `maclawsrv.service`。二进制是 `/data/soft/maclaw_srv/bin/maclawsrv`，监听来自 `/data/soft/maclaw_srv/.env` 的 `MACLAW_HTTP_ADDR=:18080`。不要改单元文件，不要改 `.env`，不要用会重写 systemd、`start.sh` 或 `.env` 的远程部署脚本。

替换步骤：停单元，把当前二进制备份成新的文件名，拷入新二进制，`chmod 755`，再启动。启动后大约 3 秒才开始监听，立刻访问会得到 connection refused。确认：

```bash
curl -fsS --max-time 3 http://127.0.0.1:18080/version
systemctl show maclawsrv.service -p MainPID,ActiveState,NRestarts --no-page
```

2026-10-09 07:55 的 `/version`：

```json
{"built_at":"2026-10-08T23:46:19Z","commit":"5e3bfe7b-dirty","version":"20261009.message-wait"}
```

当时 MainPID 是 23399，`NRestarts=0`。保留这些备份，不要覆盖：

- `/data/soft/maclaw_srv/bin/maclawsrv.bak-cdp-20261009`
- `/data/soft/maclaw_srv/bin/maclawsrv.bak-write-deadline-20261009`
- `/data/soft/maclaw_srv/bin/maclawsrv.bak-message-wait-20261009`

`bak-message-wait` 里是换上 30 分钟写超时之前的 `20261009.write-deadline` 二进制。构建必须用当前工作区。只编译 HEAD 会退回已经在线上的其他 maclawsrv 改动。Linux 构建：

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
  -ldflags "-s -w -X main.serviceVersion=... -X main.serviceCommit=... -X main.serviceBuiltAt=..." \
  -o maclawsrv ./MaClawSrv/
```

Hub 用同一组 `GOOS` / `GOARCH`，入口是 `./hub/cmd/hub/`。编完在同一个 shell 里清掉 `GOOS`、`GOARCH`、`CGO_ENABLED`。

重启 Hub 或 maclawsrv 会打断当时还没返回的那一轮。nginx reload 不切断已经接在旧 worker 上的连接；后端进程重启仍会打断这一轮。

## 现象对照

桌面聊天里的红字来自 `friendlyFailure`。原始错误留在日志里。

| 看到的结果 | 实际断开点 | 日志 |
| --- | --- | --- |
| 「这条没送到——我这边暂时连不上我的服务器。麻烦稍后再发一次。」 | 桌面程序收到的 Hub 正文含 `unavailable` 或 `不可达`。`writeBotUserError` 把 maclawsrv 的失败都写成 `bot service is unavailable`，nginx 的 504 页面和 Hub 客户端超时都走这句 | maclawsrv 的 `proxy_read_timeout` 缺失时，nginx 写 `upstream timed out while reading response header`。写超时则是 `upstream prematurely closed connection while reading response header`。maclawsrv 仍可能记下 `http_status=200`，因为 `writeJSON` 忽略写出错误，日志记的是打算返回的状态码 |
| 「这个任务等了太久，我这边超时了。稍后再发一次试试。」 | 桌面程序自己的截止时间到了。`desktopBotCall` 在还没收到响应时返回「没有在时限内」 | 已打开的窗口是 8 分钟。重新构建后是 30 分钟，并且仍比 Hub 客户端先到 |
| 确认句还在，后面没有执行结果 | 某一跳已断开，本地确认句是发送返回前写的 | 看 Hub `post_message` 的 `dur_ms` 和 `timeout=` |

`desktopCallTimedOut` 只看错误文本里有没有子串 `timeout` 或 `deadline exceeded`。nginx 的 `timed out` 对不上 `timeout`，所以 504 HTML 曾被记成 `timeout=no`。这种失败会按普通失败处理桌面键盘，不会按「调用超时、浏览器还开着」处理。Hub 日志里的 `timeout=` 和聊天红字可以不一致：客户端超时的错误文本含有 `timeout`，日志是 `timeout=yes`，返回给桌面的仍是 unavailable。判断键盘时看 `timeout=`，判断用户看见哪句红字时看返回给桌面的 `message`。

按断开时间缩小范围：约 60 秒是 nginx 默认 `proxy_read_timeout`；约 120 秒是 maclawsrv 全局 `WriteTimeout` 仍在生效，说明这次请求没有成功改掉写截止时间（没执行到 `allowMessageResponseWrite`、跑的是旧二进制，或 `SetWriteDeadline` 失败但错误被丢掉）；约 8 分钟是还没重新构建的桌面程序；约 10 分钟是 `20261009.write-deadline` 那一版写超时；600 秒是旧的 nginx，或请求打到了仍为 600 秒的 location；30 分钟是当前桌面源码、Hub、两处 `location /` 和写超时一起的上限。

## 以后再加长

一次改完这些位置，再发布 Hub、两处 nginx 和 maclawsrv。桌面源码一起改，窗口等构建：

- `messageResponseWriteBudget`
- `httpClient` 里 `/messages` 的 `30 * time.Minute`
- `relayDesktopBot` 传给 `desktopBotCall` 的超时
- `DESKTOP_BOT_REPLY_WAIT_MS`，比上面的调用多 1 分钟
- 两个 `location /` 的 `proxy_read_timeout` 和 `proxy_send_timeout`（1800 秒对应 30 分钟）
- `TestMessageResponseWriteBudgetCoversHubWait` 和 `TestMessageClientWaitsAsLongAsTheDesktopTurn`

全局 `WriteTimeout` 保持 120 秒。`/api/llm/` 保持 600 秒。

改之前在服务器上核对正在听端口的 nginx 和当前数字：

```bash
ss -ltnp | awk '/:443 /'
ps -eo pid,ppid,lstart,cmd | awk '/nginx/ && !/awk/'
python3 - <<'PY'
text = open("/data/soft/nginx/conf/nginx.conf").read()
print("read1800", text.count("proxy_read_timeout 1800s;"))
print("send1800", text.count("proxy_send_timeout 1800s;"))
print("read600", text.count("proxy_read_timeout 600s;"))
PY
curl -fsS --max-time 3 http://127.0.0.1:18080/version
ps -eo pid,lstart,cmd | awk '/maclaw-hub|maclaw-hubcenter/ && !/awk/'
```

443 上应只有 `/soft/nginx/` 的 master。`/api/llm/` 的 600 秒要还在。maclawsrv 的 `/version` 应对上本文的 `20261009.message-wait`，除非之后又发布过。
