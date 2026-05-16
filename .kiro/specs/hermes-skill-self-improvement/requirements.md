# Requirements Document

## Introduction

This feature borrows six key design patterns from the Hermes Agent project into MacLaw's existing skill/tool system. The improvements span knowledge-type skills, skill self-improvement loops, enhanced security scanning, memory frozen snapshots, atomic file writes, and post-use skill nudges. All changes maintain GUI/TUI parity and backward compatibility with existing skills.

## Glossary

- **MacLaw**: The Go-based AI agent application with GUI (Wails) and TUI interfaces
- **Skill**: A reusable automation unit defined in YAML/JSON, containing executable steps (bash/craft_tool) or knowledge content
- **Knowledge_Skill**: A new skill type containing Markdown instructions injected into the LLM system prompt as procedural memory
- **Executable_Skill**: An existing skill type containing bash/craft_tool steps that automate multi-step workflows
- **Skill_Runner**: The engine (`gui/skill_runner.go`, `tui/agent_tools.go`) that executes skill steps
- **Skill_Scanner**: The module (`corelib/skill/scanner.go`) that discovers and parses skill definitions from disk
- **Risk_Assessor**: The security module (`corelib/security/risk_assessor.go`) that evaluates skill risk levels
- **System_Prompt_Builder**: The module (`gui/im_system_prompt.go`) that constructs the LLM system prompt including memory injection
- **Agent_Loop**: The main message handling loop (`gui/im_message_handler.go`, `tui/agent_handler.go`) that processes user messages and LLM responses
- **Usage_Tracker**: The module (`corelib/tool/usage_tracker.go`) that records tool invocation outcomes
- **NLSkillEntry**: The core Go struct representing a skill definition
- **Trust_Level**: A classification of skill origin trustworthiness: `builtin` > `trusted` > `agent-created` > `community`
- **Memory_Snapshot**: A cached copy of the memory section in the system prompt, frozen at session start
- **Atomic_Write**: A file write operation using temp-file + rename to prevent partial writes on crash
- **Nudge_Message**: A low-priority system message injected after certain events to encourage skill creation or improvement
- **Patch_Action**: A targeted find-and-replace operation on a skill definition file

## Requirements

### Requirement 1: Knowledge-Type Skill Definition

**User Story:** As a MacLaw user, I want to create skills that contain procedural knowledge (Markdown instructions) rather than executable steps, so that the LLM can learn domain-specific judgment, pitfall warnings, and verification procedures.

#### Acceptance Criteria

1. THE NLSkillEntry SHALL support a `type` field with values `executable` (default) and `knowledge`
2. WHEN a skill directory contains a `KNOWLEDGE.md` file and no executable steps, THE Skill_Scanner SHALL classify the skill as type `knowledge`
3. WHEN a skill YAML defines `type: knowledge` with a `content` field, THE Skill_Scanner SHALL load the Markdown content into the NLSkillEntry
4. THE Knowledge_Skill SHALL support `triggers` for conditional activation, matching the same trigger mechanism as Executable_Skills
5. WHEN a Knowledge_Skill's trigger conditions match the current user message, THE System_Prompt_Builder SHALL inject the skill's Markdown content into the LLM system prompt
6. WHEN multiple Knowledge_Skills match, THE System_Prompt_Builder SHALL inject all matching skills, ordered by relevance score
7. THE System_Prompt_Builder SHALL enforce a combined token budget for injected Knowledge_Skills to prevent system prompt bloat
8. IF a Knowledge_Skill's content exceeds the per-skill token budget, THEN THE System_Prompt_Builder SHALL truncate the content with a smart boundary truncation and append a truncation notice
9. THE Knowledge_Skill content SHALL support Markdown formatting including headings, lists, code blocks, and conditional logic sections
10. WHEN listing skills via `manage_skill(action=list)`, THE Agent_Loop SHALL display Knowledge_Skills with a `[knowledge]` type indicator distinct from executable skills

### Requirement 2: Skill Self-Improvement Loop (Patch Action)

**User Story:** As a MacLaw user, I want the LLM to be able to patch skill definitions after encountering issues, so that skills improve over time based on real-world usage.

#### Acceptance Criteria

1. THE `manage_skill` tool SHALL support a `patch` action that performs targeted find-and-replace within a skill's YAML/JSON definition file
2. WHEN `manage_skill(action=patch)` is called, THE Agent_Loop SHALL require `skill_name`, `find`, and `replace` parameters
3. WHEN the `find` string matches exactly one location in the skill file, THE Agent_Loop SHALL perform the replacement and save the updated file
4. IF the `find` string matches zero locations, THEN THE Agent_Loop SHALL return an error indicating no match found
5. IF the `find` string matches multiple locations, THEN THE Agent_Loop SHALL return an error indicating ambiguous match and request more context
6. WHEN a patch is applied, THE Agent_Loop SHALL re-scan the modified skill directory to update the in-memory skill registry
7. WHEN a patch is applied, THE Agent_Loop SHALL record the patch in the skill's metadata for audit trail purposes
8. THE `manage_skill(action=patch)` tool description SHALL include behavioral nudges: "If you used a skill and hit issues not covered by it, patch it immediately"
9. WHEN a skill execution fails and the LLM subsequently works around the issue manually, THE Agent_Loop SHALL inject a system message nudging the LLM to patch the skill
10. THE patch action SHALL validate that the modified skill file remains valid YAML/JSON before saving
11. IF the patched skill file is invalid, THEN THE Agent_Loop SHALL reject the patch and return the validation error

