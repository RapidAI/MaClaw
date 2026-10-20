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

// deckTheme is one shipped visual system. motif selects the layout (cover,
// content chrome, cards, section pages). Colors never change that layout:
// two styles can share a motif only when they should look the same.
type deckTheme struct {
	id                      string
	coverDark               bool
	motif                   motif
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
	motif                  motif
	paper, ink             string
	navy, navy2            string
	accent, gold           string
	white, mute, slate     string
	card                   string
	onDark, onDarkMute     string
}

func (s styleSpec) theme() deckTheme {
	return deckTheme{
		id: s.id, coverDark: s.coverDark, motif: s.motif,
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
// Each entry carries a motif: the layout system, not just a palette. Older
// theme ids stay as aliases so an existing outline still resolves.
// Custom styles saved from Settings are appended after this list.
var styleCatalog = []styleSpec{
	{
		id: "clean", label: "清爽专业", summary: "白底蓝条，结论式标题和证据卡片。适合工作汇报。",
		labelEn: "Clean Professional", labelHant: "清爽專業",
		summaryEn: "White field, blue rule, evidence cards.", summaryHant: "白底藍條，結論式標題和證據卡片。",
		aliases: []string{"clean", "business", "商务", "商务汇报", "汇报", "business briefing", "清爽专业", "clean professional"}, motif: motifReport,
		keywords: []string{"经营汇报", "工作总结", "项目复盘", "晋升述职", "季度汇报", "客户提案", "工作复盘", "清爽专业", "商务汇报", "quarterly review", "business review", "client proposal"},
		paper:    "FFFFFF", ink: "0F172A", navy: "1E3A8A", navy2: "0F766E",
		accent: "2563EB", gold: "F59E0B", white: "FFFFFF", mute: "64748B",
		slate: "334155", card: "F8FAFC", onDark: "F8FAFC", onDarkMute: "CBD5E1",
	},
	{
		id: "magazine", label: "创意杂志", summary: "黑白对半，一根洋红色条。适合发布和创意提案。",
		labelEn: "Editorial Magazine", labelHant: "創意雜誌",
		summaryEn: "Black and white split, one magenta bar.", summaryHant: "黑白對半，一根洋紅色條。",
		aliases: []string{"magazine", "launch", "发布", "发布路演", "路演", "modern", "现代", "现代品牌", "品牌", "product launch", "创意杂志", "editorial magazine"}, motif: motifSplit,
		keywords: []string{"品牌发布", "创意提案", "设计作品集", "时尚发布", "产品发布", "产品介绍", "创意杂志", "发布路演", "product launch", "pitch deck", "fundraising", "brand deck"},
		paper:    "FFFFFF", ink: "111111", navy: "111111", navy2: "2A2A2A",
		accent: "FF006E", gold: "FFED00", white: "FFFFFF", mute: "6B7280",
		slate: "374151", card: "FFFFFF", onDark: "FFFFFF", onDarkMute: "E5E7EB",
	},
	{
		id: "eink", label: "电子墨水", summary: "米纸大标题，页脚一句论点。适合主题演讲。",
		labelEn: "E-ink Magazine", labelHant: "電子墨水",
		summaryEn: "Warm paper, oversized type, one thesis line.", summaryHant: "米紙大標題，頁腳一句論點。",
		aliases: []string{"eink", "minimal", "极简", "ceremony", "年会", "典礼", "e-ink", "电子墨水", "e-ink magazine"}, motif: motifInk,
		keywords: []string{"主题演讲", "观点分享", "个人介绍", "年会致辞", "颁奖典礼", "电子墨水", "极简风格", "demo day", "personal introduction"},
		paper:    "F7F4EA", ink: "111111", navy: "1E2A78", navy2: "163B2F",
		accent: "C2410C", gold: "D9C29E", white: "FFFCF5", mute: "6B6560",
		slate: "3F3A36", card: "FBF8F1", onDark: "F7F4EA", onDarkMute: "D6D0C4",
	},
	{
		id: "dashboard", label: "数据仪表盘", summary: "浅底指标卡和细状态条。适合看板和 KPI。",
		labelEn: "Data Dashboard", labelHant: "數據儀表盤",
		summaryEn: "Mist field, metric cards, thin status bars.", summaryHant: "淺底指標卡和細狀態條。",
		aliases: []string{"dashboard", "数据仪表盘", "看板", "data dashboard"}, motif: motifDashboard,
		keywords: []string{"数据看板", "运营分析", "业绩展示", "商业智能", "KPI汇报", "数据仪表盘", "看板", "analytics dashboard"},
		paper:    "F5F8FC", ink: "0F172A", navy: "1D4ED8", navy2: "0891B2",
		accent: "2563EB", gold: "10B981", white: "FFFFFF", mute: "64748B",
		slate: "334155", card: "FFFFFF", onDark: "F8FAFC", onDarkMute: "C7D2FE",
	},
	{
		id: "retro", label: "复古扁平", summary: "奶油纸、色块横条、描边卡片。适合品牌故事。",
		labelEn: "Retro Flat", labelHant: "復古扁平",
		summaryEn: "Cream paper, flat color band, outlined cards.", summaryHant: "奶油紙、色塊橫條、描邊卡片。",
		aliases: []string{"retro", "复古", "扁平插画", "复古扁平", "retro flat"}, motif: motifPoster,
		keywords: []string{"品牌故事", "生活方式", "旅游介绍", "文化创意", "复古插画", "复古扁平", "复古风格", "brand story"},
		paper:    "F5F3E8", ink: "34495E", navy: "2C3E50", navy2: "6C7A89",
		accent: "FF6B6B", gold: "F9CA24", white: "FFFFFF", mute: "6C7A89",
		slate: "34495E", card: "FFFDF6", onDark: "2C3E50", onDarkMute: "95A5A6",
	},
	{
		id: "sketch", label: "手绘技术", summary: "近白纸、石墨角标、淡彩标记。适合技术解释。",
		labelEn: "Sketch Explainer", labelHant: "手繪技術",
		summaryEn: "Near-white paper, graphite corners, pastel marks.", summaryHant: "近白紙、石墨角標、淡彩標記。",
		aliases: []string{"sketch", "tech", "技术", "技术分享", "手绘技术", "sketch explainer"}, motif: motifSketch,
		keywords: []string{"技术分享", "系统架构", "技术架构", "概念解释", "开源项目", "技术方案", "手绘技术", "engineering talk", "system architecture"},
		paper:    "FCFBF7", ink: "2F3437", navy: "2F3437", navy2: "7A8490",
		accent: "BFD7F1", gold: "F4C7B8", white: "FFFFFF", mute: "8A9096",
		slate: "4A5158", card: "FFFFFF", onDark: "2F3437", onDarkMute: "8A9096",
	},
	{
		id: "whiteboard", label: "手绘白板", summary: "白板底、马克笔下划线、彩色便签。适合研讨。",
		labelEn: "Whiteboard", labelHant: "手繪白板",
		summaryEn: "Whiteboard, marker underline, sticky notes.", summaryHant: "白板底、麥克筆下劃線、彩色便簽。",
		aliases: []string{"whiteboard", "白板", "手绘白板"}, motif: motifBoard,
		keywords: []string{"头脑风暴", "内部研讨", "白板讲解", "概念梳理", "手绘白板", "白板风格", "brainstorm"},
		paper:    "FAFAF5", ink: "1A1A1A", navy: "1A1A1A", navy2: "3498DB",
		accent: "E74C3C", gold: "F39C12", white: "FFFFFF", mute: "6B7280",
		slate: "374151", card: "FFF9C4", onDark: "FFFFFF", onDarkMute: "D1D5DB",
	},
	{
		id: "handmade", label: "温暖手工", summary: "暖纸和便签卡。适合纪念、亲子和公益。",
		labelEn: "Handmade", labelHant: "溫暖手工",
		summaryEn: "Warm paper and note cards. For keepsakes.", summaryHant: "暖紙和便簽卡。適合紀念、親子和公益。",
		aliases: []string{"handmade", "warm", "温馨", "温馨纪念", "温暖手工"}, motif: motifCraft,
		keywords: []string{"生日纪念", "生日", "温馨纪念", "儿童教育", "亲子活动", "社区公益", "温暖手工", "birthday", "wedding", "family gathering"},
		paper:    "F5F1E8", ink: "5C4033", navy: "5C4033", navy2: "A67C52",
		accent: "F5C4B8", gold: "B8D4A8", white: "FFFCF8", mute: "8A7568",
		slate: "6B5344", card: "FFFCF7", onDark: "5C4033", onDarkMute: "A67C52",
	},
	{
		id: "research", label: "科研答辩", summary: "白底学术蓝，红色只用来标结论。适合答辩。",
		labelEn: "Research Defense", labelHant: "科研答辯",
		summaryEn: "White field, academic blue, red for conclusions.", summaryHant: "白底學術藍，紅色只用來標結論。",
		aliases: []string{"research", "academic", "学术", "学术答辩", "科研答辩", "research defense"}, motif: motifDefense,
		keywords: []string{"论文答辩", "开题答辩", "毕业答辩", "课题申报", "基金申请", "文献综述", "科研答辩", "学术答辩", "答辩", "thesis defense", "dissertation"},
		paper:    "FFFFFF", ink: "111111", navy: "003F8F", navy2: "0B5CAD",
		accent: "B5121B", gold: "F3DFA2", white: "FFFFFF", mute: "4B5563",
		slate: "333333", card: "F3F6FA", onDark: "FFFFFF", onDarkMute: "D6E4F0",
	},
	{
		id: "mckinsey", label: "麦肯锡", summary: "浅灰细线、大字留白，没有大色块。适合战略咨询。",
		labelEn: "McKinsey", labelHant: "麥肯錫",
		summaryEn: "Pale field, hairlines, large type, no navy blocks.", summaryHant: "淺灰細線、大字留白，沒有大色塊。",
		aliases: []string{"mckinsey", "consulting", "咨询", "咨询顾问", "麦肯锡", "executive", "高管", "高管汇报"}, motif: motifConsulting,
		keywords: []string{"战略咨询", "商业分析", "行业分析", "尽职调查", "竞对分析", "增长战略", "战略汇报", "董事会汇报", "麦肯锡", "咨询风格", "due diligence", "strategy consulting"},
		paper:    "F6F8FA", ink: "243447", navy: "243447", navy2: "5B7C99",
		accent: "5B7C99", gold: "8AA0B5", white: "FFFFFF", mute: "6B7280",
		slate: "475569", card: "FFFFFF", onDark: "243447", onDarkMute: "6B7280",
	},
	{
		id: "official", label: "党政红", summary: "象牙底、中国红与哑光金。适合正式工作汇报。",
		labelEn: "Official Red", labelHant: "黨政紅",
		summaryEn: "Ivory field, Chinese red, matte gold.", summaryHant: "象牙底、中國紅與啞光金。",
		aliases: []string{"official", "党政", "党政红", "official red"}, motif: motifOfficial,
		keywords: []string{"党建学习", "政策宣讲", "工作部署", "党政汇报", "国企汇报", "党政红", "党建汇报"},
		paper:    "FFF9F2", ink: "262626", navy: "9E1530", navy2: "C41E3A",
		accent: "C41E3A", gold: "D6A84B", white: "FFFFFF", mute: "6B6560",
		slate: "4A4A4A", card: "FFFCF8", onDark: "FFF9F2", onDarkMute: "F3E6C8",
	},
	{
		id: "course", label: "教学课件", summary: "顶部课头色带，下面是课次标题。适合课件。",
		labelEn: "Lesson Deck", labelHant: "教學課件",
		summaryEn: "Course masthead, then the lesson title.", summaryHant: "頂部課頭色帶，下面是課次標題。",
		aliases: []string{"course", "education", "教学", "课件", "教学课件", "lesson deck"}, motif: motifLesson,
		keywords: []string{"教学课件", "培训课程", "培训课件", "课堂教案", "新员工培训", "课件", "教案", "lesson plan", "classroom"},
		paper:    "FFFBF5", ink: "1F2937", navy: "1E3A5F", navy2: "0F766E",
		accent: "F59E0B", gold: "FDE68A", white: "FFFFFF", mute: "6B7280",
		slate: "374151", card: "FFFCF7", onDark: "FFFBF5", onDarkMute: "E5E7EB",
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
// Motif names a built-in layout (report, split, ink, dashboard, poster, sketch,
// board, craft, defense, consulting, official, lesson). Empty picks report,
// or ink when the cover is dark.
type CustomStyle struct {
	ID        string      `json:"id"`
	Label     string      `json:"label"`
	Summary   string      `json:"summary"`
	Keywords  []string    `json:"keywords"`
	CoverDark bool        `json:"cover_dark"`
	Motif     string      `json:"motif,omitempty"`
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
	layout, err := normalizeMotif(s.Motif, s.CoverDark)
	if err != nil {
		return styleSpec{}, err
	}
	return styleSpec{
		id: id, label: label, summary: summary, keywords: keywords, coverDark: s.CoverDark, motif: layout,
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
	key = strings.TrimSpace(strings.TrimPrefix(key, "theme="))
	if key == "" || isAutoTheme(key) {
		return styleSpec{}, false
	}
	for _, spec := range allStyles() {
		if key == spec.id || key == strings.ToLower(spec.label) ||
			(spec.labelEn != "" && key == strings.ToLower(spec.labelEn)) ||
			(spec.labelHant != "" && key == strings.ToLower(spec.labelHant)) {
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

func extractExplicitThemeTag(text string) (styleSpec, bool) {
	lower := strings.ToLower(text)
	idx := strings.Index(lower, "theme=")
	if idx < 0 {
		return styleSpec{}, false
	}
	rest := lower[idx+len("theme="):]
	end := strings.IndexAny(rest, " \t\r\n)）」,，;；\"'`")
	if end >= 0 {
		rest = rest[:end]
	}
	return lookupStyle(rest)
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
// ones, so "生日纪念" stays handmade and "年会致辞" stays eink. Equal scores
// keep the earlier catalog entry.
func matchStyle(hint string) (styleSpec, bool) {
	if spec, ok := extractExplicitThemeTag(hint); ok {
		return spec, true
	}
	hint = strings.ToLower(hint)
	styles := allStyles()
	best := -1
	bestScore, bestLen := 0, 0
	for i, spec := range styles {
		score, longest := 0, 0
		candidates := make([]string, 0, len(spec.keywords)+3)
		candidates = append(candidates, spec.keywords...)
		for _, name := range []string{spec.label, spec.labelEn, spec.labelHant} {
			if strings.TrimSpace(name) != "" {
				candidates = append(candidates, name)
			}
		}
		seen := map[string]bool{}
		for _, keyword := range candidates {
			kw := strings.ToLower(strings.TrimSpace(keyword))
			if seen[kw] || len([]rune(kw)) < 2 || !strings.Contains(hint, kw) {
				continue
			}
			seen[kw] = true
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
	case "cards", "card", "卡片", "grid", "网格":
		return "cards"
	case "timeline", "process", "steps", "flow", "roadmap", "时间线", "流程", "步骤", "路线图", "阶段":
		return "timeline"
	case "comparison", "compare", "versus", "vs", "split", "two_column", "two-column", "columns", "对比", "对照", "双栏", "比较":
		return "comparison"
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
// Short point lists, timelines, and comparisons become cards; a title with no
// body becomes a section divider. Explicit layouts that cannot hold the text
// fall back to bullets so a point is never dropped on the floor.
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
	case "timeline", "comparison":
		if len(texts) < 2 || len(texts) > 6 {
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
		if timelineCandidate(spec, texts) || comparisonCandidate(spec, texts) || cardCandidate(texts) {
			return "cards"
		}
		return "bullets"
	default:
		return asked
	}
}

func cardCandidate(texts []string) bool {
	if len(texts) >= 2 && len(texts) <= 4 {
		for _, text := range texts {
			if len([]rune(text)) > 52 {
				return false
			}
		}
		return true
	}
	if len(texts) >= 5 && len(texts) <= 6 {
		for _, text := range texts {
			head, _ := splitCardLine(text)
			if head == "" || len([]rune(text)) > 48 {
				return false
			}
		}
		return true
	}
	return false
}

func timelineCandidate(spec OutlineSlide, texts []string) bool {
	if len(texts) < 3 || len(texts) > 6 {
		return false
	}
	for _, text := range texts {
		if len([]rune(text)) > 64 {
			return false
		}
	}
	header := strings.ToLower(spec.Title + " " + spec.Kicker)
	for _, kw := range []string{"流程", "时间线", "路线图", "步骤", "演进", "阶段", "路径", "roadmap", "timeline", "workflow"} {
		if strings.Contains(header, kw) {
			return true
		}
	}
	stepPrefixes := 0
	for _, text := range texts {
		lower := strings.ToLower(strings.TrimSpace(text))
		for _, pfx := range []string{"第1", "第2", "第3", "第4", "第一", "第二", "第三", "第四", "阶段一", "阶段二", "阶段三", "阶段 1", "阶段 2", "step ", "phase "} {
			if strings.HasPrefix(lower, pfx) {
				stepPrefixes++
				break
			}
		}
	}
	return stepPrefixes >= 3
}

func comparisonCandidate(spec OutlineSlide, texts []string) bool {
	if len(texts) != 2 && len(texts) != 4 && len(texts) != 6 {
		return false
	}
	for _, text := range texts {
		if len([]rune(text)) > 64 {
			return false
		}
	}
	header := strings.ToLower(spec.Title + " " + spec.Kicker)
	for _, kw := range []string{"对比", "对照", "比较", "优劣", "差异", " vs ", "versus"} {
		if strings.Contains(header, kw) {
			return true
		}
	}
	return false
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
	return paintMotifChrome(slide, theme, kicker, title, footer, page, total)
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
	if len(texts) >= 2 && len(texts) <= 6 && region.w >= emuIn(4.2) {
		maxRowH := emuIn(0.94)
		textWIn := float64(region.w-emuIn(0.45)) / float64(deckInchEMU)
		for _, text := range texts {
			if coverTitleLines(text, 18, textWIn) > 1 {
				maxRowH = emuIn(1.16)
				break
			}
		}
		rowH, gap := fitSpan(region.h, int64(len(texts)), emuIn(0.12), maxRowH)
		stack := rowH*int64(len(texts)) + gap*int64(len(texts)-1)
		y0 := region.y
		if stack < region.h {
			y0 = region.y + (region.h-stack)/2
		}
		for i, text := range texts {
			y := y0 + int64(i)*(rowH+gap)
			bg, padLeft := paintBulletRowChrome(slide, theme, region.x, y, region.w, rowH, i)
			lines := coverTitleLines(text, 18, float64(region.w-padLeft-emuIn(0.18))/float64(deckInchEMU))
			estH := emuIn(float64(lines)*0.34 + 0.08)
			padTop := (rowH - estH) / 2
			if padTop < emuIn(0.06) {
				padTop = emuIn(0.06)
			}
			rowBox := slide.CreateRichTextShape()
			rowBox.SetOffsetX(region.x + padLeft).SetOffsetY(y + padTop).SetWidth(region.w - padLeft - emuIn(0.18)).SetHeight(rowH - padTop)
			rowBox.SetWordWrap(true)
			para := rowBox.GetActiveParagraph()
			para.SetBullet(ppt.NewBullet().SetCharBullet("•", defaultDeckFont).SetColor(readableAccent(theme, bg)))
			run := para.CreateTextRun(text)
			font := run.GetFont().SetSize(18).SetName(defaultDeckFont).SetColor(textOn(theme.ink, bg, theme))
			font.NameEA = defaultDeckFont
		}
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
	paintMotifSection(slide, theme, title, subtitle, kicker, page, total, sectionNo)
}

func paintStyleCover(slide *ppt.Slide, theme deckTheme, title, subtitle, kicker string, page, total int) {
	title = sanitizeXMLText(strings.TrimSpace(title))
	subtitle = sanitizeXMLText(strings.TrimSpace(subtitle))
	kicker = sanitizeXMLText(strings.TrimSpace(kicker))
	paintMotifCover(slide, theme, title, subtitle, kicker, page, total)
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
	quoteBg, padX := paintQuoteFrame(slide, theme, box, markY, blockH+emuIn(0.35))
	deckText(slide, box.x+padX, markY, emuIn(1.4), emuIn(0.9), "“", 64, true, textOn(theme.accent, quoteBg, theme), ppt.HorizontalLeft)
	deckLines(slide, box.x+padX, textY, box.w-2*padX, box.h-(textY-box.y)-emuIn(0.1), lines, quoteSize, false, textOn(theme.navy, quoteBg, theme), ppt.HorizontalLeft, 80)
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
	if spec, ok := lookupStyle(id); ok {
		return motifBody(spec.motif) == bodyOpen
	}
	return false
}

func buildCards(slide *ppt.Slide, spec OutlineSlide, theme deckTheme, box contentBox) {
	texts := nonemptyBullets(spec.Bullets)
	if len(texts) == 0 {
		return
	}
	asked := normalizeSlideLayout(spec.Layout)
	if asked == "timeline" || (asked == "auto" && timelineCandidate(spec, texts)) {
		buildTimeline(slide, spec, theme, box)
		return
	}
	if asked == "comparison" || (asked == "auto" && comparisonCandidate(spec, texts)) {
		buildComparison(slide, spec, theme, box)
		return
	}
	switch theme.motif {
	case motifInk, motifConsulting:
		buildOpenPoints(slide, theme, box, texts)
	case motifSketch:
		buildSketchCards(slide, theme, box, texts)
	case motifPoster:
		buildOutlineCards(slide, theme, box, texts)
	case motifBoard:
		buildBoardCards(slide, theme, box, texts)
	case motifCraft:
		buildNoteCards(slide, theme, box, texts)
	case motifSplit:
		buildRailCards(slide, theme, box, texts)
	case motifDashboard:
		buildDashboardCards(slide, theme, box, texts)
	case motifDefense:
		buildDefenseCards(slide, theme, box, texts)
	case motifOfficial:
		buildOfficialCards(slide, theme, box, texts)
	case motifLesson:
		buildLessonCards(slide, theme, box, texts)
	default:
		buildFilledCards(slide, theme, box, texts)
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
	rowH, gap := fitSpan(box.h, int64(n), emuIn(0.1), emuIn(1.08))
	numH := emuIn(0.36)
	if numH > rowH {
		numH = rowH
	}
	for i, text := range texts {
		y := box.y + int64(i)*(rowH+gap)
		if theme.motif == motifInk {
			rect(slide, box.x, y+emuIn(0.06), emuIn(0.045), rowH-emuIn(0.18), theme.accent)
			deckText(slide, box.x+emuIn(0.14), y, emuIn(0.65), numH, fmt.Sprintf("%02d", i+1), 16, true, readableAccent(theme, theme.paper), ppt.HorizontalLeft)
		} else {
			deckText(slide, box.x, y, emuIn(0.7), numH, fmt.Sprintf("%02d", i+1), 16, true, readableAccent(theme, theme.paper), ppt.HorizontalLeft)
			if rowH > emuIn(0.24) {
				rect(slide, box.x+emuIn(0.72), y+emuIn(0.06), emuIn(0.012), rowH-emuIn(0.18), openPointRule(theme))
			}
			if i == n-1 {
				rect(slide, box.x+box.w-emuIn(0.18), y+emuIn(0.08), emuIn(0.14), emuIn(0.14), theme.accent)
			}
		}
		head, body := splitCardLine(text)
		if head != "" {
			deckText(slide, box.x+emuIn(0.88), y, box.w-emuIn(1.1), numH, head, 18, true, textOn(theme.navy, theme.paper, theme), ppt.HorizontalLeft)
			bodyTop := y + emuIn(0.38)
			deckText(slide, box.x+emuIn(0.88), bodyTop, box.w-emuIn(1.1), remain(bodyTop, y, rowH, emuIn(0.06)), body, 15, false, textOn(theme.slate, theme.paper, theme), ppt.HorizontalLeft)
		} else {
			deckText(slide, box.x+emuIn(0.88), y, box.w-emuIn(1.1), remain(y, y, rowH, emuIn(0.08)), text, 18, false, textOn(theme.ink, theme.paper, theme), ppt.HorizontalLeft)
		}
		if rowH > emuIn(0.04) {
			rect(slide, box.x, y+rowH-emuIn(0.015), box.w, emuIn(0.015), openPointRule(theme))
			if theme.motif == motifInk {
				rect(slide, box.x, y+rowH-emuIn(0.025), emuIn(0.85), emuIn(0.025), theme.accent)
			}
		}
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
	rowH, gap := fitSpan(box.h, int64(n), emuIn(0.12), emuIn(0.92))
	stack := rowH*int64(n) + gap*int64(n-1)
	y0 := box.y
	if stack < box.h {
		y0 = box.y + (box.h-stack)/2
	}
	chipH := emuIn(0.42)
	if chipH > rowH {
		chipH = rowH
	}
	for i, text := range texts {
		y := y0 + int64(i)*(rowH+gap)
		numY := y + (rowH-chipH)/2
		rowBg, chipColor := paintAgendaRow(slide, theme, box.x, y, box.w, rowH, numY, chipH, i, n)
		numTextColor := bestContrast(chipColor, theme.navy, theme.ink, theme.white)
		if chipColor == theme.accent {
			numTextColor = agendaNumberColor(theme)
		}
		deckText(slide, box.x+emuIn(0.16), numY, emuIn(0.72), chipH, fmt.Sprintf("%02d", i+1), 14, true, numTextColor, ppt.HorizontalCenter)
		deckText(slide, box.x+emuIn(1.08), numY, box.w-emuIn(1.3), chipH, text, 16, false, textOn(theme.ink, rowBg, theme), ppt.HorizontalLeft)
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
		bg, accentColor := paintKPICardFrame(slide, theme, x, y, cardW, cardH, i, n)
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
		valColor := textOn(theme.navy, bg, theme)
		if theme.motif == motifSplit || (theme.motif == motifDefense && i == n-1) {
			valColor = textOn(accentColor, bg, theme)
		}
		deckText(slide, x+emuIn(0.22), valueY, cardW-emuIn(0.4), emuIn(0.85), value, valueSize, true, valColor, ppt.HorizontalLeft)
		if label != "" {
			if cardH >= emuIn(1.7) {
				rect(slide, x+emuIn(0.22), valueY+emuIn(0.80), emuIn(0.85), emuIn(0.025), blend(accentColor, bg, 0.35))
			}
			deckText(slide, x+emuIn(0.22), valueY+emuIn(0.86), cardW-emuIn(0.4), emuIn(0.55), label, 13, false, textOn(theme.slate, bg, theme), ppt.HorizontalLeft)
		}
	}
}

func buildTimeline(slide *ppt.Slide, spec OutlineSlide, theme deckTheme, box contentBox) {
	texts := nonemptyBullets(spec.Bullets)
	n := len(texts)
	if n == 0 {
		return
	}
	cols, rows := n, 1
	if n > 4 {
		cols = 3
		rows = 2
	}
	gap := emuIn(0.18)
	cardW, _ := fitSpan(box.w, int64(cols), gap, 0)
	maxRowH := emuIn(3.30)
	if rows > 1 {
		maxRowH = emuIn(2.25)
	}
	rowH, vGap := fitSpan(box.h, int64(rows), emuIn(0.22), maxRowH)
	stack := rowH*int64(rows) + vGap*int64(rows-1)
	y0 := box.y
	if stack < box.h {
		y0 = box.y + (box.h-stack)/2
	}
	nodeD := emuIn(0.46)
	cardTopPad := emuIn(0.62)
	for r := 0; r < rows; r++ {
		rowY := y0 + int64(r)*(rowH+vGap)
		rowCount := cols
		if r == rows-1 && n-r*cols < cols {
			rowCount = n - r*cols
		}
		if rowCount > 1 {
			trackX := box.x + cardW/2
			trackW := int64(rowCount-1) * (cardW + gap)
			rect(slide, trackX, rowY+nodeD/2-emuIn(0.025), trackW, emuIn(0.05), blend(theme.accent, theme.paper, 0.45))
		}
	}
	for i, text := range texts {
		col := i % cols
		row := i / cols
		x := box.x + int64(col)*(cardW+gap)
		rowY := y0 + int64(row)*(rowH+vGap)
		stepColor := motifStepColor(theme, i, n)
		nodeX := x + (cardW-nodeD)/2
		paintTimelineNode(slide, theme, nodeX, rowY, nodeD, stepColor, i)
		cardY := rowY + cardTopPad
		cardH := rowH - cardTopPad
		if cardH < emuIn(0.9) {
			cardH = emuIn(0.9)
		}
		bg := paintTimelineCardFrame(slide, theme, x, cardY, cardW, cardH, stepColor, i)
		head, body := splitCardLine(text)
		titleSize, bodySize := 17, 15
		if rows > 1 {
			titleSize, bodySize = 15, 14
		}
		if head != "" {
			deckText(slide, x+emuIn(0.18), cardY+emuIn(0.16), cardW-emuIn(0.36), emuIn(0.42), head, titleSize, true, textOn(theme.navy, bg, theme), ppt.HorizontalLeft)
			bodyTop := cardY + emuIn(0.62)
			deckText(slide, x+emuIn(0.18), bodyTop, cardW-emuIn(0.36), remain(bodyTop, cardY, cardH, emuIn(0.1)), body, bodySize, false, textOn(theme.slate, bg, theme), ppt.HorizontalLeft)
		} else {
			deckText(slide, x+emuIn(0.18), cardY+emuIn(0.18), cardW-emuIn(0.36), remain(cardY+emuIn(0.18), cardY, cardH, emuIn(0.1)), text, bodySize+1, false, textOn(theme.ink, bg, theme), ppt.HorizontalLeft)
		}
	}
}

func buildComparison(slide *ppt.Slide, spec OutlineSlide, theme deckTheme, box contentBox) {
	texts := nonemptyBullets(spec.Bullets)
	n := len(texts)
	if n == 0 {
		return
	}
	colGap := emuIn(0.34)
	colW := (box.w - colGap) / 2
	rows := (n + 1) / 2
	maxH := emuIn(3.40)
	if rows > 1 {
		maxH = emuIn(1.95)
	}
	cardH, vGap := fitSpan(box.h, int64(rows), emuIn(0.16), maxH)
	stack := cardH*int64(rows) + vGap*int64(rows-1)
	y0 := box.y
	if stack < box.h {
		y0 = box.y + (box.h-stack)/2
	}
	// Central divider line between the two comparison columns.
	midX := box.x + colW + colGap/2
	rect(slide, midX-emuIn(0.01), y0, emuIn(0.02), stack, blend(theme.accent, theme.paper, 0.55))
	oval(slide, midX-emuIn(0.11), y0+stack/2-emuIn(0.11), emuIn(0.22), emuIn(0.22), theme.accent)
	for i, text := range texts {
		col := i % 2
		row := i / 2
		x := box.x + int64(col)*(colW+colGap)
		y := y0 + int64(row)*(cardH+vGap)
		sideColor := theme.navy
		if col == 1 {
			sideColor = theme.accent
		}
		bg := paintComparisonCardFrame(slide, theme, x, y, colW, cardH, sideColor, col, i)
		deckText(slide, x+emuIn(0.20), y+emuIn(0.16), colW-emuIn(0.40), emuIn(0.28), fmt.Sprintf("%02d", i+1), 12, true, textOn(sideColor, bg, theme), ppt.HorizontalLeft)
		head, body := splitCardLine(text)
		if head != "" {
			deckText(slide, x+emuIn(0.20), y+emuIn(0.46), colW-emuIn(0.40), emuIn(0.44), head, 18, true, textOn(sideColor, bg, theme), ppt.HorizontalLeft)
			bodyTop := y + emuIn(0.94)
			deckText(slide, x+emuIn(0.20), bodyTop, colW-emuIn(0.40), remain(bodyTop, y, cardH, emuIn(0.12)), body, 15, false, textOn(theme.slate, bg, theme), ppt.HorizontalLeft)
		} else {
			deckText(slide, x+emuIn(0.20), y+emuIn(0.48), colW-emuIn(0.40), remain(y+emuIn(0.48), y, cardH, emuIn(0.12)), text, 17, false, textOn(theme.ink, bg, theme), ppt.HorizontalLeft)
		}
	}
}
