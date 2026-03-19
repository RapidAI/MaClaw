package main

import (
	"strings"
	"testing"
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
	if !strings.Contains(html, "<li>步骤一</li>") {
		t.Error("should parse numbered list")
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
}

func TestNotifyDocumentForReview_Default(t *testing.T) {
	var captured []docReviewEvent
	emitter := func(name string, data ...interface{}) {
		captured = append(captured, docReviewEvent{Name: name, Data: data})
	}
	n := NewDefaultSwarmNotifierWithEmitter(emitter)
	run := &SwarmRun{ID: "test-run", Phase: PhaseRequirements}

	err := n.NotifyDocumentForReview(run, "base64data", "test.pdf", "application/pdf", "请审阅")
	if err != nil {
		t.Fatal(err)
	}
	if len(captured) != 1 {
		t.Fatalf("expected 1 event, got %d", len(captured))
	}
	if captured[0].Name != "swarm:document_review" {
		t.Errorf("event name = %q, want swarm:document_review", captured[0].Name)
	}
	payload := captured[0].Data[0].(map[string]interface{})
	if payload["file_name"] != "test.pdf" {
		t.Errorf("file_name = %v", payload["file_name"])
	}
}

type docReviewEvent struct {
	Name string
	Data []interface{}
}

// TestNotifyDocumentForReview_IMDelivery 验证设置 IM 投递回调后，
// NotifyDocumentForReview 同时通过 Wails 事件和 IM 管道发送文件。
func TestNotifyDocumentForReview_IMDelivery(t *testing.T) {
	var wailsEvents []docReviewEvent
	emitter := func(name string, data ...interface{}) {
		wailsEvents = append(wailsEvents, docReviewEvent{Name: name, Data: data})
	}
	n := NewDefaultSwarmNotifierWithEmitter(emitter)

	// 设置 IM 投递回调
	var imFileData, imFileName, imMimeType, imMessage string
	n.SetIMDelivery(
		func(b64Data, fileName, mimeType, message string) {
			imFileData = b64Data
			imFileName = fileName
			imMimeType = mimeType
			imMessage = message
		},
		func(text string) {},
	)

	run := &SwarmRun{ID: "test-run-im", Phase: PhaseDesign}
	err := n.NotifyDocumentForReview(run, "pdfbase64", "设计文档.pdf", "application/pdf", "请审阅设计")
	if err != nil {
		t.Fatal(err)
	}

	// 验证 Wails 事件仍然发送
	if len(wailsEvents) != 1 {
		t.Fatalf("expected 1 wails event, got %d", len(wailsEvents))
	}

	// 验证 IM 投递回调被调用
	if imFileData != "pdfbase64" {
		t.Errorf("IM file data = %q, want pdfbase64", imFileData)
	}
	if imFileName != "设计文档.pdf" {
		t.Errorf("IM file name = %q", imFileName)
	}
	if imMimeType != "application/pdf" {
		t.Errorf("IM mime type = %q", imMimeType)
	}
	if imMessage != "请审阅设计" {
		t.Errorf("IM message = %q", imMessage)
	}
}

// TestNotifyWaitingUser_IMDelivery 验证设置 IM 文本投递回调后，
// NotifyWaitingUser 同时通过 Wails 事件和 IM 管道发送文本。
func TestNotifyWaitingUser_IMDelivery(t *testing.T) {
	var wailsEvents []docReviewEvent
	emitter := func(name string, data ...interface{}) {
		wailsEvents = append(wailsEvents, docReviewEvent{Name: name, Data: data})
	}
	n := NewDefaultSwarmNotifierWithEmitter(emitter)

	var imText string
	n.SetIMDelivery(nil, func(text string) {
		imText = text
	})

	run := &SwarmRun{ID: "test-run-im", Phase: PhaseRequirements}
	err := n.NotifyWaitingUser(run, "请确认需求")
	if err != nil {
		t.Fatal(err)
	}

	if len(wailsEvents) != 1 {
		t.Fatalf("expected 1 wails event, got %d", len(wailsEvents))
	}
	if imText != "请确认需求" {
		t.Errorf("IM text = %q, want 请确认需求", imText)
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
