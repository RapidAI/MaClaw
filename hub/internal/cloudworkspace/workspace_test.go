package cloudworkspace

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestNormalizeNameNFCAndLower(t *testing.T) {
	precomposed := "É"
	decomposed := "E\u0301"
	if got := normalizeName(precomposed); got != normalizeName(decomposed) {
		t.Fatalf("nfc mismatch %q vs %q", normalizeName(precomposed), normalizeName(decomposed))
	}
	if got := normalizeName("  Foo "); got != "foo" {
		t.Fatalf("got %q", got)
	}
}

func TestNextDefaultNameSmallestMissing(t *testing.T) {
	if got := nextDefaultName(nil); got != "工作区 1" {
		t.Fatalf("got %q", got)
	}
	if got := nextDefaultName([]string{"工作区 1", "工作区 3", "other"}); got != "工作区 2" {
		t.Fatalf("got %q", got)
	}
	if got := nextDefaultName([]string{"工作区 1 extra", "工作区 01"}); got != "工作区 1" {
		t.Fatalf("non-exact names should be ignored, got %q", got)
	}
}

func TestValidateDisplayName(t *testing.T) {
	if _, err := validateDisplayName("  "); err == nil {
		t.Fatal("blank name should fail")
	}
	long := strings.Repeat("字", 65)
	if utf8.RuneCountInString(long) != 65 {
		t.Fatal("setup")
	}
	if _, err := validateDisplayName(long); err == nil {
		t.Fatal("65 runes should fail")
	}
	got, err := validateDisplayName("  标书项目  ")
	if err != nil || got != "标书项目" {
		t.Fatalf("got %q err=%v", got, err)
	}
}

func TestNewWorkspaceIDPrefixAndHex(t *testing.T) {
	id := newWorkspaceID()
	if !strings.HasPrefix(id, "cws_") {
		t.Fatalf("id=%q", id)
	}
	hexPart := strings.TrimPrefix(id, "cws_")
	if len(hexPart) != 32 {
		t.Fatalf("hex len=%d id=%q", len(hexPart), id)
	}
	for _, c := range hexPart {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			t.Fatalf("non lowercase hex in %q", id)
		}
	}
}
