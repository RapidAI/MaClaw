# Build & test notes (guiapp)

## Building guiapp on Windows

- Official build is `build_win.bat` (compiles `./cmd/maclaw-gui/` with
  `-tags desktop,production -ldflags "-s -w -H windowsgui -X main.version=<v>"`).
- `go build`/`go test` on package `guiapp` requires `guiapp/resource_windows_amd64.syso`
  (gitignored; build_win.bat generates then deletes it). To produce it for ad-hoc dev:

  ```bat
  powershell -NoProfile -ExecutionPolicy Bypass -File scripts\prepare_windows_resources.ps1 -Root . -Version 0.0.0.0 -AppName MaClaw
  go -C tools\windowsresgen run . -arch amd64 -ico %CD%\build\windows\icon.ico -manifest %CD%\build\windows\wails.exe.manifest.tmp -versioninfo %CD%\build\windows\versioninfo.json.tmp -out %CD%\guiapp\resource_windows_amd64.syso
  ```

  The version must have 4 numeric parts (`0.0.0` is rejected), and resgen
  paths must be absolute — `go -C tools\windowsresgen` changes the working
  directory before the binary runs, so relative `build\windows\...` paths
  resolve against `tools\windowsresgen`.

- A runnable GUI binary needs the embedded frontend assets:

  ```sh
  go build -tags production -o dist/MaClaw_amd64.exe ./cmd/maclaw-gui/
  ```

  Without `-tags production` (or `-tags desktop`), `guiapp/assets_stub.go` supplies a
  stub FS and the app starts with a fake index.html — startup "works" but the UI never
  loads. Do not use a stub build for end-to-end checks.

## File-companion process

- `MaClaw.exe open-file <paths...>` runs the file-companion host. It uses its own
  single-instance lock (`maclaw-lock-file-companion`), `HideWindowOnClose`, and no tray.
- `MaClaw.exe open-file --quit` is a quit relay: it asks the running companion to exit
  through its normal exit controller; if no companion is running it self-terminates
  hidden. Main-process shutdown calls `app.signalFileCompanionQuit` to retire the
  companion (see `guiapp/file_companion_process.go`, `prepareProcessExit` in
  `guiapp/app.go`). Verified end-to-end on Windows via the Wails second-instance
  channel (WM_COPYDATA on the `wails-app-*-sic` message window).
- `maclaw-data-srv.exe` is an independent long-running service, unrelated to the
  desktop lifecycle.
- Web tabs (kind "web", e.g. articles, YouTube) have no file grant and no tools.
  The page body is fetched at open by `fileCompanionPrefetchWebPage` (public-only
  `paperFetch` + readability extraction, cached in `st.webPages`), and
  `SendFileCompanionWebMessage` appends it to the turn under the
  `当前打开的网页正文：` label; `fileCompanionStripNoteExcerptText` cuts that
  label plus the note-excerpt label back out of history and bubbles, so the body
  never accumulates across questions.

## Known test quirks

- `TestKnowledgeSearchToolUsesLocalStore` is timing-flaky: an async
  `toolKnowledgeImportFiles` call can double-import the same file, producing an
  extra duplicate-card group. Re-run before assuming a regression.
- `go test` may also emit pre-existing `sync.Once` copycheck warnings in
  `coding_toolchain_paths_test.go` / `coding_workbench_msvc_test.go`, unrelated
  to semantic-routing work.
- Windows file-lock TempDir flakes (reproduced on a clean baseline): tests
  that leave `conversations.db` (sqlite) or `logs/sdk_diag.log` open fail
  `TempDir RemoveAll cleanup` with "being used by another process", and
  `TestFileCompanionVideoNoteFrames` can miss a just-written session json.
  `newVideoNoteTestApp` now stops `imHandler.memory` in cleanup for the db
  case; the rest are still open.
- `hubClient` must not block on `remoteInfraMu`: during first
  `ensureRemoteInfra` the mutex is write-held while `initCoreInfra` runs,
  and `initCoreInfra → LoadConfig → resetPathBoundStateForDataDirChange →
  sharedMemoryStoreHandlers → hubClient` reaches it on the same goroutine —
  a blocking `RLock` self-deadlocks (RWMutex is not reentrant). hubClient
  uses `TryRLock` + a same-goroutine field-read fallback. Verified via
  `TestScopeSelectionFailsClosedWithoutPlannerOrHostAdmission`.
