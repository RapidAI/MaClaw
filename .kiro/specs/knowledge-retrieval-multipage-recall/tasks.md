# Implementation Plan: Knowledge Retrieval Multi-Page Recall

## Overview

This implementation adds seven complementary mechanisms to `corelib/memory/` as shared library code: cursor-based pagination, adaptive proactive recall budget, exhaustive recall mode, scroll-through recall, Page Index, Alias Index, and staged recall pipeline. All hosts (GUI/TUI/maclawsrv) consume these through `HandleTool` and `ProactiveContextForPrompt` with no host-specific recall logic.

Implementation proceeds bottom-up: foundational types and data models first, then independent components in parallel, then integration into existing `HandleTool` and `ProactiveContextForPrompt`, and finally tool description updates and backward-compatibility verification.

## Tasks

- [x] 1. Foundational types, constants, and host interface extensions
  - [x] 1.1 Define constants, result types, and cursor token encoding
    - Create `corelib/memory/paginated_types.go` with: `PaginatedResult`, `ScrollResult`, `ExhaustiveResult`, `AdaptiveBudgetResult`, `StagedRecallResult` structs
    - Define constants: `defaultMaxEntries=12`, `expandedMaxEntries=24`, `defaultMaxTokens=2500`, `expandedMaxTokens=5000`, `topicDensityThreshold=0.15`, `expansionFactor=0.4`, `exhaustiveMaxEntries=100`, `exhaustiveMaxTokens=15000`, `cursorTTL=5min`, `maxCursorsPerUser=10`, `scrollSessionMaxCache=200`, `perPageTokenBudget=2500`
    - Implement `cursorPayload` struct with `EncodeCursor` / `DecodeCursor` (base64 JSON)
    - _Requirements: 1.8, 1.9, 2.3, 2.4, 3.2, 3.3, 6.2, 6.3_

  - [x] 1.2 Extend `ProactivePromptOptions` and `ToolOptions` structs
    - Add `PageIndexEnabled bool`, `PageIndexMaxTokens int`, `PartialResultsEnabled bool` to `ProactivePromptOptions`
    - Add `LoopID string` to `ToolOptions`
    - Ensure zero-value defaults maintain backward compatibility
    - _Requirements: 11.4, 11.5_

- [x] 2. CursorPaginator implementation
  - [x] 2.1 Implement CursorPaginator with LRU eviction
    - Create `corelib/memory/cursor_paginator.go`
    - Implement `CursorPaginator` struct with `sync.Mutex`, per-user `userCursorPool` (max 10, LRU eviction by `LastUsedAt`)
    - Implement `FirstPage(store, query, category, projectPath, ownerID)` — executes full recall pipeline, caches scored candidates, returns first page
    - Implement `NextPage(cursorID)` — slices cached candidates, advances position, returns next page
    - Implement `Evict(userID)` — removes expired cursors (>5min TTL)
    - Token-bounded page sizing: fit entries into 2500 tokens per page, max 15 entries
    - _Requirements: 1.1, 1.2, 1.3, 1.5, 1.6, 1.7, 1.8, 1.9, 1.10, 6.3, 6.4_

  - [x]* 2.2 Write property test: Paginated recall order preservation (Property 1)
    - **Property 1: Paginated recall order preservation**
    - Concatenating all pages SHALL produce the same entry sequence as a single non-paginated recall
    - Use `rapid` library, generate random stores and queries, min 100 iterations
    - **Validates: Requirements 1.2, 1.3**

  - [x]* 2.3 Write property test: has_more correctness (Property 2)
    - **Property 2: has_more correctness**
    - `has_more` SHALL be true iff additional scored entries exist beyond current position
    - **Validates: Requirements 1.5, 1.7**

  - [x]* 2.4 Write property test: Per-page token budget invariant (Property 3)
    - **Property 3: Per-page token budget invariant**
    - Sum of `EstimateTextTokens(entry.Content)` per page SHALL not exceed 2500 tokens
    - **Validates: Requirements 1.8**

  - [x]* 2.5 Write property test: Cursor pool bounded with LRU eviction (Property 14)
    - **Property 14: Cursor pool bounded with LRU eviction**
    - Active cursors per user SHALL not exceed 10; new cursor creation evicts oldest by `LastUsedAt`
    - **Validates: Requirements 6.3, 6.4**

