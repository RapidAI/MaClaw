package agent

import (
	"fmt"
	"math"
	"os"
	"sort"
	"strings"

	"github.com/unitedrhino/go-cad"
)

// extractCADText reads DWG/DXF text and groups it by drawing frame.
// It does not render the drawing.
func extractCADText(filePath, format string) (text string, err error) {
	defer recoverLegacyOfficeExtraction("CAD", &text, &err)
	if err := preflightDirectDocumentInput(filePath); err != nil {
		return "", err
	}
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", err
	}
	var doc *cad.Document
	switch format {
	case "dxf", "dxfb":
		doc, err = cad.ParseDXF(data)
	default:
		doc, err = cad.Parse(data)
	}
	if err != nil {
		return "", fmt.Errorf("无法解析图纸: %v", err)
	}
	if doc == nil {
		return "", fmt.Errorf("图纸里没有可读取的文字")
	}
	texts := compactCADTexts(cad.SheetTexts(doc))
	if len(texts) == 0 {
		for _, item := range doc.Texts() {
			if strings.TrimSpace(item.Text) == "" {
				continue
			}
			texts = append(texts, cad.SheetText{Text: item.Text, X: item.X, Y: item.Y})
		}
	}
	// SheetTexts already calls DetectSheets. Call it again only when some
	// text landed in a real frame, so a drawing with no frame is detected once.
	var realSheets []cad.Sheet
	if cadTextsHaveSheets(texts) {
		sheets := cad.DetectSheets(doc)
		if !(len(sheets) == 1 && sheets[0].Name == "整图") {
			realSheets = sheets
		}
	}
	out := renderCADTexts(realSheets, texts)
	if strings.TrimSpace(out) == "" {
		return "", fmt.Errorf("图纸里没有可读取的文字")
	}
	return out, nil
}

func renderCADTexts(sheets []cad.Sheet, texts []cad.SheetText) string {
	type page struct {
		title string
		lines []cad.SheetText
	}
	var pages []page
	if len(sheets) == 0 {
		pages = []page{{title: "", lines: texts}}
	} else {
		pages = make([]page, len(sheets))
		for i, sheet := range sheets {
			pages[i].title = sheet.Name
		}
		var outside []cad.SheetText
		for _, item := range texts {
			if item.Sheet >= 1 && item.Sheet <= len(pages) {
				pages[item.Sheet-1].lines = append(pages[item.Sheet-1].lines, item)
				continue
			}
			outside = append(outside, item)
		}
		if len(outside) > 0 {
			pages = append(pages, page{title: "框外文字", lines: outside})
		}
	}
	var b strings.Builder
	pageNo := 0
	linesWritten := 0
	for _, page := range pages {
		body := cadPageLines(page.title, page.lines)
		if len(body) == 0 {
			continue
		}
		pageNo++
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		fmt.Fprintf(&b, "## Page %d\n", pageNo)
		if page.title != "" && page.title != "整图" {
			b.WriteString(page.title)
			b.WriteByte('\n')
		}
		for _, line := range body {
			if linesWritten == cadMaxLines {
				b.WriteString("（后续文字已省略）\n")
				return strings.TrimSpace(b.String())
			}
			b.WriteString(line)
			b.WriteByte('\n')
			linesWritten++
		}
	}
	return strings.TrimSpace(b.String())
}

const cadMaxLines = 8000

func cadTextsHaveSheets(texts []cad.SheetText) bool {
	for _, item := range texts {
		if item.Sheet > 0 {
			return true
		}
	}
	return false
}

func compactCADTexts(texts []cad.SheetText) []cad.SheetText {
	out := make([]cad.SheetText, 0, len(texts))
	for _, item := range texts {
		if strings.TrimSpace(item.Text) == "" {
			continue
		}
		out = append(out, item)
	}
	return out
}

func cadPageLines(title string, lines []cad.SheetText) []string {
	sorted := append([]cad.SheetText(nil), lines...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if math.Abs(sorted[i].Y-sorted[j].Y) > 1 {
			return sorted[i].Y > sorted[j].Y
		}
		return sorted[i].X < sorted[j].X
	})
	out := make([]string, 0, len(sorted))
	seenTitle := false
	var prevX, prevY float64
	havePrev := false
	for _, item := range sorted {
		text := cleanCADText(item.Text)
		pieces := strings.Split(text, "\n")
		for _, piece := range pieces {
			piece = strings.TrimSpace(piece)
			if piece == "" {
				continue
			}
			if !seenTitle && title != "" && piece == title {
				seenTitle = true
				continue
			}
			if havePrev && piece == out[len(out)-1] && math.Abs(item.Y-prevY) <= 1 && math.Abs(item.X-prevX) <= 1 {
				continue
			}
			out = append(out, piece)
			prevX, prevY = item.X, item.Y
			havePrev = true
		}
	}
	return out
}

// cleanCADText keeps the library string. go-cad already turns MTEXT \P into
// newlines. TEXT and ATTRIB stay literal, so rewriting \P here would split a
// path such as C:\Projects.
func cleanCADText(text string) string {
	return strings.TrimSpace(text)
}
