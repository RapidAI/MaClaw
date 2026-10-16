package tool

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/computeruse"
	uicintent "github.com/RapidAI/CodeClaw/corelib/intent"
)

// markdownFileRouteTools reproduces the 2026-09-20 production mix for
// "生成markdown": core fallback, file writers, generate_pdf (whose
// description used to mention Markdown as input), and the computer_* family
// that padded MaxToolBudget.
func markdownFileRouteTools() []map[string]interface{} {
	defs := []struct{ name, desc string }{
		{"task", "Manage background tasks"},
		{"async_wait", "Wait for async operations"},
		{"compress_context", "Compress conversation context"},
		{"bash", "Run shell commands"},
		{"read_file", "Read a file from disk"},
		{"edit_file", "Edit an existing file"},
		{"write_file", "Write content to a new or existing file"},
		{"discover_tool", "Discover deferred tools"},
		{"ripgrep", "Search file contents"},
		{"generate_pdf", "生成 PDF 文档并发送给用户。将 Markdown 内容渲染为专业排版的 PDF 文件。"},
		{"office", "Office/PDF/text document tool. action: read_document (.txt/.md) generate_pdf"},
		{"knowledge_search", "Search the local knowledge base"},
		{"web_fetch", "Fetch content from a web page URL"},
		{"memory", "Recall saved notes"},
		{"search_and_install_skill", "Search and install a Skill from the hub"},
	}
	out := make([]map[string]interface{}, 0, len(defs)+len(computeruse.ToolNames)+8)
	for _, d := range defs {
		out = append(out, makeToolDef(d.name, d.desc))
	}
	for _, name := range computeruse.ToolNames {
		out = append(out, makeToolDef(name, "desktop GUI "+name))
	}
	for i := 0; i < 8; i++ {
		out = append(out, makeToolDef("noise_pad_"+string(rune('a'+i)), "unrelated filler"))
	}
	return out
}

func TestComputerUseToolNamesStayFailClosed(t *testing.T) {
	if len(allComputerUseToolNames) == 0 {
		t.Fatal("allComputerUseToolNames empty")
	}
	if len(allComputerUseToolNames) != len(computeruse.ToolNames) {
		t.Fatalf("router computer-use list %d, computeruse.ToolNames %d", len(allComputerUseToolNames), len(computeruse.ToolNames))
	}
	got := map[string]bool{}
	for _, name := range allComputerUseToolNames {
		got[name] = true
		if !IsFailClosedConditionalTool(name) {
			t.Fatalf("%s must be fail-closed", name)
		}
	}
	for _, name := range computeruse.ToolNames {
		if !got[name] {
			t.Fatalf("router computer-use list missing %q", name)
		}
	}
}

func TestRoute_GenerateMarkdownDoesNotExposeComputerUseOrPreferPDF(t *testing.T) {
	router := NewRouter(nil)
	result := router.RouteWithOptions("生成markdown", markdownFileRouteTools(), RouteOptions{SkipUnifiedClassifier: true})
	names := routedToolNames(result)

	if !names["write_file"] {
		t.Fatalf("markdown file request must keep write_file, got %#v", names)
	}
	if names["generate_pdf"] {
		t.Fatalf("generate_pdf must not appear for 生成markdown (prompt would force a PDF call): %#v", names)
	}
	if names["office"] {
		t.Fatalf("office must not appear for 生成markdown (action=generate_pdf is the leftover PDF path): %#v", names)
	}
	if names["search_and_install_skill"] {
		t.Fatalf("skill install must not appear for 生成markdown: %#v", names)
	}
	for _, forbidden := range computeruse.ToolNames {
		if names[forbidden] {
			t.Fatalf("computer-use tool %q must stay fail-closed for 生成markdown: %#v", forbidden, names)
		}
	}
}

func TestRoute_SpreadsheetToMarkdownKeepsOffice(t *testing.T) {
	router := NewRouter(nil)
	result := router.RouteWithOptions("把这个xlsx生成markdown", markdownFileRouteTools(), RouteOptions{SkipUnifiedClassifier: true})
	names := routedToolNames(result)
	if names["generate_pdf"] {
		t.Fatalf("xlsx→markdown must not expose generate_pdf, got %#v", names)
	}
	if !names["office"] {
		t.Fatalf("xlsx→markdown must keep office to read the workbook, got %#v", names)
	}
	if !names["write_file"] {
		t.Fatalf("xlsx→markdown must keep write_file, got %#v", names)
	}
	if names["search_and_install_skill"] {
		t.Fatalf("xlsx→markdown must not install a PDF skill, got %#v", names)
	}
}

func TestRoute_GenerateMarkdownHidesGeneratePDFEvenWhenCodingPinsIt(t *testing.T) {
	router := NewRouter(nil)
	coding := uicintent.ClassificationResult{
		Primary:    uicintent.LabelCoding,
		Confidence: 0.95,
		ToolNames:  []string{"generate_pdf", "office"},
	}
	result := router.RouteWithOptions("生成markdown", markdownFileRouteTools(), RouteOptions{
		SkipUnifiedClassifier: true,
		PreResolved:           &coding,
	})
	names := routedToolNames(result)
	if names["generate_pdf"] {
		t.Fatal("coding affinity must not keep generate_pdf for a markdown-file request")
	}
	if names["office"] {
		t.Fatal("coding affinity must not keep office for a markdown-file request")
	}
	if !names["write_file"] {
		t.Fatal("markdown-file request must still keep write_file")
	}
}

