package guiapp

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	mcputil "github.com/RapidAI/CodeClaw/corelib/mcp"
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
	principal := semanticContractPrincipal(agentservice.Principal{TenantID: semanticDesktopTenantID(), UserID: strings.TrimSpace(userID)})
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

// semanticContractPrincipal collapses a desktop project session onto the
// desktop owner for dynamic-contract lookup only. Session residue and the
// invocation scope keep the raw session id. Coding principals such as
// "principal" are left unchanged.
func semanticContractPrincipal(principal agentservice.Principal) agentservice.Principal {
	principal.TenantID = strings.TrimSpace(principal.TenantID)
	principal.UserID = strings.TrimSpace(principal.UserID)
	if trustedDesktopPrincipal(principal.UserID) {
		principal.UserID = desktopUserID
	}
	return principal
}

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
	localCapability := map[string]*corelib.MCPServerCapabilityRef{}
	notReady := semanticCoverageFamily("mcp", tool.CatalogCoverageIncomplete, tool.CatalogCoverageReasonNotReady)
	if registry != nil {
		for _, server := range registry.ListServers() {
			status, discoveredTools, observed := registry.remoteHealthObservation(server.ID)
			retryDue := h.app != nil && h.app.mcpRuntimeSyncShouldRetry(server.ID)
			if remoteMCPMemberOpen(status, observed, server.RuntimeSyncStatus, retryDue) {
				// This process has not finished looking at a server the
				// lifecycle still owes an observation. Publish nothing from
				// this pass, and do not probe from the turn.
				return nil, notReady
			}
			// Finished negative observation, or a stale in-memory success that
			// a durable pending/needs_review record has already superseded.
			// Keep the known tools so a need that only this server serves stays
			// provider_not_ready, without hiding a sibling whose observation
			// is still admissible.
			blocked := !mcpHealthObservationSucceeded(status) || mcpRuntimeSyncStatusClosed(server.RuntimeSyncStatus)
			if !observed {
				continue
			}
			for _, discovered := range discoveredTools {
				entry := semanticMCPEntry(ctx, principal, contracts, server.ID, server.Name, server.Capability, discovered)
				entry.RuntimeBlocked = blocked
				entries = append(entries, entry)
			}
		}
		// The automatic local runtime is the launch set (enabled and
		// AutoStart) plus any server that is already running. An enabled
		// server with AutoStart false is intentionally offline until the
		// operator starts it. A launch-set server is open only while startup
		// is still allowed to start it and has not finished that attempt.
		// needs_review, or a pending retry that is not due, will not start a
		// process; leaving it open would keep every other MCP server unusable.
		// A running process whose durable record is still a blocker keeps its
		// tools, but they are not selectable. A disabled or removed server is
		// not admitted, even if its process has not exited yet.
		blockedLocal := map[string]bool{}
		admittedLocal := map[string]bool{}
		for _, configured := range registry.ListLocalServers() {
			id := strings.TrimSpace(configured.ID)
			if configured.Capability != nil {
				localCapability[id] = configured.Capability
			}
			if configured.Disabled {
				continue
			}
			running := local != nil && local.IsRunning(configured.ID)
			if !configured.AutoStart && !running {
				continue
			}
			if !running {
				if localMCPStartStillOwed(h.app, configured, local) {
					return nil, notReady
				}
				continue
			}
			admittedLocal[id] = true
			if h.app != nil && h.app.mcpRuntimeSyncPending(configured.ID) {
				blockedLocal[id] = true
			}
		}
		if local != nil {
			for _, server := range local.GetAllTools() {
				id := strings.TrimSpace(server.ServerID)
				if !admittedLocal[id] {
					continue
				}
				for _, discovered := range server.Tools {
					entry := semanticMCPEntry(ctx, principal, contracts, server.ServerID, server.ServerName, localCapability[id], discovered)
					entry.RuntimeBlocked = blockedLocal[id]
					entries = append(entries, entry)
				}
			}
		}
	} else if local != nil {
		for _, server := range local.GetAllTools() {
			for _, discovered := range server.Tools {
				entry := semanticMCPEntry(ctx, principal, contracts, server.ServerID, server.ServerName, nil, discovered)
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

// remoteMCPMemberOpen reports that the automatic remote runtime still owes
// this process a finished tools/list outcome. A durable needs_review record,
// or a pending retry that is not yet due, is a closed admission decision:
// it must not keep every other MCP server unusable. Unknown health with a
// probe still due is open, including a ready marker left by a previous
// process, because the tool cache does not survive restart.
func remoteMCPMemberOpen(status mcpHealthStatus, toolsObserved bool, syncStatus string, retryDue bool) bool {
	switch normalizeMCPHealthStatus(status) {
	case mcpHealthStatusHealthy, mcpHealthStatusSlow:
		return !toolsObserved
	case mcpHealthStatusDegraded, mcpHealthStatusUnavailable:
		return false
	default:
		switch strings.TrimSpace(syncStatus) {
		case "needs_review":
			return false
		case "pending":
			return retryDue
		default:
			return true
		}
	}
}

// mcpRuntimeSyncStatusClosed reports a durable admission decision that
// supersedes a stale in-memory success. Ready is stored as an empty status
// by ListServers, so a genuine healthy observation stays selectable.
func mcpRuntimeSyncStatusClosed(status string) bool {
	switch strings.TrimSpace(status) {
	case "pending", "needs_review":
		return true
	default:
		return false
	}
}

// localMCPStartStillOwed is true while startup is still allowed to start this
// server and no config sync has finished that attempt. needs_review, or a
// pending retry that is not due, will not start a process. A manager object
// by itself is not an attempt: hub wiring constructs it before startup sync.
func localMCPStartStillOwed(app *App, entry corelib.LocalMCPServerEntry, local *LocalMCPManager) bool {
	if app != nil && !app.mcpRuntimeSyncAllowsAutomaticStart(entry) {
		return false
	}
	if local == nil || local.configSyncInProgress() || !local.configSyncFinished() {
		return true
	}
	return false
}

func semanticMCPEntry(ctx context.Context, principal agentservice.Principal, contracts agentservice.DynamicCapabilityContractResolver, serverID, serverName string, capability *corelib.MCPServerCapabilityRef, discovered MCPToolView) agentservice.MCPToolEntry {
	entry := agentservice.MCPToolEntry{ServerID: strings.TrimSpace(serverID), ServerName: strings.TrimSpace(serverName), ToolName: strings.TrimSpace(discovered.Name), InputSchema: discovered.InputSchema}
	if capability != nil {
		entry.CapabilityGlobalKey = strings.TrimSpace(capability.GlobalKey)
		entry.InstalledCapabilityID = strings.TrimSpace(capability.CapabilityID)
	}
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
	principal := agentservice.Principal{TenantID: semanticDesktopTenantID(), UserID: strings.TrimSpace(c.semanticSurface.scope.PrincipalID)}
	inventory, err := c.handler.semanticDynamicInventoryForPrincipal(ctx, semanticContractPrincipal(principal))
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
	return tool.SelectionRequiresExternalReceipt(selection)
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
			if !mcpHealthObservationSucceeded(server.HealthStatus) {
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
					return boundMCPToolContent(registry.CallToolForOwner(principal.UserID, binding.ServerID, binding.ToolName, arguments))
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
					return boundMCPToolContent(local.CallToolForOwner(principal.UserID, binding.ServerID, binding.ToolName, arguments))
				}
			}
			return "", fmt.Errorf("mcp_binding_stale")
		}
	}
	return "", fmt.Errorf("mcp_binding_stale")
}

func boundMCPToolContent(raw string, err error) (string, error) {
	if err != nil {
		return "", err
	}
	return mcputil.ToolCallContent(raw)
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
