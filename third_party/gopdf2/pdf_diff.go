package gopdf

import (
	"bytes"
	"fmt"
	"math"
	"sort"
	"strings"
)

// DiffType represents the type of difference found between two PDFs.
type DiffType int

const (
	// DiffPageCount indicates different number of pages.
	DiffPageCount DiffType = iota
	// DiffPageSize indicates different page dimensions.
	DiffPageSize
	// DiffTextContent indicates different text content on a page.
	DiffTextContent
	// DiffTextAdded indicates text present in doc2 but not doc1.
	DiffTextAdded
	// DiffTextRemoved indicates text present in doc1 but not doc2.
	DiffTextRemoved
	// DiffTextMoved indicates text that moved position between docs.
	DiffTextMoved
	// DiffImageCount indicates different number of images on a page.
	DiffImageCount
	// DiffFontDifference indicates different fonts used.
	DiffFontDifference
	// DiffMetadata indicates different document metadata.
	DiffMetadata
	// DiffAnnotation indicates different annotations.
	DiffAnnotation
)

// String returns a human-readable name for the diff type.
func (d DiffType) String() string {
	switch d {
	case DiffPageCount:
		return "PageCount"
	case DiffPageSize:
		return "PageSize"
	case DiffTextContent:
		return "TextContent"
	case DiffTextAdded:
		return "TextAdded"
	case DiffTextRemoved:
		return "TextRemoved"
	case DiffTextMoved:
		return "TextMoved"
	case DiffImageCount:
		return "ImageCount"
	case DiffFontDifference:
		return "FontDifference"
	case DiffMetadata:
		return "Metadata"
	case DiffAnnotation:
		return "Annotation"
	default:
		return "Unknown"
	}
}

// PDFDifference represents a single difference between two PDF documents.
type PDFDifference struct {
	// Type is the kind of difference.
	Type DiffType
	// PageIndex is the 0-based page index (-1 for document-level diffs).
	PageIndex int
	// Description is a human-readable description of the difference.
	Description string
	// Detail1 is detail from the first document.
	Detail1 string
	// Detail2 is detail from the second document.
	Detail2 string
}

// PDFDiffResult holds the complete comparison result.
type PDFDiffResult struct {
	// Differences lists all found differences.
	Differences []PDFDifference
	// IsIdentical is true if no differences were found.
	IsIdentical bool
	// Summary provides a brief overview.
	Summary string
	// PageCount1 is the page count of the first document.
	PageCount1 int
	// PageCount2 is the page count of the second document.
	PageCount2 int
}

// PDFDiffOptions configures the comparison behavior.
type PDFDiffOptions struct {
	// CompareText enables text content comparison. Default: true.
	CompareText bool
	// CompareImages enables image count comparison. Default: true.
	CompareImages bool
	// CompareFonts enables font comparison. Default: true.
	CompareFonts bool
	// CompareMetadata enables metadata comparison. Default: false.
	CompareMetadata bool
	// CompareAnnotations enables annotation comparison. Default: false.
	CompareAnnotations bool
	// TextTolerance is the position tolerance for text comparison (in points).
	// Default: 2.0.
	TextTolerance float64
	// Pages limits comparison to specific pages (0-based). Empty = all pages.
	Pages []int
}

