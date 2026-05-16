# Config Clearing Bug — Bugfix Design (All Tools)

## Overview

When maclaw launches coding tools (Claude, Gemini, Codex) with a non-builtin (third-party) provider via `LaunchTool()`, the `clearXxxConfig()` functions call `backupToolNativeConfig(tool)` which moves the **entire** tool config directory (`~/.claude/`, `~/.gemini/`, `~/.codex/`) to backup. This destroys user state — conversation history, MCP plugins, hooks, settings, and project trust entries.

The core problem (as identified by the user): **when using third-party providers to launch coding tools, the `~/.claude`, `~/.codex`, `~/.gemini` directories get modified. In theory, we should only backup/restore key config files, or even just specific config entries.**

The fix replaces the destructive `clearXxxConfig()` calls with surgical config-entry-level updates using existing functions: `WriteClaudeSettings()`, `WriteGeminiConfig()`, and `WriteCodexConfig()`. For switching back to the builtin provider, instead of restoring entire backup directories, the fix cleans up third-party-specific config entries. Other tools (opencode, iflow, kilo) that use `backupToolNativeConfig` + `syncToXxxSettings` are left unchanged since they write instance-specific config by design.

## Glossary

- **Bug_Condition (C)**: The condition that triggers the bug — launching Claude/Gemini/Codex via `LaunchTool()` with a non-builtin provider, which calls `clearXxxConfig()` → `backupToolNativeConfig(tool)`, moving/deleting the entire tool config directory
- **Property (P)**: The desired behavior — only specific config entries (API key, base URL, model ID) are updated in the tool's settings file; all other user state is preserved
- **Preservation**: Existing behaviors that must remain unchanged: SDK mode path, `WriteClaudeSettings()`/`WriteGeminiConfig()`/`WriteCodexConfig()` merge semantics, environment variable passing, opencode/iflow/kilo backup flow
- **`clearClaudeConfig()`**: Function in `gui/app.go` that calls `backupToolNativeConfig("claude")`, moving the entire `~/.claude/` directory
- **`clearGeminiConfig()`**: Function in `gui/app.go` that calls `backupToolNativeConfig("gemini")`, moving the entire `~/.gemini/` directory
- **`clearCodexConfig()`**: Function in `gui/app.go` that calls `backupToolNativeConfig("codex")`, moving the entire `~/.codex/` directory
- **`backupToolNativeConfig(tool)`**: Function in `gui/app.go` that moves a tool's native config directory to `~/.maclaw/data/config_backup/<tool>/`
- **`WriteClaudeSettings(apiKey, baseURL, modelID)`**: Function in `corelib/configfile/claude.go` that merges API env fields into `~/.claude/settings.json`, preserving all non-env fields
- **`WriteGeminiConfig(apiKey, baseURL, modelID)`**: Function in `corelib/configfile/gemini.go` that writes `~/.gemini/.env` and updates `~/.gemini/settings.json`, preserving existing fields
- **`WriteCodexConfig(apiKey, baseURL, modelID, providerName, wireApi)`**: Function in `corelib/configfile/codex.go` that writes `~/.codex/auth.json` and incrementally updates `~/.codex/config.toml`, preserving MCP servers, profiles, etc.
- **`restoreToolNativeConfig(tool)`**: Function in `gui/app.go` that restores a previously backed-up config directory, called when switching back to the builtin provider

## Bug Details

### Bug Condition

The bug manifests when a user launches Claude, Gemini, or Codex via `LaunchTool()` with a non-builtin (third-party) provider. Each tool's `clearXxxConfig()` function unconditionally calls `backupToolNativeConfig(tool)`, which either moves the entire tool config directory to backup (first time) or deletes it via `os.RemoveAll` (subsequent times when backup already exists).

