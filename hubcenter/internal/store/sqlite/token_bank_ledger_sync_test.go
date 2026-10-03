package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestTokenBankLedgerBatchIsIdempotent(t *testing.T) {
	provider := newTokenBankTestProvider(t, filepath.Join(t.TempDir(), "tbk-batch.db"))
	repo := newTokenBankTestRepo(t, provider)
	ctx := context.Background()

	entries := []TokenBankLedgerEntry{
		{ID: "usage:req-1:share-1", UserID: "u1", Bucket: TokenBankBucketEarned, AmountMicro: 1_000_000, BizKey: "usage:req-1:share-1", CreatedAt: time.Now().UTC()},
		{ID: "usage:req-2:share-1", UserID: "u1", Bucket: TokenBankBucketEarned, AmountMicro: 2_000_000, BizKey: "usage:req-2:share-1", CreatedAt: time.Now().UTC()},
		{ID: "usage:req-3:share-2", UserID: "u2", Bucket: TokenBankBucketEarned, AmountMicro: 3_000_000, BizKey: "usage:req-3:share-2", CreatedAt: time.Now().UTC()},
	}

	applied, err := repo.InsertLedgerBatch(ctx, "batch-1", "node-a", entries)
	if err != nil {
		t.Fatalf("InsertLedgerBatch() error = %v", err)
	}
	if !applied {
		t.Fatalf("InsertLedgerBatch() applied = false, want true")
	}
	for _, user := range []string{"u1", "u2"} {
		balance, err := repo.Balance(ctx, user)
		if err != nil {
			t.Fatalf("Balance(%s) error = %v", user, err)
		}
		var want int64
		if user == "u1" {
			want = 3_000_000
		} else {
			want = 3_000_000
		}
		if balance.EarnedMicro != want {
			t.Fatalf("Balance(%s).EarnedMicro = %d, want %d", user, balance.EarnedMicro, want)
		}
	}

	// Same batch, redelivered: inert.
	applied, err = repo.InsertLedgerBatch(ctx, "batch-1", "node-a", entries)
	if err != nil {
		t.Fatalf("InsertLedgerBatch(replay) error = %v", err)
	}
	if applied {
		t.Fatalf("InsertLedgerBatch(replay) applied = true, want false")
	}

	// Same rows under a *different* batch id — the seed path after a node
	// restart does exactly this. Row-level INSERT OR IGNORE must still skip
	// them, and the cache must not be bumped a second time.
	applied, err = repo.InsertLedgerBatch(ctx, "batch-1-reseed", "node-a", entries)
	if err != nil {
		t.Fatalf("InsertLedgerBatch(reseed) error = %v", err)
	}
	if !applied {
		t.Fatalf("InsertLedgerBatch(reseed) applied = false, want true (the batch is new)")
	}
	balance, err := repo.Balance(ctx, "u1")
	if err != nil {
		t.Fatalf("Balance(u1) error = %v", err)
	}
	if balance.EarnedMicro != 3_000_000 {
		t.Fatalf("Balance(u1).EarnedMicro after reseed = %d, want 3000000 (rows must not re-apply)", balance.EarnedMicro)
	}
	var cached int64
	if err := provider.Read.QueryRowContext(ctx,
		`SELECT earned_micro FROM token_bank_accounts WHERE user_id = 'u1'`).Scan(&cached); err != nil {
		t.Fatalf("read cache: %v", err)
	}
	if cached != 3_000_000 {
		t.Fatalf("cache after reseed = %d, want 3000000 (must not double-bump)", cached)
	}
}

func TestTokenBankLedgerBatchRequiresOriginNode(t *testing.T) {
	provider := newTokenBankTestProvider(t, filepath.Join(t.TempDir(), "tbk-batch-origin.db"))
	repo := newTokenBankTestRepo(t, provider)

	entries := []TokenBankLedgerEntry{
		{ID: "usage:req-1:share-1", UserID: "u1", Bucket: TokenBankBucketEarned, AmountMicro: 1, CreatedAt: time.Now().UTC()},
	}
	// An empty origin would make these rows look locally-created and get
	// republished back to the cluster by this node's own seed pass.
	if _, err := repo.InsertLedgerBatch(context.Background(), "batch-1", "", entries); err == nil {
		t.Fatalf("InsertLedgerBatch(no origin) error = nil, want error")
	}
}

func TestTokenBankLedgerBatchRejectsUnknownBucket(t *testing.T) {
	provider := newTokenBankTestProvider(t, filepath.Join(t.TempDir(), "tbk-batch-bucket.db"))
	repo := newTokenBankTestRepo(t, provider)

	entries := []TokenBankLedgerEntry{
		{ID: "bogus", UserID: "u1", Bucket: "not-a-bucket", AmountMicro: 1, CreatedAt: time.Now().UTC()},
	}
	if _, err := repo.InsertLedgerBatch(context.Background(), "batch-1", "node-a", entries); err == nil {
		t.Fatalf("InsertLedgerBatch(unknown bucket) error = nil, want error")
	}
	// The failed batch must have rolled back: no partial ledger and no batch row.
	var ledgerRows int
	if err := provider.Read.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM token_bank_ledger`).Scan(&ledgerRows); err != nil {
		t.Fatalf("count ledger: %v", err)
	}
	if ledgerRows != 0 {
		t.Fatalf("ledger rows = %d after a rejected batch, want 0", ledgerRows)
	}
	var batches int
	if err := provider.Read.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM token_bank_ledger_batches`).Scan(&batches); err != nil {
		t.Fatalf("count batches: %v", err)
	}
	if batches != 0 {
		t.Fatalf("batch rows = %d after a rejected batch, want 0", batches)
	}
}

