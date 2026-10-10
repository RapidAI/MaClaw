package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/kimicode"
)

// kimiFakeAuthServer stands in for the Kimi device-flow host. It serves the
// device authorization, one authorization_pending poll, then the token.
type kimiFakeAuthServer struct {
	*httptest.Server
	deviceCalls int
	tokenCalls  int
}

func newKimiFakeAuthServer(t *testing.T) *kimiFakeAuthServer {
	t.Helper()
	fake := &kimiFakeAuthServer{}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/oauth/device_authorization", func(w http.ResponseWriter, r *http.Request) {
		fake.deviceCalls++
		_ = r.ParseForm()
		if got := r.Form.Get("client_id"); got != kimicode.ClientID {
			t.Errorf("device_authorization client_id = %q", got)
		}
		if got := r.Header.Get("X-Msh-Platform"); got == "" {
			t.Error("device_authorization missing X-Msh-Platform")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"user_code":                "ABCD-EFGH",
			"device_code":              "device-code-123",
			"verification_uri":         "https://www.kimi.com/code",
			"verification_uri_complete": "https://www.kimi.com/code?user_code=ABCD-EFGH",
			"expires_in":               600,
			"interval":                 1,
		})
	})
	mux.HandleFunc("/api/oauth/token", func(w http.ResponseWriter, r *http.Request) {
		fake.tokenCalls++
		_ = r.ParseForm()
		switch r.Form.Get("grant_type") {
		case "urn:ietf:params:oauth:grant-type:device_code":
			if fake.tokenCalls == 1 {
				_ = json.NewEncoder(w).Encode(map[string]any{"error": "authorization_pending"})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token":  "kimi-access-token",
				"refresh_token": "kimi-refresh-token",
				"expires_in":    3600,
			})
		case "refresh_token":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token":  "kimi-access-token-2",
				"refresh_token": "kimi-refresh-token-2",
				"expires_in":    3600,
			})
		default:
			t.Errorf("unexpected grant_type %q", r.Form.Get("grant_type"))
			http.Error(w, `{"error":"unsupported"}`, http.StatusBadRequest)
		}
	})
	fake.Server = httptest.NewServer(mux)
	t.Cleanup(fake.Close)
	return fake
}

func kimiTestEnv(t *testing.T) {
	t.Helper()
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("KIMI_CODE_OAUTH_HOST", "")
	t.Setenv("KIMI_OAUTH_HOST", "")
	t.Setenv("AICODER_SKIP_CODEXPROXY_RELAUNCH", "1")
	t.Setenv("AICODER_SKIP_CODEX_PROCESS_KILL", "1")
}

func TestStartKimiWebLoginReturnsDeviceCode(t *testing.T) {
	kimiTestEnv(t)
	fake := newKimiFakeAuthServer(t)
	t.Setenv("KIMI_CODE_OAUTH_HOST", fake.URL)

	app := NewApp()
	info, err := app.StartKimiWebLogin()
	if err != nil {
		t.Fatalf("StartKimiWebLogin: %v", err)
	}
	if info.UserCode != "ABCD-EFGH" {
		t.Fatalf("user code = %q", info.UserCode)
	}
	if info.VerificationURIComplete == "" || info.VerificationURI == "" {
		t.Fatalf("verification URLs missing: %+v", info)
	}
	if fake.deviceCalls != 1 {
		t.Fatalf("device_authorization called %d times", fake.deviceCalls)
	}
	app.cancelKimiLogin()
}

