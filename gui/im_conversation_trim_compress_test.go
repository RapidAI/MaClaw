package main

import (
	"fmt"
	"strings"
	"testing"

	agent "github.com/RapidAI/CodeClaw/corelib/agent"
)

// --- Tests for compressToolResultSemantic ---

func TestCompressToolResultSemantic_ShortInput_NoChange(t *testing.T) {
	input := "total 3\ndrwxr-xr-x 2 user user 4096 Apr 29 10:00 .\ndrwxr-xr-x 3 user user 4096 Apr 29 09:00 .."
	result := compressToolResultSemantic("bash", input)
	if result != input {
		t.Errorf("short input should not be modified")
	}
}

func TestCompressToolResultSemantic_NonTargetTool_ShortInput(t *testing.T) {
	input := "file written successfully"
	result := compressToolResultSemantic("write_file", input)
	if result != input {
		t.Errorf("short input should not be modified")
	}
}

func TestCompressToolResultSemantic_DeduplicateIdenticalLines(t *testing.T) {
	var lines []string
	lines = append(lines, "Building project...")
	for i := 0; i < 20; i++ {
		lines = append(lines, "warning: unused variable 'x'")
	}
	lines = append(lines, "Build complete.")
	input := strings.Join(lines, "\n")

	result := compressToolResultSemantic("bash", input)

	if !strings.Contains(result, "重复 19 行") {
		t.Errorf("expected deduplication marker, got:\n%s", result)
	}
	if len(result) > len(input)/2 {
		t.Errorf("compressed should be shorter: input=%d result=%d", len(input), len(result))
	}
}

func TestCompressToolResultSemantic_CollapseHomogeneousBlock(t *testing.T) {
	var lines []string
	lines = append(lines, "Running tests...")
	for i := 0; i < 30; i++ {
		lines = append(lines, fmt.Sprintf("PASS test_%d (0.%ds)", i, i))
	}
	lines = append(lines, "All tests passed.")
	input := strings.Join(lines, "\n")

	result := compressToolResultSemantic("bash", input)

	if !strings.Contains(result, "省略") {
		t.Errorf("expected block collapse marker, got:\n%s", result)
	}
	if !strings.Contains(result, "PASS test_0") {
		t.Errorf("should keep first line of block")
	}
	if !strings.Contains(result, "PASS test_29") {
		t.Errorf("should keep last line of block")
	}
}

func TestCompressToolResultSemantic_MixedContent_PreservesUnique(t *testing.T) {
	var lines []string
	lines = append(lines, "Step 1: Creating files...")
	lines = append(lines, "Created src/main.go")
	lines = append(lines, "Created src/utils.go")
	for i := 0; i < 15; i++ {
		lines = append(lines, "npm WARN deprecated package@1.0.0: this package is no longer maintained")
	}
	lines = append(lines, "Step 2: Installing dependencies...")
	lines = append(lines, "Added 150 packages")
	input := strings.Join(lines, "\n")

	result := compressToolResultSemantic("bash", input)

	if !strings.Contains(result, "Created src/main.go") {
		t.Errorf("unique lines should be preserved")
	}
	if !strings.Contains(result, "Added 150 packages") {
		t.Errorf("unique lines should be preserved")
	}
	if !strings.Contains(result, "重复") {
		t.Errorf("should contain deduplication marker")
	}
}

func TestCompressToolResultSemantic_StructurallyRepetitive_Collapsed(t *testing.T) {
	var lines []string
	for i := 0; i < 50; i++ {
		lines = append(lines, fmt.Sprintf("ok  pkg_%d", i))
	}
	input := strings.Join(lines, "\n")

	result := compressToolResultSemantic("read_file", input)

	if !strings.Contains(result, "省略") {
		t.Errorf("structurally repetitive lines should be collapsed, got:\n%s", result)
	}
}

