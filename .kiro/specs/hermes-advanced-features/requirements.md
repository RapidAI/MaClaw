# Requirements Document

## Introduction

This feature ports seven additional design patterns from the Hermes Agent project into MacLaw's Go codebase. The improvements span cross-session search, intelligent context compression, conditional skill activation based on tool availability, credential file mounting for remote execution, plugin skill namespacing, platform-based skill filtering, and dialectic user modeling. All changes maintain GUI/TUI parity and backward compatibility with existing skills and tools.

This is the second batch of Hermes Agent improvements. The first batch (hermes-skill-self-improvement) covered knowledge skills, skill patching, security scanning, memory snapshots, atomic writes, and nudges.

## Glossary

- **MacLaw**: The Go-based AI agent application with GUI (Wails) and TUI interfaces
- **Session_Transcript**: A stored record of a complete conversation session, including user messages, assistant responses, and tool call results
- **Session_Search_Tool**: A new tool that performs full-text search across all historical Session_Transcripts
- **FTS5_Index**: An SQLite FTS5 (Full-Text Search version 5) virtual table used for fast full-text search across session transcripts
- **Context_Compressor**: A module that intelligently compresses conversation history when approaching context window limits
- **Compression_Threshold**: The percentage of the model's context window (80%) at which automatic compression triggers
- **Compressed_Marker**: A `[compressed]` text marker inserted into conversation history where compression occurred
- **Skill_Scanner**: The module (`corelib/skill/scanner.go`) that discovers and parses skill definitions from disk
- **Tool_Router**: The module (`corelib/tool/router.go`) that selects relevant tools for each user message
- **NLSkillEntry**: The core Go struct representing a skill definition
- **Skill_Runner**: The engine (`gui/skill_runner.go`, `tui/agent_tools.go`) that executes skill steps
- **System_Prompt_Builder**: The module (`gui/im_system_prompt.go`) that constructs the LLM system prompt
- **Agent_Loop**: The main message handling loop (`gui/im_message_handler.go`, `tui/agent_handler.go`)
- **Credential_File**: A local file containing authentication credentials (e.g., `~/.kube/config`, `~/.aws/credentials`)
- **SSH_Background_Task**: The module (`corelib/remote/ssh_background_task.go`) that executes commands on remote hosts
- **Plugin_Namespace**: A qualified naming scheme using `publisher:skill-name` format to prevent name collisions
- **Bundle_Context_Banner**: A system message injected when a namespaced skill is loaded, listing sibling skills from the same publisher
- **User_Model**: A structured, evolving profile of the user maintained across sessions with confidence-scored dimensions
- **Dialectic_Reconciliation**: A thesis-antithesis-synthesis pattern for updating user profile dimensions when new evidence contradicts existing beliefs
- **Confidence_Score**: A floating-point value in [0, 1] representing certainty about a user profile dimension

## Requirements

### Requirement 1: Session Search — FTS5 Full-Text Search Across Historical Conversations

**User Story:** As a MacLaw user, I want to search across all my past conversation sessions, so that I can find previously discussed approaches, decisions, and solutions without relying on explicitly saved memory entries.

#### Acceptance Criteria

1. THE MacLaw codebase SHALL provide a `session_search` tool accessible from both GUI and TUI interfaces
2. WHEN the `session_search` tool is called with a query string, THE Session_Search_Tool SHALL perform full-text search across all stored Session_Transcripts using the FTS5_Index
3. THE Session_Search_Tool SHALL return ranked results containing: matching text snippets, session timestamp, session topic (if available), and platform identifier
4. WHEN a session completes (conversation history is saved), THE Agent_Loop SHALL persist the Session_Transcript to the searchable FTS5_Index store
5. THE FTS5_Index SHALL be implemented using SQLite FTS5 virtual tables for fast full-text search with relevance ranking
6. WHEN search results are returned, THE Session_Search_Tool SHALL use an LLM summarization call to extract and present the most relevant context from matched sessions
7. THE Session_Search_Tool SHALL support both Chinese and English text search without requiring language-specific configuration
8. IF the FTS5_Index database does not exist on first use, THEN THE Session_Search_Tool SHALL create the database and index schema automatically
9. THE session search storage SHALL work with existing conversation persistence (no migration of historical data required); new sessions are indexed going forward
10. WHEN no results match the query, THE Session_Search_Tool SHALL return a clear "no results found" message rather than an empty response
11. THE Session_Search_Tool SHALL cap returned results to a configurable maximum (default 10) to prevent excessive LLM summarization cost
12. THE session transcript persistence SHALL maintain GUI/TUI parity — both interfaces SHALL write transcripts to the same FTS5_Index store

### Requirement 2: Context Compressor — Intelligent Conversation History Compression

