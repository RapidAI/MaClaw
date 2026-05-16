# Implementation Plan: Hermes Skill Self-Improvement

## Overview

This plan implements 9 subsystems across `corelib/`, `gui/`, and `tui/` packages, organized by dependency order: foundation layer first (atomic writes, type changes), then scanner, security, skill management, outcome tracking, memory snapshot, knowledge injection, nudge system, and finally atomic write migration. Each task builds on previous tasks and maintains GUI/TUI parity.

The implementation language is Go, matching the existing codebase.

## Tasks

- [x] 1. Implement atomic file write package (`corelib/fileutil/atomic.go`)
  - [x] 1.1 Create `corelib/fileutil/atomic.go` with `AtomicWriteFile(path string, data []byte, perm os.FileMode) error`
    - Use `os.CreateTemp` in the same directory as target with `.tmp` suffix prefix
    - Write data to temp file, set permissions
    - If target file exists, read its permissions and preserve them
    - Call `os.Rename` to atomically replace target
    - On rename failure with cross-device error (`EXDEV` / Windows equivalent), fall back to copy-and-rename strategy
    - Clean up temp file on any write failure
    - _Requirements: 6.1, 6.2, 6.3, 6.4, 6.5, 6.10, 10.1, 10.2, 10.3, 10.4_

  - [ ]* 1.2 Write unit tests for `AtomicWriteFile`
    - Test successful write creates file with correct content and permissions
    - Test temp file cleanup on write failure
    - Test permission preservation when overwriting existing file
    - Test `.tmp` suffix on temp file names
    - Test cross-device fallback (mock `os.Rename` failure with `EXDEV`)
    - _Requirements: 6.1, 6.2, 6.3, 6.4, 6.5, 6.10, 10.1, 10.2, 10.3, 10.4_

- [x] 2. Extend `NLSkillEntry` with knowledge type and outcome tracking fields
  - [x] 2.1 Add fields to `NLSkillEntry` in `corelib/types.go`
    - Add `Type string` field with json tag `"type,omitempty"` (values: `"executable"`, `"knowledge"`; default `"executable"`)
    - Add `FailureCount int` field with json tag `"failure_count"`
    - Add `WorkaroundCount int` field with json tag `"workaround_count"`
    - Add `Content string` field with json tag `"content,omitempty"` for knowledge skill Markdown content
    - Ensure backward compatibility: existing skills without `type` field default to `"executable"`
    - _Requirements: 1.1, 3.2_

  - [ ]* 2.2 Write unit tests for `NLSkillEntry` type field defaults
    - Test JSON unmarshal of skill without `type` field defaults to `"executable"`
    - Test JSON round-trip preserves `"knowledge"` type
    - Test `FailureCount` and `WorkaroundCount` serialize/deserialize correctly
    - _Requirements: 1.1, 3.2_

- [x] 3. Checkpoint — Ensure all tests pass
  - Ensure all tests pass, ask the user if questions arise.

- [x] 4. Implement knowledge skill detection in scanner (`corelib/skill/scanner.go`)
  - [x] 4.1 Add `KNOWLEDGE.md` file detection to skill directory loading
    - In `loadSkillFromDir` (or equivalent), check for `KNOWLEDGE.md` file in skill directory
    - When `KNOWLEDGE.md` exists and no executable steps are defined, set `Type = "knowledge"` and load file content into `Content` field
    - Support `type: knowledge` in YAML with `content` field — load Markdown content into `NLSkillEntry.Content`
    - Add `"type"` and `"content"` to `knownKeys` in `ParseSkillYAMLFile`
    - Preserve existing trigger mechanism for knowledge skills (same `triggers` field)
    - _Requirements: 1.2, 1.3, 1.4, 1.9_

  - [ ]* 4.2 Write unit tests for knowledge skill scanning
    - Test `KNOWLEDGE.md` detection sets type to `"knowledge"`
    - Test YAML `type: knowledge` with `content` field loads correctly
    - Test skill with both executable steps and `KNOWLEDGE.md` remains `"executable"`
    - Test triggers are preserved for knowledge skills
    - _Requirements: 1.2, 1.3, 1.4_

