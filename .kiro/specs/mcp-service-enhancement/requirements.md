# Requirements Document

## Introduction

MaClaw's MCP (Model Context Protocol) service layer currently exposes MCP tools to the LLM with minimal metadata and passes tool arguments through without validation. This leads to silent failures when the LLM guesses parameter names incorrectly (e.g., passing `query` instead of `search_query` for zhipu's `web_search_prime`), inconsistent response/error formats across different MCP servers, and no ability to search or filter the tool list. This feature enhances the MCP layer with rich parameter schema display, local argument validation, standardized response/error envelopes, and tool search/filtering capabilities.

## Glossary

- **MCP_Registry**: The `MCPRegistry` struct in `gui/app_nl_mcp.go` that manages remote HTTP-based MCP server connections, health checks, tool caching, and tool invocation via JSON-RPC.
- **Local_MCP_Manager**: The `LocalMCPManager` that manages local stdio-based MCP server processes and their tool lists.
- **Tool_Definition_Generator**: The `ToolDefinitionGenerator` in `gui/tool_definition_generator.go` that merges builtin tool definitions with dynamic MCP tool definitions for LLM consumption.
- **MCPToolView**: The struct representing an MCP tool with `Name`, `Description`, and `InputSchema` fields, cached from `tools/list` JSON-RPC responses.
- **InputSchema**: The JSON Schema object returned by an MCP server in the `tools/list` response, describing a tool's parameter names, types, required fields, enum values, and descriptions.
- **call_mcp_tool**: The builtin tool handler (`toolCallMCPTool`) that the LLM invokes to call a specific MCP server tool with arguments.
- **list_mcp_tools**: The builtin tool handler (`toolListMCPTools`) that lists all registered MCP servers and their tools.
- **Parameter_Validator**: A new component that validates tool arguments against the tool's InputSchema before sending the JSON-RPC `tools/call` request.
- **Standard_Response**: A unified response envelope structure that normalizes the heterogeneous outputs from different MCP servers into a consistent format.
- **Standard_Error**: A unified error object structure that normalizes the three different error formats (JSON-RPC error, HTTP error, server-specific error) into a consistent format.

## Requirements

### Requirement 1: Rich Parameter Schema Display in list_mcp_tools

**User Story:** As an LLM agent, I want to see each MCP tool's parameter names, types, required status, and enum values when listing tools, so that I can construct correct arguments without guessing parameter names.

#### Acceptance Criteria

1. WHEN `list_mcp_tools` is invoked, THE list_mcp_tools Handler SHALL display each tool's parameters including parameter name, type, required status, and description extracted from the tool's InputSchema.
2. WHEN a tool's InputSchema contains `enum` values for a parameter, THE list_mcp_tools Handler SHALL display the allowed enum values for that parameter.
3. WHEN a tool's InputSchema contains a `required` array, THE list_mcp_tools Handler SHALL mark each parameter as required or optional accordingly.
4. WHEN a tool's InputSchema is empty or missing, THE list_mcp_tools Handler SHALL display "(no parameters)" for that tool.
5. WHEN a tool's InputSchema contains nested object parameters, THE list_mcp_tools Handler SHALL display the top-level parameters with a note indicating nested structure.
6. THE list_mcp_tools Handler SHALL maintain backward compatibility by continuing to display server name, server ID, source type, and health status alongside the enhanced parameter information.

### Requirement 2: Rich Parameter Schema in LLM Tool Definitions

**User Story:** As an LLM agent, I want the dynamically generated MCP tool definitions to include full parameter schemas (names, types, required, enums), so that I can select correct parameter names and values at invocation time.

#### Acceptance Criteria

1. THE Tool_Definition_Generator SHALL pass through the complete InputSchema (including `properties`, `required`, `enum`, and `description` fields) when converting MCPToolView to OpenAI function calling format via `mcpToolToDefinition`.
2. WHEN an MCP tool's InputSchema contains a `required` array, THE Tool_Definition_Generator SHALL include the `required` array in the generated function parameters.
3. WHEN an MCP tool's InputSchema contains `enum` values within property definitions, THE Tool_Definition_Generator SHALL preserve the `enum` arrays in the generated function parameters.
4. WHEN an MCP tool's InputSchema contains `description` fields within property definitions, THE Tool_Definition_Generator SHALL preserve the `description` fields in the generated function parameters.
5. FOR ALL valid MCPToolView objects with non-empty InputSchema, converting to a tool definition and extracting the parameters SHALL produce a schema equivalent to the original InputSchema (round-trip property).

### Requirement 3: Local Parameter Validation Before MCP Tool Invocation

**User Story:** As an LLM agent, I want my tool arguments to be validated locally against the tool's InputSchema before the JSON-RPC request is sent, so that I receive immediate, actionable error messages instead of cryptic server-side errors.

#### Acceptance Criteria

