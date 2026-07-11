package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/security"
	cskill "github.com/RapidAI/CodeClaw/corelib/skill"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// syncSkillHubTools registers the search_and_install_skill tool when a
// SkillMarket (HubCenter) is reachable, giving the LLM the ability to
// proactively search the SkillMarket and install skills during a session.
func (h *IMMessageHandler) syncSkillHubTools() {
	if h.registry == nil {
		return
	}
	// The tool is available as long as we have an App (which provides HubCenter URL).
	hasApp := h.app != nil
	_, hasSearchTool := h.registry.Get("search_and_install_skill")

	if hasApp && !hasSearchTool {
		h.registry.Register(RegisteredTool{
			Name: "search_and_install_skill",
			Description: "Search and install a skill from SkillMarket when existing tools cannot fulfill the request. " +
				"Search and install a skill from SkillMarket when existing tools cannot fulfill the request.",
			Category: ToolCategoryBuiltin,
			Tags:     []string{"skill", "skillmarket", "install", "search", "capability"},
			Status:   RegToolAvailable,
			InputSchema: map[string]interface{}{
				"query": map[string]string{"type": "string", "description": "Search query describing the capability you need."},
			},
			Required: []string{"query"},
			HandlerProg: func(args map[string]interface{}, onProgress tool.ProgressCallback) string {
				return h.executeSkillSearchInstall(args, onProgress).Text
			},
		})
	} else if !hasApp && hasSearchTool {
		h.registry.Unregister("search_and_install_skill")
	}
}

