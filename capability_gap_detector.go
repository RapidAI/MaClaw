package main

import (
	"context"
	"fmt"
	"net/http"
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

	result, err := doSimpleLLMRequest(d.llmConfig, msgs, d.client, 30*time.Second)
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
