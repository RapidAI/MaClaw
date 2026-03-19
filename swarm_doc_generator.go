package main

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

// DocType 文档类型
type DocType string

const (
	DocTypeRequirements DocType = "requirements" // 需求文档
	DocTypeDesign       DocType = "design"       // 设计文档
	DocTypeTaskPlan     DocType = "task_plan"     // 任务计划
)

// GenerateSpecDoc 将 Markdown 内容生成为移动端友好的 PDF。
// 返回 PDF 字节数据。
func (g *SwarmDocGenerator) GenerateSpecDoc(docType DocType, projectName, content string) ([]byte, error) {
	if !g.HasFont() {
		return nil, fmt.Errorf("未找到可用的中文字体，无法生成 PDF")
	}

	pdf := gopdf.GoPdf{}
	// A5 尺寸更适合手机/平板阅读（148mm × 210mm）
	pageSize := gopdf.PaperSize("a5")
	if pageSize == nil {
		pageSize = gopdf.PageSizeA5
	}
	pdf.Start(gopdf.Config{PageSize: *pageSize})

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

	// 页面边距（A5: 420 × 595 pt）
	marginX := 28.0
	marginY := 30.0
	contentW := 420.0 - marginX*2 // ~364pt
	contentH := 595.0 - marginY*2 // ~535pt

	// 渲染封面标题区
	titleHTML := g.buildTitleHTML(docType, projectName)
	endY, err := pdf.InsertHTMLBox(marginX, marginY, contentW, 120, titleHTML, gopdf.HTMLBoxOption{
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
	pdf.Line(marginX, endY, marginX+contentW, endY)
	endY += 12

	// 将 Markdown 转为 HTML 并渲染正文
	bodyHTML := markdownToHTML(content)
	remainH := contentH - (endY - marginY)

	endY, err = pdf.InsertHTMLBox(marginX, endY, contentW, remainH, bodyHTML, gopdf.HTMLBoxOption{
		DefaultFontFamily: "regular",
		DefaultFontSize:   10,
		BoldFontFamily:    "bold",
		LineSpacing:       3,
		DefaultColor:      [3]uint8{40, 40, 40},
	})
	if err != nil {
		return nil, fmt.Errorf("渲染正文失败: %w", err)
	}

	// 页脚
	g.addFooter(&pdf, marginX, contentW, docType)

	data, err := pdf.GetBytesPdfReturnErr()
	if err != nil {
		return nil, fmt.Errorf("生成 PDF 字节失败: %w", err)
	}
	return data, nil
}

// buildTitleHTML 构建封面标题区的 HTML。
func (g *SwarmDocGenerator) buildTitleHTML(docType DocType, projectName string) string {
	var title, subtitle string
	switch docType {
	case DocTypeRequirements:
		title = "📋 需求文档"
		subtitle = "Requirements Specification"
	case DocTypeDesign:
		title = "🏗️ 设计文档"
		subtitle = "Design Document"
	case DocTypeTaskPlan:
		title = "📝 任务计划"
		subtitle = "Task Plan"
	default:
		title = "📄 文档"
		subtitle = "Document"
	}

	return fmt.Sprintf(`<center>
<p style="font-size:18pt; color:#1a1a2e"><b>%s</b></p>
<p style="font-size:9pt; color:#888">%s</p>
<p style="font-size:11pt; color:#16213e">%s</p>
<p style="font-size:8pt; color:#aaa">%s · MaClaw Swarm</p>
</center>`, title, subtitle, projectName, time.Now().Format("2006-01-02 15:04"))
}

// addFooter 在当前页底部添加页脚。
func (g *SwarmDocGenerator) addFooter(pdf *gopdf.GoPdf, marginX, contentW float64, docType DocType) {
	footerY := 565.0 // A5 页面底部附近
	pdf.SetLineWidth(0.3)
	pdf.SetStrokeColor(220, 220, 220)
	pdf.Line(marginX, footerY, marginX+contentW, footerY)

	hint := "请回复「确认」继续，或回复修改意见"
	if docType == DocTypeTaskPlan {
		hint = "任务将自动执行"
	}

	footerHTML := fmt.Sprintf(
		`<p style="font-size:7pt; color:#999"><center>%s</center></p>`, hint)
	_, _ = pdf.InsertHTMLBox(marginX, footerY+3, contentW, 25, footerHTML, gopdf.HTMLBoxOption{
		DefaultFontFamily: "regular",
		DefaultFontSize:   7,
	})
}

// markdownToHTML 将简单的 Markdown 文本转换为 HTML。
// 支持标题(##)、列表(- *)、粗体(**)、分隔线(---)等常见语法。
// 针对移动端阅读优化了字号和间距。
func markdownToHTML(md string) string {
	lines := strings.Split(md, "\n")
	var sb strings.Builder
	inList := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// 空行
		if trimmed == "" {
			if inList {
				sb.WriteString("</ul>")
				inList = false
			}
			sb.WriteString("<br/>")
			continue
		}

		// 分隔线
		if regexp.MustCompile(`^-{3,}$`).MatchString(trimmed) || regexp.MustCompile(`^\*{3,}$`).MatchString(trimmed) {
			if inList {
				sb.WriteString("</ul>")
				inList = false
			}
			sb.WriteString("<hr/>")
			continue
		}

		// 标题
		if strings.HasPrefix(trimmed, "## ") {
			if inList {
				sb.WriteString("</ul>")
				inList = false
			}
			text := strings.TrimPrefix(trimmed, "## ")
			sb.WriteString(fmt.Sprintf(`<p style="font-size:13pt; color:#1a1a2e"><b>%s</b></p>`, escapeHTML(inlineMD(text))))
			continue
		}
		if strings.HasPrefix(trimmed, "### ") {
			if inList {
				sb.WriteString("</ul>")
				inList = false
			}
			text := strings.TrimPrefix(trimmed, "### ")
			sb.WriteString(fmt.Sprintf(`<p style="font-size:11pt; color:#2c3e50"><b>%s</b></p>`, escapeHTML(inlineMD(text))))
			continue
		}
		if strings.HasPrefix(trimmed, "# ") {
			if inList {
				sb.WriteString("</ul>")
				inList = false
			}
			text := strings.TrimPrefix(trimmed, "# ")
			sb.WriteString(fmt.Sprintf(`<p style="font-size:15pt; color:#0f3460"><b>%s</b></p>`, escapeHTML(inlineMD(text))))
			continue
		}

		// 列表项
		if strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") || regexp.MustCompile(`^\d+\.\s`).MatchString(trimmed) {
			if !inList {
				sb.WriteString("<ul>")
				inList = true
			}
			text := trimmed
			if strings.HasPrefix(text, "- ") {
				text = strings.TrimPrefix(text, "- ")
			} else if strings.HasPrefix(text, "* ") {
				text = strings.TrimPrefix(text, "* ")
			} else {
				// 数字列表：去掉 "1. " 前缀
				idx := strings.Index(text, ". ")
				if idx > 0 {
					text = text[idx+2:]
				}
			}
			sb.WriteString(fmt.Sprintf("<li>%s</li>", inlineMD(escapeHTML(text))))
			continue
		}

		// 普通段落
		if inList {
			sb.WriteString("</ul>")
			inList = false
		}
		sb.WriteString(fmt.Sprintf("<p>%s</p>", inlineMD(escapeHTML(trimmed))))
	}

	if inList {
		sb.WriteString("</ul>")
	}
	return sb.String()
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
