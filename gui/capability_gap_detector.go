package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/llm"
	"github.com/RapidAI/CodeClaw/corelib/security"
	cskill "github.com/RapidAI/CodeClaw/corelib/skill"
)

// CapabilityGapDetector detects capability gaps in Agent responses and
// resolves them by searching SkillHub for matching Skills.
type CapabilityGapDetector struct {
	app                        *App
	hubClient                  *SkillHubClient
	skillExecutor              *SkillExecutor
	riskAssessor               *RiskAssessor
	auditLog                   *AuditLog
	llmConfig                  corelib.MaclawLLMConfig
	client                     *http.Client
	confirmCallback            func(skillName string, riskDetails string) bool
	confirmCallbackWithContext func(ctx context.Context, skillName string, riskDetails string) bool
	// unifiedClassifier is used to check whether the original user message
	// was classified as non-coding/ambiguous before applying gap detection.
	unifiedClassifier *intent.UnifiedIntentClassifier
}

type capabilityGapRuntimeContextKey struct{}

const (
	maxCapabilityGapLocalPackagedFileSize      int64 = 32 << 20
	maxTotalCapabilityGapLocalPackagedFileSize int64 = 256 << 20
)

var allowedLocalPackagedFileExts = map[string]bool{
	".css":  true,
	".go":   true,
	".html": true,
	".js":   true,
	".json": true,
	".jsx":  true,
	".md":   true,
	".mjs":  true,
	".ps1":  true,
	".py":   true,
	".sh":   true,
	".toml": true,
	".ts":   true,
	".tsx":  true,
	".txt":  true,
	".yaml": true,
	".yml":  true,
}

type capabilityGapRuntimeContext struct {
	Platform      string
	PolicyOwnerID string
}

func withCapabilityGapRuntimeContext(ctx context.Context, platform, policyOwnerID string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, capabilityGapRuntimeContextKey{}, capabilityGapRuntimeContext{
		Platform:      strings.TrimSpace(platform),
		PolicyOwnerID: strings.TrimSpace(policyOwnerID),
	})
}

func capabilityGapRuntimeFromContext(ctx context.Context) (platform, policyOwnerID string) {
	if ctx == nil {
		return "", ""
	}
	runtimeCtx, _ := ctx.Value(capabilityGapRuntimeContextKey{}).(capabilityGapRuntimeContext)
	return strings.TrimSpace(runtimeCtx.Platform), strings.TrimSpace(runtimeCtx.PolicyOwnerID)
}

// NewCapabilityGapDetector creates a new CapabilityGapDetector.
func NewCapabilityGapDetector(
	app *App,
	hubClient *SkillHubClient,
	skillExecutor *SkillExecutor,
	riskAssessor *RiskAssessor,
	auditLog *AuditLog,
	llmConfig corelib.MaclawLLMConfig,
) *CapabilityGapDetector {
	return &CapabilityGapDetector{
		app:           app,
		hubClient:     hubClient,
		skillExecutor: skillExecutor,
		riskAssessor:  riskAssessor,
		auditLog:      auditLog,
		llmConfig:     llmConfig,
		client:        &http.Client{Timeout: 30 * time.Second},
	}
}

// SetConfirmCallback sets a callback for user confirmation of critical-risk
// Skills. When set, the callback is invoked with the Skill name and risk
// details; returning true allows installation, false rejects it. When not
// set, critical-risk Skills are rejected automatically.
func (d *CapabilityGapDetector) SetConfirmCallback(cb func(skillName string, riskDetails string) bool) {
	d.confirmCallback = cb
	if cb == nil {
		d.confirmCallbackWithContext = nil
		return
	}
	d.confirmCallbackWithContext = func(ctx context.Context, skillName string, riskDetails string) bool {
		return cb(skillName, riskDetails)
	}
}

func (d *CapabilityGapDetector) SetConfirmCallbackWithContext(cb func(ctx context.Context, skillName string, riskDetails string) bool) {
	d.confirmCallbackWithContext = cb
	if cb == nil {
		d.confirmCallback = nil
		return
	}
	d.confirmCallback = func(skillName string, riskDetails string) bool {
		return cb(context.Background(), skillName, riskDetails)
	}
}

func (d *CapabilityGapDetector) confirmSkillReview(ctx context.Context, skillName, riskDetails string) bool {
	if d.confirmCallbackWithContext != nil {
		return d.confirmCallbackWithContext(ctx, skillName, riskDetails)
	}
	if d.confirmCallback != nil {
		return d.confirmCallback(skillName, riskDetails)
	}
	// A review callback is the only authoritative approval channel for a
	// risk-gated install.  Never treat a missing callback as approval: headless
	// callers must fail closed and let the caller surface a pending review.
	return false
}

// SetUnifiedClassifier sets the UIC instance for intent-aware gap detection.
func (d *CapabilityGapDetector) SetUnifiedClassifier(uic *intent.UnifiedIntentClassifier) {
	d.unifiedClassifier = uic
}

// gapKeywords are heuristic keywords indicating a capability gap when LLM is
// not configured.
var gapKeywords = []string{
	"无法", "不支持", "cannot", "unable", "don't have", "没有能力",
}

// Detect returns true if the LLM response indicates a capability gap.
// When an LLM is configured it asks the model to judge; otherwise it falls
// back to simple keyword matching.
//
// Short-circuit: very long responses (>500 chars) are almost certainly
// detailed summaries or reports, not "I can't do this" signals. Skip
// detection entirely to avoid false positives from informational uses of
// keywords like "无法" or "不支持".
func (d *CapabilityGapDetector) Detect(llmResponse string) bool {
	return d.DetectWithContext(context.Background(), llmResponse)
}

func (d *CapabilityGapDetector) DetectWithContext(ctx context.Context, llmResponse string) bool {
	if contextErr(ctx) != nil {
		return false
	}
	trimmed := strings.TrimSpace(llmResponse)
	if utf8.RuneCountInString(trimmed) > 500 {
		return false
	}
	if d.isLLMConfigured() {
		return d.llmDetectGapWithContext(ctx, llmResponse)
	}
	// When UIC is available, skip gapKeywords fallback — rely solely on
	// LLM-based detection (which is unavailable here). The UIC already
	// classified the user message; gapKeywords are redundant.
	if d.unifiedClassifier != nil {
		return false
	}
	// Last resort: gapKeywords fallback when no LLM and no UIC.
	lower := strings.ToLower(llmResponse)
	for _, kw := range gapKeywords {
		if strings.Contains(lower, strings.ToLower(kw)) {
			return true
		}
	}
	return false
}

