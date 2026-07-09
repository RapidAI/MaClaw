package main

import (
	"fmt"
	"regexp"
	"strings"
)

// ---------------------------------------------------------------------------
// Adaptive Tool Result Truncation for CodingSubAgent
//
// Codex-inspired improvement: tool results are truncated based on information
// density rather than returned verbatim. This dramatically reduces context
// consumption — a 3000-line read_file that would consume ~6000 tokens is
// reduced to ~1500 tokens (head 80 + tail 30 lines) while preserving
// reachability (the truncation message tells the LLM how to read omitted parts).
//
// Design principles:
// - Short results pass through unchanged (no penalty for typical edit/write confirmations)
// - Error lines are prioritized in bash output (first error >> last 500 lines of build log)
// - Truncation hints are actionable ("use offset=80 to read from this point")
// - git_diff preserves stat summary + initial hunks (structure > raw lines)
// ---------------------------------------------------------------------------

const (
	// Token thresholds per tool type. These are conservative — typical tool
	// results well below these thresholds pass through unchanged.
	subAgentTruncReadFileTokens   = 3000 // ~200 lines of code
	subAgentTruncBashTokens       = 1500 // ~100 lines of output
	subAgentTruncListDirMaxLines  = 200
	subAgentTruncSearchMaxMatches = 50
	subAgentTruncGitDiffTokens    = 2000

	// Line budgets for truncated output
	subAgentTruncReadFileHead = 80
	subAgentTruncReadFileTail = 30
	subAgentTruncBashHead     = 15
	subAgentTruncBashTail     = 15
	subAgentTruncBashErrors   = 10
	subAgentTruncSearchKeep   = 30
)

var subAgentBashErrorRe = regexp.MustCompile(`(?i)\b(error|Error|ERROR|failed|FAILED|panic|PANIC|fatal|FATAL|exception|Exception)\b`)

// truncateToolResultForSubAgent applies adaptive truncation to tool results
// based on tool type and information density. Short results pass unchanged.
func truncateToolResultForSubAgent(toolName string, result string) string {
	if result == "" {
		return result
	}

	// Inline token estimate: len(bytes) / 2.5 ≈ (len*10+24)/25
	tokenEst := func(s string) int { return (len(s)*10 + 24) / 25 }

	switch toolName {
	case "read_file":
		if tokenEst(result) <= subAgentTruncReadFileTokens {
			return result
		}
		return truncateSubAgentReadFileResult(result)

	case "bash":
		if tokenEst(result) <= subAgentTruncBashTokens {
			return result
		}
		return truncateSubAgentBashResult(result)

	case "list_directory":
		if strings.Count(result, "\n") <= subAgentTruncListDirMaxLines {
			return result
		}
		return truncateSubAgentListDirResult(result)

	case "Glob", "ripgrep":
		if countSubAgentSearchMatches(result) <= subAgentTruncSearchMaxMatches {
			return result
		}
		return truncateSubAgentSearchResult(result)

	case "git_diff":
		if tokenEst(result) <= subAgentTruncGitDiffTokens {
			return result
		}
		return truncateSubAgentGitDiffResult(result)

	default:
		return result
	}
}

func truncateSubAgentReadFileResult(result string) string {
	lines := strings.Split(result, "\n")
	totalLines := len(lines)
	if totalLines <= subAgentTruncReadFileHead+subAgentTruncReadFileTail+5 {
		return result
	}

	omitted := totalLines - subAgentTruncReadFileHead - subAgentTruncReadFileTail

	var b strings.Builder
	b.Grow(len(result) / 3)

	// Head
	for i := 0; i < subAgentTruncReadFileHead && i < totalLines; i++ {
		b.WriteString(lines[i])
		b.WriteString("\n")
	}

	// Omission hint with actionable guidance
	b.WriteString(fmt.Sprintf(
		"\n[... %d lines omitted. Use read_file with offset=%d to read from this point ...]\n\n",
		omitted, subAgentTruncReadFileHead+1))

	// Tail
	tailStart := totalLines - subAgentTruncReadFileTail
	if tailStart < subAgentTruncReadFileHead {
		tailStart = subAgentTruncReadFileHead
	}
	for i := tailStart; i < totalLines; i++ {
		b.WriteString(lines[i])
		if i < totalLines-1 {
			b.WriteString("\n")
		}
	}

	return b.String()
}

