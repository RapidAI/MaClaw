package mailbox

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func useOAuthProvider(t *testing.T, p oauthProvider) {
	t.Helper()
	prev, ok := oauthProviderTable[p.ID]
	oauthProviderTable[p.ID] = p
	t.Cleanup(func() {
		if ok {
			oauthProviderTable[p.ID] = prev
		} else {
			delete(oauthProviderTable, p.ID)
		}
	})
	oauthRefreshMu.Lock()
	oauthRefreshCache = map[string]string{}
	oauthRefreshMu.Unlock()
}

func testJWT(t *testing.T, claims map[string]string) string {
	t.Helper()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none"}`))
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	return header + "." + base64.RawURLEncoding.EncodeToString(payload) + ".sig"
}

func TestXOAuth2InitialResponse(t *testing.T) {
	mech, ir, err := newXOAuth2Client("a@gmail.com", "tok").Start()
	if err != nil {
		t.Fatal(err)
	}
	if mech != "XOAUTH2" || string(ir) != "user=a@gmail.com\x01auth=Bearer tok\x01\x01" {
		t.Fatalf("mech=%q ir=%q", mech, ir)
	}
	reply, err := newXOAuth2Client("a@gmail.com", "tok").Next([]byte(`{"status":"401","token":"tok"}`))
	if err != nil || len(reply) != 0 || strings.Contains(string(reply), "tok") {
		t.Fatalf("reply=%q err=%v", reply, err)
	}
	if token, ok := OAuthAccessToken("plain-password"); ok || token != "" {
		t.Fatalf("password treated as oauth: %q", token)
	}
}

func TestLoginOAuthHintAndCancelledRefresh(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_, err := LoginOAuthWithHint(ctx, ProviderGmail, "not-an-email", func(string) error {
		t.Fatal("browser opened")
		return nil
	})
	if err == nil || err.Error() != "邮箱地址格式不正确" {
		t.Fatal(err)
	}
	_, err = LoginOAuthWithHint(ctx, ProviderGmail, "a@gmail.com", func(authURL string) error {
		u, parseErr := url.Parse(authURL)
		if parseErr != nil {
			return parseErr
		}
		if u.Query().Get("login_hint") != "a@gmail.com" {
			t.Fatalf("hint=%q", u.Query().Get("login_hint"))
		}
		return errors.New("stop")
	})
	if err == nil || err.Error() != "无法打开浏览器" {
		t.Fatal(err)
	}

	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "fresh", "refresh_token": "rotated", "expires_in": 3600,
		})
	}))
	defer srv.Close()
	gmail := oauthProviderTable[ProviderGmail]
	gmail.TokenEndpoint = srv.URL
	useOAuthProvider(t, gmail)
	raw, err := encodeOAuthSecret(oauthSecret{
		Kind: AuthOAuth, Provider: ProviderGmail, AccessToken: "old", RefreshToken: "cancel-me",
		Expiry: time.Now().Add(-time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	gone, stop := context.WithCancel(context.Background())
	stop()
	current, updated, err := MaterializeSecret(gone, raw, time.Now())
	if err != nil || updated == "" || calls != 1 {
		t.Fatalf("current=%s updated=%s calls=%d err=%v", current, updated, calls, err)
	}
	if token, ok := OAuthAccessToken(current); !ok || token != "fresh" {
		t.Fatalf("token=%q", token)
	}
}

func TestRefreshKeepsMailboxScopes(t *testing.T) {
	var scope string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		scope = r.PostForm.Get("scope")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "fresh", "refresh_token": "next", "expires_in": 3600,
		})
	}))
	defer srv.Close()
	hotmail := oauthProviderTable[ProviderHotmail]
	hotmail.TokenEndpoint = srv.URL
	if _, err := refreshOAuthToken(context.Background(), hotmail, "old-refresh"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(scope, "openid") || strings.Contains(scope, "profile") || !strings.Contains(scope, "IMAP.AccessAsUser.All") || !strings.Contains(scope, "SMTP.Send") {
		t.Fatal(scope)
	}
}

