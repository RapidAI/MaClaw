package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// CapabilityGapDetector detects capability gaps in Agent responses and
// resolves them by searching SkillHub for matching Skills.
type CapabilityGapDetector struct {
	app             *App
	hubClient       *SkillHubClient
	skillExecutor   *SkillExecutor
	riskAssessor    *RiskAssessor
	auditLog        *AuditLog
	llmConfig       MaclawLLMConfig
	client          *http.Client
	confirmCallback func(skillName string, riskDetails string) bool
}

// NewCapabilityGapDetector creates a new CapabilityGapDetector.
func NewCapabilityGapDetector(
	app *App,
	hubClient *SkillHubClient,
	skillExecutor *SkillExecutor,
	riskAssessor *RiskAssessor,
	auditLog *AuditLog,
	llmConfig MaclawLLMConfig,
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

// gapKeywords are heuristic keywords indicating a capability gap when LLM is
// not configured.
var gapKeywords = []string{
	"无法", "不支持", "cannot", "unable", "don't have", "没有能力",
}

// Detect returns true if the LLM response indicates a capability gap.
// When an LLM is configured it asks the model to judge; otherwise it falls
// back to simple keyword matching.
func (d *CapabilityGapDetector) Detect(llmResponse string) bool {
	if d.isLLMConfigured() {
		return d.llmDetectGap(llmResponse)
	}
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
	// Step 1: Extract capability query from user message.
	query := d.extractCapabilityQuery(ctx, userMessage, conversationHistory)
	if query == "" {
		query = userMessage
	}

	// Step 2: Search SkillHub.
	sendStatus("正在搜索可用的 Skill...")
	candidates, err := d.hubClient.Search(ctx, query)
	if err != nil {
		return "", "", fmt.Errorf("search hub: %w", err)
	}
	if len(candidates) == 0 {
		return "", "", nil
	}

	// Step 3: Select best matching Skill.
	chosen := d.llmSelectBestSkill(ctx, userMessage, candidates)
	if chosen == nil {
		return "", "", nil
	}

	// Step 4: Download / install Skill.
	sendStatus(fmt.Sprintf("正在安装 Skill: %s ...", chosen.Name))
	skill, err := d.hubClient.Install(ctx, chosen.ID, chosen.HubURL)
	if err != nil {
		return "", "", fmt.Errorf("install skill: %w", err)
	}

	// Step 5: Risk assessment on the entire skill.
	sendStatus("正在进行安全审查...")
	assessment := d.riskAssessor.AssessSkill(skill, chosen.TrustLevel)
	maxRisk := assessment.Level
	if maxRisk == RiskCritical {
		riskDetails := fmt.Sprintf("Skill「%s」来自 %s (trust_level=%s) 包含 critical 级别风险操作", chosen.Name, chosen.HubURL, chosen.TrustLevel)
		confirmed := false
		if d.confirmCallback != nil {
			sendStatus(fmt.Sprintf("⚠️ 安全警告: %s\n等待用户确认...", riskDetails))
			confirmed = d.confirmCallback(chosen.Name, riskDetails)
		}
		if !confirmed {
			if d.auditLog != nil {
				_ = d.auditLog.Log(AuditEntry{
					Timestamp:    time.Now(),
					Action:       AuditActionHubSkillReject,
					ToolName:     "hub_skill_install",
					RiskLevel:    RiskCritical,
					PolicyAction: PolicyDeny,
					Result:       fmt.Sprintf("rejected skill %s from %s: critical risk, trust_level=%s", chosen.Name, chosen.HubURL, chosen.TrustLevel),
				})
			}
			return "", "", fmt.Errorf("Skill 包含高风险操作，已拒绝自动安装")
		}
		// User confirmed — continue with installation despite critical risk.
	}

	// Step 6: Register to local SkillExecutor.
	sendStatus("正在注册 Skill...")
	if err := d.skillExecutor.Register(*skill); err != nil {
		return "", "", fmt.Errorf("register skill: %w", err)
	}

	// Step 7: Execute immediately.
	sendStatus(fmt.Sprintf("正在执行 Skill: %s ...", skill.Name))
	execResult, execErr := d.skillExecutor.Execute(skill.Name)

	// Audit log.
	if d.auditLog != nil {
		auditResult := execResult
		if execErr != nil {
			auditResult = execErr.Error()
		}
		_ = d.auditLog.Log(AuditEntry{
			Timestamp:    time.Now(),
			Action:       AuditActionHubSkillInstall,
			ToolName:     "hub_skill_install",
			RiskLevel:    maxRisk,
			PolicyAction: PolicyAllow,
			Result:       fmt.Sprintf("installed and executed skill %s from %s, trust_level=%s, risk=%s: %s", skill.Name, chosen.HubURL, chosen.TrustLevel, maxRisk, auditResult),
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
// ~/.maclaw/skills/<name>/ into the upload payload.
func (d *CapabilityGapDetector) AutoPublishSkill(ctx context.Context, entry NLSkillEntry, sendStatus func(string)) error {
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

	// Package local files from ~/.maclaw/skills/<name>/.
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
func (d *CapabilityGapDetector) scanDependencies(steps []NLSkillStep) []hubSkillDependency {
	var deps []hubSkillDependency
	seen := make(map[string]bool)

	for _, step := range steps {
		if step.Action != "bash" {
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

// packageLocalFiles reads files from ~/.maclaw/skills/<name>/ and returns
// a map of relative path → base64 content, respecting size and extension limits.
func (d *CapabilityGapDetector) packageLocalFiles(skillName string) map[string]string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	skillDir := filepath.Join(home, ".maclaw", "skills", skillName)
	info, err := os.Stat(skillDir)
	if err != nil || !info.IsDir() {
		return nil
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
