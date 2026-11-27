package tool

import (
	"strings"
	"testing"
)

func TestSplitTextLinesDropsOnlyTheTrailingSplitSegment(t *testing.T) {
	cases := []struct {
		in   string
		n    int
		last string
	}{
		{in: "", n: 1, last: ""},
		{in: "a", n: 1, last: "a"},
		{in: "a\n", n: 1, last: "a\n"},
		{in: "a\nb", n: 2, last: "b"},
		{in: "a\nb\n", n: 2, last: "b\n"},
		{in: "a\n\n", n: 2, last: "\n"},
		{in: "\n", n: 1, last: "\n"},
	}
	for _, tc := range cases {
		lines := SplitTextLines(tc.in)
		if len(lines) != tc.n || lines[len(lines)-1] != tc.last {
			t.Fatalf("SplitTextLines(%q) = %#v, want %d lines ending %q", tc.in, lines, tc.n, tc.last)
		}
	}
}

func TestFormatNumberedReadPageHeaderAndNumbering(t *testing.T) {
	lines := SplitTextLines("alpha\nbeta\n")
	got := FormatNumberedReadPage("lines.txt", lines, 0, 2000)
	want := "Read file lines.txt, lines 1-2\n1: alpha\n2: beta"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestFormatNumberedReadPageEmptyFile(t *testing.T) {
	if got := FormatNumberedReadPage("e.txt", SplitTextLines(""), 0, 2000); got != "Read file e.txt, 0 lines" {
		t.Fatalf("got %q", got)
	}
}

func TestFormatNumberedReadPageContinuationUsesOffset(t *testing.T) {
	lines := SplitTextLines("a\nb\nc\n")
	got := FormatNumberedReadPage("f.txt", lines, 0, 2)
	want := "Read file f.txt, lines 1-2\n1: a\n2: b\n[Output truncated. Continue reading with offset: 3]"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestFormatNumberedReadPageClampsLongLines(t *testing.T) {
	long := strings.Repeat("x", 3000)
	lines := SplitTextLines(long + "\n")
	got := FormatNumberedReadPage("f.txt", lines, 0, 2000)
	if !strings.Contains(got, "... (line truncated to 2000 chars)") {
		t.Fatalf("missing clamp suffix: %.120q", got)
	}
	row := strings.Split(got, "\n")[1]
	if len(row) > 2100 {
		t.Fatalf("row not clamped: len=%d", len(row))
	}
}

func TestFormatNumberedReadPageByteCap(t *testing.T) {
	var sb strings.Builder
	for i := 0; i < 40; i++ {
		sb.WriteString(strings.Repeat("y", 2000) + "\n")
	}
	lines := SplitTextLines(sb.String())
	got := FormatNumberedReadPage("f.txt", lines, 0, 2000)
	if !strings.Contains(got, "[Output truncated. Continue reading with offset:") {
		t.Fatalf("byte cap must produce continuation: %.120q", got)
	}
}

func TestFormatNumberedReadPageCRLFViewIsLF(t *testing.T) {
	lines := SplitTextLines("one\r\ntwo\r\n")
	got := FormatNumberedReadPage("f.txt", lines, 0, 2000)
	if strings.Contains(got, "\r") {
		t.Fatalf("CR leaked into LF view: %q", got)
	}
	if !strings.Contains(got, "1: one\n2: two") {
		t.Fatalf("unexpected body %q", got)
	}
}

func TestFormatNumberedReadDir(t *testing.T) {
	got := FormatNumberedReadDir("d", []string{"sub/", "a.txt"}, 1, 2000)
	want := "Read directory d, entries 1-2\nsub/\na.txt"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	if empty := FormatNumberedReadDir("d", nil, 1, 2000); empty != "Read directory d, 0 entries" {
		t.Fatalf("got %q", empty)
	}
	many := []string{"a", "b", "c", "d", "e"}
	truncated := FormatNumberedReadDir("d", many, 1, 3)
	if !strings.Contains(truncated, "[Output truncated. Continue reading with offset: 4]") {
		t.Fatalf("got %q", truncated)
	}
	deeper := FormatNumberedReadDir("d", many, 4, 3)
	if !strings.Contains(deeper, "entries 4-5") || strings.Contains(deeper, "a\n") {
		t.Fatalf("paged directory read = %q", deeper)
	}
	if strings.Contains(deeper, "[Output truncated") {
		t.Fatalf("last page must not continue: %q", deeper)
	}
	pastEnd := FormatNumberedReadDir("d", many, 99, 3)
	if !strings.Contains(pastEnd, "past the end") {
		t.Fatalf("past-end directory read = %q", pastEnd)
	}
}

func TestFormatNumberedReadPagePastEndReturnsTailWithNote(t *testing.T) {
	lines := SplitTextLines("a\nb\nc\n")
	got := FormatNumberedReadPage("f.txt", lines, 10, 2000)
	if !strings.Contains(got, "(offset 11 is past the end; the file has 3 lines)") {
		t.Fatalf("missing past-end note: %q", got)
	}
	if !strings.Contains(got, "lines 1-3") || !strings.Contains(got, "3: c") {
		t.Fatalf("past-end must return the tail page: %q", got)
	}
	if strings.Contains(got, "[Output truncated") {
		t.Fatalf("tail page reaching EOF must not continue: %q", got)
	}
}
