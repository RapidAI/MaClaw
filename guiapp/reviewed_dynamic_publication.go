package guiapp

import (
	"fmt"
	"log"
	"path/filepath"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
)

// reviewedDynamicPublisher is the desktop control-plane writer. It is not an
// Agent tool. Planning and inventory only read the contracts it has already
// published from a lifecycle observation.
func (a *App) reviewedDynamicPublisher() (*agentservice.DynamicCapabilityContractPublisher, error) {
	if a == nil {
		return nil, fmt.Errorf("semantic invocation host is unavailable")
	}
	a.semanticInvocationMu.Lock()
	defer a.semanticInvocationMu.Unlock()
	if a.semanticDynamicPublisher != nil {
		return a.semanticDynamicPublisher, nil
	}
	if a.semanticDynamicContracts == nil {
		registry, err := agentservice.NewSQLiteDynamicCapabilityRegistry(filepath.Join(a.getMaclawBaseDir(), "semantic-routing", "dynamic-capability-contracts.db"))
		if err != nil {
			return nil, err
		}
		a.semanticDynamicContracts = registry
	}
	publisher, err := agentservice.NewLifecycleDynamicCapabilityContractPublisher(
		a.semanticDynamicContracts, newIMSemanticCapabilityRegistry(), nil, nil,
	)
	if err != nil {
		return nil, err
	}
	// declaredExternal is in-process bookkeeping; the contracts it projects
	// are durable. Rebuild the installed-not-reviewed listing from the
	// persisted snapshot so a restart does not silently hide installed MCP
	// providers from the management surface until their next observation.
	if err := publisher.RestoreInstalledNotReviewed(semanticDesktopContractPrincipal()); err != nil {
		log.Printf("[semantic-routing] restore installed MCP provider listing: %v", err)
	}
	a.semanticDynamicPublisher = publisher
	return publisher, nil
}

func semanticDesktopContractPrincipal() agentservice.Principal {
	return agentservice.Principal{TenantID: semanticDesktopTenantID(), UserID: desktopUserID}
}

// publishReviewedMCPServerTools records reviewed contracts for one observed
// tools/list. The digest is computed inside the publisher. A failure is
// returned to the caller so a health check can log it and still succeed.
func (a *App) publishReviewedMCPServerTools(serverID string, tools []MCPToolView) error {
	if a == nil {
		return nil
	}
	publisher, err := a.reviewedDynamicPublisher()
	if err != nil || publisher == nil {
		return err
	}
	observed := make([]agentservice.ObservedMCPTool, 0, len(tools))
	for _, tool := range tools {
		observed = append(observed, agentservice.ObservedMCPTool{
			Name:                 tool.Name,
			InputSchema:          tool.InputSchema,
			Annotations:          tool.Annotations,
			DeclaredCapabilities: tool.DeclaredCapabilities,
		})
	}
	return publisher.PublishReviewedMCPObservationForCapabilityWithOpenLookup(
		semanticDesktopContractPrincipal(), serverID, a.mcpCapabilityIdentity(serverID), observed, a.mcpOpenLookupReady,
	)
}

// mcpOpenLookupReady reports whether this server finished a successful
// tools/list observation. It gates the open read-only family only: a
// reviewed name keeps its specific contract regardless of health, because
// that contract carries its own effect and the catalog publishes the entry
// with RuntimeBlocked set when the server is not healthy.
//
// A local stdio server has no remote health cache: for it, the running
// client that produced the tool list IS the finished observation, so the
// local manager's live client count is the equivalent readiness evidence.
func (a *App) mcpOpenLookupReady(serverID string) bool {
	if a == nil {
		return false
	}
	serverID = strings.TrimSpace(serverID)
	if a.localMCPManager != nil && a.localMCPManager.IsRunning(serverID) {
		return true
	}
	if a.mcpRegistry == nil {
		return false
	}
	// A durable local observation is the restart-time equivalent of the live
	// client check above: the process can start the server on demand, and the
	// tools/list evidence survived the restart for the same endpoint.
	for _, configured := range a.mcpRegistry.ListLocalServers() {
		if strings.TrimSpace(configured.ID) != serverID || configured.Disabled {
			continue
		}
		if _, ok := durableLocalMCPTools(a.mcpRegistry, configured); ok {
			return true
		}
	}
	status, _, observed := a.mcpRegistry.remoteHealthObservation(serverID)
	if observed && mcpHealthObservationSucceeded(status) {
		return true
	}
	// Only an UNKNOWN this-process observation may fall back to durable
	// evidence. A finished failure/degraded verdict this process is newer
	// than the durable success and must keep the tool unpublished.
	if !observed && normalizeMCPHealthStatus(status) == mcpHealthStatusUnknown {
		if _, ok := durableRemoteMCPTools(a.mcpRegistry, serverID); ok {
			return true
		}
	}
	return false
}

