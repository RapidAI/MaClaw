# Adaptive prompt & shared agent loop — ops cheat sheet

Operator guide for the OpenSquilla-inspired cost/observability surfaces shipped in MaClaw.

## Shared agent loop (strangler)

Eligible chat/background turns can run on `corelib/agent.RunLoop` instead of the legacy IM loop.

| Control | How |
| --- | --- |
| Config | `shared_agent_loop_enabled` in `~/.maclaw/config.json` |
| Env override | `MACLAW_SHARED_AGENT_LOOP=on\|off\|shadow` (wins over config) |
| Canary | env `MACLAW_SHARED_AGENT_LOOP_PERCENT` **or** config `shared_agent_loop_canary_percent` (0..100, sticky by user id; env wins when set) |
| Workflow pilot | env `MACLAW_SHARED_AGENT_LOOP_WORKFLOW` **or** config `shared_agent_loop_workflow` (non-doc phases; env wins when set) |

```bash
# Persist canary + workflow pilot without env
maclaw-cli shared-loop enable --percent 25 --workflow on
```

**System Doctor → Agent loop path**: set **Canary %** (0–100) and toggle **workflow pilot** in the UI
(writes config; disabled when the matching env var is set).

### Process runtime stats (GUI Doctor)

`agent.shared_loop_stats` is **always** included in GUI `RunDoctor` (even with zero chat traffic):

| Field | Meaning |
| --- | --- |
| `mode` / `percent` | Effective mode + canary % |
| `shared_turns` / `legacy_turns` | Path mix since process start |
| `shared_success` / `shared_error` / `shared_cancelled` | Shared-path outcomes |
| `skip_canary` / `skip_ineligible` / `shadow_eligible` | Why turns stayed on legacy (or shadow-would-have) |
| `skip_by_reason` / `last_skip_reason` | Breakdown e.g. `ineligible:workflow phase`, `canary:canary` |
| Adaptive prompt counters | light% + `light_deny` / `light_upgrade` when present |
| A/B + rates | `ab=sample/eligible`, `ab_pct`, `upgrade_rate`, `deny_rate`, `by_task` |

System Doctor UI badge: `on · canary 25% · workflow` when applicable.

**System Doctor adaptive line** (via `GetSharedAgentLoopStatus`) also shows
`by_task`, `ab=…`, `upgrade_rate` / `deny_rate`, and `ab_pct` when set — same
fields as TUI `FormatLine` / `maclaw-cli shared-loop stats`.

```bash
maclaw-cli shared-loop status
maclaw-cli shared-loop status --user alice   # sticky canary preview (or --canary-user)
maclaw-cli shared-loop enable
maclaw-cli shared-loop disable
```

`shadow` = legacy executes; eligibility is logged only.

### Canary preview

Sticky membership uses FNV-1a bucket `0..99` of the user id (same algorithm in GUI runtime and CLI).

```bash
# Example: at 25% canary, does user "alice" take the shared path?
maclaw-cli shared-loop status --user alice --pretty=false
# → canaryPreview: { user_id, percent, bucket, allows }
```

GUI Wails: `PreviewSharedLoopCanary(userId, percent)` — pass `percent=-1` to use env.

**System Doctor → Agent loop path**: enter a user id → **Check** shows IN/OUT canary + bucket + percent (same sticky algorithm as runtime).

## Adaptive system prompt

Simple turns use a **light** system prompt (no coding gate / SSH catalog / MCP bulk). Complex turns keep **full**.

| Control | How |
| --- | --- |
| Auto | `ClassifyTurn` → light for `fast` / `intent` / `summary` |
| Force | `MACLAW_PROMPT_PROFILE=light\|full` (`auto`/empty = classify) |
| Quality A/B | `MACLAW_PROMPT_AB_PERCENT=0..100` — sticky sample of light-eligible turns forced full |

When light is chosen, MaClaw dual-builds light vs full prompts on CPU (no second LLM call) and records estimated system-prompt token savings.

### Where you see it

