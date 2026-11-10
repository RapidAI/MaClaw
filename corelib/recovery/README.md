# recovery — runtime self-correction (no self-modifying code)

A run corrects its own interaction failures while it runs: measured facts
decide, the run's own model diagnoses, a bounded test decides the diagnosis,
and only a verified outcome is remembered. No program, profile or prompt file
is changed by it.

## Data flow (MaClawSrv desktop tool)

```
model tool call ─► InvokeTool ─► invokeDesktopTool guards (budget, repeat, publish, failclass, no-progress, drift)
                                   │
                                   ├─ action=recover / goal_check ─► desktop_recovery.go (no side effect)
                                   │
                                   ├─ desktopRecoveryBefore: task lineage, goal key, strategy params
                                   │    (whitelist), supported-hypothesis / verified-recipe params,
                                   │    Engine.Gate precheck; mutating steps: gated by every unfinished
                                   │    check goal of the task, ctx deadline = attempt time left
                                   ├─ operation
                                   │    publish: desktopPublishExecute
                                   │      Engine.Acquire (atomic admit + reserve + lease; refused → nothing filled)
                                   │      heartbeat (Beat every LeaseTTL/3) while the pipeline really runs
                                   │      pipeline Deadline cut to the goal's attempt time left
                                   │      BeforeSubmit → Engine.SideEffect (handshake; refused → not submitted)
                                   │      pipeline end (even after the tool call timed out) → Engine.Finish
                                   │        with PublishReport.Facts (pictures_wanted, pictures_kept,
                                   │        regression, lost_item, trial, published)
                                   └─ desktopRecoveryAfter: verdict evidence appended to the tool result
                                        (or "still running, holds the goal" after a step timeout)
run completion (agentservice) ─► AssistantMessageMetadataHook (srv) ─► desktopTaskOutcomeMetadata
     ─► run.Metadata.task_outcome, run.WaitingForUser, terminal event payloads, notice line before the reply
```

* **Task lineage.** `Engine.Lineage(scope, conversation, person's message)`:
  one id per conversation (session id; a run without one is its own) and
  instruction. Continuations ("继续", "再试一次", short replies), resumes and
  repeats of the same instruction keep it; a new instruction (≥12 runes, not a
  continuation, different text) starts the next generation. Computed from the
  person's message only — nothing the model passes chooses it.
* **Goals.** `publish|<lineage>|<site>` (titles and files are not in the
  key: a renamed article is the same goal, same budget, same lockout; only
  after it was verified does a different title start `…|2`, the next article
  of the same instruction; the same title again is refused as a duplicate).
  The first attempt pins the requirement (pictures, body length); fewer
  pictures or a body under 60% of it is refused.
  `check|<lineage>|<name>` for `goal_check` (criteria: file_exists,
  file_min_lines, file_min_bytes, file_contains, json_min_items; pinned on
  first use, never relaxed). Every mutating step is part of the attempt of
  every unfinished check goal of the task (so a newly named goal does not
  escape one waiting for a diagnosis); a step that can change a file reopens
  a verified check goal, which then has to be measured again.
* **Detection.** Same failure fingerprint twice (numbers/urls stripped);
  regression (tool fact `regression=1`, or a measured fact below the previous
  comparable attempt); no measurable improvement in N attempts.
* **Diagnosis.** `action=recover` (goal must belong to the current task)
  with hypothesis, uncertainty, strategy, params (publish: insert_prep_keys,
  insert_settle_ms, body/title/upload selector, upload_trigger — validated,
  caret keys only, never Ctrl/Alt+Enter), test, expect (`fact op number`).
  Refused when the strategy or params already failed or were already tested.
* **Test.** The hypothesis' next attempt is its test; a publish test must be
  `trial_pictures=1..3` (fills and inserts, never submits). Expected facts
  decide supported/refuted; a supported strategy that then fails for real is
  refuted too.
* **Recipes.** Stored only on a verified, non-trial outcome reached with
  params: surface (`site:<id>|publish`), method, params, provenance (time,
  run, facts). Never the hypothesis text, content, credentials or commands.
  Offered to later runs of the same tenant+user as a candidate that the real
  result re-verifies; one failure retires it; not offered after 30 days
  without re-verification.

## Concurrency and crashes

* Every read-modify-write of a person's ledger is one transaction: an
  in-process mutex plus an exclusive OS file lock (`flock` / `LockFileEx`) on
  `<file>.lock`, re-read under the lock, written to a temp file, fsync'd,
  renamed, directory fsync'd. A corrupt or unreadable ledger fails closed
  (attempts refused, the file is never overwritten).
* A publish attempt holds a lease on its goal from admission to the
  pipeline's real end. While held (heartbeat) no other attempt — another run,
  another engine, another process on the same data dir — starts on the goal,
  however long it takes.
* Holder gone (crash, kill) **before** the submit handshake: after LeaseTTL
  the attempt is recorded as interrupted (it spent its reserved count) and
  the goal can be attempted again. **After** the handshake: the goal is
  stopped (`unconfirmed`), never retried automatically.