// ComparePDF compares two PDF documents and returns their differences.
//
// Example:
//
//	data1, _ := os.ReadFile("original.pdf")
//	data2, _ := os.ReadFile("modified.pdf")
//	result, _ := gopdf.ComparePDF(data1, data2, nil)
//	if result.IsIdentical {
//	    fmt.Println("Documents are identical")
//	} else {
//	    for _, d := range result.Differences {
//	        fmt.Printf("[%s] Page %d: %s\n", d.Type, d.PageIndex, d.Description)
//	    }
//	}
func ComparePDF(pdfData1, pdfData2 []byte, opts *PDFDiffOptions) (*PDFDiffResult, error) {
	if opts == nil {
		opts = &PDFDiffOptions{
			CompareText:   true,
			CompareImages: true,
			CompareFonts:  true,
			TextTolerance: 2.0,
		}
	}
	if opts.TextTolerance <= 0 {
		opts.TextTolerance = 2.0
	}

	parser1, err := newRawPDFParser(pdfData1)
	if err != nil {
		return nil, fmt.Errorf("parse first PDF: %w", err)
	}
	parser2, err := newRawPDFParser(pdfData2)
	if err != nil {
		return nil, fmt.Errorf("parse second PDF: %w", err)
	}

	result := &PDFDiffResult{
		PageCount1: len(parser1.pages),
		PageCount2: len(parser2.pages),
	}

	// Compare page counts.
	if len(parser1.pages) != len(parser2.pages) {
		result.Differences = append(result.Differences, PDFDifference{
			Type:        DiffPageCount,
			PageIndex:   -1,
			Description: fmt.Sprintf("Page count differs: %d vs %d", len(parser1.pages), len(parser2.pages)),
			Detail1:     fmt.Sprintf("%d pages", len(parser1.pages)),
			Detail2:     fmt.Sprintf("%d pages", len(parser2.pages)),
		})
	}

	// Build page set for filtering.
	pageSet := make(map[int]bool, len(opts.Pages))
	for _, p := range opts.Pages {
		pageSet[p] = true
	}

	// Compare pages.
	maxPages := len(parser1.pages)
	if len(parser2.pages) < maxPages {
		maxPages = len(parser2.pages)
	}

	for i := 0; i < maxPages; i++ {
		if len(pageSet) > 0 && !pageSet[i] {
			continue
		}

		page1 := parser1.pages[i]
		page2 := parser2.pages[i]

		// Compare page sizes.
		comparePageSizes(result, i, page1, page2)

		// Compare text content.
		if opts.CompareText {
			compareTextContent(result, pdfData1, pdfData2, parser1, parser2, i, opts.TextTolerance)
		}

		// Compare fonts.
		if opts.CompareFonts {
			compareFonts(result, parser1, parser2, i, page1, page2)
		}

		// Compare images.
		if opts.CompareImages {
			compareImages(result, parser1, parser2, i, page1, page2)
		}
	}

	// Compare metadata.
	if opts.CompareMetadata {
		compareMetadata(result, pdfData1, pdfData2)
	}

	result.IsIdentical = len(result.Differences) == 0
	result.Summary = buildDiffSummary(result)

	return result, nil
}

func comparePageSizes(result *PDFDiffResult, pageIdx int, page1, page2 rawPDFPage) {
	w1 := page1.mediaBox[2] - page1.mediaBox[0]
	h1 := page1.mediaBox[3] - page1.mediaBox[1]
	w2 := page2.mediaBox[2] - page2.mediaBox[0]
	h2 := page2.mediaBox[3] - page2.mediaBox[1]

	if math.Abs(w1-w2) > 0.1 || math.Abs(h1-h2) > 0.1 {
		result.Differences = append(result.Differences, PDFDifference{
			Type:        DiffPageSize,
			PageIndex:   pageIdx,
			Description: fmt.Sprintf("Page %d size differs", pageIdx),
			Detail1:     fmt.Sprintf("%.1f x %.1f", w1, h1),
			Detail2:     fmt.Sprintf("%.1f x %.1f", w2, h2),
		})
	}
}

