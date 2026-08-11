package excel

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	gospreadsheet "github.com/VantageDataChat/GoExcel"
	legacyxls "github.com/shakinm/xlsReader/xls"
)

// ReadFile reads cell data from an XLSX, XLS, or CSV file.
// CSV and legacy BIFF XLS files are auto-detected by extension
// (case-insensitive).
func ReadFile(filePath string, opts ReadOptions) (result *ReadResult, err error) {
	defer recoverSpreadsheetRead(&result, &err)
	// Check if file exists
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return nil, fmt.Errorf("文件不存在: %s", filePath)
	}

	ext := strings.ToLower(filepath.Ext(filePath))
	if ext == ".csv" {
		return readCSV(filePath, opts)
	}
	if ext == ".xls" {
		return readXLS(filePath, opts)
	}
	return readXLSX(filePath, opts)
}

// ReadAllSheets reads every worksheet while keeping one workbook open. It
// supports OOXML XLSX, legacy BIFF XLS, and CSV. Bulk import callers use this
// instead of ListSheets followed by one ReadFile call per sheet, which would
// repeatedly parse the same container.
func ReadAllSheets(filePath string) (results []*ReadResult, err error) {
	defer recoverAllSpreadsheetReads(&results, &err)
	if _, err := os.Stat(filePath); err != nil {
		return nil, fmt.Errorf("文件不存在或无法访问")
	}
	if strings.EqualFold(filepath.Ext(filePath), ".csv") {
		result, err := readCSV(filePath, ReadOptions{})
		if err != nil {
			return nil, err
		}
		return []*ReadResult{result}, nil
	}
	if strings.EqualFold(filepath.Ext(filePath), ".xls") {
		return readAllXLSSheets(filePath)
	}

	wb, err := gospreadsheet.OpenFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("读取文件失败")
	}
	defer wb.Close()

	names := wb.GetSheetNames()
	results = make([]*ReadResult, 0, len(names))
	for _, name := range names {
		ws, err := wb.GetSheetByName(name)
		if err != nil {
			return nil, fmt.Errorf("读取工作表失败")
		}
		result, err := readWorksheet(ws, ReadOptions{SheetName: name})
		if err != nil {
			return nil, err
		}
		results = append(results, result)
	}
	return results, nil
}

// ListSheets returns the sheet names in a workbook.
// For CSV files, returns ["Sheet1"].
func ListSheets(filePath string) (names []string, err error) {
	defer recoverSpreadsheetSheetNames(&names, &err)
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return nil, fmt.Errorf("文件不存在: %s", filePath)
	}

	ext := strings.ToLower(filepath.Ext(filePath))
	if ext == ".csv" {
		return []string{"Sheet1"}, nil
	}
	if ext == ".xls" {
		results, err := readAllXLSSheets(filePath)
		if err != nil {
			return nil, err
		}
		names := make([]string, 0, len(results))
		for _, result := range results {
			if result != nil {
				names = append(names, result.SheetName)
			}
		}
		return names, nil
	}

	wb, err := gospreadsheet.OpenFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("读取文件失败: %v", err)
	}
	defer wb.Close()

	return wb.GetSheetNames(), nil
}

