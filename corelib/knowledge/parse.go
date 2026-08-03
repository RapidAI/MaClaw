package knowledge

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"

	excelread "github.com/RapidAI/CodeClaw/corelib/excel"
	"github.com/RapidAI/CodeClaw/corelib/pptx"
	gopdf2 "github.com/VantageDataChat/GoPDF2"
)

var errUnsupportedParser = errors.New("knowledge parser is not available for this file type")

const maxNodeTextRunes = 1_000_000
const targetTextNodeRunes = 6000
const targetSheetNodeRunes = 8000

func ParseDocumentNodes(source Source, filePath, kind string) ([]DocumentNode, error) {
	var nodes []DocumentNode
	var err error
	switch kind {
	case SourceKindMarkdown:
		data, err := os.ReadFile(filePath)
		if err != nil {
			return nil, err
		}
		nodes = parseMarkdownNodes(source, normalizeKnowledgeText(string(data)))
	case SourceKindText:
		data, err := os.ReadFile(filePath)
		if err != nil {
			return nil, err
		}
		nodes = parsePlainTextNodes(source, normalizeKnowledgeText(string(data)), "text")
	case SourceKindDOCX:
		nodes, err = parseDOCXNodes(source, filePath)
	case SourceKindPDF:
		nodes, err = parsePDFNodes(source, filePath)
	case SourceKindPPTX:
		nodes, err = parsePPTXNodes(source, filePath)
	case SourceKindXLSX, SourceKindCSV:
		nodes, err = parseSpreadsheetNodes(source, filePath, kind)
	case SourceKindDOC:
		nodes, err = parseDOCFallbackNodes(source, filePath)
	case SourceKindXLS:
		nodes, err = parseXLSTextFallback(source, filePath)
	case SourceKindImage:
		// Standalone images have no text nodes from parsing.
		// Image processing (asset save + description) is handled by
		// ProcessStandaloneImage in the import pipeline.
		return nil, nil
	default:
		return nil, fmt.Errorf("%w: %s", errUnsupportedParser, kind)
	}
	if err != nil {
		return nil, err
	}
	return annotateMultilingualNodeMetadata(nodes), nil
}

func IsUnsupportedParserError(err error) bool {
	return errors.Is(err, errUnsupportedParser)
}

type markdownSection struct {
	id       string
	parentID string
	title    string
	level    int
	offset   int
	lines    []string
}

func parseMarkdownNodes(source Source, text string) []DocumentNode {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	lines := strings.Split(text, "\n")
	sections := make([]markdownSection, 0)
	var current *markdownSection
	stack := make(map[int]string)
	inFence := false

	flush := func() {
		if current == nil {
			return
		}
		section := *current
		sections = append(sections, section)
		current = nil
	}
	startSection := func(title string, level, offset int) {
		flush()
		if level <= 0 {
			level = 1
		}
		id := NewID("kdn")
		parentID := ""
		for parentLevel := level - 1; parentLevel >= 1; parentLevel-- {
			if candidate := stack[parentLevel]; candidate != "" {
				parentID = candidate
				break
			}
		}
		stack[level] = id
		for childLevel := level + 1; childLevel <= 6; childLevel++ {
			delete(stack, childLevel)
		}
		current = &markdownSection{id: id, parentID: parentID, title: title, level: level, offset: offset}
	}

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inFence = !inFence
		}
		if !inFence {
			if level, title, ok := markdownHeading(trimmed); ok {
				startSection(title, level, i+1)
				continue
			}
		}
		if current == nil {
			current = &markdownSection{id: NewID("kdn"), title: source.Title, level: 0, offset: i + 1}
		}
		current.lines = append(current.lines, line)
	}
	flush()

	nodes := make([]DocumentNode, 0, len(sections))
	for _, section := range sections {
		body := strings.TrimSpace(strings.Join(section.lines, "\n"))
		if body == "" {
			body = section.title
		} else if section.title != "" && !strings.HasPrefix(strings.TrimSpace(body), section.title) {
			body = section.title + "\n" + body
		}
		body = trimNodeText(body)
		if body == "" {
			continue
		}
		nodeType := "section"
		if section.level == 0 {
			nodeType = "document"
		}
		nodes = append(nodes, DocumentNode{
			ID:       section.id,
			SourceID: source.ID,
			ParentID: section.parentID,
			Type:     nodeType,
			Title:    fallbackText(section.title, source.Title),
			Text:     body,
			Level:    section.level,
			Offset:   section.offset,
			Metadata: map[string]string{
				"relative_path": source.RelativePath,
				"format":        "markdown",
				"line_start":    fmt.Sprint(section.offset),
			},
			TokenCount: estimateTokens(body),
		})
	}
	if len(nodes) == 0 {
		return []DocumentNode{simpleTextNode(source, trimNodeText(text))}
	}
	return nodes
}