- [x] 5. Implement enhanced security scanning (`corelib/security/risk_assessor.go`)
  - [x] 5.1 Add 12 threat pattern categories to `risk_assessor.go`
    - Define threat pattern maps for: exfiltration, injection, destructive, persistence, network, obfuscation, execution, traversal, mining, supply_chain, privilege_escalation, credential_exposure
    - Each category contains regex or substring patterns to match against skill step commands/params
    - Add prompt injection detection patterns (instruction override, role-play injection)
    - Ensure existing `safeToolCategories` and `dangerousKeywords` remain backward compatible
    - _Requirements: 4.1, 4.2, 4.3, 4.7_

  - [x] 5.2 Add invisible Unicode character detection
    - Detect zero-width spaces (U+200B, U+200C, U+200D, U+FEFF)
    - Detect right-to-left overrides (U+202E, U+202D, U+202A, U+202B)
    - Detect homoglyph substitutions (Cyrillic lookalikes for Latin chars)
    - Add Unicode anomalies to risk assessment factors
    - _Requirements: 4.4_

  - [x] 5.3 Add structural checks for skill directories
    - Flag directories with more than 50 files
    - Flag total size exceeding 10MB
    - Detect presence of binary files (non-text content)
    - Detect symlinks pointing outside the skill directory
    - Escalate risk level by one step when structural anomalies found
    - _Requirements: 4.5, 4.6_

  - [x] 5.4 Implement 4-tier trust level hierarchy in `AssessSkill`
    - Refactor `AssessSkill` trust level handling from current `"official"`/`"unknown"` to 4-tier: `builtin` > `trusted` > `agent-created` > `community`
    - `builtin`: cap maximum risk at `low` regardless of pattern matches
    - `trusted`: cap maximum risk at `medium`
    - `agent-created`: standard assessment (no modification)
    - `community`: escalate assessed risk by one step (low→medium, medium→high, high→critical)
    - Maintain backward compatibility with existing `"official"` and `"unknown"` trust levels (map to `trusted` and `community` respectively)
    - _Requirements: 4.8, 4.9, 4.10_

  - [ ]* 5.5 Write unit tests for enhanced security scanning
    - Test each of the 12 threat categories triggers correct risk factors
    - Test invisible Unicode detection flags zero-width spaces and RTL overrides
    - Test structural checks escalate risk for oversized directories
    - Test trust level hierarchy: builtin caps at low, community escalates
    - Test backward compatibility: existing safe skills not flagged as false positives
    - _Requirements: 4.1, 4.2, 4.3, 4.4, 4.5, 4.6, 4.7, 4.8, 4.9, 4.10_

- [x] 6. Checkpoint — Ensure all tests pass
  - Ensure all tests pass, ask the user if questions arise.

