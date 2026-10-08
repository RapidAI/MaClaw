# 服务商接入：Trae（国内/国际）与 LobsterAI 协议备忘

> 本文档记录 2026-10 新增的两个 OAuth 服务商的协议快照与维护要点。协议知识以代码内的
> 包注释为准（`corelib/trae` / `corelib/lobsterai`），本文是运维排查与再逆向的索引。

## 概览

| 服务商 | 名称常量 | 凭据存储 ID | 默认模型 | 上游协议 |
|---|---|---|---|---|
| Trae 国内版 | `trae.NameCN` | `trae-cn` | `glm-5.2` | SOLO 自有（`llm_utils_chat` + Cloud-IDE-JWT 头族） |
| Trae 国际版 | `trae.NameGlobal` | `trae-global` | `gpt-5` | 同上，Singapore 部署 |
| LobsterAI | `lobsterai.Name` | `lobsterai` | `glm-5.3` | OpenAI 形状（`/api/proxy/v1/chat/completions`） |

两家的对话适配层都走 WorkBuddy 同款模式：`WrapClient` 给引擎的 http.Client
套 `Transport`，把 OpenAI 请求改写为上游协议、把上游响应（SSE 或 JSON）译回
OpenAI。接线点与 WorkBuddy 相同：`corelib/llm/openai_sdk.go`、
`corelib/agent/llm_helper.go`、`guiapp/llm_request_helper.go`。

## Trae

来源：2026-09~10 社区逆向抓包（traework2api / trae2api-web / trae-workbuddy-switch）。

关键常数（`corelib/trae/profile.go`，疑似漂移先看这里）：

| 项 | 国内 | 国际 |
|---|---|---|
| 授权页（console_base） | `https://www.trae.cn` | `https://www.trae.ai` |
| 账号/令牌（auth_base） | `https://api.trae.cn`（备 `api.trae.com.cn`） | `https://growsg-normal.trae.ai` |
| SOLO 对话（chat_host） | `https://trae-api-cn.mchost.guru` | `https://coresg-normal.trae.ai` |
| IDE 版本头 | `3.3.67` | `3.5.51` |
| OAuth client | `en1oxy7wnw8j9n`（SOLO 线，双区共用） | 同左 |

登录（**2026-10-08 真机校准后的简化契约**）：`{console_base}/authorization?...`
**18 参数、不带 PKCE challenge**。真机实测：带 `code_challenge` 会把控制台
引入“新协议”——回发 AuthCode，其交换要求已注册设备证明（`DevicePublicKey`
/DeviceProof），MaClaw 无法生成，国内版报 400/10101「无效参数」、国际版报
401/20405「Device proof required」。去掉 challenge 后控制台直接回发
`refreshToken`（或 `userJwt` JSON），走旧契约即可交换。本机监听 127.0.0.1
（默认 18080，可上移；另尽力应答上游写死的 17388 在线探测口，否则页面会卡
「认证中」）。

刷新：`{auth_host}/cloudide/api/v3/trae/oauth/ExchangeToken`，请求体
`{ClientID, RefreshToken, ClientSecret:"-"}`，双 token 轮换。国内版该契约
**双 host 并存**（`api.trae.cn` 与 `api.trae.com.cn`，`Profile.AuthHosts()`
按序探测）。若实测报 10101（client 不匹配），考虑把 `Profile.ClientID`
换 IDE 线钥匙 `ono9krqynydwx5`。

设备指纹：**登录页 machine/device 对是权威**——refresh token 系于其上，
登录成功后写入凭据存储（`StoredCredential.MachineID/DeviceID`），
materialize 时传入 `MaclawLLMConfig.Trae*`，聊天/目录请求头复用同一对
（引擎经 `trae.ApplyHeaders` 打头，Transport 优先保留预置对，claims 派生
仅作无存储时的兜底）。

已知流内错误码（`event:error`）：1005 套餐额度不足、4001 参数/模型名无效、
1001 认证失效、4017 设备风控。模型目录：`{chat_host}/api/ide/v1/get_detail_param`
（`function: "solo_work_lite"`）。

