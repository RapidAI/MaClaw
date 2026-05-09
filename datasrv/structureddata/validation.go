package structureddata

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

var supportedFieldTypes = map[string]struct{}{
	"string": {}, "number": {}, "integer": {}, "float": {}, "decimal": {}, "money": {}, "boolean": {}, "array": {}, "object": {}, "date": {}, "datetime": {}, "json": {},
	"record_ref": {}, "person_ref": {}, "org_ref": {}, "file_ref": {},
}

func validateFieldDefinition(field FieldDefinition) error {
	fieldType := strings.ToLower(strings.TrimSpace(field.Type))
	if fieldType == "" {
		return fmt.Errorf("%w: field type is required", ErrInvalidInput)
	}
	if _, ok := supportedFieldTypes[fieldType]; !ok {
		return fmt.Errorf("%w: unsupported field type %s", ErrInvalidInput, fieldType)
	}
	return nil
}

func validateRecordData(fields []FieldDefinition, data map[string]any) error {
	if data == nil {
		data = map[string]any{}
	}
	for _, field := range fields {
		key := strings.TrimSpace(field.Key)
		if key == "" {
			continue
		}
		value, exists := data[key]
		if field.Required && (!exists || isEmptyValue(value)) {
			return fmt.Errorf("%w: required field %s is missing", ErrInvalidInput, key)
		}
		if !exists || value == nil {
			continue
		}
		if err := validateFieldValue(field, value); err != nil {
			return err
		}
	}
	return nil
}

func validateFieldValue(field FieldDefinition, value any) error {
	key := strings.TrimSpace(field.Key)
	fieldType := strings.ToLower(strings.TrimSpace(field.Type))
	switch fieldType {
	case "string":
		if _, ok := value.(string); !ok {
			return fmt.Errorf("%w: field %s must be string", ErrInvalidInput, key)
		}
	case "number", "float", "decimal", "money":
		if _, ok := numberFromAny(value); !ok {
			return fmt.Errorf("%w: field %s must be number", ErrInvalidInput, key)
		}
	case "integer":
		if number, ok := numberFromAny(value); !ok || number != float64(int64(number)) {
			return fmt.Errorf("%w: field %s must be integer", ErrInvalidInput, key)
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("%w: field %s must be boolean", ErrInvalidInput, key)
		}
	case "array":
		if _, ok := value.([]any); !ok {
			return fmt.Errorf("%w: field %s must be array", ErrInvalidInput, key)
		}
	case "object":
		if _, ok := value.(map[string]any); !ok {
			return fmt.Errorf("%w: field %s must be object", ErrInvalidInput, key)
		}
	case "date", "datetime":
		text, ok := value.(string)
		if !ok || !validDateString(text) {
			return fmt.Errorf("%w: field %s must be date string", ErrInvalidInput, key)
		}
	case "record_ref", "person_ref", "org_ref", "file_ref":
		text, ok := value.(string)
		if !ok || strings.TrimSpace(text) == "" {
			return fmt.Errorf("%w: field %s must be reference string", ErrInvalidInput, key)
		}
	case "json", "":
		// Any JSON value is accepted.
	default:
		return fmt.Errorf("%w: unsupported field type %s", ErrInvalidInput, fieldType)
	}
	if !matchesEnum(field, value) {
		return fmt.Errorf("%w: field %s is not in allowed enum", ErrInvalidInput, key)
	}
	return nil
}

func isEmptyValue(value any) bool {
	if value == nil {
		return true
	}
	if text, ok := value.(string); ok {
		return strings.TrimSpace(text) == ""
	}
	return false
}

func validDateString(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02", "2006-01-02 15:04:05"} {
		if _, err := time.Parse(layout, value); err == nil {
			return true
		}
	}
	return false
}

func matchesEnum(field FieldDefinition, value any) bool {
	if len(field.Config) == 0 {
		return true
	}
	raw, ok := field.Config["enum"]
	if !ok {
		raw, ok = field.Config["values"]
	}
	if !ok {
		return true
	}
	values, ok := raw.([]any)
	if !ok {
		return true
	}
	for _, allowed := range values {
		if comparableJSONValue(allowed) == comparableJSONValue(value) {
			return true
		}
	}
	return false
}

func comparableJSONValue(value any) string {
	switch v := value.(type) {
	case json.Number:
		return v.String()
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case float32:
		return strconv.FormatFloat(float64(v), 'f', -1, 32)
	default:
		return fmt.Sprint(value)
	}
}

func validateRecordDataResult(datasetID string, fields []FieldDefinition, data map[string]any) ValidateRecordResult {
	result := ValidateRecordResult{Valid: true, DatasetID: strings.TrimSpace(datasetID), FieldCount: len(fields)}
	defined := map[string]struct{}{}
	for _, field := range fields {
		if strings.TrimSpace(field.Key) != "" {
			defined[strings.TrimSpace(field.Key)] = struct{}{}
		}
	}
	for key := range data {
		if _, ok := defined[key]; !ok {
			result.UnknownFields = append(result.UnknownFields, key)
		}
	}
	if err := validateRecordData(fields, data); err != nil {
		result.Valid = false
		result.Errors = append(result.Errors, strings.TrimPrefix(err.Error(), ErrInvalidInput.Error()+": "))
	}
	appendDatasetDataErrors(&result, result.DatasetID, data)
	sort.Strings(result.UnknownFields)
	return result
}

func appendDatasetDataErrors(result *ValidateRecordResult, datasetID string, data map[string]any) {
	if result == nil {
		return
	}
	if errors := datasetDataErrors(datasetID, data); len(errors) > 0 {
		result.Valid = false
		result.Errors = append(result.Errors, errors...)
	}
}

// datasetValidationRules maps dataset IDs to custom cross-field validation
// functions. New domain-specific validations should be registered here rather
// than adding cases to a switch statement.
var datasetValidationRules = map[string]func(map[string]any) []string{
	"finance.vouchers": financeVoucherValidationErrors,
}

func datasetDataErrors(datasetID string, data map[string]any) []string {
	datasetID = strings.TrimSpace(datasetID)
	if fn, ok := datasetValidationRules[datasetID]; ok {
		return fn(data)
	}
	return nil
}
