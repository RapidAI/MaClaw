package gopdf

import "strings"

// Represents an RGB color with red, green, and blue components
type RGBColor struct {
	R uint8 // Red component (0-255)
	G uint8 // Green component (0-255)
	B uint8 // Blue component (0-255)
}

// Defines the border style for a cell or table
type BorderStyle struct {
	Top      bool     // Whether to draw the top border
	Left     bool     // Whether to draw the left border
	Right    bool     // Whether to draw the right border
	Bottom   bool     // Whether to draw the bottom border
	Width    float64  // Width of the border line
	RGBColor RGBColor // Color of the border
}

// Defines the style for a cell, including border, fill, text, and font properties
type CellStyle struct {
	BorderStyle BorderStyle // Border style for the cell
	FillColor   RGBColor    // Background color of the cell
	TextColor   RGBColor    // Color of the text in the cell
	Font        string      // Font name for the cell text
	FontSize    float64     // Font size for the cell text
}

type RowCell struct {
	content      string    // Content (display value) of the cell
	useCellStyle bool      // If true, use cellStyle instead of the style in the tableLayout
	cellStyle    CellStyle // Style of the cell
	colspan      int       // Number of columns this cell spans (1 = default)
}

func NewRowCell(content string, cellStyle CellStyle) RowCell {
	return newStyledRowCell(content, true, cellStyle)
}

// NewRowCellSpan creates a cell that spans multiple columns.
func NewRowCellSpan(content string, cellStyle CellStyle, colspan int) RowCell {
	rc := newStyledRowCell(content, true, cellStyle)
	if colspan < 1 {
		colspan = 1
	}
	rc.colspan = colspan
	return rc
}

func newStyledRowCell(content string, useCellStyle bool, cellStyle CellStyle) RowCell {
	return RowCell{
		content:      content,
		useCellStyle: useCellStyle,
		cellStyle:    cellStyle,
		colspan:      1,
	}
}

type TableLayout interface {
	AddColumn(header string, width float64, align string)
	AddRow(row []string)
	AddStyledRow(row []RowCell)
	SetTableStyle(style CellStyle)
	SetHeaderStyle(style CellStyle)
	SetCellStyle(style CellStyle)
	DrawTable() error
	// SetAutoRowHeight enables content-based row heights. When enabled the
	// rowHeight passed to NewTableLayout is the minimum height; rows grow to
	// fit wrapped text. Default: false (fixed heights, legacy behavior).
	SetAutoRowHeight(auto bool)
	// SetPaginate enables multi-page tables: when a row would cross the page
	// bottom margin, a new page is added and the header row redrawn.
	SetPaginate(paginate bool)
}

// Represents the layout of a table
type tableLayout struct {
	pdf           *GoPdf      // Reference to the GoPdf instance
	startX        float64     // Starting X coordinate of the table
	startY        float64     // Starting Y coordinate of the table
	rowHeight     float64     // Height of each row in the table (minimum when auto)
	columns       []column    // Slice of column definitions
	rows          [][]RowCell // Slice of rows, each containing cell contents
	maxRows       int         // Maximum number of rows in the table
	padding       float64     // Padding inside each cell
	cellOption    CellOption  // Options for cell content rendering
	tableStyle    CellStyle   // Style for the entire table
	headerStyle   CellStyle   // Style for the header row
	cellStyle     CellStyle   // Style for regular cells
	autoRowHeight bool        // grow rows to fit wrapped content
	paginate      bool        // break pages and repeat headers
}

var _ TableLayout = (*tableLayout)(nil)

// Represents a column in the table
type column struct {
	header string  // Header text for the column
	width  float64 // Width of the column
	align  string  // Alignment of content within the column
}

