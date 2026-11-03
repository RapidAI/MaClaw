package main

import (
	"fmt"
	"os"
	"strings"

	gopdf "github.com/VantageDataChat/GoPDF2"
)

func main() {
	data, err := os.ReadFile("test.pdf")
	if err != nil {
		fmt.Fprintf(os.Stderr, "read file: %v\n", err)
		os.Exit(1)
	}

	// 获取页数
	n, err := gopdf.GetSourcePDFPageCountFromBytes(data)
	if err != nil {
		fmt.Fprintf(os.Stderr, "get page count: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Page count: %d\n", n)

	// 用 ExtractPageText 逐页提取
	var sb strings.Builder
	for i := 0; i < n; i++ {
		text, err := gopdf.ExtractPageText(data, i)
		if err != nil {
			fmt.Fprintf(os.Stderr, "page %d error: %v\n", i+1, err)
			continue
		}
		sb.WriteString(fmt.Sprintf("===== Page %d =====\n", i+1))
		sb.WriteString(text)
		sb.WriteString("\n\n")
	}

	result := sb.String()
	fmt.Printf("Extracted %d chars\n", len(result))

	if err := os.WriteFile("test_output.txt", []byte(result), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "write: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Saved to test_output.txt")

	// 预览
	preview := strings.TrimSpace(result)
	if len(preview) > 500 {
		preview = preview[:500] + "..."
	}
	fmt.Printf("\n--- Preview ---\n%s\n", preview)
}