// installAndExecuteSkill handles the download, security review, registration,
// and execution of a found skill. Shared by both active (tool call) and
// passive (capability gap) paths.
//
// platform and userID are passed explicitly for the same reason as
// registerAndExecuteSkill 鈥?async callers must capture these before
// the agent loop's defer clears currentLoopCtx.
func (h *IMMessageHandler) installAndExecuteSkill(ctx context.Context, best *SkillSearchResult, query, platform, userID, policyOwnerID string, sendStatus func(string)) skillInstallExecutionResult {
	if h != nil && h.app != nil && best != nil {
		ownerID := strings.TrimSpace(policyOwnerID)
		if ownerID == "" {
			ownerID = strings.TrimSpace(userID)
		}
		if err := h.app.ensureWorkflowAllowsRemoteToolCallForOwner(ownerID, "manage_skill", map[string]interface{}{"action": "install", "name": best.Name, "source": best.SourceKind().String(), "query": query}); err != nil {
			return skillInstallExecutionResult{Text: err.Error(), SilentFailure: true}
		}
		if err := h.app.ensureWorkflowAllowsRemoteToolCallForOwner(ownerID, "manage_skill", map[string]interface{}{"action": "run", "name": best.Name, "source": best.SourceKind().String(), "query": query}); err != nil {
			return skillInstallExecutionResult{Text: err.Error(), SilentFailure: true}
		}
	}
	// Enterprise-only install policy: when enabled, only enterprise Hub source is allowed.
	if h != nil && h.app != nil && best != nil {
		if cfg, err := h.app.LoadConfig(); err == nil {
			if reason, blocked := cfg.CapabilityMarketPolicy.RejectNonEnterpriseInstall(best.SourceKind().String(), cfg.RemoteHubURL); blocked {
				return skillInstallExecutionResult{Text: reason, SilentFailure: true}
			}
		}
	}
	// GitHub result → import via a stable install ref when available.
	if best.SourceKind() == skillSearchSourceGitHub {
		if h.app != nil {
			guardArgs := map[string]interface{}{"action": "install", "source": "github", "skill_id": best.ID, "install_ref": best.InstallRef}
			if ok, reason := h.app.enforceHubSecurityAppPolicy("manage_skill", guardArgs); !ok {
				return skillInstallExecutionResult{Text: reason, SilentFailure: true}
			}
		}
		var imported *corelib.NLSkillEntry
		if strings.TrimSpace(best.InstallRef) != "" {
			var candidate cskill.GitHubSkillCandidate
			if err := json.Unmarshal([]byte(best.InstallRef), &candidate); err == nil && strings.TrimSpace(candidate.RawURL) != "" {
				imported, err = cskill.NewGitHubSearcher("").ImportFromCandidate(candidate)
			}
		}
		if imported == nil {
			gs := cskill.NewGitHubSearcher("")
			candidates, err := gs.SearchGitHub(best.ID)
			if err != nil || len(candidates) == 0 {
				return skillInstallExecutionResult{Text: fmt.Sprintf("GitHub skill import failed: %v", err)}
			}
			candidates = filterGitHubSkillCandidatesForIntent(query, candidates)
			if len(candidates) == 0 {
				return skillInstallExecutionResult{Text: fmt.Sprintf("GitHub skill import failed: no intent-compatible candidate for %s", best.ID)}
			}
			imported, err = gs.ImportFromCandidate(candidates[0])
			if err != nil {
				return skillInstallExecutionResult{Text: fmt.Sprintf("GitHub skill import failed: %v", err)}
			}
		}
		imported.Source = "auto_github"
		return h.registerAndExecuteSkill(ctx, imported, best.Name, "auto_github", platform, userID, policyOwnerID, sendStatus)
	}

	// SkillMarket or ClawHub result 鈫?download and register locally.
	sendStatus(fmt.Sprintf("猬囷笍 姝ｅ湪瀹夎: %s ...", best.Name))

	if best.SourceKind() == skillSearchSourceClawHub {
		if h.app != nil {
			guardArgs := map[string]interface{}{"action": "install", "source": "clawhub", "skill_id": best.ID, "hub_url": cskill.ClawHubMirrorURL}
			if ok, reason := h.app.enforceHubSecurityAppPolicy("manage_skill", guardArgs); !ok {
				return skillInstallExecutionResult{Text: reason, SilentFailure: true}
			}
		}
		skill, dlErr := downloadClawHubSkill(ctx, best.ID)
		if dlErr != nil {
			return skillInstallExecutionResult{Text: fmt.Sprintf("Found ClawHub skill %s but download failed: %v", best.Name, dlErr)}
		}
		skill.Source = "auto_clawhub"
		// Assign a staging directory for ClawHub skills so they go through
		// the same staging → security scan → commit isolation as Hub skills.
		if skill.SkillDir == "" {
			if stagingDir, stagingErr := cskill.PrepareStagingDir(skill.Name); stagingErr == nil {
				skill.SkillDir = stagingDir
			}
		}
		return h.registerAndExecuteSkill(ctx, skill, best.Name, "auto_clawhub", platform, userID, policyOwnerID, sendStatus)
	}

	// SkillMarket result: download through the HubCenter failover pool into
	// staging so file contents are scanned before they reach the final skill dir.
	if h.app != nil {
		hubURL := NewSkillMarketClient(h.app).baseURL()
		guardArgs := map[string]interface{}{"action": "install", "source": "skillhub", "skill_id": best.ID, "hub_url": hubURL}
		if ok, reason := h.app.enforceHubSecurityAppPolicy("manage_skill", guardArgs); !ok {
			return skillInstallExecutionResult{Text: reason, SilentFailure: true}
		}
	}
	stagingDir, dlErr := cskill.PrepareStagingDir(firstNonEmpty(best.ID, best.Name, "auto-hub-skill"))
	if dlErr != nil {
		return skillInstallExecutionResult{Text: fmt.Sprintf("Found skill %s but staging failed: %v", best.Name, dlErr)}
	}
	skill, dlErr := downloadSkillJSONFromHubCenterToDir(ctx, h.app, "/api/v1/skills/"+url.PathEscape(best.ID)+"/download", stagingDir)
	if dlErr != nil {
		cskill.CleanupStaging(stagingDir)
		return skillInstallExecutionResult{Text: fmt.Sprintf("Found skill %s but download failed: %v", best.Name, dlErr)}
	}
	skill.Source = "auto_hub"
	return h.registerAndExecuteSkill(ctx, skill, best.Name, "auto_hub", platform, userID, policyOwnerID, sendStatus)
}

// downloadClawHubSkill fetches a skill from the ClawHub mirror and converts
// the SKILL.md content into an NLSkillEntry with a single craft_tool step
// that uses the SKILL.md as instructions.
// Delegates to the shared corelib/skill.HubClient.
func downloadClawHubSkill(ctx context.Context, slug string) (*corelib.NLSkillEntry, error) {
	client := cskill.DefaultHubClient()
	return client.DownloadClawHub(ctx, slug)
}

