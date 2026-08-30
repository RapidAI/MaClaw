package websearch

import "testing"

func TestCanonicalFetchURLSchemelessAndProtocolRelative(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"https://example.com/a": "https://example.com/a",
		"example.invalid/skip":  "https://example.invalid/skip",
		"//example.com/docs":    "https://example.com/docs",
		"FTP://files.example.com/a": "FTP://files.example.com/a",
		"":                      "",
	}
	for raw, want := range cases {
		if got := CanonicalFetchURL(raw); got != want {
			t.Errorf("CanonicalFetchURL(%q)=%q, want %q", raw, got, want)
		}
	}
}

func TestFetchURLHostnameNormalizesSchemelessSkipTokens(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"https://example.invalid/skip": "example.invalid",
		"example.invalid/skip":         "example.invalid",
		"//example.invalid/skip":       "example.invalid",
		"http://localhost/file":        "localhost",
		"http://[::1]/file":            "::1",
		"https://example.com/a.pdf":    "example.com",
		"https://EXAMPLE.COM./a.pdf":   "example.com",
		"HTTPS://example.invalid/skip": "example.invalid",
		"":                             "",
	}
	for raw, want := range cases {
		if got := FetchURLHostname(raw); got != want {
			t.Errorf("FetchURLHostname(%q)=%q, want %q", raw, got, want)
		}
	}
}

func TestPlaceholderFetchHostDoesNotTreatLANOrExampleDotComAsSkipTokens(t *testing.T) {
	t.Parallel()
	placeholders := []string{"example.invalid", "foo.test", "docs.example", "localhost", "127.0.0.1", "::1"}
	for _, host := range placeholders {
		if !IsPlaceholderFetchHost(host) {
			t.Errorf("%q should be a placeholder fetch host", host)
		}
	}
	allowed := []string{"example.com", "github.com", "192.168.1.10", "10.0.0.5", "nas.local"}
	for _, host := range allowed {
		if IsPlaceholderFetchHost(host) {
			t.Errorf("%q must not be classified as a skip-token host", host)
		}
	}
	if !IsBlockedPublicHost("192.168.1.10") || !IsBlockedPublicHost("nas.local") {
		t.Fatal("public-network mode must still reject LAN and .local")
	}
	if IsBlockedPublicHost("example.com") {
		t.Fatal("example.com is a registered documentation domain, not a reserved TLD")
	}
}
