package main

import (
	"fmt"
	"strings"
	"testing"
)

func TestTruncateToolResult_ShortResults_PassThrough(t *testing.T) {
	// Results below threshold should pass through unchanged
	short := "file contents: hello world\n"
	if got := truncateToolResultForSubAgent("read_file", short); got != short {
		t.Errorf("short read_file should pass through, got len=%d", len(got))
	}
	if got := truncateToolResultForSubAgent("bash", "ok\n"); got != "ok\n" {
		t.Errorf("short bash should pass through, got %q", got)
	}
	if got := truncateToolResultForSubAgent("unknown_tool", short); got != short {
		t.Errorf("unknown tool should pass through unchanged")
	}
}

func TestTruncateToolResult_EmptyResult(t *testing.T) {
	if got := truncateToolResultForSubAgent("read_file", ""); got != "" {
		t.Errorf("empty result should stay empty, got %q", got)
	}
}

func TestTruncateToolResult_ReadFile_LargeFile(t *testing.T) {
	// Generate a 500-line file
	var lines []string
	for i := 1; i <= 500; i++ {
		lines = append(lines, fmt.Sprintf("line %d: some code content here", i))
	}
	input := strings.Join(lines, "\n")

	result := truncateToolResultForSubAgent("read_file", input)

	// Should contain head lines
	if !strings.Contains(result, "line 1:") {
		t.Error("should contain first line")
	}
	if !strings.Contains(result, "line 80:") {
		t.Error("should contain line 80 (head boundary)")
	}

	// Should contain omission hint
	if !strings.Contains(result, "lines omitted") {
		t.Error("should contain omission hint")
	}
	if !strings.Contains(result, "offset=") {
		t.Error("should contain actionable offset hint")
	}

	// Should contain tail lines
	if !strings.Contains(result, "line 500:") {
		t.Error("should contain last line")
	}
	if !strings.Contains(result, "line 471:") {
		t.Error("should contain start of tail (500-30+1=471)")
	}

	// Should NOT contain middle lines
	if strings.Contains(result, "line 200:") {
		t.Error("should NOT contain middle lines")
	}

	// Should be significantly shorter than input
	if len(result) > len(input)/2 {
		t.Errorf("truncated result should be <50%% of input: got %d vs %d", len(result), len(input))
	}
}

func TestTruncateToolResult_Bash_ErrorPrioritized(t *testing.T) {
	// Generate bash output with errors buried in the middle
	var lines []string
	for i := 1; i <= 200; i++ {
		if i == 100 {
			lines = append(lines, "src/main.go:45:12: error: undefined variable 'foo'")
		} else if i == 150 {
			lines = append(lines, "FAILED: test_login (assertion error)")
		} else {
			lines = append(lines, fmt.Sprintf("building package %d...", i))
		}
	}
	input := strings.Join(lines, "\n")

	result := truncateToolResultForSubAgent("bash", input)

	// Error lines should be prioritized at the top
	if !strings.Contains(result, "Error/failure lines") {
		t.Error("should have error priority section")
	}
	if !strings.Contains(result, "undefined variable") {
		t.Error("should contain the error line")
	}
	if !strings.Contains(result, "FAILED") {
		t.Error("should contain the failure line")
	}

	// Should have head + tail structure
	if !strings.Contains(result, "building package 1") {
		t.Error("should contain head (first line)")
	}
	if !strings.Contains(result, "truncated") {
		t.Error("should indicate truncation")
	}
}

func TestTruncateToolResult_ListDir_LargeDirectory(t *testing.T) {
	var lines []string
	for i := 1; i <= 300; i++ {
		if i%10 == 0 {
			lines = append(lines, fmt.Sprintf("dir_%d/", i))
		} else {
			lines = append(lines, fmt.Sprintf("file_%d.go", i))
		}
	}
	input := strings.Join(lines, "\n")

	result := truncateToolResultForSubAgent("list_directory", input)

	// Should show first 100 entries
	if !strings.Contains(result, "file_1.go") {
		t.Error("should contain first entry")
	}

	// Should have summary
	if !strings.Contains(result, "more entries omitted") {
		t.Error("should have omission summary")
	}
	if !strings.Contains(result, "directories") && !strings.Contains(result, "files") {
		t.Error("should have dir/file count summary")
	}
}

func TestTruncateToolResult_Search_ManyMatches(t *testing.T) {
	var lines []string
	for i := 1; i <= 100; i++ {
		lines = append(lines, fmt.Sprintf("src/pkg%d/handler.go:42: matched pattern", i))
		lines = append(lines, "  context line above")
		lines = append(lines, "  context line below")
	}
	input := strings.Join(lines, "\n")

	result := truncateToolResultForSubAgent("Glob", input)

	// Should keep first 30 matches
	if !strings.Contains(result, "src/pkg1/") {
		t.Error("should contain first match")
	}

	// Should indicate more matches available
	if !strings.Contains(result, "more matches omitted") {
		t.Error("should indicate remaining matches")
	}

	// Should be shorter than input
	if len(result) >= len(input) {
		t.Errorf("should be shorter: got %d >= %d", len(result), len(input))
	}
}

func TestTruncateToolResult_GitDiff_LargeDiff(t *testing.T) {
	var lines []string
	lines = append(lines, "diff --git a/file.go b/file.go")
	lines = append(lines, "--- a/file.go")
	lines = append(lines, "+++ b/file.go")
	for i := 1; i <= 500; i++ {
		lines = append(lines, fmt.Sprintf("+added line %d with some realistic code content that makes this longer", i))
	}
	input := strings.Join(lines, "\n")

	result := truncateToolResultForSubAgent("git_diff", input)

	// Should contain diff header
	if !strings.Contains(result, "diff --git") {
		t.Error("should contain diff header")
	}

	// Should have truncation notice
	if !strings.Contains(result, "more diff lines omitted") {
		t.Error("should indicate truncation")
	}

	// Should be shorter
	if len(result) >= len(input) {
		t.Errorf("should be shorter: got %d >= %d", len(result), len(input))
	}
}

func TestTruncateToolResult_Bash_NoErrors_StillTruncates(t *testing.T) {
	// Large output with no error lines
	var lines []string
	for i := 1; i <= 200; i++ {
		lines = append(lines, fmt.Sprintf("processing item %d: ok", i))
	}
	input := strings.Join(lines, "\n")

	result := truncateToolResultForSubAgent("bash", input)

	// Should NOT have error section
	if strings.Contains(result, "Error/failure lines") {
		t.Error("should not have error section when no errors present")
	}

	// Should still truncate (head + tail)
	if !strings.Contains(result, "truncated") {
		t.Error("should indicate truncation")
	}
	if !strings.Contains(result, "processing item 1") {
		t.Error("should contain head")
	}
	if !strings.Contains(result, "processing item 200") {
		t.Error("should contain tail")
	}
}
