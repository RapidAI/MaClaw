package skill

import "time"

// IssueSeverity represents the severity level of a portability issue.
type IssueSeverity string

const (
	SeverityError   IssueSeverity = "error"
	SeverityWarning IssueSeverity = "warning"
	SeverityInfo    IssueSeverity = "info"
)

// PortabilityIssue represents a single portability problem found in a skill.
type PortabilityIssue struct {
	Severity   IssueSeverity `json:"severity"`
	Category   string        `json:"category"`
	Message    string        `json:"message"`
	File       string        `json:"file"`
	Line       int           `json:"line,omitempty"`
	Suggestion string        `json:"suggestion,omitempty"`
}

// IssueSummary holds counts of issues by severity.
type IssueSummary struct {
	Errors   int `json:"errors"`
	Warnings int `json:"warnings"`
	Infos    int `json:"infos"`
}

// PortabilityReport is the structured result of portability validation.
type PortabilityReport struct {
	SkillName   string             `json:"skill_name"`
	SkillDir    string             `json:"skill_dir"`
	Issues      []PortabilityIssue `json:"issues"`
	Summary     IssueSummary       `json:"summary"`
	MarketReady bool               `json:"market_ready"`
	Timestamp   time.Time          `json:"timestamp"`
}

// PortabilityChange records a single auto-fix modification.
type PortabilityChange struct {
	File        string `json:"file"`
	Field       string `json:"field,omitempty"`
	Original    string `json:"original"`
	Replacement string `json:"replacement"`
}

// NewPortabilityReport creates a PortabilityReport from the given issues,
// computing Summary counts and MarketReady from the issues list.
func NewPortabilityReport(skillName, skillDir string, issues []PortabilityIssue) *PortabilityReport {
	var summary IssueSummary
	for _, issue := range issues {
		switch issue.Severity {
		case SeverityError:
			summary.Errors++
		case SeverityWarning:
			summary.Warnings++
		case SeverityInfo:
			summary.Infos++
		}
	}
	return &PortabilityReport{
		SkillName:   skillName,
		SkillDir:    skillDir,
		Issues:      issues,
		Summary:     summary,
		MarketReady: summary.Errors == 0,
		Timestamp:   time.Now(),
	}
}
