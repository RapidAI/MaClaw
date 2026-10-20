package docgen

import (
	"fmt"
	"math"
	"strings"
	"sync"
	"unicode/utf8"
)

// Motifs are the five built-in page layouts. A custom style picks one of
// these layouts and supplies its own colors. Colors never invent a sixth layout.
const (
	MotifMasthead = "masthead"
	MotifRail     = "rail"
	MotifFrame    = "frame"
	MotifBand     = "band"
	MotifLedger   = "ledger"
)

// Style is one PDF visual system: a layout motif plus the colors that system paints.
type Style struct {
	ID          string
	Label       string
	LabelEn     string
	LabelHant   string
	Summary     string
	SummaryEn   string
	SummaryHant string
	Keywords    []string
	Motif       string
	Builtin     bool
	Colors      StyleColors
}

// StyleColors are 6-digit hex values without a leading '#'.
// The page itself stays white, because the PDF renderer does not paint a page fill.
type StyleColors struct {
	Ink    string `json:"ink"`
	Navy   string `json:"navy"`
	Accent string `json:"accent"`
	Gold   string `json:"gold"`
	Meta   string `json:"meta"`
	Rule   string `json:"rule"`
	Quote  string `json:"quote"`
	Link   string `json:"link"`
	OnDark string `json:"on_dark"`
}

// CustomStyle is a user-authored style saved from settings.
type CustomStyle struct {
	ID       string      `json:"id"`
	Label    string      `json:"label"`
	Summary  string      `json:"summary"`
	Keywords []string    `json:"keywords"`
	Motif    string      `json:"motif"`
	Colors   StyleColors `json:"colors"`
}

// StyleInfo is one row in the PDF style settings list.
type StyleInfo struct {
	ID          string   `json:"id"`
	Label       string   `json:"label"`
	LabelEn     string   `json:"label_en,omitempty"`
	LabelHant   string   `json:"label_hant,omitempty"`
	Summary     string   `json:"summary"`
	SummaryEn   string   `json:"summary_en,omitempty"`
	SummaryHant string   `json:"summary_hant,omitempty"`
	Keywords    []string `json:"keywords,omitempty"`
	Motif       string   `json:"motif"`
	Builtin     bool     `json:"builtin"`
	Active      bool     `json:"active"`
	Navy        string   `json:"navy"`
	Accent      string   `json:"accent"`
	Gold        string   `json:"gold"`
	Ink         string   `json:"ink"`
}

var styleMu sync.Mutex
var customStyles []Style
var activeStyleID string

