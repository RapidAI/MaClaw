package guiapp

import (
	"bytes"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	gopdf2 "github.com/VantageDataChat/GoPDF2"
)

const (
	paperFigureMinSide  = 160
	paperFigureMinBytes = 500
	paperFigureMaxCount = 4
	// A near full-page image is a scan of the page, not a principle diagram.
	paperFigureMaxDisplayW = 520
	paperFigureMaxDisplayH = 740
	// An on-page box shorter than this on its long side is a template icon
	// or a logo. Elsevier paints the corresponding-author envelope as a
	// 225px JPEG at about 8pt; the pixel buffer alone looks like a figure.
	paperFigureMinDisplay = 36
)

type paperFigure struct {
	page int
	data []byte
	ext  string
	area int
	y    float64
}

// fileCompanionEmbedPaperFigures places principle diagrams directly under the
// 主要原理图 heading, before the written explanation.
// Caption pages are rasterized first, so vector charts and Flate images are
// included. A PDF with no Figure caption still contributes decodable JPEG or
// PNG XObjects. A source that is not a PDF is unchanged.
func fileCompanionEmbedPaperFigures(source, destDir, markdown, sessionID string) string {
	if lines := fileCompanionRasterizePaperFigures(source, destDir, sessionID); len(lines) > 0 {
		return insertPaperFigureLines(markdown, lines)
	}
	figures, err := fileCompanionExtractPaperFigures(source)
	if err != nil {
		logFileCompanion("paper figures failed session=%s", sessionID)
		return markdown
	}
	if len(figures) == 0 {
		return markdown
	}
	dir := filepath.Join(destDir, "figures")
	_ = os.RemoveAll(dir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		logFileCompanion("paper figures failed session=%s", sessionID)
		return markdown
	}
	lines := make([]string, 0, len(figures))
	for i, fig := range figures {
		name := fmt.Sprintf("p%d-%d%s", fig.page, i+1, fig.ext)
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, fig.data, 0o644); err != nil {
			continue
		}
		lines = append(lines, fmt.Sprintf("![原文第%d页](%s)", fig.page, filepath.ToSlash(path)))
	}
	if len(lines) == 0 {
		logFileCompanion("paper figures failed session=%s", sessionID)
		return markdown
	}
	return insertPaperFigureLines(markdown, lines)
}

func fileCompanionExtractPaperFigures(source string) ([]paperFigure, error) {
	source = strings.TrimSpace(source)
	if !strings.EqualFold(filepath.Ext(source), ".pdf") {
		return nil, nil
	}
	data, err := os.ReadFile(source)
	if err != nil {
		return nil, err
	}
	return paperFiguresFromPDFBytes(data)
}

func paperFiguresFromPDFBytes(data []byte) ([]paperFigure, error) {
	if len(data) == 0 {
		return nil, nil
	}
	pages, err := safeExtractPaperPDFImages(data)
	if err != nil {
		return nil, err
	}
	seen := map[int]bool{}
	var found []paperFigure
	pageIndexes := make([]int, 0, len(pages))
	for pageIndex := range pages {
		pageIndexes = append(pageIndexes, pageIndex)
	}
	sort.Ints(pageIndexes)
	for _, pageIndex := range pageIndexes {
		for _, extracted := range pages[pageIndex] {
			if extracted.ObjNum != 0 && seen[extracted.ObjNum] {
				continue
			}
			fig, ok := paperFigureFromExtracted(pageIndex+1, extracted)
			if !ok {
				continue
			}
			if extracted.ObjNum != 0 {
				seen[extracted.ObjNum] = true
			}
			found = append(found, fig)
		}
	}
	if len(found) == 0 {
		return nil, nil
	}
	sort.SliceStable(found, func(i, j int) bool {
		return found[i].area > found[j].area
	})
	if len(found) > paperFigureMaxCount {
		found = found[:paperFigureMaxCount]
	}
	sort.SliceStable(found, func(i, j int) bool {
		if found[i].page != found[j].page {
			return found[i].page < found[j].page
		}
		return found[i].y > found[j].y
	})
	return found, nil
}

// paperFigurePaintedAsIcon reports a bitmap placed smaller than a figure.
// A missing placement (either side near zero) is not an icon: the pixel
// check still decides, so a chart whose matrix was not recovered stays.
func paperFigurePaintedAsIcon(extracted gopdf2.ExtractedImage) bool {
	w := math.Abs(extracted.DisplayWidth)
	h := math.Abs(extracted.DisplayHeight)
	if w < 1 || h < 1 {
		return false
	}
	long := w
	if h > long {
		long = h
	}
	return long < paperFigureMinDisplay
}

func paperFigureFromExtracted(page int, extracted gopdf2.ExtractedImage) (paperFigure, bool) {
	if len(extracted.Data) < paperFigureMinBytes {
		return paperFigure{}, false
	}
	if paperFigurePaintedAsIcon(extracted) {
		return paperFigure{}, false
	}
	// A mirrored image stores a negative side. Magnitude is the on-page size.
	if math.Abs(extracted.DisplayWidth) > paperFigureMaxDisplayW && math.Abs(extracted.DisplayHeight) > paperFigureMaxDisplayH {
		return paperFigure{}, false
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(extracted.Data))
	if err != nil {
		return paperFigure{}, false
	}
	ext := ""
	switch format {
	case "jpeg":
		ext = ".jpg"
	case "png":
		ext = ".png"
	default:
		return paperFigure{}, false
	}
	longSide, shortSide := cfg.Width, cfg.Height
	if shortSide > longSide {
		longSide, shortSide = shortSide, longSide
	}
	// Wide principle diagrams stay. Thin rules and icons do not.
	if longSide < paperFigureMinSide || shortSide < 100 {
		return paperFigure{}, false
	}
	return paperFigure{
		page: page,
		data: extracted.Data,
		ext:  ext,
		area: cfg.Width * cfg.Height,
		y:    extracted.Y,
	}, true
}

func safeExtractPaperPDFImages(pdfData []byte) (pages map[int][]gopdf2.ExtractedImage, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			pages = nil
			err = fmt.Errorf("extract pdf images panicked")
		}
	}()
	return gopdf2.ExtractImagesFromAllPages(pdfData)
}

func insertPaperFigureLines(markdown string, lines []string) string {
	// A blank line after the image keeps it a separate block. Glued to the
	// following sentence, the page filler can slice the image path.
	block := "\n" + strings.Join(lines, "\n\n") + "\n\n"
	if at, ok := paperFigureHeadingEnd(markdown); ok {
		return markdown[:at] + block + markdown[at:]
	}
	return strings.TrimRight(markdown, "\n") + "\n\n## 主要原理图及说明\n" + strings.Trim(block, "\n") + "\n"
}

// paperFigureHeadingEnd is the index just after the heading line. A numbered
// heading such as "## 3. 主要原理图及说明" counts; the dataset parser already
// accepts that shape, and a strict prefix would append a second section.
func paperFigureHeadingEnd(markdown string) (int, bool) {
	offset := 0
	for _, line := range strings.Split(markdown, "\n") {
		end := offset + len(line)
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "## ") {
			title := strings.TrimSpace(strings.TrimLeft(strings.TrimPrefix(trim, "## "), "0123456789.、．)）（( \t"))
			if strings.Contains(title, "原理图") {
				if end < len(markdown) && markdown[end] == '\n' {
					end++
				}
				return end, true
			}
		}
		offset = end + 1
	}
	return 0, false
}
