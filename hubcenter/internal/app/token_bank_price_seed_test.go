package app

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/ha"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/store/sqlite"
)

// The live price book sink only fires on writes made after startup, so a rule
// configured while the node ran standalone would never reach the peers. The
// seed pass must republish exactly the rules that are not yet in the oplog,
// and stay silent on repeat runs.
func TestSeedTokenBankPriceBookHAOpsOnlySeedsMissingRules(t *testing.T) {
	provider, err := sqlite.NewProvider(sqlite.Config{DSN: filepath.Join(t.TempDir(), "ha-price-seed.db"), WAL: false})
	if err != nil {
		t.Fatalf("NewProvider() error = %v", err)
	}
	t.Cleanup(func() { _ = provider.Close() })
	if err := sqlite.RunMigrations(provider.Write); err != nil {
		t.Fatalf("RunMigrations() error = %v", err)
	}
	if err := sqlite.EnsureLLMTables(provider.Write); err != nil {
		t.Fatalf("EnsureLLMTables() error = %v", err)
	}
	st := sqlite.NewStore(provider)
	svc := ha.NewService("hc-1", "hc-1", "https://hc-1.example.com", "secret", nil)
	svc.AttachStore(st)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	repo := sqlite.NewTokenBankRepo(provider)
	seeded := sqlite.TokenBankPriceRule{ID: "tbkp-seeded", ModelPattern: "seeded-*", UnitInputPer10K: 1, UpdatedAt: now}
	missing := sqlite.TokenBankPriceRule{ID: "tbkp-missing", ModelPattern: "missing-*", UnitInputPer10K: 2, UpdatedAt: now}
	for _, rule := range []sqlite.TokenBankPriceRule{seeded, missing} {
		if _, err := repo.UpsertPriceRule(ctx, rule, now); err != nil {
			t.Fatalf("UpsertPriceRule(%s) error = %v", rule.ID, err)
		}
	}
	// The first rule was already replicated (the live sink appended its op).
	if err := svc.AppendTokenBankPriceRule(ctx, &seeded); err != nil {
		t.Fatalf("AppendTokenBankPriceRule(pre) error = %v", err)
	}

	countOps := func() int {
		ops, err := st.HASyncOps.ListAfterSeq(ctx, 0, 0)
		if err != nil {
			t.Fatalf("ListAfterSeq error = %v", err)
		}
		n := 0
		for _, op := range ops {
			if op.EntityType == ha.EntityTokenBankPriceBook {
				n++
			}
		}
		return n
	}
	if got := countOps(); got != 1 {
		t.Fatalf("price book ops before seed = %d, want 1", got)
	}

	seedTokenBankPriceBookHAOps(ctx, svc, repo)
	if got := countOps(); got != 2 {
		t.Fatalf("price book ops after seed = %d, want 2", got)
	}
	ops, err := st.HASyncOps.ListAfterSeq(ctx, 0, 0)
	if err != nil {
		t.Fatalf("ListAfterSeq(after) error = %v", err)
	}
	last := ops[len(ops)-1]
	if last.EntityID != missing.ID {
		t.Fatalf("seeded entity = %s, want %s", last.EntityID, missing.ID)
	}

	// A second pass is a no-op: every rule already has an entity version.
	seedTokenBankPriceBookHAOps(ctx, svc, repo)
	if got := countOps(); got != 2 {
		t.Fatalf("price book ops after second seed = %d, want 2", got)
	}
}

func TestSeedTokenBankPriceBookHAOpsToleratesEmptyBook(t *testing.T) {
	provider, err := sqlite.NewProvider(sqlite.Config{DSN: filepath.Join(t.TempDir(), "ha-price-seed-empty.db"), WAL: false})
	if err != nil {
		t.Fatalf("NewProvider() error = %v", err)
	}
	t.Cleanup(func() { _ = provider.Close() })
	if err := sqlite.RunMigrations(provider.Write); err != nil {
		t.Fatalf("RunMigrations() error = %v", err)
	}
	svc := ha.NewService("hc-1", "hc-1", "https://hc-1.example.com", "secret", nil)
	svc.AttachStore(sqlite.NewStore(provider))

	// Nil repo and empty book must both be silent no-ops.
	seedTokenBankPriceBookHAOps(context.Background(), svc, nil)
	seedTokenBankPriceBookHAOps(context.Background(), svc, sqlite.NewTokenBankRepo(provider))
}
