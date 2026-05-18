package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	agent "github.com/RapidAI/CodeClaw/corelib/agent"
	corememory "github.com/RapidAI/CodeClaw/corelib/memory"
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

func TestApplyHistoryCompression_GroupAligned_NeverOrphansToolMessages(t *testing.T) {
	// Reproduces the group-splitting scenario: preserve_last lands between
	// an assistant(tool_calls) and its tool result.
	//
	// History layout (12 entries):
	//   [0]  user
	//   [1]  assistant "step 1"
	//   [2]  assistant "step 2"
	//   [3]  assistant "step 3"
	//   [4]  assistant "step 4"
	//   [5]  assistant (tool_calls: [{id: "call_A", bash}])
	//   [6]  tool (tcid: "call_A")                          ← group with [5]
	//   [7]  assistant ("PDF done!")
	//   [8]  assistant (tool_calls: [{id: "call_B", compress_context}])
	//   [9]  tool (tcid: "call_B")                          ← group with [8]
	//   [10] assistant ("Compressed.")
	//   [11] assistant ("Waiting for input.")
	//
	// With preserve_last=6, raw tailStart = 12-6 = 6.
	// Entry [6] is tool(tcid=call_A) — MIDDLE of group [5,6].
	// Without group alignment: tail = [6..11], head = [0], dropped = [1..5].
	// Entry [6] is in tail but parent [5] is dropped → ORPHAN!
	// With group alignment: tailStart adjusted to 5 (group start).
	// Tail = [5..11], head = [0], dropped = [1..4] → no orphan.

	bashToolCalls := []map[string]interface{}{
		{"id": "call_A", "type": "function", "function": map[string]interface{}{"name": "bash", "arguments": "{}"}},
	}
	compressToolCalls := []map[string]interface{}{
		{"id": "call_B", "type": "function", "function": map[string]interface{}{"name": "compress_context", "arguments": "{}"}},
	}

	history := []agent.ConversationEntry{
		{Role: "user", Content: "Build a game"},                                        // [0]
		{Role: "assistant", Content: "Step 1."},                                        // [1]
		{Role: "assistant", Content: "Step 2."},                                        // [2]
		{Role: "assistant", Content: "Step 3."},                                        // [3]
		{Role: "assistant", Content: "Step 4."},                                        // [4]
		{Role: "assistant", Content: "Checking file size.", ToolCalls: bashToolCalls},  // [5]
		{Role: "tool", Content: "743.6KB", ToolCallID: "call_A"},                       // [6]
		{Role: "assistant", Content: "PDF done!"},                                      // [7]
		{Role: "assistant", Content: "Let me compress.", ToolCalls: compressToolCalls}, // [8]
		{Role: "tool", Content: "Compression queued.", ToolCallID: "call_B"},           // [9]
		{Role: "assistant", Content: "Compressed."},                                    // [10]
		{Role: "assistant", Content: "Waiting for input."},                             // [11]
	}

	req := &contextCompressionRequest{
		Summary:       "Built a game and generated PDF.",
		PreserveLastN: 6,
	}

	result := applyHistoryCompression(history, req)

	// Verify structural invariant: no orphaned tool entries.
	declaredIDs := make(map[string]bool)
	for _, e := range result {
		if e.Role == "assistant" && e.ToolCalls != nil {
			for _, tc := range e.ToolCalls.([]map[string]interface{}) {
				if id, ok := tc["id"].(string); ok {
					declaredIDs[id] = true
				}
			}
		}
	}
	for i, e := range result {
		if e.Role == "tool" {
			if !declaredIDs[e.ToolCallID] {
				t.Errorf("orphaned tool entry at index %d: tool_call_id=%q has no parent assistant(tool_calls)", i, e.ToolCallID)
			}
		}
	}

	// Verify the summary was inserted.
	hasSummary := false
	for _, e := range result {
		if e.Role == "system" {
			if s, ok := e.Content.(string); ok && strings.Contains(s, "Built a game") {
				hasSummary = true
			}
		}
	}
	if !hasSummary {
		t.Error("summary system message not found in result")
	}
}

