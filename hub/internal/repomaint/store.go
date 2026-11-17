package repomaint

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

var (
	errNoDB         = errors.New("repo maint database is nil")
	errDuplicate    = errors.New("duplicate github delivery")
	errPendingLimit = errors.New("pending delivery limit")
	errNotFound     = errors.New("not found")
	errConflict     = errors.New("rule belongs to another machine")
)

type ruleRow struct {
	RuleID       string
	TenantID     string
	MachineID    string
	Owner        string
	Repo         string
	HookToken    string
	HookSecret   string
	GitHubHookID int64
	Enabled      bool
	PushBranch   string
	EventKinds   []string
	UpdatedAt    string
}

type deliveryRow struct {
	ID      string
	Status  string
	Event   RepoMaintEvent
	Created string
}

type store struct {
	db *sql.DB
}

func newStore(db *sql.DB) *store {
	return &store{db: db}
}

func (s *store) upsertRule(ctx context.Context, row ruleRow) error {
	existing, err := s.ruleByID(ctx, row.RuleID)
	if err != nil && !errors.Is(err, errNotFound) {
		return err
	}
	if err == nil && existing.MachineID != row.MachineID {
		return errConflict
	}
	kinds := []byte("[]")
	if row.EventKinds != nil {
		kinds, err = json.Marshal(row.EventKinds)
		if err != nil {
			return err
		}
	}
	now := nowText()
	enabled := 0
	if row.Enabled {
		enabled = 1
	}
	_, err = s.db.ExecContext(ctx, `
INSERT INTO github_repo_maint_rules (
    rule_id, tenant_id, machine_id, owner, repo, hook_token, hook_secret,
    github_hook_id, enabled, push_branch, event_kinds, updated_at
) VALUES (?,?,?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(rule_id) DO UPDATE SET
    tenant_id=excluded.tenant_id,
    owner=excluded.owner,
    repo=excluded.repo,
    hook_token=excluded.hook_token,
    hook_secret=excluded.hook_secret,
    github_hook_id=excluded.github_hook_id,
    enabled=excluded.enabled,
    push_branch=excluded.push_branch,
    event_kinds=excluded.event_kinds,
    updated_at=excluded.updated_at
WHERE github_repo_maint_rules.machine_id=excluded.machine_id
`, row.RuleID, row.TenantID, row.MachineID, row.Owner, row.Repo, row.HookToken, row.HookSecret,
		row.GitHubHookID, enabled, row.PushBranch, string(kinds), now)
	if isUniqueViolation(err) {
		return errConflict
	}
	return err
}

func (s *store) ruleByID(ctx context.Context, ruleID string) (ruleRow, error) {
	return s.scanRule(ctx, `SELECT rule_id, tenant_id, machine_id, owner, repo, hook_token, hook_secret,
github_hook_id, enabled, push_branch, event_kinds, updated_at
FROM github_repo_maint_rules WHERE rule_id=?`, ruleID)
}

func (s *store) ruleByToken(ctx context.Context, token string) (ruleRow, error) {
	return s.scanRule(ctx, `SELECT rule_id, tenant_id, machine_id, owner, repo, hook_token, hook_secret,
github_hook_id, enabled, push_branch, event_kinds, updated_at
FROM github_repo_maint_rules WHERE hook_token=?`, token)
}

func (s *store) scanRule(ctx context.Context, query, arg string) (ruleRow, error) {
	var row ruleRow
	var enabled int
	var kinds string
	err := s.db.QueryRowContext(ctx, query, arg).Scan(
		&row.RuleID, &row.TenantID, &row.MachineID, &row.Owner, &row.Repo, &row.HookToken, &row.HookSecret,
		&row.GitHubHookID, &enabled, &row.PushBranch, &kinds, &row.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return ruleRow{}, errNotFound
	}
	if err != nil {
		return ruleRow{}, err
	}
	row.Enabled = enabled != 0
	if strings.TrimSpace(kinds) == "" {
		row.EventKinds = []string{}
	} else if err := json.Unmarshal([]byte(kinds), &row.EventKinds); err != nil {
		row.EventKinds = []string{}
	}
	return row, nil
}

