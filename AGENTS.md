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
- A full-package `guiapp` run can wedge in `ensureRemoteInfra`'s
  `sync.Once` vs `remoteSessionsMu` (ABBA across tests); the same tests pass
  in isolation.
- Real-model tests are gated by `internal/testenv` (`SkipModelTests` /
  `ModelTestsEnabled`): they self-skip under `go test -short` or when
  `MACLAW_TEST_MODELS=off`/`0`/`false`. `go test ./...` on a dev machine
  with `~/.maclaw/models` installed will otherwise load several GB of
  weights alongside the running app. Opt back in explicitly with
  `MACLAW_TEST_MODELS=on`.
- Local MCP tests must not spawn real servers: `LocalMCPClient.tryStart`
  refuses any command that is not the `GO_WANT_HELPER_PROCESS` re-exec of
  the test binary (`errLocalMCPSpawnSuppressedUnderTest`). Without this an
  imported `{"command":"npx","args":["-y","@playwright/mcp"]}` spends ~3
  minutes per server in initialize/discovery retries and leaves node
  children behind — this is what timed out a full `guiapp` run.
  To exercise the process path, use `newHelperLocalMCPServerEntry`.
- Beware `-run` regex quoting through `scripts\goheavy.cmd`: `-run 'a^'`
  arrives as `-run a` (the `.cmd`/`powershell` layers eat the quote), so a
  "compile-only" selector can silently become "run ~600 tests". Verify the
  selector survived by checking the first `=== RUN` line, or use
  `go vet` for a type-check-only pass.

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
- Cheap commands (`go version`, `go env`, grep/rg, `go test` on small
  leaf packages) do not need the gate. If a run seems to hang, check
  `%LOCALAPPDATA%\MaclawHeavyLocks\` for who holds the slots.

## Logs

- `~/.maclaw/logs/maclaw.log` — shared by main app, companion, and quit relays.
  `[file-companion]` = companion-side; `[exit-lifecycle]` = shutdown lifecycle;
  `[boot]` = early startup markers.
- `~/.maclaw/logs/pet-companion.log`, `registration.log` for those subsystems.
