package swarm

import (
	"encoding/base64"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	gopdf "github.com/VantageDataChat/GoPDF2"
)

// SwarmDocGenerator 生成移动端友好的 PDF 文档，用于通过 IM 发送给用户审阅。
// 排版针对手机/平板屏幕优化：大字号、宽行距、清晰的层级结构。
type SwarmDocGenerator struct {
	fontRegular string // 常规字体路径
	fontBold    string // 粗体字体路径
}

type GeneratePDFOptions struct {
	PaperSize string
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

// NewSwarmDocGenerator 创建文档生成器，自动检测系统中文字体。
func NewSwarmDocGenerator() *SwarmDocGenerator {
	regular, bold := detectSystemFonts()
	return &SwarmDocGenerator{
		fontRegular: regular,
		fontBold:    bold,
	}
}

// detectSystemFonts 检测系统中可用的中文字体。
func detectSystemFonts() (regular, bold string) {
	candidates := fontCandidates()
	for _, c := range candidates {
		if _, err := os.Stat(c.regular); err == nil {
			regular = c.regular
			bold = c.bold
			if bold == "" || func() bool { _, e := os.Stat(bold); return e != nil }() {
				bold = regular // 没有粗体就用常规体
			}
			return
		}
	}
	return "", ""
}

type fontCandidate struct {
	regular string
	bold    string
}

// fontCandidates 返回各平台的中文字体候选列表。
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
	default: // linux
		return []fontCandidate{
			{"/usr/share/fonts/truetype/noto/NotoSansCJK-Regular.ttc", "/usr/share/fonts/truetype/noto/NotoSansCJK-Bold.ttc"},
			{"/usr/share/fonts/opentype/noto/NotoSansCJK-Regular.ttc", "/usr/share/fonts/opentype/noto/NotoSansCJK-Bold.ttc"},
			{"/usr/share/fonts/truetype/droid/DroidSansFallbackFull.ttf", "/usr/share/fonts/truetype/droid/DroidSansFallbackFull.ttf"},
		}
	}
}

// HasFont 返回是否找到了可用的中文字体。
func (g *SwarmDocGenerator) HasFont() bool {
	return g.fontRegular != ""
}

// GenerateSpecDoc 将 Markdown 内容生成为移动端友好的 PDF。
// 返回 PDF 字节数据。
func (g *SwarmDocGenerator) GenerateSpecDoc(docType DocType, projectName, content string) ([]byte, error) {
	return g.GenerateSpecDocWithOptions(docType, projectName, content, GeneratePDFOptions{})
}

func (g *SwarmDocGenerator) GenerateSpecDocWithOptions(docType DocType, projectName, content string, opts GeneratePDFOptions) ([]byte, error) {
	if !g.HasFont() {
		return nil, fmt.Errorf("未找到可用的中文字体，无法生成 PDF")
	}

	layout, err := resolvePDFPageLayout(opts.PaperSize)
	if err != nil {
		return nil, err
	}

	pdf := gopdf.GoPdf{}
	pdf.Start(gopdf.Config{PageSize: *layout.pageSize})

	// 加载字体
	if err := pdf.AddTTFFont("regular", g.fontRegular); err != nil {
		return nil, fmt.Errorf("加载常规字体失败: %w", err)
	}
	if err := pdf.AddTTFFontWithOption("bold", g.fontBold, gopdf.TtfOption{Style: gopdf.Bold}); err != nil {
		// 粗体加载失败不致命，回退到常规体
		log.Printf("[SwarmDocGen] 粗体字体加载失败，使用常规体: %v", err)
		_ = pdf.AddTTFFont("bold", g.fontRegular)
	}

	pdf.AddPage()

	// 渲染封面标题区
	titleHTML := g.buildTitleHTML(docType, projectName)
	endY, err := pdf.InsertHTMLBox(layout.marginX, layout.marginY, layout.contentW, 120, titleHTML, gopdf.HTMLBoxOption{
		DefaultFontFamily: "regular",
		DefaultFontSize:   11,
		BoldFontFamily:    "bold",
		LineSpacing:       2,
	})
	if err != nil {
		return nil, fmt.Errorf("渲染标题失败: %w", err)
	}

	// 分隔线
	endY += 8
	pdf.SetLineWidth(0.5)
	pdf.SetStrokeColor(200, 200, 200)
	pdf.Line(layout.marginX, endY, layout.marginX+layout.contentW, endY)
	endY += 12

	// 将 Markdown 分页渲染正文
	if err := g.renderPagedMarkdown(&pdf, layout, endY, content); err != nil {
		return nil, fmt.Errorf("渲染正文失败: %w", err)
	}

	data, err := pdf.GetBytesPdfReturnErr()
	if err != nil {
		return nil, fmt.Errorf("生成 PDF 字节失败: %w", err)
	}
	return data, nil
}

