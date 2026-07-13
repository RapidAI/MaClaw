package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/memory"
	"github.com/RapidAI/CodeClaw/corelib/remote"
	"github.com/RapidAI/CodeClaw/corelib/scheduler"
	"github.com/RapidAI/CodeClaw/corelib/security"
	"github.com/RapidAI/CodeClaw/corelib/textutil"
	"log"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/session"
	cskill "github.com/RapidAI/CodeClaw/corelib/skill"
	v2 "github.com/RapidAI/CodeClaw/corelib/workflow/v2"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// HubSkillUpdateInfo describes an available update for a locally installed Hub Skill.
type HubSkillUpdateInfo struct {
	SkillName      string `json:"skill_name"`
	HubSkillID     string `json:"hub_skill_id"`
	CurrentVersion string `json:"current_version"`
	LatestVersion  string `json:"latest_version"`
	HubURL         string `json:"hub_url"`
}

func isVisibleAIAssistantProgressText(text string) bool {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return false
	}

	// Legacy progress rows may start with decorative pictographs; normalize first.
	body := strings.TrimSpace(textutil.StripLeadingEmojiCluster(trimmed))

	lower := strings.ToLower(body)
	blockedMarkers := []string{"search", "thinking", "thought", "search first", "running tool", "preparing", "正在执行工具", "先搜索"}

	for _, marker := range blockedMarkers {
		if strings.Contains(lower, strings.ToLower(marker)) {
			return false
		}
	}
	if strings.HasPrefix(body, "Coding Agent:") {
		return true
	}
	if isCodingAgentEventText(body) {
		return true
	}
	// Tool-name progress after optional legacy pictograph: "正在执行 weather-query..."
	// Also allow early pre-loop acks ("收到，正在处理") so first-token wait is not silent.
	visiblePrefixes := []string{
		"Preparing", "Running", "Generating", "Uploading", "Downloading", "Saving",
		"正在生成", "正在执行", "正在处理", "正在准备", "已接近", "收到",
	}
	for _, prefix := range visiblePrefixes {
		if strings.HasPrefix(body, prefix) {
			return true
		}
	}
	return false
}

// BackupSkills exports all NL Skills to a zip file (Wails binding).
func (a *App) BackupSkills(outputPath string) error {
	if err := a.ensureWorkflowAllowsRemoteToolCall("manage_skill", map[string]interface{}{"action": "backup", "output_path": outputPath}); err != nil {
		return err
	}
	a.ensureRemoteInfra()
	if a.skillExecutor == nil {
		return fmt.Errorf("skill executor not initialized")
	}
	return a.skillExecutor.BackupSkills(outputPath)
}

// ExportLearnedSkillsZip exports selected learned/crafted skills to a zip file.
// It opens a save dialog for the user to choose the output path.
func (a *App) ExportLearnedSkillsZip(names []string) error {
	a.ensureRemoteInfra()
	if a.skillExecutor == nil {
		return fmt.Errorf("skill executor not initialized")
	}
	dest, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           "Export Learned Skills",
		DefaultFilename: "learned-skills.zip",
		Filters: []runtime.FileFilter{
			{DisplayName: "Zip Files", Pattern: "*.zip"},
		},
	})
	if err != nil || dest == "" {
		return nil // user cancelled
	}
	return a.skillExecutor.ExportLearnedSkillsZip(names, dest)
}

// ImportLearnedSkillsZip opens a file dialog to select a zip, imports
// learned/crafted skills from it, and returns a RestoreReport with
// duplicate-skip information.
func (a *App) ImportLearnedSkillsZip() (*RestoreReport, error) {
	if err := a.ensureWorkflowAllowsRemoteToolCall("manage_skill", map[string]interface{}{"action": "import", "source": "zip", "learned": true}); err != nil {
		return nil, err
	}
	a.ensureRemoteInfra()
	if a.skillExecutor == nil {
		return nil, fmt.Errorf("skill executor not initialized")
	}
	selection := a.SelectSkillFile()
	if selection == "" {
		return nil, nil // user cancelled
	}
	return a.skillExecutor.RestoreSkills(selection)
}

// RestoreSkills imports NL Skills from a zip file (Wails binding).
func (a *App) RestoreSkills(zipPath string) (*RestoreReport, error) {
	if err := a.ensureWorkflowAllowsRemoteToolCall("manage_skill", map[string]interface{}{"action": "restore", "source": "zip", "path": zipPath}); err != nil {
		return nil, err
	}
	a.ensureRemoteInfra()
	if a.skillExecutor == nil {
		return nil, fmt.Errorf("skill executor not initialized")
	}
	return a.skillExecutor.RestoreSkills(zipPath)
}

// QueryAuditLog queries the audit log with the given filter (Wails binding).
func (a *App) QueryAuditLog(filter security.AuditFilter) ([]security.AuditEntry, error) {
	a.ensureAuditLog()
	if a.auditLog == nil {
		return nil, fmt.Errorf("audit log not initialized")
	}
	return a.auditLog.Query(filter)
}

// SecurityEventItem is a frontend-friendly representation of a denied audit entry.
type SecurityEventItem struct {
	Time      string `json:"time"`
	ToolName  string `json:"tool_name"`
	Target    string `json:"target"`
	RemoteIP  string `json:"remote_ip"`
	RiskLevel string `json:"risk_level"`
	Reason    string `json:"reason"`
}

// QuerySecurityEvents returns denied/rejected security events from the last N days (Wails binding).
func (a *App) QuerySecurityEvents(days int) ([]SecurityEventItem, error) {
	a.ensureAuditLog()
	if a.auditLog == nil {
		return nil, fmt.Errorf("audit log not initialized")
	}
	if days <= 0 {
		days = 7
	}
	start := time.Now().AddDate(0, 0, -days)
	entries, err := a.auditLog.Query(security.AuditFilter{StartTime: &start})
	if err != nil {
		return nil, err
	}
	var items []SecurityEventItem
	for _, e := range entries {
		if e.PolicyAction != security.PolicyDeny && !isDeniedResult(e.Result) {
			continue
		}
		items = append(items, SecurityEventItem{
			Time:      e.Timestamp.Format("2006-01-02 15:04:05"),
			ToolName:  e.ToolName,
			Target:    extractTarget(e.Arguments),
			RemoteIP:  extractRemoteIP(e.Arguments, e.SessionID),
			RiskLevel: string(e.RiskLevel),
			Reason:    formatDenyReason(e),
		})
	}
	// Reverse so newest events appear first.
	for i, j := 0, len(items)-1; i < j; i, j = i+1, j-1 {
		items[i], items[j] = items[j], items[i]
	}
	return items, nil
}

func isDeniedResult(result string) bool {
	return classifySecurityAuditResult(result).IsDenied()
}

// formatDenyReason produces a human-readable reason from an AuditEntry.
// If Result already contains a descriptive message (e.g. "rejected skill X: critical risk"),
// use it directly. Otherwise fall back to a generic label based on PolicyAction + RiskLevel.
func formatDenyReason(e security.AuditEntry) string {
	r := e.Result
	if r != "" && r != "deny" && r != "denied" && r != string(security.PolicyDeny) {
		return r
	}
	// Generic fallback
	return fmt.Sprintf("policy_deny (risk: %s)", e.RiskLevel)
}

func extractTarget(args map[string]interface{}) string {
	if len(args) == 0 {
		return "-"
	}
	for _, key := range []string{"path", "file", "url", "filepath"} {
		if v, ok := args[key]; ok {
			if s, ok := v.(string); ok && s != "" {
				return s
			}
		}
	}
	if cmd, ok := args["command"]; ok {
		if s, ok := cmd.(string); ok && s != "" {
			if len(s) > 80 {
				return s[:80] + "..."
			}
			return s
		}
	}
	return "-"
}

func extractRemoteIP(args map[string]interface{}, sessionID string) string {
	if len(args) == 0 {
		return "-"
	}
	for _, key := range []string{"remote_ip", "ip", "host"} {
		if v, ok := args[key]; ok {
			if s, ok := v.(string); ok && s != "" {
				return s
			}
		}
	}
	return "-"
}

// RecommendTool suggests the best programming tool for a task (Wails binding).
func (a *App) RecommendTool(taskDescription string) (string, string) {
	a.ensureRemoteInfra()
	if a.toolSelector == nil {
		return "", "tool selector not initialized"
	}
	// Get installed tools by checking which known tools have their binary available.
	var installed []string
	for _, tool := range []string{"claude", "codex", "opencode", "iflow", "kilo"} {
		meta, ok := remoteToolCatalog[tool]
		if !ok {
			continue
		}
		if _, err := exec.LookPath(meta.BinaryName); err == nil {
			installed = append(installed, tool)
		}
	}
	return a.toolSelector.Recommend(taskDescription, installed)
}

// SearchSkillHub searches configured SkillHubs for Skills matching the query (Wails binding).
func (a *App) SearchSkillHub(query string) ([]HubSkillMeta, error) {
	hubURL := NewSkillMarketClient(a).baseURL()
	if ok, reason := a.enforceHubSecurityAppPolicy("manage_skill", map[string]interface{}{"action": "search", "query": query, "source": "skillhub", "hub_url": hubURL}); !ok {
		return nil, fmt.Errorf("%s", reason)
	}
	a.ensureSkillHubClient()
	if a.skillHubClient == nil {
		return nil, fmt.Errorf("skill hub client not initialized")
	}
	return a.skillHubClient.Search(context.Background(), query)
}

// SearchMixedSkills searches SkillMarket, ClawHub mirror, and GitHub for Skills matching the query (Wails binding).
func (a *App) SearchMixedSkills(query string) ([]MixedSkillSearchResult, error) {
	a.ensureInteractionInfra()
	searcher := NewSkillSearcher(NewSkillMarketClient(a))
	return searcher.SearchAll(context.Background(), query)
}

// InstallMixedSkill installs a skill result from mixed search sources (Wails binding).
func (a *App) InstallMixedSkill(source, id, installRef string) error {
	return a.installMixedSkillWithIntegrity(source, id, installRef, "", "")
}

func (a *App) installMixedSkillWithIntegrity(source, id, installRef, expectedPackageSHA256, expectedPackageSignature string) error {
	return a.installMixedSkillWithIntegrityAndLocator(source, id, installRef, "", expectedPackageSHA256, expectedPackageSignature)
}

func (a *App) installMixedSkillWithIntegrityAndLocator(source, id, installRef, downloadLocator, expectedPackageSHA256, expectedPackageSignature string) error {
	_, err := a.installMixedSkillWithIntegrityAndLocatorTrace(source, id, installRef, downloadLocator, expectedPackageSHA256, expectedPackageSignature)
	return err
}

func (a *App) installMixedSkillWithIntegrityAndLocatorTrace(source, id, installRef, downloadLocator, expectedPackageSHA256, expectedPackageSignature string) (skillHubDownloadTrace, error) {
	var trace skillHubDownloadTrace
	if err := a.ensureWorkflowAllowsRemoteToolCall("manage_skill", map[string]interface{}{"action": "install", "source": source, "skill_id": id, "install_ref": installRef}); err != nil {
		return trace, err
	}
	a.ensureInteractionInfra()
	if a.skillExecutor == nil {
		return trace, fmt.Errorf("skill executor not initialized")
	}
	guardArgs := map[string]interface{}{"action": "install", "source": source, "skill_id": id, "install_ref": installRef}
	switch skillSearchSourceFromStatus(source) {
	case skillSearchSourceSkillMarket, skillSearchSourceSkillHub:
		guardArgs["hub_url"] = NewSkillMarketClient(a).baseURL()
	case skillSearchSourceClawHub:
		guardArgs["hub_url"] = cskill.ClawHubMirrorURL
	case skillSearchSourceGitHub:
		guardArgs["hub_url"] = "github"
	case skillSearchSourceEnterpriseHub:
		if cfg, err := a.LoadConfig(); err == nil {
			guardArgs["hub_url"] = cfg.RemoteHubURL
		}
	}
	if ok, reason := a.enforceHubSecurityAppPolicy("manage_skill", guardArgs); !ok {
		return trace, fmt.Errorf("%s", reason)
	}
	// Invalidate update cache - installed skill set changed.
	defer func() {
		if a.hubUpdCache != nil {
			a.hubUpdCache.invalidate()
		}
	}()
	ctx := context.Background()
	kind := skillSearchSourceFromStatus(source)
	if cfg, err := a.LoadConfig(); err == nil {
		if reason, blocked := cfg.CapabilityMarketPolicy.RejectNonEnterpriseInstall(string(kind), cfg.RemoteHubURL); blocked {
			return trace, fmt.Errorf("%s", reason)
		}
	}
	switch kind {
	case skillSearchSourceEnterpriseHub:
		ref := strings.TrimSpace(firstNonEmpty(installRef, id))
		if ref == "" {
			return trace, fmt.Errorf("enterprise capability id is required")
		}
		status := a.InstallHubCapability(ref)
		if len(status.Errors) > 0 {
			return trace, fmt.Errorf("%s", strings.Join(status.Errors, "; "))
		}
		return trace, nil
	case skillSearchSourceSkillMarket, skillSearchSourceSkillHub:
		// Prefer installRef (HubSkillID from enriched App packages) for
		// deterministic download. Fall back to bare id (name-based) for
		// backward compatibility with pre-enrichment packages.
		downloadID := strings.TrimSpace(firstNonEmpty(installRef, id))
		stagingRoot, err := a.skillStagingDir()
		if err != nil {
			return trace, err
		}
		stagingDir, err := cskill.PrepareStagingDirInRoot(stagingRoot, firstNonEmpty(id, "mixed-skill"))
		if err != nil {
			return trace, err
		}
		skill, downloadTrace, err := downloadSkillJSONFromHubCenterLocatorToDirWithIntegrityTrace(ctx, a, downloadLocator, "/api/v1/skills/"+url.PathEscape(downloadID)+"/download", stagingDir, expectedPackageSHA256, expectedPackageSignature)
		trace = downloadTrace
		if err != nil {
			cskill.CleanupStaging(stagingDir)
			return trace, annotateSkillHubDownloadError(err, trace)
		}
		if a.skillNameAlreadyRegistered(skill.Name) {
			cskill.CleanupStaging(stagingDir)
			return trace, fmt.Errorf("skill %q already exists", skill.Name)
		}
		skill.Source = maclawAppRegisteredDependencySource(string(kind))
		skill.HubSkillID = firstNonEmpty(downloadID, skill.HubSkillID)
		report, err := a.scanAndAdmitSkillBeforeRegister(ctx, skill, string(kind))
		if err != nil {
			cskill.CleanupStaging(stagingDir)
			return trace, err
		}
		committedDir := ""
		if strings.TrimSpace(skill.SkillDir) != "" {
			finalRoot, err := a.primarySkillsDir()
			if err != nil {
				cskill.CleanupStaging(stagingDir)
				return trace, err
			}
			finalDir, err := cskill.CommitStagingToDir(stagingDir, skill.Name, finalRoot)
			if err != nil {
				cskill.CleanupStaging(stagingDir)
				return trace, err
			}
			skill.SkillDir = finalDir
			committedDir = finalDir
		} else {
			cskill.CleanupStaging(stagingDir)
		}
		if err := writeSkillScanCacheForInstalledEntry(skill, report); err != nil {
			if committedDir != "" {
				_ = os.RemoveAll(committedDir)
			}
			return trace, fmt.Errorf("write skill scan cache: %w", err)
		}
		if err := a.skillExecutor.Register(*skill); err != nil {
			if committedDir != "" {
				_ = os.RemoveAll(committedDir)
			}
			return trace, err
		}
		a.emitSkillInstallProgress(skill.Name, "done", "Skill installed successfully.", report)
		return trace, nil

	case skillSearchSourceClawHub:
		skill, err := downloadClawHubSkill(ctx, id)
		if err != nil {
			return trace, err
		}
		if a.skillNameAlreadyRegistered(skill.Name) {
			return trace, fmt.Errorf("skill %q already exists", skill.Name)
		}
		report, err := a.scanAndAdmitSkillBeforeRegister(ctx, skill, "mixed clawhub search")
		if err != nil {
			return trace, err
		}
		if err := writeSkillScanCacheForInstalledEntry(skill, report); err != nil {
			return trace, fmt.Errorf("write skill scan cache: %w", err)
		}
		if err := a.skillExecutor.Register(*skill); err != nil {
			return trace, err
		}
		a.emitSkillInstallProgress(skill.Name, "done", "Skill installed successfully.", report)
		return trace, nil
	case skillSearchSourceGitHub:
		var candidate cskill.GitHubSkillCandidate
		if strings.TrimSpace(installRef) == "" {
			return trace, fmt.Errorf("missing github install ref")
		}
		if err := json.Unmarshal([]byte(installRef), &candidate); err != nil {
			return trace, fmt.Errorf("invalid github install ref: %w", err)
		}
		if strings.TrimSpace(candidate.RawURL) == "" {
			return trace, fmt.Errorf("invalid github install ref: missing raw_url")
		}
		if strings.TrimSpace(candidate.RepoFullName) == "" {
			candidate.RepoFullName = id
		}
		skill, err := cskill.NewGitHubSearcher("").ImportFromCandidate(candidate)
		if err != nil {
			return trace, err
		}
		if a.skillNameAlreadyRegistered(skill.Name) {
			return trace, fmt.Errorf("skill %q already exists", skill.Name)
		}
		report, err := a.scanAndAdmitSkillBeforeRegister(ctx, skill, "mixed github search")
		if err != nil {
			return trace, err
		}
		if err := writeSkillScanCacheForInstalledEntry(skill, report); err != nil {
			return trace, fmt.Errorf("write skill scan cache: %w", err)
		}
		if err := a.skillExecutor.Register(*skill); err != nil {
			return trace, err
		}
		a.emitSkillInstallProgress(skill.Name, "done", "Skill installed successfully.", report)
		return trace, nil
	default:
		return trace, fmt.Errorf("unsupported skill source %q", source)
	}
}

