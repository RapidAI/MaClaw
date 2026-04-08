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
type Generator struct {
	fontRegular string
	fontBold    string
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
var detectedRegularFont string
var detectedBoldFont string

// New creates a document generator using detected system fonts.
func New() *Generator {
	regular, bold := detectSystemFonts()
	return &Generator{fontRegular: regular, fontBold: bold}
}

// HasFont reports whether a usable font was found.
func (g *Generator) HasFont() bool {
	return g.fontRegular != ""
}

func detectSystemFonts() (regular, bold string) {
	detectSystemFontsOnce.Do(func() {
		candidates := fontCandidates()
		for _, c := range candidates {
			if _, err := os.Stat(c.regular); err == nil {
				detectedRegularFont = c.regular
				detectedBoldFont = c.bold
				if detectedBoldFont == "" {
					detectedBoldFont = detectedRegularFont
				} else if _, err := os.Stat(detectedBoldFont); err != nil {
					detectedBoldFont = detectedRegularFont
				}
				return
			}
		}
	})
	return detectedRegularFont, detectedBoldFont
}

type fontCandidate struct {
	regular string
	bold    string
}

func fontCandidates() []fontCandidate {
	switch runtime.GOOS {
	case "windows":
		winFonts := os.Getenv("WINDIR") + `\Fonts`
		return []fontCandidate{
			{filepath.Join(winFonts, "Deng.ttf"), filepath.Join(winFonts, "Dengb.ttf")},
			{filepath.Join(winFonts, "simhei.ttf"), filepath.Join(winFonts, "simhei.ttf")},
			{filepath.Join(winFonts, "NotoSansSC-VF.ttf"), filepath.Join(winFonts, "NotoSansSC-VF.ttf")},
			{filepath.Join(winFonts, "msyh.ttc"), filepath.Join(winFonts, "msyhbd.ttc")},
		}
	case "darwin":
		return []fontCandidate{
			{"/System/Library/Fonts/STHeiti Light.ttc", "/System/Library/Fonts/STHeiti Medium.ttc"},
			{"/System/Library/Fonts/PingFang.ttc", "/System/Library/Fonts/PingFang.ttc"},
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
		return nil, fmt.Errorf("未找到可用的中文字体，无法生成 PDF")
	}

	spec.Content = stripDuplicateLeadingHeading(spec)

	layout, err := resolvePDFPageLayout(spec.PaperSize)
	if err != nil {
		return nil, err
	}

	pdf := gopdf.GoPdf{}
	pdf.Start(gopdf.Config{PageSize: *layout.pageSize})
	if err := pdf.AddTTFFont("regular", g.fontRegular); err != nil {
		return nil, fmt.Errorf("加载常规字体失败: %w", err)
	}
	if err := pdf.AddTTFFontWithOption("bold", g.fontBold, gopdf.TtfOption{Style: gopdf.Bold}); err != nil {
		log.Printf("[DocGen] 粗体字体加载失败，使用常规体: %v", err)
		_ = pdf.AddTTFFont("bold", g.fontRegular)
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
	pdf.SetStrokeColor(200, 200, 200)
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
		return "", fmt.Errorf("未找到可用的中文字体，无法生成 PDF")
	}
	data, err := gen.Generate(spec)
	if err != nil {
		return "", fmt.Errorf("生成 PDF 失败: %w", err)
	}
	if outputPath == "" {
		home, _ := os.UserHomeDir()
		outputPath = filepath.Join(home, fmt.Sprintf("%s_%s.pdf", sanitizeFileName(fallbackText(spec.Title, "文档")), fileTimestamp(spec).Format("20060102_150405")))
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
	prefix := fallbackText(spec.FileNamePrefix, "文档")
	project := fallbackText(spec.ProjectName, spec.Title)
	return fmt.Sprintf("%s_%s_%s.pdf", prefix, sanitizeFileName(project), fileTimestamp(spec).Format("0102_1504"))
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
	sb.WriteString("<center>")
	sb.WriteString(fmt.Sprintf(`<p style="font-size:18pt; color:#1a1a2e"><b>%s</b></p>`, escapeHTML(title)))
	if subtitle != "" {
		sb.WriteString(fmt.Sprintf(`<p style="font-size:9pt; color:#888">%s</p>`, escapeHTML(subtitle)))
	}
	if projectLine != "" && projectLine != title {
		sb.WriteString(fmt.Sprintf(`<p style="font-size:11pt; color:#16213e">%s</p>`, escapeHTML(projectLine)))
	}
	if brand != "" {
		sb.WriteString(fmt.Sprintf(`<p style="font-size:9pt; color:#666">%s</p>`, escapeHTML(brand)))
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
	target := normalizeHeadingText(firstNonEmpty(spec.ProjectName, spec.Title))
	if heading == "" || target == "" || heading != target {
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
	pdf.SetLineWidth(0.3)
	pdf.SetStrokeColor(220, 220, 220)
	pdf.Line(layout.marginX, layout.footerY, layout.marginX+layout.contentW, layout.footerY)
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
	pdf.SetY(layout.footerY + 4)
	_ = pdf.SetFont("regular", "", 8)
	pdf.SetTextColor(120, 120, 120)
	_ = pdf.Cell(nil, strings.Join(footerParts, " · "))
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

func defaultHTMLBoxOption() gopdf.HTMLBoxOption {
	return gopdf.HTMLBoxOption{
		DefaultFontFamily: "regular",
		DefaultFontSize:   10,
		BoldFontFamily:    "bold",
		LineSpacing:       3,
		DefaultColor:      [3]uint8{40, 40, 40},
	}
}

func (g *Generator) newMeasurementPDF(layout pdfPageLayout) (*gopdf.GoPdf, error) {
	pdf := &gopdf.GoPdf{}
	pdf.Start(gopdf.Config{PageSize: *layout.pageSize})
	if err := pdf.AddTTFFont("regular", g.fontRegular); err != nil {
		return nil, fmt.Errorf("加载常规字体失败: %w", err)
	}
	if err := pdf.AddTTFFontWithOption("bold", g.fontBold, gopdf.TtfOption{Style: gopdf.Bold}); err != nil {
		_ = pdf.AddTTFFont("bold", g.fontRegular)
	}
	pdf.AddPage()
	return pdf, nil
}

func (g *Generator) measureMarkdownHeight(layout pdfPageLayout, currentY float64, markdown string) (float64, error) {
	pdf, err := g.newMeasurementPDF(layout)
	if err != nil {
		return 0, err
	}
	endY, err := pdf.InsertHTMLBox(layout.marginX, currentY, layout.contentW, layout.pageH, markdownToHTML(markdown), defaultHTMLBoxOption())
	if err != nil {
		return 0, err
	}
	return endY - currentY, nil
}

func (g *Generator) fitBlockPrefix(layout pdfPageLayout, currentY float64, blocks []string) (int, error) {
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
		height, err := g.measureMarkdownHeight(layout, currentY, candidate)
		if err != nil {
			return 0, err
		}
		if height <= remainH {
			best = mid
			low = mid + 1
		} else {
			high = mid - 1
		}
	}
	return best, nil
}

func (g *Generator) fitBlockPartPrefix(layout pdfPageLayout, currentY float64, baseBlocks, parts []string) (int, error) {
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
		height, err := g.measureMarkdownHeight(layout, currentY, candidate)
		if err != nil {
			return 0, err
		}
		if height <= remainH {
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
	currentY := firstPageY
	for len(blocks) > 0 {
		fitCount, err := g.fitBlockPrefix(layout, currentY, blocks)
		if err != nil {
			return err
		}
		pageBlocks := append([]string{}, blocks[:fitCount]...)
		remainingBlocks := append([]string{}, blocks[fitCount:]...)
		if fitCount < len(blocks) {
			nextParts := splitMarkdownBlockForFilling(blocks[fitCount])
			if len(nextParts) > 1 {
				extraCount, err := g.fitBlockPartPrefix(layout, currentY, pageBlocks, nextParts)
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
			subBlocks := splitOversizedMarkdownBlock(blocks[0])
			if len(subBlocks) == 0 {
				return fmt.Errorf("正文块过大，无法分页渲染")
			}
			fitCount, err = g.fitBlockPartPrefix(layout, currentY, nil, subBlocks)
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
		if _, err := pdf.InsertHTMLBox(layout.marginX, currentY, layout.contentW, layout.pageH, markdownToHTML(pageMarkdown), defaultHTMLBoxOption()); err != nil {
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
	parts := splitLongMarkdownBlock(block, 240)
	if len(parts) == 0 {
		return nil
	}
	return parts
}

func splitMarkdownBlockForFilling(block string) []string {
	block = strings.TrimSpace(block)
	if block == "" {
		return nil
	}
	if isMarkdownTableBlock(block) {
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
	if isMarkdownListItem(text) {
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
	parts := strings.Split(md, "\n\n")
	blocks := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			blocks = append(blocks, part)
		}
	}
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
