# maclaw-cli Agent Usage

This file is the integration guide for agents that call `maclaw-cli` as a subprocess.

Machine-readable spec:

```bash
maclaw-cli agent-spec
maclaw-cli invoke-schema
```

Static invoke schema file: `maclaw-cli/invoke.schema.json`.

## TL;DR

Always call with an explicit client and session:

```bash
maclaw-cli continue --require-session --client planner --session task-123 "Continue the task"
```

Never use this pattern in automation:

```bash
maclaw-cli session use task-123
maclaw-cli continue "Continue"
```

`session use` mutates a shared global `currentSession`; concurrent agents can overwrite it.

## Mental Model

`maclaw-cli` is a stateful command-line adapter for MaClaw GUI's third-party gateway.

- Each CLI process is short-lived.
- Durable state is stored in `~/.maclaw/maclaw-cli/state.json`.
- State is keyed by `clientId + sessionId`.
- The saved state mainly stores the outgoing poll cursor.
- `sessionId` maps to the protocol `conversationId`.

Use stable IDs:

- `--client`: the calling agent identity, such as `planner`, `executor`, `reviewer`, `codex`.
- `--session`: the task/conversation identity, such as `task-123`, `repo-audit-2026-06-11`.

Same `clientId + sessionId` means one continuous stream. Different `clientId + sessionId` means independent cursor state.

## First-Time Setup

When MaClaw GUI runs on the same machine:

```bash
maclaw-cli bootstrap
maclaw-cli doctor
```

`bootstrap` writes these GUI config fields into `~/.maclaw/config.json` if needed:

- `thirdparty_gateway_enabled=true`
- generated local bearer token
- `thirdparty_gateway_host=127.0.0.1`
- `thirdparty_gateway_port=18777`
- local mode enabled

If MaClaw GUI is already open and the gateway is stopped, restart the gateway in GUI or restart MaClaw GUI once.

## Recommended Automation Flags

Use these for every agent subprocess call:

```bash
--require-session --client <agent-id> --session <task-id>
```

Example:

```bash
maclaw-cli continue --json-errors --require-session --client planner --session task-123 "Find next actionable step"
```

`--require-session` makes the command fail if the agent forgot `--session`, `--conversation`, or `MACLAW_SESSION_ID`.

Use `--lock-timeout <sec>` or `MACLAW_CLI_LOCK_TIMEOUT_SEC` when another process may hold the same state/run lock longer than the default 5 seconds.

## Command Decision Table

| Need | Command |
| --- | --- |
| Verify MaClaw is reachable | `maclaw-cli doctor` |
| First-time local config | `maclaw-cli bootstrap` |
| Send prompt and wait for reply | `maclaw-cli continue --client A --session S "..."` |
| Send prompt only | `maclaw-cli send --client A --session S --text "..."` |
| Fetch pending replies | `maclaw-cli poll --client A --session S` |
| Keep reading replies as JSONL | `maclaw-cli watch --client A --session S --count 10` |
| Register client tools | `maclaw-cli handshake --tools tools.json --client A --session S` |
| Return tool execution result | `maclaw-cli tool-result ... --client A --session S` |
| Inspect sessions | `maclaw-cli session list` |
| Reset a stuck cursor | `maclaw-cli session reset-cursor --id S` |

## Common Flows

### JSON Invoke

Use `invoke` when the supervising agent wants to avoid shell quoting issues.

```bash
echo '{"action":"continue","clientId":"planner","sessionId":"task-123","text":"Continue the task","requireSession":true,"timeoutSec":30}' | maclaw-cli invoke
```

Use `lockTimeoutSec` when the supervisor expects contention:

```bash
echo '{"action":"continue","clientId":"planner","sessionId":"task-123","text":"Continue the task","requireSession":true,"lockTimeoutSec":20}' | maclaw-cli invoke
```

Inline JSON is also supported:

```bash
maclaw-cli invoke --json '{"action":"poll","clientId":"planner","sessionId":"task-123"}'
```

