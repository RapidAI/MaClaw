package workflow

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"time"
)

// ConfirmationType distinguishes executor confirmation from notifier acknowledgment.
type ConfirmationType string

const (
	ConfirmTypeExecutor ConfirmationType = "executor"
	ConfirmTypeNotifier ConfirmationType = "notifier"
)

// ConfirmationStatus tracks the state of a single confirmation request.
type ConfirmationStatus string

const (
	ConfirmPending    ConfirmationStatus = "pending"
	ConfirmConfirmed  ConfirmationStatus = "confirmed"
	ConfirmAutoClosed ConfirmationStatus = "auto_closed"
)

// Confirmation represents a single confirmation/acknowledgment record.
type Confirmation struct {
	ID                    string             `json:"id"`
	InstanceID            string             `json:"instance_id"`
	RecipientID           string             `json:"recipient_id"`
	Type                  ConfirmationType   `json:"type"`
	Status                ConfirmationStatus `json:"status"`
	Notes                 string             `json:"notes,omitempty"`
	TimeoutHours          int                `json:"timeout_hours"`
	MaxReminders          int                `json:"max_reminders"`
	RemindersSent         int                `json:"reminders_sent"`
	ReminderIntervalHours int                `json:"reminder_interval_hours"`
	LastReminderAt        *time.Time         `json:"last_reminder_at,omitempty"`
	ConfirmedAt           *time.Time         `json:"confirmed_at,omitempty"`
	AutoClosedAt          *time.Time         `json:"auto_closed_at,omitempty"`
	AutoCloseReason       string             `json:"auto_close_reason,omitempty"`
	CreatedAt             time.Time          `json:"created_at"`
}

// ConfirmationStore provides CRUD for confirmation records.
type ConfirmationStore interface {
	// Create inserts a new confirmation record.
	Create(ctx context.Context, conf *Confirmation) error
	// Get retrieves a confirmation by ID.
	Get(ctx context.Context, id string) (*Confirmation, error)
	// UpdateStatus atomically updates the confirmation status.
	// Implementations MUST use a conditional update (WHERE status = 'pending')
	// to prevent double-confirmation race conditions.
	// Returns an error if the update fails (e.g., already confirmed by another request).
	UpdateStatus(ctx context.Context, id string, status ConfirmationStatus, notes string) error
	// IncrementReminders increments reminders_sent and updates last_reminder_at.
	IncrementReminders(ctx context.Context, id string) error
	// ListPending returns all pending confirmations for a given recipient.
	ListPending(ctx context.Context, recipientID string) ([]Confirmation, error)
	// ListByInstance returns all confirmations for a given workflow instance.
	ListByInstance(ctx context.Context, instanceID string) ([]Confirmation, error)
	// FindOverdue returns confirmations that are pending and past their timeout.
	FindOverdue(ctx context.Context) ([]Confirmation, error)
}

// PgConfirmationStore implements ConfirmationStore backed by PostgreSQL/SQLite.
type PgConfirmationStore struct {
	db *sql.DB
}

// NewPgConfirmationStore creates a new PgConfirmationStore using the given database connection.
func NewPgConfirmationStore(db *sql.DB) *PgConfirmationStore {
	return &PgConfirmationStore{db: db}
}

func (s *PgConfirmationStore) Create(ctx context.Context, conf *Confirmation) error {
	if conf.ID == "" {
		conf.ID = generateConfirmationID()
	}
	if conf.Status == "" {
		conf.Status = ConfirmPending
	}
	if conf.CreatedAt.IsZero() {
		conf.CreatedAt = time.Now().UTC()
	}

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO confirmations (id, instance_id, recipient_id, type, status, notes,
			timeout_hours, max_reminders, reminders_sent, reminder_interval_hours,
			last_reminder_at, confirmed_at, auto_closed_at, auto_close_reason, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)`,
		conf.ID, conf.InstanceID, conf.RecipientID, conf.Type, conf.Status, conf.Notes,
		conf.TimeoutHours, conf.MaxReminders, conf.RemindersSent, conf.ReminderIntervalHours,
		formatNullableTime(conf.LastReminderAt), formatNullableTime(conf.ConfirmedAt),
		formatNullableTime(conf.AutoClosedAt), conf.AutoCloseReason,
		conf.CreatedAt.Format(time.RFC3339Nano),
	)
	return err
}

func (s *PgConfirmationStore) Get(ctx context.Context, id string) (*Confirmation, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, instance_id, recipient_id, type, status, notes,
			timeout_hours, max_reminders, reminders_sent, reminder_interval_hours,
			last_reminder_at, confirmed_at, auto_closed_at, auto_close_reason, created_at
		 FROM confirmations WHERE id = $1`, id)

	conf, err := scanConfirmation(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return conf, nil
}

