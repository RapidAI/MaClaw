package gopdf

import (
	"bytes"
	"fmt"
	"io"
	"sort"
	"strings"

	lpdf "github.com/ledongthuc/pdf"
)

// extractTextV2FromPage extracts text from a specific page (0-based index)
// using the ledongthuc/pdf library for robust PDF parsing.
func extractTextV2FromPage(pdfData []byte, pageIndex int) (result []ExtractedText, retErr error) {
	defer func() {
		if r := recover(); r != nil {
			retErr = fmt.Errorf("pdf text extraction panic: %v", r)
		}
	}()

	reader, err := lpdf.NewReader(bytes.NewReader(pdfData), int64(len(pdfData)))
	if err != nil {
		return nil, fmt.Errorf("failed to open PDF: %w", err)
	}

	numPages := reader.NumPage()
	if pageIndex < 0 || pageIndex >= numPages {
		return nil, fmt.Errorf("page index %d out of range (0..%d)", pageIndex, numPages-1)
	}

	page := reader.Page(pageIndex + 1) // ledongthuc/pdf uses 1-based pages
	if page.V.IsNull() {
		return nil, nil
	}

	return extractContentFromPage(page)
}

// extractTextV2FromAllPages extracts text from all pages using ledongthuc/pdf.
// Returns a map keyed by 0-based page index.
func extractTextV2FromAllPages(pdfData []byte) (result map[int][]ExtractedText, retErr error) {
	defer func() {
		if r := recover(); r != nil {
			retErr = fmt.Errorf("pdf text extraction panic: %v", r)
		}
	}()

	reader, err := lpdf.NewReader(bytes.NewReader(pdfData), int64(len(pdfData)))
	if err != nil {
		return nil, fmt.Errorf("failed to open PDF: %w", err)
	}

	numPages := reader.NumPage()
	result = make(map[int][]ExtractedText, numPages)

	for i := 1; i <= numPages; i++ {
		page := reader.Page(i)
		if page.V.IsNull() {
			continue
		}
		texts, err := extractContentFromPage(page)
		if err != nil {
			continue // skip pages that fail
		}
		if len(texts) > 0 {
			result[i-1] = texts // convert to 0-based
		}
	}
	return result, nil
}

// extractPageTextV2 extracts plain text from a single page (0-based)
// using ledongthuc/pdf, returning a single string with lines separated by newlines.
func extractPageTextV2(pdfData []byte, pageIndex int) (result string, retErr error) {
	defer func() {
		if r := recover(); r != nil {
			retErr = fmt.Errorf("pdf text extraction panic: %v", r)
		}
	}()

	reader, err := lpdf.NewReader(bytes.NewReader(pdfData), int64(len(pdfData)))
	if err != nil {
		return "", fmt.Errorf("failed to open PDF: %w", err)
	}

	numPages := reader.NumPage()
	if pageIndex < 0 || pageIndex >= numPages {
		return "", fmt.Errorf("page index %d out of range (0..%d)", pageIndex, numPages-1)
	}

	page := reader.Page(pageIndex + 1)
	if page.V.IsNull() {
		return "", nil
	}

	return getPlainTextFromPage(page)
}

// extractContentFromPage uses page.Content() to get structured text with positions.
func extractContentFromPage(page lpdf.Page) ([]ExtractedText, error) {
	content := page.Content()
	if len(content.Text) == 0 {
		return nil, nil
	}

	var result []ExtractedText
	for _, t := range content.Text {
		s := strings.TrimSpace(t.S)
		if s == "" {
			continue
		}
		result = append(result, ExtractedText{
			Text:     s,
			X:        t.X,
			Y:        t.Y,
			FontName: t.Font,
			FontSize: t.FontSize,
		})
	}
	return result, nil
}

// getPlainTextFromPage extracts plain text from a page using GetTextByRow
// for proper reading order.
func getPlainTextFromPage(page lpdf.Page) (string, error) {
	if page.V.Key("Contents").Kind() == lpdf.Null {
		return "", nil
	}

	rows, err := page.GetTextByRow()
	if err != nil {
		// fallback to GetPlainText
		return getPlainTextFallback(page)
	}

	var sb strings.Builder
	for i, row := range rows {
		if i > 0 {
			sb.WriteByte('\n')
		}
		// Sort content within row by X position
		sort.Slice(row.Content, func(a, b int) bool {
			return row.Content[a].X < row.Content[b].X
		})
		for j, word := range row.Content {
			if j > 0 {
				sb.WriteByte(' ')
			}
			sb.WriteString(word.S)
		}
	}
	return sb.String(), nil
}

func getPlainTextFallback(page lpdf.Page) (string, error) {
	text, err := page.GetPlainText(nil)
	if err != nil {
		return "", err
	}
	return text, nil
}

// GetSourcePDFPageCountV2 returns the page count using ledongthuc/pdf,
// which handles more PDF formats than gofpdi.
func GetSourcePDFPageCountV2(pdfData []byte) (n int, retErr error) {
	defer func() {
		if r := recover(); r != nil {
			retErr = fmt.Errorf("failed to parse PDF: %v", r)
		}
	}()

	reader, err := lpdf.NewReader(bytes.NewReader(pdfData), int64(len(pdfData)))
	if err != nil {
		return 0, fmt.Errorf("failed to open PDF: %w", err)
	}
	return reader.NumPage(), nil
}

// ExtractAllPagesText extracts plain text from all pages of a PDF,
// returning a single string with page breaks. Uses ledongthuc/pdf for
// robust parsing of diverse PDF formats.
//
// Example:
//
//	data, _ := os.ReadFile("input.pdf")
//	text, _ := gopdf.ExtractAllPagesText(data)
//	fmt.Println(text)
func ExtractAllPagesText(pdfData []byte) (result string, retErr error) {
	defer func() {
		if r := recover(); r != nil {
			retErr = fmt.Errorf("pdf text extraction panic: %v", r)
		}
	}()

	reader, err := lpdf.NewReader(bytes.NewReader(pdfData), int64(len(pdfData)))
	if err != nil {
		return "", fmt.Errorf("failed to open PDF: %w", err)
	}

	numPages := reader.NumPage()
	var sb strings.Builder

	for i := 1; i <= numPages; i++ {
		page := reader.Page(i)
		if page.V.IsNull() {
			continue
		}
		text, err := getPlainTextFromPage(page)
		if err != nil {
			continue
		}
		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}
		if sb.Len() > 0 {
			sb.WriteByte('\n')
		}
		sb.WriteString(text)
	}
	return sb.String(), nil
}

// GetPlainTextReader returns an io.Reader that yields all plain text
// from the PDF. This is a convenience wrapper around ledongthuc/pdf's
// Reader.GetPlainText().
func GetPlainTextReader(pdfData []byte) (io.Reader, error) {
	reader, err := lpdf.NewReader(bytes.NewReader(pdfData), int64(len(pdfData)))
	if err != nil {
		return nil, fmt.Errorf("failed to open PDF: %w", err)
	}
	return reader.GetPlainText()
}

