# Implementation Plan: Capability Market Upgrade

## Overview

This implementation covers three core changes: (1) naming unification from "Skill Market" to "Capability Market" via aliases, (2) HubCenter admin search and import for Skills/MCP from external sources, and (3) MCP Server validation (connectivity, tool availability, schema correctness, runtime health). All changes use Go for backend, TypeScript/React for frontend, with backward-compatible aliases throughout.

## Tasks

- [x] 1. MCP Validation Data Types and Core Interface
  - [x] 1.1 Create `corelib/mcp/validation_report.go` with ValidationReport, ConnectivityResult, ToolAvailabilityResult, SchemaCorrectnessResult, SchemaError, RuntimeHealthResult structs
    - Define all JSON tags as specified in design
    - Include `OverallStatus` field with "pass"/"warn"/"fail" values
    - _Requirements: 13.3, 13.4_
  - [x] 1.2 Create `corelib/mcp/client.go` with MCPServerConfig struct and `sendMCPRequest` function
    - Support "sse" and "streamable-http" transports (HTTP-based only, no stdio for server-side)
    - Implement JSON-RPC message framing for MCP protocol
    - Include configurable headers and API key support
    - _Requirements: 9.1, 9.4_
  - [x] 1.3 Create `corelib/mcp/validator.go` with Validator struct implementing MCPValidator interface
    - Define `NewValidator()` constructor with 30s default total timeout
    - Stub out `Validate`, `CheckConnectivity`, `CheckToolAvailability`, `CheckSchemaCorrectness`, `CheckRuntimeHealth` methods
    - _Requirements: 13.1, 13.5_
  - [x]* 1.4 Write unit tests for ValidationReport JSON serialization and OverallStatus computation
    - Test pass/warn/fail status determination logic
    - Test nil RuntimeHealthResult handling
    - _Requirements: 13.3, 13.4_

- [x] 2. MCP Connectivity Validation
  - [x] 2.1 Implement `CheckConnectivity` in `corelib/mcp/validator.go`
    - Send MCP `initialize` request with 10s timeout
    - Return ConnectivityResult with latency measurement on success
    - Return descriptive error on timeout or malformed URL
    - _Requirements: 9.1, 9.2, 9.3, 9.4_
  - [x]* 2.2 Write unit tests for CheckConnectivity
    - Mock HTTP server for success/timeout/malformed URL scenarios
    - Verify latency measurement accuracy
    - _Requirements: 9.1, 9.2, 9.3, 9.4_

- [x] 3. MCP Tool Availability Validation
  - [x] 3.1 Implement `CheckToolAvailability` in `corelib/mcp/validator.go`
    - Invoke MCP `tools/list` method with 10s timeout
    - Parse tool list and extract tool names
    - Return warning for empty tool list
    - Return error details on `tools/list` failure
    - _Requirements: 10.1, 10.2, 10.3, 10.4_
  - [ ]* 3.2 Write unit tests for CheckToolAvailability
    - Mock server returning tools, empty list, and error
    - _Requirements: 10.1, 10.2, 10.3, 10.4_

- [x] 4. MCP Schema Correctness Validation
  - [x] 4.1 Implement `CheckSchemaCorrectness` in `corelib/mcp/validator.go`
    - Validate each tool's input schema structure (required params exist in properties)
    - Construct sample arguments from schema property definitions
    - Use existing `ValidateArgs` function for round-trip validation
    - Collect per-tool schema errors
    - _Requirements: 11.1, 11.2, 11.3, 11.4, 11.5, 14.1, 14.2, 14.3_
  - [x] 4.2 Implement `constructSampleArgs` helper function
    - Generate sample values based on JSON Schema types (string, number, boolean, object, array)
    - Handle nested schemas and required fields
    - _Requirements: 14.2_
  - [ ]* 4.3 Write property test for schema round-trip validation
    - **Property 4: Schema Round-Trip**
    - For any valid tool schema, `constructSampleArgs(schema)` SHALL produce arguments that pass `ValidateArgs(schema, args)` with zero validation errors
    - **Validates: Requirements 14.3**
  - [ ]* 4.4 Write unit tests for CheckSchemaCorrectness
    - Test valid schema, invalid schema syntax, missing required property definitions
    - _Requirements: 11.1, 11.2, 11.3, 11.4, 11.5_

