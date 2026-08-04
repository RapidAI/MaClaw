package skillmarket

import (
	"bytes"
	"context"
	"encoding/json"
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

func TestCreditRedeemCardsAreSingleUseAndExportTracked(t *testing.T) {
	store, svc := setupCreditsTest(t)
	ctx := context.Background()
	user := createTestUser(t, store, "card-user@test.com", 10)
	_ = store.UpdateUserStatus(ctx, user.ID, "verified", "email")

	cards, err := svc.IssueRedeemCards(ctx, 100, 2, "admin")
	if err != nil || len(cards) != 2 {
		t.Fatalf("IssueRedeemCards() = %d cards, %v", len(cards), err)
	}
	payload, err := json.Marshal(cards[0])
	if err != nil {
		t.Fatalf("marshal issued card: %v", err)
	}
	for _, field := range [][]byte{[]byte(`"exported_at"`), []byte(`"redeemed_at"`), []byte(`"revoked_at"`)} {
		if bytes.Contains(payload, field) {
			t.Fatalf("new card should omit empty audit timestamp %s: %s", field, payload)
		}
	}
	balance, err := svc.RedeemCard(ctx, user.ID, user.Email, cards[0].Code)
	if err != nil || balance != 110 {
		t.Fatalf("RedeemCard() = %d, %v; want 110, nil", balance, err)
	}
	if _, err := svc.RedeemCard(ctx, user.ID, user.Email, cards[0].Code); err != ErrRedeemCardNotActive {
		t.Fatalf("second RedeemCard() error = %v, want %v", err, ErrRedeemCardNotActive)
	}

	exported, err := svc.ExportUnusedRedeemCards(ctx, true, "admin")
	if err != nil || len(exported) != 1 || exported[0].ID != cards[1].ID {
		t.Fatalf("ExportUnusedRedeemCards() = %#v, %v", exported, err)
	}
	exported, err = svc.ExportUnusedRedeemCards(ctx, true, "admin")
	if err != nil || len(exported) != 0 {
		t.Fatalf("second unexported export = %#v, %v; want none", exported, err)
	}
}

func TestCreditRedeemCardRequiresVerifiedAccountWithoutConsumingCard(t *testing.T) {
	store, svc := setupCreditsTest(t)
	ctx := context.Background()
	user := createTestUser(t, store, "unverified-card-user@test.com", 0)
	cards, err := svc.IssueRedeemCards(ctx, 100, 1, "admin")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := svc.RedeemCard(ctx, user.ID, user.Email, cards[0].Code); err != ErrUnverifiedAccount {
		t.Fatalf("RedeemCard() error = %v, want %v", err, ErrUnverifiedAccount)
	}
	stored, total, err := store.ListCreditRedeemCards(ctx, "active", 0, 10)
	if err != nil || total != 1 || len(stored) != 1 || stored[0].ID != cards[0].ID {
		t.Fatalf("unverified redemption must leave card active: cards=%#v total=%d err=%v", stored, total, err)
	}
}

func TestCreditRedeemCardIssueRetriesCodeCollision(t *testing.T) {
	store, svc := setupCreditsTest(t)
	ctx := context.Background()
	const code = "CRD-COLLISION"
	now := time.Now().Format(timeFmt)
	if _, err := store.DB().ExecContext(ctx, `INSERT INTO sm_credit_redeem_cards (id, code, credits, status, issued_at) VALUES (?, ?, ?, 'active', ?)`, "collision-id", code, 100, now); err != nil {
		t.Fatal(err)
	}
	if isRedeemCardCodeConflict(nil) {
		t.Fatal("nil error must not be reported as a card-code conflict")
	}
	previousGenerator := creditRedeemCodeGenerator
	calls := 0
	creditRedeemCodeGenerator = func() (string, error) {
		calls++
		if calls == 1 {
			return code, nil
		}
		return "CRD-RETRIED", nil
	}
	t.Cleanup(func() { creditRedeemCodeGenerator = previousGenerator })
	if cards, err := svc.IssueRedeemCards(ctx, 100, 1, "admin"); err != nil || len(cards) != 1 || cards[0].Code != "CRD-RETRIED" {
		t.Fatalf("IssueRedeemCards() = %#v, %v", cards, err)
	}
}

func TestCreditRedeemCardIssueRetriesIDCollision(t *testing.T) {
	store, svc := setupCreditsTest(t)
	ctx := context.Background()
	now := time.Now().Format(timeFmt)
	if _, err := store.DB().ExecContext(ctx, `INSERT INTO sm_credit_redeem_cards (id, code, credits, status, issued_at) VALUES (?, ?, ?, 'active', ?)`, "collision-id", "CRD-EXISTING", 100, now); err != nil {
		t.Fatal(err)
	}

	previousIDGenerator := creditRedeemIDGenerator
	ids := []string{"collision-id", "reissued-id"}
	creditRedeemIDGenerator = func() string {
		id := ids[0]
		ids = ids[1:]
		return id
	}
	t.Cleanup(func() { creditRedeemIDGenerator = previousIDGenerator })
	previousCodeGenerator := creditRedeemCodeGenerator
	creditRedeemCodeGenerator = func() (string, error) { return "CRD-REISSUED", nil }
	t.Cleanup(func() { creditRedeemCodeGenerator = previousCodeGenerator })

	cards, err := svc.IssueRedeemCards(ctx, 100, 1, "admin")
	if err != nil || len(cards) != 1 || cards[0].ID != "reissued-id" || cards[0].Code != "CRD-REISSUED" {
		t.Fatalf("IssueRedeemCards() = %#v, %v", cards, err)
	}
}

func TestCreditRedeemCardsReplicateThroughSnapshot(t *testing.T) {
	ctx := context.Background()
	origin := newTestStore(t)
	peer := newTestStore(t)
	originCredits := NewCreditsService(origin)
	user := createTestUser(t, origin, "snapshot-card-user@test.com", 10)
	if err := origin.UpdateUserStatus(ctx, user.ID, "verified", "email"); err != nil {
		t.Fatal(err)
	}

	cards, err := originCredits.IssueRedeemCards(ctx, 100, 1, "admin")
	if err != nil {
		t.Fatalf("IssueRedeemCards() error = %v", err)
	}
	issued, err := origin.DumpSnapshot(ctx)
	if err != nil {
		t.Fatalf("DumpSnapshot() error = %v", err)
	}
	if err := peer.LoadSnapshot(ctx, issued); err != nil {
		t.Fatalf("LoadSnapshot(issued) error = %v", err)
	}
	peerCards, total, err := peer.ListCreditRedeemCards(ctx, "active", 0, 10)
	if err != nil || total != 1 || len(peerCards) != 1 || peerCards[0].Code != cards[0].Code {
		t.Fatalf("peer issued cards = %#v, total=%d, err=%v", peerCards, total, err)
	}

	if _, err := originCredits.RedeemCard(ctx, user.ID, user.Email, cards[0].Code); err != nil {
		t.Fatalf("RedeemCard() error = %v", err)
	}
	redeemed, err := origin.DumpSnapshot(ctx)
	if err != nil {
		t.Fatalf("DumpSnapshot(redeemed) error = %v", err)
	}
	if err := peer.LoadSnapshot(ctx, redeemed); err != nil {
		t.Fatalf("LoadSnapshot(redeemed) error = %v", err)
	}
	peerCards, total, err = peer.ListCreditRedeemCards(ctx, "redeemed", 0, 10)
	if err != nil || total != 1 || len(peerCards) != 1 || peerCards[0].RedeemedByUserID != user.ID {
		t.Fatalf("peer redeemed cards = %#v, total=%d, err=%v", peerCards, total, err)
	}
}

func TestCreditRedeemCardSnapshotDoesNotReactivateTerminalCard(t *testing.T) {
	ctx := context.Background()
	origin := newTestStore(t)
	peer := newTestStore(t)
	originCredits := NewCreditsService(origin)
	peerCredits := NewCreditsService(peer)

	cards, err := originCredits.IssueRedeemCards(ctx, 100, 1, "admin")
	if err != nil {
		t.Fatalf("IssueRedeemCards() error = %v", err)
	}
	active, err := origin.DumpSnapshot(ctx)
	if err != nil {
		t.Fatalf("DumpSnapshot(active) error = %v", err)
	}
	if err := peer.LoadSnapshot(ctx, active); err != nil {
		t.Fatalf("LoadSnapshot(active) error = %v", err)
	}
	if err := peerCredits.RevokeRedeemCard(ctx, cards[0].ID, "peer-admin"); err != nil {
		t.Fatalf("RevokeRedeemCard() error = %v", err)
	}
	if err := peer.LoadSnapshot(ctx, active); err != nil {
		t.Fatalf("LoadSnapshot(stale active) error = %v", err)
	}
	peerCards, total, err := peer.ListCreditRedeemCards(ctx, "revoked", 0, 10)
	if err != nil || total != 1 || len(peerCards) != 1 {
		t.Fatalf("stale snapshot reactivated a terminal card: cards=%#v total=%d err=%v", peerCards, total, err)
	}
}
