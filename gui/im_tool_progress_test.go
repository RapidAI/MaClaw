package main

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/i18n"
)

func TestUserFacingToolProgressTextWithArgs_ConcreteDetails(t *testing.T) {
	tests := []struct {
		name     string
		tool     string
		argsJSON string
		wantSub  string
		wantNot  string
	}{
		{
			name:     "bash includes command",
			tool:     "bash",
			argsJSON: `{"command":"ls -la /tmp"}`,
			wantSub:  "ls -la /tmp",
			wantNot:  "bash",
		},
		{
			name:     "read_file includes path",
			tool:     "read_file",
			argsJSON: `{"path":"D:\\workprj\\aicoder\\gui\\im_tool_progress.go"}`,
			wantSub:  "im_tool_progress.go",
			wantNot:  "read_file",
		},
		{
			name:     "search_files includes pattern and path",
			tool:     "search_files",
			argsJSON: `{"pattern":"OnToolCall","project_path":"D:\\workprj\\aicoder\\gui","file_pattern":"*.go"}`,
			wantSub:  "OnToolCall",
			wantNot:  "search_files",
		},
		{
			name:     "write_file includes path",
			tool:     "write_file",
			argsJSON: `{"path":"src/main.go","content":"package main"}`,
			wantSub:  "main.go",
			wantNot:  "write_file",
		},
		{
			name:     "web_search includes query",
			tool:     "web_search",
			argsJSON: `{"query":"杭州天气"}`,
			wantSub:  "杭州天气",
			wantNot:  "web_search",
		},
		{
			name:     "run_skill includes skill name",
			tool:     "run_skill",
			argsJSON: `{"name":"docx-writer"}`,
			wantSub:  "docx-writer",
		},
		{
			name:     "ssh exec includes command",
			tool:     "ssh",
			argsJSON: `{"action":"exec","command":"uptime"}`,
			wantSub:  "uptime",
			wantNot:  "ssh",
		},
		{
			name:     "bare tool name not dumped for unknown with no args",
			tool:     "search_files",
			argsJSON: `{}`,
			wantSub:  i18n.T(i18n.MsgToolActionSearchFiles, "zh"),
			wantNot:  "search_files",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := userFacingToolProgressTextWithArgs("zh-Hans", tt.tool, tt.argsJSON)
			if got == "" {
				t.Fatal("expected non-empty progress text")
			}
			// Never dump the bare tool name as the entire message.
			if got == tt.tool {
				t.Fatalf("progress is bare tool name %q", got)
			}
			zhLabel := i18n.T(i18n.MsgToolStatusLabel, "zh")
			if !strings.HasPrefix(got, zhLabel) {
				t.Fatalf("got %q, want prefix %q", got, zhLabel)
			}
			if tt.wantSub != "" && !strings.Contains(got, tt.wantSub) {
				t.Fatalf("got %q, want substring %q", got, tt.wantSub)
			}
			if tt.wantNot != "" && got == tt.wantNot {
				t.Fatalf("got bare %q, expected human-readable detail", got)
			}
		})
	}
}

func TestUserFacingToolProgressText_EnglishUILanguage(t *testing.T) {
	got := userFacingToolProgressTextWithArgs("en", "bash", `{"command":"ls -la"}`)
	enLabel := i18n.T(i18n.MsgToolStatusLabel, "en")
	if !strings.HasPrefix(got, enLabel) {
		t.Fatalf("got %q, want English label prefix %q", got, enLabel)
	}
	if !strings.Contains(got, i18n.T(i18n.MsgToolActionRunCommand, "en")) {
		t.Fatalf("got %q, want English action", got)
	}
	if strings.Contains(got, "【工具】") || strings.Contains(got, "执行命令") {
		t.Fatalf("English UI should not contain Chinese tool labels, got %q", got)
	}
	if !strings.Contains(got, "ls -la") {
		t.Fatalf("got %q, want command detail", got)
	}
}

func TestUserFacingToolProgressText_ChineseUILanguage(t *testing.T) {
	got := userFacingToolProgressTextWithArgs("zh-Hans", "read_file", `{"path":"main.go"}`)
	if !strings.HasPrefix(got, "【工具】") {
		t.Fatalf("got %q, want Chinese status label", got)
	}
	if !strings.Contains(got, "读取文件") {
		t.Fatalf("got %q, want Chinese action", got)
	}
	if strings.Contains(got, "[Tool]") || strings.Contains(got, "Read file") {
		t.Fatalf("Chinese UI should not contain English tool labels, got %q", got)
	}
}

func TestUserFacingToolProgressText_StatusCardStyle(t *testing.T) {
	for _, tool := range []string{"bash", "read_file", "search_files", "write_file", "web_search"} {
		got := userFacingToolProgressText("zh", tool)
		if got == tool {
			t.Fatalf("userFacingToolProgressText(%q) returned bare name", tool)
		}
		if !strings.HasPrefix(got, "【工具】") {
			t.Fatalf("userFacingToolProgressText(%q) = %q, want status-card prefix", tool, got)
		}
	}
}

func TestFormatIMToolStatus_TwoLineCard(t *testing.T) {
	got := formatIMToolStatus("zh", "执行命令", "ls -la /tmp")
	want := "【工具】执行命令\nls -la /tmp"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	gotEN := formatIMToolStatus("en", "Run command", "ls -la /tmp")
	wantEN := "[Tool] Run command\nls -la /tmp"
	if gotEN != wantEN {
		t.Fatalf("got %q, want %q", gotEN, wantEN)
	}
}

