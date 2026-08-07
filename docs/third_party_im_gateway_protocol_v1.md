# MaClaw Third-Party IM Gateway Protocol v1

This protocol lets third-party software integrate with MaClaw as an IM-like
channel without exposing callback or file-serving URLs. The third-party client
always initiates connections to MaClaw over HTTP, sends user messages through
JSON, and retrieves Agent replies with cursor-based long polling.

The canonical protocol structs, validators, default limits, feature map, strict
JSON decoder, and handshake response builder live in `corelib/im`. Host-agnostic
server helpers shared by the gateway implementations (cursor queue pruning,
forwarded-header/base-URL resolution, media tokens, request IDs, bearer
extraction) live in the same package (`thirdparty_helpers.go`). MaClawSrv,
MaClaw GUI, and ThirdAPIDemo share that implementation instead of duplicating
protocol rules.

For media, MaClaw supports two paths:

- Small files are sent directly as base64 `data`.
- Large files use server-owned upload/download URLs returned by MaClaw. The
  client uploads to and downloads from those URLs; it must not provide its own
  download URL for the server to fetch.

## Transport

Default local endpoint:

```text
http://127.0.0.1:18777/api/im-gateway/v1
```

Most API calls require the third-party gateway token:

```http
Authorization: Bearer <integration_token>
```

`/health` is public. Media upload/download URLs are protected by the
server-generated `mediaToken` in the URL query or by `X-Media-Token`.

硬件设备可以先用一次性六码完成引导配对，再使用返回的持久 Gateway Token。配对码默认有效 30 分钟、仅可成功消费一次，不是 Bearer 凭据。正常启动、重连、长轮询和媒体请求均只使用持久 Token，不应重复提交配对码。

All request and response bodies use UTF-8 JSON. Responses usually include
`requestId`; include it when reporting integration issues. MaClawSrv and
MaClaw GUI both use the same gateway response shape and `gw_` request ID
prefix.

## Channel Model

The third-party client identifies itself with `clientId`. MaClaw creates a
logical platform name:

```text
thirdparty:<clientId>
```

Conversation memory is keyed by:

```text
thirdparty:<clientId>:<conversationId>
```

This avoids mixing multiple tickets, chats, or business objects from the same
external system.

## Modes

MaClaw owns the routing decision through configuration:

The `/handshake` `mode` field is protocol-level and currently returns `maclaw`
for both MaClawSrv and MaClaw GUI. The values below are internal routing modes,
not separate client protocol modes.

| Mode | Behavior |
| --- | --- |
| `local` | Incoming messages run directly through the local `IMMessageHandler`; replies are written to the outgoing queue. |
| `hub` | Incoming messages are forwarded to Hub via `im.gateway_message`; Hub replies are written to the outgoing queue. |
| `auto` | Prefer Hub when configured and connected; otherwise fall back to local. |

## Endpoints

### Hardware bootstrap pairing

带键盘或配置门户的设备直接提交六码：

```http
POST /api/device-gateway/v1/pair
Content-Type: application/json

{"clientId":"esp32s3-pet-001","pairCode":"645432"}
```

无键盘设备可以提交一段包含六码的短 WAV：

```http
POST /api/device-gateway/v1/pair/voice
Content-Type: audio/wav
X-MaClaw-Client-ID: esp32s3-pet-001
```

两种方式成功时都返回 `201`：

```json
{
  "ok": true,
  "clientId": "esp32s3-pet-001",
  "gatewayToken": "<persistent-device-token>"
}
```

- `pair/voice` 接受至多 512 KiB 的短 WAV；参考实现将推理时长限制在 10 秒。
- ASR 可识别阿拉伯数字，以及逐位说出的中文或英文数字；不得把“十、百”等数值单位解释为配对码。
- Hub 复用部署的会议 ASR worker；本地 GUI 网关复用桌面 SenseVoice。没有可用 ASR 时返回 `503`，设备应提示服务暂不可用，而不是判定短码已经失效。
- Hub 对相同来源地址与 `clientId` 的语音配对限制为每分钟 6 次；超限返回 `429` 和 `Retry-After`。
- `400/401/403/404/409/410/422` 表示当前输入不能靠原样重试解决；`408/429/5xx` 或传输错误应按指数退避重试。
- 公网部署必须使用 HTTPS。反向代理应保留可靠的客户端地址，并可在边缘增加更严格的限流。

### Health

```http
GET /api/im-gateway/v1/health
```

Response:

```json
{
  "ok": true,
  "requestId": "gw_1781028000000000000",
  "status": "connected",
  "protocolVersion": "1",
  "serverTime": 1781028000000
}
```

### Handshake