| Surface | What |
| --- | --- |
| Chat Turn chip | `prompt=light(-N)` · `full(upgraded)` · `full(ab)` · `full(soft)` |
| System Doctor | Adaptive prompt line + **Export stats** / **Open exports** / **Reset stats** |
| System Doctor Hub line | Connection state + `adaptive_prompt` heartbeat snapshot (fleet feed) |
| GUI `GetSharedAgentLoopStatus` | `prompt_*` counters + `prompt_profile_forced` when env set |
| TUI `/status` `/doctor` | `adaptive-prompt: light 66% … · env MACLAW_PROMPT_PROFILE=full` |
| TUI `/status <user>` | sticky canary preview line |
| TUI `/canary <user>` | IN/OUT canary · bucket · percent |
| TUI `/prompt-export` | write `~/.maclaw/stats/exports/prompt_profile_*.json` |
| `maclaw-cli doctor` | check `agent.adaptive_prompt` (`env_override`, `forced_profile`) |
| `maclaw-cli shared-loop stats` | JSON hit rate + `estTokensSaved` + `envOverride`/`forcedProfile` |
| Durable file | `~/.maclaw/stats/prompt_profile.json` |

```bash
maclaw-cli shared-loop stats
maclaw-cli shared-loop stats-reset
maclaw-cli doctor --local-only
```

### Multi-instance export / merge (fleet view)

Each MaClaw process keeps its own counters (GUI vs TUI vs agentservice). For a
fleet or multi-host rollup **without** changing Hub protocol:

```bash
# On each machine / process that has accumulated stats:
maclaw-cli shared-loop export --write
# → ~/.maclaw/stats/exports/prompt_profile_<host>_<ts>.json

# Or explicit path:
maclaw-cli shared-loop export --out /tmp/node-a.json

# GUI: System Doctor → Agent loop path → Export stats
# (Wails ExportAdaptivePromptStats — same default path as --write)
# Open exports folder: System Doctor → Open exports

# On ops host after collecting files:
maclaw-cli shared-loop merge-exports node-a.json node-b.json --out fleet.json
```

Machine → Hub: each connected GUI includes `adaptive_prompt` on `machine.heartbeat`.
System Doctor shows whether Hub is connected and the snapshot that would be reported.

```bash
# Live online-machine rollup (tenant/global admin JWT)
maclaw-cli shared-loop hub-metrics --hub https://hub.example --admin-token "$MACLAW_HUB_ADMIN_TOKEN"
# Defaults: --hub from MACLAW_HUB_URL or config remote_hub_url; token from MACLAW_HUB_ADMIN_TOKEN
maclaw-cli shared-loop hub-metrics --tenant tenant_acme
```

Merged output sums `light_turns`, `full_turns`, `est_tokens_saved`,
`light_tool_denies`, `light_upgrades`, `by_task`, `by_denied_tool`.
`last_*` fields come from the newest export by `exported_at`.

No user/tenant labels are included (process-level only).

### Hub live rollup (GUI heartbeat)

When MaClaw GUI is connected to Hub, each `machine.heartbeat` may include:

```json
"adaptive_prompt": {
  "light_turns": 12,
  "full_turns": 4,
  "light_percent": 75,
  "est_tokens_saved": 48000,
  "light_tool_denies": 1,
  "light_upgrades": 2,
  "summary": "adaptive-prompt: light 75% …"
}
```

Hub stores the latest snapshot on the machine runtime record (no DB migration).

| Surface | Path |
| --- | --- |
| Per-machine | `GET /api/admin/debug/machines` → `adaptive_prompt` |
| Fleet sum (online) | `GET /api/admin/adaptive-prompt/metrics` |
| Admin UI | Hub **Machines** tab: fleet card + per-machine heartbeat column |
| CLI | `maclaw-cli shared-loop hub-metrics --hub URL --admin-token JWT` |
| CLI | `maclaw-cli shared-loop hub-metrics` |

Totals are process-level sums across **currently online** machines. Offline machines drop out of the sum when they disconnect.

