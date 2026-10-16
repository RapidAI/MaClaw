package guiapp

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
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
	if !automaticCapabilityGapInstallEnabled {
		return skillInstallExecutionResult{
			Text:          "automatic capability-gap Skill installation is disabled; use the reviewed Skills/Marketplace install flow",
			SilentFailure: true,
		}
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
		if sendStatus != nil {
			sendStatus(fmt.Sprintf("正在从 GitHub 安装: %s ...", best.Name))
		}
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
		if sendStatus != nil {
			sendStatus(fmt.Sprintf("正在安装: %s ...", best.Name))
		}
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
	if sendStatus != nil {
		sendStatus(fmt.Sprintf("正在安装: %s ...", best.Name))
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
	return h.registerSkillWithoutExecution(ctx, skill, best.Name, "auto_hub", platform, userID, policyOwnerID, sendStatus)
}

// registerSkillWithoutExecution is like registerAndExecuteSkill but stops after
// registration — it does NOT call Execute(). The LLM decides when to run the skill.
//
// Every install-only mutation is routed through the App-owned shared committers.
// Keeping this adapter thin is important: capability-gap installs must never
// grow a second config/index/audit transaction that can diverge from the
// explicit GUI install flow.
func (h *IMMessageHandler) registerSkillWithoutExecution(ctx context.Context, skill *corelib.NLSkillEntry, displayName, source string, platform, userID, policyOwnerID string, sendStatus func(string)) skillInstallExecutionResult {
	if h == nil || h.app == nil || skill == nil || h.getSkillExecutor() == nil {
		return skillInstallExecutionResult{Text: fmt.Sprintf("Found skill %s but SkillExecutor is not initialized", displayName)}
	}
	if err := ensureSkillEvolutionMutationAdmission(h.app, skill.Name); err != nil {
		return skillInstallExecutionResult{Text: fmt.Sprintf("Installing Skill %s blocked: %v", displayName, err), SilentFailure: true}
	}
	if err := cskill.CheckEvolutionCompensationQueue(); err != nil {
		return skillInstallExecutionResult{Text: fmt.Sprintf("Installing Skill %s blocked: compensation queue unavailable: %v", displayName, err), SilentFailure: true}
	}

	h.getSkillExecutor().suspendStatusOverlayPersistence()
	defer h.getSkillExecutor().resumeStatusOverlayPersistence()

	stagingDir := skillStagingDir(skill.SkillDir)
	var installScanReport *cskill.ScanReport
	if !h.app.isRiskGuardrailOffMode() {
		scanner := NewSkillSecurityScanner(h.app, nil)
		scanReport := scanner.ScanInstallStaged(ctx, skill, skill.SkillDir, sendStatus)
		installScanReport = scanReport
		if h.app.skillInstallScanShouldBlockForSource(scanReport, source) {
			cskill.CleanupStaging(stagingDir)
			return skillInstallExecutionResult{Text: FormatScanReportForUser(scanReport, displayName), SilentFailure: true}
		}
		if h.app.skillInstallReviewNeedsConfirmationForSource(scanReport, source) {
			confirmed := h.confirmRiskSkillInstall(ctx, displayName, source, scanReport.FinalLevel, scanReport.PatternAssessment.Factors, platform, userID)
			if !confirmed {
				cskill.CleanupStaging(stagingDir)
				return skillInstallExecutionResult{Text: FormatScanReportForUser(scanReport, displayName), SilentFailure: true}
			}
		}
	}

	if sendStatus != nil {
		sendStatus(fmt.Sprintf("Registering Skill: %s ...", skill.Name))
	}
	if err := ctx.Err(); err != nil {
		cskill.CleanupStaging(stagingDir)
		return skillInstallExecutionResult{Text: fmt.Sprintf("Installing Skill %s cancelled: %v", displayName, err), SilentFailure: true}
	}

	requestID := fmt.Sprintf("evo_im_install_only_%d", time.Now().UnixNano())
	if stagingDir != "" {
		existing := h.app.installedSkillForInstall(skill)
		if existing != nil && skillInstallAlreadyCurrent(existing, skill) {
			cskill.CleanupStaging(stagingDir)
			return skillInstallExecutionResult{Text: fmt.Sprintf("Skill「%s」已是最新版本，无需重复安装。", displayName), Success: true}
		}
		if existing == nil && h.app.skillNameAlreadyRegistered(skill.Name) {
			cskill.CleanupStaging(stagingDir)
			return skillInstallExecutionResult{Text: fmt.Sprintf("Installing Skill %s refused: name is registered under a different identity", displayName), SilentFailure: true}
		}
		var commitErr error
		if existing == nil {
			commitErr = h.app.commitStagedSkillInstall(ctx, skill, stagingDir, source, installScanReport, requestID, skillEvolutionConfigRevision(h.app))
		} else {
			commitErr = h.app.commitStagedSkillInstallWithExisting(ctx, skill, stagingDir, source, installScanReport, requestID, skillEvolutionConfigRevision(h.app), existing)
		}
		if commitErr != nil {
			return skillInstallExecutionResult{Text: fmt.Sprintf("Installing Skill %s failed: %v", displayName, commitErr), SilentFailure: true}
		}
		go h.app.installSkillDepsIfMissing(skill.SkillDir, skill.Name)
	} else {
		existing := h.app.installedSkillForInstall(skill)
		if existing != nil {
			// Preserve the authoritative display name when a stable ID resolves
			// to an existing config-only definition.
			skill.Name = existing.Name
		} else if h.app.skillNameAlreadyRegistered(skill.Name) {
			return skillInstallExecutionResult{Text: fmt.Sprintf("Installing Skill %s refused: name is registered under a different identity", displayName), SilentFailure: true}
		}
		via := "im"
		if strings.TrimSpace(source) != "" {
			via += "_" + strings.TrimSpace(source)
		}
		if err := h.app.commitNLSkillDefinitionAfterAdmission(ctx, *skill, "install", "skill:definition_installed", true, via, installScanReport); err != nil {
			return skillInstallExecutionResult{Text: fmt.Sprintf("Installing Skill %s failed: %v", displayName, err), SilentFailure: true}
		}
	}

	log.Printf("[skill-install-only] skill %q committed from %s (not auto-executed)", skill.Name, source)
	return skillInstallExecutionResult{
		Text:    fmt.Sprintf("Skill「%s」已安装。LLM 可在下一轮对话中通过 manage_skill(action=\"run\", name=\"%s\") 执行。", skill.Name, skill.Name),
		Success: true,
	}
}