// downloadSkillJSON fetches a skill definition from the given URL and
// converts it to an NLSkillEntry ready for local registration.
// Handles both step-based skills (steps array) and file-based skills
// (files map with SKILL.md in base64). All bundled files are extracted
// to ~/.maclaw/data/skills/<name>/.
func downloadSkillJSON(ctx context.Context, endpoint string) (*corelib.NLSkillEntry, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "MaClaw/1.0")

	// Multi-asset skill JSON can be tens of MiB; keep timeout aligned with installClient.
	client := &http.Client{Timeout: 180 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	data, err := readLimitedHubCenterBodyWithLength(resp.Body, resp.ContentLength, maxDownloadSize)
	if err != nil {
		return nil, err
	}

	var full struct {
		ID          string            `json:"id"`
		SkillID     string            `json:"skill_id"`
		SemVer      string            `json:"semver"`
		Name        string            `json:"name"`
		Description string            `json:"description"`
		Triggers    []string          `json:"triggers"`
		TrustLevel  string            `json:"trust_level"`
		Version     string            `json:"version"`
		Steps       []json.RawMessage `json:"steps"`
		Files       map[string]string `json:"files"` // path → base64 content
	}
	if err := json.Unmarshal(data, &full); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}

	var steps []corelib.NLSkillStep

	if len(full.Steps) > 0 {
		for _, raw := range full.Steps {
			var s struct {
				Action  string                 `json:"action"`
				Params  map[string]interface{} `json:"params"`
				OnError string                 `json:"on_error"`
			}
			if err := json.Unmarshal(raw, &s); err == nil && s.Action != "" {
				steps = append(steps, corelib.NLSkillStep{Action: s.Action, Params: s.Params, OnError: s.OnError})
			}
		}
	}

	// Extract all bundled files to ~/.maclaw/data/skills/<name>/.
	installName := firstNonEmpty(full.Name, full.ID)
	installSkillDir := ""
	if len(full.Files) > 0 && installName != "" {
		if err := extractSkillFiles(installName, full.Files, ""); err != nil {
			return nil, fmt.Errorf("extract bundled files for skill %q: %w", installName, err)
		}
		if skillsRoot, err := cskill.PrimarySkillsDir(); err == nil {
			installSkillDir = filepath.Join(skillsRoot, installName)
		}
	}

	if len(steps) == 0 {
		steps = craftToolStepsFromBundledSkillFiles(full.Files, installSkillDir)
	}

	if len(steps) == 0 {
		return nil, fmt.Errorf("skill %s has no steps and no SKILL.md", full.Name)
	}

	// Skills from the configured hub (official store) are treated as "trusted".
	trustLevel := full.TrustLevel
	if trustLevel == "" || trustLevel == "community" {
		trustLevel = "trusted"
	}

	return &corelib.NLSkillEntry{
		SkillID:     full.SkillID,
		Name:        full.Name,
		Description: full.Description,
		Triggers:    full.Triggers,
		Steps:       steps,
		Status:      "active",
		CreatedAt:   time.Now().Format(time.RFC3339),
		Source:      "hub",
		HubSkillID:  full.ID,
		HubVersion:  full.Version,
		Version:     full.SemVer,
		TrustLevel:  trustLevel,
		SkillDir:    installSkillDir,
	}, nil
}

func skillStagingDir(skillDir string) string {
	skillDir = strings.TrimSpace(skillDir)
	if skillDir == "" {
		return ""
	}
	root, err := cskill.StagingDir()
	if err != nil {
		return ""
	}
	absDir, err := filepath.Abs(skillDir)
	if err != nil {
		return ""
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return ""
	}
	if absDir == absRoot || strings.HasPrefix(absDir, absRoot+string(filepath.Separator)) {
		return absDir
	}
	return ""
}

// extractSkillFiles decodes base64-encoded files and writes them to the
// specified targetDir, preserving subdirectory structure.
// When targetDir is empty, falls back to ~/.maclaw/data/skills/<skillName>/.
func extractSkillFiles(skillName string, files map[string]string, targetDir string) error {
	if err := extractBundledSkillFiles(skillName, files, targetDir); err != nil {
		return err
	}
	log.Printf("[skill-install] extracted %d files for %s", len(files), skillName)
	return nil
}

