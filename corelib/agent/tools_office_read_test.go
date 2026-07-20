package agent

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestToolReadDocument_Docx(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sample.docx")
	writeMinimalDOCX(t, path, "Hello MaClaw DOC reader")

	out := ToolReadDocument(map[string]interface{}{"file_path": path})
	if strings.Contains(out, "读取失败") {
		t.Fatalf("unexpected failure: %s", out)
	}
	if !strings.Contains(out, "Hello MaClaw DOC reader") {
		t.Fatalf("expected extracted text, got: %s", out)
	}
	if !strings.Contains(out, "# format: docx") {
		t.Fatalf("expected format header, got: %s", out)
	}
}

func TestToolReadDocument_PathAlias(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "alias.docx")
	writeMinimalDOCX(t, path, "via path alias")

	out := ToolReadDocument(map[string]interface{}{"path": path})
	if !strings.Contains(out, "via path alias") {
		t.Fatalf("path alias failed: %s", out)
	}
}

func TestToolReadDocument_UnsupportedSuggestsCraftTool(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "legacy.ppt")
	if err := os.WriteFile(path, []byte("not a real ppt"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := ToolReadDocument(map[string]interface{}{"path": path})
	if !strings.Contains(out, "读取失败") {
		t.Fatalf("expected failure for .ppt, got: %s", out)
	}
	if !strings.Contains(out, "craft_tool") {
		t.Fatalf("expected craft_tool recovery hint, got: %s", out)
	}
	if !strings.Contains(out, path) {
		t.Fatalf("expected path in recovery hint, got: %s", out)
	}
}

func TestToolReadDocument_InvalidDocSuggestsCraftTool(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "broken.doc")
	if err := os.WriteFile(path, []byte("not ole2"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := ToolReadDocument(map[string]interface{}{"file_path": path})
	if !strings.Contains(out, "读取失败") {
		t.Fatalf("expected structured failure, got: %s", out)
	}
	if !strings.Contains(out, "craft_tool") {
		t.Fatalf("expected craft_tool recovery, got: %s", out)
	}
}

func TestToolReadDocument_MaxCharsFloatAndInt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "long.docx")
	writeMinimalDOCX(t, path, strings.Repeat("字", 100))

	for _, args := range []map[string]interface{}{
		{"file_path": path, "max_chars": float64(10)},
		{"file_path": path, "max_chars": 10},
	} {
		out := ToolReadDocument(args)
		if !strings.Contains(out, "truncated: true") {
			t.Fatalf("expected truncation marker for %#v, got: %s", args["max_chars"], out)
		}
		if !strings.Contains(out, "next_offset:") {
			t.Fatalf("expected next_offset for paging, got: %s", out)
		}
	}
}

func TestToolReadDocument_OffsetPaging(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "page.docx")
	// 30 chars total
	writeMinimalDOCX(t, path, strings.Repeat("A", 30))
	part1 := ToolReadDocument(map[string]interface{}{
		"file_path": path,
		"max_chars": 10,
		"offset":    0,
	})
	if !strings.Contains(part1, "next_offset: 10") {
		t.Fatalf("part1 missing next_offset: %s", part1)
	}
	part2 := ToolReadDocument(map[string]interface{}{
		"file_path": path,
		"max_chars": 10,
		"offset":    10,
	})
	if !strings.Contains(part2, "# offset: 10") {
		t.Fatalf("part2 missing offset header: %s", part2)
	}
	// Reconstruct: both chunks should cover full text without inventing content.
	if !strings.Contains(part1, "AAAAAAAAAA") || !strings.Contains(part2, "AAAAAAAAAA") {
		t.Fatalf("unexpected chunk content\npart1=%s\npart2=%s", part1, part2)
	}
	// EOF paging
	end := ToolReadDocument(map[string]interface{}{
		"file_path": path,
		"offset":    30,
	})
	if !strings.Contains(end, "已到文档末尾") {
		t.Fatalf("expected EOF message, got: %s", end)
	}
}

