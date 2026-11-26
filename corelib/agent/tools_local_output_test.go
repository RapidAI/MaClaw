package agent

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestToolBashOutputTruncation(t *testing.T) {
	command := "printf 'x%.0s' {1..8193}; printf 'y%.0s' {1..4097} >&2"
	if runtime.GOOS == "windows" {
		command = "[Console]::Out.Write(('x' * 8193) -join ''); [Console]::Error.Write(('y' * 4097) -join '')"
	}

	got := ToolBash(map[string]interface{}{"command": command}, nil)
	want := strings.Repeat("x", 8192) + "\n... (输出已截断)" +
		"\n[stderr] " + strings.Repeat("y", 4096) + "\n... (错误输出已截断)"
	if got != want {
		t.Fatalf("truncated output mismatch: got length %d, want %d", len(got), len(want))
	}
}

func TestToolReadFileOffsetReadsTailLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tail.txt")
	content := "one\ntwo\nthree\nfour\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	got := ToolReadFile(map[string]interface{}{"path": path, "offset": float64(2)})
	if !strings.Contains(got, "最后 2 行，共 4 行") || !strings.Contains(got, "three\nfour\n") || strings.Contains(got, "one\n") {
		t.Fatalf("tail read = %q", got)
	}
	got = ToolReadFile(map[string]interface{}{"path": path, "offset": float64(99)})
	if got != content {
		t.Fatalf("oversized tail should return the whole file: %q", got)
	}
}

func TestToolReadFilePreciseRangeIncludesLineNumbers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "range.txt")
	if err := os.WriteFile(path, []byte("one\ntwo\nthree\nfour\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := ToolReadFile(map[string]interface{}{"path": path, "start_line": float64(2), "lines": float64(2)})
	if !strings.Contains(got, "   2 | two\n") || !strings.Contains(got, "   3 | three\n") {
		t.Fatalf("precise read lacks line numbers: %q", got)
	}
	if strings.Contains(got, "one\n") || strings.Contains(got, "four\n") {
		t.Fatalf("precise read escaped requested range: %q", got)
	}
}

func TestToolEditFileRequireUniqueRejectsAmbiguousMatch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "duplicate.txt")
	original := "alpha target omega target" + string(rune(10))
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	got := ToolEditFile(map[string]interface{}{
		"path":           path,
		"old_string":     "target",
		"new_string":     "replacement",
		"require_unique": true,
	})
	if !strings.Contains(got, "必须唯一") {
		t.Fatalf("ambiguous unique edit result = %q", got)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != original {
		t.Fatalf("ambiguous unique edit mutated file: %q", string(body))
	}
	got = ToolEditFile(map[string]interface{}{
		"path":           path,
		"old_string":     "target",
		"new_string":     "replacement",
		"replace_all":    true,
		"require_unique": true,
	})
	if !strings.Contains(got, "替换 2 处") {
		t.Fatalf("replace_all unique edit result = %q", got)
	}
}

func TestToolReadFileLineNumbersModeNumbersWholeFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "numbered.txt")
	if err := os.WriteFile(path, []byte("one"+string(rune(10))+"two"+string(rune(10))), 0o644); err != nil {
		t.Fatal(err)
	}
	got := ToolReadFile(map[string]interface{}{"path": path, "line_numbers": true})
	if !strings.Contains(got, "1: one") || !strings.Contains(got, "2: two") {
		t.Fatalf("numbered whole-file read = %q", got)
	}
}

func TestToolReadFileLineNumbersModeShowsLFViewForCRLF(t *testing.T) {
	path := filepath.Join(t.TempDir(), "crlf.txt")
	lineEnding := string(rune(13)) + string(rune(10))
	if err := os.WriteFile(path, []byte("one"+lineEnding+"two"+lineEnding), 0o644); err != nil {
		t.Fatal(err)
	}
	got := ToolReadFile(map[string]interface{}{"path": path, "line_numbers": true})
	want := "1: one" + string(rune(10)) + "2: two"
	if strings.Contains(got, string(rune(13))) || !strings.Contains(got, want) {
		t.Fatalf("numbered CRLF read = %q", got)
	}
	if !strings.HasPrefix(got, "Read file ") || !strings.Contains(got, ", lines 1-2") {
		t.Fatalf("numbered read missing official header = %q", got)
	}
}

func TestToolReadFileLineNumbersModeEmptyFileHasNoFakeLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.txt")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	got := ToolReadFile(map[string]interface{}{"path": path, "line_numbers": true})
	if !strings.HasPrefix(got, "Read file ") || !strings.HasSuffix(got, ", 0 lines") {
		t.Fatalf("numbered empty read = %q", got)
	}
}

func TestToolReadFileLineNumbersModeContinuationUsesOfficialOffset(t *testing.T) {
	path := filepath.Join(t.TempDir(), "paged.txt")
	var b strings.Builder
	for i := 1; i <= 3; i++ {
		b.WriteString("line")
		b.WriteString(string(rune(10)))
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	got := ToolReadFile(map[string]interface{}{"path": path, "lines": float64(2), "line_numbers": true})
	if !strings.Contains(got, "[Output truncated. Continue reading with offset: 3]") || strings.Contains(got, "start_line=") {
		t.Fatalf("official continuation read = %q", got)
	}
}

func TestToolReadFileLineNumbersModeReadsDirectoryListing(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	got := ToolReadFile(map[string]interface{}{"path": dir, "line_numbers": true})
	if !strings.HasPrefix(got, "Read directory ") || !strings.Contains(got, "entries 1-2") {
		t.Fatalf("directory read header = %q", got)
	}
	sub := strings.Index(got, "sub/")
	file := strings.Index(got, "a.txt")
	if sub < 0 || file < 0 || sub > file {
		t.Fatalf("directory entries must sort directories first = %q", got)
	}
	plain := ToolReadFile(map[string]interface{}{"path": dir})
	if strings.HasPrefix(plain, "Read directory ") {
		t.Fatalf("non-numbered directory read must keep the legacy error = %q", plain)
	}
}

func TestToolReadFileLineNumbersModePastEndReturnsTail(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tail.txt")
	if err := os.WriteFile(path, []byte("a\nb\nc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := ToolReadFile(map[string]interface{}{
		"path":         path,
		"start_line":   float64(99),
		"line_numbers": true,
	})
	if strings.Contains(got, "超出文件总行数") {
		t.Fatalf("past-end must not error in official mode: %q", got)
	}
	if !strings.Contains(got, "past the end") || !strings.Contains(got, "3: c") {
		t.Fatalf("past-end read = %q", got)
	}
}
