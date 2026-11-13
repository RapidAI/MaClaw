package gopdf

import (
	"os"
	"path/filepath"
	"testing"
)

// TestExtractTablesGenerated builds a ruled table with the table API and
// verifies structure detection returns the same rows/columns/text.
func TestExtractTablesGenerated(t *testing.T) {
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	if err := pdf.AddTTFFont("lib", "test/res/LiberationSerif-Regular.ttf"); err != nil {
		t.Fatal(err)
	}
	pdf.AddPage()
	if err := pdf.SetFont("lib", "", 10); err != nil {
		t.Fatal(err)
	}
	tbl := pdf.NewTableLayout(60, 60, 20, 3)
	tbl.AddColumn("Planet", 150, "left")
	tbl.AddColumn("Distance", 120, "center")
	tbl.AddColumn("Period", 120, "right")
	tbl.AddRow([]string{"Mercury", "0.39 AU", "88 days"})
	tbl.AddRow([]string{"Venus", "0.72 AU", "225 days"})
	tbl.AddRow([]string{"Earth", "1.00 AU", "365 days"})
	if err := tbl.DrawTable(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "tbl.pdf")
	if err := pdf.WritePdf(path); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)

	tables, err := ExtractTablesFromPage(data, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(tables) != 1 {
		t.Fatalf("got %d tables, want 1", len(tables))
	}
	tb := tables[0]
	if tb.ColCount != 3 {
		t.Errorf("col count = %d, want 3", tb.ColCount)
	}
	if len(tb.Rows) != 4 {
		t.Errorf("row count = %d, want 4 (header + 3)", len(tb.Rows))
	}
	// header + first data row text
	if tb.Rows[0][0].Text != "Planet" {
		t.Errorf("header cell = %q", tb.Rows[0][0].Text)
	}
	if tb.Rows[1][0].Text != "Mercury" || tb.Rows[1][1].Text != "0.39 AU" || tb.Rows[1][2].Text != "88 days" {
		t.Errorf("row 1 = %q | %q | %q", tb.Rows[1][0].Text, tb.Rows[1][1].Text, tb.Rows[1][2].Text)
	}
	if tb.Rows[3][0].Text != "Earth" {
		t.Errorf("row 3 cell 0 = %q, want Earth", tb.Rows[3][0].Text)
	}
}

// TestExtractTablesShowcase verifies detection on the HTML-rendered table of
// the showcase document.
func TestExtractTablesShowcase(t *testing.T) {
	data, err := readFileBytes("test/out/showcase/showcase.pdf")
	if err != nil {
		t.Skip("showcase not built")
	}
	tables, err := ExtractTablesFromPage(data, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(tables) == 0 {
		t.Fatalf("no table detected on showcase page 1")
	}
	tb := tables[0]
	if tb.ColCount != 3 {
		t.Errorf("col count = %d, want 3", tb.ColCount)
	}
	if len(tb.Rows) != 4 {
		t.Errorf("rows = %d, want 4", len(tb.Rows))
	}
	if tb.Rows[0][0].Text != "Region" {
		t.Errorf("header = %q", tb.Rows[0][0].Text)
	}
	if tb.Rows[1][0].Text != "North America" {
		t.Errorf("row 1 = %q", tb.Rows[1][0].Text)
	}
}

// TestTableToCSV verifies CSV export with quoting.
func TestTableToCSV(t *testing.T) {
	tb := Table{Rows: [][]TableCell{
		{{Text: "Planet"}, {Text: "Note"}},
		{{Text: "Mars"}, {Text: `has "moons", two`}},
	}}
	want := "Planet,Note\nMars,\"has \"\"moons\"\", two\"\n"
	if got := tb.ToCSV(); got != want {
		t.Errorf("ToCSV = %q, want %q", got, want)
	}
}
