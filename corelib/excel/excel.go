package excel

// CellValue represents a single cell's value with type information.
type CellValue struct {
	Value   interface{} `json:"value"`             // string, float64, bool, or nil
	Type    CellType    `json:"type"`              // "string", "number", "bool", "formula", "empty"
	Formula string      `json:"formula,omitempty"` // original formula if Type == CellTypeFormula
}

// CellType represents the type of a cell value.
type CellType string

const (
	CellTypeString  CellType = "string"
	CellTypeNumber  CellType = "number"
	CellTypeBool    CellType = "bool"
	CellTypeFormula CellType = "formula"
	CellTypeEmpty   CellType = "empty"
)

// ReadOptions configures a read operation.
type ReadOptions struct {
	SheetName string // empty = first sheet
	Range     string // A1 notation, empty = all non-empty cells
	// MaxRows caps returned rows after any range start. Zero means no additional
	// row cap. It is used by structured tool callers to keep JSON payloads and
	// intermediate row grids bounded.
	MaxRows int
}

// ReadResult contains the data read from a spreadsheet.
type ReadResult struct {
	SheetName string        `json:"sheet_name"`
	Rows      [][]CellValue `json:"rows"`
	RowCount  int           `json:"row_count"`
	ColCount  int           `json:"col_count"`
	Truncated bool          `json:"truncated,omitempty"`
}

// SheetStyle defines formatting for a cell.
type SheetStyle struct {
	Bold            bool   `json:"bold,omitempty"`
	FontSize        int    `json:"font_size,omitempty"`
	BackgroundColor string `json:"background_color,omitempty"` // hex color e.g. "#FF0000"
	NumberFormat    string `json:"number_format,omitempty"`
}

// WriteCell represents a single cell to write.
type WriteCell struct {
	Value interface{} `json:"value"` // string, float64, bool
	Style *SheetStyle `json:"style,omitempty"`
}

// WriteSheet represents a sheet to write.
type WriteSheet struct {
	Name string        `json:"name"`
	Rows [][]WriteCell `json:"rows"`
}

// WriteData is the top-level structure for write operations.
type WriteData struct {
	Sheets []WriteSheet `json:"sheets"`
}
