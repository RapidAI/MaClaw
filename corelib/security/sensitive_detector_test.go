package security

import (
	"testing"
)

func TestSensitiveDetector_DetectAPIKey(t *testing.T) {
	d := NewSensitiveDetector()
	for _, tc := range []struct {
		input    string
		wantCat  string
		wantHit  bool
	}{
		{"token: sk-abcdefghijklmnopqrstuvwxyz", "api_key", true},
		{"AKIAIOSFODNN7EXAMPLE", "api_key", true},
		{"sk-short", "", false}, // too short
		{"no secrets here", "", false},
	} {
		matches := d.Detect(tc.input)
		if tc.wantHit {
			if len(matches) == 0 {
				t.Errorf("Detect(%q) = empty, want category %q", tc.input, tc.wantCat)
				continue
			}
			found := false
			for _, m := range matches {
				if m.Category == tc.wantCat {
					found = true
				}
			}
			if !found {
				t.Errorf("Detect(%q) missing category %q, got %v", tc.input, tc.wantCat, matches)
			}
		} else if len(matches) > 0 {
			t.Errorf("Detect(%q) = %v, want empty", tc.input, matches)
		}
	}
}

func TestSensitiveDetector_DetectJWT(t *testing.T) {
	d := NewSensitiveDetector()
	jwt := "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.abc123def456"
	matches := d.Detect(jwt)
	if len(matches) == 0 {
		t.Fatal("expected JWT detection")
	}
	if matches[0].Category != "jwt" {
		t.Errorf("got category %q, want jwt", matches[0].Category)
	}
}

func TestSensitiveDetector_DetectPrivateKey(t *testing.T) {
	d := NewSensitiveDetector()
	matches := d.Detect("-----BEGIN RSA PRIVATE KEY-----")
	if len(matches) == 0 || matches[0].Category != "private_key" {
		t.Errorf("expected private_key detection, got %v", matches)
	}
}

func TestSensitiveDetector_DetectPassword(t *testing.T) {
	d := NewSensitiveDetector()
	for _, input := range []string{"password=secret123", "PASSWD: hunter2", "pwd=abc"} {
		if matches := d.Detect(input); len(matches) == 0 {
			t.Errorf("Detect(%q) = empty, want password", input)
		}
	}
}

func TestSensitiveDetector_NoFalsePositive(t *testing.T) {
	d := NewSensitiveDetector()
	for _, input := range []string{"", "hello world", "func main() {}", "x := 42"} {
		if matches := d.Detect(input); len(matches) > 0 {
			t.Errorf("Detect(%q) = %v, want empty", input, matches)
		}
	}
}

func TestSensitiveDetector_RedactRemovesSensitive(t *testing.T) {
	d := NewSensitiveDetector()
	input := "key=sk-abcdefghijklmnopqrstuvwxyz and password=secret123"
	redacted := d.Redact(input)
	if remaining := d.Detect(redacted); len(remaining) > 0 {
		t.Errorf("Detect(Redact(input)) = %v, want empty; redacted=%q", remaining, redacted)
	}
}
