package skillmarket

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// CreditsService 绠＄悊 Credits 浣欓鍜屼氦鏄撱€?
type CreditsService struct {
	store *Store
}

// NewCreditsService 鍒涘缓 CreditsService銆?
func NewCreditsService(store *Store) *CreditsService {
	return &CreditsService{store: store}
}

// PetStorePlatformFeePct is the single source of truth for the platform fee
// percentage withheld from a paid pet store sale before the seller earning is
// settled.
const PetStorePlatformFeePct int64 = 30

// CompleteExpertMarketPurchase creates a permanent entitlement and its Credits
// payment atomically. Expert packages are authored configurations, so v1 keeps
// the full price in the platform ledger rather than attempting author revenue
// settlement. This matches the platform-operated Expert Market policy and
// avoids creating a second, incompatible Credits balance.
func (s *CreditsService) CompleteExpertMarketPurchase(ctx context.Context, buyerID, buyerEmail, sellerID, listingID, entitlementID, transactionID string, amount int64) error {
	if s == nil || s.store == nil || buyerID == "" || sellerID == "" || listingID == "" || entitlementID == "" || transactionID == "" || amount < 0 {
		return fmt.Errorf("invalid expert market purchase")
	}
	tx, err := s.store.BeginImmediate(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()
	var ownerID string
	var price int64
	if err := tx.QueryRowContext(ctx, `SELECT owner_user_id, price FROM sm_expert_market_listings WHERE id=? AND visibility='public' AND status='listed'`, listingID).Scan(&ownerID, &price); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrExpertMarketUnavailable
		}
		return err
	}
	if ownerID != sellerID || price != amount {
		return ErrExpertMarketUnavailable
	}
	var owned int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM sm_expert_market_purchases WHERE listing_id=? AND buyer_user_id=? AND status='active'`, listingID, buyerID).Scan(&owned); err != nil {
		return err
	}
	if owned > 0 {
		return ErrExpertMarketAlreadyOwned
	}
	buyer, err := s.store.GetUserByIDForUpdate(ctx, tx, buyerID)
	if err != nil {
		return err
	}
	if buyer.Credits < amount {
		return ErrInsufficientCredits
	}
	now := time.Now().Format(timeFmt)
	if amount > 0 {
		balance := buyer.Credits - amount
		if _, err := tx.ExecContext(ctx, `UPDATE sm_users SET credits=?, updated_at=? WHERE id=?`, balance, now, buyerID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO sm_credits_transactions (id, user_id, type, amount, balance, skill_id, purchase_id, description, created_at) VALUES (?, ?, 'purchase', ?, ?, ?, ?, 'AI expert market purchase', ?)`, generateID(), buyerID, -amount, balance, listingID, transactionID, now); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO sm_credits_transactions (id, user_id, type, amount, balance, skill_id, purchase_id, description, created_at) VALUES (?, 'platform', 'platform_fee', ?, 0, ?, ?, 'AI expert market settlement', ?)`, generateID(), amount, listingID, transactionID, now); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO sm_expert_market_purchases (id, listing_id, buyer_user_id, buyer_email, amount_paid, status, created_at) VALUES (?, ?, ?, ?, ?, 'active', ?)`, entitlementID, listingID, buyerID, buyerEmail, amount, now); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return ErrExpertMarketAlreadyOwned
		}
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE sm_expert_market_listings SET purchase_count=purchase_count+1, sales_amount=sales_amount+?, updated_at=? WHERE id=? AND visibility='public' AND status='listed'`, amount, now, listingID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.store.emitSync(ctx)
	return nil
}

var ErrExpertMarketAlreadyOwned = errors.New("expert market listing already owned")
var ErrExpertMarketUnavailable = errors.New("expert market listing is no longer available")

func (s *CreditsService) GetBalance(ctx context.Context, userID string) (int64, error) {
	u, err := s.store.GetUserByID(ctx, userID)
	if err != nil {
		return 0, err
	}
	return u.Credits, nil
}

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
	if _, err := tx.ExecContext(ctx, `UPDATE sm_users SET credits = ?, updated_at = ? WHERE id = ?`, newBalance, time.Now().Format(timeFmt), userID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO sm_credits_transactions (id, user_id, type, amount, balance, skill_id, purchase_id, description, created_at) VALUES (?, ?, 'purchase', ?, ?, ?, ?, ?, ?)`, generateID(), userID, -amount, newBalance, skillID, purchaseID, desc, time.Now().Format(timeFmt)); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.store.emitSync(ctx)
	return nil
}

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
		if _, err := tx.ExecContext(ctx, `UPDATE sm_users SET settled_credits = ?, debt = ?, updated_at = ? WHERE id = ?`, newSettled, newDebt, now, userID); err != nil {
			return err
		}
	} else {
		newPending := u.PendingSettlement + actual
		newBalance = u.SettledCredits + newPending
		if _, err := tx.ExecContext(ctx, `UPDATE sm_users SET pending_settlement = ?, debt = ?, updated_at = ? WHERE id = ?`, newPending, newDebt, now, userID); err != nil {
			return err
		}
	}

	if _, err := tx.ExecContext(ctx, `INSERT INTO sm_credits_transactions (id, user_id, type, amount, balance, skill_id, purchase_id, description, created_at) VALUES (?, ?, 'earning', ?, ?, ?, ?, ?, ?)`, generateID(), userID, amount, newBalance, skillID, purchaseID, desc, now); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.store.emitSync(ctx)
	return nil
}

