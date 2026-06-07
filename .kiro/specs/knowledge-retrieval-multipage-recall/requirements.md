# Requirements Document

## Introduction

This feature comprehensively upgrades the knowledge retrieval system in MacLaw/CodeClaw. The upgrade addresses six interconnected problems: (1) single-batch recall coverage is insufficient for large knowledge bases; (2) recall precision degrades due to write-recall semantic gaps (aliases, contextual terms); (3) cross-page context is lost after conversation compaction; (4) complex queries exceed the proactive recall timeout; (5) the LLM agent lacks guidance on when/how to deepen recall; (6) recall capabilities are inconsistently available across GUI/TUI/maclawsrv.

All new capabilities are implemented in `corelib/memory/` as shared library functions, exposed through the unified `HandleTool` interface and `ProactiveContextForPrompt` API. GUI, TUI, and maclawsrv consume these through the same entry points (`RecallByMode`, `ProactivePromptOptions`, `ToolOptions`) with no host-specific recall logic required.

## Glossary

- **Recall_Engine**: The core recall subsystem in `corelib/memory/` (`RecallDynamic`, `RecallDynamicForTool`, `RecallForProject`, `RecallByMode`) that scores, ranks, and returns memory entries. All hosts (GUI/TUI/maclawsrv) share this implementation.
- **Memory_Tool**: The LLM-facing tool exposed via `HandleTool` in `corelib/memory/tool_service.go` with `action=recall`, allowing the agent to query long-term memory. Shared by all hosts.
- **Proactive_Recall**: The automatic injection of relevant memory entries into the system prompt before each LLM call (`ProactiveContextForPrompt` in `corelib/memory/prompt_entries.go`). Hosts pass `ProactivePromptOptions`; all scoring/filtering/expansion logic lives in corelib.
- **Recall_Cursor**: An opaque token encoding the position within a scored result set, enabling retrieval of subsequent pages without re-scoring the entire corpus
- **Topic_Density**: The ratio of entries matching a query above a relevance threshold to the total active entries in the store
- **Exhaustive_Recall**: A recall mode that iterates through all matching entries without a fixed entry/token cap, returning a complete result set
- **Scroll_Session**: A stateful recall context that persists across agent loop iterations, allowing progressive deepening without full re-ranking each time
- **Token_Budget**: The maximum number of estimated tokens that a recall operation may return in a single page
- **Fusion_Score**: The combined relevance score produced by multi-signal fusion (BM25 + Vector + Semantic Graph + Tag matching + Memory Stream Score)
- **Page_Index**: A lightweight in-memory index of key facts/paths/decisions from compacted conversation pages, queryable by the Recall_Engine for cross-page context
- **Alias_Index**: A secondary tag index that maps contextual aliases (nicknames, abbreviations, project codenames) to entry IDs, bridging the write-recall semantic gap
- **Host**: Any consumer of corelib recall capabilities — GUI (desktop AI assistant panel), TUI (terminal agent), or maclawsrv (multi-tenant IM server)

## Requirements

### Requirement 1: Cursor-Based Paginated Recall for Memory Tool

**User Story:** As an LLM agent, I want to request additional pages of recall results beyond the first batch, so that I can access all relevant knowledge when the user's query spans many entries.

#### Acceptance Criteria

1. WHEN the Memory_Tool receives a `recall` action with no `cursor` parameter, THE Recall_Engine SHALL return the first page of results along with a Recall_Cursor if additional results exist beyond the returned page.
2. WHEN the Memory_Tool receives a `recall` action with a valid `cursor` parameter, THE Recall_Engine SHALL return the next page of results starting from the position encoded in the Recall_Cursor.
3. THE Recall_Engine SHALL preserve the original scoring order across paginated pages so that page N+1 contains entries ranked immediately below those in page N.
4. IF the Memory_Tool receives a `recall` action with a `cursor` parameter that is expired, malformed, or does not correspond to any active recall session, THEN THE Recall_Engine SHALL return an error response containing a `has_more: false` field, no `cursor` field, an empty results list, and an `error` string field indicating the cursor is no longer valid.
5. THE Memory_Tool SHALL include a `has_more` boolean field in the recall response indicating whether additional pages are available.
6. THE Memory_Tool SHALL include a `page` integer field in the recall response indicating the current page number (1-indexed).
7. WHEN all matching entries have been returned across pages, THE Recall_Engine SHALL return `has_more: false` and no cursor in the response.
8. THE Recall_Engine SHALL fit as many entries as possible into each page without exceeding the per-page token budget of 2500 tokens, up to a maximum of 15 entries per page, whichever limit is reached first.
9. IF a Recall_Cursor has not been used within 5 minutes of creation, THEN THE Recall_Engine SHALL consider the cursor expired and reject subsequent requests using that cursor.
10. WHEN the Memory_Tool receives a new `recall` action with a different `query` parameter while a previous Recall_Cursor for the same caller is still active, THE Recall_Engine SHALL invalidate the previous cursor and begin a new paginated result set.

