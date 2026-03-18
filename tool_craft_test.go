package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDetectScriptLanguage(t *testing.T) {
	tests := []struct {
		task     string
		expected string
	}{
		{"用 python 分析 CSV 数据", "python"},
		{"pip install requests 然后调用 API", "python"},
		{"用 pandas 处理 Excel", "python"},
		{"node 写个 HTTP 服务器", "node"},
		{"npm install express", "node"},
		{"用 javascript 解析 JSON", "node"},
		// "js" as standalone word should match, but not as substring.
		{"用 js 写个脚本", "node"},
	}
	for _, tt := range tests {
		result := detectScriptLanguage(tt.task)
		if result != tt.expected {
			t.Errorf("detectScriptLanguage(%q) = %q, want %q", tt.task, result, tt.expected)
		}
	}

	// "json" should NOT trigger node detection (false positive guard).
	result := detectScriptLanguage("parse json file")
	if result == "node" {
		t.Errorf("detectScriptLanguage(\"parse json file\") = %q, should not match 'js' in 'json'", result)
	}
}

func TestScriptExtension(t *testing.T) {
	tests := []struct {
		lang string
		ext  string
	}{
		{"python", ".py"},
		{"node", ".js"},
		{"javascript", ".js"},
		{"powershell", ".ps1"},
		{"bash", ".sh"},
		{"", ".sh"},
	}
	for _, tt := range tests {
		result := scriptExtension(tt.lang)
		if result != tt.ext {
			t.Errorf("scriptExtension(%q) = %q, want %q", tt.lang, result, tt.ext)
		}
	}
}

func TestSanitizeFilename(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"hello world", "hello_world"},
		{"test/path\\file", "test_path_file"},
		{"abc123", "abc123"},
		{"mix_中文_eng", "mix__eng"},
	}
	for _, tt := range tests {
		result := sanitizeFilename(tt.input)
		if result != tt.expected {
			t.Errorf("sanitizeFilename(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}

	// CJK-only input should produce a hash-based name, not "script".
	cjk := sanitizeFilename("用中文描述")
	if !strings.HasPrefix(cjk, "task_") {
		t.Errorf("sanitizeFilename(CJK) = %q, expected prefix 'task_'", cjk)
	}
	if cjk == "task_00000000" {
		t.Error("CJK hash should not be zero")
	}

	// Empty input should also produce a hash-based name.
	empty := sanitizeFilename("")
	if empty != "task_00000000" {
		t.Errorf("sanitizeFilename(\"\") = %q, expected 'task_00000000'", empty)
	}

	// Different CJK inputs should produce different hashes.
	cjk2 := sanitizeFilename("另一个任务")
	if cjk == cjk2 {
		t.Errorf("different CJK inputs produced same hash: %q", cjk)
	}
}

func TestGenerateSkillName(t *testing.T) {
	name := generateSkillName("fetch weather data from API")
	if !strings.HasPrefix(name, "craft_") {
		t.Errorf("expected prefix 'craft_', got %q", name)
	}
	if strings.Contains(name, " ") {
		t.Errorf("skill name should not contain spaces: %q", name)
	}
}

func TestExtractTriggerKeywords(t *testing.T) {
	triggers := extractTriggerKeywords("fetch weather data from API and save to file")
	if len(triggers) == 0 {
		t.Error("expected at least one trigger keyword")
	}
	if len(triggers) > 5 {
		t.Errorf("expected at most 5 triggers, got %d", len(triggers))
	}
}

func TestStripScriptCodeFences(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"print('hello')", "print('hello')"},
		{"```python\nprint('hello')\n```", "print('hello')"},
		{"```\necho hello\n```", "echo hello"},
		{"  ```bash\necho hello\n```  ", "echo hello"},
	}
	for _, tt := range tests {
		result := stripScriptCodeFences(tt.input)
		if result != tt.expected {
			t.Errorf("stripScriptCodeFences(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func TestSaveScript(t *testing.T) {
	script := "echo hello world"
	path, err := saveScript(script, "bash", "test echo")
	if err != nil {
		t.Fatalf("saveScript failed: %v", err)
	}
	defer os.Remove(path)

	// Verify file exists and has correct content.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read saved script: %v", err)
	}
	if string(data) != script {
		t.Errorf("script content mismatch: got %q", string(data))
	}

	// Verify it's in the crafted_tools directory.
	if !strings.Contains(path, "crafted_tools") {
		t.Errorf("expected path to contain 'crafted_tools': %s", path)
	}

	// Verify extension.
	if filepath.Ext(path) != ".sh" {
		t.Errorf("expected .sh extension, got %s", filepath.Ext(path))
	}
}

func TestBuildRunCommand(t *testing.T) {
	tests := []struct {
		language string
		contains string
	}{
		{"python", "python"},
		{"node", "node"},
		{"powershell", "powershell"},
	}
	for _, tt := range tests {
		cmd := buildRunCommand("/tmp/test.py", tt.language)
		if !strings.Contains(cmd, tt.contains) {
			t.Errorf("buildRunCommand(%q) = %q, expected to contain %q", tt.language, cmd, tt.contains)
		}
	}
}

func TestCraftedToolsDir(t *testing.T) {
	dir := craftedToolsDir()
	if !strings.Contains(dir, ".cceasy") || !strings.Contains(dir, "crafted_tools") {
		t.Errorf("unexpected crafted tools dir: %s", dir)
	}
}
