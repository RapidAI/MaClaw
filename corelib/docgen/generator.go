package docgen

import (
	"encoding/base64"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	gopdf "github.com/VantageDataChat/GoPDF2"
)

// Generator renders Markdown documents into mobile-friendly PDFs.
//
// A Generator is stateless once built and safe to share and reuse: everything
// mutable during rendering lives in per-render objects (see measurer).
type Generator struct {
	fontRegularData []byte
	fontBoldData    []byte
}

type pdfPageLayout struct {
	pageSize *gopdf.Rect
	pageW    float64
	pageH    float64
	marginX  float64
	marginY  float64
	contentW float64
	contentH float64
	footerY  float64
}

var detectSystemFontsOnce sync.Once
var detectedRegularFontData []byte
var detectedBoldFontData []byte
var detectedFontFailure string

// New creates a document generator using detected system fonts. A candidate
// counts only if it parses into embeddable TrueType data (.ttc collections
// are reduced to their first font, CFF-outline fonts are rejected) and, when
// any candidate does, covers Chinese glyphs — so the PDF never comes back
// full of empty boxes.
func New() *Generator {
	regular, bold := detectSystemFonts()
	return &Generator{fontRegularData: regular, fontBoldData: bold}
}

// HasFont reports whether a usable font was found.
func (g *Generator) HasFont() bool {
	return len(g.fontRegularData) > 0
}

// noFontError explains why no font is usable. Naming the last rejection
// reason matters: "font not found" is misleading when the system fonts exist
// but no candidate can be embedded (e.g. CFF-only collections).
func noFontError() error {
	if detectedFontFailure == "" {
		return fmt.Errorf("未找到可用的中文字体，无法生成 PDF")
	}
	return fmt.Errorf("未找到可嵌入 PDF 的中文字体，无法生成 PDF（最后一个候选不可用原因: %s）", detectedFontFailure)
}

func detectSystemFonts() (regular, bold []byte) {
	detectSystemFontsOnce.Do(func() {
		detectedRegularFontData, detectedBoldFontData, detectedFontFailure =
			loadFontCandidates(fontCandidates())
		selectStyleRuleGlyph(detectedRegularFontData)
	})
	return detectedRegularFontData, detectedBoldFontData
}

// loadFontCandidates picks the first candidate that parses into embeddable
// TrueType data. Existence is not enough: on macOS and Linux the system CJK
// faces live in .ttc collections and some (PingFang) are CFF-only, so an
// unusable candidate must fall through to the next one instead of failing
// the export.
// loadFontCandidates returns the first usable candidate. The rejection reason
// is returned rather than stashed in a package global so that the function is
// pure: callers can test it without resetting process-wide state.
func loadFontCandidates(candidates []fontCandidate) (regular, bold []byte, failure string) {
	// First pass demands CJK coverage so a Chinese document never ends up as
	// empty boxes; the second pass accepts any embeddable font so that
	// Latin-only systems can still export English documents.
	if regular, bold, failure := firstEmbeddableFont(candidates, true); len(regular) > 0 {
		return regular, bold, failure
	}
	regular, bold, _ = firstEmbeddableFont(candidates, false)
	return regular, bold, recordFailure("", nil)
}

// firstEmbeddableFont walks candidates until one loads and, when requireCJK,
// covers Chinese glyphs. failure reports the last rejection so diagnostics can
// name it.
func firstEmbeddableFont(candidates []fontCandidate, requireCJK bool) (regular, bold []byte, failure string) {
	for _, c := range candidates {
		regularData, err := loadFontFile(c.regular)
		if err != nil {
			failure = recordFailure(c.regular, err)
			continue
		}
		if requireCJK {
			if err := fontCoversCJK(regularData); err != nil {
				failure = recordFailure(c.regular, err)
				continue
			}
		}
		if c.bold == "" || c.bold == c.regular {
			return regularData, regularData, failure
		}
		if boldData, err := loadFontFile(c.bold); err == nil {
			return regularData, boldData, failure
		}
		log.Printf("[DocGen] 粗体候选字体 %s 不可用，使用常规体: %v", c.bold, err)
		return regularData, regularData, failure
	}
	return nil, nil, failure
}

// recordFailure logs why a candidate was skipped and formats it for the
// user-facing no-font error.
func recordFailure(path string, err error) string {
	if err == nil {
		return ""
	}
	log.Printf("[DocGen] 跳过候选字体 %s: %v", path, err)
	return fmt.Sprintf("%s (%v)", path, err)
}

type fontCandidate struct {
	regular string
	bold    string
}

func fontCandidates() []fontCandidate {
	return fontCandidatesFor(runtime.GOOS)
}

