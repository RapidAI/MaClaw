package guiapp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/hopjit"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// hopGovernedTool is one skill program or ready MCP tool a hop step may name.
// Name is the tool-line spelling. Aliases are the same binding, so a task that
// says the stable id still attaches Name.
type hopGovernedTool struct {
	Name    string
	Aliases []string
}

// hopDynamicBinding is a resolved skill or MCP tool. A binding with provisions
// joins the hop plan. A skill program has no capability provision: the step's
// tool line is the authorization, and Call runs executeBoundSkill.
type hopDynamicBinding struct {
	Name             string
	Description      string
	Schema           map[string]interface{}
	Kind             string
	ProviderID       string
	ImplementationID string
	Provisions       []tool.CapabilityProvision
	Effects          []tool.EffectClass
	Call             func(context.Context, map[string]any) (string, error)
	Plan             func(context.Context, tool.InvocationScope, tool.CanonicalRequest) tool.SelectionExecutionResult
}

type hopDirectTool struct {
	schema map[string]interface{}
	auth   tool.ParameterAuthorization
	call   func(context.Context, map[string]any) (string, error)
}

// hopToolProvider runs planned registry and MCP tools, and skill programs
// whose authorization is the explicit tool line.
type hopToolProvider struct {
	plan   *tool.HopJITPlanToolProvider
	direct map[string]hopDirectTool
}

func (p *hopToolProvider) Execute(ctx context.Context, name string, args map[string]any) (any, error) {
	if p == nil {
		return nil, fmt.Errorf("hopjit_plan_provider_unavailable")
	}
	name = strings.TrimSpace(name)
	if call, ok := p.direct[name]; ok {
		return call.execute(ctx, args)
	}
	if p.plan == nil {
		return nil, fmt.Errorf("hopjit_tool_not_authorized: %s", name)
	}
	return p.plan.Execute(ctx, name, args)
}

func (t hopDirectTool) execute(ctx context.Context, args map[string]any) (any, error) {
	if t.call == nil {
		return nil, fmt.Errorf("hopjit_tool_not_authorized")
	}
	if args == nil {
		args = map[string]any{}
	}
	encoded, err := json.Marshal(args)
	if err != nil {
		return nil, fmt.Errorf("hopjit_tool_arguments_invalid: %w", err)
	}
	request, err := tool.CanonicalizeAuthorizedInvocationArguments(string(encoded), t.schema, t.auth)
	if err != nil {
		return nil, err
	}
	text, err := t.call(ctx, request.Values)
	if err != nil {
		return nil, err
	}
	return text, nil
}

func (h *IMMessageHandler) lookupHopDynamicTool(ctx context.Context, name string) (hopDynamicBinding, bool, error) {
	if h != nil && h.hopDynamicLookup != nil {
		return h.hopDynamicLookup(ctx, name)
	}
	return h.resolveHopDynamicTool(ctx, name)
}

func (h *IMMessageHandler) resolveHopDynamicTool(ctx context.Context, name string) (hopDynamicBinding, bool, error) {
	name = strings.TrimSpace(name)
	if h == nil || name == "" {
		return hopDynamicBinding{}, false, nil
	}
	skillBinding, skillOK, skillErr := h.resolveHopSkillTool(name)
	if skillErr != nil {
		return hopDynamicBinding{}, false, skillErr
	}
	mcpBinding, mcpOK, mcpErr := h.resolveHopMCPTool(ctx, name)
	if skillOK && mcpOK {
		return hopDynamicBinding{}, false, fmt.Errorf("HopJIT tool %q matches both a skill and an MCP tool", name)
	}
	if skillOK {
		return skillBinding, true, nil
	}
	if mcpErr != nil {
		return hopDynamicBinding{}, false, mcpErr
	}
	return mcpBinding, mcpOK, nil
}

func (h *IMMessageHandler) resolveHopSkillTool(name string) (hopDynamicBinding, bool, error) {
	executor := h.getSkillExecutor()
	if executor == nil {
		return hopDynamicBinding{}, false, nil
	}
	var matched []corelib.NLSkillEntry
	for _, entry := range executor.loadSkills() {
		if !semanticSkillIsRunnable(entry) {
			continue
		}
		stableID := agentservice.DynamicSkillStableID(entry)
		if strings.TrimSpace(entry.Name) != name && stableID != name {
			continue
		}
		matched = append(matched, entry)
	}
	if len(matched) == 0 {
		return hopDynamicBinding{}, false, nil
	}
	if len(matched) > 1 {
		return hopDynamicBinding{}, false, fmt.Errorf("HopJIT tool %q matches more than one skill", name)
	}
	entry := matched[0]
	schema, err := hopSkillArgumentSchema(entry.Params)
	if err != nil {
		return hopDynamicBinding{}, false, err
	}
	stableID := agentservice.DynamicSkillStableID(entry)
	skillName := strings.TrimSpace(entry.Name)
	version := strings.TrimSpace(entry.Version)
	digest := agentservice.DynamicSkillContentDigest(entry)
	return hopDynamicBinding{
		Name:             skillName,
		Description:      strings.TrimSpace(entry.Description),
		Schema:           schema,
		Kind:             "skill",
		ProviderID:       stableID,
		ImplementationID: skillName,
		Call: func(ctx context.Context, args map[string]any) (string, error) {
			if ctx != nil && ctx.Err() != nil {
				return "", ctx.Err()
			}
			current := h.getSkillExecutor()
			if current == nil {
				return "", fmt.Errorf("skill_binding_stale")
			}
			text, _, err := current.executeBoundSkill(stableID, skillName, version, digest, args)
			return text, err
		},
	}, true, nil
}

