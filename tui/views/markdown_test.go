package views

import (
	"strings"
	"testing"
)

func TestRenderMarkdown_Headings(t *testing.T) {
	md := "# Title\n## Subtitle\n### Section"
	lines := RenderMarkdown(md, 80)
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d: %v", len(lines), lines)
	}
	// Headings should contain the text (ANSI codes make exact match hard).
	if !strings.Contains(lines[0], "Title") {
		t.Errorf("H1 missing 'Title': %q", lines[0])
	}
	if !strings.Contains(lines[1], "Subtitle") {
		t.Errorf("H2 missing 'Subtitle': %q", lines[1])
	}
	if !strings.Contains(lines[2], "Section") {
		t.Errorf("H3 missing 'Section': %q", lines[2])
	}
}

func TestRenderMarkdown_CodeBlock(t *testing.T) {
	md := "text before\n```python\nprint('hello')\n```\ntext after"
	lines := RenderMarkdown(md, 80)
	// Should have: text, fence open, code line, fence close, text
	if len(lines) < 5 {
		t.Fatalf("expected >= 5 lines, got %d", len(lines))
	}
	// Code fence lines should contain ```
	found := false
	for _, l := range lines {
		if strings.Contains(l, "python") {
			found = true
		}
	}
	if !found {
		t.Error("code fence language 'python' not found")
	}
	// Code content should contain print
	foundCode := false
	for _, l := range lines {
		if strings.Contains(l, "print") {
			foundCode = true
		}
	}
	if !foundCode {
		t.Error("code content 'print' not found")
	}
}

func TestRenderMarkdown_BulletList(t *testing.T) {
	md := "- item one\n- item two\n* item three"
	lines := RenderMarkdown(md, 80)
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(lines))
	}
	for i, l := range lines {
		if !strings.Contains(l, "•") {
			t.Errorf("line %d missing bullet: %q", i, l)
		}
	}
}

func TestRenderMarkdown_NumberedList(t *testing.T) {
	md := "1. first\n2. second\n3. third"
	lines := RenderMarkdown(md, 80)
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(lines))
	}
	if !strings.Contains(lines[0], "first") {
		t.Errorf("line 0 missing 'first': %q", lines[0])
	}
}

func TestRenderMarkdown_HorizontalRule(t *testing.T) {
	md := "above\n---\nbelow"
	lines := RenderMarkdown(md, 80)
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(lines))
	}
	if !strings.Contains(lines[1], "─") {
		t.Errorf("HR line missing '─': %q", lines[1])
	}
}

func TestRenderMarkdown_Blockquote(t *testing.T) {
	md := "> quoted text"
	lines := RenderMarkdown(md, 80)
	if len(lines) != 1 {
		t.Fatalf("expected 1 line, got %d", len(lines))
	}
	if !strings.Contains(lines[0], "│") {
		t.Errorf("blockquote missing '│': %q", lines[0])
	}
	if !strings.Contains(lines[0], "quoted text") {
		t.Errorf("blockquote missing content: %q", lines[0])
	}
}

func TestRenderMarkdown_InlineCode(t *testing.T) {
	md := "use `fmt.Println` here"
	lines := RenderMarkdown(md, 80)
	if len(lines) != 1 {
		t.Fatalf("expected 1 line, got %d", len(lines))
	}
	if !strings.Contains(lines[0], "fmt.Println") {
		t.Errorf("inline code missing: %q", lines[0])
	}
}

func TestRenderMarkdown_EmptyInput(t *testing.T) {
	lines := RenderMarkdown("", 80)
	if len(lines) != 1 || strings.TrimSpace(lines[0]) != "" {
		t.Errorf("empty input should produce 1 empty line, got %d: %v", len(lines), lines)
	}
}

func TestRenderMarkdown_Table(t *testing.T) {
	md := "| 类别 | 工具 |\n|------|------|\n| 系统 | bash |\n| 文件 | read_file |"
	lines := RenderMarkdown(md, 80)
	// Should have: header row, separator, 2 data rows = 4 lines.
	if len(lines) < 4 {
		t.Fatalf("expected >= 4 lines, got %d: %v", len(lines), lines)
	}
	// Header should contain "类别".
	if !strings.Contains(lines[0], "类别") {
		t.Errorf("header missing '类别': %q", lines[0])
	}
	// Separator should contain "─".
	if !strings.Contains(lines[1], "─") {
		t.Errorf("separator missing '─': %q", lines[1])
	}
	// Data rows should contain cell content.
	found := false
	for _, l := range lines[2:] {
		if strings.Contains(l, "bash") {
			found = true
		}
	}
	if !found {
		t.Error("data row missing 'bash'")
	}
	// No raw pipes should remain in output.
	for i, l := range lines {
		if strings.Contains(l, "|") {
			t.Errorf("line %d still contains raw pipe: %q", i, l)
		}
	}
}

