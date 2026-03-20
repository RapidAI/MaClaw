package skillmarket

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// SecurityLabel 安全标签枚举。
const (
	LabelNetworkAccess    = "network_access"
	LabelFileSystemAccess = "file_system_access"
	LabelShellExec        = "shell_exec"
	LabelHardcodedSecrets = "hardcoded_secrets"
	LabelDatabaseAccess   = "database_access"
)

// SecurityFinding 单个安全发现。
type SecurityFinding struct {
	File    string `json:"file"`
	Line    int    `json:"line"`
	Label   string `json:"label"`
	Pattern string `json:"pattern"`
	Snippet string `json:"snippet"`
}

// SecurityReport 安全扫描报告。
type SecurityReport struct {
	Findings []SecurityFinding `json:"findings"`
	Labels   []string          `json:"labels"`
}

var (
	// 硬编码密钥/Token 模式
	secretPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)(api[_-]?key|secret[_-]?key|access[_-]?token|auth[_-]?token|password)\s*[:=]\s*["'][\w\-]{8,}["']`),
		regexp.MustCompile(`(?i)(sk-[a-zA-Z0-9]{20,}|ghp_[a-zA-Z0-9]{36,}|AKIA[0-9A-Z]{16})`),
	}
	// 危险操作模式
	dangerousPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\brm\s+-rf\s+/`),
		regexp.MustCompile(`(?i)\bformat\s+[a-z]:`),
		regexp.MustCompile(`(?i)\bDROP\s+(TABLE|DATABASE)\b`),
		regexp.MustCompile(`(?i)\bTRUNCATE\s+TABLE\b`),
	}
	// 网络调用模式
	networkPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\b(curl|wget)\s+`),
		regexp.MustCompile(`(?i)\brequests\.(get|post|put|delete|patch)\b`),
		regexp.MustCompile(`(?i)\bhttp\.(Get|Post|NewRequest)\b`),
		regexp.MustCompile(`(?i)\burllib\.request\b`),
		regexp.MustCompile(`(?i)\bfetch\s*\(`),
	}
	// Shell 执行模式
	shellExecPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\bos\.system\s*\(`),
		regexp.MustCompile(`(?i)\bsubprocess\.(run|call|Popen|check_output)\s*\(`),
		regexp.MustCompile(`(?i)\bexec\.(Command|CommandContext)\s*\(`),
		regexp.MustCompile(`(?i)\beval\s*\(`),
		regexp.MustCompile(`(?i)\bos\.popen\s*\(`),
	}
	// 数据库访问模式
	databasePatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\bsqlite3\.connect\b`),
		regexp.MustCompile(`(?i)\bsql\.Open\b`),
		regexp.MustCompile(`(?i)\bCREATE\s+TABLE\b`),
		regexp.MustCompile(`(?i)\bINSERT\s+INTO\b`),
		regexp.MustCompile(`(?i)\bSELECT\s+.+\s+FROM\b`),
	}
)

// ScanPackage 扫描解压后的 Skill 包目录，返回安全报告。
func ScanPackage(sandboxDir string) (*SecurityReport, error) {
	report := &SecurityReport{}
	labelSet := make(map[string]bool)

	scanRules := []struct {
		patterns []*regexp.Regexp
		label    string
	}{
		{secretPatterns, LabelHardcodedSecrets},
		{dangerousPatterns, LabelFileSystemAccess},
		{networkPatterns, LabelNetworkAccess},
		{shellExecPatterns, LabelShellExec},
		{databasePatterns, LabelDatabaseAccess},
	}

	err := filepath.Walk(sandboxDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		// 只扫描文本文件
		ext := strings.ToLower(filepath.Ext(path))
		if !isScannable(ext) {
			return nil
		}
		relPath, _ := filepath.Rel(sandboxDir, path)

		f, err := os.Open(path)
		if err != nil {
			return nil // 跳过无法打开的文件
		}
		defer f.Close()

		scanner := bufio.NewScanner(f)
		lineNum := 0
		for scanner.Scan() {
			lineNum++
			line := scanner.Text()
			for _, rule := range scanRules {
				for _, pat := range rule.patterns {
					if loc := pat.FindString(line); loc != "" {
						report.Findings = append(report.Findings, SecurityFinding{
							File:    relPath,
							Line:    lineNum,
							Label:   rule.label,
							Pattern: pat.String(),
							Snippet: truncate(line, 120),
						})
						labelSet[rule.label] = true
					}
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	for label := range labelSet {
		report.Labels = append(report.Labels, label)
	}
	return report, nil
}

// GenerateLabels 从扫描报告生成安全标签列表。
func GenerateLabels(report *SecurityReport) []string {
	if report == nil {
		return nil
	}
	return report.Labels
}

// HasHardcodedSecrets 检查报告中是否包含硬编码密钥。
func HasHardcodedSecrets(report *SecurityReport) bool {
	if report == nil {
		return false
	}
	for _, label := range report.Labels {
		if label == LabelHardcodedSecrets {
			return true
		}
	}
	return false
}

func isScannable(ext string) bool {
	switch ext {
	case ".py", ".sh", ".bash", ".go", ".js", ".ts", ".yaml", ".yml",
		".json", ".toml", ".cfg", ".ini", ".conf", ".env", ".txt", ".md",
		".rb", ".pl", ".lua", ".r", ".sql":
		return true
	}
	return ext == "" // 无扩展名的文件也扫描
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
