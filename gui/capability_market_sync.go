package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/remote"
	"github.com/RapidAI/CodeClaw/corelib/security"
	cskill "github.com/RapidAI/CodeClaw/corelib/skill"
)

const capabilityManagedSyncMinRetry = 5 * time.Minute
const capabilityManagedSyncOwnerID = "system:scheduler:background:capability-market-sync"

type CapabilitySyncStatus struct {
	ManagedChecked    int      `json:"managed_checked"`
	ManagedInstalled  int      `json:"managed_installed"`
	Updated           int      `json:"updated"`
	InventoryReported int      `json:"inventory_reported"`
	RecommendedCount  int      `json:"recommended_count"`
	NeedsUserConfig   []string `json:"needs_user_config,omitempty"`
	Errors            []string `json:"errors,omitempty"`
}

func (a *App) TriggerHubManagedCapabilitySync(reason string) {
	if err := a.ensureWorkflowAllowsRemoteToolCallForOwner(capabilityManagedSyncOwnerID, "manage_skill", map[string]interface{}{"action": "sync_capabilities", "reason": reason}); err != nil {
		log.Printf("[capability-market] managed sync blocked by workflow policy reason=%s err=%v", reason, err)
		return
	}
	now := time.Now()
	if !isCapabilitySyncImmediateReason(reason) {
		if next, ok := a.capabilitySyncNextAttempt.Load().(time.Time); ok && now.Before(next) {
			return
		}
	}
	if a.capabilityMarketplaceUnsupportedForCurrentHub() {
		return
	}
	if a.capabilitySyncRunning.Swap(true) {
		return
	}
	go func() {
		defer a.capabilitySyncRunning.Store(false)

		// Re-check inside goroutine: another goroutine may have cached a 404
		// between the fast-path check above and entering this worker.
		if a.capabilityMarketplaceUnsupportedForCurrentHub() {
			return
		}

		status := a.SyncHubManagedCapabilities()
		if len(status.Errors) > 0 {
			// Detect "hub doesn't support marketplace" (404 on the endpoint).
			// Cache the result keyed by hub URL; this is a permanent condition
			// for a given hub version at a given URL.
			//
			// Two detection paths:
			// 1. listManagedDeployments itself returns 404 (original check)
			// 2. listManagedDeployments succeeds but ALL subsequent API calls
			//    (getCapability, inventory, updates) return 404 — the hub has
			//    the deployments endpoint but not the detail/update endpoints.
			shouldDisable := false
			for _, e := range status.Errors {
				if isCapabilityMarketplaceUnsupportedError(e) {
					shouldDisable = true
					break
				}
			}
			if !shouldDisable && allErrorsAreMarketplace404(status.Errors, status) {
				shouldDisable = true
			}
			if shouldDisable {
				cfg, _ := a.LoadConfig()
				probeURL := strings.TrimRight(strings.TrimSpace(cfg.RemoteHubURL), "/")
				a.hubMarketplace404URL.Store(probeURL)
				a.hubMarketplaceUnsupported.Store(true)
				log.Printf("[capability-market] hub %s does not support marketplace API (404), disabling sync until hub URL changes", probeURL)
				return
			}
			nextAttempt := time.Now().Add(capabilityManagedSyncRetryDelay(status.Errors))
			a.capabilitySyncNextAttempt.Store(nextAttempt)
			log.Printf("[capability-market] managed sync reason=%s errors=%v next_retry=%s", reason, status.Errors, nextAttempt.Format(time.RFC3339))
			return
		}
		a.capabilitySyncNextAttempt.Store(time.Time{})
		log.Printf("[capability-market] managed sync reason=%s checked=%d installed=%d needs_config=%d", reason, status.ManagedChecked, status.ManagedInstalled, len(status.NeedsUserConfig))
	}()
}

func (a *App) capabilityMarketplaceUnsupportedForCurrentHub() bool {
	if !a.hubMarketplaceUnsupported.Load() {
		return false
	}
	cfg, err := a.LoadConfig()
	if err != nil {
		return true
	}
	currentURL := strings.TrimRight(strings.TrimSpace(cfg.RemoteHubURL), "/")
	if cachedURL, _ := a.hubMarketplace404URL.Load().(string); cachedURL == currentURL {
		return true
	}
	a.hubMarketplaceUnsupported.Store(false)
	a.capabilitySyncNextAttempt.Store(time.Time{})
	return false
}

func isCapabilitySyncImmediateReason(reason string) bool {
	switch strings.TrimSpace(strings.ToLower(reason)) {
	case "hub-connect", "hub-config-update", "manual", "user", "install", "startup":
		return true
	default:
		return false
	}
}

func capabilityManagedSyncRetryDelay(errs []string) time.Duration {
	// Escalate delay when all errors are 404 — the hub likely doesn't support
	// the marketplace detail API and the condition won't resolve on its own.
	if errorsAllContain404(errs) {
		return 30 * time.Minute
	}
	return capabilityManagedSyncMinRetry
}

func isCapabilityMarketplaceUnsupportedError(errText string) bool {
	errText = strings.TrimSpace(errText)
	lower := strings.ToLower(errText)
	return strings.Contains(lower, "managed deployments") && strings.Contains(lower, "status=404")
}

// errorsAllContain404 returns true if errs is non-empty and every entry
// contains "status=404". Shared by allErrorsAreMarketplace404 and
// capabilityManagedSyncRetryDelay.
func errorsAllContain404(errs []string) bool {
	if len(errs) == 0 {
		return false
	}
	for _, e := range errs {
		if !strings.Contains(strings.ToLower(strings.TrimSpace(e)), "status=404") {
			return false
		}
	}
	return true
}

// allErrorsAreMarketplace404 checks whether every error in the list is a
// marketplace 404 AND there were no successful operations (no installs, no
// updates, no inventory reports). This indicates the hub's capability detail
// endpoints are globally unavailable — not just a single decommissioned
// capability.
//
// Excluded: inventory report 404s are expected on some hub versions and should
// not trigger the circuit breaker by themselves.
func allErrorsAreMarketplace404(errs []string, status CapabilitySyncStatus) bool {
	if len(errs) == 0 {
		return false
	}
	// If any operation succeeded, the API is partially working — individual
	// capability 404s are legitimate "not found" responses, not API-level
	// incompatibility.
	if status.ManagedInstalled > 0 || status.Updated > 0 || status.InventoryReported > 0 || status.RecommendedCount > 0 {
		return false
	}
	// Filter out inventory report 404s — those are expected on some hub
	// versions and shouldn't contribute to the "all 404" signal.
	relevant := 0
	for _, e := range errs {
		lower := strings.ToLower(strings.TrimSpace(e))
		if strings.HasPrefix(lower, "inventory report failed:") {
			continue // expected 404, ignore
		}
		relevant++
		if !strings.Contains(lower, "status=404") {
			return false
		}
	}
	return relevant > 0
}