- Real-model tests are gated by `internal/testenv` (`SkipModelTests` /
  `ModelTestsEnabled`): they self-skip under `go test -short` or when
  `MACLAW_TEST_MODELS=off`/`0`/`false`. `go test ./...` on a dev machine
  with `~/.maclaw/models` installed will otherwise load several GB of
  weights alongside the running app. Opt back in explicitly with
  `MACLAW_TEST_MODELS=on`.
- Local MCP tests must not spawn real servers: `LocalMCPClient.tryStart`
  (guiapp) and `localMCPClient.Start` (corelib/agentservice — a second,
  independent stdio client) refuse any command that is not the
  `GO_WANT_HELPER_PROCESS` re-exec of the test binary
  (`errLocalMCPSpawnSuppressedUnderTest`). Without this an imported
  `{"command":"npx","args":["-y","@playwright/mcp"]}` spends ~3 minutes
  per server in initialize/discovery retries and leaves node children
  behind — this is what timed out a full `guiapp` run. To exercise the
  process path, use `newHelperLocalMCPServerEntry` (guiapp) or a
  nonexistent/fast-exiting command.
- Tests must not spawn companion/pet processes or windows:
  `startCompanionProcess` no-ops under `testing.Testing()` unless a test
  installs `processHooks.startProcess` (otherwise every `app.shutdown`
  cleanup re-execs the ~0.6 GB test binary as `open-file --quit`, and
  dozens of relays exhaust RAM). `newFloatingWindow` (windows/darwin/
  linux) returns `noopFloatingWindow` under `testing.Testing()` so a full
  run cannot paint real desktop-pet windows across the screen; window-state
  assertions still work (created/shown flags are tracked). `restartApp`
  likewise refuses to re-launch the binary under test.
- Recommended dev-loop full test: `MACLAW_TEST_MODELS=off
  scripts\goheavy.cmd go test -short -timeout 20m ./...` — skips real-model
  loads, MCP suppression blocks stray process spawns, and `-timeout` bounds
  a wedge instead of hanging indefinitely. Note a full `guiapp` run is
  still minutes-long: ~1,100 test files and heavy App setup per test are
  inherent to the package.
- A full `go test ./...` run needs `-timeout` > 20m for `guiapp`: the suite
  is ~14,000 tests and still executing when a 20m budget expires (the panic
  shows a fresh test at 0s — a budget cutoff, not a hang). Use
  `-timeout 40m` or split `guiapp` into its own invocation.
- Deterministic guiapp failures seen 2026-11-13 are concentrated in
  currently-modified WIP files (coding_subagent*, remote_coding_subagent,
  file_companion_library/share, goal_continuation, unfinished-slot,
  project-tab naming, experience-skill security scan) — verify against the
  user's in-flight intent before "fixing". Environment-coupled failures:
  `TestBuildCodingSubAgentSystemPrompt_WithContext` (prompt length depends
  on detected local toolchains; this machine finds VS 2026/mingw/python
  → 8876 > 8800 cap) and `TestGUILocalWorkspaceProberUsesReadOnlyGitBaseline`
  (probe inherits repo context). `TestCheckYOLOModelKeepsLiveDownloadState`
  and `TestDirtyCacheCancelDeletesLeaseAndDoesNotOpen` pass in isolation —
  suite-state pollution, not logic bugs.
- `knowledge.SaveText` runs verified-fact supersession post-commit:
  a new text whose factclaim-extracted claims contradict an existing
  rewritable source DELETES that source (`syncVerifiedTextFact` →
  `ApplyVerifiedFact{NoInsert:true}`). Test fixtures with near-identical
  numbered bodies ("...审计 0/1/2。" — `为` inside `行为` parses SPO)
  silently eat each other; use claim-free bodies in linking tests.
- Beware `-run` regex quoting through `scripts\goheavy.cmd`: `-run 'a^'`
  arrives as `-run a` (the `.cmd`/`powershell` layers eat the quote), so a
  "compile-only" selector can silently become "run ~600 tests". Verify the
  selector survived by checking the first `=== RUN` line, or use
  `go vet` for a type-check-only pass.

## Owner turn-window serialization (guiapp)

- `IMMessageHandler.beginOwnerTurnWindow` serializes foreground turns per
  owner via `sessionLoopState.mu`. The wait is bounded by OWNER LIVENESS,
  not wall-clock: while the lock is claimed (`state.windowHeld`, a live
  published `loopCtx`, or an `inFlightTurns` entry) queueing behind it is
  normal — turns routinely hold the window for 15+ minutes — but capped by
  `ownerTurnLiveWaitCeiling` (30min, ~1.7× the max observed hold): liveness
  cannot distinguish a claimed-but-wedged holder (or a same-goroutine
  re-entry, which can never release) from a healthy slow turn, and an
  unbounded wait would make that a silent permanent hang. The 60s
  `ownerTurnLockTimeout` applies to an ORPHANED lock (held with no claimant
  = leaked window); both timeouts are `var`s so tests can shrink them.
  Previously the fixed 60s deadline fired "系统正在恢复中" for any message
  queued behind a healthy long turn (false positive — see 2026-11-13
  incident where a "继续" queued behind a goal-continuation turn errored).
  `msg.CancelCtx` is checked before each spin tick so a cancelled message
  never wins the lock and holds a dead turn's window.
