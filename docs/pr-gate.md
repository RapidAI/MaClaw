# Pull-request gate

Workflow: `.github/workflows/pr-check.yml` (`PR check`). It runs on `pull_request` for the merge commit GitHub builds from the PR head and the base SHA. It does not run on tag pushes. Release publication stays in `.github/workflows/main.yml` (tags and `workflow_dispatch`, `contents: write`). Flutter stays in `.github/workflows/mobilebot.yml`.

The token is `contents: read`. The workflow does not read secrets and does not use `pull_request_target`. Fork PRs use that same read-only token. Jobs time out, one PR cancels its previous run, and Go / npm caches are keyed from `go.sum` and `guiapp/frontend/package-lock.json`. Branch protection is not changed by this file. The single status to require later is **PR check / aggregate**. A skipped expensive job is not itself a green check.

`aggregate` prints one first line:

| `PR_GATE_RESULT` | Meaning |
| --- | --- |
| `pass` | Every suite the plan selected succeeded. |
| `pass-docs-only` | Every changed path is documentation. Expensive jobs were skipped on purpose. |
| `pass-native-gui-not-run` | Only native GUI paths changed. They were not compiled. |
| `pass-delegated-mobilebot` | Only `mobilebot/**` changed, and `mobilebot.yml` still has a `pull_request` path filter that matches. Flutter was not run here. |
| `pass-explicit-skips` | Every changed path is an explicit skip below, or a mix of those skips. Not a test pass of those trees. |
| `fail` | Selection failed, a selected job failed or was skipped, or a job failed unexpectedly. |

A selected `go test` that prints `[no tests to run]` or `[no test files]`, or that does not run each named test, fails the job. An empty diff fails selection. A path that is not a documented class fails selection. `MaClawSrvv/` is not `MaClawSrv/`.

## What runs

| Change | Jobs |
| --- | --- |
| `MaClawSrv/**`, `corelib/agentservice/**`, `corelib/agent/**`, `corelib/agentruntime/**`, `corelib/desktopbot/**`, `corelib/browser/**`, `hub/internal/botmgmt/**` | `lifecycle`, `hub-outcome` |
| Module manifests: `corelib/agentruntime/builtin.go`, `corelib/plugin/manifest.go`, `corelib/skill/package_manifest.go`, `MaClawSrv/desktop_runtime.go`, `MaClawSrv/bot_pdf.go`, `MaClawSrv/desktop_local_relay.go` | those two jobs, plus `go test` of the manifest's own package when that package is not one of the three named-suite packages |
| `go.mod`, `go.sum` | `lifecycle`, `hub-outcome` |
| `.github/workflows/pr-check.yml`, `scripts/pr-gate/**` | `lifecycle`, `hub-outcome`, `frontend` |
| Another Go package that contains `_test.go` | `go test` of that package (`go-packages`, at most 30 packages) |
| `guiapp/frontend/**` | `npm ci`, vitest, `tsc --noEmit`, `vite build` |
| Markdown and other known doc types (`*.md`, images, and the same types under `docs/`, `.cursor/`, `.agents/`) | selector tests only |
| A rename or copy | both the source path and the destination path. A Go source moved into `docs/` still selects the contract suites, and the executable destination is rejected |

`MaClawSrv`, `corelib/agentservice`, and `hub/internal/botmgmt` are not run as full packages. The named tests below are the suite. Other tests in those three packages are outside this gate.

`lifecycle` (`go test -race -count=1`, `MACLAW_TEST_MODELS=off`):

- `./MaClawSrv`: `TestDesktopReceiptPersistsVisibleOutcome`, `TestDesktopDeliverableExtractionChain`, `TestPlanFileIsNotDelivery`, `TestTextItemsNeedExplicitIDs`, `TestHTTPReadDuringFinalize`, `TestDesktopReceiptStaysUnreadyPastTwoSeconds`, `TestDesktopAbandonedReceiptBecomesUnverified`, `TestDesktopReceiptPersistFailureIsNotReady`, `TestBotPDFWorksWithAndWithoutDesktopAndAttachesToTurn`, `TestNoDesktopAsyncFinishKeepsGeneratedPDF`, `TestBotMCPClientRuntime`
- `./corelib/agentservice`: `TestDesktopDeliverablesOnlyForExecuteDesktopTurns`

`hub-outcome` (no race, httptest, no credentials):

