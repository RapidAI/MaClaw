# Requirements Document

## Introduction

Merge the 5 existing skill-related LLM tools (`list_skills`, `search_skill_hub`, `install_skill_hub`, `run_skill`, `get_skill_run`) into a single `manage_skill` tool with action-based routing, following the established merge pattern used by `manage_config`, `manage_template`, and `manage_schedule`. Additionally, add a new `upload` action that exposes the existing `UploadNLSkillToMarket` Wails binding to the LLM, enabling skill uploads to SkillMarket without the GUI button or TUI CLI.

## Glossary

- **Manage_Skill_Tool**: The unified `manage_skill` LLM tool that replaces the 5 separate skill tools and adds the `upload` action
- **Action_Router**: The dispatcher function (`toolManageSkill`) that routes the `action` parameter to the appropriate handler
- **Legacy_Tool_Name**: One of the 5 original tool names (`list_skills`, `search_skill_hub`, `install_skill_hub`, `run_skill`, `get_skill_run`) that must remain functional as backward-compatible dispatch aliases
- **Tool_Definition**: The JSON schema object sent to the LLM describing a tool's name, description, and parameters
- **Tool_Registry**: The `ToolRegistry` in `gui/tool_registry_builtin.go` where tools are registered with handlers, tags, and schemas
- **Deferred_Tool_List**: The `DeferredToolNames` list in `gui/tool_deferred.go` controlling progressive tool discovery
- **BuiltinToolNames**: The map in `corelib/tool/router.go` listing all recognized builtin tool names
- **CoreToolNames**: The map in `corelib/tool/router.go` listing tools always included in the LLM context
- **SkillMarket**: The remote marketplace where users can upload and share skills
- **GUI_Handler**: The `IMMessageHandler` in the GUI (desktop) application
- **TUI_Handler**: The `AgentHandler` in the TUI (terminal) application

## Requirements

### Requirement 1: Unified manage_skill Tool Definition

**User Story:** As an LLM, I want a single `manage_skill` tool with action-based routing, so that I consume fewer context tokens while retaining full skill management capability.

#### Acceptance Criteria

1. THE Manage_Skill_Tool SHALL expose a single tool definition with `action` as a required parameter accepting values `list`, `search`, `install`, `run`, `status`, and `upload`
2. WHEN action is `list`, THE Manage_Skill_Tool SHALL accept no additional required parameters
3. WHEN action is `search`, THE Manage_Skill_Tool SHALL require a `query` parameter of type string
4. WHEN action is `install`, THE Manage_Skill_Tool SHALL require `skill_id` and `hub_url` parameters of type string, and accept optional `auto_run` (boolean) and `wait_seconds` (number) parameters
5. WHEN action is `run`, THE Manage_Skill_Tool SHALL require a `name` parameter of type string, and accept optional `args` (object), `env` (object), `operation` (string), `input` (string), `output` (string), `user_prompt` (string), and `wait_seconds` (number) parameters
6. WHEN action is `status`, THE Manage_Skill_Tool SHALL require a `run_id` parameter of type string, and accept an optional `wait_seconds` (number) parameter
7. WHEN action is `upload`, THE Manage_Skill_Tool SHALL require a `name` parameter of type string
8. THE Manage_Skill_Tool SHALL include a description that summarizes all six actions and their purposes

### Requirement 2: Action Routing Dispatcher

**User Story:** As a developer, I want the `manage_skill` tool to dispatch each action to the correct existing handler, so that the merge introduces no behavioral changes for existing actions.

#### Acceptance Criteria

1. WHEN action is `list`, THE Action_Router SHALL delegate to the existing `toolListSkills` handler and return its result unchanged
2. WHEN action is `search`, THE Action_Router SHALL delegate to the existing `toolSearchSkillHub` handler and return its result unchanged
3. WHEN action is `install`, THE Action_Router SHALL delegate to the existing `toolInstallSkillHub` handler and return its result unchanged
4. WHEN action is `run`, THE Action_Router SHALL delegate to the existing `toolRunSkill` handler and return its result unchanged
5. WHEN action is `status`, THE Action_Router SHALL delegate to the existing `toolGetSkillRun` handler and return its result unchanged
6. WHEN action is `upload`, THE Action_Router SHALL call `UploadNLSkillToMarket` with the `name` parameter and return the submission ID on success
7. IF an unrecognized action is provided, THEN THE Action_Router SHALL return an error message listing the supported actions
8. FOR ALL valid actions, parsing the action parameter then dispatching then returning SHALL produce the same result as calling the original standalone handler directly (round-trip equivalence)

### Requirement 3: Upload Action Implementation

**User Story:** As an LLM, I want an `upload` action in `manage_skill`, so that I can upload a user's skill to SkillMarket without requiring the GUI button or TUI CLI.

