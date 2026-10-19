package guiapp

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// discoveredMCPBind is one installed MCP tool the host will publish as an
// x:mcp grant. The pair is the binding identity; the model never selects the
// server or the vendor tool name.
type discoveredMCPBind struct {
	ServerID string
	ToolName string
}

const (
	// The host binds the tool. These sentences must not ask the model to
	// request a managed semantic replan: that request has no implementation.
	// They also must not name the removed gateway: discovery tests treat that
	// name as an instruction to invoke it.
	discoveredMCPBoundSentence   = "The host bound the named installed MCP tool for the next request. Call the new function whose description names that tool. Do not use the removed MCP gateway. Do not rediscover the same name."
	discoveredMCPUnboundSentence = "Installed MCP matches stay unbound until this lookup names one compound tool, or the lookup has exactly one compound match. Pass that exact tool name to discover_tool once. The host binds it. Do not use the removed MCP gateway."
	discoveredMCPNoLoopSentence  = "The host identified an installed MCP tool, but this lookup has no loop to attach the bind. Do not use the removed MCP gateway."
)

// discoveredMCPToolNameIsCompound mirrors the unexported compound-reference
// rule: a bare word such as "search" is not a vendor tool address.
func discoveredMCPToolNameIsCompound(toolName string) bool {
	trimmed := strings.TrimSpace(toolName)
	if trimmed == "" {
		return false
	}
	if strings.ContainsAny(trimmed, "_-") {
		return true
	}
	for i := 0; i < len(trimmed); i++ {
		if trimmed[i] >= 'A' && trimmed[i] <= 'Z' {
			return true
		}
	}
	return false
}

// discoveredMCPContinuationTurn is the reply that approved one installed MCP
// write. The bind is already on the loop; classification of that short reply
// must not replace the surface that publishes it.
func discoveredMCPContinuationTurn(ctx *LoopContext) bool {
	return ctx != nil && ctx.IsAskUserResponse && len(ctx.discoveredMCPBindList()) > 0
}

func discoveredMCPBindKey(serverID, toolName string) string {
	return strings.TrimSpace(serverID) + "\x00" + strings.TrimSpace(toolName)
}

func discoveredMCPBindsEqual(left, right []discoveredMCPBind) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i].ServerID != right[i].ServerID || left[i].ToolName != right[i].ToolName {
			return false
		}
	}
	return true
}

func discoveredMCPBindDigestText(binds []discoveredMCPBind) string {
	parts := make([]string, 0, len(binds))
	for _, bind := range binds {
		parts = append(parts, discoveredMCPBindKey(bind.ServerID, bind.ToolName))
	}
	sort.Strings(parts)
	return strings.Join(parts, "\x1e")
}

