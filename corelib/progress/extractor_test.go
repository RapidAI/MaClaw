package progress

import (
	"testing"
)

func TestExtractMilestone_SilentTools(t *testing.T) {
	silents := []string{"read_file", "memory", "discover_tool", "task", "ask_user"}
	for _, tool := range silents {
		m := ExtractMilestone(tool, nil, true)
		if m != nil {
			t.Errorf("expected nil milestone for silent tool %q, got %v", tool, m)
		}
	}
}

func TestExtractMilestone_UnknownTool(t *testing.T) {
	m := ExtractMilestone("some_unknown_tool", nil, true)
	if m != nil {
		t.Error("expected nil milestone for unknown tool")
	}
}

func TestExtractMilestone_ExternalCodingSessionToolsIgnored(t *testing.T) {
	for _, tool := range []string{"create_session", "send_and_observe", "control_session"} {
		if m := ExtractMilestone(tool, map[string]any{"text": "go"}, true); m != nil {
			t.Fatalf("expected nil milestone for disabled external coding-session tool %q, got %v", tool, m)
		}
	}
}

func TestExtractMilestone_StaticTool(t *testing.T) {
	m := ExtractMilestone("generate_pdf", nil, true)
	if m == nil {
		t.Fatal("expected milestone for generate_pdf")
	}
	if m.Summary != "生成 PDF 文档" {
		t.Fatalf("expected '生成 PDF 文档', got %q", m.Summary)
	}
	if m.Phase != "generating" {
		t.Fatalf("expected phase 'generating', got %q", m.Phase)
	}
}

func TestExtractMilestone_WithArgs(t *testing.T) {
	m := ExtractMilestone("web_search", map[string]any{"query": "杭州天气"}, true)
	if m == nil {
		t.Fatal("expected milestone for web_search")
	}
	if m.Summary != "搜索: 杭州天气" {
		t.Fatalf("expected '搜索: 杭州天气', got %q", m.Summary)
	}
}

func TestExtractMilestone_ArgTruncation(t *testing.T) {
	longQuery := "这是一个非常非常非常非常非常非常非常非常非常非常非常非常非常长的搜索查询"
	m := ExtractMilestone("web_search", map[string]any{"query": longQuery}, true)
	if m == nil {
		t.Fatal("expected milestone")
	}
	// Should be truncated to 30 runes + "..."
	runes := []rune(m.Summary)
	// "搜索: " is 4 runes, then 30 runes of content, then "..."
	if len(runes) > 40 {
		t.Fatalf("summary too long (%d runes): %q", len(runes), m.Summary)
	}
}

func TestExtractMilestone_SSHActions(t *testing.T) {
	tests := []struct {
		action   string
		host     string
		expected string
	}{
		{"connect", "api.example.com", "连接服务器 api.example.com"},
		{"exec", "", "服务器执行: ls -la"},
		{"close", "", "断开服务器连接"},
	}

	for _, tt := range tests {
		args := map[string]any{"action": tt.action}
		if tt.host != "" {
			args["host"] = tt.host
		}
		if tt.action == "exec" {
			args["command"] = "ls -la"
		}

		m := ExtractMilestone("ssh", args, true)
		if m == nil {
			t.Fatalf("expected milestone for ssh action=%s", tt.action)
		}
		if m.Summary != tt.expected {
			t.Errorf("ssh action=%s: expected %q, got %q", tt.action, tt.expected, m.Summary)
		}
	}
}

func TestExtractMilestone_MissingArgs(t *testing.T) {
	m := ExtractMilestone("write_file", map[string]any{}, true)
	if m == nil {
		t.Fatal("expected milestone even with missing args")
	}
	if m.Summary != "生成..." {
		t.Fatalf("expected '生成...', got %q", m.Summary)
	}
}

func TestExtractMilestone_CompletedFlag(t *testing.T) {
	m := ExtractMilestone("bash", map[string]any{"command": "ls"}, false)
	if m == nil {
		t.Fatal("expected milestone")
	}
	if m.Completed {
		t.Fatal("expected Completed=false")
	}

	m = ExtractMilestone("bash", map[string]any{"command": "ls"}, true)
	if !m.Completed {
		t.Fatal("expected Completed=true")
	}
}

func TestMergeMilestones(t *testing.T) {
	tests := []struct {
		name       string
		milestones []Milestone
		wantEmpty  bool
	}{
		{"empty", nil, true},
		{"single completed", []Milestone{{Summary: "搜索: 天气", Completed: true}}, false},
		{"single in-progress", []Milestone{{Summary: "搜索中", Completed: false}}, false},
		{"multiple completed", []Milestone{
			{Summary: "搜索: 天气", Completed: true},
			{Summary: "生成: report.md", Completed: true},
		}, false},
		{"with current", []Milestone{
			{Summary: "搜索: 天气", Completed: true},
			{Summary: "生成报告", Completed: false},
		}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := MergeMilestones(tt.milestones)
			if tt.wantEmpty && result != "" {
				t.Fatalf("expected empty, got %q", result)
			}
			if !tt.wantEmpty && result == "" {
				t.Fatal("expected non-empty merge result")
			}
		})
	}
}

func TestIsSilentTool(t *testing.T) {
	if !IsSilentTool("read_file") {
		t.Error("read_file should be silent")
	}
	if IsSilentTool("bash") {
		t.Error("bash should not be silent")
	}
	if IsSilentTool("web_search") {
		t.Error("web_search should not be silent")
	}
}
