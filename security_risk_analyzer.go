package main

import (
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"sync"
)

// RiskPattern defines a regex-based risk detection rule for the SecurityFirewall.
type RiskPattern struct {
	Name        string    `json:"name"`
	Category    string    `json:"category"`
	ToolMatch   string    `json:"tool_match"`
	ParamMatch  string    `json:"param_match"`
	ParamKey    string    `json:"param_key"`
	Level       RiskLevel `json:"level"`
	Description string    `json:"description"`
}

// SecurityCallContext provides context for risk assessment in the firewall.
type SecurityCallContext struct {
	UserMessage     string
	SessionID       string
	RecentApprovals []string
}

// SecurityRiskAnalyzer performs regex-based risk analysis on tool calls.
// It complements the existing RiskAssessor with pattern-matching rules.
type SecurityRiskAnalyzer struct {
	mu              sync.RWMutex
	builtinPatterns []RiskPattern
	customPatterns  []RiskPattern
	compiledTool    map[string]*regexp.Regexp
	compiledParam   map[string]*regexp.Regexp
}

// NewSecurityRiskAnalyzer creates a risk analyzer with default builtin patterns.
func NewSecurityRiskAnalyzer() *SecurityRiskAnalyzer {
	ra := &SecurityRiskAnalyzer{
		builtinPatterns: defaultSecurityRiskPatterns,
		compiledTool:    make(map[string]*regexp.Regexp),
		compiledParam:   make(map[string]*regexp.Regexp),
	}
	for _, p := range ra.builtinPatterns {
		ra.compilePattern(p)
	}
	return ra
}

func (a *SecurityRiskAnalyzer) compilePattern(p RiskPattern) {
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
func (a *SecurityRiskAnalyzer) Assess(toolName string, args map[string]interface{}, ctx *SecurityCallContext) RiskAssessment {
	a.mu.RLock()
	defer a.mu.RUnlock()

	result := RiskAssessment{Level: RiskLow}
	var matchedPatterns []string

	allPatterns := append(a.builtinPatterns, a.customPatterns...)
	for _, p := range allPatterns {
		if a.matchPattern(p, toolName, args) {
			matchedPatterns = append(matchedPatterns, p.Name)
			if riskLevelOrder[p.Level] > riskLevelOrder[result.Level] {
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

	if result.Level == RiskCritical || result.Level == RiskHigh {
		result.Factors = append(result.Factors, "建议在执行前确认操作范围")
	}
	return result
}

func (a *SecurityRiskAnalyzer) matchPattern(p RiskPattern, toolName string, args map[string]interface{}) bool {
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
	for _, kw := range []string{"删除", "delete", "remove", "rm ", "push", "发布", "publish", "执行", "run"} {
		if strings.Contains(msg, kw) {
			return true
		}
	}
	return false
}

func reduceRiskLevel(level RiskLevel) RiskLevel {
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

// AddCustomPattern adds a user-defined risk pattern.
func (a *SecurityRiskAnalyzer) AddCustomPattern(pattern RiskPattern) {
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
	var patterns []RiskPattern
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

// defaultSecurityRiskPatterns defines the built-in risk detection rules.
var defaultSecurityRiskPatterns = []RiskPattern{
	// File deletion
	{Name: "recursive_delete", Category: "file_delete", ToolMatch: "(?i)bash|shell",
		ParamKey: "command", ParamMatch: `rm\s+-rf|rmdir\s+/s|del\s+/[fq]`, Level: RiskCritical,
		Description: "递归删除文件或目录"},
	{Name: "shutil_rmtree", Category: "file_delete", ToolMatch: "(?i)bash|shell",
		ParamKey: "command", ParamMatch: `shutil\.rmtree|os\.removedirs`, Level: RiskCritical,
		Description: "Python 递归删除"},
	// Network exfiltration
	{Name: "data_exfil_curl", Category: "network", ToolMatch: "(?i)bash|shell",
		ParamKey: "command", ParamMatch: `curl\s+.*-X\s+POST|curl\s+.*--data|curl\s+.*-d\s`, Level: RiskHigh,
		Description: "通过 curl POST 发送数据"},
	{Name: "data_exfil_wget", Category: "network", ToolMatch: "(?i)bash|shell",
		ParamKey: "command", ParamMatch: `wget\s+--post`, Level: RiskHigh,
		Description: "通过 wget POST 发送数据"},
	{Name: "netcat", Category: "network", ToolMatch: "(?i)bash|shell",
		ParamKey: "command", ParamMatch: `\bnc\s+-|ncat\s+`, Level: RiskHigh,
		Description: "使用 netcat 进行网络通信"},
	// Permission changes
	{Name: "chmod_777", Category: "permission", ToolMatch: "(?i)bash|shell",
		ParamKey: "command", ParamMatch: `chmod\s+777`, Level: RiskHigh,
		Description: "设置文件权限为 777"},
	{Name: "chown", Category: "permission", ToolMatch: "(?i)bash|shell",
		ParamKey: "command", ParamMatch: `chown\s+`, Level: RiskMedium,
		Description: "修改文件所有者"},
	// System commands
	{Name: "shutdown", Category: "system", ToolMatch: "(?i)bash|shell",
		ParamKey: "command", ParamMatch: `\bshutdown\b|\breboot\b`, Level: RiskCritical,
		Description: "关机或重启系统"},
	{Name: "systemctl_stop", Category: "system", ToolMatch: "(?i)bash|shell",
		ParamKey: "command", ParamMatch: `systemctl\s+stop|service\s+\w+\s+stop`, Level: RiskHigh,
		Description: "停止系统服务"},
	{Name: "kill_9", Category: "system", ToolMatch: "(?i)bash|shell",
		ParamKey: "command", ParamMatch: `kill\s+-9`, Level: RiskMedium,
		Description: "强制终止进程"},
	// Environment variables
	{Name: "env_secret", Category: "system", ToolMatch: "(?i)bash|shell",
		ParamKey: "command", ParamMatch: `(?i)export\s+\w*(KEY|SECRET|TOKEN|PASSWORD)\w*=`, Level: RiskMedium,
		Description: "修改敏感环境变量"},
	// Package management
	{Name: "pip_install_global", Category: "package", ToolMatch: "(?i)bash|shell",
		ParamKey: "command", ParamMatch: `pip\s+install\s+(?!-r\s)`, Level: RiskMedium,
		Description: "全局 pip install（非 requirements.txt）"},
	{Name: "npm_install_global", Category: "package", ToolMatch: "(?i)bash|shell",
		ParamKey: "command", ParamMatch: `npm\s+install\s+-g`, Level: RiskMedium,
		Description: "全局 npm install"},
	// Database
	{Name: "drop_table", Category: "database", ToolMatch: ".*",
		ParamKey: "command", ParamMatch: `(?i)DROP\s+TABLE|DROP\s+DATABASE`, Level: RiskCritical,
		Description: "删除数据库表"},
	{Name: "delete_no_where", Category: "database", ToolMatch: ".*",
		ParamKey: "command", ParamMatch: `(?i)DELETE\s+FROM\s+\w+\s*$|TRUNCATE\s+`, Level: RiskHigh,
		Description: "无条件删除或截断数据"},
}