## LobsterAI

来源：官方开源仓 `netease-youdao/LobsterAI` + 社区 lobsterai2api。
无国内/国际之分。

| 项 | 常数（`corelib/lobsterai` 包头） |
|---|---|
| 登录门户 | `https://lobsterai.youdao.com/portal#/login?source=electron` |
| API 基址 | `https://lobsterai-server.youdao.com` |
| 登录交换 | `POST /api/auth/exchange`（authCode + uuid + firstKeyfrom） |
| 刷新 | `POST /api/auth/refresh`（refreshToken + uuid + userId，双 token 轮换） |
| 对话 | `POST /api/proxy/v1/chat/completions`（Bearer + `X-LobsterAI-Client-*`） |
| 模型目录 | `GET /api/models/available`（需 `X-LobsterAI-Client-Capabilities` 头） |
| 每日签到/积分 | `/api/client-activities/...`、`/api/user/profile-summary`（本版未做） |

注意事项：
- 上游**只支持流式**（stream:false 返回 500），非流式调用由 Transport 聚合。
- 业务错误藏在 HTTP 200 的 SSE 流首（额度 40201、未知模型 40300 等），
  流式路径由 `streamerror.go` 过滤器译成 OpenAI error 块。
- `uuid`/`firstKeyfrom` 是续期身份对，随凭据存入 credential store
  （`oauth.StoredCredential` 的 `uuid`/`first_keyfrom` 字段）。
- `clientVersion` 头固定 `0.1.0`（对话端接受陈旧值；只有签到活动要求实时版本）。
- `/api/user/quota` 只有 300 免费积分，不含活动积分——对账要用
  `/api/user/profile-summary` 的 `totalCreditsRemaining`。

## 端到端验收清单

1. 设置面板：用 Trae 国内/国际、LobsterAI 各登录一次 → 提示“登录成功；模型测试通过”。
2. 对话：跑一次含工具调用的 Agent 任务（tool_calls 双向转换）。
3. “获取模型”：目录请求返回所选区/账号的真实清单。
4. 配置向导：同一服务商走一遍（分发已统一到 `runProviderOAuthLogin`）。
5. 过期演练：把 credential store 的 `expires_at` 改到过期 → 下次请求自动刷新。
6. 代理开启时登录/对话仍通（所有 OAuth/目录请求走 oauth 代理感知客户端）。

## 回归底线

登录链路有真实 socket 的端到端测试，协议回归会先在这里红：

- `corelib/trae/login_e2e_test.go`：成功全程（URL→回调→交换→登录设备对）、
  CSRF（loginTraceID）、AuthCode 契约拒绝、Wait 超时。
- `corelib/lobsterai/login_e2e_test.go`：portal 深链→回调（错误 state 先拒）
  → `/api/auth/exchange`（uuid/firstKeyfrom 身份对）→ 账号视图。
- `corelib/trae/auth_test.go` / `transport_test.go`：18 参数形态（禁
  code_challenge/hide_saas_login/x_env/channel_name）、Result 信封容错、
  OpenAI↔SOLO 体翻译与 SSE 跨块扫描、引擎预置设备对优先。
- `corelib/lobsterai/transport_test.go` / `streamerror_test.go`：流首错误帧
  过滤（含注释头不提前判决）、非流式聚合、逐块边界。
- `go test ./corelib/trae/ ./corelib/lobsterai/` 是最小回归命令。

## 协议漂移时的改动面

- 登录/交换参数：`corelib/trae/profile.go` 常数 + `corelib/trae/auth.go`
  （URL 构造 / `tokenFromExchange` 容错解析）。
- 对话协议：`corelib/trae/transport.go`（`prepareBody` / sseScanner）。
- LobsterAI 头族/路径：`corelib/lobsterai/profile.go` + `transport.go`。
- 测试是回归底线：`go test ./corelib/trae/ ./corelib/lobsterai/`。
