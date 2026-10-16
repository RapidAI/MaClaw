package tool

import (
	"strings"
	"testing"
)

func TestReplaceTextPassageCRLFMatchesLFPassage(t *testing.T) {
	file := "func f() {\r\n\tpm.Run()\r\n\tcr.Stdout = pr.Stdout\r\n}\r\n"
	old := "func f() {\n\tpm.Run()\n\tcr.Stdout = pr.Stdout\n}"
	newText := "func f() {\n\tpr, err := pm.Run()\n\tif err != nil {\n\t\treturn err\n\t}\n\tcr.Stdout = pr.Stdout\n}"
	got, err := ReplaceTextPassage(file, old, newText, false)
	if err != nil {
		t.Fatal(err)
	}
	if got.Matches != 1 || got.Applied != 1 {
		t.Fatalf("matches=%d applied=%d", got.Matches, got.Applied)
	}
	want := "func f() {\r\n\tpr, err := pm.Run()\r\n\tif err != nil {\r\n\t\treturn err\r\n\t}\r\n\tcr.Stdout = pr.Stdout\r\n}\r\n"
	if got.Text != want {
		t.Fatalf("got %q\nwant %q", got.Text, want)
	}
}

func TestReplaceTextPassagePreservesBytesOutsideTheMatch(t *testing.T) {
	file := "keep\r\nTARGET\r\nkeep\n"
	got, err := ReplaceTextPassage(file, "TARGET", "NEXT", false)
	if err != nil {
		t.Fatal(err)
	}
	if got.Text != "keep\r\nNEXT\r\nkeep\n" {
		t.Fatalf("outside bytes changed: %q", got.Text)
	}
}

func TestReplaceTextPassageEncodesInsertedNewlinesWithTheFileConvention(t *testing.T) {
	file := "before\r\nname\r\nafter\r\n"
	got, err := ReplaceTextPassage(file, "name", "one\ntwo", false)
	if err != nil {
		t.Fatal(err)
	}
	if got.Text != "before\r\none\r\ntwo\r\nafter\r\n" {
		t.Fatalf("inserted LF into a CRLF file: %q", got.Text)
	}
}

func TestReplaceTextPassageReplaceAllCountsLogicalMatches(t *testing.T) {
	file := "a\r\nb\r\na\r\nb"
	got, err := ReplaceTextPassage(file, "a\nb", "z", true)
	if err != nil {
		t.Fatal(err)
	}
	if got.Matches != 2 || got.Applied != 2 || got.Text != "z\r\nz" {
		t.Fatalf("got %+v", got)
	}
	first, err := ReplaceTextPassage(file, "a\nb", "z", false)
	if err != nil {
		t.Fatal(err)
	}
	if first.Matches != 2 || first.Applied != 1 || first.Text != "z\r\na\r\nb" {
		t.Fatalf("first-only got %+v", first)
	}
}

func TestReplaceTextPassageNotFoundAndEmpty(t *testing.T) {
	if _, err := ReplaceTextPassage("abc", "zzz", "x", false); err != ErrTextPassageNotFound {
		t.Fatalf("err=%v", err)
	}
	if _, err := ReplaceTextPassage("abc", "", "x", false); err != ErrTextPassageEmpty {
		t.Fatalf("err=%v", err)
	}
	// A passage that contains a line break cannot occur in a file that has none.
	if _, err := ReplaceTextPassage("abc", "a\nb", "z", false); err != ErrTextPassageNotFound {
		t.Fatalf("err=%v", err)
	}
}

func TestReplaceTextPassageNoBreakFileKeepsASingleLineEdit(t *testing.T) {
	got, err := ReplaceTextPassage("hello world", "hello", "hi", false)
	if err != nil {
		t.Fatal(err)
	}
	if got.Matches != 1 || got.Text != "hi world" {
		t.Fatalf("got %+v", got)
	}
	inserted, err := ReplaceTextPassage("hello", "hello", "a\r\nb", false)
	if err != nil {
		t.Fatal(err)
	}
	if inserted.Text != "a\nb" {
		t.Fatalf("inserted %q", inserted.Text)
	}
}