func TestWaitKimiWebLoginPersistsToken(t *testing.T) {
	kimiTestEnv(t)
	fake := newKimiFakeAuthServer(t)
	t.Setenv("KIMI_CODE_OAUTH_HOST", fake.URL)

	app := NewApp()
	if err := writeSettings(Settings{
		ListenAddress:  "127.0.0.1:0",
		APIKey:         "local-key",
		ActiveAuthMode: AuthModeKimiWeb,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := app.StartKimiWebLogin(); err != nil {
		t.Fatalf("StartKimiWebLogin: %v", err)
	}
	if _, err := app.WaitKimiWebLogin(); err != nil {
		t.Fatalf("WaitKimiWebLogin: %v", err)
	}
	if fake.tokenCalls < 2 {
		t.Fatalf("token polled %d times, want the pending poll then success", fake.tokenCalls)
	}
	got, err := loadSettings()
	if err != nil {
		t.Fatal(err)
	}
	if got.AccessToken != "kimi-access-token" {
		t.Fatalf("access token = %q", got.AccessToken)
	}
	profile := got.AuthProfiles[AuthModeKimiWeb]
	if profile.RefreshToken != "kimi-refresh-token" {
		t.Fatalf("refresh token = %q", profile.RefreshToken)
	}
	if got.BaseURL != kimicode.DefaultBaseURL {
		t.Fatalf("base url = %q, want %q", got.BaseURL, kimicode.DefaultBaseURL)
	}
	if got.ModelID != kimicode.DefaultModel {
		t.Fatalf("model = %q, want %q", got.ModelID, kimicode.DefaultModel)
	}
}

func TestWaitKimiWebLoginWithoutStartFails(t *testing.T) {
	kimiTestEnv(t)
	app := NewApp()
	if _, err := app.WaitKimiWebLogin(); err == nil {
		t.Fatal("WaitKimiWebLogin without a started login should fail")
	}
}

func TestCancelKimiWebLoginStopsPendingLogin(t *testing.T) {
	kimiTestEnv(t)
	app := NewApp()
	if _, err := app.StartKimiWebLogin(); err != nil {
		t.Fatalf("StartKimiWebLogin: %v", err)
	}
	app.CancelKimiWebLogin()
	done := make(chan error, 1)
	go func() {
		_, err := app.WaitKimiWebLogin()
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancelled login should not succeed")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("WaitKimiWebLogin did not return after cancel")
	}
}

func TestRefreshKimiTokenRotatesCredentials(t *testing.T) {
	kimiTestEnv(t)
	fake := newKimiFakeAuthServer(t)
	t.Setenv("KIMI_CODE_OAUTH_HOST", fake.URL)

	app := NewApp()
	s := Settings{
		ActiveAuthMode: AuthModeKimiWeb,
		AccessToken:    "stale-token",
		BaseURL:        kimicode.DefaultBaseURL,
		AuthProfiles: map[string]AuthProfile{
			AuthModeKimiWeb: {AccessToken: "stale-token", APIKey: "stale-token", RefreshToken: "old-refresh"},
		},
	}
	got := app.refreshKimiToken(s)
	if got.AccessToken != "kimi-access-token-2" {
		t.Fatalf("access token = %q", got.AccessToken)
	}
	if got.AuthProfiles[AuthModeKimiWeb].RefreshToken != "kimi-refresh-token-2" {
		t.Fatalf("rotated refresh token not persisted: %+v", got.AuthProfiles[AuthModeKimiWeb])
	}
}

func TestRefreshKimiTokenSkipsOtherModes(t *testing.T) {
	kimiTestEnv(t)
	app := NewApp()
	s := Settings{
		ActiveAuthMode: AuthModeOpenAIOAuth,
		AccessToken:    "openai-token",
		AuthProfiles: map[string]AuthProfile{
			AuthModeKimiWeb: {RefreshToken: "old-refresh"},
		},
	}
	if got := app.refreshKimiToken(s); got.AccessToken != "openai-token" {
		t.Fatalf("non-Kimi mode was refreshed: %q", got.AccessToken)
	}
}

func TestRefreshKimiTokenKeepsTokenOnFailure(t *testing.T) {
	kimiTestEnv(t)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/oauth/token", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
	})
	fake := httptest.NewServer(mux)
	defer fake.Close()
	t.Setenv("KIMI_CODE_OAUTH_HOST", fake.URL)

	app := NewApp()
	s := Settings{
		ActiveAuthMode: AuthModeKimiWeb,
		AccessToken:    "still-good",
		BaseURL:        kimicode.DefaultBaseURL,
		AuthProfiles: map[string]AuthProfile{
			AuthModeKimiWeb: {AccessToken: "still-good", RefreshToken: "revoked-refresh"},
		},
	}
	if got := app.refreshKimiToken(s); got.AccessToken != "still-good" {
		t.Fatalf("failed refresh replaced the token: %q", got.AccessToken)
	}
}

