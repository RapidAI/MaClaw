package tool

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestEditTextFile_E2E_SubAgentScenarios validates all edit_file scenarios
// that the CodingSubAgent relies on: single-line patch, multi-line patch,
// deletion, Chinese content, whitespace sensitivity, and context-based
// disambiguation of duplicate patterns.
func TestEditTextFile_E2E_SubAgentScenarios(t *testing.T) {
	dir := t.TempDir()

	original := "package main\n\nimport \"fmt\"\n\nfunc hello() {\n\tfmt.Println(\"Hello, World!\")\n}\n\nfunc add(a, b int) int {\n\treturn a + b\n}\n\n// TODO: implement subtract\nfunc subtract(a, b int) int {\n\treturn 0 // placeholder\n}\n"
	testFile := filepath.Join(dir, "sample.go")
	os.WriteFile(testFile, []byte(original), 0644)

	t.Run("single_line_patch", func(t *testing.T) {
		res, err := EditTextFile(testFile, `fmt.Println("Hello, World!")`, `fmt.Println("Hello, MaClaw!")`, false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.Count != 1 {
			t.Errorf("expected 1 replacement, got %d", res.Count)
		}
		data, _ := os.ReadFile(testFile)
		if !strings.Contains(string(data), "Hello, MaClaw!") {
			t.Error("content not updated")
		}
	})

	t.Run("multiline_patch", func(t *testing.T) {
		res, err := EditTextFile(testFile,
			"// TODO: implement subtract\nfunc subtract(a, b int) int {\n\treturn 0 // placeholder\n}",
			"// subtract returns a - b\nfunc subtract(a, b int) int {\n\treturn a - b\n}",
			false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.Count != 1 {
			t.Errorf("expected 1 replacement, got %d", res.Count)
		}
		data, _ := os.ReadFile(testFile)
		if !strings.Contains(string(data), "return a - b") {
			t.Error("function body not updated")
		}
	})

	t.Run("not_found_error", func(t *testing.T) {
		_, err := EditTextFile(testFile, "nonexistent text", "replacement", false)
		if err == nil || !strings.Contains(err.Error(), "未找到") {
			t.Errorf("expected not-found error, got: %v", err)
		}
	})

	t.Run("empty_old_string_rejected", func(t *testing.T) {
		_, err := EditTextFile(testFile, "", "replacement", false)
		if err == nil {
			t.Error("should reject empty old_string")
		}
	})

	t.Run("delete_via_empty_new_string", func(t *testing.T) {
		res, err := EditTextFile(testFile, "// subtract returns a - b\n", "", false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.Count != 1 {
			t.Errorf("expected 1 deletion, got %d", res.Count)
		}
		data, _ := os.ReadFile(testFile)
		if strings.Contains(string(data), "// subtract returns") {
			t.Error("comment should have been deleted")
		}
	})

	t.Run("replace_all", func(t *testing.T) {
		f := filepath.Join(dir, "multi.txt")
		os.WriteFile(f, []byte("foo bar foo baz foo"), 0644)
		res, err := EditTextFile(f, "foo", "qux", true)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.Count != 3 {
			t.Errorf("expected 3 replacements, got %d", res.Count)
		}
		data, _ := os.ReadFile(f)
		if string(data) != "qux bar qux baz qux" {
			t.Errorf("got %q", string(data))
		}
	})

	t.Run("first_only_when_not_replace_all", func(t *testing.T) {
		f := filepath.Join(dir, "multi2.txt")
		os.WriteFile(f, []byte("foo bar foo baz foo"), 0644)
		res, err := EditTextFile(f, "foo", "qux", false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.Count != 1 {
			t.Errorf("expected 1 replacement, got %d", res.Count)
		}
		data, _ := os.ReadFile(f)
		if string(data) != "qux bar foo baz foo" {
			t.Errorf("got %q", string(data))
		}
	})

	t.Run("chinese_utf8", func(t *testing.T) {
		f := filepath.Join(dir, "cn.txt")
		os.WriteFile(f, []byte("你好世界\n这是测试\n再见"), 0644)
		res, err := EditTextFile(f, "这是测试", "这是修改后的测试", false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.Count != 1 {
			t.Errorf("expected 1, got %d", res.Count)
		}
		data, _ := os.ReadFile(f)
		if !strings.Contains(string(data), "修改后的测试") {
			t.Error("Chinese content not updated")
		}
	})

	t.Run("whitespace_substring_match", func(t *testing.T) {
		f := filepath.Join(dir, "ws.py")
		os.WriteFile(f, []byte("def foo():\n    return 1\n\ndef bar():\n    return 2\n"), 0644)

		// "  return 1" (2 spaces) IS a substring of "    return 1" (4 spaces).
		// EditTextFile does exact substring matching, so this WILL match.
		// This is correct behavior — the tool matches substrings, not lines.
		// The SubAgent prompt instructs the LLM to include enough context
		// (surrounding lines) to ensure unique matching.
		res, err := EditTextFile(f, "    return 1", "    return 42", false)
		if err != nil {
			t.Fatalf("exact 4-space match should succeed: %v", err)
		}
		if res.Count != 1 {
			t.Errorf("expected 1, got %d", res.Count)
		}
		data, _ := os.ReadFile(f)
		if !strings.Contains(string(data), "return 42") {
			t.Error("content not updated")
		}
		// "return 2" in func bar should be untouched
		if !strings.Contains(string(data), "return 2") {
			t.Error("func bar was incorrectly modified")
		}
	})

	t.Run("context_lines_disambiguate_duplicates", func(t *testing.T) {
		f := filepath.Join(dir, "dup.go")
		os.WriteFile(f, []byte("func a() {\n\treturn nil\n}\n\nfunc b() {\n\treturn nil\n}\n"), 0644)

		// "return nil" appears twice. Include surrounding context to target func b only.
		res, err := EditTextFile(f,
			"func b() {\n\treturn nil\n}",
			"func b() {\n\treturn fmt.Errorf(\"b error\")\n}",
			false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.Count != 1 {
			t.Errorf("expected 1, got %d", res.Count)
		}
		data, _ := os.ReadFile(f)
		// func a should still have "return nil"
		if !strings.Contains(string(data), "func a() {\n\treturn nil\n}") {
			t.Error("func a was incorrectly modified")
		}
		// func b should have the new error
		if !strings.Contains(string(data), "b error") {
			t.Error("func b was not updated")
		}
	})
}
