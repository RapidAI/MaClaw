package skillmarket

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// newTestStore 创建内存 SQLite 测试 Store。
func newTestStore(t *testing.T) *Store {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	store, err := NewStore(db, db)
	if err != nil {
		t.Fatal(err)
	}
	return store
}

// createTestUser 创建测试用户并返回。
func createTestUser(t *testing.T, store *Store, email string, credits int64) *SkillMarketUser {
	t.Helper()
	ctx := context.Background()
	now := time.Now()
	u := &SkillMarketUser{
		ID:               generateID(),
		Email:            email,
		Status:           "unverified",
		Credits:          credits,
		VoucherCount:     3,
		VoucherExpiresAt: now.Add(7 * 24 * time.Hour),
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if err := store.CreateUser(ctx, u); err != nil {
		t.Fatal(err)
	}
	return u
}

// ── Task 1.4: 数据模型单元测试 ──────────────────────────────────────────

func TestStoreMigrationAddsTenantPurchaseColumnsToExistingTable(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	_, err = db.Exec(`CREATE TABLE sm_purchase_records (
		id TEXT PRIMARY KEY,
		buyer_email TEXT NOT NULL,
		buyer_id TEXT NOT NULL,
		skill_id TEXT NOT NULL,
		purchased_version INTEGER NOT NULL DEFAULT 1,
		purchase_type TEXT NOT NULL DEFAULT 'purchase',
		amount_paid INTEGER NOT NULL DEFAULT 0,
		platform_fee INTEGER NOT NULL DEFAULT 0,
		seller_earning INTEGER NOT NULL DEFAULT 0,
		seller_id TEXT NOT NULL,
		key_status TEXT NOT NULL DEFAULT '',
		api_key_id TEXT NOT NULL DEFAULT '',
		status TEXT NOT NULL DEFAULT 'active',
		created_at TEXT NOT NULL
	);`)
	if err != nil {
		t.Fatalf("create legacy purchase table: %v", err)
	}
	if _, err := NewStore(db, db); err != nil {
		t.Fatalf("NewStore should migrate legacy purchase table: %v", err)
	}
	rows, err := db.Query(`PRAGMA table_info(sm_purchase_records)`)
	if err != nil {
		t.Fatalf("table_info: %v", err)
	}
	defer rows.Close()
	columns := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull int
		var defaultValue interface{}
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			t.Fatalf("scan table_info: %v", err)
		}
		columns[name] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("table_info rows: %v", err)
	}
	if !columns["hub_id"] || !columns["tenant_id"] {
		t.Fatalf("migration did not add tenant purchase columns: %#v", columns)
	}
}
func TestUserRepository_CRUD(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	// Create
	u := createTestUser(t, store, "test@example.com", 100)

	// Read by email
	got, err := store.GetUserByEmail(ctx, u.Email)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != u.ID || got.Credits != 100 {
		t.Errorf("GetUserByEmail: got id=%s credits=%d, want id=%s credits=100", got.ID, got.Credits, u.ID)
	}

	// Read by ID
	got2, err := store.GetUserByID(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got2.Email != u.Email {
		t.Errorf("GetUserByID: got email=%s, want %s", got2.Email, u.Email)
	}

	// Update status
	if err := store.UpdateUserStatus(ctx, u.ID, "verified", "email"); err != nil {
		t.Fatal(err)
	}
	got3, _ := store.GetUserByID(ctx, u.ID)
	if got3.Status != "verified" {
		t.Errorf("UpdateUserStatus: got %s, want verified", got3.Status)
	}

	// Update credits
	if err := store.UpdateUserCredits(ctx, u.ID, 200); err != nil {
		t.Fatal(err)
	}
	got4, _ := store.GetUserByID(ctx, u.ID)
	if got4.Credits != 200 {
		t.Errorf("UpdateUserCredits: got %d, want 200", got4.Credits)
	}

	// Not found
	_, err = store.GetUserByEmail(ctx, "nonexistent@example.com")
	if err == nil {
		t.Error("expected error for nonexistent user")
	}
}

func TestUserRepository_GetUserByEmailCaseInsensitive(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	u := createTestUser(t, store, "MixedCase@Example.com", 100)

	got, err := store.GetUserByEmail(ctx, "mixedcase@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != u.ID {
		t.Fatalf("GetUserByEmail returned id %s, want %s", got.ID, u.ID)
	}
}

func TestCreditsRepository_Transactions(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	u := createTestUser(t, store, "buyer@example.com", 500)

	// Create transactions
	for i := 0; i < 5; i++ {
		tx := &CreditsTransaction{
			ID:          generateID(),
			UserID:      u.ID,
			Type:        "purchase",
			Amount:      -100,
			Balance:     int64(500 - (i+1)*100),
			Description: "test purchase",
			CreatedAt:   time.Now(),
		}
		if err := store.CreateTransaction(ctx, tx); err != nil {
			t.Fatal(err)
		}
	}

	// List transactions
	txs, total, err := store.ListTransactionsByUser(ctx, u.ID, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if total != 5 {
		t.Errorf("total: got %d, want 5", total)
	}
	if len(txs) != 5 {
		t.Errorf("len(txs): got %d, want 5", len(txs))
	}

	// Pagination
	txs2, _, _ := store.ListTransactionsByUser(ctx, u.ID, 0, 2)
	if len(txs2) != 2 {
		t.Errorf("pagination: got %d, want 2", len(txs2))
	}
}

func TestSubmissionRepository_StatusFlow(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	now := time.Now()
	sub := &SkillSubmission{
		ID:        "sub-1",
		Email:     "dev@example.com",
		UserID:    "user-1",
		Status:    "pending",
		ZipPath:   "/tmp/test.zip",
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := store.CreateSubmission(ctx, sub); err != nil {
		t.Fatal(err)
	}

	// Read
	got, err := store.GetSubmissionByID(ctx, "sub-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "pending" {
		t.Errorf("status: got %s, want pending", got.Status)
	}

	// Update to processing
	if err := store.UpdateSubmissionStatus(ctx, "sub-1", "processing", "", ""); err != nil {
		t.Fatal(err)
	}
	got2, _ := store.GetSubmissionByID(ctx, "sub-1")
	if got2.Status != "processing" {
		t.Errorf("status: got %s, want processing", got2.Status)
	}

	// Update to success
	if err := store.UpdateSubmissionStatus(ctx, "sub-1", "success", "", "skill-123"); err != nil {
		t.Fatal(err)
	}
	got3, _ := store.GetSubmissionByID(ctx, "sub-1")
	if got3.Status != "success" || got3.SkillID != "skill-123" {
		t.Errorf("status=%s skillID=%s, want success/skill-123", got3.Status, got3.SkillID)
	}

	// Update to failed
	sub2 := &SkillSubmission{
		ID: "sub-2", Email: "dev@example.com", UserID: "user-1",
		Status: "pending", ZipPath: "/tmp/bad.zip", CreatedAt: now, UpdatedAt: now,
	}
	_ = store.CreateSubmission(ctx, sub2)
	_ = store.UpdateSubmissionStatus(ctx, "sub-2", "failed", "invalid yaml", "")
	got4, _ := store.GetSubmissionByID(ctx, "sub-2")
	if got4.Status != "failed" || got4.ErrorMsg != "invalid yaml" {
		t.Errorf("status=%s err=%s, want failed/invalid yaml", got4.Status, got4.ErrorMsg)
	}

	// Empty skillID must not wipe a previously linked skill (e.g. re-mark processing).
	_ = store.UpdateSubmissionStatus(ctx, "sub-1", "processing", "", "")
	got5, _ := store.GetSubmissionByID(ctx, "sub-1")
	if got5.SkillID != "skill-123" {
		t.Errorf("skill_id wiped on processing update: got %q, want skill-123", got5.SkillID)
	}
}

// ── Task 13.5: 评分数据模型单元测试 ─────────────────────────────────────

func TestRating_UpsertDedup(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	// 首次评分
	r := &Rating{SkillID: "skill-1", Email: "user@example.com", Score: 1}
	if err := store.UpsertRating(ctx, r); err != nil {
		t.Fatal(err)
	}

	// 同 email 再次评分（覆盖）
	r2 := &Rating{SkillID: "skill-1", Email: "user@example.com", Score: -1}
	if err := store.UpsertRating(ctx, r2); err != nil {
		t.Fatal(err)
	}

	// 验证去重：只有 1 个评分
	stats, err := store.GetRatingStats(ctx, "skill-1")
	if err != nil {
		t.Fatal(err)
	}
	if stats.UniqueRaters != 1 {
		t.Errorf("unique_raters: got %d, want 1", stats.UniqueRaters)
	}
	if stats.AverageScore != -1 {
		t.Errorf("avg_score: got %f, want -1", stats.AverageScore)
	}
}

func TestRatingStats_WithZeroScore(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	// 3 个评分：+2, 0, -1
	for _, r := range []Rating{
		{SkillID: "skill-2", Email: "a@test.com", Score: 2},
		{SkillID: "skill-2", Email: "b@test.com", Score: 0},
		{SkillID: "skill-2", Email: "c@test.com", Score: -1},
	} {
		if err := store.UpsertRating(ctx, &r); err != nil {
			t.Fatal(err)
		}
	}

	stats, _ := store.GetRatingStats(ctx, "skill-2")
	if stats.UniqueRaters != 3 {
		t.Errorf("unique_raters: got %d, want 3", stats.UniqueRaters)
	}
	// (2 + 0 + -1) / 3 ≈ 0.333
	expectedAvg := float64(1) / 3.0
	if diff := stats.AverageScore - expectedAvg; diff > 0.01 || diff < -0.01 {
		t.Errorf("avg_score: got %f, want ~%f", stats.AverageScore, expectedAvg)
	}
}

func TestAdminConfig_CRUD(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	// Set
	if err := store.SetConfig(ctx, "trial_duration_days", "14"); err != nil {
		t.Fatal(err)
	}

	// Get
	val, err := store.GetConfig(ctx, "trial_duration_days")
	if err != nil {
		t.Fatal(err)
	}
	if val != "14" {
		t.Errorf("got %s, want 14", val)
	}

	// Update (upsert)
	if err := store.SetConfig(ctx, "trial_duration_days", "30"); err != nil {
		t.Fatal(err)
	}
	val2, _ := store.GetConfig(ctx, "trial_duration_days")
	if val2 != "30" {
		t.Errorf("got %s, want 30", val2)
	}

	// GetWithDefault
	val3 := store.GetConfigWithDefault(ctx, "nonexistent", "default_val")
	if val3 != "default_val" {
		t.Errorf("got %s, want default_val", val3)
	}
}
