package pptx

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"

	ppt "github.com/Vantagics/GoPPT"
)

// deckTheme is one shipped visual system: cover, paper, accent, and type colors.
// coverDark is false only for styles whose cover stays on the paper canvas.
type deckTheme struct {
	id                      string
	coverDark               bool
	paper, ink, navy, navy2 ppt.Color
	accent, gold, white     ppt.Color
	mute, slate, card       ppt.Color
	onDark, onDarkMute      ppt.Color
}

// styleSpec is one entry in the style catalog. aliases match an explicit
// theme name. keywords match a purpose, title, or subtitle when the caller
// asks for automatic selection.
type styleSpec struct {
	id, label, summary     string
	labelEn, labelHant     string
	summaryEn, summaryHant string
	aliases, keywords      []string
	coverDark              bool
	paper, ink             string
	navy, navy2            string
	accent, gold           string
	white, mute, slate     string
	card                   string
	onDark, onDarkMute     string
}

func (s styleSpec) theme() deckTheme {
	return deckTheme{
		id: s.id, coverDark: s.coverDark,
		paper: themeColor(s.paper), ink: themeColor(s.ink),
		navy: themeColor(s.navy), navy2: themeColor(s.navy2),
		accent: themeColor(s.accent), gold: themeColor(s.gold),
		white: themeColor(s.white), mute: themeColor(s.mute),
		slate: themeColor(s.slate), card: themeColor(s.card),
		onDark: themeColor(s.onDark), onDarkMute: themeColor(s.onDarkMute),
	}
}

func themeColor(hex string) ppt.Color { return ppt.NewColor(hex) }

// blend mixes a toward b by t (0 keeps a, 1 gives b) in sRGB space. Used for
// hairlines and quiet text that sit between two palette colors.
func blend(a, b ppt.Color, t float64) ppt.Color {
	mix := func(x, y uint8) uint8 {
		v := math.Round(float64(x)*(1-t) + float64(y)*t)
		if v < 0 {
			v = 0
		}
		if v > 255 {
			v = 255
		}
		return uint8(v)
	}
	return ppt.NewColor(fmt.Sprintf("%02X%02X%02X", mix(a.GetRed(), b.GetRed()), mix(a.GetGreen(), b.GetGreen()), mix(a.GetBlue(), b.GetBlue())))
}

// styleCatalog is the built-in order shown to the user. The first entry is
// also the fallback when a deck names no style and its purpose matches nothing.
// Custom styles saved from Settings are appended after this list.
var styleCatalog = []styleSpec{
	{
		id: "business", label: "商务汇报", summary: "瑞士网格。白底、左侧色条、结论式标题。适合经营汇报。",
		labelEn: "Business Briefing", labelHant: "商務匯報",
		summaryEn: "Swiss grid. White field, left rule, action titles.", summaryHant: "瑞士網格。白底、左側色條、結論式標題。",
		aliases: []string{"business", "商务", "商务汇报", "汇报", "business briefing"}, coverDark: true,
		keywords: []string{"季度", "经营分析", "客户提案", "商务", "董事会", "复盘", "工作汇报", "季度汇报", "quarterly review", "board meeting", "client proposal", "business review"},
		paper:    "F7F8FA", ink: "142033", navy: "0E2A47", navy2: "1B3A5C",
		accent: "0E2A47", gold: "8AA0B8", white: "FFFFFF", mute: "5C6B7A",
		slate: "3E4C5A", card: "FFFFFF", onDark: "F7F8FA", onDarkMute: "C5D0DC",
	},
	{
		id: "consulting", label: "咨询顾问风", summary: "麦肯锡式。藏青主导、结论式标题、细分隔线。适合战略与商业分析。",
		labelEn: "Consulting Deck", labelHant: "諮詢顧問風",
		summaryEn: "Consulting grade. Deep navy, action titles, hairline rules.", summaryHant: "麥肯錫式。藏青主導、結論式標題、細分隔線。",
		aliases: []string{"consulting", "咨询", "咨询顾问", "顾问", "麦肯锡", "consulting deck", "mckinsey"}, coverDark: true,
		keywords: []string{"战略", "咨询", "顾问", "行业分析", "商业分析", "尽调", "竞对分析", "strategy", "consulting", "industry analysis", "due diligence"},
		paper:    "FFFFFF", ink: "1A1A1A", navy: "061F32", navy2: "0E3352",
		accent: "061F32", gold: "08A6F6", white: "FFFFFF", mute: "6B7680",
		slate: "33404D", card: "F4F6F8", onDark: "FFFFFF", onDarkMute: "B9C6D2",
	},
	{
		id: "executive", label: "高管汇报", summary: "午夜蓝。沉稳大字、冰蓝点缀、克制留白。适合董事会与高管层。",
		labelEn: "Executive Brief", labelHant: "高管匯報",
		summaryEn: "Midnight navy, ice-blue accents, executive restraint.", summaryHant: "午夜藍。沉穩大字、冰藍點綴。",
		aliases: []string{"executive", "高管", "高管汇报", "董事会汇报", "executive brief", "board deck"}, coverDark: true,
		keywords: []string{"董事会", "高管", "决策", "年度报告", "战略汇报", "经营决策", "executive", "board deck", "annual report"},
		paper:    "FFFFFF", ink: "1C2430", navy: "1E2761", navy2: "2A3575",
		accent: "1E2761", gold: "CADCFC", white: "FFFFFF", mute: "5F6B7A",
		slate: "39414E", card: "F5F7FB", onDark: "FFFFFF", onDarkMute: "C9D6F2",
	},
	{
		id: "academic", label: "学术答辩", summary: "深色学院风。褐底金框，内页羊皮纸。适合答辩。",
		labelEn: "Academic Defense", labelHant: "學術答辯",
		summaryEn: "Dark academia. Brown and gold cover, parchment pages.", summaryHant: "深色學院風。褐底金框，內頁羊皮紙。",
		aliases: []string{"academic", "学术", "学术答辩", "scholar", "academic defense"}, coverDark: true,
		keywords: []string{"论文", "答辩", "课题", "开题", "文献", "学术", "综述", "thesis", "dissertation", "research paper", "literature review"},
		paper:    "F6F0E6", ink: "2A2118", navy: "1A1208", navy2: "2C2114",
		accent: "C9A84C", gold: "8A7340", white: "FFFCF7", mute: "7A6A58",
		slate: "4A3C2E", card: "FFFCF7", onDark: "F3E6C8", onDarkMute: "C4B08A",
	},
	{
		id: "warm", label: "温馨纪念", summary: "北欧留白。米色大字、细线。适合纪念与家庭。",
		labelEn: "Warm Keepsake", labelHant: "溫馨紀念",
		summaryEn: "Nordic whitespace. Cream field, large type, one thin rule.", summaryHant: "北歐留白。米色大字、細線。",
		aliases: []string{"warm", "温馨", "温馨纪念", "warmth", "soft", "warm keepsake"}, coverDark: true,
		keywords: []string{"生日", "纪念", "婚礼", "满月", "寿辰", "温馨", "宠物", "致谢", "birthday", "wedding", "memorial", "family gathering"},
		paper:    "F4F1EC", ink: "3D3530", navy: "3D3530", navy2: "5C5148",
		accent: "8A7A6A", gold: "C4B5A5", white: "FFFCF8", mute: "8A7A6A",
		slate: "5C5148", card: "FFFCF8", onDark: "F4F1EC", onDarkMute: "8A7A6A",
	},
	{
		id: "launch", label: "发布路演", summary: "对半撞色。左朱红、右近黑，适合发布和路演。",
		labelEn: "Launch Pitch", labelHant: "發布路演",
		summaryEn: "Duotone split. Vermilion left, near-black right.", summaryHant: "對半撞色。左朱紅、右近黑。",
		aliases: []string{"launch", "发布", "发布路演", "路演", "launch pitch"}, coverDark: true,
		keywords: []string{"发布会", "路演", "融资", "产品发布", "品牌发布", "product launch", "pitch deck", "fundraising"},
		paper:    "F6F5F3", ink: "1A1A1A", navy: "141414", navy2: "2A2A2A",
		accent: "E24A2A", gold: "F0C14A", white: "FFFFFF", mute: "6A6A6A",
		slate: "3A3A3A", card: "FFFFFF", onDark: "F7F4F1", onDarkMute: "E7C7BE",
	},
	{
		id: "tech", label: "技术分享", summary: "蓝图。深蓝网格与角标，适合架构和技术分享。",
		labelEn: "Tech Talk", labelHant: "技術分享",
		summaryEn: "Blueprint. Navy grid, corner marks, cyan type.", summaryHant: "藍圖。深藍網格與角標。",
		aliases: []string{"tech", "技术", "技术分享", "tech talk"}, coverDark: true,
		keywords: []string{"技术分享", "架构", "研发", "开源", "系统设计", "技术方案", "open source", "engineering talk", "system architecture"},
		paper:    "F4F7FB", ink: "0D2240", navy: "0D2240", navy2: "16365F",
		accent: "3D8FD4", gold: "8FB8DC", white: "FFFFFF", mute: "5C6E82",
		slate: "2C455F", card: "FFFFFF", onDark: "E7F3FC", onDarkMute: "9FC4E0",
	},
	{
		id: "education", label: "教学课件", summary: "课程页眉。顶部色带下课次标题，适合课件和培训。",
		labelEn: "Lesson Deck", labelHant: "教學課件",
		summaryEn: "Course masthead. A color band, then the lesson title.", summaryHant: "課程頁眉。頂部色帶下課次標題。",
		aliases: []string{"education", "教学", "课件", "教学课件", "lesson deck"}, coverDark: true,
		keywords: []string{"课件", "培训", "课堂", "教学", "课程", "教案", "lecture", "training course", "classroom", "lesson plan"},
		paper:    "FFF8EF", ink: "2A241C", navy: "1E3A5F", navy2: "2C4C73",
		accent: "E39B2B", gold: "F3D48A", white: "FFFFFF", mute: "6E6256",
		slate: "4A4036", card: "FFFCF7", onDark: "FFF6E8", onDarkMute: "F0D7B0",
	},
	{
		id: "ceremony", label: "典礼年会", summary: "装饰艺术。黑金双框、标题居中，适合典礼和年会。",
		labelEn: "Ceremony", labelHant: "典禮年會",
		summaryEn: "Art deco. Black and gold double frame, centered type.", summaryHant: "裝飾藝術。黑金雙框、標題居中。",
		aliases: []string{"ceremony", "典礼", "年会", "典礼年会", "awards ceremony"}, coverDark: true,
		keywords: []string{"年会", "典礼", "颁奖", "致辞", "开幕", "闭幕", "纪念典礼", "awards ceremony", "annual gala", "opening remarks"},
		paper:    "F8F1E3", ink: "2A2114", navy: "0E0A05", navy2: "2A2114",
		accent: "D4AA2A", gold: "B8960C", white: "FFFCF5", mute: "8A7848",
		slate: "5C4A28", card: "FFFCF5", onDark: "F6E7C1", onDarkMute: "C4B07A",
	},
	{
		id: "minimal", label: "极简", summary: "纯字排。一根通栏黑线，没有色块。适合个人介绍。",
		labelEn: "Minimal", labelHant: "極簡",
		summaryEn: "Type only. One black rule, no color blocks.", summaryHant: "純字排。一根通欄黑線，沒有色塊。",
		aliases: []string{"minimal", "极简", "personal introduction"}, coverDark: false,
		keywords: []string{"极简", "作品集", "简历", "个人介绍", "portfolio", "resume", "personal introduction"},
		paper:    "F7F6F3", ink: "1C1C1C", navy: "1C1C1C", navy2: "3A3A3A",
		accent: "1C1C1C", gold: "B08948", white: "FFFFFF", mute: "6E6A64",
		slate: "3F3F3C", card: "FFFFFF", onDark: "1C1C1C", onDarkMute: "5C5852",
	},
	{
		id: "modern", label: "现代品牌", summary: "浅底细网格。藏青主色、暖橙点睛、圆环徽记，适合产品介绍与品牌叙事。",
		labelEn: "Modern Brand", labelHant: "現代品牌",
		summaryEn: "Light hairline grid. Deep navy, warm orange, ring mark.", summaryHant: "淺底細網格。藏青主色、暖橙點睛、圓環徽記。",
		aliases: []string{"modern", "现代", "现代品牌", "品牌", "品牌蓝", "brand", "modern brand"}, coverDark: false,
		// Brand-only vocabulary. Deliberately avoids 解决方案 / 客户案例 /
		// 数据看板 / 产品手册: those are common in business decks, and a
		// 4-char brand keyword would outrank the 2-char 季度 or 商务 that
		// actually decides those decks.
		keywords: []string{"产品介绍", "品牌故事", "白皮书", "用户增长", "品牌手册", "品牌宣传", "brand deck", "saas"},
		paper:    "FFFFFF", ink: "16202B", navy: "0B2E4F", navy2: "123D66",
		accent: "1E88E5", gold: "FF7A45", white: "FFFFFF", mute: "5F5E5A",
		slate: "4B5563", card: "F4F7FB", onDark: "FFFFFF", onDarkMute: "A1C7EE",
	},
}