func annotateSkillHubDownloadError(err error, trace skillHubDownloadTrace) error {
	if err == nil {
		return nil
	}
	detail := strings.TrimSpace(err.Error())
	if strings.Contains(detail, "download_node=") || strings.Contains(detail, "preferred_node=") {
		return err
	}
	var parts []string
	if node := strings.TrimSpace(trace.UsedBase); node != "" {
		parts = append(parts, "download_node="+node)
	} else if preferred := skillHubPreferredNodeFromLocator(trace.PreferredLocator); preferred != "" {
		parts = append(parts, "preferred_node="+preferred)
	}
	if resolved := strings.TrimSpace(trace.ResolvedDownloadURL); resolved != "" {
		parts = append(parts, "resolved_url="+resolved)
	}
	if len(trace.Candidates) > 0 {
		cands := trace.Candidates
		if len(cands) > 6 {
			cands = cands[:6]
		}
		parts = append(parts, "candidates="+strings.Join(cands, ","))
	}
	if len(parts) == 0 {
		return err
	}
	return fmt.Errorf("%v; %s", err, strings.Join(parts, " "))
}

func skillHubPreferredNodeFromLocator(locator string) string {
	locator = strings.TrimSpace(locator)
	if locator == "" {
		return ""
	}
	parsed, err := url.Parse(locator)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	return strings.TrimRight(parsed.Scheme+"://"+parsed.Host, "/")
}

// InstallHubSkill downloads a Skill from the specified Hub and registers it locally (Wails binding).
func (a *App) InstallHubSkill(skillID, hubURL string) error {
	if err := a.ensureWorkflowAllowsRemoteToolCall("manage_skill", map[string]interface{}{"action": "install", "source": "skillhub", "skill_id": skillID, "hub_url": hubURL}); err != nil {
		return err
	}
	if ok, reason := a.enforceHubSecurityAppPolicy("manage_skill", map[string]interface{}{"action": "install", "source": "skillhub", "skill_id": skillID, "hub_url": hubURL}); !ok {
		return fmt.Errorf("%s", reason)
	}
	a.ensureSkillHubClient()
	if a.skillHubClient == nil {
		return fmt.Errorf("skill hub client not initialized")
	}
	if a.skillExecutor == nil {
		return fmt.Errorf("skill executor not initialized")
	}
	ctx := context.Background()
	stagingDir, err := cskill.PrepareStagingDir(firstNonEmpty(skillID, "hub-skill"))
	if err != nil {
		return err
	}
	entry, err := a.skillHubClient.InstallToDir(ctx, skillID, hubURL, stagingDir)
	if err != nil {
		cskill.CleanupStaging(stagingDir)
		return err
	}
	if a.skillNameAlreadyRegistered(entry.Name) {
		cskill.CleanupStaging(stagingDir)
		return fmt.Errorf("skill %q already exists", entry.Name)
	}
	report, err := a.scanAndAdmitSkillBeforeRegister(ctx, entry, "skillhub")
	if err != nil {
		cskill.CleanupStaging(stagingDir)
		return err
	}
	finalDir, err := cskill.CommitStaging(stagingDir, entry.Name)
	if err != nil {
		cskill.CleanupStaging(stagingDir)
		return err
	}
	rewriteSkillStepWorkingDir(entry, finalDir)
	entry.SkillDir = finalDir
	if err := writeSkillScanCacheForInstalledEntry(entry, report); err != nil {
		_ = os.RemoveAll(finalDir)
		return fmt.Errorf("write skill scan cache: %w", err)
	}
	if err := a.skillExecutor.Register(*entry); err != nil {
		_ = os.RemoveAll(finalDir)
		return err
	}
	a.emitSkillInstallProgress(entry.Name, "done", "Skill installed successfully.", report)
	// Auto-install dependencies for file-backed skills (e.g. npm install, pip install).
	go a.installSkillDepsIfMissing(entry.SkillDir, entry.Name)
	return nil
}

func rewriteSkillStepWorkingDir(entry *corelib.NLSkillEntry, finalDir string) {
	if entry == nil || strings.TrimSpace(finalDir) == "" {
		return
	}
	oldDir := strings.TrimSpace(entry.SkillDir)
	for i := range entry.Steps {
		if entry.Steps[i].Params == nil {
			continue
		}
		if workingDir, ok := entry.Steps[i].Params["working_dir"].(string); ok {
			if strings.TrimSpace(workingDir) == "" || oldDir == "" || filepath.Clean(workingDir) == filepath.Clean(oldDir) {
				entry.Steps[i].Params["working_dir"] = finalDir
			}
		}
	}
}

// installSkillDepsIfMissing checks for package.json / requirements.txt in the
// skill directory and runs npm install or pip install if node_modules or
// __pycache__ is missing. Runs asynchronously to avoid blocking the install call.
func (a *App) installSkillDepsIfMissing(skillDir, skillName string) {
	if skillDir == "" {
		return
	}
	// Check if skill directory exists.
	if _, err := os.Stat(skillDir); err != nil {
		return
	}

	pkgJSON := filepath.Join(skillDir, "package.json")
	reqTxt := filepath.Join(skillDir, "requirements.txt")
	nodeModules := filepath.Join(skillDir, "node_modules")

	if _, err := os.Stat(pkgJSON); err == nil {
		// Has package.json - check if node_modules exists.
		if _, err := os.Stat(nodeModules); os.IsNotExist(err) {
			log.Printf("[skill-deps] installing npm dependencies for %s...", skillName)
			ctx, cancel := context.WithTimeout(context.Background(), time.Duration(corelib.DefaultAgentTimeoutSec)*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, "npm", "install", "--production")
			cmd.Dir = skillDir
			hideCommandWindow(cmd)
			output, err := cmd.CombinedOutput()
			if err != nil {
				log.Printf("[skill-deps] npm install failed for %s: %v\n%s", skillName, err, output)
			} else {
				log.Printf("[skill-deps] npm install completed for %s", skillName)
			}
		}
	}
	if _, err := os.Stat(reqTxt); err == nil {
		// Has requirements.txt - run pip install.
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(corelib.DefaultAgentTimeoutSec)*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "pip", "install", "-r", reqTxt)
		cmd.Dir = skillDir
		hideCommandWindow(cmd)
		output, err := cmd.CombinedOutput()
		if err != nil {
			log.Printf("[skill-deps] pip install failed for %s: %v\n%s", skillName, err, output)
		} else {
			log.Printf("[skill-deps] pip install completed for %s", skillName)
		}
	}
}

// CheckHubSkillUpdates checks all locally installed Hub Skills for available updates (Wails binding).
// Results are cached for 10 minutes. Within the TTL, returns cached data without HTTP requests.
// When cache is expired, fetches updates concurrently (max 3 parallel) instead of serially.
func (a *App) CheckHubSkillUpdates() ([]HubSkillUpdateInfo, error) {
	// Fast path: return cached results if still fresh.
	if a.hubUpdCache != nil {
		if cached, ok := a.hubUpdCache.getUpdates(); ok {
			return cached, nil
		}
	}

	a.ensureSkillHubClient()
	if a.skillHubClient == nil {
		return nil, fmt.Errorf("skill hub client not initialized")
	}
	if a.skillExecutor == nil {
		return nil, fmt.Errorf("skill executor not initialized")
	}

	skills := a.skillExecutor.loadSkills()
	var checks []hubSkillForUpdateCheck
	for _, s := range skills {
		if normalizeSkillEntrySource(s.Source) != skillEntrySourceHub || s.HubSkillID == "" {
			continue
		}
		checks = append(checks, hubSkillForUpdateCheck{
			name:       s.Name,
			hubSkillID: s.HubSkillID,
			hubVersion: s.HubVersion,
		})
	}

	// Concurrent fetch (max 3 parallel) instead of serial N+1.
	updates := fetchHubSkillUpdatesConcurrent(a.skillHubClient, checks)

	// Populate cache.
	if a.hubUpdCache != nil {
		a.hubUpdCache.set(updates)
	}

	return updates, nil
}

func (a *App) checkHubSkillUpdatesSafe() []HubSkillUpdateInfo {
	updates, err := a.CheckHubSkillUpdates()
	if err != nil {
		return nil
	}
	return updates
}

// UpdateHubSkill updates a locally installed Hub Skill to the latest version (Wails binding).
func (a *App) UpdateHubSkill(skillName string) error {
	if err := a.ensureWorkflowAllowsRemoteToolCall("manage_skill", map[string]interface{}{"action": "update", "name": skillName, "source": "skillhub"}); err != nil {
		return err
	}
	a.ensureRemoteInfra()
	if a.skillExecutor == nil {
		return fmt.Errorf("skill executor not initialized")
	}
	err := a.skillExecutor.UpdateFromHub(skillName)
	if err == nil && a.hubUpdCache != nil {
		a.hubUpdCache.invalidate()
	}
	return err
}

// RateHubSkill submits a rating for a Hub Skill (Wails binding).
func (a *App) RateHubSkill(skillID string, score int) error {
	a.ensureSkillHubClient()
	if a.skillHubClient == nil {
		return fmt.Errorf("skill hub client not initialized")
	}
	cfg, err := a.LoadConfig()
	if err != nil {
		return err
	}
	maclawID := cfg.RemoteMachineID
	if maclawID == "" {
		return fmt.Errorf("machine not registered")
	}
	return a.skillHubClient.Rate(context.Background(), skillID, maclawID, score)
}

// GetHubRecommendations returns the cached popular skills list for the Hub tab
// initial state (Wails binding). Returns data from the in-memory cache populated
// by the 24h RefreshRecommendations ticker - zero HTTP requests.
func (a *App) GetHubRecommendations() ([]MixedSkillSearchResult, error) {
	a.ensureSkillHubClient()
	if a.skillHubClient == nil {
		return nil, nil
	}
	recs := a.skillHubClient.GetRecommendations()
	if len(recs) == 0 {
		return nil, nil
	}
	results := make([]MixedSkillSearchResult, 0, len(recs))
	for _, r := range recs {
		results = append(results, MixedSkillSearchResult{
			ID:                           r.ID,
			Name:                         r.Name,
			Description:                  r.Description,
			Source:                       "skillhub",
			SourceLabel:                  mixedSourceLabel("skillhub"),
			TrustLevel:                   r.TrustLevel,
			Version:                      r.Version,
			Author:                       r.Author,
			AvgRating:                    r.AvgRating,
			RatingCount:                  r.RatingCount,
			Downloads:                    r.Downloads,
			ProductKind:                  r.ProductKind,
			IsMaclawApp:                  r.IsMaclawApp || strings.EqualFold(strings.TrimSpace(r.ProductKind), "maclaw_app_skill"),
			MaclawAppID:                  r.MaclawAppID,
			MaclawAppName:                r.MaclawAppName,
			MaclawAppDescription:         r.MaclawAppDescription,
			MaclawAppCategory:            r.MaclawAppCategory,
			MaclawAppIcon:                r.MaclawAppIcon,
			MaclawAppInputMode:           r.MaclawAppInputMode,
			MaclawAppOutputModes:         append([]string(nil), r.MaclawAppOutputModes...),
			MaclawAppDefinitionSHA256:    r.MaclawAppDefinitionSHA256,
			MaclawAppTestEvidence:        cloneSkillHubEvidenceForSearch(r.MaclawAppTestEvidence),
			ArtifactContractRequired:     r.ArtifactContractRequired,
			ArtifactContractOutputModes:  append([]string(nil), r.ArtifactContractOutputModes...),
			ArtifactContractPresentation: r.ArtifactContractPresentation,
		})
	}
	// Mark already-installed skills.
	if a.skillExecutor != nil {
		searcher := &SkillSearcher{app: a}
		searcher.enrichInstalledState(results)
	}
	return results, nil
}

func cloneSkillHubEvidenceForSearch(e *MaclawAppTestEvidence) *MaclawAppSearchEvidence {
	if e == nil {
		return nil
	}
	return &MaclawAppSearchEvidence{
		RunID:                 e.RunID,
		VerifiedAt:            e.VerifiedAt,
		DefinitionFingerprint: e.DefinitionFingerprint,
		ArtifactPresent:       e.ArtifactPresent,
		ArtifactName:          e.ArtifactName,
		OutputCount:           e.OutputCount,
		PrimaryResult:         e.PrimaryResult,
		ResultPayload:         cloneMapAny(e.ResultPayload),
	}
}

// ---------------------------------------------------------------------------
// Memory management Wails bindings
// ---------------------------------------------------------------------------

// ListMemories returns memory entries filtered by category and keyword (Wails binding).
func (a *App) ListMemories(category, keyword string) []memory.Entry {
	a.ensureInteractionInfra()
	if a.memoryStore == nil {
		return nil
	}
	return a.memoryStore.List(memory.Category(category), keyword)
}

