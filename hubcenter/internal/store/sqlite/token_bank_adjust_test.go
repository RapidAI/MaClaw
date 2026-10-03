package sqlite

import (
	"context"
	"path/filepath"
	"testing"
)

func TestTokenBankAdjustmentIsALedgerRowAndDoesNotApplyTwice(t *testing.T) {
	provider := newTokenBankTestProvider(t, filepath.Join(t.TempDir(), "tbk-adjust.db"))
	repo := newTokenBankTestRepo(t, provider)
	ctx := context.Background()
	seedEarned(t, repo, "user-adj", 2_000_000)
	applied, err := repo.AppendAdjustment(ctx, "user-adj", TokenBankBucketWithdrawn, 500_000, "adjust:reconcile:user-adj:1", "cross-end reconcile")
	if err != nil || !applied {
		t.Fatalf("AppendAdjustment() applied=%v err=%v", applied, err)
	}
	again, err := repo.AppendAdjustment(ctx, "user-adj", TokenBankBucketWithdrawn, 500_000, "adjust:reconcile:user-adj:1", "cross-end reconcile")
	if err != nil || again {
		t.Fatalf("replay applied=%v err=%v, want false", again, err)
	}
	balance, err := repo.Balance(ctx, "user-adj")
	if err != nil {
		t.Fatal(err)
	}
	if balance.WithdrawnMicro != 500_000 || balance.AvailableMicro() != 1_500_000 {
		t.Fatalf("balance = %+v, want withdrawn 500000 available 1500000", balance)
	}
}

func TestTokenBankReissueGrantIDUpdatesTheRecordedGrant(t *testing.T) {
	provider := newTokenBankTestProvider(t, filepath.Join(t.TempDir(), "tbk-reissue.db"))
	repo := newTokenBankTestRepo(t, provider)
	ctx := context.Background()
	seedEarned(t, repo, "user-re", 1_000_000)
	if _, _, err := repo.Withdraw(ctx, TokenBankWithdrawRequest{
		RequestID: "req-re", UserID: "user-re", HubID: "hub-1", Manual: true, AmountMicro: 1_000_000,
	}); err != nil {
		t.Fatal(err)
	}
	if err := repo.BindGrantID(ctx, "req-re", "hub-1", "grant-old"); err != nil {
		t.Fatal(err)
	}
	if err := repo.ReissueGrantID(ctx, "req-re", "hub-1", "grant-new"); err != nil {
		t.Fatal(err)
	}
	if err := repo.ReissueGrantID(ctx, "req-re", "hub-1", "grant-new"); err != nil {
		t.Fatalf("replay reissue: %v", err)
	}
	list, err := repo.ListWithdrawals(ctx, "user-re", "hub-1", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].GrantID != "grant-new" || list[0].Status != TokenBankWithdrawStatusReissued {
		t.Fatalf("withdrawal = %+v", list)
	}
	sum, err := repo.SumWithdrawalMicro(ctx, "user-re", "hub-1")
	if err != nil {
		t.Fatal(err)
	}
	if sum != 1_000_000 {
		t.Fatalf("sum = %d, want 1000000 (reissue must not debit)", sum)
	}
}
