# Response latency track freeze (2026-07)

**Status: frozen for observation**  
Default “继续” should **not** stack more first-token latency micro-features without a **named new goal**.

## Shipped (this track)

| Increment | Summary |
|-----------|---------|
| Phase 2 base | Short-message workflow bypass; lightweight classify model; perf logs; tool progress labels |
| Proactive recall budget | `imProactiveRecallBudget` 2.5s timeout skip |
| Frozen snapshot prewarm | `WarmFrozenMemorySnapshot` at store ready / AI ready / embedder activate |
| Pre-loop parallel | Early `收到，正在处理`; history load ∥ profile classify |
| Execution profile fast path | `ClassifyEmbeddingOnly` for light/full routing (no tree wait) |
| Fusion tree deadline | Dual-channel UIC fusion waits **5s** max for L3; tree-only keeps **30s** |

### Key docs

- [response-latency-optimization.md](./response-latency-optimization.md)
- [response-latency-optimization-phase2.md](./response-latency-optimization-phase2.md)
- [response-latency-frozen-snapshot-prewarm-2026.md](./response-latency-frozen-snapshot-prewarm-2026.md)
- [response-latency-preloop-parallel-2026.md](./response-latency-preloop-parallel-2026.md)

## Operator regression

```powershell
go test ./corelib/intent/ -count=1 -timeout 90s -run "TestDefaultFusionTreeDeadline|TestFusionTreeDeadlineCaps|TestSetFusionTreeDeadline|TestProperty11_UICTimeout|TestDefaultLLMTimeout"
go test ./gui/ -count=1 -timeout 120s -run "TestWarmFrozenMemorySnapshot|TestClassifyIMExecutionProfileUsesEmbeddingOnly|TestExecutePreparedIMEntryParallel"
```

Logs to expect on a short chat after startup:

```text
[frozen_snapshot] prewarmed for user "desktop-user" ...
[frozen_snapshot] reusing cached static memory snapshot ...
[perf] stage=im_pre_loop ... history_load=... loop_ctx=... system_prompt=...
# dual-channel UIC (workflow gates): tree may log "deadline exceeded (5s)" then degrade
```

## Out of freeze (explicit goals only)

1. Parallel system-prompt sections beyond current parallel history/profile  
2. History persistence / disk path redesign if Load ever leaves memory  
3. Adaptive fusion deadline from measured tree P50  
4. Frontend skeleton token / typing indicator independent of progress events  

## Review / fix (2026-07)

| Issue | Fix |
|-------|-----|
| Prewarm never helped **first** chat | `appendMemorySection` reused cache only when `!isFirstTurn` → now reuses any existing snapshot |
| Snapshot gen mixed first/later content | Session-stable generate always uses first-turn static content (guide included) |
| Embedding-only profile missing affordances | `ClassifyEmbeddingOnly` now calls `applyExecutionAffordances` |
| History loader panic safety | Buffered load + panic recover so pre-loop cannot hang on join |
| Early progress invisible on desktop | `isVisibleAIAssistantProgressText` blocked `收到，正在处理` (no allow-prefix) → allow `收到` / `正在处理` / `正在准备` |
| Workflow static-only cache inconsistency | `appendStaticMemoryOnly` now generates with guide=true and type-safe reuse |
| Prewarm triple-schedule at startup | `snapshotWarmInflight` coalesces concurrent `WarmFrozenMemorySnapshot` |
| Snapshot logic drift | Shared `loadOrBuildStaticMemorySnapshot` / `cachedStaticMemorySnapshot` for chat + workflow |
| Hot-path log spam | Dropped per-message "reusing cached snapshot" log (generate/prewarm only) |
| Prewarm vs first-message double-build | Channel singleflight in `loadOrBuild` (one builder; waiters `<-done`) |
| Refresh vs in-flight build | Gen stamp + wait for done; stale publish discarded (no orphan waiters, no resurrect after `/new`) |

## Freeze rule

Do not stack more latency micro-optimizations on generic “继续”.  
Re-open only with a named goal + acceptance criteria.  
Default “继续” should switch product line (HubCenter, workflow UX, etc.).