**Formal Specification:**
```
FUNCTION isBugCondition(input)
  INPUT: input of type LaunchToolInput {toolName: string, model: ModelConfig}
  OUTPUT: boolean

  RETURN input.toolName IN ["claude", "gemini", "codex"]
         AND input.model.IsBuiltin == false
         AND clearXxxConfig() is called for the tool
         AND backupToolNativeConfig(tool) moves/deletes the tool's config directory
END FUNCTION
```

### Examples

- **Claude Example 1**: User selects "智谱 GLM" provider → launches Claude Code → `clearClaudeConfig()` moves `~/.claude/` to `~/.maclaw/data/config_backup/claude/.claude/` → conversation history in `~/.claude/projects/` is gone
- **Claude Example 2**: User launches Claude Code with third-party provider a second time → backup already exists → `os.RemoveAll(~/.claude/)` deletes the directory → any state created since the first launch is destroyed
- **Gemini Example**: User selects a third-party provider → launches Gemini → `clearGeminiConfig()` moves `~/.gemini/` to backup → user's Gemini CLI settings, `.env` custom vars, and session data are lost
- **Codex Example**: User selects a third-party provider → launches Codex → `clearCodexConfig()` moves `~/.codex/` to backup → user's MCP server configs in `config.toml`, auth tokens, and profiles are lost
- **Restore Example**: User switches from third-party back to builtin → `restoreToolNativeConfig("claude")` calls `os.RemoveAll(~/.claude/)` before restoring old backup → conversations, plugins, and hooks created while using the third-party provider are lost
- **Edge case**: User has never used the tool before (no config directory) → `clearXxxConfig()` is a no-op → no data loss, but the pattern is still wrong

## Expected Behavior

### Preservation Requirements

**Unchanged Behaviors:**
- Builtin provider restore path must continue to work for existing backups created before this fix (backward compatibility migration)
- SDK mode launch path (`buildRemoteLaunchSpec` → `RemoteSessionManager.Create` → `ensureClaudeOnboardingComplete`) must remain unchanged
- `WriteClaudeSettings()`, `WriteGeminiConfig()`, `WriteCodexConfig()` merge semantics must be preserved
- Other tools' backup/restore flow (opencode, iflow, kilo) must continue using `backupToolNativeConfig()` + `syncToXxxSettings()` as before — these tools write instance-specific config by design
- Environment variables passed in the `env` map to launched processes must continue to work
- `ensureToolOnboardingComplete()` called later in `LaunchTool()` must continue to work

**Scope:**
All inputs that do NOT involve launching Claude/Gemini/Codex with a non-builtin provider should be completely unaffected by this fix. This includes:
- Launching any tool with the builtin (original) provider
- Launching opencode, iflow, kilo, codebuddy, or OEM tools
- SDK mode launches
- All non-launch operations in maclaw

## Hypothesized Root Cause

Based on the bug description and code analysis, the root causes are:

1. **Overly aggressive config clearing**: `clearXxxConfig()` functions were designed to prevent stale API key configs from interfering with new provider settings. However, they use `backupToolNativeConfig(tool)` which treats the entire tool config directory as disposable — but these directories contain far more than API config: conversation history, MCP plugins, hooks, user preferences, and other state.

2. **Incorrect assumption about config scope**: `toolNativeConfigPaths()` returns the entire home directory for each tool (`~/.claude/`, `~/.gemini/`, `~/.codex/`). This was appropriate when these directories only contained config, but modern coding tools store rich user data in these directories.

3. **Existing correct alternatives unused**: Surgical write functions already exist for all three tools:
   - `WriteClaudeSettings()` — merges only `env` fields into `~/.claude/settings.json`
   - `WriteGeminiConfig()` — preserves existing `.env` vars and `settings.json` fields in `~/.gemini/`
   - `WriteCodexConfig()` — incrementally updates `~/.codex/auth.json` and `config.toml`, preserving MCP servers, profiles, etc.
   
   These functions are used by other code paths (SSO login, model switching) but were not used in the `LaunchTool()` non-builtin path.