// Resolve attempts to fill a capability gap by searching SkillHub, installing
// the best matching Skill, and executing it. It returns the installed Skill
// name, execution result, and any error. Empty skillName means no suitable
// Skill was found.
func (d *CapabilityGapDetector) Resolve(
	ctx context.Context,
	userMessage string,
	conversationHistory []map[string]interface{},
	sendStatus func(string),
) (skillName string, result string, err error) {
	// Developer mode still records scan findings; policy helpers suppress blocking.
	// Capability-gap resolution is an installation transaction, not a best-effort
	// helper. Keep a durable compensation record alive from the first filesystem
	// mutation until the final strict audit succeeds. This closes the historical
	// window where Resolve committed a directory and only then registered it.
	var installCompensation cskill.EvolutionCompensationRecord
	var installTxnActive bool
	var overlaySuspended bool
	finalSkillsRoot := ""
	requestID := fmt.Sprintf("evo_capability_gap_%d", time.Now().UnixNano())
	if d == nil {
		return "", "", fmt.Errorf("capability gap detector not initialized")
	}
	defer func() {
		if installTxnActive && err != nil && d != nil && d.app != nil {
			rollbackErr := cskill.RestoreEvolutionCompensation(installCompensation,
				func(skills []corelib.NLSkillEntry) error {
					if d.skillExecutor == nil {
						return fmt.Errorf("skill executor not initialized")
					}
					return d.skillExecutor.restoreSkillsSnapshot(skills)
				}, func() error { return d.app.refreshSkillIndexesAfterMutationChecked(installCompensation.Skill) })
			if rollbackErr != nil {
				installCompensation.LastError = fmt.Sprintf("%v (rollback: %v)", err, rollbackErr)
				installCompensation.TransactionState = "audit_pending"
				installCompensation.CleanupStatus = "pending"
				_ = cskill.PersistEvolutionCompensation(installCompensation)
				err = fmt.Errorf("%v; install rollback incomplete; compensation queued", err)
			} else {
				installCompensation.TransactionState = "rolled_back"
				installCompensation.CleanupStatus = "pending"
				if clearErr := cskill.ClearEvolutionCompensation(installCompensation.RequestID, installCompensation.Skill, "install"); clearErr != nil {
					installCompensation.LastError = clearErr.Error()
					_ = cskill.ReplaceEvolutionCompensation(installCompensation)
					err = fmt.Errorf("%v; install rolled back but compensation cleanup failed: %w", err, clearErr)
				} else {
					err = fmt.Errorf("%v; install rolled back", err)
				}
			}
		}
		if overlaySuspended && d != nil && d.skillExecutor != nil {
			d.skillExecutor.resumeStatusOverlayPersistence()
		}
	}()
	if d.app == nil {
		// Standalone detector instances retain their historical read-only/test
		// behavior, but desktop installs must always pass the queue admission gate.
	} else if err = ensureSkillEvolutionMutationAdmission(d.app, "capability-gap"); err != nil {
		return "", "", err
	}
	if d.skillExecutor == nil {
		return "", "", fmt.Errorf("skill executor not initialized")
	}
	originalSkills := d.skillExecutor.loadSkills()
	prepareInstallCompensation := func(entry *corelib.NLSkillEntry, finalDir string) error {
		if d.app == nil || entry == nil {
			return nil
		}
		if err := ensureSkillEvolutionMutationAdmission(d.app, entry.Name); err != nil {
			return err
		}
		installCompensation = cskill.NewEvolutionCompensationRecord(requestID, entry.Name, "install", "", nil, false, originalSkills, "capability_gap_install_rollback_incomplete")
		installCompensation.SetCreatedDirectories([]string{finalDir})
		if _, statErr := os.Stat(finalDir); statErr == nil {
			installCompensation.SetDirectoryBackupIntent(finalDir, finalDir+".prev")
		} else if !os.IsNotExist(statErr) {
			return fmt.Errorf("inspect skill destination: %w", statErr)
		}
		if err := cskill.PersistEvolutionCompensation(installCompensation); err != nil {
			return fmt.Errorf("persist capability-gap install compensation: %w", err)
		}
		if err := cskill.RecordEvolutionEventStrict("skill:definition_install_started", map[string]string{
			"skill": entry.Name, "action": "install", "decision": "pending", "via": "capability_gap",
			"request_id": requestID, "attempt": "1", "config_revision": skillEvolutionConfigRevision(d.app),
			"schema_version": "2", "evidence_mode": "none",
		}, "desktop"); err != nil {
			_ = cskill.ClearEvolutionCompensation(requestID, entry.Name, "install")
			return fmt.Errorf("capability-gap install audit preflight failed: %w", err)
		}
		d.skillExecutor.suspendStatusOverlayPersistence()
		overlaySuspended = true
		installTxnActive = true
		return nil
	}

	// Step 1: Extract capability query from user message.
	query := d.extractCapabilityQuery(ctx, userMessage, conversationHistory)
	if query == "" {
		query = userMessage
	}

	// Determine allowed sources for filtering.
	var allowedSources []string
	if d.app != nil {
		allowedSources = d.app.GetAllowedSkillSources()
	}
	var blockedSearch []string

	// Step 2: Search SkillHub (if allowed).
	var candidates []HubSkillMeta
	if isAllowedSkillSourceList("skillhub", allowedSources) {
		allowed := true
		if d.app != nil {
			guardArgs := map[string]interface{}{"query": query, "source": "skillhub", "hub_url": NewSkillMarketClient(d.app).baseURL()}
			if ok, reason := d.app.enforceHubSecurityAppPolicy("search_and_install_skill", guardArgs); !ok {
				blockedSearch = append(blockedSearch, "skillhub: "+reason)
				allowed = false
			}
		}
		if allowed {
			sendStatus("正在搜索可用的 Skill...")
			candidates, err = d.hubClient.Search(ctx, query)
			if err != nil {
				return "", "", fmt.Errorf("search hub: %w", err)
			}
		}
	}
	lang := d.skillConfirmLang()
	if len(candidates) == 0 {
		// Fallback: search GitHub for skill.yaml files (if allowed).
		if !isAllowedSkillSourceList("github", allowedSources) {
			if len(blockedSearch) > 0 {
				return "", "", fmt.Errorf("skill search blocked by security policy: %s", strings.Join(blockedSearch, "; "))
			}
			return "", "", nil
		}
		if d.app != nil {
			guardArgs := map[string]interface{}{"query": query, "source": "github", "url": "https://github.com"}
			if ok, reason := d.app.enforceHubSecurityAppPolicy("search_and_install_skill", guardArgs); !ok {
				blockedSearch = append(blockedSearch, "github: "+reason)
				return "", "", fmt.Errorf("skill search blocked by security policy: %s", strings.Join(blockedSearch, "; "))
			}
		}
		sendStatus("SkillHub 未找到匹配技能，正在搜索 GitHub...")
		gs := cskill.NewGitHubSearcher("")
		ghCandidates, ghErr := gs.SearchGitHub(query)
		if ghErr != nil || len(ghCandidates) == 0 {
			return "", "", nil
		}
		ghCandidates = filterGitHubSkillCandidatesForIntent(userMessage, ghCandidates)
		if len(ghCandidates) == 0 {
			return "", "", nil
		}
		// Import the first candidate.
		if d.app != nil {
			guardArgs := map[string]interface{}{"action": "install", "source": "github", "skill_id": ghCandidates[0].RepoFullName, "install_ref": ghCandidates[0].RawURL}
			if guardArgs["install_ref"] == "" {
				guardArgs["install_ref"] = ghCandidates[0].RepoURL
			}
			if ok, reason := d.app.enforceHubSecurityAppPolicy("manage_skill", guardArgs); !ok {
				return "", "", fmt.Errorf("github skill install blocked by security policy: %s", reason)
			}
		}
		imported, impErr := gs.ImportFromCandidate(ghCandidates[0])
		if impErr != nil {
			sendStatus(fmt.Sprintf("GitHub 技能导入失败: %v", impErr))
			return "", "", nil
		}

		// Security review: pattern scan plus optional agent scan; policy decides whether findings block.
		// Developer mode records scan findings but never blocks installation.
		{
			var scanReport *cskill.ScanReport
			if d.app == nil || !d.app.isRiskGuardrailOffMode() {
				scanner := NewSkillSecurityScanner(d.app, nil)
				scanReport = scanner.ScanInstallStaged(ctx, imported, imported.SkillDir, sendStatus)
			}
			if d.app != nil && d.app.skillInstallScanShouldBlock(scanReport) {
				if d.auditLog != nil {
					_ = d.auditLog.Log(security.AuditEntry{
						Timestamp:    time.Now(),
						Action:       security.AuditActionHubSkillReject,
						ToolName:     "github_skill_install",
						RiskLevel:    scanReport.FinalLevel,
						PolicyAction: security.PolicyDeny,
						Result:       fmt.Sprintf("pre-install policy blocked github skill %s: %s", imported.Name, scanReport.Summary),
					})
				}
				return "", "", fmt.Errorf("GitHub Skill security scan blocked installation by current policy")
			}
			if d.app != nil && d.app.skillInstallReviewNeedsConfirmation(scanReport) {
				riskDetails := FormatScanReportForUser(scanReport, imported.Name)
				// A scan that requires confirmation must have an explicit approval
				// callback.  Missing UI/approval plumbing is not consent.
				confirmed := false
				if d.confirmCallbackWithContext != nil || d.confirmCallback != nil {
					sendStatus(localizedSkillInstallReviewStatus(lang, scanReport.Summary))
					confirmed = d.confirmSkillReview(ctx, imported.Name, riskDetails)
				} else {
					sendStatus(localizedSkillInstallNoConfirmationStatus(lang, imported.Name))
				}
				if !confirmed {
					if d.auditLog != nil {
						_ = d.auditLog.Log(security.AuditEntry{
							Timestamp:    time.Now(),
							Action:       security.AuditActionHubSkillReject,
							ToolName:     "github_skill_install",
							RiskLevel:    scanReport.FinalLevel,
							PolicyAction: security.PolicyDeny,
							Result:       fmt.Sprintf("rejected github skill %s: %s", imported.Name, scanReport.Summary),
						})
					}
					return "", "", fmt.Errorf("%s", localizedSkillInstallScanRejectedError(lang, true))
				}
				if d.auditLog != nil {
					_ = d.auditLog.Log(security.AuditEntry{
						Timestamp:    time.Now(),
						Action:       security.AuditActionHubSkillInstall,
						ToolName:     "github_skill_install",
						RiskLevel:    scanReport.FinalLevel,
						PolicyAction: security.PolicyUserOverride,
						Result:       fmt.Sprintf("user confirmed github skill %s, scanned_by=%s", imported.Name, scanReport.ScannedBy),
					})
				}
			}
			if err := writeSkillScanCacheForReportStatus(imported, imported.SkillDir, "", scanReport, skillScanCacheStatusAllowed); err != nil {
				if d.app != nil {
					d.app.log(fmt.Sprintf("[capability-gap] failed to write install scan cache for %s: %v", imported.Name, err))
				}
				return "", "", fmt.Errorf("write skill scan cache: %w", err)
			}
		}

		// Override source to indicate auto-installation by CapabilityGapDetector.
		imported.Source = "auto_github"
		sendStatus(localizedSkillInstallInstallingStatus(lang, imported.Name, true))
		if d.app != nil {
			// GitHub definitions are usually config-only (no local directory), but
			// they still need the same durable config/index/final-audit boundary as
			// directory installs. This removes the last capability-gap GitHub path
			// that could register and audit through a bespoke sequence.
			committer := &cskill.SkillCommitter{
				SkillLoader: func() []corelib.NLSkillEntry { return d.skillExecutor.loadSkills() },
				SkillSaver: func(entries []corelib.NLSkillEntry) error {
					return d.skillExecutor.withSkillListMutate(func() error { return d.skillExecutor.saveSkills(entries) })
				},
				IndexRefresher: func() error { return d.app.refreshSkillIndexesAfterMutationChecked(imported.Name, *imported) },
				FinalAuditor: func(event string, data map[string]string) error {
					return cskill.RecordEvolutionEventStrict(event, data, "desktop")
				},
				ConfigRevision: skillEvolutionConfigRevision(d.app),
				AllowCreate:    true,
			}
			result := committer.Commit(ctx, imported.Name, imported, "skill:definition_installed", map[string]string{
				"skill": imported.Name, "action": "install", "decision": "applied", "via": "capability_gap_github",
				"request_id": requestID, "attempt": "1", "config_revision": skillEvolutionConfigRevision(d.app),
				"schema_version": "2", "evidence_mode": "none",
			})
			if result.State != "committed" || result.CleanupStatus != "clear" {
				return "", "", fmt.Errorf("github install not committed: state=%s cleanup_status=%s reason=%s", result.State, result.CleanupStatus, result.FailureReason)
			}
		} else {
			if err := d.skillExecutor.Register(*imported); err != nil {
				return "", "", fmt.Errorf("register github skill: %w", err)
			}
		}

		execResult, execErr := d.skillExecutor.ExecuteInstalledWithArgs(*imported, skillExecutionRunArgs(userMessage))
		return imported.Name, execResult, execErr
	}

	// Step 3: Select best matching Skill.
	candidates = filterHubSkillMetaForIntent(userMessage, candidates)
	chosen := d.llmSelectBestSkill(ctx, userMessage, candidates)
	if chosen == nil {
		return "", "", nil
	}

	// Step 4: Download Skill into staging; final install happens after scan.
	sendStatus(localizedSkillInstallInstallingStatus(lang, chosen.Name, false))
	if d.app != nil {
		guardArgs := map[string]interface{}{"action": "install", "source": "skillhub", "skill_id": chosen.ID, "hub_url": chosen.HubURL}
		if ok, reason := d.app.enforceHubSecurityAppPolicy("manage_skill", guardArgs); !ok {
			return "", "", fmt.Errorf("hub skill install blocked by security policy: %s", reason)
		}
	}
	stagingDir, err := cskill.PrepareStagingDir(firstNonEmpty(chosen.ID, chosen.Name, "capability-gap-skill"))
	if err != nil {
		return "", "", fmt.Errorf("create skill staging dir: %w", err)
	}
	skill, err := d.hubClient.InstallToDir(ctx, chosen.ID, chosen.HubURL, stagingDir)
	if err != nil {
		cskill.CleanupStaging(stagingDir)
		return "", "", fmt.Errorf("install skill: %w", err)
	}
	if skill == nil {
		cskill.CleanupStaging(stagingDir)
		return "", "", fmt.Errorf("skill hub returned an empty skill")
	}

	// Step 5: Security review: pattern scan plus optional agent scan; policy decides whether findings block.
	// Developer mode records scan findings but never blocks installation.
	var scanReport *cskill.ScanReport
	{
		if d.app == nil || !d.app.isRiskGuardrailOffMode() {
			scanner := NewSkillSecurityScanner(d.app, nil)
			scanReport = scanner.ScanInstallStaged(ctx, skill, stagingDir, sendStatus)
		}
		if d.app != nil && d.app.skillInstallScanShouldBlockForSource(scanReport, "skillhub") {
			cskill.CleanupStaging(stagingDir)
			if d.auditLog != nil {
				_ = d.auditLog.Log(security.AuditEntry{
					Timestamp:    time.Now(),
					Action:       security.AuditActionHubSkillReject,
					ToolName:     "hub_skill_install",
					RiskLevel:    scanReport.FinalLevel,
					PolicyAction: security.PolicyDeny,
					Result:       fmt.Sprintf("pre-install policy blocked skill %s: %s", chosen.Name, scanReport.Summary),
				})
			}
			return "", "", fmt.Errorf("Skill security scan blocked installation by current policy")
		}
		if d.app != nil && d.app.skillInstallReviewNeedsConfirmationForSource(scanReport, "skillhub") {
			riskDetails := FormatScanReportForUser(scanReport, chosen.Name)
			// A scan that requires confirmation must have an explicit approval
			// callback.  Missing UI/approval plumbing is not consent.
			confirmed := false
			if d.confirmCallbackWithContext != nil || d.confirmCallback != nil {
				sendStatus(localizedSkillInstallReviewStatus(lang, scanReport.Summary))
				confirmed = d.confirmSkillReview(ctx, chosen.Name, riskDetails)
			} else {
				sendStatus(localizedSkillInstallNoConfirmationStatus(lang, chosen.Name))
			}
			if !confirmed {
				cskill.CleanupStaging(stagingDir)
				if d.auditLog != nil {
					_ = d.auditLog.Log(security.AuditEntry{
						Timestamp:    time.Now(),
						Action:       security.AuditActionHubSkillReject,
						ToolName:     "hub_skill_install",
						RiskLevel:    scanReport.FinalLevel,
						PolicyAction: security.PolicyDeny,
						Result:       fmt.Sprintf("rejected skill %s: %s", chosen.Name, scanReport.Summary),
					})
				}
				return "", "", fmt.Errorf("%s", localizedSkillInstallScanRejectedError(lang, false))
			}
			if d.auditLog != nil {
				_ = d.auditLog.Log(security.AuditEntry{
					Timestamp:    time.Now(),
					Action:       security.AuditActionHubSkillInstall,
					ToolName:     "hub_skill_install",
					RiskLevel:    scanReport.FinalLevel,
					PolicyAction: security.PolicyUserOverride,
					Result:       fmt.Sprintf("user confirmed skill %s, scanned_by=%s", chosen.Name, scanReport.ScannedBy),
				})
			}
		}
	}

	// A fresh Hub package is a directory-backed create. Reuse the shared
	// committer so this path has the same durable compensation, checked index,
	// final-audit and post-commit cleanup semantics as explicit GUI installs.
	// Existing identities remain on the legacy adapter below until their
	// source-specific replacement semantics are migrated as well.
	if d.app != nil && strings.TrimSpace(skill.SkillDir) != "" {
		var existingEntry *corelib.NLSkillEntry
		for _, installed := range d.skillExecutor.loadSkills() {
			if installed.Name == skill.Name || installed.MatchesName(skill.Name) {
				copy := installed
				existingEntry = &copy
				break
			}
		}
		if existingEntry != nil && skillInstallAlreadyCurrent(existingEntry, skill) {
			cskill.CleanupStaging(stagingDir)
			skill = existingEntry
			skillName = skill.Name
			sendStatus(localizedSkillInstallExecutingStatus(lang, skill.Name))
			execResult, execErr := d.skillExecutor.ExecuteInstalledWithArgs(*skill, skillExecutionRunArgs(userMessage))
			go d.autoRate(ctx, chosen.ID, execResult, execErr)
			return skill.Name, execResult, execErr
		}
		if existingEntry == nil && d.app.skillNameAlreadyRegistered(skill.Name) {
			cskill.CleanupStaging(stagingDir)
			return "", "", fmt.Errorf("capability-gap install refused: skill name %q is already registered under a different identity", skill.Name)
		}
		skill.Source = "auto_hub"
		var commitErr error
		if existingEntry == nil {
			commitErr = d.app.commitStagedSkillInstall(ctx, skill, skill.SkillDir, "capability_gap", scanReport, requestID, skillEvolutionConfigRevision(d.app))
		} else {
			commitErr = d.app.commitStagedSkillInstallWithExisting(ctx, skill, skill.SkillDir, "capability_gap", scanReport, requestID, skillEvolutionConfigRevision(d.app), existingEntry)
		}
		if commitErr != nil {
			return "", "", fmt.Errorf("capability-gap install commit failed: %w", commitErr)
		}
		skillName = skill.Name
		sendStatus(localizedSkillInstallExecutingStatus(lang, skill.Name))
		execResult, execErr := d.skillExecutor.ExecuteInstalledWithArgs(*skill, skillExecutionRunArgs(userMessage))
		go d.autoRate(ctx, chosen.ID, execResult, execErr)
		return skill.Name, execResult, execErr
	}

	var finalDir string
	if d.app != nil {
		var rootErr error
		finalSkillsRoot, rootErr = d.app.primarySkillsDir()
		if rootErr != nil {
			cskill.CleanupStaging(stagingDir)
			return "", "", rootErr
		}
		// Persist the rollback intent before moving the staged directory. The
		// initial snapshot closes the crash window; the post-move update below
		// records whether a .prev backup was created.
		if prepErr := prepareInstallCompensation(skill, cskill.PlannedSkillDir(finalSkillsRoot, skill.Name)); prepErr != nil {
			cskill.CleanupStaging(stagingDir)
			return "", "", prepErr
		}
	}
	var commitErr error
	if finalSkillsRoot != "" {
		finalDir, commitErr = cskill.CommitStagingToDir(stagingDir, skill.Name, finalSkillsRoot)
	} else {
		finalDir, commitErr = cskill.CommitStaging(stagingDir, skill.Name)
	}
	if commitErr != nil {
		cskill.CleanupStaging(stagingDir)
		return "", "", fmt.Errorf("commit staged skill: %w", commitErr)
	}
	skill.SkillDir = finalDir
	if d.app != nil {
		if _, statErr := os.Stat(finalDir + ".prev"); statErr == nil {
			installCompensation.SetDirectoryBackup(finalDir, finalDir+".prev", true)
		}
		installCompensation.SetDirectoryPublished(true)
		if persistErr := cskill.PersistEvolutionCompensation(installCompensation); persistErr != nil {
			return "", "", fmt.Errorf("persist post-move capability-gap compensation: %w", persistErr)
		}
	}
	preNormalizeScanHash := ""
	if scanReport != nil {
		if hash, err := skillContentHash(skill); err == nil {
			preNormalizeScanHash = hash
		} else if d.app != nil {
			d.app.log(fmt.Sprintf("[capability-gap] failed to hash approved pre-normalize skill %s: %v", skill.Name, err))
		}
	}
	if d.app != nil {
		skill = d.app.normalizeInstalledSkill(skill)
	}
	if scanReport != nil && preNormalizeScanHash != "" {
		if err := writeSkillScanCacheForReportStatus(skill, skill.SkillDir, preNormalizeScanHash, scanReport, skillScanCacheStatusAllowed); err != nil {
			if d.app != nil {
				d.app.log(fmt.Sprintf("[capability-gap] failed to write install scan cache for %s: %v", skill.Name, err))
			}
			return "", "", fmt.Errorf("write skill scan cache: %w", err)
		}
	}

	// Step 6: Register to local SkillExecutor.
	// Override source to indicate auto-installation by CapabilityGapDetector.
	skill.Source = "auto_hub"
	sendStatus("正在注册 Skill...")
	if err := d.skillExecutor.Register(*skill); err != nil {
		return "", "", fmt.Errorf("register skill: %w", err)
	}
	if d.app != nil {
		if err := d.app.refreshSkillIndexesAfterMutationChecked(skill.Name, *skill); err != nil {
			return "", "", fmt.Errorf("refresh capability-gap skill index: %w", err)
		}
	}

	// Final strict audit is the commit point. Do not execute or publish before
	// this succeeds; a failed audit must still be recoverable by the defer.
	if d.auditLog != nil {
		riskLevel := security.RiskLow
		policyAction := security.PolicyAllow
		if scanReport != nil {
			riskLevel = scanReport.FinalLevel
			if d.app != nil {
				policyAction = d.app.skillInstallFinalAuditActionForSource(scanReport, "skillhub")
			} else if scanReport.NeedsUserReview() {
				policyAction = security.PolicyAudit
			}
		}
		_ = d.auditLog.Log(security.AuditEntry{
			Timestamp:    time.Now(),
			Action:       security.AuditActionHubSkillInstall,
			ToolName:     "hub_skill_install",
			RiskLevel:    riskLevel,
			PolicyAction: policyAction,
			Result:       fmt.Sprintf("installed skill %s from %s", skill.Name, chosen.HubURL),
		})
	}
	if d.app != nil {
		if err := cskill.RecordEvolutionEventStrict("skill:definition_installed", map[string]string{
			"skill": skill.Name, "action": "install", "decision": "applied", "via": "capability_gap",
			"request_id": requestID, "attempt": "1", "config_revision": skillEvolutionConfigRevision(d.app),
			"schema_version": "2", "evidence_mode": "none",
		}, "desktop"); err != nil {
			return "", "", fmt.Errorf("capability-gap install audit commit failed: %w", err)
		}
		installCompensation.TransactionState = "committed"
		installCompensation.CleanupStatus = "pending"
		installCompensation.FailureReason = "post_commit_cleanup_pending"
		if err := cskill.ReplaceEvolutionCompensation(installCompensation); err != nil {
			installTxnActive = false
			return "", "", fmt.Errorf("capability-gap install committed but cleanup state could not be persisted: %w", err)
		}
		installTxnActive = false
		if err := cskill.ClearEvolutionCompensation(requestID, skill.Name, "install"); err != nil {
			installCompensation.LastError = err.Error()
			installCompensation.FailureReason = "post_commit_cleanup_failed"
			_ = cskill.ReplaceEvolutionCompensation(installCompensation)
			return "", "", fmt.Errorf("capability-gap install committed but compensation cleanup failed: %w", err)
		}
	}

	// Step 7: Execute immediately, after the installation has committed.
	sendStatus(localizedSkillInstallExecutingStatus(lang, skill.Name))
	execResult, execErr := d.skillExecutor.ExecuteInstalledWithArgs(*skill, skillExecutionRunArgs(userMessage))

	// Step 8: Auto-rate the skill based on execution result.
	go d.autoRate(ctx, chosen.ID, execResult, execErr)

	return skill.Name, execResult, execErr
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

// isLLMConfigured returns true when the LLM URL and Model are set.
func (d *CapabilityGapDetector) isLLMConfigured() bool {
	return strings.TrimSpace(d.llmConfig.URL) != "" && strings.TrimSpace(d.llmConfig.Model) != ""
}

// doLLMChat sends a chat completion request following the same pattern as
// IMMessageHandler.doLLMRequest.
func (d *CapabilityGapDetector) doLLMChat(messages []map[string]interface{}) (string, error) {
	return d.doLLMChatWithContext(context.Background(), messages)
}

func (d *CapabilityGapDetector) doLLMChatWithContext(ctx context.Context, messages []map[string]interface{}) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	// Convert []map[string]interface{} to []interface{} for the shared helper
	msgs := make([]interface{}, len(messages))
	for i, m := range messages {
		msgs[i] = m
	}

	ctx = llm.WithRequestTrace(ctx, llm.RequestTrace{Caller: "capability-gap"})
	result, err := doSimpleLLMRequest(ctx, d.llmConfig, msgs, d.client, 30*time.Second)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(result.Content), nil
}

