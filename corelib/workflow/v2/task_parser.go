package v2

import (
	"regexp"
	"strconv"
	"strings"
)

// TaskItem represents a parsed task from the task breakdown document.
type TaskItem struct {
	Index       int      `json:"index"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Files       []string `json:"files,omitempty"`
	DependsOn   []int    `json:"depends_on,omitempty"`
}

// taskHeadingPatterns matches various task heading formats:
// - "### T1: title"
// - "## T1: title"
// - "T1: title" (bare)
// - "**T1: title**"
// - "- T1: title"
// - "* T1: title"
var taskHeadingPatterns = []*regexp.Regexp{
	regexp.MustCompile(`^#{1,4}\s+[Tt](\d+)\s*[:：]\s*(.+)`),         // ### T1: title
	regexp.MustCompile(`^\s*[-*]\s+\*{0,2}[Tt](\d+)\s*[:：]\s*(.+)`), // - T1: title or - **T1: title
	regexp.MustCompile(`^\s*\*{0,2}[Tt](\d+)\s*[:：]\s*(.+)`),        // T1: title or **T1: title
}

// ParseTaskList extracts tasks from a Markdown task breakdown document.
// Supports various heading formats (###, **, bare T1:, list items).
func ParseTaskList(text string) []*TaskItem {
	lines := strings.Split(text, "\n")
	var tasks []*TaskItem
	var current *TaskItem
	var bodyLines []string

	flush := func() {
		if current != nil {
			current.Description, current.Files, current.DependsOn = parseTaskBody(bodyLines)
			tasks = append(tasks, current)
		}
	}

	for _, line := range lines {
		if idx, title, ok := matchTaskHeading(line); ok {
			flush()
			current = &TaskItem{
				Index: idx,
				Title: strings.TrimRight(strings.TrimSpace(title), "*"),
			}
			bodyLines = nil
			continue
		}
		if current != nil {
			bodyLines = append(bodyLines, line)
		}
	}
	flush()
	return tasks
}

// matchTaskHeading tries all heading patterns and returns (index, title, matched).
func matchTaskHeading(line string) (int, string, bool) {
	for _, re := range taskHeadingPatterns {
		if matches := re.FindStringSubmatch(line); len(matches) == 3 {
			idx, err := strconv.Atoi(matches[1])
			if err != nil {
				continue
			}
			return idx, matches[2], true
		}
	}
	return 0, "", false
}

func parseTaskBody(lines []string) (description string, files []string, dependsOn []int) {
	for _, raw := range lines {
		line := strings.TrimSpace(strings.TrimLeft(raw, "-* "))
		lower := strings.ToLower(strings.ReplaceAll(line, "*", ""))

		if fieldValue := extractFieldValue(lower, line, "描述", "description"); fieldValue != "" {
			description = fieldValue
		} else if fieldValue := extractFieldValue(lower, line, "涉及文件", "文件", "files", "file"); fieldValue != "" {
			files = splitFiles(fieldValue)
		} else if fieldValue := extractFieldValue(lower, line, "依赖", "depends", "dependency", "dependencies"); fieldValue != "" {
			dependsOn = parseDependencies(fieldValue)
		}
	}
	return
}

func extractFieldValue(lower, original string, markers ...string) string {
	for _, marker := range markers {
		lowerMarker := strings.ToLower(marker)
		idx := strings.Index(lower, lowerMarker)
		if idx < 0 {
			continue
		}
		// Find the colon after the marker in the original string
		afterMarker := idx + len(lowerMarker)
		if afterMarker >= len(original) {
			continue
		}
		rest := original[afterMarker:]
		colonIdx := strings.IndexAny(rest, ":：")
		if colonIdx >= 0 && colonIdx < 5 {
			value := strings.TrimSpace(rest[colonIdx+len(":"):])
			if strings.HasPrefix(rest[colonIdx:], "：") {
				value = strings.TrimSpace(rest[colonIdx+len("："):])
			}
			// Strip trailing bold markers
			value = strings.TrimRight(value, "*")
			return value
		}
	}
	return ""
}

func splitFiles(s string) []string {
	// Split by comma, remove backticks and whitespace
	parts := strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == '、' || r == ';'
	})
	var files []string
	for _, p := range parts {
		f := strings.TrimSpace(strings.Trim(p, "`"))
		if f != "" {
			files = append(files, f)
		}
	}
	return files
}

var depRe = regexp.MustCompile(`[Tt](\d+)`)

func parseDependencies(s string) []int {
	lower := strings.ToLower(s)
	if strings.Contains(lower, "无") || strings.Contains(lower, "none") || strings.Contains(lower, "n/a") {
		return nil
	}
	matches := depRe.FindAllStringSubmatch(s, -1)
	var deps []int
	for _, m := range matches {
		if n, err := strconv.Atoi(m[1]); err == nil {
			deps = append(deps, n)
		}
	}
	return deps
}
