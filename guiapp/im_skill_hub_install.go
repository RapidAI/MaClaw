package guiapp

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
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

// syncSkillHubTools registers search_and_install_skill when this handler has
// an App, and drops it when the App is gone. Discovery lists the name from
// the registry and does not rebuild the legacy tool cache, so the discovery
// preset calls this only while an App is present. A list built without an
// App leaves a tool that a test or host registered itself.
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
			HandlerCtx: func(ctx context.Context, args map[string]interface{}, onProgress tool.ProgressCallback) string {
				return h.recordSkillSearchInstall(ctx, args, onProgress)
			},
			HandlerProg: func(args map[string]interface{}, onProgress tool.ProgressCallback) string {
				return h.recordSkillSearchInstall(context.Background(), args, onProgress)
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
		stagingDir, stagingErr := cskill.PrepareStagingDir(firstNonEmpty(best.ID, best.Name, "clawhub-skill"))
		if stagingErr != nil {
			return skillInstallExecutionResult{Text: fmt.Sprintf("Found ClawHub skill %s but staging failed: %v", best.Name, stagingErr)}
		}
		skill, dlErr := downloadClawHubSkill(ctx, best.ID, stagingDir)
		if dlErr != nil {
			cskill.CleanupStaging(stagingDir)
			return skillInstallExecutionResult{Text: fmt.Sprintf("Found ClawHub skill %s but download failed: %v", best.Name, dlErr)}
		}
		skill.Source = "auto_clawhub"
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

// recordSkillSearchInstall runs the search and, when this call carries a
// sink, keeps the procedure directory the text result would otherwise drop.
func (h *IMMessageHandler) recordSkillSearchInstall(ctx context.Context, args map[string]interface{}, onProgress tool.ProgressCallback) string {
	result := h.executeSkillSearchInstall(args, onProgress)
	if sink := skillInstallResultFromContext(ctx); sink != nil {
		*sink = result
	}
	return result.Text
}

// downloadClawHubSkill fetches a ClawHub skill folder into targetDir.
// The directory is the package: SKILL.md, sibling documents, and any scripts.
func downloadClawHubSkill(ctx context.Context, slug, targetDir string) (*corelib.NLSkillEntry, error) {
	client := cskill.DefaultHubClient()
	return client.DownloadClawHubWithOptions(ctx, slug, cskill.ClawHubDownloadOptions{TargetDir: targetDir})
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

		if h.app != nil {
			action, recordedWithoutChannel := h.skillInstallDecidedAction(scanReport, source, platform, ownerID)
			switch action {
			case security.PolicyDeny:
				cskill.CleanupStaging(stagingDir)
				level, summary := skillInstallReportLevelSummary(scanReport)
				if h.getAuditLog() != nil {
					_ = h.getAuditLog().Log(security.AuditEntry{
						Timestamp:    time.Now(),
						Action:       security.AuditActionHubSkillReject,
						ToolName:     source + "_skill_install",
						RiskLevel:    level,
						PolicyAction: security.PolicyDeny,
						Result:       fmt.Sprintf("policy blocked %s-risk skill %s: %s", level, displayName, summary),
					})
				}
				return skillInstallExecutionResult{
					Text:          FormatScanReportForUser(scanReport, displayName) + "\n" + localizedSkillInstallBlockedMessage(h.skillConfirmLang(), displayName, false),
					SilentFailure: true,
				}
			case security.PolicyAsk:
				confirmed := h.confirmRiskSkillInstall(
					ctx, displayName, source, scanReport.FinalLevel, skillInstallRiskFactors(scanReport), platform, userID,
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
			default:
				if recordedWithoutChannel && h.getAuditLog() != nil {
					_ = h.getAuditLog().Log(security.AuditEntry{
						Timestamp:    time.Now(),
						Action:       security.AuditActionHubSkillInstall,
						ToolName:     source + "_skill_install",
						RiskLevel:    scanReport.FinalLevel,
						PolicyAction: security.PolicyAudit,
						Result:       fmt.Sprintf("no confirmation channel; recorded %s-risk skill %s and allowed by current policy: %s", scanReport.FinalLevel, displayName, scanReport.Summary),
					})
				}
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
			if !installedSkillIsRunnable(existing) {
				status := strings.TrimSpace(existing.Status)
				if status == "" {
					status = "inactive"
				}
				return skillInstallExecutionResult{
					Text:          fmt.Sprintf("Skill %s is already installed but not runnable (status %s). It was not executed.", displayName, status),
					SilentFailure: true,
				}
			}
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
	// Config-only imports (for example a GitHub skill with no local assets)
	// still mutate the authoritative registry. They must use the same durable
	// committer as directory installs; standalone Register plus a best-effort
	// index/audit sequence can leave config and evidence divergent.
	if h.app == nil {
		return skillInstallExecutionResult{Text: fmt.Sprintf("Installing Skill %s blocked: durable transaction context unavailable", displayName), SilentFailure: true}
	}
	via := "im"
	if strings.TrimSpace(source) != "" {
		via += "_" + strings.TrimSpace(source)
	}
	if err := h.app.commitNLSkillDefinitionAfterAdmission(
		ctx, *skill, "install", "skill:definition_installed", true, via, installScanReport,
	); err != nil {
		return skillInstallExecutionResult{Text: fmt.Sprintf("Installing Skill %s failed: %v", displayName, err), SilentFailure: true}
	}
	return h.executeInstalledSkill(ctx, skill, displayName, platform, userID, policyOwnerID, sendStatus)
}

// installedSkillIsRunnable is the status gate for a repeat install. A skill
// left at needs_setup already failed its first run; searching it again must
// not start that run over.
func installedSkillIsRunnable(entry *corelib.NLSkillEntry) bool {
	return entry != nil && normalizeSkillEntryStatus(entry.Status) == skillEntryStatusActive
}

// skillInitialExecutionFailureNote is the tool result after a just-installed
// skill fails its first run. A truncated model body is a transport failure.
// Telling the model to retry manage_skill turns that into another install loop
// instead of letting the task continue with the tools already on the surface.
func skillInitialExecutionFailureNote(name string, execErr error) string {
	if skillExecutionFailedOnTruncatedModelResponse(execErr) {
		return fmt.Sprintf("Skill %s was installed but its first run stopped because the model response was truncated (marked as needs_setup). It will not run automatically. Continue the original task with the tools already available. Do not install or run this skill again for this failure.", name)
	}
	return fmt.Sprintf("Skill %s was installed but initial execution failed (marked as needs_setup): %v\nRun manage_skill(action=\"run\", name=\"%s\") to retry.", name, execErr, name)
}

// installedSkillCredentialRefusal is an installed program the runner would
// also reject for an unset env var. It is not an empty package: craft_tool
// is not the next step, and naming it sends the model off to write a
// replacement that needs the same variable.
const installedSkillCredentialRefusal = "required credentials are empty"

// installedSkillInstructionDocument is a package whose program is SKILL.md.
// The runner would turn that into one craft_tool step. Adopting the document
// is how this turn follows it. An empty directory is not this reason.
const installedSkillInstructionDocument = "instruction document"

// installedSkillUnreadablePackage is a directory this process could not list.
// That is not evidence the package is empty, so it must not be marked
// needs_setup and must not send the model to craft_tool. A later turn can
// read the same folder and adopt it.
const installedSkillUnreadablePackage = "package could not be read"

// skillInitialExecutionUnrunnableNote is the tool result when a just-installed
// skill must not be executed. The first line is the shared tool-failure
// marker, so a text-only handler classifies it the same way the Success bit
// does: the catalog miss stays open. An empty package says to call craft_tool.
// A missing credential does not. manage_skill is not on the discovery surface,
// and naming it starts another install. A real step that ran and failed still
// uses skillInitialExecutionFailureNote.
func skillInitialExecutionUnrunnableNote(name, reason string) string {
	return fmt.Sprintf("failed: Skill %s was installed but it cannot run (%s) (marked as needs_setup). It will not run automatically. %s", name, reason, skillInitialExecutionUnrunnableFollow(reason))
}

func skillInitialExecutionUnpersistedUnrunnableNote(name, reason string, statusErr error) string {
	return fmt.Sprintf("failed: Skill %s was installed but it cannot run (%s). Its needs_setup status could not be persisted (%v). It will not run automatically. %s", name, reason, statusErr, skillInitialExecutionUnrunnableFollow(reason))
}

func skillInitialExecutionUnrunnableFollow(reason string) string {
	if reason == installedSkillCredentialRefusal {
		return "Do not install or run this skill again for this failure."
	}
	return "Call craft_tool for the original task. Do not install or run this skill again for this failure."
}

// skillInitialExecutionUnreadableNote is the tool result when the package
// directory could not be listed. The failed: prefix keeps the catalog miss
// open. The status is unchanged, and the note does not name craft_tool:
// an unread tree is not an empty package.
func skillInitialExecutionUnreadableNote(name string) string {
	return fmt.Sprintf("failed: Skill %s was installed but its directory could not be read. Its status was left unchanged. Continue the original task with the tools already available. Do not install or run this skill again for this failure.", name)
}

func skillInitialExecutionUnpersistedNote(name string, execErr, statusErr error) string {
	if skillExecutionFailedOnTruncatedModelResponse(execErr) {
		return fmt.Sprintf("Skill %s was installed but its first run stopped because the model response was truncated. Its needs_setup status could not be persisted (%v). Continue the original task with the tools already available. Do not install or run this skill again for this failure.", name, statusErr)
	}
	return fmt.Sprintf("Skill %s was installed but initial execution failed: %v\nIts needs_setup status could not be persisted (%v); stop automatic use and review it manually before retrying.", name, execErr, statusErr)
}

func skillExecutionFailedOnTruncatedModelResponse(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	// A command that prints "unexpected EOF" is that command's failure.
	// Only the model-body parser uses the "parse response" wrapper.
	if !strings.Contains(msg, "parse response:") {
		return false
	}
	return strings.Contains(msg, "unexpected end of json input") || strings.Contains(msg, "unexpected eof")
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
	// An empty marketplace package, or one whose required credentials are
	// unset, used to enter the runner anyway. The 2026-11-06 picture skill
	// was a craft_tool step over an empty directory: the runner started the
	// OpenAI proxy and spent minutes in simple_llm, then told the model to
	// call manage_skill, which discovery does not expose.
	if reason := installedSkillAutoRunRefusal(skill); reason != "" {
		if reason == installedSkillInstructionDocument {
			log.Printf("[skill-auto] adopt instruction skill %s", skill.Name)
			return installedSkillInstructionResult(skill)
		}
		if reason == installedSkillUnreadablePackage {
			// A list error is not an empty package. needs_setup would skip
			// the skill after the directory can be read again.
			log.Printf("[skill-auto] skip execute skill %s: %s", skill.Name, reason)
			name := ""
			if skill != nil {
				name = skill.Name
			}
			return skillInstallExecutionResult{Text: skillInitialExecutionUnreadableNote(name)}
		}
		log.Printf("[skill-auto] skip execute skill %s: %s", skill.Name, reason)
		return h.finishInstalledSkillUnrunnable(skill, reason)
	}
	if sendStatus != nil {
		sendStatus(fmt.Sprintf("正在执行 Skill: %s ...", skill.Name))
	}
	execResult, execErr := h.getSkillExecutor().ExecuteInstalledWithArgs(*skill, nil)
	if execErr != nil {
		log.Printf("[skill-auto] execute skill %s failed: %v", skill.Name, execErr)
		// Initial execution failure is a lifecycle transition, not an executor
		// cache update. Route it through App so the config overlay, checked index,
		// strict audit, and durable compensation share one result boundary.
		if h.app == nil || h.app.skillExecutor != h.getSkillExecutor() {
			statusErr := fmt.Errorf("durable App status transaction unavailable")
			log.Printf("[skill-auto] failed to mark skill %s as needs_setup: %v", skill.Name, statusErr)
			return skillInstallExecutionResult{Text: skillInitialExecutionUnpersistedNote(skill.Name, execErr, statusErr)}
		}
		if updateErr := h.app.SetNLSkillStatus(skill.Name, "needs_setup"); updateErr != nil {
			log.Printf("[skill-auto] failed to mark skill %s as needs_setup: %v", skill.Name, updateErr)
			return skillInstallExecutionResult{Text: skillInitialExecutionUnpersistedNote(skill.Name, execErr, updateErr)}
		}
		return skillInstallExecutionResult{Text: skillInitialExecutionFailureNote(skill.Name, execErr)}
	}
	return skillInstallExecutionResult{Text: fmt.Sprintf("Skill %s was installed and executed.\n%s", skill.Name, execResult), Success: true}
}

func (h *IMMessageHandler) finishInstalledSkillUnrunnable(skill *corelib.NLSkillEntry, reason string) skillInstallExecutionResult {
	name := ""
	if skill != nil {
		name = skill.Name
	}
	if h == nil || h.app == nil || skill == nil || h.app.skillExecutor != h.getSkillExecutor() {
		statusErr := fmt.Errorf("durable App status transaction unavailable")
		log.Printf("[skill-auto] failed to mark skill %s as needs_setup: %v", name, statusErr)
		return skillInstallExecutionResult{Text: skillInitialExecutionUnpersistedUnrunnableNote(name, reason, statusErr)}
	}
	if updateErr := h.app.SetNLSkillStatus(skill.Name, "needs_setup"); updateErr != nil {
		log.Printf("[skill-auto] failed to mark skill %s as needs_setup: %v", skill.Name, updateErr)
		return skillInstallExecutionResult{Text: skillInitialExecutionUnpersistedUnrunnableNote(skill.Name, reason, updateErr)}
	}
	return skillInstallExecutionResult{Text: skillInitialExecutionUnrunnableNote(skill.Name, reason)}
}

// installedSkillAutoRunRefusal reports why the just-installed skill must not
// be executed. A real step still runs, including one that then exits non-zero.
// Empty means run it.
func installedSkillAutoRunRefusal(entry *corelib.NLSkillEntry) string {
	if entry == nil {
		return ""
	}
	if installedSkillMissingCredentials(entry) {
		return installedSkillCredentialRefusal
	}
	if cskill.IsKnowledgeSkillType(entry.Type) {
		return "no runnable steps"
	}
	report := cskill.AssessRunnerCompatibility(entry, cskill.RunnerBackendGUI)
	// A bash step is the program. A sibling SKILL.md does not replace it.
	if report.Runnable && report.SupportedSteps > 0 && !report.CraftToolOnly {
		return ""
	}
	// A craft_tool step over a SKILL.md is the runner's picture of the
	// document, not a program. Running it spends the turn in simple_llm.
	// A loose script file does not change that: the step is still craft_tool.
	if installedSkillDirHasDocument(entry.SkillDir) && !report.HasBash {
		return installedSkillInstructionDocument
	}
	found, walkErr := installedSkillProgramScan(entry.SkillDir)
	if found && report.Runnable && report.SupportedSteps > 0 {
		return ""
	}
	if cskill.IsInstructionOnlySkillType(entry.Type) || !report.Runnable || report.StepCount == 0 || report.SupportedSteps == 0 {
		return "no runnable steps"
	}
	if report.CraftToolOnly && !found {
		// The scan could not read a directory. "No program" would mark
		// needs_setup and close the skill; the program may still be there.
		if walkErr != nil && !os.IsNotExist(walkErr) {
			return installedSkillUnreadablePackage
		}
		return "package has no runnable program"
	}
	return ""
}

func installedSkillInstructionResult(skill *corelib.NLSkillEntry) skillInstallExecutionResult {
	name := ""
	dir := ""
	if skill != nil {
		name = strings.TrimSpace(skill.Name)
		dir = strings.TrimSpace(skill.SkillDir)
	}
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	if name == "" || dir == "" || !filepath.IsAbs(dir) || findSkillMarkdownDocPath(dir) == "" {
		return skillInstallExecutionResult{Text: skillInitialExecutionUnrunnableNote(name, "package has no runnable program")}
	}
	return skillInstallExecutionResult{
		Text:          skillInitialExecutionInstructionNote(name, dir),
		Success:       true,
		ProcedureDir:  filepath.Clean(dir),
		ProcedureName: name,
	}
}

func skillInitialExecutionInstructionNote(name, dir string) string {
	return fmt.Sprintf("Skill %s is installed at %s and is this task's procedure. Follow ## Skill 使用文档 with bash, read_file, write_file, or edit_file. Read the sibling files the document names. Do not call discover_tool, search_and_install_skill, generate_pdf, or manage_skill.", name, dir)
}

// installedSkillMissingCredentials reports an env requirement the GUI runner
// would also reject. BuildRunCheckContextForRunner marks OPENAI_API_KEY,
// OPENAI_BASE_URL, and OPENAI_MODEL provided when the local proxy would
// inject them, and ExtractRequirements turns a command-shaped name such as
// python into a command requirement. Reading os.Getenv alone skipped those
// skills and marked them needs_setup before the runner could start.
func installedSkillMissingCredentials(entry *corelib.NLSkillEntry) bool {
	if entry == nil || !installedSkillDeclaresCredentialEnv(entry) {
		return false
	}
	ctx := cskill.BuildRunCheckContextForRunner(entry, nil, cskill.RunnerBackendGUI)
	for _, req := range cskill.ExtractRequirements(entry, ctx) {
		if req.Type != "env" || req.Provided {
			continue
		}
		if strings.TrimSpace(os.Getenv(req.Name)) == "" {
			return true
		}
	}
	return false
}

func installedSkillDeclaresCredentialEnv(entry *corelib.NLSkillEntry) bool {
	if entry == nil {
		return false
	}
	for _, name := range entry.RequiredEnv {
		if strings.TrimSpace(name) != "" {
			return true
		}
	}
	for _, step := range entry.Steps {
		if step.Params == nil {
			continue
		}
		for _, key := range []string{"required_env", "requires_env", "required_environment"} {
			if _, ok := step.Params[key]; ok {
				return true
			}
		}
	}
	return false
}

// installedSkillProgramScan walks dir for a program file. A list error does
// not end the walk, so a program later in the tree is still found. readErr
// keeps a failure to read some directory. A disappeared path yields to any
// other failure: only a tree that was fully read, or that disappeared, can
// count as empty.
func installedSkillProgramScan(dir string) (found bool, readErr error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return false, nil
	}
	walkErr := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			readErr = preferSkillTreeReadErr(readErr, err)
			return nil
		}
		if d == nil {
			return nil
		}
		name := d.Name()
		if name != "." && name != ".." && strings.HasPrefix(name, ".") {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		switch strings.ToLower(filepath.Ext(name)) {
		case ".py", ".pyw", ".js", ".mjs", ".ts", ".sh", ".ps1", ".bat", ".cmd", ".exe", ".go", ".rb":
			found = true
			return fs.SkipAll
		}
		return nil
	})
	readErr = preferSkillTreeReadErr(readErr, walkErr)
	return found, readErr
}

func preferSkillTreeReadErr(current, next error) error {
	if next == nil {
		return current
	}
	if current == nil || (os.IsNotExist(current) && !os.IsNotExist(next)) {
		return next
	}
	return current
}

func installedSkillDirHasDocument(dir string) bool {
	return findSkillMarkdownDocPath(strings.TrimSpace(dir)) != ""
}

// installedSkillPackageMissing reports a recorded directory that has neither
// the skill document nor a program. A needs_setup entry with no SkillDir is
// a real failed install and stays skipped. A missing folder, or a folder
// that holds only the scan database, is an incomplete download.
func installedSkillPackageMissing(entry *corelib.NLSkillEntry) bool {
	if entry == nil {
		return false
	}
	dir := strings.TrimSpace(entry.SkillDir)
	if dir == "" {
		return false
	}
	info, err := os.Stat(dir)
	if decided, missing := installedSkillDirectoryStat(info, err); decided {
		return missing
	}
	// Stat can succeed for a directory this process cannot list. That is not
	// an incomplete download: the package may still be there, and replacing
	// it would discard a skill we failed to read.
	_, listErr := os.ReadDir(dir)
	if decided, missing := installedSkillListMissing(listErr); decided {
		return missing
	}
	// The document lookup only reads this directory. A nested directory that
	// cannot be listed is the same failure one level down: the program may
	// still be there, so the package stays. A path that only disappeared,
	// with no document and no visible program, is still incomplete.
	if installedSkillDirHasDocument(dir) {
		return false
	}
	found, walkErr := installedSkillProgramScan(dir)
	if found {
		return false
	}
	if decided, missing := installedSkillListMissing(walkErr); decided {
		return missing
	}
	return true
}

// installedSkillListMissing reports whether listing the directory already
// answered the package question. A directory that disappeared is incomplete.
// Any other list error leaves the recorded package in place.
func installedSkillListMissing(err error) (decided bool, missing bool) {
	if err == nil {
		return false, false
	}
	return true, os.IsNotExist(err)
}

// installedSkillDirectoryStat reports whether Stat already answered the
// package question. A path that is not there, or that is not a directory, is
// an incomplete download. A permission or other transient error is not: the
// folder may still hold the package, and treating it as missing would replace
// a skill that is still installed.
func installedSkillDirectoryStat(info os.FileInfo, err error) (decided bool, missing bool) {
	if err != nil {
		return true, os.IsNotExist(err)
	}
	if info == nil || !info.IsDir() {
		return true, true
	}
	return false, false
}

// installedSkillDirIsScanResidue reports a directory whose only entries are
// the skill scan database. An empty directory, a symlink, and any real file
// are not residue: those must not be deleted to make room for a publish.
func installedSkillDirIsScanResidue(dir string) bool {
	dir = strings.TrimSpace(dir)
	info, err := os.Lstat(dir)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return false
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) == 0 {
		return false
	}
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 || entry.IsDir() || !isSkillScanStoreFile(entry.Name()) {
			return false
		}
	}
	return true
}