- `windowHeld` (not `pendingText`) is the claimant signal because a turn's
  message text may be empty; it is set right after acquire and cleared just
  before unlock.
- A queued message gets ONE interrupt-scheduler retry once the
  queued-behind loop publishes `loopCtx` (closes the acquire→publish gap
  where `hasActiveLoopForUser` was false at entry). Deterministic commands
  that must wait (`/plan approve` in `handleCodingWorkbenchIMCommand`) pass
  `steerWhileQueued=false`, and all host-authored continuation platforms
  (goal-continuation, unattended-workflow, session-round-continuation) are
  steer-ineligible — `imHostAuthoredTurnPlatform` gates both the entry
  interrupt check and the queued retry, since their "不要…" bodies are
  engine instructions, not user speech the scheduler may read. A claimed
  wait logs "still queued" every 60s so a truly wedged holder is
  diagnosable rather than a silent hang. Stale records are not claimants:
  `ownerTurnClaimed` requires `!IsDone()` on both a published `loopCtx` and
  an `inFlightTurns` ctx, and `hasActiveLoopForUser` likewise rejects a
  done-but-still-published loop so recovery paths never let a dead loop
  "own" the next turn.
- A steer/handled denial sets `ActiveLoopOwnsNextTurn` only when a live
  loop still owns the session; an orphan-lock denial leaves it false so the
  parked goal turn is restored.

## Assistant chat event protocol (guiapp)

- The panel renders assistant output only into an `ActiveRound` keyed by
  `request_id`. UI-initiated sends create the round in
  `useAIAssistant.sendMessage`; **backend-initiated turns must first emit
  `ai-assistant-foreground-round-started`** (`AIAssistantStreamEvent` with
  `request_id`/`session_key`/`display_text`) or every
  `ai-assistant-token/progress/response` is dropped as unmatched.
- Emit via `App.emitAIAssistantStreamEvent(name, event)` — the shared marshal
  + `emitEvent` transport. Round-start callers today: session-round and goal
  continuations, horizon, ACP Mode B (`mirrorACPToGUI`), and remote-control
  sends (`runAIAssistantMessageAsyncForUser` when `req.RemoteControl`).
- `session_key` routes to the task tab (`desktop-user:<projectPath>`); a
  non-matching session becomes an offscreen live round adopted when its tab
  is opened. `display_text` is the user-row text (external origins use a
  `〔…〕` prefix, e.g. `〔远控〕`, `〔VS Code / ACP〕`).

## Remote-control ops (guiapp ↔ hub)

- Controller calls `App.RemoteControlOp(grantId, op, bodyJSON)`; the Hub
  relays it as `remote_control.exec` and the controlled desktop dispatches in
  `App.executeRemoteControl` (`guiapp/remote_control.go`). Adding an op means
  THREE places: the `executeRemoteControl` switch, plus the Hub whitelist in
  `hub/internal/remotecontrol/service.go` — `OpX` const + `allowedOp` +
  `permissionAllows` (`PermView` read ops vs `PermOperate` writes; unknown
  ops are rejected before the desktop sees them).
- `create_task` accepts `{name, mode, work_dir?}`; `work_dir` must normalize
  to an absolute path (missing dirs are allowed, like the local wizard's
  typed path). `list_dirs` (`{path}`, empty = the desktop's own default
  working dir) returns `{path,parent,home,default_dir,roots,dirs,truncated}`
  for the new-task wizard's remote directory browser
  (`RemoteDirPickerPopover` → `draft.machine.workDir`; operate-grants only).

## SSH session recovery (guiapp)

