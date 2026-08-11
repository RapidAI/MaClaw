package knowledge

import (
	"fmt"
	"strings"

	"github.com/shakinm/xlsReader/doc"
	"github.com/shakinm/xlsReader/xls"
)

// parseXLSNative reads a .xls (BIFF) file using the pure-Go xlsReader library.
func parseXLSNative(source Source, filePath string) ([]DocumentNode, error) {
	return parseXLSNativeWithOpen(source, filePath, xls.OpenFile)
}

// parseXLSNativeWithOpen keeps legacy BIFF parsing behind the same panic
// boundary as the unified Office extractor. The third-party reader processes
// attacker-controlled binary records; an unexpected panic must fail the
// import rather than unwind a knowledge-import worker.
func parseXLSNativeWithOpen(source Source, filePath string, open func(string) (xls.Workbook, error)) (nodes []DocumentNode, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			nodes = nil
			err = fmt.Errorf("parse xls panicked: %v", recovered)
		}
	}()
	wb, err := open(filePath)
	if err != nil {
		return nil, fmt.Errorf("open .xls: %w", err)
	}

	numSheets := wb.GetNumberSheets()
	if numSheets == 0 {
		return nil, fmt.Errorf("no sheets in .xls file")
	}

	var allNodes []DocumentNode
	for sheetIdx := 0; sheetIdx < numSheets; sheetIdx++ {
		sheet, err := wb.GetSheet(sheetIdx)
		if err != nil {
			continue
		}
		sheetName := sheet.GetName()
		numRows := sheet.GetNumberRows()
		if numRows == 0 {
			continue
		}

		var blockText strings.Builder
		blockStartRow := 0
		flushBlock := func(endRow int) {
			text := strings.TrimSpace(blockText.String())
			if text == "" {
				return
			}
			rowRange := fmt.Sprintf("%d-%d", blockStartRow+1, endRow+1)
			allNodes = append(allNodes, DocumentNode{
				ID:        NewID("kdn"),
				SourceID:  source.ID,
				Type:      "sheet_rows",
				Title:     fmt.Sprintf("%s [%s]", sheetName, rowRange),
				Text:      truncateNodeText(text),
				SheetName: sheetName,
				RowRange:  rowRange,
			})
		}

		for rowIdx := 0; rowIdx < numRows; rowIdx++ {
			row, err := sheet.GetRow(rowIdx)
			if err != nil || row == nil {
				continue
			}
			cols := row.GetCols()
			var cells []string
			for _, col := range cols {
				val := strings.TrimSpace(col.GetString())
				cells = append(cells, val)
			}
			line := strings.Join(cells, "\t")
			if strings.TrimSpace(line) == "" {
				continue
			}
			blockText.WriteString(line)
			blockText.WriteByte('\n')

			if blockText.Len() > targetSheetNodeRunes {
				flushBlock(rowIdx)
				blockText.Reset()
				blockStartRow = rowIdx + 1
			}
		}
		if blockText.Len() > 0 {
			flushBlock(numRows - 1)
		}
	}

	if len(allNodes) == 0 {
		return nil, fmt.Errorf("no text content extracted from .xls file")
	}
	return allNodes, nil
}

// parseDOCNative reads a .doc (Word 97-2003) file using the pure-Go doc reader.
func parseDOCNative(source Source, filePath string) ([]DocumentNode, error) {
	return parseDOCNativeWithOpen(source, filePath, doc.OpenFile)
}

// parseDOCNativeWithOpen keeps legacy Word parsing inside an explicit panic
// boundary. This mirrors agent's legacy DOC extraction contract and prevents
// malformed OLE/Word records from crashing knowledge imports when rich
// OfficeRead Markdown is intentionally disabled for staged rollout.
func parseDOCNativeWithOpen(source Source, filePath string, open func(string) (doc.Document, error)) (nodes []DocumentNode, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			nodes = nil
			err = fmt.Errorf("parse doc panicked: %v", recovered)
		}
	}()
	document, err := open(filePath)
	if err != nil {
		return nil, fmt.Errorf("open .doc: %w", err)
	}

	formatted := document.GetFormattedContent()
	if formatted != nil && len(formatted.Paragraphs) > 0 {
		return parseDOCFormattedContent(source, formatted), nil
	}

	text := document.GetText()
	if strings.TrimSpace(text) == "" {
		return nil, fmt.Errorf("no text content in .doc file")
	}
	return parsePlainTextNodes(source, text, "doc_native"), nil
}