### Requirement 3: Skill Execution Outcome Tracking

**User Story:** As a MacLaw developer, I want to track skill execution outcomes (success/failure/workaround) so that the system can identify skills needing improvement.

#### Acceptance Criteria

1. WHEN a skill execution completes, THE Skill_Runner SHALL record the outcome as `success`, `failure`, or `workaround` in the NLSkillEntry metadata
2. THE NLSkillEntry SHALL maintain `UsageCount`, `SuccessCount`, `FailureCount`, and `WorkaroundCount` fields persisted to disk
3. WHEN a skill execution fails and the LLM resolves the task through alternative tool calls within the same agent loop, THE Agent_Loop SHALL classify the outcome as `workaround`
4. WHEN a skill's failure rate exceeds 30% over the last 10 executions, THE Agent_Loop SHALL flag the skill as `needs_improvement` in list output
5. THE `manage_skill(action=list)` output SHALL include success rate and last error for each skill

### Requirement 4: Enhanced Security Scanning — Threat Pattern Categories

**User Story:** As a MacLaw user, I want comprehensive security scanning of skill content to detect exfiltration, injection, destructive operations, and other threat categories, so that malicious skills are blocked before execution.

#### Acceptance Criteria

1. THE Risk_Assessor SHALL evaluate skill content against threat patterns in these categories: exfiltration, injection, destructive, persistence, network, obfuscation, execution, traversal, mining, supply_chain, privilege_escalation, credential_exposure
2. WHEN a skill step's command or parameters match a threat pattern, THE Risk_Assessor SHALL include the matched category and pattern in the risk assessment factors
3. THE Risk_Assessor SHALL detect prompt injection patterns in skill content, including instruction override attempts and role-play injection
4. THE Risk_Assessor SHALL detect invisible Unicode characters (zero-width spaces, right-to-left overrides, homoglyph substitutions) in skill content
5. THE Risk_Assessor SHALL perform structural checks on skill directories: flag directories with more than 50 files, total size exceeding 10MB, presence of binary files, or symlinks pointing outside the skill directory
6. WHEN structural checks detect anomalies, THE Risk_Assessor SHALL escalate the risk level by one step and include the anomaly in factors
7. THE enhanced threat patterns SHALL NOT trigger false positives on existing safe skills (backward compatible with current `safeToolCategories`)
8. THE Risk_Assessor SHALL support a trust level hierarchy: `builtin` (risk capped at low) > `trusted` (risk capped at medium) > `agent-created` (standard assessment) > `community` (risk escalated by one step)
9. WHEN a skill's trust level is `builtin`, THE Risk_Assessor SHALL cap the maximum risk at `low` regardless of pattern matches
10. WHEN a skill's trust level is `community`, THE Risk_Assessor SHALL escalate the assessed risk by one step (low→medium, medium→high, high→critical)

### Requirement 5: Memory Frozen Snapshot

**User Story:** As a MacLaw developer, I want the memory section of the system prompt to be cached at session start and remain stable during the session, so that mid-session memory writes do not invalidate the LLM's KV cache prefix.

#### Acceptance Criteria

1. WHEN the first message of a session is processed, THE System_Prompt_Builder SHALL generate the memory section and cache it as a frozen snapshot
2. WHILE a session is active, THE System_Prompt_Builder SHALL use the cached memory snapshot for all subsequent system prompt constructions
3. WHEN a memory write occurs mid-session (via `memory(action=save)`), THE memory tool SHALL update persistent storage on disk but SHALL NOT invalidate the cached snapshot
4. WHEN the user issues `/new` or starts a new topic, THE System_Prompt_Builder SHALL call `RefreshMemorySnapshot()` to regenerate the cached snapshot from current persistent storage
5. THE System_Prompt_Builder SHALL expose a `RefreshMemorySnapshot()` method that can be called explicitly to force a snapshot refresh
6. THE frozen snapshot mechanism SHALL NOT break proactive recall — entity-based recall queries SHALL still execute against the live persistent storage, with results appended after the frozen section
7. WHEN the application restarts, THE System_Prompt_Builder SHALL generate a fresh snapshot on the first message of the new session
8. THE frozen snapshot SHALL include all memory sources that `appendMemorySection` currently injects: project memory, proactive recall, entity supplementary recall

### Requirement 6: Atomic File Writes

**User Story:** As a MacLaw user, I want critical file writes (config, skill definitions, memory data) to be atomic, so that a crash during write never leaves a half-written file.

#### Acceptance Criteria