// buildTitleHTML 构建封面标题区的 HTML。
func (g *SwarmDocGenerator) buildTitleHTML(docType DocType, projectName string) string {
	var title, subtitle, projectLine string
	switch docType {
	case DocTypeRequirements:
		title = "📋 需求文档"
		subtitle = "Requirements Specification"
		projectLine = projectName
	case DocTypeDesign:
		title = "🏗️ 设计文档"
		subtitle = "Design Document"
		projectLine = projectName
	case DocTypeTaskPlan:
		title = "📝 任务计划"
		subtitle = "Task Plan"
		projectLine = projectName
	default:
		title = projectName
		subtitle = ""
		projectLine = ""
	}

	var sb strings.Builder
	sb.WriteString("<center>")
	sb.WriteString(fmt.Sprintf(`<p style="font-size:18pt; color:#1a1a2e"><b>%s</b></p>`, title))
	if subtitle != "" {
		sb.WriteString(fmt.Sprintf(`<p style="font-size:9pt; color:#888">%s</p>`, subtitle))
	}
	if projectLine != "" {
		sb.WriteString(fmt.Sprintf(`<p style="font-size:11pt; color:#16213e">%s</p>`, projectLine))
	}
	sb.WriteString("</center>")
	return sb.String()
}

// addFooter 在当前页底部添加页脚分隔线。
func (g *SwarmDocGenerator) addFooter(pdf *gopdf.GoPdf, layout pdfPageLayout) {
	pdf.SetLineWidth(0.3)
	pdf.SetStrokeColor(220, 220, 220)
	pdf.Line(layout.marginX, layout.footerY, layout.marginX+layout.contentW, layout.footerY)
}

// markdownToHTML 将简单的 Markdown 文本转换为 HTML。
// 支持标题(##)、列表(- *)、粗体(**)、分隔线(---)、本地图片、Markdown 表格等常见语法。
// 针对移动端阅读优化了字号和间距。
func markdownToHTML(md string) string {
	lines := strings.Split(md, "\n")
	var sb strings.Builder
	listType := ""
	closeList := func() {
		switch listType {
		case "ul":
			sb.WriteString("</ul>")
		case "ol":
			sb.WriteString("</ol>")
		}
		listType = ""
	}

	for i := 0; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])

		// 空行
		if trimmed == "" {
			if listType != "" {
				closeList()
			}
			sb.WriteString("<br/>")
			continue
		}

		// 分隔线
		if regexp.MustCompile(`^-{3,}$`).MatchString(trimmed) || regexp.MustCompile(`^\*{3,}$`).MatchString(trimmed) {
			if listType != "" {
				closeList()
			}
			sb.WriteString("<hr/>")
			continue
		}

		// 标题
		if strings.HasPrefix(trimmed, "## ") {
			if listType != "" {
				closeList()
			}
			text := strings.TrimPrefix(trimmed, "## ")
			sb.WriteString(fmt.Sprintf(`<p style="font-size:13pt; color:#1a1a2e"><b>%s</b></p>`, escapeHTML(inlineMD(text))))
			continue
		}
		if strings.HasPrefix(trimmed, "### ") {
			if listType != "" {
				closeList()
			}
			text := strings.TrimPrefix(trimmed, "### ")
			sb.WriteString(fmt.Sprintf(`<p style="font-size:11pt; color:#2c3e50"><b>%s</b></p>`, escapeHTML(inlineMD(text))))
			continue
		}
		if strings.HasPrefix(trimmed, "# ") {
			if listType != "" {
				closeList()
			}
			text := strings.TrimPrefix(trimmed, "# ")
			sb.WriteString(fmt.Sprintf(`<p style="font-size:15pt; color:#0f3460"><b>%s</b></p>`, escapeHTML(inlineMD(text))))
			continue
		}

		if listType != "" {
			closeList()
		}

		if imageHTML, ok := markdownImageToHTML(trimmed); ok {
			sb.WriteString(imageHTML)
			continue
		}

		if tableHTML, nextIdx, ok := markdownTableToHTML(lines, i); ok {
			sb.WriteString(tableHTML)
			i = nextIdx - 1
			continue
		}

		// 列表项
		if strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") {
			if listType != "ul" {
				sb.WriteString("<ul>")
				listType = "ul"
			}
			text := strings.TrimPrefix(strings.TrimPrefix(trimmed, "- "), "* ")
			sb.WriteString(fmt.Sprintf("<li>%s</li>", inlineMD(escapeHTML(text))))
			continue
		}
		if regexp.MustCompile(`^\d+\.\s`).MatchString(trimmed) {
			if listType != "ol" {
				sb.WriteString("<ol>")
				listType = "ol"
			}
			text := trimmed
			idx := strings.Index(text, ". ")
			if idx > 0 {
				text = text[idx+2:]
			}
			sb.WriteString(fmt.Sprintf("<li>%s</li>", inlineMD(escapeHTML(text))))
			continue
		}

		// 普通段落
		sb.WriteString(fmt.Sprintf("<p>%s</p>", inlineMD(escapeHTML(trimmed))))
	}

	if listType != "" {
		closeList()
	}
	return sb.String()
}

