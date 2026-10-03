package guiapp

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCloudWorkspaceShareJoinErrorsLocalized(t *testing.T) {
	t.Cleanup(func() { setAgentViewLang("zh-Hans") })

	setAgentViewLang("zh-Hans")
	err := cloudWorkspaceAPIError(400, []byte(`{"code":"CLOUD_WORKSPACE_SHARE_SELF","message":"cannot accept your own cloud workspace share"}`))
	if err == nil || !strings.Contains(err.Error(), "自己的云端工作区") {
		t.Fatalf("zh-Hans self err=%v", err)
	}
	if !errors.Is(err, errCloudWorkspaceShareSelf) {
		t.Fatalf("zh-Hans self must unwrap: %v", err)
	}
	msg, typ := cloudWorkspaceShareImportToast(err)
	if typ != "info" || !strings.Contains(msg, "自己的云端工作区") || strings.Contains(msg, "cannot accept") || strings.Contains(msg, "失败") {
		t.Fatalf("zh-Hans self toast=%q typ=%q", msg, typ)
	}

	setAgentViewLang("zh-Hant")
	if got := cloudWorkspaceShareJoinErrorText(err); !strings.Contains(got, "自己的雲端工作區") {
		t.Fatalf("zh-Hant self=%q", got)
	}

	setAgentViewLang("en")
	if got := err.Error(); !strings.Contains(got, "your own cloud workspace") {
		t.Fatalf("en self=%q", got)
	}
	if got := cloudWorkspaceShareJoinErrorText(fmt.Errorf("cannot accept your own cloud workspace share")); !strings.Contains(got, "your own cloud workspace") {
		t.Fatalf("en fallback=%q", got)
	}

	session := cloudWorkspaceAPIError(401, []byte(`{"code":"CLOUD_WORKSPACE_SESSION_REQUIRED","message":"cloud workspace instance session required"}`))
	if session == nil || strings.Contains(session.Error(), "instance session required") {
		t.Fatalf("en session err=%v", session)
	}

	setAgentViewLang("zh-Hans")
	revoked := cloudWorkspaceAPIError(404, []byte(`{"code":"NOT_FOUND","message":"cloud workspace share is revoked"}`))
	if !errors.Is(revoked, errCloudWorkspaceShareRevoked) {
		t.Fatalf("revoked unwrap=%v", revoked)
	}
	failMsg, failTyp := cloudWorkspaceShareImportToast(revoked)
	if failTyp != "error" || !strings.Contains(failMsg, "已停止分享") || strings.Contains(failMsg, "7 天") {
		t.Fatalf("revoked toast=%q typ=%q", failMsg, failTyp)
	}
}

func TestCloudWorkspaceCorruptObjectErrorLocalized(t *testing.T) {
	t.Cleanup(func() { setAgentViewLang("zh-Hans") })

	setAgentViewLang("zh-Hans")
	err := cloudWorkspaceAPIError(500, []byte(`{"code":"CLOUD_WORKSPACE_OBJECT_CORRUPT","message":"cloud workspace object is corrupt"}`))
	if err == nil || strings.Contains(err.Error(), "corrupt") || !strings.Contains(err.Error(), "已损坏") {
		t.Fatalf("zh-Hans corrupt=%v", err)
	}
	setAgentViewLang("zh-Hant")
	err = cloudWorkspaceAPIError(500, []byte(`{"code":"CLOUD_WORKSPACE_OBJECT_CORRUPT","message":"cloud workspace object is corrupt"}`))
	if err == nil || !strings.Contains(err.Error(), "已損壞") {
		t.Fatalf("zh-Hant corrupt=%v", err)
	}
	setAgentViewLang("en")
	err = cloudWorkspaceAPIError(500, []byte(`{"code":"CLOUD_WORKSPACE_OBJECT_CORRUPT","message":"cloud workspace object is corrupt"}`))
	if err == nil || !strings.Contains(err.Error(), "damaged") {
		t.Fatalf("en corrupt=%v", err)
	}
}