// llmDetectGap asks the LLM whether the response indicates a capability gap.
func (d *CapabilityGapDetector) llmDetectGap(llmResponse string) bool {
	return d.llmDetectGapWithContext(context.Background(), llmResponse)
}

func (d *CapabilityGapDetector) llmDetectGapWithContext(ctx context.Context, llmResponse string) bool {
	if contextErr(ctx) != nil {
		return false
	}
	messages := []map[string]interface{}{
		{"role": "system", "content": "你是一个判断助手。用户会给你一段 AI 助手的回复，请判断这段回复是否表明 AI 助手缺少某种能力或工具来完成用户的请求。只回答 yes 或 no。"},
		{"role": "user", "content": llmResponse},
	}
	answer, err := d.doLLMChatWithContext(ctx, messages)
	if err != nil {
		if contextErr(ctx) != nil {
			return false
		}
		// Fallback to heuristic on LLM error.
		lower := strings.ToLower(llmResponse)
		for _, kw := range gapKeywords {
			if strings.Contains(lower, strings.ToLower(kw)) {
				return true
			}
		}
		return false
	}
	return strings.Contains(strings.ToLower(answer), "yes")
}

// extractCapabilityQuery calls the LLM to distill a search query from the
// user message and conversation history. Returns userMessage directly if LLM
// is not configured or the call fails.
func (d *CapabilityGapDetector) extractCapabilityQuery(ctx context.Context, userMessage string, history []map[string]interface{}) string {
	if contextErr(ctx) != nil {
		return ""
	}
	if !d.isLLMConfigured() {
		return userMessage
	}

	prompt := fmt.Sprintf(
		"根据以下用户消息，提炼出一个简短的能力需求描述（用于搜索工具/插件），只返回搜索关键词，不要解释：\n\n用户消息: %s",
		userMessage,
	)
	messages := []map[string]interface{}{
		{"role": "system", "content": "你是一个搜索查询提炼助手。将用户的请求转化为简短的搜索关键词。只返回关键词，不要其他内容。"},
		{"role": "user", "content": prompt},
	}
	answer, err := d.doLLMChatWithContext(ctx, messages)
	if err != nil || strings.TrimSpace(answer) == "" {
		if contextErr(ctx) != nil {
			return ""
		}
		return userMessage
	}
	return answer
}

