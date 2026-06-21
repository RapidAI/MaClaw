package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestBashCommandIsAutoSpillableWithInstallPrefix(t *testing.T) {
	command := `pip install python-docx -q && python -c "` + strings.Repeat("print('x')\n", 500) + `"`
	if !bashCommandIsAutoSpillable(command) {
		t.Fatalf("command should be auto-spillable")
	}
}

func TestBashCommandIsAutoSpillableRejectsQuotedMention(t *testing.T) {
	// "python -c" inside a quoted string should NOT be spillable
	command := `echo "run python -c to test inline scripts"`
	if bashCommandIsAutoSpillable(command) {
		t.Fatalf("command with python -c inside quotes should NOT be auto-spillable")
	}
}

func TestAutoSpillPythonScriptPreservesPrefixAndSuffix(t *testing.T) {
	dir := t.TempDir()
	command := `pip install python-docx -q && python -c "print('hello')" && echo done`
	result, err := autoSpillPythonScript(command, dir)
	if err != nil {
		t.Fatalf("autoSpillPythonScript: %v", err)
	}
	if !strings.HasPrefix(result.Command, `pip install python-docx -q && python `) {
		t.Fatalf("spilled command lost prefix: %q", result.Command)
	}
	if !strings.Contains(result.Command, `&& echo done`) {
		t.Fatalf("spilled command lost suffix: %q", result.Command)
	}
	if result.TempFile == "" {
		t.Fatal("TempFile should not be empty")
	}
	defer os.Remove(result.TempFile)

	content, err := os.ReadFile(result.TempFile)
	if err != nil {
		t.Fatalf("read spilled script: %v", err)
	}
	if string(content) != "print('hello')" {
		t.Fatalf("spilled script = %q, want %q", content, "print('hello')")
	}
}

func TestAutoSpillPythonScriptUnescapesEmbeddedDoubleQuotes(t *testing.T) {
	dir := t.TempDir()
	command := `python -c "print(\"hello\")"`
	result, err := autoSpillPythonScript(command, dir)
	if err != nil {
		t.Fatalf("autoSpillPythonScript: %v", err)
	}
	defer os.Remove(result.TempFile)

	content, err := os.ReadFile(result.TempFile)
	if err != nil {
		t.Fatalf("read spilled script: %v", err)
	}
	if string(content) != `print("hello")` {
		t.Fatalf("spilled script = %q, want %q", content, `print("hello")`)
	}
}

func TestAutoSpillPythonScriptUnescapesEmbeddedSingleQuotes(t *testing.T) {
	dir := t.TempDir()
	command := `python -c 'print(\'hello\')'`
	result, err := autoSpillPythonScript(command, dir)
	if err != nil {
		t.Fatalf("autoSpillPythonScript: %v", err)
	}
	defer os.Remove(result.TempFile)

	content, err := os.ReadFile(result.TempFile)
	if err != nil {
		t.Fatalf("read spilled script: %v", err)
	}
	if string(content) != `print('hello')` {
		t.Fatalf("spilled script = %q, want %q", content, `print('hello')`)
	}
}

func TestAutoSpillTempFileCleanup(t *testing.T) {
	dir := t.TempDir()
	command := `python -c "print('cleanup test')"`
	result, err := autoSpillPythonScript(command, dir)
	if err != nil {
		t.Fatalf("autoSpillPythonScript: %v", err)
	}
	// Verify file exists
	if _, err := os.Stat(result.TempFile); err != nil {
		t.Fatalf("temp file should exist: %v", err)
	}
	// Simulate cleanup (what toolBash defer does)
	os.Remove(result.TempFile)
	if _, err := os.Stat(result.TempFile); err == nil {
		t.Fatal("temp file should have been removed")
	}
}

func TestPreCheckAllowsOversizedPrefixedPythonCommandForAutoSpill(t *testing.T) {
	command := `pip install python-docx -q && python -c "` + strings.Repeat("print('x')\n", 500) + `"`
	result := preCheckAgentLoopInlinePayloadLimit("bash", `{"command":`+strconv.Quote(command)+`}`, 1)
	if result != nil {
		t.Fatalf("precheck rejected auto-spillable command: %+v", result)
	}
}

func TestPreCheckAllowsOversizedNonPythonBashForAutoSpill(t *testing.T) {
	command := `echo "` + strings.Repeat("x", 5000) + `"`
	result := preCheckAgentLoopInlinePayloadLimit("bash", `{"command":`+strconv.Quote(command)+`}`, 1)
	if result != nil {
		t.Fatalf("precheck rejected oversized bash command that should be auto-spilled: %+v", result)
	}
}

func TestAutoSpillShellScriptWritesCommand(t *testing.T) {
	dir := t.TempDir()
	command := "echo hello\n" + strings.Repeat("echo x\n", 20)
	result, err := autoSpillShellScript(command, dir)
	if err != nil {
		t.Fatalf("autoSpillShellScript: %v", err)
	}
	defer os.Remove(result.TempFile)
	if result.TempFile == "" {
		t.Fatal("TempFile should not be empty")
	}
	content, err := os.ReadFile(result.TempFile)
	if err != nil {
		t.Fatalf("read spilled shell script: %v", err)
	}
	if !strings.Contains(string(content), "echo hello") {
		t.Fatalf("spilled shell script missing command: %q", content)
	}
	if strings.Contains(string(content), "\nset -e\n") {
		t.Fatalf("spilled shell script should preserve original exit semantics, got %q", content)
	}
	if result.Command == "" || !strings.Contains(result.Command, filepath.ToSlash(result.TempFile)) {
		t.Fatalf("spilled command should execute temp file, got %q for %q", result.Command, result.TempFile)
	}
}

func TestAutoSpillPythonScriptPreservesBackslashesInDoubleQuoted(t *testing.T) {
	dir := t.TempDir()
	// After JSON unmarshal, the command string has literal \\ (two chars) which is
	// Python's escaped backslash. The .py file must preserve them as-is because
	// Python needs \\ in source to represent a literal \ at runtime.
	// (JSON escaping and shell escaping are conflated — LLM uses JSON escaping only)
	command := "python -c \"path = 'C:\\\\Users\\\\test'\""
	result, err := autoSpillPythonScript(command, dir)
	if err != nil {
		t.Fatalf("autoSpillPythonScript: %v", err)
	}
	defer os.Remove(result.TempFile)

	content, err := os.ReadFile(result.TempFile)
	if err != nil {
		t.Fatalf("read spilled script: %v", err)
	}
	// .py file must have \\ (double backslash) for Python to interpret as literal \
	want := "path = 'C:\\\\Users\\\\test'"
	if string(content) != want {
		t.Fatalf("spilled script = %q, want %q", content, want)
	}
}
