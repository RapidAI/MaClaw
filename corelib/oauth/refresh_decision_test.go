package oauth

import (
	"context"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestShouldRefreshOAuthToken(t *testing.T) {
	valid := corelib.MaclawLLMProvider{AuthType: "oauth", RefreshToken: "rt", TokenExpiresAt: 1 << 40}
	expired := corelib.MaclawLLMProvider{AuthType: "oauth", RefreshToken: "rt", TokenExpiresAt: 1}
	noRefresh := corelib.MaclawLLMProvider{AuthType: "oauth", TokenExpiresAt: 1}
	apiKey := corelib.MaclawLLMProvider{AuthType: "api_key", RefreshToken: "rt", TokenExpiresAt: 1}

	if shouldRefreshOAuthToken(valid, false) {
		t.Fatal("unexpired oauth token should not refresh")
	}
	if !shouldRefreshOAuthToken(valid, true) {
		t.Fatal("force should refresh an unexpired oauth token")
	}
	if !shouldRefreshOAuthToken(expired, false) {
		t.Fatal("expired oauth token should refresh")
	}
	if shouldRefreshOAuthToken(noRefresh, true) {
		t.Fatal("force without refresh_token cannot refresh")
	}
	if shouldRefreshOAuthToken(apiKey, true) {
		t.Fatal("api_key providers are not oauth-refreshable")
	}
	cased := corelib.MaclawLLMProvider{AuthType: "OAuth", RefreshToken: "rt", TokenExpiresAt: 1 << 40}
	if !shouldRefreshOAuthToken(cased, true) {
		t.Fatal("AuthType OAuth should force-refresh")
	}
}

func TestRefreshXAITokenHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := RefreshXAIToken(ctx, "refresh-token")
	if err == nil {
		t.Fatal("expected canceled context to fail refresh")
	}
}
