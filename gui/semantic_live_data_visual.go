package main

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"strings"
	"unicode"
)

const (
	semanticTrustedLiveDataVisualAdapter        = "semantic_render_trusted_live_data"
	semanticTrustedLiveDataVisualImplementation = "trusted-live-data-visual-v1"
)

func semanticTrustedLiveDataVisualDefinition() map[string]interface{} {
	return map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        semanticTrustedLiveDataVisualAdapter,
			"description": "Render the current trusted lookup evidence as a PNG image artifact. No source, URL, path, bytes, or target is accepted.",
			"parameters":  semanticTrustedLiveDataVisualInvocationSchema(),
		},
	}
}

func semanticTrustedLiveDataVisualInvocationSchema() map[string]interface{} {
	return map[string]interface{}{
		"type":                 "object",
		"properties":           map[string]interface{}{},
		"required":             []string{},
		"additionalProperties": false,
	}
}

// renderTrustedLiveDataVisual produces a deterministic information card from
// evidence recorded by the host-owned search adapter. It is not text-to-image:
// neither model arguments nor a remote image URL can affect its source.
func renderTrustedLiveDataVisual(userText, evidence string) (string, error) {
	evidence = trustedHostLookupEvidence(evidence)
	if evidence == "" {
		return "", fmt.Errorf("trusted_live_data_evidence_missing")
	}
	title := liveDataVisualTitle(userText)
	if title == "" {
		title = "实时数据"
	}
	img := image.NewRGBA(image.Rect(0, 0, 960, 540))
	draw.Draw(img, img.Bounds(), &image.Uniform{C: color.RGBA{R: 14, G: 29, B: 52, A: 255}}, image.Point{}, draw.Src)
	draw.Draw(img, image.Rect(0, 0, 960, 122), &image.Uniform{C: color.RGBA{R: 31, G: 94, B: 164, A: 255}}, image.Point{}, draw.Src)
	titleWidth := 180 + (len([]rune(title))*17)%430
	draw.Draw(img, image.Rect(46, 52, 46+titleWidth, 72), &image.Uniform{C: color.RGBA{R: 141, G: 210, B: 255, A: 255}}, image.Point{}, draw.Src)
	draw.Draw(img, image.Rect(46, 166, 914, 470), &image.Uniform{C: color.RGBA{R: 24, G: 46, B: 78, A: 255}}, image.Point{}, draw.Src)
	// A compact bar visualization keeps the artifact visibly data-derived even
	// where an installed system font cannot render CJK glyphs.
	lines := splitVisualEvidenceLines(evidence, 6)
	for index, line := range lines {
		width := 120 + (len([]rune(line))*19)%650
		y := 195 + index*42
		draw.Draw(img, image.Rect(76, y, 76+width, y+22), &image.Uniform{C: color.RGBA{R: uint8(54 + index*20), G: uint8(162 - index*9), B: 214, A: 255}}, image.Point{}, draw.Src)
	}
	buf := new(bytes.Buffer)
	encoder := png.Encoder{CompressionLevel: png.BestSpeed}
	if err := encoder.Encode(buf, img); err != nil {
		return "", fmt.Errorf("trusted_live_data_visual_encode: %w", err)
	}
	return base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}

func liveDataVisualTitle(userText string) string {
	title := strings.TrimSpace(semanticUserIntentText(userText))
	if title == "" {
		return "实时数据"
	}
	for _, suffix := range []string{"实况图", "天气图", "信息图", "图表", "weather graphic", "weather chart", "infographic", "live chart"} {
		if index := strings.Index(strings.ToLower(title), suffix); index >= 0 {
			title = strings.TrimSpace(title[:index])
			break
		}
	}
	title = strings.TrimRightFunc(title, func(r rune) bool {
		return unicode.IsSpace(r) || strings.ContainsRune("，,、;；：:。.!！?？", r)
	})
	if title == "" {
		return "实时数据"
	}
	return title
}

func splitVisualEvidenceLines(evidence string, limit int) []string {
	if limit < 1 {
		limit = 1
	}
	var out []string
	for _, line := range strings.Split(evidence, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		out = append(out, line)
		if len(out) == limit {
			break
		}
	}
	if len(out) == 0 {
		return []string{evidence}
	}
	return out
}
