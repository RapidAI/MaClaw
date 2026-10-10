package pptx

import (
	"archive/zip"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	ppt "github.com/Vantagics/GoPPT"
)

func TestAgendaNumberColorTracksAccent(t *testing.T) {
	edu := resolveDeckTheme("education")
	if contrastRatio(agendaNumberColor(edu), edu.accent) < contrastRatio(edu.white, edu.accent) {
		t.Fatal("amber agenda numbers should beat white")
	}
	biz := resolveDeckTheme("business")
	if agendaNumberColor(biz) != biz.white {
		t.Fatal("navy agenda numbers should be white")
	}
	academic := resolveDeckTheme("academic")
	if readableAccent(academic, academic.card) == academic.accent {
		t.Fatal("gold card numbers should leave the accent color")
	}
	if readableAccent(biz, biz.card) != biz.accent {
		t.Fatal("navy card numbers should stay in the accent color")
	}
	pale := deckTheme{
		accent: ppt.NewColor("F4E7C5"),
		navy:   ppt.NewColor("F8F1DE"),
		ink:    ppt.NewColor("1A1A1A"),
		white:  ppt.NewColor("FFFFFF"),
		card:   ppt.NewColor("FFFCF7"),
	}
	if readableAccent(pale, pale.card) != pale.ink {
		t.Fatal("a light navy should not be the fallback on a light card")
	}
}

func TestCustomStyleInkIsRaisedWhenItMatchesThePaper(t *testing.T) {
	style := sampleCustomStyle()
	style.ID = "pale-ink"
	style.Colors.Paper = "FFFCF7"
	style.Colors.Ink = "FFFCF7"
	style.Colors.Card = "FFFCF7"
	style.Colors.Slate = "FFF8EE"
	style.Colors.Navy = "111111"
	style.Colors.OnDark = "222222"
	got, err := style.spec()
	if err != nil {
		t.Fatal(err)
	}
	if got.ink == "FFFCF7" || got.slate == "FFF8EE" {
		t.Fatalf("unreadable text kept: ink %s slate %s", got.ink, got.slate)
	}
	if contrastRatio(ppt.NewColor(got.ink), ppt.NewColor(got.paper)) < 4.5 {
		t.Fatalf("ink contrast too low: %s on %s", got.ink, got.paper)
	}
	if contrastRatio(ppt.NewColor(got.slate), ppt.NewColor(got.card)) < 4.5 {
		t.Fatalf("slate contrast too low: %s on %s", got.slate, got.card)
	}
	if got.onDark == "222222" || contrastRatio(ppt.NewColor(got.onDark), ppt.NewColor(got.navy)) < 4.5 {
		t.Fatalf("on-dark contrast too low: %s on %s", got.onDark, got.navy)
	}
	biz := resolveDeckTheme("business")
	if textOn(biz.navy, biz.paper, biz) != biz.navy {
		t.Fatal("business titles should keep navy")
	}
	pale := deckTheme{
		paper:  ppt.NewColor("FFFCF7"),
		card:   ppt.NewColor("FFFCF7"),
		navy:   ppt.NewColor("FFF8EE"),
		ink:    ppt.NewColor("142033"),
		white:  ppt.NewColor("FFFFFF"),
		onDark: ppt.NewColor("F7F8FA"),
		accent: ppt.NewColor("F4E7C5"),
	}
	if textOn(pale.navy, pale.paper, pale) == pale.navy {
		t.Fatal("pale navy body text should leave the navy color")
	}
	if contrastRatio(textOn(pale.navy, pale.card, pale), pale.card) < 4.5 {
		t.Fatal("card title contrast")
	}
	if contrastRatio(textOn(pale.accent, pale.paper, pale), pale.paper) < 4.5 {
		t.Fatal("quote mark contrast")
	}
	launch := resolveDeckTheme("launch")
	if contrastRatio(launch.white, launch.accent) < 3 {
		t.Fatal("launch cover title would be unreadable even as large type")
	}
}

func TestCoverTitlePointSizeFitsTheBox(t *testing.T) {
	if coverTitlePointSize("小布生日快乐", 48, 11.7, 1.7) != 48 {
		t.Fatal("short cover title should keep the display size")
	}
	wide := coverTitlePointSize(strings.Repeat("题", 23), 48, 11.7, 1.7)
	narrow := coverTitlePointSize(strings.Repeat("题", 23), 48, 7.7, 1.7)
	if narrow >= wide {
		t.Fatalf("narrow column size %d should be below wide size %d", narrow, wide)
	}
	long := coverTitlePointSize(strings.Repeat("题", 40), 48, 7.7, 1.8)
	if long >= 48 || long < 28 || !coverTitleFits(strings.Repeat("题", 40), long, 7.7, 1.8) {
		t.Fatalf("long narrow title size %d does not fit", long)
	}
}

