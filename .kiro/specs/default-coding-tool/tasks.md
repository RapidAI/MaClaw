# Implementation Plan: Default Coding Tool Setting

## Overview

Add user-configurable default coding tool and provider preferences with a three-tier resolution strategy: user config → brand defaults → heuristics. Backend changes in Go (config fields, brand defaults, resolution logic, Wails binding), frontend changes in TypeScript/React (settings dropdowns). Property-based tests validate the 8 correctness properties from the design.

## Tasks

- [x] 1. Add default tool config fields and brand defaults
  - [x] 1.1 Add `DefaultTool` and `DefaultToolProvider` fields to `AppConfig`
    - Add `DefaultTool string` with JSON tag `"default_tool"` and `DefaultToolProvider string` with JSON tag `"default_tool_provider"` to the `AppConfig` struct in `corelib/app_config.go`
    - No custom marshal/unmarshal logic needed — Go's `encoding/json` defaults missing string fields to `""`
    - _Requirements: 1.1, 1.2, 1.5, 1.6_

  - [x] 1.2 Add `DefaultTool` and `DefaultToolProvider` fields to `BrandConfig`
    - Add `DefaultTool string` and `DefaultToolProvider string` fields to the `BrandConfig` struct in `corelib/brand/brand.go`
    - _Requirements: 2.5_

  - [x] 1.3 Set MaClaw brand defaults
    - In `corelib/brand/brand_default.go`, set `DefaultTool: "claude"` and `DefaultToolProvider: ""` in the `init()` function
    - _Requirements: 2.1, 2.3_

  - [x] 1.4 Set TigerClaw brand defaults
    - In `corelib/brand/brand_qianxin.go`, set `DefaultTool: "claude"` and `DefaultToolProvider: "codegen"` in the `init()` function
    - _Requirements: 2.2, 2.4_

  - [ ]* 1.5 Write property test for AppConfig JSON round-trip (Property 1)
    - **Property 1: AppConfig default tool fields round-trip through JSON**
    - Generate random `AppConfig` instances with random `DefaultTool`/`DefaultToolProvider` strings, verify JSON marshal/unmarshal preserves values. Also test deserialization of JSON without these keys produces empty strings.
    - Use `testing/quick` or `rapid` with minimum 100 iterations
    - Test file: `corelib/app_config_property_test.go`
    - **Validates: Requirements 1.1, 1.2, 1.5, 1.6**

  - [ ]* 1.6 Write unit tests for brand defaults
    - Verify MaClaw brand returns `DefaultTool: "claude"`, `DefaultToolProvider: ""`
    - Verify TigerClaw brand returns `DefaultTool: "claude"`, `DefaultToolProvider: "codegen"`
    - Test file: `corelib/brand/brand_test.go`
    - _Requirements: 2.1, 2.2, 2.3, 2.4, 2.5_

- [x] 2. Checkpoint - Ensure all tests pass
  - Ensure all tests pass, ask the user if questions arise.