func fontCandidatesFor(goos string) []fontCandidate {
	switch goos {
	case "windows":
		winFonts := os.Getenv("WINDIR") + `\Fonts`
		// Body text is 宋体. Windows does not ship a bold cut of SimSun, so
		// emphasis uses 黑体 — the same pairing as a printed Chinese page.
		// Later candidates are only reached when 宋体 cannot be embedded.
		return []fontCandidate{
			{filepath.Join(winFonts, "simsun.ttc"), filepath.Join(winFonts, "simhei.ttf")},
			{filepath.Join(winFonts, "Deng.ttf"), filepath.Join(winFonts, "Dengb.ttf")},
			{filepath.Join(winFonts, "simhei.ttf"), filepath.Join(winFonts, "simhei.ttf")},
			{filepath.Join(winFonts, "NotoSansSC-VF.ttf"), filepath.Join(winFonts, "NotoSansSC-VF.ttf")},
			{filepath.Join(winFonts, "msyh.ttc"), filepath.Join(winFonts, "msyhbd.ttc")},
		}
	case "darwin":
		// macOS system CJK fonts ship almost exclusively as .ttc
		// collections. PingFang is CFF-outline and cannot be embedded, so
		// it is deliberately absent — detection validates candidates by
		// parsing, and unusable ones fall through automatically.
		return []fontCandidate{
			{"/System/Library/Fonts/STHeiti Light.ttc", "/System/Library/Fonts/STHeiti Medium.ttc"},
			{"/System/Library/Fonts/Supplemental/Songti.ttc", "/System/Library/Fonts/Supplemental/Songti.ttc"},
			{"/System/Library/Fonts/Hiragino Sans GB.ttc", "/System/Library/Fonts/Hiragino Sans GB.ttc"},
			{"/System/Library/Fonts/Supplemental/Arial Unicode.ttf", "/System/Library/Fonts/Supplemental/Arial Unicode.ttf"},
			{"/Library/Fonts/Arial Unicode.ttf", "/Library/Fonts/Arial Unicode.ttf"},
		}
	default:
		return []fontCandidate{
			{"/usr/share/fonts/truetype/noto/NotoSansCJK-Regular.ttc", "/usr/share/fonts/truetype/noto/NotoSansCJK-Bold.ttc"},
			{"/usr/share/fonts/opentype/noto/NotoSansCJK-Regular.ttc", "/usr/share/fonts/opentype/noto/NotoSansCJK-Bold.ttc"},
			{"/usr/share/fonts/truetype/droid/DroidSansFallbackFull.ttf", "/usr/share/fonts/truetype/droid/DroidSansFallbackFull.ttf"},
		}
	}
}

// Generate renders a PDF and returns its bytes.
func (g *Generator) Generate(spec Spec) ([]byte, error) {
	if !g.HasFont() {
		return nil, noFontError()
	}
	if err := ValidateContent(spec.Content); err != nil {
		return nil, err
	}

	spec.Content = stripDuplicateLeadingHeading(spec)

	layout, err := resolvePDFPageLayout(spec.PaperSize)
	if err != nil {
		return nil, err
	}

	pdf := gopdf.GoPdf{}
	pdf.Start(gopdf.Config{PageSize: *layout.pageSize})
	if err := g.addFonts(&pdf); err != nil {
		return nil, err
	}

	pdf.AddPage()
	spec.boundStyle = styleForSpec(spec)
	pageStyle := spec.boundStyle
	paintPageChrome(&pdf, layout, pageStyle)
	titleHTML := buildTitleHTML(spec)
	endY, err := pdf.InsertHTMLBox(layout.marginX, layout.marginY, layout.contentW, 120, titleHTML, gopdf.HTMLBoxOption{
		DefaultFontFamily: "regular",
		DefaultFontSize:   11,
		BoldFontFamily:    "bold",
		LineSpacing:       2,
	})
	if err != nil {
		return nil, fmt.Errorf("渲染标题失败: %w", err)
	}

	endY += 6
	paintTitleUnderline(&pdf, layout, pageStyle, endY)
	endY += 12

	if err := g.renderPagedMarkdown(&pdf, layout, endY, spec); err != nil {
		return nil, fmt.Errorf("渲染正文失败: %w", err)
	}

	data, err := pdf.GetBytesPdfReturnErr()
	if err != nil {
		return nil, fmt.Errorf("生成 PDF 字节失败: %w", err)
	}
	return data, nil
}

// GenerateAndEncode renders a PDF and returns base64 data and filename.
func (g *Generator) GenerateAndEncode(spec Spec) (string, string, error) {
	data, err := g.Generate(spec)
	if err != nil {
		return "", "", err
	}
	return base64.StdEncoding.EncodeToString(data), fileNameForSpec(spec), nil
}

// GenerateToFile renders a PDF and writes it to outputPath.
func GenerateToFile(spec Spec, outputPath string) (string, error) {
	gen := New()
	if !gen.HasFont() {
		return "", noFontError()
	}
	data, err := gen.Generate(spec)
	if err != nil {
		return "", fmt.Errorf("生成 PDF 失败: %w", err)
	}
	if outputPath == "" {
		home, _ := os.UserHomeDir()
		outputPath = filepath.Join(home, fmt.Sprintf("%s_%s.pdf", asciiFileNameSegment(spec.FileNamePrefix, "document"), fileTimestamp(spec).Format("20060102_150405")))
	}
	dir := filepath.Dir(outputPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("创建目录失败: %w", err)
	}
	if err := os.WriteFile(outputPath, data, 0644); err != nil {
		return "", fmt.Errorf("写入 PDF 失败: %w", err)
	}
	return outputPath, nil
}

func fileNameForSpec(spec Spec) string {
	prefix := asciiFileNameSegment(spec.FileNamePrefix, "document")
	project := fallbackText(spec.ProjectName, spec.Title)
	project = asciiFileNameSegment(project, "document")
	return fmt.Sprintf("%s_%s_%s.pdf", prefix, project, fileTimestamp(spec).Format("0102_1504"))
}

func asciiFileNameSegment(value, fallback string) string {
	value = filepath.Base(strings.TrimSpace(value))
	var b strings.Builder
	lastSeparator := false
	for _, r := range strings.ToLower(value) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastSeparator = false
			continue
		}
		if r == '-' || r == '_' || r == '.' || r == ' ' {
			if b.Len() > 0 && !lastSeparator {
				b.WriteByte('_')
				lastSeparator = true
			}
		}
	}
	out := strings.Trim(b.String(), "_")
	if out == "" {
		if strings.TrimSpace(fallback) != "" && fallback != value {
			return asciiFileNameSegment(fallback, "document")
		}
		out = "document"
	}
	if len(out) > 30 {
		out = strings.TrimRight(out[:30], "_")
	}
	if out == "" {
		return "document"
	}
	return out
}

func fileTimestamp(spec Spec) time.Time {
	if !spec.Timestamp.IsZero() {
		return spec.Timestamp
	}
	return time.Now()
}

