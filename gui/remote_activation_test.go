package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/configfile"
	"github.com/RapidAI/CodeClaw/corelib/remote"
	"github.com/gorilla/websocket"
)

func TestNormalizedRemotePlatform(t *testing.T) {
	original := remotePlatformGOOS
	defer func() {
		remotePlatformGOOS = original
	}()

	cases := map[string]string{
		"windows": "windows",
		"darwin":  "mac",
		"linux":   "linux",
		"freebsd": "linux",
	}

	for goos, want := range cases {
		remotePlatformGOOS = func() string { return goos }
		if got := normalizedRemotePlatform(); got != want {
			t.Fatalf("normalizedRemotePlatform() for %q = %q, want %q", goos, got, want)
		}
	}
}

func TestNormalizeRemoteRegistrationPhoneNumber(t *testing.T) {
	got := normalizeRemoteRegistrationPhoneNumber(" 199-0000 1111 ")
	if got != "19900001111" {
		t.Fatalf("normalize phone = %q, want 19900001111", got)
	}
	if short := normalizeRemoteRegistrationPhoneNumber("12-3"); len(short) >= 6 {
		t.Fatalf("short phone normalized to unexpectedly valid value %q", short)
	}
}

func TestSkillMarketAccountFromEnrollPrefersStableUserID(t *testing.T) {
	got := skillMarketAccountFromEnroll(&remote.EnrollResult{
		UserID: "usr_123",
		Email:  "user@example.com",
	}, "19900001111")
	if got != "usr_123" {
		t.Fatalf("account = %q, want usr_123", got)
	}
}

func TestSkillMarketAccountFromEnrollFallsBackForPhoneRegistration(t *testing.T) {
	if got := skillMarketAccountFromEnroll(&remote.EnrollResult{Email: "user@example.com"}, "19900001111"); got != "user@example.com" {
		t.Fatalf("email fallback = %q", got)
	}
	if got := skillMarketAccountFromEnroll(&remote.EnrollResult{}, "199-0000 1111"); got != "phone:19900001111" {
		t.Fatalf("phone fallback = %q, want phone:19900001111", got)
	}
}

func TestGetRemoteRegistrationAuthDefaultsMissingCodeLengthToSix(t *testing.T) {
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/enroll/registration-auth" {
			t.Fatalf("unexpected hub path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"method":"phone","code_ttl_minutes":5}`))
	}))
	defer hub.Close()

	app := &App{}
	got, err := app.GetRemoteRegistrationAuth(hub.URL, "")
	if err != nil {
		t.Fatalf("GetRemoteRegistrationAuth() error = %v", err)
	}
	if got.Method != "phone" || got.CodeTTLMinutes != 5 || got.CodeLength != 6 {
		t.Fatalf("registration auth = %#v", got)
	}
}

func TestProbeRemoteHubSendsPhoneIdentityAsPhoneNumber(t *testing.T) {
	var seen []map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/entry/probe" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		var payload map[string]string
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		seen = append(seen, payload)
		if payload["phone_number"] != "19900001111" || payload["email"] != "" {
			t.Fatalf("payload = %#v", payload)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","tenant_id":"tenant-phone","tenant_name":"Phone Tenant"}`))
	}))
	defer server.Close()

	got, err := (&App{}).ProbeRemoteHub(server.URL, "phone:199-0000 1111")
	if err != nil {
		t.Fatalf("ProbeRemoteHub() error = %v", err)
	}
	if got.TenantID != "tenant-phone" || got.TenantName != "Phone Tenant" {
		t.Fatalf("probe result = %#v", got)
	}
	if _, err := (&App{}).ProbeRemoteHub(server.URL, "199-0000 1111"); err != nil {
		t.Fatalf("ProbeRemoteHub() plain phone error = %v", err)
	}
	if len(seen) != 2 {
		t.Fatalf("seen payloads = %#v, want two probes", seen)
	}
}

func TestResolveRemoteRegistrationTargetUsesHubCenterPhoneRoute(t *testing.T) {
	remote.InvalidateCenterCache()
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/enroll/registration-auth" {
			t.Fatalf("unexpected hub path %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("tenant_id"); got != "tenant-phone" {
			t.Fatalf("registration auth tenant_id = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"method":"phone","code_length":6,"code_ttl_minutes":5}`))
	}))
	defer hub.Close()

	center := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/client/quality" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"quality_score":100,"routable":true,"service_status":"ok","features":{"can_resolve":true}}`))
			return
		}
		if r.URL.Path == "/api/client/hubcenters" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true,"urls":[],"nodes":[],"count":0,"ttl_seconds":300}`))
			return
		}
		if r.URL.Path != "/api/entry/resolve" {
			t.Fatalf("unexpected center path %s", r.URL.Path)
		}
		var payload map[string]string
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		if payload["email"] != "19900001111" || payload["phone_number"] != "19900001111" {
			t.Fatalf("resolve payload = %#v", payload)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"email":"19900001111","mode":"route","hubs":[{"hub_id":"hub-phone","tenant_id":"tenant-phone","name":"Phone Hub","base_url":"` + hub.URL + `","status":"online"}]}`))
	}))
	defer center.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{RemoteHubCenterURL: center.URL}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	got, err := app.ResolveRemoteRegistrationTarget("19900001111")
	if err != nil {
		t.Fatalf("ResolveRemoteRegistrationTarget() error = %v", err)
	}
	if got.HubURL != hub.URL || got.HubID != "hub-phone" || got.TenantID != "tenant-phone" || got.Method != "phone" || got.CodeLength != 6 {
		t.Fatalf("resolved target = %#v", got)
	}
}

func TestResolveRemoteRegistrationTargetFallsBackToConfiguredHubForMissingPhoneRoute(t *testing.T) {
	remote.InvalidateCenterCache()
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/enroll/registration-auth" {
			t.Fatalf("unexpected hub path %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("tenant_id"); got != "tenant_default" {
			t.Fatalf("registration auth tenant_id = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"method":"phone","code_length":6,"code_ttl_minutes":5}`))
	}))
	defer hub.Close()

	center := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/client/quality" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"quality_score":100,"routable":true,"service_status":"ok","features":{"can_resolve":true}}`))
			return
		}
		if r.URL.Path == "/api/client/hubcenters" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true,"urls":[],"nodes":[],"count":0,"ttl_seconds":300}`))
			return
		}
		if r.URL.Path != "/api/entry/resolve" {
			t.Fatalf("unexpected center path %s", r.URL.Path)
		}
		var payload map[string]string
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		if payload["email"] != "19900001111" || payload["phone_number"] != "19900001111" {
			t.Fatalf("resolve payload = %#v", payload)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"email":"phone:19900001111","mode":"none","message":"No phone route found"}`))
	}))
	defer center.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubCenterURL: center.URL,
		RemoteHubURL:       hub.URL,
		RemoteHubID:        "hub-configured",
		RemoteTenantID:     "tenant_default",
	}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	got, err := app.ResolveRemoteRegistrationTarget("19900001111")
	if err != nil {
		t.Fatalf("ResolveRemoteRegistrationTarget() error = %v", err)
	}
	if got.HubURL != hub.URL || got.HubID != "hub-configured" || got.TenantID != "tenant_default" || got.Method != "phone" || got.CodeLength != 6 {
		t.Fatalf("resolved fallback target = %#v", got)
	}
}