### Debug force full/light

```bash
# Windows PowerShell
$env:MACLAW_PROMPT_PROFILE = "light"
# or
$env:MACLAW_PROMPT_PROFILE = "full"
```

Restart GUI/TUI after changing env so new processes pick it up.

### Quality A/B sampling

When classify would pick **light**, a sticky hash of the user text may force
**full** for quality comparison (no second LLM on every turn — only the sampled arm runs full).

```powershell
# Force full on ~10% of light-eligible turns (same question keeps same arm)
$env:MACLAW_PROMPT_AB_PERCENT = "10"
```

Counters (doctor / CLI / export / heartbeat):

| Field | Meaning |
| --- | --- |
| `ab_eligible_light` | Turns that classified light (before A/B) |
| `ab_sample_full` | Of those, forced full by A/B |
| `upgrade_rate_percent` | `light_upgrades / total_turns` |
| `deny_rate_percent` | `light_tool_denies / light_turns` |

Format line: `· ab=3/40 · upgrade_rate=5% · deny_rate=2% · ab_pct=10`

## Classify task breakdown

Each adaptive turn can record the `ClassifyTurn` task (`fast` / `intent` / `summary` / `reasoning` / …) so operators can see *why* light or full was chosen.

| Field | Where |
| --- | --- |
| `by_task` / `byTask` | durable `prompt_profile.json`, CLI stats, doctor detail |
| `last_task` / `last_reason` | last classify outcome (or env force reason) |
| Format line | `· task=fast · by_task=fast:3,reasoning:1` |

## Light misroute signals

| Signal | Meaning |
| --- | --- |
| `light_tool_denies` | Model requested a non-allowlisted tool during a light turn (e.g. `bash`) |
| `by_denied_tool` / `last_denied_tool` | Which tools were blocked (top-N shown as `bash:2+1tools`) |
| `light_upgrades` / `last_upgrade_reason` | Preemptive soft-intent upgrades **or** in-loop tool-deny recovery |

Operator surfaces (compact):

| Surface | Example fragment |
| --- | --- |
| TUI `/status` FormatLine | `· light_deny=3(bash:2,write_file:1) · light_upgrade=1(bash)` |
| System Doctor adaptive line | same counters + top deny / last upgrade |
| `maclaw-cli doctor` detail | full `by_denied_tool` map + `last_upgrade_reason` |
| GUI `GetSharedAgentLoopStatus` | `prompt_by_denied_tool`, `prompt_last_upgrade_reason` |

### In-loop light→full retry (default on)

When the model requests a non-allowlisted tool on a light turn and the host implements `LightProfileUpgrader`:

1. Record `light_tool_denies`
2. Upgrade prompt profile to **full** (once per loop)
3. Rebuild tools + replace conversation system message
4. Re-authorize and **execute** the tool
5. Record `light_upgrades` with reason `tool_deny_retry:<tool>`

| Control | How |
| --- | --- |
| Disable retry | `MACLAW_PROMPT_LIGHT_RETRY=off` (or `0` / `false`) |

Hosts with upgrader: TUI, agentservice, GUI shared `RunLoop`.

Format line example: `· light_deny=2(bash) · light_upgrade=1`

## Tool surface alignment

Light turns restrict tools on **TUI**, **GUI** (execution contracts), and **agentservice** (shared `LightTurnToolAllowlist`: no bash/coding/file/MCP bulk). `read_tool_result` remains available for spilled large tool outputs.

## Related packages

- `corelib/agent` — `PromptProfile`, `RunLoop`, `TurnUsage`, prompt stats
- `corelib/doctor` — `agent.shared_loop`, `agent.adaptive_prompt`
- `corelib/toolresult` — dual-view tool handles + `read_tool_result`

## Regression suite

```powershell
./scripts/test-adaptive-shared-loop.ps1
# or: bash scripts/test-adaptive-shared-loop.sh
```

Critical-path tests for canary, adaptive stats, CLI shared-loop, GUI Doctor bindings, TUI slash ops.