// SaveMemory creates a new memory entry (Wails binding).
func (a *App) SaveMemory(content, category string, tags []string) error {
	if err := a.ensureWorkflowAllowsRemoteToolCall("memory", map[string]interface{}{"action": "save", "category": category, "tags": tags}); err != nil {
		return err
	}
	a.ensureInteractionInfra()
	if a.memoryStore == nil {
		return fmt.Errorf("memory store not initialized")
	}
	return a.memoryStore.SaveManualMemory(content, memory.Category(category), tags)
}

// UpdateMemory modifies an existing memory entry by ID (Wails binding).
func (a *App) UpdateMemory(id, content, category string, tags []string) error {
	if err := a.ensureWorkflowAllowsRemoteToolCall("memory", map[string]interface{}{"action": "save", "id": id, "category": category, "tags": tags}); err != nil {
		return err
	}
	a.ensureInteractionInfra()
	if a.memoryStore == nil {
		return fmt.Errorf("memory store not initialized")
	}
	return a.memoryStore.UpdateManualMemory(id, content, memory.Category(category), tags)
}

// DeleteMemory removes the memory entry with the given ID (Wails binding).
func (a *App) DeleteMemory(id string) error {
	if err := a.ensureWorkflowAllowsRemoteToolCall("memory", map[string]interface{}{"action": "delete", "id": id}); err != nil {
		return err
	}
	a.ensureInteractionInfra()
	if a.memoryStore == nil {
		return fmt.Errorf("memory store not initialized")
	}
	out := memory.HandleTool(a.memoryStore, map[string]interface{}{
		"action": "delete",
		"id":     id,
	}, memory.ToolOptions{})
	if strings.HasPrefix(out, "delete memory failed:") || strings.HasPrefix(out, "missing ") {
		return fmt.Errorf("%s", out)
	}
	return nil
}

