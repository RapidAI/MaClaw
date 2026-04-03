package swarm

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	gopdf "github.com/VantageDataChat/GoPDF2"
)

func TestMarkdownToHTML_Headings(t *testing.T) {
	md := "# 大标题\n## 二级标题\n### 三级标题"
	html := markdownToHTML(md)
	if !strings.Contains(html, "<b>大标题</b>") {
		t.Error("should contain h1 text")
	}
	if !strings.Contains(html, "15pt") {
		t.Error("h1 should use 15pt font")
	}
	if !strings.Contains(html, "13pt") {
		t.Error("h2 should use 13pt font")
	}
	if !strings.Contains(html, "11pt") {
		t.Error("h3 should use 11pt font")
	}
}

func TestMarkdownToHTML_Lists(t *testing.T) {
	md := "- 第一项\n- 第二项\n* 第三项"
	html := markdownToHTML(md)
	if !strings.Contains(html, "<ul>") {
		t.Error("should contain <ul>")
	}
	if !strings.Contains(html, "<li>第一项</li>") {
		t.Error("should contain list items")
	}
	if strings.Count(html, "<li>") != 3 {
		t.Errorf("expected 3 list items, got %d", strings.Count(html, "<li>"))
	}
}

func TestMarkdownToHTML_NumberedList(t *testing.T) {
	md := "1. 步骤一\n2. 步骤二"
	html := markdownToHTML(md)
	if !strings.Contains(html, "<ol>") {
		t.Error("should contain <ol>")
	}
	if !strings.Contains(html, "<li>步骤一</li>") {
		t.Error("should parse numbered list")
	}
	if strings.Contains(html, "<ul>") {
		t.Error("numbered list should not render as <ul>")
	}
}

func TestMarkdownToHTML_ImageAndTableRendering(t *testing.T) {
	imagePath := filepath.Join(t.TempDir(), "diagram.png")
	if err := os.WriteFile(imagePath, []byte("not-a-real-image"), 0644); err != nil {
		t.Fatalf("write image fixture: %v", err)
	}

	md := strings.Join([]string{
		"![系统架构图](" + filepath.ToSlash(imagePath) + ")",
		"",
		"| 模块 | 状态 | 备注 |",
		"| --- | --- | --- |",
		"| 用户模块 | 完成 | 已联调 |",
	}, "\n")
	html := markdownToHTML(md)
	if !strings.Contains(html, "<img src=") {
		t.Error("local image should render as img tag")
	}
	if strings.Contains(html, "![系统架构图]") {
		t.Error("raw markdown image syntax should not remain")
	}
	if strings.Contains(html, "| 模块 | 状态 | 备注 |") {
		t.Error("raw markdown table syntax should not remain")
	}
	if !strings.Contains(html, "<table>") {
		t.Error("table should render as html table")
	}
	if !strings.Contains(html, "<th>模块</th>") || !strings.Contains(html, "<th>状态</th>") || !strings.Contains(html, "<th>备注</th>") {
		t.Error("table headers should render as th cells")
	}
	if !strings.Contains(html, "<td>用户模块</td>") || !strings.Contains(html, "<td>完成</td>") || !strings.Contains(html, "<td>已联调</td>") {
		t.Error("table row should render as td cells")
	}
}

func TestMarkdownToHTML_RemoteImageFallback(t *testing.T) {
	md := "![系统架构图](https://example.com/diagram.png)"
	html := markdownToHTML(md)
	if strings.Contains(html, "![系统架构图](https://example.com/diagram.png)") {
		t.Error("raw remote image markdown should not remain")
	}
	if !strings.Contains(html, "暂不支持远程图片") {
		t.Error("remote image should use readable fallback")
	}
}

func TestMarkdownToHTML_MissingImageFallback(t *testing.T) {
	md := "![系统架构图](missing/diagram.png)"
	html := markdownToHTML(md)
	if strings.Contains(html, "![系统架构图](missing/diagram.png)") {
		t.Error("raw missing image markdown should not remain")
	}
	if !strings.Contains(html, "图片未找到") {
		t.Error("missing image should use readable fallback")
	}
}

func TestMarkdownToHTML_PipeTextNotTable(t *testing.T) {
	md := "普通文本里有 | 符号，但不是表格"
	html := markdownToHTML(md)
	if !strings.Contains(html, "<p>普通文本里有 | 符号，但不是表格</p>") {
		t.Error("plain pipe text should remain a paragraph")
	}
}

func TestMarkdownToHTML_Bold(t *testing.T) {
	md := "这是**粗体**文本"
	html := markdownToHTML(md)
	if !strings.Contains(html, "<b>粗体</b>") {
		t.Error("should convert **text** to <b>text</b>")
	}
}

func TestMarkdownToHTML_Italic(t *testing.T) {
	md := "这是*斜体*文本"
	html := markdownToHTML(md)
	if !strings.Contains(html, "<i>斜体</i>") {
		t.Error("should convert *text* to <i>text</i>")
	}
}

