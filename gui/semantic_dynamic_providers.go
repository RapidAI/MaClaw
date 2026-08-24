package main

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/skill"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// semanticDynamicInventory is one GUI-owned observation of the dynamic
// execution plane.  It joins each provider family with the corresponding
// lifecycle watermark before the common ToolCatalog is published; an empty
// discovery result is therefore never silently interpreted as complete.
type semanticDynamicInventory struct {
	mcpEntries   []agentservice.MCPToolEntry
	skillEntries []agentservice.SkillToolEntry
	coverage     tool.CatalogCoverage
}

func (h *IMMessageHandler) semanticDynamicInventory(ctx context.Context, userID string) (semanticDynamicInventory, error) {
	contracts, err := h.semanticDynamicCapabilityContracts()
	if err != nil {
		return semanticDynamicInventory{}, fmt.Errorf("load dynamic capability contracts: %w", err)
	}
	// A standalone handler has no authenticated desktop control plane. Its
	// dynamic families remain explicitly incomplete instead of being promoted
	// from package/server metadata.
	if contracts == nil {
		return semanticDynamicInventory{coverage: semanticDynamicCoverage("catalog_incomplete", "catalog_incomplete")}, nil
	}
	principal := agentservice.Principal{TenantID: semanticDesktopTenantID(), UserID: strings.TrimSpace(userID)}
	if principal.UserID == "" {
		return semanticDynamicInventory{coverage: semanticDynamicCoverage("catalog_incomplete", "catalog_incomplete")}, nil
	}
	mcpEntries, mcpCoverage := h.semanticMCPInventory(ctx, principal, contracts)
	skillEntries, skillCoverage := h.semanticSkillInventory(ctx, principal, contracts)
	return semanticDynamicInventory{
		mcpEntries: mcpEntries, skillEntries: skillEntries,
		coverage: tool.CatalogCoverage{State: tool.CatalogCoverageComplete, Families: []tool.CatalogCoverageFamily{mcpCoverage, skillCoverage}},
	}, nil
}

// semanticDynamicInventoryForPrincipal is the execution-time counterpart used
// by a bound adapter.  It reuses the exact principal scope captured by the
// plan rather than deriving authority from a model parameter or display name.
func (h *IMMessageHandler) semanticDynamicInventoryForPrincipal(ctx context.Context, principal agentservice.Principal) (semanticDynamicInventory, error) {
	contracts, err := h.semanticDynamicCapabilityContracts()
	if err != nil {
		return semanticDynamicInventory{}, fmt.Errorf("load dynamic capability contracts: %w", err)
	}
	if contracts == nil || strings.TrimSpace(principal.TenantID) == "" || strings.TrimSpace(principal.UserID) == "" {
		return semanticDynamicInventory{coverage: semanticDynamicCoverage("catalog_incomplete", "catalog_incomplete")}, nil
	}
	mcpEntries, mcpCoverage := h.semanticMCPInventory(ctx, principal, contracts)
	skillEntries, skillCoverage := h.semanticSkillInventory(ctx, principal, contracts)
	return semanticDynamicInventory{mcpEntries: mcpEntries, skillEntries: skillEntries, coverage: tool.CatalogCoverage{State: tool.CatalogCoverageComplete, Families: []tool.CatalogCoverageFamily{mcpCoverage, skillCoverage}}}, nil
}

func semanticDesktopTenantID() string { return "desktop" }

func semanticDynamicCoverage(mcpReason, skillReason string) tool.CatalogCoverage {
	return tool.CatalogCoverage{State: tool.CatalogCoverageIncomplete, ReasonCode: tool.CatalogCoverageReasonIncomplete, Families: []tool.CatalogCoverageFamily{
		{Kind: "mcp", State: tool.CatalogCoverageIncomplete, ReasonCode: normalizedSemanticCoverageReason(mcpReason)},
		{Kind: "skill", State: tool.CatalogCoverageIncomplete, ReasonCode: normalizedSemanticCoverageReason(skillReason)},
	}}
}

func normalizedSemanticCoverageReason(reason string) string {
	switch strings.TrimSpace(reason) {
	case tool.CatalogCoverageReasonNotReady:
		return tool.CatalogCoverageReasonNotReady
	default:
		return tool.CatalogCoverageReasonIncomplete
	}
}

