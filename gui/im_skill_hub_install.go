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
// converts it to an NLSkillEntry ready for local registration via the shared
// corelib materialisation path (steps / files / agent_skill_md).
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

	var peek struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	_ = json.Unmarshal(data, &peek)
	return cskill.ParseSkillHubDownloadJSON(data, cskill.HubDownloadOptions{
		HubURL:  endpoint,
		SkillID: firstNonEmpty(peek.ID, peek.Name),
		Source:  "hub",
	})
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
	if h.app != nil {
		if err := ensureSkillEvolutionMutationAdmission(h.app, skill.Name); err != nil {
			return skillInstallExecutionResult{Text: fmt.Sprintf("Installing Skill %s blocked: %v", displayName, err), SilentFailure: true}
		}
	}
	h.getSkillExecutor().suspendStatusOverlayPersistence()
	defer h.getSkillExecutor().resumeStatusOverlayPersistence()
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
	if err := ctx.Err(); err != nil {
		cskill.CleanupStaging(stagingDir)
		return skillInstallExecutionResult{Text: fmt.Sprintf("Installing Skill %s cancelled: %v", displayName, err), SilentFailure: true}
	}
	// File-backed installs require the desktop App transaction coordinator.
	// Standalone IM handlers do not have durable config/compensation recovery;
	// fail closed instead of publishing a directory that cannot be recovered.
	if stagingDir != "" && h.app == nil {
		cskill.CleanupStaging(stagingDir)
		return skillInstallExecutionResult{Text: fmt.Sprintf("Installing Skill %s blocked: durable transaction context unavailable", displayName), SilentFailure: true}
	}
	// Fresh directory-backed installs use the shared transaction boundary. Keep
	// replacement of an existing identity on the legacy adapter below until its
	// source-specific .prev/update semantics are migrated.
	if stagingDir != "" && h.app != nil {
		existing := h.app.installedSkillForInstall(skill)
		if existing != nil && skillInstallAlreadyCurrent(existing, skill) {
			cskill.CleanupStaging(stagingDir)
			skill = existing
			return h.executeInstalledSkill(ctx, skill, displayName, platform, userID, policyOwnerID, sendStatus)
		}
		if existing == nil && h.app.skillNameAlreadyRegistered(skill.Name) {
			cskill.CleanupStaging(stagingDir)
			return skillInstallExecutionResult{Text: fmt.Sprintf("Installing Skill %s refused: name is registered under a different identity", displayName), SilentFailure: true}
		}
		requestID := fmt.Sprintf("evo_im_install_%d", time.Now().UnixNano())
		var commitErr error
		if existing == nil {
			commitErr = h.app.commitStagedSkillInstall(ctx, skill, stagingDir, source, installScanReport, requestID, skillEvolutionConfigRevision(h.app))
		} else {
			commitErr = h.app.commitStagedSkillInstallWithExisting(ctx, skill, stagingDir, source, installScanReport, requestID, skillEvolutionConfigRevision(h.app), existing)
		}
		if commitErr != nil {
			return skillInstallExecutionResult{Text: fmt.Sprintf("Installing Skill %s failed: %v", displayName, commitErr), SilentFailure: true}
		}
		return h.executeInstalledSkill(ctx, skill, displayName, platform, userID, policyOwnerID, sendStatus)
	}
	preNormalizeScanHash := ""
	var installCompensation cskill.EvolutionCompensationRecord
	installTxnActive := false
	installRequestID := ""
	if stagingDir != "" && h.app != nil {
		finalRoot, rootErr := h.app.primarySkillsDir()
		if rootErr != nil {
			cskill.CleanupStaging(stagingDir)
			return skillInstallExecutionResult{Text: fmt.Sprintf("Installing Skill %s failed: %v", displayName, rootErr)}
		}
		installRequestID = fmt.Sprintf("evo_im_install_%d", time.Now().UnixNano())
		installCompensation = cskill.NewEvolutionCompensationRecord(installRequestID, skill.Name, "install", "", nil, false, h.getSkillExecutor().loadSkills(), "im_install_rollback_incomplete")
		finalDir := cskill.PlannedSkillDir(finalRoot, skill.Name)
		installCompensation.SetCreatedDirectories([]string{finalDir})
		if _, statErr := os.Stat(finalDir); statErr == nil {
			installCompensation.SetDirectoryBackupIntent(finalDir, finalDir+".prev")
		} else if !os.IsNotExist(statErr) {
			cskill.CleanupStaging(stagingDir)
			return skillInstallExecutionResult{Text: fmt.Sprintf("Installing Skill %s failed: inspect destination: %v", displayName, statErr)}
		}
		if err := cskill.PersistEvolutionCompensation(installCompensation); err != nil {
			cskill.CleanupStaging(stagingDir)
			return skillInstallExecutionResult{Text: fmt.Sprintf("Installing Skill %s failed: persist rollback snapshot: %v", displayName, err)}
		}
		if err := cskill.RecordEvolutionEventStrict("skill:definition_install_started", map[string]string{
			"skill": skill.Name, "action": "install", "decision": "pending", "via": "im",
			"request_id": installRequestID, "attempt": "1", "config_revision": skillEvolutionConfigRevision(h.app),
			"schema_version": "2", "evidence_mode": "none",
		}, "desktop"); err != nil {
			_ = cskill.ClearEvolutionCompensation(installRequestID, skill.Name, "install")
			cskill.CleanupStaging(stagingDir)
			return skillInstallExecutionResult{Text: fmt.Sprintf("Installing Skill %s failed: audit preflight: %v", displayName, err)}
		}
		installTxnActive = true
		defer func() {
			if !installTxnActive {
				return
			}
			if rollbackErr := cskill.RestoreEvolutionCompensation(installCompensation,
				func(skills []corelib.NLSkillEntry) error { return h.getSkillExecutor().restoreSkillsSnapshot(skills) },
				func() error { return h.app.refreshSkillIndexesAfterMutationChecked(installCompensation.Skill) }); rollbackErr != nil {
				installCompensation.LastError = rollbackErr.Error()
				installCompensation.TransactionState = "audit_pending"
				installCompensation.CleanupStatus = "pending"
				_ = cskill.PersistEvolutionCompensation(installCompensation)
			} else {
				installCompensation.TransactionState = "rolled_back"
				installCompensation.CleanupStatus = "pending"
				if clearErr := cskill.ClearEvolutionCompensation(installCompensation.RequestID, installCompensation.Skill, "install"); clearErr != nil {
					installCompensation.LastError = clearErr.Error()
					_ = cskill.ReplaceEvolutionCompensation(installCompensation)
				}
			}
		}()
	}
	if stagingDir != "" {
		finalDir, err := cskill.CommitStaging(stagingDir, skill.Name)
		if err != nil {
			cskill.CleanupStaging(stagingDir)
			return skillInstallExecutionResult{Text: fmt.Sprintf("Installing Skill %s failed: %v", displayName, err)}
		}
		skill.SkillDir = finalDir
		// Enrich the durable snapshot after the filesystem move. The initial
		// intent closes the pre-move crash window; these flags make recovery
		// authoritative when an update leaves both the replacement and .prev.
		if h.app != nil {
			if _, statErr := os.Stat(finalDir + ".prev"); statErr == nil {
				installCompensation.SetDirectoryBackup(finalDir, finalDir+".prev", true)
			}
			installCompensation.SetDirectoryPublished(true)
			if err := cskill.PersistEvolutionCompensation(installCompensation); err != nil {
				return skillInstallExecutionResult{Text: fmt.Sprintf("Installing Skill %s failed: persist post-move rollback snapshot: %v", displayName, err)}
			}
		}
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
			return skillInstallExecutionResult{Text: fmt.Sprintf("Installing Skill %s failed: write scan cache: %v", displayName, err)}
		}
	}
	if err := h.getSkillExecutor().Register(*skill); err != nil {
		return skillInstallExecutionResult{Text: fmt.Sprintf("Registering Skill %s failed: %v", displayName, err)}
	}

	// Refresh skill BM25 index so the router picks up the new skill.
	if h.getAppToolRouter() != nil {
		if h.app != nil {
			if err := h.app.refreshSkillIndexesAfterMutationChecked(skill.Name, *skill); err != nil {
				return skillInstallExecutionResult{Text: fmt.Sprintf("Installing Skill %s failed: refresh index: %v", displayName, err)}
			}
		} else {
			if err := h.getAppToolRouter().RefreshSkillIndexChecked(); err != nil {
				return skillInstallExecutionResult{Text: fmt.Sprintf("Installing Skill %s failed: refresh index: %v", displayName, err)}
			}
		}
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
	if installTxnActive {
		if err := cskill.RecordEvolutionEventStrict("skill:definition_installed", map[string]string{
			"skill": skill.Name, "action": "install", "decision": "applied", "via": "im",
			"request_id": installRequestID, "attempt": "1", "config_revision": skillEvolutionConfigRevision(h.app),
			"schema_version": "2", "evidence_mode": "none",
		}, "desktop"); err != nil {
			return skillInstallExecutionResult{Text: fmt.Sprintf("Skill %s installed but final audit is pending: %v", displayName, err)}
		}
		installCompensation.TransactionState = "committed"
		installCompensation.CleanupStatus = "pending"
		installCompensation.FailureReason = "post_commit_cleanup_pending"
		if err := cskill.ReplaceEvolutionCompensation(installCompensation); err != nil {
			installTxnActive = false
			return skillInstallExecutionResult{Text: fmt.Sprintf("Skill %s committed but cleanup state could not be persisted: %v", displayName, err)}
		}
		installTxnActive = false
		if err := cskill.ClearEvolutionCompensation(installCompensation.RequestID, installCompensation.Skill, "install"); err != nil {
			installCompensation.LastError = err.Error()
			installCompensation.FailureReason = "post_commit_cleanup_failed"
			_ = cskill.ReplaceEvolutionCompensation(installCompensation)
			return skillInstallExecutionResult{Text: fmt.Sprintf("Skill %s installed but rollback cleanup is pending: %v", displayName, err)}
		}
	}

	return h.executeInstalledSkill(ctx, skill, displayName, platform, userID, policyOwnerID, sendStatus)
}