func markdownHeading(trimmed string) (int, string, bool) {
	if !strings.HasPrefix(trimmed, "#") {
		return 0, "", false
	}
	level := 0
	for level < len(trimmed) && level < 6 && trimmed[level] == '#' {
		level++
	}
	if level == 0 || level >= len(trimmed) || trimmed[level] != ' ' {
		return 0, "", false
	}
	title := strings.TrimSpace(trimmed[level:])
	title = strings.TrimSpace(strings.TrimRight(title, "#"))
	if title == "" {
		return 0, "", false
	}
	return level, title, true
}

func parsePlainTextNodes(source Source, text, format string) []DocumentNode {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	paragraphs := splitParagraphs(text)
	if len(paragraphs) == 0 {
		return []DocumentNode{simpleTextNode(source, "")}
	}
	return nodesFromParagraphs(source, paragraphs, format)
}

func splitParagraphs(text string) []string {
	blocks := strings.Split(text, "\n\n")
	paragraphs := make([]string, 0, len(blocks))
	for _, block := range blocks {
		block = strings.TrimSpace(block)
		if block == "" {
			continue
		}
		paragraphs = append(paragraphs, block)
	}
	if len(paragraphs) == 0 && strings.TrimSpace(text) != "" {
		paragraphs = []string{strings.TrimSpace(text)}
	}
	return paragraphs
}

func nodesFromParagraphs(source Source, paragraphs []string, format string) []DocumentNode {
	nodes := make([]DocumentNode, 0)
	var current []string
	currentRunes := 0
	startOffset := 1
	flush := func(nextOffset int) {
		if len(current) == 0 {
			return
		}
		body := trimNodeText(strings.Join(current, "\n\n"))
		if strings.TrimSpace(body) == "" {
			current = nil
			currentRunes = 0
			startOffset = nextOffset
			return
		}
		title := source.Title
		if len(nodes) > 0 || nextOffset <= len(paragraphs) {
			title = fmt.Sprintf("%s part %d", fallbackText(source.Title, source.RelativePath), len(nodes)+1)
		}
		nodes = append(nodes, DocumentNode{
			ID:       NewID("kdn"),
			SourceID: source.ID,
			Type:     "document",
			Title:    title,
			Text:     body,
			Offset:   startOffset,
			Metadata: map[string]string{
				"relative_path":   source.RelativePath,
				"format":          format,
				"paragraph_start": fmt.Sprint(startOffset),
				"paragraph_end":   fmt.Sprint(nextOffset - 1),
			},
			TokenCount: estimateTokens(body),
		})
		current = nil
		currentRunes = 0
		startOffset = nextOffset
	}
	for i, paragraph := range paragraphs {
		paragraph = strings.TrimSpace(paragraph)
		if paragraph == "" {
			continue
		}
		paragraphRunes := len([]rune(paragraph))
		if len(current) > 0 && currentRunes+paragraphRunes > targetTextNodeRunes {
			flush(i + 1)
		}
		if len(current) == 0 {
			startOffset = i + 1
		}
		current = append(current, paragraph)
		currentRunes += paragraphRunes
	}
	flush(len(paragraphs) + 1)
	if len(nodes) == 0 {
		return []DocumentNode{simpleTextNode(source, "")}
	}
	if len(nodes) == 1 {
		nodes[0].Title = source.Title
	}
	return nodes
}

func parseDOCXNodes(source Source, filePath string) ([]DocumentNode, error) {
	r, err := zip.OpenReader(filePath)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	var documentXML []byte
	for _, f := range r.File {
		if f.Name != "word/document.xml" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		documentXML, err = io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			return nil, err
		}
		break
	}
	if len(documentXML) == 0 {
		return nil, fmt.Errorf("docx document.xml not found")
	}
	paragraphs, err := docxParagraphs(documentXML)
	if err != nil {
		return nil, err
	}
	text := trimNodeText(strings.Join(paragraphs, "\n"))
	if strings.TrimSpace(text) == "" {
		return nil, fmt.Errorf("docx has no readable text")
	}
	return nodesFromParagraphs(source, paragraphs, "docx"), nil
}