- [x] 3. AdaptiveBudgetCalculator implementation
  - [x] 3.1 Implement AdaptiveBudgetCalculator
    - Create `corelib/memory/adaptive_budget.go`
    - Implement `Calculate(matchingEntries, totalActiveEntries int) AdaptiveBudgetResult`
    - Formula: when `matchingEntries/totalActiveEntries > 0.15`, expanded count = `min(24, max(12, floor(matchingEntries * 0.4)))`
    - When totalActiveEntries == 0, treat density as 0 (no expansion)
    - Scale candidate pool limit proportionally: `poolLimit = expandedMaxEntries * 4`
    - _Requirements: 2.1, 2.2, 2.3, 2.4, 2.7, 2.8_

  - [x]* 3.2 Write property test: Adaptive expansion formula correctness (Property 4)
    - **Property 4: Adaptive expansion formula correctness**
    - When density > 0.15, expanded entry count SHALL equal `min(24, max(12, floor(matchingEntries * 0.4)))`
    - **Validates: Requirements 2.3**

  - [x]* 3.3 Write property test: Expanded token budget cap (Property 5)
    - **Property 5: Expanded token budget cap**
    - Total estimated tokens of all injected entries SHALL not exceed 5000 tokens
    - **Validates: Requirements 2.4**

  - [x]* 3.4 Write property test: Expansion preserves existing filters (Property 6)
    - **Property 6: Expansion preserves existing filters**
    - All returned entries SHALL satisfy OwnerID isolation, category exclusion, and project scope filtering
    - **Validates: Requirements 2.6**

- [x] 4. ExhaustiveRecaller implementation
  - [x] 4.1 Implement RecallExhaustive on Store
    - Create `corelib/memory/exhaustive_recall.go`
    - Implement `Store.RecallExhaustive(query, category, projectPath, ownerID ...string) *ExhaustiveResult`
    - Return all entries with FusionScore > 0.10, cap at 100 entries and 15000 tokens
    - Truncation: by entry count first (top 100 by score), then by token budget (remove lowest-scored)
    - Set `Truncated` and `TotalMatching` fields when caps hit
    - Apply same multi-signal fusion scoring, OwnerID isolation, category exclusion
    - _Requirements: 3.1, 3.2, 3.3, 3.4, 3.5, 3.6, 3.7_

  - [x]* 4.2 Write property test: Exhaustive mode respects caps (Property 7)
    - **Property 7: Exhaustive mode respects caps**
    - Result SHALL contain at most 100 entries AND sum of tokens SHALL not exceed 15000
    - **Validates: Requirements 3.2, 3.3**

  - [x]* 4.3 Write property test: Exhaustive mode preserves scoring order (Property 8)
    - **Property 8: Exhaustive mode preserves scoring order**
    - Entries SHALL be ordered by multi-signal fusion score, higher first
    - **Validates: Requirements 3.5**

  - [x]* 4.4 Write property test: Exhaustive mode respects owner and category filters (Property 9)
    - **Property 9: Exhaustive mode respects owner and category filters**
    - All entries SHALL match OwnerID and category filters
    - **Validates: Requirements 3.6, 3.7**

