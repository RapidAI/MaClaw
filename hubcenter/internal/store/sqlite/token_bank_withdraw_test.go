package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func seedEarned(t *testing.T, repo *TokenBankRepo, userID string, amountMicro int64) {
	t.Helper()
	if _, err := repo.AppendLedger(context.Background(), TokenBankLedgerEntry{
		ID:          "usage:seed:share-1",
		UserID:      userID,
		Bucket:      TokenBankBucketEarned,
		AmountMicro: amountMicro,
		BizKey:      "usage:seed:share-1",
		CreatedAt:   time.Now().UTC(),
	}); err != nil {
		t.Fatalf("seed earned: %v", err)
	}
}

func TestTokenBankAutoWithdrawSplitsAcrossHubs(t *testing.T) {
	if got := AutoWithdrawLimitMicro(9_000_000, 3); got != 3_000_000 {
		t.Fatalf("AutoWithdrawLimitMicro(9e6, 3) = %d, want 3000000", got)
	}
	if got := AutoWithdrawLimitMicro(9_000_000, 0); got != 9_000_000 {
		t.Fatalf("AutoWithdrawLimitMicro(9e6, 0) = %d, want 9000000 (unknown hub count degrades to 1)", got)
	}
	if got := AutoWithdrawLimitMicro(5, 10); got != 0 {
		t.Fatalf("AutoWithdrawLimitMicro(5, 10) = %d, want 0 (rounds down, never over-issues)", got)
	}
	if got := AutoWithdrawLimitMicro(0, 3); got != 0 {
		t.Fatalf("AutoWithdrawLimitMicro(0, 3) = %d, want 0", got)
	}
}

func TestTokenBankWithdrawIsIdempotentOnRequestID(t *testing.T) {
	provider := newTokenBankTestProvider(t, filepath.Join(t.TempDir(), "tbk-wd-idem.db"))
	repo := newTokenBankTestRepo(t, provider)
	ctx := context.Background()
	seedEarned(t, repo, "user-wd", 10_000_000)

	req := TokenBankWithdrawRequest{
		RequestID: "withdraw:hub-1:1",
		UserID:    "user-wd",
		HubID:     "hub-1",
		Manual:    true,
	}
	first, created, err := repo.Withdraw(ctx, req)
	if err != nil {
		t.Fatalf("Withdraw() error = %v", err)
	}
	if !created {
		t.Fatalf("first Withdraw created = false, want true")
	}
	if first.AmountMicro != 10_000_000 {
		t.Fatalf("manual withdrawal amount = %d, want 10000000 (manual may take everything)", first.AmountMicro)
	}

	// A hub that crashed mid-flight, or that asks again because it lost its
	// registry, must get the first answer back and must not be debited twice.
	second, created, err := repo.Withdraw(ctx, req)
	if err != nil {
		t.Fatalf("Withdraw(replay) error = %v", err)
	}
	if created {
		t.Fatalf("replayed Withdraw created = true, want false")
	}
	if second.AmountMicro != first.AmountMicro || second.ID != first.ID {
		t.Fatalf("replayed withdrawal = %+v, want the first one %+v", second, first)
	}

	available, err := repo.AvailableMicro(ctx, "user-wd")
	if err != nil {
		t.Fatalf("AvailableMicro() error = %v", err)
	}
	if available != 0 {
		t.Fatalf("available after withdrawal = %d, want 0 (a replay must not debit again)", available)
	}

	// Nothing left: a third attempt has to fail loudly rather than mint a
	// zero-amount grant.
	if _, _, err := repo.Withdraw(ctx, TokenBankWithdrawRequest{
		RequestID: "withdraw:hub-1:2", UserID: "user-wd", HubID: "hub-1", Manual: true,
	}); !errors.Is(err, ErrTokenBankNothingToWithdraw) {
		t.Fatalf("Withdraw(exhausted) error = %v, want ErrTokenBankNothingToWithdraw", err)
	}
}

