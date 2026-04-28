package skill

// security_scanner.go implements the security scanning pipeline for skill
// installation. This is the shared core — GUI and TUI provide platform-specific
// LLM callers via the LLMCaller interface.
//
// Security model:
//   Pattern scan is the HARD FLOOR. The agent can only UPGRADE risk (discover
//   semantic threats that patterns miss), never DOWNGRADE it.
//
//   finalLevel = max(patternLevel, agentLevel)

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/security"
)

// LLMCaller is the interface that platform-specific code (GUI/TUI) implements
// to provide LLM access for agent-based security scanning.
// This is the only injection point — everything else is shared.
type LLMCaller interface {
	// Available reports whether an LLM is configured and reachable.
	Available() bool
	// Call sends a prompt and returns the response text.
	// Must respect ctx cancellation.
	Call(ctx context.Context, prompt string) (string, error)
}

// SecurityScanner performs security scanning of staged skills.
// Platform-agnostic — GUI and TUI create one with their own LLMCaller.
type SecurityScanner struct {
	llm LLMCaller // nil = pattern-only mode
}

// NewSecurityScanner creates a scanner. llm may be nil (pattern-only mode).
func NewSecurityScanner(llm LLMCaller) *SecurityScanner {
	return &SecurityScanner{llm: llm}
}

// ScanStaged performs a security scan on a staged skill.
// stagingDir is the directory where skill files have been extracted.
// Always runs pattern scan first (hard floor), then optionally agent scan.
func (s *SecurityScanner) ScanStaged(
	ctx context.Context,
	entry *corelib.NLSkillEntry,
	stagingDir string,
	sendStatus func(string),
) *ScanReport {
	if sendStatus == nil {
		sendStatus = func(string) {}
	}

	manifest := BuildFileManifest(stagingDir)

	// ── Step 1: Pattern scan (always, hard floor) ───────────────────
	sendStatus("🔒 正在进行安全规则检查...")
	patternAssessment := patternScan(entry, stagingDir)

	report := &ScanReport{
		PatternAssessment: patternAssessment,
		AgentScore:        -1,
		FinalLevel:        patternAssessment.Level,
		Summary:           patternAssessment.Reason,
		Recommendation:    RecommendationForLevel(patternAssessment.Level),
		ScannedBy:         "pattern",
	}

	// ── Step 2: Agent scan (optional, can only upgrade) ─────────────
	if s.llm != nil && s.llm.Available() {
		sendStatus("🔍 Agent 正在智能扫描 Skill 文件...")
		agentResult := agentScan(ctx, s.llm, entry, stagingDir, manifest)
		if agentResult != nil {
			report.AgentScore = agentResult.AgentScore
			report.Findings = agentResult.Findings
			report.ScannedBy = "agent+pattern"

			// Agent can only UPGRADE risk, never downgrade.
			agentLevel := AgentScoreToLevel(agentResult.AgentScore)
			if security.RiskLevelOrder[agentLevel] > security.RiskLevelOrder[report.FinalLevel] {
				report.FinalLevel = agentLevel
				report.Summary = agentResult.Summary
			}

			// Use agent recommendation only when agent upgraded the level.
			if agentResult.Recommendation != "" &&
				security.RiskLevelOrder[agentLevel] >= security.RiskLevelOrder[report.FinalLevel] {
				report.Recommendation = agentResult.Recommendation
			} else {
				report.Recommendation = RecommendationForLevel(report.FinalLevel)
			}
		} else {
			log.Printf("[skill-security] agent scan failed for %s, using pattern-only", entry.Name)
		}
	}

	return report
}

// ── Pattern scan ────────────────────────────────────────────────────────

// patternScan runs the corelib/security regex/keyword-based assessment.
// Creates a shallow copy of entry to set SkillDir without mutating the caller's object.
func patternScan(entry *corelib.NLSkillEntry, stagingDir string) security.RiskAssessment {
	// Convert NLSkillEntry → SkillRiskInput for corelib/security.
	input := security.SkillRiskInput{
		Name:     entry.Name,
		SkillDir: entry.SkillDir,
	}
	if stagingDir != "" {
		input.SkillDir = stagingDir
	}
	for _, step := range entry.Steps {
		input.Steps = append(input.Steps, struct {
			Action string
			Params map[string]interface{}
		}{Action: step.Action, Params: step.Params})
	}

	assessor := &security.RiskAssessor{}
	return assessor.AssessSkill(input, entry.TrustLevel)
}

// ── Agent scan ──────────────────────────────────────────────────────────

// agentScan uses the LLM to analyze skill contents.
// Returns a partial ScanReport with AgentScore, Findings, Summary, Recommendation.
// Returns nil on any failure.
func agentScan(
	ctx context.Context,
	llm LLMCaller,
	entry *corelib.NLSkillEntry,
	stagingDir string,
	manifest []StagedFile,
) *ScanReport {
	contents := CollectScanContent(stagingDir, manifest, 32768)

	var steps []StepInfo
	for _, step := range entry.Steps {
		steps = append(steps, StepInfo{Action: step.Action, Params: step.Params})
	}

	prompt := BuildAgentScanPrompt(
		entry.Name, entry.Description, entry.TrustLevel,
		steps, manifest, contents,
	)

	response, err := llm.Call(ctx, prompt)
	if err != nil {
		log.Printf("[skill-security] LLM call failed: %v", err)
		return nil
	}

	report, err := ParseAgentScanResponse(response)
	if err != nil {
		log.Printf("[skill-security] failed to parse agent response: %v", err)
		return nil
	}

	return report
}

