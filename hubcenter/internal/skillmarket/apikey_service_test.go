package skillmarket

import (
	"context"
	"testing"
)

func newTestAPIKeyPool(t *testing.T) (*APIKeyPoolService, *Store) {
	t.Helper()
	store := newTestStore(t)
	secret := []byte("test-secret-key-for-encryption!!")
	svc, err := NewAPIKeyPoolService(store, secret)
	if err != nil {
		t.Fatal(err)
	}
	return svc, store
}

// ── Task 40.10: API Key 池属性测试 ──────────────────────────────────────

func TestAPIKeyPool_AssignUniqueness(t *testing.T) {
	svc, _ := newTestAPIKeyPool(t)
	ctx := context.Background()

	// 上传 3 个 Key
	keys := []string{"key-aaa", "key-bbb", "key-ccc"}
	n, err := svc.UploadKeys(ctx, "skill-1", "API_KEY", keys)
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Fatalf("uploaded %d, want 3", n)
	}

	// 分配给 3 个不同买家
	assigned := make(map[string]string) // keyID -> buyerEmail
	buyers := []string{"buyer1@test.com", "buyer2@test.com", "buyer3@test.com"}
	for _, buyer := range buyers {
		k, err := svc.AssignKey(ctx, "skill-1", buyer, "API_KEY")
		if err != nil {
			t.Fatalf("assign to %s failed: %v", buyer, err)
		}
		if prev, exists := assigned[k.ID]; exists {
			t.Errorf("key %s assigned to both %s and %s", k.ID, prev, buyer)
		}
		assigned[k.ID] = buyer
	}

	// 第 4 个买家应该分配失败（池耗尽）
	_, err = svc.AssignKey(ctx, "skill-1", "buyer4@test.com", "API_KEY")
	if err == nil {
		t.Error("expected error when pool exhausted")
	}
}

func TestAPIKeyPool_PendingOrderFIFO(t *testing.T) {
	svc, _ := newTestAPIKeyPool(t)
	ctx := context.Background()

	// 创建 3 个 pending 订单（按时间顺序）
	emails := []string{"first@test.com", "second@test.com", "third@test.com"}
	for _, email := range emails {
		err := svc.CreatePendingOrder(ctx, generateID(), "skill-1", email, "API_KEY")
		if err != nil {
			t.Fatal(err)
		}
	}

	// 上传 2 个 Key 并 fulfill
	_, _ = svc.UploadKeys(ctx, "skill-1", "API_KEY", []string{"key-1", "key-2"})
	fulfilled, err := svc.FulfillPendingOrders(ctx, "skill-1")
	if err != nil {
		t.Fatal(err)
	}
	if fulfilled != 2 {
		t.Errorf("fulfilled=%d, want 2", fulfilled)
	}

	// 验证 third 仍然 pending
	pending := svc.GetPendingOrderCount(ctx, "skill-1")
	if pending != 1 {
		t.Errorf("pending=%d, want 1", pending)
	}
}

// ── Task 40.11: API Key 池单元测试 ──────────────────────────────────────

func TestAPIKeyPool_NormalAssignment(t *testing.T) {
	svc, _ := newTestAPIKeyPool(t)
	ctx := context.Background()

	_, _ = svc.UploadKeys(ctx, "skill-1", "API_KEY", []string{"my-secret-key"})

	k, err := svc.AssignKey(ctx, "skill-1", "buyer@test.com", "API_KEY")
	if err != nil {
		t.Fatal(err)
	}
	if k.Status != "assigned" {
		t.Errorf("status=%s, want assigned", k.Status)
	}
	if k.BuyerEmail != "buyer@test.com" {
		t.Errorf("buyerEmail=%s, want buyer@test.com", k.BuyerEmail)
	}

	// 解密验证
	plain, err := svc.DecryptAssignedKey(k)
	if err != nil {
		t.Fatal(err)
	}
	if plain != "my-secret-key" {
		t.Errorf("decrypted=%s, want my-secret-key", plain)
	}
}

