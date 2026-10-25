package guiapp

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
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

func TestTruncateToolResult_ReadFile_KeepsCodegraphSurvey(t *testing.T) {
	payload, err := json.Marshal(map[string]string{
		"backend":       "codegraph",
		"operation":     "file_symbols",
		"query":         "snake/src/Game.cpp",
		"output":        strings.Repeat("void update() {}\n", 2000),
		"continue_with": codegraphKeepUsingHint,
	})
	if err != nil {
		t.Fatal(err)
	}
	got := truncateToolResultForSubAgent("read_file", string(payload))
	if got != string(payload) || strings.Contains(got, "read_file with offset") {
		t.Fatalf("codegraph survey was turned into a byte read, len=%d", len(got))
	}
	plain := strings.Repeat("void update() {}\n", 2000)
	if truncated := truncateToolResultForSubAgent("read_file", plain); !strings.Contains(truncated, "read_file with offset") {
		t.Fatal("a plain source read should still page with offset")
	}
}

func TestCodegraphProjectionKeepsTheSource(t *testing.T) {
	output := strings.Repeat("void update() {}\n", 500) + "\n\nIndexed files in this result:\nsnake/src/Game.cpp\n"
	payload, err := json.Marshal(map[string]string{
		"backend":       "codegraph",
		"operation":     "explore",
		"query":         "snake game implementation overview",
		"output":        output,
		"continue_with": codegraphExploreRemainderHint,
	})
	if err != nil {
		t.Fatal(err)
	}
	text := string(payload)
	if codegraphProjectionLimit("read_file", text) != codegraphProjectedResultLimit {
		t.Fatal("a codegraph survey returned through read_file used the short projector")
	}
	if codegraphProjectionLimit(codeNavigationToolName, text) != codegraphProjectedResultLimit {
		t.Fatal("code_navigation used the short projector")
	}
	if codegraphProjectionLimit("read_file", "void update() {}\n") != maxToolResultLen {
		t.Fatal("a plain source read changed projector budget")
	}
	sketched := projectToolResultHandle(codeNavigationToolName, "test", text, text, maxToolResultLen)
	if strings.Contains(sketched, "[json_preview]") || !strings.Contains(sketched, codegraphExploreRemainderHint) || !strings.Contains(sketched, "snake/src/Game.cpp") || !strings.Contains(sketched, "middle of this codegraph result omitted") {
		t.Fatalf("4KB code_navigation projector dropped the source:\n%s", sketched)
	}
	c := &codingSubAgentCallbacks{}
	got := c.ProjectToolResult(codeNavigationToolName, agent.ToolExecutionResult{Result: text, Outcome: agent.ToolExecutionOutcomeOK})
	if got != text {
		t.Fatalf("codegraph result under the budget was sketched or rewritten, len=%d want %d", len(got), len(text))
	}
	huge := strings.Repeat("void update() {}\n", 8000) + "\nIndexed files in this result:\nsnake/src/Game.cpp\n"
	hugePayload, err := json.Marshal(map[string]string{
		"backend":       "codegraph",
		"operation":     "explore",
		"output":        huge,
		"continue_with": codegraphExploreRemainderHint,
	})
	if err != nil {
		t.Fatal(err)
	}
	shrunk := codegraphResultPreview(string(hugePayload), 4000)
	if shrunk == "" || len(shrunk) > 4000 || !strings.Contains(shrunk, codegraphExploreRemainderHint) || !strings.Contains(shrunk, "snake/src/Game.cpp") || !strings.Contains(shrunk, "middle of this codegraph result omitted") {
		t.Fatalf("oversized preview len=%d\n%s", len(shrunk), shrunk)
	}
}

func TestListDirectoryProjectionKeepsTheCodegraphSteer(t *testing.T) {
	var b strings.Builder
	b.WriteString("directory: F:/test-prog/snake (80 entries)\n")
	var late string
	for i := 0; i < 80; i++ {
		name := fmt.Sprintf("file%02d_%s.cpp", i, strings.Repeat("x", 80))
		if i == 70 {
			late = name
		}
		fmt.Fprintf(&b, "  [file] %s (1200 bytes)\n", name)
	}
	b.WriteString("\n")
	b.WriteString(codegraphListedSourceSteer)
	b.WriteByte('\n')
	for i := 0; i < 40; i++ {
		fmt.Fprintf(&b, "snake/src/module%02d/FileName.cpp\n", i)
	}
	text := b.String()
	if strings.Count(text, "\n") > subAgentTruncListDirMaxLines {
		t.Fatalf("fixture has %d lines", strings.Count(text, "\n"))
	}
	c := &codingSubAgentCallbacks{}
	got := c.ProjectToolResult("list_directory", agent.ToolExecutionResult{Result: text, Outcome: agent.ToolExecutionOutcomeOK})
	if !strings.Contains(got, codegraphListedSourceSteer) || !strings.Contains(got, "snake/src/module00/FileName.cpp") || !strings.Contains(got, "directory: F:/test-prog/snake") {
		t.Fatalf("projected listing lost the steer:\n%s", got)
	}
	if late != "" && strings.Contains(got, late) {
		t.Fatalf("listing tail replaced the steer:\n%s", got)
	}
	short := "directory: snake (1 entries)\n  [file] Game.cpp (10 bytes)\n" + codegraphListedSourceSteer + "\nsnake/src/Game.cpp\n"
	if got := c.ProjectToolResult("list_directory", agent.ToolExecutionResult{Result: short, Outcome: agent.ToolExecutionOutcomeOK}); got != short {
		t.Fatalf("short listing was rewritten, len=%d want %d", len(got), len(short))
	}
}

func TestTruncateToolResult_ListDir_KeepsCodegraphSteer(t *testing.T) {
	var lines []string
	for i := 1; i <= 300; i++ {
		lines = append(lines, fmt.Sprintf("file_%d.go", i))
	}
	input := strings.Join(lines, "\n") + "\n" + codegraphListedSourceSteer + "\nsnake/src/Game.cpp\n"
	result := truncateToolResultForSubAgent("list_directory", input)
	if !strings.Contains(result, "file_1.go") || strings.Contains(result, "file_250.go") {
		t.Fatalf("directory head was not kept:\n%s", result)
	}
	if !strings.Contains(result, codegraphListedSourceSteer) || !strings.Contains(result, "snake/src/Game.cpp") {
		t.Fatalf("steer was truncated off the listing:\n%s", result)
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