func (h *IMMessageHandler) resolveHopMCPTool(ctx context.Context, name string) (hopDynamicBinding, bool, error) {
	inventory, err := h.semanticInstalledMCPInventory(ctx)
	if err != nil {
		return hopDynamicBinding{}, false, err
	}
	var matched []agentservice.MCPToolEntry
	for _, entry := range inventory.mcpEntries {
		if tool.InstalledMCPModelFunctionName(entry.ServerID, entry.ToolName) != name {
			continue
		}
		if assessDiscoveredMCPBind(inventory, entry.ServerID, entry.ToolName) != "" {
			continue
		}
		matched = append(matched, entry)
	}
	if len(matched) == 0 {
		return hopDynamicBinding{}, false, nil
	}
	if len(matched) > 1 {
		return hopDynamicBinding{}, false, fmt.Errorf("HopJIT tool %q matches more than one MCP tool", name)
	}
	entry := matched[0]
	projected, definition, binding, err := agentservice.ProjectMCPDynamicProvider(entry)
	if err != nil {
		return hopDynamicBinding{}, false, nil
	}
	schema, _ := hopDefinitionSchema(definition)
	if schema == nil {
		return hopDynamicBinding{}, false, nil
	}
	serverID := strings.TrimSpace(entry.ServerID)
	toolName := strings.TrimSpace(entry.ToolName)
	return hopDynamicBinding{
		Name:             name,
		Description:      agentservice.InstalledMCPToolPromptSummary(serverID, toolName),
		Schema:           schema,
		Kind:             "mcp",
		ProviderID:       serverID,
		ImplementationID: toolName,
		Provisions:       append([]tool.CapabilityProvision(nil), projected.Provides...),
		Effects:          append([]tool.EffectClass(nil), projected.Effects...),
		Plan: func(callCtx context.Context, scope tool.InvocationScope, request tool.CanonicalRequest) tool.SelectionExecutionResult {
			return h.executeHopMCP(callCtx, scope, serverID, toolName, binding.SchemaDigest, binding.ContractDigest, request)
		},
	}, true, nil
}

func (h *IMMessageHandler) executeHopMCP(ctx context.Context, scope tool.InvocationScope, serverID, toolName, schemaDigest, contractDigest string, request tool.CanonicalRequest) tool.SelectionExecutionResult {
	if ctx != nil && ctx.Err() != nil {
		return tool.SelectionExecutionResult{Result: ctx.Err().Error(), ReasonCode: "dynamic_execution_cancelled"}
	}
	inventory, err := h.semanticInstalledMCPInventory(ctx)
	if err != nil {
		return tool.SelectionExecutionResult{Result: err.Error(), ReasonCode: "dynamic_catalog_incomplete"}
	}
	var fresh *agentservice.MCPToolEntry
	for i := range inventory.mcpEntries {
		entry := &inventory.mcpEntries[i]
		if strings.TrimSpace(entry.ServerID) == serverID && strings.TrimSpace(entry.ToolName) == toolName {
			fresh = entry
			break
		}
	}
	if fresh == nil || assessDiscoveredMCPBind(inventory, serverID, toolName) != "" {
		return tool.SelectionExecutionResult{Result: "[system rejected] dynamic_binding_stale", ReasonCode: "dynamic_binding_stale"}
	}
	catalog, err := agentservice.BuildDynamicSemanticCatalog([]agentservice.MCPToolEntry{*fresh}, nil)
	if err != nil || len(catalog.Providers) != 1 {
		return tool.SelectionExecutionResult{Result: "[system rejected] dynamic_binding_stale", ReasonCode: "dynamic_binding_stale"}
	}
	projected, _, binding, err := agentservice.ProjectMCPDynamicProvider(*fresh)
	if err != nil || binding.SchemaDigest != schemaDigest || binding.ContractDigest != contractDigest {
		return tool.SelectionExecutionResult{Result: "[system rejected] dynamic_binding_stale", ReasonCode: "dynamic_binding_stale"}
	}
	selection := tool.PlannedSelection{
		ID:                     "hopjit:" + serverID + ":" + toolName,
		AdapterName:            projected.AdapterName,
		Provider:               projected.Binding,
		ParameterAuthorization: projected.ParameterAuthorization,
		Effects:                append([]tool.EffectClass(nil), projected.Effects...),
	}
	var coordinator agentservice.DynamicExternalEffectCoordinator
	if tool.SelectionRequiresExternalReceipt(selection) {
		coordinator, err = h.semanticDynamicEffectCoordinator()
		if err != nil {
			return tool.SelectionExecutionResult{Result: "[system rejected] dynamic_effect_coordinator_unavailable", ReasonCode: "dynamic_effect_coordinator_unavailable"}
		}
	}
	principal := agentservice.Principal{TenantID: semanticDesktopTenantID(), UserID: strings.TrimSpace(scope.PrincipalID)}
	return catalog.ExecuteSelectionWithEffects(ctx, scope, principal, guiSemanticMCPBridge{handler: h}, guiSemanticSkillBridge{handler: h}, coordinator, selection, string(request.CanonicalJSON))
}

