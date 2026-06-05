package browser

import (
	"bytes"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	goldmarkhtml "github.com/yuin/goldmark/renderer/html"
)

const (
	BrowserContentFormatPlain    = "plain"
	BrowserContentFormatMarkdown = "markdown"
)

var browserMarkdownRenderer = goldmark.New(
	goldmark.WithExtensions(
		extension.GFM,
	),
	goldmark.WithRendererOptions(
		goldmarkhtml.WithXHTML(),
	),
)

func normalizeBrowserContentFormat(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case BrowserContentFormatMarkdown, "md":
		return BrowserContentFormatMarkdown
	default:
		return BrowserContentFormatPlain
	}
}

func browserMarkdownToHTML(markdown string) string {
	markdown = strings.ReplaceAll(markdown, "\r\n", "\n")
	markdown = strings.ReplaceAll(markdown, "\r", "\n")
	markdown = strings.TrimSpace(markdown)
	if markdown == "" {
		return ""
	}
	var out bytes.Buffer
	if err := browserMarkdownRenderer.Convert([]byte(markdown), &out); err != nil {
		return ""
	}
	return strings.TrimSpace(out.String())
}
