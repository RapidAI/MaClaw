package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
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
		sendStatus(fmt.Sprintf("⬇️ 正在从 GitHub 安装: %s ...", best.Name))
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
		sendStatus(fmt.Sprintf("⬇️ 正在安装: %s ...", best.Name))
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
	sendStatus(fmt.Sprintf("⬇️ 正在安装: %s ...", best.Name))
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

	sendStatus(fmt.Sprintf("📦 Registering Skill: %s ...", skill.Name))
	if stagingDir != "" {
		finalDir, err := cskill.CommitStaging(stagingDir, skill.Name)
		if err != nil {
			cskill.CleanupStaging(stagingDir)
			return skillInstallExecutionResult{Text: fmt.Sprintf("Installing Skill %s failed: %v", displayName, err)}
		}
		skill.SkillDir = finalDir
		if h.app != nil {
			skill = h.app.normalizeInstalledSkill(skill)
		}
	}
	if err := h.getSkillExecutor().Register(*skill); err != nil {
		return skillInstallExecutionResult{Text: fmt.Sprintf("Registering Skill %s failed: %v", displayName, err)}
	}

	// Refresh skill BM25 index.
	if h.getAppToolRouter() != nil {
		h.getAppToolRouter().RefreshSkillIndex()
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

	log.Printf("[skill-install-only] skill %q registered from %s (not auto-executed)", skill.Name, source)
	return skillInstallExecutionResult{
		Text:    fmt.Sprintf("✅ Skill「%s」已安装。LLM 可在下一轮对话中通过 manage_skill(action=\"run\", name=\"%s\") 执行。", skill.Name, skill.Name),
		Success: true,
	}
}
