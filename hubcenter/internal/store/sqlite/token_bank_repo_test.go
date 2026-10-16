package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// newTokenBankTestProvider opens a provider configured the way hubcenter
// actually runs: WAL on, a busy timeout, and exactly one write connection
// (the hubcenter config default is still MaxWriteOpenConns 1).
//
// Connection-scoped pragmas are applied on every new connection by the
// provider connector. Raising the write pool no longer drops busy_timeout
// on the second connection; production stays at one writer until that
// change is chosen on its own.
func newTokenBankTestProvider(t *testing.T, dsn string) *Provider {
	t.Helper()
	provider, err := NewProvider(Config{
		DSN:               dsn,
		WAL:               true,
		BusyTimeoutMS:     5000,
		MaxWriteOpenConns: 1,
		MaxWriteIdleConns: 1,
		MaxReadOpenConns:  2,
		MaxReadIdleConns:  1,
	})
	if err != nil {
		t.Fatalf("NewProvider() error = %v", err)
	}
	t.Cleanup(func() { _ = provider.Close() })
	return provider
}

func newTokenBankTestRepo(t *testing.T, provider *Provider) *TokenBankRepo {
	t.Helper()
	if provider == nil {
		return nil
	}
	if err := RunMigrations(provider.Write); err != nil {
		t.Fatalf("RunMigrations() error = %v", err)
	}
	if err := EnsureLLMTables(provider.Write); err != nil {
		t.Fatalf("EnsureLLMTables() error = %v", err)
	}
	return NewTokenBankRepo(provider)
}

// TestTokenBankBucketsAreKnown guards the two-place definition of a bucket.
//
// tokenBankBucketColumn (which cache column a bucket bumps) and
// scanTokenBankBalance (how it sums into a balance) are separate switches that
// have to agree. Adding a bucket to only one of them is not a compile error and
// not a runtime error — it is a balance that silently omits that money, which
// is exactly the kind of bug that reaches production and stays there.
//
// The bucket list is derived from TokenBankBalance's fields rather than written
// out here: a bucket that has nowhere to store its sum is not a bucket, so the
// struct is the one list that cannot be forgotten. A hand-written list in this
// test would have been a third place to update, and would not have caught the
// mistake it exists to catch.
func TestTokenBankBucketsAreKnown(t *testing.T) {
	provider := newTokenBankTestProvider(t, filepath.Join(t.TempDir(), "tbk-buckets.db"))
	repo := newTokenBankTestRepo(t, provider)
	ctx := context.Background()

	balanceType := reflect.TypeOf(TokenBankBalance{})
	buckets := make([]string, 0, balanceType.NumField())
	fieldIndex := map[string]int{}
	for i := 0; i < balanceType.NumField(); i++ {
		name := balanceType.Field(i).Name
		if !strings.HasSuffix(name, "Micro") {
			continue
		}
		bucket := strings.ToLower(strings.TrimSuffix(name, "Micro"))
		buckets = append(buckets, bucket)
		fieldIndex[bucket] = i
	}
	if len(buckets) == 0 {
		t.Fatal("no buckets derived from TokenBankBalance; the field naming convention changed")
	}

	// 1. Every bucket has a cache column.
	for _, bucket := range buckets {
		if _, ok := tokenBankBucketColumn(bucket); !ok {
			t.Errorf("bucket %q has a balance field but no cache column: tokenBankBucketColumn is missing it", bucket)
		}
	}

	// 2. Every bucket lands in the balance that scanTokenBankBalance returns.
	const amount = 7_000_000
	for i, bucket := range buckets {
		applied, err := repo.AppendLedger(ctx, TokenBankLedgerEntry{
			ID:          fmt.Sprintf("bucket-probe-%s", bucket),
			UserID:      "user-buckets",
			Bucket:      bucket,
			AmountMicro: amount,
			BizKey:      fmt.Sprintf("bucket-probe:%s:%d", bucket, i),
			RefType:     "probe",
			CreatedAt:   time.Now().UTC(),
		})
		if err != nil {
			t.Fatalf("AppendLedger(%q) error = %v", bucket, err)
		}
		if !applied {
			t.Fatalf("AppendLedger(%q) applied = false, want true", bucket)
		}
	}
	balance, err := repo.Balance(ctx, "user-buckets")
	if err != nil {
		t.Fatalf("Balance() error = %v", err)
	}
	balanceValue := reflect.ValueOf(balance)
	for _, bucket := range buckets {
		if got := balanceValue.Field(fieldIndex[bucket]).Int(); got != amount {
			t.Errorf("bucket %q summed to %d, want %d: scanTokenBankBalance is missing it", bucket, got, amount)
		}
	}
}

