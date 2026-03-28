package security

import (
	"regexp"
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
			{name: "sk_api_key", category: "api_key", re: regexp.MustCompile(`sk-[a-zA-Z0-9]{20,}`)},
			{name: "aws_access_key", category: "api_key", re: regexp.MustCompile(`AKIA[A-Z0-9]{16}`)},
			{name: "private_key_header", category: "private_key", re: regexp.MustCompile(`-----BEGIN.*PRIVATE KEY-----`)},
			{name: "password_assignment", category: "password", re: regexp.MustCompile(`(?i)(password|passwd|pwd)\s*[=:]\s*\S+`)},
			{name: "jwt_token", category: "jwt", re: regexp.MustCompile(`eyJ[a-zA-Z0-9_-]+\.eyJ[a-zA-Z0-9_-]+\.[a-zA-Z0-9_-]+`)},
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
		if p.re.MatchString(text) {
			key := p.category + ":" + p.name
			if !seen[key] {
				seen[key] = true
				matches = append(matches, SensitiveMatch{
					Category: p.category,
					Pattern:  p.name,
				})
			}
		}
	}
	return matches
}

// Redact replaces all detected sensitive patterns in text with [REDACTED].
func (d *SensitiveDetector) Redact(text string) string {
	for _, p := range d.patterns {
		text = p.re.ReplaceAllString(text, "[REDACTED]")
	}
	return text
}
