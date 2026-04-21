package main

import (
	"strings"
	"testing"
)

func TestRolePrefixStreamFilter_NoPrefix(t *testing.T) {
	var out strings.Builder
	f := newRolePrefixStreamFilter(func(s string) { out.WriteString(s) })
	f.Write("服务器整体运行健康。\n资源利用率低。\n")
	f.Flush()
	if f.Halted() {
		t.Error("should not halt on normal text")
	}
	if out.String() != "服务器整体运行健康。\n资源利用率低。\n" {
		t.Errorf("unexpected output: %q", out.String())
	}
}

func TestRolePrefixStreamFilter_BrowserInMiddle(t *testing.T) {
	var out strings.Builder
	f := newRolePrefixStreamFilter(func(s string) { out.WriteString(s) })
	f.Write("服务器整体运行健康。\n\nBrowser: 伯伯，API 服务器资源状况如下：\n## 系统信息\n")
	f.Flush()
	if !f.Halted() {
		t.Error("should halt on Browser: prefix in middle of text")
	}
	want := "服务器整体运行健康。\n\n"
	if out.String() != want {
		t.Errorf("got %q, want %q", out.String(), want)
	}
}

func TestRolePrefixStreamFilter_BrowserAtStart(t *testing.T) {
	// Browser: at the very start should strip the prefix and emit the
	// content after it (Case 1). The filter does NOT halt — it continues
	// processing subsequent lines normally.
	var out strings.Builder
	f := newRolePrefixStreamFilter(func(s string) { out.WriteString(s) })
	f.Write("Browser: 伯伯，API 服务器资源状况如下：\n## 系统信息\n")
	f.Flush()
	if f.Halted() {
		t.Error("should not halt on Browser: at start (Case 1 strips prefix)")
	}
	want := "伯伯，API 服务器资源状况如下：\n## 系统信息\n"
	if out.String() != want {
		t.Errorf("got %q, want %q", out.String(), want)
	}
}

func TestRolePrefixStreamFilter_ToolPrefix(t *testing.T) {
	var out strings.Builder
	f := newRolePrefixStreamFilter(func(s string) { out.WriteString(s) })
	f.Write("正常输出内容。\nTool: 正在执行截屏操作\n结果如下\n")
	f.Flush()
	if !f.Halted() {
		t.Error("should halt on Tool: prefix")
	}
	want := "正常输出内容。\n"
	if out.String() != want {
		t.Errorf("got %q, want %q", out.String(), want)
	}
}

func TestRolePrefixStreamFilter_InsideCodeBlock(t *testing.T) {
	var out strings.Builder
	f := newRolePrefixStreamFilter(func(s string) { out.WriteString(s) })
	input := "结果如下：\n```\nBrowser: connected to session\n```\n完成。\n"
	f.Write(input)
	f.Flush()
	if f.Halted() {
		t.Error("should not halt on Browser: inside code block")
	}
	if out.String() != input {
		t.Errorf("got %q, want %q", out.String(), input)
	}
}

func TestRolePrefixStreamFilter_StreamingTokenByToken(t *testing.T) {
	// Simulate token-by-token streaming.
	var out strings.Builder
	f := newRolePrefixStreamFilter(func(s string) { out.WriteString(s) })
	tokens := []string{
		"服务器", "整体", "运行", "健康", "。\n",
		"\n",
		"Browser", ": ", "伯伯", "，API", " 服务器\n",
	}
	for _, tok := range tokens {
		f.Write(tok)
	}
	f.Flush()
	if !f.Halted() {
		t.Error("should halt on Browser: prefix even with token-by-token streaming")
	}
	want := "服务器整体运行健康。\n\n"
	if out.String() != want {
		t.Errorf("got %q, want %q", out.String(), want)
	}
}

