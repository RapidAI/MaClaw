# Browser Automation Stable Workflow

Browser automation now uses one stable mechanism only: the merged `browser` tool with a `browser-session-*` session id.

Legacy direct tools such as `browser_navigate` and `browser_click` are internal compatibility handlers. Recording/replay and arbitrary JavaScript paths are disabled. They must not be exposed or recommended as the primary workflow.

## Start Session

Use `session_start` or `connect` to create/reuse a stable browser agent session. `connect` is only an alias for `session_start`.

```json
{"action":"session_start","start_url":"https://example.com"}
```

The result includes:

```json
{"session_id":"browser-session-..."}
```

## Operate Page

Every page action must pass the returned `session_id`.

```json
{"action":"observe","session_id":"browser-session-..."}
{"action":"navigate","session_id":"browser-session-...","url":"https://example.com/login"}
{"action":"click","session_id":"browser-session-...","ref":"@e1"}
{"action":"type","session_id":"browser-session-...","ref":"@e2","text":"testuser"}
{"action":"wait","session_id":"browser-session-...","duration_ms":1000}
{"action":"screenshot","session_id":"browser-session-...","full_page":true}
```

Do not use CDP target ids such as `CA8EC545` as `session_id`. Target ids are tab ids, not browser agent sessions.

## Run Multi-Step Task

For repeatable automation, use `task_run` inside the same browser session.

```json
{
  "action":"task_run",
  "session_id":"browser-session-...",
  "steps":"[{\"action\":\"navigate\",\"params\":{\"url\":\"https://example.com/login\"}},{\"action\":\"type\",\"params\":{\"selector\":\"#username\",\"text\":\"testuser\"}},{\"action\":\"click\",\"params\":{\"selector\":\"button[type=submit]\"}}]"
}
```

Query task state with the same session id:

```json
{"action":"task_status","session_id":"browser-session-...","task_id":"task-..."}
```

## Stop Session

```json
{"action":"session_stop","session_id":"browser-session-...","close_browser":true}
```

## Disabled Unstable Paths

Recording/replay actions are not part of the stable session workflow:

- `eval`
- `click_at`
- `get_text`
- `get_html`
- `record_start`
- `record_stop`
- `task_replay`

Use `session_start` plus `task_run` instead.