// CompressMemories runs dedup + LLM compression once and returns a summary (Wails binding).
func (a *App) CompressMemories() (*memory.CompressResult, error) {
	if err := a.ensureWorkflowAllowsRemoteToolCall("memory", map[string]interface{}{"action": "save", "maintenance": "compress"}); err != nil {
		return nil, err
	}
	a.ensureInteractionInfra()
	if a.memoryStore == nil {
		return nil, fmt.Errorf("memory store not initialized")
	}
	maintenance := a.getOrCreateMemoryMaintenance()
	if maintenance == nil {
		return nil, fmt.Errorf("memory maintenance not initialized")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	result, err := maintenance.Compress(ctx)
	// Emit event so the frontend refreshes even if the component was
	// unmounted and remounted (tab switch) during compression.
	a.emitEvent("memory:compressed", result)
	return result, err
}

// ListMemoryBackups returns all available memory backup snapshots (Wails binding).
func (a *App) ListMemoryBackups() ([]MemoryBackupInfo, error) {
	a.ensureInteractionInfra()
	if a.memoryStore == nil {
		return nil, fmt.Errorf("memory store not initialized")
	}
	maintenance := a.getOrCreateMemoryMaintenance()
	if maintenance == nil {
		return nil, fmt.Errorf("memory maintenance not initialized")
	}
	return maintenance.ListBackups()
}

// RestoreMemoryBackup replaces the current memory with the named backup and
// takes effect immediately (Wails binding).
func (a *App) RestoreMemoryBackup(backupName string) error {
	if err := a.ensureWorkflowAllowsRemoteToolCall("memory", map[string]interface{}{"action": "save", "maintenance": "restore_backup", "backup_name": backupName}); err != nil {
		return err
	}
	a.ensureInteractionInfra()
	if a.memoryStore == nil {
		return fmt.Errorf("memory store not initialized")
	}
	maintenance := a.getOrCreateMemoryMaintenance()
	if maintenance == nil {
		return fmt.Errorf("memory maintenance not initialized")
	}
	return maintenance.RestoreBackup(backupName)
}

// DeleteMemoryBackup removes a backup file by name (Wails binding).
func (a *App) DeleteMemoryBackup(backupName string) error {
	if err := a.ensureWorkflowAllowsRemoteToolCall("memory", map[string]interface{}{"action": "delete", "maintenance": "delete_backup", "backup_name": backupName}); err != nil {
		return err
	}
	a.ensureInteractionInfra()
	if a.memoryStore == nil {
		return fmt.Errorf("memory store not initialized")
	}
	maintenance := a.getOrCreateMemoryMaintenance()
	if maintenance == nil {
		return fmt.Errorf("memory maintenance not initialized")
	}
	return maintenance.DeleteBackup(backupName)
}

// SetAutoCompress enables or disables the background auto-compression service (Wails binding).
func (a *App) SetAutoCompress(enabled bool) error {
	if err := a.ensureWorkflowAllowsRemoteToolCall("memory", map[string]interface{}{"action": "save", "maintenance": "set_auto_compress", "enabled": enabled}); err != nil {
		return err
	}
	a.ensureInteractionInfra()
	if a.memoryStore == nil {
		return fmt.Errorf("memory store not initialized")
	}
	maintenance := a.getOrCreateMemoryMaintenance()
	if maintenance == nil {
		return fmt.Errorf("memory maintenance not initialized")
	}
	var compressorErr error
	if enabled {
		compressorErr = maintenance.StartCompressor()
	} else {
		compressorErr = maintenance.StopCompressor()
	}
	if compressorErr != nil {
		return compressorErr
	}
	// Persist to config without rewriting a stale full snapshot.
	return a.PatchConfig(func(cfg *corelib.AppConfig) {
		cfg.MemoryAutoCompress = enabled
	})
}

// GetAutoCompressStatus returns the current state of the auto-compression service (Wails binding).
func (a *App) GetAutoCompressStatus() MemoryCompressorStatus {
	a.ensureInteractionInfra()
	if a.memoryStore == nil {
		return MemoryCompressorStatus{}
	}
	maintenance := a.getOrCreateMemoryMaintenance()
	if maintenance == nil {
		return MemoryCompressorStatus{}
	}
	status, err := maintenance.CompressorStatus()
	if err != nil {
		return MemoryCompressorStatus{}
	}
	return status
}

// IsMemoryCompressing returns whether a compression operation is currently in progress (Wails binding).
func (a *App) IsMemoryCompressing() bool {
	a.ensureInteractionInfra()
	if a.memoryStore == nil {
		return false
	}
	maintenance := a.getOrCreateMemoryMaintenance()
	if maintenance == nil {
		return false
	}
	compressing, err := maintenance.IsCompressing()
	if err != nil {
		return false
	}
	return compressing
}

// GetMemoryHealth returns an aggregated health report of the memory system (Wails binding).
func (a *App) GetMemoryHealth() *memory.HealthReport {
	a.ensureInteractionInfra()
	if a.memoryStore == nil {
		return &memory.HealthReport{}
	}
	return a.memoryStore.HealthReport()
}

// MemoryStatusData holds the structured memory status for the frontend pie chart.
type MemoryStatusData struct {
	TotalEntries    int                  `json:"total_entries"`
	MaxCapacity     int                  `json:"max_capacity"`
	CapacityPercent float64              `json:"capacity_percent"`
	ArchivedEntries int                  `json:"archived_entries"`
	StaleEntries    int                  `json:"stale_entries"`
	PinnedEntries   int                  `json:"pinned_entries"`
	EmbedderActive  bool                 `json:"embedder_active"`
	NoEmbedding     int                  `json:"no_embedding"`
	OldestEntry     string               `json:"oldest_entry,omitempty"`
	NewestEntry     string               `json:"newest_entry,omitempty"`
	Categories      []MemoryStatusCatRow `json:"categories"`
}

// MemoryStatusCatRow is one row in the category breakdown.
type MemoryStatusCatRow struct {
	Category string  `json:"category"`
	Count    int     `json:"count"`
	Percent  float64 `json:"percent"`
}

// GetMemoryStatus returns structured memory status data for the frontend
// pie chart and the /memory slash command (Wails binding).
func (a *App) GetMemoryStatus() *MemoryStatusData {
	a.ensureInteractionInfra()
	if a.memoryStore == nil {
		a.ensureMemoryStore()
	}
	if a.memoryStore == nil {
		return &MemoryStatusData{MaxCapacity: 2000, Categories: []MemoryStatusCatRow{}}
	}
	hr := a.memoryStore.HealthReport()
	data := &MemoryStatusData{
		TotalEntries:    hr.ActiveEntries,
		MaxCapacity:     hr.MaxCapacity,
		CapacityPercent: hr.CapacityPercent,
		ArchivedEntries: hr.ArchivedEntries,
		StaleEntries:    hr.StaleEntries,
		PinnedEntries:   hr.PinnedEntries,
		EmbedderActive:  hr.EmbedderActive,
		NoEmbedding:     hr.NoEmbedding,
		OldestEntry:     hr.OldestEntry,
		NewestEntry:     hr.NewestEntry,
	}

	// Build category rows from HealthReport.CategoryCounts (includes ALL categories).
	total := hr.ActiveEntries
	if total == 0 {
		total = 1 // avoid division by zero
	}
	for cat, count := range hr.CategoryCounts {
		data.Categories = append(data.Categories, MemoryStatusCatRow{
			Category: cat,
			Count:    count,
			Percent:  float64(count) / float64(total) * 100,
		})
	}
	// Sort by count descending for consistent display.
	sortCatRows(data.Categories)
	return data
}

func sortCatRows(rows []MemoryStatusCatRow) {
	for i := 1; i < len(rows); i++ {
		for j := i; j > 0 && rows[j].Count > rows[j-1].Count; j-- {
			rows[j], rows[j-1] = rows[j-1], rows[j]
		}
	}
}

// Inference diagnostics payloads are owned by corelib/memory so GUI/TUI/server
// hosts share the same projection of the long-term-memory inference layer.
type InferenceDiagnosticsData = memory.InferenceDiagnosticsData
type InferenceDiagnosticsRule = memory.InferenceDiagnosticsRule
type InferenceDiagnosticsFact = memory.InferenceDiagnosticsFact

// GetInferenceDiagnostics returns multi-hop reasoning engine diagnostics (Wails binding).
func (a *App) GetInferenceDiagnostics() *InferenceDiagnosticsData {
	a.ensureInteractionInfra()
	if a.memoryStore == nil {
		return &InferenceDiagnosticsData{}
	}
	return a.memoryStore.InferenceDiagnosticsForHost()
}

// TestInference runs the inference engine on a query and returns derived facts (Wails binding).
func (a *App) TestInference(query string) []InferenceDiagnosticsFact {
	a.ensureInteractionInfra()
	if a.memoryStore == nil || query == "" {
		return nil
	}
	projectPath := ""
	if a.contextResolver != nil {
		projectPath, _ = a.contextResolver.ResolveProject()
	}
	return a.memoryStore.TestInferenceForHost(query, memory.InferenceOptions{
		ProjectPath:     projectPath,
		MaxDerived:      20,
		MinConfidence:   0.40,
		MaxVisitedFacts: 200,
	})
}

// GetMemoryMaxBackups returns the configured max backup retention count (Wails binding).
func (a *App) GetMemoryMaxBackups() int {
	cfg, err := a.LoadConfig()
	if err != nil || cfg.MemoryMaxBackups <= 0 {
		return memory.DefaultMaxBackups
	}
	if cfg.MemoryMaxBackups < memory.MinBackups {
		return memory.MinBackups
	}
	return cfg.MemoryMaxBackups
}

// SetMemoryMaxBackups updates the max backup retention count and persists it (Wails binding).
func (a *App) SetMemoryMaxBackups(n int) error {
	if err := a.ensureWorkflowAllowsRemoteToolCall("memory", map[string]interface{}{"action": "save", "maintenance": "set_max_backups", "max_backups": n}); err != nil {
		return err
	}
	if n < memory.MinBackups {
		n = memory.MinBackups
	}
	if err := a.PatchConfig(func(cfg *corelib.AppConfig) {
		cfg.MemoryMaxBackups = n
	}); err != nil {
		return err
	}
	if a.memoryStore == nil {
		a.ensureMemoryStore()
	}
	if a.memoryStore == nil {
		return nil
	}
	maintenance := a.getOrCreateMemoryMaintenance()
	if maintenance == nil {
		return nil
	}
	return maintenance.SetMaxBackups(n)
}

// ListArchiveMemories returns archived (cold storage) entries filtered by category and keyword (Wails binding).
func (a *App) ListArchiveMemories(category, keyword string) []memory.Entry {
	a.ensureInteractionInfra()
	if a.memoryStore == nil {
		return nil
	}
	return a.memoryStore.ListArchive(memory.Category(category), keyword)
}

// RestoreArchiveMemory moves an archived entry back to active memory (Wails binding).
func (a *App) RestoreArchiveMemory(id string) error {
	if err := a.ensureWorkflowAllowsRemoteToolCall("memory", map[string]interface{}{"action": "save", "maintenance": "restore_archive", "id": id}); err != nil {
		return err
	}
	a.ensureInteractionInfra()
	if a.memoryStore == nil {
		return fmt.Errorf("memory store not initialized")
	}
	return a.memoryStore.RestoreFromArchive(id)
}

// PinMemory marks a memory entry as pinned (protected from eviction) (Wails binding).
func (a *App) PinMemory(id string) error {
	if err := a.ensureWorkflowAllowsRemoteToolCall("memory", map[string]interface{}{"action": "save", "maintenance": "pin", "id": id}); err != nil {
		return err
	}
	a.ensureInteractionInfra()
	if a.memoryStore == nil {
		return fmt.Errorf("memory store not initialized")
	}
	return a.memoryStore.PinEntry(id)
}

// UnpinMemory removes the pinned flag from a memory entry (Wails binding).
func (a *App) UnpinMemory(id string) error {
	if err := a.ensureWorkflowAllowsRemoteToolCall("memory", map[string]interface{}{"action": "save", "maintenance": "unpin", "id": id}); err != nil {
		return err
	}
	a.ensureInteractionInfra()
	if a.memoryStore == nil {
		return fmt.Errorf("memory store not initialized")
	}
	return a.memoryStore.UnpinEntry(id)
}

// ---------------------------------------------------------------------------
// Session history Wails bindings (SQLite FTS5 full-text search)
// ---------------------------------------------------------------------------

// ensureSessionStore lazily initializes the session search store.
func (a *App) ensureSessionStore() {
	a.sessionStoreMu.Do(func() {
		dbPath := a.sessionSearchDBPath()
		store, err := session.NewStore(dbPath)
		if err != nil {
			log.Printf("[session_history] failed to open store: %v", err)
			return
		}
		a.sessionSearchStore = store
	})
}

// ListSessionHistory returns the most recent session summaries (Wails binding).
func (a *App) ListSessionHistory(limit int) []session.SessionSummary {
	a.ensureSessionStore()
	if a.sessionSearchStore == nil {
		return nil
	}
	results, err := a.sessionSearchStore.ListRecent(limit)
	if err != nil {
		log.Printf("[session_history] list recent: %v", err)
		return nil
	}
	return results
}

// SearchSessionHistory performs FTS5 full-text search across session transcripts (Wails binding).
func (a *App) SearchSessionHistory(query string, maxResults int) []session.SearchResult {
	a.ensureSessionStore()
	if a.sessionSearchStore == nil {
		return nil
	}
	results, err := a.sessionSearchStore.Search(query, maxResults)
	if err != nil {
		log.Printf("[session_history] search: %v", err)
		return nil
	}
	return results
}

// GetSessionFullText returns the full transcript for a session ID (Wails binding).
func (a *App) GetSessionFullText(sessionID string) string {
	a.ensureSessionStore()
	if a.sessionSearchStore == nil {
		return ""
	}
	text, err := a.sessionSearchStore.GetFullText(sessionID)
	if err != nil {
		log.Printf("[session_history] get full text: %v", err)
		return ""
	}
	return text
}

// DeleteSession removes a session from the history (Wails binding).
func (a *App) DeleteSession(sessionID string) error {
	a.ensureSessionStore()
	if a.sessionSearchStore == nil {
		return fmt.Errorf("session store not initialized")
	}
	return a.sessionSearchStore.Delete(sessionID)
}

// GetSessionCount returns the total number of stored sessions (Wails binding).
func (a *App) GetSessionCount() int {
	a.ensureSessionStore()
	if a.sessionSearchStore == nil {
		return 0
	}
	n, err := a.sessionSearchStore.Count()
	if err != nil {
		return 0
	}
	return n
}

// getOrCreateMemoryMaintenance returns the singleton corelib memory maintenance facade,
// creating it if needed.
func (a *App) getOrCreateMemoryMaintenance() *memory.Maintenance {
	if a == nil || a.memoryStore == nil {
		return nil
	}
	_ = a.getOrCreateCompressor()
	return a.memoryMaintenance
}

// getOrCreateCompressor returns the singleton MemoryCompressor, creating it if needed.
func (a *App) getOrCreateCompressor() *MemoryCompressor {
	if a == nil || a.memoryStore == nil {
		return nil
	}
	a.compressorMu.Lock()
	needsCreate := a.memoryCompressor == nil || a.memoryMaintenance == nil
	oldCompressor := a.memoryCompressor
	a.compressorMu.Unlock()

	if needsCreate {
		compressor := a.newMemoryCompressor(a.memoryStore)
		a.compressorMu.Lock()
		a.memoryCompressor = compressor
		a.compressorMu.Unlock()
		if oldCompressor != nil && oldCompressor != compressor {
			oldCompressor.Stop()
		}
	}

	a.compressorMu.Lock()
	defer a.compressorMu.Unlock()
	return a.memoryCompressor
}

// ---------------------------------------------------------------------------
// Session template Wails bindings
// ---------------------------------------------------------------------------

// ListTemplates returns all session templates (Wails binding).
func (a *App) ListTemplates() []remote.SessionTemplate {
	a.ensureTemplateManager()
	if a.templateManager == nil {
		return nil
	}
	return a.templateManager.List()
}

// CreateTemplate creates a new session template (Wails binding).
func (a *App) CreateTemplate(name, tool, projectPath, modelConfig string, yoloMode bool) error {
	projectPath = normalizeProjectSessionPath(projectPath)
	policyOwnerID := a.defaultManualPolicyOwnerID()
	if projectPath != "" {
		policyOwnerID = projectSessionOwnerID(projectPath)
	}
	if err := a.ensureWorkflowAllowsRemoteToolCallForOwner(policyOwnerID, remoteTemplatePolicyToolName, map[string]interface{}{"action": "create_template", "name": name, "tool": tool, "project_path": projectPath}); err != nil {
		return err
	}
	a.ensureTemplateManager()
	if a.templateManager == nil {
		return fmt.Errorf("template manager not initialized")
	}
	return a.templateManager.Create(remote.SessionTemplate{
		Name:        name,
		Tool:        tool,
		ProjectPath: projectPath,
		ModelConfig: modelConfig,
		YoloMode:    yoloMode,
	})
}

// DeleteTemplate removes the session template with the given name (Wails binding).
func (a *App) DeleteTemplate(name string) error {
	if err := a.ensureWorkflowAllowsRemoteToolCall(remoteTemplatePolicyToolName, map[string]interface{}{"action": "delete_template", "name": name}); err != nil {
		return err
	}
	a.ensureTemplateManager()
	if a.templateManager == nil {
		return fmt.Errorf("template manager not initialized")
	}
	return a.templateManager.Delete(name)
}

// ---------------------------------------------------------------------------
// Configuration management Wails bindings
// ---------------------------------------------------------------------------

// GetConfigSchema returns the configuration schema as JSON (Wails binding).
func (a *App) GetConfigSchema() (string, error) {
	a.ensureRemoteInfra()
	if a.configManager == nil {
		return "", fmt.Errorf("config manager not initialized")
	}
	return a.configManager.SchemaJSON()
}

// UpdateConfigBinding modifies a single configuration key and returns the old value (Wails binding).
func (a *App) UpdateConfigBinding(section, key, value string) (string, error) {
	a.ensureRemoteInfra()
	if a.configManager == nil {
		return "", fmt.Errorf("config manager not initialized")
	}
	return a.configManager.UpdateConfig(section, key, value)
}

// ---------------------------------------------------------------------------
// Scheduled task Wails bindings
// ---------------------------------------------------------------------------

// ListScheduledTasks returns all scheduled tasks (Wails binding).
func (a *App) ListScheduledTasks() []scheduler.ScheduledTask {
	a.ensureScheduledTaskManager()
	if a.scheduledTaskManager == nil {
		return nil
	}
	return a.scheduledTaskManager.List()
}

// CreateScheduledTask creates a new scheduled task (Wails binding).
func (a *App) CreateScheduledTask(name, action string, hour, minute, dayOfWeek, dayOfMonth, intervalMinutes int, startDate, endDate, taskType string) (string, error) {
	if err := a.ensureWorkflowAllowsRemoteToolCall("delegate_task", map[string]interface{}{"agent": "scheduled_task", "request": action, "task_name": name}); err != nil {
		return "", err
	}
	a.ensureScheduledTaskManager()
	if a.scheduledTaskManager == nil {
		return "", fmt.Errorf("scheduled task manager not initialized")
	}
	return a.scheduledTaskManager.Add(scheduler.ScheduledTask{
		Name:            name,
		Action:          action,
		Hour:            hour,
		Minute:          minute,
		DayOfWeek:       dayOfWeek,
		DayOfMonth:      dayOfMonth,
		IntervalMinutes: intervalMinutes,
		StartDate:       startDate,
		EndDate:         endDate,
		TaskType:        taskType,
	})
}

// UpdateScheduledTask modifies a scheduled task (Wails binding).
func (a *App) UpdateScheduledTask(id string, fields map[string]interface{}) error {
	if err := a.ensureWorkflowAllowsRemoteToolCall("delegate_task", map[string]interface{}{"agent": "scheduled_task", "task_id": id, "fields": fields}); err != nil {
		return err
	}
	a.ensureScheduledTaskManager()
	if a.scheduledTaskManager == nil {
		return fmt.Errorf("scheduled task manager not initialized")
	}
	return a.scheduledTaskManager.Update(id, fields)
}

// DeleteScheduledTask removes a scheduled task by ID (Wails binding).
func (a *App) DeleteScheduledTask(id string) error {
	if err := a.ensureWorkflowAllowsRemoteToolCall("delegate_task", map[string]interface{}{"agent": "scheduled_task", "task_id": strings.TrimSpace(id), "action": "delete"}); err != nil {
		return err
	}
	a.ensureScheduledTaskManager()
	if a.scheduledTaskManager == nil {
		return fmt.Errorf("scheduled task manager not initialized")
	}
	return a.scheduledTaskManager.Delete(id)
}

// PauseScheduledTask pauses a scheduled task (Wails binding).
func (a *App) PauseScheduledTask(id string) error {
	if err := a.ensureWorkflowAllowsRemoteToolCall("delegate_task", map[string]interface{}{"agent": "scheduled_task", "task_id": strings.TrimSpace(id), "action": "pause"}); err != nil {
		return err
	}
	a.ensureScheduledTaskManager()
	if a.scheduledTaskManager == nil {
		return fmt.Errorf("scheduled task manager not initialized")
	}
	return a.scheduledTaskManager.Pause(id)
}

// ResumeScheduledTask resumes a paused scheduled task (Wails binding).
func (a *App) ResumeScheduledTask(id string) error {
	if err := a.ensureWorkflowAllowsRemoteToolCall("delegate_task", map[string]interface{}{"agent": "scheduled_task", "task_id": strings.TrimSpace(id), "action": "resume"}); err != nil {
		return err
	}
	a.ensureScheduledTaskManager()
	if a.scheduledTaskManager == nil {
		return fmt.Errorf("scheduled task manager not initialized")
	}
	return a.scheduledTaskManager.Resume(id)
}

// TriggerScheduledTask immediately runs a scheduled task (Wails binding).
func (a *App) TriggerScheduledTask(id string) error {
	if err := a.ensureWorkflowAllowsRemoteToolCall("delegate_task", map[string]interface{}{"agent": "scheduled_task", "task_id": strings.TrimSpace(id)}); err != nil {
		return err
	}
	a.ensureScheduledTaskManager()
	if a.scheduledTaskManager == nil {
		return fmt.Errorf("scheduled task manager not initialized")
	}
	return a.scheduledTaskManager.TriggerNow(id)
}

// ---------------------------------------------------------------------------
// AI Assistant Wails bindings
// ---------------------------------------------------------------------------

// IsAIAssistantReady returns true when the AI assistant backend is fully
// initialized and ready to handle messages without further chat-path setup.
func (a *App) IsAIAssistantReady() bool {
	hubClient := a.hubClient()
	if hubClient == nil {
		if !a.warmupDone.Load() {
			return false
		}
		hubClient = a.ensureHubClient()
		if hubClient == nil {
			return false
		}
	}
	if !a.warmupDone.Load() {
		return false
	}
	return hubClient.imHandler != nil && a.interactionInfraReady()
}

// GetAIAssistantInitStatus returns a human-readable initialization status
// string for the frontend to display during startup.
func (a *App) GetAIAssistantInitStatus() string {
	hubClient := a.hubClient()
	if hubClient == nil {
		// No Hub client yet. If warmup is done (markAIAssistantReady was called)
		// the system should still be usable in local/degraded mode. Create the
		// in-memory client/IM handler on demand instead of leaving the UI in a
		// forever-spinning "loading" state when machine credentials were cleared.
		if a.warmupDone.Load() {
			hubClient = a.ensureHubClient()
			if hubClient != nil && hubClient.imHandler != nil && a.interactionInfraReady() {
				return "ready"
			}
			return "degraded"
		}
		if a.interactionInfraReady() {
			return "loading"
		}
		return "connecting"
	}
	if !a.warmupDone.Load() {
		return "warming"
	}
	if hubClient.imHandler == nil || !a.interactionInfraReady() {
		return "loading"
	}
	return "ready"
}

func (a *App) GetAIAssistantTrace(runID string) (*AIAssistantTraceView, error) {
	a.ensureInteractionInfra()
	a.ensureAITrace()
	if a.aiTrace == nil {
		return nil, fmt.Errorf("AI trace service not initialized")
	}
	view, ok := a.aiTrace.GetTrace(runID)
	if !ok {
		return nil, fmt.Errorf("trace not found: %s", runID)
	}
	return &view, nil
}

type AIAssistantSendRequest struct {
	Text                        string                      `json:"text"`
	RequestID                   string                      `json:"request_id,omitempty"`
	RecentMessages              []AIAssistantContextMessage `json:"recent_messages,omitempty"`
	ResumeSlotID                string                      `json:"resume_slot_id,omitempty"`
	StartNewTask                bool                        `json:"start_new_task,omitempty"`
	DismissSlotID               string                      `json:"dismiss_slot_id,omitempty"`
	Lang                        string                      `json:"lang,omitempty"`
	ResumeSessionID             string                      `json:"resume_session_id,omitempty"`
	DismissRecoverableSessionID string                      `json:"dismiss_recoverable_session_id,omitempty"`
	UIAction                    bool                        `json:"ui_action,omitempty"`
	ProjectPath                 string                      `json:"project_path,omitempty"`
	EventScopeID                string                      `json:"event_scope_id,omitempty"`
}

type AIAssistantContextMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func sanitizeAIAssistantClientHistory(messages []AIAssistantContextMessage) []agent.ConversationEntry {
	if len(messages) == 0 {
		return nil
	}
	const maxClientHistoryMessages = 80
	start := 0
	if len(messages) > maxClientHistoryMessages {
		start = len(messages) - maxClientHistoryMessages
	}
	entries := make([]agent.ConversationEntry, 0, len(messages)-start)
	for _, msg := range messages[start:] {
		role := strings.ToLower(strings.TrimSpace(msg.Role))
		if role != "user" && role != "assistant" {
			continue
		}
		content := strings.TrimSpace(msg.Content)
		if content == "" {
			continue
		}
		entries = append(entries, agent.ConversationEntry{Role: role, Content: content})
	}
	return agent.DeduplicateAdjacentAssistantEntries(entries)
}

func conversationTextSuffixMatches(shorter, longer []agent.ConversationEntry) bool {
	if len(shorter) == 0 {
		return true
	}
	if len(shorter) > len(longer) {
		return false
	}
	offset := len(longer) - len(shorter)
	for i := range shorter {
		left := shorter[i]
		right := longer[offset+i]
		if strings.TrimSpace(left.Role) != strings.TrimSpace(right.Role) {
			return false
		}
		leftText, leftOK := left.Content.(string)
		rightText, rightOK := right.Content.(string)
		if !leftOK || !rightOK {
			return false
		}
		if strings.TrimSpace(leftText) != strings.TrimSpace(rightText) {
			return false
		}
	}
	return true
}

func (a *App) reconcileAIAssistantClientHistory(handler *IMMessageHandler, userID string, clientMessages []AIAssistantContextMessage) {
	if handler == nil || handler.memory == nil || len(clientMessages) == 0 {
		return
	}
	clientEntries := sanitizeAIAssistantClientHistory(clientMessages)
	if len(clientEntries) == 0 {
		return
	}
	existing := handler.memory.Load(userID)
	if len(existing) >= len(clientEntries) {
		return
	}
	if !conversationTextSuffixMatches(existing, clientEntries) {
		log.Printf("[AI assistant] client history reconciliation skipped: backendLen=%d clientLen=%d suffix=false", len(existing), len(clientEntries))
		return
	}
	handler.memory.Save(userID, clientEntries)
	log.Printf("[AI assistant] reconciled desktop conversation history from client transcript: %d->%d", len(existing), len(clientEntries))
}

type AIAssistantBackgroundTaskRequest struct {
	Text            string `json:"text"`
	ProjectPath     string `json:"project_path,omitempty"`
	ForceBackground bool   `json:"force_background,omitempty"`
}

type AIAssistantBackgroundTaskResult struct {
	Accepted  bool   `json:"accepted"`
	Mode      string `json:"mode"`
	SessionID string `json:"session_id,omitempty"`
	JobID     string `json:"job_id,omitempty"`
	RunID     string `json:"run_id,omitempty"`
	Error     string `json:"error,omitempty"`
}

type AIAssistantStreamEvent struct {
	RequestID   string `json:"request_id,omitempty"`
	Text        string `json:"text,omitempty"`
	SessionKey  string `json:"session_key,omitempty"` // userID for per-tab event routing
	DisplayText string `json:"display_text,omitempty"`
}

const (
	aiAssistantSnapshotDedupMinRunes   = 16
	aiAssistantSnapshotOverlapMinRunes = 24
)

type aiAssistantStreamDeltaNormalizer struct {
	content               string
	reasoning             string
	contentSnapshotMode   bool
	reasoningSnapshotMode bool
}

func (n *aiAssistantStreamDeltaNormalizer) Reset() {
	n.content = ""
	n.reasoning = ""
	n.contentSnapshotMode = false
	n.reasoningSnapshotMode = false
}

func (n *aiAssistantStreamDeltaNormalizer) Normalize(delta string) string {
	if delta == "" {
		return ""
	}
	if strings.HasPrefix(delta, "\x01") {
		next, snapshotMode := normalizeAIAssistantStreamDelta(n.reasoning, strings.TrimPrefix(delta, "\x01"), n.reasoningSnapshotMode)
		if next == "" {
			return ""
		}
		n.reasoning += next
		n.reasoningSnapshotMode = n.reasoningSnapshotMode || snapshotMode
		return "\x01" + next
	}
	next, snapshotMode := normalizeAIAssistantStreamDelta(n.content, delta, n.contentSnapshotMode)
	if next == "" {
		return ""
	}
	n.content += next
	n.contentSnapshotMode = n.contentSnapshotMode || snapshotMode
	return next
}

func normalizeAIAssistantStreamDelta(existing, incoming string, snapshotMode bool) (string, bool) {
	if existing == "" || incoming == "" {
		return incoming, false
	}
	existingLen := len([]rune(strings.TrimSpace(existing)))
	incomingLen := len([]rune(strings.TrimSpace(incoming)))
	if incoming == existing && incomingLen >= aiAssistantSnapshotDedupMinRunes {
		if snapshotMode {
			return "", true
		}
		return incoming, false
	}
	if strings.HasPrefix(incoming, existing) && existingLen >= aiAssistantSnapshotDedupMinRunes {
		return incoming[len(existing):], true
	}
	if strings.HasSuffix(existing, incoming) && incomingLen >= aiAssistantSnapshotOverlapMinRunes {
		if snapshotMode {
			return "", true
		}
		return incoming, false
	}
	existingRunes := []rune(existing)
	incomingRunes := []rune(incoming)
	maxOverlap := min(len(existingRunes), len(incomingRunes))
	for overlap := maxOverlap; overlap >= aiAssistantSnapshotOverlapMinRunes; overlap-- {
		if string(existingRunes[len(existingRunes)-overlap:]) == string(incomingRunes[:overlap]) {
			return string(incomingRunes[overlap:]), true
		}
	}
	return incoming, false
}

// SendAIAssistantMessage handles a desktop AI assistant message (Wails binding).
func (a *App) SendAIAssistantMessage(req AIAssistantSendRequest) (*IMAgentResponse, error) {
	a.ensureInteractionInfra()
	text := strings.TrimSpace(req.Text)
	if text == "" {
		return nil, fmt.Errorf("message text is required")
	}
	rawProjectPath := strings.TrimSpace(req.ProjectPath)
	projectPath := normalizeProjectSessionPath(req.ProjectPath)
	req.ProjectPath = projectPath
	if resp, handled, err := a.handleAgentViewControlMessage(text); handled {
		normalizeArtifactResponseSource(resp)
		return resp, err
	}
	if resp, handled := a.TryHandlePassthroughSlashCommandWithSource(text, "desktop:ai-assistant"); handled {
		if resp != nil {
			requestID := strings.TrimSpace(req.RequestID)
			if requestID == "" {
				requestID = fmt.Sprintf("desktop-ai-%d", time.Now().UnixNano())
			}
			resp.RequestID = requestID
			normalizeArtifactResponseSource(resp)
		}
		return resp, nil
	}
	requestID := strings.TrimSpace(req.RequestID)
	if requestID == "" {
		requestID = fmt.Sprintf("desktop-ai-%d", time.Now().UnixNano())
	}
	if rawProjectPath != "" && rawProjectPath != projectPath {
		log.Printf("[AI assistant] normalized project path request_id=%s raw=%q normalized=%q", requestID, rawProjectPath, projectPath)
	}
	if projectPath != "" && a.isProjectTaskClosed(projectPath) {
		userID := desktopAIAssistantUserIDForProjectPath(projectPath)
		log.Printf("[AI assistant] reject closed project request request_id=%s session_key=%q project_path=%q", requestID, userID, projectPath)
		a.cancelProjectTaskLoop(projectPath)
		return nil, fmt.Errorf("project task is closed: %s", projectPath)
	}
	if executionProjectPath := a.recentTaskExecutionProjectPath(projectPath); executionProjectPath != projectPath {
		log.Printf("[AI assistant] route managed task to working directory request_id=%s task_path=%q working_dir=%q", requestID, projectPath, executionProjectPath)
		if err := a.ensureRecentTaskExecutionWorkingDir(projectPath, executionProjectPath); err != nil {
			log.Printf("[AI assistant] reject managed task working directory request_id=%s task_path=%q working_dir=%q err=%v", requestID, projectPath, executionProjectPath, err)
			return nil, err
		}
	}
	userID := desktopAIAssistantUserIDForProjectPath(projectPath)
	if projectPath != "" && a.isProjectTaskClosed(projectPath) {
		log.Printf("[AI assistant] reject closed project request request_id=%s session_key=%q project_path=%q", requestID, userID, projectPath)
		a.cancelProjectTaskLoop(projectPath)
		return nil, fmt.Errorf("project task is closed: %s", projectPath)
	}

	// Cache the event_scope_id for this session so workflow events can include it.
	// Must be stored before any async path (hubClient==nil goroutine) to ensure
	// events emitted during the goroutine's execution carry the correct scope.
	if scopeID := strings.TrimSpace(req.EventScopeID); scopeID != "" {
		a.sessionEventScopeIDs.Store(userID, scopeID)
	}

	hubClient := a.ensureHubClient()
	if hubClient == nil {
		return nil, fmt.Errorf("AI assistant backend is unavailable")
	}
	log.Printf("[AI assistant] enqueue request request_id=%s session_key=%q project_path=%q text_len=%d", requestID, userID, projectPath, len(text))

	// All messages run in a background goroutine so the Wails IPC channel is
	// freed immediately. The final response is pushed via the
	// "ai-assistant-response" event. This prevents the WebView2 message loop
	// from being blocked during long-running agent loops.
	go a.runAIAssistantMessageAsyncForUser(req, hubClient, requestID, text, userID)

	return &IMAgentResponse{
		RequestID: requestID,
		Deferred:  true,
	}, nil
}

func desktopAIAssistantUserIDForProjectPath(projectPath string) string {
	return projectSessionOwnerID(projectPath)
}

// runAIAssistantMessageAsync executes the agent loop in a background goroutine
// and emits the final response via the "ai-assistant-response" Wails event.
func (a *App) runAIAssistantMessageAsync(req AIAssistantSendRequest, hubClient *RemoteHubClient, requestID string, text string) {
	a.runAIAssistantMessageAsyncForUser(req, hubClient, requestID, text, desktopAIAssistantUserIDForProjectPath(req.ProjectPath))
}

func (a *App) continueAIAssistantWorkflowMessage(userID string, text string, requestID string) (string, error) {
	userID = strings.TrimSpace(userID)
	text = strings.TrimSpace(text)
	if userID == "" {
		return "", fmt.Errorf("workflow userID is required")
	}
	if text == "" {
		return "", fmt.Errorf("message text is required")
	}
	hubClient := a.ensureHubClient()
	if hubClient == nil {
		return "", fmt.Errorf("AI assistant not initialized")
	}
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		requestID = fmt.Sprintf("desktop-ai-%d", time.Now().UnixNano())
	}
	if a.ctx == nil {
		handler := hubClient.ensureIMHandler()
		go handler.HandleIMMessage(IMUserMessage{UserID: userID, Platform: desktopPlatform, Text: text})
		return requestID, nil
	}
	go a.runAIAssistantMessageAsyncForUser(AIAssistantSendRequest{}, hubClient, requestID, text, userID)
	return requestID, nil
}