// parseDOCFormattedContent converts formatted DOC paragraphs into DocumentNodes.
func parseDOCFormattedContent(source Source, formatted *doc.FormattedContent) []DocumentNode {
	var allNodes []DocumentNode
	var blockText strings.Builder
	blockStartPara := 0
	paraCount := len(formatted.Paragraphs)

	paraText := func(para doc.Paragraph) string {
		var sb strings.Builder
		for _, run := range para.Runs {
			sb.WriteString(run.Text)
		}
		if para.TextBoxText != "" {
			if sb.Len() > 0 {
				sb.WriteByte('\n')
			}
			sb.WriteString(para.TextBoxText)
		}
		return strings.TrimSpace(sb.String())
	}

	flushBlock := func(endPara int) {
		text := strings.TrimSpace(blockText.String())
		if text == "" {
			return
		}
		title := ""
		for i := blockStartPara; i <= endPara && i < paraCount; i++ {
			t := paraText(formatted.Paragraphs[i])
			if t != "" {
				if len([]rune(t)) > 80 {
					title = string([]rune(t)[:80]) + "..."
				} else {
					title = t
				}
				break
			}
		}
		allNodes = append(allNodes, DocumentNode{
			ID:       NewID("kdn"),
			SourceID: source.ID,
			Type:     "paragraph",
			Title:    title,
			Text:     truncateNodeText(text),
			Offset:   blockStartPara,
		})
	}

	for i, para := range formatted.Paragraphs {
		text := paraText(para)
		if text == "" {
			continue
		}
		blockText.WriteString(text)
		blockText.WriteByte('\n')

		if blockText.Len() > targetTextNodeRunes {
			flushBlock(i)
			blockText.Reset()
			blockStartPara = i + 1
		}
	}
	if blockText.Len() > 0 {
		flushBlock(paraCount - 1)
	}

	// Headers and footers
	var headerFooterText strings.Builder
	for _, h := range formatted.Headers {
		if t := strings.TrimSpace(h); t != "" {
			headerFooterText.WriteString(t)
			headerFooterText.WriteByte('\n')
		}
	}
	for _, f := range formatted.Footers {
		if t := strings.TrimSpace(f); t != "" {
			headerFooterText.WriteString(t)
			headerFooterText.WriteByte('\n')
		}
	}
	for _, h := range formatted.HeaderEntries {
		if t := strings.TrimSpace(h.Text); t != "" {
			headerFooterText.WriteString(t)
			headerFooterText.WriteByte('\n')
		}
	}
	for _, f := range formatted.FooterEntries {
		if t := strings.TrimSpace(f.Text); t != "" {
			headerFooterText.WriteString(t)
			headerFooterText.WriteByte('\n')
		}
	}
	if hf := strings.TrimSpace(headerFooterText.String()); hf != "" {
		allNodes = append(allNodes, DocumentNode{
			ID:       NewID("kdn"),
			SourceID: source.ID,
			Type:     "header_footer",
			Title:    "Headers & Footers",
			Text:     hf,
		})
	}

	return allNodes
}

// parseDOCFallbackNodes is the entry point for .doc parsing.
func parseDOCFallbackNodes(source Source, filePath string) ([]DocumentNode, error) {
	return parseDOCNative(source, filePath)
}

// parseXLSTextFallback is the entry point for .xls parsing.
func parseXLSTextFallback(source Source, filePath string) ([]DocumentNode, error) {
	return parseXLSNative(source, filePath)
}

// truncateNodeText limits text to maxNodeTextRunes.
func truncateNodeText(text string) string {
	runes := []rune(text)
	if len(runes) > maxNodeTextRunes {
		return string(runes[:maxNodeTextRunes])
	}
	return text
}