**User Story:** As a MacLaw user, I want long conversations to be intelligently compressed rather than truncated, so that critical decisions, tool results, and error resolutions from earlier in the conversation are preserved.

#### Acceptance Criteria

1. THE MacLaw codebase SHALL provide a Context_Compressor module in `corelib/context/compressor.go`
2. WHEN conversation history token count approaches the Compression_Threshold (80% of the model's context window), THE Context_Compressor SHALL trigger automatic compression
3. THE Context_Compressor SHALL preserve: key user decisions, tool call results, error resolutions, file paths mentioned, and code snippets
4. THE Context_Compressor SHALL discard: redundant thinking steps, repeated failed tool call attempts, and verbose intermediate output
5. WHEN compression is applied, THE Context_Compressor SHALL replace the original messages with compressed versions and insert a Compressed_Marker at the compression boundary
6. THE Context_Compressor SHALL use a lightweight LLM call to summarize discarded sections, preserving factual accuracy
7. WHEN the user issues the `/compress` command, THE Agent_Loop SHALL trigger manual compression of the current conversation history regardless of token count
8. THE compressed history SHALL maintain the conversation's chronological structure — compressed sections SHALL appear in their original position, not moved to the beginning or end
9. THE Context_Compressor SHALL NOT compress the most recent 5 conversation turns, ensuring the LLM has full context for the current interaction
10. IF the LLM summarization call fails during compression, THEN THE Context_Compressor SHALL fall back to simple truncation (dropping oldest messages) and log the failure
11. THE Context_Compressor SHALL maintain GUI/TUI parity — both interfaces SHALL use the same compression logic from `corelib/context/compressor.go`
12. THE Context_Compressor SHALL report the compression ratio (original tokens vs. compressed tokens) in the Compressed_Marker for user visibility

### Requirement 3: Skill Conditional Activation — Tool Availability Conditions

**User Story:** As a MacLaw user, I want skills to activate based on tool availability (e.g., "use this skill when Docker is not available"), so that fallback skills can automatically engage when primary tools are missing.

#### Acceptance Criteria

1. THE NLSkillEntry SHALL support four new condition fields: `fallback_for_toolsets`, `requires_toolsets`, `fallback_for_tools`, `requires_tools`
2. WHEN a skill declares `requires_tools: [ssh]`, THE Tool_Router SHALL only activate the skill when the `ssh` tool IS available in the current session
3. WHEN a skill declares `fallback_for_tools: [docker]`, THE Tool_Router SHALL activate the skill when the `docker` tool is NOT available in the current session
4. WHEN a skill declares `requires_toolsets: [browser]`, THE Tool_Router SHALL only activate the skill when the browser toolset group IS available
5. WHEN a skill declares `fallback_for_toolsets: [coding]`, THE Tool_Router SHALL activate the skill when the coding toolset group is NOT available
6. THE Skill_Scanner SHALL parse the four condition fields from skill YAML definitions
7. WHEN a skill has both keyword triggers AND tool availability conditions, THE Tool_Router SHALL evaluate both using AND logic — the skill activates only when both keyword triggers match AND tool conditions are satisfied
8. IF a skill declares no tool availability conditions, THEN THE Tool_Router SHALL evaluate the skill using only keyword triggers (backward compatible)
9. THE tool availability conditions SHALL be evaluated at tool routing time alongside existing keyword trigger evaluation
10. THE SkillYAMLFile parser SHALL include `fallback_for_toolsets`, `requires_toolsets`, `fallback_for_tools`, `requires_tools` in the known keys set

### Requirement 4: Credential File Mounting for Remote Execution

**User Story:** As a MacLaw user, I want skills to declare required credential files that are automatically mounted into remote execution environments, so that I don't have to manually copy credentials to remote hosts before running skills.

#### Acceptance Criteria

1. THE NLSkillEntry SHALL support a `required_credential_files` field containing a list of local file paths (e.g., `~/.kube/config`, `~/.aws/credentials`)
2. THE Skill_Scanner SHALL parse the `required_credential_files` field from skill YAML definitions
3. WHEN a skill with `required_credential_files` is executed, THE Skill_Runner SHALL check if each declared credential file exists locally before execution begins
4. IF any declared credential file is missing locally, THEN THE Skill_Runner SHALL return a `setup_needed` status with specific guidance identifying which files are missing and how to create them
5. WHEN a skill is executed via SSH remote execution, THE SSH_Background_Task SHALL automatically SCP declared credential files to the remote host before skill execution begins
6. WHEN skill execution completes (success or failure) on a remote host, THE SSH_Background_Task SHALL clean up (delete) the transferred credential files from the remote host
7. THE Skill_Runner SHALL NOT log or display the full contents or full paths of credential files in any user-visible output or log files
8. THE credential file mounting SHALL maintain GUI/TUI parity — both `gui/skill_runner.go` and `tui/agent_tools.go` SHALL implement identical pre-execution credential checks
9. THE SkillYAMLFile parser SHALL include `required_credential_files` in the known keys set
10. WHEN credential files are transferred to a remote host, THE SSH_Background_Task SHALL set restrictive file permissions (0600) on the transferred files

### Requirement 5: Plugin Skill Namespace — Qualified Naming for Hub Skills

**User Story:** As a MacLaw user, I want Hub-installed skills to use qualified names (e.g., `lovstudio:any2pdf`), so that skills from different publishers don't collide and I can see which skills belong to the same bundle.

#### Acceptance Criteria

1. THE NLSkillEntry SHALL support a `publisher:skill-name` qualified naming format (e.g., `lovstudio:any2pdf`)
2. WHEN a skill is installed from the Hub, THE Skill_Scanner SHALL automatically assign the publisher as a namespace prefix based on the Hub metadata
3. WHEN a user creates a skill locally, THE Skill_Scanner SHALL place the skill in the default namespace (no prefix required)
4. WHEN `manage_skill(action=list)` is called, THE Agent_Loop SHALL group skills by namespace, displaying namespaced skills under their publisher heading
5. WHEN a namespaced skill is loaded for execution, THE System_Prompt_Builder SHALL inject a Bundle_Context_Banner: "This skill is part of the '{publisher}' bundle. Related skills: {sibling list}"
6. THE skill lookup mechanism SHALL support both qualified names (`lovstudio:any2pdf`) and bare names (`any2pdf`), with qualified names taking precedence when a name collision exists
7. WHEN two skills from different publishers share the same bare name, THE Agent_Loop SHALL require the qualified name for disambiguation and display a warning when the bare name is used
8. THE `MatchesName` method on NLSkillEntry SHALL be extended to match against qualified names in addition to existing name, DirName, and SkillDir basename matching
9. THE namespace assignment SHALL NOT break existing skill references — skills already installed without a namespace SHALL continue to work with their bare names
10. THE plugin namespace system SHALL maintain GUI/TUI parity — both interfaces SHALL display and resolve namespaced skills identically

### Requirement 6: Skill Platform Filtering — OS-Based Skill Visibility

**User Story:** As a MacLaw user, I want skills that only work on specific operating systems to be automatically hidden on incompatible platforms, so that I don't see macOS-only skills when running on Windows.

#### Acceptance Criteria

1. THE NLSkillEntry SHALL support a `platforms` field with values from the set `["windows", "macos", "linux"]`
2. IF a skill's `platforms` field is omitted or empty, THEN THE Skill_Scanner SHALL treat the skill as available on all platforms (backward compatible)
3. WHEN scanning skill directories, THE Skill_Scanner SHALL filter out skills whose `platforms` field does not include the current operating system
4. THE platform detection SHALL map `runtime.GOOS` values as follows: `"windows"` → `"windows"`, `"darwin"` → `"macos"`, `"linux"` → `"linux"`
5. WHEN `manage_skill(action=list)` is called, THE Agent_Loop SHALL only show skills compatible with the current platform
6. THE platform filtering SHALL occur at scan time (in the Skill_Scanner), not at display time, so that incompatible skills are never loaded into memory
7. THE existing `Platforms` field on NLSkillEntry and `platforms` field in SkillYAMLFile SHALL be reused for this feature (already present in the codebase)

### Requirement 7: Dialectic User Modeling — Evolving User Profile with Confidence Scores

**User Story:** As a MacLaw user, I want the system to build and maintain an evolving model of my preferences, expertise, and work patterns, so that the agent's responses become more personalized over time without me having to repeatedly state my preferences.

#### Acceptance Criteria

1. THE MacLaw codebase SHALL provide a `user_model` module in `corelib/user/model.go` that maintains a structured user profile
2. THE user profile SHALL contain the following dimensions: `communication_style`, `technical_level`, `preferred_languages`, `domain_expertise`, `work_patterns`, `tool_preferences`
3. EACH profile dimension SHALL have a value (string), a Confidence_Score (float64 in [0, 1]), and an evidence list (array of observation strings with timestamps)
4. WHEN new evidence contradicts an existing profile dimension, THE User_Model SHALL apply Dialectic_Reconciliation: thesis (existing belief) + antithesis (new evidence) → synthesis (updated belief with adjusted Confidence_Score)
5. WHEN Dialectic_Reconciliation produces a synthesis, THE User_Model SHALL lower the Confidence_Score of the affected dimension (reflecting increased uncertainty) and record both the old and new evidence
6. THE System_Prompt_Builder SHALL inject the user profile as a structured `## User Profile` section in the system prompt, including dimension values and confidence scores
7. THE user profile SHALL update automatically based on conversation analysis as a post-turn hook — profile updates SHALL NOT block the Agent_Loop response
8. THE MacLaw codebase SHALL provide a `manage_user_model` tool that allows users to view, correct, and reset individual profile dimensions
9. WHEN a user explicitly corrects a profile dimension via `manage_user_model`, THE User_Model SHALL set the Confidence_Score to 1.0 and mark the dimension as user-confirmed
10. THE user profile SHALL persist across sessions in a JSON file at `~/.maclaw/data/user_model.json`
11. THE User_Model SHALL maintain GUI/TUI parity — both interfaces SHALL read from and write to the same profile file and inject the same profile section into the system prompt
12. WHEN the user profile file does not exist, THE User_Model SHALL initialize with empty dimensions (no assumptions) and begin learning from the first conversation

### Requirement 8: Session Search — Transcript Storage Schema

**User Story:** As a MacLaw developer, I want session transcripts stored in a well-defined schema, so that the FTS5 index can be efficiently queried and maintained.

#### Acceptance Criteria

1. THE session transcript storage SHALL use an SQLite database at `~/.maclaw/data/session_search.db`
2. EACH stored session document SHALL contain: session_id (unique), timestamp (ISO 8601), platform (gui/tui/im), topic (auto-extracted or empty), and full_text (concatenated conversation content)
3. THE FTS5 virtual table SHALL index the `full_text` and `topic` columns for search
4. WHEN a session transcript is persisted, THE storage module SHALL extract a topic from the first user message (first 100 characters) as the session topic
5. THE storage module SHALL provide a `Prune(olderThan time.Duration)` method to remove sessions older than a configurable retention period (default 90 days)

### Requirement 9: Context Compressor — Token Estimation

**User Story:** As a MacLaw developer, I want accurate token estimation for compression triggering, so that compression activates at the right time without requiring an external tokenizer.

#### Acceptance Criteria

1. THE Context_Compressor SHALL estimate token count using a character-based heuristic: 1 token ≈ 4 characters for English, 1 token ≈ 1.5 characters for CJK text
2. WHEN the estimated token count of conversation history plus system prompt exceeds the Compression_Threshold, THE Context_Compressor SHALL trigger compression
3. THE Context_Compressor SHALL use `MaclawLLMConfig.EffectiveContextTokens()` to determine the model's usable context window for threshold calculation

### Requirement 10: Credential File Mounting — Path Expansion

**User Story:** As a MacLaw developer, I want credential file paths to support tilde expansion and environment variables, so that skill authors can use portable path references.

#### Acceptance Criteria

1. WHEN a credential file path starts with `~`, THE Skill_Runner SHALL expand it to the user's home directory
2. WHEN a credential file path contains environment variables (e.g., `$HOME`, `%USERPROFILE%`), THE Skill_Runner SHALL expand them using the current environment
3. THE path expansion SHALL work correctly on Windows (`%USERPROFILE%`), macOS (`$HOME`), and Linux (`$HOME`)

### Requirement 11: Dialectic User Modeling — Evidence Collection

**User Story:** As a MacLaw developer, I want the evidence collection for user modeling to be lightweight and non-intrusive, so that it doesn't degrade agent performance.

#### Acceptance Criteria

1. THE evidence collection SHALL analyze only the user's messages (not assistant responses or tool results) to infer profile updates
2. THE evidence collection SHALL run asynchronously after each agent turn completes, using a goroutine that does not block the response
3. THE evidence collection SHALL use pattern matching (not LLM calls) for common signals: programming language mentions, tool preference expressions, expertise indicators
4. WHEN pattern matching is insufficient to determine a profile update, THE evidence collection SHALL queue the observation for batch LLM analysis (processed every 10 turns or at session end)
5. THE evidence collection SHALL rate-limit profile updates to at most one update per dimension per session, preventing rapid oscillation from a single conversation

### Requirement 12: Session Search — Pretty Printer and Round-Trip Property

**User Story:** As a MacLaw developer, I want session transcript serialization to be round-trip safe, so that stored transcripts can be reliably reconstructed.

#### Acceptance Criteria

1. THE session transcript serializer SHALL convert conversation history ([]conversationEntry) into a searchable plain-text format
2. THE session transcript deserializer SHALL reconstruct the original conversation structure from the stored plain-text format
3. FOR ALL valid conversation histories, serializing then deserializing SHALL produce an equivalent conversation structure (round-trip property)
4. THE serializer SHALL preserve message roles (user/assistant/system/tool), message content, and tool call metadata
