# Clean working set and on-demand retrieval

Status: **Phase 4 shipped**. Attach policy stays at rev 3. Algorithm is rev 3.3. Cleanup through rev 3.12. Ambient memory is `memory.recall.agent` (ReadOnly).
English file is the authority. Digest: [clean-working-set-on-demand-retrieval-zh.md](clean-working-set-on-demand-retrieval-zh.md).
Supersedes first-turn silent inject in `docs/knowledge-auto-recall-design.md`.

## 1. Decision

Warehouse text is retrieved with tools. It is not first-turn prompt filler.

1. New task working set is empty (catalog + pull hint only).
2. The host does not BM25-inject at iteration 0.
3. The catalog is a pointer, not a fact.
4. Ingest / knowledge admin are never ambient.

Shipped: Phases 1–3, unmanaged Need path, VE/`/btw` no dump, ReadOnly `memory.recall.agent`. Name-pin deleted.

## 2. Invariants

1. Catalog-only hosts write no warehouse bodies into the system prompt.
2. Managed turns: planner selects; hosts append **needs**, never tool names.
3. Optional need (`Required: false`): missing provider, leftover budget, or `policy_denied` -> `Omitted`. Authoring faults (`unknown_capability`, `invalid_capability_need`) may still `Unmet`. Optional must not turn a required wave into `budget_exceeded` / `planning_budget_exceeded`.
4. After `applyPlanningBudget`, `Selections[0]` is the first **required** selection when any required selection was kept. It is not a specific capability (coding has four required families). Unlimited budget still partitions (required prefix, optional after). Today's early-return when `MaxSelections<=0 && MaxSchemaTokens<=0` must not skip that reorder.
5. No-ambient primaries get no extra knowledge/memory adapters from Append.
6. Append adds `knowledge.read.local` and `memory.recall.agent`. Recall is ReadOnly; light **close** keeps it. `memory.manage.agent` stays on explicit `memory_manage` only. Do not decide light at resolve.
7. Group deny knowledge omits that **optional** need. No `web_search` substitute. GUI HostReject is `len(plan.Unmet) > 0` only. A **required** `knowledge_read` that is group-denied stays `Unmet`. Do not add extra group-deny branches.
8. Extractor classify-fail stays `OpNoop`.

## 3. What actually breaks

`applyPlanningBudget` keeps **dependency waves**. Optional knowledge/memory have no `Requires` edge, so they sit in the **same first wave** as `document_open` / `file_read` / `information.search.web`.

`MaxSelections=1` then sees a first wave of size 3 and reports `planning_budget_exceeded` on **the whole wave**, including the required intent. That is why all-label Append failed, not because `need:~` sorts first (`~` > letters).

`PlannedSelection` has no `Required` field. Look up `NeedID` in `req.Needs`. Missing `NeedID` => required (fail closed).

`Requires` holds `selection.ID` (`selection:` + need.ID) and sometimes `confirmation:` + need.ID. Do not look up NeedID in `Requires`. A naive "require not in kept" that treats `confirmation:*` as a missing parent would omit every confirmed optional; ignore `confirmation:` prefixes when filling optional.

### 3.1 Phase 1 algorithm (`applyPlanningBudget`)

Change the signature to `applyPlanningBudget(plan, budget, needs)`. Do not add `Required` on `PlannedSelection`.

```
needRequired[need.ID] = need.Required    // missing NeedID => required
required, optional := partition plan.Selections
// keep relative order inside each side (do not sort the required list here;
// selectionDependencyWaves already sorts each wave by selection.ID)

if budget unlimited (MaxSelections<=0 && MaxSchemaTokens<=0):
    plan.Selections = required + optional
    return

if len(required) == 0:
    kept = nil                       // not a first-wave exceed
else:
    requiredWaves := selectionDependencyWaves(required)
    kept, droppedRequired := closed-wave prefix (same firstWaveExceeds rules as today)
    droppedRequired -> Unmet (budget_exceeded / planning_budget_exceeded)
    if first required wave exceeds:
        plan.Selections = nil
        do not fill optional         // HostReject already; warehouse-only plan is wrong
        return

keptIDs := { sel.ID for sel in kept }
for wave in selectionDependencyWaves(optional):   // parents first; each wave sorted by selection.ID
    for each optional sel in wave:                // one-at-a-time, not atomic wave-of-2
        if any require starting with "selection:" is not in keptIDs:
            Omitted optional_budget_omitted
            continue
        // confirmation:* does not block
        if leftover MaxSelections / MaxSchemaTokens fits this one selection:
            kept.append(sel)
            keptIDs.add(sel.ID)
        else:
            Omitted optional_budget_omitted

plan.Selections = kept
```