func builtinStyles() []Style {
	return []Style{
		{
			ID: "gazette", Label: "公报", LabelEn: "Gazette", LabelHant: "公報",
			Summary:     "海军蓝顶栏，朱红标题线，适合通报、天气和正式报告。",
			SummaryEn:   "Navy masthead and vermilion rules for briefs and weather reports.",
			SummaryHant: "海軍藍頂欄，朱紅標題線，適合通報、天氣和正式報告。",
			Keywords:    []string{"通报", "天气", "报告", "公文"},
			Motif:       MotifMasthead, Builtin: true,
			Colors: StyleColors{Ink: "1C1917", Navy: "0E2A47", Accent: "C2410C", Gold: "A16207", Meta: "3F3F46", Rule: "C2410C", Quote: "7C2D12", Link: "1D4ED8", OnDark: "F8FAFC"},
		},
		{
			ID: "brief", Label: "简报", LabelEn: "Brief", LabelHant: "簡報",
			Summary:     "左侧青色立柱，墨色标题，适合一页简报。",
			SummaryEn:   "A teal rail and ink titles for a one-page brief.",
			SummaryHant: "左側青色立柱，墨色標題，適合一頁簡報。",
			Keywords:    []string{"简报", "纪要", "摘要"},
			Motif:       MotifRail, Builtin: true,
			Colors: StyleColors{Ink: "1C1917", Navy: "134E4A", Accent: "0F766E", Gold: "B45309", Meta: "44403C", Rule: "0F766E", Quote: "115E59", Link: "0F766E", OnDark: "F0FDFA"},
		},
		{
			ID: "dossier", Label: "案卷", LabelEn: "Dossier", LabelHant: "案卷",
			Summary:     "金线双框，酒红标题居中，适合案卷和会议纪要。",
			SummaryEn:   "A gold double frame and centered burgundy titles for dossiers.",
			SummaryHant: "金線雙框，酒紅標題居中，適合案卷和會議紀要。",
			Keywords:    []string{"案卷", "纪要", "会议", "卷宗"},
			Motif:       MotifFrame, Builtin: true,
			Colors: StyleColors{Ink: "292524", Navy: "44403C", Accent: "7F1D1D", Gold: "A16207", Meta: "57534E", Rule: "A16207", Quote: "7F1D1D", Link: "7F1D1D", OnDark: "FFF7ED"},
		},
		{
			ID: "atlas", Label: "图志", LabelEn: "Atlas", LabelHant: "圖志",
			Summary:     "钴蓝顶带加琥珀短标，适合数据和图表说明。",
			SummaryEn:   "A cobalt band and amber marker for data and figure notes.",
			SummaryHant: "鈷藍頂帶加琥珀短標，適合數據和圖表說明。",
			Keywords:    []string{"数据", "图表", "图志", "统计"},
			Motif:       MotifBand, Builtin: true,
			Colors: StyleColors{Ink: "1E293B", Navy: "1E3A8A", Accent: "C2410C", Gold: "B45309", Meta: "334155", Rule: "1E3A8A", Quote: "9A3412", Link: "1D4ED8", OnDark: "EFF6FF"},
		},
		{
			ID: "ledger", Label: "清册", LabelEn: "Ledger", LabelHant: "清冊",
			Summary:     "粗绿线压题，标题更紧，适合表格清册。",
			SummaryEn:   "A heavy green rule and compact titles for ledgers and tables.",
			SummaryHant: "粗綠線壓題，標題更緊，適合表格清冊。",
			Keywords:    []string{"表格", "清册", "清单", "台账"},
			Motif:       MotifLedger, Builtin: true,
			Colors: StyleColors{Ink: "1C1917", Navy: "14532D", Accent: "166534", Gold: "A16207", Meta: "3F3F46", Rule: "14532D", Quote: "14532D", Link: "166534", OnDark: "F0FDF4"},
		},
	}
}

// ActiveStyle is the style every PDF uses when the document does not name one.
// An empty selection is the first built-in style.
func ActiveStyle() Style {
	styleMu.Lock()
	id := activeStyleID
	styles := append([]Style(nil), customStyles...)
	styleMu.Unlock()
	if style, ok := findStyle(id, styles); ok {
		return style
	}
	return builtinStyles()[0]
}

// SetActiveStyle selects the style used by later PDF renders.
// An empty id restores the first built-in style.
func SetActiveStyle(id string) error {
	id = strings.ToLower(strings.TrimSpace(id))
	if id == "" {
		styleMu.Lock()
		activeStyleID = ""
		styleMu.Unlock()
		return nil
	}
	if _, ok := LookupStyle(id); !ok {
		return fmt.Errorf("pdf_style_unknown")
	}
	styleMu.Lock()
	activeStyleID = id
	styleMu.Unlock()
	return nil
}

// LookupStyle returns a built-in or custom style by id.
func LookupStyle(id string) (Style, bool) {
	id = strings.ToLower(strings.TrimSpace(id))
	if id == "" {
		return Style{}, false
	}
	styleMu.Lock()
	styles := append([]Style(nil), customStyles...)
	styleMu.Unlock()
	return findStyle(id, styles)
}

func findStyle(id string, custom []Style) (Style, bool) {
	id = strings.ToLower(strings.TrimSpace(id))
	if id == "" {
		return Style{}, false
	}
	for _, style := range builtinStyles() {
		if style.ID == id {
			return style, true
		}
	}
	for _, style := range custom {
		if style.ID == id {
			return style, true
		}
	}
	return Style{}, false
}