func TestResolveRemoteRegistrationTargetDoesNotFallbackWithoutConfiguredHub(t *testing.T) {
	remote.InvalidateCenterCache()
	center := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/client/quality" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"quality_score":100,"routable":true,"service_status":"ok","features":{"can_resolve":true}}`))
			return
		}
		if r.URL.Path == "/api/client/hubcenters" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true,"urls":[],"nodes":[],"count":0,"ttl_seconds":300}`))
			return
		}
		if r.URL.Path != "/api/entry/resolve" {
			t.Fatalf("unexpected center path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"email":"phone:19900001111","mode":"none","message":"No phone route found"}`))
	}))
	defer center.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{RemoteHubCenterURL: center.URL}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	_, err := app.ResolveRemoteRegistrationTarget("19900001111")
	if err == nil || !strings.Contains(err.Error(), "No phone route found") {
		t.Fatalf("ResolveRemoteRegistrationTarget() error = %v, want No phone route found", err)
	}
}

func TestResolveRemoteRegistrationTargetDoesNotFallbackToEmailWhenAuthConfigFails(t *testing.T) {
	remote.InvalidateCenterCache()
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/enroll/registration-auth" {
			t.Fatalf("unexpected hub path %s", r.URL.Path)
		}
		http.Error(w, "auth config temporarily unavailable", http.StatusBadGateway)
	}))
	defer hub.Close()

	center := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/client/quality" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"quality_score":100,"routable":true,"service_status":"ok","features":{"can_resolve":true}}`))
			return
		}
		if r.URL.Path == "/api/client/hubcenters" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true,"urls":[],"nodes":[],"count":0,"ttl_seconds":300}`))
			return
		}
		if r.URL.Path != "/api/entry/resolve" {
			t.Fatalf("unexpected center path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"email":"19900001111","mode":"route","hubs":[{"hub_id":"hub-phone","tenant_id":"tenant-phone","name":"Phone Hub","base_url":"` + hub.URL + `","status":"online"}]}`))
	}))
	defer center.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{RemoteHubCenterURL: center.URL}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	got, err := app.ResolveRemoteRegistrationTarget("19900001111")
	if err == nil {
		t.Fatalf("ResolveRemoteRegistrationTarget() error = nil, got %#v", got)
	}
	if strings.Contains(err.Error(), "requires email") {
		t.Fatalf("unexpected email fallback error: %v", err)
	}
}

func TestSendRemoteRegistrationSMSIncludesTenantID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/enroll/sms/send-code" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		var payload map[string]string
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		if payload["phone_number"] != "19900001111" || payload["tenant_id"] != "tenant-phone" {
			t.Fatalf("payload = %#v", payload)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"tenant_id":"tenant-phone","code_length":6,"expires_min":5,"purpose":"verify_bound_phone"}`))
	}))
	defer server.Close()

	app := &App{}
	got, err := app.SendRemoteRegistrationSMS(server.URL, "199-0000 1111", "tenant-phone")
	if err != nil {
		t.Fatalf("SendRemoteRegistrationSMS() error = %v", err)
	}
	if got.TenantID != "tenant-phone" || got.CodeLength != 6 || got.Purpose != "verify_bound_phone" {
		t.Fatalf("result = %#v", got)
	}
}

func TestSendRemoteRegistrationSMSPreservesErrorCode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/enroll/sms/send-code" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"code":"PHONE_ALREADY_REGISTERED","message":"phone already registered"}`))
	}))
	defer server.Close()

	app := &App{}
	_, err := app.SendRemoteRegistrationSMS(server.URL, "19900001111", "tenant-phone")
	if err == nil {
		t.Fatal("SendRemoteRegistrationSMS() error = nil")
	}
	if !strings.Contains(err.Error(), "PHONE_ALREADY_REGISTERED") {
		t.Fatalf("error = %v, want PHONE_ALREADY_REGISTERED", err)
	}
}

func TestVerifyRemoteRegistrationContactCodeStoresNormalizedHubResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/enroll/profile/verify" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		var payload map[string]string
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		if payload["kind"] != "email" || payload["email"] != "USER@Example.COM" || payload["tenant_id"] != "tenant-acme" || payload["machine_id"] != "machine-1" || payload["machine_token"] != "token-1" || payload["verify_code"] != "123456" {
			t.Fatalf("payload = %#v", payload)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"kind":"email","email":"user@example.com"}`))
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubURL:       server.URL,
		RemoteTenantID:     "tenant-acme",
		RemoteEmail:        "phone:19900001111",
		RemoteMobile:       "19900001111",
		RemoteMachineID:    "machine-1",
		RemoteMachineToken: "token-1",
	}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	if _, err := app.VerifyRemoteRegistrationContactCode("email", "USER@Example.COM", "123456"); err != nil {
		t.Fatalf("VerifyRemoteRegistrationContactCode() error = %v", err)
	}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if cfg.RemoteEmail != "user@example.com" {
		t.Fatalf("RemoteEmail = %q, want normalized hub response", cfg.RemoteEmail)
	}
	if cfg.RemoteMobile != "19900001111" {
		t.Fatalf("RemoteMobile = %q, want existing phone preserved", cfg.RemoteMobile)
	}
}

func TestRemoteRegistrationContactPhoneUsesRegistrationSMSEndpoints(t *testing.T) {
	var sawSend bool
	var sawVerify bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]string
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/enroll/sms/send-code":
			sawSend = true
			if payload["phone_number"] != "17090134628" || payload["tenant_id"] != "tenant-acme" || payload["machine_id"] != "machine-1" || payload["machine_token"] != "token-1" {
				t.Fatalf("send payload = %#v", payload)
			}
			_, _ = w.Write([]byte(`{"ok":true,"tenant_id":"tenant-acme","code_length":6,"expires_min":5,"purpose":"registration","daily_sms_remaining":4}`))
		case "/api/enroll/sms/verify-and-start":
			sawVerify = true
			if payload["phone_number"] != "17090134628" || payload["verify_code"] != "123456" || payload["tenant_id"] != "tenant-acme" || payload["machine_id"] != "machine-1" || payload["machine_token"] != "token-1" {
				t.Fatalf("verify payload = %#v", payload)
			}
			_, _ = w.Write([]byte(`{"ok":true,"kind":"phone","tenant_id":"tenant-acme","phone_number":"17090134628","email":"bound@example.com"}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubURL:       server.URL,
		RemoteTenantID:     "tenant-acme",
		RemoteEmail:        "phone:17090134628",
		RemoteMachineID:    "machine-1",
		RemoteMachineToken: "token-1",
	}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	sendResult, err := app.SendRemoteRegistrationContactCode("phone", "170 9013 4628")
	if err != nil {
		t.Fatalf("SendRemoteRegistrationContactCode() error = %v", err)
	}
	if sendResult.Kind != "phone" || sendResult.TenantID != "tenant-acme" || sendResult.Purpose != "registration" || sendResult.CodeLength != 6 || sendResult.ExpiresMin != 5 || sendResult.DailySMSRemaining != 4 {
		t.Fatalf("send result = %#v", sendResult)
	}
	if _, err := app.VerifyRemoteRegistrationContactCode("phone", "170 9013 4628", "123456"); err != nil {
		t.Fatalf("VerifyRemoteRegistrationContactCode() error = %v", err)
	}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if !sawSend || !sawVerify {
		t.Fatalf("registration SMS endpoints not both called: send=%v verify=%v", sawSend, sawVerify)
	}
	if cfg.RemoteMobile != "17090134628" {
		t.Fatalf("RemoteMobile = %q, want verified phone", cfg.RemoteMobile)
	}
	if cfg.RemoteEmail != "bound@example.com" {
		t.Fatalf("RemoteEmail = %q, want bound email from phone verification", cfg.RemoteEmail)
	}
}

