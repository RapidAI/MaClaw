# DS2API 接口文档

语言 / Language: [中文](API.md) | [English](API.en.md)

本文档描述当前 Go 代码库的实际 API 行为。

---

## 目录

- [基础信息](#基础信息)
- [配置最佳实践](#配置最佳实践)
- [鉴权规则](#鉴权规则)
- [路由总览](#路由总览)
- [健康检查](#健康检查)
- [OpenAI 兼容接口](#openai-兼容接口)
- [Claude 兼容接口](#claude-兼容接口)
- [Admin 接口](#admin-接口)
- [错误响应格式](#错误响应格式)
- [cURL 示例](#curl-示例)

---

## 基础信息

| 项目 | 说明 |
| --- | --- |
| Base URL | `http://localhost:5001` 或你的部署域名 |
| 默认 Content-Type | `application/json` |
| 健康检查 | `GET /healthz`、`GET /readyz` |
| CORS | 已启用（`Access-Control-Allow-Origin: *`，允许 `Content-Type`, `Authorization`, `X-API-Key`, `X-Ds2-Target-Account`, `X-Vercel-Protection-Bypass`） |

---

## 配置最佳实践

推荐把 `config.json` 作为唯一配置源：

```bash
cp config.example.json config.json
# 编辑 config.json（keys/accounts）
```

按部署方式使用：

- 本地运行：直接读取 `config.json`
- Docker / Vercel：从 `config.json` 生成 Base64，填入 `DS2API_CONFIG_JSON`

```bash
DS2API_CONFIG_JSON="$(base64 < config.json | tr -d '\n')"
```

Vercel 一键部署可先只填 `DS2API_ADMIN_KEY`，部署后在 `/admin` 导入配置，再通过 “Vercel 同步” 写回环境变量。

---

## 鉴权规则

### 业务接口（`/v1/*`、`/anthropic/*`）

支持两种传参方式：

| 方式 | 示例 |
| --- | --- |
| Bearer Token | `Authorization: Bearer <token>` |
| API Key Header | `x-api-key: <token>`（无 `Bearer` 前缀） |

**鉴权行为**：

- token 在 `config.keys` 中 → **托管账号模式**，自动轮询选择账号
- token 不在 `config.keys` 中 → **直通 token 模式**，直接作为 DeepSeek token 使用

**可选请求头**：`X-Ds2-Target-Account: <email_or_mobile>` — 指定使用某个托管账号。

### Admin 接口（`/admin/*`）

| 端点 | 鉴权 |
| --- | --- |
| `POST /admin/login` | 无需鉴权 |
| `GET /admin/verify` | `Authorization: Bearer <jwt>`（仅 JWT） |
| 其他 `/admin/*` | `Authorization: Bearer <jwt>` 或 `Authorization: Bearer <admin_key>`（直传管理密钥） |

---

## 路由总览

| 方法 | 路径 | 鉴权 | 说明 |
| --- | --- | --- | --- |
| GET | `/healthz` | 无 | 存活探针 |
| GET | `/readyz` | 无 | 就绪探针 |
| GET | `/v1/models` | 无 | OpenAI 模型列表 |
| GET | `/v1/models/{id}` | 无 | OpenAI 单模型查询（支持 alias 入参） |
| POST | `/v1/chat/completions` | 业务 | OpenAI 对话补全 |
| POST | `/v1/responses` | 业务 | OpenAI Responses 接口（流式/非流式） |
| GET | `/v1/responses/{response_id}` | 业务 | 查询已生成 response（内存 TTL） |
| POST | `/v1/embeddings` | 业务 | OpenAI Embeddings 接口 |
| GET | `/anthropic/v1/models` | 无 | Claude 模型列表 |
| POST | `/anthropic/v1/messages` | 业务 | Claude 消息接口 |
| POST | `/anthropic/v1/messages/count_tokens` | 业务 | Claude token 计数 |
| POST | `/admin/login` | 无 | 管理登录 |
| GET | `/admin/verify` | JWT | 校验管理 JWT |
| GET | `/admin/vercel/config` | Admin | 读取 Vercel 预配置 |
| GET | `/admin/config` | Admin | 读取配置（脱敏） |
| POST | `/admin/config` | Admin | 更新配置 |
| POST | `/admin/keys` | Admin | 添加 API key |
| DELETE | `/admin/keys/{key}` | Admin | 删除 API key |
| GET | `/admin/accounts` | Admin | 分页账号列表 |
| POST | `/admin/accounts` | Admin | 添加账号 |
| DELETE | `/admin/accounts/{identifier}` | Admin | 删除账号 |
| GET | `/admin/queue/status` | Admin | 账号队列状态 |
| POST | `/admin/accounts/test` | Admin | 测试单个账号 |
| POST | `/admin/accounts/test-all` | Admin | 测试全部账号 |
| POST | `/admin/import` | Admin | 批量导入 keys/accounts |
| POST | `/admin/test` | Admin | 测试当前 API 可用性 |
| POST | `/admin/vercel/sync` | Admin | 同步配置到 Vercel |
| GET | `/admin/vercel/status` | Admin | Vercel 同步状态 |
| GET | `/admin/export` | Admin | 导出配置 JSON/Base64 |

---

## 健康检查

### `GET /healthz`

```json
{"status": "ok"}
```

### `GET /readyz`

```json
{"status": "ready"}
```

---

## OpenAI 兼容接口

### `GET /v1/models`

无需鉴权。返回当前支持的模型列表。

**响应示例**：

```json
{
  "object": "list",
  "data": [
    {"id": "deepseek-chat", "object": "model", "created": 1677610602, "owned_by": "deepseek", "permission": []},
    {"id": "deepseek-reasoner", "object": "model", "created": 1677610602, "owned_by": "deepseek", "permission": []},
    {"id": "deepseek-chat-search", "object": "model", "created": 1677610602, "owned_by": "deepseek", "permission": []},
    {"id": "deepseek-reasoner-search", "object": "model", "created": 1677610602, "owned_by": "deepseek", "permission": []}
  ]
}
```

### 模型 alias 解析策略

对 `chat` / `responses` / `embeddings` 的 `model` 字段采用“宽进严出”：

1. 先匹配 DeepSeek 原生模型。
2. 再匹配 `model_aliases` 精确映射。
3. 未命中时按模型家族规则回退（如 `o*`、`gpt-*`、`claude-*`）。
4. 仍未命中则返回 `invalid_request_error`。

### `POST /v1/chat/completions`

**请求头**：

```http
Authorization: Bearer your-api-key
Content-Type: application/json
```

**请求体**：

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `model` | string | | 支持 DeepSeek 原生模型 + 常见 alias（如 `gpt-4o`、`gpt-5-codex`、`o3`、`claude-sonnet-4-5`） |
| `messages` | array | | OpenAI 风格消息数组 |
| `stream` | boolean | | 默认 `false` |
| `tools` | array | | Function Calling 定义 |
| `temperature` 等 | any | | 兼容透传字段（最终效果由上游决定） |

#### 非流式响应

```json
{
  "id": "<chat_session_id>",
  "object": "chat.completion",
  "created": 1738400000,
  "model": "deepseek-reasoner",
  "choices": [
    {
      "index": 0,
      "message": {
        "role": "assistant",
        "content": "最终回复",
        "reasoning_content": "思考内容（reasoner 模型）"
      },
      "finish_reason": "stop"
    }
  ],
  "usage": {
    "prompt_tokens": 10,
    "completion_tokens": 20,
    "total_tokens": 30,
    "completion_tokens_details": {
      "reasoning_tokens": 5
    }
  }
}
```

#### 流式响应（`stream=true`）

SSE 格式：每段为 `data: <json>\n\n`，结束为 `data: [DONE]`。

```text
data: {"id":"...","object":"chat.completion.chunk","choices":[{"delta":{"role":"assistant"},"index":0}]}

data: {"id":"...","object":"chat.completion.chunk","choices":[{"delta":{"reasoning_content":"..."},"index":0}]}

data: {"id":"...","object":"chat.completion.chunk","choices":[{"delta":{"content":"..."},"index":0}]}

data: {"id":"...","object":"chat.completion.chunk","choices":[{"delta":{},"index":0,"finish_reason":"stop"}],"usage":{...}}

data: [DONE]
```

**字段说明**：

- 首个 delta 包含 `role: assistant`
- `deepseek-reasoner` / `deepseek-reasoner-search` 模型输出 `delta.reasoning_content`
- 普通文本输出 `delta.content`
- 最后一段包含 `finish_reason` 和 `usage`

#### Tool Calls

当请求中含 `tools` 时，DS2API 做防泄漏处理：

**非流式**：识别到工具调用时，返回 `message.tool_calls`，设置 `finish_reason=tool_calls`，`message.content=null`。

```json
{
  "choices": [
    {
      "index": 0,
      "message": {
        "role": "assistant",
        "content": null,
        "tool_calls": [
          {
            "id": "call_xxx",
            "type": "function",
            "function": {
              "name": "get_weather",
              "arguments": "{\"city\":\"beijing\"}"
            }
          }
        ]
      },
      "finish_reason": "tool_calls"
    }
  ]
}
```

**流式**：命中高置信特征后立即输出 `delta.tool_calls`（不等待完整 JSON 闭合），并持续发送 arguments 增量；已确认的 toolcall 原始 JSON 不会回流到 `delta.content`。

---

### `GET /v1/models/{id}`

无需鉴权。入参支持 alias（例如 `gpt-4o`），返回的是映射后的 DeepSeek 模型对象。

### `POST /v1/responses`

OpenAI Responses 风格接口，兼容 `input` 或 `messages`。

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `model` | string | | 支持原生模型 + alias 自动映射 |
| `input` | string/array/object | | 与 `messages` 二选一 |
| `messages` | array | | 与 `input` 二选一 |
| `instructions` | string | | 自动前置为 system 消息 |
| `stream` | boolean | | 默认 `false` |
| `tools` | array | | 与 chat 同样的工具识别与转译策略 |

**非流式响应**：返回标准 `response` 对象，`id` 形如 `resp_xxx`，并写入内存 TTL 存储。

**流式响应（SSE）**：最小事件序列如下。

```text
event: response.created
data: {"type":"response.created","id":"resp_xxx","status":"in_progress",...}

event: response.output_text.delta
data: {"type":"response.output_text.delta","id":"resp_xxx","delta":"..."}

event: response.output_tool_call.delta
data: {"type":"response.output_tool_call.delta","id":"resp_xxx","tool_calls":[...]}

event: response.completed
data: {"type":"response.completed","response":{...}}

data: [DONE]
```

### `GET /v1/responses/{response_id}`

需要业务鉴权。查询 `POST /v1/responses` 生成并缓存的 response 对象（按调用方鉴权隔离，仅同一 key/token 可读取）。

> 当前为内存 TTL 存储，默认过期时间 `900s`（可用 `responses.store_ttl_seconds` 调整）。

### `POST /v1/embeddings`

需要业务鉴权。返回 OpenAI Embeddings 兼容结构。

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `model` | string | | 支持原生模型 + alias 自动映射 |
| `input` | string/array | | 支持字符串、字符串数组、token 数组 |

> 需配置 `embeddings.provider`。当前支持：`mock` / `deterministic` / `builtin`。未配置或不支持时返回标准错误结构（HTTP 501）。

---

## Claude 兼容接口

### `GET /anthropic/v1/models`

无需鉴权。

**响应示例**：

```json
{
  "object": "list",
  "data": [
    {"id": "claude-sonnet-4-5", "object": "model", "created": 1715635200, "owned_by": "anthropic"},
    {"id": "claude-haiku-4-5", "object": "model", "created": 1715635200, "owned_by": "anthropic"},
    {"id": "claude-opus-4-6", "object": "model", "created": 1715635200, "owned_by": "anthropic"}
  ],
  "first_id": "claude-opus-4-6",
  "last_id": "claude-instant-1.0",
  "has_more": false
}
```

> 说明：示例仅展示部分模型；实际返回包含 Claude 1.x/2.x/3.x/4.x 历史模型 ID 与常见别名。

### `POST /anthropic/v1/messages`

**请求头**：

```http
x-api-key: your-api-key
Content-Type: application/json
anthropic-version: 2023-06-01
```

> `anthropic-version` 可省略，服务端会自动补为 `2023-06-01`。

**请求体**：

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `model` | string | | 例如 `claude-sonnet-4-5` / `claude-opus-4-6` / `claude-haiku-4-5`（兼容 `claude-3-5-haiku-latest`），并支持历史 Claude 模型 ID |
| `messages` | array | | Claude 风格消息数组 |
| `max_tokens` | number | | 缺省自动补 `8192`；当前实现不会硬性截断上游输出 |
| `stream` | boolean | | 默认 `false` |
| `system` | string | | 可选系统提示 |
| `tools` | array | | Claude tool 定义 |

#### 非流式响应

```json
{
  "id": "msg_1738400000000000000",
  "type": "message",
  "role": "assistant",
  "model": "claude-sonnet-4-5",
  "content": [
    {"type": "text", "text": "回复内容"}
  ],
  "stop_reason": "end_turn",
  "stop_sequence": null,
  "usage": {
    "input_tokens": 12,
    "output_tokens": 34
  }
}
```

若识别到工具调用，`stop_reason=tool_use`，`content` 中返回 `tool_use` block。

#### 流式响应（`stream=true`）

SSE 使用 `event:` + `data:` 双行格式，JSON 中保留 `type` 字段。

```text
event: message_start
data: {"type":"message_start","message":{...}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hello"}}

event: ping
data: {"type":"ping"}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":12}}

event: message_stop
data: {"type":"message_stop"}
```

**说明**：

- 名称中包含 `opus` / `reasoner` / `slow` 的模型会输出 `thinking_delta`
- 不会输出 `signature_delta`（上游 DeepSeek 未提供可验证签名）
- `tools` 场景优先避免泄露原始工具 JSON，不强制发送 `input_json_delta`

### `POST /anthropic/v1/messages/count_tokens`

**请求**：

```json
{
  "model": "claude-sonnet-4-5",
  "messages": [
    {"role": "user", "content": "你好"}
  ]
}
```

**响应**：

```json
{
  "input_tokens": 5
}
```

---

## Admin 接口

### `POST /admin/login`

无需鉴权。

**请求**：

```json
{
  "admin_key": "admin",
  "expire_hours": 24
}
```

`expire_hours` 可省略，默认 `24`。

**响应**：

```json
{
  "success": true,
  "token": "<jwt>",
  "expires_in": 86400
}
```

### `GET /admin/verify`

需要 JWT：`Authorization: Bearer <jwt>`

**响应**：

```json
{
  "valid": true,
  "expires_at": 1738400000,
  "remaining_seconds": 72000
}
```

### `GET /admin/vercel/config`

返回 Vercel 预配置状态。

```json
{
  "has_token": true,
  "project_id": "prj_xxx",
  "team_id": null
}
```

### `GET /admin/config`

返回脱敏后的配置。

```json
{
  "keys": ["k1", "k2"],
  "accounts": [
    {
      "identifier": "user@example.com",
      "email": "user@example.com",
      "mobile": "",
      "has_password": true,
      "has_token": true,
      "token_preview": "abcde..."
    }
  ],
  "claude_mapping": {
    "fast": "deepseek-chat",
    "slow": "deepseek-reasoner"
  }
}
```

### `POST /admin/config`

可更新 `keys`、`accounts`、`claude_mapping`。

**请求**：

```json
{
  "keys": ["k1", "k2"],
  "accounts": [
    {"email": "user@example.com", "password": "pwd", "token": ""}
  ],
  "claude_mapping": {
    "fast": "deepseek-chat",
    "slow": "deepseek-reasoner"
  }
}
```

### `POST /admin/keys`

```json
{"key": "new-api-key"}
```

**响应**：`{"success": true, "total_keys": 3}`

### `DELETE /admin/keys/{key}`

**响应**：`{"success": true, "total_keys": 2}`

### `GET /admin/accounts`

**查询参数**：

| 参数 | 默认 | 范围 |
| --- | --- | --- |
| `page` | `1` | ≥ 1 |
| `page_size` | `10` | 1–100 |

**响应**：

```json
{
  "items": [
    {
      "identifier": "user@example.com",
      "email": "user@example.com",
      "mobile": "",
      "has_password": true,
      "has_token": true,
      "token_preview": "abc..."
    }
  ],
  "total": 25,
  "page": 1,
  "page_size": 10,
  "total_pages": 3
}
```

### `POST /admin/accounts`

```json
{"email": "user@example.com", "password": "pwd"}
```

**响应**：`{"success": true, "total_accounts": 6}`

### `DELETE /admin/accounts/{identifier}`

`identifier` 可为 email、mobile，或 token-only 账号的合成标识（`token:<hash>`）。

**响应**：`{"success": true, "total_accounts": 5}`

### `GET /admin/queue/status`

```json
{
  "available": 3,
  "in_use": 1,
  "total": 4,
  "available_accounts": ["a@example.com"],
  "in_use_accounts": ["b@example.com"],
  "max_inflight_per_account": 2,
  "recommended_concurrency": 8
}
```

| 字段 | 说明 |
| --- | --- |
| `available` | 当前可用账号数 |
| `in_use` | 当前使用中的账号数 |
| `total` | 总账号数 |
| `max_inflight_per_account` | 每账号并发上限 |
| `recommended_concurrency` | 建议并发值（`total × max_inflight_per_account`） |

### `POST /admin/accounts/test`

| 字段 | 必填 | 说明 |
| --- | --- | --- |
| `identifier` | | email / mobile / token-only 合成标识 |
| `model` | | 默认 `deepseek-chat` |
| `message` | | 空字符串时仅测试会话创建 |

**响应**：

```json
{
  "account": "user@example.com",
  "success": true,
  "response_time": 1240,
  "message": "API 测试成功（仅会话创建）",
  "model": "deepseek-chat"
}
```

### `POST /admin/accounts/test-all`

可选请求字段：`model`

```json
{
  "total": 5,
  "success": 4,
  "failed": 1,
  "results": [...]
}
```

### `POST /admin/import`

批量导入 keys 与 accounts。

**请求**：

```json
{
  "keys": ["k1", "k2"],
  "accounts": [
    {"email": "user@example.com", "password": "pwd", "token": ""}
  ]
}
```

**响应**：

```json
{
  "success": true,
  "imported_keys": 2,
  "imported_accounts": 1
}
```

### `POST /admin/test`

测试当前 API 可用性（通过自身接口调用）。

| 字段 | 必填 | 默认值 |
| --- | --- | --- |
| `model` | | `deepseek-chat` |
| `message` | | `你好` |
| `api_key` | | 配置中第一个 key |

**响应**：

```json
{
  "success": true,
  "status_code": 200,
  "response": {"id": "..."}
}
```

### `POST /admin/vercel/sync`

| 字段 | 必填 | 说明 |
| --- | --- | --- |
| `vercel_token` | | 空或 `__USE_PRECONFIG__` 则读环境变量 |
| `project_id` | | 空则读 `VERCEL_PROJECT_ID` |
| `team_id` | | 空则读 `VERCEL_TEAM_ID` |
| `auto_validate` | | 默认 `true` |
| `save_credentials` | | 默认 `true` |

**成功响应**：

```json
{
  "success": true,
  "validated_accounts": 3,
  "message": "配置已同步，正在重新部署...",
  "deployment_url": "https://..."
}
```

或需要手动部署：

```json
{
  "success": true,
  "validated_accounts": 3,
  "message": "配置已同步到 Vercel，请手动触发重新部署",
  "manual_deploy_required": true
}
```

### `GET /admin/vercel/status`

```json
{
  "synced": true,
  "last_sync_time": 1738400000,
  "has_synced_before": true
}
```

### `GET /admin/export`

```json
{
  "json": "{...}",
  "base64": "ey4uLn0="
}
```

---

## 错误响应格式

兼容路由（`/v1/*`、`/anthropic/*`）统一使用以下结构：

```json
{
  "error": {
    "message": "...",
    "type": "invalid_request_error",
    "code": "invalid_request",
    "param": null
  }
}
```

Admin 接口保持 `{"detail":"..."}`。

建议客户端处理逻辑：检查 HTTP 状态码 + 解析 `error` 或 `detail` 字段。

**常见状态码**：

| 状态码 | 说明 |
| --- | --- |
| `401` | 鉴权失败（key/token 无效，或 Admin JWT 过期） |
| `429` | 请求过多（超出并发上限 + 等待队列） |
| `503` | 模型不可用或上游服务异常 |

---

## cURL 示例

### OpenAI 非流式

```bash
curl http://localhost:5001/v1/chat/completions \
  -H "Authorization: Bearer your-api-key" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "deepseek-chat",
    "messages": [{"role": "user", "content": "你好"}],
    "stream": false
  }'
```

### OpenAI 流式

```bash
curl http://localhost:5001/v1/chat/completions \
  -H "Authorization: Bearer your-api-key" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "deepseek-reasoner",
    "messages": [{"role": "user", "content": "解释一下量子纠缠"}],
    "stream": true
  }'
```

### OpenAI Responses（流式）

```bash
curl http://localhost:5001/v1/responses \
  -H "Authorization: Bearer your-api-key" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-5-codex",
    "input": "写一个 golang 的 hello world",
    "stream": true
  }'
```

### OpenAI Embeddings

```bash
curl http://localhost:5001/v1/embeddings \
  -H "Authorization: Bearer your-api-key" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4o",
    "input": ["第一段文本", "第二段文本"]
  }'
```

### OpenAI 带搜索

```bash
curl http://localhost:5001/v1/chat/completions \
  -H "Authorization: Bearer your-api-key" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "deepseek-chat-search",
    "messages": [{"role": "user", "content": "今天的新闻"}],
    "stream": true
  }'
```

### OpenAI Tool Calling

```bash
curl http://localhost:5001/v1/chat/completions \
  -H "Authorization: Bearer your-api-key" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "deepseek-chat",
    "messages": [{"role": "user", "content": "北京今天天气怎么样？"}],
    "tools": [
      {
        "type": "function",
        "function": {
          "name": "get_weather",
          "description": "获取指定城市的天气",
          "parameters": {
            "type": "object",
            "properties": {
              "city": {"type": "string", "description": "城市名"}
            },
            "required": ["city"]
          }
        }
      }
    ]
  }'
```

### Claude 非流式

```bash
curl http://localhost:5001/anthropic/v1/messages \
  -H "x-api-key: your-api-key" \
  -H "Content-Type: application/json" \
  -H "anthropic-version: 2023-06-01" \
  -d '{
    "model": "claude-sonnet-4-5",
    "max_tokens": 1024,
    "messages": [{"role": "user", "content": "你好"}]
  }'
```

### Claude 流式

```bash
curl http://localhost:5001/anthropic/v1/messages \
  -H "x-api-key: your-api-key" \
  -H "Content-Type: application/json" \
  -H "anthropic-version: 2023-06-01" \
  -d '{
    "model": "claude-opus-4-6",
    "max_tokens": 1024,
    "messages": [{"role": "user", "content": "解释相对论"}],
    "stream": true
  }'
```

### Admin 登录

```bash
curl http://localhost:5001/admin/login \
  -H "Content-Type: application/json" \
  -d '{"admin_key": "admin"}'
```

### 指定账号请求

```bash
curl http://localhost:5001/v1/chat/completions \
  -H "Authorization: Bearer your-api-key" \
  -H "X-Ds2-Target-Account: user@example.com" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "deepseek-chat",
    "messages": [{"role": "user", "content": "你好"}]
  }'
```
