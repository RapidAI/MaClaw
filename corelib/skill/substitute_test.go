package skill

import (
	"testing"
)

func TestSubstituteVariables_DoubleBrace(t *testing.T) {
	result := SubstituteVariables("echo {{name}}", map[string]string{"name": "world"})
	if result != "echo world" {
		t.Errorf("expected 'echo world', got %q", result)
	}
}

func TestSubstituteVariables_DollarBrace(t *testing.T) {
	result := SubstituteVariables("echo ${name}", map[string]string{"name": "world"})
	if result != "echo world" {
		t.Errorf("expected 'echo world', got %q", result)
	}
}

func TestSubstituteVariables_SingleBrace(t *testing.T) {
	result := SubstituteVariables("echo {name}", map[string]string{"name": "world"})
	if result != "echo world" {
		t.Errorf("expected 'echo world', got %q", result)
	}
}

func TestSubstituteVariables_MultipleKeys(t *testing.T) {
	vars := map[string]string{"input": "a.txt", "output": "b.txt"}
	result := SubstituteVariables("cp {{input}} {{output}}", vars)
	if result != "cp a.txt b.txt" {
		t.Errorf("expected 'cp a.txt b.txt', got %q", result)
	}
}

func TestSubstituteVariables_MissingKey(t *testing.T) {
	result := SubstituteVariables("echo {{missing}}", map[string]string{"other": "val"})
	if result != "echo {{missing}}" {
		t.Errorf("missing key should be left unchanged, got %q", result)
	}
}

func TestSubstituteVariables_EmptyCommand(t *testing.T) {
	result := SubstituteVariables("", map[string]string{"key": "val"})
	if result != "" {
		t.Errorf("expected empty string, got %q", result)
	}
}

func TestSubstituteVariables_EmptyVars(t *testing.T) {
	result := SubstituteVariables("echo {{key}}", nil)
	if result != "echo {{key}}" {
		t.Errorf("expected unchanged command, got %q", result)
	}
}

func TestStripUnresolvedPlaceholders(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"echo {{missing}}", "echo "},
		{"echo ${missing}", "echo "},
		{"echo {missing}", "echo "},
		{"echo hello", "echo hello"},
		{"{{a}} and {{b}}", " and "},
		{"no placeholders", "no placeholders"},
	}
	for _, tt := range tests {
		got := StripUnresolvedPlaceholders(tt.input)
		if got != tt.want {
			t.Errorf("StripUnresolvedPlaceholders(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestExtractPlaceholderKeys(t *testing.T) {
	keys := ExtractPlaceholderKeys("node gen.js --desc {{content}} --out {output} --fmt ${format}")
	if len(keys) != 3 {
		t.Fatalf("expected 3 keys, got %d: %v", len(keys), keys)
	}
	expected := map[string]bool{"content": true, "output": true, "format": true}
	for _, k := range keys {
		if !expected[k] {
			t.Errorf("unexpected key %q", k)
		}
	}
}

func TestExtractPlaceholderKeys_SkipsBaseDir(t *testing.T) {
	keys := ExtractPlaceholderKeys("node {baseDir}/gen.js {{input}}")
	if len(keys) != 1 || keys[0] != "input" {
		t.Errorf("expected [input], got %v", keys)
	}
}

func TestExtractPlaceholderKeys_Deduplicates(t *testing.T) {
	keys := ExtractPlaceholderKeys("{{name}} and {{name}} and {name}")
	if len(keys) != 1 {
		t.Errorf("expected 1 unique key, got %d: %v", len(keys), keys)
	}
}

func TestQuoteForShell_Simple(t *testing.T) {
	if got := QuoteForShell("hello"); got != "hello" {
		t.Errorf("simple value should not be quoted, got %q", got)
	}
}

func TestQuoteForShell_WithSpaces(t *testing.T) {
	got := QuoteForShell("hello world")
	if got != `"hello world"` {
		t.Errorf("expected quoted, got %q", got)
	}
}

func TestQuoteForShell_Empty(t *testing.T) {
	if got := QuoteForShell(""); got != `""` {
		t.Errorf("empty should be quoted, got %q", got)
	}
}

func TestQuoteForShell_WithQuotes(t *testing.T) {
	got := QuoteForShell(`say "hello"`)
	if got != `"say \"hello\""` {
		t.Errorf("expected escaped quotes, got %q", got)
	}
}
