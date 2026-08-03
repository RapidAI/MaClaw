package skillmarket

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
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
	err := s.store.readDB.QueryRowContext(ctx, `SELECT id, hub_id, tenant_id, buyer_email, buyer_id, skill_id, purchased_version, purchase_type, amount_paid, platform_fee, seller_earning, seller_id, key_status, api_key_id, status, created_at FROM sm_purchase_records WHERE id = ?`, purchaseRecordID).Scan(&pr.ID, &pr.HubID, &pr.TenantID, &pr.BuyerEmail, &pr.BuyerID, &pr.SkillID, &pr.PurchasedVersion, &pr.PurchaseType, &pr.AmountPaid, &pr.PlatformFee, &pr.SellerEarning, &pr.SellerID, &pr.KeyStatus, &pr.APIKeyID, &pr.Status, &createdAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// Pet store entitlements live in their own ledger; an admin refund
			// must reach them through the same endpoint.
			return s.processPetStoreRefund(ctx, purchaseRecordID, reason)
		}
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
	// Re-check the purchase state inside the write transaction. Two
	// concurrent refund requests can both pass the readDB status check above;
	// without this guard the second transaction would repay the buyer twice.
	markResult, err := tx.ExecContext(ctx, `UPDATE sm_purchase_records SET status = 'refunded' WHERE id = ? AND status <> 'refunded'`, pr.ID)
	if err != nil {
		return fmt.Errorf("mark purchase refunded: %w", err)
	}
	if changed, _ := markResult.RowsAffected(); changed == 0 {
		return ErrAlreadyRefunded
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

// processPetStoreRefund mirrors the skill refund flow for pet store
// entitlements, which are recorded in sm_pet_store_purchases rather than
// sm_purchase_records. The buyer is repaid in full and the seller loses the
// earning (settled balance first, debt for any shortfall); the platform fee is
// absorbed by the platform exactly as in the skill refund path.
func (s *RefundService) processPetStoreRefund(ctx context.Context, purchaseID, reason string) error {
	var buyerID, buyerEmail, packID, packName, sellerID, status string
	var amountPaid int64
	err := s.store.readDB.QueryRowContext(ctx, `SELECT b.buyer_user_id, b.buyer_email, b.amount_paid, b.status, b.pack_id, p.name, p.owner_user_id
		FROM sm_pet_store_purchases b JOIN sm_pet_store_packs p ON p.id=b.pack_id WHERE b.id = ?`, purchaseID).
		Scan(&buyerID, &buyerEmail, &amountPaid, &status, &packID, &packName, &sellerID)
	if err != nil {
		if isMissingPetStoreTable(err) {
			return fmt.Errorf("purchase record not found: %s", purchaseID)
		}
		return fmt.Errorf("purchase record not found: %w", err)
	}
	if status == "refunded" {
		return ErrAlreadyRefunded
	}
	sellerEarning := amountPaid - amountPaid*PetStorePlatformFeePct/100
	now := fmtTime(time.Now())
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `UPDATE sm_users SET credits = credits + ?, updated_at = ? WHERE id = ?`, amountPaid, now, buyerID); err != nil {
		return fmt.Errorf("refund buyer: %w", err)
	}
	var sellerSettled int64
	if err := tx.QueryRowContext(ctx, `SELECT settled_credits FROM sm_users WHERE id = ?`, sellerID).Scan(&sellerSettled); err != nil {
		return fmt.Errorf("query seller balance: %w", err)
	}
	if sellerSettled >= sellerEarning {
		if _, err := tx.ExecContext(ctx, `UPDATE sm_users SET settled_credits = settled_credits - ?, updated_at = ? WHERE id = ?`, sellerEarning, now, sellerID); err != nil {
			return fmt.Errorf("deduct seller settled: %w", err)
		}
	} else {
		shortfall := sellerEarning - sellerSettled
		if _, err := tx.ExecContext(ctx, `UPDATE sm_users SET settled_credits = 0, debt = debt + ?, updated_at = ? WHERE id = ?`, shortfall, now, sellerID); err != nil {
			return fmt.Errorf("deduct seller with debt: %w", err)
		}
	}
	var buyerBalance int64
	if err := tx.QueryRowContext(ctx, `SELECT credits FROM sm_users WHERE id = ?`, buyerID).Scan(&buyerBalance); err != nil {
		return fmt.Errorf("query buyer balance: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO sm_credits_transactions (id, user_id, type, amount, balance, skill_id, purchase_id, description, created_at) VALUES (?, ?, 'refund', ?, ?, ?, ?, ?, ?)`, generateID(), buyerID, amountPaid, buyerBalance, packID, purchaseID, "宠物包退款 "+reason, now); err != nil {
		return fmt.Errorf("record buyer refund tx: %w", err)
	}
	var sellerBalance int64
	if err := tx.QueryRowContext(ctx, `SELECT settled_credits FROM sm_users WHERE id = ?`, sellerID).Scan(&sellerBalance); err != nil {
		return fmt.Errorf("query seller post-refund balance: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO sm_credits_transactions (id, user_id, type, amount, balance, skill_id, purchase_id, description, created_at) VALUES (?, ?, 'refund', ?, ?, ?, ?, ?, ?)`, generateID(), sellerID, -sellerEarning, sellerBalance, packID, purchaseID, "宠物包退款扣回 "+reason, now); err != nil {
		return fmt.Errorf("record seller refund tx: %w", err)
	}
	// Re-check the entitlement state inside the write transaction. Two
	// concurrent refund requests can both pass the readDB status check above;
	// without this guard the second transaction would repay the buyer twice.
	markResult, err := tx.ExecContext(ctx, `UPDATE sm_pet_store_purchases SET status = 'refunded' WHERE id = ? AND status <> 'refunded'`, purchaseID)
	if err != nil {
		return fmt.Errorf("mark pet store purchase refunded: %w", err)
	}
	if changed, _ := markResult.RowsAffected(); changed == 0 {
		return ErrAlreadyRefunded
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.store.emitSync(ctx)
	_ = s.mailer.Send(ctx, []string{buyerEmail}, "Pet Store 退款通知", fmt.Sprintf("您购买的宠物包「%s」（ID: %s）已退款，%d Credits 已退还到您的账户。原因：%s", packName, packID, amountPaid, reason))
	return nil
}

// isMissingPetStoreTable reports whether err indicates that the lazily
// created pet store schema has not been installed on this database yet.
func isMissingPetStoreTable(err error) bool {
	return err != nil && strings.Contains(err.Error(), "no such table")
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
	queryArgs := make([]any, len(args))
	copy(queryArgs, args)
	queryArgs = append(queryArgs, limit, offset)
	rows, err := s.store.readDB.QueryContext(ctx, `SELECT id, hub_id, tenant_id, buyer_email, buyer_id, skill_id, purchased_version, purchase_type, amount_paid, platform_fee, seller_earning, seller_id, key_status, api_key_id, status, created_at FROM sm_purchase_records WHERE `+where+` ORDER BY created_at DESC LIMIT ? OFFSET ?`, queryArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var records []PurchaseRecord
	for rows.Next() {
		var r PurchaseRecord
		var ca string
		if err := rows.Scan(&r.ID, &r.HubID, &r.TenantID, &r.BuyerEmail, &r.BuyerID, &r.SkillID, &r.PurchasedVersion, &r.PurchaseType, &r.AmountPaid, &r.PlatformFee, &r.SellerEarning, &r.SellerID, &r.KeyStatus, &r.APIKeyID, &r.Status, &ca); err != nil {
			return nil, 0, err
		}
		r.CreatedAt = parseTime(ca)
		records = append(records, r)
	}
	// Pet store entitlements live in their own ledger. Surface them in the same
	// admin listing so moderation can find and refund them; their pack ID takes
	// the skill_id slot and purchase_type marks them as pet_pack.
	petRecords, petTotal, err := s.listPetStorePurchases(ctx, buyerEmail, skillID)
	if err != nil {
		return nil, 0, err
	}
	records = append(records, petRecords...)
	sort.Slice(records, func(i, j int) bool { return records[i].CreatedAt.After(records[j].CreatedAt) })
	return records, total + petTotal, nil
}

// listPetStorePurchases maps pet store entitlements onto PurchaseRecord. The
// platform fee and seller earning are not stored per purchase, so they are
// derived with the same PetStorePlatformFeePct used at checkout.
func (s *RefundService) listPetStorePurchases(ctx context.Context, buyerEmail, packID string) ([]PurchaseRecord, int, error) {
	where := "1=1"
	var args []any
	if buyerEmail != "" {
		where += " AND b.buyer_email = ?"
		args = append(args, buyerEmail)
	}
	if packID != "" {
		where += " AND b.pack_id = ?"
		args = append(args, packID)
	}
	var total int
	countArgs := make([]any, len(args))
	copy(countArgs, args)
	if err := s.store.readDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM sm_pet_store_purchases b WHERE `+where, countArgs...).Scan(&total); err != nil {
		if isMissingPetStoreTable(err) {
			return nil, 0, nil
		}
		return nil, 0, err
	}
	rows, err := s.store.readDB.QueryContext(ctx, `SELECT b.id, b.buyer_email, b.buyer_user_id, b.pack_id, b.amount_paid, b.status, b.created_at, p.owner_user_id
		FROM sm_pet_store_purchases b JOIN sm_pet_store_packs p ON p.id=b.pack_id WHERE `+where+` ORDER BY b.created_at DESC LIMIT 200`, args...)
	if err != nil {
		if isMissingPetStoreTable(err) {
			return nil, 0, nil
		}
		return nil, 0, err
	}
	defer rows.Close()
	var records []PurchaseRecord
	for rows.Next() {
		var r PurchaseRecord
		var ca string
		if err := rows.Scan(&r.ID, &r.BuyerEmail, &r.BuyerID, &r.SkillID, &r.AmountPaid, &r.Status, &ca, &r.SellerID); err != nil {
			return nil, 0, err
		}
		r.PurchaseType = "pet_pack"
		r.PlatformFee = r.AmountPaid * PetStorePlatformFeePct / 100
		r.SellerEarning = r.AmountPaid - r.PlatformFee
		r.CreatedAt = parseTime(ca)
		records = append(records, r)
	}
	return records, total, rows.Err()
}
