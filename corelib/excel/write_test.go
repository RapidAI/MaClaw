package excel

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	gospreadsheet "github.com/VantageDataChat/GoExcel"
)

// TestWriteFileRangeRejectsMergedOverlap covers the merged-cell guard in
// WriteFileRange (write.go:111-115): a range overlapping an existing merged
// region must be rejected before any write, and a non-overlapping range must
// succeed while keeping the merge intact.
func TestWriteFileRangeRejectsMergedOverlap(t *testing.T) {
	path := filepath.Join(t.TempDir(), "merged.xlsx")
	mustCreateMergedWorkbook(t, path)

	before := mustReadFileBytes(t, path)

	err := WriteFileRange(path, "Data", "A1:C2", [][]WriteCell{
		{{Value: "x"}, {Value: "y"}, {Value: "z"}},
		{{Value: "x2"}, {Value: "y2"}, {Value: "z2"}},
	})
	if err == nil || !strings.Contains(err.Error(), "range overlaps merged cells") {
		t.Fatalf("overlapping range error = %v, want \"range overlaps merged cells\"", err)
	}
	if after := mustReadFileBytes(t, path); !bytes.Equal(before, after) {
		t.Fatal("rejected write modified the file")
	}

	// A range disjoint from the A1:B1 merge must be accepted.
	if err := WriteFileRange(path, "Data", "A3:B4", [][]WriteCell{
		{{Value: "r3c1"}, {Value: "r3c2"}},
		{{Value: "r4c1"}, {Value: "r4c2"}},
	}); err != nil {
		t.Fatalf("non-overlapping WriteFileRange: %v", err)
	}

	wb, err := gospreadsheet.OpenFile(path)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	defer wb.Close()
	ws, err := wb.GetSheetByName("Data")
	if err != nil {
		t.Fatalf("GetSheetByName: %v", err)
	}
	merges := ws.GetMergeCells()
	if len(merges) != 1 || merges[0].StartRow != 0 || merges[0].StartCol != 0 || merges[0].EndRow != 0 || merges[0].EndCol != 1 {
		t.Fatalf("merged regions after write = %#v, want single A1:B1 merge", merges)
	}
	if got := ws.GetCellIfExists(0, 0).GetStringValue(); got != "merged-owner" {
		t.Fatalf("merged owner cell = %q, want merged-owner", got)
	}
	if got := ws.GetCellIfExists(2, 0).GetStringValue(); got != "r3c1" {
		t.Fatalf("A3 = %q, want r3c1", got)
	}
	if got := ws.GetCellIfExists(3, 1).GetStringValue(); got != "r4c2" {
		t.Fatalf("B4 = %q, want r4c2", got)
	}
}

// TestWriteFileRangePreservesCellsOutsideRectangle verifies that a range write
// only touches the requested rectangle: values outside it and other worksheets
// stay intact. Style preservation is deliberately not asserted here: GoExcel's
// XLSX reader does not parse cell styles back into the in-memory model (only
// number formats are consulted for date detection), so an OpenFile->SaveFile
// round trip cannot carry bold/fill styles through regardless of what
// WriteFileRange does. The read API in read.go likewise exposes no style data.
func TestWriteFileRangePreservesCellsOutsideRectangle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "preserve.xlsx")
	if err := WriteFile(path, WriteData{Sheets: []WriteSheet{
		{Name: "Main", Rows: [][]WriteCell{
			{{Value: "h1", Style: &SheetStyle{Bold: true, BackgroundColor: "#FF0000"}}, {Value: "h2"}, {Value: "h3"}},
			{{Value: "keep-a2"}, {Value: "old-b2"}, {Value: "keep-c2"}},
			{{Value: "keep-a3"}, {Value: "old-b3"}, {Value: 42}},
		}},
		{Name: "Other", Rows: [][]WriteCell{
			{{Value: "untouched"}},
		}},
	}}); err != nil {
		t.Fatalf("WriteFile fixture: %v", err)
	}

	if err := WriteFileRange(path, "Main", "B2:B3", [][]WriteCell{
		{{Value: "new-b2"}},
		{{Value: "new-b3"}},
	}); err != nil {
		t.Fatalf("WriteFileRange: %v", err)
	}

	result, err := ReadFile(path, ReadOptions{SheetName: "Main"})
	if err != nil {
		t.Fatalf("ReadFile Main: %v", err)
	}
	want := [][]interface{}{
		{"h1", "h2", "h3"},
		{"keep-a2", "new-b2", "keep-c2"},
		{"keep-a3", "new-b3", float64(42)},
	}
	if result.RowCount != len(want) || result.ColCount != len(want[0]) {
		t.Fatalf("Main dimensions = %dx%d, want %dx%d", result.RowCount, result.ColCount, len(want), len(want[0]))
	}
	for r, row := range want {
		for c, w := range row {
			if got := result.Rows[r][c].Value; got != w {
				t.Fatalf("Main[%d][%d] = %#v, want %#v", r, c, got, w)
			}
		}
	}

	names, err := ListSheets(path)
	if err != nil {
		t.Fatalf("ListSheets: %v", err)
	}
	if len(names) != 2 || names[0] != "Main" || names[1] != "Other" {
		t.Fatalf("sheet names = %#v, want [Main Other]", names)
	}
	other, err := ReadFile(path, ReadOptions{SheetName: "Other"})
	if err != nil {
		t.Fatalf("ReadFile Other: %v", err)
	}
	if other.RowCount != 1 || other.ColCount != 1 || other.Rows[0][0].Value != "untouched" {
		t.Fatalf("Other sheet = %#v, want single untouched cell", other)
	}
}

