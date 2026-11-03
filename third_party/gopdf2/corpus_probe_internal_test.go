package gopdf

// Internal probe for the raw text-extraction parser against the downloaded
// corpus (test/corpus). Excluded from normal `go test ./...` runs via the
// corpus tag: go test -tags=corpus -run TestCorpusProbe .
// Run with -v to see extraction output per file.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode"
)

func corpusFile(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("test", "corpus", name))
	if err != nil {
		t.Skipf("corpus file %s not available: %v", name, err)
	}
	return data
}

func rawExtractChars(t *testing.T, data []byte) (runs int, text string) {
	t.Helper()
	parser, err := newRawPDFParser(data)
	if err != nil {
		return 0, ""
	}
	var sb strings.Builder
	runs = 0
	for i := range parser.pages {
		stream := parser.getPageContentStream(i)
		if len(stream) == 0 {
			continue
		}
		page := parser.pages[i]
		fonts := buildFontMap(parser, page)
		texts := parseTextOperators(stream, fonts, page.mediaBox)
		runs += len(texts)
		for _, tx := range texts {
			sb.WriteString(tx.Text)
			sb.WriteByte('\n')
		}
	}
	return runs, sb.String()
}

func TestCorpusProbeRawParser(t *testing.T) {
	files := []string{
		"PDF20-simple.pdf", "PDF20-utf8-test.pdf", "PDF20-UTF8-annotation.pdf",
		"XiaoBiaoSong.pdf", "arial_unicode_en_cidfont.pdf", "complex_ttf_font.pdf",
		"font_ascent_descent.pdf", "ArabicCIDTrueType.pdf", "basicapi.pdf",
		"S2.pdf", "TAMReview.pdf", "160F-2019.pdf",
		"arxiv_1706.03762.pdf", "formulae_tables.pdf", "ThuluthFeatures.pdf",
	}
	for _, name := range files {
		t.Run(name, func(t *testing.T) {
			data := corpusFile(t, name)
			done := make(chan struct{})
			var runs int
			var text string
			go func() {
				defer close(done)
				runs, text = rawExtractChars(t, data)
			}()
			select {
			case <-done:
			case <-time.After(30 * time.Second):
				t.Fatalf("raw parser TIMED OUT on %s", name)
			}
			han := 0
			for _, r := range text {
				if unicode.Is(unicode.Han, r) {
					han++
				}
			}
			preview := text
			if len(preview) > 500 {
				preview = preview[:500]
			}
			preview = strings.ReplaceAll(preview, "\n", " | ")
			t.Logf("%s: %d runs, %d chars, %d Han\n%s", name, runs, len([]rune(text)), han, preview)
		})
	}
}