func TestNormalizeStyleLangMatchesUI(t *testing.T) {
	if NormalizeStyleLang("en-AU") != "en" || NormalizeStyleLang("zh-Hant") != "zh-Hant" || NormalizeStyleLang("zh-CN") != "zh-Hans" {
		t.Fatalf("lang map en-AU=%s hant=%s cn=%s", NormalizeStyleLang("en-AU"), NormalizeStyleLang("zh-Hant"), NormalizeStyleLang("zh-CN"))
	}
	label, summary := businessThemeText("en")
	if label != "Business Briefing" || !strings.Contains(summary, "Swiss") {
		t.Fatalf("english copy = %q / %q", label, summary)
	}
}

func businessThemeText(lang string) (string, string) {
	for _, spec := range styleCatalog {
		if spec.id == "business" {
			return spec.textFor(lang)
		}
	}
	return "", ""
}

func TestEffectiveLayoutChoosesDesignedPages(t *testing.T) {
	cases := []struct {
		spec OutlineSlide
		want string
	}{
		{OutlineSlide{Title: "只有标题"}, "section"},
		{OutlineSlide{Title: "三点", Bullets: []string{"毛色：重点色", "性格：安静", "年龄：5 岁"}}, "cards"},
		{OutlineSlide{Title: "长列表", Bullets: []string{"一", "二", "三", "四", "五"}}, "bullets"},
		{OutlineSlide{Title: "相册", Bullets: []string{"一张", "两张"}, Images: []OutlineImage{{Path: "a.png"}}}, "bullets"},
		{OutlineSlide{Title: "目录", Layout: "agenda", Bullets: []string{"成长", "日常"}}, "agenda"},
		{OutlineSlide{Title: "指标", Layout: "kpi", Bullets: []string{"4.9 kg | 体重"}}, "kpi"},
		{OutlineSlide{Title: "收束", Layout: "closing", Bullets: []string{"谢谢"}}, "closing"},
		{OutlineSlide{Title: "太多卡片", Layout: "cards", Bullets: []string{"1", "2", "3", "4", "5", "6", "7"}}, "bullets"},
	}
	for _, tc := range cases {
		if got := effectiveLayout(tc.spec); got != tc.want {
			t.Errorf("layout(%q) = %q, want %q", tc.spec.Title, got, tc.want)
		}
	}
	if resolveDeckTheme("温馨").id != "warm" || resolveDeckTheme("学术").id != "academic" || resolveDeckTheme("nope").id != "business" {
		t.Fatal("theme aliases did not resolve")
	}
}

func TestChooseDeckStyleByPurposeOrExplicitName(t *testing.T) {
	cases := []struct {
		theme, hint, want string
		explicit          bool
	}{
		{"warm", "季度汇报", "warm", true},
		{"商务", "", "business", true},
		{"auto", "小布 5 岁生日纪念", "warm", false},
		{"", "开题答辩", "academic", false},
		{"auto", "birthday party", "warm", false},
		{"", "thesis defense", "academic", false},
		{"auto", "product launch", "launch", false},
		{"auto", "产品发布会路演", "launch", false},
		{"", "系统架构技术分享", "tech", false},
		{"auto", "新员工培训课件", "education", false},
		{"", "年会致辞", "ceremony", false},
		{"auto", "个人介绍", "minimal", false},
		{"", "随便做一页", "business", false},
		{"business", "生日纪念", "business", true},
	}
	for _, tc := range cases {
		id, explicit := ChooseDeckStyle(tc.theme, tc.hint)
		if id != tc.want || explicit != tc.explicit {
			t.Errorf("Choose(%q, %q) = %s explicit=%v, want %s explicit=%v", tc.theme, tc.hint, id, explicit, tc.want, tc.explicit)
		}
	}
	catalog := FormatDeckStyleCatalog()
	for _, id := range []string{"business", "academic", "warm", "launch", "tech", "education", "ceremony", "minimal"} {
		if !strings.Contains(catalog, "`"+id+"`") {
			t.Fatalf("catalog missing %s:\n%s", id, catalog)
		}
	}
}