func styleForSpec(spec Spec) Style {
	if spec.boundStyle.ID != "" {
		return spec.boundStyle
	}
	if style, ok := LookupStyle(spec.StyleID); ok {
		return style
	}
	return ActiveStyle()
}

// ListStyleInfo returns built-in styles first, then custom styles.
func ListStyleInfo() []StyleInfo {
	active := ActiveStyle().ID
	out := make([]StyleInfo, 0, 8)
	for _, style := range allStyles() {
		out = append(out, style.info(style.ID == active))
	}
	return out
}

func allStyles() []Style {
	styleMu.Lock()
	custom := append([]Style(nil), customStyles...)
	styleMu.Unlock()
	return append(builtinStyles(), custom...)
}

func (s Style) info(active bool) StyleInfo {
	return StyleInfo{
		ID: s.ID, Label: s.Label, LabelEn: s.LabelEn, LabelHant: s.LabelHant,
		Summary: s.Summary, SummaryEn: s.SummaryEn, SummaryHant: s.SummaryHant,
		Keywords: append([]string(nil), s.Keywords...),
		Motif:    s.Motif, Builtin: s.Builtin, Active: active,
		Navy: s.Colors.Navy, Accent: s.Colors.Accent, Gold: s.Colors.Gold, Ink: s.Colors.Ink,
	}
}

// SetCustomStyles replaces user-authored styles. Nil clears them.
// A colliding or invalid style rejects the whole update.
func SetCustomStyles(styles []CustomStyle) error {
	next := make([]Style, 0, len(styles))
	seen := map[string]bool{}
	for _, style := range styles {
		spec, err := style.spec()
		if err != nil {
			return err
		}
		if seen[spec.ID] {
			return fmt.Errorf("pdf_style_id_taken")
		}
		seen[spec.ID] = true
		next = append(next, spec)
	}
	styleMu.Lock()
	customStyles = next
	if activeStyleID != "" {
		if _, ok := findStyle(activeStyleID, next); !ok {
			activeStyleID = ""
		}
	}
	styleMu.Unlock()
	return nil
}

// ValidateCustomStyle checks a user style without installing it.
func ValidateCustomStyle(style CustomStyle) error {
	_, err := style.spec()
	return err
}

// Normalized returns the style after id, motif, and contrast checks.
func (s CustomStyle) Normalized() (CustomStyle, error) {
	spec, err := s.spec()
	if err != nil {
		return CustomStyle{}, err
	}
	return CustomStyle{
		ID: spec.ID, Label: spec.Label, Summary: spec.Summary,
		Keywords: spec.Keywords, Motif: spec.Motif, Colors: spec.Colors,
	}, nil
}

func (s CustomStyle) spec() (Style, error) {
	id := strings.ToLower(strings.TrimSpace(s.ID))
	if !validPDFStyleID(id) {
		return Style{}, fmt.Errorf("pdf_style_id_invalid")
	}
	if builtinStyleID(id) {
		return Style{}, fmt.Errorf("pdf_style_id_taken")
	}
	label := strings.TrimSpace(s.Label)
	if !plainPDFStyleText(label) || utf8.RuneCountInString(label) > 16 {
		return Style{}, fmt.Errorf("pdf_style_label_invalid")
	}
	summary := strings.TrimSpace(s.Summary)
	if !plainPDFStyleText(summary) || utf8.RuneCountInString(summary) > 80 {
		return Style{}, fmt.Errorf("pdf_style_summary_invalid")
	}
	motif, err := normalizeMotif(s.Motif)
	if err != nil {
		return Style{}, err
	}
	keywords := normalizeKeywords(s.Keywords)
	if len(keywords) == 0 {
		return Style{}, fmt.Errorf("pdf_style_keywords_required")
	}
	colors, err := normalizeStyleColors(s.Colors)
	if err != nil {
		return Style{}, err
	}
	return Style{
		ID: id, Label: label, Summary: summary, Keywords: keywords,
		Motif: motif, Colors: colors,
	}, nil
}

