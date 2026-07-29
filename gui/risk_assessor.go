package main

import (
	"fmt"
	"github.com/RapidAI/CodeClaw/corelib"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/security"
)

// RiskContext contains contextual information for risk assessment.
// Kept in gui/ because it uses the gui-local PermissionMode type.
type RiskContext struct {
	ToolName       string
	Arguments      map[string]interface{}
	SessionID      string
	ProjectPath    string
	PermissionMode PermissionMode
	CallCount      int // consecutive call count for the same tool in the same session
}

// RiskAssessor performs intent-level risk assessment on tool invocations.
// Kept in gui/ because it has gui-specific methods (AssessSkill takes *NLSkillEntry).
type RiskAssessor struct{}

// PermissionMode is gui-local (not in corelib/security).

// dangerousKeywords are parameter substrings that immediately trigger critical risk.
// NOTE: "format" was removed because it causes false positives on legitimate
// skills that use "format" in non-destructive contexts (e.g. PDF format
// conversion, string formatting). Use dangerousFormatPatterns for context-aware checks.
//
// NOTE: "sudo" was moved to dangerousCmdPatternsGUI for context-aware matching.
// As a plain substring, "sudo" matches "pseudo", "sudoku", and documentation
// text like "Run without sudo". The regex version uses word boundaries.
//
// NOTE: "rm -rf" was moved to corelib/security threatPatternCategories.destructive
// with a path-aware Guard. Only root/system-path targets escalate to critical.
// User-directory cleanup (rm -rf ~/old_build/) stays at medium.
var dangerousKeywords = []string{"DROP TABLE"}

// dangerousFormatPatterns are patterns where "format" IS dangerous (disk formatting).
var dangerousFormatPatterns = []string{"format c:", "format d:", "format e:", "format f:", "diskpart", "mkfs"}

// safeToolCategories are skill action/tool names that are inherently safe
// utility operations and should not be escalated to critical risk.
//
// DESIGN RULE: Only include terms that are unambiguously safe. Avoid generic
// verbs (search, fetch, query) that could appear in destructive skill names.
var safeToolCategories = []string{
	// Document conversion / formatting
	"pdf", "qr", "qrcode", "pptx", "ppt", "image", "screenshot",
	"generator", "converter", "formatter", "markdown",
	"csv", "json", "xml", "yaml", "html", "any2pdf", "md-to-pdf",
	// Academic / news data sources (read-only by nature)
	"paper", "papers", "digest", "daily", "news", "feed", "rss",
	"arxiv", "scholar", "hugging-face", "huggingface",
	// Read-only information services
	"weather", "stock", "price", "calendar", "clock", "timer",
	"translate", "reader",
	// Output-only generation (no system access)
	"password", "uuid",
}

// systemDirPrefixes are path prefixes considered system directories.
var systemDirPrefixes = []string{
	"/etc/", "/etc",
	"/usr/", "/usr",
	"/sbin/", "/sbin",
	"/boot/", "/boot",
	"/sys/", "/sys",
	"C:\\Windows", "C:\\WINDOWS", "c:\\windows",
	"C:\\Program Files", "c:\\program files",
}

// Assess evaluates the risk level of a tool invocation based on its context.
func (a *RiskAssessor) Assess(ctx RiskContext) security.RiskAssessment {
	level := security.RiskLow
	var factors []string

	// Flatten all argument values into a single string for keyword scanning.
	argStr := flattenArgs(ctx.Arguments)

	// Rule 1: Check for dangerous keywords in arguments → critical.
	for _, kw := range dangerousKeywords {
		if containsIgnoreCase(argStr, kw) {
			level = security.RiskCritical
			factors = append(factors, fmt.Sprintf("dangerous keyword %q found in arguments", kw))
		}
	}

	// Rule 1a: Context-aware dangerous command patterns (word-boundary matching).
	for _, r := range security.CheckDangerousCmdPatterns(argStr) {
		if !r.SafeContext {
			level = security.RiskCritical
			factors = append(factors, fmt.Sprintf("dangerous command pattern %q matched in arguments", r.Pattern))
		} else {
			if security.RiskLevelOrder[level] < security.RiskLevelOrder[security.RiskHigh] {
				level = security.RiskHigh
			}
			factors = append(factors, fmt.Sprintf("dangerous command pattern %q matched but in safe context", r.Pattern))
		}
	}

	// Rule 1b: Context-aware "format" check: only flag disk-formatting patterns,
	// not benign uses like "output format", "PDF format", etc.
	for _, pat := range dangerousFormatPatterns {
		if containsIgnoreCase(argStr, pat) {
			level = security.RiskCritical
			factors = append(factors, fmt.Sprintf("dangerous format pattern %q found in arguments", pat))
		}
	}

	// Rule 1c: Structural threat patterns (destructive, traversal, privilege escalation, etc.).
	// Uses the "live-safe" subset — categories like injection/exfiltration/network/supply_chain
	// are excluded because they produce false positives when the user explicitly asks the
	// agent to run scp, ssh tunnels, pip --pre, etc. Those categories still apply in
	// skill installation scanning (AssessSkill).
	for _, tm := range security.ScanLiveThreatPatterns(argStr) {
		if security.RiskLevelOrder[level] < security.RiskLevelOrder[security.RiskHigh] {
			level = security.RiskHigh
		}
		factors = append(factors, fmt.Sprintf("threat pattern [%s]: %q matched", tm.Category, tm.Pattern))
	}

	// Rule 2: Write/execute tools → at least medium.
	if isWriteOrExecuteTool(ctx.ToolName) {
		if security.RiskLevelOrder[level] < security.RiskLevelOrder[security.RiskMedium] {
			level = security.RiskMedium
		}
		factors = append(factors, fmt.Sprintf("tool %q is a write/execute tool", ctx.ToolName))
	}

	// Rule 3: Read-only queries stay low (already the default).
	if !isWriteOrExecuteTool(ctx.ToolName) && level == security.RiskLow {
		factors = append(factors, fmt.Sprintf("tool %q is a read-only tool", ctx.ToolName))
	}

	// Context-aware adjustments:

	// Rule 4: System directory write → escalate one level.
	if isWriteOrExecuteTool(ctx.ToolName) && isSystemDirectory(ctx.ProjectPath) {
		level = escalateRiskLevel(level)
		factors = append(factors, fmt.Sprintf("operation targets system directory %q", ctx.ProjectPath))
	}

	// Rule 5: Read-only mode + write operation → critical.
	if ctx.PermissionMode == PermissionModeReadOnly && isWriteOrExecuteTool(ctx.ToolName) {
		level = security.RiskCritical
		factors = append(factors, "write operation in read-only mode")
	}

	// Rule 6: Same tool called >10 times consecutively → escalate one level.
	if ctx.CallCount > 10 {
		level = escalateRiskLevel(level)
		factors = append(factors, fmt.Sprintf("tool called %d times consecutively (>10)", ctx.CallCount))
	}

	reason := buildReason(level, factors)
	return security.RiskAssessment{
		Level:   level,
		Reason:  reason,
		Factors: factors,
	}
}