var (
	customStyleMu sync.Mutex
	customStyles  []styleSpec
)

// StyleColors is the palette a custom style must fill. Values are RRGGBB.
type StyleColors struct {
	Paper      string `json:"paper"`
	Ink        string `json:"ink"`
	Navy       string `json:"navy"`
	Navy2      string `json:"navy2"`
	Accent     string `json:"accent"`
	Gold       string `json:"gold"`
	White      string `json:"white"`
	Mute       string `json:"mute"`
	Slate      string `json:"slate"`
	Card       string `json:"card"`
	OnDark     string `json:"on_dark"`
	OnDarkMute string `json:"on_dark_mute"`
}

// CustomStyle is a user-authored palette stored outside the built-in catalog.
type CustomStyle struct {
	ID        string      `json:"id"`
	Label     string      `json:"label"`
	Summary   string      `json:"summary"`
	Keywords  []string    `json:"keywords"`
	CoverDark bool        `json:"cover_dark"`
	Colors    StyleColors `json:"colors"`
}

// StyleInfo is one row in the settings gallery and the expert's style list.
type StyleInfo struct {
	ID          string   `json:"id"`
	Label       string   `json:"label"`
	LabelEn     string   `json:"label_en,omitempty"`
	LabelHant   string   `json:"label_hant,omitempty"`
	Summary     string   `json:"summary"`
	SummaryEn   string   `json:"summary_en,omitempty"`
	SummaryHant string   `json:"summary_hant,omitempty"`
	Keywords    []string `json:"keywords"`
	CoverDark   bool     `json:"cover_dark"`
	Builtin     bool     `json:"builtin"`
	Accent      string   `json:"accent,omitempty"`
}

func (s CustomStyle) spec() (styleSpec, error) {
	id := strings.ToLower(strings.TrimSpace(s.ID))
	if !validStyleID(id) {
		return styleSpec{}, fmt.Errorf("ppt_style_id_invalid")
	}
	if builtinStyleTaken(id) {
		return styleSpec{}, fmt.Errorf("ppt_style_id_taken")
	}
	label := strings.TrimSpace(s.Label)
	if !plainStyleText(label) || len([]rune(label)) > 16 {
		return styleSpec{}, fmt.Errorf("ppt_style_label_invalid")
	}
	summary := strings.TrimSpace(s.Summary)
	if !plainStyleText(summary) || len([]rune(summary)) > 80 {
		return styleSpec{}, fmt.Errorf("ppt_style_summary_invalid")
	}
	keywords := make([]string, 0, len(s.Keywords))
	seenKW := map[string]bool{}
	for _, keyword := range s.Keywords {
		keyword = strings.TrimSpace(keyword)
		if !plainStyleText(keyword) || len([]rune(keyword)) < 2 || len([]rune(keyword)) > 16 || seenKW[keyword] {
			continue
		}
		seenKW[keyword] = true
		keywords = append(keywords, keyword)
		if len(keywords) == 8 {
			break
		}
	}
	if len(keywords) == 0 {
		return styleSpec{}, fmt.Errorf("ppt_style_keywords_required")
	}
	colors := s.Colors
	for _, field := range []string{colors.Paper, colors.Ink, colors.Navy, colors.Navy2, colors.Accent, colors.Gold, colors.White, colors.Mute, colors.Slate, colors.Card, colors.OnDark, colors.OnDarkMute} {
		if !validHexColor(field) {
			return styleSpec{}, fmt.Errorf("ppt_style_color_invalid")
		}
	}
	paper := hexColor(colors.Paper)
	card := hexColor(colors.Card)
	navy := hexColor(colors.Navy)
	return styleSpec{
		id: id, label: label, summary: summary, keywords: keywords, coverDark: s.CoverDark,
		aliases:    []string{id, label},
		paper:      paper,
		ink:        contrastingHex(colors.Ink, paper),
		navy:       navy,
		navy2:      hexColor(colors.Navy2),
		accent:     hexColor(colors.Accent),
		gold:       hexColor(colors.Gold),
		white:      hexColor(colors.White),
		mute:       hexColor(colors.Mute),
		slate:      contrastingHex(colors.Slate, card),
		card:       card,
		onDark:     contrastingHex(colors.OnDark, navy),
		onDarkMute: contrastingHex(colors.OnDarkMute, navy),
	}, nil
}

func hexColor(s string) string {
	return strings.ToUpper(strings.TrimPrefix(strings.TrimSpace(s), "#"))
}

// contrastingHex keeps candidate when it already reads on bg. Otherwise it
// picks the dark or light fallback with the stronger contrast.
func contrastingHex(candidate, bg string) string {
	if contrastRatio(ppt.NewColor(candidate), ppt.NewColor(bg)) >= 4.5 {
		return hexColor(candidate)
	}
	dark := ppt.NewColor("142033")
	light := ppt.NewColor("F7F8FA")
	if contrastRatio(dark, ppt.NewColor(bg)) >= contrastRatio(light, ppt.NewColor(bg)) {
		return "142033"
	}
	return "F7F8FA"
}

// plainStyleText rejects line breaks and backticks so a saved style cannot
// rewrite the expert prompt or the style line added to a user message.
func plainStyleText(s string) bool {
	if s == "" || strings.ContainsAny(s, "\r\n\t`") {
		return false
	}
	return true
}