- [x] 3. Implement three-tier ResolveTool integration
  - [x] 3.1 Modify `SessionContextResolver.ResolveTool()` with three-tier resolution
    - In `gui/session_context_resolver.go`, add the three-tier resolution logic at the top of `ResolveTool()` before the existing heuristic logic:
      1. Load `AppConfig`, read `cfg.DefaultTool`, trim + lowercase
      2. If non-empty AND tool exists in catalog (via `lookupRemoteToolMetadata`) AND tool is installed+healthy (via `ToolManager.GetToolStatus`): return `(defaultTool, "用户配置的默认工具")`
      3. If non-empty but tool not available: log warning, fall through
      4. Read `brand.Current().DefaultTool`, check installed+healthy: return `(brandDefault, "品牌默认工具")`
      5. If brand default not available: fall through to existing heuristic logic
    - Handle Extra Tools: if `DefaultTool` matches an `ExtraToolDef.Name`, resolve through the extra tool lookup path
    - Normalize with `strings.TrimSpace` + `strings.ToLower` before lookup
    - _Requirements: 3.1, 3.2, 3.3, 3.4, 3.5, 7.1, 7.2, 7.4, 7.5, 8.3_

  - [ ]* 3.2 Write property test: configured default tool preferred when available (Property 2)
    - **Property 2: Configured default tool is preferred when available**
    - Generate random tool names from the catalog, mock `ToolManager.GetToolStatus` to return installed+healthy, verify `ResolveTool` returns the configured default.
    - Test file: `gui/session_context_resolver_property_test.go`
    - **Validates: Requirements 3.1, 3.2**

  - [ ]* 3.3 Write property test: unavailable default tool falls back gracefully (Property 3)
    - **Property 3: Unavailable default tool falls back gracefully**
    - Generate random strings (including catalog names), mock tool status as not-installed, verify `ResolveTool` never returns the configured default.
    - Test file: `gui/session_context_resolver_property_test.go`
    - **Validates: Requirements 3.3, 7.1**

  - [ ]* 3.4 Write property test: explicit tool parameter overrides all defaults (Property 4)
    - **Property 4: Explicit tool parameter overrides all defaults**
    - Generate random (explicitTool, defaultTool) pairs, verify the explicit tool always wins in session creation callers.
    - Test file: `gui/session_context_resolver_property_test.go`
    - **Validates: Requirements 3.5**

  - [ ]* 3.5 Write unit tests for ResolveTool edge cases
    - Test whitespace/case normalization: `"  Claude  "` → `"claude"`
    - Test hidden tool still usable: tool with `ShowXxx: false` but installed → still returned as default
    - Test unknown tool name falls through to brand defaults
    - Test Extra Tool resolution for TigerClaw brand
    - Test file: `gui/session_context_resolver_test.go`
    - _Requirements: 7.1, 7.2, 7.4, 7.5, 8.3_

- [x] 4. Inject default provider in session creation paths
  - [x] 4.1 Inject default provider in `toolCreateSession()` (`gui/im_tools_session.go`)
    - After `ResolveTool` returns the tool name and before `ProviderResolver.Resolve()`, add logic:
      - If `provider == ""` and `cfg.DefaultTool` is non-empty
      - Normalize both resolved tool and `cfg.DefaultTool` to lowercase/trimmed
      - If they match and `cfg.DefaultToolProvider` is non-empty, set `provider = cfg.DefaultToolProvider`
    - Wrap the `resolver.Resolve(toolCfg, provider)` call with graceful fallback: if the default provider override fails, retry with empty override
    - _Requirements: 4.1, 4.2, 4.3, 4.4, 4.5_

  - [x] 4.2 Inject default provider in `CodingSessionStarter.Start()` (`gui/session_orchestrator.go`)
    - Apply the same default provider injection logic as in 4.1, before the `resolver.Resolve(toolCfg, req.Provider)` call
    - If `req.Provider == ""` and resolved tool matches `cfg.DefaultTool`, use `cfg.DefaultToolProvider` as the provider override
    - Wrap with graceful fallback on invalid default provider
    - _Requirements: 4.1, 4.2, 4.3, 4.4, 4.5_

  - [ ]* 4.3 Write property test: default provider used when tool matches and provider is valid (Property 5)
    - **Property 5: Default provider is used when tool matches and provider is valid**
    - Generate random valid provider names from a ToolConfig's model list, verify provider resolution returns the configured default.
    - Test file: `gui/provider_default_property_test.go`
    - **Validates: Requirements 4.1, 4.2**

  - [ ]* 4.4 Write property test: invalid default provider falls back to auto-resolution (Property 6)
    - **Property 6: Invalid default provider falls back to auto-resolution**
    - Generate random provider names NOT in the model list, verify fallback to CurrentModel or first available.
    - Test file: `gui/provider_default_property_test.go`
    - **Validates: Requirements 4.3, 7.3**

  - [ ]* 4.5 Write property test: explicit provider parameter overrides default provider (Property 7)
    - **Property 7: Explicit provider parameter overrides default provider**
    - Generate random (explicitProvider, defaultProvider) pairs, verify explicit always wins.
    - Test file: `gui/provider_default_property_test.go`
    - **Validates: Requirements 4.4**

  - [ ]* 4.6 Write property test: default provider ignored when tool does not match (Property 8)
    - **Property 8: Default provider is ignored when tool does not match**
    - Generate random (resolvedTool, defaultTool) pairs where they differ, verify DefaultToolProvider is not used.
    - Test file: `gui/provider_default_property_test.go`
    - **Validates: Requirements 4.5**