func markdownImageToHTML(line string) (string, bool) {
	matches := regexp.MustCompile(`^!\[(.*?)\]\((.*?)\)$`).FindStringSubmatch(strings.TrimSpace(line))
	if len(matches) != 3 {
		return "", false
	}

	alt := strings.TrimSpace(matches[1])
	rawPath := strings.TrimSpace(matches[2])
	imagePath, ok := resolveMarkdownImagePath(rawPath)
	if ok {
		caption := ""
		if alt != "" {
			caption = fmt.Sprintf(`<p style="font-size:9pt; color:#666"><i>%s</i></p>`, inlineMD(escapeHTML(alt)))
		}
		return fmt.Sprintf(`<p><img src="%s" width="480"/></p>%s`, escapeHTMLAttr(imagePath), caption), true
	}

	if isRemoteURL(rawPath) {
		return fmt.Sprintf(`<p><b>图片：</b>%s</p><p style="font-size:9pt; color:#666"><i>暂不支持远程图片：%s</i></p>`, inlineMD(escapeHTML(fallbackText(alt, "未命名图片"))), inlineMD(escapeHTML(rawPath))), true
	}
	return fmt.Sprintf(`<p><b>图片：</b>%s</p><p style="font-size:9pt; color:#666"><i>图片未找到：%s</i></p>`, inlineMD(escapeHTML(fallbackText(alt, "未命名图片"))), inlineMD(escapeHTML(rawPath))), true
}

func resolveMarkdownImagePath(rawPath string) (string, bool) {
	path := strings.TrimSpace(rawPath)
	if path == "" || isRemoteURL(path) {
		return "", false
	}
	if strings.HasPrefix(strings.ToLower(path), "file://") {
		path = strings.TrimPrefix(path, "file://")
		path = strings.TrimPrefix(path, "/")
	}
	path = filepath.FromSlash(path)
	if filepath.IsAbs(path) {
		if _, err := os.Stat(path); err == nil {
			return path, true
		}
		return "", false
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", false
	}
	absPath := filepath.Join(cwd, path)
	if _, err := os.Stat(absPath); err == nil {
		return absPath, true
	}
	return "", false
}

func isRemoteURL(path string) bool {
	lower := strings.ToLower(strings.TrimSpace(path))
	return strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://")
}

func markdownTableToHTML(lines []string, start int) (string, int, bool) {
	if start+1 >= len(lines) {
		return "", start, false
	}
	headerLine := strings.TrimSpace(lines[start])
	separatorLine := strings.TrimSpace(lines[start+1])
	if !looksLikeMarkdownTableRow(headerLine) || !isMarkdownTableSeparator(separatorLine) {
		return "", start, false
	}

	headers := parseMarkdownTableRow(headerLine)
	if len(headers) == 0 {
		return "", start, false
	}

	var sb strings.Builder
	sb.WriteString(`<table>`)
	sb.WriteString(`<thead><tr>`)
	for _, header := range headers {
		sb.WriteString(fmt.Sprintf(`<th>%s</th>`, inlineMD(escapeHTML(header))))
	}
	sb.WriteString(`</tr></thead><tbody>`)

	next := start + 2
	rowCount := 0
	for next < len(lines) {
		rowLine := strings.TrimSpace(lines[next])
		if !looksLikeMarkdownTableRow(rowLine) {
			break
		}
		cells := parseMarkdownTableRow(rowLine)
		if len(cells) == 0 {
			break
		}
		rowCount++
		sb.WriteString(`<tr>`)
		for i := range headers {
			value := ""
			if i < len(cells) {
				value = cells[i]
			}
			sb.WriteString(fmt.Sprintf(`<td>%s</td>`, inlineMD(escapeHTML(value))))
		}
		sb.WriteString(`</tr>`)
		next++
	}
	if rowCount == 0 {
		return "", start, false
	}
	sb.WriteString(`</tbody></table>`)
	return sb.String(), next, true
}