- [x] 5. ScrollSessionManager implementation
  - [x] 5.1 Implement ScrollSessionManager
    - Create `corelib/memory/scroll_session.go`
    - Implement `ScrollSessionManager` struct with `sync.Mutex`, sessions map keyed by loopID
    - Implement `GetOrCreate(loopID, store, query, category, projectPath, ownerID)` — returns existing session or creates new (caches up to 200 scored candidates)
    - Implement `Advance(loopID, pageTokenBudget)` — returns next slice (15 entries or 2500 tokens), sets `session_exhausted` when depleted
    - Implement `Destroy(loopID)` — removes session
    - Handle query change: discard cached candidates, re-score with new query, reset position
    - _Requirements: 4.1, 4.2, 4.3, 4.4, 4.5, 4.6, 4.7, 4.8_

  - [x]* 5.2 Write property test: Scroll session sequential access without overlap (Property 10)
    - **Property 10: Scroll session sequential access without overlap**
    - Returned entry sets SHALL be non-overlapping and concatenation forms a prefix of initial scored candidate list
    - **Validates: Requirements 4.2, 4.3**

  - [x]* 5.3 Write property test: Scroll session cache bounded at 200 (Property 11)
    - **Property 11: Scroll session cache bounded at 200**
    - Cached scored candidates SHALL not exceed 200
    - **Validates: Requirements 4.6, 6.2**

  - [x]* 5.4 Write property test: Scroll session exhaustion signal (Property 12)
    - **Property 12: Scroll session exhaustion signal**
    - Once all candidates returned, next Advance SHALL return `session_exhausted: true` with empty entries
    - **Validates: Requirements 4.7**

- [x] 6. Checkpoint - Ensure all tests pass
  - Ensure all tests pass, ask the user if questions arise.

- [x] 7. PageIndex implementation
  - [x] 7.1 Implement PageIndex for cross-page context retrieval
    - Create `corelib/memory/page_index.go`
    - Implement `PageIndex` struct with `sync.RWMutex`, per-user `userPageIndex` (max 20 pages, FIFO eviction keeping most recent 15)
    - Implement `IndexCompactedPage(userID, entries)` — extracts file paths, tool output summaries (first 200 chars), decisions, entity names; max 50 items per page; SHA-256 fingerprint for dedup
    - Implement `Query(userID, query, queryTokens)` — BM25 scoring with page-proximity recency boost (most recent = +3.0, decaying by 1.0 per page distance, min +0.5)
    - Implement `Clear(userID)` — removes all indexed pages for a user
    - Complete indexing within 500ms for conversations up to 100 entries per page
    - _Requirements: 7.1, 7.2, 7.3, 7.4, 7.5, 7.6, 7.7_

  - [x]* 7.2 Write unit tests for PageIndex
    - Test FIFO eviction at 20 pages → keeps most recent 15
    - Test dedup via SHA-256 fingerprint
    - Test recency boost scoring
    - Test `Clear` removes all pages
    - Test indexing performance (<500ms for 100 entries)
    - _Requirements: 7.5, 7.6, 7.7_

- [x] 8. AliasIndex implementation
  - [x] 8.1 Implement AliasIndex for write-recall semantic gap bridging
    - Create `corelib/memory/alias_index.go`
    - Implement `AliasIndex` struct with `sync.RWMutex`, bidirectional `aliases` map (normalized term → list of aliases), capacity 1000 with FIFO eviction
    - Implement `Expand(entities []string) []string` — returns known aliases for recognized entities
    - Implement `Register(term string, aliases []string)` — adds bidirectional mappings
    - Implement `Rebuild(entries []Entry)` — reconstructs from all active entries' Tags and Entities
    - Alias match boost: +2.0 additive (below tagExactMatchBoost +5.0, above baseline)
    - _Requirements: 8.1, 8.2, 8.3, 8.4, 8.5, 8.6_

  - [x]* 8.2 Write unit tests for AliasIndex
    - Test bidirectional mapping registration
    - Test Expand returns correct aliases
    - Test FIFO eviction at 1000 capacity
    - Test Rebuild from entry metadata
    - Test alias match boost integration with RecallDynamic
    - _Requirements: 8.4, 8.5, 8.6_

