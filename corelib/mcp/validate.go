package mcp

import (
	"fmt"
	"strings"
)

// ValidationError represents a single parameter validation failure.
type ValidationError struct {
	Param    string // parameter name
	Code     string // "missing_required", "type_mismatch", "invalid_enum"
	Expected string // expected type or enum values
	Actual   string // actual type or value provided
	Message  string // human-readable error message
}

// ValidateArgs validates tool arguments against the tool's InputSchema.
// Returns nil if validation passes or schema is nil/empty (graceful degradation).
// Returns a slice of ValidationError for each violation found.
// Never panics — internal errors are caught via recover and result in nil return (fail-open).
func ValidateArgs(schema map[string]interface{}, args map[string]interface{}) (errs []ValidationError) {
	// Fail-open: catch any panics and return nil.
	defer func() {
		if r := recover(); r != nil {
			errs = nil
		}
	}()

	// Graceful degradation: nil or empty schema means nothing to validate.
	if len(schema) == 0 {
		return nil
	}

	// 1. Required parameters check.
	if reqRaw, ok := schema["required"]; ok {
		if reqSlice, ok := reqRaw.([]interface{}); ok {
			properties, _ := schema["properties"].(map[string]interface{})
			for _, r := range reqSlice {
				name, ok := r.(string)
				if !ok {
					continue
				}
				if _, present := args[name]; !present {
					expected := expectedTypeFromProperties(properties, name)
					errs = append(errs, ValidationError{
						Param:    name,
						Code:     "missing_required",
						Expected: expected,
						Actual:   "",
						Message:  fmt.Sprintf("Required parameter '%s' (%s) is missing", name, expected),
					})
				}
			}
		}
	}

	// 2. Type checking + 3. Enum validation.
	properties, _ := schema["properties"].(map[string]interface{})
	if properties == nil {
		return nilIfEmpty(errs)
	}

	for paramName, propRaw := range properties {
		argVal, present := args[paramName]
		if !present {
			continue
		}

		propDef, ok := propRaw.(map[string]interface{})
		if !ok {
			continue
		}

		// Type checking.
		declaredType, _ := propDef["type"].(string)
		if declaredType != "" {
			actualType := jsonTypeOf(argVal)
			if !typesMatch(declaredType, actualType) {
				errs = append(errs, ValidationError{
					Param:    paramName,
					Code:     "type_mismatch",
					Expected: declaredType,
					Actual:   actualType,
					Message:  fmt.Sprintf("Parameter '%s' expects type '%s' but got '%s'", paramName, declaredType, actualType),
				})
				// Skip enum check if type doesn't match.
				continue
			}
		}

		// Enum validation (only for string parameters).
		if enumRaw, ok := propDef["enum"]; ok {
			if enumSlice, ok := enumRaw.([]interface{}); ok && len(enumSlice) > 0 {
				strVal, isStr := argVal.(string)
				if isStr && !enumContains(enumSlice, strVal) {
					errs = append(errs, ValidationError{
						Param:    paramName,
						Code:     "invalid_enum",
						Expected: formatEnumValues(enumSlice),
						Actual:   strVal,
						Message:  fmt.Sprintf("Parameter '%s' value '%s' is not in allowed values: %s", paramName, strVal, formatEnumValues(enumSlice)),
					})
				}
			}
		}
	}

	return nilIfEmpty(errs)
}

// jsonTypeOf returns the JSON type name for a Go value.
func jsonTypeOf(v interface{}) string {
	if v == nil {
		return "null"
	}
	switch v.(type) {
	case string:
		return "string"
	case float64:
		return "number"
	case bool:
		return "boolean"
	case map[string]interface{}:
		return "object"
	case []interface{}:
		return "array"
	default:
		return fmt.Sprintf("%T", v)
	}
}

// typesMatch checks if the actual JSON type matches the declared schema type.
// float64 matches both "number" and "integer".
func typesMatch(declared, actual string) bool {
	if declared == actual {
		return true
	}
	// Go's float64 can represent both JSON number and integer.
	if actual == "number" && declared == "integer" {
		return true
	}
	return false
}

// expectedTypeFromProperties extracts the declared type for a parameter from the
// properties map. Returns "unknown" if not found.
func expectedTypeFromProperties(properties map[string]interface{}, name string) string {
	if properties == nil {
		return "unknown"
	}
	propRaw, ok := properties[name]
	if !ok {
		return "unknown"
	}
	propDef, ok := propRaw.(map[string]interface{})
	if !ok {
		return "unknown"
	}
	t, _ := propDef["type"].(string)
	if t == "" {
		return "unknown"
	}
	return t
}

// enumContains checks if a string value is present in an enum slice.
func enumContains(enumSlice []interface{}, val string) bool {
	for _, e := range enumSlice {
		if s, ok := e.(string); ok && s == val {
			return true
		}
	}
	return false
}

// formatEnumValues formats an enum slice as a bracketed comma-separated string.
func formatEnumValues(enumSlice []interface{}) string {
	var vals []string
	for _, e := range enumSlice {
		if s, ok := e.(string); ok {
			vals = append(vals, s)
		}
	}
	if len(vals) == 0 {
		return "[]"
	}
	return "[" + strings.Join(vals, ", ") + "]"
}

// nilIfEmpty returns nil if the slice is empty, otherwise returns the slice.
func nilIfEmpty(errs []ValidationError) []ValidationError {
	if len(errs) == 0 {
		return nil
	}
	return errs
}
