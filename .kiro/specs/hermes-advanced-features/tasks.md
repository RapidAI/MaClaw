# Implementation Plan: Hermes Advanced Features

## Overview

This plan implements seven Hermes Agent design patterns in MacLaw's Go codebase: cross-session FTS5 search, intelligent context compression, conditional skill activation, credential file mounting, plugin namespacing, platform filtering, and dialectic user modeling. Tasks are ordered by dependency: foundation type extensions first, then independent subsystems, then GUI/TUI integration, then property-based tests.

All code is Go. Property-based tests use `pgregory.net/rapid` (already in go.mod). SQLite uses `modernc.org/sqlite` (already in go.mod).

## Tasks

- [x] 1. Foundation: NLSkillEntry and SkillYAMLFile type extensions
  - [x] 1.1 Add condition and credential fields to NLSkillEntry
    - Add `RequiresTools`, `FallbackForTools`, `RequiresToolsets`, `FallbackForToolsets` (all `[]string`) to `NLSkillEntry` in `corelib/types.go`
    - Add `RequiredCredentialFiles []string` to `NLSkillEntry`
    - Add `Publisher string` to `NLSkillEntry`
    - All fields use `json:"...,omitempty"` tags
    - _Requirements: 3.1, 4.1, 5.1_

  - [x] 1.2 Add condition and credential fields to SkillYAMLFile
    - Add `RequiresTools`, `FallbackForTools`, `RequiresToolsets`, `FallbackForToolsets` (all `[]string`) to `SkillYAMLFile` in `corelib/skill/scanner.go`
    - Add `RequiredCredentialFiles []string` to `SkillYAMLFile`
    - Add all six new field names to `knownKeys` in `ParseSkillYAMLFile`
    - _Requirements: 3.6, 3.10, 4.2, 4.9_

  - [x] 1.3 Wire new YAML fields to NLSkillEntry in scanner
    - In `loadSkillFromDir` (or equivalent), copy parsed YAML condition fields, `RequiredCredentialFiles`, and any publisher metadata to the `NLSkillEntry` struct
    - _Requirements: 3.6, 4.2_

  - [ ]* 1.4 Write property test for YAML condition field parsing round-trip (Property 6)
    - **Property 6: YAML condition field parsing round-trip**
    - Generate random valid YAML with the four condition fields and `required_credential_files`
    - Parse via `ParseSkillYAMLFile`, verify all fields populated with exact values
    - Test file: `corelib/skill/scanner_prop_test.go`
    - **Validates: Requirements 3.6, 4.2**

- [x] 2. Extend MatchesName for qualified namespace resolution
  - [x] 2.1 Implement qualified name matching in MatchesName
    - Extend `MatchesName` in `corelib/types.go` to match `publisher:name` format
    - When `Publisher` is set, match against `Publisher + ":" + Name`
    - Continue matching bare name, DirName, and SkillDir basename
    - _Requirements: 5.6, 5.8, 5.9_

  - [ ]* 2.2 Write property test for qualified name resolution (Property 9)
    - **Property 9: Qualified name resolution with precedence**
    - Generate random skills with/without Publisher, test all match paths
    - Test collision scenarios: two skills with same bare name, different publishers
    - Test file: `corelib/types_prop_test.go`
    - **Validates: Requirements 5.6, 5.7, 5.8, 5.9**

- [x] 3. Checkpoint — Ensure all tests pass
  - Ensure all tests pass, ask the user if questions arise.

- [x] 4. Platform filtering at scan time
  - [x] 4.1 Implement platform filtering in ScanSkillDir
    - In `corelib/skill/scanner.go`, after loading a skill, check its `Platforms` field against `runtime.GOOS`
    - Map `runtime.GOOS`: `"darwin"` → `"macos"`, `"windows"` → `"windows"`, `"linux"` → `"linux"`
    - If `Platforms` is non-empty and does not contain the current platform, skip the skill (do not add to results)
    - If `Platforms` is empty, include the skill (backward compatible)
    - _Requirements: 6.1, 6.2, 6.3, 6.4, 6.6, 6.7_

  - [ ]* 4.2 Write property test for platform filtering (Property 10)
    - **Property 10: Platform filtering excludes incompatible skills**
    - Generate random skills with random platform sets and random target OS values
    - Verify inclusion/exclusion logic matches the specification
    - Test file: `corelib/skill/scanner_prop_test.go`
    - **Validates: Requirements 6.2, 6.3**