- [x] 9. StagedRecallPipeline implementation
  - [x] 9.1 Implement StagedRecallPipeline with timeout resilience
    - Create `corelib/memory/staged_recall.go`
    - Implement `StagedRecallPipeline.Recall(ctx, store, query, opts, deadline)` with three stages:
      - Stage 1 (BM25): guaranteed within 200ms
      - Stage 2 (+Vector): target within 500ms
      - Stage 3 (+Semantic Graph + Page Index + Alias expansion): target within 1500ms
    - Each stage's results independently usable if subsequent stages timeout
    - Return `StagedRecallResult` with `StageReached`, `Elapsed`, `Partial` fields
    - Log stage reached and elapsed time with `[perf]` format
    - _Requirements: 9.1, 9.2, 9.3, 9.4, 9.5_

  - [x]* 9.2 Write unit tests for StagedRecallPipeline
    - Test BM25 stage completes within 200ms
    - Test partial results returned on timeout
    - Test `[partial recall - deep search skipped]` annotation when partial
    - Test all three stages complete within 1500ms budget
    - _Requirements: 9.2, 9.3, 9.5_

- [x] 10. Checkpoint - Ensure all tests pass
  - Ensure all tests pass, ask the user if questions arise.

- [x] 11. HandleTool integration
  - [x] 11.1 Extend HandleTool to dispatch new recall modes
    - Modify `corelib/memory/tool_service.go` `HandleTool` function
    - Parse new parameters: `cursor` (string), `mode` (string, "exhaustive"), `session` (bool)
    - Routing logic: no new params → existing `RecallDynamic`; `cursor=xxx` → `CursorPaginator.NextPage`; no cursor + pagination context → `CursorPaginator.FirstPage`; `mode=exhaustive` → `Store.RecallExhaustive`; `session=true` → `ScrollSessionManager.GetOrCreate` + `Advance`
    - Validate mutually exclusive combinations: exhaustive+cursor → error, session+cursor → error
    - Add response fields: `cursor`, `has_more`, `page`, `truncated`, `total_matching`, `session_exhausted` (only when activated)
    - _Requirements: 1.1, 1.2, 1.4, 3.1, 3.8, 4.1, 5.5, 5.6, 11.2_

  - [x]* 11.2 Write property test: Backward compatibility — no new fields without new params (Property 13)
    - **Property 13: Backward compatibility — no new fields without new params**
    - Without `cursor`, `mode=exhaustive`, or `session=true`, response SHALL not contain new fields
    - **Validates: Requirements 5.1, 5.3, 5.4, 5.5**

  - [x]* 11.3 Write unit tests for HandleTool error cases
    - Test invalid/expired cursor → error message
    - Test cursor belongs to different user → "cursor not found"
    - Test exhaustive+cursor → mutual exclusion error
    - Test session+cursor → mutual exclusion error
    - Test unrecognized parameters → ignored without error
    - _Requirements: 1.4, 3.8, 5.6_

- [x] 12. ProactiveContextForPrompt integration
  - [x] 12.1 Integrate adaptive budget and staged recall into ProactiveContextForPrompt
    - Modify `corelib/memory/prompt_entries.go`
    - When `PageIndexEnabled=true`: query PageIndex and integrate results with dedicated sub-budget (`PageIndexMaxTokens`, default 800)
    - When `PartialResultsEnabled=true`: use `StagedRecallPipeline.Recall` with 2-second deadline; annotate with `[partial recall - deep search skipped]` on timeout
    - Integrate `AdaptiveBudgetCalculator`: compute topic density, expand budget when > 0.15 threshold
    - Fall back to default 12 entries within 2-second budget if expansion cannot complete
    - Deduplicate page-indexed entries vs long-term memory entries (substring containment, min 20 chars)
    - _Requirements: 2.1, 2.2, 2.5, 7.3, 7.4, 9.1, 9.3, 11.3_

  - [x]* 12.2 Write unit tests for ProactiveContextForPrompt extensions
    - Test adaptive expansion triggers at density > 0.15
    - Test staged recall returns partial results on timeout
    - Test page index results deduplicated against memory entries
    - Test PageIndexMaxTokens sub-budget respected
    - _Requirements: 2.1, 7.4, 9.1_

