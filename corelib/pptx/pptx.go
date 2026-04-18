package pptx

import (
	"errors"
	"fmt"
	"os"
	"strings"

	ppt "github.com/VantageDataChat/GoPPT"
)

// Presentation is the top-level structured representation of a PPTX file.
type Presentation struct {
	SlideCount int                `json:"slide_count"`
	Properties DocumentProperties `json:"properties"`
	Slides     []Slide            `json:"slides"`
}

// DocumentProperties holds standard document metadata.
type DocumentProperties struct {
	Title       string `json:"title,omitempty"`
	Creator     string `json:"creator,omitempty"`
	Description string `json:"description,omitempty"`
}

// Slide represents a single slide.
type Slide struct {
	Number int     `json:"number"`
	Shapes []Shape `json:"shapes"`
	Notes  string  `json:"notes,omitempty"`
}

// Shape represents a shape on a slide.
type Shape struct {
	Type       ShapeType  `json:"type"`
	Name       string     `json:"name,omitempty"`
	Position   Position   `json:"position"`
	Dimensions Dimensions `json:"dimensions"`
	Text       *TextBody  `json:"text,omitempty"`
	Table      *TableData `json:"table,omitempty"`
	Chart      *ChartData `json:"chart,omitempty"`
}

// ShapeType represents the type of shape as a string constant.
type ShapeType string

const (
	ShapeTypeText  ShapeType = "text"
	ShapeTypeTable ShapeType = "table"
	ShapeTypeChart ShapeType = "chart"
	ShapeTypeImage ShapeType = "image"
	ShapeTypeLine  ShapeType = "line"
	ShapeTypeGroup ShapeType = "group"
)

// Position represents a shape's position in EMU (English Metric Units).
type Position struct {
	X int `json:"x"`
	Y int `json:"y"`
}

// Dimensions represents a shape's size in EMU.
type Dimensions struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

// TextBody contains paragraphs of formatted text.
type TextBody struct {
	Paragraphs []Paragraph `json:"paragraphs"`
}

// Paragraph represents a text paragraph.
type Paragraph struct {
	Runs []TextRun `json:"runs"`
}

// TextRun represents a run of text with formatting.
type TextRun struct {
	Text     string `json:"text"`
	Bold     bool   `json:"bold,omitempty"`
	Italic   bool   `json:"italic,omitempty"`
	FontSize int    `json:"font_size,omitempty"`
	Color    string `json:"color,omitempty"`
}

// TableData represents a table shape.
type TableData struct {
	Rows []TableRow `json:"rows"`
}

// TableRow represents a row in a table.
type TableRow struct {
	Cells []TableCell `json:"cells"`
}

// TableCell represents a cell in a table.
type TableCell struct {
	Text string `json:"text"`
}

// ChartData represents a chart shape.
type ChartData struct {
	ChartType  string       `json:"chart_type"`
	DataSeries []DataSeries `json:"data_series"`
}

// DataSeries represents a data series in a chart.
type DataSeries struct {
	Label string `json:"label"`
}

// Read parses a PPTX file and returns a structured representation.
func Read(filePath string) (*Presentation, error) {
	// Check if file exists
	if _, err := os.Stat(filePath); errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("文件不存在: %s", filePath)
	}

	// Open the PPTX file using GoPPT
	pres, err := ppt.Open(filePath)
	if err != nil {
		errMsg := err.Error()
		if isInvalidFormat(errMsg) {
			return nil, fmt.Errorf("文件格式无效，不是有效的 PPTX 文件: %s", filePath)
		}
		return nil, fmt.Errorf("读取 PPTX 失败: %w", err)
	}
	defer pres.Close()

	// Extract document properties
	props := pres.GetDocumentProperties()
	docProps := DocumentProperties{
		Title:       props.Title,
		Creator:     props.Creator,
		Description: props.Description,
	}

	// Extract slides
	allSlides := pres.GetAllSlides()
	slides := make([]Slide, 0, len(allSlides))

	for i, s := range allSlides {
		slide := Slide{
			Number: i + 1,
			Shapes: extractShapes(s.GetShapes()),
			Notes:  s.GetNotes(),
		}
		slides = append(slides, slide)
	}

	return &Presentation{
		SlideCount: pres.GetSlideCount(),
		Properties: docProps,
		Slides:     slides,
	}, nil
}

