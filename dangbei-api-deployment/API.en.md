# DS2API API Reference

Language: [中文](API.md) | [English](API.en.md)

This document describes the actual behavior of the current Go codebase.

---

## Table of Contents

- [Basics](#basics)
- [Configuration Best Practice](#configuration-best-practice)
- [Authentication](#authentication)
- [Route Index](#route-index)
- [Health Endpoints](#health-endpoints)
- [OpenAI-Compatible API](#openai-compatible-api)
- [Claude-Compatible API](#claude-compatible-api)
- [Admin API](#admin-api)
- [Error Payloads](#error-payloads)
- [cURL Examples](#curl-examples)

---

## Basics

| Item | Details |
| --- | --- |
| Base URL | `http://localhost:5001` or your deployment domain |
| Default Content-Type | `application/json` |
| Health probes | `GET /healthz`, `GET /readyz` |
| CORS | Enabled (`Access-Control-Allow-Origin: *`, allows `Content-Type`, `Authorization`, `X-API-Key`, `X-Ds2-Target-Account`, `X-Vercel-Protection-Bypass`) |

---

## Configuration Best Practice

Use `config.json` as the single source of truth:

```bash
cp config.example.json config.json
# Edit config.json (keys/accounts)
```

Use it per deployment mode:

- Local run: read `config.json` directly
- Docker / Vercel: generate Base64 from `config.json`, then set `DS2API_CONFIG_JSON`

```bash
DS2API_CONFIG_JSON="$(base64 < config.json | tr -d '\n')"
```

For Vercel one-click bootstrap, you can set only `DS2API_ADMIN_KEY` first, then import config at `/admin` and sync env vars from the "Vercel Sync" page.

---

## Authentication

### Business Endpoints (`/v1/*`, `/anthropic/*`)

Two header formats accepted:

| Method | Example |
| --- | --- |
| Bearer Token | `Authorization: Bearer <token>` |
| API Key Header | `x-api-key: <token>` (no `Bearer` prefix) |

**Auth behavior**:

- Token is in `config.keys` → **Managed account mode**: DS2API auto-selects an account via rotation
- Token is not in `config.keys` → **Direct token mode**: treated as a DeepSeek token directly

**Optional header**: `X-Ds2-Target-Account: <email_or_mobile>` — Pin a specific managed account.

### Admin Endpoints (`/admin/*`)

| Endpoint | Auth |
| --- | --- |
| `POST /admin/login` | Public |
| `GET /admin/verify` | `Authorization: Bearer <jwt>` (JWT only) |
| Other `/admin/*` | `Authorization: Bearer <jwt>` or `Authorization: Bearer <admin_key>` |

---

## Route Index

| Method | Path | Auth | Description |
| --- | --- | --- | --- |
| GET | `/healthz` | None | Liveness probe |
| GET | `/readyz` | None | Readiness probe |
| GET | `/v1/models` | None | OpenAI model list |
| GET | `/v1/models/{id}` | None | OpenAI single-model query (alias accepted) |
| POST | `/v1/chat/completions` | Business | OpenAI chat completions |
| POST | `/v1/responses` | Business | OpenAI Responses API (stream/non-stream) |
| GET | `/v1/responses/{response_id}` | Business | Query stored response (in-memory TTL) |
| POST | `/v1/embeddings` | Business | OpenAI Embeddings API |
| GET | `/anthropic/v1/models` | None | Claude model list |
| POST | `/anthropic/v1/messages` | Business | Claude messages |
| POST | `/anthropic/v1/messages/count_tokens` | Business | Claude token counting |
| POST | `/admin/login` | None | Admin login |
| GET | `/admin/verify` | JWT | Verify admin JWT |
| GET | `/admin/vercel/config` | Admin | Read preconfigured Vercel creds |
| GET | `/admin/config` | Admin | Read sanitized config |
| POST | `/admin/config` | Admin | Update config |
| POST | `/admin/keys` | Admin | Add API key |
| DELETE | `/admin/keys/{key}` | Admin | Delete API key |
| GET | `/admin/accounts` | Admin | Paginated account list |
| POST | `/admin/accounts` | Admin | Add account |
| DELETE | `/admin/accounts/{identifier}` | Admin | Delete account |
| GET | `/admin/queue/status` | Admin | Account queue status |
| POST | `/admin/accounts/test` | Admin | Test one account |
| POST | `/admin/accounts/test-all` | Admin | Test all accounts |
| POST | `/admin/import` | Admin | Batch import keys/accounts |
| POST | `/admin/test` | Admin | Test API through service |
| POST | `/admin/vercel/sync` | Admin | Sync config to Vercel |
| GET | `/admin/vercel/status` | Admin | Vercel sync status |
| GET | `/admin/export` | Admin | Export config JSON/Base64 |

---

## Health Endpoints

### `GET /healthz`

```json
{"status": "ok"}
```

### `GET /readyz`

```json
{"status": "ready"}
```

---

## OpenAI-Compatible API

### `GET /v1/models`

No auth required. Returns supported models.

**Response**:

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

### Model Alias Resolution

For `chat` / `responses` / `embeddings`, DS2API follows a wide-input/strict-output policy:

1. Match DeepSeek native model IDs first.
2. Then match exact keys in `model_aliases`.
3. If still unmatched, fall back by known family heuristics (`o*`, `gpt-*`, `claude-*`, etc.).
4. If still unmatched, return `invalid_request_error`.

### `POST /v1/chat/completions`

**Headers**:

```http
Authorization: Bearer your-api-key
Content-Type: application/json
```

**Request body**:

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `model` | string | | DeepSeek native models + common aliases (`gpt-4o`, `gpt-5-codex`, `o3`, `claude-sonnet-4-5`, etc.) |
| `messages` | array | | OpenAI-style messages |
| `stream` | boolean | | Default `false` |
| `tools` | array | | Function calling schema |
| `temperature`, etc. | any | | Accepted but final behavior depends on upstream |

#### Non-Stream Response

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
        "content": "final response",
        "reasoning_content": "reasoning trace (reasoner models)"
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

#### Streaming (`stream=true`)

SSE format: each frame is `data: <json>\n\n`, terminated by `data: [DONE]`.

```text
data: {"id":"...","object":"chat.completion.chunk","choices":[{"delta":{"role":"assistant"},"index":0}]}

data: {"id":"...","object":"chat.completion.chunk","choices":[{"delta":{"reasoning_content":"..."},"index":0}]}

data: {"id":"...","object":"chat.completion.chunk","choices":[{"delta":{"content":"..."},"index":0}]}

data: {"id":"...","object":"chat.completion.chunk","choices":[{"delta":{},"index":0,"finish_reason":"stop"}],"usage":{...}}

data: [DONE]
```

**Field notes**:

- First delta includes `role: assistant`
- `deepseek-reasoner` / `deepseek-reasoner-search` models emit `delta.reasoning_content`
- Text emits `delta.content`
- Last chunk includes `finish_reason` and `usage`

#### Tool Calls

When `tools` is present, DS2API performs anti-leak handling:

**Non-stream**: If detected, returns `message.tool_calls`, `finish_reason=tool_calls`, `message.content=null`.

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

**Stream**: Once high-confidence toolcall features are matched, DS2API emits `delta.tool_calls` immediately (without waiting for full JSON closure), then keeps sending argument deltas; confirmed raw tool JSON is never forwarded as `delta.content`.

---

### `GET /v1/models/{id}`

No auth required. Alias values are accepted as path params (for example `gpt-4o`), and the returned object is the mapped DeepSeek model.

### `POST /v1/responses`

OpenAI Responses-style endpoint, accepting either `input` or `messages`.

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `model` | string | | Supports native models + alias mapping |
| `input` | string/array/object | | One of `input` or `messages` is required |
| `messages` | array | | One of `input` or `messages` is required |
| `instructions` | string | | Prepended as a system message |
| `stream` | boolean | | Default `false` |
| `tools` | array | | Same tool detection/translation policy as chat |

**Non-stream**: Returns a standard `response` object with an ID like `resp_xxx`, and stores it in in-memory TTL cache.

**Stream (SSE)**: minimal event sequence:

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

Business auth required. Fetches cached responses created by `POST /v1/responses` (caller-scoped; only the same key/token can read).

> Backed by in-memory TTL store. Default TTL is `900s` (configurable via `responses.store_ttl_seconds`).

### `POST /v1/embeddings`

Business auth required. Returns OpenAI-compatible embeddings shape.

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `model` | string | | Supports native models + alias mapping |
| `input` | string/array | | Supports string, string array, token array |

> Requires `embeddings.provider`. Current supported values: `mock` / `deterministic` / `builtin`. If missing/unsupported, returns standard error shape with HTTP 501.

---

## Claude-Compatible API

### `GET /anthropic/v1/models`

No auth required.

**Response**:

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

> Note: the example is partial; the real response includes historical Claude 1.x/2.x/3.x/4.x IDs and common aliases.

### `POST /anthropic/v1/messages`

**Headers**:

```http
x-api-key: your-api-key
Content-Type: application/json
anthropic-version: 2023-06-01
```

> `anthropic-version` is optional; DS2API auto-fills `2023-06-01` when absent.

**Request body**:

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `model` | string | | For example `claude-sonnet-4-5` / `claude-opus-4-6` / `claude-haiku-4-5` (compatible with `claude-3-5-haiku-latest`), plus historical Claude model IDs |
| `messages` | array | | Claude-style messages |
| `max_tokens` | number | | Auto-filled to `8192` when omitted; not strictly enforced by upstream bridge |
| `stream` | boolean | | Default `false` |
| `system` | string | | Optional system prompt |
| `tools` | array | | Claude tool schema |

#### Non-Stream Response

```json
{
  "id": "msg_1738400000000000000",
  "type": "message",
  "role": "assistant",
  "model": "claude-sonnet-4-5",
  "content": [
    {"type": "text", "text": "response"}
  ],
  "stop_reason": "end_turn",
  "stop_sequence": null,
  "usage": {
    "input_tokens": 12,
    "output_tokens": 34
  }
}
```

If tool use is detected, `stop_reason` becomes `tool_use` and `content` contains `tool_use` blocks.

#### Streaming (`stream=true`)

SSE uses paired `event:` + `data:` lines. Event type is also in JSON `type`.

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

**Notes**:

- Models whose names contain `opus` / `reasoner` / `slow` stream `thinking_delta`
- `signature_delta` is not emitted (DeepSeek does not provide verifiable thinking signatures)
- In `tools` mode, the stream avoids leaking raw tool JSON and does not force `input_json_delta`

### `POST /anthropic/v1/messages/count_tokens`

**Request**:

```json
{
  "model": "claude-sonnet-4-5",
  "messages": [
    {"role": "user", "content": "Hello"}
  ]
}
```

**Response**:

```json
{
  "input_tokens": 5
}
```

---

## Admin API

### `POST /admin/login`

Public endpoint.

**Request**:

```json
{
  "admin_key": "admin",
  "expire_hours": 24
}
```

`expire_hours` is optional, default `24`.

**Response**:

```json
{
  "success": true,
  "token": "<jwt>",
  "expires_in": 86400
}
```

### `GET /admin/verify`

Requires JWT: `Authorization: Bearer <jwt>`

**Response**:

```json
{
  "valid": true,
  "expires_at": 1738400000,
  "remaining_seconds": 72000
}
```

### `GET /admin/vercel/config`

Returns Vercel preconfiguration status.

```json
{
  "has_token": true,
  "project_id": "prj_xxx",
  "team_id": null
}
```

### `GET /admin/config`

Returns sanitized config.

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

Updatable fields: `keys`, `accounts`, `claude_mapping`.

**Request**:

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

**Response**: `{"success": true, "total_keys": 3}`

### `DELETE /admin/keys/{key}`

**Response**: `{"success": true, "total_keys": 2}`

### `GET /admin/accounts`

**Query params**:

| Param | Default | Range |
| --- | --- | --- |
| `page` | `1` | ≥ 1 |
| `page_size` | `10` | 1–100 |

**Response**:

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

**Response**: `{"success": true, "total_accounts": 6}`

### `DELETE /admin/accounts/{identifier}`

`identifier` can be email, mobile, or the synthetic id for token-only accounts (`token:<hash>`).

**Response**: `{"success": true, "total_accounts": 5}`

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

| Field | Description |
| --- | --- |
| `available` | Currently available accounts |
| `in_use` | Currently in-use accounts |
| `total` | Total accounts |
| `max_inflight_per_account` | Per-account inflight limit |
| `recommended_concurrency` | Suggested concurrency (`total × max_inflight_per_account`) |

### `POST /admin/accounts/test`

| Field | Required | Notes |
| --- | --- | --- |
| `identifier` | | email / mobile / token-only synthetic id |
| `model` | | default `deepseek-chat` |
| `message` | | if empty, only session creation is tested |

**Response**:

```json
{
  "account": "user@example.com",
  "success": true,
  "response_time": 1240,
  "message": "API test successful (session creation only)",
  "model": "deepseek-chat"
}
```

### `POST /admin/accounts/test-all`

Optional request field: `model`.

```json
{
  "total": 5,
  "success": 4,
  "failed": 1,
  "results": [...]
}
```

### `POST /admin/import`

Batch import keys and accounts.

**Request**:

```json
{
  "keys": ["k1", "k2"],
  "accounts": [
    {"email": "user@example.com", "password": "pwd", "token": ""}
  ]
}
```

**Response**:

```json
{
  "success": true,
  "imported_keys": 2,
  "imported_accounts": 1
}
```

### `POST /admin/test`

Test API availability through the service itself.

| Field | Required | Default |
| --- | --- | --- |
| `model` | | `deepseek-chat` |
| `message` | | `你好` |
| `api_key` | | First key in config |

**Response**:

```json
{
  "success": true,
  "status_code": 200,
  "response": {"id": "..."}
}
```

### `POST /admin/vercel/sync`

| Field | Required | Notes |
| --- | --- | --- |
| `vercel_token` | | If empty or `__USE_PRECONFIG__`, read env |
| `project_id` | | Fallback: `VERCEL_PROJECT_ID` |
| `team_id` | | Fallback: `VERCEL_TEAM_ID` |
| `auto_validate` | | Default `true` |
| `save_credentials` | | Default `true` |

**Success response**:

```json
{
  "success": true,
  "validated_accounts": 3,
  "message": "Config synced, redeploying...",
  "deployment_url": "https://..."
}
```

Or manual deploy required:

```json
{
  "success": true,
  "validated_accounts": 3,
  "message": "Config synced to Vercel, please trigger redeploy manually",
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

## Error Payloads

Compatible routes (`/v1/*`, `/anthropic/*`) use the same error envelope:

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

Admin routes keep `{"detail":"..."}`.

Clients should handle HTTP status code plus `error` / `detail` fields.

**Common status codes**:

| Code | Meaning |
| --- | --- |
| `401` | Authentication failed (invalid key/token, or expired admin JWT) |
| `429` | Too many requests (exceeded inflight + queue capacity) |
| `503` | Model unavailable or upstream error |

---

## cURL Examples

### OpenAI Non-Stream

```bash
curl http://localhost:5001/v1/chat/completions \
  -H "Authorization: Bearer your-api-key" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "deepseek-chat",
    "messages": [{"role": "user", "content": "Hello"}],
    "stream": false
  }'
```

### OpenAI Stream

```bash
curl http://localhost:5001/v1/chat/completions \
  -H "Authorization: Bearer your-api-key" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "deepseek-reasoner",
    "messages": [{"role": "user", "content": "Explain quantum entanglement"}],
    "stream": true
  }'
```

### OpenAI Responses (Stream)

```bash
curl http://localhost:5001/v1/responses \
  -H "Authorization: Bearer your-api-key" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-5-codex",
    "input": "Write a hello world in golang",
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
    "input": ["first text", "second text"]
  }'
```

### OpenAI with Search

```bash
curl http://localhost:5001/v1/chat/completions \
  -H "Authorization: Bearer your-api-key" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "deepseek-chat-search",
    "messages": [{"role": "user", "content": "Latest news today"}],
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
    "messages": [{"role": "user", "content": "What is the weather in Beijing?"}],
    "tools": [
      {
        "type": "function",
        "function": {
          "name": "get_weather",
          "description": "Get weather for a city",
          "parameters": {
            "type": "object",
            "properties": {
              "city": {"type": "string", "description": "City name"}
            },
            "required": ["city"]
          }
        }
      }
    ]
  }'
```

### Claude Non-Stream

```bash
curl http://localhost:5001/anthropic/v1/messages \
  -H "x-api-key: your-api-key" \
  -H "Content-Type: application/json" \
  -H "anthropic-version: 2023-06-01" \
  -d '{
    "model": "claude-sonnet-4-5",
    "max_tokens": 1024,
    "messages": [{"role": "user", "content": "Hello"}]
  }'
```

### Claude Stream

```bash
curl http://localhost:5001/anthropic/v1/messages \
  -H "x-api-key: your-api-key" \
  -H "Content-Type: application/json" \
  -H "anthropic-version: 2023-06-01" \
  -d '{
    "model": "claude-opus-4-6",
    "max_tokens": 1024,
    "messages": [{"role": "user", "content": "Explain relativity"}],
    "stream": true
  }'
```

### Admin Login

```bash
curl http://localhost:5001/admin/login \
  -H "Content-Type: application/json" \
  -d '{"admin_key": "admin"}'
```

### Pin Specific Account

```bash
curl http://localhost:5001/v1/chat/completions \
  -H "Authorization: Bearer your-api-key" \
  -H "X-Ds2-Target-Account: user@example.com" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "deepseek-chat",
    "messages": [{"role": "user", "content": "Hello"}]
  }'
```
