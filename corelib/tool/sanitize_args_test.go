package tool

import "testing"

func TestCleanToolArguments(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "valid JSON unchanged",
			input:    `{"command": "ls -la"}`,
			expected: `{"command": "ls -la"}`,
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "code fence json",
			input:    "```json\n{\"command\": \"ls\"}\n```",
			expected: `{"command": "ls"}`,
		},
		{
			name:     "code fence no lang",
			input:    "```\n{\"key\": \"val\"}\n```",
			expected: `{"key": "val"}`,
		},
		{
			name:     "single quote wrapper",
			input:    `'{"command": "echo hello"}'`,
			expected: `{"command": "echo hello"}`,
		},
		{
			name:     "over-escaped quotes",
			input:    `{\"command\": \"ls -la\"}`,
			expected: `{"command": "ls -la"}`,
		},
		{
			name:     "escaped single quotes",
			input:    `{"msg": "it\'s fine"}`,
			expected: `{"msg": "it's fine"}`,
		},
		{
			name:     "whitespace trimming",
			input:    "  \n  {\"a\": 1}  \n  ",
			expected: `{"a": 1}`,
		},
		{
			name:     "code fence JSON uppercase",
			input:    "```JSON\n{\"x\": 1}\n```",
			expected: `{"x": 1}`,
		},
		{
			name:     "single quote non-json not unwrapped",
			input:    "'hello world'",
			expected: "'hello world'",
		},
		{
			name:     "array with code fence",
			input:    "```json\n[1, 2, 3]\n```",
			expected: "[1, 2, 3]",
		},
		{
			name:     "over-escaped array",
			input:    `[\"a\", \"b\"]`,
			expected: `["a", "b"]`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CleanToolArguments(tt.input)
			if got != tt.expected {
				t.Errorf("CleanToolArguments(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}
