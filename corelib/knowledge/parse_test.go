package knowledge

import (
	"archive/zip"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	excelread "github.com/RapidAI/CodeClaw/corelib/excel"
	"github.com/shakinm/xlsReader/doc"
	"github.com/shakinm/xlsReader/xls"
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

func TestParsePlainTextNodesAnnotatesMultilingualMetadata(t *testing.T) {
	source := Source{ID: "ksrc_multi", Kind: SourceKindText, Title: "多语言", RelativePath: "multi.txt"}
	nodes := parsePlainTextNodes(source, "日本語の文書です。한국어 문서입니다。", "text")
	nodes = annotateMultilingualNodes(nodes)
	if len(nodes) != 1 {
		t.Fatalf("nodes = %d, want 1", len(nodes))
	}
	if got := nodes[0].Metadata["script"]; got != "Jpan" {
		t.Fatalf("script = %q, want Jpan because kana identifies Japanese", got)
	}
	if got := nodes[0].Metadata["language"]; got != "ja" {
		t.Fatalf("language = %q, want ja", got)
	}
	if nodes[0].Metadata["chunker_version"] != chunkerVersion {
		t.Fatalf("chunker version = %q", nodes[0].Metadata["chunker_version"])
	}
}

func TestAnnotateMultilingualNodesSplitsLongTextByTokenBudget(t *testing.T) {
	source := Source{ID: "ksrc_budget", Kind: SourceKindText, Title: "Long", RelativePath: "long.txt"}
	// CJK is estimated at roughly one token per two runes, so this crosses the
	// 700-token bound while avoiding parser-specific paragraph splitting.
	text := strings.Repeat("知识库检索需要保留语义边界。", 130)
	nodes := annotateMultilingualNodes([]DocumentNode{{ID: "parent", SourceID: source.ID, Type: "document", Title: source.Title, Text: text}})
	if len(nodes) < 3 {
		t.Fatalf("nodes = %d, want token-bounded chunks", len(nodes))
	}
	if nodes[0].ID != "parent" || nodes[0].Text != "" || nodes[0].Metadata["chunk_parent"] != "true" {
		t.Fatalf("expected retained structural parent, got %#v", nodes[0])
	}
	for i, node := range nodes[1:] {
		if node.TokenCount > targetTextNodeTokens+chunkOverlapTokens {
			t.Fatalf("node %d tokens = %d, want bounded", i, node.TokenCount)
		}
		if node.ParentID != "parent" || node.Metadata["chunk_count"] == "" {
			t.Fatalf("node %d missing parent provenance: %#v", i, node)
		}
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

func TestParseDocumentNodesRejectsOfficeContainerDisguisedAsPlainText(t *testing.T) {
	for _, test := range []struct {
		name string
		kind string
		ext  string
	}{
		{name: "markdown", kind: SourceKindMarkdown, ext: ".md"},
		{name: "text", kind: SourceKindText, ext: ".txt"},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "disguised"+test.ext)
			if err := os.WriteFile(path, []byte("PK\x03\x04not a valid Office archive"), 0o600); err != nil {
				t.Fatal(err)
			}
			source := Source{ID: "ksrc_disguised_" + test.name, Kind: test.kind, Title: "Disguised", RelativePath: filepath.Base(path)}
			nodes, err := ParseDocumentNodes(source, path, test.kind)
			if !errors.Is(err, agent.ErrOfficeReadUnsafeContainer) || len(nodes) != 0 {
				t.Fatalf("ParseDocumentNodes = nodes=%#v err=%v, want container rejection", nodes, err)
			}
		})
	}
}

func TestParseDocumentNodesRejectsValidOfficeContainerDisguisedAsPlainText(t *testing.T) {
	path := filepath.Join(t.TempDir(), "disguised.md")
	writeOfficeReadDOCX(t, path, "Office bytes must not be parsed as Markdown", nil)
	source := Source{ID: "ksrc_disguised_valid", Kind: SourceKindMarkdown, Title: "Disguised", RelativePath: filepath.Base(path)}
	nodes, err := ParseDocumentNodes(source, path, SourceKindMarkdown)
	if !errors.Is(err, agent.ErrOfficeReadFormatMismatch) || len(nodes) != 0 {
		t.Fatalf("ParseDocumentNodes = nodes=%#v err=%v, want format mismatch", nodes, err)
	}
}

