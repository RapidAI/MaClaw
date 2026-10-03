package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func newGiftTestRepo(t *testing.T, name string) (*TokenBankRepo, *Provider) {
	t.Helper()
	provider := newTokenBankTestProvider(t, filepath.Join(t.TempDir(), name))
	return newTokenBankTestRepo(t, provider), provider
}

func seedEarnedFor(t *testing.T, repo *TokenBankRepo, userID, ref string, amountMicro int64) {
	t.Helper()
	id := "usage:" + ref + ":share-1"
	if _, err := repo.AppendLedger(context.Background(), TokenBankLedgerEntry{
		ID: id, UserID: userID, Bucket: TokenBankBucketEarned,
		AmountMicro: amountMicro, BizKey: id, CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("seed earned for %s: %v", userID, err)
	}
}

func TestGiftShareCapIsHalfRoundedDown(t *testing.T) {
	if got := GiftShareCapMicro(10_000_000, GiftLinkPolicy{}); got != 5_000_000 {
		t.Fatalf("GiftShareCapMicro(10e6, default policy) = %d, want 5000000", got)
	}
	// Odd balances round down: rounding up could exceed the cap.
	if got := GiftShareCapMicro(9_999_999, GiftLinkPolicy{}); got != 4_999_999 {
		t.Fatalf("GiftShareCapMicro(9999999, default policy) = %d, want 4999999", got)
	}
	if got := GiftShareCapMicro(0, GiftLinkPolicy{}); got != 0 {
		t.Fatalf("GiftShareCapMicro(0) = %d, want 0", got)
	}
	if got := GiftShareCapMicro(-5, GiftLinkPolicy{}); got != 0 {
		t.Fatalf("GiftShareCapMicro(-5) = %d, want 0", got)
	}
}

func TestGiftCreateFreezesAndRejectsOverCap(t *testing.T) {
	repo, _ := newGiftTestRepo(t, "tbk-gift-create.db")
	ctx := context.Background()
	seedEarnedFor(t, repo, "sender", "req-1", 10_000_000)

	// Over the 50% cap: 6 of 10.
	if _, err := repo.CreateGiftLink(ctx, TokenBankGiftLink{
		ID: "link-over", Code: "code-over", SenderUserID: "sender", CreditsMicro: 6_000_000,
	}, GiftLinkPolicy{}, time.Now().UTC()); !errors.Is(err, ErrGiftLinkOverCap) {
		t.Fatalf("CreateGiftLink(over cap) error = %v, want ErrGiftLinkOverCap", err)
	}

	link, err := repo.CreateGiftLink(ctx, TokenBankGiftLink{
		ID: "link-1", Code: "code-1", SenderUserID: "sender", CreditsMicro: 5_000_000,
	}, GiftLinkPolicy{}, time.Now().UTC())
	if err != nil {
		t.Fatalf("CreateGiftLink() error = %v", err)
	}
	if link.Status != TokenBankGiftStatusActive {
		t.Fatalf("new link status = %q, want active", link.Status)
	}

	// The freeze must be visible to the balance arithmetic. Without it the
	// sender could withdraw all 10 and the receiver still collect 5.
	available, err := repo.AvailableMicro(ctx, "sender")
	if err != nil {
		t.Fatalf("AvailableMicro() error = %v", err)
	}
	if available != 5_000_000 {
		t.Fatalf("available after freezing 5 of 10 = %d, want 5000000", available)
	}

	// A second link is capped against what is left, not the original balance.
	if _, err := repo.CreateGiftLink(ctx, TokenBankGiftLink{
		ID: "link-2", Code: "code-2", SenderUserID: "sender", CreditsMicro: 3_000_000,
	}, GiftLinkPolicy{}, time.Now().UTC()); !errors.Is(err, ErrGiftLinkOverCap) {
		t.Fatalf("CreateGiftLink(second, over remaining cap) error = %v, want ErrGiftLinkOverCap", err)
	}
	if _, err := repo.CreateGiftLink(ctx, TokenBankGiftLink{
		ID: "link-2", Code: "code-2", SenderUserID: "sender", CreditsMicro: 2_500_000,
	}, GiftLinkPolicy{}, time.Now().UTC()); err != nil {
		t.Fatalf("CreateGiftLink(second, within remaining cap) error = %v", err)
	}
	available, _ = repo.AvailableMicro(ctx, "sender")
	if available != 2_500_000 {
		t.Fatalf("available after two freezes = %d, want 2500000", available)
	}
}

// TestGiftClaimIsFirstComeOnly is the atomicity requirement of §3.6: two people
// open the same link, exactly one wins.
func TestGiftClaimIsFirstComeOnly(t *testing.T) {
	repo, _ := newGiftTestRepo(t, "tbk-gift-claim.db")
	ctx := context.Background()
	seedEarnedFor(t, repo, "sender", "req-1", 10_000_000)
	if _, err := repo.CreateGiftLink(ctx, TokenBankGiftLink{
		ID: "link-1", Code: "code-1", SenderUserID: "sender", CreditsMicro: 5_000_000,
	}, GiftLinkPolicy{}, time.Now().UTC()); err != nil {
		t.Fatalf("CreateGiftLink() error = %v", err)
	}

	const claimers = 8
	var wg sync.WaitGroup
	var mu sync.Mutex
	wins := 0
	start := make(chan struct{})
	for i := 0; i < claimers; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			claimer := "claimer-" + string(rune('a'+i))
			_, err := repo.ClaimGiftLink(ctx, "code-1", claimer, claimer+"@example.com", time.Now().UTC())
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				wins++
			case errors.Is(err, ErrGiftLinkNotActive) || errors.Is(err, ErrGiftLinkExpired):
				// expected for the losers
			default:
				t.Errorf("ClaimGiftLink() unexpected error = %v", err)
			}
		}()
	}
	close(start)
	wg.Wait()

	if wins != 1 {
		t.Fatalf("claim winners = %d, want exactly 1", wins)
	}

	// The link must now be held by that single winner, and only that winner.
	var claimedBy string
	if err := repo.read.QueryRowContext(ctx,
		`SELECT claimed_by_user_id FROM credit_share_links WHERE id = 'link-1'`).Scan(&claimedBy); err != nil {
		t.Fatalf("read claimed_by: %v", err)
	}
	if claimedBy == "" {
		t.Fatalf("claimed_by_user_id is empty after a winning claim")
	}
	var status string
	if err := repo.read.QueryRowContext(ctx,
		`SELECT status FROM credit_share_links WHERE id = 'link-1'`).Scan(&status); err != nil {
		t.Fatalf("read status: %v", err)
	}
	if status != TokenBankGiftStatusClaimed {
		t.Fatalf("status after claim = %q, want %q", status, TokenBankGiftStatusClaimed)
	}
}

