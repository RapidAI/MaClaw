package agent

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"net/textproto"
	"net/url"
	"os"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/text/encoding/htmlindex"
	"golang.org/x/text/transform"
)

const (
	emlMaxDepth     = 8
	emlMaxPartBytes = 4 << 20
)

// extractEMLText reads an RFC 5322 / MIME message. Headers are decoded,
// one body representation is kept, and attachments are listed by name.
func extractEMLText(filePath string) (text string, err error) {
	defer recoverLegacyOfficeExtraction("EML", &text, &err)
	if err := preflightDirectDocumentInput(filePath); err != nil {
		return "", err
	}
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", err
	}
	out, err := renderEML(data, 0)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(out) == "" {
		return "", fmt.Errorf("邮件里没有可读取的正文")
	}
	return out, nil
}

func renderEML(data []byte, depth int) (string, error) {
	if depth > emlMaxDepth {
		return "", fmt.Errorf("邮件嵌套过深")
	}
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	msg, err := mail.ReadMessage(bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("无法解析邮件: %v", err)
	}
	var b strings.Builder
	subject := emlHeaderValue(msg.Header.Get("Subject"))
	if subject != "" {
		b.WriteString("# ")
		b.WriteString(subject)
		b.WriteString("\n\n")
	}
	for _, key := range []string{"From", "To", "Cc", "Date"} {
		value := emlHeaderValue(msg.Header.Get(key))
		if value == "" {
			continue
		}
		b.WriteString(key)
		b.WriteString(": ")
		b.WriteString(value)
		b.WriteByte('\n')
	}
	if b.Len() > 0 && !strings.HasSuffix(b.String(), "\n\n") {
		b.WriteByte('\n')
	}
	var attachments []string
	body, err := emlBody(msg.Body, msg.Header, depth, &attachments)
	if err != nil {
		return "", err
	}
	b.WriteString(strings.TrimSpace(body))
	if len(attachments) > 0 {
		b.WriteString("\n\n## 附件\n")
		for _, name := range attachments {
			b.WriteString("- ")
			b.WriteString(name)
			b.WriteByte('\n')
		}
	}
	return strings.TrimSpace(b.String()), nil
}

func emlBody(body io.Reader, header mail.Header, depth int, attachments *[]string) (string, error) {
	if depth > emlMaxDepth {
		return "", nil
	}
	mediaType, params, err := mime.ParseMediaType(header.Get("Content-Type"))
	if err != nil || mediaType == "" {
		mediaType = "text/plain"
	}
	mediaType = strings.ToLower(mediaType)
	if strings.HasPrefix(mediaType, "multipart/") {
		boundary := params["boundary"]
		if boundary == "" {
			return "", nil
		}
		if mediaType == "multipart/alternative" {
			return emlAlternative(body, boundary, depth, attachments)
		}
		return emlMultipart(body, boundary, depth, attachments)
	}
	if name := emlAttachmentName(header, params, mediaType); name != "" {
		*attachments = append(*attachments, name+" ("+mediaType+")")
		return "", nil
	}
	payload := emlPartBytes(body, header.Get("Content-Transfer-Encoding"))
	payload = emlCharset(payload, emlPartCharset(params["charset"], mediaType, payload))
	switch {
	case mediaType == "message/rfc822":
		nested, err := renderEML(payload, depth+1)
		if err != nil || strings.TrimSpace(nested) == "" {
			return "", nil
		}
		return "## 嵌套邮件\n\n" + nested, nil
	case mediaType == "text/html":
		return htmlToPlain(payload), nil
	case strings.HasPrefix(mediaType, "text/"):
		return string(payload), nil
	default:
		return "", nil
	}
}

func emlMultipart(body io.Reader, boundary string, depth int, attachments *[]string) (string, error) {
	reader := multipart.NewReader(body, boundary)
	var b strings.Builder
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			break
		}
		text, _ := emlBody(part, mail.Header(part.Header), depth+1, attachments)
		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(text)
	}
	return b.String(), nil
}

