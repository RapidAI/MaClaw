package skill

// security_scanner.go implements the security scanning pipeline for skill
// installation. This is the shared core; GUI and TUI provide platform-specific
// LLM callers via the LLMCaller interface.
//
// Security model:
// Pattern scan is the HARD FLOOR. The agent can only UPGRADE risk (discover
// semantic threats that patterns miss), never DOWNGRADE it.
//
//   finalLevel = max(patternLevel, agentLevel)

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/security"
)

// LLMCaller is the interface that platform-specific code (GUI/TUI) implements
// to provide LLM access for agent-based security scanning.
// This is the only injection point; everything else is shared.
type LLMCaller interface {
	// Available reports whether an LLM is configured and reachable.
	Available() bool
	// Call sends a prompt and returns the response text.
	// Must respect ctx cancellation.
	Call(ctx context.Context, prompt string) (string, error)
}

// SecurityScanner performs security scanning of staged skills.
// Platform-agnostic; GUI and TUI create one with their own LLMCaller.
type SecurityScanner struct {
	llm LLMCaller // nil = pattern-only mode
}

// NewSecurityScanner creates a scanner. llm may be nil (pattern-only mode).
func NewSecurityScanner(llm LLMCaller) *SecurityScanner {
	return &SecurityScanner{llm: llm}
}

// ScanInstallStaged scans a skill before installation. It deliberately ignores
// package-provided trust metadata because trust is assigned by the installer or
// source policy after admission, not by the untrusted package itself.
func (s *SecurityScanner) ScanInstallStaged(
	ctx context.Context,
	entry *corelib.NLSkillEntry,
	stagingDir string,
	sendStatus func(string),
) *ScanReport {
	if entry == nil {
		return s.ScanStaged(ctx, nil, stagingDir, sendStatus)
	}
	cp := *entry
	cp.TrustLevel = security.TrustLevelCommunity
	return s.ScanStaged(ctx, &cp, stagingDir, sendStatus)
}

