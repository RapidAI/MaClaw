package cloudworkspace

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

type IdempotencyRecord struct {
	Key         string
	PayloadHash string
	StatusCode  int
	Response    []byte
	Completed   bool
}

// AtomicIdempotencyEncoder turns the value produced inside a mutation's final
// SQLite transaction into the exact response retained for HTTP replay. The
// encoder runs before COMMIT; an encoding failure therefore rolls the mutation
// back together with its idempotency receipt.
type AtomicIdempotencyEncoder func(value any) (statusCode int, response []byte, err error)

type atomicIdempotencyContextKey struct{}

type atomicIdempotencyReceipt struct {
	tenantID         string
	userID           string
	workspaceID      string
	clientInstanceID string
	key              string
	payloadHash      string
	createdAt        time.Time
	encoder          AtomicIdempotencyEncoder

	mu        sync.Mutex
	finalized bool
	committed bool
}

// IdempotencyReplayError tells a handler that another process committed the
// same key after this request's initial lookup. The losing mutation has already
// been rolled back and the original response is safe to replay.
type IdempotencyReplayError struct {
	Record *IdempotencyRecord
}

func (e *IdempotencyReplayError) Error() string {
	return "cloud workspace idempotent response committed concurrently"
}

// IdempotencyReplay returns the durable response carried by a concurrent
// atomic-idempotency winner.
func IdempotencyReplay(err error) (*IdempotencyRecord, bool) {
	var replay *IdempotencyReplayError
	if !errors.As(err, &replay) || replay == nil || replay.Record == nil {
		return nil, false
	}
	return replay.Record, true
}

// PrepareAtomicIdempotency checks for a durable response without reserving a
// pending row. The final business transaction inserts the committed response.
// This removes the old mutation-COMMIT/FinishIdempotency crash window while a
// UNIQUE conflict still serializes concurrent requests using the same key.
func (s *Store) PrepareAtomicIdempotency(ctx context.Context, tenantID, userID, workspaceID, clientInstanceID, key, payloadHash string, now time.Time, encoder AtomicIdempotencyEncoder) (context.Context, *IdempotencyRecord, error) {
	if s == nil || s.db == nil {
		return ctx, nil, ErrUnavailable
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return ctx, nil, nil
	}
	if len(key) > 200 || strings.TrimSpace(payloadHash) == "" || encoder == nil {
		return ctx, nil, ErrInvalidInput
	}
	tenantID = store.NormalizeTenantID(tenantID)
	userID = strings.TrimSpace(userID)
	workspaceID = strings.TrimSpace(workspaceID)
	clientInstanceID = strings.TrimSpace(clientInstanceID)
	now = now.UTC()
	var replay *IdempotencyRecord
	err := s.withImmediate(ctx, func(q queryer) error {
		var storedHash, status, expiresAt string
		var statusCode int
		var response []byte
		err := q.QueryRowContext(ctx, `SELECT payload_hash, status, status_code, response_json, expires_at FROM cloud_workspace_idempotency WHERE tenant_id = ? AND user_id = ? AND workspace_id = ? AND idempotency_key = ?`, tenantID, userID, workspaceID, key).Scan(&storedHash, &status, &statusCode, &response, &expiresAt)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if status == "committed" {
			if expiry, parseErr := time.Parse(time.RFC3339, expiresAt); parseErr == nil && !now.Before(expiry) {
				_, err = q.ExecContext(ctx, `DELETE FROM cloud_workspace_idempotency WHERE tenant_id = ? AND user_id = ? AND workspace_id = ? AND idempotency_key = ? AND status = 'committed'`, tenantID, userID, workspaceID, key)
				return err
			}
			if storedHash != payloadHash {
				return ErrIdempotencyKeyReused
			}
			replay = &IdempotencyRecord{Key: key, PayloadHash: storedHash, StatusCode: statusCode, Response: append([]byte(nil), response...), Completed: true}
			return nil
		}
		// Rows created by the legacy reserve-then-finish protocol remain
		// deliberately non-reclaimable: their mutation outcome is ambiguous.
		if storedHash != payloadHash {
			return ErrIdempotencyKeyReused
		}
		return ErrIdempotencyInProgress
	})
	if err != nil || replay != nil {
		return ctx, replay, err
	}
	receipt := &atomicIdempotencyReceipt{
		tenantID: tenantID, userID: userID, workspaceID: workspaceID,
		clientInstanceID: clientInstanceID, key: key, payloadHash: payloadHash,
		createdAt: now, encoder: encoder,
	}
	return context.WithValue(ctx, atomicIdempotencyContextKey{}, receipt), nil, nil
}

