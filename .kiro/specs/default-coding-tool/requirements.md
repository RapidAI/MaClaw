# Requirements Document

## Introduction

This feature adds a "Default AI Programming Tool" setting in Settings → Coding Tools (编程工具). The setting specifies which coding tool and provider the maclaw agent uses when it calls `create_session` to start a programming session on behalf of the user. Currently, when the LLM omits the `tool` parameter, `ResolveTool()` picks a tool via project-file heuristics (go.mod → opencode, package.json → claude, etc.) and falls back to "claude". This is unreliable — the user has no control over which tool or provider is chosen. With this feature, the user explicitly selects a default tool and provider in settings; the agent consults this preference first, falling back to brand-specific defaults and then heuristics only when the configured tool is unavailable.

## Glossary

- **App_Config**: The central application configuration structure (`corelib/app_config.go` `AppConfig`), persisted as JSON, holding all tool configs, project configs, and user preferences.
- **Tool_Catalog**: The registry of supported remote coding tools (`gui/remote_tool_catalog.go` `remoteToolCatalog`), mapping tool names to metadata including display names, binary names, and config selectors.
- **Tool_Config**: Per-tool configuration (`corelib/types.go` `ToolConfig`) containing `CurrentModel` (selected provider) and `Models` (list of `ModelConfig` entries).
- **Provider_Resolver**: The component (`gui/provider_resolver.go` `ProviderResolver`) that resolves which provider/model to use for a given tool, supporting explicit override, default, and fallback modes.
- **Session_Context_Resolver**: The component (`gui/session_context_resolver.go` `SessionContextResolver`) that auto-recommends a tool and project path when the user does not specify them explicitly.
- **Brand_Config**: Build-time brand configuration (`corelib/brand/brand.go` `BrandConfig`) identifying the product variant (MaClaw or TigerClaw/qianxin).
- **Default_Tool_Setting**: The new user-configurable preference for which coding tool to use by default when creating sessions.
- **Default_Provider_Setting**: The new user-configurable preference for which provider/model to use by default for the selected default tool.
- **Settings_UI**: The frontend settings panel (`gui/frontend/src/App.tsx`), specifically the "编程工具" (Coding Tools / Dev CLI) tab.
- **Extra_Tool**: An OEM-specific tool defined in `BrandConfig.ExtraTools` and stored in `AppConfig.ExtraToolConfigs`.

## Requirements

### Requirement 1: Persist Default Tool and Provider in Configuration

**User Story:** As a user, I want my default coding tool and provider preferences to be saved in the application configuration, so that they persist across application restarts.

#### Acceptance Criteria