func TestReplaceTextPassageReplaceAllKeepsEachSpansBreak(t *testing.T) {
	file := "a\nbX\ra\r\nb"
	got, err := ReplaceTextPassage(file, "a\nb", "x\ny", true)
	if err != nil {
		t.Fatal(err)
	}
	if got.Matches != 2 || got.Applied != 2 || got.Text != "x\nyX\rx\r\ny" {
		t.Fatalf("got %+v", got)
	}
	first, err := ReplaceTextPassage(file, "a\nb", "x\ny", false)
	if err != nil {
		t.Fatal(err)
	}
	if first.Matches != 2 || first.Applied != 1 || first.Text != "x\nyX\ra\r\nb" {
		t.Fatalf("first-only got %+v", first)
	}
}

func TestReplaceTextPassageCROnlyRoundTrip(t *testing.T) {
	file := "func f() {\rpm.Run()\r}\r"
	got, err := ReplaceTextPassage(file, "func f() {\npm.Run()\n}", "func f() {\n\tok()\n}", false)
	if err != nil {
		t.Fatal(err)
	}
	want := "func f() {\r\tok()\r}\r"
	if got.Text != want {
		t.Fatalf("got %q want %q", got.Text, want)
	}
}

func TestReplaceTextPassageReplaceAllWritesLeftToRight(t *testing.T) {
	file := strings.Repeat("a\r\n", 8) + "tail"
	got, err := ReplaceTextPassage(file, "a\n", "b", true)
	if err != nil {
		t.Fatal(err)
	}
	if got.Matches != 8 || got.Applied != 8 || got.Text != strings.Repeat("b", 8)+"tail" {
		t.Fatalf("got %+v", got)
	}
}

func TestReplaceTextPassageMixedSpanKeepsItsOwnBreak(t *testing.T) {
	// The match starts on CR. The CRLF and LF outside it stay put.
	file := "a\rb\r\nkeep\n"
	got, err := ReplaceTextPassage(file, "a\nb", "x\ny", false)
	if err != nil {
		t.Fatal(err)
	}
	if got.Text != "x\ry\r\nkeep\n" {
		t.Fatalf("got %q", got.Text)
	}
	// The match starts on CRLF. A later lone LF stays a lone LF.
	file = "a\r\nb\nkeep\n"
	got, err = ReplaceTextPassage(file, "a\nb", "x\ny", false)
	if err != nil {
		t.Fatal(err)
	}
	if got.Text != "x\r\ny\nkeep\n" {
		t.Fatalf("got %q", got.Text)
	}
}

func TestReplaceTextPassageCRFileInsertsCR(t *testing.T) {
	file := "before\rname\rafter\r"
	got, err := ReplaceTextPassage(file, "name", "one\ntwo", false)
	if err != nil {
		t.Fatal(err)
	}
	if got.Text != "before\rone\rtwo\rafter\r" {
		t.Fatalf("got %q", got.Text)
	}
	quoted, err := ReplaceTextPassage("a\rb\r", "a\r\nb", "z", false)
	if err != nil {
		t.Fatal(err)
	}
	if quoted.Matches != 1 || quoted.Text != "z\r" {
		t.Fatalf("quoted got %+v", quoted)
	}
}

func TestReplaceTextPassageLFFileMatchesCRLFQuotedPassage(t *testing.T) {
	file := "func f() {\n\tpm.Run()\n}\n"
	old := "func f() {\r\n\tpm.Run()\r\n}"
	got, err := ReplaceTextPassage(file, old, "func f() {\r\n\tok()\r\n}", false)
	if err != nil {
		t.Fatal(err)
	}
	want := "func f() {\n\tok()\n}\n"
	if got.Matches != 1 || got.Applied != 1 || got.Text != want {
		t.Fatalf("got %+v want %q", got, want)
	}
	// A CR-quoted line break in an LF file is the same break, including
	// when it occurs more than once.
	repeated, err := ReplaceTextPassage("a\na\n", "a\r\n", "z", false)
	if err != nil {
		t.Fatal(err)
	}
	if repeated.Matches != 2 || repeated.Applied != 1 || repeated.Text != "za\n" {
		t.Fatalf("repeated got %+v", repeated)
	}
}

func TestReplaceTextPassageLFFileStaysLF(t *testing.T) {
	file := "func b() {\n\treturn nil\n}\n"
	got, err := ReplaceTextPassage(file, "func b() {\n\treturn nil\n}", "func b() {\n\treturn fmt.Errorf(\"b\")\n}", false)
	if err != nil {
		t.Fatal(err)
	}
	want := "func b() {\n\treturn fmt.Errorf(\"b\")\n}\n"
	if got.Text != want {
		t.Fatalf("got %q", got.Text)
	}
}