- [x] 7. Implement skill patch action and audit trail
  - [x] 7.1 Update tool schema in `gui/im_tool_definitions.go`
    - Add `patch` action to `manage_skill` tool definition with `skill_name`, `find`, `replace`, and optional `reason` parameters
    - Add `history` action to `manage_skill` tool definition with `skill_name` parameter
    - Include behavioral nudge in `patch` action description: "If you used a skill and hit issues not covered by it, patch it immediately"
    - _Requirements: 2.1, 2.2, 2.8, 9.3_

  - [x] 7.2 Implement `manage_skill(action=patch)` handler in `gui/im_tools_misc.go`
    - Parse `skill_name`, `find`, `replace` parameters; return error if missing
    - Locate skill definition file (skill.yaml or skill.json) in skill directory
    - Read file content, count occurrences of `find` string
    - Zero matches → return error "no match found"
    - Multiple matches → return error "ambiguous match, provide more context"
    - Exactly one match → perform replacement
    - Validate modified content is valid YAML/JSON before saving
    - If invalid → reject patch, return validation error
    - Save using `fileutil.AtomicWriteFile`
    - Re-scan the modified skill directory to update in-memory registry
    - Append patch record to `.patches.json` audit trail
    - _Requirements: 2.1, 2.2, 2.3, 2.4, 2.5, 2.6, 2.7, 2.10, 2.11, 9.1, 9.2_

  - [x] 7.3 Implement `manage_skill(action=history)` handler in `gui/im_tools_misc.go`
    - Read `.patches.json` from skill directory
    - Return patch records in reverse chronological order
    - Each record contains: timestamp, find, replace, reason
    - Return empty list if no patches exist
    - _Requirements: 9.3, 9.4_

  - [x] 7.4 Implement TUI parity for patch and history in `tui/agent_tools.go`
    - Add `patch` and `history` action handling to TUI's `manage_skill` dispatcher
    - Reuse same logic: find-and-replace, validation, atomic write, audit trail
    - Ensure identical behavior to GUI implementation
    - _Requirements: 2.1, 2.2, 2.3, 2.4, 2.5, 2.6, 2.7, 2.10, 2.11, 9.1, 9.2, 9.3, 9.4_

  - [ ]* 7.5 Write unit tests for patch action
    - Test single match replacement succeeds
    - Test zero matches returns error
    - Test multiple matches returns ambiguous error
    - Test invalid YAML/JSON after patch is rejected
    - Test `.patches.json` audit trail is appended correctly
    - Test history action returns records in reverse chronological order
    - _Requirements: 2.3, 2.4, 2.5, 2.10, 2.11, 9.1, 9.2, 9.4_

- [x] 8. Implement skill execution outcome tracking
  - [x] 8.1 Add outcome recording to GUI skill runner (`gui/im_message_handler.go`)
    - After skill execution completes, record outcome as `success`, `failure`, or `workaround`
    - Increment `UsageCount` and corresponding `SuccessCount`/`FailureCount` on `NLSkillEntry`
    - Persist updated counts to skill metadata on disk
    - _Requirements: 3.1, 3.2_

  - [x] 8.2 Implement workaround detection in agent loop (`gui/im_message_handler.go`)
    - Track when a skill execution fails within the agent loop
    - If the LLM subsequently resolves the task through alternative tool calls in the same loop, classify as `workaround`
    - Increment `WorkaroundCount` on the skill entry
    - _Requirements: 3.3_

  - [x] 8.3 Add `needs_improvement` flag and success rate to skill list output
    - In `manage_skill(action=list)` handler (GUI and TUI), calculate failure rate over last 10 executions
    - When failure rate exceeds 30%, append `[needs_improvement]` flag to skill listing
    - Include success rate percentage and last error in list output
    - Display `[knowledge]` type indicator for knowledge skills
    - _Requirements: 3.4, 3.5, 1.10_

  - [x] 8.4 Add TUI parity for outcome tracking in `tui/agent_handler.go`
    - Mirror GUI outcome recording logic in TUI agent handler
    - Same workaround detection, same counter increments, same persistence
    - _Requirements: 3.1, 3.2, 3.3_

  - [ ]* 8.5 Write unit tests for outcome tracking
    - Test success increments `UsageCount` and `SuccessCount`
    - Test failure increments `FailureCount` and records `LastError`
    - Test workaround detection when skill fails then LLM succeeds with other tools
    - Test `needs_improvement` flag triggers at >30% failure rate
    - _Requirements: 3.1, 3.2, 3.3, 3.4, 3.5_

- [x] 9. Checkpoint — Ensure all tests pass
  - Ensure all tests pass, ask the user if questions arise.

