# Requirements Document

## Introduction

Tool Success Invalidation ensures that the Tool Outcome Learning system (#15) does not make routing decisions based on stale or outdated success records. When conditions change (tool configuration modified, tool becomes unavailable, skill updated/reinstalled, external service endpoint changed, etc.), historical outcome data for that tool should be invalidated or decayed so that the `OutcomeScore` and `ContextOutcomeScore` reflect current reality rather than past performance under different conditions.

Currently, `OutcomeScore` uses a fixed 7-day rolling window with no mechanism to detect or respond to condition changes. A tool that had 95% success rate yesterday but was reconfigured today will retain its high score for up to 7 days, potentially causing the router to keep selecting a now-broken tool and the LLM to waste iterations on repeated failures.

## Glossary

- **UsageTracker**: The component (`corelib/tool/usage_tracker.go`) that records tool invocation outcomes and computes quality scores
- **OutcomeScore**: A [0,1] quality score computed from recent success/failure/retry/abandon rates within a rolling window
- **ContextOutcomeScore**: A context-aware variant of OutcomeScore that filters records by query-token Jaccard similarity
- **Router**: The component (`corelib/tool/router.go`) that selects tools for LLM based on multi-signal scoring (retrieval, experience, skill_match, outcome, priority)
- **InvalidationEvent**: A system event indicating that conditions affecting a tool's reliability have changed
- **Invalidation_Trigger**: A specific condition change that should cause outcome records to be invalidated
- **Decay_Factor**: A multiplier applied to record weights after an invalidation event, reducing their influence on the score
- **Fingerprint**: A compact representation of a tool's current configuration state, used to detect changes

## Requirements

### Requirement 1: Detect Tool Configuration Changes

**User Story:** As the routing system, I want to detect when a tool's configuration has changed, so that outdated outcome records do not mislead routing decisions.

#### Acceptance Criteria

1. WHEN a tool's configuration is modified (API endpoint, credentials, model, version) via `PatchConfigFields` or `SaveConfig`, THE Invalidation_Engine SHALL generate an InvalidationEvent for that tool within 500ms of the configuration write completing
2. WHEN a skill is reinstalled or updated (version change detected in skill directory via `ScanSkillDir`), THE Invalidation_Engine SHALL generate an InvalidationEvent for the corresponding `manage_skill` tool entries matching that skill name, with ScopeTokens containing the skill name
3. WHEN an SSH host configuration changes (host, port, user, key path) in `SSHHostConfig`, THE Invalidation_Engine SHALL generate an InvalidationEvent for the `ssh` tool scoped to that host (ScopeTokens = ["host:user@host:port"])
4. WHEN the LLM provider is switched (`MaclawLLMCurrentProvider` field changes value), THE Invalidation_Engine SHALL generate InvalidationEvents for the fixed set of LLM-dependent tools: `craft_tool`, `delegate_task`, `ask_user`
5. EACH InvalidationEvent SHALL contain: ToolName (string), Timestamp (time.Time), Reason (string, human-readable), ScopeTokens ([]string, optional — nil means global invalidation for that tool)
6. IF the Invalidation_Engine fails to generate an event (panic recovery, nil pointer), THE system SHALL log the failure at error level and continue without crashing — stale scores are preferable to process death

### Requirement 2: Invalidate Stale Outcome Records

**User Story:** As the routing system, I want stale outcome records to be invalidated when conditions change, so that OutcomeScore accurately reflects current tool reliability.

#### Acceptance Criteria

1. WHEN an InvalidationEvent with nil ScopeTokens is received, THE UsageTracker SHALL apply decay to ALL existing outcome records for that ToolName where record.Timestamp < event.Timestamp
2. WHEN an InvalidationEvent with non-nil ScopeTokens is received, THE UsageTracker SHALL apply decay ONLY to records where record.ToolName == event.ToolName AND Jaccard(record.QueryTokens, event.ScopeTokens) >= 0.3 AND record.Timestamp < event.Timestamp
3. THE Decay_Factor SHALL reduce the effective weight of invalidated records to at most 0.1× their original weight in OutcomeScore calculations, applied through the time-based formula defined in Requirement 7
4. WHEN an InvalidationEvent is received, THE UsageTracker SHALL persist the invalidation entry (ToolName, Timestamp, ScopeTokens, Reason) to the `Invalidations` field in the persisted JSON, triggering a save within 1 second
5. Records created AFTER the invalidation timestamp SHALL NOT be affected by that invalidation event — only pre-existing records are decayed
6. WHEN multiple InvalidationEvents exist for the same tool, EACH record SHALL be decayed by the most recent applicable invalidation event only (not compounded across multiple events)

### Requirement 3: Fingerprint-Based Change Detection

**User Story:** As the system, I want to automatically detect configuration changes without requiring explicit notification from every configuration path, so that invalidation is reliable and complete.

#### Acceptance Criteria

1. THE Invalidation_Engine SHALL compute a Fingerprint for each trackable tool (tools registered in `CoreToolNames` or `sessionTools` that have a `FingerprintProvider` registered) based on its current configuration state
2. WHEN the UsageTracker records a new outcome via `RecordOutcome()`, THE Invalidation_Engine SHALL compare the current Fingerprint against the `LastFingerprint` field stored in the tool's invalidation state
3. IF the Fingerprint has changed AND `LastFingerprint` is non-empty (not a first-ever recording), THEN THE Invalidation_Engine SHALL generate an InvalidationEvent with Reason="fingerprint_change" before recording the new outcome
4. IF `LastFingerprint` is empty (first outcome ever recorded for this tool), THEN THE Invalidation_Engine SHALL store the current Fingerprint without generating an InvalidationEvent
5. THE Fingerprint SHALL be computed as SHA-256 of the sorted (by key, lexicographic) JSON-marshaled configuration fields, truncated to 16 hex characters (first 8 bytes of the hash)
6. A `FingerprintProvider` interface SHALL be defined: `ComputeFingerprint(toolName string) string` — returning empty string means "no fingerprint available, skip check"
7. THE Invalidation_Engine SHALL update `LastFingerprint` in the persisted state after each successful fingerprint comparison (whether or not it changed)

### Requirement 4: Consecutive Failure Fast Invalidation

**User Story:** As the routing system, I want rapid response to tool breakage (consecutive failures), so that a broken tool is demoted quickly without waiting for the full 7-day window to rotate out old successes.

#### Acceptance Criteria

1. WHEN a tool accumulates 3 or more consecutive failures (FollowUp="retry" or FollowUp="abandon") without any intervening success (FollowUp="continue"), THE UsageTracker SHALL apply immediate score suppression for that tool
2. THE immediate score suppression SHALL cap the OutcomeScore at max(computed_score, 0.2) — effectively capping at 0.2 since a broken tool's computed score will be low, but ensuring it never goes below whatever the decay formula produces
3. WHEN a successful invocation (FollowUp="continue") is recorded for a suppressed tool, THE UsageTracker SHALL immediately lift the suppression and resume normal OutcomeScore computation for subsequent queries
4. THE consecutive failure count SHALL be tracked per (ToolName, ContextKey) pair, where ContextKey = sorted first 3 query tokens joined by "|" — failures for `ssh` with context "deploy|server|api" do not suppress `ssh` with context "backup|database|prod"
5. THE consecutive failure counter SHALL reset to 0 on any successful outcome, regardless of context overlap with the failures
6. WHEN the 7-day rolling window naturally expires all failure records (because they age out), THE suppression SHALL be automatically lifted even without a new success — checked during OutcomeScore computation
7. THE suppression state (tool name, context key, failure count, suppression active flag) SHALL be persisted alongside other invalidation state

### Requirement 5: Invalidation Persistence and Recovery

**User Story:** As the system, I want invalidation state to survive process restarts, so that a restart does not restore trust in a broken tool.

#### Acceptance Criteria

1. THE UsageTracker SHALL persist invalidation timestamps, active suppressions, and last-known fingerprints as additional fields within the same JSON storage file used for usage records (`tool_usage.json`), serialized atomically via temporary file + rename
2. WHEN a tool invalidation state changes (a new invalidation is recorded, a suppression is added or removed, or a fingerprint is updated), THE UsageTracker SHALL persist the updated state to disk within 1 second via the existing debounced save mechanism
3. WHEN the UsageTracker is loaded from disk, THE UsageTracker SHALL restore all persisted invalidation timestamps, active suppressions, and last fingerprints, and SHALL apply the same decay formula and suppression rules to routing decisions as if no restart had occurred
4. IF the storage file contains valid JSON that cannot be parsed into the expected schema (missing fields, wrong types, or unknown structure in the invalidation section), THEN THE UsageTracker SHALL start with empty invalidation state (no decay, no suppressions, no fingerprints) while preserving any parseable usage records, and log a warning message indicating the parse failure reason
5. IF the storage file is missing or unreadable (permission error, I/O error), THEN THE UsageTracker SHALL start with empty state for everything (records + invalidation) and log a warning message indicating the file access failure reason

### Requirement 6: Manual Invalidation via Tool Interface

**User Story:** As the LLM agent, I want to explicitly invalidate a tool's outcome history when I detect the tool is broken, so that routing adapts immediately without waiting for the fingerprint mechanism.

#### Acceptance Criteria

1. THE UsageTracker SHALL expose an `InvalidateOutcomes(toolName string, reason string)` exported method that immediately generates an InvalidationEvent with nil ScopeTokens (global invalidation for that tool) and applies it through the standard invalidation pipeline (Requirement 2)
2. WHEN the `manage_skill` tool detects a skill execution failure with ErrorClass matching "config_error", "dependency_missing", "setup_failed", or "install_failed", THE system SHALL call `InvalidateOutcomes("manage_skill", reason)` where reason includes the skill name and error class
3. THE invalidation reason SHALL be logged at debug level via the standard `log.Printf("[usage-tracker] InvalidateOutcomes: tool=%s reason=%s", ...)` format for observability
4. `InvalidateOutcomes` SHALL be safe to call concurrently from multiple goroutines and SHALL acquire the UsageTracker mutex before modifying state
5. WHEN `InvalidateOutcomes` is called for a tool that has no existing outcome records, THE method SHALL still persist the invalidation timestamp (so that future records created before the next fingerprint check are properly handled) and return without error

### Requirement 7: Outcome Score Decay Over Time After Invalidation

**User Story:** As the routing system, I want invalidated records to gradually lose influence rather than being instantly deleted, so that the system can gracefully recover if the invalidation was a false alarm.

#### Acceptance Criteria

1. WHEN an InvalidationEvent occurs, THE UsageTracker SHALL NOT delete existing outcome records — it SHALL store the invalidation timestamp in the tool's invalidation state, and decay SHALL be applied at query time (OutcomeScore computation)
2. THE OutcomeScore computation SHALL apply a time-based decay to each record that predates the most recent applicable invalidation: `effective_weight = base_weight × max(0.1, 1.0 - 0.9 × min(hours_since_invalidation / 48.0, 1.0))` where hours_since_invalidation = time elapsed since the invalidation event timestamp
3. AT t=0 (moment of invalidation), effective_weight = base_weight × 1.0 (no immediate penalty — allows brief grace period); AT t=24h, effective_weight ≈ base_weight × 0.55; AT t=48h+, effective_weight = base_weight × 0.1 (minimum, held constant)
4. IF a new invalidation event occurs for the same tool while decay is in progress from a previous event, THE new event's timestamp SHALL replace the old one — decay restarts from t=0 relative to the new event (most recent invalidation wins)
5. Records that were created AFTER the invalidation timestamp are unaffected — they use base_weight = 1.0 with no decay applied
6. THE decay formula SHALL be applied lazily during OutcomeScore/ContextOutcomeScore computation, not eagerly when the invalidation event arrives — this avoids mutating stored records and simplifies persistence