// TestGiftClaimMovesNoMoney pins the v5/v6 decision: claiming only binds the
// person. The sender's balance is untouched by a claim, and the receiver has
// nothing yet — the credits move when the receiver withdraws.
func TestGiftClaimMovesNoMoney(t *testing.T) {
	repo, _ := newGiftTestRepo(t, "tbk-gift-nomoney.db")
	ctx := context.Background()
	seedEarnedFor(t, repo, "sender", "req-1", 10_000_000)
	if _, err := repo.CreateGiftLink(ctx, TokenBankGiftLink{
		ID: "link-1", Code: "code-1", SenderUserID: "sender", CreditsMicro: 5_000_000,
	}, GiftLinkPolicy{}, time.Now().UTC()); err != nil {
		t.Fatalf("CreateGiftLink() error = %v", err)
	}
	before, _ := repo.Balance(ctx, "sender")

	if _, err := repo.ClaimGiftLink(ctx, "code-1", "receiver", "r@example.com", time.Now().UTC()); err != nil {
		t.Fatalf("ClaimGiftLink() error = %v", err)
	}

	after, _ := repo.Balance(ctx, "sender")
	if after != before {
		t.Fatalf("sender balance changed by claim: before %+v after %+v", before, after)
	}
	receiver, _ := repo.Balance(ctx, "receiver")
	if receiver.ReceivedMicro != 0 {
		t.Fatalf("receiver received_micro after claim = %d, want 0 (claim binds only)", receiver.ReceivedMicro)
	}

	// Settle: now the money moves, and both sides move in one transaction.
	applied, err := repo.SettleClaimedGift(ctx, "link-1", time.Now().UTC())
	if err != nil {
		t.Fatalf("SettleClaimedGift() error = %v", err)
	}
	if !applied {
		t.Fatalf("SettleClaimedGift applied = false, want true")
	}
	receiver, _ = repo.Balance(ctx, "receiver")
	if receiver.ReceivedMicro != 5_000_000 {
		t.Fatalf("receiver received_micro = %d, want 5000000", receiver.ReceivedMicro)
	}
	sender, _ := repo.Balance(ctx, "sender")
	if sender.FrozenMicro != 0 {
		t.Fatalf("sender frozen_micro = %d, want 0 after settle", sender.FrozenMicro)
	}
	if sender.GrantedMicro != 5_000_000 {
		t.Fatalf("sender granted_micro = %d, want 5000000", sender.GrantedMicro)
	}
	// The sender's available must be unchanged by the settle: those credits were
	// already deducted from available at freeze time.
	if got := sender.AvailableMicro(); got != 5_000_000 {
		t.Fatalf("sender available after settle = %d, want 5000000", got)
	}

	// Replay must not pay twice.
	applied, err = repo.SettleClaimedGift(ctx, "link-1", time.Now().UTC())
	if err != nil {
		t.Fatalf("SettleClaimedGift(replay) error = %v", err)
	}
	if applied {
		t.Fatalf("SettleClaimedGift(replay) applied = true, want false")
	}
	receiver, _ = repo.Balance(ctx, "receiver")
	if receiver.ReceivedMicro != 5_000_000 {
		t.Fatalf("receiver received_micro after replay = %d, want 5000000", receiver.ReceivedMicro)
	}
}

// TestGiftCannotOverIssueWhenSenderWithdrawsFirst is the C1 attack from §3.6,
// end to end: freeze 10, drain everything else, then let the link settle. Not
// one microcredit may be created out of thin air.
func TestGiftCannotOverIssueWhenSenderWithdrawsFirst(t *testing.T) {
	repo, _ := newGiftTestRepo(t, "tbk-gift-c1.db")
	ctx := context.Background()
	seedEarnedFor(t, repo, "sender", "req-1", 10_000_000)

	if _, err := repo.CreateGiftLink(ctx, TokenBankGiftLink{
		ID: "link-1", Code: "code-1", SenderUserID: "sender", CreditsMicro: 5_000_000,
	}, GiftLinkPolicy{}, time.Now().UTC()); err != nil {
		t.Fatalf("CreateGiftLink() error = %v", err)
	}

	// The sender now tries to pull the whole 10 into their own hub. The
	// withdrawal must see the frozen 5 and cap itself at 5.
	wd, _, err := repo.Withdraw(ctx, TokenBankWithdrawRequest{
		RequestID: "withdraw:hub-1:1", UserID: "sender", HubID: "hub-1", Manual: true,
	})
	if err != nil {
		t.Fatalf("Withdraw() error = %v", err)
	}
	if wd.AmountMicro != 5_000_000 {
		t.Fatalf("withdrawal amount = %d, want 5000000; the frozen gift must not be withdrawable", wd.AmountMicro)
	}

	if _, err := repo.ClaimGiftLink(ctx, "code-1", "receiver", "r@example.com", time.Now().UTC()); err != nil {
		t.Fatalf("ClaimGiftLink() error = %v", err)
	}
	if _, err := repo.SettleClaimedGift(ctx, "link-1", time.Now().UTC()); err != nil {
		t.Fatalf("SettleClaimedGift() error = %v", err)
	}

	// Conservation: the 10 earned microcredits split into 5 withdrawn (which
	// left hubcenter as a hub grant, so they are legitimately outside the
	// ledger) plus 5 received by the receiver. Nothing may be created or lost.
	sender, _ := repo.Balance(ctx, "sender")
	receiver, _ := repo.Balance(ctx, "receiver")
	insideLedger := sender.AvailableMicro() + receiver.AvailableMicro()
	outsideAsGrant := sender.WithdrawnMicro
	if insideLedger+outsideAsGrant != sender.EarnedMicro {
		t.Fatalf("conservation broken: sender %+v receiver %+v -> inside %d + withdrawn %d != earned %d",
			sender, receiver, insideLedger, outsideAsGrant, sender.EarnedMicro)
	}
	if sender.AvailableMicro() != 0 {
		t.Fatalf("sender available = %d, want 0", sender.AvailableMicro())
	}
	if receiver.AvailableMicro() != 5_000_000 {
		t.Fatalf("receiver available = %d, want 5000000", receiver.AvailableMicro())
	}
}

