package security

import (
	"regexp"
	"strings"
	"sync"
)

// SensitiveMatch represents a detected sensitive information match.
type SensitiveMatch struct {
	Category string // "api_key", "private_key", "password", "jwt"
	Pattern  string // name of the matched pattern
}

// sensitivePattern is an internal compiled detection pattern.
type sensitivePattern struct {
	name     string
	category string
	re       *regexp.Regexp
	// validate optionally gates a match on its capture groups (e.g. the
	// password value in a user+password pair must contain a digit or special
	// character). Nil means any match counts.
	validate func(sub []string) bool
	// redact optionally builds the replacement string from capture groups so
	// surrounding context (username, separators) survives redaction. Nil
	// replaces the whole match with [REDACTED].
	redact func(sub []string) string
}

// SensitiveDetector detects and redacts sensitive information in text.
type SensitiveDetector struct {
	patterns []sensitivePattern
}

// defaultPatterns are compiled once and shared across all detector instances.
var (
	defaultSensitivePatterns     []sensitivePattern
	defaultSensitivePatternsOnce sync.Once
)

func builtinSensitivePatterns() []sensitivePattern {
	defaultSensitivePatternsOnce.Do(func() {
		defaultSensitivePatterns = []sensitivePattern{
			// --- Well-known token shapes (high precision) ---
			{name: "sk_api_key", category: "api_key", re: regexp.MustCompile(`sk-[a-zA-Z0-9]{20,}`)},
			{name: "aws_access_key", category: "api_key", re: regexp.MustCompile(`AKIA[A-Z0-9]{16}`)},
			{name: "github_token", category: "api_key", re: regexp.MustCompile(`(?:ghp|gho|ghu|ghs|ghr)_[A-Za-z0-9]{36,}|github_pat_[A-Za-z0-9_]{22,}`)},
			{name: "google_api_key", category: "api_key", re: regexp.MustCompile(`AIza[0-9A-Za-z_-]{35}`)},
			{name: "private_key_header", category: "private_key", re: regexp.MustCompile(`-----BEGIN.*PRIVATE KEY-----`)},
			{name: "jwt_token", category: "jwt", re: regexp.MustCompile(`eyJ[a-zA-Z0-9_-]+\.eyJ[a-zA-Z0-9_-]+\.[a-zA-Z0-9_-]+`)},

			// --- Keyword assignments (English) ---
			{name: "password_assignment", category: "password", re: regexp.MustCompile(`(?i)(password|passwd|pwd)\s*[=:]\s*\S+`)},
			{name: "secret_assignment", category: "password", re: regexp.MustCompile(`(?i)\b(secret|token|api[_-]?key|apikey|access[_-]?key|auth[_-]?token|client[_-]?secret)\b\s*[=:]\s*["']?[A-Za-z0-9_\-.]{8,}`)},

			// --- Chinese credential keywords. A separator (or 是/为) is
			// required before the value so words like 密码学 or 密钥管理 do
			// not false-positive.
			{name: "chinese_password_assignment", category: "password", re: regexp.MustCompile(`(?:密码|口令|密钥|秘钥|密匙)\s*[:：=]\s*[^\s，。；;,、]+`)},
			{name: "chinese_password_is", category: "password", re: regexp.MustCompile(`(?:密码|口令|密钥|秘钥|密匙)(?:是|为|為)\s*[^\s，。；;,、]{3,}`)},
			// Whitespace-separated Chinese password ("密码 P@ssw0rd!").
			// The value must contain a digit or special character so
			// "密码 管理" style prose does not flag.
			{
				name:     "chinese_password_space",
				category: "password",
				re:       regexp.MustCompile(`(?:密码|口令)\s+([^\s，。；;,、!！?？]{4,31})(?:[^\w]|$)`),
				validate: func(sub []string) bool {
					return strings.ContainsAny(sub[1], "0123456789!@#$%^&*._+-")
				},
				redact: func(sub []string) string {
					return "密码 [REDACTED]" + sub[2]
				},
			},

			// --- user + password pair, e.g. "root sunion123". The value must
			// contain a digit or special character (validated in code — RE2 has
			// no lookahead) so plain phrases like "root directory" or
			// "admin panel" do not match.
			{
				name:     "user_password_pair",
				category: "password",
				re:       regexp.MustCompile(`(?i)(^|[^\w])(root|admin|administrator|sa|ubuntu|debian|centos|oracle|postgres|mysql|tomcat|nginx)(\s+)([A-Za-z0-9][A-Za-z0-9_!@#$%^&*.\+\-=]{5,31})([^\w]|$)`),
				validate: func(sub []string) bool {
					return strings.ContainsAny(sub[4], "0123456789!@#$%^&*._+-")
				},
				redact: func(sub []string) string {
					return sub[1] + sub[2] + sub[3] + "[REDACTED]" + sub[5]
				},
			},
		}
	})
	return defaultSensitivePatterns
}

// NewSensitiveDetector creates a SensitiveDetector with built-in detection patterns.
func NewSensitiveDetector() *SensitiveDetector {
	return &SensitiveDetector{
		patterns: builtinSensitivePatterns(),
	}
}

// Detect scans text and returns all sensitive information matches.
func (d *SensitiveDetector) Detect(text string) []SensitiveMatch {
	var matches []SensitiveMatch
	seen := make(map[string]bool)
	for _, p := range d.patterns {
		if !patternMatches(p, text) {
			continue
		}
		key := p.category + ":" + p.name
		if !seen[key] {
			seen[key] = true
			matches = append(matches, SensitiveMatch{
				Category: p.category,
				Pattern:  p.name,
			})
		}
	}
	return matches
}

func patternMatches(p sensitivePattern, text string) bool {
	if p.validate == nil {
		return p.re.MatchString(text)
	}
	for _, sub := range p.re.FindAllStringSubmatch(text, -1) {
		if p.validate(sub) {
			return true
		}
	}
	return false
}

// Redact replaces all detected sensitive patterns in text with [REDACTED].
// For user+password pair style patterns the username is preserved and only
// the password value is replaced.
func (d *SensitiveDetector) Redact(text string) string {
	for _, p := range d.patterns {
		if p.validate == nil && p.redact == nil {
			text = p.re.ReplaceAllString(text, "[REDACTED]")
			continue
		}
		text = p.re.ReplaceAllStringFunc(text, func(m string) string {
			sub := p.re.FindStringSubmatch(m)
			if len(sub) == 0 {
				return m
			}
			if p.validate != nil && !p.validate(sub) {
				return m
			}
			if p.redact != nil {
				return p.redact(sub)
			}
			return "[REDACTED]"
		})
	}
	return text
}
