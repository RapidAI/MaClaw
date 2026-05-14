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

type CapabilitySyncStatus struct {
	ManagedChecked   int      `json:"managed_checked"`
	ManagedInstalled int      `json:"managed_installed"`
	Updated          int      `json:"updated"`
	RecommendedCount int      `json:"recommended_count"`
	NeedsUserConfig  []string `json:"needs_user_config,omitempty"`
	Errors           []string `json:"errors,omitempty"`
}

func (a *App) TriggerHubManagedCapabilitySync(reason string) {
	if a.capabilitySyncRunning.Swap(true) {
		return
	}
	go func() {
		defer a.capabilitySyncRunning.Store(false)
		status := a.SyncHubManagedCapabilities()
		if len(status.Errors) > 0 {
			log.Printf("[capability-market] managed sync reason=%s errors=%v", reason, status.Errors)
			return
		}
		log.Printf("[capability-market] managed sync reason=%s checked=%d installed=%d needs_config=%d", reason, status.ManagedChecked, status.ManagedInstalled, len(status.NeedsUserConfig))
	}()
}
func (a *App) SyncHubManagedCapabilities() CapabilitySyncStatus {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	return a.syncHubManagedCapabilities(ctx)
}

func (a *App) InstallHubCapability(capabilityRef string) CapabilitySyncStatus {
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
		if installed {
			status.ManagedInstalled = 1
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
		return status
	}
	deployments, err := c.listManagedDeployments(ctx)
	if err != nil {
		status.Errors = append(status.Errors, err.Error())
		return status
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
	if status.ManagedInstalled > 0 || status.Updated > 0 || len(status.NeedsUserConfig) > 0 {
		a.emitEvent("hub-managed-capabilities-synced", status)
	}
	return status
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

func (a *App) installManagedExternalSkill(ctx context.Context, item HubCapabilitySummary, metadata map[string]any, versionKey string, originSource string) (bool, error) {
	if a.skillExecutor == nil {
		return false, fmt.Errorf("skill executor not initialized")
	}
	skillID := firstCapabilityNonEmpty(stringFromMap(metadata, "skill_id"), item.CapabilityID, item.ID)
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
	a.emitSkillInstallProgress(entry.Name, "scan-start", "Starting managed capability skill security scan.", nil)
	scanner := cskill.NewSecurityScanner(nil)
	report := scanner.ScanInstallStaged(ctx, entry, entry.SkillDir, func(status string) {
		if a != nil {
			a.log(status)
			a.emitSkillInstallProgress(entry.Name, "scanning", status, nil)
		}
	})
	if report == nil {
		cskill.CleanupStaging(stagingDir)
		return false, fmt.Errorf("managed skill %s security scan produced no report", entry.Name)
	}
	if !report.IsSafe() {
		cskill.CleanupStaging(stagingDir)
		a.emitSkillInstallProgress(entry.Name, "blocked", "Managed capability skill blocked by pre-install security scan.", report)
		a.logSkillInstallSecurityEvent(security.AuditActionHubSkillReject, "managed_capability_skill_install", report.FinalLevel, security.PolicyDeny, fmt.Sprintf("managed capability %s rejected skill %s: %s", item.ID, entry.Name, report.Summary))
		return false, fmt.Errorf("managed capability skill %s blocked by security scan: level=%s summary=%s", entry.Name, report.FinalLevel, report.Summary)
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
	a.emitSkillInstallProgress(entry.Name, "scan-start", "Starting managed capability skill security scan.", nil)
	scanner := cskill.NewSecurityScanner(nil)
	report := scanner.ScanInstallStaged(ctx, entry, entry.SkillDir, func(status string) {
		if a != nil {
			a.log(status)
			a.emitSkillInstallProgress(entry.Name, "scanning", status, nil)
		}
	})
	if report == nil {
		cskill.CleanupStaging(stagingDir)
		return false, fmt.Errorf("managed skill %s security scan produced no report", entry.Name)
	}
	if !report.IsSafe() {
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
	server := corelib.MCPServerEntry{
		ID:          firstCapabilityNonEmpty(stringFromMap(metadata, "server_id"), item.ID),
		Name:        firstCapabilityNonEmpty(stringFromMap(metadata, "name"), item.DisplayName, item.CapabilityID),
		EndpointURL: stringFromMap(metadata, "endpoint_url"),
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
	if server.EndpointURL == "" {
		return false, false, fmt.Errorf("managed MCP %s has no endpoint_url metadata", item.ID)
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
				bindingsByName[name] = HubMCPSecretBinding{MCPServerID: server.ID, RequirementName: name, Storage: "hub", HubSecretRef: secret.ID, Status: "configured"}
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
		if mcpSecretRequirementConfigured(req, bindingsByName[name], server) {
			continue
		}
		return true
	}
	return false
}

func mcpSecretRequirementConfigured(req HubMCPSecretRequirement, binding HubMCPSecretBinding, server corelib.MCPServerEntry) bool {
	policy := strings.TrimSpace(strings.ToLower(req.StoragePolicy))
	storage := strings.TrimSpace(strings.ToLower(binding.Storage))
	status := strings.TrimSpace(strings.ToLower(binding.Status))
	configured := status == "configured" || status == "ready"
	if storage == "hub" && policy != "local" && (configured || strings.TrimSpace(binding.HubSecretRef) != "") {
		return true
	}
	if storage == "local" && policy != "hub" && (configured || strings.TrimSpace(binding.LocalSecretRef) != "") {
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