// StartWorkflowDirect is kept for compatibility with older generated frontend
// bindings. New callers should use StartWorkflowTemplate.
func (a *App) StartWorkflowDirect(workflowType string, projectPath string) (string, error) {
	return a.StartWorkflowTemplate(workflowType, projectPath)
}

// StartWorkflowTemplate starts a workflow template from the "常用工作流" panel.
// If the first phase has an input schema, the AI assistant task panel is shown
// before execution.
func (a *App) StartWorkflowTemplate(workflowType string, projectPath string) (string, error) {
	workflowType = strings.TrimSpace(workflowType)
	if workflowType == "" {
		return "", fmt.Errorf("workflow type is required")
	}

	// Note: workflow disabled setting is NOT checked here.
	// When user explicitly clicks a workflow tile, it should always start
	// regardless of the global workflow toggle. The toggle only affects
	// automatic workflow detection from free-form messages.

	projectPath = normalizeProjectSessionPath(projectPath)
	if projectPath == "" {
		// Align with ProjectDirBar / tool cwd (working_directory), not Projects list.
		projectPath = strings.TrimSpace(a.EffectiveDesktopWorkingDir())
	}
	if projectPath == "" {
		projectPath = "."
	}
	a.ensureInteractionInfra()
	hubClient := a.ensureHubClient()
	if hubClient == nil {
		return "", fmt.Errorf("AI assistant not initialized")
	}
	handler := hubClient.ensureIMHandler()

	// Validate workflow type is registered in the template registry.
	wf := handler.getWorkflowV2()
	if wf == nil {
		return "", fmt.Errorf("workflow engine not initialized")
	}
	if tmpl := wf.machine.GetRegistry().Get(workflowType); tmpl == nil {
		return "", fmt.Errorf("unknown workflow type: %s", workflowType)
	}

	// For workflow panel starts, always use the default desktop session (empty
	// project path). The workflow engine binds the actual working directory via
	// RouteResult.ProjectPath / EffectiveDesktopWorkingDir. Using empty
	// sendProjectPath ensures the message is routed to the same session that the
	// AI assistant panel's default tab is listening on.
	sendProjectPath := ""
	if projectPath == "." {
		projectPath = "" // normalize for RouteResult too
	}
	launch, err := prepareWorkflowTemplatePanelLaunch(handler, workflowType, projectPath, sendProjectPath, time.Now())
	if err != nil {
		return "", err
	}

	// Send the choice command through the normal message path. This goes through
	// enterIMMessageSerializationBoundary, ensuring proper session locking.
	// EventScopeID must be "local" so the backend caches it and subsequent workflow
	// events carry it — the frontend's useWorkflowState filters events by scope ID
	// and only accepts events matching the active tab's scope ("local" for default tab).
	go func() {
		if _, err := a.SendAIAssistantMessage(AIAssistantSendRequest{
			Text:         launch.ChoiceCommand,
			RequestID:    launch.RequestID,
			ProjectPath:  launch.SendProjectPath,
			EventScopeID: "local",
		}); err != nil {
			log.Printf("[StartWorkflowTemplate] SendAIAssistantMessage failed: type=%s err=%v", workflowType, err)
			handler.pendingWorkflowChoice.Delete(launch.UserID)
		}
	}()

	return launch.RequestID, nil
}

type workflowTemplatePanelLaunch struct {
	UserID          string
	RequestID       string
	ChoiceID        string
	ChoiceCommand   string
	SendProjectPath string
}

func prepareWorkflowTemplatePanelLaunch(handler *IMMessageHandler, workflowType, projectPath, sendProjectPath string, now time.Time) (workflowTemplatePanelLaunch, error) {
	if handler == nil {
		return workflowTemplatePanelLaunch{}, fmt.Errorf("AI assistant not initialized")
	}
	workflowType = strings.TrimSpace(workflowType)
	if workflowType == "" {
		return workflowTemplatePanelLaunch{}, fmt.Errorf("workflow type is required")
	}
	if now.IsZero() {
		now = time.Now()
	}
	userID := desktopAIAssistantUserIDForProjectPath(sendProjectPath)
	requestID := fmt.Sprintf("desktop-ai-%d-%s", now.UnixNano(), workflowType)
	choiceID := fmt.Sprintf("template-%d", now.UnixNano())
	choice := workflowChoiceComplex
	switch workflowType {
	case workflowChoiceCodingSubAgent:
		choice = workflowChoiceCodingSubAgent
	case workflowChoiceRemoteCoding:
		choice = workflowChoiceRemoteCoding
	}
	choiceCommand := buildWorkflowChoiceCommand(choice, choiceID)
	handler.pendingWorkflowChoice.Store(userID, &pendingWorkflowChoice{
		Msg: IMUserMessage{
			UserID:    userID,
			Platform:  desktopPlatform,
			Text:      fmt.Sprintf("启动%s工作流", workflowType),
			RequestID: requestID,
		},
		RouteResult: &v2.RouteResult{
			Target:       v2.RouteToWorkflow,
			WorkflowType: workflowType,
			ProjectPath:  projectPath,
		},
		ChoiceID: choiceID,
	})
	return workflowTemplatePanelLaunch{
		UserID:          userID,
		RequestID:       requestID,
		ChoiceID:        choiceID,
		ChoiceCommand:   choiceCommand,
		SendProjectPath: sendProjectPath,
	}, nil
}