// mcpCapabilityIdentity reads the capability ref stored on the installed
// server. Publication uses it only as a join key. A missing server, a local
// cache miss, or an empty ref yields an identity that matches no product binding.
func (a *App) mcpCapabilityIdentity(serverID string) agentservice.MCPCapabilityIdentity {
	serverID = strings.TrimSpace(serverID)
	if a == nil || serverID == "" || a.mcpRegistry == nil {
		return agentservice.MCPCapabilityIdentity{}
	}
	if server, err := a.mcpRegistry.findServer(serverID); err == nil && server != nil && server.Capability != nil {
		return agentservice.MCPCapabilityIdentity{
			GlobalKey:    strings.TrimSpace(server.Capability.GlobalKey),
			CapabilityID: strings.TrimSpace(server.Capability.CapabilityID),
		}
	}
	for _, local := range a.mcpRegistry.ListLocalServers() {
		if strings.TrimSpace(local.ID) != serverID || local.Capability == nil {
			continue
		}
		return agentservice.MCPCapabilityIdentity{
			GlobalKey:    strings.TrimSpace(local.Capability.GlobalKey),
			CapabilityID: strings.TrimSpace(local.Capability.CapabilityID),
		}
	}
	return agentservice.MCPCapabilityIdentity{}
}

func (a *App) publishReviewedSkillContracts(entries []corelib.NLSkillEntry) error {
	if a == nil {
		return nil
	}
	publisher, err := a.reviewedDynamicPublisher()
	if err != nil || publisher == nil {
		return err
	}
	return publisher.PublishReviewedSkillObservation(semanticDesktopContractPrincipal(), entries)
}

// revokeReviewedMCPServerContracts drops contracts after the server is removed
// or its endpoint/auth identity changes. A tools/list cache miss must not
// call this.
func (a *App) revokeReviewedMCPServerContracts(serverID string) {
	if a == nil || strings.TrimSpace(serverID) == "" {
		return
	}
	publisher, err := a.reviewedDynamicPublisher()
	if err != nil || publisher == nil {
		log.Printf("[semantic-routing] revoke MCP contracts for %s: %v", serverID, err)
		return
	}
	if err := publisher.RevokeObservedMCPServer(semanticDesktopContractPrincipal(), serverID); err != nil {
		log.Printf("[semantic-routing] revoke MCP contracts for %s: %v", serverID, err)
	}
}

// reconcileReviewedDynamicContracts republishes contracts from observations
// the process already holds. It does not discover, start a provider, or
// revoke a server whose tools/list has not been observed.
func (a *App) reconcileReviewedDynamicContracts() {
	if a == nil {
		return
	}
	if a.mcpRegistry != nil {
		for _, server := range a.mcpRegistry.ListServers() {
			tools, observed := a.mcpRegistry.CachedServerTools(server.ID)
			if !observed {
				// Restart path: the process never observed this server, but a
				// previous process did. A fresh durable observation for the
				// same endpoint republishes instead of leaving the tools
				// contracted-but-invisible until the next probe.
				tools, observed = durableRemoteMCPTools(a.mcpRegistry, server.ID)
			}
			if !observed {
				continue
			}
			if err := a.publishReviewedMCPServerTools(server.ID, tools); err != nil {
				log.Printf("[semantic-routing] publish reviewed MCP %s: %v", server.ID, err)
			}
		}
	}
	if a.localMCPManager != nil {
		for _, set := range a.localMCPManager.GetAllTools() {
			if err := a.publishReviewedMCPServerTools(set.ServerID, set.Tools); err != nil {
				log.Printf("[semantic-routing] publish reviewed local MCP %s: %v", set.ServerID, err)
			}
		}
	}
	if a.mcpRegistry != nil {
		running := map[string]bool{}
		if a.localMCPManager != nil {
			for _, set := range a.localMCPManager.GetAllTools() {
				running[set.ServerID] = true
			}
		}
		for _, configured := range a.mcpRegistry.ListLocalServers() {
			if running[configured.ID] || configured.Disabled {
				continue
			}
			tools, ok := durableLocalMCPTools(a.mcpRegistry, configured)
			if !ok {
				continue
			}
			if err := a.publishReviewedMCPServerTools(configured.ID, tools); err != nil {
				log.Printf("[semantic-routing] publish reviewed local MCP %s: %v", configured.ID, err)
			}
		}
	}
	if a.skillExecutor != nil {
		if err := a.publishReviewedSkillContracts(a.skillExecutor.loadSkills()); err != nil {
			log.Printf("[semantic-routing] publish reviewed skills: %v", err)
		}
	}
}

