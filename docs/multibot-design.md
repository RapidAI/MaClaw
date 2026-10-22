# MaClaw multi-bot: main bot as teammate + coordinator

Request (Daniel, 2026-10-23 06:01–06:05 UTC+8): like Grok Bot, the main bot in MaClaw GUI knows which other bots exist, what each is doing and its state, can control them (stop, pause/resume, hand work) and message them; it is also a bot itself with its own tasks, stays responsive while its own task and sub-bots run, uses local files/shell/network when it runs in the GUI, and orchestrates local⇄remote work with file handoff.

## 1. What exists today (code map, main @ 69f41da3a)

| Concern | Where | Notes |
|---|---|---|
| Bot definition / CRUD | Hub `hub/internal/botmgmt` (`service.go`, `Bot{ID,Name,Description,InstanceID,CreatedAt,Primary}`), routes in `hub/internal/httpapi/router.go` `/api/v1/bots*` | One Hub account owns N desktop bots; exactly one may be `primary` (`desktop_primary.go`). |
| GUI client for bots | `guiapp/desktop_bot_hub.go` (`ListDesktopBots`, `CreateDesktopBot`, `RenameDesktopBot`, `DeleteDesktopBot`, relay/admit, supplements, watch/takeover) | Every call goes through `desktopBotCall` with the person's Hub login. |
| Run state | Hub `desktop_active_runs.go` / `desktop_task_status.go`; GUI `ListDesktopBotActiveRuns` (`run_id,status,task,step,next_step,phase,waiting_for_user,stale`), `ListDesktopBotTasks`, `QueryDesktopBotTaskStatus`, in-process `activeDesktopRuns` map | MaClawSrv executes the run on a desktop; Hub follows it. |
| Stop | `CancelDesktopBotRun` → `POST /bots/{id}/runs/{run}/cancel` | No pause on Hub. |
| Cross-bot messaging | `guiapp/desktop_bot_cross.go` + Hub `/bots/{id}/cross`, `/cross-notes`, `/start`, `/run` | Primary only. `notify` (seen next turn) and `delegate` (claimed once, becomes a run). The persona "understand" reading can already emit a `cross` object. |
| Results to GUI | `PullDesktopBotDeliveries` / `emitDesktopBotDelivery` (reply text, images, **files as base64**) | |
| Files to a remote desktop | `UploadDesktopBotMaterialChunk` → `POST /bots/{id}/desktop/files` (4 MB chunks, returns desktop path) | |
| Persona replies | `desktop_bot_ack.go`, `desktop_bot_understand.go` (roster prompt `desktopBotRosterPrompt`) | Structured state in, persona sentence out. |
| Local agent (main bot in GUI) | `IMMessageHandler` agent loop (`im_agent_loop_dispatcher.go`), tools in `tool_registry_builtin.go` + `corelib/agent/tool_register_core.go` | Already has `read_file`, `write_file`, `list_directory`, glob/ripgrep search, `bash`, `web_fetch`, `web_search`, `office_read`, behind `CoreToolDeps.SecurityGuard` (Hub-managed client security policy, `corelib/clientsecurity`) and the ACP permission flow. |
| Logs | `corelib/botlog` → `~/.maclaw/logs/bots/<bot-id>.log` | |

Gap: nothing gave the local main bot a *team view* — a registry with location/capabilities/state, tools to act on it, events back, or a file handoff path between its local files and remote desktops.

## 2. Design

### 2.1 Registry (`corelib/botregistry`)
* `Bot{ID, Name, Primary, Self, Location(local|remote), Capabilities[], State, Task, Step, NextStep, RunID, StopReason, Queue, Paused, Held[], LastError, UpdatedAt}`.
* States: `idle, running, waiting_user, queued, paused, error, offline`. `ApplyRuns` folds Hub active runs: waiting_user > running > stale/failed (error) > queued.
* Capabilities: local = `file_read, file_write, shell, http_fetch` (+ `coordinator` for the main bot); remote = `browser, desktop, publish, http_fetch`. The main bot routes by these.
* **The main bot is an entry** (`local:main`, Self, Primary, Location local). Its state follows its own agent turns (`noteMainBotTurn` wraps `runAgentLoop`; overlapping turns counted), so it reports its own task like any other bot.
* Transition events → inbox (bounded 200) + subscribers: `started, finished, needs_user, failed, stopped, paused, resumed, message, artifact, appeared, disappeared`.
* Pause/resume is registry-side (Hub has no pause): a paused bot finishes its current run; new assignments are **held** and dispatched on resume.

### 2.2 Sources of truth
* Remote bots: Hub (`/api/v1/bots`, `/runs/active`) — the registry is a cache refreshed on demand (≤5 s reuse) and by a background watcher (10 s while anything is running/queued, 60 s otherwise). No Hub/MaClawSrv change needed.
* Local main bot: in-process.
* Control goes through the existing Hub routes: notify/delegate = cross notes sent **from the account's Hub primary bot** (Hub only accepts cross from the primary); stop = run cancel. If no Hub primary is set the tool says so (set one in Bot 管理).