func (a *App) runAIAssistantMessageAsyncForUser(req AIAssistantSendRequest, hubClient *RemoteHubClient, requestID string, text string, userID string) {
	sendStartedAt := time.Now()
	readyAt, firstChatLogPending := a.beginFirstAIAssistantChatTelemetry()
	responseEmitted := false
	emitFinalResponse := func(resp *IMAgentResponse) {
		if responseEmitted {
			return
		}
		if resp == nil {
			resp = &IMAgentResponse{}
		}
		resp.SessionKey = userID
		responseEmitted = a.emitAIAssistantResponse(requestID, resp)
	}
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[AI assistant] async request %s panicked: %v", requestID, r)
			if !responseEmitted {
				emitFinalResponse(&IMAgentResponse{Error: fmt.Sprintf("AI assistant request failed: %v", r)})
			}
			return
		}
		if !responseEmitted {
			log.Printf("[AI assistant] async request %s exited without final response", requestID)
			emitFinalResponse(&IMAgentResponse{Error: "AI assistant request ended without a final response."})
		}
	}()
	var ensureInteractionInfraElapsed time.Duration
	var ensureIMHandlerElapsed time.Duration
	var agentLoopElapsed time.Duration
	var finalizeTraceElapsed time.Duration
	var memorySaveElapsed time.Duration
	var capabilityGapElapsed time.Duration
	var fileMaterializeElapsed time.Duration
	var preLLMPrepElapsed time.Duration
	var preLLMConfigElapsed time.Duration
	var preLLMToolsElapsed time.Duration
	var preLLMConversationElapsed time.Duration
	var preLLMIterationPrepElapsed time.Duration
	var firstTokenWaitElapsed time.Duration
	var llmRequestBuildElapsed time.Duration
	var llmHTTPDoElapsed time.Duration
	var llmFirstSSEWaitElapsed time.Duration
	var llmRetryWaitElapsed time.Duration
	var llmStreamMaxTokenGapElapsed time.Duration
	var llmRetryCount int
	var llmIdleTimeoutCount int
	var llmIdleTimeoutAfterToken bool
	var firstTokenElapsed time.Duration
	var streamVisibleElapsed time.Duration
	var streamTailElapsed time.Duration
	var handlerTailElapsed time.Duration
	var handlerBlackholeAfterUsageElapsed time.Duration
	var handlerBlackholeBeforeReturnElapsed time.Duration
	var handlerPostStreamUsageElapsed time.Duration
	var handlerPostStreamResponseElapsed time.Duration
	var handlerPostStreamToolExecElapsed time.Duration
	var handlerPostStreamChoiceElapsed time.Duration
	var handlerPostStreamAssistantMsgElapsed time.Duration
	var handlerPostStreamHistoryAppendElapsed time.Duration
	var handlerPostStreamNoToolBranchElapsed time.Duration
	var firstTokenAt time.Time
	var streamDoneAt time.Time
	defer func() {
		if !firstChatLogPending {
			return
		}
		readyElapsed := time.Duration(0)
		if readyAt > 0 {
			readyElapsed = sendStartedAt.Sub(time.Unix(0, readyAt))
		}
		postResponseElapsed := time.Since(sendStartedAt) - ensureInteractionInfraElapsed - ensureIMHandlerElapsed - agentLoopElapsed
		if postResponseElapsed < 0 {
			postResponseElapsed = 0
		}
		log.Printf("[AI assistant] first desktop chat after ready: since_ready=%v total=%v ensure_interaction_infra=%v ensure_im_handler=%v agent_loop=%v first_token=%v pre_llm_prep=%v pre_llm_config=%v pre_llm_tools=%v pre_llm_conversation=%v pre_llm_iteration_prep=%v first_token_wait=%v llm_request_build=%v llm_http_do=%v llm_first_sse_wait=%v llm_retry_wait=%v llm_stream_max_token_gap=%v llm_retry_count=%d llm_idle_timeout_count=%d llm_idle_timeout_after_token=%v stream_visible=%v stream_tail=%v handler_tail=%v handler_blackhole_after_usage=%v handler_blackhole_before_return=%v handler_post_stream_usage=%v handler_post_stream_response=%v handler_post_stream_tool_exec=%v handler_post_stream_choice=%v handler_post_stream_assistant_msg=%v handler_post_stream_history_append=%v handler_post_stream_no_tool_branch=%v memory_save=%v capability_gap=%v file_materialize=%v finalize_trace=%v post_response=%v interaction_infra_ready=%v warmup_done=%v (trace_enrich+gossip_setup=async)", readyElapsed, time.Since(sendStartedAt), ensureInteractionInfraElapsed, ensureIMHandlerElapsed, agentLoopElapsed, firstTokenElapsed, preLLMPrepElapsed, preLLMConfigElapsed, preLLMToolsElapsed, preLLMConversationElapsed, preLLMIterationPrepElapsed, firstTokenWaitElapsed, llmRequestBuildElapsed, llmHTTPDoElapsed, llmFirstSSEWaitElapsed, llmRetryWaitElapsed, llmStreamMaxTokenGapElapsed, llmRetryCount, llmIdleTimeoutCount, llmIdleTimeoutAfterToken, streamVisibleElapsed, streamTailElapsed, handlerTailElapsed, handlerBlackholeAfterUsageElapsed, handlerBlackholeBeforeReturnElapsed, handlerPostStreamUsageElapsed, handlerPostStreamResponseElapsed, handlerPostStreamToolExecElapsed, handlerPostStreamChoiceElapsed, handlerPostStreamAssistantMsgElapsed, handlerPostStreamHistoryAppendElapsed, handlerPostStreamNoToolBranchElapsed, memorySaveElapsed, capabilityGapElapsed, fileMaterializeElapsed, finalizeTraceElapsed, postResponseElapsed, a.interactionInfraReady(), a.warmupDone.Load())
	}()
	userID = strings.TrimSpace(userID)
	if userID == "" {
		userID = desktopAIAssistantUserIDForProjectPath(req.ProjectPath)
	}
	projectPath := normalizeProjectSessionPath(req.ProjectPath)
	req.ProjectPath = projectPath
	log.Printf("[AI assistant] async start request_id=%s session_key=%q project_path=%q text_len=%d", requestID, userID, projectPath, len(text))
	// Foreground QoS is owned by beginAgentLoopRuntime, the canonical lifetime
	// of an agent loop. The Wails request wrapper can outlive the loop while it
	// emits final events or runs post-processing; counting it here makes the LLM
	// scheduler see stale foreground work and can block unrelated tabs.
	log.Printf("[AI assistant] desktop request accepted request_id=%s session_key=%q project_path=%q", requestID, userID, projectPath)
	msgLang := strings.TrimSpace(req.Lang)
	if msgLang == "" {
		msgLang = a.CurrentLanguage
	}
	msg := IMUserMessage{
		RequestID:                   requestID,
		UserID:                      userID,
		Platform:                    desktopPlatform,
		Text:                        text,
		Lang:                        msgLang,
		ResumeSlotID:                strings.TrimSpace(req.ResumeSlotID),
		StartNewTask:                req.StartNewTask,
		DismissSlotID:               strings.TrimSpace(req.DismissSlotID),
		ResumeRecoverableSessionID:  strings.TrimSpace(req.ResumeSessionID),
		DismissRecoverableSessionID: strings.TrimSpace(req.DismissRecoverableSessionID),
		UIAction:                    req.UIAction,
	}
	emitEvent := func(name, value string) {
		payload, err := json.Marshal(AIAssistantStreamEvent{RequestID: requestID, Text: value, SessionKey: userID})
		if err != nil {
			log.Printf("[SendAIAssistantMessage] marshal %s event failed: %v", name, err)
			return
		}
		a.emitEvent(name, string(payload))
	}
	onProgress := func(progressText string) {
		if progressText == imHeartbeatMsg {
			// Heartbeat must reach the frontend to reset the activity timeout
			// timer, but should not be rendered to the user. The frontend's
			// shouldHideProgressText filters it after resetting the timer.
			emitEvent("ai-assistant-progress", progressText)
			return
		}
		if !isVisibleAIAssistantProgressText(progressText) {
			return
		}
		emitEvent("ai-assistant-progress", progressText)
	}
	streamDeltaNormalizer := &aiAssistantStreamDeltaNormalizer{}
	onToken := func(delta string) {
		delta = streamDeltaNormalizer.Normalize(delta)
		if delta == "" {
			return
		}
		if firstTokenAt.IsZero() {
			firstTokenAt = time.Now()
			firstTokenElapsed = firstTokenAt.Sub(sendStartedAt)
		}
		emitEvent("ai-assistant-token", delta)
	}
	onNewRound := func() {
		streamDeltaNormalizer.Reset()
		emitEvent("ai-assistant-new-round", "")
	}
	onStreamDone := func() {
		if streamDoneAt.IsZero() {
			streamDoneAt = time.Now()
			if firstTokenAt.IsZero() {
				streamVisibleElapsed = streamDoneAt.Sub(sendStartedAt)
			} else {
				streamVisibleElapsed = streamDoneAt.Sub(firstTokenAt)
			}
		}
		emitEvent("ai-assistant-stream-done", "")
	}
	imHandlerStartedAt := time.Now()
	handler := hubClient.ensureIMHandler()
	ensureIMHandlerElapsed = time.Since(imHandlerStartedAt)
	a.reconcileAIAssistantClientHistory(handler, msg.UserID, req.RecentMessages)
	agentLoopStartedAt := time.Now()
	resp := handler.HandleIMMessageWithProgressAndStream(msg, onProgress, onToken, onNewRound, onStreamDone)
	agentLoopElapsed = time.Since(agentLoopStartedAt)
	if !streamDoneAt.IsZero() {
		if resp != nil && resp.HandlerTailNanos > 0 {
			handlerTailElapsed = time.Duration(resp.HandlerTailNanos)
		} else {
			handlerTailElapsed = time.Since(streamDoneAt)
		}
	} else if !firstTokenAt.IsZero() {
		streamVisibleElapsed = agentLoopElapsed - firstTokenElapsed
		if streamVisibleElapsed < 0 {
			streamVisibleElapsed = 0
		}
	}
	if resp != nil && resp.MemorySaveNanos > 0 {
		memorySaveElapsed = time.Duration(resp.MemorySaveNanos)
	}
	if resp != nil && resp.CapabilityGapNanos > 0 {
		capabilityGapElapsed = time.Duration(resp.CapabilityGapNanos)
	}
	if resp != nil && resp.FileMaterializeNanos > 0 {
		fileMaterializeElapsed = time.Duration(resp.FileMaterializeNanos)
	}
	if resp != nil && resp.PreLLMPrepNanos > 0 {
		preLLMPrepElapsed = time.Duration(resp.PreLLMPrepNanos)
	}
	if resp != nil && resp.PreLLMConfigNanos > 0 {
		preLLMConfigElapsed = time.Duration(resp.PreLLMConfigNanos)
	}
	if resp != nil && resp.PreLLMToolsNanos > 0 {
		preLLMToolsElapsed = time.Duration(resp.PreLLMToolsNanos)
	}
	if resp != nil && resp.PreLLMConversationNanos > 0 {
		preLLMConversationElapsed = time.Duration(resp.PreLLMConversationNanos)
	}
	if resp != nil && resp.PreLLMIterationPrepNanos > 0 {
		preLLMIterationPrepElapsed = time.Duration(resp.PreLLMIterationPrepNanos)
	}
	if resp != nil && resp.FirstTokenWaitNanos > 0 {
		firstTokenWaitElapsed = time.Duration(resp.FirstTokenWaitNanos)
	}
	if resp != nil && resp.LLMRequestBuildNanos > 0 {
		llmRequestBuildElapsed = time.Duration(resp.LLMRequestBuildNanos)
	}
	if resp != nil && resp.LLMHTTPDoNanos > 0 {
		llmHTTPDoElapsed = time.Duration(resp.LLMHTTPDoNanos)
	}
	if resp != nil && resp.LLMFirstSSEWaitNanos > 0 {
		llmFirstSSEWaitElapsed = time.Duration(resp.LLMFirstSSEWaitNanos)
	}
	if resp != nil && resp.LLMRetryWaitNanos > 0 {
		llmRetryWaitElapsed = time.Duration(resp.LLMRetryWaitNanos)
	}
	if resp != nil && resp.LLMStreamMaxTokenGapNanos > 0 {
		llmStreamMaxTokenGapElapsed = time.Duration(resp.LLMStreamMaxTokenGapNanos)
	}
	if resp != nil && resp.LLMRetryCount > 0 {
		llmRetryCount = resp.LLMRetryCount
	}
	if resp != nil && resp.LLMIdleTimeoutCount > 0 {
		llmIdleTimeoutCount = resp.LLMIdleTimeoutCount
	}
	if resp != nil && resp.LLMIdleTimeoutAfterToken {
		llmIdleTimeoutAfterToken = true
	}
	if resp != nil && resp.HandlerPostStreamUsageNanos > 0 {
		handlerPostStreamUsageElapsed = time.Duration(resp.HandlerPostStreamUsageNanos)
	}
	if resp != nil && resp.HandlerPostStreamResponseNanos > 0 {
		handlerPostStreamResponseElapsed = time.Duration(resp.HandlerPostStreamResponseNanos)
	}
	if resp != nil && resp.HandlerPostStreamToolExecNanos > 0 {
		handlerPostStreamToolExecElapsed = time.Duration(resp.HandlerPostStreamToolExecNanos)
	}
	if resp != nil && resp.HandlerPostStreamChoiceNanos > 0 {
		handlerPostStreamChoiceElapsed = time.Duration(resp.HandlerPostStreamChoiceNanos)
	}
	if resp != nil && resp.HandlerPostStreamAssistantMsgNanos > 0 {
		handlerPostStreamAssistantMsgElapsed = time.Duration(resp.HandlerPostStreamAssistantMsgNanos)
	}
	if resp != nil && resp.HandlerPostStreamHistoryAppendNanos > 0 {
		handlerPostStreamHistoryAppendElapsed = time.Duration(resp.HandlerPostStreamHistoryAppendNanos)
	}
	if resp != nil && resp.HandlerPostStreamNoToolBranchNanos > 0 {
		handlerPostStreamNoToolBranchElapsed = time.Duration(resp.HandlerPostStreamNoToolBranchNanos)
	}
	if resp != nil && resp.HandlerBlackholeAfterUsageNanos > 0 {
		handlerBlackholeAfterUsageElapsed = time.Duration(resp.HandlerBlackholeAfterUsageNanos)
	}
	if resp != nil && resp.HandlerBlackholeBeforeReturnNanos > 0 {
		handlerBlackholeBeforeReturnElapsed = time.Duration(resp.HandlerBlackholeBeforeReturnNanos)
	}
	if resp != nil && resp.FinalizeTraceNanos > 0 {
		finalizeTraceElapsed = time.Duration(resp.FinalizeTraceNanos)
	}
	// Trace enrichment and gossip detection are non-critical post-processing.
	// Run them asynchronously so the Wails binding returns immediately after
	// the agent loop completes - this unblocks the frontend input box which
	// was locked while awaiting this synchronous call.
	// Note: resp.TraceSummary/TraceEventCount/EvidenceCount are already
	// populated by finalizeTraceResult inside the agent loop. The async
	// GetAIAssistantTrace here is purely for background cache warming.
	if resp != nil && resp.RunID != "" {
		asyncRunID := resp.RunID
		go func() {
			_, _ = a.GetAIAssistantTrace(asyncRunID)
		}()
	}
	if resp != nil && resp.Text != "" {
		// ensureGossipAutoPublish is not goroutine-safe (no mutex), so call
		// it on the current goroutine. Only OnChatCompleted (which has its
		// own mutex) runs in the background.
		a.ensureGossipAutoPublish()
		if a.gossipAutoPublish != nil {
			asyncUserText := text
			asyncRespText := resp.Text
			go a.gossipAutoPublish.OnChatCompleted(asyncUserText, asyncRespText)
		}
	}
	streamTailElapsed = handlerTailElapsed + memorySaveElapsed + capabilityGapElapsed + fileMaterializeElapsed + finalizeTraceElapsed
	// Per-message timing log (fires for every message, not just the first).
	// This is the primary diagnostic tool for "user sent message -> response slow" issues.
	log.Printf("[AI assistant] async done request_id=%s session_key=%q project_path=%q agent_loop=%v first_token=%v ensure_infra=%v ensure_handler=%v pre_llm=%v llm_http=%v llm_first_sse=%v stream_tail=%v text_len=%d",
		requestID, userID, projectPath,
		agentLoopElapsed, firstTokenElapsed, ensureInteractionInfraElapsed, ensureIMHandlerElapsed,
		preLLMPrepElapsed, llmHTTPDoElapsed, llmFirstSSEWaitElapsed, streamTailElapsed,
		len(text))

	// Emit the final response via event so the frontend can process it.
	emitFinalResponse(resp)
}