// TestTokenBankWithdrawNeverOverdraftsFrozenCredits is C1: a link that has been
// issued but not settled, or claimed but not withdrawn, has already been
// promised to somebody else. The withdrawal limit has to see that.
func TestTokenBankWithdrawNeverOverdraftsFrozenCredits(t *testing.T) {
	provider := newTokenBankTestProvider(t, filepath.Join(t.TempDir(), "tbk-wd-frozen.db"))
	repo := newTokenBankTestRepo(t, provider)
	ctx := context.Background()
	seedEarned(t, repo, "user-wd", 10_000_000)
	if _, err := repo.AppendLedger(ctx, TokenBankLedgerEntry{
		ID:          "freeze:link-1",
		UserID:      "user-wd",
		Bucket:      TokenBankBucketFrozen,
		AmountMicro: 6_000_000,
		BizKey:      "freeze:link-1",
		CreatedAt:   time.Now().UTC(),
	}); err != nil {
		t.Fatalf("freeze: %v", err)
	}

	got, _, err := repo.Withdraw(ctx, TokenBankWithdrawRequest{
		RequestID: "withdraw:hub-1:1", UserID: "user-wd", HubID: "hub-1", Manual: true,
	})
	if err != nil {
		t.Fatalf("Withdraw() error = %v", err)
	}
	if got.AmountMicro != 4_000_000 {
		t.Fatalf("withdrawal amount = %d, want 4000000; ignoring frozen credits would "+
			"let the same 6 credits be both gifted and withdrawn", got.AmountMicro)
	}
}

func TestTokenBankWithdrawBindsGrantAndLists(t *testing.T) {
	provider := newTokenBankTestProvider(t, filepath.Join(t.TempDir(), "tbk-wd-bind.db"))
	repo := newTokenBankTestRepo(t, provider)
	ctx := context.Background()
	seedEarned(t, repo, "user-wd", 8_000_000)

	if _, _, err := repo.Withdraw(ctx, TokenBankWithdrawRequest{
		RequestID: "withdraw:hub-1:1", UserID: "user-wd", HubID: "hub-1", Manual: true,
	}); err != nil {
		t.Fatalf("Withdraw() error = %v", err)
	}
	if err := repo.BindGrantID(ctx, "withdraw:hub-1:1", "hub-1", "grant_1"); err != nil {
		t.Fatalf("BindGrantID() error = %v", err)
	}
	if err := repo.BindGrantID(ctx, "withdraw:hub-1:1", "hub-2", "grant_1"); err == nil {
		t.Fatalf("BindGrantID(other hub) error = nil, want not found")
	}
	if err := repo.BindGrantID(ctx, "withdraw:hub-1:1", "hub-1", "grant_2"); err == nil {
		t.Fatalf("BindGrantID(twice) error = nil, want error")
	}

	list, err := repo.ListWithdrawals(ctx, "user-wd", "hub-1", 10)
	if err != nil {
		t.Fatalf("ListWithdrawals() error = %v", err)
	}
	if len(list) != 1 || list[0].GrantID != "grant_1" || list[0].Status != TokenBankWithdrawStatusBound {
		t.Fatalf("ListWithdrawals() = %+v, want one bound withdrawal with grant_1", list)
	}

	// A hub that was rebuilt asks again with the same request_id; the replay
	// returns the original amount, and only the status changes.
	replayed, created, err := repo.Withdraw(ctx, TokenBankWithdrawRequest{
		RequestID: "withdraw:hub-1:1", UserID: "user-wd", HubID: "hub-1", Manual: true,
	})
	if err != nil || created {
		t.Fatalf("Withdraw(replay after rebuild) created=%v error=%v, want false/nil", created, err)
	}
	if replayed.AmountMicro != 8_000_000 {
		t.Fatalf("replayed amount = %d, want 8000000", replayed.AmountMicro)
	}
	if err := repo.MarkReissued(ctx, "withdraw:hub-1:1"); err != nil {
		t.Fatalf("MarkReissued() error = %v", err)
	}
	list, err = repo.ListWithdrawals(ctx, "user-wd", "", 10)
	if err != nil {
		t.Fatalf("ListWithdrawals() error = %v", err)
	}
	if len(list) != 1 || list[0].Status != TokenBankWithdrawStatusReissued {
		t.Fatalf("status after reissue = %q, want %q", list[0].Status, TokenBankWithdrawStatusReissued)
	}
}

