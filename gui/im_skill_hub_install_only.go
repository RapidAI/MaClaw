package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/security"
	cskill "github.com/RapidAI/CodeClaw/corelib/skill"
)

// installSkillOnly downloads and installs a skill from a search result WITHOUT
// executing it. This is used by the async capability gap path — the LLM should
// decide whether and when to execute the newly installed skill in the next turn,
// rather than having the system auto-execute in a background goroutine.
//
// This closes the safety gap: users confirmed "install", not "install AND execute".
// The LLM can then call run_skill explicitly with proper context.
func (h *IMMessageHandler) installSkillOnly(ctx context.Context, best *SkillSearchResult, query, platform, userID, policyOwnerID string, sendStatus func(string)) skillInstallExecutionResult {
	if h == nil || h.app == nil || best == nil {
		return skillInstallExecutionResult{Text: "install failed: missing handler or skill result"}
	}

	ownerID := strings.TrimSpace(policyOwnerID)
	if ownerID == "" {
		ownerID = strings.TrimSpace(userID)
	}

	// Permission checks (same as installAndExecuteSkill but only for "install" action).
	if err := h.app.ensureWorkflowAllowsRemoteToolCallForOwner(ownerID, "manage_skill", map[string]interface{}{"action": "install", "name": best.Name, "source": best.SourceKind().String(), "query": query}); err != nil {
		return skillInstallExecutionResult{Text: err.Error(), SilentFailure: true}
	}

	// Enterprise-only install policy.
	if cfg, err := h.app.LoadConfig(); err == nil {
		if reason, blocked := cfg.CapabilityMarketPolicy.RejectNonEnterpriseInstall(best.SourceKind().String(), cfg.RemoteHubURL); blocked {
			return skillInstallExecutionResult{Text: reason, SilentFailure: true}
		}
	}

	// GitHub result.
	if best.SourceKind() == skillSearchSourceGitHub {
		guardArgs := map[string]interface{}{"action": "install", "source": "github", "skill_id": best.ID, "install_ref": best.InstallRef}
		if ok, reason := h.app.enforceHubSecurityAppPolicy("manage_skill", guardArgs); !ok {
			return skillInstallExecutionResult{Text: reason, SilentFailure: true}
		}
		sendStatus(fmt.Sprintf("正在从 GitHub 安装: %s ...", best.Name))
		var imported *corelib.NLSkillEntry
		if strings.TrimSpace(best.InstallRef) != "" {
			var candidate cskill.GitHubSkillCandidate
			if err := json.Unmarshal([]byte(best.InstallRef), &candidate); err == nil && strings.TrimSpace(candidate.RawURL) != "" {
				imported, _ = cskill.NewGitHubSearcher("").ImportFromCandidate(candidate)
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
		return h.registerSkillWithoutExecution(ctx, imported, best.Name, "auto_github", platform, userID, policyOwnerID, sendStatus)
	}

	// ClawHub result.
	if best.SourceKind() == skillSearchSourceClawHub {
		guardArgs := map[string]interface{}{"action": "install", "source": "clawhub", "skill_id": best.ID, "hub_url": cskill.ClawHubMirrorURL}
		if ok, reason := h.app.enforceHubSecurityAppPolicy("manage_skill", guardArgs); !ok {
			return skillInstallExecutionResult{Text: reason, SilentFailure: true}
		}
		sendStatus(fmt.Sprintf("正在安装: %s ...", best.Name))
		skill, dlErr := downloadClawHubSkill(ctx, best.ID)
		if dlErr != nil {
			return skillInstallExecutionResult{Text: fmt.Sprintf("Found ClawHub skill %s but download failed: %v", best.Name, dlErr)}
		}
		skill.Source = "auto_clawhub"
		return h.registerSkillWithoutExecution(ctx, skill, best.Name, "auto_clawhub", platform, userID, policyOwnerID, sendStatus)
	}

	// SkillMarket result.
	if h.app != nil {
		hubURL := NewSkillMarketClient(h.app).baseURL()
		guardArgs := map[string]interface{}{"action": "install", "source": "skillhub", "skill_id": best.ID, "hub_url": hubURL}
		if ok, reason := h.app.enforceHubSecurityAppPolicy("manage_skill", guardArgs); !ok {
			return skillInstallExecutionResult{Text: reason, SilentFailure: true}
		}
	}
	sendStatus(fmt.Sprintf("正在安装: %s ...", best.Name))
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
	return h.registerSkillWithoutExecution(ctx, skill, best.Name, "auto_hub", platform, userID, policyOwnerID, sendStatus)
}

// registerSkillWithoutExecution is like registerAndExecuteSkill but stops after
// registration — it does NOT call Execute(). The LLM decides when to run the skill.
func (h *IMMessageHandler) registerSkillWithoutExecution(ctx context.Context, skill *corelib.NLSkillEntry, displayName, source string, platform, userID, policyOwnerID string, sendStatus func(string)) skillInstallExecutionResult {
	if h.getSkillExecutor() == nil {
		return skillInstallExecutionResult{Text: fmt.Sprintf("Found skill %s but SkillExecutor is not initialized", displayName)}
	}
	if h.app != nil {
		if err := ensureSkillEvolutionMutationAdmission(h.app, skill.Name); err != nil {
			return skillInstallExecutionResult{Text: fmt.Sprintf("Installing Skill %s blocked: %v", displayName, err), SilentFailure: true}
		}
		if err := cskill.CheckEvolutionCompensationQueue(); err != nil {
			return skillInstallExecutionResult{Text: fmt.Sprintf("Installing Skill %s blocked: compensation queue unavailable: %v", displayName, err), SilentFailure: true}
		}
	}
	h.getSkillExecutor().suspendStatusOverlayPersistence()
	defer h.getSkillExecutor().resumeStatusOverlayPersistence()

	stagingDir := skillStagingDir(skill.SkillDir)

	// Security scan (same as registerAndExecuteSkill).
	var installScanReport *cskill.ScanReport
	if h.app != nil && !h.app.isRiskGuardrailOffMode() {
		scanner := NewSkillSecurityScanner(h.app, nil)
		scanReport := scanner.ScanInstallStaged(ctx, skill, skill.SkillDir, sendStatus)
		installScanReport = scanReport

		if h.app.skillInstallScanShouldBlockForSource(scanReport, source) {
			cskill.CleanupStaging(stagingDir)
			return skillInstallExecutionResult{
				Text:          FormatScanReportForUser(scanReport, displayName),
				SilentFailure: true,
			}
		}

		// High-risk skills need user confirmation before installation.
		if h.app.skillInstallReviewNeedsConfirmationForSource(scanReport, source) {
			confirmed := h.confirmRiskSkillInstall(
				ctx, displayName, source, scanReport.FinalLevel, scanReport.PatternAssessment.Factors, platform, userID,
			)
			if !confirmed {
				cskill.CleanupStaging(stagingDir)
				return skillInstallExecutionResult{
					Text:          FormatScanReportForUser(scanReport, displayName),
					SilentFailure: true,
				}
			}
		}
	}

	sendStatus(fmt.Sprintf("Registering Skill: %s ...", skill.Name))
	if err := ctx.Err(); err != nil {
		cskill.CleanupStaging(stagingDir)
		return skillInstallExecutionResult{Text: fmt.Sprintf("Installing Skill %s cancelled: %v", displayName, err), SilentFailure: true}
	}
	// Install-only still mutates the filesystem, so it must have the App-owned
	// durable transaction context. Never publish an unrecoverable standalone
	// install and merely refresh an in-memory index.
	if stagingDir != "" && h.app == nil {
		cskill.CleanupStaging(stagingDir)
		return skillInstallExecutionResult{Text: fmt.Sprintf("Installing Skill %s blocked: durable transaction context unavailable", displayName), SilentFailure: true}
	}
	// New directory-backed installs use the same durable transaction as the
	// explicit GUI Hub install. Existing-version replacement remains on the
	// install-only adapter below because it has source-specific update semantics.
	if stagingDir != "" && h.app != nil {
		var existingEntry *corelib.NLSkillEntry
		for _, installed := range h.getSkillExecutor().loadSkills() {
			if installed.Name == skill.Name || installed.MatchesName(skill.Name) {
				copy := installed
				existingEntry = &copy
				break
			}
		}
		if existingEntry != nil && skillInstallAlreadyCurrent(existingEntry, skill) {
			cskill.CleanupStaging(stagingDir)
			return skillInstallExecutionResult{
				Text:    fmt.Sprintf("Skill「%s」已是最新版本，无需重复安装。", displayName),
				Success: true,
			}
		}
		if existingEntry == nil && !h.app.skillNameAlreadyRegistered(skill.Name) {
			requestID := fmt.Sprintf("evo_im_install_only_%d", time.Now().UnixNano())
			if err := h.app.commitStagedSkillInstall(ctx, skill, stagingDir, source, installScanReport, requestID, skillEvolutionConfigRevision(h.app)); err != nil {
				return skillInstallExecutionResult{Text: fmt.Sprintf("Installing Skill %s failed: %v", displayName, err), SilentFailure: true}
			}
			go h.app.installSkillDepsIfMissing(skill.SkillDir, skill.Name)
			return skillInstallExecutionResult{
				Text:    fmt.Sprintf("Skill「%s」已安装。LLM 可在下一轮对话中通过 manage_skill(action=\"run\", name=\"%s\") 执行。", skill.Name, skill.Name),
				Success: true,
			}
		}
	}
	var compensation cskill.EvolutionCompensationRecord
	transactionActive := false
	installRequestID := ""
	if stagingDir != "" {
		if h.app != nil {
			finalRoot, rootErr := h.app.primarySkillsDir()
			if rootErr != nil {
				cskill.CleanupStaging(stagingDir)
				return skillInstallExecutionResult{Text: fmt.Sprintf("Installing Skill %s failed: %v", displayName, rootErr)}
			}
			installRequestID = fmt.Sprintf("evo_im_install_only_%d", time.Now().UnixNano())
			compensation = cskill.NewEvolutionCompensationRecord(installRequestID, skill.Name, "install", "", nil, false, h.getSkillExecutor().loadSkills(), "im_install_only_rollback_incomplete")
			finalDir := cskill.PlannedSkillDir(finalRoot, skill.Name)
			compensation.SetCreatedDirectories([]string{finalDir})
			if _, statErr := os.Stat(finalDir); statErr == nil {
				compensation.SetDirectoryBackupIntent(finalDir, finalDir+".prev")
			} else if !os.IsNotExist(statErr) {
				cskill.CleanupStaging(stagingDir)
				return skillInstallExecutionResult{Text: fmt.Sprintf("Installing Skill %s failed: inspect destination: %v", displayName, statErr)}
			}
			if err := cskill.PersistEvolutionCompensation(compensation); err != nil {
				cskill.CleanupStaging(stagingDir)
				return skillInstallExecutionResult{Text: fmt.Sprintf("Installing Skill %s failed: persist rollback snapshot: %v", displayName, err)}
			}
			if err := cskill.RecordEvolutionEventStrict("skill:definition_install_started", map[string]string{
				"skill": skill.Name, "action": "install", "decision": "pending", "via": "im_install_only",
				"request_id": installRequestID, "attempt": "1", "config_revision": skillEvolutionConfigRevision(h.app),
				"schema_version": "2", "evidence_mode": "none",
			}, "desktop"); err != nil {
				_ = cskill.ClearEvolutionCompensation(installRequestID, skill.Name, "install")
				cskill.CleanupStaging(stagingDir)
				return skillInstallExecutionResult{Text: fmt.Sprintf("Installing Skill %s failed: audit preflight: %v", displayName, err)}
			}
			transactionActive = true
			defer func() {
				if !transactionActive {
					return
				}
				if rollbackErr := cskill.RestoreEvolutionCompensation(compensation, func(skills []corelib.NLSkillEntry) error { return h.getSkillExecutor().restoreSkillsSnapshot(skills) }, func() error { return h.app.refreshSkillIndexesAfterMutationChecked(compensation.Skill) }); rollbackErr != nil {
					compensation.LastError = rollbackErr.Error()
					compensation.TransactionState = "audit_pending"
					compensation.CleanupStatus = "pending"
					_ = cskill.PersistEvolutionCompensation(compensation)
				} else {
					compensation.TransactionState = "rolled_back"
					compensation.CleanupStatus = "pending"
					if clearErr := cskill.ClearEvolutionCompensation(compensation.RequestID, compensation.Skill, "install"); clearErr != nil {
						compensation.LastError = clearErr.Error()
						_ = cskill.ReplaceEvolutionCompensation(compensation)
					}
				}
			}()
		}
		finalDir, err := cskill.CommitStaging(stagingDir, skill.Name)
		if err != nil {
			cskill.CleanupStaging(stagingDir)
			return skillInstallExecutionResult{Text: fmt.Sprintf("Installing Skill %s failed: %v", displayName, err)}
		}
		skill.SkillDir = finalDir
		if h.app != nil {
			skill = h.app.normalizeInstalledSkill(skill)
			if _, statErr := os.Stat(finalDir + ".prev"); statErr == nil {
				compensation.SetDirectoryBackup(finalDir, finalDir+".prev", true)
			}
			compensation.SetDirectoryPublished(true)
			if err := cskill.PersistEvolutionCompensation(compensation); err != nil {
				return skillInstallExecutionResult{Text: fmt.Sprintf("Installing Skill %s failed: persist post-move rollback snapshot: %v", displayName, err)}
			}
		}
	}
	if err := h.getSkillExecutor().Register(*skill); err != nil {
		return skillInstallExecutionResult{Text: fmt.Sprintf("Registering Skill %s failed: %v", displayName, err)}
	}

	// Refresh skill BM25 index.
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

	// Audit log (consistent with registerAndExecuteSkill).
	if h.getAuditLog() != nil {
		riskLevel := security.RiskLow
		if installScanReport != nil {
			riskLevel = installScanReport.FinalLevel
		}
		_ = h.getAuditLog().Log(security.AuditEntry{
			Timestamp:    time.Now(),
			Action:       security.AuditActionHubSkillInstall,
			ToolName:     source + "_skill_install_only",
			RiskLevel:    riskLevel,
			PolicyAction: security.PolicyAllow,
			Result:       fmt.Sprintf("installed skill %s from %s (install-only, not auto-executed)", displayName, source),
		})
	}
	if transactionActive {
		if err := cskill.RecordEvolutionEventStrict("skill:definition_installed", map[string]string{
			"skill": skill.Name, "action": "install", "decision": "applied", "via": "im_install_only",
			"request_id": installRequestID, "attempt": "1", "config_revision": skillEvolutionConfigRevision(h.app),
			"schema_version": "2", "evidence_mode": "none",
		}, "desktop"); err != nil {
			return skillInstallExecutionResult{Text: fmt.Sprintf("Skill %s installed but final audit is pending: %v", displayName, err)}
		}
		compensation.TransactionState = "committed"
		compensation.CleanupStatus = "pending"
		compensation.FailureReason = "post_commit_cleanup_pending"
		if err := cskill.ReplaceEvolutionCompensation(compensation); err != nil {
			transactionActive = false
			return skillInstallExecutionResult{Text: fmt.Sprintf("Skill %s committed but cleanup state could not be persisted: %v", displayName, err)}
		}
		transactionActive = false
		if err := cskill.ClearEvolutionCompensation(compensation.RequestID, compensation.Skill, "install"); err != nil {
			compensation.LastError = err.Error()
			compensation.FailureReason = "post_commit_cleanup_failed"
			_ = cskill.ReplaceEvolutionCompensation(compensation)
			return skillInstallExecutionResult{Text: fmt.Sprintf("Skill %s installed but rollback cleanup is pending: %v", displayName, err)}
		}
	}

	log.Printf("[skill-install-only] skill %q registered from %s (not auto-executed)", skill.Name, source)
	return skillInstallExecutionResult{
		Text:    fmt.Sprintf("Skill「%s」已安装。LLM 可在下一轮对话中通过 manage_skill(action=\"run\", name=\"%s\") 执行。", skill.Name, skill.Name),
		Success: true,
	}
}