- The three SSH exec surfaces have DIFFERENT recovery contracts by design:
  - Semantic trusted SSH (`executeTrustedBoundSSH`, chat agent): self-heals.
    A dead bound session is re-dialed BEFORE the command is written
    (`trustedSSHRecoverableBoundSession` finds the single session the
    principal opened even after it died; `ReconnectByID` + a
    `trustedSSHReconnectSettle` banner wait), and writes go through
    `WriteInputChecked` (idle probe + write-failure reconnect). A mid-wait
    `outcome_unobserved` calls `recoverTrustedSSHSession`: dead PTY →
    `ReconnectByID`; live shell that ignored Ctrl+C → `Kill` + handle
    `Close` first (a plain reconnect keeps a live PTY) so a fresh shell
    replaces it, then annotates the error with the pre-disconnect output
    tail and "先核对远端状态再重试". The error keeps the
    `outcome_unobserved`/`disconnected` tokens so upstream
    `[system unknown]` classification is unchanged.
  - Legacy/skill sshExec (`h.sshExec`, `sshtool.SSHExec`,
    `SkillExecutor.sshExec`): already self-heals the same way
    (ReconnectByID on terminal status + WriteInputChecked + 3-strike
    CheckShellResponsive).
  - Remote coding (`sshExecRuntimeBoundContext`, `sshExecChannelResult`,
    `newGUIRemoteWorkspaceProber`): deliberately NEVER reconnects — the
    session is bound to a frozen verified target identity, and a lost
    session must interrupt the attempt and go through
    `PrepareCodingRuntimeRecovery`/`ConfirmCodingRuntimeRecovery` review.
    Do not "fix" it to auto-reconnect.
- Liveness verdicts come in two strengths; pick by caller intent:
  - Optimistic: `IsAlive`/`TransportFailed`/`sshSituationSessionAlive` —
    an in-flight probe still counts as bound. For catalog/binding reads.
  - Strict: `TransportLive`/`ClientTransportLive`/`sshClientTransportLive`
    require a positive keepalive answer inside `sshProbeWait` (5s). Used at
    every commit point: `AttachOpenSession` reuse, `trustedSSHSessionWritable`
    (pre-write gate), `WriteInputChecked` idle probe and post-write check,
    the SFTP borrow in `callWithSessionClient`, `Kill`/`Interrupt`/`Close`
    gates, and recovery verification.
- Stall bounds (half-open transports): `crypto/ssh` holds globalSentMu for
  the whole global request, so one unanswered keepalive serializes every
  later request on that client — `NewSession`, `Signal`, channel `Close`
  all queue behind it until the pool keepalive kills the conn (up to
  ~30-60s). The bounds:
  - `reconnectSession` POSITIVELY probes the old client before reuse;
    anything else closes the client first — which unblocks all queued
    writes — and dials via `pool.Reconnect`. `ObservedTransportFailure`
    alone is not enough: it only records FINISHED probe failures.
  - `WaitForOutputContext` bails early to `SessionError` once
    `ObservedTransportFailure` latches, and `finishDeadline` only sends
    `Interrupt` when `TransportLive` answered (the write would otherwise
    park behind the stuck request).
  - `recoverTrustedSSHSession` only sends `Kill` when `TransportLive()`
    answered; otherwise it closes the client before the channel so the
    close itself does not park. A failed recovery `RemoveSession`s the
    binding so the next call plans a fresh connect.
  - `callWithSessionClient` (SFTP) probes the borrowed client; a dead
    session-bound conn goes through `reconnectSession`, a detached dead
    pool entry through `pool.Reconnect`.
- `SSHSessionManager.reconnectSession` covers every death mode:
  transport-dead → `pool.Reconnect`, exited shell on a live connection →
  `AcquireResolved` reuses the pooled client, undetected half-open → the
  `ptySession.Start` failure redials once. It clears `PreviewLines`/
  `droppedLines`, so callers detect the reset by PTY HANDLE IDENTITY
  (`session.PTY() != handleBefore` → resample the baseline), not by
  line-count comparison: a reset buffer can regrow to the same count.
- The `ssh` tool reuses one session per host:user:port. `force_new` is in
  the tool schema for an unresponsive reused session; the "复用已有 SSH
  会话" result also names the close/force_new escape.
- SFTP copies have their own one-shot link-loss retry
  (`callWithSessionClient`); SFTP status errors are never link loss.

## Model memory lifecycle

- Heavy models live behind `corelib/modelmanager`: first `Acquire` loads,
  `done` schedules idle unload, `Unload`/`Shutdown` drop weights now (deferred
  until in-flight leases end). Wired so far: Qwen3 embedding (`LazyEmbedder`
  facade), SenseVoice ASR, CAM++ diarization, FormulaNet, PP-DocLayoutV3,
  tablestruct UNET, TTS, OCR, YOLO. Never `Peek` a model for inference —
  acquire a lease.