func (h *IMMessageHandler) semanticMCPInventory(ctx context.Context, principal agentservice.Principal, contracts agentservice.DynamicCapabilityContractResolver) ([]agentservice.MCPToolEntry, tool.CatalogCoverageFamily) {
	registry := h.getMCPRegistry()
	// Publishing a semantic catalog is deliberately a pure lifecycle
	// observation. In particular, do not call getLocalMCPManager here: that
	// compatibility accessor constructs a runtime manager and turns planning
	// into an implicit provider-start path.
	local := h.peekLocalMCPManager()
	if registry == nil && local == nil {
		return nil, semanticCoverageFamily("mcp", tool.CatalogCoverageIncomplete, tool.CatalogCoverageReasonIncomplete)
	}

	entries := make([]agentservice.MCPToolEntry, 0)
	if registry != nil {
		for _, server := range registry.ListServers() {
			if normalizeMCPHealthStatus(server.HealthStatus) != mcpHealthStatusHealthy {
				return entries, semanticCoverageFamily("mcp", tool.CatalogCoverageIncomplete, tool.CatalogCoverageReasonNotReady)
			}
			// A healthy marker without a tool-list observation does not prove an
			// empty server. Do not refresh it from a request path; lifecycle will
			// publish the next bounded snapshot after discovery succeeds.
			discoveredTools, observed := registry.CachedServerTools(server.ID)
			if !observed {
				return entries, semanticCoverageFamily("mcp", tool.CatalogCoverageIncomplete, tool.CatalogCoverageReasonNotReady)
			}
			for _, discovered := range discoveredTools {
				entry := semanticMCPEntry(ctx, principal, contracts, server.ID, server.Name, discovered)
				entries = append(entries, entry)
			}
		}
		// Local configured servers are part of the MCP lifecycle even when the
		// manager has not been initialized yet. Treat that state as not-ready;
		// an empty running-tool list is complete only when no enabled local
		// server exists.
		for _, configured := range registry.ListLocalServers() {
			if configured.Disabled {
				continue
			}
			if local == nil || !local.IsRunning(configured.ID) {
				return entries, semanticCoverageFamily("mcp", tool.CatalogCoverageIncomplete, tool.CatalogCoverageReasonNotReady)
			}
		}
	}
	if local != nil {
		for _, server := range local.GetAllTools() {
			for _, discovered := range server.Tools {
				entry := semanticMCPEntry(ctx, principal, contracts, server.ServerID, server.ServerName, discovered)
				entries = append(entries, entry)
			}
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].ServerID != entries[j].ServerID {
			return entries[i].ServerID < entries[j].ServerID
		}
		return entries[i].ToolName < entries[j].ToolName
	})
	return entries, semanticCoverageFamily("mcp", tool.CatalogCoverageComplete, "")
}

func semanticMCPEntry(ctx context.Context, principal agentservice.Principal, contracts agentservice.DynamicCapabilityContractResolver, serverID, serverName string, discovered MCPToolView) agentservice.MCPToolEntry {
	entry := agentservice.MCPToolEntry{ServerID: strings.TrimSpace(serverID), ServerName: strings.TrimSpace(serverName), ToolName: strings.TrimSpace(discovered.Name), InputSchema: discovered.InputSchema}
	if contracts == nil || entry.ServerID == "" || entry.ToolName == "" {
		return entry
	}
	contract, ok := contracts.ResolveMCPDynamicContract(ctx, principal, entry.ServerID, entry.ToolName)
	if !ok || strings.TrimSpace(contract.ObservedBindingDigest) != agentservice.DynamicMCPObservedBindingDigest(entry.ServerID, entry.ToolName, entry.InputSchema) {
		return entry // Quarantined by BuildDynamicSemanticCatalog.
	}
	entry.Contract = contract
	return entry
}

func (h *IMMessageHandler) semanticSkillInventory(ctx context.Context, principal agentservice.Principal, contracts agentservice.DynamicCapabilityContractResolver) ([]agentservice.SkillToolEntry, tool.CatalogCoverageFamily) {
	executor := h.getSkillExecutor()
	if executor == nil {
		return nil, semanticCoverageFamily("skill", tool.CatalogCoverageIncomplete, tool.CatalogCoverageReasonIncomplete)
	}
	items := executor.loadSkills()
	entries := make([]agentservice.SkillToolEntry, 0, len(items))
	for _, item := range items {
		if !semanticSkillIsRunnable(item) {
			continue
		}
		entry := agentservice.SkillToolEntry{
			StableID: agentservice.DynamicSkillStableID(item), Name: strings.TrimSpace(item.Name),
			Version: strings.TrimSpace(item.Version), ContentDigest: agentservice.DynamicSkillContentDigest(item),
			Params: append([]corelib.NLSkillParam(nil), item.Params...),
		}
		if contracts != nil {
			if contract, ok := contracts.ResolveSkillDynamicContract(ctx, principal, entry.StableID); ok && strings.TrimSpace(contract.ObservedBindingDigest) == agentservice.DynamicSkillObservedBindingDigest(entry.StableID, entry.Version, entry.ContentDigest) {
				entry.Contract = contract
			}
		}
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].StableID != entries[j].StableID {
			return entries[i].StableID < entries[j].StableID
		}
		return entries[i].Name < entries[j].Name
	})
	return entries, semanticCoverageFamily("skill", tool.CatalogCoverageComplete, "")
}

