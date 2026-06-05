package main

// earlyExitLineThreshold is the maximum number of output lines below which
// a session exit is considered "early" (the tool quit before doing real work).
// Used by both CompletionAnalyzer and runExitLoop to avoid duplicating the
// magic number.
const earlyExitLineThreshold = 10

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
//  3. Scan last N lines for completion / incompletion signals
//  4. completionCount > incompletionCount → CompletionCompleted
//  5. incompletionCount > 0 → CompletionIncomplete
//  6. Otherwise → CompletionUncertain
//
// Note: early-exit detection (session exited with very few output lines) is
// handled by runExitLoop, not here, because Analyze is also called on
// structured sessions may produce short outputs between turns.
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
		switch classifySessionCompletionSignal(line) {
		case sessionCompletionSignalCompleted:
			completionCount++
		case sessionCompletionSignalIncomplete:
			incompletionCount++
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
