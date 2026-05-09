package agentservice

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

var recordCollectionPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$`)

func (s *Service) ListStructuredRecords(ctx context.Context, p Principal, in ListStructuredRecordsInput) ([]StructuredRecord, error) {
	_ = ctx
	collection, err := normalizeRecordCollection(in.Collection)
	if err != nil {
		return nil, err
	}
	in.Collection = collection
	items, err := s.records.ListStructuredRecords(p.TenantID, p.UserID, in)
	if err != nil {
		return nil, err
	}
	return cloneStructuredRecords(items), nil
}

func (s *Service) CreateStructuredRecord(ctx context.Context, p Principal, in CreateStructuredRecordInput) (*StructuredRecord, error) {
	_ = ctx
	collection, err := normalizeRecordCollection(in.Collection)
	if err != nil {
		return nil, err
	}
	if collection == "" {
		return nil, fmt.Errorf("collection is required")
	}
	data, err := normalizeRecordData(in.Data)
	if err != nil {
		return nil, err
	}
	now := s.now().UTC()
	record := StructuredRecord{
		ID:         NewID("record"),
		TenantID:   p.TenantID,
		UserID:     p.UserID,
		Collection: collection,
		Title:      strings.TrimSpace(in.Title),
		Tags:       normalizeRecordTags(in.Tags),
		Data:       data,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := s.records.SaveStructuredRecord(record); err != nil {
		return nil, err
	}
	_ = s.recordAudit(auditRecord{TenantID: p.TenantID, UserID: p.UserID, Action: "record.created", ResourceType: "record", ResourceID: record.ID, ActorType: "user", ActorTenantID: p.TenantID, ActorUserID: p.UserID, Metadata: map[string]string{"collection": collection}})
	out := cloneStructuredRecord(record)
	return &out, nil
}

func (s *Service) GetStructuredRecord(ctx context.Context, p Principal, collection, recordID string) (*StructuredRecord, error) {
	_ = ctx
	collection, err := normalizeRecordCollection(collection)
	if err != nil {
		return nil, err
	}
	record, err := s.records.GetStructuredRecord(p.TenantID, p.UserID, collection, strings.TrimSpace(recordID))
	if err != nil {
		return nil, err
	}
	out := cloneStructuredRecord(record)
	return &out, nil
}

func (s *Service) UpdateStructuredRecord(ctx context.Context, p Principal, collection, recordID string, in UpdateStructuredRecordInput) (*StructuredRecord, error) {
	_ = ctx
	collection, err := normalizeRecordCollection(collection)
	if err != nil {
		return nil, err
	}
	record, err := s.records.GetStructuredRecord(p.TenantID, p.UserID, collection, strings.TrimSpace(recordID))
	if err != nil {
		return nil, err
	}
	if in.Title != nil {
		record.Title = strings.TrimSpace(*in.Title)
	}
	if in.Tags != nil {
		record.Tags = normalizeRecordTags(in.Tags)
	}
	if in.Data != nil {
		data, err := normalizeRecordData(in.Data)
		if err != nil {
			return nil, err
		}
		record.Data = data
	}
	record.UpdatedAt = s.now().UTC()
	if err := s.records.SaveStructuredRecord(record); err != nil {
		return nil, err
	}
	_ = s.recordAudit(auditRecord{TenantID: p.TenantID, UserID: p.UserID, Action: "record.updated", ResourceType: "record", ResourceID: record.ID, ActorType: "user", ActorTenantID: p.TenantID, ActorUserID: p.UserID, Metadata: map[string]string{"collection": collection}})
	out := cloneStructuredRecord(record)
	return &out, nil
}

func (s *Service) DeleteStructuredRecord(ctx context.Context, p Principal, collection, recordID string) error {
	_ = ctx
	collection, err := normalizeRecordCollection(collection)
	if err != nil {
		return err
	}
	recordID = strings.TrimSpace(recordID)
	if err := s.records.DeleteStructuredRecord(p.TenantID, p.UserID, collection, recordID); err != nil {
		return err
	}
	_ = s.recordAudit(auditRecord{TenantID: p.TenantID, UserID: p.UserID, Action: "record.deleted", ResourceType: "record", ResourceID: recordID, ActorType: "user", ActorTenantID: p.TenantID, ActorUserID: p.UserID, Metadata: map[string]string{"collection": collection}})
	return nil
}

func normalizeRecordCollection(collection string) (string, error) {
	collection = strings.ToLower(strings.TrimSpace(collection))
	if collection == "" {
		return "", nil
	}
	if !recordCollectionPattern.MatchString(collection) {
		return "", fmt.Errorf("collection must use 1-64 letters, numbers, underscores, or hyphens")
	}
	return collection, nil
}

func normalizeRecordData(data map[string]any) (map[string]any, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("data is required")
	}
	encoded, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("data must be valid json object")
	}
	var out map[string]any
	if err := json.Unmarshal(encoded, &out); err != nil {
		return nil, fmt.Errorf("data must be valid json object")
	}
	return out, nil
}

func normalizeRecordTags(tags []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(tags))
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}
		key := strings.ToLower(tag)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, tag)
	}
	sort.Strings(out)
	return out
}

func cloneStructuredRecords(items []StructuredRecord) []StructuredRecord {
	out := make([]StructuredRecord, len(items))
	for i, item := range items {
		out[i] = cloneStructuredRecord(item)
	}
	return out
}

func cloneStructuredRecord(in StructuredRecord) StructuredRecord {
	out := in
	out.Tags = append([]string(nil), in.Tags...)
	if in.Data != nil {
		data, _ := normalizeRecordData(in.Data)
		out.Data = data
	}
	return out
}
