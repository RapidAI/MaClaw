package skill

import (
	"fmt"
	"strings"
)

// severityIndicator returns a plain-text indicator for a given severity level.
func severityIndicator(s IssueSeverity) string {
	switch s {
	case SeverityError:
		return "[ERR]"
	case SeverityWarning:
		return "[WARN]"
	case SeverityInfo:
		return "[INFO]"
	default:
		return "?"
	}
}

// FormatPortabilityReport returns a human-readable text summary of the report
// with severity indicators (error, warning, info) and fix suggestions.
func FormatPortabilityReport(report *PortabilityReport) string {
	if report == nil {
		return ""
	}

	var b strings.Builder

	// Header
	b.WriteString(fmt.Sprintf("Portability Report: %s\n", report.SkillName))
	b.WriteString("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")

	if len(report.Issues) == 0 {
		b.WriteString("\nNo portability issues found.\n")
	} else {
		b.WriteByte('\n')
		for _, issue := range report.Issues {
			// Main issue line: indicator [category] file: message
			b.WriteString(fmt.Sprintf("%s [%s] %s: %s\n",
				severityIndicator(issue.Severity),
				issue.Category,
				issue.File,
				issue.Message,
			))
			// Suggestion line (indented)
			if issue.Suggestion != "" {
				b.WriteString(fmt.Sprintf("   %s\n", issue.Suggestion))
			}
			b.WriteByte('\n')
		}
	}

	// Footer separator
	b.WriteString("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")

	// Summary line
	b.WriteString(fmt.Sprintf("Summary: %d error, %d warning, %d info\n",
		report.Summary.Errors,
		report.Summary.Warnings,
		report.Summary.Infos,
	))

	// Market-ready status
	if report.MarketReady {
		b.WriteString("Market Ready: Yes\n")
	} else {
		b.WriteString("Market Ready: No (fix errors before uploading)\n")
	}

	return b.String()
}

// FormatPortabilityChanges returns a human-readable summary of auto-fix changes.
// Each change shows the file, field, original value, and replacement value.
func FormatPortabilityChanges(changes []PortabilityChange) string {
	if len(changes) == 0 {
		return "No changes made.\n"
	}

	var b strings.Builder

	b.WriteString(fmt.Sprintf("Auto-Fix Changes (%d):\n", len(changes)))
	b.WriteString("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")

	for i, c := range changes {
		// File and field
		if c.Field != "" {
			b.WriteString(fmt.Sprintf("%d. %s [%s]\n", i+1, c.File, c.Field))
		} else {
			b.WriteString(fmt.Sprintf("%d. %s\n", i+1, c.File))
		}
		b.WriteString(fmt.Sprintf("   - %s\n", c.Original))
		b.WriteString(fmt.Sprintf("   + %s\n", c.Replacement))
		if i < len(changes)-1 {
			b.WriteByte('\n')
		}
	}

	return b.String()
}