func buildTitleHTML(spec Spec) string {
	title := fallbackText(spec.Title, spec.ProjectName)
	subtitle := strings.TrimSpace(spec.Subtitle)
	projectLine := strings.TrimSpace(spec.ProjectName)
	brand := strings.TrimSpace(spec.Brand)
	var sb strings.Builder
	titleColor, subtitleColor, projectColor, brandColor, titleSize := styledTitleColors(styleForSpec(spec))
	sb.WriteString("<center>")
	sb.WriteString(fmt.Sprintf(`<p style="font-size:%s; color:%s"><b>%s</b></p>`, titleSize, titleColor, escapeHTML(title)))
	if subtitle != "" {
		sb.WriteString(fmt.Sprintf(`<p style="font-size:9pt; color:%s">%s</p>`, subtitleColor, escapeHTML(subtitle)))
	}
	if projectLine != "" && projectLine != title {
		sb.WriteString(fmt.Sprintf(`<p style="font-size:11pt; color:%s">%s</p>`, projectColor, escapeHTML(projectLine)))
	}
	if brand != "" {
		sb.WriteString(fmt.Sprintf(`<p style="font-size:9pt; color:%s">%s</p>`, brandColor, escapeHTML(brand)))
	}
	sb.WriteString("</center>")
	return sb.String()
}

func stripDuplicateLeadingHeading(spec Spec) string {
	content := strings.TrimLeft(spec.Content, "\ufeff")
	leading := spec.Content[:len(spec.Content)-len(content)]
	lines := strings.Split(content, "\n")
	if len(lines) == 0 {
		return spec.Content
	}
	idx := 0
	for idx < len(lines) && strings.TrimSpace(lines[idx]) == "" {
		idx++
	}
	if idx >= len(lines) {
		return spec.Content
	}
	line := strings.TrimSpace(lines[idx])
	if !strings.HasPrefix(line, "# ") {
		return spec.Content
	}
	heading := normalizeHeadingText(strings.TrimPrefix(line, "# "))
	if heading == "" {
		return spec.Content
	}
	// Check against both ProjectName and Title to catch duplication
	// regardless of which one was used as the PDF display title
	targets := []string{spec.ProjectName, spec.Title}
	matched := false
	for _, t := range targets {
		if normalizeHeadingText(t) == heading {
			matched = true
			break
		}
	}
	if !matched {
		return spec.Content
	}
	trimmedLines := append([]string{}, lines[:idx]...)
	trimmedLines = append(trimmedLines, lines[idx+1:]...)
	for len(trimmedLines) > 0 && strings.TrimSpace(trimmedLines[0]) == "" {
		trimmedLines = trimmedLines[1:]
	}
	return leading + strings.Join(trimmedLines, "\n")
}