func docxParagraphs(data []byte) ([]string, error) {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	var paragraphs []string
	var paragraph strings.Builder
	inParagraph := false
	for {
		tok, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "p":
				if inParagraph {
					flushParagraph(&paragraphs, &paragraph)
				}
				inParagraph = true
			case "tab":
				if inParagraph {
					paragraph.WriteString("\t")
				}
			case "br", "cr":
				if inParagraph {
					paragraph.WriteString("\n")
				}
			}
		case xml.EndElement:
			if t.Name.Local == "p" && inParagraph {
				flushParagraph(&paragraphs, &paragraph)
				inParagraph = false
			}
		case xml.CharData:
			if inParagraph {
				paragraph.Write([]byte(t))
			}
		}
	}
	flushParagraph(&paragraphs, &paragraph)
	return paragraphs, nil
}

func flushParagraph(paragraphs *[]string, paragraph *strings.Builder) {
	text := strings.TrimSpace(paragraph.String())
	paragraph.Reset()
	if text != "" {
		*paragraphs = append(*paragraphs, text)
	}
}

func parsePPTXNodes(source Source, filePath string) ([]DocumentNode, error) {
	pres, err := pptx.Read(filePath)
	if err != nil {
		return nil, err
	}

	// Parallel per-slide text extraction for large decks (CPU-bound shape walks).
	type slidePack struct {
		number int
		texts  []string
		notes  string
	}
	n := len(pres.Slides)
	packs := make([]slidePack, n)
	if n >= 4 {
		var wg sync.WaitGroup
		workers := runtime.NumCPU()
		if workers > 8 {
			workers = 8
		}
		if workers > n {
			workers = n
		}
		if workers < 1 {
			workers = 1
		}
		ch := make(chan int, n)
		for i := 0; i < n; i++ {
			ch <- i
		}
		close(ch)
		for w := 0; w < workers; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := range ch {
					slide := pres.Slides[i]
					var slideTexts []string
					for _, shape := range slide.Shapes {
						if text := pptxShapeText(shape); text != "" {
							slideTexts = append(slideTexts, text)
						}
					}
					packs[i] = slidePack{number: slide.Number, texts: slideTexts, notes: slide.Notes}
				}
			}()
		}
		wg.Wait()
	} else {
		for i, slide := range pres.Slides {
			var slideTexts []string
			for _, shape := range slide.Shapes {
				if text := pptxShapeText(shape); text != "" {
					slideTexts = append(slideTexts, text)
				}
			}
			packs[i] = slidePack{number: slide.Number, texts: slideTexts, notes: slide.Notes}
		}
	}

	var paragraphs []string
	for _, pack := range packs {
		if len(pack.texts) > 0 {
			header := fmt.Sprintf("--- Slide %d ---", pack.number)
			paragraphs = append(paragraphs, header)
			paragraphs = append(paragraphs, pack.texts...)
		}
		if pack.notes != "" {
			paragraphs = append(paragraphs, "[Notes] "+strings.TrimSpace(pack.notes))
		}
	}

	if len(paragraphs) == 0 {
		return nil, fmt.Errorf("pptx has no readable text")
	}

	return nodesFromParagraphs(source, paragraphs, "pptx"), nil
}

// pptxShapeText extracts plain text from a PPTX shape.
func pptxShapeText(shape pptx.Shape) string {
	switch shape.Type {
	case pptx.ShapeTypeText:
		if shape.Text == nil {
			return ""
		}
		var lines []string
		for _, para := range shape.Text.Paragraphs {
			var sb strings.Builder
			for _, run := range para.Runs {
				sb.WriteString(run.Text)
			}
			if sb.Len() > 0 {
				lines = append(lines, sb.String())
			}
		}
		return strings.Join(lines, "\n")
	case pptx.ShapeTypeTable:
		if shape.Table == nil {
			return ""
		}
		var rows []string
		for _, row := range shape.Table.Rows {
			var cells []string
			for _, cell := range row.Cells {
				cells = append(cells, cell.Text)
			}
			rows = append(rows, strings.Join(cells, " | "))
		}
		return strings.Join(rows, "\n")
	case pptx.ShapeTypeChart:
		if shape.Chart == nil {
			return ""
		}
		var parts []string
		if shape.Chart.ChartType != "" {
			parts = append(parts, "[Chart: "+shape.Chart.ChartType+"]")
		}
		for _, ds := range shape.Chart.DataSeries {
			if ds.Label != "" {
				parts = append(parts, ds.Label)
			}
		}
		return strings.Join(parts, " ")
	default:
		return ""
	}
}