func TestAPIKeyPool_ExhaustedCreatesPendingOrder(t *testing.T) {
	svc, _ := newTestAPIKeyPool(t)
	ctx := context.Background()

	// 不上传任何 Key，直接尝试分配
	_, err := svc.AssignKey(ctx, "skill-1", "buyer@test.com", "API_KEY")
	if err == nil {
		t.Error("expected error when no keys available")
	}

	// 创建 pending 订单
	err = svc.CreatePendingOrder(ctx, "purchase-1", "skill-1", "buyer@test.com", "API_KEY")
	if err != nil {
		t.Fatal(err)
	}
	pending := svc.GetPendingOrderCount(ctx, "skill-1")
	if pending != 1 {
		t.Errorf("pending=%d, want 1", pending)
	}
}

func TestAPIKeyPool_FulfillAfterRestock(t *testing.T) {
	svc, _ := newTestAPIKeyPool(t)
	ctx := context.Background()

	// 创建 pending 订单
	_ = svc.CreatePendingOrder(ctx, "purchase-1", "skill-1", "buyer@test.com", "API_KEY")

	// 补货
	_, _ = svc.UploadKeys(ctx, "skill-1", "API_KEY", []string{"restocked-key"})

	// fulfill
	fulfilled, _ := svc.FulfillPendingOrders(ctx, "skill-1")
	if fulfilled != 1 {
		t.Errorf("fulfilled=%d, want 1", fulfilled)
	}

	// pending 应为 0
	pending := svc.GetPendingOrderCount(ctx, "skill-1")
	if pending != 0 {
		t.Errorf("pending=%d, want 0", pending)
	}
}

func TestAPIKeyPool_StockStatus(t *testing.T) {
	svc, _ := newTestAPIKeyPool(t)
	ctx := context.Background()

	// 无 Key → 缺货
	status := svc.GetStockStatus(ctx, "skill-1")
	if status != "缺货" {
		t.Errorf("status=%s, want 缺货", status)
	}

	// 上传 10 个 Key → 充足
	keys := make([]string, 10)
	for i := range keys {
		keys[i] = generateID()
	}
	_, _ = svc.UploadKeys(ctx, "skill-1", "API_KEY", keys)
	status = svc.GetStockStatus(ctx, "skill-1")
	if status != "充足" {
		t.Errorf("status=%s, want 充足", status)
	}

	// 分配 8 个 → 紧张 (2/10 = 20%, 但 available=2 < 5)
	for i := 0; i < 8; i++ {
		_, _ = svc.AssignKey(ctx, "skill-1", generateID()+"@test.com", "API_KEY")
	}
	status = svc.GetStockStatus(ctx, "skill-1")
	if status != "紧张" {
		t.Errorf("status=%s, want 紧张", status)
	}

	// 分配剩余 2 个 → 缺货
	for i := 0; i < 2; i++ {
		_, _ = svc.AssignKey(ctx, "skill-1", generateID()+"@test.com", "API_KEY")
	}
	status = svc.GetStockStatus(ctx, "skill-1")
	if status != "缺货" {
		t.Errorf("status=%s, want 缺货", status)
	}
}

func TestAPIKeyPool_MultiplePurchasesSameBuyer(t *testing.T) {
	svc, _ := newTestAPIKeyPool(t)
	ctx := context.Background()

	_, _ = svc.UploadKeys(ctx, "skill-1", "API_KEY", []string{"key-1", "key-2", "key-3"})

	// 同一买家购买多次应获取不同 Key
	keyIDs := make(map[string]bool)
	for i := 0; i < 3; i++ {
		k, err := svc.AssignKey(ctx, "skill-1", "same-buyer@test.com", "API_KEY")
		if err != nil {
			t.Fatal(err)
		}
		if keyIDs[k.ID] {
			t.Errorf("duplicate key assigned: %s", k.ID)
		}
		keyIDs[k.ID] = true
	}
}

func TestAPIKeyPool_EncryptDecryptRoundTrip(t *testing.T) {
	svc, _ := newTestAPIKeyPool(t)

	testKeys := []string{"sk-abc123", "", "a-very-long-api-key-with-special-chars!@#$%"}
	for _, key := range testKeys {
		enc, err := svc.encryptKey(key)
		if err != nil {
			t.Fatalf("encrypt %q: %v", key, err)
		}
		dec, err := svc.decryptKey(enc)
		if err != nil {
			t.Fatalf("decrypt %q: %v", key, err)
		}
		if dec != key {
			t.Errorf("round-trip failed: got %q, want %q", dec, key)
		}
	}
}
