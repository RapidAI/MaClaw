package security

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// ThreatPattern represents a single pattern within a threat category.
type ThreatPattern struct {
	Pattern string // regex string or substring to match
	IsRegex bool   // true = compile as regex; false = case-insensitive substring match
	// Guard is an optional regex pattern. When non-empty and the guard matches
	// the same text, this threat pattern hit is considered a false positive and
	// is suppressed. This provides context-aware filtering at the pattern level
	// rather than relying on post-hoc safe-tool category matching.
	//
	// Example: Pattern `\$\(.*\)` (command substitution) with
	// Guard `\$\((date|pwd|hostname|whoami|uname -[a-z])\)` — safe substitutions.
	Guard string
}

// ThreatCategory groups related threat patterns under a named category.
type ThreatCategory struct {
	Name     string
	Patterns []ThreatPattern
}

// threatPatternCategories defines 12 threat pattern categories for enhanced
// security scanning. Each category contains regex or substring patterns to
// match against skill step commands/params.
// Requirements: 4.1, 4.2, 4.3, 4.7
var threatPatternCategories = []ThreatCategory{
	{
		Name: "exfiltration",
		Patterns: []ThreatPattern{
			{Pattern: `curl\s+.*-[dX].*POST`, IsRegex: true},
			{Pattern: `wget\s+--post`, IsRegex: true},
			{Pattern: `nc\s+-[^l]`, IsRegex: true},                                // netcat send (not listen)
			{Pattern: `base64\s+.*\|\s*(curl|wget|nc)`, IsRegex: true},             // encode-then-send
			{Pattern: `tar\s+.*\|\s*(curl|wget|nc|ssh)`, IsRegex: true},            // archive-then-send
			{Pattern: `scp\s+.*@`, IsRegex: true},                                  // scp to remote
			{Pattern: `rsync\s+.*@`, IsRegex: true},                                // rsync to remote
			{Pattern: `\$\(cat\s+/etc/(passwd|shadow)\)`, IsRegex: true},           // read sensitive files
			{Pattern: `sendmail`, IsRegex: false},                                   // email exfiltration
			{Pattern: `dns.*exfil`, IsRegex: false},                                 // DNS exfiltration
		},
	},
	{
		Name: "injection",
		Patterns: []ThreatPattern{
			{Pattern: `;\s*(rm|wget|curl|bash|sh|python|perl|nc)`, IsRegex: true},  // command chaining
			{Pattern: `\|\s*(bash|sh|python|perl)`, IsRegex: true},                 // pipe to shell
			// Command substitution: guard allows safe builtins like $(date), $(pwd), $(basename ...)
			{Pattern: `\$\(.*\)`, IsRegex: true,
				Guard: `\$\((date|pwd|hostname|basename|dirname|echo|printf|cat\s+[^/]|wc\s|head\s|tail\s|tr\s|cut\s|sort|uniq|grep\s|awk\s|sed\s)\b`},
			{Pattern: "`[^`]*\\b(rm|wget|curl|bash|sh|python|perl|nc|cat|echo|eval|exec|whoami|id|uname)\\b[^`]*`", IsRegex: true}, // backtick command substitution (word-boundary match, not Markdown inline code)
			{Pattern: `>\s*/dev/tcp/`, IsRegex: true},                               // bash TCP redirect
			// eval/exec injection: guard allows common safe patterns like executor.execute(), exec.Command
			{Pattern: `eval\s*\(`, IsRegex: true,
				Guard: `\b(executor|evaluator|evaluate|evaluation)\b`},
			{Pattern: `exec\s*\(`, IsRegex: true,
				Guard: `\b(exec\.Command|exec\.LookPath|executor|subprocess)\b`},
			{Pattern: `os\.system\s*\(`, IsRegex: true},                            // Python os.system
			{Pattern: `subprocess\.call`, IsRegex: false},                           // Python subprocess
		},
	},
	{
		Name: "destructive",
		Patterns: []ThreatPattern{
			// rm -rf with path awareness: only critical when targeting root or system paths.
			// Guard allows user-home-directory operations (~/..., $HOME/..., /home/..., /tmp/...).
			// Note: path traversal (../../) is separately covered by the "traversal" category.
			{Pattern: `rm\s+-r[f]?\s+/`, IsRegex: true,
				Guard: `rm\s+-r[f]?\s+(/home/|/root/|/tmp/|/var/tmp/|~/|\$HOME/)`},
			{Pattern: `rm\s+-[f]?r\s+/`, IsRegex: true,
				Guard: `rm\s+-[f]?r\s+(/home/|/root/|/tmp/|/var/tmp/|~/|\$HOME/)`},
			{Pattern: `mkfs`, IsRegex: false},                                      // format filesystem
			{Pattern: `dd\s+if=.*of=/dev/`, IsRegex: true},                        // overwrite device
			{Pattern: `shred`, IsRegex: false},                                     // secure delete
			{Pattern: `wipefs`, IsRegex: false},                                    // wipe filesystem
			{Pattern: `:(){ :|:& };:`, IsRegex: false},                            // fork bomb
			{Pattern: `>\s*/dev/sda`, IsRegex: true},                               // overwrite disk
			{Pattern: `truncate\s+-s\s+0`, IsRegex: true},                         // truncate files
		},
	},
	{
		Name: "persistence",
		Patterns: []ThreatPattern{
			{Pattern: `crontab`, IsRegex: false},                                   // cron persistence
			{Pattern: `/etc/cron`, IsRegex: false},                                 // cron directory
			{Pattern: `systemctl\s+enable`, IsRegex: true},                         // systemd persistence
			// Shell profiles: guard allows read-only access (cat, grep, head) and
			// common safe operations (source, . .bashrc for env setup)
			{Pattern: `\.bashrc`, IsRegex: false,
				Guard: `(cat|grep|head|tail|less|more|source|\.\s+)\s+.*\.bashrc`},
			{Pattern: `\.bash_profile`, IsRegex: false,
				Guard: `(cat|grep|head|tail|less|more|source)\s+.*\.bash_profile`},
			{Pattern: `\.profile`, IsRegex: false,
				Guard: `(cat|grep|head|tail|less|more|source)\s+.*\.profile|\.profile\.(png|jpg|json|ts|js|go|py)`},
			{Pattern: `authorized_keys`, IsRegex: false},                           // SSH key persistence
			{Pattern: `HKLM\\.*\\Run`, IsRegex: true},                             // Windows registry run key
			{Pattern: `schtasks\s+/create`, IsRegex: true},                         // Windows scheduled task
			{Pattern: `launchctl\s+load`, IsRegex: true},                           // macOS launch agent
		},
	},
	{
		Name: "network",
		Patterns: []ThreatPattern{
			{Pattern: `nc\s+-l`, IsRegex: true},                                    // netcat listener
			{Pattern: `ncat\s+-l`, IsRegex: true},                                  // ncat listener
			{Pattern: `socat`, IsRegex: false},                                     // socat relay
			{Pattern: `ssh\s+-R`, IsRegex: true},                                   // reverse SSH tunnel
			{Pattern: `ssh\s+-L`, IsRegex: true},                                   // local SSH tunnel
			{Pattern: `iptables`, IsRegex: false},                                  // firewall manipulation
			{Pattern: `nmap`, IsRegex: false},                                      // network scanning
			{Pattern: `tcpdump`, IsRegex: false},                                   // packet capture
			{Pattern: `tshark`, IsRegex: false},                                    // packet capture
			{Pattern: `/dev/tcp/`, IsRegex: false},                                 // bash TCP device
		},
	},
	{
		Name: "obfuscation",
		Patterns: []ThreatPattern{
			// base64 decode: guard allows safe data processing (images, JSON, certificates)
			{Pattern: `base64\s+-d`, IsRegex: true,
				Guard: `base64\s+-d\s*$|base64\s+-d\s*>\s*\S+\.(png|jpg|jpeg|gif|svg|pdf|json|pem|crt|cer)`},
			{Pattern: `base64\s+--decode`, IsRegex: true,
				Guard: `base64\s+--decode\s*$|base64\s+--decode\s*>\s*\S+\.(png|jpg|jpeg|gif|svg|pdf|json|pem|crt|cer)`},
			{Pattern: `echo\s+.*\|\s*base64\s+-d\s*\|\s*(bash|sh)`, IsRegex: true}, // decode-then-exec (no guard — always dangerous)
			{Pattern: `python.*-c.*exec\(`, IsRegex: true},                         // Python one-liner exec
			{Pattern: `perl\s+-e`, IsRegex: true},                                  // Perl one-liner
			{Pattern: `\\x[0-9a-fA-F]{2}`, IsRegex: true,
				Guard: `\\x[0-9a-fA-F]{2}.*\.(log|txt|csv|json)`},                  // hex-encoded: guard for log/data files
			{Pattern: `xxd\s+-r`, IsRegex: true},                                   // hex decode
			{Pattern: `openssl\s+enc\s+-d`, IsRegex: true},                         // openssl decrypt
		},
	},
	{
		Name: "execution",
		Patterns: []ThreatPattern{
			// chmod +x: guard allows setting executable on project scripts and named binaries.
			// Broad guard: if the target is a relative path (./) or ends in a known script
			// extension or any single filename, it's likely user-initiated.
			{Pattern: `chmod\s+\+x`, IsRegex: true,
				Guard: `chmod\s+\+x\s+(\./|\S+\.(sh|py|rb|pl|bash)\b)`},
			{Pattern: `chmod\s+[0-7]*7[0-7]*\s`, IsRegex: true},                   // world-executable
			{Pattern: `curl.*\|\s*(bash|sh)`, IsRegex: true},                       // download-and-exec
			{Pattern: `wget.*\|\s*(bash|sh)`, IsRegex: true},                       // download-and-exec
			{Pattern: `curl.*-o\s+/tmp/.*&&.*sh\s+/tmp/`, IsRegex: true},          // download-save-exec
			// python -c: guard allows safe one-liners (print, import, assignments, simple expressions)
			{Pattern: `python\s+-c`, IsRegex: true,
				Guard: `python3?\s+-c\s+['"]?(import\s+|from\s+\w+\s+import|print\s*\(|[a-zA-Z_]\w*\s*=)`},
			{Pattern: `ruby\s+-e`, IsRegex: true},                                  // Ruby one-liner
			{Pattern: `node\s+-e`, IsRegex: true},                                  // Node one-liner
		},
	},
	{
		Name: "traversal",
		Patterns: []ThreatPattern{
			{Pattern: `\.\./\.\./`, IsRegex: true},                                 // double path traversal
			{Pattern: `\.\.\\\.\.\\`, IsRegex: true},                               // Windows double path traversal
			{Pattern: `/\.\./`, IsRegex: true,
				Guard: `node_modules/\.\./|\.\./(src|lib|dist|build|test|tests)/`},  // single traversal (guard: common relative imports)
			{Pattern: `/etc/passwd`, IsRegex: false},                               // sensitive file access
			{Pattern: `/etc/shadow`, IsRegex: false},                               // sensitive file access
			{Pattern: `/proc/self/`, IsRegex: false},                               // proc filesystem
			{Pattern: `%2e%2e%2f`, IsRegex: false},                                 // URL-encoded traversal
			{Pattern: `%2e%2e/`, IsRegex: false},                                   // URL-encoded traversal
			{Pattern: `symlink\s+.*\.\./`, IsRegex: true},                          // symlink traversal
		},
	},
	{
		Name: "mining",
		Patterns: []ThreatPattern{
			{Pattern: `xmrig`, IsRegex: false},                                     // Monero miner
			{Pattern: `minerd`, IsRegex: false},                                    // CPU miner
			{Pattern: `cgminer`, IsRegex: false},                                   // GPU miner
			{Pattern: `bfgminer`, IsRegex: false},                                  // FPGA miner
			{Pattern: `stratum\+tcp://`, IsRegex: false},                           // mining pool protocol
			{Pattern: `cryptonight`, IsRegex: false},                               // mining algorithm
			{Pattern: `hashrate`, IsRegex: false},                                  // mining indicator
			{Pattern: `coinhive`, IsRegex: false},                                  // browser miner
		},
	},
	{
		Name: "supply_chain",
		Patterns: []ThreatPattern{
			{Pattern: `pip\s+install\s+--index-url`, IsRegex: true},                // custom PyPI index
			{Pattern: `npm\s+install\s+--registry`, IsRegex: true},                 // custom npm registry
			{Pattern: `curl.*\|\s*pip\s+install`, IsRegex: true},                   // pipe-to-pip
			{Pattern: `setup\.py\s+install`, IsRegex: true},                        // direct setup.py
			{Pattern: `pip\s+install\s+--pre`, IsRegex: true},                      // pre-release packages
			// npm install @latest: guard allows well-known scoped packages
			{Pattern: `npm\s+install\s+.*@latest`, IsRegex: true,
				Guard: `npm\s+install\s+(@types/|@babel/|@vue/|@angular/|@react-)`},
			// go install: guard allows well-known Go module hosts
			{Pattern: `go\s+install\s+.*@`, IsRegex: true,
				Guard: `go\s+install\s+(golang\.org/|github\.com/|google\.golang\.org/)`},
		},
	},
	{
		Name: "privilege_escalation",
		Patterns: []ThreatPattern{
			{Pattern: `sudo\s+su`, IsRegex: true},                                  // switch to root
			{Pattern: `sudo\s+-i`, IsRegex: true},                                  // root login shell
			{Pattern: `chmod\s+u\+s`, IsRegex: true},                               // setuid bit
			{Pattern: `chmod\s+[0-7]*4[0-7]*\s`, IsRegex: true},                   // setuid via octal
			{Pattern: `chown\s+root`, IsRegex: true},                               // change owner to root
			{Pattern: `visudo`, IsRegex: false},                                    // sudoers edit
			{Pattern: `/etc/sudoers`, IsRegex: false},                              // sudoers file
			{Pattern: `doas`, IsRegex: false},                                      // OpenBSD privilege escalation
			{Pattern: `pkexec`, IsRegex: false},                                    // polkit escalation
			{Pattern: `setcap`, IsRegex: false},                                    // Linux capabilities
		},
	},
	{
		Name: "credential_exposure",
		Patterns: []ThreatPattern{
			{Pattern: `/etc/shadow`, IsRegex: false},                               // shadow passwords
			{Pattern: `\.ssh/id_rsa`, IsRegex: false},                              // SSH private key
			{Pattern: `\.ssh/id_ed25519`, IsRegex: false},                          // SSH private key
			{Pattern: `\.aws/credentials`, IsRegex: false},                         // AWS credentials
			// .env: guard allows .env.example, .env.template, .env.local, .env.development,
			// python venv (.env as directory), and read-only access
			{Pattern: `\.env\b`, IsRegex: true,
				Guard: `\.env\.(example|template|sample|local|development|production|test|bak)|python.*\.env|venv.*\.env|(cat|grep|head|less)\s+.*\.env`},
			{Pattern: `\.netrc`, IsRegex: false},                                   // netrc credentials
			{Pattern: `\.pgpass`, IsRegex: false},                                  // PostgreSQL password
			{Pattern: `PRIVATE KEY`, IsRegex: false},                               // private key content
			// Hardcoded password: guard allows common safe patterns like password validation,
			// password hashing, password_field, password_input
			{Pattern: `password\s*=\s*['"]`, IsRegex: true,
				Guard: `password\s*=\s*['"]\s*['"]|password\s*=\s*['"](\*+|x+|placeholder|changeme|your[_-]password)['"]`},
			{Pattern: `api[_-]?key\s*=\s*['"]`, IsRegex: true,
				Guard: `api[_-]?key\s*=\s*['"]\s*['"]|api[_-]?key\s*=\s*['"](\*+|x+|placeholder|your[_-]api[_-]key)['"]`},
			{Pattern: `secret[_-]?key\s*=\s*['"]`, IsRegex: true,
				Guard: `secret[_-]?key\s*=\s*['"]\s*['"]|secret[_-]?key\s*=\s*['"](\*+|x+|placeholder|your[_-]secret)['"]`},
		},
	},
}

