package browser

import (
	"strings"
	"testing"
)

func TestBrowserMarkdownToHTMLRichBlocks(t *testing.T) {
	html := browserMarkdownToHTML("# Title\n\n## Section\n\n- one\n- **two**\n\n1. first\n2. second\n\n> quote\n\n| A | B |\n| - | - |\n| 1 | 2 |\n\nplain [link](https://example.com) and `code`")
	for _, want := range []string{
		"<h1>Title</h1>",
		"<h2>Section</h2>",
		"<ul>",
		"<li>one</li>",
		"<strong>two</strong>",
		"<ol>",
		"<li>first</li>",
		"<blockquote>",
		"<p>quote</p>",
		"<table>",
		"<td>1</td>",
		`<a href="https://example.com">link</a>`,
		"<code>code</code>",
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("browserMarkdownToHTML missing %q in %s", want, html)
		}
	}
	for _, raw := range []string{"# Title", "- one", "**two**", "[link]("} {
		if strings.Contains(html, raw) {
			t.Fatalf("browserMarkdownToHTML leaked raw markdown %q in %s", raw, html)
		}
	}
}

func TestBrowserMarkdownToHTMLEscapesRawHTML(t *testing.T) {
	html := browserMarkdownToHTML("# Safe\n\n<script>alert(1)</script>")
	if strings.Contains(html, "<script>") || strings.Contains(html, "</script>") {
		t.Fatalf("browserMarkdownToHTML emitted raw script: %s", html)
	}
	if strings.Contains(html, "alert(1)") {
		t.Fatalf("browserMarkdownToHTML leaked script content: %s", html)
	}
}

func TestNormalizeBrowserContentFormat(t *testing.T) {
	if got := normalizeBrowserContentFormat("md"); got != BrowserContentFormatMarkdown {
		t.Fatalf("md normalized to %q", got)
	}
	if got := normalizeBrowserContentFormat(""); got != BrowserContentFormatPlain {
		t.Fatalf("empty normalized to %q", got)
	}
	if got := normalizeBrowserContentFormat("html"); got != BrowserContentFormatPlain {
		t.Fatalf("html normalized to %q, want plain because browser type supports plain/markdown only", got)
	}
}

func TestNormalizeDuplicatePageURL(t *testing.T) {
	got := normalizeDuplicatePageURL("https://zhuanlan.zhihu.com/p/123/#comments")
	if got != "https://zhuanlan.zhihu.com/p/123" {
		t.Fatalf("normalizeDuplicatePageURL = %q", got)
	}
}

func TestNormalizeReusableNavigationURL(t *testing.T) {
	tests := map[string]string{
		"HTTPS://ZHUANLAN.ZHIHU.COM/p/123/#comments": "https://zhuanlan.zhihu.com/p/123",
		"https://zhuanlan.zhihu.com/p/123/":          "https://zhuanlan.zhihu.com/p/123",
		"https://zhuanlan.zhihu.com/":                "https://zhuanlan.zhihu.com/",
		"https://zhuanlan.zhihu.com:443/p/123":       "https://zhuanlan.zhihu.com/p/123",
		"http://example.com:80/path/":                "http://example.com/path",
		"http://example.com:8080/path/":              "http://example.com:8080/path",
	}
	for input, want := range tests {
		if got := normalizeReusableNavigationURL(input); got != want {
			t.Fatalf("normalizeReusableNavigationURL(%q) = %q, want %q", input, got, want)
		}
	}
}
