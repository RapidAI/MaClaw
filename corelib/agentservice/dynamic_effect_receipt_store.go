package agentservice

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	coretool "github.com/RapidAI/CodeClaw/corelib/tool"

	_ "modernc.org/sqlite"
)

// DynamicEffectReceiptRecord is an append-once trusted observation of a
// remote external-effect acceptance. Receipt data stays in host storage; the
// semantic plan sees only its digest and never the provider/channel payload.
type DynamicEffectReceiptRecord struct {
	OperationID   string
	ReceiptDigest string
	AcceptedAt    time.Time
}

// DynamicEffectReceiptStore records the evidence required to promote an
// awaiting external effect to succeeded. Implementations must reject a second
// differing receipt for the same operation.
type DynamicEffectReceiptStore interface {
	Accept(operationID, receipt string, now time.Time) (DynamicEffectReceiptRecord, error)
	Get(operationID string) (DynamicEffectReceiptRecord, error)
}

var ErrDynamicEffectReceiptNotFound = errors.New("dynamic effect receipt not found")

type memoryDynamicEffectReceiptStore struct {
	mu      sync.Mutex
	records map[string]DynamicEffectReceiptRecord
}

func NewMemoryDynamicEffectReceiptStore() DynamicEffectReceiptStore {
	return &memoryDynamicEffectReceiptStore{records: make(map[string]DynamicEffectReceiptRecord)}
}

func (s *memoryDynamicEffectReceiptStore) Accept(operationID, receipt string, now time.Time) (DynamicEffectReceiptRecord, error) {
	if err := validateDynamicEffectReceipt(operationID, receipt); err != nil {
		return DynamicEffectReceiptRecord{}, err
	}
	record := DynamicEffectReceiptRecord{OperationID: strings.TrimSpace(operationID), ReceiptDigest: coretool.SchemaDigest([]byte(receipt)), AcceptedAt: now.UTC()}
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.records[record.OperationID]; ok {
		if existing.ReceiptDigest != record.ReceiptDigest {
			return DynamicEffectReceiptRecord{}, fmt.Errorf("dynamic effect receipt conflict")
		}
		return existing, nil
	}
	s.records[record.OperationID] = record
	return record, nil
}

func (s *memoryDynamicEffectReceiptStore) Get(operationID string) (DynamicEffectReceiptRecord, error) {
	if s == nil {
		return DynamicEffectReceiptRecord{}, fmt.Errorf("dynamic effect receipt store is unavailable")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.records[strings.TrimSpace(operationID)]
	if !ok {
		return DynamicEffectReceiptRecord{}, ErrDynamicEffectReceiptNotFound
	}
	return record, nil
}

type SQLiteDynamicEffectReceiptStore struct{ db *sql.DB }

func NewSQLiteDynamicEffectReceiptStore(path string) (*SQLiteDynamicEffectReceiptStore, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("dynamic effect receipt store path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create dynamic effect receipt store directory: %w", err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	store := &SQLiteDynamicEffectReceiptStore{db: db}
	for _, statement := range []string{
		`PRAGMA journal_mode=WAL`, `PRAGMA synchronous=FULL`, `PRAGMA busy_timeout=5000`,
		`CREATE TABLE IF NOT EXISTS dynamic_effect_receipts (
			operation_id TEXT PRIMARY KEY,
			receipt_digest TEXT NOT NULL,
			accepted_at TEXT NOT NULL
		)`,
	} {
		if _, err := db.Exec(statement); err != nil {
			_ = db.Close()
			return nil, err
		}
	}
	return store, nil
}

func (s *SQLiteDynamicEffectReceiptStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *SQLiteDynamicEffectReceiptStore) Accept(operationID, receipt string, now time.Time) (DynamicEffectReceiptRecord, error) {
	if s == nil || s.db == nil {
		return DynamicEffectReceiptRecord{}, fmt.Errorf("dynamic effect receipt store is unavailable")
	}
	if err := validateDynamicEffectReceipt(operationID, receipt); err != nil {
		return DynamicEffectReceiptRecord{}, err
	}
	operationID = strings.TrimSpace(operationID)
	digest := coretool.SchemaDigest([]byte(receipt))
	if _, err := s.db.Exec(`INSERT OR IGNORE INTO dynamic_effect_receipts(operation_id, receipt_digest, accepted_at) VALUES (?, ?, ?)`, operationID, digest, now.UTC().Format(time.RFC3339Nano)); err != nil {
		return DynamicEffectReceiptRecord{}, err
	}
	record, err := s.Get(operationID)
	if err != nil {
		return DynamicEffectReceiptRecord{}, err
	}
	if record.ReceiptDigest != digest {
		return DynamicEffectReceiptRecord{}, fmt.Errorf("dynamic effect receipt conflict")
	}
	return record, nil
}

func (s *SQLiteDynamicEffectReceiptStore) Get(operationID string) (DynamicEffectReceiptRecord, error) {
	if s == nil || s.db == nil {
		return DynamicEffectReceiptRecord{}, fmt.Errorf("dynamic effect receipt store is unavailable")
	}
	record := DynamicEffectReceiptRecord{OperationID: strings.TrimSpace(operationID)}
	var accepted string
	err := s.db.QueryRow(`SELECT receipt_digest, accepted_at FROM dynamic_effect_receipts WHERE operation_id = ?`, record.OperationID).Scan(&record.ReceiptDigest, &accepted)
	if errors.Is(err, sql.ErrNoRows) {
		return DynamicEffectReceiptRecord{}, ErrDynamicEffectReceiptNotFound
	}
	if err != nil {
		return DynamicEffectReceiptRecord{}, err
	}
	record.AcceptedAt, _ = time.Parse(time.RFC3339Nano, accepted)
	return record, nil
}

func validateDynamicEffectReceipt(operationID, receipt string) error {
	if strings.TrimSpace(operationID) == "" || strings.TrimSpace(receipt) == "" {
		return fmt.Errorf("dynamic effect receipt is required")
	}
	if len([]byte(receipt)) > 256*1024 {
		return fmt.Errorf("dynamic effect receipt too large")
	}
	return nil
}
