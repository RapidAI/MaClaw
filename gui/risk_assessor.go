package main

import (
	"fmt"
	"strings"
)

// RiskLevel, RiskAssessment,
// riskLevelOrder, RiskLow/Medium/High/Critical — see corelib_aliases.go

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
var dangerousKeywords = []string{"rm -rf", "DROP TABLE", "sudo"}

// dangerousFormatPatterns are patterns where "format" IS dangerous (disk formatting).
var dangerousFormatPatterns = []string{"format c:", "format d:", "format e:", "format f:", "diskpart", "mkfs"}

// safeToolCategories are skill action/tool names that are inherently safe
// utility operations and should not be escalated to critical risk.
var safeToolCategories = []string{
	"pdf", "qr", "qrcode", "pptx", "ppt", "image", "screenshot",
	"generator", "converter", "formatter", "markdown",
	"csv", "json", "xml", "yaml", "html", "any2pdf", "md-to-pdf",
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
func (a *RiskAssessor) Assess(ctx RiskContext) RiskAssessment {
	level := RiskLow
	var factors []string

	// Flatten all argument values into a single string for keyword scanning.
	argStr := flattenArgs(ctx.Arguments)

	// Rule 1: Check for dangerous keywords in arguments → critical.
	for _, kw := range dangerousKeywords {
		if containsIgnoreCase(argStr, kw) {
			level = RiskCritical
			factors = append(factors, fmt.Sprintf("dangerous keyword %q found in arguments", kw))
		}
	}

	// Rule 1b: Context-aware "format" check: only flag disk-formatting patterns,
	// not benign uses like "output format", "PDF format", etc.
	for _, pat := range dangerousFormatPatterns {
		if containsIgnoreCase(argStr, pat) {
			level = RiskCritical
			factors = append(factors, fmt.Sprintf("dangerous format pattern %q found in arguments", pat))
		}
	}

	// Rule 2: Write/execute tools → at least medium.
	if isWriteOrExecuteTool(ctx.ToolName) {
		if riskLevelOrder[level] < riskLevelOrder[RiskMedium] {
			level = RiskMedium
		}
		factors = append(factors, fmt.Sprintf("tool %q is a write/execute tool", ctx.ToolName))
	}

	// Rule 3: Read-only queries stay low (already the default).
	if !isWriteOrExecuteTool(ctx.ToolName) && level == RiskLow {
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
		level = RiskCritical
		factors = append(factors, "write operation in read-only mode")
	}

	// Rule 6: Same tool called >10 times consecutively → escalate one level.
	if ctx.CallCount > 10 {
		level = escalateRiskLevel(level)
		factors = append(factors, fmt.Sprintf("tool called %d times consecutively (>10)", ctx.CallCount))
	}

	reason := buildReason(level, factors)
	return RiskAssessment{
		Level:   level,
		Reason:  reason,
		Factors: factors,
	}
}

// escalateRiskLevel raises the risk level by one step, capping at critical.
func escalateRiskLevel(current RiskLevel) RiskLevel {
	switch current {
	case RiskLow:
		return RiskMedium
	case RiskMedium:
		return RiskHigh
	case RiskHigh:
		return RiskCritical
	case RiskCritical:
		return RiskCritical
	default:
		return RiskCritical
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
func buildReason(level RiskLevel, factors []string) string {
	if len(factors) == 0 {
		return fmt.Sprintf("risk level: %s", level)
	}
	return fmt.Sprintf("risk level: %s — %s", level, strings.Join(factors, "; "))
}

// AssessSkill evaluates the risk level of an entire Skill by scanning all
// steps and taking the highest risk level. Trust level adjustments:
// - official: medium → low
// - unknown: low → medium
// Safe-tool category: skills whose name matches a safe category (pdf, qr,
// pptx, etc.) are downgraded from critical/high to medium at most, since
// they perform common utility operations that are not inherently dangerous.
func (a *RiskAssessor) AssessSkill(skill *NLSkillEntry, trustLevel string) RiskAssessment {
	maxRisk := RiskLow
	var factors []string

	for _, step := range skill.Steps {
		stepAssessment := a.Assess(RiskContext{
			ToolName:  step.Action,
			Arguments: step.Params,
		})
		if riskLevelOrder[stepAssessment.Level] > riskLevelOrder[maxRisk] {
			maxRisk = stepAssessment.Level
			factors = append(factors, stepAssessment.Factors...)
		}
	}

	// Safe-tool category downgrade: if the skill name matches a known safe
	// utility category, cap risk at medium. This prevents false-positive
	// blocking of skills like "any2pdf", "QR Code Generator", "pptx-generator".
	if maxRisk == RiskCritical || maxRisk == RiskHigh {
		skillLower := strings.ToLower(skill.Name)
		for _, cat := range safeToolCategories {
			if strings.Contains(skillLower, cat) {
				maxRisk = RiskMedium
				factors = append(factors, fmt.Sprintf("safe-tool category %q matched: risk capped at medium", cat))
				break
			}
		}
	}

	// Trust-level adjustments
	if trustLevel == "official" && maxRisk == RiskMedium {
		maxRisk = RiskLow
		factors = append(factors, "official trust level: medium downgraded to low")
	}
	if trustLevel == "unknown" && maxRisk == RiskLow {
		maxRisk = RiskMedium
		factors = append(factors, "unknown trust level: low upgraded to medium")
	}

	return RiskAssessment{
		Level:   maxRisk,
		Reason:  buildReason(maxRisk, factors),
		Factors: factors,
	}
}
