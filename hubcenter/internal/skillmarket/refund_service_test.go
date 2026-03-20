package skillmarket

import (
	"context"
	"testing"
	"time"
)

func setupRefundTest(t *testing.T) (*Store, *RefundService, *CreditsService) {
	t.Helper()
	store := newTestStore(t)
	creditsSvc := NewCreditsService(store)
	mailer := &mockMailer{}
	refundSvc := NewRefundService(store, creditsSvc, mailer)
	return store, refundSvc, creditsSvc
}

// ── Task 42.7: 退款守恒属性测试 ─────────────────────────────────────────

func TestRefund_Conservation(t *testing.T) {
	store, refundSvc, creditsSvc := setupRefundTest(t)
	ctx := context.Background()

	buyer := createTestUser(t, store, "buyer@test.com", 1000)
	seller := createTestUser(t, store, "seller@test.com", 0)
	_ = store.UpdateUserStatus(ctx, buyer.ID, "verified", "email")
	_ = store.UpdateUserStatus(ctx, seller.ID, "verified", "email")

	price := int64(100)
	platformFee := price * 30 / 100
	sellerEarning := price - platformFee
	purchaseID := generateID()

	// 模拟购买
	_ = creditsSvc.Debit(ctx, buyer.ID, price, "skill-1", purchaseID, "buy")
	_ = creditsSvc.Credit(ctx, seller.ID, sellerEarning, true, "skill-1", purchaseID, "sold")

	// 创建 purchase record
	now := time.Now()
	rec := &PurchaseRecord{
		ID: purchaseID, BuyerEmail: buyer.Email, BuyerID: buyer.ID,
		SkillID: "skill-1", PurchasedVersion: 1, PurchaseType: "purchase",
		AmountPaid: price, PlatformFee: platformFee, SellerEarning: sellerEarning,
		SellerID: seller.ID, Status: "active", CreatedAt: now,
	}
	_ = store.CreatePurchase(ctx, rec)

	// 退款前余额
	buyerBefore, _ := creditsSvc.GetBalance(ctx, buyer.ID)
	sellerBefore, _ := store.GetUserByID(ctx, seller.ID)

	// 执行退款
	if err := refundSvc.ProcessRefund(ctx, purchaseID, "admin@test.com", "test refund"); err != nil {
		t.Fatal(err)
	}

	// 验证守恒
	buyerAfter, _ := creditsSvc.GetBalance(ctx, buyer.ID)
	sellerAfter, _ := store.GetUserByID(ctx, seller.ID)

	// 买家应退还 price
	if buyerAfter-buyerBefore != price {
		t.Errorf("buyer refund: got %d, want %d", buyerAfter-buyerBefore, price)
	}

	// 上传者 settled 应减少 sellerEarning
	if sellerBefore.SettledCredits-sellerAfter.SettledCredits != sellerEarning {
		t.Errorf("seller deduction: got %d, want %d",
			sellerBefore.SettledCredits-sellerAfter.SettledCredits, sellerEarning)
	}
}

// ── Task 42.8: 退款单元测试 ─────────────────────────────────────────────

func TestRefund_AlreadyRefunded(t *testing.T) {
	store, refundSvc, creditsSvc := setupRefundTest(t)
	ctx := context.Background()

	buyer := createTestUser(t, store, "buyer2@test.com", 1000)
	seller := createTestUser(t, store, "seller2@test.com", 0)

	purchaseID := generateID()
	_ = creditsSvc.Debit(ctx, buyer.ID, 50, "skill-2", purchaseID, "buy")
	_ = creditsSvc.Credit(ctx, seller.ID, 35, true, "skill-2", purchaseID, "sold")

	now := time.Now()
	rec := &PurchaseRecord{
		ID: purchaseID, BuyerEmail: buyer.Email, BuyerID: buyer.ID,
		SkillID: "skill-2", PurchasedVersion: 1, PurchaseType: "purchase",
		AmountPaid: 50, PlatformFee: 15, SellerEarning: 35,
		SellerID: seller.ID, Status: "active", CreatedAt: now,
	}
	_ = store.CreatePurchase(ctx, rec)

	// 第一次退款成功
	if err := refundSvc.ProcessRefund(ctx, purchaseID, "admin@test.com", "reason"); err != nil {
		t.Fatal(err)
	}

	// 第二次退款应失败
	err := refundSvc.ProcessRefund(ctx, purchaseID, "admin@test.com", "reason")
	if err != ErrAlreadyRefunded {
		t.Errorf("expected ErrAlreadyRefunded, got %v", err)
	}
}

