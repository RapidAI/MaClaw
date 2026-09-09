package agentruntime

import (
	"reflect"
	"testing"
)

func TestAppendUniqueStringsPreservesOrderAndNormalizesWhitespace(t *testing.T) {
	got := AppendUniqueStrings([]string{" a ", "b"}, "b", " ", " c ", "a", "c")
	want := []string{" a ", "b", "c"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("AppendUniqueStrings=%#v, want %#v", got, want)
	}
}

func TestFilterToolDefinitionsByName(t *testing.T) {
	definitions := []map[string]any{
		{"type": "function", "function": map[string]any{"name": "keep"}},
		{"type": "function", "function": map[string]any{"name": "drop"}},
		{"type": "function", "function": map[string]any{"name": "keep"}},
	}
	got := FilterToolDefinitionsByName(definitions, " drop ")
	if len(got) != 2 || ToolDefinitionName(got[0]) != "keep" || ToolDefinitionName(got[1]) != "keep" {
		t.Fatalf("filtered definitions=%#v", got)
	}
}
