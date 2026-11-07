// Command showcase demonstrates the advanced features added to GoPDF2:
// paginated HTML rendering, formula drawing, text extraction with line
// merging, AES encryption, incremental save, and page rendering with
// text rasterization.
package main

import (
	"fmt"
	"log"
	"os"

	gopdf "github.com/VantageDataChat/GoPDF2"
)

func main() {
	demonstrateHTMLPaginated()
	demonstrateFormula()
	demonstrateTextExtraction()
	demonstrateEncryption()
	fmt.Println("All showcase examples completed successfully.")
}

// demonstrateHTMLPaginated shows multi-page HTML flow with tables,
// block backgrounds, and repeated table headers.
func demonstrateHTMLPaginated() {
	pdf := gopdf.GoPdf{}
	pdf.Start(gopdf.Config{PageSize: *gopdf.PageSizeA4})
	pdf.AddPage()
	if err := pdf.AddTTFFont("lib", "test/res/LiberationSerif-Regular.ttf"); err != nil {
		log.Fatal(err)
	}

	html := `
<style>
	h1 { font-size: 20pt; text-align: center; color: #1a3c6e; }
	.shaded { background-color: #e8f0ff; }
</style>
<h1>Quarterly Report</h1>
<div class="shaded">This summary block has a full-width background that
wraps across lines and supports pagination.</div>
<table border="1" width="100%">
	<thead><tr><th>Region</th><th>Revenue</th><th>Growth</th></tr></thead>
	<tbody>
		<tr><td>Asia-Pacific</td><td>31,200</td><td style="color:green">+5.3%</td></tr>
		<tr><td>North America</td><td>28,900</td><td style="color:green">+2.8%</td></tr>
		<tr><td rowspan="2">Europe</td><td>58,100</td><td>+1.9%</td></tr>
		<tr><td>Western</td><td>41,000</td><td>+1.7%</td></tr>
	</tbody>
</table>
<p>Entities: &copy; &mdash; &alpha;&beta; &le; &infin;</p>`
	for i := 0; i < 30; i++ {
		html += fmt.Sprintf("<p>Filler paragraph %d to force pagination across pages.</p>", i)
	}

	pdf.SetMargins(50, 50, 50, 50)
	_, err := pdf.InsertHTMLPaginated(50, 60, 495, html, gopdf.HTMLBoxOption{
		DefaultFontFamily:  "lib",
		DefaultFontSize:    11,
		PageTop:            50,
		PageBottom:         792 - 50,
		RepeatTableHeaders: true,
	})
	if err != nil {
		log.Fatal(err)
	}
	if err := pdf.WritePdf("showcase_html.pdf"); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("showcase_html.pdf: %d pages (paginated HTML with repeated headers)\n", pdf.GetNumberOfPages())
}

// demonstrateFormula shows the LaTeX-lite formula renderer.
func demonstrateFormula() {
	pdf := gopdf.GoPdf{}
	pdf.Start(gopdf.Config{PageSize: *gopdf.PageSizeA4})
	pdf.AddPage()
	if err := pdf.AddTTFFont("lib", "test/res/LiberationSerif-Regular.ttf"); err != nil {
		log.Fatal(err)
	}
	if err := pdf.SetFont("lib", "", 13); err != nil {
		log.Fatal(err)
	}
	formulas := []string{
		`E = mc^2`,
		`\int_{-\infty}^{\infty} e^{-x^2} dx = \sqrt{\pi}`,
		`\sum_{k=0}^{n} \binom{n}{k} = 2^n`,
	}
	y := 80.0
	for _, f := range formulas {
		h, err := pdf.DrawFormula(60, y, f, 14, &gopdf.FormulaOption{
			Color: &[3]uint8{0, 0, 128},
		})
		if err != nil {
			log.Fatal(err)
		}
		y += h + 30
	}
	if err := pdf.WritePdf("showcase_formula.pdf"); err != nil {
		log.Fatal(err)
	}
	fmt.Println("showcase_formula.pdf: formulas rendered")
}

// demonstrateTextExtraction shows position-aware text extraction with
// line merging on a real PDF.
func demonstrateTextExtraction() {
	data, err := os.ReadFile("test/corpus/arxiv_1706.03762.pdf")
	if err != nil {
		fmt.Println("corpus not available, skipping extraction demo")
		return
	}
	lines, err := gopdf.ExtractPageLines(data, 0)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("arXiv paper page 0: %d merged lines\n", len(lines))
	for i, ln := range lines {
		if i >= 5 {
			break
		}
		fmt.Printf("  %s\n", ln.Text)
	}
}

// demonstrateEncryption shows AES-256 encryption and password-aware
// text extraction.
func demonstrateEncryption() {
	pdf := gopdf.GoPdf{}
	pdf.Start(gopdf.Config{PageSize: *gopdf.PageSizeA4})
	if err := pdf.SetEncryption(gopdf.AESEncryptionConfig{
		Method:        gopdf.EncryptAES256,
		UserPassword:  "secret",
		OwnerPassword: "owner",
		Permissions:   gopdf.PermissionsPrint | gopdf.PermissionsCopy,
	}); err != nil {
		log.Fatal(err)
	}
	pdf.AddPage()
	if err := pdf.AddTTFFont("lib", "test/res/LiberationSerif-Regular.ttf"); err != nil {
		log.Fatal(err)
	}
	if err := pdf.SetFont("lib", "", 12); err != nil {
		log.Fatal(err)
	}
	pdf.SetXY(50, 50)
	_ = pdf.Cell(nil, "This document is AES-256 encrypted.")
	if err := pdf.WritePdf("showcase_encrypted.pdf"); err != nil {
		log.Fatal(err)
	}

	// Extract text with password
	data, _ := os.ReadFile("showcase_encrypted.pdf")
	texts, err := gopdf.ExtractTextFromAllPagesWithPassword(data, "secret")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("showcase_encrypted.pdf: AES-256 encrypted, %d pages, extraction with password OK\n", len(texts))
}
