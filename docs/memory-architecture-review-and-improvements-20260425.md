# MaClaw Memory Architecture Review and Improvement Proposal

## Executive Summary

MaClaw's memory direction is strong: it already has short-term conversation memory, a long-term `memory.Store`, multiple recall strategies, background consolidation, profile extraction, archive/restore, and a temporal structure for higher-level summarization. The issue is not a lack of mechanisms. The main gaps are that identity isolation is weak, derived indexes are not maintained as a single source of truth, and some capabilities that exist in code are not consistently enabled in the default runtime.

Before adding more recall algorithms or smarter summarizers, the system needs harder guarantees in three areas:

1. Principal isolation: memory must be partitioned by user/channel/tenant, not just by project.
2. Consistency: active entries, archive state, BM25/vector indexes, graph, and temporal structures must be updated under one mutation contract.
3. Observability: runtime must clearly show which memory subsystems are active, degraded, or bypassed.

## Current Architecture

## 1. Working layers

The current memory system can be read as five layers:

1. Short-term conversation memory.
   `corelib/agent/conversation_memory.go` keeps recent interaction context for immediate tool use and answer continuity.

2. Long-term store.
   `corelib/memory/store.go` is the central repository for entries, archive state, text search, embeddings, graph links, summaries, and recall APIs.

3. Prompt injection and runtime recall.
   `gui/im_system_prompt.go` pulls summaries and proactive recall results from the store and injects them into the system prompt used in IM flows.

4. Background maintenance pipeline.
   `corelib/memory/pipeline.go`, `consolidator.go`, `profile_consolidator.go`, and `knowledge_extractor.go` attempt to turn raw conversation traces into durable facts, summaries, and profile-level memory.

5. Session snapshots and checkpoints.
   GUI-side memoryshot and checkpoint logic provide local continuity and recovery for the active session, rather than being the canonical long-term truth.

This is a good architectural direction because it separates immediate context, durable memory, background distillation, and UX-level recovery. The problems arise at the boundaries between these layers.

## 2. What is working well

- The design is already more layered than a simple chat transcript log.
- The store supports several retrieval modes instead of hard-coding one prompt stuffing path.
- There is an explicit attempt to promote raw dialogue into higher-value facts and profile information.
- Archive and restore are first-class concepts, which is important for long-running assistants.
- Temporal modeling exists, which gives the system room to grow beyond flat vector recall.

These are good foundations. The recommendations below aim to make them safe and operationally reliable.

## Validated Problems

## 1. Principal isolation is missing in the recall contract

Validated in `corelib/memory/store.go`, `gui/im_system_prompt.go`, and app wiring.

Current summaries such as `UserFactSummary` and `categorySummary` aggregate `user_fact` entries globally. `RecallForProject` and related recall paths filter by project scope, but not by principal identity. In the GUI path, these summaries are injected into the system prompt for IM conversations.

As a result, if multiple contacts share the same app-level `memory.Store`, facts learned from user A can appear in user B's prompt. This is the highest-priority issue because it mixes privacy risk with behavior contamination.

Impact:

- Cross-contact memory leakage in IM and assistant flows.
- Persona drift because the assistant sees facts from the wrong principal.
- Hard-to-debug "why did it mention that?" failures because leakage happens before model generation.

## 2. Knowledge extraction cooldown is global, not scoped

Validated in `corelib/memory/knowledge_extractor.go` and `gui/conversation_archiver.go`.

`KnowledgeExtractor` currently uses a single `lastExtract` timestamp to enforce an extraction cooldown. In a singleton runtime, one conversation archive can throttle all other principals for the next hour.

Impact:

- Memory capture becomes non-deterministic in multi-user scenarios.
- Quiet users can starve active users, or vice versa.
- Missing memory looks random because the throttle is not visible at the call site.

## 3. Checkpoint recall can return stale snapshots

Validated in `gui/session_checkpoint.go` and `corelib/memory/store.go` search behavior.

