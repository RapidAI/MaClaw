package excel

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	gospreadsheet "github.com/VantageDataChat/GoExcel"
)

// ReadFile reads cell data from an XLSX or CSV file.
// CSV files are auto-detected by .csv extension (case-insensitive).
func ReadFile(filePath string, opts ReadOptions) (*ReadResult, error) {
	// Check if file exists
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return nil, fmt.Errorf("文件不存在: %s", filePath)
	}

	ext := strings.ToLower(filepath.Ext(filePath))
	if ext == ".csv" {
		return readCSV(filePath, opts)
	}
	return readXLSX(filePath, opts)
}

// ListSheets returns the sheet names in a workbook.
// For CSV files, returns ["Sheet1"].
func ListSheets(filePath string) ([]string, error) {
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return nil, fmt.Errorf("文件不存在: %s", filePath)
	}

	ext := strings.ToLower(filepath.Ext(filePath))
	if ext == ".csv" {
		return []string{"Sheet1"}, nil
	}

	wb, err := gospreadsheet.OpenFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("读取文件失败: %v", err)
	}
	defer wb.Close()

	return wb.GetSheetNames(), nil
}

// readXLSX reads an XLSX file using GoExcel.
func readXLSX(filePath string, opts ReadOptions) (*ReadResult, error) {
	wb, err := gospreadsheet.OpenFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("读取文件失败: %v", err)
	}
	defer wb.Close()

	// Select sheet
	var ws *gospreadsheet.Worksheet
	if opts.SheetName == "" {
		ws, err = wb.GetSheet(0)
		if err != nil {
			return nil, fmt.Errorf("读取文件失败: %v", err)
		}
	} else {
		ws, err = wb.GetSheetByName(opts.SheetName)
		if err != nil {
			names := wb.GetSheetNames()
			return nil, fmt.Errorf("工作表 \"%s\" 不存在。可用的工作表: %s", opts.SheetName, strings.Join(names, ", "))
		}
	}

	sheetName := ws.Title()

	// Get dimensions
	minRow, minCol, maxRow, maxCol, err := ws.Dimensions()
	if err != nil {
		// Empty sheet
		return &ReadResult{
			SheetName: sheetName,
			Rows:      [][]CellValue{},
			RowCount:  0,
			ColCount:  0,
		}, nil
	}

	// Apply range filter if specified
	startRow, startCol, endRow, endCol := minRow, minCol, maxRow, maxCol
	if opts.Range != "" {
		sc, sr, ec, er, parseErr := ParseRange(opts.Range)
		if parseErr != nil {
			return nil, parseErr
		}
		// ParseRange returns 1-based col and row; GoExcel uses 0-based
		startCol = sc - 1
		startRow = sr - 1
		endCol = ec - 1
		endRow = er - 1
	}

	// Clamp to actual data bounds
	if endRow > maxRow {
		endRow = maxRow
	}
	if endCol > maxCol {
		endCol = maxCol
	}
	if startRow > endRow || startCol > endCol {
		return &ReadResult{
			SheetName: sheetName,
			Rows:      [][]CellValue{},
			RowCount:  0,
			ColCount:  0,
		}, nil
	}

	// Build result rows
	rowCount := endRow - startRow + 1
	colCount := endCol - startCol + 1
	rows := make([][]CellValue, rowCount)

	for i := 0; i < rowCount; i++ {
		row := make([]CellValue, colCount)
		for j := 0; j < colCount; j++ {
			cell := ws.GetCellIfExists(startRow+i, startCol+j)
			row[j] = mapGoExcelCell(cell)
		}
		rows[i] = row
	}

	return &ReadResult{
		SheetName: sheetName,
		Rows:      rows,
		RowCount:  rowCount,
		ColCount:  colCount,
	}, nil
}

// readCSV reads a CSV file using Go's standard encoding/csv package.
func readCSV(filePath string, opts ReadOptions) (*ReadResult, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("读取文件失败: %v", err)
	}
	defer f.Close()

	reader := csv.NewReader(f)
	reader.FieldsPerRecord = -1 // allow variable field count

	var allRows [][]string
	for {
		record, readErr := reader.Read()
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return nil, fmt.Errorf("CSV 解析失败: %v", readErr)
		}
		allRows = append(allRows, record)
	}

	if len(allRows) == 0 {
		return &ReadResult{
			SheetName: "Sheet1",
			Rows:      [][]CellValue{},
			RowCount:  0,
			ColCount:  0,
		}, nil
	}

	// Determine max columns
	maxCols := 0
	for _, row := range allRows {
		if len(row) > maxCols {
			maxCols = len(row)
		}
	}

	// Apply range filter if specified
	startRow, startCol, endRow, endCol := 0, 0, len(allRows)-1, maxCols-1
	if opts.Range != "" {
		sc, sr, ec, er, parseErr := ParseRange(opts.Range)
		if parseErr != nil {
			return nil, parseErr
		}
		// ParseRange returns 1-based; convert to 0-based
		startCol = sc - 1
		startRow = sr - 1
		endCol = ec - 1
		endRow = er - 1
	}

	// Clamp to actual data bounds
	if endRow >= len(allRows) {
		endRow = len(allRows) - 1
	}
	if startRow > endRow {
		return &ReadResult{
			SheetName: "Sheet1",
			Rows:      [][]CellValue{},
			RowCount:  0,
			ColCount:  0,
		}, nil
	}

	rowCount := endRow - startRow + 1
	colCount := endCol - startCol + 1
	rows := make([][]CellValue, rowCount)

	for i := 0; i < rowCount; i++ {
		row := make([]CellValue, colCount)
		srcRow := allRows[startRow+i]
		for j := 0; j < colCount; j++ {
			colIdx := startCol + j
			if colIdx < len(srcRow) {
				val := srcRow[colIdx]
				if val == "" {
					row[j] = CellValue{Value: nil, Type: CellTypeEmpty}
				} else {
					row[j] = CellValue{Value: val, Type: CellTypeString}
				}
			} else {
				row[j] = CellValue{Value: nil, Type: CellTypeEmpty}
			}
		}
		rows[i] = row
	}

	return &ReadResult{
		SheetName: "Sheet1",
		Rows:      rows,
		RowCount:  rowCount,
		ColCount:  colCount,
	}, nil
}

// mapGoExcelCell converts a GoExcel Cell to our CellValue type.
func mapGoExcelCell(cell *gospreadsheet.Cell) CellValue {
	if cell == nil {
		return CellValue{Value: nil, Type: CellTypeEmpty}
	}

	switch cell.Type {
	case gospreadsheet.CellTypeEmpty:
		return CellValue{Value: nil, Type: CellTypeEmpty}
	case gospreadsheet.CellTypeString:
		return CellValue{Value: cell.Value, Type: CellTypeString}
	case gospreadsheet.CellTypeNumeric:
		return CellValue{Value: cell.Value, Type: CellTypeNumber}
	case gospreadsheet.CellTypeBool:
		return CellValue{Value: cell.Value, Type: CellTypeBool}
	case gospreadsheet.CellTypeFormula:
		return CellValue{
			Value:   cell.Value,
			Type:    CellTypeFormula,
			Formula: cell.Formula,
		}
	case gospreadsheet.CellTypeDate:
		// Dates are represented as their string form
		return CellValue{Value: cell.GetStringValue(), Type: CellTypeString}
	case gospreadsheet.CellTypeError:
		return CellValue{Value: cell.GetStringValue(), Type: CellTypeString}
	default:
		return CellValue{Value: cell.GetStringValue(), Type: CellTypeString}
	}
}
