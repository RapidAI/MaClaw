# Adaptive prompt & shared loop — track summary

OpenSquilla-inspired cost/efficiency work delivered for MaClaw (local + Hub).

**Status: closed.** Default “继续” should not extend this track without a new named goal.

## What shipped

| Area | Capability |
| --- | --- |
| Shared agent loop | Strangler flag, canary %, shadow mode, workflow pilot |
| Config + UI canary/workflow | `shared_agent_loop_canary_percent` / `shared_agent_loop_workflow` (CLI + System Doctor) |
| Skip counters | canary / ineligible / shadow reasons |
| Adaptive prompt | light vs full system prompt via `ClassifyTurn` |
| Shadow savings | CPU dual-build token estimate (no 2nd LLM) |
| Light tools | Shared allowlist (TUI / agentservice / GUI contracts) |
| Misroute recovery | Soft full-intent upgrade; mid-loop tool-deny → full retry |
| Turn chip | `prompt=light(-N)` / `full(upgraded)` / `full(ab)` / `full(soft)` |
| Observability | Doctor, TUI `/status`, CLI stats/reset/hub-metrics, durable JSON |
| Fleet | CLI export/merge; GUI heartbeat → Hub metrics + **Machines admin UI** |
| Quality A/B | `MACLAW_PROMPT_AB_PERCENT` sticky force-full sample |

## Operator commands

```bash
maclaw-cli shared-loop status
maclaw-cli shared-loop stats
maclaw-cli shared-loop export --write
maclaw-cli shared-loop merge-exports a.json b.json
maclaw-cli shared-loop hub-metrics --hub URL --admin-token T
maclaw-cli doctor --local-only
```

Hub (GUI online):

```text
GET /api/admin/adaptive-prompt/metrics
GET /api/admin/debug/machines   # adaptive_prompt field
Hub Admin → Machines tab       # fleet card + per-machine heartbeat column
maclaw-cli shared-loop hub-metrics   # admin JWT wrapper
```

## Env knobs

| Env | Role |
| --- | --- |
| `MACLAW_SHARED_AGENT_LOOP` | on / off / shadow |
| `MACLAW_SHARED_AGENT_LOOP_PERCENT` | canary 0–100 (wins over config) |
| config `shared_agent_loop_canary_percent` | canary 0–100 when env unset |
| config `shared_agent_loop_workflow` | non-doc workflow pilot when env unset |
| `MACLAW_PROMPT_PROFILE` | light / full / auto |
| `MACLAW_PROMPT_LIGHT_RETRY` | off disables deny→full retry |
| `MACLAW_PROMPT_AB_PERCENT` | 0–100 quality A/B sample |

## Key packages

- `corelib/agent` — RunLoop, PromptProfile, stats, export, light tools
- `corelib/doctor` — `agent.shared_loop`, `agent.adaptive_prompt`
- `maclaw-cli` — shared-loop subcommands (status/stats/export/merge/hub-metrics)
- `gui` — Turn chip, System Doctor (canary/export/Hub line), heartbeat
- `tui` — `/status` `/canary` `/prompt-export` `/doctor`
- `hub` — runtime store + admin metrics

## Regression (feature-frozen entry)

```powershell
# Windows
./scripts/test-adaptive-shared-loop.ps1

# Unix
bash scripts/test-adaptive-shared-loop.sh
```

Runs filtered `go test` on `corelib/doctor`, `corelib/agent`, `maclaw-cli`, `gui`, `tui`.

## Operator QA checklist

| Check | How |
| --- | --- |
| Shared-loop mode | Doctor / `shared-loop status` / env `MACLAW_SHARED_AGENT_LOOP` |
| Canary sticky | GUI Doctor input, CLI `--user`, TUI `/canary` — same bucket |
| Adaptive light% | chat a few turns → Doctor line / CLI `stats` / TUI `/status` |
| Light misroute | force tool on light → `light_deny` + optional upgrade retry |
| Export fleet | GUI Export / TUI `/prompt-export` / CLI `export --write` → `merge-exports` |
| Hub feed | Doctor Hub line connected; admin `hub-metrics` when JWT available |

**Track status: feature-frozen** unless a named bug or new theme is opened.

## Full ops guide

See [adaptive-prompt-and-shared-loop-ops.md](./adaptive-prompt-and-shared-loop-ops.md).

## Related: cost-route Phase 1–5

OpenSquilla-style cost routing stack:

| Phase | Capability |
| --- | --- |
| 1 | C0–C3 shadow observability |
| 2 | Apply model by tier (`on`) |
| 3 | Thinking on/off/low/high by tier |
| 4 | Tool structured compression + stats |
| 5 | Denial auto-pause + `maclaw-cli cost` |

See [cost-route-phase1.md](./cost-route-phase1.md).