协议 `1.1` 增加“具体客户端能力”协商。服务端仍接受 `protocolVersion: "1"`；旧客户端未报告输出能力时按保守的纯文本客户端处理。能力分为 `input` 和 `output`，因为“可以上传语音”不代表“可以播放语音”。未声明的输出形式一律视为不支持。

```http
POST /api/im-gateway/v1/handshake
Authorization: Bearer <token>
Content-Type: application/json
```

Request:

```json
{
  "clientId": "crm-desktop-001",
  "clientName": "Example CRM",
  "protocolVersion": "1.1",
  "capabilities": {
    "input": {
      "modalities": ["text", "audio"],
      "audio": { "mimeTypes": ["audio/wav"], "sampleRates": [16000], "channels": 1 }
    },
    "output": {
      "modalities": ["text", "audio"],
      "preferred": ["text", "audio"],
      "combinations": [["text"], ["audio"], ["audio", "text"]],
      "text": { "maxChars": 240, "markdown": false, "locale": "zh-CN" },
      "audio": {
        "mimeTypes": ["audio/wav"], "sampleRates": [16000], "channels": 1,
        "playback": true, "deliveryModes": ["inline", "url"],
        "maxInlineBytes": 8192, "maxDownloadBytes": 262144
      }
    },
    "features": {
      "petStates": true, "petAnimation": false, "petAsset": false,
      "ambientDisplay": true, "meetingRecorder": true, "volumeControl": true
    }
  }
}
```

也可以把同一结构放在顶层 `clientCapabilities`，用于仍需保留旧版扁平 `capabilities` 传输特性的客户端。服务端规范化后通过 `capabilitiesAccepted` 回显实际接受的能力。

- `modalities` 表示能够单独输入或输出的形式，当前标准值为 `text`、`audio`、`image`、`file`；后续协议版本可扩展其他形式。
- `preferred` 表示客户端希望 Agent 优先返回的顺序。
- `combinations` 表示可在同一回复中同时消费的组合。声明 `text` 和 `image` 但没有 `["text","image"]` 时，服务端只能择一发送。
- `maxChars` 按 Unicode 字符计数，不按 UTF-8 字节计数。
- 音频、图像与文件应声明 MIME、采样率、通道、尺寸或字节限制。
- 音频的 `deliveryModes` 可为 `inline` 或 `url`。旧客户端默认只视为支持 `inline`；只有显式声明 `url` 且 `maxDownloadBytes>0` 才能接收下载地址。URL 模式必须受 `maxDownloadBytes` 限制。
- `petAsset:true` 表示客户端能下载并渲染 `petAsset`/`pet_asset` 中的 GUI 宠物帧引用；`petAnimation:true` 进一步表示能按 `frameMs` 播放多帧。仅仅播放本地程序化眨眼/摇尾不能声明这两个能力。
- 对媒体消息，最终出口必须同时检查形式、MIME 和文件大小；例如只声明 `image/png` 的屏幕不能收到 JPEG，只声明 `audio/wav` 且 `playback:true` 的扬声器不能收到 MP3。若 `mimeTypes` 为空则不额外限制编码；非空时支持精确 MIME 或 `image/*` 这类通配符。
- 当一个逻辑回复实际包含多种形式时，服务端必须先剔除 MIME、`playback`、尺寸和 `maxBytes` 不合法的媒体，再从剩余形式中选择 `combinations` 允许的最丰富组合；组合大小相同时按 `preferred` 的先后顺序决定。未被选中的回复部分必须丢弃，不能拆成客户端未声明可同时消费的多条消息。
- 纯文本 ESP32 客户端只声明 `output.modalities:["text"]`，因此不得收到图片、音频或文件；它上传 `audio/wav` 只属于输入能力，并不自动获得音频播放能力。- Hub 会把能力传给 MaClaw GUI Agent，并在最终发送前再次强制降级；不能只依赖模型遵守提示。
- 同一规则也适用于直连 MaClaw GUI 和 MaClawSrv：握手端必须按认证主体 + `clientId` 保存规范化能力，后续每次 Agent 调用都带入该能力，并在出站队列入口再次过滤。`capabilitiesAccepted` 不能只是无状态回显。
- `GET /outgoing` 是单消费者、有序 cursor 流。一个 `clientId` 同一时刻只能有一个读取循环；交互任务应等待该循环发出的本地事件，不能另起一个并发 poll，否则某个读取者可能消费并确认另一个任务正在等待的回复。
- `timeout` 表示服务端长轮询秒数，当前范围 `0..30`；`0` 为立即返回。`limit` 当前范围 `1..20`。有新消息时服务端应立即唤醒请求，避免客户端持续建立 TLS 连接空轮询。

Response:

