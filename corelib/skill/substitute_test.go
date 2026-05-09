package skill

import (
	"runtime"
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

func TestSubstituteVariables_CanonicalKeyShapes(t *testing.T) {
	result := SubstituteVariables("cp {{Input-File}} ${Output File}", map[string]string{
		"input_file":  "report.md",
		"output_file": "out.pdf",
	})
	if result != "cp report.md out.pdf" {
		t.Fatalf("expected canonical placeholder substitution, got %q", result)
	}
}

func TestSubstituteVariables_PreservesAuthorQuotes(t *testing.T) {
	result := SubstituteVariables(`"{{mode}}" == "fast"`, map[string]string{"mode": "fast"})
	if result != `"fast" == "fast"` {
		t.Fatalf("expected author quotes to be preserved, got %q", result)
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
		{`tool --target_lang "{{target_lang}}"`, "tool "},
		{"tool --format=${format}", "tool "},
		{"tool --format:${format}", "tool "},
		{"tool -o {output}", "tool "},
		{"tool --input file.txt --optional {{missing}}", "tool --input file.txt "},
		{`tool "{{optional}}"`, "tool "},
		{"tool '{{optional}}'", "tool "},
		{"tool `{{optional}}`", "tool "},
		{"tool /out {{output}}", "tool "},
		{"tool /out:{output}", "tool "},
		{`tool /out="${output}"`, "tool "},
		{"tool /tmp/{{input}}", "tool /tmp/"},
	}
	for _, tt := range tests {
		got := StripUnresolvedPlaceholders(tt.input)
		if got != tt.want {
			t.Errorf("StripUnresolvedPlaceholders(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestSubstituteVariablesWithQuoteDeduplicatesAuthorQuotes(t *testing.T) {
	quote := func(value string) string { return `"` + value + `"` }
	got := SubstituteVariablesWithQuote(`python translate.py --text "{{text}}" --city ${city}`, map[string]string{
		"text": "hello world",
		"city": "New York",
	}, quote)
	want := `python translate.py --text "hello world" --city "New York"`
	if got != want {
		t.Fatalf("SubstituteVariablesWithQuote() = %q, want %q", got, want)
	}
}

func TestSubstituteVariablesWithQuoteDeduplicatesBacktickPlaceholder(t *testing.T) {
	quote := func(value string) string { return `"` + value + `"` }
	got := SubstituteVariablesWithQuote("tool --text `{{text}}`", map[string]string{
		"text": "hello world",
	}, quote)
	want := `tool --text "hello world"`
	if got != want {
		t.Fatalf("SubstituteVariablesWithQuote() = %q, want %q", got, want)
	}
}

func TestSubstituteVariablesWithQuoteStripsOptionalFlag(t *testing.T) {
	got := SubstituteVariablesWithQuote(`tool --input "{{input}}" --target_lang "{{target_lang}}"`, map[string]string{
		"input": "hello",
	}, nil)
	want := `tool --input "hello" `
	if got != want {
		t.Fatalf("SubstituteVariablesWithQuote() = %q, want %q", got, want)
	}
}

func TestSubstituteVariablesWithQuoteStripsOptionalQuotedToken(t *testing.T) {
	got := SubstituteVariablesWithQuote(`tool --input "{{input}}" "{{optional}}"`, map[string]string{
		"input": "hello",
	}, nil)
	want := `tool --input "hello" `
	if got != want {
		t.Fatalf("SubstituteVariablesWithQuote() = %q, want %q", got, want)
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

func TestExtractPlaceholderKeys_AcceptsCanonicalKeyShapes(t *testing.T) {
	keys := ExtractPlaceholderKeys("tool {{Input-File}} ${Output File} {base-dir}")
	if len(keys) != 2 {
		t.Fatalf("expected 2 keys, got %d: %v", len(keys), keys)
	}
	expected := map[string]bool{"Input-File": true, "Output File": true}
	for _, key := range keys {
		if !expected[key] {
			t.Fatalf("unexpected key %q from %v", key, keys)
		}
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

func TestQuoteForRunnerShell_PlatformQuoting(t *testing.T) {
	got := QuoteForRunnerShell(`hello "world"`)
	if runtime.GOOS == "windows" {
		if got != `"hello \"world\""` {
			t.Fatalf("QuoteForRunnerShell() = %q, want Windows double quotes", got)
		}
		return
	}
	if got != `'hello "world"'` {
		t.Fatalf("QuoteForRunnerShell() = %q, want POSIX single quotes", got)
	}
}

func TestQuoteForShellPreference(t *testing.T) {
	if got := QuoteForShellPreference("a'b", "powershell"); got != "'a''b'" {
		t.Fatalf("PowerShell quote = %q, want doubled single quote", got)
	}
	if got := QuoteForShellPreference("a'b", "bash"); got != `'a'"'"'b'` {
		t.Fatalf("bash quote = %q, want POSIX single quote escape", got)
	}
	if got := QuoteForShellPreference("a b", "cmd"); got != `"a b"` {
		t.Fatalf("cmd quote = %q, want double quotes", got)
	}
}
