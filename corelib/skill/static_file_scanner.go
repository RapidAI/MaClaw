package skill

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/security"
)

// staticFileRule is a small, pure-Go equivalent of the signature/YARA layer
// used by cisco-ai-defense/skill-scanner. It intentionally avoids invoking the
// Python scanner so installation can be protected in offline desktop builds.
type staticFileRule struct {
	ID          string
	Category    string
	Severity    string
	Description string
	FileTypes   []string
	Patterns    []*regexp.Regexp
	Excludes    []*regexp.Regexp
}

var staticFileRules = compileStaticFileRules([]struct {
	id          string
	category    string
	severity    string
	description string
	fileTypes   []string
	patterns    []string
	excludes    []string
}{
	{
		id:          "DATA_EXFIL_NETWORK_REQUESTS",
		category:    "data_exfiltration",
		severity:    "medium",
		description: "Outbound network request primitives that can transmit data externally",
		fileTypes:   []string{"python"},
		patterns: []string{
			`requests\.(?:get|post|put|delete|patch|request)\s*\(`,
			`httpx\.(?:get|post|put|delete|patch|request)\s*\(`,
			`aiohttp\.(?:ClientSession|request)\s*\(`,
			`urllib\.request\.(?:urlopen|Request)\s*\(`,
			`http\.client\.(?:HTTPConnection|HTTPSConnection)`,
		},
	},
	{
		id:          "DATA_EXFIL_HTTP_POST",
		category:    "data_exfiltration",
		severity:    "critical",
		description: "HTTP POST request that may send data externally",
		fileTypes:   []string{"python"},
		patterns: []string{
			`(?i)requests\.post\s*\([^\n)]{0,240}(?:attacker|evil|webhook|exfil|steal|leak|collect|discord\.com/api/webhooks|pastebin|telegram)`,
			`(?i)requests\.post\s*\([^\n)]{0,240}(?:data|json)\s*=\s*(?:\{[^\n}]{0,240}(?:password|passwd|secret|token|api[_-]?key|credential|auth|cookie|session|private[_-]?key)|[A-Za-z_][A-Za-z0-9_]*(?:secret|token|credential|passwd|password|api[_-]?key))`,
			`(?i)urllib\.request\.urlopen\s*\([^\n)]{0,240}(?:attacker|evil|webhook|exfil|steal|leak|collect)`,
		},
	},
	{
		id:          "DATA_EXFIL_SOCKET_CONNECT",
		category:    "data_exfiltration",
		severity:    "critical",
		description: "Direct socket connection to external server",
		fileTypes:   []string{"python"},
		patterns: []string{
			`socket\.socket\s*\([^)]*\)\.connect`,
			`socket\.create_connection`,
		},
		excludes: []string{`localhost`, `127\.0\.0\.1`, `0\.0\.0\.0`, `::1`, `def\s+(is_)?.*ready`, `def\s+.*health.*check`},
	},
	{
		id:          "DATA_EXFIL_SENSITIVE_FILES",
		category:    "data_exfiltration",
		severity:    "high",
		description: "Opening sensitive system or credential files",
		fileTypes:   []string{"python", "bash", "markdown", "javascript", "typescript"},
		patterns: []string{
			`(?:open|read)\s*\([^)]*["/](?:etc/passwd|etc/shadow|etc/sudoers)`,
			`(?:open|read)\s*\([^)]*\.aws/credentials`,
			`(?:open|read)\s*\([^)]*\.ssh/(?:id_rsa|id_dsa|id_ed25519|authorized_keys)`,
			`open\s*\([^)]*\.env(?:\.[A-Za-z0-9_-]+)?['"]\s*[,)]`,
			`(?:cat|type|Get-Content)\s+(?:~[/\\])?\.?(?:aws[/\\]credentials|ssh[/\\]id_rsa|ssh[/\\]id_ed25519|env\b)`,
		},
		excludes: []string{`(?i)example`, `(?i)test`, `(?i)placeholder`, `(?i)\.env\.(?:example|template|sample)`},
	},
	{
		id:          "DATA_EXFIL_BASE64_AND_NETWORK",
		category:    "data_exfiltration",
		severity:    "high",
		description: "Base64 encoding combined with network operations",
		fileTypes:   []string{"python", "bash", "javascript", "typescript"},
		patterns: []string{
			`base64\.(?:b64encode|encodebytes)[^\n]{0,160}(?:requests\.|urllib\.request|httpx\.|socket\.)`,
			`(?:requests\.|urllib\.request|httpx\.|socket\.)[^\n]{0,160}base64\.(?:b64encode|encodebytes)`,
			`base64\s+(?:-d|--decode)[^\n|;]{0,160}\|\s*(?:bash|sh|python|node|pwsh|powershell)`,
		},
	},
	{
		id:          "DATA_EXFIL_JS_NETWORK",
		category:    "data_exfiltration",
		severity:    "medium",
		description: "Outbound network request primitives in JavaScript/TypeScript",
		fileTypes:   []string{"javascript", "typescript"},
		patterns: []string{
			`\bfetch\s*\(`,
			`\baxios\.(?:get|post|put|delete|patch|request)\s*\(`,
			`(?:https?)\.(?:request|get)\s*\(`,
			`new\s+XMLHttpRequest\s*\(`,
		},
		excludes: []string{`(?i)example`, `(?i)mock`, `(?i)test`, `//.*fetch`},
	},
	{
		id:          "COMMAND_INJECTION_EVAL",
		category:    "command_injection",
		severity:    "critical",
		description: "Dangerous code execution functions that can execute arbitrary code",
		fileTypes:   []string{"python", "javascript", "typescript"},
		patterns: []string{
			`\beval\s*\(`,
			`(?m)(^|[^.'"])exec\s*\(`,
			`\b__import__\s*\(\s*(?:input\s*\(|request\.|os\.environ|sys\.argv|argv|args\[|user_)`,
			`(?m)(^|[^.'"])compile\s*\(`,
		},
		excludes: []string{`(?i)never\s+use\s+eval\s*\(`, `(?i)use\s+of\s+eval\s*\(`, `r['"].*eval\\s\*\\\(`},
	},
	{
		id:          "COMMAND_INJECTION_SHELL_EXEC",
		category:    "command_injection",
		severity:    "high",
		description: "Shell command execution that can be abused for injection",
		fileTypes:   []string{"python", "bash", "markdown"},
		patterns: []string{
			`subprocess\.(?:call|run|Popen)\s*\([^)]*shell\s*=\s*True`,
			`os\.system\s*\(`,
			`eval\s+["']?\$[0-9@*{]`,
			`find\s+[^|;]*-exec\s+(?:bash|sh|zsh|python|perl|ruby|node|rm|mv|cp|chmod|chown|curl|wget)\b`,
			`find\s+[^|;]*\|\s*xargs\s+(?:bash|sh|zsh|eval|exec)`,
		},
		excludes: []string{`(?i)example`, `(?i)test`, `#.*eval`},
	},
	{
		id:          "COMMAND_INJECTION_DOWNLOAD_EXECUTE",
		category:    "command_injection",
		severity:    "critical",
		description: "Downloads remote code and pipes it directly into an interpreter",
		fileTypes:   []string{"bash", "markdown", "text"},
		patterns: []string{
			`(?i)(?:curl|wget)\b[^\n|;&]{0,240}\|\s*(?:sudo\s+)?(?:bash|sh|zsh|python|python3|perl|ruby|node)\b`,
			`(?i)(?:iwr|irm|invoke-webrequest|invoke-restmethod)\b[^\n|;&]{0,240}\|\s*(?:iex|invoke-expression|powershell|pwsh)\b`,
			`(?i)(?:powershell|pwsh)(?:\.exe)?\b[^\n]{0,180}(?:-enc|-encodedcommand|iex|invoke-expression)\b`,
		},
		excludes: []string{`(?i)never\s+use`, `(?i)avoid`},
	},
	{
		id:          "COMMAND_INJECTION_JS_CHILD_PROCESS",
		category:    "command_injection",
		severity:    "critical",
		description: "Node.js child_process module usage for shell command execution",
		fileTypes:   []string{"javascript", "typescript"},
		patterns: []string{
			`require\s*\(\s*['"]child_process['"]\s*\)`,
			`from\s+['"]child_process['"]`,
			`child_process\.(?:exec|execSync|execFile|execFileSync|spawn|spawnSync|fork)\s*\(`,
			`\b(?:execSync|spawnSync)\s*\(`,
		},
		excludes: []string{`(?i)never\s+use\s+child_process`, `(?i)avoid\s+child_process`, `(?m)^\s*//`},
	},
	{
		id:          "COMMAND_INJECTION_JS_DYNAMIC_CODE",
		category:    "command_injection",
		severity:    "critical",
		description: "Dynamic JavaScript execution via Function constructor or string timers",
		fileTypes:   []string{"javascript", "typescript"},
		patterns:    []string{`new\s+Function\s*\(`, `(?m)(^|[^.])Function\s*\(\s*['"]`, `\bset(?:Timeout|Interval)\s*\(\s*['"]`},
	},
	{
		id:          "PROMPT_INJECTION_OVERRIDE",
		category:    "prompt_injection",
		severity:    "high",
		description: "Attempts to override, bypass, or manipulate system instructions",
		fileTypes:   []string{"markdown", "yaml", "text"},
		patterns: []string{
			`(?i)ignore\s+(all\s+)?(previous|prior|earlier|above)\s+(instructions|rules|prompts|guidelines)`,
			`(?i)disregard\s+(all\s+)?(previous|prior)\s+(instructions|rules)`,
			`(?i)you are now in\s+(unrestricted|debug|developer|admin|god|jailbreak)\s+mode`,
			`(?i)disable\s+(all\s+)?(safety|security|content|ethical)\s+(filters|checks|guidelines)`,
			`(?i)bypass\s+(content|usage|safety)\s+policy`,
			`(?i)do\s+not\s+(tell|inform|mention|notify)\s+(the\s+)?user`,
			`(?i)hide\s+(this|that)\s+(action|operation|step)`,
		},
	},
	{
		id:          "PROMPT_INJECTION_REVEAL_SYSTEM",
		category:    "prompt_injection",
		severity:    "medium",
		description: "Attempts to reveal system prompts or configuration",
		fileTypes:   []string{"markdown", "yaml", "text"},
		patterns: []string{
			`(?i)reveal\s+(your|the)\s+system\s+(prompt|instructions|message)`,
			`(?i)show\s+(me\s+)?(your|the)\s+(system|initial)\s+(prompt|configuration)`,
			`(?i)what\s+(are|is)\s+your\s+(system|initial)\s+(prompt|instructions)`,
		},
	},
	{
		id:          "SECRET_AWS_KEY",
		category:    "hardcoded_secrets",
		severity:    "critical",
		description: "AWS access key detected",
		fileTypes:   []string{"python", "bash", "markdown", "javascript", "typescript", "yaml", "text"},
		patterns:    []string{`(?:AKIA|AGPA|AIDA|AROA|AIPA|ANPA|ANVA|ASIA)[A-Z0-9]{16}`},
		excludes:    []string{`AKIAIOSFODNN7EXAMPLE`, `(?i)example`, `(?i)placeholder`, `(?i)test_key`, `(?i)fake`},
	},
	{
		id:          "SECRET_COMMON_TOKENS",
		category:    "hardcoded_secrets",
		severity:    "critical",
		description: "API key or token detected",
		fileTypes:   []string{"python", "bash", "markdown", "javascript", "typescript", "yaml", "text"},
		patterns: []string{
			`AIza[A-Za-z0-9_-]{35}`,
			`gh[pousr]_[A-Za-z0-9]{36,}`,
			`(?:sk|pk)_(?:live|test)_[A-Za-z0-9]{24,}`,
			`sk-proj-[A-Za-z0-9_-]{32,}`,
			`xox[baprs]-[A-Za-z0-9-]{20,}`,
		},
		excludes: []string{`(?i)example`, `(?i)placeholder`, `(?i)fake`, `(?i)test`},
	},
	{
		id:          "SECRET_PRIVATE_KEY",
		category:    "hardcoded_secrets",
		severity:    "critical",
		description: "Private key block detected",
		fileTypes:   []string{"python", "bash", "markdown", "javascript", "typescript", "yaml", "text"},
		patterns:    []string{`-----BEGIN (?:RSA |EC |DSA |OPENSSH )?PRIVATE KEY-----[\s\S]{80,}?-----END (?:RSA |EC |DSA |OPENSSH )?PRIVATE KEY-----`},
		excludes:    []string{`(?i)example`, `(?i)test`, `(?i)demo`, `(?i)sample`, `(?i)fake`, `(?i)placeholder`},
	},
	{
		id:          "SECRET_CONNECTION_STRING",
		category:    "hardcoded_secrets",
		severity:    "high",
		description: "Database connection string with embedded credentials",
		fileTypes:   []string{"python", "bash", "markdown", "javascript", "typescript", "yaml", "text"},
		patterns:    []string{`(?:mongodb|mysql|postgresql|postgres)://[^:\s]+:[^@\s]+@`},
		excludes:    []string{`(?i)user:pass`, `(?i)username:password`, `(?i)admin:admin`, `(?i)root:root`, `(?i)localhost`, `\$\{.*\}`, `%.*%`, `(?i)example`, `(?i)placeholder`},
	},
	{
		id:          "SVG_EMBEDDED_SCRIPT",
		category:    "command_injection",
		severity:    "critical",
		description: "SVG contains embedded script or event handler",
		fileTypes:   []string{"svg", "xml", "text"},
		patterns:    []string{`<script[^>]*>[\s\S]*?</script>`, `\bon\w+\s*=\s*["'][^"']*["']`, `javascript\s*:`},
	},
	{
		id:          "PDF_EMBEDDED_JAVASCRIPT",
		category:    "command_injection",
		severity:    "critical",
		description: "PDF contains embedded JavaScript or auto-action triggers",
		fileTypes:   []string{"pdf", "binary", "other"},
		patterns:    []string{`/JS\s*\(`, `/JavaScript\s`, `/OpenAction\s`, `/AA\s*<<`, `/Launch\s`},
	},
	{
		id:          "GLOB_HIDDEN_FILE_TARGETING",
		category:    "command_injection",
		severity:    "medium",
		description: "Glob or find pattern targets hidden files",
		fileTypes:   []string{"bash", "python", "markdown"},
		patterns:    []string{`glob\s*\([^)]*\.\*`, `find\s+[^|]*\s+-name\s+["']?\.\*`, `for\s+\w+\s+in\s+\.\*\s*;`},
	},
})

