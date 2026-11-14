package gopdf

import (
	"math"
	"strings"
)

// TextSearchResult represents a single text search match on a page.
type TextSearchResult struct {
	// PageIndex is the 0-based page index where the match was found.
	PageIndex int
	// X is the X coordinate of the match.
	X float64
	// Y is the Y coordinate of the match.
	Y float64
	// Width is the approximate width of the matched text.
	Width float64
	// Height is the approximate height of the matched text.
	Height float64
	// Text is the matched text.
	Text string
	// Context is the surrounding text for context.
	Context string
}

// SearchText searches for text across all pages of the given PDF data.
// Returns all matches with their positions. The search is case-sensitive
// unless caseInsensitive is true.
//
// Example:
//
//	data, _ := os.ReadFile("input.pdf")
//	results, _ := gopdf.SearchText(data, "hello", true)
//	for _, r := range results {
//	    fmt.Printf("Page %d at (%.0f, %.0f): %s\n", r.PageIndex, r.X, r.Y, r.Text)
//	}
func SearchText(pdfData []byte, query string, caseInsensitive bool) ([]TextSearchResult, error) {
	parser, err := newRawPDFParser(pdfData)
	if err != nil {
		return nil, err
	}

	var results []TextSearchResult
	for i := range parser.pages {
		pageResults, err := searchTextOnPage(parser, i, query, caseInsensitive)
		if err != nil {
			continue
		}
		results = append(results, pageResults...)
	}
	return results, nil
}

// SearchTextOnPage searches for text on a specific page (0-based).
func SearchTextOnPage(pdfData []byte, pageIndex int, query string, caseInsensitive bool) ([]TextSearchResult, error) {
	parser, err := newRawPDFParser(pdfData)
	if err != nil {
		return nil, err
	}
	return searchTextOnPage(parser, pageIndex, query, caseInsensitive)
}

func searchTextOnPage(parser *rawPDFParser, pageIndex int, query string, caseInsensitive bool) ([]TextSearchResult, error) {
	if pageIndex < 0 || pageIndex >= len(parser.pages) {
		return nil, nil
	}

	stream := parser.getPageContentStream(pageIndex)
	if len(stream) == 0 {
		return nil, nil
	}

	page := parser.pages[pageIndex]
	fonts := buildFontMap(parser, page)
	texts := parseTextOperatorsCtx(&extractionContext{
		parser:   parser,
		fonts:    fonts,
		xobjs:    page.resources.xobjs,
		mediaBox: page.mediaBox,
		rotate:   page.rotate,
		extgs:    page.resources.extgstates,
		alpha:    1,
		visited:  map[int]bool{},
	}, stream)

	if len(texts) == 0 {
		return nil, nil
	}

	var results []TextSearchResult

	// Build a full-text string with position mapping.
	searchQuery := query
	if caseInsensitive {
		searchQuery = strings.ToLower(query)
	}

	// Search within individual text items first.
	for _, t := range texts {
		textToSearch := t.Text
		if caseInsensitive {
			textToSearch = strings.ToLower(textToSearch)
		}

		idx := 0
		for {
			pos := strings.Index(textToSearch[idx:], searchQuery)
			if pos < 0 {
				break
			}
			matchStart := idx + pos
			matchText := t.Text[matchStart : matchStart+len(query)]

			results = append(results, TextSearchResult{
				PageIndex: pageIndex,
				X:         t.X,
				Y:         t.Y,
				Width:     t.FontSize * float64(len(matchText)) * 0.5,
				Height:    t.FontSize,
				Text:      matchText,
				Context:   t.Text,
			})
			idx = matchStart + len(query)
		}
	}

	// Also search across adjacent text items on the same line.
	if len(results) == 0 {
		results = append(results, searchAcrossItems(texts, pageIndex, query, caseInsensitive)...)
	}

	return results, nil
}

// searchAcrossItems searches for text that spans multiple text items on the same line.
func searchAcrossItems(texts []ExtractedText, pageIndex int, query string, caseInsensitive bool) []TextSearchResult {
	var results []TextSearchResult

	// Group texts by approximate Y position (same line).
	type lineGroup struct {
		y     float64
		texts []ExtractedText
	}
	var lines []lineGroup

	for _, t := range texts {
		found := false
		for i := range lines {
			if math.Abs(lines[i].y-t.Y) < 2 {
				lines[i].texts = append(lines[i].texts, t)
				found = true
				break
			}
		}
		if !found {
			lines = append(lines, lineGroup{y: t.Y, texts: []ExtractedText{t}})
		}
	}

	searchQuery := query
	if caseInsensitive {
		searchQuery = strings.ToLower(query)
	}

	for _, line := range lines {
		// Concatenate all text on this line.
		var combined strings.Builder
		for i, t := range line.texts {
			if i > 0 {
				combined.WriteByte(' ')
			}
			combined.WriteString(t.Text)
		}
		lineText := combined.String()
		searchLine := lineText
		if caseInsensitive {
			searchLine = strings.ToLower(lineText)
		}

		idx := 0
		for {
			pos := strings.Index(searchLine[idx:], searchQuery)
			if pos < 0 {
				break
			}
			matchStart := idx + pos
			matchText := lineText[matchStart : matchStart+len(query)]

			x := line.texts[0].X
			fontSize := line.texts[0].FontSize

			results = append(results, TextSearchResult{
				PageIndex: pageIndex,
				X:         x,
				Y:         line.y,
				Width:     fontSize * float64(len(matchText)) * 0.5,
				Height:    fontSize,
				Text:      matchText,
				Context:   lineText,
			})
			idx = matchStart + len(query)
		}
	}

	return results
}
