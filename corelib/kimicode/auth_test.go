package kimicode

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestTokenPostDoesNotFollowRedirect(t *testing.T) {
	t.Setenv("KIMI_CODE_DEVICE_ID_FILE", t.TempDir()+"/device-id")
	leaked := false
	next := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		leaked = true
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"access_token":"stolen","refresh_token":"stolen","expires_in":900}`))
	}))
	defer next.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, next.URL, http.StatusTemporaryRedirect)
	}))
	defer server.Close()
	t.Setenv("KIMI_CODE_OAUTH_HOST", server.URL)

	_, err := Refresh(context.Background(), "secret-refresh", "")
	if err == nil {
		t.Fatal("expected redirect to fail the refresh")
	}
	if leaked {
		t.Fatal("refresh token was sent to the redirect target")
	}
}

func TestRequestDeviceAuthorizationSendsPublicClient(t *testing.T) {
	t.Setenv("KIMI_CODE_DEVICE_ID_FILE", t.TempDir()+"/device-id")
	var gotClient, gotPlatform string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		form, _ := url.ParseQuery(string(body))
		gotClient = form.Get("client_id")
		gotPlatform = r.Header.Get("X-Msh-Platform")
		if r.URL.Path != "/api/oauth/device_authorization" {
			t.Errorf("path = %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"user_code":"ABCD-EFGH","device_code":"device-1","verification_uri":"https://www.kimi.com/code/authorize_device","verification_uri_complete":"https://www.kimi.com/code/authorize_device?user_code=ABCD-EFGH","expires_in":300,"interval":5}`))
	}))
	defer server.Close()

	auth, err := requestDeviceAuthorization(context.Background(), server.URL, deviceHeaders())
	if err != nil {
		t.Fatal(err)
	}
	if gotClient != ClientID {
		t.Fatalf("client_id = %q", gotClient)
	}
	if gotPlatform != Platform {
		t.Fatalf("platform = %q", gotPlatform)
	}
	if auth.UserCode != "ABCD-EFGH" || auth.DeviceCode != "device-1" || auth.Interval != 5 {
		t.Fatalf("auth = %#v", auth)
	}
}

func TestPollUntilAuthorizedAcceptsPendingThenToken(t *testing.T) {
	t.Setenv("KIMI_CODE_OAUTH_HOST", "")
	t.Setenv("KIMI_OAUTH_HOST", "")
	t.Setenv("KIMI_CODE_DEVICE_ID_FILE", t.TempDir()+"/device-id")
	polls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		polls++
		w.Header().Set("Content-Type", "application/json")
		if polls == 1 {
			_, _ = w.Write([]byte(`{"error":"authorization_pending"}`))
			return
		}
		_, _ = w.Write([]byte(`{"access_token":"access-1","refresh_token":"refresh-1","expires_in":900,"token_type":"Bearer"}`))
	}))
	defer server.Close()
	t.Setenv("KIMI_CODE_OAUTH_HOST", server.URL)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	token, err := PollUntilAuthorized(ctx, "device-1", 1)
	if err != nil {
		t.Fatal(err)
	}
	if token.AccessToken != "access-1" || token.RefreshToken != "refresh-1" || token.ExpiresIn != 900 {
		t.Fatalf("token = %#v", token)
	}
	if polls < 2 {
		t.Fatalf("polls = %d", polls)
	}
}

func TestPollDeviceTokenClassifiesDeniedAndExpired(t *testing.T) {
	headers := http.Header{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if strings.Contains(string(body), "expired") {
			_, _ = w.Write([]byte(`{"error":"expired_token"}`))
			return
		}
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"access_denied","error_description":"user rejected"}`))
	}))
	defer server.Close()

	expired, err := pollDeviceToken(context.Background(), server.URL, "expired", headers)
	if err != nil || expired.kind != pollExpired {
		t.Fatalf("expired = %#v err=%v", expired, err)
	}
	denied, err := pollDeviceToken(context.Background(), server.URL, "other", headers)
	if err != nil || denied.kind != pollDenied || !strings.Contains(denied.description, "user rejected") {
		t.Fatalf("denied = %#v err=%v", denied, err)
	}
}

func TestRefreshRotatesTokenAndStopsOnInvalidGrant(t *testing.T) {
	t.Setenv("KIMI_CODE_DEVICE_ID_FILE", t.TempDir()+"/device-id")
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		body, _ := io.ReadAll(r.Body)
		form, _ := url.ParseQuery(string(body))
		if form.Get("grant_type") != "refresh_token" || form.Get("refresh_token") != "old-refresh" {
			t.Errorf("form = %s", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"access-2","refresh_token":"refresh-2","expires_in":900}`))
	}))
	defer server.Close()
	t.Setenv("KIMI_CODE_OAUTH_HOST", server.URL)

	token, err := Refresh(context.Background(), "old-refresh", "")
	if err != nil {
		t.Fatal(err)
	}
	if token.AccessToken != "access-2" || token.RefreshToken != "refresh-2" || calls != 1 {
		t.Fatalf("token = %#v calls=%d", token, calls)
	}

	denied := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
	}))
	defer denied.Close()
	t.Setenv("KIMI_CODE_OAUTH_HOST", denied.URL)
	if _, err := Refresh(context.Background(), "old-refresh", ""); err == nil || !strings.Contains(err.Error(), "重新登录") {
		t.Fatalf("invalid grant err = %v", err)
	}
}

