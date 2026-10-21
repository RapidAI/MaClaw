package docgen

import (
	"math"
	"sort"
	"strings"
	"testing"

	gopdf "github.com/VantageDataChat/GoPDF2"
)

func TestBreakInlineTails_KeepsColonWithBoldLabel(t *testing.T) {
	tail := "：" + strings.Repeat("论文不定义自己的指标", 4)
	html := markdownToHTML("- **指标**" + tail)
	if !strings.Contains(html, "</b><span>：</span>") {
		t.Fatalf("colon after bold was not split from the long tail:\n%s", html)
	}
	if strings.Contains(html, "<span>：论文") {
		t.Fatalf("colon stayed glued to the whole tail:\n%s", html)
	}
	plain := strings.ReplaceAll(strings.ReplaceAll(html, "<span>", ""), "</span>", "")
	if !strings.Contains(plain, "</b>"+tail) {
		t.Fatalf("split dropped or rewrote characters:\n%s", html)
	}
}

func TestBreakInlineTails_KeepsLatinWordAndSpaces(t *testing.T) {
	tail := "：" + strings.Repeat("论文", 6) + " TRACE " + strings.Repeat("协议继续说明文字", 2)
	html := markdownToHTML("- **指标**" + tail)
	if !strings.Contains(html, " TRACE ") {
		t.Fatalf("spaces around TRACE were lost: %s", html)
	}
	if strings.Contains(html, ">TRA<") || strings.Contains(html, ">CE<") {
		t.Fatalf("TRACE was split mid-word: %s", html)
	}
}

func TestBreakInlineTails_LeavesShortTailAndPlainProse(t *testing.T) {
	short := markdownToHTML("- **图片**：实况")
	if !strings.Contains(short, "</b>：实况") || strings.Contains(short, "<span>：</span>") {
		t.Fatalf("short tail should stay in the text node: %s", short)
	}
	plain := markdownToHTML("三条虚线：" + strings.Repeat("展开本文要考察的三个扩展方向", 4))
	if strings.Contains(plain, "<span>") {
		t.Fatalf("a plain paragraph is one run and must not be split: %s", plain)
	}
}

func TestBreakInlineTails_SkipsTableCells(t *testing.T) {
	md := "| 名称 | 说明 |\n|---|---|\n| **指标** | " + strings.Repeat("论文不定义自己的指标", 4) + " |"
	html := markdownToHTML(md)
	if strings.Contains(html, "<span>：</span>") || strings.Contains(html, "<span>论</span>") {
		t.Fatalf("table cells flatten spans into spaced text: %s", html)
	}
}

func TestGeneratePDF_ColonStaysOnLabelLine(t *testing.T) {
	gen := New()
	if !gen.HasFont() {
		t.Skip("跳过：系统未找到中文字体")
	}
	md := strings.Join([]string{
		"- **主线（实线）**：" + strings.Repeat("描绘经典持续学习流水线", 6),
		"- **指标**：" + strings.Repeat("论文不定义自己的指标", 6),
		"- 三条虚线：" + strings.Repeat("展开本文要考察的三个扩展方向", 4),
	}, "\n")
	data, err := gen.Generate(Spec{
		Title:   "冒号换行",
		Content: md,
	})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	texts, err := gopdf.ExtractTextFromPage(data, 0)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	lines := pdfLines(texts)
	for _, marker := range []string{"实线", "指标", "三条虚线"} {
		line := pdfLineContaining(lines, marker)
		if line == "" {
			t.Fatalf("missing line %q", marker)
		}
		compact := strings.ReplaceAll(strings.ReplaceAll(line, " ", ""), "\uFFFD", "")
		idx := strings.Index(compact, marker)
		after := compact[idx+len(marker):]
		if !strings.HasPrefix(after, "：") && !strings.HasPrefix(after, "）：") {
			t.Fatalf("%s colon is not on the label line: %q", marker, after)
		}
	}
}

func pdfLines(texts []gopdf.ExtractedText) []string {
	type line struct {
		y     float64
		items []gopdf.ExtractedText
	}
	var lines []line
	for _, item := range texts {
		if item.Text == "" {
			continue
		}
		placed := false
		for i := range lines {
			if math.Abs(lines[i].y-item.Y) <= 2 {
				lines[i].items = append(lines[i].items, item)
				placed = true
				break
			}
		}
		if !placed {
			lines = append(lines, line{y: item.Y, items: []gopdf.ExtractedText{item}})
		}
	}
	sort.Slice(lines, func(i, j int) bool { return lines[i].y > lines[j].y })
	out := make([]string, 0, len(lines))
	for _, ln := range lines {
		sort.Slice(ln.items, func(i, j int) bool { return ln.items[i].X < ln.items[j].X })
		var b strings.Builder
		for _, item := range ln.items {
			b.WriteString(strings.ReplaceAll(item.Text, "\uFFFD", ""))
		}
		out = append(out, b.String())
	}
	return out
}

func pdfLineContaining(lines []string, marker string) string {
	for _, line := range lines {
		if strings.Contains(strings.ReplaceAll(line, " ", ""), marker) {
			return line
		}
	}
	return ""
}