func semanticSkillIsRunnable(entry corelib.NLSkillEntry) bool {
	switch normalizeSkillEntryStatus(entry.Status) {
	case skillEntryStatusActive, skillEntryStatusUnknown:
	default:
		return false
	}
	if isShellBrowserAutomationSkillEntry(entry) || skill.IsKnowledgeSkillType(entry.Type) || skill.IsInstructionOnlySkillType(entry.Type) || skill.IsAgentGuidedWorkflowSkill(&entry) {
		return false
	}
	return strings.TrimSpace(entry.Name) != ""
}

func semanticCoverageFamily(kind string, state tool.CatalogCoverageState, reason string) tool.CatalogCoverageFamily {
	return tool.CatalogCoverageFamily{Kind: kind, State: state, ReasonCode: reason, ObservedAt: time.Now().UTC()}
}

// executeSemanticDynamicProvider is the sole GUI bridge from a planned
// dynamic selection to its immutable MCP/Skill binding.  It refreshes the
// exact inventory for revalidation, never accepts provider identity from the
// model, and intentionally fails closed for receipt-bound effects until a
// GUI operation coordinator is installed.
func (c *sharedAgentLoopCallbacks) executeSemanticDynamicProvider(selection tool.PlannedSelection, argsJSON string) (tool.SelectionExecutionResult, bool) {
	return c.executeSemanticDynamicProviderWithContext(nil, selection, argsJSON)
}

func (c *sharedAgentLoopCallbacks) executeSemanticDynamicProviderWithContext(executionContext context.Context, selection tool.PlannedSelection, argsJSON string) (tool.SelectionExecutionResult, bool) {
	if c == nil || c.handler == nil || c.semanticSurface == nil {
		return tool.SelectionExecutionResult{Result: "[system rejected] semantic tool surface is unavailable", ReasonCode: "semantic_surface_unavailable"}, true
	}
	kind := strings.ToLower(strings.TrimSpace(selection.Provider.Kind))
	if kind != "mcp" && kind != "skill" {
		return tool.SelectionExecutionResult{}, false
	}
	ctx := executionContext
	var cancel context.CancelFunc
	if ctx == nil {
		ctx, cancel = c.semanticDynamicExecutionContext()
		defer cancel()
	}
	principal := agentservice.Principal{TenantID: semanticDesktopTenantID(), UserID: c.semanticSurface.scope.PrincipalID}
	inventory, err := c.handler.semanticDynamicInventoryForPrincipal(ctx, principal)
	if err != nil {
		return tool.SelectionExecutionResult{Result: "[system rejected] dynamic_catalog_incomplete", ReasonCode: "dynamic_catalog_incomplete"}, true
	}
	coverage := inventory.coverage.ForProviderKind(kind)
	if coverage.State != tool.CatalogCoverageComplete {
		return tool.SelectionExecutionResult{Result: "[system rejected] " + normalizedSemanticCoverageReason(coverage.ReasonCode), ReasonCode: normalizedSemanticCoverageReason(coverage.ReasonCode)}, true
	}
	catalog, err := agentservice.BuildDynamicSemanticCatalog(inventory.mcpEntries, inventory.skillEntries)
	if err != nil {
		return tool.SelectionExecutionResult{Result: "[system rejected] dynamic_semantic_catalog_unavailable", ReasonCode: "dynamic_semantic_catalog_unavailable"}, true
	}
	var coordinator agentservice.DynamicExternalEffectCoordinator
	if semanticDynamicSelectionRequiresReceipt(selection) {
		coordinator, err = c.handler.semanticDynamicEffectCoordinator()
		if err != nil {
			return tool.SelectionExecutionResult{Result: "[system rejected] dynamic_effect_coordinator_unavailable", ReasonCode: "dynamic_effect_coordinator_unavailable"}, true
		}
	}
	result := catalog.ExecuteSelectionWithEffects(ctx, c.semanticSurface.scope, principal, guiSemanticMCPBridge{handler: c.handler}, guiSemanticSkillBridge{handler: c.handler}, coordinator, selection, argsJSON)
	return result, true
}

// semanticDynamicSelectionRequiresReceipt mirrors the shared dynamic execution
// contract at the GUI host boundary. It is intentionally based solely on the
// immutable planned effect class, never an MCP/Skill name, description, or
// model argument.
func semanticDynamicSelectionRequiresReceipt(selection tool.PlannedSelection) bool {
	for _, effect := range selection.Effects {
		if effect == tool.EffectExternalEffect || effect == tool.EffectSensitive {
			return true
		}
	}
	return false
}

