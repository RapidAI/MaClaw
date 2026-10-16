package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/qoder"
	"github.com/RapidAI/CodeClaw/corelib/workbuddy"
)

func TestQoderAndWorkBuddyModesAreListed(t *testing.T) {
	want := map[string]string{
		AuthModeQoderCN:         "Qoder 国内版",
		AuthModeQoderGlobal:     "Qoder 国际版",
		AuthModeWorkBuddyCN:     "WorkBuddy 国内版",
		AuthModeWorkBuddyGlobal: "WorkBuddy 国际版",
	}
	found := map[string]bool{}
	for _, info := range authModeCatalog() {
		name, ok := want[info.ID]
		if !ok {
			continue
		}
		found[info.ID] = true
		if info.Name != name || info.Kind == "" {
			t.Fatalf("catalog entry = %+v", info)
		}
		if !validAuthMode(info.ID) || defaultAuthBaseURL(info.ID) == "" || defaultAuthModelID(info.ID) == "" {
			t.Fatalf("mode %s is not ready", info.ID)
		}
	}
	if len(found) != len(want) {
		t.Fatalf("missing modes: %+v", found)
	}
}

func TestVendorProfilesKeepAccountFields(t *testing.T) {
	qoderSettings := normalizeSettings(Settings{
		ActiveAuthMode: AuthModeQoderGlobal,
		AccessToken:    "qoder-access",
		BaseURL:        "https://evil.example/v1",
		Email:          "ada",
		AuthProfiles: map[string]AuthProfile{
			AuthModeQoderGlobal: {
				UserID: "uid-1", RefreshToken: "qoder-refresh", ExpiresAt: 99,
			},
		},
	})
	if qoderSettings.BaseURL != qoder.ChatBase {
		t.Fatalf("qoder base = %s", qoderSettings.BaseURL)
	}
	if qoderSettings.AuthProfiles[AuthModeQoderGlobal].UserID != "uid-1" || qoderSettings.AuthProfiles[AuthModeQoderGlobal].RefreshToken != "qoder-refresh" {
		t.Fatalf("qoder profile = %+v", qoderSettings.AuthProfiles[AuthModeQoderGlobal])
	}

	workSettings := normalizeSettings(Settings{
		ActiveAuthMode: AuthModeWorkBuddyCN,
		AccessToken:    "wb-access",
		BaseURL:        "https://evil.example/v1",
		AuthProfiles: map[string]AuthProfile{
			AuthModeWorkBuddyCN: {
				UserID: "user-1", EnterpriseID: "ent-1", Domain: "dept", RefreshToken: "wb-refresh", ExpiresAt: 99,
			},
		},
	})
	if workSettings.BaseURL != workbuddy.ChinaProfile().ChatURL {
		t.Fatalf("workbuddy base = %s", workSettings.BaseURL)
	}
	profile := workSettings.AuthProfiles[AuthModeWorkBuddyCN]
	if profile.UserID != "user-1" || profile.EnterpriseID != "ent-1" || profile.Domain != "dept" || profile.RefreshToken != "wb-refresh" {
		t.Fatalf("workbuddy profile = %+v", profile)
	}
}

func TestWorkBuddyLoginURLAllowlist(t *testing.T) {
	china := workbuddy.ChinaProfile()
	global := workbuddy.GlobalProfile()
	if !workbuddyLoginURLAllowed(china, "https://www.codebuddy.cn/login") {
		t.Fatal("china login host rejected")
	}
	if !workbuddyLoginURLAllowed(global, "https://www.workbuddy.ai/login") {
		t.Fatal("global login host rejected")
	}
	if workbuddyLoginURLAllowed(global, "http://www.workbuddy.ai/login") || workbuddyLoginURLAllowed(china, "https://evil.example/login") {
		t.Fatal("untrusted login host accepted")
	}
	if workbuddyLoginURLAllowed(global, "https://www.workbuddy.ai.evil.example/login") || workbuddyLoginURLAllowed(china, "https://eviltencent.com/login") || workbuddyLoginURLAllowed(global, "https://user@www.workbuddy.ai/login") {
		t.Fatal("lookalike login host accepted")
	}
	if workbuddyLoginURLAllowed(global, "https://www.tencent.com/login") {
		t.Fatal("global edition accepted a china host")
	}
}

func TestVendorAccessDueWindow(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	if vendorAccessDue(AuthModeQoderCN, 0, now) {
		t.Fatal("missing expiry should not refresh")
	}
	if vendorAccessDue(AuthModeQoderCN, now.Add(11*time.Minute).Unix(), now) {
		t.Fatal("qoder token inside the safe window was treated as due")
	}
	if !vendorAccessDue(AuthModeQoderCN, now.Add(9*time.Minute).Unix(), now) {
		t.Fatal("qoder token near expiry was not due")
	}
	if vendorAccessDue(AuthModeWorkBuddyGlobal, now.Add(6*time.Minute).Unix(), now) {
		t.Fatal("workbuddy token outside the 5 minute window was due")
	}
	if !vendorAccessDue(AuthModeWorkBuddyCN, now.Add(4*time.Minute).Unix(), now) {
		t.Fatal("workbuddy token near expiry was not due")
	}
}

