package security

import (
	"fmt"
	"strings"
)

// RiskAssessor performs intent-level risk assessment on tool invocations.
type RiskAssessor struct{}

// dangerousKeywords are parameter substrings that immediately trigger critical risk.
// NOTE: "format" was removed because it causes false positives on legitimate
// skills that use "format" in non-destructive contexts (e.g. PDF format
// conversion, string formatting). Use dangerousPatterns for context-aware checks.
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

var systemDirPrefixes = []string{
	"/etc/", "/etc", "/usr/", "/usr", "/sbin/", "/sbin",
	"/boot/", "/boot", "/sys/", "/sys",
	"C:\\Windows", "C:\\WINDOWS", "c:\\windows",
	"C:\\Program Files", "c:\\program files",
}

// Assess evaluates the risk level of a tool invocation.
func (a *RiskAssessor) Assess(ctx RiskContext) RiskAssessment {
	level := RiskLow
	var factors []string

	argStr := flattenArgs(ctx.Arguments)

	for _, kw := range dangerousKeywords {
		if containsIgnoreCase(argStr, kw) {
			level = RiskCritical
			factors = append(factors, fmt.Sprintf("dangerous keyword %q found in arguments", kw))
		}
	}

	// Context-aware "format" check: only flag disk-formatting patterns,
	// not benign uses like "output format", "PDF format", etc.
	for _, pat := range dangerousFormatPatterns {
		if containsIgnoreCase(argStr, pat) {
			level = RiskCritical
			factors = append(factors, fmt.Sprintf("dangerous format pattern %q found in arguments", pat))
		}
	}

	if IsWriteOrExecuteTool(ctx.ToolName) {
		if RiskLevelOrder[level] < RiskLevelOrder[RiskMedium] {
			level = RiskMedium
		}
		factors = append(factors, fmt.Sprintf("tool %q is a write/execute tool", ctx.ToolName))
	}

	if !IsWriteOrExecuteTool(ctx.ToolName) && level == RiskLow {
		factors = append(factors, fmt.Sprintf("tool %q is a read-only tool", ctx.ToolName))
	}

	if IsWriteOrExecuteTool(ctx.ToolName) && isSystemDirectory(ctx.ProjectPath) {
		level = EscalateRiskLevel(level)
		factors = append(factors, fmt.Sprintf("operation targets system directory %q", ctx.ProjectPath))
	}

	if ctx.PermissionMode == "read-only" && IsWriteOrExecuteTool(ctx.ToolName) {
		level = RiskCritical
		factors = append(factors, "write operation in read-only mode")
	}

	if ctx.CallCount > 10 {
		level = EscalateRiskLevel(level)
		factors = append(factors, fmt.Sprintf("tool called %d times consecutively (>10)", ctx.CallCount))
	}

	reason := BuildReason(level, factors)
	return RiskAssessment{Level: level, Reason: reason, Factors: factors}
}

// SkillRiskInput describes a skill for risk assessment.
type SkillRiskInput struct {
	Name  string // skill name for safe-tool category matching
	Steps []struct {
		Action string
		Params map[string]interface{}
	}
}

// AssessSkill evaluates the risk level of an entire skill.
// Safe-tool category: skills whose name matches a safe category (pdf, qr,
// pptx, etc.) are downgraded from critical/high to medium at most.
func (a *RiskAssessor) AssessSkill(skill SkillRiskInput, trustLevel string) RiskAssessment {
	maxRisk := RiskLow
	var factors []string

	for _, step := range skill.Steps {
		stepAssessment := a.Assess(RiskContext{
			ToolName:  step.Action,
			Arguments: step.Params,
		})
		if RiskLevelOrder[stepAssessment.Level] > RiskLevelOrder[maxRisk] {
			maxRisk = stepAssessment.Level
			factors = append(factors, stepAssessment.Factors...)
		}
	}

	// Safe-tool category downgrade: if the skill name matches a known safe
	// utility category, cap risk at medium.
	if (maxRisk == RiskCritical || maxRisk == RiskHigh) && skill.Name != "" {
		skillLower := strings.ToLower(skill.Name)
		for _, cat := range safeToolCategories {
			if strings.Contains(skillLower, cat) {
				maxRisk = RiskMedium
				factors = append(factors, fmt.Sprintf("safe-tool category %q matched: risk capped at medium", cat))
				break
			}
		}
	}

	if trustLevel == "official" && maxRisk == RiskMedium {
		maxRisk = RiskLow
		factors = append(factors, "official trust level: medium downgraded to low")
	}
	if trustLevel == "unknown" && maxRisk == RiskLow {
		maxRisk = RiskMedium
		factors = append(factors, "unknown trust level: low upgraded to medium")
	}

	return RiskAssessment{Level: maxRisk, Reason: BuildReason(maxRisk, factors), Factors: factors}
}

// EscalateRiskLevel raises the risk level by one step.
func EscalateRiskLevel(current RiskLevel) RiskLevel {
	switch current {
	case RiskLow:
		return RiskMedium
	case RiskMedium:
		return RiskHigh
	case RiskHigh, RiskCritical:
		return RiskCritical
	default:
		return RiskCritical
	}
}

// ReduceRiskLevel lowers the risk level by one step.
func ReduceRiskLevel(level RiskLevel) RiskLevel {
	switch level {
	case RiskCritical:
		return RiskHigh
	case RiskHigh:
		return RiskMedium
	case RiskMedium:
		return RiskLow
	default:
		return RiskLow
	}
}

// IsWriteOrExecuteTool checks if a tool name implies write/execute operations.
func IsWriteOrExecuteTool(toolName string) bool {
	lower := strings.ToLower(toolName)
	for _, kw := range []string{"write", "create", "delete", "remove", "execute", "run", "bash", "shell", "apply", "deploy", "push", "install"} {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

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

func containsIgnoreCase(s, substr string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}

// BuildReason generates a human-readable reason string.
func BuildReason(level RiskLevel, factors []string) string {
	if len(factors) == 0 {
		return fmt.Sprintf("risk level: %s", level)
	}
	return fmt.Sprintf("risk level: %s — %s", level, strings.Join(factors, "; "))
}