- `MACLAW_MODEL_IDLE_UNLOAD_SEC` overrides every manager's idle delay
  (0 disables auto-unload); `MACLAW_MODEL_FREE_OS_MEMORY=off` skips the
  post-unload `debug.FreeOSMemory`; `MACLAW_IDLE_MEM_RELEASE=off` disables the
  guiapp 10-minute-idle heap release; `MACLAW_GO_MEMLIMIT_MB` sets a soft Go
  heap cap (same knob as MaClawSrv).

## Markdown document preview (guiapp/frontend)

- `MarkdownPreview` (`src/components/ai/CodePreviewMarkdown.tsx`) renders
  `.md` documents and office extracts via `react-markdown@8` +
  `remark-gfm` + `remark-math` + `rehype-katex`. Find/go-to-line rides on
  `data-line-start`/`data-line-end` wrappers built from hast positions
  (1-based; `matchLineIndexes`/`activeMatchLine` are 0-based).
- Plugin majors must match `unified@10` (react-markdown@8's ecosystem):
  `remark-math@5` / `rehype-katex@6`. The current majors (`remark-math@6`,
  `rehype-katex@7`) pull `unified@11` and break `Pluggable` typing and the
  mdast/hast node shapes.
- Raw HTML is dropped (`skipHtml`) for safety, so `<script>`/event handlers
  can't run; a small rehype plugin promotes bare `<br>` raws because docs
  put them inside table cells. `\(...\)`/`\[...\]` TeX delimiters are not
  supported (same as the chat renderer); `$`/`$$` are. `==mark==` is kept
  by a tiny remark plugin.

## Heavy Go commands: gate them through `goheavy` (multi-thread safety)

- `guiapp` is ~2,300 Go files (~1,100 test files); a single
  `go test`/`go build` on it peaks at several GB for compile+link, and the
  test binary itself can use several GB. Multiple concurrent agent threads
  each running these have repeatedly pushed this 32 GB machine into an
  unresponsive overload — ALWAYS route them through the throttle:

  ```bat
  scripts\goheavy.cmd go test ./guiapp -run TestFoo
  scripts\goheavy.cmd go test ./...
  scripts\goheavy.cmd go build -tags production -o dist/MaClaw_amd64.exe ./cmd/maclaw-gui/
  ```

  Works from git-bash too (`scripts/goheavy.cmd ...`). The wrapper
  (`scripts/goheavy.ps1`) admits at most `MACLAW_HEAVY_SLOTS` (default 2)
  heavy commands across ALL processes in this user session and queues the
  rest; stale slots are reclaimed by owner-PID liveness. It also caps each
  child's Go runtime via `GOMEMLIMIT` (default `8GiB`) and `GOMAXPROCS`
  (default `8`) — override with `MACLAW_HEAVY_GOMEMLIMIT` /
  `MACLAW_HEAVY_GOMAXPROCS` (`off` disables), and exit codes pass through.
  Gated commands additionally run at `BelowNormal` process priority —
  inherited by the go toolchain, compiler, and test binaries — so a
  saturating full test run can't starve the desktop/IDE. Override with
  `MACLAW_HEAVY_PRIORITY` (`Idle`, `Normal`, or `off`).
- Cheap commands (`go version`, `go env`, grep/rg, `go test` on small
  leaf packages) do not need the gate. If a run seems to hang, check
  `%LOCALAPPDATA%\MaclawHeavyLocks\` for who holds the slots.

## Coding agent knowledge read scope

- `CodingSubAgent`/`RemoteCodingSubAgent` read `knowledge.db` through
  `codingKnowledgeReadOptions` (`guiapp/coding_subagent_knowledge.go`), which
  applies the desktop membership read (`OwnerID=desktop-user` + lineage,
  `UseMembership`, `SearchScope=all`) ONLY for desktop-owned sessions —
  `codingMemoryOwner(taskPermissionOwnerID())` decides: desktop-owned sessions
  get the same whole-library visibility a normal chat has (personal + every
  project + library memberships) with the project path demoted to a
  `scoreSearchResult` ranking boost, while non-desktop sessions (group chats,
  expert/ACP/pet personas, IM users) keep the original `project_path = ?`
  pin so the desktop owner's personal/cross-project sources stay invisible.
  Spawn children inherit both the stores and the owner.
- Tests that build a bare `knowledge.NewSQLiteStore` and expect the coding
  read to hit must call `store.SetDesktopOwnerStamp(desktopUserID)` before
  saving sources — production stores get the stamp from `openKnowledgeStore`,
  and the membership read does not match ownerless rows.
- Coding agents also get read-only `memory_search` over the shared long-term
  memory (`memory.db`) via `SetMemoryStore` — ordinary conversations archive
  into `memory.db` through `ConversationArchiver`, NOT `knowledge.db`, so
  this is where chat-saved common info lands. The handler only ever issues
  the `recall` action; no save/delete path is reachable from the model. The
  owner is resolved per session by `codingMemoryOwner` from
  `taskPermissionOwnerID()` (which children inherit via `permissionOwnerID`):
  desktop-owned sessions (main chat, `desktop-user:<path>` project tabs) share
  the `desktopUserID` namespace so tasks can reuse the owner's saved facts,
  while isolated personas (expert/ACP/pet) and external sessions (group chats,
  IM users) keep their own namespace and `StrictOwner` follows
  `isIsolatedAssistantSessionUserID` — pinning `desktopUserID` unconditionally
  would leak desktop-private memories into those sessions. Task-start prompt build also
  auto-injects a small recalled excerpt (`codingMemoryPromptSection`, dynamic
  mode, 3 entries) next to the knowledge ContextPack — the recall engine is
  soft-ranked, so a weak query can still return the closest entries; that
  mirrors what `memory_search` itself returns. Knowledge and memory tool
  defs are appended in `BuildTools` BEFORE the goal-plan/inquiry/operational
  posture filters — those branches return early, so defs appended after them
  are silently absent from read-only surfaces (the earlier ordering bug had
  local inquiry agents fall back to `rg`/`Get-Content` on `knowledge.db-wal`).

## Read-only coding children: auto review/resume (guiapp)

- `spawn_coding_agent` read-only children (explorer/reviewer) run detached:
  `AdmitReadOnlyChildren` seals the parent Attempt `waiting_child` and releases
  its lease; `runAdmitted*ReadOnlyChild` executes on a ledger-detached context
  (`guiAdmittedChildExecutions`), so normal parent completion can never cancel
  them. `PrepareParentContinuation` is read-only and only succeeds once every
  child result is durable and the parent task is re-queued.
- A delivered handoff auto-resumes the main task — no manual 「审阅子任务结果」
  click: each child-done hook calls `autoResumeCodingChildReviewIfReady`
  (per-user `codingAutoChildReviewLocks` mutex; gates: usable checkpoint,
  pending handoff for that parentTaskID, `hasActiveLoopForUser` false, ALL
  targets ready) then `runAutoCodingChildReview` starts the fresh parent
  Attempt on its own `LoopContext` + AI background session (user can
  interrupt). `finalizeCodingImplementation` chains the same review when the
  run finishes after all children already delivered.
- `cp.AutoReviewHops` (persisted, cap `codingAutoChildReviewMaxHops` = 3)
  bounds spawn→review→spawn chains; past it only the manual action remains.
- Lineage fence: detached reviews write the checkpoint OUTSIDE the owner turn
  window, so `cp.reviewEpoch`/`reviewBaseStamp` (in-memory only, `json:"-"`,
  stripped on load) guard `storeCodingExecCheckpoint` — a write whose stored
  checkpoint is newer than the lineage base AND belongs to a different
  lineage is dropped (foreign turns win); a MISSING stored entry counts as
  "cleared after base" and is also dropped (a live child implies the base was
  in memory). ALL checkpoint stores and `clearCodingExecCheckpoint`'s Delete
  share `codingExecCheckpointEpochMu` — the fence only works because foreign
  writes cannot slip between a lineage's compare and its Store.
  `codingExecCheckpointLineageCurrent` aborts superseded reviews before
  spending a parent Attempt. Epoch 0 keeps every foreground writer
  unconditional.
- `emitCodingChildReviewChat` pushes the review response as an
  `AIContinuationEvent` notice into the coding chat, mirroring
  `emitCodingPlanReviewChat`. For IM-originated coding turns the detached
  child hook also carries `trustedLoopDeliveryTarget(parent.loopCtx)` through
  to `deliverCodingChildReviewToIM`, which converts the trusted destination
  via `trustedDestinationToSchedulerTarget` + `codingChildReviewIMChannel`
  (lansenger/telegram/weixin/qq scopes) and pushes through
  `deliverIMTextForBot` — desktop scopes and unmapped channels skip silently.


## WebView renderer OOM (frontend stream hot path)

- 2026-… incident: WebView2 renderer grew 773MB→3.5GB over ~15min of a coding
  turn, then died to the "Out of Memory" page. Sawtooth drops meant GC-paced
  churn, not one retained leak: every token paid `state.content + delta` plus
  a `startsWith` rescan in `StreamAppendState` (O(n) per piece, O(n²) per
  stream), and each 33ms flush re-rendered the App root.
- Fixes in `useAIAssistant.ts`: `STREAM_TOKEN_FLUSH_MS`=80 (500 hidden),
  `MAX_CODING_TIMELINE_ITEMS`=300, lazy `StreamLane` dedup (pending parts +
  8KB suffix compares; role-marker or snapshot-shaped pieces materialize and
  fold piece-by-piece — joined folding is WRONG, a mid-burst prefix reset
  recovers post-strip text), single-part flush applies once via the ref.
- `perfReporting.ts` reports `js-heap`/`dom_nodes` every 60s; pair with the
  backend `webview_mem_mb` sample (totals ALL msedgewebview2 children) to
  split JS heap vs DOM vs GPU growth in maclaw.log.
- `messages` is NOT React state: `useAIAssistant` writes to a per-hook
  `AssistantMessagesStore` and `AIAssistantPanel` subscribes via
  `useSyncExternalStore` (falls back to `state.messages` props). Token
  flushes re-render the panel only — never add `messages` back to
  `panelState`/`useState` or App re-renders per flush again.

## Goal plan arg encodings (corelib/goal)

- `goal.ParseSteps` tolerates the encodings weak models emit for
  `goal(action="plan")`: an array whose elements are JSON strings
  (`["[{...}]"]` — the whole doc as one element), a JSON document as a bare
  string (optionally ```-fenced, BOM-prefixed, or double-encoded), a single
  step object, a `{"steps": [...]}` wrapper, and a map element whose `title`
  is itself a document. `stepsDecodeCandidates` also completes a truncated
  document — a missing `]`/`}` and a trailing partial element cut to the
  last complete object (seen live: a weak model's over-long one-line array
  lost its closer). A bare string that is not itself a document splits on
  newlines and each LINE gets element-level decoding (a JSON-document line
  expands, a plain line stays a title); elements inside an array never
  split — one element is one step. Anything else falls back to the old
  title behavior; a doc that yields no titled steps still errors so the
  corrective plan turn fires. Depth is capped at `stepsJSONDepth`.
