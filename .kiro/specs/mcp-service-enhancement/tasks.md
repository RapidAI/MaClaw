# Implementation Plan: MCP Service Enhancement

## Overview

Implement shared MCP service logic in `corelib/mcp/` (validation, formatting, response normalization, error classification, filtering), fix `buildParametersFromSchema` schema preservation, then integrate into GUI and TUI handlers. Tasks are ordered by dependency: corelib package first, then schema fix, then GUI/TUI integration, then tool definitions, then tests.

## Tasks

- [x] 1. Create `corelib/mcp/` package with core types and validation
  - [x] 1.1 Create `corelib/mcp/validate.go` with `ValidationError` struct and `ValidateArgs` function
    - Define `ValidationError` struct with `Param`, `Code`, `Expected`, `Actual`, `Message` fields
    - Implement `ValidateArgs(schema, args)` with three checks: required parameters, type checking (Go→JSON type mapping), enum validation
    - Return nil for nil/empty schema (graceful degradation)
    - Use `recover()` to catch panics and return nil (fail-open)
    - _Requirements: 3.1, 3.2, 3.3, 3.4, 3.7, 3.8_

  - [ ]* 1.2 Write unit tests for `ValidateArgs`
    - Test nil/empty schema returns nil (Req 3.7)
    - Test fail-open on internal error — no panic (Req 3.8)
    - Test missing required parameter produces `missing_required` error
    - Test type mismatch produces `type_mismatch` error
    - Test invalid enum value produces `invalid_enum` error
    - _Requirements: 3.2, 3.3, 3.4, 3.7, 3.8_

  - [ ]* 1.3 Write property test for validation error completeness
    - **Property 3: Validation error completeness**
    - Generate random schemas with required fields, typed properties, and enum constraints + args with deliberate violations
    - Assert `ValidateArgs` returns non-empty `[]ValidationError` with correct param names, error codes, and expected/actual values
    - Minimum 100 iterations
    - **Validates: Requirements 3.2, 3.3, 3.4**

- [x] 2. Create `corelib/mcp/format.go` with parameter formatting and tool list formatting
  - [x] 2.1 Implement `FormatToolParameters` and `FormatToolList` functions
    - `FormatToolParameters(schema)` returns human-readable parameter list string
    - Return "(no parameters)" for nil/empty schemas
    - Display parameter name, type, required/optional marker, description, enum values
    - Mark nested object parameters with "[nested object]" note
    - `FormatToolList(entries, query, serverID)` formats filtered tool list with server metadata
    - _Requirements: 1.1, 1.2, 1.3, 1.4, 1.5, 1.6_

  - [ ]* 2.2 Write unit tests for formatting functions
    - Test empty/nil schema returns "(no parameters)" (Req 1.4)
    - Test backward-compatible list output includes server name, server ID, source type, health status (Req 1.6)
    - Test nested object parameter displays "[nested object]" note (Req 1.5)
    - _Requirements: 1.4, 1.5, 1.6_

  - [ ]* 2.3 Write property test for schema formatting completeness
    - **Property 1: Schema formatting completeness**
    - Generate random InputSchema maps with 1-10 properties, random types, random required subsets, random enum arrays, random description strings
    - Assert output contains every property name, type, correct required/optional marker, every enum value, and every description
    - Minimum 100 iterations
    - **Validates: Requirements 1.1, 1.2, 1.3, 1.5**

- [x] 3. Create `corelib/mcp/classify.go` with error classification
  - [x] 3.1 Implement error code constants and `ClassifyError` function
    - Define 8 error code constants: `ErrValidation`, `ErrConnection`, `ErrAuth`, `ErrRateLimit`, `ErrServer`, `ErrRPC`, `ErrTool`, `ErrUnknown`
    - Implement `ClassifyError(err, httpStatusCode, rawBody)` with ordered classification rules per design table
    - Truncate raw body to 500 characters for `unknown_error`
    - _Requirements: 5.1, 5.2, 5.3, 5.4, 5.5_

  - [ ]* 3.2 Write unit tests for error classification
    - Test timeout/"deadline exceeded" → `connection_error` (Req 5.2)
    - Test HTTP 401/403 → `auth_error` (Req 5.3)
    - Test HTTP 429 → `rate_limit` (Req 5.4)
    - Test body > 500 chars truncated in `unknown_error` (Req 5.5)
    - Test "connection refused"/"no such host" → `connection_error`
    - Test HTTP 500+ → `server_error`
    - _Requirements: 5.1, 5.2, 5.3, 5.4, 5.5_

  - [ ]* 3.3 Write property test for error classification bounded range
    - **Property 5: Error classification bounded range**
    - Generate random error messages, HTTP status codes (100-599), random body strings
    - Assert `ClassifyError` always returns one of the 8 defined error code categories
    - Minimum 100 iterations
    - **Validates: Requirements 5.1, 4.4**