func atomicIdempotencyFromContext(ctx context.Context) *atomicIdempotencyReceipt {
	if ctx == nil {
		return nil
	}
	receipt, _ := ctx.Value(atomicIdempotencyContextKey{}).(*atomicIdempotencyReceipt)
	return receipt
}

func preflightAtomicIdempotency(ctx context.Context, q queryer) error {
	receipt := atomicIdempotencyFromContext(ctx)
	if receipt == nil {
		return nil
	}
	var storedHash, status string
	var storedCode int
	var storedResponse []byte
	err := q.QueryRowContext(ctx, `SELECT payload_hash, status, status_code, response_json FROM cloud_workspace_idempotency WHERE tenant_id = ? AND user_id = ? AND workspace_id = ? AND idempotency_key = ?`, receipt.tenantID, receipt.userID, receipt.workspaceID, receipt.key).Scan(&storedHash, &status, &storedCode, &storedResponse)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if storedHash != receipt.payloadHash {
		return ErrIdempotencyKeyReused
	}
	if status != "committed" {
		return ErrIdempotencyInProgress
	}
	return &IdempotencyReplayError{Record: &IdempotencyRecord{Key: receipt.key, PayloadHash: storedHash, StatusCode: storedCode, Response: append([]byte(nil), storedResponse...), Completed: true}}
}

// stageAtomicIdempotency must be called from the final business transaction.
// It inserts a committed receipt in that same transaction. If another process
// won the key, returning IdempotencyReplayError rolls back the local mutation.
func stageAtomicIdempotency(ctx context.Context, q queryer, value any) error {
	receipt := atomicIdempotencyFromContext(ctx)
	if receipt == nil {
		return nil
	}
	statusCode, response, err := receipt.encoder(value)
	if err != nil {
		return err
	}
	if statusCode < 100 || statusCode > 599 {
		return ErrInvalidInput
	}
	createdAt := receipt.createdAt.UTC()
	_, err = q.ExecContext(ctx, `INSERT INTO cloud_workspace_idempotency (
		tenant_id, user_id, workspace_id, client_instance_id, idempotency_key,
		payload_hash, status, status_code, response_json, created_at, expires_at
	) VALUES (?, ?, ?, ?, ?, ?, 'committed', ?, ?, ?, ?)`,
		receipt.tenantID, receipt.userID, receipt.workspaceID, receipt.clientInstanceID,
		receipt.key, receipt.payloadHash, statusCode, response,
		createdAt.Format(time.RFC3339), createdAt.Add(24*time.Hour).Format(time.RFC3339),
	)
	if err != nil {
		if !isUniqueConstraintError(err) {
			return err
		}
		var storedHash, status string
		var storedCode int
		var storedResponse []byte
		lookupErr := q.QueryRowContext(ctx, `SELECT payload_hash, status, status_code, response_json FROM cloud_workspace_idempotency WHERE tenant_id = ? AND user_id = ? AND workspace_id = ? AND idempotency_key = ?`, receipt.tenantID, receipt.userID, receipt.workspaceID, receipt.key).Scan(&storedHash, &status, &storedCode, &storedResponse)
		if lookupErr != nil {
			return lookupErr
		}
		if storedHash != receipt.payloadHash {
			return ErrIdempotencyKeyReused
		}
		if status != "committed" {
			return ErrIdempotencyInProgress
		}
		return &IdempotencyReplayError{Record: &IdempotencyRecord{Key: receipt.key, PayloadHash: storedHash, StatusCode: storedCode, Response: append([]byte(nil), storedResponse...), Completed: true}}
	}
	receipt.mu.Lock()
	receipt.finalized = true
	receipt.mu.Unlock()
	return nil
}

