package skillmarket

import "context"

// 鈹€鈹€ CreditsRepository implementation 鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€

func (s *Store) CreateTransaction(ctx context.Context, tx *CreditsTransaction) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO sm_credits_transactions (id, user_id, type, amount, balance, skill_id, purchase_id, description, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		tx.ID, tx.UserID, tx.Type, tx.Amount, tx.Balance,
		tx.SkillID, tx.PurchaseID, tx.Description, fmtTime(tx.CreatedAt),
	)
	if err == nil {
		s.emitSync(ctx)
	}
	return err
}

func (s *Store) ListTransactionsByUser(ctx context.Context, userID string, offset, limit int) ([]CreditsTransaction, int, error) {
	var total int
	if err := s.readDB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sm_credits_transactions WHERE user_id = ?`, userID,
	).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.readDB.QueryContext(ctx, `
		SELECT id, user_id, type, amount, balance, skill_id, purchase_id, description, created_at
		FROM sm_credits_transactions
		WHERE user_id = ?
		ORDER BY created_at DESC
		LIMIT ? OFFSET ?`, userID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var out []CreditsTransaction
	for rows.Next() {
		var t CreditsTransaction
		var createdAt string
		if err := rows.Scan(
			&t.ID, &t.UserID, &t.Type, &t.Amount, &t.Balance,
			&t.SkillID, &t.PurchaseID, &t.Description, &createdAt,
		); err != nil {
			return nil, 0, err
		}
		t.CreatedAt = parseTime(createdAt)
		out = append(out, t)
	}
	return out, total, rows.Err()
}