func compileStaticFileRules(raw []struct {
	id          string
	category    string
	severity    string
	description string
	fileTypes   []string
	patterns    []string
	excludes    []string
}) []staticFileRule {
	out := make([]staticFileRule, 0, len(raw))
	for _, r := range raw {
		rule := staticFileRule{
			ID:          r.id,
			Category:    r.category,
			Severity:    strings.ToLower(r.severity),
			Description: r.description,
			FileTypes:   r.fileTypes,
		}
		for _, p := range r.patterns {
			rule.Patterns = append(rule.Patterns, regexp.MustCompile(p))
		}
		for _, p := range r.excludes {
			rule.Excludes = append(rule.Excludes, regexp.MustCompile(p))
		}
		out = append(out, rule)
	}
	return out
}

func runStaticFileScan(stagingDir string, manifest []StagedFile) []ScanFinding {
	if stagingDir == "" {
		return nil
	}
	var findings []ScanFinding
	seen := make(map[string]bool)

	for _, file := range manifest {
		typ := scanFileType(file.RelPath, file.IsBinary)
		if structural := structuralFileFinding(file, typ); structural != nil {
			addFinding(&findings, seen, *structural)
		}

		content, ok, err := scanFileContent(filepath.Join(stagingDir, filepath.FromSlash(file.RelPath)), file)
		if err != nil {
			addFinding(&findings, seen, ScanFinding{
				Severity:    "critical",
				Category:    "unreadable_file",
				Description: "Skill file could not be read during security scan",
				Location:    file.RelPath,
			})
			continue
		}
		if !ok {
			continue
		}
		for _, rule := range staticFileRules {
			if !ruleAppliesToType(rule, typ) {
				continue
			}
			for _, pattern := range rule.Patterns {
				loc := firstUnexcludedMatchLocation(content, pattern, rule.Excludes, file.RelPath)
				if loc == "" {
					continue
				}
				addFinding(&findings, seen, ScanFinding{
					Severity:    rule.Severity,
					Category:    rule.Category,
					Description: fmt.Sprintf("%s (%s)", rule.Description, rule.ID),
					Location:    loc,
				})
				break
			}
		}
	}

	sort.SliceStable(findings, func(i, j int) bool {
		oi := security.RiskLevelOrder[severityToRiskLevel(findings[i].Severity)]
		oj := security.RiskLevelOrder[severityToRiskLevel(findings[j].Severity)]
		if oi == oj {
			return findings[i].Location < findings[j].Location
		}
		return oi > oj
	})
	return findings
}

