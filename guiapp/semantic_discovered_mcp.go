package guiapp

import (
	"context"
	"fmt"
	"log"
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
	discoveredMCPBoundSentence   = "Call the installed MCP function named above. That name stays the same for later calls. Do not use the removed MCP gateway. Do not rediscover the same name. Do not ask the user to continue."
	discoveredMCPUnboundSentence = "Installed MCP matches stay unbound until this lookup names one compound tool, or the lookup has exactly one compound match. Pass that exact tool name to discover_tool once. The host binds it. Do not use the removed MCP gateway."
	discoveredMCPNoLoopSentence  = "The host identified an installed MCP tool, but this lookup has no loop to attach the bind. Do not use the removed MCP gateway."
)

// constraintsWithoutChatConfirmation drops require_confirmation. An installed
// server is already the user's authorization. Deny constraints stay.
func constraintsWithoutChatConfirmation(in []tool.RoutingConstraint) []tool.RoutingConstraint {
	if len(in) == 0 {
		return in
	}
	out := make([]tool.RoutingConstraint, 0, len(in))
	for _, constraint := range in {
		if constraint.Effect == "require_confirmation" {
			continue
		}
		out = append(out, constraint)
	}
	return out
}

// releaseInstalledMCPConfirmation removes chat-confirmation dependencies from
// installed MCP selections. Other tools keep theirs. The tool is then ready
// to call; a confirmation flag here used to reject the whole turn.
func releaseInstalledMCPConfirmation(plan *tool.ToolPlan) {
	if plan == nil {
		return
	}
	for i := range plan.Selections {
		selection := &plan.Selections[i]
		if !strings.EqualFold(strings.TrimSpace(selection.Provider.Kind), "mcp") {
			continue
		}
		selection.RequiresConfirm = false
		selection.ConfirmationID = ""
		kept := selection.Requires[:0]
		for _, requirement := range selection.Requires {
			if strings.HasPrefix(strings.TrimSpace(requirement), "confirmation:") {
				continue
			}
			kept = append(kept, requirement)
		}
		selection.Requires = kept
	}
}

// readyInstalledMCPBinds is every observed MCP tool the host can publish.
// Installation is enough; the model does not have to discover the name first.
func readyInstalledMCPBinds(inventory semanticDynamicInventory) []discoveredMCPBind {
	seen := make(map[string]bool, len(inventory.mcpEntries))
	binds := make([]discoveredMCPBind, 0, len(inventory.mcpEntries))
	for _, entry := range inventory.mcpEntries {
		if assessDiscoveredMCPBind(inventory, entry.ServerID, entry.ToolName) != "" {
			continue
		}
		key := discoveredMCPBindKey(entry.ServerID, entry.ToolName)
		if seen[key] {
			continue
		}
		seen[key] = true
		binds = append(binds, discoveredMCPBind{ServerID: entry.ServerID, ToolName: entry.ToolName})
	}
	return discoveredMCPDistinctBinds(binds)
}

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
	// This entry's own observation finished. A sibling that is still starting
	// leaves the mcp family not_ready and must not hide this tool.
	return ""
}

// discoveredMCPSpellIsAddress reports that the stored server and tool
// spelling is the capability address itself. A folded name such as "Foo Bar"
// and the spelling "foo_bar" publish one x:mcp id. The planner can require
// that id once.
func discoveredMCPSpellIsAddress(bind discoveredMCPBind, address tool.CapabilityID) bool {
	rest := strings.TrimPrefix(string(address), "x:mcp:")
	serverSlug, toolSlug, ok := strings.Cut(rest, ":")
	if !ok || serverSlug == "" || toolSlug == "" {
		return false
	}
	return strings.TrimSpace(bind.ServerID) == serverSlug && strings.TrimSpace(bind.ToolName) == toolSlug
}