- Rows written before this could store the JSON document as a single step's
  `title` (plan collapsed to one step; marking it done finished the goal
  early — the 目标清单 card showed raw JSON). `Store.loadAll` runs
  `repairEncodedStepTitles`: a decoded title expands back into real steps,
  the first keeps the parent's status/result/detail (the work actually
  reported), the rest become pending, and the fix is persisted via
  `updateGoalText`.
- `g.Steps` has exactly one writer: `Store.SavePlan`, always fed by
  `ParseSteps` (GUI `im_tool_goal.go`, TUI `corelib/agent/tool_goal.go`,
  computeruse, desktopmcp). The plan/execute prompts and the goal tool
  schema both tell the model to pass the array itself, never a JSON string.

## Logs

- `~/.maclaw/logs/maclaw.log` — shared by main app, companion, and quit relays.
  `[file-companion]` = companion-side; `[exit-lifecycle]` = shutdown lifecycle;
  `[boot]` = early startup markers.
- `~/.maclaw/logs/pet-companion.log`, `registration.log` for those subsystems.

## mobilebot (Flutter client)

- `mobilebot/` is the active MaClawBot phone client (login → bots → chat →
  remote desktop → remote control → account). `mobile/maclaw_mobile/` is
  DEPRECATED — its CI workflow was removed with it.
