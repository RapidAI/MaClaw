package structureddata

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

func (s *Service) ImportRecordsJSONL(ctx context.Context, p Principal, datasetID string, in ImportJSONLInput) (*BatchImportRecordsResult, error) {
	datasetID = strings.TrimSpace(datasetID)
	if strings.TrimSpace(in.JSONLText) == "" {
		return nil, fmt.Errorf("%w: jsonl is required", ErrInvalidInput)
	}
	records, err := parseJSONLRecords(in.JSONLText)
	if err != nil {
		return nil, err
	}
	return s.BatchImportRecords(ctx, p, datasetID, BatchImportRecordsInput{Records: records, DryRun: in.DryRun})
}

func parseJSONLRecords(jsonlText string) ([]BatchRecordInput, error) {
	scanner := bufio.NewScanner(strings.NewReader(jsonlText))
	scanner.Buffer(make([]byte, 0, 64*1024), int(maxBodyBytes))
	out := []BatchRecordInput{}
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		record, err := parseJSONLRecordLine([]byte(line))
		if err != nil {
			return nil, fmt.Errorf("%w: line %d: %s", ErrInvalidInput, lineNo, err.Error())
		}
		out = append(out, record)
		if len(out) > maxBatchImportRecords {
			return nil, fmt.Errorf("%w: batch import supports at most %d records", ErrInvalidInput, maxBatchImportRecords)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("%w: invalid jsonl", ErrInvalidInput)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%w: jsonl contains no records", ErrInvalidInput)
	}
	return out, nil
}

func parseJSONLRecordLine(line []byte) (BatchRecordInput, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(line, &raw); err != nil {
		return BatchRecordInput{}, fmt.Errorf("invalid json object")
	}
	if len(raw) == 0 {
		return BatchRecordInput{}, fmt.Errorf("empty object")
	}
	if dataRaw, ok := raw["data"]; ok {
		item := BatchRecordInput{}
		if err := json.Unmarshal(line, &item); err != nil {
			return BatchRecordInput{}, fmt.Errorf("invalid record envelope")
		}
		if len(bytes.TrimSpace(dataRaw)) == 0 || string(bytes.TrimSpace(dataRaw)) == "null" {
			return BatchRecordInput{}, fmt.Errorf("data is required")
		}
		if item.Data == nil {
			return BatchRecordInput{}, fmt.Errorf("data must be an object")
		}
		return item, nil
	}
	item := BatchRecordInput{Data: map[string]any{}}
	for key, valueRaw := range raw {
		switch key {
		case "id":
			if err := json.Unmarshal(valueRaw, &item.ID); err != nil {
				return BatchRecordInput{}, fmt.Errorf("id must be string")
			}
		case "title":
			if err := json.Unmarshal(valueRaw, &item.Title); err != nil {
				return BatchRecordInput{}, fmt.Errorf("title must be string")
			}
		case "tags":
			tags, err := parseJSONLTags(valueRaw)
			if err != nil {
				return BatchRecordInput{}, err
			}
			item.Tags = tags
		case "source_id":
			if err := json.Unmarshal(valueRaw, &item.SourceID); err != nil {
				return BatchRecordInput{}, fmt.Errorf("source_id must be string")
			}
		default:
			var value any
			if err := json.Unmarshal(valueRaw, &value); err != nil {
				return BatchRecordInput{}, fmt.Errorf("field %s is invalid json", key)
			}
			item.Data[key] = value
		}
	}
	if len(item.Data) == 0 {
		return BatchRecordInput{}, fmt.Errorf("data is required")
	}
	return item, nil
}

func parseJSONLTags(raw json.RawMessage) ([]string, error) {
	var tags []string
	if err := json.Unmarshal(raw, &tags); err == nil {
		return tags, nil
	}
	var tagText string
	if err := json.Unmarshal(raw, &tagText); err == nil {
		return parseCSVTags(tagText), nil
	}
	return nil, fmt.Errorf("tags must be array or string")
}