// ── Response parsing (exported for testing) ─────────────────────────────

// ParseAgentScanResponse extracts the structured JSON from the agent's response.
// Uses json.Decoder to find the first valid JSON object, handling surrounding text.
func ParseAgentScanResponse(response string) (*ScanReport, error) {
	text := strings.TrimSpace(response)

	// Strip markdown code block if present.
	if strings.HasPrefix(text, "```") {
		if idx := strings.Index(text, "\n"); idx >= 0 {
			text = text[idx+1:]
		}
		if idx := strings.LastIndex(text, "```"); idx >= 0 {
			text = text[:idx]
		}
		text = strings.TrimSpace(text)
	}

	start := strings.Index(text, "{")
	if start < 0 {
		return nil, fmt.Errorf("no JSON object found in response: %.200s", text)
	}

	var raw struct {
		Score          int           `json:"score"`
		Summary        string        `json:"summary"`
		Findings       []ScanFinding `json:"findings"`
		Recommendation string        `json:"recommendation"`
	}

	dec := json.NewDecoder(strings.NewReader(text[start:]))
	if err := dec.Decode(&raw); err != nil {
		return nil, fmt.Errorf("parse JSON: %w (response: %.200s)", err, text)
	}

	if raw.Score < 0 {
		raw.Score = 0
	}
	if raw.Score > 10 {
		raw.Score = 10
	}

	return &ScanReport{
		AgentScore:     raw.Score,
		FinalLevel:     AgentScoreToLevel(raw.Score),
		Summary:        raw.Summary,
		Findings:       raw.Findings,
		Recommendation: raw.Recommendation,
		ScannedBy:      "agent",
	}, nil
}

// ── Level mapping (exported for GUI/TUI) ────────────────────────────────

// AgentScoreToLevel maps the agent's 0-10 score to a RiskLevel.
func AgentScoreToLevel(score int) security.RiskLevel {
	switch {
	case score <= 2:
		return security.RiskLow
	case score <= 4:
		return security.RiskMedium
	case score <= 6:
		return security.RiskHigh
	default:
		return security.RiskCritical
	}
}

// RecommendationForLevel returns a human-readable recommendation for a risk level.
func RecommendationForLevel(level security.RiskLevel) string {
	switch level {
	case security.RiskLow:
		return "可以安全安装"
	case security.RiskMedium:
		return "可以安装，存在少量需注意的操作"
	case security.RiskHigh:
		return "建议检查上述发现后再决定是否安装"
	case security.RiskCritical:
		return "包含高风险操作，强烈建议不要安装，除非你完全信任此 Skill 的来源"
	default:
		return ""
	}
}

// ── User-facing formatting ──────────────────────────────────────────────

// FormatScanReportForUser formats a scan report for display in chat.
func FormatScanReportForUser(report *ScanReport, skillName string) string {
	var sb strings.Builder

	switch {
	case report.IsSafe():
		sb.WriteString(fmt.Sprintf("✅ Skill「%s」安全扫描通过\n", skillName))
	case report.NeedsUserReview() && !report.IsDangerous():
		sb.WriteString(fmt.Sprintf("⚠️ Skill「%s」需要人工确认\n", skillName))
	case report.IsDangerous():
		sb.WriteString(fmt.Sprintf("🚫 Skill「%s」存在安全风险\n", skillName))
	}

	sb.WriteString(fmt.Sprintf("📋 %s\n", report.Summary))

	if len(report.Findings) == 0 && len(report.PatternAssessment.Factors) > 0 {
		sb.WriteString("\n规则检测：\n")
		for _, f := range report.PatternAssessment.Factors {
			sb.WriteString(fmt.Sprintf("  • %s\n", f))
		}
	}

	if len(report.Findings) > 0 {
		sb.WriteString("\nAI 扫描发现：\n")
		for _, f := range report.Findings {
			if report.IsSafe() && (f.Severity == "info" || f.Severity == "low") {
				continue
			}
			icon := "ℹ️"
			switch f.Severity {
			case "low":
				icon = "🔵"
			case "medium":
				icon = "🟡"
			case "high":
				icon = "🟠"
			case "critical":
				icon = "🔴"
			}
			loc := ""
			if f.Location != "" {
				loc = fmt.Sprintf(" [%s]", f.Location)
			}
			sb.WriteString(fmt.Sprintf("  %s %s%s\n", icon, f.Description, loc))
		}
	}

	if report.Recommendation != "" {
		sb.WriteString(fmt.Sprintf("\n💡 %s\n", report.Recommendation))
	}

	switch report.ScannedBy {
	case "agent+pattern":
		sb.WriteString("(AI 智能扫描 + 规则引擎)\n")
	case "pattern":
		sb.WriteString("(规则引擎扫描)\n")
	}

	return sb.String()
}
