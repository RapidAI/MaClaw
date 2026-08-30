package agent

import "testing"

// StripRolePrefixHallucinationLeading is the reasoning-safe variant: it strips
// a hallucinated role prefix only at the very start and must never truncate
// mid-text content, because model reasoning legitimately plans tool usage with
// lines like "Tool: bash ...".

func TestStripRolePrefixHallucinationLeading_StripsPrefixAtStart(t *testing.T) {
	input := "Tool: 正在分析日志\n结论是服务正常"
	got := StripRolePrefixHallucinationLeading(input)
	want := "正在分析日志\n结论是服务正常"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestStripRolePrefixHallucinationLeading_FullwidthColonAtStart(t *testing.T) {
	input := "Browser：先检查端口占用\n然后重启服务"
	got := StripRolePrefixHallucinationLeading(input)
	want := "先检查端口占用\n然后重启服务"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestStripRolePrefixHallucinationLeading_KeepsMidTextToolLines(t *testing.T) {
	// The false positive this variant exists for: an agent's reasoning often
	// writes "Tool: bash ..." lines in the middle of its thinking. These must
	// survive untouched.
	input := "先确认服务状态。\nTool: bash systemctl status api\n然后根据输出判断是否重启。\nTool: bash journalctl -u api -n 50"
	got := StripRolePrefixHallucinationLeading(input)
	if got != input {
		t.Errorf("mid-text Tool: lines in reasoning must not be truncated, got %q", got)
	}
}

func TestStripRolePrefixHallucinationLeading_OnlyPrefix(t *testing.T) {
	got := StripRolePrefixHallucinationLeading("Browser: ")
	if got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestStripRolePrefixHallucinationLeading_InsideCodeBlockAtStart(t *testing.T) {
	// A fence before the prefix means real content precedes it — not a
	// leading hallucination.
	input := "```\nTool: sample output\n```"
	got := StripRolePrefixHallucinationLeading(input)
	if got != input {
		t.Errorf("should not strip inside code block, got %q", got)
	}
}

func TestStripRolePrefixHallucinationLeading_Empty(t *testing.T) {
	if got := StripRolePrefixHallucinationLeading(""); got != "" {
		t.Errorf("empty input should return empty, got %q", got)
	}
}

// Guard: the content-cleaning variant must keep its mid-text truncation
// (Case 2), which removes duplicated hallucinated re-starts in assistant
// content. The reasoning-safe variant must not weaken this behavior.
func TestStripRolePrefixHallucination_MidTextTruncationPreserved(t *testing.T) {
	input := "服务器整体运行健康，资源利用率低。\n\nBrowser: 伯伯，API 服务器资源状况如下：\n## 系统信息"
	got := StripRolePrefixHallucination(input)
	want := "服务器整体运行健康，资源利用率低。"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