`RecallCheckpoint` asks the store for `Search(..., 3)` and then chooses the newest record from those three. But store search is not a recency-sorted query; it scans and stops at the limit. Once enough checkpoints accumulate, the newest snapshot may never enter that candidate set.

Impact:

- Session restoration can jump back to an older state.
- Users may observe partial rollback after recovery.
- Bugs will appear data-dependent and worsen over time.

## 4. Embedder hot-swap has a data race

Validated in `corelib/memory/store.go`.

`SetEmbedder` writes `s.embedder` on a background goroutine, while `queryEmbeddingCached`, `EmbedderActive`, and `EmbedderDim` read the same interface concurrently without synchronization. In Go, interface read/write races are undefined behavior.

Impact:

- Race detector failures.
- Possible crashes or inconsistent retrieval behavior under load.
- Unreliable operational behavior when embedding configuration changes at runtime.

## 5. Temporal memory tree is not maintained as the same truth as the store

Validated in `corelib/memory/store.go` mutation paths and `corelib/memory/temporal_tree.go`.

The temporal structure is rebuilt on load, but normal mutations do not consistently update it. For example, some maintenance paths rebuild BM25/vector/graph indexes but do not restore temporal state, and restore paths append entries back without a matching temporal repair.

Impact:

- Time-aware summaries and consolidation drift away from actual active entries.
- Behavior becomes dependent on whether the process restarted recently.
- Debugging gets harder because data structures disagree about what exists.

## 6. Runtime capability wiring is weaker than the design suggests

Validated in `gui/app.go` and pipeline construction.

The codebase contains richer memory components than what the default GUI runtime fully enables. Some pipeline dependencies are wired with `nil` adapters or no-op style gating, so the effective behavior is less capable than the architecture docs imply.

Impact:

- Teams may assume features are active when they are only partially active.
- Field debugging is slowed by ambiguity between "unsupported" and "present but disabled".
- Further feature work risks being built on misleading assumptions.

## Improvement Direction

## Phase 0: Fix safety and correctness first

This phase should happen before any new recall sophistication.

1. Introduce explicit principal scope in the memory API.
   Add a first-class scope struct such as `tenant_id`, `project_id`, `channel_id`, and `principal_id`. Make summary and recall APIs require it rather than infer it from tags.

2. Promote `principal_id` to a primary partition key.
   The store should support efficient filtering and aggregation by principal. Project-only filtering is not enough for IM memory.

3. Scope extractor cooldown by memory scope.
   Replace the single `lastExtract` with a keyed cooldown map, at minimum by `principal_id` and preferably by the full recall scope.

4. Fix checkpoint retrieval semantics.
   Add a recency-aware query or a dedicated latest-checkpoint lookup instead of relying on general text search with a limit.

5. Remove the embedder race.
   Guard embedder reads/writes with `atomic.Value` or store-level locking.

Success criterion: no cross-principal recall, no stale checkpoint restore, no embedder race, and no global extraction throttle.

## Phase 1: Establish a unified mutation contract

Right now the store and its derived structures can diverge. Every mutation should go through a single update path that is responsible for:

- active entry insertion/removal
- archive transitions
- BM25 updates
- vector index updates
- graph updates
- temporal memory updates
- cache invalidation
- metrics and audit events

This can be done with one mutation hook inside `memory.Store`, or by making all secondary structures derive from an append-only event stream. The key is that there must be one authoritative write path.

Success criterion: after add, archive, restore, evict, and rebuild operations, every index and temporal structure sees the same live set.

## Phase 2: Move from tag-encoded scope to schema-level scope

The current design appears to rely heavily on tags and ad hoc filtering. That works for experimentation, but it becomes fragile as the product moves toward multi-user IM and richer memory policies.

Recommended schema upgrades:

- Add explicit fields for `tenant_id`, `project_id`, `channel_id`, `principal_id`, `session_id`, and `memory_kind`.
- Reserve tags for semantic labels, not identity or isolation.
- Build compound indexes for the dominant access patterns such as `(principal_id, memory_kind, updated_at)`.
- Separate "who this memory belongs to" from "what this memory is about".

