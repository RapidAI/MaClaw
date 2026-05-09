package structureddata

import (
	"context"
	"sort"
	"strings"
)

func (s *Service) ListRelationships(ctx context.Context, p Principal, in QueryRelationshipsInput) ([]DatasetRelationship, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items, err := s.listRelationshipsLocked(ctx, p, in.DatasetID)
	if err != nil {
		return nil, err
	}
	return paginateRelationships(items, in), nil
}

func paginateRelationships(items []DatasetRelationship, in QueryRelationshipsInput) []DatasetRelationship {
	limit := in.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	out := append([]DatasetRelationship(nil), items...)
	sort.Slice(out, func(i, j int) bool { return relationshipCursorKey(out[i]) > relationshipCursorKey(out[j]) })
	beforeID := strings.TrimSpace(in.BeforeID)
	if beforeID != "" {
		filtered := out[:0]
		for _, item := range out {
			if relationshipCursorKey(item) < beforeID {
				filtered = append(filtered, item)
			}
		}
		out = filtered
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func relationshipCursorKey(item DatasetRelationship) string {
	return strings.Join([]string{strings.TrimSpace(item.SourceDatasetID), strings.TrimSpace(item.SourceField), strings.TrimSpace(item.TargetDatasetID)}, "|")
}

func (s *Service) GetRelatedRecords(ctx context.Context, p Principal, datasetID string, recordID string, in QueryRelatedRecordsInput) (*RelatedRecordsResult, error) {
	datasetID = strings.TrimSpace(datasetID)
	recordID = strings.TrimSpace(recordID)
	if datasetID == "" || recordID == "" {
		return nil, ErrInvalidInput
	}
	limit := in.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	record, err := s.store.GetRecord(ctx, p.TenantID, datasetID, recordID)
	if err != nil {
		return nil, err
	}
	fields, err := s.store.ListFields(ctx, p.TenantID, datasetID)
	if err != nil {
		return nil, err
	}
	links := append([]RelatedRecordLink{}, s.outgoingRelatedRecords(ctx, p, datasetID, *record, fields)...)
	incoming, err := s.incomingRelatedRecords(ctx, p, datasetID, recordID, 500)
	if err != nil {
		return nil, err
	}
	links = append(links, incoming...)
	links = paginateRelatedRecordLinks(links, in)
	result := &RelatedRecordsResult{DatasetID: datasetID, RecordID: recordID, Record: maskSensitiveRecord(record, fields, p), Links: links, Limit: limit, HasMore: len(links) == limit}
	if result.HasMore && len(links) > 0 {
		result.NextBeforeID = relatedRecordCursorKey(links[len(links)-1])
	}
	return result, nil
}

func paginateRelatedRecordLinks(items []RelatedRecordLink, in QueryRelatedRecordsInput) []RelatedRecordLink {
	limit := in.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	out := append([]RelatedRecordLink(nil), items...)
	sort.Slice(out, func(i, j int) bool { return relatedRecordCursorKey(out[i]) > relatedRecordCursorKey(out[j]) })
	beforeID := strings.TrimSpace(in.BeforeID)
	if beforeID != "" {
		filtered := out[:0]
		for _, item := range out {
			if relatedRecordCursorKey(item) < beforeID {
				filtered = append(filtered, item)
			}
		}
		out = filtered
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func relatedRecordCursorKey(item RelatedRecordLink) string {
	recordID := ""
	if item.Record != nil {
		recordID = item.Record.ID
	}
	missing := "0"
	if item.Missing {
		missing = "1"
	}
	return strings.Join([]string{
		strings.TrimSpace(item.Direction),
		relationshipCursorKey(item.Relationship),
		strings.TrimSpace(recordID),
		missing,
	}, "|")
}

func (s *Service) outgoingRelatedRecords(ctx context.Context, p Principal, datasetID string, record Record, fields []FieldDefinition) []RelatedRecordLink {
	out := []RelatedRecordLink{}
	for _, rel := range relationshipsFromFields(datasetID, fields, false, true) {
		if rel.FieldType != "record_ref" || rel.TargetDatasetID == "" {
			continue
		}
		raw, ok := record.Data[rel.SourceField]
		if !ok || isEmptyValue(raw) {
			continue
		}
		targetID, ok := raw.(string)
		targetID = strings.TrimSpace(targetID)
		if !ok || targetID == "" {
			continue
		}
		link := RelatedRecordLink{Direction: "outgoing", Relationship: rel}
		target, err := s.store.GetRecord(ctx, p.TenantID, rel.TargetDatasetID, targetID)
		if err != nil {
			link.Missing = true
			link.Message = "referenced record not found"
			out = append(out, link)
			continue
		}
		targetFields, err := s.store.ListFields(ctx, p.TenantID, rel.TargetDatasetID)
		if err != nil {
			link.Missing = true
			link.Message = "target fields not available"
			out = append(out, link)
			continue
		}
		link.Record = maskSensitiveRecord(target, targetFields, p)
		out = append(out, link)
	}
	return out
}

func (s *Service) incomingRelatedRecords(ctx context.Context, p Principal, datasetID string, recordID string, limit int) ([]RelatedRecordLink, error) {
	if limit <= 0 {
		return nil, nil
	}
	relationships, err := s.listRelationshipsLocked(ctx, p, "")
	if err != nil {
		return nil, err
	}
	out := []RelatedRecordLink{}
	for _, rel := range relationships {
		if len(out) >= limit || rel.FieldType != "record_ref" || rel.TargetDatasetID != datasetID {
			continue
		}
		records, err := s.store.QueryRecords(ctx, p.TenantID, rel.SourceDatasetID, QueryRecordsInput{Filter: map[string]any{"field": rel.SourceField, "op": "eq", "value": recordID}, Limit: limit - len(out)})
		if err != nil {
			return nil, err
		}
		sourceFields, err := s.store.ListFields(ctx, p.TenantID, rel.SourceDatasetID)
		if err != nil {
			return nil, err
		}
		for _, record := range records {
			if len(out) >= limit {
				break
			}
			rec := record
			out = append(out, RelatedRecordLink{Direction: "incoming", Relationship: rel, Record: maskSensitiveRecord(&rec, sourceFields, p)})
		}
	}
	return out, nil
}

func (s *Service) listRelationshipsLocked(ctx context.Context, p Principal, sourceDatasetID string) ([]DatasetRelationship, error) {
	sourceDatasetID = strings.TrimSpace(sourceDatasetID)
	datasets, err := s.store.ListDatasets(ctx, p.TenantID)
	if err != nil {
		return nil, err
	}
	initialized := map[string]struct{}{}
	out := []DatasetRelationship{}
	for _, dataset := range datasets {
		initialized[dataset.ID] = struct{}{}
		if sourceDatasetID != "" && dataset.ID != sourceDatasetID {
			continue
		}
		fields, err := s.store.ListFields(ctx, p.TenantID, dataset.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, relationshipsFromFields(dataset.ID, fields, false, true)...)
	}
	for _, tmpl := range datasetTemplates {
		if sourceDatasetID != "" && tmpl.ID != sourceDatasetID {
			continue
		}
		if _, ok := initialized[tmpl.ID]; ok {
			continue
		}
		out = append(out, relationshipsFromTemplate(tmpl)...)
	}
	sortRelationships(out)
	return out, nil
}

func relationshipsFromTemplate(tmpl DatasetTemplate) []DatasetRelationship {
	fields := make([]FieldDefinition, 0, len(tmpl.Fields))
	for _, field := range tmpl.Fields {
		fields = append(fields, FieldDefinition{Key: field.Key, Type: field.Type, Title: field.Title, Config: cloneJSONMap(field.Config)})
	}
	return relationshipsFromFields(tmpl.ID, fields, true, false)
}

func relationshipsFromFields(datasetID string, fields []FieldDefinition, fromTemplate bool, initialized bool) []DatasetRelationship {
	out := []DatasetRelationship{}
	for _, field := range fields {
		fieldType := strings.ToLower(strings.TrimSpace(field.Type))
		if !isReferenceFieldType(fieldType) {
			continue
		}
		target, _ := stringConfigValue(field.Config, "ref_dataset")
		out = append(out, DatasetRelationship{
			SourceDatasetID: strings.TrimSpace(datasetID),
			SourceField:     strings.TrimSpace(field.Key),
			SourceTitle:     strings.TrimSpace(field.Title),
			TargetDatasetID: target,
			FieldType:       fieldType,
			FromTemplate:    fromTemplate,
			Initialized:     initialized,
		})
	}
	return out
}

func isReferenceFieldType(fieldType string) bool {
	switch strings.ToLower(strings.TrimSpace(fieldType)) {
	case "record_ref", "person_ref", "org_ref", "file_ref":
		return true
	default:
		return false
	}
}

func stringConfigValue(config map[string]any, key string) (string, bool) {
	if len(config) == 0 {
		return "", false
	}
	value, ok := config[key]
	if !ok {
		return "", false
	}
	text, ok := value.(string)
	if !ok {
		return "", false
	}
	text = strings.TrimSpace(text)
	return text, text != ""
}

func sortRelationships(items []DatasetRelationship) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].SourceDatasetID == items[j].SourceDatasetID {
			if items[i].SourceField == items[j].SourceField {
				return items[i].TargetDatasetID < items[j].TargetDatasetID
			}
			return items[i].SourceField < items[j].SourceField
		}
		return items[i].SourceDatasetID < items[j].SourceDatasetID
	})
}
