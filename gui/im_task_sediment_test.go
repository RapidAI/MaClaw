package main

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/memory"
)

func TestBuildStandaloneTaskPath_DifferentTitles(t *testing.T) {
	dataDir := filepath.Join("C:", "Users", "test", ".maclaw", "data")

	p1 := buildStandaloneTaskPath(dataDir, "find recent agent papers")
	p2 := buildStandaloneTaskPath(dataDir, "translate paper to Chinese")
	p3 := buildStandaloneTaskPath(dataDir, "find recent agent papers")

	if p1 == "" || p2 == "" {
		t.Fatal("standalone paths should not be empty")
	}
	if p1 == p2 {
		t.Error("different titles should produce different paths")
	}
	if p1 != p3 {
		t.Error("same title should produce the same path")
	}
	if !strings.HasPrefix(p1, filepath.Join(dataDir, "tasks")) {
		t.Errorf("path should be under dataDir/tasks/, got: %s", p1)
	}
}

func TestBuildStandaloneTaskPath_EmptyDataDir(t *testing.T) {
	if got := buildStandaloneTaskPath("", "some task"); got != "" {
		t.Errorf("empty dataDir should return empty path, got: %s", got)
	}
}

func TestBuildSedimentTitle(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"Build a task management filter with descriptive output titles", "Build a task management filter with descriptive"},
		{"short title", "short title"},
		{"", ""},
		{"# heading", "heading"},
	}
	for _, tt := range tests {
		if got := buildSedimentTitle(tt.input); got != tt.expected {
			t.Errorf("buildSedimentTitle(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestBuildSedimentTitleFromConversationUsesResultForGenericRequest(t *testing.T) {
	got := buildSedimentTitleFromConversation("review/fix/optimize", "Added output-backed task filtering and tests")
	if got != "Added output-backed task filtering and tests" {
		t.Fatalf("generic title = %q", got)
	}
	if got := buildSedimentTitleFromConversation("Improve task titles", "Updated tests"); got != "Improve task titles" {
		t.Fatalf("specific title = %q", got)
	}
}

func TestConversationHasTangibleOutput(t *testing.T) {
	history := []agent.ConversationEntry{
		{Role: "user", Content: "review/fix/optimize"},
		{Role: "assistant", ToolCalls: []interface{}{map[string]interface{}{
			"id":       "call-1",
			"function": map[string]interface{}{"name": "edit_file"},
		}}},
		{Role: "tool", ToolCallID: "call-1", Content: "Updated gui/im_task_sediment.go"},
		{Role: "assistant", Content: "Done."},
	}
	tools, summary := conversationHasTangibleOutput(history)
	if !tools["edit_file"] {
		t.Fatalf("tools = %#v, want edit_file", tools)
	}
	if summary != "Updated gui/im_task_sediment.go" {
		t.Fatalf("summary = %q", summary)
	}
}

func TestConversationHasTangibleOutputRejectsReadOnlyAndFailedTools(t *testing.T) {
	tests := []struct {
		name    string
		history []agent.ConversationEntry
	}{
		{
			name: "read only",
			history: []agent.ConversationEntry{
				{Role: "user", Content: "look up docs"},
				{Role: "assistant", ToolCalls: []interface{}{map[string]interface{}{"id": "r1", "function": map[string]interface{}{"name": "read_file"}}}},
				{Role: "tool", ToolCallID: "r1", Content: "package main"},
			},
		},
		{
			name: "read only shell",
			history: []agent.ConversationEntry{
				{Role: "user", Content: "what time is it"},
				{Role: "assistant", ToolCalls: []interface{}{map[string]interface{}{"id": "b1", "function": map[string]interface{}{"name": "bash", "arguments": `{"cmd":"date"}`}}}},
				{Role: "tool", ToolCallID: "b1", Content: "Thu May 21 23:10:00 CST 2026"},
			},
		},
		{
			name: "failed output",
			history: []agent.ConversationEntry{
				{Role: "user", Content: "write file"},
				{Role: "assistant", ToolCalls: []interface{}{map[string]interface{}{"id": "w1", "function": map[string]interface{}{"name": "write_file"}}}},
				{Role: "tool", ToolCallID: "w1", Content: `{"ok":false,"error":"denied"}`},
			},
		},
		{
			name: "failed outcome",
			history: []agent.ConversationEntry{
				{Role: "user", Content: "write file"},
				{Role: "assistant", ToolCalls: []interface{}{map[string]interface{}{"id": "w2", "function": map[string]interface{}{"name": "write_file"}}}},
				{Role: "tool", ToolCallID: "w2", Content: "all good", ToolOutcome: toolOutcomeFailed.String()},
			},
		},
		{
			name: "missing result",
			history: []agent.ConversationEntry{
				{Role: "user", Content: "write file"},
				{Role: "assistant", ToolCalls: []interface{}{map[string]interface{}{"id": "w1", "function": map[string]interface{}{"name": "write_file"}}}},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tools, _ := conversationHasTangibleOutput(tt.history)
			if len(tools) != 0 {
				t.Fatalf("tools = %#v, want none", tools)
			}
		})
	}
}

func TestConversationHasTangibleOutputAcceptsToolResultRole(t *testing.T) {
	history := []agent.ConversationEntry{
		{Role: "user", Content: "write file"},
		{Role: "assistant", ToolCalls: []interface{}{map[string]interface{}{"id": "w1", "function": map[string]interface{}{"name": "write_file"}}}},
		{Role: "tool_result", ToolCallID: "w1", Content: "wrote report.md", ToolOutcome: toolOutcomeSucceeded.String()},
	}
	tools, summary := conversationHasTangibleOutput(history)
	if !tools["write_file"] {
		t.Fatalf("tools = %#v, want write_file", tools)
	}
	if summary != "wrote report.md" {
		t.Fatalf("summary = %q", summary)
	}
}

func TestConversationHasTangibleOutputAcceptsEditLines(t *testing.T) {
	history := []agent.ConversationEntry{
		{Role: "user", Content: "fix task filtering"},
		{Role: "assistant", ToolCalls: []interface{}{map[string]interface{}{"id": "e1", "function": map[string]interface{}{"name": "edit_lines"}}}},
		{Role: "tool_result", ToolCallID: "e1", Content: "edited gui/im_task_sediment.go", ToolOutcome: toolOutcomeSucceeded.String()},
	}
	tools, summary := conversationHasTangibleOutput(history)
	if !tools["edit_lines"] {
		t.Fatalf("tools = %#v, want edit_lines", tools)
	}
	if summary != "edited gui/im_task_sediment.go" {
		t.Fatalf("summary = %q", summary)
	}
}

func TestConversationHasTangibleOutputFiltersActionOnlyTools(t *testing.T) {
	tests := []struct {
		name      string
		toolName  string
		arguments string
		want      bool
	}{
		{name: "manage skill list", toolName: "manage_skill", arguments: `{"action":"list"}`},
		{name: "manage skill search", toolName: "manage_skill", arguments: `{"action":"search","query":"pdf"}`},
		{name: "manage skill run", toolName: "manage_skill", arguments: `{"action":"run","name":"make-pdf"}`, want: true},
		{name: "manage skill validate read only", toolName: "manage_skill", arguments: `{"action":"validate","name":"make-pdf"}`},
		{name: "manage skill validate autofix", toolName: "manage_skill", arguments: `{"action":"validate","name":"make-pdf","auto_fix":true}`, want: true},
		{name: "schedule list", toolName: "manage_schedule", arguments: `{"action":"list"}`},
		{name: "schedule create", toolName: "manage_schedule", arguments: `{"action":"create","name":"daily"}`, want: true},
		{name: "template list", toolName: "manage_template", arguments: `{"action":"list"}`},
		{name: "template create", toolName: "manage_template", arguments: `{"action":"create","name":"brief"}`, want: true},
		{name: "office read excel", toolName: "office", arguments: `{"action":"read_excel","path":"data.xlsx"}`},
		{name: "office write excel", toolName: "office", arguments: `{"action":"write_excel","path":"data.xlsx"}`, want: true},
		{name: "office generate pdf", toolName: "office", arguments: `{"action":"generate_pdf","path":"report.md"}`, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			history := []agent.ConversationEntry{
				{Role: "user", Content: tt.name},
				{Role: "assistant", ToolCalls: []interface{}{map[string]interface{}{"id": "a1", "function": map[string]interface{}{"name": tt.toolName, "arguments": tt.arguments}}}},
				{Role: "tool", ToolCallID: "a1", Content: "operation completed", ToolOutcome: toolOutcomeSucceeded.String()},
			}
			tools, _ := conversationHasTangibleOutput(history)
			if got := tools[tt.toolName]; got != tt.want {
				t.Fatalf("tools[%q] = %v, want %v (all=%#v)", tt.toolName, got, tt.want, tools)
			}
		})
	}
}

func TestConversationHasTangibleOutputAcceptsProductiveShell(t *testing.T) {
	history := []agent.ConversationEntry{
		{Role: "user", Content: "run the focused tests"},
		{Role: "assistant", ToolCalls: []interface{}{map[string]interface{}{
			"id": "test-1",
			"function": map[string]interface{}{
				"name":      "bash",
				"arguments": `{"cmd":"go test ./corelib/memory -run TestProjectIndex -count=1"}`,
			},
		}}},
		{Role: "tool", ToolCallID: "test-1", Content: "ok github.com/RapidAI/CodeClaw/corelib/memory 0.3s"},
	}
	tools, summary := conversationHasTangibleOutput(history)
	if !tools["bash"] {
		t.Fatalf("tools = %#v, want bash", tools)
	}
	if !strings.Contains(summary, "ok github.com") {
		t.Fatalf("summary = %q", summary)
	}
}

func TestConversationHasTangibleOutputPrefersAssistantSummary(t *testing.T) {
	history := []agent.ConversationEntry{
		{Role: "user", Content: "review/fix/optimize"},
		{Role: "assistant", ToolCalls: []interface{}{map[string]interface{}{
			"id":       "patch-1",
			"function": map[string]interface{}{"name": "edit_file"},
		}}},
		{Role: "tool", ToolCallID: "patch-1", Content: "Success. Updated the following files:\nM gui/im_task_sediment.go"},
		{Role: "assistant", Content: "Recent tasks now require tangible output and use result-based titles."},
	}
	_, summary := conversationHasTangibleOutput(history)
	if summary != "Recent tasks now require tangible output and use result-based titles." {
		t.Fatalf("summary = %q", summary)
	}
	title := buildSedimentTitleFromConversation("review/fix/optimize", summary)
	if !strings.Contains(title, "Recent tasks now require tangible output") {
		t.Fatalf("title = %q", title)
	}
}

func TestConversationHasTangibleOutputSummarizesPatchOutput(t *testing.T) {
	history := []agent.ConversationEntry{
		{Role: "user", Content: "review/fix/optimize"},
		{Role: "assistant", ToolCalls: []interface{}{map[string]interface{}{
			"id":       "patch-1",
			"function": map[string]interface{}{"name": "edit_file"},
		}}},
		{Role: "tool", ToolCallID: "patch-1", Content: "Success. Updated the following files:\nM gui/im_task_sediment.go"},
		{Role: "assistant", Content: "Done."},
	}
	_, summary := conversationHasTangibleOutput(history)
	if summary != "Updated gui/im_task_sediment.go" {
		t.Fatalf("summary = %q", summary)
	}
	if title := buildSedimentTitleFromConversation("review/fix/optimize", summary); title != "Updated gui/im_task_sediment.go" {
		t.Fatalf("title = %q", title)
	}
}

func TestFirstSedimentResultLineSkipsGenericOperationCompleted(t *testing.T) {
	if got := firstSedimentResultLine("operation completed"); got != "" {
		t.Fatalf("summary = %q, want empty", got)
	}
}

func TestLatestSedimentTurn(t *testing.T) {
	history := []agent.ConversationEntry{
		{Role: "user", Content: "first task"},
		{Role: "assistant", ToolCalls: []interface{}{map[string]interface{}{"id": "old", "function": map[string]interface{}{"name": "edit_file"}}}},
		{Role: "tool", ToolCallID: "old", Content: "old result"},
		{Role: "user", Content: "continue"},
		{Role: "assistant", Content: "No changes."},
	}
	req, turn := latestSedimentTurn(history)
	if req != "continue" {
		t.Fatalf("req = %q", req)
	}
	if !reflect.DeepEqual(turn, history[3:]) {
		t.Fatalf("turn = %#v, want latest user turn", turn)
	}
}

func TestBuildSedimentTags(t *testing.T) {
	standalone := filepath.Join("C:", "Users", "test", ".maclaw", "data", "tasks", "abc")
	project := filepath.Join("D:", "workprj", "aicoder")
	tags := buildSedimentTags(standalone, project, map[string]bool{"write_file": true, "edit_file": true})
	joined := strings.Join(tags, "\n")
	for _, want := range []string{"task_sediment", "auto", "tangible_output", standalone, project, "output_tool:edit_file", "output_tool:write_file"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("tags %v missing %q", tags, want)
		}
	}
}