```json
{
  "ok": true,
  "requestId": "gw_1781028000000000000",
  "channelId": "thirdparty:crm-desktop-001",
  "protocolVersion": "1.1",
  "serverTime": 1781028000000,
  "mode": "maclaw",
  "capabilities": [
    "text",
    "image",
    "file",
    "voice",
    "audio",
    "attachments",
    "server_media_upload",
    "server_media_download",
    "long_poll",
    "ack",
    "idempotency",
    "client_tools",
    "tool_call",
    "tool_plan",
    "tool_result",
    "tool_cancel"
  ],
  "poll": {
    "recommendedTimeoutSec": 30,
    "maxTimeoutSec": 60,
    "defaultLimit": 20,
    "maxLimit": 100
  },
  "limits": {
    "maxTextChars": 20000,
    "maxURLChars": 4096,
    "maxMediaCaption": 2000,
    "maxAttachments": 10,
    "maxIdentifierChars": 128,
    "maxDirectBytes": 262144,
    "maxTools": 64,
    "maxToolSteps": 32,
    "maxToolJSONBytes": 65536,
    "maxBodyBytes": 16777216,
    "maxMediaBytes": 52428800,
    "maxAckIds": 100
  },
  "delivery": {
    "guarantee": "at_least_once_by_cursor",
    "dedupeKey": "message.id",
    "ack": "delivery_receipt"
  },
  "pollTimeoutSec": 30,
  "maxBatchSize": 20,
  "features": {
    "serverMedia": true,
    "server_media_upload": true,
    "server_media_download": true,
    "client_tools": true,
    "tool_call": true,
    "tool_plan": true,
    "tool_result": true,
    "tool_cancel": true
  },
  "capabilitiesAccepted": {
    "input": {"modalities": ["text", "audio"]},
    "output": {
      "modalities": ["text", "audio"],
      "preferred": ["text", "audio"],
      "combinations": [["text"], ["audio"], ["audio", "text"]]
    }
  }
}
```

#### Voice-turn response behavior

When an incoming message is `type:"voice"` or `type:"audio"`, a server may
generate a spoken copy of the Agent answer when the client declares playable
audio output. The response is still governed by `output.combinations`:

- `['audio','text']` allows the useful text reply and a spoken WAV reply to be
  queued as one logical answer (both carry the same `replyTo` and
  `assistant_message_id`).
- `['audio']` allows only the spoken reply; `['text']` allows only text.
- A TTS or media-allocation failure falls back to text whenever text is
  declared. It must not turn a successful Agent answer into a transport error.
- Small WAV payloads use `data` (base64) only when `inline` and
  `maxInlineBytes` permit it. Larger payloads use a same-origin relative `url`
  only when `url` and `maxDownloadBytes` permit it.
- A constrained client must reject absolute or foreign-host media URLs and must
  not forward its durable gateway bearer token to them. Server-generated URL
  audio should use a separate short-lived media token.
- Text-originated turns are not spoken automatically unless the user enables
  the server's automatic TTS reply preference.
- If a client abandons an audio message after bounded playback retries, it must
  acknowledge that message with `status:"failed"` rather than `delivered`.
  Both statuses are terminal delivery receipts and therefore unblock the
  at-least-once queue; the difference preserves accurate playback telemetry.

Example URL-delivered audio message:

```json
{
  "id": "mc_voice_msg_123",
  "replyTo": "mc_in_123",
  "clientId": "esp32-pet",
  "conversationId": "default",
  "type": "audio",
  "mimeType": "audio/wav",
  "fileName": "reply.wav",
  "sizeBytes": 87344,
  "url": "/api/im-gateway/v1/media/abc123?mediaToken=short-lived-token",
  "metadata": {
    "assistant_message_id": "msg_123",
    "tts": "true"
  }
}
```

### Incoming Message

```http
POST /api/im-gateway/v1/incoming
Authorization: Bearer <token>
Content-Type: application/json
```

Text request:

```json
{
  "clientId": "crm-desktop-001",
  "eventId": "evt_20260610_000001",
  "messageId": "crm_msg_10001",
  "conversationId": "ticket_90001",
  "user": {
    "id": "customer_123",
    "name": "Zhang San"
  },
  "message": {
    "type": "text",
    "text": "Help me check this order"
  },
  "createdAt": 1781028000000
}
```

Response:

```json
{
  "ok": true,
  "requestId": "gw_1781028000100000000",
  "accepted": true,
  "duplicate": false,
  "maclawMessageId": "mc_in_1781028000000_000001"
}
```

`eventId` is required for idempotency. If the third-party client retries the
same event, MaClaw returns `duplicate: true` and does not run the agent again.

Supported `message.type` values are `text`, `image`, `file`, and `voice`.
`audio` is accepted and normalized to `voice`.

### Small Direct Media

