// Command pdfprobe extracts text from PDF files and prints per-page stats.
// Used to characterize the test corpus in test/corpus.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/VantageDataChat/GoPDF2"
)

func main() {
	var names []string
	dir := "."
	if len(os.Args) > 1 {
		info, err := os.Stat(os.Args[1])
		if err == nil && !info.IsDir() {
			names = append(names, os.Args[1])
			dir = filepath.Dir(os.Args[1])
		} else {
			dir = os.Args[1]
		}
	}
	if len(names) == 0 {
		entries, err := os.ReadDir(dir)
		if err != nil {
			fmt.Fprintln(os.Stderr, "read dir:", err)
			os.Exit(1)
		}
		for _, e := range entries {
			if strings.HasSuffix(strings.ToLower(e.Name()), ".pdf") {
				names = append(names, e.Name())
			}
		}
	}
	sort.Strings(names)

	for _, name := range names {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			fmt.Printf("=== %s: read error %v\n", name, err)
			continue
		}
		start := time.Now()
		pages, err := gopdf.ExtractTextFromAllPages(data)
		elapsed := time.Since(start)
		if err != nil {
			fmt.Printf("=== %s (%d KB): ERROR after %v: %v\n", name, len(data)/1024, elapsed, err)
			continue
		}
		totalChars := 0
		totalCJK := 0
		var sb strings.Builder
		for pg := 0; pg < len(pages); pg++ {
			for _, t := range pages[pg] {
				totalChars += len([]rune(t.Text))
				for _, r := range t.Text {
					if unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hiragana, r) || unicode.Is(unicode.Katakana, r) {
						totalCJK++
					}
				}
			}
			if pg < 2 {
				sb.WriteString(fmt.Sprintf("\n--- page %d (%d runs, merged lines) ---\n", pg, len(pages[pg])))
				lines, err := gopdf.ExtractPageLines(data, pg)
				if err == nil {
					n := len(lines)
					if n > 12 {
						n = 12
					}
					for _, ln := range lines[:n] {
						s := ln.Text
						if len(s) > 160 {
							s = s[:160]
						}
						sb.WriteString(s)
						sb.WriteByte('\n')
					}
				}
			}
		}
		fmt.Printf("=== %s (%d KB, %d pages, %v): %d chars, %d CJK\n%s\n", name, len(data)/1024, len(pages), elapsed, totalChars, totalCJK, sb.String())
	}
}
