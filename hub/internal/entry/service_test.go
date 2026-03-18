package entry

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/store/sqlite"
)

func newEntryTestService(t *testing.T) *Service {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "hub-entry-service-test.db")
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

	st := sqlite.NewStore(provider)
	identity := auth.NewIdentityService(st.Users, st.Enrollments, st.EmailBlocks, st.Machines, st.ViewerTokens, st.LoginTokens, st.System, nil, "open", true, nil, "http://127.0.0.1:8080")
	return NewService(identity, nil)
}

func TestProbeByEmailReturnsNotFound(t *testing.T) {
	svc := newEntryTestService(t)

	result, err := svc.ProbeByEmail(context.Background(), "missing@example.com")
	if err != nil {
		t.Fatalf("ProbeByEmail() error = %v", err)
	}
	if result.Status != "not_found" {
		t.Fatalf("Status = %q, want not_found", result.Status)
	}
	if result.Bound || result.CanLogin {
		t.Fatalf("unexpected result = %#v", result)
	}
}