- Flutter pin lives in `mobilebot/.fvmrc` (single source — both workflows
  read it). Java 17. Verify loop: `flutter pub get`, `dart analyze lib test`
  (must stay 0 issues), `flutter test`, `flutter build apk --debug`.
  Release signing needs `android/key.properties` + a keystore in
  `android/app/` (gitignored); without them the release build silently falls
  back to debug signing.
- Defensive parsing: every Hub JSON field goes through `lib/core/json_safe.dart`
  (`syncText`/`syncList`/`syncInt`/`syncTextAny`…). `bot_models.dart`
  re-exports it so existing imports keep working. Never read `json['x']`
  directly in model factories.
- Remote control (`features/remote/`): phone is an ACCOUNT-LEVEL controller —
  no `X-Machine-Id` header, so Hub `controller_machine_id=''` and
  `controllerIdentity` in `hub/internal/remotecontrol/service.go` resolves the
  account itself. Selector reads `state.grants` (I control), `controlled` is
  the reverse bucket (who controls my machines), `invites` incoming /
  `pending` outgoing. Accepting an invite on mobile must bind a desktop via
  `?machine_id=`. Ops ride `POST /grants/{id}/op` `{op, body}` — op names and
  view/operate tiers are whitelisted hub-side; a new op needs the desktop
  `executeRemoteControl` case AND the hub whitelist (see "Remote-control ops"
  above).
