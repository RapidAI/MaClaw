package workflow

import (
	"context"
	"time"
)

// DefaultAuditPageSize is the default number of records per page for audit queries.
const DefaultAuditPageSize = 100

// AuditEntry represents a single immutable audit record.
type AuditEntry struct {
	TenantID    string    `json:"tenant_id,omitempty"`
	ID          string    `json:"id"`
	InstanceID  string    `json:"instance_id"`
	NodeID      string    `json:"node_id,omitempty"`
	EventType   string    `json:"event_type"`
	ActorID     string    `json:"actor_id,omitempty"`
	Decision    string    `json:"decision,omitempty"`
	MatchedRule string    `json:"matched_rule,omitempty"`
	Rationale   string    `json:"rationale,omitempty"`
	Details     string    `json:"details,omitempty"`
	Timestamp   time.Time `json:"timestamp"` // UTC, millisecond precision
}

// AuditStore provides append-only access to audit records.
// There are no Update or Delete methods; the audit trail is immutable.
type AuditStore interface {
	// Append writes a new audit entry. Entries cannot be modified or deleted.
	// If the entry's Timestamp is zero, it is set to the current UTC time
	// truncated to millisecond precision.
	Append(ctx context.Context, entry *AuditEntry) error

	// QueryByInstance returns all entries for a workflow instance, chronologically.
	// Returns the matching entries, total count, and any error.
	// pageSize is capped at DefaultAuditPageSize (100) if larger or non-positive.
	QueryByInstance(ctx context.Context, instanceID string, page, pageSize int) ([]AuditEntry, int, error)

	// QueryByApprover returns entries where the given VE acted as approver.
	// Returns the matching entries, total count, and any error.
	// pageSize is capped at DefaultAuditPageSize (100) if larger or non-positive.
	QueryByApprover(ctx context.Context, approverID string, page, pageSize int) ([]AuditEntry, int, error)

	// QueryByTimeRange returns entries within a time window.
	// Returns the matching entries, total count, and any error.
	// pageSize is capped at DefaultAuditPageSize (100) if larger or non-positive.
	QueryByTimeRange(ctx context.Context, start, end time.Time, page, pageSize int) ([]AuditEntry, int, error)

	// QueryByDecision returns entries filtered by decision outcome.
	// Returns the matching entries, total count, and any error.
	// pageSize is capped at DefaultAuditPageSize (100) if larger or non-positive.
	QueryByDecision(ctx context.Context, decision string, page, pageSize int) ([]AuditEntry, int, error)
}

// NormalizeAuditTimestamp ensures the timestamp is in UTC with millisecond precision.
// If t is zero, it returns the current UTC time truncated to milliseconds.
func NormalizeAuditTimestamp(t time.Time) time.Time {
	if t.IsZero() {
		t = time.Now()
	}
	return t.UTC().Truncate(time.Millisecond)
}

// NormalizePageSize ensures pageSize is within valid bounds for audit queries.
// Returns DefaultAuditPageSize if pageSize is non-positive or exceeds the default.
func NormalizePageSize(pageSize int) int {
	if pageSize <= 0 || pageSize > DefaultAuditPageSize {
		return DefaultAuditPageSize
	}
	return pageSize
}