func TestRemoteRegistrationContactPhoneRequiresMachineCredentialsBeforeSendingSMS(t *testing.T) {
	var called bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		t.Fatalf("SMS endpoint should not be called without machine credentials: %s", r.URL.Path)
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubURL:   server.URL,
		RemoteTenantID: "tenant-acme",
	}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	_, err := app.SendRemoteRegistrationContactCode("phone", "17090134628")
	if err == nil {
		t.Fatal("SendRemoteRegistrationContactCode() error = nil")
	}
	if !strings.Contains(err.Error(), "MACHINE_UNAUTHORIZED") {
		t.Fatalf("error = %v, want MACHINE_UNAUTHORIZED", err)
	}
	if called {
		t.Fatal("SMS endpoint was called")
	}
}

func TestClearRemoteActivationClearsRegistrationContactDetails(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubURL:        "https://hub.example",
		RemoteHubCenterURL:  "https://hubcenter.example",
		RemoteHubCenterURLs: []string{"https://hubcenter.example"},
		RemoteEmail:         "user@example.com",
		RemoteMobile:        "19900001111",
		RemoteMachineID:     "machine-1",
		RemoteMachineToken:  "token-1",
	}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	if err := app.ClearRemoteActivation(); err != nil {
		t.Fatalf("ClearRemoteActivation() error = %v", err)
	}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if cfg.RemoteEmail != "" || cfg.RemoteMobile != "" || cfg.RemoteHubURL != "" || cfg.RemoteHubCenterURL != "" || len(cfg.RemoteHubCenterURLs) != 0 || cfg.RemoteMachineID != "" || cfg.RemoteMachineToken != "" {
		t.Fatalf("remote registration fields not cleared: email=%q mobile=%q hub=%q hubcenter=%q hubcenters=%v machine=%q token=%q", cfg.RemoteEmail, cfg.RemoteMobile, cfg.RemoteHubURL, cfg.RemoteHubCenterURL, cfg.RemoteHubCenterURLs, cfg.RemoteMachineID, cfg.RemoteMachineToken)
	}
}

func TestActivateRemoteSMSIncludesTenantID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/enroll/sms/verify-and-start" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		if payload["phone_number"] != "19900001111" || payload["verify_code"] != "123456" || payload["tenant_id"] != "tenant-phone" {
			t.Fatalf("payload = %#v", payload)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","hub_id":"hub-phone","tenant_id":"tenant-phone","tenant_name":"Phone Tenant","user_id":"usr_phone","email":"phone:19900001111","machine_id":"machine-1","machine_token":"token-1","viewer_token":"viewer-1","rebound_existing_user":true}`))
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir(), remoteActivationBackgroundDisabled: true}
	got, err := app.ActivateRemoteSMS(server.URL, "199-0000 1111", "123456", "", "tenant-phone", "hub-phone")
	if err != nil {
		t.Fatalf("ActivateRemoteSMS() error = %v", err)
	}
	if got.TenantID != "tenant-phone" || got.UserID != "usr_phone" || got.MachineID != "machine-1" || !got.ReboundExistingUser {
		t.Fatalf("result = %#v", got)
	}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if cfg.RemoteMobile != "19900001111" {
		t.Fatalf("RemoteMobile = %q, want 19900001111", cfg.RemoteMobile)
	}
	if cfg.RemoteHubID != "hub-phone" {
		t.Fatalf("RemoteHubID = %q, want hub-phone", cfg.RemoteHubID)
	}
}

func TestVerifyRemoteActivationPreservesCredentialsOnNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/entry/probe" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"not_found","message":"tenant route not found"}`))
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	if _, err := app.LoadConfig(); err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if err := app.PatchConfig(func(cfg *corelib.AppConfig) {
		cfg.RemoteHubURL = server.URL
		cfg.RemoteEmail = "owner@example.com"
		cfg.RemoteMachineID = "machine-1"
		cfg.RemoteMachineToken = "token-1"
		cfg.RemoteViewerToken = "viewer-1"
	}); err != nil {
		t.Fatalf("PatchConfig() error = %v", err)
	}

	if !app.VerifyRemoteActivation() {
		t.Fatal("not_found probe should be treated as still valid locally")
	}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() after verify error = %v", err)
	}
	if cfg.RemoteMachineID != "machine-1" || cfg.RemoteMachineToken != "token-1" || cfg.RemoteViewerToken != "viewer-1" {
		t.Fatalf("credentials were unexpectedly cleared: machine_id=%q token=%q viewer=%q", cfg.RemoteMachineID, cfg.RemoteMachineToken, cfg.RemoteViewerToken)
	}
}

func TestVerifyRemoteActivationClearsCredentialsOnBlocked(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/entry/probe" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"blocked","message":"user blocked by admin"}`))
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	if _, err := app.LoadConfig(); err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if err := app.PatchConfig(func(cfg *corelib.AppConfig) {
		cfg.RemoteHubURL = server.URL
		cfg.RemoteEmail = "owner@example.com"
		cfg.RemoteMachineID = "machine-1"
		cfg.RemoteMachineToken = "token-1"
		cfg.RemoteViewerToken = "viewer-1"
	}); err != nil {
		t.Fatalf("PatchConfig() error = %v", err)
	}

	if app.VerifyRemoteActivation() {
		t.Fatal("blocked probe should invalidate local activation")
	}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() after verify error = %v", err)
	}
	if cfg.RemoteMachineID != "" || cfg.RemoteMachineToken != "" || cfg.RemoteViewerToken != "" {
		t.Fatalf("credentials were not cleared: machine_id=%q token=%q viewer=%q", cfg.RemoteMachineID, cfg.RemoteMachineToken, cfg.RemoteViewerToken)
	}
	if cfg.RemoteEmail != "owner@example.com" || cfg.RemoteHubURL != server.URL {
		t.Fatalf("email/hub should be preserved after clear: email=%q hub=%q", cfg.RemoteEmail, cfg.RemoteHubURL)
	}
}

