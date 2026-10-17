package mailbox

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

type MessageSummary struct {
	UID           uint32   `json:"uid"`
	From          string   `json:"from"`
	To            string   `json:"to"`
	Subject       string   `json:"subject"`
	Date          string   `json:"date"`
	Seen          bool     `json:"seen"`
	HasAttachment bool     `json:"has_attachment"`
	Snippet       string   `json:"snippet,omitempty"`
	MessageID     string   `json:"-"`
	References    string   `json:"-"`
	ReplyTo       string   `json:"-"`
	FromAddr      string   `json:"-"`
	ToAddrs       []string `json:"-"`
	CcAddrs       []string `json:"-"`
}

type AttachmentMeta struct {
	Index       int    `json:"index"`
	Name        string `json:"name"`
	Size        int64  `json:"size"`
	ContentType string `json:"content_type"`
}

type Body struct {
	MessageSummary
	Text        string
	HTML        string
	Attachments []AttachmentMeta
}

type Attachment struct {
	Name  string
	Bytes []byte
}

// MailReader is the injected IMAP peer used by mailbox_query.
type MailReader interface {
	List(ctx context.Context, account Account, secret, box string, limit int) ([]MessageSummary, error)
	Read(ctx context.Context, account Account, secret, box string, uid uint32, offset int) (Body, error)
	Search(ctx context.Context, account Account, secret, box, query string, limit int) ([]MessageSummary, error)
	Attachment(ctx context.Context, account Account, secret, box string, uid uint32, index int) (Attachment, error)
}

func plainBody(body Body, offset int) (text string, hasMore bool, next int) {
	raw := strings.TrimSpace(body.Text)
	if raw == "" && strings.TrimSpace(body.HTML) != "" {
		raw = htmlToText(body.HTML)
	}
	raw = strings.TrimSpace(raw)
	if offset < 0 {
		offset = 0
	}
	runes := []rune(raw)
	if offset > len(runes) {
		offset = len(runes)
	}
	end := offset + BodyCapBytes
	if end > len(runes) {
		end = len(runes)
	}
	// Cap by bytes as well as runes so a wide body cannot exceed 32 KiB.
	chunk := string(runes[offset:end])
	for len(chunk) > BodyCapBytes && end > offset {
		end--
		chunk = string(runes[offset:end])
	}
	hasMore = end < len(runes)
	next = end
	if chunk == "" {
		return MsgUntrusted, false, next
	}
	return MsgUntrusted + "\n" + chunk, hasMore, next
}

// quoteBody is the text frozen into a forward. It is the raw plain part, or
// the HTML converted to text when there is no plain part. It is not the
// model-facing view: no untrusted-instruction banner and no 32 KiB cap.
// ViewBody is the plain text shown to the person who owns the mailbox.
// It is not the model-facing view: no instruction banner, and a long note is cut.
func ViewBody(body Body) (text string, truncated bool) {
	raw := quoteBody(body)
	runes := []rune(raw)
	const capRunes = 12000
	if len(runes) > capRunes {
		return string(runes[:capRunes]), true
	}
	return raw, false
}

func quoteBody(body Body) string {
	raw := strings.TrimSpace(body.Text)
	if raw == "" && strings.TrimSpace(body.HTML) != "" {
		raw = strings.TrimSpace(htmlToText(body.HTML))
	}
	return raw
}

func writeAttachment(workingDir string, account Account, uid uint32, index int, att Attachment) (string, error) {
	if len(att.Bytes) > AttachmentCapBytes {
		return "", classErr("too_large", "附件超过 10MB，没有保存")
	}
	name := safeAttachmentName(att.Name)
	if name == "" {
		return "", classErr("invalid", "附件文件名无效")
	}
	dir := filepath.Join(workingDir, "mailbox-attachments", account.ID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", classErr("connect", MsgConnect)
	}
	target := filepath.Join(dir, sprintfUID(uid, index, name))
	rel, err := filepath.Rel(workingDir, target)
	if err != nil || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return "", classErr("invalid", "附件路径无效")
	}
	if err := os.WriteFile(target, att.Bytes, 0o600); err != nil {
		return "", classErr("connect", MsgConnect)
	}
	return target, nil
}

func sprintfUID(uid uint32, index int, name string) string {
	return strings.TrimSpace(jsonNumber(uint64(uid)) + "-" + jsonNumber(uint64(index)) + "-" + name)
}

func jsonNumber(n uint64) string {
	b, _ := json.Marshal(n)
	return string(b)
}

func safeAttachmentName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" || strings.Contains(name, "..") {
		return ""
	}
	if filepath.IsAbs(name) || strings.ContainsAny(name, `/\`) {
		return ""
	}
	var b strings.Builder
	for _, r := range name {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '.' || r == '_' || r == '-' {
			b.WriteRune(r)
			continue
		}
		b.WriteByte('_')
	}
	out := strings.Trim(b.String(), ".")
	if out == "" || out == ".." || strings.Contains(out, "..") {
		return ""
	}
	return out
}