func normalizeHeadingText(text string) string {
	trimmed := strings.TrimSpace(text)
	trimmed = strings.TrimRight(trimmed, "#")
	trimmed = strings.TrimSpace(trimmed)
	if trimmed == "" {
		return ""
	}
	return strings.Join(strings.Fields(trimmed), " ")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func addFooter(pdf *gopdf.GoPdf, layout pdfPageLayout, spec Spec) {
	footerParts := make([]string, 0, 2)
	if strings.TrimSpace(spec.Brand) != "" {
		footerParts = append(footerParts, strings.TrimSpace(spec.Brand))
	}
	if strings.TrimSpace(spec.FooterHint) != "" {
		footerParts = append(footerParts, strings.TrimSpace(spec.FooterHint))
	}
	footerStyle := styleForSpec(spec)
	rule := mustRGB(footerStyle.Colors.Rule)
	meta := mustRGB(footerStyle.Colors.Meta)
	pdf.SetLineWidth(0.8)
	pdf.SetStrokeColor(rule[0], rule[1], rule[2])
	pdf.Line(layout.marginX, layout.footerY, layout.marginX+layout.contentW, layout.footerY)
	if len(footerParts) == 0 {
		return
	}
	_ = pdf.SetFont("regular", "", 8)
	pdf.SetTextColor(meta[0], meta[1], meta[2])
	// InsertHTMLBox leaves X at the end of the last line, and SetY does not
	// move X. A Cell from that point runs off the right edge. Draw from the
	// left margin and wrap inside the content width. The box height is the
	// space under the rule, so a second line stays on this page.
	pdf.SetXY(layout.marginX, layout.footerY+4)
	_ = pdf.MultiCell(&gopdf.Rect{
		W: layout.contentW,
		H: layout.marginY - 4,
	}, strings.Join(footerParts, " · "))
}

func normalizePaperSize(paperSize string) (string, error) {
	size := strings.ToLower(strings.TrimSpace(paperSize))
	if size == "" {
		return "a4", nil
	}
	switch size {
	case "a4", "b5":
		return size, nil
	default:
		return "", fmt.Errorf("不支持的纸张类型: %s（仅支持 a4/b5）", paperSize)
	}
}

func resolvePDFPageLayout(paperSize string) (pdfPageLayout, error) {
	normalized, err := normalizePaperSize(paperSize)
	if err != nil {
		return pdfPageLayout{}, err
	}
	var rect *gopdf.Rect
	switch normalized {
	case "b5":
		rect = gopdf.PaperSize("b5")
		if rect == nil {
			rect = gopdf.PageSizeB5
		}
	default:
		rect = gopdf.PaperSize("a4")
		if rect == nil {
			rect = gopdf.PageSizeA4
		}
	}
	if rect == nil {
		return pdfPageLayout{}, fmt.Errorf("无法获取纸张尺寸: %s", normalized)
	}
	marginX := 42.0
	marginY := 36.0
	if normalized == "b5" {
		marginX = 34.0
		marginY = 32.0
	}
	return pdfPageLayout{
		pageSize: rect,
		pageW:    rect.W,
		pageH:    rect.H,
		marginX:  marginX,
		marginY:  marginY,
		contentW: rect.W - marginX*2,
		contentH: rect.H - marginY*2,
		footerY:  rect.H - marginY,
	}, nil
}

// addFonts registers the detected regular/bold font data with the document.
func (g *Generator) addFonts(pdf *gopdf.GoPdf) error {
	if err := pdf.AddTTFFontData("regular", g.fontRegularData); err != nil {
		return fmt.Errorf("加载常规字体失败: %w", err)
	}
	// There is no CJK italic cut. Register the body face under the italic
	// style so a lookup still finds a font. A missing style makes GoPDF drop
	// the word back to regular, and bold inside that word disappears with it.
	if err := pdf.AddTTFFontDataWithOption("regular", g.fontRegularData, gopdf.TtfOption{Style: gopdf.Italic}); err != nil {
		return fmt.Errorf("加载常规字体失败: %w", err)
	}
	boldData := g.fontBoldData
	if err := pdf.AddTTFFontDataWithOption("bold", boldData, gopdf.TtfOption{Style: gopdf.Bold}); err != nil {
		log.Printf("[DocGen] 粗体字体加载失败，使用常规体: %v", err)
		boldData = g.fontRegularData
		if err := pdf.AddTTFFontDataWithOption("bold", boldData, gopdf.TtfOption{Style: gopdf.Bold}); err != nil {
			return fmt.Errorf("加载常规字体失败: %w", err)
		}
	}
	if err := pdf.AddTTFFontDataWithOption("bold", boldData, gopdf.TtfOption{Style: gopdf.Bold | gopdf.Italic}); err != nil {
		return fmt.Errorf("加载粗体字体失败: %w", err)
	}
	return nil
}

func defaultHTMLBoxOption() gopdf.HTMLBoxOption {
	return gopdf.HTMLBoxOption{
		DefaultFontFamily: "regular",
		DefaultFontSize:   10,
		BoldFontFamily:    "bold",
		LineSpacing:       3,
		DefaultColor:      [3]uint8{40, 40, 40},
	}
}

// measurer lays markdown out in a tall box to learn the vertical space it
// needs.
//
// One measurer serves the whole pagination pass, reusing a single GoPdf:
// parsing a CJK font costs ~12ms and the pager measures dozens of candidate
// splits, so building a fresh document per measurement dominated Generate's
// runtime (~130x slower, see TestMeasurementReuseKeepsHeightsStable). Heights
// stay correct because layout depends only on font metrics and box width, not
// on what the document drew before.
//
// A measurer belongs to one pagination pass and must not be shared between
// concurrent renders: it holds a mutable document.
type measurer struct {
	pdf *gopdf.GoPdf
	// Holds the images decoded from data URLs during this render. Owned here
	// so that measuring and drawing share one set of files, and so callers can
	// drop the whole directory when the render ends.
	scratch *dataURLScratch
	style   Style
}

// markdownToHTML renders this measurer's Markdown through its own scratch.
func (m *measurer) markdownToHTML(markdown string, colorful bool) string {
	style := Style{}
	scratch := (*dataURLScratch)(nil)
	if m != nil {
		style = m.style
		scratch = m.scratch
	}
	return markdownToHTMLStyled(scratch, markdown, colorful, style)
}

// newMeasurer builds the measurement document once the page size is known.
func (g *Generator) newMeasurer(layout pdfPageLayout) (*measurer, error) {
	pdf := &gopdf.GoPdf{}
	pdf.Start(gopdf.Config{PageSize: *layout.pageSize})
	if err := g.addFonts(pdf); err != nil {
		return nil, err
	}
	pdf.AddPage()
	return &measurer{pdf: pdf, scratch: newDataURLScratch()}, nil
}

// markdownMeasureBoxH is only for measuring. InsertHTMLBox drops an image that
// crosses the bottom of its box and still returns the height of the text it
// drew. A page-sized box therefore hides a chart, and the pager keeps a page
// that will not draw it. This box is tall enough to count every chart. The
// caller compares that height with the space above the footer.
const markdownMeasureBoxH = 1e6

func (m *measurer) measureMarkdownHeight(layout pdfPageLayout, currentY float64, markdown string, colorful bool) (float64, error) {
	endY, err := m.pdf.InsertHTMLBox(layout.marginX, currentY, layout.contentW, markdownMeasureBoxH, m.markdownToHTML(markdown, colorful), defaultHTMLBoxOption())
	if err != nil {
		return 0, err
	}
	return endY - currentY, nil
}

func (m *measurer) markdownFits(layout pdfPageLayout, currentY float64, markdown string, remainH float64, colorful bool) (bool, error) {
	height, err := m.measureMarkdownHeight(layout, currentY, markdown, colorful)
	if err != nil {
		return false, err
	}
	return height <= remainH, nil
}

// fitBlockPrefix binary-searches how many leading blocks fit above the footer.
func (m *measurer) fitBlockPrefix(layout pdfPageLayout, currentY float64, blocks []string, colorful bool) (int, error) {
	if len(blocks) == 0 {
		return 0, nil
	}
	remainH := layout.footerY - 4 - currentY
	if remainH <= 0 {
		return 0, fmt.Errorf("页面剩余高度不足")
	}
	low, high := 1, len(blocks)
	best := 0
	for low <= high {
		mid := (low + high) / 2
		candidate := strings.Join(blocks[:mid], "\n\n")
		fits, err := m.markdownFits(layout, currentY, candidate, remainH, colorful)
		if err != nil {
			return 0, err
		}
		if fits {
			best = mid
			low = mid + 1
		} else {
			high = mid - 1
		}
	}
	return best, nil
}

func (m *measurer) fitBlockPartPrefix(layout pdfPageLayout, currentY float64, baseBlocks, parts []string, colorful bool) (int, error) {
	if len(parts) == 0 {
		return 0, nil
	}
	remainH := layout.footerY - 4 - currentY
	if remainH <= 0 {
		return 0, fmt.Errorf("页面剩余高度不足")
	}
	low, high := 1, len(parts)
	best := 0
	for low <= high {
		mid := (low + high) / 2
		candidateParts := append([]string{}, baseBlocks...)
		candidateParts = append(candidateParts, joinFillingParts(parts[:mid]))
		candidate := strings.Join(candidateParts, "\n\n")
		fits, err := m.markdownFits(layout, currentY, candidate, remainH, colorful)
		if err != nil {
			return 0, err
		}
		if fits {
			best = mid
			low = mid + 1
		} else {
			high = mid - 1
		}
	}
	return best, nil
}

func (g *Generator) renderPagedMarkdown(pdf *gopdf.GoPdf, layout pdfPageLayout, firstPageY float64, spec Spec) error {
	blocks := splitMarkdownBlocks(spec.Content)
	if len(blocks) == 0 {
		addFooter(pdf, layout, spec)
		return nil
	}
	m, err := g.newMeasurer(layout)
	if err != nil {
		return err
	}
	m.style = styleForSpec(spec)
	// Every figure decoded from a data URL lands in a temporary directory for
	// this render. Deferring the cleanup covers every return path below,
	// including the ones that bail out on a measurement error.
	defer m.scratch.cleanup()
	currentY := firstPageY
	for len(blocks) > 0 {
		fitCount, err := m.fitBlockPrefix(layout, currentY, blocks, spec.Colorful)
		if err != nil {
			return err
		}
		pageBlocks := append([]string{}, blocks[:fitCount]...)
		remainingBlocks := append([]string{}, blocks[fitCount:]...)
		if fitCount < len(blocks) {
			nextParts := splitMarkdownBlockForFilling(blocks[fitCount])
			if len(nextParts) > 1 {
				extraCount, err := m.fitBlockPartPrefix(layout, currentY, pageBlocks, nextParts, spec.Colorful)
				if err != nil {
					return err
				}
				if extraCount > 0 {
					pageBlocks = append(pageBlocks, joinFillingParts(nextParts[:extraCount]))
					remaining := strings.TrimSpace(joinFillingParts(nextParts[extraCount:]))
					remainingBlocks = remainingBlocks[1:]
					if remaining != "" {
						remainingBlocks = append([]string{remaining}, remainingBlocks...)
					}
				}
			}
		}
		if len(pageBlocks) == 0 {
			// The title page starts below the header. An image that does not
			// fit there still fits at the top of the next page. Splitting it
			// would cut the path on the dots in the file name.
			if currentY > layout.marginY+1 {
				addFooter(pdf, layout, spec)
				pdf.AddPage()
				paintPageChrome(pdf, layout, m.style)
				currentY = layout.marginY
				continue
			}
			subBlocks := splitOversizedMarkdownBlock(blocks[0])
			if len(subBlocks) == 0 {
				return fmt.Errorf("正文块过大，无法分页渲染")
			}
			fitCount, err = m.fitBlockPartPrefix(layout, currentY, nil, subBlocks, spec.Colorful)
			if err != nil {
				return err
			}
			if fitCount == 0 {
				return fmt.Errorf("正文块过大，无法放入当前页")
			}
			pageBlocks = []string{joinFillingParts(subBlocks[:fitCount])}
			remaining := strings.TrimSpace(joinFillingParts(subBlocks[fitCount:]))
			remainingBlocks = blocks[1:]
			if remaining != "" {
				remainingBlocks = append([]string{remaining}, remainingBlocks...)
			}
		}
		pageMarkdown := strings.Join(pageBlocks, "\n\n")
		if _, err := pdf.InsertHTMLBox(layout.marginX, currentY, layout.contentW, layout.pageH, m.markdownToHTML(pageMarkdown, spec.Colorful), defaultHTMLBoxOption()); err != nil {
			return err
		}
		addFooter(pdf, layout, spec)
		blocks = remainingBlocks
		if len(blocks) == 0 {
			break
		}
		pdf.AddPage()
		paintPageChrome(pdf, layout, m.style)
		currentY = layout.marginY
	}
	return nil
}

func splitOversizedMarkdownBlock(block string) []string {
	// If this is a fenced code block, split by lines but preserve fence markers
	// so each chunk is a valid self-contained code fence
	trimmed := strings.TrimSpace(block)
	if marker, ok := markdownFenceMarker(trimmed); ok && strings.HasPrefix(trimmed, marker) {
		return splitOversizedCodeBlock(block)
	}
	parts := splitLongMarkdownBlock(block, 240)
	if len(parts) == 0 {
		return nil
	}
	return parts
}

// splitOversizedCodeBlock splits a large fenced code block into multiple
// self-contained fenced blocks, each renderable independently.
func splitOversizedCodeBlock(block string) []string {
	lines := strings.Split(block, "\n")
	if len(lines) < 2 {
		return []string{block}
	}
	// Extract opening fence line (e.g. "```python") and closing fence
	openFence := lines[0]
	marker, _ := markdownFenceMarker(openFence)
	if marker == "" {
		marker = "```"
	}
	closeFence := marker
	// Find the actual code lines (between open and close fence)
	codeStart := 1
	codeEnd := len(lines)
	for i := len(lines) - 1; i >= 1; i-- {
		if isClosingFence(lines[i], marker) {
			codeEnd = i
			break
		}
	}
	codeLines := lines[codeStart:codeEnd]
	if len(codeLines) == 0 {
		return []string{block}
	}
	// Split code lines into chunks of ~30 lines each (roughly half a page)
	const linesPerChunk = 30
	var chunks []string
	for i := 0; i < len(codeLines); i += linesPerChunk {
		end := i + linesPerChunk
		if end > len(codeLines) {
			end = len(codeLines)
		}
		chunk := openFence + "\n" + strings.Join(codeLines[i:end], "\n") + "\n" + closeFence
		chunks = append(chunks, chunk)
	}
	return chunks
}

func splitMarkdownBlockForFilling(block string) []string {
	block = strings.TrimSpace(block)
	if block == "" {
		return nil
	}
	if isMarkdownTableBlock(block) {
		return markdownTablePieces(block)
	}
	lines := strings.Split(block, "\n")
	// A fence that fills the block stays in one piece, including an unclosed
	// fence. A fence with a table after it does not: the table still has to
	// be able to break across pages.
	if _, ok := markdownFenceMarker(lines[0]); ok {
		if _, next, closed := markdownFenceSpan(lines, 0); !closed || next == len(lines) {
			return []string{block}
		}
	}
	if strings.Contains(block, "\n") {
		return splitMarkdownLinesForFilling(lines)
	}
	return splitParagraphForFilling(block)
}

// splitMarkdownLinesForFilling breaks a block into pieces that still fit a
// page, without tearing a table apart.
//
// A heading sitting on the line above a table is one block (there is no blank
// line between them). Splitting that block line by line, then again on ".",
// turns "Qwen2.5-3B | 0.382" into separate fragments, and the table is no
// longer a table when it is drawn.
func splitMarkdownLinesForFilling(lines []string) []string {
	parts := make([]string, 0, len(lines))
	for i := 0; i < len(lines); {
		if strings.TrimSpace(lines[i]) == "" {
			i++
			continue
		}
		if fence, next, ok := markdownFenceSpan(lines, i); ok {
			parts = append(parts, fence)
			i = next
			continue
		}
		if table, next := markdownTableSpan(lines, i); next > i {
			parts = append(parts, markdownTablePieces(table)...)
			i = next
			continue
		}
		parts = append(parts, splitParagraphForFilling(strings.TrimSpace(lines[i]))...)
		i++
	}
	return parts
}

// markdownTableSpan returns the table that begins at start, and the index of
// the first line after it. A table is a header row, a separator, and at least
// one data row. Anything else returns start unchanged.
func markdownTableSpan(lines []string, start int) (string, int) {
	if start+1 >= len(lines) {
		return "", start
	}
	header := strings.TrimSpace(lines[start])
	separator := strings.TrimSpace(lines[start+1])
	if !looksLikeMarkdownTableRow(header) || !isMarkdownTableSeparator(separator) {
		return "", start
	}
	end := start + 2
	for end < len(lines) {
		row := strings.TrimSpace(lines[end])
		if row == "" || !looksLikeMarkdownTableRow(row) {
			break
		}
		end++
	}
	if end == start+2 {
		return "", start
	}
	return strings.Join(lines[start:end], "\n"), end
}

// markdownFenceSpan returns a closed fence that begins at start.
//
// A heading sitting on the line above a fence is one block. Walking those
// lines one by one cuts "1.5" inside the code at the period, and the fence
// no longer shows the source the reader wrote.
func markdownFenceSpan(lines []string, start int) (string, int, bool) {
	marker, ok := markdownFenceMarker(lines[start])
	if !ok {
		return "", start, false
	}
	for j := start + 1; j < len(lines); j++ {
		closeMarker, cok := markdownFenceMarker(lines[j])
		if !cok || closeMarker[0] != marker[0] || len(closeMarker) < len(marker) {
			continue
		}
		rest := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(lines[j]), closeMarker))
		if rest != "" {
			continue
		}
		return strings.Join(lines[start:j+1], "\n"), j + 1, true
	}
	return "", start, false
}