func looksLikeMarkdownTableRow(line string) bool {
	trimmed := strings.TrimSpace(line)
	return strings.Count(trimmed, "|") >= 2
}

func isMarkdownTableSeparator(line string) bool {
	cells := parseMarkdownTableRow(line)
	if len(cells) == 0 {
		return false
	}
	for _, cell := range cells {
		cell = strings.TrimSpace(cell)
		if cell == "" {
			return false
		}
		for _, r := range cell {
			if r != '-' && r != ':' {
				return false
			}
		}
	}
	return true
}

func parseMarkdownTableRow(line string) []string {
	trimmed := strings.TrimSpace(line)
	trimmed = strings.TrimPrefix(trimmed, "|")
	trimmed = strings.TrimSuffix(trimmed, "|")
	parts := strings.Split(trimmed, "|")
	cells := make([]string, 0, len(parts))
	for _, part := range parts {
		cells = append(cells, strings.TrimSpace(part))
	}
	return cells
}

func isMarkdownTableBlock(block string) bool {
	lines := strings.Split(strings.TrimSpace(block), "\n")
	if len(lines) < 3 {
		return false
	}
	if !looksLikeMarkdownTableRow(strings.TrimSpace(lines[0])) || !isMarkdownTableSeparator(strings.TrimSpace(lines[1])) {
		return false
	}
	for _, line := range lines[2:] {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if !looksLikeMarkdownTableRow(line) {
			return false
		}
	}
	return true
}

func fallbackText(text, fallback string) string {
	if strings.TrimSpace(text) == "" {
		return fallback
	}
	return text
}


// inlineMD 处理行内 Markdown：**粗体**、*斜体*
func inlineMD(text string) string {
	// **粗体**
	boldRe := regexp.MustCompile(`\*\*(.+?)\*\*`)
	text = boldRe.ReplaceAllString(text, "<b>$1</b>")
	// *斜体*
	italicRe := regexp.MustCompile(`\*(.+?)\*`)
	text = italicRe.ReplaceAllString(text, "<i>$1</i>")
	return text
}

// escapeHTML 转义 HTML 特殊字符。
func escapeHTML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}

