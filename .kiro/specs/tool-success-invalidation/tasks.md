# Implementation Plan: Tool Success Invalidation

## Overview

Add a decay and suppression layer to the UsageTracker so that OutcomeScore reflects current tool reliability rather than stale historical performance. The implementation integrates entirely within the existing `UsageTracker` component—no separate goroutines, no new persistence files. Changes involve data model extensions, fingerprint-based detection, consecutive failure tracking, time-based decay at query time, and persistence format migration.

## Tasks

- [ ] 1. Data models and persistence migration
  - [ ] 1.1 Define InvalidationEvent, ToolInvalidationState, SuppressionEntry, and UsageData structs
    - Add `InvalidationEvent` struct with ToolName, Timestamp, Reason, ScopeTokens fields
    - Add `ToolInvalidationState` struct with LastInvalidation, LastFingerprint, Suppressions fields
    - Add `SuppressionEntry` struct with ContextKey, FailureCount, Active fields
    - Add `UsageData` wrapper struct with Records and Invalidations map
    - Add `invalidations map[string]ToolInvalidationState` field to UsageTracker
    - _Requirements: 1.5, 2.4, 4.7, 5.1_

  - [ ] 1.2 Implement persistence format migration in load/save
    - Modify `load()` to try parsing as `UsageData` first, fall back to `[]UsageRecord` flat array
    - Migrate legacy flat array to `UsageData{Records: parsed}` transparently
    - Handle corrupted invalidation section gracefully (start with empty state, log warning)
    - Modify `saveSnapshot()` to serialize as `UsageData` (records + invalidations)
    - Use atomic write (temp file + rename) for persistence
    - _Requirements: 5.1, 5.2, 5.3, 5.4, 5.5_

  - [ ]* 1.3 Write property test for persistence round-trip (Property 10)
    - **Property 10: Persistence Round-Trip**
    - **Validates: Requirements 5.3**

  - [ ]* 1.4 Write unit tests for persistence migration
    - Test flat `[]UsageRecord` file migration to `UsageData`
    - Test malformed invalidation section graceful degradation
    - Test missing/unreadable storage file startup
    - _Requirements: 5.3, 5.4, 5.5_

- [ ] 2. Decay formula and OutcomeScore integration
  - [ ] 2.1 Implement `decayMultiplier` method on UsageTracker
    - Compute `max(0.1, 1.0 - 0.9 × min(hours_since_invalidation / 48.0, 1.0))`
    - Only decay records that predate the invalidation timestamp
    - Implement scope check: if ScopeTokens non-nil, check Jaccard >= 0.3 threshold
    - Use only the most recent invalidation event (no compounding)
    - Handle zero-time / negative duration gracefully (treat as t=0)
    - _Requirements: 7.1, 7.2, 7.3, 7.4, 7.5, 7.6, 2.1, 2.2, 2.5, 2.6_

  - [ ]* 2.2 Write property test for decay formula correctness (Property 1)
    - **Property 1: Decay Formula Correctness**
    - **Validates: Requirements 7.2, 7.3**

  - [ ]* 2.3 Write property test for post-invalidation records unaffected (Property 2)
    - **Property 2: Post-Invalidation Records Unaffected**
    - **Validates: Requirements 2.5, 7.5**

  - [ ]* 2.4 Write property test for most recent invalidation wins (Property 3)
    - **Property 3: Most Recent Invalidation Wins (No Compounding)**
    - **Validates: Requirements 2.6, 7.4**

  - [ ] 2.5 Integrate decay into `outcomeScoreWithCount`
    - Change from integer counting to float64 weighted accumulation
    - Apply `decayMultiplier` to each record's contribution (total, successes, retry/abandon penalties)
    - Preserve existing formula semantics while applying decay
    - _Requirements: 7.2, 7.6_

  - [ ] 2.6 Integrate decay into `ExperienceScore`
    - Multiply each record's contribution by `overlap * recency * decay`
    - _Requirements: 7.2, 7.6_

  - [ ]* 2.7 Write property tests for global and scoped invalidation (Properties 4, 5)
    - **Property 4: Global Invalidation Decays All Matching Records**
    - **Property 5: Scoped Invalidation Respects Jaccard Threshold**
    - **Validates: Requirements 2.1, 2.2**

  - [ ]* 2.8 Write unit tests for decay formula spot checks
    - Test specific time points: t=0, t=12h, t=24h, t=36h, t=48h, t=72h
    - Test scoped invalidation with various Jaccard similarities
    - _Requirements: 7.2, 7.3_