Direct `data` must be base64 and no larger than `maxDirectBytes`.
The full JSON body must be no larger than `maxBodyBytes`. Server-media uploads
must be no larger than `maxMediaBytes`. MaClawSrv and MaClaw GUI expose the same
limit keys and default values from `corelib/im` so clients can apply one upload
policy.

```json
{
  "clientId": "crm-desktop-001",
  "eventId": "evt_image_000001",
  "messageId": "crm_msg_10002",
  "conversationId": "ticket_90001",
  "user": {
    "id": "customer_123",
    "name": "Zhang San"
  },
  "message": {
    "type": "image",
    "text": "Please inspect this screenshot",
    "fileName": "screenshot.png",
    "mimeType": "image/png",
    "data": "<base64>",
    "sizeBytes": 12345
  }
}
```

For multiple files, use `message.attachments[]`:

```json
{
  "type": "file",
  "text": "Please read these notes",
  "attachments": [
    {
      "type": "file",
      "fileName": "note.txt",
      "mimeType": "text/plain",
      "data": "<base64>",
      "sizeBytes": 1200
    }
  ]
}
```

For direct `data`, `sizeBytes` is optional. When it is greater than zero, it
must equal the decoded base64 byte count; otherwise the gateway rejects the
message with `400`.

Small images are passed as Agent attachments for vision-capable models. Small
text-like files are also inlined into the Agent context.

### Large Server-Owned Media

Large files must use a server-owned media object. First request upload and
download URLs:

```http
POST /api/im-gateway/v1/media/upload-url
Authorization: Bearer <token>
Content-Type: application/json
```

```json
{
  "clientId": "crm-desktop-001",
  "type": "file",
  "fileName": "report.pdf",
  "mimeType": "application/pdf",
  "sizeBytes": 7340032
}
```

Response:

```json
{
  "ok": true,
  "requestId": "gw_1781028000200000000",
  "media": {
    "id": "server-media-id",
    "type": "file",
    "fileName": "report.pdf",
    "mimeType": "application/pdf",
    "url": "http://127.0.0.1:18777/api/im-gateway/v1/media/server-media-id?mediaToken=..."
  },
  "upload": {
    "method": "PUT",
    "url": "http://127.0.0.1:18777/api/im-gateway/v1/media/server-media-id/upload?mediaToken=...",
    "contentType": "application/pdf",
    "maxBytes": 52428800
  },
  "download": {
    "url": "http://127.0.0.1:18777/api/im-gateway/v1/media/server-media-id?mediaToken=..."
  },
  "expiresAt": 1781114400000
}
```

Then upload bytes to `upload.url`:

```http
PUT /api/im-gateway/v1/media/{mediaId}/upload?mediaToken=...
Content-Type: application/pdf

<raw bytes>
```

`upload.maxBytes` is the server-enforced upload limit. If `sizeBytes` was
provided in the upload-url request and is greater than zero, the `PUT` body
must contain exactly that many bytes; otherwise the gateway returns `400`.

Then send an incoming message that references the server media:

```json
{
  "clientId": "crm-desktop-001",
  "eventId": "evt_file_000001",
  "messageId": "crm_msg_10003",
  "conversationId": "ticket_90001",
  "user": {
    "id": "customer_123",
    "name": "Zhang San"
  },
  "message": {
    "type": "file",
    "text": "Please read this report",
    "attachments": [
      {
        "id": "server-media-id",
        "type": "file",
        "fileName": "report.pdf",
        "mimeType": "application/pdf",
        "url": "http://127.0.0.1:18777/api/im-gateway/v1/media/server-media-id?mediaToken=...",
        "sizeBytes": 7340032
      }
    ]
  }
}
```

The same media can also be referenced by `id` only:

```json
{
  "type": "file",
  "attachments": [
    { "id": "server-media-id", "type": "file" }
  ]
}
```

Large media is represented in Agent context by server media ID or URL instead
of being inlined.

### Outgoing Long Poll

```http
GET /api/im-gateway/v1/outgoing?clientId=crm-desktop-001&cursor=0&timeout=30&limit=20
Authorization: Bearer <token>
```

If messages are available, the server returns immediately. Otherwise the request
waits up to `timeout` seconds. `cursor`, `limit`, and `timeout` must be
non-negative integers. `limit` defaults to `poll.defaultLimit` and is clamped to
`poll.maxLimit`; `timeout=0` performs an immediate non-blocking poll.

Response:

```json
{
  "ok": true,
  "requestId": "gw_1781028000300000000",
  "messages": [
    {
      "id": "mc_out_1781028003000_000001",
      "seq": 1,
      "cursor": "1",
      "conversationId": "ticket_90001",
      "replyToMessageId": "crm_msg_10001",
      "type": "text",
      "text": "Please send the order number.",
      "createdAt": 1781028003000
    }
  ],
  "nextCursor": "1",
  "hasMore": false
}
```

