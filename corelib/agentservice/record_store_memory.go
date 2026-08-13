package agentservice

import (
	"encoding/json"
	"sort"
	"strings"
	"sync"
)

// MemoryRecordStore is an in-process structured-record store for the agentservice
// control plane. Despite the historical name, it is not Maclaw long-term memory;
// durable user/agent memories, recall, audit, and surgery are owned by
// corelib/memory.Store.
type MemoryRecordStore struct {
	mu      sync.RWMutex
	records map[string]StructuredRecord
}

func NewMemoryRecordStore() *MemoryRecordStore {
	return &MemoryRecordStore{records: map[string]StructuredRecord{}}
}

func (s *MemoryRecordStore) SaveStructuredRecord(record StructuredRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records[record.ID] = cloneStructuredRecord(record)
	return nil
}

func (s *MemoryRecordStore) GetStructuredRecord(tenantID, userID, collection, recordID string) (StructuredRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	record, ok := s.records[recordID]
	if !ok || record.TenantID != tenantID || record.UserID != userID || record.Collection != collection {
		return StructuredRecord{}, ErrRecordNotFound
	}
	return cloneStructuredRecord(record), nil
}

func (s *MemoryRecordStore) ListStructuredRecords(tenantID, userID string, in ListStructuredRecordsInput) ([]StructuredRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	tag := strings.ToLower(strings.TrimSpace(in.Tag))
	q := strings.ToLower(strings.TrimSpace(in.Q))
	items := make([]StructuredRecord, 0)
	for _, record := range s.records {
		if record.TenantID != tenantID || record.UserID != userID {
			continue
		}
		if in.Collection != "" && record.Collection != in.Collection {
			continue
		}
		if in.Before != "" && !record.CreatedAt.Before(parseRecordCursor(in.Before)) {
			continue
		}
		if tag != "" && !memoryRecordHasTag(record, tag) {
			continue
		}
		if q != "" && !memoryRecordMatchesQuery(record, q) {
			continue
		}
		items = append(items, cloneStructuredRecord(record))
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.After(items[j].CreatedAt) })
	limit := in.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	if len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func (s *MemoryRecordStore) DeleteStructuredRecord(tenantID, userID, collection, recordID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.records[recordID]
	if !ok || record.TenantID != tenantID || record.UserID != userID || record.Collection != collection {
		return ErrRecordNotFound
	}
	delete(s.records, recordID)
	return nil
}

func (s *MemoryRecordStore) DeleteStructuredRecordsForUser(tenantID, userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, record := range s.records {
		if record.TenantID == tenantID && record.UserID == userID {
			delete(s.records, id)
		}
	}
	return nil
}

func memoryRecordHasTag(record StructuredRecord, tag string) bool {
	for _, candidate := range record.Tags {
		if strings.ToLower(strings.TrimSpace(candidate)) == tag {
			return true
		}
	}
	return false
}

func memoryRecordMatchesQuery(record StructuredRecord, q string) bool {
	if strings.Contains(strings.ToLower(record.Title), q) || strings.Contains(strings.ToLower(record.Collection), q) {
		return true
	}
	for _, tag := range record.Tags {
		if strings.Contains(strings.ToLower(tag), q) {
			return true
		}
	}
	encoded, err := json.Marshal(record.Data)
	return err == nil && strings.Contains(strings.ToLower(string(encoded)), q)
}