func escapeHTMLAttr(s string) string {
	s = escapeHTML(s)
	s = strings.ReplaceAll(s, `"`, "&quot;")
	return s
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

func (g *SwarmDocGenerator) newMeasurementPDF(layout pdfPageLayout) (*gopdf.GoPdf, error) {
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

func (g *SwarmDocGenerator) measureMarkdownHeight(layout pdfPageLayout, currentY float64, markdown string) (float64, error) {
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

func (g *SwarmDocGenerator) fitBlockPrefix(layout pdfPageLayout, currentY float64, blocks []string) (int, error) {
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

func (g *SwarmDocGenerator) fitBlockPartPrefix(layout pdfPageLayout, currentY float64, baseBlocks, parts []string) (int, error) {
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
	return regexp.MustCompile(`^\d+\.\s+`).MatchString(trimmed)
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

func splitOversizedMarkdownBlock(block string) []string {
	parts := splitLongMarkdownBlock(block, 240)
	if len(parts) == 0 {
		return nil
	}
	return parts
}

func (g *SwarmDocGenerator) renderPagedMarkdown(pdf *gopdf.GoPdf, layout pdfPageLayout, firstPageY float64, content string) error {
	blocks := splitMarkdownBlocks(content)
	if len(blocks) == 0 {
		g.addFooter(pdf, layout)
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
		g.addFooter(pdf, layout)
		blocks = remainingBlocks

		if len(blocks) == 0 {
			break
		}
		pdf.AddPage()
		currentY = layout.marginY
	}
	return nil
}

func estimatePageCharLimit(height float64) int {
	if height <= 0 {
		return 0
	}
	lines := int(height / 17.0)
	if lines < 1 {
		return 0
	}
	return lines * 28
}

func chunkMarkdownForPages(md string, maxChars int) []string {
	md = strings.TrimSpace(md)
	if md == "" {
		return nil
	}
	if maxChars <= 0 || len(md) <= maxChars {
		return []string{md}
	}

	blocks := splitMarkdownBlocks(md)
	var chunks []string
	var current strings.Builder

	flush := func() {
		text := strings.TrimSpace(current.String())
		if text != "" {
			chunks = append(chunks, text)
			current.Reset()
		}
	}

	for _, block := range blocks {
		block = strings.TrimSpace(block)
		if block == "" {
			continue
		}
		if len(block) > maxChars {
			flush()
			chunks = append(chunks, splitLongMarkdownBlock(block, maxChars)...)
			continue
		}

		candidate := block
		if current.Len() > 0 {
			candidate = current.String() + "\n\n" + block
		}
		if current.Len() > 0 && len(candidate) > maxChars {
			flush()
		}
		if current.Len() > 0 {
			current.WriteString("\n\n")
		}
		current.WriteString(block)
	}
	flush()
	return chunks
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

// GenerateAndEncode 生成 PDF 并返回 base64 编码的数据和文件名。
// 这是给 SwarmNotifier 调用的便捷方法。
func (g *SwarmDocGenerator) GenerateAndEncode(docType DocType, projectName, content string) (b64Data, fileName string, err error) {
	data, err := g.GenerateSpecDoc(docType, projectName, content)
	if err != nil {
		return "", "", err
	}

	var prefix string
	switch docType {
	case DocTypeRequirements:
		prefix = "需求文档"
	case DocTypeDesign:
		prefix = "设计文档"
	case DocTypeTaskPlan:
		prefix = "任务计划"
	default:
		prefix = "文档"
	}

	fileName = fmt.Sprintf("%s_%s_%s.pdf", prefix, sanitizeFileName(projectName), time.Now().Format("0102_1504"))
	b64Data = base64.StdEncoding.EncodeToString(data)
	return b64Data, fileName, nil
}

func (g *SwarmDocGenerator) GenerateAndEncodeWithOptions(docType DocType, projectName, content string, opts GeneratePDFOptions) (b64Data, fileName string, err error) {
	data, err := g.GenerateSpecDocWithOptions(docType, projectName, content, opts)
	if err != nil {
		return "", "", err
	}

	var prefix string
	switch docType {
	case DocTypeRequirements:
		prefix = "需求文档"
	case DocTypeDesign:
		prefix = "设计文档"
	case DocTypeTaskPlan:
		prefix = "任务计划"
	default:
		prefix = "文档"
	}

	fileName = fmt.Sprintf("%s_%s_%s.pdf", prefix, sanitizeFileName(projectName), time.Now().Format("0102_1504"))
	b64Data = base64.StdEncoding.EncodeToString(data)
	return b64Data, fileName, nil
}

// sanitizeFileName 清理文件名中的非法字符。
func sanitizeFileName(name string) string {
	// 取最后一段路径
	name = filepath.Base(name)
	// 替换非法字符
	re := regexp.MustCompile(`[<>:"/\\|?*\s]+`)
	name = re.ReplaceAllString(name, "_")
	if len(name) > 30 {
		name = name[:30]
	}
	return name
}

// GenerateToFile 生成 PDF 并写入指定路径。
// 如果 outputPath 为空，则自动生成到用户主目录。
// 返回最终写入的绝对路径。
func GenerateToFile(content, title, docTypeStr, outputPath string) (string, error) {
	return GenerateToFileWithOptions(content, title, docTypeStr, outputPath, GeneratePDFOptions{})
}

func GenerateToFileWithOptions(content, title, docTypeStr, outputPath string, opts GeneratePDFOptions) (string, error) {
	gen := NewSwarmDocGenerator()
	if !gen.HasFont() {
		return "", fmt.Errorf("未找到可用的中文字体，无法生成 PDF")
	}

	if title == "" {
		title = "文档"
	}

	var dt DocType
	switch docTypeStr {
	case "requirements":
		dt = DocTypeRequirements
	case "design":
		dt = DocTypeDesign
	case "task_plan":
		dt = DocTypeTaskPlan
	default:
		dt = ""
	}

	data, err := gen.GenerateSpecDocWithOptions(dt, title, content, opts)
	if err != nil {
		return "", fmt.Errorf("生成 PDF 失败: %w", err)
	}

	if outputPath == "" {
		home, _ := os.UserHomeDir()
		outputPath = filepath.Join(home, fmt.Sprintf("%s_%s.pdf",
			sanitizeFileName(title), time.Now().Format("20060102_150405")))
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