func TestMarkdownToHTML_HorizontalRule(t *testing.T) {
	md := "上面\n---\n下面"
	html := markdownToHTML(md)
	if !strings.Contains(html, "<hr/>") {
		t.Error("should convert --- to <hr/>")
	}
}

func TestMarkdownToHTML_Paragraph(t *testing.T) {
	md := "普通段落文本"
	html := markdownToHTML(md)
	if !strings.Contains(html, "<p>普通段落文本</p>") {
		t.Error("should wrap plain text in <p>")
	}
}

func TestMarkdownToHTML_HTMLEscape(t *testing.T) {
	md := "包含 <script> 标签"
	html := markdownToHTML(md)
	if strings.Contains(html, "<script>") {
		t.Error("should escape HTML tags")
	}
	if !strings.Contains(html, "&lt;script&gt;") {
		t.Error("should contain escaped HTML")
	}
}

func TestEscapeHTML(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"hello", "hello"},
		{"a & b", "a &amp; b"},
		{"<div>", "&lt;div&gt;"},
	}
	for _, tt := range tests {
		got := escapeHTML(tt.input)
		if got != tt.want {
			t.Errorf("escapeHTML(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestInlineMD(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"**bold**", "<b>bold</b>"},
		{"*italic*", "<i>italic</i>"},
		{"normal", "normal"},
		{"**a** and *b*", "<b>a</b> and <i>b</i>"},
	}
	for _, tt := range tests {
		got := inlineMD(tt.input)
		if got != tt.want {
			t.Errorf("inlineMD(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestSanitizeFileName(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"my-project", "my-project"},
		{"/path/to/project", "project"},
		{"a b c", "a_b_c"},
		{"file<>name", "file_name"},
	}
	for _, tt := range tests {
		got := sanitizeFileName(tt.input)
		if got != tt.want {
			t.Errorf("sanitizeFileName(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestSanitizeFileName_Long(t *testing.T) {
	long := strings.Repeat("a", 50)
	got := sanitizeFileName(long)
	if len(got) > 30 {
		t.Errorf("should truncate to 30 chars, got %d", len(got))
	}
}

func TestNewSwarmDocGenerator(t *testing.T) {
	gen := NewSwarmDocGenerator()
	// 不要求一定有字体（CI 环境可能没有），只验证不 panic
	_ = gen.HasFont()
}

func TestSwarmDocGenerator_GenerateSpecDoc_NoFont(t *testing.T) {
	gen := &SwarmDocGenerator{fontRegular: "", fontBold: ""}
	_, err := gen.GenerateSpecDoc(DocTypeRequirements, "test", "content")
	if err == nil {
		t.Error("should return error when no font available")
	}
	_, err = gen.GenerateSpecDocWithOptions(DocTypeRequirements, "test", "content", GeneratePDFOptions{PaperSize: "a4"})
	if err == nil {
		t.Error("should return error when no font available")
	}
}

func TestSwarmDocGenerator_GenerateAndEncode_NoFont(t *testing.T) {
	gen := &SwarmDocGenerator{}
	_, _, err := gen.GenerateAndEncode(DocTypeDesign, "test", "content")
	if err == nil {
		t.Error("should return error when no font available")
	}
}

func TestBuildTitleHTML(t *testing.T) {
	gen := &SwarmDocGenerator{}
	html := gen.buildTitleHTML(DocTypeRequirements, "my-project")
	if !strings.Contains(html, "需求文档") {
		t.Error("requirements title should contain 需求文档")
	}
	if !strings.Contains(html, "my-project") {
		t.Error("should contain project name")
	}

	html = gen.buildTitleHTML(DocTypeDesign, "proj")
	if !strings.Contains(html, "设计文档") {
		t.Error("design title should contain 设计文档")
	}

	html = gen.buildTitleHTML(DocTypeTaskPlan, "proj")
	if !strings.Contains(html, "任务计划") {
		t.Error("task plan title should contain 任务计划")
	}

	html = gen.buildTitleHTML(DocType(""), "通用标题")
	if !strings.Contains(html, "通用标题") {
		t.Error("general title should use project name")
	}
	if strings.Contains(html, "文档") {
		t.Error("general title should not add document type text")
	}
}

func TestNormalizePaperSize(t *testing.T) {
	tests := []struct {
		input   string
		want    string
		wantErr bool
	}{
		{"", "a4", false},
		{"A4", "a4", false},
		{"b5", "b5", false},
		{"B5", "b5", false},
		{"a5", "", true},
	}
	for _, tt := range tests {
		got, err := normalizePaperSize(tt.input)
		if tt.wantErr {
			if err == nil {
				t.Fatalf("normalizePaperSize(%q) expected error", tt.input)
			}
			continue
		}
		if err != nil {
			t.Fatalf("normalizePaperSize(%q) unexpected error: %v", tt.input, err)
		}
		if got != tt.want {
			t.Fatalf("normalizePaperSize(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestResolvePDFPageLayout(t *testing.T) {
	a4, err := resolvePDFPageLayout("")
	if err != nil {
		t.Fatalf("default layout error: %v", err)
	}
	b5, err := resolvePDFPageLayout("b5")
	if err != nil {
		t.Fatalf("b5 layout error: %v", err)
	}
	if a4.pageW <= b5.pageW {
		t.Fatalf("expected A4 width > B5 width, got %.2f <= %.2f", a4.pageW, b5.pageW)
	}
	if a4.pageH <= b5.pageH {
		t.Fatalf("expected A4 height > B5 height, got %.2f <= %.2f", a4.pageH, b5.pageH)
	}
	if a4.contentW <= b5.contentW {
		t.Fatalf("expected A4 content width > B5 content width, got %.2f <= %.2f", a4.contentW, b5.contentW)
	}
}

func TestSplitOversizedMarkdownBlock(t *testing.T) {
	block := strings.Repeat("这是一段很长的文本，用来测试超大块拆分。", 80)
	parts := splitOversizedMarkdownBlock(block)
	if len(parts) < 2 {
		t.Fatalf("expected oversized block to split, got %d parts", len(parts))
	}
	for i, part := range parts {
		if strings.TrimSpace(part) == "" {
			t.Fatalf("part %d should not be empty", i)
		}
	}
}

func TestSplitMarkdownBlockForFilling(t *testing.T) {
	block := "这是第一句。这是第二句。这是第三句。"
	parts := splitMarkdownBlockForFilling(block)
	if len(parts) < 2 {
		t.Fatalf("expected block to split into smaller parts, got %d", len(parts))
	}
}

func TestSplitMarkdownBlockForFilling_TableKeepsWhole(t *testing.T) {
	block := strings.Join([]string{
		"| 模块 | 状态 |",
		"| --- | --- |",
		"| 用户模块 | 完成 |",
		"| 支付模块 | 进行中 |",
	}, "\n")
	parts := splitMarkdownBlockForFilling(block)
	if len(parts) != 1 {
		t.Fatalf("expected table block to remain whole, got %d parts", len(parts))
	}
	if parts[0] != block {
		t.Fatalf("expected table block to stay unchanged, got %q", parts[0])
	}
}

func TestSplitParagraphForFilling_CommaFallback(t *testing.T) {
	text := "需求分析，方案设计，接口联调，回归验证"
	parts := splitParagraphForFilling(text)
	if len(parts) < 2 {
		t.Fatalf("expected comma-based split, got %d", len(parts))
	}
}

func TestSplitParagraphForFilling_ListItemKeepsMarker(t *testing.T) {
	text := "1. 子任务一：准备输入内容。"
	parts := splitParagraphForFilling(text)
	if len(parts) != 1 {
		t.Fatalf("expected list item to remain whole, got %d parts", len(parts))
	}
	if parts[0] != text {
		t.Fatalf("expected list item to stay unchanged, got %q", parts[0])
	}
}

func TestChunkMarkdownForPages(t *testing.T) {
	md := strings.Repeat("## 标题\n\n- 内容一\n- 内容二\n\n", 40)
	chunks := chunkMarkdownForPages(md, 1200)
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}
	for i, chunk := range chunks {
		if strings.TrimSpace(chunk) == "" {
			t.Fatalf("chunk %d should not be empty", i)
		}
	}
}

func TestRenderPagedMarkdown_RechunksRemainingPages(t *testing.T) {
	md := strings.Repeat("## 标题\n\n- 内容一\n- 内容二\n\n", 80)
	firstChunks := chunkMarkdownForPages(md, 500)
	if len(firstChunks) < 2 {
		t.Fatalf("expected first pass to produce multiple chunks, got %d", len(firstChunks))
	}
	remaining := strings.Join(firstChunks[1:], "\n\n")
	otherChunks := chunkMarkdownForPages(remaining, 1000)
	if len(otherChunks) >= len(firstChunks) {
		t.Fatalf("expected rechunked remaining pages to use fewer chunks, got %d vs %d", len(otherChunks), len(firstChunks))
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
		t.Errorf("PDF too small: %d bytes", len(data))
	}
	// 验证 PDF 头部魔数
	if string(data[:5]) != "%PDF-" {
		t.Error("output should be a valid PDF (starts with %%PDF-)")
	}
	t.Logf("生成 PDF 大小: %d bytes", len(data))
}

func TestSwarmDocGenerator_GenerateAndEncode_Integration(t *testing.T) {
	gen := NewSwarmDocGenerator()
	if !gen.HasFont() {
		t.Skip("跳过：系统未找到中文字体")
	}

	b64, fileName, err := gen.GenerateAndEncode(DocTypeDesign, "my-project", "## 模块设计\n\n- 模块A\n- 模块B")
	if err != nil {
		t.Fatal(err)
	}
	if b64 == "" {
		t.Error("base64 data should not be empty")
	}
	if !strings.Contains(fileName, "设计文档") {
		t.Errorf("fileName should contain 设计文档, got %q", fileName)
	}
	if !strings.HasSuffix(fileName, ".pdf") {
		t.Errorf("fileName should end with .pdf, got %q", fileName)
	}
	t.Logf("文件名: %s, base64 长度: %d", fileName, len(b64))
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
