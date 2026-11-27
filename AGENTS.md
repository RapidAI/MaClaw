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

## Vendored Wails runtime JS

- After editing `third_party/wails/internal/frontend/runtime/desktop/*.js`,
  regenerate the embedded bundles from that directory with
  `npm run build:runtime-desktop-prod` and `npm run build:runtime-desktop-debug`;
  the Go binaries embed the generated `runtime_prod_desktop.js` /
  `runtime_debug_desktop.js` files, not the source files directly. Run
  `npx vitest run` there for the runtime tests. esbuild is pinned to exactly
  0.15.6 on purpose: later 0.15.x releases rewrite the debug-bundle formatting
  (short ifs join their condition line), which buries real changes under
  cosmetic churn in the vendored diff — do not restore the caret range.

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

- Frontend vitest must run from `guiapp/frontend/` (the `npm test` cwd), not
  `frontend/src/`: several suites read CSS with
  `join(process.cwd(), 'src/...')`, so a `src` cwd fails them with ENOENT on
  the doubled `src\src\...` path (observed as ~24 phantom failures across the
  layout/ai folders). The `filter:` path passed to `npx vitest --run` is
  relative to the vitest root (`frontend/src`), which is why it accepts
  `components/...` while the shell cwd stays at `frontend/`.
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
  `go vet` for a type-check-only pass. (2026-11-20: the shim routes through
  `powershell -Command` with single-quoted tokens — dash tokens like `-o` no
  longer trip AmbiguousParameter, but regex quoting still loses one layer;
  the same verify-the-`=== RUN`-line rule applies.)
- Silent-update flow: background checks download the installer silently but
  never launch it. A downloaded installer is memory-only until the user
  chooses install/defer; durable defer state in
  `<base>/update/deferred-install.json` deliberately stores no URL/hash, and
  execution re-fetches release metadata before launching. Ignored versions
  are persisted in `<base>/update/ignored-versions.json` and filtered from
  both prompts and silent downloads.

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

## Semantic route authority (guiapp)

- Route revisions fence per lineage (root+session+principal): a newer
  `PublishSurface` on the same lineage supersedes every earlier revision,
  and a superseded revision never becomes current again. Per-call
  `coordinator.Admit`/`executor.Execute` check `routeRevisionIsCurrent` —
  `route_revision_superseded`/`route_revision_cancelled` at that point
  means THIS surface is dead forever, most often because a concurrent turn
  on the same lineage took over.
- `sharedAgentLoopCallbacks.semanticRouteAuthorityLossResult` is the
  single funnel for those errors: a dead managed surface calls
  `loopCtx.Cancel()` (the loop exits via `ShouldStop`) and returns a
  terminal "[system] 授权已被取代…本轮执行终止" text — NOT a
  `[system rejected]` tool failure. Do NOT return fencing errors as
  retryable tool results: the 2026-11 ssh deployment turn retried a dead
  route ~10 times, tripped the consecutive-same-tool "改用其他方式"
  guidance, and detoured around governed ssh with a local paramiko script.
- A discovered-MCP sidecar is exempt: it lives on a `…:mcp-sidecar`
  session (own lineage), so its death only drops the sidecar
  (`semanticSurfaceIsMCPSidecar` matches pointer identity AND the session
  suffix — the suffix catches a sidecar dropped concurrently mid-call).

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
- The coding plan checklist bar (`CodingAgentPlanChecklist`, the "步骤" strip
  above the composer) retires — unmounts, or strips to the files chip — when
  `taskExecutionStatus.tone` lands on `completed`/`cancelled`, not just when
  every step passes: a finished turn must not leave a stale pending/running
  cursor or a lone 需求理解 hover target over the composer. Exceptions that
  keep it mounted: `codingPlanGateOpen` (awaiting Start/Skip/Reject), any
  `codingStepIsFailed` row, and non-terminal tones (paused/interrupted keep
  resume context). The bar itself is centered at 45% column width
  (`100-pet-store-mc.css` overrides the shared inset rule) so the pointer can
  pass the edges without opening the step-list popover; `.capc-popover` is
  `width:100%` of that bar.