Dry-run validates JSON and prints the equivalent argv without contacting MaClaw:

```bash
echo '{"action":"continue","clientId":"planner","sessionId":"task-123","text":"Continue"}' | maclaw-cli invoke --dry-run
```

Rich send payloads are supported without shell quoting protocol flags:

```bash
echo '{"action":"send","clientId":"planner","sessionId":"task-123","eventId":"evt-001","messageId":"msg-001","message":{"type":"text","text":"Continue with full payload"}}' | maclaw-cli invoke
```

For tool results, use `idempotencyKey` when retrying is possible:

```bash
echo '{"action":"tool-result","clientId":"desktop-agent","sessionId":"task-123","toolCallId":"tc_001","status":"success","idempotencyKey":"tc_001-success","result":{"ok":true}}' | maclaw-cli invoke
```

Supported `action` values:

- `continue`
- `ask`
- `run`
- `send`
- `poll`
- `watch`
- `handshake`
- `ack`
- `tool-result`
- `doctor`
- `bootstrap`

For `invoke` with `action:"watch"`, pass `count` unless an endless subprocess is intentional.

### One-Shot Agent Step

```bash
maclaw-cli continue --require-session --client planner --session task-123 "Continue from the last result and propose the next step"
```

Read stdout JSON. Look at:

- `sessionId`
- `messages`
- `nextCursor`
- `hasMore`

### Separate Send and Poll

```bash
maclaw-cli send --require-session --client executor --session task-123 --text "Run the next step"
maclaw-cli poll --require-session --client executor --session task-123
```

### Stdin Prompt

```bash
echo "Continue from previous result" | maclaw-cli continue --stdin --require-session --client planner --session task-123
```

### Multiple Agents on Same Task

Use different `--client` values:

```bash
maclaw-cli continue --require-session --client planner --session task-123 "Plan"
maclaw-cli continue --require-session --client executor --session task-123 "Execute"
```

These keep separate cursors because the state key is `clientId + sessionId`.

### Shared Cursor for Same Agent

Use same `--client` and same `--session`:

```bash
maclaw-cli continue --require-session --client planner --session task-123 "Step 1"
maclaw-cli continue --require-session --client planner --session task-123 "Step 2"
```

The second call continues from the saved cursor.

## Output Contract

Normal commands print JSON to stdout.

`continue` / `ask` output shape:

```json
{
  "incoming": {
    "ok": true,
    "accepted": true,
    "duplicate": false,
    "maclawMessageId": "mc_in_..."
  },
  "sessionId": "task-123",
  "messages": [
    {
      "id": "mc_out_...",
      "cursor": "12",
      "conversationId": "task-123",
      "type": "text",
      "text": "..."
    }
  ],
  "nextCursor": "12",
  "hasMore": false
}
```

`poll` output shape:

```json
{
  "ok": true,
  "messages": [],
  "nextCursor": "12",
  "hasMore": false
}
```

`watch` prints JSONL: one outgoing message per line.

Errors:

- stdout may contain partial JSON only for `doctor`.
- stderr contains `error: ...`.
- exit code is non-zero.
- Use `--json-errors` or `MACLAW_JSON_ERRORS=1` to make stderr a JSON envelope.

JSON error envelope:

```json
{
  "ok": false,
  "error": {
    "message": "missing explicit session; pass --session <id> for concurrent/agent use"
  }
}
```

## Tool Calling

Tool registration file:

```json
[
  {
    "name": "desktop.open_url",
    "description": "Open a URL in the local desktop browser.",
    "risk": "read",
    "inputSchema": {
      "type": "object",
      "properties": {
        "url": { "type": "string" }
      },
      "required": ["url"]
    },
    "timeoutMs": 5000,
    "requiresApproval": false
  }
]
```

Register:

```bash
maclaw-cli handshake --tools tools.json --require-session --client desktop-agent --session task-123
```

