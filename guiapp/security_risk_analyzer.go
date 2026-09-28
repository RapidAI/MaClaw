package guiapp

import (
	"encoding/json"
	"github.com/RapidAI/CodeClaw/corelib/security"
	"os"
	"regexp"
	"strings"
	"sync"
)

// SecurityCallContext provides context for risk assessment in the firewall.
type SecurityCallContext struct {
	UserMessage     string
	SessionID       string
	UserID          string
	RecentApprovals []string
	// FullControl is the input-box 「完全控制」 grant. It skips interactive
	// PolicyAsk prompts. Hard PolicyDeny is unchanged.
	FullControl bool
}

// SecurityRiskAnalyzer performs regex-based risk analysis on tool calls.
// It complements the existing RiskAssessor with pattern-matching rules.
type SecurityRiskAnalyzer struct {
	mu              sync.RWMutex
	builtinPatterns []security.RiskPattern
	customPatterns  []security.RiskPattern
	compiledTool    map[string]*regexp.Regexp
	compiledParam   map[string]*regexp.Regexp
}

// NewSecurityRiskAnalyzer creates a risk analyzer with default builtin patterns.
func NewSecurityRiskAnalyzer() *SecurityRiskAnalyzer {
	// Copy the shared table so a later custom-pattern edit cannot mutate corelib.
	builtin := make([]security.RiskPattern, len(security.DefaultRiskPatterns))
	copy(builtin, security.DefaultRiskPatterns)
	ra := &SecurityRiskAnalyzer{
		builtinPatterns: builtin,
		compiledTool:    make(map[string]*regexp.Regexp),
		compiledParam:   make(map[string]*regexp.Regexp),
	}
	for _, p := range ra.builtinPatterns {
		ra.compilePattern(p)
	}
	return ra
}

func (a *SecurityRiskAnalyzer) compilePattern(p security.RiskPattern) {
	if p.ToolMatch != "" {
		if _, ok := a.compiledTool[p.ToolMatch]; !ok {
			if re, err := regexp.Compile(p.ToolMatch); err == nil {
				a.compiledTool[p.ToolMatch] = re
			}
		}
	}
	if p.ParamMatch != "" {
		if _, ok := a.compiledParam[p.ParamMatch]; !ok {
			if re, err := regexp.Compile(p.ParamMatch); err == nil {
				a.compiledParam[p.ParamMatch] = re
			}
		}
	}
}

// Assess evaluates the risk of a tool call using pattern matching.
func (a *SecurityRiskAnalyzer) Assess(toolName string, args map[string]interface{}, ctx *SecurityCallContext) security.RiskAssessment {
	a.mu.RLock()
	defer a.mu.RUnlock()

	result := security.RiskAssessment{Level: security.RiskLow}
	var matchedPatterns []string

	// Build a combined slice without mutating the originals.
	allPatterns := make([]security.RiskPattern, 0, len(a.builtinPatterns)+len(a.customPatterns))
	allPatterns = append(allPatterns, a.builtinPatterns...)
	allPatterns = append(allPatterns, a.customPatterns...)
	for _, p := range allPatterns {
		if a.matchPattern(p, toolName, args) {
			matchedPatterns = append(matchedPatterns, p.Name)
			if security.RiskLevelOrder[p.Level] > security.RiskLevelOrder[result.Level] {
				result.Level = p.Level
				result.Reason = p.Description
			}
		}
	}
	result.Factors = matchedPatterns

	if len(matchedPatterns) == 0 {
		return result
	}

	// Context-aware risk reduction.
	if ctx != nil {
		reduced := false
		if ctx.UserMessage != "" && securityUserExplicitlyRequested(ctx.UserMessage) {
			reduced = true
		}
		if !reduced && len(ctx.RecentApprovals) > 0 {
			for _, approved := range ctx.RecentApprovals {
				if strings.Contains(strings.ToLower(toolName), strings.ToLower(approved)) {
					reduced = true
					break
				}
			}
		}
		if reduced {
			result.Level = reduceRiskLevel(result.Level)
		}
	}

	if result.Level == security.RiskCritical || result.Level == security.RiskHigh {
		result.Factors = append(result.Factors, "建议在执行前确认操作范围")
	}
	return result
}

func (a *SecurityRiskAnalyzer) matchPattern(p security.RiskPattern, toolName string, args map[string]interface{}) bool {
	if p.ToolMatch != "" {
		re := a.compiledTool[p.ToolMatch]
		if re == nil || !re.MatchString(toolName) {
			return false
		}
	}
	if p.ParamMatch != "" && p.ParamKey != "" {
		val, ok := args[p.ParamKey]
		if !ok {
			return false
		}
		valStr, ok := val.(string)
		if !ok {
			return false
		}
		re := a.compiledParam[p.ParamMatch]
		if re == nil || !re.MatchString(valStr) {
			return false
		}
	}
	return true
}

func securityUserExplicitlyRequested(msg string) bool {
	msg = strings.ToLower(msg)
	for _, kw := range []string{"删除", "delete", "remove", "rm ", "push", "发布", "publish", "执行"} {
		if strings.Contains(msg, kw) {
			return true
		}
	}
	return false
}

func reduceRiskLevel(level security.RiskLevel) security.RiskLevel {
	switch level {
	case security.RiskCritical:
		return security.RiskHigh
	case security.RiskHigh:
		return security.RiskMedium
	case security.RiskMedium:
		return security.RiskLow
	default:
		return security.RiskLow
	}
}

// AddCustomPattern adds a user-defined risk pattern.
func (a *SecurityRiskAnalyzer) AddCustomPattern(pattern security.RiskPattern) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.compilePattern(pattern)
	a.customPatterns = append(a.customPatterns, pattern)
}

// LoadCustomPatterns loads custom patterns from a JSON file.
func (a *SecurityRiskAnalyzer) LoadCustomPatterns(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var patterns []security.RiskPattern
	if err := json.Unmarshal(data, &patterns); err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, p := range patterns {
		a.compilePattern(p)
		a.customPatterns = append(a.customPatterns, p)
	}
	return nil
}