Required stays wave-closed. Optional is walked in dependency order (so optional-to-optional `selection:` edges see the parent first) and committed **one selection at a time**. Knowledge and memory do not form a closed pair; leftover=1 keeps knowledge and omits memory.

Empty `required` is not `planning_budget_exceeded`. That case is not a managed ambient turn; do not invent a warehouse-only HostReject.

`routingeval` budget fixtures have no optional needs. Do not rewrite them unless a fixture starts failing.

`business_data` optional read + required MIS on a tight budget: keep MIS, omit the read half. Intended.

Default GUI budget is unlimited. Phase 1 is invisible on desktop except the required-first reorder (already true today because Append sorts `need:~` last). Coding-family Append still shows 6 adapters.

## 4. When to Append

```
// corelib/intent — Phase 2 adds the helper; GUI calls it. Core/Hub do not Append yet.
func WantsAmbientRetrieval(primary IntentLabel) bool
```

Use `result.Primary` only. `Labels()` includes secondary; weather+PDF is `live_data` + `document_generate`.

Call site stays `Managed && WantsAmbientRetrieval(primary)` then `AppendAmbientRetrievalNeeds`. Unmapped primaries (`workflow_task` today) are not Managed, so a `true` from the helper still does not Append.

Do not flip `AmbientRetrieval`.

### 4.1 Every `AllLabels()` primary

| Primary | Append | Why |
| --- | --- | --- |
| `non_coding` `continuation` `ambiguous` `unknown` | no | `IsNonCapabilityLabel`; unmanaged pin |
| `audio_record` `audio_transcribe` `audio_synthesize` `audio_deliver` | no | closed-effect |
| `screenshot` `computer_use` `current_time` | no | closed-effect |
| `attachment_delivery` `document_delivery` `document_open` `document_generate` | no | closed-effect |
| `app_launch` `file_download` | no | closed-effect |
| `schedule_manage` `schedule_dispatch` | no | closed-effect |
| `config_manage` `session_manage` `template_manage` | no | closed-effect |
| `knowledge_write` `knowledge_admin` | no | writes are never ambient |
| `search` `live_data` `web_fetch` | no | lookup already has a retrieval tool |
| `coding` `bug_fix` `maintenance` | yes | current GUI gate; keep |
| `document_read` `file_read` `audit_read` `git_inspect` | yes | read task; warehouse on demand |
| `file_write` `shell_command` `git_mutate` `office` `business_data` | yes | task; warehouse on demand |
| `knowledge_read` | yes | adds memory only (knowledge already required) |
| `memory_manage` | yes | adds knowledge only (memory already required) |
| `ssh` `browser` `task_track` `goal_manage` `delegate_task` | yes | default-yes for new managed labels |
| `workflow_task` | yes in helper | not Managed today; no Append until mapped |

Pin the helper against `AllLabels()`. A new label that is neither non-capability, closed-effect, nor lookup defaults to yes.

Append: keep both optional capabilities; skip duplicates and missing descriptors. Shipped sort by NeedID stays (`need:~ambient:*` after letters). Do not change it in Phase 1/2. `coding_knowledge_search` stays out.

## 5. Evidence order

- Lookup primary: use the required web/URL tool. Do not say "KB first".
- Task primary with warehouse tools: current-turn artifacts/files, then warehouse, then web.
- No-ambient primary: warehouse tools are absent.

`PromptKnowledgeBaseRules` is shared with TUI, which still injects. Do not change that shared block in the GUI-only phase.

## 6. Phases

### Phase 1 — Required-only waves (planner)

No attach-policy change.

- Implement §3.1. Pass `req.Needs`.
- P1-1: required + 2 optional, `MaxSelections=1` keeps required; both optional `Omitted`, not `Unmet`.
- P1-2: unlimited budget still has required at `[0]`, optional after (partition, not early-return).
- P1-3: schema tokens fit required only -> optional `Omitted`.
- P1-4: missing `NeedID` treated as required.
- P1-5: leftover=1 after 1 required keeps knowledge, omits memory (one-at-a-time, not optional wave-of-2).
- P1-6: required first wave exceeds -> `planning_budget_exceeded`, `Selections` empty, no optional fill.
- P1-7: no required selections + tight budget still fills optional one-at-a-time (not first-wave exceed).
- Keep `semantic_planner_optional_test.go`.
- `business_data` tight budget keeps MIS, omits optional read.

