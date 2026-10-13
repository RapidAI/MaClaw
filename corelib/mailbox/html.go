package mailbox

import (
	"strings"

	"golang.org/x/net/html"
)

func htmlToText(raw string) string {
	node, err := html.Parse(strings.NewReader(raw))
	if err != nil || node == nil {
		return ""
	}
	var b strings.Builder
	var walk func(*html.Node, bool)
	walk = func(n *html.Node, skip bool) {
		if n == nil {
			return
		}
		if n.Type == html.ElementNode {
			switch strings.ToLower(n.Data) {
			case "script", "style", "head", "noscript":
				skip = true
			case "br", "p", "div", "tr", "li", "h1", "h2", "h3", "h4", "blockquote":
				if !skip {
					b.WriteByte('\n')
				}
			case "a":
				if skip {
					return
				}
				start := b.Len()
				for c := n.FirstChild; c != nil; c = c.NextSibling {
					walk(c, false)
				}
				if href := usableLink(nodeAttr(n, "href")); href != "" && !strings.Contains(b.String()[start:], href) {
					if strings.TrimSpace(b.String()[start:]) != "" {
						b.WriteByte(' ')
					}
					b.WriteString(href)
				}
				return
			}
		}
		if n.Type == html.TextNode && !skip {
			b.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c, skip)
		}
	}
	walk(node, false)
	return strings.TrimSpace(b.String())
}

func nodeAttr(n *html.Node, key string) string {
	if n == nil {
		return ""
	}
	for _, attr := range n.Attr {
		if strings.EqualFold(attr.Key, key) {
			return attr.Val
		}
	}
	return ""
}

func usableLink(href string) string {
	href = strings.TrimSpace(href)
	if href == "" || strings.ContainsAny(href, " \t\r\n") {
		return ""
	}
	lower := strings.ToLower(href)
	switch {
	case strings.HasPrefix(lower, "https://"), strings.HasPrefix(lower, "http://"), strings.HasPrefix(lower, "mailto:"):
		return href
	default:
		return ""
	}
}
