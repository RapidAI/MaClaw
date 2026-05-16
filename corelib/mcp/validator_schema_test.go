package mcp

import (
	"context"
	"strings"
	"testing"
)

// --- CheckSchemaCorrectness unit tests ---

func TestCheckSchemaCorrectness_ValidSchema_AllRequiredHaveProperties(t *testing.T) {
	tools := []ToolEntry{
		{
			ToolName: "search",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"query":  map[string]interface{}{"type": "string"},
					"limit":  map[string]interface{}{"type": "integer"},
					"format": map[string]interface{}{"type": "string", "enum": []interface{}{"json", "text"}},
				},
				"required": []interface{}{"query", "limit"},
			},
		},
	}

	v := NewValidator()
	result := v.CheckSchemaCorrectness(context.Background(), tools)

	if !result.Valid {
		t.Fatalf("expected valid=true, got errors: %v", result.Errors)
	}
	if len(result.Errors) != 0 {
		t.Fatalf("expected zero errors, got %d", len(result.Errors))
	}
}

func TestCheckSchemaCorrectness_ValidSchema_MultipleTools(t *testing.T) {
	tools := []ToolEntry{
		{
			ToolName: "get_weather",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"city": map[string]interface{}{"type": "string"},
				},
				"required": []interface{}{"city"},
			},
		},
		{
			ToolName: "send_email",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"to":      map[string]interface{}{"type": "string"},
					"subject": map[string]interface{}{"type": "string"},
					"body":    map[string]interface{}{"type": "string"},
				},
				"required": []interface{}{"to", "subject", "body"},
			},
		},
	}

	v := NewValidator()
	result := v.CheckSchemaCorrectness(context.Background(), tools)

	if !result.Valid {
		t.Fatalf("expected valid=true for multiple valid tools, got errors: %v", result.Errors)
	}
}

func TestCheckSchemaCorrectness_ValidSchema_NoRequiredParams(t *testing.T) {
	tools := []ToolEntry{
		{
			ToolName: "list_items",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"page": map[string]interface{}{"type": "integer"},
				},
			},
		},
	}

	v := NewValidator()
	result := v.CheckSchemaCorrectness(context.Background(), tools)

	if !result.Valid {
		t.Fatalf("expected valid=true for schema with no required params, got errors: %v", result.Errors)
	}
}

func TestCheckSchemaCorrectness_ValidSchema_NilInputSchema(t *testing.T) {
	tools := []ToolEntry{
		{
			ToolName:    "ping",
			InputSchema: nil,
		},
	}

	v := NewValidator()
	result := v.CheckSchemaCorrectness(context.Background(), tools)

	if !result.Valid {
		t.Fatalf("expected valid=true for nil schema, got errors: %v", result.Errors)
	}
}

func TestCheckSchemaCorrectness_ValidSchema_EmptyRequiredSlice(t *testing.T) {
	tools := []ToolEntry{
		{
			ToolName: "noop",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"verbose": map[string]interface{}{"type": "boolean"},
				},
				"required": []interface{}{},
			},
		},
	}

	v := NewValidator()
	result := v.CheckSchemaCorrectness(context.Background(), tools)

	if !result.Valid {
		t.Fatalf("expected valid=true for empty required slice, got errors: %v", result.Errors)
	}
}

func TestCheckSchemaCorrectness_ValidSchema_RoundTripWithAllTypes(t *testing.T) {
	tools := []ToolEntry{
		{
			ToolName: "complex_tool",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name":    map[string]interface{}{"type": "string"},
					"count":   map[string]interface{}{"type": "integer"},
					"ratio":   map[string]interface{}{"type": "number"},
					"enabled": map[string]interface{}{"type": "boolean"},
					"tags":    map[string]interface{}{"type": "array"},
					"config":  map[string]interface{}{"type": "object"},
				},
				"required": []interface{}{"name", "count", "ratio", "enabled", "tags", "config"},
			},
		},
	}

	v := NewValidator()
	result := v.CheckSchemaCorrectness(context.Background(), tools)

	if !result.Valid {
		t.Fatalf("expected valid=true for round-trip with all types, got errors: %v", result.Errors)
	}
}