- `TestDesktopRunTaskOutcomeReachesEveryReader`
- `TestDelegatedRunEndHonoursTaskOutcome`
- `TestSettleFinishedRunAgreesWithUndeliveredReceipt`
- `TestDelegatedRunEndLeavesRunningJobOpen`

Frontend install is `npm ci` against `guiapp/frontend/package-lock.json` (Node 22). `npm run build` is not used: its `prebuild` runs `go run scripts/check_wails_bindings.go` and the UI-guard scripts. Those stay with the native GUI build. Vitest must report `numTotalTests >= 1` and `numFailedTests === 0`. The vite step must leave a non-empty `guiapp/frontend/dist/index.html`. CSS partials are assembled into `src/App.css` by `prebuild`; this job does not re-assemble them. Vitest, `tsc`, and vite all run; the job fails if any of them fails. A vitest failure does not skip the typecheck or the build.

On this tree (frontend files identical to `1fb27f61e`), `node scripts/pr-gate/run-vitest.mjs` reported **2 failed, 8880 passed** (522 files). The failures were `FileCompanionWindow` “stays quiet when the conversation export dialog is cancelled” (`showItemInFolder` was called with `C:/out/a-伴读对话.pdf`) and `AIAssistantPanel` “shows card store action in the title bar tools group” (the title-bar tools group had no `Notifications` button). Run again, that companion case passed alone, and the notifications case failed again. They are not excluded. `npx tsc --noEmit` and `npx vite build` exited 0, and `dist/index.html` was non-empty. Until those vitest failures are fixed in their own change, a PR that selects `frontend` — including a change to this workflow — has a red `frontend` job.

Go is the version in `go.mod`. `GOMAXPROCS=2` and `GOMEMLIMIT=5GiB` on the Go jobs.

## Explicit skips

These are not successes of the skipped tree.

- **native-gui-not-in-scope.** `guiapp/**` outside `guiapp/frontend/`, `cmd/maclaw-gui/**`, and `wails.json`. Ubuntu CI does not install GTK / webkit. Native compilation stays separate. `guiapp` tests such as `TestDesktopTaskOutcomeDecidesCompletion` are not run here.
- **delegated-mobilebot.** `mobilebot/**`, only when `mobilebot.yml` still triggers `pull_request` for that path. Hub Go files that the Flutter workflow also filters are still Go packages for this gate; they are not delegated.
- **third-party-not-in-scope.** `third_party/**` is not built or tested here.
- **hub-admin-baseline.** `hub/web/**` is not a passing suite. On `1fb27f61e` (`origin/main` when this gate was added), `node hub/web/admin/validate-admin-modules.js` exited 1 before any admin behavior was exercised: non-ASCII in `bot-tab.js` and `desktop-tab.js`, and empty i18n nodes in `hub/web/admin/index.html`. That command was not turned into a green check by dropping files.
- **Untested package.** A Go directory with no `_test.go` fails selection. There is no allowlist that turns that into a pass.
- **Unclassified path.** Anything else fails selection, including `.github/workflows/main.yml`. Add a class in `scripts/pr-gate/policy.mjs` in a reviewed change; do not catch it with a default pass.

## Run the same checks locally

From the repo root, on the same commit you want to judge:

```bash
node --test scripts/pr-gate/*.test.mjs

# Plan for the current branch against origin/main. Do not feed
# `git diff --name-only` into --files: that listing drops a rename's source.
git fetch origin main
node scripts/pr-gate/select.mjs --git origin/main

# Named suites. -json is required; a bare go test exit 0 is not this gate.
export MACLAW_TEST_MODELS=off GOMAXPROCS=2
node scripts/pr-gate/run-go.mjs --suite lifecycle
node scripts/pr-gate/run-go.mjs --suite hub-outcome

# One package the plan printed (the variable is not a shell glob).
PR_GATE_PACKAGES=./corelib/plugin node scripts/pr-gate/run-go.mjs --from-env PR_GATE_PACKAGES

# Frontend. Lockfile install, then the three checks.
cd guiapp/frontend && npm ci && cd ../..
node scripts/pr-gate/run-vitest.mjs
( cd guiapp/frontend && npx tsc --noEmit && npx vite build --config vite.config.ts )
test -s guiapp/frontend/dist/index.html
```

`node scripts/pr-gate/select.mjs --git <base-sha>` is what CI runs after `git fetch --depth=1` of the PR base. It diffs that commit against `HEAD` and exits non-zero when the plan is rejected. A missing base commit is a failure, not an empty plan.