func isClosingFence(line, marker string) bool {
	if marker == "" {
		return false
	}
	trimmed := strings.TrimSpace(line)
	closeMarker, ok := markdownFenceMarker(trimmed)
	if !ok || closeMarker[0] != marker[0] || len(closeMarker) < len(marker) {
		return false
	}
	return strings.TrimSpace(strings.TrimPrefix(trimmed, closeMarker)) == ""
}

// markdownFenceMarker reports the backtick or tilde run that opens or closes
// a fence. The info string after an opening run is not part of the marker.
func markdownFenceMarker(line string) (string, bool) {
	trimmed := strings.TrimSpace(line)
	if len(trimmed) < 3 {
		return "", false
	}
	ch := trimmed[0]
	if ch != '`' && ch != '~' {
		return "", false
	}
	n := 0
	for n < len(trimmed) && trimmed[n] == ch {
		n++
	}
	if n < 3 {
		return "", false
	}
	return trimmed[:n], true
}

// markdownTablePieces turns one table into one piece per data row. Each piece
// still carries the header, so a row that lands on the next page is a table
// on its own. Rows that stay on the same page are folded back together by
// joinFillingParts.
func markdownTablePieces(table string) []string {
	lines := strings.Split(strings.TrimSpace(table), "\n")
	if len(lines) < 3 {
		return []string{strings.TrimSpace(table)}
	}
	header := strings.TrimSpace(lines[0]) + "\n" + strings.TrimSpace(lines[1])
	pieces := make([]string, 0, len(lines)-2)
	for _, row := range lines[2:] {
		row = strings.TrimSpace(row)
		if row == "" || !looksLikeMarkdownTableRow(row) {
			break
		}
		pieces = append(pieces, header+"\n"+row)
	}
	if len(pieces) == 0 {
		return []string{strings.TrimSpace(table)}
	}
	return pieces
}