### Requirement 2: Adaptive Proactive Recall Budget Expansion

**User Story:** As a user, I want the system to automatically inject more memory entries into the system prompt when many entries are relevant to my message, so that I receive more comprehensive context without manually requesting recall.

#### Acceptance Criteria

1. WHEN Topic_Density for a user message exceeds 0.15 (more than 15% of active entries score above a Fusion_Score threshold of 0.25), THE Proactive_Recall SHALL expand MaxEntries from the default 12 to a maximum of 24.
2. WHEN Topic_Density for a user message is at or below 0.15, THE Proactive_Recall SHALL use the default MaxEntries of 12.
3. THE Proactive_Recall SHALL calculate the expanded entry count as `min(24, max(12, matchingEntries * 0.4))` where matchingEntries is the count of entries exceeding the Fusion_Score threshold of 0.25.
4. THE Proactive_Recall SHALL not exceed a total expanded token budget of 5000 tokens regardless of the expanded entry count.
5. THE Proactive_Recall SHALL complete within the existing 2-second timeout budget, falling back to default MaxEntries of 12 if the expanded recall cannot complete in time.
6. WHILE the expanded budget is active, THE Proactive_Recall SHALL still apply all existing filtering rules (OwnerID isolation, category exclusions, project scope).
7. THE Topic_Density denominator SHALL be the count of active entries visible to the current user (respecting OwnerID and project scope filters). IF the active entry count is zero, THEN Topic_Density SHALL be treated as 0 and no expansion SHALL occur.
8. THE Proactive_Recall SHALL scale the candidate pool limit proportionally with the expanded MaxEntries (poolLimit = expandedMaxEntries * 4) to ensure sufficient scoring candidates are available for selection.

### Requirement 3: Exhaustive Recall Mode

**User Story:** As an LLM agent, I want to retrieve all matching entries for a query when the user explicitly asks to "list all" or "summarize everything about X", so that I can provide a comprehensive answer without information gaps.

#### Acceptance Criteria

1. WHEN the Memory_Tool receives a `recall` action with `mode=exhaustive`, THE Recall_Engine SHALL return all entries matching the query with a Fusion_Score above the minimum relevance threshold of 0.10 without applying the standard maxEntries=15 or maxTokens=2500 limits.
2. THE Recall_Engine SHALL cap the exhaustive recall at a maximum of 100 entries to prevent unbounded memory consumption.
3. THE Recall_Engine SHALL cap the exhaustive recall at a maximum of 15000 tokens to prevent context window overflow.
4. WHEN the exhaustive result set exceeds 100 entries or 15000 tokens, THE Recall_Engine SHALL truncate by entry count first (keeping the top 100 by Fusion_Score), then by token budget (removing lowest-scored entries until within 15000 tokens), and include a `truncated: true` field and a `total_matching` count in the response.
5. THE Recall_Engine SHALL apply the same multi-signal fusion scoring (BM25 + Vector + Semantic Graph + Tag matching) in exhaustive mode as in standard mode.
6. THE Recall_Engine SHALL apply OwnerID isolation and category exclusion filters in exhaustive mode.
7. WHEN `mode=exhaustive` is combined with a `category` parameter, THE Recall_Engine SHALL restrict exhaustive retrieval to entries of the specified category.
8. IF `mode=exhaustive` is combined with a `cursor` parameter, THEN THE Recall_Engine SHALL ignore the `cursor` parameter (exhaustive mode returns a single complete result set, not paginated).

