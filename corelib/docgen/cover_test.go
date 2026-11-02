package docgen

import (
	"fmt"
	"strings"
	"testing"
	"time"

	gopdf "github.com/VantageDataChat/GoPDF2"
)

// firstBodyPage is the page index of the first body page. Page 0 is the
// style's cover, which carries its own meta rows and no footer.
const firstBodyPage = 1

func TestEveryBuiltinStyleDrawsACoverPage(t *testing.T) {
	gen := New()
	if !gen.HasFont() {
		t.Skip("跳过：系统未找到中文字体")
	}
	for _, style := range builtinStyles() {
		data, err := gen.Generate(Spec{
			Title:       "覆盖页标题",
			Subtitle:    "副标题一行",
			ProjectName: "示例项目",
			Brand:       "MaClaw",
			StyleID:     style.ID,
			Content:     "## 一节\n\n正文一段。\n",
		})
		if err != nil {
			t.Fatalf("%s: generate: %v", style.ID, err)
		}
		cover, err := gopdf.ExtractTextFromPage(data, 0)
		if err != nil {
			t.Fatalf("%s: extract cover: %v", style.ID, err)
		}
		var coverText strings.Builder
		for _, item := range cover {
			coverText.WriteString(item.Text)
		}
		for _, want := range []string{"覆盖页标题", "MaClaw"} {
			if !strings.Contains(coverText.String(), want) {
				t.Fatalf("%s: cover missing %q: %q", style.ID, want, coverText.String())
			}
		}
		body, err := gopdf.ExtractTextFromPage(data, firstBodyPage)
		if err != nil {
			t.Fatalf("%s: extract body: %v", style.ID, err)
		}
		var bodyText strings.Builder
		for _, item := range body {
			bodyText.WriteString(item.Text)
		}
		if !strings.Contains(bodyText.String(), "正文一段") {
			t.Fatalf("%s: body did not start on page 2: %q", style.ID, bodyText.String())
		}
	}
}