1. THE MacLaw codebase SHALL provide an `AtomicWriteFile(path string, data []byte, perm os.FileMode) error` function in a new `corelib/fileutil/atomic.go` package
2. THE `AtomicWriteFile` function SHALL write data to a temporary file in the same directory as the target, then rename the temporary file to the target path
3. WHEN the rename succeeds, THE `AtomicWriteFile` function SHALL return nil
4. IF the temporary file write fails, THEN THE `AtomicWriteFile` function SHALL clean up the temporary file and return the error
5. THE `AtomicWriteFile` function SHALL work correctly on Windows (NTFS same-volume rename is atomic) and Unix systems
6. WHEN writing config.json, THE config file module SHALL use `AtomicWriteFile` instead of `os.WriteFile`
7. WHEN writing skill definition files (skill.yaml, skill.json), THE Skill_Scanner SHALL use `AtomicWriteFile` instead of `os.WriteFile`
8. WHEN writing memory/recall data files, THE memory module SHALL use `AtomicWriteFile` instead of `os.WriteFile`
9. WHEN writing workflow state files, THE workflow module SHALL use `AtomicWriteFile` instead of `os.WriteFile`
10. THE `AtomicWriteFile` function SHALL preserve the original file's permissions when the target file already exists

### Requirement 7: Post-Use Skill Nudge System

**User Story:** As a MacLaw user, I want the system to suggest creating or improving skills after complex tasks, failed skill executions, or user corrections, so that the skill library grows organically from real usage.

#### Acceptance Criteria

1. WHEN an agent loop completes with 5 or more tool calls, THE Agent_Loop SHALL inject a low-priority system message: "This was a complex task. Consider saving the approach as a skill for future reuse."
2. WHEN a skill execution fails and the LLM subsequently resolves the task through manual workaround, THE Agent_Loop SHALL inject a system message: "The skill '{name}' didn't cover this scenario. Consider patching it with manage_skill(action=patch)."
3. WHEN the user explicitly corrects the LLM's approach (detected by user message following a failed tool call containing correction keywords), THE Agent_Loop SHALL inject a system message: "The user corrected your approach. Consider saving this as a memory entry or skill."
4. THE nudge messages SHALL be injected as system messages in the conversation, not as visible text to the user
5. THE nudge messages SHALL NOT interrupt the current task flow — they SHALL be appended after the current response is delivered
6. THE Agent_Loop SHALL NOT inject duplicate nudge messages within the same session for the same trigger event
7. WHILE the agent loop iteration count is below 3, THE Agent_Loop SHALL suppress nudge injection to avoid premature suggestions
8. THE nudge system SHALL respect a per-session cooldown of 10 minutes between nudge injections to avoid nudge fatigue
9. THE nudge system SHALL maintain GUI/TUI parity — both interfaces SHALL implement identical nudge logic
10. WHEN the user has disabled nudges via configuration, THE Agent_Loop SHALL skip all nudge injections

### Requirement 8: Knowledge Skill System Prompt Injection

**User Story:** As a MacLaw developer, I want Knowledge_Skills to be injected into the system prompt with clear section boundaries, so that the LLM can distinguish procedural knowledge from other system prompt content.

#### Acceptance Criteria

1. THE System_Prompt_Builder SHALL inject matched Knowledge_Skills in a dedicated `## Procedural Knowledge (Skills)` section
2. EACH injected Knowledge_Skill SHALL be wrapped with `### Skill: {name}` heading and `---` separator
3. THE injected section SHALL appear after the memory section and before the tool definitions in the system prompt
4. WHEN no Knowledge_Skills match, THE System_Prompt_Builder SHALL omit the procedural knowledge section entirely
5. THE combined token budget for all injected Knowledge_Skills SHALL default to 2000 tokens, configurable via `config.json`

### Requirement 9: Skill Patch Audit Trail

**User Story:** As a MacLaw user, I want to see the history of patches applied to a skill, so that I can understand how a skill evolved and revert changes if needed.

#### Acceptance Criteria

1. WHEN a patch is applied via `manage_skill(action=patch)`, THE Agent_Loop SHALL append a patch record to a `.patches.json` file in the skill directory
2. EACH patch record SHALL contain: timestamp, find string, replace string, and reason (if provided by the LLM)
3. THE `manage_skill` tool SHALL support a `history` action that returns the patch history for a given skill
4. WHEN `manage_skill(action=history)` is called with a skill name, THE Agent_Loop SHALL return the list of patches in reverse chronological order

### Requirement 10: Atomic Write Cross-Platform Compatibility

**User Story:** As a MacLaw developer, I want atomic writes to handle platform-specific edge cases, so that the feature works reliably on Windows, macOS, and Linux.

#### Acceptance Criteria

1. WHEN the target file is on a different volume than the temporary file on Windows, THE `AtomicWriteFile` function SHALL fall back to copy-and-rename strategy
2. IF `os.Rename` fails with a cross-device error, THEN THE `AtomicWriteFile` function SHALL copy the temporary file to the target path and remove the temporary file
3. THE `AtomicWriteFile` function SHALL use `os.CreateTemp` with a prefix in the same directory as the target to ensure same-volume placement
4. THE temporary file name SHALL use a `.tmp` suffix to be identifiable for cleanup in case of incomplete operations
