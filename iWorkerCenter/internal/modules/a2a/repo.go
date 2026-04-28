package a2a

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	corea2a "github.com/RapidAI/CodeClaw/corelib/a2a"
)

// Repo persists A2A sessions as organization-owned records in iWorkerCenter.
// The core protocol remains in corelib/a2a; this repository stores the runtime
// session snapshot plus queryable metadata for dashboards and audit trails.
type Repo struct {
	write *sql.DB
	read  *sql.DB
}

func NewRepo(write, read *sql.DB) *Repo {
	return &Repo{write: write, read: read}
}

func (r *Repo) UpsertSession(tenantID string, session *corea2a.Session) error {
	if r == nil || r.write == nil {
		return fmt.Errorf("a2a repo is not configured")
	}
	if session == nil {
		return fmt.Errorf("session is nil")
	}
	payload, err := json.Marshal(session)
	if err != nil {
		return fmt.Errorf("marshal a2a session: %w", err)
	}
	_, err = r.write.Exec(`INSERT INTO a2a_sessions
		(tenant_id, id, org_unit_id, topic, status, decision_policy, participant_count, message_count, proposal_count, review_count, payload_json, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(tenant_id, id) DO UPDATE SET
			org_unit_id=excluded.org_unit_id,
			topic=excluded.topic,
			status=excluded.status,
			decision_policy=excluded.decision_policy,
			participant_count=excluded.participant_count,
			message_count=excluded.message_count,
			proposal_count=excluded.proposal_count,
			review_count=excluded.review_count,
			payload_json=excluded.payload_json,
			updated_at=excluded.updated_at`,
		tenantID,
		session.ID,
		session.OrgUnitID,
		session.Topic,
		string(session.Status),
		string(session.DecisionPolicy),
		len(session.Participants),
		len(session.Messages),
		len(session.Proposals),
		len(session.Reviews),
		string(payload),
		formatTime(session.CreatedAt),
		formatTime(session.UpdatedAt),
	)
	if err != nil {
		return fmt.Errorf("upsert a2a session: %w", err)
	}
	return nil
}

func (r *Repo) GetSession(tenantID, sessionID string) (*corea2a.Session, error) {
	if r == nil || r.read == nil {
		return nil, fmt.Errorf("a2a repo is not configured")
	}
	row := r.read.QueryRow(`SELECT payload_json FROM a2a_sessions WHERE tenant_id=? AND id=?`, tenantID, sessionID)
	return scanSession(row)
}

func (r *Repo) ListSessions(tenantID string, filter ListSessionsFilter) ([]*corea2a.Session, error) {
	if r == nil || r.read == nil {
		return nil, fmt.Errorf("a2a repo is not configured")
	}
	query := `SELECT payload_json FROM a2a_sessions WHERE tenant_id=?`
	args := []any{tenantID}
	if filter.OrgUnitID != "" {
		query += ` AND org_unit_id=?`
		args = append(args, filter.OrgUnitID)
	}
	if filter.Status != "" {
		query += ` AND status=?`
		args = append(args, string(filter.Status))
	}
	query += ` ORDER BY updated_at DESC, created_at DESC`
	rows, err := r.read.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("list a2a sessions: %w", err)
	}
	defer rows.Close()

	var sessions []*corea2a.Session
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return nil, err
		}
		var session corea2a.Session
		if err := json.Unmarshal([]byte(payload), &session); err != nil {
			return nil, fmt.Errorf("unmarshal a2a session: %w", err)
		}
		sessions = append(sessions, &session)
	}
	return sessions, rows.Err()
}

type scanner interface {
	Scan(dest ...any) error
}

func scanSession(row scanner) (*corea2a.Session, error) {
	var payload string
	if err := row.Scan(&payload); err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("session not found")
		}
		return nil, err
	}
	var session corea2a.Session
	if err := json.Unmarshal([]byte(payload), &session); err != nil {
		return nil, fmt.Errorf("unmarshal a2a session: %w", err)
	}
	return &session, nil
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		t = time.Now().UTC()
	}
	return t.UTC().Format(time.RFC3339Nano)
}