func emlAlternative(body io.Reader, boundary string, depth int, attachments *[]string) (string, error) {
	reader := multipart.NewReader(body, boundary)
	bestScore := -1
	var bestHeader mail.Header
	var bestBody []byte
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			break
		}
		data, err := io.ReadAll(io.LimitReader(part, emlMaxPartBytes+1))
		if err != nil {
			continue
		}
		score := 1
		mediaType, _, ctErr := mime.ParseMediaType(part.Header.Get("Content-Type"))
		if ctErr == nil {
			switch {
			case strings.HasPrefix(strings.ToLower(mediaType), "multipart/"):
				score = 3
			case strings.EqualFold(mediaType, "text/html"):
				score = 2
			}
		}
		if score >= bestScore {
			bestScore = score
			bestHeader = cloneMailHeader(part.Header)
			bestBody = data
		}
	}
	if bestScore < 0 {
		return "", nil
	}
	return emlBody(bytes.NewReader(bestBody), bestHeader, depth+1, attachments)
}

func emlAttachmentName(header mail.Header, contentType map[string]string, mediaType string) string {
	disposition, params, _ := mime.ParseMediaType(header.Get("Content-Disposition"))
	disposition = strings.ToLower(strings.TrimSpace(disposition))
	name := mimeExtendedParam(params, "filename")
	if name == "" {
		name = emlHeaderValue(params["filename"])
	}
	if name == "" {
		name = mimeExtendedParam(contentType, "name")
	}
	if name == "" {
		name = emlHeaderValue(contentType["name"])
	}
	// Inline text and nested messages stay in the body even when a client
	// stores a filename on them. Every other part is an attachment, including
	// an inline image that only has a Content-ID.
	if disposition != "attachment" && (strings.HasPrefix(mediaType, "text/") || strings.HasPrefix(mediaType, "multipart/") || mediaType == "message/rfc822") {
		return ""
	}
	if name == "" {
		name = strings.Trim(emlHeaderValue(header.Get("Content-ID")), "<>")
	}
	if name == "" {
		return "attachment"
	}
	return name
}

// mimeExtendedParam reads an RFC 2231 filename* / name* parameter, including
// split filename*0* / filename*1* segments. The encoded form wins over the
// plain ASCII fallback because that fallback drops non-ASCII names.
func mimeExtendedParam(params map[string]string, base string) string {
	if len(params) == 0 {
		return ""
	}
	if value := params[base+"*"]; value != "" {
		return decodeRFC2231(value)
	}
	var b strings.Builder
	charset := ""
	for i := 0; ; i++ {
		part, encoded := "", false
		if value, ok := params[fmt.Sprintf("%s*%d*", base, i)]; ok {
			part, encoded = value, true
		} else if value, ok := params[fmt.Sprintf("%s*%d", base, i)]; ok {
			part = value
		} else {
			break
		}
		if i == 0 && encoded {
			if segs := strings.SplitN(part, "'", 3); len(segs) == 3 {
				charset = segs[0]
				part = segs[2]
			}
		}
		if encoded {
			if unesc, err := url.PathUnescape(part); err == nil {
				part = unesc
			}
		}
		b.WriteString(part)
	}
	if b.Len() == 0 {
		return ""
	}
	return string(emlCharset([]byte(b.String()), charset))
}

func decodeRFC2231(value string) string {
	value = strings.TrimSpace(value)
	charset := ""
	data := value
	if parts := strings.SplitN(value, "'", 3); len(parts) == 3 {
		charset = parts[0]
		data = parts[2]
	}
	if unesc, err := url.PathUnescape(data); err == nil {
		data = unesc
	}
	return string(emlCharset([]byte(data), charset))
}

func cloneMailHeader(h textproto.MIMEHeader) mail.Header {
	out := make(mail.Header, len(h))
	for key, values := range h {
		out[key] = append([]string(nil), values...)
	}
	return out
}