* A late result from an attempt that lost its lease is not recorded
  (`stale`); a lost lease refuses the submit handshake.
* Measured (goal_check) goals have no lease; each mutating step under them
  is a durable step reservation (`BeginStep` / `EndStep`): admitted and
  charged to `spent_ms` in the ledger **before** the step runs (min of the
  step's own hard limit and the goal's time left; that is also the step's
  deadline), settled to the real duration when the step really returns —
  also after the tool call stopped waiting (timeout, cancel), an overrun
  past the reservation is charged too. A process that dies mid-step leaves
  the full reservation charged (pruned from `steps` LeaseTTL after its
  deadline; a goal with steps in flight does not expire). A restart never
  gives time back. The first step after a measurement opens the attempt and
  counts it (`open`); the next goal_check closes it without counting again.
  Steps of several runs/processes on one goal are each charged once (same
  transaction). Out of time while other steps hold reservations refuses the
  step without exhausting the goal.

## Defaults (env `MACLAW_RECOVERY_*`)

| setting | default | env |
|---|---|---|
| same failure before diagnosis | 2 | SAME_FAILURES (1-10) |
| attempts without progress | 3 | NO_PROGRESS (2-20) |
| hypotheses per goal | 3 | DIAGNOSES (1-10) |
| attempts per goal (all strategies, titles, runs, resumes) | 10 | ATTEMPTS (2-100) |
| attempt wall time per goal (enforced as the tool deadline) | 25 min | WALL_MIN (1-240) |
| elapsed time from the first attempt (model time included) | 90 min | SPAN_MIN (5-1440) |
| dead-holder detection | 2 min | LEASE_SEC (20-900) |
| goal ledger kept after its last change (resume) | 12 h | TTL_HOURS (1-336) |
| recipe offered without re-verification | 30 days | — |

Store: `<MaclawDataDir>/desktop-recovery/<sha256(scope)[:12]>.json`, one file
per tenant+user, 0600, at most 96 goals (never one with a lease) / 32
recipes / 64 conversations.

## Guarantees

* Facts come from the tool's own structured measurements, never from page
  text; a model's words never make a goal complete.
* At most one attempt per goal is in flight across runs, engines and
  processes sharing the data dir; a consequential submit happens only while
  its attempt holds the goal.
* A submit whose result is unproven stops the goal: no retry, no recover.
* Budgets are per task goal and persisted: switching method, params, title,
  file names or resuming in a new run spends the same budget; a new
  instruction is a new task with its own budget; tenants and users never
  share goals or recipes.
* Approvals, login/captcha hand-offs and publish permission are the existing
  guards; recovery only adds refusals.
* Task outcome (agentservice `task_outcome`: done, partial, blocked,
  not_submitted, unconfirmed, awaiting_user) is set at run completion from
  the ledger, after the model's last reply: it is persisted on the run and
  the assistant message, sent in the ask_user / assistant.message /
  run.completed payloads, a not-done task is `waiting_for_user`, and its
  notice line goes before the reply. The run status stays `succeeded` for
  compatibility; cancellation and failure paths are unchanged. A
  question-only chat (no desktop step) gets no outcome.

## Limits

* Facts exist for the publish pipeline and file criteria only; other tools
  are covered by the generic repeat/no-progress guards, not by recovery.
  Page/DOM criteria for goal_check are not implemented.
* Cross-process exclusion assumes the processes share a local filesystem
  with working advisory locks (not NFS without lock support).
* A goal expires 12 h after its last change (a stopped or leased one does
  not expire); an instruction resumed after that starts a fresh budget.
* Within one instruction, all articles to one site share one goal until the
  current one is verified (conservative: a failing article blocks the next).
* A step's deadline stops MaClawSrv's wait for it and cancels its context;
  a container `bash` step is additionally wrapped in coreutils
  `timeout -k 5 <reserved s>` (when present in the container). Other
  remote operations (app_run, task_run, skill_run, computer, file_write …)
  stop when their request is cancelled only as far as desktopd/Hub honour
  that; until they return, their reservation stays charged.
* Tools other than desktop mutating steps and publish (e.g. non-desktop
  tools, sub-agents) are not instrumented.
* A publish trial leaves the trial text in the editor (sites that autosave
  keep it as the draft until the real attempt refills it).

## Reproduce offline

```
cd corelib && go test ./recovery
cd corelib && go test -run 'TestCaretEditor|TestParseKeyChord' ./browser   # needs google-chrome
cd MaClawSrv && CGO_ENABLED=0 go test -run TestRecovery -v ./           # needs google-chrome, python3
```

`corelib/browser/testdata/publish/csdn_caret.html` is an editor that keeps its
own caret (only trusted keys/mouse/input move it; focus restores it) and
toggles a picture link off when a picture is inserted on a selection already
inside `![..](..)`: the default flow stops at the first lost picture
(`regression=1 lost_item=1`); the model's first hypothesis (wait longer) is
refuted by its trial, the second (native caret keys) is supported, the real
publish keeps all pictures, and the next run starts from the verified recipe.