Success criterion: all isolation-critical queries rely on typed fields, not optional tag conventions.

## Phase 3: Add runtime health and observability

The memory subsystem needs to expose its actual status, not just compile-time capability.

Recommended additions:

- A memory health endpoint or debug panel showing which subsystems are active: BM25, embeddings, graph, temporal tree, extractor, consolidator, profile builder.
- Per-scope counters for extraction, archive, recall, restore, and rejection reasons.
- Trace logging for recall assembly: which memories were selected, by which strategy, under which scope.
- Explicit degraded-mode signals when LLM-backed extractors or summarizers are unavailable.

Success criterion: operators can answer "what memory path was active for this reply?" without code spelunking.

## Phase 4: Improve product semantics on top of the safer core

Once the foundations are corrected, MaClaw can safely add higher-level improvements:

- Per-principal profile memory with freshness and confidence.
- Memory attribution so each recalled fact shows its source conversation or consolidation step.
- Policy-based recall budgets by scenario: IM, project copilot, coding session, checkpoint recovery.
- Better separation between user profile memory, project memory, and ephemeral session state.

This is where smarter recall ranking and memory UX will start paying off.

## Recommended Implementation Order

The suggested order is:

1. Principal-scoped API redesign.
2. Global cooldown fix.
3. Checkpoint latest-query fix.
4. Embedder synchronization fix.
5. Unified mutation hook for store/index/TMT consistency.
6. Runtime health reporting.
7. Schema migration away from tag-dependent isolation.
8. Product-layer improvements such as profile confidence, memory traceability, and policy tuning.

This order reduces user-visible risk quickly while avoiding a large rewrite upfront.

## Concrete Engineering Suggestions

## 1. Introduce a `MemoryScope` type

Example responsibilities:

- define ownership and isolation
- serve as the required input to recall and summary APIs
- provide canonical keys for cooldown and metrics

Representative fields:

```go
type MemoryScope struct {
    TenantID    string
    ProjectID   string
    ChannelID   string
    PrincipalID string
    SessionID   string
}
```

Every externally reachable recall method should accept this explicitly.

## 2. Split memory classes by usage contract

At minimum, distinguish:

- session checkpoint memory
- user profile / user facts
- project knowledge
- conversation archive fragments
- derived summaries

Each class should have different retention, recall, and isolation rules.

## 3. Build dedicated latest-query helpers

Do not use generic text search for checkpoint or state recovery. Add dedicated APIs such as:

- `LatestCheckpoint(scope)`
- `LatestProfile(scope)`
- `RecentMemories(scope, kind, n)`

These APIs should be index-backed and semantically explicit.

## 4. Make rebuilds intentional, not accidental repair

Full rebuild on process load is useful, but it should be recovery logic, not the only thing that restores consistency. Daily correctness must come from correct incremental mutation handling.

## 5. Add a recall decision trace

For every final prompt assembly, capture a compact trace containing:

- scope used
- recall strategies attempted
- memories selected and rejected
- why each memory was included
- whether summarization or extraction was skipped due to cooldown or disabled dependencies

This will dramatically improve both debugging and future optimization.

## Suggested Milestone Definition

For the next stable milestone, the memory subsystem should meet the following bar:

- No memory can cross principal boundaries unless explicitly shared.
- All extraction and consolidation throttles are scope-aware.
- Checkpoint recovery always returns the latest checkpoint for the same scope.
- Derived structures cannot silently drift from active store state.
- Operators can inspect actual memory subsystem health from runtime.

If these guarantees are met, later investments in smarter ranking, profile synthesis, and proactive memory will compound instead of amplifying hidden correctness issues.

## Final Assessment

MaClaw does not primarily need more memory features right now. It needs memory contracts to become stricter.

The architecture is already ambitious enough to support a strong assistant experience. The most valuable next step is to turn the current collection of memory mechanisms into a scoped, consistent, observable system. Once that foundation is in place, the existing design has room to evolve into a much stronger multi-user and project-aware memory platform.