// discoveredMCPDistinctBinds keeps one bind per x:mcp address. The spelling
// that already is the address wins. Claiming every folded name makes the
// next request reject the whole set, so none of the functions are listed.
func discoveredMCPDistinctBinds(binds []discoveredMCPBind) []discoveredMCPBind {
	best := make(map[tool.CapabilityID]int, len(binds))
	order := make([]discoveredMCPBind, 0, len(binds))
	for _, bind := range binds {
		address := agentservice.AutoMCPCapabilityIDForBinding(bind.ServerID, bind.ToolName)
		if address == "" {
			continue
		}
		order = append(order, bind)
		index := len(order) - 1
		prev, ok := best[address]
		if !ok || (!discoveredMCPSpellIsAddress(order[prev], address) && discoveredMCPSpellIsAddress(bind, address)) {
			best[address] = index
		}
	}
	keep := make(map[int]bool, len(best))
	for _, index := range best {
		keep[index] = true
	}
	out := make([]discoveredMCPBind, 0, len(best))
	for index, bind := range order {
		if keep[index] {
			out = append(out, bind)
		}
	}
	return out
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
	candidates := make([]discoveredMCPBind, len(bindable))
	for i, item := range bindable {
		candidates[i] = discoveredMCPBind{ServerID: item.serverID, ToolName: item.toolName}
	}
	keep := make(map[string]bool, len(candidates))
	for _, bind := range discoveredMCPDistinctBinds(candidates) {
		keep[discoveredMCPBindKey(bind.ServerID, bind.ToolName)] = true
	}
	kept := bindable[:0]
	emitted := map[string]bool{}
	for _, item := range bindable {
		key := discoveredMCPBindKey(item.serverID, item.toolName)
		if keep[key] {
			if emitted[key] {
				continue
			}
			emitted[key] = true
			kept = append(kept, item)
			continue
		}
		reasons = append(reasons, fmt.Sprintf("%s/%s uses the same installed MCP address as another tool. The host cannot bind it.", strings.TrimSpace(item.serverID), strings.TrimSpace(item.toolName)))
	}
	return kept, reasons
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
	// The sidecar lists installed MCP tools only. The skill catalog is not
	// an input to this plan, and loading it on every new bind set made the
	// request wait on a family this surface does not publish.
	inventory, err := h.semanticInstalledMCPInventory(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("load installed MCP inventory: %w", err)
	}
	planned := make([]discoveredMCPBind, 0, len(binds))
	seen := make(map[string]bool, len(binds))
	skipped := make([]string, 0)
	for _, bind := range binds {
		key := discoveredMCPBindKey(bind.ServerID, bind.ToolName)
		if seen[key] {
			continue
		}
		seen[key] = true
		// One bind that cannot be planned must not hide the others.
		// Discovery already reports that tool on its own.
		if reason := assessDiscoveredMCPBind(inventory, bind.ServerID, bind.ToolName); reason != "" {
			skipped = append(skipped, reason)
			continue
		}
		planned = append(planned, bind)
	}
	// One address is one need. A stored pair that folds together still
	// publishes the spelling that is the address, instead of rejecting
	// every tool in the set.
	planned = discoveredMCPDistinctBinds(planned)
	wanted := make(map[string]discoveredMCPBind, len(planned))
	names := make([]string, 0, len(planned))
	for _, bind := range planned {
		wanted[discoveredMCPBindKey(bind.ServerID, bind.ToolName)] = bind
		names = append(names, strings.TrimSpace(bind.ToolName))
	}
	if len(planned) == 0 {
		if len(skipped) > 0 {
			return nil, nil, fmt.Errorf("%s", strings.Join(skipped, "; "))
		}
		return nil, nil, fmt.Errorf("discovered MCP bind did not mint an x:mcp need")
	}
	if len(skipped) > 0 {
		log.Printf("[semantic-routing] discovered MCP bind left unpublished: %s", strings.Join(skipped, "; "))
	}
	// Keep the first tools/list row for each bind. A repeated name projects
	// to the same adapter, and that collision fails the whole catalog.
	entries := make([]agentservice.MCPToolEntry, 0, len(wanted))
	seenEntry := make(map[string]bool, len(wanted))
	for _, entry := range inventory.mcpEntries {
		key := discoveredMCPBindKey(entry.ServerID, entry.ToolName)
		if _, ok := wanted[key]; !ok || seenEntry[key] {
			continue
		}
		seenEntry[key] = true
		entries = append(entries, entry)
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
		// One definition the host cannot authorize must not hide the tools
		// whose schemas already projected.
		definition, ok := dynamicCatalog.Definitions[provider.AdapterName]
		if !ok {
			log.Printf("[semantic-routing] discovered MCP definition missing for %q", provider.AdapterName)
			continue
		}
		invocationSchema, err := semanticInvocationSchema(definition)
		if err != nil {
			log.Printf("[semantic-routing] discovered MCP schema rejected for %q: %v", provider.AdapterName, err)
			continue
		}
		authorization, err := tool.NewParameterAuthorization(invocationSchema)
		if err != nil {
			log.Printf("[semantic-routing] discovered MCP authorization rejected for %q: %v", provider.AdapterName, err)
			continue
		}
		provider.ParameterAuthorization = authorization
		defsByName[provider.AdapterName] = definition
		schemas[provider.AdapterName] = invocationSchema
		providers = append(providers, provider)
	}
	planned, wanted, names = alignDiscoveredMCPBinds(planned, providers)
	if len(providers) == 0 || len(planned) == 0 {
		return nil, nil, fmt.Errorf("discovered MCP bind is not a published semantic provider")
	}
	// Coverage is the inventory's observation watermark. Do not mark builtin
	// or channel complete: this catalog does not publish those providers.
	// One capability that cannot be seeded is that tool's problem. The tools
	// whose vocabulary registered still publish.
	for _, capID := range semanticExternalCapabilityVocabulary(providers) {
		if err := agentservice.EnsureExternalCapabilityVocabulary(registry, []tool.CapabilityID{capID}); err != nil {
			log.Printf("[semantic-routing] installed MCP capability %s was not seeded: %v", capID, err)
		}
	}
	snapshot, providers, err := publishDiscoveredMCPProviders(catalog, providers, inventory.coverage)
	if err != nil {
		return nil, nil, fmt.Errorf("publish IM semantic catalog: %w", err)
	}
	planned, wanted, names = alignDiscoveredMCPBinds(planned, providers)
	retainDiscoveredMCPDefinitions(providers, defsByName, schemas)
	if len(providers) == 0 || len(planned) == 0 {
		return nil, nil, fmt.Errorf("discovered MCP bind is not a published semantic provider")
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
	// Installation is the authorization. A chat confirmation constraint
	// would leave the tool unpublished, which is the loop the user cannot
	// finish. Deny constraints stay.
	policyConstraints = constraintsWithoutChatConfirmation(policyConstraints)
	// The turn's selection cap is for the managed plan. Every ready
	// installed tool has to be listable; a cap of eight hid the rest.
	maxSelections, maxSchemaTokens := 0, 0
	turnID := identity.TurnID + ":mcp-discover:" + tool.SchemaDigest([]byte(discoveredMCPBindDigestText(binds))) + ":" + strconv.FormatUint(generation, 10)
	plan, err := tool.NewToolPlanner(registry).Plan(tool.RouteRequest{
		RootTaskID:                        identity.RootTaskID,
		SessionID:                         identity.SessionID,
		TurnID:                            turnID,
		ChannelScope:                      semanticChannelScope(channel),
		Snapshot:                          snapshot,
		Needs:                             namedNeeds,
		Constraints:                       policyConstraints,
		Pins:                              namedPins,
		Budget:                            semanticHostPlanningBudget(maxSelections, maxSchemaTokens),
		ReadyMCPMutationsOnPartialCatalog: true,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("plan discovered MCP route: %w", err)
	}
	releaseInstalledMCPConfirmation(&plan)
	if len(plan.Unmet) > 0 && !(len(plan.Selections) > 0 && semanticUnmetOnlyBudget(plan.Unmet)) {
		logSemanticUnmetWithInventory(plan.Unmet, inventory)
		// A pinned tool the planner cannot select stays off this list. The
		// tools it did select still render. Refusing the set hid every
		// installed server behind one rejected sibling.
		if len(plan.Selections) == 0 {
			if semanticUnmetPinnedProvider(plan.Unmet) {
				labels := semanticPinnedProviderLabels(namedPins, providers, entries)
				return nil, nil, semanticPinnedProviderUnavailableError{Providers: labels, Unmet: plan.Unmet}
			}
			return nil, nil, semanticUnmetNeedsError{Unmet: plan.Unmet}
		}
	}
	scopePlan, scopeResult := validateSemanticToolScope(plan, defsByName, identity.RootTaskID, identity.SessionID, turnID)
	if err := semanticScopeSurfaceError(scopePlan, scopeResult); err != nil {
		return nil, nil, err
	}
	prepared := &semanticPlanPreparation{
		registry: registry, plan: plan, scopePlan: scopePlan, definitions: defsByName, schemas: schemas,
		rootTaskID: identity.RootTaskID, turnID: turnID, policy: routingPolicy,
	}
	_, surface, err := h.openPreparedSemanticCallSurface(ctx, userID, joined, channel, identity.SessionID, nil, nil, prepared)
	if err != nil {
		return nil, nil, err
	}
	rendered, err := discoveredMCPGrantDefinitions(surface, planned)
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

// alignDiscoveredMCPBinds keeps the binds whose providers are still in the
// set about to be planned. A sibling that failed projection or publication
// stays on the loop and is omitted here, so the next request can try it
// again without withholding the tools that are ready.
func alignDiscoveredMCPBinds(planned []discoveredMCPBind, providers []tool.ProviderSpec) ([]discoveredMCPBind, map[string]discoveredMCPBind, []string) {
	built := make(map[string]bool, len(providers))
	for _, provider := range providers {
		built[discoveredMCPBindKey(provider.Binding.ProviderID, provider.Binding.ImplementationID)] = true
	}
	next := make([]discoveredMCPBind, 0, len(planned))
	wanted := make(map[string]discoveredMCPBind, len(planned))
	names := make([]string, 0, len(planned))
	dropped := make([]string, 0)
	for _, bind := range planned {
		key := discoveredMCPBindKey(bind.ServerID, bind.ToolName)
		if !built[key] {
			dropped = append(dropped, strings.TrimSpace(bind.ServerID)+"/"+strings.TrimSpace(bind.ToolName))
			continue
		}
		next = append(next, bind)
		wanted[key] = bind
		names = append(names, strings.TrimSpace(bind.ToolName))
	}
	if len(dropped) > 0 {
		log.Printf("[semantic-routing] discovered MCP bind left unpublished: %s", strings.Join(dropped, "; "))
	}
	return next, wanted, names
}

// retainDiscoveredMCPDefinitions drops schemas for providers that were not
// published. Scope validation only has to close the tools that will render.
func retainDiscoveredMCPDefinitions(providers []tool.ProviderSpec, defs, schemas map[string]map[string]interface{}) {
	keep := make(map[string]bool, len(providers))
	for _, provider := range providers {
		keep[strings.TrimSpace(provider.AdapterName)] = true
	}
	for name := range defs {
		if keep[name] {
			continue
		}
		delete(defs, name)
		delete(schemas, name)
	}
}

// publishDiscoveredMCPProviders publishes the installed set. One provider the
// catalog rejects is removed and the rest are published again. The rejection
// is that tool's contract, not a reason to hide every other installed tool.
func publishDiscoveredMCPProviders(catalog *tool.ToolCatalog, providers []tool.ProviderSpec, coverage tool.CatalogCoverage) (tool.ToolCatalogSnapshot, []tool.ProviderSpec, error) {
	kept := providers
	for {
		snapshot, err := catalog.PublishWithCoverage(kept, coverage, time.Now().UTC())
		if err == nil {
			return snapshot, kept, nil
		}
		dropped, ok := discoveredMCPProviderNamedInError(err, kept)
		if !ok || len(kept) <= 1 {
			return tool.ToolCatalogSnapshot{}, kept, err
		}
		log.Printf("[semantic-routing] installed MCP provider %s left unpublished: %v", dropped.AdapterName, err)
		next := make([]tool.ProviderSpec, 0, len(kept)-1)
		for _, provider := range kept {
			if provider.AdapterName == dropped.AdapterName {
				continue
			}
			next = append(next, provider)
		}
		if len(next) == len(kept) {
			return tool.ToolCatalogSnapshot{}, kept, err
		}
		kept = next
	}
}

func discoveredMCPProviderNamedInError(err error, providers []tool.ProviderSpec) (tool.ProviderSpec, bool) {
	if err == nil {
		return tool.ProviderSpec{}, false
	}
	msg := err.Error()
	var found tool.ProviderSpec
	foundLen := 0
	for _, provider := range providers {
		name := strings.TrimSpace(provider.AdapterName)
		if name == "" || len(name) <= foundLen {
			continue
		}
		if strings.Contains(msg, `"`+name+`"`) {
			found = provider
			foundLen = len(name)
		}
	}
	if foundLen == 0 {
		return tool.ProviderSpec{}, false
	}
	return found, true
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

// discoveredMCPPublishedBinds is the bind set the surface actually selected.
// Reuse compares this with the loop list. A sibling that failed assess stays
// on the loop, so the next request builds again instead of reusing a surface
// that omitted it.
func discoveredMCPPublishedBinds(surface *semanticCallSurface) []discoveredMCPBind {
	if surface == nil {
		return nil
	}
	seen := map[string]bool{}
	out := make([]discoveredMCPBind, 0, len(surface.plan.Selections))
	for _, selection := range surface.plan.Selections {
		serverID := strings.TrimSpace(selection.Provider.ProviderID)
		toolName := strings.TrimSpace(selection.Provider.ImplementationID)
		capability := agentservice.AutoMCPCapabilityIDForBinding(serverID, toolName)
		if capability == "" || selection.FitProof.MatchedCapability != capability {
			continue
		}
		key := discoveredMCPBindKey(serverID, toolName)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, discoveredMCPBind{ServerID: serverID, ToolName: toolName})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ServerID != out[j].ServerID {
			return out[i].ServerID < out[j].ServerID
		}
		return out[i].ToolName < out[j].ToolName
	})
	if len(out) == 0 {
		return nil
	}
	return out
}

// dropDiscoveredMCPFoldedAliases removes loop binds that publish the same
// x:mcp address as a bind this surface already selected. A second spelling
// cannot become another grant. Leaving it on the loop makes the next request
// cancel the live surface and mint a replacement for the same tool.
// An alias is kept when the published spelling is no longer on the loop, so
// the next publish still has one name for that address.
func dropDiscoveredMCPFoldedAliases(loop *LoopContext, published []discoveredMCPBind) {
	if loop == nil || len(published) == 0 {
		return
	}
	keepByAddress := make(map[tool.CapabilityID]string, len(published))
	for _, bind := range published {
		address := agentservice.AutoMCPCapabilityIDForBinding(bind.ServerID, bind.ToolName)
		if address == "" {
			continue
		}
		keepByAddress[address] = discoveredMCPBindKey(bind.ServerID, bind.ToolName)
	}
	if len(keepByAddress) == 0 {
		return
	}
	stored := loop.discoveredMCPBindList()
	present := make(map[string]bool, len(stored))
	for _, bind := range stored {
		present[discoveredMCPBindKey(bind.ServerID, bind.ToolName)] = true
	}
	forget := make(map[string]bool)
	for _, bind := range stored {
		address := agentservice.AutoMCPCapabilityIDForBinding(bind.ServerID, bind.ToolName)
		keep, ok := keepByAddress[address]
		if !ok {
			continue
		}
		key := discoveredMCPBindKey(bind.ServerID, bind.ToolName)
		if key == keep || !present[keep] {
			continue
		}
		forget[key] = true
	}
	loop.forgetDiscoveredMCPBinds(forget)
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