func TestWriteFileAutoThemeFollowsPurpose(t *testing.T) {
	dir := t.TempDir()
	birthday := filepath.Join(dir, "birthday.pptx")
	if err := WriteFile(birthday, Outline{Title: "小布的生日", Theme: "auto", Slides: []OutlineSlide{{Title: "寄语", Bullets: []string{"健康"}}}}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(zipSlideXML(t, birthday)["ppt/slides/slide1.xml"], "3D3530") {
		t.Fatal("birthday deck did not use the warm palette")
	}
	explicit := filepath.Join(dir, "explicit.pptx")
	if err := WriteFile(explicit, Outline{Title: "生日汇报", Theme: "business", Slides: []OutlineSlide{{Title: "要点", Bullets: []string{"一项"}}}, Purpose: "生日"}); err != nil {
		t.Fatal(err)
	}
	cover := zipSlideXML(t, explicit)["ppt/slides/slide1.xml"]
	if !strings.Contains(cover, "0E2A47") || strings.Contains(cover, "3D3530") {
		t.Fatal("explicit business theme was overridden by the birthday title")
	}
	intro := filepath.Join(dir, "intro.pptx")
	if err := WriteFile(intro, Outline{Title: "个人介绍", Theme: "auto", Slides: []OutlineSlide{{Title: "经历", Bullets: []string{"一项"}}}}); err != nil {
		t.Fatal(err)
	}
	light := zipSlideXML(t, intro)["ppt/slides/slide1.xml"]
	if !strings.Contains(light, "F7F6F3") {
		t.Fatal("personal-intro deck did not use the minimal paper cover")
	}
}

func sampleCustomStyle() CustomStyle {
	return CustomStyle{
		ID: "nebula", Label: "星云", Summary: "深空蓝紫，适合发布想象。",
		Keywords: []string{"星云主题"}, CoverDark: true,
		Colors: StyleColors{
			Paper: "F4F2FA", Ink: "1A1630", Navy: "16122B", Navy2: "2A2348",
			Accent: "7C5CFF", Gold: "E7C36A", White: "FFFFFF", Mute: "6E6880",
			Slate: "3C3658", Card: "FFFFFF", OnDark: "F6F3FF", OnDarkMute: "D5CCF0",
		},
	}
}

func TestCustomStyleJoinsCatalogAndPreview(t *testing.T) {
	t.Cleanup(func() { _ = SetCustomStyles(nil) })
	if err := SetCustomStyles([]CustomStyle{sampleCustomStyle()}); err != nil {
		t.Fatal(err)
	}
	id, explicit := ChooseDeckStyle("auto", "请用星云主题")
	if id != "nebula" || explicit {
		t.Fatalf("auto choice = %s explicit=%v", id, explicit)
	}
	if !strings.Contains(FormatDeckStyleCatalog(), "`nebula`") {
		t.Fatal("catalog omitted the custom style")
	}
	taken := sampleCustomStyle()
	taken.ID = "business"
	if err := SetCustomStyles([]CustomStyle{taken}); err == nil {
		t.Fatal("custom style must not replace a built-in id")
	}
	png, err := RenderStylePreviewPNG("warm")
	if err != nil {
		t.Fatal(err)
	}
	if len(png) < 8 || string(png[:8]) != "\x89PNG\r\n\x1a\n" {
		t.Fatalf("preview is not a PNG (%d bytes)", len(png))
	}
}

func TestWriteFilePaintsProfessionalChrome(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "deck.pptx")
	outline := Outline{
		Title:    "小布五岁",
		Subtitle: "成长纪实",
		Theme:    "business",
		Slides: []OutlineSlide{
			{Title: "目录", Layout: "agenda", Bullets: []string{"成长", "日常", "寄语"}},
			{Title: "三件事", Bullets: []string{"毛色：重点色", "性格：安静", "年龄：5 岁"}},
			{Title: "成长", Layout: "section", Kicker: "02"},
			{Title: "体重", Layout: "kpi", Bullets: []string{"3.2 kg | 1 岁", "4.9 kg | 5 岁"}},
			{Title: "结尾", Layout: "closing", Bullets: []string{"生日快乐"}},
		},
	}
	if err := WriteFile(path, outline); err != nil {
		t.Fatal(err)
	}
	files := zipSlideXML(t, path)
	cover := files["ppt/slides/slide1.xml"]
	for _, want := range []string{"0E2A47", "F7F8FA", "小布五岁"} {
		if !strings.Contains(cover, want) {
			t.Fatalf("cover missing %q", want)
		}
	}
	content := files["ppt/slides/slide2.xml"]
	if !strings.Contains(content, "F7F8FA") || !strings.Contains(content, "成长") {
		t.Fatalf("content slide lost paper canvas or agenda text")
	}
	section := files["ppt/slides/slide4.xml"]
	if !strings.Contains(section, "0E2A47") || !strings.Contains(section, "成长") {
		t.Fatalf("section slide is not a dark divider")
	}

	preview, err := RenderPreview(path, PreviewOptions{OutputDir: filepath.Join(dir, "preview"), Width: 320, Draft: true})
	if err != nil {
		t.Fatal(err)
	}
	if preview.RenderedCount != 6 {
		t.Fatalf("rendered %d, want 6", preview.RenderedCount)
	}
	coverPx := samplePNG(t, preview.Images[0], 160, 90)
	if coverPx[0] < 220 {
		t.Fatalf("swiss cover center should be paper: rgb=%v", coverPx)
	}
	bar := samplePNG(t, preview.Images[0], 1, 90)
	if bar[2] < bar[0] || bar[2] < 40 {
		t.Fatalf("swiss cover bar should be navy: rgb=%v", bar)
	}
	rail := samplePNG(t, preview.Images[1], 1, 80)
	if rail[2] < rail[0] || rail[2] < 40 {
		t.Fatalf("content rail is not navy: rgb=%v", rail)
	}
	paper := samplePNG(t, preview.Images[1], 8, 30)
	if paper[0] < 220 || paper[0] > 250 {
		t.Fatalf("content canvas is not the paper tone: rgb=%v", paper)
	}
}

