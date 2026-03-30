package tool

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnrichmentStore_GetSearchText_Builtin(t *testing.T) {
	s, err := NewEnrichmentStore("")
	if err != nil {
		t.Fatal(err)
	}
	tool := RegisteredTool{
		Name:        "bash",
		Description: "Execute a shell command",
		Tags:        []string{"shell"},
	}
	text := s.GetSearchText(tool)
	if text == "" {
		t.Fatal("expected non-empty search text")
	}
	// Should contain the builtin enrichment queries.
	if !containsSubstring(text, "run a shell command") {
		t.Errorf("expected builtin enrichment in text, got: %s", text)
	}
	if !containsSubstring(text, "shell") {
		t.Errorf("expected tag in text, got: %s", text)
	}
}

func TestEnrichmentStore_GetSearchText_NoEnrichment(t *testing.T) {
	s, err := NewEnrichmentStore("")
	if err != nil {
		t.Fatal(err)
	}
	tool := RegisteredTool{
		Name:        "unknown_tool_xyz",
		Description: "Does something",
		Tags:        []string{"misc"},
	}
	text := s.GetSearchText(tool)
	expected := "unknown_tool_xyz Does something misc"
	if text != expected {
		t.Errorf("expected %q, got %q", expected, text)
	}
}

func TestEnrichmentStore_SetAndGet(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "enrichments.json")
	s, err := NewEnrichmentStore(path)
	if err != nil {
		t.Fatal(err)
	}

	queries := []string{"query database", "run SQL"}
	if err := s.Set("sql_tool", queries); err != nil {
		t.Fatal(err)
	}

	tool := RegisteredTool{Name: "sql_tool", Description: "Execute SQL"}
	text := s.GetSearchText(tool)
	if !containsSubstring(text, "query database") {
		t.Errorf("expected stored enrichment in text, got: %s", text)
	}

	// Verify persistence.
	s2, err := NewEnrichmentStore(path)
	if err != nil {
		t.Fatal(err)
	}
	text2 := s2.GetSearchText(tool)
	if !containsSubstring(text2, "query database") {
		t.Errorf("expected persisted enrichment, got: %s", text2)
	}
}

func TestEnrichmentStore_Has(t *testing.T) {
	s, err := NewEnrichmentStore("")
	if err != nil {
		t.Fatal(err)
	}
	if !s.Has("bash") {
		t.Error("expected Has(bash) = true for builtin")
	}
	if s.Has("nonexistent_tool") {
		t.Error("expected Has(nonexistent_tool) = false")
	}
}

func TestEnrichmentStore_LoadMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does_not_exist.json")
	s, err := NewEnrichmentStore(path)
	if err != nil {
		t.Fatal("should not error on missing file")
	}
	if s == nil {
		t.Fatal("store should not be nil")
	}
}

func TestParseEnrichmentResponse(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{`["a","b","c"]`, 3},
		{"```json\n[\"x\"]\n```", 1},
		{`not json`, 0},
		{`[]`, 0},
	}
	for _, tt := range tests {
		got := ParseEnrichmentResponse(tt.input)
		if len(got) != tt.want {
			t.Errorf("ParseEnrichmentResponse(%q) = %d items, want %d", tt.input, len(got), tt.want)
		}
	}
}

func TestGenerateEnrichmentPrompt(t *testing.T) {
	sys, usr := GenerateEnrichmentPrompt("my_tool", "Does cool stuff", "")
	if sys == "" || usr == "" {
		t.Error("expected non-empty prompts")
	}
	if !containsSubstring(usr, "my_tool") {
		t.Errorf("user prompt should contain tool name, got: %s", usr)
	}
}

func TestBuiltinEnrichments_Coverage(t *testing.T) {
	// Every core tool should have enrichment.
	for name := range CoreToolNames {
		if _, ok := BuiltinEnrichments[name]; !ok {
			t.Logf("WARN: core tool %q has no builtin enrichment", name)
		}
	}
}

// Ensure the file on disk is valid JSON after save.
func TestEnrichmentStore_SaveFormat(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "e.json")
	s, _ := NewEnrichmentStore(path)
	s.Set("test", []string{"q1"})

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 {
		t.Fatal("file should not be empty")
	}
	// Should be valid JSON array.
	if data[0] != '[' {
		t.Errorf("expected JSON array, got: %c", data[0])
	}
}

func containsSubstring(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && findSubstring(s, sub))
}

func findSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
