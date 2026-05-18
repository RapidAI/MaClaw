package workflow

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sync"
	"time"
)

// FieldType represents the data type of a form field.
type FieldType string

const (
	FieldText     FieldType = "text"
	FieldNumber   FieldType = "number"
	FieldDate     FieldType = "date"
	FieldSelect   FieldType = "select"
	FieldFile     FieldType = "file"
	FieldTextarea FieldType = "textarea"
	FieldBoolean  FieldType = "boolean"
)

// FormFieldSchema defines a single field in the form.
type FormFieldSchema struct {
	Name      string   `json:"name"`
	Label     string   `json:"label"`
	Type      FieldType `json:"type"`
	Required  bool     `json:"required"`
	MaxLength int      `json:"max_length,omitempty"`
	MinValue  *float64 `json:"min_value,omitempty"`
	MaxValue  *float64 `json:"max_value,omitempty"`
	Options   []string `json:"options,omitempty"`
	Pattern   string   `json:"pattern,omitempty"`
}

// FormValidationError represents a single field validation failure.
type FormValidationError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// FormValidator validates submitted form data against a workflow's Form_Node schema.
type FormValidator struct {
	patternCache sync.Map // map[string]*regexp.Regexp
}

// maxPatternLength is the maximum allowed length for regex patterns to mitigate ReDoS risk.
const maxPatternLength = 500

// Validate checks formData against the schema, returning all validation errors.
// Returns nil if validation passes.
func (v *FormValidator) Validate(formData map[string]interface{}, schema []FormFieldSchema) []FormValidationError {
	var errors []FormValidationError

	for _, field := range schema {
		value, exists := formData[field.Name]

		// Check required fields
		if field.Required && (!exists || value == nil) {
			errors = append(errors, FormValidationError{
				Field:   field.Name,
				Message: fmt.Sprintf("required field missing: %s", field.Label),
			})
			continue
		}

		// Skip validation for absent optional fields
		if !exists || value == nil {
			continue
		}

		// Validate by field type
		switch field.Type {
		case FieldText, FieldTextarea:
			errors = append(errors, v.validateText(field, value)...)
		case FieldNumber:
			errors = append(errors, v.validateNumber(field, value)...)
		case FieldDate:
			errors = append(errors, v.validateDate(field, value)...)
		case FieldSelect:
			errors = append(errors, v.validateSelect(field, value)...)
		case FieldBoolean:
			errors = append(errors, v.validateBoolean(field, value)...)
		case FieldFile:
			errors = append(errors, v.validateFile(field, value)...)
		}
	}

	return errors
}

func (v *FormValidator) validateText(field FormFieldSchema, value interface{}) []FormValidationError {
	var errors []FormValidationError

	str, ok := value.(string)
	if !ok {
		errors = append(errors, FormValidationError{
			Field:   field.Name,
			Message: fmt.Sprintf("expected string for field %s, got %T", field.Label, value),
		})
		return errors
	}

	// Check MaxLength
	if field.MaxLength > 0 && len([]rune(str)) > field.MaxLength {
		errors = append(errors, FormValidationError{
			Field:   field.Name,
			Message: fmt.Sprintf("field %s exceeds maximum length of %d", field.Label, field.MaxLength),
		})
	}

	// Check Pattern
	if field.Pattern != "" {
		if len(field.Pattern) > maxPatternLength {
			errors = append(errors, FormValidationError{
				Field:   field.Name,
				Message: fmt.Sprintf("pattern too long for field %s (max %d chars)", field.Label, maxPatternLength),
			})
			return errors
		}
		re := v.getOrCompilePattern(field.Pattern)
		if re == nil {
			errors = append(errors, FormValidationError{
				Field:   field.Name,
				Message: fmt.Sprintf("invalid pattern for field %s", field.Label),
			})
		} else if !re.MatchString(str) {
			errors = append(errors, FormValidationError{
				Field:   field.Name,
				Message: fmt.Sprintf("field %s does not match required pattern", field.Label),
			})
		}
	}

	return errors
}

// getOrCompilePattern returns a compiled regex for the given pattern, using a cache
// to avoid recompilation. Returns nil if the pattern is invalid.
func (v *FormValidator) getOrCompilePattern(pattern string) *regexp.Regexp {
	if cached, ok := v.patternCache.Load(pattern); ok {
		return cached.(*regexp.Regexp)
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil
	}
	v.patternCache.Store(pattern, re)
	return re
}

