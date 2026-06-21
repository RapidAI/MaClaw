// Package imgconv provides image conversion utilities, notably SVG to PNG
// rasterization using pure Go (no external dependencies like Python/Inkscape).
//
// The implementation uses oksvg + rasterx for path/shape/gradient rendering.
// Note: oksvg has limited <text> support—best results come from SVGs that use
// only basic shapes and number labels (which is the patent illustration norm).
package imgconv

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/srwiley/oksvg"
	"github.com/srwiley/rasterx"
)

// ConvertSVGToPNG reads an SVG file and renders it to a PNG file.
// outputWidth specifies the desired PNG width in pixels; height is calculated
// proportionally from the SVG viewBox aspect ratio. If outputWidth <= 0, the
// SVG's native viewBox dimensions are used.
//
// The conversion renders shapes/paths via oksvg, then overlays <text> elements
// using Go's font drawing (since oksvg does not support <text>).
//
// The PNG file is written atomically: a temporary file is used during encoding,
// then renamed to pngPath on success. On failure, no partial file is left.
func ConvertSVGToPNG(svgPath, pngPath string, outputWidth int) error {
	svgData, err := os.ReadFile(svgPath)
	if err != nil {
		return fmt.Errorf("read svg: %w", err)
	}

	img, err := RenderSVGBytes(svgData, outputWidth)
	if err != nil {
		return fmt.Errorf("render svg: %w", err)
	}

	// Ensure target directory exists
	if dir := filepath.Dir(pngPath); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create png directory: %w", err)
		}
	}

	// Atomic write: write to temp file then rename
	tmp := pngPath + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return fmt.Errorf("create png temp: %w", err)
	}

	if err := png.Encode(out, img); err != nil {
		out.Close()
		os.Remove(tmp)
		return fmt.Errorf("encode png: %w", err)
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("close png: %w", err)
	}

	// On Windows, os.Rename fails if target exists; remove first.
	os.Remove(pngPath)
	if err := os.Rename(tmp, pngPath); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("rename png: %w", err)
	}
	return nil
}

// RenderSVG reads SVG content from r and rasterizes it to an image.RGBA.
// outputWidth controls the rendered width; 0 means use SVG native size.
// Note: this only renders shapes/paths. Use RenderSVGBytes for full rendering
// including <text> overlay.
func RenderSVG(r io.Reader, outputWidth int) (*image.RGBA, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("read svg data: %w", err)
	}
	return RenderSVGBytes(data, outputWidth)
}

// RenderSVGBytes renders SVG from raw bytes, including both shapes and text.
func RenderSVGBytes(data []byte, outputWidth int) (*image.RGBA, error) {
	// Pass 1: Render shapes/paths via oksvg
	// Use IgnoreErrorMode to silently skip unsupported elements (like <text>)
	// without printing warnings to stderr — we handle <text> ourselves in Pass 2.
	icon, err := oksvg.ReadIconStream(bytes.NewReader(data), oksvg.IgnoreErrorMode)
	if err != nil {
		return nil, fmt.Errorf("parse svg: %w", err)
	}

	viewW := icon.ViewBox.W
	viewH := icon.ViewBox.H
	if viewW <= 0 || viewH <= 0 {
		return nil, fmt.Errorf("invalid svg viewBox: w=%f h=%f", viewW, viewH)
	}

	w := int(viewW)
	h := int(viewH)

	if outputWidth > 0 {
		scale := float64(outputWidth) / viewW
		w = outputWidth
		h = int(viewH * scale)
	}
	if h <= 0 {
		h = 1
	}

	// Create image with white background
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(img, img.Bounds(), &image.Uniform{color.White}, image.Point{}, draw.Src)

	icon.SetTarget(0, 0, float64(w), float64(h))
	scanner := rasterx.NewScannerGV(w, h, img, img.Bounds())
	dasher := rasterx.NewDasher(w, h, scanner)
	icon.Draw(dasher, 1.0)

	// Pass 2: Extract and overlay <text> elements
	texts, _ := extractSVGTextElements(bytes.NewReader(data))
	if len(texts) > 0 {
		scaleX := float64(w) / viewW
		scaleY := float64(h) / viewH
		overlayTextOnImage(img, texts, scaleX, scaleY)
	}

	return img, nil
}

// ConvertAllSVGInDir finds all .svg files in dir and converts each to .png
// with the given width. Returns the list of generated PNG paths and any errors.
func ConvertAllSVGInDir(dir string, outputWidth int) ([]string, []error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, []error{fmt.Errorf("read dir: %w", err)}
	}

	var pngs []string
	var errs []error

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(strings.ToLower(name), ".svg") {
			continue
		}
		svgPath := filepath.Join(dir, name)
		pngName := strings.TrimSuffix(name, filepath.Ext(name)) + ".png"
		pngPath := filepath.Join(dir, pngName)

		if err := ConvertSVGToPNG(svgPath, pngPath, outputWidth); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", name, err))
		} else {
			pngs = append(pngs, pngPath)
		}
	}
	return pngs, errs
}
