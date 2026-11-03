package gopdf_test

import (
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/VantageDataChat/GoPDF2"
)

func TestTable(t *testing.T) {
	// Create a new PDF document
	pdf := &gopdf.GoPdf{}
	// Start the PDF with a custom page size (we'll adjust it later)
	pdf.Start(gopdf.Config{PageSize: gopdf.Rect{W: 430, H: 200}})
	// Add a new page to the document
	pdf.AddPage()

	err := pdf.AddTTFFont("LiberationSerif-Regular", "./test/res/LiberationSerif-Regular.ttf")
	if err != nil {
		t.Fatalf("Error loading font: %v", err)
		return
	}

	err = pdf.SetFont("LiberationSerif-Regular", "", 11)
	if err != nil {
		t.Fatalf("Error set font: %v", err)
		return
	}
	err = pdf.AddTTFFont("Ubuntu-L.ttf", "./examples/outline_example/Ubuntu-L.ttf")
	if err != nil {
		t.Fatalf("Error loading font: %v", err)
		return
	}

	err = pdf.SetFont("Ubuntu-L.ttf", "", 11)
	if err != nil {
		t.Fatalf("Error set font: %v", err)
		return
	}

	// Set the starting Y position for the table
	tableStartY := 10.0
	// Set the left margin for the table
	marginLeft := 10.0

	// Create a new table layout
	table := pdf.NewTableLayout(marginLeft, tableStartY, 25, 5)

	// Add columns to the table
	table.AddColumn("CODE", 50, "left")
	table.AddColumn("DESCRIPTION", 200, "left")
	table.AddColumn("QTY.", 40, "right")
	table.AddColumn("PRICE", 60, "right")
	table.AddColumn("TOTAL", 60, "right")

	// Add rows to the table
	table.AddRow([]string{"001", "Product A", "2", "10.00", "20.00"})
	table.AddRow([]string{"002", "Product B", "1", "15.00", "15.00"})
	table.AddRow([]string{"003", "Product C", "3", "5.00", "15.00"})

	// Set the style for table cells
	table.SetTableStyle(gopdf.CellStyle{
		BorderStyle: gopdf.BorderStyle{
			Top:    true,
			Left:   true,
			Bottom: true,
			Right:  true,
			Width:  1.0,
		},
		FillColor: gopdf.RGBColor{R: 255, G: 255, B: 255},
		TextColor: gopdf.RGBColor{R: 0, G: 0, B: 0},
		FontSize:  10,
	})

	// Set the style for table header
	table.SetHeaderStyle(gopdf.CellStyle{
		BorderStyle: gopdf.BorderStyle{
			Top:      true,
			Left:     true,
			Bottom:   true,
			Right:    true,
			Width:    2.0,
			RGBColor: gopdf.RGBColor{R: 100, G: 150, B: 255},
		},
		FillColor: gopdf.RGBColor{R: 255, G: 200, B: 200},
		TextColor: gopdf.RGBColor{R: 255, G: 100, B: 100},
		Font:      "Ubuntu-L.ttf",
		FontSize:  12,
	})

	table.SetCellStyle(gopdf.CellStyle{
		BorderStyle: gopdf.BorderStyle{
			Right:    true,
			Bottom:   true,
			Width:    0.5,
			RGBColor: gopdf.RGBColor{R: 0, G: 0, B: 0},
		},
		FillColor: gopdf.RGBColor{R: 255, G: 255, B: 255},
		TextColor: gopdf.RGBColor{R: 0, G: 0, B: 0},
		Font:      "LiberationSerif-Regular",
		FontSize:  10,
	})

	// Draw the table
	err = table.DrawTable()
	if err != nil {
		t.Errorf("Error drawing table: %v", err)
	}

	// Save the PDF to the specified path
	err = pdf.WritePdf("examples/table/example_table.pdf")
	if err != nil {
		t.Errorf("Error saving PDF: %v", err)
	}
}

