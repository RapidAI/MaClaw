package tool

import (
	"strings"
	"testing"

	"pgregory.net/rapid"
)

// Feature: skillrouter-body-aware-retrieval, Property 5: MCP Body 构建包含 Schema 信息
// For any inputSchema with ≥1 property, output contains each property's name and type.
// Empty/nil schema returns "".
// **Validates: Requirements 3.1, 3.3**
func TestProperty_BuildMCPToolBodyContainsSchema(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		numProps := rapid.IntRange(1, 10).Draw(t, "numProps")
		props := make(map[string]interface{}, numProps)
		expected := make(map[string]string, numProps)

		for i := 0; i < numProps; i++ {
			name := rapid.StringMatching(`[a-z_]{2,15}`).Draw(t, "propName")
			typ := rapid.SampledFrom([]string{"string", "integer", "boolean", "number", "object", "array"}).Draw(t, "propType")
			props[name] = map[string]interface{}{"type": typ}
			expected[name] = typ
		}

		schema := map[string]interface{}{
			"type":       "object",
			"properties": props,
		}

		result := BuildMCPToolBody(schema)
		if result == "" {
			t.Fatal("expected non-empty result for schema with properties")
		}

		for name, typ := range expected {
			if !strings.Contains(result, name) {
				t.Fatalf("result should contain property name %q, got: %s", name, result)
			}
			if !strings.Contains(result, typ) {
				t.Fatalf("result should contain type %q for property %q, got: %s", typ, name, result)
			}
		}
	})
}

func TestProperty_BuildMCPToolBodyEmpty(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// nil schema
		if got := BuildMCPToolBody(nil); got != "" {
			t.Fatalf("nil schema should return empty, got %q", got)
		}
		// empty schema
		if got := BuildMCPToolBody(map[string]interface{}{}); got != "" {
			t.Fatalf("empty schema should return empty, got %q", got)
		}
		// schema without properties key
		if got := BuildMCPToolBody(map[string]interface{}{"type": "object"}); got != "" {
			t.Fatalf("schema without properties should return empty, got %q", got)
		}
	})
}
