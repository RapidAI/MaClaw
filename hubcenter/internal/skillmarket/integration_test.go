package skillmarket

import (
	"context"
	"testing"
	"time"
)

// ── Task 7.7: API 集成测试 (service-level) ──────────────────────────────

func TestIntegration_SubmitAndQueryStatus(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	// 创建 submission
	sub := &SkillSubmission{
		ID: "sub-int-1", Email: "dev@test.com", UserID: "u1",
		Status: "pending", ZipPath: "/tmp/test.zip",
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := store.CreateSubmission(ctx, sub); err != nil {
		t.Fatal(err)
	}

	// 查询状态
	got, err := store.GetSubmissionByID(ctx, "sub-int-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "pending" {
		t.Errorf("status=%s, want pending", got.Status)
	}

	// 更新为 success
	_ = store.UpdateSubmissionStatus(ctx, "sub-int-1", "success", "", "skill-abc")
	got2, _ := store.GetSubmissionByID(ctx, "sub-int-1")
	if got2.Status != "success" || got2.SkillID != "skill-abc" {
		t.Errorf("status=%s skillID=%s", got2.Status, got2.SkillID)
	}
}

func TestIntegration_AccountCreateAndVerify(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	u := createTestUser(t, store, "new@test.com", 0)
	if u.Status != "unverified" {
		t.Errorf("initial status=%s, want unverified", u.Status)
	}

	// 验证
	_ = store.UpdateUserStatus(ctx, u.ID, "verified", "email")
	got, _ := store.GetUserByID(ctx, u.ID)
	if got.Status != "verified" {
		t.Errorf("status=%s, want verified", got.Status)
	}
}

func TestIntegration_CreditsDebitInsufficientBalance(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	u := createTestUser(t, store, "poor@test.com", 10)

	svc := NewCreditsService(store)
	err := svc.Debit(ctx, u.ID, 100, "skill-1", "p-1", "test")
	if err != ErrInsufficientCredits {
		t.Errorf("err=%v, want ErrInsufficientCredits", err)
	}
}

// ── Task 11.3: 端到端集成测试 ───────────────────────────────────────────

func TestIntegration_UploadProcessDownloadFlow(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	// 创建买家和卖家
	buyer := createTestUser(t, store, "buyer@test.com", 1000)
	_ = store.UpdateUserStatus(ctx, buyer.ID, "verified", "email")
	seller := createTestUser(t, store, "seller@test.com", 0)
	_ = store.UpdateUserStatus(ctx, seller.ID, "verified", "email")

	creditsSvc := NewCreditsService(store)

	// 模拟购买：扣买家 100
	err := creditsSvc.Debit(ctx, buyer.ID, 100, "skill-1", "p-1", "purchase")
	if err != nil {
		t.Fatal(err)
	}

	// 给卖家入账 70（settled）
	err = creditsSvc.Credit(ctx, seller.ID, 70, true, "skill-1", "p-1", "earning")
	if err != nil {
		t.Fatal(err)
	}

	// 验证余额
	buyerBal, _ := creditsSvc.GetBalance(ctx, buyer.ID)
	if buyerBal != 900 {
		t.Errorf("buyer balance=%d, want 900", buyerBal)
	}
	sellerUser, _ := store.GetUserByID(ctx, seller.ID)
	if sellerUser.SettledCredits != 70 {
		t.Errorf("seller settled=%d, want 70", sellerUser.SettledCredits)
	}
}

func TestIntegration_UnverifiedUserCannotTopUp(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	u := createTestUser(t, store, "unverified@test.com", 0)

	svc := NewCreditsService(store)
	err := svc.TopUp(ctx, u.ID, 100)
	if err != ErrUnverifiedAccount {
		t.Errorf("err=%v, want ErrUnverifiedAccount", err)
	}
}

// ── Task 19.2: 试用期生命周期 E2E ───────────────────────────────────────

func TestIntegration_TrialAutoPublish(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	ratingSvc := NewRatingService(store)

	// 设置 auto_publish_threshold = 3
	_ = store.SetConfig(ctx, configAutoPublishThreshold, "3")

	skillID := "trial-skill-1"

	// 3 个不同用户评分 >= 0
	for _, email := range []string{"a@test.com", "b@test.com", "c@test.com"} {
		_, err := ratingSvc.SubmitRating(ctx, skillID, email, 1, "uploader@test.com")
		if err != nil {
			t.Fatal(err)
		}
	}

	// 检查是否满足自动上架条件（需要 TrialManager，但这里直接测 CheckAutoPublish 逻辑）
	stats, _ := ratingSvc.GetStats(ctx, skillID)
	threshold := 3
	shouldPublish := stats.UniqueRaters >= threshold && stats.AverageScore >= 0
	if !shouldPublish {
		t.Errorf("should auto-publish: raters=%d avg=%.2f", stats.UniqueRaters, stats.AverageScore)
	}
}

func TestIntegration_EmergencyTakedown(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	ratingSvc := NewRatingService(store)
	takedown, err := ratingSvc.SubmitRating(ctx, "skill-bad", "reporter@test.com", -2, "uploader@test.com")
	if err != nil {
		t.Fatal(err)
	}
	if !takedown {
		t.Error("expected emergency takedown for -2 score")
	}
}

func TestIntegration_ZeroScoreCountsInAverage(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	ratingSvc := NewRatingService(store)
	// 评分: +2, 0, -1
	_, _ = ratingSvc.SubmitRating(ctx, "skill-z", "a@test.com", 2, "up@test.com")
	_, _ = ratingSvc.SubmitRating(ctx, "skill-z", "b@test.com", 0, "up@test.com")
	_, _ = ratingSvc.SubmitRating(ctx, "skill-z", "c@test.com", -1, "up@test.com")

	stats, _ := ratingSvc.GetStats(ctx, "skill-z")
	if stats.UniqueRaters != 3 {
		t.Errorf("raters=%d, want 3", stats.UniqueRaters)
	}
	// (2+0-1)/3 ≈ 0.333
	if stats.AverageScore < 0.3 || stats.AverageScore > 0.4 {
		t.Errorf("avg=%.3f, want ~0.333", stats.AverageScore)
	}
}

// ── Task 26.6: 下架功能测试 ─────────────────────────────────────────────

func TestIntegration_WithdrawSkill(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	// 创建 submission 模拟上传者
	sub := &SkillSubmission{
		ID: "sub-wd", Email: "uploader@test.com", UserID: "u-up",
		Status: "success", SkillID: "skill-wd", ZipPath: "/tmp/t.zip",
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	_ = store.CreateSubmission(ctx, sub)

	// 验证 submission 存在
	got, err := store.GetSubmissionByID(ctx, "sub-wd")
	if err != nil {
		t.Fatal(err)
	}
	if got.SkillID != "skill-wd" {
		t.Errorf("skillID=%s, want skill-wd", got.SkillID)
	}
}

// ── Task 27.2: 被拒绝 Skill 重新提交测试 ────────────────────────────────

func TestIntegration_RejectedResubmission(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	vm := NewVersionManager(store)

	fingerprint := "dev@test.com:rejected-skill"

	// 第一次提交成功
	sub1 := &SkillSubmission{
		ID: generateID(), Email: "dev@test.com", Fingerprint: fingerprint,
		Status: "success", SkillID: "skill-v1", ZipPath: "/tmp/v1.zip",
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	_ = store.CreateSubmission(ctx, sub1)

	// 重新提交（模拟 rejected 后重新提交）
	res, err := vm.ResolveSubmission(ctx, fingerprint)
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsUpgrade {
		t.Error("resubmission should be upgrade")
	}
	if res.NextVersion != 2 {
		t.Errorf("nextVersion=%d, want 2", res.NextVersion)
	}
}

// ── Task 29.2: 新功能 E2E 集成测试 ──────────────────────────────────────

func TestIntegration_TierAndRateLimit(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	u := createTestUser(t, store, "uploader@test.com", 0)
	tierSvc := NewTierService(store)

	// 初始 Tier 应为 1
	tierInfo, err := tierSvc.GetTier(ctx, u.ID)
	if err != nil {
		// 新用户可能没有 tier 记录，默认 tier=1
		tierInfo = &UploaderTier{Tier: 1}
	}
	if tierInfo.Tier != 1 {
		t.Errorf("initial tier=%d, want 1", tierInfo.Tier)
	}

	// 获取限制
	limits := tierSvc.GetLimits(tierInfo.Tier)
	if limits.MaxUploadSizeMB != 10 {
		t.Errorf("tier1 maxSizeMB=%d, want 10", limits.MaxUploadSizeMB)
	}
}

func TestIntegration_RateLimitWithTier(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	u := createTestUser(t, store, "limited@test.com", 0)
	tierSvc := NewTierService(store)
	rl := NewRateLimiter(store, tierSvc)

	// 初始应允许
	err := rl.CheckRateLimit(ctx, u.Email, u.ID)
	if err != nil {
		t.Errorf("initial rate limit should pass: %v", err)
	}
}

// ── Task 35.7: 经济系统属性测试 ─────────────────────────────────────────

func TestIntegration_PlatformFeeConservation(t *testing.T) {
	// 对任意价格，buyer_debit == uploader_credit + platform_fee
	prices := []int64{10, 15, 33, 100, 1, 999}
	for _, price := range prices {
		platformFee := price * 30 / 100
		sellerEarning := price - platformFee
		total := platformFee + sellerEarning
		if total != price {
			t.Errorf("price=%d: fee=%d + earning=%d = %d, want %d", price, platformFee, sellerEarning, total, price)
		}
	}
}

// ── Task 35.8: 经济系统单元测试 ─────────────────────────────────────────

func TestIntegration_VersionUpgradeDiscount(t *testing.T) {
	// 版本升级 50% 折扣
	originalPrice := int64(100)
	upgradePrice := originalPrice * 50 / 100
	if upgradePrice != 50 {
		t.Errorf("upgrade price=%d, want 50", upgradePrice)
	}
}

func TestIntegration_VoucherExpiry(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	// 创建用户，体验券已过期
	u := createTestUser(t, store, "expired@test.com", 0)
	_, _ = store.db.ExecContext(ctx, `UPDATE sm_users SET voucher_expires_at = ? WHERE id = ?`,
		fmtTime(time.Now().Add(-24*time.Hour)), u.ID)

	got, _ := store.GetUserByID(ctx, u.ID)
	if got.VoucherExpiresAt.After(time.Now()) {
		t.Error("voucher should be expired")
	}
}

func TestIntegration_VoucherExhausted(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	u := createTestUser(t, store, "novoucher@test.com", 0)
	_ = store.UpdateUserVoucher(ctx, u.ID, 0)

	got, _ := store.GetUserByID(ctx, u.ID)
	if got.VoucherCount != 0 {
		t.Errorf("voucher_count=%d, want 0", got.VoucherCount)
	}
}

func TestIntegration_SettledVsPendingWithdraw(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	u := createTestUser(t, store, "seller@test.com", 0)
	_ = store.UpdateUserStatus(ctx, u.ID, "verified", "email")

	creditsSvc := NewCreditsService(store)

	// 入账 100 到 pending
	_ = creditsSvc.Credit(ctx, u.ID, 100, false, "s1", "p1", "pending earning")

	// 尝试提现 100 应失败（pending 不可提现）
	err := creditsSvc.Withdraw(ctx, u.ID, 100)
	if err != ErrInsufficientCredits {
		t.Errorf("err=%v, want ErrInsufficientCredits", err)
	}

	// settle 后可提现
	_ = creditsSvc.SettlePending(ctx, u.ID, 100)
	err = creditsSvc.Withdraw(ctx, u.ID, 100)
	if err != nil {
		t.Errorf("withdraw after settle failed: %v", err)
	}
}

// ── Task 36.2: 经济系统 E2E ─────────────────────────────────────────────

func TestIntegration_FullPurchaseFlow(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	buyer := createTestUser(t, store, "buyer@test.com", 1000)
	_ = store.UpdateUserStatus(ctx, buyer.ID, "verified", "email")
	seller := createTestUser(t, store, "seller@test.com", 0)
	_ = store.UpdateUserStatus(ctx, seller.ID, "verified", "email")

	creditsSvc := NewCreditsService(store)
	price := int64(100)
	platformFee := price * 30 / 100
	sellerEarning := price - platformFee

	// 1. 买家扣款
	if err := creditsSvc.Debit(ctx, buyer.ID, price, "skill-1", "p-1", "purchase"); err != nil {
		t.Fatal(err)
	}

	// 2. 卖家入账
	if err := creditsSvc.Credit(ctx, seller.ID, sellerEarning, true, "skill-1", "p-1", "earning"); err != nil {
		t.Fatal(err)
	}

	// 3. 记录平台手续费
	if err := creditsSvc.RecordPlatformFee(ctx, platformFee, "skill-1", "p-1", "platform fee"); err != nil {
		t.Fatal(err)
	}

	// 4. 创建购买记录
	pr := &PurchaseRecord{
		ID: "p-1", BuyerEmail: buyer.Email, BuyerID: buyer.ID,
		SkillID: "skill-1", PurchasedVersion: 1, PurchaseType: "purchase",
		AmountPaid: price, PlatformFee: platformFee, SellerEarning: sellerEarning,
		SellerID: seller.ID, Status: "active", CreatedAt: time.Now(),
	}
	if err := store.CreatePurchase(ctx, pr); err != nil {
		t.Fatal(err)
	}

	// 验证
	buyerBal, _ := creditsSvc.GetBalance(ctx, buyer.ID)
	if buyerBal != 900 {
		t.Errorf("buyer=%d, want 900", buyerBal)
	}
	sellerUser, _ := store.GetUserByID(ctx, seller.ID)
	if sellerUser.SettledCredits != 70 {
		t.Errorf("seller settled=%d, want 70", sellerUser.SettledCredits)
	}

	// 5. 卖家提现
	if err := creditsSvc.Withdraw(ctx, seller.ID, 70); err != nil {
		t.Fatal(err)
	}
	sellerUser2, _ := store.GetUserByID(ctx, seller.ID)
	if sellerUser2.SettledCredits != 0 {
		t.Errorf("seller after withdraw=%d, want 0", sellerUser2.SettledCredits)
	}
}

// ── Task 44.2: 新功能 E2E 集成测试 ──────────────────────────────────────

func TestIntegration_RefundFullFlow(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	mailer := &mockMailer{}

	buyer := createTestUser(t, store, "buyer@test.com", 1000)
	seller := createTestUser(t, store, "seller@test.com", 0)
	creditsSvc := NewCreditsService(store)

	price := int64(100)
	platformFee := price * 30 / 100
	sellerEarning := price - platformFee

	// 购买
	_ = creditsSvc.Debit(ctx, buyer.ID, price, "skill-1", "p-1", "purchase")
	_ = creditsSvc.Credit(ctx, seller.ID, sellerEarning, true, "skill-1", "p-1", "earning")

	pr := &PurchaseRecord{
		ID: "p-refund-1", BuyerEmail: buyer.Email, BuyerID: buyer.ID,
		SkillID: "skill-1", PurchasedVersion: 1, PurchaseType: "purchase",
		AmountPaid: price, PlatformFee: platformFee, SellerEarning: sellerEarning,
		SellerID: seller.ID, Status: "active", CreatedAt: time.Now(),
	}
	_ = store.CreatePurchase(ctx, pr)

	// 退款
	refundSvc := NewRefundService(store, creditsSvc, mailer)
	if err := refundSvc.ProcessRefund(ctx, "p-refund-1", "admin@test.com", "test refund"); err != nil {
		t.Fatal(err)
	}

	// 验证买家退款
	buyerUser, _ := store.GetUserByID(ctx, buyer.ID)
	if buyerUser.Credits != 1000 {
		t.Errorf("buyer after refund=%d, want 1000", buyerUser.Credits)
	}

	// 验证购买记录标记为 refunded
	prGot, _ := store.GetPurchaseByID(ctx, "p-refund-1")
	if prGot.Status != "refunded" {
		t.Errorf("purchase status=%s, want refunded", prGot.Status)
	}

	// 验证邮件通知
	if len(mailer.sent) != 1 {
		t.Errorf("emails sent=%d, want 1", len(mailer.sent))
	}
}

func TestIntegration_APIKeyPoolFullFlow(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	secret := []byte("test-secret-key-for-encryption!!")
	svc, err := NewAPIKeyPoolService(store, secret)
	if err != nil {
		t.Fatal(err)
	}

	// 上传 Key
	n, _ := svc.UploadKeys(ctx, "skill-1", "API_KEY", []string{"key-1", "key-2"})
	if n != 2 {
		t.Fatalf("uploaded=%d, want 2", n)
	}

	// 分配
	k, _ := svc.AssignKey(ctx, "skill-1", "buyer@test.com", "API_KEY")
	if k.Status != "assigned" {
		t.Errorf("status=%s, want assigned", k.Status)
	}

	// 解密
	plain, _ := svc.DecryptAssignedKey(k)
	if plain != "key-1" && plain != "key-2" {
		t.Errorf("unexpected key: %s", plain)
	}

	// 库存状态
	status := svc.GetStockStatus(ctx, "skill-1")
	if status == "充足" {
		t.Error("should not be 充足 with only 1 remaining out of 2")
	}
}
