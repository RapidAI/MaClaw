package mailbox

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"strings"
	"testing"

	"github.com/emersion/go-imap"
)

func TestHTMLToTextKeepsLinksAndSkipsTheHead(t *testing.T) {
	got := htmlToText(`<html><head><title>not the note</title><style>.x{}</style></head><body><p>请点 <a href="https://ex.com/a">这里</a></p><a href="javascript:alert(1)">no</a><script>steal()</script></body></html>`)
	if !strings.Contains(got, "请点") || !strings.Contains(got, "这里") || !strings.Contains(got, "https://ex.com/a") {
		t.Fatalf("text=%q", got)
	}
	if strings.Contains(got, "not the note") || strings.Contains(got, "steal") || strings.Contains(got, "javascript:") || strings.Contains(got, ".x{}") {
		t.Fatalf("text=%q", got)
	}
	again := htmlToText(`<a href="https://ex.com/a">https://ex.com/a</a>`)
	if strings.Count(again, "https://ex.com/a") != 1 {
		t.Fatalf("duplicated link %q", again)
	}
}

func TestLongSubjectStaysUnderTheSMTPLineLimit(t *testing.T) {
	subject := strings.Repeat("你好", 200)
	raw := string(rfc822(OutboundMessage{From: "a@ex.com", To: []string{"b@ex.com"}, Subject: subject, Text: "hi"}))
	for _, line := range strings.Split(raw, "\r\n") {
		if len(line) > 998 {
			t.Fatalf("line length %d: %s", len(line), line[:80])
		}
	}
	var body Body
	fillParsedBody(&body, []byte(raw))
	if body.Subject != subject {
		t.Fatalf("subject=%q", body.Subject)
	}
	ascii := strings.Repeat("a", 1200)
	raw = string(rfc822(OutboundMessage{From: "a@ex.com", To: []string{"b@ex.com"}, Subject: ascii, Text: "hi"}))
	for _, line := range strings.Split(raw, "\r\n") {
		if len(line) > 998 {
			t.Fatalf("ascii line length %d", len(line))
		}
	}
	body = Body{}
	fillParsedBody(&body, []byte(raw))
	if body.Subject != ascii {
		t.Fatalf("ascii subject len=%d %q", len(body.Subject), body.Subject[:40])
	}
}

func TestListFetchDoesNotDownloadTheBody(t *testing.T) {
	items, snippet := summaryFetchItems(false)
	for _, item := range items {
		if strings.Contains(strings.ToUpper(string(item)), "TEXT") {
			t.Fatalf("list fetch includes a body section: %v", items)
		}
	}
	if snippet != nil {
		t.Fatal("list returned a snippet section")
	}
	items, snippet = summaryFetchItems(true)
	if snippet == nil || !strings.Contains(strings.ToUpper(string(snippet.FetchItem())), "TEXT") {
		t.Fatalf("search fetch lost the text section: %v", items)
	}
}

func TestParsedReplyHeadersKeepAngleBrackets(t *testing.T) {
	raw := []byte("From: a@ex.com\r\nTo: b@ex.com\r\nSubject: Re: Plan\r\n" +
		"Message-ID: <child@ex.com>\r\n" +
		"References: <root@ex.com>\r\n <mid@ex.com>\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n" +
		"\r\nhello\r\n")
	body := Body{MessageSummary: MessageSummary{MessageID: "<from-envelope@ex.com>"}}
	fillParsedBody(&body, raw)
	if body.MessageID != "<child@ex.com>" || body.References != "<root@ex.com> <mid@ex.com>" || strings.TrimSpace(body.Text) != "hello" {
		t.Fatalf("id=%q refs=%q text=%q", body.MessageID, body.References, body.Text)
	}
	kept := Body{MessageSummary: MessageSummary{MessageID: "<from-envelope@ex.com>"}}
	fillParsedBody(&kept, []byte("From: a@ex.com\r\nSubject: Hi\r\nContent-Type: text/plain; charset=utf-8\r\n\r\nhello\r\n"))
	if kept.MessageID != "<from-envelope@ex.com>" {
		t.Fatalf("missing header wiped the envelope id: %q", kept.MessageID)
	}
}