- [x] 5. Skill conditional activation in tool router
  - [x] 5.1 Define ToolsetGroups map
    - In `corelib/tool/router.go` (or a new `corelib/tool/toolsets.go`), define `ToolsetGroups` mapping toolset names to constituent tool names
    - Include `"coding"`, `"browser"`, `"ssh"` groups as specified in design
    - _Requirements: 3.4, 3.5_

  - [x] 5.2 Implement tool availability condition evaluation
    - Add a function `EvaluateToolConditions(skill *corelib.NLSkillEntry, availableTools map[string]bool) bool`
    - Implement AND logic: all `requires_tools` must be available, all `fallback_for_tools` must be unavailable, all `requires_toolsets` must be available, all `fallback_for_toolsets` must be unavailable
    - Skills with no conditions return true (backward compatible)
    - Expand toolset names to individual tool names via `ToolsetGroups`
    - _Requirements: 3.2, 3.3, 3.4, 3.5, 3.7, 3.8, 3.9_

  - [x] 5.3 Integrate condition evaluation into Route()
    - In `Route()`, after keyword/BM25 scoring, filter candidate skills through `EvaluateToolConditions`
    - Build `availableTools` set from the current tool list
    - _Requirements: 3.7, 3.9_

  - [ ]* 5.4 Write property test for skill activation logic (Property 5)
    - **Property 5: Skill activation respects tool availability conditions (AND logic)**
    - Generate random skills with random condition fields and random tool availability states
    - Verify activation matches the AND-logic specification exactly
    - Test file: `corelib/tool/router_conditions_prop_test.go`
    - **Validates: Requirements 3.2, 3.3, 3.4, 3.5, 3.7, 3.8**

- [x] 6. Checkpoint — Ensure all tests pass
  - Ensure all tests pass, ask the user if questions arise.

- [x] 7. Session transcript serializer
  - [x] 7.1 Implement Serialize and Deserialize in corelib/session/serializer.go
    - Create `corelib/session/serializer.go`
    - Define `TranscriptEntry` and `ToolCallMeta` structs as specified in design
    - Implement `Serialize([]TranscriptEntry) string` using the deterministic format from the design (role markers, `---` separators, tool call metadata)
    - Implement `Deserialize(string) ([]TranscriptEntry, error)` to reconstruct the original structure
    - Preserve message roles, content, and tool call metadata
    - _Requirements: 12.1, 12.2, 12.3, 12.4_

  - [ ]* 7.2 Write property test for transcript serialization round-trip (Property 1)
    - **Property 1: Transcript serialization round-trip**
    - Generate random `[]TranscriptEntry` with varied roles, content lengths, tool calls
    - Verify `Deserialize(Serialize(entries))` produces equivalent structure
    - Test file: `corelib/session/serializer_prop_test.go`
    - **Validates: Requirements 12.1, 12.2, 12.3, 12.4**

- [x] 8. Session search store (SQLite FTS5)
  - [x] 8.1 Implement Store with FTS5 index in corelib/session/store.go
    - Create `corelib/session/store.go`
    - Define `Store`, `SessionDocument`, `SearchResult` structs as specified in design
    - Implement `NewStore(dbPath)` — auto-create DB and FTS5 schema if not exists
    - Implement `Persist(SessionDocument)` — insert into sessions table with FTS5 triggers
    - Implement `Search(query, maxResults)` — FTS5 MATCH query with BM25 ranking, return snippets
    - Implement `Prune(olderThan)` — delete sessions older than threshold
    - Implement `Close()`
    - Use `unicode61` tokenizer for CJK support
    - Extract topic from first user message (first 100 characters)
    - Return "no results found" message for empty results
    - _Requirements: 1.2, 1.3, 1.5, 1.7, 1.8, 1.10, 1.11, 8.1, 8.2, 8.3, 8.4, 8.5_

  - [ ]* 8.2 Write property test for FTS5 search correctness (Property 2)
    - **Property 2: FTS5 search returns only matching sessions**
    - Generate random session documents, index them, search with substring queries
    - Verify results contain only matching sessions and count ≤ maxResults
    - Test file: `corelib/session/store_prop_test.go`
    - **Validates: Requirements 1.2, 1.7, 1.11**

  - [ ]* 8.3 Write property test for session topic extraction (Property 15)
    - **Property 15: Session topic extraction**
    - Generate random conversation histories, verify topic = first min(L, 100) chars of first user message
    - Test file: `corelib/session/store_prop_test.go`
    - **Validates: Requirements 8.4**

  - [ ]* 8.4 Write property test for session pruning (Property 16)
    - **Property 16: Session pruning removes only expired sessions**
    - Generate random session sets with varied timestamps, prune with threshold T
    - Verify exactly the expired sessions are removed
    - Test file: `corelib/session/store_prop_test.go`
    - **Validates: Requirements 8.5**

