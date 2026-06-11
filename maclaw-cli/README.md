# maclaw-cli

`maclaw-cli` is a scriptable CLI for agents that need to control or schedule MaClaw through the third-party IM gateway protocol.

For automated agents, read [AGENT_USAGE.md](AGENT_USAGE.md) or run:

```bash
maclaw-cli version
maclaw-cli agent-help
maclaw-cli agent-spec
maclaw-cli invoke-schema
```

For machine-driven calls that should avoid shell quoting issues:

```bash
echo '{"action":"continue","clientId":"planner","sessionId":"task-123","text":"Continue"}' | maclaw-cli invoke
```

Validate an invoke request without contacting MaClaw:

```bash
echo '{"action":"continue","clientId":"planner","sessionId":"task-123","text":"Continue"}' | maclaw-cli invoke --dry-run
```

`invoke` also accepts rich send payloads and idempotency fields:

```bash
echo '{"action":"send","clientId":"planner","sessionId":"task-123","eventId":"evt-001","messageId":"msg-001","message":{"type":"text","text":"Continue"}}' | maclaw-cli invoke
```

Static schema: [invoke.schema.json](invoke.schema.json)

Machine-readable package metadata: [manifest.json](manifest.json)

It is zero-config when it runs on the same machine as MaClaw GUI:

1. Read `~/.maclaw/config.json`.
2. Discover `thirdparty_gateway_host`, `thirdparty_gateway_port`, and `thirdparty_gateway_token`.
3. Connect to `http://127.0.0.1:18777/api/im-gateway/v1` by default.

If the GUI config uses a custom path, set `MACLAW_CONFIG` or pass `--config`.

## Build

From repo root:

```bash
go build -o maclaw-cli ./maclaw-cli
```

On Windows:

```powershell
go build -o maclaw-cli.exe ./maclaw-cli
```

Install to a user bin directory:

```powershell
.\maclaw-cli\install.ps1
```

```bash
./maclaw-cli/install.sh
```

Put the built binary on `PATH`, or call it with an absolute path from the supervising agent.

## Common Use

```bash
maclaw-cli bootstrap
maclaw-cli doctor
maclaw-cli agent-help
maclaw-cli continue --session task-123 "Continue current task"
maclaw-cli ask --text "Summarize current project status"
maclaw-cli send --text "Run daily report"
maclaw-cli poll --cursor 0
maclaw-cli watch --count 10
```

All normal commands emit JSON. `watch` emits JSONL, one outgoing message per line.

## Sessions

The CLI is stateful. It stores session state in `~/.maclaw/maclaw-cli/state.json`.

```bash
maclaw-cli session new
maclaw-cli session rename --client planner project-42 customer-audit
maclaw-cli session reset-cursor --client planner --id customer-audit
maclaw-cli session delete --client planner customer-audit
maclaw-cli session current
maclaw-cli session list
```

`ask`, `send`, `poll`, `watch`, and `tool-result` use `--session` as the protocol `conversationId` when `--conversation` is not provided. Poll cursors are saved per session, so repeated calls keep advancing the same conversation:

```bash
maclaw-cli ask --session task-123 --text "Start task"
maclaw-cli continue --session task-123 "Continue"
maclaw-cli poll --session task-123
```

`ask`, `continue`, and `send` also accept positional text:

```bash
maclaw-cli continue "keep going"
echo "continue from latest state" | maclaw-cli continue --stdin
```

For human interactive shell use, `session use` sets a global current session:

```bash
maclaw-cli session use project-42
maclaw-cli continue "Continue project 42"
```

Do not use `session use` in multi-agent automation. It mutates shared `currentSession`; another process can change it between calls.

Override per command:

```bash
maclaw-cli ask --session project-42 --text "Continue project 42"
maclaw-cli ask --conversation external-ticket-9 --text "Use exact protocol conversation id"
```

For other agents that launch `maclaw-cli` as one-shot subprocesses, pass `--session` on every call. The CLI will still load and update that session's saved cursor:

```bash
maclaw-cli continue --client planner --session task-123 "step 1"
maclaw-cli continue --client planner --session task-123 "step 2"
maclaw-cli poll --client planner --session task-123
```

Use `--require-session` or `MACLAW_REQUIRE_SESSION=1` to make accidental global-session use fail fast:

```bash
maclaw-cli continue --require-session --client planner --session task-123 "step 1"
```

State is keyed by `clientId + sessionId`. Use a stable `--client` per calling agent type. Different clients with the same session id keep separate cursors; the same client and session share one cursor.

`session rename`, `session reset-cursor`, and `session delete` operate on the selected `--client` plus session id, with fallback for older state entries that do not have a client id.

Use `--json-errors` or `MACLAW_JSON_ERRORS=1` when a supervising agent needs machine-readable stderr on failures.

The CLI also serializes stateful commands for the same `clientId + sessionId` with a run lock under `~/.maclaw/maclaw-cli/runs/`. Different keys can run concurrently. A second command for the same key waits, then exits with `run lock busy` if the first command does not finish before the lock timeout.

Lock timeout defaults to 5 seconds. Override it with `--lock-timeout <sec>`, `MACLAW_CLI_LOCK_TIMEOUT_SEC`, or `lockTimeoutSec` in `invoke` JSON.

## Tool Calls

Register client-side tools during handshake:

```bash
maclaw-cli handshake --tools tools.json
```

Report execution result:

```bash
maclaw-cli tool-result \
  --tool-call-id tc_001 \
  --status success \
  --result-json '{"ok":true}'
```

## Overrides

```bash
maclaw-cli ask \
  --base http://127.0.0.1:18777/api/im-gateway/v1 \
  --token "$MACLAW_GATEWAY_TOKEN" \
  --client other-agent \
  --conversation job-42 \
  --text "Continue"
```

Environment variables:

- `MACLAW_CONFIG`
- `MACLAW_GATEWAY_URL`
- `MACLAW_GATEWAY_TOKEN`
- `MACLAW_CLI_STATE`
- `MACLAW_CLI_LOCK_TIMEOUT_SEC`
- `MACLAW_SESSION_ID`
- `MACLAW_REQUIRE_SESSION`
- `MACLAW_JSON_ERRORS`
- `MACLAW_CLIENT_ID`
- `MACLAW_CONVERSATION_ID`
- `MACLAW_USER_ID`
- `MACLAW_USER_NAME`

## GUI Requirement

MaClaw GUI must have IM third-party access enabled and the local gateway running.

For first-time setup on the same machine:

```bash
maclaw-cli bootstrap
```

This writes `thirdparty_gateway_enabled=true`, a generated local token, `127.0.0.1`, and port `18777` into `~/.maclaw/config.json`. If MaClaw GUI is already running and the gateway is still stopped, restart the gateway from GUI or restart MaClaw GUI once.

Check readiness:

```bash
maclaw-cli doctor
```
