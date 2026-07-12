# Response latency — frozen memory snapshot prewarm (2026-07)

**Track:** response latency (post Phase 2 follow-up)  
Related freezes: coding knowledge panel, cost-route, knowledge import/export/recall

## Problem

First chat turn builds the static memory section (`StaticMemorySectionForPrompt` /
user_fact summary) on the critical path before the main LLM call. On large memory
stores this has been observed as multi-second gaps (`frozen_snapshot` / system_prompt
in logs), even after proactive_recall already has a 2.5s budget.

Subsequent turns reuse `frozenMemorySnapshots`, so only the **first** message of a
session pays the full cost.

## Change

| Piece | Behavior |
|-------|----------|
| `IMMessageHandler.WarmFrozenMemorySnapshot` | Generates + caches static section (`isFirstTurn=true`) if missing |
| `App.scheduleWarmFrozenMemorySnapshot` | Resolves `imHandler` / hub imHandler, wires memoryStore if needed, runs warm in a goroutine |
| Call sites | `ensureMemoryStore` (store open), `markAIAssistantReady`, `activateEmbedderAsync` (after tool warmup) |

No protocol change. Refresh still invalidates via `RefreshMemorySnapshot` (`/new`, topic reset).

## Review fix (2026-07)

`appendMemorySection` previously **skipped cache on first turn** (`if !isFirstTurn`), so prewarm never helped the first chat message. It now reuses any existing snapshot on **every** turn (including first). Session-stable snapshot generation always uses first-turn content (memory guide included) for KV-prefix stability.

## Operator check

```powershell
go test ./gui/ -count=1 -timeout 120s -run "TestWarmFrozenMemorySnapshot"

# Logs after startup (before first chat):
# [frozen_snapshot] prewarmed for user "desktop-user" (... bytes, took=...)
# First message should log:
# [frozen_snapshot] reusing cached static memory snapshot for user "desktop-user"
```

## Out of scope

- Async build of dynamic proactive_recall (already budget-gated at 2.5s)
- Parallel UIC + prompt build for first token further cuts
- History load disk optimization
