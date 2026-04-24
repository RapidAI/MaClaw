package views

import (
	"fmt"
	"strings"
	"testing"
)

// TestScreenshotRepro reproduces the exact content from the user's screenshot
// to identify which markdown elements fail to render.
func TestScreenshotRepro(t *testing.T) {
	// Content that matches the screenshot
	md := `PDF 已经生成并发送给你了！ 🎉

📁 文件：**HuggingFace_Daily_Papers_综述_2026-04-23.pdf**

报告内容概览：

| 章节 | 内容 |
|------|------|
| 一、概述 | 28篇论文整体介绍，精选10篇重点解读 |
| 二、重点论文解读 | 每篇论文含核心内容 + 独立评论分析 |
| 三、趋势分析 | 热词云、研究方向分布表、5大关键趋势 |
| 四、总结与展望 | 从大到精、从分到统、从能到稳、从闭到开 |

收录的10篇重点论文：

| # | 论文 | 热度 | 方向 |
|---|------|------|------|
| 1 | LLaDA2.0-Uni（统一多模态扩散模型） | 🔥207 | 多模态 |
| 2 | Near-Future Policy Optimization | 🔥43 | RLHF |
| 3 | DR-Venus（4B边缘研究智能体） | 🔥37 | Agent |`

	lines := RenderMarkdown(md, 100)

	fmt.Println("=== Rendered output ===")
	for i, l := range lines {
		fmt.Printf("[%02d] %s\n", i, l)
	}
	fmt.Println("=== End ===")

	// Check 1: Bold markers should be stripped
	for i, l := range lines {
		if strings.Contains(l, "**") {
			t.Errorf("line %d still contains raw ** markers: %q", i, l)
		}
	}

	// Check 2: Table pipes should be stripped
	for i, l := range lines {
		if strings.Contains(l, "|") {
			t.Errorf("line %d still contains raw pipe: %q", i, l)
		}
	}

	// Check 3: Table separator should use ─
	foundSep := false
	for _, l := range lines {
		if strings.Contains(l, "─") {
			foundSep = true
			break
		}
	}
	if !foundSep {
		t.Error("no table separator line with ─ found")
	}

	// Check 4: Table content should be present
	foundContent := map[string]bool{
		"章节": false, "内容": false,
		"概述": false, "趋势分析": false,
		"论文": false, "热度": false, "方向": false,
		"LLaDA2.0": false,
	}
	for _, l := range lines {
		for k := range foundContent {
			if strings.Contains(l, k) {
				foundContent[k] = true
			}
		}
	}
	for k, v := range foundContent {
		if !v {
			t.Errorf("content %q not found in rendered output", k)
		}
	}
}

// TestBoldInParagraph tests that **bold** in normal paragraphs is rendered.
func TestBoldInParagraph(t *testing.T) {
	md := "📁 文件：**HuggingFace_Daily_Papers.pdf**"
	lines := RenderMarkdown(md, 80)
	fmt.Println("=== Bold in paragraph ===")
	for i, l := range lines {
		fmt.Printf("[%02d] %q\n", i, l)
	}
	for i, l := range lines {
		if strings.Contains(l, "**") {
			t.Errorf("line %d still contains raw ** markers: %q", i, l)
		}
	}
	if !strings.Contains(lines[0], "HuggingFace_Daily_Papers.pdf") {
		t.Errorf("bold content missing: %q", lines[0])
	}
}

// TestBoldWithFullwidthColon tests bold after fullwidth colon.
func TestBoldWithFullwidthColon(t *testing.T) {
	cases := []struct {
		name string
		md   string
	}{
		{"fullwidth colon", "文件：**name.pdf**"},
		{"halfwidth colon", "文件: **name.pdf**"},
		{"no space", "文件：**name.pdf**"},
		{"with space", "文件： **name.pdf**"},
		{"emoji prefix", "📁 **name.pdf**"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			lines := RenderMarkdown(tc.md, 80)
			for i, l := range lines {
				if strings.Contains(l, "**") {
					t.Errorf("line %d still contains raw ** markers: %q", i, l)
				}
			}
		})
	}
}

// TestWrapLineBreaksMarkdown tests that wrapLine doesn't break markdown
// structure when wrapping long lines.
func TestWrapLineBreaksMarkdown(t *testing.T) {
	// A long line with bold that might get split by wrapLine
	md := "这是一段很长的文字，包含 **重要的加粗内容** 在中间，后面还有更多文字来确保这行足够长以触发换行机制"
	lines := RenderMarkdown(md, 40) // narrow width to force wrapping
	fmt.Println("=== Wrap + bold ===")
	for i, l := range lines {
		fmt.Printf("[%02d] %q\n", i, l)
	}
	for i, l := range lines {
		if strings.Contains(l, "**") {
			t.Errorf("line %d still contains raw ** markers: %q", i, l)
		}
	}
}