func TestParseCloudWorkspaceShareToken(t *testing.T) {
	cases := map[string]string{
		"abc123": "abc123",
		"https://hub.example.test/hub/cloud-workspaces/shares/tok_1":            "tok_1",
		"https://hub.example.test/hub/cloud-workspaces/shares/tok_1/extra":      "tok_1",
		"https://hub.example.test/api/v1/cloud-workspace-shares/tok_api/accept": "tok_api",
		"https://hub.example.test/api/v1/cloud-workspace-shares/tok_plain":      "tok_plain",
		"maclaw://cloud-workspace-share?token=tok_2":                            "tok_2",
		"MACLAW://Cloud-Workspace-Share?token=tok_3":                            "tok_3",
	}
	for raw, want := range cases {
		if got := parseCloudWorkspaceShareToken(raw); got != want {
			t.Fatalf("%q: got %q want %q", raw, got, want)
		}
	}
}

func TestCloudWorkspaceShareFromArgs(t *testing.T) {
	got := cloudWorkspaceShareFromArgs([]string{"--foo", "maclaw://cloud-workspace-share?token=joinme&hub_url=https%3A%2F%2Fhub.example"})
	if got.Token != "joinme" || got.HubURL != "https://hub.example" {
		t.Fatalf("got %+v", got)
	}
	if got := cloudWorkspaceShareFromArgs([]string{"plain"}); got.Token != "" {
		t.Fatalf("plain arg %+v", got)
	}
	if got := cloudWorkspaceShareFromArgs([]string{"maclaw://onboarding?referral_handoff=abc"}); got.Token != "" {
		t.Fatalf("onboarding treated as share: %+v", got)
	}
	if got := parseCloudWorkspaceShareToken("https://example.test/other"); got != "" {
		t.Fatalf("unrelated URL token=%q", got)
	}
	if got := parseCloudWorkspaceShareToken("://bad"); got != "" {
		t.Fatalf("invalid URL token=%q", got)
	}
	if got := parseCloudWorkspaceShareHub("maclaw://cloud-workspace-share?token=x&hub_url=javascript:alert(1)"); got != "" {
		t.Fatalf("rejected hub_url leaked: %q", got)
	}
	if got := parseCloudWorkspaceShareHub("https://hub.example.test/api/v1/cloud-workspace-shares/tok_api/accept"); got != "https://hub.example.test" {
		t.Fatalf("accept API hub=%q", got)
	}
	if got := parseCloudWorkspaceShareHub("https://hub.example.test/hub/cloud-workspaces/shares/tok_1"); got != "https://hub.example.test" {
		t.Fatalf("public page hub=%q", got)
	}
}

func TestCloudWorkspaceShareFallbackStatus(t *testing.T) {
	if !cloudWorkspaceShareFallbackStatus(401, "/api/v1/cloud-workspaces/cws_a/manifest") {
		t.Fatal("401 should fall back")
	}
	if !cloudWorkspaceShareFallbackStatus(403, "/api/v1/cloud-workspaces/cws_a/leases") {
		t.Fatal("403 should fall back")
	}
	if !cloudWorkspaceShareFallbackStatus(404, "/api/v1/cloud-workspaces/cws_a/manifest") {
		t.Fatal("workspace 404 should fall back")
	}
	if cloudWorkspaceShareFallbackStatus(404, "/api/v1/cloud-workspaces/cws_a/objects/deadbeef") {
		t.Fatal("object 404 should not fall back")
	}
	if cloudWorkspaceShareFallbackStatus(409, "/api/v1/cloud-workspaces/cws_a/leases") {
		t.Fatal("409 should not fall back")
	}
}

func TestCloudWorkspaceShareWorkingDir(t *testing.T) {
	app := configureCloudWorkspaceEntitlementTestApp(t, "https://hub.example.test")
	writePath := app.cloudWorkspaceShareWorkingDir("cws_share", "write")
	readPath := app.cloudWorkspaceShareWorkingDir("cws_share", "read")
	if !strings.Contains(writePath, "cloud-workspaces") || strings.Contains(writePath, "cloud-workspaces-readonly") {
		t.Fatalf("write path=%q", writePath)
	}
	if !strings.Contains(readPath, "cloud-workspaces-readonly") {
		t.Fatalf("read path=%q", readPath)
	}
}

