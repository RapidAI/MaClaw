package main

import "strings"

type agentViewFieldType string

const (
	agentViewFieldTypeUnknown       agentViewFieldType = ""
	agentViewFieldTypeText          agentViewFieldType = "text"
	agentViewFieldTypeTextarea      agentViewFieldType = "textarea"
	agentViewFieldTypeNumber        agentViewFieldType = "number"
	agentViewFieldTypeBoolean       agentViewFieldType = "boolean"
	agentViewFieldTypeSelect        agentViewFieldType = "select"
	agentViewFieldTypeMultiSelect   agentViewFieldType = "multiselect"
	agentViewFieldTypeDate          agentViewFieldType = "date"
	agentViewFieldTypeFile          agentViewFieldType = "file"
	agentViewFieldTypeDirectory     agentViewFieldType = "directory"
	agentViewFieldTypeArrayTable    agentViewFieldType = "array_table"
	agentViewFieldTypeObjectForm    agentViewFieldType = "object_form"
	agentViewFieldTypeBusinessRef   agentViewFieldType = "business_ref"
	agentViewFieldTypeUserRef       agentViewFieldType = "user_ref"
	agentViewFieldTypeDepartmentRef agentViewFieldType = "department_ref"
	agentViewFieldTypeHidden        agentViewFieldType = "hidden"
	agentViewFieldTypeFieldMapper   agentViewFieldType = "field_mapper"
)

func normalizeAgentViewFieldType(fieldType string) agentViewFieldType {
	switch agentViewFieldType(strings.ToLower(strings.TrimSpace(fieldType))) {
	case agentViewFieldTypeText:
		return agentViewFieldTypeText
	case agentViewFieldTypeTextarea:
		return agentViewFieldTypeTextarea
	case agentViewFieldTypeNumber:
		return agentViewFieldTypeNumber
	case agentViewFieldTypeBoolean:
		return agentViewFieldTypeBoolean
	case agentViewFieldTypeSelect:
		return agentViewFieldTypeSelect
	case agentViewFieldTypeMultiSelect:
		return agentViewFieldTypeMultiSelect
	case agentViewFieldTypeDate:
		return agentViewFieldTypeDate
	case agentViewFieldTypeFile:
		return agentViewFieldTypeFile
	case agentViewFieldTypeDirectory:
		return agentViewFieldTypeDirectory
	case agentViewFieldTypeArrayTable:
		return agentViewFieldTypeArrayTable
	case agentViewFieldTypeObjectForm:
		return agentViewFieldTypeObjectForm
	case agentViewFieldTypeBusinessRef:
		return agentViewFieldTypeBusinessRef
	case agentViewFieldTypeUserRef:
		return agentViewFieldTypeUserRef
	case agentViewFieldTypeDepartmentRef:
		return agentViewFieldTypeDepartmentRef
	case agentViewFieldTypeHidden:
		return agentViewFieldTypeHidden
	case agentViewFieldTypeFieldMapper:
		return agentViewFieldTypeFieldMapper
	default:
		return agentViewFieldTypeUnknown
	}
}

func (fieldType agentViewFieldType) String() string {
	return string(fieldType)
}

func (fieldType agentViewFieldType) UsesOptions() bool {
	return fieldType == agentViewFieldTypeSelect
}

func (fieldType agentViewFieldType) IsReferenceSelect() bool {
	switch fieldType {
	case agentViewFieldTypeSelect, agentViewFieldTypeBusinessRef, agentViewFieldTypeUserRef, agentViewFieldTypeDepartmentRef:
		return true
	default:
		return false
	}
}

func (fieldType agentViewFieldType) IsResourceReference() bool {
	switch fieldType {
	case agentViewFieldTypeBusinessRef, agentViewFieldTypeUserRef, agentViewFieldTypeDepartmentRef:
		return true
	default:
		return false
	}
}

func (fieldType agentViewFieldType) SuppressesPlaceholder() bool {
	return fieldType == agentViewFieldTypeSelect || fieldType == agentViewFieldTypeBoolean
}
