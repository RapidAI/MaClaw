package progress

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/i18n"
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
	want := i18n.T(i18n.MsgToolActionGeneratePDF, "zh")
	if m.Summary != want {
		t.Fatalf("expected %q, got %q", want, m.Summary)
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
	want := i18n.T(i18n.MsgToolActionWebSearch, "zh") + ": 杭州天气"
	if m.Summary != want {
		t.Fatalf("expected %q, got %q", want, m.Summary)
	}
}

func TestExtractMilestone_RunSkillAlternateKeys(t *testing.T) {
	m := ExtractMilestone("run_skill", map[string]any{"skill_name": "docx-writer"}, true)
	if m == nil {
		t.Fatal("expected milestone for skill_name key")
	}
	if !strings.Contains(m.Summary, "docx-writer") {
		t.Fatalf("expected skill name in summary, got %q", m.Summary)
	}
}

func TestExtractMilestone_NormalizesToolName(t *testing.T) {
	m := ExtractMilestone(" Web_Search ", map[string]any{"query": "x"}, true)
	if m == nil {
		t.Fatal("expected milestone for mixed-case tool name")
	}
}

func TestExtractMilestone_English(t *testing.T) {
	m := ExtractMilestoneLang("web_search", map[string]any{"query": "weather"}, true, "en")
	if m == nil {
		t.Fatal("expected milestone")
	}
	want := i18n.T(i18n.MsgToolActionWebSearch, "en") + ": weather"
	if m.Summary != want {
		t.Fatalf("expected %q, got %q", want, m.Summary)
	}
	if stringsContainsHan(m.Summary) {
		t.Fatalf("English milestone should not contain Chinese: %q", m.Summary)
	}
}

func TestExtractMilestone_ArgTruncation(t *testing.T) {
	longQuery := "这是一个非常非常非常非常非常非常非常非常非常非常非常非常非常长的搜索查询"
	m := ExtractMilestone("web_search", map[string]any{"query": longQuery}, true)
	if m == nil {
		t.Fatal("expected milestone")
	}
	// Verb + ": " + 30 runes + "..." should stay bounded.
	runes := []rune(m.Summary)
	if len(runes) > 50 {
		t.Fatalf("summary too long (%d runes): %q", len(runes), m.Summary)
	}
}

func TestExtractMilestone_SSHActions(t *testing.T) {
	tests := []struct {
		action   string
		host     string
		expected string
	}{
		{"connect", "api.example.com", i18n.T(i18n.MsgToolActionSSHConnect, "zh") + " api.example.com"},
		{"exec", "", i18n.T(i18n.MsgToolActionSSHExec, "zh") + ": ls -la"},
		{"close", "", i18n.T(i18n.MsgToolActionSSHClose, "zh")},
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
	want := i18n.Tf(i18n.MsgMilestoneVerbEllipsis, "zh", i18n.T(i18n.MsgToolActionWriteFile, "zh"))
	if m.Summary != want {
		t.Fatalf("expected %q, got %q", want, m.Summary)
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

func TestMergeMilestonesLang_English(t *testing.T) {
	got := MergeMilestonesLang("en", []Milestone{{Summary: "Web search: weather", Completed: false}})
	want := i18n.Tf(i18n.MsgMilestoneWorking, "en", "Web search: weather")
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if stringsContainsHan(got) {
		t.Fatalf("English merge should not contain Chinese: %q", got)
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

func stringsContainsHan(s string) bool {
	for _, r := range s {
		if r >= 0x4e00 && r <= 0x9fff {
			return true
		}
	}
	return false
}