func TestFormatIMToolStatus_NoDetail(t *testing.T) {
	got := formatIMToolStatus("zh", "截取屏幕", "")
	if got != "【工具】截取屏幕" {
		t.Fatalf("got %q", got)
	}
}

func TestFormatIMToolStatus_NoDoubleWrap(t *testing.T) {
	got := formatIMToolStatus("zh", "【工具】执行命令", "echo hi")
	if got != "【工具】执行命令\necho hi" {
		t.Fatalf("got %q", got)
	}
}

func TestFilterUserFacingToolProgress_RestylesInternal(t *testing.T) {
	got := filterUserFacingToolProgress("zh", "bash", "正在执行长时间任务...")
	if !strings.HasPrefix(got, "【工具】") {
		t.Fatalf("expected restyled status card, got %q", got)
	}
	if !strings.Contains(got, "命令进度") {
		t.Fatalf("expected 命令进度 label, got %q", got)
	}
	// Noise prefix should be stripped from the detail line.
	if strings.Contains(got, "正在执行长时间") {
		t.Fatalf("expected stripped detail, got %q", got)
	}
	if !strings.Contains(got, "长时间任务") {
		t.Fatalf("expected useful detail body, got %q", got)
	}
	gotEN := filterUserFacingToolProgress("en", "bash", "Running long task...")
	if !strings.HasPrefix(gotEN, "[Tool] ") {
		t.Fatalf("expected English restyled status card, got %q", gotEN)
	}
	if !strings.Contains(gotEN, "long task") {
		t.Fatalf("expected stripped English detail, got %q", gotEN)
	}
	// English prefixes are case-insensitive.
	gotLower := filterUserFacingToolProgress("en", "bash", "running compile step")
	if !strings.Contains(gotLower, "compile step") {
		t.Fatalf("case-insensitive prefix strip failed: %q", gotLower)
	}
}

func TestStyleIMIntermediateProgress(t *testing.T) {
	got := styleIMIntermediateProgress("zh", "收到，正在处理")
	if !strings.HasPrefix(got, "〔进度〕") {
		t.Fatalf("got %q, want progress label", got)
	}
	// Tool cards must not be double-wrapped.
	tool := "【工具】执行命令\nls"
	if styleIMIntermediateProgress("zh", tool) != tool {
		t.Fatalf("tool card should pass through, got %q", styleIMIntermediateProgress("zh", tool))
	}
	// Heartbeat must not be styled.
	if styleIMIntermediateProgress("zh", imHeartbeatMsg) != imHeartbeatMsg {
		t.Fatalf("heartbeat should pass through")
	}
	en := styleIMIntermediateProgress("en", "Got it, working on it...")
	if !strings.HasPrefix(en, "[Status] ") {
		t.Fatalf("got %q, want English status label", en)
	}
}

func TestNormalizeToolProgressName(t *testing.T) {
	got := userFacingToolProgressTextWithArgs("zh", " Read_File ", `{"path":"a.go"}`)
	if !strings.Contains(got, "读取文件") || !strings.Contains(got, "a.go") {
		t.Fatalf("case-insensitive tool name failed: %q", got)
	}
}

func TestFilteredToolProgressCallback_DropsNoise(t *testing.T) {
	var got []string
	cb := filteredToolProgressCallback("zh", "bash", func(msg string) { got = append(got, msg) }, false)
	if cb == nil {
		t.Fatal("expected non-nil callback for bash")
	}
	cb("debug stacktrace")
	cb("正在执行 compile")
	if len(got) != 1 {
		t.Fatalf("got %#v, want 1 filtered message", got)
	}
	if !strings.HasPrefix(got[0], "【工具】") {
		t.Fatalf("filtered message not styled: %q", got[0])
	}

	if filteredToolProgressCallback("zh", "read_file", func(string) {}, false) != nil {
		t.Fatal("read_file should not expose internal progress")
	}
}

func TestIMUILang_PrefersAppCurrentLanguage(t *testing.T) {
	// CurrentLanguage is normalized to its i18n translation tag (en-US -> en)
	// so downstream i18n.NormalizeLang mapping works correctly.
	h := &IMMessageHandler{app: &App{CurrentLanguage: "en-US"}}
	if got := h.imUILang(); got != "en" {
		t.Fatalf("imUILang = %q, want en", got)
	}
	h2 := &IMMessageHandler{}
	if got := h2.imUILang(); got != "zh" {
		t.Fatalf("imUILang fallback = %q, want zh", got)
	}
}

func TestTruncateProgressDetail(t *testing.T) {
	long := strings.Repeat("a", 100)
	got := truncateProgressDetail(long, 20)
	if len([]rune(got)) != 20 {
		t.Fatalf("len=%d, want 20 (%q)", len([]rune(got)), got)
	}
	if !strings.HasSuffix(got, "...") {
		t.Fatalf("expected ellipsis, got %q", got)
	}
}

func TestShortPathForProgress(t *testing.T) {
	short := shortPathForProgress("main.go")
	if short != "main.go" {
		t.Fatalf("short path = %q", short)
	}
	long := shortPathForProgress(`D:\very\long\path\that\should\be\shortened\for\im\display\im_tool_progress.go`)
	if !strings.Contains(long, "im_tool_progress.go") {
		t.Fatalf("expected basename retained, got %q", long)
	}
	if len([]rune(long)) > 48 {
		t.Fatalf("path too long for IM: %q (%d runes)", long, len([]rune(long)))
	}
}
