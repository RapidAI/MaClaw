package docgen

import (
	"strings"
	"testing"

	ttfcore "github.com/VantageDataChat/GoPDF2/fontmaker/core"
)

func resetPDFStyles(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		_ = SetCustomStyles(nil)
		_ = SetActiveStyle("")
	})
	if err := SetCustomStyles(nil); err != nil {
		t.Fatal(err)
	}
	if err := SetActiveStyle(""); err != nil {
		t.Fatal(err)
	}
}

func TestBuiltinPDFStylesAreFiveDistinctLayouts(t *testing.T) {
	resetPDFStyles(t)
	styles := builtinStyles()
	if len(styles) != 5 {
		t.Fatalf("styles = %d", len(styles))
	}
	if ActiveStyle().ID != styles[0].ID || styles[0].ID != "gazette" {
		t.Fatalf("default = %s", ActiveStyle().ID)
	}
	seenMotif := map[string]bool{}
	seenNavy := map[string]bool{}
	white := [3]uint8{255, 255, 255}
	for _, style := range styles {
		if seenMotif[style.Motif] {
			t.Fatalf("duplicate motif %s", style.Motif)
		}
		seenMotif[style.Motif] = true
		if seenNavy[style.Colors.Navy] {
			t.Fatalf("duplicate navy %s", style.Colors.Navy)
		}
		seenNavy[style.Colors.Navy] = true
		for _, hex := range []string{style.Colors.Ink, style.Colors.Navy, style.Colors.Meta, style.Colors.Quote} {
			if contrastRatio(mustRGB(hex), white) < 4.5 {
				t.Fatalf("%s %s contrast %.2f", style.ID, hex, contrastRatio(mustRGB(hex), white))
			}
		}
		if contrastRatio(mustRGB(style.Colors.Accent), white) < 3 {
			t.Fatalf("%s accent too pale", style.ID)
		}
	}
	infos := ListStyleInfo()
	if len(infos) != 5 || !infos[0].Active || infos[0].ID != "gazette" {
		t.Fatalf("list = %+v", infos[0])
	}
}

func TestStyledDocumentUsesStrongColors(t *testing.T) {
	resetPDFStyles(t)
	style := ActiveStyle()
	html := markdownToHTMLStyled(nil, "# 崇州天气预报\n\n崇州天气预报\n\n---\n\n## 一、今日天气概况\n\n| 项目 | 实况 |\n| --- | --- |\n| 天气 | 多云 |\n\n> 傍晚转阴\n\n![实况](missing-chart.png)\n", false, style)
	if strings.Contains(html, "#888") || strings.Contains(html, "#666") || strings.Contains(html, "<hr/>") || strings.Contains(html, "<th>") {
		t.Fatalf("pale markup remains: %s", html)
	}
	if !strings.Contains(html, cssHex(style.Colors.Navy)) || !strings.Contains(html, cssHex(style.Colors.Accent)) || !strings.Contains(html, styleMarkGlyph) {
		t.Fatalf("gazette chrome missing: %s", html)
	}
	if !strings.Contains(html, cssHex(style.Colors.Ink)) || !strings.Contains(html, styleRuleGlyph) {
		t.Fatalf("body ink or rule glyph missing: %s", html)
	}
	title := buildTitleHTML(Spec{Title: "崇州天气预报", Subtitle: "四川省成都市崇州市"})
	if strings.Contains(title, "#888") || !strings.Contains(title, cssHex(style.Colors.Navy)) || !strings.Contains(title, cssHex(style.Colors.Accent)) {
		t.Fatalf("title: %s", title)
	}
}

func TestFrameAndLedgerLayoutsDiffer(t *testing.T) {
	frame := markdownToHTMLStyled(nil, "## 纪要\n", false, builtinStyles()[2])
	ledger := markdownToHTMLStyled(nil, "## 清册\n", false, builtinStyles()[4])
	if !strings.Contains(frame, "text-align:center") || strings.Contains(ledger, "text-align:center") {
		t.Fatalf("frame=%s ledger=%s", frame, ledger)
	}
	if !strings.Contains(ledger, "11.5pt") || strings.Contains(frame, "11.5pt") {
		t.Fatalf("frame=%s ledger=%s", frame, ledger)
	}
}

