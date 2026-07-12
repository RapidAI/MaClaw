# Response latency — pre-loop parallel + fast execution profile (2026-07)

**Track:** response latency (after frozen snapshot prewarm)  
Related: [response-latency-optimization-phase2.md](./response-latency-optimization-phase2.md), [response-latency-frozen-snapshot-prewarm-2026.md](./response-latency-frozen-snapshot-prewarm-2026.md)

## Problem

In `executePreparedIMEntry`, after gates the critical path was strictly serial:

1. `memory.Load` (history)
2. `prepareIMLoopContext` + **full UIC `Classify`** (embedding **and** tree/LLM fusion, tree deadline often 30s)
3. `buildIMEntrySystemPrompt`
4. agent loop

Full UIC fusion on every message for **execution-profile routing only** is wasteful: profile routing only needs a rough light vs full signal, and waits on tree/LLM that workflow gates already handle elsewhere.

Users also saw no progress text until much later pre-loop work finished.

## Change

| Item | Behavior |
|------|----------|
| Early progress | Emit `收到，正在处理` via `OnProgress` immediately after gates |
| Parallel history | `ConversationMemory.Load` runs in a goroutine alongside loop-ctx + profile classify |
| Fast profile classify | `classifyIMExecutionProfileAndSemantic` uses **`ClassifyEmbeddingOnly`** instead of full `Classify` |

Conservative fallbacks unchanged: missing/degraded embedding → full agent profile; structural long messages still skip semantic entirely.

## Operator check

```powershell
go test ./gui/ -count=1 -timeout 120s -run "TestClassifyIMExecutionProfileUsesEmbeddingOnlyNotFullFusion|TestExecutePreparedIMEntryParallelHistoryLoadPattern|TestHandlerClassifyIMExecutionProfileSkipsSemantic"
```

Logs (typical short chat):

```text
[perf] stage=im_pre_loop ... history_load=... loop_ctx=... system_prompt=...
# loop_ctx should no longer wait multi-second tree fusion for profile routing
```

UI: first progress line appears before system prompt / first token.

## Out of scope

- Lowering global UIC fusion tree timeout for **workflow** control path (still 30s by design for some callers)
- Parallel system-prompt build with other pre-loop steps (prompt needs history)
- History disk I/O redesign (Load is already in-memory active branch)