func TestGiftRevokeReleasesFreeze(t *testing.T) {
	repo, _ := newGiftTestRepo(t, "tbk-gift-revoke.db")
	ctx := context.Background()
	seedEarnedFor(t, repo, "sender", "req-1", 10_000_000)
	if _, err := repo.CreateGiftLink(ctx, TokenBankGiftLink{
		ID: "link-1", Code: "code-1", SenderUserID: "sender", CreditsMicro: 5_000_000,
	}, GiftLinkPolicy{}, time.Now().UTC()); err != nil {
		t.Fatalf("CreateGiftLink() error = %v", err)
	}
	if err := repo.RevokeGiftLink(ctx, "link-1", "sender", time.Now().UTC()); err != nil {
		t.Fatalf("RevokeGiftLink() error = %v", err)
	}
	available, _ := repo.AvailableMicro(ctx, "sender")
	if available != 10_000_000 {
		t.Fatalf("available after revoke = %d, want 10000000", available)
	}
	// Revoking twice is a conflict, not a second release.
	if err := repo.RevokeGiftLink(ctx, "link-1", "sender", time.Now().UTC()); !errors.Is(err, ErrGiftLinkNotActive) {
		t.Fatalf("RevokeGiftLink(twice) error = %v, want ErrGiftLinkNotActive", err)
	}
	available, _ = repo.AvailableMicro(ctx, "sender")
	if available != 10_000_000 {
		t.Fatalf("available after double revoke = %d, want 10000000 (must not double-release)", available)
	}
	// A claimed link is the claimer's; the sender may not take it back.
	if _, err := repo.CreateGiftLink(ctx, TokenBankGiftLink{
		ID: "link-2", Code: "code-2", SenderUserID: "sender", CreditsMicro: 5_000_000,
	}, GiftLinkPolicy{}, time.Now().UTC()); err != nil {
		t.Fatalf("CreateGiftLink(2) error = %v", err)
	}
	if _, err := repo.ClaimGiftLink(ctx, "code-2", "receiver", "r@example.com", time.Now().UTC()); err != nil {
		t.Fatalf("ClaimGiftLink() error = %v", err)
	}
	if err := repo.RevokeGiftLink(ctx, "link-2", "sender", time.Now().UTC()); !errors.Is(err, ErrGiftLinkNotActive) {
		t.Fatalf("RevokeGiftLink(claimed) error = %v, want ErrGiftLinkNotActive", err)
	}
}

func TestGiftExpireReleasesFreeze(t *testing.T) {
	repo, _ := newGiftTestRepo(t, "tbk-gift-expire.db")
	ctx := context.Background()
	seedEarnedFor(t, repo, "sender", "req-1", 10_000_000)

	past := time.Now().UTC().Add(-time.Hour)
	if _, err := repo.CreateGiftLink(ctx, TokenBankGiftLink{
		ID: "link-1", Code: "code-1", SenderUserID: "sender", CreditsMicro: 5_000_000,
		ExpiresAt: past,
	}, GiftLinkPolicy{}, past); err != nil {
		t.Fatalf("CreateGiftLink() error = %v", err)
	}
	// An expired link must refuse a claim.
	if _, err := repo.ClaimGiftLink(ctx, "code-1", "receiver", "r@example.com", time.Now().UTC()); !errors.Is(err, ErrGiftLinkExpired) {
		t.Fatalf("ClaimGiftLink(expired) error = %v, want ErrGiftLinkExpired", err)
	}

	n, err := repo.ExpireGiftLinks(ctx, time.Now().UTC())
	if err != nil {
		t.Fatalf("ExpireGiftLinks() error = %v", err)
	}
	if n != 1 {
		t.Fatalf("ExpireGiftLinks() expired = %d, want 1", n)
	}
	available, _ := repo.AvailableMicro(ctx, "sender")
	if available != 10_000_000 {
		t.Fatalf("available after expiry = %d, want 10000000", available)
	}
	// Running the sweeper again is a no-op, not a second release.
	n, err = repo.ExpireGiftLinks(ctx, time.Now().UTC())
	if err != nil || n != 0 {
		t.Fatalf("ExpireGiftLinks(second) = %d/%v, want 0/nil", n, err)
	}
	available, _ = repo.AvailableMicro(ctx, "sender")
	if available != 10_000_000 {
		t.Fatalf("available after double sweep = %d, want 10000000", available)
	}
}

