package structureddata

import (
	"context"
	"fmt"
	"strings"
)

func fieldIsUnique(field FieldDefinition) bool {
	if len(field.Config) == 0 {
		return false
	}
	raw, ok := field.Config["unique"]
	if !ok {
		return false
	}
	switch v := raw.(type) {
	case bool:
		return v
	case string:
		return strings.EqualFold(strings.TrimSpace(v), "true") || strings.TrimSpace(v) == "1"
	case float64:
		return v != 0
	case int:
		return v != 0
	default:
		return false
	}
}

func uniqueFieldDefinitions(fields []FieldDefinition) []FieldDefinition {
	out := []FieldDefinition{}
	for _, field := range fields {
		if fieldIsUnique(field) && strings.TrimSpace(field.Key) != "" {
			out = append(out, field)
		}
	}
	return out
}

func (s *Service) uniqueConstraintErrors(ctx context.Context, p Principal, datasetID string, fields []FieldDefinition, data map[string]any, excludeRecordID string) ([]string, error) {
	uniqueFields := uniqueFieldDefinitions(fields)
	if len(uniqueFields) == 0 || len(data) == 0 {
		return nil, nil
	}
	errors := []string{}
	excludeRecordID = strings.TrimSpace(excludeRecordID)
	for _, field := range uniqueFields {
		key := strings.TrimSpace(field.Key)
		value, exists := data[key]
		if !exists || isEmptyValue(value) {
			continue
		}
		matches, err := s.store.QueryRecords(ctx, p.TenantID, datasetID, QueryRecordsInput{Filter: map[string]any{"field": key, "op": "eq", "value": value}, Limit: 5})
		if err != nil {
			return nil, err
		}
		for _, match := range matches {
			if strings.TrimSpace(match.ID) == excludeRecordID {
				continue
			}
			errors = append(errors, fmt.Sprintf("field %s value %s already exists", key, comparableJSONValue(value)))
			break
		}
	}
	return errors, nil
}

func (s *Service) validateUniqueConstraints(ctx context.Context, p Principal, datasetID string, fields []FieldDefinition, data map[string]any, excludeRecordID string) error {
	errors, err := s.uniqueConstraintErrors(ctx, p, datasetID, fields, data, excludeRecordID)
	if err != nil {
		return err
	}
	if len(errors) > 0 {
		return fmt.Errorf("%w: %s", ErrInvalidInput, strings.Join(errors, "; "))
	}
	return nil
}

func appendBatchUniqueValidationErrors(ctx context.Context, s *Service, p Principal, datasetID string, fields []FieldDefinition, records []BatchRecordInput, validations []BatchRecordValidation) error {
	uniqueFields := uniqueFieldDefinitions(fields)
	if len(uniqueFields) == 0 {
		return nil
	}
	seen := map[string]int{}
	for index, item := range records {
		recordID := strings.TrimSpace(item.ID)
		for _, field := range uniqueFields {
			key := strings.TrimSpace(field.Key)
			value, exists := item.Data[key]
			if !exists || isEmptyValue(value) {
				continue
			}
			batchKey := key + "\x00" + comparableJSONValue(value)
			if firstIndex, ok := seen[batchKey]; ok {
				validations[index].Valid = false
				validations[index].Errors = append(validations[index].Errors, fmt.Sprintf("field %s value %s duplicates batch row %d", key, comparableJSONValue(value), firstIndex))
			} else {
				seen[batchKey] = index
			}
		}
		errs, err := s.uniqueConstraintErrors(ctx, p, datasetID, fields, item.Data, recordID)
		if err != nil {
			return err
		}
		if len(errs) > 0 {
			validations[index].Valid = false
			validations[index].Errors = append(validations[index].Errors, errs...)
		}
	}
	return nil
}