// extractShapes converts GoPPT shapes to our Shape type.
func extractShapes(pptShapes []ppt.Shape) []Shape {
	shapes := make([]Shape, 0, len(pptShapes))

	for _, s := range pptShapes {
		shape := convertShape(s)
		shapes = append(shapes, shape)
	}

	return shapes
}

// convertShape converts a single GoPPT shape to our Shape type.
func convertShape(s ppt.Shape) Shape {
	shape := Shape{
		Name: s.GetName(),
		Position: Position{
			X: int(s.GetOffsetX()),
			Y: int(s.GetOffsetY()),
		},
		Dimensions: Dimensions{
			Width:  int(s.GetWidth()),
			Height: int(s.GetHeight()),
		},
	}

	switch sh := s.(type) {
	case *ppt.RichTextShape:
		shape.Type = ShapeTypeText
		shape.Text = extractTextBody(sh.GetParagraphs())

	case *ppt.PlaceholderShape:
		shape.Type = ShapeTypeText
		shape.Text = extractPlaceholderTextBody(sh)

	case *ppt.AutoShape:
		shape.Type = ShapeTypeText
		shape.Text = extractAutoShapeTextBody(sh)

	case *ppt.TableShape:
		shape.Type = ShapeTypeTable
		shape.Table = extractTableData(sh)

	case *ppt.ChartShape:
		shape.Type = ShapeTypeChart
		shape.Chart = extractChartData(sh)

	case *ppt.DrawingShape:
		shape.Type = ShapeTypeImage

	case *ppt.LineShape:
		shape.Type = ShapeTypeLine

	case *ppt.GroupShape:
		shape.Type = ShapeTypeGroup

	default:
		shape.Type = ShapeTypeText
	}

	return shape
}

// extractTextBody extracts text content from paragraphs.
func extractTextBody(paragraphs []*ppt.Paragraph) *TextBody {
	if len(paragraphs) == 0 {
		return nil
	}

	body := &TextBody{
		Paragraphs: make([]Paragraph, 0, len(paragraphs)),
	}

	for _, para := range paragraphs {
		p := Paragraph{
			Runs: extractTextRuns(para.GetElements()),
		}
		body.Paragraphs = append(body.Paragraphs, p)
	}

	return body
}

// extractPlaceholderTextBody extracts text from a PlaceholderShape.
func extractPlaceholderTextBody(ph *ppt.PlaceholderShape) *TextBody {
	paragraphs := ph.GetParagraphs()
	return extractTextBody(paragraphs)
}

// extractAutoShapeTextBody extracts text from an AutoShape.
func extractAutoShapeTextBody(as *ppt.AutoShape) *TextBody {
	// AutoShape may have rich text paragraphs or simple text
	paragraphs := as.GetParagraphs()
	if len(paragraphs) > 0 {
		return extractTextBody(paragraphs)
	}

	// Fall back to simple text
	text := as.GetText()
	if text == "" {
		return nil
	}

	return &TextBody{
		Paragraphs: []Paragraph{
			{
				Runs: []TextRun{{Text: text}},
			},
		},
	}
}

// extractTextRuns extracts text runs from paragraph elements.
func extractTextRuns(elements []ppt.ParagraphElement) []TextRun {
	runs := make([]TextRun, 0)

	for _, elem := range elements {
		switch e := elem.(type) {
		case *ppt.TextRun:
			run := TextRun{
				Text: e.GetText(),
			}
			font := e.GetFont()
			if font != nil {
				run.Bold = font.Bold
				run.Italic = font.Italic
				if font.Size > 0 {
					run.FontSize = font.Size
				}
				if font.Color.ARGB != "" && font.Color.ARGB != "FF000000" {
					run.Color = formatColor(font.Color)
				}
			}
			runs = append(runs, run)
		}
	}

	return runs
}