func TestPostAcceptCloudWorkspaceShareSameHubDoesNotNeedInstanceSession(t *testing.T) {
	var sawSessionHeader bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/api/v1/cloud-workspace-shares/") && strings.HasSuffix(r.URL.Path, "/accept") {
			if r.Header.Get(cloudWorkspaceInstanceSessionHeader) != "" {
				sawSessionHeader = true
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"workspace_id":"cws_shared","name":"分享课题","share_permission":"read","owner_email":"u1@x.com"}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	app := configureCloudWorkspaceEntitlementTestApp(t, server.URL)
	resetCloudWorkspaceInstanceSessions()
	t.Cleanup(resetCloudWorkspaceInstanceSessions)
	ctx, cancel := app.cloudWorkspaceRequestContext()
	defer cancel()
	data, status, err := app.postAcceptCloudWorkspaceShare(ctx, "tok_join", CloudWorkspaceShareLaunch{Token: "tok_join"})
	if err != nil {
		t.Fatalf("postAcceptCloudWorkspaceShare: %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("status=%d body=%s", status, data)
	}
	if sawSessionHeader {
		t.Fatal("same-hub accept sent an instance session on the first request")
	}
}

func TestPostAcceptCloudWorkspaceShareRetriesWhenHubRequiresInstanceSession(t *testing.T) {
	var acceptWithoutSession, acceptWithSession int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost && r.URL.Path == cloudWorkspaceInstanceSessionsPath {
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"session_id":"cwses_share","session_token":"cwst_share","client_instance_id":"cwi_share","protocol":"v1-sequential","expires_at":"2099-01-01T00:00:00Z"}`))
			return
		}
		if r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/api/v1/cloud-workspace-shares/") && strings.HasSuffix(r.URL.Path, "/accept") {
			if r.Header.Get(cloudWorkspaceInstanceSessionHeader) == "" {
				acceptWithoutSession++
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"code":"CLOUD_WORKSPACE_SESSION_REQUIRED","message":"cloud workspace instance session required"}`))
				return
			}
			acceptWithSession++
			_, _ = w.Write([]byte(`{"workspace_id":"cws_shared","name":"分享课题","share_permission":"read","owner_email":"u1@x.com"}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	app := configureCloudWorkspaceEntitlementTestApp(t, server.URL)
	resetCloudWorkspaceInstanceSessions()
	t.Cleanup(resetCloudWorkspaceInstanceSessions)
	ctx, cancel := app.cloudWorkspaceRequestContext()
	defer cancel()
	data, status, err := app.postAcceptCloudWorkspaceShare(ctx, "tok_join", CloudWorkspaceShareLaunch{Token: "tok_join"})
	if err != nil {
		t.Fatalf("postAcceptCloudWorkspaceShare: %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("status=%d body=%s", status, data)
	}
	if acceptWithoutSession != 1 || acceptWithSession != 1 {
		t.Fatalf("accept without=%d with=%d", acceptWithoutSession, acceptWithSession)
	}
}

func TestPostAcceptCloudWorkspaceShareDoesNotSendCachedSession(t *testing.T) {
	var sawSessionHeader bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/api/v1/cloud-workspace-shares/") && strings.HasSuffix(r.URL.Path, "/accept") {
			if r.Header.Get(cloudWorkspaceInstanceSessionHeader) != "" {
				sawSessionHeader = true
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"workspace_id":"cws_shared","name":"分享课题","share_permission":"read"}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	app := configureCloudWorkspaceEntitlementTestApp(t, server.URL)
	ctx, cancel := app.cloudWorkspaceRequestContext()
	defer cancel()
	if _, status, err := app.postAcceptCloudWorkspaceShare(ctx, "tok_join", CloudWorkspaceShareLaunch{Token: "tok_join"}); err != nil || status != http.StatusOK {
		t.Fatalf("status=%d err=%v", status, err)
	}
	if sawSessionHeader {
		t.Fatal("cached instance session must not be attached to share accept")
	}
}

func TestPostAcceptCloudWorkspaceShareKeepsNonSessionErrors(t *testing.T) {
	var acceptCalls, sessionCalls, claimCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost && r.URL.Path == cloudWorkspaceInstanceSessionsPath {
			sessionCalls++
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"session_id":"cwses_share","session_token":"cwst_share","client_instance_id":"cwi_share","protocol":"v1-sequential","expires_at":"2099-01-01T00:00:00Z"}`))
			return
		}
		if r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/api/v1/cloud-workspace-shares/") && strings.HasSuffix(r.URL.Path, "/accept") {
			if r.Header.Get("Authorization") == "" {
				claimCalls++
			}
			acceptCalls++
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"code":"NOT_FOUND","message":"cloud workspace share is revoked"}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	app := configureCloudWorkspaceEntitlementTestApp(t, server.URL)
	resetCloudWorkspaceInstanceSessions()
	t.Cleanup(resetCloudWorkspaceInstanceSessions)
	ctx, cancel := app.cloudWorkspaceRequestContext()
	defer cancel()
	data, status, err := app.postAcceptCloudWorkspaceShare(ctx, "tok_join", CloudWorkspaceShareLaunch{Token: "tok_join"})
	if err != nil {
		t.Fatalf("postAcceptCloudWorkspaceShare: %v", err)
	}
	if status != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", status, data)
	}
	if acceptCalls != 1 || sessionCalls != 0 || claimCalls != 0 {
		t.Fatalf("accept=%d sessions=%d claims=%d", acceptCalls, sessionCalls, claimCalls)
	}
}

