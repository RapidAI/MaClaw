package guiapp

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
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

func (h *IMMessageHandler) semanticDynamicInventory(ctx context.Context) (semanticDynamicInventory, error) {
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
	// Dynamic contracts are host state: the lifecycle publisher writes them
	// under the single desktop contract principal, so every session reaching
	// this host's semantic surface resolves against that same scope. The
	// session principal stays on the invocation scope instead. One snapshot
	// keeps every entry in the same control-plane generation.
	principal := semanticDesktopContractPrincipal()
	resolver, err := agentservice.DynamicCapabilityContractSnapshot(contracts, principal)
	if err != nil {
		return semanticDynamicInventory{}, fmt.Errorf("snapshot dynamic capability contracts: %w", err)
	}
	mcpEntries, mcpCoverage := h.semanticMCPInventory(ctx, principal, resolver)
	skillEntries, skillCoverage := h.semanticSkillInventory(ctx, principal, resolver)
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
	resolver, err := agentservice.DynamicCapabilityContractSnapshot(contracts, principal)
	if err != nil {
		return semanticDynamicInventory{}, fmt.Errorf("snapshot dynamic capability contracts: %w", err)
	}
	mcpEntries, mcpCoverage := h.semanticMCPInventory(ctx, principal, resolver)
	skillEntries, skillCoverage := h.semanticSkillInventory(ctx, principal, resolver)
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
	localCapability := map[string]*corelib.MCPServerCapabilityRef{}
	notReady := semanticCoverageFamily("mcp", tool.CatalogCoverageIncomplete, tool.CatalogCoverageReasonNotReady)
	if registry != nil {
		for _, server := range registry.ListServers() {
			status, discoveredTools, observed := registry.remoteHealthObservation(server.ID)
			if !observed && normalizeMCPHealthStatus(status) == mcpHealthStatusUnknown {
				// This process never finished looking at the server. A fresh
				// durable observation for the same configured endpoint is the
				// restart-time equivalent of the in-memory cache: it admits
				// the tools and kicks a background revalidation so the record
				// stays a cache, never a substitute for re-checking.
				if durableTools, ok := durableRemoteMCPTools(registry, server.ID); ok {
					// The durable record IS a finished successful observation
					// (fresh, same endpoint): admit it as such for this pass and
					// let the background refresh overwrite it either way.
					discoveredTools, observed = durableTools, true
					status = mcpHealthStatusHealthy
					scheduleRemoteMCPObservationRefresh(registry, server.ID)
				}
			}
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
		durableLocalToolSets := map[string]LocalMCPToolSet{}
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
				// An enabled server that does not auto-start is offline by
				// operator choice, but its tools are still installed:
				// CallToolForOwner starts the process on demand. Admit it
				// from the durable tools observation so the server is
				// addressable instead of invisible; the runtime call gate
				// marks it blocked whenever an on-demand start would be
				// refused right now.
				if durableTools, ok := durableLocalMCPTools(registry, configured); ok {
					admittedLocal[id] = true
					durableLocalToolSets[id] = LocalMCPToolSet{ServerID: id, ServerName: configured.Name, Tools: durableTools}
					if h.app == nil {
						blockedLocal[id] = true
					} else if blocked, _ := h.app.mcpRuntimeSyncGate(id); blocked {
						blockedLocal[id] = true
					}
				}
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
		for id, set := range durableLocalToolSets {
			for _, discovered := range set.Tools {
				entry := semanticMCPEntry(ctx, principal, contracts, set.ServerID, set.ServerName, localCapability[id], discovered)
				entry.RuntimeBlocked = blockedLocal[id]
				entries = append(entries, entry)
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

// semanticDynamicCatalogRejection is the catalog outcome for a call that
// has not started. An inventory error and incomplete coverage are the same
// fact the executor reports. A caller in front of one-shot admission must
// return this instead of continuing: admitting it consumes the only grant
// for a tool that never ran.
func semanticDynamicCatalogRejection(inventory semanticDynamicInventory, err error, kind string) (tool.SelectionExecutionResult, bool) {
	if err != nil {
		return tool.SelectionExecutionResult{Result: "[system rejected] dynamic_catalog_incomplete", ReasonCode: "dynamic_catalog_incomplete"}, true
	}
	coverage := inventory.coverage.ForProviderKind(kind)
	if coverage.State != tool.CatalogCoverageComplete {
		reason := normalizedSemanticCoverageReason(coverage.ReasonCode)
		return tool.SelectionExecutionResult{Result: "[system rejected] " + reason, ReasonCode: reason}, true
	}
	return tool.SelectionExecutionResult{}, false
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
	// Binding revalidation must observe the same host contract scope the plan
	// was admitted under; the session principal is kept only as the call
	// owner for the eventual bound dispatch.
	inventory, err := c.handler.semanticDynamicInventoryForPrincipal(ctx, semanticDesktopContractPrincipal())
	if rejected, ok := semanticDynamicCatalogRejection(inventory, err, kind); ok {
		return rejected, true
	}
	catalog, err := agentservice.BuildDynamicSemanticCatalog(inventory.mcpEntries, inventory.skillEntries)
	if err != nil {
		return tool.SelectionExecutionResult{Result: "[system rejected] dynamic_semantic_catalog_unavailable", ReasonCode: "dynamic_semantic_catalog_unavailable"}, true
	}
	var coordinator agentservice.DynamicExternalEffectCoordinator
	if semanticDynamicSelectionRequiresReceipt(selection) {
		// Unreviewed external-effect calls settle through the receipt boundary
		// AND through a per-server session confirmation: an installed vendor
		// tool must not gain silent write authority merely by being installed.
		// Read-only selections skip this gate entirely.
		if gated, ok := c.mcpServerWriteGateForSelection(selection, inventory.mcpEntries); ok {
			return gated, true
		}
		coordinator, err = c.handler.semanticDynamicEffectCoordinator()
		if err != nil {
			return tool.SelectionExecutionResult{Result: "[system rejected] dynamic_effect_coordinator_unavailable", ReasonCode: "dynamic_effect_coordinator_unavailable"}, true
		}
	}
	c.emitSemanticSelectionProgress(selection, argsJSON)
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
				// A finished negative observation this process stays
				// authoritative; only an unknown status may fall back to
				// the durable record, the same rule the inventory applied
				// when it admitted the binding. The HTTP call itself is the
				// final revalidation and fails naturally when the server is
				// actually gone.
				if normalizeMCPHealthStatus(server.HealthStatus) == mcpHealthStatusUnknown {
					if durableTools, ok := durableRemoteMCPTools(registry, server.ID); ok {
						for _, discovered := range durableTools {
							if discovered.Name == binding.ToolName {
								return boundMCPToolContent(registry.CallToolForOwner(principal.UserID, binding.ServerID, binding.ToolName, arguments))
							}
						}
					}
				}
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
	// This is an execution, not a passive observation: constructing the
	// manager here only wires the lifecycle handle — it starts no process by
	// itself. A durable-admitted AutoStart=false server must stay callable;
	// with a mere peek, a manager nobody created yet would make the admitted
	// binding permanently stale.
	local := b.handler.getLocalMCPManager()
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
		// An enabled server that does not auto-start is offline until a call
		// needs it: the inventory admitted it from the durable observation,
		// and CallToolForOwner starts the process under the same runtime gate
		// and discovery checks a synced start performs. The durable record
		// only proves the tool existed at the last observation for this exact
		// endpoint; the fresh DiscoverTools inside the call path remains the
		// final truth on whether it still does.
		if registry := b.handler.getMCPRegistry(); registry != nil {
			for _, configured := range registry.ListLocalServers() {
				if strings.TrimSpace(configured.ID) != binding.ServerID || configured.Disabled {
					continue
				}
				durableTools, ok := durableLocalMCPTools(registry, configured)
				if !ok {
					break
				}
				for _, discovered := range durableTools {
					if discovered.Name == binding.ToolName {
						return boundMCPToolContent(local.CallToolForOwner(principal.UserID, binding.ServerID, binding.ToolName, arguments))
					}
				}
				return "", fmt.Errorf("mcp_binding_stale")
			}
		}
	}
	return "", fmt.Errorf("mcp_binding_stale")
}

func boundMCPToolContent(raw string, err error) (string, error) {
	if err != nil {
		return "", err
	}
	content, err := mcputil.ToolCallContent(raw)
	if err != nil {
		return "", err
	}
	// A bound result is content, not control: the host-emitted markers a
	// vendor must not forge are escaped at the agent package (see
	// agent.SanitizeVendorToolResultText).
	return agent.SanitizeVendorToolResultText(content), nil
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
	text, err := executor.executeBoundSkill(binding.StableID, binding.Name, binding.Version, binding.ContentDigest, arguments)
	if err != nil {
		return "", err
	}
	// Skill output is content, not control — a skill that returns a forged
	// __ASK_USER__ must not pause the loop on a question the host never
	// asked, the same bound-execution rule MCP results already obey.
	return agent.SanitizeVendorToolResultText(text), nil
}