// joinFillingParts puts filling pieces back into markdown.
//
// Consecutive pieces of the same table become one table again. A piece that
// starts the next page keeps its own header, so the table does not turn into
// a paragraph of pipes.
func joinFillingParts(parts []string) string {
	var b strings.Builder
	var header string
	var rows []string
	flush := func() {
		if header == "" {
			return
		}
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(header)
		for _, row := range rows {
			b.WriteByte('\n')
			b.WriteString(row)
		}
		header = ""
		rows = nil
	}
	for _, part := range parts {
		tableHeader, dataRows, ok := splitCompleteTable(part)
		if !ok {
			flush()
			if b.Len() > 0 {
				b.WriteByte('\n')
			}
			b.WriteString(part)
			continue
		}
		if header != "" && header == tableHeader {
			rows = append(rows, dataRows...)
			continue
		}
		flush()
		header = tableHeader
		rows = append(rows, dataRows...)
	}
	flush()
	return b.String()
}

// splitCompleteTable reports the header (header row plus separator) and the
// data rows of a piece that is itself a complete markdown table.
func splitCompleteTable(part string) (header string, rows []string, ok bool) {
	lines := strings.Split(strings.TrimSpace(part), "\n")
	if len(lines) < 3 || !looksLikeMarkdownTableRow(lines[0]) || !isMarkdownTableSeparator(lines[1]) {
		return "", nil, false
	}
	for _, row := range lines[2:] {
		if strings.TrimSpace(row) == "" || !looksLikeMarkdownTableRow(row) {
			return "", nil, false
		}
	}
	return lines[0] + "\n" + lines[1], lines[2:], true
}