- The 需求理解 line (`fallbackCodingRequestRestatementFor` in
  `coding_workbench_intent.go`) must NAME the work, not describe the process.
  The sentence follows `codingRequestDecision.Kind` from the whole-request
  classifier (`resolveCodingRequestDecision`). Kind and acceptance tokens
  are matched with surrounding space removed and case folded
  (`normalizeCodingRequestDecision`), so a capitalized token stays the
  decision the model returned. `parseCodingRequestDecision` still counts
  that decision when needs_plan or acceptance is null, a string, or 0/1,
  when a comma sits immediately before } or ], and when the model echoes
  the schema the last valid object is the decision. The host does not scan the
  wording for 哪些/什么/吗/怎么/改成/介绍/完/好/图形界面 or any other marker;
  those lists misclassify other questions. The first command of a new task
  (`codingOpeningTaskCommand`: no turn, no written files, no summary, and no
  stored title that names other work) is a task analysis, not an echo.
  A stored title that is only the leading clause of this command
  (`codingStoredTitleIsThisCommand`) is still the opening command. The
  requirement is the next clause (`codingOpeningGoalPastTitle` /
  `codingDescribePeeledOpening`: 「constraint」限定的是做法). The prefix
  dropped is the stored title, even when the headline swallowed the next
  clause (`codingHeadExtendsTitle` requires a boundary rune, so 数据 does
  not match 数据库). A colon body is material when it continues on the next
  line or runs past 40 runes (`codingSplitColonBody` cuts at the last colon
  whose left side is still one line, so 标题：识别印章：\n正文 keeps the
  goal). It is named as 材料 and is not pasted. A further requirement before
  that colon stays in scope; a remainder of 2–24 runes with no newline is
  quoted (同时还要「…」), and a longer one stays 其余要求. A title colon
  that opens onto the next line makes the title the goal and the rest
  material. A one-line tail of 40 runes or fewer stays part of the
  requirement. The opening model prompt replaces the body with
  ［已附正文，N字，M行，此处不引用］ (`codingRedactSuppliedBody`); the chat
  sentence has no character count. A model reply that quotes any 8-rune
  window from the first 80 runes of the body
  (`codingAnalysisQuotesSuppliedBody`) is discarded, except a window that
  also appears on the instruction side. The desktop file-picker block
  (`[用户选择的本地文件路径]` plus paths) is an attachment, not another
  requirement and not a colon body: `codingSeparateOpeningInput` analyzes
  the stripped instruction, the host says 附上的图片 or 附上的文件, and the
  opening prompt names the count without the path. OCR and auto-extract
  bodies are quote-checked the same way. A path quote has to contain a
  slash, so the file name alone is not a leak. The 48-rune gate, the
  short-edit echo, a continue cue, and the copy check use
  `codingTypedRequest` (the picker block removed). A short edit with an
  attachment stays 「这次…，其它先不动」 and does not quote the path. A
  bare document name such as paper.pdf still counts as an attached file.
  An attachment-only send keeps that empty typed text. A stored short title
  is the goal and the file is material; with no title the goal is
  处理附上的图片 or 处理附上的文件. A later file drop stays
  「这次处理附上的文件，其它先不动」. The path and the picker marker are
  not quoted.
  `publishCodingRequestUnderstanding` asks the lightweight model first
  (`codingOpeningTaskUnderstandingSystemPrompt`); the host stand-in for a
  long opening command is `codingDescribeOpeningCommand`
  (`codingDescribePeeledOpening` when the title is the leading clause,
  `codingDescribeLongOpeningAnalysis` otherwise). The short no-title
  stand-in stays `codingDescribeOpeningTask` ("这是新建任务的第一条指令。要完成的是「ask」。…确认前不改文件。").
  That paragraph is what the chat shows, and it stays in front of the
  plan-approval text. A later short edit (≤48 runes) still echoes itself via
  `codingDescribeNewAsk` ("这次<ask>，其它先不动"). Inquiry uses `codingDescribeQuestion`
  ("对照当前实现，回答「q」，这一步只读不改文件"); a session that already
  wrote files drops 不改文件 ("对照现有实现，回答「q」。"). An operational
  launch (acceptance empty or launch, not command) says
  "运行当前已有程序并确认它能正常工作，这一步不改源代码。"
  With a work name that becomes
  "在「name」里，运行当前已有程序并确认它能正常工作，这一步不改源代码。"
  A continue cue (`codingWorkbenchContinueCue`) wins before kind, so 继续
  stays a resume. When the classifier is down the decision is
  implementation. An opening command then uses the opening-task paragraph
  (it says 确认前不改文件 because the plan still needs approval). A later
  short edit is the edit echo. Neither sentence is invented as a read-only
  answer from the words. `codingRequestLooksModeratelyComplex`
  only raises NeedsPlan after the judgment is already implementation, or
  inside that no-model fallback; it must not skip the classifier.
  Publish runs on the classifier decision and before
  `applyCodingSessionImplementationPosture`, so a written-session inquiry
  keeps the answer sentence while write tools can still be restored.
  Inquiry and an operational launch keep the host sentence
  (`codingRequestUnderstandingKeepsHostFallback`); a second model must not
  overwrite 不改文件 / 不改源代码. A work name that is the same sentence
  as the ask is skipped (`codingRequestWorkName`), including a title whose
  only difference is a short comma note. The next candidate is used: the
  previous summary, then the project identity. A readme line that is not a
  short title does not hide a later CMake name, a single source stem, or a
  unique binary. A stored line that contains a sentence boundary
  (。！？.!?；;…, including a mark with more words after it) is not a
  title, so it does not hide the project name. A dot inside an identifier
  such as snake.cpp or v1.2 still counts. The readme walk skips a sentence
  and can use a later short title; other prose does not hide the CMake
  name. A project() in a comment, or a ${variable} name, is not the
  project name. A task titled with the user's command
  is not repeated as 在「同一句话」里, and that title does not hide the
  project name. A longer follow-up still leads with its first requirement via
  `codingRequestHeadline` ("先处理「<head>」，其余要求一并处理"). The opening
  command, including a long one, uses `codingDescribeOpeningCommand` instead
  of that headline template. The
  headline peels leading discourse openers ("太好了！", "帮我，") and
  sub-2-rune fragments, cuts at the first sentence boundary, only accepts a
  comma clause ≥6 runes as the head, and resolves numbered breakdowns the
  same way the session-goal echo does (`codingRequirementGoalCut`): the
  goal before （1） is the requirement, a bare list names its items
  ("改A、改B" — first item in full when titles exceed the 16-rune cap),
  and any remainder past the head keeps `more` so "其余要求一并处理"
  acknowledges it. The generic process filler
  ("先看清当前项目里相关的实现和约束…") survives only for inputs with no
  nameable ask — punctuation-only text or a bare continue with nothing
  stored. The lightweight-LLM refinement (`llmCodingRequestRestatement`)
  still replaces the fallback when the host sentence is not locked. An
  opening-task analysis is not refined again:
  `refineCodingRequestUnderstanding` keeps the paragraph already shown, and
  the plan-approval text is appended under it
  (`joinCodingUnderstandingAndBody`), so that analysis stays on screen when
  「## 需要确认执行计划」 arrives.

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

## Cloud/remote task cover view (guiapp/frontend)

- The remote/cloud session (`useRemoteAssistantSession`) COVERS whatever tab is
  active — `activeTabId` stays underneath untouched. Any tab activation must
  retire the cover (`useRemoteCoverRetireOnTabActivate`, panel-level
  activeTabId-change effect): sidebar task-row clicks pre-close via
  `activateSwitchingTab`, but resume/new-tab/wizard-launch paths only change
  `activeTabId`, and without the effect the opened task sits invisibly under
  the cover ("点了没反应"). A cloud→cloud handoff (the wizard creating the
  next cloud task) publishes a new session without touching tabs, so it is
  unaffected.