func addFinding(findings *[]ScanFinding, seen map[string]bool, finding ScanFinding) {
	key := finding.Severity + "\x00" + finding.Category + "\x00" + finding.Description + "\x00" + finding.Location
	if seen[key] {
		return
	}
	seen[key] = true
	*findings = append(*findings, finding)
}

func structuralFileFinding(file StagedFile, typ string) *ScanFinding {
	base := filepath.Base(file.RelPath)
	lower := strings.ToLower(base)
	switch {
	case file.IsSymlink:
		return &ScanFinding{Severity: "critical", Category: "symlink", Description: "Symbolic link included in skill package", Location: file.RelPath}
	case base == ".gitkeep" || base == ".maclaw_scan_status.json":
		return nil
	case strings.HasPrefix(base, "."):
		return &ScanFinding{Severity: "medium", Category: "hidden_file", Description: "Hidden file included in skill package", Location: file.RelPath}
	case strings.HasSuffix(lower, ".pyc") || strings.HasSuffix(lower, ".pyo"):
		return &ScanFinding{Severity: "high", Category: "bytecode", Description: "Python bytecode file included; source may be hidden", Location: file.RelPath}
	case strings.HasSuffix(lower, ".exe") || strings.HasSuffix(lower, ".dll") || strings.HasSuffix(lower, ".dylib") || strings.HasSuffix(lower, ".so"):
		return &ScanFinding{Severity: "high", Category: "binary", Description: "Executable or native binary included in skill package", Location: file.RelPath}
	case file.IsBinary && typ == "binary":
		return &ScanFinding{Severity: "medium", Category: "binary", Description: "Binary file included in skill package", Location: file.RelPath}
	default:
		return nil
	}
}