// ScanStaged performs a security scan on a staged skill.
// stagingDir is the directory where skill files have been extracted.
// Always runs pattern scan first as the risk floor, then optionally agent scan.
func (s *SecurityScanner) ScanStaged(
	ctx context.Context,
	entry *corelib.NLSkillEntry,
	stagingDir string,
	sendStatus func(string),
) *ScanReport {
	if ctx == nil {
		ctx = context.Background()
	}
	if sendStatus == nil {
		sendStatus = func(string) {}
	}
	if err := ctx.Err(); err != nil {
		return invalidScanReport(fmt.Sprintf("skill security scan cancelled: %v", err))
	}
	if entry == nil {
		return invalidScanReport("skill entry is missing")
	}
	if strings.TrimSpace(stagingDir) != "" {
		info, err := os.Stat(stagingDir)
		if err != nil {
			return invalidScanReport(fmt.Sprintf("skill staging directory is not readable: %v", err))
		}
		if !info.IsDir() {
			return invalidScanReport("skill staging path is not a directory")
		}
	}

	manifest := BuildFileManifest(stagingDir)
	if err := ctx.Err(); err != nil {
		return invalidScanReport(fmt.Sprintf("skill security scan cancelled: %v", err))
	}

	// Step 1: pattern scan (always the risk floor).
	sendStatus("Running skill security rule scan...")
	patternAssessment := patternScan(entry, stagingDir)
	if err := ctx.Err(); err != nil {
		return invalidScanReport(fmt.Sprintf("skill security scan cancelled: %v", err))
	}

	report := &ScanReport{
		PatternAssessment: patternAssessment,
		AgentScore:        -1,
		FinalLevel:        patternAssessment.Level,
		Summary:           patternAssessment.Reason,
		Recommendation:    RecommendationForLevel(patternAssessment.Level),
		ScannedBy:         "pattern",
	}

	// Step 1b: pure-Go static file scan inspired by Cisco AI Defense's
	// skill-scanner signature/YARA layer. This scans actual package files
	// before install and can upgrade the deterministic risk floor.
	staticFindings := runStaticFileScan(stagingDir, manifest)
	if err := ctx.Err(); err != nil {
		return invalidScanReport(fmt.Sprintf("skill security scan cancelled: %v", err))
	}
	if len(staticFindings) > 0 {
		report.Findings = append(report.Findings, staticFindings...)
		staticLevel := highestFindingLevel(staticFindings)
		if security.RiskLevelOrder[staticLevel] > security.RiskLevelOrder[report.FinalLevel] {
			report.FinalLevel = staticLevel
			report.Summary = fmt.Sprintf("static file scan found %d issue(s); highest severity: %s", len(staticFindings), staticLevel)
			report.Recommendation = RecommendationForLevel(report.FinalLevel)
		}
	}

	// Step 2: agent scan (optional, can only upgrade).
	if s.llm != nil && s.llm.Available() {
		sendStatus("Running AI skill security scan...")
		agentResult := agentScan(ctx, s.llm, entry, stagingDir, manifest)
		if agentResult != nil {
			report.AgentScore = agentResult.AgentScore
			report.Findings = append(report.Findings, agentResult.Findings...)
			report.ScannedBy = "agent+pattern"

			agentLevel := AgentScoreToLevel(agentResult.AgentScore)
			if security.RiskLevelOrder[agentLevel] > security.RiskLevelOrder[report.FinalLevel] {
				report.FinalLevel = agentLevel
				report.Summary = agentResult.Summary
			}

			if agentResult.Recommendation != "" && security.RiskLevelOrder[agentLevel] >= security.RiskLevelOrder[report.FinalLevel] {
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
func invalidScanReport(reason string) *ScanReport {
	return &ScanReport{
		PatternAssessment: security.RiskAssessment{
			Level:   security.RiskCritical,
			Reason:  reason,
			Factors: []string{reason},
		},
		AgentScore:     -1,
		FinalLevel:     security.RiskCritical,
		Summary:        reason,
		Recommendation: RecommendationForLevel(security.RiskCritical),
		ScannedBy:      "pattern",
	}
}

// Pattern scan.

// patternScan runs the corelib/security regex/keyword-based assessment.
// Creates a shallow copy of entry to set SkillDir without mutating the caller's object.
func patternScan(entry *corelib.NLSkillEntry, stagingDir string) security.RiskAssessment {
	// Convert NLSkillEntry to SkillRiskInput for corelib/security.
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
	if meta := skillMetadataScanParams(entry); len(meta) > 0 {
		input.Steps = append(input.Steps, struct {
			Action string
			Params map[string]interface{}
		}{Action: "skill_metadata", Params: meta})
	}

	assessor := &security.RiskAssessor{}
	return assessor.AssessSkill(input, entry.TrustLevel)
}

func skillMetadataScanParams(entry *corelib.NLSkillEntry) map[string]interface{} {
	if entry == nil {
		return nil
	}
	params := make(map[string]interface{})
	add := func(key, value string) {
		value = strings.TrimSpace(value)
		if value != "" {
			params[key] = value
		}
	}
	add("name", entry.Name)
	add("description", entry.Description)
	add("content", entry.Content)
	if len(entry.Triggers) > 0 {
		params["triggers"] = strings.Join(entry.Triggers, "\n")
	}
	if len(entry.RequiredEnv) > 0 {
		params["required_env"] = strings.Join(entry.RequiredEnv, "\n")
	}
	for i, op := range entry.Operations {
		add(fmt.Sprintf("operation_%d_name", i), op.Name)
		add(fmt.Sprintf("operation_%d_description", i), op.Description)
	}
	return params
}

// Agent scan.

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

// Response parsing (exported for testing).
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

// Level mapping (exported for GUI/TUI).

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
		return "Safe to install."
	case security.RiskMedium:
		return "Install with normal caution and review permissions if needed."
	case security.RiskHigh:
		return "Review carefully; standard mode asks when confirmation is available, otherwise records and allows."
	case security.RiskCritical:
		return "Critical risk detected; strict mode blocks, other modes record or request approval according to policy."
	default:
		return ""
	}
}

// FormatScanReportForUser formats a scan report for display in chat.
func FormatScanReportForUser(report *ScanReport, skillName string) string {
	if report == nil {
		return fmt.Sprintf("Skill %s security scan did not produce a report.\n", skillName)
	}
	var sb strings.Builder

	switch {
	case report.IsSafe():
		sb.WriteString(fmt.Sprintf("Skill %s passed the security scan.\n", skillName))
	case report.NeedsUserReview() && !report.IsDangerous():
		sb.WriteString(fmt.Sprintf("Skill %s has high-risk findings. Current policy decides whether to ask, record, or block.\n", skillName))
	case report.IsDangerous():
		sb.WriteString(fmt.Sprintf("Skill %s has critical findings. Strict mode blocks; non-strict modes record or ask according to policy.\n", skillName))
	}

	sb.WriteString(fmt.Sprintf("Summary: %s\n", report.Summary))

	if len(report.Findings) == 0 && len(report.PatternAssessment.Factors) > 0 {
		sb.WriteString("\nRisk factors:\n")
		for _, f := range report.PatternAssessment.Factors {
			sb.WriteString(fmt.Sprintf("  - %s\n", f))
		}
	}

	if len(report.Findings) > 0 {
		sb.WriteString("\nFindings:\n")
		for _, f := range report.Findings {
			if report.IsSafe() && (f.Severity == "info" || f.Severity == "low") {
				continue
			}
			severity := f.Severity
			if severity == "" {
				severity = "unknown"
			}
			loc := ""
			if f.Location != "" {
				loc = fmt.Sprintf(" [%s]", f.Location)
			}
			sb.WriteString(fmt.Sprintf("  - %s: %s%s\n", severity, f.Description, loc))
		}
	}

	if report.Recommendation != "" {
		sb.WriteString(fmt.Sprintf("\nRecommendation: %s\n", report.Recommendation))
	}

	switch report.ScannedBy {
	case "agent+pattern":
		sb.WriteString("(scanned by agent and pattern rules)\n")
	case "pattern":
		sb.WriteString("(scanned by pattern rules)\n")
	}

	return sb.String()
}
