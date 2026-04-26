package llmservice

import (
	"testing"
)

func TestEncryptDecryptCardCodeRoundTrip(t *testing.T) {
	for i := 0; i < 20; i++ {
		code, err := GenerateCardCode()
		if err != nil {
			t.Fatal(err)
		}
		enc, err := EncryptCardCode(code)
		if err != nil {
			t.Fatalf("EncryptCardCode(%q) error = %v", code, err)
		}
		if enc == "" {
			t.Fatal("encrypted code is empty")
		}
		if enc == code {
			t.Fatal("encrypted code equals plaintext")
		}
		dec := DecryptCardCode(enc)
		if dec != NormalizeCardCode(code) {
			t.Fatalf("DecryptCardCode round-trip failed: got %q, want %q", dec, code)
		}
	}
}

func TestDecryptCardCodeGracefulDegradation(t *testing.T) {
	// Empty input
	if got := DecryptCardCode(""); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
	// Invalid base64
	if got := DecryptCardCode("not-base64!!!"); got != "" {
		t.Fatalf("expected empty for invalid base64, got %q", got)
	}
	// Valid base64 but garbage ciphertext
	if got := DecryptCardCode("AAAAAAAAAAAAAAAAAAAAAA=="); got != "" {
		t.Fatalf("expected empty for garbage ciphertext, got %q", got)
	}
}

func TestEncryptCardCodeRejectsEmpty(t *testing.T) {
	_, err := EncryptCardCode("")
	if err == nil {
		t.Fatal("expected error for empty code")
	}
}

func TestPlainCodeMethodOnRechargeCard(t *testing.T) {
	code, err := GenerateCardCode()
	if err != nil {
		t.Fatal(err)
	}
	enc, err := EncryptCardCode(code)
	if err != nil {
		t.Fatal(err)
	}
	card := RechargeCard{
		ID:            "card-test",
		CodeHash:      HashCode(code),
		EncryptedCode: enc,
	}
	if got := card.PlainCode(); got != NormalizeCardCode(code) {
		t.Fatalf("PlainCode() = %q, want %q", got, code)
	}

	// Legacy card without encrypted code
	legacyCard := RechargeCard{
		ID:       "card-legacy",
		CodeHash: HashCode(code),
	}
	if got := legacyCard.PlainCode(); got != "" {
		t.Fatalf("legacy PlainCode() = %q, want empty", got)
	}
}
