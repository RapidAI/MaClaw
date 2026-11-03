package main

import (
	"log"

	gopdf "github.com/VantageDataChat/GoPDF2"
)

func main() {
	pdf := gopdf.GoPdf{}
	pdf.Start(gopdf.Config{PageSize: *gopdf.PageSizeA4})
	pdf.AddPage()
	if err := pdf.AddTTFFont("lib", "test/res/LiberationSerif-Regular.ttf"); err != nil {
		log.Fatal(err)
	}
	if err := pdf.SetFont("lib", "", 20); err != nil {
		log.Fatal(err)
	}
	formulas := []string{
		`E = mc^2`,
		`x_i + y_j = z_{ij}`,
		`\frac{a+b}{c+d}`,
		`\sqrt{a^2 + b^2}`,
		`\sum_{k=0}^{n} k`,
		`e^{-x^2}`,
	}
	y := 80.0
	for _, f := range formulas {
		h, err := pdf.DrawFormula(50, y, f, 20, nil)
		if err != nil {
			log.Fatal(err)
		}
		y += h + 40
	}
	if err := pdf.WritePdf("test/out/formula_check.pdf"); err != nil {
		log.Fatal(err)
	}
}
