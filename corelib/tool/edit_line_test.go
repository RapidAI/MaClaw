package tool

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sampleFile = "line1\nline2\nline3\nline4\nline5\n"

func writeSample(t *testing.T, dir string) string {
	t.Helper()
	f := filepath.Join(dir, "test.txt")
	if err := os.WriteFile(f, []byte(sampleFile), 0644); err != nil {
		t.Fatal(err)
	}
	return f
}

func readLines(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(string(data), "\n")
}

func TestEditFileByLine_Replace_SingleLine(t *testing.T) {
	f := writeSample(t, t.TempDir())
	res, err := EditFileByLine(f, EditLineReplace, 3, 3, "replaced3")
	if err != nil {
		t.Fatal(err)
	}
	if res.LinesChanged != 1 {
		t.Errorf("LinesChanged=%d, want 1", res.LinesChanged)
	}
	lines := readLines(t, f)
	if lines[2] != "replaced3" {
		t.Errorf("line 3 = %q, want %q", lines[2], "replaced3")
	}
	// Other lines untouched.
	if lines[0] != "line1" || lines[1] != "line2" || lines[3] != "line4" {
		t.Errorf("other lines modified: %v", lines)
	}
}

func TestEditFileByLine_Replace_MultiLine(t *testing.T) {
	f := writeSample(t, t.TempDir())
	// Replace lines 2-4 with two new lines.
	res, err := EditFileByLine(f, EditLineReplace, 2, 4, "new2\nnew3")
	if err != nil {
		t.Fatal(err)
	}
	if res.LinesChanged != 3 {
		t.Errorf("LinesChanged=%d, want 3", res.LinesChanged)
	}
	lines := readLines(t, f)
	// Original: line1, line2, line3, line4, line5, ""
	// After:    line1, new2, new3, line5, ""
	if lines[0] != "line1" || lines[1] != "new2" || lines[2] != "new3" || lines[3] != "line5" {
		t.Errorf("unexpected content: %v", lines)
	}
}

func TestEditFileByLine_Insert_Middle(t *testing.T) {
	f := writeSample(t, t.TempDir())
	// Insert after line 2.
	res, err := EditFileByLine(f, EditLineInsert, 2, 0, "inserted_a\ninserted_b")
	if err != nil {
		t.Fatal(err)
	}
	if res.LinesChanged != 2 {
		t.Errorf("LinesChanged=%d, want 2", res.LinesChanged)
	}
	lines := readLines(t, f)
	// line1, line2, inserted_a, inserted_b, line3, line4, line5, ""
	if lines[2] != "inserted_a" || lines[3] != "inserted_b" || lines[4] != "line3" {
		t.Errorf("unexpected content: %v", lines)
	}
}

func TestEditFileByLine_Insert_AtTop(t *testing.T) {
	f := writeSample(t, t.TempDir())
	// Insert at the very beginning (startLine=0).
	_, err := EditFileByLine(f, EditLineInsert, 0, 0, "header")
	if err != nil {
		t.Fatal(err)
	}
	lines := readLines(t, f)
	if lines[0] != "header" || lines[1] != "line1" {
		t.Errorf("unexpected content: %v", lines)
	}
}

func TestEditFileByLine_Insert_AtEnd(t *testing.T) {
	f := writeSample(t, t.TempDir())
	lines0 := readLines(t, f)
	total := len(lines0)
	// Insert after the last line (including the trailing empty line from \n).
	_, err := EditFileByLine(f, EditLineInsert, total, 0, "footer")
	if err != nil {
		t.Fatal(err)
	}
	lines := readLines(t, f)
	// "footer" should be present somewhere near the end.
	found := false
	for _, l := range lines {
		if l == "footer" {
			found = true
		}
	}
	if !found {
		t.Errorf("footer not found in file: %v", lines)
	}
}

func TestEditFileByLine_Delete(t *testing.T) {
	f := writeSample(t, t.TempDir())
	res, err := EditFileByLine(f, EditLineDelete, 2, 4, "")
	if err != nil {
		t.Fatal(err)
	}
	if res.LinesChanged != 3 {
		t.Errorf("LinesChanged=%d, want 3", res.LinesChanged)
	}
	lines := readLines(t, f)
	// line1, line5, ""
	if lines[0] != "line1" || lines[1] != "line5" {
		t.Errorf("unexpected content: %v", lines)
	}
}