func (s *PgConfirmationStore) UpdateStatus(ctx context.Context, id string, status ConfirmationStatus, notes string) error {
	now := time.Now().UTC()
	nowStr := now.Format(time.RFC3339Nano)

	var confirmedAt, autoClosedAt *string
	switch status {
	case ConfirmConfirmed:
		confirmedAt = &nowStr
	case ConfirmAutoClosed:
		autoClosedAt = &nowStr
	}

	_, err := s.db.ExecContext(ctx,
		`UPDATE confirmations SET status = $1, notes = $2, confirmed_at = $3, auto_closed_at = $4
		 WHERE id = $5`,
		status, notes, confirmedAt, autoClosedAt, id)
	return err
}

func (s *PgConfirmationStore) IncrementReminders(ctx context.Context, id string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx,
		`UPDATE confirmations SET reminders_sent = reminders_sent + 1, last_reminder_at = $1
		 WHERE id = $2`,
		now, id)
	return err
}

func (s *PgConfirmationStore) ListPending(ctx context.Context, recipientID string) ([]Confirmation, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, instance_id, recipient_id, type, status, notes,
			timeout_hours, max_reminders, reminders_sent, reminder_interval_hours,
			last_reminder_at, confirmed_at, auto_closed_at, auto_close_reason, created_at
		 FROM confirmations WHERE status = $1 AND recipient_id = $2
		 ORDER BY created_at ASC`,
		string(ConfirmPending), recipientID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanConfirmations(rows)
}

func (s *PgConfirmationStore) ListByInstance(ctx context.Context, instanceID string) ([]Confirmation, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, instance_id, recipient_id, type, status, notes,
			timeout_hours, max_reminders, reminders_sent, reminder_interval_hours,
			last_reminder_at, confirmed_at, auto_closed_at, auto_close_reason, created_at
		 FROM confirmations WHERE instance_id = $1
		 ORDER BY created_at ASC`,
		instanceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanConfirmations(rows)
}

func (s *PgConfirmationStore) FindOverdue(ctx context.Context) ([]Confirmation, error) {
	// Find confirmations that are:
	// 1. Still pending
	// 2. Created more than timeout_hours ago (past their deadline)
	// We use the created_at + timeout_hours to determine if overdue.
	// SQLite datetime arithmetic: datetime(created_at, '+N hours')
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, instance_id, recipient_id, type, status, notes,
			timeout_hours, max_reminders, reminders_sent, reminder_interval_hours,
			last_reminder_at, confirmed_at, auto_closed_at, auto_close_reason, created_at
		 FROM confirmations
		 WHERE status = $1
		   AND datetime(created_at, '+' || timeout_hours || ' hours') <= datetime('now')
		 ORDER BY created_at ASC`,
		string(ConfirmPending))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanConfirmations(rows)
}

// --- helpers ---

func scanConfirmation(row *sql.Row) (*Confirmation, error) {
	var conf Confirmation
	var lastReminderAt, confirmedAt, autoClosedAt, createdAt sql.NullString
	err := row.Scan(
		&conf.ID, &conf.InstanceID, &conf.RecipientID, &conf.Type, &conf.Status, &conf.Notes,
		&conf.TimeoutHours, &conf.MaxReminders, &conf.RemindersSent, &conf.ReminderIntervalHours,
		&lastReminderAt, &confirmedAt, &autoClosedAt, &conf.AutoCloseReason, &createdAt,
	)
	if err != nil {
		return nil, err
	}
	conf.LastReminderAt = parseNullableTime(lastReminderAt)
	conf.ConfirmedAt = parseNullableTime(confirmedAt)
	conf.AutoClosedAt = parseNullableTime(autoClosedAt)
	if createdAt.Valid {
		if t, err := time.Parse(time.RFC3339Nano, createdAt.String); err == nil {
			conf.CreatedAt = t
		}
	}
	return &conf, nil
}

func scanConfirmations(rows *sql.Rows) ([]Confirmation, error) {
	var results []Confirmation
	for rows.Next() {
		var conf Confirmation
		var lastReminderAt, confirmedAt, autoClosedAt, createdAt sql.NullString
		err := rows.Scan(
			&conf.ID, &conf.InstanceID, &conf.RecipientID, &conf.Type, &conf.Status, &conf.Notes,
			&conf.TimeoutHours, &conf.MaxReminders, &conf.RemindersSent, &conf.ReminderIntervalHours,
			&lastReminderAt, &confirmedAt, &autoClosedAt, &conf.AutoCloseReason, &createdAt,
		)
		if err != nil {
			return nil, err
		}
		conf.LastReminderAt = parseNullableTime(lastReminderAt)
		conf.ConfirmedAt = parseNullableTime(confirmedAt)
		conf.AutoClosedAt = parseNullableTime(autoClosedAt)
		if createdAt.Valid {
			if t, err := time.Parse(time.RFC3339Nano, createdAt.String); err == nil {
				conf.CreatedAt = t
			}
		}
		results = append(results, conf)
	}
	return results, rows.Err()
}

func parseNullableTime(ns sql.NullString) *time.Time {
	if !ns.Valid || ns.String == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339Nano, ns.String)
	if err != nil {
		return nil
	}
	return &t
}

func formatNullableTime(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.Format(time.RFC3339Nano)
	return &s
}

func generateConfirmationID() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("conf_%d", time.Now().UnixNano())
	}
	return "conf_" + hex.EncodeToString(b)
}
