package excel

import (
	"fmt"
	"strings"
)

// ParseRange parses an A1-notation range string into column/row bounds.
// Returns (startCol, startRow, endCol, endRow, error).
// Single cell "A1" returns (1,1,1,1,nil).
// Range "B2:D5" returns (2,2,4,5,nil).
// Multi-letter columns: "AA1:AB10" returns (27,1,28,10,nil).
func ParseRange(rangeStr string) (int, int, int, int, error) {
	rangeStr = strings.TrimSpace(rangeStr)
	if rangeStr == "" {
		return 0, 0, 0, 0, fmt.Errorf(`范围格式错误: "%s"。请使用 A1 表示法，例如 A1:D10`, rangeStr)
	}

	parts := strings.SplitN(rangeStr, ":", 2)

	startCol, startRow, err := parseCellRef(parts[0])
	if err != nil {
		return 0, 0, 0, 0, fmt.Errorf(`范围格式错误: "%s"。请使用 A1 表示法，例如 A1:D10`, rangeStr)
	}

	if len(parts) == 1 {
		// Single cell reference
		return startCol, startRow, startCol, startRow, nil
	}

	endCol, endRow, err := parseCellRef(parts[1])
	if err != nil {
		return 0, 0, 0, 0, fmt.Errorf(`范围格式错误: "%s"。请使用 A1 表示法，例如 A1:D10`, rangeStr)
	}

	return startCol, startRow, endCol, endRow, nil
}

// parseCellRef parses a single cell reference like "A1" or "AA10" into (col, row).
// Column letters are converted to 1-based integers (A=1, B=2, ..., Z=26, AA=27, ...).
// Row numbers are 1-based integers.
func parseCellRef(ref string) (int, int, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return 0, 0, fmt.Errorf("empty cell reference")
	}

	// Find the boundary between letters and digits
	i := 0
	for i < len(ref) && isAlpha(ref[i]) {
		i++
	}

	if i == 0 {
		return 0, 0, fmt.Errorf("no column letters in cell reference: %s", ref)
	}
	if i >= len(ref) {
		return 0, 0, fmt.Errorf("no row number in cell reference: %s", ref)
	}

	colStr := strings.ToUpper(ref[:i])
	rowStr := ref[i:]

	col := colLettersToNumber(colStr)
	if col <= 0 {
		return 0, 0, fmt.Errorf("invalid column letters: %s", colStr)
	}

	row := 0
	for _, ch := range rowStr {
		if ch < '0' || ch > '9' {
			return 0, 0, fmt.Errorf("invalid row number: %s", rowStr)
		}
		row = row*10 + int(ch-'0')
	}

	if row <= 0 {
		return 0, 0, fmt.Errorf("row number must be positive: %s", rowStr)
	}

	return col, row, nil
}

// colLettersToNumber converts column letters (A-ZZ) to a 1-based integer.
// A=1, B=2, ..., Z=26, AA=27, AB=28, ...
func colLettersToNumber(letters string) int {
	result := 0
	for _, ch := range letters {
		if ch < 'A' || ch > 'Z' {
			return 0
		}
		result = result*26 + int(ch-'A') + 1
	}
	return result
}

// isAlpha returns true if the byte is an ASCII letter.
func isAlpha(b byte) bool {
	return (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z')
}