func (a *App) SyncHubManagedCapabilities() CapabilitySyncStatus {
	if err := a.ensureWorkflowAllowsRemoteToolCallForOwner(capabilityManagedSyncOwnerID, "manage_skill", map[string]interface{}{"action": "sync_capabilities"}); err != nil {
		return CapabilitySyncStatus{Errors: []string{err.Error()}}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	return a.syncHubManagedCapabilities(ctx)
}

// isCapabilityManagedDeployment checks if a capability ID corresponds to a
// managed (forced) deployment that should not be deletable by the user.
// It reads from the in-memory cache populated by syncHubManagedCapabilities.
func (a *App) isCapabilityManagedDeployment(capabilityID string) bool {
	if capabilityID == "" {
		return false
	}
	_, ok := a.managedDeploymentIDs.Load(capabilityID)
	return ok
}

func (a *App) InstallHubCapability(capabilityRef string) CapabilitySyncStatus {
	if err := a.ensureWorkflowAllowsRemoteToolCall("manage_skill", map[string]interface{}{"action": "install", "source": "capability_market", "capability_ref": capabilityRef}); err != nil {
		return CapabilitySyncStatus{ManagedChecked: 1, Errors: []string{err.Error()}}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	status := CapabilitySyncStatus{ManagedChecked: 1}
	c, err := a.newCapabilityMarketClientFromConfig()
	if err != nil {
		status.Errors = append(status.Errors, err.Error())
		return status
	}
	item, err := c.getCapability(ctx, strings.TrimSpace(capabilityRef))
	if err != nil {
		status.Errors = append(status.Errors, err.Error())
		return status
	}
	if item.CapabilityType == corelib.CapabilityTypeSkill {
		installed, err := a.ensureHubSkillInstalled(ctx, *item, item.CurrentVersionKey)
		if err != nil {
			status.Errors = append(status.Errors, err.Error())
			return status
		}
		present := a.isHubSkillCapabilityInstalled(*item)
		if installed {
			status.ManagedInstalled = 1
		}
		if !present {
			status.Errors = append(status.Errors, fmt.Sprintf("capability %s skill was not installed", item.ID))
		}
		if err := a.reportHubCapabilityInventoryItem(ctx, c, *item, item.CurrentVersionKey, skillInstallStatus(present), present); err != nil {
			status.Errors = append(status.Errors, err.Error())
		} else {
			status.InventoryReported = 1
		}
		a.emitEvent("hub-capability-installed", status)
		return status
	}
	if item.CapabilityType != corelib.CapabilityTypeMCP {
		status.Errors = append(status.Errors, fmt.Sprintf("capability %s type %s is not installable yet", item.ID, item.CapabilityType))
		return status
	}
	installed, needsConfig, err := a.ensureHubMCPInstalled(ctx, c, *item, item.CurrentVersionKey)
	if err != nil {
		status.Errors = append(status.Errors, err.Error())
		return status
	}
	if installed {
		status.ManagedInstalled = 1
	}
	if needsConfig {
		status.NeedsUserConfig = append(status.NeedsUserConfig, item.ID)
	}
	if err := a.reportHubCapabilityInventoryItem(ctx, c, *item, item.CurrentVersionKey, mcpInstallStatus(needsConfig), !needsConfig); err != nil {
		status.Errors = append(status.Errors, err.Error())
	} else {
		status.InventoryReported = 1
	}
	a.emitEvent("hub-capability-installed", status)
	return status
}

func (a *App) RequestHubCapabilityInstallIntent(intent HubCapabilityInstallIntent) (*HubCapabilityInstallIntentResult, error) {
	c, err := a.newCapabilityMarketClientFromConfig()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	return c.createInstallIntent(ctx, intent)
}

func (a *App) ListHubCapabilities(capabilityType string, query string) ([]HubCapabilitySummary, error) {
	cfg, err := a.LoadConfig()
	if err != nil {
		return nil, err
	}
	c, err := newCapabilityMarketClient(cfg)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	capabilityType = strings.TrimSpace(capabilityType)
	items, err := c.listCapabilities(ctx, capabilityType, query)
	if err != nil {
		return nil, err
	}
	if strings.EqualFold(capabilityType, corelib.CapabilityTypeMCP) && shouldMergeHubCenterMarketplace(cfg.CapabilityMarketPolicy) {
		external, extErr := a.listHubCenterMCPMarketplace(ctx, cfg, query)
		if extErr == nil {
			items = mergeCapabilitySummaries(items, external)
		} else {
			log.Printf("[capability-market] hubcenter MCP marketplace search skipped: %v", extErr)
		}
	}
	return items, nil
}
func shouldMergeHubCenterMarketplace(policy corelib.CapabilityMarketPolicy) bool {
	policy = policy.WithDefaults()
	if policy.EffectiveEnterpriseOnlySearch() {
		return false
	}
	return policy.ViewMode == "" || policy.ViewMode == "merged" || policy.ViewMode == "enterprise_first"
}

func (a *App) listHubCenterMCPMarketplace(ctx context.Context, cfg corelib.AppConfig, query string) ([]HubCapabilitySummary, error) {
	bases := []string{}
	add := func(value string) {
		value = strings.TrimRight(strings.TrimSpace(value), "/")
		if value == "" {
			return
		}
		for _, existing := range bases {
			if existing == value {
				return
			}
		}
		bases = append(bases, value)
	}
	add(cfg.RemoteHubCenterURL)
	for _, value := range cfg.RemoteHubCenterURLs {
		add(value)
	}
	if len(bases) == 0 {
		return nil, nil
	}
	client := &http.Client{Timeout: 15 * time.Second}
	var lastErr error
	for _, base := range bases {
		items, err := listHubCenterMCPCapabilities(ctx, client, base, query)
		if err == nil {
			return items, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

func mergeCapabilitySummaries(primary []HubCapabilitySummary, external []HubCapabilitySummary) []HubCapabilitySummary {
	if len(external) == 0 {
		return primary
	}
	seen := map[string]bool{}
	for _, item := range primary {
		for _, key := range []string{item.ID, item.CapabilityID, item.GlobalKey} {
			if strings.TrimSpace(key) != "" {
				seen[strings.ToLower(strings.TrimSpace(key))] = true
			}
		}
	}
	out := append([]HubCapabilitySummary{}, primary...)
	for _, item := range external {
		key := strings.ToLower(strings.TrimSpace(firstCapabilityNonEmpty(item.GlobalKey, item.CapabilityID, item.ID)))
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, item)
	}
	return out
}
func (a *App) GetHubCapability(capabilityRef string) (*HubCapabilitySummary, error) {
	c, err := a.newCapabilityMarketClientFromConfig()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	return c.getCapability(ctx, strings.TrimSpace(capabilityRef))
}

func (a *App) GetHubRecommendedCapabilities() ([]HubCapabilityRecommendation, error) {
	c, err := a.newCapabilityMarketClientFromConfig()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	return c.listRecommendations(ctx)
}

func (a *App) GetHubMCPSecretRequirements(capabilityRef string, versionKey string) ([]HubMCPSecretRequirement, error) {
	c, err := a.newCapabilityMarketClientFromConfig()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	return c.listMCPSecretRequirements(ctx, capabilityRef, versionKey)
}

func (a *App) GetHubMCPSecretBindings(mcpServerID string) ([]HubMCPSecretBinding, error) {
	c, err := a.newCapabilityMarketClientFromConfig()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	return c.listMCPSecretBindings(ctx, mcpServerID)
}
func (a *App) SaveHubMCPSecretBinding(binding HubMCPSecretBinding) error {
	c, err := a.newCapabilityMarketClientFromConfig()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	return c.saveMCPSecretBinding(ctx, binding)
}

func (a *App) GetHubMCPHubSecrets(mcpServerID string) ([]HubMCPHubSecret, error) {
	c, err := a.newCapabilityMarketClientFromConfig()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	return c.listMCPHubSecrets(ctx, mcpServerID)
}

func (a *App) SaveHubMCPHubSecret(secret HubMCPHubSecretInput) (*HubMCPHubSecret, error) {
	c, err := a.newCapabilityMarketClientFromConfig()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	return c.saveMCPHubSecret(ctx, secret)
}
func (a *App) syncHubManagedCapabilities(ctx context.Context) CapabilitySyncStatus {
	status := CapabilitySyncStatus{}
	c, err := a.newCapabilityMarketClientFromConfig()
	if err != nil {
		status.Errors = append(status.Errors, err.Error())
		return a.finishHubManagedCapabilitySync(status)
	}
	deployments, err := c.listManagedDeployments(ctx)
	if err != nil {
		status.Errors = append(status.Errors, fmt.Sprintf("managed deployments request failed: %v", err))
		return a.finishHubManagedCapabilitySync(status)
	}
	// Cache managed deployment IDs for isManagedCapability lookups.
	// Clear old entries and repopulate from the fresh list.
	a.managedDeploymentIDs.Range(func(key, _ any) bool {
		a.managedDeploymentIDs.Delete(key)
		return true
	})
	for _, dep := range deployments {
		if shouldTrackManagedCapabilityDeployment(dep) {
			a.managedDeploymentIDs.Store(strings.TrimSpace(dep.CapabilityRef), true)
		}
	}
	recommendations, err := c.listRecommendations(ctx)
	if err == nil {
		status.RecommendedCount = len(recommendations)
	}
	for _, dep := range deployments {
		status.ManagedChecked++
		capabilityRef := strings.TrimSpace(dep.CapabilityRef)
		if capabilityRef == "" {
			continue
		}
		if normalizeManagedCapabilityPolicy(dep.DeploymentPolicy) != "required" {
			continue
		}
		item, err := c.getCapability(ctx, capabilityRef)
		if err != nil {
			status.Errors = append(status.Errors, err.Error())
			continue
		}
		if item.CapabilityType == corelib.CapabilityTypeSkill {
			installed, err := a.ensureHubSkillInstalled(ctx, *item, dep.CapabilityVersionKey)
			if err != nil {
				status.Errors = append(status.Errors, err.Error())
				continue
			}
			if installed {
				status.ManagedInstalled++
			} else if !a.isHubSkillCapabilityInstalled(*item) {
				status.Errors = append(status.Errors, fmt.Sprintf("managed capability %s skill was not installed", item.ID))
			}
			continue
		}
		if item.CapabilityType == corelib.CapabilityTypeMCP {
			installed, needsConfig, err := a.ensureHubMCPInstalled(ctx, c, *item, dep.CapabilityVersionKey)
			if err != nil {
				status.Errors = append(status.Errors, err.Error())
				continue
			}
			if installed {
				status.ManagedInstalled++
			}
			if needsConfig {
				status.NeedsUserConfig = append(status.NeedsUserConfig, item.ID)
			}
			continue
		}
		status.Errors = append(status.Errors, fmt.Sprintf("managed capability %s type %s is not installable yet", item.ID, item.CapabilityType))
	}
	updateStatus := a.syncHubInstalledCapabilityUpdates(ctx, c)
	status.Updated += updateStatus.Updated
	status.NeedsUserConfig = append(status.NeedsUserConfig, updateStatus.NeedsUserConfig...)
	status.Errors = append(status.Errors, updateStatus.Errors...)
	if reported, err := a.reportHubCapabilityInventorySnapshot(ctx, c); err != nil {
		status.Errors = append(status.Errors, fmt.Sprintf("inventory report failed: %v", err))
	} else {
		status.InventoryReported = reported
	}
	return a.finishHubManagedCapabilitySync(status)
}

func (a *App) finishHubManagedCapabilitySync(status CapabilitySyncStatus) CapabilitySyncStatus {
	a.emitHubManagedCapabilitySyncEvent(status)
	return status
}

func (a *App) emitHubManagedCapabilitySyncEvent(status CapabilitySyncStatus) {
	if shouldEmitHubManagedCapabilitySyncEvent(status) {
		a.emitEvent("hub-managed-capabilities-synced", status)
	}
}

func shouldEmitHubManagedCapabilitySyncEvent(status CapabilitySyncStatus) bool {
	return status.ManagedInstalled > 0 || status.Updated > 0 || len(status.NeedsUserConfig) > 0 || len(status.Errors) > 0
}

func (a *App) reportHubCapabilityInventoryItem(ctx context.Context, client *capabilityMarketClient, item HubCapabilitySummary, versionKey string, installStatus string, installed bool) error {
	if client == nil || strings.TrimSpace(item.ID) == "" {
		return nil
	}
	return client.reportInventory(ctx, HubCapabilityInventoryReport{Items: []HubCapabilityInventoryItem{{
		CapabilityRef:        item.ID,
		CapabilityVersionKey: firstCapabilityNonEmpty(versionKey, item.CurrentVersionKey),
		CapabilityType:       item.CapabilityType,
		InstallStatus:        firstCapabilityNonEmpty(installStatus, "installed"),
		Installed:            installed,
		LastSeenAt:           time.Now().UTC().Format(time.RFC3339),
	}}})
}

func (a *App) reportHubCapabilityInventorySnapshot(ctx context.Context, client *capabilityMarketClient) (int, error) {
	if client == nil {
		return 0, nil
	}
	items, err := a.collectHubCapabilityInventorySnapshot(ctx, client)
	if err != nil {
		return 0, err
	}
	if err := client.reportInventory(ctx, HubCapabilityInventoryReport{Items: items, FullSnapshot: true}); err != nil {
		return 0, err
	}
	return len(items), nil
}

func (a *App) collectHubCapabilityInventorySnapshot(ctx context.Context, client *capabilityMarketClient) ([]HubCapabilityInventoryItem, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	items := []HubCapabilityInventoryItem{}
	seen := map[string]bool{}
	add := func(capabilityRef, versionKey, capabilityType, installStatus string, installed bool, metadata map[string]any) {
		capabilityRef = strings.TrimSpace(capabilityRef)
		if capabilityRef == "" || seen[capabilityRef] {
			return
		}
		seen[capabilityRef] = true
		items = append(items, HubCapabilityInventoryItem{CapabilityRef: capabilityRef, CapabilityVersionKey: strings.TrimSpace(versionKey), CapabilityType: capabilityType, InstallStatus: firstCapabilityNonEmpty(installStatus, "installed"), Installed: installed, Metadata: metadata, LastSeenAt: now})
	}
	cfg, err := a.LoadConfig()
	if err != nil {
		return nil, err
	}
	for _, server := range cfg.MCPServers {
		if server.Capability == nil {
			continue
		}
		installStatus := "installed"
		installed := true
		if hubMCPNeedsUserConfig(ctx, client, server) {
			installStatus = "needs_config"
			installed = false
		}
		add(server.Capability.CapabilityID, server.Capability.VersionKey, corelib.CapabilityTypeMCP, installStatus, installed, map[string]any{"name": server.Name, "server_id": server.ID, "transport": "remote"})
	}
	for _, server := range cfg.LocalMCPServers {
		if server.Capability == nil {
			continue
		}
		installed := !server.Disabled
		status := "installed"
		if !installed {
			status = "disabled"
		}
		add(server.Capability.CapabilityID, server.Capability.VersionKey, corelib.CapabilityTypeMCP, status, installed, map[string]any{"name": server.Name, "server_id": server.ID, "transport": "stdio"})
	}
	if a.skillExecutor != nil {
		for _, skill := range a.skillExecutor.loadSkills() {
			if skill.Capability == nil {
				continue
			}
			installed := !strings.EqualFold(strings.TrimSpace(skill.Status), "disabled")
			status := "installed"
			if !installed {
				status = "disabled"
			}
			add(skill.Capability.CapabilityID, skill.Capability.VersionKey, corelib.CapabilityTypeSkill, status, installed, map[string]any{"name": skill.Name, "hub_skill_id": skill.HubSkillID})
		}
	}
	return items, nil
}

func hubMCPNeedsUserConfig(ctx context.Context, client *capabilityMarketClient, server corelib.MCPServerEntry) bool {
	if client == nil || server.Capability == nil || strings.TrimSpace(server.Capability.CapabilityID) == "" {
		return false
	}
	requirements, err := client.listMCPSecretRequirements(ctx, server.Capability.CapabilityID, server.Capability.VersionKey)
	if err != nil {
		log.Printf("[capability-market] inventory secret requirement check failed for %s: %v", server.Capability.CapabilityID, err)
		return false
	}
	return mcpSecretRequirementsNeedUserConfig(ctx, client, server, requirements)
}

func mcpInstallStatus(needsConfig bool) string {
	if needsConfig {
		return "needs_config"
	}
	return "installed"
}

func normalizeManagedCapabilityPolicy(policy string) string {
	policy = strings.TrimSpace(strings.ToLower(policy))
	if policy == "blocked" || policy == "recommended" {
		return policy
	}
	return "required"
}

func shouldTrackManagedCapabilityDeployment(dep HubCapabilityDeployment) bool {
	return strings.TrimSpace(dep.CapabilityRef) != "" && dep.ReinstallIfRemoved && normalizeManagedCapabilityPolicy(dep.DeploymentPolicy) == "required"
}

func (a *App) syncHubInstalledCapabilityUpdates(ctx context.Context, client *capabilityMarketClient) CapabilitySyncStatus {
	status := CapabilitySyncStatus{}
	cfg, err := a.LoadConfig()
	if err != nil {
		status.Errors = append(status.Errors, err.Error())
		return status
	}
	seen := map[string]bool{}
	for _, server := range cfg.MCPServers {
		if server.Capability == nil || strings.TrimSpace(server.Capability.CapabilityID) == "" {
			continue
		}
		capabilityRef := strings.TrimSpace(server.Capability.CapabilityID)
		if seen[capabilityRef] {
			continue
		}
		seen[capabilityRef] = true
		item, err := client.getCapability(ctx, capabilityRef)
		if err != nil {
			status.Errors = append(status.Errors, err.Error())
			continue
		}
		if item.CapabilityType != corelib.CapabilityTypeMCP || strings.TrimSpace(item.CurrentVersionKey) == "" || strings.TrimSpace(server.Capability.VersionKey) == strings.TrimSpace(item.CurrentVersionKey) {
			continue
		}
		pricing := capabilityPricingModeFromMetadata(capabilityMetadataMap(item.MetadataJSON))
		decision := corelib.DecideCapabilityUpdate(corelib.CapabilityUpdateDecisionInput{
			Policy:  cfg.CapabilityMarketPolicy,
			Source:  normalizeCapabilityUpdateSource(firstCapabilityNonEmpty(item.Source, server.Capability.Source)),
			Pricing: pricing,
		})
		if !decision.AutoUpdate {
			status.Errors = append(status.Errors, fmt.Sprintf("capability %s has update %s but policy %s requires approval", item.ID, item.CurrentVersionKey, decision.Policy))
			continue
		}
		installed, needsConfig, err := a.ensureHubMCPInstalled(ctx, client, *item, item.CurrentVersionKey)
		if err != nil {
			status.Errors = append(status.Errors, err.Error())
			continue
		}
		if installed {
			status.Updated++
		}
		if needsConfig {
			status.NeedsUserConfig = append(status.NeedsUserConfig, item.ID)
		}
	}
	// Also check local (Stdio) MCP servers for updates.
	for _, localServer := range cfg.LocalMCPServers {
		if localServer.Capability == nil || strings.TrimSpace(localServer.Capability.CapabilityID) == "" {
			continue
		}
		capabilityRef := strings.TrimSpace(localServer.Capability.CapabilityID)
		if seen[capabilityRef] {
			continue
		}
		seen[capabilityRef] = true
		item, err := client.getCapability(ctx, capabilityRef)
		if err != nil {
			status.Errors = append(status.Errors, err.Error())
			continue
		}
		if item.CapabilityType != corelib.CapabilityTypeMCP || strings.TrimSpace(item.CurrentVersionKey) == "" || strings.TrimSpace(localServer.Capability.VersionKey) == strings.TrimSpace(item.CurrentVersionKey) {
			continue
		}
		pricing := capabilityPricingModeFromMetadata(capabilityMetadataMap(item.MetadataJSON))
		decision := corelib.DecideCapabilityUpdate(corelib.CapabilityUpdateDecisionInput{
			Policy:  cfg.CapabilityMarketPolicy,
			Source:  normalizeCapabilityUpdateSource(firstCapabilityNonEmpty(item.Source, localServer.Capability.Source)),
			Pricing: pricing,
		})
		if !decision.AutoUpdate {
			status.Errors = append(status.Errors, fmt.Sprintf("capability %s has update %s but policy %s requires approval", item.ID, item.CurrentVersionKey, decision.Policy))
			continue
		}
		installed, _, err := a.ensureHubMCPInstalled(ctx, client, *item, item.CurrentVersionKey)
		if err != nil {
			status.Errors = append(status.Errors, err.Error())
			continue
		}
		if installed {
			status.Updated++
		}
	}
	if a.skillExecutor != nil {
		for _, skill := range a.skillExecutor.loadSkills() {
			if skill.Capability == nil || strings.TrimSpace(skill.Capability.CapabilityID) == "" {
				continue
			}
			capabilityRef := strings.TrimSpace(skill.Capability.CapabilityID)
			if seen[capabilityRef] {
				continue
			}
			seen[capabilityRef] = true
			item, err := client.getCapability(ctx, capabilityRef)
			if err != nil {
				status.Errors = append(status.Errors, err.Error())
				continue
			}
			if item.CapabilityType != corelib.CapabilityTypeSkill || strings.TrimSpace(item.CurrentVersionKey) == "" || strings.TrimSpace(skill.Capability.VersionKey) == strings.TrimSpace(item.CurrentVersionKey) {
				continue
			}
			metadata := capabilityMetadataMap(item.MetadataJSON)
			pricing := capabilityPricingModeFromMetadata(metadata)
			decision := corelib.DecideCapabilityUpdate(corelib.CapabilityUpdateDecisionInput{
				Policy:  cfg.CapabilityMarketPolicy,
				Source:  normalizeCapabilityUpdateSource(firstCapabilityNonEmpty(item.Source, skill.Capability.Source)),
				Pricing: pricing,
			})
			if !decision.AutoUpdate {
				status.Errors = append(status.Errors, fmt.Sprintf("capability %s has update %s but policy %s requires approval", item.ID, item.CurrentVersionKey, decision.Policy))
				continue
			}
			installed, err := a.ensureHubSkillInstalled(ctx, *item, item.CurrentVersionKey)
			if err != nil {
				status.Errors = append(status.Errors, err.Error())
				continue
			}
			if installed {
				status.Updated++
			}
		}
	}
	return status
}

func normalizeCapabilityUpdateSource(source string) string {
	switch strings.TrimSpace(strings.ToLower(source)) {
	case "hub", "enterprise", "enterprise_hub":
		return corelib.CapabilitySourceEnterpriseHub
	case "hubcenter", "hub_center":
		return corelib.CapabilitySourceHubCenter
	default:
		return strings.TrimSpace(strings.ToLower(source))
	}
}

func (a *App) recordMarketplaceMCPSecretBinding(server corelib.MCPServerEntry) {
	if server.Capability == nil || strings.TrimSpace(server.AuthSecret) == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, err := a.newCapabilityMarketClientFromConfig()
	if err != nil {
		return
	}
	requirements, err := c.listMCPSecretRequirements(ctx, server.Capability.CapabilityID, server.Capability.VersionKey)
	if err != nil || len(requirements) == 0 {
		return
	}
	requirementName := strings.TrimSpace(requirements[0].Name)
	for _, req := range requirements {
		if req.Required && strings.TrimSpace(req.Name) != "" {
			requirementName = strings.TrimSpace(req.Name)
			break
		}
	}
	if requirementName == "" {
		return
	}
	if err := c.saveMCPSecretBinding(ctx, HubMCPSecretBinding{
		MCPServerID:     server.ID,
		RequirementName: requirementName,
		Storage:         "local",
		LocalSecretRef:  "mcp:" + server.ID + ":auth_secret",
		Status:          "configured",
	}); err != nil {
		log.Printf("[capability-market] record MCP secret binding failed for %s: %v", server.ID, err)
	}
}

func (a *App) newCapabilityMarketClientFromConfig() (*capabilityMarketClient, error) {
	cfg, err := a.LoadConfig()
	if err != nil {
		return nil, err
	}
	return newCapabilityMarketClient(cfg)
}

func (a *App) ensureHubSkillInstalled(ctx context.Context, item HubCapabilitySummary, versionKey string) (bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	metadata := capabilityMetadataMap(item.MetadataJSON)
	originSource := normalizeCapabilityUpdateSource(firstCapabilityNonEmpty(stringFromMap(metadata, "origin_source"), stringFromMap(metadata, "source")))
	if originSource == corelib.CapabilitySourceClawHub || originSource == corelib.CapabilitySourceGitHub {
		return a.installManagedExternalSkill(ctx, item, metadata, versionKey, originSource)
	}
	skillID := firstCapabilityNonEmpty(stringFromMap(metadata, "skill_id"), stringFromMap(metadata, "hub_skill_id"), item.CapabilityID, item.ID)
	hubURL := firstCapabilityNonEmpty(stringFromMap(metadata, "hub_url"), stringFromMap(metadata, "skill_hub_url"), stringFromMap(metadata, "download_hub_url"))
	if hubURL == "" {
		cfg, err := a.LoadConfig()
		if err == nil {
			hubURL = firstCapabilityNonEmpty(cfg.RemoteHubURL, cfg.SkillMarketBaseURL(remote.DefaultRemoteHubCenterURL))
		}
	}
	if skillID == "" || hubURL == "" {
		return false, fmt.Errorf("managed skill %s is missing skill_id or hub_url metadata", item.ID)
	}
	return a.installManagedHubSkill(ctx, skillID, hubURL, item.ID, firstCapabilityNonEmpty(versionKey, item.CurrentVersionKey), item.Source, item.GlobalKey)
}

func (a *App) isHubSkillCapabilityInstalled(item HubCapabilitySummary) bool {
	if a == nil {
		return false
	}
	metadata := capabilityMetadataMap(item.MetadataJSON)
	skillID := firstCapabilityNonEmpty(stringFromMap(metadata, "skill_id"), stringFromMap(metadata, "hub_skill_id"), item.CapabilityID, item.ID)
	return a.findManagedCapabilitySkill(item.ID, skillID, "") != nil
}

func skillInstallStatus(installed bool) string {
	if installed {
		return "installed"
	}
	return "missing"
}

func (a *App) installManagedExternalSkill(ctx context.Context, item HubCapabilitySummary, metadata map[string]any, versionKey string, originSource string) (bool, error) {
	if a.skillExecutor == nil {
		return false, fmt.Errorf("skill executor not initialized")
	}
	skillID := firstCapabilityNonEmpty(stringFromMap(metadata, "skill_id"), item.CapabilityID, item.ID)
	if ok, reason := a.enforceHubSecurityAppPolicy("manage_skill", map[string]interface{}{"action": "install", "source": originSource, "skill_id": skillID, "install_ref": stringFromMap(metadata, "install_ref")}); !ok {
		return false, fmt.Errorf("%s", reason)
	}
	versionKey = firstCapabilityNonEmpty(versionKey, item.CurrentVersionKey, stringFromMap(metadata, "version_key"), stringFromMap(metadata, "version"))
	if existing := a.findManagedCapabilitySkill(item.ID, skillID, ""); existing != nil && managedSkillVersionCurrent(*existing, versionKey) {
		return false, nil
	}
	stagingDir, err := cskill.PrepareStagingDir(firstCapabilityNonEmpty(skillID, "managed-external-skill"))
	if err != nil {
		return false, err
	}
	var entry *corelib.NLSkillEntry
	switch originSource {
	case corelib.CapabilitySourceClawHub:
		entry, err = cskill.DefaultHubClient().DownloadClawHub(ctx, skillID)
	case corelib.CapabilitySourceGitHub:
		installRef := stringFromMap(metadata, "install_ref")
		if installRef == "" {
			cskill.CleanupStaging(stagingDir)
			return false, fmt.Errorf("managed GitHub skill %s is missing install_ref metadata", item.ID)
		}
		entry, err = cskill.DefaultHubClient().DownloadGitHub(ctx, installRef)
	default:
		err = fmt.Errorf("unsupported external skill source %s", originSource)
	}
	if err != nil {
		cskill.CleanupStaging(stagingDir)
		return false, err
	}
	if entry == nil {
		cskill.CleanupStaging(stagingDir)
		return false, fmt.Errorf("managed external skill %s produced no entry", item.ID)
	}
	entry.HubSkillID = firstCapabilityNonEmpty(entry.HubSkillID, skillID)
	entry.HubVersion = firstCapabilityNonEmpty(versionKey, entry.HubVersion)
	entry.Capability = &corelib.SkillCapabilityRef{CapabilityID: item.ID, VersionKey: firstCapabilityNonEmpty(versionKey, entry.HubVersion), Source: originSource, GlobalKey: item.GlobalKey}
	entry.SkillDir = stagingDir
	rewriteSkillStepWorkingDir(entry, stagingDir)
	existing := a.findManagedCapabilitySkill(item.ID, skillID, entry.Name)
	if existing != nil && managedSkillVersionCurrent(*existing, firstCapabilityNonEmpty(versionKey, entry.HubVersion)) {
		cskill.CleanupStaging(stagingDir)
		return false, nil
	}
	if existing == nil && a.skillNameAlreadyRegistered(entry.Name) {
		cskill.CleanupStaging(stagingDir)
		return false, nil
	}
	var report *cskill.ScanReport
	if a.isRiskGuardrailOffMode() {
		a.emitSkillInstallProgress(entry.Name, "scan-complete", "Risk guardrails are off; installation allowed.", nil)
		a.logSkillInstallSecurityEvent(security.AuditActionHubSkillInstall, "managed_capability_skill_install", security.RiskLow, security.PolicyAllow, fmt.Sprintf("risk guardrails off allowed managed capability %s skill %s", item.ID, entry.Name))
	} else {
		a.emitSkillInstallProgress(entry.Name, "scan-start", "Starting managed capability skill security scan.", nil)
		scanner := cskill.NewSecurityScanner(nil)
		report = scanner.ScanInstallStaged(ctx, entry, entry.SkillDir, func(status string) {
			if a != nil {
				a.log(status)
				a.emitSkillInstallProgress(entry.Name, "scanning", status, nil)
			}
		})
	}
	if report == nil && !a.isRiskGuardrailOffMode() {
		if a.skillInstallMissingScanShouldBlock() {
			cskill.CleanupStaging(stagingDir)
			return false, fmt.Errorf("managed skill %s security scan produced no report", entry.Name)
		}
		a.emitSkillInstallProgress(entry.Name, "scan-complete", "Managed capability skill scan did not produce a report; current policy allows installation.", nil)
		a.logSkillInstallSecurityEvent(security.AuditActionHubSkillInstall, "managed_capability_skill_install", security.RiskCritical, security.PolicyAudit, fmt.Sprintf("current policy allowed managed capability %s skill %s even though scan report was missing", item.ID, entry.Name))
	}
	if report != nil && a.skillInstallScanShouldBlockForSource(report, originSource) {
		cskill.CleanupStaging(stagingDir)
		a.emitSkillInstallProgress(entry.Name, "blocked", "Managed capability skill blocked by pre-install security scan.", report)
		a.logSkillInstallSecurityEvent(security.AuditActionHubSkillReject, "managed_capability_skill_install", report.FinalLevel, security.PolicyDeny, fmt.Sprintf("managed capability %s rejected skill %s: %s", item.ID, entry.Name, report.Summary))
		return false, fmt.Errorf("managed capability skill %s blocked by security scan: level=%s summary=%s", entry.Name, report.FinalLevel, report.Summary)
	} else if report != nil && !report.IsSafe() {
		a.emitSkillInstallProgress(entry.Name, "approved", skillInstallRiskAllowedStatusForSource(originSource), report)
		a.logSkillInstallSecurityEvent(security.AuditActionHubSkillInstall, "managed_capability_skill_install", report.FinalLevel, security.PolicyAudit, fmt.Sprintf("current policy allowed managed capability %s skill %s: %s", item.ID, entry.Name, report.Summary))
	}
	finalDir, err := cskill.CommitStaging(stagingDir, entry.Name)
	if err != nil {
		cskill.CleanupStaging(stagingDir)
		return false, err
	}
	rewriteSkillStepWorkingDir(entry, finalDir)
	entry.SkillDir = finalDir
	if err := writeSkillScanCacheForInstalledEntry(entry, report); err != nil {
		_ = os.RemoveAll(finalDir)
		return false, fmt.Errorf("write skill scan cache: %w", err)
	}
	if err := a.registerOrReplaceManagedCapabilitySkill(*entry, existing); err != nil {
		_ = os.RemoveAll(finalDir)
		return false, err
	}
	a.emitSkillInstallProgress(entry.Name, "done", "Managed capability skill installed successfully.", report)
	go a.installSkillDepsIfMissing(entry.SkillDir, entry.Name)
	return true, nil
}

func (a *App) installManagedHubSkill(ctx context.Context, skillID, hubURL, capabilityID string, capabilityMeta ...string) (bool, error) {
	versionKey := ""
	source := ""
	globalKey := ""
	if len(capabilityMeta) > 0 {
		versionKey = capabilityMeta[0]
	}
	if len(capabilityMeta) > 1 {
		source = capabilityMeta[1]
	}
	if len(capabilityMeta) > 2 {
		globalKey = capabilityMeta[2]
	}
	effectiveSource := firstCapabilityNonEmpty(source, "skillhub")
	if ok, reason := a.enforceHubSecurityAppPolicy("manage_skill", map[string]interface{}{"action": "install", "source": effectiveSource, "skill_id": skillID, "hub_url": hubURL}); !ok {
		return false, fmt.Errorf("%s", reason)
	}
	a.ensureSkillHubClient()
	if a.skillHubClient == nil {
		return false, fmt.Errorf("skill hub client not initialized")
	}
	if a.skillExecutor == nil {
		return false, fmt.Errorf("skill executor not initialized")
	}
	if existing := a.findManagedCapabilitySkill(capabilityID, skillID, ""); existing != nil && managedSkillVersionCurrent(*existing, versionKey) {
		return false, nil
	}
	stagingDir, err := cskill.PrepareStagingDir(firstCapabilityNonEmpty(skillID, "managed-hub-skill"))
	if err != nil {
		return false, err
	}
	entry, err := a.skillHubClient.InstallToDir(ctx, skillID, hubURL, stagingDir)
	if err != nil {
		cskill.CleanupStaging(stagingDir)
		return false, err
	}
	entry.Source = "hub"
	entry.SourceProject = hubURL
	entry.HubSkillID = skillID
	entry.HubVersion = firstCapabilityNonEmpty(versionKey, entry.HubVersion)
	entry.Capability = &corelib.SkillCapabilityRef{CapabilityID: capabilityID, VersionKey: firstCapabilityNonEmpty(versionKey, entry.HubVersion), Source: source, GlobalKey: globalKey}
	existing := a.findManagedCapabilitySkill(capabilityID, skillID, entry.Name)
	if existing != nil && managedSkillVersionCurrent(*existing, firstCapabilityNonEmpty(versionKey, entry.HubVersion)) {
		cskill.CleanupStaging(stagingDir)
		return false, nil
	}
	if existing == nil && a.skillNameAlreadyRegistered(entry.Name) {
		cskill.CleanupStaging(stagingDir)
		return false, nil
	}
	var report *cskill.ScanReport
	if a.isRiskGuardrailOffMode() {
		a.emitSkillInstallProgress(entry.Name, "scan-complete", "Risk guardrails are off; installation allowed.", nil)
		a.logSkillInstallSecurityEvent(security.AuditActionHubSkillInstall, "managed_capability_skill_install", security.RiskLow, security.PolicyAllow, fmt.Sprintf("risk guardrails off allowed managed capability %s skill %s", capabilityID, entry.Name))
	} else {
		a.emitSkillInstallProgress(entry.Name, "scan-start", "Starting managed capability skill security scan.", nil)
		scanner := cskill.NewSecurityScanner(nil)
		report = scanner.ScanInstallStaged(ctx, entry, entry.SkillDir, func(status string) {
			if a != nil {
				a.log(status)
				a.emitSkillInstallProgress(entry.Name, "scanning", status, nil)
			}
		})
	}
	if report == nil && !a.isRiskGuardrailOffMode() {
		if a.skillInstallMissingScanShouldBlock() {
			cskill.CleanupStaging(stagingDir)
			return false, fmt.Errorf("managed skill %s security scan produced no report", entry.Name)
		}
		a.emitSkillInstallProgress(entry.Name, "scan-complete", "Managed capability skill scan did not produce a report; current policy allows installation.", nil)
		a.logSkillInstallSecurityEvent(security.AuditActionHubSkillInstall, "managed_capability_skill_install", security.RiskCritical, security.PolicyAudit, fmt.Sprintf("current policy allowed managed capability %s skill %s even though scan report was missing", capabilityID, entry.Name))
	}
	if report != nil && a.skillInstallScanShouldBlockForSource(report, effectiveSource) {
		cskill.CleanupStaging(stagingDir)
		a.emitSkillInstallProgress(entry.Name, "blocked", "Managed capability skill blocked by pre-install security scan.", report)
		a.logSkillInstallSecurityEvent(
			security.AuditActionHubSkillReject,
			"managed_capability_skill_install",
			report.FinalLevel,
			security.PolicyDeny,
			fmt.Sprintf("managed capability %s rejected skill %s: %s", capabilityID, entry.Name, report.Summary),
		)
		return false, fmt.Errorf("managed capability skill %s blocked by security scan: level=%s summary=%s", entry.Name, report.FinalLevel, report.Summary)
	} else if report != nil && !report.IsSafe() {
		a.emitSkillInstallProgress(entry.Name, "approved", skillInstallRiskAllowedStatusForSource(effectiveSource), report)
		a.logSkillInstallSecurityEvent(
			security.AuditActionHubSkillInstall,
			"managed_capability_skill_install",
			report.FinalLevel,
			security.PolicyAudit,
			fmt.Sprintf("current policy allowed managed capability %s skill %s: %s", capabilityID, entry.Name, report.Summary),
		)
	}
	finalDir, err := cskill.CommitStaging(stagingDir, entry.Name)
	if err != nil {
		cskill.CleanupStaging(stagingDir)
		return false, err
	}
	rewriteSkillStepWorkingDir(entry, finalDir)
	entry.SkillDir = finalDir
	if err := writeSkillScanCacheForInstalledEntry(entry, report); err != nil {
		_ = os.RemoveAll(finalDir)
		return false, fmt.Errorf("write skill scan cache: %w", err)
	}
	if err := a.registerOrReplaceManagedCapabilitySkill(*entry, existing); err != nil {
		_ = os.RemoveAll(finalDir)
		return false, err
	}
	a.emitSkillInstallProgress(entry.Name, "done", "Managed capability skill installed successfully.", report)
	go a.installSkillDepsIfMissing(entry.SkillDir, entry.Name)
	return true, nil
}

func (a *App) findManagedCapabilitySkill(capabilityID, hubSkillID, name string) *corelib.NLSkillEntry {
	if a == nil || a.skillExecutor == nil {
		return nil
	}
	capabilityID = strings.TrimSpace(capabilityID)
	hubSkillID = strings.TrimSpace(hubSkillID)
	name = strings.TrimSpace(name)
	for _, skill := range a.skillExecutor.loadSkills() {
		if skill.Capability != nil && capabilityID != "" && strings.TrimSpace(skill.Capability.CapabilityID) == capabilityID {
			cp := skill
			return &cp
		}
		if hubSkillID != "" && strings.TrimSpace(skill.HubSkillID) == hubSkillID && normalizeSkillEntrySource(skill.Source) == skillEntrySourceHub {
			cp := skill
			return &cp
		}
		if name != "" && skill.Name == name && skill.Capability != nil {
			cp := skill
			return &cp
		}
	}
	return nil
}

func managedSkillVersionCurrent(skill corelib.NLSkillEntry, versionKey string) bool {
	versionKey = strings.TrimSpace(versionKey)
	if versionKey == "" {
		return false
	}
	if skill.Capability != nil && strings.TrimSpace(skill.Capability.VersionKey) == versionKey {
		return true
	}
	return strings.TrimSpace(skill.HubVersion) == versionKey
}

func (a *App) registerOrReplaceManagedCapabilitySkill(entry corelib.NLSkillEntry, existing *corelib.NLSkillEntry) error {
	if a == nil || a.skillExecutor == nil {
		return fmt.Errorf("skill executor not initialized")
	}
	if existing == nil {
		return a.skillExecutor.Register(entry)
	}
	if strings.TrimSpace(entry.CreatedAt) == "" {
		entry.CreatedAt = firstCapabilityNonEmpty(existing.CreatedAt, time.Now().Format(time.RFC3339))
	}
	entry.UsageCount = existing.UsageCount
	entry.SuccessCount = existing.SuccessCount
	entry.FailureCount = existing.FailureCount
	entry.WorkaroundCount = existing.WorkaroundCount
	entry.LastUsedAt = existing.LastUsedAt
	entry.LastError = existing.LastError
	entry.RepairAttemptCount = existing.RepairAttemptCount
	entry.LastRepairAt = existing.LastRepairAt
	entry.RepairHistory = append([]corelib.SkillRepairRecord(nil), existing.RepairHistory...)
	if isShellBrowserAutomationSkillEntry(entry) {
		return browserAutomationSkillRejectedError(entry.Name)
	}
	skills := a.skillExecutor.loadSkills()
	for i, skill := range skills {
		if skill.Name == existing.Name || (skill.Capability != nil && entry.Capability != nil && strings.TrimSpace(skill.Capability.CapabilityID) == strings.TrimSpace(entry.Capability.CapabilityID)) {
			skills[i] = entry
			return a.skillExecutor.saveSkills(skills)
		}
	}
	return a.skillExecutor.Register(entry)
}

func (a *App) ensureHubMCPInstalled(ctx context.Context, client *capabilityMarketClient, item HubCapabilitySummary, versionKey string) (bool, bool, error) {
	metadata := capabilityMetadataMap(item.MetadataJSON)

	// Determine transport type: if metadata has "command", install as local (Stdio) MCP;
	// if it has "endpoint_url", install as remote (HTTP) MCP.
	command := stringFromMap(metadata, "command")
	endpointURL := stringFromMap(metadata, "endpoint_url")

	if command != "" {
		return a.ensureHubMCPInstalledLocal(ctx, item, metadata, versionKey, command)
	}
	if endpointURL == "" {
		return false, false, fmt.Errorf("managed MCP %s has neither command nor endpoint_url metadata", item.ID)
	}
	return a.ensureHubMCPInstalledRemote(ctx, client, item, metadata, versionKey, endpointURL)
}

// ensureHubMCPInstalledLocal installs a Stdio-type MCP capability as a local MCP server.
func (a *App) ensureHubMCPInstalledLocal(_ context.Context, item HubCapabilitySummary, metadata map[string]any, versionKey string, command string) (bool, bool, error) {
	argsRaw := metadata["args"]
	var args []string
	if arr, ok := argsRaw.([]interface{}); ok {
		for _, v := range arr {
			if s, ok := v.(string); ok {
				args = append(args, s)
			}
		}
	}
	if ok, reason := a.enforceHubSecurityAppPolicy("bash", map[string]interface{}{"command": strings.Join(append([]string{command}, args...), " ")}); !ok {
		return false, false, fmt.Errorf("%s", reason)
	}
	envRaw := metadata["env"]
	env := map[string]string{}
	if m, ok := envRaw.(map[string]interface{}); ok {
		for k, v := range m {
			if s, ok := v.(string); ok {
				env[k] = s
			}
		}
	}
	entry := corelib.LocalMCPServerEntry{
		ID:        firstCapabilityNonEmpty(stringFromMap(metadata, "server_id"), item.ID),
		Name:      firstCapabilityNonEmpty(stringFromMap(metadata, "name"), item.DisplayName, item.CapabilityID),
		Command:   command,
		Args:      args,
		Env:       env,
		AutoStart: true,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
		Source:    corelib.MCPSourceMarket,
		Capability: &corelib.MCPServerCapabilityRef{
			CapabilityID: item.ID,
			VersionKey:   firstCapabilityNonEmpty(versionKey, item.CurrentVersionKey),
			Source:       item.Source,
			GlobalKey:    item.GlobalKey,
		},
	}
	installed := false
	if err := a.PatchConfig(func(cfg *corelib.AppConfig) {
		for i := range cfg.LocalMCPServers {
			if cfg.LocalMCPServers[i].ID == entry.ID || (cfg.LocalMCPServers[i].Capability != nil && cfg.LocalMCPServers[i].Capability.CapabilityID == item.ID) {
				entry.CreatedAt = firstCapabilityNonEmpty(cfg.LocalMCPServers[i].CreatedAt, entry.CreatedAt)
				entry.Disabled = cfg.LocalMCPServers[i].Disabled
				cfg.LocalMCPServers[i] = entry
				installed = true
				return
			}
		}
		cfg.LocalMCPServers = append(cfg.LocalMCPServers, entry)
		installed = true
	}); err != nil {
		return false, false, err
	}
	// Trigger local MCP manager to pick up the new server.
	if installed {
		_ = a.SyncLocalMCPServers()
	}
	return installed, false, nil
}

// ensureHubMCPInstalledRemote installs an HTTP-type MCP capability as a remote MCP server.
func (a *App) ensureHubMCPInstalledRemote(ctx context.Context, client *capabilityMarketClient, item HubCapabilitySummary, metadata map[string]any, versionKey string, endpointURL string) (bool, bool, error) {
	if ok, reason := a.enforceHubSecurityAppPolicy("web_fetch", map[string]interface{}{"url": endpointURL}); !ok {
		return false, false, fmt.Errorf("%s", reason)
	}
	server := corelib.MCPServerEntry{
		ID:          firstCapabilityNonEmpty(stringFromMap(metadata, "server_id"), item.ID),
		Name:        firstCapabilityNonEmpty(stringFromMap(metadata, "name"), item.DisplayName, item.CapabilityID),
		EndpointURL: endpointURL,
		AuthType:    firstCapabilityNonEmpty(stringFromMap(metadata, "auth_type"), "none"),
		Headers:     stringMapFromMap(metadata, "headers"),
		CreatedAt:   time.Now().UTC().Format(time.RFC3339),
		Source:      corelib.MCPSourceMarket,
		Capability: &corelib.MCPServerCapabilityRef{
			CapabilityID: item.ID,
			VersionKey:   firstCapabilityNonEmpty(versionKey, item.CurrentVersionKey),
			Source:       item.Source,
			GlobalKey:    item.GlobalKey,
		},
	}
	installed := false
	if err := a.PatchConfig(func(cfg *corelib.AppConfig) {
		for i := range cfg.MCPServers {
			if cfg.MCPServers[i].ID == server.ID || (cfg.MCPServers[i].Capability != nil && cfg.MCPServers[i].Capability.CapabilityID == item.ID) {
				server.AuthSecret = cfg.MCPServers[i].AuthSecret
				server.CreatedAt = firstCapabilityNonEmpty(cfg.MCPServers[i].CreatedAt, server.CreatedAt)
				cfg.MCPServers[i] = server
				installed = true
				return
			}
		}
		cfg.MCPServers = append(cfg.MCPServers, server)
		installed = true
	}); err != nil {
		return false, false, err
	}
	requirements, err := client.listMCPSecretRequirements(ctx, item.ID, firstCapabilityNonEmpty(versionKey, item.CurrentVersionKey))
	if err != nil {
		log.Printf("[capability-market] list MCP secret requirements failed for %s: %v", item.ID, err)
		return installed, false, nil
	}
	return installed, mcpSecretRequirementsNeedUserConfig(ctx, client, server, requirements), nil
}

func mcpSecretRequirementsNeedUserConfig(ctx context.Context, client *capabilityMarketClient, server corelib.MCPServerEntry, requirements []HubMCPSecretRequirement) bool {
	if len(requirements) == 0 {
		return false
	}
	bindingsByName := map[string]HubMCPSecretBinding{}
	hubSecretsByName := map[string]bool{}
	if client != nil {
		bindings, err := client.listMCPSecretBindings(ctx, server.ID)
		if err != nil {
			log.Printf("[capability-market] list MCP secret bindings failed for %s: %v", server.ID, err)
		} else {
			for _, binding := range bindings {
				name := strings.TrimSpace(binding.RequirementName)
				if name != "" {
					bindingsByName[name] = binding
				}
			}
		}
		secrets, err := client.listMCPHubSecrets(ctx, server.ID)
		if err != nil {
			log.Printf("[capability-market] list MCP hub secrets failed for %s: %v", server.ID, err)
		} else {
			for _, secret := range secrets {
				name := strings.TrimSpace(secret.RequirementName)
				if name == "" || strings.TrimSpace(secret.SecretDigest) == "" {
					continue
				}
				hubSecretsByName[name] = true
			}
		}
	}
	for _, req := range requirements {
		if !req.Required {
			continue
		}
		name := strings.TrimSpace(req.Name)
		if name == "" {
			continue
		}
		if mcpSecretRequirementConfigured(req, bindingsByName[name], server, hubSecretsByName[name]) {
			continue
		}
		return true
	}
	return false
}

func mcpSecretRequirementConfigured(req HubMCPSecretRequirement, binding HubMCPSecretBinding, server corelib.MCPServerEntry, hubSecretConfigured bool) bool {
	policy := strings.TrimSpace(strings.ToLower(req.StoragePolicy))
	storage := strings.TrimSpace(strings.ToLower(binding.Storage))
	status := strings.TrimSpace(strings.ToLower(binding.Status))
	configured := status == "configured" || status == "ready"
	if storage == "hub" && policy != "local" && hubSecretConfigured && (configured || strings.TrimSpace(binding.HubSecretRef) != "") {
		return true
	}
	if storage == "local" && policy != "hub" && (configured || strings.TrimSpace(binding.LocalSecretRef) != "") && strings.TrimSpace(server.AuthSecret) != "" {
		return true
	}
	return policy != "hub" && strings.TrimSpace(server.AuthSecret) != ""
}

func capabilityMetadataMap(raw string) map[string]any {
	var out map[string]any
	if strings.TrimSpace(raw) == "" || json.Unmarshal([]byte(raw), &out) != nil {
		return map[string]any{}
	}
	return out
}

func stringFromMap(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

func capabilityPricingModeFromMetadata(m map[string]any) string {
	if mode := stringFromMap(m, "pricing"); mode != "" {
		return mode
	}
	if raw, ok := m["pricing"].(map[string]any); ok {
		if mode := firstCapabilityNonEmpty(stringFromMap(raw, "mode"), stringFromMap(raw, "type")); mode != "" {
			return mode
		}
	}
	return firstCapabilityNonEmpty(stringFromMap(m, "pricing_type"), corelib.CapabilityPricingFree)
}

func stringMapFromMap(m map[string]any, key string) map[string]string {
	raw, ok := m[key].(map[string]any)
	if !ok {
		return nil
	}
	out := map[string]string{}
	for k, v := range raw {
		if s, ok := v.(string); ok {
			out[k] = s
		}
	}
	return out
}

func firstCapabilityNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