// promptInjectionPatterns detects prompt injection attempts in skill content.
// Requirements: 4.3, 4.7
var promptInjectionPatterns = []ThreatPattern{
	// Instruction override attempts
	{Pattern: `ignore\s+(all\s+)?(previous|prior|above)\s+(instructions|rules|prompts)`, IsRegex: true},
	{Pattern: `disregard\s+(all\s+)?(previous|prior|above)\s+(instructions|rules)`, IsRegex: true},
	{Pattern: `forget\s+(all\s+)?(previous|prior|above)\s+(instructions|rules)`, IsRegex: true},
	{Pattern: `override\s+(system|safety|security)\s+(prompt|instructions|rules)`, IsRegex: true},
	{Pattern: `new\s+instructions?\s*:`, IsRegex: true},
	{Pattern: `system\s+prompt\s*:`, IsRegex: true},
	// Role-play injection
	{Pattern: `you\s+are\s+now\s+(a|an)\s+`, IsRegex: true},
	{Pattern: `act\s+as\s+(a|an)\s+`, IsRegex: true},
	{Pattern: `pretend\s+(you\s+are|to\s+be)\s+`, IsRegex: true},
	{Pattern: `from\s+now\s+on\s+you\s+(are|will)`, IsRegex: true},
	{Pattern: `switch\s+to\s+.*mode`, IsRegex: true},
	{Pattern: `enter\s+.*mode`, IsRegex: true},
	// Delimiter injection
	{Pattern: `<\|im_start\|>`, IsRegex: false},
	{Pattern: `<\|im_end\|>`, IsRegex: false},
	{Pattern: `\[INST\]`, IsRegex: false},
	{Pattern: `\[/INST\]`, IsRegex: false},
}