func TestPollUntilAuthorizedStopsOnRejectedRequest(t *testing.T) {
	t.Setenv("KIMI_CODE_DEVICE_ID_FILE", t.TempDir()+"/device-id")
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_request","error_description":"bad client"}`))
	}))
	defer server.Close()
	t.Setenv("KIMI_CODE_OAUTH_HOST", server.URL)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err := PollUntilAuthorized(ctx, "device-1", 1)
	if err == nil || !strings.Contains(err.Error(), "bad client") {
		t.Fatalf("err = %v", err)
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
}

func TestPollUntilAuthorizedSurvivesTransientFailures(t *testing.T) {
	t.Setenv("KIMI_CODE_DEVICE_ID_FILE", t.TempDir()+"/device-id")
	polls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		polls++
		if polls < 4 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"access-1","refresh_token":"refresh-1","expires_in":900}`))
	}))
	defer server.Close()
	t.Setenv("KIMI_CODE_OAUTH_HOST", server.URL)

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	token, err := PollUntilAuthorized(ctx, "device-1", 1)
	if err != nil {
		t.Fatal(err)
	}
	if token.AccessToken != "access-1" || polls != 4 {
		t.Fatalf("token = %#v polls=%d", token, polls)
	}
}

func TestOAuthHostAddsHTTPSScheme(t *testing.T) {
	t.Setenv("KIMI_OAUTH_HOST", "")
	t.Setenv("KIMI_CODE_OAUTH_HOST", "auth.kimi.ai")
	if got := OAuthHost(); got != "https://auth.kimi.ai" {
		t.Fatalf("OAuthHost = %s", got)
	}
	if got := BaseURLForOAuthHost(OAuthHost()); got != GlobalBaseURL {
		t.Fatalf("base for schemeless host = %s", got)
	}
}

func TestBaseURLFollowsOAuthHost(t *testing.T) {
	if got := BaseURLForOAuthHost("https://auth.kimi.ai"); got != GlobalBaseURL {
		t.Fatalf("global base = %s", got)
	}
	if got := BaseURLForOAuthHost("https://auth.kimi.com"); got != DefaultBaseURL {
		t.Fatalf("mainland base = %s", got)
	}
	t.Setenv("KIMI_CODE_OAUTH_HOST", "")
	t.Setenv("KIMI_OAUTH_HOST", "")
	if got := OAuthHostForBaseURL(GlobalBaseURL); got != "https://auth.kimi.ai" {
		t.Fatalf("global auth host = %s", got)
	}
	if got := OAuthHostForBaseURL(DefaultBaseURL); got != DefaultOAuthHost {
		t.Fatalf("mainland auth host = %s", got)
	}
	if got := CanonicalBaseURL("https://api.kimi.ai/coding/v1/"); got != GlobalBaseURL {
		t.Fatalf("canonical global = %s", got)
	}
	if got := CanonicalBaseURL("https://api.kimi.com/coding/v1"); got != DefaultBaseURL {
		t.Fatalf("canonical mainland = %s", got)
	}
	if got := CanonicalBaseURL("https://evil.example/coding"); got != DefaultBaseURL {
		t.Fatalf("canonical other = %s", got)
	}
}

func TestIsApprovalURLAllowsBrowserSchemesOnly(t *testing.T) {
	if !IsApprovalURL("https://www.kimi.com/code/authorize_device?user_code=ABCD-EFGH") {
		t.Fatal("expected https approval url")
	}
	if !IsApprovalURL("https://www.kimi.ai/code/authorize_device") {
		t.Fatal("expected global approval url")
	}
	if !IsApprovalURL("http://127.0.0.1:9/approve") {
		t.Fatal("expected loopback http approval url")
	}
	for _, raw := range []string{"", "javascript:alert(1)", "file:///tmp/a", "https:///missing-host", "http://www.kimi.com/code/authorize_device", "https://evil.example/login", "https://user@www.kimi.com/code/authorize_device"} {
		if IsApprovalURL(raw) {
			t.Fatalf("accepted %q", raw)
		}
	}
}

func TestLoginLifetimeKeepsServerWindow(t *testing.T) {
	if got := LoginLifetime(1800); got != 1800*time.Second {
		t.Fatalf("LoginLifetime(1800) = %s", got)
	}
	if got := LoginLifetime(0); got != 15*time.Minute {
		t.Fatalf("LoginLifetime(0) = %s", got)
	}
	if got := LoginLifetime(10); got != time.Minute {
		t.Fatalf("LoginLifetime(10) = %s", got)
	}
	if got := LoginLifetime(3600); got != 30*time.Minute {
		t.Fatalf("LoginLifetime(3600) = %s", got)
	}
}

func TestApplyHeadersSkipsAPIKeyKimi(t *testing.T) {
	t.Setenv("KIMI_CODE_DEVICE_ID_FILE", t.TempDir()+"/device-id")
	oauthHeader := make(http.Header)
	ApplyHeaders(oauthHeader, corelib.MaclawLLMConfig{ProviderName: Name, AuthType: "oauth", URL: DefaultBaseURL})
	if oauthHeader.Get("X-Msh-Platform") != Platform {
		t.Fatalf("oauth platform = %q", oauthHeader.Get("X-Msh-Platform"))
	}
	if !strings.HasPrefix(oauthHeader.Get("User-Agent"), "MaClaw/") {
		t.Fatalf("user agent = %q", oauthHeader.Get("User-Agent"))
	}

	apiKeyHeader := make(http.Header)
	ApplyHeaders(apiKeyHeader, corelib.MaclawLLMConfig{ProviderName: "Kimi", AuthType: "apikey", URL: DefaultBaseURL})
	if apiKeyHeader.Get("X-Msh-Platform") != "" {
		t.Fatalf("api key provider got oauth headers: %v", apiKeyHeader)
	}
}
