# OpenCode 免费模型：客户端识别备忘

> 2026-11-01，Windows 上用 OpenCode Desktop / CLI **2.2.0** 实机对照。
> 实现在 `corelib/llm/opencode_free_client.go`，资料在
> `corelib/llm/opencode_profile/`。协议漂移时先改那里，再按文末步骤重抓。

免费模型和去 `-free` 的计费模型是两个 id。`deepseek-v4.1-flash-free` 必须原样发到
标准推理接口。去掉后缀再打到 Go 网关会扣 Go 额度，而且那是另一个模型。

| 用途 | 方法 | 地址 |
| --- | --- | --- |
| 免费对话 | POST | `https://opencode.ai/inference/openai/v1/chat/completions` |
| 全量目录（`-free` 和计费 id 都在） | GET | `https://opencode.ai/inference/v1/models` |
| Go 订阅对话（扣额度） | POST | `https://opencode.ai/inference/go/openai/v1/chat/completions` |
| Go 目录（没有 `-free`） | GET | `https://opencode.ai/inference/go/v1/models` |

`big-pickle` 的线上 id 是 `glm-5.3-flash-free`。`cat-jam-free` 和
`longcat-2.5-preview-free` 不是这条免费门，仍可走 Go。`muse-spark-1.2-contributor-free`
走 Responses API，不走这篇的 chat 客户端。

存储里的地址经常还是旧的 `https://opencode.ai/zen/go/v1`。运行时按模型改写。
免费 id 即使存的是 zen/go、并且带了 key，也留在标准推理接口。

## 服务端怎么认客户端

认的是两件事，缺一不可。只对上其中一件，返回都是
`429 FreeUsageLimitError`（“Free models work in OpenCode”），`Retry-After`
指向当天 UTC 零点。这个 429 不是 Go 账户的额度用完。

### 1. TLS ClientHello 必须是 Bun `fetch` 的

同一份官方正文：

| 发出方 | 结果 |
| --- | --- |
| OpenCode 进程里的 `fetch` | 200 |
| Go `crypto/tls`、Python、Chrome 指纹的 uTLS | 429 |
| 按 OpenCode 里 `node:tls.connect` 抓到的握手去仿 | 429 |
| 按 Bun `fetch` 抓到的握手，用 uTLS 重放，并在最前面加上 SNI | 200 |

`fetch` 和 `node:tls` 不是同一套握手。不要用 `tls.connect` 抓指纹。

`fetch` 打到 IP 时没有 SNI，所以 `fetch.hello` 里没有 `server_name`。
连 `opencode.ai` 时要在扩展列表最前面补上 SNI。补完之后协商结果仍是
**HTTP/1.1**，不是 HTTP/2。门不在协议版本上。

稳定字段（2026-11-01 的 2.2.0）：

- 记录层版本 `0301`，session id 32 字节
- 套件顺序：`1301 1302 1303 c02b c02f c02c c030 cca9 cca8 c009 c013 c00a c014 009c 009d 002f 0035`
  （TLS 1.3 在前，ECDSA 在 RSA 前，没有 `c027`）
- 扩展：SNI、`0017`、`ff01`、`000a`（组 `11ec 001d 0017 0018`）、`000b`、`0023`、`0010`、`0005`、`000d`、`0012`、`0033`（key share `11ec` 1216 字节和 `001d` 32 字节）、`002d`、`002b`（`0304`、`0303`）

Go 标准库把 TLS 1.3 套件放在列表末尾，扩展集合也不一样。这个顺序不能靠
`crypto/tls` 的 `CipherSuites` 改出来。

### 2. 正文必须是官方代理那一份

TLS 对了也不够。下面两种在正确握手下仍然 429：

- 只有用户消息，没有官方系统提示词和工具
- 官方系统提示词还在，但工具换成一个空壳 `shell`

通过的正文是 OpenCode 2.2.0 发给免费模型的那次请求：

- `model` 保持带 `-free` 的 id
- 第一条是 `system.txt` 里的系统提示词，后面才是调用方的 user / assistant 消息。调用方自己的 system 会被丢掉
- `tools` 是 `tools.json` 里的 12 个定义：`edit`、`glob`、`grep`、`question`、`read`、`shell`、`skill`、`subagent`、`webfetch`、`websearch`、`write`、`execute`
- `stream: true`
- `stream_options.include_usage: true`
- `max_completion_tokens: 32000`
- 不带 `tool_choice`

`system.txt` 是某一次官方会话的原文，里面有当时的工作目录、日期和本机已安装的 skill。
服务端核对的是这段内容，不是“看起来像 OpenCode”就行。官方客户端升级后如果 429 回来，
按下面的步骤整段换掉，不要手改句子。