When no messages are available:

```json
{
  "ok": true,
  "requestId": "gw_1781028000400000000",
  "messages": [],
  "nextCursor": "1",
  "hasMore": false
}
```

The cursor is the last delivered `seq`. Clients should store `nextCursor` only
after they have successfully handled the response.

Outgoing media replies may include `data` for small direct media or `url` /
`attachments[]` for downloadable media.

Hardware surfaces may also receive feature messages. These messages remain in
the same ordered cursor/ACK stream and are emitted only when the handshake's
accepted feature flags allow them:

| `type` | Required accepted capability | Payload |
| --- | --- | --- |
| `ambient` | `features.ambientDisplay` | `ambient.weather.summary`, `temperatureC`, optional `location`, `expiresAt`, and compact `glyphs` |
| `pet_state` | `features.petStates` | `extra.state` using `idle/listening/thinking/speaking/done/alert/quiet`; optional `durationMs` |
| `meeting_result` | `features.meetingRecorder` and text output | `text` for the short device summary; `extra.status`, `summary`, and document identifiers may link to Mobile/GUI library content |
| `pet_profile` | none for the settings fields; `extra.pet_asset` only with `features.petAsset` | message-level `pet_skin` and `pet_motion_enabled` reflect the current GUI pet settings; optional `extra.pet_asset` contains `encoding`, dimensions, short-lived same-origin `urls`, `revision`, and optional `frameMs`; multiple URLs require `features.petAnimation` |

Feature messages must not be coerced to `text` by an intermediate GUI or Hub.
Unknown or unsupported feature types are filtered before enqueueing. Text in a
`meeting_result` is still truncated to the negotiated `output.text.maxChars`.

### ACK

```http
POST /api/im-gateway/v1/ack
Authorization: Bearer <token>
Content-Type: application/json
```

Request:

```json
{
  "clientId": "crm-desktop-001",
  "messageIds": ["mc_out_1781028003000_000001"],
  "status": "delivered"
}
```

Response:

```json
{ "ok": true, "requestId": "gw_1781028000500000000" }
```

ACK is optional for basic cursor polling, but recommended when the third-party
client can report delivery state. The outgoing cursor controls replay/resume
behavior; ACK is a delivery receipt and does not replace the client's cursor
persistence. MaClaw provides at-least-once delivery by cursor, so clients should
deduplicate by outgoing `message.id`. A single ACK request may include up to
`limits.maxAckIds` message IDs. Unknown IDs are ignored; acknowledged known
messages are not returned again by subsequent `/outgoing` polls.

## Client Tool Execution Extension

This section defines the implemented protocol extension for industrial control,
desktop automation, and other scenarios where the connected third-party client
can execute a small, explicit set of tools on behalf of MaClaw. The design is
similar to LLM tool calling, but the executor is the third-party client
software, not MaClaw.

This extension should be implemented as a capability-gated contract. A client
that does not advertise `client_tools` continues to receive normal text and
media messages only.

The extension is deliberately domain-neutral. Alarm clocks, PLC bridges,
desktop controls, sensors, and future client-owned functions use the same
registration, dispatch, acknowledgement, result, cancellation, and security
lifecycle. Domain-specific fields belong only in each tool's JSON Schema; they
must not be added as special cases to the gateway envelope.

### Runtime and routing semantics

- The validated `tools` catalog is scoped to the authenticated `clientId` and
  is replaced atomically by a later handshake from that client.
- Tool names in one catalog must be unique after protocol normalization; a
  handshake containing duplicate names is rejected instead of relying on
  order-dependent shadowing.
- The catalog travels with the originating user message through local and Hub
  routing. It is not inserted into a process-global Agent tool registry.
- Host tools win any name collision. Client integrations should therefore use
  stable, provider-specific names that satisfy the active LLM provider's
  function-name rules (letters, digits, `_` and `-` are the portable subset).
- `tool_call`, `tool_plan`, and `tool_cancel` are protocol-native outgoing
  messages. A gateway must never coerce them to `text` or filter them by media
  output capabilities.
- Client execution is asynchronous. Dispatching a call completes the current
  Agent tool step; the client's later `tool-result` is the authoritative
  completion event and may start a continuation turn.
- Do not hold an outgoing long poll open while synchronously waiting for
  `tool-result`; constrained clients commonly serialize HTTP requests.
- Client-local persisted state remains the source of truth. The gateway does
  not reconstruct that state from conversation text.
- Delivery `ack` and execution result are separate. For at-least-once delivery,
  clients should persist an idempotency key or pending result before ACKing a
  state-changing call.
