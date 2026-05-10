package main

import "strings"

func normalizeMISDatasetAgentViewFieldType(value string) agentViewFieldType {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "number", "integer", "float", "decimal":
		return agentViewFieldTypeNumber
	case "boolean", "bool":
		return agentViewFieldTypeBoolean
	case "date":
		return agentViewFieldTypeDate
	case "datetime", "timestamp":
		return agentViewFieldType("datetime")
	case "file_ref":
		return agentViewFieldTypeFile
	case "person_ref", "user_ref":
		return agentViewFieldTypeUserRef
	case "department_ref":
		return agentViewFieldTypeDepartmentRef
	case "record_ref", "org_ref":
		return agentViewFieldTypeBusinessRef
	case "array", "list", "items", "line_items", "object_array":
		return agentViewFieldTypeArrayTable
	case "object", "map", "struct", "json":
		return agentViewFieldTypeObjectForm
	default:
		return agentViewFieldTypeText
	}
}

func normalizeMISTableColumnAgentViewFieldType(value string) agentViewFieldType {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "number", "integer", "float", "decimal":
		return agentViewFieldTypeNumber
	case "boolean", "bool":
		return agentViewFieldTypeBoolean
	case "date":
		return agentViewFieldTypeDate
	case "select", "enum":
		return agentViewFieldTypeSelect
	default:
		return agentViewFieldTypeText
	}
}