func TestGiftUnfreezeShortfallDoesNotDriveFrozenNegative(t *testing.T) {
	repo, _ := newGiftTestRepo(t, "tbk-gift-shortfall.db")
	ctx := context.Background()
	seedEarnedFor(t, repo, "sender", "req-1", 10_000_000)
	if _, err := repo.CreateGiftLink(ctx, TokenBankGiftLink{
		ID: "link-1", Code: "code-1", SenderUserID: "sender", CreditsMicro: 5_000_000,
	}, GiftLinkPolicy{}, time.Now().UTC()); err != nil {
		t.Fatalf("CreateGiftLink() error = %v", err)
	}
	// Something else already consumed part of the frozen bucket. The unfreeze
	// may release only what is still frozen.
	if _, err := repo.write.ExecContext(ctx,
		`INSERT INTO token_bank_ledger (id, user_id, bucket, amount_micro, biz_key, ref_type, ref_id, note, created_at)
		 VALUES ('short-1', 'sender', ?, -4000000, 'short:link-1', 'test', 'link-1', '', ?)`,
		TokenBankBucketFrozen, time.Now().UTC().Format(time.RFC3339)); err != nil {
		t.Fatalf("insert shortfall: %v", err)
	}
	if err := repo.RevokeGiftLink(ctx, "link-1", "sender", time.Now().UTC()); err != nil {
		t.Fatalf("RevokeGiftLink() error = %v", err)
	}
	balance, err := repo.Balance(ctx, "sender")
	if err != nil {
		t.Fatal(err)
	}
	if balance.FrozenMicro != 0 {
		t.Fatalf("frozen = %d, want 0", balance.FrozenMicro)
	}
	if balance.FrozenMicro < 0 {
		t.Fatalf("frozen went negative: %d", balance.FrozenMicro)
	}
	link, err := repo.GiftLinkByID(ctx, "link-1")
	if err != nil {
		t.Fatal(err)
	}
	if link.Status != TokenBankGiftStatusRevoked {
		t.Fatalf("status = %s, want revoked", link.Status)
	}
	var note string
	if err := repo.write.QueryRowContext(ctx, `SELECT note FROM token_bank_ledger WHERE biz_key = ?`, "unfreeze:link-1").Scan(&note); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(note, "shortfall_micro=4000000") {
		t.Fatalf("unfreeze note = %q, want shortfall_micro=4000000", note)
	}
}

func TestGiftExpireClaimedUnwithdrawnReturnsFreeze(t *testing.T) {
	repo, _ := newGiftTestRepo(t, "tbk-gift-claimed-expire.db")
	ctx := context.Background()
	seedEarnedFor(t, repo, "sender", "req-1", 10_000_000)
	if _, err := repo.CreateGiftLink(ctx, TokenBankGiftLink{
		ID: "link-1", Code: "code-1", SenderUserID: "sender", CreditsMicro: 5_000_000,
	}, GiftLinkPolicy{}, time.Now().UTC()); err != nil {
		t.Fatalf("CreateGiftLink() error = %v", err)
	}
	if _, err := repo.ClaimGiftLink(ctx, "code-1", "receiver", "r@example.com", time.Now().UTC()); err != nil {
		t.Fatalf("ClaimGiftLink() error = %v", err)
	}
	past := time.Now().UTC().Add(-time.Minute).Format(time.RFC3339)
	if _, err := repo.write.ExecContext(ctx, `UPDATE credit_share_links SET expires_at = ? WHERE id = ?`, past, "link-1"); err != nil {
		t.Fatal(err)
	}
	n, err := repo.ExpireGiftLinks(ctx, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("expired = %d, want 1", n)
	}
	balance, err := repo.Balance(ctx, "sender")
	if err != nil {
		t.Fatal(err)
	}
	if balance.FrozenMicro != 0 || balance.GrantedMicro != 0 {
		t.Fatalf("sender frozen/granted = %d/%d, want 0/0", balance.FrozenMicro, balance.GrantedMicro)
	}
	if _, err := repo.SettleClaimedGift(ctx, "link-1", time.Now().UTC()); !errors.Is(err, ErrGiftLinkNotClaimed) {
		t.Fatalf("SettleClaimedGift() error = %v, want ErrGiftLinkNotClaimed", err)
	}
}

func TestGiftClaimRejectsOwnLink(t *testing.T) {
	repo, _ := newGiftTestRepo(t, "tbk-gift-own.db")
	ctx := context.Background()
	seedEarnedFor(t, repo, "sender", "req-1", 10_000_000)
	if _, err := repo.CreateGiftLink(ctx, TokenBankGiftLink{
		ID: "link-1", Code: "code-1", SenderUserID: "sender", CreditsMicro: 5_000_000,
	}, GiftLinkPolicy{}, time.Now().UTC()); err != nil {
		t.Fatalf("CreateGiftLink() error = %v", err)
	}
	if _, err := repo.ClaimGiftLink(ctx, "code-1", "sender", "s@example.com", time.Now().UTC()); !errors.Is(err, ErrGiftLinkOwnLink) {
		t.Fatalf("ClaimGiftLink(own) error = %v, want ErrGiftLinkOwnLink", err)
	}
}

