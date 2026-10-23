package agent

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/raitucarp/epub"
	"golang.org/x/text/encoding/simplifiedchinese"
)

func TestExtractOfficeTextDocxRichParts(t *testing.T) {
	t.Setenv("MACLAW_OFFICE_READ_ENGINE", "legacy")
	path := filepath.Join(t.TempDir(), "rich.docx")
	writeZip(t, path, map[string]string{
		"word/document.xml": `<?xml version="1.0" encoding="UTF-8"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" xmlns:m="http://schemas.openxmlformats.org/officeDocument/2006/math">
  <w:body>
    <w:p><w:r><w:t>正文</w:t></w:r></w:p>
    <w:p><w:ins w:author="Ann"><w:r><w:t>新增句</w:t></w:r><w:r><w:ruby><w:rt><w:r><w:t>xinzeng</w:t></w:r></w:rt><w:rubyBase><w:r><w:t></w:t></w:r></w:rubyBase></w:ruby></w:r></w:ins></w:p>
    <w:p><w:del w:author="Bob"><w:r><w:delText>删除句</w:delText></w:r></w:del></w:p>
    <w:p><m:oMath><m:f><m:num><m:r><m:t>a</m:t></m:r></m:num><m:den><m:r><m:t>b</m:t></m:r></m:den></m:f></m:oMath></w:p>
    <w:p><m:oMath><m:nary><m:naryPr><m:chr m:val="∑"/></m:naryPr><m:sub><m:r><m:t>i</m:t></m:r></m:sub><m:sup><m:r><m:t>n</m:t></m:r></m:sup><m:e><m:r><m:t>x</m:t></m:r></m:e></m:nary></m:oMath></w:p>
    <w:p><m:oMath><m:d><m:dPr><m:begChr m:val="["/><m:sepChr m:val=";"/><m:endChr m:val="]"/></m:dPr><m:e><m:r><m:t>a</m:t></m:r></m:e><m:e><m:r><m:t>b</m:t></m:r></m:e></m:d></m:oMath></w:p>
    <w:p><m:oMath><m:func><m:fName><m:r><m:t>sin</m:t></m:r></m:fName><m:e><m:r><m:t>x</m:t></m:r></m:e></m:func></m:oMath></w:p>
    <w:p><m:oMath><m:m><m:mr><m:e><m:r><m:t>a</m:t></m:r></m:e><m:e><m:r><m:t>b</m:t></m:r></m:e></m:mr><m:mr><m:e><m:r><m:t>c</m:t></m:r></m:e><m:e><m:r><m:t>d</m:t></m:r></m:e></m:mr></m:m></m:oMath></w:p>
  </w:body>
</w:document>`,
		"word/comments.xml": `<?xml version="1.0" encoding="UTF-8"?>
<w:comments xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
  <w:comment w:author="Ann"><w:p><w:r><w:t>批注内容</w:t></w:r><w:r><w:ruby><w:rt><w:r><w:t>pizhu</w:t></w:r></w:rt><w:rubyBase><w:r><w:t></w:t></w:r></w:rubyBase></w:ruby></w:r></w:p><w:p><w:r><w:t>第二段</w:t></w:r></w:p></w:comment>
</w:comments>`,
		"word/charts/chart1.xml": `<?xml version="1.0" encoding="UTF-8"?>
<c:chartSpace xmlns:c="http://schemas.openxmlformats.org/drawingml/2006/chart" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main">
  <c:chart>
    <c:title><c:tx><c:rich><a:p><a:r><a:t>销售</a:t></a:r></a:p></c:rich></c:tx></c:title>
    <c:plotArea><c:barChart><c:ser>
      <c:tx><c:v>东区</c:v></c:tx>
      <c:cat><c:strCache><c:pt idx="0"><c:v>一月</c:v></c:pt><c:pt idx="1"><c:v>二月</c:v></c:pt><c:pt idx="2"><c:v>三月</c:v></c:pt></c:strCache></c:cat>
      <c:val><c:numCache><c:pt idx="0"><c:v>12</c:v></c:pt><c:pt idx="2"><c:v>30</c:v></c:pt></c:numCache></c:val>
    </c:ser></c:barChart></c:plotArea>
  </c:chart>
</c:chartSpace>`,
		"word/charts/chart2.xml": `<?xml version="1.0" encoding="UTF-8"?>
<c:chartSpace xmlns:c="http://schemas.openxmlformats.org/drawingml/2006/chart">
  <c:chart>
    <c:plotArea><c:scatterChart><c:ser>
      <c:tx><c:v>曲线</c:v></c:tx>
      <c:xVal><c:numLit><c:pt idx="0"><c:v>1</c:v></c:pt><c:pt idx="1"><c:v>2</c:v></c:pt></c:numLit></c:xVal>
      <c:yVal><c:numLit><c:pt idx="0"><c:v>10</c:v></c:pt><c:pt idx="1"><c:v>20</c:v></c:pt></c:numLit></c:yVal>
      <c:bubbleSize><c:numLit><c:pt idx="0"><c:v>3</c:v></c:pt><c:pt idx="1"><c:v>7</c:v></c:pt></c:numLit></c:bubbleSize>
    </c:ser></c:scatterChart></c:plotArea>
  </c:chart>
</c:chartSpace>`,
		"word/diagrams/data1.xml": `<?xml version="1.0" encoding="UTF-8"?>
<dgm:dataModel xmlns:dgm="http://schemas.openxmlformats.org/drawingml/2006/diagram" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main">
  <dgm:pt><dgm:t><a:p><a:r><a:t>步骤一</a:t></a:r></a:p></dgm:t></dgm:pt>
</dgm:dataModel>`,
	})
	text, format, err := ExtractOfficeText(path)
	if err != nil || format != "docx" {
		t.Fatalf("extract format=%q err=%v text=%q", format, err, text)
	}
	for _, want := range []string{"正文", "批注内容", "第二段", "(a)/(b)", "∑_i^n(x)", "[a;b]", "sin(x)", "a, b; c, d", "删除句", "销售", "东区", "12", "| 三月 | 30 |", "曲线", "| 1 | 10（3） |", "| 2 | 20（7） |", "步骤一", "## 图表", "## SmartArt"} {
		if !strings.Contains(text, want) {
			t.Fatalf("docx text missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "pizhu") || strings.Contains(text, "xinzeng") || strings.Contains(text, "| 二月 | 30 |") {
		t.Fatalf("ruby pronunciation leaked:\n%s", text)
	}
}

func TestOOXMLSheetFormulaCommentAndRevision(t *testing.T) {
	path := filepath.Join(t.TempDir(), "book.xlsx")
	writeZip(t, path, map[string]string{
		"xl/worksheets/sheet1.xml": `<?xml version="1.0" encoding="UTF-8"?>
<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
  <sheetData><row>
    <c r="C2"><f t="shared" si="0"/><v>4</v></c>
    <c r="C1"><f t="shared" ref="C1:C2" si="0">SUM(A1:A2)</f><v>3</v></c>
    <c r="E2"><f t="array" ref="E2:E4">A2:A4</f><v>1</v></c>
  </row></sheetData>
</worksheet>`,
		"xl/comments1.xml": `<?xml version="1.0" encoding="UTF-8"?>
<comments xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
  <authors><author>Li</author></authors>
  <commentList><comment ref="B2" authorId="0"><text><r><t>单元格批注</t></r><rPh sb="0" eb="1"><t>zhu</t></rPh><phoneticPr fontId="1"/></text></comment></commentList>
</comments>`,
		"xl/sharedStrings.xml": `<?xml version="1.0" encoding="UTF-8"?>
<sst xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
  <si><t>旧值</t><rPh sb="0" eb="1"><t>jiu</t></rPh></si>
  <si><t>新值</t></si>
</sst>`,
		"xl/revisions/revisionLog1.xml": `<?xml version="1.0" encoding="UTF-8"?>
<revisions xmlns="http://schemas.microsoft.com/office/spreadsheetml/2014/revision">
  <rcc rId="1" sId="1" sqref="A1"><oc><c r="A1"><v>1</v></c></oc><nc><c r="A1"><v>2</v></c></nc></rcc>
  <rcc rId="2" sId="1" sqref="B1"><oc t="s"><v>0</v></oc><nc t="s"><v>1</v></nc></rcc>
</revisions>`,
		"xl/persons/person.xml": `<?xml version="1.0" encoding="UTF-8"?>
<personList><person displayName="Ann" id="{p1}"/></personList>`,
		"xl/threadedComments/threadedComment1.xml": `<?xml version="1.0" encoding="UTF-8"?>
<ThreadedComments><threadedComment ref="C3" personId="{p1}"><text>线程批注</text></threadedComment></ThreadedComments>`,
	})
	text := extractOOXMLRichText(path, "xlsx", "")
	for _, want := range []string{"C1: =SUM(A1:A2)", "C2: =SUM(A1:A2)", "E2:E4: =A2:A4", "B2 Li: 单元格批注", "A1: 1 → 2", "B1: 旧值 → 新值", "C3 Ann: 线程批注"} {
		if !strings.Contains(text, want) {
			t.Fatalf("xlsx rich missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "zhu") || strings.Contains(text, "jiu") {
		t.Fatalf("phonetic run leaked:\n%s", text)
	}
}

func TestExtractDXFText(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pump.dxf")
	body := strings.Join([]string{
		"0", "SECTION", "2", "HEADER", "9", "$ACADVER", "1", "AC1015", "0", "ENDSEC",
		"0", "SECTION", "2", "ENTITIES",
		"0", "TEXT", "8", "0", "10", "0.0", "20", "10.0", "40", "2.5", "1", "PumpLabel",
		"0", "TEXT", "8", "0", "10", "10.0", "20", "0.0", "40", "2.5", "1", "DN50",
		"0", "TEXT", "8", "0", "10", "20.0", "20", "0.0", "40", "2.5", "1", `C:\Projects`,
		"0", "TEXT", "8", "0", "10", "30.0", "20", "0.0", "40", "2.5", "1", "%%C50",
		"0", "TEXT", "8", "0", "10", "40.0", "20", "0.0", "40", "2.5", "1", "90%%D",
		"0", "TEXT", "8", "0", "10", "50.0", "20", "0.0", "40", "2.5", "1", "%%P0.1",
		"0", "ENDSEC", "0", "EOF", "",
	}, "\n")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	text, format, err := ExtractOfficeText(path)
	if err != nil || format != "dxf" {
		t.Fatalf("dxf format=%q err=%v text=%q", format, err, text)
	}
	for _, want := range []string{"## Page ", "PumpLabel", "DN50", `C:\Projects`, "φ50", "90°", "±0.1"} {
		if !strings.Contains(text, want) {
			t.Fatalf("dxf text missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "%%") {
		t.Fatalf("cad percent code leaked:\n%s", text)
	}
}

func TestExtractDWGGarbageDoesNotPanic(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.dwg")
	if err := os.WriteFile(path, []byte("not a drawing"), 0o644); err != nil {
		t.Fatal(err)
	}
	text, _, err := ExtractOfficeText(path)
	if err == nil || strings.TrimSpace(text) != "" {
		t.Fatalf("garbage dwg text=%q err=%v", text, err)
	}
}

func TestExtractGLBText(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pump.glb")
	if err := os.WriteFile(path, minimalGLB(), 0o644); err != nil {
		t.Fatal(err)
	}
	text, format, err := ExtractOfficeText(path)
	if err != nil || format != "glb" {
		t.Fatalf("glb format=%q err=%v text=%q", format, err, text)
	}
	for _, want := range []string{"Pump", "Main", "Body", "SN-1", "glTF 2.0"} {
		if !strings.Contains(text, want) {
			t.Fatalf("glb text missing %q:\n%s", want, text)
		}
	}
}

func TestExtractEPUBText(t *testing.T) {
	w := epub.New("urn:test:pump")
	w.Title("泵手册")
	w.Author("Ada")
	w.Languages("zh")
	w.AddContent("c1.xhtml", []byte(`<?xml version="1.0" encoding="utf-8"?>
<html xmlns="http://www.w3.org/1999/xhtml"><body><h1>第一章</h1><p>叶轮间隙为 0.2 mm。</p></body></html>`))
	if err := w.TableOfContents("toc", epub.TOC{
		Items: []epub.TOC{{Title: "第一章", Href: "c1.xhtml"}},
	}); err != nil {
		t.Fatal(err)
	}
	data, err := w.WriteBytes()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "manual.epub")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	text, format, err := ExtractOfficeText(path)
	if err != nil || format != "epub" {
		t.Fatalf("epub format=%q err=%v text=%q", format, err, text)
	}
	for _, want := range []string{"泵手册", "Ada", "叶轮间隙", "## Page "} {
		if !strings.Contains(text, want) {
			t.Fatalf("epub text missing %q:\n%s", want, text)
		}
	}
}

func TestExtractAsciiDocText(t *testing.T) {
	if asciiDocColsCount(`[cols="2"]`) != 2 || asciiDocColsCount(`[cols="2,2"]`) != 2 || asciiDocColsCount(`[cols="2*"]`) != 2 || asciiDocColsCount(`[cols="2*,3"]`) != 3 {
		t.Fatalf("cols count: 2=%d 2,2=%d 2*=%d 2*,3=%d", asciiDocColsCount(`[cols="2"]`), asciiDocColsCount(`[cols="2,2"]`), asciiDocColsCount(`[cols="2*"]`), asciiDocColsCount(`[cols="2*,3"]`))
	}
	path := filepath.Join(t.TempDir(), "manual.adoc")
	body := "= 手册\n\n== 章节\n\n一段说明。\n\n调用 std::vector 即可。\n\n口径:: DN50\n\n[NOTE]\n请核对间隙。\n\n见 image:pump-inline.png[叶轮]。\n\n第一行 +\n+\n第二行\n\n.参数表\n[cols=\"2,2\"]\n|===\n| 名称 | 数值\n| 泵 | | 12\n| 代号 | DN\\|50\n| 路径 | C:\\Projects |\n| a|叶轮 | 2+|跨列\nh|表头\n|===\n\n* 第一项\n** 嵌套项\n.. 子步骤\n. 第一步\n|===\n|项\n|泵\n说明续行\n|===\n\n// 隐藏注释\n\n[source,go]\n----\nfmt.Println(\"hi\")\n----\n\nimage::pump.png[泵图]\n\n.孤立标题\n|===\n|===\n----\nkeep-line\n----\n\n[cols=\"2\"]\n|===\n|口径\n|材质\n|DN50\n|钢\n|===\n\n|===\n|叶轮 | 密封\n|0.2\n|mm\n|===\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	text, format, err := ExtractOfficeText(path)
	if err != nil || format != "adoc" {
		t.Fatalf("adoc format=%q err=%v text=%q", format, err, text)
	}
	for _, want := range []string{"# 手册", "## 章节", "一段说明", "std::vector", "口径: DN50", "注意", "请核对间隙", "pump-inline.png", "叶轮", "第一行\n第二行", "参数表", "名称", "| 泵 |  | 12 |", `DN\|50`, `C:\Projects`, "跨列", "表头", "第一项", "  - 嵌套项", ".. 子步骤", ". 第一步", "泵 说明续行", "fmt.Println", "pump.png", "泵图", "孤立标题", "keep-line", "| 口径 | 材质 |", "| DN50 | 钢 |", "| 叶轮 | 密封 |", "| 0.2 | mm |"} {
		if !strings.Contains(text, want) {
			t.Fatalf("adoc text missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "隐藏注释") || strings.Contains(text, "cols=") || strings.Contains(text, "第一行 +") || strings.Contains(text, "[NOTE]") || strings.Contains(text, "image:pump-inline.png") || strings.Contains(text, "a|") || strings.Contains(text, "2+|") || strings.Contains(text, "h|") || strings.Contains(text, "| a |") || strings.Contains(text, "| 2+ |") || strings.Contains(text, "| h |") || strings.Contains(text, "口径::") || strings.Contains(text, "**") || strings.Contains(text, "\n第一步\n") || strings.HasPrefix(text, "第一步\n") {
		t.Fatalf("syntax leaked:\n%s", text)
	}
}

func TestExtractEMLText(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ask.eml")
	raw := fmt.Sprintf("From: Ada <ada@example.com>\r\nTo: Bob <bob@example.com>\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: multipart/mixed; boundary=BOUND\r\n\r\n--BOUND\r\nContent-Type: multipart/alternative; boundary=ALT\r\n\r\n--ALT\r\nContent-Type: text/plain; charset=utf-8\r\n\r\nshort-plain\r\n\r\n--ALT\r\nContent-Type: text/html; charset=utf-8; name=\"body.html\"\r\nContent-Disposition: inline; filename=\"body.html\"\r\nContent-Transfer-Encoding: base64\r\n\r\n%s\r\n--ALT--\r\n--BOUND\r\nContent-Type: application/pdf; name=\"pump.pdf\"\r\nContent-Disposition: attachment; filename=\"pump.pdf\"\r\nContent-Transfer-Encoding: base64\r\n\r\nJVBERg==\r\n\r\n--BOUND\r\nContent-Type: image/png\r\nContent-ID: <pump@example>\r\nContent-Transfer-Encoding: base64\r\n\r\niVBORw0KGgo=\r\n\r\n--BOUND\r\nContent-Type: application/pdf\r\nContent-Disposition: attachment; filename*=utf-8''%%E6%%B3%%B5%%E5%%9B%%BE.pdf\r\n\r\nJVBERg==\r\n\r\n--BOUND--\r\n",
		mime.BEncoding.Encode("utf-8", "请审阅"),
		foldBase64([]byte(`<p>完整正文<b>泵</b> <ruby>叶轮<rt>yelun</rt></ruby> <img alt="示意图"> <a href="https://pump.example/a">说明</a></p><p>
  旁注
</p><pre>
    泵
        DN50</pre><table><tr><td><p>口径</p></td><td><p>DN50</p></td></tr><tr><td><p>材质</p></td><td><p>钢</p></td></tr></table><dl><dt>间隙</dt><dd>0.2 mm</dd></dl>`)))
	if err := os.WriteFile(path, []byte("\uFEFF"+raw), 0o644); err != nil {
		t.Fatal(err)
	}
	text, format, err := ExtractOfficeText(path)
	if err != nil || format != "eml" {
		t.Fatalf("eml format=%q err=%v text=%q", format, err, text)
	}
	for _, want := range []string{"请审阅", "完整正文泵", "示意图", "https://pump.example/a", "旁注", "叶轮", "    泵\n        DN50", "口径 DN50\n材质 钢", "间隙\n0.2 mm", "pump.pdf", "pump@example", "泵图.pdf", "Ada"} {
		if !strings.Contains(text, want) {
			t.Fatalf("eml text missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "short-plain") || strings.Contains(text, "<p>") || strings.Contains(text, "body.html") || strings.Contains(text, "iVBORw0KGgo") || strings.Contains(text, "  旁注") || strings.Contains(text, "yelun") {
		t.Fatalf("eml kept the wrong representation:\n%s", text)
	}
	linked := htmlToPlain([]byte("<p>前文</p><a href=\"https://pump.example/b\"><pre>\n    叶轮\n</pre></a>"))
	if !strings.Contains(linked, "https://pump.example/b") || !strings.Contains(linked, "    叶轮") {
		t.Fatalf("link around pre lost text: %q", linked)
	}
}

func TestExtractEMLTextUsesMetaCharset(t *testing.T) {
	if got := emlPartCharset("utf-8", "text/html", []byte(`<meta charset="gb2312">中文`)); got != "utf-8" {
		t.Fatalf("header charset lost: %q", got)
	}
	if got := emlPartCharset("", "text/plain", []byte(`<meta charset="gb2312">`)); got != "" {
		t.Fatalf("plain part sniffed a charset: %q", got)
	}
	if got := sniffHTMLCharset([]byte("\xEF\xBB\xBF<meta charset = \"gb2312\">")); got != "utf-8" {
		t.Fatalf("bom lost: %q", got)
	}
	if got := sniffHTMLCharset([]byte(`<div charset = "gb2312">`)); got != "" {
		t.Fatalf("charset outside meta: %q", got)
	}
	if got := sniffHTMLCharset([]byte(`<meta name="charsets" content="gb2312">`)); got != "" {
		t.Fatalf("charsets attribute matched: %q", got)
	}
	if got := sniffHTMLCharset([]byte(`<?xml version="1.0" encoding="gb2312"?><meta charset="utf-8">`)); got != "utf-8" {
		t.Fatalf("meta charset lost to xml declaration: %q", got)
	}
	if got := sniffHTMLCharset([]byte(`<p>encoding="gb2312"</p>`)); got != "" {
		t.Fatalf("body encoding matched: %q", got)
	}
	for i, htmlBody := range []string{
		`<meta charset="gb2312"><p>中文正文</p>`,
		`<meta charset = "gb2312"><p>中文正文</p>`,
		`<meta http-equiv="Content-Type" content="text/html; charset = gb2312"><p>中文正文</p>`,
		`<?xml version="1.0" encoding = "gb2312"?><p>中文正文</p>`,
	} {
		htmlBytes, err := simplifiedchinese.GBK.NewEncoder().Bytes([]byte(htmlBody))
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(t.TempDir(), fmt.Sprintf("cn-%d.eml", i))
		raw := "From: Ada <ada@example.com>\r\nSubject: note\r\nMIME-Version: 1.0\r\nContent-Type: text/html\r\nContent-Transfer-Encoding: base64\r\n\r\n" + foldBase64(htmlBytes) + "\r\n"
		if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
			t.Fatal(err)
		}
		text, format, err := ExtractOfficeText(path)
		if err != nil || format != "eml" {
			t.Fatalf("eml %d format=%q err=%v text=%q", i, format, err, text)
		}
		if !strings.Contains(text, "中文正文") {
			t.Fatalf("meta charset was ignored for %q:\n%s", htmlBody, text)
		}
		if strings.Contains(text, "<p>") || strings.Contains(text, "<meta") {
			t.Fatalf("html leaked for %q:\n%s", htmlBody, text)
		}
	}
}

func TestEpubTextResourceNavIsAToken(t *testing.T) {
	nav := epub.PublicationResource{MIMEType: "application/xhtml+xml", Href: "nav.xhtml", Properties: "scripted nav"}
	if epubTextResource(nav) {
		t.Fatal("nav chapter was kept")
	}
	chapter := epub.PublicationResource{MIMEType: "application/xhtml+xml", Href: "c1.xhtml", Properties: "scripted"}
	if !epubTextResource(chapter) {
		t.Fatal("chapter was skipped")
	}
}

func TestFilterNewRichLinesDoesNotSwallowShortText(t *testing.T) {
	kept := filterNewRichLines("合计12月", []string{"12"})
	if len(kept) != 1 || kept[0] != "12" {
		t.Fatalf("short comment swallowed: %#v", kept)
	}
	if got := filterNewRichLines("批注内容", []string{"Ann: 批注内容"}); len(got) != 0 {
		t.Fatalf("comment already in the body was repeated: %#v", got)
	}
}

func TestOOXMLPptxModernCommentAuthor(t *testing.T) {
	path := filepath.Join(t.TempDir(), "deck.pptx")
	writeZip(t, path, map[string]string{
		"ppt/authors.xml": `<?xml version="1.0" encoding="UTF-8"?>
<authors><author id="{a1}" name="Ann"/></authors>`,
		"ppt/comments/modernComment1.xml": `<?xml version="1.0" encoding="UTF-8"?>
<cmLst><cm authorId="{a1}"><txBody><a:p xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"><a:r><a:t>幻灯片批注</a:t></a:r></a:p></txBody></cm></cmLst>`,
	})
	text := extractOOXMLRichText(path, "pptx", "")
	if !strings.Contains(text, "Ann: 幻灯片批注") {
		t.Fatalf("pptx comment missing author:\n%s", text)
	}
}

func foldBase64(raw []byte) string {
	encoded := strings.TrimRight(base64.StdEncoding.EncodeToString(raw), "=")
	if len(encoded) < 4 {
		return encoded
	}
	mid := len(encoded) / 2
	return encoded[:mid] + "\r\n" + encoded[mid:]
}

func writeZip(t *testing.T, path string, files map[string]string) {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func minimalGLB() []byte {
	jsonChunk := []byte(`{"asset":{"version":"2.0","generator":"test"},"scenes":[{"name":"Main","nodes":[0]}],"nodes":[{"name":"Pump","mesh":0,"extras":{"serial":"SN-1"}}],"meshes":[{"name":"Body","primitives":[{"material":0}]}],"materials":[{"name":"Steel"}]}`)
	for len(jsonChunk)%4 != 0 {
		jsonChunk = append(jsonChunk, 0)
	}
	total := 12 + 8 + len(jsonChunk)
	out := make([]byte, total)
	copy(out[0:4], "glTF")
	binary.LittleEndian.PutUint32(out[4:8], 2)
	binary.LittleEndian.PutUint32(out[8:12], uint32(total))
	binary.LittleEndian.PutUint32(out[12:16], uint32(len(jsonChunk)))
	binary.LittleEndian.PutUint32(out[16:20], 0x4E4F534A)
	copy(out[20:], jsonChunk)
	return out
}
