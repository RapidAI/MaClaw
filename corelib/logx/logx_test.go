package logx

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
)

func newCaptureLogger() (*slog.Logger, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	return New(buf, Options{Format: "json", Level: slog.LevelDebug}), buf
}

func TestRedactHandlerMasksSensitiveKeys(t *testing.T) {
	l, buf := newCaptureLogger()
	l.Info("config loaded", slog.String("api_key", "sk-live-123456"), slog.String("model", "gpt"))
	out := buf.String()
	if strings.Contains(out, "sk-live-123456") {
		t.Fatalf("api_key leaked: %s", out)
	}
	if !strings.Contains(out, "gpt") {
		t.Fatalf("non-sensitive attr missing: %s", out)
	}
}

func TestRedactStringValues(t *testing.T) {
	cases := []struct{ name, in, mustNot string }{
		{"bearer token", "Authorization: Bearer abcdefgh12345678", "abcdefgh12345678"},
		{"key value secret", "api_key=sk-abcdef123456", "sk-abcdef123456"},
		{"url userinfo", "dial https://user:pass@example.com/v1", "user:pass@"},
		{"windows path", `write C:\Users\me\secret\report.pdf failed`, `C:\Users\me\secret`},
		{"unix home path", "read /Users/me/private/notes.md failed", "/Users/me/private"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := RedactString(tc.in)
			if strings.Contains(out, tc.mustNot) {
				t.Fatalf("RedactString(%q) leaked %q -> %q", tc.in, tc.mustNot, out)
			}
		})
	}
}

func TestRedactStringKeepsSafeText(t *testing.T) {
	in := "run completed for session s-1 with 3 tool calls"
	if got := RedactString(in); got != in {
		t.Fatalf("safe text modified: %q", got)
	}
	if got := RedactString("GET /api/v1/instances/abc/runs"); !strings.Contains(got, "/api/v1/instances") {
		t.Fatalf("relative API path mangled: %q", got)
	}
}

func TestWithCorrelationAttachesIDs(t *testing.T) {
	l, buf := newCaptureLogger()
	ctx := agentruntime.WithCorrelationID(context.Background(), "req-123")
	l = WithCorrelation(ctx, l)
	l.Info("admission")
	out := buf.String()
	if !strings.Contains(out, "req-123") {
		t.Fatalf("correlation id missing: %s", out)
	}
}

func TestRedactHandlerRecursesGroups(t *testing.T) {
	l, buf := newCaptureLogger()
	l.Info("login", slog.Group("creds", slog.String("password", "hunter2secret"), slog.String("user", "alice")))
	out := buf.String()
	if strings.Contains(out, "hunter2secret") {
		t.Fatalf("grouped password leaked: %s", out)
	}
	if !strings.Contains(out, "alice") {
		t.Fatalf("grouped safe value lost: %s", out)
	}
}

func TestRedactHandlerRedactsErrorValues(t *testing.T) {
	l, buf := newCaptureLogger()
	l.Error("op failed", slog.Any("err", errors.New(`open C:\Users\me\secret\x.txt: denied`)))
	if out := buf.String(); strings.Contains(out, `C:\Users\me\secret`) {
		t.Fatalf("error value leaked path: %s", out)
	}
}

func TestRedactStringMultipleURLs(t *testing.T) {
	in := "dial https://username:password@example.com/v1 ok https://u:p@h2/v2 end"
	out := RedactString(in)
	if strings.Contains(out, "username:password") || strings.Contains(out, "u:p@") {
		t.Fatalf("second URL userinfo leaked: %q", out)
	}
	if !strings.Contains(out, "example.com") || !strings.Contains(out, "h2/v2") {
		t.Fatalf("host/path lost: %q", out)
	}
}

func TestRedactStringPreservesFragmentAndIPv6(t *testing.T) {
	out := RedactString("see https://user:pass@[::1]:8080/p#frag end")
	if strings.Contains(out, "user:pass") {
		t.Fatalf("userinfo leaked: %q", out)
	}
	if !strings.Contains(out, "#frag") {
		t.Fatalf("fragment lost: %q", out)
	}
	if !strings.Contains(out, "[::1]:8080") {
		t.Fatalf("IPv6 host mangled: %q", out)
	}
}

func TestRedactStringQuotedAndBareSecrets(t *testing.T) {
	if out := RedactString(`password="my secret pass" next`); strings.Contains(out, "secret pass") {
		t.Fatalf("quoted password leaked: %q", out)
	}
	if out := RedactString("token=ghp_abcdefghijklmnop"); strings.Contains(out, "ghp_") {
		t.Fatalf("bare token leaked: %q", out)
	}
}

func TestRedactKeepsNumericStatAttrs(t *testing.T) {
	l, buf := newCaptureLogger()
	l.Info("usage", slog.Int("token_count", 42))
	if out := buf.String(); !strings.Contains(out, "42") {
		t.Fatalf("numeric stat over-masked: %s", out)
	}
}

func TestRedactStringUppercaseScheme(t *testing.T) {
	if out := RedactString("HTTPS://user:pass@example.com/x"); strings.Contains(out, "user:pass") {
		t.Fatalf("uppercase scheme leaked: %q", out)
	}
}