func splitParagraphForFilling(text string) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	if _, isHeading := markdownHeadingPrefix(text); isHeading || strings.HasPrefix(text, ">") || isMarkdownListItem(text) || isMarkdownImageLine(text) || looksLikeMarkdownTableRow(text) || isMarkdownTableSeparator(text) {
		return []string{text}
	}
	parts := splitTextByPunctuation(text)
	if len(parts) <= 1 {
		parts = splitTextByComma(text)
	}
	if len(parts) <= 1 {
		parts = splitPlainTextByLength(text, 80)
	}
	return parts
}

func isMarkdownImageLine(text string) bool {
	return markdownImageRe.MatchString(strings.TrimSpace(text))
}

func isMarkdownListItem(text string) bool {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return false
	}
	if strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") {
		return true
	}
	return markdownOrderedListRe.MatchString(trimmed)
}

func splitTextByPunctuation(text string) []string {
	return splitOutsideMarkup(text, func(r rune) bool {
		switch r {
		case '。', '！', '？', '；', ';', '.', '!', '?':
			return true
		default:
			return false
		}
	})
}

func splitTextByComma(text string) []string {
	return splitOutsideMarkup(text, func(r rune) bool {
		switch r {
		case '，', '、', ',', ':', '：':
			return true
		default:
			return false
		}
	})
}

// splitOutsideMarkup cuts text on isBreak, except inside a closed bold, italic,
// code, or math span. An unclosed marker is left unprotected so a stray "*"
// cannot freeze the rest of the paragraph into one unsplittable piece.
func splitOutsideMarkup(text string, isBreak func(rune) bool) []string {
	runes := []rune(text)
	protected := markupProtected(runes)
	var parts []string
	var current strings.Builder
	flush := func() {
		piece := strings.TrimSpace(current.String())
		if piece != "" {
			parts = append(parts, piece)
			current.Reset()
		}
	}
	for i, r := range runes {
		current.WriteRune(r)
		if protected[i] || !isBreak(r) || keepsInlineDot(runes, i) || keepsRepeatedPunct(runes, i) || keepsTightColon(runes, i) {
			continue
		}
		flush()
	}
	flush()
	return parts
}

// keepsNumberDot reports a dot inside a number, such as 0.382 or Qwen2.5.
// That dot is not the end of a sentence.
func keepsNumberDot(runes []rune, i int) bool {
	if i <= 0 || i+1 >= len(runes) || runes[i] != '.' {
		return false
	}
	return runes[i-1] >= '0' && runes[i-1] <= '9' && runes[i+1] >= '0' && runes[i+1] <= '9'
}

// keepsInlineDot reports a dot inside a token, such as github.com or file.txt.
// A sentence period is followed by a space or the end of the line, not another letter.
func keepsInlineDot(runes []rune, i int) bool {
	if keepsNumberDot(runes, i) {
		return true
	}
	if i <= 0 || i+1 >= len(runes) || runes[i] != '.' {
		return false
	}
	return isDotTokenRune(runes[i-1]) && isDotTokenRune(runes[i+1])
}

func isDotTokenRune(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-'
}

// keepsRepeatedPunct reports a mark inside an ellipsis or a run such as
// "..." or "?!". Splitting between those marks leaves a lone "." on the page.
func keepsRepeatedPunct(runes []rune, i int) bool {
	if i < 0 || i >= len(runes) || !isSentenceMark(runes[i]) {
		return false
	}
	if i > 0 && isSentenceMark(runes[i-1]) {
		return true
	}
	return i+1 < len(runes) && isSentenceMark(runes[i+1])
}

func isSentenceMark(r rune) bool {
	return r == '.' || r == '!' || r == '?'
}

// keepsTightColon reports a colon that belongs to a URL or an id, such as
// https:// or arXiv:2510. A fullwidth colon still ends a clause.
func keepsTightColon(runes []rune, i int) bool {
	if i < 0 || i+1 >= len(runes) || runes[i] != ':' {
		return false
	}
	next := runes[i+1]
	return next == '/' || next == '\\' || (next >= '0' && next <= '9')
}

// markupProtected marks runes that belong to a closed inline span.
func markupProtected(runes []rune) []bool {
	prot := make([]bool, len(runes))
	for i := 0; i < len(runes); {
		switch runes[i] {
		case '*':
			if i+1 < len(runes) && runes[i+1] == '*' {
				if j := indexRunePair(runes, i+2, '*'); j >= 0 {
					markProtected(prot, i, j+2)
					i = j + 2
					continue
				}
			} else if j := indexRune(runes, i+1, '*'); j >= 0 && !runePairAt(runes, j, '*') {
				markProtected(prot, i, j+1)
				i = j + 1
				continue
			}
		case '_':
			if i+1 < len(runes) && runes[i+1] == '_' {
				if j := indexRunePair(runes, i+2, '_'); j >= 0 {
					markProtected(prot, i, j+2)
					i = j + 2
					continue
				}
			}
		case '`':
			if j := indexRune(runes, i+1, '`'); j >= 0 {
				markProtected(prot, i, j+1)
				i = j + 1
				continue
			}
		case '$':
			if j := indexRune(runes, i+1, '$'); j >= 0 {
				markProtected(prot, i, j+1)
				i = j + 1
				continue
			}
		}
		i++
	}
	return prot
}

func markProtected(prot []bool, from, to int) {
	for k := from; k < to && k < len(prot); k++ {
		prot[k] = true
	}
}