Accept: `go test ./corelib/tool/ ./corelib/agentservice/` and GUI `TestIMSemantic|TestSemantic` green without widening the coding gate.

### Phase 2 — GUI primary policy only

- Add `intent.WantsAmbientRetrieval`. Replace GUI `classificationWantsAmbientRetrieval`.
- Keep Core/Hub `AmbientRetrieval: false`. No Core/Hub Append.
- Keep `ensureDefaultRetrievalToolsRouted`.
- Leave `PromptKnowledgeBaseRules` alone.
- Tests: table over `AllLabels()`; "has capability" / `dropAmbientRetrievalNeeds` over exact `len(defs)`. Coding stays 6 adapters. `knowledge_read` `Selections[0]` stays the required knowledge adapter.

Accept: `document_read` / `file_read` / `coding` have `knowledge_search`. `audio_deliver`, `document_open`, `current_time`, bare `document_generate`, `live_data` (+ generate) do not. Weather+PDF default budget has no knowledge selection. Light close / group ACL stay green. Group deny on coding / `document_read` omits, does not HostReject.

### Phase 3 — TUI / Core dumps + shared prompt + Core/Hub Append

- Remove TUI/Core `appendKnowledgeAutoRecall`. Then change `PromptKnowledgeBaseRules` to §5.
- Core/Hub: same helper + post-resolve Append. Strip `need:~ambient:*` / `ambient:retrieval` in `len(Needs)==1` tests.
- Enterprise stays inside `knowledge.read.local`.

### Phase 4 — On demand

Delete the name-pin after unmanaged chat has a retrieval Need path (`AppendAmbientRetrievalNeeds` from empty → host tool names). VE / `/btw` stop warehouse dumps. ReadOnly `memory.recall.agent` split; Append uses recall, not manage.

## 7. Rejected

| Option | Why |
| --- | --- |
| Silent BM25 inject | Memory poison |
| `AmbientRetrieval: true` | Unfiltered; Core tests explode |
| Any-label denylist | `live_data`+`document_generate` |
| Warehouse-before-web on lookup | 南京天气 hits the KB |
| Optional stays in the required wave | `MaxSelections=1` kills the intent |
| Optional fill as a closed wave-of-2 | leftover=1 drops knowledge and memory |
| Skip partition when budget is unlimited | invariant 4 / P1-2 becomes sort-dependent |
| Fill optional after required first-wave fail | warehouse-only `Selections` |
| Treat `confirmation:*` as a missing parent | omits confirmed optional |
| GUI policy + Core Append in one phase | `len(Needs)==1` tests |
| Shrink Append to knowledge-only | Coding 6-adapter surface |
| Delete name-pin with Phase 1/2 | Unmanaged chat goes blind |
| Allowlist of "useful" labels | New managed labels ship without retrieval |
| Reopen attach policy | Implement §3.1 then Phase 2 helper |

## 8. Tests

```
go test ./corelib/memory/ ./corelib/agentservice/ ./corelib/tool/ ./corelib/intent/
go test -run "TestIMSemantic|TestSemantic|TestSystemPrompt_|TestIsolatedAssistant|TestLansengerGroupMemory" ./gui/
```

