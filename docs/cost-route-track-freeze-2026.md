# Cost-route track freeze (2026-07)

**Status: frozen / closed for default “继续”**  
OpenSquilla-inspired cost routing, budget gates, fleet stats, and Hub cost ops are shipped through Phase 11.

Phase log: [cost-route-phase1.md](./cost-route-phase1.md)  
Related closed track: [adaptive-prompt-track-summary.md](./adaptive-prompt-track-summary.md)

## Shipped (summary)

| Area | Capability |
|------|------------|
| Tier map C0–C3 | Rule classify + optional model switch (`MACLAW_COST_ROUTE`) |
| Thinking policy | Bound to tier (off/low/high) |
| Tool compress | Structured previews + spill counters |
| Denial auto-pause | Consecutive security denials pause tools |
| Daily budget | Fleet-aware hard-stop + CLI `budgetStatus` |
| Durable fleet stats | `llm_cost_daily.json` + `cost_route.json` host-pid slots |
| TUI surfaces | OnLLMUsage / EarlyStop / TurnRouter on chat, btw, pipe, RPC, WeChat, scheduler, `/loop` |
| Hub ops | `machine.heartbeat.cost_ops` + admin metrics + export/merge |
| Doctor | Local + hub cost summaries |

## Operator check

```bash
maclaw-cli cost
maclaw-cli doctor --local-only
# optional fleet:
maclaw-cli cost export --write
maclaw-cli shared-loop hub-metrics --hub URL --admin-token T
```

```powershell
$env:MACLAW_COST_ROUTE = "shadow"   # observe
$env:MACLAW_COST_ROUTE = "on"       # apply tiers
```

## Out of freeze (explicit goals only)

1. Per-tenant Hub cost quotas beyond machine heartbeat sum  
2. UI settings page for cost-route mode (today env-driven)  
3. Auto-tuning of tier thresholds from fleet by_model data  

## Freeze rule

Do not stack more cost-route micro-features on generic “继续”.  
Re-open only with a named goal + acceptance criteria.