func scanFileContent(path string, file StagedFile) (string, bool, error) {
	if file.IsSymlink {
		return "", false, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false, err
	}
	if len(data) > 512*1024 {
		data = data[:512*1024]
	}
	typ := scanFileType(file.RelPath, file.IsBinary)
	if file.IsBinary && typ != "pdf" && typ != "svg" && typ != "xml" {
		return "", false, nil
	}
	if bytes.IndexByte(data, 0) >= 0 && typ != "pdf" {
		return "", false, nil
	}
	if !utf8.Valid(data) && typ != "pdf" {
		return "", false, nil
	}
	return string(data), true, nil
}

func scanFileType(relPath string, isBinary bool) string {
	lower := strings.ToLower(relPath)
	ext := strings.TrimPrefix(filepath.Ext(lower), ".")
	switch ext {
	case "py":
		return "python"
	case "sh", "bash", "zsh", "fish", "ps1", "bat", "cmd":
		return "bash"
	case "js", "mjs", "cjs":
		return "javascript"
	case "ts", "tsx":
		return "typescript"
	case "md", "markdown":
		return "markdown"
	case "yaml", "yml":
		return "yaml"
	case "txt":
		return "text"
	case "svg":
		return "svg"
	case "xml":
		return "xml"
	case "pdf":
		return "pdf"
	}
	if isBinary {
		return "binary"
	}
	return "other"
}