func TestParseDocumentNodesRejectsOversizedCSVBeforeGridRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "oversized.csv")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(agent.MaxOfficeReadFileBytes + 1); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	source := Source{ID: "ksrc_oversized_csv", Kind: SourceKindCSV, Title: "Oversized", RelativePath: "oversized.csv"}
	if _, err := ParseDocumentNodes(source, path, SourceKindCSV); !errors.Is(err, agent.ErrOfficeReadInputTooLarge) {
		t.Fatalf("ParseDocumentNodes oversized CSV err = %v, want shared input limit", err)
	}
}

func TestParseDocumentNodesRejectsDocumentContainersDisguisedAsCSV(t *testing.T) {
	for _, test := range []struct {
		name  string
		write func(*testing.T, string)
		want  error
	}{
		{
			name: "docx",
			write: func(t *testing.T, path string) {
				writeOfficeReadDOCX(t, path, "must not become a spreadsheet", nil)
			},
			want: agent.ErrOfficeReadFormatMismatch,
		},
		{
			name: "pdf",
			write: func(t *testing.T, path string) {
				if err := os.WriteFile(path, []byte("%PDF-1.4\n% CSV disguise\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			},
			want: agent.ErrOfficeReadFormatMismatch,
		},
		{
			name: "encrypted ooxml",
			write: func(t *testing.T, path string) {
				writeOfficeReadEncryptedDOCX(t, path)
			},
			want: agent.ErrOfficeReadEncryptedContainer,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "disguised.csv")
			test.write(t, path)
			source := Source{ID: "ksrc_disguised_csv", Kind: SourceKindCSV, Title: "Disguised", RelativePath: "disguised.csv"}
			nodes, err := ParseDocumentNodes(source, path, SourceKindCSV)
			if !errors.Is(err, test.want) || len(nodes) != 0 {
				t.Fatalf("ParseDocumentNodes = nodes=%#v err=%v, want %v", nodes, err, test.want)
			}
		})
	}
}

func TestParseDocumentNodesRejectsOversizedPDFBeforeRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "oversized.pdf")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(agent.MaxOfficeReadFileBytes + 1); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	source := Source{ID: "ksrc_oversized_pdf", Kind: SourceKindPDF, Title: "Oversized", RelativePath: "oversized.pdf"}
	if _, err := ParseDocumentNodes(source, path, SourceKindPDF); !errors.Is(err, agent.ErrOfficeReadInputTooLarge) {
		t.Fatalf("ParseDocumentNodes oversized PDF err = %v, want shared input limit", err)
	}
}

