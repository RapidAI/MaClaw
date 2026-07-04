package mail

import (
	"context"
	"strings"
	"testing"
)

func TestBuildMessageEncodesNonASCIIHeaders(t *testing.T) {
	fromName := "\u7801\u5361\u9f99"
	subjectText := "MaClaw \u6ce8\u518c\u8d44\u6599\u9a8c\u8bc1\u7801"
	rawSubjectPart := "\u6ce8\u518c\u8d44\u6599\u9a8c\u8bc1\u7801"

	msg := string(buildMessage(fromName, "noreply@example.com", []string{"user@example.com"}, subjectText, "body"))

	subject := headerLine(t, msg, "Subject:")
	if strings.Contains(subject, rawSubjectPart) {
		t.Fatalf("subject header contains raw non-ASCII text: %q", subject)
	}
	if !strings.Contains(subject, "=?UTF-8?") {
		t.Fatalf("subject header is not MIME encoded: %q", subject)
	}

	from := headerLine(t, msg, "From:")
	if strings.Contains(from, fromName) {
		t.Fatalf("from header contains raw non-ASCII text: %q", from)
	}
	if !strings.Contains(from, "=?utf-8?") && !strings.Contains(from, "=?UTF-8?") {
		t.Fatalf("from header is not MIME encoded: %q", from)
	}
}

func TestBuildMessageKeepsASCIIHeadersReadable(t *testing.T) {
	msg := string(buildMessage("MaClaw", "noreply@example.com", []string{"user@example.com"}, "Login  code", "body"))

	if got := headerLine(t, msg, "Subject:"); got != "Subject: Login  code" {
		t.Fatalf("unexpected subject header: %q", got)
	}
	if got := headerLine(t, msg, "From:"); got != "From: \"MaClaw\" <noreply@example.com>" {
		t.Fatalf("unexpected from header: %q", got)
	}
	if got := headerLine(t, msg, "To:"); got != "To: <user@example.com>" {
		t.Fatalf("unexpected to header: %q", got)
	}
}

func TestBuildMessageFormatsQuotedRecipientLocalPart(t *testing.T) {
	msg := string(buildMessage("", "noreply@example.com", []string{`"user name"@example.com`}, "Login code", "body"))

	if got := headerLine(t, msg, "To:"); got != `To: <"user name"@example.com>` {
		t.Fatalf("unexpected to header: %q", got)
	}
}

func TestBuildMessageSanitizesHeaderLineBreaks(t *testing.T) {
	msg := string(buildMessage("Ma\r\nClaw", "noreply@example.com", []string{"user@example.com"}, "Login\r\nBcc: x@example.com", "body"))

	if got := headerLine(t, msg, "Subject:"); got != "Subject: Login  Bcc: x@example.com" {
		t.Fatalf("unexpected subject header: %q", got)
	}
	if got := headerLine(t, msg, "From:"); got != "From: \"Ma  Claw\" <noreply@example.com>" {
		t.Fatalf("unexpected from header: %q", got)
	}
	if strings.Contains(msg, "\r\nBcc:") {
		t.Fatalf("message contains injected Bcc header:\n%s", msg)
	}
}

func TestSendRejectsHeaderInjectionAddresses(t *testing.T) {
	cfg := Config{
		SMTPHost:  "127.0.0.1",
		SMTPPort:  2525,
		FromEmail: "noreply@example.com",
	}

	if err := Send(context.Background(), cfg, []string{"user@example.com\r\nBcc: x@example.com"}, "Login code", "body"); err == nil {
		t.Fatal("expected recipient line break to be rejected")
	}

	cfg.FromEmail = "noreply@example.com\r\nBcc: x@example.com"
	if err := Send(context.Background(), cfg, []string{"user@example.com"}, "Login code", "body"); err == nil {
		t.Fatal("expected sender line break to be rejected")
	}
}

func TestNormalizeRecipientsUsesAddressOnly(t *testing.T) {
	recipients, err := normalizeRecipients([]string{"User <user@example.com>"})
	if err != nil {
		t.Fatalf("normalize recipients: %v", err)
	}
	if len(recipients) != 1 || recipients[0] != "user@example.com" {
		t.Fatalf("unexpected recipients: %#v", recipients)
	}
}

func headerLine(t *testing.T, msg, prefix string) string {
	t.Helper()
	for _, line := range strings.Split(msg, "\r\n") {
		if strings.HasPrefix(line, prefix) {
			return line
		}
	}
	t.Fatalf("missing header %s in message:\n%s", prefix, msg)
	return ""
}