func TestCustomStyleDarkensPaleInkAndRejectsBuiltinID(t *testing.T) {
	resetPDFStyles(t)
	pale := CustomStyle{
		ID: "harbor", Label: "港务", Summary: "深色港务报告",
		Keywords: []string{"港务"}, Motif: "顶栏",
		Colors: StyleColors{Ink: "D4D4D8", Navy: "0E2A47", Accent: "FFD7A8", Gold: "A16207", Meta: "E5E5E5", Rule: "C2410C", Quote: "7C2D12", Link: "1D4ED8", OnDark: "111111"},
	}
	got, err := pale.spec()
	if err != nil {
		t.Fatal(err)
	}
	white := [3]uint8{255, 255, 255}
	if contrastRatio(mustRGB(got.Colors.Ink), white) < 4.5 {
		t.Fatalf("ink stayed pale: %s", got.Colors.Ink)
	}
	if contrastRatio(mustRGB(got.Colors.Accent), white) < 4.5 {
		t.Fatalf("accent stayed pale: %s", got.Colors.Accent)
	}
	if contrastRatio(mustRGB(got.Colors.OnDark), mustRGB(got.Colors.Navy)) < 4.5 {
		t.Fatalf("on dark stayed dark: %s", got.Colors.OnDark)
	}
	if got.Motif != MotifMasthead {
		t.Fatalf("motif = %s", got.Motif)
	}
	taken := pale
	taken.ID = "gazette"
	if err := ValidateCustomStyle(taken); err == nil {
		t.Fatal("builtin id accepted")
	}
	if err := SetActiveStyle("missing"); err == nil {
		t.Fatal("unknown style accepted")
	}
}

func TestSetActiveStyleChangesLaterDocuments(t *testing.T) {
	resetPDFStyles(t)
	if err := SetActiveStyle("ledger"); err != nil {
		t.Fatal(err)
	}
	html := markdownToHTMLStyled(nil, "## 清册\n\n一行", false, styleForSpec(Spec{}))
	if !strings.Contains(html, cssHex(builtinStyles()[4].Colors.Navy)) || strings.Contains(html, styleMarkGlyph) {
		t.Fatalf("active style did not apply: %s", html)
	}
	title := buildTitleHTML(Spec{Title: "清册"})
	if !strings.Contains(title, "15pt") {
		t.Fatalf("ledger title size: %s", title)
	}
}

func TestStyledColorfulBodyKeepsStyleInk(t *testing.T) {
	resetPDFStyles(t)
	style := ActiveStyle()
	html := markdownToHTMLStyled(nil, "## 方法本质\n\n使用 CNN。\n\n图 1：示意\n", true, style)
	if strings.Contains(html, "#0f766e") {
		t.Fatalf("section rainbow replaced the style: %s", html)
	}
	if !strings.Contains(html, cssHex(style.Colors.Ink)) {
		t.Fatalf("ink missing: %s", html)
	}
	if !strings.Contains(html, cssHex(style.Colors.Meta)) {
		t.Fatalf("caption meta missing: %s", html)
	}
	prose := markdownToHTMLStyled(nil, "## 方法原理\n\n执行流程为： z_j = Sel_τ(q_j, S) → x_j = Asm_τ(z_j, q_j)。后面继续。\n", true, style)
	ink := cssHex(style.Colors.Ink)
	if !strings.Contains(prose, `color:#6d28d9`) {
		t.Fatalf("formula color: %s", prose)
	}
	if !strings.Contains(prose, `style="color:`+ink+`">执行流程为：</p>`) || !strings.Contains(prose, `style="color:`+ink+`">后面继续。</p>`) {
		t.Fatalf("formula prose left the style: %s", prose)
	}
	if strings.Contains(prose, "#1e40af") || strings.Contains(prose, "#0f766e") {
		t.Fatalf("section rainbow leaked into formula prose: %s", prose)
	}
}

func TestBoundStyleIgnoresALaterSelection(t *testing.T) {
	resetPDFStyles(t)
	spec := Spec{boundStyle: builtinStyles()[0]}
	if err := SetActiveStyle("ledger"); err != nil {
		t.Fatal(err)
	}
	if got := styleForSpec(spec).ID; got != "gazette" {
		t.Fatalf("bound style = %s", got)
	}
	if got := styleForSpec(Spec{}).ID; got != "ledger" {
		t.Fatalf("unbound style = %s", got)
	}
}

func TestStyledColorfulMarkUsesAccent(t *testing.T) {
	style := builtinStyles()[1]
	html := markdownToHTMLStyled(nil, "结论（可能）成立。", true, style)
	if !strings.Contains(html, `color:`+cssHex(style.Colors.Accent)+`"><b>（可能）</b>`) {
		t.Fatalf("mark: %s", html)
	}
	if strings.Contains(strings.ToLower(html), "#c2410c") {
		t.Fatalf("fixed vermilion: %s", html)
	}
	plain := markdownToHTMLWithScratch(nil, "结论（可能）成立。", true)
	if !strings.Contains(plain, `color:#c2410c"><b>（可能）</b>`) {
		t.Fatalf("unstyled mark: %s", plain)
	}
}

