package swarm

import (
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/docgen"
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
	return docgen.GenerateToFile(spec, outputPath)
}

// ValidatePDFContent guards content handed to the PDF generator.
//
// The rules themselves live in docgen (docgen.ValidateContent) so every caller
// of that package is covered: keeping them here left the desktop companion
// export, which calls docgen.GenerateToFile directly, entirely unbounded. This
// wrapper exists so existing swarm callers keep working unchanged.
func ValidatePDFContent(content string) error {
	return docgen.ValidateContent(content)
}

func specForDocType(docType DocType, projectName, content string, opts GeneratePDFOptions) docgen.Spec {
	spec := docgen.Spec{
		ProjectName: strings.TrimSpace(projectName),
		Content:     content,
		PaperSize:   opts.PaperSize,
	}

	title := strings.TrimSpace(projectName)
	// A filesystem path is not a document title.
	if looksLikeFilePath(title) {
		title = ""
	}

	switch docType {
	case DocTypeRequirements:
		spec.FileNamePrefix = "requirements"
	case DocTypeDesign:
		spec.FileNamePrefix = "design"
	case DocTypeTaskPlan:
		spec.FileNamePrefix = "task-plan"
	default:
		spec.FileNamePrefix = "document"
	}
	if title == "" {
		title = spec.FileNamePrefix
	}

	spec.Title = title
	return spec
}

// looksLikeFilePath returns true if text looks like a filesystem path
// rather than a human-readable document title.
func looksLikeFilePath(text string) bool {
	if text == "" {
		return false
	}
	// Unix absolute paths: must have at least two path segments (/a/b)
	if strings.HasPrefix(text, "/") && strings.Count(text, "/") >= 2 {
		return true
	}
	// Windows drive paths: C:\... or D:\...
	if len(text) >= 3 && text[1] == ':' && (text[2] == '\\' || text[2] == '/') {
		return true
	}
	return false
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