- Every executable call and plan step carries a non-empty `idempotencyKey`.
  Results identify exactly one correlation target: either `toolCallId`, or a
  `toolPlanId` plus `stepId`. A `stepId` without `toolPlanId`, or a
  result containing both call and plan IDs, is invalid.
- Cancellation also identifies exactly one target. `toolCallId` and
  `toolPlanId` are mutually exclusive, and `stepId` is valid only with
  `toolPlanId`.
- A successful result must omit `error`. A non-success result must provide an
  `error` object or explanatory `text`, so failures cannot be injected into an
  Agent continuation without an actionable reason.

### Tool Registration

During `handshake`, the client may advertise tool definitions. Tool names are
stable identifiers owned by the client integration. Each tool must include a
JSON Schema for arguments.

```json
{
  "clientId": "plc-client-01",
  "clientName": "Line A PLC Bridge",
  "protocolVersion": "1",
  "capabilities": {
    "text": true,
    "longPolling": true,
    "ack": true,
    "client_tools": true
  },
  "tools": [
    {
      "name": "plc.read_register",
      "description": "Read one Modbus holding register.",
      "risk": "read",
      "inputSchema": {
        "type": "object",
        "properties": {
          "address": { "type": "integer" }
        },
        "required": ["address"]
      },
      "timeoutMs": 5000,
      "requiresApproval": false
    },
    {
      "name": "plc.write_register",
      "description": "Write one Modbus holding register.",
      "risk": "write",
      "inputSchema": {
        "type": "object",
        "properties": {
          "address": { "type": "integer" },
          "value": { "type": "integer" }
        },
        "required": ["address", "value"]
      },
      "timeoutMs": 5000,
      "requiresApproval": true
    }
  ]
}
```

Supported `risk` values:

| Value | Meaning |
| --- | --- |
| `read` | Reads status or diagnostics without changing external state. |
| `write` | Changes normal business or device state. |
| `dangerous` | Can affect safety, production, money, permissions, or irreversible state. |

MaClaw should treat the tool list as a whitelist. The Agent must not be allowed
to invent arbitrary tool names, shell commands, URLs, PLC addresses, or device
operations outside this registered schema.

### Single-Step Tool Call

MaClaw may return a tool call from `GET /outgoing`. The client should execute it
only if the tool name is registered, the arguments match the schema, and local
policy allows the operation.

```json
{
  "id": "mc_out_1781028003000_000002",
  "cursor": "2",
  "conversationId": "line-a",
  "type": "tool_call",
  "toolCall": {
    "id": "tc_001",
    "name": "plc.write_register",
    "arguments": {
      "address": 40002,
      "value": 1
    },
    "risk": "write",
    "requiresApproval": true,
    "idempotencyKey": "line-a:tc_001",
    "timeoutMs": 5000
  },
  "createdAt": 1781028003000
}
```

`ack` still means the client received or delivered the outgoing message. It does
not mean the tool has completed. Tool completion is reported through
`tool-result`.

### Multi-Step Tool Plan

For more complex tasks, MaClaw can send a `tool_plan`. A plan contains multiple
steps and an execution mode. This avoids forcing the Agent to round-trip one
message per tiny operation when the desired sequence is already known.

```json
{
  "id": "mc_out_1781028003000_000003",
  "cursor": "3",
  "conversationId": "line-a",
  "type": "tool_plan",
  "toolPlan": {
    "id": "tp_001",
    "mode": "sequential",
    "requiresApproval": true,
    "steps": [
      {
        "id": "step_1",
        "tool": "plc.read_register",
        "arguments": { "address": 40001 },
        "risk": "read"
      },
      {
        "id": "step_2",
        "tool": "plc.write_register",
        "arguments": {
          "address": 40002,
          "valueFrom": "step_1.value"
        },
        "dependsOn": ["step_1"],
        "risk": "write",
        "requiresApproval": true
      }
    ]
  },
  "createdAt": 1781028003000
}
```

Supported plan modes:

| Mode | Meaning |
| --- | --- |
| `sequential` | Execute steps in order. Recommended default for industrial control. |
| `parallel` | Execute independent steps concurrently. Use only for safe read-like tools. |
| `dag` | Execute when all `dependsOn` steps have succeeded. |
| `interactive` | Stop after each step or approval gate and wait for MaClaw/user direction. |

For industrial control, prefer `sequential` or `dag`. Avoid `parallel` for
state-changing writes unless the client has a domain-specific safety model.

### Tool Result

Tool results should be reported with a dedicated endpoint:

```http
POST /api/im-gateway/v1/tool-result
Authorization: Bearer <token>
Content-Type: application/json
```

Single-step result:

```json
{
  "clientId": "plc-client-01",
  "resultId": "tr_tc_001_success",
  "conversationId": "line-a",
  "toolCallId": "tc_001",
  "status": "success",
  "idempotencyKey": "line-a:tc_001:success",
  "result": {
    "written": true,
    "oldValue": 0,
    "newValue": 1
  },
  "createdAt": 1781028006200
}
```

