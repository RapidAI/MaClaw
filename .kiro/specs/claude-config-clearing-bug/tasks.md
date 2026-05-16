# Tasks — Config Clearing Bug Fix (All Tools)

## Task 1: Create `ClearClaudeThirdPartySettings()` function

- [x] 1.1 Add `ClearClaudeThirdPartySettings()` to `corelib/configfile/claude.go`
  - Read existing `~/.claude/settings.json`
  - Remove third-party env keys from the `env` map: `ANTHROPIC_AUTH_TOKEN`, `ANTHROPIC_BASE_URL`, `ANTHROPIC_MODEL`, `ANTHROPIC_DEFAULT_HAIKU_MODEL`, `ANTHROPIC_DEFAULT_SONNET_MODEL`, `ANTHROPIC_DEFAULT_OPUS_MODEL`, `ANTHROPIC_SMALL_FAST_MODEL`, `CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC`, `API_TIMEOUT_MS`
  - Write back atomically, preserving all non-env fields and any env fields not in the removal list
  - If `settings.json` doesn't exist, return nil (no-op)
- [x] 1.2 Write unit tests for `ClearClaudeThirdPartySettings()` in `corelib/configfile/claude_test.go`
  - Test removes only third-party env keys
  - Test preserves non-env fields (e.g. `permissions`, `theme`)
  - Test preserves user-defined env fields not in the removal list
  - Test no-op when `settings.json` doesn't exist
  - Test no-op when `env` map is empty or missing

## Task 2: Create `ClearGeminiThirdPartySettings()` function

- [x] 2.1 Add `ClearGeminiThirdPartySettings()` to `corelib/configfile/gemini.go`
  - Read existing `~/.gemini/.env`, remove managed keys (`GEMINI_API_KEY`, `GOOGLE_API_KEY`, `GOOGLE_GEMINI_BASE_URL`, `GEMINI_MODEL`), write back preserving user's custom vars
  - Read existing `~/.gemini/settings.json`, reset `security.auth.selectedType` to `"oauth-personal"`, write back preserving other fields
  - If files don't exist, return nil (no-op)
- [x] 2.2 Write unit tests for `ClearGeminiThirdPartySettings()` in `corelib/configfile/gemini_test.go`
  - Test removes managed keys from `.env` while preserving custom vars
  - Test resets auth type in `settings.json` while preserving other fields
  - Test no-op when files don't exist
  - Test handles empty `.env` file gracefully

## Task 3: Create `ClearCodexThirdPartySettings()` function

- [x] 3.1 Add `ClearCodexThirdPartySettings()` to `corelib/configfile/codex.go`
  - Remove or clear `~/.codex/auth.json` (contains only the API key)
  - Read existing `~/.codex/config.toml`, remove/reset the `model_provider` top-level field and the provider section added by `WriteCodexConfig()`, write back preserving MCP servers, profiles, user settings
  - If files don't exist, return nil (no-op)
- [x] 3.2 Write unit tests for `ClearCodexThirdPartySettings()` in `corelib/configfile/codex_test.go`
  - Test clears `auth.json`
  - Test resets provider in `config.toml` while preserving MCP servers and profiles
  - Test no-op when files don't exist
  - Test handles `config.toml` with only user content (no provider section)

## Task 4: Replace `clearClaudeConfig()` with surgical write in `LaunchTool()`

- [x] 4.1 In `gui/app.go` `LaunchTool()`, replace the Claude non-builtin case:
  - Remove: `a.clearClaudeConfig()`
  - Add: `configfile.WriteClaudeProviderSettings(selectedModel.ModelName, selectedModel.ApiKey, selectedModel.ModelUrl, selectedModel.ModelId)`
  - Update log message to indicate surgical update instead of clearing
- [x] 4.2 In `gui/app.go` `LaunchTool()`, replace the Gemini non-builtin case:
  - Remove: `a.clearGeminiConfig()`
  - Add: `configfile.WriteGeminiConfig(selectedModel.ApiKey, selectedModel.ModelUrl, selectedModel.ModelId)`
  - Update log message
- [x] 4.3 In `gui/app.go` `LaunchTool()`, replace the Codex non-builtin case:
  - Remove: `a.clearCodexConfig()`
  - Add: `configfile.WriteCodexConfig(selectedModel.ApiKey, selectedModel.ModelUrl, selectedModel.ModelId, selectedModel.ModelName, "responses")`
  - Note: keep the existing `env["WIRE_API"] = "responses"` and `env["OPENAI_API_KEY"]` / `env["OPENAI_BASE_URL"]` assignments — those are process env vars, separate from config files
  - Update log message

## Task 5: Update builtin provider restore path for Claude/Gemini/Codex

- [x] 5.1 In `gui/app.go` `LaunchTool()` builtin/original mode branch, add tool-specific surgical cleanup for Claude:
  - Call `configfile.ClearClaudeThirdPartySettings()` to remove third-party env fields from `settings.json`
  - Keep `restoreToolNativeConfig("claude")` as backward-compat fallback: only execute if a pre-fix backup directory exists at `~/.maclaw/data/config_backup/claude/.claude/`
  - Log a migration message when backward-compat restore is triggered
- [x] 5.2 Add tool-specific surgical cleanup for Gemini in the builtin restore path:
  - Call `configfile.ClearGeminiThirdPartySettings()`
  - Keep `restoreToolNativeConfig("gemini")` as backward-compat fallback
- [x] 5.3 Add tool-specific surgical cleanup for Codex in the builtin restore path:
  - Call `configfile.ClearCodexThirdPartySettings()`
  - Keep `restoreToolNativeConfig("codex")` as backward-compat fallback
- [x] 5.4 Ensure other tools (opencode, iflow, kilo) continue using `restoreToolNativeConfig()` unchanged in the builtin restore path

## Task 6: Deprecate/remove unused clearing functions

- [x] 6.1 Remove or mark as deprecated `clearClaudeConfig()`, `clearGeminiConfig()`, `clearCodexConfig()` in `gui/app.go`
  - Verify no other call sites reference these functions (grep the codebase)
  - If other call sites exist, update them to use the surgical alternatives
  - Keep `clearOpencodeConfig()`, `clearIFlowConfig()`, `clearKiloConfig()` unchanged

## Task 7: Build verification and integration testing

- [x] 7.1 Run `go build ./...` to verify all changes compile without errors
- [x] 7.2 Run existing tests (`go test ./corelib/configfile/...`) to verify no regressions
- [x] 7.3 Run existing tests (`go test ./gui/...`) to verify no regressions in app.go changes
- [x] 7.4 Manual verification checklist:
  - Launch Claude with third-party provider → verify `~/.claude/` directory preserved, `settings.json` env fields updated
  - Launch Gemini with third-party provider → verify `~/.gemini/` directory preserved, `.env` and `settings.json` updated
  - Launch Codex with third-party provider → verify `~/.codex/` directory preserved, `auth.json` and `config.toml` updated
  - Switch back to builtin provider → verify third-party config entries cleaned up, user state preserved
  - Launch opencode/iflow/kilo → verify behavior unchanged (still uses backup/restore)