// llmSelectBestSkill asks the LLM to pick the best Skill from candidates.
// Returns the first candidate if LLM is not configured or the call fails.
func (d *CapabilityGapDetector) llmSelectBestSkill(ctx context.Context, userMessage string, candidates []HubSkillMeta) *HubSkillMeta {
	if len(candidates) == 0 {
		return nil
	}
	if contextErr(ctx) != nil {
		return nil
	}
	if !d.isLLMConfigured() {
		return &candidates[0]
	}

	var sb strings.Builder
	for i, c := range candidates {
		sb.WriteString(fmt.Sprintf("%d. %s — %s (tags: %s, trust: %s)\n",
			i+1, c.Name, c.Description, strings.Join(c.Tags, ","), c.TrustLevel))
	}

	prompt := fmt.Sprintf(
		"用户请求: %s\n\n候选 Skill 列表:\n%s\n请选择最匹配用户请求的 Skill，只返回序号（数字）。",
		userMessage, sb.String(),
	)
	messages := []map[string]interface{}{
		{"role": "system", "content": "你是一个工具选择助手。根据用户请求从候选列表中选择最匹配的工具。只返回序号数字，不要其他内容。"},
		{"role": "user", "content": prompt},
	}
	answer, err := d.doLLMChatWithContext(ctx, messages)
	if err != nil {
		if contextErr(ctx) != nil {
			return nil
		}
		return &candidates[0]
	}

	// Parse the index from the answer.
	answer = strings.TrimSpace(answer)
	var idx int
	if _, err := fmt.Sscanf(answer, "%d", &idx); err == nil && idx >= 1 && idx <= len(candidates) {
		return &candidates[idx-1]
	}
	return &candidates[0]
}

