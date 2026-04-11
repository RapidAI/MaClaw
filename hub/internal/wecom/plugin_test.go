package wecom

import (
	"testing"
	"time"
)

func TestStripAtMention(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"@Bot 你好", "你好"},
		{"@机器人 帮我查一下", "帮我查一下"},
		{"@Bot", ""},
		{"你好 @Bot 世界", "你好 世界"},
		{"没有at的消息", "没有at的消息"},
		{"", ""},
		{"@A @B 你好", "你好"},
		{"  @Bot  你好  ", "你好"},
	}
	for _, tt := range tests {
		got := stripAtMention(tt.input)
		if got != tt.want {
			t.Errorf("stripAtMention(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestIsDuplicateMsg(t *testing.T) {
	// Reset dedup state
	msgDedup.mu.Lock()
	msgDedup.seen = make(map[string]time.Time)
	msgDedup.mu.Unlock()

	if isDuplicateMsg("msg1") {
		t.Error("first call should not be duplicate")
	}
	if !isDuplicateMsg("msg1") {
		t.Error("second call should be duplicate")
	}
	if isDuplicateMsg("msg2") {
		t.Error("different msg should not be duplicate")
	}
	if isDuplicateMsg("") {
		t.Error("empty msgID should not be duplicate")
	}
}

func TestLooksLikeEmail(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"user@example.com", true},
		{"a@b.c", true},
		{"not-email", false},
		{"@missing.com", false},
		{"missing@", false},
		{"has space@example.com", false},
		{"", false},
	}
	for _, tt := range tests {
		got := looksLikeEmail(tt.input)
		if got != tt.want {
			t.Errorf("looksLikeEmail(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestIsVerifyCode(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"123456", true},
		{"000000", true},
		{"12345", false},
		{"1234567", false},
		{"abcdef", false},
		{"12345a", false},
		{"", false},
		{" 123456 ", true}, // trimmed
	}
	for _, tt := range tests {
		got := isVerifyCode(tt.input)
		if got != tt.want {
			t.Errorf("isVerifyCode(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestGenerateReqID(t *testing.T) {
	id1 := generateReqID("test")
	id2 := generateReqID("test")
	if id1 == id2 {
		t.Error("generateReqID should produce unique IDs")
	}
	if len(id1) < 10 {
		t.Errorf("generateReqID too short: %q", id1)
	}
}

func TestGenerateCode(t *testing.T) {
	code := generateCode()
	if len(code) != 6 {
		t.Errorf("generateCode length = %d, want 6", len(code))
	}
	for _, c := range code {
		if c < '0' || c > '9' {
			t.Errorf("generateCode contains non-digit: %q", code)
		}
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("hello", 10); got != "hello" {
		t.Errorf("truncate short = %q", got)
	}
	if got := truncate("hello world", 5); got != "hello…" {
		t.Errorf("truncate long = %q", got)
	}
	if got := truncate("你好世界", 2); got != "你好…" {
		t.Errorf("truncate chinese = %q", got)
	}
}

func TestExtractText(t *testing.T) {
	p := &Plugin{}

	// Text message
	body := &callbackBody{MsgType: "text", Text: &struct {
		Content string `json:"content"`
	}{Content: "hello"}}
	if got := p.extractText(body); got != "hello" {
		t.Errorf("extractText text = %q", got)
	}

	// Voice with transcription
	body2 := &callbackBody{MsgType: "voice", Voice: &struct {
		URL    string `json:"url,omitempty"`
		AESKey string `json:"aeskey,omitempty"`
		Text   string `json:"text,omitempty"`
	}{Text: "transcribed text"}}
	if got := p.extractText(body2); got != "transcribed text" {
		t.Errorf("extractText voice = %q", got)
	}

	// Empty
	body3 := &callbackBody{MsgType: "image"}
	if got := p.extractText(body3); got != "" {
		t.Errorf("extractText image = %q, want empty", got)
	}
}

func TestPkcs7Unpad(t *testing.T) {
	// Valid padding
	data := []byte{1, 2, 3, 4, 4, 4, 4, 4}
	got, err := pkcs7Unpad(data)
	if err != nil {
		t.Fatalf("pkcs7Unpad error: %v", err)
	}
	if len(got) != 4 || got[0] != 1 {
		t.Errorf("pkcs7Unpad = %v", got)
	}

	// Invalid padding
	_, err = pkcs7Unpad([]byte{1, 2, 3, 0})
	if err == nil {
		t.Error("pkcs7Unpad should fail on zero padding")
	}

	// Empty
	_, err = pkcs7Unpad([]byte{})
	if err == nil {
		t.Error("pkcs7Unpad should fail on empty")
	}
}
