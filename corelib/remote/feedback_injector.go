package remote

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
)

// FeedbackEntry 表示一条结构化反馈。
type FeedbackEntry struct {
	Source   string // "linter", "test", "build", "ci"
	Severity string // "error", "warning"
	File     string // 文件路径
	Line     int    // 行号
	Message  string // 错误描述
}

// FeedbackInjector 从 OutputPipeline 事件中提取反馈并注入下次 session。
type FeedbackInjector struct {
	maxTokens       int
	sessionFeedback map[string][]FeedbackEntry
	mu              sync.RWMutex
}

// NewFeedbackInjector 创建反馈注入器。
func NewFeedbackInjector(maxTokens int) *FeedbackInjector {
	if maxTokens <= 0 {
		maxTokens = 2000
	}
	return &FeedbackInjector{
		maxTokens:       maxTokens,
		sessionFeedback: make(map[string][]FeedbackEntry),
	}
}

// lineNumPattern matches `:linenum:` patterns in summary text.
var lineNumPattern = regexp.MustCompile(`:(\d+):`)

// ConsumeEvents 从 OutputPipeline 事件中提取反馈。
func (f *FeedbackInjector) ConsumeEvents(sessionID string, events []ImportantEvent) {
	f.mu.Lock()
	defer f.mu.Unlock()

	for _, evt := range events {
		if !isErrorEvent(evt) {
			continue
		}

		entry := FeedbackEntry{
			Source:   classifySource(evt),
			Severity: normalizeSeverity(evt.Severity),
			File:     evt.RelatedFile,
			Line:     extractLineNumber(evt.Summary),
			Message:  evt.Summary,
		}
		f.sessionFeedback[sessionID] = append(f.sessionFeedback[sessionID], entry)
	}
}

// BuildFeedbackBlock 为下次 session 生成反馈块。
// 按严重程度排序，超出 token 限制时截断低优先级错误。
func (f *FeedbackInjector) BuildFeedbackBlock(prevSessionID string) string {
	f.mu.RLock()
	entries := f.sessionFeedback[prevSessionID]
	f.mu.RUnlock()

	if len(entries) == 0 {
		return ""
	}

	// Sort: errors before warnings, then by source.
	sorted := make([]FeedbackEntry, len(entries))
	copy(sorted, entries)
	sort.SliceStable(sorted, func(i, j int) bool {
		si := severityRank(sorted[i].Severity)
		sj := severityRank(sorted[j].Severity)
		if si != sj {
			return si < sj
		}
		return sorted[i].Source < sorted[j].Source
	})

	var b strings.Builder
	header := "[📋 上次 Session 反馈]\n"
	footer := "[/反馈]\n"
	b.WriteString(header)

	for _, entry := range sorted {
		line := formatFeedbackLine(entry)
		candidate := b.String() + line + footer
		if estimateTokens(candidate) > f.maxTokens {
			break
		}
		b.WriteString(line)
	}

	b.WriteString(footer)

	result := b.String()
	// If only header+footer (no entries fit), return empty.
	if result == header+footer {
		return ""
	}
	return result
}

// Clear 清除指定 session 的反馈。
func (f *FeedbackInjector) Clear(sessionID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.sessionFeedback, sessionID)
}

// isErrorEvent checks if an event qualifies as error feedback.
func isErrorEvent(evt ImportantEvent) bool {
	switch evt.Type {
	case "session.error", "command.failed":
		return true
	}
	switch evt.Severity {
	case "error", "warn":
		return true
	}
	return false
}

// classifySource determines the error source from event type and summary.
func classifySource(evt ImportantEvent) string {
	lower := strings.ToLower(evt.Summary)
	if strings.Contains(lower, "lint") {
		return "linter"
	}
	if strings.Contains(lower, "test") {
		return "test"
	}
	if strings.Contains(lower, "build") || strings.Contains(lower, "compil") {
		return "build"
	}
	return "ci"
}

// normalizeSeverity maps event severity to feedback severity.
func normalizeSeverity(sev string) string {
	switch sev {
	case "error":
		return "error"
	case "warn", "warning":
		return "warning"
	default:
		return "error"
	}
}

// extractLineNumber tries to find a :linenum: pattern in text.
func extractLineNumber(text string) int {
	matches := lineNumPattern.FindStringSubmatch(text)
	if len(matches) < 2 {
		return 0
	}
	var n int
	fmt.Sscanf(matches[1], "%d", &n)
	return n
}

// severityRank returns sort rank (lower = higher priority).
func severityRank(sev string) int {
	if sev == "error" {
		return 0
	}
	return 1
}

// formatFeedbackLine formats a single feedback entry.
func formatFeedbackLine(entry FeedbackEntry) string {
	filePart := entry.File
	if entry.Line > 0 {
		filePart = fmt.Sprintf("%s:%d", entry.File, entry.Line)
	}
	if filePart == "" {
		filePart = "(unknown)"
	}
	return fmt.Sprintf("来源: %-6s | 文件: %s | 错误: %s\n",
		entry.Source, filePart, entry.Message)
}

// estimateTokens estimates token count as len(content)/2.
func estimateTokens(content string) int {
	return len(content) / 2
}