## 请求头

这些头官方客户端会带，但单独带上过不了门。重试时按这个集合发，不要再加
`x-opencode-request`。2.2.0 的二进制里没有这个头；旧源码里的 `msg_` 单号不是这扇门。

```
Authorization: Bearer public
User-Agent: opencode/latest/2.2.0/cli
x-opencode-client: cli
x-opencode-project: global
x-opencode-session: <ses_ id>
x-opencode-session-id: <同一个 ses_ id>
x-session-affinity: <同一个 ses_ id>
x-session-id: <同一个 ses_ id>
Accept: */*
Content-Type: application/json
```

`x-opencode-project` 用 `global` 即可，真实的 40 位工程哈希也可以。
免费请求用 `Bearer public`，不要把用户的 `oc_sk` key 带上，否则会离开免费额度。

普通路径上的 User-Agent 常量仍是 `opencode/latest/2.2.5/cli`
（`corelib.OpenCodeClientVersion`）。识别重试单独用 `2.2.0`，和抓到的客户端一致。

## 代码路径

直连 `*http.Transport`（包括没传入 client 时用的共享传输）时，Zen 免费模型只发识别后的那一次请求，不再先用 Go 的 TLS 打出一次必定 429 的请求。单测里的假 Transport 仍先把请求交给测试服务；那个服务若返回 `429` 且正文含 `FreeUsageLimitError`，才走识别客户端。

Maclaw GUI 的 OpenCode 服务商走同一入口。对话用 `DoOpenAIRequestStream`，连通性测试用 `DoOpenAIRequest`，这两处以及 GUI / agent 里自己 `client.Do` 的简单请求都会先调用 `TryOpenCodeFreeClient`。保存的地址仍可以是 `https://opencode.ai/zen/v1`，免费模型会被改写到标准推理接口，模型 id 保持 `-free`。CodexProxy 对 `opencode.ai` 上的免费模型同样调用 `CompleteOpenCodeFreeClient`；本机测试地址仍走原来的转发。

识别请求在 `CompleteOpenCodeFreeClient`。HubCenter 流式入口是 `streamOpenCodeFreeClient`。每次请求用一个新的 `ses_` id，不在所有调用方之间共用同一个会话。

`TigerProxy/auth_opencode.go` 还是旧的 Zen 协议，HubCenter 不走它。

## 已经排除的做法

- 把 `deepseek-v4.1-flash-free` 改成 `deepseek-v4.1-flash` 打到
  `/inference/go/openai`。那次实调返回过 `PING`，`billing_source=go`，会扣订阅额度。
- 免费 id 打到 `/zen/go` 或 `/inference/go`：`400 Model is unavailable`。
- 不带 `-free` 的 id 打到标准推理接口、账户没有按量余额：`402 Insufficient account funds`。
- 拉起本机 `opencode-cli.exe` 再解析它的 stdout。能返回内容，但不是 Go 客户端，已去掉。

## 刷新指纹和正文

官方客户端升级后免费请求又变成 429 时：

1. 在一个空目录放插件，钩住 `ctx.session.hook("http.request")`，把 `kind == "primary"`
   的 URL、请求头和 body 写到文件。Authorization 只记 `Bearer public` 或长度，不要落 key。
2. 另起一个只读首包的 TCP 端口。插件在 `setup` 里对 `https://127.0.0.1:<port>/` 做一次
   `fetch`（会因证书失败，无所谓）。把首包存成新的 `fetch.hello`。
   不要改成 `node:tls.connect`。
3. 用 uTLS `Fingerprinter` 读这个 hello，在扩展最前面加 SNI `opencode.ai`，
   把抓到的 body 重放到
   `https://opencode.ai/inference/openai/v1/chat/completions`。
   模型仍是 `deepseek-v4.1-flash-free`，期望 SSE 200。
4. 再发两份对照：系统提示词改短、以及工具换成一个空壳 `shell`。这两份仍应 429。
   如果也 200，说明门变了，不要沿用这份备忘。
5. 用抓到的 system 文本覆盖 `system.txt`，用 tools 数组覆盖 `tools.json`，
   换 `fetch.hello`。写文件用二进制，避免 Windows 把 `\n` 变成 `\r\n`。
6. `OPENCODE_LIVE=1 OPENCODE_KEY=public go test ./corelib/llm/ -run TestOpenCodeFreeModelLive`。
   期望状态 200，模型 id 仍带 `-free`，回复里有 `PING`。

2026-11-01 的验收就是这个测试：`POST .../inference/openai/v1/chat/completions`，
`model=deepseek-v4.1-flash-free`，状态 200。