- CI: `.github/workflows/mobilebot.yml` (PR/push checks + debug APK artifact);
  `.github/workflows/main.yml` `build-android` job ships signed
  `MaClawBot.apk` into tagged Releases — needs secrets
  `MOBILEBOT_KEYSTORE_BASE64` / `_PASSWORD` / `KEY_ALIAS` / `KEY_PASSWORD`.
- Hub deploy: `.cursor/rules/deploy-hub-binaries.mdc` — cross-compile
  `GOOS=linux go build -o build/deploy/maclaw-hub ./hub/cmd/hub`, then
  backup→overwrite→`start.sh` on `hub.mypapers.top` (root, password auth —
  the password is never stored; ask the user). Compiled deploy binaries under
  `build/deploy/` are gitignored.
- HarmonyOS NEXT: code is ohos-ready via `lib/core/platform.dart`
  (`Platform.operatingSystem == 'ohos'` gates sideload update + voice input —
  `record`/`speech_to_text`/`open_filex` have no ohos ports). Building the
  .hap needs the OpenHarmony Flutter SDK (`flutter create --platforms ohos .`)
  + uncommenting the dependency_overrides block at the top of
  `mobilebot/pubspec.yaml`; see `mobilebot/README.md`「HarmonyOS NEXT」.
  Updates on ohos go through AppGallery, not the R2 self-updater.

## Bot 电脑数限额 (hub botmgmt)

- `Grant.DesktopBotLimit` caps how many desktop bots a user may own. It
  resolves as a CASCADE (`desktopBotLimit` in
  `hub/internal/botmgmt/bot_desktop_quota.go`): user grant → department
  grants nearest→root → global grant → `record.DefaultDesktopBotLimit`
  (tenant default). A blank (nil) limit INHERITS the next level — it is not
  "unlimited"; unlimited only when every level is blank. Zero still means
  "no desktop bots".
- Tenant default: `PATCH /api/admin/bots/settings`
  `{"default_desktop_bot_limit": n|null}` (field required, null clears);
  stored on the record, returned in `SettingsView`. Grant cap stays
  `PATCH /api/admin/bots/grants/{id}` `{"desktop_bot_limit": …}`; both parse
  through `optionalLimitPatch` (absent ≠ null).
- Admin UI: bot tab access card has the global-grant cap row plus the new
  "默认电脑数" row; grant rows in the list AND granted nodes in the org
  tree carry the cap input (`data-grant-limit` + `saveGrantLimit`; the org
  click walker returns early on INPUT/`bot-grant-limit` so typing can't
  collapse the node). Security tab (企业管理) shows a "Bot 电脑数限额" card
  on department/user detail and "Bot 默认电脑数" on the global panel —
  `loadBotView`/`botQuotaCascade`/`saveSecBotQuota` mirror the Go cascade;
  `results[2]` in `selectSecGlobal` stays the policy fetch, `loadBotView`
  is appended last (index shifts there bit once).
- Saving a quota for a target with no grant POSTs a grant — that ALSO
  enables bots for it (the card warns). `desktopQuotaAllows` /
  `MarkDesktopOver` / `DesktopQuotaForUser` all read the same resolved
  limit, so the default shows up in the clients' `desktop_quota` payload
  unchanged.

## Hub admin web (hub/web/admin)

- `node hub/web/admin/validate-admin-modules.js` enforces that runtime JS
  files stay ASCII: write CJK in `copy`/dicts as literal `\uXXXX` escapes
  inside the JS strings (see `desktop-tab.js`). Watch quoting — emitting a
  literal backslash-u through tool calls may need `\u` escaping or a node
  one-liner instead of the edit tool.
- Per-module unit tests are plain node scripts:
  `node hub/web/admin/desktop-tab.test.js`, `node hub/web/admin/bot-tab.test.js`.
  `check-admin.ps1` runs the validator plus
  `go test ./hub/internal/httpapi ./hub/internal/im ./hub/internal/llmservice`.
- Known pre-existing validator failures (not to be "fixed" blindly):
  `bot-tab.js` has a `→` char and `index.html` has `tenantEdge*` i18n
  complaints on HEAD.
- `desktop-tab.js` Bot-node list (`GET /api/admin/bots/nodes`): each node
  carries `owner_user_id` (self-registered personal node; resolve to the
  account via `botOrgChoices().users`) and `issued_by` (admin username from
  `IssueCodeBy`, or the user id for `IssueOwnedCode`; kept across secret
  rotation). `Broker.ApplyLive` also fills `LastSeen` from the live map —
  `last_seen` is in-memory only, not persisted to the tenant file.
