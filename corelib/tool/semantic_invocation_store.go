package tool

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// InvocationGrantConsumeResult is the authoritative outcome of a one-time
// grant admission attempt. It is deliberately separate from provider result
// and operation receipt state: consuming a grant grants one attempt, not a
// claim that an external effect completed.
type InvocationGrantConsumeResult string

const (
	InvocationGrantConsumeAccepted InvocationGrantConsumeResult = "accepted"
	InvocationGrantConsumeConsumed InvocationGrantConsumeResult = "consumed"
	InvocationGrantConsumeRevoked  InvocationGrantConsumeResult = "revoked"
	InvocationGrantConsumeExpired  InvocationGrantConsumeResult = "expired"
	InvocationGrantConsumeInvalid  InvocationGrantConsumeResult = "invalid"
)

// InvocationGrantStore owns the mutable lifecycle of signed grants. The
// signed payload remains self-contained, while this store makes issuance,
// revocation and exactly-once admission linearizable across reconnects.
type InvocationGrantStore interface {
	RecordIssued([]InvocationGrant) error
	Consume(nonce, fingerprint string, now time.Time) (InvocationGrantConsumeResult, error)
	Revoke(nonce, fingerprint string) error
	// ReplaceIssued revokes one issued nonce and inserts its successor in the
	// same transaction. The wall clock is not consulted: an expired issued row
	// is still issued. A consumed row returns invocation_grant_replayed and a
	// revoked row returns invocation_grant_revoked. A fingerprint that does not
	// identify the row returns invocation_grant_invalid so the caller can try
	// another payload layout.
	ReplaceIssued(oldNonce, fingerprint string, successor InvocationGrant) error
}

type memoryInvocationGrantRecord struct {
	fingerprint string
	expiresAt   time.Time
	state       InvocationGrantConsumeResult // accepted means issued/not consumed
}

// MemoryInvocationGrantStore is intentionally for tests and explicitly
// single-process development. It must not be used for durable external-effect
// execution.
type MemoryInvocationGrantStore struct {
	mu      sync.Mutex
	records map[string]memoryInvocationGrantRecord
}

func NewMemoryInvocationGrantStore() *MemoryInvocationGrantStore {
	return &MemoryInvocationGrantStore{records: make(map[string]memoryInvocationGrantRecord)}
}