func TestEditFileByLine_OutOfBounds(t *testing.T) {
	f := writeSample(t, t.TempDir())

	_, err := EditFileByLine(f, EditLineReplace, 0, 1, "x")
	if err == nil {
		t.Error("start_line=0 should fail for replace")
	}

	_, err = EditFileByLine(f, EditLineReplace, 1, 100, "x")
	if err == nil {
		t.Error("end_line=100 should fail")
	}

	_, err = EditFileByLine(f, EditLineReplace, 3, 2, "x")
	if err == nil {
		t.Error("end_line < start_line should fail")
	}

	_, err = EditFileByLine(f, EditLineDelete, 99, 99, "")
	if err == nil {
		t.Error("line 99 should be out of bounds")
	}
}

func TestEditFileByLine_RealWorldPatch(t *testing.T) {
	// Simulate a real SubAgent scenario: read a Go file, find the line to
	// change, then use line-level edit to patch it.
	dir := t.TempDir()
	f := filepath.Join(dir, "main.go")
	content := `package main

import "fmt"

func main() {
	fmt.Println("old message")
	doWork()
}

func doWork() {
	// TODO: implement
}
`
	os.WriteFile(f, []byte(content), 0644)

	// The SubAgent would: read_file → find line 6 has the old message → edit_lines to replace it.
	res, err := EditFileByLine(f, EditLineReplace, 6, 6, "\tfmt.Println(\"new message\")")
	if err != nil {
		t.Fatal(err)
	}
	if res.LinesChanged != 1 {
		t.Errorf("LinesChanged=%d, want 1", res.LinesChanged)
	}

	data, _ := os.ReadFile(f)
	if !strings.Contains(string(data), "new message") {
		t.Error("patch not applied")
	}
	if strings.Contains(string(data), "old message") {
		t.Error("old content still present")
	}
	// Rest of file should be intact.
	if !strings.Contains(string(data), "doWork()") {
		t.Error("other content was lost")
	}
}


func TestEditFileByLine_CRLF_Preserved(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "crlf.txt")
	// Write a file with \r\n line endings (Windows style).
	os.WriteFile(f, []byte("line1\r\nline2\r\nline3\r\n"), 0644)

	res, err := EditFileByLine(f, EditLineReplace, 2, 2, "replaced2")
	if err != nil {
		t.Fatal(err)
	}
	if res.LinesChanged != 1 {
		t.Errorf("LinesChanged=%d, want 1", res.LinesChanged)
	}

	data, _ := os.ReadFile(f)
	content := string(data)

	// The file should still use \r\n line endings.
	if !strings.Contains(content, "line1\r\n") {
		t.Error("line1 should still have \\r\\n ending")
	}
	if !strings.Contains(content, "replaced2\r\n") {
		t.Error("replaced line should have \\r\\n ending")
	}
	if !strings.Contains(content, "line3\r\n") {
		t.Error("line3 should still have \\r\\n ending")
	}
	// Should NOT have mixed line endings.
	lfOnly := strings.Count(content, "\n") - strings.Count(content, "\r\n")
	if lfOnly > 0 {
		t.Errorf("found %d bare \\n (mixed line endings)", lfOnly)
	}
}

func TestEditFileByLine_LF_Preserved(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "lf.txt")
	// Write a file with \n line endings (Unix style).
	os.WriteFile(f, []byte("line1\nline2\nline3\n"), 0644)

	_, err := EditFileByLine(f, EditLineReplace, 2, 2, "replaced2")
	if err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(f)
	content := string(data)

	// Should NOT introduce \r\n.
	if strings.Contains(content, "\r\n") {
		t.Error("Unix file should not get \\r\\n after editing")
	}
	if !strings.Contains(content, "replaced2\n") {
		t.Error("replaced line should have \\n ending")
	}
}


func TestEditFileByLine_CRLF_InNewContent(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "crlf2.txt")
	// Original file uses \r\n.
	os.WriteFile(f, []byte("aaa\r\nbbb\r\nccc\r\n"), 0644)

	// newContent contains \r\n (LLM on Windows might generate this).
	// The function should normalize it to avoid double \r.
	_, err := EditFileByLine(f, EditLineReplace, 2, 2, "BBB\r\nBBB2")
	if err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(f)
	content := string(data)

	// Should NOT have \r\r\n (double carriage return).
	if strings.Contains(content, "\r\r") {
		t.Errorf("double \\r detected in output: %q", content)
	}
	// Should have consistent \r\n line endings.
	if !strings.Contains(content, "BBB\r\n") {
		t.Errorf("expected BBB with \\r\\n, got: %q", content)
	}
}
