package knowledge

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	excelread "github.com/RapidAI/CodeClaw/corelib/excel"
	"github.com/RapidAI/CodeClaw/corelib/pptx"
	"github.com/ledongthuc/pdf"
)

var errUnsupportedParser = errors.New("knowledge parser is not available for this file type")

const maxNodeTextRunes = 1_000_000
const targetTextNodeRunes = 6000
const targetSheetNodeRunes = 8000

func ParseDocumentNodes(source Source, filePath, kind string) ([]DocumentNode, error) {
	switch kind {
	case SourceKindMarkdown:
		data, err := os.ReadFile(filePath)
		if err != nil {
			return nil, err
		}
		return parseMarkdownNodes(source, string(data)), nil
	case SourceKindText:
		data, err := os.ReadFile(filePath)
		if err != nil {
			return nil, err
		}
		return parsePlainTextNodes(source, string(data), "text"), nil
	case SourceKindDOCX:
		return parseDOCXNodes(source, filePath)
	case SourceKindPDF:
		return parsePDFNodes(source, filePath)
	case SourceKindPPTX:
		return parsePPTXNodes(source, filePath)
	case SourceKindXLSX, SourceKindCSV:
		return parseSpreadsheetNodes(source, filePath, kind)
	case SourceKindDOC:
		return parseDOCFallbackNodes(source, filePath)
	case SourceKindXLS:
		return parseXLSTextFallback(source, filePath)
	case SourceKindImage:
		// Standalone images have no text nodes from parsing.
		// Image processing (asset save + description) is handled by
		// ProcessStandaloneImage in the import pipeline.
		return nil, nil
	default:
		return nil, fmt.Errorf("%w: %s", errUnsupportedParser, kind)
	}
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

	// Extract all text content from slides into paragraphs.
	var paragraphs []string

	for _, slide := range pres.Slides {
		var slideTexts []string
		for _, shape := range slide.Shapes {
			text := pptxShapeText(shape)
			if text != "" {
				slideTexts = append(slideTexts, text)
			}
		}
		if len(slideTexts) > 0 {
			header := fmt.Sprintf("--- Slide %d ---", slide.Number)
			paragraphs = append(paragraphs, header)
			paragraphs = append(paragraphs, slideTexts...)
		}
		if slide.Notes != "" {
			paragraphs = append(paragraphs, "[Notes] "+strings.TrimSpace(slide.Notes))
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
	f, reader, err := pdf.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	nodes := make([]DocumentNode, 0)
	fonts := make(map[string]*pdf.Font)
	for i := 1; i <= reader.NumPage(); i++ {
		page := reader.Page(i)
		for _, name := range page.Fonts() {
			if _, ok := fonts[name]; !ok {
				font := page.Font(name)
				fonts[name] = &font
			}
		}
		text, err := page.GetPlainText(fonts)
		if err != nil {
			return nil, err
		}
		text = strings.TrimSpace(trimNodeText(text))
		if text == "" {
			continue
		}
		nodes = append(nodes, DocumentNode{
			ID:         NewID("kdn"),
			SourceID:   source.ID,
			Type:       "page",
			Title:      fmt.Sprintf("%s p.%d", source.Title, i),
			Text:       text,
			Page:       i,
			Metadata:   map[string]string{"relative_path": source.RelativePath, "format": "pdf"},
			TokenCount: estimateTokens(text),
		})
	}
	if len(nodes) == 0 {
		return nil, fmt.Errorf("pdf has no readable text")
	}
	return nodes, nil
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