Plan step result:

```json
{
  "clientId": "plc-client-01",
  "resultId": "tr_tp_001_step_1_success",
  "conversationId": "line-a",
  "toolPlanId": "tp_001",
  "stepId": "step_1",
  "status": "success",
  "idempotencyKey": "line-a:tp_001:step_1:success",
  "result": { "value": 123 },
  "createdAt": 1781028006100
}
```

Failure result:

```json
{
  "clientId": "plc-client-01",
  "resultId": "tr_tc_001_error",
  "conversationId": "line-a",
  "toolCallId": "tc_001",
  "status": "error",
  "idempotencyKey": "line-a:tc_001:error",
  "error": {
    "code": "device_timeout",
    "message": "PLC did not respond within 5s"
  },
  "createdAt": 1781028006200
}
```

Supported `status` values:

| Value | Meaning |
| --- | --- |
| `success` | Execution completed successfully. |
| `error` | Execution failed. Include `error.code` and `error.message`. |
| `rejected` | Client policy or human approval rejected the operation. |
| `cancelled` | Execution was cancelled before completion. `canceled` is accepted and normalized. |
| `timeout` | Execution timed out. |

### Cancellation

MaClaw may cancel a pending or running operation by sending:

```json
{
  "id": "mc_out_1781028003000_000004",
  "cursor": "4",
  "conversationId": "line-a",
  "type": "tool_cancel",
  "toolCancel": {
	"toolPlanId": "tp_001",
	"stepId": "step-2",
	"reason": "user_canceled"
  }
}
```

The client should treat cancellation as best-effort. If a physical write has
already completed, it must return the final observed state instead of pretending
the operation was rolled back.

### Safety Contract

Client tools are more dangerous than chat messages. For industrial control or
physical-world operations, use these rules:

1. Tools are disabled unless `client_tools` is negotiated during `handshake`.
2. Every executable operation must be registered by name and schema.
3. `write` and `dangerous` tools should default to `requiresApproval: true`.
4. The client must enforce its own policy even if MaClaw sends a bad tool call.
5. Every tool call must have `id`, `idempotencyKey`, timeout, and audit record.
6. Repeated `idempotencyKey` must not repeat non-idempotent writes.
7. Use read-only validation tools when a dry-run preview is needed.
8. Plans are capped by `maxToolSteps`; clients should also enforce an overall timeout.
9. Tool result data should be bounded in size. Large artifacts should use the
   existing server-owned media flow.
10. Never expose raw shell, arbitrary HTTP fetch, arbitrary script execution, or
    unrestricted PLC address spaces as a single broad tool.

### Reflection: Open Design Questions

This extension is useful, but several risks should be resolved before it is
enabled by default:

| Issue | Risk | Recommended answer |
| --- | --- | --- |
| Who approves a dangerous action? | The Agent may over-trust its own plan. | Require explicit human or local operator approval for `write`/`dangerous` tools. |
| Is `tool_plan` too powerful? | A large plan can hide many side effects. | Limit `maxSteps`; require per-step approval for dangerous steps. |
| Where does state truth live? | MaClaw may think a command succeeded when the device did not. | Treat client `tool-result` plus observed device state as authoritative. |
| Can the client lie or be stale? | A compromised client can execute unsafe work. | Use per-client trust levels, audit logs, and optional signed tool manifests. |
| Can retries repeat writes? | Network retry may duplicate physical actions. | Require `idempotencyKey`; client must dedupe completed calls. |
| How are long operations monitored? | Long-running plans may appear stuck. | Use bounded tool timeouts and cancellation; report final `success`, `error`, `rejected`, `cancelled`, or `timeout`. |
| How are large outputs returned? | Tool result JSON can become too large. | Put large outputs into server-owned media and return media refs. |

The safest initial implementation is single-step `tool_call` plus
`tool-result`, with `tool_plan` accepted only for read-only or explicitly
approved sequential workflows.

## Field Contract

| Field | Meaning |
| --- | --- |
| `clientId` | Stable unique ID for the third-party software instance. |
| `conversationId` | Conversation, customer, ticket, or group ID from the third-party system. |
| `eventId` | Unique inbound message ID for retry idempotency. |
| `message.id` / `attachments[].id` | Server media ID. It can be used as the only media reference. |
| `message.data` / `attachments[].data` | Base64 content for small files. Must be smaller than `maxDirectBytes`. |
| `message.url` / `attachments[].url` | Server-provided media download URL. Clients only access it. |
| `message.fileName` / `attachments[].fileName` | Media metadata only. It does not replace `id`, `data`, or server `url`. |
| `toolCall.id` | Unique tool call ID generated by MaClaw. |
| `toolCall.idempotencyKey` | Stable key used by the client to prevent duplicate execution. |
| `tool-result.resultId` / `tool-result.idempotencyKey` | Stable result identity. Reusing it on retry prevents duplicate Agent injection. |
| `toolPlan.steps[].dependsOn` | Step dependency list for `dag` or guarded sequential execution. |
| `cursor` | Outgoing message cursor. The client stores the last returned value. |

