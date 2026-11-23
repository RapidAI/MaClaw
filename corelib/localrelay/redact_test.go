package localrelay

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestRedactSecretCoversTheJSONEncoding(t *testing.T) {
	secret := "p&ass<word!!"
	quoted, err := json.Marshal(secret)
	if err != nil {
		t.Fatal(err)
	}
	escaped := string(quoted[1 : len(quoted)-1])
	got := RedactSecret(`status `+escaped+` still running`, secret)
	if strings.Contains(got, escaped) || strings.Contains(got, secret) {
		t.Fatal(got)
	}
	if RedactSecret("running", "run") != "running" {
		t.Fatal("a short secret ate a word")
	}
	if RedactSecret(`say "run"`, "run") != `say "••••"` {
		t.Fatal("a quoted short secret stayed")
	}
}

func TestFitMCPTextStaysInsideTheRelayLine(t *testing.T) {
	if fitMCPText("ok") != "ok" {
		t.Fatal("short text changed")
	}
	split := strings.Repeat("a", 199999) + "你"
	got := fitMCPText(split)
	if !utf8.ValidString(got) || !strings.HasSuffix(got, "\n…truncated") || strings.Contains(got, "你") {
		t.Fatalf("rune cut len=%d suffix=%q", len(got), tail(got))
	}
	huge := strings.Repeat("<", 200000)
	got = fitMCPText(huge)
	raw, err := json.Marshal(Response{OK: true, Text: got})
	if err != nil || len(raw)+1 > MaxLine || !strings.HasSuffix(got, "\n…truncated") || !strings.Contains(got, "<") {
		t.Fatalf("line=%d err=%v suffix=%q", len(raw)+1, err, tail(got))
	}
	dirty := strings.Repeat("a", 1000) + "\xff" + strings.Repeat("b", 200000)
	got = fitMCPText(dirty)
	raw, err = json.Marshal(Response{OK: true, Text: got})
	if err != nil || len(raw)+1 > MaxLine || !strings.Contains(got, "aaa") || !strings.Contains(got, "bbb") {
		t.Fatalf("bad byte erased the page line=%d err=%v", len(raw)+1, err)
	}
}

func tail(s string) string {
	if len(s) <= 24 {
		return s
	}
	return s[len(s)-24:]
}