func TestResolveProjectProxyURL_ProjectSpecificPreferred(t *testing.T) {
	app := &App{}
	cfg := corelib.AppConfig{
		CurrentProject: "proj-1",
		Projects: []corelib.ProjectConfig{
			{
				Id:            "proj-1",
				Path:          filepath.Clean(`D:\workprj\proj`),
				ProxyHost:     "project-proxy.local",
				ProxyPort:     "7890",
				ProxyUsername: "alice",
				ProxyPassword: "secret",
			},
		},
		DefaultProxyHost:     "global-proxy.local",
		DefaultProxyPort:     "8080",
		DefaultProxyUsername: "global-user",
		DefaultProxyPassword: "global-pass",
	}

	got := app.resolveProjectProxyURL(cfg, filepath.Clean(`D:\workprj\proj`))
	want := "http://alice:secret@project-proxy.local:7890"
	if got != want {
		t.Fatalf("resolveProjectProxyURL() = %q, want %q", got, want)
	}
}

func TestResolveProjectProxyURL_FallsBackToDefault(t *testing.T) {
	app := &App{}
	cfg := corelib.AppConfig{
		CurrentProject:       "proj-1",
		Projects:             []corelib.ProjectConfig{{Id: "proj-1", Path: filepath.Clean(`D:\workprj\proj`)}},
		DefaultProxyHost:     "global-proxy.local",
		DefaultProxyPort:     "8080",
		DefaultProxyUsername: "global-user",
		DefaultProxyPassword: "global-pass",
	}

	got := app.resolveProjectProxyURL(cfg, filepath.Clean(`D:\workprj\proj`))
	want := "http://global-user:global-pass@global-proxy.local:8080"
	if got != want {
		t.Fatalf("resolveProjectProxyURL() = %q, want %q", got, want)
	}
}

func TestBuildClaudeLaunchEnv_SetsAnthropicFields(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{}
	model := &corelib.ModelConfig{
		ModelName: "ChatFire",
		ModelId:   "claude-sonnet-4",
		ModelUrl:  "https://api.example.com/anthropic",
		ApiKey:    "sk-test",
		WireApi:   "anthropic",
	}

	env, err := app.buildClaudeLaunchEnv(corelib.AppConfig{}, model, filepath.Clean(`D:\workprj\proj`), false)
	if err != nil {
		t.Fatalf("buildClaudeLaunchEnv() error = %v", err)
	}

	if env["ANTHROPIC_AUTH_TOKEN"] != "sk-test" {
		t.Fatalf("ANTHROPIC_AUTH_TOKEN = %q", env["ANTHROPIC_AUTH_TOKEN"])
	}
	if env["ANTHROPIC_BASE_URL"] != "https://api.example.com/anthropic" {
		t.Fatalf("ANTHROPIC_BASE_URL = %q", env["ANTHROPIC_BASE_URL"])
	}
	if env["ANTHROPIC_MODEL"] != "claude-sonnet-4" {
		t.Fatalf("ANTHROPIC_MODEL = %q", env["ANTHROPIC_MODEL"])
	}
	if env["CLAUDE_CODE_USE_COLORS"] != "true" {
		t.Fatalf("CLAUDE_CODE_USE_COLORS = %q", env["CLAUDE_CODE_USE_COLORS"])
	}
	if env["CLAUDE_CODE_MAX_OUTPUT_TOKENS"] != "128000" {
		t.Fatalf("CLAUDE_CODE_MAX_OUTPUT_TOKENS = %q", env["CLAUDE_CODE_MAX_OUTPUT_TOKENS"])
	}
}

func TestBuildClaudeLaunchEnv_CodegenWritesDedicatedSettings(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{}
	model := &corelib.ModelConfig{
		ModelName: "codegen",
		ModelId:   "claude-codegen-1",
		ModelUrl:  "http://127.0.0.1:5001/anthropic",
		ApiKey:    "cg-test",
		WireApi:   "anthropic",
	}

	env, err := app.buildClaudeLaunchEnv(corelib.AppConfig{}, model, filepath.Clean(`D:\workprj\proj`), false)
	if err != nil {
		t.Fatalf("buildClaudeLaunchEnv() error = %v", err)
	}

	if env["ANTHROPIC_BASE_URL"] != "http://127.0.0.1:5001/anthropic" {
		t.Fatalf("ANTHROPIC_BASE_URL = %q", env["ANTHROPIC_BASE_URL"])
	}

	codegenSettings, err := configfile.ReadCodeGenSettings()
	if err != nil {
		t.Fatalf("ReadCodeGenSettings() error = %v", err)
	}
	if codegenSettings == nil {
		t.Fatal("expected codegen settings to be written")
	}
	envMap, _ := codegenSettings["env"].(map[string]interface{})
	if got, _ := envMap["ANTHROPIC_AUTH_TOKEN"].(string); got != "cg-test" {
		t.Fatalf("codegen ANTHROPIC_AUTH_TOKEN = %q", got)
	}
	if got, _ := envMap["ANTHROPIC_MODEL"].(string); got != "claude-codegen-1" {
		t.Fatalf("codegen ANTHROPIC_MODEL = %q", got)
	}
}

func TestBuildClaudeLaunchEnv_EnablesTeamModeAndProxy(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{}
	projectPath := filepath.Clean(`D:\workprj\proj`)
	cfg := corelib.AppConfig{
		CurrentProject: "proj-1",
		Projects: []corelib.ProjectConfig{
			{
				Id:       "proj-1",
				Path:     projectPath,
				TeamMode: true,
			},
		},
		DefaultProxyHost:     "proxy.local",
		DefaultProxyPort:     "8081",
		DefaultProxyUsername: "bob",
		DefaultProxyPassword: "pwd",
	}
	model := &corelib.ModelConfig{
		ModelName: "ChatFire",
		ModelId:   "claude-sonnet-4",
		ModelUrl:  "https://api.example.com/anthropic",
		ApiKey:    "sk-test",
		WireApi:   "anthropic",
	}

	env, err := app.buildClaudeLaunchEnv(cfg, model, projectPath, true)
	if err != nil {
		t.Fatalf("buildClaudeLaunchEnv() error = %v", err)
	}

	if env["CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS"] != "1" {
		t.Fatalf("CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS = %q", env["CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS"])
	}
	wantProxy := "http://bob:pwd@proxy.local:8081"
	if env["HTTP_PROXY"] != wantProxy || env["HTTPS_PROXY"] != wantProxy {
		t.Fatalf("proxy env mismatch: HTTP_PROXY=%q HTTPS_PROXY=%q", env["HTTP_PROXY"], env["HTTPS_PROXY"])
	}
}

func TestBuildClaudeLaunchEnv_RejectsNonAnthropicWireAPI(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{}
	model := &corelib.ModelConfig{
		ModelName: "ChatFire",
		ModelId:   "claude-sonnet-4",
		ModelUrl:  "https://api.example.com/v1",
		ApiKey:    "sk-test",
		WireApi:   "responses",
	}

	_, err := app.buildClaudeLaunchEnv(corelib.AppConfig{}, model, filepath.Clean(`D:\workprj\proj`), false)
	if err == nil {
		t.Fatal("expected error for non-anthropic wire_api, got nil")
	}
	if !strings.Contains(err.Error(), "must use anthropic wire_api") {
		t.Fatalf("error = %q", err.Error())
	}
}

