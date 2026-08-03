package skillmarket

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// seedPetStoreTables installs the minimal pet store schema that the httpapi
// layer normally creates lazily, so refund paths can be tested in isolation.
func seedPetStoreTables(t *testing.T, store *Store) {
	t.Helper()
	if _, err := store.db.Exec(`CREATE TABLE IF NOT EXISTS sm_pet_store_packs (
		id TEXT PRIMARY KEY, owner_user_id TEXT NOT NULL, owner_email TEXT NOT NULL, source_pack_id TEXT NOT NULL DEFAULT '',
		name TEXT NOT NULL, description TEXT NOT NULL DEFAULT '', version TEXT NOT NULL DEFAULT '1.0.0',
		price INTEGER NOT NULL DEFAULT 0, status TEXT NOT NULL DEFAULT 'active', zip_path TEXT NOT NULL,
		package_size INTEGER NOT NULL DEFAULT 0, download_count INTEGER NOT NULL DEFAULT 0,
		purchase_count INTEGER NOT NULL DEFAULT 0, sales_amount INTEGER NOT NULL DEFAULT 0,
		created_at TEXT NOT NULL, updated_at TEXT NOT NULL
	)`); err != nil {
		t.Fatalf("create pet store packs table: %v", err)
	}
	if _, err := store.db.Exec(`CREATE TABLE IF NOT EXISTS sm_pet_store_purchases (
		id TEXT PRIMARY KEY, pack_id TEXT NOT NULL, buyer_user_id TEXT NOT NULL, buyer_email TEXT NOT NULL,
		amount_paid INTEGER NOT NULL DEFAULT 0, status TEXT NOT NULL DEFAULT 'active', created_at TEXT NOT NULL,
		UNIQUE(pack_id, buyer_user_id)
	)`); err != nil {
		t.Fatalf("create pet store purchases table: %v", err)
	}
}

