package excel

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	gospreadsheet "github.com/VantageDataChat/GoExcel"
)

// WriteFile creates or overwrites an XLSX file with the given data.
func WriteFile(filePath string, data WriteData) error {
	if len(data.Sheets) == 0 {
		return fmt.Errorf("data.sheets 不能为空")
	}

	// Create directories if needed
	dir := filepath.Dir(filePath)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("写入文件失败: %v", err)
		}
	}

	wb := gospreadsheet.NewEmpty()

	for _, sheet := range data.Sheets {
		ws, err := wb.AddSheet(sheet.Name)
		if err != nil {
			return fmt.Errorf("写入文件失败: %v", err)
		}

		for rowIdx, row := range sheet.Rows {
			for colIdx, cell := range row {
				if err := writeCell(ws, rowIdx, colIdx, cell); err != nil {
					return fmt.Errorf("写入文件失败: %v", err)
				}
			}
		}
	}

	if err := gospreadsheet.SaveFile(wb, filePath); err != nil {
		return fmt.Errorf("写入文件失败: %v", err)
	}

	return nil
}

// WriteFileRange writes rows into an A1-notation rectangle while preserving
// all cells and worksheets outside that rectangle. The target workbook must
// already exist when range is used with an existing file; a new workbook is
// created when filePath does not exist. Rows must fit entirely within the
// requested rectangle, otherwise the operation is rejected before any write.
// This deliberately keeps range writes separate from WriteFile, whose
// historical contract is to create/overwrite a workbook from scratch.
func WriteFileRange(filePath, sheetName, rangeText string, rows [][]WriteCell) error {
	if strings.TrimSpace(sheetName) == "" {
		return fmt.Errorf("sheet name cannot be empty")
	}
	startCol, startRow, endCol, endRow, err := ParseRange(rangeText)
	if err != nil {
		return err
	}
	if endCol < startCol || endRow < startRow {
		return fmt.Errorf("range end must not precede range start")
	}
	maxRows, maxCols := endRow-startRow+1, endCol-startCol+1
	if len(rows) > maxRows {
		return fmt.Errorf("rows exceed range height: got %d, max %d", len(rows), maxRows)
	}
	for i, row := range rows {
		if len(row) > maxCols {
			return fmt.Errorf("rows[%d] exceed range width: got %d, max %d", i, len(row), maxCols)
		}
	}
	if dir := filepath.Dir(filePath); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("create output directory: %w", err)
		}
	}

	if _, statErr := os.Stat(filePath); statErr == nil && strings.EqualFold(filepath.Ext(filePath), ".xlsx") {
		if err := patchExistingXLSXRange(filePath, sheetName, rangeText, rows); err != nil {
			return err
		}
		return nil
	}
	var wb *gospreadsheet.Workbook
	if _, statErr := os.Stat(filePath); statErr == nil {
		wb, err = gospreadsheet.OpenFile(filePath)
		if err != nil {
			return fmt.Errorf("open workbook: %w", err)
		}
	} else if !os.IsNotExist(statErr) {
		return fmt.Errorf("stat workbook: %w", statErr)
	} else {
		wb = gospreadsheet.NewEmpty()
	}

	ws, err := wb.GetSheetByName(sheetName)
	if err != nil {
		// A new file may create the requested sheet. For an existing workbook,
		// silently creating a sheet is surprising and can bypass an allowlist.
		if _, statErr := os.Stat(filePath); statErr == nil {
			return fmt.Errorf("sheet %q not found", sheetName)
		}
		ws, err = wb.AddSheet(sheetName)
		if err != nil {
			return fmt.Errorf("create sheet: %w", err)
		}
	}
	// Writing into a merged region is ambiguous (the top-left cell owns the
	// value while the other cells are placeholders). Reject any overlap rather
	// than silently producing a workbook whose displayed value differs from
	// the requested matrix.
	for _, merged := range ws.GetMergeCells() {
		if merged.StartRow <= endRow-1 && merged.EndRow >= startRow-1 && merged.StartCol <= endCol-1 && merged.EndCol >= startCol-1 {
			return fmt.Errorf("range overlaps merged cells")
		}
	}
	for rowIdx, row := range rows {
		for colIdx, cell := range row {
			if err := writeCell(ws, startRow-1+rowIdx, startCol-1+colIdx, cell); err != nil {
				return fmt.Errorf("write range cell: %w", err)
			}
		}
	}
	if err := gospreadsheet.SaveFile(wb, filePath); err != nil {
		return fmt.Errorf("save workbook: %w", err)
	}
	return nil
}

// writeCell writes a single cell value and style to the worksheet.
func writeCell(ws *gospreadsheet.Worksheet, row, col int, wc WriteCell) error {
	if wc.Value == nil {
		// Skip nil/empty cells
		return nil
	}

	c := ws.GetCell(row, col)

	switch v := wc.Value.(type) {
	case string:
		if strings.HasPrefix(v, "=") {
			// Formula cell: strip the leading '=' for SetFormula
			c.SetFormula(v[1:])
		} else {
			c.SetValue(v)
		}
	case float64:
		c.SetValue(v)
	case float32:
		c.SetValue(float64(v))
	case int:
		c.SetValue(float64(v))
	case int64:
		c.SetValue(float64(v))
	case int32:
		c.SetValue(float64(v))
	case bool:
		c.SetValue(v)
	default:
		// For any other type, convert to string
		c.SetValue(fmt.Sprintf("%v", v))
	}

	// Apply style if present
	if wc.Style != nil {
		style := buildStyle(wc.Style)
		if style != nil {
			c.SetStyle(style)
		}
	}

	return nil
}

// buildStyle converts our SheetStyle to a GoExcel Style.
func buildStyle(ss *SheetStyle) *gospreadsheet.Style {
	if ss == nil {
		return nil
	}

	style := gospreadsheet.NewStyle()
	hasContent := false

	// Font: bold and font size
	if ss.Bold || ss.FontSize > 0 {
		font := &gospreadsheet.Font{}
		if ss.Bold {
			font.Bold = true
		}
		if ss.FontSize > 0 {
			font.Size = float64(ss.FontSize)
		}
		style.SetFont(font)
		hasContent = true
	}

	// Background color
	if ss.BackgroundColor != "" {
		color := strings.TrimPrefix(ss.BackgroundColor, "#")
		style.SetFill(&gospreadsheet.Fill{
			Type:  "solid",
			Color: color,
		})
		hasContent = true
	}

	// Number format
	if ss.NumberFormat != "" {
		style.SetNumberFormat(&gospreadsheet.NumberFormat{
			FormatCode: ss.NumberFormat,
		})
		hasContent = true
	}

	if !hasContent {
		return nil
	}
	return style
}