func parsePDFNodes(source Source, filePath string) ([]DocumentNode, error) {
	pdfData, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	nodes := make([]DocumentNode, 0)
	// Determine number of pages
	numPages, err := gopdf2.GetSourcePDFPageCountFromBytes(pdfData)
	if err != nil {
		// Fallback: try extracting all text as single page
		allText, err2 := gopdf2.ExtractAllPagesText(pdfData)
		if err2 != nil {
			return nil, fmt.Errorf("pdf parse failed: %w (fallback: %v)", err, err2)
		}
		allText = strings.TrimSpace(trimNodeText(allText))
		if allText == "" {
			return nil, fmt.Errorf("pdf has no readable text")
		}
		nodes = append(nodes, DocumentNode{
			ID:         NewID("kdn"),
			SourceID:   source.ID,
			Type:       "page",
			Title:      fmt.Sprintf("%s p.1", source.Title),
			Text:       allText,
			Page:       1,
			Metadata:   map[string]string{"relative_path": source.RelativePath, "format": "pdf"},
			TokenCount: estimateTokens(allText),
		})
		return nodes, nil
	}

	// Extract page text (parallel with panic-safe sequential fallback).
	pageTexts := extractPDFPageTexts(pdfData, numPages)

	// Segment + node construction (order-preserving).
	for i := 0; i < numPages; i++ {
		text := pageTexts[i]
		if text == "" {
			continue
		}
		// For long pages (e.g. publications lists in resumes), split into
		// sub-segments so that each item can become an independent card with
		// its own Claim, improving FTS recall for individual entries.
		segments := splitPDFPageIntoSegments(text)
		for segIdx, seg := range segments {
			seg = strings.TrimSpace(seg)
			if seg == "" {
				continue
			}
			title := fmt.Sprintf("%s p.%d", source.Title, i+1)
			if len(segments) > 1 {
				title = fmt.Sprintf("%s p.%d.%d", source.Title, i+1, segIdx+1)
			}
			nodes = append(nodes, DocumentNode{
				ID:         NewID("kdn"),
				SourceID:   source.ID,
				Type:       "page",
				Title:      title,
				Text:       seg,
				Page:       i + 1,
				Metadata:   map[string]string{"relative_path": source.RelativePath, "format": "pdf"},
				TokenCount: estimateTokens(seg),
			})
		}
	}
	if len(nodes) == 0 {
		return nil, fmt.Errorf("pdf has no readable text")
	}
	return nodes, nil
}

// extractPDFPageTexts pulls text for each page. Multi-page PDFs use a worker
// pool; if any worker panics (non-thread-safe parser edge cases), the whole
// extraction is retried sequentially.
func extractPDFPageTexts(pdfData []byte, numPages int) []string {
	if numPages <= 0 {
		return nil
	}
	if numPages == 1 {
		text, err := gopdf2.ExtractPageText(pdfData, 0)
		if err != nil {
			return []string{""}
		}
		return []string{strings.TrimSpace(trimNodeText(text))}
	}

	pageTexts := make([]string, numPages)
	workers := runtime.NumCPU()
	if workers < 1 {
		workers = 1
	}
	if workers > 8 {
		workers = 8
	}
	if workers > numPages {
		workers = numPages
	}

	var wg sync.WaitGroup
	var panicked atomic.Bool
	pageCh := make(chan int, numPages)
	for p := 0; p < numPages; p++ {
		pageCh <- p
	}
	close(pageCh)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() {
				if rec := recover(); rec != nil {
					panicked.Store(true)
				}
			}()
			for i := range pageCh {
				if panicked.Load() {
					return
				}
				text, err := gopdf2.ExtractPageText(pdfData, i)
				if err != nil {
					continue
				}
				pageTexts[i] = strings.TrimSpace(trimNodeText(text))
			}
		}()
	}
	wg.Wait()

	if !panicked.Load() {
		return pageTexts
	}
	// Sequential fallback after parallel panic.
	out := make([]string, numPages)
	for i := 0; i < numPages; i++ {
		text, err := gopdf2.ExtractPageText(pdfData, i)
		if err != nil {
			continue
		}
		out[i] = strings.TrimSpace(trimNodeText(text))
	}
	return out
}

