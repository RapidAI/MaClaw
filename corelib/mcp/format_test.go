package mcp

import (
	"strings"
	"testing"
)

func TestFormatArgSummaryUnionTypes(t *testing.T) {
	got := FormatArgSummary(map[string]any{
		"type": "object",
		"properties": map[string]any{
			"q": map[string]any{"type": []any{"string", "null"}, "description": "query"},
			"n": map[string]any{"anyOf": []any{
				map[string]any{"type": "integer"},
				map[string]any{"type": "null"},
			}},
			"plain": map[string]any{"type": "boolean"},
		},
		"required": []any{"q"},
	})
	for _, want := range []string{"q:string|null*", "n:integer|null", "plain:boolean"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %s in %s", want, got)
		}
	}
	if strings.Contains(got, ":any") {
		t.Fatalf("union collapsed to any: %s", got)
	}
}
