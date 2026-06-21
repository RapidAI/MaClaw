package swarm

import (
	"path/filepath"
	"strings"
	"testing"

	gopdf "github.com/VantageDataChat/GoPDF2"
)

func TestNewSwarmDocGenerator(t *testing.T) {
	gen := NewSwarmDocGenerator()
	if gen == nil {
		t.Fatal("expected generator")
	}
	_ = gen.HasFont()
}

func TestSwarmDocGenerator_GenerateSpecDoc_NoFont(t *testing.T) {
	gen := &SwarmDocGenerator{}
	_, err := gen.GenerateSpecDoc(DocTypeRequirements, "test", "content")
	if err == nil {
		t.Fatal("should return error when no font available")
	}
	_, err = gen.GenerateSpecDocWithOptions(DocTypeRequirements, "test", "content", GeneratePDFOptions{PaperSize: "a4"})
	if err == nil {
		t.Fatal("should return error when no font available")
	}
}

func TestSwarmDocGenerator_GenerateAndEncode_NoFont(t *testing.T) {
	gen := &SwarmDocGenerator{}
	_, _, err := gen.GenerateAndEncode(DocTypeDesign, "test", "content")
	if err == nil {
		t.Fatal("should return error when no font available")
	}
}

func TestSpecForDocType(t *testing.T) {
	tests := []struct {
		name        string
		docType     DocType
		projectName string
		wantTitle   string
		wantPrefix  string
	}{
		{
			name:        "requirements",
			docType:     DocTypeRequirements,
			projectName: "项目A",
			wantTitle:   "项目A",
			wantPrefix:  "requirements",
		},
		{
			name:        "design",
			docType:     DocTypeDesign,
			projectName: "项目B",
			wantTitle:   "项目B",
			wantPrefix:  "design",
		},
		{
			name:        "task plan",
			docType:     DocTypeTaskPlan,
			projectName: "项目C",
			wantTitle:   "项目C",
			wantPrefix:  "task-plan",
		},
		{
			name:        "default uses project name",
			docType:     DocType(""),
			projectName: "通用标题",
			wantTitle:   "通用标题",
			wantPrefix:  "document",
		},
		{
			name:        "default falls back to document",
			docType:     DocType("unknown"),
			projectName: "   ",
			wantTitle:   "文档",
			wantPrefix:  "document",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec := specForDocType(tt.docType, tt.projectName, "content", GeneratePDFOptions{PaperSize: "b5"})
			if spec.ProjectName != strings.TrimSpace(tt.projectName) {
				t.Fatalf("ProjectName = %q", spec.ProjectName)
			}
			if spec.Content != "content" {
				t.Fatalf("Content = %q", spec.Content)
			}
			if spec.PaperSize != "b5" {
				t.Fatalf("PaperSize = %q", spec.PaperSize)
			}
			if spec.Title != tt.wantTitle {
				t.Fatalf("Title = %q, want %q", spec.Title, tt.wantTitle)
			}
			if spec.Subtitle != "" {
				t.Fatalf("Subtitle should be empty, got %q", spec.Subtitle)
			}
			if spec.FileNamePrefix != tt.wantPrefix {
				t.Fatalf("FileNamePrefix = %q, want %q", spec.FileNamePrefix, tt.wantPrefix)
			}
			if spec.FooterHint != "" {
				t.Fatalf("FooterHint should be empty, got %q", spec.FooterHint)
			}
			if spec.Brand != "" {
				t.Fatalf("Brand should be empty, got %q", spec.Brand)
			}
		})
	}
}

func TestSpecForDocType_FilePathFallsBackToTypeName(t *testing.T) {
	// When projectName is a file path, title should fall back to the doc type name
	spec := specForDocType(DocTypeDesign, "/tmp/my-project", "content", GeneratePDFOptions{})
	if spec.Title == "/tmp/my-project" {
		t.Fatal("file path should not be used as PDF title")
	}
	if spec.Title != "设计文档" {
		t.Fatalf("expected fallback to type name, got %q", spec.Title)
	}

	// Windows path
	spec = specForDocType(DocTypeRequirements, `D:\workprj\aicoder`, "content", GeneratePDFOptions{})
	if spec.Title == `D:\workprj\aicoder` {
		t.Fatal("Windows path should not be used as PDF title")
	}
	if spec.Title != "需求文档" {
		t.Fatalf("expected fallback to type name, got %q", spec.Title)
	}

	// Normal project name should still be used as title
	spec = specForDocType(DocTypeDesign, "天擎终端管理软件 竞品分析报告", "content", GeneratePDFOptions{})
	if spec.Title != "天擎终端管理软件 竞品分析报告" {
		t.Fatalf("normal project name should be used as title, got %q", spec.Title)
	}
}