func TestCheckSchemaCorrectness_MissingRequiredProperty_SingleParam(t *testing.T) {
	tools := []ToolEntry{
		{
			ToolName: "broken_tool",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name": map[string]interface{}{"type": "string"},
				},
				"required": []interface{}{"name", "age"},
			},
		},
	}

	v := NewValidator()
	result := v.CheckSchemaCorrectness(context.Background(), tools)

	if result.Valid {
		t.Fatal("expected valid=false for missing required property")
	}
	if len(result.Errors) == 0 {
		t.Fatal("expected at least one error")
	}

	found := false
	for _, e := range result.Errors {
		if e.ToolName == "broken_tool" && strings.Contains(e.Message, "age") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected error mentioning 'age' for 'broken_tool', got: %v", result.Errors)
	}
}

func TestCheckSchemaCorrectness_MissingRequiredProperty_AllMissing(t *testing.T) {
	tools := []ToolEntry{
		{
			ToolName: "ghost_tool",
			InputSchema: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
				"required":   []interface{}{"alpha", "beta", "gamma"},
			},
		},
	}

	v := NewValidator()
	result := v.CheckSchemaCorrectness(context.Background(), tools)

	if result.Valid {
		t.Fatal("expected valid=false when all required params are missing from properties")
	}
	if len(result.Errors) < 3 {
		t.Fatalf("expected at least 3 errors (one per missing param), got %d: %v", len(result.Errors), result.Errors)
	}

	for _, e := range result.Errors {
		if e.ToolName != "ghost_tool" {
			t.Fatalf("expected all errors for 'ghost_tool', got tool_name=%s", e.ToolName)
		}
	}
}

func TestCheckSchemaCorrectness_MissingRequiredProperty_NoPropertiesField(t *testing.T) {
	tools := []ToolEntry{
		{
			ToolName: "no_props_tool",
			InputSchema: map[string]interface{}{
				"type":     "object",
				"required": []interface{}{"id"},
			},
		},
	}

	v := NewValidator()
	result := v.CheckSchemaCorrectness(context.Background(), tools)

	if result.Valid {
		t.Fatal("expected valid=false when required param declared but no properties defined")
	}
	if len(result.Errors) == 0 {
		t.Fatal("expected at least one error")
	}

	found := false
	for _, e := range result.Errors {
		if e.ToolName == "no_props_tool" && strings.Contains(e.Message, "no properties defined") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected error about 'no properties defined', got: %v", result.Errors)
	}
}

func TestCheckSchemaCorrectness_MissingRequiredProperty_MultipleTools(t *testing.T) {
	tools := []ToolEntry{
		{
			ToolName: "tool_a",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"x": map[string]interface{}{"type": "string"},
				},
				"required": []interface{}{"x", "y"},
			},
		},
		{
			ToolName: "tool_b",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"a": map[string]interface{}{"type": "string"},
				},
				"required": []interface{}{"a"},
			},
		},
	}

	v := NewValidator()
	result := v.CheckSchemaCorrectness(context.Background(), tools)

	if result.Valid {
		t.Fatal("expected valid=false when tool_a has missing property")
	}

	// Only tool_a should have errors.
	for _, e := range result.Errors {
		if e.ToolName == "tool_b" {
			t.Fatalf("tool_b should not have errors, got: %v", e)
		}
	}

	found := false
	for _, e := range result.Errors {
		if e.ToolName == "tool_a" && strings.Contains(e.Message, "y") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected error for tool_a mentioning 'y', got: %v", result.Errors)
	}
}

func TestCheckSchemaCorrectness_InvalidSchema_RoundTripFailure(t *testing.T) {
	// Schema declares "name" as required with type "integer", but constructSampleArgs
	// will produce a float64(0) for it. Then we add a second required param "mode"
	// with an enum. constructSampleArgs picks the first enum value. This should pass
	// round-trip. To force a round-trip failure, we need a schema where the constructed
	// sample args fail ValidateArgs.
	//
	// The simplest way: declare a required param that exists in properties but has
	// a type that constructSampleArgs generates correctly. ValidateArgs itself won't
	// fail for well-formed schemas. So we test the "required but no property" case
	// which causes constructSampleArgs to skip the param → ValidateArgs reports
	// missing_required.
	tools := []ToolEntry{
		{
			ToolName: "roundtrip_fail",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name": map[string]interface{}{"type": "string"},
				},
				// "email" is required but has no property definition.
				// constructSampleArgs only fills params that exist in properties AND are required.
				// So "email" won't be in sample args → ValidateArgs reports missing_required.
				"required": []interface{}{"name", "email"},
			},
		},
	}

	v := NewValidator()
	result := v.CheckSchemaCorrectness(context.Background(), tools)

	if result.Valid {
		t.Fatal("expected valid=false for round-trip validation failure")
	}

	// Should have errors: one for missing property definition, one for round-trip failure.
	if len(result.Errors) < 2 {
		t.Fatalf("expected at least 2 errors, got %d: %v", len(result.Errors), result.Errors)
	}

	hasPropertyError := false
	hasRoundTripError := false
	for _, e := range result.Errors {
		if strings.Contains(e.Message, "no corresponding property definition") {
			hasPropertyError = true
		}
		if strings.Contains(e.Message, "round-trip validation failed") {
			hasRoundTripError = true
		}
	}
	if !hasPropertyError {
		t.Fatal("expected error about missing property definition")
	}
	if !hasRoundTripError {
		t.Fatal("expected error about round-trip validation failure")
	}
}

