package knowledge

import (
	"fmt"
	"os"
	"testing"

	gopdf2 "github.com/VantageDataChat/GoPDF2"
	"github.com/ledongthuc/pdf"
)

func TestPDFExtractCompare(t *testing.T) {
	filePath := `C:\Users\ma139\Desktop\resume\resume-academic.pdf`
	if _, err := os.Stat(filePath); err != nil {
		t.Skip("test file not available")
	}

	fmt.Println("========== ledongthuc/pdf ==========")
	f, reader, err := pdf.Open(filePath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	for i := 1; i <= reader.NumPage(); i++ {
		page := reader.Page(i)
		text, err := page.GetPlainText(nil)
		if err != nil {
			t.Fatalf("page %d: %v", i, err)
		}
		if i == 2 {
			runes := []rune(text)
			fmt.Printf("--- ledongthuc page 2 tail (400 chars) ---\n")
			if len(runes) > 400 {
				fmt.Printf("%s\n", string(runes[len(runes)-400:]))
			} else {
				fmt.Printf("%s\n", text)
			}
		}
	}

	fmt.Println("\n========== VantageDataChat/GoPDF2 ==========")
	pdfData, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	text2, err := gopdf2.ExtractAllPagesText(pdfData)
	if err != nil {
		t.Fatalf("gopdf2 ExtractAllPagesText: %v", err)
	}
	fmt.Printf("--- gopdf2 full text (last 500 chars) ---\n")
	runes2 := []rune(text2)
	if len(runes2) > 500 {
		fmt.Printf("%s\n", string(runes2[len(runes2)-500:]))
	} else {
		fmt.Printf("%s\n", text2)
	}

	// Also try per-page extraction
	fmt.Printf("\n--- gopdf2 page 2 ---\n")
	page2Text, err := gopdf2.ExtractPageText(pdfData, 1) // 0-indexed
	if err != nil {
		fmt.Printf("gopdf2 page 2 error: %v\n", err)
	} else {
		runes3 := []rune(page2Text)
		fmt.Printf("len=%d\n", len(runes3))
		if len(runes3) > 500 {
			fmt.Printf("(tail 500):\n%s\n", string(runes3[len(runes3)-500:]))
		} else {
			fmt.Printf("%s\n", page2Text)
		}
	}
}