// splitPDFPageIntoSegments splits a long PDF page text into smaller segments.
// Short pages (< 2000 runes) are returned as-is. Long pages are split at
// paragraph boundaries (double newlines) or, if the text looks like a list
// (numbered/bulleted items, academic citation patterns), at list-item boundaries.
func splitPDFPageIntoSegments(text string) []string {
	const minSegmentRunes = 2000
	if len([]rune(text)) < minSegmentRunes {
		return []string{text}
	}

	// Try splitting by double-newline paragraphs first.
	paragraphs := strings.Split(text, "\n\n")
	if len(paragraphs) >= 3 {
		merged := mergePDFParagraphs(paragraphs, targetTextNodeRunes)
		// Post-check: if any merged segment still looks like a list and is long,
		// sub-split it by list items for finer-grained cards.
		return refineLargeListSegments(merged)
	}

	// If no double-newline structure, try splitting by single newlines
	// when lines look like list items (academic papers, numbered entries).
	lines := strings.Split(text, "\n")
	if looksLikeListContent(lines) {
		return splitListIntoChunks(lines, targetTextNodeRunes)
	}

	// Fallback: return as single segment.
	return []string{text}
}

// refineLargeListSegments checks each segment and further splits segments that
// are both long (> 2000 runes) and look like list content.
func refineLargeListSegments(segments []string) []string {
	const refinementThreshold = 2000
	result := make([]string, 0, len(segments))
	for _, seg := range segments {
		if len([]rune(seg)) <= refinementThreshold {
			result = append(result, seg)
			continue
		}
		lines := strings.Split(seg, "\n")
		if looksLikeListContent(lines) {
			subSegments := splitListIntoChunks(lines, targetTextNodeRunes)
			result = append(result, subSegments...)
		} else {
			result = append(result, seg)
		}
	}
	return result
}

// looksLikeListContent checks if the majority of lines look like list items
// (numbered entries, bullet points, or academic citation patterns like "[J]", "et al.").
func looksLikeListContent(lines []string) bool {
	if len(lines) < 4 {
		return false
	}
	listLikeCount := 0
	nonEmpty := 0
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		nonEmpty++
		if isListItemLine(line) {
			listLikeCount++
		}
	}
	if nonEmpty == 0 {
		return false
	}
	return float64(listLikeCount)/float64(nonEmpty) >= 0.4
}

var listItemPatterns = regexp.MustCompile(`^(?:\d+[\.\)）]\s|[-•·]\s|\[\d+\]\s|[A-Z]\.\s)`)
var academicCitationHints = []string{"[J]", "[C]", "[M]", "[D]", "[P]", "et al.", "et al,", "doi:", "DOI:", "pp.", "vol.", "Vol."}

func isListItemLine(line string) bool {
	if listItemPatterns.MatchString(line) {
		return true
	}
	for _, hint := range academicCitationHints {
		if strings.Contains(line, hint) {
			return true
		}
	}
	return false
}

// mergePDFParagraphs merges short paragraphs into segments of roughly targetRunes each.
func mergePDFParagraphs(paragraphs []string, targetRunes int) []string {
	var segments []string
	var current strings.Builder
	currentRunes := 0

	for _, para := range paragraphs {
		para = strings.TrimSpace(para)
		if para == "" {
			continue
		}
		paraRunes := len([]rune(para))
		if currentRunes > 0 && currentRunes+paraRunes > targetRunes {
			segments = append(segments, current.String())
			current.Reset()
			currentRunes = 0
		}
		if current.Len() > 0 {
			current.WriteString("\n\n")
		}
		current.WriteString(para)
		currentRunes += paraRunes
	}
	if current.Len() > 0 {
		segments = append(segments, current.String())
	}
	if len(segments) == 0 {
		return []string{strings.Join(paragraphs, "\n\n")}
	}
	return segments
}