func TestBuildClaudeLaunchSpec_UsesCurrentProjectAndTitle(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome, remoteActivationBackgroundDisabled: true}
	projectPath := filepath.Clean(`D:\workprj\proj`)
	cfg := corelib.AppConfig{
		CurrentProject: "proj-1",
		Projects: []corelib.ProjectConfig{
			{
				Id:       "proj-1",
				Path:     projectPath,
				TeamMode: true,
			},
		},
		Claude: corelib.ToolConfig{
			CurrentModel: "ChatFire",
			Models: []corelib.ModelConfig{
				{
					ModelName: "ChatFire",
					ModelId:   "claude-sonnet-4",
					ModelUrl:  "https://api.example.com/anthropic",
					ApiKey:    "sk-test",
				},
			},
		},
	}

	spec, err := app.buildClaudeLaunchSpec(cfg, true, false, "", projectPath, false)
	if err != nil {
		t.Fatalf("buildClaudeLaunchSpec() error = %v", err)
	}

	if spec.Tool != "claude" {
		t.Fatalf("Tool = %q", spec.Tool)
	}
	if spec.ProjectPath != projectPath {
		t.Fatalf("ProjectPath = %q, want %q", spec.ProjectPath, projectPath)
	}
	if spec.Title != "proj" {
		t.Fatalf("Title = %q", spec.Title)
	}
	if !spec.TeamMode {
		t.Fatal("TeamMode = false, want true")
	}
	if !spec.YoloMode {
		t.Fatal("YoloMode = false, want true")
	}
	if spec.Env["ANTHROPIC_MODEL"] != "claude-sonnet-4" {
		t.Fatalf("ANTHROPIC_MODEL = %q", spec.Env["ANTHROPIC_MODEL"])
	}
}

func TestBuildClaudeLaunchSpec_UsesSavedCurrentProjectWhenProjectDirEmpty(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome, remoteActivationBackgroundDisabled: true}
	projectPath := filepath.Clean(`D:\workprj\proj-saved`)
	cfg := corelib.AppConfig{
		CurrentProject: "proj-1",
		Projects: []corelib.ProjectConfig{
			{
				Id:       "proj-1",
				Path:     projectPath,
				TeamMode: true,
			},
		},
		Claude: corelib.ToolConfig{
			CurrentModel: "ChatFire",
			Models: []corelib.ModelConfig{
				{
					ModelName: "ChatFire",
					ModelId:   "claude-sonnet-4",
					ModelUrl:  "https://api.example.com/anthropic",
					ApiKey:    "sk-test",
				},
			},
		},
	}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	spec, err := app.buildClaudeLaunchSpec(cfg, false, false, "", "", false)
	if err != nil {
		t.Fatalf("buildClaudeLaunchSpec() error = %v", err)
	}
	if spec.ProjectPath != projectPath {
		t.Fatalf("ProjectPath = %q, want %q", spec.ProjectPath, projectPath)
	}
}

// Note: Tests for resolveRemoteHubURL and buildCenterURLList have been removed
// because these functions are now delegated to corelib/remote.EnrollmentClient.
// The corresponding tests are in corelib/remote/enrollment_test.go.

func TestActivateRemote_ResolvesHubAndPersistsIdentity(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	remote.InvalidateCenterCache()

	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/enroll/start":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status":        "approved",
				"user_id":       "u_123",
				"tenant_id":     "tenant_123",
				"tenant_name":   "Acme Team",
				"email":         "user@example.com",
				"sn":            "SN-2026-000001",
				"machine_id":    "m_123",
				"machine_token": "mt_123",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer hub.Close()

	center := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/entry/resolve" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"email": "user@example.com",
			"mode":  "single",
			"default_hub": map[string]any{
				"hub_id":   "hub_1",
				"base_url": hub.URL,
				"pwa_url":  hub.URL + "/app?email=user@example.com&entry=app",
			},
			"hubs": []map[string]any{
				{
					"hub_id":   "hub_1",
					"base_url": hub.URL,
					"pwa_url":  hub.URL + "/app?email=user@example.com&entry=app",
				},
			},
		})
	}))
	defer center.Close()

	// Override defaults so the enrollment client doesn't probe real HubCenter URLs.
	origDefaults := remote.DefaultRemoteHubCenterURLs
	origDefault := remote.DefaultRemoteHubCenterURL
	origGUIDefault := defaultRemoteHubCenterURL
	remote.DefaultRemoteHubCenterURLs = []string{center.URL}
	remote.DefaultRemoteHubCenterURL = center.URL
	defaultRemoteHubCenterURL = center.URL
	defer func() {
		remote.DefaultRemoteHubCenterURLs = origDefaults
		remote.DefaultRemoteHubCenterURL = origDefault
		defaultRemoteHubCenterURL = origGUIDefault
	}()

	app := &App{testHomeDir: tmpHome, remoteActivationBackgroundDisabled: true}
	cfg := corelib.AppConfig{
		RemoteHubCenterURL: center.URL,
		RemoteNickname:     "Old Desk",
	}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	result, err := app.ActivateRemote("user@example.com", "", "")
	if err != nil {
		t.Fatalf("ActivateRemote() error = %v", err)
	}
	if result.MachineID != "m_123" || result.MachineToken != "mt_123" {
		t.Fatalf("unexpected activation result: %+v", result)
	}
	if result.TenantID != "tenant_123" || result.TenantName != "Acme Team" {
		t.Fatalf("unexpected tenant result: %+v", result)
	}

	saved, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if saved.RemoteHubURL != hub.URL {
		t.Fatalf("RemoteHubURL = %q, want %q", saved.RemoteHubURL, hub.URL)
	}
	if saved.RemoteHubID != "hub_1" {
		t.Fatalf("RemoteHubID = %q, want %q", saved.RemoteHubID, "hub_1")
	}
	if saved.RemoteEmail != "user@example.com" || saved.RemoteSN != "SN-2026-000001" {
		t.Fatalf("saved identity mismatch: %+v", saved)
	}
	if saved.RemoteMachineID != "m_123" || saved.RemoteMachineToken != "mt_123" {
		t.Fatalf("saved machine identity mismatch: %+v", saved)
	}
	if saved.RemoteTenantID != "tenant_123" || saved.RemoteTenantName != "Acme Team" {
		t.Fatalf("saved tenant identity mismatch: %+v", saved)
	}
	if saved.RemoteMachineName == "" {
		t.Fatal("RemoteMachineName should be saved after activation")
	}
	if saved.RemoteNickname != "" {
		t.Fatalf("RemoteNickname = %q, want cleared until Hub assigns current nickname", saved.RemoteNickname)
	}
	// Verify RemoteEnabled is set
	if !saved.RemoteEnabled {
		t.Fatal("RemoteEnabled should be true after activation")
	}
}

