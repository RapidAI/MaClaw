package main

import (
	"context"
	"crypto/sha256"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
)

func newThirdPartyAuthCacheFixture(tb testing.TB, userCount int) (*agentservice.Service, *HTTPServer, agentservice.Principal, string) {
	tb.Helper()
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: tb.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() { _ = svc.Close() })
	tenant, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		tb.Fatal(err)
	}
	var principal agentservice.Principal
	token := ""
	for i := 0; i < userCount; i++ {
		user, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User"})
		if err != nil {
			tb.Fatal(err)
		}
		p := agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}
		cfg := testLLMConfig()
		cfg.ThirdPartyGatewayEnabled = true
		cfg.ThirdPartyGatewayToken = "gateway-token-" + string(rune('a'+i))
		if _, err := svc.UpdateUserConfig(context.Background(), p, cfg); err != nil {
			tb.Fatal(err)
		}
		principal = p
		token = cfg.ThirdPartyGatewayToken
	}
	return svc, NewHTTPServer(svc, "admin-secret", nil), principal, token
}

func TestThirdPartyAuthCacheInvalidatesOnRotationAndDisable(t *testing.T) {
	_, server, principal, token := newThirdPartyAuthCacheFixture(t, 1)
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		if _, err := server.thirdPartyPrincipalByToken(ctx, token); err != nil {
			t.Fatal(err)
		}
	}
	server.thirdPartyAuthMu.RLock()
	cached := len(server.thirdPartyAuthCache)
	server.thirdPartyAuthMu.RUnlock()
	if cached != 1 {
		t.Fatalf("cached principals = %d, want 1", cached)
	}

	server.stopThirdPartyIMForPrincipal(principal)
	if _, err := server.thirdPartyPrincipalByToken(ctx, token); err != nil {
		t.Fatal(err)
	}
	server.thirdPartyAuthMu.RLock()
	cached = len(server.thirdPartyAuthCache)
	server.thirdPartyAuthMu.RUnlock()
	if cached != 1 {
		t.Fatalf("principal after reauthentication = %d, want 1", cached)
	}
	server.stopThirdPartyIMForTenant(principal.TenantID)
	server.thirdPartyAuthMu.RLock()
	cached = len(server.thirdPartyAuthCache)
	server.thirdPartyAuthMu.RUnlock()
	if cached != 0 {
		t.Fatalf("tenant stop retained %d cached principals", cached)
	}
}

func TestThirdPartyAuthCacheRejectsStalePopulation(t *testing.T) {
	_, server, _, _ := newThirdPartyAuthCacheFixture(t, 1)
	server.thirdPartyAuthMu.RLock()
	version := server.thirdPartyAuthVersion
	server.thirdPartyAuthMu.RUnlock()
	server.invalidateThirdPartyAuthCache()

	err := server.cacheThirdPartyPrincipal(sha256.Sum256([]byte("stale-token")), version, srvThirdPartyPrincipal{})
	if err == nil {
		t.Fatal("stale authentication result was cached after an invalidation")
	}
	server.thirdPartyAuthMu.RLock()
	cached := len(server.thirdPartyAuthCache)
	server.thirdPartyAuthMu.RUnlock()
	if cached != 0 {
		t.Fatalf("stale population left %d cache entries", cached)
	}
}

func BenchmarkThirdPartyPrincipalByToken(b *testing.B) {
	_, server, _, token := newThirdPartyAuthCacheFixture(b, 16)
	ctx := context.Background()
	b.Run("cold_scan", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			server.invalidateThirdPartyAuthCache()
			if _, err := server.thirdPartyPrincipalByToken(ctx, token); err != nil {
				b.Fatal(err)
			}
		}
	})
	if _, err := server.thirdPartyPrincipalByToken(ctx, token); err != nil {
		b.Fatal(err)
	}
	b.Run("cached", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			if _, err := server.thirdPartyPrincipalByToken(ctx, token); err != nil {
				b.Fatal(err)
			}
		}
	})
}