func validStyleID(id string) bool {
	if len(id) < 2 || len(id) > 32 {
		return false
	}
	for i, r := range id {
		switch {
		case r >= 'a' && r <= 'z':
		case (r >= '0' && r <= '9') || r == '-':
			if i == 0 {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func validHexColor(raw string) bool {
	raw = strings.TrimPrefix(strings.TrimSpace(raw), "#")
	if len(raw) != 6 {
		return false
	}
	for _, r := range raw {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'f', r >= 'A' && r <= 'F':
		default:
			return false
		}
	}
	return true
}

func builtinStyleTaken(id string) bool {
	key := strings.ToLower(strings.TrimSpace(id))
	for _, spec := range styleCatalog {
		if key == spec.id {
			return true
		}
		for _, alias := range spec.aliases {
			if key == strings.ToLower(alias) {
				return true
			}
		}
	}
	return false
}

// ValidateCustomStyle checks a user palette without installing it.
func ValidateCustomStyle(style CustomStyle) error {
	_, err := style.spec()
	return err
}

// SetCustomStyles replaces the user-authored styles the renderer and the
// expert catalog can see. Nil clears them. A colliding or invalid style
// rejects the whole update and leaves the previous set in place.
func SetCustomStyles(styles []CustomStyle) error {
	specs := make([]styleSpec, 0, len(styles))
	seen := map[string]bool{}
	for _, style := range styles {
		spec, err := style.spec()
		if err != nil {
			return err
		}
		if seen[spec.id] {
			return fmt.Errorf("ppt_style_id_taken")
		}
		seen[spec.id] = true
		specs = append(specs, spec)
	}
	customStyleMu.Lock()
	customStyles = specs
	customStyleMu.Unlock()
	return nil
}

func allStyles() []styleSpec {
	customStyleMu.Lock()
	extra := append([]styleSpec(nil), customStyles...)
	customStyleMu.Unlock()
	if len(extra) == 0 {
		return styleCatalog
	}
	out := make([]styleSpec, 0, len(styleCatalog)+len(extra))
	out = append(out, styleCatalog...)
	return append(out, extra...)
}

// ListStyleInfo returns built-in styles first, then custom styles.
func ListStyleInfo() []StyleInfo {
	styles := allStyles()
	out := make([]StyleInfo, 0, len(styles))
	for i, spec := range styles {
		out = append(out, StyleInfo{
			ID: spec.id, Label: spec.label, Summary: spec.summary,
			LabelEn: spec.labelEn, LabelHant: spec.labelHant,
			SummaryEn: spec.summaryEn, SummaryHant: spec.summaryHant,
			Keywords: append([]string(nil), spec.keywords...), CoverDark: spec.coverDark,
			Builtin: i < len(styleCatalog), Accent: spec.accent,
		})
	}
	return out
}

// FormatDeckStyleCatalog is the numbered list the PPT expert shows when the
// user should pick a style. Ids match the theme field the renderer accepts.
func FormatDeckStyleCatalog() string {
	var b strings.Builder
	for i, spec := range allStyles() {
		name := spec.label
		if spec.labelEn != "" {
			name = spec.label + " / " + spec.labelEn
		}
		fmt.Fprintf(&b, "%d. `%s` %s — %s\n", i+1, spec.id, name, spec.summary)
	}
	return strings.TrimRight(b.String(), "\n")
}

func lookupStyle(name string) (styleSpec, bool) {
	key := strings.ToLower(strings.TrimSpace(name))
	if key == "" || isAutoTheme(key) {
		return styleSpec{}, false
	}
	for _, spec := range allStyles() {
		if key == spec.id {
			return spec, true
		}
		for _, alias := range spec.aliases {
			if key == strings.ToLower(alias) {
				return spec, true
			}
		}
	}
	return styleSpec{}, false
}

func isAutoTheme(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", "auto", "自动", "default", "默认":
		return true
	default:
		return false
	}
}

// matchStyle scores keywords in hint. A longer keyword beats several short
// ones, so "纪念典礼" stays ceremony and "生日纪念" stays warm. Equal scores
// keep the earlier catalog entry.
func matchStyle(hint string) (styleSpec, bool) {
	hint = strings.ToLower(hint)
	styles := allStyles()
	best := -1
	bestScore, bestLen := 0, 0
	for i, spec := range styles {
		score, longest := 0, 0
		for _, keyword := range spec.keywords {
			kw := strings.ToLower(keyword)
			if len([]rune(kw)) < 2 || !strings.Contains(hint, kw) {
				continue
			}
			n := len([]rune(kw))
			score += n
			if n > longest {
				longest = n
			}
		}
		if score == 0 {
			continue
		}
		if longest > bestLen || (longest == bestLen && score > bestScore) {
			best = i
			bestScore = score
			bestLen = longest
		}
	}
	if best < 0 {
		return styleSpec{}, false
	}
	return styles[best], true
}

// resolveDeckTheme maps an explicit style name. Unknown names fall back to
// business; automatic selection is ChooseDeckStyle / chooseOutlineTheme.
func resolveDeckTheme(name string) deckTheme {
	if spec, ok := lookupStyle(name); ok {
		return spec.theme()
	}
	return styleCatalog[0].theme()
}

// ChooseDeckStyle reports the style id for an explicit theme or, when theme
// is auto/empty/unknown, for hint text (purpose, title, subtitle). explicit
// is true when the theme name itself selected the style.
func ChooseDeckStyle(theme, hint string) (id string, explicit bool) {
	if spec, ok := lookupStyle(theme); ok {
		return spec.id, true
	}
	text := hint
	if !isAutoTheme(theme) {
		text = strings.TrimSpace(theme + " " + hint)
	}
	if spec, ok := matchStyle(text); ok {
		return spec.id, false
	}
	return styleCatalog[0].id, false
}

func chooseOutlineTheme(outline Outline) deckTheme {
	id, _ := ChooseDeckStyle(outline.Theme, styleHint(outline))
	return resolveDeckTheme(id)
}

func styleHint(outline Outline) string {
	parts := []string{outline.Purpose, outline.Title, outline.Subtitle}
	for _, slide := range outline.Slides {
		parts = append(parts, slide.Title, slide.Kicker)
	}
	return strings.Join(parts, " ")
}

// RenderStylePreviewPNG draws the cover of one style so Settings can show it.
// The image is a real slide render, not a flat color swatch.
// NormalizeStyleLang maps a UI language to zh-Hans, zh-Hant, or en.
func NormalizeStyleLang(lang string) string {
	raw := strings.ToLower(strings.TrimSpace(lang))
	switch {
	case raw == "en" || strings.HasPrefix(raw, "en-"):
		return "en"
	case raw == "zh-hant" || raw == "zh-tw" || raw == "zh-hk" || raw == "zh-mo" || strings.HasPrefix(raw, "zh-hant-"):
		return "zh-Hant"
	default:
		return "zh-Hans"
	}
}

func (s styleSpec) textFor(lang string) (label, summary string) {
	switch NormalizeStyleLang(lang) {
	case "en":
		label, summary = s.labelEn, s.summaryEn
	case "zh-Hant":
		label, summary = s.labelHant, s.summaryHant
	}
	if label == "" {
		label = s.label
	}
	if summary == "" {
		summary = s.summary
	}
	return label, summary
}

func RenderStylePreviewPNG(styleID string) ([]byte, error) {
	return renderStylePreviewPNG(styleID, "zh-Hans")
}

// RenderStylePreviewPNGLang draws the cover using the style name for lang.
func RenderStylePreviewPNGLang(styleID, lang string) ([]byte, error) {
	return renderStylePreviewPNG(styleID, lang)
}

func renderStylePreviewPNG(styleID, lang string) ([]byte, error) {
	spec, ok := lookupStyle(styleID)
	if !ok {
		return nil, fmt.Errorf("ppt_style_unknown")
	}
	label, summary := spec.textFor(lang)
	dir, err := os.MkdirTemp("", "ppt-style-preview-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "preview.pptx")
	outline := Outline{
		Title:    label,
		Subtitle: summary,
		Theme:    spec.id,
		Slides: []OutlineSlide{{
			Title:   "三个要点",
			Layout:  "cards",
			Bullets: []string{"清楚：一个观点", "节奏：封面到收束", "配色：当前风格"},
		}},
	}
	if err := WriteFile(path, outline); err != nil {
		return nil, err
	}
	result, err := RenderPreview(path, PreviewOptions{
		OutputDir: filepath.Join(dir, "img"),
		Width:     480,
		MaxSlides: 1,
		Draft:     true,
	})
	if err != nil {
		return nil, err
	}
	if result == nil || len(result.Images) == 0 {
		return nil, fmt.Errorf("ppt_style_preview_empty")
	}
	return os.ReadFile(result.Images[0])
}

func emuIn(inches float64) int64 {
	return int64(inches * deckInchEMU)
}

func clipRunes(s string, n int) string {
	s = strings.TrimSpace(s)
	if n <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n == 1 {
		return "…"
	}
	return string(r[:n-1]) + "…"
}

func nonemptyBullets(bullets []string) []string {
	out := make([]string, 0, len(bullets))
	for _, bullet := range bullets {
		text := sanitizeXMLText(strings.TrimSpace(bullet))
		if text != "" {
			out = append(out, text)
		}
	}
	return out
}

func normalizeSlideLayout(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "auto", "自动":
		return "auto"
	case "bullets", "bullet", "list", "要点", "列表":
		return "bullets"
	case "cards", "card", "卡片":
		return "cards"
	case "section", "divider", "分节", "章节":
		return "section"
	case "agenda", "toc", "目录", "议程":
		return "agenda"
	case "kpi", "metrics", "指标":
		return "kpi"
	case "quote", "引述", "金句":
		return "quote"
	case "closing", "close", "end", "结尾", "结束":
		return "closing"
	default:
		return "auto"
	}
}

// effectiveLayout picks a designed page. Media (photos or charts) stays on
// the split bullet layout so the image and chart geometry is unchanged.
// Short point lists become cards; a title with no body becomes a section
// divider. Explicit layouts that cannot hold the text fall back to bullets
// so a point is never dropped on the floor.
func effectiveLayout(spec OutlineSlide) string {
	asked := normalizeSlideLayout(spec.Layout)
	texts := nonemptyBullets(spec.Bullets)
	if len(spec.Images) > 0 || len(spec.Charts) > 0 {
		return "bullets"
	}
	switch asked {
	case "cards":
		if len(texts) == 0 || len(texts) > 6 {
			return "bullets"
		}
		return "cards"
	case "agenda":
		if len(texts) == 0 || len(texts) > 8 {
			return "bullets"
		}
		return "agenda"
	case "kpi":
		if len(texts) == 0 || len(texts) > 6 {
			return "bullets"
		}
		return "kpi"
	case "auto":
		if len(texts) == 0 {
			return "section"
		}
		if cardCandidate(texts) {
			return "cards"
		}
		return "bullets"
	default:
		return asked
	}
}

func cardCandidate(texts []string) bool {
	if len(texts) < 2 || len(texts) > 4 {
		return false
	}
	for _, text := range texts {
		if len([]rune(text)) > 36 {
			return false
		}
	}
	return true
}

func splitCardLine(text string) (head, body string) {
	text = strings.TrimSpace(text)
	for _, sep := range []string{"：", ": ", " — ", " – ", " - ", " | "} {
		i := strings.Index(text, sep)
		if i <= 0 {
			continue
		}
		head = strings.TrimSpace(text[:i])
		body = strings.TrimSpace(text[i+len(sep):])
		if head != "" && len([]rune(head)) <= 16 && body != "" {
			return head, body
		}
	}
	return "", text
}

func splitKPI(text string) (value, label string) {
	text = strings.TrimSpace(text)
	for _, sep := range []string{" | ", "｜", " — ", " – "} {
		i := strings.Index(text, sep)
		if i <= 0 {
			continue
		}
		value = strings.TrimSpace(text[:i])
		label = strings.TrimSpace(text[i+len(sep):])
		if value != "" && label != "" {
			return value, label
		}
	}
	return text, ""
}

func deckFooterLabel(title string) string {
	return clipRunes(sanitizeXMLText(title), 36)
}

// badgeWidth sizes a pill badge around its label. The label is clipped so a
// long kicker cannot grow the pill past maxW and run it off the slide or under
// a cover panel. width is derived from the point size rather than a bare
// constant so the padding stays correct if the type size changes.
func badgeWidth(label string, sizePt, maxW float64) (text string, w float64) {
	const padX = 0.5
	em := sizePt / 72.0
	for len([]rune(label)) > 1 && titleWidthUnits(label)*em+padX > maxW {
		label = clipRunes(label, len([]rune(label))-1)
	}
	if label == "" {
		return "", 0
	}
	return label, titleWidthUnits(label)*em + padX
}

// paintKickerBadge draws the pill-and-label header mark and returns the y where
// the slide title should start beneath it. A nil slide still returns the
// layout position, so callers can compute geometry without drawing.
func paintKickerBadge(slide *ppt.Slide, theme deckTheme, kicker string, left, y, sizePt, maxW float64) (string, float64) {
	label, w := badgeWidth(kicker, sizePt, maxW)
	if label == "" {
		return "", 0
	}
	h := sizePt / 72.0 * 1.9
	if slide != nil {
		roundRectPill(slide, emuIn(left), emuIn(y), emuIn(w), emuIn(h), theme.card)
		deckText(slide, emuIn(left), emuIn(y+(h-sizePt/72.0*1.3)/2), emuIn(w), emuIn(sizePt/72.0*1.3),
			label, int(sizePt), true, theme.navy, ppt.HorizontalCenter)
	}
	return label, y + h + 0.18
}

func rect(slide *ppt.Slide, x, y, w, h int64, fill ppt.Color) {
	if slide == nil || w <= 0 || h <= 0 {
		return
	}
	sh := slide.CreateAutoShape()
	sh.SetAutoShapeType(ppt.AutoShapeRectangle)
	sh.SetSolidFill(fill)
	sh.SetBorder(ppt.NewBorder())
	sh.SetOffsetX(x)
	sh.SetOffsetY(y)
	sh.SetWidth(w)
	sh.SetHeight(h)
}

func roundRect(slide *ppt.Slide, x, y, w, h int64, fill ppt.Color) {
	if slide == nil || w <= 0 || h <= 0 {
		return
	}
	sh := slide.CreateAutoShape()
	sh.SetAutoShapeType(ppt.AutoShapeRoundedRect)
	sh.SetAdjustValue("adj", 8000)
	sh.SetSolidFill(fill)
	sh.SetBorder(ppt.NewBorder())
	sh.SetOffsetX(x)
	sh.SetOffsetY(y)
	sh.SetWidth(w)
	sh.SetHeight(h)
}

func paintSolidBackground(slide *ppt.Slide, fill ppt.Color) {
	if slide == nil {
		return
	}
	bg := ppt.NewFill()
	bg.SetSolid(fill)
	slide.SetBackground(bg)
}

func deckText(slide *ppt.Slide, x, y, w, h int64, text string, size int, bold bool, color ppt.Color, align ppt.HorizontalAlignment) {
	text = sanitizeXMLText(strings.TrimSpace(text))
	if slide == nil || text == "" || w <= 0 || h <= 0 {
		return
	}
	box := slide.CreateRichTextShape()
	box.SetOffsetX(x).SetOffsetY(y).SetWidth(w).SetHeight(h)
	box.SetWordWrap(true)
	para := box.GetActiveParagraph()
	para.GetAlignment().SetHorizontal(align)
	run := para.CreateTextRun(text)
	font := run.GetFont().SetBold(bold).SetSize(size).SetName(defaultDeckFont).SetColor(color)
	font.NameEA = defaultDeckFont
}

func deckLines(slide *ppt.Slide, x, y, w, h int64, lines []string, size int, bold bool, color ppt.Color, align ppt.HorizontalAlignment, spaceAfter int) {
	lines = nonemptyBullets(lines)
	if slide == nil || len(lines) == 0 || w <= 0 || h <= 0 {
		return
	}
	box := slide.CreateRichTextShape()
	box.SetOffsetX(x).SetOffsetY(y).SetWidth(w).SetHeight(h)
	box.SetWordWrap(true)
	for i, line := range lines {
		para := box.GetActiveParagraph()
		if i > 0 {
			para = box.CreateParagraph()
		}
		para.GetAlignment().SetHorizontal(align)
		para.SetSpaceAfter(spaceAfter)
		run := para.CreateTextRun(line)
		font := run.GetFont().SetBold(bold).SetSize(size).SetName(defaultDeckFont).SetColor(color)
		font.NameEA = defaultDeckFont
	}
}

func paintPageMark(slide *ppt.Slide, page, total int, color ppt.Color) {
	if page <= 0 || total <= 0 {
		return
	}
	deckText(slide, emuIn(11.05), emuIn(7.08), emuIn(1.8), emuIn(0.28),
		fmt.Sprintf("%02d  /  %02d", page, total), 12, false, color, ppt.HorizontalRight)
}

func paintDarkCanvas(slide *ppt.Slide, theme deckTheme) {
	// One calm dark field. Rails, bands, and edge stripes read as template
	// filler, so the cover relies on type scale instead of decoration.
	paintSolidBackground(slide, theme.navy)
}

// paintFeatureCanvas is the cover, section, and closing canvas. Dark styles
// use the navy field; a light style such as minimal stays on paper.
func paintFeatureCanvas(slide *ppt.Slide, theme deckTheme) {
	if theme.coverDark {
		paintDarkCanvas(slide, theme)
		return
	}
	paintLightCanvas(slide, theme)
}

func featureColors(theme deckTheme) (title, sub, rule, kicker ppt.Color) {
	if theme.coverDark {
		return theme.onDark, theme.onDarkMute, theme.gold, theme.accent
	}
	return theme.navy, theme.slate, theme.gold, theme.ink
}

func paintLightCanvas(slide *ppt.Slide, theme deckTheme) {
	paintSolidBackground(slide, theme.paper)
}

type contentBox struct {
	x, y, w, h int64
}

func lightContentBox(hasKicker bool) contentBox {
	top := 1.16
	if hasKicker {
		top = 1.36
	}
	const bottom = 6.90
	const left = 0.48
	const right = 0.42
	return contentBox{
		x: emuIn(left),
		y: emuIn(top),
		w: deckSlideWidth - emuIn(left) - emuIn(right),
		h: emuIn(bottom - top),
	}
}

func paintLightChrome(slide *ppt.Slide, theme deckTheme, kicker, title, footer string, page, total int) contentBox {
	switch theme.id {
	case "consulting":
		return paintConsultingContent(slide, theme, kicker, title, footer, page, total)
	case "academic":
		return paintRuledContent(slide, theme, kicker, title, footer, page, total, false)
	case "warm":
		return paintWarmContent(slide, theme, kicker, title, footer, page, total)
	case "launch":
		return paintBandContent(slide, theme, kicker, title, footer, page, total, theme.navy, theme.accent)
	case "tech":
		return paintFramedContent(slide, theme, kicker, title, footer, page, total)
	case "education":
		return paintBandContent(slide, theme, kicker, title, footer, page, total, theme.accent, theme.navy)
	case "ceremony":
		return paintRuledContent(slide, theme, kicker, title, footer, page, total, true)
	case "minimal":
		return paintMinimalContent(slide, theme, kicker, title, footer, page, total)
	case "modern":
		return paintModernContent(slide, theme, kicker, title, footer, page, total)
	default:
		return paintBusinessContent(slide, theme, kicker, title, footer, page, total)
	}
}

func paintBusinessContent(slide *ppt.Slide, theme deckTheme, kicker, title, footer string, page, total int) contentBox {
	paintLightCanvas(slide, theme)
	return paintContentTitle(slide, theme, kicker, title, footer, page, total, 0.48, theme.accent, true)
}

func paintRuledContent(slide *ppt.Slide, theme deckTheme, kicker, title, footer string, page, total int, centerRule bool) contentBox {
	paintSolidBackground(slide, theme.paper)
	rule := theme.accent
	if centerRule {
		rule = theme.gold
	}
	// Keep the pair above the title. A rule at 0.28 sits inside the title box.
	rect(slide, emuIn(0.7), emuIn(0.1), deckSlideWidth-emuIn(1.4), emuIn(0.016), rule)
	rect(slide, emuIn(0.7), emuIn(0.15), deckSlideWidth-emuIn(1.4), emuIn(0.01), rule)
	box := paintContentTitle(slide, theme, kicker, title, footer, page, total, 0.7, rule, false)
	rect(slide, emuIn(0.7), emuIn(6.95), deckSlideWidth-emuIn(1.4), emuIn(0.012), rule)
	return box
}

func paintWarmContent(slide *ppt.Slide, theme deckTheme, kicker, title, footer string, page, total int) contentBox {
	// Paper stays visible so the white agenda and metric cards do not disappear.
	paintSolidBackground(slide, theme.paper)
	rect(slide, 0, 0, deckSlideWidth, emuIn(0.08), theme.accent)
	return paintContentTitle(slide, theme, kicker, title, footer, page, total, 0.72, theme.accent, false)
}

func paintBandContent(slide *ppt.Slide, theme deckTheme, kicker, title, footer string, page, total int, band, stripe ppt.Color) contentBox {
	paintSolidBackground(slide, theme.paper)
	rect(slide, 0, 0, deckSlideWidth, emuIn(0.16), band)
	rect(slide, 0, emuIn(0.16), deckSlideWidth, emuIn(0.045), stripe)
	return paintContentTitle(slide, theme, kicker, title, footer, page, total, 0.55, stripe, false)
}

func paintFramedContent(slide *ppt.Slide, theme deckTheme, kicker, title, footer string, page, total int) contentBox {
	paintSolidBackground(slide, theme.paper)
	strokeFrame(slide, emuIn(0.22), emuIn(0.18), deckSlideWidth-emuIn(0.44), deckSlideHeight-emuIn(0.36), emuIn(0.018), theme.accent)
	return paintContentTitle(slide, theme, kicker, title, footer, page, total, 0.55, theme.accent, false)
}

func paintMinimalContent(slide *ppt.Slide, theme deckTheme, kicker, title, footer string, page, total int) contentBox {
	paintSolidBackground(slide, theme.paper)
	return paintContentTitle(slide, theme, kicker, title, footer, page, total, 0.7, theme.navy, false)
}

// paintModernContent is the hairline-grid chrome: a white field under one
// faint full-bleed rule, with the kicker set in a pill badge. Hierarchy comes
// from type scale and whitespace, as in the other light styles.
func paintModernContent(slide *ppt.Slide, theme deckTheme, kicker, title, footer string, page, total int) contentBox {
	paintSolidBackground(slide, theme.paper)
	rule := blend(theme.paper, theme.mute, 0.35)
	const left = 0.7
	// One hairline above the header. It stops short of the page mark so the
	// rule never crosses the number.
	rect(slide, 0, emuIn(0.34), deckSlideWidth, emuIn(0.01), rule)
	kicker = sanitizeXMLText(strings.TrimSpace(kicker))
	titleY, titleSize := 0.62, 30
	titleBoxH := 0.55
	if label, below := paintKickerBadge(slide, theme, kicker, left, 0.52, 11, 10.2); label != "" {
		kicker = label
		titleY, titleSize = below, 28
	}
	title = sanitizeXMLText(strings.TrimSpace(title))
	if title != "" {
		deckText(slide, emuIn(left), emuIn(titleY), emuIn(11.9), emuIn(titleBoxH), title, titleSize, true, theme.navy, ppt.HorizontalLeft)
		// No accent bar under the title: a short emphasis bar is the classic
		// generated-deck tell, and the pill badge already carries the accent.
	}
	if footer != "" {
		deckText(slide, emuIn(left), emuIn(7.08), emuIn(9.0), emuIn(0.28), footer, 11, false, textOn(theme.mute, theme.paper, theme), ppt.HorizontalLeft)
	}
	paintPageMark(slide, page, total, textOn(theme.mute, theme.paper, theme))
	// The pill badge makes this header taller than the shared chrome assumes,
	// so the body starts below the title rather than colliding with it. The
	// offset is derived from the same titleY the badge returned, so the two
	// cannot drift apart if the badge geometry changes.
	box := lightContentBox(kicker != "")
	if kicker != "" {
		box.y = emuIn(titleY + titleBoxH + 0.12)
		box.h = deckSlideHeight - box.y - emuIn(0.6)
	}
	return box
}

// paintRingMark draws the concentric ring-and-core mark: an outer ring with a
// smaller core disc, echoing the badge geometry of the reference board.
func paintRingMark(slide *ppt.Slide, theme deckTheme, x, y, d float64) {
	oval(slide, emuIn(x), emuIn(y), emuIn(d), emuIn(d), blend(theme.navy, theme.paper, 0.86))
	oval(slide, emuIn(x+d*0.2), emuIn(y+d*0.2), emuIn(d*0.6), emuIn(d*0.6), theme.gold)
}

func oval(slide *ppt.Slide, x, y, w, h int64, fill ppt.Color) {
	if slide == nil || w <= 0 || h <= 0 {
		return
	}
	sh := slide.CreateAutoShape()
	sh.SetAutoShapeType(ppt.AutoShapeEllipse)
	sh.SetSolidFill(fill)
	sh.SetBorder(ppt.NewBorder())
	sh.SetOffsetX(x)
	sh.SetOffsetY(y)
	sh.SetWidth(w)
	sh.SetHeight(h)
}

func paintContentTitle(slide *ppt.Slide, theme deckTheme, kicker, title, footer string, page, total int, left float64, rule ppt.Color, rail bool) contentBox {
	titleY := 0.34
	titleSize := 30
	if rail {
		titleY = 0.28
	}
	kicker = sanitizeXMLText(strings.TrimSpace(kicker))
	if kicker != "" {
		deckText(slide, emuIn(left), emuIn(0.24), emuIn(12.0), emuIn(0.24), kicker, 12, true, textOn(rule, theme.paper, theme), ppt.HorizontalLeft)
		titleY = 0.50
		titleSize = 28
	}
	title = sanitizeXMLText(strings.TrimSpace(title))
	if title != "" {
		deckText(slide, emuIn(left), emuIn(titleY), emuIn(12.0), emuIn(0.55), title, titleSize, true, textOn(theme.navy, theme.paper, theme), ppt.HorizontalLeft)
		// No accent line under the title: a short emphasis bar is the classic
		// generated-deck tell. Hierarchy comes from type scale and whitespace.
	}
	if footer != "" {
		deckText(slide, emuIn(left), emuIn(7.08), emuIn(9.0), emuIn(0.28), footer, 11, false, textOn(theme.mute, theme.paper, theme), ppt.HorizontalLeft)
	}
	paintPageMark(slide, page, total, textOn(theme.mute, theme.paper, theme))
	return lightContentBox(kicker != "")
}

// paintConsultingContent is the consulting-grade chrome: white field, cyan
// kicker, action title in navy, one full-width hairline as the structural
// divider between the title block and the body. The hairline is layout
// grammar, not decoration, so it spans the text column and hugs the title.
func paintConsultingContent(slide *ppt.Slide, theme deckTheme, kicker, title, footer string, page, total int) contentBox {
	paintLightCanvas(slide, theme)
	const left = 0.55
	kicker = sanitizeXMLText(strings.TrimSpace(kicker))
	titleY := 0.34
	if kicker != "" {
		deckText(slide, emuIn(left), emuIn(0.24), emuIn(12.0), emuIn(0.24), kicker, 11, true, textOn(theme.gold, theme.paper, theme), ppt.HorizontalLeft)
		titleY = 0.52
	}
	title = sanitizeXMLText(strings.TrimSpace(title))
	if title != "" {
		deckText(slide, emuIn(left), emuIn(titleY), emuIn(12.0), emuIn(0.55), title, 28, true, theme.navy, ppt.HorizontalLeft)
	}
	hairY := titleY + 0.58
	rect(slide, emuIn(left), emuIn(hairY), deckSlideWidth-emuIn(left+0.45), emuIn(0.012), blend(theme.slate, theme.paper, 0.72))
	if footer != "" {
		deckText(slide, emuIn(left), emuIn(7.08), emuIn(9.0), emuIn(0.28), footer, 10, false, textOn(theme.mute, theme.paper, theme), ppt.HorizontalLeft)
	}
	paintPageMark(slide, page, total, textOn(theme.mute, theme.paper, theme))
	top := 1.34
	if kicker != "" {
		top = 1.44
	}
	return contentBox{
		x: emuIn(left),
		y: emuIn(top),
		w: deckSlideWidth - emuIn(left) - emuIn(0.45),
		h: emuIn(6.90 - top),
	}
}

func buildTitleSlide(slide *ppt.Slide, outline Outline, theme deckTheme, total int) {
	if slide == nil {
		return
	}
	paintStyleCover(slide, theme, outline.Title, outline.Subtitle, "", 1, total)
}

func buildBulletBody(slide *ppt.Slide, region contentBox, bullets []string, theme deckTheme) {
	texts := nonemptyBullets(bullets)
	if len(texts) == 0 {
		return
	}
	body := slide.CreateRichTextShape()
	body.SetOffsetX(region.x).SetOffsetY(region.y).SetWidth(region.w).SetHeight(region.h)
	body.SetWordWrap(true)
	for i, text := range texts {
		para := body.GetActiveParagraph()
		if i > 0 {
			para = body.CreateParagraph()
		}
		para.SetBullet(ppt.NewBullet().SetCharBullet("•", defaultDeckFont).SetColor(readableAccent(theme, theme.paper)))
		para.SetSpaceAfter(180)
		run := para.CreateTextRun(text)
		font := run.GetFont().SetSize(20).SetName(defaultDeckFont).SetColor(textOn(theme.ink, theme.paper, theme))
		font.NameEA = defaultDeckFont
	}
}

func buildSectionSlide(slide *ppt.Slide, spec OutlineSlide, theme deckTheme, page, total, section int) {
	paintSectionField(slide, theme, spec.Title, strings.Join(nonemptyBullets(spec.Bullets), " · "), spec.Kicker, page, total, section)
}

func buildClosingSlide(slide *ppt.Slide, spec OutlineSlide, theme deckTheme, page, total int) {
	paintSectionField(slide, theme, spec.Title, strings.Join(nonemptyBullets(spec.Bullets), " · "), spec.Kicker, page, total, 0)
}

// paintSectionField draws the dark divider canvas. sectionNo >= 1 marks a real
// section divider (consulting shows its giant index); 0 is a closing page.
func paintSectionField(slide *ppt.Slide, theme deckTheme, title, subtitle, kicker string, page, total, sectionNo int) {
	paintSolidBackground(slide, theme.navy)
	titleX := 0.7
	if sectionNo > 0 && theme.id == "consulting" {
		// Consulting divider: a giant tone-on-tone section index anchors the
		// left, the section title sits to its right. Tone-on-tone, not decoration.
		deckText(slide, emuIn(0.62), emuIn(1.85), emuIn(3.1), emuIn(2.2), fmt.Sprintf("%02d", sectionNo), 130, true, theme.navy2, ppt.HorizontalLeft)
		titleX = 3.55
		if kicker != "" {
			deckText(slide, emuIn(titleX), emuIn(2.35), emuIn(9), emuIn(0.3), kicker, 14, true, textOn(theme.gold, theme.navy, theme), ppt.HorizontalLeft)
		}
		deckText(slide, emuIn(titleX), emuIn(2.75), emuIn(8.9), emuIn(1.5), title, coverTitlePointSize(title, 40, 8.9, 1.5), true, textOn(theme.onDark, theme.navy, theme), ppt.HorizontalLeft)
		deckText(slide, emuIn(titleX), emuIn(4.35), emuIn(8.9), emuIn(0.8), subtitle, 18, false, textOn(theme.onDarkMute, theme.navy, theme), ppt.HorizontalLeft)
		paintPageMark(slide, page, total, textOn(theme.onDarkMute, theme.navy, theme))
		return
	}
	if kicker != "" {
		deckText(slide, emuIn(titleX), emuIn(2.15), emuIn(11.5), emuIn(0.3), kicker, 14, true, textOn(theme.gold, theme.navy, theme), ppt.HorizontalLeft)
	}
	deckText(slide, emuIn(titleX), emuIn(2.55), emuIn(11.6), emuIn(1.5), title, coverTitlePointSize(title, 44, 11.6, 1.5), true, textOn(theme.onDark, theme.navy, theme), ppt.HorizontalLeft)
	deckText(slide, emuIn(titleX), emuIn(4.4), emuIn(11), emuIn(0.8), subtitle, 18, false, textOn(theme.onDarkMute, theme.navy, theme), ppt.HorizontalLeft)
	paintPageMark(slide, page, total, textOn(theme.onDarkMute, theme.navy, theme))
}

func paintStyleCover(slide *ppt.Slide, theme deckTheme, title, subtitle, kicker string, page, total int) {
	title = sanitizeXMLText(strings.TrimSpace(title))
	subtitle = sanitizeXMLText(strings.TrimSpace(subtitle))
	kicker = sanitizeXMLText(strings.TrimSpace(kicker))
	switch theme.id {
	case "consulting":
		paintConsultingCover(slide, theme, title, subtitle, kicker, page, total)
	case "executive":
		paintExecutiveCover(slide, theme, title, subtitle, kicker, page, total)
	case "academic":
		paintAcademicCover(slide, theme, title, subtitle, kicker, page, total)
	case "warm":
		paintWarmCover(slide, theme, title, subtitle, kicker, page, total)
	case "launch":
		paintLaunchCover(slide, theme, title, subtitle, kicker, page, total)
	case "tech":
		paintTechCover(slide, theme, title, subtitle, kicker, page, total)
	case "education":
		paintEducationCover(slide, theme, title, subtitle, kicker, page, total)
	case "ceremony":
		paintCeremonyCover(slide, theme, title, subtitle, kicker, page, total)
	case "minimal":
		paintMinimalCover(slide, theme, title, subtitle, kicker, page, total)
	case "modern":
		paintModernCover(slide, theme, title, subtitle, kicker, page, total)
	default:
		paintBusinessCover(slide, theme, title, subtitle, kicker, page, total)
	}
}

func paintBusinessCover(slide *ppt.Slide, theme deckTheme, title, subtitle, kicker string, page, total int) {
	// Swiss consulting cover: white field, one left rule, action title.
	paintSolidBackground(slide, theme.paper)
	rect(slide, 0, 0, emuIn(0.22), deckSlideHeight, theme.navy)
	titleY, titleH := 1.85, 1.90
	if kicker != "" {
		deckText(slide, emuIn(0.7), emuIn(1.4), emuIn(11.2), emuIn(0.28), kicker, 13, true, textOn(theme.navy, theme.paper, theme), ppt.HorizontalLeft)
		titleY, titleH = 1.8, 1.7
	}
	titleSize := coverTitlePointSize(title, 44, 11.5, titleH)
	deckText(slide, emuIn(0.7), emuIn(titleY), emuIn(11.5), emuIn(titleH), title, titleSize, true, textOn(theme.ink, theme.paper, theme), ppt.HorizontalLeft)
	subY := titleY + float64(coverTitleLines(title, titleSize, 11.5))*float64(titleSize)/72*1.35 + 0.12
	deckText(slide, emuIn(0.7), emuIn(subY), emuIn(11), emuIn(0.9), subtitle, 18, false, textOn(theme.slate, theme.paper, theme), ppt.HorizontalLeft)
	paintPageMark(slide, page, total, textOn(theme.mute, theme.paper, theme))
}

func paintConsultingCover(slide *ppt.Slide, theme deckTheme, title, subtitle, kicker string, page, total int) {
	// Consulting cover: one full navy field, left-aligned action title, a
	// cyan kicker, and generous quiet space. No frames, no stripes.
	paintSolidBackground(slide, theme.navy)
	titleY, titleH := 2.15, 1.90
	if kicker != "" {
		deckText(slide, emuIn(0.7), emuIn(1.62), emuIn(11.2), emuIn(0.3), kicker, 14, true, textOn(theme.gold, theme.navy, theme), ppt.HorizontalLeft)
		titleY, titleH = 2.05, 1.75
	}
	titleSize := coverTitlePointSize(title, 46, 11.5, titleH)
	deckText(slide, emuIn(0.7), emuIn(titleY), emuIn(11.5), emuIn(titleH), title, titleSize, true, textOn(theme.onDark, theme.navy, theme), ppt.HorizontalLeft)
	subY := titleY + float64(coverTitleLines(title, titleSize, 11.5))*float64(titleSize)/72*1.35 + 0.12
	deckText(slide, emuIn(0.7), emuIn(subY), emuIn(11), emuIn(0.9), subtitle, 18, false, textOn(theme.onDarkMute, theme.navy, theme), ppt.HorizontalLeft)
	paintPageMark(slide, page, total, textOn(theme.onDarkMute, theme.navy, theme))
}

func paintExecutiveCover(slide *ppt.Slide, theme deckTheme, title, subtitle, kicker string, page, total int) {
	// Executive cover: full midnight field, ice-blue kicker, generous quiet
	// space. Restraint is the identity — no frames, no rails, no stripes.
	paintSolidBackground(slide, theme.navy)
	titleY, titleH := 2.30, 1.90
	if kicker != "" {
		deckText(slide, emuIn(0.75), emuIn(1.72), emuIn(11.2), emuIn(0.3), kicker, 14, true, textOn(theme.gold, theme.navy, theme), ppt.HorizontalLeft)
		titleY, titleH = 2.20, 1.75
	}
	titleSize := coverTitlePointSize(title, 46, 11.4, titleH)
	deckText(slide, emuIn(0.75), emuIn(titleY), emuIn(11.4), emuIn(titleH), title, titleSize, true, textOn(theme.onDark, theme.navy, theme), ppt.HorizontalLeft)
	subY := titleY + float64(coverTitleLines(title, titleSize, 11.4))*float64(titleSize)/72*1.35 + 0.12
	deckText(slide, emuIn(0.75), emuIn(subY), emuIn(11), emuIn(0.9), subtitle, 18, false, textOn(theme.onDarkMute, theme.navy, theme), ppt.HorizontalLeft)
	paintPageMark(slide, page, total, textOn(theme.onDarkMute, theme.navy, theme))
}

func paintAcademicCover(slide *ppt.Slide, theme deckTheme, title, subtitle, kicker string, page, total int) {
	// Dark academia: brown field, double gold frame, centered serif-like title.
	paintSolidBackground(slide, theme.navy)
	strokeFrame(slide, emuIn(0.38), emuIn(0.32), deckSlideWidth-emuIn(0.76), deckSlideHeight-emuIn(0.64), emuIn(0.02), theme.accent)
	strokeFrame(slide, emuIn(0.5), emuIn(0.44), deckSlideWidth-emuIn(1.0), deckSlideHeight-emuIn(0.88), emuIn(0.01), theme.gold)
	titleY, titleH := 2.05, 1.6
	if kicker != "" {
		deckText(slide, emuIn(1.2), emuIn(1.6), emuIn(10.9), emuIn(0.28), kicker, 13, true, textOn(theme.accent, theme.navy, theme), ppt.HorizontalCenter)
		titleY, titleH = 2.0, 1.5
	}
	deckText(slide, emuIn(1.3), emuIn(titleY), emuIn(10.7), emuIn(titleH), title, coverTitlePointSize(title, 42, 10.7, titleH), true, textOn(theme.onDark, theme.navy, theme), ppt.HorizontalCenter)
	rect(slide, emuIn(5.7), emuIn(titleY+titleH+0.08), emuIn(1.9), emuIn(0.025), theme.accent)
	deckText(slide, emuIn(1.8), emuIn(titleY+titleH+0.24), emuIn(9.7), emuIn(0.9), subtitle, 16, false, textOn(theme.onDarkMute, theme.navy, theme), ppt.HorizontalCenter)
	deckText(slide, emuIn(5.3), emuIn(6.45), emuIn(2.7), emuIn(0.28), pageMark(page, total), 12, false, textOn(theme.gold, theme.navy, theme), ppt.HorizontalCenter)
}

func paintWarmCover(slide *ppt.Slide, theme deckTheme, title, subtitle, kicker string, page, total int) {
	// Nordic: cream field, three dots, large quiet type, one rule near the foot.
	paintSolidBackground(slide, theme.paper)
	for i, tone := range []ppt.Color{theme.ink, theme.accent, theme.gold} {
		rect(slide, emuIn(0.85+float64(i)*0.28), emuIn(1.55), emuIn(0.12), emuIn(0.12), tone)
	}
	titleY := 2.05
	if kicker != "" {
		deckText(slide, emuIn(0.85), emuIn(1.85), emuIn(11), emuIn(0.28), kicker, 13, true, textOn(theme.accent, theme.paper, theme), ppt.HorizontalLeft)
		titleY = 2.25
	}
	deckText(slide, emuIn(0.85), emuIn(titleY), emuIn(11.4), emuIn(1.8), title, coverTitlePointSize(title, 46, 11.4, 1.8), true, theme.ink, ppt.HorizontalLeft)
	rect(slide, emuIn(0.85), emuIn(6.35), deckSlideWidth-emuIn(1.7), emuIn(0.015), theme.accent)
	deckText(slide, emuIn(0.85), emuIn(6.5), emuIn(10), emuIn(0.45), subtitle, 16, false, textOn(theme.slate, theme.paper, theme), ppt.HorizontalLeft)
	paintPageMark(slide, page, total, textOn(theme.mute, theme.paper, theme))
}

func paintLaunchCover(slide *ppt.Slide, theme deckTheme, title, subtitle, kicker string, page, total int) {
	// Duotone: exact half split, white divider, color echoed across the join.
	half := int64(deckSlideWidth) / 2
	rect(slide, 0, 0, half, deckSlideHeight, theme.accent)
	rect(slide, half, 0, int64(deckSlideWidth)-half, deckSlideHeight, theme.navy)
	rect(slide, half-emuIn(0.03), 0, emuIn(0.06), deckSlideHeight, theme.white)
	if kicker != "" {
		deckText(slide, emuIn(0.55), emuIn(1.7), emuIn(5.6), emuIn(0.3), kicker, 14, true, theme.white, ppt.HorizontalLeft)
	}
	deckText(slide, emuIn(0.55), emuIn(2.15), emuIn(5.7), emuIn(2.4), title, coverTitlePointSize(title, 44, 5.7, 2.4), true, theme.white, ppt.HorizontalLeft)
	deckText(slide, half+emuIn(0.45), emuIn(2.5), emuIn(5.5), emuIn(1.6), subtitle, 20, false, textOn(theme.accent, theme.navy, theme), ppt.HorizontalLeft)
	deckText(slide, half+emuIn(0.45), emuIn(6.7), emuIn(4), emuIn(0.3), pageMark(page, total), 12, false, textOn(theme.white, theme.navy, theme), ppt.HorizontalLeft)
}

func paintTechCover(slide *ppt.Slide, theme deckTheme, title, subtitle, kicker string, page, total int) {
	// Blueprint: navy field, coarse grid, corner brackets.
	paintSolidBackground(slide, theme.navy)
	for i := 1; i <= 6; i++ {
		rect(slide, emuIn(float64(i)*1.9), emuIn(0.35), emuIn(0.012), deckSlideHeight-emuIn(0.7), theme.navy2)
	}
	for i := 1; i <= 3; i++ {
		rect(slide, emuIn(0.35), emuIn(float64(i)*1.8), deckSlideWidth-emuIn(0.7), emuIn(0.012), theme.navy2)
	}
	strokeFrame(slide, emuIn(0.28), emuIn(0.24), deckSlideWidth-emuIn(0.56), deckSlideHeight-emuIn(0.48), emuIn(0.02), theme.accent)
	titleY, titleH := 2.1, 1.6
	if kicker != "" {
		deckText(slide, emuIn(0.7), emuIn(1.55), emuIn(11), emuIn(0.28), kicker, 13, true, textOn(theme.accent, theme.navy, theme), ppt.HorizontalLeft)
		titleY, titleH = 1.95, 1.5
	}
	deckText(slide, emuIn(0.7), emuIn(titleY), emuIn(11.4), emuIn(titleH), title, coverTitlePointSize(title, 40, 11.4, titleH), true, textOn(theme.onDark, theme.navy, theme), ppt.HorizontalLeft)
	deckText(slide, emuIn(0.7), emuIn(titleY+titleH+0.12), emuIn(11), emuIn(0.8), subtitle, 16, false, textOn(theme.onDarkMute, theme.navy, theme), ppt.HorizontalLeft)
	deckText(slide, emuIn(9.8), emuIn(6.55), emuIn(2.6), emuIn(0.28), pageMark(page, total), 12, false, textOn(theme.accent, theme.navy, theme), ppt.HorizontalRight)
}

func paintEducationCover(slide *ppt.Slide, theme deckTheme, title, subtitle, kicker string, page, total int) {
	// Course masthead: a full-width band, then the lesson title on paper.
	paintSolidBackground(slide, theme.paper)
	rect(slide, 0, 0, deckSlideWidth, emuIn(2.35), theme.navy)
	rect(slide, 0, emuIn(2.35), deckSlideWidth, emuIn(0.07), theme.accent)
	label := kicker
	if label == "" {
		label = "LESSON"
	}
	deckText(slide, emuIn(0.7), emuIn(0.7), emuIn(11), emuIn(0.35), label, 14, true, textOn(theme.gold, theme.navy, theme), ppt.HorizontalLeft)
	deckText(slide, emuIn(0.7), emuIn(1.15), emuIn(11.5), emuIn(1.0), title, coverTitlePointSize(title, 36, 11.5, 1.0), true, textOn(theme.onDark, theme.navy, theme), ppt.HorizontalLeft)
	deckText(slide, emuIn(0.7), emuIn(2.85), emuIn(11), emuIn(1.2), subtitle, 20, false, textOn(theme.ink, theme.paper, theme), ppt.HorizontalLeft)
	paintPageMark(slide, page, total, textOn(theme.mute, theme.paper, theme))
}

func paintCeremonyCover(slide *ppt.Slide, theme deckTheme, title, subtitle, kicker string, page, total int) {
	paintSolidBackground(slide, theme.navy)
	strokeFrame(slide, emuIn(0.42), emuIn(0.36), deckSlideWidth-emuIn(0.84), deckSlideHeight-emuIn(0.72), emuIn(0.02), theme.gold)
	strokeFrame(slide, emuIn(0.55), emuIn(0.48), deckSlideWidth-emuIn(1.1), deckSlideHeight-emuIn(0.96), emuIn(0.01), theme.accent)
	titleY, titleH := 2.00, 1.70
	if kicker != "" {
		deckText(slide, emuIn(1.4), emuIn(1.55), emuIn(10.5), emuIn(0.28), kicker, 14, true, textOn(theme.gold, theme.navy, theme), ppt.HorizontalCenter)
		titleY, titleH = 1.95, 1.55
	}
	deckText(slide, emuIn(1.3), emuIn(titleY), emuIn(10.7), emuIn(titleH), title, coverTitlePointSize(title, 46, 10.7, titleH), true, textOn(theme.onDark, theme.navy, theme), ppt.HorizontalCenter)
	rect(slide, emuIn(5.9), emuIn(3.85), emuIn(1.5), emuIn(0.03), theme.gold)
	deckText(slide, emuIn(1.8), emuIn(4.15), emuIn(9.7), emuIn(1.15), subtitle, 18, false, textOn(theme.onDarkMute, theme.navy, theme), ppt.HorizontalCenter)
	deckText(slide, emuIn(5.2), emuIn(6.55), emuIn(2.9), emuIn(0.28), pageMark(page, total), 12, false, textOn(theme.gold, theme.navy, theme), ppt.HorizontalCenter)
}

func paintMinimalCover(slide *ppt.Slide, theme deckTheme, title, subtitle, kicker string, page, total int) {
	paintSolidBackground(slide, theme.paper)
	if kicker != "" {
		deckText(slide, emuIn(0.75), emuIn(1.35), emuIn(11), emuIn(0.28), kicker, 12, true, textOn(theme.mute, theme.paper, theme), ppt.HorizontalLeft)
	}
	deckText(slide, emuIn(0.75), emuIn(1.7), emuIn(11.5), emuIn(1.55), title, coverTitlePointSize(title, 36, 11.5, 1.55), true, textOn(theme.navy, theme.paper, theme), ppt.HorizontalLeft)
	rect(slide, 0, emuIn(3.45), deckSlideWidth, emuIn(0.055), theme.navy)
	deckText(slide, emuIn(0.75), emuIn(3.7), emuIn(11), emuIn(1.15), subtitle, 18, false, textOn(theme.slate, theme.paper, theme), ppt.HorizontalLeft)
	deckText(slide, emuIn(10.6), emuIn(6.9), emuIn(2.1), emuIn(0.28), pageMark(page, total), 12, false, textOn(theme.mute, theme.paper, theme), ppt.HorizontalRight)
}

// paintModernCover is the brand cover: white field, a navy panel on the right
// holding the concentric ring mark, and a warm full-bleed bar that ties the
// two fields. Left-aligned type on paper carries the title.
func paintModernCover(slide *ppt.Slide, theme deckTheme, title, subtitle, kicker string, page, total int) {
	paintSolidBackground(slide, theme.paper)
	panelX := 8.05
	rect(slide, emuIn(panelX), 0, deckSlideWidth-emuIn(panelX), deckSlideHeight, theme.navy)
	// Full-bleed warm bar across the page: the one saturated mark on the cover.
	rect(slide, 0, emuIn(5.62), deckSlideWidth, emuIn(0.05), theme.gold)
	paintRingMark(slide, theme, panelX+1.05, 2.05, 3.2)
	const left = 0.7
	kicker = sanitizeXMLText(strings.TrimSpace(kicker))
	titleY, titleH := 1.95, 2.10
	if label, below := paintKickerBadge(slide, theme, kicker, left, 1.42, 11, panelX-left-0.4); label != "" {
		kicker = label
		titleY, titleH = below, 1.95
	}
	titleSize := coverTitlePointSize(title, 42, 6.9, titleH)
	deckText(slide, emuIn(left), emuIn(titleY), emuIn(6.9), emuIn(titleH), title, titleSize, true, theme.navy, ppt.HorizontalLeft)
	subY := titleY + float64(coverTitleLines(title, titleSize, 6.9))*float64(titleSize)/72*1.35 + 0.12
	deckText(slide, emuIn(left), emuIn(subY), emuIn(6.6), emuIn(0.9), subtitle, 16, false, theme.slate, ppt.HorizontalLeft)
	paintPageMark(slide, page, total, textOn(theme.mute, theme.paper, theme))
}

func strokeFrame(slide *ppt.Slide, x, y, w, h, t int64, color ppt.Color) {
	rect(slide, x, y, w, t, color)
	rect(slide, x, y+h-t, w, t, color)
	rect(slide, x, y, t, h, color)
	rect(slide, x+w-t, y, t, h, color)
}

// coverTitlePointSize picks a size that keeps the title inside widthIn x heightIn.
// A CJK glyph is about one em wide, and the line box is 1.25em. Narrow covers
// such as launch and education wrap sooner than the full-width ones.
func coverTitlePointSize(title string, base int, widthIn, heightIn float64) int {
	if titleWidthUnits(title) == 0 || widthIn <= 0 || heightIn <= 0 {
		return base
	}
	for size := base; size > 28; size -= 2 {
		if coverTitleFits(title, size, widthIn, heightIn) {
			return size
		}
	}
	return 28
}

// titleWidthUnits measures text in em units for wrap estimation. CJK and
// other full-width glyphs count one em; Latin and other narrow scripts
// average about 0.55 em.
func titleWidthUnits(text string) float64 {
	units := 0.0
	for _, r := range strings.TrimSpace(text) {
		if r >= 0x2E80 { // CJK radicals, kana, ideographs, hangul, fullwidth forms
			units++
		} else {
			units += 0.55
		}
	}
	return units
}

func coverTitleFits(title string, size int, widthIn, heightIn float64) bool {
	// PowerPoint's default text insets are 0.1" left/right and 0.05" top/bottom
	// when the shape does not set them. Fitting the outer box overflows by a glyph.
	usableH := heightIn - 0.1
	if usableH < 0.3 {
		usableH = heightIn
	}
	em := float64(size) / 72
	lines := coverTitleLines(title, size, widthIn)
	return float64(lines)*em*1.25 <= usableH
}

func pageMark(page, total int) string {
	if page <= 0 || total <= 0 {
		return ""
	}
	return fmt.Sprintf("%02d  /  %02d", page, total)
}

// coverTitleLines estimates how many lines a cover title wraps to at the
// given point size across widthIn inches, matching coverTitleFits' geometry.
// coverTitleLines estimates how many lines a cover title wraps to at the
// given point size across widthIn inches. It wraps glyph-by-glyph: CJK and
// other full-width glyphs count one em, Latin and narrow scripts about
// 0.55 em, and a glyph that does not fit moves wholly to the next line —
// matching coverTitleFits' geometry.
func coverTitleLines(title string, size int, widthIn float64) int {
	runes := []rune(strings.TrimSpace(title))
	if len(runes) == 0 {
		return 1
	}
	em := float64(size) / 72
	capacity := (widthIn - 0.2) / em
	if capacity < 1 {
		capacity = widthIn / em
		if capacity < 1 {
			capacity = 1
		}
	}
	lines, used := 1, 0.0
	for _, r := range runes {
		w := 0.55
		if r >= 0x2E80 {
			w = 1
		}
		if used+w > capacity {
			lines++
			used = 0
		}
		used += w
	}
	return lines
}

func buildQuoteSlide(slide *ppt.Slide, spec OutlineSlide, theme deckTheme, footer string, page, total int) {
	texts := nonemptyBullets(spec.Bullets)
	label := sanitizeXMLText(strings.TrimSpace(spec.Kicker))
	quote := sanitizeXMLText(strings.TrimSpace(spec.Title))
	if len(texts) > 0 {
		if label == "" {
			label = quote
		}
		quote = strings.Join(texts, "\n")
	}
	box := paintLightChrome(slide, theme, label, "", footer, page, total)
	lines := nonemptyBullets(strings.Split(quote, "\n"))
	// A short quote clinging under the mark left the lower two thirds of the
	// page empty. Center the mark+quote unit on the content box instead.
	const quoteSize = 26
	blockLines := 0
	for _, line := range lines {
		blockLines += coverTitleLines(line, quoteSize, float64(box.w)/914400)
	}
	blockH := emuIn(0.85 + float64(blockLines)*float64(quoteSize)/72*1.35)
	textY := box.y + (box.h-blockH)/2
	if textY < box.y+emuIn(0.85) {
		textY = box.y + emuIn(0.85)
	}
	markY := textY - emuIn(0.85)
	if markY < box.y {
		markY = box.y
	}
	deckText(slide, box.x, markY, emuIn(1.4), emuIn(0.9), "“", 64, true, textOn(theme.accent, theme.paper, theme), ppt.HorizontalLeft)
	deckLines(slide, box.x, textY, box.w, box.h-(textY-box.y)-emuIn(0.1), lines, quoteSize, false, textOn(theme.navy, theme.paper, theme), ppt.HorizontalLeft, 80)
}

func gridSpec(n, maxCols int) (cols, rows int) {
	if n <= 0 {
		return 1, 1
	}
	if maxCols < 1 {
		maxCols = 1
	}
	if n <= maxCols {
		return n, 1
	}
	cols = maxCols
	rows = (n + cols - 1) / cols
	return cols, rows
}

// agendaNumberColor keeps the index readable. Light chips such as gold and
// amber use the dark navy; dark chips keep white.
func agendaNumberColor(theme deckTheme) ppt.Color {
	return bestContrast(theme.accent, theme.navy, theme.ink, theme.white)
}

// readableAccent keeps a mark in the accent color when it still reads on the
// card. Otherwise it picks the navy, ink, or white that contrasts most.
func readableAccent(theme deckTheme, bg ppt.Color) ppt.Color {
	// 13pt indexes need body-text contrast, not the 3:1 large-text bar.
	if contrastRatio(theme.accent, bg) >= 4.5 {
		return theme.accent
	}
	return bestContrast(bg, theme.navy, theme.ink, theme.white)
}

// textOn keeps preferred when it clears body-text contrast on bg. A pale
// custom navy or gold otherwise yields to ink, navy, white, or on-dark type.
func textOn(preferred, bg ppt.Color, theme deckTheme) ppt.Color {
	if contrastRatio(preferred, bg) >= 4.5 {
		return preferred
	}
	return bestContrast(bg, theme.ink, theme.navy, theme.white, theme.onDark)
}

func bestContrast(bg ppt.Color, options ...ppt.Color) ppt.Color {
	best := options[0]
	bestScore := contrastRatio(best, bg)
	for _, option := range options[1:] {
		if score := contrastRatio(option, bg); score > bestScore {
			best = option
			bestScore = score
		}
	}
	return best
}

func contrastRatio(a, b ppt.Color) float64 {
	l1 := colorLuminance(a)
	l2 := colorLuminance(b)
	if l1 < l2 {
		l1, l2 = l2, l1
	}
	return (l1 + 0.05) / (l2 + 0.05)
}

func colorLuminance(c ppt.Color) float64 {
	lin := func(v float64) float64 {
		if v <= 0.04045 {
			return v / 12.92
		}
		return math.Pow((v+0.055)/1.055, 2.4)
	}
	r := lin(float64(c.GetRed()) / 255)
	g := lin(float64(c.GetGreen()) / 255)
	b := lin(float64(c.GetBlue()) / 255)
	return 0.2126*r + 0.7152*g + 0.0722*b
}

func styleUsesOpenPoints(id string) bool {
	return id == "warm" || id == "minimal"
}

func buildCards(slide *ppt.Slide, spec OutlineSlide, theme deckTheme, box contentBox) {
	texts := nonemptyBullets(spec.Bullets)
	if len(texts) == 0 {
		return
	}
	if styleUsesOpenPoints(theme.id) {
		buildOpenPoints(slide, theme, box, texts)
		return
	}
	cols, rows := gridSpec(len(texts), 3)
	if len(texts) == 4 {
		cols, rows = 2, 2
	}
	gap := emuIn(0.16)
	cardW := (box.w - gap*int64(cols-1)) / int64(cols)
	// Short points in a tall card read as an empty frame, so the card height
	// is capped — and the whole grid is then centered in the content box so
	// the page does not read as "content squeezed into the top half".
	maxH := emuIn(2.60)
	if rows > 1 {
		maxH = emuIn(1.90)
	}
	cardH := (box.h - gap*int64(rows-1)) / int64(rows)
	if cardH > maxH {
		cardH = maxH
	}
	y0 := box.y + (box.h-cardH*int64(rows)-gap*int64(rows-1))/2
	titleSize, bodySize := 18, 16
	if rows > 1 {
		titleSize, bodySize = 16, 15
	}
	for i, text := range texts {
		col := i % cols
		row := i / cols
		x := box.x + int64(col)*(cardW+gap)
		y := y0 + int64(row)*(cardH+gap)
		// A subtle tinted field separates the card; edge strips read as
		// generated filler, so emphasis comes from the index and the title.
		rect(slide, x, y, cardW, cardH, theme.card)
		head, body := splitCardLine(text)
		num := fmt.Sprintf("%02d", i+1)
		deckText(slide, x+emuIn(0.24), y+emuIn(0.18), cardW-emuIn(0.44), emuIn(0.3), num, 12, true, readableAccent(theme, theme.card), ppt.HorizontalLeft)
		textY := y + emuIn(0.5)
		if head != "" {
			deckText(slide, x+emuIn(0.22), textY, cardW-emuIn(0.4), emuIn(0.55), head, titleSize, true, textOn(theme.navy, theme.card, theme), ppt.HorizontalLeft)
			deckText(slide, x+emuIn(0.22), textY+emuIn(0.5), cardW-emuIn(0.4), cardH-emuIn(1.15), body, bodySize, false, textOn(theme.slate, theme.card, theme), ppt.HorizontalLeft)
			continue
		}
		deckText(slide, x+emuIn(0.22), textY, cardW-emuIn(0.4), cardH-emuIn(0.7), text, bodySize+1, false, textOn(theme.ink, theme.card, theme), ppt.HorizontalLeft)
	}
}

// chartSeriesColors picks the palette color for chart series i. The ladder
// goes navy → accent → secondary so the first two series of a comparison
// chart contrast immediately (two dark navies are indistinguishable), then
// falls back to quiet tones.
func chartSeriesColors(theme deckTheme, i int) ppt.Color {
	ladder := []ppt.Color{theme.navy, theme.gold, theme.navy2, theme.slate, theme.mute}
	if i < 0 {
		i = 0
	}
	return ladder[i%len(ladder)]
}

func buildOpenPoints(slide *ppt.Slide, theme deckTheme, box contentBox, texts []string) {
	n := len(texts)
	gap := emuIn(0.1)
	rowH := (box.h - gap*int64(n-1)) / int64(n)
	if rowH > emuIn(1.05) {
		rowH = emuIn(1.05)
	}
	for i, text := range texts {
		y := box.y + int64(i)*(rowH+gap)
		deckText(slide, box.x, y, emuIn(0.7), emuIn(0.36), fmt.Sprintf("%02d", i+1), 16, true, readableAccent(theme, theme.paper), ppt.HorizontalLeft)
		head, body := splitCardLine(text)
		if head != "" {
			deckText(slide, box.x+emuIn(0.85), y, box.w-emuIn(0.9), emuIn(0.36), head, 18, true, textOn(theme.navy, theme.paper, theme), ppt.HorizontalLeft)
			deckText(slide, box.x+emuIn(0.85), y+emuIn(0.38), box.w-emuIn(0.9), rowH-emuIn(0.48), body, 15, false, textOn(theme.slate, theme.paper, theme), ppt.HorizontalLeft)
		} else {
			deckText(slide, box.x+emuIn(0.85), y, box.w-emuIn(0.9), rowH-emuIn(0.16), text, 18, false, textOn(theme.ink, theme.paper, theme), ppt.HorizontalLeft)
		}
		rule := theme.accent
		if theme.id == "minimal" {
			rule = theme.navy
		}
		rect(slide, box.x, y+rowH-emuIn(0.015), box.w, emuIn(0.015), rule)
	}
}

func buildAgenda(slide *ppt.Slide, spec OutlineSlide, theme deckTheme, box contentBox) {
	texts := nonemptyBullets(spec.Bullets)
	n := len(texts)
	if n == 0 {
		return
	}
	if styleUsesOpenPoints(theme.id) {
		buildOpenPoints(slide, theme, box, texts)
		return
	}
	gap := emuIn(0.12)
	rowH := (box.h - gap*int64(n-1)) / int64(n)
	if rowH > emuIn(0.92) {
		rowH = emuIn(0.92)
	}
	y0 := box.y + (box.h-rowH*int64(n)-gap*int64(n-1))/2
	for i, text := range texts {
		y := y0 + int64(i)*(rowH+gap)
		rect(slide, box.x, y, box.w, rowH, theme.card)
		numY := y + (rowH-emuIn(0.42))/2
		rect(slide, box.x+emuIn(0.16), numY, emuIn(0.72), emuIn(0.42), theme.accent)
		deckText(slide, box.x+emuIn(0.16), numY, emuIn(0.72), emuIn(0.42), fmt.Sprintf("%02d", i+1), 14, true, agendaNumberColor(theme), ppt.HorizontalCenter)
		deckText(slide, box.x+emuIn(1.08), numY, box.w-emuIn(1.3), emuIn(0.42), text, 16, false, textOn(theme.ink, theme.card, theme), ppt.HorizontalLeft)
	}
}

func roundRectPill(slide *ppt.Slide, x, y, w, h int64, fill ppt.Color) *ppt.AutoShape {
	sh := slide.CreateAutoShape()
	sh.SetAutoShapeType(ppt.AutoShapeRoundedRect)
	sh.SetAdjustValue("adj", 20000)
	sh.SetSolidFill(fill)
	sh.SetBorder(ppt.NewBorder())
	sh.SetOffsetX(x)
	sh.SetOffsetY(y)
	sh.SetWidth(w)
	sh.SetHeight(h)
	return sh
}

func buildKPI(slide *ppt.Slide, spec OutlineSlide, theme deckTheme, box contentBox) {
	texts := nonemptyBullets(spec.Bullets)
	n := len(texts)
	if n == 0 {
		return
	}
	cols, rows := gridSpec(n, 4)
	gap := emuIn(0.16)
	cardW := (box.w - gap*int64(cols-1)) / int64(cols)
	maxH := emuIn(3.00)
	if rows > 1 {
		maxH = emuIn(2.00)
	}
	cardH := (box.h - gap*int64(rows-1)) / int64(rows)
	if cardH > maxH {
		cardH = maxH
	}
	y0 := box.y + (box.h-cardH*int64(rows)-gap*int64(rows-1))/2
	for i, text := range texts {
		col := i % cols
		row := i / cols
		x := box.x + int64(col)*(cardW+gap)
		y := y0 + int64(row)*(cardH+gap)
		rect(slide, x, y, cardW, cardH, theme.card)
		value, label := splitKPI(text)
		// Big-number principle: the value is the visual anchor, so it takes
		// most of the card and the label stays small and quiet. Both sit on
		// the card's optical centerline.
		valueSize := 36
		switch {
		case rows > 1:
			valueSize = 28
		case cols >= 4:
			valueSize = 32
		}
		if cardH >= emuIn(2.4) {
			valueSize += 4
		}
		blockH := emuIn(0.80)
		if label != "" {
			blockH += emuIn(0.55)
		}
		valueY := y + (cardH-blockH)/2
		deckText(slide, x+emuIn(0.22), valueY, cardW-emuIn(0.4), emuIn(0.85), value, valueSize, true, textOn(theme.navy, theme.card, theme), ppt.HorizontalLeft)
		if label != "" {
			deckText(slide, x+emuIn(0.22), valueY+emuIn(0.86), cardW-emuIn(0.4), emuIn(0.55), label, 13, false, textOn(theme.slate, theme.card, theme), ppt.HorizontalLeft)
		}
	}
}