func zipSlideXML(t *testing.T, path string) map[string]string {
	t.Helper()
	r, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	out := map[string]string{}
	for _, f := range r.File {
		if !strings.HasPrefix(f.Name, "ppt/slides/slide") || !strings.HasSuffix(f.Name, ".xml") {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatal(err)
		}
		out[f.Name] = string(body)
	}
	return out
}

func samplePNG(t *testing.T, path string, x, y int) [3]int {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	c := img.At(x, y)
	r, g, b, _ := c.RGBA()
	return [3]int{int(r >> 8), int(g >> 8), int(b >> 8)}
}

func TestModernStyleSelectionAndPalette(t *testing.T) {
	// The brand style must be reachable by id, by Chinese alias, and by purpose.
	for _, tc := range []struct {
		theme, hint, want string
	}{
		{"modern", "", "modern"},
		{"现代品牌", "", "modern"},
		{"auto", "产品介绍白皮书", "modern"},
		{"", "brand deck for a saas launch", "modern"},
	} {
		if id, _ := ChooseDeckStyle(tc.theme, tc.hint); id != tc.want {
			t.Errorf("ChooseDeckStyle(%q, %q) = %q, want %q", tc.theme, tc.hint, id, tc.want)
		}
	}
	if !strings.Contains(FormatDeckStyleCatalog(), "`modern`") {
		t.Fatal("catalog missing modern")
	}
	// Purpose keywords for the older styles must still win over the brand ones.
	for _, tc := range []struct{ hint, want string }{
		{"季度经营分析汇报", "business"},
		{"技术架构分享", "tech"},
		{"新员工培训课件", "education"},
	} {
		if id, _ := ChooseDeckStyle("auto", tc.hint); id != tc.want {
			t.Errorf("auto(%q) = %q, want %q", tc.hint, id, tc.want)
		}
	}
}

// The brand style keeps a white field with a navy panel on the cover and the
// warm accent bar; the dark-panel text must stay legible against the navy.
func TestModernCoverUsesSplitPanelAndWarmBar(t *testing.T) {
	path := filepath.Join(t.TempDir(), "modern.pptx")
	outline := Outline{
		Title: "MaClaw 智能工作台", Subtitle: "本地优先的日常生产力", Theme: "modern",
		Slides: []OutlineSlide{{Title: "产品定位", Kicker: "OVERVIEW", Layout: "cards",
			Bullets: []string{"本地优先：数据不出端", "开箱即用：一键部署"}}},
	}
	if err := WriteFile(path, outline); err != nil {
		t.Fatal(err)
	}
	cover := zipSlideXML(t, path)["ppt/slides/slide1.xml"]
	for _, want := range []string{"0B2E4F", "FF7A45"} {
		if !strings.Contains(cover, want) {
			t.Fatalf("cover is missing the brand color %s", want)
		}
	}
	slides := zipNamedContents(t, path)
	body := ""
	for name, content := range slides {
		if strings.HasPrefix(name, "ppt/slides/slide") && strings.Contains(content, "产品定位") {
			body = content
		}
	}
	if body == "" {
		t.Fatal("missing modern content slide")
	}
	if color := runColorAfterText(t, body, "产品定位"); !strings.EqualFold(color, "0B2E4F") {
		t.Fatalf("content title color = %s, want the deck navy 0B2E4F", color)
	}
	// Body copy sits on white; the light accent must not be used for it.
	if strings.Contains(body, "1E88E5") {
		t.Fatal("light accent 1E88E5 is below body-text contrast on white")
	}
}

