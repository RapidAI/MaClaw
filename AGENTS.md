# Build & test notes (guiapp)

## Building guiapp on Windows

- Official build is `build_win.bat` (compiles `./cmd/maclaw-gui/` with
  `-tags desktop,production -ldflags "-s -w -H windowsgui -X main.version=<v>"`).
- `go build`/`go test` on package `guiapp` requires `guiapp/resource_windows_amd64.syso`
  (gitignored; build_win.bat generates then deletes it). To produce it for ad-hoc dev:

  ```bat
  powershell -NoProfile -ExecutionPolicy Bypass -File scripts\prepare_windows_resources.ps1 -Root . -Version 0.0.0 -AppName MaClaw
  go -C tools\windowsresgen run . -arch amd64 -ico build\windows\icon.ico -manifest build\windows\wails.exe.manifest.tmp -versioninfo build\windows\versioninfo.json.tmp -out guiapp\resource_windows_amd64.syso
  ```

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

## Logs

- `~/.maclaw/logs/maclaw.log` — shared by main app, companion, and quit relays.
  `[file-companion]` = companion-side; `[exit-lifecycle]` = shutdown lifecycle;
  `[boot]` = early startup markers.
- `~/.maclaw/logs/pet-companion.log`, `registration.log` for those subsystems.
