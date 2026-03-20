package skillmarket

import (
	"context"
	"fmt"
	"time"
)

// CreditsService 管理 Credits 余额和交易。
type CreditsService struct {
	store *Store
}

// NewCreditsService 创建 CreditsService。
func NewCreditsService(store *Store) *CreditsService {
	return &CreditsService{store: store}
}

// GetBalance 查询用户 Credits 余额。
func (s *CreditsService) GetBalance(ctx context.Context, userID string) (int64, error) {
	u, err := s.store.GetUserByID(ctx, userID)
	if err != nil {
		return 0, err
	}
	return u.Credits, nil
}

// Debit 扣款（购买 Skill）。使用 BEGIN IMMEDIATE 事务确保原子性。
func (s *CreditsService) Debit(ctx context.Context, userID string, amount int64, skillID, purchaseID, desc string) error {
	if amount <= 0 {
		return fmt.Errorf("debit amount must be positive")
	}
	tx, err := s.store.BeginImmediate(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	u, err := s.store.GetUserByIDForUpdate(ctx, tx, userID)
	if err != nil {
		return err
	}
	if u.Credits < amount {
		return ErrInsufficientCredits
	}
	newBalance := u.Credits - amount
	if _, err := tx.ExecContext(ctx, `UPDATE sm_users SET credits = ?, updated_at = ? WHERE id = ?`,
		newBalance, time.Now().Format(timeFmt), userID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO sm_credits_transactions (id, user_id, type, amount, balance, skill_id, purchase_id, description, created_at)
		VALUES (?, ?, 'purchase', ?, ?, ?, ?, ?, ?)`,
		generateID(), userID, -amount, newBalance, skillID, purchaseID, desc, time.Now().Format(timeFmt)); err != nil {
		return err
	}
	return tx.Commit()
}

// Credit 入账（Skill 被购买时给上传者）。
// settled 参数控制是否计入 settled_credits（已交付）或 pending_settlement（待交付）。
// 入账前自动抵扣 debt。
func (s *CreditsService) Credit(ctx context.Context, userID string, amount int64, settled bool, skillID, purchaseID, desc string) error {
	if amount <= 0 {
		return fmt.Errorf("credit amount must be positive")
	}
	tx, err := s.store.BeginImmediate(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	u, err := s.store.GetUserByIDForUpdate(ctx, tx, userID)
	if err != nil {
		return err
	}

	// 自动抵扣 debt
	actual := amount
	newDebt := u.Debt
	if newDebt > 0 {
		if actual <= newDebt {
			newDebt -= actual
			actual = 0
		} else {
			actual -= newDebt
			newDebt = 0
		}
	}

	now := time.Now().Format(timeFmt)
	var newBalance int64
	if settled {
		newSettled := u.SettledCredits + actual
		newBalance = newSettled + u.PendingSettlement
		if _, err := tx.ExecContext(ctx, `UPDATE sm_users SET settled_credits = ?, debt = ?, updated_at = ? WHERE id = ?`,
			newSettled, newDebt, now, userID); err != nil {
			return err
		}
	} else {
		newPending := u.PendingSettlement + actual
		newBalance = u.SettledCredits + newPending
		if _, err := tx.ExecContext(ctx, `UPDATE sm_users SET pending_settlement = ?, debt = ?, updated_at = ? WHERE id = ?`,
			newPending, newDebt, now, userID); err != nil {
			return err
		}
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO sm_credits_transactions (id, user_id, type, amount, balance, skill_id, purchase_id, description, created_at)
		VALUES (?, ?, 'earning', ?, ?, ?, ?, ?, ?)`,
		generateID(), userID, amount, newBalance, skillID, purchaseID, desc, now); err != nil {
		return err
	}
	return tx.Commit()
}

// RecordPlatformFee 记录平台手续费（仅记录交易，不影响任何用户余额）。
func (s *CreditsService) RecordPlatformFee(ctx context.Context, amount int64, skillID, purchaseID, desc string) error {
	return s.store.CreateTransaction(ctx, &CreditsTransaction{
		ID:         generateID(),
		UserID:     "platform",
		Type:       "platform_fee",
		Amount:     amount,
		SkillID:    skillID,
		PurchaseID: purchaseID,
		Description: desc,
		CreatedAt:  time.Now(),
	})
}

// TopUp 充值（仅 verified 用户）。
func (s *CreditsService) TopUp(ctx context.Context, userID string, amount int64) error {
	if amount <= 0 {
		return fmt.Errorf("topup amount must be positive")
	}
	tx, err := s.store.BeginImmediate(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	u, err := s.store.GetUserByIDForUpdate(ctx, tx, userID)
	if err != nil {
		return err
	}
	if u.Status != "verified" {
		return ErrUnverifiedAccount
	}
	newBalance := u.Credits + amount
	now := time.Now().Format(timeFmt)
	if _, err := tx.ExecContext(ctx, `UPDATE sm_users SET credits = ?, updated_at = ? WHERE id = ?`,
		newBalance, now, userID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO sm_credits_transactions (id, user_id, type, amount, balance, skill_id, purchase_id, description, created_at)
		VALUES (?, ?, 'topup', ?, ?, '', '', 'Top up', ?)`,
		generateID(), userID, amount, newBalance, now); err != nil {
		return err
	}
	return tx.Commit()
}

// Withdraw 提现（仅 verified 用户，仅 settled_credits 可提现）。
func (s *CreditsService) Withdraw(ctx context.Context, userID string, amount int64) error {
	if amount <= 0 {
		return fmt.Errorf("withdraw amount must be positive")
	}
	tx, err := s.store.BeginImmediate(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	u, err := s.store.GetUserByIDForUpdate(ctx, tx, userID)
	if err != nil {
		return err
	}
	if u.Status != "verified" {
		return ErrUnverifiedAccount
	}
	if u.SettledCredits < amount {
		return ErrInsufficientCredits
	}
	newSettled := u.SettledCredits - amount
	now := time.Now().Format(timeFmt)
	if _, err := tx.ExecContext(ctx, `UPDATE sm_users SET settled_credits = ?, updated_at = ? WHERE id = ?`,
		newSettled, now, userID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO sm_credits_transactions (id, user_id, type, amount, balance, skill_id, purchase_id, description, created_at)
		VALUES (?, ?, 'withdraw', ?, ?, '', '', 'Withdraw', ?)`,
		generateID(), userID, -amount, newSettled, now); err != nil {
		return err
	}
	return tx.Commit()
}

// SettlePending 将 pending_settlement 转为 settled（API Key 交付后调用）。
func (s *CreditsService) SettlePending(ctx context.Context, userID string, amount int64) error {
	if amount <= 0 {
		return fmt.Errorf("settle amount must be positive")
	}
	tx, err := s.store.BeginImmediate(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	u, err := s.store.GetUserByIDForUpdate(ctx, tx, userID)
	if err != nil {
		return err
	}
	if u.PendingSettlement < amount {
		amount = u.PendingSettlement // 不超过实际 pending
	}
	now := time.Now().Format(timeFmt)
	if _, err := tx.ExecContext(ctx,
		`UPDATE sm_users SET settled_credits = ?, pending_settlement = ?, updated_at = ? WHERE id = ?`,
		u.SettledCredits+amount, u.PendingSettlement-amount, now, userID); err != nil {
		return err
	}
	return tx.Commit()
}