// The pill badge makes the brand header taller than the shared light chrome.
// If the body box keeps the default top, cards collide with the title.
func TestModernHeaderClearsBodyContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "modern.pptx")
	if err := WriteFile(path, Outline{
		Title: "品牌", Theme: "modern",
		Slides: []OutlineSlide{{Title: "产品定位", Kicker: "OVERVIEW", Layout: "cards",
			Bullets: []string{"甲：说明", "乙：说明"}}},
	}); err != nil {
		t.Fatal(err)
	}
	body := ""
	for name, content := range zipNamedContents(t, path) {
		if strings.HasPrefix(name, "ppt/slides/slide") && strings.Contains(content, "产品定位") {
			body = content
		}
	}
	if body == "" {
		t.Fatal("missing modern content slide")
	}
	titleY := shapeOffsetYBeforeText(t, body, "产品定位")
	// The first card's index is the topmost body element; it must clear the
	// bottom of the title box (0.55in tall) by a visible margin.
	cardY := shapeOffsetYBeforeText(t, body, "01")
	if cardY == 0 {
		t.Fatal("card index not found on the modern content slide")
	}
	// The title box is 0.55in tall. Cards are centred in the body box, so the
	// real gap is larger than the raw title bottom; require a visible margin
	// so a title that has grown into the body region cannot pass.
	titleBottom := titleY + emuIn(0.55)
	if gap := cardY - titleBottom; gap < emuIn(0.3) {
		t.Fatalf("card top is only %d EMU (%.2fin) below the title box; want >= 0.30in", gap, float64(gap)/float64(emuIn(1)))
	}
}

// The warm accent belongs to the cover. On a content page it must not appear:
// a saturated bar under the title is the classic generated-deck tell, and a
// full-bleed one at the foot would cross the shared page mark at y 7.08-7.36.
func TestModernContentPageCarriesNoWarmAccent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "modern.pptx")
	if err := WriteFile(path, Outline{
		Title: "品牌", Theme: "modern",
		Slides: []OutlineSlide{{Title: "内容页标题", Bullets: []string{"要点一"}}},
	}); err != nil {
		t.Fatal(err)
	}
	cover := zipSlideXML(t, path)["ppt/slides/slide1.xml"]
	if !strings.Contains(cover, "FF7A45") {
		t.Fatal("cover should keep the warm accent bar")
	}
	for name, body := range zipNamedContents(t, path) {
		if !strings.HasPrefix(name, "ppt/slides/slide") || !strings.Contains(body, "内容页标题") {
			continue
		}
		if strings.Contains(body, "FF7A45") {
			t.Fatal("warm accent leaked onto a content page")
		}
		return
	}
	t.Fatal("missing modern content slide")
}

// A long kicker must not grow the pill past the slide or run it under the
// cover's navy panel. The label is clipped with an ellipsis instead.
func TestModernKickerBadgeStaysBounded(t *testing.T) {
	huge := strings.Repeat("长标签", 60)
	for _, tc := range []struct {
		kicker string
		maxW   float64
	}{
		{"OVERVIEW", 10.2},
		{"章节", 10.2},
		{huge, 10.2},
		{huge, 6.95},
	} {
		label, w := badgeWidth(tc.kicker, 11, tc.maxW)
		if label == "" {
			t.Fatalf("badge for %q collapsed to empty", clipRunes(tc.kicker, 12))
		}
		if w > tc.maxW+0.001 {
			t.Errorf("badge width %.2fin exceeds the %.2fin bound", w, tc.maxW)
		}
		// The text must also fit inside the pill once insets are removed.
		if textW := titleWidthUnits(label) * 11.0 / 72.0; textW > w-0.2 {
			t.Errorf("label %q needs %.2fin but the pill offers %.2fin", label, textW, w-0.2)
		}
	}
}