- [ ] 3. Checkpoint - Ensure all tests pass
  - Ensure all tests pass, ask the user if questions arise.

- [ ] 4. Consecutive failure suppression
  - [ ] 4.1 Implement `consecutiveFailureKey` and failure tracking methods
    - Implement `consecutiveFailureKey(queryTokens)`: sorted first 3 tokens joined by "|"
    - Implement `recordConsecutiveFailure(toolName, contextKey)`: increment counter, activate suppression at threshold 3
    - Implement `resetConsecutiveFailure(toolName)`: reset counter on success
    - Implement `isSuppressed(toolName, contextKey)`: check active suppression
    - _Requirements: 4.1, 4.4, 4.5_

  - [ ] 4.2 Integrate suppression into OutcomeScore computation
    - After score computation, if `isSuppressed(toolName, contextKey)` → cap score at 0.2
    - Implement suppression auto-lift when all failure records age out of 7-day window
    - Persist suppression state alongside other invalidation state
    - _Requirements: 4.2, 4.6, 4.7_

  - [ ] 4.3 Hook failure/success tracking into RecordExperience
    - On success (FollowUp="continue"): call `resetConsecutiveFailure(toolName)`
    - On failure (FollowUp="retry"/"abandon"): call `recordConsecutiveFailure(toolName, contextKey)`
    - _Requirements: 4.3, 4.5_

  - [ ]* 4.4 Write property tests for consecutive failure suppression (Properties 7, 8)
    - **Property 7: Consecutive Failure Suppression State Machine**
    - **Property 8: Suppression Caps Score**
    - **Validates: Requirements 4.1, 4.2, 4.3, 4.4, 4.5**

  - [ ]* 4.5 Write unit tests for consecutive failure tracking
    - Test `consecutiveFailureKey` with various token counts (0, 1, 3, 5 tokens)
    - Test suppression auto-lift when failures age out of 7-day window
    - _Requirements: 4.4, 4.6_

- [ ] 5. Fingerprint-based change detection
  - [ ] 5.1 Define FingerprintProvider interface and implement providers
    - Define `FingerprintProvider` interface: `ComputeFingerprint(toolName string) string`
    - Implement `ConfigFingerprintProvider`: fingerprints tool config fields from AppConfig
    - Implement `SkillFingerprintProvider`: fingerprints skill version + directory mtime
    - Implement `SSHFingerprintProvider`: fingerprints SSH host config (host:port:user:keypath)
    - Implement `computeFingerprint(fields)`: SHA-256 of sorted JSON, truncated to 16 hex chars
    - _Requirements: 3.1, 3.5, 3.6_

  - [ ] 5.2 Implement fingerprint check in RecordExperience
    - Add `FingerprintProviders` registry to UsageTracker
    - Implement `checkFingerprint(toolName)`: compare current vs stored fingerprint
    - On first-ever recording (LastFingerprint empty): store fingerprint, no event
    - On fingerprint change: generate InvalidationEvent with Reason="fingerprint_change" before recording
    - Update LastFingerprint after each successful comparison
    - Wrap in `recover()` to handle panicking providers gracefully
    - _Requirements: 3.2, 3.3, 3.4, 3.7, 1.6_

  - [ ]* 5.3 Write property test for fingerprint state machine (Property 6)
    - **Property 6: Fingerprint State Machine**
    - **Validates: Requirements 3.3, 3.4, 3.7**

  - [ ]* 5.4 Write unit tests for fingerprint computation
    - Test `computeFingerprint` determinism with reordered keys
    - Test empty provider return ("") skips comparison
    - Test panic recovery in provider
    - _Requirements: 3.5, 3.6_

- [ ] 6. Checkpoint - Ensure all tests pass
  - Ensure all tests pass, ask the user if questions arise.