func TestFooterCarriesAPageNumber(t *testing.T) {
	gen := New()
	if !gen.HasFont() {
		t.Skip("跳过：系统未找到中文字体")
	}
	data, err := gen.Generate(Spec{
		Title:   "页码",
		Brand:   "MaClaw",
		Content: "第一页。\n\n## 新节\n\n第二页。\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	texts, err := gopdf.ExtractTextFromPage(data, firstBodyPage)
	if err != nil {
		t.Fatal(err)
	}
	var body strings.Builder
	for _, item := range texts {
		body.WriteString(item.Text)
	}
	if !strings.Contains(body.String(), "2") {
		t.Fatalf("first body page carries no page number: %q", body.String())
	}
}

func TestDecoratedBlockKinds(t *testing.T) {
	if got := decoratedBlockKindOf("```go\ncode\n```"); got != panelCode {
		t.Fatalf("code block kind = %q", got)
	}
	if got := decoratedBlockKindOf("~~~\ncode\n~~~"); got != panelCode {
		t.Fatalf("tilde block kind = %q", got)
	}
	if got := decoratedBlockKindOf("> 引用一行"); got != panelQuote {
		t.Fatalf("quote kind = %q", got)
	}
	if got := decoratedBlockKindOf("普通段落"); got != "" {
		t.Fatalf("plain kind = %q", got)
	}
	if got := decoratedBlockKindOf("  \n"); got != "" {
		t.Fatalf("blank kind = %q", got)
	}
}

func TestDecoratedBlocksStillGenerate(t *testing.T) {
	gen := New()
	if !gen.HasFont() {
		t.Skip("跳过：系统未找到中文字体")
	}
	data, err := gen.Generate(Spec{
		Title:   "面板",
		Brand:   "MaClaw",
		StyleID: "plan",
		Content: "前文。\n\n```go\nfmt.Println(\"hi\")\n```\n\n> 引用一句\n\n后文。\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(data) < 2000 {
		t.Fatalf("pdf too small: %d bytes", len(data))
	}
}

func TestSidebarAndPlateMotifsNormalize(t *testing.T) {
	for raw, want := range map[string]string{
		"sidebar": MotifSidebar, "侧栏": MotifSidebar,
		"plate": MotifPlate, "铭牌": MotifPlate,
	} {
		got, err := normalizeMotif(raw)
		if err != nil || got != want {
			t.Fatalf("normalizeMotif(%q) = %q, %v", raw, got, err)
		}
	}
	if _, err := normalizeMotif("nope"); err == nil {
		t.Fatal("unknown motif accepted")
	}
}

func TestDecorExtraIsZeroForPlainBlocks(t *testing.T) {
	gen := New()
	if !gen.HasFont() {
		t.Skip("跳过：系统未找到中文字体")
	}
	layout, err := resolvePDFPageLayout("a4")
	if err != nil {
		t.Fatal(err)
	}
	m, err := gen.newMeasurer(layout)
	if err != nil {
		t.Fatal(err)
	}
	extra, err := m.decorExtra(layout, "普通段落。", false)
	if err != nil || extra != 0 {
		t.Fatalf("plain extra = %.2f, %v", extra, err)
	}
}

func TestFitBlockPrefixReservesPanelInsets(t *testing.T) {
	gen := New()
	if !gen.HasFont() {
		t.Skip("跳过：系统未找到中文字体")
	}
	layout, err := resolvePDFPageLayout("a4")
	if err != nil {
		t.Fatal(err)
	}
	m, err := gen.newMeasurer(layout)
	if err != nil {
		t.Fatal(err)
	}
	// A long quote rewraps into more lines inside the narrower panel, so its
	// inset height exceeds the flow height the fit measures.
	block := "> " + strings.Repeat("这是一段比较长的引用文字，在更窄的引用面板里会换出更多的行来。", 30)
	full, err := m.measureMarkdownHeight(layout, 0, block, false)
	if err != nil {
		t.Fatal(err)
	}
	inset, err := m.measureBlockHeightAt(layout, 0, layout.marginX+panelPadX, layout.contentW-panelPadX-panelPadR, block, false)
	if err != nil {
		t.Fatal(err)
	}
	if inset <= full {
		t.Skipf("quote did not rewrap narrower (full=%.1f inset=%.1f)", full, inset)
	}
	// Slack between the two heights: fits at flow width, not at panel width.
	y := layout.footerY - 4 - (full + (inset-full)/2)
	fit, err := m.fitBlockPrefix(layout, y, []string{block}, false)
	if err != nil {
		t.Fatal(err)
	}
	if fit != 0 {
		t.Fatalf("fit = %d, want 0: the panel would slide under the footer", fit)
	}
}

func TestCoversHoldTogetherOnB5(t *testing.T) {
	gen := New()
	if !gen.HasFont() {
		t.Skip("跳过：系统未找到中文字体")
	}
	// B5 is shorter and narrower than A4; the ledger grid and the meta rows
	// are anchored to the page bottom and must not collide with each other.
	for _, id := range []string{"ledger", "gazette", "brief", "plan", "manual"} {
		data, err := gen.Generate(Spec{
			Title:      "B5 封面",
			Brand:      "MaClaw",
			FooterHint: "页脚提示",
			StyleID:    id,
			PaperSize:  "b5",
			Content:    "正文。\n",
		})
		if err != nil {
			t.Fatalf("%s b5: %v", id, err)
		}
		cover, err := gopdf.ExtractTextFromPage(data, 0)
		if err != nil {
			t.Fatalf("%s b5: %v", id, err)
		}
		var coverText strings.Builder
		for _, item := range cover {
			coverText.WriteString(item.Text)
		}
		compact := strings.Map(func(r rune) rune {
			if r == ' ' || r == '\uFFFD' {
				return -1
			}
			return r
		}, coverText.String())
		if !strings.Contains(compact, "B5封面") || !strings.Contains(compact, "MaClaw") || !strings.Contains(compact, "页脚提示") {
			t.Fatalf("%s b5 cover incomplete: %q", id, compact)
		}
	}
}

func TestH2NumbersContinueAcrossPages(t *testing.T) {
	gen := New()
	if !gen.HasFont() {
		t.Skip("跳过：系统未找到中文字体")
	}
	data, err := gen.Generate(Spec{
		Title:   "跨页序号",
		Brand:   "MaClaw",
		StyleID: "gazette",
		Content: "## 一节\n\n开篇。\n\n" + strings.Repeat("填充段落，用来把后面的章节顶到下一页。\n\n", 45) + "## 二节\n\n接续。\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	lastPage := -1
	for page := 1; page < 8; page++ {
		texts, err := gopdf.ExtractTextFromPage(data, page)
		if err != nil || len(texts) == 0 {
			break
		}
		var b strings.Builder
		for _, item := range texts {
			b.WriteString(item.Text)
		}
		if strings.Contains(b.String(), "二节") {
			lastPage = page
			if !strings.Contains(b.String(), "02") {
				t.Fatalf("page %d restarted H2 numbering: %q", page, b.String())
			}
			break
		}
	}
	if lastPage < 0 {
		t.Fatal("second section never rendered")
	}
}

func TestStressManyDecoratedBlocksPaginate(t *testing.T) {
	gen := New()
	if !gen.HasFont() {
		t.Skip("跳过：系统未找到中文字体")
	}
	var b strings.Builder
	for i := 0; i < 40; i++ {
		fmt.Fprintf(&b, "## 第 %d 节\n\n这一节的长正文用来填充版面，让每个面板都落在不同的分页位置上。\n\n```go\nfmt.Println(\"block %d\")\nfmt.Println(\"second line\")\n```\n\n> 引用 %d：面板与页脚的预留要在这段里得到验证。\n\n", i+1, i, i)
	}
	start := time.Now()
	data, err := gen.Generate(Spec{
		Title:   "压力分页",
		Brand:   "MaClaw",
		StyleID: "plan",
		Content: b.String(),
	})
	if err != nil {
		t.Fatal(err)
	}
	elapsed := time.Since(start)
	if elapsed > 30*time.Second {
		t.Fatalf("render took %s", elapsed)
	}
	if len(data) < 10000 {
		t.Fatalf("pdf too small: %d bytes", len(data))
	}
	// The last section must land somewhere with its number intact.
	found := false
	for page := 1; page < 60; page++ {
		texts, err := gopdf.ExtractTextFromPage(data, page)
		if err != nil || len(texts) == 0 {
			break
		}
		var pageText strings.Builder
		for _, item := range texts {
			pageText.WriteString(item.Text)
		}
		compact := strings.Map(func(r rune) rune {
			if r == ' ' || r == '\uFFFD' {
				return -1
			}
			return r
		}, pageText.String())
		if strings.Contains(compact, "第40节") {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("last section missing")
	}
	t.Logf("40 sections / ~120 blocks rendered in %s (%d KB)", elapsed, len(data)/1024)
}

func TestCoverProjectNameDoesNotRepeatTitle(t *testing.T) {
	gen := New()
	if !gen.HasFont() {
		t.Skip("跳过：系统未找到中文字体")
	}
	count := func(spec Spec) int {
		data, err := gen.Generate(spec)
		if err != nil {
			t.Fatal(err)
		}
		texts, err := gopdf.ExtractTextFromPage(data, 0)
		if err != nil {
			t.Fatal(err)
		}
		var b strings.Builder
		for _, item := range texts {
			b.WriteString(item.Text)
		}
		return strings.Count(b.String(), "需求文档")
	}
	same := count(Spec{Title: "需求文档", ProjectName: "需求文档", Brand: "MaClaw", Content: "正文。\n"})
	if same != 1 {
		t.Fatalf("title repeated %d times on the cover, want 1", same)
	}
	diff := count(Spec{Title: "需求文档", ProjectName: "星河项目", Brand: "MaClaw", Content: "正文。\n"})
	if diff != 1 {
		t.Fatalf("distinct project line vanished: %d", diff)
	}
}