func TestRolePrefixStreamFilter_IndentedPrefix(t *testing.T) {
	var out strings.Builder
	f := newRolePrefixStreamFilter(func(s string) { out.WriteString(s) })
	f.Write("正常内容。\n  Browser: 重复内容\n")
	f.Flush()
	if !f.Halted() {
		t.Error("should halt on indented Browser: prefix")
	}
	want := "正常内容。\n"
	if out.String() != want {
		t.Errorf("got %q, want %q", out.String(), want)
	}
}

func TestRolePrefixStreamFilter_AfterCodeBlock(t *testing.T) {
	var out strings.Builder
	f := newRolePrefixStreamFilter(func(s string) { out.WriteString(s) })
	f.Write("结果如下：\n```\nsome code\n```\n\nBrowser: 重复的总结内容\n")
	f.Flush()
	if !f.Halted() {
		t.Error("should halt on Browser: after code block")
	}
	want := "结果如下：\n```\nsome code\n```\n\n"
	if out.String() != want {
		t.Errorf("got %q, want %q", out.String(), want)
	}
}

func TestRolePrefixStreamFilter_BrowserColonInNormalLine(t *testing.T) {
	// "Browser:" in the middle of a line (not at start) should not trigger.
	var out strings.Builder
	f := newRolePrefixStreamFilter(func(s string) { out.WriteString(s) })
	input := "Chrome 浏览器进程 Browser: PID 917323 占用 CPU 39.6%。\n"
	f.Write("前面的内容。\n")
	f.Write(input)
	f.Flush()
	if f.Halted() {
		t.Error("should not halt on Browser: in middle of line")
	}
}

func TestRolePrefixStreamFilter_SuppressedRunesCount(t *testing.T) {
	var out strings.Builder
	f := newRolePrefixStreamFilter(func(s string) { out.WriteString(s) })
	f.Write("正常内容。\nBrowser: 重复内容很长很长很长\n更多重复\n")
	f.Flush()
	if !f.Halted() {
		t.Fatal("should halt")
	}
	if f.SuppressedRunes() == 0 {
		t.Error("should have suppressed some runes")
	}
}

func TestRolePrefixStreamFilter_FlushWithPendingPrefix(t *testing.T) {
	// If the stream ends with an incomplete line that is a role prefix,
	// it should be caught on Flush.
	var out strings.Builder
	f := newRolePrefixStreamFilter(func(s string) { out.WriteString(s) })
	f.Write("正常内容。\n")
	f.Write("Browser: 重复内容")
	f.Flush()
	if !f.Halted() {
		t.Error("should halt on Browser: prefix in pending buffer at Flush")
	}
	want := "正常内容。\n"
	if out.String() != want {
		t.Errorf("got %q, want %q", out.String(), want)
	}
}


func TestRolePrefixStreamFilter_FullwidthColon(t *testing.T) {
	// Chinese LLMs sometimes use fullwidth colon (：U+FF1A).
	var out strings.Builder
	f := newRolePrefixStreamFilter(func(s string) { out.WriteString(s) })
	f.Write("服务器整体运行健康。\nBrowser：伯伯，API 服务器资源状况如下：\n")
	f.Flush()
	if !f.Halted() {
		t.Error("should halt on Browser with fullwidth colon")
	}
	want := "服务器整体运行健康。\n"
	if out.String() != want {
		t.Errorf("got %q, want %q", out.String(), want)
	}
}

func TestRolePrefixStreamFilter_NoSpaceAfterColon(t *testing.T) {
	// Some LLMs omit the space after the colon: "Browser:伯伯"
	var out strings.Builder
	f := newRolePrefixStreamFilter(func(s string) { out.WriteString(s) })
	f.Write("服务器整体运行健康。\nBrowser:伯伯，API 服务器资源状况如下：\n")
	f.Flush()
	if !f.Halted() {
		t.Error("should halt on Browser: without space after colon")
	}
	want := "服务器整体运行健康。\n"
	if out.String() != want {
		t.Errorf("got %q, want %q", out.String(), want)
	}
}
