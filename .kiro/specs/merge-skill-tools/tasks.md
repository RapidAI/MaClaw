# Tasks

## Task 1: Update `corelib/tool/router.go` — BuiltinToolNames and CoreToolNames

- [x] 1.1 Add `"manage_skill": true` to `BuiltinToolNames` map
- [x] 1.2 Add `"manage_skill": true` to `CoreToolNames` map
- [x] 1.3 Remove `"list_skills": true` from `CoreToolNames` (keep in `BuiltinToolNames`)
- [x] 1.4 Remove `"run_skill": true` from `CoreToolNames` (keep in `BuiltinToolNames`)
- [x] 1.5 Verify `list_skills`, `run_skill`, `search_skill_hub`, `install_skill_hub`, `get_skill_run` remain in `BuiltinToolNames`

## Task 2: Add `toolManageSkill` dispatcher and `toolUploadSkill` handler to GUI (`gui/im_tools_misc.go`)

- [x] 2.1 Add `toolManageSkill(args map[string]interface{}, onProgress ProgressCallback) string` method to `IMMessageHandler` that switches on `action` parameter and delegates to existing handlers: `list` → `toolListSkills()`, `search` → `toolSearchSkillHub(args)`, `install` → `toolInstallSkillHub(args)`, `run` → `toolRunSkill(args, onProgress)`, `status` → `toolGetSkillRun(args)`, `upload` → `toolUploadSkill(args)`, default → error listing supported actions
- [x] 2.2 Add `toolUploadSkill(args map[string]interface{}) string` method to `IMMessageHandler` that extracts `name` parameter, calls `h.app.UploadNLSkillToMarket(name)`, returns success message with submission ID or error

## Task 3: Add `toolManageSkill` dispatcher and `toolUploadSkill` handler to TUI (`tui/agent_tools.go`)

- [x] 3.1 Add `toolManageSkill(args map[string]interface{}) string` method to `TUIAgentHandler` that switches on `action` parameter and delegates to existing handlers: `list` → `toolListSkills()`, `search` → `toolSearchSkillHub(args)`, `install` → `toolInstallSkillHub(args)`, `run` → `toolRunSkill(args)`, `status` → `toolGetSkillRun(args)`, `upload` → `toolUploadSkill(args)`, default → error listing supported actions
- [x] 3.2 Add `toolUploadSkill(args map[string]interface{}) string` method to `TUIAgentHandler` that handles upload logic adapted for TUI infrastructure (skill executor + skill market client initialization)

## Task 4: Update tool definition (`gui/im_tool_definitions.go`)

- [x] 4.1 Replace the 5 separate skill tool definitions (`list_skills`, `search_skill_hub`, `install_skill_hub`, `run_skill`, `get_skill_run`) with a single `manage_skill` definition containing the unified schema with `action` as required parameter (enum: list/search/install/run/status/upload) and all action-specific parameters
- [x] 4.2 Write a description for `manage_skill` that summarizes all six actions and their purposes

## Task 5: Update tool registry (`gui/tool_registry_builtin.go`)

- [x] 5.1 Replace the 5 separate skill tool registrations with a single `manage_skill` registration using `regP` (progress-aware) that delegates to `h.toolManageSkill(args, onProgress)`
- [x] 5.2 Add backward-compatible handler-only registrations for the 5 legacy tool names (`list_skills`, `search_skill_hub`, `install_skill_hub`, `run_skill`, `get_skill_run`) — these register handlers for dispatch but do not generate tool definitions. Use empty description string to signal no definition generation.

## Task 6: Update TUI dispatch (`tui/agent_handler.go`)

- [x] 6.1 Add `case "manage_skill": return h.toolManageSkill(args)` to the `dispatchTool` switch statement
- [x] 6.2 Keep existing legacy tool name cases (`list_skills`, `search_skill_hub`, `install_skill_hub`, `run_skill`, `get_skill_run`) for backward compatibility

## Task 7: Update deferred tool list (`gui/tool_deferred.go`)

- [x] 7.1 Add the 5 legacy skill tool names (`list_skills`, `search_skill_hub`, `install_skill_hub`, `run_skill`, `get_skill_run`) to `DeferredToolNames` so they don't appear in the initial prompt but remain discoverable via `discover_tool`

## Task 8: Write tests

- [x] 8.1 Write property-based test: dispatch round-trip equivalence — for any valid action and args, `toolManageSkill` produces same result as standalone handler (use mock handlers)
- [x] 8.2 Write property-based test: invalid action error — for any non-valid action string, error message contains all six supported action names
- [x] 8.3 Write unit tests for `toolUploadSkill`: success path, error propagation, empty name, nil executor
- [x] 8.4 Write unit tests verifying `CoreToolNames` contains `manage_skill` and not `list_skills`/`run_skill`
- [x] 8.5 Write unit tests verifying `BuiltinToolNames` contains `manage_skill` and all 5 legacy names
- [x] 8.6 Write unit tests verifying `DeferredToolNames` contains the 5 legacy names

## Task 9: Verify build and existing tests pass

- [x] 9.1 Run `go build ./...` to verify no compilation errors
- [x] 9.2 Run existing tests to verify no regressions