func TestRoute_MarkdownToPDFStillKeepsGeneratePDF(t *testing.T) {
	router := NewRouter(nil)
	result := router.RouteWithOptions("把这些内容生成PDF", markdownFileRouteTools(), RouteOptions{SkipUnifiedClassifier: true})
	if !routedToolNames(result)["generate_pdf"] {
		t.Fatalf("explicit PDF request must keep generate_pdf, got %#v", routedToolNames(result))
	}
}

func TestRoute_HostKeepGeneratePDFOverridesMarkdownHide(t *testing.T) {
	router := NewRouter(nil)
	result := router.RouteWithOptions("生成markdown", markdownFileRouteTools(), RouteOptions{
		SkipUnifiedClassifier: true,
		HostKeepTools:         []string{"generate_pdf"},
	})
	names := routedToolNames(result)
	if !names["generate_pdf"] {
		t.Fatal("HostKeep must still admit generate_pdf")
	}
	if !names["write_file"] {
		t.Fatal("markdown-file request must still keep write_file under HostKeep")
	}
	if names["office"] {
		t.Fatal("HostKeep generate_pdf must not un-hide office")
	}
	if names["search_and_install_skill"] {
		t.Fatal("HostKeep generate_pdf must not un-hide skill install")
	}
}

func TestRoute_ComputerUseUICKeepsObserve(t *testing.T) {
	router := NewRouter(nil)
	cu := uicintent.ClassificationResult{
		Primary: uicintent.LabelComputerUse, Confidence: 0.95, ToolNames: []string{"computer_observe", "computer_click"},
	}
	result := router.RouteWithOptions("点击窗口上的确定按钮", markdownFileRouteTools(), RouteOptions{
		SkipUnifiedClassifier: true,
		PreResolved:           &cu,
	})
	names := routedToolNames(result)
	if !names["computer_observe"] || !names["computer_click"] {
		t.Fatalf("LabelComputerUse must keep computer tools, got %#v", names)
	}
}

func TestRoute_ComputerUseAffinityWithoutToolNames(t *testing.T) {
	router := NewRouter(nil)
	cu := uicintent.ClassificationResult{
		Primary: uicintent.LabelComputerUse, Confidence: 0.95,
	}
	result := router.RouteWithOptions("看看屏幕上现在显示了什么", markdownFileRouteTools(), RouteOptions{
		SkipUnifiedClassifier: true,
		PreResolved:           &cu,
	})
	if !routedToolNames(result)["computer_observe"] {
		t.Fatalf("LabelComputerUse without ToolNames must keep computer_observe via affinity, got %#v", routedToolNames(result))
	}
}

func TestQueryWantsMarkdownFile(t *testing.T) {
	for _, msg := range []string{
		"生成markdown", "保存为 markdown", "export this as a markdown file",
		"写成md文件", "create notes.md", "保存成md", "把这个xlsx生成markdown",
		"created a markdown file", "saved this as markdown",
		"不要 pdf，生成 markdown", "生成markdown，不要PDF", "生成markdown 不要 pdf",
	} {
		if !queryWantsMarkdownFile(msg) {
			t.Fatalf("want markdown-file query: %q", msg)
		}
	}
	for _, msg := range []string{
		"把 markdown 转成 pdf", "生成PDF", "北京天气，输出 格式化pdf报告",
		"今天天气怎么样", "读取markdown", "看看 markdown 里写了什么",
		"create notes.md5", "保存 hash.md5", "create a markdownish file",
	} {
		if queryWantsMarkdownFile(msg) {
			t.Fatalf("must not treat as markdown-file query: %q", msg)
		}
	}
}

func TestApplyMarkdownFileSurface(t *testing.T) {
	cond := map[string]bool{"generate_pdf": true, "office": true}
	suppressed := map[string]bool{}
	keep := applyMarkdownFileSurface("生成markdown", cond, suppressed)
	if !suppressed["generate_pdf"] || !suppressed["office"] || !suppressed["search_and_install_skill"] {
		t.Fatalf("plain markdown write must suppress PDF tools and skill install, got %#v", suppressed)
	}
	if cond["generate_pdf"] || cond["office"] {
		t.Fatalf("UIC pins must be cleared, got %#v", cond)
	}
	if len(keep) != 1 || keep[0] != "write_file" {
		t.Fatalf("keep=%v, want [write_file]", keep)
	}

	cond = map[string]bool{}
	suppressed = map[string]bool{}
	keep = applyMarkdownFileSurface("把这个xlsx生成markdown", cond, suppressed)
	if !suppressed["generate_pdf"] {
		t.Fatal("xlsx→markdown must still hide generate_pdf")
	}
	if suppressed["office"] {
		t.Fatal("xlsx→markdown must not suppress office")
	}
	if !sliceContainsName(keep, "office") || !sliceContainsName(keep, "write_file") {
		t.Fatalf("xlsx→markdown keep=%v, want write_file and office", keep)
	}
	if !suppressed["search_and_install_skill"] {
		t.Fatal("xlsx→markdown must still hide skill install")
	}
	if applyMarkdownFileSurface("生成PDF", map[string]bool{}, map[string]bool{}) != nil {
		t.Fatal("PDF request must not apply the markdown-file surface")
	}
	if applyMarkdownFileSurface("生成markdown", nil, nil) == nil {
		t.Fatal("nil maps must still return write_file keep")
	}
}