func (c *guiHopJITCaller) governedHopTools(ctx context.Context) []hopGovernedTool {
	if c == nil {
		return nil
	}
	if c.governedTools != nil {
		return c.governedTools
	}
	if c.handler == nil {
		return nil
	}
	return c.handler.listHopGovernedTools(ctx)
}

func (h *IMMessageHandler) listHopGovernedTools(ctx context.Context) []hopGovernedTool {
	if h == nil {
		return nil
	}
	var tools []hopGovernedTool
	if executor := h.getSkillExecutor(); executor != nil {
		for _, entry := range executor.loadSkills() {
			if !semanticSkillIsRunnable(entry) {
				continue
			}
			name := strings.TrimSpace(entry.Name)
			if name == "" {
				continue
			}
			item := hopGovernedTool{Name: name}
			if stableID := agentservice.DynamicSkillStableID(entry); stableID != "" && stableID != name {
				item.Aliases = []string{stableID}
			}
			tools = append(tools, item)
		}
	}
	inventory, err := h.semanticInstalledMCPInventory(ctx)
	if err != nil {
		return tools
	}
	toolNames := map[string]int{}
	for _, entry := range inventory.mcpEntries {
		if assessDiscoveredMCPBind(inventory, entry.ServerID, entry.ToolName) != "" {
			continue
		}
		toolNames[strings.TrimSpace(entry.ToolName)]++
	}
	for _, entry := range inventory.mcpEntries {
		if assessDiscoveredMCPBind(inventory, entry.ServerID, entry.ToolName) != "" {
			continue
		}
		function := tool.InstalledMCPModelFunctionName(entry.ServerID, entry.ToolName)
		if function == "" {
			continue
		}
		item := hopGovernedTool{Name: function}
		toolName := strings.TrimSpace(entry.ToolName)
		if toolNames[toolName] == 1 && discoveredMCPToolNameIsCompound(toolName) && toolName != function {
			item.Aliases = []string{toolName}
		}
		tools = append(tools, item)
	}
	return tools
}

func hopSkillArgumentSchema(params []corelib.NLSkillParam) (map[string]interface{}, error) {
	properties := map[string]interface{}{}
	var required []string
	for _, param := range params {
		name := strings.TrimSpace(param.Name)
		if name == "" || hopReservedSkillArgument(name) {
			continue
		}
		kind := strings.TrimSpace(param.Type)
		switch kind {
		case "string", "number", "integer", "boolean", "array", "object":
		default:
			kind = "string"
		}
		properties[name] = map[string]interface{}{"type": kind}
		if param.Required {
			required = append(required, name)
		}
	}
	schema := map[string]interface{}{
		"type":                 "object",
		"properties":           properties,
		"additionalProperties": false,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return tool.CanonicalRegisteredToolInvocationSchema(schema, required)
}

func hopReservedSkillArgument(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "name", "skill", "skill_id", "skill_name", "provider", "provider_id", "selection_id", "action", "credential", "credentials", "artifact_id", "artifact_ref":
		return true
	default:
		return false
	}
}

func hopAuthorizeSchema(schema map[string]interface{}) (map[string]interface{}, tool.ParameterAuthorization, error) {
	if len(schema) == 0 {
		schema = map[string]interface{}{
			"type":                 "object",
			"properties":           map[string]interface{}{},
			"additionalProperties": false,
		}
	}
	canonical, err := tool.CanonicalRegisteredToolInvocationSchema(schema, nil)
	if err != nil {
		return nil, tool.ParameterAuthorization{}, err
	}
	authorization, err := tool.NewParameterAuthorization(canonical)
	if err != nil {
		return nil, tool.ParameterAuthorization{}, err
	}
	return canonical, authorization, nil
}

