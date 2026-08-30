package main

import (
	"bytes"
	"log"
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

func TestRolePrefixStreamFilter_LogsDoNotIncludeSuppressedContent(t *testing.T) {
	var logs bytes.Buffer
	oldWriter := log.Writer()
	oldFlags := log.Flags()
	log.SetOutput(&logs)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(oldWriter)
		log.SetFlags(oldFlags)
	})

	const sensitive = "SECRET_BROWSER_DEBUG_PAYLOAD"
	var out strings.Builder
	f := newRolePrefixStreamFilter(func(s string) { out.WriteString(s) })
	f.Write("正常内容。\nBrowser: " + sensitive + "\n")
	f.Flush()

	if !f.Halted() {
		t.Fatal("should halt on Browser prefix")
	}
	if strings.Contains(out.String(), sensitive) {
		t.Fatalf("suppressed content leaked to stream output: %q", out.String())
	}
	if strings.Contains(logs.String(), sensitive) {
		t.Fatalf("suppressed content leaked to logs: %q", logs.String())
	}
}

func TestStripRolePrefixReasoningForDisplay_StripsOnlyLeadingPrefix(t *testing.T) {
	got := stripRolePrefixReasoningForDisplay("Browser: 先打开页面\nTool: 然后调用 bash\n其余思考")
	want := "先打开页面\nTool: 然后调用 bash\n其余思考"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestStripRolePrefixReasoningForDisplay_KeepsMidTextPrefix(t *testing.T) {
	// A role prefix on a later line is legitimate reasoning about tool usage;
	// it must not truncate the remaining thought.
	input := "先分析请求。\nBrowser: 需要打开目标页面\nTool: 再执行 bash 命令\n最后总结。"
	if got := stripRolePrefixReasoningForDisplay(input); got != input {
		t.Fatalf("mid-text prefix truncated reasoning: got %q, want %q", got, input)
	}
}

func TestStripRolePrefixReasoningForDisplay_NoPrefixUnchanged(t *testing.T) {
	input := "普通思考内容，不包含角色前缀。"
	if got := stripRolePrefixReasoningForDisplay(input); got != input {
		t.Fatalf("got %q, want %q", got, input)
	}
	if got := stripRolePrefixReasoningForDisplay(""); got != "" {
		t.Fatalf("empty input: got %q", got)
	}
}