func TestLooksLikeFilePath(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"/tmp/proj", true},
		{"/home/user/project", true},
		{`D:\workprj\aicoder`, true},
		{`C:\Users\admin`, true},
		{"天擎终端管理软件", false},
		{"My Project", false},
		{"test-project", false},
		{"", false},
		{"/ just starts with slash", false}, // single / at start but no second /
	}
	for _, tt := range tests {
		if got := looksLikeFilePath(tt.input); got != tt.want {
			t.Errorf("looksLikeFilePath(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestDocTypeFromString(t *testing.T) {
	tests := []struct {
		input string
		want  DocType
	}{
		{input: string(DocTypeRequirements), want: DocTypeRequirements},
		{input: string(DocTypeDesign), want: DocTypeDesign},
		{input: string(DocTypeTaskPlan), want: DocTypeTaskPlan},
		{input: " requirements ", want: DocTypeRequirements},
		{input: "unknown", want: ""},
		{input: "", want: ""},
	}
	for _, tt := range tests {
		if got := docTypeFromString(tt.input); got != tt.want {
			t.Fatalf("docTypeFromString(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestGenerateToFileWithOptions_DefaultDocTypeUsesGenericCover(t *testing.T) {
	gen := NewSwarmDocGenerator()
	if !gen.HasFont() {
		t.Skip("跳过：系统未找到中文字体")
	}

	outputPath := t.TempDir() + "/generic.pdf"
	absPath, err := GenerateToFileWithOptions("# 标题\n\n正文", "通用标题", "", outputPath, GeneratePDFOptions{PaperSize: "a4"})
	if err != nil {
		t.Fatal(err)
	}
	if absPath == "" {
		t.Fatal("expected output path")
	}
	if !strings.HasSuffix(absPath, ".pdf") {
		t.Fatalf("expected pdf output, got %q", absPath)
	}
	base := filepath.Base(absPath)
	if strings.Contains(base, "设计文档") || strings.Contains(base, "需求文档") || strings.Contains(base, "任务计划") {
		t.Fatalf("default doc_type should use generic filename, got %q", base)
	}
}

func TestSwarmDocGenerator_GenerateSpecDoc_Integration(t *testing.T) {
	gen := NewSwarmDocGenerator()
	if !gen.HasFont() {
		t.Skip("跳过：系统未找到中文字体")
	}

	content := `# 用户登录功能需求

## 功能需求

### 用户注册
- 作为新用户，我希望通过邮箱注册账号
- 验收标准：
  1. 注册成功返回 200
  2. 邮箱重复返回 409

### 用户登录
- 作为已注册用户，我希望通过邮箱密码登录
- 验收标准：
  1. 登录成功返回 JWT token
  2. 密码错误返回 401

## 非功能需求
- 响应时间 < 200ms
- 支持并发 1000 用户

---

*由 MaClaw Swarm 自动生成*`

	data, err := gen.GenerateSpecDoc(DocTypeRequirements, "test-project", content)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) < 100 {
		t.Fatalf("PDF too small: %d bytes", len(data))
	}
	if string(data[:5]) != "%PDF-" {
		t.Fatal("output should be a valid PDF")
	}
}

func TestSwarmDocGenerator_GenerateAndEncode_Integration(t *testing.T) {
	gen := NewSwarmDocGenerator()
	if !gen.HasFont() {
		t.Skip("跳过：系统未找到中文字体")
	}

	b64, fileName, err := gen.GenerateAndEncode(DocTypeDesign, "中文项目", "## 模块设计\n\n- 模块A\n- 模块B")
	if err != nil {
		t.Fatal(err)
	}
	if b64 == "" {
		t.Fatal("base64 data should not be empty")
	}
	if !strings.Contains(fileName, "design") {
		t.Fatalf("fileName should contain design, got %q", fileName)
	}
	if strings.Contains(fileName, "设计文档") {
		t.Fatalf("fileName should not contain localized display text, got %q", fileName)
	}
	if strings.ContainsAny(fileName, "中文项目") {
		t.Fatalf("fileName should not contain localized project text, got %q", fileName)
	}
	if !strings.HasSuffix(fileName, ".pdf") {
		t.Fatalf("fileName should end with .pdf, got %q", fileName)
	}
}

func TestSwarmDocGenerator_GenerateSpecDoc_LongContent_Integration(t *testing.T) {
	gen := NewSwarmDocGenerator()
	if !gen.HasFont() {
		t.Skip("跳过：系统未找到中文字体")
	}

	content := strings.Repeat("## 标题\n\n- 内容一\n- 内容二\n\n这是用于测试分页填充率的长段落。这是第二句。这是第三句。\n\n", 60)
	data, err := gen.GenerateSpecDoc(DocTypeRequirements, "long-doc", content)
	if err != nil {
		t.Fatal(err)
	}
	if string(data[:5]) != "%PDF-" {
		t.Fatal("output should be a valid PDF")
	}
	pageSizes, err := gopdf.GetSourcePDFPageSizesFromBytes(data)
	if err != nil {
		t.Fatalf("read page sizes failed: %v", err)
	}
	if len(pageSizes) < 2 {
		t.Fatalf("expected multiple pages, got %d", len(pageSizes))
	}
	if len(pageSizes) > 30 {
		t.Fatalf("expected denser pagination, got %d pages", len(pageSizes))
	}
}

func TestSwarmDocGenerator_RichMarkdown_Integration(t *testing.T) {
	gen := NewSwarmDocGenerator()
	if !gen.HasFont() {
		t.Skip("跳过：系统未找到中文字体")
	}

	// Content exercises all new rendering features: blockquote, code block, inline code
	content := `# 竞品分析报告

## 一句话定位建议

>"比火绒更全面、比深信服更实惠、比北信源更开放、比360更专注企业服务"

## 技术架构

使用 ` + "`" + `microservice` + "`" + ` 架构，通过 ` + "`" + `gRPC` + "`" + ` 通信。

### 示例代码

` + "```" + `go
func main() {
    if err := server.Start(); err != nil {
        log.Fatal(err)
    }
}
` + "```" + `

## 竞品对比

| 维度 | 产品A | 产品B |
|------|-------|-------|
| 功能 | 全面 | 精简 |
| 价格 | 中端 | 低价 |

> 注：以上数据来源于公开资料整理
> 仅供内部参考使用
`

	data, err := gen.GenerateSpecDoc(DocType(""), "竞品分析报告", content)
	if err != nil {
		t.Fatalf("PDF generation with rich markdown failed: %v", err)
	}
	if len(data) < 200 {
		t.Fatalf("PDF too small: %d bytes", len(data))
	}
	if string(data[:5]) != "%PDF-" {
		t.Fatal("output should be a valid PDF")
	}
}

func TestValidatePDFContent_RejectsOversizedContent(t *testing.T) {
	content := strings.Repeat("a", maxPDFContentBytes+1)
	if err := ValidatePDFContent(content); err == nil || !strings.Contains(err.Error(), "PDF 内容过长") {
		t.Fatalf("expected oversized content error, got %v", err)
	}
}

func TestValidatePDFContent_RejectsOversizedParagraph(t *testing.T) {
	content := strings.Repeat("段", maxPDFParagraphBytes+1)
	if err := ValidatePDFContent(content); err == nil || !strings.Contains(err.Error(), "过长段落") {
		t.Fatalf("expected oversized paragraph error, got %v", err)
	}
}

func TestValidatePDFContent_RejectsFilePayloadMarker(t *testing.T) {
	content := "# 报告\n\n[file_base64|report.pdf|application/pdf]AAAA"
	if err := ValidatePDFContent(content); err == nil || !strings.Contains(err.Error(), "文件载荷") {
		t.Fatalf("expected file payload error, got %v", err)
	}
}

func TestValidatePDFContent_RejectsSuspiciousBase64Run(t *testing.T) {
	content := "# 报告\n\n" + strings.Repeat("A", maxSuspiciousBase64RunLen)
	if err := ValidatePDFContent(content); err == nil || !strings.Contains(err.Error(), "base64") {
		t.Fatalf("expected base64 error, got %v", err)
	}
}