func builtinStyleID(id string) bool {
	for _, style := range builtinStyles() {
		if style.ID == id {
			return true
		}
	}
	return false
}

func validPDFStyleID(id string) bool {
	if len(id) < 2 || len(id) > 32 {
		return false
	}
	for i, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
		case r == '-' && i > 0 && i < len(id)-1:
		default:
			return false
		}
	}
	return true
}

func plainPDFStyleText(text string) bool {
	if strings.TrimSpace(text) == "" {
		return false
	}
	return !strings.ContainsAny(text, "\r\n\t`")
}

func normalizeKeywords(in []string) []string {
	out := make([]string, 0, len(in))
	seen := map[string]bool{}
	for _, keyword := range in {
		keyword = strings.TrimSpace(keyword)
		if !plainPDFStyleText(keyword) || utf8.RuneCountInString(keyword) < 2 || utf8.RuneCountInString(keyword) > 16 || seen[keyword] {
			continue
		}
		seen[keyword] = true
		out = append(out, keyword)
		if len(out) == 8 {
			break
		}
	}
	return out
}

func normalizeMotif(raw string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case MotifMasthead, "顶栏", "公报":
		return MotifMasthead, nil
	case MotifRail, "立柱", "简报":
		return MotifRail, nil
	case MotifFrame, "双框", "案卷":
		return MotifFrame, nil
	case MotifBand, "色带", "图志":
		return MotifBand, nil
	case MotifLedger, "粗线", "清册":
		return MotifLedger, nil
	default:
		return "", fmt.Errorf("pdf_style_motif_invalid")
	}
}

func normalizeStyleColors(in StyleColors) (StyleColors, error) {
	fields := []*string{&in.Ink, &in.Navy, &in.Accent, &in.Gold, &in.Meta, &in.Rule, &in.Quote, &in.Link, &in.OnDark}
	for _, field := range fields {
		hex, ok := normalizeHex(*field)
		if !ok {
			return StyleColors{}, fmt.Errorf("pdf_style_color_invalid")
		}
		*field = hex
	}
	white := [3]uint8{255, 255, 255}
	// Accent, gold, and rule are 9pt title text and rule glyphs, not only page bars.
	in.Ink = darkenForContrast(in.Ink, white, 4.5)
	in.Navy = darkenForContrast(in.Navy, white, 4.5)
	in.Meta = darkenForContrast(in.Meta, white, 4.5)
	in.Quote = darkenForContrast(in.Quote, white, 4.5)
	in.Link = darkenForContrast(in.Link, white, 4.5)
	in.Accent = darkenForContrast(in.Accent, white, 4.5)
	in.Gold = darkenForContrast(in.Gold, white, 4.5)
	in.Rule = darkenForContrast(in.Rule, white, 4.5)
	navy := mustRGB(in.Navy)
	in.OnDark = lightenForContrast(in.OnDark, navy, 4.5)
	return in, nil
}

func normalizeHex(raw string) (string, bool) {
	hex := strings.TrimPrefix(strings.TrimSpace(raw), "#")
	if len(hex) != 6 {
		return "", false
	}
	for _, r := range hex {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'f', r >= 'A' && r <= 'F':
		default:
			return "", false
		}
	}
	return strings.ToUpper(hex), true
}

func cssHex(hex string) string {
	hex, ok := normalizeHex(hex)
	if !ok {
		return "#1C1917"
	}
	return "#" + hex
}

func mustRGB(hex string) [3]uint8 {
	rgb, ok := parseHex(hex)
	if !ok {
		return [3]uint8{28, 25, 23}
	}
	return rgb
}

func parseHex(hex string) ([3]uint8, bool) {
	norm, ok := normalizeHex(hex)
	if !ok {
		return [3]uint8{}, false
	}
	var rgb [3]uint8
	for i := 0; i < 3; i++ {
		var v byte
		for _, r := range norm[i*2 : i*2+2] {
			v *= 16
			switch {
			case r >= '0' && r <= '9':
				v += byte(r - '0')
			case r >= 'A' && r <= 'F':
				v += byte(r-'A') + 10
			}
		}
		rgb[i] = v
	}
	return rgb, true
}

