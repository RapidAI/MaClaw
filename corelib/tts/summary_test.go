package tts

import "testing"

func TestGenerateVoiceSummary_Structured(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{
			`{"userText":"修复登录页面的样式问题","status":"success"}`,
			"任务已完成，该任务是修复登录页面的样式问题",
		},
		{
			`{"userText":"查找项目中的配置文件","status":"error"}`,
			"任务处理失败，该任务是查找项目中的配置文件",
		},
		{
			`{"userText":"翻译技术文档为中文","status":"paused"}`,
			"任务已暂停，该任务是翻译技术文档为中文",
		},
		{
			`{"userText":"请帮我开发一个贪吃蛇游戏","status":"success"}`,
			"任务已完成，该任务是开发一个贪吃蛇游戏",
		},
		{
			`{"userText":"","status":"success"}`,
			"任务已完成",
		},
	}
	for _, tc := range tests {
		got := GenerateVoiceSummary(tc.input, 150)
		if got != tc.want {
			t.Errorf("input=%s\n  got:  %s\n  want: %s", tc.input, got, tc.want)
		}
	}
}

func TestGenerateVoiceSummary_PlainText(t *testing.T) {
	// Non-JSON input — fallback to generic summary
	got := GenerateVoiceSummary("这是一段普通文本回复", 150)
	if got != "任务完成。这是一段普通文本回复" {
		t.Errorf("got: %s", got)
	}
}

func TestGenerateVoiceSummary_MarkdownCleaning(t *testing.T) {
	input := `{"userText":"帮我修复 **main.go** 中的 ` + "`" + `compile error` + "`" + `","status":"success"}`
	got := GenerateVoiceSummary(input, 150)
	t.Logf("got: %s", got)
	// Should not contain ** or backticks
	if contains(got, "**") || contains(got, "`") {
		t.Errorf("Markdown not cleaned: %s", got)
	}
}

func TestCleanForSpeech(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"**加粗文本**", "加粗文本"},
		{"# 标题", "标题"},
		{"[链接](https://example.com)", "链接"},
		{"https://example.com/path", ""},
		{"C:\\Users\\test\\file.go", ""},
		{"```go\nfmt.Println()\n```", ""},
		{"`inline code`", "inline code"},
		{"- 列表项1\n- 列表项2", "列表项1 列表项2"},
	}
	for _, tc := range tests {
		got := cleanForSpeech(tc.input)
		if got != tc.want {
			t.Errorf("cleanForSpeech(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		(len(s) > 0 && len(sub) > 0 && findSubstring(s, sub)))
}

func findSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
