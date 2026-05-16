# Requirements Document

## Introduction

This feature upgrades the existing "技能市场" (Skill Market / Marketplace) across the entire codebase to "能力市场" (Capability Market), adds Hub admin backend capabilities for searching and adding both Skills and MCP Servers from external sources, and introduces MCP Server validation capabilities for verifying connectivity, tool availability, schema correctness, and runtime health.

## Glossary

- **Capability_Market**: The unified market abstraction (能力市场) that replaces the former Skill Market / Marketplace terminology across all system components
- **Hub**: Enterprise-level server that manages capabilities, users, and policies for an organization
- **HubCenter**: Public capability marketplace that provides discovery, distribution, and commercial capabilities
- **MCP_Server**: Model Context Protocol server — a tool service that exposes tools, resources, and prompts to AI agents
- **MCP_Validator**: Component responsible for verifying MCP Server connectivity, tool availability, schema correctness, and runtime health
- **Admin_Search_Handler**: Backend handler that enables Hub administrators to search external sources (HubCenter, ClawHub, GitHub) for Skills and MCP Servers
- **HubCenter_Admin**: HubCenter administrator who can search ClawHub/GitHub for Skills and MCP Servers and import them into HubCenter as free capability packages
- **Maclaw_Client**: The desktop GUI and TUI client application that connects to Hub and HubCenter
- **iWorkerCloud_Admin**: Cloud administration interface for managing the capability market
- **iWorkerCenter**: Worker center interface that displays capability market features
- **ClawHub**: External skill repository source (ClawHub mirror)
- **Capability_Type**: Classification of a capability — either `skill` or `mcp`
- **Validation_Result**: Structured output from MCP validation containing connectivity status, tool list, schema errors, and health metrics

## Requirements

### Requirement 1: Rename Skill Market to Capability Market in GUI Frontend

**User Story:** As a user, I want the GUI interface to display "能力市场" (Capability Market) instead of "技能市场" (Skill Market), so that the terminology reflects the broader scope of capabilities available.

#### Acceptance Criteria

1. WHEN the SkillsManagementPanel is rendered, THE Capability_Market SHALL display the tab label as "Capability Market" / "能力市场" / "能力市場" instead of "Skill Market" / "技能市场" / "技能市場"
2. WHEN the CloudRegistrationPage is rendered, THE Capability_Market SHALL display the module label as "capability_market" instead of "skill_market"
3. THE Capability_Market SHALL update all user-visible strings in `SkillsManagementPanel.tsx` that reference "Skill Market" or "技能市场" to use "Capability Market" or "能力市场"
4. THE Capability_Market SHALL update the HTML title of the Hub marketplace page to "Capability Marketplace" where it does not already use this term

### Requirement 2: Rename Skill Market to Capability Market in TUI Commands

**User Story:** As a TUI user, I want the CLI commands and help text to reference "capability market" instead of "skill market", so that the terminology is consistent across all interfaces.

#### Acceptance Criteria

1. WHEN the user invokes the TUI skillmarket subcommand, THE Capability_Market SHALL accept both "capabilitymarket" and "skillmarket" as command names for backward compatibility
2. THE Capability_Market SHALL update the usage text from "maclaw-tui skillmarket" to "maclaw-tui capabilitymarket" with "skillmarket" retained as an alias
3. THE Capability_Market SHALL update all help text and error messages in `tui/commands/skillmarket.go` to reference "capability market" instead of "skill market"

### Requirement 3: Rename Skill Market to Capability Market in Shared Libraries

**User Story:** As a developer, I want the shared library types and error messages to use "Capability Market" terminology, so that the codebase is internally consistent.

#### Acceptance Criteria

1. THE Capability_Market SHALL rename the `SkillMarketAuthClient` type in `corelib/remote/skillmarket_auth.go` to `CapabilityMarketAuthClient`
2. THE Capability_Market SHALL rename the `NewSkillMarketAuthClient` function to `NewCapabilityMarketAuthClient`
3. THE Capability_Market SHALL rename the `SkillMarketAuthResult` type to `CapabilityMarketAuthResult`
4. THE Capability_Market SHALL update all error messages in the auth client to reference "Capability Market" instead of "SkillMarket"
5. THE Capability_Market SHALL retain backward-compatible type aliases for `SkillMarketAuthClient` and `SkillMarketAuthResult` to avoid breaking existing callers during migration

### Requirement 4: Rename Skill Market to Capability Market in HubCenter Backend

**User Story:** As a HubCenter administrator, I want the backend handlers to use "Capability Market" terminology, so that the API and internal naming are consistent with the product direction.

#### Acceptance Criteria

1. THE Capability_Market SHALL rename handler functions in `hubcenter/internal/httpapi/skillmarket_handlers.go` to use "capabilitymarket" naming convention
2. THE Capability_Market SHALL update API route paths from `/skillmarket/` to `/capabilitymarket/` while retaining `/skillmarket/` routes as aliases for backward compatibility
3. THE Capability_Market SHALL update all log messages and error strings in HubCenter handlers to reference "capability market"

