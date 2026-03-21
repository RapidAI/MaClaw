package oauth

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"pgregory.net/rapid"
)

// ─── Task 3.5 ───────────────────────────────────────────────────────────────
// Feature: openai-oauth-provider, Property 4: code_verifier 符合 RFC 7636
// **Validates: Requirements 3.1**
//
// For any call to GenerateCodeVerifier(), the result length must be in [43,128]
// and every character must belong to the unreserved set [A-Za-z0-9\-._~].
func TestProperty_CodeVerifier_RFC7636(t *testing.T) {
	re := regexp.MustCompile(`^[A-Za-z0-9\-._~]+$`)

	rapid.Check(t, func(t *rapid.T) {
		v, err := GenerateCodeVerifier()
		if err != nil {
			t.Fatalf("GenerateCodeVerifier error: %v", err)
		}
		if len(v) < 43 || len(v) > 128 {
			t.Fatalf("code_verifier length %d out of range [43,128]: %q", len(v), v)
		}
		if !re.MatchString(v) {
			t.Fatalf("code_verifier contains invalid characters: %q", v)
		}
	})
}

// ─── Task 3.6 ───────────────────────────────────────────────────────────────
// Feature: openai-oauth-provider, Property 5: code_challenge SHA256 Base64URL 确定性
// **Validates: Requirements 3.1, 3.2**
//
// For any verifier string, GenerateCodeChallenge is deterministic and equals
// base64url(sha256(verifier)) with no padding.
func TestProperty_CodeChallenge_Deterministic(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		verifier := rapid.StringMatching(`[A-Za-z0-9\-._~]{43,128}`).Draw(t, "verifier")

		c1 := GenerateCodeChallenge(verifier)
		c2 := GenerateCodeChallenge(verifier)
		if c1 != c2 {
			t.Fatalf("non-deterministic: %q vs %q for verifier %q", c1, c2, verifier)
		}

		// Manual computation
		h := sha256.Sum256([]byte(verifier))
		expected := base64.RawURLEncoding.EncodeToString(h[:])
		if c1 != expected {
			t.Fatalf("challenge mismatch: got %q, want %q", c1, expected)
		}
	})
}

// ─── Task 3.7 ───────────────────────────────────────────────────────────────
// Feature: openai-oauth-provider, Property 6: 授权 URL 包含所有必要参数
// **Validates: Requirements 3.2**
//
// For any valid Config, BuildAuthURL must produce a URL containing client_id,
// redirect_uri, response_type=code, code_challenge, code_challenge_method=S256,
// and state. When using the default endpoint the host must be auth.openai.com.
// When scopes are non-empty, the scope param must exist.
func TestProperty_BuildAuthURL_RequiredParams(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		clientID := rapid.StringMatching(`[a-z0-9\-]{3,30}`).Draw(t, "client_id")
		nScopes := rapid.IntRange(0, 3).Draw(t, "n_scopes")
		scopes := make([]string, nScopes)
		for i := range scopes {
			scopes[i] = rapid.StringMatching(`[a-z]{3,10}`).Draw(t, fmt.Sprintf("scope_%d", i))
		}

		cfg := Config{
			ClientID:      clientID,
			AuthEndpoint:  OpenAIAuthEndpoint,
			TokenEndpoint: OpenAITokenEndpoint,
			Scopes:        scopes,
			CallbackPath:  "/auth/callback",
			Timeout:       120 * time.Second,
		}

		challenge := rapid.StringMatching(`[A-Za-z0-9\-_]{43}`).Draw(t, "challenge")
		redirectURI := fmt.Sprintf("http://127.0.0.1:%d/auth/callback",
			rapid.IntRange(1024, 65535).Draw(t, "port"))
		state := rapid.StringMatching(`[a-zA-Z0-9]{8,16}`).Draw(t, "state")

		rawURL := BuildAuthURL(cfg, challenge, redirectURI, state)

		parsed, err := url.Parse(rawURL)
		if err != nil {
			t.Fatalf("invalid URL: %v", err)
		}

		// Host check (default endpoint)
		if parsed.Host != "auth.openai.com" {
			t.Fatalf("expected host auth.openai.com, got %q", parsed.Host)
		}

		q := parsed.Query()

		requireParam := func(key, wantValue string) {
			got := q.Get(key)
			if wantValue != "" && got != wantValue {
				t.Fatalf("param %s: got %q, want %q", key, got, wantValue)
			}
			if got == "" {
				t.Fatalf("missing required param %s in URL %s", key, rawURL)
			}
		}

		requireParam("client_id", clientID)
		requireParam("redirect_uri", redirectURI)
		requireParam("response_type", "code")
		requireParam("code_challenge", challenge)
		requireParam("code_challenge_method", "S256")
		requireParam("state", state)

		if len(scopes) > 0 {
			scopeVal := q.Get("scope")
			if scopeVal == "" {
				t.Fatalf("scopes non-empty but scope param missing")
			}
			for _, s := range scopes {
				if !strings.Contains(scopeVal, s) {
					t.Fatalf("scope %q not found in scope param %q", s, scopeVal)
				}
			}
		}
	})
}