// readAllXLSSheets adapts the legacy BIFF reader to the public spreadsheet
// result model. Keeping this conversion in corelib/excel means knowledge
// import, read_excel, and other structured consumers see the same .xls shape
// instead of each maintaining a weaker text-only fallback.
func readAllXLSSheets(filePath string) ([]*ReadResult, error) {
	wb, err := legacyxls.OpenFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("read legacy XLS: %w", err)
	}
	count := wb.GetNumberSheets()
	if count == 0 {
		return nil, fmt.Errorf("legacy XLS contains no worksheets")
	}
	results := make([]*ReadResult, 0, count)
	for index := 0; index < count; index++ {
		sheet, err := wb.GetSheet(index)
		if err != nil || sheet == nil {
			return nil, fmt.Errorf("read legacy XLS worksheet %d: %w", index+1, err)
		}
		rows := make([][]CellValue, sheet.GetNumberRows())
		maxCols := 0
		for rowIndex := range rows {
			row, err := sheet.GetRow(rowIndex)
			if err != nil || row == nil {
				return nil, fmt.Errorf("read legacy XLS row %d: %w", rowIndex+1, err)
			}
			cells := row.GetCols()
			if len(cells) > maxCols {
				maxCols = len(cells)
			}
			rows[rowIndex] = make([]CellValue, len(cells))
			for colIndex, cell := range cells {
				rows[rowIndex][colIndex] = mapLegacyXLSCell(cell.GetString(), cell.GetType())
			}
		}
		for rowIndex := range rows {
			if len(rows[rowIndex]) < maxCols {
				padded := make([]CellValue, maxCols)
				copy(padded, rows[rowIndex])
				for colIndex := len(rows[rowIndex]); colIndex < maxCols; colIndex++ {
					padded[colIndex] = CellValue{Type: CellTypeEmpty}
				}
				rows[rowIndex] = padded
			}
		}
		results = append(results, &ReadResult{
			SheetName: sheet.GetName(),
			Rows:      rows,
			RowCount:  len(rows),
			ColCount:  maxCols,
		})
	}
	return results, nil
}

func readXLS(filePath string, opts ReadOptions) (*ReadResult, error) {
	results, err := readAllXLSSheets(filePath)
	if err != nil {
		return nil, err
	}
	for _, result := range results {
		if result == nil || (opts.SheetName != "" && result.SheetName != opts.SheetName) {
			continue
		}
		return sliceLegacyXLSResult(result, opts)
	}
	if opts.SheetName != "" {
		return nil, fmt.Errorf("legacy XLS worksheet %q not found", opts.SheetName)
	}
	return nil, fmt.Errorf("legacy XLS contains no readable worksheets")
}

func sliceLegacyXLSResult(result *ReadResult, opts ReadOptions) (*ReadResult, error) {
	if result == nil {
		return nil, fmt.Errorf("legacy XLS worksheet is empty")
	}
	startRow, startCol := 0, 0
	endRow, endCol := len(result.Rows)-1, result.ColCount-1
	if opts.Range != "" {
		sc, sr, ec, er, err := ParseRange(opts.Range)
		if err != nil {
			return nil, err
		}
		startCol, startRow, endCol, endRow = sc-1, sr-1, ec-1, er-1
	}
	if startRow < 0 {
		startRow = 0
	}
	if startCol < 0 {
		startCol = 0
	}
	if endRow >= len(result.Rows) {
		endRow = len(result.Rows) - 1
	}
	if endCol >= result.ColCount {
		endCol = result.ColCount - 1
	}
	if startRow > endRow || startCol > endCol || endRow < 0 || endCol < 0 {
		return &ReadResult{SheetName: result.SheetName}, nil
	}
	truncated := false
	if opts.MaxRows > 0 && endRow-startRow+1 > opts.MaxRows {
		endRow = startRow + opts.MaxRows - 1
		truncated = true
	}
	rows := make([][]CellValue, 0, endRow-startRow+1)
	for rowIndex := startRow; rowIndex <= endRow; rowIndex++ {
		row := make([]CellValue, endCol-startCol+1)
		copy(row, result.Rows[rowIndex][startCol:endCol+1])
		rows = append(rows, row)
	}
	return &ReadResult{
		SheetName: result.SheetName,
		Rows:      rows,
		RowCount:  len(rows),
		ColCount:  endCol - startCol + 1,
		Truncated: truncated,
	}, nil
}

