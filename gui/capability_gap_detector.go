package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/security"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/intent"
	cskill "github.com/RapidAI/CodeClaw/corelib/skill"
)

// CapabilityGapDetector detects capability gaps in Agent responses and
// resolves them by searching SkillHub for matching Skills.
type CapabilityGapDetector struct {
	app             *App
	hubClient       *SkillHubClient
	skillExecutor   *SkillExecutor
	riskAssessor    *RiskAssessor
	auditLog        *AuditLog
	llmConfig       corelib.MaclawLLMConfig
	client          *http.Client
	confirmCallback func(skillName string, riskDetails string) bool
	// unifiedClassifier is used to check whether the original user message
	// was classified as non-coding/ambiguous before applying gap detection.
	unifiedClassifier *intent.UnifiedIntentClassifier
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
	trimmed := strings.TrimSpace(llmResponse)
	if utf8.RuneCountInString(trimmed) > 500 {
		return false
	}
	if d.isLLMConfigured() {
		return d.llmDetectGap(llmResponse)
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
	// Developer mode: skip all security reviews in this function.
	isDeveloper := d.app != nil && d.app.policyEngine != nil && d.app.policyEngine.IsDeveloperMode()

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

	// Step 2: Search SkillHub (if allowed).
	var candidates []HubSkillMeta
	if cskill.IsSourceAllowed("skillhub", allowedSources) {
		sendStatus("正在搜索可用的 Skill...")
		candidates, err = d.hubClient.Search(ctx, query)
		if err != nil {
			return "", "", fmt.Errorf("search hub: %w", err)
		}
	}
	if len(candidates) == 0 {
		// Fallback: search GitHub for skill.yaml files (if allowed).
		if !cskill.IsSourceAllowed("github", allowedSources) {
			return "", "", nil
		}
		sendStatus("SkillHub 未找到匹配技能，正在搜索 GitHub...")
		gs := cskill.NewGitHubSearcher("")
		ghCandidates, ghErr := gs.SearchGitHub(query)
		if ghErr != nil || len(ghCandidates) == 0 {
			return "", "", nil
		}
		// Import the first candidate.
		imported, impErr := gs.ImportFromCandidate(ghCandidates[0])
		if impErr != nil {
			sendStatus(fmt.Sprintf("GitHub 技能导入失败: %v", impErr))
			return "", "", nil
		}

		// Security review: pattern scan (hard floor) + agent scan (upgrade only).
		// Developer mode: skip security review entirely.
		var githubScanReport *cskill.ScanReport
		if !isDeveloper {
			scanner := NewSkillSecurityScanner(d.app, nil)
			scanReport := scanner.ScanInstallStaged(ctx, imported, imported.SkillDir, sendStatus)
			githubScanReport = scanReport
			if scanReport.IsDangerous() {
				if d.auditLog != nil {
					_ = d.auditLog.Log(security.AuditEntry{
						Timestamp:    time.Now(),
						Action:       security.AuditActionHubSkillReject,
						ToolName:     "github_skill_install",
						RiskLevel:    security.RiskCritical,
						PolicyAction: security.PolicyDeny,
						Result:       fmt.Sprintf("pre-install policy rejected critical github skill %s: %s", imported.Name, scanReport.Summary),
					})
				}
				return "", "", fmt.Errorf("GitHub Skill security scan rejected critical risk before installation")
			}
			if scanReport.NeedsUserReview() {
				riskDetails := FormatScanReportForUser(scanReport, imported.Name)
				confirmed := false
				if d.confirmCallback != nil {
					sendStatus(fmt.Sprintf("⚠️ 安全警告: %s", scanReport.Summary))
					confirmed = d.confirmCallback(imported.Name, riskDetails)
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
					return "", "", fmt.Errorf("GitHub Skill 安全审查未通过，已拒绝自动安装")
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
			if err := writeSkillScanCacheForReportStatus(imported, imported.SkillDir, "", scanReport, skillScanCacheStatusAllowed); err != nil && d.app != nil {
				d.app.log(fmt.Sprintf("[capability-gap] failed to write install scan cache for %s: %v", imported.Name, err))
			}
		}

		// Override source to indicate auto-installation by CapabilityGapDetector.
		imported.Source = "auto_github"
		sendStatus(fmt.Sprintf("正在从 GitHub 安装 Skill: %s ...", imported.Name))
		if err := d.skillExecutor.Register(*imported); err != nil {
			return "", "", fmt.Errorf("register github skill: %w", err)
		}

		// Audit log for GitHub install.
		if d.auditLog != nil {
			riskLevel := security.RiskLow
			policyAction := security.PolicyAllow
			if githubScanReport != nil {
				riskLevel = githubScanReport.FinalLevel
				if githubScanReport.NeedsUserReview() {
					policyAction = security.PolicyUserOverride
				}
			}
			_ = d.auditLog.Log(security.AuditEntry{
				Timestamp:    time.Now(),
				Action:       security.AuditActionHubSkillInstall,
				ToolName:     "github_skill_install",
				RiskLevel:    riskLevel,
				PolicyAction: policyAction,
				Result:       fmt.Sprintf("installed github skill %s from %s", imported.Name, ghCandidates[0].RepoURL),
			})
		}

		execResult, execErr := d.skillExecutor.ExecuteWithArgs(imported.Name, skillExecutionRunArgs(userMessage))
		return imported.Name, execResult, execErr
	}

	// Step 3: Select best matching Skill.
	chosen := d.llmSelectBestSkill(ctx, userMessage, candidates)
	if chosen == nil {
		return "", "", nil
	}

	// Step 4: Download Skill into staging; final install happens after scan.
	sendStatus(fmt.Sprintf("正在安装 Skill: %s ...", chosen.Name))
	stagingDir, err := cskill.PrepareStagingDir(firstNonEmpty(chosen.ID, chosen.Name, "capability-gap-skill"))
	if err != nil {
		return "", "", fmt.Errorf("create skill staging dir: %w", err)
	}
	skill, err := d.hubClient.InstallToDir(ctx, chosen.ID, chosen.HubURL, stagingDir)
	if err != nil {
		cskill.CleanupStaging(stagingDir)
		return "", "", fmt.Errorf("install skill: %w", err)
	}

	// Step 5: Security review: pattern scan (hard floor) + agent scan (upgrade only).
	// Developer mode: skip security review entirely.
	var scanReport *cskill.ScanReport
	if !isDeveloper {
		scanner := NewSkillSecurityScanner(d.app, nil)
		scanReport = scanner.ScanInstallStaged(ctx, skill, stagingDir, sendStatus)
		if scanReport.IsDangerous() {
			cskill.CleanupStaging(stagingDir)
			if d.auditLog != nil {
				_ = d.auditLog.Log(security.AuditEntry{
					Timestamp:    time.Now(),
					Action:       security.AuditActionHubSkillReject,
					ToolName:     "hub_skill_install",
					RiskLevel:    security.RiskCritical,
					PolicyAction: security.PolicyDeny,
					Result:       fmt.Sprintf("pre-install policy rejected critical skill %s: %s", chosen.Name, scanReport.Summary),
				})
			}
			return "", "", fmt.Errorf("Skill security scan rejected critical risk before installation")
		}
		if scanReport.NeedsUserReview() {
			riskDetails := FormatScanReportForUser(scanReport, chosen.Name)
			confirmed := false
			if d.confirmCallback != nil {
				sendStatus(fmt.Sprintf("⚠️ 安全警告: %s", scanReport.Summary))
				confirmed = d.confirmCallback(chosen.Name, riskDetails)
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
				return "", "", fmt.Errorf("Skill 安全审查未通过，已拒绝自动安装")
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

	finalDir, err := cskill.CommitStaging(stagingDir, skill.Name)
	if err != nil {
		cskill.CleanupStaging(stagingDir)
		return "", "", fmt.Errorf("commit staged skill: %w", err)
	}
	skill.SkillDir = finalDir
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
		if err := writeSkillScanCacheForReportStatus(skill, skill.SkillDir, preNormalizeScanHash, scanReport, skillScanCacheStatusAllowed); err != nil && d.app != nil {
			d.app.log(fmt.Sprintf("[capability-gap] failed to write install scan cache for %s: %v", skill.Name, err))
		}
	}

	// Step 6: Register to local SkillExecutor.
	// Override source to indicate auto-installation by CapabilityGapDetector.
	skill.Source = "auto_hub"
	sendStatus("正在注册 Skill...")
	if err := d.skillExecutor.Register(*skill); err != nil {
		_ = os.RemoveAll(finalDir)
		return "", "", fmt.Errorf("register skill: %w", err)
	}

	// Step 7: Execute immediately.
	sendStatus(fmt.Sprintf("正在执行 Skill: %s ...", skill.Name))
	execResult, execErr := d.skillExecutor.ExecuteWithArgs(skill.Name, skillExecutionRunArgs(userMessage))

	// Audit log.
	if d.auditLog != nil {
		auditResult := execResult
		if execErr != nil {
			auditResult = execErr.Error()
		}
		riskLevel := security.RiskLow
		policyAction := security.PolicyAllow
		if scanReport != nil {
			riskLevel = scanReport.FinalLevel
			if scanReport.NeedsUserReview() {
				policyAction = security.PolicyUserOverride
			}
		}
		_ = d.auditLog.Log(security.AuditEntry{
			Timestamp:    time.Now(),
			Action:       security.AuditActionHubSkillInstall,
			ToolName:     "hub_skill_install",
			RiskLevel:    riskLevel,
			PolicyAction: policyAction,
			Result:       fmt.Sprintf("installed and executed skill %s from %s: %s", skill.Name, chosen.HubURL, auditResult),
		})
	}

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
	// Convert []map[string]interface{} to []interface{} for the shared helper
	msgs := make([]interface{}, len(messages))
	for i, m := range messages {
		msgs[i] = m
	}

	result, err := doSimpleLLMRequest(context.Background(), d.llmConfig, msgs, d.client, 30*time.Second)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(result.Content), nil
}

// llmDetectGap asks the LLM whether the response indicates a capability gap.
func (d *CapabilityGapDetector) llmDetectGap(llmResponse string) bool {
	messages := []map[string]interface{}{
		{"role": "system", "content": "你是一个判断助手。用户会给你一段 AI 助手的回复，请判断这段回复是否表明 AI 助手缺少某种能力或工具来完成用户的请求。只回答 yes 或 no。"},
		{"role": "user", "content": llmResponse},
	}
	answer, err := d.doLLMChat(messages)
	if err != nil {
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
	answer, err := d.doLLMChat(messages)
	if err != nil || strings.TrimSpace(answer) == "" {
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
	answer, err := d.doLLMChat(messages)
	if err != nil {
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
	files := d.packageLocalFiles(entry.Name)

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

	sendStatus(fmt.Sprintf("Skill「%s」已发布到 SkillHub ✓", entry.Name))
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

// packageLocalFiles reads files from ~/.maclaw/data/skills/<name>/ and returns
// a map of relative path → base64 content, respecting size and extension limits.
func (d *CapabilityGapDetector) packageLocalFiles(skillName string) map[string]string {
	skillsRoot, err := cskill.PrimarySkillsDir()
	if err != nil {
		return nil
	}
	skillDir := filepath.Join(skillsRoot, skillName)
	info, err := os.Stat(skillDir)
	if err != nil || !info.IsDir() {
		// Fallback: check legacy path in case migration hasn't moved this skill.
		for _, root := range cskill.SkillScanRoots() {
			alt := filepath.Join(root, skillName)
			if fi, e := os.Stat(alt); e == nil && fi.IsDir() {
				skillDir = alt
				break
			}
		}
		// Re-check after fallback.
		if fi, e := os.Stat(skillDir); e != nil || !fi.IsDir() {
			return nil
		}
	}

	files := make(map[string]string)
	var totalSize int64

	_ = filepath.Walk(skillDir, func(path string, fi os.FileInfo, err error) error {
		if err != nil || fi.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(fi.Name()))
		if !allowedFileExts[ext] {
			return nil
		}
		if fi.Size() > maxSingleFileSize {
			return nil
		}
		totalSize += fi.Size()
		if totalSize > maxTotalFileSize {
			return filepath.SkipAll
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(skillDir, path)
		rel = filepath.ToSlash(rel)
		files[rel] = base64.StdEncoding.EncodeToString(data)
		return nil
	})

	if len(files) == 0 {
		return nil
	}
	return files
}