func TestParsePDFNodesRecoversPageTreePanic(t *testing.T) {
	path := filepath.Join(t.TempDir(), "panic.pdf")
	if err := os.WriteFile(path, []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := parsePDFNodesWith(Source{ID: "panic", Title: "Panic"}, path,
		func([]byte) (int, error) { panic("malformed page tree") },
		func([]byte) (string, error) { return "", nil },
	)
	if err == nil || !strings.Contains(err.Error(), "parse PDF panicked") {
		t.Fatalf("page tree panic error = %v", err)
	}
}

func TestParsePDFNodesRecoversFallbackPanic(t *testing.T) {
	path := filepath.Join(t.TempDir(), "panic-fallback.pdf")
	if err := os.WriteFile(path, []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := parsePDFNodesWith(Source{ID: "panic", Title: "Panic"}, path,
		func([]byte) (int, error) { return 0, errors.New("page tree unavailable") },
		func([]byte) (string, error) { panic("malformed fallback stream") },
	)
	if err == nil || !strings.Contains(err.Error(), "parse PDF panicked") {
		t.Fatalf("fallback panic error = %v", err)
	}
}

func TestParsePDFNodesRejectsExcessivePageCountBeforeExtraction(t *testing.T) {
	path := filepath.Join(t.TempDir(), "many-pages.pdf")
	if err := os.WriteFile(path, []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := parsePDFNodesWith(Source{ID: "many", Title: "Many"}, path,
		func([]byte) (int, error) { return maxKnowledgePDFPages + 1, nil },
		func([]byte) (string, error) {
			t.Fatal("fallback must not run for a valid excessive page count")
			return "", nil
		},
	)
	if err == nil || !strings.Contains(err.Error(), "too many pages") {
		t.Fatalf("excessive page count error = %v", err)
	}
}

func TestExtractPDFPageTextsRecoversPerPagePanic(t *testing.T) {
	texts := extractPDFPageTextsWith([]byte("fixture"), 3, func(_ []byte, page int) (string, error) {
		if page == 1 {
			panic("malformed page stream")
		}
		return fmt.Sprintf("page %d", page+1), nil
	})
	if got, want := texts, []string{"page 1", "", "page 3"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("page text extraction after panic = %#v, want %#v", got, want)
	}
}

func TestExtractPDFPageTextsSinglePageRecoversPanic(t *testing.T) {
	texts := extractPDFPageTextsWith([]byte("fixture"), 1, func([]byte, int) (string, error) {
		panic("malformed page stream")
	})
	if got, want := texts, []string{""}; !reflect.DeepEqual(got, want) {
		t.Fatalf("single-page panic result = %#v, want %#v", got, want)
	}
}

func TestDOCXParagraphReaderRejectsOversizedParagraphBeforeRetention(t *testing.T) {
	tooLarge := strings.Repeat("文", maxNodeTextRunes+1)
	reader := strings.NewReader(`<w:document xmlns:w="urn:test"><w:body><w:p><w:r><w:t>` + tooLarge + `</w:t></w:r></w:p></w:body></w:document>`)
	if _, err := docxParagraphsFromReader(reader); !errors.Is(err, agent.ErrOfficeReadOutputTooLarge) {
		t.Fatalf("oversized DOCX paragraph error = %v", err)
	}
}

func TestDOCXParagraphReaderPreservesParagraphStructure(t *testing.T) {
	reader := strings.NewReader(`<w:document xmlns:w="urn:test"><w:body><w:p><w:r><w:t>First</w:t></w:r><w:tab/><w:r><w:t>column</w:t></w:r></w:p><w:p><w:r><w:t>Second</w:t></w:r><w:br/><w:r><w:t>line</w:t></w:r></w:p></w:body></w:document>`)
	paragraphs, err := docxParagraphsFromReader(reader)
	if err != nil {
		t.Fatalf("docxParagraphsFromReader: %v", err)
	}
	if got, want := paragraphs, []string{"First\tcolumn", "Second\nline"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("paragraphs = %#v, want %#v", got, want)
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

func TestParseLegacyDOCNativeRecoversThirdPartyPanic(t *testing.T) {
	_, err := parseDOCNativeWithOpen(Source{ID: "ksrc_doc_panic", Kind: SourceKindDOC}, "ignored.doc", func(string) (doc.Document, error) {
		panic("malformed Word record")
	})
	if err == nil || !strings.Contains(err.Error(), "parse doc panicked") {
		t.Fatalf("DOC panic error = %v", err)
	}
}

func TestParseLegacyXLSNativeRecoversThirdPartyPanic(t *testing.T) {
	_, err := parseXLSNativeWithOpen(Source{ID: "ksrc_xls_panic", Kind: SourceKindXLS}, "ignored.xls", func(string) (xls.Workbook, error) {
		panic("malformed BIFF record")
	})
	if err == nil || !strings.Contains(err.Error(), "parse xls panicked") {
		t.Fatalf("XLS panic error = %v", err)
	}
}

func TestParseOfficeReadOrLegacyNodesRecoversLegacyParserPanic(t *testing.T) {
	_, err := parseOfficeReadOrLegacyNodes(
		Source{ID: "ksrc_legacy_panic", Kind: SourceKindPPTX},
		"ignored.pptx",
		SourceKindPPTX,
		func(Source, string) ([]DocumentNode, error) { panic("malformed presentation record") },
		&officeReadRichExtraction{loaded: true},
	)
	if err == nil || !strings.Contains(err.Error(), "legacy Office parser panicked") {
		t.Fatalf("legacy parser panic error = %v", err)
	}
}

func TestParseOfficeReadOrLegacyNodesDoesNotFallbackForRichOutputLimit(t *testing.T) {
	called := false
	extraction := &officeReadRichExtraction{
		loaded:  true,
		enabled: true,
		err:     agent.ErrOfficeReadOutputTooLarge,
	}
	_, err := parseOfficeReadOrLegacyNodes(
		Source{ID: "ksrc_rich_output_limit", Kind: SourceKindDOCX},
		"ignored.docx",
		SourceKindDOCX,
		func(Source, string) ([]DocumentNode, error) {
			called = true
			return []DocumentNode{{ID: "legacy-node", Text: "must not persist"}}, nil
		},
		extraction,
	)
	if !errors.Is(err, agent.ErrOfficeReadOutputTooLarge) {
		t.Fatalf("rich output limit error = %v", err)
	}
	if called {
		t.Fatal("rich output limit must not reopen the legacy node parser")
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