// semanticDynamicExecutionContext preserves the host turn's cancellation
// boundary for dynamic revalidation and dispatch. The fallback is only for
// direct/unit callers that have no loop lifecycle; production shared loops
// always carry LoopContext.
func (c *sharedAgentLoopCallbacks) semanticDynamicExecutionContext() (context.Context, context.CancelFunc) {
	if c != nil && c.loopCtx != nil {
		return c.loopCtx.Context()
	}
	return context.WithCancel(context.Background())
}

type guiSemanticMCPBridge struct{ handler *IMMessageHandler }

func (b guiSemanticMCPBridge) ListAvailableTools(context.Context, agentservice.Principal) []agentservice.MCPToolEntry {
	return nil
}
func (b guiSemanticMCPBridge) CallTool(context.Context, agentservice.Principal, string, string, map[string]interface{}) (string, error) {
	return "", fmt.Errorf("mcp bound execution is unavailable")
}
func (b guiSemanticMCPBridge) CallBoundTool(_ context.Context, principal agentservice.Principal, binding agentservice.MCPToolBinding, arguments map[string]interface{}) (string, error) {
	if b.handler == nil {
		return "", fmt.Errorf("mcp bound execution is unavailable")
	}
	registry := b.handler.getMCPRegistry()
	if registry != nil {
		for _, server := range registry.ListServers() {
			if server.ID != binding.ServerID {
				continue
			}
			if normalizeMCPHealthStatus(server.HealthStatus) != mcpHealthStatusHealthy {
				return "", fmt.Errorf("mcp_binding_stale")
			}
			// Inventory revalidation already required a lifecycle-owned cached
			// tools/list observation. Preserve that property for the final
			// transport guard too: an execution path must not turn cache loss
			// into a fresh discovery/connect attempt.
			discoveredTools, observed := registry.CachedServerTools(server.ID)
			if !observed {
				return "", fmt.Errorf("mcp_binding_stale")
			}
			for _, discovered := range discoveredTools {
				if discovered.Name == binding.ToolName {
					// DynamicSemanticCatalog has just compared the selected binding to
					// this fresh inventory, including schema and contract identity.
					return registry.CallToolForOwner(principal.UserID, binding.ServerID, binding.ToolName, arguments)
				}
			}
			return "", fmt.Errorf("mcp_binding_stale")
		}
	}
	// Never construct/start a local provider during a bound semantic call. A
	// selected binding was admitted only from the lifecycle-owned ready
	// snapshot; if that runtime vanished afterwards, it is stale.
	local := b.handler.peekLocalMCPManager()
	if local != nil {
		for _, server := range local.GetAllTools() {
			if server.ServerID != binding.ServerID {
				continue
			}
			for _, discovered := range server.Tools {
				if discovered.Name == binding.ToolName {
					// Use the caller's principal when entering the local MCP
					// runtime. The manager may create an owner-dedicated client,
					// but it can only do so from the lifecycle-approved server
					// entry already checked above; no model-controlled provider
					// identity or anonymous cross-session client is involved.
					return local.CallToolForOwner(principal.UserID, binding.ServerID, binding.ToolName, arguments)
				}
			}
			return "", fmt.Errorf("mcp_binding_stale")
		}
	}
	return "", fmt.Errorf("mcp_binding_stale")
}

type guiSemanticSkillBridge struct{ handler *IMMessageHandler }

func (b guiSemanticSkillBridge) ListSkills(context.Context, agentservice.Principal) []agentservice.SkillToolEntry {
	return nil
}
func (b guiSemanticSkillBridge) InstallSkill(context.Context, agentservice.Principal, map[string]interface{}) ([]corelib.NLSkillEntry, error) {
	return nil, fmt.Errorf("skill bound execution is unavailable")
}
func (b guiSemanticSkillBridge) RunSkill(context.Context, agentservice.Principal, string, map[string]interface{}) (string, error) {
	return "", fmt.Errorf("skill bound execution is unavailable")
}
func (b guiSemanticSkillBridge) SearchSkills(context.Context, agentservice.Principal, string) ([]agentservice.SkillSearchResult, error) {
	return nil, fmt.Errorf("skill bound execution is unavailable")
}
func (b guiSemanticSkillBridge) CallBoundSkill(_ context.Context, _ agentservice.Principal, binding agentservice.SkillBinding, arguments map[string]interface{}) (string, error) {
	if b.handler == nil {
		return "", fmt.Errorf("skill_bound_execution_unavailable")
	}
	executor := b.handler.getSkillExecutor()
	if executor == nil {
		return "", fmt.Errorf("skill_bound_execution_unavailable")
	}
	return executor.executeBoundSkill(binding.StableID, binding.Name, binding.Version, binding.ContentDigest, arguments)
}