func compareTextContent(result *PDFDiffResult, data1, data2 []byte,
	parser1, parser2 *rawPDFParser, pageIdx int, tolerance float64) {

	// Extract structured text using the already-parsed parsers.
	texts1 := extractTextsWithParser(parser1, data1, pageIdx)
	texts2 := extractTextsWithParser(parser2, data2, pageIdx)

	// Derive plain text for quick equality check.
	text1 := joinExtractedTexts(texts1)
	text2 := joinExtractedTexts(texts2)

	if text1 == text2 {
		// Text content is the same; check if positions differ (moved text).
		if len(texts1) == len(texts2) {
			for j := 0; j < len(texts1); j++ {
				if math.Abs(texts1[j].X-texts2[j].X) > tolerance ||
					math.Abs(texts1[j].Y-texts2[j].Y) > tolerance {
					result.Differences = append(result.Differences, PDFDifference{
						Type:        DiffTextMoved,
						PageIndex:   pageIdx,
						Description: fmt.Sprintf("Text '%s' moved", truncateText(texts1[j].Text, 30)),
						Detail1:     fmt.Sprintf("(%.0f, %.0f)", texts1[j].X, texts1[j].Y),
						Detail2:     fmt.Sprintf("(%.0f, %.0f)", texts2[j].X, texts2[j].Y),
					})
				}
			}
		}
		return
	}

	// Build text maps for comparison.
	map1 := make(map[string][]ExtractedText)
	for _, t := range texts1 {
		map1[t.Text] = append(map1[t.Text], t)
	}
	map2 := make(map[string][]ExtractedText)
	for _, t := range texts2 {
		map2[t.Text] = append(map2[t.Text], t)
	}

	// Collect text keys and sort for deterministic output.
	allKeys := make(map[string]struct{})
	for k := range map1 {
		allKeys[k] = struct{}{}
	}
	for k := range map2 {
		allKeys[k] = struct{}{}
	}
	sortedKeys := make([]string, 0, len(allKeys))
	for k := range allKeys {
		sortedKeys = append(sortedKeys, k)
	}
	sort.Strings(sortedKeys)

	for _, text := range sortedKeys {
		items1, in1 := map1[text]
		items2, in2 := map2[text]

		if in1 && !in2 {
			// Text removed.
			for _, item := range items1 {
				result.Differences = append(result.Differences, PDFDifference{
					Type:        DiffTextRemoved,
					PageIndex:   pageIdx,
					Description: fmt.Sprintf("Text removed at (%.0f, %.0f)", item.X, item.Y),
					Detail1:     text,
				})
			}
		} else if !in1 && in2 {
			// Text added.
			for _, item := range items2 {
				result.Differences = append(result.Differences, PDFDifference{
					Type:        DiffTextAdded,
					PageIndex:   pageIdx,
					Description: fmt.Sprintf("Text added at (%.0f, %.0f)", item.X, item.Y),
					Detail2:     text,
				})
			}
		} else if in1 && in2 {
			// Check for moved text.
			for j := 0; j < len(items1) && j < len(items2); j++ {
				if math.Abs(items1[j].X-items2[j].X) > tolerance ||
					math.Abs(items1[j].Y-items2[j].Y) > tolerance {
					result.Differences = append(result.Differences, PDFDifference{
						Type:        DiffTextMoved,
						PageIndex:   pageIdx,
						Description: fmt.Sprintf("Text '%s' moved", truncateText(text, 30)),
						Detail1:     fmt.Sprintf("(%.0f, %.0f)", items1[j].X, items1[j].Y),
						Detail2:     fmt.Sprintf("(%.0f, %.0f)", items2[j].X, items2[j].Y),
					})
				}
			}
		}
	}
}

// extractTextsWithParser extracts text from a page using an already-parsed PDF.
func extractTextsWithParser(parser *rawPDFParser, data []byte, pageIdx int) []ExtractedText {
	if pageIdx < 0 || pageIdx >= len(parser.pages) {
		return nil
	}
	stream := parser.getPageContentStream(pageIdx)
	if len(stream) == 0 {
		return nil
	}
	page := parser.pages[pageIdx]
	fonts := buildFontMap(parser, page)
	return parseTextOperatorsCtx(&extractionContext{
		parser:   parser,
		fonts:    fonts,
		xobjs:    page.resources.xobjs,
		mediaBox: page.mediaBox,
		rotate:   page.rotate,
		visited:  map[int]bool{},
	}, stream)
}

// joinExtractedTexts concatenates extracted text items into a single string.
func joinExtractedTexts(texts []ExtractedText) string {
	if len(texts) == 0 {
		return ""
	}
	var sb strings.Builder
	for i, t := range texts {
		if i > 0 {
			sb.WriteByte(' ')
		}
		sb.WriteString(t.Text)
	}
	return sb.String()
}

func compareFonts(result *PDFDiffResult, parser1, parser2 *rawPDFParser,
	pageIdx int, page1, page2 rawPDFPage) {

	fonts1 := make(map[string]string)
	for name, objNum := range page1.resources.fonts {
		if obj, ok := parser1.objects[objNum]; ok {
			fonts1[name] = extractName(obj.dict, "/BaseFont")
		}
	}

	fonts2 := make(map[string]string)
	for name, objNum := range page2.resources.fonts {
		if obj, ok := parser2.objects[objNum]; ok {
			fonts2[name] = extractName(obj.dict, "/BaseFont")
		}
	}

	// Collect all font names and sort for deterministic output.
	allFonts := make(map[string]struct{})
	for name := range fonts1 {
		allFonts[name] = struct{}{}
	}
	for name := range fonts2 {
		allFonts[name] = struct{}{}
	}
	sortedFonts := make([]string, 0, len(allFonts))
	for name := range allFonts {
		sortedFonts = append(sortedFonts, name)
	}
	sort.Strings(sortedFonts)

	for _, name := range sortedFonts {
		base1, in1 := fonts1[name]
		base2, in2 := fonts2[name]

		if in1 && !in2 {
			result.Differences = append(result.Differences, PDFDifference{
				Type:        DiffFontDifference,
				PageIndex:   pageIdx,
				Description: fmt.Sprintf("Font %s removed", name),
				Detail1:     base1,
			})
		} else if !in1 && in2 {
			result.Differences = append(result.Differences, PDFDifference{
				Type:        DiffFontDifference,
				PageIndex:   pageIdx,
				Description: fmt.Sprintf("Font %s added", name),
				Detail2:     base2,
			})
		} else if base1 != base2 {
			result.Differences = append(result.Differences, PDFDifference{
				Type:        DiffFontDifference,
				PageIndex:   pageIdx,
				Description: fmt.Sprintf("Font %s changed", name),
				Detail1:     base1,
				Detail2:     base2,
			})
		}
	}
}

