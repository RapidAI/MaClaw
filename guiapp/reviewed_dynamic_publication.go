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
			Name:        tool.Name,
			InputSchema: tool.InputSchema,
		})
	}
	return publisher.PublishReviewedMCPObservationForCapability(semanticDesktopContractPrincipal(), serverID, a.mcpCapabilityIdentity(serverID), observed)
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
	if a.skillExecutor != nil {
		if err := a.publishReviewedSkillContracts(a.skillExecutor.loadSkills()); err != nil {
			log.Printf("[semantic-routing] publish reviewed skills: %v", err)
		}
	}
}

func (r *MCPRegistry) publishReviewedMCPContracts(serverID string, tools []MCPToolView) {
	if r == nil || r.app == nil {
		return
	}
	if err := r.app.publishReviewedMCPServerTools(serverID, tools); err != nil {
		log.Printf("[MCPRegistry] reviewed capability publication for %s: %v", serverID, err)
	}
}

func (m *LocalMCPManager) publishReviewedServerTools(serverID string, tools []MCPToolView) {
	if m == nil || m.registry == nil || m.registry.app == nil {
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
