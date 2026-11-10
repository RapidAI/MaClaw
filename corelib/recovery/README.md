# recovery — runtime self-correction (no self-modifying code)

A run corrects its own interaction failures while it runs: measured facts
decide, the run's own model diagnoses, a bounded test decides the diagnosis,
and only a verified outcome is remembered. No program, profile or prompt file
is changed by it.

## Data flow (MaClawSrv desktop tool)

```
model tool call ─► InvokeTool ─► invokeDesktopTool guards (budget, repeat, publish, failclass, no-progress, drift)
                                   │
                                   ├─ action=recover / goal_check ─► desktop_recovery.go (early, no side effect)
                                   │
                                   ├─ desktopRecoveryBefore: goal key, strategy params (whitelist),
                                   │    supported-hypothesis / verified-recipe params, Engine.Gate
                                   │    (refuses: diagnosis pending, trial that would submit, failed
                                   │    strategy, budget out, unconfirmed submit)
                                   ├─ operation (publish pipeline / bash / …)
                                   │    publish: PublishReport.Facts (pictures_wanted, pictures_kept,
                                   │    regression, lost_item, trial, published) → desktopRecoveryNotePublish
                                   └─ desktopRecoveryAfter: Engine.Observe → verdict + evidence text
                                        appended to the tool result the model reads next
run end ─► desktopEnsureReceipt ─► desktopRecoveryOutcome → metadata task_outcome + honest first line
```

* **Goals.** `publish|<site>|<title hash>` for publish calls;
  `check:<name>` for `goal_check` (criteria: file_exists, file_min_lines,
  file_min_bytes, file_contains, json_min_items; pinned on first use, never
  relaxed). Mutating calls between two checks are that goal's attempt and are
  refused while it needs a diagnosis.
* **Detection.** Same failure fingerprint twice (numbers/urls stripped);
  regression (tool fact `regression=1`, or a measured fact below the previous
  comparable attempt); no measurable improvement in N attempts.
* **Diagnosis.** `action=recover` with hypothesis, uncertainty, strategy,
  params (publish: insert_prep_keys, insert_settle_ms, body/title/upload
  selector, upload_trigger — validated, caret keys only, never Ctrl/Alt+Enter),
  test, expect (`fact op number`). Refused when the strategy or params already
  failed or were already tested.
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

## Defaults (env `MACLAW_RECOVERY_*`)

| setting | default | env |
|---|---|---|
| same failure before diagnosis | 2 | SAME_FAILURES (1-10) |
| attempts without progress | 3 | NO_PROGRESS (2-20) |
| hypotheses per goal | 3 | DIAGNOSES (1-10) |
| attempts per goal (all strategies, runs, resumes) | 10 | ATTEMPTS (2-100) |
| attempt wall time per goal | 25 min | WALL_MIN (1-240) |
| goal ledger kept for resume | 12 h | TTL_HOURS (1-336) |
| recipe offered without re-verification | 30 days | — |

Store: `<MaclawDataDir>/desktop-recovery/<sha256(scope)[:12]>.json`, one file
per tenant+user, 0600, at most 64 goals / 32 recipes.

## Guarantees

* Facts come from the tool's own structured measurements, never from page
  text; a model's words never make a goal complete.
* A submit whose result is unproven (`submitted_unconfirmed`) stops the goal:
  no retry, no recover.
* Budgets are per goal and persisted: switching method, params, file names or
  resuming in a new run spends the same budget.
* Approvals, login/captcha hand-offs and publish permission are the existing
  guards; recovery only adds refusals.
* blocked / partial / not_submitted / unconfirmed / awaiting_user are written
  to the reply's `task_outcome` and stated in its first line; run status stays
  as before for compatibility.

## Limits

* Facts exist for the publish pipeline and file criteria only; other tools
  are covered by the generic repeat/no-progress guards, not by recovery.
* Page/DOM criteria for goal_check are not implemented.
* Goal key uses the title (publish) or the check name; a retitled article is
  a new goal.
* A publish trial leaves the trial text in the editor (sites that autosave
  keep it as the draft until the real attempt refills it).
* Observe is not atomic across concurrent calls of one goal (one desktop
  step at a time per person in practice).

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