- [x] 13. Store initialization and lifecycle wiring
  - [x] 13.1 Wire CursorPaginator, ScrollSessionManager, PageIndex, AliasIndex into Store
    - Add `CursorPaginator`, `ScrollSessionManager`, `PageIndex`, `AliasIndex` as fields on `Store`
    - Initialize in `NewStore`
    - Wire `AliasIndex.Rebuild` into `rebuildDerivedIndexesLocked`
    - Wire `AliasIndex.Register` into `SaveWithContext` (extract aliases from contextHint)
    - Wire `AliasIndex.Expand` into `RecallDynamic` query expansion (augment BM25 multi-query set)
    - Expose `Store.PageIndex()` and `Store.ScrollSessions()` accessors for host lifecycle hooks
    - _Requirements: 8.3, 8.6, 11.1, 11.6_

  - [x]* 13.2 Write integration tests for Store wiring
    - Test AliasIndex rebuilt on store load
    - Test alias expansion produces additional BM25 hits
    - Test PageIndex accessible via Store accessor
    - Test ScrollSessionManager accessible via Store accessor
    - _Requirements: 8.3, 11.1_

- [x] 14. LLM recall guidance in tool description
  - [x] 14.1 Update Memory Tool description and response hints
    - Update tool definition in `corelib/memory/tool_service.go` (or tool definition generator)
    - Add parameter descriptions for `cursor`, `mode=exhaustive`, `session=true` with usage guidance
    - Add response hints: when `has_more=true` → "Use cursor='{cursor}' to see more results"; when `truncated=true` → "Total matching: {N}. Use mode=exhaustive with category filter for focused results."
    - Define all description text and hints as constants/functions in `corelib/memory/` for host consistency
    - _Requirements: 10.1, 10.2, 10.3, 10.4_

- [x] 15. Checkpoint - Ensure all tests pass
  - Ensure all tests pass, ask the user if questions arise.

- [x] 16. Backward compatibility and performance verification
  - [x]* 16.1 Write backward compatibility tests
    - Test `RecallDynamic` output unchanged when no new params
    - Test `RecallForProject` output unchanged
    - Test `RecallByMode` routing unchanged
    - Test response format has no new fields for standard recalls
    - _Requirements: 5.1, 5.2, 5.3, 5.4_

  - [x]* 16.2 Write concurrency and performance tests
    - Test concurrent paginated recalls don't corrupt cursor state (race detector)
    - Test paginated recall < 500ms P95 for 1000 entries
    - Test exhaustive recall < 2000ms P95 for 1000 entries
    - Test concurrent store writes don't block cursor/session reads
    - _Requirements: 6.1, 6.5, 6.6, 6.7_

- [x] 17. Final checkpoint - Ensure all tests pass
  - Ensure all tests pass, ask the user if questions arise.

## Notes

- Tasks marked with `*` are optional and can be skipped for faster MVP
- Each task references specific requirements for traceability
- Checkpoints ensure incremental validation
- Property tests validate universal correctness properties from the design document using `pgregory.net/rapid`
- Unit tests validate specific examples, edge cases, and error conditions
- All implementation lives in `corelib/memory/` — no host-specific recall logic in `gui/`, `tui/`, or `maclawsrv/`
- The `rapid` library is already used in `corelib/memory/` for existing property tests
- Hosts only need to: (1) pass extended `ProactivePromptOptions`/`ToolOptions`, (2) call `Store.PageIndex().IndexCompactedPage` on compaction, (3) call `Store.PageIndex().Clear` on session reset, (4) call `Store.ScrollSessions().Destroy(loopID)` on agent loop exit

## Task Dependency Graph

```json
{
  "waves": [
    { "id": 0, "tasks": ["1.1", "1.2"] },
    { "id": 1, "tasks": ["2.1", "3.1", "4.1", "5.1"] },
    { "id": 2, "tasks": ["2.2", "2.3", "2.4", "2.5", "3.2", "3.3", "3.4", "4.2", "4.3", "4.4", "5.2", "5.3", "5.4", "7.1", "8.1", "9.1"] },
    { "id": 3, "tasks": ["7.2", "8.2", "9.2"] },
    { "id": 4, "tasks": ["11.1", "13.1"] },
    { "id": 5, "tasks": ["11.2", "11.3", "12.1", "13.2"] },
    { "id": 6, "tasks": ["12.2", "14.1"] },
    { "id": 7, "tasks": ["16.1", "16.2"] }
  ]
}
```
