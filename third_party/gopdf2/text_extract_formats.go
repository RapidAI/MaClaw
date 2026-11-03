package gopdf

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
)

// TextExtractionFormat specifies the output format for text extraction.
type TextExtractionFormat int

const (
	// FormatText returns plain text with line breaks.
	FormatText TextExtractionFormat = iota
	// FormatBlocks returns text grouped by blocks (paragraphs).
	FormatBlocks
	// FormatWords returns individual words with positions.
	FormatWords
	// FormatHTML returns text formatted as HTML.
	FormatHTML
	// FormatJSON returns structured JSON output.
	FormatJSON
)

// TextBlock represents a block of text (paragraph-level grouping).
type TextBlock struct {
	// X, Y are the top-left coordinates of the block.
	X, Y float64
	// Width, Height are the block dimensions.
	Width, Height float64
	// Lines contains the text lines in this block.
	Lines []TextLine
}

// TextLine represents a single line of text within a block.
type TextLine struct {
	// Y is the Y coordinate of this line.
	Y float64
	// Words contains the words on this line.
	Words []TextWord
	// Text is the concatenated text of all words.
	Text string
}

// TextWord represents a single word with position information.
type TextWord struct {
	// X, Y are the word position.
	X, Y float64
	// Width is the approximate word width.
	Width float64
	// Height is the word height (font size).
	Height float64
	// Text is the word content.
	Text string
	// FontName is the font used.
	FontName string
	// FontSize is the font size.
	FontSize float64
}

// textExtractionJSON is the JSON output structure.
type textExtractionJSON struct {
	PageIndex int             `json:"page_index"`
	Width     float64         `json:"width"`
	Height    float64         `json:"height"`
	Blocks    []textBlockJSON `json:"blocks"`
}

type textBlockJSON struct {
	X      float64        `json:"x"`
	Y      float64        `json:"y"`
	Width  float64        `json:"width"`
	Height float64        `json:"height"`
	Lines  []textLineJSON `json:"lines"`
}

type textLineJSON struct {
	Y     float64        `json:"y"`
	Text  string         `json:"text"`
	Words []textWordJSON `json:"words"`
}

type textWordJSON struct {
	X        float64 `json:"x"`
	Y        float64 `json:"y"`
	Width    float64 `json:"width"`
	Height   float64 `json:"height"`
	Text     string  `json:"text"`
	FontName string  `json:"font_name,omitempty"`
	FontSize float64 `json:"font_size"`
}

// ExtractTextFormatted extracts text from a page in the specified format.
//
// Supported formats:
//   - FormatText: plain text string
//   - FormatBlocks: []TextBlock
//   - FormatWords: []TextWord
//   - FormatHTML: HTML string
//   - FormatJSON: JSON string
//
// Example:
//
//	data, _ := os.ReadFile("input.pdf")
//	html, _ := gopdf.ExtractTextFormatted(data, 0, gopdf.FormatHTML)
//	fmt.Println(html.(string))
func ExtractTextFormatted(pdfData []byte, pageIndex int, format TextExtractionFormat) (interface{}, error) {
	// Parse once and reuse for both text extraction and page dimensions.
	parser, err := newRawPDFParser(pdfData)
	if err != nil {
		return nil, fmt.Errorf("parse PDF: %w", err)
	}

	texts, err := extractTextFromPageWithParser(parser, pdfData, pageIndex)
	if err != nil {
		return nil, err
	}

	switch format {
	case FormatText:
		return formatAsText(texts), nil
	case FormatBlocks:
		return formatAsBlocks(texts), nil
	case FormatWords:
		return formatAsWords(texts), nil
	case FormatHTML:
		return formatAsHTML(texts, pageIndex), nil
	case FormatJSON:
		return formatAsJSON(texts, pageIndex, parser)
	default:
		return nil, fmt.Errorf("unsupported format: %d", format)
	}
}