## Stage-0 acceptance that this gate does not claim

No post was published, nobody was contacted, and no authenticated task was run. Production was not changed. The checks below are source and httptest only.

### Cross-version srv / Hub / GUI

Current source, covered by the named tests:

- MaClawSrv can finish a run as `succeeded` and still store `task_outcome` `done`, `partial`, or `unverified`. A text answer with an empty contract is `done`. A plan-phase file is not delivery (`TestPlanFileIsNotDelivery`). Several `text` items stay `unverified` until `finish` names their ids (`TestTextItemsNeedExplicitIDs`). File and post items need their own ids; the same bytes or the same post do not close a second item (`TestDesktopDeliverableExtractionChain`).
- `desktop_ready` stays false while the receipt is still being written, including past two seconds (`TestDesktopReceiptStaysUnreadyPastTwoSeconds`). A GET during that window is the race in `TestHTTPReadDuringFinalize`: readers see a saved snapshot, and the run becomes ready only after the receipt is stored. A failed write stays unready (`TestDesktopReceiptPersistFailureIsNotReady`). A receipt abandoned by a dead process reopens as `unverified` (`TestDesktopAbandonedReceiptBecomesUnverified`).
- Hub, reading a fake srv, treats a finished run with no `task_outcome` or an unknown value as `unverified`, except a turn that is asking a question (`TestDesktopRunTaskOutcomeReachesEveryReader`, case `older srv`). A not-done outcome is prefixed onto `send_to_user` so a client that only shows that sentence does not call the task finished. Delegate mail uses `done` only for `done` (`TestDelegatedRunEndHonoursTaskOutcome`).
- While `receipt_pending` is `1` and `desktop_ready` is still false, Hub keeps reading for `admitReceiptWait` (8 seconds). That is longer than MaClawSrv's `desktopContractWait` (5 seconds), so a receipt that becomes ready inside the server's extraction wait is the body Hub settles (`TestPendingReceiptWaitsUntilReady`). A pre-receipt `task_outcome` of `done`, or an empty body, is not settled as done or as a quiet success (`TestPendingReceiptIsNotDone`). A run with no `receipt_pending` mark still uses the 3 second grace. The poll stops at the bound. These two tests are httptest JSON, not a live srv.
- The current GUI notifies completion for a desktop run only when `task_outcome` is `done` (`guiapp/app_tray_notify.go`, `TestDesktopTaskOutcomeDecidesCompletion`). That GUI test is not executed in this workflow.

Not covered, and not run against live processes:

- An already-deployed older srv, Hub, or GUI binary. The Hub test feeds JSON; it does not start an old `maclawsrv`. Online srv at `1fb27f61e` already speaks this receipt protocol. A Hub binary built before `admitReceiptWait` still stops at 3 seconds.
- A GUI build that does not send `item_id` / `item_ids`. Numbered file and post slots close only when the id matches (`MaClawSrv/desktop_watchdog.go`). One `text` item can still close from a non-empty answer. Several `text` items cannot.
- `desktop_mode` values `shared`, `isolated`, and `none` on a real Hub and desktopd. The automated none-desktop cases are `TestBotPDFWorksWithAndWithoutDesktopAndAttachesToTurn` and `TestNoDesktopAsyncFinishKeepsGeneratedPDF` (PDF bytes still attach) plus the no-computer text answer inside `TestDesktopReceiptPersistsVisibleOutcome`.

### Bot 1–3 smoke

These are the three desktop modes on one account. They were not executed. Do not treat a green `aggregate` as this smoke.

1. **Bot 1, shared desktop.** Ask for a text answer and read the run: `task_outcome` `done`, and `desktop_ready` true only after the receipt is stored. Ask for a publish and leave it unverified (a process screenshot is not a post). Expect `unverified` or `partial`, not `done`.
2. **Bot 2, isolated desktop.** Same two reads on that bot's own screen. This gate does not start a display.
3. **Bot 3, `desktop_mode=none`.** A text answer is `done`. A generated PDF is still attached and is not dropped because there is no screen. Do not publish a post.

Stop there. No credential, no production write, no claim that the three bots finished a real task.

## Out of scope

Native GUI builds on Windows, macOS, and Linux, Hub / hubcenter / GUI release binaries, and srv / desktopd deployment are not this workflow. `go test ./guiapp` and `go test ./...` are not this workflow. A full `MaClawSrv` or `hub/internal/botmgmt` package run is not this workflow.