// escalateRiskLevel raises the risk level by one step, capping at critical.
func escalateRiskLevel(current security.RiskLevel) security.RiskLevel {
	switch current {
	case security.RiskLow:
		return security.RiskMedium
	case security.RiskMedium:
		return security.RiskHigh
	case security.RiskHigh:
		return security.RiskCritical
	case security.RiskCritical:
		return security.RiskCritical
	default:
		return security.RiskCritical
	}
}

// isSystemDirectory checks whether the given path is under a system directory.
func isSystemDirectory(path string) bool {
	if path == "" {
		return false
	}
	lower := strings.ToLower(path)
	for _, prefix := range systemDirPrefixes {
		if strings.HasPrefix(lower, strings.ToLower(prefix)) {
			return true
		}
	}
	return false
}

// flattenArgs recursively converts argument values to a single string for scanning.
func flattenArgs(args map[string]interface{}) string {
	if len(args) == 0 {
		return ""
	}
	var sb strings.Builder
	for _, v := range args {
		flattenValue(&sb, v)
		sb.WriteByte(' ')
	}
	return sb.String()
}

// flattenValue appends the string representation of a value to the builder.
func flattenValue(sb *strings.Builder, v interface{}) {
	switch val := v.(type) {
	case string:
		sb.WriteString(val)
	case map[string]interface{}:
		for _, inner := range val {
			flattenValue(sb, inner)
			sb.WriteByte(' ')
		}
	case []interface{}:
		for _, item := range val {
			flattenValue(sb, item)
			sb.WriteByte(' ')
		}
	default:
		sb.WriteString(fmt.Sprintf("%v", val))
	}
}

// containsIgnoreCase checks if s contains substr (case-insensitive).
func containsIgnoreCase(s, substr string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}

// buildReason generates a human-readable reason string from the risk level and factors.
func buildReason(level security.RiskLevel, factors []string) string {
	if len(factors) == 0 {
		return fmt.Sprintf("risk level: %s", level)
	}
	return fmt.Sprintf("risk level: %s — %s", level, strings.Join(factors, "; "))
}

