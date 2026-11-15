package guiapp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestDesktopNodeDetailLogRecordsTheStepAndHidesSecrets(t *testing.T) {
	corelib.SetLogDetailEnabled(true)
	t.Cleanup(func() { corelib.SetLogDetailEnabled(false) })
	a := &App{testHomeDir: t.TempDir()}
	a.desktopNodeDetail("dn_1", "finish", "health failed password=secret DESKTOPD_TOKEN=abc Bearer tok-value password=\"my secret\" desktopd_key:\"abcdef0123\" authorization: Bearer live-token https://user:pw@hub.example/x blob=0123456789abcdef0123456789abcdef0123456789abcdef\nnext")
	path := filepath.Join(a.getMaclawBaseDir(), "logs", "desktop-node-detail.log")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, want := range []string{"id=dn_1", "step=finish", "health failed", "password=<redacted>", "DESKTOPD_TOKEN=<redacted>", "Bearer <redacted>", "desktopd_key=<redacted>", "https://<redacted>@hub.example/x blob=<redacted> next"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in %s", want, text)
		}
	}
	if strings.Count(text, "\n") != 1 {
		t.Fatalf("detail line broke across lines: %q", text)
	}
	for _, secret := range []string{"secret", "abc", "tok-value", "abcdef0123", "user:pw", "live-token", "0123456789abcdef0123456789abcdef0123456789abcdef"} {
		if strings.Contains(text, secret) {
			t.Fatalf("logged secret %q in %s", secret, text)
		}
	}
	corelib.SetLogDetailEnabled(false)
	a.desktopNodeDetail("dn_1", "later", "should not appear")
	raw, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "later") || strings.Contains(string(raw), "should not appear") {
		t.Fatalf("wrote while detail logging was off: %s", raw)
	}
}