### Agent-side targeted IM file delivery

Hardware clients such as ESP32 remain input/display endpoints; they do not
execute arbitrary IM sends locally. When a voice request asks MaClaw to send a
generated file to another IM conversation, the Agent uses `send_to_im` (or
`send_file` with `destination=im`) and may provide this structured target:

```json
{
  "path": "report.pdf",
  "channel": "lansenger",
  "group_id": "group-42",
  "group_name": "研发群"
}
```

Supported target fields are `channel`, `group_id`, `group_name`, and `user_id`.
When the name is ambiguous, the Agent first calls
`im_message(action="list_targets", channel=..., query=...)`. An explicitly
targeted file is never broadcast or silently rerouted to another channel. If no
target fields are present, the legacy bound/active-channel behavior is retained.

Hardware may persist the downloaded `petAsset` locally. The cache should bind
the `revision`, dimensions, frame timing and complete RGB565 payload to an
integrity digest, use an atomic replacement strategy, and reject malformed or
truncated data at boot. A matching installed revision may skip another media
download; this is only a client-side cache optimization and does not change the
authenticated media URL or capability negotiation rules.

GUI-to-Hub `im.proactive_file` carries the same optional target object:

```json
{
  "file_data": "BASE64",
  "file_name": "report.pdf",
  "mime_type": "application/pdf",
  "message": "这是您要的报告",
  "target": {
    "channel": "feishu",
    "group_id": "oc_xxx"
  }
}
```

## Error Format

```json
{
  "ok": false,
  "requestId": "gw_1781028000600000000",
  "error": {
    "code": "unauthorized",
    "message": "missing or invalid bearer token"
  }
}
```

MaClawSrv and MaClaw GUI return the same error envelope. Read
`error.code` and `error.message`; do not depend on top-level `code` or
`message` fields.

Common codes:

| Code | HTTP | Meaning |
| --- | --- | --- |
| `unauthorized` | 401 | Missing or invalid token. |
| `bad_request` | 400 | Invalid JSON, missing required field, invalid media reference, or invalid `mediaToken`. |
| `not_found` | 404 | Media object was not found, was not uploaded, expired, or has an invalid `mediaToken`. |
| `method_not_allowed` | 405 | Wrong HTTP method. |
| `payload_too_large` | 413 | Attachment exceeds the configured limit. |
| `llm_not_configured` | 503 | Local mode is selected but MaClaw LLM is not configured. |
| `hub_unavailable` | 503 | Hub mode is selected but Hub is not connected. |

## Recommended Client Loop

1. Call `handshake` at startup.
2. For each third-party user message, send small files directly or upload large
   files with `media/upload-url`.
3. Call `incoming` with a stable `eventId`.
4. Keep one long-poll request open against `outgoing`.
5. Process returned messages in order, then persist `nextCursor`.
6. Optionally call `ack` for delivered messages.

Pseudo-code:

```text
cursor = loadCursor(clientId)
while running:
  resp = GET /outgoing?clientId=...&cursor=cursor&timeout=30
  for msg in resp.messages:
    deliverToThirdPartyUI(msg)
  if delivery succeeded:
    cursor = resp.nextCursor
    saveCursor(clientId, cursor)
    POST /ack { messageIds }
```

## Test Client: connnectMaClaw

A small command-line test client is available at `cmd/connnectMaClaw`. It
performs a real handshake, sends one incoming text message, long-polls outgoing
replies, and optionally ACKs returned messages.

Example:

```powershell
$env:MACLAW_GATEWAY_TOKEN = "your-token"
go run ./cmd/connnectMaClaw -text "hello, check third-party gateway"
```

Useful flags:

| Flag | Default | Meaning |
| --- | --- | --- |
| `-base` | `http://127.0.0.1:18777/api/im-gateway/v1` | Gateway base URL. |
| `-token` | `MACLAW_GATEWAY_TOKEN` | Bearer token. |
| `-client` | `connnectMaClaw` | Third-party client id. |
| `-conversation` | `demo` | Conversation id used for MaClaw memory. |
| `-cursor` | `0` | Outgoing cursor to resume from. |
| `-timeout` | `30` | Long-poll timeout seconds. Use `0` for immediate polling. |
| `-poll-only` | `false` | Skip sending and only poll outgoing messages. |