func indexRune(runes []rune, from int, ch rune) int {
	for j := from; j < len(runes); j++ {
		if runes[j] == ch {
			return j
		}
	}
	return -1
}

func indexRunePair(runes []rune, from int, ch rune) int {
	for j := from; j+1 < len(runes); j++ {
		if runes[j] == ch && runes[j+1] == ch {
			return j
		}
	}
	return -1
}

func runePairAt(runes []rune, i int, ch rune) bool {
	return i+1 < len(runes) && runes[i] == ch && runes[i+1] == ch
}

func splitMarkdownBlocks(md string) []string {
	// Split on double newlines but respect fenced code blocks —
	// blank lines inside ``` fences should not cause a split.
	lines := strings.Split(md, "\n")
	var blocks []string
	var current strings.Builder
	inFence := false

	flush := func() {
		text := strings.TrimSpace(current.String())
		if text != "" {
			blocks = append(blocks, text)
		}
		current.Reset()
	}

	for i := 0; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])

		// Track fence state. Tilde fences count too, so a blank line inside
		// ~~~ does not become a block boundary.
		if _, ok := markdownFenceMarker(trimmed); ok {
			inFence = !inFence
		}

		// Inside a fence, never split — accumulate everything
		if inFence {
			if current.Len() > 0 {
				current.WriteByte('\n')
			}
			current.WriteString(lines[i])
			continue
		}

		// Outside fence: blank line = block boundary
		if trimmed == "" {
			flush()
			continue
		}

		if current.Len() > 0 {
			current.WriteByte('\n')
		}
		current.WriteString(lines[i])
	}
	flush()
	return blocks
}

func splitLongMarkdownBlock(block string, maxChars int) []string {
	lines := strings.Split(block, "\n")
	var chunks []string
	var current strings.Builder
	flush := func() {
		text := strings.TrimSpace(current.String())
		if text != "" {
			chunks = append(chunks, text)
			current.Reset()
		}
	}
	for i := 0; i < len(lines); {
		line := strings.TrimRight(lines[i], " \t")
		if line == "" {
			i++
			continue
		}
		if fence, next, ok := markdownFenceSpan(lines, i); ok {
			if len(fence) > maxChars {
				flush()
				chunks = append(chunks, splitOversizedCodeBlock(fence)...)
			} else if current.Len() > 0 && current.Len()+1+len(fence) > maxChars {
				flush()
				current.WriteString(fence)
			} else {
				if current.Len() > 0 {
					current.WriteString("\n")
				}
				current.WriteString(fence)
			}
			i = next
			continue
		}
		if table, next := markdownTableSpan(lines, i); next > i {
			if len(table) > maxChars {
				flush()
				chunks = append(chunks, markdownTablePieces(table)...)
			} else {
				if current.Len() > 0 && current.Len()+1+len(table) > maxChars {
					flush()
				}
				if current.Len() > 0 {
					current.WriteString("\n")
				}
				current.WriteString(table)
			}
			i = next
			continue
		}
		if isMarkdownImageLine(line) {
			flush()
			chunks = append(chunks, line)
			i++
			continue
		}
		if len(line) > maxChars && !looksLikeMarkdownTableRow(line) {
			flush()
			chunks = append(chunks, splitPlainTextByLength(line, maxChars)...)
			i++
			continue
		}
		candidate := line
		if current.Len() > 0 {
			candidate = current.String() + "\n" + line
		}
		if current.Len() > 0 && len(candidate) > maxChars {
			flush()
		}
		if current.Len() > 0 {
			current.WriteString("\n")
		}
		current.WriteString(line)
		i++
	}
	flush()
	return chunks
}

// avoidMarkupCut moves a byte cut out of a closed bold, italic, code, or math
// span. Cutting there would leave the markers on opposite pieces, and the
// PDF would draw the words without the emphasis.
func avoidMarkupCut(text string, splitAt int) int {
	if splitAt <= 0 || splitAt >= len(text) {
		return splitAt
	}
	runes := []rune(text)
	protected := markupProtected(runes)
	bytePos := 0
	runeIdx := 0
	for runeIdx < len(runes) && bytePos < splitAt {
		bytePos += utf8.RuneLen(runes[runeIdx])
		runeIdx++
	}
	if runeIdx >= len(protected) || !protected[runeIdx] {
		return splitAt
	}
	start := runeIdx
	for start > 0 && protected[start-1] {
		start--
	}
	end := runeIdx
	for end < len(protected) && protected[end] {
		end++
	}
	startByte := runeByteOffset(runes, start)
	if startByte > 0 {
		return startByte
	}
	endByte := runeByteOffset(runes, end)
	if endByte > splitAt {
		return endByte
	}
	return splitAt
}

func snapToRuneBoundary(text string, i int) int {
	if i <= 0 || i >= len(text) {
		return i
	}
	for i > 0 && !utf8.RuneStart(text[i]) {
		i--
	}
	return i
}

func runeByteOffset(runes []rune, index int) int {
	n := 0
	for i := 0; i < index && i < len(runes); i++ {
		n += utf8.RuneLen(runes[i])
	}
	return n
}

func splitPlainTextByLength(text string, maxChars int) []string {
	if maxChars <= 0 || len(text) <= maxChars {
		return []string{text}
	}
	var chunks []string
	for len(text) > maxChars {
		splitAt := strings.LastIndex(text[:maxChars], " ")
		if splitAt < maxChars/2 {
			splitAt = maxChars
		}
		// maxChars counts bytes. Landing inside a Chinese character would
		// write a broken rune into the PDF.
		splitAt = snapToRuneBoundary(text, splitAt)
		splitAt = avoidMarkupCut(text, splitAt)
		if splitAt <= 0 || splitAt >= len(text) {
			break
		}
		chunks = append(chunks, strings.TrimSpace(text[:splitAt]))
		text = strings.TrimSpace(text[splitAt:])
	}
	if text != "" {
		chunks = append(chunks, text)
	}
	return chunks
}
