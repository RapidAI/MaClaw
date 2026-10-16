# Qoder 签名请求契约（逆向实录）

来源：官方 CLI 实机抓取 —— `qoderclicn 1.1.64`（国内版，qoder.cn）与
`qodercli 1.1.65`（国际版，qoder.com），2026-10-07 在 Windows x64 上验证。
Go 侧实现：`corelib/qoder/cosy`（wazero 内嵌官方 `qoder_auth_wasm`）。

## 端点拓扑

| 用途 | host | 路径 |
| --- | --- | --- |
| 设备登录授权页 | qoder.cn / qoder.com | `/device/selectAccounts?challenge=…&challenge_method=S256&nonce=…&machine_id=…&client_id=…` |
| 设备令牌轮询/刷新 | openapi.qoder.com.cn / openapi.qoder.sh | `/api/v1/deviceToken/poll`、`/api/v1/deviceToken/refresh` |
| 账号信息（plan/额度） | 同上 | `/api/v1/userinfo`、`/api/v2/user/plan`、`/api/v3/user/status` |
| 模型目录 | gateway.qoder.com.cn / api2.qoder.sh | **`/algo/api/v2/model/list`**（无 `?Encode=1`） |
| 聊天 | api2-v2.qoder.sh | `/algo/api/v2/service/pro/sse/agent_chat_generation?FetchKeys=llm_model_result&AgentId=agent_common&Encode=1` |

要点：
- **所有推理类请求路径带 `/algo` 前缀**。官方日志显示的是剥离后的路径
  （`/api/v2/model/list`），但实际发出的 URL 有 `/algo`——少了它就是 404。
- 轮询响应里 `expires_at` 是 **RFC3339 字符串**（`2026-11-06T21:19:03Z`），
  `expires_in` 是**毫秒**（2591999999 ≈ 30 天）。
- 设备登录两个版本共用同一个 client_id：`e883ade2-e6e3-4d6d-adf7-f92ceff5fdcb`。
  旧 id `e93fe488-…` 仍内嵌于两个二进制，但 qoder.cn 对它返回"参数无效"。

## 签名头（wasm 生成，18–20 个）

```
Authorization: Bearer COSY.<base64(payload)>.<hex 签名>
Cosy-Business-Product: cli
Cosy-Business-Type: agent
Cosy-ClientIp: <machineId>
Cosy-ClientType: 5
Cosy-Data-Policy: disagree|agree
Cosy-Date: <unix 秒>
Cosy-Key: <base64>
Cosy-MachineId: <machineId>
… （其余为身份/追踪头）
```

COSY payload 解码后：`{"version":"v1","requestId":"<uuid>","info":"<base64 加密材料>","cosyVersion":"1.1.64","ideVersion":""}`。
`info` 里折叠了 uid + 设备 token，用当前上下文的 key 加密——服务端校验
COSY blob 与 `Cosy-Key` 的自洽性（用 freshly 生成的材料即可通过）。

## 聊天 wire 格式（二阶段待办）

1. POST body **不是** JSON：由 wasm `prepareInferRequest` 编码成自定义字符集
   文本（如 `NH%LJHLbuEV$G(jn#xpb#…`，`Encode=1` 语义）。
2. 头里出现 `X-Model-Key`（含编码值，非 ASCII，需按原样发送）。
3. 响应为 SSE 流；哨兵值：`[DONE]`、`[NOT_EXCEED_QUOTA]`、`[EXCEED_QUOTA]…`、
   `[NOTIFICATIONS]`；错误事件形如 `{statusCodeValue, body}`。
4. 部分响应需要 `decrypt_server_response`（wasm 导出）解密。

### 调用链（已还原并实测打通）

```
NHA(m=推理host origin, b=JSON.stringify(body), k=model_config.key, v=model_config.source="system")
  → wasm prepareInferRequest(m, b, k, v)      (注意第一参给 origin，勿带路径)
  → {url, headers(20 个签名头), body(编码文本)}
```