// executeInstalledSkill runs an already-installed entry. It is deliberately
// separate from registration so an idempotent install can reuse the existing
// authoritative entry without re-registering or rewriting it.
func (h *IMMessageHandler) executeInstalledSkill(ctx context.Context, skill *corelib.NLSkillEntry, displayName, platform, userID, policyOwnerID string, sendStatus func(string)) skillInstallExecutionResult {
	if h == nil || h.getSkillExecutor() == nil || skill == nil {
		return skillInstallExecutionResult{Text: fmt.Sprintf("Skill %s is unavailable", displayName), SilentFailure: true}
	}
	if err := ctx.Err(); err != nil {
		return skillInstallExecutionResult{Text: fmt.Sprintf("Skill %s execution cancelled: %v", displayName, err), SilentFailure: true}
	}
	if sendStatus != nil {
		sendStatus(fmt.Sprintf("正在执行 Skill: %s ...", skill.Name))
	}
	execResult, execErr := h.getSkillExecutor().ExecuteInstalledWithArgs(*skill, nil)
	if execErr != nil {
		log.Printf("[skill-auto] execute skill %s failed: %v", skill.Name, execErr)
		if updateErr := h.getSkillExecutor().UpdateStatus(skill.Name, "needs_setup"); updateErr != nil {
			log.Printf("[skill-auto] failed to mark skill %s as needs_setup: %v", skill.Name, updateErr)
		}
		return skillInstallExecutionResult{Text: fmt.Sprintf("Skill %s was installed but initial execution failed (marked as needs_setup): %v\nRun manage_skill(action=\"run\", name=\"%s\") to retry.", skill.Name, execErr, skill.Name)}
	}
	return skillInstallExecutionResult{Text: fmt.Sprintf("Skill %s was installed and executed.\n%s", skill.Name, execResult), Success: true}
}