- [x] 4. Create `corelib/mcp/response.go` with response normalization
  - [x] 4.1 Implement `StandardResponse`, `StandardError`, `NormalizeResponse`, and `FormatForLLM`
    - Define `StandardResponse` struct with Status/ServerID/ToolName/Result fields
    - Define `StandardError` struct with Status/ServerID/ToolName/ErrorCode/ErrorMessage fields
    - `NormalizeResponse(serverID, toolName, rawResponse)` detects `result.isError=true` and converts to `StandardError`
    - `FormatForLLM(resp, err)` formats as human-readable string for LLM consumption
    - _Requirements: 4.1, 4.2, 4.5, 4.6, 4.7_

  - [ ]* 4.2 Write unit tests for response normalization
    - Test `result.isError=true` detection → `StandardError` with `tool_error` code (Req 4.5)
    - Test successful response wrapping includes server_id and tool_name (Req 4.7)
    - Test `FormatForLLM` output includes status, server identity, and result/error details (Req 4.6)
    - _Requirements: 4.1, 4.2, 4.5, 4.6, 4.7_

  - [ ]* 4.3 Write property test for response normalization invariant
    - **Property 4: Response normalization invariant**
    - Generate random server_id/tool_name + random success/error payloads
    - Assert output always contains non-empty server_id, tool_name, status ("ok" or "error"), and either result or error_code+error_message
    - Minimum 100 iterations
    - **Validates: Requirements 4.1, 4.2, 4.7**

- [x] 5. Create `corelib/mcp/filter.go` with tool search and filtering
  - [x] 5.1 Implement `ToolEntry` struct and `FilterTools` function
    - Define `ToolEntry` with ServerName/ServerID/SourceType/HealthStatus/ToolName/Description/InputSchema
    - `FilterTools(entries, query, serverID)` filters by case-insensitive substring match on name/description/server name and/or exact serverID match
    - Return all entries when both query and serverID are empty
    - _Requirements: 6.1, 6.2, 6.3_

  - [ ]* 5.2 Write unit tests for filtering
    - Test empty query + empty serverID returns full list (Req 6.3)
    - Test empty filter result returns message with total count (Req 6.4)
    - Test case-insensitive substring matching on tool name, description, server name
    - _Requirements: 6.1, 6.2, 6.3, 6.4_

  - [ ]* 5.3 Write property test for text search filter correctness
    - **Property 6: Text search filter correctness**
    - Generate random ToolEntry slices (1-20 entries) + random query strings
    - Assert every returned entry has query as case-insensitive substring of ToolName, Description, or ServerName; no excluded entry matches
    - Minimum 100 iterations
    - **Validates: Requirements 6.1**

  - [ ]* 5.4 Write property test for server ID filter correctness
    - **Property 7: Server ID filter correctness**
    - Generate random ToolEntry slices (1-20 entries) + random serverID from the entries
    - Assert every returned entry has matching ServerID; no excluded entry has matching ServerID
    - Minimum 100 iterations
    - **Validates: Requirements 6.2**

- [x] 6. Checkpoint — Ensure all corelib/mcp tests pass
  - Ensure all tests pass, ask the user if questions arise.

- [x] 7. Fix `buildParametersFromSchema` in `gui/tool_definition_generator.go`
  - [x] 7.1 Fix the `looksLikePropertiesMap` branch to preserve `required`, `enum`, `description` fields
    - In the `looksLikePropertiesMap` fallback, separate non-object top-level keys (e.g., `required`, `additionalProperties`) from property definitions
    - Move non-object keys to the result map and remove them from the `properties` sub-map
    - Verify the JSON marshal/unmarshal fallback path already preserves all fields (no change needed)
    - _Requirements: 2.1, 2.2, 2.3, 2.4_

  - [ ]* 7.2 Write property test for schema round-trip preservation
    - **Property 2: Schema round-trip preservation**
    - Generate random MCPToolView with valid InputSchema (type:"object", 1-8 properties with types/required/enum/description)
    - Assert converting to tool definition via `mcpToolToDefinition` and extracting parameters produces a map deeply equal to the original InputSchema
    - Minimum 100 iterations
    - **Validates: Requirements 2.1, 2.2, 2.3, 2.4, 2.5**