// emitAIAssistantResponse pushes the final agent loop response to the frontend
// emitStreamingToken sends a streaming token event to the frontend for a given
// requestID and sessionKey. Used to push immediate text content into an active
// streaming round (e.g. form submission echo before the async agent loop starts).
func (a *App) emitStreamingToken(requestID, sessionKey, text string) {
	if a == nil || a.ctx == nil || requestID == "" || text == "" {
		return
	}
	payload, err := json.Marshal(AIAssistantStreamEvent{RequestID: requestID, Text: text, SessionKey: sessionKey})
	if err != nil {
		return
	}
	a.emitEvent("ai-assistant-token", string(payload))
}

// emitAIAssistantResponse emits the final response for an async agent loop
// via the "ai-assistant-response" Wails event. This is the async counterpart
// to the old synchronous return from SendAIAssistantMessage.
func (a *App) emitAIAssistantResponse(requestID string, resp *IMAgentResponse) (ok bool) {
	if resp == nil {
		resp = &IMAgentResponse{}
	}
	resp.RequestID = requestID
	normalizeArtifactResponseSource(resp)
	if canonicalIMResponseSourceKind(resp.ResponseSource).IsArtifactDelivery() || strings.TrimSpace(resp.LocalFilePath) != "" || hasNonEmptyString(resp.LocalFilePaths) {
		paths := resp.LocalFilePaths
		if resp.LocalFilePath != "" && !containsString(paths, resp.LocalFilePath) {
			paths = append([]string{resp.LocalFilePath}, paths...)
		}
		fileNames := make([]string, 0, len(paths))
		for _, p := range paths {
			if strings.TrimSpace(p) != "" {
				fileNames = append(fileNames, filepath.Base(p))
			}
		}
		BrowserDiagFileDelivery("wails-event", resp.Text, fileNames, paths, resp.ResponseSource)
	}
	payload, err := json.Marshal(resp)
	if err != nil {
		log.Printf("[emitAIAssistantResponse] marshal failed: %v", err)
		return false
	}
	if a == nil || a.ctx == nil {
		log.Printf("[emitAIAssistantResponse] skip emit without Wails context request_id=%s session_key=%q", requestID, strings.TrimSpace(resp.SessionKey))
		return false
	}
	log.Printf("[emitAIAssistantResponse] emit request_id=%s session_key=%q text_len=%d error_len=%d fields=%d actions=%d payload_len=%d",
		requestID, strings.TrimSpace(resp.SessionKey), len(resp.Text), len(resp.Error), len(resp.Fields), len(resp.Actions), len(payload))
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[emitAIAssistantResponse] EventsEmit panic: %v", r)
			ok = false
		}
	}()
	a.emitEvent("ai-assistant-response", string(payload))
	return true
}

// SendBtwQuery handles a /btw side query from the desktop AI assistant panel.
// Unlike SendAIAssistantMessage, this runs in a lightweight independent agent
// loop and does NOT block or interfere with the main chat loop. It can be
// called while a main agent loop is active (submitLocked=true on the frontend).
//
// The frontend calls this via a separate code path that bypasses the buffer
// queue and the activeRound idle-phase guard.
func (a *App) SendBtwQuery(query string, requestID string) (*IMAgentResponse, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return &IMAgentResponse{Text: "用法: /btw <查询内容>\n\n示例:\n  /btw 最新的 Go 1.23 有什么新特性\n  /btw React 19 的主要变化\n  /btw 这个项目用了什么框架"}, nil
	}

	a.ensureInteractionInfra()
	hubClient := a.ensureHubClient()
	if hubClient == nil {
		return nil, fmt.Errorf("AI assistant not initialized")
	}
	handler := hubClient.ensureIMHandler()

	if requestID == "" {
		requestID = fmt.Sprintf("btw-%d", time.Now().UnixNano())
	}

	// Stream tokens and progress via dedicated /btw event channels so they
	// don't interfere with the main chat's streaming events.
	emitEvent := func(name, value string) {
		payload, _ := json.Marshal(AIAssistantStreamEvent{RequestID: requestID, Text: value})
		a.emitEvent(name, string(payload))
	}
	onProgress := func(text string) {
		emitEvent("ai-btw-progress", text)
	}
	onToken := func(delta string) {
		emitEvent("ai-btw-token", delta)
	}

	msg := IMUserMessage{
		UserID:   desktopUserID,
		Platform: desktopPlatform,
		Text:     "/btw " + query,
	}
	resp := handler.handleBtwCommand(msg, query, onProgress, onToken)
	if resp != nil {
		resp.RequestID = requestID
		normalizeArtifactResponseSource(resp)
	}
	return resp, nil
}

// ClearAIAssistantHistory clears the desktop AI assistant conversation memory
// and resets all per-user session state — fully equivalent to the /clear command (Wails binding).
func (a *App) ClearAIAssistantHistory() error {
	return a.ClearAIAssistantHistoryForSession("")
}

// ClearAIAssistantHistoryForSession clears conversation memory and resets all
// per-user session state for the given session key (project tab aware).
// An empty sessionKey defaults to the base desktopUserID for backward compat.
func (a *App) ClearAIAssistantHistoryForSession(sessionKey string) error {
	a.ensureInteractionInfra()
	hubClient := a.ensureHubClient()
	if hubClient == nil {
		// Not initialized yet — nothing to clear. Return success (no-op).
		return nil
	}
	handler := hubClient.ensureIMHandler()
	targetUserID := desktopUserID
	if trimmed := strings.TrimSpace(sessionKey); trimmed != "" && trimmed != desktopUserID {
		if normalized, err := normalizeAIAssistantSessionUserID(trimmed); err == nil && normalized != "" {
			targetUserID = normalized
		}
	}
	// Cancel any active agent loop first, so it does not write back into
	// memory after we clear it. This mirrors IM-channel behavior where /clear
	// is serialized behind the per-session mutex and only runs after the loop exits.
	_, _ = handler.CancelSessionForUser(targetUserID)
	handler.memory.Clear(targetUserID)
	handler.clearPerUserSessionState(targetUserID)
	handler.flushEvidenceOnSessionEnd(targetUserID)
	// Clear pending gossip auto-publish buffer as well.
	if a.gossipAutoPublish != nil {
		a.gossipAutoPublish.ClearBuffer()
	}
	return nil
}

// StartAIAssistantBackgroundTask starts a visible AI background task and returns immediately.
func (a *App) StartAIAssistantBackgroundTask(req AIAssistantBackgroundTaskRequest) (*AIAssistantBackgroundTaskResult, error) {
	projectPath := normalizeProjectSessionPath(req.ProjectPath)
	if projectPath == "" {
		projectPath = normalizeProjectSessionPath(a.GetCurrentProjectPath())
	}
	if err := a.ensureWorkflowAllowsRemoteToolCallForOwner(projectSessionOwnerID(projectPath), "delegate_task", map[string]interface{}{"agent": "background", "request": strings.TrimSpace(req.Text), "project_path": projectPath}); err != nil {
		return nil, err
	}
	a.ensureInteractionInfra()
	hubClient := a.ensureHubClient()
	if hubClient == nil {
		return nil, fmt.Errorf("AI assistant not initialized")
	}
	handler := hubClient.ensureIMHandler()
	if handler == nil {
		return nil, fmt.Errorf("AI assistant handler not initialized")
	}
	result, err := handler.StartDesktopBackgroundTask(strings.TrimSpace(req.Text), projectPath)
	if err != nil {
		return nil, err
	}
	if result == nil {
		return &AIAssistantBackgroundTaskResult{Accepted: false, Mode: "background", Error: "failed to start background task"}, nil
	}
	return result, nil
}

// CancelAIAssistantTask cancels a background AI task by remote session ID.
func (a *App) CancelAIAssistantTask(sessionID string) error {
	a.ensureInteractionInfra()
	if a.remoteSessions == nil {
		a.ensureRemoteInfra()
	}
	if a.remoteSessions == nil {
		return fmt.Errorf("remote sessions not initialized")
	}
	return a.remoteSessions.Interrupt(strings.TrimSpace(sessionID))
}

// CancelAIAssistantSession cancels the currently running AI assistant session.
func (a *App) CancelAIAssistantSession() (string, error) {
	a.ensureInteractionInfra()
	hubClient := a.ensureHubClient()
	if hubClient == nil {
		return "", fmt.Errorf("AI assistant not initialized")
	}
	return cancelAIAssistantSessionForHandler(hubClient.ensureIMHandler(), "")
}

// CancelAIAssistantSessionForSession cancels the selected desktop/project AI
// assistant session instead of whichever loop most recently updated global
// legacy state.
func (a *App) CancelAIAssistantSessionForSession(userID string) (string, error) {
	a.ensureInteractionInfra()
	hubClient := a.ensureHubClient()
	if hubClient == nil {
		return "", fmt.Errorf("AI assistant not initialized")
	}
	handler := hubClient.ensureIMHandler()
	targetUserID, err := normalizeAIAssistantSessionUserID(userID)
	if err != nil {
		return "", err
	}
	return cancelAIAssistantSessionForHandler(handler, targetUserID)
}

func cancelAIAssistantSessionForHandler(handler *IMMessageHandler, userID string) (string, error) {
	if handler == nil {
		return "", fmt.Errorf("AI assistant not initialized")
	}
	targetUserID, err := normalizeAIAssistantSessionUserID(userID)
	if err != nil {
		return "", err
	}
	if targetUserID == "" {
		targetUserID = activeAIAssistantLoopUserID(handler)
	}
	return handler.CancelSessionForUser(targetUserID)
}

// InjectAIAssistantSupplementary injects a supplementary message into the
// currently running agent loop without cancelling it. The message is consumed
// at the start of the next iteration as a system-level "[用户补充]" entry.
// Returns true if the injection was accepted (a loop is running), false if
// no loop is active (caller should fall back to normal sendMessage).
func (a *App) InjectAIAssistantSupplementary(text string) (bool, error) {
	a.ensureInteractionInfra()
	hubClient := a.ensureHubClient()
	if hubClient == nil {
		return false, fmt.Errorf("AI assistant not initialized")
	}
	handler := hubClient.ensureIMHandler()
	return handler.InjectSupplementary(activeAIAssistantLoopUserID(handler), text), nil
}

// InjectAIAssistantSupplementaryForSession injects supplementary text into an
// explicitly selected desktop/project session. This prevents task-panel fallback
// messages from being routed through legacy lastUserID when another channel is
// active.
func (a *App) InjectAIAssistantSupplementaryForSession(text string, userID string) (bool, error) {
	a.ensureInteractionInfra()
	hubClient := a.ensureHubClient()
	if hubClient == nil {
		return false, fmt.Errorf("AI assistant not initialized")
	}
	handler := hubClient.ensureIMHandler()
	targetUserID, err := normalizeAIAssistantSessionUserID(userID)
	if err != nil {
		return false, err
	}
	if targetUserID == "" {
		targetUserID = activeAIAssistantLoopUserID(handler)
	}
	return handler.InjectSupplementary(targetUserID, text), nil
}

// InjectAIAssistantGuideReference injects input-buffer guide-launch text as
// background reference for the next agent loop iteration. It is not treated as
// a new user turn and should not make the current session finalize by itself.
func (a *App) InjectAIAssistantGuideReference(text string) (bool, error) {
	a.ensureInteractionInfra()
	hubClient := a.ensureHubClient()
	if hubClient == nil {
		return false, fmt.Errorf("AI assistant not initialized")
	}
	handler := hubClient.ensureIMHandler()
	return handler.InjectGuideReference(activeAIAssistantLoopUserID(handler), text), nil
}

// InjectAIAssistantGuideReferenceForSession injects guide-launch reference text
// into the explicitly selected desktop/project session. This avoids routing a
// buffer fire to whichever loop most recently updated lastUserID when multiple
// tabs are running concurrently.
func (a *App) InjectAIAssistantGuideReferenceForSession(text string, userID string) (bool, error) {
	a.ensureInteractionInfra()
	hubClient := a.ensureHubClient()
	if hubClient == nil {
		return false, fmt.Errorf("AI assistant not initialized")
	}
	handler := hubClient.ensureIMHandler()
	targetUserID, err := normalizeAIAssistantSessionUserID(userID)
	if err != nil {
		return false, err
	}
	if targetUserID == "" {
		targetUserID = activeAIAssistantLoopUserID(handler)
	}
	return handler.InjectGuideReference(targetUserID, text), nil
}

func normalizeAIAssistantSessionUserID(userID string) (string, error) {
	trimmed := strings.TrimSpace(userID)
	if trimmed == "" {
		return "", nil
	}
	if trimmed == desktopUserID {
		return desktopUserID, nil
	}
	if ownerID := projectSessionOwnerID(projectPathFromSessionOwnerID(trimmed)); ownerID != desktopUserID {
		return ownerID, nil
	}
	return "", fmt.Errorf("invalid AI assistant session userID: %q", userID)
}

func activeAIAssistantLoopUserID(handler *IMMessageHandler) string {
	// Legacy desktop APIs do not carry a tab/session id. Under concurrent tabs,
	// lastUserID is whichever loop most recently started, so using it here can
	// cancel or inject into a task-management tab from the main panel. Session-aware
	// callers must use the *ForSession variants.
	return desktopUserID
}

// ResolveCriticalConfirm is called by the desktop frontend when the user
// responds to a critical-risk skill installation confirmation prompt.
// Returns an error if the confirmation has expired or the handler is unavailable,
// so the frontend can show appropriate feedback.
func (a *App) ResolveCriticalConfirm(confirmID string, confirmed bool) error {
	if err := a.resolveSkillInstallConfirm(confirmID, confirmed); err == nil {
		return nil
	} else if strings.HasPrefix(confirmID, "skill_install_") {
		return err
	}
	hubClient := a.ensureHubClient()
	if hubClient == nil {
		return fmt.Errorf("AI assistant not initialized")
	}
	handler := hubClient.ensureIMHandler()
	if handler == nil {
		return fmt.Errorf("message handler not available")
	}
	return handler.ResolveCriticalConfirm(confirmID, confirmed)
}

// ResolveScopeApproval handles the user's response to a SubAgent scope approval prompt.
// decision: "deny", "allow_once", "allow_dir", or "full_access"
func (a *App) ResolveScopeApproval(approvalID string, decision string) {
	ResolveScopeApproval(approvalID, decision)
}

// ---------------------------------------------------------------------------
// Background Loop Wails bindings
// ---------------------------------------------------------------------------

// SSHBackgroundTaskView is the frontend-safe shape for SSH exec_background
// tasks. It intentionally describes commands, not SSH session loops.
type SSHBackgroundTaskView struct {
	TaskID     string                         `json:"task_id"`
	SessionID  string                         `json:"session_id"`
	TaskRole   string                         `json:"task_role,omitempty"`
	Status     remote.SSHBackgroundTaskStatus `json:"status"`
	StartedAt  string                         `json:"started_at"`
	MirrorFile string                         `json:"mirror_file,omitempty"`
}

// ListBackgroundLoops returns all active background loops for the frontend.
func (a *App) ListBackgroundLoops() []BackgroundLoopView {
	hubClient := a.hubClient()
	if hubClient == nil {
		return nil
	}
	handler := hubClient.ensureIMHandler()
	if handler == nil || handler.bgManager == nil {
		return nil
	}
	return handler.bgManager.ListViews()
}

