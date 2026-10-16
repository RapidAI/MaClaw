// pptx-preview renders each slide of a .pptx deck to a PNG/JPEG image using
// the pure-Go GoPPT renderer, so a generated presentation can be visually
// checked without PowerPoint.
//
// Usage:
//
//	pptx-preview [flags] deck.pptx
//
// Flags:
//
//	-out     output directory (default: <name>_preview next to the deck)
//	-width   image width in pixels (default 1280, max 4096)
//	-offset  zero-based first slide to render
//	-max     max slides to render (default all, max 200)
//	-format  png (default) or jpeg
//	-quality JPEG quality 1-100 (default 90)
//
// The result is printed as JSON listing the rendered image paths.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/RapidAI/CodeClaw/corelib/pptx"
)

func main() {
	outDir := flag.String("out", "", "output directory (default: <name>_preview next to the deck)")
	width := flag.Int("width", pptx.DefaultPreviewWidth, "image width in pixels")
	offset := flag.Int("offset", 0, "zero-based first slide to render")
	maxSlides := flag.Int("max", 0, "max slides to render (0 = all)")
	format := flag.String("format", "png", "image format: png or jpeg")
	quality := flag.Int("quality", 0, "JPEG quality 1-100 (default 90)")
	fontDir := flag.String("fontdir", "", "extra font directory (repeatable via ; separator)")
	draft := flag.Bool("draft", false, "draft mode: faster, lower fidelity")
	flag.Parse()

	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: pptx-preview [flags] deck.pptx")
		flag.PrintDefaults()
		os.Exit(2)
	}

	var fontDirs []string
	if *fontDir != "" {
		for _, d := range splitList(*fontDir) {
			fontDirs = append(fontDirs, d)
		}
	}

	result, err := pptx.RenderPreview(flag.Arg(0), pptx.PreviewOptions{
		OutputDir:   *outDir,
		Width:       *width,
		SlideOffset: *offset,
		MaxSlides:   *maxSlides,
		Format:      *format,
		JPEGQuality: *quality,
		FontDirs:    fontDirs,
		Draft:       *draft,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "pptx-preview:", err)
		os.Exit(1)
	}

	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		fmt.Fprintln(os.Stderr, "pptx-preview:", err)
		os.Exit(1)
	}
	fmt.Println(string(data))
}

func splitList(s string) []string {
	var out []string
	start := 0
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == ';' {
			if part := s[start:i]; part != "" {
				out = append(out, part)
			}
			start = i + 1
		}
	}
	return out
}