func TestCheckSchemaCorrectness_InvalidSchema_MalformedPropertiesType(t *testing.T) {
	// Properties field is not a map — schema is structurally malformed.
	// CheckSchemaCorrectness should handle this gracefully (no panic).
	tools := []ToolEntry{
		{
			ToolName: "malformed_tool",
			InputSchema: map[string]interface{}{
				"type":       "object",
				"properties": "not-a-map",
				"required":   []interface{}{"foo"},
			},
		},
	}

	v := NewValidator()
	result := v.CheckSchemaCorrectness(context.Background(), tools)

	// When properties is not a map, the code treats it as nil properties.
	// Required param "foo" with nil properties → error.
	if result.Valid {
		t.Fatal("expected valid=false for malformed properties field")
	}
	if len(result.Errors) == 0 {
		t.Fatal("expected at least one error for malformed schema")
	}
}

func TestCheckSchemaCorrectness_InvalidSchema_RequiredNotSlice(t *testing.T) {
	// "required" field is not a slice — structurally malformed.
	// CheckSchemaCorrectness should handle gracefully (no panic, no errors).
	tools := []ToolEntry{
		{
			ToolName: "bad_required_type",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"x": map[string]interface{}{"type": "string"},
				},
				"required": "x", // should be []interface{}{"x"}
			},
		},
	}

	v := NewValidator()
	result := v.CheckSchemaCorrectness(context.Background(), tools)

	// The code checks `reqSlice, ok := reqRaw.([]interface{})` — if not a slice,
	// it skips the required check entirely. No errors reported, schema considered valid.
	if !result.Valid {
		t.Fatalf("expected valid=true (graceful skip) for non-slice required, got errors: %v", result.Errors)
	}
}

func TestCheckSchemaCorrectness_ValidSchema_WithEnumAndDefault(t *testing.T) {
	tools := []ToolEntry{
		{
			ToolName: "format_tool",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"output_format": map[string]interface{}{
						"type":    "string",
						"enum":    []interface{}{"json", "xml", "csv"},
						"default": "json",
					},
					"pretty": map[string]interface{}{
						"type":    "boolean",
						"default": true,
					},
				},
				"required": []interface{}{"output_format"},
			},
		},
	}

	v := NewValidator()
	result := v.CheckSchemaCorrectness(context.Background(), tools)

	if !result.Valid {
		t.Fatalf("expected valid=true for schema with enum and default, got errors: %v", result.Errors)
	}
}

func TestCheckSchemaCorrectness_EmptyToolList(t *testing.T) {
	v := NewValidator()
	result := v.CheckSchemaCorrectness(context.Background(), []ToolEntry{})

	if !result.Valid {
		t.Fatal("expected valid=true for empty tool list")
	}
	if len(result.Errors) != 0 {
		t.Fatalf("expected zero errors for empty tool list, got %d", len(result.Errors))
	}
}

func TestCheckSchemaCorrectness_PerToolErrorIsolation(t *testing.T) {
	// Verify that errors from one tool don't affect validation of other tools.
	tools := []ToolEntry{
		{
			ToolName: "valid_tool",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"id": map[string]interface{}{"type": "string"},
				},
				"required": []interface{}{"id"},
			},
		},
		{
			ToolName: "invalid_tool",
			InputSchema: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
				"required":   []interface{}{"missing_field"},
			},
		},
		{
			ToolName: "another_valid_tool",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name": map[string]interface{}{"type": "string"},
				},
				"required": []interface{}{"name"},
			},
		},
	}

	v := NewValidator()
	result := v.CheckSchemaCorrectness(context.Background(), tools)

	if result.Valid {
		t.Fatal("expected valid=false due to invalid_tool")
	}

	// All errors should be from invalid_tool only.
	for _, e := range result.Errors {
		if e.ToolName != "invalid_tool" {
			t.Fatalf("expected errors only from 'invalid_tool', got error from '%s': %s", e.ToolName, e.Message)
		}
	}
}