func TestSettleGiftPaysUntilTheSweepReturnsIt(t *testing.T) {
	repo, _ := newGiftTestRepo(t, "tbk-gift-settle-before-sweep.db")
	ctx := context.Background()
	seedEarnedFor(t, repo, "sender", "req-1", 10_000_000)
	if _, err := repo.CreateGiftLink(ctx, TokenBankGiftLink{
		ID: "link-1", Code: "code-1", SenderUserID: "sender", CreditsMicro: 5_000_000,
	}, GiftLinkPolicy{}, time.Now().UTC()); err != nil {
		t.Fatalf("CreateGiftLink() error = %v", err)
	}
	if _, err := repo.ClaimGiftLink(ctx, "code-1", "receiver", "r@example.com", time.Now().UTC()); err != nil {
		t.Fatalf("ClaimGiftLink() error = %v", err)
	}
	past := time.Now().UTC().Add(-time.Minute)
	if _, err := repo.write.ExecContext(ctx, `UPDATE credit_share_links SET expires_at = ? WHERE id = ?`, past.Format(time.RFC3339), "link-1"); err != nil {
		t.Fatal(err)
	}
	listed, err := repo.ListClaimedGiftLinks(ctx, "receiver", time.Now().UTC(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].ID != "link-1" {
		t.Fatalf("claimed list = %+v, want the unreturned link", listed)
	}
	applied, err := repo.SettleClaimedGift(ctx, "link-1", time.Now().UTC())
	if err != nil || !applied {
		t.Fatalf("SettleClaimedGift(before sweep) applied=%v err=%v", applied, err)
	}
	sender, err := repo.Balance(ctx, "sender")
	if err != nil {
		t.Fatal(err)
	}
	if sender.FrozenMicro != 0 || sender.GrantedMicro != 5_000_000 {
		t.Fatalf("sender frozen/granted = %d/%d, want 0/5000000", sender.FrozenMicro, sender.GrantedMicro)
	}
	receiver, err := repo.Balance(ctx, "receiver")
	if err != nil {
		t.Fatal(err)
	}
	if receiver.ReceivedMicro != 5_000_000 {
		t.Fatalf("receiver received = %d, want 5000000", receiver.ReceivedMicro)
	}
}

// TestSettleGiftFinishesWhenTheReceiveRowArrivedFirst is the HA hole in the
// old short-circuit: a replicated receive row used to commit the unfreeze and
// grant and leave the link claimed, so the sweeper could still return the
// freeze after the receiver already had the credit.
func TestSettleGiftFinishesWhenTheReceiveRowArrivedFirst(t *testing.T) {
	repo, _ := newGiftTestRepo(t, "tbk-gift-receive-first.db")
	ctx := context.Background()
	seedEarnedFor(t, repo, "sender", "req-1", 10_000_000)
	if _, err := repo.CreateGiftLink(ctx, TokenBankGiftLink{
		ID: "link-1", Code: "code-1", SenderUserID: "sender", SenderEmail: "s@example.com", CreditsMicro: 5_000_000,
	}, GiftLinkPolicy{}, time.Now().UTC()); err != nil {
		t.Fatalf("CreateGiftLink() error = %v", err)
	}
	if _, err := repo.ClaimGiftLink(ctx, "code-1", "receiver", "r@example.com", time.Now().UTC()); err != nil {
		t.Fatalf("ClaimGiftLink() error = %v", err)
	}
	if _, err := repo.AppendLedger(ctx, TokenBankLedgerEntry{
		ID:          giftLedgerID("rcv", "receive:link-1"),
		UserID:      "receiver",
		Bucket:      TokenBankBucketReceived,
		AmountMicro: 5_000_000,
		BizKey:      "receive:link-1",
		RefType:     "gift_link",
		RefID:       "link-1",
		Note:        "s@example.com",
		CreatedAt:   time.Now().UTC(),
	}); err != nil {
		t.Fatalf("AppendLedger(receive) error = %v", err)
	}

	applied, err := repo.SettleClaimedGift(ctx, "link-1", time.Now().UTC())
	if err != nil || !applied {
		t.Fatalf("SettleClaimedGift() applied=%v err=%v", applied, err)
	}
	var status string
	if err := repo.write.QueryRowContext(ctx, `SELECT status FROM credit_share_links WHERE id = 'link-1'`).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != TokenBankGiftStatusSettled {
		t.Fatalf("status = %q, want settled", status)
	}
	sender, err := repo.Balance(ctx, "sender")
	if err != nil {
		t.Fatal(err)
	}
	if sender.FrozenMicro != 0 || sender.GrantedMicro != 5_000_000 {
		t.Fatalf("sender frozen/granted = %d/%d, want 0/5000000", sender.FrozenMicro, sender.GrantedMicro)
	}
	receiver, err := repo.Balance(ctx, "receiver")
	if err != nil {
		t.Fatal(err)
	}
	if receiver.ReceivedMicro != 5_000_000 {
		t.Fatalf("receiver received = %d, want 5000000", receiver.ReceivedMicro)
	}

	applied, err = repo.SettleClaimedGift(ctx, "link-1", time.Now().UTC())
	if err != nil || applied {
		t.Fatalf("replay applied=%v err=%v, want false, nil", applied, err)
	}
	past := time.Now().UTC().Add(-time.Minute).Format(time.RFC3339)
	if _, err := repo.write.ExecContext(ctx, `UPDATE credit_share_links SET expires_at = ? WHERE id = ?`, past, "link-1"); err != nil {
		t.Fatal(err)
	}
	n, err := repo.ExpireGiftLinks(ctx, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("ExpireGiftLinks() = %d, want 0 after the link is settled", n)
	}
	if err := repo.write.QueryRowContext(ctx, `SELECT status FROM credit_share_links WHERE id = 'link-1'`).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != TokenBankGiftStatusSettled {
		t.Fatalf("status after sweep = %q, want settled", status)
	}
	sender, err = repo.Balance(ctx, "sender")
	if err != nil {
		t.Fatal(err)
	}
	if sender.FrozenMicro != 0 || sender.GrantedMicro != 5_000_000 {
		t.Fatalf("sender after sweep frozen/granted = %d/%d, want 0/5000000", sender.FrozenMicro, sender.GrantedMicro)
	}
}

func TestGiftWithdrawDebitsTheLinkAmountOnly(t *testing.T) {
	repo, _ := newGiftTestRepo(t, "tbk-gift-amount.db")
	ctx := context.Background()
	seedEarnedFor(t, repo, "sender", "req-sender", 20_000_000)
	seedEarnedFor(t, repo, "receiver", "req-receiver", 10_000_000)
	if _, err := repo.CreateGiftLink(ctx, TokenBankGiftLink{
		ID: "link-1", Code: "code-1", SenderUserID: "sender", CreditsMicro: 5_000_000,
	}, GiftLinkPolicy{}, time.Now().UTC()); err != nil {
		t.Fatalf("CreateGiftLink() error = %v", err)
	}
	if _, err := repo.ClaimGiftLink(ctx, "code-1", "receiver", "r@example.com", time.Now().UTC()); err != nil {
		t.Fatalf("ClaimGiftLink() error = %v", err)
	}
	if _, _, err := repo.Withdraw(ctx, TokenBankWithdrawRequest{
		RequestID: "gift-early", UserID: "receiver", HubID: "hub-1",
		AmountMicro: 5_000_000, Manual: true, Kind: "gift", LinkID: "link-1",
	}); !errors.Is(err, ErrGiftLinkNotClaimed) {
		t.Fatalf("Withdraw before settle error = %v, want ErrGiftLinkNotClaimed", err)
	}
	if _, err := repo.SettleClaimedGift(ctx, "link-1", time.Now().UTC()); err != nil {
		t.Fatalf("SettleClaimedGift() error = %v", err)
	}
	// 9_000_000 is more than the gift and less than the receiver's balance.
	// 0 would otherwise mean the whole available balance.
	got, created, err := repo.Withdraw(ctx, TokenBankWithdrawRequest{
		RequestID: "gift-over", UserID: "receiver", HubID: "hub-1",
		AmountMicro: 9_000_000, Manual: true, Kind: "gift", LinkID: "link-1",
	})
	if err != nil || !created || got.AmountMicro != 5_000_000 {
		t.Fatalf("gift withdraw created=%v amount=%d err=%v, want 5000000", created, got.AmountMicro, err)
	}
	receiver, err := repo.Balance(ctx, "receiver")
	if err != nil {
		t.Fatal(err)
	}
	if receiver.WithdrawnMicro != 5_000_000 || receiver.AvailableMicro() != 10_000_000 {
		t.Fatalf("receiver withdrawn/available = %d/%d, want 5000000/10000000", receiver.WithdrawnMicro, receiver.AvailableMicro())
	}

	if _, err := repo.CreateGiftLink(ctx, TokenBankGiftLink{
		ID: "link-2", Code: "code-2", SenderUserID: "sender", CreditsMicro: 4_000_000,
	}, GiftLinkPolicy{}, time.Now().UTC()); err != nil {
		t.Fatalf("CreateGiftLink(2) error = %v", err)
	}
	if _, err := repo.ClaimGiftLink(ctx, "code-2", "receiver", "r@example.com", time.Now().UTC()); err != nil {
		t.Fatalf("ClaimGiftLink(2) error = %v", err)
	}
	if _, err := repo.SettleClaimedGift(ctx, "link-2", time.Now().UTC()); err != nil {
		t.Fatalf("SettleClaimedGift(2) error = %v", err)
	}
	// Zero is the self-withdrawal "take the cap" signal. On a gift it must
	// still be the link amount, not the remaining balance.
	got, created, err = repo.Withdraw(ctx, TokenBankWithdrawRequest{
		RequestID: "gift-zero", UserID: "receiver", HubID: "hub-1",
		AmountMicro: 0, Manual: true, Kind: "gift", LinkID: "link-2",
	})
	if err != nil || !created || got.AmountMicro != 4_000_000 {
		t.Fatalf("zero gift withdraw created=%v amount=%d err=%v, want 4000000", created, got.AmountMicro, err)
	}
	receiver, err = repo.Balance(ctx, "receiver")
	if err != nil {
		t.Fatal(err)
	}
	if receiver.WithdrawnMicro != 9_000_000 || receiver.AvailableMicro() != 10_000_000 {
		t.Fatalf("receiver withdrawn/available = %d/%d, want 9000000/10000000", receiver.WithdrawnMicro, receiver.AvailableMicro())
	}
}

func TestGiftWithdrawSecondRequestDoesNotDebitAgain(t *testing.T) {
	repo, _ := newGiftTestRepo(t, "tbk-gift-withdraw-once.db")
	ctx := context.Background()
	seedEarnedFor(t, repo, "sender", "req-sender", 20_000_000)
	seedEarnedFor(t, repo, "receiver", "req-receiver", 10_000_000)
	if _, err := repo.CreateGiftLink(ctx, TokenBankGiftLink{
		ID: "link-1", Code: "code-1", SenderUserID: "sender", CreditsMicro: 5_000_000,
	}, GiftLinkPolicy{}, time.Now().UTC()); err != nil {
		t.Fatalf("CreateGiftLink() error = %v", err)
	}
	if _, err := repo.ClaimGiftLink(ctx, "code-1", "receiver", "r@example.com", time.Now().UTC()); err != nil {
		t.Fatalf("ClaimGiftLink() error = %v", err)
	}
	if _, err := repo.SettleClaimedGift(ctx, "link-1", time.Now().UTC()); err != nil {
		t.Fatalf("SettleClaimedGift() error = %v", err)
	}
	first, created, err := repo.Withdraw(ctx, TokenBankWithdrawRequest{
		RequestID: "gift-a", UserID: "receiver", HubID: "hub-1",
		AmountMicro: 5_000_000, Manual: true, Kind: "gift", LinkID: "link-1",
	})
	if err != nil || !created || first.AmountMicro != 5_000_000 {
		t.Fatalf("first withdraw created=%v amount=%d err=%v", created, first.AmountMicro, err)
	}
	replay, created, err := repo.Withdraw(ctx, TokenBankWithdrawRequest{
		RequestID: "gift-a", UserID: "receiver", HubID: "hub-1",
		AmountMicro: 5_000_000, Manual: true, Kind: "gift", LinkID: "link-1",
	})
	if err != nil || created || replay.ID != first.ID {
		t.Fatalf("replay created=%v id=%s err=%v", created, replay.ID, err)
	}
	_, _, err = repo.Withdraw(ctx, TokenBankWithdrawRequest{
		RequestID: "gift-b", UserID: "receiver", HubID: "hub-1",
		AmountMicro: 5_000_000, Manual: true, Kind: "gift", LinkID: "link-1",
	})
	if !errors.Is(err, ErrTokenBankGiftWithdrawn) {
		t.Fatalf("second request error = %v, want ErrTokenBankGiftWithdrawn", err)
	}
	receiver, err := repo.Balance(ctx, "receiver")
	if err != nil {
		t.Fatal(err)
	}
	// Own 10 plus the gift 5, minus the one gift debit 5.
	if got := receiver.AvailableMicro(); got != 10_000_000 {
		t.Fatalf("receiver available = %d, want 10000000", got)
	}
}

func TestSettleGiftRequiresAClaim(t *testing.T) {
	repo, _ := newGiftTestRepo(t, "tbk-gift-unclaimed.db")
	ctx := context.Background()
	seedEarnedFor(t, repo, "sender", "req-1", 10_000_000)
	if _, err := repo.CreateGiftLink(ctx, TokenBankGiftLink{
		ID: "link-1", Code: "code-1", SenderUserID: "sender", CreditsMicro: 5_000_000,
	}, GiftLinkPolicy{}, time.Now().UTC()); err != nil {
		t.Fatalf("CreateGiftLink() error = %v", err)
	}
	if _, err := repo.SettleClaimedGift(ctx, "link-1", time.Now().UTC()); !errors.Is(err, ErrGiftLinkNotClaimed) {
		t.Fatalf("SettleClaimedGift(unclaimed) error = %v, want ErrGiftLinkNotClaimed", err)
	}
}

func TestListGiftLinksFiltersByStatus(t *testing.T) {
	repo, _ := newGiftTestRepo(t, "tbk-gift-list.db")
	ctx := context.Background()
	seedEarnedFor(t, repo, "sender", "req-1", 10_000_000)
	for i, code := range []string{"code-1", "code-2"} {
		if _, err := repo.CreateGiftLink(ctx, TokenBankGiftLink{
			ID: "link-" + code, Code: code, SenderUserID: "sender", CreditsMicro: 2_000_000,
		}, GiftLinkPolicy{}, time.Now().UTC().Add(time.Duration(i)*time.Second)); err != nil {
			t.Fatalf("CreateGiftLink(%s) error = %v", code, err)
		}
	}
	if err := repo.RevokeGiftLink(ctx, "link-code-2", "sender", time.Now().UTC()); err != nil {
		t.Fatalf("RevokeGiftLink() error = %v", err)
	}

	active, err := repo.ListGiftLinks(ctx, "sender", TokenBankGiftStatusActive, 10)
	if err != nil {
		t.Fatalf("ListGiftLinks(active) error = %v", err)
	}
	if len(active) != 1 || active[0].Code != "code-1" {
		t.Fatalf("active links = %+v, want only code-1", active)
	}
	all, err := repo.ListGiftLinks(ctx, "sender", "", 10)
	if err != nil {
		t.Fatalf("ListGiftLinks(all) error = %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("all links = %d, want 2", len(all))
	}
	if all[0].Code != "code-2" {
		t.Fatalf("links should be newest first, got %q", all[0].Code)
	}
}

// --- configured anti-abuse limits (§5) --------------------------------------
//
// These four settings are editable in the admin form and persisted, but until
// now the create path ignored them. A setting that is stored and never read is
// worse than no setting at all: the admin believes the limit is 10 and the
// operator's evidence says otherwise.

func TestGiftConfiguredRatioReplacesTheBuiltInHalf(t *testing.T) {
	if got := GiftShareCapMicro(10_000_000, GiftLinkPolicy{MaxRatio: 0.25}); got != 2_500_000 {
		t.Fatalf("cap at 25%% = %d, want 2500000", got)
	}
	// The default path must be unchanged for a policy that says nothing: half of
	// ten credits is still five.
	if got := GiftShareCapMicro(10_000_000, GiftLinkPolicy{}); got != 5_000_000 {
		t.Fatalf("cap without a configured ratio = %d, want 5000000", got)
	}
	// Floored, not rounded: 25% of an odd microcredit must not round up into a
	// cap the balance cannot cover.
	if got := GiftShareCapMicro(3, GiftLinkPolicy{MaxRatio: 0.25}); got != 0 {
		t.Fatalf("cap of 3 at 25%% = %d, want 0 (must round down)", got)
	}
}

func TestGiftBelowConfiguredFloorIsRefused(t *testing.T) {
	repo, _ := newGiftTestRepo(t, "tbk-gift-floor.db")
	ctx := context.Background()
	seedEarnedFor(t, repo, "sender", "req-1", 10_000_000)

	// 0.5 credits is well within the 50% cap, so only the floor can refuse it.
	if _, err := repo.CreateGiftLink(ctx, TokenBankGiftLink{
		ID: "link-dust", Code: "code-dust", SenderUserID: "sender", CreditsMicro: 500_000,
	}, GiftLinkPolicy{MinMicro: 1_000_000}, time.Now().UTC()); !errors.Is(err, ErrGiftLinkBelowFloor) {
		t.Fatalf("CreateGiftLink(dust) error = %v, want ErrGiftLinkBelowFloor", err)
	}

	// The floor must not have frozen anything: a rejected create opens a
	// transaction, and leaving a freeze row behind would silently eat the
	// sender's available balance.
	if available, _ := repo.AvailableMicro(ctx, "sender"); available != 10_000_000 {
		t.Fatalf("available after refused dust = %d, want 10000000 (no freeze)", available)
	}

	// Exactly at the floor is allowed.
	if _, err := repo.CreateGiftLink(ctx, TokenBankGiftLink{
		ID: "link-exact", Code: "code-exact", SenderUserID: "sender", CreditsMicro: 1_000_000,
	}, GiftLinkPolicy{MinMicro: 1_000_000}, time.Now().UTC()); err != nil {
		t.Fatalf("CreateGiftLink(at floor) error = %v", err)
	}
}

func TestGiftDailyLimitStopsTheNthLink(t *testing.T) {
	repo, _ := newGiftTestRepo(t, "tbk-gift-daily.db")
	ctx := context.Background()
	seedEarnedFor(t, repo, "sender", "req-1", 100_000_000)
	policy := GiftLinkPolicy{DailyLimit: 2}

	now := time.Now().UTC()
	for i := 0; i < 2; i++ {
		if _, err := repo.CreateGiftLink(ctx, TokenBankGiftLink{
			ID: "link-" + string(rune('a'+i)), Code: "code-" + string(rune('a'+i)),
			SenderUserID: "sender", CreditsMicro: 1_000_000,
		}, policy, now.Add(time.Duration(i)*time.Second)); err != nil {
			t.Fatalf("CreateGiftLink(%d) error = %v", i, err)
		}
	}
	if _, err := repo.CreateGiftLink(ctx, TokenBankGiftLink{
		ID: "link-c", Code: "code-c", SenderUserID: "sender", CreditsMicro: 1_000_000,
	}, policy, now.Add(3*time.Second)); !errors.Is(err, ErrGiftLinkRateLimited) {
		t.Fatalf("CreateGiftLink(3rd) error = %v, want ErrGiftLinkRateLimited", err)
	}

	// The limit is per-user: somebody else is untouched by it.
	seedEarnedFor(t, repo, "other", "req-2", 100_000_000)
	if _, err := repo.CreateGiftLink(ctx, TokenBankGiftLink{
		ID: "link-other", Code: "code-other", SenderUserID: "other", CreditsMicro: 1_000_000,
	}, policy, now); err != nil {
		t.Fatalf("CreateGiftLink(other user) error = %v, want it to pass", err)
	}
}

func TestGiftDailyLimitCountsRevokedLinks(t *testing.T) {
	// Regression: counting only *live* links would make the cap escapable by
	// create→revoke→create. The limit bounds how many links are minted, not how
	// many are currently outstanding.
	repo, _ := newGiftTestRepo(t, "tbk-gift-daily-revoked.db")
	ctx := context.Background()
	seedEarnedFor(t, repo, "sender", "req-1", 100_000_000)
	policy := GiftLinkPolicy{DailyLimit: 2}
	now := time.Now().UTC()

	if _, err := repo.CreateGiftLink(ctx, TokenBankGiftLink{
		ID: "link-1", Code: "code-1", SenderUserID: "sender", CreditsMicro: 1_000_000,
	}, policy, now); err != nil {
		t.Fatalf("CreateGiftLink(1) error = %v", err)
	}
	if err := repo.RevokeGiftLink(ctx, "link-1", "sender", now.Add(time.Second)); err != nil {
		t.Fatalf("RevokeGiftLink() error = %v", err)
	}
	if _, err := repo.CreateGiftLink(ctx, TokenBankGiftLink{
		ID: "link-2", Code: "code-2", SenderUserID: "sender", CreditsMicro: 1_000_000,
	}, policy, now.Add(2*time.Second)); err != nil {
		t.Fatalf("CreateGiftLink(2) error = %v", err)
	}
	// The revoked one still counts, so this is the third mint of the day.
	if _, err := repo.CreateGiftLink(ctx, TokenBankGiftLink{
		ID: "link-3", Code: "code-3", SenderUserID: "sender", CreditsMicro: 1_000_000,
	}, policy, now.Add(3*time.Second)); !errors.Is(err, ErrGiftLinkRateLimited) {
		t.Fatalf("CreateGiftLink(3rd after revoke) error = %v, want ErrGiftLinkRateLimited", err)
	}
}

func TestGiftConfiguredTTLIsApplied(t *testing.T) {
	repo, _ := newGiftTestRepo(t, "tbk-gift-ttl.db")
	ctx := context.Background()
	seedEarnedFor(t, repo, "sender", "req-1", 10_000_000)

	now := time.Now().UTC()
	link, err := repo.CreateGiftLink(ctx, TokenBankGiftLink{
		ID: "link-1", Code: "code-1", SenderUserID: "sender", CreditsMicro: 1_000_000,
	}, GiftLinkPolicy{TTL: 72 * time.Hour}, now)
	if err != nil {
		t.Fatalf("CreateGiftLink() error = %v", err)
	}
	want := now.Add(72 * time.Hour)
	if delta := link.ExpiresAt.Sub(want); delta > time.Minute || delta < -time.Minute {
		t.Fatalf("expires_at = %v, want ~%v", link.ExpiresAt, want)
	}

	// A zero TTL falls back to the built-in week rather than expiring instantly,
	// which is what a settings blob predating the field must do.
	link2, err := repo.CreateGiftLink(ctx, TokenBankGiftLink{
		ID: "link-2", Code: "code-2", SenderUserID: "sender", CreditsMicro: 1_000_000,
	}, GiftLinkPolicy{}, now)
	if err != nil {
		t.Fatalf("CreateGiftLink(default ttl) error = %v", err)
	}
	if delta := link2.ExpiresAt.Sub(now.Add(TokenBankGiftLinkTTL)); delta > time.Minute || delta < -time.Minute {
		t.Fatalf("default expires_at = %v, want ~%v", link2.ExpiresAt, now.Add(TokenBankGiftLinkTTL))
	}
}

func TestStartOfUTCDayIsMidnightUTC(t *testing.T) {
	// The boundary must be UTC, not the server's local zone: a local-zone day
	// boundary would let a user get two days' worth of links by straddling
	// midnight, and the stored created_at is UTC RFC3339.
	in := time.Date(2026, 10, 1, 23, 59, 59, 0, time.FixedZone("UTC+8", 8*3600))
	got := StartOfUTCDay(in)
	if got.UTC().Hour() != 0 || got.Location() != time.UTC {
		t.Fatalf("StartOfUTCDay(%v) = %v, want midnight UTC", in, got)
	}
	wantDate := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	if !got.Equal(wantDate) {
		t.Fatalf("StartOfUTCDay(%v) = %v, want %v (UTC date, not local)", in, got, wantDate)
	}
}
