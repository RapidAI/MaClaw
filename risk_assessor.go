package main

import (
	"fmt"
	"strings"
)

// RiskLevel represents the assessed risk level of a tool invocation.
type RiskLevel string

const (
	RiskLow      RiskLevel = "low"
	RiskMedium   RiskLevel = "medium"
	RiskHigh     RiskLevel = "high"
	RiskCritical RiskLevel = "critical"
)

// riskLevelOrder maps each RiskLevel to a numeric value for comparison.
var riskLevelOrder = map[RiskLevel]int{
	RiskLow:      0,
	RiskMedium:   1,
	RiskHigh:     2,
	RiskCritical: 3,
}

// RiskContext contains contextual information for risk assessment.
type RiskContext struct {
	ToolName       string
	Arguments      map[string]interface{}
	SessionID      string
	ProjectPath    string
	PermissionMode PermissionMode
	CallCount      int // consecutive call count for the same tool in the same session
}

// RiskAssessment is the result of a risk evaluation.
type RiskAssessment struct {
	Level   RiskLevel
	Reason  string
	Factors []string // factors that influenced the assessment
}

// RiskAssessor performs intent-level risk assessment on tool invocations.
type RiskAssessor struct{}

// dangerousKeywords are parameter substrings that immediately trigger critical risk.
var dangerousKeywords = []string{"rm -rf", "DROP TABLE", "format", "sudo"}

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