func TestTokenBankWithdrawReplayRejectsAnotherAccountOrHub(t *testing.T) {
	provider := newTokenBankTestProvider(t, filepath.Join(t.TempDir(), "tbk-wd-mismatch.db"))
	repo := newTokenBankTestRepo(t, provider)
	ctx := context.Background()
	seedEarned(t, repo, "user-wd", 10_000_000)

	first, created, err := repo.Withdraw(ctx, TokenBankWithdrawRequest{
		RequestID: "withdraw:hub-1:1", UserID: "user-wd", HubID: "hub-1", Manual: true, AmountMicro: 3_000_000,
	})
	if err != nil || !created || first.AmountMicro != 3_000_000 {
		t.Fatalf("first withdraw created=%v amount=%d err=%v", created, first.AmountMicro, err)
	}

	if _, _, err := repo.Withdraw(ctx, TokenBankWithdrawRequest{
		RequestID: "withdraw:hub-1:1", UserID: "user-other", HubID: "hub-1", Manual: true,
	}); !errors.Is(err, ErrTokenBankWithdrawalMismatch) {
		t.Fatalf("other account replay err = %v, want mismatch", err)
	}
	if _, _, err := repo.Withdraw(ctx, TokenBankWithdrawRequest{
		RequestID: "withdraw:hub-1:1", UserID: "user-wd", HubID: "hub-2", Manual: true,
	}); !errors.Is(err, ErrTokenBankWithdrawalMismatch) {
		t.Fatalf("other hub replay err = %v, want mismatch", err)
	}

	replayed, created, err := repo.Withdraw(ctx, TokenBankWithdrawRequest{
		RequestID: "withdraw:hub-1:1", UserID: "user-wd", HubID: "hub-1", Manual: true,
	})
	if err != nil || created || replayed.AmountMicro != 3_000_000 {
		t.Fatalf("same hub replay created=%v amount=%d err=%v", created, replayed.AmountMicro, err)
	}
	available, err := repo.AvailableMicro(ctx, "user-wd")
	if err != nil {
		t.Fatal(err)
	}
	if available != 7_000_000 {
		t.Fatalf("available = %d, want 7000000", available)
	}
	if err := repo.ReissueGrantID(ctx, "withdraw:hub-1:1", "hub-2", "grant-stolen"); err == nil {
		t.Fatal("reissue from another hub succeeded")
	}
	if err := repo.ReissueGrantID(ctx, "withdraw:hub-1:1", "hub-1", "grant-rebuilt"); err != nil {
		t.Fatalf("reissue from the owning hub: %v", err)
	}
}