func TestRenderMarkdown_TableNoPipes(t *testing.T) {
	// Table with leading pipe but no trailing pipe — still valid.
	md := "| Name | Value\n| --- | ---\n| foo | bar"
	lines := RenderMarkdown(md, 80)
	if len(lines) < 3 {
		t.Fatalf("expected >= 3 lines, got %d", len(lines))
	}
	if !strings.Contains(lines[0], "Name") {
		t.Errorf("header missing 'Name': %q", lines[0])
	}
	found := false
	for _, l := range lines {
		if strings.Contains(l, "bar") {
			found = true
		}
	}
	if !found {
		t.Error("data row missing 'bar'")
	}
}

func TestRenderMarkdown_TableCJKAlignment(t *testing.T) {
	md := "| 名称 | 描述 |\n|------|------|\n| SSH | 远程连接 |"
	lines := RenderMarkdown(md, 80)
	if len(lines) < 3 {
		t.Fatalf("expected >= 3 lines, got %d", len(lines))
	}
	// Both "名称" and "SSH" should be present.
	if !strings.Contains(lines[0], "名称") {
		t.Errorf("header missing '名称': %q", lines[0])
	}
	foundSSH := false
	for _, l := range lines {
		if strings.Contains(l, "SSH") {
			foundSSH = true
		}
	}
	if !foundSSH {
		t.Error("data row missing 'SSH'")
	}
}

func TestIsTableLine(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"| a | b |", true},
		{"|---|---|", true},
		{"a | b |", true},   // ends with |
		{"| a | b", true},   // starts with |
		{"--- | ---", true},  // valid table separator (dashes + pipes)
		{"no pipes here", false},
		{"", false},
		{"just a | in text", false}, // pipe in prose, no leading/trailing |
		{"---", false},              // horizontal rule, not table
		{"a | b", false},            // no leading/trailing pipe
	}
	for _, tt := range tests {
		got := isTableLine(tt.input)
		if got != tt.want {
			t.Errorf("isTableLine(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestDisplayWidth(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"hello", 5},
		{"你好", 4},     // 2 CJK chars × 2 = 4
		{"hi你好", 6},   // 2 + 4 = 6
		{"", 0},
		{"bash", 4},
	}
	for _, tt := range tests {
		got := displayWidth(tt.input)
		if got != tt.want {
			t.Errorf("displayWidth(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestTruncateToWidth(t *testing.T) {
	tests := []struct {
		input  string
		target int
		want   string
	}{
		{"hello", 3, "hel"},
		{"hello", 10, "hello"},
		{"你好世界", 4, "你好"},     // 2 CJK chars = width 4
		{"你好世界", 5, "你好"},     // can't fit half a CJK char
		{"hi你好", 4, "hi你"},     // 2 + 2 = 4
		{"", 5, ""},
		{"abc", 0, ""},
	}
	for _, tt := range tests {
		got := truncateToWidth(tt.input, tt.target)
		if got != tt.want {
			t.Errorf("truncateToWidth(%q, %d) = %q, want %q", tt.input, tt.target, got, tt.want)
		}
	}
}

func TestRenderMarkdown_TableBoldCells(t *testing.T) {
	md := "| 项目 | 数据 |\n|------|------|\n| **天气** | 晴 |\n| **气温** | 20°C |"
	lines := RenderMarkdown(md, 80)
	if len(lines) < 4 {
		t.Fatalf("expected >= 4 lines, got %d: %v", len(lines), lines)
	}
	// Bold markers should NOT appear in output.
	for i, l := range lines {
		if strings.Contains(l, "**") {
			t.Errorf("line %d still contains raw ** markers: %q", i, l)
		}
	}
	// Content should still be present.
	foundWeather := false
	foundTemp := false
	for _, l := range lines {
		if strings.Contains(l, "天气") {
			foundWeather = true
		}
		if strings.Contains(l, "气温") {
			foundTemp = true
		}
	}
	if !foundWeather {
		t.Error("table missing '天气'")
	}
	if !foundTemp {
		t.Error("table missing '气温'")
	}
}

func TestContentDisplayWidth(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"hello", 5},
		{"**bold**", 4},         // strips ** → "bold" = 4
		{"**天气**", 4},          // strips ** → "天气" = 4
		{"__bold__", 4},         // strips __ → "bold" = 4
		{"`code`", 4},           // strips ` → "code" = 4
		{"no markers", 10},
		{"", 0},
	}
	for _, tt := range tests {
		got := contentDisplayWidth(tt.input)
		if got != tt.want {
			t.Errorf("contentDisplayWidth(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestDisplayWidthVisible(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  int
	}{
		{"plain ASCII", "hello", 5},
		{"plain CJK", "你好", 4},
		{"ANSI bold", "\x1b[1mbold\x1b[0m", 4},
		{"ANSI with CJK", "\x1b[1m天气\x1b[0m", 4},
		{"mixed", "pre\x1b[1mbold\x1b[0mpost", 11}, // pre(3) + bold(4) + post(4)
		{"empty", "", 0},
		{"only ANSI", "\x1b[1m\x1b[0m", 0},
	}
	for _, tt := range tests {
		got := displayWidthVisible(tt.input)
		if got != tt.want {
			t.Errorf("displayWidthVisible[%s](%q) = %d, want %d", tt.name, tt.input, got, tt.want)
		}
	}
}
