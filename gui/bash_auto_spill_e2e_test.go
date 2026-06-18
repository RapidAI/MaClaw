package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestAutoSpillEndToEndExecutesCorrectly verifies that the spilled .py file
// executes correctly and produces the expected output.
// This is the ultimate correctness test for the auto-spill mechanism.
func TestAutoSpillEndToEndExecutesCorrectly(t *testing.T) {
	pythonBin := "python"
	if _, err := exec.LookPath(pythonBin); err != nil {
		pythonBin = "python3"
		if _, err := exec.LookPath(pythonBin); err != nil {
			t.Skip("python not found, skipping end-to-end test")
		}
	}

	dir := t.TempDir()
	outFile := filepath.Join(dir, "result.txt")

	// Simulate what arrives after JSON unmarshal of:
	// {"command": "python -c \"import os\\nwith open('out.txt','w') as f:\\n    f.write('OK')\""}
	//
	// After JSON unmarshal, command string is:
	// python -c "import os\nwith open('out.txt','w') as f:\n    f.write('OK')"
	// where \n = actual newline, \" = literal \", \\ = literal \\
	//
	// This simulates the EXACT format that toolBash receives from the agent loop.
	command := pythonBin + " -c \"" +
		"import os\n" +
		"msg = \\\"hello world\\\"\n" + // Python source: msg = "hello world"
		"path = 'C:\\\\Users\\\\test'\n" + // Python source: path = 'C:\\Users\\test' -> value C:\Users\test
		"with open(r'" + filepath.ToSlash(outFile) + "', 'w', encoding='utf-8') as f:\n" +
		"    f.write(msg + '|' + path)\n" +
		"\""

	t.Logf("Command (repr): %q", command)

	// Auto-spill
	result, err := autoSpillPythonScript(command, dir)
	if err != nil {
		t.Fatalf("autoSpillPythonScript failed: %v", err)
	}
	defer os.Remove(result.TempFile)

	// Verify the .py file content is valid Python
	pyContent, _ := os.ReadFile(result.TempFile)
	t.Logf("Spilled .py content:\n%s", pyContent)

	// Execute the spilled command
	var shell string
	var args []string
	if runtime.GOOS == "windows" {
		shell = "powershell"
		args = []string{"-NoProfile", "-NonInteractive", "-Command",
			"[Console]::OutputEncoding = [System.Text.Encoding]::UTF8; " + result.Command}
	} else {
		shell = "bash"
		args = []string{"-c", result.Command}
	}
	cmd := exec.Command(shell, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("execution failed: %v\ncommand: %s\noutput: %s\npy file:\n%s", err, result.Command, out, pyContent)
	}

	// Verify output file content
	content, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("read output file: %v\nstdout: %s", err, out)
	}

	want := `hello world|C:\Users\test`
	if string(content) != want {
		t.Fatalf("output = %q, want %q\npy file:\n%s", content, want, pyContent)
	}
	t.Logf("Output correct: %q", content)
}

// TestAutoSpillEndToEndChinesePatent verifies auto-spill with Chinese content
// (the actual patent workflow scenario).
func TestAutoSpillEndToEndChinesePatent(t *testing.T) {
	pythonBin := "python"
	if _, err := exec.LookPath(pythonBin); err != nil {
		pythonBin = "python3"
		if _, err := exec.LookPath(pythonBin); err != nil {
			t.Skip("python not found, skipping end-to-end test")
		}
	}

	dir := t.TempDir()
	outFile := filepath.Join(dir, "patent.txt")

	// Simulate a patent script with Chinese text (as it arrives after JSON unmarshal)
	command := pythonBin + " -c \"" +
		"claims = [\\\"1. \u4e00\u79cd\u6587\u4ef6\u7c7b\u578b\u667a\u80fd\u8bc6\u522b\u88c5\u7f6e\\\", \\\"2. \u6839\u636e\u6743\u5229\u8981\u6c421\\\"]\n" +
		"with open(r'" + filepath.ToSlash(outFile) + "', 'w', encoding='utf-8') as f:\n" +
		"    f.write('\\\\n'.join(claims))\n" +
		"\""

	result, err := autoSpillPythonScript(command, dir)
	if err != nil {
		t.Fatalf("autoSpillPythonScript failed: %v", err)
	}
	defer os.Remove(result.TempFile)

	pyContent, _ := os.ReadFile(result.TempFile)
	t.Logf("Spilled .py content:\n%s", pyContent)

	var shell string
	var shellArgs []string
	if runtime.GOOS == "windows" {
		shell = "powershell"
		shellArgs = []string{"-NoProfile", "-NonInteractive", "-Command",
			"[Console]::OutputEncoding = [System.Text.Encoding]::UTF8; " + result.Command}
	} else {
		shell = "bash"
		shellArgs = []string{"-c", result.Command}
	}
	cmd := exec.Command(shell, shellArgs...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("execution failed: %v\ncommand: %s\noutput: %s\npy file:\n%s", err, result.Command, out, pyContent)
	}

	content, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("read output: %v\nstdout: %s", err, out)
	}

	if !strings.Contains(string(content), "\u4e00\u79cd\u6587\u4ef6") {
		t.Fatalf("Chinese content lost: %q\npy file:\n%s", content, pyContent)
	}
	if !strings.Contains(string(content), `\n`) {
		t.Fatalf("literal \\n separator lost: %q", content)
	}
	t.Logf("Patent output correct: %q", content)
}