// ─── Task 3.8 ───────────────────────────────────────────────────────────────
// Unit tests for CallbackServer

func TestCallbackServer_StartStop(t *testing.T) {
	srv := NewCallbackServer()
	if err := srv.Start("/auth/callback"); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	port := srv.Port()
	if port <= 0 {
		t.Fatalf("expected port > 0, got %d", port)
	}

	// Verify port is in use
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err == nil {
		ln.Close()
		t.Fatalf("port %d should be in use but was available", port)
	}

	srv.Stop()

	// Verify port is released (may need a brief pause for OS cleanup)
	time.Sleep(50 * time.Millisecond)
	ln, err = net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatalf("port %d should be released after Stop, but got: %v", port, err)
	}
	ln.Close()
}

func TestCallbackServer_SuccessCallback(t *testing.T) {
	srv := NewCallbackServer()
	if err := srv.Start("/auth/callback"); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer srv.Stop()

	expectedCode := "test_auth_code_12345"
	callbackURL := fmt.Sprintf("http://127.0.0.1:%d/auth/callback?code=%s",
		srv.Port(), expectedCode)

	resp, err := http.Get(callbackURL)
	if err != nil {
		t.Fatalf("HTTP GET failed: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	htmlBody := string(body)
	if !strings.Contains(htmlBody, "授权成功") {
		t.Fatalf("success HTML should contain '授权成功', got: %s", htmlBody)
	}

	code, err := srv.WaitForCode(2 * time.Second)
	if err != nil {
		t.Fatalf("WaitForCode error: %v", err)
	}
	if code != expectedCode {
		t.Fatalf("expected code %q, got %q", expectedCode, code)
	}
}

func TestCallbackServer_ErrorCallback(t *testing.T) {
	srv := NewCallbackServer()
	if err := srv.Start("/auth/callback"); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer srv.Stop()

	callbackURL := fmt.Sprintf(
		"http://127.0.0.1:%d/auth/callback?error=access_denied&error_description=user+denied",
		srv.Port())

	resp, err := http.Get(callbackURL)
	if err != nil {
		t.Fatalf("HTTP GET failed: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	htmlBody := string(body)
	if !strings.Contains(htmlBody, "授权失败") {
		t.Fatalf("error HTML should contain '授权失败', got: %s", htmlBody)
	}

	_, err = srv.WaitForCode(2 * time.Second)
	if err == nil {
		t.Fatal("expected error from WaitForCode on error callback, got nil")
	}
	if !strings.Contains(err.Error(), "access_denied") {
		t.Fatalf("error should contain 'access_denied', got: %v", err)
	}
}

func TestCallbackServer_Timeout(t *testing.T) {
	srv := NewCallbackServer()
	if err := srv.Start("/auth/callback"); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer srv.Stop()

	_, err := srv.WaitForCode(50 * time.Millisecond)
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("error should mention timeout, got: %v", err)
	}
}

// ─── Task 4.2 ───────────────────────────────────────────────────────────────
// Feature: openai-oauth-provider, Property 2: AuthType 空值向后兼容
// **Validates: Requirements 1.4**
//
// For any MaclawLLMProvider with AuthType="" (empty), NeedsRefresh should
// return false regardless of TokenExpiresAt value.
func TestProperty_AuthType_EmptyBackwardCompat(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		provider := corelib.MaclawLLMProvider{
			Name:           rapid.String().Draw(t, "name"),
			URL:            rapid.String().Draw(t, "url"),
			Key:            rapid.String().Draw(t, "key"),
			Model:          rapid.String().Draw(t, "model"),
			AuthType:       "", // always empty
			RefreshToken:   rapid.String().Draw(t, "refresh_token"),
			TokenExpiresAt: rapid.Int64().Draw(t, "token_expires_at"),
		}

		if NeedsRefresh(provider) {
			t.Fatalf("NeedsRefresh should return false for AuthType=\"\", got true (TokenExpiresAt=%d)", provider.TokenExpiresAt)
		}
	})
}

// ─── Task 4.3 ───────────────────────────────────────────────────────────────
// Feature: openai-oauth-provider, Property 3: NeedsRefresh 过期检测
// **Validates: Requirements 5.1**
//
// For any MaclawLLMProvider with AuthType="oauth":
//   - TokenExpiresAt within 5 minutes of now → NeedsRefresh returns true
//   - TokenExpiresAt more than 5 minutes from now → NeedsRefresh returns false
//   - TokenExpiresAt == 0 → NeedsRefresh returns false
func TestProperty_NeedsRefresh_ExpiryDetection(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		now := time.Now().Unix()
		margin := int64(TokenRefreshMargin.Seconds()) // 300 seconds

		// Choose a scenario: 0 = within margin, 1 = beyond margin, 2 = zero
		scenario := rapid.IntRange(0, 2).Draw(t, "scenario")

		var expiresAt int64
		var expectRefresh bool

		switch scenario {
		case 0:
			// Within 5 minutes: offset in [0, margin)
			offset := rapid.Int64Range(0, margin-1).Draw(t, "offset_within")
			expiresAt = now + offset
			expectRefresh = true
		case 1:
			// Beyond 5 minutes: offset in [margin+1, margin+86400]
			offset := rapid.Int64Range(margin+1, margin+86400).Draw(t, "offset_beyond")
			expiresAt = now + offset
			expectRefresh = false
		case 2:
			// Zero value
			expiresAt = 0
			expectRefresh = false
		}

		provider := corelib.MaclawLLMProvider{
			Name:           rapid.String().Draw(t, "name"),
			URL:            rapid.String().Draw(t, "url"),
			Key:            rapid.String().Draw(t, "key"),
			Model:          rapid.String().Draw(t, "model"),
			AuthType:       "oauth",
			TokenExpiresAt: expiresAt,
		}

		got := NeedsRefresh(provider)
		if got != expectRefresh {
			t.Fatalf("NeedsRefresh(expiresAt=%d, now≈%d, margin=%d) = %v, want %v (scenario=%d)",
				expiresAt, now, margin, got, expectRefresh, scenario)
		}
	})
}