func TestStandaloneTaskPath_PassesLooksLikeProjectPath(t *testing.T) {
	for _, dataDir := range []string{"C:\\Users\\test\\.maclaw\\data", "/home/user/.maclaw/data"} {
		p := buildStandaloneTaskPath(dataDir, "paper summary task")
		if p == "" {
			t.Fatal("path should not be empty")
		}
		fwd := strings.ReplaceAll(p, "\\", "/")
		if !memory.LooksLikeFilePath(fwd) {
			t.Errorf("path should pass LooksLikeFilePath, got: %s", p)
		}
		if !strings.Contains(fwd, "/tasks/") {
			t.Errorf("path should contain /tasks/ segment, got: %s", fwd)
		}
	}
}

func TestStandaloneTaskPath_HashStability(t *testing.T) {
	dataDir := "/home/user/.maclaw/data"
	first := buildStandaloneTaskPath(dataDir, "stable title")
	for i := 0; i < 100; i++ {
		if got := buildStandaloneTaskPath(dataDir, "stable title"); got != first {
			t.Fatalf("hash not stable: call %d returned %q, expected %q", i, got, first)
		}
	}
}

func TestStandaloneTaskPath_InferProjectPathRequiresOutputTag(t *testing.T) {
	dataDir := "/home/user/.maclaw/data"
	standalonePath := buildStandaloneTaskPath(dataDir, "paper summary")
	entry := memory.Entry{
		Content:    "Task: paper summary\nResult: wrote report",
		Title:      "Paper Summary Report",
		Category:   memory.CategoryProjectKnowledge,
		SourceType: "task_sediment",
		Tags:       []string{"task_sediment", "tangible_output", "output_tool:write_file", standalonePath, "/home/user/projects/aicoder"},
	}

	pi := memory.NewProjectIndex()
	pi.IndexEntry(&entry)

	rec := pi.Get(standalonePath)
	if rec == nil {
		t.Fatalf("entry should be indexed under standalone path; recent=%v", pi.ListRecent(10))
	}
	if rec.Name != "Paper Summary Report" || !rec.HasOutput {
		t.Fatalf("record = %+v, want output task", rec)
	}
}
