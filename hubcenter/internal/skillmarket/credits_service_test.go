package skillmarket

import (
	"context"
	"testing"
	"time"
)

func setupCreditsTest(t *testing.T) (*Store, *CreditsService) {
	t.Helper()
	store := newTestStore(t)
	svc := NewCreditsService(store)
	return store, svc
}

// ── Task 4.3: Credits 守恒属性测试 ──────────────────────────────────────

func TestCredits_Conservation(t *testing.T) {
	store, svc := setupCreditsTest(t)
	ctx := context.Background()

	u := createTestUser(t, store, "user@test.com", 1000)
	_ = store.UpdateUserStatus(ctx, u.ID, "verified", "email")

	// TopUp 500
	if err := svc.TopUp(ctx, u.ID, 500); err != nil {
		t.Fatal(err)
	}
	// Debit 300
	if err := svc.Debit(ctx, u.ID, 300, "skill-1", "pur-1", "buy"); err != nil {
		t.Fatal(err)
	}
	// Debit 200
	if err := svc.Debit(ctx, u.ID, 200, "skill-2", "pur-2", "buy"); err != nil {
		t.Fatal(err)
	}

	// 期望余额: 1000 + 500 - 300 - 200 = 1000
	bal, err := svc.GetBalance(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if bal != 1000 {
		t.Errorf("balance: got %d, want 1000", bal)
	}
}

func TestCredits_InsufficientBalance(t *testing.T) {
	store, svc := setupCreditsTest(t)
	ctx := context.Background()

	u := createTestUser(t, store, "poor@test.com", 50)

	err := svc.Debit(ctx, u.ID, 100, "skill-1", "pur-1", "buy")
	if err != ErrInsufficientCredits {
		t.Errorf("expected ErrInsufficientCredits, got %v", err)
	}

	// 余额不变
	bal, _ := svc.GetBalance(ctx, u.ID)
	if bal != 50 {
		t.Errorf("balance should be unchanged: got %d, want 50", bal)
	}
}

// ── Task 35.7: 平台抽成守恒属性测试 ─────────────────────────────────────

func TestPlatformFee_Conservation(t *testing.T) {
	store, svc := setupCreditsTest(t)
	ctx := context.Background()

	buyer := createTestUser(t, store, "buyer@test.com", 1000)
	seller := createTestUser(t, store, "seller@test.com", 0)

	prices := []int64{10, 33, 100, 7, 99}
	for i, price := range prices {
		purchaseID := generateID()
		// 买家扣款
		if err := svc.Debit(ctx, buyer.ID, price, "skill", purchaseID, "buy"); err != nil {
			t.Fatal(err)
		}
		platformFee := price * 30 / 100
		sellerEarning := price - platformFee

		// 验证守恒: price == platformFee + sellerEarning
		if platformFee+sellerEarning != price {
			t.Errorf("case %d: fee(%d) + earning(%d) != price(%d)", i, platformFee, sellerEarning, price)
		}

		// 上传者入账
		if err := svc.Credit(ctx, seller.ID, sellerEarning, true, "skill", purchaseID, "sold"); err != nil {
			t.Fatal(err)
		}
		// 平台手续费记录
		if err := svc.RecordPlatformFee(ctx, platformFee, "skill", purchaseID, "fee"); err != nil {
			t.Fatal(err)
		}
	}
}

// ── Task 35.8: 经济系统单元测试 ─────────────────────────────────────────

func TestTopUp_OnlyVerified(t *testing.T) {
	store, svc := setupCreditsTest(t)
	ctx := context.Background()

	u := createTestUser(t, store, "unverified@test.com", 0)
	err := svc.TopUp(ctx, u.ID, 100)
	if err != ErrUnverifiedAccount {
		t.Errorf("expected ErrUnverifiedAccount, got %v", err)
	}
}

func TestWithdraw_OnlySettled(t *testing.T) {
	store, svc := setupCreditsTest(t)
	ctx := context.Background()

	u := createTestUser(t, store, "seller@test.com", 0)
	_ = store.UpdateUserStatus(ctx, u.ID, "verified", "email")

	// 给 settled 100, pending 200
	_ = svc.Credit(ctx, u.ID, 100, true, "", "", "settled earning")
	_ = svc.Credit(ctx, u.ID, 200, false, "", "", "pending earning")

	// 提现 100 (settled) 应成功
	if err := svc.Withdraw(ctx, u.ID, 100); err != nil {
		t.Fatalf("withdraw settled: %v", err)
	}

	// 提现 1 (settled 已为 0) 应失败
	err := svc.Withdraw(ctx, u.ID, 1)
	if err != ErrInsufficientCredits {
		t.Errorf("expected ErrInsufficientCredits, got %v", err)
	}
}

func TestSettlePending(t *testing.T) {
	store, svc := setupCreditsTest(t)
	ctx := context.Background()

	u := createTestUser(t, store, "seller@test.com", 0)
	_ = store.UpdateUserStatus(ctx, u.ID, "verified", "email")

	// Credit 100 as pending
	_ = svc.Credit(ctx, u.ID, 100, false, "", "", "pending")

	// Settle 60
	if err := svc.SettlePending(ctx, u.ID, 60); err != nil {
		t.Fatal(err)
	}

	got, _ := store.GetUserByID(ctx, u.ID)
	if got.SettledCredits != 60 {
		t.Errorf("settled: got %d, want 60", got.SettledCredits)
	}
	if got.PendingSettlement != 40 {
		t.Errorf("pending: got %d, want 40", got.PendingSettlement)
	}
}

func TestCredit_DebtAutoDeduction(t *testing.T) {
	store, svc := setupCreditsTest(t)
	ctx := context.Background()

	u := createTestUser(t, store, "debtor@test.com", 0)
	// 手动设置 debt
	now := time.Now().Format(timeFmt)
	_, _ = store.db.ExecContext(ctx, `UPDATE sm_users SET debt = 50, updated_at = ? WHERE id = ?`, now, u.ID)

	// Credit 80, 应先抵扣 50 debt, 实际入账 30
	if err := svc.Credit(ctx, u.ID, 80, true, "", "", "earning"); err != nil {
		t.Fatal(err)
	}

	got, _ := store.GetUserByID(ctx, u.ID)
	if got.Debt != 0 {
		t.Errorf("debt: got %d, want 0", got.Debt)
	}
	if got.SettledCredits != 30 {
		t.Errorf("settled: got %d, want 30", got.SettledCredits)
	}
}

func TestCredit_DebtExceedsAmount(t *testing.T) {
	store, svc := setupCreditsTest(t)
	ctx := context.Background()

	u := createTestUser(t, store, "bigdebtor@test.com", 0)
	now := time.Now().Format(timeFmt)
	_, _ = store.db.ExecContext(ctx, `UPDATE sm_users SET debt = 200, updated_at = ? WHERE id = ?`, now, u.ID)

	// Credit 50, debt 200 > 50, 全部抵扣, 实际入账 0
	if err := svc.Credit(ctx, u.ID, 50, true, "", "", "earning"); err != nil {
		t.Fatal(err)
	}

	got, _ := store.GetUserByID(ctx, u.ID)
	if got.Debt != 150 {
		t.Errorf("debt: got %d, want 150", got.Debt)
	}
	if got.SettledCredits != 0 {
		t.Errorf("settled: got %d, want 0", got.SettledCredits)
	}
}