4. **Restore path equally destructive**: `restoreToolNativeConfig(tool)` calls `os.RemoveAll(srcDir)` on the current config directory before restoring the backup, destroying any state created during third-party provider usage.

## Correctness Properties

Property 1: Bug Condition - Non-builtin tool launch preserves user state

_For any_ `LaunchTool()` call where `toolName` is "claude", "gemini", or "codex" and `selectedModel.IsBuiltin == false`, the fixed code SHALL update only the tool-specific API configuration entries (settings.json env fields for Claude, .env + settings.json for Gemini, auth.json + config.toml provider section for Codex) without moving, deleting, or otherwise modifying the tool's config directory structure, conversation history, hooks, MCP plugins, or any non-API-related config fields.

**Validates: Requirements 2.1, 2.2, 2.3, 2.4, 2.5**

Property 2: Preservation - Non-affected tools and builtin provider path unchanged

_For any_ `LaunchTool()` call where `toolName` is NOT in ["claude", "gemini", "codex"] OR `selectedModel.IsBuiltin == true`, the fixed code SHALL produce exactly the same behavior as the original code, preserving the existing `backupToolNativeConfig()`/`restoreToolNativeConfig()` flow for other tools and the builtin provider restore path.

**Validates: Requirements 3.1, 3.2, 3.3, 3.5, 3.6**

## Fix Implementation

### Changes Required

Assuming our root cause analysis is correct:

**File**: `gui/app.go`

**Function**: `LaunchTool()` — Claude non-builtin switch case (~line 3190)

**Specific Changes**:

1. **Replace `a.clearClaudeConfig()` with surgical settings write**:
   - Remove: `a.clearClaudeConfig()`
   - Add: `configfile.WriteClaudeProviderSettings(selectedModel.ModelName, selectedModel.ApiKey, selectedModel.ModelUrl, selectedModel.ModelId)`
   - This uses the existing `WriteClaudeSettings()` which merges only `env` fields into `~/.claude/settings.json`
   - Update log message to `"Claude: Updated settings.json with provider config (preserving user state)"`

2. **Replace `a.clearGeminiConfig()` with surgical settings write**:
   - Remove: `a.clearGeminiConfig()`
   - Add: `configfile.WriteGeminiConfig(selectedModel.ApiKey, selectedModel.ModelUrl, selectedModel.ModelId)`
   - This uses the existing `WriteGeminiConfig()` which preserves existing `.env` vars and `settings.json` fields
   - Update log message to `"Gemini: Updated config with provider settings (preserving user state)"`

3. **Replace `a.clearCodexConfig()` with surgical settings write**:
   - Remove: `a.clearCodexConfig()`
   - Add: `configfile.WriteCodexConfig(selectedModel.ApiKey, selectedModel.ModelUrl, selectedModel.ModelId, selectedModel.ModelName, "responses")`
   - This uses the existing `WriteCodexConfig()` which incrementally updates `auth.json` and `config.toml`
   - Update log message to `"Codex: Updated config with provider settings (preserving user state)"`

4. **Handle builtin provider restore for Claude**: In the `else` (builtin/original mode) branch:
   - For Claude: Call new `configfile.ClearClaudeThirdPartySettings()` to remove third-party env fields from `settings.json`
   - Keep `restoreToolNativeConfig("claude")` as backward-compat fallback: if a pre-fix backup exists, restore it (one-time migration), then the backup is consumed

5. **Handle builtin provider restore for Gemini**: 
   - Call new `configfile.ClearGeminiThirdPartySettings()` to remove third-party env keys from `~/.gemini/.env` and reset `settings.json` auth type
   - Keep `restoreToolNativeConfig("gemini")` as backward-compat fallback

6. **Handle builtin provider restore for Codex**:
   - Call new `configfile.ClearCodexThirdPartySettings()` to remove third-party auth and provider config
   - Keep `restoreToolNativeConfig("codex")` as backward-compat fallback

**File**: `corelib/configfile/claude.go`