func TestEmailFromJWTSkipsPrefixedAccountName(t *testing.T) {
	raw := testJWT(t, map[string]string{
		"unique_name":        "live.com#me@hotmail.com",
		"preferred_username": "me@hotmail.com",
	})
	if email := emailFromJWT(raw); email != "me@hotmail.com" {
		t.Fatal(email)
	}
	prefixed := testJWT(t, map[string]string{"unique_name": "live.com#me@hotmail.com"})
	if email := emailFromJWT(prefixed); email != "me@hotmail.com" {
		t.Fatal(email)
	}
	named := testJWT(t, map[string]string{"unique_name": "Live User <me@hotmail.com>"})
	if email := emailFromJWT(named); email != "me@hotmail.com" {
		t.Fatal(email)
	}
}

func TestLoginOAuthHotmailReadsIDToken(t *testing.T) {
	idToken := testJWT(t, map[string]string{"unique_name": "live.com#me@hotmail.com"})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/token" {
			t.Errorf("profile fetched for id token: %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "opaque-token",
			"id_token":      idToken,
			"refresh_token": "hotmail-refresh",
			"expires_in":    3600,
		})
	}))
	defer srv.Close()
	hotmail := oauthProviderTable[ProviderHotmail]
	hotmail.TokenEndpoint = srv.URL + "/token"
	hotmail.ProfileURL = srv.URL + "/me"
	hotmail.ProfileAlts = nil
	useOAuthProvider(t, hotmail)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	grant, err := LoginOAuth(ctx, ProviderHotmail, func(authURL string) error {
		return completeOAuthURL(t, authURL, "")
	})
	if err != nil {
		t.Fatal(err)
	}
	if grant.Email != "me@hotmail.com" {
		t.Fatalf("grant=%+v", grant)
	}
}

func TestHotmailProfileTriesNextHostAfterGone(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/gone" {
			http.Error(w, "gone", http.StatusGone)
			return
		}
		if r.Header.Get("Authorization") != "Bearer opaque-token" {
			http.Error(w, "no", http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"EmailAddress": "me@hotmail.com"})
	}))
	defer srv.Close()
	email, err := oauthIdentity(context.Background(), oauthProvider{
		ProfileURL:  srv.URL + "/gone",
		ProfileAlts: []string{srv.URL + "/me"},
	}, oauthToken{AccessToken: "opaque-token"})
	if err != nil || email != "me@hotmail.com" {
		t.Fatalf("email=%q err=%v", email, err)
	}
}

func TestLoginOAuthHotmailUsesProfileWhenTokenHasNoEmail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token":  "opaque-token",
				"refresh_token": "hotmail-refresh",
				"expires_in":    3600,
			})
		case "/me":
			if r.Header.Get("Authorization") != "Bearer opaque-token" {
				http.Error(w, "no", http.StatusUnauthorized)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"EmailAddress": "me@hotmail.com"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	hotmail := oauthProviderTable[ProviderHotmail]
	hotmail.TokenEndpoint = srv.URL + "/token"
	hotmail.ProfileURL = srv.URL + "/me"
	hotmail.ProfileAlts = nil
	useOAuthProvider(t, hotmail)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	grant, err := LoginOAuth(ctx, ProviderHotmail, func(authURL string) error {
		return completeOAuthURL(t, authURL, "")
	})
	if err != nil {
		t.Fatal(err)
	}
	if grant.Email != "me@hotmail.com" || grant.Provider != ProviderHotmail {
		t.Fatalf("grant=%+v", grant)
	}
}