### Requirement 5: Rename Skill Market to Capability Market in iWorkerCloud Admin

**User Story:** As an iWorkerCloud administrator, I want the admin page to display "能力市场" instead of "技能市场", so that the cloud admin interface is consistent with other components.

#### Acceptance Criteria

1. THE Capability_Market SHALL rename the `SkillMarketPage.tsx` component in iWorkerCloud admin to `CapabilityMarketPage.tsx`
2. THE Capability_Market SHALL update all user-visible strings in the iWorkerCloud admin page to use "Capability Market" / "能力市场"
3. THE Capability_Market SHALL update the page route from any "skillmarket" path to "capabilitymarket" while retaining the old path as a redirect

### Requirement 6: Rename Marketplace References in Hub Backend

**User Story:** As a Hub administrator, I want the Hub backend marketplace handlers to use "Capability Market" terminology consistently, so that the enterprise marketplace aligns with the unified naming.

#### Acceptance Criteria

1. THE Capability_Market SHALL update function names in `hub/internal/httpapi/marketplace_handlers.go` that reference "marketplace" to use "capability_market" where appropriate
2. THE Capability_Market SHALL update API response field names from "marketplace" to "capability_market" in new endpoints while retaining existing field names in current endpoints for backward compatibility
3. THE Capability_Market SHALL update all internal comments and log messages to reference "capability market" instead of "skill market" or "marketplace"

### Requirement 7: Hub Admin Search for Skills and MCP Servers from External Sources

**User Story:** As a Hub administrator, I want to search and add both Skills and MCP Servers from external sources (HubCenter, ClawHub, GitHub), so that I can curate the enterprise capability market with both types of capabilities.

#### Acceptance Criteria

1. WHEN a Hub administrator searches with `type=skill`, THE Admin_Search_Handler SHALL return skill results from HubCenter, ClawHub, and GitHub sources
2. WHEN a Hub administrator searches with `type=mcp`, THE Admin_Search_Handler SHALL return MCP Server results from HubCenter
3. WHEN a Hub administrator searches with `type=mcp` and `source=clawhub`, THE Admin_Search_Handler SHALL search ClawHub for MCP Server configurations
4. WHEN a Hub administrator searches with `type=mcp` and `source=github`, THE Admin_Search_Handler SHALL search GitHub for MCP Server configurations
5. WHEN a Hub administrator selects an MCP Server from search results, THE Admin_Search_Handler SHALL allow the administrator to add the MCP Server to the enterprise Hub capability market
6. WHEN a Hub administrator selects a Skill from search results, THE Admin_Search_Handler SHALL allow the administrator to add the Skill to the enterprise Hub capability market
7. THE Admin_Search_Handler SHALL return each search result with `capability_type` field indicating whether the result is a `skill` or `mcp`

### Requirement 8: HubCenter Admin Search and Import Skills/MCP from External Sources

**User Story:** As a HubCenter administrator, I want to search ClawHub and GitHub for Skills and MCP Servers and import them into HubCenter as free capability packages, so that the public capability market has a rich catalog of available capabilities.

#### Acceptance Criteria

1. WHEN a HubCenter administrator searches with `type=skill` and `source=clawhub`, THE HubCenter Admin SHALL return skill results from ClawHub
2. WHEN a HubCenter administrator searches with `type=skill` and `source=github`, THE HubCenter Admin SHALL return skill results from GitHub
3. WHEN a HubCenter administrator searches with `type=mcp` and `source=clawhub`, THE HubCenter Admin SHALL return MCP Server configuration results from ClawHub
4. WHEN a HubCenter administrator searches with `type=mcp` and `source=github`, THE HubCenter Admin SHALL return MCP Server configuration results from GitHub
5. THE HubCenter Admin SHALL NOT search HubCenter itself as a source (since it IS HubCenter — unlike Hub which searches HubCenter as an upstream source)
6. WHEN a HubCenter administrator selects a search result and confirms import, THE HubCenter Admin SHALL download the capability package and register it in HubCenter as a free capability with `pricing: free`
7. WHEN a Skill is imported, THE HubCenter Admin SHALL store the skill definition (skill.yaml/SKILL.md) and mark it as `capability_type: skill` with source attribution (clawhub/github)
8. WHEN an MCP Server is imported, THE HubCenter Admin SHALL store the MCP configuration (mcp.json schema) and mark it as `capability_type: mcp` with source attribution
9. THE HubCenter Admin SHALL run MCP validation (connectivity + tool availability + schema correctness) on imported MCP Servers before making them available in the market
10. IF MCP validation fails, THEN THE HubCenter Admin SHALL still allow the import but mark the capability with `validation_status: failed` and display a warning to the administrator

### Requirement 9: MCP Server Connectivity Validation

**User Story:** As a Hub administrator, I want to validate MCP Server connectivity before adding it to the enterprise market, so that I can ensure the server is reachable and responsive.

#### Acceptance Criteria