**聊天 host 按版本**（实测确认）：
- 国内版：`https://gateway.qoder.com.cn`（与目录同 host）
- 国际版：`https://api2-v2.qoder.sh`
原始 URL 由 wasm 定型为 `<host>/algo/api/v2/service/pro/sse/agent_chat_generation
?FetchKeys=llm_model_result&AgentId=agent_common&Encode=1`。

### 请求体（实测打通的最小形状，普通 OpenAI content 形态直通）

```json
{"model":"<模型 key，如 qfmodel|auto>",
 "stream":true,"temperature":0.7,"max_tokens":64,
 "request_id":"<uuid>","request_set_id":"<uuid>","session_id":"<uuid>","task_id":"",
 "business":{"type":"agent","scene":"cli"},
 "messages":[{"role":"user","content":"<文本>"}],
 "model_config":{"key":"<同 model>","source":"system"},
 "data_policy_agreed":true}
```

注意：`messages[].content` 直接生效；`text_content` 会被上游丢掉内容并触发
"Role must be in … and the role in last message must be in [user, function,
tool]"（算法网关把内容为空的消息降级处理）。`contents` 字段则命中 fastjson
解析错误。响应不加密——SSE 信封 `data:{"headers":{…},"body":"<内层 JSON>"}`，
内层为标准 OpenAI chunk（choices/delta/finish_reason/usage），终止事件
`[DONE]` 与一个 metrics 事件 `{"firstTokenDuration":…,"totalDuration":…}`。


## Go 实现（已落地，`corelib/qoder` + `corelib/qoder/cosy`）

- `qoder.ListModels(ctx, profile, uid, token)` — 全量模型目录（CN 113 / 国际 104 条）。
- `qoder.ChatProbe(ctx, profile, uid, token, model, prompt)` — 单轮非流式探测，
  已接入 `TestMaclawLLM`（`IsChatBaseURL` 分支 → `testQoderLLM`）：`检测并保存` 通过。
  模型 key 沿 `canonicalQoderModelKey` 规范化（遗留命名 qwen3.8-max→qmodel_38max 等），
  Cosy-Data-Policy 头随数据政策态发送 agree（默认 disagree）。
- `qoder.Transport` — agent 循环的 OpenAI→algo 双向翻译桥（WrapClientForConfig
  在 wrap 时锁定 uid+edition）。
- `qoder.Transport`（`WrapClientForConfig`）— agent 循环零改动的 OpenAI→algo 翻译桥：
  拦截 ChatBase 的 POST，`prepareAgentChatBody`（含遗留模型名映射
  qwen3.8-max→qmodel_38max 等），`prepareInferRequest` 签名+编码，
  `unwrapSSE` 把信封解回标准 OpenAI 流；**聊天 gateway 按 token 所属 edition 选**
  （CN→gateway.qoder.com.cn，intl→api2-v2.qoder.sh，与 URL front 无关——
  两个 provider 共用一个 ChatBase）。
- agent 环流验证：GUI 配置 URL + CN 凭证 → 流式回复 ok →agent-loop wire OK。

## 复刻路径（已实现，见 cosy 包）

- Node 侧先行验证：提取二进制内联 base64 的 `qoder_auth_wasm_bg.wasm`
  （298,606 字节），配最小 wasm-bindgen shim 即可离线生成签名请求；
  CN host 重放 → 200 + 67KB 全量目录。
- Go 侧 `cosy.New(machineID, cosyVersion, uid)` → `RefreshAuthFields(token)`
  → `PrepareRequest(endpoint, path, method, "auth", bodyJSON, extraJSON)`。
- wasm 的 31 个 import 全部为标准 wasm-bindgen crypto/node shim，
  `runtime.go` 按 glue 的堆约定复刻（槽 0..1023 undefined、1025=null、
  1026/1027=true/false、自由链表头从 1028 起）。