func TestRefund_SellerDebtWhenInsufficientBalance(t *testing.T) {
	store, refundSvc, creditsSvc := setupRefundTest(t)
	ctx := context.Background()

	buyer := createTestUser(t, store, "buyer3@test.com", 1000)
	seller := createTestUser(t, store, "seller3@test.com", 0)

	purchaseID := generateID()
	_ = creditsSvc.Debit(ctx, buyer.ID, 100, "skill-3", purchaseID, "buy")
	// 上传者入账 70 (settled)
	_ = creditsSvc.Credit(ctx, seller.ID, 70, true, "skill-3", purchaseID, "sold")
	// 上传者提现 60，剩余 settled=10
	_ = store.UpdateUserStatus(ctx, seller.ID, "verified", "email")
	_ = creditsSvc.Withdraw(ctx, seller.ID, 60)

	now := time.Now()
	rec := &PurchaseRecord{
		ID: purchaseID, BuyerEmail: buyer.Email, BuyerID: buyer.ID,
		SkillID: "skill-3", PurchasedVersion: 1, PurchaseType: "purchase",
		AmountPaid: 100, PlatformFee: 30, SellerEarning: 70,
		SellerID: seller.ID, Status: "active", CreatedAt: now,
	}
	_ = store.CreatePurchase(ctx, rec)

	// 退款：上传者 settled=10 < earning=70, 差额 60 记为 debt
	if err := refundSvc.ProcessRefund(ctx, purchaseID, "admin@test.com", "reason"); err != nil {
		t.Fatal(err)
	}

	sellerAfter, _ := store.GetUserByID(ctx, seller.ID)
	if sellerAfter.SettledCredits != 0 {
		t.Errorf("seller settled: got %d, want 0", sellerAfter.SettledCredits)
	}
	if sellerAfter.Debt != 60 {
		t.Errorf("seller debt: got %d, want 60", sellerAfter.Debt)
	}
}

func TestRefund_PurchaseRecordMarkedRefunded(t *testing.T) {
	store, refundSvc, creditsSvc := setupRefundTest(t)
	ctx := context.Background()

	buyer := createTestUser(t, store, "buyer4@test.com", 500)
	seller := createTestUser(t, store, "seller4@test.com", 0)

	purchaseID := generateID()
	_ = creditsSvc.Debit(ctx, buyer.ID, 50, "skill-4", purchaseID, "buy")
	_ = creditsSvc.Credit(ctx, seller.ID, 35, true, "skill-4", purchaseID, "sold")

	now := time.Now()
	rec := &PurchaseRecord{
		ID: purchaseID, BuyerEmail: buyer.Email, BuyerID: buyer.ID,
		SkillID: "skill-4", PurchasedVersion: 1, PurchaseType: "purchase",
		AmountPaid: 50, PlatformFee: 15, SellerEarning: 35,
		SellerID: seller.ID, Status: "active", CreatedAt: now,
	}
	_ = store.CreatePurchase(ctx, rec)

	_ = refundSvc.ProcessRefund(ctx, purchaseID, "admin@test.com", "reason")

	// 验证 purchase record 状态
	pr, err := store.GetPurchaseByID(ctx, purchaseID)
	if err != nil {
		t.Fatal(err)
	}
	if pr.Status != "refunded" {
		t.Errorf("purchase status: got %s, want refunded", pr.Status)
	}
}