// TestCountHubsForUserMissingTableDegradesToOne pins the one case in which the
// hub count may be unknown on purpose: a deployment that never initialised
// SkillMarket has no hub_user_links at all.
//
// It exists because that tolerance was implemented as "swallow every error",
// which also swallowed real failures — and N is the divisor of
// AutoWithdrawLimitMicro, so downgrading it from 3 to 1 lets one automatic
// withdrawal take the whole balance. This test is what stops a future
// "simplify" from taking the error handling back out.
func TestCountHubsForUserMissingTableDegradesToOne(t *testing.T) {
	// A provider with no migrations run: neither table exists.
	provider := newTokenBankTestProvider(t, filepath.Join(t.TempDir(), "tbk-nohub.db"))
	t.Cleanup(func() { _ = provider.Close() })
	ctx := context.Background()

	n, err := countHubsForUser(ctx, provider.Write.QueryRowContext, "user-1")
	if err != nil {
		t.Fatalf("countHubsForUser() error = %v, want nil (a missing table degrades to 1)", err)
	}
	if n != 1 {
		t.Fatalf("countHubsForUser() = %d, want 1", n)
	}
}

// TestCountHubsForUserPropagatesRealErrors is the other half of the pair, and
// the reason the tolerance was narrowed.
//
// N is the divisor of AutoWithdrawLimitMicro, so a failure that quietly
// degrades to 1 does not merely lose the hub count: it tells the withdrawal
// path that the user owns one hub and lets a single automatic pull take the
// whole balance instead of 1/N of it (E6). The version this guards against was
// "swallow every error and return 1", which is indistinguishable from success
// at every call site.
func TestCountHubsForUserPropagatesRealErrors(t *testing.T) {
	// A provider with no migrations run, so nothing here depends on the schema.
	provider := newTokenBankTestProvider(t, filepath.Join(t.TempDir(), "tbk-huberr.db"))
	ctx := context.Background()

	// A query that fails for a reason other than a missing table. The row comes
	// from a real connection so the error is a real driver error rather than a
	// hand-made one.
	broken := func(ctx context.Context, _ string, _ ...any) *sql.Row {
		return provider.Write.QueryRowContext(ctx, "SELECT 1 FROM (")
	}
	n, err := countHubsForUser(ctx, broken, "user-1")
	if err == nil {
		t.Fatalf("countHubsForUser() = %d, nil; want an error (a real failure must not degrade to 1)", n)
	}
	if n != 0 {
		t.Fatalf("countHubsForUser() = %d, want 0 alongside the error", n)
	}
}

