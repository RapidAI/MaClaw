# Cost route — rule tiers (Phase 1 shadow + Phase 2 apply)

OpenSquilla-inspired **agentic cost routing**: recommend C0–C3 per turn, optionally
switch models.

## Control

| Env `MACLAW_COST_ROUTE` | Effect |
| --- | --- |
| unset / `off` | No Turn-chip tier tag (tier still computed on route decision) |
| `shadow` / `observe` | Log + Turn chip `tier=cN(shadow)` + Doctor; **model unchanged** |
| `on` / `apply` | **Phase 2:** select model by tier (see below); chip shows `tier=cN` |

## Tier map (rules)

| ClassifyTurn task / hints | Tier |
| --- | --- |
| `fast`, `intent` | **c0** |
| `summary` | **c1** |
| `default` | **c2** |
| `reasoning`, `vision` | **c3** |
| `ToolHeavy` | at least **c2** |
| `HasAttachments` / `ForceReasoning` | **c3** |

## Phase 2 apply map (`on`) — model

| Tier | Model selection order |
| --- | --- |
| **c0** | `ModelRoutes[fast\|intent]` → **aux** → primary |
| **c1** | `ModelRoutes[summary\|fast\|intent]` → **aux** → primary |
| **c2** | `ModelRoutes[default]` → primary |
| **c3** | `ModelRoutes[reasoning\|vision\|default]` → primary |

Requires aux and/or `model_routes` configured for cheap tiers to save money.
Without them, c0/c1 stay on primary (still `applied=true` — policy ran).

## Phase 3 — thinking bound to tier (`on` applies)

| Tier | Thinking policy | Request effect |
| --- | --- | --- |
| **c0 / c1** | **off** | `thinking.type=disabled`, `reasoning_effort=minimal` |
| **c2** | **low** | thinking enabled + `reasoning_effort=low` (budget 1024 w/ tools) |
| **c3** | **high** | thinking enabled + `reasoning_effort=high` |

Shadow mode records `think=off(shadow)` on Turn chip without changing requests.

## Surfaces

- Log: `[cost-route] mode=… tier=… task=… model=A→B applied=…`
- Chat Turn chip: `… · tier=c0(shadow)` or `… · tier=c0` when applied
- System Doctor last route: `… · tier=c0(shadow)`
- JSON: `cost_tier`, `cost_route_mode`, `cost_route_applied`

## Code

- `corelib/llm/cost_route.go` — recommend + `ApplyCostTierConfig`
- `gui/openhuman_wiring.go` — `applyTurnModelRoute`
- `corelib/agent.FormatTurnMeta` — chip tags

## Ops tips

```powershell
# Observe only
$env:MACLAW_COST_ROUTE = "shadow"

# Apply: ensure aux LLM and/or model_routes for fast/summary/reasoning
$env:MACLAW_COST_ROUTE = "on"
```

Mid-loop tool appearance can still **escalate** to reasoning via existing
`escalateRunStateToReasoning` (independent of cost-route).

## Phase 4 — tool structured compression

Large tool results use `toolresult.StructuredPreview`:

| Tool / shape | Strategy |
| --- | --- |
| bash / terminal | head 25% + tail 75% |
| web_fetch | keep integrity footer |
| JSON | top-level keys + value sketches |
| unified diff | headers/hunks + sample lines |
| default | head/tail DefaultPreview |

Full raw body spills to disk (`tool_result_handle`); process stats:

- `tool_compress_saved_bytes` / `spills` on System Doctor shared-loop status  
- `toolresult.FormatCompressionLine()` for CLI/operator one-liners

## Phase 5 — denial auto-pause + cost CLI

### Denial ledger

Consecutive **security policy denials** auto-pause further tools:

| Env | Meaning |
| --- | --- |
| `MACLAW_DENIAL_PAUSE=off` | disable |
| `MACLAW_DENIAL_PAUSE_THRESHOLD=N` | default **5** |

Allow paths reset the streak. When paused, `SecurityFirewall.Check` blocks tools with a clear message.