func hopDynamicDefinition(name, description string, schema map[string]interface{}) map[string]interface{} {
	return map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        name,
			"description": description,
			"parameters":  schema,
		},
	}
}

func hopDefinitionSchema(definition map[string]interface{}) (map[string]interface{}, bool) {
	function, _ := definition["function"].(map[string]interface{})
	schema, ok := function["parameters"].(map[string]interface{})
	return schema, ok && schema != nil
}

type hopPlannedDynamic struct {
	provider   tool.ProviderSpec
	definition map[string]interface{}
	schema     map[string]interface{}
	need       tool.CapabilityNeed
	pin        tool.ProviderPin
}

// hopPrepareDynamicPlan publishes one governed MCP tool under the whitelist
// name. The capability vocabulary uses the namespace ceiling, so a provider
// whose contract effects exceed that ceiling cannot be planned.
func hopPrepareDynamicPlan(registry *tool.CapabilityRegistry, name string, binding hopDynamicBinding) (hopPlannedDynamic, error) {
	name = strings.TrimSpace(name)
	if registry == nil || name == "" || len(binding.Provisions) == 0 || len(binding.Effects) == 0 {
		return hopPlannedDynamic{}, fmt.Errorf("HopJIT tool is unavailable or ungoverned: %s", name)
	}
	if strings.TrimSpace(binding.Kind) == "" || strings.TrimSpace(binding.ProviderID) == "" || strings.TrimSpace(binding.ImplementationID) == "" {
		return hopPlannedDynamic{}, fmt.Errorf("HopJIT tool is unavailable or ungoverned: %s", name)
	}
	ids := make([]tool.CapabilityID, 0, len(binding.Provisions))
	for _, provision := range binding.Provisions {
		ids = append(ids, provision.Capability)
	}
	if err := agentservice.EnsureExternalCapabilityVocabulary(registry, ids); err != nil {
		return hopPlannedDynamic{}, err
	}
	schema, authorization, err := hopAuthorizeSchema(binding.Schema)
	if err != nil {
		return hopPlannedDynamic{}, err
	}
	definition := hopDynamicDefinition(name, binding.Description, schema)
	provision := hopGovernedPlanProvision(binding)
	return hopPlannedDynamic{
		provider: tool.ProviderSpec{
			AdapterName: name,
			Binding: tool.ProviderBinding{
				Kind:             strings.TrimSpace(binding.Kind),
				ProviderID:       strings.TrimSpace(binding.ProviderID),
				ImplementationID: strings.TrimSpace(binding.ImplementationID),
				SchemaDigest:     tool.SchemaDigest(canonicalToolDefinitionBytes(definition)),
			},
			ParameterAuthorization: authorization,
			Provides:               append([]tool.CapabilityProvision(nil), binding.Provisions...),
			Effects:                append([]tool.EffectClass(nil), binding.Effects...),
			Ready:                  true,
		},
		definition: definition,
		schema:     schema,
		need: tool.CapabilityNeed{
			ID: "hopjit:" + name, Capability: provision.Capability, Qualifiers: provision.Qualifiers, Required: true,
		},
		pin: tool.ProviderPin{
			Capability: provision.Capability, Kind: strings.TrimSpace(binding.Kind),
			ProviderID: strings.TrimSpace(binding.ProviderID), ImplementationID: strings.TrimSpace(binding.ImplementationID),
		},
	}, nil
}

// hopGovernedPlanProvision is the capability the hop plan needs. An installed
// MCP tool is addressed by its x:mcp: provision, the one the external
// vocabulary registers at the namespace ceiling. The contract's first
// provision can be a qualifier-heavy host capability; planning against that
// one fails the step even though the tool itself is ready. A binding with no
// x:mcp: provision keeps its first provision, so a single registered
// capability still plans. Effects stay the contract's effects: a tool whose
// effects exceed the ceiling still cannot be planned.
func hopGovernedPlanProvision(binding hopDynamicBinding) tool.CapabilityProvision {
	if strings.EqualFold(strings.TrimSpace(binding.Kind), "mcp") {
		if id := agentservice.AutoMCPCapabilityIDForBinding(binding.ProviderID, binding.ImplementationID); id != "" {
			for _, provision := range binding.Provisions {
				if provision.Capability == id {
					return provision
				}
			}
		}
		for _, provision := range binding.Provisions {
			if strings.HasPrefix(string(provision.Capability), "x:mcp:") {
				return provision
			}
		}
	}
	return binding.Provisions[0]
}

// hopToolProvider must satisfy the engine even when the plan is absent.
var _ hopjit.ToolProvider = (*hopToolProvider)(nil)