- [x] 5. MCP Runtime Health Check
  - [x] 5.1 Implement `CheckRuntimeHealth` in `corelib/mcp/validator.go`
    - Implement `selectSafeHealthCheckTool` (prefer no required params → string params only → first tool)
    - Return `runtime_healthy: null` with note when no safe tool found
    - Invoke selected tool with 15s timeout and measure response time
    - _Requirements: 12.1, 12.2, 12.3, 12.4, 12.5_
  - [ ]* 5.2 Write unit tests for CheckRuntimeHealth
    - Test tool selection priority, success, timeout, and no-safe-tool scenarios
    - _Requirements: 12.1, 12.2, 12.3, 12.4, 12.5_

- [x] 6. Combined MCP Validation (Validate method)
  - [x] 6.1 Implement `Validate` method orchestrating all four checks in sequence
    - Execute checks in order: connectivity → tool availability → schema → runtime health
    - Short-circuit on connectivity failure (skip subsequent checks)
    - Compute `overall_status` based on check results
    - Enforce 30s total timeout
    - _Requirements: 13.1, 13.2, 13.3, 13.4, 13.5_
  - [ ]* 6.2 Write property test for validation idempotency
    - **Property 3: Validation Idempotency**
    - For the same MCP Server with unchanged state, two consecutive calls to `Validate()` SHALL return the same `overall_status` value
    - **Validates: Requirements 13.1**
  - [ ]* 6.3 Write integration test for combined validation flow
    - Mock MCP server with configurable behavior for all four checks
    - Verify short-circuit on connectivity failure
    - Verify overall_status computation
    - _Requirements: 13.1, 13.2, 13.3, 13.4, 13.5_

- [x] 7. Checkpoint - MCP Validator core complete
  - Ensure all tests pass, ask the user if questions arise.

- [x] 8. MCP Search Interface (ClawHub + GitHub)
  - [x] 8.1 Create `corelib/skill/hub_search_mcp.go` with MCP search functions
    - Implement `SearchMCPClawHub(ctx, query)` querying ClawHub API with `type=mcp` filter
    - Implement `SearchMCPGitHub(ctx, query)` using GitHub Repository Search API with `topic:mcp-server`
    - Return `[]HubSearchResult` with `capability_type: "mcp"` field
    - _Requirements: 7.3, 7.4, 8.3, 8.4_
  - [ ]* 8.2 Write unit tests for MCP search functions
    - Mock ClawHub and GitHub API responses
    - Verify result parsing and capability_type field
    - _Requirements: 7.3, 7.4, 8.3, 8.4_

- [x] 9. Naming Migration - Shared Libraries
  - [x] 9.1 Create `corelib/remote/capability_market_auth.go` with type aliases
    - Define `CapabilityMarketAuthClient = SkillMarketAuthClient` type alias
    - Define `CapabilityMarketAuthResult = SkillMarketAuthResult` type alias
    - Implement `NewCapabilityMarketAuthClient()` delegating to `NewSkillMarketAuthClient()`
    - Update error messages to reference "Capability Market"
    - _Requirements: 3.1, 3.2, 3.3, 3.4, 3.5_
  - [ ]* 9.2 Write property test for type alias equivalence
    - **Property 2: Type Alias Equivalence**
    - `CapabilityMarketAuthClient` and `SkillMarketAuthClient` SHALL be the same type (Go type alias), usable interchangeably in all call sites
    - **Validates: Requirements 3.5**

- [x] 10. Naming Migration - TUI Commands
  - [x] 10.1 Update `tui/commands/skillmarket.go` to add `capabilitymarket` command alias
    - Add `RunCapabilityMarket` function delegating to `RunSkillMarket`
    - Register both "capabilitymarket" and "skillmarket" command names in TUI app
    - Update help text and usage to reference "capability market"
    - _Requirements: 2.1, 2.2, 2.3_
  - [ ]* 10.2 Write unit test verifying both command names are accepted
    - _Requirements: 2.1_