// ListSSHBackgroundTasks returns SSH exec_background tasks for the frontend.
func (a *App) ListSSHBackgroundTasks() []SSHBackgroundTaskView {
	hubClient := a.hubClient()
	if hubClient == nil {
		return nil
	}
	handler := hubClient.ensureIMHandler()
	if handler == nil || handler.bgTaskMgr == nil {
		return nil
	}
	ownerID := strings.TrimSpace(a.defaultManualPolicyOwnerID())
	tasks := handler.bgTaskMgr.ListTasks()
	views := make([]SSHBackgroundTaskView, 0, len(tasks))
	for _, task := range tasks {
		if ownerID == "" && strings.TrimSpace(task.OwnerID) != "" {
			continue
		}
		if ownerID != "" && !remote.SSHBackgroundTaskOwnerMatches(task.OwnerID, ownerID) {
			continue
		}
		if task.Status.IsActive() && (task.LastCheck.IsZero() || time.Since(task.LastCheck) > 15*time.Second) {
			handler.bgTaskMgr.RefreshTaskStatusAsyncForOwner(task.TaskID, 5, ownerID)
		}
		views = append(views, SSHBackgroundTaskView{
			TaskID:     task.TaskID,
			SessionID:  task.SessionID,
			TaskRole:   task.TaskRole,
			Status:     task.Status,
			StartedAt:  task.StartedAt.Format(time.RFC3339),
			MirrorFile: task.MirrorFile,
		})
	}
	return views
}

// StopAllBackgroundLoops stops all running background loops.
// Returns the list of stopped loop IDs.
func (a *App) StopAllBackgroundLoops() []string {
	hubClient := a.ensureHubClient()
	if hubClient == nil {
		return nil
	}
	handler := hubClient.ensureIMHandler()
	if handler == nil || handler.bgManager == nil {
		return nil
	}
	// Snapshot IDs of running/paused loops before stopping.
	views := handler.bgManager.ListViews()
	var ids []string
	for _, v := range views {
		if v.Status == LoopStateRunning || v.Status == LoopStatePaused {
			ids = append(ids, v.ID)
		}
	}
	handler.bgManager.StopAll()
	return ids
}

// StopAllBackgroundTasks stops all background loops AND all active remote
// sessions (AI coding sessions). Returns the total number of items stopped.
func (a *App) StopAllBackgroundTasks() int {
	stopped := 0

	// 1. Stop background loops
	loopIDs := a.StopAllBackgroundLoops()
	stopped += len(loopIDs)

	// 2. Kill all active remote sessions
	if a.remoteSessions != nil {
		killed := a.remoteSessions.KillAllActive()
		stopped += len(killed)
	}

	return stopped
}

// DismissRemoteSession removes a terminated session from the session manager
// so it no longer appears in the session list. Returns an error if the session
// is still active.
func (a *App) DismissRemoteSession(sessionID string) error {
	if a.remoteSessions == nil {
		return fmt.Errorf("session manager not initialized")
	}
	if ok := a.remoteSessions.RemoveTerminated(sessionID); !ok {
		return fmt.Errorf("session %s not found or still active", sessionID)
	}
	return nil
}

// StopBackgroundLoop gracefully stops a background loop by ID.
func (a *App) StopBackgroundLoop(loopID string) error {
	hubClient := a.ensureHubClient()
	if hubClient == nil {
		return fmt.Errorf("background loop manager not initialized")
	}
	handler := hubClient.ensureIMHandler()
	if handler == nil || handler.bgManager == nil {
		return fmt.Errorf("background loop manager not initialized")
	}
	handler.bgManager.Stop(loopID)
	return nil
}

// ContinueBackgroundLoop sends additional rounds to a paused loop.
func (a *App) ContinueBackgroundLoop(loopID string, additionalRounds int) error {
	hubClient := a.ensureHubClient()
	if hubClient == nil {
		return fmt.Errorf("background loop manager not initialized")
	}
	handler := hubClient.ensureIMHandler()
	if handler == nil || handler.bgManager == nil {
		return fmt.Errorf("background loop manager not initialized")
	}
	return handler.bgManager.SendContinue(loopID, additionalRounds)
}

// GetBackgroundLoopOutput returns the terminal output lines for a background
// loop's associated session. For SSH-type loops, it reads from the SSH session
// manager; for other types, it falls back to the remote session manager.
func (a *App) GetBackgroundLoopOutput(sessionID string) []string {
	if sessionID == "" {
		return nil
	}
	// Try SSH session manager first (SSH loops store sessions there).
	hubClient := a.hubClient()
	if hubClient != nil {
		handler := hubClient.ensureIMHandler()
		if handler.sshMgr != nil {
			if sess, ok := handler.sshMgr.Get(sessionID); ok {
				return sess.PreviewTail(2000)
			}
		}
	}
	// Fall back to remote session manager.
	if a.remoteSessions != nil {
		if sess, ok := a.remoteSessions.Get(sessionID); ok {
			sess.mu.RLock()
			out := make([]string, len(sess.RawOutputLines))
			copy(out, sess.RawOutputLines)
			sess.mu.RUnlock()
			return out
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Agent Skill compatibility Wails bindings
// ---------------------------------------------------------------------------

// ImportAgentSkillDir imports an Anthropic Agent Skills directory (SKILL.md)
// and registers it as a local NL Skill (Wails binding).
func (a *App) ImportAgentSkillDir(skillDir string) (string, error) {
	if err := a.ensureWorkflowAllowsRemoteToolCall("manage_skill", map[string]interface{}{"action": "import", "source": "agent_skill_dir", "path": skillDir}); err != nil {
		return "", err
	}
	a.ensureRemoteInfra()
	if a.skillExecutor == nil {
		return "", fmt.Errorf("skill executor not initialized")
	}
	entry, err := ImportAgentSkill(skillDir)
	if err != nil {
		return "", err
	}
	report, err := a.scanAndAdmitSkillBeforeRegister(context.Background(), entry, "agent skill directory import")
	if err != nil {
		return "", err
	}
	if err := writeSkillScanCacheForInstalledEntry(entry, report); err != nil {
		return "", fmt.Errorf("write skill scan cache: %w", err)
	}
	if err := a.skillExecutor.Register(*entry); err != nil {
		return "", err
	}
	return entry.Name, nil
}

// ExportAgentSkillDir exports a local NL Skill to Anthropic Agent Skills
// format (SKILL.md + scripts/) in the specified output directory (Wails binding).
func (a *App) ExportAgentSkillDir(skillName string, outputDir string) error {
	a.ensureRemoteInfra()
	if a.skillExecutor == nil {
		return fmt.Errorf("skill executor not initialized")
	}
	skills := a.skillExecutor.loadSkills()
	for _, s := range skills {
		if s.Name == skillName {
			return ExportAgentSkill(s, outputDir, a)
		}
	}
	return fmt.Errorf("skill %q not found", skillName)
}

// ---------------------------------------------------------------------------
// Pasted image save Wails binding
// ---------------------------------------------------------------------------

// SavePastedImage saves a base64-encoded image to a temporary file and returns
// the absolute file path. The image is stored in os.TempDir()/maclaw-paste/
// with a timestamped filename.
func (a *App) SavePastedImage(base64Data string, extension string) (string, error) {
	// Normalize extension: lowercase, strip leading dot.
	ext := strings.ToLower(strings.TrimSpace(extension))
	ext = strings.TrimPrefix(ext, ".")

	// Validate extension against whitelist.
	allowedExts := map[string]bool{
		"png": true, "jpg": true, "jpeg": true,
		"gif": true, "webp": true, "bmp": true,
	}
	if !allowedExts[ext] {
		return "", fmt.Errorf("unsupported image extension: %s", ext)
	}

	// Validate base64 data size (<= 50MB before decoding).
	const maxBase64Size = 50 * 1024 * 1024 // 50MB
	if len(base64Data) > maxBase64Size {
		return "", fmt.Errorf("image data too large (max 50MB)")
	}

	// Create temp directory.
	tempDir := filepath.Join(os.TempDir(), "maclaw-paste")
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create temp directory: %w", err)
	}

	// Generate timestamped filename with random suffix.
	now := time.Now()
	randBytes := make([]byte, 2)
	if _, err := rand.Read(randBytes); err != nil {
		return "", fmt.Errorf("failed to generate random suffix: %w", err)
	}
	randHex := fmt.Sprintf("%x", randBytes)
	fileName := fmt.Sprintf("paste_%s_%s.%s", now.Format("20060102_150405"), randHex, ext)

	// Decode base64 data.
	data, err := base64.StdEncoding.DecodeString(base64Data)
	if err != nil {
		return "", fmt.Errorf("invalid base64 data: %w", err)
	}

	// Write to file.
	filePath := filepath.Join(tempDir, fileName)
	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return "", fmt.Errorf("failed to write image file: %w", err)
	}

	// Return absolute path.
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return filePath, nil // fallback to non-absolute path
	}
	return absPath, nil
}

// SavePastedFile saves a base64-encoded clipboard file to a temporary file and
// returns the absolute path. It is used when the WebView clipboard does not
// expose a direct local filesystem path for a pasted file.
func (a *App) SavePastedFile(base64Data string, fileName string, mimeType string) (string, error) {
	safeName := sanitizePastedFileName(fileName)
	if safeName == "" {
		safeName = "pasted-file"
	}

	const maxBase64Size = 100 * 1024 * 1024
	if len(base64Data) > maxBase64Size {
		return "", fmt.Errorf("file data too large (max 100MB)")
	}

	data, err := base64.StdEncoding.DecodeString(base64Data)
	if err != nil {
		return "", fmt.Errorf("invalid base64 data: %w", err)
	}

	tempDir := filepath.Join(os.TempDir(), "maclaw-paste")
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create temp directory: %w", err)
	}

	now := time.Now()
	randBytes := make([]byte, 2)
	if _, err := rand.Read(randBytes); err != nil {
		return "", fmt.Errorf("failed to generate random suffix: %w", err)
	}
	randHex := fmt.Sprintf("%x", randBytes)
	ext := filepath.Ext(safeName)
	base := strings.TrimSuffix(safeName, ext)
	if base == "" {
		base = "pasted-file"
	}
	if ext == "" {
		ext = pastedFileExtensionFromMIME(mimeType)
	}
	filePath := filepath.Join(tempDir, fmt.Sprintf("paste_%s_%s_%s%s", now.Format("20060102_150405"), randHex, base, ext))

	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return "", fmt.Errorf("failed to write pasted file: %w", err)
	}

	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return filePath, nil
	}
	return absPath, nil
}

func sanitizePastedFileName(fileName string) string {
	normalizedPath := strings.ReplaceAll(strings.TrimSpace(fileName), "\\", "/")
	name := strings.TrimSpace(filepath.Base(normalizedPath))
	if name == "." || name == string(filepath.Separator) {
		return ""
	}
	name = strings.Map(func(r rune) rune {
		switch r {
		case '<', '>', ':', '"', '/', '\\', '|', '?', '*':
			return '_'
		}
		if r < 32 {
			return '_'
		}
		return r
	}, name)
	name = strings.Trim(name, " .")
	if len([]rune(name)) > 180 {
		ext := filepath.Ext(name)
		base := strings.TrimSuffix(name, ext)
		if len(ext) > 20 {
			ext = ""
		}
		limit := 180 - len(ext)
		if limit < 1 {
			limit = 1
		}
		baseRunes := []rune(base)
		if len(baseRunes) > limit {
			base = string(baseRunes[:limit])
		}
		name = base + ext
	}
	return name
}

func pastedFileExtensionFromMIME(mimeType string) string {
	switch strings.ToLower(strings.TrimSpace(mimeType)) {
	case "application/pdf":
		return ".pdf"
	case "text/plain":
		return ".txt"
	case "text/csv":
		return ".csv"
	case "application/json":
		return ".json"
	case "application/zip", "application/x-zip-compressed":
		return ".zip"
	case "application/vnd.openxmlformats-officedocument.wordprocessingml.document":
		return ".docx"
	case "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":
		return ".xlsx"
	case "application/vnd.openxmlformats-officedocument.presentationml.presentation":
		return ".pptx"
	default:
		return ""
	}
}

// ReadErrorLog reads maclaw.log and returns only lines
// containing error-level keywords. Returns the most recent 500 error lines
// in reverse chronological order (newest first).
func (a *App) ReadErrorLog() ([]string, error) {
	logPath := filepath.Join(corelib.MaclawLogsDir(), "maclaw.log")

	f, err := os.Open(logPath)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, fmt.Errorf("cannot open log file: %w", err)
	}
	defer f.Close()

	var errors []string
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 64*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if classifyAppLogLine(line) == appLogLineError {
			errors = append(errors, line)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading log file: %w", err)
	}

	// Keep only the most recent 500 lines.
	const maxLines = 500
	if len(errors) > maxLines {
		errors = errors[len(errors)-maxLines:]
	}

	// Reverse so newest errors appear first.
	for i, j := 0, len(errors)-1; i < j; i, j = i+1, j-1 {
		errors[i], errors[j] = errors[j], errors[i]
	}

	return errors, nil
}

// ---------------------------------------------------------------------------
// IM Audit Store - Wails bindings
// ---------------------------------------------------------------------------

// ensureIMAuditStore lazily initializes the IM audit SQLite store.
func (a *App) ensureIMAuditStore() {
	a.imAuditStoreMu.Do(func() {
		dbPath := filepath.Join(a.GetDataDir(), "im_audit.db")
		store, err := NewIMAuditStore(dbPath)
		if err != nil {
			log.Printf("[im-audit] failed to open store: %v", err)
			return
		}
		a.imAuditStore = store
	})
}

// getIMAuditStore returns the audit store, initializing if needed.
func (a *App) getIMAuditStore() *IMAuditStore {
	a.ensureIMAuditStore()
	return a.imAuditStore
}

// QueryIMAuditMessages returns paginated IM audit messages (Wails binding).
func (a *App) QueryIMAuditMessages(platform, userID, keyword string, page int) (*IMAuditQueryResult, error) {
	store := a.getIMAuditStore()
	if store == nil {
		return &IMAuditQueryResult{Messages: []IMAuditMessage{}, PageSize: imAuditPageSize}, nil
	}
	return store.Query(platform, userID, keyword, page)
}

// DeleteIMAuditMessagesBefore removes IM audit messages older than N days (Wails binding).
func (a *App) DeleteIMAuditMessagesBefore(days int) (int64, error) {
	store := a.getIMAuditStore()
	if store == nil {
		return 0, nil
	}
	return store.DeleteBefore(days)
}

// ExportIMAuditCSV exports matching IM audit messages to CSV and returns the file path (Wails binding).
func (a *App) ExportIMAuditCSV(platform, userID, keyword string) (string, error) {
	store := a.getIMAuditStore()
	if store == nil {
		return "", fmt.Errorf("audit store not available")
	}
	outputDir := a.GetTempDir()
	return store.ExportCSV(platform, userID, keyword, outputDir)
}

// GetIMAuditUsers returns distinct user IDs for the given platform (Wails binding).
func (a *App) GetIMAuditUsers(platform string) ([]string, error) {
	store := a.getIMAuditStore()
	if store == nil {
		return []string{}, nil
	}
	return store.ListUsers(platform)
}

// GetIMAuditStats returns per-platform message count statistics (Wails binding).
func (a *App) GetIMAuditStats() (*IMAuditStats, error) {
	store := a.getIMAuditStore()
	if store == nil {
		return &IMAuditStats{}, nil
	}
	return store.Stats()
}
