# Desktop observability (for the assistant's own debugging)

No UI. Everything is JSONL on disk plus a read-only CLI, so you can reach it over SSH.

## 1. Per-run event stream (MaClawSrv)

Path: `$MACLAW_DATA_ROOT/desktop-runs/<tenant>/<bot>/<run_id>.jsonl`.
On both hosts that is `/data/soft/maclaw_srv/data/desktop-runs/...` (see `/data/soft/maclaw_srv/.env`).

- The directory is created on the first desktop step after deploy.
- One line per desktop tool call (`event:"step"`), plus note lines (`takeover_request`, `lease`).
- Limits: each file stops at 2 MB. The tree is pruned to 300 MB and 14 days, checked at most every 10 minutes.
- To disable, set `MACLAW_DESKTOP_EVENTS=off` in `.env`.

| field | meaning |
|---|---|
| `ts`, `event`, `run_id`, `tenant`, `bot`, `instance` | identity |
| `user_key` | sha256(tenant+user) truncated to 12 hex characters; not the raw id |
| `step` | trajectory step number (the same as `step-NNN`) |
| `model` | `provider/name` only, never keys |
| `action`, `args` | action and summarized args: text is truncated, and typed text, secrets and cookies are redacted (the same summarizer as the trajectory) |
| `result` | `ok`, `not_applied`, `cross_origin_block`, `element_not_found`, `timeout`, `refused` (repeat or budget; `reason` says which), `guard` (failure-class guard), or `error` (unclassified) |
| `error` | error text, clipped to 200 characters |
| `think_ms` | time from the previous call's end to this call's start (mostly model thinking) |
| `exec_ms` | tool execution time |
| `image_tokens`, `actions`, `max_actions`, `max_image_tokens` | budget counters |
| `site` | current site host |
| `lease`, `reason` | lease or takeover notes |
| `trajectory` | `~/素材/<bot>/runs/<run>/step-NNN.json` inside the user's desktop container (the `.png` is next to it) |

### Query CLI (read-only)

```sh
export TZ=Asia/Shanghai   # hosts run in UTC; the CLI prints local time
R=/data/soft/maclaw_srv/data/desktop-runs
M=/data/soft/maclaw_srv/bin/maclawsrv
$M runs list --root $R                       # recent runs (24h), steps and result counts
$M runs list --root $R --since 168h --bot owner --limit 50
$M runs tail <run_id> --root $R -n 30        # pretty; add --json for raw lines
$M runs failures --root $R --since 72h       # failures grouped by result class and action, with an example
$M runs find --root $R --bot <bot> --since 6h            # first line of each matching run
$M runs find --root $R --result guard --since 24h        # every guard event
$M runs find --root $R --action type --until 2026-10-22T12:00:00+08:00 --since 2h
```

Plain jq also works: `cat $R/*/*/*.jsonl | jq -c 'select(.result!="ok")'`.

## 2. Supervisor and gate events (inside each desktop container)

Path: `/desktops/events.jsonl` in the container. It rotates at 5 MB to `events.jsonl.1`.
Every line has `ts`, `event`, `key` and `bot` (the desktop key minus the owner prefix).

| event | fields |
|---|---|
| `display_start` / `display_stop` | `kind` (owner/bot), `display` |
| `display_idle_timeout` | `display`, idle seconds |
| `h264_open` | `display`, `interactive`, `takeover`, `input`, `mse`, `fps` |
| `h264_close` | `frames`, `seconds`, `reason` (viewer_closed, viewer_gone, read_error, send_error, closed) |
| `takeover_begin` / `takeover_end` | `source` (bot or handback) |
| `lease_change` | old and new lease state |

```sh
C=$(docker ps --format '{{.Names}}' | grep maclaw-desktop | head -1)
docker exec $C tail -n 50 /desktops/events.jsonl
docker exec $C sh -c 'grep h264_close /desktops/events.jsonl | tail'
docker exec $C sh -c 'grep -E "takeover|lease" /desktops/events.jsonl | tail -20'
```

## 3. Other places

- `journalctl -u maclawsrv | grep '\[desktop-step\]'` shows the same steps as one-line logs. `[desktop-guard]` shows failure-class guard decisions.
- Trajectory files: `docker exec $C ls ~/素材/<bot>/runs/<run_id>/` (`step-NNN.json` and `step-NNN.png`).

## Zhihu / navigate hardening (2026-10-22)

- `step_start` is written before every desktop call (step number, action, site). A start
  without its `step` is a call still running or hung; `runs list` shows it as `step_start=1`.
- Hard timeout per desktop call: `MACLAW_DESKTOP_STEP_TIMEOUT` (seconds, default 90; 0 off);
  bash/install/task_run/skill_run/deliver/app_run/computer/file_write use
  `MACLAW_DESKTOP_STEP_TIMEOUT_LONG` (default 900). A timed-out step has result `timeout`.
- navigate waits for `document.readyState=complete` and at least one visible interactive
  element (`MACLAW_BROWSER_NAV_READY_TIMEOUT`, default 18 s), reloads a blank page once and
  reports the final URL. A sign-in URL/form, captcha / 安全验证 text, or a page still blank
  after `MACLAW_BROWSER_BLANK_WALL` (default 20 s) adds `[login_wall]`; the step result is
  `login_wall` and the model is told to call `request_takeover` at once.
- Site experience: seeded playbook for zhihu.com (and subdomains). Every use logs
  `[desktop-experience] hit site=… source=seed|learned|seed+learned` and writes an
  `experience_hit` event; every learned note writes `experience_write`.
- Supervisor `/desktops/events.jsonl` lines carry `srv_bot` / `srv_run` (MaClawSrv bot and
  run id, sent with ctl `bot_state`) so they join the run file.
