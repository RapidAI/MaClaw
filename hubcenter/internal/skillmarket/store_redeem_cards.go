package skillmarket

import (
	"context"
	"database/sql"
	"errors"
)

func (s *Store) scanCreditRedeemCard(row interface{ Scan(...any) error }) (*CreditRedeemCard, error) {
	var card CreditRedeemCard
	var exportedAt, issuedAt, redeemedAt, revokedAt string
	err := row.Scan(&card.ID, &card.Code, &card.Credits, &card.Status, &exportedAt, &card.ExportedBy,
		&card.IssuedBy, &issuedAt, &card.RedeemedByUserID, &card.RedeemedByEmail, &redeemedAt,
		&card.RevokedBy, &revokedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if exportedAt != "" {
		parsed := parseTime(exportedAt)
		card.ExportedAt = &parsed
	}
	card.IssuedAt = parseTime(issuedAt)
	if redeemedAt != "" {
		parsed := parseTime(redeemedAt)
		card.RedeemedAt = &parsed
	}
	if revokedAt != "" {
		parsed := parseTime(revokedAt)
		card.RevokedAt = &parsed
	}
	return &card, nil
}

const creditRedeemCardColumns = `id, code, credits, status, exported_at, exported_by, issued_by, issued_at,
	redeemed_by_user_id, redeemed_by_email, redeemed_at, revoked_by, revoked_at`

func (s *Store) ListCreditRedeemCards(ctx context.Context, status string, offset, limit int) ([]CreditRedeemCard, int, error) {
	query := `SELECT ` + creditRedeemCardColumns + ` FROM sm_credit_redeem_cards`
	countQuery := `SELECT COUNT(*) FROM sm_credit_redeem_cards`
	args := []any{}
	if status != "" {
		query += ` WHERE status=?`
		countQuery += ` WHERE status=?`
		args = append(args, status)
	}
	var total int
	if err := s.readDB.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	query += ` ORDER BY issued_at DESC, id DESC LIMIT ? OFFSET ?`
	args = append(args, limit, offset)
	rows, err := s.readDB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := make([]CreditRedeemCard, 0)
	for rows.Next() {
		card, err := s.scanCreditRedeemCard(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *card)
	}
	return out, total, rows.Err()
}