func (v *FormValidator) validateNumber(field FormFieldSchema, value interface{}) []FormValidationError {
	var errors []FormValidationError

	var num float64
	switch n := value.(type) {
	case float64:
		num = n
	case float32:
		num = float64(n)
	case int:
		num = float64(n)
	case int64:
		num = float64(n)
	case json.Number:
		f, err := n.Float64()
		if err != nil {
			errors = append(errors, FormValidationError{
				Field:   field.Name,
				Message: fmt.Sprintf("expected number for field %s, got invalid number", field.Label),
			})
			return errors
		}
		num = f
	default:
		errors = append(errors, FormValidationError{
			Field:   field.Name,
			Message: fmt.Sprintf("expected number for field %s, got %T", field.Label, value),
		})
		return errors
	}

	// Check MinValue
	if field.MinValue != nil && num < *field.MinValue {
		errors = append(errors, FormValidationError{
			Field:   field.Name,
			Message: fmt.Sprintf("field %s value %v is less than minimum %v", field.Label, num, *field.MinValue),
		})
	}

	// Check MaxValue
	if field.MaxValue != nil && num > *field.MaxValue {
		errors = append(errors, FormValidationError{
			Field:   field.Name,
			Message: fmt.Sprintf("field %s value %v exceeds maximum %v", field.Label, num, *field.MaxValue),
		})
	}

	return errors
}

func (v *FormValidator) validateDate(field FormFieldSchema, value interface{}) []FormValidationError {
	var errors []FormValidationError

	str, ok := value.(string)
	if !ok {
		errors = append(errors, FormValidationError{
			Field:   field.Name,
			Message: fmt.Sprintf("expected date string for field %s, got %T", field.Label, value),
		})
		return errors
	}

	// Try common date formats
	dateFormats := []string{
		"2006-01-02",
		"2006-01-02T15:04:05Z",
		"2006-01-02T15:04:05-07:00",
		"2006-01-02 15:04:05",
		time.RFC3339,
	}

	valid := false
	for _, format := range dateFormats {
		if _, err := time.Parse(format, str); err == nil {
			valid = true
			break
		}
	}

	if !valid {
		errors = append(errors, FormValidationError{
			Field:   field.Name,
			Message: fmt.Sprintf("field %s has invalid date format: %s", field.Label, str),
		})
	}

	return errors
}

func (v *FormValidator) validateSelect(field FormFieldSchema, value interface{}) []FormValidationError {
	var errors []FormValidationError

	str, ok := value.(string)
	if !ok {
		errors = append(errors, FormValidationError{
			Field:   field.Name,
			Message: fmt.Sprintf("expected string for select field %s, got %T", field.Label, value),
		})
		return errors
	}

	if len(field.Options) > 0 {
		found := false
		for _, opt := range field.Options {
			if opt == str {
				found = true
				break
			}
		}
		if !found {
			errors = append(errors, FormValidationError{
				Field:   field.Name,
				Message: fmt.Sprintf("field %s value %q is not a valid option", field.Label, str),
			})
		}
	}

	return errors
}

func (v *FormValidator) validateBoolean(field FormFieldSchema, value interface{}) []FormValidationError {
	var errors []FormValidationError

	if _, ok := value.(bool); !ok {
		errors = append(errors, FormValidationError{
			Field:   field.Name,
			Message: fmt.Sprintf("expected boolean for field %s, got %T", field.Label, value),
		})
	}

	return errors
}

func (v *FormValidator) validateFile(field FormFieldSchema, value interface{}) []FormValidationError {
	var errors []FormValidationError

	// File fields accept a string (file path or URL) or a map with file metadata
	switch value.(type) {
	case string:
		// Valid: file path or URL as string
	case map[string]interface{}:
		// Valid: file metadata object
	default:
		errors = append(errors, FormValidationError{
			Field:   field.Name,
			Message: fmt.Sprintf("expected string or object for file field %s, got %T", field.Label, value),
		})
	}

	return errors
}

// FormNodeConfig represents the configuration of a Form_Node in the workflow graph.
type FormNodeConfig struct {
	Fields []FormFieldSchema `json:"fields"`
}

// ExtractFormSchema extracts the FormFieldSchema from a workflow's Form_Node.
// If no Form_Node exists in the graph, returns an error.
func ExtractFormSchema(graph *WorkflowGraph) ([]FormFieldSchema, error) {
	if graph == nil {
		return nil, fmt.Errorf("workflow graph is nil")
	}

	for _, node := range graph.Nodes {
		if node.Type == NodeForm {
			var config FormNodeConfig
			if err := json.Unmarshal(node.Config, &config); err != nil {
				return nil, fmt.Errorf("failed to parse Form_Node config: %w", err)
			}
			return config.Fields, nil
		}
	}

	return nil, fmt.Errorf("no Form_Node found in workflow graph")
}