func filterHubSkillMetaForIntent(userMessage string, candidates []HubSkillMeta) []HubSkillMeta {
	if len(candidates) == 0 {
		return candidates
	}
	userIntent := extractUserIntentCategory(userMessage)
	filtered := candidates[:0]
	for _, candidate := range candidates {
		skillText := strings.TrimSpace(candidate.Name + " " + candidate.Description)
		if isTaskCompatibleWithSkillCandidate(userIntent, userMessage, skillText) {
			filtered = append(filtered, candidate)
		} else {
			log.Printf("[capability-gap] rejected intent-incompatible hub skill candidate %q for query intent %q", candidate.Name, userIntent)
		}
	}
	return filtered
}

func filterGitHubSkillCandidatesForIntent(userMessage string, candidates []cskill.GitHubSkillCandidate) []cskill.GitHubSkillCandidate {
	if len(candidates) == 0 {
		return candidates
	}
	userIntent := extractUserIntentCategory(userMessage)
	filtered := candidates[:0]
	for _, candidate := range candidates {
		skillText := strings.TrimSpace(candidate.RepoFullName + " " + candidate.Description)
		if isTaskCompatibleWithSkillCandidate(userIntent, userMessage, skillText) {
			filtered = append(filtered, candidate)
		} else {
			log.Printf("[capability-gap] rejected intent-incompatible github skill candidate %q for query intent %q", candidate.RepoFullName, userIntent)
		}
	}
	return filtered
}