func seedPetStorePurchase(t *testing.T, store *Store, packID, packName string, seller *SkillMarketUser, purchaseID string, buyer *SkillMarketUser, amount int64) {
	t.Helper()
	now := time.Now().Format(timeFmt)
	if _, err := store.db.Exec(`INSERT INTO sm_pet_store_packs (id, owner_user_id, owner_email, source_pack_id, name, zip_path, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		packID, seller.ID, seller.Email, "src-"+packID, packName, "unused.zip", now, now); err != nil {
		t.Fatalf("seed pet pack: %v", err)
	}
	if _, err := store.db.Exec(`INSERT INTO sm_pet_store_purchases (id, pack_id, buyer_user_id, buyer_email, amount_paid, status, created_at) VALUES (?, ?, ?, ?, ?, 'active', ?)`,
		purchaseID, packID, buyer.ID, buyer.Email, amount, now); err != nil {
		t.Fatalf("seed pet purchase: %v", err)
	}
}

func TestPetStoreRefund_Conservation(t *testing.T) {
	store, refundSvc, creditsSvc := setupRefundTest(t)
	seedPetStoreTables(t, store)
	ctx := context.Background()

	buyer := createTestUser(t, store, "pet-buyer@test.com", 1000)
	seller := createTestUser(t, store, "pet-seller@test.com", 0)

	price := int64(100)
	platformFee := price * PetStorePlatformFeePct / 100
	sellerEarning := price - platformFee
	purchaseID := "pet_" + generateID()

	// 模拟宠物包购买的资金流（与 CompletePetStorePurchase 一致）。
	_ = creditsSvc.Debit(ctx, buyer.ID, price, "pet_pack-1", purchaseID, "buy")
	_ = creditsSvc.Credit(ctx, seller.ID, sellerEarning, true, "pet_pack-1", purchaseID, "sold")
	seedPetStorePurchase(t, store, "pet_pack-1", "机器猫", seller, purchaseID, buyer, price)

	buyerBefore, _ := creditsSvc.GetBalance(ctx, buyer.ID)
	sellerBefore, _ := store.GetUserByID(ctx, seller.ID)

	if err := refundSvc.ProcessRefund(ctx, purchaseID, "admin@test.com", "test refund"); err != nil {
		t.Fatal(err)
	}

	buyerAfter, _ := creditsSvc.GetBalance(ctx, buyer.ID)
	sellerAfter, _ := store.GetUserByID(ctx, seller.ID)
	if buyerAfter-buyerBefore != price {
		t.Errorf("buyer refund: got %d, want %d", buyerAfter-buyerBefore, price)
	}
	if sellerBefore.SettledCredits-sellerAfter.SettledCredits != sellerEarning {
		t.Errorf("seller deduction: got %d, want %d", sellerBefore.SettledCredits-sellerAfter.SettledCredits, sellerEarning)
	}

	var status string
	if err := store.db.QueryRow(`SELECT status FROM sm_pet_store_purchases WHERE id = ?`, purchaseID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "refunded" {
		t.Errorf("pet purchase status: got %s, want refunded", status)
	}

	// 重复退款必须被拒绝。
	if err := refundSvc.ProcessRefund(ctx, purchaseID, "admin@test.com", "again"); err != ErrAlreadyRefunded {
		t.Errorf("expected ErrAlreadyRefunded, got %v", err)
	}
}

func TestPetStoreRefund_SellerDebtWhenInsufficientBalance(t *testing.T) {
	store, refundSvc, creditsSvc := setupRefundTest(t)
	seedPetStoreTables(t, store)
	ctx := context.Background()

	buyer := createTestUser(t, store, "pet-buyer2@test.com", 1000)
	seller := createTestUser(t, store, "pet-seller2@test.com", 0)

	price := int64(100)
	sellerEarning := price - price*PetStorePlatformFeePct/100
	purchaseID := "pet_" + generateID()
	_ = creditsSvc.Debit(ctx, buyer.ID, price, "pet_pack-2", purchaseID, "buy")
	_ = creditsSvc.Credit(ctx, seller.ID, sellerEarning, true, "pet_pack-2", purchaseID, "sold")
	// 卖家提现后 settled 不足，差额应记为 debt。
	_ = store.UpdateUserStatus(ctx, seller.ID, "verified", "email")
	_ = creditsSvc.Withdraw(ctx, seller.ID, sellerEarning-10)
	seedPetStorePurchase(t, store, "pet_pack-2", "机器猫2", seller, purchaseID, buyer, price)

	if err := refundSvc.ProcessRefund(ctx, purchaseID, "admin@test.com", "reason"); err != nil {
		t.Fatal(err)
	}
	sellerAfter, _ := store.GetUserByID(ctx, seller.ID)
	if sellerAfter.SettledCredits != 0 {
		t.Errorf("seller settled: got %d, want 0", sellerAfter.SettledCredits)
	}
	if sellerAfter.Debt != sellerEarning-10 {
		t.Errorf("seller debt: got %d, want %d", sellerAfter.Debt, sellerEarning-10)
	}
}

func TestListPurchasesIncludesPetStoreRecords(t *testing.T) {
	store, refundSvc, _ := setupRefundTest(t)
	seedPetStoreTables(t, store)
	ctx := context.Background()

	buyer := createTestUser(t, store, "pet-buyer3@test.com", 1000)
	seller := createTestUser(t, store, "pet-seller3@test.com", 0)
	seedPetStorePurchase(t, store, "pet_pack-3", "机器猫3", seller, "pet_purchase-3", buyer, 100)

	records, total, err := refundSvc.ListPurchases(ctx, "", "", 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(records) != 1 {
		t.Fatalf("total=%d records=%d, want 1/1", total, len(records))
	}
	rec := records[0]
	if rec.PurchaseType != "pet_pack" || rec.SkillID != "pet_pack-3" || rec.SellerID != seller.ID || rec.BuyerID != buyer.ID {
		t.Errorf("unexpected pet record: %+v", rec)
	}
	wantFee := int64(100) * PetStorePlatformFeePct / 100
	if rec.PlatformFee != wantFee || rec.SellerEarning != 100-wantFee {
		t.Errorf("fee=%d earning=%d, want %d/%d", rec.PlatformFee, rec.SellerEarning, wantFee, 100-wantFee)
	}

	// skill_id 过滤应命中宠物包的 pack_id。
	records, total, err = refundSvc.ListPurchases(ctx, "", "pet_pack-3", 0, 20)
	if err != nil || total != 1 || len(records) != 1 {
		t.Fatalf("pack filter total=%d records=%d err=%v, want 1/1", total, len(records), err)
	}
}

// TestPetStoreRefund_ConcurrentDoubleRefundIsRepelled guards the in-transaction
// status re-check: concurrent refund requests for the same entitlement must not
// repay the buyer more than once. A file-backed database is required because
// concurrent pooled connections to ":memory:" would be independent databases.
func TestPetStoreRefund_ConcurrentDoubleRefundIsRepelled(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "refund-race.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	// Mirror the production write pool (MaxWriteOpenConns: 1): refund
	// transactions serialize on a single connection instead of losing lock
	// races, so a missing in-transaction re-check would repay the buyer twice.
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	store, err := NewStore(db, db)
	if err != nil {
		t.Fatalf("new skillmarket store: %v", err)
	}
	creditsSvc := NewCreditsService(store)
	refundSvc := NewRefundService(store, creditsSvc, &mockMailer{})
	seedPetStoreTables(t, store)
	ctx := context.Background()

	buyer := createTestUser(t, store, "pet-race-buyer@test.com", 0)
	seller := createTestUser(t, store, "pet-race-seller@test.com", 0)
	price := int64(100)
	purchaseID := "pet_" + generateID()
	seedPetStorePurchase(t, store, "pet_pack-race", "竞速猫", seller, purchaseID, buyer, price)

	const workers = 8
	start := make(chan struct{})
	errs := make([]error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			errs[i] = refundSvc.ProcessRefund(ctx, purchaseID, "admin@test.com", "race")
		}(i)
	}
	close(start)
	wg.Wait()

	succeeded := 0
	for _, err := range errs {
		if err == nil {
			succeeded++
			continue
		}
		// Production serializes write transactions on a single connection, which
		// surfaces ErrAlreadyRefunded; a multi-connection pool may instead lose a
		// lock race. Both refuse the duplicate without repaying the buyer.
		if errors.Is(err, ErrAlreadyRefunded) || strings.Contains(strings.ToLower(err.Error()), "locked") {
			continue
		}
		t.Fatalf("unexpected concurrent refund error: %v", err)
	}
	if succeeded != 1 {
		t.Fatalf("concurrent refunds succeeded=%d, want exactly 1", succeeded)
	}
	buyerAfter, err := creditsSvc.GetBalance(ctx, buyer.ID)
	if err != nil {
		t.Fatal(err)
	}
	if buyerAfter != price {
		t.Errorf("buyer credited %d, want exactly %d (double refund repelled)", buyerAfter, price)
	}
}
