package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

// TestRunMigrationsAddsCacheSettlementColumnsWithoutBackfill simulates an
// upgrade from the pre-cache-v1 ledger shape: the ten directional columns are
// appended as nullable columns and the existing row keeps them NULL (design
// §9.2 forbids backfilling historical rows with recalculated values).
func TestRunMigrationsAddsCacheSettlementColumnsWithoutBackfill(t *testing.T) {
	provider, err := NewProvider(Config{
		DSN:               filepath.Join(t.TempDir(), "hub-ledger-migration.db"),
		WAL:               true,
		BusyTimeoutMS:     5000,
		MaxReadOpenConns:  4,
		MaxReadIdleConns:  2,
		MaxWriteOpenConns: 1,
		MaxWriteIdleConns: 1,
	})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	defer provider.Close()

	db := provider.Write
	legacySchema := `CREATE TABLE llm_billing_ledger (
		tenant_id TEXT NOT NULL DEFAULT 'tenant_default',
		request_id TEXT NOT NULL,
		user_id TEXT NOT NULL DEFAULT '',
		email TEXT NOT NULL DEFAULT '',
		provider_id TEXT NOT NULL DEFAULT '',
		service_group_ids_json TEXT NOT NULL DEFAULT '[]',
		input_tokens INTEGER NOT NULL DEFAULT 0,
		output_tokens INTEGER NOT NULL DEFAULT 0,
		requested_microcredits INTEGER NOT NULL DEFAULT 0,
		deducted_microcredits INTEGER NOT NULL DEFAULT 0,
		provider_multiplier REAL NOT NULL DEFAULT 1,
		billing_group_multiplier REAL NOT NULL DEFAULT 1,
		pricing_json TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL,
		PRIMARY KEY (tenant_id, request_id)
	);`
	if _, err := db.Exec(legacySchema); err != nil {
		t.Fatalf("create legacy ledger: %v", err)
	}
	now := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC).Format(time.RFC3339)
	if _, err := db.Exec(`INSERT INTO llm_billing_ledger (tenant_id, request_id, input_tokens, output_tokens, deducted_microcredits, created_at)
		VALUES ('tenant_default', 'legacy-request', 100, 50, 1000000, ?)`, now); err != nil {
		t.Fatalf("insert legacy row: %v", err)
	}

	if err := RunMigrations(db); err != nil {
		t.Fatalf("RunMigrations: %v", err)
	}

	columns := []string{
		"cached_input_tokens", "cache_write_tokens",
		"normal_input_credits", "cache_read_credits", "cache_write_credits", "output_credits",
		"normal_input_cost_rmb", "cache_read_cost_rmb", "cache_write_cost_rmb", "output_cost_rmb",
	}
	rows, err := db.Query(`SELECT cached_input_tokens, cache_write_tokens, normal_input_credits, cache_read_credits,
		cache_write_credits, output_credits, normal_input_cost_rmb, cache_read_cost_rmb, cache_write_cost_rmb, output_cost_rmb
		FROM llm_billing_ledger WHERE tenant_id = 'tenant_default' AND request_id = 'legacy-request'`)
	if err != nil {
		t.Fatalf("select migrated legacy row: %v", err)
	}
	// Close before any further statement: the write pool allows a single
	// connection, and an open result set would starve the transaction below.
	if !rows.Next() {
		rows.Close()
		t.Fatal("legacy row lost during migration")
	}
	values := make([]any, len(columns))
	dest := make([]any, len(columns))
	for i := range values {
		dest[i] = &values[i]
	}
	if err := rows.Scan(dest...); err != nil {
		rows.Close()
		t.Fatalf("scan migrated legacy row: %v", err)
	}
	for i, column := range columns {
		if values[i] != nil {
			rows.Close()
			t.Fatalf("legacy row column %s = %v, want NULL (no backfill)", column, values[i])
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		t.Fatalf("iterate migrated legacy row: %v", err)
	}
	rows.Close()

	// The upgraded table accepts new directional settlements.
	repo := NewLLMBillingLedgerRepository(db, nil)
	inserted, err := repo.RecordSettlement(context.Background(), &store.LLMBillingSettlement{
		TenantID:               store.DefaultTenantID,
		RequestID:              "post-upgrade-request",
		InputTokens:            10,
		OutputTokens:           5,
		DeductedMicrocredits:   1000,
		CachedInputTokens:      int64PtrForTest(4),
		CacheWriteTokens:       int64PtrForTest(0),
		NormalInputCredits:     float64PtrForTest(0.001),
		CacheReadCredits:       float64PtrForTest(0),
		CacheWriteCredits:      float64PtrForTest(0),
		OutputCredits:          float64PtrForTest(0),
		BillingGroupMultiplier: 1,
		CreatedAt:              time.Now().UTC(),
	})
	if err != nil || !inserted {
		t.Fatalf("record post-upgrade settlement: inserted=%v err=%v", inserted, err)
	}

	// Read the inserted row back: the cache legs persist as real usage facts
	// and the directional amounts keep even their explicit zeros.
	var (
		cachedLeg, writeLeg                                  int64
		normalCredits, readCredits, writeCredits, outCredits float64
	)
	err = db.QueryRow(`SELECT cached_input_tokens, cache_write_tokens, normal_input_credits,
		cache_read_credits, cache_write_credits, output_credits
		FROM llm_billing_ledger WHERE tenant_id = 'tenant_default' AND request_id = 'post-upgrade-request'`).
		Scan(&cachedLeg, &writeLeg, &normalCredits, &readCredits, &writeCredits, &outCredits)
	if err != nil {
		t.Fatalf("read back post-upgrade settlement: %v", err)
	}
	if cachedLeg != 4 || writeLeg != 0 || normalCredits != 0.001 || readCredits != 0 || writeCredits != 0 || outCredits != 0 {
		t.Fatalf("post-upgrade row = legs %d/%d credits %v/%v/%v/%v, want 4/0 and 0.001/0/0/0",
			cachedLeg, writeLeg, normalCredits, readCredits, writeCredits, outCredits)
	}
}

func TestIsIgnorableMigrationError(t *testing.T) {
	cases := []struct {
		name string
		stmt string
		err  error
		want bool
	}{
		{"no error is ignorable", "ALTER TABLE t ADD COLUMN c INTEGER", nil, true},
		{"duplicate column on alter", "ALTER TABLE t ADD COLUMN c INTEGER", errors.New("duplicate column name: c"), true},
		{"duplicate column on non-alter", "CREATE TABLE t (c INTEGER)", errors.New("duplicate column name: c"), false},
		{"existing object with if-not-exists", "CREATE INDEX IF NOT EXISTS idx_t ON t(c)", errors.New("index idx_t already exists"), true},
		{"existing object without opt-in", "CREATE INDEX idx_t ON t(c)", errors.New("index idx_t already exists"), false},
		{"unrelated error", "ALTER TABLE t ADD COLUMN c INTEGER", errors.New("no such table: t"), false},
	}
	for _, tc := range cases {
		if got := isIgnorableMigrationError(tc.stmt, tc.err); got != tc.want {
			t.Fatalf("%s: isIgnorableMigrationError(%q, %v) = %v, want %v", tc.name, tc.stmt, tc.err, got, tc.want)
		}
	}
}