// autoRate evaluates the execution result via LLM and submits a 1-5 rating.
// Runs in a goroutine — errors are silently ignored.
func (d *CapabilityGapDetector) autoRate(ctx context.Context, skillID string, execResult string, execErr error) {
	if d.hubClient == nil {
		return
	}
	cfg, err := d.app.LoadConfig()
	if err != nil {
		return
	}
	maclawID := cfg.RemoteMachineID
	if maclawID == "" {
		return
	}

	score := d.llmEvaluateScore(execResult, execErr)
	_ = d.hubClient.Rate(ctx, skillID, maclawID, score)
}

// llmEvaluateScore asks the LLM to rate execution quality 1-5.
// Falls back to heuristic if LLM is unavailable.
func (d *CapabilityGapDetector) llmEvaluateScore(execResult string, execErr error) int {
	// Heuristic fallback.
	if !d.isLLMConfigured() {
		if execErr != nil {
			return 2
		}
		return 4
	}

	summary := execResult
	if execErr != nil {
		summary += "\n[error] " + execErr.Error()
	}
	if len(summary) > 2000 {
		summary = summary[:2000]
	}

	messages := []map[string]interface{}{
		{"role": "system", "content": "你是一个评分助手。根据 Skill 的执行结果评分 1-5 分。1=完全失败，2=大部分失败，3=部分成功，4=基本成功，5=完美完成。只返回一个数字。"},
		{"role": "user", "content": fmt.Sprintf("执行结果:\n%s", summary)},
	}
	answer, err := d.doLLMChat(messages)
	if err != nil {
		if execErr != nil {
			return 2
		}
		return 4
	}

	var score int
	if _, err := fmt.Sscanf(strings.TrimSpace(answer), "%d", &score); err == nil && score >= 1 && score <= 5 {
		return score
	}
	if execErr != nil {
		return 2
	}
	return 4
}