// Creates a new table layout with the given parameters
func (gp *GoPdf) NewTableLayout(startX, startY, rowHeight float64, maxRows int) TableLayout {
	return &tableLayout{
		pdf:       gp,
		startX:    startX,
		startY:    startY,
		rowHeight: rowHeight,
		maxRows:   maxRows,
		padding:   2.0,
		cellOption: CellOption{
			BreakOption: &BreakOption{
				Mode:           BreakModeIndicatorSensitive,
				BreakIndicator: ' ',
			},
		},
		tableStyle: CellStyle{
			BorderStyle: BorderStyle{
				Top: true, Left: true, Right: true, Bottom: true,
				Width:    0.5,
				RGBColor: RGBColor{R: 0, G: 0, B: 0},
			},
		},
		headerStyle: CellStyle{
			BorderStyle: BorderStyle{
				Top: true, Left: true, Right: true, Bottom: true,
				Width:    0.5,
				RGBColor: RGBColor{R: 0, G: 0, B: 0},
			},
			FillColor: RGBColor{R: 240, G: 240, B: 240},
			TextColor: RGBColor{R: 0, G: 0, B: 0},
		},
		cellStyle: CellStyle{
			BorderStyle: BorderStyle{
				Top: true, Left: true, Right: true, Bottom: true,
				Width:    0.5,
				RGBColor: RGBColor{R: 0, G: 0, B: 0},
			},
			TextColor: RGBColor{R: 0, G: 0, B: 0},
		},
	}
}

// Adds a column to the table with the specified header, width, and alignment
func (t *tableLayout) AddColumn(header string, width float64, align string) {
	t.columns = append(t.columns, column{header, width, align})
}

// Adds a row of data to the table
func (t *tableLayout) AddRow(row []string) {
	rowCell := make([]RowCell, len(row))
	for i, cell := range row {
		rowCell[i] = newStyledRowCell(cell, false, CellStyle{})
	}
	t.rows = append(t.rows, rowCell)
}

// Adds a row of data to the table with individual styled cells
// Useful for styling individual cells in a row
func (t *tableLayout) AddStyledRow(row []RowCell) {
	t.rows = append(t.rows, row)
}

// Sets the style for the entire table
func (t *tableLayout) SetTableStyle(style CellStyle) {
	t.tableStyle = style
}

// Sets the style for the header row
func (t *tableLayout) SetHeaderStyle(style CellStyle) {
	t.headerStyle = style
}

// Sets the style for regular cells
func (t *tableLayout) SetCellStyle(style CellStyle) {
	t.cellStyle = style
}

// SetAutoRowHeight enables content-based row heights (see TableLayout).
func (t *tableLayout) SetAutoRowHeight(auto bool) {
	t.autoRowHeight = auto
}

// SetPaginate enables multi-page tables with repeated headers (see TableLayout).
func (t *tableLayout) SetPaginate(paginate bool) {
	t.paginate = paginate
}

// DrawTable the entire table on the PDF
func (t *tableLayout) DrawTable() error {
	x := t.startX
	y := t.startY

	// Page bounds for pagination (units).
	pageH := float64(t.pdf.config.PageSize.H)
	bottomLimit := pageH - t.pdf.margins.Bottom - t.rowHeight
	if bottomLimit <= y {
		bottomLimit = pageH // failsafe
	}

	// segments track per-page table extents for border drawing; headerH is
	// the drawn header height of that segment (differs from rowHeight when
	// auto row heights are enabled and the header wraps)
	type segment struct {
		y1, y2  float64
		headerH float64
	}
	var segments []segment
	segStart := y
	segHeaderH := t.headerRowHeight()

	drawHeader := func(y float64) (float64, error) {
		x := t.startX
		h := t.headerRowHeight()
		segHeaderH = h
		for _, col := range t.columns {
			if err := t.drawCell(
				x,
				y,
				col.width,
				h,
				col.header,
				"center",
				true, /*isHeader*/
				t.headerStyle,
			); err != nil {
				return y, err
			}
			x += col.width
		}
		return y + h, nil
	}

	var err error
	y, err = drawHeader(y)
	if err != nil {
		return err
	}

	breakPage := func() error {
		segments = append(segments, segment{segStart, y, segHeaderH})
		t.pdf.AddPage()
		y = t.pdf.margins.Top
		if y <= 0 {
			y = 20
		}
		segStart = y
		x = t.startX
		var err error
		y, err = drawHeader(y)
		return err
	}

	// Draw the data rows
	for _, row := range t.rows {
		rowH := t.dataRowHeight(row)
		if t.paginate && y+rowH > bottomLimit {
			if err := breakPage(); err != nil {
				return err
			}
		}
		x = t.startX
		colIdx := 0
		for _, cell := range row {
			if colIdx >= len(t.columns) {
				break
			}
			span := cell.colspan
			if span < 1 {
				span = 1
			}
			cellWidth := 0.0
			for s := 0; s < span && colIdx+s < len(t.columns); s++ {
				cellWidth += t.columns[colIdx+s].width
			}
			cellStyle := t.cellStyle
			if cell.useCellStyle {
				cellStyle = cell.cellStyle
			}
			if err := t.drawCell(
				x,
				y,
				cellWidth,
				rowH,
				cell.content,
				t.columns[colIdx].align,
				false, /*isHeader*/
				cellStyle,
			); err != nil {
				return err
			}
			x += cellWidth
			colIdx += span
		}
		// border any remaining columns of a short row
		for ; colIdx < len(t.columns); colIdx++ {
			if err := t.drawCell(
				x,
				y,
				t.columns[colIdx].width,
				rowH,
				"",
				t.columns[colIdx].align,
				false, /*isHeader*/
				t.cellStyle,
			); err != nil {
				return err
			}
			x += t.columns[colIdx].width
		}
		y += rowH
	}

	// Fill any remaining rows with empty cells
	fillerCount := t.maxRows - len(t.rows)
	if fillerCount < 0 {
		fillerCount = 0
	}
	for i := 0; i < fillerCount; i++ {
		rowH := t.rowHeight
		if t.paginate && y+rowH > bottomLimit {
			if err := breakPage(); err != nil {
				return err
			}
		}
		x = t.startX
		for _, col := range t.columns {
			if err := t.drawCell(
				x,
				y,
				col.width,
				rowH,
				"",
				col.align,
				false, /*isHeader*/
				t.cellStyle,
			); err != nil {
				return err
			}
			x += col.width
		}
		y += rowH
	}
	segments = append(segments, segment{segStart, y, segHeaderH})

	// Draw the outer border of each page segment (plus the header rule).
	for _, seg := range segments {
		if err := t.drawSegmentBorder(seg.y1, seg.y2, seg.headerH); err != nil {
			return err
		}
	}

	return nil
}

