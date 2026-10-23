package agent

import (
	"fmt"
	"os"
	"strings"

	"github.com/raitucarp/epub"
)

// extractEPUBText reads an EPUB in spine order and returns chapter text.
// Images and fonts are not decoded.
func extractEPUBText(filePath string) (text string, err error) {
	defer recoverLegacyOfficeExtraction("EPUB", &text, &err)
	if err := preflightDirectDocumentInput(filePath); err != nil {
		return "", err
	}
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", err
	}
	book, err := epub.NewReader(data)
	if err != nil {
		return "", fmt.Errorf("无法解析 EPUB: %v", err)
	}
	var b strings.Builder
	if title := strings.TrimSpace(strings.Join(book.Title(), " / ")); title != "" {
		b.WriteString("# ")
		b.WriteString(title)
		b.WriteString("\n\n")
	}
	if author := strings.TrimSpace(strings.Join(book.Author(), ", ")); author != "" {
		b.WriteString("作者：")
		b.WriteString(author)
		b.WriteString("\n\n")
	}
	page := 0
	for _, res := range book.Spine() {
		if !epubTextResource(res) {
			continue
		}
		body := strings.TrimSpace(book.ReadContentMarkdownById(res.ID))
		if body == "" {
			continue
		}
		if page == epubMaxChapters {
			b.WriteString("\n\n（仅读取前 500 章）")
			break
		}
		page++
		fmt.Fprintf(&b, "## Page %d\n", page)
		b.WriteString(body)
		b.WriteString("\n\n")
	}
	out := strings.TrimSpace(b.String())
	if page == 0 {
		return "", fmt.Errorf("EPUB 里没有可读取的章节正文")
	}
	return out, nil
}

const epubMaxChapters = 500

func epubTextResource(res epub.PublicationResource) bool {
	// properties is a space-separated list. "nav" is often paired with
	// another token, as in "scripted nav".
	for _, prop := range strings.Fields(string(res.Properties)) {
		if prop == "nav" {
			return false
		}
	}
	mime := strings.ToLower(res.MIMEType)
	href := strings.ToLower(res.Href)
	if strings.HasSuffix(href, ".ncx") {
		return false
	}
	if strings.Contains(mime, "html") || strings.HasSuffix(href, ".xhtml") || strings.HasSuffix(href, ".html") || strings.HasSuffix(href, ".htm") {
		return true
	}
	return false
}
