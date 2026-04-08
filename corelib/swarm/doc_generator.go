package swarm

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/RapidAI/CodeClaw/corelib/docgen"
)

const (
	maxPDFContentBytes        = 180 * 1024
	maxPDFParagraphBytes      = 48 * 1024
	maxSuspiciousBase64RunLen = 8192
)

// SwarmDocGenerator 生成移动端友好的 PDF 文档，用于通过 IM 发送给用户审阅。
type SwarmDocGenerator struct {
	base *docgen.Generator
}

type GeneratePDFOptions struct {
	PaperSize string
}

// NewSwarmDocGenerator 创建文档生成器，自动检测系统中文字体。
func NewSwarmDocGenerator() *SwarmDocGenerator {
	return &SwarmDocGenerator{base: docgen.New()}
}

// HasFont 返回是否找到了可用的中文字体。
func (g *SwarmDocGenerator) HasFont() bool {
	return g != nil && g.base != nil && g.base.HasFont()
}

// GenerateSpecDoc 将 Markdown 内容生成为移动端友好的 PDF。
func (g *SwarmDocGenerator) GenerateSpecDoc(docType DocType, projectName, content string) ([]byte, error) {
	return g.GenerateSpecDocWithOptions(docType, projectName, content, GeneratePDFOptions{})
}

func (g *SwarmDocGenerator) GenerateSpecDocWithOptions(docType DocType, projectName, content string, opts GeneratePDFOptions) ([]byte, error) {
	if !g.HasFont() {
		return nil, fmt.Errorf("未找到可用的中文字体，无法生成 PDF")
	}
	if err := ValidatePDFContent(content); err != nil {
		return nil, err
	}
	return g.base.Generate(specForDocType(docType, projectName, content, opts))
}

// GenerateAndEncode 生成 PDF 并返回 base64 编码的数据和文件名。
func (g *SwarmDocGenerator) GenerateAndEncode(docType DocType, projectName, content string) (b64Data, fileName string, err error) {
	return g.GenerateAndEncodeWithOptions(docType, projectName, content, GeneratePDFOptions{})
}

func (g *SwarmDocGenerator) GenerateAndEncodeWithOptions(docType DocType, projectName, content string, opts GeneratePDFOptions) (b64Data, fileName string, err error) {
	if !g.HasFont() {
		return "", "", fmt.Errorf("未找到可用的中文字体，无法生成 PDF")
	}
	if err := ValidatePDFContent(content); err != nil {
		return "", "", err
	}
	return g.base.GenerateAndEncode(specForDocType(docType, projectName, content, opts))
}

// GenerateToFile 生成 PDF 并写入指定路径。
func GenerateToFile(content, title, docTypeStr, outputPath string) (string, error) {
	return GenerateToFileWithOptions(content, title, docTypeStr, outputPath, GeneratePDFOptions{})
}

func GenerateToFileWithOptions(content, title, docTypeStr, outputPath string, opts GeneratePDFOptions) (string, error) {
	if err := ValidatePDFContent(content); err != nil {
		return "", err
	}
	spec := specForDocType(docTypeFromString(docTypeStr), title, content, opts)
	if strings.TrimSpace(spec.ProjectName) == "" {
		spec.ProjectName = "文档"
	}
	if strings.TrimSpace(spec.Title) == "" {
		spec.Title = spec.ProjectName
	}
	return docgen.GenerateToFile(spec, outputPath)
}

func ValidatePDFContent(content string) error {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return fmt.Errorf("缺少 content 参数（Markdown 格式的文档内容）")
	}
	if len(content) > maxPDFContentBytes {
		return fmt.Errorf("PDF 内容过长（%d 字节），请缩短后再生成", len(content))
	}
	if strings.Contains(content, "[file_base64|") {
		return fmt.Errorf("PDF 内容疑似包含文件载荷，请先整理正文再生成")
	}
	if longestSuspiciousBase64Run(content) >= maxSuspiciousBase64RunLen {
		return fmt.Errorf("PDF 内容疑似包含超长 base64 或二进制载荷，请先整理正文再生成")
	}
	for _, block := range splitMarkdownParagraphs(content) {
		if len(block) > maxPDFParagraphBytes {
			return fmt.Errorf("PDF 中存在过长段落（%d 字节），请拆分后再生成", len(block))
		}
	}
	return nil
}

func splitMarkdownParagraphs(content string) []string {
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	parts := strings.Split(normalized, "\n\n")
	blocks := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			blocks = append(blocks, trimmed)
		}
	}
	return blocks
}

func longestSuspiciousBase64Run(content string) int {
	maxRun, run := 0, 0
	for _, r := range content {
		if isBase64Rune(r) {
			run++
			if run > maxRun {
				maxRun = run
			}
			continue
		}
		run = 0
	}
	return maxRun
}

func isBase64Rune(r rune) bool {
	if r >= 'A' && r <= 'Z' {
		return true
	}
	if r >= 'a' && r <= 'z' {
		return true
	}
	if r >= '0' && r <= '9' {
		return true
	}
	if r == '+' || r == '/' || r == '=' {
		return true
	}
	return !unicode.IsSpace(r) && false
}

func specForDocType(docType DocType, projectName, content string, opts GeneratePDFOptions) docgen.Spec {
	spec := docgen.Spec{
		ProjectName: strings.TrimSpace(projectName),
		Content:     content,
		Brand:       "MaClaw Swarm",
		PaperSize:   opts.PaperSize,
	}

	switch docType {
	case DocTypeRequirements:
		spec.Title = "📋 需求文档"
		spec.Subtitle = "Requirements Specification"
		spec.FileNamePrefix = "需求文档"
		spec.FooterHint = "请确认需求范围与验收标准"
	case DocTypeDesign:
		spec.Title = "🏗️ 设计文档"
		spec.Subtitle = "Design Document"
		spec.FileNamePrefix = "设计文档"
		spec.FooterHint = "请确认设计方案与实现边界"
	case DocTypeTaskPlan:
		spec.Title = "📝 任务计划"
		spec.Subtitle = "Task Plan"
		spec.FileNamePrefix = "任务计划"
		spec.FooterHint = "请确认任务拆分与执行顺序"
	default:
		spec.Title = strings.TrimSpace(projectName)
		if spec.Title == "" {
			spec.Title = "文档"
		}
		spec.Brand = ""
	}

	return spec
}

func docTypeFromString(docType string) DocType {
	switch strings.TrimSpace(docType) {
	case string(DocTypeRequirements):
		return DocTypeRequirements
	case string(DocTypeDesign):
		return DocTypeDesign
	case string(DocTypeTaskPlan):
		return DocTypeTaskPlan
	default:
		return ""
	}
}
