# MaClaw Third-Party IM Gateway Protocol v1

This protocol lets third-party software integrate with MaClaw as an IM-like channel without exposing a callback URL. The third-party client always initiates connections to MaClaw, using HTTP plus cursor-based long polling. The design follows the same client-side gateway shape as the local WeChat integration: pull updates, process locally or through Hub, and push outbound messages through the same client connection model.

## Goals

- Keep integration simple for desktop, intranet, and self-hosted systems.
- Avoid requiring a public callback address, TLS certificate, or inbound firewall rule on the third-party side.
- Normalize third-party messages into MaClaw's existing `IMMessageHandler` pipeline.
- Support both local mode and Hub mode with the same semantics as other IM gateways.
- Provide reliable retry behavior through `eventId`, `cursor`, and optional `ack`.

## Transport

Default local endpoint:

```text
http://127.0.0.1:18777/api/im-gateway/v1
```

The service requires an integration token. Use the Generate Token button in MaClaw, or provide a strong random token manually. A production deployment should bind to `127.0.0.1` unless the user explicitly needs LAN access.

Authentication:

```http
Authorization: Bearer <integration_token>
```

All request and response bodies use UTF-8 JSON. Every response includes a `requestId`; include it when reporting integration issues.

## Channel Model

The third-party client identifies itself with `clientId`. MaClaw creates a logical platform name:

```text
thirdparty:<clientId>
```

For local processing, conversation memory is keyed by:

```text
thirdparty:<clientId>:<conversationId>
```

This avoids mixing multiple tickets, chats, or business objects from the same external user.

## Modes

MaClaw owns the routing decision through configuration:

| Mode | Behavior |
| --- | --- |
| `local` | Incoming messages run directly through the local `IMMessageHandler`; replies are written to the outgoing queue. |
| `hub` | Incoming messages are forwarded to Hub via `im.gateway_message`; Hub replies are written to the outgoing queue. |
| `auto` | Prefer Hub when configured and connected; otherwise fall back to local. |

The initial implementation uses the same effective local-mode rule as other IM gateways: unset means Hub mode when Hub is activated, otherwise local mode.

## Endpoints

### Health

```http
GET /api/im-gateway/v1/health
```

Response:

```json
{
  "ok": true,
  "status": "connected",
  "protocolVersion": "1.0",
  "serverTime": 1777660800000
}
```

### Handshake

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
  "protocolVersion": "1.0",
  "capabilities": {
    "text": true,
    "image": true,
    "file": true,
    "voice": false,
    "longPolling": true
  }
}
```

Response:

```json
{
  "ok": true,
  "requestId": "gw_1777660800000000000",
  "channelId": "thirdparty:crm-desktop-001",
  "protocolVersion": "1.0",
  "serverTime": 1777660800000,
  "mode": "local",
  "capabilities": ["text", "image", "file", "voice", "long_poll", "ack", "idempotency"],
  "poll": {
    "recommendedTimeoutSec": 30,
    "maxTimeoutSec": 60,
    "defaultLimit": 20,
    "maxLimit": 100
  },
  "limits": {
    "maxTextChars": 12000,
    "maxBodyBytes": 16777216,
    "maxMediaBytes": 10485760
  },
  "delivery": {
    "guarantee": "at_least_once_by_cursor",
    "dedupeKey": "message.id",
    "ack": "delivery_receipt"
  },
  "pollTimeoutSec": 30,
  "maxBatchSize": 20,
  "features": {
    "text": true,
    "image": true,
    "file": true,
    "voice": true,
    "longPolling": true,
    "ack": true
  }
}
```

### Incoming Message

```http
POST /api/im-gateway/v1/incoming
Authorization: Bearer <token>
Content-Type: application/json
```

Request:

```json
{
  "clientId": "crm-desktop-001",
  "eventId": "evt_20260502_000001",
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
  "createdAt": 1777660800000
}
```

Response:

```json
{
  "ok": true,
  "requestId": "gw_1777660800100000000",
  "accepted": true,
  "duplicate": false,
  "maclawMessageId": "mc_in_1777660800000_000001"
}
```

`eventId` is required for idempotency. If the third-party client retries the same event, MaClaw returns `duplicate: true` and does not run the agent again.

Supported message payloads:

```json
{ "type": "text", "text": "hello" }
```

```json
{
  "type": "image",
  "fileName": "screenshot.png",
  "contentType": "image/png",
  "data": "<base64>"
}
```

```json
{
  "type": "file",
  "fileName": "report.xlsx",
  "contentType": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
  "data": "<base64>"
}
```

### Outgoing Long Poll

```http
GET /api/im-gateway/v1/outgoing?clientId=crm-desktop-001&cursor=0&timeout=30&limit=20
Authorization: Bearer <token>
```

If messages are available, the server returns immediately. Otherwise the request waits up to `timeout` seconds.

Response:

```json
{
  "ok": true,
  "requestId": "gw_1777660800300000000",
  "messages": [
    {
      "id": "mc_out_1777660803000_000001",
      "seq": 1,
      "conversationId": "ticket_90001",
      "replyToMessageId": "crm_msg_10001",
      "type": "text",
      "text": "Please send the order number.",
      "createdAt": 1777660803000
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
  "requestId": "gw_1777660800400000000",
  "messages": [],
  "nextCursor": "1",
  "hasMore": false
}
```

The cursor is the last delivered `seq`. Clients should store `nextCursor` only after they have successfully handled the response.

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
  "messageIds": ["mc_out_1777660803000_000001"],
  "status": "delivered"
}
```

Response:

```json
{ "ok": true, "requestId": "gw_1777660800500000000" }
```

ACK is optional for basic cursor polling, but recommended when the third-party client can report delivery state. The outgoing cursor controls replay/resume behavior; ACK is a delivery receipt and does not replace the client's cursor persistence. MaClaw provides at-least-once delivery by cursor, so clients should deduplicate by outgoing `message.id`.

## Error Format

```json
{
  "ok": false,
  "code": "unauthorized",
  "message": "missing or invalid bearer token",
  "requestId": "gw_1777660800600000000",
  "error": {
    "code": "unauthorized",
    "message": "missing or invalid bearer token",
    "requestId": "gw_1777660800600000000"
  }
}
```

Common codes:

| Code | HTTP | Meaning |
| --- | --- | --- |
| `unauthorized` | 401 | Missing or invalid token. |
| `bad_request` | 400 | Invalid JSON or missing required field. |
| `unsupported_message_type` | 400 | Message type is not supported. |
| `payload_too_large` | 413 | Attachment exceeds the configured limit. |
| `llm_not_configured` | 503 | Local mode is selected but MaClaw LLM is not configured. |
| `hub_unavailable` | 503 | Hub mode is selected but Hub is not connected. |
| `method_not_allowed` | 405 | Wrong HTTP method. |

## Recommended Client Loop

1. Call `handshake` at startup.
2. For each third-party user message, call `incoming` with a stable `eventId`.
3. Keep one long-poll request open against `outgoing`.
4. Process returned messages in order, then persist `nextCursor`.
5. Optionally call `ack` for delivered messages.

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

A small command-line test client is available at `cmd/connnectMaClaw`. It performs a real handshake, sends one incoming text message, long-polls outgoing replies, and optionally ACKs returned messages.

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

