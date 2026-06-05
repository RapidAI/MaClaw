package tool

import (
	"os"
	"path/filepath"
	"strings"
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
	if _, ok := BuiltinBodies["edit_file"]; !ok {
		t.Fatal("expected edit_file builtin body")
	}
	queries, ok := BuiltinEnrichments["edit_file"]
	if !ok || len(queries) == 0 {
		t.Fatal("expected edit_file builtin enrichments")
	}
}

func TestExternalCodingSessionToolsAbsentFromPromptEnrichment(t *testing.T) {
	for _, name := range []string{"create_session", "send_and_observe", "control_session"} {
		if CoreToolNames[name] {
			t.Fatalf("%s should not be a core routed tool", name)
		}
		if _, ok := BuiltinBodies[name]; ok {
			t.Fatalf("%s should not have builtin prompt body", name)
		}
		if _, ok := BuiltinEnrichments[name]; ok {
			t.Fatalf("%s should not have builtin search enrichment", name)
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

func TestBrowserBuiltinMetadataUsesMergedToolOnly(t *testing.T) {
	if _, ok := BuiltinBodies["browser"]; !ok {
		t.Fatal("expected builtin body for merged browser tool")
	}
	body := BuiltinBodies["browser"]
	for _, want := range []string{"focused element", "content_format=markdown", "No screenshot fallback"} {
		if !strings.Contains(body, want) {
			t.Fatalf("browser builtin body missing %q: %s", want, body)
		}
	}
	queries, ok := BuiltinEnrichments["browser"]
	if !ok || len(queries) == 0 {
		t.Fatal("expected builtin enrichments for merged browser tool")
	}

	store := &EnrichmentStore{enrichments: map[string]*ToolEnrichment{}}
	for _, name := range []string{"browser_session_start", "browser_observe", "browser_navigate", "browser_click"} {
		if store.Has(name) {
			t.Fatalf("internal browser dispatch tool %q must not be enriched", name)
		}
		text := store.GetSearchText(RegisteredTool{Name: name, Description: "", Tags: []string{"browser"}})
		if strings.Contains(text, "stable refs") || strings.Contains(text, "use browser action") {
			t.Fatalf("internal browser dispatch tool %q got browser enrichment text: %q", name, text)
		}

		reg := NewRegistry()
		if err := reg.Register(RegisteredTool{Name: name, Description: "internal dispatch"}); err != nil {
			t.Fatalf("register internal browser dispatch tool %q: %v", name, err)
		}
		registered, ok := reg.Get(name)
		if !ok {
			t.Fatalf("expected registered internal browser dispatch tool %q", name)
		}
		if registered.Body != "" || registered.BodySummary != "" {
			t.Fatalf("internal browser dispatch tool %q must not get prompt body: %#v", name, registered)
		}
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

func TestGenerateEnrichmentPrompt_WithBody(t *testing.T) {
	bodySummary := "Parameters:\n- query (string): SQL query\n- timeout (int): seconds"
	sys, usr := GenerateEnrichmentPrompt("sql_tool", "Execute SQL queries", bodySummary)

	if !containsSubstring(usr, bodySummary) {
		t.Errorf("user prompt should contain bodySummary, got: %s", usr)
	}
	if !containsSubstring(usr, "sql_tool") {
		t.Errorf("user prompt should contain tool name, got: %s", usr)
	}
	if !containsSubstring(sys, "implementation") || !containsSubstring(sys, "body") {
		t.Errorf("system prompt should mention implementation/body details, got: %s", sys)
	}
	if !containsSubstring(sys, "distinguish") {
		t.Errorf("system prompt should instruct to distinguish from similar tools, got: %s", sys)
	}
}

func TestGenerateEnrichmentPrompt_EmptyBody(t *testing.T) {
	sys, usr := GenerateEnrichmentPrompt("my_tool", "Does cool stuff", "")

	if containsSubstring(usr, "Body Summary") {
		t.Errorf("user prompt should not contain 'Body Summary' when empty, got: %s", usr)
	}
	if containsSubstring(sys, "body summary") {
		t.Errorf("system prompt should not mention 'body summary' when empty, got: %s", sys)
	}
	if !containsSubstring(usr, "my_tool") {
		t.Errorf("user prompt should contain tool name, got: %s", usr)
	}
	if !containsSubstring(usr, "Does cool stuff") {
		t.Errorf("user prompt should contain description, got: %s", usr)
	}
}