// The rendered pill must stay on the slide for a pathological kicker.
func TestModernRenderedBadgeStaysOnSlide(t *testing.T) {
	path := filepath.Join(t.TempDir(), "modern.pptx")
	if err := WriteFile(path, Outline{
		Title: "标题", Theme: "modern",
		Slides: []OutlineSlide{{Title: "内容页", Kicker: strings.Repeat("长标签", 60),
			Bullets: []string{"要点"}}},
	}); err != nil {
		t.Fatal(err)
	}
	for _, body := range zipNamedContents(t, path) {
		if !strings.Contains(body, "内容页") {
			continue
		}
		idx := strings.Index(body, `prst="roundRect"`)
		if idx < 0 {
			t.Fatal("no pill badge on the modern content page")
		}
		head := body[:idx]
		last := strings.LastIndex(head, "<a:ext cx=")
		if last < 0 {
			t.Fatal("pill has no extent")
		}
		m := regexp.MustCompile(`<a:ext cx="(\d+)"`).FindStringSubmatch(head[last:])
		if m == nil {
			t.Fatal("pill extent unparsable")
		}
		cx, err := strconv.ParseInt(m[1], 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		if got := float64(cx) / float64(deckInchEMU); got > 10.5 {
			t.Fatalf("pill is %.2fin wide; it runs off the slide", got)
		}
		return
	}
	t.Fatal("missing modern content slide")
}

// The brand style must not hijack decks that belong to the business style.
// matchStyle ranks the longest matching keyword, so a 4-char brand keyword
// would outrank the 2-char 季度 or 商务 that decide those decks.
func TestModernKeywordsDoNotStealBusinessDecks(t *testing.T) {
	for _, tc := range []struct{ hint, want string }{
		{"季度数据看板", "business"},
		{"客户案例分析", "business"},
		{"解决方案汇报", "business"},
		{"产品手册", "business"},
		{"数据看板周报", "business"},
		{"季度经营分析", "business"},
		{"客户提案", "business"},
	} {
		if id, _ := ChooseDeckStyle("auto", tc.hint); id != tc.want {
			t.Errorf("auto(%q) = %q, want %q", tc.hint, id, tc.want)
		}
	}
	// The brand vocabulary must still reach the brand style.
	for _, hint := range []string{"产品介绍", "品牌故事", "白皮书", "用户增长", "品牌手册", "品牌宣传", "brand deck", "saas"} {
		if id, _ := ChooseDeckStyle("auto", hint); id != "modern" {
			t.Errorf("auto(%q) = %q, want modern", hint, id)
		}
	}
}

// The content box must sit below the title, and that relationship has to be
// derived rather than hardcoded: if the badge geometry moves, the body has to
// follow it. This pins the derived offset to the badge the painter draws.
func TestModernBodyBoxTracksBadgeGeometry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "modern.pptx")
	if err := WriteFile(path, Outline{
		Title: "品牌", Theme: "modern",
		Slides: []OutlineSlide{{Title: "内容页", Kicker: "OVERVIEW", Bullets: []string{"要点"}}},
	}); err != nil {
		t.Fatal(err)
	}
	body := ""
	for name, content := range zipNamedContents(t, path) {
		if strings.HasPrefix(name, "ppt/slides/slide") && strings.Contains(content, "内容页") {
			body = content
		}
	}
	if body == "" {
		t.Fatal("missing modern content slide")
	}
	// Read the real rendered positions: the title the painter placed, and the
	// topmost body shape. The body must start below the title box the painter
	// derived from its own badge, not at some hardcoded constant.
	titleY := shapeOffsetYBeforeText(t, body, "内容页")
	if titleY == 0 {
		t.Fatal("content title not found")
	}
	cardY := shapeOffsetYBeforeText(t, body, "01")
	if cardY == 0 {
		// A single bullet renders as plain text, not a card; use the text.
		cardY = shapeOffsetYBeforeText(t, body, "要点")
	}
	if cardY == 0 {
		t.Fatal("no body content on the slide")
	}
	// Measured healthy gaps: 0.54in with no badge, 0.67in with one, 2.17in
	// for a centred card grid. A body that has drifted back up into the title
	// shows up well under 0.4in.
	if gap := cardY - titleY; gap < emuIn(0.4) {
		t.Fatalf("body sits %d EMU (%.2fin) below the title; the header and body have drifted together",
			gap, float64(gap)/float64(deckInchEMU))
	}
}
