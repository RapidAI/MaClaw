package security

import (
	"strings"
	"testing"
)

func TestSensitiveDetector_DetectAPIKey(t *testing.T) {
	d := NewSensitiveDetector()
	for _, tc := range []struct {
		input   string
		wantCat string
		wantHit bool
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

// TestSensitiveDetector_IncidentPayload is the P0-4 acceptance case: the
// exact §1.1 incident text must be detected, and redaction must preserve the
// hostname while removing the password.
func TestSensitiveDetector_IncidentPayload(t *testing.T) {
	d := NewSensitiveDetector()
	incident := "将以下信息保存于知识库：驱网服务器 www.driverdevelop.com root sunion123"
	matches := d.Detect(incident)
	if len(matches) == 0 {
		t.Fatalf("Detect(%q) = empty, want a password match", incident)
	}
	found := false
	for _, m := range matches {
		if m.Category == "password" && m.Pattern == "user_password_pair" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected user_password_pair match, got %v", matches)
	}
	redacted := d.Redact(incident)
	if strings.Contains(redacted, "sunion123") {
		t.Fatalf("redacted text still contains the password: %q", redacted)
	}
	if !strings.Contains(redacted, "www.driverdevelop.com") {
		t.Fatalf("redacted text lost the hostname: %q", redacted)
	}
	if !strings.Contains(redacted, "root [REDACTED]") {
		t.Fatalf("redacted text should keep the username: %q", redacted)
	}
}

func TestSensitiveDetector_ChineseCredentialForms(t *testing.T) {
	d := NewSensitiveDetector()
	for _, input := range []string{
		"密码：abc123",
		"密码: abc123",
		"口令=hello2024",
		"访问密钥：AKIAIOSFODNN7EXAMPLE",
		"数据库密码是 Sunion@2024",
		"用户名 admin 密码 P@ssw0rd!",
		"服务器 192.168.1.10，root 账户密码：sunion123",
	} {
		if matches := d.Detect(input); len(matches) == 0 {
			t.Errorf("Detect(%q) = empty, want a credential match", input)
		}
	}
}

func TestSensitiveDetector_UserPasswordPairs(t *testing.T) {
	d := NewSensitiveDetector()
	for _, input := range []string{
		"root sunion123",
		"login as admin Passw0rd!",
		"ssh ubuntu deploy@2024",
		"mysql -u root secret99",
	} {
		matches := d.Detect(input)
		found := false
		for _, m := range matches {
			if m.Pattern == "user_password_pair" {
				found = true
			}
		}
		if !found {
			t.Errorf("Detect(%q) missing user_password_pair, got %v", input, matches)
		}
	}
}

func TestSensitiveDetector_NegativeNoFalsePositives(t *testing.T) {
	d := NewSensitiveDetector()
	// Ordinary addresses, names, and prose must not flag. Favor misses over
	// false alarms, but obvious credential shapes must hit.
	for _, input := range []string{
		"meet Zhang Wei at the office",
		"visit https://example.com/docs for details",
		"the root directory of the project",
		"open the admin panel in your browser",
		"密码学是研究信息安全的学科",
		"密钥管理是系统工程",
		"他叫密码子，是个昵称",
		"admin panel overview",
		"root user permissions",
		"my cat is named Token",
		"token ring network topology",
	} {
		if matches := d.Detect(input); len(matches) > 0 {
			t.Errorf("Detect(%q) = %v, want empty", input, matches)
		}
	}
}