func compareImages(result *PDFDiffResult, parser1, parser2 *rawPDFParser,
	pageIdx int, page1, page2 rawPDFPage) {

	count1 := countImages(parser1, page1)
	count2 := countImages(parser2, page2)

	if count1 != count2 {
		result.Differences = append(result.Differences, PDFDifference{
			Type:        DiffImageCount,
			PageIndex:   pageIdx,
			Description: fmt.Sprintf("Image count differs on page %d", pageIdx),
			Detail1:     fmt.Sprintf("%d images", count1),
			Detail2:     fmt.Sprintf("%d images", count2),
		})
	}
}

func countImages(parser *rawPDFParser, page rawPDFPage) int {
	count := 0
	for _, objNum := range page.resources.xobjs {
		if obj, ok := parser.objects[objNum]; ok {
			if strings.Contains(obj.dict, "/Image") {
				count++
			}
		}
	}
	return count
}

func compareMetadata(result *PDFDiffResult, data1, data2 []byte) {
	// Simple metadata comparison via Info dictionary.
	keys := []string{"/Title", "/Author", "/Subject", "/Creator", "/Producer"}
	for _, key := range keys {
		val1 := extractInfoValue(data1, key)
		val2 := extractInfoValue(data2, key)
		if val1 != val2 {
			result.Differences = append(result.Differences, PDFDifference{
				Type:        DiffMetadata,
				PageIndex:   -1,
				Description: fmt.Sprintf("Metadata %s differs", key),
				Detail1:     val1,
				Detail2:     val2,
			})
		}
	}
}

func extractInfoValue(data []byte, key string) string {
	keyBytes := []byte(key)
	idx := bytes.Index(data, keyBytes)
	if idx < 0 {
		return ""
	}
	// Look at a small window after the key to find the value.
	start := idx + len(keyBytes)
	end := start + 256
	if end > len(data) {
		end = len(data)
	}
	rest := data[start:end]
	// Skip whitespace.
	i := 0
	for i < len(rest) && (rest[i] == ' ' || rest[i] == '\t' || rest[i] == '\r' || rest[i] == '\n') {
		i++
	}
	if i < len(rest) && rest[i] == '(' {
		i++ // skip opening (
		// Handle nested parentheses and escape sequences.
		depth := 1
		j := i
		for j < len(rest) && depth > 0 {
			if rest[j] == '\\' {
				j += 2 // skip escaped char
				if j >= len(rest) {
					break
				}
				continue
			}
			if rest[j] == '(' {
				depth++
			} else if rest[j] == ')' {
				depth--
			}
			if depth > 0 {
				j++
			}
		}
		if j <= len(rest) {
			return string(rest[i:j])
		}
	}
	return ""
}

func buildDiffSummary(result *PDFDiffResult) string {
	if result.IsIdentical {
		return "Documents are identical"
	}

	counts := make(map[DiffType]int)
	for _, d := range result.Differences {
		counts[d.Type]++
	}

	// Sort diff types for deterministic output.
	types := make([]DiffType, 0, len(counts))
	for dt := range counts {
		types = append(types, dt)
	}
	sort.Slice(types, func(i, j int) bool { return types[i] < types[j] })

	var parts []string
	for _, dt := range types {
		parts = append(parts, fmt.Sprintf("%d %s", counts[dt], dt.String()))
	}
	return fmt.Sprintf("%d differences found: %s", len(result.Differences), strings.Join(parts, ", "))
}

func truncateText(s string, maxLen int) string {
	if maxLen <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return string(runes[:maxLen])
	}
	return string(runes[:maxLen-3]) + "..."
}
