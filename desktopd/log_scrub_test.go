package desktopd

import (
	"strings"
	"testing"
)

func TestScrubDesktopdLogDropsSecrets(t *testing.T) {
	got := scrubDesktopdLog("dial failed node_secret: abcdef Bearer tok-value\npassword=\"my secret\" authorization: Bearer live-token blob=0123456789abcdef0123456789abcdef0123456789abcdef")
	if strings.Contains(got, "\n") || strings.Contains(got, "abcdef") || strings.Contains(got, "tok-value") || strings.Contains(got, "my secret") || strings.Contains(got, "live-token") || strings.Contains(got, "0123456789abcdef0123456789abcdef0123456789abcdef") {
		t.Fatalf("secret leaked: %q", got)
	}
	if !strings.Contains(got, "node_secret=<redacted>") || !strings.Contains(got, "Bearer <redacted>") || !strings.Contains(got, "password=<redacted>") {
		t.Fatalf("redaction missing: %q", got)
	}
}
