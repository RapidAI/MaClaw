package knowledge

import (
	"testing"
)

func TestValidateSeedURL(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantErr bool
	}{
		{"valid http", "http://example.com", false},
		{"valid https", "https://example.com/path", false},
		{"valid https with query", "https://example.com/path?q=1", false},
		{"empty string", "", true},
		{"whitespace only", "   ", true},
		{"no scheme", "example.com", true},
		{"ftp scheme", "ftp://example.com", true},
		{"file scheme", "file:///etc/passwd", true},
		{"javascript scheme", "javascript:alert(1)", true},
		{"uppercase HTTP", "HTTP://example.com", false},
		{"uppercase HTTPS", "HTTPS://example.com", false},
		{"mixed case", "HtTpS://example.com", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateSeedURL(tt.url)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateSeedURL(%q) error = %v, wantErr %v", tt.url, err, tt.wantErr)
			}
		})
	}
}

func TestNormalizeURL(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want string
	}{
		{
			name: "lowercase scheme and host",
			url:  "HTTPS://Example.COM/Path",
			want: "https://example.com/Path",
		},
		{
			name: "remove fragment",
			url:  "https://example.com/page#section",
			want: "https://example.com/page",
		},
		{
			name: "sort query params",
			url:  "https://example.com/page?z=1&a=2&m=3",
			want: "https://example.com/page?a=2&m=3&z=1",
		},
		{
			name: "remove trailing slash",
			url:  "https://example.com/path/",
			want: "https://example.com/path",
		},
		{
			name: "keep root slash",
			url:  "https://example.com/",
			want: "https://example.com/",
		},
		{
			name: "combined normalization",
			url:  "HTTP://Example.COM/Page/?b=2&a=1#frag",
			want: "http://example.com/Page?a=1&b=2",
		},
		{
			name: "empty string",
			url:  "",
			want: "",
		},
		{
			name: "no query no fragment",
			url:  "https://example.com/path",
			want: "https://example.com/path",
		},
		{
			name: "duplicate query params sorted",
			url:  "https://example.com?b=1&a=2&b=3",
			want: "https://example.com?a=2&b=1&b=3",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeURL(tt.url)
			if got != tt.want {
				t.Errorf("normalizeURL(%q) = %q, want %q", tt.url, got, tt.want)
			}
		})
	}
}

func TestIsSameDomain(t *testing.T) {
	tests := []struct {
		name      string
		seedURL   string
		candidate string
		want      bool
	}{
		{
			name:      "same domain",
			seedURL:   "https://example.com/page1",
			candidate: "https://example.com/page2",
			want:      true,
		},
		{
			name:      "same domain different case",
			seedURL:   "https://Example.COM/page1",
			candidate: "https://example.com/page2",
			want:      true,
		},
		{
			name:      "different domain",
			seedURL:   "https://example.com/page1",
			candidate: "https://other.com/page2",
			want:      false,
		},
		{
			name:      "subdomain is different",
			seedURL:   "https://example.com/page1",
			candidate: "https://sub.example.com/page2",
			want:      false,
		},
		{
			name:      "same domain different scheme",
			seedURL:   "http://example.com/page1",
			candidate: "https://example.com/page2",
			want:      true,
		},
		{
			name:      "same domain with port",
			seedURL:   "https://example.com:8080/page1",
			candidate: "https://example.com:8080/page2",
			want:      true,
		},
		{
			name:      "different port same host",
			seedURL:   "https://example.com:8080/page1",
			candidate: "https://example.com:9090/page2",
			want:      true,
		},
		{
			name:      "empty seed",
			seedURL:   "",
			candidate: "https://example.com",
			want:      false,
		},
		{
			name:      "empty candidate",
			seedURL:   "https://example.com",
			candidate: "",
			want:      false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isSameDomain(tt.seedURL, tt.candidate)
			if got != tt.want {
				t.Errorf("isSameDomain(%q, %q) = %v, want %v", tt.seedURL, tt.candidate, got, tt.want)
			}
		})
	}
}
