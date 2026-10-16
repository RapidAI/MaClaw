package main

import (
	"archive/zip"
	"bytes"
	"fmt"
	"html"
	"mime"
	"path"
	"strings"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

const (
	desktopDocxMIME        = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	desktopDeliverMaxRunes = 100_000
)

// desktopDocument turns arranged text into the file the chat will carry.
// A .docx name is a Word package built from that text. Every other name is
// those text bytes under that name. A file that already exists on the desktop
// is read by path and is not built here.
func desktopDocument(name, content string) (string, string, []byte, error) {
	name, err := desktopDeliverName(name)
	if err != nil {
		return "", "", nil, err
	}
	content = strings.TrimSpace(content)
	if content == "" {
		return "", "", nil, fmt.Errorf("desktop deliver requires content")
	}
	if utf8.RuneCountInString(content) > desktopDeliverMaxRunes || len(content) > desktop.FileBytesMax {
		return "", "", nil, fmt.Errorf("desktop deliver content is too long")
	}
	if strings.HasSuffix(strings.ToLower(name), ".docx") {
		raw, err := desktopDocx(content)
		if err != nil {
			return "", "", nil, err
		}
		return name, desktopDocxMIME, raw, nil
	}
	return name, desktopDeliverMIME(name), []byte(content), nil
}

func desktopDeliverName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" || strings.ContainsAny(name, `/\`) || strings.Contains(name, "..") || strings.HasPrefix(name, ".") {
		return "", fmt.Errorf("desktop deliver name is not a file name")
	}
	if utf8.RuneCountInString(name) > 80 {
		return "", fmt.Errorf("desktop deliver name is too long")
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return "", fmt.Errorf("desktop deliver name is not a file name")
		}
	}
	return name, nil
}

// desktopDeliverMIME is the type the chat download uses. An unknown name is
// still sent, as application/octet-stream.
func desktopDeliverMIME(name string) string {
	switch strings.ToLower(path.Ext(name)) {
	case ".docx":
		return desktopDocxMIME
	case ".txt", ".md", ".csv", ".log":
		return "text/plain"
	case ".pdf":
		return "application/pdf"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".json":
		return "application/json"
	case ".html", ".htm":
		return "text/html"
	case ".xml":
		return "application/xml"
	case ".zip":
		return "application/zip"
	case ".pptx":
		return "application/vnd.openxmlformats-officedocument.presentationml.presentation"
	case ".xlsx":
		return "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	case "":
		return "application/octet-stream"
	default:
		if kind := mime.TypeByExtension(strings.ToLower(path.Ext(name))); kind != "" {
			media, _, err := mime.ParseMediaType(kind)
			if err == nil && desktopMIMEOK(media) {
				return strings.ToLower(media)
			}
		}
		return "application/octet-stream"
	}
}

func desktopMIMEOK(raw string) bool {
	mimeType := strings.ToLower(strings.TrimSpace(raw))
	if i := strings.IndexByte(mimeType, ';'); i >= 0 {
		mimeType = strings.TrimSpace(mimeType[:i])
	}
	parts := strings.Split(mimeType, "/")
	if len(parts) != 2 {
		return false
	}
	for _, part := range parts {
		if part == "" || len(part) > 127 {
			return false
		}
		for j := 0; j < len(part); j++ {
			c := part[j]
			letter := c >= 'a' && c <= 'z' || c >= '0' && c <= '9'
			if j == 0 {
				if !letter {
					return false
				}
				continue
			}
			if !letter && c != '.' && c != '+' && c != '_' && c != '-' {
				return false
			}
		}
	}
	return true
}

func desktopDocx(content string) ([]byte, error) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	parts := []struct{ name, body string }{
		{"[Content_Types].xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
			`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
			`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>` +
			`<Default Extension="xml" ContentType="application/xml"/>` +
			`<Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>` +
			`</Types>`},
		{"_rels/.rels", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
			`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>` +
			`</Relationships>`},
		{"word/document.xml", desktopDocumentXML(content)},
		{"word/_rels/document.xml.rels", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
			`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"></Relationships>`},
	}
	for _, part := range parts {
		writer, err := zw.Create(part.name)
		if err != nil {
			return nil, err
		}
		if _, err := writer.Write([]byte(part.body)); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func desktopDocumentXML(content string) string {
	var body strings.Builder
	body.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`)
	body.WriteString(`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>`)
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimRight(line, "\r")
		body.WriteString(`<w:p><w:r><w:t xml:space="preserve">`)
		body.WriteString(html.EscapeString(line))
		body.WriteString(`</w:t></w:r></w:p>`)
	}
	body.WriteString(`<w:sectPr><w:pgSz w:w="11906" w:h="16838"/></w:sectPr></w:body></w:document>`)
	return body.String()
}