- The task header's remote branch is `[暂停任务] [TaskTabSwitcher]
  [BugReportButton]` — same cluster as the local header, NO standalone 关闭
  (`remote-task-chat-close` removed 2026-11-25; do not re-add it — the
  switcher is the escape, matching the local experience). 分享/更多 stay
  local-only: cloud tasks have no save-as-task/clear-history mechanisms.
- 新建任务 over the cover: `openNewTaskWizard` treats `remoteAssistantActive()`
  as covering and always opens the wizard overlay — the `localGuideVisible`
  early-return only fires when the guide is actually rendered, and under the
  cover it never is (the remote view wins over the local welcome page).
  `wizardScreen` therefore includes `|| !!remoteChat`. Composer wiring keys off
  `wizardCoverActiveRef` (panel ref synced after `useTaskConfigWiring`):
  typing/`handleClearInput`/paste-drop guards write the LOCAL draft while the
  cover is up (otherwise keystrokes vanish into the covered session's
  remoteDraft), the slash palette bails (`handleFireSlashCommand` returns while
  the cover is up — palette picks must not leak into the covered session),
  wizard-typed /clear-/new-/branch reset commands DROP in both send paths
  (`sendOnCurrentPage` and the chip path `handleWelcomePromptSendWithTaskConfig`
  return early with the cover KEPT up — they must neither leak into the covered
  session nor dismiss the cover out from under the user),
  `wizardComposerCanSend` replaces the remote-draft-keyed
  `canSend`, and the wiring's `showComposerText` stash-restore clears the ref
  first because it runs before the overlay-closed re-render.

## 云端 Agent 开通开关 (guiapp/frontend)

- `ListCloudAgents().enabled` is the Hub TENANT switch for cloud agents, and
  the settings rail's 云端 Agent tab is gated by it:
  `SettingsTabVisibility.hideCloudAgent` (`config/settingsTabs.ts`) filters
  the rail entry and `resolveSettingsTabId` redirects a cloudAgent request to
  general; `SettingsActiveContent` receives it as the REQUIRED prop
  `cloudAgentAvailable` and resolves before render, so a hidden tab never
  paints an empty body. Nothing deep-links `tab: 'cloudAgent'` today, and if
  one ever does it lands on general like every other hidden tab.
- The single source of truth is `components/layout/cloudAgentAvailability.ts`
  — a module store (`noteCloudAgentsEnabled` / `useCloudAgentsEnabled`) with a
  REFCOUNTED poller (starts on the first subscriber, stops at zero; visible
  interval + `cloud-agent-list-refresh` event). POLL_MS is 60s ON PURPOSE: the
  task list polls the same endpoint every 30s anyway (it needs instances) and
  feeds the store via `noteCloudAgentsEnabled`, so the shared poller only has
  to keep the value warm while the sidebar is absent.
- Do NOT gate the settings tab off `useCloudTaskPlace()`: that snapshot's
  publisher (`SidebarTaskManagement`) only lives while
  `navTab === 'ai' && !taskPaneCollapsed`, and pane collapse is PERSISTED — a
  collapsed user would never see the tab. The availability store exists
  precisely to decouple the gate from the sidebar's lifecycle.
- Semantics: unproven = closed (the `noted` flag — no successful answer yet
  hides the tab, same as the 云端 place); a REJECTED call is not an answer
  (signed-out / transient network) and keeps the last value; only the newest
  fetch may write (`fetchGeneration` retires in-flight requests on
  unsubscribe).
- Tests: the store is module-global, so any suite rendering a consumer must
  call `resetCloudAgentsEnabledForTests()` (SidebarTaskManagement's afterEach
  does). No test renders the whole `App`, so the poller only starts where a
  consumer is actually mounted.

## Sidebar collapse 功能栏/任务列表折叠 (guiapp/frontend)

- State home: `src/components/layout/useSidebarCollapse.ts` — two booleans
  persisted to `maclaw:task-pane-collapsed.v1` / `maclaw:nav-rail-collapsed.v1`
  (localStorage, same family as `maclaw:task-pane-width`). Ctrl/Cmd+B toggles
  the task pane (AI workbench tab only), Ctrl/Cmd+Shift+B toggles the nav rail
  (every tab). The hook owns the global keydown listener; don't add another.
- The toggle pair (`SidebarToggleCluster`, tests
  `__tests__/SidebarToggleCluster.test.tsx`) lives at the RIGHT END of the
  task pane's search row (`.mc-task-pane__search-row` in
  `SidebarTaskManagement`, fed the `sidebarToggles` prop — a
  `SidebarTogglesState` object). AppSidebarShell composes that object from
  its existing collapse props plus its `onToggleRail` prop (App passes
  `toggleNavRailCollapsed`) and threads it down `SidebarAiPane` →
  `SidebarTasksPane` → `SidebarTaskManagement` as ONE object — keep the
  object shape, do not split it back into flat props (SidebarAiPane sits at
  its 385-line file-size guard). When the pane collapses the search row goes
  with it, so the AI headers (welcome `AssistantTitleBar`, execution
  `.mc-task-execution-header`) render the cluster as a restore-only fallback
  gated on `sidebarToggles.paneCollapsed` — expanded headers show NO
  collapse buttons. Non-AI `MainTopHeader` keeps its rail-only instance (the
  pane does not exist off the AI tab). With both columns collapsed it sits
  at the window's top-left corner, WorkBuddy-style. The buttons are plain
  `<button>` (already excluded from drag arming by
  `WINDOW_DRAG_EXCLUDE_SELECTOR`); their mousedown/double-click
  stopPropagation keeps the header's drag + double-click-to-maximize intact.
- Collapsing is UNMOUNT, not CSS: `AppSidebarShell` drops `SidebarNavRail` /
  `SidebarAiPane` and recomputes
  `shellWidth = railWidth + (paneVisible ? paneWidth + (railWidth ? GAP : 0) : 0)`
  for both the shell div and the drag strip (`--mc-task-pane-width` itself is
  untouched, so width survives collapse/restore; contract locked by
  `__tests__/AppSidebarShell.test.tsx`).
- **Shell-width authority**: the `!important` block in
  `120-app-responsive.css` (four breakpoints, `#App[data-nav-tab='ai'] > …`)
  beats any inline width, so collapsed widths reach it through the
  `--mc-sidebar-shell-width` inline var (consumed with the expanded calc as
  `var(--mc-sidebar-shell-width, <per-breakpoint calc>)`; never add new
  `!important` — the css-discipline ratchet is at zero headroom, EDIT values
  in place instead). `AppSidebarShell` mirrors the breakpoints in JS (rail
  112 / 84 ≤1200 / 64 ≤560, pane hidden ≤900 via a bucket-only resize
  listener — bucket changes re-render, pixel drags don't) and ALWAYS sets the
  var; keep the JS mirror and the CSS block in lockstep when touching either.
  Pane MOUNT stays manual-collapse-only (not window-width): unmounting on
  narrow windows would kill the pane's event listeners.
- `.sb-toggle-btn` colors must use `--theme-text-secondary` /
  `--theme-text-primary` / `--theme-primary` — `--theme-text` is UNDEFINED
  anywhere in the app and actively banned by
  `utilitiesDarkModeContrast.test.ts` (a hover that falls back to its light
  hex is black-on-black in dark schemes).
- Because the pane unmounts, in-pane event consumers die with it. The hook
  listens for `EVENT_OPEN_CREATE_CODING_TASK` WHILE collapsed, restores the
  pane, and re-dispatches once via `setTimeout(0)` (after the pane's listeners
  re-attach; the hook's own handler is gone by then, so exactly one retry).
  Anything else that means "user intends to reach the workbench/task list"
  must call `openTaskPane()`: rail 工作台 + 数字员工 items,
  `createExpertTask`, `UtilitiesPage onOpenExpert`, TutorialPage back. Paths
  that only open an execution tab (`openCodingTask`, `activateTaskInstance`,
  VE/history opens from the list itself) intentionally do NOT force-restore.
- Double-clicking the pane resize divider (`SidebarAiPane`) also toggles the
  pane; the two resize start/stop cycles it produces leave no stuck state
  (the drag effect cleanup unconditionally resets cursor/userSelect).

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
- 2026-11-24: a second OOM was NOT the main renderer — dom_nodes stayed
  ~1500 and js-heap kept reporting while `webview_procs` went 7→8 and the
  tree totalled 98→975MB in ~90s of mouse activity. The growing child was
  the desktop-stage iframe (`h264.html`/noVNC), a cross-origin OOPIF with
  its own renderer that `performance.memory`/`dom_nodes` never see — the
  frontend sampler covers the top frame only. Fixes:
  - `desktopViewerSrc` takes a `DesktopStreamSpec{w,fps}` that lands on
    `h264.html?w=&fps=` (the page forwards both to `/h264`; `w` scales
    encoder output server-side, `fps` can only be lowered). A URL that is
    already `h264.html` gets the spec appended before any `#hash` (a
    `w`/`fps` already in the query wins — the page reads the first match).
  - `DesktopBotWorkspace.desktopFrameSource` memoizes the iframe src and
    samples size ONCE per mount (deps = the iframe's remount key triggers
    — url, watch↔live, viewer, is-fullscreen — so expand/collapse and
    takeover reload at the new stage size but a list-divider drag never
    reloads the stream). Embedded stage lives in the ≤400px
    `.desktop-bot-panel` (32px padding + 8%-a-side gutter, 4% <960px
    viewport) ≈ 310 css px; is-fullscreen is fixed inset:0 → innerWidth.
    Physical px = layout × devicePixelRatio × uiZoom
    (`rect.width/clientWidth` of `workspaceRef` folds the transform in);
    w clamped 640–1920, watch→fps=10, interactive keeps server default.
  - `streamHidden` unmounts the iframe after 60s of `document.hidden`
    (hidden OOPIF keeps ws+decoder churning); quick task switches keep
    it, restore remounts and reconnects. Interactive frames are exempt:
    a live /h264 connection heartbeats the gate's `viewer_takeover` file
    (5s lapse → `takeover_state` stops reporting "人类接管" → bot
    resumes), so unmounting would silently release the hold mid-takeover.
    `streamHidden` initializes from `document.hidden` at mount, so a
    hidden app start never connects in the first place.
  Bot container native is `MACLAW_DESKTOP_GEOMETRY` (default 1440x900,
  max 3840x2160); `view_width` outside [320, native) is ignored.
  `perfTakeSample` now also dumps the per-PID WebView breakdown without
  =verbose when the tree grows >128MB in one 30s window, so the next
  OOPIF balloon names its PID in the default log instead of needing a
  verbose rerun. The sample line carries `self_mem_mb` too, and the same
  >128MB window growth on the Go side logs a `self-growth` line that
  points at MACLAW_HEAP_PROFILE — "which side of the webview boundary
  ballooned" is answered by the default log either way.

## MCP host LAN access (guiapp)

- The tools-page MCP server is loopback-only by default. `mcp_host_lan_enabled`
  (corelib `McpHostLanEnabled`, nil = off) widens the bind to `0.0.0.0`
  (`listenMCP(preferredPort, lan)` — the wildcard bind makes `127.0.0.1`
  reachable too, so there is only ever ONE listener; do not add a second one
  on another port). `PatchConfigFields` restarts the host through the
  existing `mcpHostChanged` channel.
- Two request gates flip together with `h.lanAllowed` (`newMCPHTTPLAN`):
  `mcpRemoteIsLoopback` stops rejecting non-loopback clients, and
  `mcpOriginAllowed(origin, lanAllowed)` additionally accepts private-IP
  origins — public hostnames stay rejected in BOTH modes (DNS-rebinding
  guard). The Bearer key is mandatory for loopback and LAN callers alike;
  do not add a loopback auth bypass.
- The status payload carries `lanEnabled` from CONFIG in both
  `GetMCPHostStatus` and `mcpStatusWhileEnabled` — NOT from the host
  snapshot: a nil host (listener never started) must not flip the panel's
  LAN switch back to Off after a `GenerateMCPHostAPIKey` response.
  `lanURL`/`lanHTTPConfig` are built per status fetch from
  `mcpLANAddressFn()` (UDP-dial primary private IPv4, interface-enum
  fallback, injectable for tests), so a DHCP address change needs no
  listener restart. `~/.maclaw/mcp/endpoint.json` stays loopback-only for
  local discovery; the LAN URL exists only in the UI payload.

## MCP host access log (guiapp)

- `mcpAccessStore` (`guiapp/mcp_access_log.go`) records remote TASK
  INITIATIONS only: `create_task` (local + remote-coding paths) and
  `send_command` (recovery confirms included). The send record fires inside
  `mcpStartPrompt`'s `bind` — i.e. only after `mcpCommitRun` succeeds, so a
  send that never starts a run stays unlogged. Read-only tools, 401/403
  rejections, and claim replays (`mcpReplayRequest`) are never recorded.
- Client identity rides the REQUEST CONTEXT: `ServeHTTP` stashes
  `mcpClientInfo` (IP from `RemoteAddr` — no X-Forwarded-For, the listener
  is direct — plus the key MASKED by `mcpMaskKey`, which is idempotent)
  right after the Bearer check. `recordMCPAccess` re-masks at entry: never
  store the full key, the JSONL (`~/.maclaw/logs/mcp_host_access.jsonl`)
  is a user-shareable file. A context WITHOUT client info (tests calling
  the task methods directly) is a NO-OP, not an error.
- Storage: in-memory ring capped at `mcpAccessEntryLimit` (200) + JSONL
  append; past `mcpAccessFileMaxBytes` (1MB) the file is rewritten from the
  ring, and a FAILED rewrite falls back to a plain append instead of
  dropping the entry. All file ops happen under the store mutex (the MCP
  host is the single writer). Partial/corrupt lines are skipped on load.
- Panel bindings: `GetMCPAccessLog` (newest first; keys `time/ip/apiKey/
  action/task/projectPath`) and `ClearMCPAccessLog` (memory + file delete).
  The MCPServerPanel access-log card is ALWAYS rendered (history can be
  reviewed/cleared while the server is off) and live-refreshes via
  `mcp-host-message`; keep the `EventsOn` unsubscribe in the effect cleanup.
- Panel test mocks must return a FRESH array per `GetMCPAccessLog` call —
  the backend builds a new slice every call, and a same-reference resolve
  lets React's `Object.is` bail out of the re-render (this bit the panel
  test once).

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

- `mobilebot/` is the active MaclawGo phone client (login → bots → chat →
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
- Remote-task live viewer (`viewer_reducer.dart` ↔ desktop
  `reduceViewerEvent.ts` ↔ Go contract reference `viewer_reduce.go`, all
  locked to the same JSON vectors in
  `guiapp/testdata/viewer_contract_vectors.json`): the viewer feed replays
  EVERY progress publish, so ALL THREE reducers keep the desktop chat's
  sticky tail row — a repeated delivery of the same running step dedups
  against the TIMELINE TAIL by phase+task+title (coding rows; run_id
  excluded, mirroring the compat text) or exact text (taskless rows), and
  each caps the timeline at 160 like `VIEWER_TIMELINE_KEPT`. Without this
  one running step paints a row per publish ("running · 实现…" ×N in
  `_LiveRound`'s notes and the desktop remote mirror). The relay also
  collapses: `relayRemoteStreamEvent` drops a progress whose wire text
  equals the session's previous one (`viewerProgressTail`) BEFORE
  `publishViewerEvent` — cursors stay contiguous (allocated per publish,
  never skipped) and OLD phone builds, which have no reducer dedup, stop
  stacking without an app update. Heartbeats/control lanes pass through;
  queue-overflow tests must fill with DISTINCT texts. Known parse drift,
  kept: legacy-text phases go through the desktop's
  `CODING_AGENT_KNOWN_PHASES` whitelist ("done" → ''), the phone keeps the
  raw word; shared vectors must use phases both sides represent
  identically ("completed", not "done") — coding-map rows skip
  normalization on BOTH sides. Adding a vector means updating all THREE
  reducers at once — the Go one is embed-golden
  (`TestReduceViewerEventMatchesGolden`) and fails the build first when a
  consumer implementation drifts.
- Login-wall handoff ask → takeover card (`_TakeoverCard` in
  `bot_chat_screen.dart`; gate `isDesktopHandoffAsk` in `bot_models.dart`):
  a desktop-bot ask that is really a handoff (`attention_reason`/`novnc_url`
  on the same message, ask still pending) must NOT render `_AskCard`'s
  answer field — the person acts on the computer, not in chat. The card
  shows `attentionReasonLabel(reason)` (never the raw enum; unknown reasons
  fall back to themselves for forward-compat); an ask with option chips
  (choice/confirm) or a secret field is NEVER a takeover card —
  `isDesktopHandoffAsk` excludes it so the chips/obscured input stay
  usable. Plus the screenshot MaClawSrv
  attached + 「接管」→ `/bot/<id>/desktop?takeover=1` which auto-claims the
  keyboard once the first watch reports a controllable desktop (one attempt,
  no 45s retry nag). Server side: `finishDesktopActionPhase` sets
  `AskUserRequest.Takeover` (marker field, display hint "请在电脑上完成，
  或点「接管」继续。" replaces 请直接输入您的回复) and
  `desktopAttachHandoffShot` (http_agent.go, after `takeDesktopTurn` in all
  THREE reply paths) captures one screen via the round wake and appends it
  when the reply has no image yet — `ensureDesktopPictureDelivered` skips
  `awaiting_user` by design, so this is the only picture source for a
  handoff reply. The `_HandoffBanner` is suppressed while the takeover card
  is up (same reason twice); it still renders for replies without a pending
  ask. `BotDesktopScreen` is fullscreen landscape: no AppBar, no watch row —
  back/takeover/menu are semi-transparent overlays on the letterbox sides,
  the attention reason is an `IgnorePointer` pill on top, and the page runs
  `SystemUiMode.immersiveSticky` (restore `edgeToEdge` + portrait on
  dispose). Keep the `_noComputer` AppBar scaffold.
- CI: `.github/workflows/mobilebot.yml` (PR/push checks + debug APK artifact);
  `.github/workflows/main.yml` `build-android` job ships signed
  `MaclawGo.apk` into tagged Releases — needs secrets
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

## mobilebot: voice input, bot media, update signing, l10n

- Voice input on Android was dead on EVERY Android 11+ device: the manifest
  lacked the `<queries>` intent for `android.speech.RecognitionService`, so
  package visibility filtered out the recognizer and `speech_to_text`
  `initialize()` always failed → every mic tap fell to the ~330 MB local
  SenseVoice download. The manifest needs that queries intent plus the
  bluetooth permissions the plugin requests (`BLUETOOTH_CONNECT` runtime on
  S+; `BLUETOOTH`/`BLUETOOTH_ADMIN` maxSdkVersion 30 for pre-S headsets).
- Dictation locale: never hardcode `zh_CN` — `pickAsrLocale(available)`
  (`core/voice_engine.dart`) picks zh-CN → any zh-* → null (recognizer
  default); a hardcoded unsupported locale dies with
  language-not-supported before a single word is heard. Plugin side, a
  null `localeId` omits `EXTRA_LANGUAGE` entirely = device default.
- Dictation text lands in `onResult`, NOT in `onStatus`: recognized words
  go straight into the composer per callback (bot chat `_toggleListening`,
  assistant `_dictate`; snapshot the composer as the prefix right before
  `listen`). Some OEM recognizers deliver results without ever firing the
  terminal `'done'`/`'notListening'` status — gating the landing on status
  left the assistant composer stuck at 聆听中 with nothing on screen.
  `onStatus` only resets `_listening`; a `listen()` throw resets it too.
  Voice-assistant mode (`_voiceListen`) has the same trap on the SEND side:
  auto-send/relisten funnels through `_handleVoiceTurnEnd`, called from
  BOTH the terminal status and `onResult(finalResult)` — `_listening`
  makes it idempotent, so a recognizer that never fires the status still
  sends, and a status after a final result cannot double-fire into a
  spurious relisten.
- Bot reply media never travels in the journal: the projection strips
  bytes, the desktop saves delivered files on ITS OWN disk (only
  unsavable ones keep attachment ids), and Hub's `ReplyFile` carries no
  attachment id. So on the phone: `_settleReply` PATCHES media into the
  bubble the desktop's journal projection already landed (per-field —
  never clobber a restored list with an empty one), `_keepEarlierSeq`
  keeps the local media over the projection echo of our own line, and
  `_restoreSyncedMedia` PROBES idless assistant bubbles within
  `mediaRestoreWindow` (48h) — explicit `attachmentIds` still refetch
  unconditionally regardless of age. Without these, attachments vanish
  after an app restart and on a phone watching a desktop-sent turn.
- Update signature check (`MainActivity.kt` `apkSignatureMatches`):
  `false` = REAL mismatch (the OS would fail the install with
  INSTALL_FAILED_UPDATE_INCOMPATIBLE); `null` = undeterminable —
  unparsable archive, different applicationId, or EMPTY cert set
  (`signingInfo` is null for archives on Android 9/10, and v2-only APKs
  expose no legacy `signatures` pre-P; AGP disables v1 when minSdk ≥ 24).
  Treating the empty set as a mismatch blocked every legitimate same-key
  update on those devices. Dart side: `null` → let the system installer
  decide; `false` → the sheet shows uninstall-first guidance plus a
  browser download-page entry (`updOpenDownloadPage`).
- 2026-11-19 online-update signature incident: every release through
  V8.2.0.12689 shipped DEBUG-signed. `MOBILEBOT_KEYSTORE_BASE64` held
  non-base64 text, and the "Write Android signing config" step degrades
  to `::warning:: … falls back to debug signing` + exit 0 when the base64
  decode fails — with `REQUIRE_MOBILEBOT_RELEASE_SIGNING` (a repo
  VARIABLE, keep it `true`) unset, the tag build published anyway. Each
  runner VM auto-generates a FRESH `~/.android/debug.keystore` (not
  covered by the gradle cache), so consecutive debug-signed releases have
  mutually incompatible certs and in-app updates always fail with
  INSTALL_FAILED_UPDATE_INCOMPATIBLE. The secret value must be ONE
  continuous base64 line of `mobilebot-upload.jks` (PKCS12, alias
  `mobilebot`) — no certutil `-----BEGIN CERTIFICATE-----` wrappers, no
  `data:` prefix (whitespace is stripped by the step, anything else breaks
  decode). Verify a released APK with
  `apksigner verify --print-certs MaclawGo.apk`: the real key is
  `CN=MobileBot, OU=Mobile, O=MaClaw, C=CN`, SHA-256
  `DE:32:3A:AF:72:CE:0B:98:9A:9F:0B:6E:66:50:4F:ED:81:28:DA:D1:A8:FB:C2:6C:9C:27:3C:25:B2:F0:35:B3`;
  `CN=Android Debug` on a release asset means the fallback fired. The
  first release signed with the real key requires a ONE-TIME uninstall on
  every already-installed device; from then on updates install over each
  other normally.
- l10n: `app_en.arb` is fully translated; `app_zh_Hant.arb` is OpenCC s2t
  of the zh template (regenerate with s2t when new keys land);
  `app_mn.arb` is fully translated to CYRILLIC Mongolian (all 515 value
  keys, 2026-11-19 — machine-translation quality; have a native speaker
  proofread before marketing mn as polished). Tests that pump `MaclawGoApp`
  and assert zh text MUST override `initialLanguageProvider` with `zhHans`:
  the test host resolves en_US, and the translated en bundle no longer
  hides behind placeholder Chinese (see bot_route_scope_test).
- l10n rule for NEW strings: never hardcode zh in Dart. Widget code uses
  `context.S`; non-widget code (controllers, API/error mappers, pure
  helpers) uses `sharedL10n` (locale.dart) — a locale snapshot synced by
  `MaclawGoApp`'s `localeResolutionCallback` via `updateSharedL10nLocale`,
  defaulting to zh so pure-function unit tests and pre-first-frame
  behavior stay byte-identical. When a new key replaces a previously
  hardcoded zh fallback, the zh template value must match the old string
  BYTE-FOR-BYTE (see `wakeTimeout` vs `dtWakeTimeout` — near-duplicates
  stay separate keys instead of reusing one and silently changing zh
  wording). Strings that stay zh on purpose: `_voiceNames` (pending a
  locale decision), `botConfirmTaskText` (engine instruction to the
  zh-speaking desktop bot AND compared by string equality against persisted
  journal history in `bot_chat_screen` — localizing it would silently break
  that match across app versions), QR FormatException messages (caught and
  remapped to `S.qrInvalid`, never surfaced), server-text matchers
  (`desktop_wake_text` node_offline sentence, `remote_task_screen` 失败
  status check), and CJK character regexes.
- Commit set when landing the l10n migration: `l10n.yaml`, `lib/l10n/`
  (the four ARBs; `gen/` is gitignored), and the untracked
  `lib/core/locale.dart`/`chat_ui.dart`/`ui_kit.dart` must ride along with
  the `generate: true` pubspec — a pubspec without the ARBs makes CI's
  `flutter pub get` fail on the missing template. `flutter pub get`
  DOES regenerate `gen/` (verified), so the workflow needs no extra step.
- Known follow-ups beyond the 48h probe: (1) thread the attachment id
  through Hub's `ReplyFile` (`replyAttachment.item_id` →
  `attachment_id`) — MaClawSrv's `MessageAttachment.ItemID` is only set
  for Hub file-store deliveries, so the mobile's already-ready
  `BotReplyFile.attachmentId` parse + unconditional `attachmentIds`
  refetch would then cover old media indefinitely; (2) large deliverables
  (>700 KB base64 cap) still never reach the phone — that needs a mobile
  download path over Hub's bot file store, not just an id; (3) the
  `_voiceNames` in voice_settings_sheet are zh proper names, kept
  untranslated on purpose pending a locale decision.

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

## HubCenter node visibility authority (hubcenter)

- `hub_instances.visibility` has two writers: the hub itself (every
  heartbeat/re-registration reports its own `hub_visibility`) and the
  HubCenter admin (`POST /api/admin/hubs/{id}/visibility`). A reported
  value used to win unconditionally, silently reverting admin edits.
- `hub_instances.visibility_locked` is the arbiter: the admin UPDATE sets
  it atomically; `applyReportedVisibility` (hubs/service.go) ignores
  reports while locked and clears the lock once the hub reports the same
  value (convergence — the hub then owns the field again). Empty reported
  visibility is a no-op, never normalized to "private".
- `hubRepo.UpdateRegistration` and the `ON CONFLICT DO UPDATE` in
  `ReplaceConflictingHubInstance` both write the pair under the same CASE
  guard on the live row, so a full-row write or replica built from a
  pre-admin snapshot cannot clobber the lock — do not "simplify" them
  back to literal SETs. A replica that carries the converged state still
  applies (ELSE branch), so peer locks/convergences replicate normally.
- `hub_instances.visibility_locked_by` records which admin set the lock
  (username from `AdminFromContext`, fallback email then "admin").
  `normalizeHubRegistrationPolicyFields` enforces the invariant
  `locked=false -> locked_by=""`, so convergence clears the name too.
- Downlink: the heartbeat response AND the re-registration result both
  carry `visibility_lock` (`Service.HubVisibilityAuthority` →
  `{visibility, locked, locked_by}`, always a fresh row read so the value
  is the post-guard truth, not the in-memory snapshot). The hub stores it
  on `registrationRecord.VisibilityLock`, exposes it via
  `RegistrationState`, and its Center tab shows "已被 Hub Center 管理员
  {by} 接管" plus the effective value (center-tab.js `renderCenterStatus`,
  element `#visibilityLockHero` — keep that JS ASCII, CJK via \u escapes).
  A response without the field (older Hub Center) clears the display.
- To change a node's visibility from the hub side, set it on the hub's own
  admin console (writes `system_settings.hub_visibility`); the next
  heartbeat converges HubCenter and clears the lock.

## Pinned messages 置顶消息 (hub + hubcenter + guiapp)

- A notification can carry `pinned` / `pinned_forever` / `link_url` / `button_text`
  (button defaults to 查看详情 server-side when pinned and empty; link must be
  http(s), ≤500 runes; button label ≤20 runes). The fields persist through the
  HubCenter→Hub cascade as plain JSON struct fields, so both sides' structs and
  stores must stay in lockstep (hub `admin_notifications`, hubcenter
  `hc_notifications`; additive ALTER TABLE migrations live in the best-effort
  loop of each InitSchema).
- Client pull is `GET /api/v1/notifications/pinned` (machine auth): the ONE
  newest published `pinned_forever` row first, then the latest 3 regular pinned
  rows, audience-filtered like unread but WITHOUT the read-join — a pinned
  message shows regardless of read state until revoked/expired. Read tracking
  does not apply to the banner.
- guiapp: `pinnedCache` + `PullPinnedNotifications`/`GetPinnedNotifications`
  (notification_binding.go) publish `notification:pinned`; the pull rides WS
  reconnect (next to PullUnreadNotifications) and re-fires on any WS
  `notification.push` with `pinned` or a revoke.
- Frontend: `PinnedMessageBanner` (components/layout) floats fixed-positioned
  above `.mc-workbench-status-row` (anchor = statusRowRef; fixed because the
  sidebar scroll wrapper clips absolute overlays — same as the status/devices
  popovers, which sit at z-index 99999 above the banner's 60). Dots + arrows
  page the slides; the X is hover-revealed ONLY (CSS opacity) and dismisses
  ALL visible ids for the SESSION (`sessionDismissedIds` in
  usePinnedNotifications.ts, in-memory: restart shows the banner again, a
  newly-published id still pops).
- The banner's fixed left/bottom/width are measured from the anchor rect and
  MUST be re-measured when the sidebar relayouts: a nav-rail collapse/expand
  or pane-divider drag moves the row without any window resize/scroll event,
  and a stale measure made the card overhang into the workspace. The
  reposition effect therefore adds a ResizeObserver (guarded for jsdom) on
  the anchor row AND its `.sidebar` shell ancestor — the row's own box
  covers pane-width changes, the shell covers rail toggles (pure
  translation). `sessionDismissedIds` is module-scoped, so the pane
  collapse→remount cycle does not resurrect dismissed cards.
- Admin forms: hub `notification-tab.js` (CJK via \uXXXX escapes) and hubcenter
  `notification-admin.js` both gate the pinned extras (永久置顶/链接/按钮名)
  behind the 置顶 checkbox; the list/detail views badge pinned rows.
- `PullPinnedNotifications` is serialized end to end by `App.pinnedPullMu`:
  reconnect (×2 sites), a pinned WS push, and a revoke can each fire a pull,
  and without the lock a stale fetch finishing last would publish last and
  freeze the banner on an outdated list until the next event.
- Known bound: hub expiry NEVER flips status — `Service.CheckExpired` has no
  caller (no ticker wires it), so DB rows stay `published` past expire_at and
  the admin list keeps showing 已发布. Clients are still correct: every
  pinned/unread query filters `expire_at > now` in SQL, so an expired card
  disappears at the next pull (reconnect / any pinned push / revoke); a
  revoke always pushes, so admin removal is immediate. Wire CheckExpired to
  a ticker only if the admin status badge (not clients) needs the flip.
- Verified end-to-end 2026-11-20: the production pinned endpoint returns the
  full payload (`ok/items` with pinned, pinned_forever, link_url,
  button_text) for real machine credentials — the desktop-side gap was
  purely the stale embedded frontend.
- Banner-only display: pinned rows are EXCLUDED from the unread bell list —
  hub store adds `AND n.pinned = 0` to all three unread variants (legacy
  included), and guiapp's handleNotificationPush early-returns on a pinned
  push (no notifCache.Add, no notification:new / urgent-toast; just a pinned
  re-pull). MarkAllRead therefore never consumes pinned rows; the bell badge
  ignores them. The pinned endpoint is their only client surface.
- `PullPinnedNotifications` logs the result breadcrumb
  `[notification] pinned pull: N item(s)` (and the failure line on HTTP
  errors), so "banner empty" splits into hub-side (0 items / 404 = hub not
  redeployed, or rows predating the deploy lost their pinned fields on
  cascade) vs frontend-side (N>0 but no card). `GetPinnedNotifications`
  fires one background refresh when the cache is empty — covers the
  reconnect pull landing before the frontend subscribed (event lost).
- Build-chain gotchas found while shipping the banner (2026-11-20):
  `rebuild_windows_gui.ps1` had NO frontend step — it embedded whatever
  `frontend/dist` happened to hold, shipping stale UI in a fresh exe
  (build_win.bat / build_win.metastaff.bat were already correct). It now
  wipes + rebuilds dist via npm before go build and fails hard. ALSO:
  `scripts/goheavy.cmd` lost `%~dp0` after its arg `shift` loop (cmd's
  `shift` rewrites %0 too), and unquoted `-o` hit PowerShell -File's
  AmbiguousParameter (-o prefix-matches -OutVariable/-OutBuffer); the shim
  now captures the dir first and routes through `powershell -Command` with
  single-quoted tokens (space grouping preserved, `-o` passes through).

## Cloud-agent workspace jail (MaClawSrv/cloudagent)

- `jailRoot` for `KindCoding` is `workspace/<repo>` only when BOTH the
  git.json binding exists AND `workspace/<name>/.git/HEAD` is present;
  otherwise the bare `workspace/` is the root — repo-less coding is
  allowed, so "还没有检出代码库" is no longer a possible send error.
  The binding (written by ConnectGit after clone, outside the jail) is the
  SOLE authority: a scripted `.git` dir — trivially minted through an
  interpreter, since argv/path checks only cover their own arguments —
  cannot shift the root, and unbound impostor dirs demote to ordinary
  loose files merged into the real clone on connect.
- Shell git is a READ-ONLY allowlist (`gitSubcommandBanned` in jail.go):
  status/diff/log/show/etc only, and it is a GUIDE RAIL for a cooperative
  model, not a boundary — a scripted subprocess can run git directly. The
  hard invariants are: no token in `bashEnv`, the pinned push URL, and
  `noHooks` on the srv's own ops. Flags that smuggle a path
  (`--git-dir=`, `-Cdir`, `--work-tree=`, `--exec-path=`) are regex-scanned
  in EVERY argv entry and inside decoded/`-Command` strings — they reach
  outside the jail on their own, so a wrapper (`env git …`) or an inline
  command string cannot dodge the check. Every whitespace-separated FIELD
  of every argv entry gets the same jail treatment as a top-level arg
  (quote-stripped secretBase/gitBase/path checks), because `cmd /c` and
  `powershell -Command` take a whole command string as ONE argv element —
  without field scanning `cd .. & type git.json` sails through as a
  harmless-looking relative name while the invoked shell splits it and
  leaks the inst.dir secrets.
- `pushRepo` pushes to the stored `secret.Clone` URL with an explicit
  `HEAD:refs/heads/<upstream-branch>` refspec — never to the configured
  remote name — so remote-config tampering cannot redirect the askpass
  token to an arbitrary host.
- `ConnectGit` merges pre-connection workspace files into the fresh clone
  (`mergeWorkspaceLoose`): loose top-level entries move into
  `workspace/<repo>`, the clone keeps colliding names, and the loser lands
  beside it as `<name>.pre-checkout`. Bare `.git` entries are never moved
  into the clone (they would ride inside a stash dir as an embedded repo
  instead). Nothing is deleted or stranded outside the new jail root.
- `git_commit` stages with `git add -A`, not `-u`: merged scaffold files
  are untracked, and scaffold→connect→commit is the whole point. The
  `rejectSecretIndex` gate (.env/git.json/keys…) is what makes the wide
  stage safe.

## Wails v3 migration (branch wails3-migration)

- State (FINAL, v2 fully retired): wails/v3 v3.1.0 is THE runtime for all
  four apps (guiapp, TigerProxy, ClawMateMaker, srvdemo). `third_party/wails`
  is DELETED; go.mod/go.sum carry zero wails/v2 entries; the v3 fork is
  vendored at `third_party/wails3` (replace in go.mod, examples/ trimmed)
  with three local patches: second-instance exit via `TerminateProcess`
  without DLL_PROCESS_DETACH (OBS graphics-hook zombie fix, port of
  9818e301), fullscreen drag deny in `startDrag` (port of the v2
  caption_drag patch), and the window-level `HideOnClose` option
  (companion Alt+F4 must hide, not destroy).
- Seam: `guiapp/wruntime` (package NAME is `runtime` — 40 files import it
  unaliased as `runtime.X`; the import path was the only change). The v2
  delegation mode is deleted; v3.go is the sole implementation. Entry:
  `cmd/maclaw-gui` calls `guiapp.Main` (main_wails3.go — the v2 Main and
  the Wails3Enabled/Main3 dispatch scaffolding are deleted). The v2 Main
  preamble's 10 init steps (log file, program logger, companion-paper host
  argv, bm25.PrewarmDict, skill.MigrateSkillsDir, TUI dispatch) and the
  Run tail (signalFileCompanionQuit + finishDesktopExit) were PORTED into
  Main — dropping them silently regressed file logging and companion
  retirement.
- Frontend shim: `frontend/wailsjs/runtime/runtime.js` keeps the 52-export
  surface (803 call sites / 174 vitest mocks untouched) and routes through
  `v3bridge.js`; the `hasV2Runtime()` fast-path guards were deleted with
  the v2 runtime (removing the guards was the first white screen — v2
  builds lost their event channel to a nonexistent /wails URL).
  Bindings: index.html installs a synchronous `window.go.main.App` Proxy →
  `Call.ByName('github.com/RapidAI/CodeClaw/guiapp.App.' + method)` — the
  FQN is pkgPath.TypeName.Method with the FULL import path
  (v3 bindings.go: `namedType.PkgPath()`; only `package main` shortens to
  `main.` — why the spike worked with `main.SpikeService.X`). index.html
  also preloads `/wails/runtime.js` at parse time (`window.__maclawV3Runtime`)
  so EventsOn registrations land before Go emits early lifecycle events.
- RDP white screen (root cause, three layers): (1) GPU self-heal wired via
  `WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS=--disable-gpu` env var — v3.1.0 has
  no Windows GPU toggle in options; (2) preload kills the early-event race on
  `env-check-done`; (3) **RDP sessions need composition hosting**: the per-window
  `WindowsWindow.WebView2CompositionHosting: true` gated on
  `perfSessionIsRemote()`. v3.1.0's app-level `UseVisualHosting` option is
  a NO-OP (its env var has no consumer in this version — verified against
  the vendored source); the live switch is the composition controller
  (official wails3 RDP issue — stock windowed hosting never presents over
  RDP).
  Control experiment: the spike is equally unrendered over RDP, so this is
  environment-level, not app code.
- Build: `scripts\goheavy.cmd go build -tags "wails3 production" -ldflags
  "-s -w -H windowsgui" -o dist/gate_wails3_prod.exe ./cmd/maclaw-gui/`.
  No `wails3 generate bindings` needed (the Proxy routes by name). wails/v3
  desktop targets require CGO on darwin/linux (`internal/buildguard`
  refuses plain cross-compiles; `-tags purego` is the escape hatch but
  prefer a native CGO toolchain). Rapid sequential relaunch can stall
  WebView2 env creation for seconds (watchdog added in the spike, C0).
- EditMenu parity: Main attaches an application EditMenu role
  (SetApplicationMenu — Windows never renders the bar, it exists for the
  Ctrl+A/C/V/X accelerators, same as the v2 setupTray attach). Companion close paths: the frontend title-bar close button calls
  WindowHide, and the fork's HideOnClose window option covers Alt+F4
  (Main sets it for file-companion launches); `firstPaintMark` renderer reports were
  unverified under wails3 so the GPU-failure marker write is suppressed
  there (frontend_paint_watch.go gate). TigerProxy is migrated
  (wruntime seam + dual-tag dispatch, `go build ./TigerProxy` honours the
  wails3 tag); ClawMateMaker is fully on v3 (own module, go 1.26, replaces
  to ../third_party/wails3, ShouldQuit maps the v2 OnBeforeClose
  flash-active guard).
- Desktop-stage pop-out (wails3 exclusive): the 2026-11-24 OOPIF OOM fix is
  now real — `desktop_stage_window.go` (wails3 tag) exposes
  DesktopStageWindowSupported/Open/Close bindings and DesktopBotWorkspace
  renders an 独立窗口/回到主窗口 toggle in the stage dock (fail-closed
  probe; iframe unmounts while the native h264 window streams, takeover
  heartbeats continue from the standalone page).
- Resize cursors: the v2 fork patch (8-way edge cursors for frameless
  windows, 406407d5) needs NO port — v3's runtime implements all 8 natively
  (drag.ts cursorForEdge + corner detection) plus `wails:resize:` IPC.
- This machine runs MaClaw over an RDP session without GPU acceleration —
  keep `WebView2CompositionHosting` on for wails3 builds here.
- Debugging caveat on this machine: when the RDP session is disconnected or
  locked, DWM stops composing and EVERY window reports as unrendered —
  CUA/Playwright screenshots refuse (even for known-visible apps like ZCode)
  and PowerShell CopyFromScreen fails with 句柄无效. Those verdicts are
  session-state artifacts, not app evidence: pixel-level verification
  requires an attached session. In-app render instrumentation
  (`[perf] mark=first-paint` via the binding channel, paint-watchdog silence)
  is the reliable signal while disconnected.

- **PROMOTED (2026-11-27): wails3 is the shipped runtime.** The user passed
  the visual pass and approved v2 removal. guiapp's v2 Main, main_wails2.go,
  wruntime/v2.go, the hasV2Runtime frontend guards and the TigerProxy v2
  path are DELETED; build_win.bat builds wails3+CGO by default (/w3 kept as
  a no-op compat switch). main_wails3.go's Main is THE entry;
  frontend_paint_watch's GPU-marker suppression is LIFTED (the renderer
  report channel is proven — firstPaintMark/FCP arrive via bindings).
  Completion (2026-11-28): maclawplayer/maclawcapture moved to
  `guiapp/internal/` and the companion player is a v3-native owned popup
  (internal/maclawplayer/host_v3_windows.go — borderless window owned via
  GWLP_HWNDPARENT, slot-follow on WindowDidMove/Resize, composition hosting
  over RDP, PrintWindow capture view); third_party/wails is deleted,
  srvdemo is ported, and go.mod carries zero wails/v2 entries.
- Old promotion checklist (superseded): `build_win.bat /w3` is the promoted release path — the /w3
  switch flips the GUI steps (amd64+arm64) to -tags wails3 with CGO_ENABLED=1
  (default without /w3 stays v2, CGO off); build_win_wails3.bat remains the
  single-arch dev loop; then retire the wailsjs runtime.js v2 fast path and
  main_wails2.go once the v2 binaries are gone from all install bases. The
  47-file wruntime import swap is already applied in the working tree and
  rides with the next user commit batch. `qwinsta` is the fastest check for
  why pixels are unavailable (状态=断开 → session artifact). over an RDP session without GPU acceleration —
  keep `UseVisualHosting` on for wails3 builds here.
