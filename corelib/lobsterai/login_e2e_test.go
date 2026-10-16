package lobsterai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// redirectPortOf pulls the local callback port from the built login URL.
func redirectPortOf(t *testing.T, loginURL string) int {
	t.Helper()
	// loginURL = {portal}/portal#/login?source=electron&redirect_uri=...&state=...
	fragment := loginURL[strings.Index(loginURL, "#")+1:]
	parsed, err := url.Parse("?" + strings.TrimPrefix(fragment, "/portal"))
	if err != nil {
		t.Fatalf("parse fragment: %v", err)
	}
	redirect := parsed.Query().Get("redirect_uri")
	rdParsed, err := url.Parse(redirect)
	if err != nil {
		t.Fatalf("parse redirect_uri: %v", err)
	}
	port := 0
	fmt.Sscanf(rdParsed.Port(), "%d", &port)
	return port
}

// TestLobsterLoginLoopbackE2E drives the whole browser round trip over a real
// socket: portal URL → local callback with code+state → Wait → Resolve into
// /api/auth/exchange on a local server → token with identity pair.
func TestLobsterLoginLoopbackE2E(t *testing.T) {
	var gotPath, gotAuth string
	exchange := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		var payload map[string]any
		_ = json.NewDecoder(r.Body).Decode(&payload)
		if payload["authCode"] != "portal-code" {
			t.Errorf("exchange body wrong: %v", payload)
		}
		if v, ok := payload["uuid"].(string); !ok || v == "" {
			t.Errorf("exchange body must carry the login uuid: %v", payload)
		}
		if v, ok := payload["firstKeyfrom"].(string); !ok || v == "" {
			t.Errorf("exchange body must carry the firstKeyfrom: %v", payload)
		}
		_, _ = w.Write([]byte(`{"code":0,"data":{"accessToken":"lob-jwt","refreshToken":"lob-rt","expiresIn":7200,` +
			`"uuid":"saved-uuid","firstKeyfrom":"1000","user":{"id":"7","nickname":"龙虾"}}}`))
	}))
	defer exchange.Close()

	previous := apiBaseTestOverride
	apiBaseTestOverride = exchange.URL
	defer func() { apiBaseTestOverride = previous }()

	session, err := StartLogin(0)
	if err != nil {
		t.Fatalf("start login: %v", err)
	}
	defer session.Cancel()
	if session.LoginURL == "" || session.RedirectURI == "" || session.State == "" {
		t.Fatalf("session not fully populated: %+v", session)
	}
	port := redirectPortOf(t, session.LoginURL)
	if port == 0 {
		t.Fatalf("login URL missing callback port: %s", session.LoginURL)
	}

	// Wrong state first: the callback must refuse it.
	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/auth/callback?code=x&state=evil", port))
	if err != nil {
		t.Fatalf("foreign-state redirect: %v", err)
	}
	resp.Body.Close()
	if _, err := session.Wait(context.Background()); err == nil || !strings.Contains(err.Error(), "state") {
		t.Fatalf("foreign state must fail the session: %v", err)
	}

	// Fresh session for the happy path (the refusal above terminated this one).
	session, err = StartLogin(0)
	if err != nil {
		t.Fatalf("restart login: %v", err)
	}
	defer session.Cancel()
	port = redirectPortOf(t, session.LoginURL)

	resp, err = http.Get(fmt.Sprintf("http://127.0.0.1:%d/auth/callback?code=portal-code&state=%s", port, session.State))
	if err != nil {
		t.Fatalf("redirect: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("callback page status: %d", resp.StatusCode)
	}

	code, err := session.Wait(context.Background())
	if err != nil {
		t.Fatalf("wait: %v", err)
	}
	if code != "portal-code" {
		t.Fatalf("code mismatch: %q", code)
	}
	token, err := session.Resolve(context.Background(), code)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if token.AccessToken != "lob-jwt" || token.RefreshToken != "lob-rt" {
		t.Fatalf("token wrong: %+v", token)
	}
	if token.UserID != "7" || token.Nickname != "龙虾" {
		t.Fatalf("account view wrong: %+v", token)
	}
	if token.UUID != "saved-uuid" || token.FirstKeyfrom != "1000" {
		t.Fatalf("refresh identity pair missing: %+v", token)
	}
	if gotPath != ExchangePath {
		t.Fatalf("exchange hit %s", gotPath)
	}
	_ = gotAuth
}
