# Bugfix Requirements Document

## Introduction

When maclaw launches Claude Code with a third-party (non-builtin) provider via the desktop `LaunchTool()` path, `clearClaudeConfig()` calls `backupToolNativeConfig("claude")` which moves the **entire** `~/.claude/` directory to `~/.maclaw/data/config_backup/claude/.claude/`. This destroys Claude Code's conversation history (`~/.claude/projects/`), installed MCP plugins, hooks, settings, and project trust entries. The user loses all Claude Code state on every launch through maclaw when using a non-builtin provider. The intent was only to prevent stale API key configs from interfering, but the implementation is overly aggressive — it nukes all user data instead of surgically updating only the API configuration in `~/.claude/settings.json`.

## Bug Analysis

### Current Behavior (Defect)

1.1 WHEN a user launches Claude Code via `LaunchTool()` with a non-builtin (third-party) provider THEN the system moves the entire `~/.claude/` directory to `~/.maclaw/data/config_backup/claude/.claude/`, destroying all Claude Code user state

1.2 WHEN the entire `~/.claude/` directory is moved during `clearClaudeConfig()` THEN the system loses Claude Code conversation history stored in `~/.claude/projects/`, causing prompt history to disappear when reopening the same project

1.3 WHEN the entire `~/.claude/` directory is moved during `clearClaudeConfig()` THEN the system loses installed MCP plugins (e.g. `~/.claude/plugins/` or plugin references in settings), causing plugins installed via Claude Code's built-in system to disappear

1.4 WHEN the entire `~/.claude/` directory is moved during `clearClaudeConfig()` THEN the system loses Claude Code settings (`~/.claude/settings.json` user preferences, `~/.claude/hooks/` custom hooks, project trust entries in `~/.claude.json`)

1.5 WHEN a user launches Claude Code via `LaunchTool()` with a non-builtin provider multiple times THEN the system destroys user state on every launch because `backupToolNativeConfig` either moves the directory (first time) or removes it via `os.RemoveAll` (subsequent times when backup already exists)

1.6 WHEN a user switches back to the builtin provider THEN `restoreToolNativeConfig("claude")` restores the backup, but any Claude Code state created while using the third-party provider (new conversations, newly installed plugins) is lost because `os.RemoveAll(srcDir)` is called before restoring the old backup

### Expected Behavior (Correct)

2.1 WHEN a user launches Claude Code via `LaunchTool()` with a non-builtin provider THEN the system SHALL only update `~/.claude/settings.json` with the new provider's API configuration (via `WriteClaudeSettings()`) without moving or deleting the `~/.claude/` directory

2.2 WHEN a user launches Claude Code via `LaunchTool()` with a non-builtin provider THEN the system SHALL preserve Claude Code conversation history in `~/.claude/projects/` so that prompt history is retained when reopening the same project

2.3 WHEN a user launches Claude Code via `LaunchTool()` with a non-builtin provider THEN the system SHALL preserve installed MCP plugins and their configuration so that plugins remain available across launches

2.4 WHEN a user launches Claude Code via `LaunchTool()` with a non-builtin provider THEN the system SHALL preserve Claude Code hooks (`~/.claude/hooks/`), project trust entries, and other user settings not related to API provider configuration

2.5 WHEN a user launches Claude Code via `LaunchTool()` with a non-builtin provider multiple times THEN the system SHALL not destroy or move any user state on repeated launches — only `~/.claude/settings.json` env fields are updated each time

2.6 WHEN a user switches from a third-party provider back to the builtin provider THEN the system SHALL restore the original API configuration in `~/.claude/settings.json` (or remove third-party env fields) without destroying conversations, plugins, or hooks created while using the third-party provider

### Unchanged Behavior (Regression Prevention)

3.1 WHEN a user launches Claude Code via `LaunchTool()` with a builtin (original) provider THEN the system SHALL CONTINUE TO restore previously backed-up native config via `restoreToolNativeConfig("claude")`

3.2 WHEN a user launches Claude Code via the SDK mode path (`buildRemoteLaunchSpec` → `RemoteSessionManager.Create`) THEN the system SHALL CONTINUE TO use `ensureClaudeOnboardingComplete()` which correctly preserves existing config without calling `clearClaudeConfig()`

3.3 WHEN `WriteClaudeSettings()` is called with API key, base URL, and model ID THEN the system SHALL CONTINUE TO merge the new env fields into the existing `~/.claude/settings.json` content, preserving non-env fields already present in the file

3.4 WHEN `clearOpencodeConfig()`, `clearIFlowConfig()`, `clearKiloConfig()` are called for their respective tools THEN the system SHALL CONTINUE TO use `backupToolNativeConfig()` + `syncToXxxSettings()` as before (these tools write instance-specific config by design). Note: `clearGeminiConfig()` and `clearCodexConfig()` are also being fixed to use surgical config updates, same as Claude.

3.5 WHEN `~/.claude.json` and `~/.claude.json.backup` contain onboarding/trust configuration THEN the system SHALL CONTINUE TO handle these files appropriately — `ensureClaudeOnboardingComplete()` already manages them correctly via merge, so they should not be moved/deleted by the new flow

3.6 WHEN environment variables (`ANTHROPIC_AUTH_TOKEN`, `ANTHROPIC_BASE_URL`, `ANTHROPIC_MODEL`, etc.) are set in the `env` map for the Claude Code process THEN the system SHALL CONTINUE TO pass them to the launched process as before
