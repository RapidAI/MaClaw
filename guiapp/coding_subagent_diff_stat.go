package guiapp

import (
	"fmt"
	"strconv"
	"strings"
)

// ---------------------------------------------------------------------------
// Structured Git Diff Stat for CodingSubAgent
//
// Codex-inspired improvement: parse `git diff --numstat` into structured data
// instead of only storing the raw diff text. This enables orchestrator reports
// to show GitHub-PR-style change summaries (+145 -23) rather than plain text.
// ---------------------------------------------------------------------------

// SubAgentDiffStat holds structured diff information.
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
	// HasCounts distinguishes measured line counts (including an exact zero)
	// from a merely observed file touch. Progress UI may still use the numeric
	// fields, but final user-facing tables must not present unknown counts as zero.
	HasCounts bool
}

// Summary returns a compact one-line summary like "3 files changed (+145 -23)".
func (s *SubAgentDiffStat) Summary() string {
	if s == nil || s.FilesChanged == 0 {
		return "no changes"
	}
	unknown := 0
	for _, file := range s.FileStats {
		if !file.HasCounts {
			unknown++
		}
	}
	if unknown > 0 {
		return fmt.Sprintf("%d files changed (+%d -%d, %d unknown)", s.FilesChanged, s.Insertions, s.Deletions, unknown)
	}
	return fmt.Sprintf("%d files changed (+%d -%d)", s.FilesChanged, s.Insertions, s.Deletions)
}

// parseGitDiffNumStat parses `git diff --numstat`. Unlike --stat, numstat
// carries exact added/removed line counts instead of a scaled visual +/- bar.
// Binary entries use "-" and are kept as observed files with unknown counts.
func parseGitDiffNumStat(numstatOutput string) *SubAgentDiffStat {
	numstatOutput = strings.TrimSpace(numstatOutput)
	if numstatOutput == "" {
		return nil
	}
	result := &SubAgentDiffStat{}
	for _, line := range strings.Split(strings.ReplaceAll(numstatOutput, "\r\n", "\n"), "\n") {
		fields := strings.SplitN(line, "\t", 3)
		if len(fields) != 3 {
			continue
		}
		path := gitNumstatDisplayPath(gitNumstatDecodePath(strings.TrimSpace(fields[2])))
		if path == "" {
			continue
		}
		added, addedErr := strconv.Atoi(strings.TrimSpace(fields[0]))
		removed, removedErr := strconv.Atoi(strings.TrimSpace(fields[1]))
		hasCounts := addedErr == nil && removedErr == nil
		if !hasCounts {
			added, removed = 0, 0
		}
		result.FileStats = append(result.FileStats, SubAgentFileDiffStat{
			Path:       path,
			Insertions: added,
			Deletions:  removed,
			HasCounts:  hasCounts,
		})
		if hasCounts {
			result.Insertions += added
			result.Deletions += removed
		}
	}
	if len(result.FileStats) == 0 {
		return nil
	}
	result.FilesChanged = len(result.FileStats)
	return result
}

// gitNumstatDecodePath decodes git's C-style quoted path spelling. Git quotes
// non-ASCII and control-character paths by default, so leaving that spelling in
// place would prevent the evidence matcher from recognizing the same Unicode
// project path.
func gitNumstatDecodePath(path string) string {
	if len(path) < 2 || path[0] != '"' || path[len(path)-1] != '"' {
		return path
	}
	if decoded, err := strconv.Unquote(path); err == nil {
		return decoded
	}
	return path
}

// gitNumstatDisplayPath selects the post-rename path from git's numstat rename
// spellings: "old => new" and "dir/{old => new}/file.go".
func gitNumstatDisplayPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" || !strings.Contains(path, " => ") {
		return path
	}
	if open := strings.Index(path, "{"); open >= 0 {
		if close := strings.Index(path[open:], "}"); close > 0 {
			close += open
			inside := path[open+1 : close]
			if parts := strings.SplitN(inside, " => ", 2); len(parts) == 2 {
				return path[:open] + parts[1] + path[close+1:]
			}
		}
	}
	if parts := strings.SplitN(path, " => ", 2); len(parts) == 2 {
		return strings.TrimSpace(parts[1])
	}
	return path
}