func TestToolReadDocument_CacheHelpsOffsetPaging(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cache.docx")
	writeMinimalDOCX(t, path, strings.Repeat("Z", 50))
	// First call populates cache; second offset page should still succeed.
	_ = ToolReadDocument(map[string]interface{}{"file_path": path, "max_chars": 20, "offset": 0})
	out := ToolReadDocument(map[string]interface{}{"file_path": path, "max_chars": 20, "offset": 20})
	if strings.Contains(out, "读取失败") || strings.Contains(out, "文件不存在") {
		t.Fatalf("cached page read failed: %s", out)
	}
	if !strings.Contains(out, "# offset: 20") {
		t.Fatalf("expected offset header, got: %s", out)
	}
}

func TestToolReadDocument_MaxCharsCapped(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cap.docx")
	writeMinimalDOCX(t, path, strings.Repeat("X", 100))
	// Absurd max_chars must be clamped; still returns content.
	out := ToolReadDocument(map[string]interface{}{
		"file_path": path,
		"max_chars": 50_000_000,
	})
	if strings.Contains(out, "读取失败") {
		t.Fatalf("unexpected failure: %s", out)
	}
	if !strings.Contains(out, "XXXXXXXXXX") {
		t.Fatalf("missing content: %s", out)
	}
}

func TestToolReadDocument_LineNumbersContinueAcrossOffset(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "lines.docx")
	// Three lines
	writeMinimalDOCXMultiPara(t, path, []string{"alpha", "beta", "gamma"})
	// First chunk: enough for "alpha\nbe" roughly — use large max to get first two lines then page.
	full := ToolReadDocument(map[string]interface{}{
		"file_path":    path,
		"line_numbers": true,
	})
	if !strings.Contains(full, "L1: alpha") || !strings.Contains(full, "L2: beta") {
		t.Fatalf("expected absolute line numbers in full read: %s", full)
	}
	// Offset past "alpha\n" (6 runes) should start numbering at L2
	part := ToolReadDocument(map[string]interface{}{
		"file_path":    path,
		"offset":       6,
		"line_numbers": true,
	})
	if !strings.Contains(part, "L2:") {
		t.Fatalf("expected line numbers to continue at L2 after offset, got: %s", part)
	}
	if strings.Contains(part, "L1:") {
		t.Fatalf("should not restart at L1 after offset: %s", part)
	}
}

func writeMinimalDOCXMultiPara(t *testing.T, path string, paras []string) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(file)
	w, err := zw.Create("word/document.xml")
	if err != nil {
		t.Fatal(err)
	}
	var body strings.Builder
	body.WriteString(`<?xml version="1.0" encoding="UTF-8"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>`)
	for _, p := range paras {
		esc := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(p)
		body.WriteString(`<w:p><w:r><w:t>`)
		body.WriteString(esc)
		body.WriteString(`</w:t></w:r></w:p>`)
	}
	body.WriteString(`</w:body></w:document>`)
	_, _ = w.Write([]byte(body.String()))
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestExtractDocxText_TableCells(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "table.docx")
	// Minimal docx with a 1x2 table
	writeMinimalDOCXTable(t, path, [][]string{{"评分标准", "分值"}})
	text, err := ExtractDocxText(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "评分标准") || !strings.Contains(text, "分值") {
		t.Fatalf("table cells missing: %q", text)
	}
	if !strings.Contains(text, "\t") {
		t.Fatalf("expected tab-separated cells, got %q", text)
	}
}