// TestWriteFileRangeRejectsOversizedRows verifies that rows exceeding the
// rectangle's height or width are rejected before the file is opened for
// writing, leaving the file byte-identical.
func TestWriteFileRangeRejectsOversizedRows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "oversized.xlsx")
	if err := WriteFile(path, WriteData{Sheets: []WriteSheet{
		{Name: "Data", Rows: [][]WriteCell{{{Value: "seed"}}}},
	}}); err != nil {
		t.Fatalf("WriteFile fixture: %v", err)
	}
	before := mustReadFileBytes(t, path)

	err := WriteFileRange(path, "Data", "A1:B2", [][]WriteCell{
		{{Value: "a"}, {Value: "b"}},
		{{Value: "c"}, {Value: "d"}},
		{{Value: "e"}, {Value: "f"}},
	})
	if err == nil || !strings.Contains(err.Error(), "rows exceed range height") {
		t.Fatalf("height overflow error = %v, want \"rows exceed range height\"", err)
	}

	err = WriteFileRange(path, "Data", "A1:B2", [][]WriteCell{
		{{Value: "a"}, {Value: "b"}, {Value: "c"}},
	})
	if err == nil || !strings.Contains(err.Error(), "rows[0] exceed range width") {
		t.Fatalf("width overflow error = %v, want \"rows[0] exceed range width\"", err)
	}

	if after := mustReadFileBytes(t, path); !bytes.Equal(before, after) {
		t.Fatal("rejected writes modified the file")
	}
}

// TestWriteFileRangeRejectsMissingSheetOnExistingWorkbook verifies that a
// range write naming a sheet absent from an existing workbook fails rather
// than silently creating the sheet (write.go:99-101).
func TestWriteFileRangeRejectsMissingSheetOnExistingWorkbook(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing-sheet.xlsx")
	if err := WriteFile(path, WriteData{Sheets: []WriteSheet{
		{Name: "Data", Rows: [][]WriteCell{{{Value: "seed"}}}},
	}}); err != nil {
		t.Fatalf("WriteFile fixture: %v", err)
	}
	before := mustReadFileBytes(t, path)

	err := WriteFileRange(path, "Nope", "A1:B2", [][]WriteCell{{{Value: "x"}, {Value: "y"}}})
	if err == nil || !strings.Contains(err.Error(), `sheet "Nope" not found`) {
		t.Fatalf("missing sheet error = %v, want `sheet \"Nope\" not found`", err)
	}
	if after := mustReadFileBytes(t, path); !bytes.Equal(before, after) {
		t.Fatal("rejected write modified the file")
	}
}

// TestWriteFileRangeCreatesNewFileWithOffsetRange verifies the new-file path:
// the workbook and sheet are created on demand, and values land at the exact
// offset implied by a range whose start is not A1.
func TestWriteFileRangeCreatesNewFileWithOffsetRange(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "new.xlsx")

	if err := WriteFileRange(path, "Fresh", "C3:D4", [][]WriteCell{
		{{Value: "c3"}, {Value: "d3"}},
		{{Value: "c4"}, {Value: 7}},
	}); err != nil {
		t.Fatalf("WriteFileRange on new file: %v", err)
	}

	// Read an explicit A1-anchored window so the leading blank rows/columns
	// above and left of the range origin are visible; an unbounded read would
	// clamp to the populated cells and hide the offset.
	full, err := ReadFile(path, ReadOptions{SheetName: "Fresh", Range: "A1:D4"})
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if full.RowCount != 4 || full.ColCount != 4 {
		t.Fatalf("new sheet dimensions = %dx%d, want 4x4 (origin offset must materialize leading blanks)", full.RowCount, full.ColCount)
	}
	if got := full.Rows[0][0].Value; got != nil {
		t.Fatalf("A1 = %#v, want empty", got)
	}
	if got := full.Rows[2][2].Value; got != "c3" {
		t.Fatalf("C3 = %#v, want c3", got)
	}
	if got := full.Rows[2][3].Value; got != "d3" {
		t.Fatalf("D3 = %#v, want d3", got)
	}
	if got := full.Rows[3][2].Value; got != "c4" {
		t.Fatalf("C4 = %#v, want c4", got)
	}
	if got := full.Rows[3][3].Value; got != float64(7) {
		t.Fatalf("D4 = %#v, want 7", got)
	}

	ranged, err := ReadFile(path, ReadOptions{SheetName: "Fresh", Range: "C3:D4"})
	if err != nil {
		t.Fatalf("ReadFile range: %v", err)
	}
	if ranged.RowCount != 2 || ranged.ColCount != 2 || ranged.Rows[0][0].Value != "c3" || ranged.Rows[1][1].Value != float64(7) {
		t.Fatalf("ranged read = %#v", ranged)
	}
}

// mustCreateMergedWorkbook writes a one-sheet workbook whose A1:B1 region is
// merged, using the GoExcel API directly.
func mustCreateMergedWorkbook(t *testing.T, path string) {
	t.Helper()
	wb := gospreadsheet.NewEmpty()
	ws, err := wb.AddSheet("Data")
	if err != nil {
		t.Fatalf("AddSheet: %v", err)
	}
	ws.GetCell(0, 0).SetValue("merged-owner")
	if err := ws.MergeCells("A1:B1"); err != nil {
		t.Fatalf("MergeCells: %v", err)
	}
	if err := gospreadsheet.SaveFile(wb, path); err != nil {
		t.Fatalf("SaveFile: %v", err)
	}
}

func mustReadFileBytes(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