- [x] 8. Enhance GUI `toolListMCPTools` handler
  - [x] 8.1 Update GUI `toolListMCPTools` to use `corelib/mcp` formatting and filtering
    - Import `corelib/mcp` package
    - Build `[]ToolEntry` from MCPRegistry and LocalMCPManager tool caches
    - Call `FilterTools(entries, query, serverID)` with parameters from tool arguments
    - Call `FormatToolList` to produce the output string with rich parameter schemas
    - Return "no tools matched" message with total count when filter result is empty
    - _Requirements: 1.1, 1.2, 1.3, 1.4, 1.5, 1.6, 6.1, 6.2, 6.3, 6.4_

- [x] 9. Enhance GUI `toolCallMCPTool` handler
  - [x] 9.1 Add local validation and response normalization to GUI `toolCallMCPTool`
    - Look up target tool's InputSchema from tools cache
    - Call `ValidateArgs(schema, args)` — if validation errors, format as `StandardError` with `error_code: "validation_error"` and return without RPC call
    - Skip validation if schema not in cache (graceful degradation)
    - After RPC call, use `NormalizeResponse` for success or `ClassifyError` + `StandardError` for errors
    - Call `FormatForLLM` to produce the final response string
    - _Requirements: 3.1, 3.5, 3.6, 3.7, 3.8, 4.1, 4.2, 4.3, 4.4, 4.5, 4.6, 4.7, 5.1, 5.2, 5.3, 5.4, 5.5_

- [x] 10. Enhance TUI `toolListMCPTools` handler
  - [x] 10.1 Update TUI `toolListMCPTools` to use `corelib/mcp` formatting and filtering
    - Import `corelib/mcp` package
    - Build `[]ToolEntry` from MCPRegistry and LocalMCPManager tool caches (same logic as GUI)
    - Call `FilterTools` and `FormatToolList` for output
    - _Requirements: 7.1, 7.4, 7.5_

- [x] 11. Enhance TUI `toolCallMCPTool` handler
  - [x] 11.1 Replace TUI stub with full validation + normalization using `corelib/mcp`
    - Remove "暂不支持直接调用" stub
    - Implement same validation → RPC → normalization flow as GUI handler using shared `corelib/mcp` functions
    - _Requirements: 7.2, 7.3, 7.5_

- [x] 12. Update tool definitions for `list_mcp_tools`
  - [x] 12.1 Add `query` and `server_id` optional parameters to GUI `list_mcp_tools` definition
    - Update `gui/im_tool_definitions.go` to add `query` (string, search keyword) and `server_id` (string, filter by server) parameters
    - _Requirements: 6.5_

  - [x] 12.2 Add `query` and `server_id` optional parameters to TUI `list_mcp_tools` definition
    - Update `tui/agent_handler.go` to add identical `query` and `server_id` parameters
    - _Requirements: 7.4_

  - [ ]* 12.3 Write unit test verifying tool definition has query/server_id parameters
    - Verify GUI `list_mcp_tools` definition includes `query` and `server_id` parameters
    - _Requirements: 6.5_

- [x] 13. Checkpoint — Ensure all tests pass
  - Ensure all tests pass, ask the user if questions arise.

- [ ]* 14. Write integration tests
  - [ ]* 14.1 Write integration test for `toolCallMCPTool` with mock MCP server
    - Test end-to-end flow: validation → RPC → normalization for success, validation error, RPC error, HTTP error, and `result.isError` scenarios
    - _Requirements: 3.1, 3.5, 3.6, 4.1, 4.2, 4.3, 4.5_

  - [ ]* 14.2 Write integration test for `toolListMCPTools` with mock registry
    - Test end-to-end flow: multiple servers with rich schemas, query filtering, server_id filtering, empty result
    - _Requirements: 1.1, 1.6, 6.1, 6.2, 6.4_

  - [ ]* 14.3 Write TUI/GUI parity test
    - Verify both GUI and TUI handlers call the same `corelib/mcp` functions for validation, formatting, and normalization
    - _Requirements: 7.1, 7.2, 7.3, 7.4, 7.5_

- [x] 15. Final checkpoint — Ensure all tests pass
  - Ensure all tests pass, ask the user if questions arise.

## Notes

- Tasks marked with `*` are optional and can be skipped for faster MVP
- Each task references specific requirements for traceability
- Checkpoints ensure incremental validation
- Property tests validate universal correctness properties from the design document (Properties 1-7)
- Unit tests validate specific examples and edge cases
- All shared logic lives in `corelib/mcp/` to ensure GUI/TUI parity (Requirement 7.5)
