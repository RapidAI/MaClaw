package database

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// FileAuditStore is an append-only JSONL audit store. Each line is one JSON
// object with a "kind" field: "event" for every AuditEvent, plus an
// immutable "receipt" record for successful mutations (result_class=="ok"
// with a receipt id). Receipts are only ever appended, never modified.
//
// The store is metadata-only by construction: AuditEvent already carries no
// SQL text, parameter values, or tokens, and no new fields are added here.
type FileAuditStore struct {
	mu   sync.Mutex
	path string
}

// auditRecord wraps an AuditEvent with a discriminator for the JSONL file.
type auditRecord struct {
	Kind string `json:"kind"`
	AuditEvent
}

// mutationReceipt is the immutable receipt persisted for a successful
// mutation. It mirrors the metadata-only fields of the originating event.
type mutationReceipt struct {
	Kind           string `json:"kind"`
	ReceiptID      string `json:"receipt_id"`
	ApprovalID     string `json:"approval_id,omitempty"`
	ProfileID      string `json:"profile_id,omitempty"`
	ConnectionID   string `json:"connection_id,omitempty"`
	OwnerID        string `json:"owner_id,omitempty"`
	SessionID      string `json:"session_id,omitempty"`
	Action         string `json:"action"`
	SQLFingerprint string `json:"sql_fingerprint,omitempty"`
	ParameterCount int    `json:"parameter_count,omitempty"`
	AffectedRows   int64  `json:"affected_rows,omitempty"`
	Timestamp      string `json:"timestamp"`
}

// NewFileAuditStore creates (or opens) the append-only audit file at path,
// creating the parent directory if needed. The file itself is created
// lazily on first write with mode 0600.
func NewFileAuditStore(path string) (*FileAuditStore, error) {
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, err
		}
	}
	// Validate the file is writable now so hosts learn about a bad path at
	// wiring time rather than on the first audited statement.
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return nil, err
	}
	if err := f.Close(); err != nil {
		return nil, err
	}
	return &FileAuditStore{path: path}, nil
}

// Sink returns an AuditSink that appends every event (and mutation receipts)
// to the JSONL file. Write failures are logged and never block the caller.
func (s *FileAuditStore) Sink() AuditSink {
	return func(_ context.Context, event AuditEvent) {
		s.append(auditRecord{Kind: "event", AuditEvent: event})
		if event.ResultClass == "ok" && event.ReceiptID != "" {
			s.append(mutationReceipt{
				Kind:           "receipt",
				ReceiptID:      event.ReceiptID,
				ApprovalID:     event.ApprovalID,
				ProfileID:      event.ProfileID,
				ConnectionID:   event.ConnectionID,
				OwnerID:        event.OwnerID,
				SessionID:      event.SessionID,
				Action:         event.Action,
				SQLFingerprint: event.SQLFingerprint,
				ParameterCount: event.ParameterCount,
				AffectedRows:   event.AffectedRows,
				Timestamp:      event.Timestamp.UTC().Format("2006-01-02T15:04:05.999999999Z"),
			})
		}
	}
}

// NewFileAuditSink is a convenience wrapper returning an AuditSink directly
// for Manager.SetAuditSink.
func NewFileAuditSink(path string) (AuditSink, error) {
	store, err := NewFileAuditStore(path)
	if err != nil {
		return nil, err
	}
	return store.Sink(), nil
}

// LookupReceipt scans the JSONL store for an immutable mutation receipt.
func (s *FileAuditStore) LookupReceipt(id string) (map[string]interface{}, error) {
	if s == nil || strings.TrimSpace(id) == "" {
		return nil, fmt.Errorf("receipt not found")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := os.ReadFile(s.path)
	if err != nil {
		return nil, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var rec map[string]interface{}
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			continue
		}
		if rec["kind"] == "receipt" && rec["receipt_id"] == id {
			return rec, nil
		}
	}
	return nil, fmt.Errorf("receipt not found")
}

// append writes one JSONL record. Each write opens and closes the file so no
// lingering handle blocks readers (notably on Windows).
func (s *FileAuditStore) append(record interface{}) {
	data, err := json.Marshal(record)
	if err != nil {
		log.Printf("[database] audit record marshal failed: %v", err)
		return
	}
	data = append(data, '\n')
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := os.OpenFile(s.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		log.Printf("[database] audit store open failed path=%s: %v", s.path, err)
		return
	}
	if _, err := f.Write(data); err != nil {
		log.Printf("[database] audit store write failed path=%s: %v", s.path, err)
	}
	if err := f.Close(); err != nil {
		log.Printf("[database] audit store close failed path=%s: %v", s.path, err)
	}
}