// registerAndExecuteSkill registers a skill locally, runs security review,
// executes it, and returns the result string.
//
// platform and userID are passed explicitly (not read from h.currentLoopCtx)
// because this function may be called from async goroutines where
// currentLoopCtx has already been cleared by the agent loop's defer.
func (h *IMMessageHandler) registerAndExecuteSkill(ctx context.Context, skill *corelib.NLSkillEntry, displayName, source string, platform, userID, policyOwnerID string, sendStatus func(string)) skillInstallExecutionResult {
	if h.getSkillExecutor() == nil {
		return skillInstallExecutionResult{Text: fmt.Sprintf("Found skill %s but SkillExecutor is not initialized", displayName)}
	}
	ownerID := strings.TrimSpace(policyOwnerID)
	if ownerID == "" {
		ownerID = strings.TrimSpace(userID)
	}
	stagingDir := skillStagingDir(skill.SkillDir)

	// Security review: staging scan; policy decides whether findings block.
	// Developer mode records scan findings but never blocks installation.
	var installScanReport *cskill.ScanReport
	{
		var scanReport *cskill.ScanReport
		if h.app == nil || !h.app.isRiskGuardrailOffMode() {
			scanner := NewSkillSecurityScanner(h.app, nil)
			scanReport = scanner.ScanInstallStaged(ctx, skill, skill.SkillDir, sendStatus)
			installScanReport = scanReport
		}

		if h.app != nil && h.app.skillInstallScanShouldBlockForSource(scanReport, source) {
			cskill.CleanupStaging(stagingDir)
			if h.getAuditLog() != nil {
				_ = h.getAuditLog().Log(security.AuditEntry{
					Timestamp:    time.Now(),
					Action:       security.AuditActionHubSkillReject,
					ToolName:     source + "_skill_install",
					RiskLevel:    scanReport.FinalLevel,
					PolicyAction: security.PolicyDeny,
					Result:       fmt.Sprintf("policy blocked %s-risk skill %s: %s", scanReport.FinalLevel, displayName, scanReport.Summary),
				})
			}
			return skillInstallExecutionResult{
				Text:          FormatScanReportForUser(scanReport, displayName) + "\n" + localizedSkillInstallBlockedMessage(h.skillConfirmLang(), displayName, false),
				SilentFailure: true,
			}
		}

		if h.app != nil && h.app.skillInstallReviewNeedsConfirmationForSource(scanReport, source) {
			confirmed := h.confirmRiskSkillInstall(
				ctx, displayName, source, scanReport.FinalLevel, scanReport.PatternAssessment.Factors, platform, userID,
			)
			if !confirmed {
				cskill.CleanupStaging(stagingDir)
				if h.getAuditLog() != nil {
					_ = h.getAuditLog().Log(security.AuditEntry{
						Timestamp:    time.Now(),
						Action:       security.AuditActionHubSkillReject,
						ToolName:     source + "_skill_install",
						RiskLevel:    scanReport.FinalLevel,
						PolicyAction: security.PolicyDeny,
						Result:       fmt.Sprintf("user rejected %s-risk skill %s: %s", scanReport.FinalLevel, displayName, scanReport.Summary),
					})
				}
				return skillInstallExecutionResult{
					Text:          FormatScanReportForUser(scanReport, displayName) + "\n" + localizedSkillInstallRejectedMessage(h.skillConfirmLang(), displayName),
					SilentFailure: true,
				}
			}
			if h.getAuditLog() != nil {
				_ = h.getAuditLog().Log(security.AuditEntry{
					Timestamp:    time.Now(),
					Action:       security.AuditActionHubSkillInstall,
					ToolName:     source + "_skill_install",
					RiskLevel:    scanReport.FinalLevel,
					PolicyAction: security.PolicyUserOverride,
					Result:       fmt.Sprintf("user approved %s-risk skill %s: %s", scanReport.FinalLevel, displayName, scanReport.Summary),
				})
			}
		}
	}

	if h != nil && h.app != nil {
		if err := h.app.ensureWorkflowAllowsRemoteToolCallForOwner(ownerID, "manage_skill", map[string]interface{}{"action": "run", "name": skill.Name, "source": source}); err != nil {
			cskill.CleanupStaging(stagingDir)
			return skillInstallExecutionResult{Text: err.Error(), SilentFailure: true}
		}
	}

	sendStatus(fmt.Sprintf("Registering Skill: %s ...", skill.Name))
	preNormalizeScanHash := ""
	committedDir := ""
	if stagingDir != "" {
		finalDir, err := cskill.CommitStaging(stagingDir, skill.Name)
		if err != nil {
			cskill.CleanupStaging(stagingDir)
			return skillInstallExecutionResult{Text: fmt.Sprintf("Installing Skill %s failed: %v", displayName, err)}
		}
		skill.SkillDir = finalDir
		committedDir = finalDir
		if installScanReport != nil {
			if hash, err := skillContentHash(skill); err == nil {
				preNormalizeScanHash = hash
			} else {
				log.Printf("[skill-auto] failed to hash approved pre-normalize skill %s: %v", skill.Name, err)
			}
		}
		if h.app != nil {
			skill = h.app.normalizeInstalledSkill(skill)
		}
	}
	if installScanReport != nil && preNormalizeScanHash != "" {
		if err := writeSkillScanCacheForReportStatus(skill, skill.SkillDir, preNormalizeScanHash, installScanReport, skillScanCacheStatusAllowed); err != nil {
			if committedDir != "" {
				_ = os.RemoveAll(committedDir)
			}
			return skillInstallExecutionResult{Text: fmt.Sprintf("Installing Skill %s failed: write scan cache: %v", displayName, err)}
		}
	}
	if err := h.getSkillExecutor().Register(*skill); err != nil {
		if committedDir != "" {
			_ = os.RemoveAll(committedDir)
		}
		return skillInstallExecutionResult{Text: fmt.Sprintf("Registering Skill %s failed: %v", displayName, err)}
	}

	// Refresh skill BM25 index so the router picks up the new skill.
	if h.getAppToolRouter() != nil {
		h.getAppToolRouter().RefreshSkillIndex()
	}

	// Audit log.
	_ = h.getAuditLog() // ensure
	if h.getAuditLog() != nil {
		riskLevel := security.RiskLow
		policyAction := security.PolicyAllow
		if installScanReport != nil {
			riskLevel = installScanReport.FinalLevel
			if h.app != nil {
				policyAction = h.app.skillInstallFinalAuditActionForSource(installScanReport, source)
			} else if installScanReport.NeedsUserReview() {
				policyAction = security.PolicyAudit
			}
		}
		_ = h.getAuditLog().Log(security.AuditEntry{
			Timestamp:    time.Now(),
			Action:       security.AuditActionHubSkillInstall,
			ToolName:     source + "_skill_install",
			RiskLevel:    riskLevel,
			PolicyAction: policyAction,
			Result:       fmt.Sprintf("installed skill %s from %s", displayName, source),
		})
	}

	sendStatus(fmt.Sprintf("鈻讹笍 姝ｅ湪鎵ц Skill: %s ...", skill.Name))
	execResult, execErr := h.getSkillExecutor().Execute(skill.Name)
	if execErr != nil {
		log.Printf("[skill-auto] execute skill %s failed: %v", skill.Name, execErr)
		// Mark as needs_setup so the skill list shows it's not ready to use.
		// The user/agent can retry with manage_skill(action="run") after fixing.
		if updateErr := h.getSkillExecutor().UpdateStatus(skill.Name, "needs_setup"); updateErr != nil {
			log.Printf("[skill-auto] failed to mark skill %s as needs_setup: %v", skill.Name, updateErr)
		}
		return skillInstallExecutionResult{Text: fmt.Sprintf("Skill %s was installed but initial execution failed (marked as needs_setup): %v\nRun manage_skill(action=\"run\", name=\"%s\") to retry.", skill.Name, execErr, skill.Name)}
	}
	return skillInstallExecutionResult{Text: fmt.Sprintf("Skill %s was installed and executed.\n%s", skill.Name, execResult), Success: true}
}