func TestPostAcceptCloudWorkspaceShareDoesNotClaimAfterShareSelf(t *testing.T) {
	var claimCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost && r.URL.Path == cloudWorkspaceInstanceSessionsPath {
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"session_id":"cwses_share","session_token":"cwst_share","client_instance_id":"cwi_share","protocol":"v1-sequential","expires_at":"2099-01-01T00:00:00Z"}`))
			return
		}
		if r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/api/v1/cloud-workspace-shares/") && strings.HasSuffix(r.URL.Path, "/accept") {
			if r.Header.Get("Authorization") == "" {
				claimCalls++
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"workspace_id":"cws_shared","name":"自己的工作区","share_permission":"read","access_token":"cwss_should_not"}`))
				return
			}
			if r.Header.Get(cloudWorkspaceInstanceSessionHeader) == "" {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"code":"CLOUD_WORKSPACE_SESSION_REQUIRED","message":"cloud workspace instance session required"}`))
				return
			}
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"code":"CLOUD_WORKSPACE_SHARE_SELF","message":"cannot accept your own cloud workspace share"}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	app := configureCloudWorkspaceEntitlementTestApp(t, server.URL)
	resetCloudWorkspaceInstanceSessions()
	t.Cleanup(resetCloudWorkspaceInstanceSessions)
	ctx, cancel := app.cloudWorkspaceRequestContext()
	defer cancel()
	data, status, err := app.postAcceptCloudWorkspaceShare(ctx, "tok_join", CloudWorkspaceShareLaunch{Token: "tok_join"})
	if err != nil {
		t.Fatalf("postAcceptCloudWorkspaceShare: %v", err)
	}
	if status != http.StatusBadRequest || claimCalls != 0 {
		t.Fatalf("status=%d claims=%d body=%s", status, claimCalls, data)
	}
}

func TestPostAcceptCloudWorkspaceShareClaimsWhenSessionCannotBeIssued(t *testing.T) {
	var claimCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost && r.URL.Path == cloudWorkspaceInstanceSessionsPath {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"code":"CLOUD_WORKSPACE_FORBIDDEN","message":"cloud workspace is not enabled for this user"}`))
			return
		}
		if r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/api/v1/cloud-workspace-shares/") && strings.HasSuffix(r.URL.Path, "/accept") {
			if r.Header.Get("Authorization") == "" {
				claimCalls++
				_, _ = w.Write([]byte(`{"workspace_id":"cws_shared","name":"分享课题","share_permission":"read","access_token":"cwss_join"}`))
				return
			}
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"code":"CLOUD_WORKSPACE_SESSION_REQUIRED","message":"cloud workspace instance session required"}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	app := configureCloudWorkspaceEntitlementTestApp(t, server.URL)
	resetCloudWorkspaceInstanceSessions()
	t.Cleanup(resetCloudWorkspaceInstanceSessions)
	ctx, cancel := app.cloudWorkspaceRequestContext()
	defer cancel()
	data, status, err := app.postAcceptCloudWorkspaceShare(ctx, "tok_join", CloudWorkspaceShareLaunch{Token: "tok_join"})
	if err != nil {
		t.Fatalf("postAcceptCloudWorkspaceShare: %v", err)
	}
	if status != http.StatusOK || claimCalls != 1 {
		t.Fatalf("status=%d claims=%d body=%s", status, claimCalls, data)
	}
}