// removeInstalledSkillScanResidue deletes that database. Windows can keep
// the file open for a moment after the scanner reads it, so a single
// RemoveAll is not enough.
func removeInstalledSkillScanResidue(dir string) error {
	var last error
	for attempt := 0; attempt < 40; attempt++ {
		err := os.RemoveAll(dir)
		if err == nil || os.IsNotExist(err) {
			return nil
		}
		last = err
		delay := time.Duration(attempt+1) * 10 * time.Millisecond
		if delay > 100*time.Millisecond {
			delay = 100 * time.Millisecond
		}
		time.Sleep(delay)
	}
	return last
}

// prepareInstalledSkillPublishDestination checks the destination before any
// residue is deleted. A stale .prev is the previous package kept for
// recovery. Deleting the scan-only directory first, then failing on that
// backup, would throw away the only leftover and leave the next install
// stuck on the same backup.
func prepareInstalledSkillPublishDestination(finalDir string, replacing bool) error {
	if _, backupErr := os.Stat(finalDir + ".prev"); backupErr == nil {
		return fmt.Errorf("skill destination has a stale previous backup: %s", filepath.Base(finalDir+".prev"))
	} else if !os.IsNotExist(backupErr) {
		return fmt.Errorf("inspect previous skill backup: %w", backupErr)
	}
	if _, statErr := os.Stat(finalDir); statErr == nil {
		if replacing {
			return nil
		}
		// A failed publish leaves the scan database and no package. That
		// directory is not another skill. Removing it lets this install
		// create the folder. A directory with a document or a program still
		// blocks. A recorded entry replaces through .prev and does not land
		// here.
		if installedSkillDirIsScanResidue(finalDir) {
			if rmErr := removeInstalledSkillScanResidue(finalDir); rmErr != nil {
				return fmt.Errorf("remove incomplete skill directory %s: %w", filepath.Base(finalDir), rmErr)
			}
			return nil
		}
		return fmt.Errorf("skill directory %q already exists", filepath.Base(finalDir))
	} else if !os.IsNotExist(statErr) {
		return fmt.Errorf("inspect skill destination: %w", statErr)
	}
	return nil
}
