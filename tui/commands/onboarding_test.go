package commands

import (
	"bufio"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestRenderTerminalQRIsCompactForSSH(t *testing.T) {
	rendered, err := renderTerminalQR("https://example.com/weixin-login?token=test")
	if err != nil {
		t.Fatalf("render QR: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(stripANSITest(rendered)), "\n")
	if len(lines) > 24 {
		t.Fatalf("QR rendered too tall for terminal onboarding: %d lines", len(lines))
	}
	for _, line := range lines {
		if got := utf8.RuneCountInString(line); got > 88 {
			t.Fatalf("QR line too wide: %d chars: %q", got, line)
		}
	}
}

func TestPromptHubCenterChoosesNumber(t *testing.T) {
	reader := bufio.NewReader(strings.NewReader("2\n"))
	got, err := promptHubCenter(reader, []string{"https://primary.example", "https://backup.example"}, "https://primary.example")
	if err != nil {
		t.Fatalf("prompt HubCenter: %v", err)
	}
	if got != "https://backup.example" {
		t.Fatalf("HubCenter = %q, want selected backup", got)
	}
}

func TestPromptHubCenterManualFallback(t *testing.T) {
	reader := bufio.NewReader(strings.NewReader("m\nhttps://private.example/\n"))
	got, err := promptHubCenter(reader, []string{"https://primary.example"}, "https://primary.example")
	if err != nil {
		t.Fatalf("prompt HubCenter: %v", err)
	}
	if got != "https://private.example" {
		t.Fatalf("HubCenter = %q, want manual URL without trailing slash", got)
	}
}

func TestPromptHubCenterUnknownKeepsCurrent(t *testing.T) {
	reader := bufio.NewReader(strings.NewReader("wat\n"))
	got, err := promptHubCenter(reader, []string{"https://primary.example"}, "https://primary.example")
	if err != nil {
		t.Fatalf("prompt HubCenter: %v", err)
	}
	if got != "https://primary.example" {
		t.Fatalf("HubCenter = %q, want current", got)
	}
}

func TestWeixinQRPayloadLineHiddenByDefault(t *testing.T) {
	const payload = "https://example.com/weixin-login?token=secret"
	got := weixinQRPayloadLine(payload, false)
	if strings.Contains(got, "token=secret") {
		t.Fatalf("default QR payload line should not reveal token: %q", got)
	}
	if !strings.Contains(got, "--show-qr-payload") {
		t.Fatalf("hidden QR payload line should mention explicit fallback flag: %q", got)
	}
}

func TestWeixinQRPayloadLineCanBeExplicitlyShown(t *testing.T) {
	const payload = "https://example.com/weixin-login?token=secret"
	got := weixinQRPayloadLine(payload, true)
	if !strings.Contains(got, payload) {
		t.Fatalf("explicit QR payload line should include payload: %q", got)
	}
}

func TestValidOnboardingEmailForCLI(t *testing.T) {
	for _, email := range []string{"user@example.com", "USER+tag@example.co"} {
		if !validOnboardingEmailForCLI(email) {
			t.Fatalf("email should be valid: %q", email)
		}
	}
	for _, email := range []string{"", "not-an-email", "user@", "@example.com", "user@example", "user @example.com"} {
		if validOnboardingEmailForCLI(email) {
			t.Fatalf("email should be invalid: %q", email)
		}
	}
}

func TestValidOnboardingHubCenterForCLI(t *testing.T) {
	for _, value := range []string{"https://center.example", "http://127.0.0.1:8080"} {
		if !validOnboardingHubCenterForCLI(value) {
			t.Fatalf("HubCenter should be valid: %q", value)
		}
	}
	for _, value := range []string{"", "center.example", "ftp://center.example", "https://"} {
		if validOnboardingHubCenterForCLI(value) {
			t.Fatalf("HubCenter should be invalid: %q", value)
		}
	}
}

func stripANSITest(s string) string {
	var b strings.Builder
	inEsc := false
	for _, r := range s {
		if inEsc {
			if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') {
				inEsc = false
			}
			continue
		}
		if r == 0x1b {
			inEsc = true
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