// splitListIntoChunks groups list-like lines into chunks of roughly targetRunes each.
func splitListIntoChunks(lines []string, targetRunes int) []string {
	var segments []string
	var current strings.Builder
	currentRunes := 0

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			if current.Len() > 0 {
				current.WriteString("\n")
			}
			continue
		}
		lineRunes := len([]rune(line))
		if currentRunes > 0 && currentRunes+lineRunes > targetRunes {
			segments = append(segments, current.String())
			current.Reset()
			currentRunes = 0
		}
		if current.Len() > 0 {
			current.WriteString("\n")
		}
		current.WriteString(line)
		currentRunes += lineRunes
	}
	if current.Len() > 0 {
		segments = append(segments, current.String())
	}
	if len(segments) == 0 {
		return []string{strings.Join(lines, "\n")}
	}
	return segments
}

func parseSpreadsheetNodes(source Source, filePath string, kind string) ([]DocumentNode, error) {
	sheets, err := excelread.ListSheets(filePath)
	if err != nil {
		return nil, err
	}
	if len(sheets) == 0 {
		return nil, fmt.Errorf("%s has no sheets", kind)
	}
	nodes := make([]DocumentNode, 0, len(sheets))
	for _, sheet := range sheets {
		result, err := excelread.ReadFile(filePath, excelread.ReadOptions{SheetName: sheet})
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, sheetToNodes(source, result, kind)...)
	}
	if len(nodes) == 0 {
		return nil, fmt.Errorf("%s has no readable text", kind)
	}
	return nodes, nil
}

func sheetToNodes(source Source, result *excelread.ReadResult, format string) []DocumentNode {
	if result == nil || len(result.Rows) == 0 {
		return nil
	}
	sheetName := fallbackText(result.SheetName, source.Title)
	nodes := make([]DocumentNode, 0)
	var current []string
	currentRunes := 0
	startRow := 1
	flush := func(endRow int) {
		if len(current) == 0 {
			return
		}
		text := trimNodeText(strings.Join(current, "\n"))
		if strings.TrimSpace(text) == "" {
			current = nil
			currentRunes = 0
			startRow = endRow + 1
			return
		}
		title := sheetName
		if len(nodes) > 0 || endRow < len(result.Rows) {
			title = fmt.Sprintf("%s rows %d-%d", sheetName, startRow, endRow)
		}
		nodes = append(nodes, DocumentNode{
			ID:        NewID("kdn"),
			SourceID:  source.ID,
			Type:      "sheet",
			Title:     title,
			Text:      text,
			SheetName: sheetName,
			RowRange:  fmt.Sprintf("%d:%d", startRow, endRow),
			Offset:    startRow,
			Metadata: map[string]string{
				"relative_path": source.RelativePath,
				"format":        format,
				"sheet_name":    sheetName,
				"row_start":     fmt.Sprint(startRow),
				"row_end":       fmt.Sprint(endRow),
			},
			TokenCount: estimateTokens(text),
		})
		current = nil
		currentRunes = 0
		startRow = endRow + 1
	}
	for i, row := range result.Rows {
		line := rowToText(row)
		if strings.TrimSpace(line) == "" {
			continue
		}
		lineRunes := len([]rune(line))
		if len(current) > 0 && currentRunes+lineRunes > targetSheetNodeRunes {
			flush(i)
		}
		if len(current) == 0 {
			startRow = i + 1
		}
		current = append(current, line)
		currentRunes += lineRunes
	}
	flush(len(result.Rows))
	return nodes
}

func sheetToText(result *excelread.ReadResult) string {
	if result == nil || len(result.Rows) == 0 {
		return ""
	}
	var sb strings.Builder
	for _, row := range result.Rows {
		line := rowToText(row)
		if strings.TrimSpace(line) != "" {
			sb.WriteString(line)
			sb.WriteString("\n")
		}
		if len([]rune(sb.String())) >= maxNodeTextRunes {
			break
		}
	}
	return trimNodeText(sb.String())
}

func rowToText(row []excelread.CellValue) string {
	values := make([]string, 0, len(row))
	for _, cell := range row {
		if cell.Value == nil {
			values = append(values, "")
			continue
		}
		values = append(values, strings.TrimSpace(fmt.Sprint(cell.Value)))
	}
	return strings.TrimRight(strings.Join(values, "\t"), "\t")
}

func trimNodeText(text string) string {
	text = strings.TrimSpace(text)
	if len([]rune(text)) <= maxNodeTextRunes {
		return text
	}
	runes := []rune(text)
	return strings.TrimSpace(string(runes[:maxNodeTextRunes]))
}