- [x] 9. Checkpoint — Ensure all tests pass
  - Ensure all tests pass, ask the user if questions arise.

- [x] 10. Context compressor
  - [x] 10.1 Implement EstimateTokens in corelib/context/compressor.go
    - Create `corelib/context/` package and `compressor.go`
    - Implement `EstimateTokens(text string) int` using character-based heuristic: `ceil(asciiChars/4) + ceil(cjkChars/1.5)`
    - Detect CJK characters via Unicode range checks
    - _Requirements: 9.1_

  - [x] 10.2 Implement Compressor with ShouldCompress and Compress
    - Define `CompressConfig`, `CompressResult`, `Compressor` structs as specified in design
    - Implement `NewCompressor(config, summarize)` with LLM callback
    - Implement `ShouldCompress(messages)` — estimate total tokens, compare against threshold
    - Implement `Compress(messages)` — preserve last `ProtectedTurns`, summarize older content via LLM callback, insert `[compressed]` marker with token ratio
    - Implement fallback: if LLM summarization fails, truncate oldest messages
    - Maintain chronological order of all messages
    - _Requirements: 2.1, 2.2, 2.3, 2.4, 2.5, 2.6, 2.8, 2.9, 2.10, 2.12_

  - [ ]* 10.3 Write property test for token estimation (Property 3)
    - **Property 3: Token estimation follows heuristic formula**
    - Generate random mixed ASCII/CJK strings
    - Verify `EstimateTokens(text)` equals `ceil(asciiChars/4) + ceil(cjkChars/1.5)` (±1 rounding)
    - Test file: `corelib/context/compressor_prop_test.go`
    - **Validates: Requirements 9.1**

  - [ ]* 10.4 Write property test for compression invariants (Property 4)
    - **Property 4: Compression preserves recent turns and chronological order**
    - Generate random conversation histories exceeding threshold
    - Verify: last 5 turns unchanged, chronological order maintained, compressed marker present with ratio
    - Test file: `corelib/context/compressor_prop_test.go`
    - **Validates: Requirements 2.8, 2.9, 2.12**

- [x] 11. User model with dialectic reconciliation
  - [x] 11.1 Implement Profile and Model in corelib/user/model.go
    - Create `corelib/user/` package and `model.go`
    - Define `Dimension`, `Evidence`, `Profile`, `Model` structs as specified in design
    - Implement `NewModel(filePath)` — load from JSON or initialize empty
    - Implement `GetProfile()` — return snapshot under read lock
    - Implement `UpdateDimension(dimension, newValue, evidence)` — dialectic reconciliation: if new value contradicts existing, lower confidence and record both evidences
    - Implement `CorrectDimension(dimension, value)` — set confidence to 1.0, mark UserConfirmed
    - Implement `ResetDimension(dimension)` — clear to empty state
    - Implement `Save()` — persist to JSON file
    - Implement `FormatForPrompt()` — format profile for system prompt injection
    - Use `sync.RWMutex` for thread safety
    - _Requirements: 7.1, 7.2, 7.3, 7.4, 7.5, 7.8, 7.9, 7.10, 7.12_

  - [ ]* 11.2 Write property test for dialectic reconciliation (Property 11)
    - **Property 11: Dialectic reconciliation lowers confidence on contradiction**
    - Generate random dimensions with existing values and confidence > 0
    - Call `UpdateDimension` with contradicting value
    - Verify confidence is strictly lower and both evidences recorded
    - Test file: `corelib/user/model_prop_test.go`
    - **Validates: Requirements 7.4, 7.5**

  - [ ]* 11.3 Write property test for user correction (Property 12)
    - **Property 12: User correction sets confidence to 1.0**
    - Generate random dimensions with various confidence values
    - Call `CorrectDimension`, verify confidence == 1.0 and UserConfirmed == true
    - Test file: `corelib/user/model_prop_test.go`
    - **Validates: Requirements 7.9**

  - [ ]* 11.4 Write property test for profile JSON persistence round-trip (Property 13)
    - **Property 13: User profile JSON persistence round-trip**
    - Generate random Profile structs, save to temp file, load back
    - Verify all dimension values, confidence scores, and evidence lists preserved
    - Test file: `corelib/user/model_prop_test.go`
    - **Validates: Requirements 7.10**

