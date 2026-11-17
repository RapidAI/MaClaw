# Desktop bot improvement program

中文摘要：这是分阶段改进桌面 bot 的路线，不是一次性重写。本 PR 只落地第 1 阶段：在现有校验路径上做精确读回、有界换策略，以及带条件的已验证输入策略复用。隔离桌面没有人和 bot 抢键盘，不移植前台锁、PowerShell 或固定长等待。参考实现只读设计，不复制源码。未合并的 PR 57、60、61 保持各自边界。

Reference pin: [nightly-labs/openbot](https://github.com/nightly-labs/openbot) `f694dea63e971afc3fd856d6ccff6c5a286ce347`. The user identified that repository as the installed app. The exact installed build was not verified, so this note cites only that public pin. License: PolyForm Noncommercial 1.0.0. Paths below were read for design. No source from that tree is copied into MaClaw.

This program does not replace the server planner with a container model, and it does not add a second browser. MaClaw already drives the page with CDP (`corelib/browser`) and batches same-page steps (`RunAgentFastBatch`). Later stages extend those paths.

## Phase 1, this PR

Server-side correction for ordinary text entry on the desktop bot:

- The existing verify step (`MaClawSrv/desktop_verify.go`) calls `corelib/desktop/inputcorrect`.
- The desktop control relay and the browser session only observe and perform an allow-listed action.
- The field text must match exactly after Unicode NFC and newline unification (CR and CRLF become LF). A 24-character prefix is not success when the relay can read the field.
- A wrong element, a changed frame, ref epoch, or layout scale discards the plan and reads again. There is no fixed sleep.
- On a plain-field mismatch the loop clears only that identified field, then tries another general strategy, at most three typings.
- A rich field is not cleared. Replacement keeps the image count, or the attempt fails.
- An upload or editor that is still busy is unverified, not success.
- A strategy is stored only after an exact read-back, with environment, field class, layout scale, and capabilities. A mismatch of those conditions does not reuse it. A failed reuse is demoted and stays demoted across a new process. Owner and bot do not share the file.
- The stored record has no typed text, window title, cookie, key, coordinate, or recipient. It never grants send, publish, or pay.
- A denied or clipped read-back is unverified. A secret field is not read into the result or the store.
- An ambiguous send, publish, or pay outcome is not issued again in that run.

What this stage does not do: it does not change snapshot generation inside `corelib/browser`, it does not add element ids to the desktop supervisor, and it does not execute the page script in a live browser in CI. The script is guarded by source checks. Behavior tests use an in-process relay.

## What MaClaw already has

| Area | Where | Assessment |
| --- | --- | --- |
| CDP session, focus, `Input.insertText`, JS fallback | `corelib/browser/browser.go` `insertTextLocked` | Keep. Insert that does not land is already detected. A non-empty normalized field can still be accepted there. |
| Snapshot refs, frame id, backend node id, stale ref error | `corelib/browser/types_agent.go`, `refs.go`, `actions.go` | Keep. A type or click with a dead ref asks for a fresh probe. |
| One observation after a same-page batch | `corelib/browser/fast_batch_export.go` | Keep. The batch does not exact-compare every filled field. |
| 24-rune read-back probe | `corelib/browser/verify.go` | Keep for the case where the field cannot be read in full. It is not the success condition once a full read exists. |
| Desktop steps, accessibility set-text, clipboard paste | `corelib/desktop/computeruse`, `MaClawSrv` control relay | Keep. Phase 1 calls these. It does not add a shell typist. |
| Rich-text DOM writes refused; publish needs an in-task approval | `MaClawSrv/desktop_richtext.go` | Keep. Phase 1 does not weaken it. |
| Site notes and data-gathering recipes as prompt text | `MaClawSrv/desktop_experience.go`, `desktop_recipe.go` | Keep as notes. They are not the executor's strategy selection. Recipes can store a command string; phase 1 procedures do not. |
| Run progress sentences | `MaClawSrv/desktop_run_progress.go` | Exists. The default interval is off (`MACLAW_RUN_STATUS_MINUTES`). Not a substitute for an input postcondition. |

## Compared with the pinned reference

Read, not copied:

- `src/backend/browser-host.ts`: `snapshot` bumps a revision; `act` refuses a revision that is not current and then reads a new snapshot; `fill_fields` fills against one revision; `run_script` runs several primitives and keeps a final snapshot. `siteMemories` is an optional note source for snapshots. `act` does not load a verified procedure.
- `src/backend/browser-cdp-snapshot.ts`: a ref carries revision, target, and backend node id, and a frame the parent walk cannot enter is read in its own world.
- `src/backend/browser-cdp-input.ts`: resolve the ref, focus, then insert text. Short US-layout text can be key events; multiline or editor-completed text is one insert. A later key checks that focus did not move. Clicks are pointer events. Screenshots use the page capture on the same debugger channel.

No performance numbers are claimed. None were measured here.

## Concrete gaps, later stages

Each stage needs its own review. Do not start a stage by importing an unmerged PR.

### 2. Fail closed on a stale snapshot before typing

Integration: `BrowserAgentSession.Type` / `Click` and the snapshot id already stored on `BrowserElementRef`.

Gap: a ref error is returned after a candidate miss. The reference refuses the whole action when the snapshot revision is not the tab's current revision, before dispatch.

Acceptance: an action whose snapshot id is not the latest returns a stale error and does not insert. A fresh probe then types, and the following read-back uses phase 1's exact compare.

Component: `corelib/browser` only.

### 3. Exact postcondition after FastBatch

Integration: `RunAgentFastBatch` already observes once at the end.

Gap: that observation is not an exact per-field compare, and a partial field can still look successful under the 24-rune probe.

Acceptance: a two-field batch whose second field is truncated or duplicated is not success. One settled upload (image count stable, not busy) is recorded separately from text match. No new browser process.

Component: `corelib/browser` batch finish plus `inputcorrect.CompareExact`.

### 4. Upload and rich editor before any publish

Integration: `MaClawSrv/desktop_upload.go` and the existing publish approval in `desktop_richtext.go`.

Gap: phase 1 sees `aria-busy` and image count only when a type correction reads the focused field. The upload action itself does not yet wait for a settled count, and it must not gain a site-specific selector.

Acceptance: a pending upload stays unverified with no sleep budget longer than a few re-reads. A settled count that drops existing images fails. Publish still requires the current in-task approval. An ambiguous publish timeout is not retried (phase 1's effect gate).

Component: `MaClawSrv` upload path. No Hub whitelist change unless a new remote-control op is added, which this stage should not need.

### 5. Element id from the desktop supervisor

Integration: phase 1's native relay already sends `a11y_act` `set_text` when the probe returns an id. Today's `a11y_focused_text` payload does not include that id (`desktopd/image/desktop_supervisor.py`).

Gap: without an id, native correction uses clipboard or segmented keys, not semantic set-text.

Acceptance: a probe of one focused field returns a stable id; a later `set_text` uses that id; a changed id after navigation is not reused. No coordinate is stored.

Component: desktop supervisor image and `MaClawSrv` mapping. This is a desktopd change and needs its own rollout. Do not deploy it with phase 1.

### 6. Truthful chat progress

Integration: `desktop_run_progress.go` and the activity line.

Gap: a progress sentence can describe work without separating "the field matched" from "the message was sent". Phase 1 puts that distinction on the tool result. The chat status line does not yet repeat it.

Acceptance: a status line that follows a verified input does not say the item was sent or published. A stalled run still uses the existing stall warning. No new card type.

Component: `MaClawSrv` progress text. Depends on nothing unmerged.

### 7. Experience beyond input strategies

Integration: the phase 1 procedure file, not `desktop_recipe.go`.

Gap: recipes inject a prior command into the prompt. That is not conditional execution, and a stored command can be the wrong kind of memory.

Acceptance: a later non-text strategy (for example "re-probe after navigation", not a shell script) is selected only when environment, layout scale, and capabilities match, and is demoted when the postcondition fails. The file still excludes drafts, recipients, and secrets.

Component: `inputcorrect` plus the desktop action that owns that postcondition.

## Uncertain, or no evidence in the files read

- The installed OpenBot binary's commit. Only `f694dea` was read.
- Whether another OpenBot package, outside `browser-host.ts`, `browser-cdp-input.ts`, and `browser-cdp-snapshot.ts`, persists a strategy and replays it. Those three files do not.
- Whether MaClaw's accessibility probe on a deployed image can grow an element id without a supervisor change. The current focused-text response has role and text, not an id.
- Chat decision cards, task handoff, and continuation after a card. That work is PR 60 and is not in this tree. Phase 1 does not implement it and does not claim the card resumes the task.
- Observation A/B and model vision comparisons. That work is PR 61. This program does not call a model and does not report a winner.
- User-config protection for Hub-managed bot capabilities. That work is PR 57. This program does not change config writes.
- Any speed or token advantage. Not measured.

## Boundaries

- PR 57 (`cursor/protect-bot-capabilities-3944`): config integrity. Do not merge it here and do not weaken it.
- PR 60 (`cursor/chat-interaction-cards-9f05`): chat cards and handoff. Continuation after a card is still that PR's problem. Phase 1's effect gate only stops an ambiguous send, publish, or pay from being issued again.
- PR 61 (`cursor/browser-observation-ab-afb8`): synthetic observation harness. It does not change the production browser path. Do not vendor it into `corelib/browser`.

No merge and no deploy in this stage. A later deploy of phase 1 is a `MaClawSrv` binary only. It does not require a Hub or desktopd restart. Phase 5 does.
