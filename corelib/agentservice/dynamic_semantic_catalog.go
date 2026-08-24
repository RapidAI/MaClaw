package agentservice

import (
	"fmt"
	"strings"

	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

// ProjectMCPDynamicProvider turns one ready, contract-verified MCP inventory
// entry into a common semantic catalog provider. It is intentionally a pure
// projection: discovery does not choose a capability, expose a tool, or invoke
// the provider. The caller must publish the result with ToolCatalog and route
// it through ToolPlanner before rendering a grant.
func ProjectMCPDynamicProvider(entry MCPToolEntry) (coretool.ProviderSpec, map[string]interface{}, MCPToolBinding, error) {
	binding, err := BindMCPTool([]MCPToolEntry{entry}, entry.ServerID, entry.ToolName)
	if err != nil {
		return coretool.ProviderSpec{}, nil, MCPToolBinding{}, err
	}
	provider, definition, err := (coretool.DynamicProviderDescriptor{
		Kind:                 "mcp",
		ProviderID:           binding.ServerID,
		ImplementationID:     binding.ToolName,
		ObservedSchemaDigest: binding.SchemaDigest,
		ContractDigest:       binding.ContractDigest,
		Provides:             entry.Contract.Provisions,
		Consumes:             entry.Contract.Consumes,
		Produces:             entry.Contract.Produces,
		Effects:              entry.Contract.Effects,
		Ready:                true,
		// Dynamic Skill/MCP contracts are channel-neutral unless the trusted
		// host policy projects an explicit channel restriction. An empty scope
		// means the common planner may consider the provider for any admitted
		// channel; copying a provider-supplied channel name here would let
		// discovery metadata control routing.
		ChannelScopes:    nil,
		InvocationSchema: safeMCPInvocationSchema(entry.InputSchema),
	}).Project()
	if err != nil {
		return coretool.ProviderSpec{}, nil, MCPToolBinding{}, fmt.Errorf("project MCP binding: %w", err)
	}
	return provider, definition, binding, nil
}

// ProjectSkillDynamicProvider is the Skill counterpart to
// ProjectMCPDynamicProvider. ContentDigest is part of ObservedSchemaDigest so
// a changed package cannot retain selection authority merely because it kept a
// display name and parameter list.
func ProjectSkillDynamicProvider(entry SkillToolEntry) (coretool.ProviderSpec, map[string]interface{}, SkillBinding, error) {
	binding, err := BindSkill([]SkillToolEntry{entry}, entry.StableID, entry.Name)
	if err != nil {
		return coretool.ProviderSpec{}, nil, SkillBinding{}, err
	}
	parameterSchema := skillInvocationSchema(entry.Params)
	observedSchemaDigest := coretool.SchemaDigest([]byte(strings.Join([]string{
		binding.Version,
		binding.ContentDigest,
		coretool.SchemaDigest(canonicalMCPSchema(parameterSchema)),
	}, "\x00")))
	provider, definition, err := (coretool.DynamicProviderDescriptor{
		Kind:                 "skill",
		ProviderID:           binding.StableID,
		ImplementationID:     binding.Name,
		ObservedSchemaDigest: observedSchemaDigest,
		ContractDigest:       binding.ContractDigest,
		Provides:             entry.Contract.Provisions,
		Consumes:             entry.Contract.Consumes,
		Produces:             entry.Contract.Produces,
		Effects:              entry.Contract.Effects,
		Ready:                true,
		ChannelScopes:        nil,
		InvocationSchema:     parameterSchema,
	}).Project()
	if err != nil {
		return coretool.ProviderSpec{}, nil, SkillBinding{}, fmt.Errorf("project Skill binding: %w", err)
	}
	return provider, definition, binding, nil
}