func TestRecentSequenceTakesTheNewestWindow(t *testing.T) {
	if _, _, ok := recentSequence(0, 20); ok {
		t.Fatal("empty mailbox produced a window")
	}
	start, end, ok := recentSequence(5, 20)
	if !ok || start != 1 || end != 5 {
		t.Fatalf("small mailbox start=%d end=%d ok=%v", start, end, ok)
	}
	start, end, ok = recentSequence(1000, 20)
	if !ok || start != 981 || end != 1000 {
		t.Fatalf("large mailbox start=%d end=%d ok=%v", start, end, ok)
	}
}

func TestMessageSearchMatchesSubjectFromToOrText(t *testing.T) {
	flat := flattenSearch(messageSearchCriteria("invoice").Format())
	for _, want := range []string{"OR", "SUBJECT", "FROM", "TO", "CC", "TEXT", "invoice"} {
		if !strings.Contains(flat, want) {
			t.Fatalf("missing %s in %s", want, flat)
		}
	}
	if strings.TrimSpace(flat) == "TEXT invoice" {
		t.Fatal("search is only a TEXT term")
	}
}

func TestGBKBodyAndEncodedSubjectDecode(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString([]byte{0xC4, 0xE3, 0xBA, 0xC3})
	raw := []byte("From: =?UTF-8?B?5L2g5aW9?= <a@ex.com>\r\n" +
		"To: b@ex.com\r\n" +
		"Subject: =?UTF-8?B?5L2g5aW9?=\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: text/plain; charset=gbk\r\n" +
		"Content-Transfer-Encoding: base64\r\n" +
		"\r\n" +
		encoded + "\r\n")
	var body Body
	fillParsedBody(&body, raw)
	if body.Text != "你好" || body.Subject != "你好" {
		t.Fatalf("text=%q subject=%q", body.Text, body.Subject)
	}
	if decodeMIMEHeader("=?UTF-8?B?5L2g5aW9?=") != "你好" {
		t.Fatalf("header=%q", decodeMIMEHeader("=?UTF-8?B?5L2g5aW9?="))
	}
}

func TestSnippetSkipsMIMEHeaders(t *testing.T) {
	raw := []byte("--b\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n请查收发票\r\n--b--\r\n")
	got := snippetText(raw)
	if strings.Contains(got, "Content-Type") || got != "请查收发票" {
		t.Fatalf("snippet=%q", got)
	}
	if snippetText([]byte("只要正文")) != "只要正文" {
		t.Fatalf("plain=%q", snippetText([]byte("只要正文")))
	}
}

func TestSinglePartFileIsListedAsAttachment(t *testing.T) {
	parts := attachmentParts(&imap.BodyStructure{
		MIMEType: "application", MIMESubType: "pdf",
		Disposition:       "attachment",
		DispositionParams: map[string]string{"filename": "报价.pdf"},
		Encoding:          "base64",
		Size:              12,
	})
	if len(parts) != 1 || parts[0].path != "1" || parts[0].name != "报价.pdf" || parts[0].encoding != "base64" {
		t.Fatalf("single=%+v", parts)
	}
	mixed := attachmentParts(&imap.BodyStructure{
		MIMEType: "multipart", MIMESubType: "mixed",
		Parts: []*imap.BodyStructure{
			{MIMEType: "text", MIMESubType: "plain"},
			{MIMEType: "application", MIMESubType: "pdf", Disposition: "attachment", DispositionParams: map[string]string{"filename": "a.pdf"}, Size: 4},
		},
	})
	if len(mixed) != 1 || mixed[0].path != "2" || mixed[0].name != "a.pdf" {
		t.Fatalf("mixed=%+v", mixed)
	}
}

func TestSnippetDecodesGBKAndSkipsALongPreamble(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString([]byte{0xC4, 0xE3, 0xBA, 0xC3})
	raw := "--b\r\nContent-Type: text/plain; charset=gbk\r\nContent-Transfer-Encoding: base64\r\nX-Pad: " +
		strings.Repeat("a", 400) + "\r\n\r\n" + encoded + "\r\n--b--\r\n"
	bs := &imap.BodyStructure{
		MIMEType: "multipart", MIMESubType: "mixed",
		Params: map[string]string{"boundary": "b"},
	}
	got := readSnippet(bytes.NewReader([]byte(raw)), bs)
	if got != "你好" {
		t.Fatalf("multipart snippet=%q", got)
	}
	single := &imap.BodyStructure{
		MIMEType: "text", MIMESubType: "plain", Encoding: "base64",
		Params: map[string]string{"charset": "gbk"},
	}
	if got := readSnippet(bytes.NewReader([]byte(encoded)), single); got != "你好" {
		t.Fatalf("single snippet=%q", got)
	}
}