func TestActivateRemote_ReconfirmsHubViaHubCenterWhenCachedHubURLExists(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	staleHub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("stale cached hub should not be used: %s", r.URL.Path)
	}))
	defer staleHub.Close()

	resolvedHub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/enroll/start":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status":        "ok",
				"tenant_id":     "tenant_dynamic",
				"tenant_name":   "Dynamic Team",
				"email":         "dynamic@example.com",
				"sn":            "SN-DYNAMIC",
				"machine_id":    "m_dynamic",
				"machine_token": "mt_dynamic",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer resolvedHub.Close()

	var resolveHits int32
	center := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/entry/resolve":
			atomic.AddInt32(&resolveHits, 1)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"email":          "dynamic@example.com",
				"mode":           "single",
				"default_hub_id": "hub_dynamic",
				"hubs": []map[string]any{{
					"hub_id":   "hub_dynamic",
					"base_url": resolvedHub.URL,
					"status":   "online",
				}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer center.Close()

	origDefaults := remote.DefaultRemoteHubCenterURLs
	origDefault := remote.DefaultRemoteHubCenterURL
	origGUIDefault := defaultRemoteHubCenterURL
	remote.DefaultRemoteHubCenterURLs = []string{center.URL}
	remote.DefaultRemoteHubCenterURL = center.URL
	defaultRemoteHubCenterURL = center.URL
	defer func() {
		remote.DefaultRemoteHubCenterURLs = origDefaults
		remote.DefaultRemoteHubCenterURL = origDefault
		defaultRemoteHubCenterURL = origGUIDefault
	}()

	app := &App{testHomeDir: tmpHome, remoteActivationBackgroundDisabled: true}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubURL:       staleHub.URL,
		RemoteHubCenterURL: center.URL,
	}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	if _, err := app.ActivateRemote("dynamic@example.com", "", ""); err != nil {
		t.Fatalf("ActivateRemote() error = %v", err)
	}
	if atomic.LoadInt32(&resolveHits) == 0 {
		t.Fatal("HubCenter resolve endpoint was not called")
	}
	saved, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if saved.RemoteHubURL != resolvedHub.URL {
		t.Fatalf("RemoteHubURL = %q, want dynamically resolved %q", saved.RemoteHubURL, resolvedHub.URL)
	}
}

func TestSanitizeHubCenterRegistrationURLsDropsLoopback(t *testing.T) {
	preferred, discovered := sanitizeHubCenterRegistrationURLs(
		"http://127.0.0.1:65140",
		[]string{"http://127.0.0.1:65140", "https://hubs.mypapers.top/", "https://hubs.maclaw.top"},
	)
	if preferred != "https://hubs.mypapers.top" {
		t.Fatalf("preferred = %q, want public URL", preferred)
	}
	want := []string{"https://hubs.mypapers.top", "https://hubs.maclaw.top", "https://hubs2.maclaw.top"}
	if !remote.StringSliceEqual(discovered, want) {
		t.Fatalf("discovered = %#v, want %#v", discovered, want)
	}
}

func TestSanitizeHubCenterRegistrationURLsAllLoopback(t *testing.T) {
	origDefaults := remote.DefaultRemoteHubCenterURLs
	remote.DefaultRemoteHubCenterURLs = nil
	defer func() { remote.DefaultRemoteHubCenterURLs = origDefaults }()

	preferred, discovered := sanitizeHubCenterRegistrationURLs(
		"http://127.0.0.1:65140",
		[]string{"http://localhost:9388"},
	)
	if preferred != "" || discovered != nil {
		t.Fatalf("preferred=%q discovered=%#v, want no unconfirmed HubCenter", preferred, discovered)
	}
}

func TestSanitizeHubCenterRegistrationURLsKeepsDefaultFailover(t *testing.T) {
	origDefaults := remote.DefaultRemoteHubCenterURLs
	remote.DefaultRemoteHubCenterURLs = []string{
		"https://hubs.mypapers.top",
		"https://hubs.maclaw.top",
		"https://hubs2.maclaw.top",
	}
	defer func() { remote.DefaultRemoteHubCenterURLs = origDefaults }()

	preferred, discovered := sanitizeHubCenterRegistrationURLs(
		"https://hubs2.maclaw.top",
		[]string{"https://hubs2.maclaw.top"},
	)
	if preferred != "https://hubs2.maclaw.top" {
		t.Fatalf("preferred = %q, want selected HubCenter first", preferred)
	}
	want := []string{
		"https://hubs2.maclaw.top",
		"https://hubs.mypapers.top",
		"https://hubs.maclaw.top",
	}
	if !remote.StringSliceEqual(discovered, want) {
		t.Fatalf("discovered = %#v, want %#v", discovered, want)
	}
}

func TestActivateRemote_SwitchesToHubProviderWhenRegisteredAccountHasOfficialService(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	var hubURL string
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/enroll/start":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status":        "approved",
				"user_id":       "u_456",
				"email":         "user@example.com",
				"sn":            "SN-2026-000456",
				"machine_id":    "m_456",
				"machine_token": "mt_456",
				"viewer_token":  "viewer-token",
			})
		case "/api/llm/service/account":
			if got := r.Header.Get("Authorization"); got != "Bearer viewer-token" {
				t.Errorf("Authorization = %q, want viewer token", got)
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": map[string]any{
					"active":              true,
					"service_group_ids":   []string{"official"},
					"service_group_names": []string{"MaClaw官方服务"},
					"available_models":    []string{"auto"},
					"default_model":       "auto",
					"hub_llm_base_url":    hubURL + "/api/llm/v1",
					"credits_total":       100,
					"credits_available":   100,
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	hubURL = hub.URL
	defer hub.Close()

	app := &App{testHomeDir: tmpHome, remoteActivationBackgroundDisabled: true}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubURL:             hub.URL,
		MaclawLLMCurrentProvider: "Custom1",
		MaclawLLMUrl:             "https://custom.example.com/v1",
		MaclawLLMKey:             "custom-key",
		MaclawLLMModel:           "gpt-test",
		MaclawLLMProviders: []corelib.MaclawLLMProvider{{
			Name:     "Custom1",
			URL:      "https://custom.example.com/v1",
			Key:      "custom-key",
			Model:    "gpt-test",
			Protocol: "openai",
			IsCustom: true,
		}},
	}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	if _, err := app.ActivateRemote("user@example.com", "", ""); err != nil {
		t.Fatalf("ActivateRemote() error = %v", err)
	}

	saved, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if saved.MaclawLLMCurrentProvider != hubServiceProviderName {
		t.Fatalf("MaclawLLMCurrentProvider = %q, want %q", saved.MaclawLLMCurrentProvider, hubServiceProviderName)
	}
	provider, ok := findProviderByName(saved.MaclawLLMProviders, hubServiceProviderName)
	if !ok {
		t.Fatalf("saved providers missing hub provider: %+v", saved.MaclawLLMProviders)
	}
	if provider.URL != hub.URL+"/api/llm/v1" || provider.Key != "viewer-token" || provider.Model != hubServiceAutoModel || !provider.IsHubService {
		t.Fatalf("unexpected hub provider: %+v", provider)
	}
	if saved.MaclawLLMUrl != provider.URL || saved.MaclawLLMKey != provider.Key || saved.MaclawLLMModel != provider.Model {
		t.Fatalf("legacy fields not synced to hub provider: url=%q key=%q model=%q provider=%+v", saved.MaclawLLMUrl, saved.MaclawLLMKey, saved.MaclawLLMModel, provider)
	}
}