1. THE App_Config SHALL include a `default_tool` string field that stores the user's preferred default coding tool name.
2. THE App_Config SHALL include a `default_tool_provider` string field that stores the user's preferred default provider name for the default tool.
3. WHEN the `default_tool` field is empty, THE App_Config SHALL treat the value as "no user preference set" (defer to brand defaults).
4. WHEN the `default_tool_provider` field is empty, THE App_Config SHALL treat the value as "no user provider preference set" (defer to the tool's `CurrentModel` or first available provider).
5. THE App_Config SHALL serialize both fields to JSON with keys `"default_tool"` and `"default_tool_provider"`.
6. WHEN the App_Config is loaded from a JSON file that does not contain the `default_tool` or `default_tool_provider` keys, THE App_Config SHALL default both fields to empty strings.

### Requirement 2: Brand-Specific Default Tool Fallback

**User Story:** As a product owner, I want each brand to have sensible default tool and provider settings, so that users get a working configuration out of the box without manual setup.

#### Acceptance Criteria

1. WHEN the user has not configured a `default_tool` preference (field is empty) AND the current brand is MaClaw, THE Session_Context_Resolver SHALL use `"claude"` as the default tool.
2. WHEN the user has not configured a `default_tool` preference (field is empty) AND the current brand is TigerClaw (ID: "qianxin"), THE Session_Context_Resolver SHALL use `"claude"` as the default tool.
3. WHEN the user has not configured a `default_tool_provider` preference (field is empty) AND the current brand is MaClaw, THE Provider_Resolver SHALL use the first configured provider in the default tool's model list.
4. WHEN the user has not configured a `default_tool_provider` preference (field is empty) AND the current brand is TigerClaw (ID: "qianxin"), THE Provider_Resolver SHALL use `"codegen"` as the default provider.
5. THE Brand_Config SHALL expose a method or fields that return the brand-specific default tool name and default provider name.

### Requirement 3: Default Tool Resolution Integration

**User Story:** As a user, I want the system to use my configured default tool when I create a coding session without specifying a tool, so that I do not have to select my preferred tool every time.

#### Acceptance Criteria

1. WHEN the `tool` parameter is empty in a session creation request AND the user has a non-empty `default_tool` configured, THE Session_Context_Resolver SHALL return the configured `default_tool` as the recommended tool.
2. WHEN the `tool` parameter is empty AND the user has a non-empty `default_tool` configured AND the configured tool is installed and healthy, THE Session_Context_Resolver SHALL prefer the configured default over heuristic-based recommendations.
3. WHEN the `tool` parameter is empty AND the user has a non-empty `default_tool` configured AND the configured tool is NOT installed or NOT healthy, THE Session_Context_Resolver SHALL fall back to the existing heuristic-based tool resolution.
4. WHEN the `tool` parameter is empty AND the user has no `default_tool` configured (empty), THE Session_Context_Resolver SHALL fall back to brand-specific defaults, then to heuristic-based resolution.
5. WHEN the `tool` parameter is explicitly provided (non-empty), THE Session_Context_Resolver SHALL use the explicitly provided tool and ignore the default tool setting.

### Requirement 4: Default Provider Resolution Integration

**User Story:** As a user, I want the system to use my configured default provider for my default tool, so that sessions start with my preferred service provider without manual selection.

#### Acceptance Criteria

1. WHEN the `provider` parameter is empty in a session creation request AND the resolved tool matches the user's `default_tool` AND `default_tool_provider` is non-empty, THE Provider_Resolver SHALL use `default_tool_provider` as the preferred provider.
2. WHEN the `default_tool_provider` is non-empty AND the specified provider exists in the tool's model list AND the provider is valid (has API key or is builtin), THE Provider_Resolver SHALL select that provider.
3. WHEN the `default_tool_provider` is non-empty AND the specified provider does NOT exist in the tool's model list or is NOT valid, THE Provider_Resolver SHALL fall back to the tool's `CurrentModel` and then to the standard fallback chain.
4. WHEN the `provider` parameter is explicitly provided (non-empty), THE Provider_Resolver SHALL use the explicitly provided provider and ignore the default provider setting.
5. WHEN the resolved tool does NOT match the user's `default_tool`, THE Provider_Resolver SHALL ignore `default_tool_provider` and use the tool's own `CurrentModel` or fallback chain.

### Requirement 5: Settings UI for Default Tool Selection

**User Story:** As a user, I want a dropdown in Settings → Coding Tools to select my default programming tool, so that I can easily configure my preference through the GUI.

#### Acceptance Criteria

1. THE Settings_UI SHALL display a "Default AI Programming Tool" (默认编程工具) dropdown in the Coding Tools (编程工具) settings tab.
2. THE Settings_UI SHALL populate the dropdown with all visible and installed coding tools from the Tool_Catalog, plus a "Auto (brand default)" option representing no user preference.
3. WHEN the user selects a tool from the dropdown, THE Settings_UI SHALL save the selection to `App_Config.default_tool` and persist the configuration.
4. WHEN the user selects "Auto (brand default)", THE Settings_UI SHALL set `App_Config.default_tool` to an empty string.
5. THE Settings_UI SHALL display the current `default_tool` value as the selected option when the settings panel opens.
6. WHEN a tool in the dropdown is not installed, THE Settings_UI SHALL display the tool name with a "(not installed)" suffix and disable selection of that tool.

### Requirement 6: Settings UI for Default Provider Selection

**User Story:** As a user, I want a dropdown to select the default provider for my chosen default tool, so that I can control which service provider is used by default.

#### Acceptance Criteria

1. WHEN a specific tool is selected as the default tool (not "Auto"), THE Settings_UI SHALL display a "Default Provider" (默认服务商) dropdown below the tool dropdown.
2. THE Settings_UI SHALL populate the provider dropdown with all providers configured in the selected tool's model list, plus an "Auto (first available)" option.
3. WHEN the user selects a provider, THE Settings_UI SHALL save the selection to `App_Config.default_tool_provider` and persist the configuration.
4. WHEN the user selects "Auto (first available)", THE Settings_UI SHALL set `App_Config.default_tool_provider` to an empty string.
5. WHEN the user changes the default tool selection, THE Settings_UI SHALL reset the provider dropdown to "Auto (first available)" and clear `App_Config.default_tool_provider`.
6. WHEN "Auto (brand default)" is selected as the default tool, THE Settings_UI SHALL hide the provider dropdown.

### Requirement 7: Validation of Configured Defaults

**User Story:** As a user, I want the system to gracefully handle cases where my configured default tool or provider becomes unavailable, so that session creation does not fail unexpectedly.

#### Acceptance Criteria

1. WHEN the configured `default_tool` refers to a tool name that is not in the Tool_Catalog (e.g., tool was removed or config was manually edited), THE Session_Context_Resolver SHALL log a warning and fall back to brand defaults.
2. WHEN the configured `default_tool` refers to a tool that exists in the catalog but is not visible (hidden in settings), THE Session_Context_Resolver SHALL still use the configured default (visibility controls tab display, not default resolution).
3. WHEN the configured `default_tool_provider` refers to a provider that no longer exists in the tool's model list, THE Provider_Resolver SHALL log a warning and fall back to the tool's `CurrentModel` or first available provider.
4. IF the configured `default_tool` contains leading or trailing whitespace, THEN THE Session_Context_Resolver SHALL trim the value before lookup.
5. IF the configured `default_tool` contains uppercase characters, THEN THE Session_Context_Resolver SHALL normalize the value to lowercase before lookup.

### Requirement 8: Extra Tool (OEM) Compatibility

**User Story:** As a TigerClaw user, I want to be able to set OEM-specific tools (like TigerClaw Code) as my default coding tool, so that the default tool setting works with brand-specific extra tools.

#### Acceptance Criteria

1. THE Settings_UI SHALL include Extra_Tool entries (from `BrandConfig.ExtraTools`) in the default tool dropdown alongside built-in tools.
2. WHEN an Extra_Tool is selected as the default tool, THE App_Config SHALL store the Extra_Tool's `Name` field value in `default_tool`.
3. WHEN the configured `default_tool` matches an Extra_Tool name, THE Session_Context_Resolver SHALL resolve the tool through the Extra_Tool lookup path in the Tool_Catalog.
4. WHEN the configured `default_tool` matches an Extra_Tool name, THE Provider_Resolver SHALL resolve providers from `App_Config.ExtraToolConfigs[configKey]`.