func TestLoginOAuthGmailAndHotmail(t *testing.T) {
	var gmailForm url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			_ = r.ParseForm()
			gmailForm = r.PostForm
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token":  "ya29.gmail",
				"refresh_token": "gmail-refresh",
				"expires_in":    3600,
			})
		case "/profile":
			if r.Header.Get("Authorization") != "Bearer ya29.gmail" {
				http.Error(w, "no", http.StatusUnauthorized)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"emailAddress": "A@gmail.com"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	gmail := oauthProviderTable[ProviderGmail]
	gmail.TokenEndpoint = srv.URL + "/token"
	gmail.ProfileURL = srv.URL + "/profile"
	useOAuthProvider(t, gmail)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	grant, err := LoginOAuth(ctx, ProviderGmail, func(authURL string) error {
		return completeOAuthURL(t, authURL, "")
	})
	if err != nil {
		t.Fatal(err)
	}
	if grant.Email != "A@gmail.com" || grant.Provider != ProviderGmail {
		t.Fatalf("grant=%+v", grant)
	}
	if gmailForm.Get("code_verifier") == "" || gmailForm.Get("client_secret") == "" || gmailForm.Get("code") != "abc" {
		t.Fatalf("token form=%v", gmailForm)
	}
	token, ok := OAuthAccessToken(grant.Secret)
	if !ok || token != "ya29.gmail" || strings.Contains(grant.Secret, gmailOAuthClientSecret) {
		t.Fatalf("secret=%s", grant.Secret)
	}

	hotmailAccess := testJWT(t, map[string]string{"preferred_username": "me@hotmail.com"})
	hotmailSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.PostForm.Get("code_verifier") != "" || r.PostForm.Get("client_secret") != "" {
			http.Error(w, "unexpected client auth", http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  hotmailAccess,
			"refresh_token": "hotmail-refresh",
			"expires_in":    3600,
		})
	}))
	defer hotmailSrv.Close()
	hotmail := oauthProviderTable[ProviderHotmail]
	hotmail.TokenEndpoint = hotmailSrv.URL
	useOAuthProvider(t, hotmail)
	hotmailGrant, err := LoginOAuth(ctx, ProviderHotmail, func(authURL string) error {
		u, err := url.Parse(authURL)
		if err != nil {
			return err
		}
		if u.Query().Get("code_challenge") != "" {
			t.Fatal("hotmail login sent pkce")
		}
		scope := u.Query().Get("scope")
		if !strings.Contains(scope, "IMAP.AccessAsUser.All") || !strings.Contains(scope, "SMTP.Send") || !strings.Contains(scope, "openid") {
			t.Fatal(scope)
		}
		return completeOAuthURL(t, authURL, "")
	})
	if err != nil {
		t.Fatal(err)
	}
	if hotmailGrant.Email != "me@hotmail.com" {
		t.Fatalf("hotmail=%+v", hotmailGrant)
	}
}

func TestLoginOAuthRejectsStateMismatch(t *testing.T) {
	gmail := oauthProviderTable[ProviderGmail]
	gmail.TokenEndpoint = "http://127.0.0.1:1/token"
	useOAuthProvider(t, gmail)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_, err := LoginOAuth(ctx, ProviderGmail, func(authURL string) error {
		return completeOAuthURL(t, authURL, "wrong-state")
	})
	if err == nil || err.Error() != "登录回调无效" {
		t.Fatal(err)
	}
}

