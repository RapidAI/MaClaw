package main

import "strings"

type registeredToolAgentViewWidgetKind string

const (
	registeredToolAgentViewUnknown        registeredToolAgentViewWidgetKind = ""
	registeredToolAgentViewResourcePicker registeredToolAgentViewWidgetKind = "resource_picker"
	registeredToolAgentViewFieldMapper    registeredToolAgentViewWidgetKind = "field_mapper"
)

func normalizeRegisteredToolAgentViewKind(kind string) registeredToolAgentViewWidgetKind {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "resource_picker", "resource-picker", "resourcepicker":
		return registeredToolAgentViewResourcePicker
	case "field_mapper", "field-mapper", "fieldmapper":
		return registeredToolAgentViewFieldMapper
	default:
		return registeredToolAgentViewUnknown
	}
}

func (kind registeredToolAgentViewWidgetKind) String() string {
	return string(kind)
}

type registeredToolSchemaPropertyName string

const (
	registeredToolSchemaPropertyRequired             registeredToolSchemaPropertyName = "required"
	registeredToolSchemaPropertyType                 registeredToolSchemaPropertyName = "type"
	registeredToolSchemaPropertyProperties           registeredToolSchemaPropertyName = "properties"
	registeredToolSchemaPropertyDependentRequired    registeredToolSchemaPropertyName = "dependentRequired"
	registeredToolSchemaPropertyDependencies         registeredToolSchemaPropertyName = "dependencies"
	registeredToolSchemaPropertyAdditionalProperties registeredToolSchemaPropertyName = "additionalProperties"
)

func isRegisteredToolSchemaContainerProperty(name string) bool {
	switch registeredToolSchemaPropertyName(strings.TrimSpace(name)) {
	case registeredToolSchemaPropertyRequired,
		registeredToolSchemaPropertyType,
		registeredToolSchemaPropertyProperties,
		registeredToolSchemaPropertyDependentRequired,
		registeredToolSchemaPropertyDependencies,
		registeredToolSchemaPropertyAdditionalProperties:
		return true
	default:
		return false
	}
}

type registeredToolJSONSchemaType string

const (
	registeredToolJSONSchemaUnknown registeredToolJSONSchemaType = ""
	registeredToolJSONSchemaNumber  registeredToolJSONSchemaType = "number"
	registeredToolJSONSchemaInteger registeredToolJSONSchemaType = "integer"
	registeredToolJSONSchemaString  registeredToolJSONSchemaType = "string"
	registeredToolJSONSchemaBoolean registeredToolJSONSchemaType = "boolean"
	registeredToolJSONSchemaArray   registeredToolJSONSchemaType = "array"
	registeredToolJSONSchemaObject  registeredToolJSONSchemaType = "object"
	registeredToolJSONSchemaDate    registeredToolJSONSchemaType = "date"
)

func normalizeRegisteredToolJSONSchemaType(kind string) registeredToolJSONSchemaType {
	switch registeredToolJSONSchemaType(strings.ToLower(strings.TrimSpace(kind))) {
	case registeredToolJSONSchemaNumber:
		return registeredToolJSONSchemaNumber
	case registeredToolJSONSchemaInteger:
		return registeredToolJSONSchemaInteger
	case registeredToolJSONSchemaString:
		return registeredToolJSONSchemaString
	case registeredToolJSONSchemaBoolean:
		return registeredToolJSONSchemaBoolean
	case registeredToolJSONSchemaArray:
		return registeredToolJSONSchemaArray
	case registeredToolJSONSchemaObject:
		return registeredToolJSONSchemaObject
	case registeredToolJSONSchemaDate:
		return registeredToolJSONSchemaDate
	default:
		return registeredToolJSONSchemaUnknown
	}
}

func (kind registeredToolJSONSchemaType) IsObjectLike() bool {
	return kind == registeredToolJSONSchemaObject || kind == registeredToolJSONSchemaArray
}