// TestStreamingPartialBold simulates streaming where bold markers arrive
// in separate chunks, causing incomplete markdown during rendering.
func TestStreamingPartialBold(t *testing.T) {
	// During streaming, content arrives incrementally.
	// At some point the content might be:
	partial := "📁 文件：**HuggingFace_Daily_Papers_综述_2026-04-23.pdf"
	// Missing closing **
	lines := RenderMarkdown(partial, 80)
	fmt.Println("=== Partial bold (no closing **) ===")
	for i, l := range lines {
		fmt.Printf("[%02d] %q\n", i, l)
	}
	// After fix: orphaned ** should be cleaned, no raw markers visible.
	for i, l := range lines {
		if strings.Contains(l, "**") {
			t.Errorf("line %d still contains raw ** markers: %q", i, l)
		}
	}
	if !strings.Contains(lines[0], "HuggingFace_Daily_Papers") {
		t.Errorf("content missing after orphan cleanup: %q", lines[0])
	}
}

// TestStreamingPartialTable simulates streaming where table rows arrive
// incrementally.
func TestStreamingPartialTable(t *testing.T) {
	// During streaming, only the first table line has arrived
	partial := "报告内容概览：\n\n| 章节 | 内容 |"
	lines := RenderMarkdown(partial, 80)
	fmt.Println("=== Partial table (only header, no separator) ===")
	for i, l := range lines {
		fmt.Printf("[%02d] %q\n", i, l)
	}
	// After fix: orphaned table line should have pipes stripped.
	for i, l := range lines {
		if strings.Contains(l, "|") {
			t.Errorf("line %d still contains raw pipe: %q", i, l)
		}
	}
	// Content should still be present.
	found := false
	for _, l := range lines {
		if strings.Contains(l, "章节") {
			found = true
		}
	}
	if !found {
		t.Error("table cell content '章节' missing after orphan cleanup")
	}
}

// TestStreamingPartialInlineCode tests unclosed backtick during streaming.
func TestStreamingPartialInlineCode(t *testing.T) {
	partial := "use `fmt.Println"
	lines := RenderMarkdown(partial, 80)
	// Content should be present regardless of backtick state.
	if !strings.Contains(lines[0], "fmt.Println") {
		t.Errorf("content missing: %q", lines[0])
	}
}

// TestStreamingPartialLink tests unclosed link during streaming.
func TestStreamingPartialLink(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"unclosed bracket", "see [Go docs"},
		{"unclosed paren", "see [Go docs](https://golang.org"},
		{"complete link", "see [Go docs](https://golang.org) for details"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			lines := RenderMarkdown(tc.input, 80)
			for i, l := range lines {
				// Should not have orphaned [ or ]( without closing )
				if tc.name != "complete link" {
					if strings.Contains(l, "](") && !strings.Contains(l, ")") {
						t.Errorf("line %d has orphaned link syntax: %q", i, l)
					}
				}
			}
		})
	}
}

// TestStreamingMultipleOrphanedBold tests multiple unclosed bold markers.
func TestStreamingMultipleOrphanedBold(t *testing.T) {
	// Two bold sections, second one unclosed
	partial := "**first** and **second"
	lines := RenderMarkdown(partial, 80)
	for i, l := range lines {
		if strings.Contains(l, "**") {
			t.Errorf("line %d still contains raw ** markers: %q", i, l)
		}
	}
	if !strings.Contains(lines[0], "first") {
		t.Errorf("first bold content missing: %q", lines[0])
	}
	if !strings.Contains(lines[0], "second") {
		t.Errorf("second content missing: %q", lines[0])
	}
}

// TestOrphanedDelimitersInCodeBlock ensures code block content is not modified.
func TestOrphanedDelimitersInCodeBlock(t *testing.T) {
	md := "```python\nprint(**kwargs)\n```"
	lines := RenderMarkdown(md, 80)
	found := false
	for _, l := range lines {
		if strings.Contains(l, "**kwargs") {
			found = true
		}
	}
	if !found {
		t.Error("**kwargs inside code block should be preserved")
	}
}

// TestOrphanedDelimitersUnclosedCodeBlock ensures unclosed code block
// content is not modified by orphan cleanup.
func TestOrphanedDelimitersUnclosedCodeBlock(t *testing.T) {
	md := "```python\nprint(**kwargs"
	lines := RenderMarkdown(md, 80)
	found := false
	for _, l := range lines {
		if strings.Contains(l, "**kwargs") {
			found = true
		}
	}
	if !found {
		t.Error("**kwargs inside unclosed code block should be preserved")
	}
}