func consumeAtomicIdempotencyFinalized(ctx context.Context) bool {
	receipt := atomicIdempotencyFromContext(ctx)
	if receipt == nil {
		return false
	}
	receipt.mu.Lock()
	defer receipt.mu.Unlock()
	finalized := receipt.finalized
	receipt.finalized = false
	return finalized
}

func clearAtomicIdempotencyFinalized(ctx context.Context) {
	receipt := atomicIdempotencyFromContext(ctx)
	if receipt == nil {
		return
	}
	receipt.mu.Lock()
	receipt.finalized = false
	receipt.mu.Unlock()
}

func markAtomicIdempotencyCommitted(ctx context.Context) {
	receipt := atomicIdempotencyFromContext(ctx)
	if receipt == nil {
		return
	}
	receipt.mu.Lock()
	receipt.committed = true
	receipt.mu.Unlock()
}

func atomicIdempotencyCommitted(ctx context.Context) bool {
	receipt := atomicIdempotencyFromContext(ctx)
	if receipt == nil {
		return false
	}
	receipt.mu.Lock()
	defer receipt.mu.Unlock()
	return receipt.committed
}

// BeginIdempotency reserves a key or returns the durable original response.
// Keys are scoped to tenant/user/workspace; a different payload can never
// reuse an existing key.
func (s *Store) BeginIdempotency(ctx context.Context, tenantID, userID, workspaceID, clientInstanceID, key, payloadHash string, now time.Time) (*IdempotencyRecord, error) {
	if s == nil || s.db == nil {
		return nil, ErrUnavailable
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, nil
	}
	if len(key) > 200 || strings.TrimSpace(payloadHash) == "" {
		return nil, ErrInvalidInput
	}
	tenantID = store.NormalizeTenantID(tenantID)
	userID = strings.TrimSpace(userID)
	workspaceID = strings.TrimSpace(workspaceID)
	now = now.UTC()
	var out *IdempotencyRecord
	err := s.withImmediate(ctx, func(q queryer) error {
		var storedHash, status, expiresAt string
		var statusCode int
		var response []byte
		err := q.QueryRowContext(ctx, `SELECT payload_hash, status, status_code, response_json, expires_at FROM cloud_workspace_idempotency WHERE tenant_id = ? AND user_id = ? AND workspace_id = ? AND idempotency_key = ?`, tenantID, userID, workspaceID, key).Scan(&storedHash, &status, &statusCode, &response, &expiresAt)
		if errors.Is(err, sql.ErrNoRows) {
			_, err = q.ExecContext(ctx, `INSERT INTO cloud_workspace_idempotency (tenant_id, user_id, workspace_id, client_instance_id, idempotency_key, payload_hash, status, status_code, response_json, created_at, expires_at) VALUES (?, ?, ?, ?, ?, ?, 'pending', 0, NULL, ?, ?)`, tenantID, userID, workspaceID, strings.TrimSpace(clientInstanceID), key, payloadHash, now.Format(time.RFC3339), now.Add(24*time.Hour).Format(time.RFC3339))
			return err
		}
		if err != nil {
			return err
		}
		// Committed responses are retained for a bounded replay window. Once
		// that window elapses, the key may be recycled (including with a new
		// payload). Pending rows are deliberately never recycled by time alone:
		// doing so could duplicate a side effect whose response was lost after
		// the original transaction committed.
		if expiry, parseErr := time.Parse(time.RFC3339, expiresAt); status == "committed" && parseErr == nil && !now.Before(expiry) {
			_, err = q.ExecContext(ctx, `DELETE FROM cloud_workspace_idempotency WHERE tenant_id = ? AND user_id = ? AND workspace_id = ? AND idempotency_key = ? AND status = 'committed'`, tenantID, userID, workspaceID, key)
			if err != nil {
				return err
			}
			_, err = q.ExecContext(ctx, `INSERT INTO cloud_workspace_idempotency (tenant_id, user_id, workspace_id, client_instance_id, idempotency_key, payload_hash, status, status_code, response_json, created_at, expires_at) VALUES (?, ?, ?, ?, ?, ?, 'pending', 0, NULL, ?, ?)`, tenantID, userID, workspaceID, strings.TrimSpace(clientInstanceID), key, payloadHash, now.Format(time.RFC3339), now.Add(24*time.Hour).Format(time.RFC3339))
			return err
		}
		if storedHash != payloadHash {
			return ErrIdempotencyKeyReused
		}
		if status != "committed" {
			// Never reclaim a pending key solely because it is old. The original
			// handler may have committed its side effect and crashed before
			// FinishIdempotency; replacing the row here would permit a retry to
			// execute that side effect a second time. Pending rows remain owned by
			// the original attempt until it explicitly finishes/cancels (or the
			// workspace is purged). APIs with a durable operation
			// (for example task provisioning) expose that operation's status so a
			// client can recover without issuing a second mutation.
			return ErrIdempotencyInProgress
		}
		out = &IdempotencyRecord{Key: key, PayloadHash: storedHash, StatusCode: statusCode, Response: append([]byte(nil), response...), Completed: true}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Store) FinishIdempotency(ctx context.Context, tenantID, userID, workspaceID, key, payloadHash string, statusCode int, response []byte, now time.Time) error {
	return s.FinishIdempotencyForClient(ctx, tenantID, userID, workspaceID, "", key, payloadHash, statusCode, response, now)
}

func (s *Store) FinishIdempotencyForClient(ctx context.Context, tenantID, userID, workspaceID, clientInstanceID, key, payloadHash string, statusCode int, response []byte, now time.Time) error {
	if s == nil || s.db == nil || strings.TrimSpace(key) == "" {
		return nil
	}
	// Atomic handlers already persisted the response in the business
	// transaction. Keep legacy Finish calls source-compatible without taking a
	// second SQLite write lock.
	if atomicIdempotencyCommitted(ctx) {
		return nil
	}
	tenantID = store.NormalizeTenantID(tenantID)
	return s.withImmediate(ctx, func(q queryer) error {
		query := `UPDATE cloud_workspace_idempotency SET status = 'committed', status_code = ?, response_json = ? WHERE tenant_id = ? AND user_id = ? AND workspace_id = ? AND idempotency_key = ? AND payload_hash = ?`
		args := []any{statusCode, response, tenantID, strings.TrimSpace(userID), strings.TrimSpace(workspaceID), strings.TrimSpace(key), payloadHash}
		if strings.TrimSpace(clientInstanceID) != "" {
			query += ` AND client_instance_id = ?`
			args = append(args, strings.TrimSpace(clientInstanceID))
		}
		res, err := q.ExecContext(ctx, query, args...)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrIdempotencyKeyReused
		}
		return nil
	})
}

func (s *Store) CancelIdempotency(ctx context.Context, tenantID, userID, workspaceID, key, payloadHash string) error {
	return s.CancelIdempotencyForClient(ctx, tenantID, userID, workspaceID, "", key, payloadHash)
}

func (s *Store) CancelIdempotencyForClient(ctx context.Context, tenantID, userID, workspaceID, clientInstanceID, key, payloadHash string) error {
	if s == nil || s.db == nil || strings.TrimSpace(key) == "" {
		return nil
	}
	query := `DELETE FROM cloud_workspace_idempotency WHERE tenant_id = ? AND user_id = ? AND workspace_id = ? AND idempotency_key = ? AND payload_hash = ? AND status = 'pending'`
	args := []any{store.NormalizeTenantID(tenantID), strings.TrimSpace(userID), strings.TrimSpace(workspaceID), strings.TrimSpace(key), payloadHash}
	if strings.TrimSpace(clientInstanceID) != "" {
		query += ` AND client_instance_id = ?`
		args = append(args, strings.TrimSpace(clientInstanceID))
	}
	_, err := s.db.ExecContext(ctx, query, args...)
	return err
}

func (s *Store) PurgeExpiredIdempotency(ctx context.Context, now time.Time) error {
	if s == nil || s.db == nil {
		return ErrUnavailable
	}
	// Only committed responses have a safe retention boundary. A pending row
	// may represent a mutation that committed immediately before a process
	// crash; deleting it would allow a later retry to duplicate that mutation.
	_, err := s.db.ExecContext(ctx, `DELETE FROM cloud_workspace_idempotency WHERE status = 'committed' AND expires_at <= ?`, now.UTC().Format(time.RFC3339))
	return err
}
