package gopdf

import (
	"fmt"
	"os"
	"path/filepath"
)

// The Example functions document the primary APIs on pkg.go.dev and are
// executed by `go test` (output assertions below keep them honest).

func buildExampleDoc(dir string) ([]byte, error) {
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	if err := pdf.AddTTFFont("lib", "test/res/LiberationSerif-Regular.ttf"); err != nil {
		return nil, err
	}
	if err := pdf.SetFont("lib", "", 12); err != nil {
		return nil, err
	}
	pdf.AddPage()
	pdf.SetXY(50, 60)
	_ = pdf.Cell(nil, "Example document text")
	pdf.SetXY(50, 90)
	_ = pdf.Cell(nil, "Second line of the example")
	path := filepath.Join(dir, "example_doc.pdf")
	if err := pdf.WritePdf(path); err != nil {
		return nil, err
	}
	return os.ReadFile(path)
}

func ExampleOpenPDFDocument() {
	data, _ := buildExampleDoc(os.TempDir())
	doc, err := OpenPDFDocument(data)
	if err != nil {
		fmt.Println("open:", err)
		return
	}
	for p := 0; p < doc.PageCount(); p++ {
		text, _ := doc.Text(p)
		fmt.Print(text)
	}
	// Output: Example document text
	// Second line of the example
}

func ExampleSearchTextWithOptions() {
	data, _ := buildExampleDoc(os.TempDir())
	res, _ := SearchTextWithOptions(data, "second line", SearchOptions{
		CaseInsensitive: true,
		Dehyphenate:     true,
	})
	for _, r := range res {
		fmt.Printf("page %d at %.0f,%.0f\n", r.PageIndex, r.X, r.Y)
	}
	// Output: page 0 at 50,86
}

func ExampleExtractPageLines() {
	data, _ := buildExampleDoc(os.TempDir())
	lines, err := ExtractPageLines(data, 0)
	if err != nil {
		fmt.Println("extract:", err)
		return
	}
	for _, ln := range lines {
		fmt.Println(ln.Text)
	}
	// Output: Example document text
	// Second line of the example
}
