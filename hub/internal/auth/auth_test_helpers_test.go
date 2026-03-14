package auth

import (
	"testing"

	"github.com/RapidAI/CodeClaw/hub/internal/store"
	"github.com/RapidAI/CodeClaw/hub/internal/store/sqlite"
)

type testDeps struct {
	store    *store.Store
	provider *sqlite.Provider
}

func newTestStore(t *testing.T) *testDeps {
	t.Helper()

	dbPath := t.TempDir() + `\hub-test.db`
	provider, err := sqlite.NewProvider(sqlite.Config{
		DSN:               dbPath,
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

	if err := sqlite.RunMigrations(provider.Write); err != nil {
		t.Fatalf("run migrations: %v", err)
	}

	t.Cleanup(func() {
		_ = provider.Close()
	})

	return &testDeps{
		store:    sqlite.NewStore(provider),
		provider: provider,
	}
}
