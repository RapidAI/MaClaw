package main

import (
	"fmt"
	"os"

	gopdf "github.com/VantageDataChat/GoPDF2"
)

func main() {
	pdf := gopdf.GoPdf{}
	err := pdf.OpenPDF("test.pdf", nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "OpenPDF error: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Opened test.pdf, pages: %d\n", pdf.GetNumberOfPages())
}