// extractTableData extracts table data from a TableShape.
func extractTableData(ts *ppt.TableShape) *TableData {
	pptRows := ts.GetRows()
	rows := make([]TableRow, 0, len(pptRows))

	for _, pptRow := range pptRows {
		cells := make([]TableCell, 0, len(pptRow))
		for _, cell := range pptRow {
			cellText := extractCellText(cell)
			cells = append(cells, TableCell{Text: cellText})
		}
		rows = append(rows, TableRow{Cells: cells})
	}

	return &TableData{Rows: rows}
}

// extractCellText extracts text from a table cell's paragraphs.
func extractCellText(cell *ppt.TableCell) string {
	if cell == nil {
		return ""
	}
	paragraphs := cell.GetParagraphs()
	var parts []string
	for _, para := range paragraphs {
		var sb strings.Builder
		for _, elem := range para.GetElements() {
			if tr, ok := elem.(*ppt.TextRun); ok {
				sb.WriteString(tr.GetText())
			}
		}
		if sb.Len() > 0 {
			parts = append(parts, sb.String())
		}
	}
	return strings.Join(parts, "\n")
}

// extractChartData extracts chart data from a ChartShape.
func extractChartData(cs *ppt.ChartShape) *ChartData {
	chartData := &ChartData{
		DataSeries: make([]DataSeries, 0),
	}

	plotArea := cs.GetPlotArea()
	if plotArea == nil {
		return chartData
	}

	chartType := plotArea.GetType()
	if chartType != nil {
		chartData.ChartType = chartType.GetChartTypeName()
	}

	// Extract data series from the chart type
	chartData.DataSeries = extractSeriesFromChartType(chartType)

	return chartData
}

// extractSeriesFromChartType extracts series labels from a chart type.
func extractSeriesFromChartType(ct ppt.ChartType) []DataSeries {
	if ct == nil {
		return nil
	}

	var series []DataSeries

	switch c := ct.(type) {
	case *ppt.BarChart:
		for _, s := range c.Series {
			series = append(series, DataSeries{Label: s.Title})
		}
	case *ppt.Bar3DChart:
		for _, s := range c.Series {
			series = append(series, DataSeries{Label: s.Title})
		}
	case *ppt.LineChart:
		for _, s := range c.Series {
			series = append(series, DataSeries{Label: s.Title})
		}
	case *ppt.AreaChart:
		for _, s := range c.Series {
			series = append(series, DataSeries{Label: s.Title})
		}
	case *ppt.PieChart:
		for _, s := range c.Series {
			series = append(series, DataSeries{Label: s.Title})
		}
	case *ppt.Pie3DChart:
		for _, s := range c.Series {
			series = append(series, DataSeries{Label: s.Title})
		}
	case *ppt.DoughnutChart:
		for _, s := range c.Series {
			series = append(series, DataSeries{Label: s.Title})
		}
	case *ppt.ScatterChart:
		for _, s := range c.Series {
			series = append(series, DataSeries{Label: s.Title})
		}
	case *ppt.RadarChart:
		for _, s := range c.Series {
			series = append(series, DataSeries{Label: s.Title})
		}
	}

	return series
}

// formatColor converts a GoPPT Color to a hex color string.
func formatColor(c ppt.Color) string {
	if c.ARGB == "" {
		return ""
	}
	// ARGB format is "AARRGGBB", we want "#RRGGBB"
	if len(c.ARGB) == 8 {
		return "#" + c.ARGB[2:]
	}
	return "#" + c.ARGB
}

// isInvalidFormat checks if an error message indicates an invalid PPTX format.
func isInvalidFormat(errMsg string) bool {
	lower := strings.ToLower(errMsg)
	return strings.Contains(lower, "zip") ||
		strings.Contains(lower, "not a valid") ||
		strings.Contains(lower, "invalid") ||
		strings.Contains(lower, "corrupt") ||
		strings.Contains(lower, "unexpected eof") ||
		strings.Contains(lower, "cannot open")
}
