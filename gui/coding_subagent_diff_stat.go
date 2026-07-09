package main

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// ---------------------------------------------------------------------------
// Structured Git Diff Stat for CodingSubAgent
//
// Codex-inspired improvement: parse `git diff --stat` into structured data
// instead of only storing the raw diff text. This enables orchestrator reports
// to show GitHub-PR-style change summaries (+145 -23) rather than plain text.
// ---------------------------------------------------------------------------

// SubAgentDiffStat holds structured information from `git diff --stat`.
type SubAgentDiffStat struct {
	FilesChanged int
	Insertions   int
	Deletions    int
	FileStats    []SubAgentFileDiffStat
}

// SubAgentFileDiffStat holds per-file change statistics.
type SubAgentFileDiffStat struct {
	Path       string
	Insertions int
	Deletions  int
}

// Summary returns a compact one-line summary like "3 files changed (+145 -23)".
func (s *SubAgentDiffStat) Summary() string {
	if s == nil || s.FilesChanged == 0 {
		return "no changes"
	}
	return fmt.Sprintf("%d files changed (+%d -%d)", s.FilesChanged, s.Insertions, s.Deletions)
}

// FileReport returns a multi-line report showing per-file changes.
func (s *SubAgentDiffStat) FileReport() string {
	if s == nil || len(s.FileStats) == 0 {
		return ""
	}
	var b strings.Builder
	for _, f := range s.FileStats {
		b.WriteString(fmt.Sprintf("  %s +%d -%d\n", f.Path, f.Insertions, f.Deletions))
	}
	return b.String()
}

// git diff --stat output format:
//  src/auth/login.go      | 103 +++++++--
//  src/auth/login_test.go |  45 ++++
//  2 files changed, 140 insertions(+), 8 deletions(-)
var (
	// Per-file line: " path | N +++---" or " path | Bin 0 -> 1234 bytes"
	subAgentDiffStatFileRe = regexp.MustCompile(`^\s*(.+?)\s*\|\s*(\d+)\s*[+\-]*\s*$`)
	// Summary line: "N file(s) changed, M insertion(s)(+), K deletion(s)(-)"
	subAgentDiffStatSummaryRe = regexp.MustCompile(`(\d+)\s+files?\s+changed(?:,\s*(\d+)\s+insertions?\(\+\))?(?:,\s*(\d+)\s+deletions?\(-\))?`)
)

// parseGitDiffStat parses the output of `git diff --stat` into structured data.
// Returns nil if the output cannot be parsed (e.g., not a git repo, empty diff).
func parseGitDiffStat(statOutput string) *SubAgentDiffStat {
	statOutput = strings.TrimSpace(statOutput)
	if statOutput == "" {
		return nil
	}

	lines := strings.Split(strings.ReplaceAll(statOutput, "\r\n", "\n"), "\n")

	result := &SubAgentDiffStat{}

	for _, line := range lines {
		// Try summary line first (usually last line)
		if m := subAgentDiffStatSummaryRe.FindStringSubmatch(line); m != nil {
			result.FilesChanged, _ = strconv.Atoi(m[1])
			if m[2] != "" {
				result.Insertions, _ = strconv.Atoi(m[2])
			}
			if m[3] != "" {
				result.Deletions, _ = strconv.Atoi(m[3])
			}
			continue
		}

		// Try per-file line
		if m := subAgentDiffStatFileRe.FindStringSubmatch(line); m != nil {
			path := strings.TrimSpace(m[1])
			changes, _ := strconv.Atoi(m[2])
			// Approximate split: count + and - chars in the bar
			ins, del := approximateInsDelFromStatLine(line, changes)
			result.FileStats = append(result.FileStats, SubAgentFileDiffStat{
				Path:       path,
				Insertions: ins,
				Deletions:  del,
			})
		}
	}

	// If we parsed file stats but no summary line, derive from file stats
	if result.FilesChanged == 0 && len(result.FileStats) > 0 {
		result.FilesChanged = len(result.FileStats)
		for _, f := range result.FileStats {
			result.Insertions += f.Insertions
			result.Deletions += f.Deletions
		}
	}

	if result.FilesChanged == 0 {
		return nil
	}
	return result
}

// approximateInsDelFromStatLine approximates insertions/deletions from the
// visual +/- bar in git diff --stat output. If we can't parse the bar,
// assume all changes are insertions (common for new/heavily modified files).
func approximateInsDelFromStatLine(line string, totalChanges int) (int, int) {
	// Find the bar portion after the pipe and number
	pipeIdx := strings.LastIndex(line, "|")
	if pipeIdx < 0 {
		return totalChanges, 0
	}
	bar := line[pipeIdx+1:]

	plusCount := strings.Count(bar, "+")
	minusCount := strings.Count(bar, "-")

	total := plusCount + minusCount
	if total == 0 {
		return totalChanges, 0
	}

	ins := totalChanges * plusCount / total
	del := totalChanges - ins
	return ins, del
}
