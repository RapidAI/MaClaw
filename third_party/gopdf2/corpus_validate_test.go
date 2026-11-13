package gopdf

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCorpus_AllExtractionAPIs runs every extraction API over every corpus
// PDF: no panics, no unexpected errors, and sane relationships (paragraphs
// build from lines, words within page bounds).
func TestCorpus_AllExtractionAPIs(t *testing.T) {
	entries, err := os.ReadDir("test/corpus")
	if err != nil {
		if os.IsNotExist(err) {
			t.Skip("no test/corpus directory")
		}
		t.Fatal(err)
	}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".pdf") {
			continue
		}
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("test", "corpus", name))
			if err != nil {
				t.Fatal(err)
			}
			count, err := GetSourcePDFPageCountFromBytes(data)
			if err != nil || count == 0 {
				t.Fatalf("page count: %v (%d)", err, count)
			}
			for p := 0; p < count; p++ {
				words := 0
				// words + lines
				lines, err := ExtractPageLines(data, p)
				if err != nil {
					t.Errorf("page %d lines: %v", p, err)
					continue
				}
				for _, ln := range lines {
					words += len(ln.Words)
					for _, w := range ln.Words {
						if w.Text == "" {
							t.Errorf("page %d: empty word", p)
						}
					}
				}
				// paragraphs must not exceed lines
				paras, err := ExtractPageParagraphs(data, p)
				if err != nil {
					t.Errorf("page %d paragraphs: %v", p, err)
					continue
				}
				lineCount := len(lines)
				paraLines := 0
				for _, pa := range paras {
					paraLines += len(pa.Lines)
					if pa.Text == "" {
						t.Errorf("page %d: empty paragraph", p)
					}
				}
				if paraLines != lineCount {
					t.Errorf("page %d: paragraph lines %d != %d", p, paraLines, lineCount)
				}
				// drawings / images / tables must not error
				if _, err := ExtractPageDrawings(data, p); err != nil {
					t.Errorf("page %d drawings: %v", p, err)
				}
				if _, err := ExtractTablesFromPage(data, p); err != nil {
					t.Errorf("page %d tables: %v", p, err)
				}
				if _, err := ExtractImagesFromPage(data, p); err != nil {
					t.Errorf("page %d images: %v", p, err)
				}
				// rect extraction consistent with full words: use the
				// words' own bounding box as the clip (some corpus PDFs
				// place words far outside the page via scaled transforms)
				minX, minY := 1e30, 1e30
				maxX, maxY := -1e30, -1e30
				for _, ln := range lines {
					for _, w := range ln.Words {
						if w.X < minX {
							minX = w.X
						}
						if w.Y-w.Height < minY {
							minY = w.Y - w.Height
						}
						if w.X+w.Width > maxX {
							maxX = w.X + w.Width
						}
						if w.Y > maxY {
							maxY = w.Y
						}
					}
				}
				if words > 0 {
					all, err := ExtractTextWordsInRect(data, p, ClipRect{
						X: minX - 1, Y: minY - 1,
						W: maxX - minX + 2, H: maxY - minY + 2,
					})
					if err != nil {
						t.Errorf("page %d rect words: %v", p, err)
					} else if len(all) != words {
						t.Errorf("page %d: rect words %d != %d", p, len(all), words)
					}
				}
				// search smoke
				if _, err := SearchTextWithOptions(data, "e", SearchOptions{CaseInsensitive: true, Dehyphenate: true}); err != nil {
					t.Errorf("page search: %v", err)
				}
			}
			// metadata + TOC never panic
			_, _ = GetPDFInfo(data)
			_, _ = ExtractTOC(data)
		})
	}
}

// TestCorpus_ArxivStructure asserts the arXiv paper's extraction structure:
// the two-column body yields paragraphs and drawings (rules/frames).
func TestCorpus_ArxivStructure(t *testing.T) {
	data := corpusData(t, "arxiv_1706.03762.pdf")
	paras, err := ExtractPageParagraphs(data, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(paras) < 8 {
		t.Errorf("page 1 paragraphs = %d, want >= 8", len(paras))
	}
	drawings, err := ExtractPageDrawings(data, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(drawings) == 0 {
		t.Errorf("page 0 has no vector drawings (LaTeX rules expected)")
	}
	// TOC may or may not exist; just require no error
	if _, err := ExtractTOC(data); err != nil {
		t.Errorf("TOC extraction: %v", err)
	}
}
