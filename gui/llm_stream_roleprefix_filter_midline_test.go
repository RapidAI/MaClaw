package main

import (
	"strings"
	"testing"
)

// TestRolePrefixStreamFilter_MidLineBrowserWithNewlineInBuffer tests that
// Browser: preceded by \n within the line buffer (no split on \n yet)
// is caught by checkMidLinePrefix.
func TestRolePrefixStreamFilter_MidLineBrowserWithNewlineInBuffer(t *testing.T) {
	var out strings.Builder
	f := newRolePrefixStreamFilter(func(s string) { out.WriteString(s) })

	// First emit some content to set seenContent=true
	f.Write("正常内容。\n")
	// Now write content with \n followed by Browser: but no trailing \n.
	// The outer loop splits on the first \n in "更多内容\nBrowser: 幻觉内容",
	// emitting "更多内容\n" as a line, then "Browser: 幻觉内容" stays in lineBuf.
	// On Flush, rolePrefixLineRe matches "Browser: 幻觉内容" → Case 2 halt.
	f.Write("更多内容\nBrowser: 幻觉内容")
	f.Flush()

	if !f.Halted() {
		t.Fatal("expected halt for Browser: after \\n in line buffer")
	}
}

// TestRolePrefixStreamFilter_NormalBrowserColonInText verifies that
// "Browser:" in normal text (like process listings) is NOT caught.
func TestRolePrefixStreamFilter_NormalBrowserColonInText(t *testing.T) {
	var out strings.Builder
	f := newRolePrefixStreamFilter(func(s string) { out.WriteString(s) })

	f.Write("前面的内容。\n")
	f.Write("Chrome 浏览器进程 Browser: PID 917323 占用 CPU 39.6%。\n")
	f.Flush()

	if f.Halted() {
		t.Fatal("should not halt on Browser: in normal process listing text")
	}
	if !strings.Contains(out.String(), "Browser: PID 917323") {
		t.Fatal("normal Browser: text should pass through unchanged")
	}
}

// TestRolePrefixStreamFilter_EmbeddedNewlineBrowser tests the scenario
// where a single delta contains "content\nBrowser: hallucination" and
// the outer loop correctly splits it, catching Browser: on its own line.
func TestRolePrefixStreamFilter_EmbeddedNewlineBrowser(t *testing.T) {
	var out strings.Builder
	f := newRolePrefixStreamFilter(func(s string) { out.WriteString(s) })

	f.Write("正常内容。\n")
	// Single delta with embedded \n before Browser:
	f.Write("表格最后一行\nBrowser: 请先在浏览器中登录\n")
	f.Flush()

	if !f.Halted() {
		t.Fatal("expected halt for Browser: on its own line after embedded \\n")
	}
	if strings.Contains(out.String(), "Browser:") {
		t.Fatal("Browser: should not appear in output")
	}
	if !strings.Contains(out.String(), "表格最后一行") {
		t.Fatal("content before Browser: should be preserved")
	}
}

// TestRolePrefixStreamFilter_MidLineCheckThreshold verifies that the
// mid-line check only runs when the buffer exceeds the threshold.
func TestRolePrefixStreamFilter_MidLineCheckThreshold(t *testing.T) {
	var out strings.Builder
	f := newRolePrefixStreamFilter(func(s string) { out.WriteString(s) })

	f.Write("正常内容。\n")
	// Write a short delta without \n — should NOT trigger mid-line check
	// because buffer is under threshold.
	f.Write("短")
	f.Flush()

	if f.Halted() {
		t.Fatal("should not halt on short buffer")
	}
}