- [x] 11. Naming Migration - GUI Frontend
  - [x] 11.1 Update `gui/frontend/src/components/remote/SkillsManagementPanel.tsx`
    - Replace all `localizeText` calls: "Skill Market"→"Capability Market", "技能市场"→"能力市场", "技能市場"→"能力市場"
    - _Requirements: 1.1, 1.3_
  - [x] 11.2 Update `iWorkerCenter/frontend/src/pages/CloudRegistrationPage.tsx`
    - Change module label from `skill_market` to `capability_market`
    - _Requirements: 1.2_
  - [x] 11.3 Rename `iWorkerCloud/web/admin/src/pages/SkillMarketPage.tsx` to `CapabilityMarketPage.tsx`
    - Update all internal user-visible strings to "Capability Market" / "能力市场"
    - Update route from "skillmarket" to "capabilitymarket" with old path as redirect
    - _Requirements: 5.1, 5.2, 5.3_

- [x] 12. Naming Migration - HubCenter Backend
  - [x] 12.1 Update `hubcenter/internal/httpapi/router.go` to register `/capabilitymarket` route alias
    - Retain `/skillmarket/` routes as aliases for backward compatibility
    - Register new `/capabilitymarket` static route pointing to same web directory
    - _Requirements: 4.2_
  - [x] 12.2 Update handler function names and log messages in `hubcenter/internal/httpapi/capability_market_handlers.go`
    - Rename handler functions to use "capabilitymarket" naming convention
    - Update all log messages and error strings to reference "capability market"
    - _Requirements: 4.1, 4.3_

- [x] 13. Naming Migration - Hub Backend
  - [x] 13.1 Update `hub/internal/httpapi/marketplace_handlers.go` comments and log messages
    - Update internal comments to reference "capability market"
    - Update log messages from "marketplace"/"skill market" to "capability market"
    - Retain existing API field names in current endpoints for backward compatibility
    - _Requirements: 6.1, 6.2, 6.3_

- [x] 14. Checkpoint - Naming migration complete
  - Ensure all tests pass, ask the user if questions arise.
  - [ ]* 14.1 Write property test for naming backward compatibility
    - **Property 1: Naming Backward Compatibility**
    - All legacy API paths (`/skillmarket/`, `/marketplace`) SHALL remain accessible and return identical results to new paths (`/capabilitymarket/`)
    - **Validates: Requirements 2.1, 4.2, 5.3**

- [x] 15. HubCenter Admin External Search - MCP Support
  - [x] 15.1 Extend `AdminCapabilityMarketExternalSearchHandler` in `hubcenter/internal/httpapi/capability_market_handlers.go`
    - Add `type=mcp` branch to existing handler
    - Call `SearchMCPClawHub` and `SearchMCPGitHub` based on `source` parameter
    - Return results with `capability_type: "mcp"` field
    - HubCenter SHALL NOT search itself as a source
    - _Requirements: 8.1, 8.2, 8.3, 8.4, 8.5_
  - [ ]* 15.2 Write property test for search source isolation
    - **Property 5: Search Source Isolation**
    - HubCenter admin search SHALL never return results with `source=hubcenter`. Hub admin search MAY return results with `source=hubcenter`
    - **Validates: Requirements 8.5, 7.2**

- [x] 16. HubCenter Admin Import Handler
  - [x] 16.1 Implement `AdminCapabilityMarketImportHandler` in `hubcenter/internal/httpapi/capability_market_handlers.go`
    - Accept POST with `capability_type`, `source`, `install_ref`, `run_validation` fields
    - Download capability package from source
    - For MCP with `run_validation=true`, execute MCP validation
    - Register in HubCenter with `pricing: free` and source attribution
    - Mark `validation_status: failed` if validation fails (do not block import)
    - _Requirements: 8.6, 8.7, 8.8, 8.9, 8.10_
  - [x] 16.2 Register import route in `hubcenter/internal/httpapi/router.go`
    - `POST /api/admin/capability-market/import` with admin auth middleware
    - _Requirements: 8.6_
  - [ ]* 16.3 Write property test for import pricing invariant
    - **Property 6: Import Pricing Invariant**
    - All capabilities imported via HubCenter admin import SHALL have `pricing` field set to `"free"` regardless of the source capability's original pricing
    - **Validates: Requirements 8.6**
  - [ ]* 16.4 Write unit tests for import handler
    - Test skill import, MCP import with validation pass, MCP import with validation fail (still imports)
    - _Requirements: 8.6, 8.7, 8.8, 8.9, 8.10_