If `poll` or `continue` returns a message with `type: "tool_call"`, execute the local tool, then submit:

```bash
maclaw-cli tool-result \
  --require-session \
  --client desktop-agent \
  --session task-123 \
  --tool-call-id tc_001 \
  --status success \
  --result-json '{"opened":true}'
```

Failure:

```bash
maclaw-cli tool-result \
  --require-session \
  --client desktop-agent \
  --session task-123 \
  --tool-call-id tc_001 \
  --status error \
  --error-code local_failure \
  --error-message "Cannot open URL"
```

Supported statuses:

- `success`
- `error`
- `rejected`
- `cancelled`
- `timeout`

## State and Concurrency

State path:

```text
~/.maclaw/maclaw-cli/state.json
```

Lock path:

```text
~/.maclaw/maclaw-cli/state.json.lock
```

Run lock directory:

```text
~/.maclaw/maclaw-cli/runs/
```

The state lock protects state file reads and writes. The run lock serializes stateful commands for the same `clientId + sessionId`: `continue`, `ask`, `send`, `poll`, `watch`, and `tool-result`.

Default lock wait timeout is 5 seconds.

Rules:

1. Different `clientId + sessionId`: safe to run concurrently.
2. Same `clientId + sessionId`: CLI serializes calls with a run lock.
3. Avoid intentionally starting concurrent `watch` or long `poll` for the same `clientId + sessionId`; second command waits and may fail with `run lock busy`.
4. Always pass `--require-session` in automation.

## Session Commands

These are mostly for humans and diagnostics:

```bash
maclaw-cli session new
maclaw-cli session use task-123
maclaw-cli session current
maclaw-cli session list
maclaw-cli session rename --client planner old-id new-id
maclaw-cli session reset-cursor --client planner --id task-123
maclaw-cli session delete --client planner task-123
```

Automation should not rely on `session use`.

Session mutation commands are client-aware. Pass the same `--client` used by the agent stream you want to inspect or modify.

## Environment Variables

| Variable | Meaning |
| --- | --- |
| `MACLAW_CONFIG` | Path to MaClaw GUI `config.json` |
| `MACLAW_GATEWAY_URL` | Gateway base URL override |
| `MACLAW_GATEWAY_TOKEN` | Bearer token override |
| `MACLAW_CLI_STATE` | State file path override |
| `MACLAW_CLI_LOCK_TIMEOUT_SEC` | Default state/run lock wait timeout seconds |
| `MACLAW_CLIENT_ID` | Default client id |
| `MACLAW_SESSION_ID` | Default explicit session id |
| `MACLAW_REQUIRE_SESSION` | `1` or `true` to require explicit sessions |
| `MACLAW_JSON_ERRORS` | `1` or `true` to emit JSON errors on stderr |
| `MACLAW_CONVERSATION_ID` | Protocol conversation override |
| `MACLAW_USER_ID` | External user id |
| `MACLAW_USER_NAME` | External user display name |

## Troubleshooting

### Missing Token

Run:

```bash
maclaw-cli bootstrap
maclaw-cli doctor
```

If `doctor` still fails, restart MaClaw GUI or restart the third-party gateway in GUI.

### Repeated Old Messages

The cursor may be reset or multiple pollers may be running.

```bash
maclaw-cli session reset-cursor --id task-123
```

Then retry with explicit client/session:

```bash
maclaw-cli poll --require-session --client planner --session task-123
```

### State Lock Busy

Another `maclaw-cli` process is writing state. Retry after a few seconds. If no process is alive and the lock remains, remove:

```bash
rm ~/.maclaw/maclaw-cli/state.json.lock
```

On Windows PowerShell:

```powershell
Remove-Item "$env:USERPROFILE\.maclaw\maclaw-cli\state.json.lock"
```

### `run lock busy`

Another `maclaw-cli` process is already using the same `clientId + sessionId`. Wait for that command to finish, or use a different `--client` / `--session` if this is a separate stream.
