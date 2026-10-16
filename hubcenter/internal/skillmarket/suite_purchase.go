package skillmarket

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

func (s *Store) CreateSuitePurchase(ctx context.Context, rec *SuitePurchaseRecord) error {
	if rec == nil || rec.ID == "" || rec.SuiteID == "" {
		return fmt.Errorf("suite purchase id and suite id are required")
	}
	if rec.AmountPaid < 0 {
		return fmt.Errorf("suite purchase amount cannot be negative")
	}
	if len(rec.MemberSkillIDs) > 32 {
		return fmt.Errorf("suite purchase has too many members")
	}
	if rec.Status == "" {
		rec.Status = "active"
	}
	if rec.CreatedAt.IsZero() {
		rec.CreatedAt = time.Now().UTC()
	}
	members, err := json.Marshal(rec.MemberSkillIDs)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO sm_suite_purchases (id,suite_id,member_skill_ids,buyer_email,buyer_id,amount_paid,version,status,created_at) VALUES (?,?,?,?,?,?,?,?,?)`, rec.ID, rec.SuiteID, string(members), rec.BuyerEmail, rec.BuyerID, rec.AmountPaid, rec.Version, rec.Status, rec.CreatedAt.Format(time.RFC3339Nano))
	return err
}

func (s *Store) MarkSuitePurchaseRefunded(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE sm_suite_purchases SET status='refunded' WHERE id=?`, id)
	return err
}

func (s *Store) GetSuitePurchaseByID(ctx context.Context, id string) (*SuitePurchaseRecord, error) {
	var rec SuitePurchaseRecord
	var members, created string
	err := s.db.QueryRowContext(ctx, `SELECT id,suite_id,member_skill_ids,buyer_email,buyer_id,amount_paid,version,status,created_at FROM sm_suite_purchases WHERE id=?`, id).Scan(&rec.ID, &rec.SuiteID, &members, &rec.BuyerEmail, &rec.BuyerID, &rec.AmountPaid, &rec.Version, &rec.Status, &created)
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(members), &rec.MemberSkillIDs)
	rec.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	return &rec, nil
}

func (s *Store) GetActiveSuitePurchase(ctx context.Context, suiteID, buyerID string) (*SuitePurchaseRecord, error) {
	var id string
	err := s.db.QueryRowContext(ctx, `SELECT id FROM sm_suite_purchases WHERE suite_id=? AND buyer_id=? AND status='active' ORDER BY created_at DESC LIMIT 1`, suiteID, buyerID).Scan(&id)
	if err != nil {
		return nil, err
	}
	return s.GetSuitePurchaseByID(ctx, id)
}

func (s *Store) RecordSuiteAuditEvent(ctx context.Context, suiteID, eventType, actorID, purchaseID string, members []string) error {
	data, err := json.Marshal(members)
	if err != nil {
		return err
	}
	eventID := "suite-audit-" + generateID()
	_, err = s.db.ExecContext(ctx, `INSERT INTO sm_suite_audit_events (id,suite_id,member_skill_ids,event_type,actor_id,purchase_id,created_at) VALUES (?,?,?,?,?,?,?)`, eventID, suiteID, string(data), eventType, actorID, purchaseID, time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

func (s *Store) ListSuitePurchases(ctx context.Context, suiteID, buyerID string, limit int) ([]SuitePurchaseRecord, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	where := "1=1"
	args := []any{}
	if suiteID != "" {
		where += " AND suite_id=?"
		args = append(args, suiteID)
	}
	if buyerID != "" {
		where += " AND buyer_id=?"
		args = append(args, buyerID)
	}
	args = append(args, limit)
	rows, err := s.readDB.QueryContext(ctx, `SELECT id,suite_id,member_skill_ids,buyer_email,buyer_id,amount_paid,version,status,created_at FROM sm_suite_purchases WHERE `+where+` ORDER BY created_at DESC LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SuitePurchaseRecord
	for rows.Next() {
		var r SuitePurchaseRecord
		var members, created string
		if err := rows.Scan(&r.ID, &r.SuiteID, &members, &r.BuyerEmail, &r.BuyerID, &r.AmountPaid, &r.Version, &r.Status, &created); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(members), &r.MemberSkillIDs)
		r.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		out = append(out, r)
	}
	return out, rows.Err()
}

// ListSuitePurchasesByEmail is the buyer-facing variant used by clients that
// have not yet resolved a SkillMarket user ID.
func (s *Store) ListSuitePurchasesByEmail(ctx context.Context, suiteID, email string, limit int) ([]SuitePurchaseRecord, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.readDB.QueryContext(ctx, `SELECT id,suite_id,member_skill_ids,buyer_email,buyer_id,amount_paid,version,status,created_at FROM sm_suite_purchases WHERE (?='' OR suite_id=?) AND LOWER(buyer_email)=LOWER(?) ORDER BY created_at DESC LIMIT ?`, suiteID, suiteID, email, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SuitePurchaseRecord
	for rows.Next() {
		var r SuitePurchaseRecord
		var members, created string
		if err := rows.Scan(&r.ID, &r.SuiteID, &members, &r.BuyerEmail, &r.BuyerID, &r.AmountPaid, &r.Version, &r.Status, &created); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(members), &r.MemberSkillIDs)
		r.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) ListSuiteAuditEvents(ctx context.Context, suiteID string, limit int) ([]SuiteAuditEvent, error) {
	return s.ListSuiteAuditEventsFiltered(ctx, suiteID, "", limit)
}

func (s *Store) ListSuiteAuditEventsFiltered(ctx context.Context, suiteID, eventType string, limit int) ([]SuiteAuditEvent, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	rows, err := s.readDB.QueryContext(ctx, `SELECT id,suite_id,member_skill_ids,event_type,actor_id,purchase_id,created_at FROM sm_suite_audit_events WHERE (?='' OR suite_id=?) AND (?='' OR event_type=?) ORDER BY created_at DESC LIMIT ?`, suiteID, suiteID, eventType, eventType, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SuiteAuditEvent
	for rows.Next() {
		var e SuiteAuditEvent
		var members, created string
		if err := rows.Scan(&e.ID, &e.SuiteID, &members, &e.EventType, &e.ActorID, &e.PurchaseID, &created); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(members), &e.MemberSkillIDs)
		e.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		out = append(out, e)
	}
	return out, rows.Err()
}
