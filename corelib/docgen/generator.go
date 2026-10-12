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
		return []fontCandidate{
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

	endY += 8
	pdf.SetLineWidth(0.5)
	if spec.Colorful {
		pdf.SetStrokeColor(29, 78, 216)
	} else {
		pdf.SetStrokeColor(200, 200, 200)
	}
	pdf.Line(layout.marginX, endY, layout.marginX+layout.contentW, endY)
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
	titleColor, subtitleColor, projectColor, brandColor := "#1a1a2e", "#888", "#16213e", "#666"
	if spec.Colorful {
		titleColor, subtitleColor, projectColor, brandColor = "#1d4ed8", "#0f766e", "#7c3aed", "#1d4ed8"
	}
	sb.WriteString("<center>")
	sb.WriteString(fmt.Sprintf(`<p style="font-size:18pt; color:%s"><b>%s</b></p>`, titleColor, escapeHTML(title)))
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
	if len(footerParts) == 0 {
		return
	}
	pdf.SetLineWidth(0.3)
	if spec.Colorful {
		pdf.SetStrokeColor(29, 78, 216)
	} else {
		pdf.SetStrokeColor(220, 220, 220)
	}
	pdf.Line(layout.marginX, layout.footerY, layout.marginX+layout.contentW, layout.footerY)
	_ = pdf.SetFont("regular", "", 8)
	if spec.Colorful {
		pdf.SetTextColor(15, 118, 110)
	} else {
		pdf.SetTextColor(120, 120, 120)
	}
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
	if err := pdf.AddTTFFontDataWithOption("bold", g.fontBoldData, gopdf.TtfOption{Style: gopdf.Bold}); err != nil {
		log.Printf("[DocGen] 粗体字体加载失败，使用常规体: %v", err)
		if err := pdf.AddTTFFontData("bold", g.fontRegularData); err != nil {
			return fmt.Errorf("加载常规字体失败: %w", err)
		}
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
}

// markdownToHTML renders this measurer's Markdown through its own scratch.
func (m *measurer) markdownToHTML(markdown string, colorful bool) string {
	return markdownToHTMLWithScratch(m.scratch, markdown, colorful)
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
		candidateParts = append(candidateParts, strings.Join(parts[:mid], "\n"))
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
					pageBlocks = append(pageBlocks, strings.Join(nextParts[:extraCount], "\n"))
					remaining := strings.TrimSpace(strings.Join(nextParts[extraCount:], "\n"))
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
			pageBlocks = []string{strings.Join(subBlocks[:fitCount], "\n")}
			remaining := strings.TrimSpace(strings.Join(subBlocks[fitCount:], "\n"))
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
		currentY = layout.marginY
	}
	return nil
}

func splitOversizedMarkdownBlock(block string) []string {
	// If this is a fenced code block, split by lines but preserve fence markers
	// so each chunk is a valid self-contained code fence
	trimmed := strings.TrimSpace(block)
	if strings.HasPrefix(trimmed, "```") {
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
	closeFence := "```"
	// Find the actual code lines (between open and close fence)
	codeStart := 1
	codeEnd := len(lines)
	for i := len(lines) - 1; i >= 1; i-- {
		if strings.TrimSpace(lines[i]) == "```" {
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
		return []string{block}
	}
	// Fenced code blocks must not be split — keep as single unit
	if strings.HasPrefix(strings.TrimSpace(block), "```") {
		return []string{block}
	}
	if strings.Contains(block, "\n") {
		lines := strings.Split(block, "\n")
		parts := make([]string, 0, len(lines))
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			parts = append(parts, splitParagraphForFilling(line)...)
		}
		return parts
	}
	return splitParagraphForFilling(block)
}

func splitParagraphForFilling(text string) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	if isMarkdownListItem(text) || isMarkdownImageLine(text) {
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
	var parts []string
	var current strings.Builder
	flush := func() {
		piece := strings.TrimSpace(current.String())
		if piece != "" {
			parts = append(parts, piece)
			current.Reset()
		}
	}
	for _, r := range text {
		current.WriteRune(r)
		switch r {
		case '。', '！', '？', '；', ';', '.', '!', '?':
			flush()
		}
	}
	flush()
	return parts
}

func splitTextByComma(text string) []string {
	var parts []string
	var current strings.Builder
	flush := func() {
		piece := strings.TrimSpace(current.String())
		if piece != "" {
			parts = append(parts, piece)
			current.Reset()
		}
	}
	for _, r := range text {
		current.WriteRune(r)
		switch r {
		case '，', '、', ',', ':', '：':
			flush()
		}
	}
	flush()
	return parts
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

		// Track fence state
		if strings.HasPrefix(trimmed, "```") {
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
	for _, line := range lines {
		line = strings.TrimRight(line, " \t")
		if line == "" {
			continue
		}
		candidate := line
		if current.Len() > 0 {
			candidate = current.String() + "\n" + line
		}
		if current.Len() > 0 && len(candidate) > maxChars {
			flush()
		}
		if isMarkdownImageLine(line) {
			flush()
			chunks = append(chunks, line)
			continue
		}
		if len(line) > maxChars {
			flush()
			chunks = append(chunks, splitPlainTextByLength(line, maxChars)...)
			continue
		}
		if current.Len() > 0 {
			current.WriteString("\n")
		}
		current.WriteString(line)
	}
	flush()
	return chunks
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
		chunks = append(chunks, strings.TrimSpace(text[:splitAt]))
		text = strings.TrimSpace(text[splitAt:])
	}
	if text != "" {
		chunks = append(chunks, text)
	}
	return chunks
}
