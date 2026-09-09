package guiapp

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"reflect"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/security"
	cskill "github.com/RapidAI/CodeClaw/corelib/skill"
)

const capabilityManagedSyncMinRetry = 5 * time.Minute
const capabilityManagedSyncMinInterval = 5 * time.Minute
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

func (a *App) shouldAutoSyncHubManagedCapabilitiesOnConnect() bool {
	if a == nil {
		return false
	}
	return strings.TrimSpace(a.testHomeDir) == ""
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
		if delay := capabilityManagedSyncSuccessDelay(reason); delay <= 0 {
			a.capabilitySyncNextAttempt.Store(time.Time{})
		} else {
			a.capabilitySyncNextAttempt.Store(time.Now().Add(delay))
		}
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
	// Hub URL changed — permanent skip decisions may no longer be valid for the
	// new hub (different capability configurations). Clear and re-evaluate.
	a.capabilitySyncPermanentSkips.Range(func(key, _ interface{}) bool {
		a.capabilitySyncPermanentSkips.Delete(key)
		return true
	})
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

func capabilityManagedSyncSuccessDelay(reason string) time.Duration {
	if isCapabilitySyncImmediateReason(reason) {
		return 0
	}
	return capabilityManagedSyncMinInterval
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
			// Skip capabilities that have been diagnosed as permanently non-installable.
			// The skip has a TTL (1 hour) so that if the user manually resolves the
			// conflict (e.g., uninstalls a conflicting skill), the next sync after
			// expiry will re-attempt installation.
			if ts, ok := a.capabilitySyncPermanentSkips.Load(item.ID); ok {
				if skipTime, isTime := ts.(time.Time); isTime && time.Since(skipTime) < 1*time.Hour {
					continue
				}
				// Skip expired — re-evaluate.
				a.capabilitySyncPermanentSkips.Delete(item.ID)
			}
			installed, err := a.ensureHubSkillInstalled(ctx, *item, dep.CapabilityVersionKey)
			if err != nil {
				// A managed deployment can outlive its package on the Hub (for
				// example after an administrator deletes or republishes a skill).
				// This is not a transient download failure, so do not make every
				// heartbeat retry it and flood the application log.
				if isManagedSkillPackageNotFoundError(err) {
					a.capabilitySyncPermanentSkips.Store(item.ID, time.Now())
					log.Printf("[capability-market] managed capability %s permanently skipped: skill package %q no longer exists on hub", item.ID, firstCapabilityNonEmpty(item.CapabilityID, item.ID))
					continue
				}
				status.Errors = append(status.Errors, err.Error())
				continue
			}
			if installed {
				status.ManagedInstalled++
			} else if !a.isHubSkillCapabilityInstalled(*item) {
				// ensureHubSkillInstalled returned (false, nil) AND the skill is not
				// found locally → permanent condition (name conflict / stale deployment).
				// Don't add to status.Errors (which triggers 5-minute retry). Instead
				// log once and suppress future attempts for this capability (with TTL).
				reason := a.diagnoseSkillNotInstalled(*item)
				a.capabilitySyncPermanentSkips.Store(item.ID, time.Now())
				log.Printf("[capability-market] managed capability %s permanently skipped: %s", item.ID, reason)
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
		if cfg, err := a.LoadConfig(); err == nil {
			hubURL = strings.TrimSpace(cfg.RemoteHubURL)
		}
		if hubURL == "" {
			hubURL = NewSkillMarketClient(a).baseURL()
		}
	}
	if skillID == "" || hubURL == "" {
		return false, fmt.Errorf("managed skill %s is missing skill_id or hub_url metadata", item.ID)
	}
	return a.installManagedHubSkill(ctx, skillID, hubURL, item.ID, firstCapabilityNonEmpty(versionKey, item.CurrentVersionKey), item.Source, item.GlobalKey, firstCapabilityNonEmpty(item.PackageSHA256, item.PackageChecksum, stringFromMap(metadata, "package_sha256"), stringFromMap(metadata, "sha256"), stringFromMap(metadata, "package_checksum"), stringFromMap(metadata, "checksum")), firstCapabilityNonEmpty(item.PackageSignature, stringFromMap(metadata, "package_signature"), stringFromMap(metadata, "signature")))
}

func (a *App) isHubSkillCapabilityInstalled(item HubCapabilitySummary) bool {
	if a == nil {
		return false
	}
	metadata := capabilityMetadataMap(item.MetadataJSON)
	skillID := firstCapabilityNonEmpty(stringFromMap(metadata, "skill_id"), stringFromMap(metadata, "hub_skill_id"), item.CapabilityID, item.ID)
	return a.findManagedCapabilitySkill(item.ID, skillID, "") != nil
}

// diagnoseSkillNotInstalled returns a non-empty reason string when the skill
// cannot be installed due to a permanent condition (name conflict, missing
// metadata, etc.) that won't resolve on retry. Returns "" only when the
// condition is genuinely transient (e.g., network failure during download).
//
// Mechanism: ensureHubSkillInstalled returns (false, nil) in exactly three cases:
//  1. Version already current → caught by isHubSkillCapabilityInstalled (returns true),
//     so we never reach this function for that case.
//  2. Name conflict: skillNameAlreadyRegistered(entry.Name) — a different skill
//     with the same display name exists (permanent).
//  3. Name conflict: findManagedCapabilitySkill found a match under a different
//     capability ID (permanent).
//
// Since case 1 is excluded before calling this function, reaching here means
// we're in case 2 or 3. We attempt to identify which one for diagnostic logging.
// If we can't identify the specific sub-case (e.g., the name conflict requires
// downloading the skill first to discover the name), we still return a generic
// permanent reason — because ALL paths that reach here are permanent.
func (a *App) diagnoseSkillNotInstalled(item HubCapabilitySummary) string {
	if a == nil || a.skillExecutor == nil {
		return "skill executor not initialized"
	}
	metadata := capabilityMetadataMap(item.MetadataJSON)
	skillID := firstCapabilityNonEmpty(stringFromMap(metadata, "skill_id"), stringFromMap(metadata, "hub_skill_id"), item.CapabilityID, item.ID)
	if skillID == "" {
		return "missing skill_id in capability metadata"
	}
	// Check if a skill with matching hubSkillID exists but under a different
	// capability ID or without a capability ref (manual install).
	for _, skill := range a.skillExecutor.loadSkills() {
		if strings.TrimSpace(skill.HubSkillID) != skillID {
			continue
		}
		if skill.Capability == nil {
			return fmt.Sprintf("name conflict: skill '%s' already registered (hubSkillID=%s) without capability ref — likely a manual install", skill.Name, skillID)
		}
		if strings.TrimSpace(skill.Capability.CapabilityID) != strings.TrimSpace(item.ID) {
			return fmt.Sprintf("name conflict: skill '%s' registered under different capability %s", skill.Name, skill.Capability.CapabilityID)
		}
	}
	// Can't identify the specific sub-case from loaded skills alone (the name
	// conflict is only discoverable after downloading the skill entry). But we
	// know this is still permanent — ensureHubSkillInstalled returned (false, nil)
	// and the skill is NOT found by isHubSkillCapabilityInstalled.
	return fmt.Sprintf("skill install silently skipped (likely name conflict with an existing skill; hubSkillID=%s)", skillID)
}

func skillInstallStatus(installed bool) string {
	if installed {
		return "installed"
	}
	return "missing"
}

// isManagedSkillPackageNotFoundError identifies the Hub's explicit, permanent
// package-not-found response. It deliberately does not treat arbitrary 404s as
// permanent: those can still indicate a mismatched or temporarily unavailable
// marketplace endpoint and must retain the normal retry/circuit-breaker flow.
func isManagedSkillPackageNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	lower := strings.ToLower(err.Error())
	return strings.Contains(lower, "skill_not_found") ||
		strings.Contains(lower, "skill package not found")
}