func TestNormalizeSettingsCanonicalizesKimiBaseURL(t *testing.T) {
	got := normalizeSettings(Settings{
		ActiveAuthMode: AuthModeKimiWeb,
		AccessToken:    "token",
		BaseURL:        "https://evil.example.com/v1",
	})
	if got.BaseURL != kimicode.DefaultBaseURL {
		t.Fatalf("base url = %q, want %q", got.BaseURL, kimicode.DefaultBaseURL)
	}
	// The international coding endpoint must survive normalization.
	global := normalizeSettings(Settings{
		ActiveAuthMode: AuthModeKimiWeb,
		AccessToken:    "token",
		BaseURL:        kimicode.GlobalBaseURL,
	})
	if global.BaseURL != kimicode.GlobalBaseURL {
		t.Fatalf("global base url = %q, want %q", global.BaseURL, kimicode.GlobalBaseURL)
	}
}

func TestKimiAuthProfileReadyWithoutBaseURL(t *testing.T) {
	ready := authProfileReady(Settings{
		ActiveAuthMode: AuthModeKimiWeb,
		AccessToken:    "kimi-token",
	})
	if !ready {
		t.Fatal("Kimi login with a token should be ready; the coding endpoint is pinned")
	}
}

func TestKimiAuthModeCatalogIsDeviceLogin(t *testing.T) {
	var info *AuthModeInfo
	for i := range authModeCatalog() {
		if authModeCatalog()[i].ID == AuthModeKimiWeb {
			info = &authModeCatalog()[i]
		}
	}
	if info == nil {
		t.Fatal("Kimi auth mode missing from catalog")
	}
	if info.Kind != "kimi_device" {
		t.Fatalf("Kimi auth mode kind = %q, want kimi_device", info.Kind)
	}
	if strings.Contains(info.Hint, "粘贴") {
		t.Fatalf("Kimi hint still describes pasting a key: %q", info.Hint)
	}
}

// The approval URL carries the user code, so it must never reach the log file.
func TestKimiBrowserOpenFailureOmitsURL(t *testing.T) {
	reason := browserOpenReason(&url.Error{Op: "open", URL: "https://www.kimi.com/code?user_code=SECRET", Err: http.ErrNotSupported})
	if strings.Contains(reason, "SECRET") {
		t.Fatalf("browser open reason leaked the user code: %q", reason)
	}
}

// A Kimi login replaces the upstream, so it must arm the restart itself rather
// than inherit whatever flag the mode selection left behind.
func TestKimiLoginArmsRelaunchOnSuccess(t *testing.T) {
	kimiTestEnv(t)
	t.Setenv("AICODER_SKIP_CODEXPROXY_RELAUNCH", "1")
	fake := newKimiFakeAuthServer(t)
	t.Setenv("KIMI_CODE_OAUTH_HOST", fake.URL)

	app := NewApp()
	// A mode switch that could not complete leaves the flag armed.
	app.setRelaunchAfterAuth(true)

	if err := writeSettings(Settings{
		ListenAddress:  "127.0.0.1:0",
		APIKey:         "local-key",
		ActiveAuthMode: AuthModeKimiWeb,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := app.StartKimiWebLogin(); err != nil {
		t.Fatal(err)
	}
	status, err := app.WaitKimiWebLogin()
	if err != nil {
		t.Fatalf("WaitKimiWebLogin: %v", err)
	}
	if !status.RelaunchScheduled {
		t.Fatal("successful Kimi login did not schedule a relaunch; the new upstream would never take effect")
	}
	if status.Settings.ActiveAuthMode != AuthModeKimiWeb {
		t.Fatalf("active mode = %q, want kimi_web", status.Settings.ActiveAuthMode)
	}
}

// The pending-restart flag must be consumed exactly once, so two concurrent
// callers cannot each spawn a restart process.
func TestRelaunchAfterAuthIsConsumedOnce(t *testing.T) {
	app := NewApp()
	app.setRelaunchAfterAuth(true)
	if !app.takeRelaunchAfterAuth() {
		t.Fatal("first take should report the pending relaunch")
	}
	if app.takeRelaunchAfterAuth() {
		t.Fatal("second take should not report a relaunch")
	}
}

func TestRelaunchGraceOutlastsTheLoginResult(t *testing.T) {
	if relaunchGrace <= 0 {
		t.Fatal("relaunch grace must be positive")
	}
	// The window must survive long enough for the login call to return its
	// result to the frontend before the process quits.
	if relaunchGrace < time.Second {
		t.Fatalf("relaunch grace = %v, too short to deliver the login result", relaunchGrace)
	}
}