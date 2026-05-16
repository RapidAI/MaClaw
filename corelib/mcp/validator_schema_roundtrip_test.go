package mcp

import (
	"testing"

	"pgregory.net/rapid"
)

// TestProperty_SchemaRoundTrip verifies that for any valid tool schema,
// constructSampleArgs(schema) produces arguments that pass ValidateArgs(schema, args)
// with zero validation errors.
//
// Requirements validated: 14.3
func TestProperty_SchemaRoundTrip(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		schema := drawValidJSONSchema(t)
		args := constructSampleArgs(schema)
		errs := ValidateArgs(schema, args)
		if len(errs) > 0 {
			t.Fatalf("constructSampleArgs produced args that fail ValidateArgs:\n  schema: %v\n  args: %v\n  errors: %v", schema, args, errs)
		}
	})
}

// --- Generators ---

// drawValidJSONSchema draws a random valid JSON Schema object with various
// property types and a subset of them marked as required.
func drawValidJSONSchema(t *rapid.T) map[string]interface{} {
	numProps := rapid.IntRange(1, 8).Draw(t, "numProps")
	properties := make(map[string]interface{})
	propNames := make([]string, 0, numProps)

	for i := 0; i < numProps; i++ {
		name := rapid.StringMatching(`[a-z][a-z0-9_]{1,12}`).Draw(t, "propName")
		// Ensure unique property names by appending suffix if collision.
		if _, exists := properties[name]; exists {
			name = name + rapid.StringMatching(`[a-z]{2,4}`).Draw(t, "suffix")
		}
		propDef := drawPropertyDefinition(t)
		properties[name] = propDef
		propNames = append(propNames, name)
	}

	// Select a random subset of properties as required.
	numRequired := rapid.IntRange(0, len(propNames)).Draw(t, "numRequired")
	required := make([]interface{}, 0, numRequired)
	for i := 0; i < numRequired && i < len(propNames); i++ {
		required = append(required, propNames[i])
	}

	schema := map[string]interface{}{
		"type":       "object",
		"properties": properties,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

// drawPropertyDefinition draws a random JSON Schema property definition
// with one of the supported types: string, number, integer, boolean, array, object.
func drawPropertyDefinition(t *rapid.T) map[string]interface{} {
	propType := rapid.SampledFrom([]string{
		"string", "number", "integer", "boolean", "array", "object",
	}).Draw(t, "type")

	propDef := map[string]interface{}{
		"type": propType,
	}

	// Optionally add enum for string type.
	if propType == "string" {
		hasEnum := rapid.Bool().Draw(t, "hasEnum")
		if hasEnum {
			numEnumValues := rapid.IntRange(1, 5).Draw(t, "numEnumValues")
			enumValues := make([]interface{}, numEnumValues)
			for i := 0; i < numEnumValues; i++ {
				enumValues[i] = rapid.StringMatching(`[a-z]{3,8}`).Draw(t, "enumVal")
			}
			propDef["enum"] = enumValues
		}
	}

	// Optionally add a default value (only for types that have meaningful defaults).
	if propType == "string" || propType == "number" || propType == "integer" || propType == "boolean" {
		hasDefault := rapid.Bool().Draw(t, "hasDefault")
		if hasDefault {
			switch propType {
			case "string":
				propDef["default"] = rapid.StringMatching(`[a-zA-Z0-9]{1,10}`).Draw(t, "strDefault")
			case "number":
				propDef["default"] = rapid.Float64Range(-1000, 1000).Draw(t, "numDefault")
			case "integer":
				propDef["default"] = float64(rapid.IntRange(-100, 100).Draw(t, "intDefault"))
			case "boolean":
				propDef["default"] = rapid.Bool().Draw(t, "boolDefault")
			}
		}
	}

	return propDef
}