7. **New function `ClearClaudeThirdPartySettings()`**:
   - Read existing `~/.claude/settings.json`
   - Remove third-party-specific env keys from the `env` map: `ANTHROPIC_AUTH_TOKEN`, `ANTHROPIC_BASE_URL`, `ANTHROPIC_MODEL`, `ANTHROPIC_DEFAULT_HAIKU_MODEL`, `ANTHROPIC_DEFAULT_SONNET_MODEL`, `ANTHROPIC_DEFAULT_OPUS_MODEL`, `ANTHROPIC_SMALL_FAST_MODEL`, `CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC`, `API_TIMEOUT_MS`
   - Write back atomically, preserving all non-env fields
   - If `settings.json` doesn't exist, no-op

**File**: `corelib/configfile/gemini.go`

8. **New function `ClearGeminiThirdPartySettings()`**:
   - Read existing `~/.gemini/.env`, remove managed keys (`GEMINI_API_KEY`, `GOOGLE_API_KEY`, `GOOGLE_GEMINI_BASE_URL`, `GEMINI_MODEL`), write back preserving user's custom vars
   - Read existing `~/.gemini/settings.json`, reset `security.auth.selectedType` to `"oauth-personal"`, write back preserving other fields
   - If files don't exist, no-op

**File**: `corelib/configfile/codex.go`

9. **New function `ClearCodexThirdPartySettings()`**:
   - Remove `~/.codex/auth.json` (or clear its contents) — this only contains the API key
   - Read existing `~/.codex/config.toml`, remove/reset the `model_provider` and provider section added by `WriteCodexConfig()`, write back preserving MCP servers, profiles, etc.
   - If files don't exist, no-op

**File**: `gui/app.go`

10. **Update builtin restore path**: In the `else` (original mode) branch of `LaunchTool()`:
    - Add tool-specific surgical cleanup before the generic `restoreToolNativeConfig()` call
    - For claude/gemini/codex: call the new `ClearXxxThirdPartySettings()` function first
    - Then check if a pre-fix backup exists; if so, restore it (backward compat migration) and log a migration message
    - If no backup exists, the surgical cleanup is sufficient

11. **Deprecate `clearClaudeConfig()`, `clearGeminiConfig()`, `clearCodexConfig()`**: These functions are no longer called. Mark them as deprecated or remove them. Keep `clearOpencodeConfig()`, `clearIFlowConfig()`, `clearKiloConfig()` unchanged.

## Testing Strategy

### Validation Approach

The testing strategy follows a two-phase approach: first, surface counterexamples that demonstrate the bug on unfixed code, then verify the fix works correctly and preserves existing behavior.

### Exploratory Bug Condition Checking

**Goal**: Surface counterexamples that demonstrate the bug BEFORE implementing the fix. Confirm or refute the root cause analysis. If we refute, we will need to re-hypothesize.

**Test Plan**: Write tests that set up tool config directories with known contents, then call `clearXxxConfig()` and verify the directories are moved/deleted. Run these tests on the UNFIXED code to observe failures.

**Test Cases**:
1. **Claude Directory Move Test**: Create `~/.claude/projects/test-project/` with a file, call `clearClaudeConfig()`, verify `~/.claude/` no longer exists (will fail on unfixed code — confirming the bug)
2. **Gemini Directory Move Test**: Create `~/.gemini/.env` with custom vars, call `clearGeminiConfig()`, verify `~/.gemini/` no longer exists (will fail on unfixed code)
3. **Codex Directory Move Test**: Create `~/.codex/config.toml` with MCP servers, call `clearCodexConfig()`, verify `~/.codex/` no longer exists (will fail on unfixed code)
4. **Repeated Launch Test**: Call `clearClaudeConfig()` twice, verify `~/.claude/` is destroyed both times (will fail on unfixed code — confirming 1.5)

