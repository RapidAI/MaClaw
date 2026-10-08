package trae

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// callbackPort extracts the local redirect port from a built login URL.
func callbackPort(t *testing.T, loginURL string) int {
	t.Helper()
	parsed, err := url.Parse(loginURL)
	if err != nil {
		t.Fatalf("login URL parse: %v", err)
	}
	callback := parsed.Query().Get("auth_callback_url")
	cbParsed, err := url.Parse(callback)
	if err != nil {
		t.Fatalf("callback parse: %v", err)
	}
	port := 0
	fmt.Sscanf(cbParsed.Port(), "%d", &port)
	return port
}

// TestTraeLoginLoopbackE2E drives the entire browser round trip over a real
// socket: build URL → console callback → session.Wait → Resolve → token, with
// the exchange pinned to a local server. This is the layer the previous
// real-device failure surfaced through — keep it green so protocol drift at
// least breaks here first.
func TestTraeLoginLoopbackE2E(t *testing.T) {
	exchange := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		_ = json.NewDecoder(r.Body).Decode(&payload)
		if payload["RefreshToken"] != "rt-loopback" {
			t.Errorf("unexpected exchange body: %v", payload)
		}
		if payload["ClientSecret"] != "-" {
			t.Errorf("legacy exchange must carry the tolerated secret: %v", payload)
		}
		_, _ = w.Write([]byte(`{"Result":{"Token":"loop-jwt","TokenExpireAt":1786847930141,"RefreshToken":"rt-rotated"}}`))
	}))
	defer exchange.Close()

	profile := CNProfile()
	profile.AuthBase = exchange.URL
	profile.AuthAltBase = ""

	session, loginURL, err := StartLogin(profile, 0)
	if err != nil {
		t.Fatalf("start login: %v", err)
	}
	defer session.Cancel()
	port := callbackPort(t, loginURL)
	if port == 0 {
		t.Fatalf("login URL missing callback port: %s", loginURL)
	}

	userInfo := url.QueryEscape(`{"UserID":"42","ScreenName":"小明"}`)
	callback := fmt.Sprintf("http://127.0.0.1:%d/authorize?refreshToken=rt-loopback&loginTraceID=%s&userInfo=%s",
		port, session.TraceID, userInfo)
	resp, err := http.Get(callback)
	if err != nil {
		t.Fatalf("browser redirect: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("callback page status: %d", resp.StatusCode)
	}

	details, err := session.Wait(context.Background())
	if err != nil {
		t.Fatalf("wait: %v", err)
	}
	if details.RefreshToken != "rt-loopback" || details.UserID != "42" || details.DisplayName != "小明" {
		t.Fatalf("callback details wrong: %+v", details)
	}
	if details.LoginTraceID != session.TraceID {
		t.Fatalf("callback trace mismatch: %q vs %q", details.LoginTraceID, session.TraceID)
	}

	token, err := session.Resolve(context.Background(), profile, details)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if token.AccessToken != "loop-jwt" || token.RefreshToken != "rt-rotated" {
		t.Fatalf("token wrong: %+v", token)
	}
	// The login-page device pair binds the refresh token and must ride the
	// token into persistence.
	if token.MachineID != session.MachineID || token.DeviceID != session.DeviceID {
		t.Fatalf("login pair lost: token=%q/%q session=%q/%q",
			token.MachineID, token.DeviceID, session.MachineID, session.DeviceID)
	}
	if token.UserID != "42" {
		t.Fatalf("user id lift missing: %q", token.UserID)
	}
	if token.ExpiresAt != 1786847930 {
		t.Fatalf("expiry normalized wrong: %d", token.ExpiresAt)
	}
}

// TestTraeLoginLoopbackRejectsForeignTrace drives the CSRF corner over the
// same socket: a callback carrying a mismatched loginTraceID must be refused.
func TestTraeLoginLoopbackRejectsForeignTrace(t *testing.T) {
	exchange := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"Result":{"Token":"never"}}`))
	}))
	defer exchange.Close()

	profile := CNProfile()
	profile.AuthBase = exchange.URL
	session, loginURL, err := StartLogin(profile, 0)
	if err != nil {
		t.Fatalf("start login: %v", err)
	}
	defer session.Cancel()
	port := callbackPort(t, loginURL)

	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/authorize?refreshToken=rt-x&loginTraceID=attacker", port))
	if err != nil {
		t.Fatalf("redirect: %v", err)
	}
	defer resp.Body.Close()
	if _, err := session.Wait(context.Background()); err == nil || !strings.Contains(err.Error(), "loginTraceID") {
		t.Fatalf("mismatched trace must fail the session: %v", err)
	}
}

// TestTraeLoginLoopbackAuthCodeContractIsRefused keeps the device-proof
// contract from silently regressing in: the simplified flow cannot service an
// AuthCode callback, so it must fail with explicit guidance.
func TestTraeLoginLoopbackAuthCodeContractIsRefused(t *testing.T) {
	session, loginURL, err := StartLogin(CNProfile(), 0)
	if err != nil {
		t.Fatalf("start login: %v", err)
	}
	defer session.Cancel()
	port := callbackPort(t, loginURL)
	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/authorize?authCodeInfo=%s&loginTraceID=%s",
		port, url.QueryEscape(`{"AuthCode":"acode"}`), session.TraceID))
	if err != nil {
		t.Fatalf("redirect: %v", err)
	}
	defer resp.Body.Close()

	details, err := session.Wait(context.Background())
	if err != nil {
		t.Fatalf("wait should still surface the callback: %v", err)
	}
	details.AuthCode = "acode" // Wait only enforces usability; Resolve rejects.
	_, resolveErr := session.Resolve(context.Background(), CNProfile(), details)
	if resolveErr == nil || !strings.Contains(resolveErr.Error(), "AuthCode") {
		t.Fatalf("authCode contract must be refused with guidance: %v", resolveErr)
	}
}

// TestWaitRespectsDeadline exercises the Wait deadline path with a real
// listener and no callback.
func TestWaitRespectsDeadline(t *testing.T) {
	session, _, err := StartLogin(CNProfile(), 0)
	if err != nil {
		t.Fatalf("start login: %v", err)
	}
	defer session.Cancel()
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	if _, err := session.Wait(ctx); err == nil {
		t.Fatal("expired wait must error")
	}
	// The session is still cancelable afterwards without wedging.
	session.Cancel()
}
