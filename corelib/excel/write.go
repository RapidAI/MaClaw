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
