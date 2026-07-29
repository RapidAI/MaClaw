package main

import "testing"

func TestStripRolePrefixHallucination_BrowserAtStart(t *testing.T) {
	input := "Browser: 伯伯，API 服务器资源状况如下：\n## 系统信息"
	got := stripRolePrefixHallucination(input)
	want := "伯伯，API 服务器资源状况如下：\n## 系统信息"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestStripRolePrefixHallucination_BrowserInMiddle(t *testing.T) {
	input := "服务器整体运行健康，资源利用率低。\n\nBrowser: 伯伯，API 服务器资源状况如下：\n## 系统信息"
	got := stripRolePrefixHallucination(input)
	want := "服务器整体运行健康，资源利用率低。"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestStripRolePrefixHallucination_ToolPrefix(t *testing.T) {
	input := "Tool: 正在执行截屏操作\n结果如下"
	got := stripRolePrefixHallucination(input)
	want := "正在执行截屏操作\n结果如下"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestStripRolePrefixHallucination_NoPrefix(t *testing.T) {
	input := "服务器运行正常，Chrome 浏览器进程占 CPU 39.6%。"
	got := stripRolePrefixHallucination(input)
	if got != input {
		t.Errorf("should not modify text without role prefix, got %q", got)
	}
}

func TestStripRolePrefixHallucination_InsideCodeBlock(t *testing.T) {
	input := "结果如下：\n```\nBrowser: connected to session\n```\n完成。"
	got := stripRolePrefixHallucination(input)
	if got != input {
		t.Errorf("should not strip inside code block, got %q", got)
	}
}

func TestStripRolePrefixHallucination_Empty(t *testing.T) {
	got := stripRolePrefixHallucination("")
	if got != "" {
		t.Errorf("empty input should return empty, got %q", got)
	}
}

func TestStripRolePrefixHallucination_BrowserColonInNormalText(t *testing.T) {
	// "Browser:" appearing as part of normal prose (not at line start) should not be stripped.
	input := "Chrome 浏览器进程 Browser: PID 917323 占用 CPU 39.6%。"
	got := stripRolePrefixHallucination(input)
	if got != input {
		t.Errorf("should not strip Browser: in middle of line, got %q", got)
	}
}

func TestStripRolePrefixHallucination_IndentedPrefix(t *testing.T) {
	input := "正常内容。\n  Browser: 重复内容"
	got := stripRolePrefixHallucination(input)
	want := "正常内容。"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestStripRolePrefixHallucination_AfterCodeBlock(t *testing.T) {
	// Browser: prefix appearing after a code block should still be stripped.
	input := "结果如下：\n```\nsome code\n```\n\nBrowser: 重复的总结内容"
	got := stripRolePrefixHallucination(input)
	want := "结果如下：\n```\nsome code\n```"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestStripRolePrefixHallucination_OnlyPrefix(t *testing.T) {
	// Edge case: entire text is just the prefix with no content after it.
	input := "Browser: "
	got := stripRolePrefixHallucination(input)
	want := ""
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestStripRolePrefixHallucination_MultipleCodeBlocks(t *testing.T) {
	// Browser: between two code blocks should be stripped.
	input := "```\nblock1\n```\nBrowser: hallucinated\n```\nblock2\n```"
	got := stripRolePrefixHallucination(input)
	want := "```\nblock1\n```"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestStripRolePrefixHallucination_FullwidthColon(t *testing.T) {
	// Chinese LLMs sometimes use fullwidth colon (：U+FF1A).
	input := "服务器整体运行健康。\n\nBrowser：伯伯，API 服务器资源状况如下：\n## 系统信息"
	got := stripRolePrefixHallucination(input)
	want := "服务器整体运行健康。"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestStripRolePrefixHallucination_FullwidthColonAtStart(t *testing.T) {
	input := "Browser：伯伯，API 服务器资源状况如下：\n## 系统信息"
	got := stripRolePrefixHallucination(input)
	want := "伯伯，API 服务器资源状况如下：\n## 系统信息"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestStripRolePrefixHallucination_NoSpaceAfterColon(t *testing.T) {
	input := "服务器整体运行健康。\n\nBrowser:伯伯，API 服务器资源状况如下：\n## 系统信息"
	got := stripRolePrefixHallucination(input)
	want := "服务器整体运行健康。"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
