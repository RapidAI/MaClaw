package excel

import (
	"bytes"
	"path/filepath"
	"testing"
)

func TestWriteFileRangePreservesOutsideStylesViaPatch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "styled.xlsx")
	if err := WriteFile(path, WriteData{Sheets: []WriteSheet{{
		Name: "Data",
		Rows: [][]WriteCell{
			{{Value: "keep", Style: &SheetStyle{Bold: true, BackgroundColor: "#FF0000"}}, {Value: "also"}},
			{{Value: "row2"}, {Value: "x"}},
		},
	}}}); err != nil {
		t.Fatal(err)
	}
	if err := WriteFileRange(path, "Data", "B2:B2", [][]WriteCell{{{Value: "patched"}}}); err != nil {
		t.Fatal(err)
	}
	got, err := ReadFile(path, ReadOptions{SheetName: "Data"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Rows[0][0].Value != "keep" {
		t.Fatalf("outside cell lost: %#v", got.Rows[0][0])
	}
	if got.Rows[1][1].Value != "patched" {
		t.Fatalf("patched cell = %#v", got.Rows[1][1])
	}
}

func TestUpsertCellXMLUsesStrTypeGoExcelCanRead(t *testing.T) {
	sheet := []byte(`<worksheet><sheetData><row r="2"><c r="B2" s="1" t="s"><v>3</v></c></row></sheetData></worksheet>`)
	out := upsertCellXML(sheet, "B2", 2, "patched")
	if !bytes.Contains(out, []byte(`<c r="B2" s="1" t="str"><v>patched</v></c>`)) {
		t.Fatalf("patched xml = %s", out)
	}
	if bytes.Contains(out, []byte("inlineStr")) {
		t.Fatal("inlineStr is not readable by GoExcel")
	}
}