1. WHEN `call_mcp_tool` is invoked, THE Parameter_Validator SHALL validate the provided arguments against the target tool's InputSchema before sending the JSON-RPC `tools/call` request.
2. WHEN a required parameter is missing from the arguments, THE Parameter_Validator SHALL return an error message listing the missing required parameter names and their expected types.
3. WHEN an argument value does not match the expected type declared in the InputSchema, THE Parameter_Validator SHALL return an error message identifying the parameter name, the expected type, and the actual type provided.
4. WHEN an argument value is not in the allowed `enum` values for a parameter, THE Parameter_Validator SHALL return an error message listing the parameter name and the allowed enum values.
5. WHEN validation fails, THE call_mcp_tool Handler SHALL return the validation error to the LLM without sending the JSON-RPC request to the MCP server.
6. WHEN validation succeeds, THE call_mcp_tool Handler SHALL proceed to send the JSON-RPC `tools/call` request with the original arguments unchanged.
7. WHEN the target tool's InputSchema is not available in the tools cache, THE call_mcp_tool Handler SHALL skip validation and pass arguments through to the MCP server (graceful degradation).
8. IF the Parameter_Validator encounters an internal error during validation, THEN THE call_mcp_tool Handler SHALL log the error and proceed with the MCP call without blocking (fail-open).

### Requirement 4: Standardized MCP Response Format

**User Story:** As an LLM agent, I want all MCP tool responses to follow a consistent structure regardless of which server produced them, so that I can reliably parse and act on the results.

#### Acceptance Criteria

1. THE MCP_Registry SHALL normalize all successful `tools/call` responses into a Standard_Response containing: `status` ("ok"), `server_id`, `tool_name`, and `result` (the original response content).
2. THE MCP_Registry SHALL normalize all error responses into a Standard_Error containing: `status` ("error"), `server_id`, `tool_name`, `error_code` (a category string), and `error_message` (a human-readable description).
3. WHEN a JSON-RPC error response is received (containing an `error` field with `code` and `message`), THE MCP_Registry SHALL extract the code and message into the Standard_Error format.
4. WHEN an HTTP-level error occurs (non-2xx status code), THE MCP_Registry SHALL map the HTTP status code to an appropriate `error_code` category and include the status code in the `error_message`.
5. WHEN a server returns a successful JSON-RPC response with `result.isError` set to true (MCP protocol error), THE MCP_Registry SHALL treat the response as an error and normalize it into the Standard_Error format.
6. THE call_mcp_tool Handler SHALL format the Standard_Response or Standard_Error as a human-readable string for the LLM, including the status, server identity, and result or error details.
7. FOR ALL MCP tool invocations (both successful and failed), the response returned to the LLM SHALL contain the `server_id` and `tool_name` fields to enable the LLM to correlate responses with requests.

### Requirement 5: Unified Error Format Across Error Sources

**User Story:** As an LLM agent, I want all MCP errors (JSON-RPC errors, HTTP errors, connection errors, validation errors) to follow the same structure, so that I can apply a single error-handling strategy.

#### Acceptance Criteria

1. THE Standard_Error SHALL use the following `error_code` categories: `validation_error` (local schema validation failure), `connection_error` (network/timeout), `auth_error` (401/403), `server_error` (5xx), `rpc_error` (JSON-RPC error response), `tool_error` (MCP `result.isError`), and `unknown_error` (unclassifiable).
2. WHEN a connection timeout occurs, THE MCP_Registry SHALL return a Standard_Error with `error_code` "connection_error" and an `error_message` describing the timeout.
3. WHEN an authentication failure occurs (HTTP 401 or 403), THE MCP_Registry SHALL return a Standard_Error with `error_code` "auth_error".
4. WHEN the MCP server returns HTTP 429, THE MCP_Registry SHALL return a Standard_Error with `error_code` "rate_limit" and an `error_message` advising to retry after a delay.
5. IF an unexpected error format is received from the MCP server, THEN THE MCP_Registry SHALL return a Standard_Error with `error_code` "unknown_error" and include the raw response body (truncated to 500 characters) in the `error_message`.

### Requirement 6: Tool Search and Filtering in list_mcp_tools

**User Story:** As an LLM agent, I want to search and filter MCP tools by keyword, server name, or function description, so that I can quickly find the right tool without scanning the entire list.

#### Acceptance Criteria

1. WHEN `list_mcp_tools` is invoked with a `query` parameter, THE list_mcp_tools Handler SHALL filter the tool list to include only tools whose name, description, or server name contains the query string (case-insensitive substring match).
2. WHEN `list_mcp_tools` is invoked with a `server_id` parameter, THE list_mcp_tools Handler SHALL filter the tool list to include only tools from the specified server.
3. WHEN `list_mcp_tools` is invoked without `query` or `server_id` parameters, THE list_mcp_tools Handler SHALL return the full tool list (backward compatible behavior).
4. WHEN the filtered result is empty, THE list_mcp_tools Handler SHALL return a message indicating no tools matched the filter criteria, along with the total number of available tools.
5. THE list_mcp_tools tool definition SHALL be updated to include `query` and `server_id` as optional parameters with descriptions.

### Requirement 7: GUI/TUI Parity

**User Story:** As a developer, I want the MCP enhancements to work consistently in both the GUI (desktop/IM) and TUI (terminal) interfaces, so that users have the same experience regardless of interface.

#### Acceptance Criteria

1. THE TUI `toolListMCPTools` handler SHALL display the same rich parameter schema information as the GUI handler.
2. THE TUI `toolCallMCPTool` handler SHALL perform the same local parameter validation as the GUI handler.
3. THE TUI `toolCallMCPTool` handler SHALL return responses and errors in the same Standard_Response and Standard_Error format as the GUI handler.
4. THE TUI `list_mcp_tools` tool definition SHALL include the same `query` and `server_id` optional parameters as the GUI tool definition.
5. WHEN shared validation or formatting logic is needed, THE implementation SHALL place the logic in the `corelib` package to avoid code duplication between GUI and TUI.