// TestOrphanedBoldItalicTripleStar tests that *** (bold+italic) is not
// corrupted by the orphaned bold cleanup.
func TestOrphanedBoldItalicTripleStar(t *testing.T) {
	// Complete bold+italic — should render without raw markers.
	md := "***bold italic***"
	lines := RenderMarkdown(md, 80)
	if strings.Contains(lines[0], "***") {
		t.Errorf("complete ***bold italic*** still has raw markers: %q", lines[0])
	}

	// Unclosed *** — should clean up gracefully.
	md2 := "***bold italic"
	lines2 := RenderMarkdown(md2, 80)
	if strings.Contains(lines2[0], "***") {
		t.Errorf("unclosed *** still has raw markers: %q", lines2[0])
	}
}

// TestOrphanedBacktickNotTriple ensures triple backticks (code fences)
// are not corrupted by the orphaned backtick cleanup.
func TestOrphanedBacktickNotTriple(t *testing.T) {
	// A code fence line as the last line should not have backticks removed.
	md := "some text\n```python"
	lines := RenderMarkdown(md, 80)
	// The ``` should be consumed by the code fence toggle, not by orphan cleanup.
	// After the fence toggle, "python" appears as a language label.
	foundPython := false
	for _, l := range lines {
		if strings.Contains(l, "python") {
			foundPython = true
		}
	}
	if !foundPython {
		t.Error("code fence language label 'python' missing")
	}
}

// TestOrphanedSeparatorLine ensures a standalone table separator line
// (|---|---|) is silently dropped, not rendered as "---  ---".
func TestOrphanedSeparatorLine(t *testing.T) {
	md := "some text\n|---|---|"
	lines := RenderMarkdown(md, 80)
	for _, l := range lines {
		if strings.Contains(l, "---") && !strings.Contains(l, "─") {
			t.Errorf("orphaned separator rendered as raw dashes: %q", l)
		}
	}
	// Should only have "some text" line.
	if len(lines) != 1 {
		t.Errorf("expected 1 line, got %d: %v", len(lines), lines)
	}
}

// TestWrapToWidthCJK tests that wrapToWidth correctly handles CJK characters.
func TestWrapToWidthCJK(t *testing.T) {
	// 10 CJK chars = 20 display width. Wrap at 12 should split after 6 chars.
	s := "你好世界测试一二三四"
	lines := wrapToWidth(s, 12)
	if len(lines) < 2 {
		t.Fatalf("expected >= 2 lines, got %d: %v", len(lines), lines)
	}
	// First line should be 6 CJK chars (12 display width).
	if displayWidth(lines[0]) > 12 {
		t.Errorf("first line too wide: %d > 12: %q", displayWidth(lines[0]), lines[0])
	}
}

// TestWrapToWidthShortString tests that short strings are not wrapped.
func TestWrapToWidthShortString(t *testing.T) {
	s := "hello"
	lines := wrapToWidth(s, 80)
	if len(lines) != 1 || lines[0] != s {
		t.Errorf("short string should not be wrapped: %v", lines)
	}
}

// TestEmojiInTableCells tests tables with emoji content.
func TestEmojiInTableCells(t *testing.T) {
	md := "| # | 名称 | 热度 |\n|---|------|------|\n| 1 | Test | 🔥207 |"
	lines := RenderMarkdown(md, 80)
	fmt.Println("=== Emoji table ===")
	for i, l := range lines {
		fmt.Printf("[%02d] %s\n", i, l)
	}
	// Pipes should be stripped
	for i, l := range lines {
		if strings.Contains(l, "|") {
			t.Errorf("line %d still contains raw pipe: %q", i, l)
		}
	}
}

// TestTableFollowedByParagraph tests that a table followed by a paragraph
// doesn't break rendering.
func TestTableFollowedByParagraph(t *testing.T) {
	md := "| A | B |\n|---|---|\n| 1 | 2 |\n\n收录的10篇重点论文："
	lines := RenderMarkdown(md, 80)
	fmt.Println("=== Table + paragraph ===")
	for i, l := range lines {
		fmt.Printf("[%02d] %s\n", i, l)
	}
	// Table should be rendered
	for i, l := range lines {
		if strings.Contains(l, "|") {
			t.Errorf("line %d still contains raw pipe: %q", i, l)
		}
	}
	// Paragraph should be present
	found := false
	for _, l := range lines {
		if strings.Contains(l, "收录") {
			found = true
		}
	}
	if !found {
		t.Error("paragraph after table missing")
	}
}

// TestMultipleTablesWithTextBetween tests two tables separated by text.
func TestMultipleTablesWithTextBetween(t *testing.T) {
	md := `| A | B |
|---|---|
| 1 | 2 |

中间文字

| C | D |
|---|---|
| 3 | 4 |`
	lines := RenderMarkdown(md, 80)
	fmt.Println("=== Multiple tables ===")
	for i, l := range lines {
		fmt.Printf("[%02d] %s\n", i, l)
	}
	for i, l := range lines {
		if strings.Contains(l, "|") {
			t.Errorf("line %d still contains raw pipe: %q", i, l)
		}
	}
}
