package agentservice

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
)

func TestCheckMCPServerClassifiesOnlyProbeFailureAsJobRetryable(t *testing.T) {
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "temporarily unavailable", http.StatusServiceUnavailable)
	}))
	defer remote.Close()

	store := NewMemoryStore()
	svc, err := NewService(Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789abcdef"}, store, EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	_ = store.SaveTenant(Tenant{ID: "tenant", Name: "Tenant"})
	_ = store.SaveUser(User{TenantID: "tenant", ID: "user", Name: "User"})
	if err := store.SaveUserConfig(UserConfig{
		TenantID: "tenant",
		UserID:   "user",
		AppConfig: corelib.AppConfig{MCPServers: []corelib.MCPServerEntry{{
			ID: "mcp_remote", Name: "Remote", EndpointURL: remote.URL,
		}}},
	}); err != nil {
		t.Fatalf("SaveUserConfig: %v", err)
	}

	principal := Principal{TenantID: "tenant", UserID: "user"}
	if _, err := svc.CheckMCPServer(context.Background(), principal, "mcp_remote"); err == nil || !agentruntime.IsJobErrorRetryable(err) {
		t.Fatalf("remote probe error was not retryable: %v", err)
	}
	if _, err := svc.CheckMCPServer(context.Background(), principal, "missing"); err == nil || agentruntime.IsJobErrorRetryable(err) {
		t.Fatalf("lookup/configuration error became retryable: %v", err)
	}
}
