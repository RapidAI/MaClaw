package main

import (
	"strings"
	"testing"
)

func TestRepetitionFilter_MarkdownTableRepetition(t *testing.T) {
	var out strings.Builder
	f := newRepetitionFilter(func(s string) { out.WriteString(s) })

	tableBlock := "| \xe5\x91\xbd\xe4\xbb\xa4 | \xe8\xaf\xb4\xe6\x98\x8e | \xe7\xa4\xba\xe4\xbe\x8b |\n" +
		"|------|------|------|\n" +
		"| search | search_notes keyword |\n" +
		"| note | get_note_detail id |\n" +
		"| comments | manage_comments note_id |\n" +
		"| download | download_note id |\n"

	for i := 0; i < 3; i++ {
		f.Write(tableBlock)
		if i < 2 {
			f.Write("\n\n")
		}
		if f.Halted() {
			break
		}
	}
	f.Flush()

	if !f.Halted() {
		t.Fatal("expected halt for repeated Markdown table blocks")
	}
}

func TestRepetitionFilter_ParagraphRepetitionWithDifferentContent(t *testing.T) {
	var out strings.Builder
	f := newRepetitionFilter(func(s string) { out.WriteString(s) })

	f.Write("This is the first paragraph with enough content to exceed the minimum length threshold for detection.\n\n")
	f.Write("This is the second completely different paragraph also with enough content to exceed the minimum.\n\n")
	f.Write("Third paragraph is again different content ensuring no repetition detection triggers here.\n\n")
	f.Flush()

	if f.Halted() {
		t.Fatal("expected no halt for different paragraphs")
	}
}

func TestRepetitionFilter_SingleParagraphRepetition(t *testing.T) {
	var out strings.Builder
	f := newRepetitionFilter(func(s string) { out.WriteString(s) })

	para := "This is a long paragraph with enough content to exceed the minimum paragraph length threshold for testing paragraph-level repetition detection"
	for i := 0; i < 3; i++ {
		f.Write(para)
		if i < 2 {
			f.Write("\n\n")
		}
		if f.Halted() {
			break
		}
	}
	f.Flush()

	if !f.Halted() {
		t.Fatal("expected halt for single paragraph repetition")
	}
}

func TestRepetitionFilter_TwoParagraphBlockRepetition(t *testing.T) {
	var out strings.Builder
	f := newRepetitionFilter(func(s string) { out.WriteString(s) })

	paraA := "First paragraph contains enough content to exceed the minimum paragraph length threshold for two-paragraph block repetition testing"
	paraB := "Second paragraph also contains enough content to exceed the minimum paragraph length threshold paired with the first paragraph"

	for rep := 0; rep < 3; rep++ {
		f.Write(paraA + "\n\n" + paraB)
		if rep < 2 {
			f.Write("\n\n")
		}
		if f.Halted() {
			break
		}
	}
	f.Flush()

	if !f.Halted() {
		t.Fatal("expected halt for two-paragraph block repetition")
	}
}

func TestRepetitionFilter_ShortParagraphsNotFiltered(t *testing.T) {
	var out strings.Builder
	f := newRepetitionFilter(func(s string) { out.WriteString(s) })

	for i := 0; i < 5; i++ {
		f.Write("short para")
		if i < 4 {
			f.Write("\n\n")
		}
	}
	f.Flush()

	if f.Halted() {
		t.Fatal("expected no halt for short repeated paragraphs")
	}
}

func TestRepetitionFilter_TableWithBrowserPrefix(t *testing.T) {
	var out strings.Builder
	f := newRepetitionFilter(func(s string) { out.WriteString(s) })

	tableBlock := "| action | description |\n" +
		"|------|------|\n" +
		"| search | search notes by keyword |\n" +
		"| detail | get note detail by id |\n" +
		"| publish | publish new note |"

	f.Write(tableBlock + "\n\n")
	f.Write(tableBlock + "\n\n")
	f.Write("Browser: please login to the website first and then use the following commands")
	f.Flush()

	if !f.Halted() {
		t.Fatal("expected halt for repeated table blocks before Browser: prefix")
	}
	if strings.Contains(out.String(), "Browser:") {
		t.Fatal("Browser: prefix should have been suppressed after halt")
	}
}

func TestRepetitionFilter_TableWithWarningRepetition(t *testing.T) {
	var out strings.Builder
	f := newRepetitionFilter(func(s string) { out.WriteString(s) })

	block := "| cmd | desc |\n" +
		"|------|------|\n" +
		"| search | search notes |\n" +
		"\n" +
		"Warning: most operations require login credentials"

	for i := 0; i < 3; i++ {
		f.Write(block)
		if i < 2 {
			f.Write("\n\n")
		}
		if f.Halted() {
			break
		}
	}
	f.Flush()

	if !f.Halted() {
		t.Fatal("expected halt for repeated table+warning blocks")
	}
}

func TestFindParagraphBreak(t *testing.T) {
	tests := []struct {
		input    string
		expected int
	}{
		{"hello\n\nworld", 5},
		{"hello\n  \nworld", 5},
		{"hello\nworld", -1},
		{"no breaks here", -1},
		{"first\n\nsecond\n\nthird", 5},
		{"\n\nstart", 0},
	}
	for _, tt := range tests {
		got := findParagraphBreak(tt.input)
		if got != tt.expected {
			t.Errorf("findParagraphBreak(%q) = %d, want %d", tt.input, got, tt.expected)
		}
	}
}

func TestDetectRepetition_Paragraphs_PatternLength1(t *testing.T) {
	para := "This is a long enough paragraph to exceed the minimum paragraph length threshold for detection"
	window := []string{para, para}
	if !detectRepetition(window, repMaxParagraphPatternLen) {
		t.Fatal("expected paragraph repetition detected for pattern length 1")
	}
}

func TestDetectRepetition_Paragraphs_NoRepetition(t *testing.T) {
	window := []string{
		"First paragraph with enough content to exceed the minimum paragraph length threshold",
		"Second paragraph also with enough content to exceed the minimum paragraph length threshold",
		"Third paragraph again with enough content to exceed the minimum paragraph length threshold",
	}
	if detectRepetition(window, repMaxParagraphPatternLen) {
		t.Fatal("expected no paragraph repetition for different paragraphs")
	}
}