func ruleAppliesToType(rule staticFileRule, typ string) bool {
	for _, allowed := range rule.FileTypes {
		if allowed == typ || allowed == "other" && typ == "other" {
			return true
		}
		if allowed == "text" && (typ == "markdown" || typ == "yaml" || typ == "other") {
			return true
		}
	}
	return false
}

func firstUnexcludedMatchLocation(content string, pattern *regexp.Regexp, excludes []*regexp.Regexp, relPath string) string {
	for _, loc := range pattern.FindAllStringIndex(content, -1) {
		if loc == nil {
			continue
		}
		if matchExcluded(content, loc, excludes) {
			continue
		}
		line := 1 + strings.Count(content[:loc[0]], "\n")
		return fmt.Sprintf("%s:%d", relPath, line)
	}
	return ""
}

func matchExcluded(content string, loc []int, excludes []*regexp.Regexp) bool {
	if len(excludes) == 0 || len(loc) != 2 {
		return false
	}
	start := strings.LastIndex(content[:loc[0]], "\n") + 1
	end := len(content)
	if idx := strings.Index(content[loc[1]:], "\n"); idx >= 0 {
		end = loc[1] + idx
	}
	line := content[start:end]
	match := content[loc[0]:loc[1]]
	for _, exclude := range excludes {
		if exclude.MatchString(match) || exclude.MatchString(line) {
			return true
		}
	}
	return false
}

func severityToRiskLevel(severity string) security.RiskLevel {
	switch strings.ToLower(strings.TrimSpace(severity)) {
	case "critical":
		return security.RiskCritical
	case "high":
		return security.RiskHigh
	case "medium":
		return security.RiskMedium
	default:
		return security.RiskLow
	}
}

func highestFindingLevel(findings []ScanFinding) security.RiskLevel {
	level := security.RiskLow
	for _, finding := range findings {
		next := severityToRiskLevel(finding.Severity)
		if security.RiskLevelOrder[next] > security.RiskLevelOrder[level] {
			level = next
		}
	}
	return level
}
