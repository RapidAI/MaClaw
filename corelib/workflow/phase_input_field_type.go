package workflow

import "strings"

func normalizePhaseInputFieldType(fieldType string) string {
	return strings.ToLower(strings.TrimSpace(fieldType))
}

func isSupportedPhaseInputFieldType(fieldType string) bool {
	switch normalizePhaseInputFieldType(fieldType) {
	case "", "text", "textarea", "number", "date", "datetime",
		"select", "multiselect", "boolean", "file", "directory", "hidden",
		"user_ref", "department_ref", "business_ref",
		"object_form", "array_table":
		return true
	default:
		return false
	}
}

func isStringPhaseInputFieldType(fieldType string) bool {
	switch normalizePhaseInputFieldType(fieldType) {
	case "", "text", "textarea", "date", "datetime", "file", "directory", "hidden",
		"user_ref", "department_ref", "business_ref":
		return true
	default:
		return false
	}
}
