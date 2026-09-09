package security

import (
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"sync"
)

// RiskAnalyzer performs regex-based risk analysis on tool calls.
type RiskAnalyzer struct {
	mu              sync.RWMutex
	builtinPatterns []RiskPattern
	customPatterns  []RiskPattern
	compiledTool    map[string]*regexp.Regexp
	compiledParam   map[string]*regexp.Regexp
}

// NewRiskAnalyzer creates a risk analyzer with default builtin patterns.
func NewRiskAnalyzer() *RiskAnalyzer {
	ra := &RiskAnalyzer{
		builtinPatterns: DefaultRiskPatterns,
		compiledTool:    make(map[string]*regexp.Regexp),
		compiledParam:   make(map[string]*regexp.Regexp),
	}
	for _, p := range ra.builtinPatterns {
		ra.compilePattern(p)
	}
	return ra
}

func (a *RiskAnalyzer) compilePattern(p RiskPattern) {
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
func (a *RiskAnalyzer) Assess(toolName string, args map[string]interface{}, ctx *CallContext) RiskAssessment {
	a.mu.RLock()
	defer a.mu.RUnlock()

	result := RiskAssessment{Level: RiskLow}
	var matchedPatterns []string

	allPatterns := make([]RiskPattern, 0, len(a.builtinPatterns)+len(a.customPatterns))
	allPatterns = append(allPatterns, a.builtinPatterns...)
	allPatterns = append(allPatterns, a.customPatterns...)
	for _, p := range allPatterns {
		if a.matchPattern(p, toolName, args) {
			matchedPatterns = append(matchedPatterns, p.Name)
			if RiskLevelOrder[p.Level] > RiskLevelOrder[result.Level] {
				result.Level = p.Level
				result.Reason = p.Description
			}
		}
	}
	result.Factors = matchedPatterns

	if len(matchedPatterns) == 0 {
		return result
	}

	if ctx != nil {
		reduced := false
		if ctx.UserMessage != "" && userExplicitlyRequested(ctx.UserMessage) {
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
			result.Level = ReduceRiskLevel(result.Level)
		}
	}

	if result.Level == RiskCritical || result.Level == RiskHigh {
		result.Factors = append(result.Factors, "建议在执行前确认操作范围")
	}
	return result
}

func (a *RiskAnalyzer) matchPattern(p RiskPattern, toolName string, args map[string]interface{}) bool {
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

func userExplicitlyRequested(msg string) bool {
	msg = strings.ToLower(msg)
	for _, kw := range []string{"删除", "delete", "remove", "rm ", "push", "发布", "publish", "执行"} {
		if strings.Contains(msg, kw) {
			return true
		}
	}
	return false
}

// AddCustomPattern adds a user-defined risk pattern.
func (a *RiskAnalyzer) AddCustomPattern(pattern RiskPattern) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.compilePattern(pattern)
	a.customPatterns = append(a.customPatterns, pattern)
}

// LoadCustomPatterns loads custom patterns from a JSON file.
func (a *RiskAnalyzer) LoadCustomPatterns(path string) error {
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

// DefaultRiskPatterns defines the built-in risk detection rules.
var DefaultRiskPatterns = []RiskPattern{
	{Name: "recursive_delete", Category: "file_delete", ToolMatch: "(?i)bash|shell",
		ParamKey: "command", ParamMatch: `rm\s+-rf|rmdir\s+/s|del\s+/[fq]`, Level: RiskCritical,
		Description: "递归删除文件或目录"},
	{Name: "shutil_rmtree", Category: "file_delete", ToolMatch: "(?i)bash|shell",
		ParamKey: "command", ParamMatch: `shutil\.rmtree|os\.removedirs`, Level: RiskCritical,
		Description: "Python 递归删除"},
	{Name: "data_exfil_curl", Category: "network", ToolMatch: "(?i)bash|shell",
		ParamKey: "command", ParamMatch: `curl\s+.*-X\s+POST|curl\s+.*--data|curl\s+.*-d\s`, Level: RiskHigh,
		Description: "通过 curl POST 发送数据"},
	{Name: "data_exfil_wget", Category: "network", ToolMatch: "(?i)bash|shell",
		ParamKey: "command", ParamMatch: `wget\s+--post`, Level: RiskHigh,
		Description: "通过 wget POST 发送数据"},
	{Name: "netcat", Category: "network", ToolMatch: "(?i)bash|shell",
		ParamKey: "command", ParamMatch: `\bnc\s+-|ncat\s+`, Level: RiskHigh,
		Description: "使用 netcat 进行网络通信"},
	{Name: "chmod_777", Category: "permission", ToolMatch: "(?i)bash|shell",
		ParamKey: "command", ParamMatch: `chmod\s+777`, Level: RiskHigh,
		Description: "设置文件权限为 777"},
	{Name: "chown", Category: "permission", ToolMatch: "(?i)bash|shell",
		ParamKey: "command", ParamMatch: `chown\s+`, Level: RiskMedium,
		Description: "修改文件所有者"},
	{Name: "shutdown", Category: "system", ToolMatch: "(?i)bash|shell",
		ParamKey: "command", ParamMatch: `\bshutdown\b|\breboot\b`, Level: RiskCritical,
		Description: "关机或重启系统"},
	{Name: "systemctl_stop", Category: "system", ToolMatch: "(?i)bash|shell",
		ParamKey: "command", ParamMatch: `systemctl\s+stop|service\s+\w+\s+stop`, Level: RiskHigh,
		Description: "停止系统服务"},
	{Name: "kill_9", Category: "system", ToolMatch: "(?i)bash|shell",
		ParamKey: "command", ParamMatch: `kill\s+-9`, Level: RiskMedium,
		Description: "强制终止进程"},
	{Name: "env_secret", Category: "system", ToolMatch: "(?i)bash|shell",
		ParamKey: "command", ParamMatch: `(?i)export\s+\w*(KEY|SECRET|TOKEN|PASSWORD)\w*=`, Level: RiskMedium,
		Description: "修改敏感环境变量"},
	{Name: "pip_install_global", Category: "package", ToolMatch: "(?i)bash|shell",
		ParamKey: "command", ParamMatch: `pip\s+install\s+\w`, Level: RiskMedium,
		Description: "全局 pip install（非 requirements.txt）"},
	{Name: "npm_install_global", Category: "package", ToolMatch: "(?i)bash|shell",
		ParamKey: "command", ParamMatch: `npm\s+install\s+-g`, Level: RiskMedium,
		Description: "全局 npm install"},
	{Name: "drop_table", Category: "database", ToolMatch: ".*",
		ParamKey: "command", ParamMatch: `(?i)DROP\s+TABLE|DROP\s+DATABASE`, Level: RiskCritical,
		Description: "删除数据库表"},
	{Name: "delete_no_where", Category: "database", ToolMatch: ".*",
		ParamKey: "command", ParamMatch: `(?i)DELETE\s+FROM\s+\w+\s*$|TRUNCATE\s+`, Level: RiskHigh,
		Description: "无条件删除或截断数据"},
	{Name: "database_execute", Category: "database", ToolMatch: "^database$",
		ParamKey: "action", ParamMatch: `^(execute|batch_execute)$`, Level: RiskHigh,
		Description: "数据库写入或批处理"},
	{Name: "database_export", Category: "database", ToolMatch: "^database$",
		ParamKey: "action", ParamMatch: `^(write_table|export_excel)$`, Level: RiskMedium,
		Description: "数据库导出或表格写入"},
}