func TestQueryMentionsOfficeSource(t *testing.T) {
	if queryMentionsOfficeSource("生成markdown") {
		t.Fatal("plain markdown write has no Office source")
	}
	if queryMentionsOfficeSource("生成 excellent markdown") {
		t.Fatal("excel must not match inside excellent")
	}
	if !queryMentionsOfficeSource("把这个xlsx生成markdown") {
		t.Fatal("xlsx path must count as an Office source")
	}
	if !queryMentionsOfficeSource("用 excel 生成markdown") {
		t.Fatal("standalone excel must count as an Office source")
	}
	if !queryMentionsOfficeSource("把 csv 生成markdown") {
		t.Fatal("csv token must count as an Office source")
	}
	if queryMentionsOfficeSource("xlsish markdown") {
		t.Fatal("xls must not match inside xlsish")
	}
	if queryMentionsOfficeSource("xlsxish markdown") {
		t.Fatal("xlsx must not match inside xlsxish")
	}
	if !queryMentionsOfficeSource("把 ppt 生成markdown") {
		t.Fatal("ppt token must count as an Office source")
	}
	if queryMentionsOfficeSource("create spec.document.md") {
		t.Fatal(".doc must not match inside .document")
	}
	if !queryMentionsOfficeSource("convert report.docx to markdown") {
		t.Fatal(".docx must still count as an Office source")
	}
}

func sliceContainsName(names []string, want string) bool {
	for _, name := range names {
		if name == want {
			return true
		}
	}
	return false
}

func TestQueryWantsLocalFileDelete(t *testing.T) {
	for _, msg := range []string{
		"删除刚才的markdown文件",
		"删掉这个本地文件",
		"特别删掉这个文件",
		"删除空内容的文件",
		"删除 notes.md",
		"删掉项目里的 notes.md",
		"delete the markdown file I just created",
		"remove notes.txt",
		"rm the file",
	} {
		if !QueryWantsLocalFileDelete(msg) {
			t.Fatalf("want local file delete: %q", msg)
		}
	}
	for _, msg := range []string{
		"生成markdown",
		"把这段内容保存到 notes.txt",
		"删掉那条关于我地址的记忆",
		"删除知识库里的这个来源",
		"删除定时任务",
		"删除服务器上的日志文件",
		"不要删除这个markdown文件",
		"给这个文件加删除线",
		"已删除的文件还在吗",
		"要不要删除这个文件",
		"把内容保存到 notes.txt，写错了就删掉这个文件",
		"移除文件夹里的日志",
		"把文件里的一行删掉",
		"删除 markdown 里的表格",
		"今天天气怎么样",
		"remove the stripe artifacts",
		"remove the heading from the file",
	} {
		if QueryWantsLocalFileDelete(msg) {
			t.Fatalf("must not treat as local file delete: %q", msg)
		}
	}
}

func TestQueryWantsMarkdownFileEnglishWriterIsNotAWrite(t *testing.T) {
	if queryWantsMarkdownFile("markdown writer") {
		t.Fatal("writer must not count as the write verb")
	}
}

func TestMinCandidateRouteScoreRejectsExactZeros(t *testing.T) {
	if MinCandidateRouteScore <= 0 {
		t.Fatal("MinCandidateRouteScore must skip unmatched candidates")
	}
	if MinCandidateRouteScore >= 0.25 {
		t.Fatalf("MinCandidateRouteScore=%v drops legitimate #2 retrieval hits (weather web_search ~0.26 fused-normalized)", MinCandidateRouteScore)
	}
}

func TestMarkdownFileRouteToolsIncludeComputerFamily(t *testing.T) {
	// Guard the fixture so the fail-closed assertion is actually exercised.
	tools := markdownFileRouteTools()
	names := routedToolNames(tools)
	if !names["computer_observe"] || !names["generate_pdf"] || !names["write_file"] {
		t.Fatalf("fixture missing required tools: %#v", names)
	}
	foundPDFMarkdown := false
	for _, def := range tools {
		if ExtractToolName(def) == "generate_pdf" && strings.Contains(ExtractToolDescription(def), "Markdown") {
			foundPDFMarkdown = true
			break
		}
	}
	if !foundPDFMarkdown {
		t.Fatal("fixture generate_pdf description should mention Markdown as input")
	}
}