func darkenForContrast(hex string, against [3]uint8, min float64) string {
	rgb := mustRGB(hex)
	for i := 0; i < 8 && contrastRatio(rgb, against) < min; i++ {
		rgb[0] = uint8(float64(rgb[0]) * 0.82)
		rgb[1] = uint8(float64(rgb[1]) * 0.82)
		rgb[2] = uint8(float64(rgb[2]) * 0.82)
	}
	if contrastRatio(rgb, against) < min {
		return "142033"
	}
	return formatHex(rgb)
}

func lightenForContrast(hex string, against [3]uint8, min float64) string {
	rgb := mustRGB(hex)
	for i := 0; i < 8 && contrastRatio(rgb, against) < min; i++ {
		rgb[0] = uint8(math.Min(255, float64(rgb[0])+28))
		rgb[1] = uint8(math.Min(255, float64(rgb[1])+28))
		rgb[2] = uint8(math.Min(255, float64(rgb[2])+28))
	}
	if contrastRatio(rgb, against) < min {
		return "F8FAFC"
	}
	return formatHex(rgb)
}

func formatHex(rgb [3]uint8) string {
	return fmt.Sprintf("%02X%02X%02X", rgb[0], rgb[1], rgb[2])
}

func contrastRatio(a, b [3]uint8) float64 {
	l1 := relativeLuminance(a)
	l2 := relativeLuminance(b)
	if l1 < l2 {
		l1, l2 = l2, l1
	}
	return (l1 + 0.05) / (l2 + 0.05)
}

func relativeLuminance(rgb [3]uint8) float64 {
	ch := func(v uint8) float64 {
		c := float64(v) / 255
		if c <= 0.03928 {
			return c / 12.92
		}
		return math.Pow((c+0.055)/1.055, 2.4)
	}
	return 0.2126*ch(rgb[0]) + 0.7152*ch(rgb[1]) + 0.0722*ch(rgb[2])
}

// headingLook is the styled heading size, color, and alignment.
// align is empty or "center". size and color are empty when no style is active.
func (s Style) headingLook(prefix string) (size, color, align string) {
	if s.ID == "" {
		return "", "", ""
	}
	navy := cssHex(s.Colors.Navy)
	accent := cssHex(s.Colors.Accent)
	gold := cssHex(s.Colors.Gold)
	ink := cssHex(s.Colors.Ink)
	switch s.Motif {
	case MotifLedger:
		switch prefix {
		case "##### ":
			return "8.5pt", gold, ""
		case "#### ":
			return "9.5pt", accent, ""
		case "### ":
			return "10.5pt", navy, ""
		case "## ":
			return "11.5pt", navy, ""
		default:
			return "13pt", navy, ""
		}
	case MotifFrame:
		switch prefix {
		case "##### ":
			return "8.5pt", gold, "center"
		case "#### ":
			return "9.5pt", accent, "center"
		case "### ":
			return "11pt", navy, ""
		case "## ":
			return "13pt", accent, "center"
		default:
			return "16pt", accent, "center"
		}
	case MotifRail:
		switch prefix {
		case "##### ":
			return "8.5pt", gold, ""
		case "#### ":
			return "9.5pt", navy, ""
		case "### ":
			return "11pt", navy, ""
		case "## ":
			return "13pt", accent, ""
		default:
			return "16pt", ink, ""
		}
	case MotifBand:
		switch prefix {
		case "##### ":
			return "8.5pt", gold, ""
		case "#### ":
			return "9.5pt", gold, ""
		case "### ":
			return "11pt", accent, ""
		case "## ":
			return "13.5pt", accent, ""
		default:
			return "16pt", navy, ""
		}
	default:
		switch prefix {
		case "##### ":
			return "8.5pt", gold, ""
		case "#### ":
			return "9.5pt", accent, ""
		case "### ":
			return "11pt", accent, ""
		case "## ":
			return "13.5pt", navy, ""
		default:
			return "16pt", navy, ""
		}
	}
}