// AutoPublishSkill publishes a locally created skill to the hub so other
// MaClaws can discover and use it. Called after a skill is created and tested.
// It scans steps for dependency hints and packages local files from
// ~/.maclaw/data/skills/<name>/ into the upload payload.
func (d *CapabilityGapDetector) AutoPublishSkill(ctx context.Context, entry corelib.NLSkillEntry, sendStatus func(string)) error {
	if d.hubClient == nil {
		return fmt.Errorf("hub client not initialized")
	}

	sendStatus(fmt.Sprintf("正在发布 Skill「%s」到 SkillHub...", entry.Name))

	// Build the hub skill structure.
	steps := make([]hubSkillStep, 0, len(entry.Steps))
	for _, s := range entry.Steps {
		steps = append(steps, hubSkillStep{
			Action:  s.Action,
			Params:  s.Params,
			OnError: s.OnError,
		})
	}

	cfg, _ := d.app.LoadConfig()
	author := cfg.RemoteMachineID
	if author == "" {
		author = "anonymous"
	}

	// Scan steps for dependency hints.
	deps := d.scanDependencies(entry.Steps)

	// Package local files from ~/.maclaw/data/skills/<name>/.
	skillDir := d.localSkillDir(entry.Name)
	files := d.packageLocalFilesFromDir(skillDir)

	// Auto-publish has no interactive approval channel. Strict mode blocks
	// high/critical reports; other modes record the finding and allow publish.
	scanEntry := entry
	scanEntry.SkillDir = skillDir
	var report *cskill.ScanReport
	if d.app == nil || !d.app.isRiskGuardrailOffMode() {
		scanner := NewSkillSecurityScanner(d.app, nil)
		report = scanner.ScanInstallStaged(ctx, &scanEntry, skillDir, sendStatus)
	}
	if report == nil && !(d.app != nil && d.app.isRiskGuardrailOffMode()) {
		if d.app != nil && !d.app.skillInstallMissingScanShouldBlock() {
			if d.auditLog != nil {
				_ = d.auditLog.Log(security.AuditEntry{
					Timestamp:    time.Now(),
					Action:       security.AuditActionHubSkillInstall,
					ToolName:     "skill_auto_publish",
					RiskLevel:    security.RiskCritical,
					PolicyAction: security.PolicyAudit,
					Result:       fmt.Sprintf("auto-publish allowed skill %s even though pre-publish scan report was missing", entry.Name),
				})
			}
		} else {
			return fmt.Errorf("skill auto-publish security scan produced no report")
		}
	} else if d.app != nil && d.app.skillInstallScanShouldBlock(report) {
		if d.auditLog != nil {
			_ = d.auditLog.Log(security.AuditEntry{
				Timestamp:    time.Now(),
				Action:       security.AuditActionHubSkillReject,
				ToolName:     "skill_auto_publish",
				RiskLevel:    report.FinalLevel,
				PolicyAction: security.PolicyDeny,
				Result:       fmt.Sprintf("auto-publish blocked skill %s by pre-publish security scan: %s", entry.Name, report.Summary),
			})
		}
		return fmt.Errorf("skill auto-publish blocked by security scan: level=%s summary=%s", report.FinalLevel, report.Summary)
	} else if report != nil && report.NeedsUserReview() && d.auditLog != nil {
		_ = d.auditLog.Log(security.AuditEntry{
			Timestamp:    time.Now(),
			Action:       security.AuditActionHubSkillInstall,
			ToolName:     "skill_auto_publish",
			RiskLevel:    report.FinalLevel,
			PolicyAction: security.PolicyAudit,
			Result:       fmt.Sprintf("auto-publish recorded risk for skill %s and allowed by current policy: %s", entry.Name, report.Summary),
		})
	}

	full := hubSkillFull{
		hubSkillItem: hubSkillItem{
			ID:          entry.Name,
			Name:        entry.Name,
			Description: entry.Description,
			Tags:        entry.Triggers,
			Version:     "1.0.0",
			Author:      author,
			TrustLevel:  "community",
		},
		Triggers: entry.Triggers,
		Steps:    steps,
		Manifest: hubSkillManifest{
			Dependencies: deps,
		},
		Files: files,
	}

	if err := d.hubClient.Publish(ctx, full); err != nil {
		return fmt.Errorf("发布失败: %w", err)
	}

	sendStatus(fmt.Sprintf("Skill「%s」已发布到 SkillHub OK", entry.Name))
	return nil
}