#### Acceptance Criteria

1. WHEN action is `upload` and a valid skill name is provided, THE Action_Router SHALL call the existing `UploadNLSkillToMarket` function with the skill name
2. WHEN the upload succeeds, THE Action_Router SHALL return a success message containing the submission ID
3. IF the skill name is empty or missing, THEN THE Action_Router SHALL return a descriptive error message indicating the name parameter is required
4. IF `UploadNLSkillToMarket` returns an error, THEN THE Action_Router SHALL return the error message to the LLM
5. THE Action_Router SHALL ensure the skill executor and skill market client are initialized before attempting upload

### Requirement 4: Legacy Tool Name Backward Compatibility

**User Story:** As a developer, I want the old tool names to continue working as dispatch aliases, so that existing tests and any cached LLM tool calls do not break.

#### Acceptance Criteria

1. WHEN the GUI_Handler receives a tool call with name `list_skills`, THE GUI_Handler SHALL dispatch it to the `toolListSkills` handler
2. WHEN the GUI_Handler receives a tool call with name `search_skill_hub`, THE GUI_Handler SHALL dispatch it to the `toolSearchSkillHub` handler
3. WHEN the GUI_Handler receives a tool call with name `install_skill_hub`, THE GUI_Handler SHALL dispatch it to the `toolInstallSkillHub` handler
4. WHEN the GUI_Handler receives a tool call with name `run_skill`, THE GUI_Handler SHALL dispatch it to the `toolRunSkill` handler
5. WHEN the GUI_Handler receives a tool call with name `get_skill_run`, THE GUI_Handler SHALL dispatch it to the `toolGetSkillRun` handler
6. THE Legacy_Tool_Name entries SHALL remain in BuiltinToolNames so they are recognized as valid builtin tools
7. THE Legacy_Tool_Name entries SHALL NOT generate Tool_Definition objects sent to the LLM (only `manage_skill` generates a definition)

### Requirement 5: Tool Definition Consolidation

**User Story:** As an LLM, I want only the `manage_skill` definition in my tool list, so that I save approximately 800-1000 tokens of context space previously consumed by 5 separate definitions.

#### Acceptance Criteria

1. THE Tool_Registry SHALL register `manage_skill` as a single tool with the unified schema
2. THE Tool_Registry SHALL register the 5 Legacy_Tool_Name entries with handlers only (for backward-compatible dispatch), without generating tool definitions
3. THE `buildToolDefinitions` function in `im_tool_definitions.go` SHALL replace the 5 separate skill tool definitions with a single `manage_skill` definition
4. THE BuiltinToolNames map SHALL include `manage_skill` as a recognized builtin tool name
5. THE CoreToolNames map SHALL replace `list_skills` and `run_skill` with `manage_skill` so the merged tool is always available in the LLM context

### Requirement 6: TUI Dispatch Support

**User Story:** As a TUI user, I want `manage_skill` and the legacy tool names to work in the terminal interface, so that both GUI and TUI have consistent behavior.

#### Acceptance Criteria

1. WHEN the TUI_Handler receives a tool call with name `manage_skill`, THE TUI_Handler SHALL dispatch it to a `toolManageSkill` action router
2. THE TUI_Handler SHALL continue to dispatch Legacy_Tool_Name calls to their existing handlers for backward compatibility
3. WHEN action is `upload` in the TUI, THE TUI_Handler SHALL call the appropriate upload function and return the result
4. FOR ALL six actions, THE TUI_Handler SHALL produce equivalent results to the GUI_Handler

### Requirement 7: Deferred Tool List Update

**User Story:** As a developer, I want the deferred tool list to reference `manage_skill` instead of the individual skill tool names, so that progressive tool discovery works correctly with the merged tool.

#### Acceptance Criteria

1. THE Deferred_Tool_List SHALL include `manage_skill` if skill tools should be deferred
2. THE Deferred_Tool_List SHALL include the 5 Legacy_Tool_Name entries so they remain discoverable but do not appear in the initial prompt
3. IF `manage_skill` is not deferred (kept in CoreToolNames), THEN the Legacy_Tool_Name entries SHALL still be listed in the Deferred_Tool_List for backward-compat dispatch only

### Requirement 8: run_skill Progress Callback Preservation

**User Story:** As a developer, I want the `run` action to preserve the progress callback mechanism, so that long-running skill executions continue to report progress.

#### Acceptance Criteria

1. WHEN action is `run`, THE Manage_Skill_Tool SHALL support the `HandlerProg` (progress callback) interface, not just the basic `Handler` interface
2. THE Tool_Registry registration for `manage_skill` SHALL use `regP` (progress-aware registration) to support the progress callback
3. WHEN the `run` action delegates to `toolRunSkill`, THE Action_Router SHALL pass through the `ProgressCallback` parameter unchanged