func TestCompressToolResultSemantic_VariedContent_NotCollapsed(t *testing.T) {
	lines := []string{
		"error: missing semicolon at line 5",
		"error: undefined variable 'longVariableNameThatMakesThisLineMuchLongerThanTheOthers' at line 12 column 45 in file src/main.go",
		"error: type mismatch",
		"error: cannot convert string to int in expression foo(bar, baz, qux, quux, corge, grault, garply)",
		"error: x",
		"error: duplicate declaration of 'MyVeryLongClassName' in package 'com.example.myapp.services.impl'",
		"error: missing return statement in function 'calculateTotalRevenueForQuarterlyReport'",
		"error: a",
		"error: syntax error near unexpected token '(' at line 234 column 12 in file /very/long/path/to/source.go",
		"error: b",
		"error: import cycle detected between packages 'auth', 'middleware', 'handlers', 'services', 'repository'",
		"error: c",
	}
	input := strings.Join(lines, "\n")

	result := compressToolResultSemantic("bash", input)

	if strings.Contains(result, "省略") {
		t.Errorf("varied-length lines should NOT be collapsed, got:\n%s", result)
	}
}

// --- Tests for extractLinePrefix ---

func TestExtractLinePrefix_CommonPatterns(t *testing.T) {
	tests := []struct {
		line     string
		expected string
	}{
		{"PASS test_foo (0.1s)", "PASS "},
		{"warning: unused variable", "warning: "},
		{"error: compilation failed", "error: "},
		{"ok\tpackage/name", "ok\t"},
		{"  ✓ should work", ""},
		{"ab", ""},
		{"", ""},
	}
	for _, tt := range tests {
		result := extractLinePrefix(tt.line)
		if result != tt.expected {
			t.Errorf("extractLinePrefix(%q) = %q, want %q", tt.line, result, tt.expected)
		}
	}
}

// --- Tests for applyHistoryCompression ---

func TestApplyHistoryCompression_Basic(t *testing.T) {
	history := []agent.ConversationEntry{
		{Role: "user", Content: "Build a game"},
		{Role: "assistant", Content: "I'll create the files."},
		{Role: "tool", Content: "file created", ToolCallID: "1"},
		{Role: "assistant", Content: "Now testing."},
		{Role: "tool", Content: "tests passed", ToolCallID: "2"},
		{Role: "assistant", Content: "Let me compress."},
		{Role: "tool", Content: "compressed", ToolCallID: "3"},
		{Role: "assistant", Content: "Continuing..."},
	}

	req := &contextCompressionRequest{
		Summary:       "Created game files and ran tests. All passed.",
		PreserveLastN: 2,
	}

	result := applyHistoryCompression(history, req)

	// user + summary + last 2 = 4
	if len(result) != 4 {
		t.Errorf("expected 4 entries, got %d", len(result))
		for i, e := range result {
			t.Logf("  [%d] role=%s", i, e.Role)
		}
	}

	if result[0].Role != "user" {
		t.Errorf("first entry should be user, got %s", result[0].Role)
	}

	if result[1].Role != "system" {
		t.Errorf("second entry should be summary system msg, got %s", result[1].Role)
	}
	summaryStr := fmt.Sprintf("%v", result[1].Content)
	if !strings.Contains(summaryStr, "Created game files") {
		t.Errorf("summary should contain the provided text, got: %s", summaryStr)
	}

	if result[3].Role != "assistant" {
		t.Errorf("last entry should be assistant, got %s", result[3].Role)
	}
}

func TestApplyHistoryCompression_TooFewEntries_NoChange(t *testing.T) {
	history := []agent.ConversationEntry{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi"},
		{Role: "user", Content: "bye"},
	}
	req := &contextCompressionRequest{Summary: "test", PreserveLastN: 4}
	result := applyHistoryCompression(history, req)
	if len(result) != len(history) {
		t.Errorf("should not compress: got %d, want %d", len(result), len(history))
	}
}

func TestApplyHistoryCompression_NilRequest_NoChange(t *testing.T) {
	history := []agent.ConversationEntry{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi"},
	}
	result := applyHistoryCompression(history, nil)
	if len(result) != len(history) {
		t.Errorf("nil request should not change history")
	}
}