// pythonStdlib contains common Python standard library module names that
// should NOT be treated as pip dependencies.
var pythonStdlib = map[string]bool{
	"os": true, "sys": true, "re": true, "io": true, "json": true,
	"math": true, "time": true, "datetime": true, "collections": true,
	"itertools": true, "functools": true, "pathlib": true, "shutil": true,
	"subprocess": true, "threading": true, "multiprocessing": true,
	"logging": true, "argparse": true, "unittest": true, "typing": true,
	"abc": true, "copy": true, "csv": true, "hashlib": true, "hmac": true,
	"http": true, "urllib": true, "socket": true, "ssl": true,
	"string": true, "struct": true, "tempfile": true, "textwrap": true,
	"uuid": true, "xml": true, "zipfile": true, "gzip": true,
	"base64": true, "binascii": true, "codecs": true, "configparser": true,
	"contextlib": true, "dataclasses": true, "enum": true, "glob": true,
	"inspect": true, "operator": true, "pickle": true, "platform": true,
	"pprint": true, "random": true, "signal": true, "sqlite3": true,
	"stat": true, "traceback": true, "warnings": true, "weakref": true,
}

// scanDependencies analyzes bash steps for pip/npm install commands and
// Python import statements, returning discovered dependencies.
// Standard library modules are filtered out from import-based detection.
func (d *CapabilityGapDetector) scanDependencies(steps []corelib.NLSkillStep) []hubSkillDependency {
	var deps []hubSkillDependency
	seen := make(map[string]bool)

	for _, step := range steps {
		if !classifySkillStepAction(step.Action).IsBash() {
			continue
		}
		cmd, _ := step.Params["command"].(string)
		if cmd == "" {
			continue
		}

		// Detect pip install commands: pip install <pkg>, pip3 install <pkg>
		for _, prefix := range []string{"pip install ", "pip3 install "} {
			if idx := strings.Index(cmd, prefix); idx >= 0 {
				rest := cmd[idx+len(prefix):]
				for _, pkg := range strings.Fields(rest) {
					pkg = strings.TrimSpace(pkg)
					if pkg == "" || strings.HasPrefix(pkg, "-") {
						continue
					}
					key := "pip:" + pkg
					if !seen[key] {
						seen[key] = true
						deps = append(deps, hubSkillDependency{Type: "pip", Name: pkg})
					}
				}
			}
		}

		// Detect npm install commands: npm install -g <pkg>
		if idx := strings.Index(cmd, "npm install"); idx >= 0 {
			rest := cmd[idx+len("npm install"):]
			fields := strings.Fields(rest)
			for _, f := range fields {
				if strings.HasPrefix(f, "-") {
					continue
				}
				key := "npm:" + f
				if !seen[key] {
					seen[key] = true
					deps = append(deps, hubSkillDependency{Type: "npm", Name: f})
				}
			}
		}

		// Detect Python imports: import <pkg>, from <pkg> import ...
		// Skip standard library modules.
		for _, line := range strings.Split(cmd, "\n") {
			line = strings.TrimSpace(line)
			var pkg string
			if strings.HasPrefix(line, "import ") {
				fields := strings.Fields(line)
				if len(fields) >= 2 {
					pkg = strings.Split(fields[1], ".")[0]
					pkg = strings.TrimRight(pkg, ",")
				}
			} else if strings.HasPrefix(line, "from ") && strings.Contains(line, " import ") {
				fields := strings.Fields(line)
				if len(fields) >= 2 {
					pkg = strings.Split(fields[1], ".")[0]
				}
			}
			if pkg == "" || pythonStdlib[pkg] {
				continue
			}
			key := "pip:" + pkg
			if !seen[key] {
				seen[key] = true
				deps = append(deps, hubSkillDependency{Type: "pip", Name: pkg})
			}
		}
	}
	return deps
}

// localSkillDir resolves a skill's local file directory without creating it.
func (d *CapabilityGapDetector) localSkillDir(skillName string) string {
	if strings.TrimSpace(skillName) == "" {
		return ""
	}
	skillsRoot, err := cskill.PrimarySkillsDir()
	if err == nil {
		skillDir := filepath.Join(skillsRoot, skillName)
		if info, statErr := os.Stat(skillDir); statErr == nil && info.IsDir() {
			return skillDir
		}
	}
	// Fallback: check all scan roots in case migration has not moved this skill.
	for _, root := range cskill.SkillScanRoots() {
		alt := filepath.Join(root, skillName)
		if fi, statErr := os.Stat(alt); statErr == nil && fi.IsDir() {
			return alt
		}
	}
	return ""
}

// packageLocalFiles reads files from ~/.maclaw/data/skills/<name>/ and returns
// a map of relative path to base64 content, respecting extension limits and a
// generous per-file guardrail for abnormal local content. Symlinks and
// non-regular files are deliberately skipped so export cannot leak files
// outside the skill directory.
func (d *CapabilityGapDetector) packageLocalFiles(skillName string) map[string]string {
	return d.packageLocalFilesFromDir(d.localSkillDir(skillName))
}

func (d *CapabilityGapDetector) packageLocalFilesFromDir(skillDir string) map[string]string {
	if strings.TrimSpace(skillDir) == "" {
		return nil
	}
	info, err := os.Stat(skillDir)
	if err != nil || !info.IsDir() {
		return nil
	}

	files := make(map[string]string)
	var totalSize int64

	_ = filepath.WalkDir(skillDir, func(path string, de os.DirEntry, err error) error {
		if err != nil || de.IsDir() {
			return nil
		}
		info, err := de.Info()
		if err != nil {
			return nil
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(de.Name()))
		if !allowedLocalPackagedFileExts[ext] {
			return nil
		}
		if info.Size() > maxCapabilityGapLocalPackagedFileSize {
			return nil
		}
		if totalSize+info.Size() > maxTotalCapabilityGapLocalPackagedFileSize {
			return filepath.SkipAll
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		rel, err := filepath.Rel(skillDir, path)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || filepath.IsAbs(rel) {
			return nil
		}
		rel = filepath.ToSlash(rel)
		totalSize += info.Size()
		files[rel] = base64.StdEncoding.EncodeToString(data)
		return nil
	})

	if len(files) == 0 {
		return nil
	}
	return files
}
