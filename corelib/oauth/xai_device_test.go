package oauth

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestDeviceCodeEndpointFromTokenURL(t *testing.T) {
	got, err := deviceCodeEndpoint("https://auth.x.ai/oauth2/token")
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://auth.x.ai/oauth2/device/code" {
		t.Fatalf("device endpoint = %q", got)
	}
}

func TestXAIBrowserApprovalURLPrefersCompleteURI(t *testing.T) {
	got, err := xaiBrowserApprovalURL(&xaiDeviceCode{
		VerificationURI:         "https://accounts.x.ai/device",
		VerificationURIComplete: "https://accounts.x.ai/device?user_code=ABCD-EFGH",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "user_code=ABCD-EFGH") {
		t.Fatalf("approval URL = %q", got)
	}
	if _, err := xaiBrowserApprovalURL(&xaiDeviceCode{VerificationURI: "http://evil.example/device"}); err == nil {
		t.Fatal("expected a non-xAI approval URL to be rejected")
	}
	if _, err := xaiBrowserApprovalURL(&xaiDeviceCode{VerificationURIComplete: "https://accounts.x.ai/oauth2/device\n?user_code=ABCD"}); err == nil {
		t.Fatal("expected a control character in the approval URL to be rejected")
	}
}

func TestRequestXAIDeviceCodeRejectsAuthorizationCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"device_code":"device-1","user_code":"abc_def","verification_uri":"https://accounts.x.ai/oauth2/device","verification_uri_complete":"https://accounts.x.ai/oauth2/device?user_code=abc_def","expires_in":900,"interval":5}`))
	}))
	defer srv.Close()
	if _, err := requestXAIDeviceCode(context.Background(), srv.URL, XAIClientID, XAIConfig().Scopes); err == nil {
		t.Fatal("expected an authorization-code-shaped user_code to be rejected")
	}
}

func TestRequestXAIDeviceCodeHonoursCancel(t *testing.T) {
	hit := false
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		hit = true
	}))
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := requestXAIDeviceCode(ctx, srv.URL, XAIClientID, nil)
	if hit {
		t.Fatal("cancelled device request reached the server")
	}
	if err == nil || !strings.Contains(err.Error(), "oauth cancelled") {
		t.Fatalf("error = %v", err)
	}
}

func TestReplaceLoginCredentialDropsPreviousRefresh(t *testing.T) {
	prev := corelib.MaclawLLMProvider{Key: "old-access", RefreshToken: "old-refresh"}
	kept := ReplaceLoginCredential(prev, &TokenResult{AccessToken: "new-access", RefreshToken: "new-refresh", ExpiresIn: 60})
	if kept.Key != "new-access" || kept.RefreshToken != "new-refresh" {
		t.Fatalf("kept = %+v", kept)
	}
	cleared := ReplaceLoginCredential(prev, &TokenResult{AccessToken: "new-access", ExpiresIn: 60})
	if cleared.Key != "new-access" || cleared.RefreshToken != "" {
		t.Fatalf("cleared refresh = %q", cleared.RefreshToken)
	}
}

func TestRequestXAIDeviceCodeNotEnabled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	_, err := requestXAIDeviceCode(context.Background(), srv.URL, XAIClientID, XAIConfig().Scopes)
	if !errors.Is(err, ErrXAIDeviceFlowUnavailable) {
		t.Fatalf("error = %v, want device flow unavailable", err)
	}
}

func TestRunXAIDeviceFlowPollsUntilApproved(t *testing.T) {
	var mu sync.Mutex
	var polls int
	var opened string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		switch r.URL.Path {
		case "/oauth2/device/code":
			if !strings.Contains(string(body), "client_id="+XAIClientID) || !strings.Contains(string(body), "referrer=grok-build") {
				t.Errorf("device request body = %s", body)
			}
			if r.Header.Get("x-grok-client-surface") != "ui" || r.Header.Get("x-grok-client-version") == "" {
				t.Errorf("device request headers = %v", r.Header)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"device_code":"device-1",
				"user_code":"ABCD-EFGH",
				"verification_uri":"https://accounts.x.ai/device",
				"verification_uri_complete":"https://accounts.x.ai/device?user_code=ABCD-EFGH",
				"expires_in":900,
				"interval":5
			}`))
		case "/oauth2/token":
			mu.Lock()
			polls++
			n := polls
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			if n == 1 {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":"authorization_pending"}`))
				return
			}
			if !strings.Contains(string(body), "device_code=device-1") {
				t.Errorf("token body = %s", body)
			}
			_, _ = w.Write([]byte(`{"access_token":"access-1","refresh_token":"refresh-1","expires_in":1800}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	prev := sleepDevicePoll
	sleepDevicePoll = func(context.Context, time.Duration) error { return nil }
	t.Cleanup(func() { sleepDevicePoll = prev })

	cfg := XAIConfig()
	cfg.TokenEndpoint = srv.URL + "/oauth2/token"
	result, err := runXAIDeviceFlow(context.Background(), cfg, func(raw string) error {
		opened = raw
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if opened != "https://accounts.x.ai/device?user_code=ABCD-EFGH" {
		t.Fatalf("opened %q", opened)
	}
	if result.AccessToken != "access-1" || result.RefreshToken != "refresh-1" || result.ExpiresIn != 1800 {
		t.Fatalf("token = %+v", result)
	}
}

func TestPollXAIDeviceTokenDenied(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"access_denied"}`))
	}))
	defer srv.Close()
	prev := sleepDevicePoll
	sleepDevicePoll = func(context.Context, time.Duration) error { return nil }
	t.Cleanup(func() { sleepDevicePoll = prev })

	_, err := pollXAIDeviceToken(context.Background(), srv.URL, XAIClientID, "device-1", time.Second, 15*time.Minute)
	if err == nil || !strings.Contains(err.Error(), "authorization denied") {
		t.Fatalf("error = %v", err)
	}
}

func TestPollXAIDeviceTokenSlowsDown(t *testing.T) {
	var waits []time.Duration
	var polls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		polls++
		w.Header().Set("Content-Type", "application/json")
		if polls == 1 {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"slow_down"}`))
			return
		}
		_, _ = w.Write([]byte(`{"access_token":"access-1","expires_in":60}`))
	}))
	defer srv.Close()
	prev := sleepDevicePoll
	sleepDevicePoll = func(_ context.Context, d time.Duration) error {
		waits = append(waits, d)
		return nil
	}
	t.Cleanup(func() { sleepDevicePoll = prev })

	if _, err := pollXAIDeviceToken(context.Background(), srv.URL, XAIClientID, "device-1", time.Second, 15*time.Minute); err != nil {
		t.Fatal(err)
	}
	if len(waits) != 2 || waits[0] != time.Second || waits[1] != 6*time.Second {
		t.Fatalf("waits = %v", waits)
	}
}

func TestCallbackAcceptsJSONCode(t *testing.T) {
	srv := NewCallbackServer()
	if err := srv.Start("/callback"); err != nil {
		t.Fatal(err)
	}
	defer srv.Stop()

	req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("http://127.0.0.1:%d/callback", srv.Port()), strings.NewReader(`{"code":"from-json","state":"st"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://accounts.x.ai")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	code, state, err := srv.WaitForCallbackCtx(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if code != "from-json" || state != "st" {
		t.Fatalf("callback = %q %q", code, state)
	}
}