// TestFindDriftedAccountsDetectsAndRebuildRepairs pins the pair that makes the
// cache recoverable: the detector has to find a drifted account, and the repair
// has to clear it.
//
// Either half alone is half a feature. RebuildAccount already existed and was
// tested, but nothing called it, so the drift it repairs was undetectable and
// permanent. FindDriftedAccounts is what makes the repair schedulable — this
// test is what makes both of them load-bearing.
func TestFindDriftedAccountsDetectsAndRebuildRepairs(t *testing.T) {
	provider := newTokenBankTestProvider(t, filepath.Join(t.TempDir(), "tbk-drift.db"))
	repo := newTokenBankTestRepo(t, provider)
	ctx := context.Background()

	const userID = "user-drift"
	const amount = 4_000_000
	applied, err := repo.AppendLedger(ctx, TokenBankLedgerEntry{
		ID:          "drift-earn-1",
		UserID:      userID,
		Bucket:      TokenBankBucketEarned,
		AmountMicro: amount,
		BizKey:      "drift:earn:1",
		RefType:     "probe",
		CreatedAt:   time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("AppendLedger() error = %v", err)
	}
	if !applied {
		t.Fatal("AppendLedger() applied = false, want true")
	}

	// A freshly written account agrees with its ledger.
	if users, err := repo.FindDriftedAccounts(ctx, 10); err != nil {
		t.Fatalf("FindDriftedAccounts() error = %v", err)
	} else if len(users) != 0 {
		t.Fatalf("FindDriftedAccounts() = %v, want empty (cache agrees with the ledger)", users)
	}

	// Drive the cache away from the ledger directly. This is what a lost bump
	// looks like: the ledger row is there, the cache never heard about it.
	if _, err := provider.Write.ExecContext(ctx,
		`UPDATE token_bank_accounts SET earned_micro = 0 WHERE user_id = ?`, userID); err != nil {
		t.Fatalf("corrupt cache: %v", err)
	}

	users, err := repo.FindDriftedAccounts(ctx, 10)
	if err != nil {
		t.Fatalf("FindDriftedAccounts() error = %v", err)
	}
	if len(users) != 1 || users[0] != userID {
		t.Fatalf("FindDriftedAccounts() = %v, want [%s]", users, userID)
	}

	if err := repo.RebuildAccount(ctx, userID); err != nil {
		t.Fatalf("RebuildAccount() error = %v", err)
	}
	if users, err := repo.FindDriftedAccounts(ctx, 10); err != nil {
		t.Fatalf("FindDriftedAccounts() after rebuild error = %v", err)
	} else if len(users) != 0 {
		t.Fatalf("FindDriftedAccounts() after rebuild = %v, want empty", users)
	}

	balance, err := repo.Balance(ctx, userID)
	if err != nil {
		t.Fatalf("Balance() error = %v", err)
	}
	if balance.EarnedMicro != amount {
		t.Fatalf("Balance().EarnedMicro = %d, want %d", balance.EarnedMicro, amount)
	}
}

// TestIsMissingRelationErrorIsNarrow keeps the "missing table" test from
// broadening. It is a string match, so a sloppy version of it would classify a
// lock timeout or a syntax error as "the table is not there" and downgrade a
// real failure into a silent default.
func TestIsMissingRelationErrorIsNarrow(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil", err: nil, want: false},
		{name: "missing table", err: errors.New("no such table: hub_user_links"), want: true},
		{name: "lock timeout", err: errors.New("database is locked"), want: false},
		{name: "syntax", err: errors.New("near \"FROM\": syntax error"), want: false},
		{name: "constraint", err: errors.New("UNIQUE constraint failed: token_bank_ledger.biz_key"), want: false},
	}
	for _, tc := range cases {
		if got := isMissingRelationError(tc.err); got != tc.want {
			t.Errorf("isMissingRelationError(%s) = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestTokenBankAppendLedgerIsIdempotent(t *testing.T) {
	provider := newTokenBankTestProvider(t, filepath.Join(t.TempDir(), "tbk-idem.db"))
	repo := newTokenBankTestRepo(t, provider)
	ctx := context.Background()

	entry := TokenBankLedgerEntry{
		ID:          "usage:req-1:share-1",
		UserID:      "user-1",
		Bucket:      TokenBankBucketEarned,
		AmountMicro: 1_500_000,
		BizKey:      "usage:req-1:share-1",
		RefType:     "usage",
		RefID:       "req-1",
		CreatedAt:   time.Now().UTC(),
	}
	applied, err := repo.AppendLedger(ctx, entry)
	if err != nil {
		t.Fatalf("AppendLedger() error = %v", err)
	}
	if !applied {
		t.Fatalf("first AppendLedger applied = false, want true")
	}

	// The same entry replayed by HA sync, or by a settlement that ran twice,
	// must not credit the user a second time.
	applied, err = repo.AppendLedger(ctx, entry)
	if err != nil {
		t.Fatalf("AppendLedger(replay) error = %v", err)
	}
	if applied {
		t.Fatalf("replayed AppendLedger applied = true, want false")
	}

	balance, err := repo.Balance(ctx, "user-1")
	if err != nil {
		t.Fatalf("Balance() error = %v", err)
	}
	if balance.EarnedMicro != 1_500_000 {
		t.Fatalf("earned_micro = %d, want 1500000", balance.EarnedMicro)
	}
	if got := balance.AvailableMicro(); got != 1_500_000 {
		t.Fatalf("AvailableMicro() = %d, want 1500000", got)
	}
}

// settlementID is the deterministic ledger id convention from the design
// (§14.3): usage:<request_id>:<share_id>. Three nodes minting a random id for
// the same settlement would triplicate the credit; this one cannot.
func settlementID(requestID string) string {
	return "usage:" + requestID + ":share-1"
}

func settlementEntry(requestID string, amount int64) TokenBankLedgerEntry {
	return TokenBankLedgerEntry{
		ID:          settlementID(requestID),
		UserID:      "user-concurrent",
		Bucket:      TokenBankBucketEarned,
		AmountMicro: amount,
		BizKey:      settlementID(requestID),
		RefType:     "usage",
		RefID:       requestID,
		CreatedAt:   time.Now().UTC(),
	}
}

// TestTokenBankConcurrentSettlementKeepsExactSum is the test the design asks
// for: settlements arriving for the same user at the same time must produce a
// balance equal to the exact arithmetic sum, not "close enough". A balance
// column read-modify-written by two writers loses one of the two writes; the
// append-only ledger cannot, so the assertion is integer equality.
func TestTokenBankConcurrentSettlementKeepsExactSum(t *testing.T) {
	provider := newTokenBankTestProvider(t, filepath.Join(t.TempDir(), "tbk-concurrent.db"))
	repo := newTokenBankTestRepo(t, provider)

	ctx := context.Background()
	const perWriter = 100
	const writers = 2
	const amount = 1_234_567
	var appliedCount int64

	var wg sync.WaitGroup
	for w := 0; w < writers; w++ {
		w := w
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < perWriter; i++ {
				applied, err := repo.AppendLedger(ctx, settlementEntry(fmt.Sprintf("req-%d-%d", w, i), amount))
				if err != nil {
					t.Errorf("AppendLedger(writer %d, i %d) error = %v", w, i, err)
					return
				}
				if applied {
					atomic.AddInt64(&appliedCount, 1)
				}
			}
		}()
	}
	wg.Wait()

	want := int64(writers * perWriter * amount)
	if appliedCount != int64(writers*perWriter) {
		t.Fatalf("applied = %d, want %d", appliedCount, writers*perWriter)
	}

	balance, err := repo.Balance(ctx, "user-concurrent")
	if err != nil {
		t.Fatalf("Balance() error = %v", err)
	}
	if balance.EarnedMicro != want {
		t.Fatalf("earned_micro = %d, want exact %d (lost %d microcredits)",
			balance.EarnedMicro, want, want-balance.EarnedMicro)
	}

	// The cache must agree too. It is bumped with an in-transaction SQL
	// increment, never a read-modify-write in Go, so it cannot drift under
	// concurrency either.
	var cached int64
	if err := provider.Read.QueryRowContext(ctx,
		`SELECT earned_micro FROM token_bank_accounts WHERE user_id = ?`, "user-concurrent").
		Scan(&cached); err != nil {
		t.Fatalf("read cached earned_micro: %v", err)
	}
	if cached != want {
		t.Fatalf("cached earned_micro = %d, want exact %d", cached, want)
	}
}

// TestTokenBankReplicatedRowsNeverDoubleCredit is the other half of E1. A hub
// that loses its registry, or an HA peer that receives the same settlement row
// twice, replays identical entries; the receiving side applies them with
// INSERT OR IGNORE keyed by the deterministic id, so the balance must not move.
func TestTokenBankReplicatedRowsNeverDoubleCredit(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "tbk-replay.db")
	providerA := newTokenBankTestProvider(t, dsn)
	repoA := newTokenBankTestRepo(t, providerA)

	ctx := context.Background()
	const rows = 50
	const amount = 987_654
	for i := 0; i < rows; i++ {
		applied, err := repoA.AppendLedger(ctx, settlementEntry(fmt.Sprintf("req-%d", i), amount))
		if err != nil || !applied {
			t.Fatalf("AppendLedger(%d) applied=%v error=%v, want applied", i, applied, err)
		}
	}
	want := int64(rows * amount)

	// A second node — modelled as a second handle on the store, which is what
	// ha_sync replay looks like from the ledger's point of view — applies the
	// very same rows again.
	providerB := newTokenBankTestProvider(t, dsn)
	repoB := NewTokenBankRepo(providerB)
	for i := 0; i < rows; i++ {
		applied, err := repoB.AppendLedger(ctx, settlementEntry(fmt.Sprintf("req-%d", i), amount))
		if err != nil {
			t.Fatalf("replay AppendLedger(%d) error = %v", i, err)
		}
		if applied {
			t.Fatalf("replay AppendLedger(%d) applied = true, want false", i)
		}
	}

	balance, err := repoA.Balance(ctx, "user-concurrent")
	if err != nil {
		t.Fatalf("Balance() error = %v", err)
	}
	if balance.EarnedMicro != want {
		t.Fatalf("earned_micro after replay = %d, want exact %d", balance.EarnedMicro, want)
	}
	var cached int64
	if err := providerA.Read.QueryRowContext(ctx,
		`SELECT earned_micro FROM token_bank_accounts WHERE user_id = ?`, "user-concurrent").
		Scan(&cached); err != nil {
		t.Fatalf("read cached earned_micro: %v", err)
	}
	if cached != want {
		t.Fatalf("cached earned_micro after replay = %d, want exact %d", cached, want)
	}
}

func TestTokenBankAvailableMicroSubtractsFrozen(t *testing.T) {
	provider := newTokenBankTestProvider(t, filepath.Join(t.TempDir(), "tbk-frozen.db"))
	repo := newTokenBankTestRepo(t, provider)
	ctx := context.Background()

	appendEntry := func(id, bucket string, amount int64) {
		t.Helper()
		if _, err := repo.AppendLedger(ctx, TokenBankLedgerEntry{
			ID:          id,
			UserID:      "user-frozen",
			Bucket:      bucket,
			AmountMicro: amount,
			BizKey:      id,
			CreatedAt:   time.Now().UTC(),
		}); err != nil {
			t.Fatalf("AppendLedger(%s) error = %v", id, err)
		}
	}

	appendEntry("usage:req-1:share-1", TokenBankBucketEarned, 10_000_000)
	appendEntry("freeze:link-1", TokenBankBucketFrozen, 4_000_000)
	appendEntry("withdraw:req-w1", TokenBankBucketWithdrawn, 1_000_000)

	available, err := repo.AvailableMicro(ctx, "user-frozen")
	if err != nil {
		t.Fatalf("AvailableMicro() error = %v", err)
	}
	// 10 earned - 4 frozen (a link issued but not yet settled) - 1 withdrawn.
	if available != 5_000_000 {
		t.Fatalf("AvailableMicro() = %d, want 5000000; omitting frozen would let the "+
			"same credits be withdrawn twice", available)
	}
}

func TestTokenBankRebuildAccountRecoversDriftedCache(t *testing.T) {
	provider := newTokenBankTestProvider(t, filepath.Join(t.TempDir(), "tbk-rebuild.db"))
	repo := newTokenBankTestRepo(t, provider)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		id := fmt.Sprintf("usage:req-%d:share-1", i)
		if _, err := repo.AppendLedger(ctx, TokenBankLedgerEntry{
			ID:          id,
			UserID:      "user-rebuild",
			Bucket:      TokenBankBucketEarned,
			AmountMicro: 2_000_000,
			BizKey:      id,
			CreatedAt:   time.Now().UTC(),
		}); err != nil {
			t.Fatalf("AppendLedger(%s) error = %v", id, err)
		}
	}

	// Simulate drift: someone hand-edited the cache, or a bump was lost.
	if _, err := provider.Write.ExecContext(ctx,
		`UPDATE token_bank_accounts SET earned_micro = 42 WHERE user_id = ?`, "user-rebuild"); err != nil {
		t.Fatalf("corrupt cache: %v", err)
	}
	if err := repo.RebuildAccount(ctx, "user-rebuild"); err != nil {
		t.Fatalf("RebuildAccount() error = %v", err)
	}

	var cached int64
	if err := provider.Read.QueryRowContext(ctx,
		`SELECT earned_micro FROM token_bank_accounts WHERE user_id = ?`, "user-rebuild").
		Scan(&cached); err != nil {
		t.Fatalf("read cached earned_micro: %v", err)
	}
	if cached != 6_000_000 {
		t.Fatalf("rebuilt earned_micro = %d, want 6000000", cached)
	}
}

func TestTokenBankAppendLedgerRejectsUnknownBucket(t *testing.T) {
	provider := newTokenBankTestProvider(t, filepath.Join(t.TempDir(), "tbk-bucket.db"))
	repo := newTokenBankTestRepo(t, provider)

	if _, err := repo.AppendLedger(context.Background(), TokenBankLedgerEntry{
		ID: "usage:req-1:share-1", UserID: "user-1", Bucket: "magic", AmountMicro: 1,
	}); err == nil {
		t.Fatalf("AppendLedger(unknown bucket) error = nil, want error")
	}
	if _, err := repo.AppendLedger(context.Background(), TokenBankLedgerEntry{
		ID: "usage:req-1:share-1", Bucket: TokenBankBucketEarned, AmountMicro: 1,
	}); err == nil {
		t.Fatalf("AppendLedger(missing user) error = nil, want error")
	}
}
