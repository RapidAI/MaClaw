package gopdf

import (
	"strings"
)

// SearchOptions tunes text search behaviour.
type SearchOptions struct {
	// CaseInsensitive folds case before matching.
	CaseInsensitive bool
	// Dehyphenate joins hyphenated line wraps before matching, so
	// "transduc-" + "tion" matches "transduction" (PyMuPDF's
	// search_for(dehyphenate=true)). Matches spanning lines report the
	// rectangle of the line where the match starts.
	Dehyphenate bool
}

// SearchTextWithOptions searches all pages with the given options.
func SearchTextWithOptions(pdfData []byte, query string, opt SearchOptions) ([]TextSearchResult, error) {
	parser, err := newRawPDFParser(pdfData)
	if err != nil {
		return nil, err
	}
	var results []TextSearchResult
	for i := range parser.pages {
		var pageResults []TextSearchResult
		if opt.Dehyphenate {
			pageResults = searchDehyphenated(parser, i, query, opt.CaseInsensitive)
		} else {
			pageResults, err = searchTextOnPage(parser, i, query, opt.CaseInsensitive)
		}
		if err != nil {
			continue
		}
		results = append(results, pageResults...)
	}
	return results, nil
}

// SearchTextOnPageWithOptions searches one page (0-based) with options.
func SearchTextOnPageWithOptions(pdfData []byte, pageIndex int, query string, opt SearchOptions) ([]TextSearchResult, error) {
	parser, err := newRawPDFParser(pdfData)
	if err != nil {
		return nil, err
	}
	if opt.Dehyphenate {
		return searchDehyphenated(parser, pageIndex, query, opt.CaseInsensitive), nil
	}
	return searchTextOnPage(parser, pageIndex, query, opt.CaseInsensitive)
}

// searchDehyphenated searches paragraph-joined text so matches can span
// hyphenated line wraps. A match is reported at the line that contains its
// first character.
func searchDehyphenated(parser *rawPDFParser, pageIndex int, query string, caseInsensitive bool) []TextSearchResult {
	if pageIndex < 0 || pageIndex >= len(parser.pages) {
		return nil
	}
	page := parser.pages[pageIndex]
	stream := parser.getPageContentStream(pageIndex)
	if len(stream) == 0 {
		return nil
	}
	fonts := buildFontMap(parser, page)
	texts := parseTextOperatorsCtx(&extractionContext{
		parser:   parser,
		fonts:    fonts,
		xobjs:    page.resources.xobjs,
		mediaBox: page.mediaBox,
		rotate:   page.rotate,
		visited:  map[int]bool{},
	}, stream)
	if len(texts) == 0 {
		return nil
	}

	lines := GroupIntoTextLines(texts)
	paras := ParagraphsFromLines(lines)
	if len(paras) == 0 {
		return nil
	}

	needle := query
	if caseInsensitive {
		needle = strings.ToLower(needle)
	}

	var results []TextSearchResult
	for pi := range paras {
		p := &paras[pi]
		hay := p.Text
		if caseInsensitive {
			hay = strings.ToLower(hay)
		}
		idx := 0
		for {
			pos := strings.Index(hay[idx:], needle)
			if pos < 0 {
				break
			}
			matchStart := idx + pos
			// map the character offset back to a line
			off := 0
			var hitLine *TextLine
			for li := range p.Lines {
				ln := &p.Lines[li]
				if matchStart <= off+len(ln.Text) {
					hitLine = ln
					break
				}
				off += len(ln.Text) + 1 // +1 for the joining space
			}
			res := TextSearchResult{PageIndex: pageIndex, Text: query, Context: p.Text}
			if hitLine != nil && len(hitLine.Words) > 0 {
				res.X = hitLine.Words[0].X
				res.Y = hitLine.Words[0].Y - hitLine.Words[0].Height
				res.Height = hitLine.Words[0].Height
				w := lineWidthFromWords(hitLine.Words)
				res.Width = w
			}
			results = append(results, res)
			idx = matchStart + len(needle)
		}
	}
	return results
}

func lineWidthFromWords(words []TextWord) float64 {
	if len(words) == 0 {
		return 0
	}
	first, last := words[0], words[len(words)-1]
	w := last.X + last.Width - first.X
	if w < 0 {
		w = 0
	}
	return w
}