func TestTableCenter(t *testing.T) {
	// Create a new PDF document
	pdf := &gopdf.GoPdf{}
	// Start the PDF with a custom page size (we'll adjust it later)
	pdf.Start(gopdf.Config{PageSize: gopdf.Rect{W: 430, H: 200}})
	// Add a new page to the document
	pdf.AddPage()

	err := pdf.AddTTFFont("LiberationSerif-Regular", "./test/res/LiberationSerif-Regular.ttf")
	if err != nil {
		t.Fatalf("Error loading font: %v", err)
		return
	}

	err = pdf.SetFont("LiberationSerif-Regular", "", 11)
	if err != nil {
		t.Fatalf("Error set font: %v", err)
		return
	}
	err = pdf.AddTTFFont("Ubuntu-L.ttf", "./examples/outline_example/Ubuntu-L.ttf")
	if err != nil {
		t.Fatalf("Error loading font: %v", err)
		return
	}

	err = pdf.SetFont("Ubuntu-L.ttf", "", 11)
	if err != nil {
		t.Fatalf("Error set font: %v", err)
		return
	}

	// Set the starting Y position for the table
	tableStartY := 10.0
	// Set the left margin for the table
	marginLeft := 10.0

	// Create a new table layout
	table := pdf.NewTableLayout(marginLeft, tableStartY, 25, 5)

	// Add columns to the table
	table.AddColumn("CODE", 50, "center")
	table.AddColumn("DESCRIPTION", 200, "center")
	table.AddColumn("QTY.", 40, "center")
	table.AddColumn("PRICE", 60, "center")
	table.AddColumn("TOTAL", 60, "center")

	// Add rows to the table
	table.AddRow([]string{"001", "Product A", "2", "10.00", "20.00"})
	table.AddRow([]string{"002", "Product B", "1", "15.00", "15.00"})
	table.AddRow([]string{"003", "Product C", "3", "5.00", "15.00"})

	// Set the style for table cells
	table.SetTableStyle(gopdf.CellStyle{
		BorderStyle: gopdf.BorderStyle{
			Top:    true,
			Left:   true,
			Bottom: true,
			Right:  true,
			Width:  1.0,
		},
		FillColor: gopdf.RGBColor{R: 255, G: 255, B: 255},
		TextColor: gopdf.RGBColor{R: 0, G: 0, B: 0},
		FontSize:  10,
	})

	// Set the style for table header
	table.SetHeaderStyle(gopdf.CellStyle{
		BorderStyle: gopdf.BorderStyle{
			Top:      true,
			Left:     true,
			Bottom:   true,
			Right:    true,
			Width:    2.0,
			RGBColor: gopdf.RGBColor{R: 100, G: 150, B: 255},
		},
		FillColor: gopdf.RGBColor{R: 255, G: 200, B: 200},
		TextColor: gopdf.RGBColor{R: 255, G: 100, B: 100},
		Font:      "Ubuntu-L.ttf",
		FontSize:  12,
	})

	table.SetCellStyle(gopdf.CellStyle{
		BorderStyle: gopdf.BorderStyle{
			Right:    true,
			Bottom:   true,
			Width:    0.5,
			RGBColor: gopdf.RGBColor{R: 0, G: 0, B: 0},
		},
		FillColor: gopdf.RGBColor{R: 255, G: 255, B: 255},
		TextColor: gopdf.RGBColor{R: 0, G: 0, B: 0},
		Font:      "LiberationSerif-Regular",
		FontSize:  10,
	})

	// Draw the table
	err = table.DrawTable()
	if err != nil {
		t.Errorf("Error drawing table: %v", err)
	}

	// Save the PDF to the specified path
	err = pdf.WritePdf("examples/table/example_table_center.pdf")
	if err != nil {
		t.Errorf("Error saving PDF: %v", err)
	}
}