**Expected Counterexamples**:
- Tool config directories are moved to `~/.maclaw/data/config_backup/<tool>/` on first call
- Tool config directories are deleted via `os.RemoveAll` on subsequent calls when backup exists
- Confirmed cause: `backupToolNativeConfig` treats entire directory as disposable config

### Fix Checking

**Goal**: Verify that for all inputs where the bug condition holds, the fixed function produces the expected behavior.

**Pseudocode:**
```
FOR ALL input WHERE isBugCondition(input) DO
  result := launchTool_fixed(input)
  ASSERT tool config directory still exists
  ASSERT user state files (projects/, hooks/, plugins, custom vars) unchanged
  ASSERT API config entries updated to new provider values
  ASSERT non-API config entries unchanged
END FOR
```

### Preservation Checking

**Goal**: Verify that for all inputs where the bug condition does NOT hold, the fixed function produces the same result as the original function.

**Pseudocode:**
```
FOR ALL input WHERE NOT isBugCondition(input) DO
  ASSERT launchTool_original(input) = launchTool_fixed(input)
END FOR
```

**Testing Approach**: Property-based testing is recommended for preservation checking because:
- It generates many test cases automatically across the input domain (different tool names, builtin vs non-builtin, various model configs)
- It catches edge cases that manual unit tests might miss (empty API keys, missing directories, concurrent launches)
- It provides strong guarantees that behavior is unchanged for all non-buggy inputs

**Test Plan**: Observe behavior on UNFIXED code first for non-affected tools and builtin launches, then write property-based tests capturing that behavior.

**Test Cases**:
1. **Other Tools Preservation**: Verify opencode/iflow/kilo still call `backupToolNativeConfig()` + `syncToXxxSettings()` and behave identically
2. **Builtin Provider Preservation**: Verify launching any tool with builtin provider still calls `restoreToolNativeConfig()` (with backward-compat fallback for claude/gemini/codex)
3. **SDK Mode Preservation**: Verify SDK mode path still uses `ensureClaudeOnboardingComplete()` without calling `clearClaudeConfig()`
4. **Surgical Write Merge Preservation**: Verify `WriteClaudeSettings()`, `WriteGeminiConfig()`, `WriteCodexConfig()` continue to merge correctly, preserving non-API fields
5. **Environment Variable Preservation**: Verify the `env` map passed to launched processes still contains all expected variables

### Unit Tests

- Test `ClearClaudeThirdPartySettings()` removes only third-party env keys from `settings.json`
- Test `ClearClaudeThirdPartySettings()` preserves non-env fields and user-defined env fields
- Test `ClearClaudeThirdPartySettings()` is a no-op when `settings.json` doesn't exist
- Test `ClearGeminiThirdPartySettings()` removes managed keys from `.env`, preserves custom vars
- Test `ClearGeminiThirdPartySettings()` resets auth type in `settings.json`
- Test `ClearCodexThirdPartySettings()` clears `auth.json` and resets provider in `config.toml`
- Test `ClearCodexThirdPartySettings()` preserves MCP servers and profiles in `config.toml`
- Test the modified `LaunchTool()` Claude/Gemini/Codex non-builtin paths call surgical write functions
- Test backward compatibility: if a pre-fix backup exists, it's restored on builtin switch (one-time migration)

### Property-Based Tests

- Generate random tool config directory structures and verify non-builtin launches preserve all contents except API config entries
- Generate random `settings.json`/`.env`/`config.toml` contents and verify surgical write functions only modify API-related fields
- Generate random sequences of provider switches (builtin → third-party → builtin → different third-party) and verify no data loss at any step
- Generate random tool names and model configs, verify non-affected tools are completely unaffected

### Integration Tests

- Test full launch flow for each tool: configure third-party provider → launch tool → verify config directory intact and API settings updated
- Test provider switching for each tool: launch with third-party → switch to builtin → switch to different third-party → verify all user state preserved throughout
- Test backward compatibility migration: create a pre-fix backup → switch to builtin → verify backup is restored and consumed