// headerRowHeight returns the drawn height of the header row.
func (t *tableLayout) headerRowHeight() float64 {
	h := t.rowHeight
	if !t.autoRowHeight {
		return h
	}
	// measure each header within its column width
	for _, col := range t.columns {
		measured, err := t.wrapLineCount(col.header, col.width-2*t.padding, t.currentFontSize())
		if err == nil {
			mh := float64(measured)*t.currentFontSize()*1.25 + 2*t.padding
			if mh > h {
				h = mh
			}
		}
	}
	return h
}

// currentFontSize returns the currently selected font size (or 12).
func (t *tableLayout) currentFontSize() float64 {
	if t.pdf.curr.FontSize > 0 {
		return t.pdf.curr.FontSize
	}
	return 12
}

// dataRowHeight computes the height of a data row.
func (t *tableLayout) dataRowHeight(row []RowCell) float64 {
	h := t.rowHeight
	if !t.autoRowHeight {
		return h
	}
	for _, cell := range row {
		span := cell.colspan
		if span < 1 {
			span = 1
		}
		width := 0.0
		for i := 0; i < span; i++ {
			width += t.columns[i].width
		}
		measured, err := t.measureRowHeight([]RowCell{cell}, width)
		if err == nil && measured > h {
			h = measured
		}
	}
	return h
}

// measureRowHeight measures the height needed for wrapped cell content.
func (t *tableLayout) measureRowHeight(row []RowCell, width float64) (float64, error) {
	maxLines := 1
	for _, cell := range row {
		fontSize := t.pdf.curr.FontSize
		if cell.useCellStyle && cell.cellStyle.FontSize > 0 {
			fontSize = cell.cellStyle.FontSize
		}
		if fontSize <= 0 {
			fontSize = 12
		}
		contentWidth := width - 2*t.padding
		if contentWidth <= 0 {
			contentWidth = width
		}
		lines, err := t.wrapLineCount(cell.content, contentWidth, fontSize)
		if err != nil {
			return 0, err
		}
		if lines > maxLines {
			maxLines = lines
		}
	}
	lineH := t.pdf.curr.FontSize * 1.25
	if t.pdf.curr.FontSize <= 0 {
		lineH = 15
	}
	return float64(maxLines)*lineH + 2*t.padding, nil
}