1. WHEN a connectivity validation is requested for an MCP Server, THE MCP_Validator SHALL attempt to establish a connection to the server endpoint within 10 seconds
2. WHEN the MCP Server responds within the timeout, THE MCP_Validator SHALL return a Validation_Result with `connectivity: true` and the measured latency in milliseconds
3. IF the MCP Server does not respond within the timeout, THEN THE MCP_Validator SHALL return a Validation_Result with `connectivity: false` and a descriptive error message indicating the failure reason
4. IF the MCP Server endpoint URL is malformed, THEN THE MCP_Validator SHALL return a Validation_Result with `connectivity: false` and an error message indicating the URL is invalid

### Requirement 10: MCP Server Tool Availability Validation

**User Story:** As a Hub administrator, I want to verify that an MCP Server exposes the expected tools, so that I can confirm the server provides the capabilities it claims.

#### Acceptance Criteria

1. WHEN a tool availability validation is requested, THE MCP_Validator SHALL invoke the MCP `tools/list` method on the target server
2. WHEN the server returns a tool list, THE MCP_Validator SHALL include the list of available tool names in the Validation_Result
3. IF the server returns an empty tool list, THEN THE MCP_Validator SHALL include a warning in the Validation_Result indicating no tools are exposed
4. IF the `tools/list` call fails, THEN THE MCP_Validator SHALL return a Validation_Result with `tools_available: false` and the error details

### Requirement 11: MCP Server Schema Correctness Validation

**User Story:** As a Hub administrator, I want to validate that MCP Server tool schemas are well-formed, so that I can ensure tools will work correctly when invoked by agents.

#### Acceptance Criteria

1. WHEN a schema validation is requested, THE MCP_Validator SHALL retrieve the input schema for each tool exposed by the MCP Server
2. WHEN a tool's input schema is retrieved, THE MCP_Validator SHALL verify that the schema is valid JSON Schema (draft-07 or later)
3. IF a tool's input schema contains invalid JSON Schema syntax, THEN THE MCP_Validator SHALL include the tool name and specific schema error in the Validation_Result
4. WHEN all tool schemas pass validation, THE MCP_Validator SHALL return a Validation_Result with `schema_valid: true`
5. FOR ALL tools with `required` parameters declared in the schema, THE MCP_Validator SHALL verify that each required parameter has a corresponding property definition (round-trip property: declared required parameters exist in properties)

### Requirement 12: MCP Server Runtime Health Check

**User Story:** As a Hub administrator, I want to perform a runtime health check on an MCP Server, so that I can verify the server is functioning correctly under normal operation.

#### Acceptance Criteria

1. WHEN a runtime health check is requested, THE MCP_Validator SHALL invoke a lightweight tool call on the MCP Server using a safe, read-only tool if one is available
2. WHEN the health check tool call succeeds within 15 seconds, THE MCP_Validator SHALL return a Validation_Result with `runtime_healthy: true` and the response time
3. IF the health check tool call fails or times out, THEN THE MCP_Validator SHALL return a Validation_Result with `runtime_healthy: false` and the failure details
4. IF no safe read-only tool is available for health checking, THEN THE MCP_Validator SHALL skip the runtime invocation and return `runtime_healthy: null` with a note indicating no safe tool was found for testing
5. THE MCP_Validator SHALL select the health check tool by preferring tools with no required parameters, then tools with only string parameters, then the first tool in the list

### Requirement 13: Combined MCP Validation Report

**User Story:** As a Hub administrator, I want a single validation report that combines all MCP checks, so that I can make an informed decision about adding the server to the enterprise market.

#### Acceptance Criteria

1. WHEN a full validation is requested, THE MCP_Validator SHALL execute connectivity, tool availability, schema correctness, and runtime health checks in sequence
2. IF connectivity validation fails, THEN THE MCP_Validator SHALL skip subsequent checks and return the partial Validation_Result with only connectivity information
3. WHEN all checks complete, THE MCP_Validator SHALL return a combined Validation_Result containing all four check results in a single structured response
4. THE MCP_Validator SHALL include a summary `overall_status` field with value `pass` (all checks passed), `warn` (some checks have warnings), or `fail` (critical checks failed)
5. THE MCP_Validator SHALL complete the full validation within 30 seconds total timeout

### Requirement 14: Existing Parameter Validation Integration

**User Story:** As a developer, I want the existing `ValidateArgs` function in `corelib/mcp/validate.go` to be reused for MCP schema validation, so that validation logic is not duplicated.

#### Acceptance Criteria

1. WHEN the MCP_Validator performs schema correctness validation, THE MCP_Validator SHALL use the existing `ValidateArgs` function to verify that tool schemas can correctly validate sample arguments
2. THE MCP_Validator SHALL construct sample arguments from the schema's property definitions for validation testing
3. FOR ALL tools with required parameters, parsing the schema then constructing sample args then validating with `ValidateArgs` SHALL produce zero validation errors (round-trip property: valid schema produces valid sample args)