func (a *App) installManagedExternalSkill(ctx context.Context, item HubCapabilitySummary, metadata map[string]any, versionKey string, originSource string) (bool, error) {
	if a.skillExecutor == nil {
		return false, fmt.Errorf("skill executor not initialized")
	}
	// The queue is a system-wide safety boundary. Do this before any remote
	// download or filesystem mutation so an unreadable/unsupported queue cannot
	// be bypassed by an Enterprise/ClawHub/GitHub install path.
	if err := cskill.CheckEvolutionCompensationQueue(); err != nil {
		return false, fmt.Errorf("managed skill install blocked: evolution compensation queue unavailable: %w", err)
	}
	// Managed installs mutate YAML/config/index state. Prevent asynchronous
	// status-overlay reconciliation from racing the transaction.
	a.skillExecutor.suspendStatusOverlayPersistence()
	defer a.skillExecutor.resumeStatusOverlayPersistence()
	requestID := fmt.Sprintf("evo_managed_install_%d", time.Now().UnixNano())
	configRevision := skillEvolutionConfigRevision(a)
	skillID := firstCapabilityNonEmpty(stringFromMap(metadata, "skill_id"), item.CapabilityID, item.ID)
	if ok, reason := a.enforceHubSecurityAppPolicy("manage_skill", map[string]interface{}{"action": "install", "source": originSource, "skill_id": skillID, "install_ref": stringFromMap(metadata, "install_ref")}); !ok {
		return false, fmt.Errorf("%s", reason)
	}
	versionKey = firstCapabilityNonEmpty(versionKey, item.CurrentVersionKey, stringFromMap(metadata, "version_key"), stringFromMap(metadata, "version"))
	if existing := a.findManagedCapabilitySkill(item.ID, skillID, ""); existing != nil && managedSkillVersionCurrent(*existing, versionKey) {
		return false, nil
	}
	stagingRoot, err := a.skillStagingDir()
	if err != nil {
		return false, err
	}
	stagingDir, err := cskill.PrepareStagingDirInRoot(stagingRoot, firstCapabilityNonEmpty(skillID, "managed-external-skill"))
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
	if existing == nil {
		if err := a.commitStagedSkillInstall(ctx, entry, stagingDir, "managed_capability_external", report, requestID, configRevision); err != nil {
			return false, err
		}
		go a.installSkillDepsIfMissing(entry.SkillDir, entry.Name)
		return true, nil
	}
	// Existing managed-capability installs use the same shared directory
	// transaction as fresh installs. The committer preserves runtime counters,
	// retains .prev until final audit, and owns rollback/cleanup state.
	if err := a.commitStagedSkillInstallWithExisting(ctx, entry, stagingDir, "managed_capability_external", report, requestID, configRevision, existing); err != nil {
		return false, err
	}
	go a.installSkillDepsIfMissing(entry.SkillDir, entry.Name)
	return true, nil
}

