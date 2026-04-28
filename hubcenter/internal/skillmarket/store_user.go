package skillmarket

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// 鈹€鈹€ UserRepository implementation 鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€

func (s *Store) CreateUser(ctx context.Context, user *SkillMarketUser) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO sm_users (id, email, status, verify_method, credits, settled_credits,
			pending_settlement, debt, voucher_count, voucher_expires_at,
			created_at, updated_at, verified_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		user.ID, user.Email, user.Status, user.VerifyMethod,
		user.Credits, user.SettledCredits, user.PendingSettlement, user.Debt,
		user.VoucherCount, fmtTime(user.VoucherExpiresAt),
		fmtTime(user.CreatedAt), fmtTime(user.UpdatedAt), fmtTime(user.VerifiedAt),
	)
	if err == nil {
		s.emitSync(ctx)
	}
	return err
}

func (s *Store) GetUserByEmail(ctx context.Context, email string) (*SkillMarketUser, error) {
	return s.scanUser(s.readDB.QueryRowContext(ctx, `
		SELECT id, email, status, verify_method, credits, settled_credits,
			pending_settlement, debt, voucher_count, voucher_expires_at,
			created_at, updated_at, verified_at
		FROM sm_users WHERE email = ? COLLATE NOCASE ORDER BY created_at ASC LIMIT 1`, normalizeEmail(email)))
}

func (s *Store) GetUserByID(ctx context.Context, id string) (*SkillMarketUser, error) {
	return s.scanUser(s.readDB.QueryRowContext(ctx, `
		SELECT id, email, status, verify_method, credits, settled_credits,
			pending_settlement, debt, voucher_count, voucher_expires_at,
			created_at, updated_at, verified_at
		FROM sm_users WHERE id = ?`, id))
}

func (s *Store) UpdateUserStatus(ctx context.Context, id, status, verifyMethod string) error {
	now := time.Now().Format(timeFmt)
	_, err := s.db.ExecContext(ctx, `
		UPDATE sm_users SET status = ?, verify_method = ?, verified_at = ?, updated_at = ?
		WHERE id = ?`, status, verifyMethod, now, now, id)
	if err == nil {
		s.emitSync(ctx)
	}
	return err
}

func (s *Store) UpdateUserCredits(ctx context.Context, id string, credits int64) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE sm_users SET credits = ?, updated_at = ? WHERE id = ?`,
		credits, time.Now().Format(timeFmt), id)
	if err == nil {
		s.emitSync(ctx)
	}
	return err
}

func (s *Store) UpdateUserSettled(ctx context.Context, id string, settled int64) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE sm_users SET settled_credits = ?, updated_at = ? WHERE id = ?`,
		settled, time.Now().Format(timeFmt), id)
	if err == nil {
		s.emitSync(ctx)
	}
	return err
}

func (s *Store) UpdateUserPendingSettlement(ctx context.Context, id string, pending int64) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE sm_users SET pending_settlement = ?, updated_at = ? WHERE id = ?`,
		pending, time.Now().Format(timeFmt), id)
	if err == nil {
		s.emitSync(ctx)
	}
	return err
}

func (s *Store) UpdateUserDebt(ctx context.Context, id string, debt int64) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE sm_users SET debt = ?, updated_at = ? WHERE id = ?`,
		debt, time.Now().Format(timeFmt), id)
	if err == nil {
		s.emitSync(ctx)
	}
	return err
}

func (s *Store) UpdateUserVoucher(ctx context.Context, id string, count int) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE sm_users SET voucher_count = ?, updated_at = ? WHERE id = ?`,
		count, time.Now().Format(timeFmt), id)
	if err == nil {
		s.emitSync(ctx)
	}
	return err
}

// GetUserByIDForUpdate 鍦ㄤ簨鍔′腑閿佸畾鐢ㄦ埛琛岋紙SQLite 閫氳繃 BEGIN IMMEDIATE 瀹炵幇锛夈€?
func (s *Store) GetUserByIDForUpdate(ctx context.Context, tx *sql.Tx, id string) (*SkillMarketUser, error) {
	return s.scanUser(tx.QueryRowContext(ctx, `
		SELECT id, email, status, verify_method, credits, settled_credits,
			pending_settlement, debt, voucher_count, voucher_expires_at,
			created_at, updated_at, verified_at
		FROM sm_users WHERE id = ?`, id))
}

// BeginImmediate 寮€鍚竴涓?IMMEDIATE 浜嬪姟锛岀‘淇濆啓閿併€?
func (s *Store) BeginImmediate(ctx context.Context) (*sql.Tx, error) {
	return s.db.BeginTx(ctx, &sql.TxOptions{})
}

func (s *Store) scanUser(row *sql.Row) (*SkillMarketUser, error) {
	var u SkillMarketUser
	var verifyMethod, voucherExp, createdAt, updatedAt, verifiedAt string
	err := row.Scan(
		&u.ID, &u.Email, &u.Status, &verifyMethod,
		&u.Credits, &u.SettledCredits, &u.PendingSettlement, &u.Debt,
		&u.VoucherCount, &voucherExp,
		&createdAt, &updatedAt, &verifiedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	u.VerifyMethod = verifyMethod
	u.VoucherExpiresAt = parseTime(voucherExp)
	u.CreatedAt = parseTime(createdAt)
	u.UpdatedAt = parseTime(updatedAt)
	u.VerifiedAt = parseTime(verifiedAt)
	return &u, nil
}