// extractTextFromPageWithParser extracts text using an already-parsed PDF.
func extractTextFromPageWithParser(parser *rawPDFParser, pdfData []byte, pageIndex int) ([]ExtractedText, error) {
	if pageIndex < 0 || pageIndex >= len(parser.pages) {
		return nil, fmt.Errorf("page index %d out of range (0-%d)", pageIndex, len(parser.pages)-1)
	}

	stream := parser.getPageContentStream(pageIndex)
	if len(stream) == 0 {
		return nil, nil
	}

	page := parser.pages[pageIndex]
	fonts := buildFontMap(parser, page)
	return parseTextOperatorsCtx(&extractionContext{
		parser:   parser,
		fonts:    fonts,
		xobjs:    page.resources.xobjs,
		mediaBox: page.mediaBox,
		visited:  map[int]bool{},
	}, stream), nil
}

// formatAsText converts extracted text items to a plain text string.
func formatAsText(texts []ExtractedText) string {
	if len(texts) == 0 {
		return ""
	}
	var sb strings.Builder
	lastY := math.MaxFloat64
	for _, t := range texts {
		if math.Abs(t.Y-lastY) > 2 && lastY != math.MaxFloat64 {
			sb.WriteByte('\n')
		} else if sb.Len() > 0 && lastY != math.MaxFloat64 {
			sb.WriteByte(' ')
		}
		sb.WriteString(t.Text)
		lastY = t.Y
	}
	return sb.String()
}

// formatAsBlocks groups text items into blocks (paragraphs).
func formatAsBlocks(texts []ExtractedText) []TextBlock {
	if len(texts) == 0 {
		return nil
	}

	// Group by Y coordinate into lines.
	lines := groupIntoLines(texts)

	// Group lines into blocks based on vertical spacing.
	var blocks []TextBlock
	var currentBlock *TextBlock

	for i, line := range lines {
		if len(line.Words) == 0 {
			continue
		}
		if currentBlock == nil || (i > 0 && len(lines[i-1].Words) > 0 &&
			math.Abs(line.Y-lines[i-1].Y) > lines[i-1].Words[0].Height*2) {
			// Start new block.
			blocks = append(blocks, TextBlock{
				X: line.Words[0].X,
				Y: line.Y,
			})
			currentBlock = &blocks[len(blocks)-1]
		}
		currentBlock.Lines = append(currentBlock.Lines, line)
	}

	// Compute block dimensions.
	for i := range blocks {
		b := &blocks[i]
		if len(b.Lines) == 0 {
			continue
		}
		minX := math.MaxFloat64
		maxX := -math.MaxFloat64
		lastLineHeight := 0.0
		for _, line := range b.Lines {
			for _, w := range line.Words {
				if w.X < minX {
					minX = w.X
				}
				if w.X+w.Width > maxX {
					maxX = w.X + w.Width
				}
			}
		}
		b.X = minX
		if minX == math.MaxFloat64 {
			b.X = 0
		}
		b.Width = maxX - b.X
		// Use the last line's first word height for block height calculation.
		lastLine := b.Lines[len(b.Lines)-1]
		if len(lastLine.Words) > 0 {
			lastLineHeight = lastLine.Words[0].Height
		}
		b.Height = lastLine.Y - b.Y + lastLineHeight
	}

	return blocks
}

// formatAsWords extracts individual words with positions.
func formatAsWords(texts []ExtractedText) []TextWord {
	var words []TextWord
	for _, t := range texts {
		// Split text into individual words.
		parts := strings.Fields(t.Text)
		x := t.X
		for _, part := range parts {
			runeCount := len([]rune(part))
			w := TextWord{
				X:        x,
				Y:        t.Y,
				Width:    t.FontSize * float64(runeCount) * 0.5,
				Height:   t.FontSize,
				Text:     part,
				FontName: t.FontName,
				FontSize: t.FontSize,
			}
			words = append(words, w)
			x += w.Width + t.FontSize*0.25 // approximate word spacing
		}
	}
	return words
}

