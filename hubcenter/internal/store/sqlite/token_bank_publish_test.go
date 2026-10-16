package sqlite

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// collectSink records every entry the repo hands to the replication hook.
type collectSink struct {
	mu      sync.Mutex
	entries []TokenBankLedgerEntry
}

func (s *collectSink) Add(entry TokenBankLedgerEntry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = append(s.entries, entry)
}

func (s *collectSink) snapshot() []TokenBankLedgerEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]TokenBankLedgerEntry, len(s.entries))
	copy(out, s.entries)
	return out
}

// TestTokenBankEveryLedgerWriteIsPublished guards the producer half of ledger
// replication. The receiving half (InsertLedgerBatch) can be perfect and the
// cluster still diverges if nothing ever hands the rows over — which is exactly
// the state the store layer shipped in at first.
func TestTokenBankEveryLedgerWriteIsPublished(t *testing.T) {
	provider := newTokenBankTestProvider(t, filepath.Join(t.TempDir(), "tbk-publish.db"))
	repo := newTokenBankTestRepo(t, provider)
	sink := &collectSink{}
	repo.SetSyncSink(sink.Add)

	ctx := context.Background()
	now := time.Now().UTC()
	// Seed generously: the flow below gifts twice and also withdraws everything
	// that is left, so the balance has to cover all of it.
	seedEarnedFor(t, repo, "sender", "req-1", 20_000_000)

	// 1. Gifting freezes a row.
	if _, err := repo.CreateGiftLink(ctx, TokenBankGiftLink{
		ID: "link-1", Code: "code-1", SenderUserID: "sender", CreditsMicro: 3_000_000,
	}, GiftLinkPolicy{}, now); err != nil {
		t.Fatalf("CreateGiftLink() error = %v", err)
	}
	if _, err := repo.ClaimGiftLink(ctx, "code-1", "receiver", "r@example.com", now); err != nil {
		t.Fatalf("ClaimGiftLink() error = %v", err)
	}
	// 2. Settling moves three rows: unfreeze, grant, receive.
	if applied, err := repo.SettleClaimedGift(ctx, "link-1", now); err != nil || !applied {
		t.Fatalf("SettleClaimedGift() applied=%v error=%v", applied, err)
	}
	// 3. A second link, this time revoked rather than settled.
	if _, err := repo.CreateGiftLink(ctx, TokenBankGiftLink{
		ID: "link-2", Code: "code-2", SenderUserID: "sender", CreditsMicro: 1_000_000,
	}, GiftLinkPolicy{}, now); err != nil {
		t.Fatalf("CreateGiftLink(2) error = %v", err)
	}
	if err := repo.RevokeGiftLink(ctx, "link-2", "sender", now); err != nil {
		t.Fatalf("RevokeGiftLink() error = %v", err)
	}
	// 4. Withdrawing writes a row.
	if _, _, err := repo.Withdraw(ctx, TokenBankWithdrawRequest{
		RequestID: "withdraw:hub-1:1", UserID: "sender", HubID: "hub-1", Manual: true,
	}); err != nil {
		t.Fatalf("Withdraw() error = %v", err)
	}

	published := sink.snapshot()
	// Every ledger row the flow produced must have been handed to the hook.
	wantIDs := map[string]bool{
		"usage:req-1:share-1":                         false, // seeded earned
		giftLedgerID("frz", "freeze:link-1"):          false, // freeze
		giftLedgerID("unfrz", "unfreeze:link-1"):      false, // settle: release
		giftLedgerID("grnt", "grant:link-1"):          false, // settle: spent
		giftLedgerID("rcv", "receive:link-1"):         false, // settle: credited
		tokenBankWithdrawLedgerID("withdraw:hub-1:1"): false, // withdrawal
		giftLedgerID("frz", "freeze:link-2"):          false, // second freeze
		giftLedgerID("unfrz", "unfreeze:link-2"):      false, // revoke release
	}
	for _, entry := range published {
		if _, ok := wantIDs[entry.ID]; ok {
			wantIDs[entry.ID] = true
		}
	}
	for id, seen := range wantIDs {
		if !seen {
			t.Fatalf("ledger row %q was written but never published for replication; got %v", id, idsOf(published))
		}
	}
}

func idsOf(entries []TokenBankLedgerEntry) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.ID)
	}
	return out
}

// TestTokenBankPublishHappensAfterCommit pins the ordering. A sink called before
// the commit could hand a peer a row that then rolled back, and the peer has no
// way to distinguish it from a real movement.
func TestTokenBankPublishHappensAfterCommit(t *testing.T) {
	provider := newTokenBankTestProvider(t, filepath.Join(t.TempDir(), "tbk-publish-order.db"))
	repo := newTokenBankTestRepo(t, provider)

	var seenWhileInFlight int
	repo.SetSyncSink(func(entry TokenBankLedgerEntry) {
		var count int
		if err := provider.Read.QueryRowContext(context.Background(),
			`SELECT COUNT(*) FROM token_bank_ledger WHERE id = ?`, entry.ID).Scan(&count); err == nil {
			seenWhileInFlight += count
		}
	})

	if _, err := repo.AppendLedger(context.Background(), TokenBankLedgerEntry{
		ID: "usage:req-1:share-1", UserID: "u1", Bucket: TokenBankBucketEarned,
		AmountMicro: 1_000, BizKey: "usage:req-1:share-1", CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("AppendLedger() error = %v", err)
	}
	if seenWhileInFlight != 1 {
		t.Fatalf("the published row was not yet visible when the sink ran; "+
			"the sink must fire after commit (saw %d)", seenWhileInFlight)
	}
}

func TestTokenBankNilSinkIsSafe(t *testing.T) {
	provider := newTokenBankTestProvider(t, filepath.Join(t.TempDir(), "tbk-nil-sink.db"))
	repo := newTokenBankTestRepo(t, provider)
	repo.SetSyncSink(nil)

	if _, err := repo.AppendLedger(context.Background(), TokenBankLedgerEntry{
		ID: "usage:req-1:share-1", UserID: "u1", Bucket: TokenBankBucketEarned,
		AmountMicro: 1_000, BizKey: "usage:req-1:share-1", CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("AppendLedger() with a nil sink error = %v; a single-node deployment must work", err)
	}
}