```bash
maclaw-cli denial-pause status
maclaw-cli denial-pause clear   # operator resume
# GUI: ClearDenialPause Wails binding
```

### Cost CLI

```bash
maclaw-cli cost
```

Returns adaptive-prompt durable stats, this-process tool-compress counters, denial ledger, configured `daily_llm_budget_usd`, and **fleet daily LLM cost**.

| Field | Source |
| --- | --- |
| `llmCostDaily` | Sum of host-pid slots in `~/.maclaw/stats/llm_cost_daily.json` |
| `dailyBudgetUSD` | Config `daily_llm_budget_usd` |
| session `$` | GUI/TUI process-local CostTracker (System Doctor `cost_*`) |

### Durable daily cost (post phase 5)

Each GUI/TUI process writes its own **host-pid** slot on every `CostTracker.Record`:

```json
{
  "date": "2026-07-12",
  "instances": {
    "hostname-12345": {
      "cost_usd": 0.15,
      "calls": 12,
      "input_tokens": 100000,
      "output_tokens": 20000,
      "updated_at": "2026-07-12T12:00:00Z"
    }
  },
  "updated_at": "2026-07-12T12:00:00Z"
}
```

- **CLI** `maclaw-cli cost` → `llmCostDaily` + summary line `llm-cost today=$… calls=… instances=…`
- **System Doctor** → process `cost_daily_*` plus fleet `cost_fleet_*` when the file has data
- Date rollover: only **today** is kept; a new calendar day starts a fresh fleet file
- Multi-process: each pid overwrites only its own instance key; fleet view sums all slots for today
- Same-process concurrent `Record` is serialized; disk writes are **debounced (~800ms)** — call `FlushDailyCostPersist` / `cost export` for immediate durability
- `LoadCostDailyFleet` **overlays** this process's pending debounce snapshot (CLI/doctor see live totals without waiting on the timer)
- Failed disk writes re-arm dirty + debounce (retry), so a failed flush is not silently dropped

## Phase 6 — daily budget hard-stop (fleet-aware)

Config: `daily_llm_budget_usd` (0 = unlimited).

| Stage | Behavior |
| --- | --- |
| **≥ 80%** | Log `[cost] budget warning`; CLI `budgetStatus=warn` |
| **≥ 100%** | **Hard-stop** agent turns (`error=daily_llm_budget_exceeded`, `response_source=budget_gate`); CLI `budgetStatus=exceeded` |

Stop points:

1. **Entry** — `runAgentLoop` before shared/legacy path  
2. **Mid-loop (legacy)** — each iteration `prepareAgentLoopRound` when `iteration > 0`  
3. **Mid-loop (shared)** — `agent.EarlyStopper` before each LLM round; usage charged via `LLMUsageRecorder` after every response  

Effective spend for the gate is `max(process today, fleet today)` so:

- multi-process GUI+TUI spend is counted
- restarting the GUI does **not** reset the daily budget
- shared agent loop now updates CostTracker **per LLM round** (not only at loop end)

```bash
# Inspect
maclaw-cli cost   # budgetStatus: ok | warn | exceeded

# Raise budget in config (or set daily_llm_budget_usd: 0 to disable)
```

## Phase 7 — by_model buckets + TUI cost/budget

### By-model daily buckets

Each process slot in `llm_cost_daily.json` includes `by_model`:

```json
{
  "date": "2026-07-12",
  "instances": {
    "host-pid": {
      "cost_usd": 0.42,
      "calls": 8,
      "by_model": {
        "gpt-4o-mini": { "cost_usd": 0.12, "calls": 5, "input_tokens": 100000, "output_tokens": 20000 },
        "deepseek-chat": { "cost_usd": 0.30, "calls": 3 }
      }
    }
  }
}
```

`maclaw-cli cost` → `llmCostDaily.by_model` is the **fleet sum** across host-pid slots; summary includes `by_model top: …`.

### TUI wiring

TUI owns a process `CostTracker` (same durable file as GUI):

- `OnLLMUsage` after each shared-loop LLM round  
- `EarlyStop` when `daily_llm_budget_usd` exceeded (fleet-aware)  
- Chat surfaces budget gate text instead of a silent cancel  