func TestPostAcceptCloudWorkspaceShareSendsPassword(t *testing.T) {
	var got string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/accept") {
			var payload struct {
				Password string `json:"password"`
			}
			_ = json.NewDecoder(r.Body).Decode(&payload)
			got = payload.Password
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"workspace_id":"cws_shared","name":"分享课题","share_permission":"read"}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	app := configureCloudWorkspaceEntitlementTestApp(t, server.URL)
	resetCloudWorkspaceInstanceSessions()
	t.Cleanup(resetCloudWorkspaceInstanceSessions)
	ctx, cancel := app.cloudWorkspaceRequestContext()
	defer cancel()
	if _, status, err := app.postAcceptCloudWorkspaceShareWithPassword(ctx, "tok_join", CloudWorkspaceShareLaunch{Token: "tok_join"}, "s3cret"); err != nil || status != http.StatusOK {
		t.Fatalf("status=%d err=%v", status, err)
	}
	if got != "s3cret" {
		t.Fatalf("password=%q", got)
	}
}

func TestShareAccessSessionDroppedOnRemoteUserSwitch(t *testing.T) {
	resetCloudWorkspaceShareAccessCache()
	t.Cleanup(resetCloudWorkspaceShareAccessCache)
	app := configureCloudWorkspaceEntitlementTestApp(t, "https://hub.example.test")
	shareAccessRemoteUserOverride = "u1"
	app.saveShareAccessSession(cloudWorkspaceShareAccessSession{
		WorkspaceID: "cws_share",
		HubURL:      "https://hub.example.test",
		AccessToken: "cwss_testtoken",
	})
	if _, ok := app.lookupShareAccessSession("cws_share"); !ok {
		t.Fatal("missing session for current user")
	}
	shareAccessRemoteUserOverride = "u2"
	if _, ok := app.lookupShareAccessSession("cws_share"); ok {
		t.Fatal("session survived account switch")
	}
}

func TestShareAccessSessionKeptOnFirstLogin(t *testing.T) {
	resetCloudWorkspaceShareAccessCache()
	t.Cleanup(resetCloudWorkspaceShareAccessCache)
	app := configureCloudWorkspaceEntitlementTestApp(t, "https://hub.example.test")
	app.saveShareAccessSession(cloudWorkspaceShareAccessSession{
		WorkspaceID: "cws_share",
		HubURL:      "https://hub.example.test",
		AccessToken: "cwss_testtoken",
	})
	if _, ok := app.lookupShareAccessSession("cws_share"); !ok {
		t.Fatal("missing session before login")
	}
	shareAccessRemoteUserOverride = "u1"
	if _, ok := app.lookupShareAccessSession("cws_share"); !ok {
		t.Fatal("session dropped on first login")
	}
}

func TestShareAccessSessionDroppedOnRestartAsDifferentUser(t *testing.T) {
	resetCloudWorkspaceShareAccessCache()
	t.Cleanup(resetCloudWorkspaceShareAccessCache)
	app := configureCloudWorkspaceEntitlementTestApp(t, "https://hub.example.test")
	shareAccessRemoteUserOverride = "u1"
	app.saveShareAccessSession(cloudWorkspaceShareAccessSession{
		WorkspaceID: "cws_share",
		HubURL:      "https://hub.example.test",
		AccessToken: "cwss_testtoken",
	})
	resetCloudWorkspaceShareAccessCache()
	shareAccessRemoteUserOverride = "u2"
	if _, ok := app.lookupShareAccessSession("cws_share"); ok {
		t.Fatal("session survived restart as a different user")
	}
}

func TestDropShareAccessSessionRemovesWorkspace(t *testing.T) {
	resetCloudWorkspaceShareAccessCache()
	t.Cleanup(resetCloudWorkspaceShareAccessCache)
	app := configureCloudWorkspaceEntitlementTestApp(t, "https://hub.example.test")
	app.saveShareAccessSession(cloudWorkspaceShareAccessSession{
		WorkspaceID: "cws_share",
		HubURL:      "https://hub.example.test",
		AccessToken: "cwss_testtoken",
	})
	app.saveShareAccessSession(cloudWorkspaceShareAccessSession{
		WorkspaceID: "cws_keep",
		HubURL:      "https://hub.example.test",
		AccessToken: "cwss_keep",
	})
	app.dropShareAccessSession("cws_share")
	if _, ok := app.lookupShareAccessSession("cws_share"); ok {
		t.Fatal("dropped session still present")
	}
	if _, ok := app.lookupShareAccessSession("cws_keep"); !ok {
		t.Fatal("unrelated session removed")
	}
}