func truncateSubAgentBashResult(result string) string {
	lines := strings.Split(result, "\n")
	totalLines := len(lines)
	if totalLines <= subAgentTruncBashHead+subAgentTruncBashTail+5 {
		return result
	}

	// Extract error lines (prioritized information) with their line indices
	type errorLine struct {
		text string
		idx  int
	}
	var errorLines []errorLine
	for i, line := range lines {
		if subAgentBashErrorRe.MatchString(line) {
			errorLines = append(errorLines, errorLine{text: line, idx: i})
			if len(errorLines) >= subAgentTruncBashErrors {
				break
			}
		}
	}

	// Build a set of error line indices to avoid duplicating them in head/tail
	errorIdxSet := make(map[int]bool, len(errorLines))
	for _, el := range errorLines {
		errorIdxSet[el.idx] = true
	}

	var b strings.Builder
	b.Grow(len(result) / 3)

	// Error lines first (highest information density)
	if len(errorLines) > 0 {
		b.WriteString("=== Error/failure lines (prioritized) ===\n")
		for _, el := range errorLines {
			b.WriteString(el.text)
			b.WriteString("\n")
		}
		b.WriteString("\n=== Output (head + tail) ===\n")
	}

	// Head (skip lines already shown in error section)
	headEnd := subAgentTruncBashHead
	if headEnd > totalLines {
		headEnd = totalLines
	}
	for i := 0; i < headEnd; i++ {
		if errorIdxSet[i] {
			continue // already shown above
		}
		b.WriteString(lines[i])
		b.WriteString("\n")
	}

	// Omission
	omitted := totalLines - subAgentTruncBashHead - subAgentTruncBashTail
	if omitted > 0 {
		b.WriteString(fmt.Sprintf("\n[... %d lines truncated ...]\n\n", omitted))
	}

	// Tail (skip lines already shown in error section)
	tailStart := totalLines - subAgentTruncBashTail
	if tailStart <= headEnd {
		tailStart = headEnd
	}
	for i := tailStart; i < totalLines; i++ {
		if errorIdxSet[i] {
			continue // already shown above
		}
		b.WriteString(lines[i])
		if i < totalLines-1 {
			b.WriteString("\n")
		}
	}

	return b.String()
}

func truncateSubAgentListDirResult(result string) string {
	lines := strings.Split(result, "\n")
	totalLines := len(lines)

	// Count directories vs files for summary
	dirCount := 0
	fileCount := 0
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasSuffix(trimmed, "/") || strings.HasSuffix(trimmed, "\\") {
			dirCount++
		} else {
			fileCount++
		}
	}

	var b strings.Builder
	// Show first 100 entries
	shown := 100
	if shown > totalLines {
		shown = totalLines
	}
	for i := 0; i < shown; i++ {
		b.WriteString(lines[i])
		b.WriteString("\n")
	}
	b.WriteString(fmt.Sprintf(
		"\n[... %d more entries omitted. Total: %d directories, %d files. Use list_directory on subdirectories for details ...]\n",
		totalLines-shown, dirCount, fileCount))

	return b.String()
}

func countSubAgentSearchMatches(result string) int {
	// Each match typically starts with a file path line
	count := 0
	for _, line := range strings.Split(result, "\n") {
		if strings.TrimSpace(line) != "" && !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") {
			count++
		}
	}
	return count
}

func truncateSubAgentSearchResult(result string) string {
	lines := strings.Split(result, "\n")
	totalLines := len(lines)

	// Keep first N non-empty match groups
	var b strings.Builder
	matchCount := 0
	for i, line := range lines {
		if matchCount >= subAgentTruncSearchKeep {
			remaining := totalLines - i
			b.WriteString(fmt.Sprintf("\n[... %d more matches omitted. Narrow your search pattern for more targeted results ...]\n", remaining))
			break
		}
		b.WriteString(line)
		b.WriteString("\n")
		// Count non-continuation lines as matches
		if strings.TrimSpace(line) != "" && !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") {
			matchCount++
		}
	}

	return b.String()
}

func truncateSubAgentGitDiffResult(result string) string {
	lines := strings.Split(result, "\n")
	totalLines := len(lines)

	// Find the --stat summary line (usually near the end or at the beginning if --stat was used)
	// For regular diff output, keep first N lines (usually enough for context)
	keepLines := 80
	if keepLines >= totalLines {
		return result
	}

	var b strings.Builder
	for i := 0; i < keepLines; i++ {
		b.WriteString(lines[i])
		b.WriteString("\n")
	}
	omitted := totalLines - keepLines
	b.WriteString(fmt.Sprintf("\n[... %d more diff lines omitted. The full diff is available via git_diff tool ...]\n", omitted))

	return b.String()
}