func TestMaterializeSecretRefreshesOnce(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "fresh",
			"refresh_token": "rotated",
			"expires_in":    3600,
		})
	}))
	defer srv.Close()
	gmail := oauthProviderTable[ProviderGmail]
	gmail.TokenEndpoint = srv.URL
	useOAuthProvider(t, gmail)
	raw, err := encodeOAuthSecret(oauthSecret{
		Kind: AuthOAuth, Provider: ProviderGmail, AccessToken: "old", RefreshToken: "stale",
		Expiry: time.Now().Add(-time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	current, updated, err := MaterializeSecret(context.Background(), raw, now)
	if err != nil || updated == "" || calls != 1 {
		t.Fatalf("current=%s updated=%s calls=%d err=%v", current, updated, calls, err)
	}
	token, ok := OAuthAccessToken(current)
	if !ok || token != "fresh" {
		t.Fatalf("token=%q", token)
	}
	again, againUpdated, err := MaterializeSecret(context.Background(), raw, now)
	if err != nil || again != current || againUpdated != current || calls != 1 {
		t.Fatalf("again=%s calls=%d err=%v", again, calls, err)
	}
	fresh, updatedFresh, err := MaterializeSecret(context.Background(), current, now)
	if err != nil || fresh != current || updatedFresh != "" {
		t.Fatalf("fresh refresh updated=%q err=%v", updatedFresh, err)
	}
	later, laterUpdated, err := MaterializeSecret(context.Background(), current, now.Add(2*time.Hour))
	if err != nil || laterUpdated == "" || later == current || calls != 2 {
		t.Fatalf("expired cache reused: later=%s calls=%d err=%v", later, calls, err)
	}
}

func TestLoginOAuthRequiresRefreshToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "ya29.only", "expires_in": 3600})
	}))
	defer srv.Close()
	gmail := oauthProviderTable[ProviderGmail]
	gmail.TokenEndpoint = srv.URL + "/token"
	gmail.ProfileURL = srv.URL + "/profile"
	useOAuthProvider(t, gmail)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_, err := LoginOAuth(ctx, ProviderGmail, func(authURL string) error {
		return completeOAuthURL(t, authURL, "")
	})
	if err == nil || err.Error() != "登录没有拿到长期授权，请重新登录" {
		t.Fatal(err)
	}
}

func TestSaveOAuthAccountUpdatesSameAddress(t *testing.T) {
	now := time.Date(2026, 10, 16, 8, 0, 0, 0, time.UTC)
	first, saved, err := SaveOAuthAccount(nil, "a@gmail.com", ProviderGmail, now)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Auth != AuthOAuth || saved.Provider != ProviderGmail || saved.Name != "Gmail" {
		t.Fatalf("saved=%+v", saved)
	}
	if saved.Inbound.Host != "imap.gmail.com" || saved.Inbound.Username != "a@gmail.com" || saved.Inbound.Port != 993 {
		t.Fatalf("inbound=%+v", saved.Inbound)
	}
	if saved.Outbound.Host != "smtp.gmail.com" || saved.Outbound.Port != 465 || saved.Outbound.FromAddress != "a@gmail.com" {
		t.Fatalf("outbound=%+v", saved.Outbound)
	}
	raw, _ := json.Marshal(first)
	if strings.Contains(string(raw), "access_token") || strings.Contains(string(raw), gmailOAuthClientSecret) {
		t.Fatal(string(raw))
	}
	summary := Summary(saved, true, true)
	if summary["auth"] != AuthOAuth || summary["provider"] != ProviderGmail {
		t.Fatalf("summary=%v", summary)
	}
	renamed, _, err := UpsertAccount(first, SaveRequest{ID: saved.ID, Name: "个人", Address: saved.Address, Inbound: saved.Inbound, Outbound: saved.Outbound}, now)
	if err != nil {
		t.Fatal(err)
	}
	if renamed[0].Auth != AuthOAuth || renamed[0].Provider != ProviderGmail || renamed[0].Name != "个人" {
		t.Fatalf("rename dropped login: %+v", renamed[0])
	}
	second, updated, err := SaveOAuthAccount(renamed, "A@gmail.com", ProviderGmail, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if updated.ID != saved.ID || updated.Name != "个人" || len(second) != 1 {
		t.Fatalf("updated=%+v len=%d", updated, len(second))
	}
	other, _, err := SaveOAuthAccount(second, "b@hotmail.com", ProviderHotmail, now.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(other) != 2 || other[1].Name != "Hotmail" || other[1].Outbound.Host != "smtp.office365.com" || other[1].Outbound.Port != 587 {
		t.Fatalf("hotmail=%+v", other[1])
	}
}

func completeOAuthURL(t *testing.T, authURL, state string) error {
	t.Helper()
	u, err := url.Parse(authURL)
	if err != nil {
		return err
	}
	redirect := u.Query().Get("redirect_uri")
	if state == "" {
		state = u.Query().Get("state")
	}
	resp, err := http.Get(redirect + "/?code=abc&state=" + url.QueryEscape(state))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}