func (a *App) installManagedHubSkill(ctx context.Context, skillID, hubURL, capabilityID string, capabilityMeta ...string) (bool, error) {
	versionKey := ""
	source := ""
	globalKey := ""
	expectedPackageSHA256 := ""
	expectedPackageSignature := ""
	if len(capabilityMeta) > 0 {
		versionKey = capabilityMeta[0]
	}
	if len(capabilityMeta) > 1 {
		source = capabilityMeta[1]
	}
	if len(capabilityMeta) > 2 {
		globalKey = capabilityMeta[2]
	}
	if len(capabilityMeta) > 3 {
		expectedPackageSHA256 = capabilityMeta[3]
	}
	if len(capabilityMeta) > 4 {
		expectedPackageSignature = capabilityMeta[4]
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
	if err := cskill.CheckEvolutionCompensationQueue(); err != nil {
		return false, fmt.Errorf("managed Hub install blocked: evolution compensation queue unavailable: %w", err)
	}
	a.skillExecutor.suspendStatusOverlayPersistence()
	defer a.skillExecutor.resumeStatusOverlayPersistence()
	requestID := fmt.Sprintf("evo_managed_hub_install_%d", time.Now().UnixNano())
	configRevision := skillEvolutionConfigRevision(a)
	if existing := a.findManagedCapabilitySkill(capabilityID, skillID, ""); existing != nil && managedSkillVersionCurrent(*existing, versionKey) {
		return false, nil
	}
	stagingRoot, err := a.skillStagingDir()
	if err != nil {
		return false, err
	}
	stagingDir, err := cskill.PrepareStagingDirInRoot(stagingRoot, firstCapabilityNonEmpty(skillID, "managed-hub-skill"))
	if err != nil {
		return false, err
	}
	entry, err := a.skillHubClient.InstallToDirWithIntegrity(ctx, skillID, hubURL, stagingDir, expectedPackageSHA256, expectedPackageSignature)
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
	if existing == nil {
		if err := a.commitStagedSkillInstall(ctx, entry, stagingDir, "managed_capability_hub", report, requestID, configRevision); err != nil {
			return false, err
		}
		go a.installSkillDepsIfMissing(entry.SkillDir, entry.Name)
		return true, nil
	}
	if err := a.commitStagedSkillInstallWithExisting(ctx, entry, stagingDir, "managed_capability_hub", report, requestID, configRevision, existing); err != nil {
		return false, err
	}
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
	if isShellBrowserAutomationSkillEntry(entry) {
		return browserAutomationSkillRejectedError(entry.Name)
	}
	// All production managed-capability paths now stage the package and call
	// commitStagedSkillInstall{WithExisting}. Keep this historical helper
	// fail-closed so a future caller cannot silently recreate its former direct
	// Register/saveSkills bypass without supplying scan, compensation, checked
	// index and final-audit evidence.
	return fmt.Errorf("managed capability skill %q must be committed through the staged skill transaction", entry.Name)
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

// commitMarketplaceMCPConfig provides the configuration-only transaction
// boundary for marketplace MCP installs/updates. MCP entries do not live in
// the Skill registry, so the durable pre-image is the complete config file.
// The snapshot is restored on mutation/audit failure and is discarded only
// after a strict final audit and committed-state persistence.
func (a *App) commitMarketplaceMCPConfig(ctx context.Context, item HubCapabilitySummary, versionKey, transport string, mutate func(*corelib.AppConfig) (bool, error)) (bool, error) {
	if a == nil || mutate == nil {
		return false, fmt.Errorf("MCP config transaction is not configured")
	}
	if err := cskill.CheckEvolutionCompensationQueue(); err != nil {
		return false, fmt.Errorf("MCP install blocked: evolution compensation queue unavailable: %w", err)
	}
	skillName := firstCapabilityNonEmpty(item.ID, item.CapabilityID)
	const action = "capability_mcp_config"
	if _, pending, err := cskill.RecoverPendingEvolutionCompensationsForActionPrefixAndSkill(action, skillName, nil, nil); err != nil {
		return false, fmt.Errorf("MCP compensation recovery failed: %w", err)
	} else if pending > 0 {
		return false, fmt.Errorf("MCP install blocked: pending compensation requires review")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	configPath, err := a.getConfigPath()
	if err != nil {
		return false, err
	}
	// Perform a read-only preflight before creating durable state. This keeps
	// an already-current MCP registration a true zero-side-effect no-op (no
	// compensation row and no audit pair). The mutation is repeated inside the
	// serialized PatchConfigIfChanged callback below to close the normal
	// concurrent-writer window; if another writer changes the result meanwhile,
	// that callback remains authoritative.
	preflightCfg, err := a.LoadConfig()
	if err != nil {
		return false, fmt.Errorf("load MCP config: %w", err)
	}
	preflightChanged, err := mutate(&preflightCfg)
	if err != nil {
		return false, err
	}
	if !preflightChanged {
		return false, nil
	}
	original, readErr := os.ReadFile(configPath)
	exists := readErr == nil
	if readErr != nil && !os.IsNotExist(readErr) {
		return false, fmt.Errorf("read MCP config pre-image: %w", readErr)
	}
	requestID := fmt.Sprintf("evo_mcp_config_%d", time.Now().UnixNano())
	record := cskill.NewEvolutionCompensationRecord(requestID, skillName, action, "", nil, false, nil, "mcp_config_rollback")
	record.FinalAuditKind = cskill.KindFromEventName("skill:mcp_config_installed")
	// This transaction owns only the application config file; there is no Skill
	// definition or routing index to rebuild during generic crash recovery.
	record.SetSkipIndexRefresh(true)
	record.SetFileSnapshots([]cskill.EvolutionFileSnapshot{{Path: configPath, Exists: exists, BackupB64: base64.StdEncoding.EncodeToString(original)}})
	if err := cskill.PersistEvolutionCompensation(record); err != nil {
		return false, fmt.Errorf("persist MCP compensation: %w", err)
	}
	data := map[string]string{"skill": skillName, "action": action, "transport": transport, "decision": "pending", "request_id": requestID, "attempt": "1", "config_revision": skillEvolutionConfigRevision(a), "schema_version": "2", "evidence_mode": "none"}
	var postImage []byte
	writeAttempted := false
	persisted := false
	rollback := func(cause error) error {
		if !writeAttempted {
			// The callback either observed a no-op or failed before asking the
			// config transaction to write. There is no business pre-image to
			// restore; only remove the prepared compensation record.
			if clearErr := cskill.ClearEvolutionCompensation(requestID, skillName, action); clearErr != nil {
				if markErr := cskill.MarkEvolutionCompensationRollbackFailure(&record, clearErr); markErr != nil {
					return fmt.Errorf("%v; persist MCP preflight cleanup state: %w", cause, markErr)
				}
				return fmt.Errorf("%v; MCP preflight cleanup pending: %w", cause, clearErr)
			}
			return cause
		}
		// Never restore a stale pre-image over an unrelated concurrent config
		// mutation. Once PatchConfig has persisted our image, compare the file
		// with the post-image captured immediately afterwards; a mismatch means
		// another writer won the race and the compensation must remain pending
		// for explicit reconciliation.
		if persisted {
			current, readErr := os.ReadFile(configPath)
			if readErr != nil || !bytes.Equal(current, postImage) {
				concurrentErr := fmt.Errorf("%v; concurrent MCP config mutation detected (restore skipped)", cause)
				if markErr := cskill.MarkEvolutionCompensationRollbackFailure(&record, concurrentErr); markErr != nil {
					return fmt.Errorf("%v; persist concurrent-mutation compensation: %w", concurrentErr, markErr)
				}
				return concurrentErr
			}
		}
		if restoreErr := cskill.RestoreEvolutionCompensation(record, nil, nil); restoreErr != nil {
			rollbackErr := fmt.Errorf("%v; restore config: %v", cause, restoreErr)
			if markErr := cskill.MarkEvolutionCompensationRollbackFailure(&record, rollbackErr); markErr != nil {
				return fmt.Errorf("%v; persist MCP restore failure: %w", rollbackErr, markErr)
			}
			return rollbackErr
		}
		// RestoreEvolutionCompensation repairs the durable file pre-image. Drop
		// the in-memory published snapshot as well; otherwise a subsequent
		// LoadConfig could continue serving the failed MCP mutation until another
		// unrelated config write happens to invalidate the cache.
		a.configMu.Lock()
		a.invalidateConfigCacheLocked()
		a.configMu.Unlock()
		if clearErr := cskill.ClearEvolutionCompensation(requestID, skillName, action); clearErr != nil {
			if markErr := cskill.MarkEvolutionCompensationRollbackFailure(&record, clearErr); markErr != nil {
				return fmt.Errorf("%v; persist MCP cleanup state: %w", cause, markErr)
			}
			return fmt.Errorf("%v; MCP compensation cleanup pending: %w", cause, clearErr)
		}
		return cause
	}
	if err := cskill.RecordEvolutionEventStrict("skill:mcp_config_install_started", data, "desktop"); err != nil {
		return false, rollback(fmt.Errorf("MCP pre-audit failed: %w", err))
	}
	// Apply the mutation through PatchConfigIfChanged so the config transaction
	// serializes against concurrent writers and always mutates the latest
	// published snapshot. Replacing a stale LoadConfig result wholesale could
	// otherwise clobber unrelated settings changed between the read and write.
	var mutateErr error
	changed, err := a.PatchConfigIfChanged(func(current *corelib.AppConfig) bool {
		if ctxErr := ctx.Err(); ctxErr != nil {
			mutateErr = ctxErr
			return false
		}
		changed, err := mutate(current)
		if err != nil {
			mutateErr = err
			return false
		}
		if changed {
			writeAttempted = true
		}
		return changed
	})
	if mutateErr != nil {
		return false, rollback(mutateErr)
	}
	if err != nil {
		return false, rollback(fmt.Errorf("save MCP config: %w", err))
	}
	if !changed {
		if clearErr := cskill.ClearEvolutionCompensation(requestID, skillName, action); clearErr != nil {
			// A no-op has no business mutation to roll back, but the durable
			// pre-image is still an admission blocker until it is cleared.
			// Preserve it as a committed cleanup-pending record so recovery does
			// not mistake this path for an uncommitted config change.
			if markErr := cskill.MarkEvolutionCompensationRollbackFailure(&record, clearErr); markErr != nil {
				return false, fmt.Errorf("MCP no-op cleanup state: %w (clear: %v)", markErr, clearErr)
			}
			return false, fmt.Errorf("MCP no-op cleanup pending: %w", clearErr)
		}
		return false, nil
	}
	if err := ctx.Err(); err != nil {
		return false, rollback(err)
	}
	persisted = true
	postImage, err = os.ReadFile(configPath)
	if err != nil {
		return true, rollback(fmt.Errorf("read persisted MCP config: %w", err))
	}
	if err := record.SetFileSnapshotPostImage(configPath, postImage, true); err != nil {
		return true, rollback(fmt.Errorf("record MCP post-image: %w", err))
	}
	if err := cskill.ReplaceEvolutionCompensation(record); err != nil {
		return true, rollback(fmt.Errorf("persist MCP post-image fence: %w", err))
	}
	if err := ctx.Err(); err != nil {
		return false, rollback(err)
	}
	finalData := make(map[string]string, len(data)+2)
	for key, value := range data {
		finalData[key] = value
	}
	finalData["decision"] = "applied"
	finalData["transaction_state"] = "committed"
	if err := cskill.RecordEvolutionEventStrict("skill:mcp_config_installed", finalData, "desktop"); err != nil {
		return false, rollback(fmt.Errorf("MCP final audit failed: %w", err))
	}
	record.TransactionState = "committed"
	record.CleanupStatus = "pending"
	record.FailureReason = "post_commit_cleanup_pending"
	if err := cskill.ReplaceEvolutionCompensation(record); err != nil {
		return true, fmt.Errorf("MCP committed but cleanup state unavailable: %w", err)
	}
	if err := cskill.ClearEvolutionCompensation(requestID, skillName, action); err != nil {
		if markErr := cskill.MarkEvolutionCompensationCleanupFailure(&record, err); markErr != nil {
			return true, fmt.Errorf("MCP committed; persist cleanup state: %w", markErr)
		}
		return true, fmt.Errorf("MCP committed but cleanup is pending: %w", err)
	}
	return true, nil
}

// ensureHubMCPInstalledLocal installs a Stdio-type MCP capability as a local MCP server.
func (a *App) ensureHubMCPInstalledLocal(ctx context.Context, item HubCapabilitySummary, metadata map[string]any, versionKey string, command string) (bool, bool, error) {
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
	installed, err := a.commitMarketplaceMCPConfig(ctx, item, versionKey, "stdio", func(cfg *corelib.AppConfig) (bool, error) {
		if cfg == nil {
			return false, fmt.Errorf("MCP config is nil")
		}
		for i := range cfg.LocalMCPServers {
			if cfg.LocalMCPServers[i].ID == entry.ID || (cfg.LocalMCPServers[i].Capability != nil && cfg.LocalMCPServers[i].Capability.CapabilityID == item.ID) {
				entry.CreatedAt = firstCapabilityNonEmpty(cfg.LocalMCPServers[i].CreatedAt, entry.CreatedAt)
				entry.Disabled = cfg.LocalMCPServers[i].Disabled
				if reflect.DeepEqual(cfg.LocalMCPServers[i], entry) {
					return false, nil
				}
				cfg.LocalMCPServers[i] = entry
				return true, nil
			}
		}
		cfg.LocalMCPServers = append(cfg.LocalMCPServers, entry)
		return true, nil
	})
	if err != nil {
		return false, false, err
	}
	// Trigger local MCP manager to pick up the new server. A previously
	// committed config with a persisted runtime failure is retried only when
	// its bounded backoff has elapsed; no-op config reconciliation must not
	// erase or bypass that admission state.
	if installed || a.mcpRuntimeSyncShouldRetry(entry.ID) {
		if installed {
			// A new config revision starts a fresh runtime-sync retry budget;
			// carrying a previous endpoint/command's failures into the new
			// revision could escalate it to needs_review prematurely.
			if stateErr := a.resetMCPRuntimeSyncPending(entry.ID, "stdio"); stateErr != nil {
				return true, false, fmt.Errorf("MCP config committed but runtime state reset failed: %w", stateErr)
			}
		}
		a.ensureLocalMCPManager()
		if a.localMCPManager == nil {
			err := fmt.Errorf("MCP config committed but local runtime manager is unavailable")
			if stateErr := a.recordMCPRuntimeSyncFailure(entry.ID, "stdio", "", err); stateErr != nil {
				err = fmt.Errorf("%v; persist runtime sync state: %w", err, stateErr)
			}
			return true, false, err
		}
		if err := a.localMCPManager.SyncFromConfigCheckedContext(ctx); err != nil {
			// The config transaction is already committed, so do not claim a
			// rollback. Surface the runtime-sync failure to keep inventory and
			// execution admission fail-closed until the manager is reconciled.
			runtimeErr := fmt.Errorf("MCP config committed but local runtime sync failed: %w", err)
			if stateErr := a.recordMCPRuntimeSyncFailure(entry.ID, "stdio", "", runtimeErr); stateErr != nil {
				runtimeErr = fmt.Errorf("%v; persist runtime sync state: %w", runtimeErr, stateErr)
			}
			return true, false, runtimeErr
		}
		if stateErr := a.markMCPRuntimeSyncReady(entry.ID); stateErr != nil {
			return true, false, fmt.Errorf("MCP runtime ready but state cleanup failed: %w", stateErr)
		}
	}
	if !installed && a.mcpRuntimeSyncPending(entry.ID) {
		return false, false, fmt.Errorf("MCP runtime for %q remains unavailable; retry is pending or requires review", entry.ID)
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
	installed, err := a.commitMarketplaceMCPConfig(ctx, item, versionKey, "http", func(cfg *corelib.AppConfig) (bool, error) {
		if cfg == nil {
			return false, fmt.Errorf("MCP config is nil")
		}
		for i := range cfg.MCPServers {
			if cfg.MCPServers[i].ID == server.ID || (cfg.MCPServers[i].Capability != nil && cfg.MCPServers[i].Capability.CapabilityID == item.ID) {
				server.AuthSecret = cfg.MCPServers[i].AuthSecret
				server.CreatedAt = firstCapabilityNonEmpty(cfg.MCPServers[i].CreatedAt, server.CreatedAt)
				if reflect.DeepEqual(cfg.MCPServers[i], server) {
					return false, nil
				}
				cfg.MCPServers[i] = server
				return true, nil
			}
		}
		cfg.MCPServers = append(cfg.MCPServers, server)
		return true, nil
	})
	if err != nil {
		return false, false, err
	}
	// Configuration commit and remote runtime readiness are separate state
	// axes. For a newly installed/updated managed MCP, require a checked
	// initialize + tools/list probe before reporting the runtime as ready. A
	// failure does not roll back the already-audited config transaction, but it
	// is surfaced so callers keep execution/inventory admission fail-closed.
	if installed || a.mcpRuntimeSyncShouldRetry(server.ID) {
		if installed {
			// Reset failures from an older endpoint/auth revision before probing
			// this newly committed configuration.
			if stateErr := a.resetMCPRuntimeSyncPending(server.ID, "http"); stateErr != nil {
				return true, false, fmt.Errorf("MCP config committed but runtime state reset failed: %w", stateErr)
			}
		}
		a.ensureInteractionInfra()
		if a.mcpRegistry == nil {
			err := fmt.Errorf("MCP config committed but remote runtime registry is unavailable")
			if stateErr := a.recordMCPRuntimeSyncFailure(server.ID, "http", "", err); stateErr != nil {
				err = fmt.Errorf("%v; persist runtime sync state: %w", err, stateErr)
			}
			return true, false, err
		}
		if err := a.mcpRegistry.HealthCheckStrictContext(ctx, server.ID); err != nil {
			runtimeErr := fmt.Errorf("MCP config committed but remote runtime sync failed: %w", err)
			if stateErr := a.recordMCPRuntimeSyncFailure(server.ID, "http", "", runtimeErr); stateErr != nil {
				runtimeErr = fmt.Errorf("%v; persist runtime sync state: %w", runtimeErr, stateErr)
			}
			return true, false, runtimeErr
		}
		if stateErr := a.markMCPRuntimeSyncReady(server.ID); stateErr != nil {
			return true, false, fmt.Errorf("MCP runtime ready but state cleanup failed: %w", stateErr)
		}
	}
	if !installed && a.mcpRuntimeSyncPending(server.ID) {
		return false, false, fmt.Errorf("MCP runtime for %q remains unavailable; retry is pending or requires review", server.ID)
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