func (s *store) noteHookID(ctx context.Context, ruleID string, hookID int64) error {
	if hookID <= 0 {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `
UPDATE github_repo_maint_rules
SET github_hook_id=?, updated_at=?
WHERE rule_id=? AND github_hook_id=0
`, hookID, nowText(), ruleID)
	return err
}

func (s *store) deleteRule(ctx context.Context, ruleID, machineID string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM github_repo_maint_rules WHERE rule_id=? AND machine_id=?`, ruleID, machineID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return errNotFound
	}
	_, err = s.db.ExecContext(ctx, `DELETE FROM github_repo_maint_deliveries WHERE rule_id=? AND machine_id=? AND status=?`, ruleID, machineID, statusPending)
	return err
}

func (s *store) insertPending(ctx context.Context, machineID string, ev RepoMaintEvent, limit int) error {
	payload, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_, _ = conn.ExecContext(context.Background(), "ROLLBACK")
		}
	}()
	var exists int
	if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM github_repo_maint_deliveries WHERE github_delivery=?`, ev.GitHubDelivery).Scan(&exists); err != nil {
		return err
	}
	if exists > 0 {
		return errDuplicate
	}
	var pending int
	if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM github_repo_maint_deliveries WHERE machine_id=? AND status=?`, machineID, statusPending).Scan(&pending); err != nil {
		return err
	}
	if limit > 0 && pending >= limit {
		return errPendingLimit
	}
	now := nowText()
	_, err = conn.ExecContext(ctx, `
INSERT INTO github_repo_maint_deliveries (
    id, rule_id, machine_id, github_delivery, event_kind, object_id, payload_json, status, created_at, updated_at
) VALUES (?,?,?,?,?,?,?,?,?,?)
`, ev.DeliveryID, ev.RuleID, machineID, ev.GitHubDelivery, ev.EventKind, ev.ObjectID, string(payload), statusPending, now, now)
	if isUniqueViolation(err) {
		return errDuplicate
	}
	if err != nil {
		return err
	}
	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		return err
	}
	committed = true
	return nil
}

func (s *store) listPending(ctx context.Context, machineID string, limit int) ([]deliveryRow, error) {
	if limit <= 0 {
		limit = defaultPendingLimit
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT id, status, payload_json, created_at
FROM github_repo_maint_deliveries
WHERE machine_id=? AND status=?
ORDER BY created_at ASC
LIMIT ?
`, machineID, statusPending, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []deliveryRow
	for rows.Next() {
		var row deliveryRow
		var payload string
		if err := rows.Scan(&row.ID, &row.Status, &payload, &row.Created); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(payload), &row.Event); err != nil {
			continue
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (s *store) ack(ctx context.Context, id, machineID string) error {
	var status, owner string
	err := s.db.QueryRowContext(ctx, `SELECT status, machine_id FROM github_repo_maint_deliveries WHERE id=?`, id).Scan(&status, &owner)
	if errors.Is(err, sql.ErrNoRows) {
		return errNotFound
	}
	if err != nil {
		return err
	}
	if owner != machineID {
		return errNotFound
	}
	if status != statusPending {
		return nil
	}
	_, err = s.db.ExecContext(ctx, `
UPDATE github_repo_maint_deliveries SET status=?, updated_at=? WHERE id=? AND machine_id=? AND status=?
`, statusHandedOff, nowText(), id, machineID, statusPending)
	return err
}

func (s *store) deliveryStatus(ctx context.Context, id string) (string, error) {
	var status string
	err := s.db.QueryRowContext(ctx, `SELECT status FROM github_repo_maint_deliveries WHERE id=?`, id).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return "", errNotFound
	}
	return status, err
}

func (s *store) countDeliveries(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM github_repo_maint_deliveries`).Scan(&n)
	return n, err
}

func (s *store) sweepPending(ctx context.Context, cutoff string) (int, error) {
	res, err := s.db.ExecContext(ctx, `
DELETE FROM github_repo_maint_deliveries WHERE status=? AND created_at < ?
`, statusPending, cutoff)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	return int(n), err
}

func nowText() string {
	return time.Now().UTC().Format(timeLayout)
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	var se interface{ Code() int }
	if errors.As(err, &se) {
		switch se.Code() {
		case 19, 2067:
			return true
		}
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique") || strings.Contains(msg, "constraint failed")
}
