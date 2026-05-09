package structureddata

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

var qualityChecks = []QualityCheckDefinition{
	{ID: "schema_validation", Title: "Schema validation", Description: "Checks required fields, field types, date formats, and enum values.", Severity: "error"},
	{ID: "unknown_fields", Title: "Unknown fields", Description: "Reports fields that are present in records but not yet defined in the dataset schema.", Severity: "warning"},
	{ID: "unique_duplicates", Title: "Unique duplicates", Description: "Checks duplicate values for fields marked config.unique=true.", Severity: "error"},
	{ID: "relationship_refs", Title: "Relationship references", Description: "Checks record_ref fields that declare config.ref_dataset and reports references to missing target records.", Severity: "error"},
}

func (s *Service) ListQualityChecks(ctx context.Context, p Principal, query ...QueryQualityChecksInput) ([]QualityCheckDefinition, error) {
	_ = ctx
	_ = p
	out := append([]QualityCheckDefinition(nil), qualityChecks...)
	if len(query) == 0 {
		sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
		return out, nil
	}
	return paginateQualityChecks(out, query[0]), nil
}

func paginateQualityChecks(items []QualityCheckDefinition, in QueryQualityChecksInput) []QualityCheckDefinition {
	limit := in.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	out := append([]QualityCheckDefinition(nil), items...)
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	beforeID := strings.TrimSpace(in.BeforeID)
	if beforeID != "" {
		filtered := out[:0]
		for _, check := range out {
			if check.ID < beforeID {
				filtered = append(filtered, check)
			}
		}
		out = filtered
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func (s *Service) RunQualityCheck(ctx context.Context, p Principal, datasetID string, in RunQualityCheckInput) (*QualityCheckResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	datasetID = strings.TrimSpace(datasetID)
	if _, err := s.store.GetDataset(ctx, p.TenantID, datasetID); err != nil {
		return nil, err
	}
	fields, err := s.store.ListFields(ctx, p.TenantID, datasetID)
	if err != nil {
		return nil, err
	}
	limit := in.Limit
	if limit <= 0 || limit > 5000 {
		limit = 1000
	}
	records, err := s.qualityRecords(ctx, p, datasetID, limit)
	if err != nil {
		return nil, err
	}
	selected := selectedQualityChecks(in.Checks)
	checkIDs := sortedSelectedQualityCheckIDs(selected)
	now := s.now().UTC()
	result := &QualityCheckResult{ID: newID("quality_run"), TenantID: p.TenantID, DatasetID: datasetID, Checks: checkIDs, Scanned: len(records), Valid: true, Limit: limit, IncludeWarnings: in.IncludeWarnings, CreatedBy: p.UserID, CreatedAt: now}
	if selected["schema_validation"] || selected["unknown_fields"] {
		appendSchemaQualityIssues(result, datasetID, fields, records, in.IncludeWarnings)
	}
	if selected["unique_duplicates"] {
		appendUniqueQualityIssues(result, p, datasetID, fields, records)
	}
	if selected["relationship_refs"] {
		appendRelationshipQualityIssues(ctx, s, result, p, datasetID, fields, records)
	}
	if len(result.Issues) > 0 {
		for _, issue := range result.Issues {
			if issue.Severity == "error" {
				result.Valid = false
				break
			}
		}
	}
	if len(result.Issues) == 0 {
		result.Valid = true
	}
	result.IssueCount = len(result.Issues)
	out, err := s.store.AppendQualityRun(ctx, *result)
	if err != nil {
		return nil, err
	}
	s.audit(ctx, p, "quality.run", datasetID, "quality_run", out.ID, "Ran quality check", map[string]any{"valid": out.Valid, "scanned": out.Scanned, "issue_count": out.IssueCount})
	return out, nil
}

func (s *Service) ListQualityRuns(ctx context.Context, p Principal, datasetID string, in QueryQualityRunsInput) ([]QualityCheckResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	datasetID = strings.TrimSpace(datasetID)
	if _, err := s.store.GetDataset(ctx, p.TenantID, datasetID); err != nil {
		return nil, err
	}
	in.Before = strings.TrimSpace(in.Before)
	in.BeforeID = strings.TrimSpace(in.BeforeID)
	return s.store.ListQualityRuns(ctx, p.TenantID, datasetID, in)
}

func (s *Service) GetQualityRun(ctx context.Context, p Principal, datasetID, runID string) (*QualityCheckResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	datasetID = strings.TrimSpace(datasetID)
	if _, err := s.store.GetDataset(ctx, p.TenantID, datasetID); err != nil {
		return nil, err
	}
	return s.store.GetQualityRun(ctx, p.TenantID, datasetID, strings.TrimSpace(runID))
}

func (s *Service) qualityRecords(ctx context.Context, p Principal, datasetID string, limit int) ([]Record, error) {
	out := []Record{}
	before := ""
	beforeID := ""
	for len(out) < limit {
		pageLimit := 500
		if remaining := limit - len(out); remaining < pageLimit {
			pageLimit = remaining
		}
		page, err := s.store.QueryRecords(ctx, p.TenantID, datasetID, QueryRecordsInput{Limit: pageLimit, Before: before, BeforeID: beforeID})
		if err != nil {
			return nil, err
		}
		if len(page) == 0 {
			break
		}
		out = append(out, page...)
		last := page[len(page)-1]
		before = formatTime(last.CreatedAt)
		beforeID = last.ID
		if len(page) < pageLimit {
			break
		}
	}
	return out, nil
}

func selectedQualityChecks(checks []string) map[string]bool {
	out := map[string]bool{}
	if len(checks) == 0 {
		for _, check := range qualityChecks {
			out[check.ID] = true
		}
		return out
	}
	known := map[string]bool{}
	for _, check := range qualityChecks {
		known[check.ID] = true
	}
	for _, check := range checks {
		check = strings.TrimSpace(check)
		if known[check] {
			out[check] = true
		}
	}
	return out
}

func sortedSelectedQualityCheckIDs(selected map[string]bool) []string {
	out := make([]string, 0, len(selected))
	for id, ok := range selected {
		if ok {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}

func appendSchemaQualityIssues(result *QualityCheckResult, datasetID string, fields []FieldDefinition, records []Record, includeWarnings bool) {
	defined := map[string]FieldDefinition{}
	for _, field := range fields {
		if strings.TrimSpace(field.Key) != "" {
			defined[strings.TrimSpace(field.Key)] = field
		}
	}
	for _, record := range records {
		for _, field := range fields {
			key := strings.TrimSpace(field.Key)
			if key == "" {
				continue
			}
			value, exists := record.Data[key]
			if field.Required && (!exists || isEmptyValue(value)) {
				result.Issues = append(result.Issues, QualityIssue{Severity: "error", Check: "schema_validation", DatasetID: datasetID, RecordID: record.ID, Field: key, Message: "required field is missing"})
				continue
			}
			if exists && value != nil {
				if err := validateFieldValue(field, value); err != nil {
					result.Issues = append(result.Issues, QualityIssue{Severity: "error", Check: "schema_validation", DatasetID: datasetID, RecordID: record.ID, Field: key, Message: strings.TrimPrefix(err.Error(), ErrInvalidInput.Error()+": "), Value: value})
				}
			}
		}
		if includeWarnings {
			for key, value := range record.Data {
				if _, ok := defined[key]; !ok {
					result.Issues = append(result.Issues, QualityIssue{Severity: "warning", Check: "unknown_fields", DatasetID: datasetID, RecordID: record.ID, Field: key, Message: "field is not defined in dataset schema", Value: value})
				}
			}
		}
	}
}

func appendUniqueQualityIssues(result *QualityCheckResult, p Principal, datasetID string, fields []FieldDefinition, records []Record) {
	uniqueFields := uniqueFieldDefinitions(fields)
	if len(uniqueFields) == 0 {
		return
	}
	sensitive := sensitiveFieldSet(fields)
	for _, field := range uniqueFields {
		key := strings.TrimSpace(field.Key)
		seen := map[string]string{}
		for _, record := range records {
			value, exists := record.Data[key]
			if !exists || isEmptyValue(value) {
				continue
			}
			comparable := comparableJSONValue(value)
			if firstID, ok := seen[comparable]; ok {
				issueValue := value
				if _, isSensitive := sensitive[key]; isSensitive && !canViewSensitive(p) {
					issueValue = maskedValue
				}
				result.Issues = append(result.Issues, QualityIssue{Severity: "error", Check: "unique_duplicates", DatasetID: datasetID, RecordID: record.ID, Field: key, Message: fmt.Sprintf("duplicate unique value also used by record %s", firstID), Value: issueValue})
			} else {
				seen[comparable] = record.ID
			}
		}
	}
}

func appendRelationshipQualityIssues(ctx context.Context, s *Service, result *QualityCheckResult, p Principal, datasetID string, fields []FieldDefinition, records []Record) {
	refFields := []FieldDefinition{}
	for _, field := range fields {
		fieldType := strings.ToLower(strings.TrimSpace(field.Type))
		if fieldType != "record_ref" {
			continue
		}
		if targetDatasetID, ok := stringConfigValue(field.Config, "ref_dataset"); ok {
			field.Config = cloneJSONMap(field.Config)
			field.Config["ref_dataset"] = targetDatasetID
			refFields = append(refFields, field)
		}
	}
	if len(refFields) == 0 {
		return
	}
	cache := map[string]bool{}
	for _, record := range records {
		for _, field := range refFields {
			key := strings.TrimSpace(field.Key)
			value, exists := record.Data[key]
			if !exists || isEmptyValue(value) {
				continue
			}
			targetID, ok := value.(string)
			targetID = strings.TrimSpace(targetID)
			if !ok || targetID == "" {
				continue
			}
			targetDatasetID, _ := stringConfigValue(field.Config, "ref_dataset")
			cacheKey := targetDatasetID + "\x00" + targetID
			existsInTarget, cached := cache[cacheKey]
			if !cached {
				if _, err := s.store.GetRecord(ctx, p.TenantID, targetDatasetID, targetID); err == nil {
					existsInTarget = true
				}
				cache[cacheKey] = existsInTarget
			}
			if !existsInTarget {
				result.Issues = append(result.Issues, QualityIssue{Severity: "error", Check: "relationship_refs", DatasetID: datasetID, RecordID: record.ID, Field: key, Message: fmt.Sprintf("referenced record %s not found in %s", targetID, targetDatasetID), Value: targetID})
			}
		}
	}
}