- [x] 10. Implement memory frozen snapshot (`gui/im_system_prompt.go`)
  - [x] 10.1 Add frozen snapshot caching mechanism
    - Add `frozenMemorySnapshot` field (string) and `snapshotInitialized` flag to the system prompt builder (or relevant struct)
    - On first message of a session, generate the memory section via existing `appendMemorySection` logic and cache it
    - On subsequent system prompt constructions, use the cached snapshot instead of regenerating
    - Include all memory sources: project memory, proactive recall, entity supplementary recall
    - _Requirements: 5.1, 5.2, 5.8_

  - [x] 10.2 Implement `RefreshMemorySnapshot()` method
    - Expose a public `RefreshMemorySnapshot()` method that regenerates the cached snapshot from current persistent storage
    - Call `RefreshMemorySnapshot()` when user issues `/new` or starts a new topic
    - Call `RefreshMemorySnapshot()` on application restart (first message of new session)
    - _Requirements: 5.4, 5.5, 5.7_

  - [x] 10.3 Ensure live recall is not broken by frozen snapshot
    - Entity-based proactive recall queries must still execute against live persistent storage
    - Append live recall results after the frozen snapshot section
    - Mid-session `memory(action=save)` updates disk but does NOT invalidate the cached snapshot
    - _Requirements: 5.3, 5.6_

  - [ ]* 10.4 Write unit tests for memory frozen snapshot
    - Test snapshot is generated on first message and reused on subsequent messages
    - Test `RefreshMemorySnapshot()` regenerates from current storage
    - Test mid-session memory write does not invalidate snapshot
    - Test live recall still queries persistent storage
    - _Requirements: 5.1, 5.2, 5.3, 5.4, 5.5, 5.6, 5.7, 5.8_

- [x] 11. Implement knowledge skill system prompt injection (`gui/im_system_prompt.go`)
  - [x] 11.1 Add `## Procedural Knowledge (Skills)` section to system prompt builder
    - After the memory section and before tool definitions, check for matched knowledge skills
    - Match knowledge skills by comparing their `triggers` against the current user message
    - When multiple skills match, order by relevance score (trigger match quality)
    - Wrap each skill with `### Skill: {name}` heading and `---` separator
    - Omit the section entirely when no knowledge skills match
    - _Requirements: 1.5, 1.6, 8.1, 8.2, 8.3, 8.4_

  - [x] 11.2 Implement token budget enforcement for knowledge skill injection
    - Default combined budget: 2000 tokens for all injected knowledge skills
    - Make budget configurable via `config.json` (e.g. `knowledge_skill_token_budget`)
    - If a single skill's content exceeds per-skill budget, truncate at a smart boundary (paragraph or sentence break) and append `[truncated]` notice
    - _Requirements: 1.7, 1.8, 8.5_

  - [ ]* 11.3 Write unit tests for knowledge skill injection
    - Test matched skills are injected with correct section heading and separators
    - Test no section when no skills match
    - Test token budget truncation with truncation notice
    - Test multiple skills ordered by relevance
    - Test section placement: after memory, before tool definitions
    - _Requirements: 1.5, 1.6, 1.7, 1.8, 8.1, 8.2, 8.3, 8.4, 8.5_

- [x] 12. Checkpoint — Ensure all tests pass
  - Ensure all tests pass, ask the user if questions arise.

- [x] 13. Implement nudge system (`corelib/nudge/nudge.go`)
  - [x] 13.1 Create `corelib/nudge/nudge.go` with `NudgeTracker`
    - Define `NudgeTracker` struct with: session cooldown timer (10 min), dedup set (trigger event keys), iteration counter
    - Implement `ShouldNudge(event NudgeEvent) bool` — checks cooldown, dedup, iteration threshold (suppress below 3 iterations)
    - Implement `RecordNudge(event NudgeEvent)` — updates cooldown timer and dedup set
    - Implement `NudgeMessage(event NudgeEvent) string` — returns appropriate system message text based on event type
    - Define `NudgeEvent` type with variants: `ComplexTask` (≥5 tool calls), `SkillFailureWorkaround` (skill name), `UserCorrection`
    - Implement `IsDisabled(config map[string]interface{}) bool` — checks config for nudge disable flag
    - _Requirements: 7.1, 7.2, 7.3, 7.4, 7.5, 7.6, 7.7, 7.8, 7.9, 7.10_

  - [ ]* 13.2 Write unit tests for `NudgeTracker`
    - Test cooldown suppresses nudges within 10 minutes
    - Test dedup prevents same trigger event from firing twice in a session
    - Test iteration threshold suppresses nudges below 3 iterations
    - Test correct message text for each event type
    - Test disabled config skips all nudges
    - _Requirements: 7.1, 7.2, 7.3, 7.6, 7.7, 7.8, 7.10_