- [ ] 7. InvalidationEngine public API and event hooks
  - [ ] 7.1 Implement ApplyInvalidation and InvalidateOutcomes methods
    - Implement `ApplyInvalidation(event InvalidationEvent)`: persist event, update state, trigger save
    - Implement `InvalidateOutcomes(toolName, reason string)`: public API for manual invalidation with nil ScopeTokens
    - Ensure mutex safety for concurrent access
    - Log invalidation at debug level
    - Handle invalidation for tool with no existing records (persist timestamp anyway)
    - _Requirements: 6.1, 6.3, 6.4, 6.5, 2.4_

  - [ ] 7.2 Wire PatchConfigFields hook for SSH/LLM config changes
    - In `PatchConfigFields` (gui/app.go): detect SSH host field changes → call `tracker.InvalidateOutcomes(toolName, reason)`
    - In `PatchConfigFields`: detect LLM provider change → call InvalidateOutcomes for craft_tool, delegate_task, ask_user
    - Ensure event generation within 500ms of config write
    - _Requirements: 1.1, 1.3, 1.4_

  - [ ] 7.3 Wire skill scanner hook for skill version changes
    - After `ScanSkillDir` result comparison: detect skill version change → call ApplyInvalidation with skill-scoped ScopeTokens
    - _Requirements: 1.2_

  - [ ] 7.4 Wire manage_skill error hook for automatic invalidation
    - On skill execution failure with ErrorClass "config_error", "dependency_missing", "setup_failed", "install_failed": call InvalidateOutcomes("manage_skill", reason)
    - _Requirements: 6.2_

  - [ ]* 7.5 Write property test for records never deleted (Property 9)
    - **Property 9: Records Never Deleted or Mutated by Invalidation**
    - **Validates: Requirements 7.1, 7.6**

  - [ ]* 7.6 Write property test for concurrency safety (Property 11)
    - **Property 11: InvalidateOutcomes Concurrency Safety**
    - **Validates: Requirements 6.4**

  - [ ]* 7.7 Write property test for config change triggers (Property 12)
    - **Property 12: Config Change Triggers Invalidation for Tool-Affecting Fields**
    - **Validates: Requirements 1.1**

  - [ ]* 7.8 Write unit tests for event hooks
    - Test LLM provider switch triggers exactly 3 events for fixed tool set
    - Test skill version change produces scoped event with skill name in ScopeTokens
    - Test config patches with only non-tool fields do NOT trigger invalidation
    - _Requirements: 1.1, 1.2, 1.4_

- [ ] 8. Integration tests and wiring
  - [ ] 8.1 Wire UsageTracker initialization with invalidation support
    - Ensure UsageTracker loads invalidation state on startup
    - Register FingerprintProviders during app initialization
    - Connect to existing debounced save mechanism for persistence within 1 second
    - _Requirements: 5.1, 5.2, 5.3_

  - [ ]* 8.2 Write integration tests
    - End-to-end: PatchConfigFields with SSH host change → verify OutcomeScore drops
    - End-to-end: RecordExperience with fingerprint change → verify decay applied
    - File round-trip: write state, read back, verify equivalence
    - Backward compat: load legacy flat array file, verify migration
    - _Requirements: 1.1, 1.3, 3.3, 5.3_

- [ ] 9. Final checkpoint - Ensure all tests pass
  - Ensure all tests pass, ask the user if questions arise.

## Notes

- Tasks marked with `*` are optional and can be skipped for faster MVP
- Each task references specific requirements for traceability
- Checkpoints ensure incremental validation
- Property tests validate universal correctness properties using `pgregory.net/rapid`
- Unit tests validate specific examples and edge cases
- All logic integrates within the existing `UsageTracker` component under the existing `mu` mutex
- No new goroutines, no new persistence files—uses existing debounced save mechanism
- Fingerprint providers use stdlib only (crypto/sha256, encoding/json, sync, time)

## Task Dependency Graph

```json
{
  "waves": [
    { "id": 0, "tasks": ["1.1"] },
    { "id": 1, "tasks": ["1.2", "4.1", "5.1"] },
    { "id": 2, "tasks": ["1.3", "1.4", "2.1", "5.2"] },
    { "id": 3, "tasks": ["2.2", "2.3", "2.4", "2.5", "2.6", "5.3", "5.4"] },
    { "id": 4, "tasks": ["2.7", "2.8", "4.2", "4.3"] },
    { "id": 5, "tasks": ["4.4", "4.5", "7.1"] },
    { "id": 6, "tasks": ["7.2", "7.3", "7.4", "7.5", "7.6", "7.7", "7.8"] },
    { "id": 7, "tasks": ["8.1"] },
    { "id": 8, "tasks": ["8.2"] }
  ]
}
```