// selectDiscoveredMCPBinds chooses which ranked MCP matches the host will
// bind. A name the lookup actually contains is an address. Otherwise exactly
// one compound match is an address. Every other BM25 hit stays listed and
// unbound, including a bare one-word tool name.
func selectDiscoveredMCPBinds(need string, ranked []discoveredToolScore, mcpMatches map[string]discoverableMCPTool) []discoverableMCPTool {
	compounds := make([]discoverableMCPTool, 0, 1)
	mentioned := make([]discoverableMCPTool, 0, 1)
	seen := map[string]bool{}
	seenMentioned := map[string]bool{}
	for _, item := range ranked {
		mcp, ok := mcpMatches[item.name]
		if !ok || !discoveredMCPToolNameIsCompound(mcp.toolName) {
			continue
		}
		key := discoveredMCPBindKey(mcp.serverID, mcp.toolName)
		if discoveryNeedMentionsTool(need, mcp.toolName) {
			if !seenMentioned[key] {
				seenMentioned[key] = true
				mentioned = append(mentioned, mcp)
			}
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		compounds = append(compounds, mcp)
	}
	if len(mentioned) > 0 {
		return mentioned
	}
	if len(compounds) == 1 {
		return compounds
	}
	return nil
}

// discoveredMCPNamedNeeds mints one required x:mcp need per bind. The prose
// pin matcher refuses a reference shorter than its floor, because a short
// word in an ordinary sentence is not a tool address. These binds are
// already the address. Running them back through that matcher drops the
// function after discovery told the model not to rediscover the name.
func discoveredMCPNamedNeeds(binds []discoveredMCPBind) ([]tool.CapabilityNeed, []tool.ProviderPin) {
	namedSeen := map[tool.CapabilityID]bool{}
	needs := make([]tool.CapabilityNeed, 0, len(binds))
	pins := make([]tool.ProviderPin, 0, len(binds))
	for _, bind := range binds {
		serverID := strings.TrimSpace(bind.ServerID)
		toolName := strings.TrimSpace(bind.ToolName)
		capID := agentservice.AutoMCPCapabilityIDForBinding(serverID, toolName)
		if capID == "" || namedSeen[capID] {
			continue
		}
		namedSeen[capID] = true
		needs = append(needs, tool.CapabilityNeed{
			ID:          "need:~named:" + string(capID),
			Capability:  capID,
			Polarity:    tool.NeedRequire,
			Required:    true,
			Confidence:  1,
			EvidenceIDs: []string{"named:mcp_tool"},
		})
		pins = append(pins, tool.ProviderPin{
			Capability: capID, Kind: "mcp", ProviderID: serverID, ImplementationID: toolName,
		})
	}
	sort.Slice(needs, func(i, j int) bool { return needs[i].ID < needs[j].ID })
	sort.Slice(pins, func(i, j int) bool { return pins[i].Capability < pins[j].Capability })
	return needs, pins
}

// assessDiscoveredMCPBind reports why a discovery hit cannot be planned.
// An empty string means the semantic inventory can publish it. A tools/list
// cache hit without a contract is not described as waiting on a replan.
func assessDiscoveredMCPBind(inventory semanticDynamicInventory, serverID, toolName string) string {
	serverID = strings.TrimSpace(serverID)
	toolName = strings.TrimSpace(toolName)
	var entry *agentservice.MCPToolEntry
	for i := range inventory.mcpEntries {
		candidate := &inventory.mcpEntries[i]
		if strings.TrimSpace(candidate.ServerID) == serverID && strings.TrimSpace(candidate.ToolName) == toolName {
			entry = candidate
			break
		}
	}
	if entry == nil {
		return fmt.Sprintf("%s/%s was observed by discovery but is not a published semantic provider. The host cannot bind it.", serverID, toolName)
	}
	if entry.RuntimeBlocked {
		return fmt.Sprintf("%s/%s is installed but its latest observation is not ready. The host cannot bind it.", serverID, toolName)
	}
	// BuildDynamicSemanticCatalog drops a contract whose schema does not
	// project. A bind claimed here would tell the model the next request
	// lists a function that publish then omits.
	if _, _, _, err := agentservice.ProjectMCPDynamicProvider(*entry); err != nil {
		return fmt.Sprintf("%s/%s was observed by discovery but is not a published semantic provider. The host cannot bind it.", serverID, toolName)
	}
	// The next request mints a required x:mcp need from the binding identity
	// and renders only a grant for that address. publishObservedMCP still
	// stores the open-lookup contract when the exact provision is not
	// attached. Claiming a bind then omits the function and tells the model
	// not to rediscover the name.
	capability := agentservice.AutoMCPCapabilityIDForBinding(serverID, toolName)
	provisioned := false
	if capability != "" {
		for _, provision := range entry.Contract.Provisions {
			if provision.Capability == capability {
				provisioned = true
				break
			}
		}
	}
	if !provisioned {
		return fmt.Sprintf("%s/%s was observed by discovery but is not a published semantic provider. The host cannot bind it.", serverID, toolName)
	}
	readOnly := len(entry.Contract.Effects) == 1 && entry.Contract.Effects[0] == tool.EffectReadOnly
	if !readOnly && inventory.coverage.ForProviderKind("mcp").State != tool.CatalogCoverageComplete {
		return fmt.Sprintf("%s/%s cannot be bound until the MCP catalog observation is complete.", serverID, toolName)
	}
	return ""
}

func (h *IMMessageHandler) assessDiscoveredMCPBinds(ctx context.Context, selected []discoverableMCPTool) (bindable []discoverableMCPTool, reasons []string) {
	if h == nil || len(selected) == 0 {
		return nil, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	inventory, err := h.semanticDynamicInventory(ctx)
	if err != nil {
		reasons = make([]string, 0, len(selected))
		for _, item := range selected {
			reasons = append(reasons, fmt.Sprintf("%s/%s was observed by discovery but is not a published semantic provider. The host cannot bind it (%v).", item.serverID, item.toolName, err))
		}
		return nil, reasons
	}
	for _, item := range selected {
		if reason := assessDiscoveredMCPBind(inventory, item.serverID, item.toolName); reason != "" {
			reasons = append(reasons, reason)
			continue
		}
		bindable = append(bindable, item)
	}
	return bindable, reasons
}

// semanticSurfaceForDiscoveredMCP plans and publishes x:mcp grants for the
// binds discovery already identified. It does not classify the user text and
// does not enter semanticPlanForTurn: a legacy turn has no managed label, and
// inventing one would authorize whatever that label grants.
func (h *IMMessageHandler) semanticSurfaceForDiscoveredMCP(ctx context.Context, userID, channel string, identity semanticLoopInvocationIdentity, binds []discoveredMCPBind, generation uint64) ([]map[string]interface{}, *semanticCallSurface, error) {
	if h == nil {
		return nil, nil, fmt.Errorf("semantic tool surface is unavailable")
	}
	userID = strings.TrimSpace(userID)
	if userID == "" || strings.TrimSpace(identity.RootTaskID) == "" || strings.TrimSpace(identity.SessionID) == "" || strings.TrimSpace(identity.TurnID) == "" {
		return nil, nil, fmt.Errorf("semantic_invocation_identity_required")
	}
	if len(binds) == 0 {
		return nil, nil, fmt.Errorf("discovered MCP bind is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := semanticRoutingRequestErr(ctx); err != nil {
		return nil, nil, err
	}
	routingPolicy, err := h.discoveredMCPRoutingPolicy()
	if err != nil {
		return nil, nil, err
	}
	inventory, err := h.semanticDynamicInventory(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("load GUI dynamic semantic inventory: %w", err)
	}
	wanted := make(map[string]discoveredMCPBind, len(binds))
	planned := make([]discoveredMCPBind, 0, len(binds))
	names := make([]string, 0, len(binds))
	for _, bind := range binds {
		key := discoveredMCPBindKey(bind.ServerID, bind.ToolName)
		if _, ok := wanted[key]; ok {
			continue
		}
		if reason := assessDiscoveredMCPBind(inventory, bind.ServerID, bind.ToolName); reason != "" {
			return nil, nil, fmt.Errorf("%s", reason)
		}
		wanted[key] = bind
		planned = append(planned, bind)
		names = append(names, strings.TrimSpace(bind.ToolName))
	}
	entries := make([]agentservice.MCPToolEntry, 0, len(wanted))
	for _, entry := range inventory.mcpEntries {
		if _, ok := wanted[discoveredMCPBindKey(entry.ServerID, entry.ToolName)]; ok {
			entries = append(entries, entry)
		}
	}
	dynamicCatalog, err := agentservice.BuildDynamicSemanticCatalog(entries, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("build GUI dynamic semantic catalog: %w", err)
	}
	registry := newIMSemanticCapabilityRegistry()
	catalog := tool.NewToolCatalog(registry)
	defsByName := make(map[string]map[string]interface{}, len(dynamicCatalog.Providers))
	schemas := make(map[string]map[string]interface{}, len(dynamicCatalog.Providers))
	providers := make([]tool.ProviderSpec, 0, len(dynamicCatalog.Providers))
	for _, provider := range dynamicCatalog.Providers {
		definition, ok := dynamicCatalog.Definitions[provider.AdapterName]
		if !ok {
			return nil, nil, fmt.Errorf("dynamic semantic definition missing for %q", provider.AdapterName)
		}
		invocationSchema, err := semanticInvocationSchema(definition)
		if err != nil {
			return nil, nil, fmt.Errorf("validate dynamic semantic invocation schema for %q: %w", provider.AdapterName, err)
		}
		authorization, err := tool.NewParameterAuthorization(invocationSchema)
		if err != nil {
			return nil, nil, fmt.Errorf("authorize dynamic semantic invocation schema for %q: %w", provider.AdapterName, err)
		}
		provider.ParameterAuthorization = authorization
		defsByName[provider.AdapterName] = definition
		schemas[provider.AdapterName] = invocationSchema
		providers = append(providers, provider)
	}
	if len(providers) != len(wanted) {
		return nil, nil, fmt.Errorf("discovered MCP bind is not a published semantic provider")
	}
	// Coverage is the inventory's observation watermark. Do not mark builtin
	// or channel complete: this catalog does not publish those providers.
	if err := agentservice.EnsureExternalCapabilityVocabulary(registry, semanticExternalCapabilityVocabulary(providers)); err != nil {
		return nil, nil, fmt.Errorf("seed external capability vocabulary: %w", err)
	}
	snapshot, err := catalog.PublishWithCoverage(providers, inventory.coverage, time.Now().UTC())
	if err != nil {
		return nil, nil, fmt.Errorf("publish IM semantic catalog: %w", err)
	}
	if routingPolicy.Enabled {
		if !routingPolicy.AllowsExecutableScope() {
			return nil, nil, fmt.Errorf("semantic_routing_policy_mode_%s", routingPolicy.Mode)
		}
		if !routingPolicy.AllowsCatalogCoverage(string(snapshot.Coverage.State)) {
			return nil, nil, fmt.Errorf("semantic_routing_catalog_coverage_%s", snapshot.Coverage.State)
		}
	}
	joined := strings.Join(names, " ")
	namedNeeds, namedPins := discoveredMCPNamedNeeds(planned)
	if len(namedNeeds) != len(wanted) {
		return nil, nil, fmt.Errorf("discovered MCP bind did not mint an x:mcp need")
	}
	policyConstraints, err := h.semanticCapabilityPolicyConstraints(userID)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve IM semantic capability policy: %w", err)
	}
	host := semanticHostContextFromRequest(ctx, channel)
	policyConstraints = append(policyConstraints, semanticHostContextConstraints(host)...)
	maxSelections, maxSchemaTokens := 0, 0
	if routingPolicy.Enabled {
		maxSelections, maxSchemaTokens = routingPolicy.MaxSelections, routingPolicy.MaxSchemaTokens
	}
	turnID := identity.TurnID + ":mcp-discover:" + tool.SchemaDigest([]byte(discoveredMCPBindDigestText(binds))) + ":" + strconv.FormatUint(generation, 10)
	plan, err := tool.NewToolPlanner(registry).Plan(tool.RouteRequest{
		RootTaskID:   identity.RootTaskID,
		SessionID:    identity.SessionID,
		TurnID:       turnID,
		ChannelScope: semanticChannelScope(channel),
		Snapshot:     snapshot,
		Needs:        namedNeeds,
		Constraints:  policyConstraints,
		Pins:         namedPins,
		Budget:       semanticHostPlanningBudget(maxSelections, maxSchemaTokens),
	})
	if err != nil {
		return nil, nil, fmt.Errorf("plan discovered MCP route: %w", err)
	}
	if len(plan.Unmet) > 0 && !(len(plan.Selections) > 0 && semanticUnmetOnlyBudget(plan.Unmet)) {
		logSemanticUnmetWithInventory(plan.Unmet, inventory)
		if semanticUnmetPinnedProvider(plan.Unmet) {
			labels := semanticPinnedProviderLabels(namedPins, providers, entries)
			return nil, nil, semanticPinnedProviderUnavailableError{Providers: labels, Unmet: plan.Unmet}
		}
		return nil, nil, semanticUnmetNeedsError{Unmet: plan.Unmet}
	}
	scopePlan, scopeResult := validateSemanticToolScope(plan, defsByName, identity.RootTaskID, identity.SessionID, turnID)
	if err := semanticScopeSurfaceError(scopePlan, scopeResult); err != nil {
		return nil, nil, err
	}
	for _, selection := range plan.Selections {
		if selection.RequiresConfirm {
			return nil, nil, errSemanticAwaitingConfirmation
		}
	}
	prepared := &semanticPlanPreparation{
		registry: registry, plan: plan, scopePlan: scopePlan, definitions: defsByName, schemas: schemas,
		rootTaskID: identity.RootTaskID, turnID: turnID, policy: routingPolicy,
	}
	_, surface, err := h.openPreparedSemanticCallSurface(ctx, userID, joined, channel, identity.SessionID, nil, nil, prepared)
	if err != nil {
		return nil, nil, err
	}
	rendered, err := discoveredMCPGrantDefinitions(surface, binds)
	if err != nil {
		cancelSemanticCallSurface(surface)
		return nil, nil, err
	}
	if len(rendered) == 0 {
		cancelSemanticCallSurface(surface)
		return nil, nil, fmt.Errorf("discovered MCP plan did not render the bound tool")
	}
	return rendered, surface, nil
}

// discoveredMCPGrantDefinitions keeps only the rendered grants for this bind
// set. A sibling selection, including information.lookup, is not exposed on
// the legacy surface.
func discoveredMCPGrantDefinitions(surface *semanticCallSurface, binds []discoveredMCPBind) ([]map[string]interface{}, error) {
	if surface == nil {
		return nil, fmt.Errorf("semantic tool surface is unavailable")
	}
	definitions, err := visibleSemanticCallSurfaceDefinitions(surface)
	if err != nil {
		return nil, err
	}
	wanted := make(map[string]bool, len(binds))
	for _, bind := range binds {
		wanted[discoveredMCPBindKey(bind.ServerID, bind.ToolName)] = true
	}
	out := make([]map[string]interface{}, 0, len(binds))
	for _, definition := range definitions {
		name := strings.TrimSpace(extractToolName(definition))
		grant, ok := surface.grants[name]
		if !ok {
			continue
		}
		selection, ok := semanticSelectionByID(surface.plan, grant.SelectionID)
		if !ok {
			continue
		}
		key := discoveredMCPBindKey(selection.Provider.ProviderID, selection.Provider.ImplementationID)
		if !wanted[key] {
			continue
		}
		capability := agentservice.AutoMCPCapabilityIDForBinding(selection.Provider.ProviderID, selection.Provider.ImplementationID)
		if capability == "" || selection.FitProof.MatchedCapability != capability {
			continue
		}
		out = append(out, definition)
	}
	return out, nil
}

func discoveredMCPResultNeedsFreshPlan(result string) bool {
	result = strings.ToLower(result)
	return strings.Contains(result, "binding_stale") || strings.Contains(result, "bound_execution_unavailable")
}

func (h *IMMessageHandler) discoveredMCPRoutingPolicy() (corelib.EffectiveRoutingPolicy, error) {
	if h == nil || h.app == nil {
		return corelib.EffectiveRoutingPolicy{}, nil
	}
	cfg, err := h.loadConfig()
	if err != nil {
		return corelib.EffectiveRoutingPolicy{}, fmt.Errorf("semantic_routing_config_unavailable: %w", err)
	}
	policy := cfg.EffectiveRoutingPolicy()
	if policy.Enabled && !policy.Valid {
		return corelib.EffectiveRoutingPolicy{}, fmt.Errorf("semantic_routing_policy_invalid:%s", policy.InvalidReason)
	}
	return policy, nil
}

// stashDiscoveredMCPContinuation keeps the approved tool until the reply's
// loop is prepared. The key is the user: one pending ask is in flight, and a
// later approval replaces an untaken one.
func (h *IMMessageHandler) stashDiscoveredMCPContinuation(userID string, bind discoveredMCPBind) {
	if h == nil {
		return
	}
	userID = strings.TrimSpace(userID)
	bind.ServerID = strings.TrimSpace(bind.ServerID)
	bind.ToolName = strings.TrimSpace(bind.ToolName)
	if userID == "" || bind.ServerID == "" || bind.ToolName == "" {
		h.dropDiscoveredMCPContinuation(userID)
		return
	}
	h.discoveredMCPContinuation.Store(userID, bind)
}

func (h *IMMessageHandler) takeDiscoveredMCPContinuation(userID string) (discoveredMCPBind, bool) {
	if h == nil {
		return discoveredMCPBind{}, false
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return discoveredMCPBind{}, false
	}
	raw, ok := h.discoveredMCPContinuation.LoadAndDelete(userID)
	if !ok {
		return discoveredMCPBind{}, false
	}
	bind, ok := raw.(discoveredMCPBind)
	bind.ServerID = strings.TrimSpace(bind.ServerID)
	bind.ToolName = strings.TrimSpace(bind.ToolName)
	if !ok || bind.ServerID == "" || bind.ToolName == "" {
		return discoveredMCPBind{}, false
	}
	return bind, true
}

func (h *IMMessageHandler) dropDiscoveredMCPContinuation(userID string) {
	if h == nil {
		return
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return
	}
	h.discoveredMCPContinuation.Delete(userID)
}
