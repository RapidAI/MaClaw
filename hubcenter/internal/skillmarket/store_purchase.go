package skillmarket

import (
	"context"
	"database/sql"
	"errors"
)

// 鈹€鈹€ PurchaseRepository implementation 鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€

func (s *Store) CreatePurchase(ctx context.Context, rec *PurchaseRecord) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO sm_purchase_records (id, buyer_email, buyer_id, skill_id, purchased_version,
			purchase_type, amount_paid, platform_fee, seller_earning, seller_id,
			key_status, api_key_id, status, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		rec.ID, rec.BuyerEmail, rec.BuyerID, rec.SkillID, rec.PurchasedVersion,
		rec.PurchaseType, rec.AmountPaid, rec.PlatformFee, rec.SellerEarning, rec.SellerID,
		rec.KeyStatus, rec.APIKeyID, rec.Status, fmtTime(rec.CreatedAt),
	)
	if err == nil {
		s.emitSync(ctx)
	}
	return err
}

func (s *Store) GetPurchaseByID(ctx context.Context, id string) (*PurchaseRecord, error) {
	return s.scanPurchase(s.readDB.QueryRowContext(ctx, `
		SELECT id, buyer_email, buyer_id, skill_id, purchased_version,
			purchase_type, amount_paid, platform_fee, seller_earning, seller_id,
			key_status, api_key_id, status, created_at
		FROM sm_purchase_records WHERE id = ?`, id))
}

func (s *Store) GetLatestPurchaseByBuyerAndSkill(ctx context.Context, buyerID, skillID string) (*PurchaseRecord, error) {
	return s.scanPurchase(s.readDB.QueryRowContext(ctx, `
		SELECT id, buyer_email, buyer_id, skill_id, purchased_version,
			purchase_type, amount_paid, platform_fee, seller_earning, seller_id,
			key_status, api_key_id, status, created_at
		FROM sm_purchase_records
		WHERE buyer_id = ? AND skill_id = ? AND status = 'active'
		ORDER BY purchased_version DESC
		LIMIT 1`, buyerID, skillID))
}

func (s *Store) UpdatePurchaseKeyStatus(ctx context.Context, id, keyStatus, apiKeyID string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE sm_purchase_records SET key_status = ?, api_key_id = ? WHERE id = ?`,
		keyStatus, apiKeyID, id)
	if err == nil {
		s.emitSync(ctx)
	}
	return err
}

func (s *Store) MarkPurchaseRefunded(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE sm_purchase_records SET status = 'refunded' WHERE id = ?`, id)
	if err == nil {
		s.emitSync(ctx)
	}
	return err
}

func (s *Store) ListPurchasesByBuyer(ctx context.Context, buyerID string, offset, limit int) ([]PurchaseRecord, int, error) {
	return s.listPurchases(ctx, "buyer_id", buyerID, offset, limit)
}

func (s *Store) ListPurchasesBySeller(ctx context.Context, sellerID string, offset, limit int) ([]PurchaseRecord, int, error) {
	return s.listPurchases(ctx, "seller_id", sellerID, offset, limit)
}

func (s *Store) GetOldestPendingKeyPurchase(ctx context.Context, skillID string) (*PurchaseRecord, error) {
	return s.scanPurchase(s.readDB.QueryRowContext(ctx, `
		SELECT id, buyer_email, buyer_id, skill_id, purchased_version,
			purchase_type, amount_paid, platform_fee, seller_earning, seller_id,
			key_status, api_key_id, status, created_at
		FROM sm_purchase_records
		WHERE skill_id = ? AND key_status = 'pending_key'
		ORDER BY created_at ASC
		LIMIT 1`, skillID))
}

// 鈹€鈹€ helpers 鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€

func (s *Store) listPurchases(ctx context.Context, col, val string, offset, limit int) ([]PurchaseRecord, int, error) {
	var total int
	if err := s.readDB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sm_purchase_records WHERE `+col+` = ?`, val,
	).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.readDB.QueryContext(ctx, `
		SELECT id, buyer_email, buyer_id, skill_id, purchased_version,
			purchase_type, amount_paid, platform_fee, seller_earning, seller_id,
			key_status, api_key_id, status, created_at
		FROM sm_purchase_records
		WHERE `+col+` = ?
		ORDER BY created_at DESC
		LIMIT ? OFFSET ?`, val, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var out []PurchaseRecord
	for rows.Next() {
		rec, err := s.scanPurchaseRow(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *rec)
	}
	return out, total, rows.Err()
}

func (s *Store) scanPurchase(row *sql.Row) (*PurchaseRecord, error) {
	var r PurchaseRecord
	var createdAt string
	err := row.Scan(
		&r.ID, &r.BuyerEmail, &r.BuyerID, &r.SkillID, &r.PurchasedVersion,
		&r.PurchaseType, &r.AmountPaid, &r.PlatformFee, &r.SellerEarning, &r.SellerID,
		&r.KeyStatus, &r.APIKeyID, &r.Status, &createdAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	r.CreatedAt = parseTime(createdAt)
	return &r, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func (s *Store) scanPurchaseRow(row rowScanner) (*PurchaseRecord, error) {
	var r PurchaseRecord
	var createdAt string
	err := row.Scan(
		&r.ID, &r.BuyerEmail, &r.BuyerID, &r.SkillID, &r.PurchasedVersion,
		&r.PurchaseType, &r.AmountPaid, &r.PlatformFee, &r.SellerEarning, &r.SellerID,
		&r.KeyStatus, &r.APIKeyID, &r.Status, &createdAt,
	)
	if err != nil {
		return nil, err
	}
	r.CreatedAt = parseTime(createdAt)
	return &r, nil
}