func TestExtractDocxText_NestedTableKeepsOuterCells(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested.docx")
	// Outer row: [outer-left | nested-table-cell]; nested has one cell "inner"
	writeMinimalDOCXNestedTable(t, path)
	text, err := ExtractDocxText(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "outer-left") {
		t.Fatalf("outer cell lost: %q", text)
	}
	if !strings.Contains(text, "inner") {
		t.Fatalf("nested cell lost: %q", text)
	}
	// Outer structure should still produce a tab-separated row containing outer-left.
	if !strings.Contains(text, "outer-left\t") && !strings.Contains(text, "\touter-left") {
		// nested content may be appended inside the second cell; require outer-left present with a tab somewhere on its line.
		found := false
		for _, line := range strings.Split(text, "\n") {
			if strings.Contains(line, "outer-left") && strings.Contains(line, "\t") {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected outer row tab structure, got %q", text)
		}
	}
}

func writeMinimalDOCXNestedTable(t *testing.T, path string) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(file)
	w, err := zw.Create("word/document.xml")
	if err != nil {
		t.Fatal(err)
	}
	// Outer table 1x2; second cell contains nested 1x1 table.
	xmlBody := `<?xml version="1.0" encoding="UTF-8"?>` +
		`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:tbl>` +
		`<w:tr>` +
		`<w:tc><w:p><w:r><w:t>outer-left</w:t></w:r></w:p></w:tc>` +
		`<w:tc><w:tbl><w:tr><w:tc><w:p><w:r><w:t>inner</w:t></w:r></w:p></w:tc></w:tr></w:tbl></w:tc>` +
		`</w:tr>` +
		`</w:tbl></w:body></w:document>`
	_, _ = w.Write([]byte(xmlBody))
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func writeMinimalDOCXTable(t *testing.T, path string, rows [][]string) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(file)
	w, err := zw.Create("word/document.xml")
	if err != nil {
		t.Fatal(err)
	}
	var body strings.Builder
	body.WriteString(`<?xml version="1.0" encoding="UTF-8"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:tbl>`)
	for _, row := range rows {
		body.WriteString(`<w:tr>`)
		for _, cell := range row {
			esc := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(cell)
			body.WriteString(`<w:tc><w:p><w:r><w:t>`)
			body.WriteString(esc)
			body.WriteString(`</w:t></w:r></w:p></w:tc>`)
		}
		body.WriteString(`</w:tr>`)
	}
	body.WriteString(`</w:tbl></w:body></w:document>`)
	_, _ = w.Write([]byte(body.String()))
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestToolReadPPTX_LegacyPPTSuggestsCraftTool(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "deck.ppt")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := ToolReadPPTX(map[string]interface{}{"file_path": path})
	if !strings.Contains(out, "craft_tool") {
		t.Fatalf("expected craft_tool hint, got: %s", out)
	}
}

func TestToolReadExcel_MissingFile(t *testing.T) {
	out := ToolReadExcel(map[string]interface{}{"file_path": filepath.Join(t.TempDir(), "nope.xlsx")})
	if !strings.Contains(out, "文件不存在") {
		t.Fatalf("expected missing file message, got: %s", out)
	}
}

func TestExtractDocxText(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.docx")
	writeMinimalDOCX(t, path, "段落一")
	text, err := ExtractDocxText(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "段落一") {
		t.Fatalf("got %q", text)
	}
}

func TestExtractOfficeText_UnknownExt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "note.rtf")
	if err := os.WriteFile(path, []byte("{\\rtf1}"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, format, err := ExtractOfficeText(path)
	if err == nil {
		t.Fatal("expected unsupported error")
	}
	if format != "rtf" && format != "unknown" {
		// format may be "rtf" from extension
		t.Fatalf("format=%q", format)
	}
	// Tool-level wrapper must still include craft_tool guidance.
	out := ToolReadDocument(map[string]interface{}{"file_path": path})
	if !strings.Contains(out, "craft_tool") {
		t.Fatalf("expected craft_tool, got: %s", out)
	}
}

func TestExtractOfficeText_SniffPDFWrongExt(t *testing.T) {
	dir := t.TempDir()
	// Minimal PDF header; extractor may still fail content, but sniff should route to pdf.
	path := filepath.Join(dir, "misnamed.bin")
	if err := os.WriteFile(path, []byte("%PDF-1.4\n%\xe2\xe3\xcf\xd3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Without extension knowledge, sniff returns pdf; parse may fail on tiny stub.
	sniffed := sniffOfficeFormat(path)
	if sniffed != "pdf" {
		t.Fatalf("sniff=%q want pdf", sniffed)
	}
}

func TestExtractOfficeText_SniffDOCXWrongExt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "misnamed.bin")
	writeMinimalDOCX(t, path, "sniffed docx body")
	text, format, err := ExtractOfficeText(path)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if format != "docx" {
		t.Fatalf("format=%q want docx", format)
	}
	if !strings.Contains(text, "sniffed docx body") {
		t.Fatalf("text=%q", text)
	}
}

func writeMinimalDOCX(t *testing.T, path, text string) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create docx: %v", err)
	}
	zw := zip.NewWriter(file)
	w, err := zw.Create("word/document.xml")
	if err != nil {
		t.Fatalf("create document xml: %v", err)
	}
	// Escape XML special chars in text for valid docx fixtures.
	escaped := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(text)
	_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>` + escaped + `</w:t></w:r></w:p></w:body></w:document>`))
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close docx: %v", err)
	}
}