func mapLegacyXLSCell(text, kind string) CellValue {
	text = strings.TrimSpace(text)
	if text == "" {
		return CellValue{Type: CellTypeEmpty}
	}
	// xlsReader presents formatted numbers as strings. Preserve the original
	// textual representation while exposing simple numeric cells to structured
	// search and table type inference, consistent with the XLSX adapter.
	kind = strings.ToLower(kind)
	if number, err := strconv.ParseFloat(text, 64); err == nil && !strings.Contains(kind, "label") {
		return CellValue{Value: number, Type: CellTypeNumber}
	}
	if value, err := strconv.ParseBool(text); err == nil {
		return CellValue{Value: value, Type: CellTypeBool}
	}
	return CellValue{Value: text, Type: CellTypeString}
}

// The GoExcel parser consumes attacker-controlled OOXML/CSV input through
// several public callers (agent tools, knowledge import, and exports). Keep
// its public read seams fail-closed so a dependency panic cannot bring down a
// GUI worker or hand a caller a partially materialized workbook.
func recoverSpreadsheetRead(result **ReadResult, err *error) {
	if recover() != nil {
		*result = nil
		*err = fmt.Errorf("spreadsheet parser panicked")
	}
}

func recoverAllSpreadsheetReads(results *[]*ReadResult, err *error) {
	if recover() != nil {
		*results = nil
		*err = fmt.Errorf("spreadsheet parser panicked")
	}
}

func recoverSpreadsheetSheetNames(names *[]string, err *error) {
	if recover() != nil {
		*names = nil
		*err = fmt.Errorf("spreadsheet parser panicked")
	}
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

	return readWorksheet(ws, opts)
}

func readWorksheet(ws *gospreadsheet.Worksheet, opts ReadOptions) (*ReadResult, error) {
	if ws == nil {
		return nil, fmt.Errorf("读取工作表失败")
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
	truncated := false
	if opts.MaxRows > 0 && endRow-startRow+1 > opts.MaxRows {
		endRow = startRow + opts.MaxRows - 1
		truncated = true
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
		Truncated: truncated,
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

	startRow, startCol, endRow, endCol := 0, 0, -1, -1
	maxRowsLimited := opts.MaxRows > 0 && opts.Range == ""
	if opts.Range != "" {
		sc, sr, ec, er, parseErr := ParseRange(opts.Range)
		if parseErr != nil {
			return nil, parseErr
		}
		startCol, startRow, endCol, endRow = sc-1, sr-1, ec-1, er-1
		maxRowsLimited = opts.MaxRows > 0 && endRow-startRow+1 > opts.MaxRows
	}
	if opts.MaxRows > 0 && (endRow < 0 || endRow-startRow+1 > opts.MaxRows) {
		endRow = startRow + opts.MaxRows - 1
	}
	var selectedRows [][]string
	maxCols := 0
	rowIndex := 0
	truncated := false
	for {
		record, readErr := reader.Read()
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return nil, fmt.Errorf("CSV 解析失败: %v", readErr)
		}
		if rowIndex < startRow {
			rowIndex++
			continue
		}
		if endRow >= 0 && rowIndex > endRow {
			truncated = maxRowsLimited
			break
		}
		selectedRows = append(selectedRows, record)
		if len(record) > maxCols {
			maxCols = len(record)
		}
		rowIndex++
	}

	if len(selectedRows) == 0 {
		return &ReadResult{
			SheetName: "Sheet1",
			Rows:      [][]CellValue{},
			RowCount:  0,
			ColCount:  0,
		}, nil
	}

	if endCol < 0 {
		endCol = maxCols - 1
	}
	if endCol < startCol {
		return &ReadResult{SheetName: "Sheet1", Rows: [][]CellValue{}, RowCount: 0, ColCount: 0, Truncated: truncated}, nil
	}
	rowCount := len(selectedRows)
	colCount := endCol - startCol + 1
	rows := make([][]CellValue, rowCount)

	for i := 0; i < rowCount; i++ {
		row := make([]CellValue, colCount)
		srcRow := selectedRows[i]
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
		Truncated: truncated,
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
