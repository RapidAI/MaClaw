package skillmarket

import (
	"context"
	"fmt"
	"time"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/mail"
)

type RefundService struct {
	store      *Store
	creditsSvc *CreditsService
	mailer     mail.Mailer
}

func NewRefundService(store *Store, creditsSvc *CreditsService, mailer mail.Mailer) *RefundService {
	return &RefundService{store: store, creditsSvc: creditsSvc, mailer: mailer}
}

func (s *RefundService) ProcessRefund(ctx context.Context, purchaseRecordID, adminEmail, reason string) error {
	var pr PurchaseRecord
	var createdAt string
	err := s.store.readDB.QueryRowContext(ctx, `SELECT id, buyer_email, buyer_id, skill_id, purchased_version, purchase_type, amount_paid, platform_fee, seller_earning, seller_id, key_status, api_key_id, status, created_at FROM sm_purchase_records WHERE id = ?`, purchaseRecordID).Scan(&pr.ID, &pr.BuyerEmail, &pr.BuyerID, &pr.SkillID, &pr.PurchasedVersion, &pr.PurchaseType, &pr.AmountPaid, &pr.PlatformFee, &pr.SellerEarning, &pr.SellerID, &pr.KeyStatus, &pr.APIKeyID, &pr.Status, &createdAt)
	if err != nil {
		return fmt.Errorf("purchase record not found: %w", err)
	}
	if pr.Status == "refunded" {
		return ErrAlreadyRefunded
	}
	now := fmtTime(time.Now())
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `UPDATE sm_users SET credits = credits + ?, updated_at = ? WHERE id = ?`, pr.AmountPaid, now, pr.BuyerID); err != nil {
		return fmt.Errorf("refund buyer: %w", err)
	}
	var sellerSettled, sellerPending int64
	if err := tx.QueryRowContext(ctx, `SELECT settled_credits, pending_settlement FROM sm_users WHERE id = ?`, pr.SellerID).Scan(&sellerSettled, &sellerPending); err != nil {
		return fmt.Errorf("query seller balance: %w", err)
	}
	deductAmount := pr.SellerEarning
	if sellerSettled >= deductAmount {
		if _, err := tx.ExecContext(ctx, `UPDATE sm_users SET settled_credits = settled_credits - ?, updated_at = ? WHERE id = ?`, deductAmount, now, pr.SellerID); err != nil {
			return fmt.Errorf("deduct seller settled: %w", err)
		}
	} else {
		shortfall := deductAmount - sellerSettled
		if _, err := tx.ExecContext(ctx, `UPDATE sm_users SET settled_credits = 0, debt = debt + ?, updated_at = ? WHERE id = ?`, shortfall, now, pr.SellerID); err != nil {
			return fmt.Errorf("deduct seller with debt: %w", err)
		}
	}
	var buyerBalance int64
	if err := tx.QueryRowContext(ctx, `SELECT credits FROM sm_users WHERE id = ?`, pr.BuyerID).Scan(&buyerBalance); err != nil {
		return fmt.Errorf("query buyer balance: %w", err)
	}
	buyerTxID := generateID()
	if _, err := tx.ExecContext(ctx, `INSERT INTO sm_credits_transactions (id, user_id, type, amount, balance, skill_id, purchase_id, description, created_at) VALUES (?, ?, 'refund', ?, ?, ?, ?, ?, ?)`, buyerTxID, pr.BuyerID, pr.AmountPaid, buyerBalance, pr.SkillID, pr.ID, "退款 "+reason, now); err != nil {
		return fmt.Errorf("record buyer refund tx: %w", err)
	}
	var sellerBalance int64
	if err := tx.QueryRowContext(ctx, `SELECT settled_credits FROM sm_users WHERE id = ?`, pr.SellerID).Scan(&sellerBalance); err != nil {
		return fmt.Errorf("query seller post-refund balance: %w", err)
	}
	sellerTxID := generateID()
	if _, err := tx.ExecContext(ctx, `INSERT INTO sm_credits_transactions (id, user_id, type, amount, balance, skill_id, purchase_id, description, created_at) VALUES (?, ?, 'refund', ?, ?, ?, ?, ?, ?)`, sellerTxID, pr.SellerID, -pr.SellerEarning, sellerBalance, pr.SkillID, pr.ID, "退款扣回 "+reason, now); err != nil {
		return fmt.Errorf("record seller refund tx: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE sm_purchase_records SET status = 'refunded' WHERE id = ?`, pr.ID); err != nil {
		return fmt.Errorf("mark purchase refunded: %w", err)
	}
	if pr.APIKeyID != "" && pr.KeyStatus == "assigned" {
		if _, err := tx.ExecContext(ctx, `UPDATE sm_api_keys SET status = 'refunded' WHERE id = ?`, pr.APIKeyID); err != nil {
			return fmt.Errorf("refund api key: %w", err)
		}
	}
	if pr.KeyStatus == "pending_key" {
		if _, err := tx.ExecContext(ctx, `UPDATE sm_pending_key_orders SET status = 'cancelled', updated_at = ? WHERE purchase_record_id = ? AND status = 'pending_key'`, now, pr.ID); err != nil {
			return fmt.Errorf("cancel pending key order: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE sm_users SET pending_settlement = CASE WHEN pending_settlement >= ? THEN pending_settlement - ? ELSE 0 END, updated_at = ? WHERE id = ?`, pr.SellerEarning, pr.SellerEarning, now, pr.SellerID); err != nil {
			return fmt.Errorf("deduct pending settlement: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.store.emitSync(ctx)
	_ = s.mailer.Send(ctx, []string{pr.BuyerEmail}, "SkillMarket 退款通知", fmt.Sprintf("您购买的 Skill（ID: %s）已退款，%d Credits 已退还到您的账户。原因：%s", pr.SkillID, pr.AmountPaid, reason))
	return nil
}

func (s *RefundService) ListPurchases(ctx context.Context, buyerEmail, skillID string, offset, limit int) ([]PurchaseRecord, int, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	where := "1=1"
	var args []any
	if buyerEmail != "" {
		where += " AND buyer_email = ?"
		args = append(args, buyerEmail)
	}
	if skillID != "" {
		where += " AND skill_id = ?"
		args = append(args, skillID)
	}
	var total int
	countArgs := make([]any, len(args))
	copy(countArgs, args)
	_ = s.store.readDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM sm_purchase_records WHERE `+where, countArgs...).Scan(&total)
	args = append(args, limit, offset)
	rows, err := s.store.readDB.QueryContext(ctx, `SELECT id, buyer_email, buyer_id, skill_id, purchased_version, purchase_type, amount_paid, platform_fee, seller_earning, seller_id, key_status, api_key_id, status, created_at FROM sm_purchase_records WHERE `+where+` ORDER BY created_at DESC LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var records []PurchaseRecord
	for rows.Next() {
		var r PurchaseRecord
		var ca string
		if err := rows.Scan(&r.ID, &r.BuyerEmail, &r.BuyerID, &r.SkillID, &r.PurchasedVersion, &r.PurchaseType, &r.AmountPaid, &r.PlatformFee, &r.SellerEarning, &r.SellerID, &r.KeyStatus, &r.APIKeyID, &r.Status, &ca); err != nil {
			return nil, 0, err
		}
		r.CreatedAt = parseTime(ca)
		records = append(records, r)
	}
	return records, total, nil
}
