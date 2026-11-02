package toolresult

import (
	"math"
	"strings"
	"testing"
)

func TestModelReadLimitUsesOnePage(t *testing.T) {
	if got := ModelReadLimit(0); got != DefaultReadLimit {
		t.Fatalf("omitted limit = %d, want default page %d", got, DefaultReadLimit)
	}
	if got := ModelReadLimit(1); got != DefaultReadLimit {
		t.Fatalf("limit 1 = %d, want default page %d", got, DefaultReadLimit)
	}
	if got := ModelReadLimit(DefaultReadLimit - 1); got != DefaultReadLimit {
		t.Fatalf("sub-page limit = %d, want default page %d", got, DefaultReadLimit)
	}
	if got := ModelReadLimit(DefaultReadLimit); got != DefaultReadLimit {
		t.Fatalf("default page = %d", got)
	}
	mid := DefaultReadLimit + 100
	if got := ModelReadLimit(mid); got != mid {
		t.Fatalf("in-range limit = %d, want %d", got, mid)
	}
	if got := ModelReadLimit(MaxReadLimit + 1); got != MaxReadLimit {
		t.Fatalf("over-max limit = %d, want %d", got, MaxReadLimit)
	}
}

func TestParseArgsIntAcceptsADecimalString(t *testing.T) {
	args := map[string]interface{}{"offset": "16384.0", "blank": "next"}
	if got := ParseArgsInt(args, "offset", 0); got != 16384 {
		t.Fatalf("decimal offset = %d", got)
	}
	if got := ParseArgsInt(args, "blank", 0); got != 0 {
		t.Fatalf("non-numeric offset = %d", got)
	}
	if !InvalidArgsInt(args, "blank") {
		t.Fatal("the field name is not a byte index")
	}
	if InvalidArgsInt(args, "offset") {
		t.Fatal("a decimal integer is a byte index")
	}
	if InvalidArgsInt(map[string]interface{}{}, "offset") {
		t.Fatal("a missing offset is the first page")
	}
	if !InvalidArgsInt(map[string]interface{}{"offset": math.Inf(1)}, "offset") {
		t.Fatal("a non-finite offset is not a byte index")
	}
	if !InvalidArgsInt(map[string]interface{}{"offset": 16384.9}, "offset") {
		t.Fatal("a fractional offset must not truncate onto a real page")
	}
	if InvalidArgsInt(map[string]interface{}{"offset": 16384.0}, "offset") {
		t.Fatal("a whole float is a byte index")
	}
}

func TestFormatReadResultPutsTheNextActionAfterThePage(t *testing.T) {
	body := strings.Repeat("x", 100)
	got := FormatReadResult(ReadResult{
		ID: "page", Offset: 0, ReturnedBytes: 100, TotalBytes: 250,
		Truncated: true, NextOffset: 100, Content: body,
	})
	contentAt := strings.Index(got, "\n---\n")
	endAt := strings.Index(got, "\n--- end ---\n")
	if contentAt < 0 || endAt < contentAt {
		t.Fatalf("page fences missing: %q", got)
	}
	if got[contentAt+len("\n---\n"):endAt] != body {
		t.Fatalf("page bytes changed: %q", got[contentAt:endAt])
	}
	if strings.LastIndex(got, "hint:") < endAt {
		t.Fatalf("the next offset must follow the closing fence, got %q", got)
	}
	if !strings.Contains(got, "offset=100") || !strings.Contains(got, "Do not send offset=0") {
		t.Fatalf("truncated page must name the next offset and reject the one just read: %q", got)
	}
	done := FormatReadResult(ReadResult{
		ID: "page", Offset: 100, ReturnedBytes: 50, TotalBytes: 150,
		Content: "tail",
	})
	if !strings.HasSuffix(strings.TrimRight(done, "\n"), "offset 100.") && !strings.Contains(done, "This page is complete") {
		t.Fatalf("a finished page must say not to read it again: %q", done)
	}
	if strings.Contains(done, "path:") {
		t.Fatalf("hint exposed a storage path: %q", done)
	}
}