func TestAttachmentBytesAreTransferDecoded(t *testing.T) {
	pdf := []byte("%PDF-1.4")
	encoded := []byte(base64.StdEncoding.EncodeToString(pdf))
	got, err := partBytes(mimePart{encoding: "base64"}, encoded)
	if err != nil || string(got) != string(pdf) {
		t.Fatalf("base64 got=%q err=%v", got, err)
	}
	got, err = partBytes(mimePart{encoding: "quoted-printable"}, []byte("=E4=BD=A0=E5=A5=BD"))
	if err != nil || string(got) != "你好" {
		t.Fatalf("qp got=%q err=%v", got, err)
	}
	big := bytes.Repeat([]byte("a"), AttachmentCapBytes+1)
	if _, err := partBytes(mimePart{encoding: "8bit"}, big); err == nil || !strings.Contains(err.Error(), "10MB") {
		t.Fatalf("oversize err=%v", err)
	}
}

func TestInlinePictureDoesNotTakeTheFileIndex(t *testing.T) {
	bs := &imap.BodyStructure{
		MIMEType: "multipart", MIMESubType: "mixed",
		Parts: []*imap.BodyStructure{
			{MIMEType: "text", MIMESubType: "plain"},
			{MIMEType: "image", MIMESubType: "png", Disposition: "inline", DispositionParams: map[string]string{"filename": "logo.png"}, Size: 9},
			{MIMEType: "application", MIMESubType: "pdf", Disposition: "attachment", DispositionParams: map[string]string{"filename": "报价.pdf"}, Size: 4},
		},
	}
	metas := metasFromStructure(bs)
	if len(metas) != 1 || metas[0].Index != 0 || metas[0].Name != "报价.pdf" || metas[0].Size != 4 {
		t.Fatalf("metas=%+v", metas)
	}
	parts := attachmentParts(bs)
	if len(parts) != 1 || parts[0].path != "3" {
		t.Fatalf("parts=%+v", parts)
	}
}

func TestUnknownCharsetStillReturnsTheBody(t *testing.T) {
	raw := []byte("From: a@ex.com\r\nSubject: hello\r\nMIME-Version: 1.0\r\n" +
		"Content-Type: text/plain; charset=x-not-a-charset\r\n\r\nhello-body\r\n")
	var body Body
	fillParsedBody(&body, raw)
	if strings.Contains(body.Text, "Content-Type:") || !strings.Contains(body.Text, "hello-body") {
		t.Fatalf("text=%q", body.Text)
	}
}

func TestOutboundChineseBodyIsQuotedPrintable(t *testing.T) {
	raw := rfc822(OutboundMessage{
		From: "a@ex.com", To: []string{"b@ex.com"}, Bcc: []string{"hidden@ex.com"},
		Subject: "你好", Text: "你好",
	})
	s := string(raw)
	if strings.Contains(s, "hidden@ex.com") || strings.Contains(s, "Content-Transfer-Encoding: 8bit") || strings.Contains(s, "Subject: 你好") {
		t.Fatalf("wire=%s", s)
	}
	if !strings.Contains(s, "quoted-printable") {
		t.Fatalf("wire=%s", s)
	}
	var body Body
	fillParsedBody(&body, raw)
	if strings.TrimSpace(body.Text) != "你好" || body.Subject != "你好" {
		t.Fatalf("text=%q subject=%q", body.Text, body.Subject)
	}
	plain := string(rfc822(OutboundMessage{From: "a@ex.com", To: []string{"b@ex.com"}, Subject: "Hi", Text: "hello"}))
	if !strings.Contains(plain, "Content-Transfer-Encoding: 7bit") || !strings.Contains(plain, "hello") {
		t.Fatalf("plain=%s", plain)
	}
	long := string(rfc822(OutboundMessage{From: "a@ex.com", To: []string{"b@ex.com"}, Subject: "Hi", Text: strings.Repeat("a", 1200)}))
	if !strings.Contains(long, "quoted-printable") {
		t.Fatal("a line past 998 characters stayed 7bit")
	}
}

func flattenSearch(fields []interface{}) string {
	var b strings.Builder
	var walk func(v interface{})
	walk = func(v interface{}) {
		switch item := v.(type) {
		case []interface{}:
			for _, child := range item {
				walk(child)
			}
		default:
			fmt.Fprintf(&b, "%v ", item)
		}
	}
	walk(fields)
	return b.String()
}