// emlPartBytes caps the raw part, then decodes it. A base64 or
// quoted-printable error keeps any prefix that already decoded: a missing
// pad or a cut quartet must not drop the whole part.
func emlPartBytes(body io.Reader, encoding string) []byte {
	raw, _ := io.ReadAll(io.LimitReader(body, emlMaxPartBytes+1))
	if len(raw) > emlMaxPartBytes {
		raw = raw[:emlMaxPartBytes]
	}
	switch strings.ToLower(strings.TrimSpace(encoding)) {
	case "base64":
		return decodeMIMEBase64(raw)
	case "quoted-printable":
		decoded, err := io.ReadAll(quotedprintable.NewReader(bytes.NewReader(raw)))
		if err != nil && len(decoded) == 0 {
			return nil
		}
		return decoded
	default:
		return raw
	}
}

func decodeMIMEBase64(raw []byte) []byte {
	cleaned := make([]byte, 0, len(raw))
	for _, b := range raw {
		switch b {
		case ' ', '\t', '\r', '\n':
		default:
			cleaned = append(cleaned, b)
		}
	}
	if len(cleaned) == 0 {
		return nil
	}
	if rem := len(cleaned) % 4; rem != 0 {
		cleaned = append(cleaned, bytes.Repeat([]byte{'='}, 4-rem)...)
	}
	dst := make([]byte, base64.StdEncoding.DecodedLen(len(cleaned)))
	n, _ := base64.StdEncoding.Decode(dst, cleaned)
	if n == 0 {
		return nil
	}
	return dst[:n]
}

func emlHeaderValue(value string) string {
	value = emlDecodeHeader(value)
	value = strings.NewReplacer("\r", " ", "\n", " ").Replace(value)
	return strings.Join(strings.Fields(value), " ")
}

func emlDecodeHeader(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || !strings.Contains(value, "=?") {
		return value
	}
	dec := &mime.WordDecoder{CharsetReader: emlCharsetReader}
	if out, err := dec.DecodeHeader(value); err == nil {
		return out
	}
	return value
}

func emlCharsetReader(charset string, input io.Reader) (io.Reader, error) {
	charset = strings.TrimSpace(charset)
	if charset == "" || strings.EqualFold(charset, "utf-8") || strings.EqualFold(charset, "us-ascii") {
		return input, nil
	}
	enc, err := htmlindex.Get(charset)
	if err != nil || enc == nil {
		return input, nil
	}
	return transform.NewReader(input, enc.NewDecoder()), nil
}

// emlPartCharset uses the MIME charset. When an HTML part omits it, the
// charset written in a meta tag is the one the bytes were encoded with.
func emlPartCharset(headerCharset, mediaType string, payload []byte) string {
	headerCharset = strings.TrimSpace(headerCharset)
	if headerCharset != "" || !strings.Contains(mediaType, "html") {
		return headerCharset
	}
	return sniffHTMLCharset(payload)
}

func sniffHTMLCharset(payload []byte) string {
	if bytes.HasPrefix(payload, []byte{0xEF, 0xBB, 0xBF}) {
		return "utf-8"
	}
	head := payload
	if len(head) > 4096 {
		head = head[:4096]
	}
	lower := bytes.ToLower(head)
	if name := sniffMetaCharset(head, lower); name != "" {
		return name
	}
	// XHTML with no meta tag stores the encoding on the xml declaration.
	return sniffXMLEncoding(head)
}

func sniffMetaCharset(head, lower []byte) string {
	for from := 0; from < len(lower); {
		meta := bytes.Index(lower[from:], []byte("<meta"))
		if meta < 0 {
			return ""
		}
		meta += from
		end := bytes.IndexByte(lower[meta:], '>')
		if end < 0 {
			return ""
		}
		tag := head[meta : meta+end]
		tagLower := lower[meta : meta+end]
		if name := metaCharsetValue(tag, tagLower); name != "" {
			return name
		}
		from = meta + end + 1
	}
	return ""
}

