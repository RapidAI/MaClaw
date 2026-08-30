package knowledge

import "testing"

func TestValidatePublicHTTPURL(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		wantErr bool
	}{
		{name: "public https", raw: "https://example.com/a#frag"},
		{name: "scheme default", raw: "example.com/docs"},
		{name: "protocol relative public", raw: "//example.com/docs"},
		{name: "localhost", raw: "http://localhost:8080", wantErr: true},
		{name: "loopback", raw: "http://127.0.0.1", wantErr: true},
		{name: "private ipv4", raw: "http://192.168.1.1", wantErr: true},
		{name: "metadata", raw: "http://169.254.169.254/latest", wantErr: true},
		{name: "file scheme", raw: "file:///tmp/a", wantErr: true},
		{name: "reserved tld", raw: "https://example.invalid/skip", wantErr: true},
		{name: "protocol relative reserved", raw: "//example.invalid/skip", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u, err := ValidatePublicHTTPURL(tt.raw)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q", tt.raw)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if u.Fragment != "" {
				t.Fatalf("fragment should be stripped, got %q", u.Fragment)
			}
		})
	}
}

func TestIsBlockedHost(t *testing.T) {
	blocked := []string{"localhost", "api.localhost", "10.0.0.1", "172.16.0.1", "172.31.255.1", "192.168.0.1", "::1", "fd00::1", "example.invalid", "foo.test"}
	for _, host := range blocked {
		if !IsBlockedHost(host) {
			t.Fatalf("expected %s to be blocked", host)
		}
	}
	allowed := []string{"example.com", "openai.com", "8.8.8.8", "2606:4700:4700::1111"}
	for _, host := range allowed {
		if IsBlockedHost(host) {
			t.Fatalf("expected %s to be allowed", host)
		}
	}
}