### Requirement 4: Scroll-Through Recall Within Agent Loop

**User Story:** As an LLM agent executing a multi-step task, I want to iteratively deepen my recall across agent loop iterations without re-scoring the entire corpus each time, so that I can efficiently gather more context as needed.

#### Acceptance Criteria

1. WHEN the Memory_Tool receives a `recall` action with `session=true`, THE Recall_Engine SHALL create or reuse a Scroll_Session tied to the current agent loop execution, identified by the `LoopID` field in ToolOptions.
2. THE Scroll_Session SHALL cache the scored candidate list from the first recall invocation within that session.
3. WHEN a subsequent `recall` action within the same Scroll_Session uses a query whose normalized lowercase form is identical to the cached query, THE Recall_Engine SHALL return the next slice of 15 entries (or up to 2500 tokens, whichever is reached first) from the cached candidate list without re-executing scoring.
4. WHEN a subsequent `recall` action within the same Scroll_Session uses a query whose normalized lowercase form differs from the cached query, THE Recall_Engine SHALL discard the cached candidates and perform a fresh scoring for the new query, caching the new results.
5. THE Scroll_Session SHALL expire when the agent loop execution completes (normal exit, cancel, or error), identified by the LoopID becoming invalid.
6. THE Scroll_Session SHALL store at most 200 scored candidates in its cache to bound memory usage.
7. IF the Scroll_Session cache is exhausted (all cached candidates have been returned across successive calls), THEN THE Memory_Tool SHALL return `session_exhausted: true` and an empty entries list.
8. WHEN multiple concurrent agent loops exist for different users, each Scroll_Session SHALL be isolated by (OwnerID, LoopID) tuple, preventing cross-user or cross-loop cache leakage.

### Requirement 5: Backward Compatibility

**User Story:** As an existing user of the recall system, I want all current recall behavior to remain unchanged when I do not use new pagination/expansion features, so that my workflows are not disrupted.

#### Acceptance Criteria

1. WHEN the Memory_Tool receives a `recall` action without `cursor`, `mode=exhaustive`, or `session=true` parameters, THE Recall_Engine SHALL return results using maxEntries=15 and maxTokens=2500 for RecallDynamic, applying the same multi-signal fusion scoring, the same ordering, and the same filtering rules (OwnerID isolation, category exclusions, project scope) as the pre-pagination implementation.
2. IF Topic_Density for a user message is at or below 0.15, THEN THE Proactive_Recall SHALL inject at most 12 entries with a token budget of 2500 tokens, applying the same scoring, ordering, and filtering rules as the pre-expansion implementation.
3. WHEN the RecallForProject function is invoked without new pagination or expansion parameters, THE Recall_Engine SHALL apply its existing dynamic budget behavior (20-30 entries, 2000-5000 tokens scaled by active entry count) unchanged.
4. IF no new parameters (`cursor`, `mode=exhaustive`, `session`) are provided in a RecallByMode invocation, THEN THE RecallByMode function SHALL route to the same mode implementation (dynamic, hybrid, lightmem, adaptive, auto) using the same selection logic as the pre-pagination implementation.
5. THE Memory_Tool response format SHALL remain backward-compatible: `cursor` SHALL only appear when additional pages exist, `has_more` and `page` SHALL only appear when pagination is activated, `truncated` and `total_matching` SHALL only appear when `mode=exhaustive` and results exceed caps, and `session_exhausted` SHALL only appear when `session=true` is specified.
6. WHEN the Memory_Tool receives a `recall` action containing unrecognized parameters, THE Recall_Engine SHALL ignore the unrecognized parameters and process the request using only recognized parameters without returning an error.

### Requirement 6: Performance and Resource Constraints

**User Story:** As a system operator, I want the multi-page recall features to operate within bounded resource consumption, so that the system remains responsive under heavy usage.

#### Acceptance Criteria

