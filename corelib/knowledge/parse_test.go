package knowledge

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	excelread "github.com/RapidAI/CodeClaw/corelib/excel"
)

func TestParseMarkdownNodesSplitsHeadings(t *testing.T) {
	source := Source{ID: "ksrc_md", Kind: SourceKindMarkdown, Title: "Design", RelativePath: "design.md"}
	nodes := parseMarkdownNodes(source, "# Intro\n\nAlpha overview.\n\n## Details\n\nBeta detail.\n\n```go\n# not a heading\n```\n\n### Deep\nGamma")
	if len(nodes) != 3 {
		t.Fatalf("nodes = %d, want 3: %#v", len(nodes), nodes)
	}
	if nodes[0].Title != "Intro" || nodes[0].Level != 1 || nodes[0].Type != "section" || nodes[0].Offset != 1 {
		t.Fatalf("unexpected first node: %#v", nodes[0])
	}
	if nodes[1].Title != "Details" || nodes[1].Level != 2 || nodes[1].ParentID != nodes[0].ID {
		t.Fatalf("unexpected second node: %#v parent=%s", nodes[1], nodes[0].ID)
	}
	if nodes[2].Title != "Deep" || nodes[2].Level != 3 || nodes[2].ParentID != nodes[1].ID {
		t.Fatalf("unexpected third node: %#v parent=%s", nodes[2], nodes[1].ID)
	}
	if nodes[1].Text == "" || nodes[1].Metadata["format"] != "markdown" || nodes[1].Metadata["line_start"] == "" {
		t.Fatalf("expected markdown metadata and text: %#v", nodes[1])
	}
}

func TestParseMarkdownNodesFallsBackWithoutHeadings(t *testing.T) {
	source := Source{ID: "ksrc_plain", Kind: SourceKindMarkdown, Title: "Plain", RelativePath: "plain.md"}
	nodes := parseMarkdownNodes(source, "Plain body without headings.")
	if len(nodes) != 1 {
		t.Fatalf("nodes = %d, want 1", len(nodes))
	}
	if nodes[0].Type != "document" || nodes[0].Title != "Plain" || nodes[0].Text == "" {
		t.Fatalf("unexpected fallback node: %#v", nodes[0])
	}
}

func TestParsePlainTextNodesSplitsLongParagraphGroups(t *testing.T) {
	source := Source{ID: "ksrc_txt", Kind: SourceKindText, Title: "Plain", RelativePath: "plain.txt"}
	longA := strings.Repeat("alpha ", 900)
	longB := strings.Repeat("beta ", 900)
	nodes := parsePlainTextNodes(source, longA+"\n\n"+longB, "text")
	if len(nodes) != 2 {
		t.Fatalf("nodes = %d, want 2", len(nodes))
	}
	if nodes[0].Offset != 1 || nodes[1].Offset != 2 || nodes[1].Metadata["paragraph_start"] != "2" {
		t.Fatalf("unexpected text node offsets: %#v", nodes)
	}
	if nodes[0].Title == nodes[1].Title || nodes[0].Text == "" || nodes[1].Text == "" {
		t.Fatalf("expected titled text chunks: %#v", nodes)
	}
}

func TestSheetToNodesSplitsLargeSheetsWithRowRanges(t *testing.T) {
	source := Source{ID: "ksrc_xlsx", Kind: SourceKindXLSX, Title: "Workbook", RelativePath: "book.xlsx"}
	rows := make([][]excelread.CellValue, 0, 4)
	rows = append(rows, []excelread.CellValue{{Value: "Name"}, {Value: "Value"}})
	rows = append(rows, []excelread.CellValue{{Value: strings.Repeat("alpha ", 1400)}, {Value: "A"}})
	rows = append(rows, []excelread.CellValue{{Value: strings.Repeat("beta ", 1400)}, {Value: "B"}})
	rows = append(rows, []excelread.CellValue{{Value: "tail"}, {Value: "C"}})
	nodes := sheetToNodes(source, &excelread.ReadResult{SheetName: "Data", Rows: rows, RowCount: len(rows), ColCount: 2}, SourceKindXLSX)
	if len(nodes) < 2 {
		t.Fatalf("nodes = %d, want at least 2: %#v", len(nodes), nodes)
	}
	if nodes[0].SheetName != "Data" || nodes[0].RowRange == "" || nodes[0].Metadata["row_start"] == "" {
		t.Fatalf("expected row metadata: %#v", nodes[0])
	}
	if nodes[1].Offset <= nodes[0].Offset || !strings.Contains(nodes[1].Title, "rows") {
		t.Fatalf("expected row chunk titles and offsets: %#v", nodes)
	}
}

func TestParseDocumentNodesSupportsCSV(t *testing.T) {
	path := t.TempDir() + "/data.csv"
	if err := os.WriteFile(path, []byte("name,value\nalpha,1\nbeta,2\n"), 0o644); err != nil {
		t.Fatalf("write csv: %v", err)
	}
	source := Source{ID: "ksrc_csv", Kind: SourceKindCSV, Title: "Data", RelativePath: "data.csv"}
	nodes, err := ParseDocumentNodes(source, path, SourceKindCSV)
	if err != nil {
		t.Fatalf("ParseDocumentNodes csv: %v", err)
	}
	if len(nodes) != 1 || nodes[0].SheetName != "Sheet1" || nodes[0].Metadata["format"] != SourceKindCSV || !strings.Contains(nodes[0].Text, "alpha") {
		t.Fatalf("unexpected csv nodes: %#v", nodes)
	}
}

func TestParseLegacyDOCNative(t *testing.T) {
	// The native DOC parser requires a real .doc binary file.
	// With a placeholder file it should return an error (not panic).
	root := t.TempDir()
	legacyPath := filepath.Join(root, "legacy.doc")
	if err := os.WriteFile(legacyPath, []byte("legacy placeholder not a real doc"), 0o644); err != nil {
		t.Fatalf("write legacy doc: %v", err)
	}

	source := Source{ID: "ksrc_doc", Kind: SourceKindDOC, Title: "Legacy", RelativePath: "legacy.doc"}
	_, err := ParseDocumentNodes(source, legacyPath, SourceKindDOC)
	if err == nil {
		t.Fatalf("expected error parsing invalid .doc placeholder, got nil")
	}
}

func TestParseLegacyXLSNative(t *testing.T) {
	// The native XLS parser requires a real .xls binary file.
	// With a placeholder file it should return an error (not panic).
	root := t.TempDir()
	legacyPath := filepath.Join(root, "legacy.xls")
	if err := os.WriteFile(legacyPath, []byte("legacy placeholder not a real xls"), 0o644); err != nil {
		t.Fatalf("write legacy xls: %v", err)
	}

	source := Source{ID: "ksrc_xls", Kind: SourceKindXLS, Title: "Legacy", RelativePath: "legacy.xls"}
	_, err := ParseDocumentNodes(source, legacyPath, SourceKindXLS)
	if err == nil {
		t.Fatalf("expected error parsing invalid .xls placeholder, got nil")
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
	_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>` + text + `</w:t></w:r></w:p></w:body></w:document>`))
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close docx: %v", err)
	}
}