func TestStyledListCarriesInkForTheMarker(t *testing.T) {
	style := builtinStyles()[1]
	html := markdownToHTMLStyled(nil, "- 一项", false, style)
	want := `<span style="color:` + cssHex(style.Colors.Ink) + `"><ul>`
	if !strings.Contains(html, want) || !strings.Contains(html, "</ul></span>") {
		t.Fatalf("list: %s", html)
	}
	plain := markdownToHTML("- 一项")
	if strings.Contains(plain, "<span") || !strings.Contains(plain, "<ul><li>一项</li></ul>") {
		t.Fatalf("unstyled list: %s", plain)
	}
}

func TestStyledCodeAndMissingImageUseInk(t *testing.T) {
	style := builtinStyles()[3]
	ink := cssHex(style.Colors.Ink)
	code := markdownToHTMLStyled(nil, "~~~\nscore := 1\n~~~", false, style)
	if !strings.Contains(code, "color:"+ink) || strings.Contains(code, "#1f2937") {
		t.Fatalf("code: %s", code)
	}
	plainCode := markdownToHTML("~~~\nscore := 1\n~~~")
	if !strings.Contains(plainCode, "color:#1f2937") {
		t.Fatalf("unstyled code: %s", plainCode)
	}
	image := markdownToHTMLStyled(nil, "![实况](missing-chart.png)", false, style)
	if !strings.Contains(image, `<p style="color:`+ink+`"><b>图片：</b>实况</p>`) {
		t.Fatalf("label: %s", image)
	}
	if strings.Contains(image, "#666") || !strings.Contains(image, cssHex(style.Colors.Meta)) {
		t.Fatalf("note: %s", image)
	}
	remote := markdownToHTMLStyled(nil, "![实况](https://example.com/a.png)", false, style)
	if !strings.Contains(remote, `<p style="color:`+ink+`"><b>图片：</b>实况</p>`) || strings.Contains(remote, "#666") {
		t.Fatalf("remote: %s", remote)
	}
	plainImage := markdownToHTML("![实况](missing-chart.png)")
	if !strings.Contains(plainImage, "<p><b>图片：</b>实况</p>") || !strings.Contains(plainImage, "color:#666") {
		t.Fatalf("unstyled image: %s", plainImage)
	}
}

func TestStyledLinkUsesStyleColorInsideTheAnchor(t *testing.T) {
	style := builtinStyles()[1]
	html := markdownToHTMLStyled(nil, "见 [公报](https://example.com/a)。", false, style)
	want := `<a href="https://example.com/a"><span style="color:` + cssHex(style.Colors.Link) + `">公报</span></a>`
	if !strings.Contains(html, want) {
		t.Fatalf("link: %s", html)
	}
	bare := markdownToHTMLStyled(nil, "https://example.com/a", true, style)
	if !strings.Contains(bare, `<a href="https://example.com/a"><span style="color:`+cssHex(style.Colors.Link)+`">https://example.com/a</span></a>`) {
		t.Fatalf("bare link: %s", bare)
	}
	plain := markdownToHTML("见 [公报](https://example.com/a)。")
	if strings.Contains(plain, "<span") || !strings.Contains(plain, `<a href="https://example.com/a">公报</a>`) {
		t.Fatalf("unstyled link: %s", plain)
	}
}

func TestEmbeddedFacesContainStyleGlyphs(t *testing.T) {
	gen := New()
	if !gen.HasFont() {
		t.Skip("no embeddable font")
	}
	switch styleRuleGlyph {
	case "━", "—", "－":
	default:
		t.Fatalf("rule glyph %q", styleRuleGlyph)
	}
	for name, data := range map[string][]byte{
		"regular": gen.fontRegularData,
		"bold":    gen.fontBoldData,
	} {
		var parser ttfcore.TTFParser
		if err := parser.ParseFontData(data); err != nil {
			t.Fatal(err)
		}
		chars := parser.Chars()
		for _, r := range []rune(styleMarkGlyph + styleRuleGlyph) {
			if _, ok := chars[int(r)]; !ok {
				t.Fatalf("%s missing U+%04X", name, r)
			}
		}
	}
}
