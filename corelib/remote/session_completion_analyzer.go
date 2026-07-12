package remote

import "strings"

var completionSignals = []string{
	"", "i've completed", "已完成", "all done",
	"changes applied", "task is complete",
	"all tasks completed", "everything is done",
}

var incompletionSignals = []string{
	"i'll continue", "接下来我会", "next, i'll",
	"let me continue", "i need to", "还需要",
	"left to do", "not yet finished",
	"还没完成", "未完成", "待完成",
	"to do next", "next step is",
	"i'll now", "let me now", "now i'll",
	"i will now", "let's continue",
}

// CompletionAnalyzerConfig holds configuration for the CompletionAnalyzer.
type CompletionAnalyzerConfig struct {
	AnalyzeLineCount int // number of recent lines to scan; default 50
}

// CompletionAnalyzer performs semantic task-completion analysis on session
// output lines. It is a pure function with no I/O.
type CompletionAnalyzer struct {
	config CompletionAnalyzerConfig
}

// NewCompletionAnalyzer creates a CompletionAnalyzer with the given config.
func NewCompletionAnalyzer(config CompletionAnalyzerConfig) *CompletionAnalyzer {
	if config.AnalyzeLineCount <= 0 {
		config.AnalyzeLineCount = 50
	}
	return &CompletionAnalyzer{config: config}
}

// Analyze inspects the most recent output lines and returns a CompletionLevel.
func (a *CompletionAnalyzer) Analyze(lines []string, tool string, sdkResult *SDKResultPayload) CompletionLevel {
	if len(lines) == 0 {
		return CompletionUncertain
	}

	start := 0
	if len(lines) > a.config.AnalyzeLineCount {
		start = len(lines) - a.config.AnalyzeLineCount
	}
	tail := lines[start:]

	completionCount := 0
	incompletionCount := 0

	// NOTE: sdkResult != nil only means "a turn ended", NOT "the task is
	// done". Claude Code sends a result message after every turn, even if
	// the task is only partially complete. Do not count it as a completion
	// signal — rely solely on output content analysis.

	for _, line := range tail {
		lower := strings.ToLower(line)

		for _, sig := range completionSignals {
			if strings.Contains(lower, sig) {
				completionCount++
				break
			}
		}
		for _, sig := range incompletionSignals {
			if strings.Contains(lower, sig) {
				incompletionCount++
				break
			}
		}
	}

	if completionCount > incompletionCount {
		return CompletionCompleted
	}
	if incompletionCount > 0 {
		return CompletionIncomplete
	}
	return CompletionUncertain
}