- [x] 5. Checkpoint - Ensure all tests pass
  - Ensure all tests pass, ask the user if questions arise.

- [x] 6. Add Wails binding for frontend
  - [x] 6.1 Add `ListToolProviders` Wails binding and `ToolProviderView` type
    - In `gui/app.go` (or a new file `gui/app_tool_providers.go`), add:
      - `ToolProviderView` struct with `Name string`, `Valid bool`, `Builtin bool` (JSON-tagged)
      - `func (a *App) ListToolProviders(toolName string) []ToolProviderView` method
    - Implementation: load config, call `remoteToolConfig(cfg, toolName)`, iterate `toolCfg.Models`, build `ToolProviderView` slice
    - Handle Extra Tools: if `toolName` matches an ExtraTool, look up from `cfg.ExtraToolConfigs`
    - Return empty slice for invalid/unknown tool names
    - _Requirements: 6.1, 6.2, 8.4_

  - [ ]* 6.2 Write unit tests for `ListToolProviders`
    - Test returns correct provider list for a known tool
    - Test returns empty slice for unknown tool name
    - Test includes Extra Tool providers from `ExtraToolConfigs`
    - Test file: `gui/app_tool_providers_test.go`
    - _Requirements: 6.1, 6.2, 8.4_

- [x] 7. Implement frontend Settings UI
  - [x] 7.1 Add default tool dropdown in Settings → Coding Tools tab
    - In `gui/frontend/src/App.tsx`, in the `settingsTab === 'display'` panel, add a "Default Coding Tool" (默认编程工具) section above the existing "Tool Visibility" section
    - Populate dropdown with "Auto (品牌默认)" option + all tools from `ListRemoteToolMetadata()` where `installed == true`
    - Show not-installed tools with "(未安装)" suffix, disabled
    - Include Extra Tool entries from the tool metadata list
    - On selection: save to `config.default_tool` via `SaveConfig`, reset provider to ""
    - On "Auto" selection: set `config.default_tool` to empty string
    - Display current `config.default_tool` as selected option on load
    - Run `wails generate` (or equivalent) to regenerate TypeScript bindings for the new `ListToolProviders` and `ToolProviderView` types
    - _Requirements: 5.1, 5.2, 5.3, 5.4, 5.5, 5.6, 8.1, 8.2_

  - [x] 7.2 Add default provider dropdown in Settings → Coding Tools tab
    - Below the tool dropdown, add a "Default Provider" (默认服务商) dropdown
    - Only visible when a specific tool is selected (not "Auto")
    - Populate with "Auto (自动选择)" + providers from `ListToolProviders(selectedTool)`
    - On selection: save to `config.default_tool_provider` via `SaveConfig`
    - On "Auto" selection: set `config.default_tool_provider` to empty string
    - Reset to "Auto" when tool selection changes (cascading behavior)
    - _Requirements: 6.1, 6.2, 6.3, 6.4, 6.5, 6.6_

- [x] 8. Final checkpoint - Ensure all tests pass
  - Ensure all tests pass, ask the user if questions arise.

## Notes

- Tasks marked with `*` are optional and can be skipped for faster MVP
- Each task references specific requirements for traceability
- Checkpoints ensure incremental validation
- Property tests validate the 8 universal correctness properties from the design document
- Unit tests validate specific examples and edge cases
- The design uses Go (backend) and TypeScript/React (frontend) — no language selection needed
- Backend changes should be built and verified with `go build ./...` after each task group
- Frontend changes should be verified with the Wails TypeScript binding generation step