- [x] 12. Evidence collector
  - [x] 12.1 Implement Collector in corelib/user/evidence.go
    - Create `corelib/user/evidence.go`
    - Define `Collector` struct with `model`, `batchQueue`, `updateCounts`, `mu`
    - Implement `NewCollector(model)` — bind to user model
    - Implement `Analyze(userMessage)` — pattern matching for common signals (programming language mentions, tool preferences, expertise indicators), queue ambiguous observations for batch LLM
    - Implement `FlushBatch(summarize)` — process queued observations via LLM callback (every 10 turns or session end)
    - Implement `ResetSession()` — reset per-session rate limits
    - Rate-limit: at most one update per dimension per session
    - _Requirements: 11.1, 11.2, 11.3, 11.4, 11.5_

  - [ ]* 12.2 Write property test for evidence collection rate limiting (Property 14)
    - **Property 14: Evidence collection rate limiting**
    - Generate N > 1 signals for the same dimension in one session
    - Verify at most one profile update applied per dimension
    - Test file: `corelib/user/evidence_prop_test.go`
    - **Validates: Requirements 11.5**

- [x] 13. Checkpoint — Ensure all tests pass
  - Ensure all tests pass, ask the user if questions arise.

- [x] 14. Credential file mounting
  - [x] 14.1 Implement credential mount in corelib/remote/credential_mount.go
    - Create `corelib/remote/credential_mount.go`
    - Define `CredentialMounter` struct with `sshMgr` field
    - Implement `ExpandCredentialPath(path)` — expand `~` to home dir, expand `$HOME`/`%USERPROFILE%` env vars, cross-platform
    - Implement `ValidateCredentialFiles(files)` — expand each path, check existence, return list of missing files
    - Implement `MountCredentials(sessionID, files)` — SCP upload to remote host, set 0600 permissions, return cleanup function that deletes uploaded files
    - Cleanup function must execute regardless of skill execution outcome (deferred)
    - _Requirements: 4.1, 4.3, 4.5, 4.6, 4.7, 4.10, 10.1, 10.2, 10.3_

  - [ ]* 14.2 Write property test for credential file validation (Property 7)
    - **Property 7: Credential file validation detects all missing files**
    - Generate random file paths, create some as temp files, validate
    - Verify `ValidateCredentialFiles` returns exactly the missing subset
    - Test file: `corelib/remote/credential_mount_prop_test.go`
    - **Validates: Requirements 4.3, 10.1, 10.2, 10.3**

  - [ ]* 14.3 Write property test for credential cleanup (Property 8)
    - **Property 8: Credential cleanup always executes**
    - Simulate mount operations with various execution outcomes (success/failure/panic)
    - Verify cleanup function is always called
    - Test file: `corelib/remote/credential_mount_prop_test.go`
    - **Validates: Requirements 4.6**

- [x] 15. Checkpoint — Ensure all tests pass
  - Ensure all tests pass, ask the user if questions arise.