func TestTableWithStyledRows(t *testing.T) {
	// Create a new PDF document
	pdf := &gopdf.GoPdf{}
	// Start the PDF with a custom page size (we'll adjust it later)
	pdf.Start(gopdf.Config{PageSize: gopdf.Rect{W: 430, H: 200}})
	// Add a new page to the document
	pdf.AddPage()

	err := pdf.AddTTFFont("LiberationSerif-Regular", "./test/res/LiberationSerif-Regular.ttf")
	if err != nil {
		t.Fatalf("Error loading font: %v", err)
		return
	}

	err = pdf.SetFont("LiberationSerif-Regular", "", 11)
	if err != nil {
		t.Fatalf("Error set font: %v", err)
		return
	}
	err = pdf.AddTTFFont("Ubuntu-L.ttf", "./examples/outline_example/Ubuntu-L.ttf")
	if err != nil {
		t.Fatalf("Error loading font: %v", err)
		return
	}

	err = pdf.SetFont("Ubuntu-L.ttf", "", 11)
	if err != nil {
		t.Fatalf("Error set font: %v", err)
		return
	}

	// Set the starting Y position for the table
	tableStartY := 10.0
	// Set the left margin for the table
	marginLeft := 10.0

	// Create a new table layout
	table := pdf.NewTableLayout(marginLeft, tableStartY, 25, 5)

	// Add columns to the table
	table.AddColumn("CODE", 50, "left")
	table.AddColumn("DESCRIPTION", 200, "left")
	table.AddColumn("QTY.", 40, "right")
	table.AddColumn("PRICE", 60, "right")
	table.AddColumn("TOTAL", 60, "right")

	// Add rows to the table
	table.AddStyledRow([]gopdf.RowCell{
		gopdf.NewRowCell("001", gopdf.CellStyle{
			TextColor: gopdf.RGBColor{R: 255, G: 0, B: 0},
		}),
		gopdf.NewRowCell("Product A", gopdf.CellStyle{
			TextColor: gopdf.RGBColor{R: 255, G: 0, B: 0},
		}),
		gopdf.NewRowCell("2", gopdf.CellStyle{
			TextColor: gopdf.RGBColor{R: 255, G: 0, B: 0},
		}),
		gopdf.NewRowCell("10.00", gopdf.CellStyle{
			TextColor: gopdf.RGBColor{R: 255, G: 0, B: 0},
		}),
		gopdf.NewRowCell("20.00", gopdf.CellStyle{
			TextColor: gopdf.RGBColor{R: 255, G: 0, B: 0},
		}),
	})
	table.AddStyledRow([]gopdf.RowCell{
		gopdf.NewRowCell("002", gopdf.CellStyle{
			TextColor: gopdf.RGBColor{R: 0, G: 255, B: 0},
		}),
		gopdf.NewRowCell("Product B", gopdf.CellStyle{
			TextColor: gopdf.RGBColor{R: 0, G: 255, B: 0},
		}),
		gopdf.NewRowCell("1", gopdf.CellStyle{
			TextColor: gopdf.RGBColor{R: 0, G: 255, B: 0},
		}),
		gopdf.NewRowCell("15.00", gopdf.CellStyle{
			TextColor: gopdf.RGBColor{R: 0, G: 255, B: 0},
		}),
		gopdf.NewRowCell("15.00", gopdf.CellStyle{
			TextColor: gopdf.RGBColor{R: 0, G: 255, B: 0},
		}),
	})
	table.AddStyledRow([]gopdf.RowCell{
		gopdf.NewRowCell("003", gopdf.CellStyle{
			TextColor: gopdf.RGBColor{R: 255, G: 0, B: 0},
		}),
		gopdf.NewRowCell("Product C", gopdf.CellStyle{
			TextColor: gopdf.RGBColor{R: 0, G: 255, B: 0},
		}),
		gopdf.NewRowCell("3", gopdf.CellStyle{
			TextColor: gopdf.RGBColor{R: 0, G: 0, B: 255},
		}),
		gopdf.NewRowCell("5.00", gopdf.CellStyle{
			TextColor: gopdf.RGBColor{R: 255, G: 0, B: 0},
		}),
		gopdf.NewRowCell("15.00", gopdf.CellStyle{
			TextColor: gopdf.RGBColor{R: 0, G: 255, B: 0},
		}),
	})

	table.AddRow([]string{"004", "Product D", "7", "51.00", "1.00"})

	// Set the style for table cells
	table.SetTableStyle(gopdf.CellStyle{
		BorderStyle: gopdf.BorderStyle{
			Top:    true,
			Left:   true,
			Bottom: true,
			Right:  true,
			Width:  1.0,
		},
		FillColor: gopdf.RGBColor{R: 255, G: 255, B: 255},
		TextColor: gopdf.RGBColor{R: 0, G: 0, B: 0},
		FontSize:  10,
	})

	// Set the style for table header
	table.SetHeaderStyle(gopdf.CellStyle{
		BorderStyle: gopdf.BorderStyle{
			Top:      true,
			Left:     true,
			Bottom:   true,
			Right:    true,
			Width:    2.0,
			RGBColor: gopdf.RGBColor{R: 100, G: 150, B: 255},
		},
		FillColor: gopdf.RGBColor{R: 255, G: 200, B: 200},
		TextColor: gopdf.RGBColor{R: 255, G: 100, B: 100},
		Font:      "Ubuntu-L.ttf",
		FontSize:  12,
	})

	table.SetCellStyle(gopdf.CellStyle{
		BorderStyle: gopdf.BorderStyle{
			Right:    true,
			Bottom:   true,
			Width:    0.5,
			RGBColor: gopdf.RGBColor{R: 0, G: 0, B: 0},
		},
		FillColor: gopdf.RGBColor{R: 255, G: 255, B: 255},
		TextColor: gopdf.RGBColor{R: 0, G: 0, B: 0},
		Font:      "LiberationSerif-Regular",
		FontSize:  10,
	})

	// Draw the table
	err = table.DrawTable()
	if err != nil {
		t.Errorf("Error drawing table: %v", err)
	}

	// Save the PDF to the specified path
	err = pdf.WritePdf("examples/table/example_table_with_styled_row.pdf")
	if err != nil {
		t.Errorf("Error saving PDF: %v", err)
	}
}