func (s *CreditsService) RecordPlatformFee(ctx context.Context, amount int64, skillID, purchaseID, desc string) error {
	return s.store.CreateTransaction(ctx, &CreditsTransaction{ID: generateID(), UserID: "platform", Type: "platform_fee", Amount: amount, SkillID: skillID, PurchaseID: purchaseID, Description: desc, CreatedAt: time.Now()})
}

// CompletePetStorePurchase atomically creates the lifetime entitlement and
// settles the matching Credits movements. Entitlements follow the stable
// source_pack_id, rather than a transient market listing ID: a creator may
// withdraw and re-publish the same local pack without charging existing buyers
// again. Keeping these writes in one SQLite IMMEDIATE transaction prevents a
// buyer debit without an entitlement (or an entitlement without a completed
// payment) when a process fails mid-request.
func (s *CreditsService) CompletePetStorePurchase(ctx context.Context, buyerID, buyerEmail, sellerID, packID, entitlementID, transactionID string, amount int64) error {
	if s == nil || s.store == nil || buyerID == "" || sellerID == "" || packID == "" || entitlementID == "" || transactionID == "" || amount < 0 {
		return fmt.Errorf("invalid pet store purchase")
	}
	tx, err := s.store.BeginImmediate(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()
	// The HTTP handler's initial listing read is deliberately outside this
	// transaction so ordinary browse requests stay cheap. Re-check the listing
	// under the write transaction, though: an owner may withdraw it between
	// that read and checkout. Never create a new entitlement for a withdrawn
	// listing, even if the client was already looking at its card.
	var currentSellerID, sourcePackID string
	var currentPrice int64
	if err := tx.QueryRowContext(ctx, `SELECT owner_user_id, source_pack_id, price FROM sm_pet_store_packs WHERE id=? AND status='active'`, packID).Scan(&currentSellerID, &sourcePackID, &currentPrice); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrPetStoreUnavailable
		}
		return err
	}
	if currentSellerID != sellerID || currentPrice != amount {
		return ErrPetStoreUnavailable
	}
	// The schema requires newly published listings to have a source ID. Treat an
	// old, malformed listing without one as unavailable instead of silently
	// falling back to an unrelated listing-level entitlement.
	if strings.TrimSpace(sourcePackID) == "" {
		return ErrPetStoreUnavailable
	}
	var alreadyOwned int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*)
		FROM sm_pet_store_purchases b
		JOIN sm_pet_store_packs owned ON owned.id=b.pack_id
		WHERE b.buyer_user_id=? AND b.status='active' AND owned.source_pack_id=?`, buyerID, sourcePackID).Scan(&alreadyOwned); err != nil {
		return err
	}
	if alreadyOwned > 0 {
		return ErrPetStoreAlreadyOwned
	}
	buyer, err := s.store.GetUserByIDForUpdate(ctx, tx, buyerID)
	if err != nil {
		return err
	}
	if buyer.Credits < amount {
		return ErrInsufficientCredits
	}
	now := time.Now().Format(timeFmt)
	if amount > 0 {
		buyerBalance := buyer.Credits - amount
		if _, err := tx.ExecContext(ctx, `UPDATE sm_users SET credits=?, updated_at=? WHERE id=?`, buyerBalance, now, buyerID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO sm_credits_transactions (id, user_id, type, amount, balance, skill_id, purchase_id, description, created_at) VALUES (?, ?, 'purchase', ?, ?, ?, ?, 'pet pack purchase', ?)`, generateID(), buyerID, -amount, buyerBalance, packID, transactionID, now); err != nil {
			return err
		}
		if sellerID != buyerID {
			seller, err := s.store.GetUserByIDForUpdate(ctx, tx, sellerID)
			if err != nil {
				return err
			}
			fee := amount * PetStorePlatformFeePct / 100
			earning, actual, debt := amount-fee, amount-fee, seller.Debt
			if actual <= debt {
				debt -= actual
				actual = 0
			} else {
				actual -= debt
				debt = 0
			}
			settled := seller.SettledCredits + actual
			balance := settled + seller.PendingSettlement
			if _, err := tx.ExecContext(ctx, `UPDATE sm_users SET settled_credits=?, debt=?, updated_at=? WHERE id=?`, settled, debt, now, sellerID); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO sm_credits_transactions (id, user_id, type, amount, balance, skill_id, purchase_id, description, created_at) VALUES (?, ?, 'earning', ?, ?, ?, ?, 'pet pack sold', ?)`, generateID(), sellerID, earning, balance, packID, transactionID, now); err != nil {
				return err
			}
			if fee > 0 {
				if _, err := tx.ExecContext(ctx, `INSERT INTO sm_credits_transactions (id, user_id, type, amount, balance, skill_id, purchase_id, description, created_at) VALUES (?, 'platform', 'platform_fee', ?, 0, ?, ?, 'pet store platform fee', ?)`, generateID(), fee, packID, transactionID, now); err != nil {
					return err
				}
			}
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO sm_pet_store_purchases (id, pack_id, buyer_user_id, buyer_email, amount_paid, status, created_at) VALUES (?, ?, ?, ?, ?, 'active', ?)`, entitlementID, packID, buyerID, buyerEmail, amount, now); err != nil {
		// The unique (pack_id, buyer_user_id) constraint is an additional
		// idempotency gate for legacy listings. The source-ID lookup above is the
		// primary entitlement guard for current listings.
		if strings.Contains(strings.ToLower(err.Error()), "unique") || errors.Is(err, sql.ErrNoRows) {
			return ErrPetStoreAlreadyOwned
		}
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE sm_pet_store_packs SET purchase_count=purchase_count+1, sales_amount=sales_amount+?, updated_at=? WHERE id=? AND status='active'`, amount, now, packID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.store.emitSync(ctx)
	return nil
}

var ErrPetStoreAlreadyOwned = errors.New("pet store pack already owned")
var ErrPetStoreUnavailable = errors.New("pet store pack is no longer available")

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
	if _, err := tx.ExecContext(ctx, `UPDATE sm_users SET credits = ?, updated_at = ? WHERE id = ?`, newBalance, now, userID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO sm_credits_transactions (id, user_id, type, amount, balance, skill_id, purchase_id, description, created_at) VALUES (?, ?, 'topup', ?, ?, '', '', 'Top up', ?)`, generateID(), userID, amount, newBalance, now); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.store.emitSync(ctx)
	return nil
}

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
	if _, err := tx.ExecContext(ctx, `UPDATE sm_users SET settled_credits = ?, updated_at = ? WHERE id = ?`, newSettled, now, userID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO sm_credits_transactions (id, user_id, type, amount, balance, skill_id, purchase_id, description, created_at) VALUES (?, ?, 'withdraw', ?, ?, '', '', 'Withdraw', ?)`, generateID(), userID, -amount, newSettled, now); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.store.emitSync(ctx)
	return nil
}

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
		amount = u.PendingSettlement
	}
	now := time.Now().Format(timeFmt)
	if _, err := tx.ExecContext(ctx, `UPDATE sm_users SET settled_credits = ?, pending_settlement = ?, updated_at = ? WHERE id = ?`, u.SettledCredits+amount, u.PendingSettlement-amount, now, userID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.store.emitSync(ctx)
	return nil
}