func (r *MCPRegistry) publishReviewedMCPContracts(serverID string, tools []MCPToolView) {
	if r == nil {
		return
	}
	// A finished tools/list is durable evidence before it is a contract: the
	// observation must outlive this process or every restart hides installed
	// tools until the next probe. Record it first; the contract publish below
	// then has an endpoint-bound record to lean on.
	r.recordRemoteToolObservation(serverID, tools)
	if r.app == nil {
		return
	}
	if err := r.app.publishReviewedMCPServerTools(serverID, tools); err != nil {
		log.Printf("[MCPRegistry] reviewed capability publication for %s: %v", serverID, err)
	}
}

func (m *LocalMCPManager) publishReviewedServerTools(serverID string, tools []MCPToolView) {
	if m == nil {
		return
	}
	m.recordLocalToolObservation(serverID, tools)
	if m.registry == nil || m.registry.app == nil {
		return
	}
	if err := m.registry.app.publishReviewedMCPServerTools(serverID, tools); err != nil {
		log.Printf("[LocalMCP] reviewed capability publication for %s: %v", serverID, err)
	}
}

func (e *SkillExecutor) publishReviewedSkillContracts() {
	if e == nil || e.app == nil {
		return
	}
	if err := e.app.publishReviewedSkillContracts(e.loadSkills()); err != nil {
		log.Printf("[skill-load] reviewed capability publication: %v", err)
	}
}

// InstalledNotReviewedMCPProviderView is the management-surface projection of
// one installed MCP server whose declared capabilities are still governed by
// the host default effect ceiling — installed, contract-published, but never
// host-reviewed. It must be visible rather than silently hidden.
type InstalledNotReviewedMCPProviderView struct {
	ServerID             string   `json:"server_id"`
	ServerName           string   `json:"server_name,omitempty"`
	DeclaredCapabilities []string `json:"declared_capabilities"`
	Namespace            string   `json:"namespace"`
	CeilingReviewed      bool     `json:"ceiling_reviewed"`
}

// InstalledNotReviewedMCPView is the Wails payload for the unreviewed
// provider listing plus the discovery budget the surface should grant it.
type InstalledNotReviewedMCPView struct {
	Providers     []InstalledNotReviewedMCPProviderView `json:"providers"`
	PetitionSlots int                                   `json:"petition_slots"`
	SearchPasses  int                                   `json:"search_passes"`
}

// ListInstalledNotReviewedMCPProviders exposes the publisher's
// installed-but-unreviewed provider list to the MCP management UI. Empty
// result is a valid answer; an unavailable publisher fails closed to empty
// rather than inventing providers.
func (a *App) ListInstalledNotReviewedMCPProviders() InstalledNotReviewedMCPView {
	out := InstalledNotReviewedMCPView{Providers: []InstalledNotReviewedMCPProviderView{}}
	publisher, err := a.reviewedDynamicPublisher()
	if err != nil || publisher == nil {
		return out
	}
	names := map[string]string{}
	if a.mcpRegistry != nil {
		for _, server := range a.mcpRegistry.ListServers() {
			if name := strings.TrimSpace(server.Name); name != "" {
				names[server.ID] = name
			}
		}
		for _, server := range a.mcpRegistry.ListLocalServers() {
			if name := strings.TrimSpace(server.Name); name != "" {
				names[server.ID] = name
			}
		}
	}
	for _, provider := range publisher.ListInstalledNotReviewedProviders() {
		caps := make([]string, 0, len(provider.DeclaredCapabilities))
		for _, capID := range provider.DeclaredCapabilities {
			caps = append(caps, string(capID))
		}
		out.Providers = append(out.Providers, InstalledNotReviewedMCPProviderView{
			ServerID:             provider.ServerID,
			ServerName:           names[provider.ServerID],
			DeclaredCapabilities: caps,
			Namespace:            provider.Namespace,
			CeilingReviewed:      provider.CeilingReviewed,
		})
	}
	budget := publisher.RecommendedDiscoveryBudgetForInstalled()
	out.PetitionSlots = budget.PetitionSlots
	out.SearchPasses = budget.SearchPasses
	return out
}