// TestTable_AutoHeightAndPagination verifies auto row heights, multi-page
// tables with repeated headers, and colspan cells.
func TestTable_AutoHeightAndPagination(t *testing.T) {
	pdf := gopdf.GoPdf{}
	pdf.Start(gopdf.Config{PageSize: *gopdf.PageSizeA4})
	pdf.AddPage()
	if err := pdf.AddTTFFont("lib", "test/res/LiberationSerif-Regular.ttf"); err != nil {
		t.Fatal(err)
	}
	if err := pdf.SetFont("lib", "", 10); err != nil {
		t.Fatal(err)
	}
	pdf.SetMargins(50, 50, 50, 50)

	table := pdf.NewTableLayout(50, 60, 20, 0)
	table.AddColumn("ID", 60, "left")
	table.AddColumn("Description", 300, "left")
	table.AddColumn("Amount", 80, "right")
	table.SetAutoRowHeight(true)
	table.SetPaginate(true)

	// long description that wraps -> auto height row
	long := "This is a long description that must wrap across several lines inside the table cell to trigger auto row height measurement and verify that nothing overlaps."
	table.AddStyledRow([]gopdf.RowCell{
		gopdf.NewRowCell("1", gopdf.CellStyle{}),
		gopdf.NewRowCell(long, gopdf.CellStyle{}),
		gopdf.NewRowCell("10.00", gopdf.CellStyle{}),
	})
	// colspan row
	table.AddStyledRow([]gopdf.RowCell{
		gopdf.NewRowCellSpan("This cell spans the first two columns", gopdf.CellStyle{}, 2),
		gopdf.NewRowCell("99.50", gopdf.CellStyle{}),
	})
	// many rows to force pagination
	for i := 0; i < 60; i++ {
		table.AddRow([]string{itoaT(i+2), "row " + itoaT(i+2), "1.00"})
	}

	if err := table.DrawTable(); err != nil {
		t.Fatal(err)
	}
	path := "test_table_paginate.pdf"
	if err := pdf.WritePdf(path); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(path)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if n, err := gopdf.GetSourcePDFPageCountFromBytes(data); err != nil || n < 2 {
		t.Fatalf("expected multi-page table, got %d pages (err %v)", n, err)
	}
	// repeated header on later pages
	text, err := gopdf.ExtractAllPagesText(data)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(text, "Description") < 2 {
		t.Errorf("header not repeated across pages")
	}
}

func itoaT(i int) string {
	return strconv.Itoa(i)
}