func TestActivateRemote_RemovesStaleHubProviderWhenRegisteredAccountHasNoOfficialService(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	var hubURL string
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/enroll/start":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status":        "approved",
				"user_id":       "u_789",
				"email":         "user@example.com",
				"sn":            "SN-2026-000789",
				"machine_id":    "m_789",
				"machine_token": "mt_789",
				"viewer_token":  "viewer-token-new",
			})
		case "/api/llm/service/account":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": map[string]any{
					"active":           false,
					"available_models": []string{"auto"},
					"default_model":    "auto",
					"hub_llm_base_url": hubURL + "/api/llm/v1",
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	hubURL = hub.URL
	defer hub.Close()

	app := &App{testHomeDir: tmpHome, remoteActivationBackgroundDisabled: true}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubURL:             hub.URL,
		RemoteViewerToken:        "viewer-token-old",
		MaclawLLMCurrentProvider: "Custom1",
		MaclawLLMProviders: []corelib.MaclawLLMProvider{
			{Name: hubServiceProviderName, URL: "https://old-hub.example.com/api/llm/v1", Key: "viewer-token-old", Model: hubServiceAutoModel, Protocol: "openai", IsHubService: true},
			{Name: "Custom1", URL: "https://custom.example.com/v1", Key: "custom-key", Model: "gpt-test", Protocol: "openai", IsCustom: true},
		},
	}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	if _, err := app.ActivateRemote("user@example.com", "", ""); err != nil {
		t.Fatalf("ActivateRemote() error = %v", err)
	}

	saved, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if saved.MaclawLLMCurrentProvider != "Custom1" {
		t.Fatalf("MaclawLLMCurrentProvider = %q, want Custom1", saved.MaclawLLMCurrentProvider)
	}
	if _, ok := findProviderByName(saved.MaclawLLMProviders, hubServiceProviderName); ok {
		t.Fatalf("stale hub provider should be removed when account has no official service: %+v", saved.MaclawLLMProviders)
	}
}

func TestActivateRemote_RemovesStaleHubProviderWhenOfficialServiceAuthorizationFails(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/enroll/start":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status":        "approved",
				"user_id":       "u_890",
				"email":         "user@example.com",
				"sn":            "SN-2026-000890",
				"machine_id":    "m_890",
				"machine_token": "mt_890",
				"viewer_token":  "viewer-token-new",
			})
		case "/api/llm/service/account":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]any{"message": "viewer token expired"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer hub.Close()

	app := &App{testHomeDir: tmpHome, remoteActivationBackgroundDisabled: true}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubURL:             hub.URL,
		RemoteViewerToken:        "viewer-token-old",
		MaclawLLMCurrentProvider: "Custom1",
		MaclawLLMProviders: []corelib.MaclawLLMProvider{
			{Name: hubServiceProviderName, URL: "https://old-hub.example.com/api/llm/v1", Key: "viewer-token-old", Model: hubServiceAutoModel, Protocol: "openai", IsHubService: true},
			{Name: "Custom1", URL: "https://custom.example.com/v1", Key: "custom-key", Model: "gpt-test", Protocol: "openai", IsCustom: true},
		},
	}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	if _, err := app.ActivateRemote("user@example.com", "", ""); err != nil {
		t.Fatalf("ActivateRemote() error = %v", err)
	}

	saved, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if saved.RemoteViewerToken != "viewer-token-new" {
		t.Fatalf("RemoteViewerToken = %q, want new viewer token", saved.RemoteViewerToken)
	}
	if saved.MaclawLLMCurrentProvider != "Custom1" {
		t.Fatalf("MaclawLLMCurrentProvider = %q, want Custom1", saved.MaclawLLMCurrentProvider)
	}
	if _, ok := findProviderByName(saved.MaclawLLMProviders, hubServiceProviderName); ok {
		t.Fatalf("stale hub provider should be removed after authorization failure: %+v", saved.MaclawLLMProviders)
	}
}

func TestActivateRemote_ClearsStaleViewerTokenAndHubProviderWhenEnrollOmitsViewerToken(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/enroll/start":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status":        "approved",
				"user_id":       "u_987",
				"email":         "user@example.com",
				"sn":            "SN-2026-000987",
				"machine_id":    "m_987",
				"machine_token": "mt_987",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer hub.Close()

	app := &App{testHomeDir: tmpHome, remoteActivationBackgroundDisabled: true}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubURL:             hub.URL,
		RemoteViewerToken:        "viewer-token-old",
		MaclawLLMCurrentProvider: hubServiceProviderName,
		MaclawLLMProviders:       []corelib.MaclawLLMProvider{{Name: hubServiceProviderName, URL: hub.URL + "/api/llm/v1", Key: "viewer-token-old", Model: hubServiceAutoModel, Protocol: "openai", IsHubService: true}},
	}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	if _, err := app.ActivateRemote("user@example.com", "", ""); err != nil {
		t.Fatalf("ActivateRemote() error = %v", err)
	}

	saved, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if saved.RemoteHubURL != hub.URL {
		t.Fatalf("RemoteHubURL = %q, want %q", saved.RemoteHubURL, hub.URL)
	}
	if saved.RemoteViewerToken != "" {
		t.Fatalf("RemoteViewerToken = %q, want cleared", saved.RemoteViewerToken)
	}
	if _, ok := findProviderByName(saved.MaclawLLMProviders, hubServiceProviderName); ok {
		t.Fatalf("stale hub provider should be removed after registering different hub without viewer token: %+v", saved.MaclawLLMProviders)
	}
	if saved.MaclawLLMCurrentProvider != "" {
		t.Fatalf("MaclawLLMCurrentProvider = %q, want cleared with stale hub provider", saved.MaclawLLMCurrentProvider)
	}
}