// compiledThreatPatterns caches compiled regex patterns for threat categories.
var compiledThreatPatterns map[string][]*compiledPattern

// compiledPromptInjection caches compiled regex patterns for prompt injection.
var compiledPromptInjection []*compiledPattern

type compiledPattern struct {
	Original ThreatPattern
	Regex    *regexp.Regexp // non-nil only for IsRegex patterns
	GuardRe  *regexp.Regexp // non-nil when Original.Guard is set
}

func init() {
	compiledThreatPatterns = make(map[string][]*compiledPattern)
	for _, cat := range threatPatternCategories {
		var compiled []*compiledPattern
		for _, p := range cat.Patterns {
			cp := &compiledPattern{Original: p}
			if p.IsRegex {
				cp.Regex = regexp.MustCompile("(?i)" + p.Pattern)
			}
			if p.Guard != "" {
				cp.GuardRe = regexp.MustCompile("(?i)" + p.Guard)
			}
			compiled = append(compiled, cp)
		}
		compiledThreatPatterns[cat.Name] = compiled
	}
	for _, p := range promptInjectionPatterns {
		cp := &compiledPattern{Original: p}
		if p.IsRegex {
			cp.Regex = regexp.MustCompile("(?i)" + p.Pattern)
		}
		if p.Guard != "" {
			cp.GuardRe = regexp.MustCompile("(?i)" + p.Guard)
		}
		compiledPromptInjection = append(compiledPromptInjection, cp)
	}
}