// formatAsHTML converts extracted text to an HTML representation.
func formatAsHTML(texts []ExtractedText, pageIndex int) string {
	if len(texts) == 0 {
		return "<div></div>"
	}

	lines := groupIntoLines(texts)

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("<div class=\"page\" data-page=\"%d\">\n", pageIndex))

	for _, line := range lines {
		if len(line.Words) == 0 {
			continue
		}
		sb.WriteString(fmt.Sprintf("  <p style=\"position:absolute;top:%.1fpx;left:%.1fpx;\">",
			line.Y, line.Words[0].X))

		for i, w := range line.Words {
			if i > 0 {
				sb.WriteByte(' ')
			}
			sb.WriteString(fmt.Sprintf("<span style=\"font-size:%.1fpx;", w.FontSize))
			if w.FontName != "" {
				sb.WriteString(fmt.Sprintf("font-family:'%s';", htmlEscapeAttr(w.FontName)))
			}
			sb.WriteString("\">")
			sb.WriteString(htmlEscapeText(w.Text))
			sb.WriteString("</span>")
		}

		sb.WriteString("</p>\n")
	}

	sb.WriteString("</div>")
	return sb.String()
}

// formatAsJSON converts extracted text to a structured JSON string.
func formatAsJSON(texts []ExtractedText, pageIndex int, parser *rawPDFParser) (string, error) {
	blocks := formatAsBlocks(texts)

	output := textExtractionJSON{
		PageIndex: pageIndex,
		Blocks:    make([]textBlockJSON, 0, len(blocks)),
	}

	// Get page dimensions from the already-parsed PDF.
	if pageIndex >= 0 && pageIndex < len(parser.pages) {
		mb := parser.pages[pageIndex].mediaBox
		output.Width = mb[2] - mb[0]
		output.Height = mb[3] - mb[1]
	}

	for _, b := range blocks {
		bj := textBlockJSON{
			X:      b.X,
			Y:      b.Y,
			Width:  b.Width,
			Height: b.Height,
			Lines:  make([]textLineJSON, 0, len(b.Lines)),
		}
		for _, line := range b.Lines {
			lj := textLineJSON{
				Y:     line.Y,
				Text:  line.Text,
				Words: make([]textWordJSON, 0, len(line.Words)),
			}
			for _, w := range line.Words {
				lj.Words = append(lj.Words, textWordJSON{
					X:        w.X,
					Y:        w.Y,
					Width:    w.Width,
					Height:   w.Height,
					Text:     w.Text,
					FontName: w.FontName,
					FontSize: w.FontSize,
				})
			}
			bj.Lines = append(bj.Lines, lj)
		}
		output.Blocks = append(output.Blocks, bj)
	}

	data, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// groupIntoLines groups ExtractedText items into lines by Y coordinate.
func groupIntoLines(texts []ExtractedText) []TextLine {
	if len(texts) == 0 {
		return nil
	}

	// Sort by Y first for efficient grouping.
	sorted := make([]ExtractedText, len(texts))
	copy(sorted, texts)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Y < sorted[j].Y
	})

	type lineGroup struct {
		y     float64
		items []ExtractedText
	}
	var groups []lineGroup

	// Since sorted by Y, we only need to compare with the last group.
	for _, t := range sorted {
		if len(groups) > 0 && math.Abs(groups[len(groups)-1].y-t.Y) < 2 {
			groups[len(groups)-1].items = append(groups[len(groups)-1].items, t)
		} else {
			groups = append(groups, lineGroup{y: t.Y, items: []ExtractedText{t}})
		}
	}

	lines := make([]TextLine, 0, len(groups))
	for _, g := range groups {
		// Sort items by X within each line.
		sort.Slice(g.items, func(i, j int) bool {
			return g.items[i].X < g.items[j].X
		})

		line := TextLine{Y: g.y}
		var textParts []string
		for _, item := range g.items {
			words := strings.Fields(item.Text)
			for _, word := range words {
				runeCount := len([]rune(word))
				line.Words = append(line.Words, TextWord{
					X:        item.X,
					Y:        item.Y,
					Width:    item.FontSize * float64(runeCount) * 0.5,
					Height:   item.FontSize,
					Text:     word,
					FontName: item.FontName,
					FontSize: item.FontSize,
				})
			}
			textParts = append(textParts, item.Text)
		}
		line.Text = strings.Join(textParts, " ")
		lines = append(lines, line)
	}

	return lines
}

// htmlEscapeText escapes text for HTML content.
func htmlEscapeText(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}

// htmlEscapeAttr escapes text for HTML attributes.
func htmlEscapeAttr(s string) string {
	s = htmlEscapeText(s)
	s = strings.ReplaceAll(s, "'", "&#39;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	return s
}