- [x] 14. Integrate nudge system into GUI agent loop (`gui/im_message_handler.go`)
  - [x] 14.1 Add nudge injection points to GUI agent loop
    - Initialize `NudgeTracker` per session
    - After agent loop completes with ≥5 tool calls, check `ShouldNudge(ComplexTask)` and inject system message
    - After skill failure + workaround detection, check `ShouldNudge(SkillFailureWorkaround)` and inject system message with skill name
    - After user correction detection (user message following failed tool call with correction keywords), check `ShouldNudge(UserCorrection)` and inject system message
    - Inject nudges as system messages appended after current response is delivered
    - Check config for nudge disable flag before any injection
    - _Requirements: 7.1, 7.2, 7.3, 7.4, 7.5, 7.9, 7.10_

  - [x] 14.2 Add nudge injection to TUI agent loop (`tui/agent_handler.go`)
    - Mirror GUI nudge injection logic in TUI agent handler
    - Use same `corelib/nudge.NudgeTracker` for identical behavior
    - Same trigger events, same cooldown, same dedup
    - _Requirements: 7.9_

  - [ ]* 14.3 Write integration tests for nudge injection
    - Test complex task nudge fires after ≥5 tool calls
    - Test skill failure workaround nudge includes skill name
    - Test nudge is injected as system message, not user-visible text
    - Test nudge not injected when disabled in config
    - _Requirements: 7.1, 7.2, 7.4, 7.10_

- [x] 15. Migrate critical file writes to `AtomicWriteFile`
  - [x] 15.1 Migrate config file writes
    - Replace `os.WriteFile` with `fileutil.AtomicWriteFile` in config file save paths (`corelib/configfile/` and related modules)
    - _Requirements: 6.6_

  - [x] 15.2 Migrate skill definition file writes
    - Replace `os.WriteFile` with `fileutil.AtomicWriteFile` in skill scanner write paths (`corelib/skill/scanner.go`, `tui/commands/skill.go`)
    - Also covers the new patch action's save path (already using `AtomicWriteFile` from task 7.2)
    - _Requirements: 6.7_

  - [x] 15.3 Migrate memory/recall data file writes
    - Replace `os.WriteFile` with `fileutil.AtomicWriteFile` in memory module write paths (`corelib/memory/` and related)
    - _Requirements: 6.8_

  - [x] 15.4 Migrate workflow state file writes
    - Replace `os.WriteFile` with `fileutil.AtomicWriteFile` in workflow state persistence paths (`corelib/workflow/`)
    - _Requirements: 6.9_

  - [ ]* 15.5 Write integration tests for atomic write migration
    - Test config save uses atomic write (verify no partial writes on simulated failure)
    - Test skill definition save uses atomic write
    - _Requirements: 6.6, 6.7, 6.8, 6.9_

- [x] 16. Final checkpoint — Ensure all tests pass
  - Ensure all tests pass, ask the user if questions arise.

## Notes

- Tasks marked with `*` are optional and can be skipped for faster MVP
- Each task references specific requirements for traceability
- Checkpoints ensure incremental validation
- The design has no Correctness Properties section, so unit tests and integration tests are used instead of property-based tests
- GUI/TUI parity is maintained by sharing core logic in `corelib/` packages (`nudge`, `fileutil`) and implementing parallel handlers in `gui/` and `tui/`
- The `corelib/fileutil/atomic.go` package is implemented first as it is a dependency for skill patch saves and the migration tasks
- Existing `UsageCount`, `SuccessCount`, `LastUsedAt`, and `LastError` fields on `NLSkillEntry` are already present — only `Type`, `Content`, `FailureCount`, and `WorkaroundCount` need to be added