1. THE Recall_Engine SHALL complete a single paginated recall page within 500 milliseconds at P95 for stores with up to 1000 active entries.
2. THE Recall_Engine SHALL bound per-user Scroll_Session memory to a maximum of 200 cached candidates, rejecting further caching beyond this count regardless of individual candidate size.
3. THE Recall_Engine SHALL store at most 10 active Recall_Cursors per user to prevent unbounded cursor accumulation.
4. IF a user has 10 active cursors and requests a new recall with pagination, THEN THE Recall_Engine SHALL evict the cursor with the oldest creation timestamp before creating a new one.
5. THE Recall_Engine SHALL allow concurrent Store writes to proceed without blocking while cursor or session cache reads are in progress, so that a paginated read does not increase write latency beyond the baseline measured without active cursors.
6. THE exhaustive recall mode SHALL complete within 2000 milliseconds at P95 for stores with up to 1000 active entries.
7. WHILE 10 or more concurrent users each hold active Recall_Cursors or Scroll_Sessions, THE Recall_Engine SHALL maintain per-page recall latency within 750 milliseconds at P95 for stores with up to 1000 active entries.


### Requirement 7: Cross-Page Context Recall via Page Index

**User Story:** As a user working on a multi-step task that spans conversation compaction boundaries, I want information from earlier pages (file paths, tool outputs, confirmed decisions) to remain retrievable by the Recall_Engine, so that the agent does not lose context after compaction.

#### Acceptance Criteria

1. WHEN a compaction boundary is created by `trimHistoryWithSummary`, THE Page_Index SHALL extract and index file paths, tool output summaries (first 200 characters per tool result), confirmed decisions, and entity names (via ExpandQuery) from the entries being compacted, up to 50 indexed items per page.
2. THE Page_Index SHALL be implemented in `corelib/memory/` as a standalone type (`PageIndex`) usable by any host without host-specific logic.
3. WHEN a recall query matches items in the Page_Index with BM25 score above the relevance threshold, THE Recall_Engine SHALL include page-indexed items in the candidate set alongside long-term memory entries, with a recency boost proportional to page proximity (most recent compacted page = +3.0, decaying by 1.0 per page distance, minimum +0.5).
4. THE Recall_Engine SHALL deduplicate between page-indexed entries and long-term memory entries based on content similarity (substring containment with minimum 20 characters), preferring the more complete version.
5. THE Page_Index SHALL retain indexed pages for the duration of the host's session lifetime (determined by the host via a `ClearPageIndex(userID)` call), with a maximum of 20 pages per user.
6. IF the Page_Index exceeds 20 pages for a user, THEN it SHALL evict the oldest page entries using FIFO ordering while preserving the most recent 15 pages.
7. THE Page_Index SHALL complete indexing within 500 milliseconds for conversations up to 100 entries per page.

### Requirement 8: Alias-Aware Recall (Write-Recall Semantic Gap)

**User Story:** As a user who refers to the same entity by different names across conversations (e.g., "4090服务器" vs "api.rapidai.tech", "小明" vs "Zhang Ming"), I want the recall system to find relevant entries regardless of which alias I use in my query, so that I do not need to remember exactly how information was originally saved.

#### Acceptance Criteria

1. WHEN a memory entry is saved via `SaveWithContext`, THE Alias_Index SHALL extract entity aliases from both the entry content and the contextHint, storing bidirectional alias mappings (e.g., "4090服务器" ↔ entry containing "api.rapidai.tech").
2. WHEN the Recall_Engine processes a query, THE Alias_Index SHALL expand the query with known aliases for any recognized entities, adding the expanded terms to the BM25 multi-query set.
3. THE Alias_Index SHALL be implemented in `corelib/memory/` as a component of the Store, initialized during `NewStore` and rebuilt during `rebuildDerivedIndexesLocked`.
4. WHEN alias expansion produces additional BM25 hits that were not in the original query results, THE Recall_Engine SHALL apply an alias match boost of +2.0 (additive) to these entries, below the existing `tagExactMatchBoost` of +5.0 but above the baseline.
5. THE Alias_Index SHALL support a maximum of 1000 alias mappings per store to bound memory usage, evicting oldest (by creation time) mappings when the limit is exceeded.
6. THE Alias_Index SHALL persist as part of the store's derived index state and be rebuilt from entry tags and entities during store initialization.

### Requirement 9: Recall Timeout Resilience