func TestTokenBankListLedgerAfterPaginates(t *testing.T) {
	provider := newTokenBankTestProvider(t, filepath.Join(t.TempDir(), "tbk-ledger-page.db"))
	repo := newTokenBankTestRepo(t, provider)
	ctx := context.Background()

	base := time.Now().UTC().Truncate(time.Second).Add(-time.Hour)
	for i := 0; i < 5; i++ {
		id := "usage:req-" + string(rune('a'+i)) + ":share-1"
		if _, err := repo.AppendLedger(ctx, TokenBankLedgerEntry{
			ID: id, UserID: "u1", Bucket: TokenBankBucketEarned, AmountMicro: 1_000_000,
			BizKey: id, CreatedAt: base.Add(time.Duration(i) * time.Second),
		}); err != nil {
			t.Fatalf("AppendLedger(%d) error = %v", i, err)
		}
	}

	page1, err := repo.ListLedgerAfter(ctx, base.Add(-time.Second), "", 3)
	if err != nil {
		t.Fatalf("ListLedgerAfter() error = %v", err)
	}
	if len(page1) != 3 {
		t.Fatalf("first page = %d rows, want 3", len(page1))
	}
	if page1[0].ID != "usage:req-a:share-1" || page1[2].ID != "usage:req-c:share-1" {
		t.Fatalf("first page order = %s..%s, want oldest first", page1[0].ID, page1[2].ID)
	}
	page2, err := repo.ListLedgerAfter(ctx, page1[2].CreatedAt, page1[2].ID, 3)
	if err != nil {
		t.Fatalf("ListLedgerAfter(page2) error = %v", err)
	}
	if len(page2) != 2 {
		t.Fatalf("second page = %d rows, want 2", len(page2))
	}
}

// created_at is stored at second precision, so a busy second can hold more
// rows than one page. The cursor must carry the id, or the rows of that second
// beyond the page boundary are skipped forever and the peer's ledger diverges.
func TestTokenBankListLedgerAfterPaginatesWithinOneSecond(t *testing.T) {
	provider := newTokenBankTestProvider(t, filepath.Join(t.TempDir(), "tbk-ledger-tie.db"))
	repo := newTokenBankTestRepo(t, provider)
	ctx := context.Background()

	sameSecond := time.Now().UTC().Truncate(time.Second).Add(-time.Hour)
	const total = 5
	for i := 0; i < total; i++ {
		id := "usage:req-" + string(rune('a'+i)) + ":share-1"
		if _, err := repo.AppendLedger(ctx, TokenBankLedgerEntry{
			ID: id, UserID: "u1", Bucket: TokenBankBucketEarned, AmountMicro: 1_000_000,
			BizKey: id, CreatedAt: sameSecond,
		}); err != nil {
			t.Fatalf("AppendLedger(%d) error = %v", i, err)
		}
	}

	var got []string
	after := time.Time{}
	afterID := ""
	for {
		page, err := repo.ListLedgerAfter(ctx, after, afterID, 2)
		if err != nil {
			t.Fatalf("ListLedgerAfter() error = %v", err)
		}
		if len(page) == 0 {
			break
		}
		for _, entry := range page {
			got = append(got, entry.ID)
		}
		if len(page) < 2 {
			break
		}
		last := page[len(page)-1]
		after, afterID = last.CreatedAt, last.ID
	}
	if len(got) != total {
		t.Fatalf("paginated rows = %d (%v), want all %d — a same-second page boundary dropped rows", len(got), got, total)
	}
}

func TestLedgerBatchIDIsDeterministic(t *testing.T) {
	entries := []TokenBankLedgerEntry{
		{ID: "usage:req-1:share-1"},
		{ID: "usage:req-2:share-1"},
	}
	a := TokenBankLedgerBatchID("node-a", entries)
	b := TokenBankLedgerBatchID("node-a", entries)
	if a != b {
		t.Fatalf("ledgerBatchID is not deterministic: %q vs %q", a, b)
	}
	if c := TokenBankLedgerBatchID("node-b", entries); c == a {
		t.Fatalf("ledgerBatchID ignores the origin node: %q", c)
	}
	// Order matters: a re-derivation that produces the same id must have the
	// same content, otherwise a batch would be deduped against different rows.
	reordered := []TokenBankLedgerEntry{entries[1], entries[0]}
	if d := TokenBankLedgerBatchID("node-a", reordered); d == a {
		t.Fatalf("ledgerBatchID ignores ordering")
	}
}