func sniffXMLEncoding(head []byte) string {
	i := 0
	for i < len(head) && isASCIISpace(head[i]) {
		i++
	}
	rest := head[i:]
	if len(rest) < 6 || !bytes.EqualFold(rest[:5], []byte("<?xml")) || !isASCIISpace(rest[5]) {
		return ""
	}
	lower := bytes.ToLower(rest)
	end := bytes.Index(lower, []byte("?>"))
	if end < 0 {
		return ""
	}
	return attrTokenValue(rest[:end], lower[:end], "encoding")
}

// metaCharsetValue reads a charset attribute inside one meta tag.
// HTML allows ASCII whitespace around the equals sign, including
// charset = "gb2312" and content="text/html; charset = gb2312".
// The value is taken from the original tag so its spelling is kept.
func metaCharsetValue(tag, tagLower []byte) string {
	return attrTokenValue(tag, tagLower, "charset")
}

func attrTokenValue(tag, tagLower []byte, key string) string {
	for from := 0; from < len(tagLower); {
		i := bytes.Index(tagLower[from:], []byte(key))
		if i < 0 {
			return ""
		}
		i += from
		endKey := i + len(key)
		if (i > 0 && isHTMLAttrNameByte(tagLower[i-1])) || (endKey < len(tagLower) && isHTMLAttrNameByte(tagLower[endKey])) {
			from = endKey
			continue
		}
		j := endKey
		for j < len(tagLower) && isASCIISpace(tagLower[j]) {
			j++
		}
		if j >= len(tagLower) || tagLower[j] != '=' {
			from = endKey
			continue
		}
		if name := trimCharsetToken(tag[j+1:]); name != "" {
			return name
		}
		from = j + 1
	}
	return ""
}

func isHTMLAttrNameByte(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= '0' && b <= '9' || b == '-' || b == '_' || b == ':'
}

func isASCIISpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r' || b == '\f'
}

func trimCharsetToken(rest []byte) string {
	rest = bytes.TrimLeft(rest, " \t\r\n\f\"'")
	end := 0
	for end < len(rest) && !isCharsetStop(rest[end]) {
		end++
	}
	return string(rest[:end])
}

func isCharsetStop(b byte) bool {
	return b == '"' || b == '\'' || b == ';' || b == '>' || b == '/' || isASCIISpace(b)
}

func emlCharset(data []byte, charset string) []byte {
	charset = strings.TrimSpace(charset)
	if charset == "" || strings.EqualFold(charset, "utf-8") || strings.EqualFold(charset, "us-ascii") {
		return data
	}
	enc, err := htmlindex.Get(charset)
	if err != nil || enc == nil {
		return data
	}
	decoded, _, err := transform.Bytes(enc.NewDecoder(), data)
	if err != nil || len(decoded) == 0 {
		return data
	}
	return decoded
}

type htmlTextBlock struct {
	pre  bool
	text string
}