func TestActivateRemote_ReturnsBeforeBackgroundHubConnect(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	var authCount atomic.Int32
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/enroll/start":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status":        "approved",
				"user_id":       "u_234",
				"email":         "user@example.com",
				"sn":            "SN-2026-000234",
				"machine_id":    "m_234",
				"machine_token": "mt_234",
			})
		case "/ws":
			conn, err := upgrader.Upgrade(w, r, nil)
			if err != nil {
				t.Errorf("upgrade websocket: %v", err)
				return
			}
			defer conn.Close()
			time.Sleep(800 * time.Millisecond)

			for {
				var msg map[string]any
				if err := conn.ReadJSON(&msg); err != nil {
					return
				}
				switch msg["type"] {
				case "auth.machine":
					authCount.Add(1)
					_ = conn.WriteJSON(map[string]any{"type": "auth.ok", "payload": map[string]any{"role": "machine"}})
				default:
					_ = conn.WriteJSON(map[string]any{"type": "ack", "payload": map[string]any{"ok": true}})
				}
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer hub.Close()

	app := &App{testHomeDir: tmpHome}
	t.Cleanup(func() { app.shutdown(context.Background()) })
	if err := app.SaveConfig(corelib.AppConfig{RemoteHubURL: hub.URL}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	app.remoteSessions = NewRemoteSessionManager(app)
	start := time.Now()
	result, err := app.ActivateRemote("user@example.com", "", "")
	if err != nil {
		t.Fatalf("ActivateRemote() error = %v", err)
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("ActivateRemote() returned too slowly: %s", elapsed)
	}
	if result.MachineID != "m_234" {
		t.Fatalf("MachineID = %q, want %q", result.MachineID, "m_234")
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if app.remoteSessions != nil && app.remoteSessions.hubClient != nil && app.remoteSessions.hubClient.IsConnected() && authCount.Load() > 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}

	if app.remoteSessions == nil || app.remoteSessions.hubClient == nil {
		t.Fatal("expected remote hub client to be initialized")
	}

	t.Fatalf("hub client did not connect after activation: connected=%v authCount=%d ai_ready=%v init_status=%q",
		app.remoteSessions.hubClient.IsConnected(), authCount.Load(), app.IsAIAssistantReady(), app.GetAIAssistantInitStatus())
}

func TestActivateRemote_SendsNormalizedPlatform(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	original := remotePlatformGOOS
	remotePlatformGOOS = func() string { return "darwin" }
	defer func() {
		remotePlatformGOOS = original
	}()

	var enrollPayload map[string]any
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/enroll/start" {
			http.NotFound(w, r)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&enrollPayload); err != nil {
			t.Fatalf("decode enroll body: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":        "approved",
			"user_id":       "u_345",
			"email":         "user@example.com",
			"sn":            "SN-2026-000345",
			"machine_id":    "m_345",
			"machine_token": "mt_345",
		})
	}))
	defer hub.Close()

	app := &App{testHomeDir: tmpHome, remoteActivationBackgroundDisabled: true}
	if err := app.SaveConfig(corelib.AppConfig{RemoteHubURL: hub.URL}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	if _, err := app.ActivateRemote("user@example.com", "", ""); err != nil {
		t.Fatalf("ActivateRemote() error = %v", err)
	}

	if got := enrollPayload["platform"]; got != "mac" {
		t.Fatalf("platform = %v, want mac", got)
	}
}

func TestActivateRemote_TimesOutSlowEnrollRequest(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	previousTimeout := remoteEnrollTimeout
	remoteEnrollTimeout = 100 * time.Millisecond
	t.Cleanup(func() {
		remoteEnrollTimeout = previousTimeout
	})

	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/enroll/start" {
			http.NotFound(w, r)
			return
		}
		time.Sleep(remoteEnrollTimeout + 50*time.Millisecond)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":        "approved",
			"user_id":       "u_slow",
			"email":         "user@example.com",
			"sn":            "SN-slow",
			"machine_id":    "m_slow",
			"machine_token": "mt_slow",
		})
	}))
	defer hub.Close()

	app := &App{testHomeDir: tmpHome, remoteActivationBackgroundDisabled: true}
	if err := app.SaveConfig(corelib.AppConfig{RemoteHubURL: hub.URL}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	started := time.Now()
	_, err := app.ActivateRemote("user@example.com", "", "")
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if !strings.Contains(err.Error(), "registration timed out") {
		t.Fatalf("expected timeout error, got %v", err)
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("ActivateRemote() took too long: %s", elapsed)
	}
}

func TestSkillMarketAutoLoginThrottlesFailedMachineLogin(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/auth/machine-login" {
			http.NotFound(w, r)
			return
		}
		calls.Add(1)
		w.WriteHeader(http.StatusTooManyRequests)
		_ = json.NewEncoder(w).Encode(map[string]string{"message": "Too many requests, please slow down"})
	}))
	defer server.Close()

	app := &App{testHomeDir: tmpHome}
	if err := app.SaveConfig(corelib.AppConfig{RemoteHubCenterURL: server.URL}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	app.acquireSkillMarketTokenAfterEnroll("user@example.com", "m_123", "viewer-token")
	app.acquireSkillMarketTokenAfterEnroll("user@example.com", "m_123", "viewer-token")

	if got := calls.Load(); got != 1 {
		t.Fatalf("machine-login calls = %d, want throttled to 1", got)
	}
	if next, ok := app.skillMarketAutoLoginNextAttempt.Load().(time.Time); !ok || time.Until(next) <= 0 {
		t.Fatalf("expected future retry time, got %v ok=%v", next, ok)
	}

	app.skillMarketAutoLoginNextAttempt.Store(time.Now().Add(-time.Second))
	app.acquireSkillMarketTokenAfterEnroll("user@example.com", "m_123", "viewer-token")
	if got := calls.Load(); got != 2 {
		t.Fatalf("machine-login calls after retry window = %d, want 2", got)
	}
}

func TestClearRemoteActivation_DisconnectsHubClient(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !websocket.IsWebSocketUpgrade(r) {
			http.NotFound(w, r)
			return
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade websocket: %v", err)
			return
		}
		defer conn.Close()

		for {
			var msg map[string]any
			if err := conn.ReadJSON(&msg); err != nil {
				return
			}
			switch msg["type"] {
			case "auth.machine":
				_ = conn.WriteJSON(map[string]any{"type": "auth.ok", "payload": map[string]any{"role": "machine"}})
			default:
				_ = conn.WriteJSON(map[string]any{"type": "ack", "payload": map[string]any{"ok": true}})
			}
		}
	}))
	defer hub.Close()

	app := &App{testHomeDir: tmpHome}
	cfg := corelib.AppConfig{
		RemoteHubURL:       hub.URL,
		RemoteEmail:        "user@example.com",
		RemoteSN:           "SN-2026-000345",
		RemoteUserID:       "u_345",
		RemoteTenantID:     "tenant_345",
		RemoteTenantName:   "Old Team",
		RemoteMachineID:    "m_345",
		RemoteMachineName:  "old-machine",
		RemoteMachineToken: "mt_345",
		RemoteNickname:     "Old Desk",
	}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	app.remoteSessions = NewRemoteSessionManager(app)
	hubClient := NewRemoteHubClient(app, app.remoteSessions)
	app.remoteSessions.SetHubClient(hubClient)
	if err := hubClient.Connect(); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if hubClient.IsConnected() {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !hubClient.IsConnected() {
		t.Fatal("expected hub client to connect before clearing activation")
	}

	if err := app.ClearRemoteActivation(); err != nil {
		t.Fatalf("ClearRemoteActivation() error = %v", err)
	}
	if hubClient.IsConnected() {
		t.Fatal("expected hub client to disconnect after clearing activation")
	}

	saved, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if saved.RemoteMachineID != "" || saved.RemoteMachineToken != "" || saved.RemoteEmail != "" || saved.RemoteSN != "" {
		t.Fatalf("expected activation identity to be cleared, got %+v", saved)
	}
	if saved.RemoteTenantID != "" || saved.RemoteTenantName != "" || saved.RemoteMachineName != "" || saved.RemoteNickname != "" {
		t.Fatalf("expected hub identity metadata to be cleared, got %+v", saved)
	}
}