func TestQoderCancelStopsWait(t *testing.T) {
	login, err := qoder.StartLogin("test-machine")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	app := NewApp()
	app.qoderLogin = &qoderLoginFlow{
		ctx: ctx, cancel: cancel, login: login,
		profile: qoder.CNProfile(), mode: AuthModeQoderCN,
	}
	go func() {
		time.Sleep(50 * time.Millisecond)
		app.CancelQoderLogin()
	}()
	done := make(chan error, 1)
	go func() {
		_, waitErr := app.WaitQoderLogin()
		done <- waitErr
	}()
	select {
	case waitErr := <-done:
		if waitErr == nil || !strings.Contains(waitErr.Error(), "取消") {
			t.Fatalf("wait err = %v", waitErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancel did not stop the Qoder wait")
	}
}

func TestApplyVendorLoginRejectsOtherMode(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("AICODER_SKIP_CODEXPROXY_RELAUNCH", "1")
	t.Setenv("AICODER_SKIP_CODEX_PROCESS_KILL", "1")
	app := NewApp()
	if err := writeSettings(Settings{
		ListenAddress:  "127.0.0.1:0",
		APIKey:         "local-key",
		ActiveAuthMode: AuthModeDevin,
		AccessToken:    "devin-token",
	}); err != nil {
		t.Fatal(err)
	}
	app.setRelaunchAfterAuth(true)
	_, err := app.applyVendorLogin(vendorCredential{
		Mode:        AuthModeQoderCN,
		AccessToken: "qoder-access",
		UserID:      "uid-1",
	})
	if err == nil {
		t.Fatal("expected mode mismatch")
	}
	if app.takeRelaunchAfterAuth() {
		t.Fatal("failed login left a relaunch armed")
	}
	got, err := loadSettings()
	if err != nil {
		t.Fatal(err)
	}
	if got.AccessToken != "devin-token" || got.AuthProfiles[AuthModeQoderCN].UserID != "" {
		t.Fatal("mismatched login changed the saved account")
	}
}

func TestQoderModelCatalogSkipsDisabledAndDoesNotCacheFailure(t *testing.T) {
	previous := loadQoderModels
	t.Cleanup(func() { loadQoderModels = previous })
	var calls int
	loadQoderModels = func(context.Context, qoder.Profile, string, string) ([]qoder.Model, string, error) {
		calls++
		if calls == 1 {
			return nil, "", errors.New("catalog down")
		}
		return []qoder.Model{
			{Key: "off", DisplayName: "Off", Enabled: false},
			{Key: "later", DisplayName: "Later", Enabled: true},
			{Key: "best", DisplayName: "Best", Enabled: true},
		}, "best", nil
	}
	upstream := &qoderChatUpstream{profile: qoder.CNProfile()}
	first, err := upstream.DoModels(context.Background(), "", "token", "")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(first.Body)
	_ = first.Body.Close()
	if !strings.Contains(string(body), qoder.DefaultModel) {
		t.Fatalf("fallback body = %s", body)
	}
	if !upstream.modelsAt.IsZero() {
		t.Fatal("failed catalog was cached")
	}
	second, err := upstream.DoModels(context.Background(), "", "token", "")
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(second.Body)
	_ = second.Body.Close()
	text := string(body)
	if strings.Contains(text, "off") || !strings.Contains(text, "best") {
		t.Fatalf("catalog body = %s", text)
	}
	if !strings.HasPrefix(strings.TrimSpace(text), "{") || !strings.Contains(text, `"id":"best"`) {
		t.Fatalf("default model was not first: %s", text)
	}
	best := strings.Index(text, `"id":"best"`)
	later := strings.Index(text, `"id":"later"`)
	if best < 0 || later < 0 || best > later {
		t.Fatalf("model order = %s", text)
	}
	if _, err := upstream.DoModels(context.Background(), "", "token", ""); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("catalog calls = %d", calls)
	}
}

func TestApplyQoderLoginPersistsAccount(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("AICODER_SKIP_CODEXPROXY_RELAUNCH", "1")
	t.Setenv("AICODER_SKIP_CODEX_PROCESS_KILL", "1")

	app := NewApp()
	if err := writeSettings(Settings{
		ListenAddress:  "127.0.0.1:0",
		APIKey:         "local-key",
		ActiveAuthMode: AuthModeQoderCN,
	}); err != nil {
		t.Fatal(err)
	}
	status, err := app.applyVendorLogin(vendorCredential{
		Mode:         AuthModeQoderCN,
		BaseURL:      "https://evil.example/v1",
		AccessToken:  "qoder-access",
		RefreshToken: "qoder-refresh",
		Email:        "ada",
		UserID:       "uid-1",
		ExpiresAt:    time.Now().Add(24 * time.Hour).Unix(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !status.LoggedIn {
		t.Fatal("not logged in")
	}
	got, err := loadSettings()
	if err != nil {
		t.Fatal(err)
	}
	if got.BaseURL != qoder.ChatBase || got.AccessToken != "qoder-access" || got.Email != "ada" || got.ModelID != qoder.DefaultModel {
		t.Fatalf("settings = %+v", got)
	}
	profile := got.AuthProfiles[AuthModeQoderCN]
	if profile.UserID != "uid-1" || profile.RefreshToken != "qoder-refresh" {
		t.Fatalf("profile = %+v", profile)
	}
}

func TestWorkBuddyChatTranslatesAndStripsMarker(t *testing.T) {
	var gotUser, gotMarker, gotProduct, gotRefresh string
	var streamed bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUser = r.Header.Get("X-User-Id")
		gotMarker = r.Header.Get(workbuddy.MarkerHeader)
		gotProduct = r.Header.Get("X-Product")
		gotRefresh = r.Header.Get("X-Refresh-Token")
		raw, _ := io.ReadAll(r.Body)
		var payload map[string]any
		_ = json.Unmarshal(raw, &payload)
		streamed, _ = payload["stream"].(bool)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\n")
	}))
	defer srv.Close()

	upstream := &workbuddyChatUpstream{
		profile: workbuddy.GlobalProfile(),
		cred: &rotatingCredential{
			mode: AuthModeWorkBuddyGlobal, access: "access-token", refresh: "refresh-secret",
			userID: "user-1", enterpriseID: "ent-1",
		},
		client: workbuddy.WrapClient(srv.Client()),
	}
	resp, err := upstream.DoChatCompletions(context.Background(), srv.URL+"/v1/chat/completions", "access-token", "codex", []byte(`{"model":"gpt-5.4","messages":[{"role":"user","content":"hi"}]}`), "application/json", false)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if gotUser != "user-1" || gotProduct != workbuddy.Product || gotRefresh == "" {
		t.Fatal("account headers were not forwarded")
	}
	if gotMarker != "" {
		t.Fatal("translation marker left the process")
	}
	if !streamed {
		t.Fatal("chat body was not translated")
	}
	if strings.Contains(gotRefresh, " ") {
		t.Fatal("refresh header was not a single token")
	}
}

type stubVendorRotator struct {
	settings Settings
	err      error
	calls    int
}

func (s *stubVendorRotator) rotateVendorCredential() (Settings, error) {
	s.calls++
	if s.err != nil {
		return Settings{}, s.err
	}
	return s.settings, nil
}

func TestWorkBuddyChatUsesRefreshedToken(t *testing.T) {
	var auth, refresh string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		refresh = r.Header.Get("X-Refresh-Token")
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[]}\n\n")
	}))
	defer srv.Close()
	rotator := &stubVendorRotator{settings: Settings{
		ActiveAuthMode: AuthModeWorkBuddyGlobal,
		AccessToken:    "rotated-access",
		AuthProfiles: map[string]AuthProfile{
			AuthModeWorkBuddyGlobal: {RefreshToken: "rotated-refresh", UserID: "user-9", ExpiresAt: time.Now().Add(2 * time.Hour).Unix()},
		},
	}}
	upstream := &workbuddyChatUpstream{
		profile: workbuddy.GlobalProfile(),
		cred: &rotatingCredential{
			mode: AuthModeWorkBuddyGlobal, access: "stale-access", refresh: "stale-refresh",
			expires: time.Now().Add(-time.Minute).Unix(), userID: "user-1", rotator: rotator,
		},
		client: workbuddy.WrapClient(srv.Client()),
	}
	body := []byte(`{"model":"gpt-5.4","messages":[{"role":"user","content":"hi"}]}`)
	resp, err := upstream.DoChatCompletions(context.Background(), srv.URL+"/v1/chat/completions", "stale-access", "codex", body, "application/json", true)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if auth != "Bearer rotated-access" || refresh != "rotated-refresh" {
		t.Fatal("chat kept the stale account token")
	}
	resp, err = upstream.DoChatCompletions(context.Background(), srv.URL+"/v1/chat/completions", "stale-access", "codex", body, "application/json", true)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if rotator.calls != 1 {
		t.Fatalf("refresh calls = %d", rotator.calls)
	}
}

func TestVendorRefreshFailureBacksOff(t *testing.T) {
	rotator := &stubVendorRotator{err: errors.New("upstream unavailable")}
	cred := &rotatingCredential{
		mode: AuthModeQoderCN, access: "stale", refresh: "refresh",
		expires: time.Now().Add(-time.Minute).Unix(), rotator: rotator,
	}
	cred.maybeRotate()
	cred.maybeRotate()
	if rotator.calls != 1 {
		t.Fatalf("refresh calls = %d", rotator.calls)
	}
	if access, _, _, _, _ := cred.snapshot(""); access != "stale" {
		t.Fatal("failed refresh replaced the current token")
	}
}