func htmlToPlain(data []byte) string {
	node, err := html.Parse(bytes.NewReader(data))
	if err != nil {
		return strings.TrimSpace(stripHTMLTags(string(data)))
	}
	var blocks []htmlTextBlock
	var cur strings.Builder
	var linkText strings.Builder
	linkDepth := 0
	writeText := func(s string) {
		if s == "" {
			return
		}
		cur.WriteString(s)
		if linkDepth > 0 {
			linkText.WriteString(s)
		}
	}
	preMode := false
	flush := func() {
		if cur.Len() == 0 {
			return
		}
		blocks = append(blocks, htmlTextBlock{pre: preMode, text: cur.String()})
		cur.Reset()
	}
	setPre := func(pre bool) {
		if pre == preMode {
			return
		}
		flush()
		preMode = pre
	}
	cellDepth := 0
	preDepth := 0
	rubyDepth := 0
	skipRT := 0
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n == nil {
			return
		}
		if n.Type == html.ElementNode {
			switch n.Data {
			case "script", "style", "noscript":
				return
			case "img":
				alt := htmlAttr(n, "alt")
				if alt == "" {
					alt = htmlAttr(n, "title")
				}
				if alt != "" {
					writeText(alt)
					writeText(" ")
				}
				return
			case "a":
				href := htmlAttr(n, "href")
				linkDepth++
				start := linkText.Len()
				for c := n.FirstChild; c != nil; c = c.NextSibling {
					walk(c)
				}
				// cur may have been flushed by a nested pre. linkText is not.
				if href != "" && !strings.Contains(linkText.String()[start:], href) {
					writeText(" ")
					writeText(href)
				}
				if linkDepth > 0 {
					linkDepth--
				}
				return
			case "td", "th":
				cellDepth++
			case "pre":
				preDepth++
				// Indentation inside a cell would split the row. Keep pre only outside cells.
				if cellDepth == 0 {
					setPre(true)
				}
			case "ruby":
				rubyDepth++
			case "rt", "rp":
				// rt is the pronunciation, rp is the fallback parentheses.
				if rubyDepth > 0 {
					skipRT++
				}
			}
		}
		if n.Type == html.TextNode && skipRT == 0 {
			text := n.Data
			if cellDepth > 0 {
				text = strings.NewReplacer("\r", " ", "\n", " ").Replace(text)
			}
			// Keep the text as written. A space after every node splits
			// 完整正文<b>泵</b> into two words.
			writeText(text)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
		if n.Type == html.ElementNode {
			switch n.Data {
			case "rt", "rp":
				if skipRT > 0 {
					skipRT--
				}
			case "ruby":
				if rubyDepth > 0 {
					rubyDepth--
				}
			case "td", "th":
				if cellDepth > 0 {
					cellDepth--
				}
				cur.WriteByte('\t')
			case "tr":
				if cellDepth > 0 {
					cur.WriteByte(' ')
				} else {
					cur.WriteByte('\n')
				}
			case "p", "div", "br", "li", "dt", "dd", "pre", "blockquote", "caption", "h1", "h2", "h3", "h4", "h5", "h6":
				if n.Data == "pre" && preDepth > 0 {
					preDepth--
					if preDepth == 0 {
						setPre(false)
					}
				}
				// A paragraph inside a cell is not a new table row.
				if cellDepth > 0 {
					cur.WriteByte(' ')
				} else {
					cur.WriteByte('\n')
				}
			}
		}
	}
	walk(node)
	flush()
	return joinHTMLBlocks(blocks)
}

func joinHTMLBlocks(blocks []htmlTextBlock) string {
	var out []string
	for _, block := range blocks {
		if block.pre {
			text := block.text
			if strings.HasPrefix(text, "\r\n") {
				text = text[2:]
			} else if strings.HasPrefix(text, "\n") {
				text = text[1:]
			}
			text = strings.TrimRight(text, " \t\r\n")
			if text != "" {
				out = append(out, text)
			}
			continue
		}
		if collapsed := collapsePlainLines(block.text); collapsed != "" {
			out = append(out, collapsed)
		}
	}
	return strings.Join(out, "\n")
}

func htmlAttr(n *html.Node, key string) string {
	for _, attr := range n.Attr {
		if attr.Key == key {
			return strings.TrimSpace(attr.Val)
		}
	}
	return ""
}

func stripHTMLTags(s string) string {
	var b strings.Builder
	in := false
	for _, r := range s {
		switch {
		case r == '<':
			in = true
		case r == '>':
			in = false
		case !in:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func collapsePlainLines(s string) string {
	lines := strings.Split(s, "\n")
	var out []string
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		out = append(out, strings.Join(fields, " "))
	}
	return strings.Join(out, "\n")
}