// matchPattern checks if text matches a compiled pattern.
// If the pattern has a Guard and the guard also matches, the hit is considered
// a false positive and matchPattern returns false.
func matchPattern(cp *compiledPattern, text string) bool {
	matched := false
	if cp.Regex != nil {
		matched = cp.Regex.MatchString(text)
	} else {
		matched = containsIgnoreCase(text, cp.Original.Pattern)
	}
	if !matched {
		return false
	}
	// Guard check: if the guard regex matches, this is a known safe context.
	if cp.GuardRe != nil && cp.GuardRe.MatchString(text) {
		return false
	}
	return true
}

// ThreatMatch records a matched threat pattern for risk assessment factors.
type ThreatMatch struct {
	Category string
	Pattern  string
}

// RiskAssessor performs intent-level risk assessment on tool invocations.
type RiskAssessor struct{}

// dangerousKeywords are parameter substrings that immediately trigger critical risk.
// NOTE: "format" was removed because it causes false positives on legitimate
// skills that use "format" in non-destructive contexts (e.g. PDF format
// conversion, string formatting). Use dangerousPatterns for context-aware checks.
//
// NOTE: "sudo" was moved to dangerousCmdPatterns for context-aware matching.
// As a plain substring, "sudo" matches "pseudo", "sudoku", and documentation
// text like "Run without sudo". The regex version uses word boundaries.
//
// NOTE: "rm -rf" was moved to threatPatternCategories.destructive with a
// path-aware Guard. Plain "rm -rf" matches user-directory cleanup (e.g.
// "rm -rf ~/old_build/") which is routine. Only root/system-path targets
// should escalate to critical.
var dangerousKeywords = []string{"DROP TABLE"}