// wrapLineCount counts wrapped lines for text at a given size within width.
func (t *tableLayout) wrapLineCount(text string, width, fontSize float64) (int, error) {
	if strings.TrimSpace(text) == "" {
		return 1, nil
	}
	words := strings.Fields(text)
	if len(words) == 0 {
		return 1, nil
	}
	// measure with the requested font metrics
	spaceW, err := t.pdf.MeasureTextWidth(" ")
	if err != nil {
		return 0, err
	}
	lines := 1
	lineW := 0.0
	for _, word := range words {
		w, err := t.pdf.MeasureTextWidth(word)
		if err != nil {
			return 0, err
		}
		// scale: MeasureTextWidth uses the current font size
		if t.pdf.curr.FontSize > 0 && t.pdf.curr.FontSize != fontSize {
			w = w * fontSize / t.pdf.curr.FontSize
		}
		if lineW > 0 && lineW+spaceW+w > width {
			lines++
			lineW = w
		} else if lineW > 0 {
			lineW += spaceW + w
		} else {
			lineW = w
		}
	}
	return lines, nil
}

// drawSegmentBorder draws the outer border of one page segment of the table.
func (t *tableLayout) drawSegmentBorder(y1, y2, headerH float64) error {
	x1 := t.startX
	x2 := t.startX

	for _, col := range t.columns {
		x2 += col.width
	}

	// Draw borders of the table segment
	if err := t.drawBorder(x1, y1, x2, y2, t.tableStyle.BorderStyle); err != nil {
		return err
	}

	// Draw borders of the header (height as actually drawn)
	return t.drawBorder(x1, y1, x2, y1+headerH, t.headerStyle.BorderStyle)
}

// Draws a single cell of the table
func (t *tableLayout) drawCell(
	x float64,
	y float64,
	width float64,
	height float64,
	content string,
	align string,
	isHeader bool,
	style CellStyle,
) error {
	// Fill the cell background if a fill color is specified
	if style.FillColor != (RGBColor{}) {
		t.pdf.SetFillColor(style.FillColor.R, style.FillColor.G, style.FillColor.B)
		t.pdf.RectFromUpperLeftWithStyle(x, y, width, height, "F")
	}

	if !isHeader {
		// Draw the cell border
		if err := t.drawBorder(x, y, x+width, y+height, style.BorderStyle); err != nil {
			return err
		}
	}

	// Calculate the text area within the cell
	textX := x + t.padding
	textY := y + t.padding
	textWidth := width - (2 * t.padding)
	textHeight := height - (2 * t.padding)

	t.pdf.SetXY(textX, textY)

	// Set the text alignment
	var textOption = t.cellOption
	if align == "right" {
		textOption.Align = Right | Middle
	} else if align == "center" {
		textOption.Align = Center | Middle
	} else {
		textOption.Align = Left | Middle
	}

	// Set the text color and font
	t.pdf.SetTextColor(style.TextColor.R, style.TextColor.G, style.TextColor.B)
	// A zero font size would render invisible text — keep the current size.
	if style.Font != "" && style.FontSize > 0 {
		t.pdf.SetFont(style.Font, "", style.FontSize)
	}

	// Draw the cell content
	err := t.pdf.MultiCellWithOption(&Rect{W: textWidth, H: textHeight}, content, textOption)
	if err != nil && err.Error() == "empty string" {
		err = nil
	}

	return err
}

// Draws a border around a rectangular area
func (t *tableLayout) drawBorder(x1, y1, x2, y2 float64, borderStyle BorderStyle) error {
	if borderStyle.Width <= 0 {
		return nil
	}
	t.pdf.SetLineWidth(borderStyle.Width)
	t.pdf.SetStrokeColor(borderStyle.RGBColor.R, borderStyle.RGBColor.G, borderStyle.RGBColor.B)
	half := borderStyle.Width / 2.0

	// Draw each side of the border if specified
	if borderStyle.Top {
		t.pdf.Line(x1-half, y1, x2+half, y1)
	}
	if borderStyle.Bottom {
		t.pdf.Line(x1-half, y2, x2+half, y2)
	}
	if borderStyle.Left {
		t.pdf.Line(x1, y1-half, x1, y2+half)
	}
	if borderStyle.Right {
		t.pdf.Line(x2, y1-half, x2, y2+half)
	}

	return nil
}
