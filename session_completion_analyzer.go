package main

import "strings"

// completionSignals are phrases that indicate a task has been completed.
var completionSignals = []string{
	"✅",
	"i've completed",
	"已完成",
	"all done",
	"successfully",
	"changes applied",
}

// incompletionSignals are phrases that indicate a task is still in progress.
var incompletionSignals = []string{
	"i'll continue",
	"接下来我会",
	"next, i'll",
	"let me continue",
	"i need to",
	"还需要",
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
// If AnalyzeLineCount is <= 0, it defaults to 50.
func NewCompletionAnalyzer(config CompletionAnalyzerConfig) *CompletionAnalyzer {
	if config.AnalyzeLineCount <= 0 {
		config.AnalyzeLineCount = 50
	}
	return &CompletionAnalyzer{config: config}
}

// Analyze inspects the most recent output lines and returns a CompletionLevel.
//
// Logic:
//  1. Empty lines → CompletionUncertain
//  2. Non-nil sdkResult (SDK finished without error) → bias toward CompletionCompleted
//  3. Scan last N lines for completion / incompletion signals and Gemini ACP markers
//  4. completionCount > incompletionCount → CompletionCompleted
//  5. incompletionCount > 0 → CompletionIncomplete
//  6. Otherwise → CompletionUncertain
func (a *CompletionAnalyzer) Analyze(lines []string, tool string, sdkResult *SDKResultPayload) CompletionLevel {
	if len(lines) == 0 {
		return CompletionUncertain
	}

	// Take the last N lines.
	start := 0
	if len(lines) > a.config.AnalyzeLineCount {
		start = len(lines) - a.config.AnalyzeLineCount
	}
	tail := lines[start:]

	completionCount := 0
	incompletionCount := 0

	// If sdkResult is present (non-nil), the SDK completed without error.
	if sdkResult != nil {
		completionCount++
	}

	for _, line := range tail {
		lower := strings.ToLower(line)

		// Check Gemini ACP turn-complete marker.
		if strings.HasPrefix(lower, "[gemini-acp] turn complete:") {
			rest := strings.TrimSpace(line[len("[gemini-acp] turn complete:"):])
			restLower := strings.ToLower(rest)
			if strings.Contains(restLower, "success") || strings.Contains(restLower, "done") || strings.Contains(restLower, "completed") {
				completionCount++
			}
			continue
		}

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