// AssessSkill evaluates the risk level of an entire Skill by scanning all
// steps and taking the highest risk level. Trust level hierarchy:
// - builtin: cap maximum risk at low
// - trusted: cap maximum risk at medium
// - agent-created: standard assessment (no modification)
// - community: escalate assessed risk by one step
// Backward compatibility: "official" → "trusted", "unknown" → "community".
// Safe-tool category: skills whose name matches a safe category (pdf, qr,
// pptx, etc.) are downgraded from critical/high to medium at most, since
// they perform common utility operations that are not inherently dangerous.
// Enhanced with 12 threat pattern categories and prompt injection detection.
// Requirements: 4.1, 4.2, 4.3, 4.7, 4.8, 4.9, 4.10
func (a *RiskAssessor) AssessSkill(skill *corelib.NLSkillEntry, trustLevel string) security.RiskAssessment {
	maxRisk := security.RiskLow
	var factors []string
	hasHardSecuritySignal := false // tracks prompt injection, threat patterns, structural/unicode anomalies, dangerous keywords

	for _, step := range skill.Steps {
		stepAssessment := a.Assess(RiskContext{
			ToolName:  step.Action,
			Arguments: step.Params,
		})
		if security.RiskLevelOrder[stepAssessment.Level] > security.RiskLevelOrder[maxRisk] {
			maxRisk = stepAssessment.Level
			factors = append(factors, stepAssessment.Factors...)
		}
		// Assess() sets critical for dangerous keywords/commands/format patterns.
		if stepAssessment.Level == security.RiskCritical {
			hasHardSecuritySignal = true
		}

		// Scan step commands/params against threat pattern categories not already
		// covered by Assess() above. Assess() internally runs ScanLiveThreatPatterns
		// (destructive, traversal, obfuscation, mining, execution); here we scan
		// the remaining categories (exfiltration, injection, network, supply_chain,
		// persistence, credential_exposure, privilege_escalation).
		argStr := flattenArgs(step.Params)
		threatMatches := security.ScanSkillOnlyThreatPatterns(argStr)
		for _, tm := range threatMatches {
			if security.RiskLevelOrder[maxRisk] < security.RiskLevelOrder[security.RiskHigh] {
				maxRisk = security.RiskHigh
			}
			hasHardSecuritySignal = true
			factors = append(factors, fmt.Sprintf("threat pattern [%s]: %q matched", tm.Category, tm.Pattern))
		}

		// Scan for prompt injection patterns
		injectionMatches := security.ScanPromptInjection(argStr)
		for _, tm := range injectionMatches {
			if security.RiskLevelOrder[maxRisk] < security.RiskLevelOrder[security.RiskCritical] {
				maxRisk = security.RiskCritical
			}
			hasHardSecuritySignal = true
			factors = append(factors, fmt.Sprintf("prompt injection detected: %q matched", tm.Pattern))
		}

		// Scan for invisible Unicode characters, RTL overrides, and homoglyphs
		// Requirements: 4.4
		unicodeMatches := security.ScanUnicodeAnomalies(argStr)
		if len(unicodeMatches) > 0 {
			maxRisk = escalateRiskLevel(maxRisk)
			hasHardSecuritySignal = true
			for _, tm := range unicodeMatches {
				factors = append(factors, fmt.Sprintf("unicode anomaly: %s", tm.Pattern))
			}
		}
	}

	// Structural checks on skill directory
	// Requirements: 4.5, 4.6
	if skill.SkillDir != "" {
		structuralMatches := security.ScanDirectoryStructure(skill.SkillDir)
		if len(structuralMatches) > 0 {
			maxRisk = escalateRiskLevel(maxRisk)
			hasHardSecuritySignal = true
			for _, tm := range structuralMatches {
				factors = append(factors, fmt.Sprintf("structural anomaly: %s", tm.Pattern))
			}
		}
	}

	// 4-tier trust level hierarchy: builtin > trusted > agent-created > community
	// Normalize legacy values: "official" → "trusted", "unknown" → "community"
	normalized := security.NormalizeTrustLevel(trustLevel)
	switch normalized {
	case security.TrustLevelBuiltin:
		// Cap maximum risk at low regardless of pattern matches
		if security.RiskLevelOrder[maxRisk] > security.RiskLevelOrder[security.RiskLow] {
			factors = append(factors, fmt.Sprintf("builtin trust level: %s capped to low", maxRisk))
			maxRisk = security.RiskLow
		}
	case security.TrustLevelTrusted:
		// Cap maximum risk at medium
		if security.RiskLevelOrder[maxRisk] > security.RiskLevelOrder[security.RiskMedium] {
			factors = append(factors, fmt.Sprintf("trusted trust level: %s capped to medium", maxRisk))
			maxRisk = security.RiskMedium
		}
	case security.TrustLevelCommunity:
		// Escalate assessed risk by one step
		escalated := escalateRiskLevel(maxRisk)
		if escalated != maxRisk {
			factors = append(factors, fmt.Sprintf("community trust level: %s escalated to %s", maxRisk, escalated))
			maxRisk = escalated
		}
		// agent-created and any other value: standard assessment (no modification)
	}

	// Safe-tool category downgrade: MUST run AFTER trust level escalation.
	// Caps safe-category skills (weather, translate, pdf, etc.) at medium,
	// preventing the common false positive of bash(medium) + community(high).
	//
	// SECURITY GUARD: Does NOT apply when hard security signals are present
	// (prompt injection, threat patterns, structural anomalies, unicode
	// attacks, dangerous keywords). A skill name cannot override actual malice.
	if (maxRisk == security.RiskCritical || maxRisk == security.RiskHigh) && skill.Name != "" && !hasHardSecuritySignal {
		skillLower := strings.ToLower(skill.Name)
		for _, cat := range safeToolCategories {
			if strings.Contains(skillLower, cat) {
				maxRisk = security.RiskMedium
				factors = append(factors, fmt.Sprintf("safe-tool category %q matched: risk capped at medium", cat))
				break
			}
		}
	}

	return security.RiskAssessment{
		Level:   maxRisk,
		Reason:  buildReason(maxRisk, factors),
		Factors: factors,
	}
}
