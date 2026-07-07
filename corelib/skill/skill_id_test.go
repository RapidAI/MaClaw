package skill

import "testing"

func TestIsValidSkillID(t *testing.T) {
	valid := []string{
		"lovstudio.any2pdf",
		"zhangsan-a1b2.paper-translator",
		"abc.de",                       // min valid: publisher=3, name=2
		"community.drawio-export",
		"enterprise-hub.contract-review",
	}
	for _, id := range valid {
		if !IsValidSkillID(id) {
			t.Errorf("IsValidSkillID(%q) = false, want true", id)
		}
	}

	invalid := []string{
		"",
		"a",
		"ab",                    // too short (no dot)
		"a.b",                   // publisher too short (1 char) + name too short (1 char)
		"ab.c",                  // publisher too short (2 chars)
		"abc.d",                 // name too short (1 char)
		".any2pdf",              // empty publisher
		"lovstudio.",            // empty name
		"lovstudio",             // no dot
		"Lovstudio.Any2pdf",     // uppercase
		"lov studio.any2pdf",    // space
		"lov_studio.any2pdf",    // underscore
		"lovstudio.any2pdf!",    // special char
		"-studio.any2pdf",       // starts with hyphen
		"studio-.any2pdf",       // ends with hyphen
		"lovstudio.-any2pdf",    // name starts with hyphen
		"lovstudio.any2pdf-",    // name ends with hyphen
	}
	for _, id := range invalid {
		if IsValidSkillID(id) {
			t.Errorf("IsValidSkillID(%q) = true, want false", id)
		}
	}
}

func TestParseSkillID(t *testing.T) {
	pub, name, ok := ParseSkillID("lovstudio.any2pdf")
	if !ok || pub != "lovstudio" || name != "any2pdf" {
		t.Errorf("ParseSkillID(lovstudio.any2pdf) = (%q, %q, %v), want (lovstudio, any2pdf, true)", pub, name, ok)
	}

	pub, name, ok = ParseSkillID("zhangsan-a1b2.paper-translator")
	if !ok || pub != "zhangsan-a1b2" || name != "paper-translator" {
		t.Errorf("ParseSkillID(zhangsan-a1b2.paper-translator) = (%q, %q, %v)", pub, name, ok)
	}

	_, _, ok = ParseSkillID("invalid")
	if ok {
		t.Error("ParseSkillID(invalid) = ok, want !ok")
	}
}

func TestDerivePublisher(t *testing.T) {
	tests := []struct {
		email string
		want  string // just check prefix + format, not exact hash
	}{
		{"zhangsan@gmail.com", "zhangsan-"},
		{"alice@company.com", "alice-"},
		{"bob123@example.org", "bob123-"},
		{"a@x.com", "user-"}, // too short prefix → "user" fallback
		{"", ""},
	}
	for _, tt := range tests {
		got := DerivePublisher(tt.email)
		if tt.want == "" {
			if got != "" {
				t.Errorf("DerivePublisher(%q) = %q, want empty", tt.email, got)
			}
			continue
		}
		if !startsWith(got, tt.want) {
			t.Errorf("DerivePublisher(%q) = %q, want prefix %q", tt.email, got, tt.want)
		}
		// Check format: only a-z, 0-9, -
		for _, r := range got {
			if !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-') {
				t.Errorf("DerivePublisher(%q) = %q contains invalid char %q", tt.email, got, string(r))
			}
		}
	}

	// Same email always produces the same publisher (deterministic)
	p1 := DerivePublisher("test@example.com")
	p2 := DerivePublisher("test@example.com")
	if p1 != p2 {
		t.Errorf("DerivePublisher not deterministic: %q != %q", p1, p2)
	}

	// Different domains produce different publishers (hash suffix differs)
	pa := DerivePublisher("zhangsan@gmail.com")
	pb := DerivePublisher("zhangsan@qq.com")
	if pa == pb {
		t.Errorf("DerivePublisher same for different domains: %q == %q", pa, pb)
	}
}

func TestDeriveSkillID(t *testing.T) {
	id := DeriveSkillID("zhangsan@gmail.com", "Any2PDF 文档转换")
	if id == "" {
		t.Fatal("DeriveSkillID returned empty")
	}
	if !IsValidSkillID(id) {
		t.Errorf("DeriveSkillID produced invalid id: %q", id)
	}

	// Pure CJK name should still produce valid ID (via hash fallback)
	id2 := DeriveSkillID("user@test.com", "文档转换工具")
	if id2 == "" {
		t.Fatal("DeriveSkillID returned empty for CJK name")
	}
	if !IsValidSkillID(id2) {
		t.Errorf("DeriveSkillID produced invalid id for CJK: %q", id2)
	}

	// Empty inputs
	if DeriveSkillID("", "any2pdf") != "" {
		t.Error("DeriveSkillID should return empty for empty email")
	}
	if DeriveSkillID("a@b.com", "") != "" {
		t.Error("DeriveSkillID should return empty for empty name")
	}
}

func TestSanitizeSkillNameForID(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"any2pdf", "any2pdf"},
		{"Any2PDF", "any2pdf"},
		{"Paper Translator", "paper-translator"},
		{"my_cool_skill", "my-cool-skill"},
		{"  spaces  ", "spaces"},
		{"hello.world", "hello-world"},
		{"a--b", "a-b"}, // consecutive hyphens collapsed
	}
	for _, tt := range tests {
		got := SanitizeSkillNameForID(tt.input)
		if got != tt.want {
			t.Errorf("SanitizeSkillNameForID(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func startsWith(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}