### 2.3 Tools for the main bot's LLM (`guiapp/multibot_tools.go`, registered in `registerBuiltinTools`)
| Tool | Does |
|---|---|
| `list_bots` | Everyone (self first) with location, capabilities, state, task, step, unread-event count. Starts the watcher. |
| `get_bot_status(bot)` | Fresh status for one bot / "all". |
| `send_to_bot(bot,text,confirm?)` | Cross `notify`; seen at the bot's next turn, never interrupts. |
| `assign_task(bot,task,note?,artifact_ids?,local_paths?,confirm?)` | Uploads files to the remote desktop first, then cross `delegate` with the desktop paths in the order. Returns immediately (non-blocking). Paused bot → held. Self → "do it with your own tools". |
| `stop_bot(bot,run_id?,confirm?)` | Cancels active run(s). |
| `pause_bot(bot)` / `resume_bot(bot)` | Hold / release new work. |
| `bot_events(peek?)` | Unread inbox, marked read unless peek. |
| `share_file_with_bot(path,bot?,note?,confirm?)` | Registers a local file as an artifact and optionally copies it to a bot's desktop. |
| `list_bot_artifacts` | Files passed between bots, with ids/local paths. |

"Change" a task = `stop_bot` + `assign_task` (or `send_to_bot` for an in-flight hint; Hub supplements stay with the existing GUI flow).

### 2.4 Notifications back to the main bot
* Watcher refresh → registry transitions → events (inbox + Wails event `multibot-event` for the UI).
* Remote deliveries with files → saved as artifacts + `artifact` event.
* The LLM checks `bot_events` after handing work off / before reporting; the tool descriptions tell it to keep talking to the person meanwhile. (Follow-up: auto-inject unread events into the next turn's system prompt / proactive message — see open questions.)

### 2.5 Permissions & safety
* `botregistry.Check`: reads are free; a bot may act on itself; **only the primary (the local main bot) controls other bots**.
* Confirmation (`NEEDS_CONFIRMATION`, tool must be re-called with `confirm=true` after the person says yes): assign/notify text that looks irreversible (install/delete/pay/transfer money/清空/代发…), stopping a bot that is waiting on the person, transfer notes that look irreversible.
* Outbound files: refuse `.ssh/.gnupg/.aws/.azure/.kube/.docker` dirs, `.env`, private keys, `credentials`, `token`, `.netrc`, … and > 50 MB. Artifacts dir `~/.maclaw/data/bot_artifacts` (0700/0600), names sanitized.
* Local tools keep their existing gates: Hub-managed client security policy (`SecurityGuard`, network level) and the ACP permission prompts for write/shell. No new local capability was added; the registry only advertises them.
* Hub still enforces account ownership and the primary-only cross rule.

### 2.6 Persona replies
`desktopBotRosterPrompt` now appends each bot's live `location/state/task/step` from the registry cache, so the persona bot can say "发布员正在上传图片，等它好了我告诉你" from real state rather than guessing.

### 2.7 Responsiveness
Remote work is asynchronous on Hub; `assign_task` returns at once; the watcher runs in its own goroutine; the main bot's own turns are just registry state. Nothing in the main loop waits on a teammate.

## 3. End-to-end example: local file → remote publish
Person: 「把 D:\稿子\周报.md 整理一下，用发布员发到公司博客，发好告诉我」
1. Main bot (`running`, task=整理周报): `read_file D:\稿子\周报.md` (local), edits, `write_file D:\稿子\周报-定稿.md` (permission prompt as today).
2. `list_bots` → 发布员 `remote, [browser,desktop,publish], idle`.
3. `assign_task(bot="发布员", task="登录公司博客后台，新建文章，正文用桌面上的 周报-定稿.md，发布后截图并返回文章链接", local_paths=["D:\稿子\周报-定稿.md"])` → file chunk-uploaded to the desktop (`/…/Materials/周报-定稿.md`), cross `delegate` from Hub primary → reply "handed to 发布员…". Main bot answers the person immediately: 「交给发布员了，我先把下一份也整理好」. Its own turn ends → `idle`.
4. Watcher: 发布员 `queued → running (step=打开后台)` → `started` event; later `waiting_user (需要扫码登录)` → `needs_user` event → main bot (next turn / bot_events) tells the person to take over; person scans; run continues.
5. Run ends; delivery has `screenshot.png` → artifact `art_…` + `artifact` event; state → `idle` → `finished` event. Main bot: 「发好了，链接 …，截图在 ~/.maclaw/data/bot_artifacts/art_…/screenshot.png」 and could pass that artifact on to another bot with `assign_task(..., artifact_ids=[...])`.

## 4. GUI: Bot 管理 create/edit dialog
「新建」 opens a dialog (name prefilled `Bot N`, description, color palette of 10, 6 shapes — blob, cloud, leaf, rounded square, pill, circle — drawn as inline SVG with two white eyes; defaults to the next unused color). Empty and duplicate (case/space-insensitive) names are blocked. The pencil edit dialog changes name/description/color/shape. Look is persisted per account in localStorage (`maclaw.desktopBotAppearance.v1`) and rendered in the list, chat header, side panel and cross-notice chips. Hub stores name/description only.

## 5. Open questions
1. Persist color/shape on Hub (add `appearance` to Bot) so it follows the account across machines? Needs a Hub deploy.
2. Should unread team events be pushed proactively into the main bot's chat (a synthetic turn) rather than read via `bot_events`?
3. Local sub-bots (several bots inside one GUI with separate loops) — the registry supports `Location=local`, but only the main bot exists locally today.
4. True pause of an in-flight remote run would need a MaClawSrv/Hub "suspend" route.
5. Should Hub's primary bot and the local main bot be unified in the UI (today Hub primary is the relay identity for cross notes)?