// dangerousCmdPatterns are regex patterns for dangerous commands that need
// word-boundary matching to avoid false positives on substrings.
var dangerousCmdPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\bsudo\b`),
}

// dangerousCmdSafeContexts are regex patterns that, when matched alongside a
// dangerousCmdPattern hit, indicate the command is in a safe context and should
// not be escalated to critical. Each entry corresponds to a dangerousCmdPattern
// by index.
var dangerousCmdSafeContexts = [][]*regexp.Regexp{
	// Safe contexts for \bsudo\b:
	// Common system administration commands that are routine operations.
	// These are downgraded to high (not critical) — still recorded in audit
	// but won't block the user with a confirmation dialog in standard mode
	// unless they are targeting system directories.
	{
		regexp.MustCompile(`(?i)\bsudo\s+-n\b`),                                                     // non-interactive
		regexp.MustCompile(`(?i)\bsudo\s+(apt|apt-get|yum|dnf|pacman|brew)\s+(install|update|upgrade|remove)\b`), // package management
		regexp.MustCompile(`(?i)\bsudo\s+pip3?\s+install\b`),                                        // Python package
		regexp.MustCompile(`(?i)\bsudo\s+npm\s+install\s+-g\b`),                                     // Node.js global
		regexp.MustCompile(`(?i)\bsudo\s+systemctl\s+(start|stop|restart|status|reload|enable|disable)\b`), // service management
		regexp.MustCompile(`(?i)\bsudo\s+docker\b`),                                                 // Docker
		regexp.MustCompile(`(?i)\bsudo\s+chown\b`),                                                  // change ownership
		regexp.MustCompile(`(?i)\bsudo\s+chmod\s+[0-7]{3,4}\b`),                                    // permission change (numeric)
		regexp.MustCompile(`(?i)\bsudo\s+mkdir\b`),                                                  // create directory
		regexp.MustCompile(`(?i)\bsudo\s+cp\b`),                                                     // copy
		regexp.MustCompile(`(?i)\bsudo\s+mv\b`),                                                     // move/rename
		regexp.MustCompile(`(?i)\bsudo\s+ln\b`),                                                     // create link
		regexp.MustCompile(`(?i)\bsudo\s+tar\b`),                                                    // archive operations
		regexp.MustCompile(`(?i)\bsudo\s+cat\b`),                                                    // read file
		regexp.MustCompile(`(?i)\bsudo\s+tee\b`),                                                    // write to file via pipe
		regexp.MustCompile(`(?i)\bsudo\s+kill\b`),                                                   // terminate process
		regexp.MustCompile(`(?i)\bsudo\s+killall\b`),                                                // terminate by name
		regexp.MustCompile(`(?i)\bsudo\s+journalctl\b`),                                             // view logs
		regexp.MustCompile(`(?i)\bsudo\s+service\s+\w+\s+(start|stop|restart|status)\b`),           // legacy service management
		regexp.MustCompile(`(?i)\bsudo\s+snap\s+(install|remove|refresh)\b`),                        // snap packages
		regexp.MustCompile(`(?i)\bsudo\s+dpkg\b`),                                                   // Debian package
		regexp.MustCompile(`(?i)\bsudo\s+rpm\b`),                                                    // RPM package
		regexp.MustCompile(`(?i)\bsudo\s+ufw\s+(allow|deny|enable|status)\b`),                      // firewall management
		regexp.MustCompile(`(?i)\bsudo\s+certbot\b`),                                                // SSL certificate
	},
}

// dangerousFormatPatterns are patterns where "format" IS dangerous (disk formatting).
var dangerousFormatPatterns = []string{"format c:", "format d:", "format e:", "format f:", "diskpart", "mkfs"}

// safeToolCategories are skill action/tool names that are inherently safe
// utility operations and should not be escalated to critical risk.
// Includes: document conversion, data visualization, data fetching/reading,
// password generation, and other read-only or utility operations.
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

	// Context-aware dangerous command patterns (word-boundary matching).
	// Delegates to CheckDangerousCmdPatterns to keep the pattern list and safe-context
	// guards as a single source of truth rather than duplicating the logic here.
	for _, r := range CheckDangerousCmdPatterns(argStr) {
		if !r.SafeContext {
			level = RiskCritical
			factors = append(factors, fmt.Sprintf("dangerous command pattern %q matched in arguments", r.Pattern))
		} else {
			if RiskLevelOrder[level] < RiskLevelOrder[RiskHigh] {
				level = RiskHigh
			}
			factors = append(factors, fmt.Sprintf("dangerous command pattern %q matched but in safe context", r.Pattern))
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

	// Scan for structural threat patterns (destructive, traversal, exfiltration, etc.).
	// This runs on live tool invocations (not just skill installation) so that
	// commands like "rm -rf /" are caught at execution time with path-aware Guards.
	// Pre-compiled regexes make this fast enough for per-call use (<1ms typical).
	//
	// IMPORTANT: only "live-safe" categories are scanned here. Categories like
	// injection (pipe-to-shell), exfiltration (scp/rsync), network (ssh tunnels),
	// and supply_chain (pip --pre) are intentionally excluded — they produce too many
	// false positives when the user explicitly asks the agent to run those commands.
	// Those categories remain active in AssessSkill (skill installation scanning).
	for _, tm := range ScanLiveThreatPatterns(argStr) {
		if RiskLevelOrder[level] < RiskLevelOrder[RiskHigh] {
			level = RiskHigh
		}
		factors = append(factors, fmt.Sprintf("threat pattern [%s]: %q matched", tm.Category, tm.Pattern))
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
	Name     string // skill name for safe-tool category matching
	SkillDir string // skill directory path for structural checks
	Steps    []struct {
		Action string
		Params map[string]interface{}
	}
}

// invisibleUnicodeChars lists zero-width and invisible Unicode characters that
// may be used to hide malicious content in skill definitions.
// Requirements: 4.4
var invisibleUnicodeChars = map[rune]string{
	'\u200B': "zero-width space (U+200B)",
	'\u200C': "zero-width non-joiner (U+200C)",
	'\u200D': "zero-width joiner (U+200D)",
	'\uFEFF': "byte order mark / zero-width no-break space (U+FEFF)",
}

// rtlOverrideChars lists right-to-left and left-to-right override characters
// that can be used to disguise text direction in skill content.
// Requirements: 4.4
var rtlOverrideChars = map[rune]string{
	'\u202A': "left-to-right embedding (U+202A)",
	'\u202B': "right-to-left embedding (U+202B)",
	'\u202D': "left-to-right override (U+202D)",
	'\u202E': "right-to-left override (U+202E)",
}

// cyrillicHomoglyphs maps Cyrillic characters that visually resemble Latin
// characters. These can be used for homoglyph attacks where Cyrillic lookalikes
// are substituted for Latin characters to disguise malicious content.
// Requirements: 4.4
var cyrillicHomoglyphs = map[rune]string{
	'\u0430': "Cyrillic 'а' (U+0430) looks like Latin 'a'",
	'\u0435': "Cyrillic 'е' (U+0435) looks like Latin 'e'",
	'\u043E': "Cyrillic 'о' (U+043E) looks like Latin 'o'",
	'\u0440': "Cyrillic 'р' (U+0440) looks like Latin 'p'",
	'\u0441': "Cyrillic 'с' (U+0441) looks like Latin 'c'",
	'\u0445': "Cyrillic 'х' (U+0445) looks like Latin 'x'",
	'\u0456': "Cyrillic 'і' (U+0456) looks like Latin 'i'",
	'\u0455': "Cyrillic 'ѕ' (U+0455) looks like Latin 's'",
	'\u0443': "Cyrillic 'у' (U+0443) looks like Latin 'y'",
	'\u0412': "Cyrillic 'В' (U+0412) looks like Latin 'B'",
	'\u041D': "Cyrillic 'Н' (U+041D) looks like Latin 'H'",
	'\u041C': "Cyrillic 'М' (U+041C) looks like Latin 'M'",
	'\u0422': "Cyrillic 'Т' (U+0422) looks like Latin 'T'",
}

// ScanUnicodeAnomalies scans text for invisible Unicode characters, RTL overrides,
// and homoglyph substitutions that may indicate obfuscation or attack attempts.
// Returns ThreatMatch entries with category "unicode_anomaly".
// Requirements: 4.4
func ScanUnicodeAnomalies(text string) []ThreatMatch {
	if text == "" {
		return nil
	}
	var matches []ThreatMatch
	seen := make(map[string]bool) // deduplicate by description

	for _, r := range text {
		// Check invisible/zero-width characters
		if desc, ok := invisibleUnicodeChars[r]; ok {
			if !seen[desc] {
				seen[desc] = true
				matches = append(matches, ThreatMatch{
					Category: "unicode_anomaly",
					Pattern:  desc,
				})
			}
		}
		// Check RTL/LTR override characters
		if desc, ok := rtlOverrideChars[r]; ok {
			if !seen[desc] {
				seen[desc] = true
				matches = append(matches, ThreatMatch{
					Category: "unicode_anomaly",
					Pattern:  desc,
				})
			}
		}
		// Check Cyrillic homoglyphs
		if desc, ok := cyrillicHomoglyphs[r]; ok {
			if !seen[desc] {
				seen[desc] = true
				matches = append(matches, ThreatMatch{
					Category: "unicode_anomaly",
					Pattern:  desc,
				})
			}
		}
	}
	return matches
}

// ScanThreatPatterns scans text against all 12 threat pattern categories and
// returns any matches found.
//
// NOTE: In production code, callers should prefer the more targeted functions:
//   - ScanLiveThreatPatterns: for live tool invocations (Assess)
//   - ScanSkillOnlyThreatPatterns: for AssessSkill (complements Assess)
//
// ScanThreatPatterns is retained for tests and external callers that need to
// verify a specific pattern against the full 12-category set.
// Requirements: 4.1, 4.2
func ScanThreatPatterns(text string) []ThreatMatch {
	if text == "" {
		return nil
	}
	var matches []ThreatMatch
	for catName, patterns := range compiledThreatPatterns {
		for _, cp := range patterns {
			if matchPattern(cp, text) {
				matches = append(matches, ThreatMatch{
					Category: catName,
					Pattern:  cp.Original.Pattern,
				})
			}
		}
	}
	return matches
}

// liveThreatCategories is the subset of threat categories scanned during live
// tool invocations (Assess). Categories excluded from this list produce too
// many false positives when the user explicitly instructs the agent to run
// commands like scp, ssh -L, pip install --pre, etc.
//
// Excluded categories and rationale:
//   - exfiltration: user asks "deploy to server" → scp/rsync/curl POST are expected
//   - injection: user asks "run my script" → pipe-to-python, command chaining normal
//   - network: user asks "set up tunnel" → ssh -R/-L are expected
//   - supply_chain: user asks "install beta" → pip --pre is expected
//   - persistence: user asks "set up crontab" → crontab is expected
//   - credential_exposure: user asks "read .env" → agent reads .env
//   - privilege_escalation: sudo is already handled by dangerousCmdPatterns with
//     context-aware safe-context guards (earlier in Assess). Including it here
//     would only duplicate factors. Non-sudo escalation (setuid, visudo) is rare
//     enough in user-initiated commands and handled by AssessSkill.
//
// Included (always dangerous regardless of user intent):
//   - destructive: rm -rf /, mkfs, dd of=/dev/, fork bomb
//   - traversal: path traversal to sensitive system files
//   - obfuscation: base64-decode-then-exec, hex inject
//   - mining: crypto miners
//   - execution: curl|bash, download-and-exec (chmod +x and python -c have Guards)
var liveThreatCategories = map[string]bool{
	"destructive": true,
	"traversal":   true,
	"obfuscation": true,
	"mining":      true,
	"execution":   true,
}

// ScanLiveThreatPatterns scans text against the subset of threat categories
// appropriate for live tool invocations. Categories that produce false positives
// when the user explicitly requests an operation are excluded.
// See liveThreatCategories for the included/excluded rationale.
func ScanLiveThreatPatterns(text string) []ThreatMatch {
	if text == "" {
		return nil
	}
	var matches []ThreatMatch
	for catName, patterns := range compiledThreatPatterns {
		if !liveThreatCategories[catName] {
			continue
		}
		for _, cp := range patterns {
			if matchPattern(cp, text) {
				matches = append(matches, ThreatMatch{
					Category: catName,
					Pattern:  cp.Original.Pattern,
				})
			}
		}
	}
	return matches
}

// ScanSkillOnlyThreatPatterns scans text against the threat categories that are
// NOT covered by ScanLiveThreatPatterns. Used in AssessSkill to complement
// the Assess() call (which already runs ScanLiveThreatPatterns internally),
// so together they cover all 12 categories without duplication.
func ScanSkillOnlyThreatPatterns(text string) []ThreatMatch {
	if text == "" {
		return nil
	}
	var matches []ThreatMatch
	for catName, patterns := range compiledThreatPatterns {
		if liveThreatCategories[catName] {
			// Already scanned by Assess() via ScanLiveThreatPatterns — skip.
			continue
		}
		for _, cp := range patterns {
			if matchPattern(cp, text) {
				matches = append(matches, ThreatMatch{
					Category: catName,
					Pattern:  cp.Original.Pattern,
				})
			}
		}
	}
	return matches
}

// ScanPromptInjection scans text for prompt injection patterns including
// instruction override attempts and role-play injection.
// Requirements: 4.3, 4.7
func ScanPromptInjection(text string) []ThreatMatch {
	if text == "" {
		return nil
	}
	var matches []ThreatMatch
	for _, cp := range compiledPromptInjection {
		if matchPattern(cp, text) {
			matches = append(matches, ThreatMatch{
				Category: "prompt_injection",
				Pattern:  cp.Original.Pattern,
			})
		}
	}
	return matches
}

// maxFileCount is the threshold for flagging directories with too many files.
const maxFileCount = 50

// maxTotalSize is the threshold (10 MB) for flagging oversized skill directories.
const maxTotalSize int64 = 10 * 1024 * 1024

// binaryCheckSize is the number of bytes to read from a file to detect binary content.
const binaryCheckSize = 512

// ScanDirectoryStructure walks a skill directory and checks for structural
// anomalies: too many files, total size exceeding 10MB, binary files, and
// symlinks pointing outside the skill directory.
// Returns ThreatMatch entries with category "structural_anomaly".
// Requirements: 4.5, 4.6
func ScanDirectoryStructure(skillDir string) []ThreatMatch {
	if skillDir == "" {
		return nil
	}

	// Resolve the skill directory to an absolute path for symlink comparison.
	absSkillDir, err := filepath.Abs(skillDir)
	if err != nil {
		return nil
	}
	// Normalize to clean path with forward slashes for consistent comparison.
	absSkillDir = filepath.Clean(absSkillDir)

	var matches []ThreatMatch
	var fileCount int
	var totalSize int64

	_ = filepath.Walk(absSkillDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip inaccessible entries
		}

		// Use Lstat to detect symlinks (Walk follows symlinks by default,
		// but we need the original FileInfo to check the mode).
		linfo, lerr := os.Lstat(path)
		if lerr != nil {
			return nil
		}

		// Check for symlinks pointing outside the skill directory.
		if linfo.Mode()&os.ModeSymlink != 0 {
			resolved, evalErr := filepath.EvalSymlinks(path)
			if evalErr == nil {
				resolved = filepath.Clean(resolved)
				if !isInsideDir(resolved, absSkillDir) {
					matches = append(matches, ThreatMatch{
						Category: "structural_anomaly",
						Pattern:  fmt.Sprintf("symlink %q points outside skill directory to %q", filepath.Base(path), resolved),
					})
				}
			}
		}

		// Only count regular files for file count and size checks.
		if !info.IsDir() {
			fileCount++
			totalSize += info.Size()

			// Check for binary content (null bytes in first 512 bytes).
			if IsBinaryFile(path) {
				relPath, _ := filepath.Rel(absSkillDir, path)
				if relPath == "" {
					relPath = filepath.Base(path)
				}
				matches = append(matches, ThreatMatch{
					Category: "structural_anomaly",
					Pattern:  fmt.Sprintf("binary file detected: %s", relPath),
				})
			}
		}

		return nil
	})

	if fileCount > maxFileCount {
		matches = append(matches, ThreatMatch{
			Category: "structural_anomaly",
			Pattern:  fmt.Sprintf("directory contains %d files (threshold: %d)", fileCount, maxFileCount),
		})
	}

	if totalSize > maxTotalSize {
		sizeMB := float64(totalSize) / (1024 * 1024)
		matches = append(matches, ThreatMatch{
			Category: "structural_anomaly",
			Pattern:  fmt.Sprintf("total directory size %.1f MB exceeds 10 MB threshold", sizeMB),
		})
	}

	return matches
}

// IsBinaryFile checks if a file contains null bytes in the first 512 bytes,
// which is a common heuristic for detecting binary (non-text) content.
func IsBinaryFile(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()

	buf := make([]byte, binaryCheckSize)
	n, err := io.ReadAtLeast(f, buf, 1)
	if err != nil {
		return false
	}

	for i := 0; i < n; i++ {
		if buf[i] == 0 {
			return true
		}
	}
	return false
}

// isInsideDir checks if the given path is inside (or equal to) the directory.
func isInsideDir(path, dir string) bool {
	// Use filepath.Rel to determine the relationship.
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return false
	}
	// If the relative path starts with "..", it's outside the directory.
	return !strings.HasPrefix(rel, "..")
}

// Trust level constants for the 4-tier trust hierarchy.
// Requirements: 4.8, 4.9, 4.10
const (
	TrustLevelBuiltin      = "builtin"
	TrustLevelTrusted      = "trusted"
	TrustLevelAgentCreated = "agent-created"
	TrustLevelCommunity    = "community"
)

// NormalizeTrustLevel maps legacy trust level values to the 4-tier hierarchy.
// "official" → "trusted", "unknown" → "community".
// Unrecognized values are returned as-is (treated as agent-created / standard).
func NormalizeTrustLevel(trustLevel string) string {
	switch trustLevel {
	case "official":
		return TrustLevelTrusted
	case "unknown":
		return TrustLevelCommunity
	default:
		return trustLevel
	}
}

// AssessSkill evaluates the risk level of an entire skill.
// Safe-tool category: skills whose name matches a safe category (pdf, qr,
// pptx, etc.) are downgraded from critical/high to medium at most.
// Enhanced with 12 threat pattern categories and prompt injection detection.
// Trust level hierarchy: builtin > trusted > agent-created > community.
// Requirements: 4.1, 4.2, 4.3, 4.7, 4.8, 4.9, 4.10
func (a *RiskAssessor) AssessSkill(skill SkillRiskInput, trustLevel string) RiskAssessment {
	maxRisk := RiskLow
	var factors []string
	hasHardSecuritySignal := false // tracks prompt injection, threat patterns, structural/unicode anomalies, dangerous keywords

	for _, step := range skill.Steps {
		stepAssessment := a.Assess(RiskContext{
			ToolName:  step.Action,
			Arguments: step.Params,
		})
		if RiskLevelOrder[stepAssessment.Level] > RiskLevelOrder[maxRisk] {
			maxRisk = stepAssessment.Level
			factors = append(factors, stepAssessment.Factors...)
		}
		// Assess() sets critical for dangerous keywords/commands/format patterns.
		if stepAssessment.Level == RiskCritical {
			hasHardSecuritySignal = true
		}

		// Scan step commands/params against threat pattern categories not already
		// covered by Assess() above. Assess() internally runs ScanLiveThreatPatterns
		// (destructive, traversal, obfuscation, mining, execution); here we scan
		// the remaining categories (exfiltration, injection, network, supply_chain,
		// persistence, credential_exposure, privilege_escalation).
		argStr := flattenArgs(step.Params)
		threatMatches := ScanSkillOnlyThreatPatterns(argStr)
		for _, tm := range threatMatches {
			if RiskLevelOrder[maxRisk] < RiskLevelOrder[RiskHigh] {
				maxRisk = RiskHigh
			}
			hasHardSecuritySignal = true
			factors = append(factors, fmt.Sprintf("threat pattern [%s]: %q matched", tm.Category, tm.Pattern))
		}

		// Scan for prompt injection patterns
		injectionMatches := ScanPromptInjection(argStr)
		for _, tm := range injectionMatches {
			if RiskLevelOrder[maxRisk] < RiskLevelOrder[RiskCritical] {
				maxRisk = RiskCritical
			}
			hasHardSecuritySignal = true
			factors = append(factors, fmt.Sprintf("prompt injection detected: %q matched", tm.Pattern))
		}

		// Scan for invisible Unicode characters, RTL overrides, and homoglyphs
		// Requirements: 4.4
		unicodeMatches := ScanUnicodeAnomalies(argStr)
		if len(unicodeMatches) > 0 {
			maxRisk = EscalateRiskLevel(maxRisk)
			hasHardSecuritySignal = true
			for _, tm := range unicodeMatches {
				factors = append(factors, fmt.Sprintf("unicode anomaly: %s", tm.Pattern))
			}
		}
	}

	// Structural checks on skill directory
	// Requirements: 4.5, 4.6
	if skill.SkillDir != "" {
		structuralMatches := ScanDirectoryStructure(skill.SkillDir)
		if len(structuralMatches) > 0 {
			maxRisk = EscalateRiskLevel(maxRisk)
			hasHardSecuritySignal = true
			for _, tm := range structuralMatches {
				factors = append(factors, fmt.Sprintf("structural anomaly: %s", tm.Pattern))
			}
		}
	}

	// 4-tier trust level hierarchy: builtin > trusted > agent-created > community
	// Normalize legacy values: "official" → "trusted", "unknown" → "community"
	normalized := NormalizeTrustLevel(trustLevel)
	switch normalized {
	case TrustLevelBuiltin:
		// Cap maximum risk at low regardless of pattern matches
		if RiskLevelOrder[maxRisk] > RiskLevelOrder[RiskLow] {
			factors = append(factors, fmt.Sprintf("builtin trust level: %s capped to low", maxRisk))
			maxRisk = RiskLow
		}
	case TrustLevelTrusted:
		// Cap maximum risk at medium
		if RiskLevelOrder[maxRisk] > RiskLevelOrder[RiskMedium] {
			factors = append(factors, fmt.Sprintf("trusted trust level: %s capped to medium", maxRisk))
			maxRisk = RiskMedium
		}
	case TrustLevelCommunity:
		// Escalate assessed risk by one step
		escalated := EscalateRiskLevel(maxRisk)
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
	if (maxRisk == RiskCritical || maxRisk == RiskHigh) && skill.Name != "" && !hasHardSecuritySignal {
		skillLower := strings.ToLower(skill.Name)
		for _, cat := range safeToolCategories {
			if strings.Contains(skillLower, cat) {
				maxRisk = RiskMedium
				factors = append(factors, fmt.Sprintf("safe-tool category %q matched: risk capped at medium", cat))
				break
			}
		}
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

// DangerousCmdResult holds the result of a dangerous command pattern check.
type DangerousCmdResult struct {
	Matched     bool   // true if a dangerous command pattern was found
	SafeContext bool   // true if the match is in a known safe context
	Pattern     string // the matched pattern string (for logging)
}

// CheckDangerousCmdPatterns checks text against dangerous command patterns
// (word-boundary aware) and their safe context guards.
// Returns all matches found. Callers decide how to use the results:
// - Matched && !SafeContext → escalate to critical
// - Matched && SafeContext → escalate to high (reduced)
// - !Matched → no action
func CheckDangerousCmdPatterns(text string) []DangerousCmdResult {
	var results []DangerousCmdResult
	for i, re := range dangerousCmdPatterns {
		if re.MatchString(text) {
			safeCtx := false
			if i < len(dangerousCmdSafeContexts) {
				for _, guard := range dangerousCmdSafeContexts[i] {
					if guard.MatchString(text) {
						safeCtx = true
						break
					}
				}
			}
			results = append(results, DangerousCmdResult{
				Matched:     true,
				SafeContext: safeCtx,
				Pattern:     re.String(),
			})
		}
	}
	return results
}