// TestTokenBankWithdrawEmptyHubIDIsClaimedByTheFirstHub is the hole left when a
// user-session withdrawal omits hub_id. Replay used to hand that row to every
// later hub of the same account. Each hub then wrote a local grant before bind,
// and bind's hub_id match missed the empty row, so one debit became two grants.
func TestTokenBankWithdrawEmptyHubIDIsClaimedByTheFirstHub(t *testing.T) {
	provider := newTokenBankTestProvider(t, filepath.Join(t.TempDir(), "tbk-wd-claim.db"))
	repo := newTokenBankTestRepo(t, provider)
	ctx := context.Background()
	seedEarned(t, repo, "user-wd", 4_000_000)

	first, created, err := repo.Withdraw(ctx, TokenBankWithdrawRequest{
		RequestID: "withdraw:none:1", UserID: "user-wd", HubID: "", Manual: true,
	})
	if err != nil || !created || first.AmountMicro != 4_000_000 || first.HubID != "" {
		t.Fatalf("empty-hub withdraw created=%v amount=%d hub=%q err=%v", created, amountOf(first), hubOf(first), err)
	}

	again, created, err := repo.Withdraw(ctx, TokenBankWithdrawRequest{
		RequestID: "withdraw:none:1", UserID: "user-wd", HubID: "", Manual: true,
	})
	if err != nil || created || again.AmountMicro != 4_000_000 || again.HubID != "" {
		t.Fatalf("owner retry created=%v amount=%d hub=%q err=%v", created, amountOf(again), hubOf(again), err)
	}

	claimed, created, err := repo.Withdraw(ctx, TokenBankWithdrawRequest{
		RequestID: "withdraw:none:1", UserID: "user-wd", HubID: "hub-1", Manual: true,
	})
	if err != nil || created || claimed.AmountMicro != 4_000_000 || claimed.HubID != "hub-1" {
		t.Fatalf("claim created=%v amount=%d hub=%q err=%v", created, amountOf(claimed), hubOf(claimed), err)
	}

	list, err := repo.ListWithdrawals(ctx, "user-wd", "hub-1", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].HubID != "hub-1" || list[0].AmountMicro != 4_000_000 {
		t.Fatalf("ListWithdrawals(hub-1) = %+v, want the claimed row", list)
	}
	other, err := repo.ListWithdrawals(ctx, "user-wd", "hub-2", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(other) != 0 {
		t.Fatalf("ListWithdrawals(hub-2) = %+v, want none", other)
	}

	if _, _, err := repo.Withdraw(ctx, TokenBankWithdrawRequest{
		RequestID: "withdraw:none:1", UserID: "user-wd", HubID: "hub-2", Manual: true,
	}); !errors.Is(err, ErrTokenBankWithdrawalMismatch) {
		t.Fatalf("second hub replay err = %v, want mismatch", err)
	}
	if _, _, err := repo.Withdraw(ctx, TokenBankWithdrawRequest{
		RequestID: "withdraw:none:1", UserID: "user-other", HubID: "hub-1", Manual: true,
	}); !errors.Is(err, ErrTokenBankWithdrawalMismatch) {
		t.Fatalf("other account replay err = %v, want mismatch", err)
	}

	available, err := repo.AvailableMicro(ctx, "user-wd")
	if err != nil {
		t.Fatal(err)
	}
	if available != 0 {
		t.Fatalf("available = %d, want 0 (a rejected hub must not debit again)", available)
	}

	replayed, created, err := repo.Withdraw(ctx, TokenBankWithdrawRequest{
		RequestID: "withdraw:none:1", UserID: "user-wd", HubID: "hub-1", Manual: true,
	})
	if err != nil || created || replayed.AmountMicro != 4_000_000 || replayed.HubID != "hub-1" {
		t.Fatalf("owning hub replay created=%v amount=%d hub=%q err=%v", created, amountOf(replayed), hubOf(replayed), err)
	}

	// After the claim, an owner retry that still omits hub_id keeps the row.
	owner, created, err := repo.Withdraw(ctx, TokenBankWithdrawRequest{
		RequestID: "withdraw:none:1", UserID: "user-wd", Manual: true,
	})
	if err != nil || created || owner.AmountMicro != 4_000_000 || owner.HubID != "hub-1" {
		t.Fatalf("owner retry after claim created=%v amount=%d hub=%q err=%v", created, amountOf(owner), hubOf(owner), err)
	}

	if err := repo.BindGrantID(ctx, "withdraw:none:1", "hub-1", "grant-1"); err != nil {
		t.Fatalf("BindGrantID(claiming hub) error = %v", err)
	}
	if err := repo.BindGrantID(ctx, "withdraw:none:1", "hub-2", "grant-stolen"); err == nil {
		t.Fatal("BindGrantID(other hub) succeeded")
	}
}

func amountOf(w *TokenBankWithdrawal) int64 {
	if w == nil {
		return 0
	}
	return w.AmountMicro
}

func hubOf(w *TokenBankWithdrawal) string {
	if w == nil {
		return ""
	}
	return w.HubID
}

// TestTokenBankHubCountDrivesAutoLimit wires the N of §14.5 to the hub registry:
// the ledger is keyed by sm_users.id, the registry by email, so the join has to
// go through sm_users or the count silently answers 0 and the cap disappears.
func TestTokenBankHubCountDrivesAutoLimit(t *testing.T) {
	provider := newTokenBankTestProvider(t, filepath.Join(t.TempDir(), "tbk-wd-hubs.db"))
	repo := newTokenBankTestRepo(t, provider)
	ctx := context.Background()

	now := time.Now().UTC().Format(time.RFC3339)
	mustExec := func(stmt string, args ...any) {
		t.Helper()
		if _, err := provider.Write.ExecContext(ctx, stmt, args...); err != nil {
			t.Fatalf("seed %q: %v", stmt, err)
		}
	}
	// sm_users is owned by skillmarket's migrate, which runs against this same
	// sqlite file in production (hubcenter/internal/app/bootstrap.go wires
	// skillmarket.NewStore with the shared provider). This package's migrations
	// do not create it, so the test creates the two columns the join needs.
	mustExec(`CREATE TABLE IF NOT EXISTS sm_users (
		id TEXT PRIMARY KEY, email TEXT NOT NULL UNIQUE,
		created_at TEXT NOT NULL, updated_at TEXT NOT NULL)`)
	mustExec(`INSERT INTO sm_users (id, email, created_at, updated_at) VALUES (?, ?, ?, ?)`,
		"user-wd", "wd@example.com", now, now)
	for i, hub := range []string{"hub-a", "hub-b"} {
		mustExec(`INSERT INTO hub_user_links (id, hub_id, email, is_default, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
			"link-"+hub, hub, "wd@example.com", i, now, now)
	}

	seedEarned(t, repo, "user-wd", 9_000_000)
	got, _, err := repo.Withdraw(ctx, TokenBankWithdrawRequest{
		RequestID: "withdraw:hub-a:1", UserID: "user-wd", HubID: "hub-a",
	})
	if err != nil {
		t.Fatalf("Withdraw() error = %v", err)
	}
	// 9 credits over 2 hubs: one automatic pull may take at most 4.5.
	if got.AmountMicro != 4_500_000 {
		t.Fatalf("auto withdrawal amount = %d, want 4500000 (half of 9000000 over 2 hubs)", got.AmountMicro)
	}

	// The second hub is not locked out: the first pull left half the pool
	// behind, which is the whole point of the rule.
	//
	// Note the cap is floor(available / N) measured against what is *left*, not
	// a reserved per-hub quota, so a second pull takes half of the remainder
	// (2.25 of the original 9). That is the formula §14.5 pins down; it is what
	// keeps credits flowing to whoever asks instead of stranding them behind a
	// quota a hub may never claim. The consequence is that a hub which asks
	// late gets a smaller share, so it simply asks again later.
	got, _, err = repo.Withdraw(ctx, TokenBankWithdrawRequest{
		RequestID: "withdraw:hub-b:1", UserID: "user-wd", HubID: "hub-b",
	})
	if err != nil {
		t.Fatalf("Withdraw(hub-b) error = %v", err)
	}
	if got.AmountMicro != 2_250_000 {
		t.Fatalf("second auto withdrawal amount = %d, want 2250000 (half of the remaining 4500000)", got.AmountMicro)
	}
	remaining, err := repo.AvailableMicro(ctx, "user-wd")
	if err != nil {
		t.Fatalf("AvailableMicro() error = %v", err)
	}
	if remaining != 2_250_000 {
		t.Fatalf("remaining after two pulls = %d, want 2250000", remaining)
	}
}

// TestTokenBankWithdrawHonoursExplicitAmount pins the amount contract: a caller
// that names a figure gets exactly that figure, not the whole balance. Before
// this field existed the only expressible requests were "everything" and
// "exactly 1/N", so a hub budgeting 2 credits out of 10 was handed all 10.
func TestTokenBankWithdrawHonoursExplicitAmount(t *testing.T) {
	provider := newTokenBankTestProvider(t, filepath.Join(t.TempDir(), "tbk-wd-amount.db"))
	repo := newTokenBankTestRepo(t, provider)
	ctx := context.Background()
	seedEarned(t, repo, "user-amt", 10_000_000)

	got, created, err := repo.Withdraw(ctx, TokenBankWithdrawRequest{
		RequestID:   "withdraw:hub-1:budget",
		UserID:      "user-amt",
		HubID:       "hub-1",
		AmountMicro: 2_000_000,
		Manual:      true,
	})
	if err != nil {
		t.Fatalf("Withdraw() error = %v", err)
	}
	if !created {
		t.Fatalf("created = false, want true")
	}
	if got.AmountMicro != 2_000_000 {
		t.Fatalf("amount = %d, want 2000000 (the requested figure, not the whole balance)", got.AmountMicro)
	}
	remaining, err := repo.AvailableMicro(ctx, "user-amt")
	if err != nil {
		t.Fatalf("AvailableMicro() error = %v", err)
	}
	if remaining != 8_000_000 {
		t.Fatalf("remaining = %d, want 8000000", remaining)
	}
}

// TestTokenBankWithdrawRefusesAmountAboveCap pins the refusal rule. Truncating
// instead would leave the hub believing it moved 9 when only 3 moved; because
// the retry reuses the same request_id it would be deduplicated, so the hub
// would never converge on the truth.
func TestTokenBankWithdrawRefusesAmountAboveCap(t *testing.T) {
	provider := newTokenBankTestProvider(t, filepath.Join(t.TempDir(), "tbk-wd-overcap.db"))
	repo := newTokenBankTestRepo(t, provider)
	ctx := context.Background()
	seedEarned(t, repo, "user-over", 3_000_000)

	_, _, err := repo.Withdraw(ctx, TokenBankWithdrawRequest{
		RequestID:   "withdraw:hub-1:over",
		UserID:      "user-over",
		HubID:       "hub-1",
		AmountMicro: 9_000_000,
		Manual:      true,
	})
	if !errors.Is(err, ErrTokenBankInsufficient) {
		t.Fatalf("Withdraw() error = %v, want ErrTokenBankInsufficient", err)
	}
	// The rejection must leave no trace: no debit row, no withdrawal row.
	balance, err := repo.Balance(ctx, "user-over")
	if err != nil {
		t.Fatalf("Balance() error = %v", err)
	}
	if balance.WithdrawnMicro != 0 {
		t.Fatalf("withdrawn_micro = %d after a refused withdrawal, want 0", balance.WithdrawnMicro)
	}
	list, err := repo.ListWithdrawals(ctx, "user-over", "", 0)
	if err != nil {
		t.Fatalf("ListWithdrawals() error = %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("withdrawals = %d after a refused withdrawal, want 0", len(list))
	}
}

// TestTokenBankAutoWithdrawHonoursAmountUpToItsShare is the interaction of the
// two rules: the 1/N cap still binds when an amount is supplied. An automatic
// caller cannot escape the split by naming a number.
func TestTokenBankAutoWithdrawHonoursAmountUpToItsShare(t *testing.T) {
	provider := newTokenBankTestProvider(t, filepath.Join(t.TempDir(), "tbk-wd-auto-amount.db"))
	repo := newTokenBankTestRepo(t, provider)
	ctx := context.Background()
	seedEarned(t, repo, "user-auto", 9_000_000)
	// sm_users and hub_user_links are created by other modules' migrations; this
	// package's schema does not own them. See TestTokenBankHubCountDrivesAutoLimit.
	now := time.Now().UTC().Format(time.RFC3339)
	mustExec := func(stmt string, args ...any) {
		t.Helper()
		if _, err := provider.Write.ExecContext(ctx, stmt, args...); err != nil {
			t.Fatalf("seed %q: %v", stmt, err)
		}
	}
	mustExec(`CREATE TABLE IF NOT EXISTS sm_users (
		id TEXT PRIMARY KEY, email TEXT NOT NULL UNIQUE,
		created_at TEXT NOT NULL, updated_at TEXT NOT NULL)`)
	mustExec(`INSERT INTO sm_users (id, email, created_at, updated_at) VALUES (?, ?, ?, ?)`,
		"user-auto", "auto@example.test", now, now)
	for i, hub := range []string{"hub-a", "hub-b", "hub-c"} {
		mustExec(`INSERT INTO hub_user_links (id, hub_id, email, is_default, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
			"auto-link-"+hub, hub, "auto@example.test", i, now, now)
	}

	// 9e6 over 3 hubs = 3e6 cap; asking for 1e6 is inside it and is honoured.
	got, _, err := repo.Withdraw(ctx, TokenBankWithdrawRequest{
		RequestID:   "withdraw:hub-a:auto",
		UserID:      "user-auto",
		HubID:       "hub-a",
		AmountMicro: 1_000_000,
	})
	if err != nil {
		t.Fatalf("Withdraw(auto, 1e6) error = %v", err)
	}
	if got.AmountMicro != 1_000_000 {
		t.Fatalf("amount = %d, want 1000000", got.AmountMicro)
	}

	// Asking for 5e6 with a 4e6 cap (8e6 remaining over 3 hubs, rounded down)
	// must be refused rather than clamped to the cap.
	_, _, err = repo.Withdraw(ctx, TokenBankWithdrawRequest{
		RequestID:   "withdraw:hub-b:auto",
		UserID:      "user-auto",
		HubID:       "hub-b",
		AmountMicro: 5_000_000,
	})
	if !errors.Is(err, ErrTokenBankInsufficient) {
		t.Fatalf("Withdraw(auto, 5e6) error = %v, want ErrTokenBankInsufficient", err)
	}
}

func TestListUnboundGiftWithdrawalsIgnoresTheHistoryPage(t *testing.T) {
	repo, _ := newGiftTestRepo(t, "tbk-unbound-gift-list.db")
	ctx := context.Background()
	seedEarnedFor(t, repo, "sender", "req-sender", 20_000_000)
	seedEarnedFor(t, repo, "receiver", "req-receiver", 20_000_000)
	openAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	boundAt := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	selfAt := time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC)

	if _, err := repo.CreateGiftLink(ctx, TokenBankGiftLink{
		ID: "link-open", Code: "code-open", SenderUserID: "sender", CreditsMicro: 5_000_000,
	}, GiftLinkPolicy{}, openAt); err != nil {
		t.Fatalf("CreateGiftLink(open) error = %v", err)
	}
	if _, err := repo.ClaimGiftLink(ctx, "code-open", "receiver", "r@example.com", openAt); err != nil {
		t.Fatalf("ClaimGiftLink(open) error = %v", err)
	}
	if _, err := repo.SettleClaimedGift(ctx, "link-open", openAt); err != nil {
		t.Fatalf("SettleClaimedGift(open) error = %v", err)
	}
	if _, _, err := repo.Withdraw(ctx, TokenBankWithdrawRequest{
		RequestID: "gift-open", UserID: "receiver", HubID: "hub-1",
		AmountMicro: 5_000_000, Manual: true, Kind: "gift", LinkID: "link-open", Now: openAt,
	}); err != nil {
		t.Fatalf("Withdraw(open) error = %v", err)
	}

	if _, err := repo.CreateGiftLink(ctx, TokenBankGiftLink{
		ID: "link-bound", Code: "code-bound", SenderUserID: "sender", CreditsMicro: 4_000_000,
	}, GiftLinkPolicy{}, boundAt); err != nil {
		t.Fatalf("CreateGiftLink(bound) error = %v", err)
	}
	if _, err := repo.ClaimGiftLink(ctx, "code-bound", "receiver", "r@example.com", boundAt); err != nil {
		t.Fatalf("ClaimGiftLink(bound) error = %v", err)
	}
	if _, err := repo.SettleClaimedGift(ctx, "link-bound", boundAt); err != nil {
		t.Fatalf("SettleClaimedGift(bound) error = %v", err)
	}
	if _, _, err := repo.Withdraw(ctx, TokenBankWithdrawRequest{
		RequestID: "gift-bound", UserID: "receiver", HubID: "hub-1",
		AmountMicro: 4_000_000, Manual: true, Kind: "gift", LinkID: "link-bound", Now: boundAt,
	}); err != nil {
		t.Fatalf("Withdraw(bound) error = %v", err)
	}
	if err := repo.BindGrantID(ctx, "gift-bound", "hub-1", "grant-1"); err != nil {
		t.Fatalf("BindGrantID() error = %v", err)
	}
	if _, _, err := repo.Withdraw(ctx, TokenBankWithdrawRequest{
		RequestID: "self-new", UserID: "receiver", HubID: "hub-1",
		AmountMicro: 1_000_000, Manual: true, Now: selfAt,
	}); err != nil {
		t.Fatalf("Withdraw(self) error = %v", err)
	}

	page, err := repo.ListWithdrawals(ctx, "receiver", "", 1)
	if err != nil {
		t.Fatalf("ListWithdrawals() error = %v", err)
	}
	if len(page) != 1 || page[0].RequestID != "self-new" {
		t.Fatalf("history page = %+v, want only self-new", page)
	}
	pending, err := repo.ListUnboundGiftWithdrawals(ctx, "receiver", "", 100)
	if err != nil {
		t.Fatalf("ListUnboundGiftWithdrawals() error = %v", err)
	}
	if len(pending) != 1 || pending[0].RequestID != "gift-open" || pending[0].LinkID != "link-open" || pending[0].GrantID != "" {
		t.Fatalf("unbound gifts = %+v, want only gift-open", pending)
	}
	otherHub, err := repo.ListUnboundGiftWithdrawals(ctx, "receiver", "hub-2", 100)
	if err != nil {
		t.Fatalf("ListUnboundGiftWithdrawals(hub-2) error = %v", err)
	}
	if len(otherHub) != 0 {
		t.Fatalf("other hub unbound gifts = %+v, want none", otherHub)
	}
	if _, err := repo.ListUnboundGiftWithdrawals(ctx, "  ", "", 100); err == nil {
		t.Fatal("empty user id was accepted")
	}
}
