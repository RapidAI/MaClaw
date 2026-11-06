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

// PlainTextByPage reads the first pages with one PDF open.
// limit <= 0 reads every page. A page with no text, or a page whose text
// cannot be read, is an empty string so the indexes stay aligned with the file.
func PlainTextByPage(pdfData []byte, limit int) ([]string, error) {
	return PlainTextByPageUntil(pdfData, limit, nil)
}

// PlainTextByPageUntil is PlainTextByPage with an early stop.
// stop is called with the 1-based page number after that page is read.
// A true result keeps the page and does not read the rest.
// When ledongthuc/pdf returns no text for the whole window, one raw parse
// retries those pages. stop may then run again, so a caller must ignore empty
// page text. A nil stop reads the window.
func PlainTextByPageUntil(pdfData []byte, limit int, stop func(page int, text string) bool) (pages []string, retErr error) {
	defer func() {
		if r := recover(); r != nil {
			retErr = fmt.Errorf("pdf text extraction panic: %v", r)
		}
	}()

	pages, stopped, err := plainTextV2Until(pdfData, limit, stop)
	if err != nil {
		raw, rawErr := plainTextRawUntil(pdfData, limit, stop)
		if rawErr != nil {
			return nil, err
		}
		return raw, nil
	}
	if stopped || plainTextAny(pages) {
		return pages, nil
	}
	raw, rawErr := plainTextRawUntil(pdfData, limit, stop)
	if rawErr != nil || !plainTextAny(raw) {
		return pages, nil
	}
	return raw, nil
}

func plainTextAny(pages []string) bool {
	for _, page := range pages {
		if strings.TrimSpace(page) != "" {
			return true
		}
	}
	return false
}

func plainTextV2Until(pdfData []byte, limit int, stop func(page int, text string) bool) (pages []string, stopped bool, err error) {
	reader, err := lpdf.NewReader(bytes.NewReader(pdfData), int64(len(pdfData)))
	if err != nil {
		return nil, false, fmt.Errorf("failed to open PDF: %w", err)
	}
	n := reader.NumPage()
	if limit > 0 && n > limit {
		n = limit
	}
	pages = make([]string, n)
	for i := 1; i <= n; i++ {
		pages[i-1] = textOfV2Page(reader.Page(i))
		if stop != nil && stop(i, pages[i-1]) {
			return pages[:i], true, nil
		}
	}
	return pages, false, nil
}

func textOfV2Page(page lpdf.Page) (text string) {
	defer func() { _ = recover() }()
	if page.V.IsNull() {
		return ""
	}
	text, err := getPlainTextFromPage(page)
	if err != nil {
		return ""
	}
	return text
}

func plainTextRawUntil(pdfData []byte, limit int, stop func(page int, text string) bool) (pages []string, err error) {
	parser, err := newRawPDFParser(pdfData)
	if err != nil {
		return nil, err
	}
	n := len(parser.pages)
	if limit > 0 && n > limit {
		n = limit
	}
	pages = make([]string, n)
	for i := 0; i < n; i++ {
		pages[i] = rawPagePlainText(parser, i)
		if stop != nil && stop(i+1, pages[i]) {
			return pages[:i+1], nil
		}
	}
	return pages, nil
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
