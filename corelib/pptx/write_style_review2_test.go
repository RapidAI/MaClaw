package pptx

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// Latin glyphs average ~0.55 em, not a full em. The line estimator must
// measure width units, or English titles wrap-estimate ~2x high: fonts shrink
// below base and the subtitle floats far below the title.
func TestCoverTitleLinesMeasuresLatinWidth(t *testing.T) {
	latin := strings.Repeat("A", 54)
	if got := coverTitleLines(latin, 44, 11.5); got != 2 {
		t.Fatalf("latin title lines = %d, want 2 (0.55em per glyph)", got)
	}
	cjk := strings.Repeat("永", 24)
	if got := coverTitleLines(cjk, 44, 11.5); got != 2 {
		t.Fatalf("cjk title lines = %d, want 2 (1em per glyph)", got)
	}
	if got := coverTitleLines(strings.Repeat("永", 40), 44, 11.5); got != 3 {
		t.Fatalf("long cjk title lines = %d, want 3", got)
	}
}

// The consulting footer is 10pt meta text; blending the mute toward white
// drops it under the 4.5:1 body-text contrast floor.
func TestConsultingFooterKeepsMuteContrast(t *testing.T) {
	path := filepath.Join(t.TempDir(), "footer.pptx")
	outline := Outline{
		Title: "增长战略",
		Theme: "consulting",
		Slides: []OutlineSlide{
			{Title: "市场背景", Layout: "bullets", Bullets: []string{"增速放缓"}},
		},
	}
	if err := WriteFile(path, outline); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	files := zipNamedContents(t, path)
	body := ""
	for name, content := range files {
		if strings.HasPrefix(name, "ppt/slides/slide") && strings.Contains(content, "增长战略") &&
			strings.Contains(content, "市场背景") {
			body = content
		}
	}
	if body == "" {
		t.Fatal("missing consulting content slide")
	}
	color := runColorAfterText(t, body, "增长战略")
	if color == "" {
		t.Fatal("footer run color not found")
	}
	if !strings.EqualFold(color, "6B7680") {
		t.Fatalf("footer color %s lightened below 4.5:1 contrast floor, want 6B7680", color)
	}
}

// A one-line quote must sit on the content box's optical center, not cling to
// the top third with the lower two thirds empty.
func TestQuoteBodyVerticallyCentered(t *testing.T) {
	path := filepath.Join(t.TempDir(), "quote.pptx")
	outline := Outline{
		Theme: "consulting",
		Slides: []OutlineSlide{
			{Title: "克制是一种力量", Layout: "quote", Bullets: []string{"少即是多"}},
		},
	}
	if err := WriteFile(path, outline); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	files := zipNamedContents(t, path)
	body := ""
	for name, content := range files {
		if strings.HasPrefix(name, "ppt/slides/slide") && strings.Contains(content, "少即是多") {
			body = content
		}
	}
	if body == "" {
		t.Fatal("missing quote slide")
	}
	y := shapeOffsetYBeforeText(t, body, "少即是多")
	if y == 0 {
		t.Fatal("quote text shape offset not found")
	}
	if y < emuIn(3.0) {
		t.Fatalf("one-line quote sits at y=%d EMU (%.2fin), want vertically centered (>= %.2fin)", y, float64(y)/914400, 3.0)
	}
}

// runColorAfterText finds the <a:r> run containing text and returns its
// srgbClr value from the run properties.
func runColorAfterText(t *testing.T, xml, text string) string {
	t.Helper()
	for _, run := range strings.Split(xml, "<a:r>") {
		if !strings.Contains(run, "<a:t>"+text+"</a:t>") {
			continue
		}
		m := regexp.MustCompile(`<a:srgbClr val="([0-9A-Fa-f]{6})"`).FindStringSubmatch(run)
		if m != nil {
			return m[1]
		}
	}
	return ""
}

// shapeOffsetYBeforeText returns the y offset of the shape whose txBody
// contains text: the last <a:off> appearing before the run.
func shapeOffsetYBeforeText(t *testing.T, xml, text string) int64 {
	t.Helper()
	idx := strings.Index(xml, "<a:t>"+text+"</a:t>")
	if idx < 0 {
		return 0
	}
	head := xml[:idx]
	last := strings.LastIndex(head, "<a:off ")
	if last < 0 {
		return 0
	}
	m := regexp.MustCompile(`<a:off x="-?\d+" y="(-?\d+)"`).FindStringSubmatch(head[last:])
	if m == nil {
		return 0
	}
	y, err := strconv.ParseInt(m[1], 10, 64)
	if err != nil {
		t.Fatalf("parse offset %s: %v", m[1], err)
	}
	return y
}