// ─── Task 4.4 ───────────────────────────────────────────────────────────────
// Feature: openai-oauth-provider, Property 7: TokenResult 正确应用到 Provider
// **Validates: Requirements 4.1, 4.2, 4.3, 5.3**
//
// For any TokenResult and any MaclawLLMProvider:
//   - After ApplyTokenResult: provider.Key == result.AccessToken
//   - If result.RefreshToken != "": provider.RefreshToken == result.RefreshToken
//   - If result.RefreshToken == "": provider.RefreshToken unchanged from original
//   - provider.TokenExpiresAt ≈ now + result.ExpiresIn (within 2 seconds)
func TestProperty_ApplyTokenResult(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		originalRefresh := rapid.String().Draw(t, "original_refresh")

		provider := corelib.MaclawLLMProvider{
			Name:         rapid.String().Draw(t, "name"),
			URL:          rapid.String().Draw(t, "url"),
			Key:          rapid.String().Draw(t, "old_key"),
			Model:        rapid.String().Draw(t, "model"),
			AuthType:     "oauth",
			RefreshToken: originalRefresh,
		}

		result := &TokenResult{
			AccessToken:  rapid.StringMatching(`[a-zA-Z0-9]{10,50}`).Draw(t, "access_token"),
			RefreshToken: rapid.StringMatching(`[a-zA-Z0-9]{0,50}`).Draw(t, "refresh_token"),
			ExpiresIn:    rapid.IntRange(60, 86400).Draw(t, "expires_in"),
		}

		beforeApply := time.Now().Unix()
		updated := ApplyTokenResult(provider, result)
		afterApply := time.Now().Unix()

		// Key must equal AccessToken
		if updated.Key != result.AccessToken {
			t.Fatalf("Key = %q, want %q", updated.Key, result.AccessToken)
		}

		// RefreshToken: updated if non-empty, preserved if empty
		if result.RefreshToken != "" {
			if updated.RefreshToken != result.RefreshToken {
				t.Fatalf("RefreshToken = %q, want %q (result non-empty)", updated.RefreshToken, result.RefreshToken)
			}
		} else {
			if updated.RefreshToken != originalRefresh {
				t.Fatalf("RefreshToken = %q, want %q (result empty, should preserve original)", updated.RefreshToken, originalRefresh)
			}
		}

		// TokenExpiresAt ≈ now + ExpiresIn (within 2 seconds tolerance)
		expectedLow := beforeApply + int64(result.ExpiresIn)
		expectedHigh := afterApply + int64(result.ExpiresIn) + 2
		if updated.TokenExpiresAt < expectedLow || updated.TokenExpiresAt > expectedHigh {
			t.Fatalf("TokenExpiresAt = %d, want in [%d, %d]", updated.TokenExpiresAt, expectedLow, expectedHigh)
		}
	})
}
