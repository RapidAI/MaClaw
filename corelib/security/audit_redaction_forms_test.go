package security

import (
	"strings"
	"testing"
)

// Regression tests for two defects found while implementing P0-3
// (2026-09-09): the redactor missed `key: value` and `API Key = value`
// shapes, and misread the hyphen of a preceding word as a flag prefix.

func TestRedactSensitiveStringHandlesColonAndSpacedKeywordForms(t *testing.T) {
	cases := []struct {
		in     string
		secret string
	}{
		{"apikey=compact-key", "compact-key"},
		{"apisecret:compact-secret", "compact-secret"},
		{"API Key = display-key", "display-key"},
		{"API Key: display-key", "display-key"},
		{"API Secret: display-secret", "display-secret"},
		{"my-api-key: abc123", "abc123"},
		{"private_key = pem-material", "pem-material"},
	}
	for _, c := range cases {
		got := RedactSensitiveString(c.in)
		if strings.Contains(got, c.secret) {
			t.Errorf("RedactSensitiveString(%q) = %q, still contains secret %q", c.in, got, c.secret)
		}
		if !strings.Contains(got, auditRedactedValue) {
			t.Errorf("RedactSensitiveString(%q) = %q, want it to contain %q", c.in, got, auditRedactedValue)
		}
	}
}

// The hyphen in `compact-secret` must not be mistaken for a flag dash, which
// previously caused `-secret API` to match: the real secret survived and an
// unrelated neighbouring word was redacted instead.
func TestRedactSensitiveStringDoesNotMisreadHyphenatedWord(t *testing.T) {
	in := `apisecret:compact-secret API Key = display-key`
	got := RedactSensitiveString(in)

	if strings.Contains(got, "compact-secret") {
		t.Errorf("secret value survived: %q", got)
	}
	if strings.Contains(got, "display-key") {
		t.Errorf("second secret value survived: %q", got)
	}
	// The literal word "API" must not have been redacted on its own: it is the
	// key, not the secret, and only its value should be replaced.
	if strings.Contains(got, "[REDACTED] Key") {
		t.Errorf("redactor corrupted the key instead of the value: %q", got)
	}
}

func TestRedactSensitiveStringLeavesOrdinaryTextAlone(t *testing.T) {
	plain := []string{
		`path=C:\data\project`,
		`--verbose --output build/report.json`,
		`elapsed=12ms status=ok`,
	}
	for _, in := range plain {
		if got := RedactSensitiveString(in); got != in {
			t.Errorf("RedactSensitiveString(%q) = %q, want unchanged", in, got)
		}
	}
}

func TestRedactSensitiveStringFlagsStillWork(t *testing.T) {
	cases := []struct {
		in     string
		secret string
	}{
		{"run --password hunter2 now", "hunter2"},
		{"run --password=hunter2 now", "hunter2"},
		{"run -token abc123 now", "abc123"},
		{"/password: topsecret", "topsecret"},
	}
	for _, c := range cases {
		got := RedactSensitiveString(c.in)
		if strings.Contains(got, c.secret) {
			t.Errorf("RedactSensitiveString(%q) = %q, still contains %q", c.in, got, c.secret)
		}
	}
}

// Chaining is what broke the export path: a lossy first pass rewrote the text
// into a shape the second pass no longer recognised. Redaction must be
// idempotent.
func TestRedactSensitiveStringIsIdempotent(t *testing.T) {
	in := `apikey=compact-key apisecret:compact-secret API Key = display-key path=C:\data`
	once := RedactSensitiveString(in)
	twice := RedactSensitiveString(once)
	if once != twice {
		t.Errorf("redaction is not idempotent:\n once=%q\n twice=%q", once, twice)
	}
	for _, secret := range []string{"compact-key", "compact-secret", "display-key"} {
		if strings.Contains(twice, secret) {
			t.Errorf("secret %q survived two passes: %q", secret, twice)
		}
	}
}

// A bare value that swallows the closing quote leaves structurally broken log
// text (`'X-Token: [REDACTED] https://...`). The quoted alternatives are tried
// first, so the unquoted character class must not contain quote characters —
// otherwise it wins before they can match.
func TestRedactSensitiveStringPreservesSurroundingQuotes(t *testing.T) {
	cases := []struct {
		in     string
		want   string
		secret string
	}{
		{"curl -H 'X-Token: tkn-1' https://example.com", "curl -H 'X-Token: [REDACTED]' https://example.com", "tkn-1"},
		{`--password 'quoted pass' --other x`, `--password [REDACTED] --other x`, "quoted pass"},
		{`secret="dq" tail`, `secret=[REDACTED] tail`, "dq"},
	}
	for _, c := range cases {
		got := RedactSensitiveString(c.in)
		if got != c.want {
			t.Errorf("RedactSensitiveString(%q)\n got = %q\n want= %q", c.in, got, c.want)
		}
		if strings.Contains(got, c.secret) {
			t.Errorf("RedactSensitiveString(%q) = %q, still contains %q", c.in, got, c.secret)
		}
	}
}