- [x] 17. Hub Admin External Search - MCP Extension
  - [x] 17.1 Extend `AdminCapabilityExternalSearchHandler` in `hub/internal/httpapi/marketplace_handlers.go`
    - Add ClawHub and GitHub MCP search support for `type=mcp` + `source=clawhub`/`source=github`
    - Retain existing HubCenter MCP search for `type=mcp` + `source=hubcenter`
    - Return results with `capability_type` field indicating `skill` or `mcp`
    - _Requirements: 7.1, 7.2, 7.3, 7.4, 7.5, 7.6, 7.7_
  - [ ]* 17.2 Write unit tests for Hub admin MCP search
    - Test search with type=skill, type=mcp across different sources
    - _Requirements: 7.1, 7.2, 7.3, 7.4, 7.7_

- [x] 18. Hub Admin MCP Validation API Endpoint
  - [x] 18.1 Implement `AdminMCPValidateHandler` in `hub/internal/httpapi/marketplace_handlers.go`
    - Accept POST with endpoint_url, transport, headers, checks fields
    - Instantiate Validator and call Validate or individual check methods
    - Return ValidationReport as JSON response
    - _Requirements: 9.1, 10.1, 11.1, 12.1, 13.1_
  - [x] 18.2 Register validation routes in Hub and HubCenter routers
    - Hub: `POST /api/admin/capabilities/mcp/validate`
    - HubCenter: `POST /api/admin/capability-market/mcp/validate`
    - Both require admin auth middleware
    - _Requirements: 9.1, 13.1_
  - [ ]* 18.3 Write integration tests for validation API endpoints
    - Test full validation flow through HTTP endpoint
    - Test partial checks (connectivity only, tools only)
    - _Requirements: 13.1, 13.3_

- [x] 19. Checkpoint - All features integrated
  - Ensure all tests pass, ask the user if questions arise.

- [x] 20. Final wiring and backward compatibility verification
  - [x] 20.1 Verify all backward-compatible aliases work end-to-end
    - Test old TUI command name `skillmarket` still works
    - Test old API paths `/skillmarket/` still accessible
    - Test old type names `SkillMarketAuthClient` still compile
    - _Requirements: 2.1, 3.5, 4.2, 5.3_
  - [x] 20.2 Verify MCP validation integration with import flow
    - Test that import with `run_validation=true` executes validation and stores report
    - Test that validation failure marks `validation_status: failed` but allows import
    - _Requirements: 8.9, 8.10_

- [x] 21. Final checkpoint - Ensure all tests pass
  - Ensure all tests pass, ask the user if questions arise.

## Notes

- Tasks marked with `*` are optional and can be skipped for faster MVP
- Each task references specific requirements for traceability
- Checkpoints ensure incremental validation
- Property tests validate universal correctness properties from the design document
- Unit tests validate specific examples and edge cases
- All naming changes use aliases (type aliases, route aliases, command aliases) — no breaking renames
- MCP validation only supports HTTP-based transport (SSE / Streamable HTTP), not stdio
- Validation failure does not block import — marks as `validation_status: failed`
- HubCenter admin search never searches HubCenter itself as a source

## Task Dependency Graph

```json
{
  "waves": [
    { "id": 0, "tasks": ["1.1", "1.2", "9.1", "10.1", "11.1", "11.2", "11.3"] },
    { "id": 1, "tasks": ["1.3", "1.4", "9.2", "10.2", "12.1", "12.2", "13.1"] },
    { "id": 2, "tasks": ["2.1", "3.1", "8.1"] },
    { "id": 3, "tasks": ["2.2", "3.2", "4.1", "4.2", "8.2", "14.1"] },
    { "id": 4, "tasks": ["4.3", "4.4", "5.1"] },
    { "id": 5, "tasks": ["5.2", "6.1"] },
    { "id": 6, "tasks": ["6.2", "6.3", "15.1"] },
    { "id": 7, "tasks": ["15.2", "16.1", "17.1"] },
    { "id": 8, "tasks": ["16.2", "16.3", "16.4", "17.2", "18.1"] },
    { "id": 9, "tasks": ["18.2", "18.3"] },
    { "id": 10, "tasks": ["20.1", "20.2"] }
  ]
}
```
