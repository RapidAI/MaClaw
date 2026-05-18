// Package condeval provides shared condition evaluation logic for workflow
// rule engines. Both the hub workflow executor (condition branch nodes) and
// the GUI approval rule engine delegate to this package.
package condeval

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Operator constants for condition evaluation.
const (
	OpEquals     = "equals"
	OpNotEquals  = "not_equals"
	OpGreaterThan = "greater_than"
	OpLessThan   = "less_than"
	OpContains   = "contains"
	OpInList     = "in_list"
	OpNotInList  = "not_in_list"
	OpIsEmpty    = "is_empty"
	OpIsNotEmpty = "is_not_empty"
)

// ResolveField extracts a value from nested map data using dot-notation path.
// Max depth is 3 (e.g., "request.details.amount").
// Returns (value, true) if found, (nil, false) if missing or null.
func ResolveField(data map[string]interface{}, fieldPath string) (interface{}, bool) {
	parts := strings.Split(fieldPath, ".")
	if len(parts) == 0 || len(parts) > 3 {
		return nil, false
	}

	var current interface{} = data
	for _, part := range parts {
		m, ok := current.(map[string]interface{})
		if !ok {
			return nil, false
		}
		val, exists := m[part]
		if !exists {
			return nil, false
		}
		if val == nil {
			return nil, false
		}
		current = val
	}
	return current, true
}

// EvaluateCondition checks whether a single condition (field + operator + value)
// matches against the given data map.
// Returns false if the field is missing/null (condition treated as not matched),
// except for is_empty which returns true for missing fields.
func EvaluateCondition(field string, operator string, value interface{}, data map[string]interface{}) bool {
	if data == nil {
		data = make(map[string]interface{})
	}

	// Handle is_empty and is_not_empty specially for missing fields
	if operator == OpIsEmpty {
		val, found := ResolveField(data, field)
		if !found {
			return true // missing/null is considered empty
		}
		return IsEmpty(val)
	}
	if operator == OpIsNotEmpty {
		val, found := ResolveField(data, field)
		if !found {
			return false // missing/null is not "not empty"
		}
		return !IsEmpty(val)
	}

	// For all other operators, missing/null field means condition not matched
	fieldVal, found := ResolveField(data, field)
	if !found {
		return false
	}

	switch operator {
	case OpEquals:
		return Equals(fieldVal, value)
	case OpNotEquals:
		return !Equals(fieldVal, value)
	case OpGreaterThan:
		return CompareNumeric(fieldVal, value) > 0
	case OpLessThan:
		return CompareNumeric(fieldVal, value) < 0
	case OpContains:
		return Contains(fieldVal, value)
	case OpInList:
		return InList(fieldVal, value)
	case OpNotInList:
		return !InList(fieldVal, value)
	default:
		return false
	}
}

// IsEmpty checks if a value is considered empty.
func IsEmpty(val interface{}) bool {
	if val == nil {
		return true
	}
	switch v := val.(type) {
	case string:
		return v == ""
	case []interface{}:
		return len(v) == 0
	case map[string]interface{}:
		return len(v) == 0
	default:
		return false
	}
}

// Equals checks equality between field value and condition value.
// Both values are normalized to string representation for comparison.
func Equals(fieldVal, condVal interface{}) bool {
	fStr := fmt.Sprintf("%v", fieldVal)
	cStr := fmt.Sprintf("%v", condVal)
	return fStr == cStr
}

// CompareNumeric compares two values numerically.
// Returns -1 if fieldVal < condVal, 0 if equal, 1 if fieldVal > condVal.
// Returns 0 (no match for GT/LT) if either value is not numeric.
func CompareNumeric(fieldVal, condVal interface{}) int {
	fNum, fOk := ToFloat64(fieldVal)
	cNum, cOk := ToFloat64(condVal)
	if !fOk || !cOk {
		return 0
	}
	if fNum < cNum {
		return -1
	}
	if fNum > cNum {
		return 1
	}
	return 0
}

// ToFloat64 converts a value to float64 if possible.
func ToFloat64(val interface{}) (float64, bool) {
	switch v := val.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int32:
		return float64(v), true
	case int64:
		return float64(v), true
	case json.Number:
		f, err := v.Float64()
		return f, err == nil
	default:
		return 0, false
	}
}

// Contains checks if the field value contains the condition value.
// For strings: substring match.
// For slices: element membership.
func Contains(fieldVal, condVal interface{}) bool {
	if fStr, ok := fieldVal.(string); ok {
		cStr := fmt.Sprintf("%v", condVal)
		return strings.Contains(fStr, cStr)
	}
	if fSlice, ok := fieldVal.([]interface{}); ok {
		cStr := fmt.Sprintf("%v", condVal)
		for _, item := range fSlice {
			if fmt.Sprintf("%v", item) == cStr {
				return true
			}
		}
	}
	return false
}

// InList checks if the field value is in the condition value list.
func InList(fieldVal, condVal interface{}) bool {
	list, ok := condVal.([]interface{})
	if !ok {
		return false
	}
	fStr := fmt.Sprintf("%v", fieldVal)
	for _, item := range list {
		if fmt.Sprintf("%v", item) == fStr {
			return true
		}
	}
	return false
}