func TestApplyHistoryCompression_PreserveLast4_ExactBugScenario(t *testing.T) {
	// Exact reproduction of the persisted conversation that caused the
	// DeepSeek HTTP 400 error. The LLM called compress_context with
	// preserve_last=4. The last 5 history entries before compression were:
	//
	//   [N-5] assistant (tool_calls: [{id: "call_sFf", bash}])
	//   [N-4] tool (tcid: "call_sFf", content: "743.6...")
	//   [N-3] assistant ("PDF 综述已生成完毕！...")
	//   [N-2] assistant (tool_calls: [{id: "call_fqO", compress_context}])
	//   [N-1] tool (tcid: "call_fqO", content: "✅ 上下文压缩已排队")
	//
	// With preserve_last=4, raw tailStart = N-4.
	// Entry [N-4] is tool(tcid=call_sFf) — MIDDLE of group [N-5, N-4].
	// Without group alignment: parent [N-5] is dropped → orphan!
	// With group alignment: tailStart adjusted to N-5 → no orphan.

	bashToolCalls := []map[string]interface{}{
		{"id": "call_sFf", "type": "function", "function": map[string]interface{}{"name": "bash", "arguments": "{}"}},
	}
	compressToolCalls := []map[string]interface{}{
		{"id": "call_fqO", "type": "function", "function": map[string]interface{}{"name": "compress_context", "arguments": "{}"}},
	}

	// Build a history with enough entries to trigger compression.
	history := make([]agent.ConversationEntry, 0, 20)
	history = append(history, agent.ConversationEntry{Role: "user", Content: "最近一周 HuggingFace 论文"})
	// Add filler entries to make len > 6.
	for i := 0; i < 10; i++ {
		history = append(history, agent.ConversationEntry{Role: "assistant", Content: fmt.Sprintf("Working on step %d...", i)})
	}
	// The critical tail entries:
	history = append(history, agent.ConversationEntry{Role: "assistant", Content: "Checking file size.", ToolCalls: bashToolCalls})
	history = append(history, agent.ConversationEntry{Role: "tool", Content: "743.6123046875\r\n", ToolCallID: "call_sFf"})
	history = append(history, agent.ConversationEntry{Role: "assistant", Content: "PDF 综述已生成完毕！"})
	history = append(history, agent.ConversationEntry{Role: "assistant", Content: "已完成 PDF 综述的生成与交付。", ToolCalls: compressToolCalls})
	history = append(history, agent.ConversationEntry{Role: "tool", Content: "✅ 上下文压缩已排队。", ToolCallID: "call_fqO"})

	req := &contextCompressionRequest{
		Summary:       "已完成 HuggingFace 一周 LLM Agent 论文综述 PDF 生成。",
		PreserveLastN: 4,
	}

	result := applyHistoryCompression(history, req)

	// Verify structural invariant: no orphaned tool entries.
	declaredIDs := make(map[string]bool)
	for _, e := range result {
		if e.Role == "assistant" && e.ToolCalls != nil {
			for _, tc := range e.ToolCalls.([]map[string]interface{}) {
				if id, ok := tc["id"].(string); ok {
					declaredIDs[id] = true
				}
			}
		}
	}
	for i, e := range result {
		if e.Role == "tool" {
			if !declaredIDs[e.ToolCallID] {
				t.Errorf("orphaned tool entry at index %d: tool_call_id=%q has no parent assistant(tool_calls)", i, e.ToolCallID)
			}
		}
	}

	// The result should have: user + summary + (group-aligned tail).
	// With group alignment, tailStart moves from N-4 (tool) to N-5 (assistant),
	// so the tail has 5 entries instead of 4.
	t.Logf("result has %d entries:", len(result))
	for i, e := range result {
		tc := ""
		if e.ToolCalls != nil {
			tc = " [has tool_calls]"
		}
		tcid := ""
		if e.ToolCallID != "" {
			tcid = fmt.Sprintf(" [tcid=%s]", e.ToolCallID)
		}
		t.Logf("  [%d] role=%s%s%s", i, e.Role, tc, tcid)
	}
}

func TestPersistLastCompressionSummaryWritesSourceRef(t *testing.T) {
	store, err := corememory.NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Stop()

	summary := strings.Repeat("checkpoint detail ", 90) + "COMPRESS_SENTINEL_AT_END"
	persistLastCompressionSummary(store, "user/A", summary)

	entries := store.Entries()
	if len(entries) != 1 {
		t.Fatalf("expected 1 memory entry, got %d", len(entries))
	}
	entry := entries[0]
	if entry.SourceType != "context_checkpoint_ref" || entry.SourceURL == "" {
		t.Fatalf("expected context checkpoint source ref, got type=%q url=%q", entry.SourceType, entry.SourceURL)
	}
	if entry.OwnerID != "user/A" {
		t.Fatalf("OwnerID = %q, want user/A", entry.OwnerID)
	}
	if len([]rune(entry.Content)) > memoryRefPreviewRunes {
		t.Fatalf("entry content was not preview-limited: %d runes", len([]rune(entry.Content)))
	}
	data, err := os.ReadFile(entry.SourceURL)
	if err != nil {
		t.Fatalf("read source ref: %v", err)
	}
	if !strings.Contains(string(data), "COMPRESS_SENTINEL_AT_END") {
		t.Fatalf("source ref did not preserve full summary")
	}
}