## Phase 8 — full TUI surfaces + cost-route tier stats

### All TUI agent loops

These `LoopCallbacks` implement `OnLLMUsage` + `EarlyStop` (lazy `ensureCostTracker`):

| Surface | Callback |
| --- | --- |
| Interactive chat | `tuiCallbacks` |
| `/btw` | `tuiBtwCallbacks` |
| Pipe `-p` | `pipeCallbacks` |
| RPC JSONL | `rpcCallbacks` |
| WeChat gateway | `tuiWeixinCallbacks` |
| Scheduled tasks | `tuiSchedulerCallbacks` |
| `/loop` cycles | `tuiLoopCycleCallbacks` |

Pipe/RPC construct `CostTracker` at startup; other paths can lazy-init.

### Cost-route tier stats

File: `~/.maclaw/stats/cost_route.json` (host-pid slots, same pattern as daily cost)

```json
{
  "date": "2026-07-12",
  "instances": {
    "hostname-12345": { "decisions": 8, "applied": 3, "shadow": 5, "by_tier": { "c0": 5, "c2": 3 } }
  }
}
```

| Field | Meaning |
| --- | --- |
| `instances[host-pid]` | per-process counters (GUI/TUI do not clobber each other) |
| fleet view | `LoadCostRouteStats` sums slots for today + overlays live process memory |
| `by_tier` / `by_thinking` | c0–c3 / thinking policy counts |
| `decisions` / `shadow` / `applied` | totals |

Legacy flat files are migrated on first write (`legacy` slot). Debounced ~800ms; export flushes.

```bash
maclaw-cli cost
# → costRoute: { by_tier, applied, shadow, ... }
# → summary: cost-route decisions=N applied=A shadow=S last=c0/on
```

## Phase 9 — Doctor + TUI cost-route apply

### System Doctor

- Check `llm.cost_route`: mode, durable `by_tier` / applied / shadow, fleet today `$`
- Shared agent loop status also carries `cost_route_*` fields for GUI System Doctor detail

### TUI model selection

TUI implements `agent.TurnRouter` on interactive, `/btw`, pipe, RPC, WeChat, scheduled-task, and `/loop` cycle callbacks:

- `MACLAW_COST_ROUTE=on` → same C0–C3 map as GUI (`ApplyCostTierConfig` + thinking)
- `shadow` → classic `DecideTurn` + tier observation (stats only)
- `off` → classic `DecideTurn` only
- `activeLLM` keeps the routed model for the whole loop (GetLLMConfig does not snap back to primary)
- `/loop` cycles use `ToolHeavy` classify hints (coding-oriented floor)

## Phase 10 — Hub cost_ops heartbeat + fleet metrics

### Heartbeat payload

GUI `machine.heartbeat` includes optional `cost_ops` (`corelib.CostOpsStat`):

| Field | Source |
| --- | --- |
| `route_decisions` / `route_applied` / `route_shadow` | local `cost_route.json` |
| `daily_cost_usd` / `daily_calls` / `daily_instances` | local `llm_cost_daily.json` fleet sum |
| `cost_route_mode` | `MACLAW_COST_ROUTE` |

### Hub admin API

```http
GET /api/admin/cost-ops/metrics?tenant_id=...
```

Sums online machines' latest `cost_ops` (same pattern as adaptive-prompt metrics).

```bash
maclaw-cli shared-loop hub-metrics --hub URL --admin-token T
# → metrics (adaptive_prompt) + costOps (cost-route + daily $ fleet)
```

## Phase 11 — cost export/merge + Doctor heartbeat summary

### Export / merge (offline fleet)

```bash
maclaw-cli cost export --write
# → ~/.maclaw/stats/exports/cost_ops_<host>_<ts>.json

maclaw-cli cost merge-exports a.json b.json [--out merged.json]
```

Export includes `cost_route`, `daily_fleet`, and the compact `heartbeat` shape used on Hub.

### Doctor

Shared-loop status / System Doctor surfaces `hub_cost_ops_summary` — the same snapshot GUI would attach to `machine.heartbeat.cost_ops`.