| ID | Phase | Assertion |
| --- | --- | --- |
| P1-1 | 1 | `MaxSelections=1` keeps required; optional `Omitted` |
| P1-2 | 1 | unlimited: required at `[0]`, optional after |
| P1-3 | 1 | tight schema budget omits optional |
| P1-4 | 1 | missing `NeedID` treated as required |
| P1-5 | 1 | leftover=1 keeps knowledge, omits memory |
| P1-6 | 1 | required first wave exceeds: empty `Selections`, no optional |
| P1-7 | 1 | empty required is not first-wave exceed |
| P2-1 | 2 | `AllLabels()` helper table |
| P2-2 | 2 | `document_read` yes; `document_open` / `live_data` / `audio_deliver` no |
| P2-3 | 2 | light kept knowledge; Phase 4 keeps `memory.recall.agent` |
| P2-4 | 2 | weather+PDF default budget has no knowledge selection |
| P2-5 | 2 | group deny knowledge on coding/`document_read` omits, does not HostReject |
| P3-1 | 3 | TUI/Core first prompt has no auto-recall header |
| P3-2 | 3 | Core file_read still plans; trailing ambient needs ignored or expected |
| P4-1 | 4 | Unmanaged Need path adds knowledge_search + memory; not coding_knowledge_search |
| P4-2 | 4 | VE / `/btw` first prompt has no auto-recall header |
| P4-3 | 4 | Ambient selects `memory.recall.agent`; light keeps it |
| P4-4 | 4 | CatalogOnly + non-empty query returns no warehouse bodies; GUI has no in-flight recall dump |
| P4-5 | 4 | Project Tab resume copy does not say artifacts are already loaded; tab-seed has no warehouse body |
| P4-6 | 4 | LoadProjectContext RecentProgress has no warehouse body; CreateProjectTabSession does not call ProjectContextForHost |

Manual after Phase 2 (desktop IM):

1. Fact only in KB, not a web-lookup ask -> `knowledge_search` + cite.
2. 现在几点 -> clock only.
3. OS-open a document -> no knowledge tool.
4. 南京天气 / 天气+PDF -> web path, no KB tool.
5. Group denies knowledge on a coding / document-read ask -> no `knowledge_search`, no inject, turn still answers.
6. Coding this repo -> read/inspect first.

## 9. Residual risks (do not reopen policy for these)

- Phase 2 may add warehouse tools on `ssh` / `browser` / `office` / `file_read`. Prefer capability assertions over exact grant counts.
- Light close keeps `memory.recall.agent`; it still drops `memory.manage.agent`.
- Group policy denies `memory.manage.agent`, not `memory.recall.agent`.
- `workflow_task` helper is yes; it only Appends after that label is mapped.

## 10. Changelog

- Rev 1: denylist, keep name-pin, knowledge-only ambient.
- Rev 2: primary-only policy; lookup is no-ambient; keep both Append capabilities; NeedID lookup.
- Rev 3: optional must leave the required **wave**; optional budget drop is `Omitted`; Phase 2 is GUI-only.
- Rev 3.1: HostReject only sees `Unmet`; Append NeedID sort stays.
- Rev 3.2: always partition (even unlimited); no optional fill after required first-wave fail; optional filled one-at-a-time; `Requires` uses `selection.ID`, ignore `confirmation:`; complete `AllLabels()` table; helper lives in `corelib/intent`.
- Rev 3.3 (final): empty required is not first-wave exceed; optional walked in dependency waves then committed one-at-a-time. Planning closed.
- Rev 3.4: Phase 4 — `memory.recall.agent` ReadOnly split; unmanaged Need path; VE/`/btw` dumps removed.
- Rev 3.5: Delete leftover host dump wrappers (`appendKnowledgeAutoRecall`, `AppendEnterpriseKnowledgeAutoRecall`) and the IM source-count cache that only served prompt inject. Keep the reusable store for tool retrieval. Library `enterpriseknowledge.AppendAutoRecall*` stays unwired.
- Rev 3.6: Delete library `AppendAutoRecall*`. IM catalog path skips BM25/embedding. Unused VE/`/btw` prompt profiles are CatalogOnly.
- Rev 3.7: Remove `SystemPromptDeps.KnowledgeAutoRecall` hook. Group web-fallback evidence is tool results only. Hide unused chat auto-recall settings.
- Rev 3.8: Delete unused inject-threshold helpers (`MaxInject`, `ExpandKnowledgeAutoRecallQuery`). Keep header sentinels only. Core catalog path no longer compact-queries.
- Rev 3.9: Delete unused GUI async recall in-flight machinery (`proactiveRecallInFlight`, budgeted `ProactiveContextForPrompt(msg)`). Catalog-only hosts keep the empty query. DeleteMemories still invalidates frozen snapshots.
- Rev 3.10: Project Tab resume card no longer claims warehouse artifacts are already in the model. Unused tab-seed message is titles/paths only (no Content/Preview).
- Rev 3.11: LoadProjectContext RecentProgress is titles/paths only. CreateProjectTabSession no longer recalls warehouse text for an unused return string.
- Rev 3.12: Full-prompt rules no longer say warehouse-first. Catalog footer and memory guide only pull when those tools are in the current list (lookup turns stay on web/URL).
