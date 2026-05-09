package structureddata

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
)

func (s *Service) ImportRecordsCSV(ctx context.Context, p Principal, datasetID string, in ImportCSVInput) (*BatchImportRecordsResult, error) {
	datasetID = strings.TrimSpace(datasetID)
	if strings.TrimSpace(in.CSVText) == "" {
		return nil, fmt.Errorf("%w: csv is required", ErrInvalidInput)
	}
	fields, err := s.ListFields(ctx, p, datasetID)
	if err != nil {
		return nil, err
	}
	records, err := parseCSVRecords(in.CSVText, fields)
	if err != nil {
		return nil, err
	}
	return s.BatchImportRecords(ctx, p, datasetID, BatchImportRecordsInput{Records: records, DryRun: in.DryRun})
}

func parseCSVRecords(csvText string, fields []FieldDefinition) ([]BatchRecordInput, error) {
	reader := csv.NewReader(strings.NewReader(csvText))
	reader.TrimLeadingSpace = true
	rows, err := reader.ReadAll()
	if err != nil {
		if err == io.EOF {
			return nil, fmt.Errorf("%w: csv is empty", ErrInvalidInput)
		}
		return nil, fmt.Errorf("%w: invalid csv", ErrInvalidInput)
	}
	if len(rows) < 2 {
		return nil, fmt.Errorf("%w: csv requires header and at least one data row", ErrInvalidInput)
	}
	headers := make([]string, len(rows[0]))
	for i, header := range rows[0] {
		headers[i] = strings.TrimSpace(header)
	}
	fieldByKey := map[string]FieldDefinition{}
	for _, field := range fields {
		fieldByKey[strings.TrimSpace(field.Key)] = field
	}
	out := make([]BatchRecordInput, 0, len(rows)-1)
	for rowIndex, row := range rows[1:] {
		item := BatchRecordInput{Data: map[string]any{}}
		empty := true
		for colIndex, raw := range row {
			if colIndex >= len(headers) {
				continue
			}
			key := headers[colIndex]
			if key == "" {
				continue
			}
			value := strings.TrimSpace(raw)
			if value != "" {
				empty = false
			}
			switch key {
			case "id":
				item.ID = value
			case "title":
				item.Title = value
			case "tags":
				item.Tags = parseCSVTags(value)
			case "source_id":
				item.SourceID = value
			default:
				converted, err := convertCSVValue(value, fieldByKey[key])
				if err != nil {
					return nil, fmt.Errorf("%w: row %d field %s: %s", ErrInvalidInput, rowIndex+2, key, err.Error())
				}
				item.Data[key] = converted
			}
		}
		if !empty {
			out = append(out, item)
			if len(out) > maxBatchImportRecords {
				return nil, fmt.Errorf("%w: batch import supports at most %d records", ErrInvalidInput, maxBatchImportRecords)
			}
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%w: csv contains no data rows", ErrInvalidInput)
	}
	return out, nil
}

func parseCSVTags(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == ';' || r == '|' })
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

func convertCSVValue(raw string, field FieldDefinition) (any, error) {
	if raw == "" {
		return "", nil
	}
	switch strings.ToLower(strings.TrimSpace(field.Type)) {
	case "number":
		value, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return raw, fmt.Errorf("must be number")
		}
		return value, nil
	case "boolean":
		value, err := strconv.ParseBool(strings.ToLower(raw))
		if err != nil {
			return raw, fmt.Errorf("must be boolean")
		}
		return value, nil
	case "array", "object", "json":
		var value any
		if err := json.Unmarshal([]byte(raw), &value); err != nil {
			return raw, fmt.Errorf("must be json")
		}
		return value, nil
	default:
		return raw, nil
	}
}