**User Story:** As a user with a large knowledge base (500+ entries), I want proactive recall to consistently return useful results within its time budget, so that the system never silently drops recall context due to timeout.

#### Acceptance Criteria

1. WHEN the Proactive_Recall is approaching the 2-second timeout budget (elapsed > 1.5 seconds), THE Recall_Engine SHALL return the best results computed so far (partial results) rather than returning nothing.
2. THE Recall_Engine SHALL implement a staged recall pipeline where BM25 results are available within 200ms, vector results within 500ms, and semantic graph expansion within 1500ms — each stage's results are independently usable if subsequent stages timeout.
3. WHEN partial results are returned due to timeout, THE Proactive_Recall SHALL annotate the injected context with a signal (e.g., `[partial recall - deep search skipped]`) so the LLM knows additional recall via the memory tool may be productive.
4. THE staged recall pipeline SHALL be implemented in `corelib/memory/` as part of `RecallDynamic` (or a new `RecallStaged` variant), usable by all hosts via `ProactiveContextForPrompt`.
5. THE Recall_Engine SHALL log the stage reached (bm25_only / bm25_vec / full) and elapsed time for observability, using the existing `[perf]` log format.

### Requirement 10: LLM Recall Guidance in Tool Description

**User Story:** As an LLM agent, I want the memory tool's description to clearly explain pagination, exhaustive mode, and scroll-through capabilities, so that I can autonomously decide when to deepen recall without external prompting.

#### Acceptance Criteria

1. THE Memory_Tool definition (in `corelib/memory/tool_service.go` or the tool definition generator) SHALL include parameter descriptions for `cursor`, `mode=exhaustive`, and `session=true` that explain when each is appropriate.
2. THE Memory_Tool description SHALL include usage guidance: "If initial recall returns has_more=true, use the cursor to retrieve additional pages. If the user asks to 'list all' or 'summarize everything about X', use mode=exhaustive. If working through a multi-step task and need progressive context, use session=true."
3. THE Memory_Tool response format SHALL include actionable hints when results are limited: when `has_more=true`, append "Use cursor='{cursor}' to see more results"; when `truncated=true`, append "Total matching: {N}. Use mode=exhaustive with category filter for focused results."
4. THE tool description and response hints SHALL be defined in `corelib/memory/` constants/functions so all hosts (GUI/TUI/maclawsrv) emit consistent guidance without duplicating text.

### Requirement 11: Corelib-First Architecture Constraint

**User Story:** As a platform maintainer, I want all knowledge retrieval capabilities to be implemented in `corelib/memory/` with host-agnostic interfaces, so that GUI, TUI, and maclawsrv benefit from improvements simultaneously without code duplication.

#### Acceptance Criteria

1. ALL new recall capabilities (CursorPaginator, ScrollSessionManager, AdaptiveBudgetCalculator, ExhaustiveRecaller, PageIndex, AliasIndex, staged recall) SHALL be implemented as exported types/functions in `corelib/memory/`.
2. THE `HandleTool` function in `corelib/memory/tool_service.go` SHALL be the single entry point for all hosts' memory tool dispatch. No host SHALL implement recall logic outside of `HandleTool` for the memory tool.
3. THE `ProactiveContextForPrompt` function in `corelib/memory/prompt_entries.go` SHALL encapsulate all proactive recall logic including adaptive budget expansion, page index integration, and timeout resilience. Hosts SHALL only pass configuration via `ProactivePromptOptions`.
4. THE `ToolOptions` struct SHALL be extended with a `LoopID string` field for scroll session identification. Hosts set this field; corelib manages the session lifecycle.
5. THE `ProactivePromptOptions` struct SHALL be extended with fields for page index integration (`PageIndexEnabled bool`, `PageIndexMaxTokens int`) and timeout resilience (`PartialResultsEnabled bool`). Hosts configure these; corelib implements the behavior.
6. WHEN a host calls `Store.ClearPageIndex(userID)`, THE Page_Index SHALL clear all indexed pages for that user. This is the only host-specific lifecycle hook required — all other logic is internal to corelib.
7. THE recall capabilities SHALL not import any package from `gui/`, `tui/`, or `maclawsrv/`. Dependencies flow one direction: hosts import `corelib/memory/`.