- [x] 16. GUI integration
  - [x] 16.1 Implement session_search tool handler in GUI
    - Create `gui/im_tools_session_search.go`
    - Register `session_search` tool with query parameter and maxResults (default 10)
    - Call `session.Store.Search()`, optionally summarize results via LLM
    - Return ranked results with snippets, timestamps, topics, platform
    - Return "no results found" for empty results
    - _Requirements: 1.1, 1.2, 1.3, 1.6, 1.10, 1.11_

  - [x] 16.2 Implement session persistence in GUI agent loop
    - In `gui/im_message_handler.go`, after conversation history is saved, persist transcript to FTS5 store
    - Use `session.Serialize()` to convert conversation to searchable text
    - Extract topic from first user message (first 100 chars)
    - _Requirements: 1.4, 1.9, 1.12_

  - [x] 16.3 Implement /compress command and auto-compression in GUI
    - In `gui/im_message_handler.go`, add `/compress` command handler
    - Before each LLM call, check `compressor.ShouldCompress(messages)` and auto-compress if needed
    - Use `MaclawLLMConfig.EffectiveContextTokens()` for threshold calculation
    - _Requirements: 2.2, 2.7, 2.11, 9.2, 9.3_

  - [x] 16.4 Implement manage_user_model tool handler in GUI
    - Create `gui/im_tools_user_model.go`
    - Register `manage_user_model` tool with actions: view, correct, reset
    - Wire to `user.Model` methods
    - _Requirements: 7.8_

  - [x] 16.5 Inject user profile into system prompt in GUI
    - In `gui/im_system_prompt.go`, call `model.FormatForPrompt()` and inject as `## User Profile` section
    - _Requirements: 7.6_

  - [x] 16.6 Implement evidence collection hook in GUI agent loop
    - In `gui/im_message_handler.go`, after each agent turn, launch goroutine calling `collector.Analyze(userMessage)`
    - Flush batch every 10 turns or at session end
    - Must not block the agent loop response
    - _Requirements: 7.7, 11.2_

  - [x] 16.7 Implement credential pre-check in GUI skill runner
    - In `gui/skill_runner.go`, before skill execution, call `ValidateCredentialFiles` on skill's `RequiredCredentialFiles`
    - If any missing, return `setup_needed` status with guidance
    - For SSH execution, call `MountCredentials` and defer cleanup
    - Do not log full credential file contents or paths
    - _Requirements: 4.3, 4.4, 4.5, 4.6, 4.7, 4.8_

  - [x] 16.8 Implement namespace resolution and bundle context banner in GUI
    - In `gui/skill_runner.go`, use extended `MatchesName` for namespace resolution
    - In `gui/im_system_prompt.go`, inject bundle context banner when loading a namespaced skill: "This skill is part of the '{publisher}' bundle. Related skills: {sibling list}"
    - In `manage_skill(action=list)`, group skills by namespace
    - Handle bare name collision: require qualified name, display warning
    - _Requirements: 5.2, 5.3, 5.4, 5.5, 5.6, 5.7, 5.10_

  - [x] 16.9 Implement platform filtering display in GUI
    - In `manage_skill(action=list)`, only show skills compatible with current platform
    - Platform filtering already happens at scan time (task 4.1), so this is verification
    - _Requirements: 6.5_

- [x] 17. TUI integration
  - [x] 17.1 Implement session_search and manage_user_model tool handlers in TUI
    - In `tui/agent_tools.go`, add `session_search` and `manage_user_model` tool dispatch
    - Wire to same `session.Store` and `user.Model` instances as GUI
    - _Requirements: 1.1, 1.12, 7.8, 7.11_

  - [x] 17.2 Implement session persistence and compression in TUI
    - In `tui/agent_handler.go`, persist session transcripts to FTS5 store after conversation save
    - Add `/compress` command and auto-compression trigger
    - _Requirements: 1.4, 1.12, 2.7, 2.11_

  - [x] 17.3 Implement evidence collection hook in TUI
    - In `tui/agent_handler.go`, launch async evidence collection after each turn
    - Inject user profile into system prompt
    - _Requirements: 7.7, 7.11, 11.2_

  - [x] 17.4 Implement credential pre-check in TUI skill runner
    - In `tui/agent_tools.go`, add credential validation before skill execution
    - For SSH execution, call `MountCredentials` and defer cleanup
    - _Requirements: 4.3, 4.4, 4.8_

  - [x] 17.5 Implement namespace resolution and platform filtering in TUI
    - In `tui/agent_tools.go`, use extended `MatchesName` for skill lookup
    - Group skills by namespace in list display
    - Platform filtering already handled at scan time
    - _Requirements: 5.4, 5.10, 6.5_

- [x] 18. Final checkpoint — Ensure all tests pass
  - Ensure all tests pass, ask the user if questions arise.

## Notes

- Tasks marked with `*` are optional and can be skipped for faster MVP
- Each task references specific requirements for traceability
- Checkpoints ensure incremental validation after each major subsystem
- Property tests validate the 16 correctness properties defined in the design document using `pgregory.net/rapid`
- Unit tests validate specific examples and edge cases
- All corelib packages are shared between GUI and TUI — GUI/TUI tasks wire the shared logic to their respective interfaces
- The `Platforms` field already exists on `NLSkillEntry` and `SkillYAMLFile` — task 4.1 adds the filtering logic
- SQLite FTS5 is available via the existing `modernc.org/sqlite` dependency
