package main

// isFatalSessionError scans the recent output lines for patterns that
// indicate an unrecoverable configuration error (missing API key,
// authentication failure, tool not installed, etc.). These errors will
// not be fixed by retrying; the user must fix the configuration first.
// Returns false for transient errors (rate limits, network timeouts,
// server 5xx, etc.) which are worth retrying.
func isFatalSessionError(lines []string) bool {
	// Only scan the last 20 lines; error messages are near the end.
	start := 0
	if len(lines) > 20 {
		start = len(lines) - 20
	}
	for _, line := range lines[start:] {
		if classifyFatalSessionOutputLine(line) != fatalSessionErrorNone {
			return true
		}
	}
	return false
}