func (s *MemoryInvocationGrantStore) RecordIssued(grants []InvocationGrant) error {
	if s == nil {
		return fmt.Errorf("invocation grant store is unavailable")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, grant := range grants {
		if _, exists := s.records[grant.Nonce]; exists {
			return fmt.Errorf("invocation grant nonce collision")
		}
	}
	for _, grant := range grants {
		s.records[grant.Nonce] = memoryInvocationGrantRecord{fingerprint: invocationGrantFingerprint(grant), expiresAt: grant.ExpiresAt.UTC(), state: InvocationGrantConsumeAccepted}
	}
	return nil
}

func (s *MemoryInvocationGrantStore) Consume(nonce, fingerprint string, now time.Time) (InvocationGrantConsumeResult, error) {
	if s == nil {
		return InvocationGrantConsumeInvalid, fmt.Errorf("invocation grant store is unavailable")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.records[nonce]
	if !ok || record.fingerprint != fingerprint {
		return InvocationGrantConsumeInvalid, nil
	}
	if !now.UTC().Before(record.expiresAt.UTC()) {
		return InvocationGrantConsumeExpired, nil
	}
	switch record.state {
	case InvocationGrantConsumeAccepted:
		record.state = InvocationGrantConsumeConsumed
		s.records[nonce] = record
		return InvocationGrantConsumeAccepted, nil
	case InvocationGrantConsumeConsumed:
		return InvocationGrantConsumeConsumed, nil
	case InvocationGrantConsumeRevoked:
		return InvocationGrantConsumeRevoked, nil
	default:
		return InvocationGrantConsumeInvalid, nil
	}
}

func (s *MemoryInvocationGrantStore) Revoke(nonce, fingerprint string) error {
	if s == nil {
		return fmt.Errorf("invocation grant store is unavailable")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.records[nonce]
	if !ok || record.fingerprint != fingerprint {
		return nil
	}
	if record.state == InvocationGrantConsumeAccepted {
		record.state = InvocationGrantConsumeRevoked
		s.records[nonce] = record
	}
	return nil
}

// SQLiteInvocationGrantStore is the durable grant state required by
// restartable or multi-process hosts. SQLite's conditional UPDATE is the
// linearization point for one-time admission.
type SQLiteInvocationGrantStore struct {
	db *sql.DB
}

func NewSQLiteInvocationGrantStore(dbPath string) (*SQLiteInvocationGrantStore, error) {
	if dbPath == "" {
		return nil, fmt.Errorf("invocation grant store path is required")
	}
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o700); err != nil {
		return nil, fmt.Errorf("create invocation grant store directory: %w", err)
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	store := &SQLiteInvocationGrantStore{db: db}
	if err := store.init(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *SQLiteInvocationGrantStore) init() error {
	if s == nil || s.db == nil {
		return fmt.Errorf("invocation grant store is unavailable")
	}
	for _, statement := range []string{
		`PRAGMA journal_mode=WAL`,
		`PRAGMA synchronous=FULL`,
		`PRAGMA busy_timeout=5000`,
		`CREATE TABLE IF NOT EXISTS invocation_grants (
			nonce TEXT PRIMARY KEY,
			fingerprint TEXT NOT NULL,
			expires_at TEXT NOT NULL,
			state TEXT NOT NULL CHECK(state IN ('issued', 'consumed', 'revoked')),
			created_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_invocation_grants_expiry ON invocation_grants(expires_at)`,
	} {
		if _, err := s.db.Exec(statement); err != nil {
			return err
		}
	}
	return nil
}

func (s *SQLiteInvocationGrantStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *SQLiteInvocationGrantStore) RecordIssued(grants []InvocationGrant) (err error) {
	if s == nil || s.db == nil {
		return fmt.Errorf("invocation grant store is unavailable")
	}
	if len(grants) == 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	for _, grant := range grants {
		if grant.Nonce == "" || grant.ExpiresAt.IsZero() {
			return fmt.Errorf("invalid invocation grant")
		}
		if _, err = tx.Exec(`INSERT INTO invocation_grants(nonce, fingerprint, expires_at, state, created_at) VALUES (?, ?, ?, 'issued', ?)`, grant.Nonce, invocationGrantFingerprint(grant), grant.ExpiresAt.UTC().Format(time.RFC3339Nano), grant.IssuedAt.UTC().Format(time.RFC3339Nano)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *SQLiteInvocationGrantStore) Consume(nonce, fingerprint string, now time.Time) (InvocationGrantConsumeResult, error) {
	if s == nil || s.db == nil {
		return InvocationGrantConsumeInvalid, fmt.Errorf("invocation grant store is unavailable")
	}
	nowText := now.UTC().Format(time.RFC3339Nano)
	result, err := s.db.Exec(`UPDATE invocation_grants SET state = 'consumed' WHERE nonce = ? AND fingerprint = ? AND state = 'issued' AND expires_at > ?`, nonce, fingerprint, nowText)
	if err != nil {
		return InvocationGrantConsumeInvalid, err
	}
	if changed, err := result.RowsAffected(); err != nil {
		return InvocationGrantConsumeInvalid, err
	} else if changed == 1 {
		return InvocationGrantConsumeAccepted, nil
	}
	var storedFingerprint, expiresAt, state string
	if err := s.db.QueryRow(`SELECT fingerprint, expires_at, state FROM invocation_grants WHERE nonce = ?`, nonce).Scan(&storedFingerprint, &expiresAt, &state); err != nil {
		if err == sql.ErrNoRows {
			return InvocationGrantConsumeInvalid, nil
		}
		return InvocationGrantConsumeInvalid, err
	}
	if storedFingerprint != fingerprint {
		return InvocationGrantConsumeInvalid, nil
	}
	parsedExpiry, err := time.Parse(time.RFC3339Nano, expiresAt)
	if err != nil || !now.UTC().Before(parsedExpiry.UTC()) {
		return InvocationGrantConsumeExpired, nil
	}
	switch state {
	case "consumed":
		return InvocationGrantConsumeConsumed, nil
	case "revoked":
		return InvocationGrantConsumeRevoked, nil
	default:
		return InvocationGrantConsumeInvalid, nil
	}
}

func (s *SQLiteInvocationGrantStore) Revoke(nonce, fingerprint string) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("invocation grant store is unavailable")
	}
	result, err := s.db.Exec(`UPDATE invocation_grants SET state = 'revoked' WHERE nonce = ? AND fingerprint = ? AND state = 'issued'`, nonce, fingerprint)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 0 {
		return nil
	}
	var storedFingerprint string
	if err := s.db.QueryRow(`SELECT fingerprint FROM invocation_grants WHERE nonce = ?`, nonce).Scan(&storedFingerprint); err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("invocation grant not found")
		}
		return err
	}
	if storedFingerprint != fingerprint {
		return fmt.Errorf("invocation grant not found")
	}
	return nil
}

func (s *MemoryInvocationGrantStore) ReplaceIssued(oldNonce, fingerprint string, successor InvocationGrant) error {
	if s == nil {
		return fmt.Errorf("invocation grant store is unavailable")
	}
	if !validReplacementGrant(oldNonce, fingerprint, successor) {
		return fmt.Errorf("invocation_grant_invalid")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.records[oldNonce]
	if !ok || record.fingerprint != fingerprint {
		return fmt.Errorf("invocation_grant_invalid")
	}
	switch record.state {
	case InvocationGrantConsumeConsumed:
		return fmt.Errorf("invocation_grant_replayed")
	case InvocationGrantConsumeRevoked:
		return fmt.Errorf("invocation_grant_revoked")
	case InvocationGrantConsumeAccepted:
		if _, exists := s.records[successor.Nonce]; exists {
			return fmt.Errorf("invocation grant nonce collision")
		}
		record.state = InvocationGrantConsumeRevoked
		s.records[oldNonce] = record
		s.records[successor.Nonce] = memoryInvocationGrantRecord{
			fingerprint: invocationGrantFingerprint(successor),
			expiresAt:   successor.ExpiresAt.UTC(),
			state:       InvocationGrantConsumeAccepted,
		}
		return nil
	default:
		return fmt.Errorf("invocation_grant_invalid")
	}
}

func (s *SQLiteInvocationGrantStore) ReplaceIssued(oldNonce, fingerprint string, successor InvocationGrant) (err error) {
	if s == nil || s.db == nil {
		return fmt.Errorf("invocation grant store is unavailable")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	if err = replaceIssuedGrantTx(tx, oldNonce, fingerprint, successor); err != nil {
		return err
	}
	return tx.Commit()
}

func validReplacementGrant(oldNonce, fingerprint string, successor InvocationGrant) bool {
	return strings.TrimSpace(oldNonce) != "" && strings.TrimSpace(fingerprint) != "" &&
		strings.TrimSpace(successor.Nonce) != "" && successor.Nonce != oldNonce &&
		strings.TrimSpace(successor.Token) != "" && strings.TrimSpace(successor.Signature) != "" &&
		!successor.ExpiresAt.IsZero()
}

// replaceIssuedGrantTx revokes an issued row and inserts the successor. The
// caller owns the transaction. An expired issued row is still replaceable; the
// clock is the signer's concern, not this mutation's. The successor nonce is
// checked before the old row changes, so a collision leaves that row issued.
func replaceIssuedGrantTx(tx *sql.Tx, oldNonce, fingerprint string, successor InvocationGrant) error {
	if tx == nil {
		return fmt.Errorf("invocation grant store is unavailable")
	}
	if !validReplacementGrant(oldNonce, fingerprint, successor) {
		return fmt.Errorf("invocation_grant_invalid")
	}
	state, err := issuedGrantReplacementState(tx, oldNonce, fingerprint)
	if err != nil {
		return err
	}
	if state != "issued" {
		return issuedGrantReplacementStateError(state)
	}
	var occupied string
	err = tx.QueryRow(`SELECT nonce FROM invocation_grants WHERE nonce = ?`, successor.Nonce).Scan(&occupied)
	if err == nil {
		return fmt.Errorf("invocation grant nonce collision")
	}
	if err != sql.ErrNoRows {
		return err
	}
	result, err := tx.Exec(`UPDATE invocation_grants SET state = 'revoked' WHERE nonce = ? AND fingerprint = ? AND state = 'issued'`, oldNonce, fingerprint)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		state, err = issuedGrantReplacementState(tx, oldNonce, fingerprint)
		if err != nil {
			return err
		}
		return issuedGrantReplacementStateError(state)
	}
	_, err = tx.Exec(`INSERT INTO invocation_grants(nonce, fingerprint, expires_at, state, created_at) VALUES (?, ?, ?, 'issued', ?)`, successor.Nonce, invocationGrantFingerprint(successor), successor.ExpiresAt.UTC().Format(time.RFC3339Nano), successor.IssuedAt.UTC().Format(time.RFC3339Nano))
	return err
}

func issuedGrantReplacementState(tx *sql.Tx, nonce, fingerprint string) (string, error) {
	var storedFingerprint, state string
	err := tx.QueryRow(`SELECT fingerprint, state FROM invocation_grants WHERE nonce = ?`, nonce).Scan(&storedFingerprint, &state)
	if err == sql.ErrNoRows || (err == nil && storedFingerprint != fingerprint) {
		return "", fmt.Errorf("invocation_grant_invalid")
	}
	if err != nil {
		return "", err
	}
	return state, nil
}

func issuedGrantReplacementStateError(state string) error {
	switch state {
	case "consumed":
		return fmt.Errorf("invocation_grant_replayed")
	case "revoked":
		return fmt.Errorf("invocation_grant_revoked")
	default:
		return fmt.Errorf("invocation_grant_invalid")
	}
}

// replaceExpiredGrantTx tries each fingerprint layout of previous. A layout
// miss continues. Any other error stops, including a successor nonce that is
// already stored: the shared coordinator transaction must not walk into the
// next layout after the first attempt has classified the row.
func replaceExpiredGrantTx(tx *sql.Tx, issuer *InvocationIssuer, previous, successor InvocationGrant) error {
	if issuer == nil {
		return fmt.Errorf("nil invocation issuer")
	}
	var last error
	for _, fingerprint := range issuer.FingerprintCandidates(previous) {
		err := replaceIssuedGrantTx(tx, previous.Nonce, fingerprint, successor)
		if err == nil {
			return nil
		}
		if !invocationGrantReplacementMiss(err) {
			return err
		}
		last = err
	}
	if last == nil {
		last = fmt.Errorf("invocation_grant_invalid")
	}
	return last
}
